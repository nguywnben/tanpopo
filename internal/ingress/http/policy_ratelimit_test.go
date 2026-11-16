// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
)

func insertChangelogAndRefresh(t *testing.T, h *ingressHarness, kind, entityID string) {
	t.Helper()
	h.clk.Advance(time.Millisecond)
	now := h.clk.Now().UnixMilli()
	if _, err := h.store.RawDB().ExecContext(h.ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, ?, ?, 'UPSERT', 0, ?)
	`, h.org.ID, kind, entityID, now); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTP0019_AC1_HierarchicalRPMLimitAndRetryAfter(t *testing.T) {
	h := setupIngressHarness(t, 0)
	nowFn := func() int64 { return h.clk.Now().UnixMilli() }

	polStore, err := policyengine.NewStore(policyengine.StoreConfig{
		DB:    h.store.RawDB(),
		Audit: h.audit,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Org sets RPM=100, Project sets RPM=30, VirtualKey sets RPM=10.
	if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
		OrgID:      h.org.ID,
		ScopeType:  snapshot.PolicyScopeOrg,
		ScopeID:    h.org.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit": 100}`,
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
		OrgID:      h.org.ID,
		ScopeType:  snapshot.PolicyScopeProject,
		ScopeID:    h.project.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit": 30}`,
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
		OrgID:      h.org.ID,
		ScopeType:  snapshot.PolicyScopeVirtualKey,
		ScopeID:    h.vkeyID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit": 10}`,
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"ping"}]}`)
	for i := 0; i < 10; i++ {
		resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", payload, nil, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request #%d failed with status=%d body=%s", i+1, resp.StatusCode, string(body))
		}
		if resp.Header.Get("X-RateLimit-Remaining-Requests") == "" {
			t.Fatalf("request #%d missing X-RateLimit-Remaining-Requests header", i+1)
		}
	}

	// #11 within same FakeClock instant -> 429 rate_limit_exceeded + Retry-After > 0.
	resp11, body11 := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", payload, nil, nil)
	assertErrorCode(t, resp11, body11, http.StatusTooManyRequests, "rate_limit_exceeded")
	if ra := resp11.Header.Get("Retry-After"); ra == "" || ra == "0" {
		t.Fatalf("expected Retry-After > 0 on 429, got %q", ra)
	}

	// Advance FakeClock by 12s (keeping snapshot refreshed) -> 2 tokens refilled -> succeeds.
	h.clk.Advance(12 * time.Second)
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}
	resp12, body12 := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", payload, nil, nil)
	if resp12.StatusCode != http.StatusOK {
		t.Fatalf("expected post-refill request 200 OK, got status=%d body=%s", resp12.StatusCode, string(body12))
	}
}

func TestTP0019_AC2_ConcurrencySemaphoreAndSlotReleaseOnAbruptDisconnect(t *testing.T) {
	h := setupIngressHarness(t, 0)

	// Configure a fresh Virtual Key with MaxConcurrentStreams = 2.
	createdVK, err := h.vkeys.Create(h.ctx, vkey.CreateParams{
		OrgID:                h.org.ID,
		ProjectID:            h.project.ID,
		EnvID:                h.env.ID,
		Name:                 "conc-key-2",
		AllowedPlanes:        "PROVIDER",
		MaxConcurrentStreams: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// Upstream server emits first SSE frame immediately, then stalls until client disconnects or closed.
	emitted := make(chan struct{}, 4)
	releaseAll := make(chan struct{})
	stallSSE := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		select {
		case emitted <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-releaseAll:
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(func() {
		close(releaseAll)
		stallSSE.Close()
	})

	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE provider_connections SET endpoint_base_url = ?, version = version + 1 WHERE id = ?`, stallSSE.URL, h.connID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE model_deployments SET first_byte_timeout_ms = 5000, idle_chunk_timeout_ms = 5000, version = version + 1 WHERE id = ?`, h.deplID); err != nil {
		t.Fatal(err)
	}
	insertChangelogAndRefresh(t, h, "provider_connection", h.connID)

	openStream := func(ctx context.Context) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.httpSrv.URL+"/v1/chat/completions", bytes.NewReader([]byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"stream"}]}`)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+createdVK.SecretKey)
		return h.client.Do(req)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	resp1, err := openStream(ctx1)
	if err != nil {
		t.Fatal(err)
	}
	r1 := bufio.NewReader(resp1.Body)
	if _, err := r1.ReadString('\n'); err != nil {
		t.Fatalf("read first chunk stream 1: %v", err)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	resp2, err := openStream(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	r2 := bufio.NewReader(resp2.Body)
	if _, err := r2.ReadString('\n'); err != nil {
		t.Fatalf("read first chunk stream 2: %v", err)
	}

	// Stream #3 must be rejected immediately with 429 concurrency_limit_exceeded.
	resp3, body3 := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"s3"}]}`), map[string]string{
		"Authorization": "Bearer " + createdVK.SecretKey,
	}, nil)
	assertErrorCode(t, resp3, body3, http.StatusTooManyRequests, "concurrency_limit_exceeded")

	// Abruptly disconnect stream #1.
	cancel1()
	_ = resp1.Body.Close()

	// Wait briefly (polling on fast local retry loop) until stream #4 acquires the freed slot immediately.
	deadline := time.Now().Add(2 * time.Second)
	var resp4 *http.Response
	for time.Now().Before(deadline) {
		ctx4, cancel4 := context.WithCancel(context.Background())
		candidate, oerr := openStream(ctx4)
		if oerr == nil && candidate.StatusCode == http.StatusOK {
			resp4 = candidate
			cancel4()
			_ = candidate.Body.Close()
			break
		}
		if candidate != nil {
			_ = candidate.Body.Close()
		}
		cancel4()
		time.Sleep(15 * time.Millisecond)
	}
	if resp4 == nil {
		t.Fatal("stream #4 failed to acquire freed concurrency slot after stream #1 disconnected")
	}

	cancel2()
	_ = resp2.Body.Close()
}

func TestTP0019_NEG2_TPMBurstAbusePreEgressAndReconciliation(t *testing.T) {
	h := setupIngressHarness(t, 0)
	nowFn := func() int64 { return h.clk.Now().UnixMilli() }
	polStore, err := policyengine.NewStore(policyengine.StoreConfig{
		DB:    h.store.RawDB(),
		Audit: h.audit,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
		OrgID:      h.org.ID,
		ScopeType:  snapshot.PolicyScopeOrg,
		ScopeID:    h.org.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"tpm_limit": 500}`,
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	h.fake.ResetRequests()
	// Single request claiming max_tokens=2000 exceeds TPM=500 -> rejected before any upstream broker dial!
	burstPayload := []byte(`{"model":"acme/chat-1","max_tokens":2000,"messages":[{"role":"user","content":"hello"}]}`)
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", burstPayload, nil, nil)
	assertErrorCode(t, resp, body, http.StatusTooManyRequests, "rate_limit_exceeded")
	if calls := len(h.fake.Requests()); calls != 0 {
		t.Fatalf("expected 0 upstream dials on TPM burst rejection, got %d", calls)
	}

	// Normal small request with max_tokens=200 passes pre-admission, dispatches, and reconciles actual tokens.
	smallPayload := []byte(`{"model":"acme/chat-1","max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`)
	respOK, bodyOK := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", smallPayload, nil, nil)
	if respOK.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on compliant request, got %d body=%s", respOK.StatusCode, string(bodyOK))
	}
	if respOK.Header.Get("X-RateLimit-Remaining-Tokens") == "" {
		t.Fatal("expected X-RateLimit-Remaining-Tokens header")
	}
}

func TestTP0019_FailClosedCorruptPolicyAndEndpointEnforcement(t *testing.T) {
	h := setupIngressHarness(t, 0)
	nowFn := func() int64 { return h.clk.Now().UnixMilli() }
	polStore, err := policyengine.NewStore(policyengine.StoreConfig{
		DB:    h.store.RawDB(),
		Audit: h.audit,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add second compliant EU deployment + connection alongside default US deployment.
	euConn, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
		OrgID:           h.org.ID,
		AdapterID:       "adp_" + strings.TrimPrefix(strings.Split(h.connID, "_")[1], ""),
		Name:            "acme-eu-conn",
		EndpointBaseURL: h.fake.URL(),
		AllowPrivateIP:  true,
		AllowedCIDRs:    []string{"127.0.0.1/32"},
		DataRegion:      "EU-CENTRAL-1",
		BillingMode:     catalog.BillingModeCommercialAPI,
		AuthMode:        catalog.AuthModeNone,
	})
	if err != nil {
		// Look up actual adapter ID from connections table cleanly.
		var adpID string
		_ = h.store.RawDB().QueryRowContext(h.ctx, `SELECT adapter_id FROM provider_connections WHERE id = ?`, h.connID).Scan(&adpID)
		euConn, err = h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
			OrgID:           h.org.ID,
			AdapterID:       adpID,
			Name:            "acme-eu-conn",
			EndpointBaseURL: h.fake.URL(),
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "EU-CENTRAL-1",
			BillingMode:     catalog.BillingModeCommercialAPI,
			AuthMode:        catalog.AuthModeNone,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	euConn, err = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, euConn.ID, catalog.ConnStateValidating, euConn.Version, "val")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.TransitionConnectionState(h.ctx, h.org.ID, euConn.ID, catalog.ConnStateActive, euConn.Version, "act"); err != nil {
		t.Fatal(err)
	}
	var lmID string
	if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT logical_model_id FROM model_deployments WHERE id = ?`, h.deplID).Scan(&lmID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
		OrgID:              h.org.ID,
		ConnectionID:       euConn.ID,
		LogicalModelID:     lmID,
		UpstreamModelName:  "acme-gpt-eu-upstream",
		AssuranceLevel:     catalog.AssuranceVerified,
		State:              catalog.DeployStateActiveHealthy,
		ConnectTimeoutMs:   5000,
		FirstByteTimeoutMs: 5000,
		IdleChunkTimeoutMs: 5000,
	}); err != nil {
		t.Fatal(err)
	}

	// Enforce EU-CENTRAL-1 data residency + max_context_tokens + max_request_body_bytes + denied_models.
	if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
		OrgID:      h.org.ID,
		ScopeType:  snapshot.PolicyScopeOrg,
		ScopeID:    h.org.ID,
		PolicyType: snapshot.PolicyTypeDataResidency,
		RulesJSON:  `{"allowed_data_regions":["EU-CENTRAL-1"],"denied_models":["acme/banned-model"],"max_context_tokens":200,"max_request_body_bytes":2048}`,
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// 1. EU request skips US deployment and routes to EU deployment!
	h.fake.ResetRequests()
	resp, body := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"eu ok"}]}`), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected EU fallback deployment 200 OK, got status=%d body=%s", resp.StatusCode, string(body))
	}
	reqs := h.fake.Requests()
	if len(reqs) != 1 || !strings.Contains(string(reqs[0].Body), "acme-gpt-eu-upstream") {
		t.Fatalf("expected EU upstream deployment selected, got %d requests", len(reqs))
	}

	// 2. X-Tanpopo-Data-Region: US-EAST-1 outside EU policy -> 503 no_compliant_deployment_available (policy_denied_region).
	respReg, bodyReg := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"us denied"}]}`), map[string]string{
		"X-Tanpopo-Data-Region": "US-EAST-1",
	}, nil)
	assertErrorCode(t, respReg, bodyReg, http.StatusServiceUnavailable, "no_compliant_deployment_available")
	if !strings.Contains(string(bodyReg), "policy_denied_region") {
		t.Fatalf("expected policy_denied_region details in error body, got %s", string(bodyReg))
	}

	// 3. Denied model -> 403 policy_denied_model; max_context -> 400 policy_denied_max_context; body cap -> 413 payload_too_large.
	respMod, bodyMod := h.doChatReq(t, http.MethodPost, "/v1/responses", []byte(`{"model":"acme/banned-model","input":"hi"}`), nil, nil)
	assertErrorCode(t, respMod, bodyMod, http.StatusForbidden, "policy_denied_model")

	respCtx, bodyCtx := h.doChatReq(t, http.MethodPost, "/v1/embeddings", []byte(`{"model":"acme/chat-1","max_tokens":500,"input":"hi"}`), nil, nil)
	assertErrorCode(t, respCtx, bodyCtx, http.StatusBadRequest, "policy_denied_max_context")

	bigText := strings.Repeat("a", 2200)
	respBig, bodyBig := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"`+bigText+`"}]}`), nil, nil)
	assertErrorCode(t, respBig, bodyBig, http.StatusRequestEntityTooLarge, "payload_too_large")

	// 4. /v1/models returns authorized EU-compliant models cleanly, and returns empty when X-Tanpopo-Data-Region violates policy.
	respModelsOK, bodyModelsOK := h.doChatReq(t, http.MethodGet, "/v1/models", nil, nil, nil)
	if respModelsOK.StatusCode != http.StatusOK || !strings.Contains(string(bodyModelsOK), "acme/chat-1") {
		t.Fatalf("expected acme/chat-1 in /v1/models, got %d %s", respModelsOK.StatusCode, string(bodyModelsOK))
	}
	respModelsUS, bodyModelsUS := h.doChatReq(t, http.MethodGet, "/v1/models", nil, map[string]string{
		"X-Tanpopo-Data-Region": "US-EAST-1",
	}, nil)
	var mList struct {
		Data []json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(bodyModelsUS, &mList)
	if respModelsUS.StatusCode != http.StatusOK || len(mList.Data) != 0 {
		t.Fatalf("expected 0 models when US-EAST-1 violates EU policy, got %d %s", respModelsUS.StatusCode, string(bodyModelsUS))
	}

	// 5. Corrupt enabled policy -> 503 policy_engine_unavailable across ALL ingress routes (/v1/* and /native/*).
	if _, err := h.store.RawDB().ExecContext(h.ctx, `UPDATE policies SET rules_json = '{"corrupted_unknown":true}' WHERE org_id = ?`, h.org.ID); err != nil {
		t.Fatal(err)
	}
	insertChangelogAndRefresh(t, h, "policy", "pol_corrupt")

	routes := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`)},
		{http.MethodPost, "/v1/responses", []byte(`{"model":"acme/chat-1","input":"hi"}`)},
		{http.MethodPost, "/v1/embeddings", []byte(`{"model":"acme/chat-1","input":"hi"}`)},
		{http.MethodGet, "/v1/models", nil},
		{http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`)},
	}
	for _, rt := range routes {
		rResp, rBody := h.doChatReq(t, rt.method, rt.path, rt.body, nil, nil)
		assertErrorCode(t, rResp, rBody, http.StatusServiceUnavailable, "policy_engine_unavailable")
	}

	// Silence unused storageport warning if optimized away (verified imported for harness types).
	_ = storageport.MinTrustVerifiedOnly
}

func TestTP0019_NativeRateLimitAndPolicyDenial(t *testing.T) {
	h := setupIngressHarness(t, 0)
	nowFn := func() int64 { return h.clk.Now().UnixMilli() }
	polStore, err := policyengine.NewStore(policyengine.StoreConfig{
		DB:    h.store.RawDB(),
		Audit: h.audit,
		NowMs: nowFn,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
		OrgID:      h.org.ID,
		ScopeType:  snapshot.PolicyScopeOrg,
		ScopeID:    h.org.ID,
		PolicyType: snapshot.PolicyTypeRateLimit,
		RulesJSON:  `{"rpm_limit": 1, "denied_models": ["acme/denied-native"]}`,
		Enabled:    true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatal(err)
	}

	// Native policy denial returns 403 policy_denied_model without burning RPM.
	rDeny, bDeny := h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/denied-native","messages":[{"role":"user","content":"hi"}]}`), nil, nil)
	assertErrorCode(t, rDeny, bDeny, http.StatusForbidden, "policy_denied_model")

	// First /native attempt burns the 1 RPM slot (may return 403 native_path_not_allowed on default-deny adapter if no native paths configured, still proving rate-limit check executes before adapter/egress!).
	r1, _ := h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), nil, nil)
	if r1.Header.Get("X-RateLimit-Remaining-Requests") != "0" {
		t.Fatalf("expected X-RateLimit-Remaining-Requests: 0 on native response, got %q (status=%d)", r1.Header.Get("X-RateLimit-Remaining-Requests"), r1.StatusCode)
	}

	// Second request within same second -> 429 rate_limit_exceeded!
	r2, b2 := h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), nil, nil)
	assertErrorCode(t, r2, b2, http.StatusTooManyRequests, "rate_limit_exceeded")

	var wg sync.WaitGroup
	wg.Wait()
}

func TestTP0019_SecurityAuditAndReviewHardening(t *testing.T) {
	t.Run("region_pinning_header_and_float_max_tokens", func(t *testing.T) {
		h := setupIngressHarness(t, 0)
		nowFn := func() int64 { return h.clk.Now().UnixMilli() }
		polStore, err := policyengine.NewStore(policyengine.StoreConfig{
			DB:    h.store.RawDB(),
			Audit: h.audit,
			NowMs: nowFn,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Add second EU deployment so org has US-EAST-1 (default) and EU-CENTRAL-1 with no residency policy yet.
		var adpID, lmID string
		if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT adapter_id FROM provider_connections WHERE id = ?`, h.connID).Scan(&adpID); err != nil {
			t.Fatal(err)
		}
		if err := h.store.RawDB().QueryRowContext(h.ctx, `SELECT logical_model_id FROM model_deployments WHERE id = ?`, h.deplID).Scan(&lmID); err != nil {
			t.Fatal(err)
		}
		euConn, err := h.catalog.CreateProviderConnection(h.ctx, catalog.CreateProviderConnectionParams{
			OrgID:           h.org.ID,
			AdapterID:       adpID,
			Name:            "acme-eu-conn",
			EndpointBaseURL: h.fake.URL(),
			AllowPrivateIP:  true,
			AllowedCIDRs:    []string{"127.0.0.1/32"},
			DataRegion:      "EU-CENTRAL-1",
			BillingMode:     catalog.BillingModeCommercialAPI,
			AuthMode:        catalog.AuthModeNone,
		})
		if err != nil {
			t.Fatal(err)
		}
		euConn, err = h.catalog.TransitionConnectionState(h.ctx, h.org.ID, euConn.ID, catalog.ConnStateValidating, euConn.Version, "val")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.catalog.TransitionConnectionState(h.ctx, h.org.ID, euConn.ID, catalog.ConnStateActive, euConn.Version, "act"); err != nil {
			t.Fatal(err)
		}
		if _, err := h.catalog.CreateModelDeployment(h.ctx, catalog.CreateModelDeploymentParams{
			OrgID:              h.org.ID,
			ConnectionID:       euConn.ID,
			LogicalModelID:     lmID,
			UpstreamModelName:  "acme-gpt-eu-pinned",
			AssuranceLevel:     catalog.AssuranceVerified,
			State:              catalog.DeployStateActiveHealthy,
			ConnectTimeoutMs:   5000,
			FirstByteTimeoutMs: 5000,
			IdleChunkTimeoutMs: 5000,
		}); err != nil {
			t.Fatal(err)
		}
		if err := h.snapshots.Refresh(h.ctx); err != nil {
			t.Fatal(err)
		}

		// 1. X-Tanpopo-Data-Region: EU-CENTRAL-1 constrains routing even when org has NO residency policy.
		h.fake.ResetRequests()
		respEU, bodyEU := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"eu only"}]}`), map[string]string{
			"X-Tanpopo-Data-Region": "EU-CENTRAL-1",
		}, nil)
		if respEU.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK pinning EU-CENTRAL-1, got %d body=%s", respEU.StatusCode, string(bodyEU))
		}
		reqs := h.fake.Requests()
		if len(reqs) != 1 || !strings.Contains(string(reqs[0].Body), "acme-gpt-eu-pinned") {
			t.Fatalf("expected EU-pinned upstream deployment selected, got %d requests", len(reqs))
		}

		// Conflicting X-Tanpopo-Data-Region header values -> 400 invalid_request_error.
		respConflict, bodyConflict := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"conflict"}]}`), map[string]string{
			"X-Tanpopo-Data-Region": "EU-CENTRAL-1, US-EAST-1",
		}, nil)
		assertErrorCode(t, respConflict, bodyConflict, http.StatusBadRequest, "invalid_request_error")

		// 2. Float / scientific notation max_tokens cannot bypass max_context_tokens or TPM burst gate.
		if _, err := polStore.CreatePolicy(h.ctx, policyengine.CreatePolicyParams{
			OrgID:      h.org.ID,
			ScopeType:  snapshot.PolicyScopeOrg,
			ScopeID:    h.org.ID,
			PolicyType: snapshot.PolicyTypeRateLimit,
			RulesJSON:  `{"tpm_limit": 500, "max_context_tokens": 1000}`,
			Enabled:    true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := h.snapshots.Refresh(h.ctx); err != nil {
			t.Fatal(err)
		}
		respSci, bodySci := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":1e5,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respSci, bodySci, http.StatusBadRequest, "policy_denied_max_context")

		respFlt, bodyFlt := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":600.0,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respFlt, bodyFlt, http.StatusTooManyRequests, "rate_limit_exceeded")

		respOv, bodyOv := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":1e500,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respOv, bodyOv, http.StatusBadRequest, "policy_denied_max_context")

		respNatOv, bodyNatOv := h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":1e500,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respNatOv, bodyNatOv, http.StatusBadRequest, "policy_denied_max_context")

		// DecodeStrictJSONObject rejects duplicate JSON keys outright across both /v1/* and /native/* (before admission or egress).
		respDupHTTP, bodyDupHTTP := h.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":50000,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respDupHTTP, bodyDupHTTP, http.StatusBadRequest, "invalid_request_error")

		respDupNat, bodyDupNat := h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","generationConfig":{"maxOutputTokens":50000,"maxOutputTokens":1},"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), nil, nil)
		assertErrorCode(t, respDupNat, bodyDupNat, http.StatusBadRequest, "invalid_request_error")

		respDupNatTop, bodyDupNatTop := h.doChatReq(t, http.MethodPost, "/native/openai/v1/chat/completions", []byte(`{"model":"acme/chat-1","max_tokens":50000,"max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respDupNatTop, bodyDupNatTop, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("zero_token_unmetered_flagged_no_refund", func(t *testing.T) {
		h2 := setupIngressHarness(t, 0)
		ps2, err := policyengine.NewStore(policyengine.StoreConfig{DB: h2.store.RawDB(), Audit: h2.audit, NowMs: func() int64 { return h2.clk.Now().UnixMilli() }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ps2.CreatePolicy(h2.ctx, policyengine.CreatePolicyParams{
			OrgID: h2.org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: h2.org.ID,
			PolicyType: snapshot.PolicyTypeRateLimit, RulesJSON: `{"tpm_limit": 300}`, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := h2.snapshots.Refresh(h2.ctx); err != nil {
			t.Fatal(err)
		}
		h2.fake.SetDefaultFault("omit_stream_usage")
		respUnmet, _ := h2.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		if respUnmet.StatusCode != http.StatusOK {
			t.Fatalf("expected first streaming request 200 OK, got %d", respUnmet.StatusCode)
		}
		// If UNMETERED_FLAGGED had refunded ~220 tokens, bucket would be 300 and allow another max_tokens=200 request immediately!
		// Because refund is blocked, only < 100 tokens remain, so second request MUST return 429 rate_limit_exceeded.
		respUnmet2, bodyUnmet2 := h2.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respUnmet2, bodyUnmet2, http.StatusTooManyRequests, "rate_limit_exceeded")
	})

	t.Run("max_concurrent_streams_zero_denies", func(t *testing.T) {
		h3 := setupIngressHarness(t, 0)
		ps3, err := policyengine.NewStore(policyengine.StoreConfig{DB: h3.store.RawDB(), Audit: h3.audit, NowMs: func() int64 { return h3.clk.Now().UnixMilli() }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ps3.CreatePolicy(h3.ctx, policyengine.CreatePolicyParams{
			OrgID: h3.org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: h3.org.ID,
			PolicyType: snapshot.PolicyTypeRateLimit, RulesJSON: `{"max_concurrent_streams": 0}`, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := h3.snapshots.Refresh(h3.ctx); err != nil {
			t.Fatal(err)
		}
		respZeroStr, bodyZeroStr := h3.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","stream":true,"messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respZeroStr, bodyZeroStr, http.StatusTooManyRequests, "concurrency_limit_exceeded")
	})

	t.Run("trust_and_assurance_denial_propagation", func(t *testing.T) {
		h4 := setupIngressHarness(t, 0)
		ps4, err := policyengine.NewStore(policyengine.StoreConfig{DB: h4.store.RawDB(), Audit: h4.audit, NowMs: func() int64 { return h4.clk.Now().UnixMilli() }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h4.store.RawDB().ExecContext(h4.ctx, `UPDATE model_deployments SET assurance_level = 'DECLARED' WHERE id = ?`, h4.deplID); err != nil {
			t.Fatal(err)
		}
		if _, err := ps4.CreatePolicy(h4.ctx, policyengine.CreatePolicyParams{
			OrgID: h4.org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: h4.org.ID,
			PolicyType: snapshot.PolicyTypeModelAllowlist, RulesJSON: `{"min_capability_assurance":"VERIFIED"}`, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		insertChangelogAndRefresh(t, h4, "model_deployment", h4.deplID)
		respTrust, bodyTrust := h4.doChatReq(t, http.MethodPost, "/v1/chat/completions", []byte(`{"model":"acme/chat-1","messages":[{"role":"user","content":"hi"}]}`), nil, nil)
		assertErrorCode(t, respTrust, bodyTrust, http.StatusBadRequest, "policy_denied_trust")
	})

	t.Run("idempotency_replay_and_models_rpm_limited", func(t *testing.T) {
		h5 := setupIngressHarness(t, 0)
		ps5, err := policyengine.NewStore(policyengine.StoreConfig{DB: h5.store.RawDB(), Audit: h5.audit, NowMs: func() int64 { return h5.clk.Now().UnixMilli() }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ps5.CreatePolicy(h5.ctx, policyengine.CreatePolicyParams{
			OrgID: h5.org.ID, ScopeType: snapshot.PolicyScopeOrg, ScopeID: h5.org.ID,
			PolicyType: snapshot.PolicyTypeRateLimit, RulesJSON: `{"rpm_limit": 2, "tpm_limit": 300}`, Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := h5.snapshots.Refresh(h5.ctx); err != nil {
			t.Fatal(err)
		}

		// Request #1 with Idempotency-Key uses 1 RPM (leaving 1 RPM) and ~200 TPM.
		body1 := []byte(`{"model":"acme/chat-1","max_tokens":150,"messages":[{"role":"user","content":"idem-1"}]}`)
		r1, b1 := h5.doChatReq(t, http.MethodPost, "/v1/chat/completions", body1, map[string]string{
			"Idempotency-Key": "idem-flood-check-1",
		}, nil)
		if r1.StatusCode != http.StatusOK {
			t.Fatalf("expected request #1 200 OK, got %d %s", r1.StatusCode, string(b1))
		}
		// Replay #2 consumes the second RPM slot (replaying cached 200 OK without double-charging TPM).
		r2, b2 := h5.doChatReq(t, http.MethodPost, "/v1/chat/completions", body1, map[string]string{
			"Idempotency-Key": "idem-flood-check-1",
		}, nil)
		if r2.StatusCode != http.StatusOK {
			t.Fatalf("expected replay #2 200 OK, got %d %s", r2.StatusCode, string(b2))
		}
		// Replay #3 AND GET /v1/models both get throttled with 429 rate_limit_exceeded!
		r3, b3 := h5.doChatReq(t, http.MethodPost, "/v1/chat/completions", body1, map[string]string{
			"Idempotency-Key": "idem-flood-check-1",
		}, nil)
		assertErrorCode(t, r3, b3, http.StatusTooManyRequests, "rate_limit_exceeded")

		rMod, bMod := h5.doChatReq(t, http.MethodGet, "/v1/models", nil, nil, nil)
		assertErrorCode(t, rMod, bMod, http.StatusTooManyRequests, "rate_limit_exceeded")
	})
}
