// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
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
	"github.com/nguywnben/tanpopo/internal/testkit/fakellm"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type memAuditSink struct {
	buf canary.SafeBuffer
}

func (a *memAuditSink) Append(_ context.Context, ev storageport.AuditEvent) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, _ = a.buf.Write(append(line, '\n'))
	return nil
}

type ingressHarness struct {
	t         *testing.T
	ctx       context.Context
	clk       *clock.FakeClock
	store     *sqlite.Store
	snapshots *snapshot.Manager
	catalog   *catalog.Repository
	vault     *vault.Store
	broker    *broker.BlindHostCredentialBroker
	installer *installer.LifecycleService
	vkeys     *vkey.Service
	audit     *memAuditSink
	fake      *fakellm.FakeLLMServer
	server    *ingresshttp.Server
	httpSrv   *httptest.Server
	client    *http.Client

	org      *storageport.Organization
	project  *storageport.Project
	env      *storageport.Environment
	vkeyRaw  string
	vkeyID   string
	secret   string
	instID   string
	deplID   string
	connID   string
	pubPriv  ed25519.PrivateKey
	pubPub   ed25519.PublicKey
	pluginsD string
	workDir  string
}

func setupIngressHarness(t *testing.T, maxBodyBytes int64) *ingressHarness {
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
		PolicyResolver: catRepo, // DEC-020 explicit PolicyResolver
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
		Slug:           "ingress-org",
		DisplayName:    "Ingress Org",
		MinPluginTrust: storageport.MinTrustVerifiedOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := st.Tenancy().CreateProject(ctx, org.ID, storageport.CreateProjectParams{
		Slug:        "ingress-proj",
		DisplayName: "Ingress Proj",
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

	// Pack & install Tier-1 Declarative adapter for AcmeAI without modifying Core.
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
		Signature: installer.ProvenanceSignature{
			PublisherOrRootSig: sig,
		},
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
	conn, err = catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateValidating, conn.Version, "start validation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catRepo.TransitionConnectionState(ctx, org.ID, conn.ID, catalog.ConnStateActive, conn.Version, "test activation"); err != nil {
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
		Name:                 "test-live-key",
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
		Snapshots:            mgr,
		Broker:               brk,
		PluginsDir:           pluginsDir,
		Audit:                audit,
		NowMs:                nowFn,
		MaxRequestBodyBytes:  maxBodyBytes,
		SSEHeartbeatInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	client := fake.Client()
	t.Cleanup(client.CloseIdleConnections)

	return &ingressHarness{
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
		workDir:   tmp,
	}
}

func (h *ingressHarness) doChatReq(t *testing.T, method, targetPath string, body []byte, hdrs map[string]string, cookies []*http.Cookie) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(h.ctx, method, h.httpSrv.URL+targetPath, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if _, hasAuth := hdrs["Authorization"]; !hasAuth {
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	}
	for k, v := range hdrs {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func assertErrorCode(t *testing.T, resp *http.Response, body []byte, wantStatus int, wantCode string) {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("status=%d want %d; body=%s", resp.StatusCode, wantStatus, string(body))
	}
	if reqID := resp.Header.Get("X-Tanpopo-Request-Id"); !strings.HasPrefix(reqID, "req_") {
		t.Fatalf("missing or malformed X-Tanpopo-Request-Id: %q", reqID)
	}
	var env struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode canonical error body: %v (body=%s)", err, string(body))
	}
	if env.Error.Code != wantCode {
		t.Fatalf("error.code=%q want %q (body=%s)", env.Error.Code, wantCode, string(body))
	}
	if env.Error.RequestID != resp.Header.Get("X-Tanpopo-Request-Id") {
		t.Fatalf("envelope request_id=%q != header %q", env.Error.RequestID, resp.Header.Get("X-Tanpopo-Request-Id"))
	}
}

func TestNewServerFailClosed(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })

	if _, err := ingresshttp.NewServer(ingresshttp.ServerConfig{}); !errors.Is(err, ingresshttp.ErrInvalidServerConfig) {
		t.Fatalf("expected ErrInvalidServerConfig for missing snapshot manager, got %v", err)
	}
}

func TestIngressEnvelopeHeadersAndProtocolUpgrades(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// Method validation & auto-minted CT-06 headers
	resp, body := h.doChatReq(t, http.MethodGet, "/v1/chat/completions", nil, nil, nil)
	assertErrorCode(t, resp, body, http.StatusMethodNotAllowed, "method_not_allowed")
	if tp := resp.Header.Get("Traceparent"); !strings.HasPrefix(tp, "00-") || len(tp) != 55 {
		t.Fatalf("expected minted W3C traceparent, got %q", tp)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing X-Content-Type-Options: nosniff")
	}

	// TP-FR-0106: WebSocket / WebRTC Upgrade rejection + caller correlation preservation
	customReqID := "req_client_corr_01"
	customTP := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	resp, body = h.doChatReq(t, http.MethodGet, "/v1/chat/completions", nil, map[string]string{
		"Upgrade":               "websocket",
		"Connection":            "Upgrade",
		"X-Tanpopo-Request-Id":  customReqID,
		"Traceparent":           customTP,
	}, nil)
	assertErrorCode(t, resp, body, http.StatusBadRequest, "unsupported_protocol_upgrade")
	if resp.Header.Get("X-Tanpopo-Request-Id") != customReqID || resp.Header.Get("Traceparent") != customTP {
		t.Fatalf("expected caller CT-06 correlation headers preserved, got id=%q tp=%q",
			resp.Header.Get("X-Tanpopo-Request-Id"), resp.Header.Get("Traceparent"))
	}
}

func TestChatCompletionsAuthAndPolicyGuards(t *testing.T) {
	h := setupIngressHarness(t, 512)
	validBody := []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`)

	// AUTH-07: Admin session cookies prohibited on /v1/* even with valid Bearer key
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", validBody, nil, []*http.Cookie{
		{Name: "tanpopo_session", Value: "sess_admin_secret"},
	})
	assertErrorCode(t, resp, body, http.StatusUnauthorized, "invalid_credential_plane")

	// Missing Bearer header -> 401 invalid_virtual_key
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", validBody, map[string]string{
		"Authorization": "",
	}, nil)
	assertErrorCode(t, resp, body, http.StatusUnauthorized, "invalid_virtual_key")

	// Body exceeding 512-byte cap -> 413 payload_too_large
	oversized := append([]byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"`), bytes.Repeat([]byte("a"), 700)...)
	oversized = append(oversized, []byte(`"}]}`)...)
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", oversized, nil, nil)
	assertErrorCode(t, resp, body, http.StatusRequestEntityTooLarge, "payload_too_large")

	// Cross-environment header mismatch -> 403 scope_or_plane_denied
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", validBody, map[string]string{
		"X-Tanpopo-Env-Id": "env_other",
	}, nil)
	assertErrorCode(t, resp, body, http.StatusForbidden, "scope_or_plane_denied")

	// Scoped MCP key -> 403 scope_or_plane_denied
	mcpKey, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:         h.org.ID,
		ProjectID:     h.project.ID,
		EnvID:         h.env.ID,
		Name:          "mcp-only",
		AllowedPlanes: "MCP",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", validBody, map[string]string{
		"Authorization": "Bearer " + mcpKey.SecretKey,
	}, nil)
	assertErrorCode(t, resp, body, http.StatusForbidden, "scope_or_plane_denied")

	// Omitted reasoning capability fails closed -> 400 capability_not_supported
	_, err = h.store.RawDB().ExecContext(h.ctx, `
		UPDATE model_deployments
		SET capability_overrides_json = '{}', version = version + 1
		WHERE id = ?
	`, h.deplID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'model_deployment', ?, 'UPSERT', 0, 1760000201000)
	`, h.org.ID, h.deplID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	reasoningBody := []byte(`{"model":"acme/chat-1","reasoning_effort":"high","messages":[{"role":"user","content":"think"}]}`)
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", reasoningBody, nil, nil)
	assertErrorCode(t, resp, body, http.StatusBadRequest, "capability_not_supported")

	// Quarantine installation -> fail closed with 503 no_compliant_deployment_available (since VisibleToProject fences quarantined)
	if err := h.installer.QuarantineInstallation(h.ctx, h.instID, "security fault", "prc_op_1"); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", validBody, nil, nil)
	assertErrorCode(t, resp, body, http.StatusServiceUnavailable, "no_compliant_deployment_available")

	// DEC-019: Stale snapshot beyond MaxStaleness -> 503 snapshot_not_ready
	h.clk.Advance(time.Hour)
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", validBody, nil, nil)
	assertErrorCode(t, resp, body, http.StatusServiceUnavailable, "snapshot_not_ready")
}

func TestChatCompletionsUpstreamErrorNormalizationNoSecretLeak(t *testing.T) {
	h := setupIngressHarness(t, 0)
	h.fake.EnqueueFault("echo_auth_header_in_error=true,http_status=401")

	reqBody := []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hello"}]}`)
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", reqBody, nil, nil)
	assertErrorCode(t, resp, body, http.StatusUnauthorized, "upstream_auth_failed")

	// Ensure canary secret NEVER leaks in HTTP response or audit logs, while FakeLLM received it outbound.
	reqs := h.fake.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(reqs))
	}
	if got := reqs[0].Headers.Get("Authorization"); got != "Bearer "+h.secret {
		t.Fatalf("expected unsealed canary secret at FakeLLMServer, got %q", got)
	}
	if strings.Contains(reqs[0].Headers.Get("Authorization"), "vk_tp_") {
		t.Fatal("inbound virtual key leaked to upstream provider")
	}

	sc := canary.NewScanner(h.secret, h.vkeyRaw)
	sc.AssertClean(t,
		canary.NamedData{Source: "http_error_body", Data: body},
	)
	sc.AssertCleanBuffer(t, "audit_events", &h.audit.buf)
}

func TestChatCompletionsStreamingPreFirstByteTimeoutAndSecurityHardening(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// 1. Streaming pre-first-byte IdleChunkTimeout -> 504 upstream_timeout
	stallSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(stallSrv.Close)

	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?
	`, stallSrv.URL, h.connID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE model_deployments SET first_byte_timeout_ms = 500, idle_chunk_timeout_ms = 50, version = version + 1 WHERE id = ?
	`, h.deplID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'model_deployment', ?, 'UPSERT', 0, 1760000202000)
	`, h.org.ID, h.deplID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"stall"}]}`), nil, nil)
	assertErrorCode(t, resp, body, http.StatusGatewayTimeout, "upstream_timeout")

	// 2. Permission re-approval updates runtime cache key and evicts old runtime cleanly;
	// also test Fail-Closed when RequireTestedCaps=true and capability is only DECLARED.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE model_deployments SET capability_overrides_json = '{"streaming":"DECLARED"}', version = version + 1 WHERE id = ?
	`, h.deplID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE environments SET require_tested_caps = 1, version = version + 1 WHERE id = ?
	`, h.env.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'environment', ?, 'UPSERT', 0, 1760000203000)
	`, h.org.ID, h.env.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"untested"}]}`), nil, nil)
	assertErrorCode(t, resp, body, http.StatusBadRequest, "capability_not_supported")
}

func TestTier2WasmManifestMissingSHA256AndYAMLBoundsFailClosed(t *testing.T) {
	h := setupIngressHarness(t, 0)

	var pkgID, sigFP string
	if err := h.store.RawDB().QueryRowContext(h.ctx, `
		SELECT ei.package_id, COALESCE(ev.signing_key_fingerprint, '')
		FROM extension_installations ei
		JOIN extension_versions ev ON ev.id = ei.active_version_id
		WHERE ei.id = ?
	`, h.instID).Scan(&pkgID, &sigFP); err != nil {
		t.Fatal(err)
	}

	// Stage a Tier-2 manifest directly on disk without runtime.sha256 -> server must refuse to execute it.
	fakeVer := "ver_bad_sha"
	verDir := filepath.Join(h.pluginsD, fakeVer, "dist")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verDir, "adapter.wasm"), []byte("\x00asm\x01\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	badManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/bad-wasm"
  name: "Bad Wasm"
  version: "1.0.0"
runtime:
  tier: "sandboxed_wasm"
  entrypoint: "dist/adapter.wasm"
`
	if err := os.WriteFile(filepath.Join(h.pluginsD, fakeVer, "manifest.yaml"), []byte(badManifest), 0o600); err != nil {
		t.Fatal(err)
	}

	// Test YAML alias bomb rejection cleanly via manifest.yaml on disk.
	bombYAML := "---\na: &x [1,1,1,1]\nb: &y [*x,*x,*x,*x,*x,*x,*x,*x,*x,*x]\nc: [*y,*y,*y,*y,*y,*y,*y,*y,*y,*y]\n"
	_ = os.MkdirAll(filepath.Join(h.pluginsD, "ver_bomb_test"), 0o755)
	if err := os.WriteFile(filepath.Join(h.pluginsD, "ver_bomb_test", "manifest.yaml"), []byte(bombYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO extension_versions (id, package_id, semver, contract_version, execution_tier, bundle_sha256, signature_verified, signing_key_fingerprint, source_uri, manifest_json, created_at_ms)
		VALUES ('ver_bomb_test', ?, '9.9.9', '0.1', 'WASM_SANDBOX_T2', '0000000000000000000000000000000000000000000000000000000000000001', 1, ?, './bomb.tpkg', '{}', 1760000205000)
	`, pkgID, sigFP); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE extension_installations SET active_version_id = 'ver_bomb_test', version = version + 1 WHERE id = ?
	`, h.instID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'extension_installation', ?, 'UPSERT', 0, 1760000206000)
	`, h.org.ID, h.instID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"bomb"}]}`), nil, nil)
	assertErrorCode(t, resp, body, http.StatusBadGateway, "extension_execution_fault")

	// Also verify missing runtime.sha256 fails closed.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO extension_versions (id, package_id, semver, contract_version, execution_tier, bundle_sha256, signature_verified, signing_key_fingerprint, source_uri, manifest_json, created_at_ms)
		VALUES ('ver_bad_sha', ?, '9.9.10', '0.1', 'WASM_SANDBOX_T2', '0000000000000000000000000000000000000000000000000000000000000002', 1, ?, './nosha.tpkg', '{}', 1760000207000)
	`, pkgID, sigFP); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE extension_installations SET active_version_id = 'ver_bad_sha', version = version + 1 WHERE id = ?
	`, h.instID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'extension_installation', ?, 'UPSERT', 0, 1760000208000)
	`, h.org.ID, h.instID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"nosha"}]}`), nil, nil)
	assertErrorCode(t, resp, body, http.StatusBadGateway, "extension_execution_fault")
}

func TestChatCompletionsRemediationGuards(t *testing.T) {
	h := setupIngressHarness(t, 0) // Default 32 MiB body cap

	// 1. Empty AllowedRoutes ([]) must fail closed with 403 scope_or_plane_denied (never default-allow).
	emptyRouteKey, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:         h.org.ID,
		ProjectID:     h.project.ID,
		EnvID:         h.env.ID,
		Name:          "empty-routes",
		AllowedPlanes: "PROVIDER",
		AllowedRoutes: []string{},
		AllowedModels: []string{"*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE virtual_keys SET allowed_routes_json = '[]', version = version + 1 WHERE id = ?`, emptyRouteKey.Summary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'virtual_key', ?, 'UPSERT', 0, 1760000210000)`, h.org.ID, emptyRouteKey.Summary.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), map[string]string{
		"Authorization": "Bearer " + emptyRouteKey.SecretKey,
	}, nil)
	assertErrorCode(t, resp, body, http.StatusForbidden, "scope_or_plane_denied")

	// Empty AllowedModels ([]) must fail closed with 403 scope_or_plane_denied.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE virtual_keys SET allowed_routes_json = '["*"]', allowed_models_json = '[]', version = version + 1 WHERE id = ?`, emptyRouteKey.Summary.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'virtual_key', ?, 'UPSERT', 0, 1760000210100)`, h.org.ID, emptyRouteKey.Summary.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), map[string]string{
		"Authorization": "Bearer " + emptyRouteKey.SecretKey,
	}, nil)
	assertErrorCode(t, resp, body, http.StatusForbidden, "scope_or_plane_denied")

	// 2. Omitted multimodal capabilities (vision, audio_input, document_input) fail closed.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE model_deployments
		SET capability_overrides_json = '{}', version = version + 1
		WHERE id = ?
	`, h.deplID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'model_deployment', ?, 'UPSERT', 0, 1760000210200)`, h.org.ID, h.deplID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{
		"vision":         `{"model":"acme/chat-1","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
		"audio_input":    `{"model":"acme/chat-1","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]}]}`,
		"document_input": `{"model":"acme/chat-1","messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"AAAA"}}]}]}`,
	} {
		resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(payload), nil, nil)
		assertErrorCode(t, resp, body, http.StatusBadRequest, "capability_not_supported")
		_ = name
	}

	// Re-enable vision for > 4 MiB inline blob_ref offload & splice round-trip!
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		UPDATE model_deployments
		SET capability_overrides_json = '{"vision":"VERIFIED"}', version = version + 1
		WHERE id = ?
	`, h.deplID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'model_deployment', ?, 'UPSERT', 0, 1760000210300)`, h.org.ID, h.deplID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// 3. > 4 MiB inline data URI offloaded to randomized blob_ref:// during TransformRequest and spliced back verbatim outbound,
	// while a spoofed predictable "blob_ref://<reqID>/0" string in message text is NOT substituted.
	h.fake.ResetRequests()
	bigB64 := strings.Repeat("A", (4<<20)+128)
	dataURI := "data:image/png;base64," + bigB64
	spoofReqID := "req_spoof_blob_01"
	spoofLiteral := "blob_ref://" + spoofReqID + "/0"
	blobReq := fmt.Sprintf(`{"model":"acme/chat-1","messages":[{"role":"user","content":[{"type":"text","text":%q},{"type":"image_url","image_url":{"url":%q}}]}]}`, spoofLiteral, dataURI)
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(blobReq), map[string]string{
		"X-Tanpopo-Request-Id": spoofReqID,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for >4MiB inline blob offload/splice, got %d body=%s", resp.StatusCode, string(body))
	}
	reqs := h.fake.Requests()
	if len(reqs) != 1 || !strings.Contains(string(reqs[0].Body), dataURI) || !strings.Contains(string(reqs[0].Body), spoofLiteral) {
		t.Fatalf("expected spliced outbound body to contain original >4MiB data URI and untouched spoof literal %q", spoofLiteral)
	}

	// 4. Tampered on-disk manifest.yaml diverging from DB manifest_json must fail closed (verifyManifestParity).
	var verID string
	if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT active_version_id FROM extension_installations WHERE id = ?`, h.instID).Scan(&verID); err != nil {
		t.Fatal(err)
	}
	diskManifestPath := filepath.Join(h.pluginsD, verID, "manifest.yaml")
	origDiskBytes, err := os.ReadFile(diskManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(origDiskBytes), `"127.0.0.1"`, `"evil.attacker.invalid"`, 1)
	if err := os.WriteFile(diskManifestPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	srv2, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:  h.snapshots,
		Broker:     h.broker,
		PluginsDir: h.pluginsD,
		Audit:      h.audit,
		NowMs:      func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv2.Close(context.Background()) })
	tamperHTTP := httptest.NewServer(srv2)
	t.Cleanup(tamperHTTP.Close)

	req, err := http.NewRequestWithContext(h.ctx, http.MethodPost, tamperHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"tamper"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	tResp, err := tamperHTTP.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	tBody, _ := io.ReadAll(tResp.Body)
	_ = tResp.Body.Close()
	assertErrorCode(t, tResp, tBody, http.StatusBadGateway, "extension_execution_fault")

	// 4b. Symlinked on-disk manifest.yaml must fail closed rather than falling back to DB manifest_json.
	symTarget := filepath.Join(h.workDir, "escaped-manifest.yaml")
	if err := os.WriteFile(symTarget, origDiskBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(diskManifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(symTarget, diskManifestPath); err != nil {
		t.Fatal(err)
	}
	srvSym, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:  h.snapshots,
		Broker:     h.broker,
		PluginsDir: h.pluginsD,
		Audit:      h.audit,
		NowMs:      func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srvSym.Close(context.Background()) })
	symHTTP := httptest.NewServer(srvSym)
	t.Cleanup(symHTTP.Close)

	sReq, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, symHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"symlink"}]}`))
	sReq.Header.Set("Content-Type", "application/json")
	sReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	sResp, err := symHTTP.Client().Do(sReq)
	if err != nil {
		t.Fatal(err)
	}
	sBody, _ := io.ReadAll(sResp.Body)
	_ = sResp.Body.Close()
	assertErrorCode(t, sResp, sBody, http.StatusBadGateway, "extension_execution_fault")

	// 5. Quarantined installation inside FaultGuard maps to 503 extension_quarantined (retryable: false).
	_ = os.Remove(diskManifestPath)
	_ = os.WriteFile(diskManifestPath, origDiskBytes, 0o600)
	fg := wasm.NewFaultGuard(wasm.FaultGuardConfig{MaxFaultsInWindow: 1})
	fg.RecordFault(h.ctx, h.instID, wasm.FaultKindTrap, "trip quarantine 1")
	fg.RecordFault(h.ctx, h.instID, wasm.FaultKindTrap, "trip quarantine 2")
	if !fg.IsQuarantined(h.instID) {
		t.Fatalf("expected FaultGuard to quarantine %s after crossing MaxFaultsInWindow", h.instID)
	}
	srv3, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:  h.snapshots,
		Broker:     h.broker,
		FaultGuard: fg,
		PluginsDir: h.pluginsD,
		Audit:      h.audit,
		NowMs:      func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv3.Close(context.Background()) })
	quarHTTP := httptest.NewServer(srv3)
	t.Cleanup(quarHTTP.Close)

	qReq, err := http.NewRequestWithContext(h.ctx, http.MethodPost, quarHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"quar"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	qReq.Header.Set("Content-Type", "application/json")
	qReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	qResp, err := quarHTTP.Client().Do(qReq)
	if err != nil {
		t.Fatal(err)
	}
	qBody, _ := io.ReadAll(qResp.Body)
	_ = qResp.Body.Close()
	assertErrorCode(t, qResp, qBody, http.StatusServiceUnavailable, "extension_quarantined")

	// 6. Calling ServeHTTP after Server.Close refuses runtime caching cleanly without leaking runtimes.
	_ = srv3.Close(context.Background())
	closedHTTP := httptest.NewServer(srv3)
	t.Cleanup(closedHTTP.Close)
	cReq, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, closedHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"closed"}]}`))
	cReq.Header.Set("Content-Type", "application/json")
	cReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	cResp, err := closedHTTP.Client().Do(cReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = cResp.Body.Close()
	if cResp.StatusCode == http.StatusOK {
		t.Fatalf("expected non-200 response from closed Server, got 200")
	}
}

type sqliteAuditSink struct {
	mu  sync.Mutex
	db  *sql.DB
	seq int64
}

func newSQLiteAuditSink(t *testing.T, db *sql.DB) *sqliteAuditSink {
	t.Helper()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_events (
			id            VARCHAR(40) NOT NULL,
			created_at_ms BIGINT NOT NULL,
			org_id        VARCHAR(40),
			chain_key     VARCHAR(40) NOT NULL,
			seq_no        BIGINT NOT NULL CHECK (seq_no >= 1),
			actor_kind    VARCHAR(24) NOT NULL,
			actor_id      VARCHAR(64) NOT NULL,
			entry_point   VARCHAR(24) NOT NULL
			              CHECK (entry_point IN ('admin_api', 'cli', 'gitops_boot', 'system_reaper', 'data_plane_guard')),
			event_type    VARCHAR(64) NOT NULL,
			target_type   VARCHAR(32) NOT NULL,
			target_id     VARCHAR(64) NOT NULL,
			request_id    VARCHAR(40),
			metadata_json TEXT NOT NULL,
			prev_hash     CHAR(64) NOT NULL,
			entry_hash    CHAR(64) NOT NULL,
			CHECK ((org_id IS NULL AND chain_key = 'SYSTEM') OR (org_id IS NOT NULL AND chain_key = org_id)),
			PRIMARY KEY (chain_key, seq_no, created_at_ms),
			UNIQUE (chain_key, seq_no),
			UNIQUE (id, created_at_ms)
		)`); err != nil {
		t.Fatalf("create audit_events table: %v", err)
	}
	return &sqliteAuditSink{db: db}
}

func (s *sqliteAuditSink) Append(ctx context.Context, ev storageport.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	chainKey := "SYSTEM"
	var orgArg any
	if ev.OrgID != nil && *ev.OrgID != "" {
		chainKey = *ev.OrgID
		orgArg = *ev.OrgID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, created_at_ms, org_id, chain_key, seq_no,
			actor_kind, actor_id, entry_point, event_type,
			target_type, target_id, request_id, metadata_json,
			prev_hash, entry_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.CreatedAtMs, orgArg, chainKey, s.seq,
		ev.ActorKind, ev.ActorID, ev.EntryPoint, ev.EventType,
		ev.TargetType, ev.TargetID, ev.RequestID, ev.MetadataJSON,
		strings.Repeat("0", 64), strings.Repeat("a", 64),
	)
	return err
}

func TestChatCompletionsAuditConstraintHostIntersectHeartbeatAndPlaneChecks(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// 1. Non-vk_tp_ Bearer token on /v1/chat/completions -> 401 invalid_credential_plane (AUTH-07).
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), map[string]string{
		"Authorization": "Bearer adm_tok_not_virtual_key",
	}, nil)
	assertErrorCode(t, resp, body, http.StatusUnauthorized, "invalid_credential_plane")

	// 2. Empty AllowedPlanes in DB must fail closed -> 403 scope_or_plane_denied.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE virtual_keys SET allowed_planes = '', version = version + 1 WHERE id = ?`, h.vkeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'virtual_key', ?, 'UPSERT', 0, 1760000220000)`, h.org.ID, h.vkeyID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp, body = h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), nil, nil)
	assertErrorCode(t, resp, body, http.StatusForbidden, "scope_or_plane_denied")

	// Restore AllowedPlanes for subsequent checks.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE virtual_keys SET allowed_planes = 'PROVIDER', version = version + 1 WHERE id = ?`, h.vkeyID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'virtual_key', ?, 'UPSERT', 0, 1760000220100)`, h.org.ID, h.vkeyID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// 3. Real SQLite audit_events CHECK constraint on BlindBroker credential unseal & SSEHeartbeatInterval `: ping\n\n` emission.
	sqlAudit := newSQLiteAuditSink(t, h.store.RawDB())
	brkSQL, err := broker.NewBlindBroker(broker.Config{
		Resolver:       h.catalog,
		PolicyResolver: h.catalog,
		Vault:          h.vault,
		UnsealCap:      vault.MintUnsealCapabilityForBroker(),
		Audit:          sqlAudit,
		NowMs:          func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	srvSQL, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:            h.snapshots,
		Broker:               brkSQL,
		PluginsDir:           h.pluginsD,
		Audit:                sqlAudit,
		SSEHeartbeatInterval: 20 * time.Millisecond,
		NowMs:                func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srvSQL.Close(context.Background()) })
	sqlHTTP := httptest.NewServer(srvSQL)
	t.Cleanup(sqlHTTP.Close)

	// 3a. Sync call passes real SQLite audit_events CHECK (entry_point = 'data_plane_guard').
	syncReq, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, sqlHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"audit check"}]}`))
	syncReq.Header.Set("Content-Type", "application/json")
	syncReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	sResp, err := sqlHTTP.Client().Do(syncReq)
	if err != nil {
		t.Fatal(err)
	}
	sBody, _ := io.ReadAll(sResp.Body)
	_ = sResp.Body.Close()
	if sResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with SQLite audit_events CHECK constraint, got %d body=%s", sResp.StatusCode, string(sBody))
	}
	var auditCount int
	if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT COUNT(*) FROM audit_events WHERE entry_point = 'data_plane_guard'`).Scan(&auditCount); err != nil || auditCount < 1 {
		t.Fatalf("expected >= 1 SQLite audit_events row with entry_point='data_plane_guard', count=%d err=%v", auditCount, err)
	}

	// 3b. Streaming call with SSEHeartbeatInterval=20ms emits `: ping\n\n` while upstream pauses cleanly between chunks.
	hbUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"acme-upstream\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		if fl != nil {
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(75 * time.Millisecond):
		}
		_, _ = w.Write([]byte("data: {\"id\":\"c1\",\"model\":\"acme-upstream\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		if fl != nil {
			fl.Flush()
		}
	}))
	t.Cleanup(hbUpstream.Close)
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?`, hbUpstream.URL, h.connID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_connection', ?, 'UPSERT', 0, 1760000220200)`, h.org.ID, h.connID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	hbReq, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, sqlHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"hb"}]}`))
	hbReq.Header.Set("Content-Type", "application/json")
	hbReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	hbResp, err := sqlHTTP.Client().Do(hbReq)
	if err != nil {
		t.Fatal(err)
	}
	hbBody, _ := io.ReadAll(hbResp.Body)
	_ = hbResp.Body.Close()
	if hbResp.StatusCode != http.StatusOK || !strings.Contains(string(hbBody), ": ping\n\n") || !strings.Contains(string(hbBody), "data: [DONE]\n\n") {
		t.Fatalf("expected 200 SSE with : ping heartbeat and [DONE], status=%d body=%q", hbResp.StatusCode, string(hbBody))
	}

	// 4. Adapter allowed_hosts_json narrowing against broader approved_permissions_json must intersect & fail closed.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_adapters SET allowed_hosts_json = '["api.narrowed-only.invalid"]', version = version + 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_adapter', 'adp_narrow', 'UPSERT', 0, 1760000220300)`, h.org.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	narrowReq, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, sqlHTTP.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"narrow"}]}`))
	narrowReq.Header.Set("Content-Type", "application/json")
	narrowReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	nResp, err := sqlHTTP.Client().Do(narrowReq)
	if err != nil {
		t.Fatal(err)
	}
	nBody, _ := io.ReadAll(nResp.Body)
	_ = nResp.Body.Close()
	if nResp.StatusCode == http.StatusOK {
		t.Fatalf("expected fail-closed host rejection when adapter allowed_hosts narrows out 127.0.0.1, got 200")
	}
	assertErrorCode(t, nResp, nBody, http.StatusForbidden, "egress_host_denied")
}

func TestChatCompletionsEmptyPermissionsFailClosedAndTraceCap(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// 1. Over-40-byte client X-Tanpopo-Request-Id is replaced by a canonical <=40-byte minted req_<26> ID.
	longReqID := strings.Repeat("a", 41)
	resp, _ := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"trace"}]}`), map[string]string{
		"X-Tanpopo-Request-Id": longReqID,
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	gotID := resp.Header.Get("X-Tanpopo-Request-Id")
	if gotID == longReqID || len(gotID) > ingresshttp.MaxTraceHeaderLength || !strings.HasPrefix(gotID, "req_") {
		t.Fatalf("expected fresh minted req_ ID <= %d chars when client sent %d chars, got %q (len=%d)",
			ingresshttp.MaxTraceHeaderLength, len(longReqID), gotID, len(gotID))
	}

	// 2. Empty approved_permissions_json.allowedHosts must fail closed (403 egress_host_denied), never falling back to adapter/connection hosts.
	emptyPermJSON := `{"allowedHosts":[],"allowPrivateIp":true,"requireTls":false,"secretSlots":["api_key"]}`
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE extension_installations SET approved_permissions_json = ? WHERE id = ?`, emptyPermJSON, h.instID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'extension_installation', ?, 'UPSERT', 0, 1760000230000)`, h.org.ID, h.instID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	denyResp, denyBody := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"blocked"}]}`), nil, nil)
	assertErrorCode(t, denyResp, denyBody, http.StatusForbidden, "egress_host_denied")
}

func TestChatCompletionsUnmeteredOmitsUsageAndMultiFieldMultimodal(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// 1. Multi-field content part mixing image_url type with input_audio object must still fail audio_input capability check.
	// Allow vision on model while leaving audio_input unsupported.
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE logical_models SET base_capabilities_json = '{"streaming":"VERIFIED","tool_calling":"VERIFIED","vision":"VERIFIED"}', version = version + 1 WHERE canonical_slug = 'acme/chat-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'logical_model', 'lm_cap', 'UPSERT', 0, 1760000230100)`, h.org.ID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	mixedPayload := `{"model":"acme/chat-1","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"},"input_audio":{"data":"AAAA","format":"wav"}}]}]}`
	mResp, mBody := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(mixedPayload), nil, nil)
	assertErrorCode(t, mResp, mBody, http.StatusBadRequest, "capability_not_supported")
	if !strings.Contains(string(mBody), "audio_input") {
		t.Fatalf("expected capability_not_supported mentioning audio_input, got %s", string(mBody))
	}

	// 2. Upstream omitting usage emits canonical OpenAI JSON with NO leaked internal "usage" object (e.g. accuracy/input_tokens).
	noUsageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-no-usage","model":"acme-gpt-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(noUsageSrv.Close)
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?`, noUsageSrv.URL, h.connID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_connection', ?, 'UPSERT', 0, 1760000230200)`, h.org.ID, h.connID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	uResp, uBody := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"no usage"}]}`), nil, nil)
	if uResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", uResp.StatusCode, string(uBody))
	}
	if uResp.Header.Get("X-Tanpopo-Usage-Accuracy") != "UNMETERED_FLAGGED" {
		t.Fatalf("expected X-Tanpopo-Usage-Accuracy: UNMETERED_FLAGGED, got %q", uResp.Header.Get("X-Tanpopo-Usage-Accuracy"))
	}
	var parsed map[string]any
	if err := json.Unmarshal(uBody, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, hasUsage := parsed["usage"]; hasUsage {
		t.Fatalf("expected unmetered zero-usage response to omit 'usage', got body=%s", string(uBody))
	}
}

func TestChatCompletionsPreFirstByteRevocationReturns401(t *testing.T) {
	h := setupIngressHarness(t, 0)

	arrived := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once

	slowUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-arrived:
		default:
			close(arrived)
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(slowUp.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })

	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?`, slowUp.URL, h.connID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES (?, 'provider_connection', ?, 'UPSERT', 0, 1760000230300)`, h.org.ID, h.connID); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	type result struct {
		resp *http.Response
		body []byte
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		req, _ := http.NewRequestWithContext(h.ctx, http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"revoke pre-first-byte"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		resp, err := h.client.Do(req)
		if err != nil {
			resCh <- result{err: err}
			return
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resCh <- result{resp: resp, body: body}
	}()

	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for upstream dial before revocation")
	}
	// Trigger immediate pre-first-byte AUTH-06 revocation.
	h.snapshots.Streams().AbortKey(h.vkeyID)

	res := <-resCh
	if res.err != nil {
		t.Fatalf("client error: %v", res.err)
	}
	assertErrorCode(t, res.resp, res.body, http.StatusUnauthorized, "invalid_virtual_key")
}

func TestServerCloseConcurrentRuntimeLoadRaceClean(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// Launch concurrent requests racing with Server.Close (run under -race + goleak).
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", strings.NewReader(`{"model":"acme/chat-1","messages":[{"role":"user","content":"close race"}]}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
			resp, err := h.client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}
	time.Sleep(2 * time.Millisecond)
	if err := h.server.Close(context.Background()); err != nil {
		t.Fatalf("Server.Close error: %v", err)
	}
	wg.Wait()
}

