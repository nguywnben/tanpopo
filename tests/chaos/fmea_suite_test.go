// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package chaos_test executes automated reproduction and verification of all
// eight deep failure modes (FMEA-01..FMEA-08) and disaster-recovery drills
// (DR-01..DR-04, OPS-AC-06) under hermetic conditions (TP-IMP-0043).
package chaos_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/ingress/cache"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/firewall"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/oauth"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/migrate"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
	"github.com/nguywnben/tanpopo/internal/testkit/wasmfixtures"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type chaosAuditSink struct {
	mu     sync.Mutex
	events []storageport.AuditEvent
}

func (s *chaosAuditSink) Append(_ context.Context, ev storageport.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}

func (s *chaosAuditSink) AppendTx(_ context.Context, _ *sql.Tx, ev storageport.AuditEvent) error {
	return s.Append(context.Background(), ev)
}

func newChaosKEK(t *testing.T, id string) vault.KEKProvider {
	t.Helper()
	raw := make([]byte, vault.KeySizeBytes)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	kek, err := vault.NewStaticKEKProvider(id, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)
	return kek
}

func insertOperatorPrincipal(t *testing.T, ctx context.Context, db *sql.DB, nowMs int64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_chaos_op', NULL, 'HUMAN_LOCAL', 'chaos-op@tanpopo.test', 'ACTIVE', 1, ?, ?)
	`, nowMs, nowMs); err != nil {
		t.Fatalf("insert operator principal: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_chaos_op', NULL, 'prc_chaos_op', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowMs); err != nil {
		t.Fatalf("insert operator role_binding: %v", err)
	}
}

func seedChaosDeclarativeAdapter(
	t *testing.T,
	ctx context.Context,
	st *sqlite.Store,
	catRepo *catalog.Repository,
	instSvc *installer.LifecycleService,
	pubPriv ed25519.PrivateKey,
	orgID string,
	pluginsDir string,
) string {
	t.Helper()
	extSrcDir := filepath.Join(t.TempDir(), "acme-decl-src")
	if err := os.MkdirAll(extSrcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestYAML := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/acme-chaos-decl"
  name: "AcmeAI Chaos Declarative"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-chaos-decl"
    supportedDialects: ["openai_compatible"]
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:build_validation_probe"
    - "provider:transform_request"
    - "provider:transform_response"
    - "provider:transform_stream_chunk"
    - "provider:normalize_error"
adapter:
  slug: "acme-chaos-decl"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	if err := os.WriteFile(filepath.Join(extSrcDir, "manifest.yaml"), []byte(manifestYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	tpkgPath := filepath.Join(filepath.Dir(pluginsDir), "acme-chaos-decl-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(ctx, extSrcDir, tpkgPath)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/acme-chaos-decl", "1.0.0", packRes.SHA256))
	staged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath,
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            "./acme-chaos-decl-1.0.0.tpkg",
			ExpectedSHA256: packRes.SHA256,
		},
		Signature: installer.ProvenanceSignature{
			PublisherOrRootSig: sig,
		},
		ActorID:     "prc_chaos_op",
		RequestID:   "req_chaos_stage_01",
		TargetOrgID: orgID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: "prc_chaos_op",
		ActivateImmediately: true,
		OrgID:               orgID,
		RequestID:           "req_chaos_approve_01",
	})
	if err != nil {
		t.Fatal(err)
	}
	adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       "acme-chaos-decl",
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = st
	return adp.ID
}

// TestFMEA01MidStreamDisconnectProhibitsFailover verifies CR-05/FMEA-01:
// Pre-stream failures (k=0) fail over cleanly to backup, whereas mid-stream TCP RST
// after flushing k>=1 chunks prohibits failover (0 requests hit backup) and emits
// terminal SSE event: error (mid_stream_abort).
func TestFMEA01MidStreamDisconnectProhibitsFailover(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000200000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }

	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(tmp, "fmea01.db"), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub}}
	auditSink := &chaosAuditSink{}
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn, Keyring: keyring})
	if err != nil {
		t.Fatal(err)
	}
	vStore, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: newChaosKEK(t, "kek-fmea01"), NowMs: nowFn, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vStore.Close()
	catRepo, err := catalog.NewRepository(catalog.Config{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, Keyring: keyring, Audit: auditSink})
	if err != nil {
		t.Fatal(err)
	}
	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo,
		Vault:          vStore,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          auditSink,
		NowMs:          nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: pluginsDir,
		Keyring:    keyring,
		Audit:      auditSink,
		NowMs:      nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Snapshots: mgr, AuditSink: auditSink})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "fmea01-org", DisplayName: "FMEA01 Org", MinPluginTrust: storageport.MinTrustVerifiedOnly})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "fmea01-proj", DisplayName: "FMEA01 Proj"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	insertOperatorPrincipal(t, ctx, st.RawDB(), nowFn())

	adpID := seedChaosDeclarativeAdapter(t, ctx, st, catRepo, instSvc, pubPriv, org.ID, pluginsDir)
	secMeta, err := vStore.SealSecret(ctx, vault.SealSecretParams{OrgID: org.ID, Name: "k", SecretKind: vault.SecretKindAPIKey, Plaintext: []byte(canary.DefaultCanarySecret())})
	if err != nil {
		t.Fatal(err)
	}

	primaryLLM := fakellm.NewServer(t)
	backupLLM := fakellm.NewServer(t)

	mkConnAndDeploy := func(name, baseURL string, modelID string) string {
		conn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
			OrgID: org.ID, AdapterID: adpID, Name: name, EndpointBaseURL: baseURL,
			AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.1/32"}, DataRegion: "us",
			BillingMode: catalog.BillingModeCommercialAPI, AuthMode: catalog.AuthModeAPIKey,
			SecretBindings: []catalog.SecretBindingInput{{SlotName: "api_key", CredentialID: secMeta.ID, InjectMode: catalog.InjectModeHostBlind}},
		})
		if err != nil {
			t.Fatal(err)
		}
		conn, err = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "validate")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "activate"); err != nil {
			t.Fatal(err)
		}
		depl, err := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
			OrgID: org.ID, ConnectionID: conn.ID, LogicalModelID: modelID,
			UpstreamModelName: "acme-gpt-" + name, AssuranceLevel: catalog.AssuranceTested, State: catalog.DeployStateActiveHealthy,
			ConnectTimeoutMs: 5000, FirstByteTimeoutMs: 5000, IdleChunkTimeoutMs: 5000,
		})
		if err != nil {
			t.Fatal(err)
		}
		return depl.ID
	}

	lmod, err := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
		OrgID: &org.ID, CanonicalSlug: "acme/fmea01-chat", ModalityKind: catalog.ModalityChat,
		ContextWindow: 128000, MaxOutputTokens: 4096,
		BaseCapabilities: map[string]string{"streaming": "VERIFIED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	depPrimary := mkConnAndDeploy("primary", primaryLLM.URL(), lmod.ID)
	depBackup := mkConnAndDeploy("backup", backupLLM.URL(), lmod.ID)

	routeID := idgen.MustNew(idgen.PrefixRoute)
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO route_policies (
			id, org_id, project_id, env_id, route_slug, strategy, max_failover_hops,
			version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, 'fmea01-route', 'PRIORITY_FAILOVER', 2, 1, ?, ?)
	`, routeID, org.ID, proj.ID, env.ID, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight)
		VALUES (?, ?, ?, 1, 100), (?, ?, ?, 2, 100)
	`, org.ID, routeID, depPrimary, org.ID, routeID, depBackup); err != nil {
		t.Fatal(err)
	}

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "fmea01-vk", AllowedPlanes: "PROVIDER", MaxConcurrentStreams: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots: mgr, Broker: brk, PluginsDir: pluginsDir, Audit: auditSink, NowMs: nowFn, SSEHeartbeatInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close(context.Background()) }()
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	client := netguard.NewHTTPClient()

	// Subcase A: k=0 pre-stream 503 fails over cleanly to backup.
	primaryLLM.SetDefaultFault("http_status=503")
	backupLLM.ResetRequests()
	reqBody := []byte(`{"model":"fmea01-route","stream":true,"messages":[{"role":"user","content":"fmea01"}]}`)
	reqPre, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/chat/completions", bytes.NewReader(reqBody))
	reqPre.Header.Set("Authorization", "Bearer "+createdVK.SecretKey)
	reqPre.Header.Set("Content-Type", "application/json")
	respPre, err := client.Do(reqPre)
	if err != nil {
		t.Fatalf("pre-stream failover request: %v", err)
	}
	bodyPre, _ := io.ReadAll(respPre.Body)
	_ = respPre.Body.Close()
	if respPre.StatusCode != http.StatusOK || !strings.Contains(string(bodyPre), "[DONE]") || len(backupLLM.Requests()) != 1 {
		t.Fatalf("expected k=0 pre-stream failover to backup, status=%d backupHits=%d body=%s", respPre.StatusCode, len(backupLLM.Requests()), string(bodyPre))
	}

	// Subcase B: k>=1 post-first-chunk TCP RST MUST abort without touching backup.
	primaryLLM.SetDefaultFault("tcp_rst_after_chunks=2")
	backupLLM.ResetRequests()
	reqMid, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/chat/completions", bytes.NewReader(reqBody))
	reqMid.Header.Set("Authorization", "Bearer "+createdVK.SecretKey)
	reqMid.Header.Set("Content-Type", "application/json")
	respMid, err := client.Do(reqMid)
	if err != nil {
		t.Fatalf("mid-stream abort request: %v", err)
	}
	bodyMid, _ := io.ReadAll(respMid.Body)
	_ = respMid.Body.Close()
	if len(backupLLM.Requests()) != 0 {
		t.Fatalf("FMEA-01 violation: backup upstream hit %d times after mid-stream chunk flush", len(backupLLM.Requests()))
	}
	if !strings.Contains(string(bodyMid), "event: error") || !strings.Contains(string(bodyMid), "mid_stream_abort") {
		t.Fatalf("expected terminal SSE mid_stream_abort error event, got: %s", string(bodyMid))
	}
}

// TestFMEA02SideEffectRetryProhibitionAndSingleUseNonce verifies CR-08/FMEA-02:
// unclassified/mutating/destructive MCP tools permit 0 automatic retries on ambiguous failure,
// and destructive HITL approvals enforce single-use 5-tuple JCS argument binding.
func TestFMEA02SideEffectRetryProhibitionAndSingleUseNonce(t *testing.T) {
	if got := firewall.NormalizeClass(""); got != firewall.Mutating {
		t.Fatalf("unknown risk tier must fail closed to mutating, got %q", got)
	}
	if firewall.RetryAllowed(firewall.Mutating, true) || firewall.RetryAllowed(firewall.Destructive, true) {
		t.Fatalf("mutating and destructive tools must disallow retry on ambiguous failure")
	}
	if !firewall.RetryAllowed(firewall.ReadOnly, true) {
		t.Fatalf("read_only tool must allow retry on ambiguous failure")
	}

	ctx := context.Background()
	now := time.UnixMilli(1760000300000).UTC()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "fmea02.db"), NowMs: func() int64 { return now.UnixMilli() }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: func() int64 { return now.UnixMilli() }})
	if err != nil {
		t.Fatal(err)
	}
	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "fmea02-org", DisplayName: "FMEA02"})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "fmea02-proj", DisplayName: "FMEA02 Proj"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.RawDB().ExecContext(ctx, `INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms) VALUES ('prc_req', ?, 'HUMAN_LOCAL', 'req@acme.test', 'ACTIVE', ?, ?), ('prc_rev', ?, 'HUMAN_LOCAL', 'rev@acme.test', 'ACTIVE', ?, ?)`, org.ID, now.UnixMilli(), now.UnixMilli(), org.ID, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), AllowDetachedControlPlane: true, NowMs: func() int64 { return now.UnixMilli() }, AuditSink: auditLog})
	if err != nil {
		t.Fatal(err)
	}
	vk, err := vkSvc.Create(ctx, vkey.CreateParams{OrgID: org.ID, ProjectID: proj.ID, EnvID: env.ID, Name: "vk-fmea02", AllowedPlanes: "MCP"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := approval.NewStore(approval.Config{DB: st.RawDB(), Audit: auditLog, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	binding := approval.Binding{
		OrgID:              org.ID,
		ProjectID:          proj.ID,
		VirtualKeyID:       vk.Summary.ID,
		ServerSlug:         "prod-db",
		ToolName:           "drop_table",
		Arguments:          json.RawMessage(`{"table":"sessions"}`),
		ApprovedSchemaHash: strings.Repeat("a", 64),
	}
	created, err := store.Create(ctx, binding, "prc_req", now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Create approval: %v", err)
	}
	if _, err := store.Decide(ctx, created.ID, binding, 1, "prc_rev", approval.DecisionApprove, "verified change"); err != nil {
		t.Fatalf("Decide approval: %v", err)
	}

	tampered := binding
	tampered.Arguments = json.RawMessage(`{"table":"users"}`)
	if _, err := store.Consume(ctx, created.PreApprovalToken, tampered); err == nil {
		t.Fatalf("FMEA-02 violation: tampered arguments consumed approval nonce")
	}
	if _, err := store.Consume(ctx, created.PreApprovalToken, binding); err != nil {
		t.Fatalf("first valid Consume failed: %v", err)
	}
	if _, err := store.Consume(ctx, created.PreApprovalToken, binding); err == nil {
		t.Fatalf("FMEA-02 violation: approval nonce replayed twice")
	}
}

// TestFMEA03OptInResponseCacheIsolationAndCryptoShredding verifies CR-09/FMEA-03:
// response cache is default-disabled, bypasses temperature>0 and Cache-Control: no-store,
// partitions strictly across org/project/policy revisions, and becomes permanently unreadable
// upon Project DEK crypto-shredding.
func TestFMEA03OptInResponseCacheIsolationAndCryptoShredding(t *testing.T) {
	ctx := context.Background()
	nowMs := int64(1760000400000)
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "fmea03.db"), NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	vs, err := vault.NewStore(vault.Config{DB: st.RawDB(), KEK: newChaosKEK(t, "kek-fmea03"), NowMs: func() int64 { return nowMs }, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vs.Close()

	ec, err := cache.NewExactCache(cache.Config{DB: st.RawDB(), Vault: vs, NowMs: func() int64 { return nowMs }})
	if err != nil {
		t.Fatal(err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{Slug: "fmea03-org", DisplayName: "FMEA03"})
	if err != nil {
		t.Fatal(err)
	}
	projA, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "proj-a", DisplayName: "Proj A"})
	if err != nil {
		t.Fatal(err)
	}
	projB, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{Slug: "proj-b", DisplayName: "Proj B"})
	if err != nil {
		t.Fatal(err)
	}
	envA, err := st.Tenancy().CreateEnvironment(ctx, org.ID, projA.ID, storageport.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatal(err)
	}

	reqDet := []byte(`{"model":"acme/chat","temperature":0,"messages":[{"role":"user","content":"hello"}]}`)
	reqNonDet := []byte(`{"model":"acme/chat","temperature":0.7,"messages":[{"role":"user","content":"hello"}]}`)

	if el, err := ec.EvaluateEligibility(http.Header{}, policyengine.EffectivePolicy{}, cache.KeyInput{OrgID: org.ID, ProjectID: projA.ID, EnvID: envA.ID, ModelDeploymentID: "dep_1", RequestBody: reqDet}); err != nil || el.Eligible {
		t.Fatalf("expected default-disabled cache to bypass, got el=%+v err=%v", el, err)
	}
	enabledPol := policyengine.EffectivePolicy{ResponseCacheEnabled: true}
	if el, err := ec.EvaluateEligibility(http.Header{}, enabledPol, cache.KeyInput{OrgID: org.ID, ProjectID: projA.ID, EnvID: envA.ID, ModelDeploymentID: "dep_1", RequestBody: reqNonDet}); err != nil || el.Eligible {
		t.Fatalf("expected temperature>0 to bypass, got el=%+v err=%v", el, err)
	}
	noStoreHdr := http.Header{}
	noStoreHdr.Set("Cache-Control", "no-store")
	if el, err := ec.EvaluateEligibility(noStoreHdr, enabledPol, cache.KeyInput{OrgID: org.ID, ProjectID: projA.ID, EnvID: envA.ID, ModelDeploymentID: "dep_1", RequestBody: reqDet}); err != nil || el.Eligible {
		t.Fatalf("expected no-store header to bypass, got el=%+v err=%v", el, err)
	}

	elA, err := ec.EvaluateEligibility(http.Header{}, enabledPol, cache.KeyInput{OrgID: org.ID, ProjectID: projA.ID, EnvID: envA.ID, PolicyRevision: "rev1", ModelDeploymentID: "dep_1", RequestBody: reqDet})
	if err != nil || !elA.Eligible {
		t.Fatalf("expected deterministic eligible, got el=%+v err=%v", elA, err)
	}
	kB, _ := cache.ComputeCacheKeySHA256(cache.KeyInput{OrgID: org.ID, ProjectID: projB.ID, EnvID: envA.ID, PolicyRevision: "rev1", ModelDeploymentID: "dep_1", RequestBody: reqDet})
	if kB == elA.CacheKeySHA256 {
		t.Fatalf("expected distinct cache keys across projects")
	}

	payload := []byte(`{"id":"c1","choices":[{"message":{"role":"assistant","content":"secret-cached"}}]}`)
	if err := ec.Store(ctx, org.ID, projA.ID, envA.ID, "dep_1", elA.CacheKeySHA256, payload, elA.TTLSeconds); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if _, err := ec.Lookup(ctx, org.ID, projB.ID, envA.ID, elA.CacheKeySHA256); !errors.Is(err, cache.ErrCacheMiss) {
		t.Fatalf("cross-project cache lookup must miss, got %v", err)
	}
	got, err := ec.Lookup(ctx, org.ID, projA.ID, envA.ID, elA.CacheKeySHA256)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("same-project cache lookup mismatch: got=%q err=%v", string(got), err)
	}

	// Shred Project A DEK -> cache entry becomes permanently undecryptable (even before row GC).
	if err := vs.ShredProjectDEK(ctx, org.ID, projA.ID, vault.ShredRequest{RequestID: "req_shred_fmea03", ExpectedGeneration: 1}); err != nil {
		t.Fatalf("ShredProjectDEK: %v", err)
	}
	if _, err := ec.Lookup(ctx, org.ID, projA.ID, envA.ID, elA.CacheKeySHA256); err == nil {
		t.Fatalf("expected destroyed DEK lookup to fail after ShredProjectDEK")
	}
}

// TestFMEA04MissingOrMalformedUsageFallbackLadder verifies FMEA-04:
// Tanpopo never records EXACT=0 for non-empty payloads lacking upstream usage; falls back
// cleanly to ceil(text_utf8_bytes/3) ESTIMATED, or UNMETERED_FLAGGED when rate card is absent.
func TestFMEA04MissingOrMalformedUsageFallbackLadder(t *testing.T) {
	rate := pricing.RateCard{InputPer1MNanoUSD: 1_000_000, OutputPer1MNanoUSD: 2_000_000, BillingMode: pricing.BillingModeCommercialAPI}

	// Zero upstream tokens with non-empty text bytes must NEVER emit AccuracyExact = 0.
	res, err := pricing.Calculate(rate, pricing.UsageInput{
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     pricing.AccuracyExact,
		FallbackTextBytes:    30,
		FallbackOutputBytes:  15,
	})
	if err != nil {
		t.Fatalf("pricing.Calculate: %v", err)
	}
	if res.Accuracy != pricing.AccuracyEstimated || res.EffectiveUsage.InputTokens != 10 || res.EffectiveUsage.OutputTokens != 5 || res.TotalMicroUSD <= 0 {
		t.Fatalf("expected ESTIMATED fallback (>0 microUSD), got %+v", res)
	}

	// Missing rate card must emit UNMETERED_FLAGGED.
	unmetered, err := pricing.Calculate(pricing.RateCard{BillingMode: pricing.BillingModeCommercialAPI}, pricing.UsageInput{
		FallbackTextBytes: 12,
	})
	if err != nil {
		t.Fatalf("pricing.Calculate unpriced: %v", err)
	}
	if unmetered.Accuracy != pricing.AccuracyUnmeteredFlagged || !unmetered.UnmeteredFlagged {
		t.Fatalf("expected UNMETERED_FLAGGED when rate card is unconfigured, got %+v", unmetered)
	}
}

// TestFMEA05ExtensionSandboxTrapOOMAndAutoQuarantine verifies CR-05/FMEA-05:
// Tier-2 Wasm enforces <=15ms deadline on infinite loop, 16 MiB OOM boundary, trap recovery,
// and >20 faults / 60s auto-quarantine without host crash.
func TestFMEA05ExtensionSandboxTrapOOMAndAutoQuarantine(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000500000))
	quarantined := false
	fg := wasm.NewFaultGuard(wasm.FaultGuardConfig{
		Now:               clk.Now,
		MaxFaultsInWindow: 20,
		Window:            60 * time.Second,
		Quarantine: func(_ context.Context, _, _, _ string) error {
			quarantined = true
			return nil
		},
	})

	rtLoop, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.InfiniteLoopWASM(),
		InstallationID: "ins_chaos_loop",
		CallTimeout:    15 * time.Millisecond,
		FaultGuard:     fg,
	})
	if err != nil {
		t.Fatalf("NewRuntime(loop): %v", err)
	}
	defer func() { _ = rtLoop.Close(ctx) }()

	start := time.Now()
	_, loopErr := rtLoop.TransformRawSlice(ctx, "req_fmea05_loop", []byte(`{"ping":true}`))
	dur := time.Since(start)
	var fe *wasm.ExecutionFaultError
	if !errors.As(loopErr, &fe) || fe.Kind != wasm.FaultKindTimeout || dur > 500*time.Millisecond {
		t.Fatalf("expected fast FaultKindTimeout for infinite loop, got err=%v dur=%v", loopErr, dur)
	}

	rtOOM, err := wasm.NewRuntime(ctx, wasm.RuntimeConfig{
		WasmBytes:      wasmfixtures.OOM64MiBWASM(),
		InstallationID: "ins_chaos_oom",
		CallTimeout:    15 * time.Millisecond,
		FaultGuard:     fg,
	})
	if err != nil {
		t.Fatalf("NewRuntime(oom): %v", err)
	}
	defer func() { _ = rtOOM.Close(ctx) }()
	_, oomErr := rtOOM.TransformRawSlice(ctx, "req_fmea05_oom", []byte(`{"oom":true}`))
	if !errors.As(oomErr, &fe) || (fe.Kind != wasm.FaultKindOOM && fe.Kind != wasm.FaultKindTrap) {
		t.Fatalf("expected FaultKindOOM/Trap for 64 MiB growth attempt, got %v", oomErr)
	}

	// Exceed > 20 faults within 60s window -> auto-quarantine flips true cleanly.
	for i := 0; i < 25 && !fg.IsQuarantined("ins_chaos_loop"); i++ {
		fg.RecordFault(ctx, "ins_chaos_loop", wasm.FaultKindTrap, fmt.Sprintf("fault-%d", i))
	}
	if !fg.IsQuarantined("ins_chaos_loop") || !quarantined {
		t.Fatalf("expected ins_chaos_loop to auto-quarantine past 20 faults/60s")
	}
	if _, qErr := rtLoop.TransformRawSlice(ctx, "req_fmea05_q", []byte(`{}`)); !errors.As(qErr, &fe) || fe.Kind != wasm.FaultKindQuarantined {
		t.Fatalf("expected FaultKindQuarantined once quarantined, got %v", qErr)
	}
}

// TestFMEA06DirtyMigrationExit31 verifies FMEA-06:
// failed or checksum-tampered SQL migrations leave dirty state returning exit code 31 fail-closed.
func TestFMEA06DirtyMigrationExit31(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fmea06.db")
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	// Standalone fresh DB opens cleanly, while dirty SQL migration halts with exit 31.
	badDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "bad_mig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = badDB.Close() }()

	brokenFS := fstest.MapFS{
		"0001_ok.sql":     &fstest.MapFile{Data: []byte("CREATE TABLE t1 (id TEXT PRIMARY KEY);\n")},
		"0002_broken.sql": &fstest.MapFile{Data: []byte("THIS IS NOT VALID SQL;\n")},
	}
	_, applyErr := migrate.Apply(ctx, badDB, brokenFS, nil)
	if applyErr == nil || migrate.ExitCode(applyErr) != migrate.ExitCodeDirtyMigration {
		t.Fatalf("expected ExitCode 31 on failed migration, got code=%d err=%v", migrate.ExitCode(applyErr), applyErr)
	}

	// Re-opening after dirty state ALSO fails closed with exit 31.
	cleanFS := fstest.MapFS{
		"0001_ok.sql":     &fstest.MapFile{Data: []byte("CREATE TABLE t1 (id TEXT PRIMARY KEY);\n")},
		"0002_broken.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t2 (id TEXT PRIMARY KEY);\n")},
	}
	_, rerunErr := migrate.Apply(ctx, badDB, cleanFS, nil)
	if rerunErr == nil || migrate.ExitCode(rerunErr) != migrate.ExitCodeDirtyMigration {
		t.Fatalf("expected persistent dirty ExitCode 31 on restart, got code=%d err=%v", migrate.ExitCode(rerunErr), rerunErr)
	}
}

// TestFMEA07ClusterRevocationPartitionStaleBreaker verifies ADR-0006/FMEA-07:
// losing connectivity to PostgreSQL+Valkey beyond max_stale trips fail-closed ErrClusterPartitioned
// (503 stale_security_state), and heals cleanly when backends recover.
func TestFMEA07ClusterRevocationPartitionStaleBreaker(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000700000))
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "fmea07.db"), NowMs: func() int64 { return clk.Now().UnixMilli() }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: func() int64 { return clk.Now().UnixMilli() }})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := snapshot.NewClusterSync(snapshot.ClusterSyncConfig{
		Manager:      mgr,
		DB:           st.RawDB(),
		PollInterval: 2 * time.Second,
		MaxStale:     10 * time.Second,
		NowMs:        func() int64 { return clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("NewClusterSync: %v", err)
	}

	cs.ProbeBackends(ctx)
	if err := cs.CheckServingReady(); err != nil {
		t.Fatalf("expected healthy cluster sync before partition, got %v", err)
	}

	// Inject partition + advance virtual clock 11s (> 10s max_stale).
	cs.SetFaulted(true, true)
	clk.Advance(11 * time.Second)
	cs.ProbeBackends(ctx)

	if !cs.IsPartitioned() {
		t.Fatalf("expected IsPartitioned=true past 10s max_stale")
	}
	if err := cs.CheckServingReady(); !errors.Is(err, snapshot.ErrClusterPartitioned) {
		t.Fatalf("expected ErrClusterPartitioned, got %v", err)
	}

	// Heal partition -> CheckServingReady clears immediately.
	cs.SetFaulted(false, false)
	cs.ProbeBackends(ctx)
	if err := cs.CheckServingReady(); err != nil {
		t.Fatalf("expected CheckServingReady nil after heal, got %v", err)
	}
}

// TestFMEA08OAuthSingleFlightAndInvalidGrantHalt verifies FMEA-08:
// concurrent expired-token refreshes coalesce behind single-flight (1 upstream refresh)
// and deterministic invalid_grant halts retries with AUTH_EXPIRED_REAUTH_REQUIRED + ALT-OAUTH-EXPIRY.
func TestFMEA08OAuthSingleFlightAndInvalidGrantHalt(t *testing.T) {
	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000800000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	now := nowFn()

	var refreshMode atomic.Int32 // 0 = valid coalesce, 1 = invalid_grant
	var refreshCalls atomic.Int32
	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		_ = r.ParseForm()
		if refreshMode.Load() == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked upstream"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-refreshed","refresh_token":"rt-next","token_type":"Bearer","expires_in":3600}`))
	}))
	defer oauthSrv.Close()

	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "fmea08.db"), NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	db := st.RawDB()
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db, NowMs: nowFn})
	if err != nil {
		t.Fatal(err)
	}
	vs, err := vault.NewStore(vault.Config{DB: db, KEK: newChaosKEK(t, "kek-fmea08"), NowMs: nowFn, PostShredHook: st.CheckpointWAL})
	if err != nil {
		t.Fatal(err)
	}
	defer vs.Close()

	orgID := idgen.MustNew(idgen.PrefixOrg)
	prcID := idgen.MustNew(idgen.PrefixPrincipal)
	connID := idgen.MustNew(idgen.PrefixConnection)
	pkgID := idgen.MustNew(idgen.PrefixExtension)
	verID := idgen.MustNew(idgen.PrefixExtensionVersion)
	instID := idgen.MustNew(idgen.PrefixInstallation)
	adpID := idgen.MustNew(idgen.PrefixAdapter)
	u, _ := url.Parse(oauthSrv.URL)
	if _, err := db.Exec(`INSERT INTO organizations (id, slug, display_name, created_at_ms, updated_at_ms) VALUES (?, 'fmea08', 'FMEA08', ?, ?)`, orgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms) VALUES (?, ?, 'HUMAN_LOCAL', 'oauth@fmea08.test', 'ACTIVE', ?, ?)`, prcID, orgID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES (?, 'oauth.fmea08', 'PROVIDER_ADAPTER', 'test', 'official', ?)`, pkgID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO extension_versions (id, package_id, semver, contract_version, execution_tier, bundle_sha256, source_uri, manifest_json, created_at_ms) VALUES (?, ?, '1.0.0', 'v1', 'DECLARATIVE_T1', ?, 'builtin://test', '{}', ?)`, verID, pkgID, strings.Repeat("b", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO extension_installations (id, package_id, active_version_id, state, created_at_ms, updated_at_ms) VALUES (?, ?, ?, 'ENABLED_ACTIVE', ?, ?)`, instID, pkgID, verID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO provider_adapters (id, installation_id, adapter_slug, allowed_hosts_json, created_at_ms) VALUES (?, ?, 'oauth-fmea08', ?, ?)`, adpID, instID, fmt.Sprintf(`[%q]`, u.Host), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, created_at_ms, updated_at_ms) VALUES (?, ?, ?, 'oauth-conn', 'https://resource.example/', 'GLOBAL', 'COMMERCIAL_API', 'OAUTH2', 'ACTIVE', ?, ?)`, connID, orgID, adpID, now, now); err != nil {
		t.Fatal(err)
	}

	resource := oauthSrv.URL + "/"
	tsBytes, err := json.Marshal(oauth.TokenSet{
		AccessToken:  []byte("tok-stale"),
		RefreshToken: []byte("rt-0"),
		TokenType:    "Bearer",
		TokenURL:     oauthSrv.URL + "/token",
		ClientID:     "client-1",
		ConnectionID: connID,
		GrantType:    oauth.GrantAuthorizationCodePKCE,
		Scope:        "read",
		Resource:     resource,
		ExpiresAtMs:  now - 10_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := vs.SealSecret(ctx, vault.SealSecretParams{
		OrgID: orgID, Name: "oauth-tok", SecretKind: vault.SecretKindOAuth2TokenSet, BoundAudience: &resource, Plaintext: tsBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO connection_secret_bindings (org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms) VALUES (?, ?, 'oauth_access_token', ?, 'HOST_BLIND_INJECT', ?)`, orgID, connID, meta.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO oauth_token_states (org_id, credential_id, bound_principal_id, state, granted_scopes, bound_resource_uri, access_expires_at_ms, updated_at_ms) VALUES (?, ?, ?, 'TOKEN_VALID', 'read', ?, ?, ?)`, orgID, meta.ID, prcID, resource, now-1, now); err != nil {
		t.Fatal(err)
	}

	svc, err := oauth.NewService(oauth.ServiceConfig{
		DB: db, Vault: vs, UnsealCapability: vault.MintUnsealCapabilityForBroker(), Audit: auditLog,
		NowMs: nowFn, HostResolver: egress.NewStdHostResolver(), AllowLoopbackHTTP: true, RedirectURI: "http://127.0.0.1/admin/v1/oauth/callback",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Burst of 50 concurrent AccessToken calls must coalesce to single upstream refresh.
	const callers = 50
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tok, e := svc.AccessToken(ctx, orgID, meta.ID, resource)
			if e == nil && string(tok) != "tok-refreshed" {
				e = fmt.Errorf("unexpected token %q", string(tok))
			}
			clear(tok)
			errs <- e
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("concurrent AccessToken: %v", e)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("expected 1 single-flight upstream refresh for %d callers, got %d", callers, got)
	}

	// Advance clock past refreshed token TTL and force invalid_grant on next refresh.
	refreshMode.Store(1)
	refreshCalls.Store(0)
	clk.Advance(2 * time.Hour)
	if _, err := svc.AccessToken(ctx, orgID, meta.ID, resource); err == nil {
		t.Fatalf("expected invalid_grant failure")
	}
	if _, err := svc.AccessToken(ctx, orgID, meta.ID, resource); err == nil {
		t.Fatalf("expected subsequent AccessToken after invalid_grant to fail closed immediately")
	}
	if refreshCalls.Load() != 1 {
		t.Fatalf("FMEA-08 violation: expected 0 retry storms after invalid_grant, got calls=%d", refreshCalls.Load())
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM oauth_token_states WHERE org_id = ? AND credential_id = ?`, orgID, meta.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "AUTH_EXPIRED_REAUTH_REQUIRED" {
		t.Fatalf("expected AUTH_EXPIRED_REAUTH_REQUIRED, got %q", state)
	}
}
