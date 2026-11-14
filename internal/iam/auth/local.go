// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package auth implements Argon2id local administrator password hashing,
// brute-force login rate limiting, HttpOnly/SameSite=Strict/Secure admin session
// cookies with double-submit X-CSRF-Token verification, and strict Control/Data
// credential-plane isolation (AUTH-02, AUTH-03, AUTH-07 / TP-IMP-0009).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Canonical HTTP cookie, header, and principal constants (SPEC-API-01 / openapi/admin-v1.yaml).
const (
	SessionCookieName = "__Host-tanpopo_admin_session"
	CSRFHeaderName    = "X-CSRF-Token"

	PrincipalKindHumanLocal        = "HUMAN_LOCAL"
	PrincipalKindHumanOIDC         = "HUMAN_OIDC"
	PrincipalKindAdminServiceToken = "ADMIN_SERVICE_TOKEN"

	PrincipalStatusActive    = "ACTIVE"
	PrincipalStatusSuspended = "SUSPENDED"
	PrincipalStatusDisabled  = "DISABLED"

	MinOwnerPasswordLength = 12
	MinCSRFTokenLength     = 32

	DefaultSessionTTL          = 12 * time.Hour
	DefaultRateLimitMaxFails   = 10
	DefaultRateLimitWindow     = 5 * time.Minute
	DefaultRateLimitLockWindow = 5 * time.Minute

	defaultMaxFailureBuckets      = 10_000
	defaultMaxActiveSessions      = 50_000
	defaultMaxSessionsPerPrincipal = 32
	defaultMaxConcurrentArgon2    = 16
	globalAcctLimitMult           = 3
	maxCandidatePrincipals        = 50
)

// Canonical authentication error sentinels.
var (
	ErrInvalidCredentials     = errors.New("auth: invalid credentials")
	ErrAmbiguousTenantLogin   = errors.New("auth: ambiguous email across organizations; org_id required")
	ErrPrincipalInactive      = errors.New("auth: principal suspended or disabled")
	ErrAuthRateLimited        = errors.New("auth: auth_rate_limited")
	ErrUnauthenticated        = errors.New("auth: unauthenticated")
	ErrInvalidCredentialPlane = errors.New("auth: invalid_credential_plane")
	ErrCSRFValidationFailed   = errors.New("auth: csrf_validation_failed")
	ErrInvalidPasswordHash    = errors.New("auth: invalid argon2id hash encoding")
)

// RateLimitError wraps ErrAuthRateLimited with deterministic Retry-After backoff metadata.
type RateLimitError struct {
	RetryAfterSeconds int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s (retry after %ds)", ErrAuthRateLimited.Error(), e.RetryAfterSeconds)
}

func (e *RateLimitError) Unwrap() error {
	return ErrAuthRateLimited
}

// Argon2Params configures Argon2id password key derivation work factors.
type Argon2Params struct {
	Time      uint32
	MemoryKiB uint32
	Threads   uint8
	KeyLen    uint32
}

// DefaultArgon2Params returns production OWASP-aligned Argon2id work factors (64 MiB, t=3, p=2).
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		Time:      3,
		MemoryKiB: 64 * 1024,
		Threads:   2,
		KeyLen:    32,
	}
}

// ValidateArgon2Params verifies Argon2id parameters meet minimum OWASP safety floors.
func ValidateArgon2Params(p Argon2Params) error {
	if p.Time < 1 || p.MemoryKiB < 1024 || p.Threads < 1 || p.KeyLen < 16 {
		return fmt.Errorf("%w: argon2id parameters below minimum safe bounds", port.ErrInvalidInput)
	}
	return nil
}

// HashPassword derives a PHC-formatted Argon2id password digest using production default parameters.
func HashPassword(password string) (string, error) {
	return HashPasswordWithParams(password, DefaultArgon2Params())
}

// HashPasswordWithParams derives a PHC-formatted Argon2id password digest with caller-validated parameters.
func HashPasswordWithParams(password string, params Argon2Params) (string, error) {
	if err := ValidateArgon2Params(params); err != nil {
		return "", err
	}
	if password == "" || strings.ContainsRune(password, '\x00') {
		return "", fmt.Errorf("%w: invalid password input", port.ErrInvalidInput)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	pwBytes := []byte(password)
	defer clear(pwBytes)
	hash := argon2.IDKey(pwBytes, salt, params.Time, params.MemoryKiB, params.Threads, params.KeyLen)
	defer clear(hash)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.MemoryKiB, params.Time, params.Threads, b64Salt, b64Hash), nil
}

// VerifyPassword verifies a plaintext password against a PHC-encoded Argon2id hash in constant time.
func VerifyPassword(password, encodedHash string) (bool, error) {
	if password == "" || strings.ContainsRune(password, '\x00') {
		return false, nil
	}
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, ErrInvalidPasswordHash
	}
	var ver int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &ver); err != nil || ver != argon2.Version {
		return false, ErrInvalidPasswordHash
	}
	var memKiB, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memKiB, &timeCost, &threads); err != nil {
		return false, ErrInvalidPasswordHash
	}
	if memKiB < 1024 || timeCost < 1 || threads < 1 || memKiB > 1024*1024 || timeCost > 32 {
		return false, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false, ErrInvalidPasswordHash
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedHash) < 16 {
		return false, ErrInvalidPasswordHash
	}

	pwBytes := []byte(password)
	defer clear(pwBytes)
	computed := argon2.IDKey(pwBytes, salt, timeCost, memKiB, threads, uint32(len(expectedHash)))
	defer clear(computed)

	return subtle.ConstantTimeCompare(computed, expectedHash) == 1, nil
}

// SessionInfo exposes read-safe authenticated session metadata matching AdminSessionResponse.
type SessionInfo struct {
	PrincipalID string             `json:"principal_id"`
	Email       string             `json:"email"`
	OrgID       *string            `json:"org_id"`
	Roles       []string           `json:"roles"`
	Bindings    []rbac.RoleBinding `json:"-"`
	CSRFToken   string             `json:"csrf_token"`
	ExpiresAtMs int64              `json:"expires_at_ms"`
}

// PrincipalRecord represents a persisted principal row in SQLite.
type PrincipalRecord struct {
	ID             string
	OrgID          *string
	Kind           string
	EmailOrSub     string
	Status         string
	Version        int64
	LastLoginAtMs  *int64
	CreatedAtMs    int64
	UpdatedAtMs    int64
}

// CreatePrincipalParams specifies fields for persisting a local or service principal.
type CreatePrincipalParams struct {
	ID         string
	OrgID      *string
	Kind       string
	EmailOrSub string
	Password   string
}

// BindRoleParams specifies fields for creating an RBAC binding matching CHECK constraints.
type BindRoleParams struct {
	OrgID       *string
	PrincipalID string
	ScopeType   string
	ScopeID     string
	RoleName    string
}

// RateLimitConfig controls brute-force login throttling (AUTH-02 / TP-AC-F0502).
type RateLimitConfig struct {
	MaxFailures       int
	Window            time.Duration
	BlockDuration     time.Duration
	MaxFailureBuckets int
}

// ServiceConfig configures LocalService.
type ServiceConfig struct {
	DB                      *sql.DB
	NowMs                   func() int64
	Argon2                  Argon2Params
	SessionTTL              time.Duration
	MaxActiveSessions       int
	MaxSessionsPerPrincipal int
	RateLimit               RateLimitConfig
	AuditSink               port.AuditSink
	AllowedOrigins          []string
}

type storedSession struct {
	digest      [32]byte
	principalID string
	email       string
	orgID       *string
	roles       []string
	bindings    []rbac.RoleBinding
	csrfToken   string
	expiresAtMs int64
}

type failureBucket struct {
	windowStartMs int64
	failures      int
	blockedUntil  int64
}

// LocalService handles local Argon2id authentication, session/CSRF enforcement,
// brute-force throttling, and Control/Data credential-plane separation.
type LocalService struct {
	db                      *sql.DB
	nowMs                   func() int64
	argon2                  Argon2Params
	sessionTTL              time.Duration
	maxActiveSessions       int
	maxSessionsPerPrincipal int
	rateCfg                 RateLimitConfig
	auditSink               port.AuditSink
	allowedOrigins          map[string]bool
	dummyHash               string
	argonSem                chan struct{}

	sessMu   sync.RWMutex
	sessions map[[32]byte]*storedSession

	rlMu     sync.Mutex
	failures map[string]*failureBucket
}

// NewLocalService validates dependencies and constructs a fail-closed LocalService.
func NewLocalService(cfg ServiceConfig) (*LocalService, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	a2 := cfg.Argon2
	if a2 == (Argon2Params{}) {
		a2 = DefaultArgon2Params()
	}
	if err := ValidateArgon2Params(a2); err != nil {
		return nil, err
	}
	sessTTL := cfg.SessionTTL
	if sessTTL <= 0 {
		sessTTL = DefaultSessionTTL
	}
	maxSessions := cfg.MaxActiveSessions
	if maxSessions <= 0 {
		maxSessions = defaultMaxActiveSessions
	}
	maxPerPrincipal := cfg.MaxSessionsPerPrincipal
	if maxPerPrincipal <= 0 {
		maxPerPrincipal = defaultMaxSessionsPerPrincipal
	}
	rl := cfg.RateLimit
	if rl.MaxFailures <= 0 {
		rl.MaxFailures = DefaultRateLimitMaxFails
	}
	if rl.Window <= 0 {
		rl.Window = DefaultRateLimitWindow
	}
	if rl.BlockDuration <= 0 {
		rl.BlockDuration = DefaultRateLimitLockWindow
	}
	if rl.MaxFailureBuckets <= 0 {
		rl.MaxFailureBuckets = defaultMaxFailureBuckets
	}

	dummy, err := HashPasswordWithParams("tanpopo-dummy-timing-equalizer-secret", a2)
	if err != nil {
		return nil, err
	}

	origins := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		norm := strings.ToLower(strings.TrimRight(strings.TrimSpace(o), "/"))
		if norm != "" {
			origins[norm] = true
		}
	}

	return &LocalService{
		db:                      cfg.DB,
		nowMs:                   nowFn,
		argon2:                  a2,
		sessionTTL:              sessTTL,
		maxActiveSessions:       maxSessions,
		maxSessionsPerPrincipal: maxPerPrincipal,
		rateCfg:                 rl,
		auditSink:               cfg.AuditSink,
		allowedOrigins:          origins,
		dummyHash:               dummy,
		argonSem:                make(chan struct{}, defaultMaxConcurrentArgon2),
		sessions:                make(map[[32]byte]*storedSession),
		failures:                make(map[string]*failureBucket),
	}, nil
}

// CreatePrincipal hashes the local password with Argon2id and persists a new principal.
func (s *LocalService) CreatePrincipal(ctx context.Context, params CreatePrincipalParams) (*PrincipalRecord, error) {
	email := strings.ToLower(strings.TrimSpace(params.EmailOrSub))
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return nil, fmt.Errorf("%w: valid email_or_sub required", port.ErrInvalidInput)
	}
	kind := params.Kind
	if kind == "" {
		kind = PrincipalKindHumanLocal
	}
	var pwHash sql.NullString
	if kind == PrincipalKindHumanLocal {
		if len(params.Password) < MinOwnerPasswordLength {
			return nil, fmt.Errorf("%w: password must be at least %d characters", port.ErrInvalidInput, MinOwnerPasswordLength)
		}
		h, err := HashPasswordWithParams(params.Password, s.argon2)
		if err != nil {
			return nil, err
		}
		pwHash = sql.NullString{String: h, Valid: true}
	}

	id := params.ID
	if id == "" {
		var err error
		id, err = idgen.New(idgen.PrefixPrincipal)
		if err != nil {
			return nil, err
		}
	}
	now := s.nowMs()

	var orgVal any
	if params.OrgID != nil && strings.TrimSpace(*params.OrgID) != "" {
		orgVal = strings.TrimSpace(*params.OrgID)
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO principals (
			id, org_id, kind, email_or_sub, password_argon2id, oidc_issuer,
			status, version, last_login_at_ms, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, NULL, 'ACTIVE', 1, NULL, ?, ?)`,
		id, orgVal, kind, email, pwHash, now, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("%w: principal already exists", port.ErrAlreadyExists)
		}
		return nil, err
	}

	var outOrg *string
	if v, ok := orgVal.(string); ok {
		outOrg = &v
	}
	return &PrincipalRecord{
		ID:          id,
		OrgID:       outOrg,
		Kind:        kind,
		EmailOrSub:  email,
		Status:      PrincipalStatusActive,
		Version:     1,
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}, nil
}

// BindRole assigns one of the 6 canonical RBAC roles to a principal inside a strictly validated scope
// enforcing canonical CR-04 role-to-scope invariants (OPERATOR<->INSTANCE, ORG_OWNER<->ORG, TEAM_ADMIN<->PROJECT).
func (s *LocalService) BindRole(ctx context.Context, params BindRoleParams) (*rbac.RoleBinding, error) {
	if strings.TrimSpace(params.PrincipalID) == "" || !rbac.IsValidRole(params.RoleName) {
		return nil, fmt.Errorf("%w: valid principal_id and canonical role_name required", port.ErrInvalidInput)
	}
	params.PrincipalID = strings.TrimSpace(params.PrincipalID)
	params.ScopeID = strings.TrimSpace(params.ScopeID)

	var expectedOrg string
	switch params.ScopeType {
	case rbac.ScopeInstance:
		if params.OrgID != nil || params.RoleName != rbac.RoleOperator {
			return nil, fmt.Errorf("%w: INSTANCE binding requires RoleOperator and nil org_id", port.ErrInvalidInput)
		}
		if params.ScopeID == "" {
			params.ScopeID = "instance"
		} else if params.ScopeID != "instance" {
			return nil, fmt.Errorf("%w: INSTANCE binding requires scope_id 'instance'", port.ErrInvalidInput)
		}
	case rbac.ScopeOrg:
		if params.RoleName == rbac.RoleOperator {
			return nil, fmt.Errorf("%w: OPERATOR cannot be bound to ORG scope", port.ErrInvalidInput)
		}
		if params.OrgID == nil || strings.TrimSpace(*params.OrgID) == "" || params.ScopeID == "" {
			return nil, fmt.Errorf("%w: ORG binding requires non-empty org_id and scope_id", port.ErrInvalidInput)
		}
		expectedOrg = strings.TrimSpace(*params.OrgID)
		if params.ScopeID != expectedOrg {
			return nil, fmt.Errorf("%w: ORG binding requires scope_id equal to org_id", port.ErrInvalidInput)
		}
	case rbac.ScopeProject:
		if params.RoleName == rbac.RoleOperator || params.RoleName == rbac.RoleOrgOwner {
			return nil, fmt.Errorf("%w: %s cannot be bound at PROJECT scope", port.ErrInvalidInput, params.RoleName)
		}
		if params.OrgID == nil || strings.TrimSpace(*params.OrgID) == "" || params.ScopeID == "" {
			return nil, fmt.Errorf("%w: PROJECT binding requires non-empty org_id and scope_id", port.ErrInvalidInput)
		}
		expectedOrg = strings.TrimSpace(*params.OrgID)
	default:
		return nil, fmt.Errorf("%w: invalid scope_type %q", port.ErrInvalidInput, params.ScopeType)
	}

	var prcOrg sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT org_id FROM principals WHERE id = ?`, params.PrincipalID).Scan(&prcOrg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth: lookup principal: %w", err)
	}
	switch params.ScopeType {
	case rbac.ScopeInstance:
		if prcOrg.Valid {
			return nil, fmt.Errorf("%w: INSTANCE role cannot be bound to org-scoped principal", port.ErrInvalidInput)
		}
	case rbac.ScopeOrg, rbac.ScopeProject:
		if !prcOrg.Valid || prcOrg.String != expectedOrg {
			return nil, port.ErrNotFound
		}
		if params.ScopeType == rbac.ScopeProject {
			var one int
			pErr := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, expectedOrg, params.ScopeID).Scan(&one)
			if errors.Is(pErr, sql.ErrNoRows) {
				return nil, port.ErrNotFound
			}
			if pErr != nil {
				return nil, fmt.Errorf("auth: lookup project: %w", pErr)
			}
		}
	}

	rawID, err := idgen.New(idgen.PrefixPrincipal)
	if err != nil {
		return nil, err
	}
	rbID := "rbd_" + rawID[len(idgen.PrefixPrincipal)+1:]
	now := s.nowMs()

	var orgVal any
	var orgCopy *string
	if params.OrgID != nil {
		v := expectedOrg
		orgVal = v
		orgCopy = &v
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		rbID, orgVal, params.PrincipalID, params.ScopeType, params.ScopeID, params.RoleName, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("%w: role binding already exists", port.ErrAlreadyExists)
		}
		return nil, err
	}

	return &rbac.RoleBinding{
		ID:          rbID,
		OrgID:       orgCopy,
		PrincipalID: params.PrincipalID,
		ScopeType:   params.ScopeType,
		ScopeID:     params.ScopeID,
		RoleName:    params.RoleName,
	}, nil
}

// Login authenticates local email/password credentials across the instance (or disambiguates by
// verifying against matching tenant candidates without cross-tenant email shadowing), enforces
// multi-bucket brute-force throttling (AUTH-02), and mints an HttpOnly; SameSite=Strict; Secure
// session cookie alongside a random double-submit X-CSRF-Token.
func (s *LocalService) Login(ctx context.Context, clientIP, email, password string) (*SessionInfo, *http.Cookie, error) {
	return s.LoginForOrg(ctx, clientIP, "", email, password)
}

// LoginForOrg authenticates local email/password credentials optionally scoped to a target orgID.
func (s *LocalService) LoginForOrg(ctx context.Context, clientIP, orgID, email, password string) (*SessionInfo, *http.Cookie, error) {
	normEmail := strings.ToLower(strings.TrimSpace(email))
	normIP := strings.TrimSpace(clientIP)
	if normIP == "" {
		normIP = "unknown"
	}
	normOrg := strings.TrimSpace(orgID)

	now := s.nowMs()
	ipKey := "ip:" + normIP
	acctIPKey := "acct:" + normIP + ":" + normEmail
	globalAcctKey := "gacct:" + normEmail
	checks := []rateBucketCheck{
		{key: ipKey, limit: s.rateCfg.MaxFailures},
		{key: acctIPKey, limit: s.rateCfg.MaxFailures},
		{key: globalAcctKey, limit: s.globalAcctMaxFailures()},
	}
	if retryAfter, limited := s.checkRateLimits(checks, now); limited {
		s.emitAudit(ctx, nil, "SYSTEM", normIP, "auth.login_rate_limited", "principal", normEmail, `{"reason":"brute_force_throttled"}`)
		return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
	}

	if normEmail == "" || password == "" {
		if retryAfter, saturated := s.recordFailure(ctx, checks, normIP, normEmail, now); saturated {
			return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
		}
		return nil, nil, ErrInvalidCredentials
	}

	type principalCandidate struct {
		id     string
		orgID  *string
		status string
		pwHash string
	}
	var (
		rows *sql.Rows
		err  error
	)
	if normOrg != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, org_id, status, COALESCE(password_argon2id, '')
			FROM principals
			WHERE kind = ? AND email_or_sub = ? AND org_id = ?
			LIMIT 2`,
			PrincipalKindHumanLocal, normEmail, normOrg,
		)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, org_id, status, COALESCE(password_argon2id, '')
			FROM principals
			WHERE kind = ? AND email_or_sub = ?
			ORDER BY (org_id IS NULL) DESC, created_at_ms ASC, id ASC
			LIMIT ?`,
			PrincipalKindHumanLocal, normEmail, maxCandidatePrincipals,
		)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("auth: query principals: %w", err)
	}

	var candidates []principalCandidate
	for rows.Next() {
		var c principalCandidate
		var orgNS sql.NullString
		if scanErr := rows.Scan(&c.id, &orgNS, &c.status, &c.pwHash); scanErr != nil {
			_ = rows.Close()
			return nil, nil, fmt.Errorf("auth: scan principal: %w", scanErr)
		}
		if orgNS.Valid {
			v := orgNS.String
			c.orgID = &v
		}
		candidates = append(candidates, c)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		_ = rows.Close()
		return nil, nil, fmt.Errorf("auth: iterate principals: %w", rowsErr)
	}
	_ = rows.Close()

	if len(candidates) == 0 {
		if err := s.verifyPasswordBounded(ctx, password, s.dummyHash); err != nil {
			return nil, nil, err
		}
		if retryAfter, saturated := s.recordFailure(ctx, checks, normIP, normEmail, now); saturated {
			return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
		}
		return nil, nil, ErrInvalidCredentials
	}

	var verified []principalCandidate
	for _, c := range candidates {
		if c.pwHash == "" {
			continue
		}
		ok, verifyErr := s.verifyPasswordWithSemaphore(ctx, password, c.pwHash)
		if errors.Is(verifyErr, context.Canceled) || errors.Is(verifyErr, context.DeadlineExceeded) {
			return nil, nil, verifyErr
		}
		if verifyErr == nil && ok {
			verified = append(verified, c)
		}
	}

	if len(verified) == 0 {
		if retryAfter, saturated := s.recordFailure(ctx, checks, normIP, normEmail, now); saturated {
			return nil, nil, &RateLimitError{RetryAfterSeconds: retryAfter}
		}
		return nil, nil, ErrInvalidCredentials
	}
	if len(verified) > 1 {
		s.emitAudit(ctx, nil, "SYSTEM", normIP, "auth.login_ambiguous_tenant", "principal", normEmail, `{"reason":"duplicate_password_across_orgs"}`)
		return nil, nil, ErrAmbiguousTenantLogin
	}

	matched := verified[0]
	if matched.status != PrincipalStatusActive {
		s.emitAudit(ctx, matched.orgID, "PRINCIPAL", matched.id, "auth.login_rejected_inactive", "principal", matched.id, `{}`)
		return nil, nil, ErrPrincipalInactive
	}

	bindings, roles, err := s.loadBindings(ctx, matched.id)
	if err != nil {
		return nil, nil, err
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE principals SET last_login_at_ms = ? WHERE id = ?`, now, matched.id); err != nil {
		return nil, nil, err
	}

	// Never clear ipKey on single-account success so valid low-priv logins cannot reset IP throttling.
	s.clearFailures([]string{acctIPKey, globalAcctKey})

	sessionToken, err := randomURLToken(32)
	if err != nil {
		return nil, nil, err
	}
	csrfToken, err := randomURLToken(32)
	if err != nil {
		return nil, nil, err
	}

	expiresAtMs := now + s.sessionTTL.Milliseconds()
	digest := sha256.Sum256([]byte(sessionToken))

	stored := &storedSession{
		digest:      digest,
		principalID: matched.id,
		email:       normEmail,
		orgID:       matched.orgID,
		roles:       roles,
		bindings:    bindings,
		csrfToken:   csrfToken,
		expiresAtMs: expiresAtMs,
	}

	s.sessMu.Lock()
	for k, v := range s.sessions {
		if now >= v.expiresAtMs {
			clear(v.digest[:])
			delete(s.sessions, k)
		}
	}
	// Enforce per-principal session cap so one compromised low-priv account cannot flood global sessions.
	for {
		principalCount := 0
		var oldestKey [32]byte
		var oldestExp int64
		for k, v := range s.sessions {
			if v.principalID != matched.id {
				continue
			}
			principalCount++
			if principalCount == 1 || v.expiresAtMs < oldestExp {
				oldestKey = k
				oldestExp = v.expiresAtMs
			}
		}
		if principalCount < s.maxSessionsPerPrincipal {
			break
		}
		if victim := s.sessions[oldestKey]; victim != nil {
			clear(victim.digest[:])
		}
		delete(s.sessions, oldestKey)
	}
	// Strictly bound total active sessions by evicting the earliest-expiring entry if full.
	for len(s.sessions) >= s.maxActiveSessions {
		var oldestKey [32]byte
		var oldestExp int64
		first := true
		for k, v := range s.sessions {
			if first || v.expiresAtMs < oldestExp {
				oldestKey = k
				oldestExp = v.expiresAtMs
				first = false
			}
		}
		if victim := s.sessions[oldestKey]; victim != nil {
			clear(victim.digest[:])
		}
		delete(s.sessions, oldestKey)
	}
	s.sessions[digest] = stored
	s.sessMu.Unlock()

	s.emitAudit(ctx, matched.orgID, "PRINCIPAL", matched.id, "auth.login_succeeded", "principal", matched.id, `{}`)

	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(s.sessionTTL.Seconds()),
	}

	info := &SessionInfo{
		PrincipalID: matched.id,
		Email:       normEmail,
		OrgID:       matched.orgID,
		Roles:       append([]string(nil), roles...),
		Bindings:    append([]rbac.RoleBinding(nil), bindings...),
		CSRFToken:   csrfToken,
		ExpiresAtMs: expiresAtMs,
	}
	return info, cookie, nil
}

// AuthenticateAdminHTTPRequest enforces Control Plane session auth, live DB principal/RBAC checks,
// double-submit CSRF verification on mutating methods (AUTH-03), and strict Data Plane rejection (AUTH-07).
func (s *LocalService) AuthenticateAdminHTTPRequest(r *http.Request) (*rbac.Actor, *SessionInfo, error) {
	if r == nil {
		return nil, nil, ErrUnauthenticated
	}

	// AUTH-07: Data Plane Virtual Keys (vk_tp_*) must never touch /admin/v1/*.
	if hasVirtualKeyHeader(r) {
		return nil, nil, ErrInvalidCredentialPlane
	}

	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return nil, nil, ErrUnauthenticated
	}

	digest := sha256.Sum256([]byte(cookie.Value))
	now := s.nowMs()

	s.sessMu.RLock()
	sess, ok := s.sessions[digest]
	if !ok || subtle.ConstantTimeCompare(sess.digest[:], digest[:]) != 1 {
		s.sessMu.RUnlock()
		return nil, nil, ErrUnauthenticated
	}
	principalID := sess.principalID
	email := sess.email
	var orgCopy *string
	if sess.orgID != nil {
		v := *sess.orgID
		orgCopy = &v
	}
	csrfToken := sess.csrfToken
	expiresAtMs := sess.expiresAtMs
	s.sessMu.RUnlock()

	if now >= expiresAtMs {
		s.sessMu.Lock()
		if cur, exists := s.sessions[digest]; exists {
			clear(cur.digest[:])
			delete(s.sessions, digest)
		}
		s.sessMu.Unlock()
		return nil, nil, ErrUnauthenticated
	}

	// Re-verify principal remains ACTIVE in DB; only wipe sessions when principal is missing or inactive
	// (never on transient DB errors or request cancellation).
	var dbStatus string
	statusErr := s.db.QueryRowContext(r.Context(), `SELECT status FROM principals WHERE id = ?`, principalID).Scan(&dbStatus)
	if statusErr != nil {
		if errors.Is(statusErr, sql.ErrNoRows) {
			s.RevokePrincipalSessions(principalID)
			return nil, nil, ErrUnauthenticated
		}
		return nil, nil, fmt.Errorf("auth: verify principal status: %w", statusErr)
	}
	if dbStatus != PrincipalStatusActive {
		s.RevokePrincipalSessions(principalID)
		return nil, nil, ErrPrincipalInactive
	}

	// Reload live RBAC bindings from DB so role downgrades or removals take effect immediately.
	liveBindings, liveRoles, bindErr := s.loadBindings(r.Context(), principalID)
	if bindErr != nil {
		return nil, nil, fmt.Errorf("auth: reload principal bindings: %w", bindErr)
	}

	// AUTH-03: Enforce Origin and X-CSRF-Token on all non-safe (state-mutating) Admin API calls.
	if isMutatingMethod(r.Method) {
		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if !s.isAllowedOrigin(origin, r.Host) {
				return nil, nil, ErrCSRFValidationFailed
			}
		}
		csrf := strings.TrimSpace(r.Header.Get(CSRFHeaderName))
		if len(csrf) < MinCSRFTokenLength || subtle.ConstantTimeCompare([]byte(csrf), []byte(csrfToken)) != 1 {
			return nil, nil, ErrCSRFValidationFailed
		}
	}

	actor := &rbac.Actor{
		PrincipalID:     principalID,
		SessionOrgID:    orgCopy,
		CredentialPlane: rbac.CredentialPlaneAdminSession,
		Bindings:        liveBindings,
	}
	info := &SessionInfo{
		PrincipalID: principalID,
		Email:       email,
		OrgID:       orgCopy,
		Roles:       liveRoles,
		Bindings:    append([]rbac.RoleBinding(nil), liveBindings...),
		CSRFToken:   csrfToken,
		ExpiresAtMs: expiresAtMs,
	}
	return actor, info, nil
}

// ExtractDataPlaneVirtualKey enforces AUTH-07 on Data Plane requests (/v1/*, /native/*, /mcp, /a2a/*):
// rejects direct Admin session cookies or non-vk_tp_ credentials with ErrInvalidCredentialPlane (401).
func ExtractDataPlaneVirtualKey(r *http.Request) (string, error) {
	if r == nil {
		return "", ErrUnauthenticated
	}

	rawKey, sawAnyHeaderCredential := extractCandidateDataPlaneKey(r)
	if _, cookieErr := r.Cookie(SessionCookieName); cookieErr == nil && !sawAnyHeaderCredential {
		return "", ErrInvalidCredentialPlane
	}
	if !sawAnyHeaderCredential {
		return "", ErrUnauthenticated
	}
	if rawKey == "" || !strings.HasPrefix(rawKey, "vk_tp_") {
		return "", ErrInvalidCredentialPlane
	}
	return rawKey, nil
}

// Logout terminates an active Admin session by raw cookie token.
func (s *LocalService) Logout(sessionToken string) {
	if sessionToken == "" {
		return
	}
	digest := sha256.Sum256([]byte(sessionToken))
	s.sessMu.Lock()
	if cur, exists := s.sessions[digest]; exists {
		clear(cur.digest[:])
		delete(s.sessions, digest)
	}
	s.sessMu.Unlock()
}

// RevokePrincipalSessions terminates all active Admin sessions held by principalID.
func (s *LocalService) RevokePrincipalSessions(principalID string) {
	if principalID == "" {
		return
	}
	s.sessMu.Lock()
	for k, v := range s.sessions {
		if v.principalID == principalID {
			clear(v.digest[:])
			delete(s.sessions, k)
		}
	}
	s.sessMu.Unlock()
}

// RevokeAllSessions terminates all active Admin sessions (e.g. during break-glass owner reset).
func (s *LocalService) RevokeAllSessions() {
	s.sessMu.Lock()
	for _, v := range s.sessions {
		clear(v.digest[:])
	}
	clear(s.sessions)
	s.sessMu.Unlock()
}

func (s *LocalService) loadBindings(ctx context.Context, principalID string) ([]rbac.RoleBinding, []string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, org_id, principal_id, scope_type, scope_id, role_name
		FROM role_bindings
		WHERE principal_id = ?
		ORDER BY created_at_ms ASC, id ASC`, principalID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var bindings []rbac.RoleBinding
	seenRoles := make(map[string]bool)
	var roles []string
	for rows.Next() {
		var b rbac.RoleBinding
		var orgNS sql.NullString
		if scanErr := rows.Scan(&b.ID, &orgNS, &b.PrincipalID, &b.ScopeType, &b.ScopeID, &b.RoleName); scanErr != nil {
			return nil, nil, scanErr
		}
		if orgNS.Valid {
			v := orgNS.String
			b.OrgID = &v
		}
		bindings = append(bindings, b)
		if !seenRoles[b.RoleName] {
			seenRoles[b.RoleName] = true
			roles = append(roles, b.RoleName)
		}
	}
	return bindings, roles, rows.Err()
}

type rateBucketCheck struct {
	key   string
	limit int
}

func (s *LocalService) verifyPasswordBounded(ctx context.Context, password, encodedHash string) error {
	_, err := s.verifyPasswordWithSemaphore(ctx, password, encodedHash)
	return err
}

func (s *LocalService) verifyPasswordWithSemaphore(ctx context.Context, password, encodedHash string) (bool, error) {
	select {
	case s.argonSem <- struct{}{}:
		defer func() { <-s.argonSem }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	return VerifyPassword(password, encodedHash)
}

func (s *LocalService) globalAcctMaxFailures() int {
	g := s.rateCfg.MaxFailures * globalAcctLimitMult
	if g < 15 {
		g = 15
	}
	return g
}

func (s *LocalService) checkRateLimits(checks []rateBucketCheck, now int64) (int, bool) {
	windowMs := s.rateCfg.Window.Milliseconds()

	s.rlMu.Lock()
	defer s.rlMu.Unlock()

	maxRetry := 0
	missingCount := 0
	for _, c := range checks {
		b := s.failures[c.key]
		if b == nil {
			missingCount++
			continue
		}
		if b.blockedUntil > now {
			rem := int((b.blockedUntil - now + 999) / 1000)
			if rem < 1 {
				rem = 1
			}
			if rem > maxRetry {
				maxRetry = rem
			}
		}
	}
	if maxRetry > 0 {
		return maxRetry, true
	}
	if missingCount > 0 && len(s.failures) >= s.rateCfg.MaxFailureBuckets {
		s.purgeExpiredBucketsLocked(now, windowMs)
		if len(s.failures)+missingCount > s.rateCfg.MaxFailureBuckets {
			if !s.hasEvictableUnblockedLocked(checks, now) {
				// 100% of failure buckets are actively locked out: fail closed instead of bypassing limits.
				return s.minRetryAfterSecondsLocked(now), true
			}
		}
	}
	return 0, false
}

func (s *LocalService) recordFailure(ctx context.Context, checks []rateBucketCheck, normIP, normEmail string, now int64) (int, bool) {
	windowMs := s.rateCfg.Window.Milliseconds()
	blockMs := s.rateCfg.BlockDuration.Milliseconds()
	tripped := false
	saturatedRetryAfter := 0

	s.rlMu.Lock()
	if len(s.failures) >= s.rateCfg.MaxFailureBuckets {
		s.purgeExpiredBucketsLocked(now, windowMs)
	}
	for _, c := range checks {
		b := s.failures[c.key]
		if b == nil || (b.blockedUntil == 0 && now-b.windowStartMs >= windowMs) {
			if b == nil && len(s.failures) >= s.rateCfg.MaxFailureBuckets {
				if !s.evictUnblockedBucketLocked(checks, now) {
					// Fail closed: all failure slots are actively locked out; never allow un-tracked brute force.
					tripped = true
					saturatedRetryAfter = s.minRetryAfterSecondsLocked(now)
					continue
				}
			}
			b = &failureBucket{windowStartMs: now}
			s.failures[c.key] = b
		} else if b.blockedUntil > 0 && now >= b.blockedUntil {
			b.windowStartMs = now
			b.failures = 0
			b.blockedUntil = 0
		}
		b.failures++
		if b.failures >= c.limit {
			b.blockedUntil = now + blockMs
			tripped = true
		}
	}
	s.rlMu.Unlock()

	if tripped {
		s.emitAudit(ctx, nil, "SYSTEM", normIP, "auth.login_rate_limited", "principal", normEmail, `{"reason":"max_failures_exceeded"}`)
	} else {
		s.emitAudit(ctx, nil, "SYSTEM", normIP, "auth.login_failed", "principal", normEmail, `{}`)
	}
	if saturatedRetryAfter > 0 {
		return saturatedRetryAfter, true
	}
	return 0, false
}

func (s *LocalService) purgeExpiredBucketsLocked(now, windowMs int64) {
	for k, b := range s.failures {
		if b.blockedUntil == 0 && now-b.windowStartMs >= windowMs {
			delete(s.failures, k)
		} else if b.blockedUntil > 0 && now >= b.blockedUntil {
			delete(s.failures, k)
		}
	}
}

func (s *LocalService) hasEvictableUnblockedLocked(activeChecks []rateBucketCheck, now int64) bool {
	k, ok := s.pickUnblockedEvictionCandidateLocked(activeChecks, now)
	return ok && k != ""
}

func (s *LocalService) evictUnblockedBucketLocked(activeChecks []rateBucketCheck, now int64) bool {
	victimKey, ok := s.pickUnblockedEvictionCandidateLocked(activeChecks, now)
	if !ok {
		return false
	}
	delete(s.failures, victimKey)
	return true
}

func (s *LocalService) pickUnblockedEvictionCandidateLocked(activeChecks []rateBucketCheck, now int64) (string, bool) {
	inUse := make(map[string]bool, len(activeChecks))
	for _, c := range activeChecks {
		inUse[c.key] = true
	}
	var (
		victimKey    string
		victimGacct  bool
		victimFails  int
		victimWindow int64
		found        bool
	)
	for k, b := range s.failures {
		if inUse[k] || b.blockedUntil > now {
			continue
		}
		isGacct := strings.HasPrefix(k, "gacct:")
		if !found ||
			(!isGacct && victimGacct) ||
			(isGacct == victimGacct && (b.failures < victimFails || (b.failures == victimFails && b.windowStartMs < victimWindow))) {
			victimKey = k
			victimGacct = isGacct
			victimFails = b.failures
			victimWindow = b.windowStartMs
			found = true
		}
	}
	return victimKey, found
}

func (s *LocalService) minRetryAfterSecondsLocked(now int64) int {
	minRem := int(s.rateCfg.BlockDuration.Seconds())
	if minRem < 1 {
		minRem = 1
	}
	found := false
	for _, b := range s.failures {
		if b.blockedUntil > now {
			rem := int((b.blockedUntil - now + 999) / 1000)
			if rem < 1 {
				rem = 1
			}
			if !found || rem < minRem {
				minRem = rem
				found = true
			}
		}
	}
	return minRem
}

func (s *LocalService) clearFailures(keys []string) {
	s.rlMu.Lock()
	for _, k := range keys {
		delete(s.failures, k)
	}
	s.rlMu.Unlock()
}

func (s *LocalService) emitAudit(ctx context.Context, orgID *string, actorKind, actorID, eventType, targetType, targetID, meta string) {
	if s.auditSink == nil {
		return
	}
	id, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return
	}
	_ = s.auditSink.Append(ctx, port.AuditEvent{
		ID:           id,
		CreatedAtMs:  s.nowMs(),
		OrgID:        orgID,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   "admin_api",
		EventType:    eventType,
		TargetType:   targetType,
		TargetID:     targetID,
		MetadataJSON: meta,
	})
}

func (s *LocalService) isAllowedOrigin(origin, reqHost string) bool {
	norm := strings.ToLower(strings.TrimRight(origin, "/"))
	if len(s.allowedOrigins) > 0 {
		return s.allowedOrigins[norm]
	}
	parsed, err := url.Parse(norm)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return false
	}
	if parsed.Scheme != "https" {
		return false
	}
	if reqHost != "" && strings.EqualFold(parsed.Host, reqHost) {
		return true
	}
	return false
}

func isMutatingMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func hasVirtualKeyHeader(r *http.Request) bool {
	for _, rawAuth := range r.Header.Values("Authorization") {
		if tok := extractBearerToken(rawAuth); strings.HasPrefix(tok, "vk_tp_") {
			return true
		}
	}
	for _, h := range []string{"X-Tanpopo-Virtual-Key", "x-api-key", "x-goog-api-key"} {
		for _, v := range r.Header.Values(h) {
			if strings.HasPrefix(strings.TrimSpace(v), "vk_tp_") {
				return true
			}
		}
	}
	return false
}

func extractCandidateDataPlaneKey(r *http.Request) (key string, sawAnyCredential bool) {
	var firstVK string
	for _, rawAuth := range r.Header.Values("Authorization") {
		trimmed := strings.TrimSpace(rawAuth)
		if trimmed == "" {
			continue
		}
		sawAnyCredential = true
		tok := extractBearerToken(trimmed)
		if tok == "" || !strings.HasPrefix(tok, "vk_tp_") {
			return "", true
		}
		if firstVK == "" {
			firstVK = tok
		} else if tok != firstVK {
			return "", true
		}
	}
	for _, h := range []string{"X-Tanpopo-Virtual-Key", "x-api-key", "x-goog-api-key"} {
		for _, rawV := range r.Header.Values(h) {
			trimmed := strings.TrimSpace(rawV)
			if trimmed == "" {
				continue
			}
			sawAnyCredential = true
			if !strings.HasPrefix(trimmed, "vk_tp_") {
				return "", true
			}
			if firstVK == "" {
				firstVK = trimmed
			} else if trimmed != firstVK {
				return "", true
			}
		}
	}
	return firstVK, sawAnyCredential
}

func extractBearerToken(authHeader string) string {
	fields := strings.Fields(authHeader)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") {
		return strings.TrimSpace(fields[1])
	}
	return ""
}

func randomURLToken(numBytes int) (string, error) {
	if numBytes < 24 {
		numBytes = 24
	}
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: rand read: %w", err)
	}
	defer clear(buf)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ParseRetryAfter parses an HTTP Retry-After integer header value cleanly.
func ParseRetryAfter(headerVal string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(headerVal))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
