// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type ServiceConfig struct {
	DB                *sql.DB
	Vault             *vault.Store
	UnsealCapability  vault.UnsealCapability
	Audit             port.TxAuditSink
	NowMs             func() int64
	HostResolver      egress.HostResolver
	RedirectURI       string
	AllowLoopbackHTTP bool
}

type Service struct {
	db        *sql.DB
	vault     *vault.Store
	cap       vault.UnsealCapability
	audit     port.TxAuditSink
	nowMs     func() int64
	resolver  egress.HostResolver
	redirect  string
	allowHTTP bool
	refreshes singleflight.Group
}

type StartInput struct {
	OrgID                  string
	PrincipalID            string
	ConnectionID           string
	Flow                   string   `json:"flow"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint"`
	TokenEndpoint          string   `json:"token_endpoint"`
	DeviceAuthorizationURL string   `json:"device_authorization_endpoint"`
	ClientID               string   `json:"client_id"`
	Scopes                 []string `json:"scopes"`
	Resource               string   `json:"resource"`
}

type StartResult struct {
	Flow             string `json:"flow"`
	State            string `json:"state"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
	VerificationURI  string `json:"verification_uri,omitempty"`
	UserCode         string `json:"user_code,omitempty"`
	ExpiresAt        string `json:"expires_at"`
	PollIntervalMs   int64  `json:"poll_interval_ms,omitempty"`
	Status           string `json:"status,omitempty"`
}

type PollInput struct {
	OrgID       string
	PrincipalID string
	State       string
}

type sessionPayload struct {
	Verifier     []byte `json:"verifier,omitempty"`
	DeviceCode   []byte `json:"device_code,omitempty"`
	TokenURL     string `json:"token_url"`
	ClientID     string `json:"client_id"`
	Scopes       string `json:"scopes,omitempty"`
	Resource     string `json:"resource"`
	ConnectionID string `json:"connection_id"`
}

func (p *sessionPayload) zeroize() {
	if p == nil {
		return
	}
	clear(p.Verifier)
	clear(p.DeviceCode)
	p.Verifier, p.DeviceCode = nil, nil
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.DB == nil || cfg.Vault == nil || cfg.Audit == nil {
		return nil, fmt.Errorf("oauth: db, vault, and transactional audit sink are required")
	}
	if cfg.NowMs == nil {
		cfg.NowMs = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	redirect, err := url.Parse(strings.TrimSpace(cfg.RedirectURI))
	if err != nil || redirect == nil || redirect.Hostname() == "" || redirect.User != nil || redirect.Fragment != "" {
		return nil, fmt.Errorf("oauth: a valid callback redirect URI is required")
	}
	// OAuth loopback redirect URIs are permitted by the native-app profile; this exception
	// does not apply to outbound authorization, token, or device endpoints.
	if redirect.Scheme != "https" && !(redirect.Scheme == "http" && isLoopbackHost(redirect.Hostname())) {
		return nil, fmt.Errorf("oauth: callback redirect URI must use HTTPS")
	}
	if redirect.Path != "/admin/v1/oauth/callback" {
		return nil, fmt.Errorf("oauth: callback redirect URI path must be /admin/v1/oauth/callback")
	}
	return &Service{db: cfg.DB, vault: cfg.Vault, cap: cfg.UnsealCapability, audit: cfg.Audit, nowMs: cfg.NowMs, resolver: cfg.HostResolver, redirect: redirect.String(), allowHTTP: cfg.AllowLoopbackHTTP}, nil
}

func (s *Service) Start(ctx context.Context, in StartInput) (*StartResult, error) {
	if ctx == nil || strings.TrimSpace(in.OrgID) == "" || strings.TrimSpace(in.PrincipalID) == "" || strings.TrimSpace(in.ConnectionID) == "" {
		return nil, fmt.Errorf("%w: authenticated organization, principal, and connection are required", ErrInvalidOAuthInput)
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	in.Resource = strings.TrimSpace(in.Resource)
	if in.ClientID == "" || in.Resource == "" {
		return nil, fmt.Errorf("%w: client_id and RFC 8707 resource are required", ErrInvalidOAuthInput)
	}
	resourceURL, err := url.Parse(in.Resource)
	if err != nil || resourceURL.Scheme != "https" || resourceURL.Hostname() == "" || resourceURL.User != nil || resourceURL.Fragment != "" {
		return nil, fmt.Errorf("%w: resource must be an HTTPS URI", ErrInvalidOAuthInput)
	}
	conn, err := s.loadConnection(ctx, in.OrgID, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	if conn.authMode != "OAUTH2" || conn.state == "REVOKED" || conn.state == "ARCHIVED" || conn.state == "SUSPENDED_BY_ADMIN" {
		return nil, fmt.Errorf("%w: connection is not an active OAuth 2.1 connection", ErrInvalidOAuthInput)
	}
	if err := s.ensureOAuthSlotOnly(ctx, in.OrgID, in.ConnectionID); err != nil {
		return nil, err
	}
	tokenURL, err := ValidateEndpoint(in.TokenEndpoint, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	if in.Scopes == nil {
		in.Scopes = []string{}
	}
	for i, scope := range in.Scopes {
		if strings.TrimSpace(scope) == "" || strings.ContainsAny(scope, "\x00\r\n") {
			return nil, fmt.Errorf("%w: invalid scope at index %d", ErrInvalidOAuthInput, i)
		}
		in.Scopes[i] = strings.TrimSpace(scope)
	}
	scope := strings.Join(in.Scopes, " ")
	switch strings.ToUpper(strings.TrimSpace(in.Flow)) {
	case "AUTHORIZATION_CODE_PKCE", "AUTHORIZATION_CODE", "authorization_code_pkce":
		return s.startPKCE(ctx, in, conn, tokenURL, scope)
	case "DEVICE_CODE", "device_code":
		return s.startDevice(ctx, in, conn, tokenURL, scope)
	case GrantClientCredentials, "client_credentials":
		return s.startClientCredentials(ctx, in, conn, tokenURL, scope)
	default:
		return nil, fmt.Errorf("%w: unsupported OAuth grant", ErrInvalidOAuthInput)
	}
}

type connectionContext struct {
	name         string
	projectID    sql.NullString
	authMode     string
	state        string
	allowedHosts []string
}

func (s *Service) loadConnection(ctx context.Context, orgID, connectionID string) (*connectionContext, error) {
	var rawHosts string
	c := &connectionContext{}
	err := s.db.QueryRowContext(ctx, `
		SELECT pc.name, pc.owner_project_id, pc.auth_mode, pc.state, pa.allowed_hosts_json
		FROM provider_connections pc
		JOIN provider_adapters pa ON pa.id = pc.adapter_id
		WHERE pc.org_id = ? AND pc.id = ?`, orgID, connectionID,
	).Scan(&c.name, &c.projectID, &c.authMode, &c.state, &rawHosts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: load connection: %w", err)
	}
	if err := json.Unmarshal([]byte(rawHosts), &c.allowedHosts); err != nil || len(c.allowedHosts) == 0 {
		return nil, fmt.Errorf("oauth: adapter host allowlist is missing or invalid")
	}
	return c, nil
}

func (s *Service) ensureOAuthSlotOnly(ctx context.Context, orgID, connectionID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM connection_secret_bindings WHERE org_id = ? AND connection_id = ? AND slot_name NOT IN ('oauth_client_secret', 'oauth_access_token')`, orgID, connectionID).Scan(&count)
	if err != nil {
		return fmt.Errorf("oauth: inspect connection credential bindings: %w", err)
	}
	if count != 0 {
		return fmt.Errorf("%w: OAuth connection cannot retain non-OAuth secret slots", ErrInvalidOAuthInput)
	}
	return nil
}

func (s *Service) startPKCE(ctx context.Context, in StartInput, conn *connectionContext, tokenURL *url.URL, scope string) (*StartResult, error) {
	authURL, err := ValidateEndpoint(in.AuthorizationEndpoint, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	defer clear(verifier)
	state, err := NewState()
	if err != nil {
		return nil, err
	}
	sessionID, err := idgen.New(idgen.PrefixOAuthSession)
	if err != nil {
		return nil, err
	}
	payload := sessionPayload{Verifier: append([]byte(nil), verifier...), TokenURL: tokenURL.String(), ClientID: in.ClientID, Scopes: scope, Resource: in.Resource, ConnectionID: in.ConnectionID}
	encoded, err := json.Marshal(payload)
	payload.zeroize()
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	nonce, ciphertext, err := s.vault.SealOAuthSessionValue(ctx, in.OrgID, sessionID, "session_payload", encoded)
	if err != nil {
		return nil, err
	}
	now := s.nowMs()
	expires := now + MaxAuthorizationTTL.Milliseconds()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO oauth_authorization_sessions (id, org_id, project_id, principal_id, target_kind, target_id, grant_type, state, sealed_code_verifier, verifier_nonce, bound_resource_uri, session_status, expires_at_ms, created_at_ms) VALUES (?, ?, ?, ?, 'PROVIDER_CONNECTION', ?, 'AUTHORIZATION_CODE_PKCE', ?, ?, ?, ?, 'PENDING', ?, ?)`, sessionID, in.OrgID, nullableString(conn.projectID), in.PrincipalID, in.ConnectionID, state, ciphertext, nonce, in.Resource, expires, now); err != nil {
		return nil, fmt.Errorf("oauth: persist authorization session: %w", err)
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", in.ClientID)
	query.Set("redirect_uri", s.redirect)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("resource", in.Resource)
	if scope != "" {
		query.Set("scope", scope)
	}
	authURL.RawQuery = query.Encode()
	return &StartResult{Flow: "authorization_code_pkce", State: state, AuthorizationURL: authURL.String(), ExpiresAt: time.UnixMilli(expires).UTC().Format(time.RFC3339)}, nil
}

func nullableString(v sql.NullString) any {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	value := v.String
	return &value
}

func (s *Service) loadClientSecret(ctx context.Context, orgID, connectionID string) ([]byte, error) {
	var secretID string
	err := s.db.QueryRowContext(ctx, `SELECT credential_id FROM connection_secret_bindings WHERE org_id = ? AND connection_id = ? AND slot_name = 'oauth_client_secret'`, orgID, connectionID).Scan(&secretID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("oauth: resolve OAuth client secret: %w", err)
	}
	secret, err := s.vault.UnsealWithCapability(ctx, s.cap, orgID, secretID)
	if err != nil {
		return nil, fmt.Errorf("oauth: unseal OAuth client secret")
	}
	defer secret.Zeroize()
	if secret.SecretKind != vault.SecretKindAPIKey || len(secret.Plaintext) == 0 {
		return nil, fmt.Errorf("oauth: OAuth client secret binding has an invalid secret kind")
	}
	return append([]byte(nil), secret.Plaintext...), nil
}

func (s *Service) formClientCredentials(ctx context.Context, orgID, connectionID, clientID, scope, resource string) (url.Values, error) {
	secret, err := s.loadClientSecret(ctx, orgID, connectionID)
	if err != nil {
		return nil, err
	}
	if len(secret) == 0 {
		return nil, fmt.Errorf("%w: confidential OAuth grant requires an oauth_client_secret binding", ErrInvalidOAuthInput)
	}
	fields := url.Values{"grant_type": {"client_credentials"}, "client_id": {clientID}, "resource": {resource}}
	if scope != "" {
		fields.Set("scope", scope)
	}
	fields.Set("client_secret", string(secret))
	clear(secret)
	return fields, nil
}

func newOpaqueID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	defer clear(b)
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) startClientCredentials(ctx context.Context, in StartInput, conn *connectionContext, tokenURL *url.URL, scope string) (*StartResult, error) {
	fields, err := s.formClientCredentials(ctx, in.OrgID, in.ConnectionID, in.ClientID, scope, in.Resource)
	if err != nil {
		return nil, err
	}
	defer zeroValues(fields)
	client, err := NewGuardedClient(tokenURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	tokens, err := RequestToken(ctx, client, tokenURL, fields)
	if err != nil {
		return nil, err
	}
	defer tokens.Zeroize()
	if tokens.RefreshToken == nil {
		// Client-credential tokens do not refresh; they are reacquired before expiry.
	}
	if err := s.persistTokens(ctx, in.OrgID, in.PrincipalID, in.ConnectionID, conn, tokenURL.String(), in.ClientID, scope, in.Resource, tokens); err != nil {
		return nil, err
	}
	now := s.nowMs()
	state, err := NewState()
	if err != nil {
		return nil, err
	}
	return &StartResult{Flow: "client_credentials", State: state, Status: "completed", ExpiresAt: time.UnixMilli(now + tokens.ExpiresIn*1000).UTC().Format(time.RFC3339)}, nil
}

type deviceAuthorizationResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Error           string `json:"error"`
}

func (s *Service) startDevice(ctx context.Context, in StartInput, conn *connectionContext, tokenURL *url.URL, scope string) (*StartResult, error) {
	deviceURL, err := ValidateEndpoint(in.DeviceAuthorizationURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	fields := url.Values{"client_id": {in.ClientID}, "resource": {in.Resource}}
	if scope != "" {
		fields.Set("scope", scope)
	}
	secret, err := s.loadClientSecret(ctx, in.OrgID, in.ConnectionID)
	if err != nil {
		return nil, err
	}
	if len(secret) > 0 {
		fields.Set("client_secret", string(secret))
		clear(secret)
	}
	defer zeroValues(fields)
	client, err := NewGuardedClient(deviceURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	body := []byte(fields.Encode())
	defer clear(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oauth: create device authorization request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: device authorization request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("oauth: read device authorization response")
	}
	defer clear(raw)
	if len(raw) > maxTokenResponseBytes || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: device authorization endpoint rejected the request")
	}
	var wire deviceAuthorizationResponse
	if err := json.Unmarshal(raw, &wire); err != nil || wire.DeviceCode == "" || wire.UserCode == "" || wire.ExpiresIn <= 0 {
		return nil, fmt.Errorf("oauth: malformed device authorization response")
	}
	verifyURL, err := ValidateEndpoint(wire.VerificationURI, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, fmt.Errorf("oauth: verification URI is outside the adapter host policy")
	}
	if wire.Interval < 1 {
		wire.Interval = 5
	}
	if wire.ExpiresIn > int64(MaxAuthorizationTTL.Seconds()) {
		wire.ExpiresIn = int64(MaxAuthorizationTTL.Seconds())
	}
	state, err := NewState()
	if err != nil {
		return nil, err
	}
	sessionID, err := idgen.New(idgen.PrefixOAuthSession)
	if err != nil {
		return nil, err
	}
	payload := sessionPayload{DeviceCode: []byte(wire.DeviceCode), TokenURL: tokenURL.String(), ClientID: in.ClientID, Scopes: scope, Resource: in.Resource, ConnectionID: in.ConnectionID}
	encoded, err := json.Marshal(payload)
	payload.zeroize()
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	nonce, ciphertext, err := s.vault.SealOAuthSessionValue(ctx, in.OrgID, sessionID, "session_payload", encoded)
	if err != nil {
		return nil, err
	}
	now := s.nowMs()
	expires := now + wire.ExpiresIn*1000
	if _, err := s.db.ExecContext(ctx, `INSERT INTO oauth_authorization_sessions (id, org_id, project_id, principal_id, target_kind, target_id, grant_type, state, sealed_code_verifier, verifier_nonce, verification_uri, poll_interval_ms, bound_resource_uri, session_status, expires_at_ms, created_at_ms) VALUES (?, ?, ?, ?, 'PROVIDER_CONNECTION', ?, 'DEVICE_CODE', ?, ?, ?, ?, ?, ?, 'PENDING', ?, ?)`, sessionID, in.OrgID, nullableString(conn.projectID), in.PrincipalID, in.ConnectionID, state, ciphertext, nonce, verifyURL.String(), wire.Interval*1000, in.Resource, expires, now); err != nil {
		return nil, fmt.Errorf("oauth: persist device authorization session")
	}
	return &StartResult{Flow: "device_code", State: state, VerificationURI: verifyURL.String(), UserCode: wire.UserCode, PollIntervalMs: wire.Interval * 1000, ExpiresAt: time.UnixMilli(expires).UTC().Format(time.RFC3339)}, nil
}

func (s *Service) Callback(ctx context.Context, orgID, principalID, state, code string) error {
	if ctx == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(principalID) == "" || strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return ErrInvalidOAuthState
	}
	row, err := s.consumeSession(ctx, orgID, principalID, state, GrantAuthorizationCodePKCE)
	if err != nil {
		return err
	}
	defer clear(row.nonce)
	defer clear(row.ciphertext)
	plain, err := s.vault.UnsealOAuthSessionValue(ctx, orgID, row.id, "session_payload", row.nonce, row.ciphertext)
	if err != nil {
		return fmt.Errorf("oauth: decrypt authorization session")
	}
	defer clear(plain)
	var payload sessionPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return fmt.Errorf("oauth: invalid encrypted authorization session")
	}
	defer payload.zeroize()
	conn, err := s.loadConnection(ctx, orgID, payload.ConnectionID)
	if err != nil {
		return err
	}
	tokenURL, err := ValidateEndpoint(payload.TokenURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return err
	}
	client, err := NewGuardedClient(tokenURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return err
	}
	fields := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.redirect},
		"client_id": {payload.ClientID}, "code_verifier": {string(payload.Verifier)}, "resource": {payload.Resource},
	}
	if payload.Scopes != "" {
		fields.Set("scope", payload.Scopes)
	}
	secret, err := s.loadClientSecret(ctx, orgID, payload.ConnectionID)
	if err != nil {
		return err
	}
	if len(secret) > 0 {
		fields.Set("client_secret", string(secret))
		clear(secret)
	}
	defer zeroValues(fields)
	tokens, err := RequestToken(ctx, client, tokenURL, fields)
	if err != nil {
		return err
	}
	defer tokens.Zeroize()
	return s.persistTokens(ctx, orgID, principalID, payload.ConnectionID, conn, tokenURL.String(), payload.ClientID, payload.Scopes, payload.Resource, tokens)
}

type sessionRow struct {
	id, connectionID  string
	nonce, ciphertext []byte
	resource          string
	pollIntervalMs    int64
}

func (s *Service) consumeSession(ctx context.Context, orgID, principalID, state, grant string) (*sessionRow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var row sessionRow
	var status string
	var expires int64
	var targetKind, targetID string
	var grantType string
	var pollInterval sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, target_kind, target_id, grant_type, sealed_code_verifier, verifier_nonce, bound_resource_uri, poll_interval_ms, session_status, expires_at_ms FROM oauth_authorization_sessions WHERE org_id = ? AND principal_id = ? AND state = ?`, orgID, principalID, state).Scan(&row.id, &targetKind, &targetID, &grantType, &row.ciphertext, &row.nonce, &row.resource, &pollInterval, &status, &expires)
	if err != nil || targetKind != "PROVIDER_CONNECTION" || grantType != grant || status != "PENDING" || s.nowMs() >= expires {
		return nil, ErrInvalidOAuthState
	}
	if grant == GrantAuthorizationCodePKCE {
		row.connectionID = targetID
	}
	if pollInterval.Valid {
		row.pollIntervalMs = pollInterval.Int64
	}
	res, err := tx.ExecContext(ctx, `UPDATE oauth_authorization_sessions SET session_status = 'CONSUMED' WHERE org_id = ? AND id = ? AND session_status = 'PENDING' AND expires_at_ms > ?`, orgID, row.id, s.nowMs())
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return nil, ErrInvalidOAuthState
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &row, nil
}

func zeroValues(v url.Values) {
	for key, values := range v {
		for i := range values {
			values[i] = ""
		}
		delete(v, key)
	}
}

// PollDevice exchanges one RFC 8628 device code. Session ownership is checked on every poll,
// polling cadence is enforced by the database, and the device code itself exists only sealed.
func (s *Service) PollDevice(ctx context.Context, in PollInput) (*StartResult, error) {
	if ctx == nil || strings.TrimSpace(in.OrgID) == "" || strings.TrimSpace(in.PrincipalID) == "" || strings.TrimSpace(in.State) == "" {
		return nil, ErrInvalidOAuthState
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var row sessionRow
	var status string
	var expires int64
	var lastPoll sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, target_id, sealed_code_verifier, verifier_nonce, bound_resource_uri, poll_interval_ms, last_poll_at_ms, session_status, expires_at_ms FROM oauth_authorization_sessions WHERE org_id = ? AND principal_id = ? AND state = ? AND grant_type = 'DEVICE_CODE'`, in.OrgID, in.PrincipalID, in.State).Scan(&row.id, &row.connectionID, &row.ciphertext, &row.nonce, &row.resource, &row.pollIntervalMs, &lastPoll, &status, &expires)
	if err != nil || status != "PENDING" || s.nowMs() >= expires {
		return nil, ErrInvalidOAuthState
	}
	now := s.nowMs()
	if lastPoll.Valid && now-lastPoll.Int64 < row.pollIntervalMs {
		return nil, ErrSlowDown
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_authorization_sessions SET last_poll_at_ms = ? WHERE org_id = ? AND id = ? AND session_status = 'PENDING'`, now, in.OrgID, row.id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	defer clear(row.nonce)
	defer clear(row.ciphertext)
	plain, err := s.vault.UnsealOAuthSessionValue(ctx, in.OrgID, row.id, "session_payload", row.nonce, row.ciphertext)
	if err != nil {
		return nil, fmt.Errorf("oauth: decrypt device session")
	}
	defer clear(plain)
	var payload sessionPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, fmt.Errorf("oauth: invalid device session")
	}
	defer payload.zeroize()
	conn, err := s.loadConnection(ctx, in.OrgID, payload.ConnectionID)
	if err != nil {
		return nil, err
	}
	tokenURL, err := ValidateEndpoint(payload.TokenURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	client, err := NewGuardedClient(tokenURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	fields := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {string(payload.DeviceCode)}, "client_id": {payload.ClientID}, "resource": {payload.Resource}}
	if payload.Scopes != "" {
		fields.Set("scope", payload.Scopes)
	}
	secret, err := s.loadClientSecret(ctx, in.OrgID, payload.ConnectionID)
	if err != nil {
		return nil, err
	}
	if len(secret) > 0 {
		fields.Set("client_secret", string(secret))
		clear(secret)
	}
	defer zeroValues(fields)
	tokens, err := RequestToken(ctx, client, tokenURL, fields)
	if errors.Is(err, ErrAuthorizationPending) {
		return &StartResult{Flow: "device_code", State: in.State, Status: "authorization_pending", PollIntervalMs: row.pollIntervalMs}, ErrAuthorizationPending
	}
	if errors.Is(err, ErrSlowDown) {
		_, _ = s.db.ExecContext(ctx, `UPDATE oauth_authorization_sessions SET poll_interval_ms = poll_interval_ms + 5000 WHERE org_id = ? AND id = ? AND session_status = 'PENDING'`, in.OrgID, row.id)
		return nil, ErrSlowDown
	}
	if err != nil {
		return nil, err
	}
	defer tokens.Zeroize()
	if _, err := s.db.ExecContext(ctx, `UPDATE oauth_authorization_sessions SET session_status = 'COMPLETED' WHERE org_id = ? AND id = ? AND session_status = 'PENDING'`, in.OrgID, row.id); err != nil {
		return nil, err
	}
	if err := s.persistTokens(ctx, in.OrgID, in.PrincipalID, payload.ConnectionID, conn, tokenURL.String(), payload.ClientID, payload.Scopes, payload.Resource, tokens); err != nil {
		return nil, err
	}
	return &StartResult{Flow: "device_code", State: in.State, Status: "completed"}, nil
}

func (s *Service) persistTokens(ctx context.Context, orgID, principalID, connectionID string, conn *connectionContext, tokenURL, clientID, scope, resource string, tokens *TokenResponse) error {
	serialized, err := tokenSetJSON(tokens, tokenURL, clientID, connectionID, scope, resource, time.UnixMilli(s.nowMs()))
	if err != nil {
		return err
	}
	defer clear(serialized)
	var existingID string
	err = s.db.QueryRowContext(ctx, `SELECT credential_id FROM connection_secret_bindings WHERE org_id = ? AND connection_id = ? AND slot_name = 'oauth_access_token'`, orgID, connectionID).Scan(&existingID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existingID != "" {
		meta, err := s.vault.GetSecretMetadata(ctx, orgID, existingID)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		_, activate, err := s.vault.RotateSecretTx(ctx, tx, orgID, existingID, meta.Version, vault.RotateSecretParams{Plaintext: serialized, BoundAudience: &resource})
		if err != nil {
			return err
		}
		now := s.nowMs()
		if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_token_states (org_id, credential_id, bound_principal_id, state, granted_scopes, bound_resource_uri, access_expires_at_ms, updated_at_ms) VALUES (?, ?, ?, 'TOKEN_VALID', ?, ?, ?, ?) ON CONFLICT(org_id, credential_id) DO UPDATE SET bound_principal_id=excluded.bound_principal_id, state='TOKEN_VALID', granted_scopes=excluded.granted_scopes, bound_resource_uri=excluded.bound_resource_uri, access_expires_at_ms=excluded.access_expires_at_ms, last_refresh_error=NULL, version=oauth_token_states.version+1, updated_at_ms=excluded.updated_at_ms`, orgID, existingID, principalID, scope, resource, now+tokens.ExpiresIn*1000, now); err != nil {
			return err
		}
		if err := s.audit.AppendTx(ctx, tx, port.AuditEvent{CreatedAtMs: now, OrgID: &orgID, ActorKind: "PRINCIPAL", ActorID: principalID, EntryPoint: port.AuditEntryPointAdminAPI, EventType: "oauth.authorization_completed", TargetType: "provider_connection", TargetID: connectionID, MetadataJSON: `{"grant_type":"authorized_oauth","resource_bound":true}`}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		activate()
		return nil
	}
	credentialID, err := idgen.New(idgen.PrefixSecret)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	meta, commitCache, err := s.vault.SealSecretTx(ctx, tx, vault.SealSecretParams{ID: credentialID, OrgID: orgID, OwnerProjectID: nullStringPtr(conn.projectID), Name: "OAuth token set: " + conn.name, SecretKind: vault.SecretKindOAuth2TokenSet, BoundAudience: &resource, Plaintext: serialized})
	if err != nil {
		return err
	}
	now := s.nowMs()
	if _, err := tx.ExecContext(ctx, `INSERT INTO connection_secret_bindings (org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms) VALUES (?, ?, 'oauth_access_token', ?, 'HOST_BLIND_INJECT', ?)`, orgID, connectionID, meta.ID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_token_states (org_id, credential_id, bound_principal_id, state, granted_scopes, bound_resource_uri, access_expires_at_ms, updated_at_ms) VALUES (?, ?, ?, 'TOKEN_VALID', ?, ?, ?, ?)`, orgID, meta.ID, principalID, scope, resource, now+tokens.ExpiresIn*1000, now); err != nil {
		return err
	}
	if err := s.audit.AppendTx(ctx, tx, port.AuditEvent{CreatedAtMs: now, OrgID: &orgID, ActorKind: "PRINCIPAL", ActorID: principalID, EntryPoint: port.AuditEntryPointAdminAPI, EventType: "oauth.authorization_completed", TargetType: "provider_connection", TargetID: connectionID, MetadataJSON: `{"grant_type":"authorized_oauth","resource_bound":true}`}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	commitCache()
	return nil
}

// AccessToken returns a copy of a target-bound bearer token and serializes refreshes by
// credential id. The caller must clear the returned byte slice after injection.
func (s *Service) AccessToken(ctx context.Context, orgID, credentialID string) ([]byte, error) {
	if ctx == nil || strings.TrimSpace(orgID) == "" || strings.TrimSpace(credentialID) == "" {
		return nil, ErrInvalidOAuthInput
	}
	v, err, _ := s.refreshes.Do(orgID+"/"+credentialID, func() (any, error) {
		return s.resolveAccessToken(ctx, orgID, credentialID)
	})
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), v.([]byte)...), nil
}

func (s *Service) resolveAccessToken(ctx context.Context, orgID, credentialID string) ([]byte, error) {
	secret, err := s.vault.UnsealWithCapability(ctx, s.cap, orgID, credentialID)
	if err != nil {
		return nil, fmt.Errorf("oauth: token credential unavailable")
	}
	defer secret.Zeroize()
	if secret.SecretKind != vault.SecretKindOAuth2TokenSet {
		return nil, fmt.Errorf("oauth: credential is not an OAuth token set")
	}
	tokens, err := DecodeTokenSet(secret.Plaintext)
	if err != nil {
		return nil, err
	}
	defer tokens.Zeroize()
	var bound string
	err = s.db.QueryRowContext(ctx, `SELECT bound_resource_uri FROM oauth_token_states WHERE org_id = ? AND credential_id = ? AND state IN ('TOKEN_VALID','REFRESH_GRACE_DEGRADED')`, orgID, credentialID).Scan(&bound)
	if err != nil || bound == "" || bound != tokens.Resource || secret.BoundAudience == nil || *secret.BoundAudience != bound {
		return nil, ErrTokenAudience
	}
	if s.nowMs() < tokens.ExpiresAtMs-RefreshSkew.Milliseconds() {
		return append([]byte(nil), tokens.AccessToken...), nil
	}
	connID := tokens.ConnectionID
	conn, err := s.loadConnection(ctx, orgID, connID)
	if err != nil {
		return nil, err
	}
	endpoint, err := ValidateEndpoint(tokens.TokenURL, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	client, err := NewGuardedClient(endpoint, conn.allowedHosts, s.resolver, s.allowHTTP)
	if err != nil {
		return nil, err
	}
	var fields url.Values
	if len(tokens.RefreshToken) == 0 {
		fields, err = s.formClientCredentials(ctx, orgID, connID, tokens.ClientID, tokens.Scope, bound)
	} else {
		fields = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {string(tokens.RefreshToken)}, "client_id": {tokens.ClientID}, "resource": {bound}}
		secretBytes, secretErr := s.loadClientSecret(ctx, orgID, connID)
		if secretErr != nil {
			return nil, secretErr
		}
		if len(secretBytes) > 0 {
			fields.Set("client_secret", string(secretBytes))
			clear(secretBytes)
		}
	}
	if err != nil {
		return nil, err
	}
	defer zeroValues(fields)
	refreshStarted := s.nowMs()
	if _, err := s.db.ExecContext(ctx, `UPDATE oauth_token_states SET state='REFRESHING_SINGLEFLIGHT', version=version+1, updated_at_ms=? WHERE org_id=? AND credential_id=? AND state IN ('TOKEN_VALID','REFRESH_GRACE_DEGRADED')`, refreshStarted, orgID, credentialID); err != nil {
		return nil, err
	}
	refreshFinished := false
	defer func() {
		if !refreshFinished {
			_, _ = s.db.ExecContext(context.Background(), `UPDATE oauth_token_states SET state='REFRESH_GRACE_DEGRADED', last_refresh_error='transient_refresh_failure', version=version+1, updated_at_ms=? WHERE org_id=? AND credential_id=? AND state='REFRESHING_SINGLEFLIGHT'`, s.nowMs(), orgID, credentialID)
		}
	}()
	refreshed, err := RequestToken(ctx, client, endpoint, fields)
	if errors.Is(err, ErrInvalidGrant) {
		now := s.nowMs()
		_, _ = s.db.ExecContext(ctx, `UPDATE oauth_token_states SET state='AUTH_EXPIRED_REAUTH_REQUIRED', last_refresh_error='invalid_grant', version=version+1, updated_at_ms=? WHERE org_id=? AND credential_id=?`, now, orgID, credentialID)
		refreshFinished = true
		orgCopy := orgID
		if auditErr := s.audit.Append(ctx, port.AuditEvent{CreatedAtMs: now, OrgID: &orgCopy, ActorKind: "SYSTEM", ActorID: "oauth-refresh", EntryPoint: port.AuditEntryPointSystemReaper, EventType: "ALT-OAUTH-EXPIRY", TargetType: "secret_credential", TargetID: credentialID, MetadataJSON: `{"reason":"invalid_grant","retry_count":0}`}); auditErr != nil {
			return nil, fmt.Errorf("oauth: persist reauthorization alert: %w", auditErr)
		}
		return nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, err
	}
	defer refreshed.Zeroize()
	if len(refreshed.RefreshToken) == 0 {
		refreshed.RefreshToken = append([]byte(nil), tokens.RefreshToken...)
	}
	encoded, err := tokenSetJSON(refreshed, tokens.TokenURL, tokens.ClientID, connID, refreshed.Scope, bound, time.UnixMilli(s.nowMs()))
	if err != nil {
		return nil, err
	}
	defer clear(encoded)
	meta, err := s.vault.GetSecretMetadata(ctx, orgID, credentialID)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	_, activate, err := s.vault.RotateSecretTx(ctx, tx, orgID, credentialID, meta.Version, vault.RotateSecretParams{Plaintext: encoded, BoundAudience: &bound})
	if err != nil {
		return nil, err
	}
	now := s.nowMs()
	if _, err = tx.ExecContext(ctx, `UPDATE oauth_token_states SET state='TOKEN_VALID', access_expires_at_ms=?, last_refresh_error=NULL, version=version+1, updated_at_ms=? WHERE org_id=? AND credential_id=?`, now+refreshed.ExpiresIn*1000, now, orgID, credentialID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	activate()
	refreshFinished = true
	return append([]byte(nil), refreshed.AccessToken...), nil
}
