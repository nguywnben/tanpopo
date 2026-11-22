// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
)

func TestProtectiveOverloadSheddingHeapAndStreamsNEG1(t *testing.T) {
	t.Run("heap_85_percent_sheds_503_and_recovers_immediately", func(t *testing.T) {
		h := setupIngressHarness(t, 0)
		var heapRatio atomic.Int64 // stored as basis points (8500 = 0.85)
		heapRatio.Store(8600)

		srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots:          h.snapshots,
			Broker:             h.broker,
			PluginsDir:         h.pluginsD,
			Audit:              h.audit,
			NowMs:              func() int64 { return h.clk.Now().UnixMilli() },
			HeapUsageRatioFunc: func() float64 { return float64(heapRatio.Load()) / 10000.0 },
		})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		defer func() { _ = srv.Close(context.Background()) }()
		h.httpSrv.Config.Handler = srv

		body := []byte(`{"model":"acme/chat-1","stream":false,"messages":[{"role":"user","content":"ping"}]}`)
		req, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		req.Header.Set("Content-Type", "application/json")

		start := time.Now()
		resp, err := h.client.Do(req)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("overload Do: %v", err)
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(respBody), `"gateway_overloaded"`) {
			t.Fatalf("expected 503 gateway_overloaded, got %d body=%s", resp.StatusCode, string(respBody))
		}
		if strings.TrimSpace(resp.Header.Get("Retry-After")) == "" {
			t.Fatalf("expected Retry-After header on 503 gateway_overloaded")
		}
		if elapsed > 250*time.Millisecond {
			t.Fatalf("expected sub-millisecond fast shed, took %v", elapsed)
		}

		heapRatio.Store(4000) // Recover to 40%
		reqOK, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(body))
		reqOK.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		reqOK.Header.Set("Content-Type", "application/json")
		respOK, err := h.client.Do(reqOK)
		if err != nil {
			t.Fatalf("recovery Do: %v", err)
		}
		_, _ = io.Copy(io.Discard, respOK.Body)
		_ = respOK.Body.Close()
		if respOK.StatusCode != http.StatusOK {
			t.Fatalf("expected instant 200 recovery after heap subsides, got %d", respOK.StatusCode)
		}
	})

	t.Run("stream_90_percent_protects_inflight_and_sheds_excess", func(t *testing.T) {
		h := setupIngressHarness(t, 0)
		h.fake.SetDefaultFault("chunk_delay_ms=150")

		srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots:                h.snapshots,
			Broker:                   h.broker,
			PluginsDir:               h.pluginsD,
			Audit:                    h.audit,
			NowMs:                    func() int64 { return h.clk.Now().UnixMilli() },
			MaxNodeConcurrentStreams: 1, // 90% threshold -> 1 active stream saturates node ceiling
			MaxHeapLimitRatio:        -1,
		})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		defer func() { _ = srv.Close(context.Background()) }()
		h.httpSrv.Config.Handler = srv

		streamBody := []byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"hold"}]}`)
		streamReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(streamBody))
		streamReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		streamReq.Header.Set("Content-Type", "application/json")
		resp, err := h.client.Do(streamReq)
		if err != nil {
			t.Fatalf("start stream: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("initial stream status=%d", resp.StatusCode)
		}
		reader := bufio.NewReader(resp.Body)
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read first SSE line: %v", err)
		}

		// Wait briefly until activeStreams registers >= 1 while stream stays active.
		deadline := time.Now().Add(2 * time.Second)
		for srv.ActiveStreamCount() < 1 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if srv.ActiveStreamCount() < 1 {
			t.Fatalf("expected active stream count >= 1, got %d", srv.ActiveStreamCount())
		}
		if srv.ActiveStreamCount() >= 1 {
			syncBody := []byte(`{"model":"acme/chat-1","stream":false,"messages":[{"role":"user","content":"excess"}]}`)
			excessReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(syncBody))
			excessReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
			excessReq.Header.Set("Content-Type", "application/json")
			exResp, err := h.client.Do(excessReq)
			if err != nil {
				t.Fatalf("excess request: %v", err)
			}
			exBytes, _ := io.ReadAll(exResp.Body)
			_ = exResp.Body.Close()
			if exResp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(exBytes), `"gateway_overloaded"`) {
				t.Fatalf("expected 503 gateway_overloaded while active stream saturated ceiling, got %d %s", exResp.StatusCode, string(exBytes))
			}
		}

		// Finish consuming held stream and confirm it completes cleanly + recovery succeeds.
		remBytes, _ := io.ReadAll(reader)
		_ = resp.Body.Close()
		if !strings.Contains(string(remBytes), "[DONE]") {
			t.Fatalf("expected held stream to finish with [DONE], got %q", string(remBytes))
		}
		deadline = time.Now().Add(2 * time.Second)
		for srv.ActiveStreamCount() > 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}

		syncBody := []byte(`{"model":"acme/chat-1","stream":false,"messages":[{"role":"user","content":"recovered"}]}`)
		recReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader(syncBody))
		recReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		recReq.Header.Set("Content-Type", "application/json")
		recResp, err := h.client.Do(recReq)
		if err != nil {
			t.Fatalf("recovered request: %v", err)
		}
		_, _ = io.Copy(io.Discard, recResp.Body)
		_ = recResp.Body.Close()
		if recResp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 after held stream closed, got %d", recResp.StatusCode)
		}
	})

	t.Run("responses_active_stream_cap_shedding", func(t *testing.T) {
		h := setupIngressHarness(t, 0)
		h.fake.SetDefaultFault("chunk_delay_ms=150")
		defer h.fake.SetDefaultFault("")

		srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
			Snapshots:                h.snapshots,
			Broker:                   h.broker,
			PluginsDir:               h.pluginsD,
			Audit:                    h.audit,
			NowMs:                    func() int64 { return h.clk.Now().UnixMilli() },
			MaxNodeConcurrentStreams: 1,
			MaxHeapLimitRatio:        -1,
		})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
		defer func() { _ = srv.Close(context.Background()) }()
		h.httpSrv.Config.Handler = srv

		streamBody := []byte(`{"model":"acme/chat-1","stream":true,"input":"hold"}`)
		streamReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/responses", bytes.NewReader(streamBody))
		streamReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		streamReq.Header.Set("Content-Type", "application/json")
		resp, err := h.client.Do(streamReq)
		if err != nil {
			t.Fatalf("start responses stream: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("initial responses stream status=%d", resp.StatusCode)
		}
		reader := bufio.NewReader(resp.Body)
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatalf("read first responses SSE line: %v", err)
		}

		deadline := time.Now().Add(2 * time.Second)
		for srv.ActiveStreamCount() < 1 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if srv.ActiveStreamCount() < 1 {
			t.Fatalf("expected active stream count >= 1 on /v1/responses, got %d", srv.ActiveStreamCount())
		}
		excessReq, _ := http.NewRequest(http.MethodPost, h.httpSrv.URL+"/v1/responses", bytes.NewReader([]byte(`{"model":"acme/chat-1","stream":false,"input":"excess"}`)))
		excessReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		excessReq.Header.Set("Content-Type", "application/json")
		exResp, err := h.client.Do(excessReq)
		if err != nil {
			t.Fatalf("excess responses request: %v", err)
		}
		exBytes, _ := io.ReadAll(exResp.Body)
		_ = exResp.Body.Close()
		if exResp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(exBytes), `"gateway_overloaded"`) {
			t.Fatalf("expected 503 gateway_overloaded on /v1/responses, got %d %s", exResp.StatusCode, string(exBytes))
		}
		remBytes, _ := io.ReadAll(reader)
		_ = resp.Body.Close()
		if !strings.Contains(string(remBytes), "response.completed") {
			t.Fatalf("expected held /v1/responses stream to finish with response.completed, got %q", string(remBytes))
		}
	})
}
