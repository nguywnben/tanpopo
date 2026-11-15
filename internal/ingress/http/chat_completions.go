// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/extension/wasm"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

// MaxInlineBlobBytes enforces CR-05's 64 KiB boundary above which inline multimodal
// payloads are offloaded to Host buffers and passed across extension runtimes via blob_ref://.
const MaxInlineBlobBytes = blob.MaxInlineBlobBytes

type chatCompletionIngressRequest struct {
	Model                string
	Messages             []any
	Stream               bool
	CallerRequestedUsage bool
	ResponseFormat       *responseFormat
	Tools                []any
	ReasoningEffort      string
}

type responseFormat struct {
	Type       string              `json:"type"`
	Name       string              `json:"name,omitempty"`
	Strict     *bool               `json:"strict,omitempty"`
	JSONSchema *responseSchemaSpec `json:"json_schema,omitempty"`
}

type responseSchemaSpec struct {
	Name   string `json:"name"`
	Strict bool   `json:"strict"`
}

func parseStrictChatCompletionRequest(raw []byte) (*chatCompletionIngressRequest, error) {
	obj, err := blob.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, err
	}
	req := &chatCompletionIngressRequest{}
	if v, ok := obj["model"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("field 'model' must be a string")
		}
		req.Model = s
	}
	if v, ok := obj["messages"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("field 'messages' must be an array")
		}
		req.Messages = arr
	}
	if v, ok := obj["stream"]; ok && v != nil {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("field 'stream' must be a boolean")
		}
		req.Stream = b
	}
	if v, ok := obj["stream_options"]; ok && v != nil {
		soMap, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field 'stream_options' must be an object")
		}
		if inc, hasInc := soMap["include_usage"]; hasInc && inc != nil {
			b, ok := inc.(bool)
			if !ok {
				return nil, fmt.Errorf("field 'stream_options.include_usage' must be a boolean")
			}
			req.CallerRequestedUsage = b
		}
	}
	if v, ok := obj["tools"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("field 'tools' must be an array")
		}
		req.Tools = arr
	}
	if v, ok := obj["reasoning_effort"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("field 'reasoning_effort' must be a string")
		}
		req.ReasoningEffort = s
	}
	if v, ok := obj["response_format"]; ok && v != nil {
		rf, err := parseStrictResponseFormat(v)
		if err != nil {
			return nil, err
		}
		req.ResponseFormat = rf
	}
	return req, nil
}

func parseStrictResponseFormat(v any) (*responseFormat, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("response format must be an object")
	}
	rf := &responseFormat{}
	if t, ok := m["type"]; ok && t != nil {
		s, ok := t.(string)
		if !ok {
			return nil, fmt.Errorf("response format 'type' must be a string")
		}
		rf.Type = s
	}
	if n, ok := m["name"]; ok && n != nil {
		s, ok := n.(string)
		if !ok {
			return nil, fmt.Errorf("response format 'name' must be a string")
		}
		rf.Name = s
	}
	if st, ok := m["strict"]; ok && st != nil {
		b, ok := st.(bool)
		if !ok {
			return nil, fmt.Errorf("response format 'strict' must be a boolean")
		}
		rf.Strict = &b
	}
	if js, ok := m["json_schema"]; ok && js != nil {
		jsMap, ok := js.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("response format 'json_schema' must be an object")
		}
		spec := &responseSchemaSpec{}
		if jn, ok := jsMap["name"]; ok && jn != nil {
			s, ok := jn.(string)
			if !ok {
				return nil, fmt.Errorf("json_schema 'name' must be a string")
			}
			spec.Name = s
		}
		if jst, ok := jsMap["strict"]; ok && jst != nil {
			b, ok := jst.(bool)
			if !ok {
				return nil, fmt.Errorf("json_schema 'strict' must be a boolean")
			}
			spec.Strict = b
		}
		rf.JSONSchema = spec
	}
	return rf, nil
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	meta := ingressFromContext(r.Context())
	if r.Method != http.MethodPost {
		writeCanonicalError(w, meta.RequestID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "POST is required on /v1/chat/completions.", false)
		return
	}

	streamCtx, cancelStream, match, handleID, ok := s.authenticateProviderRequest(w, r, meta.RequestID, "/v1/chat/completions")
	if !ok {
		return
	}
	defer cancelStream()
	defer s.snapshots.Streams().Unregister(match.Key.ID, handleID)

	rawBody, ok := s.readBoundedBody(w, r, meta.RequestID)
	if !ok {
		return
	}
	defer clear(rawBody)

	reqEnvelope, err := parseStrictChatCompletionRequest(rawBody)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Malformed JSON request body.", false)
		return
	}
	modelSlug := strings.TrimSpace(reqEnvelope.Model)
	if modelSlug == "" || len(reqEnvelope.Messages) == 0 {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Both 'model' and non-empty 'messages' are required.", false)
		return
	}
	if !modelAllowlistPermits(match.Key.AllowedModels, modelSlug) {
		writeCanonicalError(w, meta.RequestID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", modelSlug), false)
		return
	}

	var idemClaim *idempotency.Claim
	if !reqEnvelope.Stream {
		claim, replayedOrRejected := s.beginIdempotency(w, r, meta.RequestID, match.Key, "/v1/chat/completions", rawBody)
		if replayedOrRejected {
			return
		}
		idemClaim = claim
		if idemClaim != nil {
			defer idemClaim.Abort(context.Background())
		}
	}

	target, ok := s.resolveActiveTarget(w, meta.RequestID, match.Key, modelSlug)
	if !ok {
		return
	}

	if capErr := checkM1Capabilities(target, match.Config, reqEnvelope); capErr != "" {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "capability_not_supported", "invalid_request_error", capErr, false)
		return
	}

	cachedRT, rtErr := s.getOrLoadAdapterRuntime(streamCtx, target)
	if rtErr != nil {
		s.writeRuntimeFaultError(w, meta.RequestID, rtErr)
		return
	}
	defer cachedRT.release()

	offloadRes, offloadErr := blob.OffloadEndpointMultimodalRequest(
		streamCtx,
		meta.RequestID,
		rawBody,
		target.Deployment.UpstreamModelName,
		reqEnvelope.Stream,
		s.maxReqBodyBytes,
		s.remoteMediaPolicy,
		"messages",
	)
	if offloadErr != nil {
		s.writeBlobError(w, meta.RequestID, offloadErr)
		return
	}
	defer offloadRes.Scrub()

	spec, egressReq, constraint, err := s.transformAndBuildEgress(
		streamCtx,
		meta,
		match.Key,
		target,
		cachedRT,
		offloadRes.NormalizedJSON,
		offloadRes.BlobRefs,
		offloadRes.HostBlobs,
		"chat",
	)
	if err != nil {
		s.writePreparedRequestError(w, meta.RequestID, err)
		return
	}
	defer clear(egressReq.BodyBytes)
	_ = spec

	if reqEnvelope.Stream {
		s.executeStreamingChatCompletion(w, r.Context(), streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug, !reqEnvelope.CallerRequestedUsage)
		return
	}
	s.executeSyncChatCompletion(w, r.Context(), streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug, idemClaim)
}

func (s *Server) executeSyncChatCompletion(
	w http.ResponseWriter,
	clientCtx context.Context,
	ctx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	_ *snapshot.ResolvedTarget,
	requestedModel string,
	idemClaim *idempotency.Claim,
) {
	upResp, err := s.broker.ExecuteWithHostConstraint(ctx, egressReq, constraint)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && ctx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before response dispatch.", false)
			}
			return
		}
		s.writeBrokerOrEgressError(w, reqID, err)
		return
	}

	sanitizedHdrs := flattenHTTPHeaders(upResp.SanitizedHeaders)
	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		s.writeNormalizedUpstreamError(w, ctx, reqID, rt, upResp.StatusCode, sanitizedHdrs, upResp.Body)
		return
	}

	normResp, err := rt.TransformResponse(ctx, port.TransformResponseInput{
		RequestID:        reqID,
		HTTPStatus:       int32(upResp.StatusCode),
		SanitizedHeaders: sanitizedHdrs,
		RawResponseBody:  upResp.Body,
		Modality:         "CHAT",
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && ctx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before response dispatch.", false)
			}
			return
		}
		if errors.Is(err, declarative.ErrUpstreamMalformedResponse) {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream provider returned an invalid or truncated chat completion payload.", false)
			return
		}
		s.writeRuntimeFaultError(w, reqID, err)
		return
	}

	finalBody, err := finalizeChatCompletionJSON(reqID, requestedModel, normResp, s.nowMs())
	if err != nil {
		writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Normalized chat completion payload failed OpenAI schema validation.", false)
		return
	}

	if s.usageObserver != nil && normResp.Usage != nil {
		s.usageObserver(UsageRecord{
			RequestID: reqID,
			Route:     "/v1/chat/completions",
			Model:     requestedModel,
			Stream:    false,
			Usage:     normResp.Usage,
		})
	}
	writeSettledJSONResponse(w, ctx, reqID, normResp.Usage, finalBody, idemClaim)
}

func (s *Server) executeStreamingChatCompletion(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	elideUsageChunk bool,
) {
	upCtx, cancelUp := context.WithCancel(streamCtx)
	defer cancelUp()

	// Preserve egressReq.TimeoutOverride so BlindBroker.ExecuteStreamWithHostConstraint
	// enforces FirstByteTimeoutMs (clamped by spec.TimeoutOverrideMs) as ResponseHeaderTimeout
	// while keeping total stream duration open-ended.
	upStream, err := s.broker.ExecuteStreamWithHostConstraint(upCtx, egressReq, constraint)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && streamCtx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before stream dispatch.", false)
			}
			return
		}
		s.writeBrokerOrEgressError(w, reqID, err)
		return
	}
	defer func() { _ = upStream.Body.Close() }()

	if upStream.StatusCode < 200 || upStream.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(upStream.Body, 1<<20))
		s.writeNormalizedUpstreamError(w, streamCtx, reqID, rt, upStream.StatusCode, flattenHTTPHeaders(upStream.SanitizedHeaders), errBody)
		return
	}

	var idleChunkTimeout time.Duration
	if target != nil && target.Deployment != nil && target.Deployment.IdleChunkTimeoutMs > 0 {
		idleChunkTimeout = time.Duration(target.Deployment.IdleChunkTimeoutMs) * time.Millisecond
	}

	pumpRes, pumpErr := sse.Pump(streamCtx, sse.PumpConfig{
		RequestID:           reqID,
		Upstream:            upStream.Body,
		CancelUpstream:      cancelUp,
		Runtime:             rt,
		Writer:              w,
		MaxLineBytes:        sse.DefaultMaxLineBytes,
		HeartbeatInterval:   s.heartbeatInterval,
		IdleChunkTimeout:    idleChunkTimeout,
		RequireDoneSentinel: true,
		ElideUsageChunk:     elideUsageChunk,
	})
	if s.usageObserver != nil && pumpRes.Usage != nil {
		s.usageObserver(UsageRecord{
			RequestID: reqID,
			Route:     "/v1/chat/completions",
			Model:     requestedModel,
			Stream:    true,
			Usage:     pumpRes.Usage,
		})
	}
	if pumpErr != nil && !pumpRes.StreamStarted {
		if errors.Is(pumpErr, sse.ErrLineTooLarge) {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream SSE stream exceeded maximum line byte limit.", false)
			return
		}
		if errors.Is(pumpErr, context.DeadlineExceeded) || errors.Is(pumpErr, sse.ErrIdleChunkTimeout) {
			writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream SSE stream timed out before emitting first frame.", true)
			return
		}
		if errors.Is(pumpErr, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && streamCtx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before stream dispatch.", false)
			}
			return
		}
		if errors.Is(pumpErr, port.ErrExecutionFault) {
			s.writeRuntimeFaultError(w, reqID, pumpErr)
			return
		}
		writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream stream terminated before emitting a valid SSE frame.", false)
		return
	}
}

func (s *Server) writeNormalizedUpstreamError(w http.ResponseWriter, ctx context.Context, reqID string, rt port.ProviderRuntime, upstreamStatus int, headers map[string]string, body []byte) {
	normErr, nerr := rt.NormalizeError(ctx, port.NormalizeErrorInput{
		RequestID:        reqID,
		HTTPStatus:       int32(upstreamStatus),
		SanitizedHeaders: headers,
		SanitizedBody:    body,
	})
	if nerr != nil {
		writeCanonicalError(w, reqID, mapUpstreamHTTPStatus(upstreamStatus), "upstream_error", "upstream_error", "Upstream provider request failed.", upstreamStatus == http.StatusTooManyRequests || upstreamStatus >= 500)
		return
	}
	code := sanitizeErrorToken(normErr.CanonicalCode, "upstream_error")
	errType := sanitizeErrorToken(normErr.ErrorType, "upstream_error")
	msg := sanitizePublicErrorMessage(normErr.SanitizedPublicMessage, 512)
	if msg == "" {
		msg = fmt.Sprintf("Upstream provider returned HTTP %d.", upstreamStatus)
	}
	writeCanonicalError(w, reqID, mapUpstreamHTTPStatus(upstreamStatus), code, errType, msg, normErr.RetryablePreStream)
}

func sanitizeErrorToken(raw, fallback string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 64 {
		return fallback
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return fallback
	}
	return trimmed
}

func sanitizePublicErrorMessage(raw string, maxBytes int) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(min(len(trimmed), maxBytes))
	for _, r := range trimmed {
		if r == '\r' || r == '\n' || r == '\t' {
			r = ' '
		} else if r < 0x20 || r == 0x7f {
			continue
		}
		rLen := utf8.RuneLen(r)
		if rLen < 0 {
			continue
		}
		if b.Len()+rLen > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func mapUpstreamHTTPStatus(status int) int {
	switch status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity,
		http.StatusTooManyRequests:
		return status
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func (s *Server) writeSnapshotAuthError(w http.ResponseWriter, reqID string, err error) {
	switch {
	case errors.Is(err, snapshot.ErrSnapshotNotLoaded),
		errors.Is(err, snapshot.ErrSnapshotStale),
		errors.Is(err, snapshot.ErrSnapshotWatcherStopped):
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Gateway configuration snapshot is stale or unavailable.", true)
	case errors.Is(err, snapshot.ErrKeyQuotaPaused):
		writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Virtual Key is temporarily quota-paused.", true)
	case errors.Is(err, snapshot.ErrKeySuspended),
		errors.Is(err, snapshot.ErrEnvironmentScopeDenied),
		errors.Is(err, snapshot.ErrTenantSuspended):
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key or tenant scope is inactive or denied.", false)
	default:
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key is invalid, expired, or revoked.", false)
	}
}

func (s *Server) writeRuntimeFaultError(w http.ResponseWriter, reqID string, err error) {
	var fault *wasm.ExecutionFaultError
	if errors.As(err, &fault) {
		if fault.Kind == wasm.FaultKindQuarantined {
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "extension_quarantined", "extension_error", "The backing provider adapter extension is quarantined.", false)
			return
		}
		writeCanonicalError(w, reqID, fault.HTTPStatus(), fault.CanonicalCode(), fault.ErrorType(), "Provider adapter runtime encountered an isolated execution fault.", true)
		return
	}
	writeCanonicalError(w, reqID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter runtime failed during request or response transform.", true)
}

func (s *Server) writeBrokerOrEgressError(w http.ResponseWriter, reqID string, err error) {
	var bErr *broker.Error
	if errors.As(err, &bErr) {
		status := bErr.HTTPStatus
		if status <= 0 {
			status = http.StatusBadGateway
		}
		errType := "upstream_error"
		if status == http.StatusForbidden || status == http.StatusUnprocessableEntity {
			errType = "permission_error"
		}
		writeCanonicalError(w, reqID, status, bErr.Code, errType, "Outbound provider request rejected or failed at host broker.", false)
		return
	}
	var eErr *egress.Error
	if errors.As(err, &eErr) {
		status := eErr.HTTPStatus
		if status <= 0 {
			status = http.StatusForbidden
		}
		writeCanonicalError(w, reqID, status, eErr.Code, "invalid_request_error", "Outbound target failed host SSRF/egress policy enforcement.", false)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		writeCanonicalError(w, reqID, http.StatusGatewayTimeout, "upstream_timeout", "upstream_error", "Upstream provider timed out.", true)
		return
	}
	writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_connection_failed", "upstream_error", "Failed to reach upstream provider.", true)
}

type preparedRequestErr struct {
	httpStatus int
	code       string
	errType    string
	message    string
	cause      error
}

func (e *preparedRequestErr) Error() string { return e.message }

func (s *Server) authenticateProviderRequest(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	routePath string,
) (context.Context, context.CancelFunc, snapshot.KeyMatchResult, uint64, bool) {
	// AUTH-07: Admin session cookies are strictly forbidden on /v1/* Data Plane routes.
	if hasAdminSessionCookie(r) {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Admin session cookies are prohibited on /v1/* inference endpoints; authenticate with a Virtual Key Bearer token.", false)
		return nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	rawKey, ok := extractBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Missing or malformed Bearer Virtual Key.", false)
		return nil, nil, snapshot.KeyMatchResult{}, 0, false
	}
	if !strings.HasPrefix(rawKey, "vk_tp_") {
		writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Presented credential is not a Data Plane Virtual Key.", false)
		return nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	// DEC-019: Authenticate strictly via snapshot.Manager APIs enforcing CheckServingReady()
	// and register active cancellation handle atomically before admitting any work.
	streamCtx, cancelStream := context.WithCancel(r.Context())
	targetEnvID := strings.TrimSpace(r.Header.Get("X-Tanpopo-Env-Id"))
	match, handleID, authErr := s.snapshots.RegisterAuthenticatedStream(rawKey, targetEnvID, cancelStream)
	if authErr != nil {
		cancelStream()
		s.writeSnapshotAuthError(w, reqID, authErr)
		return nil, nil, snapshot.KeyMatchResult{}, 0, false
	}

	if !planePermitsProvider(match.Key.AllowedPlanes) {
		s.snapshots.Streams().Unregister(match.Key.ID, handleID)
		cancelStream()
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key is not authorized for the PROVIDER plane.", false)
		return nil, nil, snapshot.KeyMatchResult{}, 0, false
	}
	if !routeAllowlistPermits(match.Key.AllowedRoutes, routePath) {
		s.snapshots.Streams().Unregister(match.Key.ID, handleID)
		cancelStream()
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key route allowlist denies %s.", routePath), false)
		return nil, nil, snapshot.KeyMatchResult{}, 0, false
	}
	return streamCtx, cancelStream, match, handleID, true
}

func (s *Server) readBoundedBody(w http.ResponseWriter, r *http.Request, reqID string) ([]byte, bool) {
	// Bounded JSON body read (default 32 MiB ceiling per SPEC-EXT-06).
	r.Body = http.MaxBytesReader(w, r.Body, s.maxReqBodyBytes)
	rawBody, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		clear(rawBody)
		var maxErr *http.MaxBytesError
		if errors.As(readErr, &maxErr) {
			writeCanonicalError(w, reqID, http.StatusRequestEntityTooLarge, "payload_too_large", "invalid_request_error", fmt.Sprintf("Request body exceeds %d byte limit.", s.maxReqBodyBytes), false)
			return nil, false
		}
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Failed to read request body.", false)
		return nil, false
	}
	return rawBody, true
}

func (s *Server) beginIdempotency(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	key *snapshot.VirtualKeyView,
	routePath string,
	rawBody []byte,
) (*idempotency.Claim, bool) {
	rawIdem := r.Header.Values("Idempotency-Key")
	if len(rawIdem) == 0 || s.idempotency == nil {
		return nil, false
	}
	if len(rawIdem) > 1 {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Multiple Idempotency-Key headers are prohibited.", false)
		return nil, true
	}
	idemKey, err := idempotency.ValidateKey(rawIdem[0])
	if err != nil {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Idempotency-Key must be 1..128 printable non-whitespace ASCII characters.", false)
		return nil, true
	}

	digest := idempotency.ComputePayloadSHA256(r.Method, routePath, rawBody)
	scope := idempotency.ScopeKey{
		OrgID:          key.OrgID,
		ProjectID:      key.ProjectID,
		VirtualKeyID:   key.ID,
		IdempotencyKey: idemKey,
	}
	res, berr := s.idempotency.Begin(r.Context(), scope, digest)
	if berr != nil {
		writeCanonicalError(w, reqID, http.StatusInternalServerError, "internal_error", "api_error", "Failed to claim idempotency key.", true)
		return nil, true
	}
	switch res.Outcome {
	case idempotency.OutcomeAcquired:
		return res.Claim, false
	case idempotency.OutcomeReplay:
		for k, v := range res.Replay.Headers {
			if k != "" {
				w.Header().Set(k, v)
			}
		}
		w.Header().Set("X-Tanpopo-Request-Id", reqID)
		w.Header().Set("Idempotent-Replayed", "true")
		w.WriteHeader(res.Replay.StatusCode)
		_, _ = w.Write(res.Replay.Body)
		return nil, true
	case idempotency.OutcomeInFlight:
		writeCanonicalError(w, reqID, http.StatusConflict, "request_in_progress", "conflict_error", "A request with this Idempotency-Key is currently in progress.", true)
		return nil, true
	case idempotency.OutcomeMismatch:
		writeCanonicalError(w, reqID, http.StatusUnprocessableEntity, "idempotency_key_reused_mismatch", "invalid_request_error", "Idempotency-Key was reused with a modified request payload.", false)
		return nil, true
	default:
		writeCanonicalError(w, reqID, http.StatusInternalServerError, "internal_error", "api_error", "Unexpected idempotency state.", true)
		return nil, true
	}
}

func writeSettledJSONResponse(
	w http.ResponseWriter,
	ctx context.Context,
	reqID string,
	usage *port.NormalizedUsage,
	finalBody []byte,
	idemClaim *idempotency.Claim,
) {
	replayHdrs := map[string]string{
		"Content-Type":  "application/json; charset=utf-8",
		"Cache-Control": "no-store",
	}
	if usage != nil && usage.Accuracy != "" {
		acc := formatUsageAccuracyHeader(usage.Accuracy)
		w.Header().Set("X-Tanpopo-Usage-Accuracy", acc)
		replayHdrs["X-Tanpopo-Usage-Accuracy"] = acc
	}
	if idemClaim != nil {
		baseCtx := ctx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		completeCtx, cancelComplete := context.WithTimeout(context.WithoutCancel(baseCtx), 2*time.Second)
		cerr := idemClaim.Complete(completeCtx, http.StatusOK, replayHdrs, finalBody)
		cancelComplete()
		if cerr != nil {
			idemClaim.Abort(context.Background())
		}
	}
	w.Header().Set("Content-Type", replayHdrs["Content-Type"])
	w.Header().Set("Cache-Control", replayHdrs["Cache-Control"])
	w.Header().Set("X-Tanpopo-Request-Id", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(finalBody)
}

func (s *Server) resolveActiveTarget(w http.ResponseWriter, reqID string, key *snapshot.VirtualKeyView, modelSlug string) (*snapshot.ResolvedTarget, bool) {
	snap := s.snapshots.Current()
	if snap == nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Configuration snapshot unavailable.", true)
		return nil, false
	}

	target, found := snap.ResolveDefaultDeployment(key.OrgID, key.ProjectID, key.EnvID, modelSlug)
	if !found || target == nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", fmt.Sprintf("No compliant active deployment is available for model %q.", modelSlug), true)
		return nil, false
	}
	if strings.EqualFold(target.Adapter.ExtensionState, installer.StateQuarantined) {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "extension_quarantined", "extension_error", "The backing provider adapter extension is quarantined.", false)
		return nil, false
	}
	return target, true
}

func (s *Server) writeBlobError(w http.ResponseWriter, reqID string, err error) {
	var bErr *blob.Error
	if errors.As(err, &bErr) {
		status := bErr.HTTPStatus
		if status <= 0 {
			status = http.StatusBadRequest
		}
		errType := bErr.ErrType
		if errType == "" {
			errType = "invalid_request_error"
		}
		writeCanonicalError(w, reqID, status, bErr.Code, errType, bErr.Message, false)
		return
	}
	writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Invalid multimodal request payload.", false)
}

func (s *Server) transformAndBuildEgress(
	streamCtx context.Context,
	meta ingressContext,
	key *snapshot.VirtualKeyView,
	target *snapshot.ResolvedTarget,
	cachedRT *cachedAdapterEntry,
	normReqBytes []byte,
	blobRefs []port.BlobRef,
	hostBlobs map[string][]byte,
	modality string,
) (port.UpstreamHttpSpec, broker.EgressRequest, broker.HostEgressConstraint, error) {
	connCfgJSON, err := buildAdapterConfigJSONWithModality(target.Connection, modality)
	if err != nil {
		return port.UpstreamHttpSpec{}, broker.EgressRequest{}, broker.HostEgressConstraint{}, &preparedRequestErr{
			httpStatus: http.StatusInternalServerError,
			code:       "internal_error",
			errType:    "api_error",
			message:    "Invalid connection configuration JSON.",
		}
	}

	secretRefs := make(map[string]string, len(target.Connection.SecretBindings))
	for slotName := range target.Connection.SecretBindings {
		secretRefs[slotName] = slotName
	}

	narrowPolicy := buildHostNarrowPolicy(cachedRT.perms, target.Adapter, target.Connection)
	wasmCtx := wasm.WithEgressScope(streamCtx, wasm.EgressScope{
		OrgID:        key.OrgID,
		ConnectionID: target.Connection.ID,
		ActorKind:    "virtual_key",
		ActorID:      key.ID,
		EntryPoint:   wasm.DefaultAuditEntryPoint,
		RequestID:    meta.RequestID,
		NarrowPolicy: &narrowPolicy,
	})

	spec, err := cachedRT.rt.TransformRequest(wasmCtx, port.TransformRequestInput{
		RequestID:             meta.RequestID,
		ConfigJSON:            connCfgJSON,
		NormalizedRequestJSON: normReqBytes,
		BlobRefs:              blobRefs,
		SecretSlotRefs:        secretRefs,
	})
	if err != nil {
		return port.UpstreamHttpSpec{}, broker.EgressRequest{}, broker.HostEgressConstraint{}, &preparedRequestErr{cause: err}
	}

	outboundBody, err := blob.SpliceHostBlobs(spec.BodyBytes, hostBlobs, s.maxReqBodyBytes)
	if err != nil {
		return port.UpstreamHttpSpec{}, broker.EgressRequest{}, broker.HostEgressConstraint{}, &preparedRequestErr{
			httpStatus: http.StatusBadGateway,
			code:       "extension_execution_fault",
			errType:    "extension_error",
			message:    "Failed to reconstruct outbound request payload from Host blob references.",
		}
	}

	credInjections, err := mapCredentialInjections(spec.CredentialInjections, target.Connection.SecretBindings)
	if err != nil {
		clear(outboundBody)
		return port.UpstreamHttpSpec{}, broker.EgressRequest{}, broker.HostEgressConstraint{}, &preparedRequestErr{
			httpStatus: http.StatusBadGateway,
			code:       "extension_execution_fault",
			errType:    "extension_error",
			message:    "Provider adapter requested an invalid or unauthorized credential injection.",
		}
	}

	// Merge W3C trace context & correlation headers into sanitized SafeHeaders without allowing
	// extension-returned case or whitespace variants to overwrite Gateway correlation headers.
	safeHeaders := make(map[string]string, len(spec.SafeHeaders)+2)
	for k, v := range spec.SafeHeaders {
		tk := strings.TrimSpace(k)
		if tk == "" || strings.EqualFold(tk, "X-Tanpopo-Request-Id") || strings.EqualFold(tk, "Traceparent") {
			continue
		}
		safeHeaders[tk] = v
	}
	safeHeaders["X-Tanpopo-Request-Id"] = meta.RequestID
	safeHeaders["Traceparent"] = meta.Traceparent

	firstByteTimeout := s.firstByteTimeout
	if target.Deployment.FirstByteTimeoutMs > 0 {
		firstByteTimeout = time.Duration(target.Deployment.FirstByteTimeoutMs) * time.Millisecond
	}
	timeoutOverride := firstByteTimeout
	if spec.TimeoutOverrideMs > 0 {
		ov := time.Duration(spec.TimeoutOverrideMs) * time.Millisecond
		if ov < timeoutOverride {
			timeoutOverride = ov
		}
	}

	egressReq := broker.EgressRequest{
		OrgID:                key.OrgID,
		ConnectionID:         target.Connection.ID,
		Method:               spec.Method,
		TargetURL:            spec.TargetURL,
		SafeHeaders:          safeHeaders,
		BodyBytes:            outboundBody,
		CredentialInjections: credInjections,
		TimeoutOverride:      timeoutOverride,
		ActorKind:            "virtual_key",
		ActorID:              key.ID,
		EntryPoint:           wasm.DefaultAuditEntryPoint,
		RequestID:            meta.RequestID,
	}
	constraint := broker.HostEgressConstraint{
		NarrowPolicy: &narrowPolicy,
	}
	if _, cerr := narrowPolicy.ValidateTargetURL(spec.TargetURL); cerr != nil {
		clear(outboundBody)
		return port.UpstreamHttpSpec{}, broker.EgressRequest{}, broker.HostEgressConstraint{}, &preparedRequestErr{cause: cerr}
	}
	return spec, egressReq, constraint, nil
}

func (s *Server) writePreparedRequestError(w http.ResponseWriter, reqID string, err error) {
	var pErr *preparedRequestErr
	if errors.As(err, &pErr) {
		if pErr.cause != nil {
			var bErr *broker.Error
			var eErr *egress.Error
			if errors.As(pErr.cause, &bErr) || errors.As(pErr.cause, &eErr) {
				s.writeBrokerOrEgressError(w, reqID, pErr.cause)
				return
			}
			s.writeRuntimeFaultError(w, reqID, pErr.cause)
			return
		}
		writeCanonicalError(w, reqID, pErr.httpStatus, pErr.code, pErr.errType, pErr.message, false)
		return
	}
	s.writeRuntimeFaultError(w, reqID, err)
}

func hasAdminSessionCookie(r *http.Request) bool {
	for _, c := range r.Cookies() {
		name := strings.ToLower(strings.TrimSpace(c.Name))
		switch name {
		case "tanpopo_session", "__host-tanpopo_session", "tanpopo_admin_session", "__host-tanpopo_admin_session":
			if strings.TrimSpace(c.Value) != "" {
				return true
			}
		}
	}
	return false
}

func extractBearerToken(authHdr string) (string, bool) {
	if authHdr == "" {
		return "", false
	}
	const prefix = "Bearer "
	if len(authHdr) <= len(prefix) || !strings.EqualFold(authHdr[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(authHdr[len(prefix):])
	if tok == "" || strings.ContainsAny(tok, " \t\r\n") {
		return "", false
	}
	return tok, true
}

func planePermitsProvider(allowedPlanes string) bool {
	trimmed := strings.TrimSpace(allowedPlanes)
	if trimmed == "" {
		return false
	}
	if trimmed == "*" {
		return true
	}
	for _, part := range strings.Split(trimmed, ",") {
		p := strings.ToUpper(strings.TrimSpace(part))
		if p == "*" || p == "PROVIDER" || p == "DATA" || p == "ALL" {
			return true
		}
	}
	return false
}

func routeAllowlistPermits(allowedRoutes []string, path string) bool {
	for _, r := range allowedRoutes {
		tr := strings.TrimSpace(r)
		if tr == "*" || tr == "/v1/*" || tr == path {
			return true
		}
		switch path {
		case "/v1/chat/completions":
			if tr == "chat.completions" {
				return true
			}
		case "/v1/responses":
			if tr == "responses" {
				return true
			}
		case "/v1/embeddings":
			if tr == "embeddings" {
				return true
			}
		case "/v1/models":
			if tr == "models" {
				return true
			}
		}
	}
	return false
}

func modelAllowlistPermits(allowedModels []string, model string) bool {
	for _, m := range allowedModels {
		tr := strings.TrimSpace(m)
		if tr == "*" || tr == model {
			return true
		}
	}
	return false
}

func checkResponseFormatCapabilities(supports func(string) bool, targetID string, rf *responseFormat) string {
	if rf == nil {
		return ""
	}
	typ := strings.ToLower(strings.TrimSpace(rf.Type))
	switch typ {
	case "json_schema":
		isStrict := (rf.Strict != nil && *rf.Strict) || (rf.JSONSchema != nil && rf.JSONSchema.Strict)
		if isStrict {
			if !supports("strict_json_schema") {
				return fmt.Sprintf("Deployment %q does not support strict_json_schema.", targetID)
			}
		} else if !supports("strict_json_schema") && !supports("json_object") {
			return fmt.Sprintf("Deployment %q does not support strict_json_schema.", targetID)
		}
	case "json_object":
		if !supports("json_object") {
			return fmt.Sprintf("Deployment %q does not support json_object.", targetID)
		}
	}
	return ""
}

func checkM1Capabilities(target *snapshot.ResolvedTarget, cfg *snapshot.EffectiveConfig, req *chatCompletionIngressRequest) string {
	requireTested := cfg != nil && cfg.RequireTestedCaps
	supports := func(capName string) bool {
		return snapshot.DeploymentSupportsCapability(target, requireTested, capName)
	}

	if req.Stream && !supports("streaming") {
		return fmt.Sprintf("Deployment %q does not support streaming.", target.Deployment.ID)
	}
	if len(req.Tools) > 0 && !supports("tool_calling") {
		return fmt.Sprintf("Deployment %q does not support tool_calling.", target.Deployment.ID)
	}
	if rfErr := checkResponseFormatCapabilities(supports, target.Deployment.ID, req.ResponseFormat); rfErr != "" {
		return rfErr
	}
	if strings.TrimSpace(req.ReasoningEffort) != "" && !supports("reasoning") {
		return fmt.Sprintf("Deployment %q does not support reasoning.", target.Deployment.ID)
	}

	hasVision, hasAudio, hasDoc := detectMultimodalParts(req.Messages)
	if hasVision && !supports("vision") {
		return fmt.Sprintf("Deployment %q does not support vision.", target.Deployment.ID)
	}
	if hasAudio && !supports("audio_input") {
		return fmt.Sprintf("Deployment %q does not support audio_input.", target.Deployment.ID)
	}
	if hasDoc && !supports("document_input") {
		return fmt.Sprintf("Deployment %q does not support document_input.", target.Deployment.ID)
	}
	return ""
}

func detectMultimodalParts(items []any) (vision, audio, doc bool) {
	inspectPart := func(part map[string]any) {
		if part == nil {
			return
		}
		ptype := strings.ToLower(strings.TrimSpace(stringField(part["type"])))
		if ptype == "image_url" || ptype == "input_image" || ptype == "image" || part["image_url"] != nil || part["image"] != nil {
			vision = true
		}
		if ptype == "input_audio" || ptype == "audio" || part["input_audio"] != nil || part["audio"] != nil {
			audio = true
		}
		if ptype == "file" || ptype == "input_file" || ptype == "document" || part["file"] != nil || part["input_file"] != nil || part["document"] != nil {
			doc = true
		}
	}

	for _, rawItem := range items {
		itemObj, ok := rawItem.(map[string]any)
		if !ok || itemObj == nil {
			continue
		}
		inspectPart(itemObj)
		parts, ok := itemObj["content"].([]any)
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			if part, ok := rawPart.(map[string]any); ok {
				inspectPart(part)
			}
		}
	}
	return vision, audio, doc
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}

func buildAdapterConfigJSON(conn *snapshot.ProviderConnectionView) (string, error) {
	return buildAdapterConfigJSONWithModality(conn, "")
}

func buildAdapterConfigJSONWithModality(conn *snapshot.ProviderConnectionView, modality string) (string, error) {
	merged := make(map[string]any)
	raw := strings.TrimSpace(conn.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &merged); err != nil || merged == nil {
			return "", fmt.Errorf("decode connection config_json: %w", err)
		}
	}
	merged["endpoint_base_url"] = conn.EndpointBaseURL
	if strings.TrimSpace(modality) != "" {
		merged["_tanpopo_modality"] = strings.ToLower(strings.TrimSpace(modality))
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func endpointEffectivePort(rawURL string) int {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return 0
	}
	if pStr := u.Port(); pStr != "" {
		if pNum, perr := strconv.Atoi(pStr); perr == nil && pNum >= 1 && pNum <= 65535 {
			return pNum
		}
		return 0
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return 443
	case "http":
		return 80
	default:
		return 0
	}
}

type hostAllowEntry struct {
	host    string
	port    string
	hasPort bool
}

func parseNormalizedHostAllowEntry(raw string) (hostAllowEntry, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return hostAllowEntry{}, false
	}
	if addr, err := netip.ParseAddr(strings.Trim(trimmed, "[]")); err == nil {
		return hostAllowEntry{host: strings.ToLower(addr.String())}, true
	}
	if strings.Contains(trimmed, ":") {
		if h, p, err := net.SplitHostPort(trimmed); err == nil {
			nh := strings.ToLower(strings.Trim(strings.TrimSpace(h), "[]"))
			np := strings.TrimSpace(p)
			if nh == "" || np == "" {
				return hostAllowEntry{}, false
			}
			return hostAllowEntry{host: nh, port: np, hasPort: true}, true
		}
	}
	return hostAllowEntry{host: strings.ToLower(trimmed)}, true
}

func formatHostAllowEntry(e hostAllowEntry) string {
	if e.hasPort {
		return net.JoinHostPort(e.host, e.port)
	}
	if strings.Contains(e.host, ":") {
		return "[" + e.host + "]"
	}
	return e.host
}

// intersectAllowedHosts requires non-empty approved manifest allowed_hosts (fail-closed)
// and narrows against non-empty adapter allowed_hosts using canonical (host, port) matching.
func intersectAllowedHosts(permHosts, adpHosts []string) []string {
	normalize := func(in []string) []hostAllowEntry {
		if len(in) == 0 {
			return nil
		}
		seen := make(map[string]struct{}, len(in))
		out := make([]hostAllowEntry, 0, len(in))
		for _, raw := range in {
			e, ok := parseNormalizedHostAllowEntry(raw)
			if !ok {
				continue
			}
			key := formatHostAllowEntry(e)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, e)
		}
		return out
	}

	np := normalize(permHosts)
	if len(np) == 0 {
		// Fail closed: zero approved manifest hosts means zero permitted outbound hosts.
		return nil
	}
	na := normalize(adpHosts)
	if len(na) == 0 {
		out := make([]string, len(np))
		for i, e := range np {
			out[i] = formatHostAllowEntry(e)
		}
		sort.Strings(out)
		return out
	}

	seen := make(map[string]struct{})
	var out []string
	for _, pe := range np {
		for _, ae := range na {
			if pe.host != ae.host {
				continue
			}
			var narrowed hostAllowEntry
			switch {
			case pe.hasPort && ae.hasPort:
				if pe.port != ae.port {
					continue
				}
				narrowed = pe
			case pe.hasPort:
				narrowed = pe
			case ae.hasPort:
				narrowed = ae
			default:
				narrowed = pe
			}
			k := formatHostAllowEntry(narrowed)
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func buildHostNarrowPolicy(perms installer.PermissionSet, adp *snapshot.ProviderAdapterView, conn *snapshot.ProviderConnectionView) egress.Policy {
	var adpHosts []string
	if adp != nil {
		adpHosts = adp.AllowedHosts
	}
	hosts := intersectAllowedHosts(perms.AllowedHosts, adpHosts)

	var allowedPorts []int
	var allowPriv bool
	var allowedCIDRs []string
	if conn != nil {
		if port := endpointEffectivePort(conn.EndpointBaseURL); port > 0 {
			allowedPorts = []int{port}
		}
		allowPriv = conn.AllowPrivateIP
		allowedCIDRs = append([]string(nil), conn.AllowedCIDRs...)
	}
	if adp != nil && !perms.AllowPrivateIP {
		// If extension manifest permissions did not approve private IP egress, keep allow_private_ip false.
		allowPriv = false
	}

	return egress.Policy{
		AllowedHosts:   hosts,
		AllowedPorts:   allowedPorts,
		AllowPrivateIP: allowPriv,
		AllowedCIDRs:   allowedCIDRs,
		RequireTLS:     perms.RequireTLS,
	}
}

func mapCredentialInjections(specs []port.CredentialInjectInstruction, bindings map[string]*snapshot.ConnectionSecretBindingView) ([]broker.CredentialInjection, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]broker.CredentialInjection, 0, len(specs))
	for _, s := range specs {
		if b, ok := bindings[s.SecretSlotName]; ok && b != nil && b.InjectMode == broker.InjectModeHostCryptoSign {
			if s.TargetLocation != port.CredentialTargetLocationHostCryptoSign {
				return nil, fmt.Errorf("slot %q requires HOST_CRYPTO_SIGN binding mode", s.SecretSlotName)
			}
		}
		var loc broker.TargetLocation
		switch s.TargetLocation {
		case port.CredentialTargetLocationHeader:
			loc = broker.TargetHeader
		case port.CredentialTargetLocationQueryParam:
			loc = broker.TargetQueryParam
		case port.CredentialTargetLocationHostCryptoSign:
			loc = broker.TargetHostCryptoSign
		default:
			return nil, fmt.Errorf("unsupported credential target_location %q", s.TargetLocation)
		}
		inj := broker.CredentialInjection{
			SecretSlotName: s.SecretSlotName,
			Location:       loc,
			FieldName:      s.FieldName,
			ValuePrefix:    s.ValuePrefix,
		}
		if s.CryptoSign != nil {
			inj.CryptoSign = &broker.CryptoSignSpec{
				Algorithm:                broker.SigningAlgorithm(strings.TrimPrefix(string(s.CryptoSign.Algorithm), "SIGNING_ALGORITHM_")),
				CanonicalPayload:         append([]byte(nil), s.CryptoSign.CanonicalPayload...),
				OutputEncoding:           broker.SignatureEncoding(strings.TrimPrefix(string(s.CryptoSign.OutputEncoding), "SIGNATURE_ENCODING_")),
				AWSRegion:                s.CryptoSign.AWSRegion,
				AWSService:               s.CryptoSign.AWSService,
				SigningTimestampEpochSec: s.CryptoSign.SigningTimestampEpochSec,
				SignedHeaders:            append([]string(nil), s.CryptoSign.SignedHeaders...),
			}
		}
		out = append(out, inj)
	}
	return out, nil
}

func finalizeChatCompletionJSON(reqID, requestedModel string, norm port.NormalizedInferenceResponse, nowMs int64) ([]byte, error) {
	if !json.Valid(norm.NormalizedResponseJSON) {
		return nil, fmt.Errorf("invalid JSON response")
	}
	dec := json.NewDecoder(bytes.NewReader(norm.NormalizedResponseJSON))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	if id, ok := obj["id"].(string); !ok || strings.TrimSpace(id) == "" {
		obj["id"] = reqID
	}
	obj["object"] = "chat.completion"
	if _, ok := obj["created"]; !ok {
		obj["created"] = nowMs / 1000
	}
	if m, ok := obj["model"].(string); !ok || strings.TrimSpace(m) == "" {
		if strings.TrimSpace(norm.UpstreamModelID) != "" {
			obj["model"] = norm.UpstreamModelID
		} else {
			obj["model"] = requestedModel
		}
	}
	choicesRaw, hasChoices := obj["choices"].([]any)
	if !hasChoices || len(choicesRaw) == 0 {
		return nil, fmt.Errorf("missing choices array")
	}
	if norm.Usage != nil {
		isZeroUnmetered := norm.Usage.Accuracy == port.UsageAccuracyUnmeteredFlagged &&
			norm.Usage.InputTokens == 0 &&
			norm.Usage.OutputTokens == 0 &&
			norm.Usage.CacheReadTokens == 0 &&
			norm.Usage.CacheWriteTokens == 0 &&
			norm.Usage.ReasoningTokens == 0
		if !isZeroUnmetered {
			usageMap := map[string]any{
				"prompt_tokens":     norm.Usage.InputTokens,
				"completion_tokens": norm.Usage.OutputTokens,
				"total_tokens":      norm.Usage.InputTokens + norm.Usage.OutputTokens,
			}
			if norm.Usage.CacheReadTokens > 0 {
				usageMap["cache_read_tokens"] = norm.Usage.CacheReadTokens
			}
			if norm.Usage.CacheWriteTokens > 0 {
				usageMap["cache_write_tokens"] = norm.Usage.CacheWriteTokens
			}
			if norm.Usage.ReasoningTokens > 0 {
				usageMap["reasoning_tokens"] = norm.Usage.ReasoningTokens
			}
			obj["usage"] = usageMap
		} else {
			delete(obj, "usage")
		}
	}
	return json.Marshal(obj)
}

func flattenHTTPHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vals := range h {
		if len(vals) > 0 {
			out[k] = vals[0]
		}
	}
	return out
}

func formatUsageAccuracyHeader(acc port.UsageAccuracy) string {
	switch acc {
	case port.UsageAccuracyExact:
		return "EXACT"
	case port.UsageAccuracyUnmeteredFlagged:
		return "UNMETERED_FLAGGED"
	default:
		return "ESTIMATED"
	}
}
