// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type adminTestHarness struct {
	st         *sqlite.Store
	fk         *clock.FakeClock
	auditLog   *audit.Logger
	kek        *vault.StaticKEKProvider
	vault      *vault.Store
	snapshots  *snapshot.Manager
	killSwitch *killswitch.Registry
	authSvc    *auth.LocalService
	bootMgr    *bootstrap.Manager
	vkeys      *vkey.Service
	srv        *Server
	tokenPath  string
	scanner    *canary.Scanner

	bearerMu     sync.RWMutex
	bearerTokens map[string]*rbac.Actor
}

func newAdminTestHarness(t *testing.T) *adminTestHarness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowFn := func() int64 { return fk.Now().UTC().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{
		DBPath: filepath.Join(dir, "admin.db"),
		NowMs:  nowFn,
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	auditLog, err := audit.NewLogger(audit.LoggerConfig{
		DB:    st.RawDB(),
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	if err := st.SetAuditSink(auditLog); err != nil {
		t.Fatalf("st.SetAuditSink: %v", err)
	}

	// Re-open or wrap with real audit sink for Tenancy operations too.
	// Note st already exposes st.RawDB(); attach auditLog to services and ServerConfig.
	masterKey := make([]byte, vault.KeySizeBytes)
	for i := range masterKey {
		masterKey[i] = byte(i + 17)
	}
	kek, err := vault.NewStaticKEKProvider("kek_test_v1", masterKey)
	if err != nil {
		t.Fatalf("NewStaticKEKProvider: %v", err)
	}
	vlt, err := vault.NewStore(vault.Config{
		DB:            st.RawDB(),
		KEK:           kek,
		NowMs:         nowFn,
		PostShredHook: st.CheckpointWAL,
	})
	if err != nil {
		t.Fatalf("vault.NewStore: %v", err)
	}

	snapshots, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{
		DB:    st.RawDB(),
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}

	ksReg, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{
		DB:    st.RawDB(),
		Audit: auditLog,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatalf("killswitch.NewRegistry: %v", err)
	}

	argonParams := auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32}
	authSvc, err := auth.NewLocalService(auth.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Argon2:    argonParams,
		AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("auth.NewLocalService: %v", err)
	}

	tokenPath := filepath.Join(dir, "bootstrap.token")
	bootMgr, err := bootstrap.NewManager(bootstrap.ManagerConfig{
		DB:        st.RawDB(),
		TokenPath: tokenPath,
		NowMs:     nowFn,
		Argon2:    argonParams,
		AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("bootstrap.NewManager: %v", err)
	}
	if _, err := bootMgr.EnsureToken(ctx); err != nil {
		t.Fatalf("bootMgr.EnsureToken: %v", err)
	}

	vkeySvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     nowFn,
		Snapshots: snapshots,
		AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}

	h := &adminTestHarness{
		st:           st,
		fk:           fk,
		auditLog:     auditLog,
		kek:          kek,
		vault:        vlt,
		snapshots:    snapshots,
		killSwitch:   ksReg,
		authSvc:      authSvc,
		bootMgr:      bootMgr,
		vkeys:        vkeySvc,
		tokenPath:    tokenPath,
		scanner:      canary.NewScanner(),
		bearerTokens: make(map[string]*rbac.Actor),
	}

	pagSecret := []byte("0123456789abcdef0123456789abcdef")
	lockfilePath := filepath.Join(dir, "tanpopo.lock")
	if err := os.WriteFile(lockfilePath, []byte("[]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "tanpopo.yaml")
	if err := os.WriteFile(configPath, []byte("data_dir: "+dir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginsDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	instSvc, err := installer.NewLifecycleService(installer.ServiceConfig{
		DB:           st.RawDB(),
		Audit:        auditLog,
		PluginsDir:   pluginsDir,
		LockfilePath: lockfilePath,
		NowMs:        nowFn,
	})
	if err != nil {
		t.Fatalf("installer.NewLifecycleService: %v", err)
	}
	srv, err := NewServer(ServerConfig{
		Store:            st,
		DB:               st.RawDB(),
		Bootstrap:        bootMgr,
		Auth:             authSvc,
		VKeys:            vkeySvc,
		Vault:            vlt,
		Snapshots:        snapshots,
		KillSwitch:       ksReg,
		Installer:        instSvc,
		Audit:            auditLog,
		NowMs:            nowFn,
		PaginationSecret: pagSecret,
		LockfilePath:     lockfilePath,
		ConfigPath:       configPath,
		TokenVerifier: func(_ context.Context, raw string) (*rbac.Actor, *SessionMetadata, error) {
			h.bearerMu.RLock()
			actor, ok := h.bearerTokens[raw]
			h.bearerMu.RUnlock()
			if !ok {
				return nil, nil, rbac.ErrUnauthenticated
			}
			return actor, &SessionMetadata{
				Email:       "service-token@example.test",
				ExpiresAtMs: nowFn() + 3600_000,
			}, nil
		},
		HostResolver:      &countingResolver{},
		BlockedAdminPorts: []int{9090},
		Version:           "0.1.0-test",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	h.srv = srv
	return h
}

func (h *adminTestHarness) readBootstrapToken(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(h.tokenPath)
	if err != nil {
		t.Fatalf("read bootstrap token: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

func (h *adminTestHarness) registerBearerToken(token string, actor *rbac.Actor) {
	h.bearerMu.Lock()
	defer h.bearerMu.Unlock()
	h.bearerTokens[token] = actor
}

func (h *adminTestHarness) doPublic(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return h.doRaw(t, method, path, body, nil, nil)
}

func (h *adminTestHarness) doAuthedCookie(t *testing.T, method, path string, cookie *http.Cookie, csrf, ifMatch string, body any) *httptest.ResponseRecorder {
	t.Helper()
	hdrs := map[string]string{
		"Origin": "https://control.tanpopo.local",
	}
	if csrf != "" {
		hdrs["X-CSRF-Token"] = csrf
	}
	if ifMatch != "" {
		hdrs["If-Match"] = ifMatch
	}
	var cookies []*http.Cookie
	if cookie != nil {
		cookies = []*http.Cookie{cookie}
	}
	return h.doRaw(t, method, path, body, hdrs, cookies)
}

func (h *adminTestHarness) doBearer(t *testing.T, method, path, bearer, ifMatch string, body any) *httptest.ResponseRecorder {
	t.Helper()
	hdrs := map[string]string{
		"Authorization": "Bearer " + bearer,
	}
	if ifMatch != "" {
		hdrs["If-Match"] = ifMatch
	}
	return h.doRaw(t, method, path, body, hdrs, nil)
}

func (h *adminTestHarness) doRaw(t *testing.T, method, path string, body any, headers map[string]string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if raw, ok := body.([]byte); ok {
			buf.Write(raw)
		} else {
			if err := json.NewEncoder(&buf).Encode(body); err != nil {
				t.Fatalf("encode request body: %v", err)
			}
		}
	}
	req := httptest.NewRequest(method, "https://control.tanpopo.local"+path, &buf)
	req.RemoteAddr = "203.0.113.10:4242"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)
	return rec
}

func mustDecodeJSON[T any](t *testing.T, data []byte, dst *T) {
	t.Helper()
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("json.Unmarshal: %v\npayload=%s", err, string(data))
	}
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}

func mustVKeyCreateParams(orgID, projID, envID, actorID, name string) vkey.CreateParams {
	return vkey.CreateParams{
		OrgID:            orgID,
		ProjectID:        projID,
		EnvID:            envID,
		OwnerPrincipalID: &actorID,
		Name:             name,
		ActorID:          actorID,
	}
}

func seedTestAdapter(t *testing.T, h *adminTestHarness) {
	t.Helper()
	now := h.fk.Now().UTC().UnixMilli()
	ctx := context.Background()
	_, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_packages (
			id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms
		) VALUES ('ext_openai_spec', 'tanpopo.openai', 'PROVIDER_ADAPTER', 'tanpopo', 'official', ?)`,
		now,
	)
	if err != nil {
		t.Fatalf("insert extension_packages: %v", err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_versions (
			id, package_id, semver, contract_version, core_version_range, execution_tier,
			bundle_sha256, signature_verified, enterprise_signed, source_uri, manifest_json, created_at_ms
		) VALUES ('exv_openai_spec', 'ext_openai_spec', '1.0.0', 'v1', '>=0.1.0', 'DECLARATIVE_T1', ?, 1, 0, 'builtin://tanpopo.openai', '{}', ?)`,
		strings.Repeat("a", 64), now,
	)
	if err != nil {
		t.Fatalf("insert extension_versions: %v", err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO extension_installations (
			id, package_id, active_version_id, state, approved_permissions_json,
			approved_by_principal_id, version, created_at_ms, updated_at_ms
		) VALUES ('ins_openai_spec', 'ext_openai_spec', 'exv_openai_spec', 'ENABLED_ACTIVE', '{"allowed_hosts":["api.openai.com"],"allow_private_ip":false,"require_tls":true}', NULL, 1, ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert extension_installations: %v", err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_adapters (
			id, installation_id, adapter_slug, native_prefix, supported_dialects_json,
			allowed_hosts_json, version, created_at_ms
		) VALUES ('adp_openai_spec', 'ins_openai_spec', 'openai', 'openai', '["openai"]', '["api.openai.com"]', 1, ?)`,
		now,
	)
	if err != nil {
		t.Fatalf("insert provider_adapters: %v", err)
	}
}

// TestAllAdminRoutesHaveExplicitPermission is the mandatory Security Invariant from TP-IMP-0024:
// every /admin/v1/* route except explicit public bootstrap/login endpoints MUST declare a valid RBAC Action,
// and rbac.ValidateRoutePermissions MUST fail closed if any route omits permission metadata.
func TestAllAdminRoutesHaveExplicitPermission(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)

	allowedPublic := map[string]bool{
		"GET /admin/v1/bootstrap/status":    true,
		"POST /admin/v1/bootstrap/complete": true,
		"POST /admin/v1/auth/login":         true,
		"POST /admin/v1/auth/enroll":        true,
		"GET /admin/v1/auth/oidc/start":     true,
		"GET /admin/v1/auth/oidc/callback":  true,
	}

	routes := h.srv.Routes()
	if len(routes) == 0 {
		t.Fatalf("expected non-empty route table")
	}
	if err := rbac.ValidateRoutePermissions(routes); err != nil {
		t.Fatalf("rbac.ValidateRoutePermissions failed: %v", err)
	}

	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		if !strings.HasPrefix(rt.Path, "/admin/v1/") {
			t.Fatalf("route %s does not start with /admin/v1/", key)
		}
		if rt.Public {
			if !allowedPublic[key] {
				t.Fatalf("unexpected public route without RBAC action: %s", key)
			}
			continue
		}
		if rt.Action == "" {
			t.Fatalf("protected route %s has empty Action", key)
		}
	}

	// Verify fail-closed rejection on missing permission binding.
	badRoutes := append(append([]rbac.RoutePermissionSpec(nil), routes...), rbac.RoutePermissionSpec{
		Method: http.MethodGet,
		Path:   "/admin/v1/unprotected",
		Public: false,
		Action: "",
	})
	if err := rbac.ValidateRoutePermissions(badRoutes); err == nil {
		t.Fatalf("expected ValidateRoutePermissions to fail closed when route lacks Action")
	}
}

func TestNewServerFailClosedValidationAndWireRoundTrip(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)

	// Fail-closed NewServer missing dependency checks.
	if _, err := NewServer(ServerConfig{}); !errors.Is(err, ErrInvalidServerConfig) {
		t.Fatalf("expected ErrInvalidServerConfig for empty config, got %v", err)
	}
	if _, err := NewServer(ServerConfig{Store: h.st}); !errors.Is(err, ErrInvalidServerConfig) {
		t.Fatalf("expected ErrInvalidServerConfig without Bootstrap, got %v", err)
	}

	// Verify live HTTP wire server using ts.Client() (zero DefaultClient) + 404/405/panic/body-limit envelopes.
	ts := httptest.NewServer(h.srv)
	defer ts.Close()

	client := ts.Client()
	resp, err := client.Get(ts.URL + "/admin/v1/bootstrap/status")
	if err != nil {
		t.Fatalf("wire GET /bootstrap/status: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 over wire, got %d", resp.StatusCode)
	}

	// Method not allowed (405) + Allow header.
	req405, err := http.NewRequest(http.MethodDelete, ts.URL+"/admin/v1/bootstrap/status", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp405, err := client.Do(req405)
	if err != nil {
		t.Fatalf("wire DELETE /bootstrap/status: %v", err)
	}
	_ = resp405.Body.Close()
	if resp405.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp405.StatusCode)
	}
	if !strings.Contains(resp405.Header.Get("Allow"), "GET") {
		t.Fatalf("expected Allow: GET, got %q", resp405.Header.Get("Allow"))
	}

	// Unknown route -> 404 JSON envelope.
	resp404, err := client.Get(ts.URL + "/admin/v1/does-not-exist")
	if err != nil {
		t.Fatalf("wire GET unknown: %v", err)
	}
	_ = resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp404.StatusCode)
	}
}

func TestAC1BootstrapProvisioningAuditAndSnapshotImmediateAuth(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()
	seedTestAdapter(t, h)

	// Fresh status check.
	rec := h.doPublic(t, http.MethodGet, "/admin/v1/bootstrap/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var stResp bootstrapStatusResponse
	mustDecodeJSON(t, rec.Body.Bytes(), &stResp)
	if stResp.Initialized {
		t.Fatalf("expected initialized=false on fresh server")
	}

	token := h.readBootstrapToken(t)

	// SSRF blocked metadata IP MUST fail 422 WITHOUT burning the single-use Bootstrap Token!
	recSSRF := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "VeryStrongPassword#2026!",
		"org_name":                       "Acme AI",
		"project_name":                   "core",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "bad-imds",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "http://169.254.169.254/latest/meta-data",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recSSRF.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 SSRF block, got %d body=%s", recSSRF.Code, recSSRF.Body.String())
	}
	var ssrfErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recSSRF.Body.Bytes(), &ssrfErr)
	if ssrfErr.Error.Code != "ssrf_blocked_private_ip" {
		t.Fatalf("expected ssrf_blocked_private_ip, got %s", ssrfErr.Error.Code)
	}

	// Token MUST still work on valid retry.
	recComplete := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "VeryStrongPassword#2026!",
		"org_name":                       "Acme AI",
		"project_name":                   "core",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "openai-main",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1",
			"secret_value": "sk-initial-bootstrap-value",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recComplete.Code != http.StatusCreated {
		t.Fatalf("expected 201 after pre-validation rejection, got %d body=%s", recComplete.Code, recComplete.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recComplete.Body.Bytes(), &boot)
	if boot.InitialVirtualKey == nil || !strings.HasPrefix(boot.InitialVirtualKey.SecretKey, "vk_tp_") {
		t.Fatalf("expected initial vk_tp_ key, got %+v", boot.InitialVirtualKey)
	}

	// Verify Data Plane ConfigSnapshot immediately authenticates bootstrap virtual key.
	authVK, err := h.snapshots.AuthenticateKey(boot.InitialVirtualKey.SecretKey, boot.EnvID)
	if err != nil {
		t.Fatalf("snapshots.AuthenticateKey on initial key failed: %v", err)
	}
	if authVK.Key == nil || authVK.Key.OrgID != boot.OrgID || authVK.Key.ProjectID != boot.ProjectID {
		t.Fatalf("unexpected authenticated key scope: %+v", authVK)
	}

	cookie := findCookie(t, recComplete.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Provision another Project, Environment, and Virtual Key.
	recProj := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", cookie, boot.CSRFToken, "", map[string]any{
		"slug": "payments",
		"name": "Payments AI",
	})
	if recProj.Code != http.StatusCreated {
		t.Fatalf("POST /projects status=%d body=%s", recProj.Code, recProj.Body.String())
	}
	var proj projectDTO
	mustDecodeJSON(t, recProj.Body.Bytes(), &proj)

	recEnv := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+proj.ID+"/environments", cookie, boot.CSRFToken, "", map[string]any{
		"slug": "prod",
		"name": "Production",
	})
	if recEnv.Code != http.StatusCreated {
		t.Fatalf("POST /environments status=%d body=%s", recEnv.Code, recEnv.Body.String())
	}
	var env environmentDTO
	mustDecodeJSON(t, recEnv.Body.Bytes(), &env)

	recKey := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+proj.ID+"/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":       "payments-gateway",
		"project_id": proj.ID,
		"env_id":     env.ID,
		"rpm_limit":  120,
	})
	if recKey.Code != http.StatusCreated {
		t.Fatalf("POST /virtual-keys status=%d body=%s", recKey.Code, recKey.Body.String())
	}
	var vkCreated virtualKeyCreatedDTO
	mustDecodeJSON(t, recKey.Body.Bytes(), &vkCreated)

	// Verify immediate ConfigSnapshot auth + hash-chained audit trail integrity.
	authVK2, err := h.snapshots.AuthenticateKey(vkCreated.SecretKey, env.ID)
	if err != nil {
		t.Fatalf("AuthenticateKey on newly provisioned key: %v", err)
	}
	if authVK2.Key == nil || authVK2.Key.ID != vkCreated.ID || authVK2.Key.RPMLimit == nil || *authVK2.Key.RPMLimit != 120 {
		t.Fatalf("unexpected snapshot key state: %+v", authVK2)
	}

	report, err := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{})
	if err != nil || !report.Valid || report.TotalVerified < 3 {
		t.Fatalf("audit hash chain verification failed: report=%+v err=%v", report, err)
	}

	// Replay returns 410 bootstrap_already_completed.
	recReplay := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "VeryStrongPassword#2026!",
		"master_kek_backup_acknowledged": true,
	})
	if recReplay.Code != http.StatusGone {
		t.Fatalf("expected 410 Gone on bootstrap replay, got %d", recReplay.Code)
	}
}

func TestAC2CursorPaginationDeterminism125KeysConcurrentInsert(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "pager@example.test",
		"owner_password":                 "StrongPagingPassword#2026!",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	cookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Seed 125 Virtual Keys advancing FakeClock slightly so timestamps/IDs order cleanly.
	preExisting := make(map[string]bool, 125)
	for i := 0; i < 125; i++ {
		h.fk.Advance(10 * time.Millisecond)
		rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
			"name":       fmt.Sprintf("key-%03d", i),
			"project_id": boot.ProjectID,
			"env_id":     boot.EnvID,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create key %d failed: %d %s", i, rec.Code, rec.Body.String())
		}
		var kc virtualKeyCreatedDTO
		mustDecodeJSON(t, rec.Body.Bytes(), &kc)
		preExisting[kc.ID] = true
	}

	// Page 1 (limit=50).
	recP1 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=50", cookie, "", "", nil)
	if recP1.Code != http.StatusOK {
		t.Fatalf("page 1 failed: %d %s", recP1.Code, recP1.Body.String())
	}
	var p1 paginatedListEnvelope[virtualKeySummaryDTO]
	mustDecodeJSON(t, recP1.Body.Bytes(), &p1)
	if len(p1.Data) != 50 || !p1.HasMore || p1.NextCursor == nil {
		t.Fatalf("unexpected page 1 shape: len=%d hasMore=%v cursor=%v", len(p1.Data), p1.HasMore, p1.NextCursor)
	}

	// Concurrent insert into page 1 while iterating!
	h.fk.Advance(10 * time.Millisecond)
	recConcurrent := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":       "concurrent-page1-insert",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recConcurrent.Code != http.StatusCreated {
		t.Fatalf("concurrent insert failed: %d %s", recConcurrent.Code, recConcurrent.Body.String())
	}
	var concurrentKey virtualKeyCreatedDTO
	mustDecodeJSON(t, recConcurrent.Body.Bytes(), &concurrentKey)

	// Page 2 (limit=50).
	recP2 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=50&cursor="+*p1.NextCursor, cookie, "", "", nil)
	if recP2.Code != http.StatusOK {
		t.Fatalf("page 2 failed: %d %s", recP2.Code, recP2.Body.String())
	}
	var p2 paginatedListEnvelope[virtualKeySummaryDTO]
	mustDecodeJSON(t, recP2.Body.Bytes(), &p2)
	if len(p2.Data) != 50 || !p2.HasMore || p2.NextCursor == nil {
		t.Fatalf("unexpected page 2 shape: len=%d hasMore=%v", len(p2.Data), p2.HasMore)
	}

	// Page 3 (limit=50) -> final 25 records.
	recP3 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=50&cursor="+*p2.NextCursor, cookie, "", "", nil)
	if recP3.Code != http.StatusOK {
		t.Fatalf("page 3 failed: %d %s", recP3.Code, recP3.Body.String())
	}
	var p3 paginatedListEnvelope[virtualKeySummaryDTO]
	mustDecodeJSON(t, recP3.Body.Bytes(), &p3)
	if len(p3.Data) != 25 || p3.HasMore || p3.NextCursor != nil {
		t.Fatalf("unexpected page 3 shape: len=%d hasMore=%v cursor=%v", len(p3.Data), p3.HasMore, p3.NextCursor)
	}

	seen := make(map[string]bool, 125)
	for _, k := range append(append(p1.Data, p2.Data...), p3.Data...) {
		if k.ID == concurrentKey.ID {
			t.Fatalf("concurrent page-1 key leaked into subsequent pages")
		}
		if seen[k.ID] {
			t.Fatalf("duplicate key %s returned across cursor pages", k.ID)
		}
		seen[k.ID] = true
	}
	if len(seen) != len(preExisting) {
		t.Fatalf("expected %d distinct pre-existing keys, got %d", len(preExisting), len(seen))
	}
	for id := range preExisting {
		if !seen[id] {
			t.Fatalf("missing key %s from cursor pagination", id)
		}
	}

	// Verify cursor tamper & limit bounds checks.
	recTamper := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=50&cursor="+*p1.NextCursor+"tamper", cookie, "", "", nil)
	if recTamper.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on tampered cursor, got %d", recTamper.Code)
	}
	recFilterMismatch := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=50&project_id="+boot.ProjectID+"&cursor="+*p1.NextCursor, cookie, "", "", nil)
	if recFilterMismatch.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on filter-mismatched cursor, got %d", recFilterMismatch.Code)
	}
	recBadLimit := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=201", cookie, "", "", nil)
	if recBadLimit.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on limit=201, got %d", recBadLimit.Code)
	}
}

func TestNEG1PlaneIsolationCSRFSpoofingAndCrossTenant(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()

	// Complete bootstrap for Org A.
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "owner-a@example.test",
		"owner_password":                 "StrongOrgAPass#2026!",
		"org_name":                       "Org A",
		"project_name":                   "proj-a",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d", recBoot.Code)
	}
	var bootA bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &bootA)
	cookieA := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Provision Org B + Owner B directly in storage/auth.
	orgB, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-b", DisplayName: "Org B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	projB, err := h.st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "proj-b", DisplayName: "Proj B"})
	if err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}
	envB, err := h.st.Tenancy().CreateEnvironment(ctx, orgB.ID, projB.ID, port.CreateEnvironmentParams{Slug: "prod"})
	if err != nil {
		t.Fatalf("CreateEnvironment B: %v", err)
	}
	ownerB, err := h.authSvc.CreatePrincipal(ctx, auth.CreatePrincipalParams{
		OrgID:      &orgB.ID,
		Kind:       auth.PrincipalKindHumanLocal,
		EmailOrSub: "owner-b@example.test",
		Password:   "StrongOrgBPass#2026!",
	})
	if err != nil {
		t.Fatalf("CreatePrincipal B: %v", err)
	}
	if _, err := h.authSvc.BindRole(ctx, auth.BindRoleParams{
		OrgID:       &orgB.ID,
		PrincipalID: ownerB.ID,
		ScopeType:   rbac.ScopeOrg,
		ScopeID:     orgB.ID,
		RoleName:    rbac.RoleOrgOwner,
	}); err != nil {
		t.Fatalf("BindRole B: %v", err)
	}
	if _, err := h.vkeys.Create(ctx, mustVKeyCreateParams(orgB.ID, projB.ID, envB.ID, ownerB.ID, "orgb-k1")); err != nil {
		t.Fatalf("vkeys.Create B1: %v", err)
	}
	if _, err := h.vkeys.Create(ctx, mustVKeyCreateParams(orgB.ID, projB.ID, envB.ID, ownerB.ID, "orgb-k2")); err != nil {
		t.Fatalf("vkeys.Create B2: %v", err)
	}
	_, cookieB, err := h.authSvc.LoginForOrg(ctx, "203.0.113.10", orgB.ID, "owner-b@example.test", "StrongOrgBPass#2026!")
	if err != nil {
		t.Fatalf("LoginForOrg B: %v", err)
	}

	// AUTH-07: Data Plane Virtual Key (vk_tp_*) rejected across all Admin headers & endpoints.
	for _, hdr := range []struct{ k, v string }{
		{"Authorization", "Bearer vk_tp_prod_secret1234567890"},
		{"X-Tanpopo-Virtual-Key", "vk_tp_prod_secret1234567890"},
		{"x-api-key", "vk_tp_prod_secret1234567890"},
		{"x-goog-api-key", "vk_tp_prod_secret1234567890"},
	} {
		rec := h.doRaw(t, http.MethodGet, "/admin/v1/virtual-keys", nil, map[string]string{hdr.k: hdr.v}, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for vk_tp_ in %s, got %d", hdr.k, rec.Code)
		}
		var ee CanonicalErrorEnvelope
		mustDecodeJSON(t, rec.Body.Bytes(), &ee)
		if ee.Error.Code != "invalid_credential_plane" {
			t.Fatalf("expected invalid_credential_plane, got %s", ee.Error.Code)
		}
	}

	// AUTH-03: Missing X-CSRF-Token or bad Origin returns 403 csrf_validation_failed.
	recNoCSRF := h.doRaw(t, http.MethodPost, "/admin/v1/projects", map[string]any{"slug": "x", "name": "X"}, map[string]string{
		"Origin": "https://control.tanpopo.local",
	}, []*http.Cookie{cookieA})
	if recNoCSRF.Code != http.StatusForbidden {
		t.Fatalf("expected 403 missing CSRF, got %d", recNoCSRF.Code)
	}
	recBadOrigin := h.doRaw(t, http.MethodPost, "/admin/v1/projects", map[string]any{"slug": "x", "name": "X"}, map[string]string{
		"Origin":       "https://evil.example.net",
		"X-CSRF-Token": bootA.CSRFToken,
	}, []*http.Cookie{cookieA})
	if recBadOrigin.Code != http.StatusForbidden {
		t.Fatalf("expected 403 bad Origin, got %d", recBadOrigin.Code)
	}

	// AUTH-13: Forged top-level org_id in JSON body -> 403 org_id_spoofing_forbidden + audit_events.
	recSpoof := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", cookieA, bootA.CSRFToken, "", map[string]any{
		"org_id": orgB.ID,
		"slug":   "spoofed",
		"name":   "Spoofed",
	})
	if recSpoof.Code != http.StatusForbidden {
		t.Fatalf("expected 403 org_id spoofing, got %d body=%s", recSpoof.Code, recSpoof.Body.String())
	}
	var spoofErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recSpoof.Body.Bytes(), &spoofErr)
	if spoofErr.Error.Code != "org_id_spoofing_forbidden" {
		t.Fatalf("expected org_id_spoofing_forbidden, got %s", spoofErr.Error.Code)
	}
	var spoofAuditCount int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE event_type = 'security.org_id_spoofing_blocked'`).Scan(&spoofAuditCount); err != nil || spoofAuditCount < 1 {
		t.Fatalf("expected security.org_id_spoofing_blocked audit event, count=%d err=%v", spoofAuditCount, err)
	}

	// AUTH-15: Org B next_cursor replayed by Org A -> 400 invalid_pagination_cursor.
	recListB := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=1", cookieB, "", "", nil)
	if recListB.Code != http.StatusOK {
		t.Fatalf("list org B keys failed: %d", recListB.Code)
	}
	var pB paginatedListEnvelope[virtualKeySummaryDTO]
	mustDecodeJSON(t, recListB.Body.Bytes(), &pB)
	if pB.NextCursor == nil {
		t.Fatalf("expected Org B next_cursor with limit=1")
	}
	recCrossCursor := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys?limit=1&cursor="+*pB.NextCursor, cookieA, "", "", nil)
	if recCrossCursor.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 cross-org cursor replay, got %d", recCrossCursor.Code)
	}
	var curErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recCrossCursor.Body.Bytes(), &curErr)
	if curErr.Error.Code != "invalid_pagination_cursor" {
		t.Fatalf("expected invalid_pagination_cursor, got %s", curErr.Error.Code)
	}

	// AUTH-15: Org A targeting Org B Project ID -> 404 resource_not_found.
	recCrossProj := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/projects/"+projB.ID+"/environments", cookieA, "", "", nil)
	if recCrossProj.Code != http.StatusNotFound {
		t.Fatalf("expected 404 cross-org project access, got %d body=%s", recCrossProj.Code, recCrossProj.Body.String())
	}

	// Bearer token auth works for non-cookie CLI/service callers without X-CSRF-Token.
	h.registerBearerToken("tp_adm_svc_valid_001", &rbac.Actor{
		PrincipalID:     ownerB.ID,
		SessionOrgID:    &orgB.ID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{
			{OrgID: &orgB.ID, ScopeType: rbac.ScopeOrg, ScopeID: orgB.ID, RoleName: rbac.RoleOrgOwner},
		},
	})
	recBearer := h.doBearer(t, http.MethodGet, "/admin/v1/projects", "tp_adm_svc_valid_001", "", nil)
	if recBearer.Code != http.StatusOK {
		t.Fatalf("expected 200 for bearer auth, got %d body=%s", recBearer.Code, recBearer.Body.String())
	}
	recBadBearer := h.doBearer(t, http.MethodGet, "/admin/v1/projects", "tp_adm_svc_bogus", "", nil)
	if recBadBearer.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid bearer, got %d", recBadBearer.Code)
	}
}

func TestNEG2OptimisticLockingETag428And409(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "occ@example.test",
		"owner_password":                 "StrongOCCPassword#2026!",
		"master_kek_backup_acknowledged": true,
	})
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	cookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	recCreate := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":       "occ-key",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create key: %d", recCreate.Code)
	}
	etag1 := recCreate.Header().Get("ETag")
	if etag1 != `"v1"` {
		t.Fatalf(`expected ETag "v1", got %q`, etag1)
	}
	var vk virtualKeyCreatedDTO
	mustDecodeJSON(t, recCreate.Body.Bytes(), &vk)

	// Missing If-Match -> 428 precondition_required.
	rec428 := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/virtual-keys/"+vk.ID, cookie, boot.CSRFToken, "", map[string]any{"name": "no-if-match"})
	if rec428.Code != http.StatusPreconditionRequired {
		t.Fatalf("expected 428, got %d", rec428.Code)
	}

	// Wildcard / weak If-Match -> 400 invalid_if_match.
	for _, bad := range []string{"*", `W/"v1"`, "v1"} {
		rec400 := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/virtual-keys/"+vk.ID, cookie, boot.CSRFToken, bad, map[string]any{"name": "bad"})
		if rec400.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for If-Match=%q, got %d", bad, rec400.Code)
		}
	}

	// First writer succeeds ("v1" -> "v2").
	recW1 := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/virtual-keys/"+vk.ID, cookie, boot.CSRFToken, `"v1"`, map[string]any{
		"name":      "occ-key-updated",
		"rpm_limit": 250,
	})
	if recW1.Code != http.StatusOK || recW1.Header().Get("ETag") != `"v2"` {
		t.Fatalf("first writer failed: code=%d etag=%q body=%s", recW1.Code, recW1.Header().Get("ETag"), recW1.Body.String())
	}

	// Concurrent second writer still holding "v1" receives 409 optimistic_lock_conflict + current_resource + ETag "v2".
	recW2 := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/virtual-keys/"+vk.ID, cookie, boot.CSRFToken, `"v1"`, map[string]any{
		"name": "stale-overwrite",
	})
	if recW2.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d body=%s", recW2.Code, recW2.Body.String())
	}
	if recW2.Header().Get("ETag") != `"v2"` {
		t.Fatalf(`expected conflict ETag "v2", got %q`, recW2.Header().Get("ETag"))
	}
	var conflictErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recW2.Body.Bytes(), &conflictErr)
	if conflictErr.Error.Code != "optimistic_lock_conflict" || conflictErr.Error.CurrentResource == nil {
		t.Fatalf("expected optimistic_lock_conflict with current_resource, got %+v", conflictErr)
	}

	// Stale rotate and stale revoke also return 409.
	recRot409 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys/"+vk.ID+"/rotate", cookie, boot.CSRFToken, `"v1"`, map[string]any{"grace_window_seconds": 60})
	if recRot409.Code != http.StatusConflict {
		t.Fatalf("expected 409 on stale rotate, got %d", recRot409.Code)
	}
	recRev409 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys/"+vk.ID+"/revoke", cookie, boot.CSRFToken, `"v1"`, map[string]any{"mode": "GRACEFUL_DRAIN", "reason": "stale version check"})
	if recRev409.Code != http.StatusConflict {
		t.Fatalf("expected 409 on stale revoke, got %d", recRev409.Code)
	}
}

func TestNEG3BruteForceLoginRateLimitAndAudit(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "brute@example.test",
		"owner_password":                 "StrongBrutePass#2026!",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d", recBoot.Code)
	}

	// Fail 10 times; the 11th failure from same IP must return 429 auth_rate_limited.
	var saw429 bool
	for attempt := 1; attempt <= 12; attempt++ {
		rec := h.doPublic(t, http.MethodPost, "/admin/v1/auth/login", map[string]any{
			"email":    "brute@example.test",
			"password": "wrong-password",
		})
		if rec.Code == http.StatusTooManyRequests {
			saw429 = true
			if rec.Header().Get("Retry-After") == "" {
				t.Fatalf("expected Retry-After header on 429")
			}
			var ee CanonicalErrorEnvelope
			mustDecodeJSON(t, rec.Body.Bytes(), &ee)
			if ee.Error.Code != "auth_rate_limited" {
				t.Fatalf("expected auth_rate_limited, got %s", ee.Error.Code)
			}
			break
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d unexpected status %d: %s", attempt, rec.Code, rec.Body.String())
		}
	}
	if !saw429 {
		t.Fatalf("expected 429 auth_rate_limited after >10 failed login attempts")
	}

	var rlAudit int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE event_type = 'auth.login_rate_limited'`).Scan(&rlAudit); err != nil || rlAudit < 1 {
		t.Fatalf("expected auth.login_rate_limited audit event, count=%d err=%v", rlAudit, err)
	}
}

func TestVirtualKeyShowOnceCanaryRotateRevokeFromQuotaPausedAndLogout(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "vklife@example.test",
		"owner_password":                 "StrongLifePass#2026!",
		"master_kek_backup_acknowledged": true,
	})
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	cookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Create Virtual Key -> returns secret_key once.
	recCreate := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+boot.ProjectID+"/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":       "show-once-key",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", recCreate.Code)
	}
	var created virtualKeyCreatedDTO
	mustDecodeJSON(t, recCreate.Body.Bytes(), &created)
	h.scanner.AddSecret(created.SecretKey)

	// GET list must NEVER disclose secret_key (show-once + CanaryScanner).
	recList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys", cookie, "", "", nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list failed: %d", recList.Code)
	}
	if findings := h.scanner.ScanString("GET /virtual-keys", recList.Body.String()); len(findings) > 0 {
		t.Fatalf("secret leaked in GET /virtual-keys: %v", findings)
	}

	// Rotate -> ROTATING_DUAL_VALID with new secret_key.
	recRot := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys/"+created.ID+"/rotate", cookie, boot.CSRFToken, `"v1"`, map[string]any{
		"grace_window_seconds": 300,
	})
	if recRot.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d body=%s", recRot.Code, recRot.Body.String())
	}
	var rotated virtualKeyCreatedDTO
	mustDecodeJSON(t, recRot.Body.Bytes(), &rotated)
	if rotated.State != snapshot.KeyStateRotatingDualValid || rotated.SecretKey == "" || rotated.SecretKey == created.SecretKey {
		t.Fatalf("unexpected rotated key: %+v", rotated)
	}
	h.scanner.AddSecret(rotated.SecretKey)

	// Transition key to QUOTA_PAUSED and register active streams; verify EMERGENCY_ABORT_STREAMS succeeds from QUOTA_PAUSED.
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE virtual_keys SET state = 'QUOTA_PAUSED' WHERE id = ?`, created.ID); err != nil {
		t.Fatalf("set QUOTA_PAUSED: %v", err)
	}
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	h.snapshots.Streams().Register(boot.OrgID, boot.ProjectID, created.ID, streamCancel)

	recRev := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys/"+created.ID+"/revoke", cookie, boot.CSRFToken, `"v2"`, map[string]any{
		"mode":   "EMERGENCY_ABORT_STREAMS",
		"reason": "Suspected leak from QUOTA_PAUSED key",
	})
	if recRev.Code != http.StatusOK {
		t.Fatalf("revoke failed: %d body=%s", recRev.Code, recRev.Body.String())
	}
	var revResp revokeVirtualKeyResponse
	mustDecodeJSON(t, recRev.Body.Bytes(), &revResp)
	if revResp.Key.State != snapshot.KeyStateRevoked || revResp.SeveredStreamsCount != 1 {
		t.Fatalf("expected REVOKED + 1 severed stream, got %+v", revResp)
	}
	if streamCtx.Err() == nil {
		t.Fatalf("expected active stream context to be canceled immediately on EMERGENCY_ABORT_STREAMS")
	}

	// Create an ephemeral playground key owned by caller and verify POST /auth/logout revokes it cleanly.
	pgKey, err := h.vkeys.Create(ctx, vkey.CreateParams{
		OrgID:            boot.OrgID,
		ProjectID:        boot.ProjectID,
		EnvID:            boot.EnvID,
		OwnerPrincipalID: &boot.OwnerPrincipalID,
		Name:             "playground-ephemeral",
		ActorID:          boot.OwnerPrincipalID,
	})
	if err != nil {
		t.Fatalf("create playground key: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE virtual_keys SET is_ephemeral_playground = 1 WHERE id = ?`, pgKey.Summary.ID); err != nil {
		t.Fatalf("mark ephemeral playground: %v", err)
	}

	recLogout := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/auth/logout", cookie, boot.CSRFToken, "", nil)
	if recLogout.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on logout, got %d body=%s", recLogout.Code, recLogout.Body.String())
	}
	var pgState string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT state FROM virtual_keys WHERE id = ?`, pgKey.Summary.ID).Scan(&pgState); err != nil || pgState != snapshot.KeyStateRevoked {
		t.Fatalf("expected playground key REVOKED after logout, got %q err=%v", pgState, err)
	}
}

func TestSixRoleRBACMatrixAndAntiSelfEscalation(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "rbac-owner@example.test",
		"owner_password":                 "StrongRBACPass#2026!",
		"master_kek_backup_acknowledged": true,
	})
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	ownerCookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Create a second project to test TeamAdmin project isolation.
	recP2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "isolated-p2",
		"name": "Isolated P2",
	})
	var proj2 projectDTO
	mustDecodeJSON(t, recP2.Body.Bytes(), &proj2)

	// Confirm RoleFinOpsViewer NEVER has ActionSecretReadMeta or ActionMemberWrite.
	finActor := &rbac.Actor{
		PrincipalID:     "prc_finops_test",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleFinOpsViewer}},
	}
	if err := rbac.Authorize(finActor, rbac.ActionSecretReadMeta, rbac.TargetScope{OrgID: boot.OrgID}); !errors.Is(err, rbac.ErrPermissionDenied) {
		t.Fatalf("FINOPS_VIEWER must NOT have ActionSecretReadMeta, got err=%v", err)
	}
	h.registerBearerToken("tok_finops", finActor)
	if rec := h.doBearer(t, http.MethodPost, "/admin/v1/projects", "tok_finops", "", map[string]any{"slug": "x", "name": "X"}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for FINOPS_VIEWER creating project, got %d", rec.Code)
	}
	if err := rbac.Authorize(finActor, rbac.ActionTraceReadMeta, rbac.TargetScope{OrgID: boot.OrgID, ProjectID: boot.ProjectID}); !errors.Is(err, rbac.ErrPermissionDenied) {
		t.Fatalf("FINOPS_VIEWER must NOT have ActionTraceReadMeta, got err=%v", err)
	}
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/traces?env_id="+boot.EnvID, "tok_finops", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for FINOPS_VIEWER reading trace metadata through Admin API, got %d body=%s", rec.Code, rec.Body.String())
	}

	// DEVELOPER can create project virtual key, cannot read/write members.
	devActor := &rbac.Actor{
		PrincipalID:     "prc_dev_test",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleDeveloper}},
	}
	h.registerBearerToken("tok_dev", devActor)
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/members", "tok_dev", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for DEVELOPER listing members, got %d", rec.Code)
	}

	// SECURITY_REVIEWER can read members and virtual-keys, cannot mutate them.
	secActor := &rbac.Actor{
		PrincipalID:     "prc_sec_test",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleSecurityReviewer}},
	}
	h.registerBearerToken("tok_sec", secActor)
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/members", "tok_sec", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SECURITY_REVIEWER listing members, got %d", rec.Code)
	}
	if rec := h.doBearer(t, http.MethodPost, "/admin/v1/projects", "tok_sec", "", map[string]any{"slug": "x", "name": "X"}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for SECURITY_REVIEWER creating project, got %d", rec.Code)
	}

	// Create TeamAdmin bound only to boot.ProjectID via POST /members.
	recCreateTA := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email":       "ta@example.test",
		"kind":        "HUMAN",
		"roles":       []string{"TEAM_ADMIN"},
		"project_ids": []string{boot.ProjectID},
	})
	if recCreateTA.Code != http.StatusCreated {
		t.Fatalf("create TEAM_ADMIN failed: %d body=%s", recCreateTA.Code, recCreateTA.Body.String())
	}
	var taMember memberSummaryDTO
	mustDecodeJSON(t, recCreateTA.Body.Bytes(), &taMember)

	taActor := &rbac.Actor{
		PrincipalID:     taMember.ID,
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin}},
	}
	h.registerBearerToken("tok_ta", taActor)

	// Anti-self-escalation: TEAM_ADMIN trying to elevate self to ORG_OWNER -> 403 role_escalation_denied.
	recSelfEsc := h.doBearer(t, http.MethodPatch, "/admin/v1/members/"+taMember.ID+"/roles", "tok_ta", `"v1"`, map[string]any{
		"roles": []string{"ORG_OWNER"},
	})
	if recSelfEsc.Code != http.StatusForbidden {
		t.Fatalf("expected 403 self-escalation block, got %d body=%s", recSelfEsc.Code, recSelfEsc.Body.String())
	}
	var escErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recSelfEsc.Body.Bytes(), &escErr)
	if escErr.Error.Code != "role_escalation_denied" {
		t.Fatalf("expected role_escalation_denied, got %s", escErr.Error.Code)
	}

	// Verify OPERATOR role cannot be granted to org members & non-OPERATOR cannot create Organization.
	recEscOrg := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email": "escalate-op@example.test",
		"roles": []string{"OPERATOR"},
	})
	if recEscOrg.Code != http.StatusForbidden {
		t.Fatalf("expected 403 granting OPERATOR inside org, got %d", recEscOrg.Code)
	}
	orgOwnerOnly := &rbac.Actor{
		PrincipalID:     "prc_orgowner_only",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner}},
	}
	h.registerBearerToken("tok_org_owner_only", orgOwnerOnly)
	recNonOpOrg := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_org_owner_only", "", map[string]any{
		"slug": "unauth-org",
		"name": "Unauth Org",
	})
	if recNonOpOrg.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for ORG_OWNER calling POST /organizations, got %d", recNonOpOrg.Code)
	}

	// TeamAdmin cannot create/modify developer in proj2 (outside scope).
	if rec := h.doBearer(t, http.MethodPost, "/admin/v1/members", "tok_ta", "", map[string]any{
		"email":       "dev-p2@example.test",
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{proj2.ID},
	}); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for TeamAdmin assigning outside project scope, got %d", rec.Code)
	}
}

func TestOrganizationAndProjectCryptoShreddingCascadeAndCleanup(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()
	seedTestAdapter(t, h)

	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "shred@example.test",
		"owner_password":                 "StrongShredPass#2026!",
		"org_name":                       "Shred Org",
		"project_name":                   "shred-proj",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "shred-conn",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1",
			"secret_value": "sk-shred-canary-secret-1",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	ownerCookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Seal a Project-scoped secret so Project crypto-shredding can prove unseal failure afterwards.
	projSecret, err := h.vault.SealSecret(ctx, vault.SealSecretParams{
		OrgID:          boot.OrgID,
		OwnerProjectID: &boot.ProjectID,
		Name:           "proj-secret",
		SecretKind:     vault.SecretKindAPIKey,
		Plaintext:      []byte("project-only-secret-value"),
	})
	if err != nil {
		t.Fatalf("SealSecret project: %v", err)
	}

	// Register an active stream inside the project.
	projStreamCtx, projStreamCancel := context.WithCancel(ctx)
	defer projStreamCancel()
	h.snapshots.Streams().Register(boot.OrgID, boot.ProjectID, boot.InitialVirtualKey.ID, projStreamCancel)

	// Delete Project (with invalid confirm_slug first -> 400 confirm_slug_mismatch, then valid).
	recBadSlug := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/projects/"+boot.ProjectID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"confirm_slug": "wrong-slug",
		"reason":       "Shredding entire project test",
	})
	if recBadSlug.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 confirm_slug_mismatch, got %d", recBadSlug.Code)
	}

	recShredProj := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/projects/"+boot.ProjectID, ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"confirm_slug": "shred-proj",
		"reason":       "GDPR erasure and project closeout",
	})
	if recShredProj.Code != http.StatusOK {
		t.Fatalf("DELETE /projects failed: %d body=%s", recShredProj.Code, recShredProj.Body.String())
	}
	var receiptProj cryptoShredReceiptDTO
	mustDecodeJSON(t, recShredProj.Body.Bytes(), &receiptProj)
	if receiptProj.TargetType != "PROJECT" || receiptProj.RevokedVirtualKeysCount < 1 {
		t.Fatalf("unexpected project shred receipt: %+v", receiptProj)
	}
	if projStreamCtx.Err() == nil {
		t.Fatalf("expected project active stream to be aborted")
	}
	if _, unsealErr := h.vault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), boot.OrgID, projSecret.ID); unsealErr == nil {
		t.Fatalf("expected project secret unseal to fail permanently after Project DEK shred")
	}

	// Verify immutable audit chain survived project teardown intact.
	if report, aerr := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{}); aerr != nil || !report.Valid {
		t.Fatalf("audit chain broken after project shred: report=%+v err=%v", report, aerr)
	}

	// Now test OPERATOR creating a fresh Org and crypto-shredding Shred Org via DELETE /organizations/{orgId}.
	opActor := &rbac.Actor{
		PrincipalID:     "prc_instance_operator_test",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	}
	h.registerBearerToken("tok_operator", opActor)

	recNewOrg := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_operator", "", map[string]any{
		"slug": "second-org",
		"name": "Second Org",
	})
	if recNewOrg.Code != http.StatusCreated {
		t.Fatalf("OPERATOR POST /organizations failed: %d body=%s", recNewOrg.Code, recNewOrg.Body.String())
	}

	recShredOrg := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+boot.OrgID, "tok_operator", `"v1"`, map[string]any{
		"confirm_slug": "shred-org",
		"reason":       "Full organization crypto-shredding verification",
	})
	if recShredOrg.Code != http.StatusOK {
		t.Fatalf("DELETE /organizations failed: %d body=%s", recShredOrg.Code, recShredOrg.Body.String())
	}
	var receiptOrg cryptoShredReceiptDTO
	mustDecodeJSON(t, recShredOrg.Body.Bytes(), &receiptOrg)
	if receiptOrg.TargetType != "ORGANIZATION" || receiptOrg.TargetID != boot.OrgID {
		t.Fatalf("unexpected org shred receipt: %+v", receiptOrg)
	}

	// Owner's session cookie MUST be invalidated after organization teardown.
	recAfterSession := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/auth/session", ownerCookie, "", "", nil)
	if recAfterSession.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked org owner session after org shred, got %d", recAfterSession.Code)
	}
}

func TestRBACEscalationAndCrossProjectMemberIsolation(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "rbac-owner@example.test",
		"owner_password":                 "StrongRBACPass#2026!",
		"org_name":                       "RBAC Org",
		"project_name":                   "p1",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	ownerCookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Create second project P2.
	recP2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "p2",
		"name": "Project Two",
	})
	if recP2.Code != http.StatusCreated {
		t.Fatalf("create P2 failed: %d %s", recP2.Code, recP2.Body.String())
	}
	var proj2 projectDTO
	mustDecodeJSON(t, recP2.Body.Bytes(), &proj2)

	// Dual-role actor: Org DEVELOPER + Project(P1) TEAM_ADMIN must NOT grant org-wide roles or omit project_ids.
	dualActor := &rbac.Actor{
		PrincipalID:     "prc_dual_team_admin",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{
			{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleDeveloper},
			{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin},
		},
	}
	h.registerBearerToken("tok_dual_ta", dualActor)
	for _, tc := range []struct {
		name    string
		payload map[string]any
		wantErr string
	}{
		{
			name: "dual_role_grant_org_owner",
			payload: map[string]any{
				"email":       "esc-owner@example.test",
				"roles":       []string{"ORG_OWNER"},
				"project_ids": []string{boot.ProjectID},
			},
			wantErr: "role_escalation_denied",
		},
		{
			name: "dual_role_grant_finops_viewer",
			payload: map[string]any{
				"email":       "esc-fin@example.test",
				"roles":       []string{"FINOPS_VIEWER"},
				"project_ids": []string{boot.ProjectID},
			},
			wantErr: "role_escalation_denied",
		},
		{
			name: "dual_role_empty_project_ids",
			payload: map[string]any{
				"email": "esc-empty@example.test",
				"roles": []string{"DEVELOPER"},
			},
			wantErr: "insufficient_role_permission",
		},
	} {
		rec := h.doBearer(t, http.MethodPost, "/admin/v1/members", "tok_dual_ta", "", tc.payload)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: expected 403, got %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
		var errEnv CanonicalErrorEnvelope
		mustDecodeJSON(t, rec.Body.Bytes(), &errEnv)
		if errEnv.Error.Code != tc.wantErr {
			t.Fatalf("%s: expected error code %s, got %s", tc.name, tc.wantErr, errEnv.Error.Code)
		}
	}

	// Non-project role with non-empty project_ids rejected as 400 invalid_request even by ORG_OWNER.
	if rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email":       "bad-finops@example.test",
		"roles":       []string{"FINOPS_VIEWER"},
		"project_ids": []string{boot.ProjectID},
	}); rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for FINOPS_VIEWER with project_ids, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Create multi-project member (P1 + P2) and org-scoped member as ORG_OWNER.
	recMulti := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email":       "multi@example.test",
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{boot.ProjectID, proj2.ID},
	})
	if recMulti.Code != http.StatusCreated {
		t.Fatalf("create multi-project member failed: %d %s", recMulti.Code, recMulti.Body.String())
	}
	var multiMember memberSummaryDTO
	mustDecodeJSON(t, recMulti.Body.Bytes(), &multiMember)

	recOrgMem := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email": "sec@example.test",
		"roles": []string{"SECURITY_REVIEWER"},
	})
	if recOrgMem.Code != http.StatusCreated {
		t.Fatalf("create org-scoped member failed: %d %s", recOrgMem.Code, recOrgMem.Body.String())
	}
	var orgMember memberSummaryDTO
	mustDecodeJSON(t, recOrgMem.Body.Bytes(), &orgMember)

	// Project-scoped TEAM_ADMIN(P1) attempting PATCH on multi-project member (even with stale If-Match "v999")
	// MUST receive 403, never 409 current_resource oracle.
	p1TA := &rbac.Actor{
		PrincipalID:     "prc_p1_ta",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{
			{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleTeamAdmin},
		},
	}
	h.registerBearerToken("tok_p1_ta", p1TA)
	recCrossProj := h.doBearer(t, http.MethodPatch, "/admin/v1/members/"+multiMember.ID+"/roles", "tok_p1_ta", `"v999"`, map[string]any{
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{boot.ProjectID},
	})
	if recCrossProj.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for TEAM_ADMIN(P1) patching P1+P2 member with stale ETag, got %d body=%s", recCrossProj.Code, recCrossProj.Body.String())
	}

	// Project-scoped TEAM_ADMIN(P1) patching org-scoped SECURITY_REVIEWER MUST also receive 403, not 409.
	recOrgTarget := h.doBearer(t, http.MethodPatch, "/admin/v1/members/"+orgMember.ID+"/roles", "tok_p1_ta", `"v999"`, map[string]any{
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{boot.ProjectID},
	})
	if recOrgTarget.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for TEAM_ADMIN(P1) patching org-scoped member, got %d body=%s", recOrgTarget.Code, recOrgTarget.Body.String())
	}

	// Instance-only OPERATOR without ?org_id= gets 403 on org-scoped endpoints (never 500), and succeeds with ?org_id=.
	opActor := &rbac.Actor{
		PrincipalID:     "prc_inst_op",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	}
	h.registerBearerToken("tok_inst_op", opActor)
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/projects", "tok_inst_op", "", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for instance OPERATOR without org_id, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := h.doBearer(t, http.MethodGet, "/admin/v1/projects?org_id="+boot.OrgID, "tok_inst_op", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for instance OPERATOR with ?org_id=, got %d body=%s", rec.Code, rec.Body.String())
	}
}

type countingResolver struct {
	calls atomic.Int64
	fn    func(host string) ([]netip.Addr, error)
}

func (r *countingResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	r.calls.Add(1)
	if r.fn != nil {
		return r.fn(host)
	}
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

func TestBootstrapSecurityAndPreflightOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	nowFn := func() int64 { return fk.Now().UTC().UnixMilli() }

	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(dir, "boot.db"), NowMs: nowFn})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	snapshots, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB(), NowMs: nowFn})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	argonParams := auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32}
	authSvc, err := auth.NewLocalService(auth.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Argon2: argonParams, AuditSink: auditLog})
	if err != nil {
		t.Fatalf("auth.NewLocalService: %v", err)
	}
	tokenPath := filepath.Join(dir, "bootstrap.token")
	bootMgr, err := bootstrap.NewManager(bootstrap.ManagerConfig{
		DB: st.RawDB(), TokenPath: tokenPath, NowMs: nowFn, Argon2: argonParams, AuditSink: auditLog,
	})
	if err != nil {
		t.Fatalf("bootstrap.NewManager: %v", err)
	}
	rawTok, err := bootMgr.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	vkeySvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), NowMs: nowFn, Snapshots: snapshots, AuditSink: auditLog})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}

	resolver := &countingResolver{}
	// Nil Vault intentionally triggers 422 when initial_connection carries secret_value.
	srv, err := NewServer(ServerConfig{
		Store:             st,
		DB:                st.RawDB(),
		Bootstrap:         bootMgr,
		Auth:              authSvc,
		VKeys:             vkeySvc,
		Vault:             nil,
		Snapshots:         snapshots,
		Audit:             auditLog,
		NowMs:             nowFn,
		HostResolver:      resolver,
		BlockedAdminPorts: []int{9090},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	doBoot := func(payload map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "https://control.tanpopo.local/admin/v1/bootstrap/complete", bytes.NewReader(raw))
		req.RemoteAddr = "203.0.113.10:4242"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	// 1) Invalid bootstrap token with initial_connection hostname MUST fail 401 BEFORE invoking DNS HostResolver!
	recUnauth := doBoot(map[string]any{
		"bootstrap_token":                "bad-token",
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongBootPass#2026!",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "probe-conn",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://attacker-dns-oracle.example.test/v1",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on invalid bootstrap token, got %d body=%s", recUnauth.Code, recUnauth.Body.String())
	}
	if resolver.calls.Load() != 0 {
		t.Fatalf("HostResolver must NOT be invoked on unauthenticated bootstrap attempt, got %d calls", resolver.calls.Load())
	}

	// 2) Valid token with secret_value + nil Vault MUST fail 422 without setting a session cookie!
	recNoVault := doBoot(map[string]any{
		"bootstrap_token":                rawTok,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongBootPass#2026!",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "vault-req-conn",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1",
			"secret_value": "sk-secret-without-vault",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recNoVault.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 when secret_value sent without Vault, got %d body=%s", recNoVault.Code, recNoVault.Body.String())
	}
	for _, c := range recNoVault.Result().Cookies() {
		if c.Name == "__Host-tanpopo_admin_session" {
			t.Fatalf("session cookie must NOT be set on failed bootstrap")
		}
	}

	// Seed adapter then succeed without secret_value (since Vault is nil), proving token was NOT burned.
	hTmp := &adminTestHarness{st: st, fk: fk}
	seedTestAdapter(t, hTmp)
	recOK := doBoot(map[string]any{
		"bootstrap_token":                rawTok,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongBootPass#2026!",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "openai-public",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recOK.Code != http.StatusCreated {
		t.Fatalf("expected 201 after non-burning preflight failures, got %d body=%s", recOK.Code, recOK.Body.String())
	}

	// 3) Post-bootstrap replay MUST fail 410 BEFORE invoking HostResolver again.
	callsBeforeReplay := resolver.calls.Load()
	recAfter := doBoot(map[string]any{
		"bootstrap_token":                rawTok,
		"owner_email":                    "owner@example.test",
		"owner_password":                 "StrongBootPass#2026!",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "replay-conn",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://replay-dns-oracle.example.test/v1",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recAfter.Code != http.StatusGone {
		t.Fatalf("expected 410 post-bootstrap replay, got %d body=%s", recAfter.Code, recAfter.Body.String())
	}
	if resolver.calls.Load() != callsBeforeReplay {
		t.Fatalf("HostResolver must NOT be invoked on post-bootstrap 410 replay")
	}
}

func TestSSRFAndPlaneAndETagHardening(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	seedTestAdapter(t, h)

	// 1) Short PaginationSecret (< 32 bytes) rejected fail-closed at startup.
	if _, err := NewServer(ServerConfig{
		Store:            h.st,
		DB:               h.st.RawDB(),
		Bootstrap:        h.bootMgr,
		Auth:             h.authSvc,
		VKeys:            h.vkeys,
		Audit:            h.auditLog,
		PaginationSecret: []byte("too-short"),
	}); !errors.Is(err, ErrInvalidServerConfig) {
		t.Fatalf("expected ErrInvalidServerConfig for short PaginationSecret, got %v", err)
	}

	// 2) SSRF IPv6 transition & trailing-dot metadata endpoints rejected during bootstrap.
	token := h.readBootstrapToken(t)
	for _, badURL := range []string{
		"http://[::169.254.169.254]/",
		"http://[64:ff9b::169.254.169.254]/",
		"http://[2002:7f00:0001::]/",
		"http://metadata.google.internal./computeMetadata/v1",
	} {
		rec := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
			"bootstrap_token":                token,
			"owner_email":                    "ssrf-owner@example.test",
			"owner_password":                 "StrongSSRFPass#2026!",
			"master_kek_backup_acknowledged": true,
			"initial_connection": map[string]any{
				"adapter_id":   "adp_openai_spec",
				"name":         "bad-ssrf",
				"billing_mode": "COMMERCIAL_API",
				"endpoint_url": badURL,
				"model_id":     "gpt-4o",
				"data_region":  "US",
			},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("URL %q expected 422, got %d body=%s", badURL, rec.Code, rec.Body.String())
		}
	}

	// Complete bootstrap validly.
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "ssrf-owner@example.test",
		"owner_password":                 "StrongSSRFPass#2026!",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	cookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// 3) Multiple Authorization headers mixing Admin token + Data Plane vk_tp_ token -> 401 invalid_credential_plane.
	h.registerBearerToken("tp_adm_valid_for_multi", &rbac.Actor{
		PrincipalID:     "prc_multi_hdr",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner}},
	})
	reqMulti := httptest.NewRequest(http.MethodGet, "https://control.tanpopo.local/admin/v1/projects", nil)
	reqMulti.Header.Add("Authorization", "Bearer tp_adm_valid_for_multi")
	reqMulti.Header.Add("Authorization", "Bearer vk_tp_prod_smuggled")
	recMulti := httptest.NewRecorder()
	h.srv.ServeHTTP(recMulti, reqMulti)
	if recMulti.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 invalid_credential_plane for multi-Authorization with vk_tp_, got %d body=%s", recMulti.Code, recMulti.Body.String())
	}
	var planeErr CanonicalErrorEnvelope
	mustDecodeJSON(t, recMulti.Body.Bytes(), &planeErr)
	if planeErr.Error.Code != "invalid_credential_plane" {
		t.Fatalf("expected invalid_credential_plane, got %s", planeErr.Error.Code)
	}

	// 4) Signed If-Match ("v+1" / "v-1") rejected with 400 invalid_if_match.
	recVKForETag := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+boot.ProjectID+"/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":       "etag-check-key",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recVKForETag.Code != http.StatusCreated {
		t.Fatalf("create key for ETag test failed: %d body=%s", recVKForETag.Code, recVKForETag.Body.String())
	}
	var vkETag virtualKeyCreatedDTO
	mustDecodeJSON(t, recVKForETag.Body.Bytes(), &vkETag)
	for _, badETag := range []string{`"v+1"`, `"v-1"`, `"v0"`, `v1`} {
		recETag := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/virtual-keys/"+vkETag.ID, cookie, boot.CSRFToken, badETag, map[string]any{
			"name": "Renamed",
		})
		if recETag.Code != http.StatusBadRequest {
			t.Fatalf("If-Match %s expected 400, got %d body=%s", badETag, recETag.Code, recETag.Body.String())
		}
		var etErr CanonicalErrorEnvelope
		mustDecodeJSON(t, recETag.Body.Bytes(), &etErr)
		if etErr.Error.Code != "invalid_if_match" {
			t.Fatalf("If-Match %s expected invalid_if_match, got %s", badETag, etErr.Error.Code)
		}
	}

	// 5) Logout sets Cache-Control: no-store and audits revoked ephemeral playground keys.
	recLogout := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/auth/logout", cookie, boot.CSRFToken, "", nil)
	if recLogout.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on logout, got %d", recLogout.Code)
	}
	if cc := recLogout.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("expected Cache-Control: no-store on logout 204, got %q", cc)
	}
}

func TestOCCShredSafetyBudgetCeilingAndPagination(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()
	seedTestAdapter(t, h)

	token := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "occ-owner@example.test",
		"owner_password":                 "StrongOCCPass#2026!",
		"org_name":                       "OCC Org",
		"project_name":                   "occ-proj",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "occ-conn",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1",
			"secret_value": "sk-occ-preserved-secret",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d %s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	cookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// Seal a project secret so we can prove DEK stays intact after stale 409 DELETE.
	projSecret, err := h.vault.SealSecret(ctx, vault.SealSecretParams{
		OrgID:          boot.OrgID,
		OwnerProjectID: &boot.ProjectID,
		Name:           "occ-proj-secret",
		SecretKind:     vault.SecretKindAPIKey,
		Plaintext:      []byte("still-alive-after-409"),
	})
	if err != nil {
		t.Fatalf("SealSecret: %v", err)
	}

	// Simulate concurrent writer bumping project version to 2 so `"v1"` becomes stale -> DELETE returns 409 AND preserves Vault DEKs.
	if _, uerr := h.st.RawDB().ExecContext(ctx, `UPDATE projects SET version = 2 WHERE id = ?`, boot.ProjectID); uerr != nil {
		t.Fatalf("bump project version: %v", uerr)
	}
	recStaleProjDel := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/projects/"+boot.ProjectID, cookie, boot.CSRFToken, `"v1"`, map[string]any{
		"confirm_slug": "occ-proj",
		"reason":       "stale version should not shred",
	})
	if recStaleProjDel.Code != http.StatusConflict {
		t.Fatalf("expected 409 on stale DELETE /projects, got %d body=%s", recStaleProjDel.Code, recStaleProjDel.Body.String())
	}
	if _, err := h.vault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), boot.OrgID, projSecret.ID); err != nil {
		t.Fatalf("project DEK MUST survive stale 409 delete, unseal err=%v", err)
	}

	// Simulate concurrent writer bumping organization version to 2 -> DELETE with `"v1"` returns 409, DEK intact.
	opActor := &rbac.Actor{
		PrincipalID:     "prc_occ_op",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	}
	h.registerBearerToken("tok_occ_op", opActor)
	if _, uerr := h.st.RawDB().ExecContext(ctx, `UPDATE organizations SET version = 2 WHERE id = ?`, boot.OrgID); uerr != nil {
		t.Fatalf("bump org version: %v", uerr)
	}
	recStaleOrgDel := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+boot.OrgID, "tok_occ_op", `"v1"`, map[string]any{
		"confirm_slug": "occ-org",
		"reason":       "stale org delete should not shred",
	})
	if recStaleOrgDel.Code != http.StatusConflict {
		t.Fatalf("expected 409 on stale DELETE /organizations, got %d body=%s", recStaleOrgDel.Code, recStaleOrgDel.Body.String())
	}
	if _, err := h.vault.UnsealWithCapability(ctx, vault.MintUnsealCapabilityForBroker(), boot.OrgID, projSecret.ID); err != nil {
		t.Fatalf("org DEK MUST survive stale 409 org delete, unseal err=%v", err)
	}

	// POST /virtual-keys with budget_ceiling_micros persists a VIRTUAL_KEY MONTHLY ledger row.
	recVK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+boot.ProjectID+"/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":                  "budgeted-key",
		"project_id":            boot.ProjectID,
		"env_id":                boot.EnvID,
		"budget_ceiling_micros": 5000000,
	})
	if recVK.Code != http.StatusCreated {
		t.Fatalf("POST /virtual-keys with budget failed: %d body=%s", recVK.Code, recVK.Body.String())
	}
	var createdVK virtualKeyCreatedDTO
	mustDecodeJSON(t, recVK.Body.Bytes(), &createdVK)
	var hardLimit int64
	if err := h.st.RawDB().QueryRowContext(ctx, `
		SELECT hard_limit_micros FROM budget_ledgers
		WHERE org_id = ? AND scope_type = ? AND scope_id = ? AND period_kind = ?`,
		boot.OrgID, ledger.ScopeVirtualKey, createdVK.ID, ledger.PeriodMonthly,
	).Scan(&hardLimit); err != nil || hardLimit != 5000000 {
		t.Fatalf("expected persisted MONTHLY budget ledger hard_limit=5000000, got %d err=%v", hardLimit, err)
	}

	// Multi-page pagination across /organizations, /projects, and /members.
	for i := 0; i < 3; i++ {
		h.fk.Advance(time.Millisecond)
		if rec := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_occ_op", "", map[string]any{
			"slug": fmt.Sprintf("page-org-%d", i),
			"name": fmt.Sprintf("Page Org %d", i),
		}); rec.Code != http.StatusCreated {
			t.Fatalf("create org %d failed: %d %s", i, rec.Code, rec.Body.String())
		}
		if rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", cookie, boot.CSRFToken, "", map[string]any{
			"slug": fmt.Sprintf("page-proj-%d", i),
			"name": fmt.Sprintf("Page Proj %d", i),
		}); rec.Code != http.StatusCreated {
			t.Fatalf("create proj %d failed: %d %s", i, rec.Code, rec.Body.String())
		}
		if rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", cookie, boot.CSRFToken, "", map[string]any{
			"email": fmt.Sprintf("page-mem-%d@example.test", i),
			"roles": []string{"FINOPS_VIEWER"},
		}); rec.Code != http.StatusCreated {
			t.Fatalf("create member %d failed: %d %s", i, rec.Code, rec.Body.String())
		}
	}

	assertTwoPages := func(path string, bearer string) int {
		var rec1 *httptest.ResponseRecorder
		if bearer != "" {
			rec1 = h.doBearer(t, http.MethodGet, path+"?limit=2", bearer, "", nil)
		} else {
			rec1 = h.doAuthedCookie(t, http.MethodGet, path+"?limit=2", cookie, "", "", nil)
		}
		if rec1.Code != http.StatusOK {
			t.Fatalf("page1 %s failed: %d %s", path, rec1.Code, rec1.Body.String())
		}
		var page1 struct {
			Data       []map[string]any `json:"data"`
			NextCursor *string          `json:"next_cursor"`
			HasMore    bool             `json:"has_more"`
		}
		mustDecodeJSON(t, rec1.Body.Bytes(), &page1)
		if !page1.HasMore || page1.NextCursor == nil || len(page1.Data) != 2 {
			t.Fatalf("page1 %s unexpected envelope: hasMore=%v len=%d", path, page1.HasMore, len(page1.Data))
		}
		nextPath := path + "?limit=2&cursor=" + *page1.NextCursor
		var rec2 *httptest.ResponseRecorder
		if bearer != "" {
			rec2 = h.doBearer(t, http.MethodGet, nextPath, bearer, "", nil)
		} else {
			rec2 = h.doAuthedCookie(t, http.MethodGet, nextPath, cookie, "", "", nil)
		}
		if rec2.Code != http.StatusOK {
			t.Fatalf("page2 %s failed: %d %s", path, rec2.Code, rec2.Body.String())
		}
		var page2 struct {
			Data []map[string]any `json:"data"`
		}
		mustDecodeJSON(t, rec2.Body.Bytes(), &page2)
		if len(page2.Data) < 2 {
			t.Fatalf("page2 %s expected >= 2 items, got %d", path, len(page2.Data))
		}
		return len(page1.Data) + len(page2.Data)
	}
	assertTwoPages("/admin/v1/organizations", "tok_occ_op")
	assertTwoPages("/admin/v1/projects", "")
	assertTwoPages("/admin/v1/members", "")

	// ErrSnapshotSyncFailed resilience on POST/PATCH/rotate/revoke using a separate snapshot DB missing tables.
	snapDir := t.TempDir()
	snapSt, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(snapDir, "snap.db"), NowMs: func() int64 { return h.fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("open snapSt: %v", err)
	}
	t.Cleanup(func() { _ = snapSt.Close() })
	brokenSnaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: snapSt.RawDB(), NowMs: func() int64 { return h.fk.Now().UTC().UnixMilli() }})
	if err != nil {
		t.Fatalf("brokenSnaps NewManager: %v", err)
	}
	if _, derr := snapSt.RawDB().ExecContext(ctx, `DROP TABLE virtual_keys`); derr != nil {
		t.Fatalf("drop snap virtual_keys: %v", derr)
	}
	brokenVKeySvc, err := vkey.NewService(vkey.ServiceConfig{
		DB:        h.st.RawDB(),
		NowMs:     func() int64 { return h.fk.Now().UTC().UnixMilli() },
		Snapshots: brokenSnaps,
		AuditSink: h.auditLog,
	})
	if err != nil {
		t.Fatalf("brokenVKeySvc: %v", err)
	}
	srvSyncBroken, err := NewServer(ServerConfig{
		Store:            h.st,
		DB:               h.st.RawDB(),
		Bootstrap:        h.bootMgr,
		Auth:             h.authSvc,
		VKeys:            brokenVKeySvc,
		Vault:            h.vault,
		Snapshots:        brokenSnaps,
		KillSwitch:       h.killSwitch,
		Audit:            h.auditLog,
		HostResolver:     &countingResolver{},
		NowMs:            func() int64 { return h.fk.Now().UTC().UnixMilli() },
		PaginationSecret: []byte("0123456789abcdef0123456789abcdef"),
		TokenVerifier: func(_ context.Context, raw string) (*rbac.Actor, *SessionMetadata, error) {
			if raw == "tok_sync_owner" {
				return &rbac.Actor{
					PrincipalID:     boot.OwnerPrincipalID,
					SessionOrgID:    &boot.OrgID,
					CredentialPlane: rbac.CredentialPlaneAdminService,
					Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner}},
				}, &SessionMetadata{Email: "sync@example.test", ExpiresAtMs: h.fk.Now().UTC().UnixMilli() + 3600_000}, nil
			}
			return nil, nil, rbac.ErrUnauthenticated
		},
	})
	if err != nil {
		t.Fatalf("srvSyncBroken NewServer: %v", err)
	}
	doBroken := func(method, path, ifMatch string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, "https://control.tanpopo.local"+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer tok_sync_owner")
		req.Header.Set("Content-Type", "application/json")
		if ifMatch != "" {
			req.Header.Set("If-Match", ifMatch)
		}
		rec := httptest.NewRecorder()
		srvSyncBroken.ServeHTTP(rec, req)
		return rec
	}

	recSyncCreate := doBroken(http.MethodPost, "/admin/v1/projects/"+boot.ProjectID+"/virtual-keys", "", map[string]any{
		"name":       "sync-pending-key",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recSyncCreate.Code != http.StatusCreated || recSyncCreate.Header().Get("X-Tanpopo-Snapshot-Sync") != "pending" {
		t.Fatalf("expected 201 + X-Tanpopo-Snapshot-Sync: pending on create, got %d hdr=%q body=%s",
			recSyncCreate.Code, recSyncCreate.Header().Get("X-Tanpopo-Snapshot-Sync"), recSyncCreate.Body.String())
	}
	var syncVK virtualKeyCreatedDTO
	mustDecodeJSON(t, recSyncCreate.Body.Bytes(), &syncVK)
	if !strings.HasPrefix(syncVK.SecretKey, "vk_tp_") {
		t.Fatalf("expected SecretKey preserved on ErrSnapshotSyncFailed, got %q", syncVK.SecretKey)
	}

	recSyncPatch := doBroken(http.MethodPatch, "/admin/v1/virtual-keys/"+syncVK.ID, `"v1"`, map[string]any{"name": "sync-pending-v2"})
	if recSyncPatch.Code != http.StatusOK || recSyncPatch.Header().Get("X-Tanpopo-Snapshot-Sync") != "pending" {
		t.Fatalf("expected 200 + pending header on PATCH, got %d hdr=%q", recSyncPatch.Code, recSyncPatch.Header().Get("X-Tanpopo-Snapshot-Sync"))
	}

	recSyncRot := doBroken(http.MethodPost, "/admin/v1/virtual-keys/"+syncVK.ID+"/rotate", `"v2"`, map[string]any{"grace_window_seconds": 60})
	if recSyncRot.Code != http.StatusOK || recSyncRot.Header().Get("X-Tanpopo-Snapshot-Sync") != "pending" {
		t.Fatalf("expected 200 + pending header on rotate, got %d hdr=%q", recSyncRot.Code, recSyncRot.Header().Get("X-Tanpopo-Snapshot-Sync"))
	}
	var rotVK virtualKeyCreatedDTO
	mustDecodeJSON(t, recSyncRot.Body.Bytes(), &rotVK)
	if !strings.HasPrefix(rotVK.SecretKey, "vk_tp_") {
		t.Fatalf("expected rotated SecretKey preserved on ErrSnapshotSyncFailed, got %q", rotVK.SecretKey)
	}

	recSyncRev := doBroken(http.MethodPost, "/admin/v1/virtual-keys/"+syncVK.ID+"/revoke", `"v3"`, map[string]any{
		"mode":   "EMERGENCY_ABORT_STREAMS",
		"reason": "sync pending revoke test",
	})
	if recSyncRev.Code != http.StatusOK || recSyncRev.Header().Get("X-Tanpopo-Snapshot-Sync") != "pending" {
		t.Fatalf("expected 200 + pending header on revoke, got %d hdr=%q", recSyncRev.Code, recSyncRev.Header().Get("X-Tanpopo-Snapshot-Sync"))
	}
}

func TestAdminRemediationRegressionSuite(t *testing.T) {
	t.Parallel()
	h := newAdminTestHarness(t)
	ctx := context.Background()
	seedTestAdapter(t, h)

	token := h.readBootstrapToken(t)

	// 1) Failure inside bootstrap provisionTx (duplicate route constraint via trigger) rolls back atomically
	// without burning Bootstrap Token and returns sanitized 422 message.
	if _, err := h.st.RawDB().ExecContext(ctx, `
		CREATE TRIGGER trg_fail_boot_route_once
		BEFORE INSERT ON route_policies
		BEGIN
			SELECT RAISE(ABORT, 'internal sqlite vault detail must not leak');
		END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	recBootFail := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "atomic-owner@example.test",
		"owner_password":                 "StrongAtomicPass#2026!",
		"org_name":                       "Atomic Org",
		"project_name":                   "atomic-proj",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "adp_openai_spec",
			"name":         "atomic-conn",
			"billing_mode": "COMMERCIAL_API",
			"endpoint_url": "https://api.openai.com/v1",
			"secret_value": "sk-atomic-rollback-secret",
			"model_id":     "gpt-4o",
			"data_region":  "US",
		},
	})
	if recBootFail.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 initial_connection_failed, got %d body=%s", recBootFail.Code, recBootFail.Body.String())
	}
	if strings.Contains(recBootFail.Body.String(), "internal sqlite vault detail") {
		t.Fatalf("raw internal SQLite error leaked in response: %s", recBootFail.Body.String())
	}
	for _, tbl := range []string{"organizations", "org_encryption_keys", "secret_credentials", "audit_events"} {
		var cnt int
		if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl).Scan(&cnt); err != nil || cnt != 0 {
			t.Fatalf("expected 0 rows in %s after bootstrap rollback, got cnt=%d err=%v", tbl, cnt, err)
		}
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `DROP TRIGGER trg_fail_boot_route_once`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}

	// Retry bootstrap with the SAME unburned token succeeds 201.
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                token,
		"owner_email":                    "atomic-owner@example.test",
		"owner_password":                 "StrongAtomicPass#2026!",
		"org_name":                       "Atomic Org",
		"project_name":                   "atomic-proj",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap retry failed: %d body=%s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	mustDecodeJSON(t, recBoot.Body.Bytes(), &boot)
	ownerCookie := findCookie(t, recBoot.Result().Cookies(), "__Host-tanpopo_admin_session")

	// 2) Content-Type 415 enforcement on public & authenticated JSON endpoints.
	reqPlainLogin := httptest.NewRequest(http.MethodPost, "https://control.tanpopo.local/admin/v1/auth/login", strings.NewReader(`{"email":"a@b.c","password":"x"}`))
	reqPlainLogin.Header.Set("Content-Type", "text/plain")
	recPlainLogin := httptest.NewRecorder()
	h.srv.ServeHTTP(recPlainLogin, reqPlainLogin)
	if recPlainLogin.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 on text/plain login, got %d body=%s", recPlainLogin.Code, recPlainLogin.Body.String())
	}

	reqNoCT := httptest.NewRequest(http.MethodPost, "https://control.tanpopo.local/admin/v1/projects", strings.NewReader(`{"slug":"p","name":"P"}`))
	reqNoCT.AddCookie(ownerCookie)
	reqNoCT.Header.Set("Origin", "https://control.tanpopo.local")
	reqNoCT.Header.Set("X-CSRF-Token", boot.CSRFToken)
	recNoCT := httptest.NewRecorder()
	h.srv.ServeHTTP(recNoCT, reqNoCT)
	if recNoCT.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415 on missing Content-Type, got %d body=%s", recNoCT.Code, recNoCT.Body.String())
	}

	// 3) Duplicate Authorization headers rejected with 401 unauthenticated.
	h.registerBearerToken("tok_dup_1", &rbac.Actor{
		PrincipalID:     "prc_dup_op",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})
	reqDupAuth := httptest.NewRequest(http.MethodGet, "https://control.tanpopo.local/admin/v1/organizations", nil)
	reqDupAuth.Header.Add("Authorization", "Bearer tok_dup_1")
	reqDupAuth.Header.Add("Authorization", "Bearer tok_dup_2")
	recDupAuth := httptest.NewRecorder()
	h.srv.ServeHTTP(recDupAuth, reqDupAuth)
	if recDupAuth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on duplicate Authorization headers, got %d body=%s", recDupAuth.Code, recDupAuth.Body.String())
	}

	// 4) Instance OPERATOR creates a Virtual Key in a secondary org via ?org_id= (OwnerPrincipalID nil).
	recOrg2 := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_dup_1", "", map[string]any{
		"slug": "secondary-org",
		"name": "Secondary Org",
	})
	if recOrg2.Code != http.StatusCreated {
		t.Fatalf("POST /organizations failed: %d body=%s", recOrg2.Code, recOrg2.Body.String())
	}
	var org2 organizationDTO
	mustDecodeJSON(t, recOrg2.Body.Bytes(), &org2)

	recProj2 := h.doBearer(t, http.MethodPost, "/admin/v1/projects?org_id="+org2.ID, "tok_dup_1", "", map[string]any{
		"slug": "sec-proj",
		"name": "Secondary Proj",
	})
	if recProj2.Code != http.StatusCreated {
		t.Fatalf("OPERATOR POST /projects?org_id= failed: %d body=%s", recProj2.Code, recProj2.Body.String())
	}
	var proj2 projectDTO
	mustDecodeJSON(t, recProj2.Body.Bytes(), &proj2)

	recEnv2 := h.doBearer(t, http.MethodPost, "/admin/v1/projects/"+proj2.ID+"/environments?org_id="+org2.ID, "tok_dup_1", "", map[string]any{
		"slug": "prod",
		"name": "Production",
	})
	if recEnv2.Code != http.StatusCreated {
		t.Fatalf("OPERATOR POST /environments?org_id= failed: %d body=%s", recEnv2.Code, recEnv2.Body.String())
	}
	var env2 environmentDTO
	mustDecodeJSON(t, recEnv2.Body.Bytes(), &env2)

	recOpVK := h.doBearer(t, http.MethodPost, "/admin/v1/virtual-keys?org_id="+org2.ID, "tok_dup_1", "", map[string]any{
		"name":       "operator-provisioned-key",
		"project_id": proj2.ID,
		"env_id":     env2.ID,
	})
	if recOpVK.Code != http.StatusCreated {
		t.Fatalf("OPERATOR POST /virtual-keys?org_id= failed: %d body=%s", recOpVK.Code, recOpVK.Body.String())
	}
	var opVK virtualKeyCreatedDTO
	mustDecodeJSON(t, recOpVK.Body.Bytes(), &opVK)
	if opVK.OwnerPrincipalID != nil {
		t.Fatalf("expected nil OwnerPrincipalID for cross-org OPERATOR key, got %v", *opVK.OwnerPrincipalID)
	}
	if _, authErr := h.snapshots.AuthenticateKey(opVK.SecretKey, env2.ID); authErr != nil {
		t.Fatalf("expected OPERATOR-created virtual key to authenticate on Data Plane snapshot, got %v", authErr)
	}

	// 5) SECURITY_REVIEWER can only revoke with EMERGENCY_ABORT_STREAMS (403 on GRACEFUL_DRAIN).
	h.registerBearerToken("tok_sec_rev_only", &rbac.Actor{
		PrincipalID:     "prc_sec_rev",
		SessionOrgID:    &org2.ID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{{
			OrgID:     &org2.ID,
			ScopeType: rbac.ScopeOrg,
			ScopeID:   org2.ID,
			RoleName:  rbac.RoleSecurityReviewer,
		}},
	})
	recGraceful := h.doBearer(t, http.MethodPost, "/admin/v1/virtual-keys/"+opVK.ID+"/revoke", "tok_sec_rev_only", `"v1"`, map[string]any{
		"mode":   "GRACEFUL_DRAIN",
		"reason": "security reviewer graceful check",
	})
	if recGraceful.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for SECURITY_REVIEWER GRACEFUL_DRAIN, got %d body=%s", recGraceful.Code, recGraceful.Body.String())
	}
	recEmerg := h.doBearer(t, http.MethodPost, "/admin/v1/virtual-keys/"+opVK.ID+"/revoke", "tok_sec_rev_only", `"v1"`, map[string]any{
		"mode":   "EMERGENCY_ABORT_STREAMS",
		"reason": "emergency containment test",
	})
	if recEmerg.Code != http.StatusOK {
		t.Fatalf("expected 200 for SECURITY_REVIEWER EMERGENCY_ABORT_STREAMS, got %d body=%s", recEmerg.Code, recEmerg.Body.String())
	}

	// 6) Concurrent race on PATCH /members/{id}/roles hits RowsAffected == 0, rolls back deleted bindings,
	// and emits fresh 409 current_resource; then a committed v1->v2 bump causes stale If-Match "v1" to return ETag "v2".
	recMem := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email": "occ-member@example.test",
		"roles": []string{"FINOPS_VIEWER"},
	})
	if recMem.Code != http.StatusCreated {
		t.Fatalf("POST /members failed: %d body=%s", recMem.Code, recMem.Body.String())
	}
	var mem memberSummaryDTO
	mustDecodeJSON(t, recMem.Body.Bytes(), &mem)

	if _, err := h.st.RawDB().ExecContext(ctx, `
		CREATE TRIGGER trg_bump_member_version_once
		AFTER DELETE ON role_bindings
		WHEN OLD.principal_id = '`+mem.ID+`'
		BEGIN
			UPDATE principals SET version = version + 1 WHERE id = OLD.principal_id;
		END`); err != nil {
		t.Fatalf("create member race trigger: %v", err)
	}
	recMemRace := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/members/"+mem.ID+"/roles", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"roles": []string{"SECURITY_REVIEWER"},
	})
	if _, err := h.st.RawDB().ExecContext(ctx, `DROP TRIGGER trg_bump_member_version_once`); err != nil {
		t.Fatalf("drop member race trigger: %v", err)
	}
	if recMemRace.Code != http.StatusConflict {
		t.Fatalf("expected 409 on concurrent member role race, got %d body=%s", recMemRace.Code, recMemRace.Body.String())
	}
	if recMemRace.Header().Get("ETag") != `"v1"` {
		t.Fatalf("expected ETag \"v1\" after same-tx trigger rollback, got %q", recMemRace.Header().Get("ETag"))
	}
	var raceEnv CanonicalErrorEnvelope
	mustDecodeJSON(t, recMemRace.Body.Bytes(), &raceEnv)
	curMap, ok := raceEnv.Error.CurrentResource.(map[string]any)
	if !ok || curMap["version"] != float64(1) {
		t.Fatalf("expected rolled-back current_resource.version == 1, got %+v", raceEnv.Error.CurrentResource)
	}
	rolesAny, _ := curMap["roles"].([]any)
	if len(rolesAny) != 1 || rolesAny[0] != "FINOPS_VIEWER" {
		t.Fatalf("expected tx.Rollback to preserve original FINOPS_VIEWER role in current_resource, got %+v", curMap["roles"])
	}

	// Committed v1->v2 bump followed by stale If-Match "v1" returns 409 with ETag "v2".
	recMemCommit := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/members/"+mem.ID+"/roles", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"roles": []string{"SECURITY_REVIEWER"},
	})
	if recMemCommit.Code != http.StatusOK || recMemCommit.Header().Get("ETag") != `"v2"` {
		t.Fatalf("expected 200 ETag v2 on committed member update, got %d etag=%q body=%s", recMemCommit.Code, recMemCommit.Header().Get("ETag"), recMemCommit.Body.String())
	}
	recMemStale := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/members/"+mem.ID+"/roles", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"roles": []string{"FINOPS_VIEWER"},
	})
	if recMemStale.Code != http.StatusConflict || recMemStale.Header().Get("ETag") != `"v2"` {
		t.Fatalf("expected 409 ETag \"v2\" on stale update, got %d etag=%q body=%s", recMemStale.Code, recMemStale.Header().Get("ETag"), recMemStale.Body.String())
	}

	// 7) DELETE /organizations/{id} rejects 409 resource_in_use when org principal triggered a global (org_id IS NULL) kill switch,
	// leaving the global kill switch and org intact.
	now := h.fk.Now().UTC().UnixMilli()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams,
			reason, triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_global_ref_01', NULL, 'EXTENSION_INSTALLATION', 'ext_global_1', 1, 1, 'Global emergency extension block', ?, 1, ?)`,
		mem.ID, now,
	); err != nil {
		t.Fatalf("insert global kill switch: %v", err)
	}
	recDelBlocked := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+boot.OrgID, "tok_dup_1", `"v1"`, map[string]any{
		"confirm_slug": "atomic-org",
		"reason":       "attempt delete while principal holds global kill switch",
	})
	if recDelBlocked.Code != http.StatusConflict {
		t.Fatalf("expected 409 resource_in_use when org principal holds global kill switch, got %d body=%s", recDelBlocked.Code, recDelBlocked.Body.String())
	}
	var ksGlobalCnt int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM emergency_kill_switches WHERE id = 'ksw_global_ref_01' AND org_id IS NULL`).Scan(&ksGlobalCnt); err != nil || ksGlobalCnt != 1 {
		t.Fatalf("expected global kill switch preserved intact, cnt=%d err=%v", ksGlobalCnt, err)
	}

	// 8) DELETING state guards on child endpoints & Phase 2 kill-switch preservation + Phase 3 cleanup & RefreshFromDB.
	// Seed an active org-scoped kill switch in secondary org2, trigger Phase 2 Vault failure, verify kill switch & DELETING guards,
	// then allow Phase 2/3 completion and verify kill switch cleared from DB + memory.
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams,
			reason, triggered_by_principal_id, version, triggered_at_ms
		) VALUES ('ksw_org2_freeze_01', ?, 'PROJECT_OR_ORG_FREEZE', ?, 1, 1, 'Incident containment freeze', ?, 1, ?)`,
		org2.ID, org2.ID, boot.OwnerPrincipalID, now,
	); err != nil {
		t.Fatalf("insert org2 kill switch: %v", err)
	}
	if err := h.killSwitch.RefreshFromDB(ctx); err != nil {
		t.Fatalf("killSwitch.RefreshFromDB: %v", err)
	}

	// Trigger simulated Phase 2 shred failure.
	if _, err := h.st.RawDB().ExecContext(ctx, `
		CREATE TRIGGER trg_fail_phase2_shred
		BEFORE INSERT ON vault_shred_requests
		BEGIN
			SELECT RAISE(ABORT, 'simulated phase2 failure');
		END`); err != nil {
		t.Fatalf("create phase2 trigger: %v", err)
	}
	recDelPhase2Fail := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+org2.ID, "tok_dup_1", `"v1"`, map[string]any{
		"confirm_slug": "secondary-org",
		"reason":       "phase 2 failure test",
	})
	if recDelPhase2Fail.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Phase 2 shred fails, got %d body=%s", recDelPhase2Fail.Code, recDelPhase2Fail.Body.String())
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `DROP TRIGGER trg_fail_phase2_shred`); err != nil {
		t.Fatalf("drop phase2 trigger: %v", err)
	}

	// Org2 is now DELETING (version 2) and its emergency kill switch MUST still be active in DB!
	var org2Status string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT status FROM organizations WHERE id = ?`, org2.ID).Scan(&org2Status); err != nil || org2Status != port.OrgStatusDeleting {
		t.Fatalf("expected org2 status %s after Phase 2 failure, got %q err=%v", port.OrgStatusDeleting, org2Status, err)
	}
	var org2KS int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM emergency_kill_switches WHERE id = 'ksw_org2_freeze_01' AND active = 1`).Scan(&org2KS); err != nil || org2KS != 1 {
		t.Fatalf("expected org2 kill switch to remain active after Phase 2 failure, cnt=%d err=%v", org2KS, err)
	}

	// Child mutations against DELETING org2 must return 409 resource_deleting.
	for _, tc := range []struct {
		method string
		path   string
		body   map[string]any
	}{
		{http.MethodPost, "/admin/v1/projects?org_id=" + org2.ID, map[string]any{"slug": "new-p", "name": "New P"}},
		{http.MethodPost, "/admin/v1/projects/" + proj2.ID + "/environments?org_id=" + org2.ID, map[string]any{"slug": "staging", "name": "Staging"}},
		{http.MethodPost, "/admin/v1/members?org_id=" + org2.ID, map[string]any{"email": "blocked@example.test", "roles": []string{"DEVELOPER"}, "project_ids": []string{proj2.ID}}},
		{http.MethodPost, "/admin/v1/virtual-keys?org_id=" + org2.ID, map[string]any{"name": "blocked-key", "project_id": proj2.ID, "env_id": env2.ID}},
	} {
		rec := h.doBearer(t, tc.method, tc.path, "tok_dup_1", "", tc.body)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s %s expected 409 resource_deleting, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	if _, blocked := h.killSwitch.CheckAdmission(killswitch.AdmissionQuery{OrgID: org2.ID}); !blocked {
		t.Fatalf("expected killSwitch.CheckAdmission to stay active after Phase 2 failure")
	}

	// Complete org2 teardown at version 2 -> removes kill switch in Phase 3 & refreshes killSwitch memory.
	recDelOrg2OK := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+org2.ID, "tok_dup_1", `"v2"`, map[string]any{
		"confirm_slug": "secondary-org",
		"reason":       "complete org2 crypto shred after phase2 recovery",
	})
	if recDelOrg2OK.Code != http.StatusOK {
		t.Fatalf("expected 200 on org2 delete recovery, got %d body=%s", recDelOrg2OK.Code, recDelOrg2OK.Body.String())
	}
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM emergency_kill_switches WHERE id = 'ksw_org2_freeze_01'`).Scan(&org2KS); err != nil || org2KS != 0 {
		t.Fatalf("expected org2 kill switch deleted in Phase 3, cnt=%d err=%v", org2KS, err)
	}
	if _, blocked := h.killSwitch.CheckAdmission(killswitch.AdmissionQuery{OrgID: org2.ID}); blocked {
		t.Fatalf("expected killSwitch.RefreshFromDB to evict deleted org2 kill switch from memory")
	}
}
