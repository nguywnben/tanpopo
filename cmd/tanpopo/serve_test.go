// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/storage/sqlite"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestDrainTimeoutFromEnvIsBounded(t *testing.T) {
	tests := []struct {
		name  string
		value string
		ok    bool
		want  time.Duration
	}{
		{name: "default", want: defaultDrainTimeout, ok: true},
		{name: "zero disables wait", value: "0s", want: 0, ok: true},
		{name: "configured", value: "2m", want: 2 * time.Minute, ok: true},
		{name: "maximum", value: "300s", want: maxDrainTimeout, ok: true},
		{name: "above maximum", value: "301s"},
		{name: "negative", value: "-1s"},
		{name: "invalid", value: "later"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(string) (string, bool) {
				if tt.name == "default" {
					return "", false
				}
				return tt.value, true
			}
			got, err := drainTimeoutFromEnv(lookup)
			if (err == nil) != tt.ok {
				t.Fatalf("drainTimeoutFromEnv() err=%v, want success=%t", err, tt.ok)
			}
			if err == nil && got != tt.want {
				t.Fatalf("drainTimeoutFromEnv()=%s, want %s", got, tt.want)
			}
		})
	}
}

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestUnifiedRuntimeRoutesDataPlaneAdminAndEmbeddedConsole(t *testing.T) {
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	handler, closeRuntime, err := buildRuntime(context.Background(), "all", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("build unified runtime: %v", err)
	}
	defer closeRuntime()

	ui := httptest.NewRecorder()
	handler.ServeHTTP(ui, httptest.NewRequest(http.MethodGet, "/", nil))
	if ui.Code != http.StatusOK || ui.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("embedded console unavailable: status=%d headers=%v", ui.Code, ui.Header())
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("health probe %s returned %d: %s", path, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"/admin/v1/overview/summary", "/v1/models"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Header().Get("Content-Type") == "text/html" {
			t.Fatalf("API path %s fell through to SPA: status=%d", path, rec.Code)
		}
		if rec.Code == http.StatusNotFound {
			t.Fatalf("API surface %s was not mounted: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/admin/v1/not-a-route", nil))
	if unknown.Header().Get("Content-Type") == "text/html" {
		t.Fatal("unknown Admin API path fell through to the console")
	}
}

func TestRuntimeWithoutPublicOAuthRedirectStillServesAndFailsOAuthClosed(t *testing.T) {
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	t.Setenv("TANPOPO_TEST_HERMETIC", "0")
	t.Setenv("TANPOPO_OAUTH_REDIRECT_URI", "")

	handler, closeRuntime, err := buildRuntime(context.Background(), "all", "0.0.0.0:8080")
	if err != nil {
		t.Fatalf("build public runtime without OAuth callback configuration: %v", err)
	}
	defer closeRuntime()

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("public runtime readiness returned %d: %s", ready.Code, ready.Body.String())
	}

	callback := httptest.NewRecorder()
	handler.ServeHTTP(callback, httptest.NewRequest(http.MethodGet, "/admin/v1/oauth/callback", nil))
	if callback.Code != http.StatusServiceUnavailable || !strings.Contains(callback.Body.String(), "oauth_unavailable") {
		t.Fatalf("OAuth callback without explicit HTTPS URI returned %d: %s", callback.Code, callback.Body.String())
	}
}

func TestReadinessFailsImmediatelyAfterDrainStartsButLivenessStaysHealthy(t *testing.T) {
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	ctx, cancel := context.WithCancel(context.Background())
	handler, closeRuntime, err := buildRuntime(ctx, "all", "127.0.0.1:8080")
	if err != nil {
		cancel()
		t.Fatalf("build runtime: %v", err)
	}
	defer closeRuntime()
	cancel()

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness returned %d after drain began", ready.Code)
	}
	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("liveness returned %d while process was draining", live.Code)
	}
}

func TestGracefulShutdownLetsActiveSSEStreamFinish(t *testing.T) {
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	runtimeCtx, cancelRuntime := context.WithCancel(context.Background())
	appHandler, closeRuntime, err := buildRuntime(runtimeCtx, "all", "127.0.0.1:8080")
	if err != nil {
		cancelRuntime()
		t.Fatalf("build runtime: %v", err)
	}
	t.Cleanup(func() {
		cancelRuntime()
		closeRuntime()
	})

	streamStarted := make(chan struct{})
	finishStream := make(chan struct{})
	handler := http.NewServeMux()
	handler.HandleFunc("/test/stream", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if _, writeErr := io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"in progress\"}}]}\n\n"); writeErr != nil {
			return
		}
		w.(http.Flusher).Flush()
		close(streamStarted)
		<-finishStream
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	})
	handler.Handle("/", appHandler)
	server := httptest.NewUnstartedServer(handler)
	server.Start()
	t.Cleanup(server.Close)

	client := netguard.NewHTTPClient()
	client.Timeout = 5 * time.Second
	t.Cleanup(client.CloseIdleConnections)
	streamResult := make(chan struct {
		body string
		err  error
	}, 1)
	go func() {
		resp, getErr := client.Get(server.URL + "/test/stream")
		if getErr != nil {
			streamResult <- struct {
				body string
				err  error
			}{err: getErr}
			return
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		streamResult <- struct {
			body string
			err  error
		}{body: string(body), err: readErr}
	}()
	select {
	case <-streamStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE handler did not emit its first frame")
	}

	cancelRuntime()
	ready, err := client.Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("request readiness while draining: %v", err)
	}
	_ = ready.Body.Close()
	if ready.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness during drain=%d, want %d", ready.StatusCode, http.StatusServiceUnavailable)
	}

	shutdownDone := make(chan error, 1)
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelShutdown()
	go func() { shutdownDone <- server.Config.Shutdown(shutdownCtx) }()
	select {
	case err := <-shutdownDone:
		t.Fatalf("graceful shutdown returned before active SSE stream finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(finishStream)
	select {
	case result := <-streamResult:
		if result.err != nil {
			t.Fatalf("read drained SSE stream: %v", result.err)
		}
		if !strings.Contains(result.body, "in progress") || !strings.Contains(result.body, "data: [DONE]") {
			t.Fatalf("graceful drain did not preserve stream frames and terminal marker: %q", result.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SSE stream did not finish after release")
	}
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("graceful shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("graceful shutdown did not complete after the SSE stream finished")
	}
}

func TestControlPlaneRoleDoesNotMountDataPlane(t *testing.T) {
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	handler, closeRuntime, err := buildRuntime(context.Background(), "control-plane", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("build control-plane runtime: %v", err)
	}
	defer closeRuntime()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") == "text/html" {
		t.Fatalf("control-plane role exposed a Data Plane handler: status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestReadinessFailsWhenVaultOrPluginStorageIsUnavailable(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	handler, closeRuntime, err := buildRuntime(context.Background(), "all", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("build runtime: %v", err)
	}
	pluginsDir := filepath.Join(dataDir, "plugins")
	if err := os.Remove(pluginsDir); err != nil {
		closeRuntime()
		t.Fatalf("remove plugin storage: %v", err)
	}
	probe := httptest.NewRecorder()
	handler.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	closeRuntime()
	if probe.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness returned %d while plugin storage was missing", probe.Code)
	}
}

func TestReadinessRejectsWrongButValidMasterKEK(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dataDir, "tanpopo.db")
	ctx := context.Background()
	store, err := sqlite.Open(ctx, sqlite.Config{DBPath: dbPath})
	if err != nil {
		t.Fatalf("open setup database: %v", err)
	}
	org, err := store.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "readiness-org", DisplayName: "Readiness Org"})
	if err != nil {
		_ = store.Close()
		t.Fatalf("create organization: %v", err)
	}
	correctKey, err := vault.ParseMasterKeyString("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		_ = store.Close()
		t.Fatalf("parse setup key: %v", err)
	}
	kek, err := vault.NewStaticKEKProvider("kek_v1", correctKey)
	clear(correctKey)
	if err != nil {
		_ = store.Close()
		t.Fatalf("create setup KEK: %v", err)
	}
	keyVault, err := vault.NewStore(vault.Config{DB: store.RawDB(), KEK: kek, PostShredHook: store.CheckpointWAL})
	if err != nil {
		kek.Close()
		_ = store.Close()
		t.Fatalf("create setup vault: %v", err)
	}
	if _, err := keyVault.EnsureOrgDEK(ctx, org.ID); err != nil {
		keyVault.Close()
		kek.Close()
		_ = store.Close()
		t.Fatalf("persist Org DEK: %v", err)
	}
	keyVault.Close()
	kek.Close()
	if err := store.Close(); err != nil {
		t.Fatalf("close setup database: %v", err)
	}

	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	handler, closeRuntime, err := buildRuntime(ctx, "all", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("build runtime with syntactically valid wrong key: %v", err)
	}
	defer closeRuntime()
	probe := httptest.NewRecorder()
	handler.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if probe.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness returned %d with wrong Master KEK", probe.Code)
	}
}

func TestServeRefusesInsecureMasterKEKFileBeforeListening(t *testing.T) {
	if runtime.GOOS == "windows" {
		return // Windows uses ACL-based key-file checks, covered by platform-specific tests.
	}
	root := t.TempDir()
	keyPath := filepath.Join(root, "master.key")
	if err := os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", keyPath)
	if err := serve(context.Background(), "all", "127.0.0.1:0"); !errors.Is(err, vault.ErrInsecureKeyfilePermissions) {
		t.Fatalf("serve should reject insecure Master KEK permissions before binding a listener, got %v", err)
	}
}

func TestDataPlaneRoleDoesNotMountControlPlaneOrMintBootstrapToken(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	handler, closeRuntime, err := buildRuntime(context.Background(), "data-plane", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("build data-plane runtime: %v", err)
	}
	t.Cleanup(func() {
		closeRuntime()
		handler = nil
		// modernc SQLite's Windows file handle can outlive sql.DB.Close until the
		// driver's finalizable connection wrapper is collected.
		runtime.GC()
		// Windows runners can briefly retain a SQLite file handle after database
		// Close returns. Allow the OS's file-handle teardown to settle, while still
		// failing if the runtime keeps the database connection open. Re-collect
		// finalizable modernc wrappers during the bounded retry window; one forced
		// collection before the loop is not deterministic on Windows.
		var removeErr error
		for attempt := 0; attempt < 600; attempt++ {
			if attempt > 0 && attempt%10 == 0 {
				runtime.GC()
			}
			removeErr = os.RemoveAll(dataDir)
			if removeErr == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Errorf("runtime left data directory locked after close: %v", removeErr)
	})
	for _, path := range []string{"/admin/v1/unknown", "/"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") == "text/html" {
			t.Fatalf("data-plane role exposed control-plane route %s: status=%d type=%q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "bootstrap.token")); !os.IsNotExist(err) {
		t.Fatalf("data-plane role created an unused bootstrap token: err=%v", err)
	}
}

func TestStandaloneBinaryServesWithoutSourceAssets(t *testing.T) {
	buildDir := t.TempDir()
	binaryName := "tanpopo"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(buildDir, binaryName)
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build standalone binary: %v\n%s", err, output)
	}
	emptyDir := filepath.Join(t.TempDir(), "empty")
	if err := os.Mkdir(emptyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	standalone := filepath.Join(emptyDir, binaryName)
	if err := copyFile(binaryPath, standalone); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(emptyDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != binaryName {
		t.Fatalf("standalone directory has extra files: %v err=%v", entries, err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dataDir := filepath.Join(t.TempDir(), "instance")
	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	command := exec.CommandContext(ctx, standalone, "serve", "--listen", address)
	command.Dir = emptyDir
	command.Env = os.Environ()
	var logs strings.Builder
	command.Stdout = io.Discard
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	client := netguard.NewHTTPClient()
	baseURL := "http://" + address
	var ready bool
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		resp, requestErr := client.Get(baseURL + "/")
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("standalone server did not start: %v; logs=%s", ctx.Err(), logs.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatalf("standalone server did not become ready; logs=%s", logs.String())
	}
	for _, path := range []string{"/admin/v1/unknown", "/v1/models"} {
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Fatalf("standalone API path %s fell through to embedded UI", path)
		}
	}
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
