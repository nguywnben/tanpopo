// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

type recordingSpanExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (e *recordingSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	e.spans = append(e.spans, spans...)
	e.mu.Unlock()
	return nil
}

func (e *recordingSpanExporter) Shutdown(_ context.Context) error { return nil }

func (e *recordingSpanExporter) snapshot() []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := make([]sdktrace.ReadOnlySpan, len(e.spans))
	copy(cp, e.spans)
	return cp
}

type telemetryHarness struct {
	*ingressHarness
	pipe       *otel.Pipeline
	logBuf     *canary.SafeBuffer
	spanExp    *recordingSpanExporter
	faultGuard *wasm.FaultGuard
	breaker    *breaker.CircuitBreaker
}

func setupTelemetryIngressHarness(t *testing.T, pipeCfgMod func(cfg *otel.PipelineConfig)) *telemetryHarness {
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
	kek, err := vault.NewStaticKEKProvider("kek-tel-1", rawMaster)
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

	org, err := st.Tenancy().CreateOrganization(ctx, storageport.CreateOrgParams{
		Slug:           "tel-org",
		DisplayName:    "Telemetry Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "tel-proj",
		DisplayName: "Telemetry Proj",
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

	fake := fakellm.NewServer(t)

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
    supportedDialects: ["openai_compatible", "openai"]
    allowedNativePaths:
      - "^v1/(chat/completions|responses)$"
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
    - "provider:extract_native_usage"
adapter:
  slug: "acme-decl"
  wireProtocol: "openai_chat_v1"
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
		RequestID:   "req_stage_tel",
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
		RequestID:           "req_approve_tel",
	})
	if err != nil {
		t.Fatal(err)
	}

	adp, err := catRepo.CreateProviderAdapter(ctx, catalog.CreateProviderAdapterParams{
		InstallationID:    rec.InstallationID,
		AdapterSlug:       "acme-decl",
		SupportedDialects: []string{"openai"},
		AllowedHosts:      []string{"127.0.0.1"},
	})
	if err != nil {
		t.Fatal(err)
	}

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
		EndpointBaseURL: fake.URL(),
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
	conn, err = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "validate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "activate"); err != nil {
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
		},
	})
	if err != nil {
		t.Fatal(err)
	}

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

	createdVK, err := vkSvc.Create(ctx, vkey.CreateParams{
		OrgID:                org.ID,
		ProjectID:            proj.ID,
		EnvID:                env.ID,
		Name:                 "tel-live-key",
		AllowedPlanes:        "PROVIDER",
		AllowedRoutes:        []string{"*"},
		AllowedModels:        []string{"*"},
		MaxConcurrentStreams: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	logBuf := &canary.SafeBuffer{}
	spanExp := &recordingSpanExporter{}
	pCfg := otel.PipelineConfig{
		DB:                 st.RawDB(),
		Vault:              vStore,
		LogOut:             logBuf,
		MetricsScrapeToken: "scrape-token-xyz",
		OTLPExporter:       spanExp,
		FlushInterval:      10 * time.Millisecond,
		NowMs:              nowFn,
	}
	if pipeCfgMod != nil {
		pipeCfgMod(&pCfg)
	}
	pipe, err := otel.NewPipeline(pCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pipe.Shutdown(context.Background()) })

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:            mgr,
		Broker:               brk,
		PluginsDir:           pluginsDir,
		Audit:                audit,
		NowMs:                nowFn,
		SSEHeartbeatInterval: 200 * time.Millisecond,
		Telemetry:            pipe,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	client := fake.Client()
	t.Cleanup(client.CloseIdleConnections)

	return &telemetryHarness{
		ingressHarness: &ingressHarness{
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
			fake:      fake,
			server:    srv,
			httpSrv:   httpSrv,
			client:    client,
			org:       org,
			project:   proj,
			env:       env,
			vkeyRaw:   createdVK.SecretKey,
			vkeyID:    createdVK.Summary.ID,
			secret:    canarySec,
			instID:    rec.InstallationID,
			deplID:    depl.ID,
			connID:    conn.ID,
			pubPriv:   pubPriv,
			pubPub:    pubPub,
			pluginsD:  pluginsDir,
		},
		pipe:       pipe,
		logBuf:     logBuf,
		spanExp:    spanExp,
		faultGuard: srv.FaultGuard(),
		breaker:    srv.Breaker(),
	}
}

func TestGatewayTelemetryOPSAC10ZeroPromptByteAndStages(t *testing.T) {
	th := setupTelemetryIngressHarness(t, nil)

	sensitivePrompt := "SECRET_PATIENT_RECORD_998877"
	canarySk := "sk-tp-" + "canary-998877"

	// 1. Unified HTTP /v1/chat/completions carrying sensitive prompt + sk-tp canary + stream=false.
	chatPayload := fmt.Sprintf(`{"model":"acme/chat-1","messages":[{"role":"user","content":"%s token=%s"}]}`, sensitivePrompt, canarySk)
	req, _ := http.NewRequest(http.MethodPost, th.httpSrv.URL+"/v1/chat/completions", strings.NewReader(chatPayload))
	req.Header.Set("Authorization", "Bearer "+th.vkeyRaw)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", canarySk)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := th.client.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /v1/chat/completions, got %d", resp.StatusCode)
	}

	// 2. Native passthrough /native/openai/v1/chat/completions carrying sensitive payload.
	nativePayload := fmt.Sprintf(`{"model":"acme/chat-1","messages":[{"role":"user","content":"%s %s"}]}`, sensitivePrompt, canarySk)
	nReq, _ := http.NewRequest(http.MethodPost, th.httpSrv.URL+"/native/openai/v1/chat/completions", strings.NewReader(nativePayload))
	nReq.Header.Set("Authorization", "Bearer "+th.vkeyRaw)
	nReq.Header.Set("Content-Type", "application/json")
	nResp, err := th.client.Do(nReq)
	if err != nil {
		t.Fatalf("POST /native/openai/v1/chat/completions: %v", err)
	}
	_, _ = io.Copy(io.Discard, nResp.Body)
	_ = nResp.Body.Close()
	if nResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /native/openai/v1/chat/completions, got %d", nResp.StatusCode)
	}

	// 3. Streaming SSE request verifying active_streams returns to 0 and chunk relay histogram populates.
	ssePayload := `{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"hello sse"}]}`
	sReq, _ := http.NewRequest(http.MethodPost, th.httpSrv.URL+"/v1/chat/completions", strings.NewReader(ssePayload))
	sReq.Header.Set("Authorization", "Bearer "+th.vkeyRaw)
	sReq.Header.Set("Content-Type", "application/json")
	sResp, err := th.client.Do(sReq)
	if err != nil {
		t.Fatalf("POST /v1/chat/completions stream: %v", err)
	}
	_, _ = io.Copy(io.Discard, sResp.Body)
	_ = sResp.Body.Close()
	if sResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on SSE stream, got %d", sResp.StatusCode)
	}

	if err := th.pipe.Ring.ForceFlush(th.ctx); err != nil {
		t.Fatalf("ForceFlush spans: %v", err)
	}

	// Verify 0 debug_payload_blobs persisted by default.
	var blobCount int
	if err := th.store.RawDB().QueryRowContext(th.ctx, `SELECT count(*) FROM debug_payload_blobs`).Scan(&blobCount); err != nil {
		t.Fatal(err)
	}
	if blobCount != 0 {
		t.Fatalf("expected 0 debug_payload_blobs by default, got %d", blobCount)
	}

	// Verify request_traces_meta rows exist for both unified_http and native_http and contain 0 sensitive bytes.
	rows, err := th.store.RawDB().QueryContext(th.ctx, `
		SELECT entry_point, otel_trace_id, waterfall_stages_json
		FROM request_traces_meta ORDER BY rowid ASC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var entryPoints []string
	var sawUnifiedTraceParent bool
	for rows.Next() {
		var ep, tid, stagesJSON string
		if err := rows.Scan(&ep, &tid, &stagesJSON); err != nil {
			t.Fatal(err)
		}
		entryPoints = append(entryPoints, ep)
		if ep == "unified_http" && tid == "4bf92f3577b34da6a3ce929d0e0e4736" {
			sawUnifiedTraceParent = true
		}
		for _, forbidden := range []string{sensitivePrompt, canarySk, th.vkeyRaw, th.secret} {
			if strings.Contains(stagesJSON, forbidden) {
				t.Fatalf("trace waterfall_stages_json leaked sensitive content %q: %s", forbidden, stagesJSON)
			}
		}
		var stages []otel.WaterfallStage
		if err := json.Unmarshal([]byte(stagesJSON), &stages); err != nil {
			t.Fatalf("unmarshal waterfall_stages_json: %v", err)
		}
		stageSet := map[string]bool{}
		for _, st := range stages {
			stageSet[st.Stage] = true
		}
		for _, reqStage := range []string{"Policy", "RouteAttempt", "ExtensionHook", "Egress"} {
			if !stageSet[reqStage] {
				t.Fatalf("trace for %s missing waterfall stage %q in %v", ep, reqStage, stages)
			}
		}
	}
	if len(entryPoints) != 3 {
		t.Fatalf("expected 3 request_traces_meta rows (2 unified + 1 native), got %v", entryPoints)
	}
	if !sawUnifiedTraceParent {
		t.Fatalf("expected W3C traceparent trace_id preserved on unified_http request")
	}

	// Verify structured JSON log buffer contains zero secrets or prompt strings.
	logs := th.logBuf.String()
	for _, forbidden := range []string{sensitivePrompt, canarySk, th.vkeyRaw, th.secret} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("JSON logs leaked sensitive value %q:\n%s", forbidden, logs)
		}
	}
	if !strings.Contains(logs, `"entry_point":"unified_http"`) || !strings.Contains(logs, `"entry_point":"native_http"`) {
		t.Fatalf("expected both unified_http and native_http JSON log entries:\n%s", logs)
	}

	// Verify exported OTel spans contain zero sensitive strings and include all 5 waterfall stage names.
	exported := th.spanExp.snapshot()
	spanNames := map[string]bool{}
	for _, sp := range exported {
		spanNames[sp.Name()] = true
		for _, kv := range sp.Attributes() {
			val := kv.Value.Emit()
			for _, forbidden := range []string{sensitivePrompt, canarySk, th.vkeyRaw, th.secret} {
				if strings.Contains(val, forbidden) {
					t.Fatalf("exported OTel span %q attr %q leaked %q", sp.Name(), kv.Key, forbidden)
				}
			}
		}
	}
	for _, wantSpan := range []string{"Ingress", "Policy", "RouteAttempt", "ExtensionHook", "Egress"} {
		if !spanNames[wantSpan] {
			t.Fatalf("missing exported OTel span %q, got %v", wantSpan, spanNames)
		}
	}

	// Activate opt-in 24h debug capture session -> blob sealed under Project DEK and decrypts cleanly.
	if _, err := th.pipe.CreateDebugCaptureSession("dbg_sess_http", th.org.ID, th.project.ID, th.vkeyID, 24); err != nil {
		t.Fatal(err)
	}
	dbgReq, _ := http.NewRequest(http.MethodPost, th.httpSrv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"debug trace payload"}]}`))
	dbgReq.Header.Set("Authorization", "Bearer "+th.vkeyRaw)
	dbgReq.Header.Set("Content-Type", "application/json")
	dbgResp, err := th.client.Do(dbgReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, dbgResp.Body)
	_ = dbgResp.Body.Close()
	if dbgResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on debug-captured request, got %d", dbgResp.StatusCode)
	}
	var dbgReqID string
	var dbgNonce, dbgCT []byte
	if err := th.store.RawDB().QueryRowContext(th.ctx, `
		SELECT request_id, nonce, encrypted_payload_aead
		FROM debug_payload_blobs WHERE org_id = ? AND project_id = ?`,
		th.org.ID, th.project.ID).Scan(&dbgReqID, &dbgNonce, &dbgCT); err != nil {
		t.Fatalf("query debug_payload_blobs after opt-in session: %v", err)
	}
	unsealedDbg, err := th.vault.UnsealProjectBlob(th.ctx, th.org.ID, th.project.ID, vault.RecordKindDebugBlob, dbgReqID, dbgNonce, dbgCT)
	if err != nil {
		t.Fatalf("UnsealProjectBlob: %v", err)
	}
	if !strings.Contains(string(unsealedDbg), "debug trace payload") {
		t.Fatalf("unexpected decrypted debug payload: %q", string(unsealedDbg))
	}

	// Trigger FaultGuard and Breaker callbacks to confirm telemetry gauges/counters receive events.
	th.faultGuard.RecordFault(th.ctx, th.instID, wasm.FaultKindTimeout, "watchdog test")
	th.breaker.RecordOutcome(th.deplID, breaker.AttemptOutcome{Kind: breaker.OutcomeThrottle429, RetryAfterRaw: "30"})

	// Verify /metrics HTTP route requires bearer token and exposes canonical RED/USE metrics.
	unauthMetrics, err := th.client.Get(th.httpSrv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthMetrics.Body.Close()
	if unauthMetrics.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on unauthenticated /metrics, got %d", unauthMetrics.StatusCode)
	}

	mReq, _ := http.NewRequest(http.MethodGet, th.httpSrv.URL+"/metrics", nil)
	mReq.Header.Set("Authorization", "Bearer scrape-token-xyz")
	mResp, err := th.client.Do(mReq)
	if err != nil {
		t.Fatal(err)
	}
	mBodyBytes, _ := io.ReadAll(mResp.Body)
	_ = mResp.Body.Close()
	if mResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on authenticated /metrics, got %d", mResp.StatusCode)
	}
	mBody := string(mBodyBytes)
	for _, metricName := range []string{
		otel.MetricHTTPRequestsTotal,
		otel.MetricProxyOverheadSeconds,
		otel.MetricSSEChunkRelaySeconds,
		otel.MetricUpstreamDurationSeconds,
		otel.MetricDeploymentCircuitState,
		otel.MetricActiveStreams,
		otel.MetricSandboxTrapsTotal,
	} {
		if !strings.Contains(mBody, metricName) {
			t.Fatalf("expected %q in /metrics scrape output", metricName)
		}
	}
}

func TestGatewayTPACF0804DeadBlackholeOTLPCollector2000Requests(t *testing.T) {
	// Blackhole TCP listener simulating unreachable/stalled OTLP collector.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		var held []net.Conn
		defer func() {
			for _, c := range held {
				_ = c.Close()
			}
		}()
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			held = append(held, c)
		}
	}()

	const ringCapBytes = 64 * 1024
	th := setupTelemetryIngressHarness(t, func(cfg *otel.PipelineConfig) {
		cfg.OTLPExporter = nil
		cfg.OTLPEndpoint = "http://" + ln.Addr().String() + "/v1/traces"
		cfg.OTLPAllowPrivateIP = true
		cfg.OTLPAllowedCIDRs = []string{"127.0.0.1/32"}
		cfg.OTLPControlHook = netguard.Control
		cfg.MaxRingBufferBytes = ringCapBytes
		cfg.ExportTimeout = 100 * time.Millisecond
		cfg.FlushInterval = 10 * time.Millisecond
	})
	// Register listener close cleanup after setupTelemetryIngressHarness cleanups so
	// held blackhole sockets close before goleak.VerifyNone runs (and after srv.Close/pipe.Shutdown).
	t.Cleanup(func() {
		_ = ln.Close()
		<-acceptDone
	})

	const totalRequests = 2000
	const workers = 8
	var okCount atomic.Int64
	var failCount atomic.Int64
	jobCh := make(chan int, totalRequests)
	for i := 0; i < totalRequests; i++ {
		jobCh <- i
	}
	close(jobCh)

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobCh {
				body := fmt.Sprintf(`{"model":"acme/chat-1","messages":[{"role":"user","content":"req-%d"}]}`, idx)
				req, _ := http.NewRequest(http.MethodPost, th.httpSrv.URL+"/v1/chat/completions", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+th.vkeyRaw)
				req.Header.Set("Content-Type", "application/json")
				resp, rerr := th.client.Do(req)
				if rerr != nil {
					failCount.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					okCount.Add(1)
				} else {
					failCount.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if okCount.Load() != totalRequests || failCount.Load() != 0 {
		t.Fatalf("expected 2000/2000 200 OK under dead OTLP collector, got ok=%d fail=%d", okCount.Load(), failCount.Load())
	}

	_, peakBytes, droppedSpans := th.pipe.Ring.BufferStats()
	if peakBytes > ringCapBytes || peakBytes > otel.DefaultMaxRingBufferBytes {
		t.Fatalf("OTLP ring buffer exceeded cap: peak=%d cap=%d", peakBytes, ringCapBytes)
	}
	if droppedSpans == 0 || th.pipe.Metrics.DroppedCount("traces") == 0 {
		t.Fatalf("expected tanpopo_telemetry_dropped_total > 0 under blackhole collector, got ring=%d metric=%d",
			droppedSpans, th.pipe.Metrics.DroppedCount("traces"))
	}
	t.Logf("TP-AC-F0804: 2000 requests completed in %v, peak_ring=%d bytes, dropped_spans=%d, metric_dropped=%d",
		elapsed, peakBytes, droppedSpans, th.pipe.Metrics.DroppedCount("traces"))
}
