// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package failover executes sequential Pre-Stream Failover retries over
// Step-6 bounded candidate sets while maintaining immutable mid-stream lockout,
// SM-04 breaker outcome accounting, and per-hop RouteAttemptTrace diagnostics
// (SPEC-CORE-01 / TP-FR-0103 / TP-IMP-0020).
package failover

import (
	"context"
	"errors"
	"time"

	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
)

// RouteAttemptTrace captures per-hop diagnostic metadata emitted by pre-stream failover.
type RouteAttemptTrace struct {
	AttemptIndex  int
	DeploymentID  string
	DataRegion    string
	BreakerState  string
	HTTPStatus    int
	Outcome       breaker.OutcomeKind
	DurationMs    int64
	RetryAfterRaw string
	StreamStarted bool
}

// AttemptResult summarizes the outcome of a single upstream attempt.
type AttemptResult struct {
	Succeeded     bool
	StreamStarted bool
	Retryable     bool
	Outcome       breaker.OutcomeKind
	HTTPStatus    int
	RetryAfterRaw string
	FlushFinal    func()
}

// ExecutionResult reports the final outcome and all attempted hops.
type ExecutionResult struct {
	AttemptCount     int
	Succeeded        bool
	StreamStarted    bool
	ExhaustedBreaker bool
	ClientCanceled   bool
	DeadlineExceeded bool
	Winner           *resolver.RankedTarget
	Traces           []RouteAttemptTrace
}

// AttemptFunc executes a single deployment candidate hop pre-stream (or completes an SSE stream).
type AttemptFunc func(hopCtx context.Context, attemptIdx int, candidate *resolver.RankedTarget) AttemptResult

// Config configures sequential pre-stream failover execution.
type Config struct {
	Breaker         *breaker.CircuitBreaker
	RequestDeadline time.Time
	Now             func() time.Time
}

func deadlineReached(runCtx context.Context, deadline, now time.Time) bool {
	if !deadline.IsZero() && !now.Before(deadline) {
		return true
	}
	return runCtx != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded)
}

// Execute runs sequential failover strictly pre-stream across ranked candidates
// within ctx and RequestDeadline bounds, updating SM-04 CircuitBreaker state.
func Execute(
	ctx context.Context,
	cfg Config,
	candidates []*resolver.RankedTarget,
	fn AttemptFunc,
) ExecutionResult {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	if ctx == nil {
		ctx = context.Background()
	}

	res := ExecutionResult{
		Traces: make([]RouteAttemptTrace, 0, len(candidates)),
	}

	var runCtx context.Context
	var cancelRun context.CancelFunc
	if !cfg.RequestDeadline.IsZero() {
		remaining := cfg.RequestDeadline.Sub(nowFn())
		if remaining <= 0 {
			res.DeadlineExceeded = true
			return res
		}
		runCtx, cancelRun = context.WithTimeout(ctx, remaining)
	} else {
		runCtx, cancelRun = context.WithCancel(ctx)
	}
	defer cancelRun()

	var lastFlush func()
	for _, cand := range candidates {
		if cand == nil || cand.Target == nil || cand.Target.Deployment == nil {
			continue
		}
		if deadlineReached(runCtx, cfg.RequestDeadline, nowFn()) {
			res.DeadlineExceeded = true
			break
		}
		if runCtx.Err() != nil {
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				res.DeadlineExceeded = true
			} else {
				res.ClientCanceled = true
			}
			break
		}

		dep := cand.Target.Deployment
		allowed, brState, isProbe, release := cfg.Breaker.AcquireAttempt(dep.ID, dep.State, dep.CooldownUntilMs)
		if !allowed {
			continue
		}

		res.AttemptCount++
		attemptIdx := res.AttemptCount
		start := nowFn()

		var attemptRes AttemptResult
		var elapsed time.Duration
		recorded := false
		func() {
			defer func() {
				if !recorded && release != nil {
					release()
				}
			}()
			attemptRes = fn(runCtx, attemptIdx, cand)
			elapsed = nowFn().Sub(start)
			if elapsed < 0 {
				elapsed = 0
			}
			// RecordOutcome transitions state AND clears probeInFlight under one lock before releasing.
			cfg.Breaker.RecordOutcome(dep.ID, breaker.AttemptOutcome{
				Kind:          attemptRes.Outcome,
				Latency:       elapsed,
				RetryAfterRaw: attemptRes.RetryAfterRaw,
				IsProbe:       isProbe,
			})
			recorded = true
		}()

		res.Traces = append(res.Traces, RouteAttemptTrace{
			AttemptIndex:  attemptIdx,
			DeploymentID:  dep.ID,
			DataRegion:    dep.DataRegion,
			BreakerState:  brState,
			HTTPStatus:    attemptRes.HTTPStatus,
			Outcome:       attemptRes.Outcome,
			DurationMs:    elapsed.Milliseconds(),
			RetryAfterRaw: attemptRes.RetryAfterRaw,
			StreamStarted: attemptRes.StreamStarted,
		})

		lastFlush = attemptRes.FlushFinal

		// Immutable No Mid-Stream Fallback Lockout (TP-FR-0103):
		// Once first SSE byte/frame reached the client, never attempt another candidate.
		if attemptRes.StreamStarted {
			res.StreamStarted = true
			res.Succeeded = attemptRes.Succeeded
			if attemptRes.Succeeded {
				res.Winner = cand
			}
			if !attemptRes.Succeeded && attemptRes.FlushFinal != nil {
				attemptRes.FlushFinal()
			}
			return res
		}

		if attemptRes.Succeeded {
			res.Succeeded = true
			res.Winner = cand
			return res
		}

		deadlineHit := deadlineReached(runCtx, cfg.RequestDeadline, nowFn())
		if deadlineHit {
			res.DeadlineExceeded = true
		} else if runCtx.Err() != nil {
			res.ClientCanceled = true
		}

		// Non-retryable outcomes (e.g. caller 400/422, client cancel) or deadline expiry stop immediately.
		if !attemptRes.Retryable || deadlineHit || runCtx.Err() != nil {
			if !res.ClientCanceled && attemptRes.FlushFinal != nil {
				attemptRes.FlushFinal()
			}
			return res
		}
	}

	if res.AttemptCount == 0 {
		if deadlineReached(runCtx, cfg.RequestDeadline, nowFn()) {
			res.DeadlineExceeded = true
			return res
		}
		if runCtx.Err() != nil {
			res.ClientCanceled = true
			return res
		}
		res.ExhaustedBreaker = true
		return res
	}
	if !res.ClientCanceled && lastFlush != nil {
		lastFlush()
	}
	return res
}
