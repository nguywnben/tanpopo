// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/builtin"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func packDeclarativeTpkg(t *testing.T, manifestYAML string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		t.Fatalf("gzip.NewWriterLevel: %v", err)
	}
	gw.Header.ModTime = time.Unix(0, 0).UTC()
	gw.Header.OS = 255
	tw := tar.NewWriter(gw)
	data := []byte(manifestYAML)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "manifest.yaml",
		Mode:     0o644,
		Size:     int64(len(data)),
		ModTime:  time.Unix(0, 0).UTC(),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("tw.Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gw.Close: %v", err)
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func startRealHTTPListener(t *testing.T, handler http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return "http://" + ln.Addr().String()
}

func TestDEF048_NativeIngressAllRolesDialectsSecurityAndAccounting(t *testing.T) {
	dataDir := t.TempDir()
	archiveRoot := t.TempDir()
	masterKey := "1112131415161718191a1b1c1d1e1f202122232425262728292a2b2c2d2e2f30"

	// Generate an offline release Ed25519 keypair to sign built-in @tanpopo/ollama without KEK derivation.
	relPub, relPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cat, err := builtin.NewCatalog()
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	var ollamaPkg builtin.Package
	for _, pkg := range cat.Packages() {
		if pkg.PackageID == "@tanpopo/ollama" {
			ollamaPkg = pkg
			break
		}
	}
	if ollamaPkg.PackageID == "" {
		t.Fatal("missing built-in @tanpopo/ollama")
	}
	ollamaSig := ed25519.Sign(relPriv, installer.SignatureSigningPayload(ollamaPkg.PackageID, ollamaPkg.Version, ollamaPkg.ArchiveSHA256))
	sigBytes, err := json.Marshal(map[string]any{
		"signatures": []map[string]string{{
			"package_id":            ollamaPkg.PackageID,
			"version":               ollamaPkg.Version,
			"archive_sha256":        ollamaPkg.ArchiveSHA256,
			"root_signature_base64": base64.StdEncoding.EncodeToString(ollamaSig),
		}},
	})
	if err != nil {
		t.Fatalf("Marshal signatures: %v", err)
	}
	sigPath := filepath.Join(t.TempDir(), "release-signatures.json")
	if err := os.WriteFile(sigPath, sigBytes, 0o600); err != nil {
		t.Fatalf("WriteFile release-signatures: %v", err)
	}

	// Upstream mocks capturing exact incoming headers & native bodies + testing SSE cancellation.
	var (
		mu              sync.Mutex
		sawOllamaOpts   bool
		sawAnthHeader   string
		sawAnthAuthKey  string
		sawAnthThinking bool
		sawGemHeaderKey string
		streamCanceled  atomic.Bool
		cancelReady     = make(chan struct{})
		cancelOnce      sync.Once
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen2.5:7b"}]}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"cmpl-1","object":"chat.completion","created":1700000000,"model":"qwen2.5:7b","choices":[{"index":0,"message":{"role":"assistant","content":"v1-ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
		case r.URL.Path == "/api/chat" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"seed":42`) && strings.Contains(string(body), `"temperature":0.15`) {
				mu.Lock()
				sawOllamaOpts = true
				mu.Unlock()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"model":"qwen2.5:7b","message":{"role":"assistant","content":"native-ollama-ok"},"done":true,"prompt_eval_count":12,"eval_count":7}`))
		case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			sawAnthHeader = r.Header.Get("anthropic-version")
			sawAnthAuthKey = r.Header.Get("x-api-key")
			if strings.Contains(string(body), `"budget_tokens":1024`) {
				sawAnthThinking = true
			}
			mu.Unlock()
			if strings.Contains(string(body), `"hold_open":true`) {
				w.Header().Set("Content-Type", "text/event-stream")
				fl, _ := w.(http.Flusher)
				_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":0}}}\n\n"))
				if fl != nil {
					fl.Flush()
				}
				cancelOnce.Do(func() { close(cancelReady) })
				<-r.Context().Done()
				streamCanceled.Store(true)
				return
			}
			if strings.Contains(string(body), `"stream":true`) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"anth-sse-ok\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4","content":[{"type":"text","text":"native-anthropic-ok"}],"usage":{"input_tokens":14,"output_tokens":6}}`))
		case r.URL.Path == "/v1beta/models/gemini-2.5-flash:generateContent" && r.Method == http.MethodPost:
			mu.Lock()
			sawGemHeaderKey = r.Header.Get("x-goog-api-key")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"native-gemini-ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`))
		case r.URL.Path == "/v1beta/models/gemini-2.5-flash:streamGenerateContent" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"gem-sse-ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":6,\"candidatesTokenCount\":4}}\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	// Pack local declarative adapters for Anthropic & Gemini native dialects against loopback.
	anthTpkg, anthSHA := packDeclarativeTpkg(t, fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@third/native-anthropic-local"
  name: "Native Anthropic Local"
  version: "0.1.0"
  license: "Apache-2.0"
  originTrustTier: "unverified_external"
  description: "Local native Anthropic passthrough test adapter"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "anthropic"
    supportedDialects: ["anthropic", "anthropic_messages", "openai_chat"]
    nativePassthroughPrefix: "/native/anthropic"
    allowedNativePaths: ["^v1/messages$"]
    allowedNativeHeaders: ["anthropic-version", "anthropic-beta"]
    authMethods:
      - id: "x_api_key"
        mode: "host_blind_inject"
        headerName: "x-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["x_api_key"]
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1"]
    allowPrivateIp: true
    requireTls: false
  hooks: ["provider:build_validation_probe", "provider:verify_validation_result", "provider:transform_request", "provider:transform_response", "provider:extract_usage", "provider:inspect_native_request"]
adapter:
  slug: "anthropic"
  nativePrefix: "/native/anthropic"
  baseUrl: %q
  supportedDialects: ["anthropic", "anthropic_messages", "openai_chat"]
  endpoints:
    chat: "/v1/messages"
    models: "/v1/models"
  auth:
    - id: "x_api_key"
      secretSlot: "api_key"
      mode: "host_blind_inject"
      headerName: "x-api-key"
  usageExtraction:
    inputTokensPath: "$.usage.input_tokens"
    outputTokensPath: "$.usage.output_tokens"
`, upstream.URL))
	if err := os.WriteFile(filepath.Join(archiveRoot, "anthropic-local.tpkg"), anthTpkg, 0o600); err != nil {
		t.Fatalf("write anthropic-local.tpkg: %v", err)
	}

	gemTpkg, gemSHA := packDeclarativeTpkg(t, fmt.Sprintf(`apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@third/native-gemini-local"
  name: "Native Gemini Local"
  version: "0.1.0"
  license: "Apache-2.0"
  originTrustTier: "unverified_external"
  description: "Local native Gemini passthrough test adapter"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0 <0.3.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "gemini"
    supportedDialects: ["gemini", "gemini_generate_content", "openai_chat"]
    nativePassthroughPrefix: "/native/gemini"
    allowedNativePaths: ["^v1beta/models/[^/]+:(generateContent|streamGenerateContent)$"]
    authMethods:
      - id: "x_goog_api_key"
        mode: "host_blind_inject"
        headerName: "x-goog-api-key"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["x_goog_api_key"]
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1"]
    allowPrivateIp: true
    requireTls: false
  hooks: ["provider:build_validation_probe", "provider:verify_validation_result", "provider:transform_request", "provider:transform_response", "provider:extract_usage", "provider:inspect_native_request"]
adapter:
  slug: "gemini"
  nativePrefix: "/native/gemini"
  baseUrl: %q
  supportedDialects: ["gemini", "gemini_generate_content", "openai_chat"]
  endpoints:
    chat: "/v1beta/models/gemini-2.5-flash:generateContent"
    models: "/v1/models"
  auth:
    - id: "x_goog_api_key"
      secretSlot: "api_key"
      mode: "host_blind_inject"
      headerName: "x-goog-api-key"
  usageExtraction:
    inputTokensPath: "$.usageMetadata.promptTokenCount"
    outputTokensPath: "$.usageMetadata.candidatesTokenCount"
`, upstream.URL))
	if err := os.WriteFile(filepath.Join(archiveRoot, "gemini-local.tpkg"), gemTpkg, 0o600); err != nil {
		t.Fatalf("write gemini-local.tpkg: %v", err)
	}

	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", masterKey)
	t.Setenv("TANPOPO_TEST_HERMETIC", "1")
	t.Setenv("TANPOPO_OFFLINE_MODE", "true")
	t.Setenv(installer.LocalMarketplaceArchiveRootEnv, archiveRoot)
	t.Setenv("TANPOPO_RELEASE_ROOT_PUBKEYS", hex.EncodeToString(relPub))
	t.Setenv("TANPOPO_RELEASE_SIGNATURES_PATH", sigPath)
	t.Setenv("TANPOPO_CORS_ALLOWED_ORIGINS", "https://native-app.example.com")

	// Stage 1: Boot real `--role all` HTTP server on TCP listener.
	allHandler, closeAll, err := buildRuntime(context.Background(), "all", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("buildRuntime all: %v", err)
	}
	allClosed := false
	defer func() {
		if !allClosed {
			_ = closeAll()
		}
	}()
	baseAllURL := startRealHTTPListener(t, allHandler)
	client := netguard.NewHTTPClient()
	client.Timeout = 10 * time.Second

	tokBytes, err := os.ReadFile(filepath.Join(dataDir, "bootstrap.token"))
	if err != nil {
		t.Fatalf("read bootstrap.token: %v", err)
	}
	bootResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/bootstrap/complete", "", "", map[string]any{
		"bootstrap_token":                strings.TrimSpace(string(tokBytes)),
		"owner_email":                    "owner@native.test",
		"owner_password":                 "StrongPassword!2026",
		"org_name":                       "Native Org",
		"project_name":                   "native-proj",
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

	installAndApprove := func(uri, digest string, needsUnverified bool) (pkgID, insID string) {
		stResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/extensions/install", cookie, bootBody.CSRFToken, map[string]any{
			"source_uri": uri, "expected_sha256": digest,
		})
		etag := stResp.Header.Get("ETag")
		var diff struct {
			InstallationID string `json:"installation_id"`
		}
		mustDecodeBody(t, stResp.Body, &diff)
		appResp := mustDoJSONWithETag(t, client, http.MethodPost, baseAllURL+"/admin/v1/extensions/"+diff.InstallationID+"/approve-permissions", cookie, bootBody.CSRFToken, etag, map[string]any{
			"enable_immediately":            true,
			"acknowledge_unverified_external": needsUnverified,
		})
		var app struct {
			PackageID string `json:"package_id"`
			ID        string `json:"id"`
		}
		mustDecodeBody(t, appResp.Body, &app)
		return app.PackageID, app.ID
	}

	_, _ = installAndApprove(ollamaPkg.SourceURI, ollamaPkg.ArchiveSHA256, false)
	anthPkgID, anthInsID := installAndApprove("tanpopo-local://anthropic-local.tpkg", anthSHA, true)
	gemPkgID, gemInsID := installAndApprove("tanpopo-local://gemini-local.tpkg", gemSHA, true)

	// Opt in unverified local test adapters for project routes per TP-BR-0202.
	polResp := mustDoJSONWithETag(t, client, http.MethodPut, baseAllURL+"/admin/v1/organizations/"+bootBody.OrgID+"/extension-policies", cookie, bootBody.CSRFToken, `"v1"`, map[string]any{
		"min_plugin_trust": "ANY_APPROVED",
		"policies": []map[string]any{
			{"package_id": anthPkgID, "enabled": true, "pinned_installation_id": anthInsID},
			{"package_id": gemPkgID, "enabled": true, "pinned_installation_id": gemInsID},
		},
	})
	_ = polResp.Body.Close()

	// Find registered adapter IDs.
	adpResp := mustDoJSON(t, client, http.MethodGet, baseAllURL+"/admin/v1/provider-adapters", cookie, "", nil)
	var adpList struct {
		Data []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	mustDecodeBody(t, adpResp.Body, &adpList)
	adpBySlug := map[string]string{}
	for _, a := range adpList.Data {
		adpBySlug[a.Slug] = a.ID
	}
	for _, s := range []string{"ollama", "anthropic", "gemini"} {
		if adpBySlug[s] == "" {
			t.Fatalf("adapter %q missing from /provider-adapters: %+v", s, adpBySlug)
		}
	}

	sealSecret := func(name, val string) string {
		r := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/secrets", cookie, bootBody.CSRFToken, map[string]any{
			"name": name, "secret_kind": "API_KEY", "secret_value": val,
		})
		var sec struct {
			CredentialID string `json:"credential_id"`
		}
		mustDecodeBody(t, r.Body, &sec)
		return sec.CredentialID
	}
	anthSecID := sealSecret("anth-sec", "sk-anth-upstream")
	gemSecID := sealSecret("gem-sec", "gk-gem-upstream")

	createReadyRoute := func(slug, adapterSlug string, bindings map[string]string, upstreamModel, routeSlug string) {
		connPayload := map[string]any{
			"adapter_id": adapterSlug, "name": slug, "billing_mode": "SELF_HOSTED_INFRA",
			"endpoint_url": upstream.URL, "allow_private_ip": true,
			"allowed_cidrs": []string{"127.0.0.1/32"}, "data_region": "ON_PREM",
		}
		if len(bindings) > 0 {
			connPayload["secret_bindings"] = bindings
		}
		cResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/connections", cookie, bootBody.CSRFToken, connPayload)
		cETag := cResp.Header.Get("ETag")
		var c struct{ ID string `json:"id"` }
		mustDecodeBody(t, cResp.Body, &c)
		tResp := mustDoJSONWithETag(t, client, http.MethodPost, baseAllURL+"/admin/v1/connections/"+c.ID+"/test", cookie, bootBody.CSRFToken, cETag, map[string]any{})
		_ = tResp.Body.Close()

		mResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/models", cookie, bootBody.CSRFToken, map[string]any{
			"slug": routeSlug, "display_name": routeSlug, "modality_kind": "CHAT",
			"context_window": 32768, "max_output_tokens": 4096, "assurance_tier": "TESTED",
			"capabilities": map[string]any{"streaming": true},
		})
		var m struct{ ID string `json:"id"` }
		mustDecodeBody(t, mResp.Body, &m)

		act := true
		dResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/deployments", cookie, bootBody.CSRFToken, map[string]any{
			"env_id": bootBody.EnvID, "connection_id": c.ID, "logical_model_id": m.ID,
			"upstream_model_name": upstreamModel, "data_region": "ON_PREM", "activate_immediately": &act,
			"rate_card": map[string]any{"input_token_nano_usd": 1000, "output_token_nano_usd": 2000},
		})
		var d struct{ ID string `json:"id"` }
		mustDecodeBody(t, dResp.Body, &d)

		rResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/routes", cookie, bootBody.CSRFToken, map[string]any{
			"slug": routeSlug, "env_id": bootBody.EnvID, "strategy": "PRIORITY_FAILOVER",
			"allowed_data_regions": []string{"ON_PREM"},
			"targets": []map[string]any{{"deployment_id": d.ID, "priority_order": 1}},
		})
		_ = rResp.Body.Close()
	}

	createReadyRoute("conn-ollama", adpBySlug["ollama"], nil, "qwen2.5:7b", "qwen2.5:7b")
	createReadyRoute("conn-anth", adpBySlug["anthropic"], map[string]string{"api_key": anthSecID}, "claude-sonnet-4", "claude-sonnet-4")
	createReadyRoute("conn-gem", adpBySlug["gemini"], map[string]string{"api_key": gemSecID}, "gemini-2.5-flash", "gemini-2.5-flash")

	// SSRF protection: creating a connection to loopback with allow_private_ip=false fails closed!
	ssrfResp := mustDoJSONFull(t, client, http.MethodPost, baseAllURL+"/admin/v1/connections", cookie, bootBody.CSRFToken, "", map[string]any{
		"adapter_id": adpBySlug["ollama"], "name": "ssrf-block", "billing_mode": "SELF_HOSTED_INFRA",
		"endpoint_url": upstream.URL, "allow_private_ip": false, "data_region": "US",
	}, http.StatusBadRequest)
	_ = ssrfResp.Body.Close()

	// Create Virtual Key with PROVIDER plane AND another key restricted strictly to MCP plane.
	vkResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/virtual-keys", cookie, bootBody.CSRFToken, map[string]any{
		"project_id": bootBody.ProjectID, "env_id": bootBody.EnvID, "name": "native-vk",
		"allowed_planes": []string{"PROVIDER"},
	})
	var vk struct{ SecretKey string `json:"secret_key"` }
	mustDecodeBody(t, vkResp.Body, &vk)

	mcpVKResp := mustDoJSON(t, client, http.MethodPost, baseAllURL+"/admin/v1/virtual-keys", cookie, bootBody.CSRFToken, map[string]any{
		"project_id": bootBody.ProjectID, "env_id": bootBody.EnvID, "name": "mcp-only-vk",
		"allowed_planes": []string{"MCP"},
	})
	var mcpVK struct{ SecretKey string `json:"secret_key"` }
	mustDecodeBody(t, mcpVKResp.Body, &mcpVK)

	verifyNativeSuite := func(roleLabel, baseURL string) {
		t.Helper()
		// 1. Auth fail -> 401 JSON canonical envelope (never HTML/404!).
		unauthResp := mustDoJSONFull(t, client, http.MethodPost, baseURL+"/native/ollama/api/chat", "", "", "", map[string]any{
			"model": "qwen2.5:7b", "messages": []map[string]any{{"role": "user", "content": "hi"}},
		}, http.StatusUnauthorized)
		if !strings.Contains(unauthResp.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("[%s] expected JSON Content-Type on 401, got %q", roleLabel, unauthResp.Header.Get("Content-Type"))
		}
		_ = unauthResp.Body.Close()

		// 2. Wrong plane (MCP-only key calling /native/...) -> 403 JSON!
		wrongPlaneResp := mustDoJSONFull(t, client, http.MethodPost, baseURL+"/native/ollama/api/chat", "", "", "", map[string]any{
			"model": "qwen2.5:7b", "messages": []map[string]any{{"role": "user", "content": "hi"}},
		}, http.StatusForbidden, "Authorization", "Bearer "+mcpVK.SecretKey)
		_ = wrongPlaneResp.Body.Close()

		// 3. Disallowed native path -> 403 native_path_not_allowed!
		badPathResp := mustDoJSONFull(t, client, http.MethodPost, baseURL+"/native/gemini/v1beta/models/gemini-2.5-flash:evilAction", "", "", "", map[string]any{
			"model": "gemini-2.5-flash", "contents": []any{},
		}, http.StatusForbidden, "x-goog-api-key", vk.SecretKey)
		_ = badPathResp.Body.Close()

		// 4. Security envelope on /native/: no CORS origin reflection, nosniff present, OPTIONS 405, WebSocket upgrade 400.
		optReq, _ := http.NewRequest(http.MethodOptions, baseURL+"/native/ollama/api/chat", nil)
		optReq.Header.Set("Origin", "https://evil-cors.example.com")
		optReq.Header.Set("Access-Control-Request-Method", "POST")
		optResp, optErr := client.Do(optReq)
		if optErr != nil {
			t.Fatalf("[%s] OPTIONS /native failed: %v", roleLabel, optErr)
		}
		if optResp.StatusCode != http.StatusMethodNotAllowed || optResp.Header.Get("Access-Control-Allow-Origin") != "" || optResp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("[%s] unexpected OPTIONS security envelope on /native: status=%d acao=%q nosniff=%q", roleLabel, optResp.StatusCode, optResp.Header.Get("Access-Control-Allow-Origin"), optResp.Header.Get("X-Content-Type-Options"))
		}
		_ = optResp.Body.Close()

		upgReq, _ := http.NewRequest(http.MethodPost, baseURL+"/native/ollama/api/chat", strings.NewReader(`{}`))
		upgReq.Header.Set("Connection", "Upgrade")
		upgReq.Header.Set("Upgrade", "websocket")
		upgResp, upgErr := client.Do(upgReq)
		if upgErr != nil {
			t.Fatalf("[%s] Upgrade /native failed: %v", roleLabel, upgErr)
		}
		_ = upgResp.Body.Close()
		if upgResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("[%s] expected 400 on WebSocket upgrade to /native, got %d", roleLabel, upgResp.StatusCode)
		}

		// 5. Native Ollama JSON (/native/ollama/api/chat) preserves options + token accounting!
		olResp := mustDoJSON(t, client, http.MethodPost, baseURL+"/native/ollama/api/chat", "", "", map[string]any{
			"model":    "qwen2.5:7b",
			"messages": []map[string]any{{"role": "user", "content": "hi"}},
			"stream":   false,
			"options":  map[string]any{"temperature": 0.15, "seed": 42},
		}, "Authorization", "Bearer "+vk.SecretKey)
		var olOut struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		}
		mustDecodeBody(t, olResp.Body, &olOut)
		if olOut.Message.Content != "native-ollama-ok" {
			t.Fatalf("[%s] unexpected ollama native content: %+v", roleLabel, olOut)
		}

		// 6. Native Anthropic JSON + SSE (/native/anthropic/v1/messages) with x-api-key credential swap!
		anResp := mustDoJSON(t, client, http.MethodPost, baseURL+"/native/anthropic/v1/messages", "", "", map[string]any{
			"model":      "claude-sonnet-4",
			"max_tokens": 256,
			"thinking":   map[string]any{"type": "enabled", "budget_tokens": 1024},
			"messages":   []map[string]any{{"role": "user", "content": "hello"}},
		}, "x-api-key", vk.SecretKey, "anthropic-version", "2023-06-01")
		anRaw, _ := io.ReadAll(anResp.Body)
		_ = anResp.Body.Close()
		if !strings.Contains(string(anRaw), "native-anthropic-ok") {
			t.Fatalf("[%s] unexpected anthropic native response: %s", roleLabel, string(anRaw))
		}

		anSSE := mustDoJSON(t, client, http.MethodPost, baseURL+"/native/anthropic/v1/messages", "", "", map[string]any{
			"model": "claude-sonnet-4", "max_tokens": 128, "stream": true,
			"messages": []map[string]any{{"role": "user", "content": "stream"}},
		}, "x-api-key", vk.SecretKey, "anthropic-version", "2023-06-01")
		sseRaw, _ := io.ReadAll(anSSE.Body)
		_ = anSSE.Body.Close()
		if !strings.Contains(string(sseRaw), "anth-sse-ok") || !strings.Contains(string(sseRaw), "event: message_stop") {
			t.Fatalf("[%s] unexpected anthropic SSE body: %s", roleLabel, string(sseRaw))
		}

		// 7. Native Gemini JSON + SSE (/native/gemini/v1beta/models/...) with x-goog-api-key blind swap!
		gmResp := mustDoJSON(t, client, http.MethodPost, baseURL+"/native/gemini/v1beta/models/gemini-2.5-flash:generateContent", "", "", map[string]any{
			"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "hi"}}}},
		}, "x-goog-api-key", vk.SecretKey)
		gmRaw, _ := io.ReadAll(gmResp.Body)
		_ = gmResp.Body.Close()
		if !strings.Contains(string(gmRaw), "native-gemini-ok") {
			t.Fatalf("[%s] unexpected gemini native response: %s", roleLabel, string(gmRaw))
		}

		gmSSE := mustDoJSON(t, client, http.MethodPost, baseURL+"/native/gemini/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse", "", "", map[string]any{
			"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "hi sse"}}}},
		}, "x-goog-api-key", vk.SecretKey)
		gmSSERaw, _ := io.ReadAll(gmSSE.Body)
		_ = gmSSE.Body.Close()
		if !strings.Contains(string(gmSSERaw), "gem-sse-ok") {
			t.Fatalf("[%s] unexpected gemini SSE response: %s", roleLabel, string(gmSSERaw))
		}
	}

	verifyNativeSuite("all", baseAllURL)

	// 8. Client cancellation during active SSE stream propagates abort to upstream.
	cancelCtx, cancelReq := context.WithCancel(context.Background())
	reqHold, _ := http.NewRequestWithContext(cancelCtx, http.MethodPost, baseAllURL+"/native/anthropic/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4","max_tokens":64,"stream":true,"hold_open":true,"messages":[{"role":"user","content":"wait"}]}`))
	reqHold.Header.Set("Content-Type", "application/json")
	reqHold.Header.Set("x-api-key", vk.SecretKey)
	reqHold.Header.Set("anthropic-version", "2023-06-01")
	holdResp, holdErr := client.Do(reqHold)
	if holdErr != nil {
		t.Fatalf("start hold_open stream: %v", holdErr)
	}
	rd := bufio.NewReader(holdResp.Body)
	_, _ = rd.ReadString('\n')
	select {
	case <-cancelReady:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for upstream hold_open stream start")
	}
	cancelReq()
	_ = holdResp.Body.Close()
	deadlineCancel := time.Now().Add(3 * time.Second)
	for !streamCanceled.Load() && time.Now().Before(deadlineCancel) {
		time.Sleep(10 * time.Millisecond)
	}
	if !streamCanceled.Load() {
		t.Fatal("expected upstream SSE handler context cancellation after client abort")
	}

	mu.Lock()
	okOllama := sawOllamaOpts
	gotAnthVer := sawAnthHeader
	gotAnthKey := sawAnthAuthKey
	okAnthThink := sawAnthThinking
	gotGemKey := sawGemHeaderKey
	mu.Unlock()
	if !okOllama || gotAnthVer != "2023-06-01" || gotAnthKey != "sk-anth-upstream" || !okAnthThink || gotGemKey != "gk-gem-upstream" {
		t.Fatalf("native field/blind-credential fidelity mismatch: ollama=%v anthVer=%q anthKey=%q anthThink=%v gemKey=%q", okOllama, gotAnthVer, gotAnthKey, okAnthThink, gotGemKey)
	}

	// 9. Verify FinOps settlements & request_traces_meta persisted for native calls before closing `--role all`.
	dbConn, err := sql.Open("sqlite", filepath.Join(dataDir, "tanpopo.db"))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer dbConn.Close()
	deadlinePersist := time.Now().Add(5 * time.Second)
	var traceCount, settlementCount int
	for time.Now().Before(deadlinePersist) {
		if qerr := dbConn.QueryRow(`SELECT COUNT(*) FROM request_traces_meta WHERE entry_point = 'native_http' AND http_status = 200`).Scan(&traceCount); qerr != nil {
			t.Fatalf("query request_traces_meta: %v", qerr)
		}
		if qerr := dbConn.QueryRow(`SELECT COUNT(*) FROM usage_settlements WHERE total_cost_micros > 0`).Scan(&settlementCount); qerr != nil {
			t.Fatalf("query usage_settlements: %v", qerr)
		}
		if traceCount >= 4 && settlementCount >= 2 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if traceCount < 4 || settlementCount < 2 {
		t.Fatalf("expected persisted native traces (>=4) & FinOps settlements (>=2), got traces=%d settlements=%d", traceCount, settlementCount)
	}

	// Shut down `--role all` cleanly, then verify `--role data-plane` exposes /native/* + /v1/* cleanly without /admin/v1/*.
	if err := closeAll(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("closeAll: %v", err)
	}
	allClosed = true

	dpHandler, closeDP, err := buildRuntime(context.Background(), "data-plane", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("buildRuntime data-plane: %v", err)
	}
	defer func() { _ = closeDP() }()
	baseDPURL := startRealHTTPListener(t, dpHandler)

	admResp, err := client.Get(baseDPURL + "/admin/v1/auth/session")
	if err != nil {
		t.Fatalf("GET /admin on data-plane: %v", err)
	}
	_ = admResp.Body.Close()
	if admResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 on /admin/v1 under --role data-plane, got %d", admResp.StatusCode)
	}

	verifyNativeSuite("data-plane", baseDPURL)
}
