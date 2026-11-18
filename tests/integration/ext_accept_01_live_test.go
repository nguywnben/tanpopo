// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package integration_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

// TestEXTAccept01_LiveBootstrapAuthenticationAndFailClosedInstall exercises
// authenticated behavior against the actual single-binary serve process. It
// proves bootstrap-token consumption, owner login/session, CSRF enforcement,
// and that installation cannot return success when the live runtime has no
// archive resolver. It does not claim the extension lifecycle or routing
// acceptance steps that require an archive resolver and configured providers.
func TestEXTAccept01_LiveBootstrapAuthenticationAndFailClosedInstall(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("locate Tanpopo module: %v", err)
	}
	work := t.TempDir()
	binary := filepath.Join(work, "tanpopo")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Tanpopo binary: %v\n%s", err, output)
	}
	initialSHA := fileSHA256(t, binary)

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
	logFile, err := os.Create(filepath.Join(work, "serve.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	server := exec.Command(binary, "serve", "--role=all", "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	server.Dir = root
	server.Env = append(os.Environ(),
		"TANPOPO_DATA_DIR="+dataDir,
		"TANPOPO_MASTER_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"TANPOPO_MASTER_KEY_FILE=",
		"TANPOPO_TEST_HERMETIC=1",
	)
	server.Stdout, server.Stderr = logFile, logFile
	if err := server.Start(); err != nil {
		t.Fatalf("start live Tanpopo process: %v", err)
	}
	pid := server.Process.Pid
	t.Cleanup(func() {
		if server.ProcessState == nil {
			_ = server.Process.Kill()
		}
		_ = server.Wait()
	})

	client := netguard.NewHTTPClient()
	client.Timeout = 3 * time.Second
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
			logBytes, _ := os.ReadFile(filepath.Join(work, "serve.log"))
			t.Fatalf("live server did not become ready (pid=%d): %s", pid, logBytes)
		}
		time.Sleep(40 * time.Millisecond)
	}

	assertProcessInvariant := func(label string) {
		t.Helper()
		if server.ProcessState != nil {
			t.Fatalf("%s: Core exited unexpectedly (pid=%d)", label, pid)
		}
		if server.Process.Pid != pid {
			t.Fatalf("%s: Core PID changed: got %d, want %d", label, server.Process.Pid, pid)
		}
		if got := fileSHA256(t, binary); got != initialSHA {
			t.Fatalf("%s: Core binary SHA-256 changed: got %s, want %s", label, got, initialSHA)
		}
	}
	assertProcessInvariant("ready")

	status := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/bootstrap/status", nil, nil, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body, `"initialized":false`) {
		t.Fatalf("bootstrap status: want uninitialized 200, got %d: %s", status.Code, status.Body)
	}

	tokenBytes, err := os.ReadFile(filepath.Join(dataDir, "bootstrap.token"))
	if err != nil {
		t.Fatalf("read one-time bootstrap token from private test data dir: %v", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		t.Fatal("server created an empty bootstrap token")
	}

	bootstrapBody := map[string]any{
		"bootstrap_token": token,
		"owner_email": "owner-ext-accept@example.test",
		"owner_password": "StrongLiveOwner#2031!",
		"org_name": "EXT Accept Live Org",
		"project_name": "EXT Accept Live Project",
		"master_kek_backup_acknowledged": true,
	}
	boot := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/bootstrap/complete", bootstrapBody, nil, baseURL)
	if boot.Code != http.StatusCreated {
		t.Fatalf("bootstrap completion: want 201, got %d: %s", boot.Code, boot.Body)
	}
	var bootResult struct {
		OrgID string `json:"org_id"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal([]byte(boot.Body), &bootResult); err != nil || bootResult.OrgID == "" || bootResult.CSRFToken == "" {
		t.Fatalf("bootstrap response lacks owner session contract: err=%v body=%s", err, boot.Body)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bootstrap.token")); !os.IsNotExist(err) {
		t.Fatalf("successful bootstrap must consume and remove the token file; stat err=%v", err)
	}
	assertProcessInvariant("bootstrap")

	unauth := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/auth/session", nil, nil, "")
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated session request: want 401, got %d: %s", unauth.Code, unauth.Body)
	}

	login := liveJSONRequest(t, client, http.MethodPost, baseURL+"/admin/v1/auth/login", map[string]string{
		"email": "owner-ext-accept@example.test",
		"password": "StrongLiveOwner#2031!",
		"org_id": bootResult.OrgID,
	}, nil, baseURL)
	if login.Code != http.StatusOK {
		t.Fatalf("owner login: want 200, got %d: %s", login.Code, login.Body)
	}
	loginCookie := findLiveSessionCookie(t, login.Cookies)
	var loginResult struct {
		PrincipalID string `json:"principal_id"`
		OrgID *string `json:"org_id"`
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal([]byte(login.Body), &loginResult); err != nil || loginResult.PrincipalID == "" || loginResult.CSRFToken == "" || loginResult.OrgID == nil || *loginResult.OrgID != bootResult.OrgID {
		t.Fatalf("login response does not identify bootstrapped owner: err=%v body=%s", err, login.Body)
	}
	assertProcessInvariant("login")

	session := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/auth/session", nil, []*http.Cookie{loginCookie}, baseURL)
	if session.Code != http.StatusOK || !strings.Contains(session.Body, loginResult.PrincipalID) {
		t.Fatalf("authenticated session: want owner session 200, got %d: %s", session.Code, session.Body)
	}

	blastRadius := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/blast-radius?target_type=EXTENSION&target_id=uninstalled-test-extension", nil, []*http.Cookie{loginCookie}, baseURL)
	if blastRadius.Code != http.StatusOK {
		t.Fatalf("authenticated empty extension blast-radius query: want 200, got %d: %s", blastRadius.Code, blastRadius.Body)
	}
	var blastResult struct {
		Organizations []string `json:"organizations"`
		Projects      []string `json:"projects"`
		VirtualKeys   []string `json:"virtual_keys"`
		Connections   []string `json:"connections"`
	}
	if err := json.Unmarshal([]byte(blastRadius.Body), &blastResult); err != nil || blastResult.Organizations == nil || blastResult.Projects == nil || blastResult.VirtualKeys == nil || blastResult.Connections == nil {
		t.Fatalf("blast-radius response must return all four initialized scopes: err=%v body=%s", err, blastRadius.Body)
	}

	installURL := baseURL + "/admin/v1/extensions/install"
	installBody := map[string]string{
		"source_uri": "https://marketplace.tanpopo.dev/acme-0.1.0.tpkg",
		"expected_sha256": strings.Repeat("a", 64),
	}
	missingCSRF := liveJSONRequest(t, client, http.MethodPost, installURL, installBody, []*http.Cookie{loginCookie}, baseURL)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("authenticated state-changing request without CSRF: want 403, got %d: %s", missingCSRF.Code, missingCSRF.Body)
	}
	assertProcessInvariant("CSRF rejection")

	// The production binary currently constructs the lifecycle service without an
	// archive resolver. Confirm the authenticated, CSRF-valid request fails closed
	// instead of recording a staged installation or returning a false success.
	withCSRF := liveJSONRequest(t, client, http.MethodPost, installURL, installBody, []*http.Cookie{loginCookie}, baseURL, loginResult.CSRFToken)
	if withCSRF.Code != http.StatusServiceUnavailable {
		t.Fatalf("install without configured archive resolver: want fail-closed 503, got %d: %s", withCSRF.Code, withCSRF.Body)
	}
	assertProcessInvariant("fail-closed install")

	installations := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/extensions/installations", nil, []*http.Cookie{loginCookie}, baseURL)
	if installations.Code != http.StatusOK {
		t.Fatalf("list installations after rejected install: want 200, got %d: %s", installations.Code, installations.Body)
	}
	if strings.Contains(installations.Body, "acme-0.1.0") {
		t.Fatalf("rejected install created an installation record: %s", installations.Body)
	}
	assertProcessInvariant("post-install state check")

	finalStatus := liveJSONRequest(t, client, http.MethodGet, baseURL+"/admin/v1/bootstrap/status", nil, nil, "")
	if finalStatus.Code != http.StatusOK || !strings.Contains(finalStatus.Body, `"initialized":true`) {
		t.Fatalf("bootstrap final state: want initialized 200, got %d: %s", finalStatus.Code, finalStatus.Body)
	}
	assertProcessInvariant("end of covered live flow")
}

type liveResponse struct {
	Code int
	Body string
	Cookies []*http.Cookie
}

func liveJSONRequest(t *testing.T, client *http.Client, method, target string, body any, cookies []*http.Cookie, origin string, csrf ...string) liveResponse {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode %s request: %v", target, err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, target, payload)
	if err != nil {
		t.Fatalf("create %s request: %v", target, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if len(csrf) > 0 && csrf[0] != "" {
		req.Header.Set("X-CSRF-Token", csrf[0])
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request %s %s: %v", method, target, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", target, err)
	}
	return liveResponse{Code: resp.StatusCode, Body: string(raw), Cookies: resp.Cookies()}
}

func findLiveSessionCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == "__Host-tanpopo_admin_session" {
			if !cookie.Secure || !cookie.HttpOnly {
				t.Fatalf("admin session cookie lacks Secure/HttpOnly attributes: %+v", cookie)
			}
			return cookie
		}
	}
	t.Fatalf("login did not set __Host-tanpopo_admin_session cookie")
	return nil
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read binary %s: %v", path, err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
