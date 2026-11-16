// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

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
	"github.com/nguywnben/tanpopo/internal/ingress/sse"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
)

type requestBudgetSession struct {
	svc             *ledger.Service
	defaultHoldOut  int64
	renewEvery      time.Duration
	orgID           string
	projectID       string
	envID           string
	vkeyID          string
	reqID           string
	ledgerReqID     string
	modelSlug       string
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
	completionCount := int64(1)
	if preAdm != nil {
		baseIn = preAdm.estBaseIn
		maxOut = preAdm.estMaxOut
		if preAdm.completionCount > 1 {
			completionCount = preAdm.completionCount
		}
	}
	defaultHold := s.defaultHoldOutputTokens
	if completionCount > 1 {
		if defaultHold <= 0 {
			defaultHold = pricing.DefaultHoldOutputTokens
		}
		defaultHold = mulCapInt64(defaultHold, completionCount, 1<<62)
	}
	textBytes := int64(len(rawBody))
	if baseIn > 0 && baseIn*4 < textBytes {
		textBytes = baseIn * 4
	}
	ttlMs := s.budgetLedger.LeaseTTLMs()
	renewEvery := time.Duration(ttlMs/3) * time.Millisecond
	if renewEvery <= 0 {
		renewEvery = time.Millisecond
	}
	return &requestBudgetSession{
		svc:             s.budgetLedger,
		defaultHoldOut:  defaultHold,
		renewEvery:      renewEvery,
		orgID:           key.OrgID,
		projectID:       key.ProjectID,
		envID:           key.EnvID,
		vkeyID:          key.ID,
		reqID:           reqID,
		ledgerReqID:     idgen.MustNew(idgen.PrefixTrace),
		modelSlug:       modelSlug,
		inputTextBytes:  textBytes,
		maxOutputTokens: maxOut,
		media:           countRequestMediaUnits(items),
	}
}

const maxEstimatedAudioMillis int64 = 1_000_000_000

func countRequestMediaUnits(items []any) pricing.MediaUsage {
	var m pricing.MediaUsage
	var walk func(v any, depth int, skipClassify bool)
	walk = func(v any, depth int, skipClassify bool) {
		if depth > 24 || v == nil {
			return
		}
		switch node := v.(type) {
		case []any:
			for _, elem := range node {
				walk(elem, depth+1, false)
			}
		case map[string]any:
			var consumed map[string]bool
			if !skipClassify {
				consumed = classifyMediaPart(node, &m)
			}
			for k, child := range node {
				switch child.(type) {
				case map[string]any, []any:
					lk := strings.ToLower(strings.TrimSpace(k))
					skipChild := false
					if consumed[lk] {
						if _, isMap := child.(map[string]any); isMap {
							skipChild = true
						}
					}
					walk(child, depth+1, skipChild)
				}
			}
		}
	}
	walk(items, 0, false)
	return m
}

func classifyMediaPart(part map[string]any, m *pricing.MediaUsage) map[string]bool {
	if part == nil {
		return nil
	}
	consumed := make(map[string]bool)
	ptype := strings.ToLower(strings.TrimSpace(stringField(part["type"])))

	var inlineImage, inlineAudio, inlineDoc bool
	var inlineConsumed []string
	for _, inlineKey := range []string{"inlineData", "inline_data", "source"} {
		inlineObj, ok := part[inlineKey].(map[string]any)
		if !ok || inlineObj == nil {
			continue
		}
		inlineConsumed = append(inlineConsumed, strings.ToLower(inlineKey))
		mt := strings.ToLower(strings.TrimSpace(firstStringField(inlineObj, "mimeType", "mime_type", "media_type")))
		switch {
		case strings.HasPrefix(mt, "image/"):
			inlineImage = true
		case strings.HasPrefix(mt, "audio/"):
			inlineAudio = true
		case mt != "":
			inlineDoc = true
		case inlineKey == "source" && hasAnyStringField(inlineObj, "data", "url", "file_data", "file_url"):
			switch ptype {
			case "file", "input_file", "document":
				inlineDoc = true
			case "input_audio", "audio":
				inlineAudio = true
			default:
				inlineImage = true
			}
		}
	}

	hasImageField := part["image_url"] != nil || part["image"] != nil || part["image_base64"] != nil || part["b64_json"] != nil
	hasAudioField := part["input_audio"] != nil || part["audio"] != nil || part["audio_base64"] != nil

	hasNonAudioDocField := false
	for _, docKey := range []string{"file", "input_file", "document", "fileData", "file_data"} {
		v, ok := part[docKey]
		if !ok || v == nil {
			continue
		}
		if sub, isMap := v.(map[string]any); isMap && sub != nil {
			subMT := strings.ToLower(strings.TrimSpace(firstStringField(sub, "mimeType", "mime_type", "media_type")))
			if strings.HasPrefix(subMT, "audio/") || hasAudioDataURIInMap(sub) {
				inlineAudio = true
				consumed[strings.ToLower(docKey)] = true
				continue
			}
		} else if s, isStr := v.(string); isStr && strings.HasPrefix(strings.ToLower(strings.TrimSpace(s)), "data:audio/") {
			inlineAudio = true
			continue
		}
		hasNonAudioDocField = true
		consumed[strings.ToLower(docKey)] = true
	}

	audioDataURIOnSelf := hasAudioDataURIInMap(part)
	if ptype == "input_audio" || ptype == "audio" || hasAudioField || inlineAudio || audioDataURIOnSelf {
		addAudioMillis(m, estimateAudioPartMillis(part))
		for _, k := range []string{"input_audio", "audio", "audio_base64"} {
			if part[k] != nil {
				consumed[k] = true
			}
		}
		for _, k := range inlineConsumed {
			consumed[k] = true
		}
	}

	if ptype == "image_url" || ptype == "input_image" || ptype == "image" || hasImageField || inlineImage {
		m.ImageCount++
		for _, k := range []string{"image_url", "image", "input_image", "image_base64", "b64_json"} {
			if part[k] != nil {
				consumed[k] = true
			}
		}
		for _, k := range inlineConsumed {
			consumed[k] = true
		}
	}

	isDocPType := ptype == "file" || ptype == "input_file" || ptype == "document"
	if inlineDoc || hasNonAudioDocField || (isDocPType && !inlineAudio && !audioDataURIOnSelf && !inlineImage) {
		m.DocumentPages++
		for _, k := range inlineConsumed {
			consumed[k] = true
		}
	}

	return consumed
}

func addAudioMillis(m *pricing.MediaUsage, delta int64) {
	if delta <= 0 {
		return
	}
	if m.AudioInputMillis >= maxEstimatedAudioMillis-delta {
		m.AudioInputMillis = maxEstimatedAudioMillis
		return
	}
	m.AudioInputMillis += delta
}

func firstStringField(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := stringField(obj[k]); s != "" {
			return s
		}
	}
	return ""
}

func hasAnyStringField(obj map[string]any, keys ...string) bool {
	for _, k := range keys {
		if strings.TrimSpace(stringField(obj[k])) != "" {
			return true
		}
	}
	return false
}

func hasAudioDataURIInMap(obj map[string]any) bool {
	for _, k := range []string{"url", "file_url", "file_data", "data"} {
		if s := strings.ToLower(strings.TrimSpace(stringField(obj[k]))); strings.HasPrefix(s, "data:audio/") {
			return true
		}
	}
	return false
}

func estimateAudioPartMillis(part map[string]any) int64 {
	var maxB64Len int64
	var explicitMillis int64
	inspectMap := func(obj map[string]any) {
		if obj == nil {
			return
		}
		if ms := extractDurationMillis(obj); ms > explicitMillis {
			explicitMillis = ms
		}
		for _, k := range []string{"data", "audio_base64", "file_data", "url", "file_url"} {
			s, ok := obj[k].(string)
			if !ok {
				continue
			}
			bLen := base64PayloadCharLen(s)
			if bLen > maxB64Len {
				maxB64Len = bLen
			}
		}
	}
	inspectMap(part)
	for _, subKey := range []string{"input_audio", "audio", "inlineData", "inline_data", "source", "file", "input_file", "document", "fileData", "file_data"} {
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
	if est > maxEstimatedAudioMillis {
		return maxEstimatedAudioMillis
	}
	return est
}

func base64PayloadCharLen(s string) int64 {
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

func extractDurationMillis(obj map[string]any) int64 {
	if v, ok := obj["duration_ms"]; ok {
		if n := anyToNonNegCeilMillis(v, 1); n > 0 {
			return n
		}
	}
	for _, k := range []string{"duration_seconds", "duration"} {
		if v, ok := obj[k]; ok {
			if n := anyToNonNegCeilMillis(v, 1000); n > 0 {
				return n
			}
		}
	}
	return 0
}

func anyToNonNegCeilMillis(v any, scale int64) int64 {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case int64:
		if t <= 0 {
			return 0
		}
		if t > maxEstimatedAudioMillis/scale {
			return maxEstimatedAudioMillis
		}
		return t * scale
	case int:
		if t <= 0 {
			return 0
		}
		iv := int64(t)
		if iv > maxEstimatedAudioMillis/scale {
			return maxEstimatedAudioMillis
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
	if scaled >= float64(maxEstimatedAudioMillis) {
		return maxEstimatedAudioMillis
	}
	return int64(scaled)
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

func (b *requestBudgetSession) startLeaseRenewalLocked(resID string) {
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

func (b *requestBudgetSession) stopLeaseRenewal() {
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
	if resID != "" && !settled {
		b.startLeaseRenewalLocked(resID)
	}
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

func (b *requestBudgetSession) settleSync(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	usage *port.NormalizedUsage,
	finalBody []byte,
) (*port.NormalizedUsage, error) {
	if b == nil {
		return usage, nil
	}
	inUsage := convertPortUsageToPricing(usage, b.media, b.inputTextBytes, int64(len(finalBody)), false)
	return b.settleInternal(ctx, target, requestedModel, inUsage, usage)
}

func (b *requestBudgetSession) settleStream(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	pumpRes sse.PumpResult,
	pumpErr error,
) (*port.NormalizedUsage, error) {
	if b == nil {
		return pumpRes.Usage, nil
	}
	forceEstimated := pumpErr != nil || pumpRes.Usage == nil
	inUsage := convertPortUsageToPricing(pumpRes.Usage, b.media, b.inputTextBytes, pumpRes.EmittedBytes, forceEstimated)
	return b.settleInternal(ctx, target, requestedModel, inUsage, pumpRes.Usage)
}

func (b *requestBudgetSession) settleInternal(
	ctx context.Context,
	target *snapshot.ResolvedTarget,
	requestedModel string,
	inUsage pricing.UsageInput,
	origUsage *port.NormalizedUsage,
) (*port.NormalizedUsage, error) {
	b.stopLeaseRenewal()

	b.mu.Lock()
	resID := b.reservationID
	already := b.settled
	b.mu.Unlock()
	if already {
		return origUsage, nil
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
		Rate:             rateCardForTarget(target),
		Usage:            inUsage,
	})
	if err != nil {
		slog.Error("budget ledger settlement failed",
			"org_id", b.orgID,
			"reservation_id", resID,
			"request_id", b.reqID,
			"ledger_request_id", b.ledgerReqID,
			"error", err,
		)
		return origUsage, err
	}

	b.mu.Lock()
	b.settled = true
	b.mu.Unlock()

	if origUsage == nil {
		out := &port.NormalizedUsage{
			Accuracy: portAccuracyFromLedger(res.Accuracy),
		}
		if res.Accuracy == pricing.AccuracyEstimated {
			out.InputTokens = res.InputTokens
			out.OutputTokens = res.OutputTokens
			out.CacheReadTokens = res.CacheReadTokens
			out.CacheWriteTokens = res.CacheWriteTokens
			out.ReasoningTokens = res.ReasoningTokens
		}
		return out, nil
	}
	cp := *origUsage
	cp.Accuracy = portAccuracyFromLedger(res.Accuracy)
	if res.Accuracy == pricing.AccuracyEstimated {
		cp.InputTokens = res.InputTokens
		cp.OutputTokens = res.OutputTokens
		cp.CacheReadTokens = res.CacheReadTokens
		cp.CacheWriteTokens = res.CacheWriteTokens
		cp.ReasoningTokens = res.ReasoningTokens
	}
	return &cp, nil
}

func convertPortUsageToPricing(u *port.NormalizedUsage, reqMedia pricing.MediaUsage, fallbackInBytes, fallbackOutBytes int64, forceEstimated bool) pricing.UsageInput {
	in := pricing.UsageInput{
		FallbackTextBytes:   max(int64(0), fallbackInBytes),
		FallbackOutputBytes: max(int64(0), fallbackOutBytes),
		Media:               reqMedia,
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
		m := pricing.MediaUsage{
			ImageCount:        max(int64(0), int64(u.MediaUsage.ImageCount)),
			ImagePixels:       max(int64(0), u.MediaUsage.ImagePixels),
			AudioInputMillis:  secondsFloatToMillis(u.MediaUsage.AudioInputSeconds),
			AudioOutputMillis: secondsFloatToMillis(u.MediaUsage.AudioOutputSeconds),
			DocumentPages:     max(int64(0), int64(u.MediaUsage.DocumentPages)),
			RerankSearchUnits: max(int64(0), int64(u.MediaUsage.RerankSearchUnits)),
		}
		if m.ImageCount == 0 {
			m.ImageCount = reqMedia.ImageCount
		}
		if m.AudioInputMillis == 0 {
			m.AudioInputMillis = reqMedia.AudioInputMillis
		}
		if m.DocumentPages == 0 {
			m.DocumentPages = reqMedia.DocumentPages
		}
		if m.RerankSearchUnits == 0 {
			m.RerankSearchUnits = reqMedia.RerankSearchUnits
		}
		in.Media = m
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
