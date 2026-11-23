// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuiltinOnboardingCleanRuntime(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	t.Setenv("TANPOPO_TEST_HERMETIC", "1")
	t.Setenv("TANPOPO_OFFLINE_MODE", "true")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"local-qwen","object":"model"}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl-test-1","object":"chat.completion","created":1700000000,"model":"local-qwen","choices":[{"index":0,"message":{"role":"assistant","content":"hello from adapter"},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	handler, closeRuntime, err := buildRuntime(context.Background(), "all", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	defer func() {
		if cerr := closeRuntime(); cerr != nil {
			t.Fatalf("closeRuntime: %v", cerr)
		}
	}()

	srv := httptest.NewServer(handler)
	defer srv.Close()
	client := srv.Client()

	// 1. Clean DB: /bootstrap/status reports initialized=false, 8 marketplace items, 0 active adapters.
	statusResp := mustDoJSON(t, client, http.MethodGet, srv.URL+"/admin/v1/bootstrap/status", "", "", nil)
	var status struct {
		Initialized             bool  `json:"initialized"`
		MarketplaceCatalogCount int   `json:"marketplace_catalog_count"`
		AvailableAdapters       []any `json:"available_adapters"`
	}
	mustDecodeBody(t, statusResp.Body, &status)
	if status.Initialized {
		t.Fatalf("expected uninitialized clean database")
	}
	if status.MarketplaceCatalogCount != 8 {
		t.Fatalf("expected 8 built-in marketplace packages, got %d", status.MarketplaceCatalogCount)
	}
	if len(status.AvailableAdapters) != 0 {
		t.Fatalf("expected 0 available adapters before activation, got %d", len(status.AvailableAdapters))
	}

	// 2. Default bootstrap token exists at <dataDir>/bootstrap.token.
	tokBytes, err := os.ReadFile(filepath.Join(dataDir, "bootstrap.token"))
	if err != nil {
		t.Fatalf("read default bootstrap.token: %v", err)
	}
	bootToken := strings.TrimSpace(string(tokBytes))
	if bootToken == "" {
		t.Fatalf("expected non-empty bootstrap.token")
	}

	// 3. Attempting bootstrap/complete with an unavailable adapter fails closed (unknown_provider_adapter).
	badBootResp := mustDoJSONExpect(t, client, http.MethodPost, srv.URL+"/admin/v1/bootstrap/complete", "", "", map[string]any{
		"bootstrap_token":                bootToken,
		"owner_email":                    "owner@example.com",
		"owner_password":                 "StrongPassword!2026",
		"master_kek_backup_acknowledged": true,
		"initial_connection": map[string]any{
			"adapter_id":   "openai-compatible",
			"name":         "invalid",
			"billing_mode": "SELF_HOSTED_INFRA",
			"endpoint_url": upstream.URL + "/v1",
			"model_id":     "local-qwen",
			"data_region":  "GLOBAL",
		},
	}, http.StatusUnprocessableEntity)
	_ = badBootResp

	// 4. Complete Setup cleanly without initial_connection, capturing session cookie + CSRF token.
	bootResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/bootstrap/complete", "", "", map[string]any{
		"bootstrap_token":                bootToken,
		"owner_email":                    "owner@example.com",
		"owner_password":                 "StrongPassword!2026",
		"org_name":                       "Acme Local",
		"project_name":                   "default",
		"master_kek_backup_acknowledged": true,
	})
	var bootBody struct {
		OrgID     string `json:"org_id"`
		ProjectID string `json:"project_id"`
		EnvID     string `json:"env_id"`
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeBody(t, bootResp.Body, &bootBody)
	cookie := extractSessionCookie(t, bootResp.Cookies())

	// 5. Inspect built-in marketplace catalog, find @tanpopo/openai-compatible, stage & activate.
	catResp := mustDoJSON(t, client, http.MethodGet, srv.URL+"/admin/v1/marketplace/catalog", cookie, "", nil)
	var catBody struct {
		Data []struct {
			PackageID      string `json:"package_id"`
			OriginTrust    string `json:"origin_trust_tier"`
			SourceURI      string `json:"source_uri"`
			ExpectedSHA256 string `json:"expected_sha256"`
		} `json:"data"`
	}
	mustDecodeBody(t, catResp.Body, &catBody)
	if len(catBody.Data) != 8 {
		t.Fatalf("expected 8 marketplace entries in clean binary, got %d", len(catBody.Data))
	}
	badBuiltinResp := mustDoJSONExpect(t, client, http.MethodPost, srv.URL+"/admin/v1/extensions/install", cookie, bootBody.CSRFToken, map[string]any{
		"source_uri":      "tanpopo-builtin://does-not-exist.tpkg",
		"expected_sha256": strings.Repeat("a", 64),
	}, http.StatusNotFound)
	_ = badBuiltinResp
	var targetURI, targetSHA string
	for _, entry := range catBody.Data {
		if entry.PackageID == "@tanpopo/openai-compatible" {
			targetURI = entry.SourceURI
			targetSHA = entry.ExpectedSHA256
			if entry.OriginTrust != "official" {
				t.Fatalf("expected official trust tier for built-in adapter, got %s", entry.OriginTrust)
			}
		}
	}
	if targetURI == "" {
		t.Fatalf("@tanpopo/openai-compatible missing from marketplace catalog")
	}

	stageResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/extensions/install", cookie, bootBody.CSRFToken, map[string]any{
		"source_uri":      targetURI,
		"expected_sha256": targetSHA,
	})
	stageETag := stageResp.Header.Get("ETag")
	var stageDiff struct {
		InstallationID  string `json:"installation_id"`
		OriginTrustTier string `json:"origin_trust_tier"`
	}
	mustDecodeBody(t, stageResp.Body, &stageDiff)
	if stageDiff.OriginTrustTier != "official" {
		t.Fatalf("expected official origin trust tier after signature verification, got %s", stageDiff.OriginTrustTier)
	}

	approveResp := mustDoJSONWithETag(t, client, http.MethodPost, srv.URL+"/admin/v1/extensions/"+stageDiff.InstallationID+"/approve-permissions", cookie, bootBody.CSRFToken, stageETag, map[string]any{
		"enable_immediately": true,
	})
	var approved struct {
		State string `json:"state"`
	}
	mustDecodeBody(t, approveResp.Body, &approved)
	if approved.State != "ENABLED_ACTIVE" {
		t.Fatalf("expected ENABLED_ACTIVE, got %s", approved.State)
	}

	// 6. Newly activated adapter is immediately registered in Providers -> Adapters!
	adpResp := mustDoJSON(t, client, http.MethodGet, srv.URL+"/admin/v1/provider-adapters", cookie, "", nil)
	var adpBody struct {
		Data []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	mustDecodeBody(t, adpResp.Body, &adpBody)
	var adapterID string
	for _, item := range adpBody.Data {
		if item.Slug == "openai-compatible" {
			adapterID = item.ID
		}
	}
	if adapterID == "" {
		t.Fatalf("expected openai-compatible to appear in GET /admin/v1/provider-adapters after activation")
	}

	// 7. Create Provider Connection -> Logical Model -> Deployment -> Route -> Virtual Key.
	connResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/connections", cookie, bootBody.CSRFToken, map[string]any{
		"adapter_id":       adapterID,
		"name":             "Local Upstream",
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     upstream.URL,
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
		"data_region":      "ON_PREM",
	})
	connETag := connResp.Header.Get("ETag")
	var conn struct {
		ID string `json:"id"`
	}
	mustDecodeBody(t, connResp.Body, &conn)

	testConnResp := mustDoJSONWithETag(t, client, http.MethodPost, srv.URL+"/admin/v1/connections/"+conn.ID+"/test", cookie, bootBody.CSRFToken, connETag, map[string]any{})
	var testConn struct {
		Valid            bool    `json:"valid"`
		FailureCode      *string `json:"failure_code"`
		SanitizedMessage *string `json:"sanitized_message"`
	}
	mustDecodeBody(t, testConnResp.Body, &testConn)
	if !testConn.Valid {
		code, msg := "", ""
		if testConn.FailureCode != nil {
			code = *testConn.FailureCode
		}
		if testConn.SanitizedMessage != nil {
			msg = *testConn.SanitizedMessage
		}
		t.Fatalf("expected connection test probe to return valid=true (code=%q msg=%q)", code, msg)
	}

	modResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/models", cookie, bootBody.CSRFToken, map[string]any{
		"slug":              "local-qwen",
		"display_name":      "Local Qwen",
		"modality_kind":     "CHAT",
		"context_window":    32768,
		"max_output_tokens": 4096,
		"assurance_tier":    "TESTED",
		"capabilities":      map[string]any{"streaming": true},
	})
	var model struct {
		ID string `json:"id"`
	}
	mustDecodeBody(t, modResp.Body, &model)

	act := true
	depResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/deployments", cookie, bootBody.CSRFToken, map[string]any{
		"env_id":               bootBody.EnvID,
		"connection_id":        conn.ID,
		"logical_model_id":     model.ID,
		"upstream_model_name":  "local-qwen",
		"data_region":          "ON_PREM",
		"activate_immediately": &act,
		"rate_card": map[string]any{
			"input_token_nano_usd":  100,
			"output_token_nano_usd": 200,
		},
	})
	var dep struct {
		ID string `json:"id"`
	}
	mustDecodeBody(t, depResp.Body, &dep)

	mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/routes", cookie, bootBody.CSRFToken, map[string]any{
		"slug":                 "local-qwen",
		"env_id":               bootBody.EnvID,
		"strategy":             "WEIGHTED_ROUND_ROBIN",
		"allowed_data_regions": []string{"ON_PREM"},
		"targets": []map[string]any{
			{"deployment_id": dep.ID, "weight": 100, "priority_order": 1},
		},
	})

	vkResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/admin/v1/virtual-keys", cookie, bootBody.CSRFToken, map[string]any{
		"project_id": bootBody.ProjectID,
		"env_id":     bootBody.EnvID,
		"name":       "e2e-test-key",
	})
	var vk struct {
		SecretKey string `json:"secret_key"`
	}
	mustDecodeBody(t, vkResp.Body, &vk)
	if !strings.HasPrefix(vk.SecretKey, "vk_") {
		t.Fatalf("unexpected virtual key token %q", vk.SecretKey)
	}

	// 8. Execute OpenAI inference call -> 200 OK -> visible in Traces!
	chatResp := mustDoJSON(t, client, http.MethodPost, srv.URL+"/v1/chat/completions", "", "", map[string]any{
		"model":    "local-qwen",
		"messages": []map[string]any{{"role": "user", "content": "ping"}},
	}, "Authorization", "Bearer "+vk.SecretKey)
	var chatBody struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	mustDecodeBody(t, chatResp.Body, &chatBody)
	if len(chatBody.Choices) == 0 || chatBody.Choices[0].Message.Content != "hello from adapter" {
		t.Fatalf("unexpected chat response: %+v", chatBody)
	}

	var tracesBody struct {
		Data []struct {
			StatusCode int    `json:"status_code"`
			RouteSlug  string `json:"route_slug"`
		} `json:"data"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		tracesResp := mustDoJSON(t, client, http.MethodGet, srv.URL+"/admin/v1/traces?limit=20", cookie, "", nil)
		tracesBody.Data = nil
		mustDecodeBody(t, tracesResp.Body, &tracesBody)
		if len(tracesBody.Data) > 0 && tracesBody.Data[0].StatusCode == http.StatusOK && tracesBody.Data[0].RouteSlug == "local-qwen" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected recorded trace for local-qwen with status_code=200, got %+v", tracesBody)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func mustDoJSON(t *testing.T, client *http.Client, method, url, cookie, csrf string, body any, extraHeaders ...string) *http.Response {
	t.Helper()
	return mustDoJSONFull(t, client, method, url, cookie, csrf, "", body, 0, extraHeaders...)
}

func mustDoJSONExpect(t *testing.T, client *http.Client, method, url, cookie, csrf string, body any, wantCode int) *http.Response {
	t.Helper()
	return mustDoJSONFull(t, client, method, url, cookie, csrf, "", body, wantCode)
}

func mustDoJSONWithETag(t *testing.T, client *http.Client, method, url, cookie, csrf, etag string, body any) *http.Response {
	t.Helper()
	return mustDoJSONFull(t, client, method, url, cookie, csrf, etag, body, 0)
}

func mustDoJSONFull(t *testing.T, client *http.Client, method, url, cookie, csrf, etag string, body any, wantCode int, extraHeaders ...string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	for i := 0; i+1 < len(extraHeaders); i += 2 {
		req.Header.Set(extraHeaders[i], extraHeaders[i+1])
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s): %v", method, url, err)
	}
	if wantCode != 0 {
		if resp.StatusCode != wantCode {
			t.Fatalf("%s %s expected status %d, got %d", method, url, wantCode, resp.StatusCode)
		}
		return resp
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("%s %s failed with status %d body=%s", method, url, resp.StatusCode, raw.String())
	}
	return resp
}

func mustDecodeBody(t *testing.T, rc interface {
	Read([]byte) (int, error)
	Close() error
}, out any) {
	t.Helper()
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(out); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func extractSessionCookie(t *testing.T, cookies []*http.Cookie) string {
	t.Helper()
	for _, c := range cookies {
		if c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatalf("expected non-empty session cookie in response")
	return ""
}
