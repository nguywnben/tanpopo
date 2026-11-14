// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package clock provides a deterministic Clock abstraction with RealClock and
// a concurrency-safe FakeClock for zero-sleep unit and contract tests.
package clock

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

// Clock abstracts wall-clock reads and timer/ticker scheduling.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Until(t time.Time) time.Duration
	NewTimer(d time.Duration) Timer
	After(d time.Duration) <-chan time.Time
	AfterFunc(d time.Duration, f func()) Timer
	NewTicker(d time.Duration) Ticker
}

// Timer represents a single scheduled future event.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Ticker represents a recurring scheduled event stream.
type Ticker interface {
	C() <-chan time.Time
	Stop()
	Reset(d time.Duration)
}

// RealClock delegates directly to Go's standard time package.
type RealClock struct{}

// NewRealClock returns a Clock backed by real system time.
func NewRealClock() Clock {
	return RealClock{}
}

func (RealClock) Now() time.Time                  { return time.Now() }
func (RealClock) Since(t time.Time) time.Duration { return time.Since(t) }
func (RealClock) Until(t time.Time) time.Duration { return time.Until(t) }
func (RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

func (RealClock) NewTimer(d time.Duration) Timer {
	return &realTimer{t: time.NewTimer(d)}
}

func (RealClock) AfterFunc(d time.Duration, f func()) Timer {
	return &realTimer{t: time.AfterFunc(d, f)}
}

func (RealClock) NewTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time        { return r.t.C }
func (r *realTimer) Stop() bool                 { return r.t.Stop() }
func (r *realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

func (r *realTicker) C() <-chan time.Time   { return r.t.C }
func (r *realTicker) Stop()                 { r.t.Stop() }
func (r *realTicker) Reset(d time.Duration) { r.t.Reset(d) }

// FakeClock is a deterministic, concurrency-safe virtual Clock.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	nextSeq uint64
	events  eventHeap
	notify  chan struct{}
}

// DefaultFakeStart returns a deterministic UTC baseline timestamp used when zero time is passed.
func DefaultFakeStart() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

// NewFakeClock initializes a FakeClock starting at initial (or DefaultFakeStart if zero).
func NewFakeClock(initial time.Time) *FakeClock {
	if initial.IsZero() {
		initial = DefaultFakeStart()
	}
	return &FakeClock{
		now:    initial.UTC(),
		notify: make(chan struct{}),
	}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}

func (c *FakeClock) Until(t time.Time) time.Duration {
	return t.Sub(c.Now())
}

// Waiters returns the count of active pending timers and tickers registered on the clock.
func (c *FakeClock) Waiters() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// AwaitWaiters blocks until at least minWaiters active timers/tickers exist or ctx is canceled.
func (c *FakeClock) AwaitWaiters(ctx context.Context, minWaiters int) error {
	for {
		c.mu.Lock()
		if len(c.events) >= minWaiters {
			c.mu.Unlock()
			return nil
		}
		ch := c.notify
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

func (c *FakeClock) broadcastWaiterChangeLocked() {
	close(c.notify)
	c.notify = make(chan struct{})
}

// Advance moves the virtual clock forward by d and fires all timers/tickers whose
// deadlines are <= now+d in chronological order without holding internal locks during callbacks.
func (c *FakeClock) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	target := c.now.Add(d)

	for len(c.events) > 0 {
		nextEv := c.events[0]
		if nextEv.deadline.After(target) {
			break
		}
		ev := heap.Pop(&c.events).(*fakeEvent)
		c.now = ev.deadline

		if ev.isTicker {
			// Skip intermediate missed ticks in O(1) while respecting earlier timers so c.now stays monotonic.
			deliverAt := ev.deadline
			capTime := target
			if len(c.events) > 0 && c.events[0].deadline.Before(capTime) {
				capTime = c.events[0].deadline
			}
			if ev.period > 0 && capTime.After(ev.deadline) {
				missed := int64(capTime.Sub(ev.deadline) / ev.period)
				if missed > 0 {
					deliverAt = ev.deadline.Add(time.Duration(missed) * ev.period)
				}
			}
			ev.deadline = deliverAt.Add(ev.period)
			ev.seq = c.nextSeq
			c.nextSeq++
			heap.Push(&c.events, ev)
			c.broadcastWaiterChangeLocked()
			ch := ev.ch
			c.mu.Unlock()

			select {
			case ch <- deliverAt:
			default:
			}
			c.mu.Lock()
			continue
		}

		ev.active = false
		c.broadcastWaiterChangeLocked()
		fn := ev.fn
		ch := ev.ch
		fireAt := ev.deadline
		c.mu.Unlock()

		if fn != nil {
			go fn()
		} else if ch != nil {
			select {
			case ch <- fireAt:
			default:
			}
		}

		c.mu.Lock()
	}

	if target.After(c.now) {
		c.now = target
	}
	c.mu.Unlock()
}

// Set jumps the virtual clock forward to target using Advance if target is after Now.
func (c *FakeClock) Set(target time.Time) {
	c.mu.Lock()
	diff := target.Sub(c.now)
	c.mu.Unlock()
	if diff > 0 {
		c.Advance(diff)
	}
}

func (c *FakeClock) NewTimer(d time.Duration) Timer {
	ch := make(chan time.Time, 1)
	ev := &fakeEvent{
		clock: c,
		ch:    ch,
	}
	c.scheduleTimer(ev, d)
	return ev
}

func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	return c.NewTimer(d).C()
}

func (c *FakeClock) AfterFunc(d time.Duration, f func()) Timer {
	ev := &fakeEvent{
		clock: c,
		fn:    f,
	}
	c.scheduleTimer(ev, d)
	return ev
}

func (c *FakeClock) NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		panic("clock: non-positive interval for NewTicker")
	}
	c.mu.Lock()
	ev := &fakeEvent{
		clock:    c,
		isTicker: true,
		period:   d,
		deadline: c.now.Add(d),
		active:   true,
		seq:      c.nextSeq,
		ch:       make(chan time.Time, 1),
	}
	c.nextSeq++
	heap.Push(&c.events, ev)
	c.broadcastWaiterChangeLocked()
	c.mu.Unlock()
	return &fakeTicker{ev: ev}
}

func (c *FakeClock) scheduleTimer(ev *fakeEvent, d time.Duration) {
	c.mu.Lock()
	if d <= 0 {
		now := c.now
		fn := ev.fn
		ch := ev.ch
		ev.active = false
		c.mu.Unlock()
		if fn != nil {
			go fn()
		} else if ch != nil {
			select {
			case ch <- now:
			default:
			}
		}
		return
	}
	ev.deadline = c.now.Add(d)
	ev.seq = c.nextSeq
	ev.active = true
	c.nextSeq++
	heap.Push(&c.events, ev)
	c.broadcastWaiterChangeLocked()
	c.mu.Unlock()
}

type fakeEvent struct {
	clock    *FakeClock
	deadline time.Time
	seq      uint64
	index    int
	active   bool
	isTicker bool
	period   time.Duration
	ch       chan time.Time
	fn       func()
}

func (e *fakeEvent) C() <-chan time.Time {
	return e.ch
}

func (e *fakeEvent) Stop() bool {
	c := e.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	if !e.active {
		return false
	}
	heap.Remove(&c.events, e.index)
	e.active = false
	c.broadcastWaiterChangeLocked()
	return true
}

func (e *fakeEvent) Reset(d time.Duration) bool {
	c := e.clock
	c.mu.Lock()
	wasActive := e.active
	if e.active {
		heap.Remove(&c.events, e.index)
		e.active = false
	}
	if d <= 0 {
		now := c.now
		fn := e.fn
		ch := e.ch
		c.broadcastWaiterChangeLocked()
		c.mu.Unlock()
		if fn != nil {
			go fn()
		} else if ch != nil {
			select {
			case ch <- now:
			default:
			}
		}
		return wasActive
	}
	e.deadline = c.now.Add(d)
	e.seq = c.nextSeq
	e.active = true
	c.nextSeq++
	heap.Push(&c.events, e)
	c.broadcastWaiterChangeLocked()
	c.mu.Unlock()
	return wasActive
}

type fakeTicker struct {
	ev *fakeEvent
}

func (t *fakeTicker) C() <-chan time.Time {
	return t.ev.ch
}

func (t *fakeTicker) Stop() {
	t.ev.Stop()
}

func (t *fakeTicker) Reset(d time.Duration) {
	if d <= 0 {
		panic("clock: non-positive interval for Ticker.Reset")
	}
	c := t.ev.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.ev.active {
		heap.Remove(&c.events, t.ev.index)
	}
	t.ev.period = d
	t.ev.deadline = c.now.Add(d)
	t.ev.seq = c.nextSeq
	t.ev.active = true
	c.nextSeq++
	heap.Push(&c.events, t.ev)
	c.broadcastWaiterChangeLocked()
}

type eventHeap []*fakeEvent

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	if !h[i].deadline.Equal(h[j].deadline) {
		return h[i].deadline.Before(h[j].deadline)
	}
	return h[i].seq < h[j].seq
}
func (h eventHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *eventHeap) Push(x any) {
	ev := x.(*fakeEvent)
	ev.index = len(*h)
	*h = append(*h, ev)
}
func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	ev := old[n-1]
	old[n-1] = nil
	ev.index = -1
	*h = old[:n-1]
	return ev
}
