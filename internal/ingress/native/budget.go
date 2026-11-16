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
	"strconv"
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
	completionCount int64,
) *nativeBudgetSession {
	if h == nil || h.budgetLedger == nil || key == nil {
		return nil
	}
	if completionCount < 1 {
		completionCount = 1
	}
	defaultHold := h.defaultHoldOutputTokens
	if completionCount > 1 {
		if defaultHold <= 0 {
			defaultHold = pricing.DefaultHoldOutputTokens
		}
		defaultHold = mulCapNativeInt64(defaultHold, completionCount, 1<<62)
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
		defaultHoldOut:  defaultHold,
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

const maxEstimatedNativeAudioMillis int64 = 1_000_000_000

func countNativeMediaUnits(rawBody []byte) pricing.MediaUsage {
	var m pricing.MediaUsage
	if len(rawBody) == 0 {
		return m
	}
	var root map[string]any
	if err := json.Unmarshal(rawBody, &root); err != nil || root == nil {
		return m
	}

	var walk func(v any, depth int)
	walk = func(v any, depth int) {
		if depth > 24 || v == nil {
			return
		}
		switch node := v.(type) {
		case []any:
			for _, elem := range node {
				walk(elem, depth+1)
			}
		case map[string]any:
			if classifyNativeMediaPart(node, &m) {
				return
			}
			for _, child := range node {
				switch child.(type) {
				case map[string]any, []any:
					walk(child, depth+1)
				}
			}
		}
	}

	for _, arrKey := range []string{"messages", "input", "contents"} {
		if items, ok := root[arrKey]; ok && items != nil {
			walk(items, 0)
		}
	}
	return m
}

func classifyNativeMediaPart(part map[string]any, m *pricing.MediaUsage) bool {
	if part == nil {
		return false
	}
	ptype := strings.ToLower(strings.TrimSpace(nativeStringField(part["type"])))
	if ptype == "image_url" || ptype == "input_image" || ptype == "image" || part["image_url"] != nil || part["image"] != nil {
		m.ImageCount++
		return true
	}
	if ptype == "input_audio" || ptype == "audio" || part["input_audio"] != nil || part["audio"] != nil || part["audio_base64"] != nil {
		addNativeAudioMillis(m, estimateNativeAudioPartMillis(part))
		return true
	}
	for _, inlineKey := range []string{"inlineData", "inline_data", "source"} {
		if inlineObj, ok := part[inlineKey].(map[string]any); ok && inlineObj != nil {
			mt := strings.ToLower(strings.TrimSpace(firstNativeStringField(inlineObj, "mimeType", "mime_type", "media_type")))
			switch {
			case strings.HasPrefix(mt, "image/"):
				m.ImageCount++
				return true
			case strings.HasPrefix(mt, "audio/"):
				addNativeAudioMillis(m, estimateNativeAudioPartMillis(part))
				return true
			case mt != "":
				m.DocumentPages++
				return true
			}
		}
	}
	if hasNativeAudioDataURIInPart(part) {
		addNativeAudioMillis(m, estimateNativeAudioPartMillis(part))
		return true
	}
	if ptype == "file" || ptype == "input_file" || ptype == "document" || part["file"] != nil || part["input_file"] != nil || part["document"] != nil || part["fileData"] != nil || part["file_data"] != nil {
		m.DocumentPages++
		return true
	}
	return false
}

func nativeStringField(v any) string {
	s, _ := v.(string)
	return s
}

func firstNativeStringField(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := nativeStringField(obj[k]); s != "" {
			return s
		}
	}
	return ""
}

func addNativeAudioMillis(m *pricing.MediaUsage, delta int64) {
	if delta <= 0 {
		return
	}
	if m.AudioInputMillis >= maxEstimatedNativeAudioMillis-delta {
		m.AudioInputMillis = maxEstimatedNativeAudioMillis
		return
	}
	m.AudioInputMillis += delta
}

func hasNativeAudioDataURIInPart(part map[string]any) bool {
	for _, k := range []string{"url", "file_url", "file_data", "data"} {
		if s := strings.ToLower(strings.TrimSpace(nativeStringField(part[k]))); strings.HasPrefix(s, "data:audio/") {
			return true
		}
	}
	for _, subKey := range []string{"file", "input_file", "document", "fileData"} {
		if sub, ok := part[subKey].(map[string]any); ok && sub != nil {
			for _, k := range []string{"url", "file_url", "file_data", "data"} {
				if s := strings.ToLower(strings.TrimSpace(nativeStringField(sub[k]))); strings.HasPrefix(s, "data:audio/") {
					return true
				}
			}
		}
	}
	return false
}

func estimateNativeAudioPartMillis(part map[string]any) int64 {
	var maxB64Len int64
	var explicitMillis int64
	inspectMap := func(obj map[string]any) {
		if obj == nil {
			return
		}
		if ms := extractNativeDurationMillis(obj); ms > explicitMillis {
			explicitMillis = ms
		}
		for _, k := range []string{"data", "audio_base64", "file_data", "url", "file_url"} {
			s, ok := obj[k].(string)
			if !ok {
				continue
			}
			bLen := nativeBase64PayloadCharLen(s)
			if bLen > maxB64Len {
				maxB64Len = bLen
			}
		}
	}
	inspectMap(part)
	for _, subKey := range []string{"input_audio", "audio", "inlineData", "inline_data", "source", "file", "input_file"} {
		if sub, ok := part[subKey].(map[string]any); ok {
			inspectMap(sub)
		}
	}
	var bytesMillis int64
	if maxB64Len > 0 {
		decodedBytes := (maxB64Len * 3) / 4
		bytesMillis = (decodedBytes * 1000) / 16000
	}
	est := max(int64(1000), max(explicitMillis, bytesMillis))
	if est > maxEstimatedNativeAudioMillis {
		return maxEstimatedNativeAudioMillis
	}
	return est
}

func nativeBase64PayloadCharLen(s string) int64 {
	if s == "" {
		return 0
	}
	if len(s) >= 5 && strings.EqualFold(s[:5], "data:") {
		head := strings.ToLower(s[:min(len(s), 128)])
		if idx := strings.Index(head, ";base64,"); idx >= 0 {
			return int64(len(s) - (idx + len(";base64,")))
		}
		return 0
	}
	return int64(len(s))
}

func extractNativeDurationMillis(obj map[string]any) int64 {
	if v, ok := obj["duration_ms"]; ok {
		if n := anyToNonNegCeilNativeMillis(v, 1); n > 0 {
			return n
		}
	}
	for _, k := range []string{"duration_seconds", "duration"} {
		if v, ok := obj[k]; ok {
			if n := anyToNonNegCeilNativeMillis(v, 1000); n > 0 {
				return n
			}
		}
	}
	return 0
}

func anyToNonNegCeilNativeMillis(v any, scale int64) int64 {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case int64:
		if t <= 0 {
			return 0
		}
		if t > maxEstimatedNativeAudioMillis/scale {
			return maxEstimatedNativeAudioMillis
		}
		return t * scale
	case int:
		if t <= 0 {
			return 0
		}
		iv := int64(t)
		if iv > maxEstimatedNativeAudioMillis/scale {
			return maxEstimatedNativeAudioMillis
		}
		return iv * scale
	case json.Number:
		parsed, err := strconv.ParseFloat(t.String(), 64)
		if err != nil {
			return 0
		}
		f = parsed
	default:
		return 0
	}
	if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	scaled := math.Ceil(f * float64(scale))
	if scaled >= float64(maxEstimatedNativeAudioMillis) {
		return maxEstimatedNativeAudioMillis
	}
	return int64(scaled)
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
	if res.Accuracy == pricing.AccuracyEstimated {
		usage.InputTokens = res.InputTokens
		usage.OutputTokens = res.OutputTokens
		usage.CacheReadTokens = res.CacheReadTokens
		usage.CacheWriteTokens = res.CacheWriteTokens
		usage.ReasoningTokens = res.ReasoningTokens
	}
	return usage, nil
}

func mulCapNativeInt64(a, b, limit int64) int64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	if a >= limit || b >= limit || a > limit/b {
		return limit
	}
	return a * b
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
