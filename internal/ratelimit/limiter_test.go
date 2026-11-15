// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package ratelimit_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/ratelimit"
	"github.com/nguywnben/tanpopo/internal/testkit/clock"
)

func int64Ptr(v int64) *int64 { return &v }

func TestMemoryLimiterRPMWindowAndRefillAC1(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	lim := ratelimit.NewMemoryLimiter(ratelimit.Config{
		Now: clk.Now,
	})
	scope := ratelimit.LimitScope{
		OrgID:        "org_1",
		ProjectID:    "prj_1",
		EnvID:        "env_1",
		VirtualKeyID: "vk_1",
	}
	limits := ratelimit.Limits{
		RPMLimit: int64Ptr(10),
	}

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		res, err := lim.AllowRequest(ctx, scope, 50, limits)
		if err != nil {
			t.Fatalf("request #%d unexpectedly rejected: %v", i+1, err)
		}
		if !res.Allowed || !res.HasRPM || res.RemainingRequests != int64(9-i) {
			t.Fatalf("request #%d unexpected result: %+v", i+1, res)
		}
	}

	// #11 within the same second -> rejected with rate_limit_exceeded and RetryAfter > 0.
	rej, err := lim.AllowRequest(ctx, scope, 50, limits)
	if !errors.Is(err, ratelimit.ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded on #11, got: %v", err)
	}
	if rej.Allowed || rej.DenialCode != "rate_limit_exceeded" || rej.RetryAfterSec <= 0 {
		t.Fatalf("unexpected rejection payload: %+v", rej)
	}

	// Advance FakeClock by 12 seconds -> 10 RPM / 60s * 12s = 2 requests refilled.
	clk.Advance(12 * time.Second)
	rem, err := lim.Remaining(ctx, scope, limits)
	if err != nil || rem.RemainingRequests != 2 {
		t.Fatalf("expected 2 requests remaining after 12s refill, got rem=%+v err=%v", rem, err)
	}
	if _, err := lim.AllowRequest(ctx, scope, 50, limits); err != nil {
		t.Fatalf("expected request after clock advance to succeed: %v", err)
	}
}

func TestMemoryLimiterTPMBurstAndReconcileNEG2(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	lim := ratelimit.NewMemoryLimiter(ratelimit.Config{Now: clk.Now})
	scope := ratelimit.LimitScope{OrgID: "org_1", VirtualKeyID: "vk_tpm"}
	limits := ratelimit.Limits{
		RPMLimit: int64Ptr(10),
		TPMLimit: int64Ptr(1000),
	}
	ctx := context.Background()

	// NEG-2: Single oversized request (> TPMLimit) rejected atomically without consuming RPM.
	rej, err := lim.AllowRequest(ctx, scope, 1500, limits)
	if !errors.Is(err, ratelimit.ErrRateLimitExceeded) || rej.RetryAfterSec <= 0 {
		t.Fatalf("expected TPM burst rejection, got res=%+v err=%v", rej, err)
	}
	rem, err := lim.Remaining(ctx, scope, limits)
	if err != nil || rem.RemainingRequests != 10 || rem.RemainingTokens != 1000 {
		t.Fatalf("atomic rejection must not consume RPM or TPM; got %+v err=%v", rem, err)
	}

	// Reserve 600 estimated tokens, then reconcile to 200 actual -> refund 400.
	res, err := lim.AllowRequest(ctx, scope, 600, limits)
	if err != nil || res.RemainingTokens != 400 {
		t.Fatalf("expected 400 remaining tokens after 600 estimate, got res=%+v err=%v", res, err)
	}
	if err := lim.ReconcileTokens(ctx, scope, 600, 200, limits); err != nil {
		t.Fatal(err)
	}
	rem, _ = lim.Remaining(ctx, scope, limits)
	if rem.RemainingTokens != 800 {
		t.Fatalf("expected 800 remaining tokens after refund, got %d", rem.RemainingTokens)
	}

	// Next request reserving 800 succeeds, then overshoot actual=950 pushes bucket to -150 debt.
	if _, err := lim.AllowRequest(ctx, scope, 800, limits); err != nil {
		t.Fatalf("expected 800-token request to pass, got %v", err)
	}
	if err := lim.ReconcileTokens(ctx, scope, 800, 950, limits); err != nil {
		t.Fatal(err)
	}
	if _, err := lim.AllowRequest(ctx, scope, 100, limits); !errors.Is(err, ratelimit.ErrRateLimitExceeded) {
		t.Fatalf("expected subsequent request blocked by overshoot debt, got %v", err)
	}
}

func TestMemoryLimiterConcurrencySemaphoreAC2AndDoubleReleaseSafety(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	lim := ratelimit.NewMemoryLimiter(ratelimit.Config{Now: clk.Now})
	scope := ratelimit.LimitScope{OrgID: "org_1", VirtualKeyID: "vk_streams"}
	ctx := context.Background()

	l1, r1, err := lim.AcquireStreamSlot(ctx, scope, 2)
	if err != nil || r1.RemainingStreams != 1 {
		t.Fatalf("slot 1 failed: r=%+v err=%v", r1, err)
	}
	l2, r2, err := lim.AcquireStreamSlot(ctx, scope, 2)
	if err != nil || r2.RemainingStreams != 0 {
		t.Fatalf("slot 2 failed: r=%+v err=%v", r2, err)
	}

	// #3 fails with concurrency_limit_exceeded.
	_, r3, err := lim.AcquireStreamSlot(ctx, scope, 2)
	if !errors.Is(err, ratelimit.ErrConcurrencyLimitExceeded) || r3.DenialCode != "concurrency_limit_exceeded" || r3.RetryAfterSec < 1 {
		t.Fatalf("expected ErrConcurrencyLimitExceeded on #3, got res=%+v err=%v", r3, err)
	}

	// Double release l1 must only free 1 slot via sync.Once (never underflow).
	l1.Release()
	l1.Release()

	rem, _ := lim.Remaining(ctx, scope, ratelimit.Limits{MaxConcurrentStreams: int64Ptr(2)})
	if rem.RemainingStreams != 1 {
		t.Fatalf("expected exactly 1 remaining stream after double-release of l1, got %d", rem.RemainingStreams)
	}

	l3, _, err := lim.AcquireStreamSlot(ctx, scope, 2)
	if err != nil {
		t.Fatalf("expected slot 3 to succeed immediately after release: %v", err)
	}
	l2.Release()
	l3.Release()
	(*ratelimit.StreamLease)(nil).Release()
}

func TestMemoryLimiterEdgeCasesAndConcurrencyRace(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	lim := ratelimit.NewMemoryLimiter(ratelimit.Config{Now: clk.Now})
	ctx := context.Background()

	if _, err := lim.AllowRequest(ctx, ratelimit.LimitScope{}, 1, ratelimit.Limits{}); !errors.Is(err, ratelimit.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope, got %v", err)
	}
	scope := ratelimit.LimitScope{OrgID: "org_race", VirtualKeyID: "vk_race"}
	if _, err := lim.AllowRequest(ctx, scope, -10, ratelimit.Limits{RPMLimit: int64Ptr(0)}); !errors.Is(err, ratelimit.ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded for RPM=0, got %v", err)
	}
	if _, res, err := lim.AcquireStreamSlot(ctx, scope, 0); !errors.Is(err, ratelimit.ErrConcurrencyLimitExceeded) || res.Allowed || res.DenialCode != "concurrency_limit_exceeded" {
		t.Fatalf("expected ErrConcurrencyLimitExceeded for maxConcurrent=0, got res=%+v err=%v", res, err)
	}
	if _, res, err := lim.AcquireStreamSlot(ctx, scope, -1); !errors.Is(err, ratelimit.ErrConcurrencyLimitExceeded) || res.Allowed {
		t.Fatalf("expected ErrConcurrencyLimitExceeded for maxConcurrent=-1, got res=%+v err=%v", res, err)
	}

	// Concurrency stress under -race.
	var active atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, _, err := lim.AcquireStreamSlot(ctx, scope, 4)
			if err != nil {
				return
			}
			defer lease.Release()
			cur := active.Add(1)
			if cur > 4 {
				t.Errorf("concurrent active streams exceeded cap 4: %d", cur)
			}
			_, _ = lim.AllowRequest(ctx, scope, 10, ratelimit.Limits{RPMLimit: int64Ptr(1000), TPMLimit: int64Ptr(100000), MaxConcurrentStreams: int64Ptr(4)})
			active.Add(-1)
		}()
	}
	wg.Wait()
	rem, _ := lim.Remaining(ctx, scope, ratelimit.Limits{MaxConcurrentStreams: int64Ptr(4)})
	if rem.RemainingStreams != 4 {
		t.Fatalf("expected 0 leaked stream slots after concurrent stress, got remaining=%d", rem.RemainingStreams)
	}
}

func TestMemoryLimiterRPMRejectPreservesRemainingTokensAndRetryAfterClamp(t *testing.T) {
	t.Parallel()
	clk := clock.NewFakeClock(time.Unix(1760000000, 0).UTC())
	lim := ratelimit.NewMemoryLimiter(ratelimit.Config{Now: clk.Now})
	ctx := context.Background()

	scope := ratelimit.LimitScope{OrgID: "org_sym", ProjectID: "prj_1", EnvID: "env_1", VirtualKeyID: "vk_sym"}
	limits := ratelimit.Limits{RPMLimit: int64Ptr(1), TPMLimit: int64Ptr(1000)}

	// 1st call succeeds: 0 RPM left, 900 TPM left.
	res1, err := lim.AllowRequest(ctx, scope, 100, limits)
	if err != nil || !res1.Allowed || res1.RemainingTokens != 900 {
		t.Fatalf("call 1 unexpected: res=%+v err=%v", res1, err)
	}

	// 2nd call fails on RPM while TPM has 900 headroom -> RemainingTokens MUST report 900 (not 800!).
	res2, err := lim.AllowRequest(ctx, scope, 100, limits)
	if !errors.Is(err, ratelimit.ErrRateLimitExceeded) || res2.Allowed {
		t.Fatalf("expected ErrRateLimitExceeded on call 2, got res=%+v err=%v", res2, err)
	}
	if res2.RemainingRequests != 0 || res2.RemainingTokens != 900 {
		t.Fatalf("expected RemainingRequests=0 RemainingTokens=900 when rejected on RPM, got %+v", res2)
	}

	// Extreme burst estTokens = 1<<62 against TPMLimit=10 must never overflow RetryAfterSec negative or collapse to 1.
	burstScope := ratelimit.LimitScope{OrgID: "org_sym", VirtualKeyID: "vk_burst"}
	res3, err := lim.AllowRequest(ctx, burstScope, 1<<62, ratelimit.Limits{TPMLimit: int64Ptr(10)})
	if !errors.Is(err, ratelimit.ErrRateLimitExceeded) || res3.Allowed {
		t.Fatalf("expected ErrRateLimitExceeded on extreme burst, got res=%+v err=%v", res3, err)
	}
	if res3.RetryAfterSec < 60 {
		t.Fatalf("expected bounded large positive RetryAfterSec (>= 60), got %d", res3.RetryAfterSec)
	}

	// Cover capTPM=0, ReconcileTokens clamp & Remaining over active stream scope.
	zeroTPMScope := ratelimit.LimitScope{OrgID: "org_sym", VirtualKeyID: "vk_ztpm"}
	if _, err := lim.AllowRequest(ctx, zeroTPMScope, 1, ratelimit.Limits{TPMLimit: int64Ptr(0)}); !errors.Is(err, ratelimit.ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded for TPMLimit=0, got %v", err)
	}
	if err := lim.ReconcileTokens(ctx, ratelimit.LimitScope{}, 10, 5, ratelimit.Limits{TPMLimit: int64Ptr(100)}); !errors.Is(err, ratelimit.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope on ReconcileTokens with empty scope, got %v", err)
	}
	_ = lim.ReconcileTokens(ctx, zeroTPMScope, -10, -5, ratelimit.Limits{TPMLimit: int64Ptr(100)})
	if _, err := lim.Remaining(ctx, ratelimit.LimitScope{}, ratelimit.Limits{}); !errors.Is(err, ratelimit.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope on Remaining with empty scope, got %v", err)
	}
	if _, _, err := lim.AcquireStreamSlot(ctx, ratelimit.LimitScope{}, 1); !errors.Is(err, ratelimit.ErrInvalidScope) {
		t.Fatalf("expected ErrInvalidScope on AcquireStreamSlot with empty scope, got %v", err)
	}
	if err := lim.ReconcileTokens(ctx, scope, 10, 5, ratelimit.Limits{}); err != nil {
		t.Fatalf("expected nil on ReconcileTokens when TPMLimit=nil, got %v", err)
	}
	if rem, err := lim.Remaining(ctx, scope, limits); err != nil || !rem.HasRPM || !rem.HasTPM {
		t.Fatalf("Remaining failed: rem=%+v err=%v", rem, err)
	}
}
