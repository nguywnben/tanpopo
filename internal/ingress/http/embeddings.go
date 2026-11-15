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
	"github.com/nguywnben/tanpopo/internal/extension/declarative"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/ingress/blob"
	"github.com/nguywnben/tanpopo/internal/ingress/idempotency"
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
	estTokens := estimateIngressTokensFromJSON(rawBody)
	preAdm, ok := s.evaluatePreAdmissionPolicy(w, r, meta.RequestID, match, modelSlug, rawBody, estTokens)
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

	target, ok := s.resolveActiveTargetWithPolicy(w, meta.RequestID, match, preAdm, modelSlug)
	if !ok {
		return
	}

	if capErr := checkEmbeddingCapabilities(target, match.Config, reqEnvelope); capErr != "" {
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
		false,
		s.maxReqBodyBytes,
		s.remoteMediaPolicy,
		"none",
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
		"embeddings",
	)
	if err != nil {
		s.writePreparedRequestError(w, meta.RequestID, err)
		return
	}
	defer clear(egressReq.BodyBytes)

	s.executeSyncEmbedding(w, r.Context(), streamCtx, meta.RequestID, cachedRT.rt, egressReq, constraint, modelSlug, idemClaim, admLease)
}

func (s *Server) executeSyncEmbedding(
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
		Modality:         "EMBEDDINGS",
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			if clientCtx != nil && clientCtx.Err() == nil && ctx.Err() != nil {
				writeCanonicalError(w, reqID, http.StatusUnauthorized, "invalid_virtual_key", "authentication_error", "Virtual Key was revoked or suspended before response dispatch.", false)
			}
			return
		}
		if errors.Is(err, declarative.ErrUpstreamMalformedResponse) {
			writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Upstream provider returned an invalid or truncated embeddings payload.", false)
			return
		}
		s.writeRuntimeFaultError(w, reqID, err)
		return
	}

	finalBody, err := finalizeEmbeddingsJSON(requestedModel, normResp)
	if err != nil {
		writeCanonicalError(w, reqID, http.StatusBadGateway, "upstream_malformed_response", "upstream_error", "Normalized embeddings payload failed OpenAI schema validation.", false)
		return
	}

	if normResp.Usage != nil {
		s.reconcileAdmissionUsage(admLease, normResp.Usage)
	}
	writeSettledJSONResponse(w, ctx, reqID, normResp.Usage, finalBody, idemClaim)
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

func checkEmbeddingCapabilities(target *snapshot.ResolvedTarget, cfg *snapshot.EffectiveConfig, req *embeddingIngressRequest) string {
	requireTested := cfg != nil && cfg.RequireTestedCaps
	supports := func(capName string) bool {
		return snapshot.DeploymentSupportsCapability(target, requireTested, capName)
	}

	if !supports("embeddings") {
		return fmt.Sprintf("Deployment %q does not support embeddings.", target.Deployment.ID)
	}
	if req.Dimensions != nil && !supports("custom_dimensions") && !supports("dimensions") {
		return fmt.Sprintf("Deployment %q does not support custom dimensions.", target.Deployment.ID)
	}
	return ""
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
