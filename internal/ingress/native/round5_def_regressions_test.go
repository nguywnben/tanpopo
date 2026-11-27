// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
)

func TestDEF083_StreamingUsageAccuracyHTTPRepeatableTrailers(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	h := setupNativeHarness(t, nil)
	nowFn := func() int64 { return h.clk.Now().UnixMilli() }

	telPipe, err := otel.NewPipeline(otel.PipelineConfig{
		ServiceName: "tanpopo-native-r5-test",
		DB:          h.store.RawDB(),
		NowMs:       nowFn,
	})
	if err != nil {
		t.Fatalf("otel.NewPipeline: %v", err)
	}
	t.Cleanup(func() { _ = telPipe.Shutdown(h.ctx) })

	lSvc, err := ledger.NewService(ledger.Config{DB: h.store.RawDB(), Audit: h.audit, NowMs: nowFn})
	if err != nil {
		t.Fatalf("ledger.NewService: %v", err)
	}
	if _, err := lSvc.UpsertLedger(h.ctx, ledger.UpsertLedgerParams{
		ID:              "bdg_nat_r5",
		OrgID:           h.org.ID,
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         h.org.ID,
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   0,
		PeriodEndMs:     nowFn() + 86_400_000,
		HardLimitMicros: 5_000_000,
	}); err != nil {
		t.Fatalf("UpsertLedger: %v", err)
	}

	srv, err := ingresshttp.NewServer(ingresshttp.ServerConfig{
		Snapshots:            h.snapshots,
		Broker:               h.broker,
		PluginsDir:           filepath.Join(h.tmp, "plugins"),
		Audit:                h.audit,
		NowMs:                nowFn,
		BudgetLedger:         lSvc,
		Telemetry:            telPipe,
		SSEHeartbeatInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("ingresshttp.NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(h.ctx) })
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	// Modes: "exact" -> upstream reports counts; "estimated" -> chunks emitted without usage; "unmetered" -> done chunk with zero counts and no usage
	var streamMode atomic.Int32 // 0=exact, 1=unmetered(no counts), 2=estimated(abort mid-stream)
	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		mode := streamMode.Load()
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if mode == 1 {
				_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_r5\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
				return
			}
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_r5\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n"))
			return
		}
		if bytes.Contains(body, []byte(`"stream":true`)) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			rc := http.NewResponseController(w)
			_, _ = w.Write([]byte("{\"model\":\"qwen2.5\",\"message\":{\"role\":\"assistant\",\"content\":\"hello world\"},\"done\":false}\n"))
			_ = rc.Flush()
			if mode == 2 {
				// Premature close without done chunk -> triggers ESTIMATED or UNMETERED_FLAGGED post-settlement
				return
			}
			if mode == 1 {
				// Done chunk with no token count fields -> unmetered / estimated without upstream counts
				_, _ = w.Write([]byte("{\"model\":\"qwen2.5\",\"done\":true}\n"))
				_ = rc.Flush()
				return
			}
			_, _ = w.Write([]byte("{\"model\":\"qwen2.5\",\"done\":true,\"prompt_eval_count\":12,\"eval_count\":5}\n"))
			_ = rc.Flush()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"qwen2.5","done":true,"prompt_eval_count":9,"eval_count":4}`))
	})

	ollamaManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/ollama-r5"
  name: "Ollama Native R5"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "ollama-r5"
    supportedDialects: ["ollama"]
    allowedNativePaths:
      - "^api/(chat|generate)$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "ollama-r5"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
  usageExtraction:
    inputTokensPath: "$.usage.prompt_tokens || $.prompt_eval_count"
    outputTokensPath: "$.usage.completion_tokens || $.eval_count"
`
	openAIManifest := `apiVersion: tanpopo.dev/extension/v1alpha1
kind: ExtensionManifest
metadata:
  id: "@acme/openai-r5"
  name: "OpenAI Native R5"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "openai-r5"
    supportedDialects: ["openai"]
    allowedNativePaths:
      - "^v1/chat/completions$"
    authMethods:
      - id: "bearer"
        mode: "host_blind_inject"
        headerName: "Authorization"
secretSlots:
  - slot: "api_key"
    requiredForAuthMethods: ["bearer"]
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
    allowPrivateIp: true
    requireTls: false
  hooks:
    - "provider:inspect_native_request"
    - "provider:extract_usage"
adapter:
  slug: "openai-r5"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	h.registerAdapterDeployment("@acme/ollama-r5", "ollama-r5", "ollama", "qwen2.5", "qwen2.5", up.srv.URL, ollamaManifest, nil)
	h.registerAdapterDeployment("@acme/openai-r5", "openai-r5", "openai", "gpt-4o", "gpt-4o", up.srv.URL, openAIManifest, nil)

	if _, err := h.store.RawDB().ExecContext(h.ctx,
		`UPDATE model_deployments SET pricing_nano_usd_json = '{"pricing_schema_version":2,"token_rate_unit":"nano_usd_per_token","input_token_nano_usd":1000,"output_token_nano_usd":2000}', version = version + 1`,
	); err != nil {
		t.Fatalf("update pricing: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("snapshots.Refresh: %v", err)
	}

	assertStreamTrailer := func(t *testing.T, url, payload, wantTrailer string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
		req.Header.Set("Content-Type", "application/json")
		resp, err := h.client.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", url, err)
		}
		if _, declared := resp.Trailer[http.CanonicalHeaderKey("X-Tanpopo-Usage-Accuracy")]; !declared {
			_ = resp.Body.Close()
			t.Fatalf("POST %s missing pre-stream Trailer declaration in resp.Trailer, headers=%v trailer=%v", url, resp.Header, resp.Trailer)
		}
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if got := resp.Trailer.Get("X-Tanpopo-Usage-Accuracy"); got != wantTrailer {
			t.Fatalf("POST %s Trailer X-Tanpopo-Usage-Accuracy = %q, want %q", url, got, wantTrailer)
		}
	}

	// 1. Streamed Ollama /api/chat and /api/generate with exact counts emit EXACT trailer.
	streamMode.Store(0)
	assertStreamTrailer(t, httpSrv.URL+"/native/ollama/api/chat", `{"model":"qwen2.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`, "EXACT")
	assertStreamTrailer(t, httpSrv.URL+"/native/ollama/api/generate", `{"model":"qwen2.5","stream":true,"prompt":"hi"}`, "EXACT")

	// 2. Streamed /api/chat without upstream usage counts settles ESTIMATED or UNMETERED_FLAGGED honestly via trailer.
	streamMode.Store(1)
	assertStreamTrailer(t, httpSrv.URL+"/native/ollama/api/chat", `{"model":"qwen2.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`, "ESTIMATED")

	// 3. Streamed native /native/openai/v1/chat/completions SSE emits EXACT trailer too.
	streamMode.Store(0)
	assertStreamTrailer(t, httpSrv.URL+"/native/openai/v1/chat/completions", `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`, "EXACT")

	// 4. Non-streaming native response returns X-Tanpopo-Usage-Accuracy response header.
	nonStreamReq, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/native/ollama/api/chat", strings.NewReader(`{"model":"qwen2.5","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	nonStreamReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	nonStreamResp, err := h.client.Do(nonStreamReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = nonStreamResp.Body.Close()
	if got := nonStreamResp.Header.Get("X-Tanpopo-Usage-Accuracy"); got != "EXACT" {
		t.Fatalf("non-stream X-Tanpopo-Usage-Accuracy header = %q, want EXACT", got)
	}

	// 5. Streamed native /api/generate against an unmetered deployment (no pricing, no usage counts) emits UNMETERED_FLAGGED trailer.
	if _, err := h.store.RawDB().ExecContext(h.ctx,
		`UPDATE model_deployments SET pricing_nano_usd_json = '{}', version = version + 1 WHERE upstream_model_name = 'qwen2.5'`,
	); err != nil {
		t.Fatalf("clear qwen2.5 pricing: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("snapshots.Refresh clear pricing: %v", err)
	}
	streamMode.Store(1)
	assertStreamTrailer(t, httpSrv.URL+"/native/ollama/api/generate", `{"model":"qwen2.5","stream":true,"prompt":"hi"}`, "UNMETERED_FLAGGED")
}
