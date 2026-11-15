// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
)

func TestAC2ChatCompletionsStreamIncludeUsageElision(t *testing.T) {
	h := setupIngressHarness(t, 0)

	var mu sync.Mutex
	var capturedUsage []ingresshttp.UsageRecord

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:  h.snapshots,
		Broker:     h.broker,
		PluginsDir: h.pluginsD,
		Audit:      h.audit,
		NowMs:      func() int64 { return h.clk.Now().UnixMilli() },
		UsageObserver: func(rec ingresshttp.UsageRecord) {
			mu.Lock()
			capturedUsage = append(capturedUsage, rec)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("NewServer with UsageObserver: %v", err)
	}
	defer func() { _ = srv.Close(context.Background()) }()
	h.httpSrv.Config.Handler = srv

	// Case 1 (AC-2): Caller omits stream_options -> adapter injects stream_options.include_usage=true
	// upstream so UsageObserver receives exact terminal usage while downstream SSE elides "usage".
	reqBody := []byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"ping"}]}`)
	req, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("request 1 failed: %v", err)
	}
	rawSSE1, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.StatusCode, string(rawSSE1))
	}
	if !strings.Contains(string(rawSSE1), `"finish_reason":"stop"`) || !strings.Contains(string(rawSSE1), "[DONE]") {
		t.Fatalf("expected finish_reason + [DONE] in downstream SSE, got: %s", string(rawSSE1))
	}
	if strings.Contains(string(rawSSE1), `"usage"`) {
		t.Fatalf("AC-2 violation: expected unrequested usage field to be elided downstream, got: %s", string(rawSSE1))
	}

	// Case 2: Caller explicitly requests stream_options.include_usage=true -> downstream SSE retains usage.
	reqBody2 := []byte(`{"model":"acme/chat-1","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"ping"}]}`)
	req2, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(reqBody2))
	req2.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := h.client.Do(req2)
	if err != nil {
		t.Fatalf("request 2 failed: %v", err)
	}
	rawSSE2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if !strings.Contains(string(rawSSE2), `"usage"`) {
		t.Fatalf("expected caller-requested usage in downstream SSE, got: %s", string(rawSSE2))
	}

	// Verify both upstream requests actually received stream_options.include_usage=true.
	upReqs := h.fake.Requests()
	if len(upReqs) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(upReqs))
	}
	for i, ur := range upReqs {
		var parsed map[string]any
		if uerr := json.Unmarshal(ur.Body, &parsed); uerr != nil {
			t.Fatalf("upstream req %d invalid JSON: %v", i, uerr)
		}
		so, _ := parsed["stream_options"].(map[string]any)
		inc, _ := so["include_usage"].(bool)
		if !inc {
			t.Fatalf("upstream req %d missing stream_options.include_usage=true: %s", i, string(ur.Body))
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(capturedUsage) != 2 || capturedUsage[0].Usage == nil || capturedUsage[0].Usage.InputTokens != 10 || capturedUsage[0].Usage.OutputTokens != 5 {
		t.Fatalf("expected exact UsageObserver telemetry (10 in, 5 out), got %+v", capturedUsage)
	}
}
