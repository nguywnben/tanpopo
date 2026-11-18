// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
)

func TestOAuthBrowserSessionHashRequiresCookieAndRejectsBearer(t *testing.T) {
	const session = "opaque-browser-session-value"
	request := httptest.NewRequest("GET", "/admin/v1/oauth/callback", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: session})
	request.Header.Set("Authorization", "Bearer service-token")
	response := httptest.NewRecorder()
	if _, ok := oauthBrowserSessionHash(response, request); ok || response.Code != 401 {
		t.Fatalf("bearer-authenticated browser handshake accepted: ok=%v status=%d", ok, response.Code)
	}

	request = httptest.NewRequest("GET", "/admin/v1/oauth/callback", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: session})
	response = httptest.NewRecorder()
	hash, ok := oauthBrowserSessionHash(response, request)
	sum := sha256.Sum256([]byte(session))
	if !ok || response.Code != 200 || hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("cookie session hash mismatch: ok=%v status=%d hash=%q", ok, response.Code, hash)
	}
}
