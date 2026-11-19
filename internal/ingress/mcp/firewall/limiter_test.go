// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package firewall

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestLimiter(t *testing.T, now func() time.Time, limit, cap int) *Limiter {
	t.Helper()
	limiter, err := NewLimiter(LimiterConfig{Limit: limit, Window: time.Minute, MaxEntries: cap, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func TestLimiterBoundariesClassesAndKeyBounds(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newTestLimiter(t, func() time.Time { return now }, 2, 2)
	if !limiter.Allow(ReadOnly, "") {
		t.Fatal("read-only call should bypass high-risk limiter")
	}
	if limiter.Allow(Mutating, "") || limiter.Allow(Destructive, " \t") {
		t.Fatal("empty keys must be denied for high-risk calls")
	}
	if limiter.Allow(Mutating, string(make([]byte, maxKeyBytes+1))) {
		t.Fatal("oversized key must be denied")
	}
	keyAtLimit := newTestLimiter(t, func() time.Time { return now }, 1, 1)
	if !keyAtLimit.Allow(Mutating, string(make([]byte, maxKeyBytes))) {
		t.Fatal("key at the byte limit should be admitted")
	}
	if !limiter.Allow(Mutating, "tenant/key") || !limiter.Allow(Destructive, "tenant/key") {
		t.Fatal("calls up to the configured boundary should pass")
	}
	if limiter.Allow(Mutating, "tenant/key") {
		t.Fatal("call beyond the configured limit should be denied")
	}
	unknownLimiter := newTestLimiter(t, func() time.Time { return now }, 2, 1)
	if !unknownLimiter.Allow(Class("unrecognized"), "first") || unknownLimiter.Allow(Class("unrecognized"), "second") {
		t.Fatal("unknown classes must normalize to high-risk and capacity exhaustion must fail closed")
	}
}

func TestLimiterExpiryAndCapacityReclamation(t *testing.T) {
	now := time.Unix(200, 0)
	limiter := newTestLimiter(t, func() time.Time { return now }, 1, 1)
	if !limiter.Allow(Mutating, "first") || limiter.Allow(Mutating, "second") {
		t.Fatal("expected one active key and fail-closed capacity at cap")
	}
	now = now.Add(time.Minute - time.Nanosecond)
	if limiter.Allow(Mutating, "second") {
		t.Fatal("entry must remain active just before expiry")
	}
	now = now.Add(time.Nanosecond)
	if !limiter.Allow(Mutating, "second") {
		t.Fatal("expired entry should be reclaimed at the exact boundary")
	}
	if limiter.Allow(Mutating, "second") {
		t.Fatal("new window should enforce its configured limit")
	}
}

func TestLimiterConcurrentLimitIsAtomic(t *testing.T) {
	limiter := newTestLimiter(t, func() time.Time { return time.Unix(300, 0) }, 7, 4)
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Allow(Mutating, "shared") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 7 {
		t.Fatalf("concurrent admissions = %d, want exactly 7", got)
	}
}

func TestLimiterRejectsInvalidConfiguration(t *testing.T) {
	for _, cfg := range []LimiterConfig{
		{Limit: 0, Window: time.Second, MaxEntries: 1},
		{Limit: 1, Window: 0, MaxEntries: 1},
		{Limit: 1, Window: time.Second, MaxEntries: 0},
		{Limit: 1, Window: time.Second, MaxEntries: maxLimiterEntries + 1},
	} {
		if limiter, err := NewLimiter(cfg); err == nil || limiter != nil {
			t.Fatalf("NewLimiter(%+v) = (%v, %v), want configuration error", cfg, limiter, err)
		}
	}
}
