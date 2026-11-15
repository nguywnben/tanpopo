// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package ratelimit implements Tanpopo's 3-dimensional Data Plane rate limiter
// (RPM, pre-estimated + post-reconciled TPM, and defer-guarded MaxConcurrentStreams)
// behind RateLimiterPort for standalone RAM and future Valkey Lua backends (TP-IMP-0019).
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Canonical rate limiting errors and HTTP wire codes.
var (
	ErrRateLimitExceeded        = errors.New("ratelimit: rate_limit_exceeded")
	ErrConcurrencyLimitExceeded = errors.New("ratelimit: concurrency_limit_exceeded")
	ErrInvalidScope             = errors.New("ratelimit: invalid limit scope")
)

// LimitScope uniquely isolates rate-limit and concurrency buckets by tenant + Virtual Key.
type LimitScope struct {
	OrgID        string
	ProjectID    string
	EnvID        string
	VirtualKeyID string
}

// Limits defines effective monotonic ceilings applied to a LimitScope.
// A nil pointer means unbounded along that dimension.
type Limits struct {
	RPMLimit             *int64
	TPMLimit             *int64
	MaxConcurrentStreams *int64
}

// CheckResult reports rate-limit headroom and retry guidance for X-RateLimit-* and Retry-After headers.
type CheckResult struct {
	Allowed           bool
	DenialCode        string // "rate_limit_exceeded" or "concurrency_limit_exceeded"
	RetryAfterSec     int
	HasRPM            bool
	RemainingRequests int64
	HasTPM            bool
	RemainingTokens   int64
	HasStreams        bool
	RemainingStreams  int64
}

// StreamLease represents an acquired concurrent stream slot guarded by sync.Once.
type StreamLease struct {
	once    sync.Once
	release func()
}

// Release frees the concurrency slot exactly once (safe across defers, panics, or retries).
func (l *StreamLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.release != nil {
			l.release()
		}
	})
}

// RateLimiterPort abstracts single-node RAM and distributed Valkey Lua limiters.
type RateLimiterPort interface {
	AllowRequest(ctx context.Context, scope LimitScope, estTokens int64, limits Limits) (CheckResult, error)
	AcquireStreamSlot(ctx context.Context, scope LimitScope, maxConcurrent int64) (*StreamLease, CheckResult, error)
	ReconcileTokens(ctx context.Context, scope LimitScope, estimatedTokens, actualTokens int64, limits Limits) error
	Remaining(ctx context.Context, scope LimitScope, limits Limits) (CheckResult, error)
}

type tokenBucketState struct {
	tokens      float64
	lastRefill  time.Time
	initialized bool
}

const (
	defaultIdleEvictAfter = 5 * time.Minute
	defaultPruneThreshold = 1024
)

type scopeState struct {
	mu               sync.Mutex
	rpm              tokenBucketState
	tpm              tokenBucketState
	activeStreams    int64
	lastSeenUnixNano atomic.Int64
}

// MemoryLimiter is a thread-safe, clock-injectable in-memory Token Bucket / GCRA
// rate limiter and concurrency semaphore implementing RateLimiterPort.
type MemoryLimiter struct {
	now func() time.Time

	mu     sync.Mutex
	scopes map[LimitScope]*scopeState
}

// Config configures a MemoryLimiter.
type Config struct {
	Now func() time.Time
}

// NewMemoryLimiter constructs an in-memory RateLimiterPort implementation.
func NewMemoryLimiter(cfg Config) *MemoryLimiter {
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryLimiter{
		now:    nowFn,
		scopes: make(map[LimitScope]*scopeState),
	}
}

func (m *MemoryLimiter) getOrCreateScope(scope LimitScope) (*scopeState, error) {
	if scope.OrgID == "" || scope.VirtualKeyID == "" {
		return nil, ErrInvalidScope
	}
	now := m.now()
	nowNano := now.UnixNano()

	m.mu.Lock()
	st, ok := m.scopes[scope]
	if ok {
		st.lastSeenUnixNano.Store(nowNano)
		m.mu.Unlock()
		return st, nil
	}
	var candidates map[LimitScope]*scopeState
	if len(m.scopes) >= defaultPruneThreshold {
		candidates = make(map[LimitScope]*scopeState, len(m.scopes))
		for ck, cst := range m.scopes {
			candidates[ck] = cst
		}
	}
	m.mu.Unlock()

	if len(candidates) > 0 {
		idleCutoff := int64(defaultIdleEvictAfter)
		var staleKeys []LimitScope
		for ck, cst := range candidates {
			cst.mu.Lock()
			ls := cst.lastSeenUnixNano.Load()
			if cst.activeStreams == 0 && ls > 0 && nowNano-ls >= idleCutoff {
				staleKeys = append(staleKeys, ck)
			}
			cst.mu.Unlock()
		}
		if len(staleKeys) > 0 {
			m.mu.Lock()
			for _, ck := range staleKeys {
				if cst := m.scopes[ck]; cst == candidates[ck] {
					cst.mu.Lock()
					ls := cst.lastSeenUnixNano.Load()
					if cst.activeStreams == 0 && ls > 0 && nowNano-ls >= idleCutoff {
						delete(m.scopes, ck)
					}
					cst.mu.Unlock()
				}
			}
			m.mu.Unlock()
		}
	}

	m.mu.Lock()
	st, ok = m.scopes[scope]
	if !ok {
		st = &scopeState{}
		st.lastSeenUnixNano.Store(nowNano)
		m.scopes[scope] = st
	} else {
		st.lastSeenUnixNano.Store(nowNano)
	}
	m.mu.Unlock()
	return st, nil
}

// AllowRequest atomically evaluates both RPM and pre-estimated TPM token buckets.
// On rejection along either axis, zero tokens are deducted from either bucket.
func (m *MemoryLimiter) AllowRequest(_ context.Context, scope LimitScope, estTokens int64, limits Limits) (CheckResult, error) {
	if estTokens < 0 {
		estTokens = 0
	}
	st, err := m.getOrCreateScope(scope)
	if err != nil {
		return CheckResult{}, err
	}

	now := m.now()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeenUnixNano.Store(now.UnixNano())

	var res CheckResult
	res.Allowed = true

	var rpmNeedWaitSec int
	var rpmOK = true
	if limits.RPMLimit != nil {
		res.HasRPM = true
		capRPM := float64(*limits.RPMLimit)
		refillBucket(&st.rpm, now, capRPM)
		if capRPM <= 0 {
			rpmOK = false
			rpmNeedWaitSec = 60
			res.RemainingRequests = 0
		} else if st.rpm.tokens < 1.0 {
			rpmOK = false
			deficit := 1.0 - st.rpm.tokens
			ratePerSec := capRPM / 60.0
			rpmNeedWaitSec = ceilSecondsClamped(deficit, ratePerSec)
			res.RemainingRequests = 0
		} else {
			res.RemainingRequests = int64(math.Floor(st.rpm.tokens - 1.0))
		}
	}

	var tpmNeedWaitSec int
	var tpmOK = true
	if limits.TPMLimit != nil {
		res.HasTPM = true
		capTPM := float64(*limits.TPMLimit)
		refillBucket(&st.tpm, now, capTPM)
		reqTok := float64(estTokens)
		if capTPM <= 0 {
			tpmOK = false
			tpmNeedWaitSec = 60
			res.RemainingTokens = 0
		} else if reqTok > capTPM {
			// Single request exceeds total TPM ceiling -> fast reject pre-egress (NEG-2).
			tpmOK = false
			deficit := reqTok - st.tpm.tokens
			if deficit < 1.0 {
				deficit = 1.0
			}
			ratePerSec := capTPM / 60.0
			tpmNeedWaitSec = max(60, ceilSecondsClamped(deficit, ratePerSec))
			res.RemainingTokens = max(0, int64(math.Floor(st.tpm.tokens)))
		} else if st.tpm.tokens < reqTok {
			tpmOK = false
			deficit := reqTok - st.tpm.tokens
			ratePerSec := capTPM / 60.0
			tpmNeedWaitSec = ceilSecondsClamped(deficit, ratePerSec)
			res.RemainingTokens = max(0, int64(math.Floor(st.tpm.tokens)))
		} else {
			res.RemainingTokens = max(0, int64(math.Floor(st.tpm.tokens-reqTok)))
		}
	}

	if limits.MaxConcurrentStreams != nil {
		res.HasStreams = true
		rem := *limits.MaxConcurrentStreams - st.activeStreams
		if rem < 0 {
			rem = 0
		}
		res.RemainingStreams = rem
	}

	if !rpmOK || !tpmOK {
		res.Allowed = false
		res.DenialCode = "rate_limit_exceeded"
		res.RetryAfterSec = max(1, max(rpmNeedWaitSec, tpmNeedWaitSec))
		if limits.RPMLimit != nil && rpmOK {
			res.RemainingRequests = max(0, int64(math.Floor(st.rpm.tokens)))
		}
		if limits.TPMLimit != nil && tpmOK {
			res.RemainingTokens = max(0, int64(math.Floor(st.tpm.tokens)))
		}
		return res, fmt.Errorf("%w: rpm_ok=%v tpm_ok=%v", ErrRateLimitExceeded, rpmOK, tpmOK)
	}

	// Both RPM and TPM passed -> commit deductions atomically.
	if limits.RPMLimit != nil {
		st.rpm.tokens -= 1.0
		res.RemainingRequests = max(0, int64(math.Floor(st.rpm.tokens)))
	}
	if limits.TPMLimit != nil && estTokens > 0 {
		st.tpm.tokens -= float64(estTokens)
		res.RemainingTokens = max(0, int64(math.Floor(st.tpm.tokens)))
	}
	return res, nil
}

func ceilSecondsClamped(deficit, ratePerSec float64) int {
	if deficit <= 0 {
		return 1
	}
	if ratePerSec <= 0 {
		return 60
	}
	wait := math.Ceil(deficit / ratePerSec)
	if math.IsNaN(wait) || math.IsInf(wait, 1) || wait >= float64(math.MaxInt32) {
		return math.MaxInt32
	}
	if wait < 1.0 {
		return 1
	}
	return int(wait)
}

// AcquireStreamSlot acquires one MaxConcurrentStreams slot guarded by sync.Once Release.
func (m *MemoryLimiter) AcquireStreamSlot(_ context.Context, scope LimitScope, maxConcurrent int64) (*StreamLease, CheckResult, error) {
	st, err := m.getOrCreateScope(scope)
	if err != nil {
		return nil, CheckResult{}, err
	}
	if maxConcurrent <= 0 {
		res := CheckResult{
			Allowed:          false,
			DenialCode:       "concurrency_limit_exceeded",
			RetryAfterSec:    1,
			HasStreams:       true,
			RemainingStreams: 0,
		}
		return nil, res, fmt.Errorf("%w: active=%d max=%d", ErrConcurrencyLimitExceeded, 0, maxConcurrent)
	}

	now := m.now()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeenUnixNano.Store(now.UnixNano())

	if st.activeStreams >= maxConcurrent {
		res := CheckResult{
			Allowed:          false,
			DenialCode:       "concurrency_limit_exceeded",
			RetryAfterSec:    1,
			HasStreams:       true,
			RemainingStreams: 0,
		}
		return nil, res, fmt.Errorf("%w: active=%d max=%d", ErrConcurrencyLimitExceeded, st.activeStreams, maxConcurrent)
	}

	st.activeStreams++
	rem := maxConcurrent - st.activeStreams
	if rem < 0 {
		rem = 0
	}
	lease := &StreamLease{
		release: func() {
			relNow := m.now()
			st.mu.Lock()
			if st.activeStreams > 0 {
				st.activeStreams--
			}
			st.lastSeenUnixNano.Store(relNow.UnixNano())
			st.mu.Unlock()
		},
	}
	return lease, CheckResult{
		Allowed:          true,
		HasStreams:       true,
		RemainingStreams: rem,
	}, nil
}

// ReconcileTokens refunds unused pre-estimated TPM tokens or debits overshoot
// once actual normalized usage is observed.
func (m *MemoryLimiter) ReconcileTokens(_ context.Context, scope LimitScope, estimatedTokens, actualTokens int64, limits Limits) error {
	if limits.TPMLimit == nil {
		return nil
	}
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	st, err := m.getOrCreateScope(scope)
	if err != nil {
		return err
	}
	now := m.now()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeenUnixNano.Store(now.UnixNano())

	capTPM := float64(*limits.TPMLimit)
	refillBucket(&st.tpm, now, capTPM)
	delta := float64(estimatedTokens - actualTokens)
	st.tpm.tokens += delta
	if st.tpm.tokens > capTPM {
		st.tpm.tokens = capTPM
	}
	return nil
}

// Remaining inspects current RPM, TPM, and concurrency headroom without consuming capacity.
func (m *MemoryLimiter) Remaining(_ context.Context, scope LimitScope, limits Limits) (CheckResult, error) {
	st, err := m.getOrCreateScope(scope)
	if err != nil {
		return CheckResult{}, err
	}
	now := m.now()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.lastSeenUnixNano.Store(now.UnixNano())

	res := CheckResult{Allowed: true}
	if limits.RPMLimit != nil {
		res.HasRPM = true
		capRPM := float64(*limits.RPMLimit)
		refillBucket(&st.rpm, now, capRPM)
		res.RemainingRequests = max(0, int64(math.Floor(st.rpm.tokens)))
	}
	if limits.TPMLimit != nil {
		res.HasTPM = true
		capTPM := float64(*limits.TPMLimit)
		refillBucket(&st.tpm, now, capTPM)
		res.RemainingTokens = max(0, int64(math.Floor(st.tpm.tokens)))
	}
	if limits.MaxConcurrentStreams != nil {
		res.HasStreams = true
		rem := *limits.MaxConcurrentStreams - st.activeStreams
		if rem < 0 {
			rem = 0
		}
		res.RemainingStreams = rem
	}
	return res, nil
}

func refillBucket(b *tokenBucketState, now time.Time, capacity float64) {
	if capacity < 0 {
		capacity = 0
	}
	if !b.initialized {
		b.tokens = capacity
		b.lastRefill = now
		b.initialized = true
		return
	}
	if now.After(b.lastRefill) && capacity > 0 {
		elapsedSec := now.Sub(b.lastRefill).Seconds()
		ratePerSec := capacity / 60.0
		b.tokens += elapsedSec * ratePerSec
		b.lastRefill = now
	} else if now.Before(b.lastRefill) {
		b.lastRefill = now
	}
	if b.tokens > capacity {
		b.tokens = capacity
	}
}
