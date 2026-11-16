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
