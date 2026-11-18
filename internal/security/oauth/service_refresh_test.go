// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package oauth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
)

func TestAccessTokenSingleflightRefreshesOnceForTwoHundredCallers(t *testing.T) {
	var refreshCalls atomic.Int32
	allowResponse := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("refresh method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse refresh form: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-before" {
			t.Errorf("refresh_token = %q, want original refresh token", got)
		}
		<-allowResponse
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-after","refresh_token":"refresh-after","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	fx := newRefreshFixture(t, server.URL, "https://resource.example/")
	const callers = 200
	start := make(chan struct{})
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			token, err := fx.service.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://resource.example/")
			if err == nil {
				if string(token) != "access-after" {
					err = errors.New("caller received a token other than the refreshed token")
				}
				clear(token)
			}
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	deadline := time.After(10 * time.Second)
	for refreshCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("no refresh request reached the test endpoint")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	// Keep the first refresh in flight while concurrent callers converge on the same key.
	time.Sleep(100 * time.Millisecond)
	close(allowResponse)
	for range callers {
		if err := <-results; err != nil {
			t.Errorf("AccessToken: %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh endpoint calls = %d, want exactly one for %d concurrent callers", got, callers)
	}
	wrongAudienceToken, wrongAudienceErr := fx.service.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://another-resource.example/")
	clear(wrongAudienceToken)
	if !errors.Is(wrongAudienceErr, ErrTokenAudience) {
		t.Fatalf("AccessToken wrong expected resource error=%v, want ErrTokenAudience", wrongAudienceErr)
	}
}

func TestRefreshLeaseCoordinatesIndependentServiceInstances(t *testing.T) {
	var refreshCalls atomic.Int32
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		entered <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"leased-access","refresh_token":"leased-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	fx := newRefreshFixture(t, server.URL, "https://resource.example/")
	second, err := NewService(ServiceConfig{DB: fx.db, Vault: fx.vault, UnsealCapability: fx.capability, Audit: fx.audit, NowMs: fx.service.nowMs, HostResolver: egress.NewStdHostResolver(), AllowLoopbackHTTP: true, RedirectURI: "http://127.0.0.1/admin/v1/oauth/callback"})
	if err != nil {
		t.Fatalf("create second service instance: %v", err)
	}
	results := make(chan error, 2)
	go func() {
		token, err := fx.service.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://resource.example/")
		clear(token)
		results <- err
	}()
	go func() {
		token, err := second.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://resource.example/")
		clear(token)
		results <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no refresh request reached server")
	}
	select {
	case <-entered:
		t.Fatal("second service instance sent a duplicate rotating refresh request")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("AccessToken: %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls=%d, want one across independent service instances", got)
	}
}

func TestAccessTokenInvalidGrantExpiresAuthAndAuditsWithoutRetry(t *testing.T) {
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	fx := newRefreshFixture(t, server.URL, "https://resource.example/")
	token, err := fx.service.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://resource.example/")
	clear(token)
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("AccessToken error = %v, want ErrInvalidGrant", err)
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("invalid_grant caused %d upstream requests, want exactly one", got)
	}

	var state, lastError string
	if err := fx.db.QueryRow(`SELECT state, last_refresh_error FROM oauth_token_states WHERE org_id=? AND credential_id=?`, fx.orgID, fx.credentialID).Scan(&state, &lastError); err != nil {
		t.Fatal(err)
	}
	if state != "AUTH_EXPIRED_REAUTH_REQUIRED" || lastError != "invalid_grant" {
		t.Fatalf("OAuth token state = (%q, %q), want expired reauthentication required / invalid_grant", state, lastError)
	}

	var eventType, metadata string
	if err := fx.db.QueryRow(`SELECT event_type, metadata_json FROM audit_events WHERE event_type='ALT-OAUTH-EXPIRY' AND target_id=?`, fx.credentialID).Scan(&eventType, &metadata); err != nil {
		t.Fatalf("read expiry alert: %v", err)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(metadata), &details); err != nil {
		t.Fatalf("decode alert metadata: %v", err)
	}
	if eventType != "ALT-OAUTH-EXPIRY" || details["reason"] != "invalid_grant" || details["retry_count"] != float64(0) {
		t.Fatalf("expiry alert = %q %s, want invalid_grant with retry_count 0", eventType, metadata)
	}

	// Once transitioned out of TOKEN_VALID, another caller must not retry the upstream grant.
	_, secondErr := fx.service.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://resource.example/")
	if secondErr == nil {
		t.Fatal("AccessToken succeeded after invalid_grant expired the authorization")
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("subsequent call retried upstream after invalid_grant: calls=%d", got)
	}
}

func TestAuthorizationCodeGrantWithoutRefreshTokenRequiresReauthorization(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"wrongly-reacquired","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	fx := newRefreshFixture(t, server.URL, "https://resource.example/")
	meta, err := fx.vault.GetSecretMetadata(context.Background(), fx.orgID, fx.credentialID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := tokenSetJSON(&TokenResponse{AccessToken: []byte("access-before"), TokenType: "Bearer", ExpiresIn: 3600}, server.URL, "oauth-client", fx.connectionID, "read", "https://resource.example/", GrantAuthorizationCodePKCE, time.UnixMilli(1_800_000_000_000-3_600_000))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(encoded)
	resource := "https://resource.example/"
	if _, err := fx.vault.RotateSecret(context.Background(), fx.orgID, fx.credentialID, meta.Version, vault.RotateSecretParams{Plaintext: encoded, BoundAudience: &resource}); err != nil {
		t.Fatal(err)
	}
	token, err := fx.service.AccessToken(context.Background(), fx.orgID, fx.credentialID, "https://resource.example/")
	clear(token)
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("AccessToken error=%v, want ErrInvalidGrant", err)
	}
	if got := tokenCalls.Load(); got != 0 {
		t.Fatalf("token endpoint calls=%d, want no silent client_credentials fallback", got)
	}
	var state string
	if err := fx.db.QueryRow(`SELECT state FROM oauth_token_states WHERE org_id=? AND credential_id=?`, fx.orgID, fx.credentialID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "AUTH_EXPIRED_REAUTH_REQUIRED" {
		t.Fatalf("token state=%q, want reauthorization required", state)
	}
}

func TestAuthorizationCallbackBindsPrincipalAndConsumesStateOnce(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse authorization code form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "one-time-code" {
			t.Errorf("unexpected authorization code form: %v", r.Form)
		}
		if len(r.Form.Get("code_verifier")) < 43 {
			t.Errorf("code verifier length = %d, want at least 43", len(r.Form.Get("code_verifier")))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"callback-access","refresh_token":"callback-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	fx := newRefreshFixture(t, server.URL+"/token", "https://resource.example/")
	started, err := fx.service.Start(context.Background(), StartInput{
		OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("a", 64), ConnectionID: fx.connectionID,
		Flow: GrantAuthorizationCodePKCE, AuthorizationEndpoint: server.URL + "/authorize",
		TokenEndpoint: server.URL + "/token", ClientID: "oauth-client", Resource: "https://resource.example/",
	})
	if err != nil {
		t.Fatalf("Start authorization code flow: %v", err)
	}
	authURL, err := url.Parse(started.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := authURL.Query().Get("code_challenge_method"); got != "S256" {
		t.Fatalf("code_challenge_method = %q, want S256", got)
	}
	if got := authURL.Query().Get("state"); got != started.State || got == "" {
		t.Fatalf("authorization URL state = %q, Start state = %q", got, started.State)
	}
	if err := fx.service.Callback(context.Background(), fx.orgID, fx.principalID, strings.Repeat("b", 64), started.State, "one-time-code"); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("callback from another browser session error = %v, want ErrInvalidOAuthState", err)
	}

	if err := fx.service.Callback(context.Background(), fx.orgID, idgen.MustNew(idgen.PrefixPrincipal), strings.Repeat("a", 64), started.State, "one-time-code"); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("callback with another principal error = %v, want ErrInvalidOAuthState", err)
	}
	if err := fx.service.Callback(context.Background(), idgen.MustNew(idgen.PrefixOrg), fx.principalID, strings.Repeat("a", 64), started.State, "one-time-code"); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("callback with another organization error = %v, want ErrInvalidOAuthState", err)
	}
	var savedPrincipal, savedTargetKind, savedGrant, savedStatus string
	if err := fx.db.QueryRow(`SELECT principal_id, target_kind, grant_type, session_status FROM oauth_authorization_sessions WHERE org_id=? AND state=?`, fx.orgID, started.State).Scan(&savedPrincipal, &savedTargetKind, &savedGrant, &savedStatus); err != nil {
		t.Fatalf("read started OAuth state: %v", err)
	}
	if savedPrincipal != fx.principalID || savedTargetKind != "PROVIDER_CONNECTION" || savedGrant != GrantAuthorizationCodePKCE || savedStatus != "PENDING" {
		t.Fatalf("saved state mismatch before callback: principal=%q target=%q grant=%q status=%q", savedPrincipal, savedTargetKind, savedGrant, savedStatus)
	}
	if err := fx.service.Callback(context.Background(), fx.orgID, fx.principalID, strings.Repeat("a", 64), started.State, "one-time-code"); err != nil {
		t.Fatalf("first callback for valid pending session: %v", err)
	}
	if err := fx.service.Callback(context.Background(), fx.orgID, fx.principalID, strings.Repeat("a", 64), started.State, "one-time-code"); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("replayed callback error = %v, want ErrInvalidOAuthState", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token endpoint calls = %d, want one after ownership checks and replay", got)
	}
}

func TestDeviceFlowSealsDeviceCodeAndCompletesOnAuthorizedPoll(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/device" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"device_code":"device-secret-value","user_code":"USER-1234","verification_uri":"http://` + r.Host + `/activate","expires_in":300,"interval":5}`))
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse device token request: %v", err)
		}
		if got := r.Form.Get("device_code"); got != "device-secret-value" {
			t.Errorf("device_code=%q", got)
		}
		if tokenCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"device-access","refresh_token":"device-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	fx := newRefreshFixture(t, server.URL+"/token", "https://resource.example/")
	started, err := fx.service.Start(context.Background(), StartInput{OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("a", 64), ConnectionID: fx.connectionID, Flow: GrantDeviceCode, TokenEndpoint: server.URL + "/token", DeviceAuthorizationURL: server.URL + "/device", ClientID: "device-client", Resource: "https://resource.example/"})
	if err != nil {
		t.Fatalf("Start device flow: %v", err)
	}
	var sessionID string
	var deviceCode sql.NullString
	var sealed, nonce []byte
	if err := fx.db.QueryRow(`SELECT id,device_code,sealed_code_verifier,verifier_nonce FROM oauth_authorization_sessions WHERE org_id=? AND state=?`, fx.orgID, started.State).Scan(&sessionID, &deviceCode, &sealed, &nonce); err != nil {
		t.Fatal(err)
	}
	if deviceCode.Valid {
		t.Fatalf("raw device code persisted: %q", deviceCode.String)
	}
	if len(sealed) == 0 || len(nonce) == 0 {
		t.Fatal("device code was not persisted in an AEAD envelope")
	}
	if _, err := fx.service.PollDevice(context.Background(), PollInput{OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("a", 64), State: started.State}); !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("first device poll error=%v, want authorization_pending", err)
	}
	if _, err := fx.service.PollDevice(context.Background(), PollInput{OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("b", 64), State: started.State}); !errors.Is(err, ErrInvalidOAuthState) {
		t.Fatalf("device poll from another browser session error=%v, want ErrInvalidOAuthState", err)
	}
	if _, err := fx.db.Exec(`UPDATE oauth_authorization_sessions SET last_poll_at_ms=created_at_ms-poll_interval_ms WHERE org_id=? AND id=?`, fx.orgID, sessionID); err != nil {
		t.Fatal(err)
	}
	result, err := fx.service.PollDevice(context.Background(), PollInput{OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("a", 64), State: started.State})
	if err != nil || result == nil || result.Status != "completed" {
		t.Fatalf("authorized device poll result=%+v err=%v", result, err)
	}
	if tokenCalls.Load() != 2 {
		t.Fatalf("token endpoint calls=%d, want pending plus completion", tokenCalls.Load())
	}
	var state string
	if err := fx.db.QueryRow(`SELECT session_status FROM oauth_authorization_sessions WHERE org_id=? AND id=?`, fx.orgID, sessionID).Scan(&state); err != nil || state != "COMPLETED" {
		t.Fatalf("device session status=%q err=%v", state, err)
	}
}

func TestDevicePollSetupFailureReleasesClaimForRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/device" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"device_code":"device-secret-value","user_code":"USER-1234","verification_uri":"http://` + r.Host + `/activate","expires_in":300,"interval":5}`))
			return
		}
		t.Errorf("token endpoint should not be reached after setup failure")
	}))
	defer server.Close()
	fx := newRefreshFixture(t, server.URL+"/token", "https://resource.example/")
	started, err := fx.service.Start(context.Background(), StartInput{OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("a", 64), ConnectionID: fx.connectionID, Flow: GrantDeviceCode, TokenEndpoint: server.URL + "/token", DeviceAuthorizationURL: server.URL + "/device", ClientID: "device-client", Resource: "https://resource.example/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.Exec(`UPDATE provider_adapters SET allowed_hosts_json='[]' WHERE id=(SELECT adapter_id FROM provider_connections WHERE org_id=? AND id=?)`, fx.orgID, fx.connectionID); err != nil {
		t.Fatal(err)
	}
	_, err = fx.service.PollDevice(context.Background(), PollInput{OrgID: fx.orgID, PrincipalID: fx.principalID, BrowserSessionHash: strings.Repeat("a", 64), State: started.State})
	if err == nil {
		t.Fatal("expected setup failure after poll claim")
	}
	var status string
	if err := fx.db.QueryRow(`SELECT session_status FROM oauth_authorization_sessions WHERE org_id=? AND state=?`, fx.orgID, started.State).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "PENDING" {
		t.Fatalf("session status=%q, want PENDING after retryable setup failure", status)
	}
}

type refreshFixture struct {
	db           *sql.DB
	service      *Service
	orgID        string
	principalID  string
	connectionID string
	credentialID string
	vault        *vault.Store
	capability   vault.UnsealCapability
	audit        *audit.Logger
}

func newRefreshFixture(t *testing.T, tokenEndpoint, resource string) *refreshFixture {
	t.Helper()
	ctx := context.Background()
	now := int64(1_800_000_000_000)
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: filepath.Join(t.TempDir(), "oauth.db"), NowMs: func() int64 { return now }})
	if err != nil {
		t.Fatalf("open sqlite test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.RawDB()
	logger, err := audit.NewLogger(audit.LoggerConfig{DB: db, NowMs: func() int64 { return now }})
	if err != nil {
		t.Fatalf("create audit logger: %v", err)
	}
	key := make([]byte, vault.KeySizeBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate test KEK: %v", err)
	}
	kek, err := vault.NewStaticKEKProvider("oauth-test-kek", key)
	clear(key)
	if err != nil {
		t.Fatalf("create test KEK: %v", err)
	}
	t.Cleanup(kek.Close)
	vs, err := vault.NewStore(vault.Config{DB: db, KEK: kek, NowMs: func() int64 { return now }, PostShredHook: store.CheckpointWAL})
	if err != nil {
		t.Fatalf("create test vault: %v", err)
	}
	t.Cleanup(vs.Close)

	orgID := idgen.MustNew(idgen.PrefixOrg)
	principalID := idgen.MustNew(idgen.PrefixPrincipal)
	connectionID := idgen.MustNew(idgen.PrefixConnection)
	packageID := idgen.MustNew(idgen.PrefixExtension)
	versionID := idgen.MustNew(idgen.PrefixExtensionVersion)
	installationID := idgen.MustNew(idgen.PrefixInstallation)
	adapterID := idgen.MustNew(idgen.PrefixAdapter)
	if _, err := db.Exec(`INSERT INTO organizations (id, slug, display_name, created_at_ms, updated_at_ms) VALUES (?, 'oauth-test', 'OAuth Test', ?, ?)`, orgID, now, now); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO principals (id, org_id, kind, email_or_sub, status, created_at_ms, updated_at_ms) VALUES (?, ?, 'HUMAN_LOCAL', 'oauth-test@example.invalid', 'ACTIVE', ?, ?)`, principalID, orgID, now, now); err != nil {
		t.Fatalf("insert principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms) VALUES (?, 'oauth.test.adapter', 'PROVIDER_ADAPTER', 'test', 'official', ?)`, packageID, now); err != nil {
		t.Fatalf("insert extension package: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO extension_versions (id, package_id, semver, contract_version, execution_tier, bundle_sha256, source_uri, manifest_json, created_at_ms) VALUES (?, ?, '1.0.0', 'v1', 'DECLARATIVE_T1', ?, 'builtin://test', '{}', ?)`, versionID, packageID, strings.Repeat("a", 64), now); err != nil {
		t.Fatalf("insert extension version: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO extension_installations (id, package_id, active_version_id, state, created_at_ms, updated_at_ms) VALUES (?, ?, ?, 'ENABLED_ACTIVE', ?, ?)`, installationID, packageID, versionID, now, now); err != nil {
		t.Fatalf("insert extension installation: %v", err)
	}
	host, err := url.Parse(tokenEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO provider_adapters (id, installation_id, adapter_slug, allowed_hosts_json, created_at_ms) VALUES (?, ?, 'oauth-test', ?, ?)`, adapterID, installationID, `[`+`"`+host.Host+`"`+`]`, now); err != nil {
		t.Fatalf("insert provider adapter: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO provider_connections (id, org_id, adapter_id, name, endpoint_base_url, data_region, billing_mode, auth_mode, state, created_at_ms, updated_at_ms) VALUES (?, ?, ?, 'OAuth test connection', 'https://resource.example/', 'GLOBAL', 'COMMERCIAL_API', 'OAUTH2', 'ACTIVE', ?, ?)`, connectionID, orgID, adapterID, now, now); err != nil {
		t.Fatalf("insert provider connection: %v", err)
	}
	capability := vault.MintUnsealCapabilityForBroker()
	encoded, err := tokenSetJSON(&TokenResponse{AccessToken: []byte("access-before"), RefreshToken: []byte("refresh-before"), TokenType: "Bearer", ExpiresIn: 3600}, tokenEndpoint, "oauth-client", connectionID, "read", resource, GrantAuthorizationCodePKCE, time.UnixMilli(now-3600_000))
	if err != nil {
		t.Fatalf("encode token set: %v", err)
	}
	defer clear(encoded)
	resourceCopy := resource
	meta, err := vs.SealSecret(ctx, vault.SealSecretParams{OrgID: orgID, Name: "OAuth test token", SecretKind: vault.SecretKindOAuth2TokenSet, BoundAudience: &resourceCopy, Plaintext: encoded})
	if err != nil {
		t.Fatalf("seal OAuth token set: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO connection_secret_bindings (org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms) VALUES (?, ?, 'oauth_access_token', ?, 'HOST_BLIND_INJECT', ?)`, orgID, connectionID, meta.ID, now); err != nil {
		t.Fatalf("bind OAuth token set: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO oauth_token_states (org_id, credential_id, bound_principal_id, state, granted_scopes, bound_resource_uri, access_expires_at_ms, updated_at_ms) VALUES (?, ?, ?, 'TOKEN_VALID', 'read', ?, ?, ?)`, orgID, meta.ID, principalID, resource, now-1, now); err != nil {
		t.Fatalf("insert OAuth token state: %v", err)
	}
	service, err := NewService(ServiceConfig{DB: db, Vault: vs, UnsealCapability: capability, Audit: logger, NowMs: func() int64 { return now }, HostResolver: egress.NewStdHostResolver(), AllowLoopbackHTTP: true, RedirectURI: "http://127.0.0.1/admin/v1/oauth/callback"})
	if err != nil {
		t.Fatalf("create OAuth service: %v", err)
	}
	return &refreshFixture{db: db, service: service, orgID: orgID, principalID: principalID, connectionID: connectionID, credentialID: meta.ID, vault: vs, capability: capability, audit: logger}
}

type oauthBrokerTestResolver struct{ slot *broker.ResolvedSlot }

func (r oauthBrokerTestResolver) ResolveSlot(_ context.Context, orgID, connectionID, slotName string) (*broker.ResolvedSlot, error) {
	if orgID != r.slot.OrgID || connectionID != r.slot.ConnectionID || slotName != r.slot.SlotName {
		return nil, errors.New("slot not found")
	}
	return r.slot, nil
}
func (r oauthBrokerTestResolver) ResolveConnectionPolicy(_ context.Context, _, _ string) (*egress.Policy, error) {
	return &egress.Policy{AllowedHosts: append([]string(nil), r.slot.AllowedHosts...), AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.0/8"}, RequireTLS: false}, nil
}

func TestBlindBrokerRefreshesOnceForTwoHundredConcurrentCallsAndRejectsOtherResource(t *testing.T) {
	var refreshCalls atomic.Int32
	var targetCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse refresh: %v", err)
			}
			if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "refresh-before" {
				t.Errorf("unexpected refresh request: %v", r.Form)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-after","refresh_token":"refresh-after","token_type":"Bearer","expires_in":3600}`))
			return
		}
		targetCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer access-after" {
			t.Errorf("injected auth=%q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer other.Close()
	resource := server.URL + "/"
	fx := newRefreshFixture(t, server.URL+"/token", resource)
	allowedHosts := []string{strings.TrimPrefix(server.URL, "http://"), strings.TrimPrefix(other.URL, "http://")}
	resolver := oauthBrokerTestResolver{slot: &broker.ResolvedSlot{OrgID: fx.orgID, ConnectionID: fx.connectionID, SlotName: "oauth_access_token", SecretID: fx.credentialID, InjectMode: broker.InjectModeHostBlind, BoundAudience: &resource, AllowedHosts: allowedHosts, AllowPrivateIP: true, AllowedCIDRs: []string{"127.0.0.0/8"}, RequireTLS: false}}
	br, err := broker.NewBlindBroker(broker.Config{Resolver: resolver, PolicyResolver: resolver, Vault: fx.vault, OAuthTokens: fx.service, UnsealCap: fx.capability, Audit: fx.audit, HostResolver: egress.NewStdHostResolver()})
	if err != nil {
		t.Fatalf("create broker: %v", err)
	}
	request := func(target string, n int) error {
		resp, err := br.Execute(context.Background(), broker.EgressRequest{OrgID: fx.orgID, ConnectionID: fx.connectionID, Method: http.MethodGet, TargetURL: target, CredentialInjections: []broker.CredentialInjection{{SecretSlotName: "oauth_access_token", Location: broker.TargetBearer, FieldName: "Authorization", ValuePrefix: "Bearer "}}, RequestID: fmt.Sprintf("oauth-%d", n)})
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("broker status %d", resp.StatusCode)
		}
		return nil
	}
	const callers = 200
	start := make(chan struct{})
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func(i int) { <-start; results <- request(server.URL+"/resource", i) }(i)
	}
	close(start)
	for i := 0; i < callers; i++ {
		if err := <-results; err != nil {
			t.Errorf("broker caller: %v", err)
		}
	}
	if got := refreshCalls.Load(); got != 1 {
		t.Fatalf("OAuth refresh requests=%d, want one", got)
	}
	if got := targetCalls.Load(); got != callers {
		t.Fatalf("resource requests=%d, want %d", got, callers)
	}
	if err := request(other.URL+"/resource", callers+1); err == nil {
		t.Fatal("broker accepted a token for a different resource")
	}
}
