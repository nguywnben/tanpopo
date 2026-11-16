// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/broker"
)

type embeddingIngressRequest struct {
	Model          string
	Input          any
	EncodingFormat string
	Dimensions     *int
	Stream         bool
}

func parseStrictEmbeddingRequest(raw []byte) (*embeddingIngressRequest, error) {
	obj, err := blob.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, err
	}
	req := &embeddingIngressRequest{}
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
	if v, ok := obj["encoding_format"]; ok && v != nil {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("field 'encoding_format' must be a string")
		}
		req.EncodingFormat = s
	}
	if v, ok := obj["dimensions"]; ok && v != nil {
		num, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("field 'dimensions' must be an integer")
		}
		iv, err := num.Int64()
		if err != nil {
			return nil, fmt.Errorf("field 'dimensions' must be an integer")
		}
		dim := int(iv)
		req.Dimensions = &dim
	}
	if v, ok := obj["stream"]; ok && v != nil {
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("field 'stream' must be a boolean")
		}
		req.Stream = b
	}
	return req, nil
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	meta := ingressFromContext(r.Context())
	if r.Method != http.MethodPost {
		writeCanonicalError(w, meta.RequestID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "POST is required on /v1/embeddings.", false)
		return
	}

	streamCtx, cancelStream, match, handleID, ok := s.authenticateProviderRequest(w, r, meta.RequestID, "/v1/embeddings")
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

	reqEnvelope, err := parseStrictEmbeddingRequest(rawBody)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Malformed JSON request body.", false)
		return
	}
	if reqEnvelope.Stream {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Streaming is not supported on /v1/embeddings.", false)
		return
	}
	modelSlug := strings.TrimSpace(reqEnvelope.Model)
	if modelSlug == "" || !validateEmbeddingInput(reqEnvelope.Input) {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Both 'model' and valid non-empty 'input' (string, string array, or token array) are required.", false)
		return
	}
	encFmt := strings.ToLower(strings.TrimSpace(reqEnvelope.EncodingFormat))
	if encFmt != "" && encFmt != "float" && encFmt != "base64" {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Unsupported 'encoding_format'; expected 'float' or 'base64'.", false)
		return
	}
	if reqEnvelope.Dimensions != nil && *reqEnvelope.Dimensions <= 0 {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Field 'dimensions' must be a positive integer.", false)
		return
	}
	estTokens, estBaseIn, estMaxOut := estimateIngressTokensFromJSON(rawBody)
	preAdm, ok := s.evaluatePreAdmissionPolicy(w, r, meta.RequestID, match, modelSlug, rawBody, estTokens, estBaseIn, estMaxOut)
	if !ok {
		return
	}

	var idemClaim *idempotency.Claim
	var admLease *admissionLease
	if len(r.Header.Values("Idempotency-Key")) > 0 {
		if !s.acquirePreIdempotencyRPM(w, r, meta.RequestID, match, preAdm) {
			return
		}
		claim, replayedOrRejected := s.beginIdempotency(w, r, meta.RequestID, match.Key, "/v1/embeddings", rawBody)
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
		lease, ok := s.acquireRateLimits(w, r, meta.RequestID, match, preAdm, false)
		if !ok {
			return
		}
		admLease = lease
	}
	defer admLease.Release()

	requiredCaps := extractEmbeddingCapabilities(reqEnvelope)
	candidates, ok := s.resolveTargetsWithPolicy(w, meta.RequestID, match, preAdm, modelSlug, requiredCaps)
	if !ok {
		return
	}

	budgetSess := s.newRequestBudgetSession(match.Key, meta.RequestID, modelSlug, rawBody, preAdm, nil)
	if !budgetSess.reserveInitial(r.Context(), w, candidates) {
		return
	}
	defer budgetSess.releaseIfUnsettled()

	s.executeRoutedPreStreamFailover(w, r.Context(), streamCtx, meta.RequestID, modelSlug, candidates, func(hopCtx context.Context, _ int, cand *resolver.RankedTarget) failover.AttemptResult {
		return s.executeEmbeddingHop(w, r.Context(), hopCtx, meta, match.Key, cand.Target, rawBody, modelSlug, idemClaim, admLease, budgetSess)
	})
}

func (s *Server) executeEmbeddingHop(
	w http.ResponseWriter,
	clientCtx context.Context,
	hopCtx context.Context,
	meta ingressContext,
	key *snapshot.VirtualKeyView,
	target *snapshot.ResolvedTarget,
	rawBody []byte,
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
		false,
		s.maxReqBodyBytes,
		s.remoteMediaPolicy,
		"none",
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
		"embeddings",
	)
	if err != nil {
		return s.classifyPreparedAttemptError(w, clientCtx, hopCtx, meta.RequestID, err)
	}
	defer clear(egressReq.BodyBytes)

	return s.executeSyncEmbeddingAttempt(w, clientCtx, hopCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, target, modelSlug, idemClaim, admLease, budgetSess)
}

func (s *Server) executeSyncEmbeddingAttempt(
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
	upResp, err := s.executeBrokerSync(ctx, egressReq, constraint, target, requestedModel)
	if err != nil {
		return s.classifyBrokerAttemptError(w, clientCtx, ctx, reqID, err, true)
	}

	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		return s.makeNormalizedUpstreamAttemptError(ctx, w, reqID, rt, upResp.StatusCode, upResp.SanitizedHeaders, upResp.Body)
	}

	sanitizedHdrs := flattenHTTPHeaders(upResp.SanitizedHeaders)
	normResp, err := s.transformResponseWithStage(ctx, rt, port.TransformResponseInput{
		RequestID:        reqID,
		HTTPStatus:       int32(upResp.StatusCode),
		SanitizedHeaders: sanitizedHdrs,
		RawResponseBody:  upResp.Body,
		Modality:         "EMBEDDINGS",
	}, target)
	if err != nil {
		return s.classifyTransformResponseError(w, clientCtx, ctx, reqID, err, "Upstream provider returned an invalid or truncated embeddings payload.")
	}

	finalBody, err := finalizeEmbeddingsJSON(requestedModel, normResp)
	if err != nil {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  true,
			Outcome:    breaker.OutcomeSystemFault,
			HTTPStatus: http.StatusBadGateway,
			FlushFinal: func() {
				writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Normalized embeddings payload failed OpenAI schema validation.", false)
			},
		}
	}

	headerUsage, serr := budgetSess.settleSync(ctx, target, requestedModel, normResp.Usage, nil)
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
		s.recordSettledUsageTelemetry(ctx, requestedModel, headerUsage)
	}
	writeSettledJSONResponse(w, ctx, reqID, headerUsage, finalBody, idemClaim)
	return failover.AttemptResult{
		Succeeded:  true,
		Outcome:    breaker.OutcomeSuccess,
		HTTPStatus: http.StatusOK,
	}
}

func validateEmbeddingInput(raw any) bool {
	if raw == nil {
		return false
	}
	switch v := raw.(type) {
	case string:
		return v != ""
	case []any:
		if len(v) == 0 {
			return false
		}
		allStrings := true
		for _, elem := range v {
			s, ok := elem.(string)
			if !ok || s == "" {
				allStrings = false
				break
			}
		}
		if allStrings {
			return true
		}

		allInts := true
		for _, elem := range v {
			num, ok := elem.(json.Number)
			if !ok {
				allInts = false
				break
			}
			if _, err := num.Int64(); err != nil {
				allInts = false
				break
			}
		}
		if allInts {
			return true
		}

		for _, elem := range v {
			inner, ok := elem.([]any)
			if !ok || len(inner) == 0 {
				return false
			}
			for _, tok := range inner {
				num, ok := tok.(json.Number)
				if !ok {
					return false
				}
				if _, err := num.Int64(); err != nil {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}

func finalizeEmbeddingsJSON(requestedModel string, norm port.NormalizedInferenceResponse) ([]byte, error) {
	if !json.Valid(norm.NormalizedResponseJSON) {
		return nil, fmt.Errorf("invalid JSON response")
	}
	dec := json.NewDecoder(bytes.NewReader(norm.NormalizedResponseJSON))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	obj["object"] = "list"
	if m, ok := obj["model"].(string); !ok || strings.TrimSpace(m) == "" {
		if strings.TrimSpace(norm.UpstreamModelID) != "" {
			obj["model"] = norm.UpstreamModelID
		} else {
			obj["model"] = requestedModel
		}
	}
	dataArr, hasData := obj["data"].([]any)
	if !hasData || len(dataArr) == 0 {
		return nil, fmt.Errorf("missing embedding data array")
	}
	if norm.Usage != nil {
		isZeroUnmetered := norm.Usage.Accuracy == port.UsageAccuracyUnmeteredFlagged &&
			norm.Usage.InputTokens == 0 &&
			norm.Usage.OutputTokens == 0
		if !isZeroUnmetered {
			obj["usage"] = map[string]any{
				"prompt_tokens": norm.Usage.InputTokens,
				"total_tokens":  norm.Usage.InputTokens + norm.Usage.OutputTokens,
			}
		}
	}
	if _, hasUsage := obj["usage"]; !hasUsage {
		obj["usage"] = map[string]any{
			"prompt_tokens": 0,
			"total_tokens":  0,
		}
	}
	return json.Marshal(obj)
}
