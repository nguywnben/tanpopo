// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
)

type requestBudgetSession struct {
	svc             *ledger.Service
	defaultHoldOut  int64
	orgID           string
	projectID       string
	envID           string
	vkeyID          string
	reqID           string
	modelSlug       string
	inputTextBytes  int64
	maxOutputTokens int64
	media           pricing.MediaUsage

	mu            sync.Mutex
	reservationID string
	currentHold   int64
	settled       bool
}

func (s *Server) newRequestBudgetSession(
	key *snapshot.VirtualKeyView,
	reqID string,
	modelSlug string,
	rawBody []byte,
	preAdm *preAdmissionDecision,
	items []any,
) *requestBudgetSession {
	if s == nil || s.budgetLedger == nil || key == nil {
		return nil
	}
	var baseIn, maxOut int64
	if preAdm != nil {
		baseIn = preAdm.estBaseIn
		maxOut = preAdm.estMaxOut
	}
	textBytes := int64(len(rawBody))
	if baseIn > 0 && baseIn*4 < textBytes {
		textBytes = baseIn * 4
	}
	return &requestBudgetSession{
		svc:             s.budgetLedger,
		defaultHoldOut:  s.defaultHoldOutputTokens,
		orgID:           key.OrgID,
		projectID:       key.ProjectID,
		envID:           key.EnvID,
		vkeyID:          key.ID,
		reqID:           reqID,
		modelSlug:       modelSlug,
		inputTextBytes:  textBytes,
		maxOutputTokens: maxOut,
		media:           countRequestMediaUnits(items),
	}
}

func countRequestMediaUnits(items []any) pricing.MediaUsage {
	var m pricing.MediaUsage
	inspect := func(part map[string]any) {
		if part == nil {
			return
		}
		ptype := strings.ToLower(strings.TrimSpace(stringField(part["type"])))
		switch {
		case ptype == "image_url" || ptype == "input_image" || ptype == "image" || part["image_url"] != nil || part["image"] != nil:
			m.ImageCount++
		case ptype == "file" || ptype == "input_file" || ptype == "document" || part["file"] != nil || part["input_file"] != nil || part["document"] != nil:
			m.DocumentPages++
		}
	}
	for _, rawItem := range items {
		obj, ok := rawItem.(map[string]any)
		if !ok || obj == nil {
			continue
		}
		inspect(obj)
		parts, ok := obj["content"].([]any)
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			if part, ok := rawPart.(map[string]any); ok {
				inspect(part)
			}
		}
	}
	return m
}

func rateCardForTarget(target *snapshot.ResolvedTarget) pricing.RateCard {
	if target == nil || target.Deployment == nil {
		return pricing.RateCard{}
	}
	billingMode := ""
	if target.Connection != nil {
		billingMode = target.Connection.BillingMode
	}
	return pricing.RateCardFromSnapshot(target.Deployment.Pricing, billingMode)
}

func (b *requestBudgetSession) estimateHold(target *snapshot.ResolvedTarget) (int64, error) {
	rate := rateCardForTarget(target)
	bd, err := pricing.EstimatePreHold(rate, b.inputTextBytes, b.maxOutputTokens, b.defaultHoldOut, b.media)
	if err != nil {
		return 0, err
	}
	return bd.TotalMicroUSD, nil
}

func (b *requestBudgetSession) reserveInitial(ctx context.Context, w http.ResponseWriter, candidates []*resolver.RankedTarget) bool {
	if b == nil || len(candidates) == 0 {
		return true
	}
	var initialTarget *snapshot.ResolvedTarget
	for _, cand := range candidates {
		if cand != nil && cand.Target != nil {
			initialTarget = cand.Target
			break
		}
	}
	if initialTarget == nil {
		return true
	}
	hold, err := b.estimateHold(initialTarget)
	if err != nil {
		writeCanonicalError(w, b.reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Failed to compute deterministic pre-hold budget estimate.", false)
		return false
	}
	res, rerr := b.svc.Reserve(ctx, ledger.ReserveRequest{
		OrgID:        b.orgID,
		ProjectID:    b.projectID,
		EnvID:        b.envID,
		VirtualKeyID: b.vkeyID,
		RequestID:    b.reqID,
		HoldMicros:   hold,
	})
	if rerr != nil {
		if errors.Is(rerr, ledger.ErrBudgetExceeded) {
			writeCanonicalError(w, b.reqID, http.StatusTooManyRequests, "project_budget_exceeded", "rate_limit_error", "Budget limit exceeded across organization, project, environment, or virtual key scope.", false)
			return false
		}
		writeCanonicalError(w, b.reqID, http.StatusServiceUnavailable, "budget_ledger_unavailable", "api_error", "Budget ledger backend unavailable; failing closed.", true)
		return false
	}

	b.mu.Lock()
	b.reservationID = res.ID
	b.currentHold = res.HoldMicros
	b.mu.Unlock()
	return true
}

func (b *requestBudgetSession) ensureHoldForTarget(ctx context.Context, w http.ResponseWriter, target *snapshot.ResolvedTarget) (failover.AttemptResult, bool) {
	if b == nil {
		return failover.AttemptResult{}, true
	}
	needed, err := b.estimateHold(target)
	if err != nil {
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusBadRequest,
			FlushFinal: func() {
				writeCanonicalError(w, b.reqID, http.StatusBadRequest, "invalid_request_error", "invalid_request_error", "Failed to compute failover pre-hold estimate.", false)
			},
		}, false
	}

	b.mu.Lock()
	resID := b.reservationID
	curHold := b.currentHold
	b.mu.Unlock()

	if resID == "" || needed <= curHold {
		return failover.AttemptResult{}, true
	}

	updated, err := b.svc.DeltaReserve(ctx, ledger.DeltaReserveRequest{
		OrgID:         b.orgID,
		ReservationID: resID,
		NewHoldMicros: needed,
	})
	if err != nil {
		if errors.Is(err, ledger.ErrBudgetExceeded) {
			return failover.AttemptResult{
				Succeeded:  false,
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusTooManyRequests,
				FlushFinal: func() {
					writeCanonicalError(w, b.reqID, http.StatusTooManyRequests, "project_budget_exceeded", "rate_limit_error", "Failover target exceeds remaining budget headroom.", false)
				},
			}, false
		}
		return failover.AttemptResult{
			Succeeded:  false,
			Retryable:  false,
			Outcome:    breaker.OutcomeCallerError,
			HTTPStatus: http.StatusServiceUnavailable,
			FlushFinal: func() {
				writeCanonicalError(w, b.reqID, http.StatusServiceUnavailable, "budget_ledger_unavailable", "api_error", "Budget ledger delta reservation failed; failing closed.", true)
			},
		}, false
	}

	b.mu.Lock()
	b.currentHold = updated.HoldMicros
	b.mu.Unlock()
	return failover.AttemptResult{}, true
}

func (b *requestBudgetSession) markStreaming(ctx context.Context) {
	if b == nil {
		return
	}
	b.mu.Lock()
	resID := b.reservationID
	settled := b.settled
	b.mu.Unlock()
	if resID == "" || settled {
		return
	}
	_ = b.svc.MarkStreaming(context.WithoutCancel(ctx), b.orgID, resID)
}

func (b *requestBudgetSession) releaseIfUnsettled() {
	if b == nil {
		return
	}
	b.mu.Lock()
	resID := b.reservationID
	settled := b.settled
	b.mu.Unlock()
	if resID == "" || settled {
		return
	}
	_, _ = b.svc.Release(context.Background(), b.orgID, resID)
}

func (b *requestBudgetSession) settleSync(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	usage *port.NormalizedUsage,
	finalBody []byte,
) *port.NormalizedUsage {
	if b == nil {
		return usage
	}
	inUsage := convertPortUsageToPricing(usage, b.inputTextBytes, int64(len(finalBody)), false)
	return b.settleInternal(ctx, target, requestedModel, inUsage, usage)
}

func (b *requestBudgetSession) settleStream(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	pumpRes sse.PumpResult,
	pumpErr error,
) {
	if b == nil {
		return
	}
	forceEstimated := pumpErr != nil || pumpRes.Usage == nil
	inUsage := convertPortUsageToPricing(pumpRes.Usage, b.inputTextBytes, pumpRes.EmittedBytes, forceEstimated)
	b.settleInternal(ctx, target, requestedModel, inUsage, pumpRes.Usage)
}

func (b *requestBudgetSession) settleInternal(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	inUsage pricing.UsageInput,
	origUsage *port.NormalizedUsage,
) *port.NormalizedUsage {
	b.mu.Lock()
	resID := b.reservationID
	already := b.settled
	b.mu.Unlock()
	if already {
		return origUsage
	}

	deplID := ""
	modelSlug := strings.TrimSpace(requestedModel)
	if target != nil && target.Deployment != nil {
		deplID = target.Deployment.ID
		if strings.TrimSpace(target.Deployment.CanonicalModelSlug) != "" {
			modelSlug = strings.TrimSpace(target.Deployment.CanonicalModelSlug)
		}
	}

	res, err := b.svc.Settle(context.WithoutCancel(ctx), ledger.SettleRequest{
		OrgID:            b.orgID,
		ReservationID:    resID,
		RequestID:        b.reqID,
		ProjectID:        b.projectID,
		EnvID:            b.envID,
		VirtualKeyID:     b.vkeyID,
		Plane:            ledger.PlaneProvider,
		DeploymentID:     deplID,
		LogicalModelSlug: modelSlug,
		Rate:             rateCardForTarget(target),
		Usage:            inUsage,
	})
	if err != nil {
		return origUsage
	}

	b.mu.Lock()
	b.settled = true
	b.mu.Unlock()

	if origUsage == nil {
		return &port.NormalizedUsage{
			Accuracy: portAccuracyFromLedger(res.Accuracy),
		}
	}
	cp := *origUsage
	cp.Accuracy = portAccuracyFromLedger(res.Accuracy)
	return &cp
}

func convertPortUsageToPricing(u *port.NormalizedUsage, fallbackInBytes, fallbackOutBytes int64, forceEstimated bool) pricing.UsageInput {
	in := pricing.UsageInput{
		FallbackTextBytes:   max(int64(0), fallbackInBytes),
		FallbackOutputBytes: max(int64(0), fallbackOutBytes),
	}
	if u == nil {
		in.UpstreamUsagePresent = false
		return in
	}
	in.InputTokens = max(int64(0), u.InputTokens)
	in.OutputTokens = max(int64(0), u.OutputTokens)
	in.CacheReadTokens = max(int64(0), u.CacheReadTokens)
	in.CacheWriteTokens = max(int64(0), u.CacheWriteTokens)
	in.ReasoningTokens = max(int64(0), u.ReasoningTokens)
	if u.MediaUsage != nil {
		in.Media = pricing.MediaUsage{
			ImageCount:        max(int64(0), int64(u.MediaUsage.ImageCount)),
			ImagePixels:       max(int64(0), u.MediaUsage.ImagePixels),
			AudioInputMillis:  secondsFloatToMillis(u.MediaUsage.AudioInputSeconds),
			AudioOutputMillis: secondsFloatToMillis(u.MediaUsage.AudioOutputSeconds),
			DocumentPages:     max(int64(0), int64(u.MediaUsage.DocumentPages)),
			RerankSearchUnits: max(int64(0), int64(u.MediaUsage.RerankSearchUnits)),
		}
	}

	if forceEstimated {
		// Partial/aborted SSE stream (AC-2.1): fill missing input/output tokens from observed bytes
		// and stamp ESTIMATED accuracy honestly.
		in.UpstreamUsagePresent = true
		in.UpstreamAccuracy = pricing.AccuracyEstimated
		if in.InputTokens == 0 && in.FallbackTextBytes > 0 {
			in.InputTokens = pricing.EstimateTokensFromUTF8Bytes(in.FallbackTextBytes)
		}
		if in.OutputTokens == 0 && in.FallbackOutputBytes > 0 {
			in.OutputTokens = pricing.EstimateTokensFromUTF8Bytes(in.FallbackOutputBytes)
		}
		return in
	}

	in.UpstreamUsagePresent = true
	in.UpstreamAccuracy = ledgerAccuracyFromPort(u.Accuracy)
	return in
}

func secondsFloatToMillis(sec float64) int64 {
	if sec <= 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0
	}
	v := math.Ceil(sec * 1000.0)
	if v >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(v)
}

func ledgerAccuracyFromPort(acc port.UsageAccuracy) string {
	switch acc {
	case port.UsageAccuracyExact:
		return pricing.AccuracyExact
	case port.UsageAccuracyUnmeteredFlagged:
		return pricing.AccuracyUnmeteredFlagged
	default:
		return pricing.AccuracyEstimated
	}
}

func portAccuracyFromLedger(acc string) port.UsageAccuracy {
	switch acc {
	case pricing.AccuracyExact:
		return port.UsageAccuracyExact
	case pricing.AccuracyUnmeteredFlagged:
		return port.UsageAccuracyUnmeteredFlagged
	default:
		return port.UsageAccuracyEstimated
	}
}
