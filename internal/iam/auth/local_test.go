// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type recordingAuditSink struct {
	mu     sync.Mutex
	events []port.AuditEvent
}

func (s *recordingAuditSink) Append(_ context.Context, ev port.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
	return nil
}

func (s *recordingAuditSink) Count(eventType string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, ev := range s.events {
		if ev.EventType == eventType {
			n++
		}
	}
	return n
}

func openAuthTestDB(t *testing.T, fk *clock.FakeClock) *sqlite.Store {
	t.Helper()
	st, err := sqlite.Open(context.Background(), sqlite.Config{
		DBPath: filepath.Join(t.TempDir(), "auth.db"),
		NowMs: func() int64 {
			return fk.Now().UTC().UnixMilli()
		},
	})
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestArgon2idSessionCSRFAndPlaneIsolation(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openAuthTestDB(t, fk)
	sink := &recordingAuditSink{}
	ctx := context.Background()

	svc, err := NewLocalService(ServiceConfig{
		DB:        st.RawDB(),
		NowMs:     func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:    Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "auth-org", DisplayName: "Auth Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	const ownerPass = "correct-horse-battery-staple-99"
	prc, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID:      &org.ID,
		Kind:       PrincipalKindHumanLocal,
		EmailOrSub: "owner@example.test",
		Password:   ownerPass,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID:       &org.ID,
		PrincipalID: prc.ID,
		ScopeType:   rbac.ScopeOrg,
		ScopeID:     org.ID,
		RoleName:    rbac.RoleOrgOwner,
	}); err != nil {
		t.Fatalf("BindRole: %v", err)
	}

	// Verify partial unique index WHERE org_id IS NULL for instance OPERATORs (0005_iam_keys.sql).
	op1, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID:      nil,
		Kind:       PrincipalKindHumanLocal,
		EmailOrSub: "sysop@example.test",
		Password:   ownerPass,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal instance operator: %v", err)
	}
	if _, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID:      nil,
		Kind:       PrincipalKindHumanLocal,
		EmailOrSub: "sysop@example.test",
		Password:   ownerPass,
	}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists on duplicate org_id IS NULL operator, got %v", err)
	}
	_ = op1

	// Login issues HttpOnly; SameSite=Strict; Secure session cookie + CSRF token.
	sess, cookie, err := svc.Login(ctx, "198.51.100.10", "owner@example.test", ownerPass)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if cookie.Name != SessionCookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie security flags missing: %+v", cookie)
	}
	if len(sess.CSRFToken) < MinCSRFTokenLength {
		t.Fatalf("csrf token too short: %d", len(sess.CSRFToken))
	}

	// NEG-3 / AUTH-03: Mutating /admin/v1/* without X-CSRF-Token or with wrong token -> ErrCSRFValidationFailed.
	reqMissingCSRF := httptest.NewRequest(http.MethodPost, "https://gateway.local/admin/v1/virtual-keys", nil)
	reqMissingCSRF.AddCookie(cookie)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqMissingCSRF); !errors.Is(err, ErrCSRFValidationFailed) {
		t.Fatalf("missing CSRF expected ErrCSRFValidationFailed, got %v", err)
	}

	reqBadCSRF := httptest.NewRequest(http.MethodPost, "https://gateway.local/admin/v1/virtual-keys", nil)
	reqBadCSRF.AddCookie(cookie)
	reqBadCSRF.Header.Set(CSRFHeaderName, "wrong-csrf-token-value-1234567890abcdef")
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqBadCSRF); !errors.Is(err, ErrCSRFValidationFailed) {
		t.Fatalf("mismatched CSRF expected ErrCSRFValidationFailed, got %v", err)
	}

	reqBadOrigin := httptest.NewRequest(http.MethodPost, "https://gateway.local/admin/v1/virtual-keys", nil)
	reqBadOrigin.Host = "gateway.local"
	reqBadOrigin.AddCookie(cookie)
	reqBadOrigin.Header.Set("Origin", "https://evil.attacker.invalid")
	reqBadOrigin.Header.Set(CSRFHeaderName, sess.CSRFToken)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqBadOrigin); !errors.Is(err, ErrCSRFValidationFailed) {
		t.Fatalf("foreign Origin expected ErrCSRFValidationFailed, got %v", err)
	}

	// Valid CSRF + matching Origin succeeds.
	reqValid := httptest.NewRequest(http.MethodPost, "https://gateway.local/admin/v1/virtual-keys", nil)
	reqValid.Host = "gateway.local"
	reqValid.AddCookie(cookie)
	reqValid.Header.Set("Origin", "https://gateway.local")
	reqValid.Header.Set(CSRFHeaderName, sess.CSRFToken)
	actor, _, err := svc.AuthenticateAdminHTTPRequest(reqValid)
	if err != nil {
		t.Fatalf("valid admin request failed: %v", err)
	}
	if actor.PrincipalID != prc.ID {
		t.Fatalf("unexpected actor principal: %s", actor.PrincipalID)
	}

	// NEG-1 / AUTH-07: Virtual Key (vk_tp_*) presented on /admin/v1/* -> ErrInvalidCredentialPlane.
	reqVKOnAdmin := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/organizations", nil)
	reqVKOnAdmin.Header.Set("Authorization", "Bearer vk_tp_live_0123456789abcdefABCDEF01234567")
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqVKOnAdmin); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("Virtual Key on /admin/v1/* expected ErrInvalidCredentialPlane, got %v", err)
	}

	// NEG-1 / AUTH-07: Admin session cookie directly on /v1/chat/completions -> ErrInvalidCredentialPlane.
	reqCookieOnData := httptest.NewRequest(http.MethodPost, "https://gateway.local/v1/chat/completions", nil)
	reqCookieOnData.AddCookie(cookie)
	if _, err := ExtractDataPlaneVirtualKey(reqCookieOnData); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("Admin session cookie on /v1/* expected ErrInvalidCredentialPlane, got %v", err)
	}

	// Logout invalidates session.
	svc.Logout(cookie.Value)
	reqAfterLogout := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/auth/session", nil)
	reqAfterLogout.AddCookie(cookie)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqAfterLogout); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected ErrUnauthenticated after logout, got %v", err)
	}
}

func TestBruteForceLoginRateLimitingAndAudit(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openAuthTestDB(t, fk)
	sink := &recordingAuditSink{}
	ctx := context.Background()

	svc, err := NewLocalService(ServiceConfig{
		DB:     st.RawDB(),
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2: Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		RateLimit: RateLimitConfig{
			MaxFailures:   5,
			Window:        5 * time.Minute,
			BlockDuration: 2 * time.Minute,
		},
		AuditSink: sink,
	})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "rl-org", DisplayName: "RL Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID:      &org.ID,
		Kind:       PrincipalKindHumanLocal,
		EmailOrSub: "victim@example.test",
		Password:   "super-strong-password-42",
	}); err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}

	// Fire 30 consecutive failed logins (AUTH-02): first 5 return InvalidCredentials, 6..30 hit 429 rate limit.
	rateLimitedCount := 0
	for i := 0; i < 30; i++ {
		_, _, loginErr := svc.Login(ctx, "203.0.113.5", "victim@example.test", "wrong-pass")
		if errors.Is(loginErr, ErrAuthRateLimited) {
			rateLimitedCount++
			var rle *RateLimitError
			if !errors.As(loginErr, &rle) || rle.RetryAfterSeconds < 1 {
				t.Fatalf("expected RateLimitError with RetryAfterSeconds >= 1, got %v", loginErr)
			}
		} else if !errors.Is(loginErr, ErrInvalidCredentials) {
			t.Fatalf("unexpected error at attempt %d: %v", i, loginErr)
		}
	}
	if rateLimitedCount != 25 {
		t.Fatalf("expected 25 rate-limited responses out of 30 attempts, got %d", rateLimitedCount)
	}
	if sink.Count("auth.login_rate_limited") == 0 {
		t.Fatalf("expected auth.login_rate_limited audit event")
	}

	// Advance clock past block duration -> valid password succeeds again.
	fk.Advance(3 * time.Minute)
	if _, _, err := svc.Login(ctx, "203.0.113.5", "victim@example.test", "super-strong-password-42"); err != nil {
		t.Fatalf("login after cooldown failed: %v", err)
	}
}

func TestSessionExpirySuspendedPrincipalAndEdgeCases(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openAuthTestDB(t, fk)
	ctx := context.Background()

	if _, err := NewLocalService(ServiceConfig{}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("nil DB expected ErrInvalidInput, got %v", err)
	}

	svc, err := NewLocalService(ServiceConfig{
		DB:             st.RawDB(),
		NowMs:          func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:         Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		SessionTTL:     30 * time.Minute,
		AllowedOrigins: []string{"https://console.tanpopo.local"},
	})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}

	// Verify password verification rejects malformed hashes fail-closed.
	for _, bad := range []string{"invalid", "$bcrypt$v=1$m=1024,t=1,p=1$aa$bb", "$argon2id$v=99$m=1024,t=1,p=1$aa$bb", "$argon2id$v=19$m=100,t=1,p=1$aa$bb"} {
		if ok, _ := VerifyPassword("password123456", bad); ok {
			t.Fatalf("VerifyPassword must reject malformed hash %q", bad)
		}
	}

	org, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "edge-org", DisplayName: "Edge Org"})
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	if _, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &org.ID, EmailOrSub: "short@example.test", Password: "too-short",
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("short password expected ErrInvalidInput, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &org.ID, PrincipalID: "prc_1", ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("INSTANCE binding with non-nil OrgID expected ErrInvalidInput, got %v", err)
	}

	const pass = "edge-owner-password-2026!"
	prc, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &org.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "edge@example.test", Password: pass,
	})
	if err != nil {
		t.Fatalf("CreatePrincipal: %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &org.ID, PrincipalID: prc.ID, ScopeType: rbac.ScopeOrg, ScopeID: org.ID, RoleName: rbac.RoleOrgOwner,
	}); err != nil {
		t.Fatalf("BindRole: %v", err)
	}

	sess, cookie, err := svc.Login(ctx, "127.0.0.1", "edge@example.test", pass)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	reqAllowedOrigin := httptest.NewRequest(http.MethodPost, "https://gateway.local/admin/v1/projects", nil)
	reqAllowedOrigin.AddCookie(cookie)
	reqAllowedOrigin.Header.Set("Origin", "https://console.tanpopo.local")
	reqAllowedOrigin.Header.Set(CSRFHeaderName, sess.CSRFToken)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqAllowedOrigin); err != nil {
		t.Fatalf("allowed origin request failed: %v", err)
	}

	// Suspend principal -> existing session rejected fail-closed with ErrPrincipalInactive.
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE principals SET status = 'SUSPENDED' WHERE id = ?`, prc.ID); err != nil {
		t.Fatalf("suspend principal: %v", err)
	}
	reqGet := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/auth/session", nil)
	reqGet.AddCookie(cookie)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqGet); !errors.Is(err, ErrPrincipalInactive) {
		t.Fatalf("suspended principal expected ErrPrincipalInactive, got %v", err)
	}

	// Reactivate, login again, advance clock past 30m SessionTTL -> expired.
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE principals SET status = 'ACTIVE' WHERE id = ?`, prc.ID); err != nil {
		t.Fatalf("reactivate principal: %v", err)
	}
	_, cookie2, err := svc.Login(ctx, "127.0.0.1", "edge@example.test", pass)
	if err != nil {
		t.Fatalf("Login 2: %v", err)
	}
	fk.Advance(31 * time.Minute)
	reqExpired := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/auth/session", nil)
	reqExpired.AddCookie(cookie2)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqExpired); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session expected ErrUnauthenticated, got %v", err)
	}

	// Non-vk_tp_ bearer token on Data Plane -> ErrInvalidCredentialPlane; valid vk_tp_ extracts cleanly.
	reqBadDP := httptest.NewRequest(http.MethodPost, "https://gateway.local/v1/chat/completions", nil)
	reqBadDP.Header.Set("Authorization", "Bearer admin_service_token_abc")
	if _, err := ExtractDataPlaneVirtualKey(reqBadDP); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("admin bearer on Data Plane expected ErrInvalidCredentialPlane, got %v", err)
	}
	reqGoodDP := httptest.NewRequest(http.MethodPost, "https://gateway.local/v1/chat/completions", nil)
	reqGoodDP.Header.Set("Authorization", "Bearer vk_tp_prod_0123456789abcdefABCDEF01234567")
	if gotKey, err := ExtractDataPlaneVirtualKey(reqGoodDP); err != nil || gotKey == "" {
		t.Fatalf("valid vk_tp_ on Data Plane failed: got=%q err=%v", gotKey, err)
	}

	svc.RevokeAllSessions()
	if n, ok := ParseRetryAfter("120"); !ok || n != 120 {
		t.Fatalf("ParseRetryAfter failed: n=%d ok=%v", n, ok)
	}
	if _, ok := ParseRetryAfter("invalid"); ok {
		t.Fatalf("ParseRetryAfter invalid should return false")
	}

	// Verify production HashPassword + DefaultArgon2Params round-trip cleanly.
	defHash, err := HashPassword("production-default-argon2-secret!")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if ok, verifyErr := VerifyPassword("production-default-argon2-secret!", defHash); verifyErr != nil || !ok {
		t.Fatalf("VerifyPassword default params failed: ok=%v err=%v", ok, verifyErr)
	}
	if ok, _ := VerifyPassword("wrong-secret", defHash); ok {
		t.Fatalf("VerifyPassword must reject wrong password")
	}

	// Verify RateLimitError.Error formatting and BindRole duplicate/invalid errors.
	rle := &RateLimitError{RetryAfterSeconds: 45}
	if rle.Error() == "" || !errors.Is(rle, ErrAuthRateLimited) {
		t.Fatalf("unexpected RateLimitError behavior: %+v", rle)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &org.ID, PrincipalID: prc.ID, ScopeType: rbac.ScopeOrg, ScopeID: org.ID, RoleName: rbac.RoleOrgOwner,
	}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("duplicate role binding expected ErrAlreadyExists, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &org.ID, PrincipalID: prc.ID, ScopeType: "INVALID_SCOPE", ScopeID: org.ID, RoleName: rbac.RoleDeveloper,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("invalid scope_type expected ErrInvalidInput, got %v", err)
	}
	if _, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &org.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "edge@example.test", Password: pass,
	}); !errors.Is(err, port.ErrAlreadyExists) {
		t.Fatalf("duplicate org principal expected ErrAlreadyExists, got %v", err)
	}
}

func TestCrossTenantEmailShadowingCanceledCtxAndLiveRBACReload(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openAuthTestDB(t, fk)
	ctx := context.Background()

	svc, err := NewLocalService(ServiceConfig{
		DB:     st.RawDB(),
		NowMs:  func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2: Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		RateLimit: RateLimitConfig{
			MaxFailures:   5,
			Window:        5 * time.Minute,
			BlockDuration: 2 * time.Minute,
		},
	})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tenant-a", DisplayName: "Tenant A"})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "tenant-b", DisplayName: "Tenant B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}

	// Canonical role-scope rejections in BindRole.
	pA, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &orgA.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "alice@corp.test", Password: "alice-password-org-a-999",
	})
	if err != nil {
		t.Fatalf("CreatePrincipal pA: %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgA.ID, PrincipalID: pA.ID, ScopeType: rbac.ScopeOrg, ScopeID: orgA.ID, RoleName: rbac.RoleOperator,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("OPERATOR on ORG scope expected ErrInvalidInput, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgA.ID, PrincipalID: pA.ID, ScopeType: rbac.ScopeOrg, ScopeID: orgA.ID, RoleName: rbac.RoleOrgOwner,
	}); err != nil {
		t.Fatalf("BindRole ORG_OWNER A: %v", err)
	}

	// Tenant B inserts the exact same email with a different password.
	pB, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &orgB.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "alice@corp.test", Password: "mallory-password-org-b-999",
	})
	if err != nil {
		t.Fatalf("CreatePrincipal pB: %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgB.ID, PrincipalID: pB.ID, ScopeType: rbac.ScopeOrg, ScopeID: orgB.ID, RoleName: rbac.RoleDeveloper,
	}); err != nil {
		t.Fatalf("BindRole DEVELOPER B: %v", err)
	}

	// Regression check: Alice's password still authenticates into Org A (no shadowing / DoS!).
	sessA, cookieA, err := svc.Login(ctx, "198.51.100.1", "alice@corp.test", "alice-password-org-a-999")
	if err != nil || sessA.PrincipalID != pA.ID || *sessA.OrgID != orgA.ID {
		t.Fatalf("Alice login failed or crossed tenant: sess=%+v err=%v", sessA, err)
	}

	// Downgrade Alice in DB while session is active -> next request must immediately reflect DEVELOPER, not cached ORG_OWNER!
	if _, err := st.RawDB().ExecContext(ctx, `UPDATE role_bindings SET role_name = 'DEVELOPER' WHERE principal_id = ?`, pA.ID); err != nil {
		t.Fatalf("downgrade role in DB: %v", err)
	}
	reqLive := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/projects", nil)
	reqLive.AddCookie(cookieA)
	actorLive, _, err := svc.AuthenticateAdminHTTPRequest(reqLive)
	if err != nil {
		t.Fatalf("AuthenticateAdminHTTPRequest after role downgrade: %v", err)
	}
	if authErr := rbac.Authorize(actorLive, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgA.ID}); !errors.Is(authErr, rbac.ErrPermissionDenied) {
		t.Fatalf("expected downgraded session to be denied ActionBudgetWrite, got %v", authErr)
	}

	// Canceled request context must NOT wipe Alice's active session.
	cctx, ccancel := context.WithCancel(ctx)
	ccancel()
	reqCanceled := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/projects", nil).WithContext(cctx)
	reqCanceled.AddCookie(cookieA)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqCanceled); err == nil {
		t.Fatalf("expected canceled ctx error")
	}
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqLive); err != nil {
		t.Fatalf("session was erroneously wiped by canceled request ctx: %v", err)
	}

	// Basic auth / duplicate Authorization / http Origin downgrade checks.
	reqBasic := httptest.NewRequest(http.MethodPost, "https://gateway.local/v1/chat/completions", nil)
	reqBasic.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if _, err := ExtractDataPlaneVirtualKey(reqBasic); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("Basic auth on /v1/* expected ErrInvalidCredentialPlane, got %v", err)
	}

	reqHTTPOrigin := httptest.NewRequest(http.MethodPost, "https://gateway.local/admin/v1/projects", nil)
	reqHTTPOrigin.Host = "gateway.local"
	reqHTTPOrigin.AddCookie(cookieA)
	reqHTTPOrigin.Header.Set("Origin", "http://gateway.local")
	reqHTTPOrigin.Header.Set(CSRFHeaderName, sessA.CSRFToken)
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqHTTPOrigin); !errors.Is(err, ErrCSRFValidationFailed) {
		t.Fatalf("http:// Origin downgrade expected ErrCSRFValidationFailed, got %v", err)
	}

	// Rate-limit bypass check: attacker succeeds once on an unrelated account from same IP -> IP failures persist.
	_, err = svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &orgB.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "helper@corp.test", Password: "helper-password-org-b-999",
	})
	if err != nil {
		t.Fatalf("CreatePrincipal helper: %v", err)
	}
	const attackerIP = "203.0.113.99"
	for i := 0; i < 4; i++ {
		_, _, _ = svc.Login(ctx, attackerIP, "alice@corp.test", "bad-guess")
	}
	if _, _, err := svc.Login(ctx, attackerIP, "helper@corp.test", "helper-password-org-b-999"); err != nil {
		t.Fatalf("helper valid login failed: %v", err)
	}
	// 5th failure from attackerIP hits the IP cap despite valid interleaved helper login.
	_, _, _ = svc.Login(ctx, attackerIP, "alice@corp.test", "bad-guess-5")
	if _, _, err := svc.Login(ctx, attackerIP, "alice@corp.test", "bad-guess-6"); !errors.Is(err, ErrAuthRateLimited) {
		t.Fatalf("expected IP bucket preserved across helper login and hitting ErrAuthRateLimited, got %v", err)
	}
}

func TestCapacityBoundingBindRoleAndHeaderConflicts(t *testing.T) {
	t.Parallel()
	fk := clock.NewFakeClock(clock.DefaultFakeStart())
	st := openAuthTestDB(t, fk)
	ctx := context.Background()

	svc, err := NewLocalService(ServiceConfig{
		DB:                      st.RawDB(),
		NowMs:                   func() int64 { return fk.Now().UTC().UnixMilli() },
		Argon2:                  Argon2Params{Time: 1, MemoryKiB: 1024, Threads: 1, KeyLen: 32},
		SessionTTL:              30 * time.Minute,
		MaxActiveSessions:       5,
		MaxSessionsPerPrincipal: 3,
		RateLimit: RateLimitConfig{
			MaxFailures:       2,
			Window:            5 * time.Minute,
			BlockDuration:     2 * time.Minute,
			MaxFailureBuckets: 6,
		},
	})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}

	orgA, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "cap-org-a", DisplayName: "Cap Org A"})
	if err != nil {
		t.Fatalf("CreateOrganization A: %v", err)
	}
	orgB, err := st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "cap-org-b", DisplayName: "Cap Org B"})
	if err != nil {
		t.Fatalf("CreateOrganization B: %v", err)
	}
	projA, err := st.Tenancy().CreateProject(ctx, orgA.ID, port.CreateProjectParams{Slug: "proj-a", DisplayName: "Project A"})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	projB, err := st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "proj-b", DisplayName: "Project B"})
	if err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}

	pA, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &orgA.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "a@cap.test", Password: "password-alpha-2026!",
	})
	if err != nil {
		t.Fatalf("CreatePrincipal pA: %v", err)
	}
	pB, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &orgB.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "b@cap.test", Password: "password-bravo-2026!",
	})
	if err != nil {
		t.Fatalf("CreatePrincipal pB: %v", err)
	}
	if _, err := svc.CreatePrincipal(ctx, CreatePrincipalParams{
		OrgID: &orgB.ID, Kind: PrincipalKindHumanLocal, EmailOrSub: "c@cap.test", Password: "password-charlie-2026!",
	}); err != nil {
		t.Fatalf("CreatePrincipal pC: %v", err)
	}

	// BindRole strict scope & ownership checks.
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgA.ID, PrincipalID: pA.ID, ScopeType: rbac.ScopeOrg, ScopeID: orgB.ID, RoleName: rbac.RoleOrgOwner,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("ORG binding with mismatched ScopeID expected ErrInvalidInput, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgA.ID, PrincipalID: pB.ID, ScopeType: rbac.ScopeOrg, ScopeID: orgA.ID, RoleName: rbac.RoleOrgOwner,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cross-org principal binding expected ErrNotFound, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: nil, PrincipalID: pA.ID, ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator,
	}); !errors.Is(err, port.ErrInvalidInput) {
		t.Fatalf("INSTANCE binding on org-scoped principal expected ErrInvalidInput, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgA.ID, PrincipalID: pA.ID, ScopeType: rbac.ScopeProject, ScopeID: projB.ID, RoleName: rbac.RoleTeamAdmin,
	}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("PROJECT binding referencing Org B project expected ErrNotFound, got %v", err)
	}
	if _, err := svc.BindRole(ctx, BindRoleParams{
		OrgID: &orgA.ID, PrincipalID: pA.ID, ScopeType: rbac.ScopeProject, ScopeID: projA.ID, RoleName: rbac.RoleTeamAdmin,
	}); err != nil {
		t.Fatalf("valid PROJECT binding failed: %v", err)
	}

	// Per-principal (3) & global (5) active session caps evict earliest-expiring sessions strictly.
	var cookiesA []*http.Cookie
	for i := 0; i < 5; i++ {
		fk.Advance(time.Second)
		_, ck, loginErr := svc.LoginForOrg(ctx, "198.51.100.1", orgA.ID, "a@cap.test", "password-alpha-2026!")
		if loginErr != nil {
			t.Fatalf("Login A %d failed: %v", i, loginErr)
		}
		cookiesA = append(cookiesA, ck)
	}
	svc.sessMu.RLock()
	totalA := len(svc.sessions)
	svc.sessMu.RUnlock()
	if totalA != 3 {
		t.Fatalf("expected per-principal cap to hold 3 sessions for pA, got %d", totalA)
	}
	reqEvicted := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/projects", nil)
	reqEvicted.AddCookie(cookiesA[0])
	if _, _, err := svc.AuthenticateAdminHTTPRequest(reqEvicted); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("oldest evicted session cookiesA[0] expected ErrUnauthenticated, got %v", err)
	}
	for i := 0; i < 3; i++ {
		fk.Advance(time.Second)
		if _, _, err := svc.LoginForOrg(ctx, "198.51.100.2", orgB.ID, "b@cap.test", "password-bravo-2026!"); err != nil {
			t.Fatalf("Login B %d failed: %v", i, err)
		}
	}
	fk.Advance(time.Second)
	if _, _, err := svc.LoginForOrg(ctx, "198.51.100.3", orgB.ID, "c@cap.test", "password-charlie-2026!"); err != nil {
		t.Fatalf("Login C failed: %v", err)
	}
	svc.sessMu.RLock()
	totalAll := len(svc.sessions)
	svc.sessMu.RUnlock()
	if totalAll > 5 {
		t.Fatalf("expected global active session count <= 5, got %d", totalAll)
	}

	// Conflicting differing vk_tp_* headers -> ErrInvalidCredentialPlane.
	reqConflict := httptest.NewRequest(http.MethodPost, "https://gateway.local/v1/chat/completions", nil)
	reqConflict.Header.Set("Authorization", "Bearer vk_tp_prod_0123456789abcdefABCDEF01234567")
	reqConflict.Header.Set("x-api-key", "vk_tp_prod_fedcba9876543210FEDCBA9876543210")
	if _, err := ExtractDataPlaneVirtualKey(reqConflict); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("conflicting vk_tp_* headers expected ErrInvalidCredentialPlane, got %v", err)
	}

	// Rate-limiter capacity saturation: rotating unblocked probes evict oldest unblocked slots, stay bounded <= 6,
	// and never fail open; filling all 6 slots with active lockouts causes fresh unknown IP to FAIL CLOSED.
	for i := 0; i < 6; i++ {
		_, _, _ = svc.Login(ctx, fmt.Sprintf("198.51.100.%d", 10+i), fmt.Sprintf("probe%d@cap.test", i), "wrong")
	}
	svc.rlMu.Lock()
	unblockedSize := len(svc.failures)
	svc.rlMu.Unlock()
	if unblockedSize > 6 {
		t.Fatalf("expected failure map bounded <= 6 during unblocked flood, got %d", unblockedSize)
	}

	for i := 0; i < 2; i++ {
		_, _, _ = svc.Login(ctx, "192.0.2.1", "bogus1@cap.test", "wrong")
	}
	for i := 0; i < 2; i++ {
		_, _, _ = svc.Login(ctx, "192.0.2.2", "bogus2@cap.test", "wrong")
	}
	svc.rlMu.Lock()
	rlSize := len(svc.failures)
	svc.rlMu.Unlock()
	if rlSize > 6 {
		t.Fatalf("expected failure map bounded <= 6, got %d", rlSize)
	}
	if _, _, err := svc.Login(ctx, "192.0.2.99", "bogus99@cap.test", "wrong"); !errors.Is(err, ErrAuthRateLimited) {
		t.Fatalf("saturated active lockouts expected fail-closed ErrAuthRateLimited, got %v", err)
	}

	// Concurrent AuthenticateAdminHTTPRequest alongside Logout/RevokePrincipalSessions (race-detector verification).
	_, raceCookie, err := svc.LoginForOrg(ctx, "198.51.100.1", orgA.ID, "a@cap.test", "password-alpha-2026!")
	if err != nil {
		t.Fatalf("LoginForOrg race setup: %v", err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for step := 0; step < 25; step++ {
				req := httptest.NewRequest(http.MethodGet, "https://gateway.local/admin/v1/projects", nil)
				req.AddCookie(raceCookie)
				_, _, authErr := svc.AuthenticateAdminHTTPRequest(req)
				if authErr != nil && !errors.Is(authErr, ErrUnauthenticated) {
					t.Errorf("unexpected concurrent auth error: %v", authErr)
				}
				if id%2 == 0 && step == 10 {
					svc.Logout(raceCookie.Value)
				}
			}
		}(g)
	}
	wg.Wait()
}
