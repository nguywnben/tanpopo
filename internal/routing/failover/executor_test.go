// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package failover_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/routing/failover"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func makeCandidate(id, region string) *resolver.RankedTarget {
	return &resolver.RankedTarget{
		Target: &snapshot.ResolvedTarget{
			Deployment: &snapshot.ModelDeploymentView{
				ID:         id,
				DataRegion: region,
				State:      breaker.StateActiveHealthy,
			},
		},
	}
}

func TestPreStreamFailoverAC1AC2AndMidStreamLockout(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	cands := []*resolver.RankedTarget{
		makeCandidate("dep_eu_primary", "EU"),
		makeCandidate("dep_eu_fallback", "EU"),
	}

	// AC-1 / CT-12: Primary returns 429 Retry-After: 12 -> immediate failover to Fallback, attempt_count=2.
	finalFlushed := 0
	execRes := failover.Execute(context.Background(), failover.Config{
		Breaker: cb,
		Now:     func() time.Time { return clk.Now() },
	}, cands, func(_ context.Context, attempt int, cand *resolver.RankedTarget) failover.AttemptResult {
		if cand.Target.Deployment.ID == "dep_eu_primary" {
			return failover.AttemptResult{
				Retryable:     true,
				Outcome:       breaker.OutcomeThrottle429,
				HTTPStatus:    http.StatusTooManyRequests,
				RetryAfterRaw: "12",
				FlushFinal:    func() { finalFlushed++ },
			}
		}
		clk.Advance(25 * time.Millisecond)
		return failover.AttemptResult{
			Succeeded:  true,
			Outcome:    breaker.OutcomeSuccess,
			HTTPStatus: http.StatusOK,
		}
	})

	if !execRes.Succeeded || execRes.AttemptCount != 2 || execRes.Winner.Target.Deployment.ID != "dep_eu_fallback" {
		t.Fatalf("unexpected AC-2 failover result: %+v", execRes)
	}
	if finalFlushed != 0 {
		t.Fatalf("expected 0 error flushes when fallback succeeds, got %d", finalFlushed)
	}
	if st := cb.PeekState("dep_eu_primary", breaker.StateActiveHealthy, nil); st.State != breaker.StateThrottledCooldown || st.CooldownUntil.Sub(clk.Now()) != 11975*time.Millisecond {
		t.Fatalf("expected dep_eu_primary parked in THROTTLED_COOLDOWN for 12s window, got %+v", st)
	}

	// Immutable No Mid-Stream Fallback Lockout (TP-FR-0103):
	// Even if OutcomeSystemFault occurs, StreamStarted=true locks out fallback attempts.
	midAttempts := 0
	midRes := failover.Execute(context.Background(), failover.Config{
		Breaker: cb,
		Now:     func() time.Time { return clk.Now() },
	}, []*resolver.RankedTarget{makeCandidate("dep_stream_1", "EU"), makeCandidate("dep_stream_2", "EU")},
		func(_ context.Context, _ int, _ *resolver.RankedTarget) failover.AttemptResult {
			midAttempts++
			return failover.AttemptResult{
				StreamStarted: true,
				Succeeded:     false,
				Retryable:     true,
				Outcome:       breaker.OutcomeSystemFault,
				HTTPStatus:    http.StatusBadGateway,
			}
		})
	if midAttempts != 1 || !midRes.StreamStarted || midRes.Succeeded {
		t.Fatalf("expected mid-stream abort with strictly 1 attempt, got attempts=%d res=%+v", midAttempts, midRes)
	}

	// Non-retryable 400 caller error aborts without retrying or tripping SM-04.
	callerFlushed := 0
	callerRes := failover.Execute(context.Background(), failover.Config{
		Breaker: cb,
		Now:     func() time.Time { return clk.Now() },
	}, []*resolver.RankedTarget{makeCandidate("dep_badreq_1", "EU"), makeCandidate("dep_badreq_2", "EU")},
		func(_ context.Context, _ int, _ *resolver.RankedTarget) failover.AttemptResult {
			return failover.AttemptResult{
				Retryable:  false,
				Outcome:    breaker.OutcomeCallerError,
				HTTPStatus: http.StatusBadRequest,
				FlushFinal: func() { callerFlushed++ },
			}
		})
	if callerRes.AttemptCount != 1 || callerFlushed != 1 {
		t.Fatalf("expected caller 400 to stop after 1 attempt and flush error once, got attempts=%d flushed=%d", callerRes.AttemptCount, callerFlushed)
	}
	if st := cb.PeekState("dep_badreq_1", breaker.StateActiveHealthy, nil); st.State != breaker.StateActiveHealthy {
		t.Fatalf("expected caller 400 to keep deployment ACTIVE_HEALTHY, got %+v", st)
	}
}

func TestFailoverExecutePanicReleasesHalfOpenProbe(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_panic", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	clk.Advance(31 * time.Second)

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic from AttemptFunc")
			}
		}()
		failover.Execute(context.Background(), failover.Config{
			Breaker: cb,
			Now:     func() time.Time { return clk.Now() },
		}, []*resolver.RankedTarget{makeCandidate("dep_panic", "EU")},
			func(_ context.Context, _ int, _ *resolver.RankedTarget) failover.AttemptResult {
				panic("simulated adapter panic")
			})
	}()

	st := cb.PeekState("dep_panic", breaker.StateActiveHealthy, nil)
	if st.State != breaker.StateHalfOpenProbing || st.ProbeInFlight || !st.Eligible {
		t.Fatalf("expected HALF_OPEN_PROBING probe slot freed after panic via defer release(), got %+v", st)
	}
}

func TestFailoverDeadlineExhaustionAndTraceFields(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	cands := []*resolver.RankedTarget{
		makeCandidate("dep_dl_1", "EU"),
		makeCandidate("dep_dl_2", "EU"),
	}

	// 1. RequestDeadline already expired on FakeClock -> 0 attempts, DeadlineExceeded=true, ExhaustedBreaker=false.
	expRes := failover.Execute(context.Background(), failover.Config{
		Breaker:         cb,
		RequestDeadline: clk.Now().Add(-1 * time.Second),
		Now:             func() time.Time { return clk.Now() },
	}, cands, func(_ context.Context, _ int, _ *resolver.RankedTarget) failover.AttemptResult {
		t.Fatal("AttemptFunc must not run when RequestDeadline already expired")
		return failover.AttemptResult{}
	})
	if !expRes.DeadlineExceeded || expRes.AttemptCount != 0 || expRes.ExhaustedBreaker || expRes.ClientCanceled {
		t.Fatalf("expected DeadlineExceeded with 0 attempts, got %+v", expRes)
	}

	// 2. RequestDeadline expires during Attempt 1 -> stops retries before Attempt 2, flushes final error, populates Traces.
	flushed := 0
	deadlineRes := failover.Execute(context.Background(), failover.Config{
		Breaker:         cb,
		RequestDeadline: clk.Now().Add(500 * time.Millisecond),
		Now:             func() time.Time { return clk.Now() },
	}, cands, func(hopCtx context.Context, idx int, cand *resolver.RankedTarget) failover.AttemptResult {
		if idx != 1 || cand.Target.Deployment.ID != "dep_dl_1" {
			t.Fatalf("unexpected hop %d on %s", idx, cand.Target.Deployment.ID)
		}
		if hopCtx.Err() != nil {
			t.Fatalf("expected active hopCtx relative to FakeClock remaining duration, got err=%v", hopCtx.Err())
		}
		clk.Advance(600 * time.Millisecond)
		return failover.AttemptResult{
			Retryable:     true,
			Outcome:       breaker.OutcomeThrottle429,
			HTTPStatus:    http.StatusTooManyRequests,
			RetryAfterRaw: "12",
			FlushFinal:    func() { flushed++ },
		}
	})
	if !deadlineRes.DeadlineExceeded || deadlineRes.AttemptCount != 1 || flushed != 1 || len(deadlineRes.Traces) != 1 {
		t.Fatalf("expected DeadlineExceeded after hop 1 with 1 flush and 1 trace, got res=%+v flushed=%d", deadlineRes, flushed)
	}
	tr := deadlineRes.Traces[0]
	if tr.AttemptIndex != 1 || tr.DeploymentID != "dep_dl_1" || tr.DataRegion != "EU" ||
		tr.BreakerState != breaker.StateActiveHealthy || tr.HTTPStatus != http.StatusTooManyRequests ||
		tr.Outcome != breaker.OutcomeThrottle429 || tr.DurationMs != 600 || tr.RetryAfterRaw != "12" || tr.StreamStarted {
		t.Fatalf("unexpected RouteAttemptTrace fields: %+v", tr)
	}

	// 3. ExhaustedBreaker when all candidates have open circuits.
	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_exh_1", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
		cb.RecordOutcome("dep_exh_2", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	exhRes := failover.Execute(context.Background(), failover.Config{
		Breaker: cb,
		Now:     func() time.Time { return clk.Now() },
	}, []*resolver.RankedTarget{makeCandidate("dep_exh_1", "EU"), makeCandidate("dep_exh_2", "EU")},
		func(_ context.Context, _ int, _ *resolver.RankedTarget) failover.AttemptResult {
			t.Fatal("AttemptFunc must not run when all circuits are open")
			return failover.AttemptResult{}
		})
	if !exhRes.ExhaustedBreaker || exhRes.AttemptCount != 0 || exhRes.DeadlineExceeded || exhRes.ClientCanceled {
		t.Fatalf("expected ExhaustedBreaker=true with 0 attempts, got %+v", exhRes)
	}
}
