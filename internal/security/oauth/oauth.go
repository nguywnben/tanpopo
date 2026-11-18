// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package oauth implements the Host-managed OAuth 2.1 coordinator. It never returns
// upstream refresh tokens to adapters and creates every outbound client through SSRFGuardDialer.
package oauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
)

const (
	GrantAuthorizationCodePKCE = "AUTHORIZATION_CODE_PKCE"
	GrantDeviceCode            = "DEVICE_CODE"
	GrantClientCredentials     = "CLIENT_CREDENTIALS"
	MaxAuthorizationTTL        = 10 * time.Minute
	RefreshSkew                = time.Minute
	maxTokenResponseBytes      = 64 << 10
)

var (
	ErrInvalidOAuthInput    = errors.New("oauth: invalid input")
	ErrInvalidOAuthState    = errors.New("oauth: invalid or expired authorization state")
	ErrInvalidGrant         = errors.New("oauth: upstream grant is invalid")
	ErrAuthorizationPending = errors.New("oauth: device authorization is pending")
	ErrSlowDown             = errors.New("oauth: device authorization polling is too frequent")
	ErrTokenAudience        = errors.New("oauth: token audience mismatch")
)

type TokenResponse struct {
	AccessToken  []byte
	RefreshToken []byte
	TokenType    string
	Scope        string
	ExpiresIn    int64
}

func (t *TokenResponse) Zeroize() {
	if t == nil {
		return
	}
	clear(t.AccessToken)
	clear(t.RefreshToken)
	t.AccessToken = nil
	t.RefreshToken = nil
}

type tokenWire struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

func NewPKCE() (verifier []byte, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return nil, "", fmt.Errorf("oauth: generate PKCE verifier: %w", err)
	}
	verifier = []byte(base64.RawURLEncoding.EncodeToString(buf))
	clear(buf)
	sum := sha256.Sum256(verifier)
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func NewState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oauth: generate state: %w", err)
	}
	defer clear(buf)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func ValidateEndpoint(raw string, allowedHosts []string, resolver egress.HostResolver, allowLoopbackHTTP bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("%w: malformed OAuth endpoint", ErrInvalidOAuthInput)
	}
	if u.Scheme != "https" {
		if !allowLoopbackHTTP || u.Scheme != "http" || !isLoopbackHost(u.Hostname()) {
			return nil, fmt.Errorf("%w: OAuth endpoints require HTTPS", ErrInvalidOAuthInput)
		}
	}
	policy := egress.Policy{AllowedHosts: append([]string(nil), allowedHosts...), RequireTLS: u.Scheme == "https", AllowPrivateIP: allowLoopbackHTTP}
	if allowLoopbackHTTP && u.Scheme == "http" {
		policy.AllowedCIDRs = []string{"127.0.0.0/8", "::1/128"}
	}
	if _, err := policy.ValidateTargetURL(u.String()); err != nil {
		return nil, fmt.Errorf("%w: endpoint is outside the adapter host policy", ErrInvalidOAuthInput)
	}
	return u, nil
}

func NewGuardedClient(endpoint *url.URL, allowedHosts []string, resolver egress.HostResolver, allowLoopbackHTTP bool) (*http.Client, error) {
	if endpoint == nil {
		return nil, fmt.Errorf("%w: endpoint is required", ErrInvalidOAuthInput)
	}
	policy := egress.Policy{AllowedHosts: append([]string(nil), allowedHosts...), RequireTLS: endpoint.Scheme == "https", AllowPrivateIP: allowLoopbackHTTP}
	if allowLoopbackHTTP && endpoint.Scheme == "http" {
		policy.AllowedCIDRs = []string{"127.0.0.0/8", "::1/128"}
	}
	return egress.NewGuardedHTTPClient(egress.ClientConfig{
		Policy: policy, Timeout: 20 * time.Second, MaxRedirects: -1,
		DenyCrossOriginRedirects: true, Resolver: resolver,
	})
}

func RequestToken(ctx context.Context, client *http.Client, endpoint *url.URL, fields url.Values) (*TokenResponse, error) {
	if ctx == nil || client == nil || endpoint == nil || fields == nil || fields.Get("grant_type") == "" {
		return nil, fmt.Errorf("%w: token request is incomplete", ErrInvalidOAuthInput)
	}
	body := []byte(fields.Encode())
	defer clear(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oauth: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: token endpoint request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("oauth: read token response")
	}
	defer clear(raw)
	if len(raw) > maxTokenResponseBytes {
		return nil, fmt.Errorf("oauth: token response exceeds size limit")
	}
	var wire tokenWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("oauth: malformed token response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch wire.Error {
		case "invalid_grant":
			return nil, ErrInvalidGrant
		case "authorization_pending":
			return nil, ErrAuthorizationPending
		case "slow_down":
			return nil, ErrSlowDown
		}
		return nil, fmt.Errorf("oauth: token endpoint returned HTTP %d", resp.StatusCode)
	}
	if wire.Error != "" || wire.AccessToken == "" || wire.ExpiresIn < 0 {
		return nil, fmt.Errorf("oauth: token response is missing required fields")
	}
	return &TokenResponse{
		AccessToken: []byte(wire.AccessToken), RefreshToken: []byte(wire.RefreshToken),
		TokenType: wire.TokenType, Scope: wire.Scope, ExpiresIn: wire.ExpiresIn,
	}, nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || strings.HasPrefix(host, "127.") || host == "::1" || host == "[::1]"
}

// TokenSet is serialized only inside the envelope-encrypted Vault credential.
type TokenSet struct {
	AccessToken  []byte `json:"access_token"`
	RefreshToken []byte `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	TokenURL     string `json:"token_url"`
	ClientID     string `json:"client_id"`
	ConnectionID string `json:"connection_id"`
	Scope        string `json:"scope,omitempty"`
	Resource     string `json:"resource,omitempty"`
	ExpiresAtMs  int64  `json:"expires_at_ms"`
}

func (t *TokenSet) Zeroize() {
	if t == nil {
		return
	}
	clear(t.AccessToken)
	clear(t.RefreshToken)
	t.AccessToken = nil
	t.RefreshToken = nil
}

func tokenSetJSON(tokens *TokenResponse, tokenURL, clientID, connectionID, scope, resource string, now time.Time) ([]byte, error) {
	if tokens == nil || len(tokens.AccessToken) == 0 || tokens.ExpiresIn <= 0 {
		return nil, fmt.Errorf("%w: access token and positive expiry are required", ErrInvalidOAuthInput)
	}
	set := TokenSet{
		AccessToken: append([]byte(nil), tokens.AccessToken...), RefreshToken: append([]byte(nil), tokens.RefreshToken...),
		TokenType: tokens.TokenType, TokenURL: tokenURL, ClientID: clientID, ConnectionID: connectionID, Scope: scope,
		Resource: resource, ExpiresAtMs: now.Add(time.Duration(tokens.ExpiresIn) * time.Second).UnixMilli(),
	}
	defer set.Zeroize()
	return json.Marshal(set)
}

func DecodeTokenSet(raw []byte) (*TokenSet, error) {
	var set TokenSet
	if err := json.Unmarshal(raw, &set); err != nil || len(set.AccessToken) == 0 || set.TokenURL == "" || set.ClientID == "" {
		set.Zeroize()
		return nil, fmt.Errorf("oauth: invalid encrypted token-set payload")
	}
	return &set, nil
}
