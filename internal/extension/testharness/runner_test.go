// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package testharness_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/testharness"
	"github.com/nguywnben/tanpopo/internal/testkit/canary"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func TestAC1Tier1AndTier2LifecycleAndReproducibility(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		tier string
	}{
		{name: "acme-tier1", tier: "1"},
		{name: "acme-tier2", tier: "2"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			workDir := filepath.Join(t.TempDir(), tc.name)

			scaf, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{
				Name: tc.name,
				Tier: tc.tier,
				Dir:  workDir,
			})
			if err != nil {
				t.Fatalf("ScaffoldExtension(tier=%s): %v", tc.tier, err)
			}
			if scaf.ExtensionID != "@example/"+tc.name {
				t.Fatalf("unexpected extension id %q", scaf.ExtensionID)
			}

			valReport, _, _, err := testharness.ValidateExtensionDir(ctx, workDir)
			if err != nil {
				t.Fatalf("ValidateExtensionDir(tier=%s): %v", tc.tier, err)
			}
			if valReport.Status != "VALID" || valReport.ExitCode != testharness.ExitOK {
				t.Fatalf("unexpected validation report: %+v", valReport)
			}

			testReport, err := testharness.TestExtension(ctx, testharness.TestOptions{Dir: workDir})
			if err != nil {
				t.Fatalf("TestExtension(tier=%s): %v (report=%+v)", tc.tier, err, testReport)
			}
			if testReport.CasesPassed != 4 || testReport.CasesFailed != 0 || testReport.ExitCode != testharness.ExitOK {
				t.Fatalf("expected 4 passed cases, got %+v", testReport)
			}
			for _, capKey := range []string{"chat_completion", "chat_stream", "tool_calling", "error_normalization"} {
				if got := testReport.CapabilityBadges[capKey]; got != "TESTED" {
					t.Fatalf("capability %s badge = %q, want TESTED", capKey, got)
				}
			}

			pkg1 := filepath.Join(t.TempDir(), "out1.tpkg")
			pkg2 := filepath.Join(t.TempDir(), "out2.tpkg")
			p1, err := testharness.PackExtension(ctx, workDir, pkg1)
			if err != nil {
				t.Fatalf("PackExtension 1: %v", err)
			}
			// Wait slightly to verify epoch-0 timestamp determinism ignores wall-clock drift.
			time.Sleep(15 * time.Millisecond)
			p2, err := testharness.PackExtension(ctx, workDir, pkg2)
			if err != nil {
				t.Fatalf("PackExtension 2: %v", err)
			}
			if p1.SHA256 == "" || p1.SHA256 != p2.SHA256 {
				t.Fatalf("expected bit-for-bit deterministic SHA-256 across packs: %q vs %q", p1.SHA256, p2.SHA256)
			}
			if elapsed := time.Since(start); elapsed >= 5*time.Second {
				t.Fatalf("full lifecycle exceeded 5s budget: %v", elapsed)
			}
		})
	}
}

type buggyAdapterRuntime struct {
	corruptUTF8  bool
	dropDone     bool
	leakCanary   bool
	unapprovedIP bool
}

func (b *buggyAdapterRuntime) BuildValidationProbe(context.Context, port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	return port.UpstreamHttpSpec{}, nil
}

func (b *buggyAdapterRuntime) VerifyValidationResult(context.Context, port.ValidationResultInput) (port.ValidationVerdict, error) {
	return port.ValidationVerdict{Valid: true}, nil
}

func (b *buggyAdapterRuntime) TransformRequest(context.Context, port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	target := "https://api.openai.com/v1/chat/completions"
	if b.unapprovedIP {
		target = "http://169.254.169.254/latest/meta-data"
	}
	return port.UpstreamHttpSpec{
		Method:      http.MethodPost,
		TargetURL:   target,
		SafeHeaders: map[string]string{"Content-Type": "application/json"},
		BodyBytes:   []byte(`{"model":"gpt-4o-mini","stream":true,"stream_options":{"include_usage":true}}`),
		CredentialInjections: []port.CredentialInjectInstruction{
			{
				SecretSlotName: "api_key",
				TargetLocation: port.CredentialTargetLocationHeader,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
	}, nil
}

func (b *buggyAdapterRuntime) TransformResponse(context.Context, port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	return port.NormalizedInferenceResponse{
		NormalizedResponseJSON: []byte(`{"model":"wrong-model","choices":[{"message":{"content":"Wrong"},"finish_reason":"stop"}]}`),
		UpstreamModelID:        "wrong-model",
		FinishReason:           "stop",
	}, nil
}

func (b *buggyAdapterRuntime) TransformStreamChunk(_ context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	if b.corruptUTF8 && in.IsEOF {
		payload, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": "Hello \uFFFD"}},
			},
		})
		return port.StreamChunkOutput{
			NormalizedSSEFrames: [][]byte{append([]byte("data: "), append(payload, []byte("\n\n")...)...), []byte("data: [DONE]\n\n")},
			StreamCompleted:     true,
		}, nil
	}
	if b.dropDone {
		return port.StreamChunkOutput{
			NormalizedSSEFrames: [][]byte{[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello 世\"}}]}\n\n")},
			StreamCompleted:     false,
		}, nil
	}
	return port.StreamChunkOutput{}, nil
}

func (b *buggyAdapterRuntime) NormalizeError(context.Context, port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	msg := "auth denied"
	if b.leakCanary {
		// Adapter echoes canary secret -> MUST be caught by SecretLeakScanner (exit 14) with redacted logs.
		msg = "Authorization failed for Bearer " + canary.DefaultCanarySecret()
	}
	return port.CanonicalProviderError{
		CanonicalCode:           "upstream_auth_failed",
		ErrorType:               "authentication_error",
		TriggerConnectionReauth: true,
		SanitizedPublicMessage:  msg,
	}, nil
}

func (b *buggyAdapterRuntime) ExtractUsage(context.Context, port.ExtractUsageInput) (port.NormalizedUsage, error) {
	return port.NormalizedUsage{}, nil
}

func (b *buggyAdapterRuntime) InspectNativeRequest(context.Context, port.NativeRequestInput) (port.NativeRoutingHints, error) {
	return port.NativeRoutingHints{}, nil
}

func TestAC2SplitStreamDefectDetectionAndNEGCases(t *testing.T) {
	ctx := context.Background()
	workDir := filepath.Join(t.TempDir(), "neg-base")
	if _, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{Name: "neg-base", Tier: "1", Dir: workDir}); err != nil {
		t.Fatalf("ScaffoldExtension: %v", err)
	}
	valReport, vm, _, err := testharness.ValidateExtensionDir(ctx, workDir)
	if err != nil {
		t.Fatalf("ValidateExtensionDir: %v", err)
	}

	t.Run("AC2_CorruptUTF8_ReplacementRune", func(t *testing.T) {
		report, err := testharness.TestExtensionWithRuntime(
			ctx,
			testharness.TestOptions{Dir: workDir, CassettePath: filepath.Join(workDir, "cassettes", "chat_stream.yaml")},
			valReport, vm, &buggyAdapterRuntime{corruptUTF8: true}, time.Now(),
		)
		if testharness.CodeOf(err) != testharness.ExitContract {
			t.Fatalf("expected ExitContract (22), got code=%d err=%v", testharness.CodeOf(err), err)
		}
		if report == nil || report.CasesFailed != 1 {
			t.Fatalf("expected 1 failed case, got %+v", report)
		}
	})

	t.Run("AC2_DroppedDoneSentinel", func(t *testing.T) {
		_, err := testharness.TestExtensionWithRuntime(
			ctx,
			testharness.TestOptions{Dir: workDir, CassettePath: filepath.Join(workDir, "cassettes", "chat_stream.yaml")},
			valReport, vm, &buggyAdapterRuntime{dropDone: true}, time.Now(),
		)
		if testharness.CodeOf(err) != testharness.ExitContract {
			t.Fatalf("expected ExitContract (22) on dropped [DONE], got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})

	t.Run("NEG1_CanarySecretLeakInError", func(t *testing.T) {
		scanner := canary.NewScanner()
		_, err := testharness.TestExtensionWithRuntime(
			ctx,
			testharness.TestOptions{Dir: workDir, CassettePath: filepath.Join(workDir, "cassettes", "error_401.yaml")},
			valReport, vm, &buggyAdapterRuntime{leakCanary: true}, time.Now(),
		)
		if testharness.CodeOf(err) != testharness.ExitSecurity {
			t.Fatalf("expected ExitSecurity (14) on secret leak, got code=%d err=%v", testharness.CodeOf(err), err)
		}
		// Failure diagnostic MUST NOT contain plaintext canary secret.
		scanner.AssertClean(t, canary.NamedData{Source: "error-output", Data: []byte(err.Error())})
	})

	t.Run("NEG2_ContractOutputMismatch", func(t *testing.T) {
		_, err := testharness.TestExtensionWithRuntime(
			ctx,
			testharness.TestOptions{Dir: workDir, CassettePath: filepath.Join(workDir, "cassettes", "chat_sync.yaml")},
			valReport, vm, &buggyAdapterRuntime{}, time.Now(),
		)
		if testharness.CodeOf(err) != testharness.ExitContract {
			t.Fatalf("expected ExitContract (22) on normalized mismatch, got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})

	t.Run("SEC_MetadataIPEgressBlocked", func(t *testing.T) {
		_, err := testharness.TestExtensionWithRuntime(
			ctx,
			testharness.TestOptions{Dir: workDir, CassettePath: filepath.Join(workDir, "cassettes", "chat_sync.yaml")},
			valReport, vm, &buggyAdapterRuntime{unapprovedIP: true}, time.Now(),
		)
		if testharness.CodeOf(err) != testharness.ExitSecurity {
			t.Fatalf("expected ExitSecurity (14) on metadata IP egress, got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})
}

func TestNEG2ManifestSchemaAndEntrypointDigestFailures(t *testing.T) {
	ctx := context.Background()

	t.Run("MissingContractVersion_Exit2", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "missing-cv")
		if _, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{Name: "missing-cv", Tier: "1", Dir: dir}); err != nil {
			t.Fatalf("ScaffoldExtension: %v", err)
		}
		mPath := filepath.Join(dir, "manifest.yaml")
		raw, _ := os.ReadFile(mPath)
		mutated := strings.ReplaceAll(string(raw), "  contractVersion: \"0.1\"\n", "")
		_ = os.WriteFile(mPath, []byte(mutated), 0o644)

		_, _, _, err := testharness.ValidateExtensionDir(ctx, dir)
		if testharness.CodeOf(err) != testharness.ExitValidation {
			t.Fatalf("expected ExitValidation (2), got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})

	t.Run("WildcardEgressHost_Exit14", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "wildcard-host")
		if _, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{Name: "wildcard-host", Tier: "1", Dir: dir}); err != nil {
			t.Fatalf("ScaffoldExtension: %v", err)
		}
		mPath := filepath.Join(dir, "manifest.yaml")
		raw, _ := os.ReadFile(mPath)
		mutated := strings.ReplaceAll(string(raw), "\"api.openai.com\"", "\"*.openai.com\"")
		_ = os.WriteFile(mPath, []byte(mutated), 0o644)

		_, _, _, err := testharness.ValidateExtensionDir(ctx, dir)
		if testharness.CodeOf(err) != testharness.ExitSecurity {
			t.Fatalf("expected ExitSecurity (14) on wildcard egress host, got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})

	t.Run("WasmEntrypointSHA256Mismatch_Exit22", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bad-sha")
		if _, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{Name: "bad-sha", Tier: "2", Dir: dir}); err != nil {
			t.Fatalf("ScaffoldExtension: %v", err)
		}
		mPath := filepath.Join(dir, "manifest.yaml")
		raw, _ := os.ReadFile(mPath)
		idx := strings.Index(string(raw), "sha256: \"")
		if idx < 0 {
			t.Fatalf("manifest missing sha256 line")
		}
		oldDigest := string(raw)[idx+9 : idx+9+64]
		badDigest := strings.Repeat("0", 64)
		if oldDigest == badDigest {
			badDigest = strings.Repeat("a", 64)
		}
		_ = os.WriteFile(mPath, []byte(strings.ReplaceAll(string(raw), oldDigest, badDigest)), 0o644)

		_, _, _, err := testharness.ValidateExtensionDir(ctx, dir)
		if testharness.CodeOf(err) != testharness.ExitContract && !errors.Is(err, installer.ErrEntrypointMismatch) {
			t.Fatalf("expected ExitContract (22) on entrypoint sha256 mismatch, got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})

	t.Run("InvalidQuotasAndOriginTrustTier_Exit2", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bad-schema")
		if _, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{Name: "bad-schema", Tier: "2", Dir: dir}); err != nil {
			t.Fatalf("ScaffoldExtension: %v", err)
		}
		mPath := filepath.Join(dir, "manifest.yaml")
		raw, _ := os.ReadFile(mPath)
		mutated := strings.ReplaceAll(string(raw), "    maxCpuMsPerCall: 50\n", "    maxCpuMsPerCall: 0\n")
		_ = os.WriteFile(mPath, []byte(mutated), 0o644)

		_, _, _, err := testharness.ValidateExtensionDir(ctx, dir)
		if testharness.CodeOf(err) != testharness.ExitValidation {
			t.Fatalf("expected ExitValidation (2) on maxCpuMsPerCall: 0, got code=%d err=%v", testharness.CodeOf(err), err)
		}
	})
}

func TestTier2WasmAndWatSourceParityAndSanitization(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "wasm-parity")
	if _, err := testharness.ScaffoldExtension(testharness.ScaffoldOptions{Name: "wasm-parity", Tier: "2", Dir: dir}); err != nil {
		t.Fatalf("ScaffoldExtension: %v", err)
	}
	watRaw, err := os.ReadFile(filepath.Join(dir, "src", "adapter.wat"))
	if err != nil {
		t.Fatalf("read src/adapter.wat: %v", err)
	}
	watText := string(watRaw)
	for _, exp := range []string{
		`export "memory"`,
		`export "build_validation_probe"`,
		`export "verify_validation_result"`,
		`export "transform_request"`,
		`export "transform_response"`,
		`export "transform_stream_chunk"`,
		`export "normalize_error"`,
		`export "extract_usage"`,
		`export "inspect_native_request"`,
		`export "evaluate_guardrail"`,
		`export "transform"`,
	} {
		if !strings.Contains(watText, exp) {
			t.Fatalf("src/adapter.wat missing %s", exp)
		}
	}
	if strings.Contains(watText, "stub") || !strings.Contains(watText, `func $transform_stream_chunk`) {
		t.Fatalf("src/adapter.wat appears incomplete or stubbed")
	}

	// ExpectedUpstreamHeaders mismatch MUST NOT leak plaintext canary secret.
	casPath := filepath.Join(dir, "cassettes", "hdr_mismatch.yaml")
	casYAML := `case_id: "hdr_mismatch_redacted"
capability: "chat_completion"
secret_slots:
  api_key: "{{TANPOPO_CANARY_API_KEY}}"
normalized_input:
  model: "gpt-4o-mini"
  messages:
    - role: "user"
      content: "hi"
upstream_recorded_exchange:
  status: 200
  expected_upstream_headers:
    Authorization: "Bearer wrong-expected-{{TANPOPO_CANARY_API_KEY}}"
  raw_body: '{"id":"1","model":"gpt-4o-mini","choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}'
expected_normalized_output:
  model: "gpt-4o-mini"
`
	if err := os.WriteFile(casPath, []byte(casYAML), 0o644); err != nil {
		t.Fatalf("write cassette: %v", err)
	}
	scanner := canary.NewScanner()
	report, err := testharness.TestExtension(ctx, testharness.TestOptions{Dir: dir, CassettePath: casPath})
	if testharness.CodeOf(err) != testharness.ExitContract {
		t.Fatalf("expected ExitContract (22) on upstream header mismatch, got code=%d err=%v", testharness.CodeOf(err), err)
	}
	scanner.AssertClean(t, canary.NamedData{Source: "header-mismatch-err", Data: []byte(err.Error())})
	if report != nil && len(report.Cases) == 1 {
		scanner.AssertClean(t, canary.NamedData{Source: "header-mismatch-case", Data: []byte(report.Cases[0].Error)})
	}

	// Verify SanitizeTerminalText strips ESC, \r, DEL, and UTF-8 C1 controls while keeping CJK runes.
	dirty := "ok\r\x1b[31mred\x1b[0m\x1b]52;c;YQ==\x07\u0085\u009b世界\x7f!"
	clean := testharness.SanitizeTerminalText(dirty)
	want := "okred世界!"
	if clean != want {
		t.Fatalf("SanitizeTerminalText = %q, want %q", clean, want)
	}
}

