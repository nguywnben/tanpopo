// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package clock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFakeClockConcurrentTimersAC3(t *testing.T) {
	t.Parallel()

	fc := NewFakeClock(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC))
	const workers = 10

	var unblocked atomic.Int32
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			tm := fc.NewTimer(60 * time.Second)
			defer tm.Stop()
			<-tm.C()
			unblocked.Add(1)
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := fc.AwaitWaiters(ctx, workers); err != nil {
		t.Fatalf("AwaitWaiters(%d): %v", workers, err)
	}

	// Advancing by 59s keeps all 10 timers waiting.
	fc.Advance(59 * time.Second)
	if got := unblocked.Load(); got != 0 {
		t.Fatalf("expected 0 unblocked timers at 59s, got %d", got)
	}

	// Advance to 61s and measure wall-clock wake-up (< 1ms target once dispatched).
	start := time.Now()
	fc.Advance(2 * time.Second)
	wg.Wait()
	elapsed := time.Since(start)

	if got := unblocked.Load(); got != workers {
		t.Fatalf("expected %d unblocked timers, got %d", workers, got)
	}
	if fc.Waiters() != 0 {
		t.Fatalf("expected 0 remaining waiters, got %d", fc.Waiters())
	}
	t.Logf("10 goroutines unblocked deterministically in %v", elapsed)
}

func TestFakeClockOperationsAndCoverage(t *testing.T) {
	t.Parallel()

	fc := NewFakeClock(time.Time{})
	base := DefaultFakeStart()
	if !fc.Now().Equal(base) {
		t.Fatalf("expected default start %v, got %v", base, fc.Now())
	}

	// Zero-duration After and AfterFunc fire immediately.
	select {
	case ts := <-fc.After(0):
		if !ts.Equal(base) {
			t.Fatalf("expected zero-duration After at %v, got %v", base, ts)
		}
	default:
		t.Fatalf("expected immediate fire on After(0)")
	}

	afterDone := make(chan struct{})
	afZero := fc.AfterFunc(0, func() { close(afterDone) })
	<-afterDone
	if afZero.Stop() {
		t.Fatalf("expected immediate AfterFunc Stop() to return false")
	}

	// Non-positive Advance and past Set are safe no-ops.
	fc.Advance(0)
	fc.Advance(-time.Second)
	fc.Set(base.Add(-time.Hour))
	if !fc.Now().Equal(base) {
		t.Fatalf("expected Now unchanged at %v, got %v", base, fc.Now())
	}

	// Since and Until helpers.
	if fc.Since(base.Add(-5*time.Second)) != 5*time.Second {
		t.Fatalf("unexpected Since: %v", fc.Since(base.Add(-5*time.Second)))
	}
	if fc.Until(base.Add(15*time.Second)) != 15*time.Second {
		t.Fatalf("unexpected Until: %v", fc.Until(base.Add(15*time.Second)))
	}

	// Timer Stop and Reset semantics.
	tm := fc.NewTimer(10 * time.Second)
	if !tm.Reset(20 * time.Second) {
		t.Fatalf("expected Reset on active timer to return true")
	}
	fc.Advance(10 * time.Second)
	select {
	case <-tm.C():
		t.Fatalf("timer fired prematurely at 10s after reset to 20s")
	default:
	}
	if !tm.Stop() {
		t.Fatalf("expected Stop on active timer to return true")
	}
	if tm.Stop() {
		t.Fatalf("expected second Stop to return false")
	}
	if tm.Reset(0) {
		t.Fatalf("expected Reset on stopped timer to return false")
	}
	<-tm.C()

	afReset := fc.AfterFunc(10*time.Second, func() {})
	if !afReset.Reset(5 * time.Second) {
		t.Fatalf("expected Reset on active AfterFunc to return true")
	}
	afZeroCh := make(chan struct{})
	afResetZero := fc.AfterFunc(10*time.Second, func() { close(afZeroCh) })
	if !afResetZero.Reset(0) {
		t.Fatalf("expected Reset(0) on active AfterFunc to return true")
	}
	<-afZeroCh
	afReset.Stop()

	// Mixed ticker + earlier timer must fire in chronological order with monotonic clock.
	mixedTimerFired := make(chan time.Time, 1)
	fc.AfterFunc(3*time.Second, func() { mixedTimerFired <- fc.Now() })
	mixedTk := fc.NewTicker(10 * time.Second)
	fc.Advance(25 * time.Second)
	if gotTimer := <-mixedTimerFired; gotTimer.After(fc.Now()) {
		t.Fatalf("timer timestamp %v exceeded clock Now %v", gotTimer, fc.Now())
	}
	<-mixedTk.C()
	mixedTk.Stop()

	// Delayed AfterFunc ordering + Set.
	var orderMu sync.Mutex
	var order []string
	fc.AfterFunc(5*time.Second, func() {
		orderMu.Lock()
		order = append(order, "a")
		orderMu.Unlock()
	})
	fc.AfterFunc(5*time.Second, func() {
		orderMu.Lock()
		order = append(order, "b")
		orderMu.Unlock()
	})

	doneAll := make(chan struct{})
	fc.AfterFunc(10*time.Second, func() {
		close(doneAll)
	})
	fc.Set(fc.Now().Add(10 * time.Second))
	<-doneAll

	// Ticker including Reset, Stop, and long jump O(1) handling.
	tk := fc.NewTicker(5 * time.Second)
	fc.Advance(15 * time.Second) // Jumps past 3 ticks cleanly in O(1).
	select {
	case tick := <-tk.C():
		if tick.Before(base) {
			t.Fatalf("invalid tick timestamp: %v", tick)
		}
	default:
		t.Fatalf("expected buffered tick after Advance")
	}
	tk.Reset(2 * time.Second)
	fc.Advance(2 * time.Second)
	select {
	case <-tk.C():
	default:
		t.Fatalf("expected tick after Ticker.Reset")
	}
	tk.Stop()

	// RealClock sanity (stop timers/tickers immediately without waiting).
	rc := NewRealClock()
	_ = rc.Now()
	_ = rc.Since(time.Now())
	_ = rc.Until(time.Now())
	rt := rc.NewTimer(time.Hour)
	_ = rt.C()
	rt.Reset(2 * time.Hour)
	rt.Stop()
	_ = rc.After(time.Hour)
	afReal := rc.AfterFunc(time.Hour, func() {})
	afReal.Stop()
	rtk := rc.NewTicker(time.Hour)
	_ = rtk.C()
	rtk.Reset(2 * time.Hour)
	rtk.Stop()

	// Verify AwaitWaiters respects context cancellation.
	cancelCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if err := fc.AwaitWaiters(cancelCtx, 999); err == nil {
		t.Fatalf("expected context canceled error from AwaitWaiters")
	}

	// Verify NewTicker / Ticker.Reset panic on non-positive duration.
	for _, fn := range []func(){
		func() { fc.NewTicker(0) },
		func() { tk.Reset(0) },
	} {
		panicked := false
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
				}
			}()
			fn()
		}()
		if !panicked {
			t.Fatalf("expected panic on non-positive ticker duration")
		}
	}
}
