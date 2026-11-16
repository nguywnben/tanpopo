// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package native

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/finops/pricing"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
)

type nativeBudgetSession struct {
	svc             *ledger.Service
	defaultHoldOut  int64
	renewEvery      time.Duration
	orgID           string
	projectID       string
	envID           string
	vkeyID          string
	reqID           string
	ledgerReqID     string
	inputTextBytes  int64
	maxOutputTokens int64
	media           pricing.MediaUsage

	mu            sync.Mutex
	reservationID string
	currentHold   int64
	settled       bool
	renewCancel   context.CancelFunc
	renewDone     chan struct{}
}

func (h *Handler) newNativeBudgetSession(
	key *snapshot.VirtualKeyView,
	reqID string,
	rawBody []byte,
	estBaseIn int64,
	estMaxOut int64,
) *nativeBudgetSession {
	if h == nil || h.budgetLedger == nil || key == nil {
		return nil
	}
	textBytes := int64(len(rawBody))
	if estBaseIn > 0 && estBaseIn*4 < textBytes {
		textBytes = estBaseIn * 4
	}
	ttlMs := h.budgetLedger.LeaseTTLMs()
	renewEvery := time.Duration(ttlMs/3) * time.Millisecond
	if renewEvery <= 0 {
		renewEvery = time.Millisecond
	}
	return &nativeBudgetSession{
		svc:             h.budgetLedger,
		defaultHoldOut:  h.defaultHoldOutputTokens,
		renewEvery:      renewEvery,
		orgID:           key.OrgID,
		projectID:       key.ProjectID,
		envID:           key.EnvID,
		vkeyID:          key.ID,
		reqID:           reqID,
		ledgerReqID:     idgen.MustNew(idgen.PrefixTrace),
		inputTextBytes:  textBytes,
		maxOutputTokens: estMaxOut,
		media:           countNativeMediaUnits(rawBody),
	}
}

func countNativeMediaUnits(rawBody []byte) pricing.MediaUsage {
	var m pricing.MediaUsage
	if len(rawBody) == 0 {
		return m
	}
	var root map[string]any
	if err := json.Unmarshal(rawBody, &root); err != nil || root == nil {
		return m
	}

	inspectPart := func(part map[string]any) {
		if part == nil {
			return
		}
		ptype := ""
		if s, ok := part["type"].(string); ok {
			ptype = strings.ToLower(strings.TrimSpace(s))
		}
		switch {
		case ptype == "image_url" || ptype == "input_image" || ptype == "image" || part["image_url"] != nil || part["image"] != nil:
			m.ImageCount++
			return
		case ptype == "file" || ptype == "input_file" || ptype == "document" || part["file"] != nil || part["input_file"] != nil || part["document"] != nil || part["fileData"] != nil || part["file_data"] != nil:
			m.DocumentPages++
			return
		}
		for _, inlineKey := range []string{"inlineData", "inline_data"} {
			if inlineObj, ok := part[inlineKey].(map[string]any); ok && inlineObj != nil {
				mt := ""
				if s, ok := inlineObj["mimeType"].(string); ok {
					mt = strings.ToLower(strings.TrimSpace(s))
				} else if s, ok := inlineObj["mime_type"].(string); ok {
					mt = strings.ToLower(strings.TrimSpace(s))
				}
				switch {
				case strings.HasPrefix(mt, "image/"):
					m.ImageCount++
				case mt != "":
					m.DocumentPages++
				}
				return
			}
		}
	}

	for _, arrKey := range []string{"messages", "input", "contents"} {
		items, ok := root[arrKey].([]any)
		if !ok {
			continue
		}
		for _, rawItem := range items {
			obj, ok := rawItem.(map[string]any)
			if !ok || obj == nil {
				continue
			}
			inspectPart(obj)
			for _, subKey := range []string{"content", "parts"} {
				parts, ok := obj[subKey].([]any)
				if !ok {
					continue
				}
				for _, rawPart := range parts {
					if part, ok := rawPart.(map[string]any); ok {
						inspectPart(part)
					}
				}
			}
		}
	}
	return m
}

func nativeRateCardForTarget(target *snapshot.ResolvedTarget) pricing.RateCard {
	if target == nil || target.Deployment == nil {
		return pricing.RateCard{}
	}
	billingMode := ""
	if target.Connection != nil {
		billingMode = target.Connection.BillingMode
	}
	return pricing.RateCardFromSnapshot(target.Deployment.Pricing, billingMode)
}

func (b *nativeBudgetSession) estimateHold(target *snapshot.ResolvedTarget) (int64, error) {
	rate := nativeRateCardForTarget(target)
	bd, err := pricing.EstimatePreHold(rate, b.inputTextBytes, b.maxOutputTokens, b.defaultHoldOut, b.media)
	if err != nil {
		return 0, err
	}
	return bd.TotalMicroUSD, nil
}

func (b *nativeBudgetSession) startLeaseRenewalLocked(resID string) {
	if b.renewCancel != nil || b.settled || resID == "" || b.renewEvery <= 0 {
		return
	}
	rCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	b.renewCancel = cancel
	b.renewDone = done
	svc := b.svc
	orgID := b.orgID
	interval := b.renewEvery
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-rCtx.Done():
				return
			case <-ticker.C:
				_ = svc.RenewLease(rCtx, orgID, resID)
			}
		}
	}()
}

func (b *nativeBudgetSession) stopLeaseRenewal() {
	if b == nil {
		return
	}
	b.mu.Lock()
	cancel := b.renewCancel
	done := b.renewDone
	b.renewCancel = nil
	b.renewDone = nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (b *nativeBudgetSession) reserveInitial(ctx context.Context, w http.ResponseWriter, candidates []*resolver.RankedTarget) bool {
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
		RequestID:    b.ledgerReqID,
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
	b.startLeaseRenewalLocked(res.ID)
	b.mu.Unlock()
	return true
}

func (b *nativeBudgetSession) ensureHoldForTarget(ctx context.Context, w http.ResponseWriter, target *snapshot.ResolvedTarget) (failover.AttemptResult, bool) {
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

func (b *nativeBudgetSession) markStreaming(ctx context.Context) {
	if b == nil {
		return
	}
	b.mu.Lock()
	resID := b.reservationID
	settled := b.settled
	if resID != "" && !settled {
		b.startLeaseRenewalLocked(resID)
	}
	b.mu.Unlock()
	if resID == "" || settled {
		return
	}
	_ = b.svc.MarkStreaming(context.WithoutCancel(ctx), b.orgID, resID)
}

func (b *nativeBudgetSession) releaseIfUnsettled() {
	if b == nil {
		return
	}
	b.stopLeaseRenewal()
	b.mu.Lock()
	resID := b.reservationID
	settled := b.settled
	b.mu.Unlock()
	if resID == "" || settled {
		return
	}
	_, _ = b.svc.Release(context.Background(), b.orgID, resID)
}

func (b *nativeBudgetSession) settle(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	usage port.NormalizedUsage,
	fallbackOutBytes int64,
	forceEstimated bool,
) (port.NormalizedUsage, error) {
	if b == nil {
		return usage, nil
	}
	b.stopLeaseRenewal()

	b.mu.Lock()
	resID := b.reservationID
	already := b.settled
	b.mu.Unlock()
	if already {
		return usage, nil
	}

	inUsage := pricing.UsageInput{
		InputTokens:          max(int64(0), usage.InputTokens),
		OutputTokens:         max(int64(0), usage.OutputTokens),
		CacheReadTokens:      max(int64(0), usage.CacheReadTokens),
		CacheWriteTokens:     max(int64(0), usage.CacheWriteTokens),
		ReasoningTokens:      max(int64(0), usage.ReasoningTokens),
		FallbackTextBytes:    max(int64(0), b.inputTextBytes),
		FallbackOutputBytes:  max(int64(0), fallbackOutBytes),
		UpstreamUsagePresent: true,
		UpstreamAccuracy:     nativeLedgerAccuracy(usage.Accuracy),
	}
	if usage.MediaUsage != nil {
		inUsage.Media = pricing.MediaUsage{
			ImageCount:        max(int64(0), int64(usage.MediaUsage.ImageCount)),
			ImagePixels:       max(int64(0), usage.MediaUsage.ImagePixels),
			AudioInputMillis:  secondsToMillis(usage.MediaUsage.AudioInputSeconds),
			AudioOutputMillis: secondsToMillis(usage.MediaUsage.AudioOutputSeconds),
			DocumentPages:     max(int64(0), int64(usage.MediaUsage.DocumentPages)),
			RerankSearchUnits: max(int64(0), int64(usage.MediaUsage.RerankSearchUnits)),
		}
	}
	if forceEstimated {
		inUsage.UpstreamAccuracy = pricing.AccuracyEstimated
		if inUsage.InputTokens == 0 && inUsage.FallbackTextBytes > 0 {
			inUsage.InputTokens = pricing.EstimateTokensFromUTF8Bytes(inUsage.FallbackTextBytes)
		}
		if inUsage.OutputTokens == 0 && inUsage.FallbackOutputBytes > 0 {
			inUsage.OutputTokens = pricing.EstimateTokensFromUTF8Bytes(inUsage.FallbackOutputBytes)
		}
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
		RequestID:        b.ledgerReqID,
		ProjectID:        b.projectID,
		EnvID:            b.envID,
		VirtualKeyID:     b.vkeyID,
		Plane:            ledger.PlaneProvider,
		DeploymentID:     deplID,
		LogicalModelSlug: modelSlug,
		Rate:             nativeRateCardForTarget(target),
		Usage:            inUsage,
	})
	if err != nil {
		slog.Error("native budget ledger settlement failed",
			"org_id", b.orgID,
			"reservation_id", resID,
			"request_id", b.reqID,
			"ledger_request_id", b.ledgerReqID,
			"error", err,
		)
		return usage, err
	}
	b.mu.Lock()
	b.settled = true
	b.mu.Unlock()

	usage.Accuracy = nativePortAccuracy(res.Accuracy)
	return usage, nil
}

func secondsToMillis(sec float64) int64 {
	if sec <= 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0
	}
	v := math.Ceil(sec * 1000.0)
	if v >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(v)
}

func nativeLedgerAccuracy(acc port.UsageAccuracy) string {
	switch acc {
	case port.UsageAccuracyExact:
		return pricing.AccuracyExact
	case port.UsageAccuracyUnmeteredFlagged:
		return pricing.AccuracyUnmeteredFlagged
	default:
		return pricing.AccuracyEstimated
	}
}

func nativePortAccuracy(acc string) port.UsageAccuracy {
	switch acc {
	case pricing.AccuracyExact:
		return port.UsageAccuracyExact
	case pricing.AccuracyUnmeteredFlagged:
		return port.UsageAccuracyUnmeteredFlagged
	default:
		return port.UsageAccuracyEstimated
	}
}
