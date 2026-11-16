// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package breaker_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/routing/breaker"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func TestSM04CircuitBreakerCooldownAndHalfOpenRecovery(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	// AC-2 / CT-12: 429 with Retry-After: 12 parks deployment in THROTTLED_COOLDOWN for 12s.
	cb.RecordOutcome("dep_a", breaker.AttemptOutcome{
		Kind:          breaker.OutcomeThrottle429,
		RetryAfterRaw: "12",
	})
	st := cb.PeekState("dep_a", breaker.StateActiveHealthy, nil)
	if st.State != breaker.StateThrottledCooldown || st.Eligible {
		t.Fatalf("expected THROTTLED_COOLDOWN ineligible, got %+v", st)
	}

	clk.Advance(11 * time.Second)
	if ok, s, _, _ := cb.AcquireAttempt("dep_a", breaker.StateActiveHealthy, nil); ok || s != breaker.StateThrottledCooldown {
		t.Fatalf("expected still throttled at 11s, got ok=%v state=%s", ok, s)
	}

	clk.Advance(1 * time.Second)
	ok, s, _, rel := cb.AcquireAttempt("dep_a", breaker.StateActiveHealthy, nil)
	if !ok || s != breaker.StateActiveHealthy {
		t.Fatalf("expected cooldown expiry at 12s to re-enable deployment, got ok=%v state=%s", ok, s)
	}
	rel()

	// Trip to CIRCUIT_OPEN_UNHEALTHY via 5 consecutive system faults.
	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_a", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	st = cb.PeekState("dep_a", breaker.StateActiveHealthy, nil)
	if st.State != breaker.StateCircuitOpenUnhealthy || st.Eligible {
		t.Fatalf("expected CIRCUIT_OPEN_UNHEALTHY after 5 faults, got %+v", st)
	}

	// Advancing 31s advances CIRCUIT_OPEN_UNHEALTHY -> HALF_OPEN_PROBING admitting strictly 1 probe.
	clk.Advance(31 * time.Second)
	ok1, s1, isProbe1, rel1 := cb.AcquireAttempt("dep_a", breaker.StateActiveHealthy, nil)
	if !ok1 || s1 != breaker.StateHalfOpenProbing || !isProbe1 {
		t.Fatalf("expected single probe admitted at 31s, got ok=%v s=%s probe=%v", ok1, s1, isProbe1)
	}
	// Concurrent second request must be denied while probe is in flight.
	if ok2, s2, _, _ := cb.AcquireAttempt("dep_a", breaker.StateActiveHealthy, nil); ok2 || s2 != breaker.StateHalfOpenProbing {
		t.Fatalf("expected concurrent second probe denied, got ok=%v state=%s", ok2, s2)
	}

	// 1 successful probe restores ACTIVE_HEALTHY (TP-IMP-0020 AC-2 default ProbeSuccessRequired=1).
	cb.RecordOutcome("dep_a", breaker.AttemptOutcome{
		Kind:    breaker.OutcomeSuccess,
		Latency: 42 * time.Millisecond,
		IsProbe: isProbe1,
	})
	rel1()
	st = cb.PeekState("dep_a", breaker.StateActiveHealthy, nil)
	if st.State != breaker.StateActiveHealthy || !st.Eligible {
		t.Fatalf("expected ACTIVE_HEALTHY after 1 successful probe, got %+v", st)
	}
	if lat, has := cb.EWMALatency("dep_a"); !has || lat != 42*time.Millisecond {
		t.Fatalf("expected EWMA 42ms, got %v (%v)", lat, has)
	}
}

func TestSM04HalfOpenTwoProbeModeAndFailureReopen(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		ProbeSuccessRequired: 2,
		Now:                  func() time.Time { return clk.Now() },
		JitterFn:             func(base time.Duration) time.Duration { return base },
	})

	// 10 samples in 60s window with 5 failures -> ratio 0.50 -> trip!
	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_ratio", breaker.AttemptOutcome{Kind: breaker.OutcomeSuccess, Latency: 20 * time.Millisecond})
	}
	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_ratio", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	if st := cb.PeekState("dep_ratio", breaker.StateActiveHealthy, nil); st.State != breaker.StateCircuitOpenUnhealthy {
		t.Fatalf("expected CIRCUIT_OPEN_UNHEALTHY on 50%% ratio at 10 samples, got %+v", st)
	}

	clk.Advance(31 * time.Second)
	ok1, _, isP1, _ := cb.AcquireAttempt("dep_ratio", breaker.StateActiveHealthy, nil)
	if !ok1 || !isP1 {
		t.Fatal("expected probe 1 admitted")
	}
	cb.RecordOutcome("dep_ratio", breaker.AttemptOutcome{Kind: breaker.OutcomeSuccess, IsProbe: true, Latency: 25 * time.Millisecond})
	if st := cb.PeekState("dep_ratio", breaker.StateActiveHealthy, nil); st.State != breaker.StateHalfOpenProbing || !st.Eligible {
		t.Fatalf("expected still HALF_OPEN_PROBING after 1/2 probes, got %+v", st)
	}

	// Probe failure immediately re-opens circuit for 30s.
	ok2, _, isP2, _ := cb.AcquireAttempt("dep_ratio", breaker.StateActiveHealthy, nil)
	if !ok2 || !isP2 {
		t.Fatal("expected probe 2 admitted")
	}
	cb.RecordOutcome("dep_ratio", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault, IsProbe: true})
	if st := cb.PeekState("dep_ratio", breaker.StateActiveHealthy, nil); st.State != breaker.StateCircuitOpenUnhealthy {
		t.Fatalf("expected immediate re-open on failed probe, got %+v", st)
	}
}

func TestSM04CallerErrorsNeverTripBreakerAndExponentialCooldown(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	// NEG-2: 100 caller 400/422 errors leave SM-04 strictly ACTIVE_HEALTHY.
	for i := 0; i < 100; i++ {
		cb.RecordOutcome("dep_caller", breaker.AttemptOutcome{Kind: breaker.OutcomeCallerError})
	}
	if st := cb.PeekState("dep_caller", breaker.StateActiveHealthy, nil); st.State != breaker.StateActiveHealthy || !st.Eligible {
		t.Fatalf("caller 400 errors must never trip SM-04 breaker, got %+v", st)
	}

	// Retry-After absent -> live AcquireAttempt + RecordOutcome(429) follows 2s -> 4s -> 8s; OutcomeSuccess resets to 2s.
	for idx, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second} {
		allowed, _, _, rel := cb.AcquireAttempt("dep_exp", breaker.StateActiveHealthy, nil)
		if !allowed {
			t.Fatalf("attempt %d: expected AcquireAttempt allowed after cooldown expiry", idx)
		}
		cb.RecordOutcome("dep_exp", breaker.AttemptOutcome{Kind: breaker.OutcomeThrottle429})
		rel()
		if st := cb.PeekState("dep_exp", breaker.StateActiveHealthy, nil); st.CooldownUntil.Sub(clk.Now()) != want {
			t.Fatalf("attempt %d: expected %v fallback cooldown, got %v", idx, want, st.CooldownUntil.Sub(clk.Now()))
		}
		clk.Advance(want)
	}
	allowed, _, _, rel := cb.AcquireAttempt("dep_exp", breaker.StateActiveHealthy, nil)
	if !allowed {
		t.Fatal("expected AcquireAttempt allowed after 8s cooldown expiry")
	}
	cb.RecordOutcome("dep_exp", breaker.AttemptOutcome{Kind: breaker.OutcomeSuccess, Latency: 10 * time.Millisecond})
	rel()
	allowed, _, _, rel = cb.AcquireAttempt("dep_exp", breaker.StateActiveHealthy, nil)
	if !allowed {
		t.Fatal("expected AcquireAttempt allowed after success reset")
	}
	cb.RecordOutcome("dep_exp", breaker.AttemptOutcome{Kind: breaker.OutcomeThrottle429})
	rel()
	if st := cb.PeekState("dep_exp", breaker.StateActiveHealthy, nil); st.CooldownUntil.Sub(clk.Now()) != 2*time.Second {
		t.Fatalf("expected 2s fallback cooldown after success reset, got %v", st.CooldownUntil.Sub(clk.Now()))
	}
	cb.RecordOutcome("dep_clamp", breaker.AttemptOutcome{Kind: breaker.OutcomeThrottle429, RetryAfterRaw: "999"})
	if st := cb.PeekState("dep_clamp", breaker.StateActiveHealthy, nil); st.CooldownUntil.Sub(clk.Now()) != 120*time.Second {
		t.Fatalf("expected 120s clamp, got %v", st.CooldownUntil.Sub(clk.Now()))
	}
}

func TestSM04HalfOpenConcurrencyRaceSafety(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})
	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_race", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	clk.Advance(31 * time.Second)

	var admitted atomic.Int32
	hold := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			ready.Wait()
			if ok, _, _, rel := cb.AcquireAttempt("dep_race", breaker.StateActiveHealthy, nil); ok {
				admitted.Add(1)
				<-hold
				rel()
			}
		}()
	}
	time.Sleep(10 * time.Millisecond)
	close(hold)
	wg.Wait()
	if admitted.Load() != 1 {
		t.Fatalf("expected strictly 1 probe admitted under race, got %d", admitted.Load())
	}
}

func TestSM04RetryAfterLargeIntegerClamp(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	for _, raw := range []string{"10000000000", "9223372036854775807"} {
		depID := "dep_overflow_" + raw
		cb.RecordOutcome(depID, breaker.AttemptOutcome{
			Kind:          breaker.OutcomeThrottle429,
			RetryAfterRaw: raw,
		})
		st := cb.PeekState(depID, breaker.StateActiveHealthy, nil)
		if st.State != breaker.StateThrottledCooldown {
			t.Fatalf("raw=%s: expected THROTTLED_COOLDOWN, got %+v", raw, st)
		}
		if got := st.CooldownUntil.Sub(clk.Now()); got != 120*time.Second {
			t.Fatalf("raw=%s: expected cooldown clamped to 120s max, got %v", raw, got)
		}
	}
}

func TestSM04HalfOpenProbeSlotReleasedOnCallerOrCancel(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	for i := 0; i < 5; i++ {
		cb.RecordOutcome("dep_probe_rel", breaker.AttemptOutcome{Kind: breaker.OutcomeSystemFault})
	}
	clk.Advance(31 * time.Second)

	// Probe 1 receives CallerError -> circuit stays HALF_OPEN_PROBING and clears probeInFlight.
	ok1, s1, isP1, rel1 := cb.AcquireAttempt("dep_probe_rel", breaker.StateActiveHealthy, nil)
	if !ok1 || s1 != breaker.StateHalfOpenProbing || !isP1 {
		t.Fatalf("expected probe 1 admitted, got ok=%v s=%s isP=%v", ok1, s1, isP1)
	}
	cb.RecordOutcome("dep_probe_rel", breaker.AttemptOutcome{Kind: breaker.OutcomeCallerError, IsProbe: isP1})
	rel1()

	// Probe 2 receives ClientCanceled -> still eligible for next probe without reopening.
	ok2, s2, isP2, rel2 := cb.AcquireAttempt("dep_probe_rel", breaker.StateActiveHealthy, nil)
	if !ok2 || s2 != breaker.StateHalfOpenProbing || !isP2 {
		t.Fatalf("expected probe 2 admitted after caller error, got ok=%v s=%s isP=%v", ok2, s2, isP2)
	}
	cb.RecordOutcome("dep_probe_rel", breaker.AttemptOutcome{Kind: breaker.OutcomeClientCanceled, IsProbe: isP2})
	rel2()

	// Probe 3 succeeds -> restores ACTIVE_HEALTHY.
	ok3, _, isP3, rel3 := cb.AcquireAttempt("dep_probe_rel", breaker.StateActiveHealthy, nil)
	if !ok3 || !isP3 {
		t.Fatal("expected probe 3 admitted after client cancel")
	}
	cb.RecordOutcome("dep_probe_rel", breaker.AttemptOutcome{Kind: breaker.OutcomeSuccess, IsProbe: isP3, Latency: 15 * time.Millisecond})
	rel3()
	if st := cb.PeekState("dep_probe_rel", breaker.StateActiveHealthy, nil); st.State != breaker.StateActiveHealthy {
		t.Fatalf("expected ACTIVE_HEALTHY after probe 3 success, got %+v", st)
	}
}

func TestSM04SnapshotCircuitOpenWithoutCooldownSeedsHalfOpen(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	cb := breaker.New(breaker.Config{
		Now:      func() time.Time { return clk.Now() },
		JitterFn: func(base time.Duration) time.Duration { return base },
	})

	// Past cooldown_until_ms and nil cooldown_until_ms on CIRCUIT_OPEN_UNHEALTHY MUST enter HALF_OPEN_PROBING (1 probe max).
	past := clk.Now().Add(-5 * time.Second).UnixMilli()
	stNil := cb.PeekState("dep_open_nil", breaker.StateCircuitOpenUnhealthy, nil)
	if stNil.State != breaker.StateHalfOpenProbing || !stNil.Eligible {
		t.Fatalf("expected HALF_OPEN_PROBING on nil cooldown_until_ms, got %+v", stNil)
	}
	ok1, s1, isP1, rel1 := cb.AcquireAttempt("dep_open_past", breaker.StateCircuitOpenUnhealthy, &past)
	if !ok1 || s1 != breaker.StateHalfOpenProbing || !isP1 {
		t.Fatalf("expected single HALF_OPEN_PROBING slot for expired snapshot circuit_open, got ok=%v s=%s probe=%v", ok1, s1, isP1)
	}
	if ok2, s2, _, _ := cb.AcquireAttempt("dep_open_past", breaker.StateCircuitOpenUnhealthy, &past); ok2 || s2 != breaker.StateHalfOpenProbing {
		t.Fatalf("expected concurrent second attempt blocked in HALF_OPEN_PROBING, got ok=%v state=%s", ok2, s2)
	}
	rel1()
}
