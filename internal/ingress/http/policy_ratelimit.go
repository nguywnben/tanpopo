// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/ratelimit"
)

type preAdmissionDecision struct {
	effPolicy policyengine.EffectivePolicy
	estTokens int64
	reqRegion string
}

type admissionLease struct {
	streamLease *ratelimit.StreamLease
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

	return &preAdmissionDecision{
		effPolicy: eff,
		estTokens: estTokens,
		reqRegion: reqRegion,
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

func (s *Server) resolveActiveTargetWithPolicy(
	w http.ResponseWriter,
	reqID string,
	match snapshot.KeyMatchResult,
	pre *preAdmissionDecision,
	modelSlug string,
) (*snapshot.ResolvedTarget, bool) {
	snap := s.snapshots.Current()
	if snap == nil {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "snapshot_not_ready", "api_error", "Configuration snapshot unavailable.", true)
		return nil, false
	}

	candidates := snap.ListCandidateTargets(match.Key.OrgID, modelSlug)
	var (
		sawQuarantined  bool
		sawModelDenied  bool
		modelDenial     policyengine.EvalDecision
		sawTrustDenied  bool
		trustDenial     policyengine.EvalDecision
		sawRegionDenied bool
		regionDenial    policyengine.EvalDecision
	)
	for _, cand := range candidates {
		if cand == nil || cand.Deployment == nil || cand.Deployment.State != "ACTIVE_HEALTHY" {
			continue
		}
		if cand.Connection == nil || cand.Connection.State != snapshot.ConnectionStateActive || !cand.Connection.VisibleToProject(match.Key.OrgID, match.Key.ProjectID) {
			continue
		}
		if cand.Adapter == nil {
			continue
		}
		if strings.EqualFold(cand.Adapter.ExtensionState, installer.StateQuarantined) {
			sawQuarantined = true
			continue
		}
		dec := policyengine.EvaluateEffective(pre.effPolicy, policyengine.EvalRequest{
			OrgID:                     match.Key.OrgID,
			ProjectID:                 match.Key.ProjectID,
			EnvID:                     match.Key.EnvID,
			ModelSlug:                 modelSlug,
			AdapterSlug:               cand.Adapter.AdapterSlug,
			DataRegion:                cand.Deployment.DataRegion,
			RequestedDataRegion:       pre.reqRegion,
			ExtensionTrustTier:        cand.Connection.DerivedPublicTrust,
			ExtensionEnterpriseSigned: cand.Connection.DerivedEnterpriseSigned,
			CapabilityAssurance:       cand.Deployment.AssuranceLevel,
			TargetEvaluation:          true,
		})
		if !dec.Allowed {
			switch dec.DenialCode {
			case policyengine.DenialCodeModelDenied:
				if !sawModelDenied {
					sawModelDenied = true
					modelDenial = dec
				}
			case policyengine.DenialCodeTrustDenied:
				if !sawTrustDenied {
					sawTrustDenied = true
					trustDenial = dec
				}
			case policyengine.DenialCodeRegionDenied:
				if !sawRegionDenied {
					sawRegionDenied = true
					regionDenial = dec
				}
			}
			continue
		}
		return cand, true
	}

	if sawQuarantined {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "extension_quarantined", "extension_error", "The backing provider adapter extension is quarantined.", false)
		return nil, false
	}
	if sawModelDenied {
		writeCanonicalError(w, reqID, http.StatusForbidden, policyengine.DenialCodeModelDenied, "permission_error", modelDenial.DenialReason, false)
		return nil, false
	}
	if sawTrustDenied {
		writeCanonicalError(w, reqID, trustDenial.HTTPStatus, policyengine.DenialCodeTrustDenied, trustDenial.ErrorType, trustDenial.DenialReason, false)
		return nil, false
	}
	if sawRegionDenied {
		writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", regionDenial.DenialReason+" ("+policyengine.DenialCodeRegionDenied+")", true)
		return nil, false
	}
	writeCanonicalError(w, reqID, http.StatusServiceUnavailable, "no_compliant_deployment_available", "residency_unavailable_error", fmt.Sprintf("No compliant active deployment is available for model %q.", modelSlug), true)
	return nil, false
}

func estimateIngressTokensFromJSON(raw []byte) int64 {
	base := max(int64(1), int64(len(raw)+3)/4)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return base
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return base
	}

	var maxOut int64
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			break
		}
		key, ok := keyTok.(string)
		if !ok {
			break
		}
		if key == "max_completion_tokens" || key == "max_tokens" || key == "max_output_tokens" {
			valTok, err := dec.Token()
			if err != nil {
				break
			}
			if num, ok := valTok.(json.Number); ok {
				if parsed, ok := parsePositiveJSONNumberCeiling(num); ok && parsed > maxOut {
					maxOut = parsed
				}
			} else if d, ok := valTok.(json.Delim); ok {
				if skipErr := skipJSONComposite(dec, d); skipErr != nil {
					break
				}
			}
			continue
		}
		if skipErr := skipNextJSONValue(dec); skipErr != nil {
			break
		}
	}

	if maxOut > 0 {
		if maxOut >= (1<<62) || base > (1<<62)-maxOut {
			return 1 << 62
		}
		return base + maxOut
	}
	return base
}

func skipNextJSONValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		return skipJSONComposite(dec, d)
	}
	return nil
}

func skipJSONComposite(dec *json.Decoder, open json.Delim) error {
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
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
