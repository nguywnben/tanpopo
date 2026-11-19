// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
)

type rerankDocument struct {
	ID   string
	Text string
}

type rerankIngressRequest struct {
	Model           string
	Query           string
	Documents       []rerankDocument
	TopN            *int
	ReturnDocuments bool
}

func parseStrictRerankRequest(raw []byte) (*rerankIngressRequest, error) {
	obj, err := blob.DecodeStrictJSONObject(raw)
	if err != nil {
		return nil, err
	}
	for key := range obj {
		switch key {
		case "model", "query", "documents", "top_n", "return_documents":
		default:
			return nil, fmt.Errorf("unknown field %q", key)
		}
	}
	model, modelOK := obj["model"].(string)
	query, queryOK := obj["query"].(string)
	docsRaw, docsOK := obj["documents"].([]any)
	if !modelOK || strings.TrimSpace(model) == "" || !queryOK || strings.TrimSpace(query) == "" || !docsOK || len(docsRaw) == 0 || len(docsRaw) > 1000 {
		return nil, fmt.Errorf("model, query, and 1..1000 documents are required")
	}
	req := &rerankIngressRequest{Model: strings.TrimSpace(model), Query: query, Documents: make([]rerankDocument, 0, len(docsRaw))}
	for _, rawDoc := range docsRaw {
		doc := rerankDocument{}
		switch value := rawDoc.(type) {
		case string:
			doc.Text = value
		case map[string]any:
			for key := range value {
				if key != "id" && key != "text" {
					return nil, fmt.Errorf("unknown document field %q", key)
				}
			}
			var ok bool
			doc.Text, ok = value["text"].(string)
			if !ok {
				return nil, fmt.Errorf("document text is required")
			}
			if id, present := value["id"]; present {
				doc.ID, ok = id.(string)
				if !ok {
					return nil, fmt.Errorf("document id must be a string")
				}
			}
		default:
			return nil, fmt.Errorf("document must be a string or object")
		}
		if strings.TrimSpace(doc.Text) == "" {
			return nil, fmt.Errorf("document text must not be empty")
		}
		req.Documents = append(req.Documents, doc)
	}
	if value, present := obj["top_n"]; present {
		n, ok := value.(json.Number)
		if !ok {
			return nil, fmt.Errorf("top_n must be an integer")
		}
		i64, err := n.Int64()
		if err != nil || i64 < 1 || i64 > int64(len(req.Documents)) {
			return nil, fmt.Errorf("top_n must be between 1 and the number of documents")
		}
		num := int(i64)
		req.TopN = &num
	}
	if value, present := obj["return_documents"]; present {
		var ok bool
		req.ReturnDocuments, ok = value.(bool)
		if !ok {
			return nil, fmt.Errorf("return_documents must be a boolean")
		}
	}
	return req, nil
}

func finalizeRerankJSON(requestedModel string, request *rerankIngressRequest, norm port.NormalizedInferenceResponse) ([]byte, error) {
	var payload struct {
		ID      string           `json:"id"`
		Model   string           `json:"model"`
		Usage   map[string]int64 `json:"usage,omitempty"`
		Results []struct {
			Index          int      `json:"index"`
			RelevanceScore *float64 `json:"relevance_score"`
			Document       any      `json:"document,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(norm.NormalizedResponseJSON, &payload); err != nil || payload.ID == "" || len(payload.Results) == 0 {
		return nil, fmt.Errorf("malformed normalized rerank response")
	}
	if payload.Model == "" {
		payload.Model = norm.UpstreamModelID
	}
	if payload.Model == "" {
		payload.Model = requestedModel
	}
	if norm.Usage != nil {
		payload.Usage = map[string]int64{"total_tokens": norm.Usage.InputTokens + norm.Usage.OutputTokens}
		if norm.Usage.MediaUsage != nil {
			payload.Usage["search_units"] = int64(norm.Usage.MediaUsage.RerankSearchUnits)
		}
	}
	limit := len(request.Documents)
	if request.TopN != nil && *request.TopN < limit {
		limit = *request.TopN
	}
	if len(payload.Results) > limit {
		return nil, fmt.Errorf("provider returned more results than requested")
	}
	seen := make(map[int]bool, len(payload.Results))
	for i := range payload.Results {
		result := &payload.Results[i]
		if result.Index < 0 || result.Index >= len(request.Documents) || seen[result.Index] || result.RelevanceScore == nil || math.IsNaN(*result.RelevanceScore) || math.IsInf(*result.RelevanceScore, 0) {
			return nil, fmt.Errorf("provider returned invalid rerank result")
		}
		seen[result.Index] = true
		if request.ReturnDocuments {
			result.Document = map[string]string{"text": request.Documents[result.Index].Text}
		} else {
			result.Document = nil
		}
	}
	return json.Marshal(payload)
}

func (s *Server) handleRerank(w http.ResponseWriter, r *http.Request) {
	telemetryFromContext(r.Context()).setRoutePattern("/v1/rerank")
	meta := ingressFromContext(r.Context())
	if r.Method != http.MethodPost {
		writeCanonicalError(w, meta.RequestID, http.StatusMethodNotAllowed, "method_not_allowed", "invalid_request_error", "POST is required on /v1/rerank.", false)
		return
	}
	streamCtx, cancelStream, match, handleID, ok := s.authenticateProviderRequest(w, r, meta.RequestID, "/v1/rerank")
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
	req, err := parseStrictRerankRequest(rawBody)
	if err != nil {
		writeCanonicalError(w, meta.RequestID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Malformed rerank request.", false)
		return
	}
	estTokens, estBaseIn, estMaxOut := estimateIngressTokensFromJSON(rawBody)
	preAdm, ok := s.evaluatePreAdmissionPolicy(w, r, meta.RequestID, match, req.Model, rawBody, estTokens, estBaseIn, estMaxOut)
	if !ok {
		return
	}
	var idemClaim *idempotency.Claim
	var admLease *admissionLease
	if len(r.Header.Values("Idempotency-Key")) > 0 {
		if !s.acquirePreIdempotencyRPM(w, r, meta.RequestID, match, preAdm) {
			return
		}
		claim, rejected := s.beginIdempotency(w, r, meta.RequestID, match.Key, "/v1/rerank", rawBody)
		if rejected {
			return
		}
		idemClaim = claim
		if idemClaim != nil {
			defer idemClaim.Abort(context.Background())
		}
		lease, admitted := s.acquirePostIdempotencyTPM(w, r, meta.RequestID, match, preAdm)
		if !admitted {
			return
		}
		admLease = lease
	} else {
		lease, admitted := s.acquireRateLimits(w, r, meta.RequestID, match, preAdm, false)
		if !admitted {
			return
		}
		admLease = lease
	}
	defer admLease.Release()
	candidates, ok := s.resolveTargetsWithPolicy(r.Context(), w, meta.RequestID, match, preAdm, req.Model, []string{"rerank"})
	if !ok {
		return
	}
	budgetSess := s.newRequestBudgetSession(match.Key, meta.RequestID, req.Model, rawBody, preAdm, nil)
	if !budgetSess.reserveInitial(r.Context(), w, candidates) {
		return
	}
	defer budgetSess.releaseIfUnsettled()
	s.executeRoutedPreStreamFailover(w, r.Context(), streamCtx, meta.RequestID, req.Model, candidates, func(hopCtx context.Context, _ int, cand *resolver.RankedTarget) failover.AttemptResult {
		return s.executeRerankHop(w, r.Context(), hopCtx, meta, match.Key, cand.Target, rawBody, req, idemClaim, admLease, budgetSess)
	})
}

func (s *Server) executeRerankHop(w http.ResponseWriter, clientCtx, hopCtx context.Context, meta ingressContext, key *snapshot.VirtualKeyView, target *snapshot.ResolvedTarget, rawBody []byte, req *rerankIngressRequest, idemClaim *idempotency.Claim, admLease *admissionLease, budgetSess *requestBudgetSession) failover.AttemptResult {
	if abort, ok := s.bindHopTargetAndCheckKillSwitch(hopCtx, w, meta.RequestID, key, target); !ok {
		return abort
	}
	if abort, ok := budgetSess.ensureHoldForTarget(hopCtx, w, target); !ok {
		return abort
	}
	runtime, err := s.getOrLoadAdapterRuntime(hopCtx, target)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return s.classifyBrokerAttemptError(w, clientCtx, hopCtx, meta.RequestID, err, true)
		}
		return classifyRuntimeLoadAttemptError(w, s, meta.RequestID, err)
	}
	defer runtime.release()
	offload, offloadErr := blob.OffloadEndpointMultimodalRequest(
		hopCtx, meta.RequestID, rawBody, target.Deployment.UpstreamModelName, false,
		s.maxReqBodyBytes, s.remoteMediaPolicy, "none",
	)
	if offloadErr != nil {
		return failover.AttemptResult{Succeeded: false, Retryable: false, Outcome: breaker.OutcomeCallerError, HTTPStatus: http.StatusBadRequest, FlushFinal: func() { s.writeBlobError(w, meta.RequestID, offloadErr) }}
	}
	defer offload.Scrub()
	_, egressReq, constraint, err := s.transformAndBuildEgress(hopCtx, meta, key, target, runtime, offload.NormalizedJSON, offload.BlobRefs, offload.HostBlobs, "rerank")
	if err != nil {
		return s.classifyPreparedAttemptError(w, clientCtx, hopCtx, meta.RequestID, err)
	}
	defer clear(egressReq.BodyBytes)
	response, err := s.executeBrokerSync(hopCtx, egressReq, constraint, target, req.Model)
	if err != nil {
		return s.classifyBrokerAttemptError(w, clientCtx, hopCtx, meta.RequestID, err, true)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return s.makeNormalizedUpstreamAttemptError(hopCtx, w, meta.RequestID, runtime.rt, response.StatusCode, response.SanitizedHeaders, response.Body)
	}
	normalized, err := s.transformResponseWithStage(hopCtx, runtime.rt, port.TransformResponseInput{RequestID: meta.RequestID, HTTPStatus: int32(response.StatusCode), SanitizedHeaders: flattenHTTPHeaders(response.SanitizedHeaders), RawResponseBody: response.Body, Modality: "RERANK"}, target)
	if err != nil {
		return s.classifyTransformResponseError(w, clientCtx, hopCtx, meta.RequestID, err, "Upstream provider returned an invalid rerank response.")
	}
	body, err := finalizeRerankJSON(req.Model, req, normalized)
	if err != nil {
		return failover.AttemptResult{Succeeded: false, Retryable: true, Outcome: breaker.OutcomeSystemFault, HTTPStatus: http.StatusBadGateway, FlushFinal: func() {
			writeCanonicalError(w, meta.RequestID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Normalized rerank payload failed schema validation.", false)
		}}
	}
	usage, err := budgetSess.settleSync(hopCtx, target, req.Model, normalized.Usage, nil)
	if err != nil {
		budgetSess.releaseIfUnsettled()
		return failover.AttemptResult{Succeeded: false, Retryable: false, Outcome: breaker.OutcomeCallerError, HTTPStatus: http.StatusServiceUnavailable, FlushFinal: func() {
			writeCanonicalError(w, meta.RequestID, http.StatusServiceUnavailable, "budget_ledger_unavailable", "api_error", "Budget ledger settlement failed; failing closed to prevent unbilled response delivery.", false)
		}}
	}
	if usage != nil {
		s.reconcileAdmissionUsage(admLease, usage)
		s.recordSettledUsageTelemetry(hopCtx, req.Model, usage)
		if s.usageObserver != nil {
			s.usageObserver(UsageRecord{RequestID: meta.RequestID, Route: "/v1/rerank", Model: req.Model, Stream: false, Usage: usage})
		}
	}
	writeSettledJSONResponse(w, hopCtx, meta.RequestID, usage, body, idemClaim)
	return failover.AttemptResult{Succeeded: true, Outcome: breaker.OutcomeSuccess, HTTPStatus: http.StatusOK}
}
