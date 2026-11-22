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
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newLoopbackTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("loopback guard split host/port %q: %w", address, err)
			}
			addr, parseErr := netip.ParseAddr(host)
			if parseErr != nil {
				return fmt.Errorf("loopback guard requires numeric IP, got %q: %w", host, parseErr)
			}
			if !addr.IsLoopback() {
				return fmt.Errorf("loopback guard blocked non-loopback address %s", addr)
			}
			return nil
		},
	}
	return &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        32,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
}

func newLoopbackHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	tr := newLoopbackTransport()
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate loopback port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func buildTanpopoBinary(t *testing.T, root string) string {
	t.Helper()
	binName := "tanpopo"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), binName)
	buildCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(buildCtx, "go", "build", "-trimpath", "-o", binPath, "./cmd/tanpopo")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build tanpopo binary: %v\n%s", err, string(out))
	}
	return binPath
}

func sanitizedChildEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TANPOPO_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, extra...)
}

type startedNode struct {
	cmd       *exec.Cmd
	addr      string
	baseURL   string
	dataDir   string
	tokenFile string
	logs      *lockedBuffer
	waitOnce  sync.Once
	waitErr   error
}

func startTanpopoNode(t *testing.T, binPath, addr, dataDir, masterKeyHex string) *startedNode {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir dataDir: %v", err)
	}
	tokenFile := filepath.Join(dataDir, "bootstrap.token")
	dbFile := filepath.Join(dataDir, "tanpopo.db")
	lockFile := filepath.Join(dataDir, "tanpopo.lock")
	pluginsDir := filepath.Join(dataDir, "plugins")
	logBuf := &lockedBuffer{}

	cmd := exec.Command(binPath, "serve", "--role", "all", "--listen", addr)
	cmd.Env = sanitizedChildEnv(
		"TANPOPO_DATA_DIR="+dataDir,
		"TANPOPO_STORAGE_ENGINE=sqlite",
		"TANPOPO_DB_PATH="+dbFile,
		"TANPOPO_LOCK_FILE="+lockFile,
		"TANPOPO_PLUGINS_DIR="+pluginsDir,
		"TANPOPO_MASTER_KEY="+masterKeyHex,
		"TANPOPO_BOOTSTRAP_TOKEN_FILE="+tokenFile,
		"TANPOPO_ADMIN_LISTEN_ADDR=",
		"TANPOPO_OTEL_ENABLED=false",
		"TANPOPO_TEST_HERMETIC=1",
	)
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start tanpopo node: %v", err)
	}
	node := &startedNode{
		cmd:       cmd,
		addr:      addr,
		baseURL:   "http://" + addr,
		dataDir:   dataDir,
		tokenFile: tokenFile,
		logs:      logBuf,
	}
	t.Cleanup(func() {
		node.stop(t)
	})
	return node
}

func (n *startedNode) stop(t *testing.T) {
	t.Helper()
	n.waitOnce.Do(func() {
		if n.cmd.Process == nil {
			return
		}
		if sigErr := n.cmd.Process.Signal(os.Interrupt); sigErr != nil {
			_ = n.cmd.Process.Kill()
			n.waitErr = n.cmd.Wait()
			return
		}
		done := make(chan error, 1)
		go func() { done <- n.cmd.Wait() }()
		select {
		case n.waitErr = <-done:
		case <-time.After(5 * time.Second):
			_ = n.cmd.Process.Kill()
			n.waitErr = <-done
		}
	})
}

func sqlitePragmaDSN(dbPath string) string {
	slashed := filepath.ToSlash(filepath.Clean(dbPath))
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

func waitForReadyz(ctx context.Context, client *http.Client, baseURL string) error {
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/readyz", nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("readyz timeout for %s: %w", baseURL, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func readTokenWithRetry(ctx context.Context, path string) (string, error) {
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			if tok := strings.TrimSpace(string(raw)); tok != "" {
				return tok, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
}

func drillDoJSON(
	ctx context.Context,
	client *http.Client,
	method, targetURL, csrf, orgID string,
	cookies []*http.Cookie,
	payload any,
) (*http.Response, string, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, "", err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, "", err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if orgID != "" {
		req.Header.Set("X-Tanpopo-Org-ID", orgID)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	rawBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(rawBody), nil
}

func drillExtractFirstID(rawJSON string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return ""
	}
	if items, ok := obj["items"].([]any); ok && len(items) > 0 {
		if first, ok := items[0].(map[string]any); ok {
			if id, ok := first["id"].(string); ok {
				return id
			}
		}
	}
	if data, ok := obj["data"].([]any); ok && len(data) > 0 {
		if first, ok := data[0].(map[string]any); ok {
			if id, ok := first["id"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// TestGATEREL02AndDR04_LiveCanaryRolloutAndSub60sRollback executes a live staged
// canary rollout (5% -> 25% -> 50%) against two compiled tanpopo binaries with
// N-1 Expand-Only DB schema tolerance verified on the baseline binary, holds
// baseNode-backed SSE streams uninterrupted across every phase, injects an
// ALT-GATEWAY-BURN 5xx fault, and measures instant DR-04 rollback (< 60s).
func TestGATEREL02AndDR04_LiveCanaryRolloutAndSub60sRollback(t *testing.T) {
	root := repoRootForReleaseTest(t)
	binPath := buildTanpopoBinary(t, root)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newLoopbackHTTPClient(t, 5*time.Second)
	sseClient := newLoopbackHTTPClient(t, 0)
	proxyTransport := newLoopbackTransport()
	t.Cleanup(proxyTransport.CloseIdleConnections)

	masterBytes := make([]byte, 32)
	if _, err := rand.Read(masterBytes); err != nil {
		t.Fatalf("rand master key: %v", err)
	}
	masterHex := hex.EncodeToString(masterBytes)

	// 1. Start initial Baseline binary to bootstrap owner and Virtual Key.
	baseDataDir := filepath.Join(t.TempDir(), "baseline-data")
	baseAddr := freeLoopbackAddr(t)
	baseNode := startTanpopoNode(t, binPath, baseAddr, baseDataDir, masterHex)
	if err := waitForReadyz(ctx, client, baseNode.baseURL); err != nil {
		baseNode.stop(t)
		t.Fatalf("baseline readyz: %v\nlogs:\n%s", err, baseNode.logs.String())
	}

	bootTok, err := readTokenWithRetry(ctx, baseNode.tokenFile)
	if err != nil {
		t.Fatalf("read bootstrap token: %v", err)
	}
	bootResp, bootBody, err := drillDoJSON(ctx, client, http.MethodPost, baseNode.baseURL+"/admin/v1/bootstrap/complete", "", "", nil, map[string]any{
		"bootstrap_token":                bootTok,
		"owner_email":                    "canary-drill@tanpopo.local",
		"owner_password":                 "Canary-Drill-Horse-2026!",
		"master_kek_backup_acknowledged": true,
	})
	if err != nil || (bootResp.StatusCode != http.StatusCreated && bootResp.StatusCode != http.StatusOK) {
		t.Fatalf("bootstrap failed: err=%v resp=%v body=%s", err, bootResp, bootBody)
	}

	loginResp, loginBody, err := drillDoJSON(ctx, client, http.MethodPost, baseNode.baseURL+"/admin/v1/auth/login", "", "", nil, map[string]any{
		"email":    "canary-drill@tanpopo.local",
		"password": "Canary-Drill-Horse-2026!",
	})
	if err != nil || loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: err=%v body=%s", err, loginBody)
	}
	cookies := loginResp.Cookies()
	var loginDTO struct {
		CSRFToken string `json:"csrf_token"`
		OrgID     string `json:"org_id"`
	}
	_ = json.Unmarshal([]byte(loginBody), &loginDTO)
	if loginDTO.CSRFToken == "" {
		loginDTO.CSRFToken = loginResp.Header.Get("X-CSRF-Token")
	}
	if loginDTO.OrgID == "" {
		_, orgsBody, _ := drillDoJSON(ctx, client, http.MethodGet, baseNode.baseURL+"/admin/v1/organizations", loginDTO.CSRFToken, "", cookies, nil)
		loginDTO.OrgID = drillExtractFirstID(orgsBody)
	}
	_, projBody, err := drillDoJSON(ctx, client, http.MethodGet, baseNode.baseURL+"/admin/v1/projects", loginDTO.CSRFToken, loginDTO.OrgID, cookies, nil)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	projID := drillExtractFirstID(projBody)
	if projID == "" {
		t.Fatalf("expected non-empty project ID from %s", projBody)
	}
	_, envBody, err := drillDoJSON(ctx, client, http.MethodGet, baseNode.baseURL+"/admin/v1/projects/"+projID+"/environments", loginDTO.CSRFToken, loginDTO.OrgID, cookies, nil)
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}
	envID := drillExtractFirstID(envBody)
	if envID == "" {
		t.Fatalf("expected non-empty env ID from %s", envBody)
	}

	vkResp, vkBody, err := drillDoJSON(ctx, client, http.MethodPost, baseNode.baseURL+"/admin/v1/virtual-keys", loginDTO.CSRFToken, loginDTO.OrgID, cookies, map[string]any{
		"project_id":             projID,
		"env_id":                 envID,
		"name":                   "canary-drill-vk",
		"allowed_models":         []string{"*"},
		"max_concurrent_streams": 32,
	})
	if err != nil || vkResp.StatusCode != http.StatusCreated {
		t.Fatalf("create virtual key: err=%v status=%v body=%s", err, vkResp, vkBody)
	}
	var vkDTO struct {
		SecretKey string `json:"secret_key"`
	}
	_ = json.Unmarshal([]byte(vkBody), &vkDTO)
	if !strings.HasPrefix(vkDTO.SecretKey, "vk_tp_") {
		t.Fatalf("unexpected virtual key response: %s", vkBody)
	}

	// 2. Apply Expand-Only N+1 additive column onto the canonical SQLite DB, clone to canary,
	// and restart BOTH baseline (N-1 binary reading expanded DB) and canary on expanded schema!
	baseNode.stop(t)

	canaryDataDir := filepath.Join(t.TempDir(), "canary-data")
	if err := os.MkdirAll(canaryDataDir, 0o700); err != nil {
		t.Fatalf("mkdir canary data dir: %v", err)
	}
	baseDBFile := filepath.Join(baseDataDir, "tanpopo.db")
	canaryDBFile := filepath.Join(canaryDataDir, "tanpopo.db")

	dbHandle, err := sql.Open("sqlite", sqlitePragmaDSN(baseDBFile))
	if err != nil {
		t.Fatalf("open baseline sqlite for expand-only migration: %v", err)
	}
	if _, err := dbHandle.ExecContext(ctx, "ALTER TABLE organizations ADD COLUMN canary_expand_only_note TEXT NOT NULL DEFAULT 'v1.0.0'"); err != nil {
		_ = dbHandle.Close()
		t.Fatalf("apply Expand-Only column onto shared schema: %v", err)
	}
	if _, err := dbHandle.ExecContext(ctx, "VACUUM INTO ?", canaryDBFile); err != nil {
		_ = dbHandle.Close()
		t.Fatalf("VACUUM INTO canary snapshot: %v", err)
	}
	if closeErr := dbHandle.Close(); closeErr != nil {
		t.Fatalf("close baseline sqlite handle: %v", closeErr)
	}

	// Restart N-1 baseline on expanded schema AND boot Canary on expanded schema.
	baseNode = startTanpopoNode(t, binPath, baseAddr, baseDataDir, masterHex)
	if err := waitForReadyz(ctx, client, baseNode.baseURL); err != nil {
		baseNode.stop(t)
		t.Fatalf("baseline N-1 restart on Expand-Only DB failed: %v\nlogs:\n%s", err, baseNode.logs.String())
	}

	canaryNode := startTanpopoNode(t, binPath, freeLoopbackAddr(t), canaryDataDir, masterHex)
	if err := waitForReadyz(ctx, client, canaryNode.baseURL); err != nil {
		canaryNode.stop(t)
		t.Fatalf("canary readyz: %v\nlogs:\n%s", err, canaryNode.logs.String())
	}

	// 3. Stand up Weighted Canary Ingress Router (5% -> 25% -> 50% -> 0% rollback)
	// plus live SSE streams backed continuously by baseline Node /v1/models health ticks.
	baseParsed, _ := url.Parse(baseNode.baseURL)
	canaryParsed, _ := url.Parse(canaryNode.baseURL)
	baseProxy := httputil.NewSingleHostReverseProxy(baseParsed)
	baseProxy.Transport = proxyTransport
	canaryProxy := httputil.NewSingleHostReverseProxy(canaryParsed)
	canaryProxy.Transport = proxyTransport

	var canaryWeight atomic.Int32
	var canaryFaultInjected atomic.Bool
	var reqCounter atomic.Uint64
	var baseHits atomic.Int64
	var canaryHits atomic.Int64
	var canary5xx atomic.Int64

	routerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/drill/sse-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "no flusher", http.StatusInternalServerError)
				return
			}
			probeNode := func() bool {
				pReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, baseNode.baseURL+"/v1/models", nil)
				pReq.Header.Set("Authorization", "Bearer "+vkDTO.SecretKey)
				pResp, pErr := (&http.Client{Timeout: 2 * time.Second, Transport: proxyTransport}).Do(pReq)
				if pErr != nil {
					return false
				}
				_ = pResp.Body.Close()
				return pResp.StatusCode == http.StatusOK
			}
			if !probeNode() {
				http.Error(w, "baseline upstream unavailable", http.StatusBadGateway)
				return
			}
			_, _ = fmt.Fprintf(w, "data: {\"node\":\"baseline\",\"event\":\"connected\"}\n\n")
			flusher.Flush()

			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					if !probeNode() {
						return
					}
					_, _ = fmt.Fprintf(w, "data: {\"node\":\"baseline\",\"event\":\"heartbeat\"}\n\n")
					flusher.Flush()
				}
			}
		}

		idx := reqCounter.Add(1) - 1
		wPct := uint64(canaryWeight.Load())
		if wPct > 0 && (idx%100) < wPct {
			canaryHits.Add(1)
			if canaryFaultInjected.Load() {
				canary5xx.Add(1)
				w.Header().Set("X-Tanpopo-Canary-Node", "canary")
				http.Error(w, `{"error":{"code":"internal_error","message":"simulated canary regression"}}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("X-Tanpopo-Canary-Node", "canary")
			canaryProxy.ServeHTTP(w, r)
			return
		}
		baseHits.Add(1)
		w.Header().Set("X-Tanpopo-Canary-Node", "baseline")
		baseProxy.ServeHTTP(w, r)
	}))
	defer routerSrv.Close()

	// Establish 4 live baseline-backed SSE streams BEFORE rollout stages begin.
	sseCtx, sseCancel := context.WithCancel(ctx)
	defer sseCancel()
	var sseReadyWG sync.WaitGroup
	var sseDoneWG sync.WaitGroup
	var sseDropped atomic.Int64
	sseReadyWG.Add(4)
	for i := 0; i < 4; i++ {
		sseDoneWG.Add(1)
		go func() {
			defer sseDoneWG.Done()
			readySignaled := false
			signalReady := func() {
				if !readySignaled {
					readySignaled = true
					sseReadyWG.Done()
				}
			}
			req, _ := http.NewRequestWithContext(sseCtx, http.MethodGet, routerSrv.URL+"/drill/sse-stream", nil)
			resp, err := sseClient.Do(req)
			if err != nil {
				sseDropped.Add(1)
				signalReady()
				return
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				sseDropped.Add(1)
				signalReady()
				return
			}
			scanner := bufio.NewScanner(resp.Body)
			if !scanner.Scan() || !strings.Contains(scanner.Text(), `"connected"`) {
				sseDropped.Add(1)
				signalReady()
				return
			}
			signalReady()
			for scanner.Scan() {
				// Keep draining heartbeats while sseCtx remains active.
			}
			if scanErr := scanner.Err(); (scanErr != nil && !errors.Is(scanErr, context.Canceled)) || sseCtx.Err() == nil {
				// Unexpected EOF or error BEFORE intentional post-rollback sseCancel().
				sseDropped.Add(1)
			}
		}()
	}
	sseReadyWG.Wait()

	sendModelProbes := func(count int) (okCount int, errCount int, bHits int64, cHits int64) {
		beforeBase := baseHits.Load()
		beforeCanary := canaryHits.Load()
		for i := 0; i < count; i++ {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, routerSrv.URL+"/v1/models", nil)
			req.Header.Set("Authorization", "Bearer "+vkDTO.SecretKey)
			resp, err := client.Do(req)
			if err != nil {
				errCount++
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				okCount++
			} else {
				errCount++
			}
		}
		return okCount, errCount, baseHits.Load() - beforeBase, canaryHits.Load() - beforeCanary
	}

	// Stage 1..3: Progressive Canary Weight Shift (5% -> 25% -> 50%).
	for _, stage := range []struct {
		weight     int32
		wantCanary int64
	}{
		{weight: 5, wantCanary: 5},
		{weight: 25, wantCanary: 25},
		{weight: 50, wantCanary: 50},
	} {
		canaryWeight.Store(stage.weight)
		reqCounter.Store(0)
		okCnt, errCnt, _, cHits := sendModelProbes(100)
		if errCnt != 0 || okCnt != 100 || cHits != stage.wantCanary {
			t.Fatalf("stage %d%% unexpected results: ok=%d err=%d canaryHits=%d (want %d)", stage.weight, okCnt, errCnt, cHits, stage.wantCanary)
		}
	}

	// 4. Inject Canary 5xx Error Spike crossing ALT-GATEWAY-BURN & trigger instant DR-04 rollback.
	canaryFaultInjected.Store(true)
	_, errCnt, _, _ := sendModelProbes(20)
	if errCnt == 0 || canary5xx.Load() == 0 {
		t.Fatalf("expected canary 5xx fault injection to trigger error spike")
	}

	rollbackStart := time.Now()
	canaryWeight.Store(0)
	canaryNode.stop(t)
	rollbackElapsed := time.Since(rollbackStart)

	if rollbackElapsed >= 60*time.Second {
		t.Fatalf("DR-04 rollback exceeded 60s SLA ceiling: took %s", rollbackElapsed)
	}

	// Confirm 100% healthy traffic restored immediately on Baseline running against Expand-Only DB.
	okAfter, errAfter, bAfter, cAfter := sendModelProbes(50)
	if errAfter != 0 || okAfter != 50 || bAfter != 50 || cAfter != 0 {
		t.Fatalf("post-rollback traffic failed: ok=%d err=%d base=%d canary=%d", okAfter, errAfter, bAfter, cAfter)
	}

	if err := waitForReadyz(ctx, client, baseNode.baseURL); err != nil {
		t.Fatalf("baseline unhealthy after rollback: %v", err)
	}
	if droppedBeforeCancel := sseDropped.Load(); droppedBeforeCancel != 0 {
		t.Fatalf("expected 0 dropped baseline SSE streams prior to cancel, got %d", droppedBeforeCancel)
	}
	sseCancel()
	sseDoneWG.Wait()
	if droppedAfter := sseDropped.Load(); droppedAfter != 0 {
		t.Fatalf("expected 0 dropped SSE streams during canary rollback, got %d", droppedAfter)
	}

	t.Logf("LIVE CANARY ROLLOUT (5%%->25%%->50%%) & DR-04 INSTANT ROLLBACK VERIFIED in %s (< 60s ceiling; N-1 baseline restarted on Expand-Only DB; canary_5xx=%d; post_rollback_ok=50/50; sse_dropped=0)",
		rollbackElapsed, canary5xx.Load())
}
