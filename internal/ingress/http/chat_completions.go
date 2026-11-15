// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
)

// MaxInlineBlobBytes enforces CR-05's 64 KiB boundary above which inline multimodal
// payloads are offloaded to Host buffers and passed across extension runtimes via blob_ref://.
const MaxInlineBlobBytes = 64 * 1024

type chatCompletionIngressRequest struct {
	Model           string            `json:"model"`
	Messages        []json.RawMessage `json:"messages"`
	Stream          bool              `json:"stream"`
	ResponseFormat  *responseFormat   `json:"response_format,omitempty"`
	Tools           []json.RawMessage `json:"tools,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
}

type responseFormat struct {
	Type       string              `json:"type"`
	JSONSchema *responseSchemaSpec `json:"json_schema,omitempty"`
}

type responseSchemaSpec struct {
	Name   string `json:"name"`
	Strict bool   `json:"strict"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	meta := ingressFromContext(r.Context())
	if r.Method != http.MethodPost {
		writeCanonicalError(w, meta.RequestID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "POST is required on /v1/chat/completions.", false)
		return
	}

	// AUTH-07: Admin session cookies are strictly forbidden on /v1/* Data Plane routes.
	if hasAdminSessionCookie(r) {
		writeCanonicalError(w, meta.RequestID, http.StatusUnauthorized, "invalid_credential_plane", "authentication_error", "Admin session cookies are prohibited on /v1/* inference endpoints; authenticate with a Virtual Key Bearer token.", false)
		return
	}

	rawKey, ok := extractBearerToken(r.Header.Get("Authorization"))
	if !ok {
		writeCanonicalError(w, meta.RequestID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Missing or malformed Bearer Virtual Key.", false)
		return
	}

	// DEC-019: Authenticate strictly via snapshot.Manager APIs enforcing CheckServingReady()
	// and register active cancellation handle atomically before admitting any work.
	streamCtx, cancelStream := context.WithCancel(r.Context())
	defer cancelStream()

	targetEnvID := strings.TrimSpace(r.Header.Get("X-Tanpopo-Env-Id"))
	match, handleID, authErr := s.snapshots.RegisterAuthenticatedStream(rawKey, targetEnvID, cancelStream)
	if authErr != nil {
		s.writeSnapshotAuthError(w, meta.RequestID, authErr)
		return
	}
	defer s.snapshots.Streams().Unregister(match.Key.ID, handleID)

	if !planePermitsProvider(match.Key.AllowedPlanes) {
		writeCanonicalError(w, meta.RequestID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key is not authorized for the PROVIDER plane.", false)
		return
	}
	if !routeAllowlistPermits(match.Key.AllowedRoutes, "/v1/chat/completions") {
		writeCanonicalError(w, meta.RequestID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", "Virtual Key route allowlist denies /v1/chat/completions.", false)
		return
	}

	// Bounded JSON body read (default 32 MiB ceiling per SPEC-EXT-06).
	r.Body = http.MaxBytesReader(w, r.Body, s.maxReqBodyBytes)
	rawBody, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		var maxErr *http.MaxBytesError
		if errors.As(readErr, &maxErr) {
			writeCanonicalError(w, meta.RequestID, http.StatusRequestEntityTooLarge, "payload_too_large", "invalid_request_error", fmt.Sprintf("Request body exceeds %d byte limit.", s.maxReqBodyBytes), false)
			return
		}
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Failed to read request body.", false)
		return
	}
	defer clear(rawBody)

	var reqEnvelope chatCompletionIngressRequest
	if err := json.Unmarshal(rawBody, &reqEnvelope); err != nil {
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

	snap := s.snapshots.Current()
	if snap == nil {
		writeCanonicalError(w, meta.RequestID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Configuration snapshot unavailable.", true)
		return
	}

	target, found := snap.ResolveDefaultDeployment(match.Key.OrgID, match.Key.ProjectID, match.Key.EnvID, modelSlug)
	if !found || target == nil {
		writeCanonicalError(w, meta.RequestID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", fmt.Sprintf("No compliant active deployment is available for model %q.", modelSlug), true)
		return
	}
	if strings.EqualFold(target.Adapter.ExtensionState, installer.StateQuarantined) {
		writeCanonicalError(w, meta.RequestID, http.StatusServiceUnavailable, "extension_quarantined", "extension_error", "The backing provider adapter extension is quarantined.", false)
		return
	}

	if capErr := checkM1Capabilities(target, match.Config, &reqEnvelope); capErr != "" {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "capability_not_supported", "invalid_request_error", capErr, false)
		return
	}

	cachedRT, rtErr := s.getOrLoadAdapterRuntime(streamCtx, target)
	if rtErr != nil {
		s.writeRuntimeFaultError(w, meta.RequestID, rtErr)
		return
	}
	defer cachedRT.release()

	// Build normalized payload targeting the deployment's concrete upstream_model_name,
	// offloading > 64 KiB inline multimodal payloads into Host buffers as blob_ref:// handles (CR-05).
	normReqBytes, blobRefs, hostBlobs, err := prepareRequestForTransform(meta.RequestID, rawBody, target.Deployment.UpstreamModelName, reqEnvelope.Stream)
	defer scrubHostBlobs(hostBlobs)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Invalid JSON object payload.", false)
		return
	}

	connCfgJSON, err := buildAdapterConfigJSON(target.Connection)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusInternalServerError, "internal_error", "api_error", "Invalid connection configuration JSON.", false)
		return
	}

	secretRefs := make(map[string]string, len(target.Connection.SecretBindings))
	for slotName := range target.Connection.SecretBindings {
		secretRefs[slotName] = slotName
	}

	narrowPolicy := buildHostNarrowPolicy(cachedRT.perms, target.Adapter, target.Connection)
	wasmCtx := wasm.WithEgressScope(streamCtx, wasm.EgressScope{
		OrgID:        match.Key.OrgID,
		ConnectionID: target.Connection.ID,
		ActorKind:    "virtual_key",
		ActorID:      match.Key.ID,
		EntryPoint:   "data_plane",
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
		s.writeRuntimeFaultError(w, meta.RequestID, err)
		return
	}

	outboundBody, err := spliceHostBlobs(spec.BodyBytes, hostBlobs, s.maxReqBodyBytes)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Failed to reconstruct outbound request payload from Host blob references.", false)
		return
	}
	defer clear(outboundBody)

	credInjections, err := mapCredentialInjections(spec.CredentialInjections, target.Connection.SecretBindings)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadGateway, "extension_execution_fault", "extension_error", "Provider adapter requested an invalid or unauthorized credential injection.", false)
		return
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
		OrgID:                match.Key.OrgID,
		ConnectionID:         target.Connection.ID,
		Method:               spec.Method,
		TargetURL:            spec.TargetURL,
		SafeHeaders:          safeHeaders,
		BodyBytes:            outboundBody,
		CredentialInjections: credInjections,
		TimeoutOverride:      timeoutOverride,
		ActorKind:            "virtual_key",
		ActorID:              match.Key.ID,
		EntryPoint:           "data_plane",
		RequestID:            meta.RequestID,
	}
	constraint := broker.HostEgressConstraint{
		NarrowPolicy: &narrowPolicy,
	}

	if reqEnvelope.Stream {
		s.executeStreamingChatCompletion(w, streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug)
		return
	}
	s.executeSyncChatCompletion(w, streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug)
}

func (s *Server) executeSyncChatCompletion(
	w http.ResponseWriter,
	ctx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	_ *snapshot.ResolvedTarget,
	requestedModel string,
) {
	upResp, err := s.broker.ExecuteWithHostConstraint(ctx, egressReq, constraint)
	if err != nil {
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

	if normResp.Usage != nil && normResp.Usage.Accuracy != "" {
		w.Header().Set("X-Tanpopo-Usage-Accuracy", formatUsageAccuracyHeader(normResp.Usage.Accuracy))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Tanpopo-Request-Id", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(finalBody)
}

func (s *Server) executeStreamingChatCompletion(
	w http.ResponseWriter,
	streamCtx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	_ string,
) {
	upCtx, cancelUp := context.WithCancel(streamCtx)
	defer cancelUp()

	// Preserve egressReq.TimeoutOverride so BlindBroker.ExecuteStreamWithHostConstraint
	// enforces FirstByteTimeoutMs (clamped by spec.TimeoutOverrideMs) as ResponseHeaderTimeout
	// while keeping total stream duration open-ended.
	upStream, err := s.broker.ExecuteStreamWithHostConstraint(upCtx, egressReq, constraint)
	if err != nil {
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
	})
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
	code := strings.TrimSpace(normErr.CanonicalCode)
	if code == "" {
		code = "upstream_error"
	}
	errType := strings.TrimSpace(normErr.ErrorType)
	if errType == "" {
		errType = "upstream_error"
	}
	msg := sanitizePublicErrorMessage(normErr.SanitizedPublicMessage, 512)
	if msg == "" {
		msg = fmt.Sprintf("Upstream provider returned HTTP %d.", upstreamStatus)
	}
	writeCanonicalError(w, reqID, mapUpstreamHTTPStatus(upstreamStatus), code, errType, msg, normErr.RetryablePreStream)
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
	if trimmed == "" || trimmed == "*" {
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
		if tr == "*" || tr == path || (tr == "chat.completions" && path == "/v1/chat/completions") {
			return true
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

func checkM1Capabilities(target *snapshot.ResolvedTarget, cfg *snapshot.EffectiveConfig, req *chatCompletionIngressRequest) string {
	supports := func(capName string) bool {
		val := ""
		if v, ok := target.Deployment.CapabilityOverrides[capName]; ok {
			val = v
		} else if v, ok := target.LogicalModel.BaseCapabilities[capName]; ok {
			val = v
		}
		upper := strings.ToUpper(strings.TrimSpace(val))
		if cfg != nil && cfg.RequireTestedCaps {
			return upper == "TESTED" || upper == "VERIFIED"
		}
		return upper == "DECLARED" || upper == "TESTED" || upper == "VERIFIED"
	}

	if req.Stream && !supports("streaming") {
		return fmt.Sprintf("Deployment %q does not support streaming.", target.Deployment.ID)
	}
	if len(req.Tools) > 0 && !supports("tool_calling") {
		return fmt.Sprintf("Deployment %q does not support tool_calling.", target.Deployment.ID)
	}
	if req.ResponseFormat != nil && strings.EqualFold(req.ResponseFormat.Type, "json_schema") && req.ResponseFormat.JSONSchema != nil && req.ResponseFormat.JSONSchema.Strict {
		if !supports("strict_json_schema") {
			return fmt.Sprintf("Deployment %q does not support strict_json_schema.", target.Deployment.ID)
		}
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

func detectMultimodalParts(messages []json.RawMessage) (vision, audio, doc bool) {
	for _, rawMsg := range messages {
		var msg struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(rawMsg, &msg); err != nil || len(bytes.TrimSpace(msg.Content)) == 0 {
			continue
		}
		var parts []map[string]any
		if err := json.Unmarshal(msg.Content, &parts); err != nil {
			continue
		}
		for _, part := range parts {
			if part == nil {
				continue
			}
			ptype := strings.ToLower(strings.TrimSpace(stringField(part["type"])))
			switch {
			case ptype == "image_url" || ptype == "image" || part["image_url"] != nil:
				vision = true
			case ptype == "input_audio" || part["input_audio"] != nil:
				audio = true
			case ptype == "file" || ptype == "document" || part["file"] != nil || part["document"] != nil:
				doc = true
			}
		}
	}
	return vision, audio, doc
}

func stringField(v any) string {
	s, _ := v.(string)
	return s
}

func prepareRequestForTransform(reqID string, rawBody []byte, upstreamModel string, stream bool) ([]byte, []port.BlobRef, map[string][]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(rawBody))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, nil, nil, fmt.Errorf("expected JSON object")
	}
	if strings.TrimSpace(upstreamModel) != "" {
		obj["model"] = strings.TrimSpace(upstreamModel)
	}
	if stream {
		obj["stream"] = true
	}

	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return nil, nil, nil, err
	}
	blobPrefix := fmt.Sprintf("blob_ref://%s-%s/", reqID, hex.EncodeToString(nonceBytes[:]))
	if bytes.Contains(rawBody, []byte(blobPrefix)) {
		return nil, nil, nil, fmt.Errorf("request payload contains reserved blob_ref handle prefix")
	}

	var blobRefs []port.BlobRef
	hostBlobs := make(map[string][]byte)
	nextIdx := 0

	addBlob := func(payload string, fallbackMime string) string {
		rawBytes := []byte(payload)
		sum := sha256.Sum256(rawBytes)
		uri := fmt.Sprintf("%s%d", blobPrefix, nextIdx)
		blobID := fmt.Sprintf("blob_%d", nextIdx)
		nextIdx++
		mimeType := parseDataURIMimeType(payload, fallbackMime)
		blobRefs = append(blobRefs, port.BlobRef{
			BlobID:     blobID,
			URI:        uri,
			MimeType:   mimeType,
			ByteLength: int64(len(rawBytes)),
			SHA256Hex:  hex.EncodeToString(sum[:]),
		})
		hostBlobs[uri] = rawBytes
		return uri
	}

	if msgs, ok := obj["messages"].([]any); ok {
		for _, m := range msgs {
			msgMap, ok := m.(map[string]any)
			if !ok {
				continue
			}
			parts, ok := msgMap["content"].([]any)
			if !ok {
				continue
			}
			for _, p := range parts {
				partMap, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if imgMap, ok := partMap["image_url"].(map[string]any); ok {
					if urlStr, ok := imgMap["url"].(string); ok && shouldOffloadInlineBlob(urlStr) {
						imgMap["url"] = addBlob(urlStr, "application/octet-stream")
					}
				}
				if audioMap, ok := partMap["input_audio"].(map[string]any); ok {
					if dataStr, ok := audioMap["data"].(string); ok && shouldOffloadInlineBlob(dataStr) {
						audioMap["data"] = addBlob(dataStr, "audio/wav")
					}
				}
				if fileMap, ok := partMap["file"].(map[string]any); ok {
					if dataStr, ok := fileMap["file_data"].(string); ok && shouldOffloadInlineBlob(dataStr) {
						fileMap["file_data"] = addBlob(dataStr, "application/octet-stream")
					}
				}
				if docMap, ok := partMap["document"].(map[string]any); ok {
					if dataStr, ok := docMap["data"].(string); ok && shouldOffloadInlineBlob(dataStr) {
						docMap["data"] = addBlob(dataStr, "application/pdf")
					}
				}
			}
		}
	}

	encoded, err := json.Marshal(obj)
	if err != nil {
		scrubHostBlobs(hostBlobs)
		return nil, nil, nil, err
	}
	if len(hostBlobs) == 0 {
		hostBlobs = nil
	}
	return encoded, blobRefs, hostBlobs, nil
}

func shouldOffloadInlineBlob(val string) bool {
	if len(val) <= MaxInlineBlobBytes {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(val))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return false
	}
	return true
}

func parseDataURIMimeType(val, fallback string) string {
	if !strings.HasPrefix(val, "data:") {
		return fallback
	}
	rest := val[len("data:"):]
	if idx := strings.IndexByte(rest, ','); idx >= 0 {
		meta := rest[:idx]
		if semi := strings.IndexByte(meta, ';'); semi >= 0 {
			meta = meta[:semi]
		}
		if strings.TrimSpace(meta) != "" {
			return strings.TrimSpace(meta)
		}
	}
	return fallback
}

func spliceHostBlobs(body []byte, hostBlobs map[string][]byte, maxBytes int64) ([]byte, error) {
	if len(hostBlobs) == 0 {
		return append([]byte(nil), body...), nil
	}
	out := append([]byte(nil), body...)
	for uri, rawVal := range hostBlobs {
		quotedPlaceholder, err := json.Marshal(uri)
		if err != nil {
			clear(out)
			return nil, err
		}
		count := bytes.Count(out, quotedPlaceholder)
		if count == 0 {
			continue
		}
		if count != 1 {
			clear(out)
			return nil, fmt.Errorf("duplicate or unexpected blob reference %q", uri)
		}
		quotedPayload, err := json.Marshal(string(rawVal))
		if err != nil {
			clear(out)
			return nil, err
		}
		projected := int64(len(out)) + (int64(len(quotedPayload)) - int64(len(quotedPlaceholder)))
		if maxBytes > 0 && projected > maxBytes+4096 {
			clear(quotedPayload)
			clear(out)
			return nil, fmt.Errorf("spliced payload exceeds bounded request limit")
		}
		next := bytes.Replace(out, quotedPlaceholder, quotedPayload, 1)
		clear(quotedPayload)
		clear(out)
		out = next
	}
	return out, nil
}

func scrubHostBlobs(hostBlobs map[string][]byte) {
	for k, v := range hostBlobs {
		clear(v)
		delete(hostBlobs, k)
	}
}

func buildAdapterConfigJSON(conn *snapshot.ProviderConnectionView) (string, error) {
	merged := make(map[string]any)
	raw := strings.TrimSpace(conn.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &merged); err != nil || merged == nil {
			return "", fmt.Errorf("decode connection config_json: %w", err)
		}
	}
	merged["endpoint_base_url"] = conn.EndpointBaseURL
	out, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func buildHostNarrowPolicy(perms installer.PermissionSet, adp *snapshot.ProviderAdapterView, conn *snapshot.ProviderConnectionView) egress.Policy {
	var hosts []string
	if len(perms.AllowedHosts) > 0 {
		hosts = append([]string(nil), perms.AllowedHosts...)
	} else if adp != nil && len(adp.AllowedHosts) > 0 {
		hosts = append([]string(nil), adp.AllowedHosts...)
	} else if u, err := url.Parse(conn.EndpointBaseURL); err == nil && u.Hostname() != "" {
		hosts = []string{strings.ToLower(u.Hostname())}
	}
	sort.Strings(hosts)

	var allowedPorts []int
	if u, err := url.Parse(strings.TrimSpace(conn.EndpointBaseURL)); err == nil {
		if pStr := u.Port(); pStr != "" {
			if pNum, perr := strconv.Atoi(pStr); perr == nil && pNum >= 1 && pNum <= 65535 {
				allowedPorts = []int{pNum}
			}
		} else if strings.EqualFold(u.Scheme, "https") {
			allowedPorts = []int{443}
		} else if strings.EqualFold(u.Scheme, "http") {
			allowedPorts = []int{80}
		}
	}

	allowPriv := conn.AllowPrivateIP
	if adp != nil && len(perms.AllowedHosts) > 0 && !perms.AllowPrivateIP {
		// If extension manifest permissions did not approve private IP egress, keep allow_private_ip false.
		allowPriv = false
	}

	return egress.Policy{
		AllowedHosts:   hosts,
		AllowedPorts:   allowedPorts,
		AllowPrivateIP: allowPriv,
		AllowedCIDRs:   append([]string(nil), conn.AllowedCIDRs...),
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
	var obj map[string]any
	if err := json.Unmarshal(norm.NormalizedResponseJSON, &obj); err != nil || obj == nil {
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
