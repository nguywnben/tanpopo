// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tetratelabs/wazero/api"

	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

// Compile-time verification that *Runtime satisfies the unified 10-method ExtensionRuntime contract.
var _ port.ExtensionRuntime = (*Runtime)(nil)

type envelopeRequest struct {
	Op    string `json:"op"`
	Input any    `json:"input"`
}

// BuildValidationProbe invokes the Wasm provider adapter's probe builder and verifies DEC-004 / SM-01 bounds.
func (r *Runtime) BuildValidationProbe(ctx context.Context, in port.ValidationProbeInput) (port.UpstreamHttpSpec, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		spec := r.sliceSpecFromConfig(nil, nil)
		if err := r.validateOutboundSpec(spec); err != nil {
			return port.UpstreamHttpSpec{}, err
		}
		return spec, nil
	}
	spec, err := invokeEnvelope[port.ValidationProbeInput, port.UpstreamHttpSpec](
		r, ctx, in.RequestID, "build_validation_probe", []string{"build_validation_probe", "transform"}, in, r.callTimeout,
	)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	if err := r.validateOutboundSpec(spec); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	return spec, nil
}

// VerifyValidationResult invokes Phase 2 of blind connection validation inside the Wasm sandbox.
func (r *Runtime) VerifyValidationResult(ctx context.Context, in port.ValidationResultInput) (port.ValidationVerdict, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.ValidationVerdict{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		raw, err := json.Marshal(in)
		if err != nil {
			return port.ValidationVerdict{}, fmt.Errorf("%w: marshal validation result: %v", port.ErrInvalidSpec, err)
		}
		if _, err := r.TransformRawSlice(ctx, in.RequestID, raw); err != nil {
			return port.ValidationVerdict{}, err
		}
		return port.ValidationVerdict{Valid: in.HTTPStatus >= 200 && in.HTTPStatus < 300}, nil
	}
	return invokeEnvelope[port.ValidationResultInput, port.ValidationVerdict](
		r, ctx, in.RequestID, "verify_validation_result", []string{"verify_validation_result", "transform"}, in, r.callTimeout,
	)
}

// TransformRequest invokes the Wasm adapter to build a Host-verified UpstreamHttpSpec (zero secrets in Wasm).
func (r *Runtime) TransformRequest(ctx context.Context, in port.TransformRequestInput) (port.UpstreamHttpSpec, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		body, err := r.transformSlice(ctx, in.RequestID, in.NormalizedRequestJSON, r.callTimeout, true)
		if err != nil {
			return port.UpstreamHttpSpec{}, err
		}
		spec := r.sliceSpecFromConfig(body, in.BlobRefs)
		if err := r.validateOutboundSpec(spec); err != nil {
			return port.UpstreamHttpSpec{}, err
		}
		return spec, nil
	}

	spec, err := invokeEnvelope[port.TransformRequestInput, port.UpstreamHttpSpec](
		r, ctx, in.RequestID, "transform_request", []string{"transform_request", "transform"}, in, r.callTimeout,
	)
	if err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	if err := r.validateOutboundSpec(spec); err != nil {
		return port.UpstreamHttpSpec{}, err
	}
	return spec, nil
}

// TransformResponse normalizes a synchronous upstream HTTP response inside the Wasm sandbox.
func (r *Runtime) TransformResponse(ctx context.Context, in port.TransformResponseInput) (port.NormalizedInferenceResponse, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.NormalizedInferenceResponse{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		body, err := r.transformSlice(ctx, in.RequestID, in.RawResponseBody, r.callTimeout, true)
		if err != nil {
			return port.NormalizedInferenceResponse{}, err
		}
		return port.NormalizedInferenceResponse{NormalizedResponseJSON: body}, nil
	}
	resp, err := invokeEnvelope[port.TransformResponseInput, port.NormalizedInferenceResponse](
		r, ctx, in.RequestID, "transform_response", []string{"transform_response", "transform"}, in, r.callTimeout,
	)
	if err != nil {
		return port.NormalizedInferenceResponse{}, err
	}
	if resp.Usage != nil && resp.Usage.Accuracy != "" && !resp.Usage.Accuracy.IsValid() {
		return port.NormalizedInferenceResponse{}, fmt.Errorf("%w: invalid usage accuracy %q", port.ErrInvalidSpec, resp.Usage.Accuracy)
	}
	return resp, nil
}

// TransformStreamChunk transforms one bounded upstream SSE/chunk slice inside the Wasm pool.
func (r *Runtime) TransformStreamChunk(ctx context.Context, in port.StreamChunkInput) (port.StreamChunkOutput, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.StreamChunkOutput{}, err
	}
	if len(in.SessionState) > MaxStreamSessionStateBytes {
		return port.StreamChunkOutput{}, fmt.Errorf("%w: session_state exceeds %d bytes", port.ErrExecutionFault, MaxStreamSessionStateBytes)
	}
	if r.abiMode == ABIModeSliceTransform {
		frame, err := r.transformSlice(ctx, in.RequestID, in.RawUpstreamChunk, r.streamChunkTimeout, false)
		if err != nil {
			return port.StreamChunkOutput{}, err
		}
		var frames [][]byte
		if len(frame) > 0 {
			frames = [][]byte{frame}
		}
		return port.StreamChunkOutput{
			NextSessionState:    append([]byte(nil), in.SessionState...),
			NormalizedSSEFrames: frames,
			StreamCompleted:     in.IsEOF,
		}, nil
	}

	out, err := invokeEnvelope[port.StreamChunkInput, port.StreamChunkOutput](
		r, ctx, in.RequestID, "transform_stream_chunk", []string{"transform_stream_chunk", "transform"}, in, r.streamChunkTimeout,
	)
	if err != nil {
		return port.StreamChunkOutput{}, err
	}
	if len(out.NextSessionState) > MaxStreamSessionStateBytes {
		r.recordABIViolation(ctx, "next_session_state exceeds 64 KiB cap")
		return port.StreamChunkOutput{}, &ExecutionFaultError{
			Kind:   FaultKindABIViolation,
			Reason: fmt.Sprintf("next_session_state %d bytes exceeds %d", len(out.NextSessionState), MaxStreamSessionStateBytes),
		}
	}
	if out.IncrementalUsage != nil && out.IncrementalUsage.Accuracy != "" && !out.IncrementalUsage.Accuracy.IsValid() {
		return port.StreamChunkOutput{}, fmt.Errorf("%w: invalid incremental usage accuracy %q", port.ErrInvalidSpec, out.IncrementalUsage.Accuracy)
	}
	return out, nil
}

// NormalizeError converts a sanitized upstream HTTP error into CanonicalProviderError inside Wasm.
func (r *Runtime) NormalizeError(ctx context.Context, in port.NormalizeErrorInput) (port.CanonicalProviderError, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.CanonicalProviderError{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		if _, err := r.TransformRawSlice(ctx, in.RequestID, in.SanitizedBody); err != nil {
			return port.CanonicalProviderError{}, err
		}
		retryable := in.HTTPStatus == http.StatusTooManyRequests || in.HTTPStatus >= 500
		return port.CanonicalProviderError{
			CanonicalCode:      "upstream_error",
			ErrorType:          "upstream_error",
			RetryablePreStream: retryable,
		}, nil
	}
	return invokeEnvelope[port.NormalizeErrorInput, port.CanonicalProviderError](
		r, ctx, in.RequestID, "normalize_error", []string{"normalize_error", "transform"}, in, r.callTimeout,
	)
}

// ExtractUsage extracts 5-component token and multimodal accounting counters inside Wasm.
func (r *Runtime) ExtractUsage(ctx context.Context, in port.ExtractUsageInput) (port.NormalizedUsage, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.NormalizedUsage{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		if _, err := r.TransformRawSlice(ctx, in.RequestID, in.RawResponseOrTrailerBytes); err != nil {
			return port.NormalizedUsage{}, err
		}
		return port.NormalizedUsage{Accuracy: port.UsageAccuracyEstimated}, nil
	}
	usage, err := invokeEnvelope[port.ExtractUsageInput, port.NormalizedUsage](
		r, ctx, in.RequestID, "extract_usage", []string{"extract_usage", "transform"}, in, r.callTimeout,
	)
	if err != nil {
		return port.NormalizedUsage{}, err
	}
	if usage.Accuracy != "" && !usage.Accuracy.IsValid() {
		return port.NormalizedUsage{}, fmt.Errorf("%w: invalid usage accuracy %q", port.ErrInvalidSpec, usage.Accuracy)
	}
	return usage, nil
}

// InspectNativeRequest inspects bounded /native/{dialect}/{upstreamPath} metadata inside Wasm.
func (r *Runtime) InspectNativeRequest(ctx context.Context, in port.NativeRequestInput) (port.NativeRoutingHints, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.NativeRoutingHints{}, err
	}
	if r.abiMode == ABIModeSliceTransform {
		if _, err := r.TransformRawSlice(ctx, in.RequestID, in.RawRequestBody); err != nil {
			return port.NativeRoutingHints{}, err
		}
		var injCopy []port.CredentialInjectInstruction
		if r.sliceCfg != nil {
			injCopy = append([]port.CredentialInjectInstruction(nil), r.sliceCfg.CredentialInjections...)
		}
		if err := r.validateSecretSlots(injCopy); err != nil {
			return port.NativeRoutingHints{}, err
		}
		return port.NativeRoutingHints{CredentialInjections: injCopy}, nil
	}
	hints, err := invokeEnvelope[port.NativeRequestInput, port.NativeRoutingHints](
		r, ctx, in.RequestID, "inspect_native_request", []string{"inspect_native_request", "transform"}, in, r.callTimeout,
	)
	if err != nil {
		return port.NativeRoutingHints{}, err
	}
	if err := r.validateSecretSlots(hints.CredentialInjections); err != nil {
		return port.NativeRoutingHints{}, err
	}
	return hints, nil
}

// EvaluateGuardrail executes the pre-request or post-response guardrail extension hook.
func (r *Runtime) EvaluateGuardrail(ctx context.Context, in port.GuardrailInput) (port.GuardrailVerdict, error) {
	if err := r.preflightGate(ctx); err != nil {
		return port.GuardrailVerdict{}, err
	}
	if in.Stage != "" && !in.Stage.IsValid() {
		return port.GuardrailVerdict{}, fmt.Errorf("%w: invalid guardrail stage %q", port.ErrInvalidSpec, in.Stage)
	}
	if r.abiMode == ABIModeSliceTransform {
		out, err := r.transformSlice(ctx, in.RequestID, in.PayloadJSON, r.callTimeout, true)
		if err != nil {
			return port.GuardrailVerdict{}, err
		}
		return port.GuardrailVerdict{Allowed: true, RedactedPayloadJSON: out}, nil
	}
	return invokeEnvelope[port.GuardrailInput, port.GuardrailVerdict](
		r, ctx, in.RequestID, "evaluate_guardrail", []string{"evaluate_guardrail", "transform"}, in, r.callTimeout,
	)
}

// CancelExecution immediately terminates any active Wasm execution tagged with sig.RequestID.
func (r *Runtime) CancelExecution(_ context.Context, sig port.CancelExecutionSignal) (port.CancelAck, error) {
	if r == nil || r.pool == nil {
		return port.CancelAck{}, nil
	}
	return port.CancelAck{Released: r.pool.CancelRequest(sig.RequestID)}, nil
}

// TransformRawSlice executes the raw POC-01 Linear-Memory Slice ABI `(ptr, len) -> packed u64`
// against `"transform"`, obeying kill-switch, quarantine, watchdog, cancellation, and zeroing rules.
func (r *Runtime) TransformRawSlice(ctx context.Context, reqID string, payload []byte) ([]byte, error) {
	if err := r.preflightGate(ctx); err != nil {
		return nil, err
	}
	return r.execWithFaultGuard(ctx, reqID, []string{"transform"}, payload, r.callTimeout, false)
}

func (r *Runtime) preflightGate(ctx context.Context) error {
	if r == nil || r.pool == nil || r.pool.closed.Load() {
		return &ExecutionFaultError{Kind: FaultKindClosed, Reason: "wasm runtime closed", Cause: ErrWasmRuntimeClosed}
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if !r.isEnabled() {
		return ErrWasmRuntimeDisabled
	}
	if r.faultGuard != nil && r.installationID != "" && r.faultGuard.IsQuarantined(r.installationID) {
		return &ExecutionFaultError{Kind: FaultKindQuarantined, Reason: "extension installation is quarantined"}
	}
	return nil
}

func (r *Runtime) transformSlice(ctx context.Context, reqID string, payload []byte, timeout time.Duration, expectUTF8 bool) ([]byte, error) {
	if err := r.preflightGate(ctx); err != nil {
		return nil, err
	}
	return r.execWithFaultGuard(ctx, reqID, []string{"transform"}, payload, timeout, expectUTF8)
}

func (r *Runtime) sliceSpecFromConfig(body []byte, blobs []port.BlobRef) port.UpstreamHttpSpec {
	var injCopy []port.CredentialInjectInstruction
	if len(r.sliceCfg.CredentialInjections) > 0 {
		injCopy = append([]port.CredentialInjectInstruction(nil), r.sliceCfg.CredentialInjections...)
	}
	return port.UpstreamHttpSpec{
		Method:               r.sliceCfg.Method,
		TargetURL:            r.sliceCfg.TargetURL,
		SafeHeaders:          cloneStringMap(r.sliceCfg.SafeHeaders),
		BodyBytes:            body,
		OutboundBlobSplices:  append([]port.BlobRef(nil), blobs...),
		CredentialInjections: injCopy,
	}
}

func invokeEnvelope[In any, Out any](
	r *Runtime,
	ctx context.Context,
	reqID string,
	opName string,
	fnCandidates []string,
	in In,
	timeout time.Duration,
) (Out, error) {
	var zero Out
	if err := r.preflightGate(ctx); err != nil {
		return zero, err
	}

	var payload []byte
	var err error
	if r.hasDedicatedExport(fnCandidates[0]) {
		payload, err = json.Marshal(in)
	} else {
		payload, err = json.Marshal(envelopeRequest{Op: opName, Input: in})
	}
	if err != nil {
		return zero, fmt.Errorf("%w: marshal %s input: %v", port.ErrInvalidSpec, opName, err)
	}

	rawOut, err := r.execWithFaultGuard(ctx, reqID, fnCandidates, payload, timeout, true)
	if err != nil {
		return zero, err
	}

	var out Out
	if err := json.Unmarshal(rawOut, &out); err != nil {
		r.recordABIViolation(ctx, fmt.Sprintf("%s returned malformed JSON: %v", opName, err))
		return zero, &ExecutionFaultError{
			Kind:   FaultKindABIViolation,
			Reason: fmt.Sprintf("guest %s returned invalid JSON", opName),
			Cause:  err,
		}
	}
	return out, nil
}

func (r *Runtime) hasDedicatedExport(name string) bool {
	if r == nil || r.compiled == nil {
		return false
	}
	def, ok := r.compiled.ExportedFunctions()[name]
	return ok && isValidSliceABISig(def)
}

func (r *Runtime) execWithFaultGuard(
	ctx context.Context,
	reqID string,
	fnCandidates []string,
	payload []byte,
	timeout time.Duration,
	expectUTF8 bool,
) ([]byte, error) {
	out, err := r.pool.exec(ctx, reqID, fnCandidates, payload, timeout, expectUTF8)
	if err != nil {
		var fe *ExecutionFaultError
		if errors.As(err, &fe) && fe.Kind != FaultKindQuarantined && fe.Kind != FaultKindClosed && r.faultGuard != nil && r.installationID != "" {
			r.faultGuard.RecordFault(ctx, r.installationID, fe.Kind, fe.Reason)
		}
		return nil, err
	}
	return out, nil
}

func (r *Runtime) recordABIViolation(ctx context.Context, reason string) {
	if r.faultGuard != nil && r.installationID != "" {
		r.faultGuard.RecordFault(ctx, r.installationID, FaultKindABIViolation, reason)
	}
}

type blindEgressGuestRequest struct {
	Method               string                             `json:"method"`
	TargetURL            string                             `json:"target_url"`
	SafeHeaders          map[string]string                  `json:"safe_headers,omitempty"`
	BodyBytes            []byte                             `json:"body_bytes,omitempty"`
	CredentialInjections []port.CredentialInjectInstruction `json:"credential_injections,omitempty"`
	TimeoutOverrideMs    int32                              `json:"timeout_override_ms,omitempty"`
}

type blindEgressGuestResponse struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       []byte            `json:"body,omitempty"`
}

// hostBlindHTTPEgress implements `"tanpopo_host_v1"."blind_http_egress"(reqPtr, reqLen, respPtr, respCap) -> i32`.
// All socket egress, secret unsealing, Cookie bans, and SSRF enforcement stay strictly inside Host BlindBroker.
func (r *Runtime) hostBlindHTTPEgress(ctx context.Context, mod api.Module, stack []uint64) {
	reqPtr := uint32(stack[0])
	reqLen := uint32(stack[1])
	respPtr := uint32(stack[2])
	respCap := uint32(stack[3])

	setRet := func(code int32) {
		stack[0] = uint64(uint32(code))
	}

	if r == nil || r.pool == nil || r.pool.closed.Load() || !r.isEnabled() || !r.allowHostBlindEgress || r.blindBroker == nil {
		setRet(-2)
		return
	}
	if r.faultGuard != nil && r.installationID != "" && r.faultGuard.IsQuarantined(r.installationID) {
		setRet(-2)
		return
	}
	mem := mod.Memory()
	if mem == nil {
		setRet(-1)
		return
	}
	memSize := mem.Size()

	reqView, ok := readBoundedRange(mem, memSize, reqPtr, reqLen, MaxGuestPayloadBytes)
	if !ok {
		setRet(-1)
		return
	}
	if !checkWriteRange(memSize, respPtr, respCap) {
		setRet(-1)
		return
	}
	reqCopy := append([]byte(nil), reqView...)

	scope, hasScope := egressScopeFromContext(ctx)
	if !hasScope && r.defaultScope != nil {
		scope = *r.defaultScope
		hasScope = true
	}
	if !hasScope || strings.TrimSpace(scope.OrgID) == "" || strings.TrimSpace(scope.ConnectionID) == "" {
		setRet(-2)
		return
	}

	var guestReq blindEgressGuestRequest
	if err := json.Unmarshal(reqCopy, &guestReq); err != nil {
		setRet(-2)
		return
	}
	spec := port.UpstreamHttpSpec{
		Method:               guestReq.Method,
		TargetURL:            guestReq.TargetURL,
		SafeHeaders:          guestReq.SafeHeaders,
		BodyBytes:            guestReq.BodyBytes,
		CredentialInjections: guestReq.CredentialInjections,
		TimeoutOverrideMs:    guestReq.TimeoutOverrideMs,
	}
	if err := r.validateOutboundSpec(spec); err != nil {
		setRet(-2)
		return
	}

	injList, err := toBrokerInjections(guestReq.CredentialInjections)
	if err != nil {
		setRet(-2)
		return
	}

	if scope.NarrowPolicy != nil {
		if _, err := scope.NarrowPolicy.ValidateTargetURL(spec.TargetURL); err != nil {
			setRet(-3)
			return
		}
	}
	narrowPolicy, err := intersectEgressPolicies(r.narrowPolicy, scope.NarrowPolicy)
	if err != nil {
		setRet(-3)
		return
	}
	brokerResp, err := r.blindBroker.ExecuteWithHostConstraint(ctx, broker.EgressRequest{
		OrgID:                scope.OrgID,
		ConnectionID:         scope.ConnectionID,
		Method:               guestReq.Method,
		TargetURL:            guestReq.TargetURL,
		SafeHeaders:          guestReq.SafeHeaders,
		BodyBytes:            guestReq.BodyBytes,
		CredentialInjections: injList,
		TimeoutOverride:      time.Duration(guestReq.TimeoutOverrideMs) * time.Millisecond,
		ActorKind:            scope.ActorKind,
		ActorID:              scope.ActorID,
		EntryPoint:           scope.EntryPoint,
		RequestID:            scope.RequestID,
	}, broker.HostEgressConstraint{NarrowPolicy: narrowPolicy})
	if err != nil {
		setRet(-3)
		return
	}

	flatHeaders := make(map[string]string, len(brokerResp.SanitizedHeaders))
	for k, vals := range brokerResp.SanitizedHeaders {
		if len(vals) > 0 {
			flatHeaders[k] = vals[0]
		}
	}
	respBytes, err := json.Marshal(blindEgressGuestResponse{
		StatusCode: brokerResp.StatusCode,
		Headers:    flatHeaders,
		Body:       brokerResp.Body,
	})
	if err != nil {
		setRet(-3)
		return
	}
	if uint64(len(respBytes)) > uint64(respCap) {
		setRet(-4)
		return
	}
	if len(respBytes) > 0 && !mem.Write(respPtr, respBytes) {
		setRet(-1)
		return
	}
	setRet(int32(len(respBytes)))
}

func readBoundedRange(mem api.Memory, memSize, ptr, length, maxBytes uint32) ([]byte, bool) {
	if int32(ptr) < 0 || int32(length) < 0 || length > maxBytes {
		return nil, false
	}
	if uint64(ptr)+uint64(length) > uint64(memSize) {
		return nil, false
	}
	if length == 0 {
		return nil, true
	}
	return mem.Read(ptr, length)
}

func checkWriteRange(memSize, ptr, capacity uint32) bool {
	if int32(ptr) < 0 || int32(capacity) < 0 {
		return false
	}
	return uint64(ptr)+uint64(capacity) <= uint64(memSize)
}

func toBrokerInjections(items []port.CredentialInjectInstruction) ([]broker.CredentialInjection, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]broker.CredentialInjection, 0, len(items))
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return nil, err
		}
		var loc broker.TargetLocation
		switch item.TargetLocation {
		case port.CredentialTargetLocationHeader:
			loc = broker.TargetHeader
		case port.CredentialTargetLocationQueryParam:
			loc = broker.TargetQueryParam
		case port.CredentialTargetLocationHostCryptoSign:
			loc = broker.TargetHostCryptoSign
		default:
			return nil, fmt.Errorf("%w: unsupported credential target location %q", port.ErrInvalidSpec, item.TargetLocation)
		}
		var cryptoSpec *broker.CryptoSignSpec
		if item.CryptoSign != nil {
			cryptoSpec = &broker.CryptoSignSpec{
				Algorithm:                toBrokerSigningAlg(item.CryptoSign.Algorithm),
				CanonicalPayload:         append([]byte(nil), item.CryptoSign.CanonicalPayload...),
				OutputEncoding:           toBrokerEncoding(item.CryptoSign.OutputEncoding),
				AWSRegion:                item.CryptoSign.AWSRegion,
				AWSService:               item.CryptoSign.AWSService,
				SigningTimestampEpochSec: item.CryptoSign.SigningTimestampEpochSec,
				SignedHeaders:            append([]string(nil), item.CryptoSign.SignedHeaders...),
			}
		}
		out = append(out, broker.CredentialInjection{
			SecretSlotName: item.SecretSlotName,
			Location:       loc,
			FieldName:      item.FieldName,
			ValuePrefix:    item.ValuePrefix,
			CryptoSign:     cryptoSpec,
		})
	}
	return out, nil
}

func toBrokerSigningAlg(alg port.SigningAlgorithm) broker.SigningAlgorithm {
	switch alg {
	case port.SigningAlgorithmHMACSHA256:
		return broker.SigningHMACSHA256
	case port.SigningAlgorithmAWSSigV4:
		return broker.SigningAWSSigV4
	case port.SigningAlgorithmRSASHA256:
		return broker.SigningRSASHA256
	default:
		return broker.SigningAlgorithm(alg)
	}
}

func toBrokerEncoding(enc port.SignatureEncoding) broker.SignatureEncoding {
	switch enc {
	case port.SignatureEncodingHexLower:
		return broker.EncodingHexLower
	case port.SignatureEncodingBase64:
		return broker.EncodingBase64
	case port.SignatureEncodingBase64URL:
		return broker.EncodingBase64URL
	default:
		return broker.SignatureEncoding(enc)
	}
}

func intersectEgressPolicies(a, b *egress.Policy) (*egress.Policy, error) {
	if a == nil && b == nil {
		return nil, nil
	}
	if a == nil {
		if _, err := egress.NewDialer(egress.DialerConfig{Policy: *b}); err != nil {
			return nil, err
		}
		return cloneEgressPolicy(b), nil
	}
	if b == nil {
		return cloneEgressPolicy(a), nil
	}
	if _, err := egress.NewDialer(egress.DialerConfig{Policy: *b}); err != nil {
		return nil, err
	}

	aMap, err := wasmPolicyHostPortMap(*a)
	if err != nil {
		return nil, err
	}
	bMap, err := wasmPolicyHostPortMap(*b)
	if err != nil {
		return nil, err
	}

	for host, curPorts := range aMap {
		otherPorts, exists := bMap[host]
		if !exists {
			delete(aMap, host)
			continue
		}
		for pt := range curPorts {
			if !otherPorts[pt] {
				delete(curPorts, pt)
			}
		}
		if len(curPorts) == 0 {
			delete(aMap, host)
		}
	}
	if len(aMap) == 0 {
		return nil, egress.ErrHostDenied
	}

	hosts := make([]string, 0, len(aMap))
	for h := range aMap {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	var commonHosts []string
	portSet := make(map[int]bool)
	for _, h := range hosts {
		ports := aMap[h]
		pList := make([]int, 0, len(ports))
		for pt := range ports {
			pList = append(pList, pt)
			portSet[pt] = true
		}
		sort.Ints(pList)
		for _, pt := range pList {
			commonHosts = append(commonHosts, net.JoinHostPort(h, strconv.Itoa(pt)))
		}
	}
	commonPorts := make([]int, 0, len(portSet))
	for pt := range portSet {
		commonPorts = append(commonPorts, pt)
	}
	sort.Ints(commonPorts)

	allowPrivate := a.AllowPrivateIP && b.AllowPrivateIP
	var commonCIDRs []string
	if allowPrivate {
		var cerr error
		commonCIDRs, cerr = intersectPrefixStrings(a.AllowedCIDRs, b.AllowedCIDRs)
		if cerr != nil {
			return nil, cerr
		}
		if len(commonCIDRs) == 0 {
			return nil, egress.ErrPrivateIPBlocked
		}
	}

	sensSeen := make(map[string]bool)
	var sensHeaders []string
	for _, h := range append(slices.Clone(a.SensitiveHeaders), b.SensitiveHeaders...) {
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			continue
		}
		lk := strings.ToLower(trimmed)
		if !sensSeen[lk] {
			sensSeen[lk] = true
			sensHeaders = append(sensHeaders, trimmed)
		}
	}

	merged := egress.Policy{
		AllowedHosts:     commonHosts,
		AllowPrivateIP:   allowPrivate,
		AllowedCIDRs:     commonCIDRs,
		AllowedPorts:     commonPorts,
		RequireTLS:       a.RequireTLS || b.RequireTLS,
		SensitiveHeaders: sensHeaders,
	}
	if _, err := egress.NewDialer(egress.DialerConfig{Policy: merged}); err != nil {
		return nil, err
	}
	return &merged, nil
}

func wasmPolicyHostPortMap(p egress.Policy) (map[string]map[int]bool, error) {
	explicitPorts := make(map[int]bool, len(p.AllowedPorts))
	for _, pt := range p.AllowedPorts {
		if pt < 1 || pt > 65535 {
			return nil, fmt.Errorf("%w: invalid allowed port %d", egress.ErrInvalidPolicy, pt)
		}
		explicitPorts[pt] = true
	}

	out := make(map[string]map[int]bool, len(p.AllowedHosts))
	for _, rawHost := range p.AllowedHosts {
		trimmed := strings.TrimSpace(rawHost)
		if trimmed == "" {
			continue
		}
		var hostPart string
		var explicitPort int
		hasPort := false
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			hostPart = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
		} else if h, pStr, err := net.SplitHostPort(trimmed); err == nil {
			n, perr := strconv.Atoi(pStr)
			if perr != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("%w: invalid port in allowed_hosts %q", egress.ErrInvalidPolicy, rawHost)
			}
			hostPart = h
			explicitPort = n
			hasPort = true
		} else {
			hostPart = trimmed
		}

		if strings.HasPrefix(hostPart, "[") && strings.HasSuffix(hostPart, "]") {
			hostPart = hostPart[1 : len(hostPart)-1]
		}
		var canon string
		if ip, err := netip.ParseAddr(hostPart); err == nil {
			if ip.Zone() != "" {
				return nil, fmt.Errorf("%w: zone-scoped IPv6 hosts are forbidden", egress.ErrHostDenied)
			}
			canon = ip.String()
		} else {
			canon = strings.ToLower(hostPart)
		}
		if canon == "" || strings.HasSuffix(canon, ".") {
			return nil, fmt.Errorf("%w: invalid allowed_hosts entry %q", egress.ErrInvalidPolicy, rawHost)
		}
		ports, ok := out[canon]
		if !ok {
			ports = make(map[int]bool)
			out[canon] = ports
		}
		if hasPort {
			if len(explicitPorts) == 0 || explicitPorts[explicitPort] {
				ports[explicitPort] = true
			}
			continue
		}
		if len(explicitPorts) > 0 {
			for pt := range explicitPorts {
				ports[pt] = true
			}
		} else if p.RequireTLS {
			ports[443] = true
		} else {
			ports[80] = true
			ports[443] = true
		}
	}
	return out, nil
}

func intersectPrefixStrings(aList, bList []string) ([]string, error) {
	if len(aList) == 0 || len(bList) == 0 {
		return nil, nil
	}
	parseList := func(raw []string) ([]netip.Prefix, error) {
		prefixes := make([]netip.Prefix, 0, len(raw))
		for _, s := range raw {
			p, err := netip.ParsePrefix(strings.TrimSpace(s))
			if err != nil {
				return nil, fmt.Errorf("%w: invalid CIDR %q: %v", egress.ErrInvalidPolicy, s, err)
			}
			p = p.Masked()
			if p.Bits() == 0 {
				return nil, fmt.Errorf("%w: wildcard CIDR %q forbidden", egress.ErrInvalidPolicy, s)
			}
			prefixes = append(prefixes, p)
		}
		return prefixes, nil
	}
	aPrefixes, err := parseList(aList)
	if err != nil {
		return nil, err
	}
	bPrefixes, err := parseList(bList)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(p netip.Prefix) {
		s := p.String()
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, pa := range aPrefixes {
		for _, pb := range bPrefixes {
			if !pa.Overlaps(pb) {
				continue
			}
			if pb.Bits() >= pa.Bits() {
				add(pb)
			} else {
				add(pa)
			}
		}
	}
	return out, nil
}
