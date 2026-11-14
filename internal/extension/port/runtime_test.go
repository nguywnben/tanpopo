// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package port

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

type stubRuntime struct{}

func (stubRuntime) BuildValidationProbe(_ context.Context, in ValidationProbeInput) (UpstreamHttpSpec, error) {
	return UpstreamHttpSpec{
		Method:    "GET",
		TargetURL: "https://api.example.com/v1/models",
		SafeHeaders: map[string]string{
			"X-Request-Id": in.RequestID,
		},
		CredentialInjections: []CredentialInjectInstruction{
			{
				SecretSlotName: "api_key",
				TargetLocation: CredentialTargetLocationHeader,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
		},
	}, nil
}

func (stubRuntime) VerifyValidationResult(_ context.Context, in ValidationResultInput) (ValidationVerdict, error) {
	return ValidationVerdict{Valid: in.HTTPStatus == 200}, nil
}

func (stubRuntime) TransformRequest(_ context.Context, _ TransformRequestInput) (UpstreamHttpSpec, error) {
	return UpstreamHttpSpec{Method: "POST", TargetURL: "https://api.example.com/v1/chat/completions"}, nil
}

func (stubRuntime) TransformResponse(_ context.Context, _ TransformResponseInput) (NormalizedInferenceResponse, error) {
	return NormalizedInferenceResponse{}, nil
}

func (stubRuntime) TransformStreamChunk(_ context.Context, in StreamChunkInput) (StreamChunkOutput, error) {
	return StreamChunkOutput{StreamCompleted: in.IsEOF}, nil
}

func (stubRuntime) NormalizeError(_ context.Context, _ NormalizeErrorInput) (CanonicalProviderError, error) {
	return CanonicalProviderError{CanonicalCode: "upstream_error"}, nil
}

func (stubRuntime) ExtractUsage(_ context.Context, _ ExtractUsageInput) (NormalizedUsage, error) {
	return NormalizedUsage{Accuracy: UsageAccuracyExact}, nil
}

func (stubRuntime) InspectNativeRequest(_ context.Context, _ NativeRequestInput) (NativeRoutingHints, error) {
	return NativeRoutingHints{}, nil
}

func (stubRuntime) EvaluateGuardrail(_ context.Context, _ GuardrailInput) (GuardrailVerdict, error) {
	return GuardrailVerdict{Allowed: true}, nil
}

func (stubRuntime) CancelExecution(_ context.Context, _ CancelExecutionSignal) (CancelAck, error) {
	return CancelAck{Released: true}, nil
}

var (
	_ ProviderRuntime  = stubRuntime{}
	_ ExtensionRuntime = stubRuntime{}
)

func TestUpstreamHttpSpecValidate(t *testing.T) {
	t.Parallel()

	validSpec := UpstreamHttpSpec{
		Method:    "POST",
		TargetURL: "https://api.example.com/v1/chat/completions",
		SafeHeaders: map[string]string{
			"Content-Type": "application/json",
		},
		CredentialInjections: []CredentialInjectInstruction{
			{
				SecretSlotName: "primary_api_key",
				TargetLocation: CredentialTargetLocationHeader,
				FieldName:      "Authorization",
				ValuePrefix:    "Bearer ",
			},
			{
				SecretSlotName: "hmac_key",
				TargetLocation: CredentialTargetLocationHostCryptoSign,
				FieldName:      "X-Signature",
				CryptoSign: &HostCryptoSignSpec{
					Algorithm:        SigningAlgorithmHMACSHA256,
					CanonicalPayload: []byte("POST\n/v1/chat/completions"),
					OutputEncoding:   SignatureEncodingHexLower,
				},
			},
		},
	}
	if err := validSpec.Validate(); err != nil {
		t.Fatalf("expected valid spec to pass, got %v", err)
	}

	raw, err := json.Marshal(validSpec)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded UpstreamHttpSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("round-trip decoded spec failed validation: %v", err)
	}

	cookieSpec := UpstreamHttpSpec{
		Method:    "GET",
		TargetURL: "https://api.example.com/v1/models",
		CredentialInjections: []CredentialInjectInstruction{
			{
				SecretSlotName: "session",
				TargetLocation: CredentialTargetLocationHeader,
				FieldName:      "cOoKiE",
			},
		},
	}
	if err := cookieSpec.Validate(); !errors.Is(err, ErrProhibitedCookieInjection) {
		t.Fatalf("expected ErrProhibitedCookieInjection for Cookie injection, got %v", err)
	}

	safeCookieHeaderSpec := UpstreamHttpSpec{
		Method:    "GET",
		TargetURL: "https://api.example.com/v1/models",
		SafeHeaders: map[string]string{
			"Set-Cookie": "sid=bad",
		},
	}
	if err := safeCookieHeaderSpec.Validate(); !errors.Is(err, ErrProhibitedCookieInjection) {
		t.Fatalf("expected ErrProhibitedCookieInjection for SafeHeaders Set-Cookie, got %v", err)
	}
}

func TestEnumValidation(t *testing.T) {
	t.Parallel()

	for _, acc := range []UsageAccuracy{UsageAccuracyExact, UsageAccuracyEstimated, UsageAccuracyUnmeteredFlagged} {
		if !acc.IsValid() {
			t.Fatalf("expected %q to be valid", acc)
		}
	}
	if UsageAccuracy("BAD").IsValid() {
		t.Fatalf("expected unknown UsageAccuracy to be invalid")
	}

	if !SigningAlgorithmHMACSHA256.IsValid() || SigningAlgorithm("MD5").IsValid() {
		t.Fatalf("unexpected SigningAlgorithm validation result")
	}
	if !SignatureEncodingBase64URL.IsValid() || SignatureEncoding("ROT13").IsValid() {
		t.Fatalf("unexpected SignatureEncoding validation result")
	}
	if !CredentialTargetLocationHostCryptoSign.IsValid() || CredentialTargetLocation("BODY").IsValid() {
		t.Fatalf("unexpected CredentialTargetLocation validation result")
	}
	if !GuardrailStagePreRequest.IsValid() || !GuardrailStagePostResponse.IsValid() || GuardrailStage("UNKNOWN").IsValid() {
		t.Fatalf("unexpected GuardrailStage validation result")
	}
}

func TestUpstreamHttpSpecValidateNegativeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec UpstreamHttpSpec
		want error
	}{
		{
			name: "bad_method",
			spec: UpstreamHttpSpec{Method: "POST /bad", TargetURL: "https://api.example.com/v1"},
			want: ErrInvalidSpec,
		},
		{
			name: "ftp_url",
			spec: UpstreamHttpSpec{Method: "GET", TargetURL: "ftp://api.example.com/v1"},
			want: ErrInvalidSpec,
		},
		{
			name: "userinfo_url",
			spec: UpstreamHttpSpec{Method: "GET", TargetURL: "https://u:p@api.example.com/v1"},
			want: ErrInvalidSpec,
		},
		{
			name: "negative_timeout",
			spec: UpstreamHttpSpec{Method: "GET", TargetURL: "https://api.example.com/v1", TimeoutOverrideMs: -1},
			want: ErrInvalidSpec,
		},
		{
			name: "space_in_header_name",
			spec: UpstreamHttpSpec{Method: "GET", TargetURL: "https://api.example.com/v1", SafeHeaders: map[string]string{"X Bad": "1"}},
			want: ErrInvalidSpec,
		},
		{
			name: "crlf_in_header_value",
			spec: UpstreamHttpSpec{Method: "GET", TargetURL: "https://api.example.com/v1", SafeHeaders: map[string]string{"X-Test": "ok\r\nEvil: 1"}},
			want: ErrInvalidSpec,
		},
		{
			name: "hop_by_hop_safe_header",
			spec: UpstreamHttpSpec{Method: "GET", TargetURL: "https://api.example.com/v1", SafeHeaders: map[string]string{"Transfer-Encoding": "chunked"}},
			want: ErrInvalidSpec,
		},
		{
			name: "bad_crypto_sign_missing",
			spec: UpstreamHttpSpec{
				Method:    "POST",
				TargetURL: "https://api.example.com/v1",
				CredentialInjections: []CredentialInjectInstruction{
					{SecretSlotName: "k", TargetLocation: CredentialTargetLocationHostCryptoSign, FieldName: "X-Sig"},
				},
			},
			want: ErrInvalidSpec,
		},
		{
			name: "bad_query_param_field",
			spec: UpstreamHttpSpec{
				Method:    "GET",
				TargetURL: "https://api.example.com/v1",
				CredentialInjections: []CredentialInjectInstruction{
					{SecretSlotName: "k", TargetLocation: CredentialTargetLocationQueryParam, FieldName: "a=b"},
				},
			},
			want: ErrInvalidSpec,
		},
		{
			name: "hop_by_hop_credential_field",
			spec: UpstreamHttpSpec{
				Method:    "GET",
				TargetURL: "https://api.example.com/v1",
				CredentialInjections: []CredentialInjectInstruction{
					{SecretSlotName: "k", TargetLocation: CredentialTargetLocationHeader, FieldName: "Connection"},
				},
			},
			want: ErrInvalidSpec,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.spec.Validate(); !errors.Is(err, tc.want) {
				t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, err)
			}
		})
	}
}
