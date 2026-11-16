// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
)

type preAdmissionDecision struct {
	snap            *snapshot.ConfigSnapshot
	effPolicy       policyengine.EffectivePolicy
	estTokens       int64
	estBaseIn       int64
	estMaxOut       int64
	completionCount int64
	reqRegion       string
}

type admissionLease struct {
	streamLease *ratelimit.StreamLease
	snap        *snapshot.ConfigSnapshot
	scope       ratelimit.LimitScope
	limits      ratelimit.Limits
	effPolicy   policyengine.EffectivePolicy
	estTokens   int64
	reqRegion   string
}

func (a *admissionLease) Release() {
	if a != nil && a.streamLease != nil {
		a.streamLease.Release()
	}
}

func resolveRequestedDataRegion(r *http.Request) (string, bool) {
	if r == nil {
		return "", true
	}
	var resolved string
	for _, hdr := range r.Header.Values("X-Tanpopo-Data-Region") {
		for _, part := range strings.Split(hdr, ",") {
			tok := strings.ToUpper(strings.TrimSpace(part))
			if tok == "" {
				continue
			}
			if resolved != "" && resolved != tok {
				return "", false
			}
			resolved = tok
		}
	}
	return resolved, true
}

func (s *Server) evaluatePreAdmissionPolicy(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	modelSlug string,
	rawBody []byte,
	estTokens int64,
	estBaseIn int64,
	estMaxOut int64,
) (*preAdmissionDecision, bool) {
	snap := s.snapshots.Current()
	eff, pErr := s.policyEval.ResolveEffective(snap, match.Config, match.Key)
	if pErr != nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, policyengine.DenialCodeEngineUnavailable, "api_error", "Policy engine snapshot is corrupted or unavailable; failing closed.", true)
		return nil, false
	}

	if !modelAllowlistPermits(match.Key.AllowedModels, modelSlug) {
		writeCanonicalError(w, reqID, http.StatusForbidden, "scope_or_plane_denied", "permission_error", fmt.Sprintf("Virtual Key model allowlist denies %q.", modelSlug), false)
		return nil, false
	}

	reqRegion, regionOK := resolveRequestedDataRegion(r)
	if !regionOK {
		writeCanonicalError(w, reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Conflicting X-Tanpopo-Data-Region header values.", false)
		return nil, false
	}
	dec := policyengine.EvaluateEffective(eff, policyengine.EvalRequest{
		OrgID:                match.Key.OrgID,
		ProjectID:            match.Key.ProjectID,
		EnvID:                match.Key.EnvID,
		ModelSlug:            modelSlug,
		RequestedDataRegion:  reqRegion,
		RequestBodyBytes:     int64(len(rawBody)),
		EstimatedTotalTokens: estTokens,
	})
	if !dec.Allowed {
		if dec.DenialCode == policyengine.DenialCodeRegionDenied {
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", dec.DenialReason+" ("+policyengine.DenialCodeRegionDenied+")", true)
			return nil, false
		}
		writeCanonicalError(w, reqID, dec.HTTPStatus, dec.DenialCode, dec.ErrorType, dec.DenialReason, false)
		return nil, false
	}

	_, _, _, completionCount := estimateIngressTokensAndCountFromJSON(rawBody)
	return &preAdmissionDecision{
		snap:            snap,
		effPolicy:       eff,
		estTokens:       estTokens,
		estBaseIn:       estBaseIn,
		estMaxOut:       estMaxOut,
		completionCount: completionCount,
		reqRegion:       reqRegion,
	}, true
}

func (s *Server) acquirePreIdempotencyRPM(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	pre *preAdmissionDecision,
) bool {
	scope := ratelimit.LimitScope{
		OrgID:        match.Key.OrgID,
		ProjectID:    match.Key.ProjectID,
		EnvID:        match.Key.EnvID,
		VirtualKeyID: match.Key.ID,
	}
	rpmOnly := ratelimit.Limits{
		RPMLimit:             pre.effPolicy.RPMLimit,
		MaxConcurrentStreams: pre.effPolicy.MaxConcurrentStreams,
	}
	rlRes, rlErr := s.rateLimiter.AllowRequest(r.Context(), scope, 0, rpmOnly)
	writeRateLimitHeaders(w, rlRes, false)
	if rlErr != nil {
		if errors.Is(rlErr, ratelimit.ErrRateLimitExceeded) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, rlRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Rate limit exceeded for Virtual Key or parent policy scope.", true)
			return false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return false
	}
	return true
}

func (s *Server) acquirePostIdempotencyTPM(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	pre *preAdmissionDecision,
) (*admissionLease, bool) {
	scope := ratelimit.LimitScope{
		OrgID:        match.Key.OrgID,
		ProjectID:    match.Key.ProjectID,
		EnvID:        match.Key.EnvID,
		VirtualKeyID: match.Key.ID,
	}
	limits := ratelimit.Limits{
		RPMLimit:             pre.effPolicy.RPMLimit,
		TPMLimit:             pre.effPolicy.TPMLimit,
		MaxConcurrentStreams: pre.effPolicy.MaxConcurrentStreams,
	}
	tpmOnly := ratelimit.Limits{
		TPMLimit:             pre.effPolicy.TPMLimit,
		MaxConcurrentStreams: pre.effPolicy.MaxConcurrentStreams,
	}
	rlRes, rlErr := s.rateLimiter.AllowRequest(r.Context(), scope, pre.estTokens, tpmOnly)
	writeRateLimitHeaders(w, rlRes, false)
	if rlErr != nil {
		if errors.Is(rlErr, ratelimit.ErrRateLimitExceeded) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, rlRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Rate limit exceeded for Virtual Key or parent policy scope.", true)
			return nil, false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return nil, false
	}
	return &admissionLease{
		snap:      pre.snap,
		scope:     scope,
		limits:    limits,
		effPolicy: pre.effPolicy,
		estTokens: pre.estTokens,
		reqRegion: pre.reqRegion,
	}, true
}

func (s *Server) acquireRateLimits(
	w http.ResponseWriter,
	r *http.Request,
	reqID string,
	match snapshot.KeyMatchResult,
	pre *preAdmissionDecision,
	stream bool,
) (*admissionLease, bool) {
	scope := ratelimit.LimitScope{
		OrgID:        match.Key.OrgID,
		ProjectID:    match.Key.ProjectID,
		EnvID:        match.Key.EnvID,
		VirtualKeyID: match.Key.ID,
	}
	limits := ratelimit.Limits{
		RPMLimit:             pre.effPolicy.RPMLimit,
		TPMLimit:             pre.effPolicy.TPMLimit,
		MaxConcurrentStreams: pre.effPolicy.MaxConcurrentStreams,
	}

	var streamLease *ratelimit.StreamLease
	if stream && limits.MaxConcurrentStreams != nil {
		lease, sRes, sErr := s.rateLimiter.AcquireStreamSlot(r.Context(), scope, *limits.MaxConcurrentStreams)
		if sErr != nil {
			if errors.Is(sErr, ratelimit.ErrConcurrencyLimitExceeded) {
				w.Header().Set("X-RateLimit-Remaining-Streams", "0")
				w.Header().Set("Retry-After", strconv.Itoa(max(1, sRes.RetryAfterSec)))
				writeCanonicalError(w, reqID, http.StatusTooManyRequests, "concurrency_limit_exceeded", "rate_limit_error", "Maximum concurrent active streams limit reached for this Virtual Key.", true)
				return nil, false
			}
			writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
			return nil, false
		}
		streamLease = lease
		w.Header().Set("X-RateLimit-Remaining-Streams", strconv.FormatInt(sRes.RemainingStreams, 10))
	}

	rlRes, rlErr := s.rateLimiter.AllowRequest(r.Context(), scope, pre.estTokens, limits)
	writeRateLimitHeaders(w, rlRes, streamLease != nil)
	if rlErr != nil {
		if streamLease != nil {
			streamLease.Release()
		}
		if errors.Is(rlErr, ratelimit.ErrRateLimitExceeded) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, rlRes.RetryAfterSec)))
			writeCanonicalError(w, reqID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate_limit_error", "Rate limit exceeded for Virtual Key or parent policy scope.", true)
			return nil, false
		}
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "rate_limiter_unavailable", "api_error", "Rate limiter backend unavailable; failing closed.", true)
		return nil, false
	}

	return &admissionLease{
		streamLease: streamLease,
		snap:        pre.snap,
		scope:       scope,
		limits:      limits,
		effPolicy:   pre.effPolicy,
		estTokens:   pre.estTokens,
		reqRegion:   pre.reqRegion,
	}, true
}

func writeRateLimitHeaders(w http.ResponseWriter, res ratelimit.CheckResult, streamHeaderWritten bool) {
	if res.HasRPM {
		w.Header().Set("X-RateLimit-Remaining-Requests", strconv.FormatInt(res.RemainingRequests, 10))
	}
	if res.HasTPM {
		w.Header().Set("X-RateLimit-Remaining-Tokens", strconv.FormatInt(res.RemainingTokens, 10))
	}
	if res.HasStreams && !streamHeaderWritten {
		w.Header().Set("X-RateLimit-Remaining-Streams", strconv.FormatInt(res.RemainingStreams, 10))
	}
}

func (s *Server) reconcileAdmissionUsage(adm *admissionLease, usage *port.NormalizedUsage) {
	if s == nil || s.rateLimiter == nil || adm == nil || usage == nil {
		return
	}
	actual := max(int64(0), usage.InputTokens) +
		max(int64(0), usage.OutputTokens) +
		max(int64(0), usage.ReasoningTokens) +
		max(int64(0), usage.CacheWriteTokens)
	if usage.Accuracy == port.UsageAccuracyUnmeteredFlagged && actual == 0 {
		return
	}
	_ = s.rateLimiter.ReconcileTokens(context.Background(), adm.scope, adm.estTokens, actual, adm.limits)
}

func (s *Server) resolveTargetsWithPolicy(
	w http.ResponseWriter,
	reqID string,
	match snapshot.KeyMatchResult,
	pre *preAdmissionDecision,
	modelSlug string,
	requiredCaps []string,
) ([]*resolver.RankedTarget, bool) {
	var snap *snapshot.ConfigSnapshot
	if pre != nil && pre.snap != nil {
		snap = pre.snap
	} else {
		snap = s.snapshots.Current()
	}
	if snap == nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Configuration snapshot unavailable.", true)
		return nil, false
	}

	requireTested := match.Config != nil && match.Config.RequireTestedCaps
	candidates, rej := s.routeResolver.Resolve(resolver.ResolveRequest{
		RequestID:            reqID,
		Snapshot:             snap,
		OrgID:                match.Key.OrgID,
		ProjectID:            match.Key.ProjectID,
		EnvID:                match.Key.EnvID,
		TargetModel:          modelSlug,
		EffectivePolicy:      pre.effPolicy,
		RequestedDataRegion:  pre.reqRegion,
		RequiredCapabilities: requiredCaps,
		EstimatedInputTokens: pre.estBaseIn,
		MaxOutputTokens:      pre.estMaxOut,
		RequireTestedCaps:    requireTested,
		Breaker:              s.breaker,
	})
	if rej != nil {
		writeCanonicalError(w, reqID, rej.HTTPStatus, rej.Code, rej.ErrorType, rej.Message, rej.Retryable)
		return nil, false
	}
	return candidates, true
}

func estimateIngressTokensFromJSON(raw []byte) (estTotal, baseIn, maxOut int64) {
	estTotal, baseIn, maxOut, _ = estimateIngressTokensAndCountFromJSON(raw)
	return estTotal, baseIn, maxOut
}

func estimateIngressTokensAndCountFromJSON(raw []byte) (estTotal, baseIn, maxOut, completionCount int64) {
	completionCount = 1
	rawLen := int64(len(raw))
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		b := max(int64(1), (rawLen+3)/4)
		return b, b, 0, 1
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		b := max(int64(1), (rawLen+3)/4)
		return b, b, 0, 1
	}

	var excessMediaBytes int64
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := keyTok.(string)
		if !ok {
			break
		}
		switch key {
		case "max_completion_tokens", "max_tokens", "max_output_tokens":
			valTok, err := dec.Token()
			if err != nil {
				break
			}
			if num, ok := valTok.(json.Number); ok {
				if parsed, ok := parsePositiveJSONNumberCeiling(num); ok && parsed > maxOut {
					maxOut = parsed
				}
			} else if d, ok := valTok.(json.Delim); ok {
				excessMediaBytes += skipJSONCompositeCountMedia(dec, d, key, false)
			}
		case "n", "candidateCount", "candidate_count":
			valTok, err := dec.Token()
			if err != nil {
				break
			}
			if num, ok := valTok.(json.Number); ok {
				if parsed, ok := parsePositiveJSONNumberCeiling(num); ok && parsed > completionCount {
					completionCount = parsed
				}
			} else if d, ok := valTok.(json.Delim); ok {
				excessMediaBytes += skipJSONCompositeCountMedia(dec, d, key, false)
			}
		default:
			valTok, err := dec.Token()
			if err != nil {
				break
			}
			if v, ok := valTok.(json.Delim); ok {
				excessMediaBytes += skipJSONCompositeCountMedia(dec, v, key, isHTTPMultimodalRootKey(key))
			}
		}
	}

	effBytes := rawLen - excessMediaBytes
	if effBytes < 1 {
		effBytes = 1
	}
	baseIn = max(int64(1), (effBytes+3)/4)
	if maxOut > 0 {
		if completionCount > 1 {
			maxOut = mulCapInt64(maxOut, completionCount, 1<<62)
		}
		if maxOut >= (1<<62) || baseIn > (1<<62)-maxOut {
			return 1 << 62, baseIn, maxOut, completionCount
		}
		return baseIn + maxOut, baseIn, maxOut, completionCount
	}
	return baseIn, baseIn, 0, completionCount
}

func mulCapInt64(a, b, capVal int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a >= capVal || b >= capVal {
		return capVal
	}
	if a > capVal/b {
		return capVal
	}
	return a * b
}

func isHTTPMultimodalRootKey(key string) bool {
	return key == "messages" || key == "input"
}

func skipJSONCompositeCountMedia(dec *json.Decoder, open json.Delim, parentKey string, inMediaRoot bool) int64 {
	type frame struct {
		isObject    bool
		expectKey   bool
		curKey      string
		objName     string
		inMediaRoot bool
	}
	stack := []frame{{
		isObject:    open == '{',
		expectKey:   open == '{',
		curKey:      parentKey,
		objName:     parentKey,
		inMediaRoot: inMediaRoot,
	}}
	var excess int64
	for len(stack) > 0 {
		tok, err := dec.Token()
		if err != nil {
			return excess
		}
		top := &stack[len(stack)-1]
		if d, ok := tok.(json.Delim); ok {
			nextObjName := top.objName
			if top.isObject {
				nextObjName = top.curKey
			}
			switch d {
			case '{':
				stack = append(stack, frame{isObject: true, expectKey: true, objName: nextObjName, inMediaRoot: top.inMediaRoot})
			case '[':
				stack = append(stack, frame{isObject: false, expectKey: false, curKey: top.curKey, objName: nextObjName, inMediaRoot: top.inMediaRoot})
			case '}', ']':
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].isObject {
					stack[len(stack)-1].expectKey = true
				}
			}
			continue
		}
		if top.isObject && top.expectKey {
			if k, ok := tok.(string); ok {
				top.curKey = k
			}
			top.expectKey = false
			continue
		}
		if s, ok := tok.(string); ok {
			excess += mediaBlobExcessBytes(top.inMediaRoot, top.objName, top.curKey, s)
		}
		if top.isObject {
			top.expectKey = true
		}
	}
	return excess
}

func mediaBlobExcessBytes(inMediaRoot bool, parentObjKey, key, s string) int64 {
	const cappedBlobBytes = 1024
	if !inMediaRoot || len(s) <= cappedBlobBytes {
		return 0
	}
	k := strings.ToLower(strings.TrimSpace(key))
	pk := strings.ToLower(strings.TrimSpace(parentObjKey))
	switch k {
	case "url", "image_url", "file_url":
		if isMediaURLParentKey(pk) && isStrictMultimodalBase64DataURI(s) {
			return int64(len(s) - cappedBlobBytes)
		}
	case "file_data", "image_base64", "audio_base64", "b64_json":
		if isMediaPayloadParentKey(pk) && (isStrictMultimodalBase64DataURI(s) || isStrictBase64Payload(s)) {
			return int64(len(s) - cappedBlobBytes)
		}
	case "data":
		if isMediaContainerKey(pk) && (isStrictMultimodalBase64DataURI(s) || isStrictBase64Payload(s)) {
			return int64(len(s) - cappedBlobBytes)
		}
	}
	return 0
}

func isMediaURLParentKey(pk string) bool {
	switch pk {
	case "image_url", "image", "input_image", "file", "input_file", "document", "source", "input_audio", "audio", "content", "parts", "messages", "input":
		return true
	default:
		return false
	}
}

func isMediaPayloadParentKey(pk string) bool {
	switch pk {
	case "file", "input_file", "document", "image", "image_url", "input_image", "audio", "input_audio", "video", "source", "inlinedata", "inline_data", "content", "parts", "messages", "input":
		return true
	default:
		return false
	}
}

func isMediaContainerKey(pk string) bool {
	switch pk {
	case "source", "input_audio", "inlinedata", "inline_data", "image", "audio", "video", "file", "input_file", "document", "image_url":
		return true
	default:
		return false
	}
}

func isStrictMultimodalBase64DataURI(s string) bool {
	if len(s) < 12 || !strings.EqualFold(s[:5], "data:") {
		return false
	}
	headLimit := min(len(s), 128)
	headLower := strings.ToLower(s[5:headLimit])
	semiIdx := strings.Index(headLower, ";base64,")
	if semiIdx <= 0 {
		return false
	}
	mimeType := strings.SplitN(headLower[:semiIdx], ";", 2)[0]
	slash := strings.IndexByte(mimeType, '/')
	if slash <= 0 || slash == len(mimeType)-1 {
		return false
	}
	major := mimeType[:slash]
	subtype := mimeType[slash+1:]
	switch {
	case major == "image" || major == "audio" || major == "video":
	case mimeType == "application/pdf" || mimeType == "application/octet-stream":
	default:
		return false
	}
	for i := 0; i < len(subtype); i++ {
		c := subtype[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-') {
			return false
		}
	}
	payloadStart := 5 + semiIdx + len(";base64,")
	return isStrictBase64Payload(s[payloadStart:])
}

func isStrictBase64Payload(s string) bool {
	if len(s) == 0 || len(s)%4 != 0 {
		return false
	}
	strictEnc := base64.StdEncoding.Strict()
	var src [4096]byte
	var dst [3072]byte
	n := len(s)
	for off := 0; off < n; off += len(src) {
		end := min(off+len(src), n)
		chunkLen := end - off
		if end < n {
			// Ensure padding '=' never appears outside the very end of the full string.
			for i := off; i < end; i++ {
				if s[i] == '=' {
					return false
				}
			}
		}
		copy(src[:chunkLen], s[off:end])
		if _, err := strictEnc.Decode(dst[:], src[:chunkLen]); err != nil {
			return false
		}
	}
	return true
}

func parsePositiveJSONNumberCeiling(num json.Number) (int64, bool) {
	if iv, err := num.Int64(); err == nil {
		if iv <= 0 {
			return 0, false
		}
		if iv >= (1 << 62) {
			return 1 << 62, true
		}
		return iv, true
	}
	fv, err := strconv.ParseFloat(num.String(), 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) && !math.IsNaN(fv) && fv > 0 {
			return 1 << 62, true
		}
		return 0, false
	}
	if math.IsNaN(fv) || fv <= 0 {
		return 0, false
	}
	if math.IsInf(fv, 1) || fv >= float64(1<<62) {
		return 1 << 62, true
	}
	return int64(math.Ceil(fv)), true
}
