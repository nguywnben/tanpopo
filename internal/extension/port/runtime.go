// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package port defines the shared ProviderRuntime interface and Host-Guest data
// contracts aligned 1:1 with proto/tanpopo/extension/v1/runtime.proto so
// Tier-1 Declarative (TP-IMP-0012), Tier-2 Wasm (TP-IMP-0013), and Tier-3
// Isolated Sidecar (TP-IMP-0042) runtimes develop in parallel without signature
// drift.
//
// DAG Boundary (Tier 5): Imports Go standard library only so all extension
// implementations and Data Plane callers depend on stable value types without
// dragging in storage, vault, or network egress concrete drivers.
package port

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Canonical extension runtime errors mapped by Data Plane ingress and fault guards.
var (
	// ErrInvalidSpec indicates a malformed UpstreamHttpSpec or Host-Guest envelope.
	ErrInvalidSpec = errors.New("extension: invalid runtime specification")
	// ErrExecutionFault maps guest traps, timeouts, memory exhaustion, or ABI violations
	// onto canonical 502 extension_execution_fault without crashing the Core process.
	ErrExecutionFault = errors.New("extension: guest execution fault")
	// ErrProhibitedCookieInjection indicates Cookie or Set-Cookie was requested in outbound
	// headers or CredentialInjectInstruction.FieldName (DEC-004 / CR-05).
	ErrProhibitedCookieInjection = errors.New("extension: cookie and set-cookie injection prohibited")
)

// UsageAccuracy classifies FinOps settlement precision for the two-phase micro-USD ledger.
type UsageAccuracy string

const (
	UsageAccuracyUnspecified     UsageAccuracy = "USAGE_ACCURACY_UNSPECIFIED"
	UsageAccuracyExact           UsageAccuracy = "USAGE_ACCURACY_EXACT"
	UsageAccuracyEstimated       UsageAccuracy = "USAGE_ACCURACY_ESTIMATED"
	UsageAccuracyUnmeteredFlagged UsageAccuracy = "USAGE_ACCURACY_UNMETERED_FLAGGED"
)

// IsValid reports whether u is a recognized UsageAccuracy value.
func (u UsageAccuracy) IsValid() bool {
	switch u {
	case UsageAccuracyUnspecified,
		UsageAccuracyExact,
		UsageAccuracyEstimated,
		UsageAccuracyUnmeteredFlagged:
		return true
	default:
		return false
	}
}

// SigningAlgorithm enumerates Host-executed cryptographic request-signing primitives.
type SigningAlgorithm string

const (
	SigningAlgorithmUnspecified SigningAlgorithm = "SIGNING_ALGORITHM_UNSPECIFIED"
	SigningAlgorithmHMACSHA256  SigningAlgorithm = "SIGNING_ALGORITHM_HMAC_SHA256"
	SigningAlgorithmAWSSigV4    SigningAlgorithm = "SIGNING_ALGORITHM_AWS_SIGV4"
	SigningAlgorithmRSASHA256   SigningAlgorithm = "SIGNING_ALGORITHM_RSA_SHA256"
)

// IsValid reports whether a is a recognized SigningAlgorithm value.
func (a SigningAlgorithm) IsValid() bool {
	switch a {
	case SigningAlgorithmUnspecified,
		SigningAlgorithmHMACSHA256,
		SigningAlgorithmAWSSigV4,
		SigningAlgorithmRSASHA256:
		return true
	default:
		return false
	}
}

// SignatureEncoding controls how Host formats computed signature bytes before attachment.
type SignatureEncoding string

const (
	SignatureEncodingUnspecified SignatureEncoding = "SIGNATURE_ENCODING_UNSPECIFIED"
	SignatureEncodingHexLower    SignatureEncoding = "SIGNATURE_ENCODING_HEX_LOWER"
	SignatureEncodingBase64      SignatureEncoding = "SIGNATURE_ENCODING_BASE64"
	SignatureEncodingBase64URL   SignatureEncoding = "SIGNATURE_ENCODING_BASE64URL"
)

// IsValid reports whether e is a recognized SignatureEncoding value.
func (e SignatureEncoding) IsValid() bool {
	switch e {
	case SignatureEncodingUnspecified,
		SignatureEncodingHexLower,
		SignatureEncodingBase64,
		SignatureEncodingBase64URL:
		return true
	default:
		return false
	}
}

// CredentialTargetLocation specifies where the Host Blind Credential Broker injects credentials.
type CredentialTargetLocation string

const (
	CredentialTargetLocationUnspecified    CredentialTargetLocation = "CREDENTIAL_TARGET_LOCATION_UNSPECIFIED"
	CredentialTargetLocationHeader         CredentialTargetLocation = "CREDENTIAL_TARGET_LOCATION_HEADER"
	CredentialTargetLocationQueryParam     CredentialTargetLocation = "CREDENTIAL_TARGET_LOCATION_QUERY_PARAM"
	CredentialTargetLocationHostCryptoSign CredentialTargetLocation = "CREDENTIAL_TARGET_LOCATION_HOST_CRYPTO_SIGN"
)

// IsValid reports whether l is a recognized CredentialTargetLocation value.
func (l CredentialTargetLocation) IsValid() bool {
	switch l {
	case CredentialTargetLocationUnspecified,
		CredentialTargetLocationHeader,
		CredentialTargetLocationQueryParam,
		CredentialTargetLocationHostCryptoSign:
		return true
	default:
		return false
	}
}

// GuardrailStage identifies the pipeline hook stage for policy and content guardrail extensions.
type GuardrailStage string

const (
	GuardrailStageUnspecified  GuardrailStage = "GUARDRAIL_STAGE_UNSPECIFIED"
	GuardrailStagePreRequest   GuardrailStage = "GUARDRAIL_STAGE_PRE_REQUEST"
	GuardrailStagePostResponse GuardrailStage = "GUARDRAIL_STAGE_POST_RESPONSE"
)

// IsValid reports whether s is a recognized GuardrailStage value.
func (s GuardrailStage) IsValid() bool {
	switch s {
	case GuardrailStageUnspecified,
		GuardrailStagePreRequest,
		GuardrailStagePostResponse:
		return true
	default:
		return false
	}
}

// BlobRef represents an out-of-band multimodal payload handle (> 64 KiB up to 32 MiB)
// held in Host streaming buffers outside Tier-2 Wasm linear memory.
type BlobRef struct {
	BlobID     string `json:"blob_id"`
	URI        string `json:"uri"`
	MimeType   string `json:"mime_type"`
	ByteLength int64  `json:"byte_length"`
	SHA256Hex  string `json:"sha256_hex"`
}

// HostCryptoSignSpec instructs the Host Blind Broker to sign canonical payload bytes
// using a vault-sealed secret slot.
type HostCryptoSignSpec struct {
	Algorithm               SigningAlgorithm  `json:"algorithm"`
	CanonicalPayload        []byte            `json:"canonical_payload,omitempty"`
	OutputEncoding          SignatureEncoding `json:"output_encoding"`
	AWSRegion               string            `json:"aws_region,omitempty"`
	AWSService              string            `json:"aws_service,omitempty"`
	SigningTimestampEpochSec int64            `json:"signing_timestamp_epoch_sec,omitempty"`
	SignedHeaders           []string          `json:"signed_headers,omitempty"`
}

// CredentialInjectInstruction binds an approved manifest secret slot to an outbound
// HTTP header, query parameter, or Host signature.
type CredentialInjectInstruction struct {
	SecretSlotName string                   `json:"secret_slot_name"`
	TargetLocation CredentialTargetLocation `json:"target_location"`
	FieldName      string                   `json:"field_name"`
	ValuePrefix    string                   `json:"value_prefix,omitempty"`
	CryptoSign     *HostCryptoSignSpec      `json:"crypto_sign,omitempty"`
}

// isValidHTTPToken checks RFC 9110 tchar token syntax for HTTP header field names.
func isValidHTTPToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func isProhibitedFramingOrHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "host", "connection", "transfer-encoding", "te", "trailer", "upgrade",
		"content-length", "keep-alive", "proxy-connection", "proxy-authorization":
		return true
	default:
		return false
	}
}

// Validate checks structural invariants and enforces DEC-004's Cookie/Set-Cookie ban.
func (c CredentialInjectInstruction) Validate() error {
	if strings.TrimSpace(c.SecretSlotName) == "" {
		return fmt.Errorf("%w: secret_slot_name is required", ErrInvalidSpec)
	}
	field := strings.TrimSpace(c.FieldName)
	if field == "" {
		return fmt.Errorf("%w: field_name is required", ErrInvalidSpec)
	}
	if strings.EqualFold(field, "cookie") || strings.EqualFold(field, "set-cookie") {
		return fmt.Errorf("%w: header %q is forbidden by DEC-004", ErrProhibitedCookieInjection, c.FieldName)
	}
	if strings.ContainsAny(c.FieldName, "\r\n\x00") || strings.ContainsAny(c.ValuePrefix, "\r\n\x00") {
		return fmt.Errorf("%w: control characters are forbidden in credential instruction", ErrInvalidSpec)
	}
	switch c.TargetLocation {
	case CredentialTargetLocationHeader:
		if !isValidHTTPToken(field) || isProhibitedFramingOrHopHeader(field) {
			return fmt.Errorf("%w: invalid or prohibited HTTP header field_name %q", ErrInvalidSpec, c.FieldName)
		}
		return nil
	case CredentialTargetLocationQueryParam:
		if strings.ContainsAny(field, "=&?# \t") || field != c.FieldName {
			return fmt.Errorf("%w: invalid query param field_name %q", ErrInvalidSpec, c.FieldName)
		}
		return nil
	case CredentialTargetLocationHostCryptoSign:
		if !isValidHTTPToken(field) || isProhibitedFramingOrHopHeader(field) {
			return fmt.Errorf("%w: invalid or prohibited HTTP signature header field_name %q", ErrInvalidSpec, c.FieldName)
		}
		if c.CryptoSign == nil {
			return fmt.Errorf("%w: crypto_sign is required when target_location is HOST_CRYPTO_SIGN", ErrInvalidSpec)
		}
		if !c.CryptoSign.Algorithm.IsValid() || c.CryptoSign.Algorithm == SigningAlgorithmUnspecified {
			return fmt.Errorf("%w: valid signing algorithm is required", ErrInvalidSpec)
		}
		if !c.CryptoSign.OutputEncoding.IsValid() {
			return fmt.Errorf("%w: invalid signature output encoding %q", ErrInvalidSpec, c.CryptoSign.OutputEncoding)
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported credential target_location %q", ErrInvalidSpec, c.TargetLocation)
	}
}

// UpstreamHttpSpec describes the outbound HTTP request returned by a Provider Adapter
// to be executed strictly by Tanpopo Host Core via SSRFGuard + BlindBroker.
type UpstreamHttpSpec struct {
	Method               string                        `json:"method"`
	TargetURL            string                        `json:"target_url"`
	SafeHeaders          map[string]string             `json:"safe_headers,omitempty"`
	BodyBytes            []byte                        `json:"body_bytes,omitempty"`
	OutboundBlobSplices  []BlobRef                     `json:"outbound_blob_splices,omitempty"`
	CredentialInjections []CredentialInjectInstruction `json:"credential_injections,omitempty"`
	TimeoutOverrideMs    int32                         `json:"timeout_override_ms,omitempty"`
}

// Validate enforces syntactic and security invariants on UpstreamHttpSpec prior to egress.
func (s UpstreamHttpSpec) Validate() error {
	method := strings.TrimSpace(s.Method)
	if !isValidHTTPToken(method) {
		return fmt.Errorf("%w: valid HTTP method is required", ErrInvalidSpec)
	}
	if strings.TrimSpace(s.TargetURL) == "" {
		return fmt.Errorf("%w: target_url is required", ErrInvalidSpec)
	}
	u, err := url.Parse(s.TargetURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("%w: invalid target_url %q", ErrInvalidSpec, s.TargetURL)
	}
	if s.TimeoutOverrideMs < 0 {
		return fmt.Errorf("%w: timeout_override_ms cannot be negative", ErrInvalidSpec)
	}
	for k, v := range s.SafeHeaders {
		tk := strings.TrimSpace(k)
		if !isValidHTTPToken(tk) || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("%w: invalid header %q", ErrInvalidSpec, k)
		}
		if strings.EqualFold(tk, "cookie") || strings.EqualFold(tk, "set-cookie") {
			return fmt.Errorf("%w: header %q is forbidden in safe_headers by DEC-004", ErrProhibitedCookieInjection, k)
		}
		if isProhibitedFramingOrHopHeader(tk) || strings.EqualFold(tk, "authorization") {
			return fmt.Errorf("%w: header %q is forbidden in safe_headers", ErrInvalidSpec, k)
		}
	}
	for _, inj := range s.CredentialInjections {
		if err := inj.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidationProbeInput initiates Phase 1 of blind connection validation.
type ValidationProbeInput struct {
	RequestID       string   `json:"request_id"`
	ConfigJSON      string   `json:"config_json"`
	SecretSlotNames []string `json:"secret_slot_names,omitempty"`
}

// ValidationResultInput delivers the Host-sanitized probe response into Phase 2.
type ValidationResultInput struct {
	RequestID        string            `json:"request_id"`
	HTTPStatus       int32             `json:"http_status"`
	SanitizedHeaders map[string]string `json:"sanitized_headers,omitempty"`
	ResponseBody     []byte            `json:"response_body,omitempty"`
}

// ValidationVerdict reports whether connection credentials and endpoint configuration are healthy.
type ValidationVerdict struct {
	Valid            bool     `json:"valid"`
	DetectedModelIDs []string `json:"detected_model_ids,omitempty"`
	FailureCode      string   `json:"failure_code,omitempty"`
	SanitizedMessage string   `json:"sanitized_message,omitempty"`
}

// TransformRequestInput carries the compact normalized request envelope into Guest.
type TransformRequestInput struct {
	RequestID             string            `json:"request_id"`
	ConfigJSON            string            `json:"config_json"`
	NormalizedRequestJSON []byte            `json:"normalized_request_json"`
	BlobRefs              []BlobRef         `json:"blob_refs,omitempty"`
	SecretSlotRefs        map[string]string `json:"secret_slot_refs,omitempty"`
}

// TransformResponseInput carries a synchronous upstream HTTP response into Guest.
type TransformResponseInput struct {
	RequestID        string            `json:"request_id"`
	HTTPStatus       int32             `json:"http_status"`
	SanitizedHeaders map[string]string `json:"sanitized_headers,omitempty"`
	RawResponseBody  []byte            `json:"raw_response_body"`
	Modality         string            `json:"modality"`
}

// NormalizedInferenceResponse contains the unified JSON payload and extracted usage counters.
type NormalizedInferenceResponse struct {
	NormalizedResponseJSON []byte           `json:"normalized_response_json"`
	Usage                  *NormalizedUsage `json:"usage,omitempty"`
	UpstreamModelID        string           `json:"upstream_model_id,omitempty"`
	FinishReason           string           `json:"finish_reason,omitempty"`
}

// StreamChunkInput feeds an incremental upstream stream slice into TransformStreamChunk.
type StreamChunkInput struct {
	RequestID        string `json:"request_id"`
	SessionState     []byte `json:"session_state,omitempty"`
	RawUpstreamChunk []byte `json:"raw_upstream_chunk"`
	IsEOF            bool   `json:"is_eof"`
}

// StreamChunkOutput emits zero or more normalized SSE frames plus updated bounded scratch state.
type StreamChunkOutput struct {
	NextSessionState    []byte           `json:"next_session_state,omitempty"`
	NormalizedSSEFrames [][]byte         `json:"normalized_sse_frames,omitempty"`
	IncrementalUsage    *NormalizedUsage `json:"incremental_usage,omitempty"`
	StreamCompleted     bool             `json:"stream_completed"`
}

// MediaUsage tracks non-text multimodal billing units for media settlement.
type MediaUsage struct {
	ImageCount          int32   `json:"image_count,omitempty"`
	ImagePixels         int64   `json:"image_pixels,omitempty"`
	AudioInputSeconds   float64 `json:"audio_input_seconds,omitempty"`
	AudioOutputSeconds  float64 `json:"audio_output_seconds,omitempty"`
	DocumentPages       int32   `json:"document_pages,omitempty"`
	RerankSearchUnits   int32   `json:"rerank_search_units,omitempty"`
}

// NormalizedUsage standardizes 5-component token accounting plus multimodal unit usage.
type NormalizedUsage struct {
	InputTokens      int64         `json:"input_tokens"`
	OutputTokens     int64         `json:"output_tokens"`
	CacheReadTokens  int64         `json:"cache_read_tokens"`
	CacheWriteTokens int64         `json:"cache_write_tokens"`
	ReasoningTokens  int64         `json:"reasoning_tokens"`
	Accuracy         UsageAccuracy `json:"accuracy"`
	MediaUsage       *MediaUsage   `json:"media_usage,omitempty"`
}

// ExtractUsageInput provides raw upstream response/trailer payloads alongside Host byte fallbacks.
type ExtractUsageInput struct {
	RequestID                 string    `json:"request_id"`
	Modality                  string    `json:"modality"`
	RawResponseOrTrailerBytes []byte    `json:"raw_response_or_trailer_bytes"`
	FallbackUTF8InputBytes    int64     `json:"fallback_utf8_input_bytes"`
	FallbackUTF8OutputBytes   int64     `json:"fallback_utf8_output_bytes"`
	InputBlobs                []BlobRef `json:"input_blobs,omitempty"`
}

// NormalizeErrorInput supplies Host-scrubbed upstream HTTP failure details to NormalizeError.
type NormalizeErrorInput struct {
	RequestID        string            `json:"request_id"`
	HTTPStatus       int32             `json:"http_status"`
	SanitizedHeaders map[string]string `json:"sanitized_headers,omitempty"`
	SanitizedBody    []byte            `json:"sanitized_body,omitempty"`
}

// CanonicalProviderError drives deterministic SM-02 / SM-04 circuit-breaker and failover logic.
type CanonicalProviderError struct {
	CanonicalCode            string `json:"canonical_code"`
	ErrorType                string `json:"error_type"`
	RetryablePreStream       bool   `json:"retryable_pre_stream"`
	CooldownMs               int32  `json:"cooldown_ms,omitempty"`
	TriggerConnectionReauth  bool   `json:"trigger_connection_reauth"`
	SanitizedPublicMessage   string `json:"sanitized_public_message"`
}

// NativeRequestInput passes metadata and bounded headers/body of a /native/{dialect}/{upstreamPath} request.
type NativeRequestInput struct {
	RequestID        string            `json:"request_id"`
	Dialect          string            `json:"dialect"`
	UpstreamPath     string            `json:"upstream_path"`
	SanitizedHeaders map[string]string `json:"sanitized_headers,omitempty"`
	RawRequestBody   []byte            `json:"raw_request_body,omitempty"`
	BlobRefs         []BlobRef         `json:"blob_refs,omitempty"`
}

// NativeRoutingHints enables Core policy, capability routing, and Phase-1 budget holds on /native/* calls.
type NativeRoutingHints struct {
	TargetModel          string                        `json:"target_model"`
	Stream               bool                          `json:"stream"`
	RequiredCapabilities []string                      `json:"required_capabilities,omitempty"`
	MaxOutputTokensHint  int64                         `json:"max_output_tokens_hint,omitempty"`
	CredentialInjections []CredentialInjectInstruction `json:"credential_injections,omitempty"`
}

// GuardrailInput carries pre-request or post-response JSON payloads into a guardrail extension.
type GuardrailInput struct {
	RequestID   string         `json:"request_id"`
	Stage       GuardrailStage `json:"stage"`
	PayloadJSON []byte         `json:"payload_json"`
}

// GuardrailVerdict determines whether a payload passes policy inspection or is redacted/blocked.
type GuardrailVerdict struct {
	Allowed             bool   `json:"allowed"`
	ViolationCode       string `json:"violation_code,omitempty"`
	RedactedPayloadJSON []byte `json:"redacted_payload_json,omitempty"`
	SanitizedReason     string `json:"sanitized_reason,omitempty"`
}

// CancelExecutionSignal notifies Guest to release per-request state associated with request_id.
type CancelExecutionSignal struct {
	RequestID string `json:"request_id"`
	Reason    string `json:"reason,omitempty"`
}

// CancelAck confirms Guest scratch state teardown.
type CancelAck struct {
	Released bool `json:"released"`
}

// ProviderRuntime is the canonical 8-method Host-Extension runtime contract shared
// identically across Tier-1 Declarative (TP-IMP-0012), Tier-2 Wasm Sandbox (TP-IMP-0013),
// and Tier-3 Isolated Sidecar (TP-IMP-0042) implementations.
type ProviderRuntime interface {
	BuildValidationProbe(ctx context.Context, in ValidationProbeInput) (UpstreamHttpSpec, error)
	VerifyValidationResult(ctx context.Context, in ValidationResultInput) (ValidationVerdict, error)
	TransformRequest(ctx context.Context, in TransformRequestInput) (UpstreamHttpSpec, error)
	TransformResponse(ctx context.Context, in TransformResponseInput) (NormalizedInferenceResponse, error)
	TransformStreamChunk(ctx context.Context, in StreamChunkInput) (StreamChunkOutput, error)
	NormalizeError(ctx context.Context, in NormalizeErrorInput) (CanonicalProviderError, error)
	ExtractUsage(ctx context.Context, in ExtractUsageInput) (NormalizedUsage, error)
	InspectNativeRequest(ctx context.Context, in NativeRequestInput) (NativeRoutingHints, error)
}

// GuardrailEvaluator defines the optional policy/PII guardrail extension hook.
type GuardrailEvaluator interface {
	EvaluateGuardrail(ctx context.Context, in GuardrailInput) (GuardrailVerdict, error)
}

// ExecutionCanceler defines the cooperative cancellation hook for isolated runtimes.
type ExecutionCanceler interface {
	CancelExecution(ctx context.Context, sig CancelExecutionSignal) (CancelAck, error)
}

// ExtensionRuntime combines all 10 RPC operations of ExtensionRuntimeService in runtime.proto.
type ExtensionRuntime interface {
	ProviderRuntime
	GuardrailEvaluator
	ExecutionCanceler
}
