// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	ingresshttp "github.com/nguywnben/tanpopo/internal/ingress/http"
	"github.com/nguywnben/tanpopo/internal/ingress/native"
	"github.com/nguywnben/tanpopo/internal/telemetry/otel"
)

func TestDEF051_052_070_074_OllamaNativeNDJSONAndImageUnits(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	var usages []native.UsageRecord
	h := setupNativeHarness(t, nil)
	nowFn := func() int64 { return h.clk.Now().UnixMilli() }

	telPipe, err := otel.NewPipeline(otel.PipelineConfig{
		ServiceName: "tanpopo-native-r4-test",
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
		ID:              "bdg_nat_r4",
		OrgID:           h.org.ID,
		ScopeType:       ledger.ScopeOrg,
		ScopeID:         h.org.ID,
		PeriodKind:      ledger.PeriodTotalLifetime,
		PeriodStartMs:   0,
		PeriodEndMs:     nowFn() + 86_400_000,
		HardLimitMicros: 1_000_000,
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
		SSEHeartbeatInterval: 15 * time.Millisecond,
		NativeUsageObserver: func(rec native.UsageRecord) {
			usages = append(usages, rec)
		},
	})
	if err != nil {
		t.Fatalf("ingresshttp.NewServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close(h.ctx) })
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)

	up := newSpyUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n"))
			return
		}
		if bytes.Contains(body, []byte(`"stream":true`)) {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			rc := http.NewResponseController(w)
			_, _ = w.Write([]byte("{\"model\":\"qwen2.5\",\"message\":{\"role\":\"assistant\",\"content\":\"hello\"},\"done\":false}\n"))
			_ = rc.Flush()
			time.Sleep(35 * time.Millisecond) // longer than 15ms heartbeat interval to verify NDJSON skips ": ping"
			_, _ = w.Write([]byte("{\"model\":\"qwen2.5\",\"done\":true,\"prompt_eval_count\":11,\"eval_count\":6}\n"))
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
  id: "@acme/ollama-native"
  name: "Ollama Native"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "ollama-native"
    supportedDialects: ["ollama"]
    allowedNativePaths:
      - "^api/(chat|generate|embeddings)$"
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
  slug: "ollama-native"
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
  id: "@acme/openai-sse-native"
  name: "OpenAI SSE Native"
  version: "1.0.0"
  license: "Apache-2.0"
compatibility:
  contractVersion: "0.1"
  coreVersionRange: ">=0.1.0"
runtime:
  tier: "declarative"
capabilities:
  - type: "provider_adapter"
    adapterSlug: "openai-sse-native"
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
  slug: "openai-sse-native"
  wireProtocol: "openai_chat_v1"
  auth:
    - id: "bearer"
      mode: "host_blind_inject"
      secretSlot: "api_key"
      headerName: "Authorization"
      valuePrefix: "Bearer "
`
	h.registerAdapterDeployment("@acme/ollama-native", "ollama-native", "ollama", "qwen2.5", "qwen2.5", up.srv.URL, ollamaManifest, nil)
	h.registerAdapterDeployment("@acme/openai-sse-native", "openai-sse-native", "openai", "gpt-4o", "gpt-4o", up.srv.URL, openAIManifest, nil)

	// Set non-zero canonical per-token + per-image rate card on qwen2.5 deployment so DEF-074 image settlement writes >0 micro-USD.
	if _, err := h.store.RawDB().ExecContext(h.ctx,
		`UPDATE model_deployments SET pricing_nano_usd_json = '{"pricing_schema_version":2,"token_rate_unit":"nano_usd_per_token","input_token_nano_usd":1000,"output_token_nano_usd":2000,"image_per_unit_nano_usd":5000000}', version = version + 1 WHERE upstream_model_name = 'qwen2.5'`,
	); err != nil {
		t.Fatalf("update qwen2.5 pricing: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("snapshots.Refresh: %v", err)
	}

	// 1. DEF-070 + DEF-051: Ollama NDJSON stream uses application/x-ndjson, no SSE ": ping", exact token settlement.
	ndReq, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/native/ollama/api/chat", bytes.NewReader([]byte(`{"model":"qwen2.5","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	ndReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	ndResp, err := h.client.Do(ndReq)
	if err != nil {
		t.Fatal(err)
	}
	var ndBuf bytes.Buffer
	_, _ = ndBuf.ReadFrom(ndResp.Body)
	_ = ndResp.Body.Close()
	if ndResp.StatusCode != http.StatusOK {
		t.Fatalf("Ollama NDJSON status=%d body=%s", ndResp.StatusCode, ndBuf.String())
	}
	if ct := ndResp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
		t.Fatalf("DEF-070: expected Content-Type application/x-ndjson, got %q", ct)
	}
	if strings.Contains(ndBuf.String(), ": ping") {
		t.Fatalf("DEF-070: expected no SSE heartbeat comments in Ollama NDJSON stream, got %q", ndBuf.String())
	}
	lastU := usages[len(usages)-1]
	if lastU.Usage.Accuracy != port.UsageAccuracyExact || lastU.Usage.InputTokens != 11 || lastU.Usage.OutputTokens != 6 {
		t.Fatalf("DEF-051: expected exact Ollama NDJSON usage 11/6, got %+v", lastU)
	}

	// Non-Ollama native SSE dialect preserves text/event-stream; charset=utf-8.
	sseReq, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/native/openai/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	sseReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	sseResp, err := h.client.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = bytes.NewBuffer(nil).ReadFrom(sseResp.Body)
	_ = sseResp.Body.Close()
	if ct := sseResp.Header.Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Fatalf("DEF-070: expected OpenAI native SSE text/event-stream; charset=utf-8, got %q", ct)
	}

	// 2. DEF-052 + DEF-074: Ollama /api/generate images[] and /api/chat messages[].images[] settle exact image units bound to X-Tanpopo-Trace-Id.
	imgB64 := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89, 0x50, 0x4E, 0x47}, 400))

	genReq, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/native/ollama/api/generate", bytes.NewReader([]byte(`{"model":"qwen2.5","stream":false,"prompt":"describe","images":["`+imgB64+`"]}`)))
	genReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	genResp, err := h.client.Do(genReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = genResp.Body.Close()
	if genResp.StatusCode != http.StatusOK {
		t.Fatalf("DEF-074 generate status=%d", genResp.StatusCode)
	}
	genTraceID := genResp.Header.Get("X-Tanpopo-Trace-Id")
	var genMediaJSON string
	var genCostMicros int64
	if err := h.store.RawDB().QueryRow(`SELECT media_usage_json, total_cost_micros FROM usage_settlements WHERE request_id = ?`, genTraceID).Scan(&genMediaJSON, &genCostMicros); err != nil {
		t.Fatalf("DEF-052/074 query /api/generate settlement by X-Tanpopo-Trace-Id=%q: %v", genTraceID, err)
	}
	// 9*1000 + 4*2000 + 1*5000000 nano-USD = 5,017,000 nano-USD -> ceil(5017000/1000) = 5017 micro-USD!
	if !strings.Contains(genMediaJSON, `"image_count":1`) || genCostMicros != 5017 {
		t.Fatalf("DEF-074 /api/generate settlement mismatch: media=%s cost=%d want image_count:1 & cost=5017", genMediaJSON, genCostMicros)
	}

	chatReq, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/native/ollama/api/chat", bytes.NewReader([]byte(`{"model":"qwen2.5","stream":false,"messages":[{"role":"user","content":"compare","images":["`+imgB64+`","`+imgB64+`"]}]}`)))
	chatReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	chatResp, err := h.client.Do(chatReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = chatResp.Body.Close()
	if chatResp.StatusCode != http.StatusOK {
		t.Fatalf("DEF-074 chat images status=%d", chatResp.StatusCode)
	}
	chatTraceID := chatResp.Header.Get("X-Tanpopo-Trace-Id")
	var chatMediaJSON string
	var chatCostMicros int64
	if err := h.store.RawDB().QueryRow(`SELECT media_usage_json, total_cost_micros FROM usage_settlements WHERE request_id = ?`, chatTraceID).Scan(&chatMediaJSON, &chatCostMicros); err != nil {
		t.Fatalf("DEF-052/074 query /api/chat settlement by X-Tanpopo-Trace-Id=%q: %v", chatTraceID, err)
	}
	// 9*1000 + 4*2000 + 2*5000000 nano-USD = 10,017,000 nano-USD -> 10017 micro-USD!
	if !strings.Contains(chatMediaJSON, `"image_count":2`) || chatCostMicros != 10017 {
		t.Fatalf("DEF-074 /api/chat settlement mismatch: media=%s cost=%d want image_count:2 & cost=10017", chatMediaJSON, chatCostMicros)
	}

	// 3. DEF-059: Quarantined Ollama adapter fails closed with 503 extension_quarantined (not no_compliant_deployment_available).
	var ollamaInstID string
	if err := h.store.RawDB().QueryRowContext(h.ctx,
		`SELECT ei.id FROM extension_installations ei JOIN extension_packages ep ON ep.id = ei.package_id WHERE ep.canonical_name = '@acme/ollama-native' LIMIT 1`,
	).Scan(&ollamaInstID); err != nil {
		t.Fatalf("lookup ollama installation id: %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx,
		`UPDATE extension_installations SET state = 'QUARANTINED', version = version + 1 WHERE id = ?`,
		ollamaInstID,
	); err != nil {
		t.Fatalf("quarantine ollama installation: %v", err)
	}
	if _, err := h.store.RawDB().ExecContext(h.ctx,
		`INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms) VALUES ('', 'extension_installation', ?, 'QUARANTINE', 1, ?)`,
		ollamaInstID, nowFn(),
	); err != nil {
		t.Fatalf("changelog quarantine: %v", err)
	}
	if err := h.snapshots.Refresh(h.ctx); err != nil {
		t.Fatalf("snapshots.Refresh after quarantine: %v", err)
	}
	quarReq, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/native/ollama/api/generate", bytes.NewReader([]byte(`{"model":"qwen2.5","stream":false,"prompt":"hi"}`)))
	quarReq.Header.Set("Authorization", "Bearer "+h.vkeyRaw)
	quarResp, err := h.client.Do(quarReq)
	if err != nil {
		t.Fatal(err)
	}
	var quarBuf bytes.Buffer
	_, _ = quarBuf.ReadFrom(quarResp.Body)
	_ = quarResp.Body.Close()
	if quarResp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(quarBuf.String(), `"extension_quarantined"`) {
		t.Fatalf("DEF-059 native quarantine expected 503 extension_quarantined, got status=%d body=%s", quarResp.StatusCode, quarBuf.String())
	}
}
