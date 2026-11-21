// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/ingress/cache"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
)

func TestDataPlaneExactResponseCacheE2E(t *testing.T) {
	h := setupIngressHarness(t, 0)
	ctx := h.ctx

	ec, err := cache.NewExactCache(cache.Config{
		DB:    h.store.RawDB(),
		Vault: h.vault,
		NowMs: func() int64 { return h.clk.Now().UnixMilli() },
	})
	if err != nil {
		t.Fatalf("cache.NewExactCache: %v", err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:            h.snapshots,
		Broker:               h.broker,
		PluginsDir:           h.pluginsD,
		Audit:                h.audit,
		NowMs:                func() int64 { return h.clk.Now().UnixMilli() },
		SSEHeartbeatInterval: 200 * time.Millisecond,
		ResponseCache:        ec,
	})
	if err != nil {
		t.Fatalf("ingresshttp.NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(context.Background()) })

	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	send := func(body string, extraHdr map[string]string) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, httpSrv.URL+"/v1/chat/completions", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		for k, v := range extraHdr {
			req.Header.Set(k, v)
		}
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("Do /v1/chat/completions: %v", err)
		}
		_ = resp.Body.Close()
		return resp
	}

	detBody := `{"model":"acme/chat-1","temperature":0,"messages":[{"role":"user","content":"deterministic question"}]}`

	// 1. Default disabled -> BYPASS (upstream invoked once).
	r0 := send(detBody, nil)
	if r0.StatusCode != http.StatusOK || r0.Header.Get("X-Tanpopo-Cache") != "BYPASS" {
		t.Fatalf("expected 200 + X-Tanpopo-Cache: BYPASS when policy unset, got status=%d cache=%q", r0.StatusCode, r0.Header.Get("X-Tanpopo-Cache"))
	}
	if len(h.fake.Requests()) != 1 {
		t.Fatalf("expected 1 upstream call, got %d", len(h.fake.Requests()))
	}

	// 2. Enable RESPONSE_CACHE policy for project -> first request MISS (upstream count=2), second request HIT (upstream count stays 2!).
	pStore, err := policyengine.NewStore(policyengine.StoreConfig{DB: h.store.RawDB(), Audit: h.audit})
	if err != nil {
		t.Fatalf("policyengine.NewStore: %v", err)
	}
	if _, err := pStore.CreatePolicy(ctx, policyengine.CreatePolicyParams{
		OrgID:         h.org.ID,
		ScopeType:     snapshot.PolicyScopeProject,
		ScopeID:       h.project.ID,
		PolicyType:    snapshot.PolicyTypeResponseCache,
		FailureMode:   snapshot.PolicyFailureClosed,
		RulesJSON:     `{"enabled":true,"ttl_seconds":300}`,
		Enabled:       true,
		ActorKind:     "SYSTEM",
		ActorID:       "system",
		EntryPoint:    "ADMIN_API",
	}); err != nil {
		t.Fatalf("policy CreatePolicy RESPONSE_CACHE: %v", err)
	}
	if err := h.snapshots.Refresh(ctx); err != nil {
		t.Fatalf("snapshots.Refresh: %v", err)
	}

	r1 := send(detBody, nil)
	if r1.StatusCode != http.StatusOK || r1.Header.Get("X-Tanpopo-Cache") != "MISS" {
		t.Fatalf("expected 200 + MISS, got status=%d cache=%q", r1.StatusCode, r1.Header.Get("X-Tanpopo-Cache"))
	}
	if len(h.fake.Requests()) != 2 {
		t.Fatalf("expected 2 upstream calls after MISS, got %d", len(h.fake.Requests()))
	}

	r2 := send(detBody, nil)
	if r2.StatusCode != http.StatusOK || r2.Header.Get("X-Tanpopo-Cache") != "HIT" {
		t.Fatalf("expected 200 + HIT, got status=%d cache=%q", r2.StatusCode, r2.Header.Get("X-Tanpopo-Cache"))
	}
	if len(h.fake.Requests()) != 2 {
		t.Fatalf("expected upstream call count to remain 2 on HIT, got %d", len(h.fake.Requests()))
	}

	// 3. Cache-Control: no-store -> BYPASS even with policy enabled (upstream count=3).
	r3 := send(detBody, map[string]string{"Cache-Control": "no-store"})
	if r3.StatusCode != http.StatusOK || r3.Header.Get("X-Tanpopo-Cache") != "BYPASS" {
		t.Fatalf("expected BYPASS on Cache-Control: no-store, got %d cache=%q", r3.StatusCode, r3.Header.Get("X-Tanpopo-Cache"))
	}
	if len(h.fake.Requests()) != 3 {
		t.Fatalf("expected upstream count 3 on no-store BYPASS, got %d", len(h.fake.Requests()))
	}

	// 4. Non-deterministic temperature != 0 -> BYPASS (upstream count=4).
	r4 := send(`{"model":"acme/chat-1","temperature":0.7,"messages":[{"role":"user","content":"deterministic question"}]}`, nil)
	if r4.StatusCode != http.StatusOK || r4.Header.Get("X-Tanpopo-Cache") != "BYPASS" {
		t.Fatalf("expected BYPASS when temperature=0.7, got %d cache=%q", r4.StatusCode, r4.Header.Get("X-Tanpopo-Cache"))
	}
	if len(h.fake.Requests()) != 4 {
		t.Fatalf("expected upstream count 4 on temp=0.7 BYPASS, got %d", len(h.fake.Requests()))
	}
}
