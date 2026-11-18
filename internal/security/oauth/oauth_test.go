// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/egress"
)

func TestNewPKCEUsesS256AndValidVerifier(t *testing.T) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(verifier)
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("verifier length=%d", len(verifier))
	}
	sum := sha256.Sum256(verifier)
	if challenge != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatal("challenge is not S256(verifier)")
	}
}

func TestValidateEndpointFailsClosedAndAllowsHermeticLoopback(t *testing.T) {
	resolver := egress.NewStdHostResolver()
	for _, raw := range []string{"http://oauth.example.com/token", "https://user@oauth.example.com/token", "https://evil.example/token", "https://oauth.example.com/token#fragment"} {
		if _, err := ValidateEndpoint(raw, []string{"oauth.example.com"}, resolver, false); err == nil {
			t.Fatalf("ValidateEndpoint(%q) unexpectedly succeeded", raw)
		}
	}
	if _, err := ValidateEndpoint("http://127.0.0.1/token", []string{"127.0.0.1"}, resolver, true); err != nil {
		t.Fatalf("loopback endpoint rejected: %v", err)
	}
	if _, err := ValidateEndpoint("http://127.0.0.1/token", []string{"127.0.0.1"}, resolver, false); err == nil {
		t.Fatal("non-TLS loopback endpoint accepted without hermetic allowance")
	}
}

func TestRequestTokenReturnsOnlySanitizedErrorsAndZeroizableTokens(t *testing.T) {
	secretValue := "rotating-secret-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("refresh_token") != secretValue {
			t.Error("refresh token missing from form body")
		}
		if r.URL.RawQuery != "" {
			t.Error("token request unexpectedly placed secrets in URL")
		}
		if r.URL.Path == "/invalid" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"` + secretValue + `"}`))
			return
		}
		if r.URL.Path == "/missing-type" {
			_, _ = w.Write([]byte(`{"access_token":"access-value","expires_in":3600}`))
			return
		}
		if r.URL.Path == "/unsupported-type" {
			_, _ = w.Write([]byte(`{"access_token":"access-value","token_type":"MAC","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"access-value","refresh_token":"next-refresh","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL + "/token")
	fields := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {secretValue}}
	tokens, err := RequestToken(context.Background(), server.Client(), endpoint, fields)
	if err != nil {
		t.Fatal(err)
	}
	if string(tokens.AccessToken) != "access-value" || string(tokens.RefreshToken) != "next-refresh" {
		t.Fatalf("unexpected token response: %+v", tokens)
	}
	tokens.Zeroize()
	if len(tokens.AccessToken) != 0 || len(tokens.RefreshToken) != 0 {
		t.Fatal("Zeroize retained token bytes")
	}
	bad, _ := url.Parse(server.URL + "/invalid")
	_, err = RequestToken(context.Background(), server.Client(), bad, fields)
	if !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expected ErrInvalidGrant, got %v", err)
	}
	if err.Error() == secretValue {
		t.Fatal("secret leaked through error")
	}
	for _, path := range []string{"/missing-type", "/unsupported-type"} {
		invalidEndpoint, _ := url.Parse(server.URL + path)
		if tokens, err := RequestToken(context.Background(), server.Client(), invalidEndpoint, fields); err == nil {
			if tokens != nil {
				tokens.Zeroize()
			}
			t.Fatalf("accepted non-Bearer token response at %s", path)
		}
	}
}

func TestTokenSetKeepsResourceAndConnectionBinding(t *testing.T) {
	resp := &TokenResponse{AccessToken: []byte("a"), RefreshToken: []byte("r"), TokenType: "Bearer", ExpiresIn: 60}
	raw, err := tokenSetJSON(resp, "https://idp.example/token", "client", "con_1", "read", "https://api.example/", GrantAuthorizationCodePKCE, time.UnixMilli(1000))
	if err != nil {
		t.Fatal(err)
	}
	set, err := DecodeTokenSet(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Zeroize()
	if set.ConnectionID != "con_1" || set.Resource != "https://api.example/" || string(set.AccessToken) != "a" {
		t.Fatalf("unexpected token binding: %+v", set)
	}
	clear(raw)
}
