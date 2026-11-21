// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	valkeygo "github.com/valkey-io/valkey-go"

	admincli "github.com/nguywnben/tanpopo/internal/cli/admin"
	vkcluster "github.com/nguywnben/tanpopo/internal/cluster/valkey"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/postgres"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestTPIMP0040GracefulDrainReadyzAndSSE(t *testing.T) {
	t.Setenv("TANPOPO_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	t.Setenv("TANPOPO_MASTER_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	t.Setenv("TANPOPO_DRAIN_TIMEOUT", "5s")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var draining atomic.Bool
	baseHandler, closeRuntime, err := buildRuntimeInternal(ctx, "all", "127.0.0.1:0", nil, &draining)
	if err != nil {
		t.Fatalf("buildRuntimeInternal: %v", err)
	}
	defer func() { _ = closeRuntime() }()

	// Wrap with a deterministic streaming endpoint to prove /readyz switches 503
	// immediately on drain while open streams finish cleanly before Shutdown returns.
	streamStarted := make(chan struct{})
	releaseStream := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/test-sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "missing flusher", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, "data: chunk1\n\n")
		flusher.Flush()
		close(streamStarted)
		<-releaseStream
		_, _ = fmt.Fprint(w, "data: chunk2\n\n")
		flusher.Flush()
	})
	mux.Handle("/", baseHandler)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.Serve(ln) }()

	httpClient := netguard.NewHTTPClient()
	httpClient.Timeout = 10 * time.Second
	baseURL := "http://" + ln.Addr().String()
	// Confirm ready before drain.
	resp, err := httpClient.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /readyz before drain, got %d", resp.StatusCode)
	}

	// Open active SSE stream.
	streamErrCh := make(chan error, 1)
	var bodyBuf bytes.Buffer
	go func() {
		sresp, serr := httpClient.Get(baseURL + "/test-sse")
		if serr != nil {
			streamErrCh <- serr
			return
		}
		defer sresp.Body.Close()
		_, copyErr := io.Copy(&bodyBuf, sresp.Body)
		streamErrCh <- copyErr
	}()
	<-streamStarted

	// Simulate SIGTERM drain transition.
	draining.Store(true)
	respDrain, err := httpClient.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz during drain: %v", err)
	}
	_ = respDrain.Body.Close()
	if respDrain.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on /readyz during drain, got %d", respDrain.StatusCode)
	}

	shutdownDone := make(chan error, 1)
	go func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		shutdownDone <- srv.Shutdown(sctx)
	}()

	// Release final SSE chunk after shutdown has started.
	time.Sleep(100 * time.Millisecond)
	close(releaseStream)

	if serr := <-streamErrCh; serr != nil {
		t.Fatalf("active SSE stream interrupted during graceful drain: %v", serr)
	}
	if !strings.Contains(bodyBuf.String(), "data: chunk1") || !strings.Contains(bodyBuf.String(), "data: chunk2") {
		t.Fatalf("incomplete SSE stream payload: %q", bodyBuf.String())
	}
	if sdErr := <-shutdownDone; sdErr != nil {
		t.Fatalf("server.Shutdown failed: %v", sdErr)
	}
	<-srvDone
}

func TestTPIMP0040MultiNodeClusterPG16AndValkeyAC1ToAC4(t *testing.T) {
	pgDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_DSN"))
	valkeyAddrsRaw := strings.TrimSpace(os.Getenv("TANPOPO_TEST_VALKEY_ADDRS"))
	if pgDSN == "" || valkeyAddrsRaw == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Create an isolated temporary PG16 database when admin DSN is present so every test run is clean & repeatable.
	effectiveDSN, cleanupDB := prepareIsolatedPGDatabase(t, pgDSN)
	defer cleanupDB()

	tmpRoot := t.TempDir()
	kekHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	bootstrapFile := filepath.Join(tmpRoot, "bootstrap.token")

	t.Setenv("TANPOPO_STORAGE_ENGINE", "postgres")
	t.Setenv("TANPOPO_PG_DSN", effectiveDSN)
	t.Setenv("TANPOPO_VALKEY_ADDRS", valkeyAddrsRaw)
	t.Setenv("TANPOPO_VALKEY_REPLICAS", "3")
	t.Setenv("TANPOPO_VALKEY_REQUIRE_TLS", "false")
	t.Setenv("TANPOPO_MASTER_KEY", kekHex)
	t.Setenv("TANPOPO_MASTER_KEY_FILE", "")
	t.Setenv("TANPOPO_DISABLE_VALKEY_PUBSUB", "")
	t.Setenv("TANPOPO_TEST_HERMETIC", "1")
	t.Setenv("TANPOPO_DRAIN_TIMEOUT", "10s")

	// Start 1 Control Plane node + 3 Data Plane nodes sharing PG16 & 3-shard Valkey Cluster.
	cpURL, cpClose := startNodeServer(t, "control-plane", filepath.Join(tmpRoot, "cp"), bootstrapFile, false)
	defer cpClose()

	dpNodes := make([]*nodeServerHandle, 3)
	for i := 0; i < 3; i++ {
		dpNodes[i] = startDataPlaneNode(t, fmt.Sprintf("dp-%d", i+1), filepath.Join(tmpRoot, fmt.Sprintf("dp-%d", i+1)), false)
	}
	defer func() {
		for _, n := range dpNodes {
			if n != nil && n.closeFn != nil {
				n.closeFn()
			}
		}
	}()

	// Verify /readyz on all 4 nodes.
	client := netguard.NewHTTPClient()
	client.Timeout = 15 * time.Second
	assertReady(t, client, cpURL)
	for _, dp := range dpNodes {
		assertReady(t, client, dp.url)
	}

	// Bootstrap & authenticate on Control Plane Admin API.
	bootTokenRaw, err := os.ReadFile(bootstrapFile)
	if err != nil {
		t.Fatalf("read bootstrap token: %v", err)
	}
	bootResp := bootstrapViaHTTP(t, client, cpURL, strings.TrimSpace(string(bootTokenRaw)))
	csrfToken, adminCookies := loginViaHTTP(t, client, cpURL, bootResp.OrgID)

	// Create Virtual Key K1 and wait for initial cluster sync (< 2.5s).
	key1 := createVKeyViaHTTP(t, client, cpURL, csrfToken, adminCookies, bootResp.OrgID, bootResp.ProjectID, bootResp.EnvID, "cluster-k1")
	for _, dp := range dpNodes {
		waitForVKeyStatus(t, client, dp.url, key1.SecretKey, http.StatusOK, 4*time.Second)
	}

	// AC-1 Part A: Emergency revoke K1 via Control Plane -> all 3 Data Plane nodes reject (< 1s via Layer 1 Valkey Pub/Sub).
	revokeStart := time.Now()
	revokeVKeyViaHTTP(t, client, cpURL, csrfToken, adminCookies, bootResp.OrgID, key1.ID, key1.Version, "EMERGENCY_ABORT_STREAMS")
	for _, dp := range dpNodes {
		waitForVKeyStatus(t, client, dp.url, key1.SecretKey, http.StatusUnauthorized, 1500*time.Millisecond)
	}
	if elapsed := time.Since(revokeStart); elapsed >= time.Second {
		t.Fatalf("AC-1 violation: Valkey Pub/Sub revocation took %v across 3 nodes (want < 1s)", elapsed)
	}

	// AC-1 Part B: Disable Valkey Pub/Sub on dp-fallback -> verify < 3s propagation via Layer 2 PG LISTEN/NOTIFY + poll.
	dpFallback := startDataPlaneNode(t, "dp-pg-fallback", filepath.Join(tmpRoot, "dp-pg"), true)
	defer dpFallback.closeFn()
	assertReady(t, client, dpFallback.url)

	key2 := createVKeyViaHTTP(t, client, cpURL, csrfToken, adminCookies, bootResp.OrgID, bootResp.ProjectID, bootResp.EnvID, "cluster-k2-pg")
	waitForVKeyStatus(t, client, dpFallback.url, key2.SecretKey, http.StatusOK, 4*time.Second)

	fallbackRevokeStart := time.Now()
	revokeVKeyViaHTTP(t, client, cpURL, csrfToken, adminCookies, bootResp.OrgID, key2.ID, key2.Version, "EMERGENCY_ABORT_STREAMS")
	waitForVKeyStatus(t, client, dpFallback.url, key2.SecretKey, http.StatusUnauthorized, 3*time.Second)
	if elapsed := time.Since(fallbackRevokeStart); elapsed >= 3*time.Second {
		t.Fatalf("AC-1 fallback violation: PG LISTEN/NOTIFY + poll revocation took %v (want < 3s)", elapsed)
	}

	// AC-2 (FMEA-07): Partitioned node marks /readyz=503 & ErrClusterPartitioned after >10s unreachable.
	verifyFMEA07PartitionCircuitBreaker(t, ctx, effectiveDSN, strings.Split(valkeyAddrsRaw, ","))

	// AC-3: CR-07 Zero CROSSSLOT across 3-shard Valkey Cluster + FLUSHALL + DR-02 verify-state rebuild.
	verifyAC3CrossSlotAndFlushallRecovery(t, ctx, effectiveDSN, strings.Split(valkeyAddrsRaw, ","), bootResp.OrgID, bootResp.ProjectID, bootResp.EnvID, key1.ID)

	// AC-4 (OPS-AC-06): 100 active concurrent SSE streams across 3 Data Plane nodes during staggered rolling restart with 0 client errors.
	verifyAC4RollingRestart100SSEStreams(t, client, dpNodes)
}

type nodeServerHandle struct {
	url      string
	draining *atomic.Bool
	server   *http.Server
	closeFn  func()
}

func startNodeServer(t *testing.T, role, dir, bootstrapFile string, disableValkeySub bool) (string, func()) {
	t.Helper()
	t.Setenv("TANPOPO_DATA_DIR", dir)
	if bootstrapFile != "" {
		t.Setenv("TANPOPO_BOOTSTRAP_TOKEN_FILE", bootstrapFile)
	} else {
		t.Setenv("TANPOPO_BOOTSTRAP_TOKEN_FILE", filepath.Join(dir, "bootstrap.token"))
	}
	if disableValkeySub {
		t.Setenv("TANPOPO_DISABLE_VALKEY_PUBSUB", "1")
	} else {
		t.Setenv("TANPOPO_DISABLE_VALKEY_PUBSUB", "")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen %s: %v", role, err)
	}
	var draining atomic.Bool
	handler, closeRuntime, err := buildRuntimeInternal(context.Background(), role, ln.Addr().String(), nil, &draining)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("buildRuntimeInternal(%s): %v", role, err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()
	return "http://" + ln.Addr().String(), func() {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(sctx)
		cancel()
		<-done
		_ = closeRuntime()
	}
}

func startDataPlaneNode(t *testing.T, nodeID, dir string, disableValkeySub bool) *nodeServerHandle {
	t.Helper()
	t.Setenv("TANPOPO_DATA_DIR", dir)
	t.Setenv("TANPOPO_NODE_ID", nodeID)
	t.Setenv("TANPOPO_BOOTSTRAP_TOKEN_FILE", filepath.Join(dir, "bootstrap.token"))
	if disableValkeySub {
		t.Setenv("TANPOPO_DISABLE_VALKEY_PUBSUB", "1")
	} else {
		t.Setenv("TANPOPO_DISABLE_VALKEY_PUBSUB", "")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen %s: %v", nodeID, err)
	}
	var draining atomic.Bool
	baseHandler, closeRuntime, err := buildRuntimeInternal(context.Background(), "data-plane", ln.Addr().String(), nil, &draining)
	if err != nil {
		_ = ln.Close()
		t.Fatalf("buildRuntimeInternal(%s): %v", nodeID, err)
	}

	// Mount /v1/test-sse on the same HTTP server to benchmark 100 active SSE streams during rolling drain (AC-4).
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/test-sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		for i := 0; i < 6; i++ {
			_, _ = fmt.Fprintf(w, "data: {\"seq\":%d}\n\n", i)
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	mux.Handle("/", baseHandler)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ln)
	}()
	return &nodeServerHandle{
		url:      "http://" + ln.Addr().String(),
		draining: &draining,
		server:   srv,
		closeFn: func() {
			draining.Store(true)
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = srv.Shutdown(sctx)
			cancel()
			<-done
			_ = closeRuntime()
		},
	}
}

func assertReady(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/readyz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("node %s did not become /readyz 200 in time", baseURL)
}

type bootstrapHTTPResp struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	EnvID     string `json:"env_id"`
}

func bootstrapViaHTTP(t *testing.T, client *http.Client, cpURL, token string) bootstrapHTTPResp {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"bootstrap_token":                token,
		"org_name":                       "Cluster Org",
		"project_name":                   "Default Project",
		"owner_email":                    "owner@cluster.example",
		"owner_password":                 "CorrectHorseBatteryStaple!2026",
		"master_kek_backup_acknowledged": true,
	})
	resp, err := client.Post(cpURL+"/admin/v1/bootstrap/complete", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST bootstrap: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		OrgID     string `json:"org_id"`
		ProjectID string `json:"project_id"`
		EnvID     string `json:"env_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode bootstrap: %v (%s)", err, string(raw))
	}
	if out.OrgID == "" || out.ProjectID == "" || out.EnvID == "" {
		t.Fatalf("missing bootstrap topology IDs in response: %s", string(raw))
	}
	return bootstrapHTTPResp{OrgID: out.OrgID, ProjectID: out.ProjectID, EnvID: out.EnvID}
}

func loginViaHTTP(t *testing.T, client *http.Client, cpURL, orgID string) (string, []*http.Cookie) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"email":    "owner@cluster.example",
		"password": "CorrectHorseBatteryStaple!2026",
		"org_id":   orgID,
	})
	resp, err := client.Post(cpURL+"/admin/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		CSRFToken string `json:"csrf_token"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.CSRFToken == "" {
		out.CSRFToken = resp.Header.Get("X-CSRF-Token")
	}
	return out.CSRFToken, resp.Cookies()
}

type createdKeyDTO struct {
	ID        string
	SecretKey string
	Version   int64
}

func createVKeyViaHTTP(t *testing.T, client *http.Client, cpURL, csrf string, cookies []*http.Cookie, orgID, projectID, envID, name string) createdKeyDTO {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"project_id":             projectID,
		"env_id":                 envID,
		"name":                   name,
		"allowed_models":         []string{"*"},
		"max_concurrent_streams": 50,
	})
	req, _ := http.NewRequest(http.MethodPost, cpURL+"/admin/v1/virtual-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("X-Tanpopo-Org-Id", orgID)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("create vkey: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create vkey status %d: %s", resp.StatusCode, string(raw))
	}
	var parsed struct {
		ID        string `json:"id"`
		Version   int64  `json:"version"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode vkey: %v (%s)", err, string(raw))
	}
	if parsed.ID == "" || parsed.SecretKey == "" {
		t.Fatalf("unexpected vkey payload: %s", string(raw))
	}
	return createdKeyDTO{ID: parsed.ID, SecretKey: parsed.SecretKey, Version: parsed.Version}
}

func revokeVKeyViaHTTP(t *testing.T, client *http.Client, cpURL, csrf string, cookies []*http.Cookie, orgID, keyID string, ver int64, mode string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"mode":   mode,
		"reason": "cluster ac-1 verification",
	})
	req, _ := http.NewRequest(http.MethodPost, cpURL+"/admin/v1/virtual-keys/"+keyID+"/revoke", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("X-Tanpopo-Org-Id", orgID)
	req.Header.Set("If-Match", fmt.Sprintf(`"v%d"`, ver))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("revoke vkey: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke vkey status %d: %s", resp.StatusCode, string(raw))
	}
}

func waitForVKeyStatus(t *testing.T, client *http.Client, dpURL, secretKey string, wantCode int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	lastCode := 0
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, dpURL+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+secretKey)
		resp, err := client.Do(req)
		if err == nil {
			lastCode = resp.StatusCode
			_ = resp.Body.Close()
			if lastCode == wantCode {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s /v1/models timed out waiting for status %d (last=%d)", dpURL, wantCode, lastCode)
}

func verifyFMEA07PartitionCircuitBreaker(t *testing.T, ctx context.Context, dsn string, addrs []string) {
	t.Helper()
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: dsn, SkipMigrate: true, PartitionLookaheadMonths: -1})
	if err != nil {
		t.Fatalf("postgres.Open FMEA-07: %v", err)
	}
	defer pgStore.Close()
	vkClient, err := vkcluster.Dial(vkcluster.Config{Addrs: addrs})
	if err != nil {
		t.Fatalf("vkcluster.Dial FMEA-07: %v", err)
	}
	defer vkClient.Close()

	var fakeNow atomic.Int64
	fakeNow.Store(time.Now().UTC().UnixMilli())
	mgr, err := snapshot.NewManager(ctx, snapshot.ManagerConfig{DB: pgStore.RawDB(), NowMs: fakeNow.Load})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	cs, err := snapshot.NewClusterSync(snapshot.ClusterSyncConfig{
		Manager:  mgr,
		DB:       pgStore.RawDB(),
		Bus:      vkClient,
		NowMs:    fakeNow.Load,
		MaxStale: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClusterSync: %v", err)
	}
	cs.SetFaulted(true, true)
	fakeNow.Add(11_000)
	if cerr := cs.CheckServingReady(); !errors.Is(cerr, snapshot.ErrClusterPartitioned) {
		t.Fatalf("AC-2 FMEA-07 expected cs.CheckServingReady ErrClusterPartitioned after >10s partition, got %v", cerr)
	}
	if rerr := mgr.CheckServingReady(); !errors.Is(rerr, snapshot.ErrClusterPartitioned) && !errors.Is(rerr, snapshot.ErrSnapshotStale) {
		t.Fatalf("AC-2 FMEA-07 expected fail-closed readiness error on mgr.CheckServingReady, got %v", rerr)
	}
	cs.SetFaulted(false, false)
	cs.ProbeBackends(ctx)
	if rerr := mgr.CheckServingReady(); rerr != nil {
		t.Fatalf("AC-2 expected recovery after partition healed, got %v", rerr)
	}
}

func verifyAC3CrossSlotAndFlushallRecovery(t *testing.T, ctx context.Context, dsn string, addrs []string, orgID, projectID, envID, vkeyID string) {
	t.Helper()
	pgStore, err := postgres.Open(ctx, postgres.Options{DSN: dsn, SkipMigrate: true, PartitionLookaheadMonths: -1})
	if err != nil {
		t.Fatalf("postgres.Open AC-3: %v", err)
	}
	defer pgStore.Close()
	db := pgStore.RawDB()

	vkClient, err := vkcluster.Dial(vkcluster.Config{Addrs: addrs})
	if err != nil {
		t.Fatalf("vkcluster.Dial AC-3: %v", err)
	}
	defer vkClient.Close()

	// Exercise CR-07 single-slot multi-key Lua rate-limiter across multiple orgs (distinct cluster hash slots) -> 0 CROSSSLOT errors.
	rl, err := vkcluster.NewRateLimiter(vkcluster.RateLimiterConfig{Client: vkClient, EstimatedReplicaCount: 3})
	if err != nil {
		t.Fatalf("NewRateLimiter: %v", err)
	}
	orgs := []string{orgID, "org_shard_b", "org_shard_c", "org_shard_d"}
	rpmLim := int64(100)
	tpmLim := int64(10_000)
	maxStreams := int64(10)
	for _, oid := range orgs {
		scope := ratelimit.LimitScope{OrgID: oid, ProjectID: "prj_" + oid, EnvID: "env_prod", VirtualKeyID: "vk_ac3_" + oid}
		decision, rerr := rl.AllowRequest(ctx, scope, 50, ratelimit.Limits{
			RPMLimit: &rpmLim, TPMLimit: &tpmLim, MaxConcurrentStreams: &maxStreams,
		})
		if rerr != nil {
			t.Fatalf("Valkey AllowRequest(%s) returned error (CROSSSLOT?): %v", oid, rerr)
		}
		if !decision.Allowed {
			t.Fatalf("expected allowed decision for %s, got %+v", oid, decision)
		}
		lease, sres, serr := rl.AcquireStreamSlot(ctx, scope, maxStreams)
		if serr != nil || !sres.Allowed {
			t.Fatalf("Valkey AcquireStreamSlot(%s) error=%v res=%+v", oid, serr, sres)
		}
		if lease != nil {
			lease.Release()
		}
	}

	// Create PG ledger + open hold, acquire in Valkey, then FLUSHALL all 3 shards and run DR-02 VerifyState rebuild.
	auditLog, err := audit.NewLogger(audit.LoggerConfig{DB: db})
	if err != nil {
		t.Fatalf("audit.NewLogger: %v", err)
	}
	nowMs := time.Now().UTC().UnixMilli()
	ledgerSvc, err := ledger.NewService(ledger.Config{DB: db, Audit: auditLog})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	ldg, err := ledgerSvc.UpsertLedger(ctx, ledger.UpsertLedgerParams{
		OrgID:           orgID,
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         orgID,
		PeriodKind:      ledger.PeriodMonthly,
		PeriodStartMs:   nowMs - 60_000,
		PeriodEndMs:     nowMs + 86_400_000,
		HardLimitMicros: 5_000_000,
	})
	if err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}
	rsv, err := ledgerSvc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        orgID,
		ProjectID:    projectID,
		EnvID:        envID,
		VirtualKeyID: vkeyID,
		RequestID:    "req_ac3_flushall",
		HoldMicros:   350_000,
		LeaseTTLMs:   60_000,
	})
	if err != nil {
		t.Fatalf("ledger.Reserve: %v", err)
	}
	coord, err := vkcluster.NewBudgetCoordinator(vkcluster.BudgetCoordinatorConfig{
		Client: vkClient, DB: db, Ledger: ledgerSvc, NodeID: "ac3-node",
	})
	if err != nil {
		t.Fatalf("NewBudgetCoordinator: %v", err)
	}
	out, err := coord.AcquireHold(ctx, vkcluster.HoldRequest{
		OrgID:             orgID,
		LedgerID:          ldg.ID,
		ReservationID:     rsv.ID,
		CeilingMicroUSD:   5_000_000,
		EstimatedMicroUSD: 350_000,
	})
	if err != nil || !out.Allowed {
		t.Fatalf("AcquireHold before FLUSHALL: out=%+v err=%v", out, err)
	}
	if err := coord.FlushCheckpoints(ctx); err != nil {
		t.Fatalf("FlushCheckpoints: %v", err)
	}

	// Simulate catastrophic Valkey cache loss by issuing FLUSHALL SYNC directly on every cluster node.
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		sc, serr := valkeygo.NewClient(valkeygo.ClientOption{InitAddress: []string{addr}, ForceSingleClient: true, DisableCache: true})
		if serr != nil {
			t.Fatalf("connect shard %s for FLUSHALL: %v", addr, serr)
		}
		if ferr := sc.Do(ctx, sc.B().Flushall().Sync().Build()).Error(); ferr != nil {
			sc.Close()
			t.Fatalf("FLUSHALL on shard %s: %v", addr, ferr)
		}
		sc.Close()
	}

	// Run DR-02 verify-state rebuild against PG16 + 3-shard Valkey Cluster.
	rep, err := admincli.VerifyState(ctx, admincli.VerifyStateOptions{
		PGDSN:                  dsn,
		ValkeyAddrs:            addrs,
		CheckAuditChain:        true,
		ReconcileOrphanedHolds: true,
		RebuildValkeyState:     true,
	})
	if err != nil {
		t.Fatalf("VerifyState(PG+Valkey) failed: %v", err)
	}
	if rep.Status != "ok" || !rep.AuditChainValid || !rep.ValkeyRebuilt || rep.ValkeyHoldsRestored < 1 {
		t.Fatalf("unexpected DR-02 VerifyState report: %+v", rep)
	}

	holdKey, _ := vkcluster.Key(orgID, "budget", "hold:"+ldg.ID)
	restoredVal, err := vkClient.Raw().Do(ctx, vkClient.Raw().B().Hget().Key(holdKey).Field("active_micro_usd").Build()).AsInt64()
	if err != nil || restoredVal != 350_000 {
		t.Fatalf("AC-3 hold state mismatch after FLUSHALL + rebuild: got=%d err=%v (want 350000)", restoredVal, err)
	}
}

func verifyAC4RollingRestart100SSEStreams(t *testing.T, client *http.Client, dpNodes []*nodeServerHandle) {
	t.Helper()
	const totalStreams = 100
	streamClient := netguard.NewHTTPClient()
	streamClient.Timeout = 20 * time.Second

	var wg sync.WaitGroup
	errs := make(chan error, totalStreams)
	allStarted := make(chan struct{})
	var startedCount atomic.Int32

	for i := 0; i < totalStreams; i++ {
		node := dpNodes[i%len(dpNodes)]
		wg.Add(1)
		go func(idx int, baseURL string) {
			defer wg.Done()
			resp, err := streamClient.Get(baseURL + "/v1/test-sse")
			if err != nil {
				errs <- fmt.Errorf("stream %d connect: %w", idx, err)
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf("stream %d status=%d", idx, resp.StatusCode)
				return
			}
			if startedCount.Add(1) == totalStreams {
				close(allStarted)
			}
			scanner := bufio.NewScanner(resp.Body)
			sawDone := false
			frames := 0
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data: ") {
					frames++
					if line == "data: [DONE]" {
						sawDone = true
					}
				}
			}
			if serr := scanner.Err(); serr != nil {
				errs <- fmt.Errorf("stream %d scan: %w", idx, serr)
				return
			}
			if !sawDone || frames < 7 {
				errs <- fmt.Errorf("stream %d incomplete (frames=%d done=%t)", idx, frames, sawDone)
			}
		}(i, node.url)
	}

	// Wait until all 100 concurrent SSE connections are established across the 3 nodes.
	select {
	case <-allStarted:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for all 100 SSE streams to start (started=%d)", startedCount.Load())
	}

	// Staggered rolling restart across all 3 Data Plane nodes while SSE streams are active.
	for idx, dp := range dpNodes {
		dp.draining.Store(true)
		rresp, rerr := client.Get(dp.url + "/readyz")
		if rerr != nil {
			t.Fatalf("readyz on draining node %d: %v", idx, rerr)
		}
		_ = rresp.Body.Close()
		if rresp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 on /readyz during rolling drain on node %d, got %d", idx, rresp.StatusCode)
		}
		go func(srv *http.Server) {
			sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(sctx)
		}(dp.server)
		time.Sleep(50 * time.Millisecond)
	}

	wg.Wait()
	close(errs)
	for serr := range errs {
		t.Fatalf("AC-4 rolling restart SSE failure: %v", serr)
	}
}

func prepareIsolatedPGDatabase(t *testing.T, baseDSN string) (string, func()) {
	t.Helper()
	adminDSN := strings.TrimSpace(os.Getenv("TANPOPO_TEST_PG_ADMIN_DSN"))
	if adminDSN == "" {
		return baseDSN, func() {}
	}
	u, err := url.Parse(baseDSN)
	if err != nil {
		return baseDSN, func() {}
	}
	var rb [4]byte
	_, _ = rand.Read(rb[:])
	dbName := "tanpopo_c0040_" + hex.EncodeToString(rb[:])

	// Connect to postgres maintenance database using admin credentials.
	au, err := url.Parse(adminDSN)
	if err != nil {
		return baseDSN, func() {}
	}
	au.Path = "/postgres"
	adminDB, err := sql.Open("pgx", au.String())
	if err != nil {
		t.Fatalf("open admin pg db: %v", err)
	}
	owner := u.User.Username()
	if owner == "" {
		owner = "tanpopo_app"
	}
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(`
		DO $$ BEGIN
			CREATE ROLE tanpopo_webhook_worker NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS;
		EXCEPTION WHEN duplicate_object THEN NULL;
		END $$;
		GRANT tanpopo_webhook_worker TO %s;`, owner)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("ensure tanpopo_webhook_worker role: %v", err)
	}
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, dbName, owner)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create isolated pg db %s: %v", dbName, err)
	}
	du := *au
	du.Path = "/" + dbName
	dbAdminConn, err := sql.Open("pgx", du.String())
	if err != nil {
		_ = adminDB.Close()
		t.Fatalf("open isolated db as admin: %v", err)
	}
	if _, err := dbAdminConn.ExecContext(context.Background(), fmt.Sprintf(`
		GRANT USAGE ON SCHEMA public TO tanpopo_webhook_worker;
		ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tanpopo_webhook_worker;`, owner)); err != nil {
		_ = dbAdminConn.Close()
		_ = adminDB.Close()
		t.Fatalf("grant webhook worker privileges on %s: %v", dbName, err)
	}
	_ = dbAdminConn.Close()
	u.Path = "/" + dbName
	return u.String(), func() {
		_, _ = adminDB.ExecContext(context.Background(), fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()`, dbName))
		_, _ = adminDB.ExecContext(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, dbName))
		_ = adminDB.Close()
	}
}
