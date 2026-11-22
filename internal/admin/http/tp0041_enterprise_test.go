// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/oidc"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/foureyes"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

type httpFakeOIDCIdP struct {
	mu           sync.Mutex
	rsaKey       *rsa.PrivateKey
	ecKey        *ecdsa.PrivateKey
	activeAlg    string
	activeKID    string
	userGroups   map[string][]string
	pendingCodes map[string]struct{ sub, email, nonce string }
	forgedNext   bool
	srv          *httptest.Server
}

func newHTTPFakeOIDCIdP(t *testing.T) *httpFakeOIDCIdP {
	t.Helper()
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	f := &httpFakeOIDCIdP{
		rsaKey:     rsaPriv,
		ecKey:      ecPriv,
		activeAlg:  "RS256",
		activeKID:  "rsa-http-1",
		userGroups: map[string][]string{},
		pendingCodes: map[string]struct {
			sub, email, nonce string
		}{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *httpFakeOIDCIdP) sign(hdr, claims map[string]any, alg string) string {
	hRaw, _ := json.Marshal(hdr)
	cRaw, _ := json.Marshal(claims)
	input := base64.RawURLEncoding.EncodeToString(hRaw) + "." + base64.RawURLEncoding.EncodeToString(cRaw)
	sum := sha256.Sum256([]byte(input))
	var sig []byte
	switch alg {
	case "RS256":
		sig, _ = rsa.SignPKCS1v15(rand.Reader, f.rsaKey, crypto.SHA256, sum[:])
	case "ES256":
		r, s, _ := ecdsa.Sign(rand.Reader, f.ecKey, sum[:])
		sig = make([]byte, 64)
		r.FillBytes(sig[:32])
		s.FillBytes(sig[32:])
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *httpFakeOIDCIdP) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/jwks":
		eInt := f.rsaKey.E
		var eBytes []byte
		for v := eInt; v > 0; v >>= 8 {
			eBytes = append([]byte{byte(v & 0xff)}, eBytes...)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "rsa-http-1",
					"n": base64.RawURLEncoding.EncodeToString(f.rsaKey.N.Bytes()),
					"e": base64.RawURLEncoding.EncodeToString(eBytes),
				},
				{
					"kty": "EC", "use": "sig", "alg": "ES256", "kid": "ec-http-1", "crv": "P-256",
					"x": base64.RawURLEncoding.EncodeToString(f.ecKey.X.Bytes()),
					"y": base64.RawURLEncoding.EncodeToString(f.ecKey.Y.Bytes()),
				},
			},
		})
	case "/token":
		_ = r.ParseForm()
		var sub, email, nonce string
		switch r.FormValue("grant_type") {
		case "authorization_code":
			g := f.pendingCodes[r.FormValue("code")]
			delete(f.pendingCodes, r.FormValue("code"))
			sub, email, nonce = g.sub, g.email, g.nonce
		case "refresh_token":
			sub = r.FormValue("refresh_token")
			email = sub + "@example.test"
		}
		now := time.Now().UTC()
		alg := f.activeAlg
		if f.forgedNext {
			f.forgedNext = false
			alg = "none"
		}
		idTok := f.sign(
			map[string]any{"alg": alg, "kid": f.activeKID, "typ": "JWT"},
			map[string]any{
				"iss": f.srv.URL, "aud": "tanpopo-http-oidc", "sub": sub,
				"email": email, "nonce": nonce, "groups": f.userGroups[sub],
				"exp": now.Add(10 * time.Minute).Unix(),
			},
			alg,
		)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at_" + sub, "id_token": idTok, "refresh_token": sub, "token_type": "Bearer",
		})
	}
}

func TestTPIMP0041HTTP_OIDCLoginRefreshRevocationAndFourEyesKillSwitchClear(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(dir, "tp0041.db")})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	aud, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB()})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	ksReg, err := killswitch.NewRegistry(ctx, killswitch.RegistryConfig{DB: st.RawDB(), Audit: aud})
	if err != nil {
		t.Fatalf("killswitch.NewRegistry: %v", err)
	}
	a2 := auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32}
	authSvc, err := auth.NewLocalService(auth.ServiceConfig{DB: st.RawDB(), AuditSink: aud, Argon2: a2})
	if err != nil {
		t.Fatalf("auth.NewLocalService: %v", err)
	}
	bootMgr, err := bootstrap.NewManager(bootstrap.ManagerConfig{DB: st.RawDB(), TokenPath: filepath.Join(dir, "boot.tok"), AuditSink: aud, Argon2: a2})
	if err != nil {
		t.Fatalf("bootstrap.NewManager: %v", err)
	}
	rawBootTok, _ := bootMgr.EnsureToken(ctx)
	bootRes, err := bootMgr.Complete(ctx, bootstrap.CompleteRequest{
		BootstrapToken: rawBootTok, OwnerEmail: "owner1@example.test", OwnerPassword: "StrongOwnerPass#2026",
		OrgName: "TP41 Org", ProjectName: "tp41-proj", MasterKEKBackupAcknowledged: true,
	})
	if err != nil {
		t.Fatalf("bootstrap.Complete: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = 1 WHERE id = ?`, bootRes.OrgID); err != nil {
		t.Fatalf("enable four eyes: %v", err)
	}

	idp := newHTTPFakeOIDCIdP(t)
	idpURL, _ := url.Parse(idp.srv.URL)
	oidcProv, err := oidc.NewProvider(oidc.Config{
		OrgID:                 bootRes.OrgID,
		Issuer:                idp.srv.URL,
		ClientID:              "tanpopo-http-oidc",
		RedirectURI:           "https://console.tanpopo.local/admin/v1/auth/oidc/callback",
		AuthorizationEndpoint: idp.srv.URL + "/authorize",
		TokenEndpoint:         idp.srv.URL + "/token",
		JWKSURI:               idp.srv.URL + "/jwks",
		GroupMappings: []oidc.GroupRoleMapping{
			{Group: "tanpopo-sec-reviewers", OrgID: bootRes.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: bootRes.OrgID, RoleName: rbac.RoleSecurityReviewer},
		},
		DB:        st.RawDB(),
		Auth:      authSvc,
		AuditSink: aud,
		EgressPolicy: &egress.Policy{
			AllowedHosts: []string{idpURL.Host}, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.0/8"}, RequireTLS: false,
		},
	})
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	snaps, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: st.RawDB()})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	vkSvc, err := vkey.NewService(vkey.ServiceConfig{DB: st.RawDB(), Snapshots: snaps, AuditSink: aud})
	if err != nil {
		t.Fatalf("vkey.NewService: %v", err)
	}

	srv, err := NewServer(ServerConfig{
		Store: st, DB: st.RawDB(), Bootstrap: bootMgr, Auth: authSvc, OIDC: oidcProv, VKeys: vkSvc,
		Snapshots: snaps, KillSwitch: ksReg, Audit: aud, PaginationSecret: []byte("tp0041-cursor-secret-32-bytes!!!"),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	// 1. OIDC Start -> 302 redirect, NEG-1 forged alg="none" -> 401 invalid_oidc_token.
	recStartForged := httptest.NewRecorder()
	srv.ServeHTTP(recStartForged, httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/start", nil))
	if recStartForged.Code != http.StatusFound {
		t.Fatalf("oidc start status=%d", recStartForged.Code)
	}
	uForged, _ := url.Parse(recStartForged.Header().Get("Location"))
	idp.mu.Lock()
	idp.forgedNext = true
	idp.pendingCodes["code_forged"] = struct{ sub, email, nonce string }{"sub_forged", "f@example.test", uForged.Query().Get("nonce")}
	idp.mu.Unlock()
	recCBForged := httptest.NewRecorder()
	reqCBForged := httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/callback?code=code_forged&state="+url.QueryEscape(uForged.Query().Get("state")), nil)
	for _, c := range recStartForged.Result().Cookies() {
		reqCBForged.AddCookie(c)
	}
	srv.ServeHTTP(recCBForged, reqCBForged)
	if recCBForged.Code != http.StatusUnauthorized || !strings.Contains(recCBForged.Body.String(), "invalid_oidc_token") {
		t.Fatalf("expected 401 invalid_oidc_token on forged token, got %d body=%s", recCBForged.Code, recCBForged.Body.String())
	}

	// 1b. Login-CSRF check: missing or mismatched state cookie on callback fails closed (401).
	recStartCSRF := httptest.NewRecorder()
	srv.ServeHTTP(recStartCSRF, httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/start", nil))
	uCSRF, _ := url.Parse(recStartCSRF.Header().Get("Location"))
	recCBNoCookie := httptest.NewRecorder()
	srv.ServeHTTP(recCBNoCookie, httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/callback?code=code_unused&state="+url.QueryEscape(uCSRF.Query().Get("state")), nil))
	if recCBNoCookie.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on missing OIDC login-CSRF cookie, got %d body=%s", recCBNoCookie.Code, recCBNoCookie.Body.String())
	}

	// 1c. Security hardening: untrusted RemoteAddr with X-Forwarded-Proto: https must NOT emit Secure __Host- cookie.
	reqSpoofTLS := httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/start", nil)
	reqSpoofTLS.RemoteAddr = "198.51.100.77:54321"
	reqSpoofTLS.Header.Set("X-Forwarded-Proto", "https")
	recSpoofTLS := httptest.NewRecorder()
	srv.ServeHTTP(recSpoofTLS, reqSpoofTLS)
	for _, c := range recSpoofTLS.Result().Cookies() {
		if c.Name == "__Host-tanpopo_oidc_state" || c.Secure {
			t.Fatalf("untrusted X-Forwarded-Proto unexpectedly set HTTPS/Secure cookie %+v", c)
		}
	}

	// 2. OIDC Start -> Valid Callback -> SECURITY_REVIEWER session -> Refresh removes group -> RBAC revoked!
	recStart := httptest.NewRecorder()
	srv.ServeHTTP(recStart, httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/start", nil))
	uStart, _ := url.Parse(recStart.Header().Get("Location"))
	idp.mu.Lock()
	idp.userGroups["sub_reviewer"] = []string{"tanpopo-sec-reviewers"}
	idp.pendingCodes["code_valid"] = struct{ sub, email, nonce string }{"sub_reviewer", "sec-oidc@example.test", uStart.Query().Get("nonce")}
	idp.mu.Unlock()
	recCB := httptest.NewRecorder()
	reqCB := httptest.NewRequest(http.MethodGet, "/admin/v1/auth/oidc/callback?code=code_valid&state="+url.QueryEscape(uStart.Query().Get("state")), nil)
	for _, c := range recStart.Result().Cookies() {
		reqCB.AddCookie(c)
	}
	srv.ServeHTTP(recCB, reqCB)
	if recCB.Code != http.StatusFound {
		t.Fatalf("valid oidc callback expected 302, got %d body=%s", recCB.Code, recCB.Body.String())
	}
	var oidcCookie *http.Cookie
	for _, c := range recCB.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			oidcCookie = c
		}
	}
	if oidcCookie == nil {
		t.Fatalf("missing OIDC session cookie")
	}

	reqSess := httptest.NewRequest(http.MethodGet, "/admin/v1/auth/session", nil)
	reqSess.AddCookie(oidcCookie)
	recSess := httptest.NewRecorder()
	srv.ServeHTTP(recSess, reqSess)
	var sessOut adminSessionResponse
	if recSess.Code != http.StatusOK || json.Unmarshal(recSess.Body.Bytes(), &sessOut) != nil || len(sessOut.Roles) != 1 || sessOut.Roles[0] != rbac.RoleSecurityReviewer {
		t.Fatalf("expected SECURITY_REVIEWER role on OIDC session, got code=%d body=%s", recSess.Code, recSess.Body.String())
	}

	reqAuditPre := httptest.NewRequest(http.MethodGet, "/admin/v1/audit-events", nil)
	reqAuditPre.AddCookie(oidcCookie)
	recAuditPre := httptest.NewRecorder()
	srv.ServeHTTP(recAuditPre, reqAuditPre)
	if recAuditPre.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /audit-events while SECURITY_REVIEWER is active, got %d body=%s", recAuditPre.Code, recAuditPre.Body.String())
	}

	// Cross-principal OIDC refresh hijacking attempt MUST fail closed (401 invalid_oidc_token).
	reqHijack := httptest.NewRequest(http.MethodPost, "/admin/v1/auth/oidc/refresh", strings.NewReader(`{"refresh_token":"victim_other_sub"}`))
	reqHijack.Header.Set("Content-Type", "application/json")
	reqHijack.Header.Set(auth.CSRFHeaderName, sessOut.CSRFToken)
	reqHijack.AddCookie(oidcCookie)
	recHijack := httptest.NewRecorder()
	srv.ServeHTTP(recHijack, reqHijack)
	if recHijack.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on cross-principal OIDC refresh attempt, got %d body=%s", recHijack.Code, recHijack.Body.String())
	}

	// Raw id_token shortcut MUST fail closed (400 invalid_request).
	reqRawID := httptest.NewRequest(http.MethodPost, "/admin/v1/auth/oidc/refresh", strings.NewReader(`{"id_token":"forged.jwt.here"}`))
	reqRawID.Header.Set("Content-Type", "application/json")
	reqRawID.Header.Set(auth.CSRFHeaderName, sessOut.CSRFToken)
	reqRawID.AddCookie(oidcCookie)
	recRawID := httptest.NewRecorder()
	srv.ServeHTTP(recRawID, reqRawID)
	if recRawID.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on id_token refresh shortcut, got %d body=%s", recRawID.Code, recRawID.Body.String())
	}

	// Remove group and refresh via /admin/v1/auth/oidc/refresh under ES256 -> privileges auto-revoked!
	idp.mu.Lock()
	idp.activeAlg = "ES256"
	idp.activeKID = "ec-http-1"
	idp.userGroups["sub_reviewer"] = []string{}
	idp.mu.Unlock()
	reqRef := httptest.NewRequest(http.MethodPost, "/admin/v1/auth/oidc/refresh", strings.NewReader(`{"refresh_token":"sub_reviewer"}`))
	reqRef.Header.Set("Content-Type", "application/json")
	reqRef.Header.Set(auth.CSRFHeaderName, sessOut.CSRFToken)
	reqRef.AddCookie(oidcCookie)
	recRef := httptest.NewRecorder()
	srv.ServeHTTP(recRef, reqRef)
	if recRef.Code != http.StatusOK {
		t.Fatalf("POST /auth/oidc/refresh status=%d body=%s", recRef.Code, recRef.Body.String())
	}
	var refSess adminSessionResponse
	_ = json.Unmarshal(recRef.Body.Bytes(), &refSess)
	if len(refSess.Roles) != 0 {
		t.Fatalf("expected 0 roles after group removal on refresh, got %v", refSess.Roles)
	}
	var refreshedCookie *http.Cookie
	for _, c := range recRef.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			refreshedCookie = c
		}
	}
	// Verify previously granted kill-switch / audit permissions now fail closed (403/404 uniform anti-oracle)!
	reqAudit := httptest.NewRequest(http.MethodGet, "/admin/v1/audit-events", nil)
	reqAudit.AddCookie(refreshedCookie)
	recAudit := httptest.NewRecorder()
	srv.ServeHTTP(recAudit, reqAudit)
	if recAudit.Code != http.StatusForbidden && recAudit.Code != http.StatusNotFound {
		t.Fatalf("expected 403/404 after OIDC role revocation, got %d body=%s", recAudit.Code, recAudit.Body.String())
	}

	// 3. Four-Eyes Dual Control on Kill-Switch Clear + Change Request Queue (AC-2):
	_, ownerCookie, err := authSvc.LoginForOrg(ctx, "127.0.0.1", bootRes.OrgID, "owner1@example.test", "StrongOwnerPass#2026")
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}
	ownerInfo, ownerCookie2, err := authSvc.LoginForOrg(ctx, "127.0.0.1", bootRes.OrgID, "owner1@example.test", "StrongOwnerPass#2026")
	_ = ownerCookie
	if err != nil {
		t.Fatalf("owner login 2: %v", err)
	}

	// HUMAN_LOCAL session cannot call /auth/oidc/refresh (403).
	reqLocalOIDC := httptest.NewRequest(http.MethodPost, "/admin/v1/auth/oidc/refresh", strings.NewReader(`{"refresh_token":"sub_reviewer"}`))
	reqLocalOIDC.Header.Set("Content-Type", "application/json")
	reqLocalOIDC.Header.Set(auth.CSRFHeaderName, ownerInfo.CSRFToken)
	reqLocalOIDC.AddCookie(ownerCookie2)
	recLocalOIDC := httptest.NewRecorder()
	srv.ServeHTTP(recLocalOIDC, reqLocalOIDC)
	if recLocalOIDC.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on HUMAN_LOCAL /auth/oidc/refresh, got %d body=%s", recLocalOIDC.Code, recLocalOIDC.Body.String())
	}

	// Cross-organization root key shred & non-operator KEK rotation via /change-requests MUST fail closed (403).
	reqCrossShred := httptest.NewRequest(http.MethodPost, "/admin/v1/change-requests", strings.NewReader(`{"action_type":"VAULT_DESTROY_ROOT_KEY","target_type":"org_encryption_key","target_id":"org_victim_other","payload":{"expected_generation":1}}`))
	reqCrossShred.Header.Set("Content-Type", "application/json")
	reqCrossShred.Header.Set(auth.CSRFHeaderName, ownerInfo.CSRFToken)
	reqCrossShred.AddCookie(ownerCookie2)
	recCrossShred := httptest.NewRecorder()
	srv.ServeHTTP(recCrossShred, reqCrossShred)
	if recCrossShred.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-org VAULT_DESTROY_ROOT_KEY initiation, got %d body=%s", recCrossShred.Code, recCrossShred.Body.String())
	}

	// Seed second ORG_OWNER admin2.
	admin2ID := idgen.MustNew(idgen.PrefixPrincipal)
	hash2, _ := auth.HashPasswordWithParams("StrongSecondPass#2026", a2)
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, password_argon2id, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'owner2@example.test', ?, 'ACTIVE', 1, 1000, 1000)`,
		admin2ID, bootRes.OrgID, hash2,
	); err != nil {
		t.Fatalf("insert admin2: %v", err)
	}
	if _, err := st.RawDB().ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES ('rb_tp41_owner2', ?, ?, 'ORG', ?, 'ORG_OWNER', 1, 1000)`,
		bootRes.OrgID, admin2ID, bootRes.OrgID,
	); err != nil {
		t.Fatalf("insert admin2 binding: %v", err)
	}
	admin2Info, admin2Cookie, err := authSvc.LoginForOrg(ctx, "127.0.0.1", bootRes.OrgID, "owner2@example.test", "StrongSecondPass#2026")
	if err != nil {
		t.Fatalf("admin2 login: %v", err)
	}

	// Trigger kill switch as owner1 -> active.
	abortTrue := true
	ks, _, err := ksReg.TriggerWithSeveredCount(ctx, killswitch.TriggerParams{
		OrgID: &bootRes.OrgID, Level: killswitch.LevelProjectOrOrgFreeze, TargetID: bootRes.ProjectID,
		Reason: "TP41 incident drill", TriggeredByPrincipalID: ownerInfo.PrincipalID,
		EntryPoint: port.AuditEntryPointAdminAPI, AbortActiveStreams: &abortTrue,
	})
	if err != nil {
		t.Fatalf("Trigger killswitch: %v", err)
	}

	// owner1 calls DELETE /admin/v1/kill-switches/{id} -> pauses in 202 PENDING_SECOND_APPROVER!
	reqDelKS := httptest.NewRequest(http.MethodDelete, "/admin/v1/kill-switches/"+ks.ID, strings.NewReader(`{"expected_version":1,"clear_reason":"incident resolved cleanly"}`))
	reqDelKS.Header.Set("Content-Type", "application/json")
	reqDelKS.Header.Set(auth.CSRFHeaderName, ownerInfo.CSRFToken)
	reqDelKS.AddCookie(ownerCookie2)
	recDelKS := httptest.NewRecorder()
	srv.ServeHTTP(recDelKS, reqDelKS)
	if recDelKS.Code != http.StatusAccepted {
		t.Fatalf("DELETE kill-switch with Four-Eyes expected 202, got %d body=%s", recDelKS.Code, recDelKS.Body.String())
	}
	var cr foureyes.ChangeRequest
	if err := json.Unmarshal(recDelKS.Body.Bytes(), &cr); err != nil || cr.State != foureyes.StatePendingSecondApprover {
		t.Fatalf("expected PENDING_SECOND_APPROVER change request, got %+v err=%v", cr, err)
	}

	// owner1 tries self-approval -> 403 self_approval_forbidden!
	reqSelf := httptest.NewRequest(http.MethodPost, "/admin/v1/change-requests/"+cr.ID+"/decide", strings.NewReader(`{"decision":"APPROVE","reason":"self attempt"}`))
	reqSelf.Header.Set("Content-Type", "application/json")
	reqSelf.Header.Set("If-Match", `"`+FormatETag(cr.Version)[1:len(FormatETag(cr.Version))-1]+`"`)
	reqSelf.Header.Set(auth.CSRFHeaderName, ownerInfo.CSRFToken)
	reqSelf.AddCookie(ownerCookie2)
	recSelf := httptest.NewRecorder()
	srv.ServeHTTP(recSelf, reqSelf)
	if recSelf.Code != http.StatusForbidden || !strings.Contains(recSelf.Body.String(), "self_approval_forbidden") {
		t.Fatalf("expected 403 self_approval_forbidden, got %d body=%s", recSelf.Code, recSelf.Body.String())
	}

	// admin2 approves -> 200 APPROVED_APPLIED and kill-switch is cleared!
	reqApp := httptest.NewRequest(http.MethodPost, "/admin/v1/change-requests/"+cr.ID+"/decide", strings.NewReader(`{"decision":"APPROVE","reason":"verified resolved by second admin"}`))
	reqApp.Header.Set("Content-Type", "application/json")
	reqApp.Header.Set("If-Match", FormatETag(cr.Version))
	reqApp.Header.Set(auth.CSRFHeaderName, admin2Info.CSRFToken)
	reqApp.AddCookie(admin2Cookie)
	recApp := httptest.NewRecorder()
	srv.ServeHTTP(recApp, reqApp)
	if recApp.Code != http.StatusOK {
		t.Fatalf("admin2 approve expected 200, got %d body=%s", recApp.Code, recApp.Body.String())
	}
	var approvedCR foureyes.ChangeRequest
	_ = json.Unmarshal(recApp.Body.Bytes(), &approvedCR)
	if approvedCR.State != foureyes.StateApprovedApplied {
		t.Fatalf("expected APPROVED_APPLIED, got %+v", approvedCR)
	}
	reloadedKS, err := ksReg.Get(ctx, bootRes.OrgID, ks.ID)
	if err != nil || reloadedKS.Active {
		t.Fatalf("expected kill-switch cleared after second approver, got %+v err=%v", reloadedKS, err)
	}
}

func TestTPIMP0041HTTP_ResponseCachePolicyAndFourEyesVaultDeleteWithMismatchedOrgVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newAdminTestHarness(t)
	rawTok, err := h.bootMgr.EnsureToken(ctx)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	boot, err := h.bootMgr.Complete(ctx, bootstrap.CompleteRequest{
		BootstrapToken:              rawTok,
		OwnerEmail:                  "owner-cache41@example.test",
		OwnerPassword:               "StrongOwnerPass#2026",
		OrgName:                     "TP41 Cache Org",
		ProjectName:                 "tp41-cache-proj",
		MasterKEKBackupAcknowledged: true,
	})
	if err != nil {
		t.Fatalf("bootMgr.Complete: %v", err)
	}
	ownerSess, ownerCookie, err := h.authSvc.LoginForOrg(ctx, "127.0.0.1", boot.OrgID, "owner-cache41@example.test", "StrongOwnerPass#2026")
	if err != nil {
		t.Fatalf("owner login: %v", err)
	}

	// 1. PUT /admin/v1/policies/pol_cache_tp41 with response_cache_enabled, response_cache_ttl_seconds, response_cache_max_entry_bytes.
	recCache := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/policies/pol_cache_tp41", ownerCookie, ownerSess.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":                     "PROJECT",
		"scope_id":                       boot.ProjectID,
		"allowed_data_regions":           []string{},
		"min_plugin_trust":               "OFFICIAL_ONLY",
		"min_capability_assurance":       "DECLARED",
		"response_cache_enabled":         true,
		"response_cache_ttl_seconds":     120,
		"response_cache_max_entry_bytes": 65536,
	})
	if recCache.Code != http.StatusOK {
		t.Fatalf("PUT response_cache policy expected 200, got %d body=%s", recCache.Code, recCache.Body.String())
	}
	var cacheDTO policyDocumentDTO
	if err := json.Unmarshal(recCache.Body.Bytes(), &cacheDTO); err != nil {
		t.Fatalf("decode cache policy DTO: %v", err)
	}
	if cacheDTO.ResponseCacheEnabled == nil || !*cacheDTO.ResponseCacheEnabled ||
		cacheDTO.ResponseCacheTTLSeconds == nil || *cacheDTO.ResponseCacheTTLSeconds != 120 ||
		cacheDTO.ResponseCacheMaxEntryBytes == nil || *cacheDTO.ResponseCacheMaxEntryBytes != 65536 {
		t.Fatalf("unexpected cache policy DTO fields: %+v", cacheDTO)
	}

	// Verify GET effective policy reflects response cache settings cleanly.
	recEff := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/policies/effective?project_id="+boot.ProjectID, ownerCookie, "", "", nil)
	if recEff.Code != http.StatusOK {
		t.Fatalf("GET effective policy status=%d body=%s", recEff.Code, recEff.Body.String())
	}
	var effDTO policyDocumentDTO
	if err := json.Unmarshal(recEff.Body.Bytes(), &effDTO); err != nil {
		t.Fatalf("decode effective policy DTO: %v", err)
	}
	if effDTO.ResponseCacheEnabled == nil || !*effDTO.ResponseCacheEnabled ||
		effDTO.ResponseCacheTTLSeconds == nil || *effDTO.ResponseCacheTTLSeconds != 120 ||
		effDTO.ResponseCacheMaxEntryBytes == nil || *effDTO.ResponseCacheMaxEntryBytes != 65536 {
		t.Fatalf("expected effective policy response cache config preserved, got %+v", effDTO)
	}

	// 2. Ensure Org DEK at key_generation=1 while bumping organizations.version=3 under require_four_eyes_approval=1.
	if _, err := h.vault.EnsureOrgDEK(ctx, boot.OrgID); err != nil {
		t.Fatalf("EnsureOrgDEK: %v", err)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = 1, version = 3 WHERE id = ?`, boot.OrgID); err != nil {
		t.Fatalf("update org version & four-eyes: %v", err)
	}
	orgAfter, err := h.st.Tenancy().GetOrganization(ctx, boot.OrgID)
	if err != nil {
		t.Fatalf("GetOrganization: %v", err)
	}

	// Initiate DELETE /admin/v1/organizations/{id} -> Four-Eyes 202 Accepted with expected_generation=1 (NOT org.Version=3).
	recDel := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/organizations/"+boot.OrgID, ownerCookie, ownerSess.CSRFToken, FormatETag(orgAfter.Version), map[string]any{
		"confirm_slug": orgAfter.Slug,
		"reason":       "four-eyes org decommission test",
	})
	if recDel.Code != http.StatusAccepted {
		t.Fatalf("DELETE org under Four-Eyes expected 202, got %d body=%s", recDel.Code, recDel.Body.String())
	}
	var cr foureyes.ChangeRequest
	if err := json.Unmarshal(recDel.Body.Bytes(), &cr); err != nil {
		t.Fatalf("decode org delete CR: %v", err)
	}
	if !strings.Contains(cr.PayloadJCSJSON, `"expected_generation":1`) {
		t.Fatalf("expected vault key_generation=1 in CR payload, got %s", cr.PayloadJCSJSON)
	}

	// Second ORG_OWNER approves -> 200 APPROVED_APPLIED and Org DEK is shredded despite org.Version=3.
	admin2Tok := "tok_tp41_second_owner"
	admin2ID := idgen.MustNew(idgen.PrefixPrincipal)
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO principals (id, org_id, kind, email_or_sub, status, version, created_at_ms, updated_at_ms)
		VALUES (?, ?, 'HUMAN_LOCAL', 'owner2-del@example.test', 'ACTIVE', 1, 1000, 1000)`,
		admin2ID, boot.OrgID,
	); err != nil {
		t.Fatalf("insert admin2: %v", err)
	}
	h.bearerMu.Lock()
	h.bearerTokens[admin2Tok] = &rbac.Actor{
		PrincipalID:     admin2ID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner}},
	}
	h.bearerMu.Unlock()
	recDecide := h.doBearer(t, http.MethodPost, "/admin/v1/change-requests/"+cr.ID+"/decide", admin2Tok, FormatETag(cr.Version), map[string]any{
		"decision": "APPROVE",
		"reason":   "second admin approves org root key shred",
	})
	if recDecide.Code != http.StatusOK {
		t.Fatalf("second admin approve org shred expected 200, got %d body=%s", recDecide.Code, recDecide.Body.String())
	}
	var decidedCR foureyes.ChangeRequest
	_ = json.Unmarshal(recDecide.Body.Bytes(), &decidedCR)
	if decidedCR.State != foureyes.StateApprovedApplied {
		t.Fatalf("expected APPROVED_APPLIED, got %+v", decidedCR)
	}
	if _, err := h.vault.GetOrgKeyMetadata(ctx, boot.OrgID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("expected Org DEK shredded after Four-Eyes approval, got err=%v", err)
	}
}
