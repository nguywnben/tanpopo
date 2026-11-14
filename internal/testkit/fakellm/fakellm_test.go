// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package fakellm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestFakeLLMServerOpenAISyncAnd50ChunkSSEAC1(t *testing.T) {
	t.Parallel()
	srv := NewServer(t)
	client := srv.Client()

	if !strings.HasPrefix(srv.URL(), "http://127.0.0.1:") || !strings.HasPrefix(srv.Addr(), "127.0.0.1:") {
		t.Fatalf("expected loopback bind 127.0.0.1:0, got %s", srv.URL())
	}

	// Warm loopback TCP keep-alive pool before measuring AC-1 roundtrip latency.
	modelsReq, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL()+"/v1/models", nil)
	if err != nil {
		t.Fatalf("new models req: %v", err)
	}
	modelsResp, err := client.Do(modelsReq)
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	_, _ = io.ReadAll(modelsResp.Body)
	_ = modelsResp.Body.Close()
	if modelsResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/models status=%d", modelsResp.StatusCode)
	}

	// 1. Synchronous JSON /v1/chat/completions
	syncStart := time.Now()
	syncReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	syncReq.Header.Set("Content-Type", "application/json")
	syncReq.Header.Set("Authorization", "Bearer "+canary.DefaultCanarySecret())
	syncResp, err := client.Do(syncReq)
	if err != nil {
		t.Fatalf("sync chat request: %v", err)
	}
	syncBody, _ := io.ReadAll(syncResp.Body)
	_ = syncResp.Body.Close()
	syncElapsed := time.Since(syncStart)
	if syncResp.StatusCode != http.StatusOK || !strings.Contains(string(syncBody), "ok-from-fakellm") {
		t.Fatalf("unexpected sync chat response status=%d body=%s", syncResp.StatusCode, string(syncBody))
	}

	// 2. Streaming 50-chunk SSE with : ping comments
	sseStart := time.Now()
	sseReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+"/v1/chat/completions?chunks=50", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	sseReq.Header.Set("Content-Type", "application/json")
	sseResp, err := client.Do(sseReq)
	if err != nil {
		t.Fatalf("50-chunk SSE request: %v", err)
	}
	defer sseResp.Body.Close()

	dataFrames := 0
	pingComments := 0
	doneSeen := false
	sc := bufio.NewScanner(sseResp.Body)
	for sc.Scan() {
		line := sc.Text()
		if line == ": ping" {
			pingComments++
		}
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				doneSeen = true
				continue
			}
			dataFrames++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read 50-chunk SSE: %v", err)
	}
	sseElapsed := time.Since(sseStart)
	if dataFrames != 51 || pingComments < 9 || !doneSeen {
		t.Fatalf("expected 51 data frames (50 content + 1 stop/usage), >=9 pings, [DONE]; got frames=%d pings=%d done=%v", dataFrames, pingComments, doneSeen)
	}
	t.Logf("AC-1 telemetry: sync=%v 50-chunk-SSE=%v", syncElapsed, sseElapsed)

	// Verify recorded requests preserved Authorization header.
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("expected 3 recorded requests, got %d", len(reqs))
	}
	if gotAuth := reqs[1].Headers.Get("Authorization"); !strings.Contains(gotAuth, "Bearer ") {
		t.Fatalf("expected recorded Authorization header on sync request, got %q", gotAuth)
	}
}

func TestFakeLLMServerTCPRSTAfterExact5ChunksAC2(t *testing.T) {
	t.Parallel()
	srv := NewServer(t)
	client := srv.Client()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+"/v1/chat/completions?chunks=20", strings.NewReader(`{"stream":true}`))
	req.Header.Set(FaultHeader, "tcp_rst_after_chunks=5")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("start stream with tcp_rst fault: %v", err)
	}
	defer resp.Body.Close()

	validChunks := 0
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			var parsed map[string]any
			if jerr := json.Unmarshal([]byte(payload), &parsed); jerr != nil {
				t.Fatalf("invalid JSON chunk %q: %v", payload, jerr)
			}
			validChunks++
		}
	}
	streamErr := sc.Err()
	if validChunks != 5 {
		t.Fatalf("expected exactly 5 valid SSE chunks before TCP reset, got %d", validChunks)
	}
	if streamErr == nil {
		// On some platforms raw EOF vs ECONNRESET may surface differently if kernel FIN races SO_LINGER=0;
		// ensure [DONE] was never sent and request stopped strictly at ChunksSent == 5.
		reqs := srv.Requests()
		if len(reqs) != 1 || reqs[0].ChunksSent != 5 {
			t.Fatalf("expected deterministic abrupt termination at chunk 5 without error/EOF mismatch, reqs=%+v", reqs)
		}
		t.Fatalf("expected deterministic TCP reset / premature stream error after chunk 5, got nil error")
	}
}

func TestFakeLLMServerFaultsDialectsAndClientCancel(t *testing.T) {
	t.Parallel()

	fc := clock.NewFakeClock(time.Time{})
	srv := NewServerWithOptions(t, Options{Clock: fc, StreamChunks: 6})
	client := srv.Client()

	// 1. 429 + Retry-After + echo_auth_header_in_error
	srv.EnqueueFault("http_status=429;retry_after=12", "echo_auth_header_in_error=true;http_status=401")

	r429, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{}`))
	resp429, err := client.Do(r429)
	if err != nil {
		t.Fatalf("429 request: %v", err)
	}
	_ = resp429.Body.Close()
	if resp429.StatusCode != http.StatusTooManyRequests || resp429.Header.Get("Retry-After") != "12" {
		t.Fatalf("expected 429 Retry-After:12, got status=%d retry=%q", resp429.StatusCode, resp429.Header.Get("Retry-After"))
	}

	rEcho, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{}`))
	secret := canary.DefaultCanarySecret()
	rEcho.Header.Set("x-api-key", secret)
	respEcho, err := client.Do(rEcho)
	if err != nil {
		t.Fatalf("echo auth fault request: %v", err)
	}
	echoBody, _ := io.ReadAll(respEcho.Body)
	_ = respEcho.Body.Close()
	if respEcho.StatusCode != http.StatusUnauthorized || !bytes.Contains(echoBody, []byte(secret)) {
		t.Fatalf("expected 401 echoing auth header for scrubber tests, got %d", respEcho.StatusCode)
	}

	// 2. delay_first_byte_ms driven deterministically by FakeClock + client cancellation!
	ctx, cancel := context.WithCancel(context.Background())
	reqDone := make(chan error, 1)
	go func() {
		r, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL()+"/v1/chat/completions", strings.NewReader(`{"stream":true}`))
		r.Header.Set(FaultHeader, "delay_first_byte_ms=5000")
		resp, doErr := client.Do(r)
		if resp != nil {
			_ = resp.Body.Close()
		}
		reqDone <- doErr
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if err := fc.AwaitWaiters(waitCtx, 1); err != nil {
		t.Fatalf("AwaitWaiters for delay_first_byte_ms: %v", err)
	}
	cancel()
	if err := <-reqDone; err == nil {
		t.Fatalf("expected canceled request error during delay_first_byte_ms")
	}
	select {
	case <-srv.DisconnectEvents():
	case <-time.After(2 * time.Second):
		t.Fatalf("expected DisconnectEvents signal after client cancel")
	}

	// 3. Multi-dialect sync & streaming endpoints + omit_stream_usage + malformed_tool_json.
	for _, tc := range []struct {
		method string
		path   string
		body   string
		fault  string
		want   string
	}{
		{http.MethodPost, "/v1/embeddings", `{"input":"hello"}`, "", "embedding"},
		{http.MethodPost, "/v1/rerank", `{"query":"q"}`, "", "relevance_score"},
		{http.MethodPost, "/v1/responses", `{"model":"gpt-4o","stream":false}`, "", "ok-from-responses"},
		{http.MethodPost, "/v1/responses", `{"model":"gpt-4o","stream":true}`, "omit_stream_usage", "response.completed"},
		{http.MethodPost, "/v1/messages", `{"model":"claude-3-7-sonnet","stream":false}`, "", "ok-from-anthropic"},
		{http.MethodPost, "/v1/messages", `{"model":"claude-3-7-sonnet","stream":true}`, "omit_stream_usage", "message_stop"},
		{http.MethodPost, "/v1beta/models/gemini-2.5-flash:generateContent", `{}`, "", "ok-from-gemini"},
		{http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", `{}`, "", "usageMetadata"},
		{http.MethodPost, "/v1beta/models/gemini-2.5-flash:streamGenerateContent", `{}`, "omit_stream_usage", "gem-0"},
		{http.MethodPost, "/v1/chat/completions", `{"stream":true}`, "malformed_tool_json,omit_stream_usage", "get_weather"},
		{http.MethodPost, "/v1/unknown", `{}`, "", "not_found"},
	} {
		req, _ := http.NewRequestWithContext(context.Background(), tc.method, srv.URL()+tc.path, strings.NewReader(tc.body))
		req.Header.Set(StreamChunksHeader, "6")
		if tc.fault != "" {
			req.Header.Set(FaultHeader, tc.fault)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s failed: %v", tc.method, tc.path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !strings.Contains(string(b), tc.want) {
			t.Fatalf("%s %s missing %q in body: %s", tc.method, tc.path, tc.want, string(b))
		}
	}

	// Verify ?chunks=1 emits malformed_tool_json and post-loop tcp_rst_after_chunks works on Responses/Anthropic.
	r1Chunk, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+"/v1/chat/completions?chunks=1", strings.NewReader(`{"stream":true}`))
	r1Chunk.Header.Set(FaultHeader, "malformed_tool_json")
	resp1, err := client.Do(r1Chunk)
	if err != nil {
		t.Fatalf("1-chunk malformed_tool_json: %v", err)
	}
	b1, _ := io.ReadAll(resp1.Body)
	_ = resp1.Body.Close()
	if !strings.Contains(string(b1), "get_weather") {
		t.Fatalf("expected malformed tool call on ?chunks=1, got %s", string(b1))
	}

	for _, path := range []string{
		"/v1/responses",
		"/v1/messages",
		"/v1beta/models/gemini-2.5-flash:streamGenerateContent",
	} {
		// Also test exact boundary tcp_rst_after_chunks == targetChunks (post-loop guard).
		for _, q := range []string{"?chunks=10", "?chunks=2"} {
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL()+path+q, strings.NewReader(`{"stream":true}`))
			req.Header.Set(FaultHeader, "tcp_rst_after_chunks=2")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("tcp_rst %s%s: %v", path, q, err)
			}
			_, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil {
				t.Fatalf("expected deterministic TCP reset read error on %s%s", path, q)
			}
		}
	}

	// Verify mid-stream client disconnect telemetry across OpenAI Chat, Responses, Anthropic, and Gemini.
	for _, tc := range []struct {
		path string
		body string
	}{
		{"/v1/chat/completions?chunks=50000", `{"stream":true}`},
		{"/v1/responses?chunks=50000", `{"stream":true}`},
		{"/v1/messages?chunks=50000", `{"stream":true}`},
		{"/v1beta/models/gemini-2.5-flash:streamGenerateContent?chunks=50000", `{"stream":true}`},
	} {
	drainDisconnects:
		for {
			select {
			case <-srv.DisconnectEvents():
			default:
				break drainDisconnects
			}
		}
		srv.ResetRequests()
		streamCtx, cancelStream := context.WithCancel(context.Background())
		req, _ := http.NewRequestWithContext(streamCtx, http.MethodPost, srv.URL()+tc.path, strings.NewReader(tc.body))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("start long stream %s: %v", tc.path, err)
		}
		buf := make([]byte, 64)
		_, _ = resp.Body.Read(buf)
		cancelStream()
		_ = resp.Body.Close()

		select {
		case <-srv.DisconnectEvents():
		case <-time.After(2 * time.Second):
			t.Fatalf("expected DisconnectEvents signal after mid-stream cancel on %s", tc.path)
		}
	}

	srv.SetDefaultFault("")
	srv.ResetRequests()
	if len(srv.Requests()) != 0 {
		t.Fatalf("expected 0 recorded requests after ResetRequests")
	}
	srv.Close()
	srv.Close() // verify idempotent Close
}
