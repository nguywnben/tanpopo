// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

// TestEXTAccept01_LiveEURoutingAndSettlement runs the production binary,
// configures an EU-only route through the authenticated Admin API, and proves
// both endpoint selection and the persisted exact/estimated ledger split.
func TestEXTAccept01_LiveEURoutingAndSettlement(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate Tanpopo module: %v", err)
	}
	work := t.TempDir()
	binary := filepath.Join(work, "tanpopo")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/tanpopo")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Tanpopo binary: %v\n%s", err, output)
	}
	initialSHA := fileSHA256(t, binary)

	var euRequests, usRequests atomic.Int64
	fakeProvider := func(counter *atomic.Int64) *http.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"accept-chat"}]}`))
		})
		mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
			counter.Add(1)
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer live-routing-test-secret" {
				http.Error(w, "unexpected method or missing brokered credential", http.StatusUnauthorized)
				return
			}
			var request struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
				Stream bool `json:"stream"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Messages) == 0 {
				http.Error(w, "invalid chat request", http.StatusBadRequest)
				return
			}
			if request.Stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
					return
				}
				_, _ = fmt.Fprintln(w, `data: {"id":"chatcmpl-live-stream","object":"chat.completion.chunk","model":"accept-chat","choices":[{"index":0,"delta":{"role":"assistant","content":"tool result "},"finish_reason":null}]}`)
				flusher.Flush()
				_, _ = fmt.Fprintln(w, `data: {"id":"chatcmpl-live-stream","object":"chat.completion.chunk","model":"accept-chat","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_live","type":"function","function":{"name":"lookup_weather","arguments":"{}"}}]},"finish_reason":null}]}`)
				flusher.Flush()
				_, _ = fmt.Fprintln(w, `data: {"id":"chatcmpl-live-stream","object":"chat.completion.chunk","model":"accept-chat","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":64,"completion_tokens":13,"total_tokens":77}}`)
				_, _ = fmt.Fprintln(w, "data: [DONE]")
				flusher.Flush()
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(request.Messages[0].Content, "estimated") {
				// Deliberately omit upstream usage counters. The live gateway must
				// estimate from observed request/response text and persist ESTIMATED.
				_, _ = w.Write([]byte(`{"id":"chatcmpl-live-eu-estimated","object":"chat.completion","model":"accept-chat","choices":[{"index":0,"message":{"role":"assistant","content":"deterministic estimated usage response"},"finish_reason":"stop"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"chatcmpl-live-eu-exact","object":"chat.completion","model":"accept-chat","choices":[{"index":0,"message":{"role":"assistant","content":"deterministic estimated usage response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":64,"completion_tokens":13,"total_tokens":77}}`))
		})
		return &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}
	}
	euFake, usFake := fakeProvider(&euRequests), fakeProvider(&usRequests)
	startFake := func(server *http.Server) string {
		t.Helper()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(func() { _ = server.Close() })
		return "http://" + listener.Addr().String()
	}
	euURL, usURL := startFake(euFake), startFake(usFake)
	euHost, usHost := hostPort(t, euURL), hostPort(t, usURL)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	dataDir := filepath.Join(work, "data")
	marketplaceDir := filepath.Join(work, "marketplace")
	if err := os.MkdirAll(marketplaceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	serverLog, err := os.Create(filepath.Join(work, "serve.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer serverLog.Close()
	server := exec.Command(binary, "serve", "--role=all", "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	server.Dir = root
	server.Env = append(os.Environ(),
		"TANPOPO_DATA_DIR="+dataDir,
		"TANPOPO_MASTER_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"TANPOPO_MASTER_KEY_FILE=",
		"TANPOPO_MARKETPLACE_ARCHIVE_ROOT="+marketplaceDir,
		"TANPOPO_METRICS_LISTEN=127.0.0.1:0",
		"TANPOPO_TEST_HERMETIC=1",
	)
	server.Stdout, server.Stderr = serverLog, serverLog
	if err := server.Start(); err != nil {
		t.Fatalf("start Tanpopo process: %v", err)
	}
	pid := server.Process.Pid
	t.Cleanup(func() {
		if server.ProcessState == nil {
			_ = server.Process.Kill()
		}
		_ = server.Wait()
	})
	client := netguard.NewHTTPClient()
	client.Timeout = 20 * time.Second
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, getErr := client.Get(baseURL + "/readyz")
		if getErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			_ = server.Process.Kill()
			_ = server.Wait()
			log, _ := os.ReadFile(filepath.Join(work, "serve.log"))
			t.Fatalf("live server did not become ready (pid=%d): %s", pid, log)
		}
		time.Sleep(40 * time.Millisecond)
	}
	assertCore := func(step string) {
		t.Helper()
		if server.ProcessState != nil || server.Process.Pid != pid {
			t.Fatalf("%s: Tanpopo process changed or exited (pid=%d)", step, pid)
		}
		if got := fileSHA256(t, binary); got != initialSHA {
			t.Fatalf("%s: binary SHA-256 changed: got %s, want %s", step, got, initialSHA)
		}
	}
	assertCore("ready")

	status := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/bootstrap/status", nil, nil, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body, `"initialized":false`) {
		t.Fatalf("bootstrap status: got %d: %s", status.Code, status.Body)
	}
	tokenBytes, err := os.ReadFile(filepath.Join(dataDir, "bootstrap.token"))
	if err != nil {
		t.Fatalf("read one-time bootstrap token: %v", err)
	}
	boot := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token": strings.TrimSpace(string(tokenBytes)), "owner_email": "owner-routing@example.test",
		"owner_password": "StrongLiveOwner#2032!", "org_name": "Live Routing Org", "project_name": "Live Routing Project",
		"master_kek_backup_acknowledged": true,
	}, nil, baseURL)
	if boot.Code != http.StatusCreated {
		t.Fatalf("complete bootstrap: got %d: %s", boot.Code, boot.Body)
	}
	var bootResult struct {
		OrgID     string `json:"org_id"`
		ProjectID string `json:"project_id"`
		EnvID     string `json:"env_id"`
	}
	if err := json.Unmarshal([]byte(boot.Body), &bootResult); err != nil || bootResult.OrgID == "" || bootResult.ProjectID == "" || bootResult.EnvID == "" {
		t.Fatalf("bootstrap response lacks organization/project/environment IDs: err=%v body=%s", err, boot.Body)
	}
	login := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/auth/login", map[string]string{
		"email": "owner-routing@example.test", "password": "StrongLiveOwner#2032!", "org_id": bootResult.OrgID,
	}, nil, baseURL)
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: got %d: %s", login.Code, login.Body)
	}
	cookie := findLiveSessionCookie(t, login.Cookies)
	var loginResult struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal([]byte(login.Body), &loginResult); err != nil || loginResult.CSRFToken == "" {
		t.Fatalf("login response lacks CSRF token: err=%v body=%s", err, login.Body)
	}
	assertCore("bootstrap and login")

	// Install a real Declarative adapter whose egress permission is restricted
	// to the two local provider listeners used by this test.
	extensionDir := filepath.Join(work, "live-routing-adapter")
	init := exec.Command(binary, "plugin", "init", "live-routing-adapter", "--namespace", "acceptance", "--dir", extensionDir)
	init.Dir = work
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("scaffold provider adapter: %v\n%s", err, output)
	}
	manifestPath := filepath.Join(extensionDir, "manifest.yaml")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(string(manifestBytes), `      - "api.openai.com"`, `      - "`+euHost+`"`+"\n"+`      - "`+usHost+`"`, 1)
	manifest = strings.Replace(manifest, `baseUrl: "https://api.openai.com"`, `baseUrl: "`+euURL+`"`, 1)
	manifest = strings.ReplaceAll(manifest, "starter-declarative", "live-routing-adapter")
	manifest = strings.Replace(manifest, "    allowPrivateIp: false", "    allowPrivateIp: true", 1)
	manifest = strings.Replace(manifest, "    requireTls: true", "    requireTls: false", 1)
	if manifest == string(manifestBytes) || !strings.Contains(manifest, euHost) || !strings.Contains(manifest, usHost) || !strings.Contains(manifest, "allowPrivateIp: true") {
		t.Fatal("could not constrain adapter manifest egress to both local fake provider endpoints")
	}
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(marketplaceDir, "live-routing-adapter.tpkg")
	pack := exec.Command(binary, "plugin", "pack", extensionDir, "--out", archive)
	pack.Dir = work
	if output, err := pack.CombinedOutput(); err != nil {
		t.Fatalf("pack local adapter: %v\n%s", err, output)
	}
	stage := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/install", map[string]string{
		"source_uri": "tanpopo-local://live-routing-adapter.tpkg", "expected_sha256": fileSHA256(t, archive),
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if stage.Code != http.StatusAccepted {
		t.Fatalf("stage adapter via Admin API: got %d: %s", stage.Code, stage.Body)
	}
	var staged struct {
		InstallationID string `json:"installation_id"`
		PackageID      string `json:"package_id"`
	}
	if err := json.Unmarshal([]byte(stage.Body), &staged); err != nil || staged.InstallationID == "" || staged.PackageID == "" || stage.ETag == "" {
		t.Fatalf("stage response lacks installation ID/ETag: err=%v body=%s", err, stage.Body)
	}
	approved := liveJSONRequestWithHeaders(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/"+staged.InstallationID+"/approve-permissions", liveApprovePermissionsRequest{
		EnableImmediately: true, AcknowledgeUnverifiedExternal: true,
	}, []*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": stage.ETag}, loginResult.CSRFToken)
	if approved.Code != http.StatusOK {
		t.Fatalf("approve provider adapter: got %d: %s", approved.Code, approved.Body)
	}
	orgList := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/organizations", nil, []*http.Cookie{cookie}, baseURL)
	if orgList.Code != http.StatusOK {
		t.Fatalf("list organization before owner trust opt-in: got %d: %s", orgList.Code, orgList.Body)
	}
	var orgResult struct {
		Data []struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(orgList.Body), &orgResult); err != nil {
		t.Fatalf("decode owner organization list: %v: %s", err, orgList.Body)
	}
	var orgVersion int64
	for _, item := range orgResult.Data {
		if item.ID == bootResult.OrgID {
			orgVersion = item.Version
			break
		}
	}
	if orgVersion < 1 {
		t.Fatalf("owner organization missing from list: %s", orgList.Body)
	}
	trustOptIn := liveJSONRequestWithHeaders(t, client, http.MethodPut, baseURL+"/admin/v1/organizations/"+bootResult.OrgID+"/extension-policies", map[string]any{
		"min_plugin_trust": "ANY_APPROVED", "policies": []map[string]any{{
			"package_id": staged.PackageID, "enabled": true, "pinned_installation_id": staged.InstallationID,
		}},
	}, []*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": fmt.Sprintf("\"v%d\"", orgVersion)}, loginResult.CSRFToken)
	if trustOptIn.Code != http.StatusOK {
		t.Fatalf("owner opt-in for the unverified external adapter: got %d: %s", trustOptIn.Code, trustOptIn.Body)
	}
	denied := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/provider-adapters", map[string]any{
		"installation_id": staged.InstallationID, "adapter_slug": "live-routing-adapter",
		"allowed_hosts": []string{"not-approved.example.test"},
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if denied.Code != http.StatusBadRequest {
		t.Fatalf("registration must reject a host outside approved extension permissions: got %d: %s", denied.Code, denied.Body)
	}
	duplicate := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/provider-adapters", map[string]any{
		"installation_id": staged.InstallationID, "adapter_slug": "live-routing-adapter",
		"allowed_hosts": []string{euHost, usHost},
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate registration after auto-register on activation expected 409, got %d: %s", duplicate.Code, duplicate.Body)
	}
	var adapters struct {
		Data []struct {
			ID           string   `json:"id"`
			Slug         string   `json:"slug"`
			PackageID    string   `json:"package_id"`
			AllowedHosts []string `json:"allowed_hosts"`
		} `json:"data"`
	}
	adapterList := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/provider-adapters", nil, []*http.Cookie{cookie}, baseURL)
	if adapterList.Code != http.StatusOK || json.Unmarshal([]byte(adapterList.Body), &adapters) != nil {
		t.Fatalf("list installed provider adapters: got %d: %s", adapterList.Code, adapterList.Body)
	}
	var adapterID string
	for _, adapter := range adapters.Data {
		if adapter.Slug == "live-routing-adapter" && adapter.PackageID == staged.PackageID {
			adapterID = adapter.ID
			hostSet := map[string]bool{}
			for _, h := range adapter.AllowedHosts {
				hostSet[h] = true
			}
			if !hostSet[euHost] || !hostSet[usHost] {
				t.Fatalf("auto-registered adapter allowed_hosts=%v missing %q/%q", adapter.AllowedHosts, euHost, usHost)
			}
			break
		}
	}
	if adapterID == "" {
		t.Fatalf("approved adapter missing from live provider-adapters list: %s", adapterList.Body)
	}
	assertCore("adapter approval")

	secret := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/secrets", map[string]string{
		"name": "live fake provider key", "secret_kind": "API_KEY", "secret_value": "live-routing-test-secret",
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if secret.Code != http.StatusCreated {
		t.Fatalf("create brokered fake-provider credential: got %d: %s", secret.Code, secret.Body)
	}
	var secretResult struct {
		CredentialID string `json:"credential_id"`
	}
	if err := json.Unmarshal([]byte(secret.Body), &secretResult); err != nil || secretResult.CredentialID == "" {
		t.Fatalf("secret create response lacks credential ID: err=%v body=%s", err, secret.Body)
	}

	// Both regional connections below explicitly reference the adapter installed
	// and pinned above, so streaming and update traffic traverses this extension.
	createConnection := func(name, endpoint, region string) string {
		t.Helper()
		conn := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/connections", map[string]any{
			"name": name, "adapter_id": adapterID,
			"billing_mode": "COMMERCIAL_API", "endpoint_url": endpoint, "data_region": region,
			"allow_private_ip": true, "allowed_cidrs": []string{"127.0.0.1/32"},
			"secret_bindings": map[string]string{"api_key": secretResult.CredentialID},
		}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
		if conn.Code != http.StatusCreated {
			t.Fatalf("create %s provider connection: got %d: %s", region, conn.Code, conn.Body)
		}
		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(conn.Body), &result); err != nil || result.ID == "" {
			t.Fatalf("%s connection response lacks ID: err=%v body=%s", region, err, conn.Body)
		}
		tested := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/connections/"+result.ID+"/test", nil, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
		if tested.Code == http.StatusServiceUnavailable && strings.Contains(tested.Body, `"code":"probe_runtime_unavailable"`) {
			t.Fatalf("serve must expose the approved provider runtime to the Admin API; POST /admin/v1/connections/{id}/test returned 503: %s", tested.Body)
		}
		if tested.Code != http.StatusOK || !strings.Contains(tested.Body, `"valid":true`) {
			t.Fatalf("validate %s provider connection: got %d: %s", region, tested.Code, tested.Body)
		}
		return result.ID
	}
	euConnection := createConnection("EU local deterministic fake", euURL, "EU")
	usConnection := createConnection("US local deterministic fake", usURL, "US")

	model := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/models", map[string]any{
		"slug": "live-routing-chat", "display_name": "Live Routing Chat", "context_window": 4096,
		"max_output_tokens": 256, "assurance_tier": "TESTED", "capabilities": map[string]any{"streaming": true, "tool_calling": true},
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if model.Code != http.StatusCreated {
		t.Fatalf("create logical model: got %d: %s", model.Code, model.Body)
	}
	var modelResult struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(model.Body), &modelResult); err != nil || modelResult.ID == "" {
		t.Fatalf("logical model lacks ID: err=%v body=%s", err, model.Body)
	}

	createDeployment := func(connID, region string) string {
		t.Helper()
		resp := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/deployments", map[string]any{
			"env_id": bootResult.EnvID, "logical_model_id": modelResult.ID, "connection_id": connID,
			"upstream_model_name": "accept-chat", "data_region": region,
			"rate_card": map[string]any{"input_token_nano_usd": 13012000, "output_token_nano_usd": 12864000},
		}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create %s deployment: got %d: %s", region, resp.Code, resp.Body)
		}
		var result struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(resp.Body), &result); err != nil || result.ID == "" {
			t.Fatalf("%s deployment lacks ID: err=%v body=%s", region, err, resp.Body)
		}
		return result.ID
	}
	euDeployment := createDeployment(euConnection, "EU")
	usDeployment := createDeployment(usConnection, "US")
	blockedCrossRegionRoute := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/routes", map[string]any{
		"slug": "accept-reject-cross-region", "env_id": bootResult.EnvID, "strategy": "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"EU"}, "targets": []map[string]any{
			{"deployment_id": euDeployment, "priority_order": 1, "weight": 50},
			{"deployment_id": usDeployment, "priority_order": 2, "weight": 50},
		},
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if blockedCrossRegionRoute.Code != http.StatusUnprocessableEntity || !strings.Contains(blockedCrossRegionRoute.Body, `"code":"residency_policy_violation"`) {
		t.Fatalf("EU-constrained route must reject a US deployment candidate: got %d: %s", blockedCrossRegionRoute.Code, blockedCrossRegionRoute.Body)
	}
	route := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/routes", map[string]any{
		"slug": "accept-eu-only", "env_id": bootResult.EnvID, "strategy": "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"EU"}, "targets": []map[string]any{
			{"deployment_id": euDeployment, "priority_order": 1, "weight": 100},
		},
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if route.Code != http.StatusCreated {
		t.Fatalf("create EU-constrained route: got %d: %s", route.Code, route.Body)
	}
	explanation := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/routes/explain", map[string]any{
		"env_id": bootResult.EnvID, "target_model": "accept-eu-only", "data_region": "EU",
		"dialect": "openai_chat", "estimated_input_tokens": 38, "max_output_tokens": 13,
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if explanation.Code != http.StatusOK || strings.Contains(explanation.Body, `"selected_candidates_ordered":[]`) {
		t.Fatalf("EU route must resolve to an eligible deployment before ingress: got %d: %s", explanation.Code, explanation.Body)
	}

	key := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/virtual-keys", map[string]any{
		"name": "live EU routing key", "project_id": bootResult.ProjectID, "env_id": bootResult.EnvID,
		"allowed_models": []string{"accept-eu-only"},
	}, []*http.Cookie{cookie}, baseURL, loginResult.CSRFToken)
	if key.Code != http.StatusCreated {
		t.Fatalf("create ingress virtual key: got %d: %s", key.Code, key.Body)
	}
	var keyResult struct {
		ID        string `json:"id"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.Unmarshal([]byte(key.Body), &keyResult); err != nil || keyResult.ID == "" || keyResult.SecretKey == "" {
		t.Fatalf("virtual-key response lacks its ID or one-time secret: err=%v body=%s", err, key.Body)
	}

	callChat := func(content string) string {
		t.Helper()
		resp, err := liveIngressJSON(client, baseURL+"/v1/chat/completions", keyResult.SecretKey, map[string]any{
			"model": "accept-eu-only", "stream": false, "messages": []map[string]string{{"role": "user", "content": content}},
		})
		if err != nil {
			t.Fatalf("call live chat completions: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			t.Fatalf("live chat completion returned %d (content=%q body=%q)", resp.StatusCode, content, body)
		}
		return resp.Header.Get("X-Tanpopo-Usage-Accuracy")
	}
	if accuracy := callChat(strings.Repeat("x", 109)); accuracy != "EXACT" {
		t.Fatalf("response with authoritative provider usage must classify EXACT, got %q", accuracy)
	}
	assertCore("exact settlement")
	estimatedPrompt := "estimated" + strings.Repeat("x", 100) // The 192-byte JSON request estimates 64 input tokens; the 38-byte completion estimates 13 output tokens.
	if accuracy := callChat(estimatedPrompt); accuracy != "ESTIMATED" {
		t.Fatalf("provider response without usage counters must classify ESTIMATED after ledger settlement, got %q", accuracy)
	}
	assertCore("estimated settlement")

	if got := euRequests.Load(); got != 2 {
		t.Fatalf("EU provider must receive both requests, got %d", got)
	}
	if got := usRequests.Load(); got != 0 {
		t.Fatalf("EU-only route leaked traffic to US provider: got %d requests", got)
	}
	summary := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/usage/summary?group_by=project", nil, []*http.Cookie{cookie}, baseURL)
	if summary.Code != http.StatusOK {
		t.Fatalf("read live ledger summary: got %d: %s", summary.Code, summary.Body)
	}
	var ledger struct {
		Data []struct {
			GroupKey       string `json:"group_key"`
			ExactCost      int64  `json:"exact_micro_usd"`
			EstimatedCost  int64  `json:"estimated_micro_usd"`
			ExactCount     int64  `json:"exact_request_count"`
			EstimatedCount int64  `json:"estimated_request_count"`
			UnmeteredCount int64  `json:"unmetered_request_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(summary.Body), &ledger); err != nil {
		t.Fatalf("decode ledger summary: %v: %s", err, summary.Body)
	}
	if len(ledger.Data) != 1 || ledger.Data[0].ExactCount != 1 || ledger.Data[0].EstimatedCount != 1 || ledger.Data[0].ExactCost != 1_000_000 || ledger.Data[0].EstimatedCost != 1_000_000 || ledger.Data[0].UnmeteredCount != 0 {
		t.Fatalf("live ledger must prove separate $1.00 exact and estimated settlements: rows=%+v body=%s", ledger.Data, summary.Body)
	}
	assertCore("usage ledger verification")

	radiusResponse := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/blast-radius?target_type=EXTENSION&target_id="+staged.InstallationID, nil, []*http.Cookie{cookie}, baseURL)
	if radiusResponse.Code != http.StatusOK {
		t.Fatalf("query populated extension blast radius: got %d: %s", radiusResponse.Code, radiusResponse.Body)
	}
	var radius struct {
		Organizations []string `json:"organizations"`
		Projects      []string `json:"projects"`
		VirtualKeys   []string `json:"virtual_keys"`
		Connections   []string `json:"connections"`
	}
	if err := json.Unmarshal([]byte(radiusResponse.Body), &radius); err != nil {
		t.Fatalf("decode extension blast radius: %v: %s", err, radiusResponse.Body)
	}
	if len(radius.Organizations) != 1 || radius.Organizations[0] != bootResult.OrgID || len(radius.Projects) != 1 || radius.Projects[0] != bootResult.ProjectID || len(radius.VirtualKeys) != 1 || radius.VirtualKeys[0] != keyResult.ID || len(radius.Connections) != 1 || radius.Connections[0] != euConnection {
		t.Fatalf("blast radius must report the live EU request's org, project, virtual key, and connection: %+v", radius)
	}
	assertCore("populated four-scope blast-radius verification")

	streamResp, err := liveIngressJSON(client, baseURL+"/v1/chat/completions", keyResult.SecretKey, map[string]any{
		"model": "accept-eu-only", "stream": true,
		"messages": []map[string]string{{"role": "user", "content": "stream and call a tool"}},
		"tools":    []map[string]any{{"type": "function", "function": map[string]any{"name": "lookup_weather", "parameters": map[string]any{"type": "object"}}}},
	})
	if err != nil {
		t.Fatalf("send live streaming tool-call request: %v", err)
	}
	streamBody, readErr := io.ReadAll(streamResp.Body)
	_ = streamResp.Body.Close()
	if readErr != nil || streamResp.StatusCode != http.StatusOK || !strings.Contains(string(streamBody), `"tool_calls"`) || !strings.Contains(string(streamBody), "data: [DONE]") {
		t.Fatalf("live streaming tool-call contract failed: status=%d err=%v body=%s", streamResp.StatusCode, readErr, streamBody)
	}
	assertCore("streaming tool-call request")

	// Keep successful traffic flowing while a candidate permission expansion
	// waits for administrator re-consent.
	trafficStop := make(chan struct{})
	trafficDone := make(chan struct{})
	trafficErrors := make(chan error, 1)
	var successfulTraffic atomic.Int64
	go func() {
		defer close(trafficDone)
		for {
			select {
			case <-trafficStop:
				return
			default:
			}
			resp, requestErr := liveIngressJSON(client, baseURL+"/v1/chat/completions", keyResult.SecretKey, map[string]any{
				"model": "accept-eu-only", "stream": false,
				"messages": []map[string]string{{"role": "user", "content": "continuous upgrade traffic"}},
			})
			if requestErr != nil {
				select {
				case trafficErrors <- requestErr:
				default:
				}
				return
			}
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				select {
				case trafficErrors <- fmt.Errorf("traffic returned %d: %s", resp.StatusCode, body):
				default:
				}
				return
			}
			_ = resp.Body.Close()
			successfulTraffic.Add(1)
			time.Sleep(10 * time.Millisecond)
		}
	}()
	time.Sleep(20 * time.Millisecond)

	manifestV2 := strings.Replace(manifest, `version: "0.1.0"`, `version: "0.2.0"`, 1)
	manifestV2 = strings.Replace(manifestV2, `      - "`+usHost+`"`, `      - "`+usHost+`"`+"\n"+`      - "unapproved.example.test"`, 1)
	if manifestV2 == manifest || !strings.Contains(manifestV2, "unapproved.example.test") {
		t.Fatal("could not create an escalated live adapter candidate")
	}
	if err := os.WriteFile(manifestPath, []byte(manifestV2), 0o600); err != nil {
		t.Fatal(err)
	}
	escalatedArchive := filepath.Join(marketplaceDir, "live-routing-adapter-v2.tpkg")
	packEscalated := exec.Command(binary, "plugin", "pack", extensionDir, "--out", escalatedArchive)
	packEscalated.Dir = work
	if output, packErr := packEscalated.CombinedOutput(); packErr != nil {
		t.Fatalf("pack escalated live adapter: %v\n%s", packErr, output)
	}
	escalated := liveJSONRequestWithHeaders(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/"+staged.InstallationID+"/upgrade", map[string]string{
		"source_uri": "tanpopo-local://live-routing-adapter-v2.tpkg", "expected_sha256": fileSHA256(t, escalatedArchive),
	}, []*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": approved.ETag}, loginResult.CSRFToken)
	if escalated.Code != http.StatusAccepted || !strings.Contains(escalated.Body, `"has_escalation":true`) {
		t.Fatalf("permission-expanding live upgrade must wait for re-consent: got %d: %s", escalated.Code, escalated.Body)
	}
	pendingTrafficStart := successfulTraffic.Load()
	pendingDeadline := time.Now().Add(2 * time.Second)
	for successfulTraffic.Load() == pendingTrafficStart && time.Now().Before(pendingDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if successfulTraffic.Load() == pendingTrafficStart {
		t.Fatal("no inference succeeded while the extension upgrade was pending re-consent")
	}
	select {
	case trafficErr := <-trafficErrors:
		t.Fatalf("inference failed while the extension upgrade was pending re-consent: %v", trafficErr)
	default:
	}
	var escalation struct {
		ActiveVersion    *string `json:"active_version"`
		CandidateVersion *string `json:"candidate_version"`
	}
	if err := json.Unmarshal([]byte(escalated.Body), &escalation); err != nil || escalation.ActiveVersion == nil || *escalation.ActiveVersion != "0.1.0" || escalation.CandidateVersion == nil || *escalation.CandidateVersion != "0.2.0" {
		t.Fatalf("old adapter version must stay active during re-consent: err=%v state=%+v body=%s", err, escalation, escalated.Body)
	}
	installationState := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/extensions/installations", nil, []*http.Cookie{cookie}, baseURL)
	if installationState.Code != http.StatusOK || !strings.Contains(installationState.Body, staged.InstallationID) || !strings.Contains(installationState.Body, "UPDATING_PENDING_RECONSENT") {
		t.Fatalf("candidate must remain pending permission re-consent while the old version stays active: got %d: %s", installationState.Code, installationState.Body)
	}
	reconsented := liveJSONRequestWithHeaders(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/"+staged.InstallationID+"/approve-permissions", liveApprovePermissionsRequest{
		EnableImmediately: true, AcknowledgeUnverifiedExternal: true,
	}, []*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": escalated.ETag}, loginResult.CSRFToken)
	if reconsented.Code != http.StatusOK {
		t.Fatalf("re-consent to live permission expansion: got %d: %s", reconsented.Code, reconsented.Body)
	}
	close(trafficStop)
	<-trafficDone
	select {
	case trafficErr := <-trafficErrors:
		t.Fatalf("traffic was interrupted while the candidate awaited re-consent: %v", trafficErr)
	default:
	}
	if successfulTraffic.Load() == 0 {
		t.Fatal("no requests completed successfully while the candidate awaited re-consent")
	}

	// A permission-preserving version activates without asking for another
	// re-consent, and rollback restores the preceding version.
	manifestV21 := strings.Replace(manifestV2, `version: "0.2.0"`, `version: "0.2.1"`, 1)
	if err := os.WriteFile(manifestPath, []byte(manifestV21), 0o600); err != nil {
		t.Fatal(err)
	}
	archiveV21 := filepath.Join(marketplaceDir, "live-routing-adapter-v2.1.tpkg")
	packV21 := exec.Command(binary, "plugin", "pack", extensionDir, "--out", archiveV21)
	packV21.Dir = work
	if output, packErr := packV21.CombinedOutput(); packErr != nil {
		t.Fatalf("pack non-escalating live adapter: %v\n%s", packErr, output)
	}
	upgraded := liveJSONRequestWithHeaders(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/"+staged.InstallationID+"/upgrade", map[string]string{
		"source_uri": "tanpopo-local://live-routing-adapter-v2.1.tpkg", "expected_sha256": fileSHA256(t, archiveV21),
	}, []*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": reconsented.ETag}, loginResult.CSRFToken)
	if upgraded.Code != http.StatusOK || !strings.Contains(upgraded.Body, `"active_version":"0.2.1"`) {
		t.Fatalf("permission-preserving update must activate immediately: got %d: %s", upgraded.Code, upgraded.Body)
	}
	rolledBack := liveJSONRequestWithHeaders(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/"+staged.InstallationID+"/rollback", liveRollbackExtensionRequest{Reason: "Acceptance rollback verification"},
		[]*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": upgraded.ETag}, loginResult.CSRFToken)
	if rolledBack.Code != http.StatusOK || !strings.Contains(rolledBack.Body, `"active_version":"0.2.0"`) {
		t.Fatalf("live rollback must restore the previous pinned adapter: got %d: %s", rolledBack.Code, rolledBack.Body)
	}
	assertCore("re-consent, non-escalating upgrade, and rollback")

	quarantined := liveJSONRequestWithHeaders(t, client, http.MethodPost, baseURL+"/admin/v1/extensions/"+staged.InstallationID+"/quarantine", liveRollbackExtensionRequest{Reason: "Acceptance quarantine verification"},
		[]*http.Cookie{cookie}, baseURL, map[string]string{"If-Match": rolledBack.ETag}, loginResult.CSRFToken)
	if quarantined.Code != http.StatusOK || !strings.Contains(quarantined.Body, `"state":"QUARANTINED"`) {
		t.Fatalf("quarantine live adapter: got %d: %s", quarantined.Code, quarantined.Body)
	}
	blocked, err := liveIngressJSON(client, baseURL+"/v1/chat/completions", keyResult.SecretKey, map[string]any{
		"model": "accept-eu-only", "stream": false, "messages": []map[string]string{{"role": "user", "content": "request after quarantine"}},
	})
	if err != nil {
		t.Fatalf("request after quarantine: %v", err)
	}
	blockedBody, _ := io.ReadAll(blocked.Body)
	_ = blocked.Body.Close()
	if blocked.StatusCode != http.StatusServiceUnavailable || (!strings.Contains(string(blockedBody), `"extension_quarantined"`) && !strings.Contains(string(blockedBody), `"no_compliant_deployment_available"`)) {
		t.Fatalf("quarantined adapter must reject new inference with a fail-closed 503: got %d: %s", blocked.StatusCode, blockedBody)
	}
	if got := usRequests.Load(); got != 0 {
		t.Fatalf("quarantine or EU residency failure must not fall back to the US provider, which received %d requests", got)
	}
	assertCore("quarantine blocks new inference")
}

func hostPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse fake provider URL %q: %v", rawURL, err)
	}
	return u.Host
}

func liveIngressJSON(client *http.Client, target, apiKey string, body any) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(string(encoded)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("X-Tanpopo-Data-Region", "EU")
	return client.Do(req)
}
