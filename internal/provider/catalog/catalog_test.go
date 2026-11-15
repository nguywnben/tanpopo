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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

	snapMgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:    st.RawDB(),
		NowMs: nowMs,
	})
	if err != nil {
		t.Fatalf("snapshot.NewManager failed: %v", err)
	}

	repo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowMs,
		Snapshots: snapMgr,
		Audit:     noopAuditSink{},
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

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	pluginsDir := filepath.Join(tmpDir, "plugins")
	lockPath := filepath.Join(tmpDir, "tanpopo.lock")
	keyID := installer.PublicKeyFingerprint(pub)

	inst, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           st.RawDB(),
		Audit:        noopAuditSink{},
		PluginsDir:   pluginsDir,
		LockfilePath: lockPath,
		Keyring: installer.TrustedKeyring{
			RootPublicKeys: map[string]ed25519.PublicKey{keyID: pub},
		},
		NowMs: nowMs,
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
	if err := h.repo.DeleteProviderConnection(h.ctx, orgA, conn.ID); !errors.Is(err, port.ErrResourceInUse) {
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
	if err := h.repo.DeleteProviderConnection(h.ctx, orgA, conn.ID); err != nil {
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
permissions:
  networkEgress:
    allowedHosts: ["api.example.com", "127.0.0.1"]
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

