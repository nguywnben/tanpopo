// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package catalog_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	sqlitestore "github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type noopAuditSink struct{}

func (noopAuditSink) Append(context.Context, port.AuditEvent) error { return nil }
func (noopAuditSink) AppendTx(context.Context, *sql.Tx, port.AuditEvent) error {
	return nil
}

type testHarness struct {
	ctx       context.Context
	store     *sqlitestore.Store
	vault     *vault.Store
	snapshots *snapshot.Manager
	repo      *catalog.Repository
	installer *installer.LifecycleService
	broker    *broker.BlindHostCredentialBroker
	clk       *clock.FakeClock
	pubKey    ed25519.PublicKey
	privKey   ed25519.PrivateKey
	keyID     string
	plugins   string
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "tanpopo.db")

	clk := clock.NewFakeClock(time.UnixMilli(1760000000000))
	nowMs := func() int64 { return clk.Now().UnixMilli() }
	st, err := sqlitestore.Open(ctx, sqlitestore.Config{
		DBPath: dbPath,
		NowMs:  nowMs,
	})
	if err != nil {
		t.Fatalf("sqlite.Open failed: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	var masterKey [32]byte
	for i := range masterKey {
		masterKey[i] = byte(i + 1)
	}
	kekProvider, err := vault.NewStaticKEKProvider("kek_test_v1", masterKey[:])
	if err != nil {
		t.Fatalf("NewStaticKEKProvider failed: %v", err)
	}
	vs, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kekProvider,
		CacheTTL:      5 * time.Minute,
		NowMs:         nowMs,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore failed: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	pluginsDir := filepath.Join(tmpDir, "plugins")
	lockPath := filepath.Join(tmpDir, "tanpopo.lock")
	keyID := installer.PublicKeyFingerprint(pub)
	keyring := installer.TrustedKeyring{
		RootPublicKeys: map[string]ed25519.PublicKey{keyID: pub},
	}

	snapMgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:      st.RawDB(),
		NowMs:   nowMs,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatalf("snapshot.NewManager failed: %v", err)
	}

	repo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowMs,
		Snapshots: snapMgr,
		Audit:     noopAuditSink{},
		Keyring:   keyring,
	})
	if err != nil {
		t.Fatalf("catalog.NewRepository failed: %v", err)
	}

	// DEC-020: Wire BOTH Config.Resolver and Config.PolicyResolver explicitly!
	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       repo,
		PolicyResolver: repo,
		Vault:          vs,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          noopAuditSink{},
		NowMs:          nowMs,
		DefaultTimeout: 5 * time.Second,
		MaxTimeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("broker.NewBlindBroker failed: %v", err)
	}

	inst, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           st.RawDB(),
		Audit:        noopAuditSink{},
		PluginsDir:   pluginsDir,
		LockfilePath: lockPath,
		Keyring:      keyring,
		NowMs:        nowMs,
	})
	if err != nil {
		t.Fatalf("installer.NewLifecycleService failed: %v", err)
	}

	return &testHarness{
		ctx:       ctx,
		store:     st,
		vault:     vs,
		snapshots: snapMgr,
		repo:      repo,
		installer: inst,
		broker:    brk,
		clk:       clk,
		pubKey:    pub,
		privKey:   priv,
		keyID:     keyID,
		plugins:   pluginsDir,
	}
}

func (h *testHarness) createOrgProjectApprover(t *testing.T, orgSlug string) (orgID, projID, approverID string) {
	t.Helper()
	org, err := h.store.Tenancy().CreateOrganization(h.ctx, port.CreateOrgParams{
		Slug:        orgSlug,
		DisplayName: "Org " + orgSlug,
	})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}
	proj, err := h.store.Tenancy().CreateProject(h.ctx, org.ID, port.CreateProjectParams{
		Slug:        "proj-main",
		DisplayName: "Main Project",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}
	approverID = "prc_operator_" + orgSlug
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', ?, 'ACTIVE', 1, ?, ?)`,
		approverID, org.ID, "op-"+orgSlug+"@example.com", h.clk.Now().UnixMilli(), h.clk.Now().UnixMilli(),
	); err != nil {
		t.Fatalf("insert principal failed: %v", err)
	}
	roleID := "rb_owner_" + orgSlug
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, ?)`,
		roleID, approverID, h.clk.Now().UnixMilli(),
	); err != nil {
		t.Fatalf("insert role_binding failed: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("snapshot.Refresh failed: %v", err)
	}
	return org.ID, proj.ID, approverID
}

func (h *testHarness) installDeclarativePackage(t *testing.T, pkgID, approverID, manifestYAML string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	mb := []byte(manifestYAML)
	if err := tw.WriteHeader(&tar.Header{
		Name: "manifest.yaml",
		Mode: 0o644,
		Size: int64(len(mb)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(mb); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gw.Close()

	bundleBytes := buf.Bytes()
	sum := sha256.Sum256(bundleBytes)
	hexSum := hex.EncodeToString(sum[:])

	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(bundleBytes),
		Source: installer.SourceSpec{
			Kind:           "oci",
			URI:            fmt.Sprintf("oci://registry.tanpopo.dev/adapters/%s@sha256:%s", strings.TrimPrefix(pkgID, "@"), hexSum),
			ExpectedSHA256: hexSum,
		},
		Signature: installer.ProvenanceSignature{
			PublisherOrRootSig: ed25519.Sign(h.privKey, installer.SignatureSigningPayload(pkgID, "1.0.0", hexSum)),
		},
		ActorID: approverID,
	})
	if err != nil {
		t.Fatalf("StageFreshInstall failed: %v", err)
	}
	approved, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: approverID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatalf("ApproveAndActivate failed: %v", err)
	}
	return approved.InstallationID
}

func TestAC1DeclarativeAdapter4EntityHierarchyAndBlindBrokerValidation(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, projID, approverID := h.createOrgProjectApprover(t, "ac1")

	llm := fakellm.NewServer(t)
	defer llm.Close()
	u, err := url.Parse(llm.URL())
	if err != nil {
		t.Fatal(err)
	}
	manifestYAML := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-openai-test"
  name: OpenAI Declarative Test Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["api_key"]
permissions:
  networkEgress:
    allowedHosts: ["%s"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: openai
  baseUrl: %q
  supportedDialects: ["openai_chat"]
  endpoints:
    models: "/v1/models"
    chat: "/v1/chat/completions"
  auth:
    - slot: "api_key"
      mode: "host_blind_inject"
      location: "header"
      headerName: "Authorization"
      prefix: "Bearer "
  requestMapping:
    injectStreamUsage: true
`, u.Hostname(), llm.URL())

	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-openai-test", approverID, manifestYAML)
	rtEngine, err := declarative.ParseDeclarativeManifestYAML([]byte(manifestYAML))
	if err != nil {
		t.Fatalf("ParseDeclarativeManifestYAML failed: %v", err)
	}

	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    installID,
		AdapterSlug:       "openai",
		SupportedDialects: []string{"openai_chat"},
		AllowedHosts:      []string{u.Hostname()},
	})
	if err != nil {
		t.Fatalf("CreateProviderAdapter failed: %v", err)
	}

	secretPlain := "sk-live-secret-ac1-99999"
	aud := llm.URL()
	secMeta, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:         orgID,
		Name:          "openai-prod-key",
		SecretKind:    vault.SecretKindAPIKey,
		BoundAudience: &aud,
		Plaintext:     []byte(secretPlain),
	})
	if err != nil {
		t.Fatalf("SealSecret failed: %v", err)
	}

	conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		CredentialID:    &secMeta.ID,
		Name:            "OpenAI Main",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	})
	if err != nil {
		t.Fatalf("CreateProviderConnection failed: %v", err)
	}
	if conn.State != catalog.ConnStateDraft {
		t.Fatalf("expected initial state DRAFT, got %s", conn.State)
	}

	lmodel, err := h.repo.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		CanonicalSlug:   "gpt-4o",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   128000,
		MaxOutputTokens: 16384,
		BaseCapabilities: map[string]string{
			"streaming":          catalog.AssuranceVerified,
			"tool_calling":       catalog.AssuranceTested,
			"strict_json_schema": catalog.AssuranceDeclared,
		},
	})
	if err != nil {
		t.Fatalf("CreateLogicalModel failed: %v", err)
	}

	depl, err := h.repo.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             orgID,
		ConnectionID:      conn.ID,
		LogicalModelID:    lmodel.ID,
		UpstreamModelName: "fake-gpt-4o",
		AssuranceLevel:    catalog.AssuranceDeclared,
		Pricing: snapshot.PricingNanoUSD{
			InputPer1MNanoUSD:  2_500_000_000,
			OutputPer1MNanoUSD: 10_000_000_000,
		},
		IdleChunkTimeoutMs: 12000,
	})
	if err != nil {
		t.Fatalf("CreateModelDeployment failed: %v", err)
	}

	// Verify O(1) ConfigSnapshot lookup reflects hierarchy in DRAFT before validation.
	snap := h.snapshots.Current()
	if snap == nil {
		t.Fatal("snapshot Current() is nil")
	}
	if gotConn, ok := snap.LookupProviderConnection(orgID, conn.ID); !ok || gotConn.State != catalog.ConnStateDraft {
		t.Fatalf("snapshot connection lookup unexpected: ok=%v conn=%+v", ok, gotConn)
	}
	depls := snap.LookupDeploymentsForSlug(orgID, "gpt-4o")
	if len(depls) != 1 || depls[0].ID != depl.ID || depls[0].DataRegion != "us" || depls[0].IdleChunkTimeoutMs != 12000 {
		t.Fatalf("unexpected snapshot deployments for slug: %+v", depls)
	}

	// Phase 1 & 2 validation through BlindHostCredentialBroker advances DRAFT -> VALIDATING -> ACTIVE!
	verdict, activeConn, err := h.repo.ValidateConnection(h.ctx, orgID, conn.ID, rtEngine, h.broker)
	if err != nil {
		t.Fatalf("ValidateConnection failed: %v (verdict=%+v)", err, verdict)
	}
	if !verdict.Valid || activeConn.State != catalog.ConnStateActive || len(verdict.DetectedModelIDs) == 0 {
		t.Fatalf("expected Valid=true & State=ACTIVE with models, got verdict=%+v state=%s", verdict, activeConn.State)
	}

	// Execute synchronous chat completion via Declarative + BlindBroker.
	reqSpec, err := rtEngine.TransformRequest(h.ctx, extport.TransformRequestInput{
		RequestID:             "req_sync_1",
		ConfigJSON:            fmt.Sprintf(`{"endpoint_base_url":%q}`, llm.URL()),
		NormalizedRequestJSON: []byte(`{"model":"fake-gpt-4o","messages":[{"role":"user","content":"Hello Tanpopo"}]}`),
		SecretSlotRefs:        map[string]string{"api_key": secMeta.ID},
	})
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	inj, err := catalog.ConvertCredentialInjections(reqSpec.CredentialInjections)
	if err != nil {
		t.Fatal(err)
	}
	syncResp, err := h.broker.Execute(h.ctx, broker.EgressRequest{
		OrgID:                orgID,
		ConnectionID:         conn.ID,
		Method:               reqSpec.Method,
		TargetURL:            reqSpec.TargetURL,
		SafeHeaders:          reqSpec.SafeHeaders,
		BodyBytes:            reqSpec.BodyBytes,
		CredentialInjections: inj,
		RequestID:            "req_sync_1",
	})
	if err != nil {
		t.Fatalf("BlindBroker synchronous inference failed: %v", err)
	}
	normResp, err := rtEngine.TransformResponse(h.ctx, extport.TransformResponseInput{
		RequestID:       "req_sync_1",
		HTTPStatus:      int32(syncResp.StatusCode),
		RawResponseBody: syncResp.Body,
		Modality:        "CHAT",
	})
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if normResp.UpstreamModelID != "fake-gpt-4o" || normResp.Usage == nil || normResp.Usage.OutputTokens == 0 {
		t.Fatalf("unexpected TransformResponse output: %+v", normResp)
	}

	// Verify BlindBroker injected secret upstream without returning it anywhere in snapshots/resolver.
	reqs := llm.Requests()
	if len(reqs) < 2 {
		t.Fatalf("expected >= 2 upstream requests, got %d", len(reqs))
	}
	if reqs[len(reqs)-1].Headers.Get("Authorization") != "Bearer "+secretPlain {
		t.Fatalf("expected injected bearer credential upstream, got %q", reqs[len(reqs)-1].Headers.Get("Authorization"))
	}

	// Execute SSE stream request and verify CR-09 + SSE frame parsing!
	streamReqSpec, err := rtEngine.TransformRequest(h.ctx, extport.TransformRequestInput{
		RequestID:             "req_sse_1",
		ConfigJSON:            fmt.Sprintf(`{"endpoint_base_url":%q}`, llm.URL()),
		NormalizedRequestJSON: []byte(`{"model":"fake-gpt-4o","stream":true,"messages":[{"role":"user","content":"Stream"}]}`),
		SecretSlotRefs:        map[string]string{"api_key": secMeta.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	var bodyCheck map[string]any
	_ = json.Unmarshal(streamReqSpec.BodyBytes, &bodyCheck)
	if so, _ := bodyCheck["stream_options"].(map[string]any); so == nil || so["include_usage"] != true {
		t.Fatalf("expected CR-09 stream_options.include_usage=true, got %v", bodyCheck)
	}
	streamInj, _ := catalog.ConvertCredentialInjections(streamReqSpec.CredentialInjections)
	sseResp, err := h.broker.Execute(h.ctx, broker.EgressRequest{
		OrgID:                orgID,
		ConnectionID:         conn.ID,
		Method:               streamReqSpec.Method,
		TargetURL:            streamReqSpec.TargetURL,
		SafeHeaders:          streamReqSpec.SafeHeaders,
		BodyBytes:            streamReqSpec.BodyBytes,
		CredentialInjections: streamInj,
		RequestID:            "req_sse_1",
	})
	if err != nil {
		t.Fatalf("BlindBroker SSE request failed: %v", err)
	}
	streamOut, err := rtEngine.TransformStreamChunk(h.ctx, extport.StreamChunkInput{
		RequestID:        "req_sse_1",
		RawUpstreamChunk: sseResp.Body,
		IsEOF:            true,
	})
	if err != nil {
		t.Fatalf("TransformStreamChunk failed: %v", err)
	}
	if !streamOut.StreamCompleted || len(streamOut.NormalizedSSEFrames) < 2 || streamOut.IncrementalUsage == nil {
		t.Fatalf("expected completed normalized SSE stream + usage, got %+v", streamOut)
	}

	// Promote assurance tier optimistically and verify ConfigSnapshot updated.
	updatedDepl, err := h.repo.UpdateModelDeployment(h.ctx, orgID, depl.ID, catalog.UpdateModelDeploymentParams{
		ExpectedVersion: depl.Version,
		AssuranceLevel:  ptrStr(catalog.AssuranceVerified),
	})
	if err != nil || updatedDepl.AssuranceLevel != catalog.AssuranceVerified {
		t.Fatalf("UpdateModelDeployment failed: %v (%+v)", err, updatedDepl)
	}
	snapAfter := h.snapshots.Current()
	if dv, ok := snapAfter.LookupModelDeployment(orgID, depl.ID); !ok || dv.AssuranceLevel != catalog.AssuranceVerified {
		t.Fatalf("snapshot deployment not updated to VERIFIED: ok=%v dv=%+v", ok, dv)
	}
}

func TestAC2SelfHostedNoneAuthResolvesInSnapshotWithoutVaultSecret(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, _, approverID := h.createOrgProjectApprover(t, "ac2")

	llm := fakellm.NewServer(t)
	defer llm.Close()
	u, _ := url.Parse(llm.URL())

	manifestYAML := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-vllm-local"
  name: vLLM Self-Hosted
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["%s"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: vllm
  baseUrl: %q
  supportedDialects: ["openai_chat"]
  endpoints:
    models: "/v1/models"
    chat: "/v1/chat/completions"
`, u.Hostname(), llm.URL())

	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-vllm-local", approverID, manifestYAML)
	rtEngine, err := declarative.ParseDeclarativeManifestYAML([]byte(manifestYAML))
	if err != nil {
		t.Fatal(err)
	}
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "vllm",
		AllowedHosts:   []string{u.Hostname()},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Self-hosted LAN connection: SELF_HOSTED_INFRA, allow_private_ip=true, auth_mode=NONE, zero vault secrets!
	conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "LAN vLLM Cluster",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "on-prem",
		BillingMode:     catalog.BillingModeSelfHostedInfra,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatalf("CreateProviderConnection SELF_HOSTED_INFRA auth_mode=NONE failed: %v", err)
	}
	lm, err := h.repo.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:           &orgID,
		CanonicalSlug:   "llama-3.3-70b",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   65536,
		MaxOutputTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	depl, err := h.repo.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             orgID,
		ConnectionID:      conn.ID,
		LogicalModelID:    lm.ID,
		UpstreamModelName: "meta-llama/Llama-3.3-70B-Instruct",
		Pricing:           snapshot.PricingNanoUSD{},
	})
	if err != nil {
		t.Fatalf("CreateModelDeployment failed: %v", err)
	}

	snap := h.snapshots.Current()
	depls := snap.LookupDeploymentsForSlug(orgID, "llama-3.3-70b")
	if len(depls) != 1 || depls[0].ID != depl.ID || depls[0].DataRegion != "on-prem" {
		t.Fatalf("expected self-hosted deployment resolved in ConfigSnapshot, got %+v", depls)
	}

	// ValidateConnection runs cleanly via Config.PolicyResolver with ZERO Vault secret lookups!
	verdict, activeConn, err := h.repo.ValidateConnection(h.ctx, orgID, conn.ID, rtEngine, h.broker)
	if err != nil || !verdict.Valid || activeConn.State != catalog.ConnStateActive {
		t.Fatalf("ValidateConnection zero-slot LAN failed: err=%v verdict=%+v conn=%+v", err, verdict, activeConn)
	}
}

func TestNEG2AndNEG3BillingModeDeletionGuardsIsolationAndSM02(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgA, projA1, approverA := h.createOrgProjectApprover(t, "org-a")
	projA2, err := h.store.Tenancy().CreateProject(h.ctx, orgA, port.CreateProjectParams{
		Slug:        "proj-shared",
		DisplayName: "Shared Sibling Project",
	})
	if err != nil {
		t.Fatal(err)
	}
	projA3, err := h.store.Tenancy().CreateProject(h.ctx, orgA, port.CreateProjectParams{
		Slug:        "proj-isolated",
		DisplayName: "Unshared Sibling Project",
	})
	if err != nil {
		t.Fatal(err)
	}
	orgB, projB, _ := h.createOrgProjectApprover(t, "org-b")

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-neg-test"
  name: Negative Test Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.example.com"]
adapter:
  slug: example
  baseUrl: "https://api.example.com"
`
	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-neg-test", approverA, manifestYAML)
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "example",
		AllowedHosts:   []string{"api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// NEG-2: CONSUMER_SUBSCRIPTION rejected at API validation layer.
	_, err = h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		AdapterID:       adp.ID,
		Name:            "Consumer Subscription Attempt",
		EndpointBaseURL: "https://api.example.com",
		DataRegion:      "us",
		BillingMode:     "CONSUMER_SUBSCRIPTION",
		AuthMode:        catalog.AuthModeNone,
	})
	if !errors.Is(err, catalog.ErrInvalidBillingMode) {
		t.Fatalf("expected ErrInvalidBillingMode, got %v", err)
	}

	// NEG-2: CONSUMER_SUBSCRIPTION also rejected by raw SQLite CHECK constraint.
	_, rawErr := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO provider_connections (
			id, org_id, adapter_id, name, endpoint_base_url, allow_private_ip,
			allowed_cidrs_json, data_region, billing_mode, auth_mode, state,
			shared_project_ids_json, config_json, version, created_at_ms, updated_at_ms
		) VALUES ('con_bad', ?, ?, 'Bad', 'https://api.example.com', 0, '[]', 'us', 'CONSUMER_SUBSCRIPTION', 'NONE', 'DRAFT', '[]', '{}', 1, 1, 1)`,
		orgA, adp.ID,
	)
	if rawErr == nil || !strings.Contains(rawErr.Error(), "CHECK constraint failed") {
		t.Fatalf("expected SQLite CHECK constraint rejection on CONSUMER_SUBSCRIPTION, got %v", rawErr)
	}

	// Create valid connection owned by projA1, shared downward only to projA2.
	conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:            orgA,
		OwnerProjectID:   &projA1,
		AdapterID:        adp.ID,
		Name:             "Scoped Shared Connection",
		EndpointBaseURL:  "https://api.example.com",
		DataRegion:       "us",
		BillingMode:      catalog.BillingModeCommercialAPI,
		AuthMode:         catalog.AuthModeNone,
		SharedProjectIDs: []string{projA2.ID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Cross-org isolation: Org B cannot resolve Org A's connection in repo or snapshot!
	if _, err := h.repo.GetProviderConnection(h.ctx, orgB, conn.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-org GetProviderConnection, got %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgB,
		OwnerProjectID:  &projA1, // cross-org project must fail closed
		AdapterID:       adp.ID,
		Name:            "Cross-Org Owner",
		EndpointBaseURL: "https://api.example.com",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-org project wiring, got %v", err)
	}

	// Downward sharing visibility in ConfigSnapshot.
	snapConn, ok := h.snapshots.Current().LookupProviderConnection(orgA, conn.ID)
	if !ok {
		t.Fatal("missing connection in snapshot")
	}
	if !snapConn.VisibleToProject(orgA, projA1) || !snapConn.VisibleToProject(orgA, projA2.ID) {
		t.Fatal("expected owner and explicitly shared sibling project to see connection")
	}
	if snapConn.VisibleToProject(orgA, projA3.ID) || snapConn.VisibleToProject(orgB, projB) {
		t.Fatal("unshared project or other org must not see connection")
	}

	lm, err := h.repo.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		CanonicalSlug:   "neg-model",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   8192,
		MaxOutputTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	depl, err := h.repo.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             orgA,
		ConnectionID:      conn.ID,
		LogicalModelID:    lm.ID,
		UpstreamModelName: "neg-model",
		Pricing:           snapshot.PricingNanoUSD{},
	})
	if err != nil {
		t.Fatal(err)
	}

	// NEG-3: Deleting ProviderAdapter or ProviderConnection or LogicalModel with active ModelDeployment -> ErrResourceInUse.
	if err := h.repo.DeleteProviderAdapter(h.ctx, adp.ID); !errors.Is(err, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse deleting referenced adapter, got %v", err)
	}
	if err := h.repo.DeleteProviderConnection(h.ctx, orgA, conn.ID, conn.Version+1); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict for a stale delete ETag, got %v", err)
	}
	if err := h.repo.DeleteProviderConnection(h.ctx, orgA, conn.ID, conn.Version); !errors.Is(err, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse deleting referenced connection, got %v", err)
	}
	if err := h.repo.DeleteLogicalModel(h.ctx, nil, lm.ID); !errors.Is(err, port.ErrResourceInUse) {
		t.Fatalf("expected ErrResourceInUse deleting referenced logical model, got %v", err)
	}

	// SM-02 guard: Cannot jump DRAFT -> ACTIVE without VALIDATING.
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgA, conn.ID, catalog.ConnStateActive, conn.Version, "skip_probe"); !errors.Is(err, catalog.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition on DRAFT->ACTIVE, got %v", err)
	}
	// Emergency revocation moves DRAFT -> REVOKED and fails closed in ResolveConnectionPolicy.
	revoked, err := h.repo.TransitionConnectionState(h.ctx, orgA, conn.ID, catalog.ConnStateRevoked, conn.Version, "compromised")
	if err != nil || revoked.State != catalog.ConnStateRevoked {
		t.Fatalf("expected REVOKED transition, got err=%v state=%+v", err, revoked)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgA, conn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected REVOKED connection to fail ResolveConnectionPolicy, got %v", err)
	}

	// Unwind references cleanly -> deletion now succeeds.
	if err := h.repo.DeleteModelDeployment(h.ctx, orgA, depl.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DeleteProviderConnection(h.ctx, orgA, conn.ID, revoked.Version); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DeleteProviderAdapter(h.ctx, adp.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DeleteLogicalModel(h.ctx, nil, lm.ID); err != nil {
		t.Fatal(err)
	}
}

func ptrStr(s string) *string { return &s }

func TestConnectionHealthFailureRedactionQuarantineAndOptimisticLock(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, _, approverID := h.createOrgProjectApprover(t, "health")

	llm := fakellm.NewServer(t)
	defer llm.Close()
	u, _ := url.Parse(llm.URL())

	manifestYAML := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-health-test"
  name: Health Test Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["api_key"]
permissions:
  networkEgress:
    allowedHosts: ["%s"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: health
  baseUrl: %q
  supportedDialects: ["openai_chat"]
  endpoints:
    models: "/v1/models"
    chat: "/v1/chat/completions"
  auth:
    - slot: "api_key"
      mode: "host_blind_inject"
      location: "header"
      headerName: "Authorization"
      prefix: "Bearer "
`, u.Hostname(), llm.URL())

	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-health-test", approverID, manifestYAML)
	rtEngine, err := declarative.ParseDeclarativeManifestYAML([]byte(manifestYAML))
	if err != nil {
		t.Fatal(err)
	}
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "health",
		AllowedHosts:   []string{u.Hostname()},
	})
	if err != nil {
		t.Fatal(err)
	}

	secretPlain := "sk-sensitive-echo-trap-77777"
	aud := llm.URL()
	secMeta, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:         orgID,
		Name:          "health-key",
		SecretKind:    vault.SecretKindAPIKey,
		BoundAudience: &aud,
		Plaintext:     []byte(secretPlain),
	})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		CredentialID:    &secMeta.ID,
		Name:            "Health Connection",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "eu",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Upstream echoes the secret on 401 -> ValidateConnection reverts to DRAFT and NEVER leaks secret!
	llm.EnqueueFault("http_status=401,echo_auth_header_in_error=1")
	verdict, afterFail, err := h.repo.ValidateConnection(h.ctx, orgID, conn.ID, rtEngine, h.broker)
	if err != nil {
		t.Fatalf("unexpected ValidateConnection infra error on HTTP 401: %v", err)
	}
	if verdict.Valid || verdict.FailureCode != "auth_failed" || afterFail.State != catalog.ConnStateDraft {
		t.Fatalf("expected Valid=false auth_failed & DRAFT revert, got verdict=%+v state=%s", verdict, afterFail.State)
	}
	if strings.Contains(verdict.SanitizedMessage, secretPlain) {
		t.Fatalf("SECURITY VIOLATION: secret leaked into verdict.SanitizedMessage: %q", verdict.SanitizedMessage)
	}

	// Healthy probe now succeeds DRAFT -> VALIDATING -> ACTIVE.
	verdict2, activeConn, err := h.repo.ValidateConnection(h.ctx, orgID, conn.ID, rtEngine, h.broker)
	if err != nil || !verdict2.Valid || activeConn.State != catalog.ConnStateActive {
		t.Fatalf("expected second ValidateConnection to reach ACTIVE, got err=%v verdict=%+v state=%s", err, verdict2, activeConn.State)
	}

	// Runtime feedback transitions: ACTIVE -> DEGRADED -> ACTIVE -> AUTH_EXPIRED_REAUTH_REQUIRED.
	degraded, err := h.repo.RecordRuntimeHealthOutcome(h.ctx, orgID, conn.ID, &extport.CanonicalProviderError{
		CanonicalCode:      "upstream_unavailable",
		RetryablePreStream: true,
	})
	if err != nil || degraded.State != catalog.ConnStateDegraded {
		t.Fatalf("expected DEGRADED transition, got err=%v conn=%+v", err, degraded)
	}
	recovered, err := h.repo.RecordRuntimeHealthOutcome(h.ctx, orgID, conn.ID, nil)
	if err != nil || recovered.State != catalog.ConnStateActive {
		t.Fatalf("expected DEGRADED->ACTIVE recovery, got err=%v conn=%+v", err, recovered)
	}
	expired, err := h.repo.RecordRuntimeHealthOutcome(h.ctx, orgID, conn.ID, &extport.CanonicalProviderError{
		CanonicalCode:           "upstream_auth_failed",
		TriggerConnectionReauth: true,
	})
	if err != nil || expired.State != catalog.ConnStateAuthExpired {
		t.Fatalf("expected AUTH_EXPIRED_REAUTH_REQUIRED, got err=%v conn=%+v", err, expired)
	}

	// Optimistic lock mismatch fails with port.ErrVersionConflict.
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, conn.ID, catalog.ConnStateSuspendedAdmin, expired.Version-1, "stale"); !errors.Is(err, port.ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict on stale version, got %v", err)
	}

	// Quarantining backing extension installation immediately blocks broker slot/policy resolution.
	if _, _, err := h.repo.ValidateConnection(h.ctx, orgID, conn.ID, rtEngine, h.broker); err != nil {
		t.Fatalf("re-validate failed: %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET state = 'QUARANTINED' WHERE id = ?`, installID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, conn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ErrConnectionNotReady when installation QUARANTINED, got %v", err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, conn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ErrConnectionNotReady on ResolveSlot when QUARANTINED, got %v", err)
	}
}

func TestCatalogSecurityAndFailClosedRemediations(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgA, projA1, approverA := h.createOrgProjectApprover(t, "sec-a")
	projA2, err := h.store.Tenancy().CreateProject(h.ctx, orgA, port.CreateProjectParams{
		Slug:        "proj-sibling",
		DisplayName: "Sibling Project",
	})
	if err != nil {
		t.Fatal(err)
	}
	orgB, _, _ := h.createOrgProjectApprover(t, "sec-b")

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-sec-rem"
  name: Sec Rem Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["api_key"]
  - slot: "organization"
    requiredForAuthMethods: ["api_key"]
permissions:
  networkEgress:
    allowedHosts: ["api.example.com", "127.0.0.1"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: secrem
  baseUrl: "https://api.example.com"
`
	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-sec-rem", approverA, manifestYAML)
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "secrem",
		AllowedHosts:   []string{"api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Project-owned secret belonging to projA1.
	projSecret, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:          orgA,
		OwnerProjectID: &projA1,
		Name:           "projA1-secret",
		SecretKind:     vault.SecretKindAPIKey,
		Plaintext:      []byte("sk-projA1-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	siblingProjectID := projA2.ID
	siblingSecret, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:          orgA,
		OwnerProjectID: &siblingProjectID,
		Name:           "projA2-secret",
		SecretKind:     vault.SecretKindAPIKey,
		Plaintext:      []byte("sk-projA2-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Project-owned secret CANNOT bind to org-wide connection (OwnerProjectID == nil) or downward-shared connection.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		OwnerProjectID:  nil,
		AdapterID:       adp.ID,
		CredentialID:    &projSecret.ID,
		Name:            "Org-Wide Escalation",
		EndpointBaseURL: "https://api.example.com",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput binding project secret to org-wide connection, got %v", err)
	}

	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:            orgA,
		OwnerProjectID:   &projA1,
		SharedProjectIDs: []string{projA2.ID},
		AdapterID:        adp.ID,
		Name:             "Shared Escalation",
		EndpointBaseURL:  "https://api.example.com",
		DataRegion:       "us",
		BillingMode:      catalog.BillingModeCommercialAPI,
		AuthMode:         catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: projSecret.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput binding project secret to downward-shared connection, got %v", err)
	}

	// 2. Eager validation guards: AllowPrivateIP without AllowedCIDRs and duplicate slot names.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		AdapterID:       adp.ID,
		Name:            "Missing CIDRs",
		EndpointBaseURL: "https://api.example.com",
		AllowPrivateIP:  true,
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on allow_private_ip=true with empty CIDRs, got %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		OwnerProjectID:  &projA1,
		AdapterID:       adp.ID,
		Name:            "Dup Slot",
		EndpointBaseURL: "https://api.example.com",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: projSecret.ID},
			{SlotName: "api_key", CredentialID: projSecret.ID},
		},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on duplicate slot_name, got %v", err)
	}

	// Valid unshared projA1 connection bound to projSecret.
	validConn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		OwnerProjectID:  &projA1,
		AdapterID:       adp.ID,
		CredentialID:    &projSecret.ID,
		Name:            "Strict Project Conn",
		EndpointBaseURL: "https://api.example.com",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.UpdateProviderConnection(h.ctx, catalog.UpdateProviderConnectionParams{
		OrgID: orgA, ConnectionID: validConn.ID, ExpectedVersion: validConn.Version,
		CredentialID: &siblingSecret.ID,
	}); err == nil || !strings.Contains(err.Error(), "conflicts with secret_bindings api_key") {
		t.Fatalf("expected explicit primary to conflict with existing api_key binding, got %v", err)
	}
	legacyConn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		OwnerProjectID:  &projA1,
		AdapterID:       adp.ID,
		CredentialID:    &projSecret.ID,
		Name:            "Legacy Primary Connection",
		EndpointBaseURL: "https://api.example.com",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `DELETE FROM connection_secret_bindings WHERE org_id = ? AND connection_id = ? AND slot_name = 'api_key'`, orgA, legacyConn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.UpdateProviderConnection(h.ctx, catalog.UpdateProviderConnectionParams{
		OrgID: orgA, ConnectionID: legacyConn.ID, ExpectedVersion: legacyConn.Version,
		CredentialID: &siblingSecret.ID, MergeBindings: true,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "organization", CredentialID: projSecret.ID}},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected explicit out-of-scope primary credential to be rejected on partial merge, got %v", err)
	}
	legacyMerged, err := h.repo.UpdateProviderConnection(h.ctx, catalog.UpdateProviderConnectionParams{
		OrgID: orgA, ConnectionID: legacyConn.ID, ExpectedVersion: legacyConn.Version,
		MergeBindings:  true,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "organization", CredentialID: projSecret.ID}},
	})
	if err != nil {
		t.Fatalf("merge into legacy connection without api_key binding: %v", err)
	}
	if legacyMerged.CredentialID == nil || *legacyMerged.CredentialID != projSecret.ID {
		t.Fatalf("legacy primary credential changed during secondary-slot merge: got %v, want %q", legacyMerged.CredentialID, projSecret.ID)
	}
	organizationSecret, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID: orgA, OwnerProjectID: &projA1, Name: "projA1-organization-secret", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte("sk-projA1-organization"),
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedConn, err := h.repo.UpdateProviderConnection(h.ctx, catalog.UpdateProviderConnectionParams{
		OrgID: orgA, ConnectionID: validConn.ID, ExpectedVersion: validConn.Version, MergeBindings: true,
		SecretBindings: []catalog.SecretBindingInput{{SlotName: "organization", CredentialID: organizationSecret.ID}},
	})
	if err != nil {
		t.Fatalf("merge a single secret slot: %v", err)
	}
	if len(updatedConn.SecretBindings) != 2 {
		t.Fatalf("partial slot rotation dropped an existing binding: got %#v", updatedConn.SecretBindings)
	}
	validConn = updatedConn
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgA, validConn.ID, catalog.ConnStateValidating, validConn.Version, "test"); err != nil {
		t.Fatal(err)
	}
	// ResolveSlot defense-in-depth: if DB row is tampered to share downward, ResolveSlot rejects it.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET shared_project_ids_json = ? WHERE id = ?`, `["`+projA2.ID+`"]`, validConn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgA, validConn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveSlot defense-in-depth rejection on project secret sharing, got %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET shared_project_ids_json = '[]' WHERE id = ?`, validConn.ID); err != nil {
		t.Fatal(err)
	}

	// 3. org_extension_policies enabled=0 blocks ResolveConnectionPolicy & ResolveSlot.
	now := h.clk.Now().UnixMilli()
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO org_extension_policies (org_id, package_id, enabled, version, created_at_ms, updated_at_ms)
		VALUES (?, (SELECT package_id FROM extension_installations WHERE id = ?), 0, 1, ?, ?)`,
		orgA, installID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgA, validConn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ErrConnectionNotReady when org_extension_policies.enabled=0, got %v", err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgA, validConn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ErrConnectionNotReady on ResolveSlot when org_extension_policies.enabled=0, got %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `DELETE FROM org_extension_policies WHERE org_id = ?`, orgA); err != nil {
		t.Fatal(err)
	}

	// 4. Corrupt DB JSON fails closed in ResolveConnectionPolicy & Snapshot Refresh, then restore.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET allowed_cidrs_json = '{bad' WHERE id = ?`, validConn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgA, validConn.ID); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected fail-closed ErrInvalidInput on corrupt allowed_cidrs_json, got %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET allowed_cidrs_json = '[]' WHERE id = ?`, validConn.ID); err != nil {
		t.Fatal(err)
	}

	// 5. GetLogicalModel & DeleteLogicalModel enforce tenant isolation.
	orgAModel, err := h.repo.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		OrgID:           &orgA,
		CanonicalSlug:   "org-a-private",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   8192,
		MaxOutputTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	globModel, err := h.repo.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		CanonicalSlug:   "global-shared",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   8192,
		MaxOutputTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.GetLogicalModel(h.ctx, &orgB, orgAModel.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cross-org GetLogicalModel ErrNotFound, got %v", err)
	}
	if err := h.repo.DeleteLogicalModel(h.ctx, &orgB, orgAModel.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected cross-org DeleteLogicalModel ErrNotFound, got %v", err)
	}
	if err := h.repo.DeleteLogicalModel(h.ctx, &orgA, globModel.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected org caller DeleteLogicalModel on global model ErrNotFound, got %v", err)
	}

	// 6. ValidateConnection reverts VALIDATING -> DRAFT cleanly even if caller context is canceled mid-probe.
	cancelRt, err := declarative.CompileSpec(declarative.DefaultOpenAICompatibleSpec("cancel", []string{"127.0.0.1"}))
	if err != nil {
		t.Fatal(err)
	}
	adpCancel, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "cancel-adp",
		AllowedHosts:   []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	llm := fakellm.NewServer(t)
	defer llm.Close()
	connCancel, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgA,
		AdapterID:       adpCancel.ID,
		Name:            "Cancel Test",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeSelfHostedInfra,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Cancel ctx right during probe execution using a wrapped cancelable runtime spy.
	probeCtx, probeCancel := context.WithCancel(h.ctx)
	_, revertedConn, valErr := h.repo.ValidateConnection(probeCtx, orgA, connCancel.ID, &cancelingRuntimeSpy{inner: cancelRt, onBuildProbe: probeCancel}, h.broker)
	if valErr == nil {
		t.Fatal("expected ValidateConnection error when canceled")
	}
	if revertedConn == nil || revertedConn.State != catalog.ConnStateDraft {
		t.Fatalf("expected VALIDATING rollback to DRAFT under canceled caller ctx, got %+v", revertedConn)
	}

	// 7. Cancel ctx right during VerifyValidationResult (before final TransitionConnectionState) also reverts cleanly.
	probeCtx2, probeCancel2 := context.WithCancel(h.ctx)
	_, revertedConn2, valErr2 := h.repo.ValidateConnection(probeCtx2, orgA, connCancel.ID, &cancelingRuntimeSpy{inner: cancelRt, onVerifyResult: probeCancel2}, h.broker)
	if valErr2 == nil {
		t.Fatal("expected ValidateConnection error when canceled before final transition")
	}
	if revertedConn2 == nil || revertedConn2.State != catalog.ConnStateDraft {
		t.Fatalf("expected final transition cancel rollback to DRAFT, got %+v", revertedConn2)
	}

	// 8. ConfigJSON "null" and "[1]" rejected eagerly + no nil-map panic if DB row contains 'null'.
	for _, badCfg := range []string{"null", "[1]", `{"a":1}{"b":2}`} {
		if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
			OrgID:           orgA,
			AdapterID:       adpCancel.ID,
			Name:            "BadCfg " + badCfg,
			EndpointBaseURL: llm.URL(),
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "us",
			BillingMode:     catalog.BillingModeSelfHostedInfra,
			AuthMode:        catalog.AuthModeNone,
			ConfigJSON:      badCfg,
		}); !errors.Is(err, port.ErrInvalidInput) {
			t.Fatalf("expected ErrInvalidInput for ConfigJSON=%q, got %v", badCfg, err)
		}
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET config_json = 'null' WHERE id = ?`, connCancel.ID); err != nil {
		t.Fatal(err)
	}
	_, nilCfgConn, nilCfgErr := h.repo.ValidateConnection(h.ctx, orgA, connCancel.ID, cancelRt, h.broker)
	if !errors.Is(nilCfgErr, port.ErrInvalidInput) || nilCfgConn == nil || nilCfgConn.State != catalog.ConnStateDraft {
		t.Fatalf("expected non-panicking ErrInvalidInput & DRAFT revert for config_json='null', got err=%v conn=%+v", nilCfgErr, nilCfgConn)
	}
}

type cancelingRuntimeSpy struct {
	inner          extport.ProviderRuntime
	onBuildProbe   func()
	onVerifyResult func()
}

func (s *cancelingRuntimeSpy) BuildValidationProbe(ctx context.Context, in extport.ValidationProbeInput) (extport.UpstreamHttpSpec, error) {
	spec, err := s.inner.BuildValidationProbe(ctx, in)
	if s.onBuildProbe != nil {
		s.onBuildProbe()
	}
	return spec, err
}
func (s *cancelingRuntimeSpy) VerifyValidationResult(ctx context.Context, in extport.ValidationResultInput) (extport.ValidationVerdict, error) {
	verdict, err := s.inner.VerifyValidationResult(ctx, in)
	if s.onVerifyResult != nil {
		s.onVerifyResult()
	}
	return verdict, err
}
func (s *cancelingRuntimeSpy) TransformRequest(ctx context.Context, in extport.TransformRequestInput) (extport.UpstreamHttpSpec, error) {
	return s.inner.TransformRequest(ctx, in)
}
func (s *cancelingRuntimeSpy) TransformResponse(ctx context.Context, in extport.TransformResponseInput) (extport.NormalizedInferenceResponse, error) {
	return s.inner.TransformResponse(ctx, in)
}
func (s *cancelingRuntimeSpy) TransformStreamChunk(ctx context.Context, in extport.StreamChunkInput) (extport.StreamChunkOutput, error) {
	return s.inner.TransformStreamChunk(ctx, in)
}
func (s *cancelingRuntimeSpy) NormalizeError(ctx context.Context, in extport.NormalizeErrorInput) (extport.CanonicalProviderError, error) {
	return s.inner.NormalizeError(ctx, in)
}
func (s *cancelingRuntimeSpy) ExtractUsage(ctx context.Context, in extport.ExtractUsageInput) (extport.NormalizedUsage, error) {
	return s.inner.ExtractUsage(ctx, in)
}
func (s *cancelingRuntimeSpy) InspectNativeRequest(ctx context.Context, in extport.NativeRequestInput) (extport.NativeRoutingHints, error) {
	return s.inner.InspectNativeRequest(ctx, in)
}

type faultySnapshotRefresher struct {
	inner     *snapshot.Manager
	failOn    atomic.Int32
	calls     atomic.Int32
	onRefresh func(callNum int32)
}

func (f *faultySnapshotRefresher) Refresh(ctx context.Context) error {
	callNum := f.calls.Add(1)
	if f.onRefresh != nil {
		f.onRefresh(callNum)
	}
	target := f.failOn.Load()
	if target < 0 || (target > 0 && callNum == target) {
		return errors.New("injected snapshot refresh failure")
	}
	return f.inner.Refresh(ctx)
}

func TestSM01ApprovedPermissionIntersectionAndFailClosed(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, _, approverID := h.createOrgProjectApprover(t, "sm01-perms")

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-sm01-perm"
  name: Strict Permission Intersection Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.approved.example:8443", "alt.approved.example"]
    allowPrivateIp: false
    requireTls: true
adapter:
  slug: strict-perm
  baseUrlTemplate: "https://api.approved.example:8443/v1"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer_api_key"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-sm01-perm", approverID, manifestYAML)

	// 1. CreateProviderAdapter fails closed on empty allowed_hosts, unapproved host, and port widening.
	if _, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "empty-hosts",
		AllowedHosts:   nil,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on empty AllowedHosts, got %v", err)
	}
	if _, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "unapproved-host",
		AllowedHosts:   []string{"evil.example"},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on unapproved host, got %v", err)
	}
	if _, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "port-widen",
		AllowedHosts:   []string{"api.approved.example"}, // approved pinned :8443
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput when adapter widens approved port, got %v", err)
	}

	// Narrow adapter to ONLY api.approved.example:8443 (omitting alt.approved.example approved on extension).
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "strict-perm",
		AllowedHosts:   []string{"api.approved.example:8443"},
	})
	if err != nil {
		t.Fatalf("valid narrowed CreateProviderAdapter failed: %v", err)
	}

	sec, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:      orgID,
		Name:       "strict-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte("sk-strict"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Host intersection at CreateProviderConnection: alt.approved.example approved by extension but NOT adapter.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "Adapter Narrow Denied",
		EndpointBaseURL: "https://alt.approved.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on host outside adapter intersection, got %v", err)
	}

	// 3. TLS intersection: requireTls=true rejects http:// even for approved host.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "HTTP Denied RequireTLS",
		EndpointBaseURL: "http://api.approved.example:8443/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on http:// when requireTls=true, got %v", err)
	}

	// 4. allowPrivateIp intersection: extension allowPrivateIp=false rejects AllowPrivateIP=true connection.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "PrivateIP Denied",
		EndpointBaseURL: "https://api.approved.example:8443/v1",
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"10.0.0.0/8"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput on AllowPrivateIP=true when approved allowPrivateIp=false, got %v", err)
	}

	// 5. Secret slot & inject_mode intersection: unapproved slot or escalated HOST_CRYPTO_SIGN rejected.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "Unapproved Slot",
		EndpointBaseURL: "https://api.approved.example:8443/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "unapproved_slot", CredentialID: sec.ID, InjectMode: catalog.InjectModeHostBlind},
		},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for unapproved slot, got %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "Escalated Inject Mode",
		EndpointBaseURL: "https://api.approved.example:8443/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAWSSigV4,
		SecretBindings: []catalog.SecretBindingInput{
			{SlotName: "api_key", CredentialID: sec.ID, InjectMode: catalog.InjectModeHostCryptoSign},
		},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for unapproved HOST_CRYPTO_SIGN mode, got %v", err)
	}

	// Valid connection matching exact intersection succeeds and transitions to VALIDATING for resolver tests.
	validConn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "Strict Approved Conn",
		EndpointBaseURL: "https://api.approved.example:8443/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatalf("CreateProviderConnection valid failed: %v", err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, validConn.ID, catalog.ConnStateValidating, validConn.Version, "test"); err != nil {
		t.Fatal(err)
	}

	// Verify BOTH resolvers initially succeed.
	if pol, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, validConn.ID); err != nil || !pol.RequireTLS || pol.AllowPrivateIP {
		t.Fatalf("expected valid ResolveConnectionPolicy RequireTLS=true AllowPrivateIP=false, got pol=%+v err=%v", pol, err)
	}
	if slot, err := h.repo.ResolveSlot(h.ctx, orgID, validConn.ID, "api_key"); err != nil || slot == nil || !slot.RequireTLS || slot.InjectMode != broker.InjectModeHostBlind {
		t.Fatalf("expected slot with TLS and host blind injection, got slot=%+v err=%v", slot, err)
	}

	// 6. Both ResolveConnectionPolicy and ResolveSlot fail closed when approved_permissions_json narrows, corrupts, or drops active_version_id.
	var origApproved string
	var origActiveVer string
	if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT approved_permissions_json, active_version_id FROM extension_installations WHERE id = ?`, installID).Scan(&origApproved, &origActiveVer); err != nil {
		t.Fatal(err)
	}

	// Remove "api_key" from approved_permissions_json -> ResolveSlot fails closed while host-only policy still holds.
	noSlotPerms := `{"allowed_hosts":["api.approved.example:8443"],"allow_private_ip":false,"require_tls":true,"execution_tier":"DECLARATIVE_T1","secret_slots":[]}`
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET approved_permissions_json = ? WHERE id = ?`, noSlotPerms, installID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, validConn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveSlot ErrConnectionNotReady when slot dropped from SM-01 approval, got %v", err)
	}

	// Narrow host in approved_permissions_json -> BOTH ResolveConnectionPolicy and ResolveSlot fail closed.
	narrowHostPerms := `{"allowed_hosts":["other.example"],"allow_private_ip":false,"require_tls":true,"execution_tier":"DECLARATIVE_T1","secret_slots":["api_key"]}`
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET approved_permissions_json = ? WHERE id = ?`, narrowHostPerms, installID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, validConn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveConnectionPolicy fail closed when host narrowed in SM-01 approval, got %v", err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, validConn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveSlot fail closed when host narrowed in SM-01 approval, got %v", err)
	}

	// Corrupt/empty approved_permissions_json -> BOTH resolvers AND CreateProviderAdapter fail closed.
	for _, badJSON := range []string{`{}`, `null`, `{corrupt`} {
		if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET approved_permissions_json = ? WHERE id = ?`, badJSON, installID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
			InstallationID: installID,
			AdapterSlug:    "corrupt-perm-" + badJSON,
			AllowedHosts:   []string{"api.approved.example:8443"},
		}); !errors.Is(err, port.ErrInvalidInput) {
			t.Fatalf("CreateProviderAdapter(%s) expected ErrInvalidInput, got %v", badJSON, err)
		}
		if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, validConn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
			t.Fatalf("ResolveConnectionPolicy(%s) expected ErrConnectionNotReady, got %v", badJSON, err)
		}
		if _, err := h.repo.ResolveSlot(h.ctx, orgID, validConn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
			t.Fatalf("ResolveSlot(%s) expected ErrConnectionNotReady, got %v", badJSON, err)
		}
	}

	// Restore valid permissions then clear active_version_id -> BOTH adapters and resolvers fail closed.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET approved_permissions_json = ?, active_version_id = NULL WHERE id = ?`, origApproved, installID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "no-active-ver",
		AllowedHosts:   []string{"api.approved.example:8443"},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("CreateProviderAdapter without active_version_id expected ErrInvalidInput, got %v", err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, validConn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("ResolveConnectionPolicy without active_version_id expected ErrConnectionNotReady, got %v", err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, validConn.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("ResolveSlot without active_version_id expected ErrConnectionNotReady, got %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET active_version_id = ? WHERE id = ?`, origActiveVer, installID); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterMayApproveExactEndpointWithinSignedAzureResourceSuffix(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, projectID, approverID := h.createOrgProjectApprover(t, "azure-exact-host")
	manifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/azure-endpoint-test"
  name: "Azure Endpoint Host Test"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "azure-openai"
    supportedDialects: ["azure_openai", "openai_chat"]
permissions:
  networkEgress:
    allowedHosts: ["tanpopo-resource.openai.azure.com"]
    allowPrivateIp: false
    requireTls: true
adapter:
  slug: "azure-openai"
  endpointHostSuffixes: [".openai.azure.com"]
`
	installationID := h.installDeclarativePackage(t, "@tanpopo/azure-endpoint-test", approverID, manifest)
	adapter, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installationID, AdapterSlug: "azure-openai",
		SupportedDialects: []string{"azure_openai", "openai_chat"},
		AllowedHosts:      []string{"customer-resource.openai.azure.com"},
	})
	if err != nil {
		t.Fatalf("approve exact organization Azure host: %v", err)
	}
	if adapter.AllowedHosts[0] != "customer-resource.openai.azure.com" {
		t.Fatalf("adapter host approval broadened or changed: %v", adapter.AllowedHosts)
	}
	connection, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID: orgID, OwnerProjectID: &projectID, AdapterID: adapter.ID,
		Name: "customer-azure-resource", EndpointBaseURL: "https://customer-resource.openai.azure.com",
		DataRegion: "us", BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeNone,
	})
	if err != nil || connection.EndpointBaseURL != "https://customer-resource.openai.azure.com" {
		t.Fatalf("exact suffix-scoped Azure provider connection rejected: conn=%+v err=%v", connection, err)
	}
	connection, err = h.repo.TransitionConnectionState(h.ctx, orgID, connection.ID, catalog.ConnStateValidating, connection.Version, "validate exact resource host")
	if err != nil {
		t.Fatalf("transition exact-host connection to validating: %v", err)
	}
	connection, err = h.repo.TransitionConnectionState(h.ctx, orgID, connection.ID, catalog.ConnStateActive, connection.Version, "activate exact resource host")
	if err != nil {
		t.Fatalf("activate exact-host connection: %v", err)
	}
	policy, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, connection.ID)
	if err != nil || len(policy.AllowedHosts) != 1 || policy.AllowedHosts[0] != "customer-resource.openai.azure.com" || len(policy.AllowedPorts) != 1 || policy.AllowedPorts[0] != 443 || !policy.RequireTLS {
		t.Fatalf("connection policy must stay exact-host TLS-only: policy=%+v err=%v", policy, err)
	}
	if _, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installationID, AdapterSlug: "azure-openai",
		SupportedDialects: []string{"azure_openai", "openai_chat"},
		AllowedHosts:      []string{"customer.openai.azure.com.attacker.test"},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("lookalike Azure host should not be approved: %v", err)
	}
}

func TestTPBR0202OrgMinPluginTrustAndDisablePolicy(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	nowMs := func() int64 { return h.clk.Now().UnixMilli() }

	rootPub, rootPriv, _ := ed25519.GenerateKey(rand.Reader)
	pubPub, pubPriv, _ := ed25519.GenerateKey(rand.Reader)
	entPub, entPriv, _ := ed25519.GenerateKey(rand.Reader)

	keyring := installer.TrustedKeyring{
		RootPublicKeys:       map[string]ed25519.PublicKey{"root": rootPub},
		PublisherPublicKeys:  map[string]ed25519.PublicKey{"pub": pubPub},
		EnterprisePublicKeys: map[string]ed25519.PublicKey{"ent": entPub},
	}
	repoConfig := catalog.Config{DB: h.store.RawDB(), Keyring: keyring, Snapshots: h.snapshots, Audit: noopAuditSink{}, NowMs: nowMs}
	repo, err := catalog.NewRepository(repoConfig)
	if err != nil {
		t.Fatal(err)
	}
	h.repo = repo

	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           h.store.RawDB(),
		Audit:        noopAuditSink{},
		PluginsDir:   filepath.Join(t.TempDir(), "plugins"),
		LockfilePath: filepath.Join(t.TempDir(), "tanpopo.lock"),
		Keyring:      keyring,
		NowMs:        nowMs,
	})
	if err != nil {
		t.Fatal(err)
	}

	orgID, _, approverID := h.createOrgProjectApprover(t, "br0202")
	installWithTrust := func(pkgID string, sigMode string) (installID string, pkgRowID string) {
		yamlBody := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: %q
  name: Trust Adapter %s
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.trust.example"]
    allowPrivateIp: false
    requireTls: true
adapter:
  slug: "trust-adp"
  baseUrlTemplate: "https://api.trust.example/v1"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "k"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`, pkgID, sigMode)
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(yamlBody))})
		_, _ = tw.Write([]byte(yamlBody))
		_ = tw.Close()
		_ = gw.Close()
		bundleBytes := buf.Bytes()
		sum := sha256.Sum256(bundleBytes)
		hexSum := hex.EncodeToString(sum[:])

		payload := installer.SignatureSigningPayload(pkgID, "1.0.0", hexSum)
		var sig installer.ProvenanceSignature
		switch sigMode {
		case "official":
			sig.PublisherOrRootSig = ed25519.Sign(rootPriv, payload)
		case "verified":
			sig.PublisherOrRootSig = ed25519.Sign(pubPriv, payload)
		case "enterprise_signed":
			sig.EnterpriseSig = ed25519.Sign(entPriv, payload)
		case "unverified":
		}

		staged, err := instSvc.StageFreshInstall(h.ctx, installer.StageInstallRequest{
			ArchiveReader: bytes.NewReader(bundleBytes),
			Source: installer.SourceSpec{
				Kind:           "oci",
				URI:            "oci://registry.tanpopo.dev/ext/" + sigMode + "@sha256:" + hexSum,
				ExpectedSHA256: hexSum,
			},
			Signature: sig,
			ActorID:   approverID,
		})
		if err != nil {
			t.Fatalf("StageFreshInstall(%s) failed: %v", sigMode, err)
		}
		app, err := instSvc.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
			InstallationID:      staged.Installation.InstallationID,
			ExpectedVersion:     staged.Installation.Version,
			ApproverPrincipalID: approverID,
			ActivateImmediately: true,
		})
		if err != nil {
			t.Fatalf("ApproveAndActivate(%s) failed: %v", sigMode, err)
		}
		return app.InstallationID, app.PackageID
	}

	insOfficial, pkgOfficial := installWithTrust("@tanpopo/trust-official", "official")
	insVerified, _ := installWithTrust("@thirdparty/trust-verified", "verified")
	insEnt, _ := installWithTrust("@thirdparty/trust-ent", "enterprise_signed")
	insUnverified, _ := installWithTrust("@thirdparty/trust-unverified", "unverified")

	mkAdapter := func(insID, slug string) *catalog.ProviderAdapter {
		a, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
			InstallationID: insID,
			AdapterSlug:    slug,
			AllowedHosts:   []string{"api.trust.example"},
		})
		if err != nil {
			t.Fatalf("CreateProviderAdapter(%s) failed: %v", slug, err)
		}
		return a
	}
	adpOfficial := mkAdapter(insOfficial, "official")
	adpVerified := mkAdapter(insVerified, "verified")
	adpEnt := mkAdapter(insEnt, "ent")
	adpUnverified := mkAdapter(insUnverified, "unverified")

	sec, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:      orgID,
		Name:       "trust-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte("sk-trust"),
	})
	if err != nil {
		t.Fatal(err)
	}

	setMinTrust := func(tier string) {
		if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE organizations SET min_plugin_trust = ? WHERE id = ?`, tier, orgID); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Tighten org to OFFICIAL_ONLY -> verified, enterprise-signed, and unverified blocked on BOTH NONE and API_KEY creation.
	setMinTrust(port.MinTrustOfficialOnly)
	for _, tc := range []struct {
		name string
		adp  string
		mode string
		cred *string
	}{
		{"verified_none", adpVerified.ID, catalog.AuthModeNone, nil},
		{"verified_apikey", adpVerified.ID, catalog.AuthModeAPIKey, &sec.ID},
		{"ent_none", adpEnt.ID, catalog.AuthModeNone, nil},
		{"unverified_none", adpUnverified.ID, catalog.AuthModeNone, nil},
	} {
		if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
			OrgID:           orgID,
			AdapterID:       tc.adp,
			CredentialID:    tc.cred,
			Name:            tc.name,
			EndpointBaseURL: "https://api.trust.example/v1",
			DataRegion:      "us",
			BillingMode:     catalog.BillingModeCommercialAPI,
			AuthMode:        tc.mode,
		}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
			t.Fatalf("%s under OFFICIAL_ONLY expected ErrOrgTrustPolicyViolation, got %v", tc.name, err)
		}
	}

	// 2. Under VERIFIED_ONLY -> enterprise-signed unverified blocked; verified allowed.
	setMinTrust(port.MinTrustVerifiedOnly)
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adpEnt.ID,
		Name:            "ent_under_verified_only",
		EndpointBaseURL: "https://api.trust.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected enterprise-signed unverified blocked under VERIFIED_ONLY, got %v", err)
	}

	// 3. Under VERIFIED_OR_ENTERPRISE_SIGNED -> enterprise-signed succeeds for NONE and API_KEY; unsigned fails.
	setMinTrust(port.MinTrustVerifiedOrEnterpriseSigned)
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adpUnverified.ID,
		Name:            "unsigned_under_ent_tier",
		EndpointBaseURL: "https://api.trust.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected unverified blocked under VERIFIED_OR_ENTERPRISE_SIGNED, got %v", err)
	}
	connEntNone, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adpEnt.ID,
		Name:            "ent_none_ok",
		EndpointBaseURL: "https://api.trust.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatalf("ent NONE connection under VERIFIED_OR_ENTERPRISE_SIGNED failed: %v", err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, connEntNone.ID, catalog.ConnStateValidating, connEntNone.Version, "test"); err != nil {
		t.Fatal(err)
	}

	// Verify ResolveConnectionPolicy works now on connEntNone, then tighten org to VERIFIED_ONLY -> resolver fails closed!
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, connEntNone.ID); err != nil {
		t.Fatalf("expected ResolveConnectionPolicy OK under VERIFIED_OR_ENTERPRISE_SIGNED, got %v", err)
	}
	setMinTrust(port.MinTrustVerifiedOnly)
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, connEntNone.ID); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected zero-slot ResolveConnectionPolicy rejection after tightening org to VERIFIED_ONLY, got %v", err)
	}

	// Also verify credentialed connection resolves under VERIFIED_ONLY and fails after tightening to OFFICIAL_ONLY.
	connVerifiedKey, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adpVerified.ID,
		CredentialID:    &sec.ID,
		Name:            "verified_key_ok",
		EndpointBaseURL: "https://api.trust.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, connVerifiedKey.ID, catalog.ConnStateValidating, connVerifiedKey.Version, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, connVerifiedKey.ID, "api_key"); err != nil {
		t.Fatalf("expected ResolveSlot OK under VERIFIED_ONLY, got %v", err)
	}
	rotatedConfig := repoConfig
	rotatedConfig.Keyring.PublisherPublicKeys = nil
	rotatedRepo, err := catalog.NewRepository(rotatedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotatedRepo.ResolveSlot(h.ctx, orgID, connVerifiedKey.ID, "api_key"); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected public trust to reflect the current keyring, got %v", err)
	}
	if _, err := rotatedRepo.ResolveConnectionPolicy(h.ctx, orgID, connVerifiedKey.ID); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected connection policy to reflect the current keyring, got %v", err)
	}

	setMinTrust(port.MinTrustOfficialOnly)
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, connVerifiedKey.ID, "api_key"); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ResolveSlot ErrOrgTrustPolicyViolation under OFFICIAL_ONLY, got %v", err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, connVerifiedKey.ID); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ResolveConnectionPolicy ErrOrgTrustPolicyViolation under OFFICIAL_ONLY, got %v", err)
	}

	// 4. Preserve org disable decision (org_extension_policies.enabled=0) even for official package under ANY_APPROVED!
	setMinTrust(port.MinTrustAnyApproved)
	connOfficial, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adpOfficial.ID,
		CredentialID:    &sec.ID,
		Name:            "official_disable_check",
		EndpointBaseURL: "https://api.trust.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, connOfficial.ID, catalog.ConnStateValidating, connOfficial.Version, "test"); err != nil {
		t.Fatal(err)
	}
	if err := instSvc.SetOrgExtensionPolicy(h.ctx, orgID, pkgOfficial, false, nil, approverID); err != nil {
		t.Fatalf("SetOrgExtensionPolicy disable failed: %v", err)
	}
	// Blocked BOTH at egress resolvers AND at CreateProviderConnection!
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, connOfficial.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveConnectionPolicy blocked when org policy disabled, got %v", err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, connOfficial.ID, "api_key"); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveSlot blocked when org policy disabled, got %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adpOfficial.ID,
		Name:            "blocked_by_org_disable",
		EndpointBaseURL: "https://api.trust.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected CreateProviderConnection blocked when org policy disabled, got %v", err)
	}
}

func TestValidateConnectionPostCommitSnapshotFailureFaultInjection(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, _, approverID := h.createOrgProjectApprover(t, "val-snap-fi")

	llm := fakellm.NewServer(t)
	defer llm.Close()
	u, err := url.Parse(llm.URL())
	if err != nil {
		t.Fatal(err)
	}

	manifestYAML := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-snap-fi"
  name: Snapshot Fault Injection Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["%s"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: snap-fi
  baseUrl: %q
  supportedDialects: ["openai_chat"]
  endpoints:
    models: "/v1/models"
    chat: "/v1/chat/completions"
`, u.Hostname(), llm.URL())

	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-snap-fi", approverID, manifestYAML)
	rt, err := declarative.ParseDeclarativeManifestYAML([]byte(manifestYAML))
	if err != nil {
		t.Fatal(err)
	}

	faultySync := &faultySnapshotRefresher{inner: h.snapshots}
	repoFI, err := catalog.NewRepository(catalog.Config{
		DB:                h.store.RawDB(),
		NowMs:             func() int64 { return h.clk.Now().UnixMilli() },
		Snapshots:         h.snapshots,
		SnapshotRefresher: faultySync,
		Audit:             noopAuditSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := repoFI.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "snap-fi",
		AllowedHosts:   []string{u.Hostname()},
	})
	if err != nil {
		t.Fatal(err)
	}

	readDBState := func(connID string) (string, int64) {
		var st string
		var ver int64
		if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT state, version FROM provider_connections WHERE org_id = ? AND id = ?`, orgID, connID).Scan(&st, &ver); err != nil {
			t.Fatalf("query DB state failed: %v", err)
		}
		return st, ver
	}

	// Case 1: Snapshot refresh fails AFTER committing entry into VALIDATING -> rolls back to DRAFT and allows clean retry.
	conn1, err := repoFI.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "FI Enter Validating",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeSelfHostedInfra,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	faultySync.calls.Store(0)
	faultySync.failOn.Store(-1) // fail both the VALIDATING sync and revert sync
	_, resConn1, valErr1 := repoFI.ValidateConnection(h.ctx, orgID, conn1.ID, rt, h.broker)
	if !errors.Is(valErr1, catalog.ErrSnapshotSyncFailed) {
		t.Fatalf("expected ErrSnapshotSyncFailed after VALIDATING commit, got %v", valErr1)
	}
	if resConn1 == nil || resConn1.State != catalog.ConnStateDraft {
		t.Fatalf("expected rolled-back DRAFT connection returned, got %+v", resConn1)
	}
	dbState1, _ := readDBState(conn1.ID)
	if dbState1 != catalog.ConnStateDraft {
		t.Fatalf("expected actual DB state reverted to DRAFT (not stuck VALIDATING), got %q", dbState1)
	}
	// Retry succeeds once snapshot sync recovers.
	faultySync.failOn.Store(0)
	vRetry, resRetry, errRetry := repoFI.ValidateConnection(h.ctx, orgID, conn1.ID, rt, h.broker)
	if errRetry != nil || vRetry == nil || !vRetry.Valid || resRetry.State != catalog.ConnStateActive {
		t.Fatalf("expected retry ValidateConnection to reach ACTIVE cleanly, got err=%v verdict=%+v conn=%+v", errRetry, vRetry, resRetry)
	}

	// Case 2: Concurrent REVOKED right after VALIDATING commit when snapshot sync fails -> respects REVOKED (optimistic lock).
	conn2, err := repoFI.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "FI Concurrent Revoke",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeSelfHostedInfra,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	faultySync.calls.Store(0)
	faultySync.failOn.Store(1)
	faultySync.onRefresh = func(callNum int32) {
		if callNum == 1 {
			// Concurrent admin revokes while snapshot sync runs on VALIDATING commit (version=2).
			_, _ = h.repo.TransitionConnectionState(h.ctx, orgID, conn2.ID, catalog.ConnStateRevoked, 2, "concurrent_revoke")
		}
	}
	_, resConn2, valErr2 := repoFI.ValidateConnection(h.ctx, orgID, conn2.ID, rt, h.broker)
	faultySync.onRefresh = nil
	if !errors.Is(valErr2, port.ErrVersionConflict) || !errors.Is(valErr2, catalog.ErrSnapshotSyncFailed) {
		t.Fatalf("expected ErrSnapshotSyncFailed + ErrVersionConflict on concurrent revoke, got %v", valErr2)
	}
	if resConn2 == nil || resConn2.State != catalog.ConnStateRevoked {
		t.Fatalf("expected returned connection to reflect concurrent REVOKED, got %+v", resConn2)
	}
	if dbState2, _ := readDBState(conn2.ID); dbState2 != catalog.ConnStateRevoked {
		t.Fatalf("expected actual DB state to stay REVOKED, got %q", dbState2)
	}

	// Case 3: Snapshot refresh fails AFTER final ACTIVE transition commits -> DB stays ACTIVE, no stale-version rollback, snapshot retry converges!
	conn3, err := repoFI.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "FI Final Active",
		EndpointBaseURL: llm.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeSelfHostedInfra,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	faultySync.calls.Store(0)
	faultySync.failOn.Store(2) // Call 1 = VALIDATING ok, Call 2 = post-ACTIVE commit fails!
	verdict3, resConn3, valErr3 := repoFI.ValidateConnection(h.ctx, orgID, conn3.ID, rt, h.broker)
	if !errors.Is(valErr3, catalog.ErrSnapshotSyncFailed) || errors.Is(valErr3, port.ErrVersionConflict) {
		t.Fatalf("expected pure ErrSnapshotSyncFailed (no spurious ErrVersionConflict), got %v", valErr3)
	}
	if verdict3 == nil || !verdict3.Valid || resConn3 == nil || resConn3.State != catalog.ConnStateActive {
		t.Fatalf("expected valid verdict & committed ACTIVE conn returned, got verdict=%+v conn=%+v", verdict3, resConn3)
	}
	if dbState3, _ := readDBState(conn3.ID); dbState3 != catalog.ConnStateActive {
		t.Fatalf("expected actual DB state ACTIVE after final commit, got %q", dbState3)
	}
	if resConn3.Version != conn3.Version+2 {
		t.Fatalf("unexpected committed connection version %d", resConn3.Version)
	}
	if before, ok := h.snapshots.Current().LookupProviderConnection(orgID, conn3.ID); !ok ||
		before.State != catalog.ConnStateValidating || before.Version != conn3.Version+1 {
		t.Fatalf("expected snapshot to still reflect VALIDATING, got %+v", before)
	}
	faultySync.failOn.Store(0)
	if err := repoFI.SyncSnapshots(h.ctx); err != nil {
		t.Fatalf("SyncSnapshots retry failed: %v", err)
	}
	if after, ok := h.snapshots.Current().LookupProviderConnection(orgID, conn3.ID); !ok ||
		after.State != catalog.ConnStateActive || after.Version != resConn3.Version {
		t.Fatalf("expected snapshot to converge to committed ACTIVE state, got %+v", after)
	}

	// Case 4: Snapshot refresh fails AFTER final DRAFT rollback (probe returns HTTP 401) -> DB is DRAFT & retry succeeds once upstream recovers.
	unauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer unauthSrv.Close()
	uu, err := url.Parse(unauthSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	unauthManifest := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-snap-401"
  name: Snapshot 401 Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["%s"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: snap-401
  baseUrl: %q
`, uu.Hostname(), unauthSrv.URL)
	ins401 := h.installDeclarativePackage(t, "@tanpopo/adapter-snap-401", approverID, unauthManifest)
	rt401, err := declarative.ParseDeclarativeManifestYAML([]byte(unauthManifest))
	if err != nil {
		t.Fatal(err)
	}
	adp401, err := repoFI.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: ins401,
		AdapterSlug:    "snap-401",
		AllowedHosts:   []string{uu.Hostname()},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn4, err := repoFI.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp401.ID,
		Name:            "FI Final Draft On 401",
		EndpointBaseURL: unauthSrv.URL,
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeSelfHostedInfra,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	faultySync.calls.Store(0)
	faultySync.failOn.Store(2) // Call 1 VALIDATING ok; Call 2 final DRAFT rollback fails snapshot sync
	verdict4, resConn4, valErr4 := repoFI.ValidateConnection(h.ctx, orgID, conn4.ID, rt401, h.broker)
	if !errors.Is(valErr4, catalog.ErrSnapshotSyncFailed) {
		t.Fatalf("expected ErrSnapshotSyncFailed on post-DRAFT snapshot failure, got %v", valErr4)
	}
	if verdict4 == nil || verdict4.Valid || resConn4 == nil || resConn4.State != catalog.ConnStateDraft {
		t.Fatalf("expected invalid verdict + DRAFT conn returned, got verdict=%+v conn=%+v", verdict4, resConn4)
	}
	if dbState4, _ := readDBState(conn4.ID); dbState4 != catalog.ConnStateDraft {
		t.Fatalf("expected actual DB state DRAFT, got %q", dbState4)
	}
	// Retry ValidateConnection cleanly with recovered snapshot & valid runtime/endpoint fails verdict -> stays DRAFT cleanly without error.
	faultySync.failOn.Store(0)
	verdict4Retry, res4Retry, err4Retry := repoFI.ValidateConnection(h.ctx, orgID, conn4.ID, rt401, h.broker)
	if err4Retry != nil || verdict4Retry == nil || verdict4Retry.Valid || res4Retry.State != catalog.ConnStateDraft {
		t.Fatalf("expected clean retry on DRAFT conn returning invalid verdict without error, got err=%v verdict=%+v conn=%+v", err4Retry, verdict4Retry, res4Retry)
	}

}

func (h *testHarness) createOrgOwnerRole(t *testing.T, orgID, orgSlug string) string {
	t.Helper()
	ownerID := "prc_orgowner_" + orgSlug
	now := h.clk.Now().UnixMilli()
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', ?, 'ACTIVE', 1, ?, ?)`,
		ownerID, orgID, "owner-"+orgSlug+"@example.com", now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, ?, ?, 'ORG', ?, 'ORG_OWNER', 1, ?)`,
		"rb_orgowner_"+orgSlug, orgID, ownerID, orgID, now,
	); err != nil {
		t.Fatal(err)
	}
	return ownerID
}

func TestValidateConnectionProbeNeverReportsActiveWhenRevokedMidProbe(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, _, approverID := h.createOrgProjectApprover(t, "midprobe-rev")
	orgOwnerID := h.createOrgOwnerRole(t, orgID, "midprobe-rev")

	var onProbe atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fn, ok := onProbe.Load().(func()); ok && fn != nil {
			fn()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)

	setupAdapterAndConn := func(pkgID, slug string) (string, string, string, *catalog.ProviderConnection, extport.ProviderRuntime) {
		manifestYAML := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: %q
  name: Midprobe Revoke Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["%s"]
    allowPrivateIp: true
    requireTls: false
adapter:
  slug: %q
  baseUrl: %q
  auth:
    - id: "k"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`, pkgID, u.Hostname(), slug, upstream.URL)
		insID := h.installDeclarativePackage(t, pkgID, approverID, manifestYAML)
		rec, err := h.installer.GetInstallation(h.ctx, insID)
		if err != nil {
			t.Fatal(err)
		}
		rt, err := declarative.ParseDeclarativeManifestYAML([]byte(manifestYAML))
		if err != nil {
			t.Fatal(err)
		}
		adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
			InstallationID: insID,
			AdapterSlug:    slug,
			AllowedHosts:   []string{u.Hostname()},
		})
		if err != nil {
			t.Fatal(err)
		}
		sec, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
			OrgID:         orgID,
			Name:          "key-" + slug,
			SecretKind:    vault.SecretKindAPIKey,
			BoundAudience: &[]string{u.Hostname()}[0],
			Plaintext:     []byte("sk-probe-" + slug),
		})
		if err != nil {
			t.Fatal(err)
		}
		conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
			OrgID:           orgID,
			AdapterID:       adp.ID,
			CredentialID:    &sec.ID,
			Name:            "Conn " + slug,
			EndpointBaseURL: upstream.URL,
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "us",
			BillingMode:     catalog.BillingModeSelfHostedInfra,
			AuthMode:        catalog.AuthModeAPIKey,
		})
		if err != nil {
			t.Fatal(err)
		}
		return insID, rec.PackageID, sec.ID, conn, rt
	}

	// A. Quarantining extension installation AFTER upstream returns 200 OK -> ValidateConnection MUST NOT report Valid=true or commit ACTIVE.
	insA, _, _, connA, rtA := setupAdapterAndConn("@tanpopo/midprobe-q", "midq")
	onProbe.Store(func() {
		_ = h.installer.QuarantineInstallation(h.ctx, insA, "mid_probe_cve", approverID)
	})
	verdictA, gotA, errA := h.repo.ValidateConnection(h.ctx, orgID, connA.ID, rtA, h.broker)
	if errA == nil || verdictA == nil || verdictA.Valid || gotA == nil || gotA.State == catalog.ConnStateActive {
		t.Fatalf("expected non-ACTIVE & invalid verdict when extension quarantined mid-probe, got err=%v verdict=%+v conn=%+v", errA, verdictA, gotA)
	}
	var dbStateA string
	_ = h.store.RawDB().QueryRowContext(h.ctx, `SELECT state FROM provider_connections WHERE id = ?`, connA.ID).Scan(&dbStateA)
	if dbStateA == catalog.ConnStateActive {
		t.Fatalf("security bypass: DB connection transitioned to ACTIVE despite quarantined extension")
	}

	// B. Disabling org extension policy AFTER upstream 200 OK -> ValidateConnection MUST NOT report Valid=true or commit ACTIVE.
	_, pkgB, _, connB, rtB := setupAdapterAndConn("@tanpopo/midprobe-orgdis", "midorg")
	onProbe.Store(func() {
		_ = h.installer.SetOrgExtensionPolicy(h.ctx, orgID, pkgB, false, nil, orgOwnerID)
	})
	verdictB, gotB, errB := h.repo.ValidateConnection(h.ctx, orgID, connB.ID, rtB, h.broker)
	if errB == nil || verdictB == nil || verdictB.Valid || gotB == nil || gotB.State == catalog.ConnStateActive {
		t.Fatalf("expected non-ACTIVE & invalid verdict when org disables package mid-probe, got err=%v verdict=%+v conn=%+v", errB, verdictB, gotB)
	}

	// C. Shredding/revoking bound secret mid-probe -> ValidateConnection MUST NOT report Valid=true or commit ACTIVE.
	_, _, _, connC, rtC := setupAdapterAndConn("@tanpopo/midprobe-sec", "midsec")
	onProbe.Store(func() {
		_, _ = h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET credential_id = NULL WHERE id = ?`, connC.ID)
		_, _ = h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET approved_permissions_json = '{"allowed_hosts":["127.0.0.1"],"allow_private_ip":true,"execution_tier":"DECLARATIVE_T1"}' WHERE id = (SELECT installation_id FROM provider_adapters WHERE id = ?)`, connC.AdapterID)
	})
	verdictC, gotC, errC := h.repo.ValidateConnection(h.ctx, orgID, connC.ID, rtC, h.broker)
	if errC == nil || verdictC == nil || verdictC.Valid || gotC == nil || gotC.State == catalog.ConnStateActive {
		t.Fatalf("expected non-ACTIVE & invalid verdict when slot permissions revoked mid-probe, got err=%v verdict=%+v conn=%+v", errC, verdictC, gotC)
	}
}

func TestTPBR0202ChildProjectUnsignedRequiresOrgOwnerOptIn(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, projID, approverID := h.createOrgProjectApprover(t, "tpbr0202-child")
	orgOwnerID := h.createOrgOwnerRole(t, orgID, "tpbr0202-child")

	// Second principal who is ONLY SECURITY_REVIEWER (not ORG_OWNER).
	secRevID := "prc_secrev_tpbr0202"
	now := h.clk.Now().UnixMilli()
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'secrev-0202@example.com', 'ACTIVE', 1, ?, ?)`,
		secRevID, orgID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_secrev_0202', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, ?)`,
		orgID, secRevID, orgID, now,
	); err != nil {
		t.Fatal(err)
	}

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@thirdparty/unsigned-child-proj"
  name: Unsigned Child Project Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.unsigned.example"]
adapter:
  slug: unsigned-child
  baseUrl: "https://api.unsigned.example/v1"
`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(manifestYAML))})
	_, _ = tw.Write([]byte(manifestYAML))
	_ = tw.Close()
	_ = gw.Close()
	bundleBytes := buf.Bytes()
	sum := sha256.Sum256(bundleBytes)
	hexSum := hex.EncodeToString(sum[:])

	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(bundleBytes),
		Source:        installer.SourceSpec{Kind: "oci", URI: "oci://registry.tanpopo.dev/thirdparty/u@sha256:" + hexSum, ExpectedSHA256: hexSum},
		ActorID:       approverID,
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: approverID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: app.InstallationID,
		AdapterSlug:    "unsigned-child",
		AllowedHosts:   []string{"api.unsigned.example"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Default newly created org (min_plugin_trust=ANY_APPROVED, no org_extension_policies opt-in):
	//    Child Project connection MUST be DENIED; org-wide connection succeeds.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		Name:            "Blocked Child Project Conn",
		EndpointBaseURL: "https://api.unsigned.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected child project blocked for unsigned unverified_external without ORG_OWNER opt-in, got %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:            orgID,
		AdapterID:        adp.ID,
		Name:             "Blocked Downward Shared Conn",
		EndpointBaseURL:  "https://api.unsigned.example/v1",
		DataRegion:       "us",
		BillingMode:      catalog.BillingModeCommercialAPI,
		AuthMode:         catalog.AuthModeNone,
		SharedProjectIDs: []string{projID},
	}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected shared project scope blocked for unsigned unverified_external without ORG_OWNER opt-in, got %v", err)
	}
	orgConn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "Org-Wide Allowed Under ANY_APPROVED",
		EndpointBaseURL: "https://api.unsigned.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatalf("expected org-wide connection under ANY_APPROVED allowed, got %v", err)
	}
	_ = orgConn

	// 2. Non-ORG_OWNER (SECURITY_REVIEWER) cannot grant the opt-in.
	if perr := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, secRevID); !errors.Is(perr, installer.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER opt-in, got %v", perr)
	}

	// 3. Explicit ORG_OWNER opt-in unlocks child Project creation & ResolveConnectionPolicy!
	if err := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, orgOwnerID); err != nil {
		t.Fatalf("SetOrgExtensionPolicy by ORG_OWNER failed: %v", err)
	}
	childConn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		Name:            "Enabled Child Project Conn",
		EndpointBaseURL: "https://api.unsigned.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatalf("expected child project allowed after ORG_OWNER opt-in, got %v", err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, childConn.ID, catalog.ConnStateValidating, childConn.Version, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, childConn.ID); err != nil {
		t.Fatalf("expected ResolveConnectionPolicy OK after ORG_OWNER opt-in, got %v", err)
	}

	// 4. Disabling or removing policy re-blocks child Project resolution immediately.
	if err := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, false, nil, orgOwnerID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveConnectionPolicy(h.ctx, orgID, childConn.ID); !errors.Is(err, catalog.ErrConnectionNotReady) {
		t.Fatalf("expected ResolveConnectionPolicy blocked after ORG_OWNER disable, got %v", err)
	}
}

func TestLegacyInstallationMissingSlotReconsentEndToEndWithCatalog(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, projID, approverID := h.createOrgProjectApprover(t, "legacy-cat")

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/legacy-cat-adapter"
  name: Legacy Catalog Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.legacy.example"]
    requireTls: true
adapter:
  slug: legacy-cat
  baseUrl: "https://api.legacy.example/v1"
  auth:
    - id: "k"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	insID := h.installDeclarativePackage(t, "@tanpopo/legacy-cat-adapter", approverID, manifestYAML)
	// Simulate legacy approved_permissions_json missing secret_slots.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE extension_installations
		SET approved_permissions_json = '{"allowed_hosts":["api.legacy.example"],"require_tls":true,"execution_tier":"DECLARATIVE_T1"}'
		WHERE id = ?`, insID,
	); err != nil {
		t.Fatal(err)
	}
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: insID,
		AdapterSlug:    "legacy-cat",
		AllowedHosts:   []string{"api.legacy.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sec, err := h.vault.SealSecret(h.ctx, vault.SealSecretParams{
		OrgID:          orgID,
		OwnerProjectID: &projID,
		Name:           "legacy-key",
		SecretKind:     vault.SecretKindAPIKey,
		Plaintext:      []byte("sk-legacy"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Before re-consent -> CreateProviderConnection MUST fail closed on unapproved api_key slot.
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "Blocked Before Reconsent",
		EndpointBaseURL: "https://api.legacy.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected unapproved legacy slot rejected prior to re-consent, got %v", err)
	}

	// Request re-consent -> while UPDATING_PENDING_RECONSENT, STILL rejected (no auto-grant!).
	cur, err := h.installer.GetInstallation(h.ctx, insID)
	if err != nil {
		t.Fatal(err)
	}
	pending, _, err := h.installer.RequestActivePermissionReconsent(h.ctx, insID, cur.Version, approverID, orgID, "req_cat_rc")
	if err != nil {
		t.Fatalf("RequestActivePermissionReconsent failed: %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "Blocked While Pending Reconsent",
		EndpointBaseURL: "https://api.legacy.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected slot rejected while UPDATING_PENDING_RECONSENT, got %v", err)
	}

	// Explicit ApproveEscalatedUpgrade unlocks slot for CreateProviderConnection + ResolveSlot!
	if _, err := h.installer.ApproveEscalatedUpgrade(h.ctx, insID, pending.Version, approverID, orgID, "req_cat_app"); err != nil {
		t.Fatalf("ApproveEscalatedUpgrade failed: %v", err)
	}
	conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		CredentialID:    &sec.ID,
		Name:            "Allowed After Reconsent",
		EndpointBaseURL: "https://api.legacy.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeAPIKey,
	})
	if err != nil {
		t.Fatalf("expected CreateProviderConnection OK after re-consent, got %v", err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, conn.ID, catalog.ConnStateValidating, conn.Version, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ResolveSlot(h.ctx, orgID, conn.ID, "api_key"); err != nil {
		t.Fatalf("expected ResolveSlot OK after re-consent, got %v", err)
	}
}

func TestTPBR0202TrustDowngradeBlocksChildProjectUntilOrgOwnerReoptsIn(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, projID, approverID := h.createOrgProjectApprover(t, "tpbr0202-downgrade")
	orgOwnerID := h.createOrgOwnerRole(t, orgID, "tpbr0202-downgrade")

	secRevID := "prc_secrev_downgrade"
	now := h.clk.Now().UnixMilli()
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'secrev-dg@example.com', 'ACTIVE', 1, ?, ?)`,
		secRevID, orgID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_secrev_dg', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, ?)`,
		orgID, secRevID, orgID, now,
	); err != nil {
		t.Fatal(err)
	}

	buildTGZ := func(ver string, extraHost bool) ([]byte, string) {
		hosts := `["api.dg.example"]`
		if extraHost {
			hosts = `["api.dg.example","api2.dg.example"]`
		}
		manifest := fmt.Sprintf(`
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@partner/downgrade-0202"
  name: Downgrade Adapter
  version: "%s"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: %s
adapter:
  slug: dg-0202
  baseUrl: "https://api.dg.example/v1"
`, ver, hosts)
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		_ = tw.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(manifest))})
		_, _ = tw.Write([]byte(manifest))
		_ = tw.Close()
		_ = gw.Close()
		raw := buf.Bytes()
		sum := sha256.Sum256(raw)
		return raw, hex.EncodeToString(sum[:])
	}

	// 1. Install official signed v1.0.0 and let non-ORG_OWNER SECURITY_REVIEWER enable org policy.
	b1, s1 := buildTGZ("1.0.0", false)
	st1, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(b1),
		Source:        installer.SourceSpec{Kind: "oci", URI: "oci://registry.tanpopo.dev/tanpopo/dg@sha256:" + s1, ExpectedSHA256: s1},
		Signature:     installer.ProvenanceSignature{PublisherOrRootSig: ed25519.Sign(h.privKey, installer.SignatureSigningPayload("@partner/downgrade-0202", "1.0.0", s1))},
		ActorID:       approverID,
	})
	if err != nil {
		t.Fatal(err)
	}
	act1, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      st1.Installation.InstallationID,
		ExpectedVersion:     st1.Installation.Version,
		ApproverPrincipalID: approverID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, act1.PackageID, true, nil, secRevID); err != nil {
		t.Fatalf("SECURITY_REVIEWER enablement of signed official package should pass: %v", err)
	}
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: act1.InstallationID,
		AdapterSlug:    "dg-0202",
		AllowedHosts:   []string{"api.dg.example"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Stage escalating upgrade to unsigned v1.1.0 (not @tanpopo/ official sig -> unverified_external):
	//    prior non-owner policy must be invalidated immediately (enabled=0), SECURITY_REVIEWER cannot re-enable during UPDATING_PENDING_RECONSENT,
	//    and child project CreateProviderConnection must fail until ORG_OWNER explicitly re-opts in!
	b2, s2 := buildTGZ("1.1.0", true)
	up2, err := h.installer.StageUpgrade(h.ctx, installer.StageUpgradeRequest{
		InstallationID:  act1.InstallationID,
		ExpectedVersion: act1.Version,
		ArchiveReader:   bytes.NewReader(b2),
		Source:          installer.SourceSpec{Kind: "oci", URI: "oci://registry.tanpopo.dev/tanpopo/dg@sha256:" + s2, ExpectedSHA256: s2},
		ActorID:         approverID,
	})
	if err != nil || !up2.Escalated {
		t.Fatalf("expected escalating StageUpgrade to unsigned v1.1.0, err=%v escalated=%v", err, up2 != nil && up2.Escalated)
	}
	if err := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, act1.PackageID, true, nil, secRevID); !errors.Is(err, installer.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER while unsigned candidate staged, got %v", err)
	}
	if _, err := h.installer.ApproveEscalatedUpgrade(h.ctx, up2.Installation.InstallationID, up2.Installation.Version, approverID, "", "req_dg_app"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		Name:            "Must Be Blocked After Trust Downgrade",
		EndpointBaseURL: "https://api.dg.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected child project blocked after trust downgrade (reset enabled=0) until ORG_OWNER re-opts in, got %v", err)
	}

	// 3. Explicit ORG_OWNER opt-in unlocks child Project creation again.
	if err := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, act1.PackageID, true, nil, orgOwnerID); err != nil {
		t.Fatalf("expected ORG_OWNER re-opt-in to succeed: %v", err)
	}
	if _, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		Name:            "Allowed After ORG_OWNER Re-Opt-In",
		EndpointBaseURL: "https://api.dg.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); err != nil {
		t.Fatalf("expected child project allowed after ORG_OWNER re-opt-in, got %v", err)
	}
}

func TestTPBR0202OrgConnectionEmptySharedProjectsSnapshotAndOrgFlow(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, projID, approverID := h.createOrgProjectApprover(t, "tpbr0202-orgempty")
	orgOwnerID := h.createOrgOwnerRole(t, orgID, "tpbr0202-orgempty")

	secRevID := "prc_secrev_orgempty"
	now := h.clk.Now().UnixMilli()
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'secrev-oe@example.com', 'ACTIVE', 1, ?, ?)`,
		secRevID, orgID, now, now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_secrev_oe', ?, ?, 'ORG', ?, 'SECURITY_REVIEWER', 1, ?)`,
		orgID, secRevID, orgID, now,
	); err != nil {
		t.Fatal(err)
	}

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@thirdparty/unsigned-org-empty"
  name: Unsigned Org Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1"]
    allowPrivateIp: true
adapter:
  slug: unsigned-orgempty
  baseUrl: "` + srv.URL + `"
`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(manifestYAML))})
	_, _ = tw.Write([]byte(manifestYAML))
	_ = tw.Close()
	_ = gw.Close()
	bundleBytes := buf.Bytes()
	sum := sha256.Sum256(bundleBytes)
	hexSum := hex.EncodeToString(sum[:])

	staged, err := h.installer.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(bundleBytes),
		Source:        installer.SourceSpec{Kind: "oci", URI: "oci://registry.tanpopo.dev/thirdparty/uoe@sha256:" + hexSum, ExpectedSHA256: hexSum},
		ActorID:       approverID,
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := h.installer.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: approverID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: app.InstallationID,
		AdapterSlug:    "unsigned-orgempty",
		AllowedHosts:   []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Valid org-level connection (owner_project_id = nil, shared_project_ids = [] / empty) succeeds
	//    even without ORG_OWNER opt-in, transitions to ACTIVE, and executes org-level egress cleanly.
	orgConn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  nil,
		AdapterID:       adp.ID,
		Name:            "Org Level Unsigned Empty SharedProjects",
		EndpointBaseURL: srv.URL,
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatalf("expected valid org-level connection with empty shared_project_ids to succeed, got %v", err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, orgConn.ID, catalog.ConnStateValidating, orgConn.Version, "probe"); err != nil {
		t.Fatal(err)
	}
	orgConnV, err := h.repo.GetProviderConnection(h.ctx, orgID, orgConn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.TransitionConnectionState(h.ctx, orgID, orgConn.ID, catalog.ConnStateActive, orgConnV.Version, "probe-ok"); err != nil {
		t.Fatal(err)
	}
	resp, err := h.broker.Execute(h.ctx, broker.EgressRequest{
		OrgID:        orgID,
		ConnectionID: orgConn.ID,
		Method:       http.MethodGet,
		TargetURL:    srv.URL + "/v1/models",
	})
	if err != nil || resp == nil || resp.StatusCode != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("expected valid org-level egress to succeed cleanly, resp=%+v err=%v hits=%d", resp, err, hits.Load())
	}

	// 2. Snapshot visibility: org scope (projectID=="") is allowed, while child project (projID) MUST be blocked!
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	snapConn, ok := h.snapshots.Current().LookupProviderConnection(orgID, orgConn.ID)
	if !ok {
		t.Fatal("missing orgConn in snapshot")
	}
	if !snapConn.VisibleToProject(orgID, "") {
		t.Fatal("expected org-level snapshot visibility (projectID=='') to remain true")
	}
	if snapConn.VisibleToProject(orgID, projID) {
		t.Fatal("security bypass (TP-BR-0202): child project saw org-level unsigned unverified_external connection with empty shared_project_ids")
	}

	// 3. Non-ORG_OWNER cannot grant opt-in; explicit ORG_OWNER opt-in unlocks child project visibility after snapshot refresh.
	if perr := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, secRevID); !errors.Is(perr, installer.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER opt-in, got %v", perr)
	}
	if err := h.installer.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, orgOwnerID); err != nil {
		t.Fatalf("SetOrgExtensionPolicy by ORG_OWNER failed: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	snapConnAfter, ok := h.snapshots.Current().LookupProviderConnection(orgID, orgConn.ID)
	if !ok {
		t.Fatal("missing orgConn in snapshot after opt-in")
	}
	if !snapConnAfter.VisibleToProject(orgID, projID) {
		t.Fatal("expected child project visibility after explicit ORG_OWNER opt-in")
	}
}

func TestTPBR0202OrgConnectionEmptySharedProjectsKeyWithdrawalAndLegacySecurityReviewerPolicy(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	now := h.clk.Now().UnixMilli()

	org, err := h.store.Tenancy().CreateOrganization(h.ctx, port.CreateOrgParams{
		Slug:           "tpbr0202-kr-drift",
		DisplayName:    "TP-BR-0202 Keyring Drift Org",
		MinPluginTrust: port.MinTrustAnyApproved,
	})
	if err != nil {
		t.Fatal(err)
	}
	orgID := org.ID
	proj, err := h.store.Tenancy().CreateProject(h.ctx, orgID, port.CreateProjectParams{
		Slug:        "child-proj",
		DisplayName: "Child Project",
	})
	if err != nil {
		t.Fatal(err)
	}
	projID := proj.ID

	opID, orgOwnerID, secRevID := "prc_op_kr", "prc_owner_kr", "prc_sec_kr"
	for _, p := range []struct {
		id, email, role string
	}{
		{opID, "op-kr@example.com", "OPERATOR"},
		{orgOwnerID, "owner-kr@example.com", "ORG_OWNER"},
		{secRevID, "sec-kr@example.com", "SECURITY_REVIEWER"},
	} {
		if _, err := h.store.RawDB().ExecContext(h.ctx, `
			INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, 'HUMAN_LOCAL', ?, 'ACTIVE', 1, ?, ?)`,
			p.id, orgID, p.email, now, now,
		); err != nil {
			t.Fatal(err)
		}
		scopeType, scopeID, rbOrgID := "ORG", orgID, any(&orgID)
		if p.role == "OPERATOR" {
			scopeType, scopeID, rbOrgID = "INSTANCE", "instance", nil
		}
		if _, err := h.store.RawDB().ExecContext(h.ctx, `
			INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
			"rb_"+p.id, rbOrgID, p.id, scopeType, scopeID, p.role, now,
		); err != nil {
			t.Fatal(err)
		}
	}

	pubP, privP, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubFP := installer.PublicKeyFingerprint(pubP)
	activeKeyring := installer.TrustedKeyring{
		RootPublicKeys:      map[string]ed25519.PublicKey{h.keyID: h.pubKey},
		PublisherPublicKeys: map[string]ed25519.PublicKey{pubFP: pubP},
	}
	withdrawnKeyring := installer.TrustedKeyring{
		RootPublicKeys: map[string]ed25519.PublicKey{h.keyID: h.pubKey},
	}

	instActive, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           h.store.RawDB(),
		Audit:        noopAuditSink{},
		PluginsDir:   h.plugins,
		LockfilePath: filepath.Join(t.TempDir(), "tanpopo.lock"),
		Keyring:      activeKeyring,
		NowMs:        func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	instWithdrawn, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           h.store.RawDB(),
		Audit:        noopAuditSink{},
		PluginsDir:   h.plugins,
		LockfilePath: filepath.Join(t.TempDir(), "tanpopo-withdrawn.lock"),
		Keyring:      withdrawnKeyring,
		NowMs:        func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}

	repoActive, err := catalog.NewRepository(catalog.Config{
		DB:        h.store.RawDB(),
		NowMs:     func() int64 { return h.clk.Now().UnixMilli() },
		Snapshots: h.snapshots,
		Audit:     noopAuditSink{},
		Keyring:   activeKeyring,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapActive, err := snapshot.NewManager(h.ctx, snapshot.ManagerConfig{
		DB:      h.store.RawDB(),
		NowMs:   func() int64 { return h.clk.Now().UnixMilli() },
		Keyring: activeKeyring,
	})
	if err != nil {
		t.Fatal(err)
	}

	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@thirdparty/kr-drift-adapter"
  name: Keyring Drift Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.drift.example"]
adapter:
  slug: kr-drift-adapter
  baseUrl: "https://api.drift.example/v1"
`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(manifestYAML))})
	_, _ = tw.Write([]byte(manifestYAML))
	_ = tw.Close()
	_ = gw.Close()
	bundleBytes := buf.Bytes()
	sum := sha256.Sum256(bundleBytes)
	hexSum := hex.EncodeToString(sum[:])

	sig := ed25519.Sign(privP, installer.SignatureSigningPayload("@thirdparty/kr-drift-adapter", "1.0.0", hexSum))
	staged, err := instActive.StageFreshInstall(h.ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(bundleBytes),
		Source:        installer.SourceSpec{Kind: "oci", URI: "oci://registry.tanpopo.dev/thirdparty/kr@sha256:" + hexSum, ExpectedSHA256: hexSum},
		Signature:     installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:       opID,
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := instActive.ApproveAndActivate(h.ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: opID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if app.OriginTrustTier != "verified" {
		t.Fatalf("expected initial DB trust tier verified, got %q", app.OriginTrustTier)
	}

	// SECURITY_REVIEWER enables policy while package is still verified in activeKeyring (sets enabled=1, org_owner_opt_in=0).
	if err := instActive.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, secRevID); err != nil {
		t.Fatalf("SECURITY_REVIEWER enablement of verified package failed: %v", err)
	}

	adp, err := repoActive.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: app.InstallationID,
		AdapterSlug:    "kr-drift-adapter",
		AllowedHosts:   []string{"api.drift.example"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Org-level connection with owner_project_id = nil and empty shared_project_ids = [].
	orgConn, err := repoActive.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "Org Level Drift Conn",
		EndpointBaseURL: "https://api.drift.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoActive.TransitionConnectionState(h.ctx, orgID, orgConn.ID, catalog.ConnStateValidating, orgConn.Version, "test"); err != nil {
		t.Fatal(err)
	}

	// Also create a child-project connection while publisher key is still present.
	childConn, err := repoActive.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		Name:            "Child Project Conn",
		EndpointBaseURL: "https://api.drift.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoActive.TransitionConnectionState(h.ctx, orgID, childConn.ID, catalog.ConnStateValidating, childConn.Version, "test"); err != nil {
		t.Fatal(err)
	}

	if err := snapActive.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	orgViewActive, ok := snapActive.Current().LookupProviderConnection(orgID, orgConn.ID)
	if !ok || !orgViewActive.VisibleToProject(orgID, "") || !orgViewActive.VisibleToProject(orgID, projID) {
		t.Fatalf("expected active publisher keyring to expose orgConn to org and child project, ok=%v view=%+v", ok, orgViewActive)
	}

	// Now withdraw publisher key from keyring WITHOUT modifying DB extension_packages.origin_trust_tier or org_extension_policies!
	repoWithdrawn, err := catalog.NewRepository(catalog.Config{
		DB:        h.store.RawDB(),
		NowMs:     func() int64 { return h.clk.Now().UnixMilli() },
		Snapshots: h.snapshots,
		Audit:     noopAuditSink{},
		Keyring:   withdrawnKeyring,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapWithdrawn, err := snapshot.NewManager(h.ctx, snapshot.ManagerConfig{
		DB:      h.store.RawDB(),
		NowMs:   func() int64 { return h.clk.Now().UnixMilli() },
		Keyring: withdrawnKeyring,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Confirm stale DB rows still claim 'verified' and enabled=1.
	var dbTier string
	var dbEnabled, dbOwnerOptIn int
	if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT origin_trust_tier FROM extension_packages WHERE id = ?`, app.PackageID).Scan(&dbTier); err != nil {
		t.Fatal(err)
	}
	if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT enabled, org_owner_opt_in FROM org_extension_policies WHERE org_id = ? AND package_id = ?`, orgID, app.PackageID).Scan(&dbEnabled, &dbOwnerOptIn); err != nil {
		t.Fatal(err)
	}
	if dbTier != "verified" || dbEnabled != 1 || dbOwnerOptIn != 0 {
		t.Fatalf("unexpected setup DB state: tier=%q enabled=%d optIn=%d", dbTier, dbEnabled, dbOwnerOptIn)
	}

	// Snapshot & Catalog MUST agree after key withdrawal:
	// (a) Org-level flow still works in both snapshot and catalog under ANY_APPROVED.
	orgViewWithdrawn, ok := snapWithdrawn.Current().LookupProviderConnection(orgID, orgConn.ID)
	if !ok {
		t.Fatal("missing orgConn in withdrawn snapshot")
	}
	if !orgViewWithdrawn.VisibleToProject(orgID, "") {
		t.Fatal("expected org-level VisibleToProject(orgID, '') to remain true under ANY_APPROVED after key withdrawal")
	}
	if _, err := repoWithdrawn.ResolveConnectionPolicy(h.ctx, orgID, orgConn.ID); err != nil {
		t.Fatalf("expected org-level ResolveConnectionPolicy to keep working under ANY_APPROVED, got %v", err)
	}

	// (b) Child project scope MUST be blocked in both snapshot and catalog because SECURITY_REVIEWER enabled=1 is NOT ORG_OWNER opt-in!
	if orgViewWithdrawn.VisibleToProject(orgID, projID) {
		t.Fatal("security bypass: org-level empty shared_project_ids leaked to child project after key withdrawal via stale DB tier / SECURITY_REVIEWER policy")
	}
	if _, err := repoWithdrawn.ResolveConnectionPolicy(h.ctx, orgID, childConn.ID); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected catalog ResolveConnectionPolicy to deny childConn after key withdrawal, got %v", err)
	}
	if _, err := repoWithdrawn.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		OwnerProjectID:  &projID,
		AdapterID:       adp.ID,
		Name:            "Blocked Child After Withdrawal",
		EndpointBaseURL: "https://api.drift.example/v1",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected CreateProviderConnection for child project denied after key withdrawal, got %v", err)
	}
	if _, err := repoWithdrawn.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:            orgID,
		AdapterID:        adp.ID,
		Name:             "Blocked Downward Share After Withdrawal",
		EndpointBaseURL:  "https://api.drift.example/v1",
		DataRegion:       "us",
		BillingMode:      catalog.BillingModeCommercialAPI,
		AuthMode:         catalog.AuthModeNone,
		SharedProjectIDs: []string{projID},
	}); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected CreateProviderConnection with SharedProjectIDs denied after key withdrawal, got %v", err)
	}

	// (c) SECURITY_REVIEWER remains denied under withdrawn keyring; ORG_OWNER explicit opt-in unlocks BOTH snapshot and catalog.
	if perr := instWithdrawn.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, secRevID); !errors.Is(perr, installer.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied for SECURITY_REVIEWER after key withdrawal, got %v", perr)
	}
	if err := instWithdrawn.SetOrgExtensionPolicy(h.ctx, orgID, app.PackageID, true, nil, orgOwnerID); err != nil {
		t.Fatalf("expected ORG_OWNER opt-in after key withdrawal to succeed: %v", err)
	}
	if err := snapWithdrawn.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	orgViewAfterOwner, _ := snapWithdrawn.Current().LookupProviderConnection(orgID, orgConn.ID)
	if !orgViewAfterOwner.VisibleToProject(orgID, "") || !orgViewAfterOwner.VisibleToProject(orgID, projID) {
		t.Fatal("expected ORG_OWNER explicit opt-in to unlock child project visibility in snapshot")
	}
	if _, err := repoWithdrawn.ResolveConnectionPolicy(h.ctx, orgID, childConn.ID); err != nil {
		t.Fatalf("expected ORG_OWNER explicit opt-in to unlock childConn in catalog, got %v", err)
	}

	// (d) Enterprise key withdrawal: while DB keeps enterprise_signed=1, removing the enterprise key from
	// EnterprisePublicKeys blocks VERIFIED_OR_ENTERPRISE_SIGNED orgs in both snapshot and catalog.
	entPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entFP := installer.PublicKeyFingerprint(entPub)
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_versions SET signature_verified = 0, enterprise_signed = 1, signing_key_fingerprint = ?, enterprise_key_fingerprint = ? WHERE package_id = ?`, entFP, entFP, app.PackageID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE organizations SET min_plugin_trust = ?, version = version + 1, updated_at_ms = ? WHERE id = ?`, port.MinTrustVerifiedOrEnterpriseSigned, now+6, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'organization', ?, 'UPSERT', 0, ?)`, orgID, orgID, now+6); err != nil {
		t.Fatal(err)
	}
	entKeyring := installer.TrustedKeyring{EnterprisePublicKeys: map[string]ed25519.PublicKey{"ent": entPub}}
	snapWithdrawn.SetKeyring(entKeyring)
	if err := snapWithdrawn.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	repoEntActive, err := catalog.NewRepository(catalog.Config{
		DB:        h.store.RawDB(),
		NowMs:     func() int64 { return h.clk.Now().UnixMilli() },
		Snapshots: snapWithdrawn,
		Audit:     noopAuditSink{},
		Keyring:   entKeyring,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repoEntActive.ResolveConnectionPolicy(h.ctx, orgID, orgConn.ID); err != nil {
		t.Fatalf("expected active enterprise key to permit ResolveConnectionPolicy, got: %v", err)
	}
	vEntOK, _ := snapWithdrawn.Current().LookupProviderConnection(orgID, orgConn.ID)
	if !vEntOK.VisibleToProject(orgID, "") || !vEntOK.VisibleToProject(orgID, projID) {
		t.Fatal("expected snapshot VisibleToProject to allow active enterprise key")
	}

	snapWithdrawn.SetKeyring(withdrawnKeyring)
	if err := snapWithdrawn.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repoWithdrawn.ResolveConnectionPolicy(h.ctx, orgID, orgConn.ID); !errors.Is(err, installer.ErrOrgTrustPolicyDenied) {
		t.Fatalf("expected ResolveConnectionPolicy ErrOrgTrustPolicyDenied after enterprise key withdrawal, got: %v", err)
	}
	vEntAfter, _ := snapWithdrawn.Current().LookupProviderConnection(orgID, orgConn.ID)
	if vEntAfter.VisibleToProject(orgID, "") || vEntAfter.VisibleToProject(orgID, projID) {
		t.Fatal("expected snapshot VisibleToProject false after enterprise key withdrawal even with DB enterprise_signed=1")
	}
}

func TestCreateAndUpdateDeploymentRejectsNegativeMediaPricing(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	orgID, _, approverID := h.createOrgProjectApprover(t, "org-neg-media-price")
	manifestYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-neg-price"
  name: Negative Pricing Test Adapter
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.price.example"]
adapter:
  slug: price-test
  baseUrl: "https://api.price.example"
`
	installID := h.installDeclarativePackage(t, "@tanpopo/adapter-neg-price", approverID, manifestYAML)
	adp, err := h.repo.CreateProviderAdapter(h.ctx, catalog.CreateProviderAdapterParams{
		InstallationID: installID,
		AdapterSlug:    "price-test",
		AllowedHosts:   []string{"api.price.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := h.repo.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           orgID,
		AdapterID:       adp.ID,
		Name:            "PriceConn",
		EndpointBaseURL: "https://api.price.example",
		DataRegion:      "us",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	lm, err := h.repo.CreateLogicalModel(h.ctx, catalog.CreateLogicalModelParams{
		CanonicalSlug:   "neg-price-model",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   8192,
		MaxOutputTokens: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}

	negativeCases := []struct {
		name    string
		pricing snapshot.PricingNanoUSD
	}{
		{name: "image_unit", pricing: snapshot.PricingNanoUSD{ImagePerUnitNanoUSD: -1}},
		{name: "image_mpixels", pricing: snapshot.PricingNanoUSD{ImagePer1MPixelsNanoUSD: -1}},
		{name: "audio_in", pricing: snapshot.PricingNanoUSD{AudioInputPerSecNanoUSD: -1}},
		{name: "audio_out", pricing: snapshot.PricingNanoUSD{AudioOutputPerSecNanoUSD: -1}},
		{name: "doc", pricing: snapshot.PricingNanoUSD{DocumentPageNanoUSD: -1}},
		{name: "rerank", pricing: snapshot.PricingNanoUSD{RerankUnitNanoUSD: -1}},
	}
	for _, tc := range negativeCases {
		if _, err := h.repo.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
			OrgID:             orgID,
			ConnectionID:      conn.ID,
			LogicalModelID:    lm.ID,
			UpstreamModelName: "neg-" + tc.name,
			Pricing:           tc.pricing,
		}); !errors.Is(err, port.ErrInvalidInput) {
			t.Fatalf("CreateModelDeployment(%s): expected ErrInvalidInput, got %v", tc.name, err)
		}
	}

	validDepl, err := h.repo.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:             orgID,
		ConnectionID:      conn.ID,
		LogicalModelID:    lm.ID,
		UpstreamModelName: "valid-price",
		Pricing:           snapshot.PricingNanoUSD{InputPer1MNanoUSD: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range negativeCases {
		badPrice := tc.pricing
		if _, err := h.repo.UpdateModelDeployment(h.ctx, orgID, validDepl.ID, catalog.UpdateModelDeploymentParams{
			ExpectedVersion: validDepl.Version,
			Pricing:         &badPrice,
		}); !errors.Is(err, port.ErrInvalidInput) {
			t.Fatalf("UpdateModelDeployment(%s): expected ErrInvalidInput, got %v", tc.name, err)
		}
	}
}

type syncFailingCatalogAuditSink struct {
	fail atomic.Bool
}

func (s *syncFailingCatalogAuditSink) Append(context.Context, port.AuditEvent) error {
	if s.fail.Load() {
		return errors.New("injected catalog audit failure")
	}
	return nil
}

func (s *syncFailingCatalogAuditSink) AppendTx(context.Context, *sql.Tx, port.AuditEvent) error {
	if s.fail.Load() {
		return errors.New("injected catalog tx audit failure")
	}
	return nil
}

func TestNEG2TransactionalAuditRollbackCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "cat_neg2.db")
	clk := clock.NewFakeClock(time.UnixMilli(1760000500000))
	nowMs := func() int64 { return clk.Now().UnixMilli() }

	st, err := sqlitestore.Open(ctx, sqlitestore.Config{DBPath: dbPath, NowMs: nowMs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyID := installer.PublicKeyFingerprint(pub)
	keyring := installer.TrustedKeyring{RootPublicKeys: map[string]ed25519.PublicKey{keyID: pub}}

	snapMgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowMs, Keyring: keyring})
	if err != nil {
		t.Fatal(err)
	}
	sink := &syncFailingCatalogAuditSink{}
	repo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowMs,
		Snapshots: snapMgr,
		Audit:     sink,
		Keyring:   keyring,
	})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           st.RawDB(),
		Audit:        noopAuditSink{},
		PluginsDir:   filepath.Join(tmpDir, "plugins"),
		LockfilePath: filepath.Join(tmpDir, "tanpopo.lock"),
		Keyring:      keyring,
		NowMs:        nowMs,
	})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "neg2-cat", DisplayName: "NEG2 Cat"})
	if err != nil {
		t.Fatal(err)
	}
	approverID := "prc_neg2_op"
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms) VALUES (?, ?, 'HUMAN_LOCAL', 'neg2@example.com', 'ACTIVE', 1, 1000, 1000)`, approverID, org.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms) VALUES ('rb_neg2', NULL, ?, 'INSTANCE', 'instance', 'OPERATOR', 1, 1000)`, approverID); err != nil {
		t.Fatal(err)
	}

	manifestYAML := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-neg2-cat"
  name: Neg2 Test
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
permissions:
  networkEgress:
    allowedHosts: ["api.example.com"]
adapter:
  slug: example
  baseUrl: "https://api.example.com"
`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.yaml", Mode: 0o644, Size: int64(len(manifestYAML))})
	_, _ = tw.Write([]byte(manifestYAML))
	_ = tw.Close()
	_ = gw.Close()
	sum := sha256.Sum256(buf.Bytes())
	hexSum := hex.EncodeToString(sum[:])

	staged, err := inst.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchiveReader: bytes.NewReader(buf.Bytes()),
		Source:        installer.SourceSpec{Kind: "oci", URI: "oci://registry.tanpopo.dev/adapters/neg2@sha256:" + hexSum, ExpectedSHA256: hexSum},
		Signature:     installer.ProvenanceSignature{PublisherOrRootSig: ed25519.Sign(priv, installer.SignatureSigningPayload("@tanpopo/adapter-neg2-cat", "1.0.0", hexSum))},
		ActorID:       approverID,
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := inst.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: approverID,
		ActivateImmediately: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := repo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID: approved.InstallationID, AdapterSlug: "example", AllowedHosts: []string{"api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := repo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID: org.ID, AdapterID: adp.ID, Name: "Neg2 Conn", EndpointBaseURL: "https://api.example.com",
		DataRegion: "us", BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	valConn, err := repo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "probe")
	if err != nil {
		t.Fatal(err)
	}

	sink.fail.Store(true)
	if _, err := repo.TransitionConnectionState(ctx, org.ID, valConn.ID, catalog.ConnStateActive, valConn.Version, "activate"); err == nil {
		t.Fatalf("expected TransitionConnectionState to roll back when AppendTx fails")
	}
	got, err := repo.GetProviderConnection(ctx, org.ID, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != catalog.ConnStateValidating || got.Version != valConn.Version {
		t.Fatalf("expected rollback to VALIDATING v%d, got state=%s v%d", valConn.Version, got.State, got.Version)
	}
}

type nonTxCatAuditSink struct{}

func (nonTxCatAuditSink) Append(context.Context, port.AuditEvent) error { return nil }

func TestCatalogNewRepositoryRejectsMissingOrNonTxAuditSink(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)

	if _, err := catalog.NewRepository(catalog.Config{
		DB:        h.store.RawDB(),
		Snapshots: h.snapshots,
		Audit:     nil,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected NewRepository nil Audit to fail ErrInvalidInput, got %v", err)
	}
	if _, err := catalog.NewRepository(catalog.Config{
		DB:        h.store.RawDB(),
		Snapshots: h.snapshots,
		Audit:     nonTxCatAuditSink{},
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("expected NewRepository non-TxAuditSink to fail ErrInvalidInput, got %v", err)
	}
}
