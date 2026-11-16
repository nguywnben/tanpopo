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
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
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
	estTokens, estBaseIn, estMaxOut := estimateIngressTokensFromJSON(rawBody)
	preAdm, ok := s.evaluatePreAdmissionPolicy(w, r, meta.RequestID, match, modelSlug, rawBody, estTokens, estBaseIn, estMaxOut)
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

	requiredCaps := extractResponsesCapabilities(reqEnvelope, inputItems)
	candidates, ok := s.resolveTargetsWithPolicy(w, meta.RequestID, match, preAdm, modelSlug, requiredCaps)
	if !ok {
		return
	}

	budgetSess := s.newRequestBudgetSession(match.Key, meta.RequestID, modelSlug, rawBody, preAdm, inputItems)
	if !budgetSess.reserveInitial(r.Context(), w, candidates) {
		return
	}
	defer budgetSess.releaseIfUnsettled()

	s.executeRoutedPreStreamFailover(w, r.Context(), streamCtx, meta.RequestID, modelSlug, candidates, func(hopCtx context.Context, _ int, cand *resolver.RankedTarget) failover.AttemptResult {
		return s.executeResponsesHop(w, r.Context(), hopCtx, meta, match.Key, cand.Target, rawBody, reqEnvelope, modelSlug, idemClaim, admLease, budgetSess)
	})
}

func (s *Server) executeResponsesHop(
	w http.ResponseWriter,
	clientCtx context.Context,
	hopCtx context.Context,
	meta ingressContext,
	key *snapshot.VirtualKeyView,
	target *snapshot.ResolvedTarget,
	rawBody []byte,
	reqEnvelope *responsesIngressRequest,
	modelSlug string,
	idemClaim *idempotency.Claim,
	admLease *admissionLease,
	budgetSess *requestBudgetSession,
) failover.AttemptResult {
	if abortRes, ok := budgetSess.ensureHoldForTarget(hopCtx, w, target); !ok {
		return abortRes
	}
	cachedRT, rtErr := s.getOrLoadAdapterRuntime(hopCtx, target)
	if rtErr != nil {
		if errors.Is(rtErr, context.Canceled) {
			return s.classifyBrokerAttemptError(w, clientCtx, hopCtx, meta.RequestID, rtErr, true)
		}
		return classifyRuntimeLoadAttemptError(w, s, meta.RequestID, rtErr)
	}
	defer cachedRT.release()

	offloadRes, offloadErr := blob.OffloadEndpointMultimodalRequest(
		hopCtx,
		meta.RequestID,
		rawBody,
		target.Deployment.UpstreamModelName,
		reqEnvelope.Stream,
		s.maxReqBodyBytes,
		s.remoteMediaPolicy,
		"input",
	)
	if offloadErr != nil {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusBadRequest,
			FlushFinal: func() {
				s.writeBlobError(w, meta.RequestID, offloadErr)
			},
		}
	}
	defer offloadRes.Scrub()

	_, egressReq, constraint, err := s.transformAndBuildEgress(
		hopCtx,
		meta,
		key,
		target,
		cachedRT,
		offloadRes.NormalizedJSON,
		offloadRes.BlobRefs,
		offloadRes.HostBlobs,
		"responses",
	)
	if err != nil {
		return s.classifyPreparedAttemptError(w, clientCtx, hopCtx, meta.RequestID, err)
	}
	defer clear(egressReq.BodyBytes)

	if reqEnvelope.Stream {
		return s.executeStreamingResponsesAttempt(w, clientCtx, hopCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug, admLease, budgetSess)
	}
	return s.executeSyncResponsesAttempt(w, clientCtx, hopCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug, idemClaim, admLease, budgetSess)
}

func (s *Server) executeSyncResponsesAttempt(
	w http.ResponseWriter,
	clientCtx context.Context,
	ctx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	idemClaim *idempotency.Claim,
	admLease *admissionLease,
	budgetSess *requestBudgetSession,
) failover.AttemptResult {
	upResp, err := s.broker.ExecuteWithHostConstraint(ctx, egressReq, constraint)
	if err != nil {
		return s.classifyBrokerAttemptError(w, clientCtx, ctx, reqID, err, true)
	}

	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		return s.makeNormalizedUpstreamAttemptError(ctx, w, reqID, rt, upResp.StatusCode, upResp.SanitizedHeaders, upResp.Body)
	}

	sanitizedHdrs := flattenHTTPHeaders(upResp.SanitizedHeaders)
	normResp, err := rt.TransformResponse(ctx, port.TransformResponseInput{
		RequestID:        reqID,
		HTTPStatus:       int32(upResp.StatusCode),
		SanitizedHeaders: sanitizedHdrs,
		RawResponseBody:  upResp.Body,
		Modality:         "RESPONSES",
	})
	if err != nil {
		return s.classifyTransformResponseError(w, clientCtx, ctx, reqID, err, "Upstream provider returned an invalid or truncated responses payload.")
	}

	finalBody, err := finalizeResponsesJSON(reqID, requestedModel, normResp, s.nowMs())
	if err != nil {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Normalized responses payload failed OpenAI schema validation.", false)
			},
		}
	}

	headerUsage, serr := budgetSess.settleSync(ctx, target, requestedModel, normResp.Usage, finalBody)
	if serr != nil {
		budgetSess.releaseIfUnsettled()
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusServiceUnavailable,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "budget_ledger_unavailable", "api_error", "Budget ledger settlement failed; failing closed to prevent unbilled response delivery.", false)
			},
		}
	}
	if headerUsage != nil {
		s.reconcileAdmissionUsage(admLease, headerUsage)
	}
	writeSettledJSONResponse(w, ctx, reqID, headerUsage, finalBody, idemClaim)
	return failover.AttemptResult{
		Succeeded:  true,
		Outcome:    breaker.OutcomeSuccess,
		HTTPStatus: http.StatusOK,
	}
}

func (s *Server) executeStreamingResponsesAttempt(
	w http.ResponseWriter,
	clientCtx context.Context,
	streamCtx context.Context,
	reqID string,
	rt port.ProviderRuntime,
	egressReq broker.EgressRequest,
	constraint broker.HostEgressConstraint,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	admLease *admissionLease,
	budgetSess *requestBudgetSession,
) failover.AttemptResult {
	upCtx, cancelUp := context.WithCancel(streamCtx)
	defer cancelUp()

	upStream, err := s.broker.ExecuteStreamWithHostConstraint(upCtx, egressReq, constraint)
	if err != nil {
		return s.classifyBrokerAttemptError(w, clientCtx, streamCtx, reqID, err, false)
	}
	defer func() { _ = upStream.Body.Close() }()

	if upStream.StatusCode < 200 || upStream.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(upStream.Body, 1<<20))
		return s.makeNormalizedUpstreamAttemptError(streamCtx, w, reqID, rt, upStream.StatusCode, upStream.SanitizedHeaders, errBody)
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
		OnStreamStarted: func() {
			budgetSess.markStreaming(streamCtx)
		},
	})
	observedUsage := pumpRes.Usage
	if pumpRes.StreamStarted {
		settledUsage, serr := budgetSess.settleStream(streamCtx, target, requestedModel, pumpRes, pumpErr)
		if serr == nil && settledUsage != nil {
			observedUsage = settledUsage
		}
	}
	if observedUsage != nil {
		s.reconcileAdmissionUsage(admLease, observedUsage)
	}
	if pumpErr != nil {
		if pumpRes.StreamStarted {
			return failover.AttemptResult{
				Succeeded:     false,
				StreamStarted: true,
				Retryable:     false,
				Outcome:       classifyMidStreamPumpOutcome(clientCtx, streamCtx, pumpRes, pumpErr),
				HTTPStatus:    upStream.StatusCode,
			}
		}
		return s.classifyPreStreamPumpAttemptError(w, clientCtx, streamCtx, reqID, pumpErr)
	}
	return failover.AttemptResult{
		Succeeded:     true,
		StreamStarted: true,
		Outcome:       breaker.OutcomeSuccess,
		HTTPStatus:    upStream.StatusCode,
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
