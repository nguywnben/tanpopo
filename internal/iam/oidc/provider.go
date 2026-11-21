// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package oidc implements Enterprise OIDC SSO Authorization Code + PKCE (S256)
// login, JWKS signature verification (RS256/ES256) routed strictly through
// SSRFGuardDialer, strict iss/aud/exp/nonce checks, and deterministic
// group-to-6-role RBAC synchronization with automatic privilege revocation
// on group removal (TP-FR-0504 / AUTH-01 / TP-IMP-0041).
package oidc

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	maxOIDCHTTPBodyBytes   = 1 << 20
	maxPendingStates       = 4096
	defaultStateTTL        = 10 * time.Minute
	defaultJWKSCacheTTL    = 15 * time.Minute
	minJWKSRefreshCooldown = 2 * time.Second
	clockSkewTolerance     = 30 * time.Second
)

// Canonical OIDC error sentinels mapped to 401/403 HTTP envelopes.
var (
	ErrInvalidOIDCConfig  = errors.New("oidc: invalid provider configuration")
	ErrInvalidOIDCToken   = errors.New("oidc: invalid_oidc_token")
	ErrInvalidOIDCState   = errors.New("oidc: invalid or expired login state")
	ErrGroupMappingDenied = errors.New("oidc: no permitted RBAC role mapped for claims")
)

// GroupRoleMapping deterministically maps one IdP group string to an org-scoped canonical RBAC binding.
type GroupRoleMapping struct {
	Group     string `json:"group"`
	OrgID     string `json:"org_id"`
	ScopeType string `json:"scope_type"` // ORG | PROJECT
	ScopeID   string `json:"scope_id"`
	RoleName  string `json:"role_name"`
}

// Config configures the OIDC Provider fail-closed.
type Config struct {
	OrgID                 string
	Issuer                string
	ClientID              string
	ClientSecret          string
	RedirectURI           string
	AuthorizationEndpoint string
	TokenEndpoint         string
	JWKSURI               string
	UserInfoEndpoint      string
	Scopes                []string
	GroupsClaim           string
	GroupMappings         []GroupRoleMapping
	AllowOperatorRole     bool
	RequireGroupsMatch    bool
	DB                    *sql.DB
	Auth                  *auth.LocalService
	AuditSink             port.TxAuditSink
	HTTPClient            *http.Client
	EgressPolicy          *egress.Policy
	HostResolver          egress.HostResolver
	StateTTL              time.Duration
	JWKSCacheTTL          time.Duration
	Now                   func() time.Time
}

type pendingAuthState struct {
	nonce        string
	codeVerifier string
	expiresAt    time.Time
}

type verifiedClaims struct {
	Subject string
	Email   string
	Nonce   string
	Groups  []string
}

// CallbackResult contains the authenticated session cookie, CSRF token metadata, and optional refresh token.
type CallbackResult struct {
	PrincipalID  string
	Subject      string
	Email        string
	Groups       []string
	Roles        []string
	Session      *auth.SessionInfo
	Cookie       *http.Cookie
	RefreshToken string
}

// Provider manages OIDC Authorization Code + PKCE (S256) authentication, JWKS signature verification,
// and deterministic database RBAC role synchronization.
type Provider struct {
	cfg           Config
	dialect       port.Dialect
	client        *http.Client
	groupsClaim   string
	mappingsByGrp map[string][]GroupRoleMapping
	now           func() time.Time

	stateMu sync.Mutex
	states  map[string]pendingAuthState

	jwksMu          sync.Mutex
	jwksKeys        map[string]crypto.PublicKey
	jwksFetchedAt   time.Time
	jwksLastAttempt time.Time
}

// NewProvider validates configuration and constructs an SSRF-guarded OIDC Provider.
func NewProvider(cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.OrgID) == "" {
		return nil, fmt.Errorf("%w: org_id is required to prevent cross-tenant escalation", ErrInvalidOIDCConfig)
	}
	if strings.TrimSpace(cfg.Issuer) == "" || strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("%w: issuer and client_id required", ErrInvalidOIDCConfig)
	}
	if strings.TrimSpace(cfg.RedirectURI) == "" || strings.TrimSpace(cfg.AuthorizationEndpoint) == "" ||
		strings.TrimSpace(cfg.TokenEndpoint) == "" || strings.TrimSpace(cfg.JWKSURI) == "" {
		return nil, fmt.Errorf("%w: redirect_uri, authorization_endpoint, token_endpoint, and jwks_uri required", ErrInvalidOIDCConfig)
	}
	if cfg.DB == nil || cfg.Auth == nil {
		return nil, fmt.Errorf("%w: non-nil DB and Auth service required", ErrInvalidOIDCConfig)
	}
	client, err := buildOIDCHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	mappings := make(map[string][]GroupRoleMapping)
	for _, m := range cfg.GroupMappings {
		grp := strings.TrimSpace(m.Group)
		orgID := strings.TrimSpace(m.OrgID)
		if orgID == "" {
			orgID = strings.TrimSpace(cfg.OrgID)
		}
		if orgID != strings.TrimSpace(cfg.OrgID) {
			return nil, fmt.Errorf("%w: cross-organization group mapping %q denied", ErrInvalidOIDCConfig, m.Group)
		}
		scopeType := strings.ToUpper(strings.TrimSpace(m.ScopeType))
		if scopeType == "" {
			scopeType = rbac.ScopeOrg
		}
		scopeID := strings.TrimSpace(m.ScopeID)
		if scopeType == rbac.ScopeOrg && scopeID == "" {
			scopeID = orgID
		}
		role := strings.ToUpper(strings.TrimSpace(m.RoleName))
		if grp == "" || scopeID == "" || !isValidCanonicalRole(role) {
			return nil, fmt.Errorf("%w: invalid group mapping %+v", ErrInvalidOIDCConfig, m)
		}
		if (role == rbac.RoleOperator || scopeType == rbac.ScopeInstance) && !cfg.AllowOperatorRole {
			return nil, fmt.Errorf("%w: instance OPERATOR escalation forbidden in OIDC mapping for group %q", ErrInvalidOIDCConfig, grp)
		}
		mappings[grp] = append(mappings[grp], GroupRoleMapping{
			Group:     grp,
			OrgID:     orgID,
			ScopeType: scopeType,
			ScopeID:   scopeID,
			RoleName:  role,
		})
	}

	if cfg.StateTTL <= 0 {
		cfg.StateTTL = defaultStateTTL
	}
	if cfg.JWKSCacheTTL <= 0 {
		cfg.JWKSCacheTTL = defaultJWKSCacheTTL
	}
	groupsClaim := strings.TrimSpace(cfg.GroupsClaim)
	if groupsClaim == "" {
		groupsClaim = "groups"
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &Provider{
		cfg:           cfg,
		dialect:       port.DetectDialect(context.Background(), cfg.DB),
		client:        client,
		groupsClaim:   groupsClaim,
		mappingsByGrp: mappings,
		now:           nowFn,
		states:        make(map[string]pendingAuthState),
		jwksKeys:      make(map[string]crypto.PublicKey),
	}, nil
}

// StartAuth generates CSPRNG state, nonce, and PKCE S256 verifier and returns the IdP redirect URL.
func (p *Provider) StartAuth(_ context.Context) (redirectURL, state string, err error) {
	u, err := url.Parse(strings.TrimSpace(p.cfg.AuthorizationEndpoint))
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("%w: invalid authorization_endpoint", ErrInvalidOIDCConfig)
	}
	stateVal, err := randomURLSafeToken(32)
	if err != nil {
		return "", "", err
	}
	nonceVal, err := randomURLSafeToken(32)
	if err != nil {
		return "", "", err
	}
	verifier, err := randomURLSafeToken(48)
	if err != nil {
		return "", "", err
	}
	chSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(chSum[:])

	now := p.now()
	p.stateMu.Lock()
	for k, v := range p.states {
		if !v.expiresAt.After(now) {
			delete(p.states, k)
		}
	}
	if len(p.states) >= maxPendingStates {
		var oldestK string
		var oldestT time.Time
		first := true
		for k, v := range p.states {
			if first || v.expiresAt.Before(oldestT) {
				oldestK = k
				oldestT = v.expiresAt
				first = false
			}
		}
		if oldestK != "" {
			delete(p.states, oldestK)
		}
	}
	p.states[stateVal] = pendingAuthState{
		nonce:        nonceVal,
		codeVerifier: verifier,
		expiresAt:    now.Add(p.cfg.StateTTL),
	}
	p.stateMu.Unlock()

	scopes := p.cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile", "groups"}
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURI)
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", stateVal)
	q.Set("nonce", nonceVal)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), stateVal, nil
}

// HandleCallback consumes single-use state, exchanges the authorization code with PKCE verifier,
// validates the ID Token signature/claims/nonce, synchronizes RBAC bindings, and mints a Tanpopo session.
func (p *Provider) HandleCallback(ctx context.Context, code, state string) (*CallbackResult, error) {
	trimmedCode := strings.TrimSpace(code)
	trimmedState := strings.TrimSpace(state)
	if trimmedCode == "" || trimmedState == "" {
		return nil, ErrInvalidOIDCState
	}

	now := p.now()
	p.stateMu.Lock()
	st, ok := p.states[trimmedState]
	if ok {
		delete(p.states, trimmedState)
	}
	p.stateMu.Unlock()
	if !ok || !st.expiresAt.After(now) {
		return nil, ErrInvalidOIDCState
	}

	tokResp, err := p.exchangeCode(ctx, trimmedCode, st.codeVerifier)
	if err != nil {
		return nil, err
	}
	claims, err := p.VerifyIDToken(ctx, tokResp.IDToken, st.nonce)
	if err != nil {
		return nil, err
	}
	return p.syncAndIssueSession(ctx, claims, tokResp.RefreshToken, "auth.oidc_login_succeeded", "")
}

// RefreshSession exchanges refreshToken at TokenEndpoint, verifies the refreshed ID Token
// (or fallback UserInfo response), synchronizes RBAC group bindings (revoking removed groups),
// and issues an updated session.
func (p *Provider) RefreshSession(ctx context.Context, principalID, refreshToken string) (*CallbackResult, error) {
	if strings.TrimSpace(principalID) == "" || strings.TrimSpace(refreshToken) == "" {
		return nil, ErrInvalidOIDCToken
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", strings.TrimSpace(refreshToken))
	form.Set("client_id", p.cfg.ClientID)
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}

	tokResp, err := p.postTokenForm(ctx, form)
	if err != nil {
		return nil, err
	}
	claims, err := p.VerifyIDToken(ctx, tokResp.IDToken, "")
	if err != nil {
		return nil, err
	}
	expectedPID := strings.TrimSpace(principalID)
	res, err := p.syncAndIssueSession(ctx, claims, tokResp.RefreshToken, "auth.oidc_session_refreshed", expectedPID)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// SyncGroupsFromIDToken verifies rawIDToken (ignoring nonce on out-of-band refresh when expectedNonce=="")
// and deterministically adds/revokes group-mapped RBAC role bindings for the principal in DB.
func (p *Provider) SyncGroupsFromIDToken(ctx context.Context, rawIDToken, expectedNonce string) (*CallbackResult, error) {
	claims, err := p.VerifyIDToken(ctx, rawIDToken, expectedNonce)
	if err != nil {
		return nil, err
	}
	return p.syncAndIssueSession(ctx, claims, "", "auth.oidc_groups_synced", "")
}

// VerifyIDToken validates a signed OIDC ID Token JWT against cached/refreshed JWKS.
// Hard-rejects alg="none", symmetric HMAC, embedded jwk/jku/x5u headers, unknown kid,
// mismatched iss/aud/nonce, and expired timestamps with ErrInvalidOIDCToken.
func (p *Provider) VerifyIDToken(ctx context.Context, rawJWT, expectedNonce string) (*verifiedClaims, error) {
	parts := strings.Split(strings.TrimSpace(rawJWT), ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("%w: malformed JWT segments", ErrInvalidOIDCToken)
	}
	hdrBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JWT header encoding", ErrInvalidOIDCToken)
	}
	var hdr map[string]any
	if err := json.Unmarshal(hdrBytes, &hdr); err != nil {
		return nil, fmt.Errorf("%w: invalid JWT header JSON", ErrInvalidOIDCToken)
	}
	for _, forbidden := range []string{"jwk", "jku", "x5u", "crit"} {
		if _, exists := hdr[forbidden]; exists {
			return nil, fmt.Errorf("%w: forbidden header %q", ErrInvalidOIDCToken, forbidden)
		}
	}
	alg, _ := hdr["alg"].(string)
	if alg != "RS256" && alg != "ES256" {
		return nil, fmt.Errorf("%w: unsupported or insecure JWT alg %q", ErrInvalidOIDCToken, alg)
	}
	kid, _ := hdr["kid"].(string)

	pubKey, err := p.resolveSigningKey(ctx, strings.TrimSpace(kid), alg)
	if err != nil {
		return nil, err
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JWT signature encoding", ErrInvalidOIDCToken)
	}
	signingInput := []byte(parts[0] + "." + parts[1])
	digest := sha256.Sum256(signingInput)

	switch alg {
	case "RS256":
		rsaPub, ok := pubKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: JWKS key type mismatch for RS256", ErrInvalidOIDCToken)
		}
		if err := rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, digest[:], sigBytes); err != nil {
			return nil, fmt.Errorf("%w: RSA signature verification failed", ErrInvalidOIDCToken)
		}
	case "ES256":
		ecPub, ok := pubKey.(*ecdsa.PublicKey)
		if !ok || ecPub.Curve != elliptic.P256() {
			return nil, fmt.Errorf("%w: JWKS key type mismatch for ES256", ErrInvalidOIDCToken)
		}
		if len(sigBytes) != 64 {
			return nil, fmt.Errorf("%w: invalid ES256 signature length", ErrInvalidOIDCToken)
		}
		rBig := new(big.Int).SetBytes(sigBytes[:32])
		sBig := new(big.Int).SetBytes(sigBytes[32:])
		if !ecdsa.Verify(ecPub, digest[:], rBig, sBig) {
			return nil, fmt.Errorf("%w: ECDSA signature verification failed", ErrInvalidOIDCToken)
		}
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid JWT payload encoding", ErrInvalidOIDCToken)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid JWT payload JSON", ErrInvalidOIDCToken)
	}

	iss, _ := payload["iss"].(string)
	if strings.TrimSpace(iss) != strings.TrimSpace(p.cfg.Issuer) {
		return nil, fmt.Errorf("%w: issuer mismatch", ErrInvalidOIDCToken)
	}
	if !audContainsClientID(payload["aud"], payload["azp"], p.cfg.ClientID) {
		return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidOIDCToken)
	}

	now := p.now()
	expSec, hasExp := parseNumericClaim(payload["exp"])
	if !hasExp || now.Add(-clockSkewTolerance).Unix() >= expSec {
		return nil, fmt.Errorf("%w: token expired", ErrInvalidOIDCToken)
	}
	if nbfSec, hasNbf := parseNumericClaim(payload["nbf"]); hasNbf && now.Add(clockSkewTolerance).Unix() < nbfSec {
		return nil, fmt.Errorf("%w: token not yet valid", ErrInvalidOIDCToken)
	}

	nonce, _ := payload["nonce"].(string)
	if expectedNonce != "" {
		if len(nonce) == 0 || subtle.ConstantTimeCompare([]byte(nonce), []byte(expectedNonce)) != 1 {
			return nil, fmt.Errorf("%w: nonce mismatch", ErrInvalidOIDCToken)
		}
	}

	sub, _ := payload["sub"].(string)
	sub = strings.TrimSpace(sub)
	if sub == "" {
		return nil, fmt.Errorf("%w: missing subject", ErrInvalidOIDCToken)
	}
	email, _ := payload["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		email = sub
	}

	groups := extractGroupsClaim(payload[p.groupsClaim])
	return &verifiedClaims{
		Subject: sub,
		Email:   email,
		Nonce:   nonce,
		Groups:  groups,
	}, nil
}

func (p *Provider) syncAndIssueSession(ctx context.Context, claims *verifiedClaims, refreshToken, auditEvent, expectedPrincipalID string) (*CallbackResult, error) {
	targetBindings := make([]GroupRoleMapping, 0)
	seen := make(map[string]struct{})
	for _, g := range claims.Groups {
		for _, m := range p.mappingsByGrp[g] {
			key := m.OrgID + "|" + m.ScopeType + "|" + m.ScopeID + "|" + m.RoleName
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			targetBindings = append(targetBindings, m)
		}
	}
	if p.cfg.RequireGroupsMatch && len(targetBindings) == 0 {
		return nil, ErrGroupMappingDenied
	}

	orgID := strings.TrimSpace(p.cfg.OrgID)
	nowMs := p.now().UnixMilli()

	tx, err := p.cfg.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := port.SetOrgRLSTx(ctx, tx, p.dialect, orgID); err != nil {
		return nil, err
	}

	var principalID string
	var status string
	lookupQ := port.Rebind(p.dialect, `
		SELECT id, status FROM principals
		WHERE org_id = ? AND kind = ? AND email_or_sub = ?`)
	err = tx.QueryRowContext(ctx, lookupQ, orgID, auth.PrincipalKindHumanOIDC, claims.Subject).Scan(&principalID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if expectedPrincipalID != "" {
			return nil, fmt.Errorf("%w: token subject does not match authenticated principal", ErrInvalidOIDCToken)
		}
		principalID, err = idgen.New(idgen.PrefixPrincipal)
		if err != nil {
			return nil, err
		}
		insQ := port.Rebind(p.dialect, `
			INSERT INTO principals (
				id, org_id, kind, email_or_sub, oidc_issuer, status, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, 'ACTIVE', 1, ?, ?)`)
		if _, err := tx.ExecContext(ctx, insQ, principalID, orgID, auth.PrincipalKindHumanOIDC, claims.Subject, p.cfg.Issuer, nowMs, nowMs); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		if expectedPrincipalID != "" && principalID != expectedPrincipalID {
			return nil, fmt.Errorf("%w: token subject does not match authenticated principal", ErrInvalidOIDCToken)
		}
		if status != auth.PrincipalStatusActive {
			return nil, auth.ErrPrincipalInactive
		}
	}

	// Load current bindings in this org for this principal and perform deterministic add/remove reconciliation.
	type existingBinding struct {
		id  string
		key string
	}
	existing := make([]existingBinding, 0)
	curRows, err := tx.QueryContext(ctx, port.Rebind(p.dialect, `
		SELECT id, org_id, scope_type, scope_id, role_name
		FROM role_bindings
		WHERE org_id = ? AND principal_id = ?`), orgID, principalID)
	if err != nil {
		return nil, err
	}
	for curRows.Next() {
		var bid, bOrg, st, sid, role string
		if serr := curRows.Scan(&bid, &bOrg, &st, &sid, &role); serr != nil {
			_ = curRows.Close()
			return nil, serr
		}
		existing = append(existing, existingBinding{
			id:  bid,
			key: bOrg + "|" + st + "|" + sid + "|" + role,
		})
	}
	if rerr := curRows.Err(); rerr != nil {
		_ = curRows.Close()
		return nil, rerr
	}
	_ = curRows.Close()

	desiredSet := make(map[string]GroupRoleMapping, len(targetBindings))
	for _, m := range targetBindings {
		desiredSet[m.OrgID+"|"+m.ScopeType+"|"+m.ScopeID+"|"+m.RoleName] = m
	}

	existingSet := make(map[string]struct{}, len(existing))
	for _, ex := range existing {
		existingSet[ex.key] = struct{}{}
		if _, keep := desiredSet[ex.key]; !keep {
			if _, derr := tx.ExecContext(ctx, port.Rebind(p.dialect, `DELETE FROM role_bindings WHERE org_id = ? AND id = ?`), orgID, ex.id); derr != nil {
				return nil, derr
			}
		}
	}

	rolesOut := make([]string, 0, len(targetBindings))
	for key, m := range desiredSet {
		rolesOut = append(rolesOut, m.RoleName)
		if _, exists := existingSet[key]; exists {
			continue
		}
		rbRawID, rbErr := idgen.New(idgen.PrefixApproval)
		if rbErr != nil {
			return nil, rbErr
		}
		rbID := "rb_" + strings.TrimPrefix(rbRawID, idgen.PrefixApproval+"_")
		if _, ierr := tx.ExecContext(ctx, port.Rebind(p.dialect, `
			INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT (principal_id, scope_type, scope_id, role_name) DO NOTHING`),
			rbID, orgID, principalID, m.ScopeType, m.ScopeID, m.RoleName, nowMs,
		); ierr != nil {
			return nil, ierr
		}
	}
	sort.Strings(rolesOut)

	if p.cfg.AuditSink != nil {
		audID, aerr := idgen.New(idgen.PrefixAudit)
		if aerr != nil {
			return nil, aerr
		}
		metaBytes, _ := json.Marshal(map[string]any{
			"issuer":  p.cfg.Issuer,
			"subject": claims.Subject,
			"groups":  claims.Groups,
			"roles":   rolesOut,
		})
		if err := p.cfg.AuditSink.AppendTx(ctx, tx, port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  nowMs,
			OrgID:        &orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      principalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    auditEvent,
			TargetType:   "principal",
			TargetID:     principalID,
			MetadataJSON: string(metaBytes),
		}); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true

	sessInfo, cookie, err := p.cfg.Auth.IssueExternalSession(ctx, auth.ExternalSessionParams{
		PrincipalID:     principalID,
		Email:           claims.Email,
		OrgID:           &orgID,
		CredentialPlane: rbac.CredentialPlaneAdminOIDC,
		AuditEventType:  auditEvent,
	})
	if err != nil {
		return nil, err
	}
	return &CallbackResult{
		PrincipalID:  principalID,
		Subject:      claims.Subject,
		Email:        claims.Email,
		Groups:       append([]string(nil), claims.Groups...),
		Roles:        rolesOut,
		Session:      sessInfo,
		Cookie:       cookie,
		RefreshToken: refreshToken,
	}, nil
}

type oidcTokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

func (p *Provider) exchangeCode(ctx context.Context, code, codeVerifier string) (*oidcTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", codeVerifier)
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	return p.postTokenForm(ctx, form)
}

func (p *Provider) postTokenForm(ctx context.Context, form url.Values) (*oidcTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOIDCHTTPBodyBytes))
	if err != nil {
		return nil, err
	}
	defer clear(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: token endpoint returned HTTP %d", ErrInvalidOIDCToken, resp.StatusCode)
	}
	var out oidcTokenResponse
	if err := json.Unmarshal(body, &out); err != nil || strings.TrimSpace(out.IDToken) == "" {
		return nil, fmt.Errorf("%w: token endpoint response missing id_token", ErrInvalidOIDCToken)
	}
	return &out, nil
}

func (p *Provider) resolveSigningKey(ctx context.Context, kid, alg string) (crypto.PublicKey, error) {
	now := p.now()
	p.jwksMu.Lock()
	defer p.jwksMu.Unlock()

	cacheExpired := p.jwksFetchedAt.IsZero() || now.Sub(p.jwksFetchedAt) >= p.cfg.JWKSCacheTTL
	if !cacheExpired {
		if key := lookupKeyMap(p.jwksKeys, kid); key != nil {
			return key, nil
		}
		// Unknown kid: throttle forced JWKS refresh to avoid amplification attacks.
		if !p.jwksLastAttempt.IsZero() && now.Sub(p.jwksLastAttempt) < minJWKSRefreshCooldown {
			return nil, fmt.Errorf("%w: unknown signing key kid %q", ErrInvalidOIDCToken, kid)
		}
	}

	p.jwksLastAttempt = now
	fetched, err := p.fetchJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: jwks fetch failed: %v", ErrInvalidOIDCToken, err)
	}
	p.jwksKeys = fetched
	p.jwksFetchedAt = now

	key := lookupKeyMap(p.jwksKeys, kid)
	if key == nil {
		return nil, fmt.Errorf("%w: signing key kid %q absent from JWKS", ErrInvalidOIDCToken, kid)
	}
	_ = alg
	return key, nil
}

func lookupKeyMap(m map[string]crypto.PublicKey, kid string) crypto.PublicKey {
	if kid != "" {
		return m[kid]
	}
	if len(m) == 1 {
		for _, v := range m {
			return v
		}
	}
	return nil
}

func (p *Provider) fetchJWKS(ctx context.Context) (map[string]crypto.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.JWKSURI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOIDCHTTPBodyBytes))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Keys []jsonJWK `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]crypto.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		pub, perr := parseJWKPublicKey(k)
		if perr != nil {
			continue
		}
		kid := strings.TrimSpace(k.Kid)
		if kid == "" {
			kid = fmt.Sprintf("_anon_%d", len(out))
		}
		out[kid] = pub
	}
	return out, nil
}

type jsonJWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func parseJWKPublicKey(k jsonJWK) (crypto.PublicKey, error) {
	switch k.Kty {
	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil || len(nBytes) < 256 {
			return nil, errors.New("invalid RSA modulus")
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil || len(eBytes) == 0 {
			return nil, errors.New("invalid RSA exponent")
		}
		eInt := new(big.Int).SetBytes(eBytes).Int64()
		if eInt < 3 {
			return nil, errors.New("insecure RSA exponent")
		}
		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(eInt),
		}, nil
	case "EC":
		if k.Crv != "P-256" {
			return nil, errors.New("unsupported EC curve")
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil || len(xBytes) == 0 {
			return nil, errors.New("invalid EC X coordinate")
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil || len(yBytes) == 0 {
			return nil, errors.New("invalid EC Y coordinate")
		}
		x := new(big.Int).SetBytes(xBytes)
		y := new(big.Int).SetBytes(yBytes)
		if !elliptic.P256().IsOnCurve(x, y) {
			return nil, errors.New("EC point not on P-256 curve")
		}
		return &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     x,
			Y:     y,
		}, nil
	default:
		return nil, errors.New("unsupported JWK kty")
	}
}

func audContainsClientID(audClaim, azpClaim any, clientID string) bool {
	if azpClaim != nil {
		azp, ok := azpClaim.(string)
		if !ok || subtle.ConstantTimeCompare([]byte(azp), []byte(clientID)) != 1 {
			return false
		}
	}
	switch v := audClaim.(type) {
	case string:
		return subtle.ConstantTimeCompare([]byte(v), []byte(clientID)) == 1
	case []any:
		found := false
		for _, item := range v {
			if s, ok := item.(string); ok && subtle.ConstantTimeCompare([]byte(s), []byte(clientID)) == 1 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		if len(v) > 1 {
			azp, _ := azpClaim.(string)
			return subtle.ConstantTimeCompare([]byte(azp), []byte(clientID)) == 1
		}
		return true
	default:
		return false
	}
}

func parseNumericClaim(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func extractGroupsClaim(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func isValidCanonicalRole(r string) bool {
	switch r {
	case rbac.RoleOperator, rbac.RoleOrgOwner, rbac.RoleTeamAdmin, rbac.RoleSecurityReviewer, rbac.RoleFinOpsViewer, rbac.RoleDeveloper:
		return true
	default:
		return false
	}
}

func randomURLSafeToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var zeros [64]byte
	if byteLen <= len(zeros) && bytes.Equal(buf, zeros[:byteLen]) {
		return "", errors.New("oidc: degenerate entropy generated")
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func buildOIDCHTTPClient(cfg Config) (*http.Client, error) {
	if cfg.HTTPClient != nil {
		if cfg.HTTPClient.Transport == nil {
			return nil, fmt.Errorf("%w: custom HTTPClient requires non-nil Transport", ErrInvalidOIDCConfig)
		}
		return cfg.HTTPClient, nil
	}
	var pol egress.Policy
	if cfg.EgressPolicy != nil {
		pol = *cfg.EgressPolicy
	} else {
		hostSet := map[string]struct{}{}
		var hosts []string
		for _, raw := range []string{cfg.TokenEndpoint, cfg.JWKSURI, cfg.UserInfoEndpoint} {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			u, err := url.Parse(strings.TrimSpace(raw))
			if err != nil || u.Host == "" {
				return nil, fmt.Errorf("%w: invalid OIDC endpoint URL %q", ErrInvalidOIDCConfig, raw)
			}
			if _, seen := hostSet[u.Host]; !seen {
				hostSet[u.Host] = struct{}{}
				hosts = append(hosts, u.Host)
			}
		}
		pol = egress.Policy{
			AllowedHosts:   hosts,
			AllowPrivateIP: false,
			RequireTLS:     true,
		}
	}
	return egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy:                   pol,
		Timeout:                  10 * time.Second,
		Resolver:                 cfg.HostResolver,
		DenyCrossOriginRedirects: true,
	})
}
