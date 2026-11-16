// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package privacy_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestRedactorScrubsSecretsAndElidesPayloadsByDefault(t *testing.T) {
	t.Parallel()
	r := privacy.NewDefaultRedactor()
	if r.CapturePayloads {
		t.Fatalf("expected CapturePayloads=false by default")
	}

	canarySecret := "SECRET_PATIENT_RECORD_998877"
	canarySk := "sk-tp-" + "canary-998877"
	canaryVk := "vk_tp_secretkey_123456"

	hdrs := http.Header{
		"Authorization":        {"Bearer " + canarySk},
		"X-Api-Key":            {"super-secret-provider-key"},
		"X-Goog-Api-Key":       {"AIzaSyAbCdEfGhIjKlMnOpQrStUvWxYz"},
		"X-Tanpopo-Virtual-Key": {canaryVk},
		"Cookie":               {"session=abc123; csrf=xyz"},
		"Set-Cookie":           {"sid=secretcookie; HttpOnly"},
		"X-Tanpopo-Request-Id": {"req_01TEST"},
	}
	cleanHdrs := r.SanitizeHeaders(hdrs)
	for _, k := range []string{"authorization", "x-api-key", "x-goog-api-key", "x-tanpopo-virtual-key", "cookie", "set-cookie"} {
		if cleanHdrs[k] != privacy.RedactedMarker {
			t.Fatalf("expected header %q redacted, got %q", k, cleanHdrs[k])
		}
	}
	if cleanHdrs["x-tanpopo-request-id"] != "req_01TEST" {
		t.Fatalf("unexpected non-secret header: %q", cleanHdrs["x-tanpopo-request-id"])
	}

	rawInput := "https://alice:hunter2@api.example.com/v1?api_key=abc&k=" + canarySk + " Bearer " + canaryVk + "\r\nline2"
	scrubbed := r.SanitizeString(rawInput)
	for _, forbidden := range []string{"hunter2", canarySk, canaryVk, "\r", "\n"} {
		if strings.Contains(scrubbed, forbidden) {
			t.Fatalf("scrubbed string leaked %q: %q", forbidden, scrubbed)
		}
	}
	if !strings.Contains(scrubbed, privacy.RedactedMarker) {
		t.Fatalf("expected redacted marker in %q", scrubbed)
	}

	// SanitizeAttribute omits payload keys when CapturePayloads=false, while retaining token counts.
	if _, ok := r.SanitizeAttribute("messages", canarySecret); ok {
		t.Fatalf("expected payload key 'messages' to be omitted")
	}
	if _, ok := r.SanitizeAttribute("input", canarySecret); ok {
		t.Fatalf("expected payload key 'input' to be omitted")
	}
	if _, ok := r.SanitizeAttribute("output", canarySecret); ok {
		t.Fatalf("expected payload key 'output' to be omitted")
	}
	if _, ok := r.SanitizeAttribute("tool_calls", canarySecret); ok {
		t.Fatalf("expected payload key 'tool_calls' to be omitted")
	}
	if val, ok := r.SanitizeAttribute("input_tokens", 42); !ok || val != 42 {
		t.Fatalf("expected non-secret token counter preserved, got val=%v ok=%v", val, ok)
	}
	if val, ok := r.SanitizeAttribute("api_key", canarySk); !ok || val != privacy.RedactedMarker {
		t.Fatalf("expected sensitive key replaced with RedactedMarker, got val=%v ok=%v", val, ok)
	}

	// SanitizeMap scrubs nested secrets & elides payload keys recursively.
	cleanMap := r.SanitizeMap(map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": canarySecret}},
		"nested": map[string]any{
			"authorization": "Bearer " + canarySk,
			"err":           errors.New("dial failed with " + canaryVk),
			"max_tokens":    512,
		},
	})
	if cleanMap["messages"] != privacy.ElidedMarker {
		t.Fatalf("expected messages elided, got %v", cleanMap["messages"])
	}
	nested := cleanMap["nested"].(map[string]any)
	if nested["authorization"] != privacy.RedactedMarker {
		t.Fatalf("expected nested authorization redacted, got %v", nested["authorization"])
	}
	if strings.Contains(nested["err"].(string), canaryVk) {
		t.Fatalf("expected nested error scrubbed of vk_tp_ token, got %v", nested["err"])
	}
	if nested["max_tokens"] != 512 {
		t.Fatalf("expected max_tokens preserved, got %v", nested["max_tokens"])
	}

	// Even when CapturePayloads=true, secrets remain redacted.
	capRed := &privacy.Redactor{CapturePayloads: true}
	capMap := capRed.SanitizeMap(map[string]any{
		"messages": canarySk,
		"api_key":  canarySk,
	})
	if capMap["api_key"] != privacy.RedactedMarker || strings.Contains(capMap["messages"].(string), canarySk) {
		t.Fatalf("expected credentials scrubbed even when CapturePayloads=true, got %+v", capMap)
	}
}

func TestJSONLoggerFlatSnakeCaseAndSecretScrubbing(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	fixedTime := time.Date(2026, 11, 16, 6, 0, 0, 123000000, time.UTC)
	logger := privacy.NewJSONLogger(&buf, privacy.NewDefaultRedactor(), func() time.Time { return fixedTime })

	canarySk := "sk-tp-" + "canary-998877"
	err := logger.Log(privacy.LogRecord{
		Level:      privacy.LevelInfo,
		Event:      "gateway_request",
		EntryPoint: "unified_http",
		RequestID:  "req_01ABC",
		TraceID:    "0123456789abcdef0123456789abcdef",
		Fields: map[string]any{
			"httpStatus": 200,
			"messages":   "SECRET_PATIENT_RECORD_998877",
			"authHeader": "Bearer " + canarySk,
			"nestedMeta": map[string]any{
				"clientSecret":      "top-secret",
				"UpstreamLatencyMs": 45,
			},
		},
	})
	if err != nil {
		t.Fatalf("JSONLogger.Log: %v", err)
	}

	raw := buf.String()
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected single-line JSON log, got %d lines: %q", len(lines), raw)
	}
	if strings.Contains(raw, "SECRET_PATIENT_RECORD_998877") || strings.Contains(raw, canarySk) || strings.Contains(raw, "top-secret") {
		t.Fatalf("secret or prompt leaked into JSON log line: %s", raw)
	}

	var parsed map[string]any
	if uerr := json.Unmarshal([]byte(lines[0]), &parsed); uerr != nil {
		t.Fatalf("json.Unmarshal: %v", uerr)
	}
	for k, v := range parsed {
		if k != privacy.NormalizeSnakeCase(k) {
			t.Fatalf("top-level JSON log key %q is not strict snake_case", k)
		}
		if _, isMap := v.(map[string]any); isMap {
			t.Fatalf("expected flat JSON object, key %q had nested map", k)
		}
	}
	if _, hasMessages := parsed["messages"]; hasMessages {
		t.Fatalf("expected payload key 'messages' elided completely from metadata-only log")
	}
	if parsed["nested_meta_client_secret"] != privacy.RedactedMarker {
		t.Fatalf("expected nested clientSecret redacted, got %v", parsed["nested_meta_client_secret"])
	}
	if parsed["http_status"] != float64(200) || parsed["nested_meta_upstream_latency_ms"] != float64(45) {
		t.Fatalf("unexpected flattened fields: %+v", parsed)
	}
}

func TestRedactorPrefixedCookiesAndDebugPayload(t *testing.T) {
	t.Parallel()
	r := privacy.NewDefaultRedactor()

	cookieStr := "tanpopo_session=sess_secret_val; __Host-tanpopo_admin_session=admin_secret_val; theme=dark"
	scrubCookie := r.SanitizeString(cookieStr)
	for _, forbidden := range []string{"sess_secret_val", "admin_secret_val"} {
		if strings.Contains(scrubCookie, forbidden) {
			t.Fatalf("prefixed session cookie leaked %q in %q", forbidden, scrubCookie)
		}
	}
	if !strings.Contains(scrubCookie, "tanpopo_session=[REDACTED]") ||
		!strings.Contains(scrubCookie, "__Host-tanpopo_admin_session=[REDACTED]") {
		t.Fatalf("expected prefixed cookie keys preserved with redacted values: %q", scrubCookie)
	}

	rawJSON := []byte(`{"api_key":"azure-secret","nested":{"client_secret":"cs-123","password":"pw-456"},"messages":[{"role":"user","content":"preserve debug prompt"}]}`)
	cleanJSON := string(r.SanitizeDebugPayload(rawJSON, 4096))
	for _, forbidden := range []string{"azure-secret", "cs-123", "pw-456"} {
		if strings.Contains(cleanJSON, forbidden) {
			t.Fatalf("SanitizeDebugPayload leaked %q: %s", forbidden, cleanJSON)
		}
	}
	if !strings.Contains(cleanJSON, "preserve debug prompt") {
		t.Fatalf("expected prompt content preserved in SanitizeDebugPayload output: %s", cleanJSON)
	}

	// Malformed/truncated JSON fallback also scrubs non-prefixed secrets including escaped quotes.
	truncated := []byte(`{"api_key":"part1\"part2-secret","secret_key":"tail-unclosed`)
	cleanTrunc := string(r.SanitizeDebugPayload(truncated, 4096))
	for _, forbidden := range []string{"part1", "part2-secret", "tail-unclosed"} {
		if strings.Contains(cleanTrunc, forbidden) {
			t.Fatalf("truncated JSON leaked secret %q: %s", forbidden, cleanTrunc)
		}
	}

	// Non-suffixed credential keys, nested credential objects, underscore prefixes, and colon headers.
	advJSON := []byte(`{"secret_key":"sk-val","aws_secret_access_key":"aws-val","signing_key":"sig-val","private_key_pem":"pem-val","encryption_key":"enc-val","access_key":"acc-val","master_key":"mk-val","auth_header":"hdr-val","credentials":{"nested":"val"},"prompt_tokens":12,"prompt":"task-1234"}`)
	cleanAdv := string(r.SanitizeDebugPayload(advJSON, 4096))
	for _, forbidden := range []string{"sk-val", "aws-val", "sig-val", "pem-val", "enc-val", "acc-val", "mk-val", "hdr-val", "nested"} {
		if strings.Contains(cleanAdv, forbidden) {
			t.Fatalf("SanitizeDebugPayload leaked credential %q: %s", forbidden, cleanAdv)
		}
	}
	if !strings.Contains(cleanAdv, `"prompt_tokens":12`) || !strings.Contains(cleanAdv, `"task-1234"`) {
		t.Fatalf("SanitizeDebugPayload corrupted non-secret token count or benign task-1234 string: %s", cleanAdv)
	}

	inline := r.SanitizeString("key_vk_tp_abc123 key_sk-tp-canary99 x-api-key: hdr-secret-val task-1234")
	for _, forbidden := range []string{"vk_tp_abc123", "sk-tp-canary99", "hdr-secret-val"} {
		if strings.Contains(inline, forbidden) {
			t.Fatalf("SanitizeString leaked inline secret %q: %s", forbidden, inline)
		}
	}
	if !strings.Contains(inline, "task-1234") {
		t.Fatalf("SanitizeString falsely redacted benign task-1234: %s", inline)
	}

	// Multi-MiB JSON array is redacted in bounded streaming time and capped strictly <= maxBytes.
	var hugeBuilder strings.Builder
	hugeBuilder.WriteString(`{"api_key":"secret-huge","items":[`)
	for i := 0; i < 400000; i++ {
		if i > 0 {
			hugeBuilder.WriteByte(',')
		}
		hugeBuilder.WriteString(`"item_payload_value"`)
	}
	hugeBuilder.WriteString(`]}`)
	hugeOut := r.SanitizeDebugPayload([]byte(hugeBuilder.String()), 256*1024-16)
	if len(hugeOut) == 0 || len(hugeOut) > 256*1024-16 {
		t.Fatalf("expected bounded output <= 262128 bytes, got %d", len(hugeOut))
	}
	if strings.Contains(string(hugeOut), "secret-huge") {
		t.Fatalf("huge JSON stream leaked api_key")
	}
}

