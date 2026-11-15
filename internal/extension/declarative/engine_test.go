// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package declarative

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/port"
)

func TestDeclarativeOpenAIRoundTripAndCR09StreamUsage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	spec := DefaultOpenAICompatibleSpec("openai", []string{"api.openai.com"})
	spec.RequestMapping.FieldRenames = map[string]string{
		"max_tokens": "max_completion_tokens",
	}
	spec.RequestMapping.FieldDefaults = map[string]any{
		"temperature": 0.7,
	}
	spec.RequestMapping.DropFields = []string{"internal_trace_id"}

	eng, err := CompileSpec(spec)
	if err != nil {
		t.Fatalf("CompileSpec failed: %v", err)
	}

	cfg := `{"endpoint_base_url":"https://api.openai.com"}`

	// 1. Two-phase validation probe + verification.
	probeSpec, err := eng.BuildValidationProbe(ctx, port.ValidationProbeInput{
		RequestID:       "req_probe_1",
		ConfigJSON:      cfg,
		SecretSlotNames: []string{"api_key"},
	})
	if err != nil {
		t.Fatalf("BuildValidationProbe failed: %v", err)
	}
	if probeSpec.Method != "GET" || probeSpec.TargetURL != "https://api.openai.com/v1/models" {
		t.Fatalf("unexpected probe spec: %+v", probeSpec)
	}
	if len(probeSpec.CredentialInjections) != 1 || probeSpec.CredentialInjections[0].FieldName != "Authorization" {
		t.Fatalf("unexpected probe credential injections: %+v", probeSpec.CredentialInjections)
	}

	verdict, err := eng.VerifyValidationResult(ctx, port.ValidationResultInput{
		RequestID:    "req_probe_1",
		HTTPStatus:   200,
		ResponseBody: []byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"gpt-4o-mini"}]}`),
	})
	if err != nil {
		t.Fatalf("VerifyValidationResult failed: %v", err)
	}
	if !verdict.Valid || len(verdict.DetectedModelIDs) != 2 || verdict.DetectedModelIDs[0] != "gpt-4o" {
		t.Fatalf("unexpected probe verdict: %+v", verdict)
	}

	// 2. TransformRequest with CR-09 stream_options.include_usage injection.
	reqSpec, err := eng.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "req_1",
		ConfigJSON:            cfg,
		NormalizedRequestJSON: []byte(`{"model":"gpt-4o","max_tokens":256,"stream":true,"internal_trace_id":"drop-me","messages":[{"role":"user","content":"hi"}]}`),
		SecretSlotRefs:        map[string]string{"api_key": "sec_123"},
	})
	if err != nil {
		t.Fatalf("TransformRequest failed: %v", err)
	}
	var bodyMap map[string]any
	if err := json.Unmarshal(reqSpec.BodyBytes, &bodyMap); err != nil {
		t.Fatalf("unmarshal transformed body: %v", err)
	}
	if _, stillHasOld := bodyMap["max_tokens"]; stillHasOld {
		t.Fatalf("expected max_tokens renamed away: %v", bodyMap)
	}
	if bodyMap["max_completion_tokens"] != float64(256) {
		t.Fatalf("expected max_completion_tokens=256, got %v", bodyMap["max_completion_tokens"])
	}
	if bodyMap["temperature"] != 0.7 {
		t.Fatalf("expected temperature default 0.7, got %v", bodyMap["temperature"])
	}
	if _, dropped := bodyMap["internal_trace_id"]; dropped {
		t.Fatalf("expected internal_trace_id dropped: %v", bodyMap)
	}
	so, _ := bodyMap["stream_options"].(map[string]any)
	if so == nil || so["include_usage"] != true {
		t.Fatalf("CR-09 stream_options.include_usage=true missing: %v", bodyMap)
	}

	// 3. TransformResponse + exact 5-component usage extraction.
	resp, err := eng.TransformResponse(ctx, port.TransformResponseInput{
		RequestID:       "req_1",
		HTTPStatus:      200,
		RawResponseBody: []byte(`{"id":"chatcmpl_1","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":1}}}`),
		Modality:        "CHAT",
	})
	if err != nil {
		t.Fatalf("TransformResponse failed: %v", err)
	}
	if resp.UpstreamModelID != "gpt-4o" || resp.FinishReason != "stop" {
		t.Fatalf("unexpected response metadata: %+v", resp)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 4 || resp.Usage.CacheReadTokens != 3 || resp.Usage.ReasoningTokens != 1 || resp.Usage.Accuracy != port.UsageAccuracyExact {
		t.Fatalf("unexpected normalized usage: %+v", resp.Usage)
	}

	// 4. TransformStreamChunk across split TCP boundaries + final [DONE].
	chunk1, err := eng.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_1",
		RawUpstreamChunk: []byte("data: {\"id\":\"c1\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"Hel\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"c2\",\"model\":"),
	})
	if err != nil {
		t.Fatalf("TransformStreamChunk 1 failed: %v", err)
	}
	if len(chunk1.NormalizedSSEFrames) != 1 || chunk1.StreamCompleted || len(chunk1.NextSessionState) == 0 {
		t.Fatalf("unexpected chunk1 output: %+v", chunk1)
	}
	chunk2, err := eng.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_1",
		SessionState:     chunk1.NextSessionState,
		RawUpstreamChunk: []byte("\"gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"),
		IsEOF:            true,
	})
	if err != nil {
		t.Fatalf("TransformStreamChunk 2 failed: %v", err)
	}
	if !chunk2.StreamCompleted || len(chunk2.NormalizedSSEFrames) != 2 {
		t.Fatalf("expected completion and 2 frames, got %+v", chunk2)
	}
	if chunk2.IncrementalUsage == nil || chunk2.IncrementalUsage.OutputTokens != 2 || chunk2.IncrementalUsage.Accuracy != port.UsageAccuracyExact {
		t.Fatalf("expected exact incremental usage, got %+v", chunk2.IncrementalUsage)
	}
}

func TestDeclarativeManifestYAMLParsingAndSafetyGuards(t *testing.T) {
	t.Parallel()
	yamlDoc := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/adapter-ollama"
  name: Ollama Local Adapter
permissions:
  networkEgress:
    allowedHosts:
      - "127.0.0.1"
adapter:
  slug: ollama
  baseUrl: "http://127.0.0.1:11434"
  supportedDialects: ["openai_chat"]
  endpoints:
    models: "/v1/models"
    chat: "/v1/chat/completions"
  requestMapping:
    injectStreamUsage: true
`
	eng, err := ParseDeclarativeManifestYAML([]byte(yamlDoc))
	if err != nil {
		t.Fatalf("ParseDeclarativeManifestYAML valid doc failed: %v", err)
	}
	// AC-2 style auth_mode=NONE -> zero SecretSlotRefs succeeds with zero CredentialInjections.
	out, err := eng.TransformRequest(context.Background(), port.TransformRequestInput{
		RequestID:             "req_local",
		NormalizedRequestJSON: []byte(`{"model":"llama3.2","messages":[{"role":"user","content":"ping"}]}`),
	})
	if err != nil {
		t.Fatalf("TransformRequest for auth_mode=NONE failed: %v", err)
	}
	if len(out.CredentialInjections) != 0 || out.TargetURL != "http://127.0.0.1:11434/v1/chat/completions" {
		t.Fatalf("unexpected local output: %+v", out)
	}

	// Cookie header prohibition (DEC-004).
	badCookieYAML := strings.ReplaceAll(yamlDoc, "injectStreamUsage: true", "injectStreamUsage: true\n    staticHeaders:\n      Cookie: session=evil")
	if _, err := ParseDeclarativeManifestYAML([]byte(badCookieYAML)); !errors.Is(err, port.ErrProhibitedCookieInjection) {
		t.Fatalf("expected ErrProhibitedCookieInjection, got %v", err)
	}

	// Wildcard egress prohibition.
	badWildcard := strings.ReplaceAll(yamlDoc, `"127.0.0.1"`, `"*.example.com"`)
	if _, err := ParseDeclarativeManifestYAML([]byte(badWildcard)); !errors.Is(err, ErrInvalidDeclarativeSpec) {
		t.Fatalf("expected ErrInvalidDeclarativeSpec for wildcard host, got %v", err)
	}
}

func TestDeclarativeNEG1DepthRecursiveTemplatesAndBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Recursive / nested template placeholders rejected at load time.
	badTemplates := []string{
		"/v1/{{model}}/chat",
		"/v1/${ENV_VAR}/chat",
		"/v1/{outer{inner}}/chat",
		"/v1/{../escape}/chat",
		"/v1/models/../admin",
		"/v1/{unclosed",
	}
	for _, tmpl := range badTemplates {
		spec := DefaultOpenAICompatibleSpec("bad", []string{"api.example.com"})
		spec.RequestMapping.PathTemplate = tmpl
		if _, err := CompileSpec(spec); !errors.Is(err, port.ErrInvalidSpec) {
			t.Fatalf("template %q should be rejected with ErrInvalidSpec, got %v", tmpl, err)
		}
	}

	eng, err := CompileSpec(DefaultOpenAICompatibleSpec("safe", []string{"api.example.com"}))
	if err != nil {
		t.Fatalf("CompileSpec failed: %v", err)
	}

	// 2. Nesting depth 33 and pathological depth 20000 rejected cleanly in < 1 ms.
	for _, depth := range []int{33, 20000} {
		var buf bytes.Buffer
		buf.WriteString(`{"model":"gpt-4o","deep":`)
		for i := 0; i < depth; i++ {
			buf.WriteByte('[')
		}
		buf.WriteByte('1')
		for i := 0; i < depth; i++ {
			buf.WriteByte(']')
		}
		buf.WriteByte('}')

		start := time.Now()
		_, err := eng.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "req_deep",
			ConfigJSON:            `{"endpoint_base_url":"https://api.example.com"}`,
			NormalizedRequestJSON: buf.Bytes(),
		})
		elapsed := time.Since(start)
		if !errors.Is(err, ErrJSONDepthExceeded) {
			t.Fatalf("depth %d expected ErrJSONDepthExceeded, got %v", depth, err)
		}
		if elapsed >= 5*time.Millisecond {
			t.Fatalf("depth %d rejection took too long: %v", depth, elapsed)
		}
	}

	// 3. Operator step budget exhaustion via >256 SSE lines in a single chunk.
	var sseSpam strings.Builder
	for i := 0; i < 300; i++ {
		sseSpam.WriteString(": keepalive\n")
	}
	_, err = eng.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_spam",
		RawUpstreamChunk: []byte(sseSpam.String()),
	})
	if !errors.Is(err, ErrOperatorBudgetExceeded) {
		t.Fatalf("expected ErrOperatorBudgetExceeded for >256 SSE lines, got %v", err)
	}

	// 4. Error normalization & usage fallback.
	nerr, err := eng.NormalizeError(ctx, port.NormalizeErrorInput{
		RequestID:        "req_e",
		HTTPStatus:       401,
		SanitizedHeaders: map[string]string{"Retry-After": "12"},
		SanitizedBody:    []byte(`{"error":{"message":"invalid api key\nline2"}}`),
	})
	if err != nil || !nerr.TriggerConnectionReauth || nerr.RetryablePreStream {
		t.Fatalf("expected TriggerConnectionReauth=true on 401, got %+v (err=%v)", nerr, err)
	}
	nerr429, err := eng.NormalizeError(ctx, port.NormalizeErrorInput{
		RequestID:        "req_429",
		HTTPStatus:       429,
		SanitizedHeaders: map[string]string{"Retry-After": "15"},
	})
	if err != nil || !nerr429.RetryablePreStream || nerr429.CooldownMs != 15000 {
		t.Fatalf("expected retryable 15000ms cooldown on 429, got %+v (err=%v)", nerr429, err)
	}
	estUsage, err := eng.ExtractUsage(ctx, port.ExtractUsageInput{
		RequestID:               "req_u",
		FallbackUTF8InputBytes:  100,
		FallbackUTF8OutputBytes: 40,
	})
	if err != nil || estUsage.InputTokens != 25 || estUsage.OutputTokens != 10 || estUsage.Accuracy != port.UsageAccuracyEstimated {
		t.Fatalf("unexpected fallback estimated usage: %+v (err=%v)", estUsage, err)
	}
}

func FuzzDeclarativeEngine(f *testing.F) {
	f.Add([]byte(`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`), []byte(`apiVersion: tanpopo.dev/extension/v1alpha1`))
	f.Add([]byte(`[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]`), []byte{})
	f.Add([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: [DONE]\n\n"), []byte{})

	eng, err := CompileSpec(DefaultOpenAICompatibleSpec("fuzz", []string{"api.example.com"}))
	if err != nil {
		f.Fatalf("CompileSpec failed: %v", err)
	}
	ctx := context.Background()

	f.Fuzz(func(t *testing.T, payload []byte, yamlBytes []byte) {
		_ = ValidateJSONDepth(payload, MaxJSONDepth)
		if len(yamlBytes) > 0 {
			_, _ = ParseDeclarativeManifestYAML(yamlBytes)
		}
		_, _ = eng.TransformRequest(ctx, port.TransformRequestInput{
			RequestID:             "fuzz_req",
			ConfigJSON:            `{"endpoint_base_url":"https://api.example.com"}`,
			NormalizedRequestJSON: payload,
		})
		_, _ = eng.TransformResponse(ctx, port.TransformResponseInput{
			RequestID:       "fuzz_resp",
			HTTPStatus:      200,
			RawResponseBody: payload,
		})
		_, _ = eng.TransformStreamChunk(ctx, port.StreamChunkInput{
			RequestID:        "fuzz_stream",
			RawUpstreamChunk: payload,
			IsEOF:            true,
		})
		_, _ = eng.InspectNativeRequest(ctx, port.NativeRequestInput{
			RequestID:      "fuzz_native",
			Dialect:        "openai_chat",
			UpstreamPath:   "/v1/chat/completions",
			RawRequestBody: payload,
		})
	})
}

func BenchmarkDeclarativeTransformRequest(b *testing.B) {
	spec := DefaultOpenAICompatibleSpec("bench", []string{"api.openai.com"})
	spec.RequestMapping.FieldRenames = map[string]string{"max_tokens": "max_completion_tokens"}
	spec.RequestMapping.FieldDefaults = map[string]any{"temperature": 0.5}
	eng, err := CompileSpec(spec)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	in := port.TransformRequestInput{
		RequestID:             "bench_1",
		ConfigJSON:            `{"endpoint_base_url":"https://api.openai.com"}`,
		NormalizedRequestJSON: []byte(`{"model":"gpt-4o","max_tokens":512,"stream":true,"messages":[{"role":"user","content":"Benchmark prompt"}]}`),
		SecretSlotRefs:        map[string]string{"api_key": "sec_1"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.TransformRequest(ctx, in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeclarativeTransformStreamChunk(b *testing.B) {
	eng, err := CompileSpec(DefaultOpenAICompatibleSpec("bench", []string{"api.openai.com"}))
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	in := port.StreamChunkInput{
		RequestID:        "bench_s",
		RawUpstreamChunk: []byte("data: {\"id\":\"c1\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"token\"},\"finish_reason\":null}]}\n\n"),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.TransformStreamChunk(ctx, in); err != nil {
			b.Fatal(err)
		}
	}
}

func TestDeclarativeReviewRemediations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. YAML alias amplification (billion-laughs DAG) rejected before full unmarshal.
	billionLaughsYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/yaml-bomb"
  name: Bomb
  version: "1.0.0"
adapter:
  slug: bomb
  baseUrl: "https://api.example.com"
  extra:
    a0: &a0 ["x", "x", "x", "x", "x", "x", "x", "x", "x", "x"]
    a1: &a1 [*a0, *a0, *a0, *a0, *a0, *a0, *a0, *a0, *a0, *a0]
    a2: &a2 [*a1, *a1, *a1, *a1, *a1, *a1, *a1, *a1, *a1, *a1]
    a3: &a3 [*a2, *a2, *a2, *a2, *a2, *a2, *a2, *a2, *a2, *a2]
    a4: &a4 [*a3, *a3, *a3, *a3, *a3, *a3, *a3, *a3, *a3, *a3]
`
	start := time.Now()
	if _, err := ParseDeclarativeManifestYAML([]byte(billionLaughsYAML)); !errors.Is(err, ErrInvalidDeclarativeSpec) {
		t.Fatalf("expected ErrInvalidDeclarativeSpec on YAML alias DAG amplification, got %v", err)
	}
	if time.Since(start) > 25*time.Millisecond {
		t.Fatalf("YAML alias bomb rejection too slow: %v", time.Since(start))
	}

	// 2. Unsupported host_crypto_sign explicitly rejected.
	cryptoSignYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/sig-test"
  name: SigTest
  version: "1.0.0"
permissions:
  networkEgress:
    allowedHosts: ["api.example.com"]
adapter:
  slug: sigtest
  baseUrl: "https://api.example.com"
  auth:
    - slot: "api_key"
      mode: "host_crypto_sign"
`
	if _, err := ParseDeclarativeManifestYAML([]byte(cryptoSignYAML)); !errors.Is(err, ErrInvalidDeclarativeSpec) {
		t.Fatalf("expected ErrInvalidDeclarativeSpec on host_crypto_sign, got %v", err)
	}

	// 3. host:port exact match supported in allowedHosts + defaultBaseUrl + runtime config.
	hostPortYAML := `
apiVersion: tanpopo.dev/extension/v1alpha1
kind: DeclarativeProviderAdapter
metadata:
  id: "@tanpopo/hostport"
  name: HostPort
  version: "1.0.0"
permissions:
  networkEgress:
    allowedHosts: ["127.0.0.1:8443"]
adapter:
  slug: hostport
  baseUrl: "https://127.0.0.1:8443"
`
	hpEng, err := ParseDeclarativeManifestYAML([]byte(hostPortYAML))
	if err != nil {
		t.Fatalf("host:port manifest failed: %v", err)
	}
	hpReq, err := hpEng.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "req_hp",
		ConfigJSON:            `{"endpoint_base_url":"https://127.0.0.1:8443"}`,
		NormalizedRequestJSON: []byte(`{"model":"m1","messages":[{"role":"user","content":"hi"}]}`),
	})
	if err != nil || hpReq.TargetURL != "https://127.0.0.1:8443/v1/chat/completions" {
		t.Fatalf("unexpected host:port TransformRequest result: err=%v spec=%+v", err, hpReq)
	}

	eng, err := CompileSpec(DefaultOpenAICompatibleSpec("rem", []string{"api.example.com"}))
	if err != nil {
		t.Fatal(err)
	}

	// 4. Empty delta omission on finish_reason-only SSE chunk + stream error frame extraction.
	finishChunk, err := eng.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_fin",
		RawUpstreamChunk: []byte("data: {\"id\":\"c1\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"),
	})
	if err != nil {
		t.Fatalf("TransformStreamChunk finish-only failed: %v", err)
	}
	if len(finishChunk.NormalizedSSEFrames) != 1 || bytes.Contains(finishChunk.NormalizedSSEFrames[0], []byte(`"content":""`)) {
		t.Fatalf("expected finish-only SSE frame without empty content field, got %q", finishChunk.NormalizedSSEFrames)
	}

	_, err = eng.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_serr",
		RawUpstreamChunk: []byte("data: {\"error\":{\"message\":\"upstream overloaded\"}}\n\n"),
	})
	if !errors.Is(err, port.ErrExecutionFault) || !strings.Contains(err.Error(), "upstream overloaded") {
		t.Fatalf("expected ErrExecutionFault with extracted message, got %v", err)
	}

	// 5. ExtractUsage handles SSE data: prefix + NormalizeError rune-safe <=256 byte truncation & code extraction.
	sseUsage, err := eng.ExtractUsage(ctx, port.ExtractUsageInput{
		RequestID:                 "req_use",
		RawResponseOrTrailerBytes: []byte("data: {\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":7}}\n\n"),
	})
	if err != nil || sseUsage.InputTokens != 12 || sseUsage.OutputTokens != 7 || sseUsage.Accuracy != port.UsageAccuracyExact {
		t.Fatalf("unexpected SSE ExtractUsage: %+v (err=%v)", sseUsage, err)
	}

	longUTF8 := strings.Repeat("é", 200) // 400 bytes, valid UTF-8
	normErr, err := eng.NormalizeError(ctx, port.NormalizeErrorInput{
		RequestID:     "req_utf8",
		HTTPStatus:    429,
		SanitizedBody: []byte(`{"error":{"code":"ignored_on_429","message":"` + longUTF8 + `"}}`),
	})
	if err != nil {
		t.Fatalf("NormalizeError failed: %v", err)
	}
	if len(normErr.SanitizedPublicMessage) > 256 || strings.ContainsRune(normErr.SanitizedPublicMessage, '\uFFFD') {
		t.Fatalf("expected rune-safe <=256 byte truncation, got len=%d str=%q", len(normErr.SanitizedPublicMessage), normErr.SanitizedPublicMessage)
	}
	if normErr.CanonicalCode != "upstream_rate_limited" {
		t.Fatalf("expected canonical upstream_rate_limited preserved on 429, got %q", normErr.CanonicalCode)
	}

	norm400, err := eng.NormalizeError(ctx, port.NormalizeErrorInput{
		RequestID:     "req_400_code",
		HTTPStatus:    400,
		SanitizedBody: []byte(`{"error":{"code":"context_length_exceeded","message":"prompt too long"}}`),
	})
	if err != nil || norm400.CanonicalCode != "context_length_exceeded" {
		t.Fatalf("expected extracted CanonicalCode=context_length_exceeded on 400, got err=%v norm=%+v", err, norm400)
	}

	// 6. Single SSE chunk with >= 32 data frames succeeds cleanly (per-frame operator budget).
	var bulkSSE strings.Builder
	for i := 0; i < 32; i++ {
		bulkSSE.WriteString("data: {\"id\":\"c\",\"model\":\"gpt-4o\",\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":null}]}\n\n")
	}
	bulkOut, err := eng.TransformStreamChunk(ctx, port.StreamChunkInput{
		RequestID:        "req_bulk_32",
		RawUpstreamChunk: []byte(bulkSSE.String()),
	})
	if err != nil || len(bulkOut.NormalizedSSEFrames) != 32 {
		t.Fatalf("expected 32 SSE frames to succeed, got err=%v frames=%d", err, len(bulkOut.NormalizedSSEFrames))
	}

	// 7. Unsupported scoped_secret_unseal rejected and {model} slash stays %2F single segment.
	unsealYAML := strings.ReplaceAll(cryptoSignYAML, "host_crypto_sign", "scoped_secret_unseal")
	if _, err := ParseDeclarativeManifestYAML([]byte(unsealYAML)); !errors.Is(err, ErrInvalidDeclarativeSpec) {
		t.Fatalf("expected ErrInvalidDeclarativeSpec on scoped_secret_unseal, got %v", err)
	}
	modelPathSpec := DefaultOpenAICompatibleSpec("slash", []string{"api.example.com"})
	modelPathSpec.RequestMapping.PathTemplate = "/v1/models/{model}:generate"
	slashEng, err := CompileSpec(modelPathSpec)
	if err != nil {
		t.Fatal(err)
	}
	slashReq, err := slashEng.TransformRequest(ctx, port.TransformRequestInput{
		RequestID:             "req_slash",
		ConfigJSON:            `{"endpoint_base_url":"https://api.example.com"}`,
		NormalizedRequestJSON: []byte(`{"model":"meta-llama/Llama-3.3-70B","messages":[]}`),
	})
	if err != nil || !strings.Contains(slashReq.TargetURL, "meta-llama%2FLlama-3.3-70B:generate") {
		t.Fatalf("expected %%2F single path segment preservation, got err=%v url=%q", err, slashReq.TargetURL)
	}
}
