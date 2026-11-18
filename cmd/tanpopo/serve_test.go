// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"context"
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

func TestDataPlaneRoleDoesNotMountControlPlaneOrMintBootstrapToken(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	t.Setenv("TANPOPO_DATA_DIR", dataDir)
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	handler, closeRuntime, err := buildRuntime(context.Background(), "data-plane", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("build data-plane runtime: %v", err)
	}
	defer closeRuntime()
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
