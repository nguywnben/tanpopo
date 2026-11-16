// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

type budgetIngressHarness struct {
	h      *ingressHarness
	ledger *ledger.Service
	alerts []ledger.AlertEvent
	alMu   sync.Mutex
	lmID   string
	adpID  string
	upFn   func(w http.ResponseWriter, r *http.Request, body []byte)
	upMu   sync.Mutex
	upHits atomic.Int64
}

func (b *budgetIngressHarness) setUpstream(fn func(w http.ResponseWriter, r *http.Request, body []byte)) {
	b.upMu.Lock()
	b.upFn = fn
	b.upMu.Unlock()
}

func setupBudgetIngressHarness(t *testing.T, pricingJSON string, hardLimitMicros int64, defaultHoldOut int64) *budgetIngressHarness {
	t.Helper()
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})

	ctx := context.Background()
	clk := clock.NewFakeClock(time.UnixMilli(1760000200000))
	nowFn := func() int64 { return clk.Now().UnixMilli() }
	tmp := t.TempDir()
	pluginsDir := filepath.Join(tmp, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(tmp, "tanpopo.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	audit := &memAuditSink{}
	bh := &budgetIngressHarness{}
	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	})

	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bh.upHits.Add(1)
		bh.upMu.Lock()
		fn := bh.upFn
		bh.upMu.Unlock()
		if fn != nil {
			fn(w, r, body)
		}
	}))
	t.Cleanup(func() {
		upSrv.CloseClientConnections()
		upSrv.Close()
	})

	pubPub, pubPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := trust.TrustedKeyring{
		PublisherPublicKeys: map[string]ed25519.PublicKey{"acme": pubPub},
	}

	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:      st.RawDB(),
		NowMs:   nowFn,
		Keyring: keyring,
	})
	if err != nil {
		t.Fatal(err)
	}

	rawMaster := make([]byte, 32)
	if _, err := rand.Read(rawMaster); err != nil {
		t.Fatal(err)
	}
	kek, err := vault.NewStaticKEKProvider("kek-test-1", rawMaster)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kek.Close)

	vStore, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vStore.Close)

	catRepo, err := catalog.NewRepository(catalog.Config{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		Keyring:   keyring,
		Audit:     audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver:       catRepo,
		PolicyResolver: catRepo,
		Vault:          vStore,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          audit,
		NowMs:          nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:         st.RawDB(),
		PluginsDir: pluginsDir,
		Keyring:    keyring,
		Audit:      audit,
		NowMs:      nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	vkSvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: mgr,
		AuditSink: audit,
	})
	if err != nil {
		t.Fatal(err)
	}

	lSvc, err := ledger.NewService(ledger.Config{
		DB:    st.RawDB(),
		Audit: audit,
		NowMs: nowFn,
		Alerts: ledger.AlertSinkFunc(func(_ context.Context, ev ledger.AlertEvent) {
			bh.alMu.Lock()
			bh.alerts = append(bh.alerts, ev)
			bh.alMu.Unlock()
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	bh.ledger = lSvc

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		Slug:           "finops-org",
		DisplayName:    "FinOps Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "finops-proj",
		DisplayName: "FinOps Proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := st.Tenancy().CreateEnvironment(ctx, org.ID, proj.ID, storageport.CreateEnvironmentParams{
		Slug: "prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES ('prc_op_1', NULL, 'HUMAN_LOCAL', 'op@acme.test', 'ACTIVE', 1, ?, ?)
	`, nowFn(), nowFn()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_op_1', NULL, 'prc_op_1', 'INSTANCE', 'instance', 'OPERATOR', 1, ?)
	`, nowFn()); err != nil {
		t.Fatal(err)
	}

	if hardLimitMicros >= 0 {
		if _, err := lSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
			ID:                     "led_proj_budget",
			OrgID:                  org.ID,
			ScopeType:              ledger.ScopeProject,
			ScopeID:                proj.ID,
			PeriodKind:             ledger.PeriodTotalLifetime,
			PeriodStartMs:          0,
			PeriodEndMs:            nowFn() + 86_400_000,
			HardLimitMicros:        hardLimitMicros,
			SoftAlertThresholdsPct: []int{80, 90},
		}); err != nil {
			t.Fatal(err)
		}
	}

	extSrcDir := filepath.Join(tmp, "acme-decl-src")
	if err := os.MkdirAll(extSrcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	declManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/acme-decl"
  name: "AcmeAI Declarative Adapter"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "acme-decl"
    supportedDialects: ["openai"]
    allowedNativePaths: ["v1/chat/completions"]
    allowedNativeHeaders: ["content-type", "accept"]
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
    - "provider:inspect_native_request"
adapter:
  slug: "acme-decl"
  nativePrefix: "/native/openai"
  wireProtocol: "openai_chat_v1"
  endpoints:
    chat: "/v1/chat/completions"
    responses: "/v1/responses"
    embeddings: "/v1/embeddings"
  streamMapping:
    doneSentinel: "[DONE]"
    deltaContentPath: "choices.0.delta.content"
    finishReasonPath: "choices.0.finish_reason"
    modelPath: "model"
  usageExtraction:
    inputTokensPath: "usage.prompt_tokens"
    outputTokensPath: "usage.completion_tokens"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	if err := os.WriteFile(filepath.Join(extSrcDir, "manifest.yaml"), []byte(declManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	tpkgPath := filepath.Join(tmp, "acme-decl-1.0.0.tpkg")
	packRes, err := testharness.PackExtension(ctx, extSrcDir, tpkgPath)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(pubPriv, installer.SignatureSigningPayload("@acme/acme-decl", "1.0.0", packRes.SHA256))
	staged, err := instSvc.StageFreshInstall(ctx, installer.StageInstallRequest{
		ArchivePath: tpkgPath,
		Source: installer.SourceSpec{
			Kind:           installer.SourceKindLocalArchive,
			URI:            "./acme-decl-1.0.0.tpkg",
			ExpectedSHA256: packRes.SHA256,
		},
		Signature:   installer.ProvenanceSignature{PublisherOrRootSig: sig},
		ActorID:     "prc_op_1",
		RequestID:   "req_stage_01",
		TargetOrgID: org.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := instSvc.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
		InstallationID:      staged.Installation.InstallationID,
		ExpectedVersion:     staged.Installation.Version,
		ApproverPrincipalID: "prc_op_1",
		ActivateImmediately: true,
		OrgID:               org.ID,
		RequestID:           "req_approve_01",
	})
	if err != nil {
		t.Fatal(err)
	}

	nativePrefix := "/native/openai"
	adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       "acme-decl",
		NativePrefix:      &nativePrefix,
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	bh.adpID = adp.ID

	canarySec := canary.DefaultCanarySecret()
	secMeta, err := vStore.SealSecret(ctx, vault.SealSecretParams{
		OrgID:      org.ID,
		Name:       "acme-key",
		SecretKind: vault.SecretKindAPIKey,
		Plaintext:  []byte(canarySec),
	})
	if err != nil {
		t.Fatal(err)
	}

	conn, err := catRepo.CreateProviderConnection(ctx, catalog.CreateProviderConnectionParams{
		OrgID:           org.ID,
		AdapterID:       adp.ID,
		Name:            "acme-conn",
		EndpointBaseURL: upSrv.URL,
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
		t.Fatal(err)
	}
	conn, err = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "val")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "act"); err != nil {
		t.Fatal(err)
	}

	lmod, err := catRepo.CreateLogicalModel(ctx, catalog.CreateLogicalModelParams{
		OrgID:           &org.ID,
		CanonicalSlug:   "acme/chat-1",
		ModalityKind:    catalog.ModalityChat,
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		BaseCapabilities: map[string]string{
			"streaming":    "VERIFIED",
			"tool_calling": "VERIFIED",
			"embeddings":   "VERIFIED",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bh.lmID = lmod.ID

	depl, err := catRepo.CreateModelDeployment(ctx, catalog.CreateModelDeploymentParams{
		OrgID:              org.ID,
		ConnectionID:       conn.ID,
		LogicalModelID:     lmod.ID,
		UpstreamModelName:  "acme-gpt-upstream",
		AssuranceLevel:     catalog.AssuranceTested,
		State:              catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs:   5000,
		FirstByteTimeoutMs: 5000,
		IdleChunkTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pricingJSON != "" {
		if _, err := st.RawDB().ExecContext(ctx, `
			UPDATE model_deployments SET pricing_nano_usd_json = ?, version = version + 1 WHERE id = ?
		`, pricingJSON, depl.ID); err != nil {
			t.Fatal(err)
		}
	}

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj.ID,
		EnvID:                env.ID,
		Name:                 "test-budget-key",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 16,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:               mgr,
		Broker:                  brk,
		PluginsDir:              pluginsDir,
		Audit:                   audit,
		NowMs:                   nowFn,
		IdempotencyDB:           st.RawDB(),
		BudgetLedger:            lSvc,
		DefaultHoldOutputTokens: defaultHoldOut,
		SSEHeartbeatInterval:    200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	client := netguard.NewHTTPClient()
	t.Cleanup(client.CloseIdleConnections)

	bh.h = &ingressHarness{
		t:         t,
		ctx:       ctx,
		clk:       clk,
		store:     st,
		snapshots: mgr,
		catalog:   catRepo,
		vault:     vStore,
		broker:    brk,
		installer: instSvc,
		vkeys:     vkSvc,
		audit:     audit,
		server:    srv,
		httpSrv:   httpSrv,
		client:    client,
		org:       org,
		project:   proj,
		env:       env,
		vkeyRaw:   createdVK.SecretKey,
		vkeyID:    createdVK.Summary.ID,
		secret:    canarySec,
		deplID:    depl.ID,
		connID:    conn.ID,
		pluginsD:  pluginsDir,
	}
	return bh
}

func TestTP0021_HTTPBudgetPreHoldAndExceedBlocksChatResponsesEmbeddings(t *testing.T) {
	// 10 micro-USD/token input & output (1e10 nano-USD/1M); DefaultHoldOutputTokens=250 -> pre-hold ~2,700 micro-USD per request.
	// HardLimit=10,000 allows 3 requests cleanly (~7,650 reserved/settled) and blocks once exhausted before hitting upstream.
	pricingJSON := `{"input_per_1m_nano_usd":10000000000,"output_per_1m_nano_usd":10000000000}`
	bh := setupBudgetIngressHarness(t, pricingJSON, 10_000, 250)

	// Configure upstream to consume 3,500 micro-USD per completion (10 prompt + 340 completion = 350 tokens = 3,500 micro-USD).
	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if r.URL.Path == "/v1/embeddings" {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"acme-gpt-upstream","usage":{"prompt_tokens":350,"total_tokens":350}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":340,"total_tokens":350}}`))
	})

	// Request 1 (/v1/chat/completions): settles 3,500 micro-USD (remaining: 6,500).
	resp1, body1 := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":250,"messages":[{"role":"user","content":"hello"}]}`), nil, nil)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("req1 status=%d body=%s", resp1.StatusCode, string(body1))
	}

	// Request 2 (/v1/responses): settles another 3,500 micro-USD (total settled: 7,000, remaining: 3,000).
	resp2, body2 := bh.h.doChatReq(t, http.MethodPost, "/v1/responses", []byte(`{"model":"acme/chat-1","max_output_tokens":250,"input":"hello"}`), nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("req2 status=%d body=%s", resp2.StatusCode, string(body2))
	}

	// Request 3 (/v1/embeddings with remaining=3,000 -> hold ~2,550 <= 3,000 succeeds -> settles 3,500 -> total 10,500, carryover -500).
	resp3, body3 := bh.h.doChatReq(t, http.MethodPost, "/v1/embeddings", []byte(`{"model":"acme/chat-1","input":"hello"}`), nil, nil)
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("req3 status=%d body=%s", resp3.StatusCode, string(body3))
	}

	hitsBeforeBlock := bh.upHits.Load()

	// Next request across /v1/chat/completions, /v1/responses, and /v1/embeddings MUST fail fast with 429 project_budget_exceeded without touching upstream.
	for _, ep := range []struct {
		path string
		body string
	}{
		{"/v1/chat/completions", `{"model":"acme/chat-1","max_tokens":10,"messages":[{"role":"user","content":"blocked"}]}`},
		{"/v1/responses", `{"model":"acme/chat-1","max_output_tokens":10,"input":"blocked"}`},
		{"/v1/embeddings", `{"model":"acme/chat-1","input":"blocked"}`},
	} {
		resp4, body4 := bh.h.doChatReq(t, http.MethodPost, ep.path, []byte(ep.body), nil, nil)
		if resp4.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body4), "project_budget_exceeded") {
			t.Fatalf("expected 429 project_budget_exceeded on %s, got status=%d body=%s", ep.path, resp4.StatusCode, string(body4))
		}
	}
	if bh.upHits.Load() != hitsBeforeBlock {
		t.Fatalf("expected 0 upstream hits on budget-blocked requests, got %d extra", bh.upHits.Load()-hitsBeforeBlock)
	}
}

func TestTP0021_HTTPFailoverDeltaReserveBlocksBeforeHigherCostFallback(t *testing.T) {
	// Primary holds ~120 micro-USD (1 micro-USD/tok); Fallback holds ~12,000 micro-USD (100 micro-USD/tok). Budget limit is 5,000 micro-USD.
	// Primary returns 503 pre-stream -> failover checks DeltaReserve -> MUST reject with 429 BEFORE contacting fallbackSrv!
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 5_000, 100)

	fallbackSrv := newSpyHTTPUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"fb","object":"chat.completion","model":"fb","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}`))
	})

	caps := `{"streaming":"VERIFIED","tool_calling":"VERIFIED","embeddings":"VERIFIED"}`
	_, depExpensiveFB := seedConnectionAndDeployment(t, bh.h, "fb-expensive", fallbackSrv.srv.URL, "us", bh.lmID, "fb-upstream", caps)
	if _, err := bh.h.store.RawDB().ExecContext(bh.h.ctx, `
		UPDATE model_deployments SET pricing_nano_usd_json = ?, version = version + 1 WHERE id = ?
	`, `{"input_per_1m_nano_usd":100000000000,"output_per_1m_nano_usd":100000000000}`, depExpensiveFB); err != nil {
		t.Fatal(err)
	}

	seedRoutePolicy(t, bh.h, "rt_delta_test", "delta-route", "PRIORITY_FAILOVER", 2, nil, []struct {
		id       string
		depID    string
		priority int
		weight   int
	}{
		{id: "rtg_pri", depID: bh.h.deplID, priority: 1, weight: 100},
		{id: "rtg_fb", depID: depExpensiveFB, priority: 2, weight: 100},
	})

	if _, err := bh.h.store.RawDB().ExecContext(bh.h.ctx, `
		UPDATE virtual_keys SET allowed_models_json = '["*"]', version = version + 1 WHERE id = ?
	`, bh.h.vkeyID); err != nil {
		t.Fatal(err)
	}
	insertChangelogAndRefresh(t, bh.h, "virtual_key", bh.h.vkeyID)

	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"primary 503"}}`))
	})

	resp, body := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"delta-route","max_tokens":100,"messages":[{"role":"user","content":"failover check"}]}`), nil, nil)
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "project_budget_exceeded") {
		t.Fatalf("expected 429 project_budget_exceeded when fallback exceeds headroom, got status=%d body=%s", resp.StatusCode, string(body))
	}
	if fallbackSrv.hits.Load() != 0 {
		t.Fatalf("expected 0 hits on expensive fallback after DeltaReserve failure, got %d", fallbackSrv.hits.Load())
	}

	// Verify initial hold was cleanly released after failure.
	led, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.ReservedHoldMicros != 0 {
		t.Fatalf("expected reserved_hold_micros=0 after aborted failover, got %d", led.ReservedHoldMicros)
	}

	// Now raise budget limit so DeltaReserve covers fallback pre-hold (~12,000 micro-USD) and verify failover succeeds.
	if _, err := bh.ledger.UpsertLedger(bh.h.ctx, ledger.UpsertLedgerParams{
		ID:              "led_proj_budget",
		OrgID:           bh.h.org.ID,
		ScopeType:       ledger.ScopeProject,
		ScopeID:         bh.h.project.ID,
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   0,
		PeriodEndMs:     bh.h.clk.Now().UnixMilli() + 86_400_000,
		HardLimitMicros: 50_000,
	}); err != nil {
		t.Fatal(err)
	}
	respOK, bodyOK := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"delta-route","max_tokens":100,"messages":[{"role":"user","content":"failover ok"}]}`), nil, nil)
	if respOK.StatusCode != http.StatusOK {
		t.Fatalf("expected failover 200 OK after headroom increase, got status=%d body=%s", respOK.StatusCode, string(bodyOK))
	}
	if fallbackSrv.hits.Load() != 1 {
		t.Fatalf("expected 1 fallback hit, got %d", fallbackSrv.hits.Load())
	}
}

func TestTP0021_HTTPMidStreamAbortSettlesEstimatedFromEmittedBytes(t *testing.T) {
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 50_000, 100)

	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_abort\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial stream payload here\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	})

	resp, body := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"partial"}]}`), nil, nil)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "mid_stream_abort") {
		t.Fatalf("expected mid-stream abort SSE frame, got status=%d body=%s", resp.StatusCode, string(body))
	}

	var accuracy string
	var outTokens int64
	var costMicros int64
	if err := bh.h.store.RawDB().QueryRowContext(bh.h.ctx, `
		SELECT usage_accuracy, output_tokens, total_cost_micros FROM usage_settlements ORDER BY settled_at_ms DESC LIMIT 1
	`).Scan(&accuracy, &outTokens, &costMicros); err != nil {
		t.Fatalf("query usage_settlements: %v", err)
	}
	if accuracy != "ESTIMATED" || outTokens <= 0 || costMicros <= 0 {
		t.Fatalf("AC-2.1 failed: expected ESTIMATED settlement with outputTokens>0 & costMicros>0, got accuracy=%s outTokens=%d cost=%d", accuracy, outTokens, costMicros)
	}

	led, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.ReservedHoldMicros != 0 {
		t.Fatalf("expected reserved_hold_micros=0 after aborted SSE settlement, got %d", led.ReservedHoldMicros)
	}
}

func TestTP0021_HTTPUnpricedSelfHostedModelSettlesUnmeteredFlagged(t *testing.T) {
	// Deployment starts with empty `{}` pricing rates (AC-2.2).
	bh := setupBudgetIngressHarness(t, `{}`, 50_000, 100)

	resp, body := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"self hosted"}]}`), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for unpriced model, got status=%d body=%s", resp.StatusCode, string(body))
	}
	if got := resp.Header.Get("X-Tanpopo-Usage-Accuracy"); got != "UNMETERED_FLAGGED" {
		t.Fatalf("expected X-Tanpopo-Usage-Accuracy=UNMETERED_FLAGGED, got %q", got)
	}

	var accuracy string
	var totalCost int64
	if err := bh.h.store.RawDB().QueryRowContext(bh.h.ctx, `
		SELECT usage_accuracy, total_cost_micros FROM usage_settlements LIMIT 1
	`).Scan(&accuracy, &totalCost); err != nil {
		t.Fatalf("query usage_settlements: %v", err)
	}
	if accuracy != "UNMETERED_FLAGGED" || totalCost != 0 {
		t.Fatalf("AC-2.2 failed: expected accuracy=UNMETERED_FLAGGED totalCost=0, got %s cost=%d", accuracy, totalCost)
	}
	if bh.ledger.UnmeteredSpendCount() != 1 {
		t.Fatalf("expected UnmeteredSpendCount=1 (ALT-UNMETERED-SPEND), got %d", bh.ledger.UnmeteredSpendCount())
	}
	bh.alMu.Lock()
	alertsCopy := append([]ledger.AlertEvent(nil), bh.alerts...)
	bh.alMu.Unlock()
	foundAlert := false
	for _, ev := range alertsCopy {
		if ev.Code == "ALT-UNMETERED-SPEND" {
			foundAlert = true
		}
	}
	if !foundAlert {
		t.Fatalf("expected ALT-UNMETERED-SPEND alert emission, got %+v", alertsCopy)
	}
}

func TestTP0021_HTTPNegativeCarryoverBlocksNextRequest(t *testing.T) {
	// 1 micro-USD/token (1e9 nano-USD/1M); max_tokens=500 -> pre-hold ~510 < 10,000 limit; actual response usage reports 10,050 tokens -> 10,050 micro-USD.
	// Carryover = -50 -> immediate next HTTP request MUST fail with 429 project_budget_exceeded!
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 10_000, 100)

	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"c_over","object":"chat.completion","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"overrun"},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":10000,"total_tokens":10050}}`))
	})

	resp1, body1 := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":500,"messages":[{"role":"user","content":"go"}]}`), nil, nil)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("AC-3 active request failed unexpectedly: status=%d body=%s", resp1.StatusCode, string(body1))
	}

	led, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.AvailableBalance() != -50 || led.NegativeCarryoverMicros != 50 {
		t.Fatalf("AC-3 failed: expected available=-50 carryover=50, got available=%d carryover=%d", led.AvailableBalance(), led.NegativeCarryoverMicros)
	}

	hits := bh.upHits.Load()
	resp2, body2 := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":10,"messages":[{"role":"user","content":"next"}]}`), nil, nil)
	if resp2.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body2), "project_budget_exceeded") {
		t.Fatalf("expected immediate 429 project_budget_exceeded after negative carryover, got status=%d body=%s", resp2.StatusCode, string(body2))
	}
	if bh.upHits.Load() != hits {
		t.Fatalf("expected upstream not called after negative carryover block")
	}
}

func TestTP0021_HTTPNativePassthroughBudgetRoundTrip(t *testing.T) {
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 10_000, 100)

	// 1. Sync /native/openai/v1/chat/completions reports exact usage (200 in + 300 out = 500 micro-USD).
	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"nat_1","object":"chat.completion","model":"acme/chat-1","choices":[{"index":0,"message":{"role":"assistant","content":"native"},"finish_reason":"stop"}],"usage":{"prompt_tokens":200,"completion_tokens":300,"total_tokens":500}}`))
	})
	resp, body := bh.h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"native"}]}`), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("native sync failed: status=%d body=%s", resp.StatusCode, string(body))
	}
	if got := resp.Header.Get("X-Tanpopo-Usage-Accuracy"); got != "EXACT" {
		t.Fatalf("expected EXACT accuracy header on native sync, got %q", got)
	}

	led, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.SettledExactMicros != 500 || led.ReservedHoldMicros != 0 {
		t.Fatalf("expected native settled=500 reserved=0, got settled=%d reserved=%d", led.SettledExactMicros, led.ReservedHoldMicros)
	}

	// 2. Streaming /native/openai/v1/chat/completions mid-stream abort -> settles ESTIMATED with positive cost.
	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"nat_sse\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"native-chunk\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	})
	reqBody, _ := json.Marshal(map[string]any{
		"model":      "acme/chat-1",
		"stream":     true,
		"max_tokens": 50,
		"messages":   []map[string]string{{"role": "user", "content": "stream abort"}},
	})
	respSSE, bodySSE := bh.h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", reqBody, nil, nil)
	if respSSE.StatusCode != http.StatusOK || !strings.Contains(string(bodySSE), "mid_stream_abort") {
		t.Fatalf("expected mid_stream_abort on native SSE, got status=%d body=%s", respSSE.StatusCode, string(bodySSE))
	}

	var estMicros int64
	led2, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	estMicros = led2.SettledEstimatedMicros
	if estMicros <= 0 || led2.ReservedHoldMicros != 0 {
		t.Fatalf("expected native aborted SSE settled_estimated>0 & reserved=0, got est=%d reserved=%d", estMicros, led2.ReservedHoldMicros)
	}
}

func TestTP0021_DuplicateClientRequestIDDoesNotBypassBudget(t *testing.T) {
	// Limit=150 micro-USD, pricing=1 micro-USD/token, defaultHold=100 -> pre-hold ~110 micro-USD per request.
	// Only 1 request fits headroom; 4 concurrent requests sharing identical X-Tanpopo-Request-Id MUST NOT share one reservation!
	for _, route := range []string{"/v1/chat/completions", "/native/openai/v1/chat/completions"} {
		t.Run(route, func(t *testing.T) {
			bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 150, 100)
			releaseUpstream := make(chan struct{})
			bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
				<-releaseUpstream
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"c_dup","object":"chat.completion","model":"acme/chat-1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20}}`))
			})

			const N = 4
			statuses := make([]int, N)
			bodies := make([]string, N)
			var ready sync.WaitGroup
			ready.Add(N)
			var wg sync.WaitGroup
			for i := 0; i < N; i++ {
				idx := i
				wg.Add(1)
				go func() {
					defer wg.Done()
					ready.Done()
					ready.Wait()
					reqBody := []byte(`{"model":"acme/chat-1","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`)
					resp, body := bh.h.doChatReq(t, http.MethodPost, route, reqBody, map[string]string{
						"X-Tanpopo-Request-Id": "req_01ARZ3NDEKTSV4RRFFQ69G5FAV",
					}, nil)
					statuses[idx] = resp.StatusCode
					bodies[idx] = string(body)
				}()
			}
			// Give concurrent Reserve goroutines a brief moment, then release admitted upstream.
			time.Sleep(80 * time.Millisecond)
			close(releaseUpstream)
			wg.Wait()

			okCount, blockedCount := 0, 0
			for i, st := range statuses {
				switch st {
				case http.StatusOK:
					okCount++
				case http.StatusTooManyRequests:
					if !strings.Contains(bodies[i], "project_budget_exceeded") {
						t.Fatalf("unexpected 429 body: %s", bodies[i])
					}
					blockedCount++
				default:
					t.Fatalf("unexpected status[%d]=%d body=%s", i, st, bodies[i])
				}
			}
			if okCount != 1 || blockedCount != N-1 {
				t.Fatalf("expected exactly 1 OK and %d 429s, got ok=%d blocked=%d statuses=%v", N-1, okCount, blockedCount, statuses)
			}
		})
	}
}

func TestTP0021_StreamingLeaseRenewalPreventsPrematureReclaim(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t)
	})
	// Build harness with LeaseTTLMs = 150ms using wall clock so renewal ticker (~50ms) and reaper (25ms) exercise real renewal under load.
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 50_000, 100)
	wallNow := func() int64 { return time.Now().UTC().UnixMilli() }

	shortLedger, err := ledger.NewService(ledger.Config{
		DB:         bh.h.store.RawDB(),
		Audit:      bh.h.audit,
		NowMs:      wallNow,
		LeaseTTLMs: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shortLedger.UpsertLedger(bh.h.ctx, ledger.UpsertLedgerParams{
		ID:              "led_proj_budget",
		OrgID:           bh.h.org.ID,
		ScopeType:       ledger.ScopeProject,
		ScopeID:         bh.h.project.ID,
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   0,
		PeriodEndMs:     wallNow() + 86_400_000,
		HardLimitMicros: 50_000,
	}); err != nil {
		t.Fatal(err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:               bh.h.snapshots,
		Broker:                  bh.h.broker,
		PluginsDir:              bh.h.pluginsD,
		Audit:                   bh.h.audit,
		NowMs:                   wallNow,
		BudgetLedger:            shortLedger,
		DefaultHoldOutputTokens: 100,
		SSEHeartbeatInterval:    50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = srv.Close(context.Background()) }()
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	reaper, err := ledger.NewOrphanReservationReaper(shortLedger, 25*time.Millisecond, 50)
	if err != nil {
		t.Fatal(err)
	}
	stopReaper := reaper.Start(bh.h.ctx)
	defer stopReaper()

	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"c_renew\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"part1\"}}]}\n\n"))
		if fl != nil {
			fl.Flush()
		}
		time.Sleep(360 * time.Millisecond)
		_, _ = w.Write([]byte("data: {\"id\":\"c_renew\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":15,\"completion_tokens\":25,\"total_tokens\":40}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if fl != nil {
			fl.Flush()
		}
	})

	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","stream":true,"max_tokens":50,"messages":[{"role":"user","content":"long stream"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bh.h.vkeyRaw)
	req.Header.Set("Content-Type", "application/json")
	resp, err := bh.h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "[DONE]") {
		t.Fatalf("expected successful long stream, got status=%d body=%s", resp.StatusCode, string(body))
	}

	var resState string
	if err := bh.h.store.RawDB().QueryRowContext(bh.h.ctx, `SELECT state FROM budget_reservations ORDER BY created_at_ms DESC LIMIT 1`).Scan(&resState); err != nil {
		t.Fatal(err)
	}
	if resState != ledger.StateSettledExact {
		t.Fatalf("expected reservation state SETTLED_EXACT (no premature reclaim across 360ms stream / 150ms TTL), got %s", resState)
	}
	led, err := shortLedger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.SettledExactMicros != 40 || led.ReservedHoldMicros != 0 {
		t.Fatalf("expected settled_exact=40 reserved=0, got %+v", led)
	}
}

func TestTP0021_NativeMultimodalPreHoldBlocksExceedingBudget(t *testing.T) {
	// image_per_unit_nano_usd = 5,000,000,000 (5,000 micro-USD/image). Budget hard limit = 4,000 micro-USD.
	// /native/openai/v1/chat/completions carrying an image_url MUST be rejected with 429 before contacting upstream!
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000,"image_per_unit_nano_usd":5000000000}`, 4_000, 100)
	hitsBefore := bh.upHits.Load()

	bodyJSON := `{"model":"acme/chat-1","max_tokens":10,"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]}]}`
	resp, body := bh.h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(bodyJSON), nil, nil)
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(string(body), "project_budget_exceeded") {
		t.Fatalf("expected 429 project_budget_exceeded for native multimodal pre-hold, got status=%d body=%s", resp.StatusCode, string(body))
	}
	if bh.upHits.Load() != hitsBefore {
		t.Fatalf("expected 0 upstream hits on blocked multimodal request, got %d", bh.upHits.Load()-hitsBefore)
	}
}

func TestTP0021_SyncSettleErrorFailsClosedWith503(t *testing.T) {
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 10_000, 100)
	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if _, err := bh.h.store.RawDB().ExecContext(bh.h.ctx, `
			CREATE TRIGGER fail_settlement_insert BEFORE INSERT ON usage_settlements
			BEGIN
				SELECT RAISE(ABORT, 'injected_settlement_failure');
			END;
		`); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"c_failclosed","object":"chat.completion","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"secret"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20}}`))
	})
	defer func() {
		_, _ = bh.h.store.RawDB().ExecContext(bh.h.ctx, `DROP TRIGGER IF EXISTS fail_settlement_insert;`)
	}()

	for _, ep := range []string{"/v1/chat/completions", "/native/openai/v1/chat/completions"} {
		_, _ = bh.h.store.RawDB().ExecContext(bh.h.ctx, `DROP TRIGGER IF EXISTS fail_settlement_insert;`)
		resp, body := bh.h.doChatReq(t, http.MethodPost, ep, []byte(`{"model":"acme/chat-1","max_tokens":50,"messages":[{"role":"user","content":"fail closed"}]}`), nil, nil)
		if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "budget_ledger_unavailable") {
			t.Fatalf("%s: expected fail-closed 503 budget_ledger_unavailable, got status=%d body=%s", ep, resp.StatusCode, string(body))
		}
		if strings.Contains(string(body), `"secret"`) {
			t.Fatalf("%s: leaked upstream unbilled response body: %s", ep, string(body))
		}
	}
	led, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.ReservedHoldMicros != 0 {
		t.Fatalf("expected reserved_hold_micros=0 released on fail-closed settle failure, got %d", led.ReservedHoldMicros)
	}
}

func TestTP0021_IdempotencyKeyReplayDoesNotDoubleChargeBudget(t *testing.T) {
	bh := setupBudgetIngressHarness(t, `{"input_per_1m_nano_usd":1000000000,"output_per_1m_nano_usd":1000000000}`, 10_000, 100)
	bh.setUpstream(func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"c_idem","object":"chat.completion","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"once"},"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":80,"total_tokens":200}}`))
	})

	reqBody := []byte(`{"model":"acme/chat-1","max_tokens":50,"messages":[{"role":"user","content":"idempotent budget"}]}`)
	hdrs := map[string]string{"Idempotency-Key": "idem-budget-key-01"}

	resp1, body1 := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", reqBody, hdrs, nil)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first idem call failed: status=%d body=%s", resp1.StatusCode, string(body1))
	}
	resp2, body2 := bh.h.doChatReq(t, http.MethodPost, "/v1/chat/completions", reqBody, hdrs, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("replay idem call failed: status=%d body=%s", resp2.StatusCode, string(body2))
	}
	if resp2.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("expected Idempotent-Replayed=true on replay, got %q", resp2.Header.Get("Idempotent-Replayed"))
	}
	if bh.upHits.Load() != 1 {
		t.Fatalf("expected upstream called exactly once across idempotent replay, got %d", bh.upHits.Load())
	}
	led, err := bh.ledger.GetLedger(bh.h.ctx, bh.h.org.ID, "led_proj_budget")
	if err != nil {
		t.Fatal(err)
	}
	if led.SettledExactMicros != 200 || led.ReservedHoldMicros != 0 {
		t.Fatalf("expected single 200 micro-USD settlement after replay, got settled=%d reserved=%d", led.SettledExactMicros, led.ReservedHoldMicros)
	}
}

