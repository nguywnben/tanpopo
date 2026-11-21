// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package oidc

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
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type fakeOAuthIdP struct {
	mu           sync.Mutex
	rsaKey       *rsa.PrivateKey
	ecKey        *ecdsa.PrivateKey
	activeKID    string
	activeAlg    string
	userGroups   map[string][]string
	pendingCodes map[string]fakeCodeGrant
	srv          *httptest.Server
}

type fakeCodeGrant struct {
	subject string
	email   string
	nonce   string
}

func newFakeOAuthIdP(t *testing.T) *fakeOAuthIdP {
	t.Helper()
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	idp := &fakeOAuthIdP{
		rsaKey:       rsaPriv,
		ecKey:        ecPriv,
		activeKID:    "rsa-key-1",
		activeAlg:    "RS256",
		userGroups:   make(map[string][]string),
		pendingCodes: make(map[string]fakeCodeGrant),
	}
	idp.srv = httptest.NewServer(http.HandlerFunc(idp.handle))
	t.Cleanup(idp.srv.Close)
	return idp
}

func (f *fakeOAuthIdP) signJWT(hdr map[string]any, claims map[string]any, alg string) string {
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
	case "none":
		sig = nil
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fakeOAuthIdP) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/jwks.json":
		eBytes := bigEndianUint(f.rsaKey.E)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{
					"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "rsa-key-1",
					"n": base64.RawURLEncoding.EncodeToString(f.rsaKey.N.Bytes()),
					"e": base64.RawURLEncoding.EncodeToString(eBytes),
				},
				{
					"kty": "EC", "use": "sig", "alg": "ES256", "kid": "ec-key-1", "crv": "P-256",
					"x": base64.RawURLEncoding.EncodeToString(f.ecKey.X.Bytes()),
					"y": base64.RawURLEncoding.EncodeToString(f.ecKey.Y.Bytes()),
				},
			},
		})
	case "/token":
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		var sub, email, nonce string
		switch r.FormValue("grant_type") {
		case "authorization_code":
			g, ok := f.pendingCodes[r.FormValue("code")]
			if !ok {
				http.Error(w, "invalid code", http.StatusBadRequest)
				return
			}
			delete(f.pendingCodes, r.FormValue("code"))
			sub, email, nonce = g.subject, g.email, g.nonce
		case "refresh_token":
			rt := r.FormValue("refresh_token")
			sub = rt
			email = rt + "@example.test"
		default:
			http.Error(w, "unsupported grant", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		idTok := f.signJWT(
			map[string]any{"alg": f.activeAlg, "typ": "JWT", "kid": f.activeKID},
			map[string]any{
				"iss":    f.srv.URL,
				"aud":    "tanpopo-console-client",
				"sub":    sub,
				"email":  email,
				"nonce":  nonce,
				"groups": f.userGroups[sub],
				"exp":    now.Add(10 * time.Minute).Unix(),
			},
			f.activeAlg,
		)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at_" + sub,
			"id_token":      idTok,
			"refresh_token": sub,
			"token_type":    "Bearer",
		})
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func bigEndianUint(e int) []byte {
	var out []byte
	for v := e; v > 0; v >>= 8 {
		out = append([]byte{byte(v & 0xff)}, out...)
	}
	return out
}

func TestOIDCLoginGroupToRoleMappingRevocationAC1AndForgeryNEG1(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "oidc.db")})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	defer func() { _ = st.Close() }()

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "oidc-org", DisplayName: "OIDC Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	aud, err := audit.NewLogger(audit.LoggerConfig{DB: st.RawDB()})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	authSvc, err := auth.NewLocalService(auth.ServiceConfig{
		DB:        st.RawDB(),
		AuditSink: aud,
		Argon2:    auth.Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
	})
	if err != nil {
		t.Fatalf("auth.NewLocalService: %v", err)
	}

	idp := newFakeOAuthIdP(t)
	idpURL, _ := url.Parse(idp.srv.URL)

	prov, err := NewProvider(Config{
		OrgID:                 org.ID,
		Issuer:                idp.srv.URL,
		ClientID:              "tanpopo-console-client",
		RedirectURI:           "https://console.tanpopo.local/admin/v1/auth/oidc/callback",
		AuthorizationEndpoint: idp.srv.URL + "/authorize",
		TokenEndpoint:         idp.srv.URL + "/token",
		JWKSURI:               idp.srv.URL + "/jwks.json",
		GroupMappings: []GroupRoleMapping{
			{Group: "tanpopo-sec-reviewers", OrgID: org.ID, ScopeType: rbac.ScopeOrg, ScopeID: org.ID, RoleName: rbac.RoleSecurityReviewer},
			{Group: "tanpopo-developers", OrgID: org.ID, ScopeType: rbac.ScopeOrg, ScopeID: org.ID, RoleName: rbac.RoleDeveloper},
		},
		DB:        st.RawDB(),
		Auth:      authSvc,
		AuditSink: aud,
		EgressPolicy: &egress.Policy{
			AllowedHosts:   []string{idpURL.Host},
			AllowPrivateIP: true,
			AllowedCIDRs:   []string{"127.0.0.0/8"},
			RequireTLS:     false,
		},
	})
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}

	// NEG-1: Reject alg="none", wrong aud, wrong nonce, and unknown kid with ErrInvalidOIDCToken.
	now := time.Now().UTC()
	forgedTokens := []struct {
		name string
		jwt  string
	}{
		{
			name: "alg=none",
			jwt:  idp.signJWT(map[string]any{"alg": "none", "typ": "JWT"}, map[string]any{"iss": idp.srv.URL, "aud": "tanpopo-console-client", "sub": "alice", "nonce": "n1", "exp": now.Add(time.Minute).Unix()}, "none"),
		},
		{
			name: "wrong audience",
			jwt:  idp.signJWT(map[string]any{"alg": "RS256", "kid": "rsa-key-1"}, map[string]any{"iss": idp.srv.URL, "aud": "wrong-client", "sub": "alice", "nonce": "n1", "exp": now.Add(time.Minute).Unix()}, "RS256"),
		},
		{
			name: "wrong nonce",
			jwt:  idp.signJWT(map[string]any{"alg": "RS256", "kid": "rsa-key-1"}, map[string]any{"iss": idp.srv.URL, "aud": "tanpopo-console-client", "sub": "alice", "nonce": "bad-nonce", "exp": now.Add(time.Minute).Unix()}, "RS256"),
		},
		{
			name: "unknown kid",
			jwt:  idp.signJWT(map[string]any{"alg": "RS256", "kid": "unknown-kid-99"}, map[string]any{"iss": idp.srv.URL, "aud": "tanpopo-console-client", "sub": "alice", "nonce": "n1", "exp": now.Add(time.Minute).Unix()}, "RS256"),
		},
	}
	for _, tc := range forgedTokens {
		if _, err := prov.VerifyIDToken(ctx, tc.jwt, "n1"); !errors.Is(err, ErrInvalidOIDCToken) {
			t.Fatalf("%s: expected ErrInvalidOIDCToken, got %v", tc.name, err)
		}
	}

	// AC-1: Authenticate with tanpopo-sec-reviewers -> grants SECURITY_REVIEWER via RS256.
	authURL, state, err := prov.StartAuth(ctx)
	if err != nil {
		t.Fatalf("StartAuth: %v", err)
	}
	parsedAuthURL, _ := url.Parse(authURL)
	nonce := parsedAuthURL.Query().Get("nonce")
	idp.mu.Lock()
	idp.userGroups["sub_alice"] = []string{"tanpopo-sec-reviewers"}
	idp.pendingCodes["code_1"] = fakeCodeGrant{subject: "sub_alice", email: "alice@example.test", nonce: nonce}
	idp.mu.Unlock()

	cbRes, err := prov.HandleCallback(ctx, "code_1", state)
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if len(cbRes.Roles) != 1 || cbRes.Roles[0] != rbac.RoleSecurityReviewer {
		t.Fatalf("expected SECURITY_REVIEWER role, got %v", cbRes.Roles)
	}

	// Verify active session cookie authenticates SECURITY_REVIEWER live from DB.
	req, _ := http.NewRequest(http.MethodGet, "/admin/v1/auth/session", nil)
	req.AddCookie(cbRes.Cookie)
	actor, _, err := authSvc.AuthenticateAdminHTTPRequest(req)
	if err != nil || len(actor.Bindings) != 1 || actor.Bindings[0].RoleName != rbac.RoleSecurityReviewer {
		t.Fatalf("expected SECURITY_REVIEWER on live session, got actor=%+v err=%v", actor, err)
	}

	// AC-1 Session Refresh with rotated ES256 key + removed group membership -> auto-revokes privilege!
	idp.mu.Lock()
	idp.activeAlg = "ES256"
	idp.activeKID = "ec-key-1"
	idp.userGroups["sub_alice"] = []string{} // removed from tanpopo-sec-reviewers
	idp.mu.Unlock()

	refRes, err := prov.RefreshSession(ctx, cbRes.PrincipalID, cbRes.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshSession (ES256): %v", err)
	}
	if len(refRes.Roles) != 0 {
		t.Fatalf("expected 0 roles after group removal on refresh, got %v", refRes.Roles)
	}
	// Old session cookie also immediately observes revoked bindings live from DB.
	actorAfter, _, err := authSvc.AuthenticateAdminHTTPRequest(req)
	if err != nil {
		t.Fatalf("AuthenticateAdminHTTPRequest after refresh: %v", err)
	}
	if len(actorAfter.Bindings) != 0 {
		t.Fatalf("expected 0 active bindings after group revocation, got %+v", actorAfter.Bindings)
	}

	// Verify instance OPERATOR escalation mapping is rejected fail-closed at provider construction.
	if _, err := NewProvider(Config{
		OrgID:                 org.ID,
		Issuer:                idp.srv.URL,
		ClientID:              "c",
		RedirectURI:           "https://console.tanpopo.local/cb",
		AuthorizationEndpoint: idp.srv.URL + "/a",
		TokenEndpoint:         idp.srv.URL + "/t",
		JWKSURI:               idp.srv.URL + "/j",
		GroupMappings:         []GroupRoleMapping{{Group: "admins", OrgID: org.ID, ScopeType: rbac.ScopeOrg, RoleName: rbac.RoleOperator}},
		DB:                    st.RawDB(),
		Auth:                  authSvc,
	}); !errors.Is(err, ErrInvalidOIDCConfig) {
		t.Fatalf("expected ErrInvalidOIDCConfig preventing OIDC OPERATOR escalation, got %v", err)
	}
}
