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
	"net/http"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/security/broker"
)

type responsesIngressRequest struct {
	Model     string
	Input     any
	Stream    bool
	Tools     []any
	Reasoning *struct {
		Effort string
	}
	Text *struct {
		Format *responseFormat
	}
}

func parseStrictResponsesRequest(raw []byte) (*responsesIngressRequest, error) {
	obj, err := blob.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, err
	}
	req := &responsesIngressRequest{}
	if v, ok := obj["model"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("field 'model' must be a string")
		}
		req.Model = s
	}
	if v, ok := obj["input"]; ok {
		req.Input = v
	}
	if v, ok := obj["stream"]; ok && v != nil {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("field 'stream' must be a boolean")
		}
		req.Stream = b
	}
	if v, ok := obj["tools"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("field 'tools' must be an array")
		}
		req.Tools = arr
	}
	if v, ok := obj["reasoning"]; ok && v != nil {
		rm, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field 'reasoning' must be an object")
		}
		req.Reasoning = &struct{ Effort string }{}
		if eff, ok := rm["effort"]; ok && eff != nil {
			s, ok := eff.(string)
			if !ok {
				return nil, fmt.Errorf("field 'reasoning.effort' must be a string")
			}
			req.Reasoning.Effort = s
		}
	}
	if v, ok := obj["text"]; ok && v != nil {
		tm, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field 'text' must be an object")
		}
		req.Text = &struct{ Format *responseFormat }{}
		if f, ok := tm["format"]; ok && f != nil {
			rf, err := parseStrictResponseFormat(f)
			if err != nil {
				return nil, err
			}
			req.Text.Format = rf
		}
	}
	return req, nil
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	meta := ingressFromContext(r.Context())
	if r.Method != http.MethodPost {
		writeCanonicalError(w, meta.RequestID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "POST is required on /v1/responses.", false)
		return
	}

	streamCtx, cancelStream, match, handleID, ok := s.authenticateProviderRequest(w, r, meta.RequestID, "/v1/responses")
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

	reqEnvelope, err := parseStrictResponsesRequest(rawBody)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Malformed JSON request body.", false)
		return
	}
	modelSlug := strings.TrimSpace(reqEnvelope.Model)
	inputItems, validInput := parseResponsesInput(reqEnvelope.Input)
	if modelSlug == "" || !validInput {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Both 'model' and valid non-empty 'input' are required.", false)
		return
	}
	estTokens := estimateIngressTokensFromJSON(rawBody)
	preAdm, ok := s.evaluatePreAdmissionPolicy(w, r, meta.RequestID, match, modelSlug, rawBody, estTokens)
	if !ok {
		return
	}

	var idemClaim *idempotency.Claim
	var admLease *admissionLease
	if !reqEnvelope.Stream && len(r.Header.Values("Idempotency-Key")) > 0 {
		if !s.acquirePreIdempotencyRPM(w, r, meta.RequestID, match, preAdm) {
			return
		}
		claim, replayedOrRejected := s.beginIdempotency(w, r, meta.RequestID, match.Key, "/v1/responses", rawBody)
		if replayedOrRejected {
			return
		}
		idemClaim = claim
		if idemClaim != nil {
			defer idemClaim.Abort(context.Background())
		}
		lease, ok := s.acquirePostIdempotencyTPM(w, r, meta.RequestID, match, preAdm)
		if !ok {
			return
		}
		admLease = lease
	} else {
		lease, ok := s.acquireRateLimits(w, r, meta.RequestID, match, preAdm, reqEnvelope.Stream)
		if !ok {
			return
		}
		admLease = lease
	}
	defer admLease.Release()

	target, ok := s.resolveActiveTargetWithPolicy(w, meta.RequestID, match, preAdm, modelSlug)
	if !ok {
		return
	}

	if capErr := checkResponsesCapabilities(target, match.Config, reqEnvelope, inputItems); capErr != "" {
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
		"input",
	)
	if offloadErr != nil {
		s.writeBlobError(w, meta.RequestID, offloadErr)
		return
	}
	defer offloadRes.Scrub()

	_, egressReq, constraint, err := s.transformAndBuildEgress(
		streamCtx,
		meta,
		match.Key,
		target,
		cachedRT,
		offloadRes.NormalizedJSON,
		offloadRes.BlobRefs,
		offloadRes.HostBlobs,
		"responses",
	)
	if err != nil {
		s.writePreparedRequestError(w, meta.RequestID, err)
		return
	}
	defer clear(egressReq.BodyBytes)

	if reqEnvelope.Stream {
		s.executeStreamingResponses(w, r.Context(), streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, admLease)
		return
	}
	s.executeSyncResponses(w, r.Context(), streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, modelSlug, idemClaim, admLease)
}

func (s *Server) executeSyncResponses(
	w http.ResponseWriter,
	clientCtx context.Context,
	ctx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	requestedModel string,
	idemClaim *idempotency.Claim,
	admLease *admissionLease,
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
		Modality:         "RESPONSES",
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && ctx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before response dispatch.", false)
			}
			return
		}
		if errors.Is(err, declarative.ErrUpstreamMalformedResponse) {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream provider returned an invalid or truncated responses payload.", false)
			return
		}
		s.writeRuntimeFaultError(w, reqID, err)
		return
	}

	finalBody, err := finalizeResponsesJSON(reqID, requestedModel, normResp, s.nowMs())
	if err != nil {
		writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Normalized responses payload failed OpenAI schema validation.", false)
		return
	}

	if normResp.Usage != nil {
		s.reconcileAdmissionUsage(admLease, normResp.Usage)
	}
	writeSettledJSONResponse(w, ctx, reqID, normResp.Usage, finalBody, idemClaim)
}

func (s *Server) executeStreamingResponses(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	admLease *admissionLease,
) {
	upCtx, cancelUp := context.WithCancel(streamCtx)
	defer cancelUp()

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
	})
	if pumpRes.Usage != nil {
		s.reconcileAdmissionUsage(admLease, pumpRes.Usage)
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

func parseResponsesInput(raw any) ([]any, bool) {
	if raw == nil {
		return nil, false
	}
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return nil, true
	case []any:
		if len(v) == 0 {
			return nil, false
		}
		for _, elem := range v {
			obj, ok := elem.(map[string]any)
			if !ok || obj == nil {
				return nil, false
			}
		}
		return v, true
	default:
		return nil, false
	}
}

func checkResponsesCapabilities(
	target *snapshot.ResolvedTarget,
	cfg *snapshot.EffectiveConfig,
	req *responsesIngressRequest,
	inputItems []any,
) string {
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
	if req.Text != nil && req.Text.Format != nil {
		if rfErr := checkResponseFormatCapabilities(supports, target.Deployment.ID, req.Text.Format); rfErr != "" {
			return rfErr
		}
	}
	if req.Reasoning != nil && strings.TrimSpace(req.Reasoning.Effort) != "" && !supports("reasoning") {
		return fmt.Sprintf("Deployment %q does not support reasoning.", target.Deployment.ID)
	}

	if len(inputItems) > 0 {
		hasVision, hasAudio, hasDoc := detectMultimodalParts(inputItems)
		if hasVision && !supports("vision") {
			return fmt.Sprintf("Deployment %q does not support vision.", target.Deployment.ID)
		}
		if hasAudio && !supports("audio_input") {
			return fmt.Sprintf("Deployment %q does not support audio_input.", target.Deployment.ID)
		}
		if hasDoc && !supports("document_input") {
			return fmt.Sprintf("Deployment %q does not support document_input.", target.Deployment.ID)
		}
	}
	return ""
}

func finalizeResponsesJSON(reqID, requestedModel string, norm port.NormalizedInferenceResponse, nowMs int64) ([]byte, error) {
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
		obj["id"] = strings.Replace(reqID, "req_", "resp_", 1)
	}
	obj["object"] = "response"
	if _, ok := obj["created_at"]; !ok {
		obj["created_at"] = nowMs / 1000
	}
	if m, ok := obj["model"].(string); !ok || strings.TrimSpace(m) == "" {
		if strings.TrimSpace(norm.UpstreamModelID) != "" {
			obj["model"] = norm.UpstreamModelID
		} else {
			obj["model"] = requestedModel
		}
	}
	if s, ok := obj["status"].(string); !ok || strings.TrimSpace(s) == "" {
		obj["status"] = "completed"
	}
	outputRaw, hasOutput := obj["output"].([]any)
	if !hasOutput || len(outputRaw) == 0 {
		return nil, fmt.Errorf("missing output array")
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
				"input_tokens":      norm.Usage.InputTokens,
				"output_tokens":     norm.Usage.OutputTokens,
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
