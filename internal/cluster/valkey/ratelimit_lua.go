// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package valkey

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguywnben/tanpopo/internal/ratelimit"
)

const (
	bucketWindowMs = int64(60_000)
	bucketTTLMs    = int64(120_000)
	streamTTLMs    = int64(300_000)
)

// RateLimiterConfig configures a Valkey Cluster RateLimiter implementing ratelimit.RateLimiterPort.
type RateLimiterConfig struct {
	Client                *Client
	EstimatedReplicaCount int64 // Used for Local Fallback Quotient (limit / replicas) during Valkey outages.
	Now                   func() time.Time
}

// RateLimiter implements ratelimit.RateLimiterPort using atomic single-slot Lua scripts
// under tp:v1:{<org_id>}:rl:* with conservative local token-bucket fallback on Valkey outages.
type RateLimiter struct {
	client   *Client
	replicas int64
	now      func() time.Time
	fallback *ratelimit.MemoryLimiter

	degraded          atomic.Bool
	fallbackCount     atomic.Int64
	lastErrMu         sync.RWMutex
	lastErr           error
}

// NewRateLimiter constructs a ValkeyRateLimiter backed by client and fallback replica hint.
func NewRateLimiter(cfg RateLimiterConfig) (*RateLimiter, error) {
	replicas := cfg.EstimatedReplicaCount
	if replicas < 1 {
		replicas = 1
	}
	nowFn := cfg.Now
	if nowFn == nil {
		if cfg.Client != nil && cfg.Client.now != nil {
			nowFn = cfg.Client.now
		} else {
			nowFn = time.Now
		}
	}
	return &RateLimiter{
		client:   cfg.Client,
		replicas: replicas,
		now:      nowFn,
		fallback: ratelimit.NewMemoryLimiter(ratelimit.Config{Now: nowFn}),
	}, nil
}

// IsDegraded reports whether the most recent Valkey operation fell back to local quota mode.
func (r *RateLimiter) IsDegraded() bool { return r != nil && r.degraded.Load() }

// DegradedFallbacks returns the count of requests served via local fallback during Valkey outages.
func (r *RateLimiter) DegradedFallbacks() int64 {
	if r == nil {
		return 0
	}
	return r.fallbackCount.Load()
}

// LastError returns the most recent Valkey failure observed, or nil when healthy.
func (r *RateLimiter) LastError() error {
	if r == nil {
		return nil
	}
	r.lastErrMu.RLock()
	defer r.lastErrMu.RUnlock()
	return r.lastErr
}

func (r *RateLimiter) recordHealthy() {
	r.degraded.Store(false)
	r.lastErrMu.Lock()
	r.lastErr = nil
	r.lastErrMu.Unlock()
}

func (r *RateLimiter) recordDegraded(err error) {
	r.degraded.Store(true)
	r.fallbackCount.Add(1)
	r.lastErrMu.Lock()
	r.lastErr = err
	r.lastErrMu.Unlock()
}

// Lua script atomically evaluating and deducting BOTH RPM and TPM buckets under {org_id}.
// KEYS: [1] rpm_key, [2] tpm_key
// ARGV: [1] now_ms, [2] rpm_limit, [3] has_rpm (0|1), [4] tpm_limit, [5] has_tpm (0|1), [6] est_tokens, [7] ttl_ms
// Returns: {allowed (0|1), rpm_rem_int, tpm_rem_int, retry_after_sec}
const luaAllowRequestScript = `
local rpmKey = KEYS[1]
local tpmKey = KEYS[2]
local nowMs = tonumber(ARGV[1])
local rpmCap = tonumber(ARGV[2])
local hasRPM = tonumber(ARGV[3]) == 1
local tpmCap = tonumber(ARGV[4])
local hasTPM = tonumber(ARGV[5]) == 1
local estTokens = tonumber(ARGV[6])
local ttlMs = tonumber(ARGV[7])

local function readBucket(key, cap, enabled)
	if not enabled then
		return 0, nowMs
	end
	if cap <= 0 then
		return 0, nowMs
	end
	local vals = redis.call('HMGET', key, 'tokens_milli', 'last_ms')
	local rawTok = vals[1]
	local rawLast = vals[2]
	if not rawTok or not rawLast then
		return cap * 1000, nowMs
	end
	local cur = tonumber(rawTok)
	local last = tonumber(rawLast)
	local elapsed = nowMs - last
	if elapsed < 0 then
		elapsed = 0
	end
	local refill = math.floor((elapsed * cap * 1000) / 60000)
	local nextTok = cur + refill
	local maxMilli = cap * 1000
	if nextTok > maxMilli then
		nextTok = maxMilli
	end
	return nextTok, nowMs
end

local rpmMilli, _ = readBucket(rpmKey, rpmCap, hasRPM)
local tpmMilli, _ = readBucket(tpmKey, tpmCap, hasTPM)

local retrySec = 0
if hasRPM then
	if rpmCap <= 0 then
		retrySec = 60
	elseif rpmMilli < 1000 then
		local needMilli = 1000 - rpmMilli
		local waitSec = math.ceil((needMilli * 60) / (rpmCap * 1000))
		if waitSec < 1 then waitSec = 1 end
		retrySec = waitSec
	end
end

if hasTPM and retrySec == 0 then
	local needTokMilli = estTokens * 1000
	if needTokMilli < 0 then needTokMilli = 0 end
	if tpmCap <= 0 then
		retrySec = 60
	elseif tpmMilli < needTokMilli then
		local deficit = needTokMilli - tpmMilli
		local waitSec = math.ceil((deficit * 60) / (tpmCap * 1000))
		if waitSec < 1 then waitSec = 1 end
		if waitSec > 60 then waitSec = 60 end
		retrySec = waitSec
	end
end

if retrySec > 0 then
	local rpmRem = 0
	if hasRPM then rpmRem = math.floor(rpmMilli / 1000) end
	local tpmRem = 0
	if hasTPM then tpmRem = math.floor(tpmMilli / 1000) end
	if rpmRem < 0 then rpmRem = 0 end
	if tpmRem < 0 then tpmRem = 0 end
	return {0, rpmRem, tpmRem, retrySec}
end

if hasRPM then
	rpmMilli = rpmMilli - 1000
	if rpmMilli < 0 then rpmMilli = 0 end
	redis.call('HSET', rpmKey, 'tokens_milli', tostring(rpmMilli), 'last_ms', tostring(nowMs))
	redis.call('PEXPIRE', rpmKey, ttlMs)
end
if hasTPM then
	tpmMilli = tpmMilli - (estTokens * 1000)
	if tpmMilli < 0 then tpmMilli = 0 end
	redis.call('HSET', tpmKey, 'tokens_milli', tostring(tpmMilli), 'last_ms', tostring(nowMs))
	redis.call('PEXPIRE', tpmKey, ttlMs)
end

local rpmRem = 0
if hasRPM then rpmRem = math.floor(rpmMilli / 1000) end
local tpmRem = 0
if hasTPM then tpmRem = math.floor(tpmMilli / 1000) end
return {1, rpmRem, tpmRem, 0}
`

// Lua script atomically reconciling TPM tokens after completion without underflow or overflow.
// KEYS: [1] tpm_key
// ARGV: [1] now_ms, [2] tpm_cap, [3] delta_tokens (estimated - actual), [4] ttl_ms
const luaReconcileTokensScript = `
local tpmKey = KEYS[1]
local nowMs = tonumber(ARGV[1])
local tpmCap = tonumber(ARGV[2])
local deltaTokens = tonumber(ARGV[3])
local ttlMs = tonumber(ARGV[4])
if tpmCap <= 0 then
	return 0
end
local vals = redis.call('HMGET', tpmKey, 'tokens_milli', 'last_ms')
local cur = tonumber(vals[1]) or (tpmCap * 1000)
local last = tonumber(vals[2]) or nowMs
local elapsed = nowMs - last
if elapsed < 0 then elapsed = 0 end
local refill = math.floor((elapsed * tpmCap * 1000) / 60000)
local maxMilli = tpmCap * 1000
cur = math.min(maxMilli, cur + refill)
cur = cur + (deltaTokens * 1000)
if cur < 0 then cur = 0 end
if cur > maxMilli then cur = maxMilli end
redis.call('HSET', tpmKey, 'tokens_milli', tostring(cur), 'last_ms', tostring(nowMs))
redis.call('PEXPIRE', tpmKey, ttlMs)
return math.floor(cur / 1000)
`

// Lua script reading current bucket headroom without deducting.
// KEYS: [1] rpm_key, [2] tpm_key, [3] streams_key
// ARGV: [1] now_ms, [2] rpm_cap, [3] has_rpm, [4] tpm_cap, [5] has_tpm, [6] max_streams, [7] has_streams
const luaRemainingScript = `
local nowMs = tonumber(ARGV[1])
local rpmCap = tonumber(ARGV[2])
local hasRPM = tonumber(ARGV[3]) == 1
local tpmCap = tonumber(ARGV[4])
local hasTPM = tonumber(ARGV[5]) == 1
local maxStreams = tonumber(ARGV[6])
local hasStreams = tonumber(ARGV[7]) == 1

local function readRem(key, cap, enabled)
	if not enabled or cap <= 0 then return 0 end
	local vals = redis.call('HMGET', key, 'tokens_milli', 'last_ms')
	if not vals[1] or not vals[2] then return cap end
	local cur = tonumber(vals[1])
	local last = tonumber(vals[2])
	local elapsed = nowMs - last
	if elapsed < 0 then elapsed = 0 end
	local refill = math.floor((elapsed * cap * 1000) / 60000)
	local nextTok = math.min(cap * 1000, cur + refill)
	if nextTok < 0 then nextTok = 0 end
	return math.floor(nextTok / 1000)
end

local rpmRem = readRem(KEYS[1], rpmCap, hasRPM)
local tpmRem = readRem(KEYS[2], tpmCap, hasTPM)
local streamRem = 0
if hasStreams then
	local active = tonumber(redis.call('GET', KEYS[3])) or 0
	streamRem = math.max(0, maxStreams - active)
end
return {rpmRem, tpmRem, streamRem}
`

// Lua script acquiring a cluster-wide concurrent stream slot under {org_id}.
// KEYS: [1] streams_key
// ARGV: [1] max_concurrent, [2] ttl_ms
const luaAcquireStreamScript = `
local maxC = tonumber(ARGV[1])
local ttlMs = tonumber(ARGV[2])
local cur = tonumber(redis.call('GET', KEYS[1])) or 0
if cur < 0 then cur = 0 end
if cur >= maxC then
	return {0, 0}
end
local nextVal = redis.call('INCR', KEYS[1])
redis.call('PEXPIRE', KEYS[1], ttlMs)
local rem = maxC - nextVal
if rem < 0 then rem = 0 end
return {1, rem}
`

// Lua script releasing a stream slot safely without dropping below 0.
const luaReleaseStreamScript = `
local cur = tonumber(redis.call('GET', KEYS[1])) or 0
if cur <= 1 then
	redis.call('DEL', KEYS[1])
	return 0
end
return redis.call('DECR', KEYS[1])
`

// AllowRequest checks RPM & TPM atomically against Valkey Cluster under {org_id},
// falling back to local (limit / replicas) quota if Valkey is unreachable.
func (r *RateLimiter) AllowRequest(ctx context.Context, scope ratelimit.LimitScope, estTokens int64, limits ratelimit.Limits) (ratelimit.CheckResult, error) {
	if err := validateRateLimitScope(scope); err != nil {
		return ratelimit.CheckResult{}, err
	}
	if estTokens < 0 {
		estTokens = 0
	}
	rpmKey, tpmKey, _, err := scopeKeys(scope)
	if err != nil {
		return ratelimit.CheckResult{}, err
	}
	if r.client == nil || r.client.raw == nil {
		r.recordDegraded(errors.New("valkey client unavailable"))
		return r.fallback.AllowRequest(ctx, scope, estTokens, r.scaledLimits(limits))
	}
	hasRPM, rpmVal := intFlag(limits.RPMLimit)
	hasTPM, tpmVal := intFlag(limits.TPMLimit)
	if !hasRPM && !hasTPM {
		return ratelimit.CheckResult{Allowed: true}, nil
	}
	nowMs := r.now().UnixMilli()
	res, err := r.client.EvalLua(
		ctx,
		luaAllowRequestScript,
		[]string{rpmKey, tpmKey},
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(rpmVal, 10),
		boolBit(hasRPM),
		strconv.FormatInt(tpmVal, 10),
		boolBit(hasTPM),
		strconv.FormatInt(estTokens, 10),
		strconv.FormatInt(bucketTTLMs, 10),
	)
	if err != nil {
		if IsCrossSlotError(err) {
			return ratelimit.CheckResult{}, fmt.Errorf("%w: %v", ErrCrossOrgKeyViolation, err)
		}
		r.recordDegraded(err)
		return r.fallback.AllowRequest(ctx, scope, estTokens, r.scaledLimits(limits))
	}
	arr, err := res.AsIntSlice()
	if err != nil || len(arr) < 4 {
		r.recordDegraded(fmt.Errorf("unexpected lua AllowRequest reply: %w", err))
		return r.fallback.AllowRequest(ctx, scope, estTokens, r.scaledLimits(limits))
	}
	r.recordHealthy()
	out := ratelimit.CheckResult{
		Allowed:           arr[0] == 1,
		HasRPM:            hasRPM,
		RemainingRequests: arr[1],
		HasTPM:            hasTPM,
		RemainingTokens:   arr[2],
		RetryAfterSec:     int(arr[3]),
	}
	if !out.Allowed {
		out.DenialCode = "rate_limit_exceeded"
		return out, ratelimit.ErrRateLimitExceeded
	}
	return out, nil
}

// AcquireStreamSlot acquires a cluster-wide stream concurrency slot with safe Release().
func (r *RateLimiter) AcquireStreamSlot(ctx context.Context, scope ratelimit.LimitScope, maxConcurrent int64) (*ratelimit.StreamLease, ratelimit.CheckResult, error) {
	if err := validateRateLimitScope(scope); err != nil {
		return nil, ratelimit.CheckResult{}, err
	}
	if maxConcurrent <= 0 {
		return nil, ratelimit.CheckResult{
			Allowed:       false,
			DenialCode:    "concurrency_limit_exceeded",
			RetryAfterSec: 1,
			HasStreams:    true,
		}, ratelimit.ErrConcurrencyLimitExceeded
	}
	_, _, streamKey, err := scopeKeys(scope)
	if err != nil {
		return nil, ratelimit.CheckResult{}, err
	}
	if r.client == nil || r.client.raw == nil {
		r.recordDegraded(errors.New("valkey client unavailable"))
		return r.fallback.AcquireStreamSlot(ctx, scope, r.scaledLimitValue(maxConcurrent))
	}
	res, err := r.client.EvalLua(
		ctx,
		luaAcquireStreamScript,
		[]string{streamKey},
		strconv.FormatInt(maxConcurrent, 10),
		strconv.FormatInt(streamTTLMs, 10),
	)
	if err != nil {
		if IsCrossSlotError(err) {
			return nil, ratelimit.CheckResult{}, fmt.Errorf("%w: %v", ErrCrossOrgKeyViolation, err)
		}
		r.recordDegraded(err)
		return r.fallback.AcquireStreamSlot(ctx, scope, r.scaledLimitValue(maxConcurrent))
	}
	arr, err := res.AsIntSlice()
	if err != nil || len(arr) < 2 {
		r.recordDegraded(fmt.Errorf("unexpected lua AcquireStreamSlot reply: %w", err))
		return r.fallback.AcquireStreamSlot(ctx, scope, r.scaledLimitValue(maxConcurrent))
	}
	r.recordHealthy()
	resOut := ratelimit.CheckResult{
		Allowed:          arr[0] == 1,
		HasStreams:       true,
		RemainingStreams: arr[1],
	}
	if !resOut.Allowed {
		resOut.DenialCode = "concurrency_limit_exceeded"
		resOut.RetryAfterSec = 1
		return nil, resOut, ratelimit.ErrConcurrencyLimitExceeded
	}
	lease := ratelimit.NewStreamLease(func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = r.client.EvalLua(releaseCtx, luaReleaseStreamScript, []string{streamKey})
	})
	return lease, resOut, nil
}

// ReconcileTokens credits unused estimated tokens or deducts token overrun in Valkey.
func (r *RateLimiter) ReconcileTokens(ctx context.Context, scope ratelimit.LimitScope, estimatedTokens, actualTokens int64, limits ratelimit.Limits) error {
	if err := validateRateLimitScope(scope); err != nil {
		return err
	}
	if limits.TPMLimit == nil {
		return nil
	}
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	delta := estimatedTokens - actualTokens
	if delta == 0 {
		return nil
	}
	_, tpmKey, _, err := scopeKeys(scope)
	if err != nil {
		return err
	}
	if r.client == nil || r.client.raw == nil {
		r.recordDegraded(errors.New("valkey client unavailable"))
		return r.fallback.ReconcileTokens(ctx, scope, estimatedTokens, actualTokens, r.scaledLimits(limits))
	}
	nowMs := r.now().UnixMilli()
	_, err = r.client.EvalLua(
		ctx,
		luaReconcileTokensScript,
		[]string{tpmKey},
		strconv.FormatInt(nowMs, 10),
		strconv.FormatInt(*limits.TPMLimit, 10),
		strconv.FormatInt(delta, 10),
		strconv.FormatInt(bucketTTLMs, 10),
	)
	if err != nil {
		if IsCrossSlotError(err) {
			return fmt.Errorf("%w: %v", ErrCrossOrgKeyViolation, err)
		}
		r.recordDegraded(err)
		return r.fallback.ReconcileTokens(ctx, scope, estimatedTokens, actualTokens, r.scaledLimits(limits))
	}
	r.recordHealthy()
	return nil
}

// Remaining returns non-mutating remaining headroom across RPM, TPM, and streams.
func (r *RateLimiter) Remaining(ctx context.Context, scope ratelimit.LimitScope, limits ratelimit.Limits) (ratelimit.CheckResult, error) {
	if err := validateRateLimitScope(scope); err != nil {
		return ratelimit.CheckResult{}, err
	}
	rpmKey, tpmKey, streamKey, err := scopeKeys(scope)
	if err != nil {
		return ratelimit.CheckResult{}, err
	}
	if r.client == nil || r.client.raw == nil {
		r.recordDegraded(errors.New("valkey client unavailable"))
		return r.fallback.Remaining(ctx, scope, r.scaledLimits(limits))
	}
	hasRPM, rpmVal := intFlag(limits.RPMLimit)
	hasTPM, tpmVal := intFlag(limits.TPMLimit)
	hasStreams, streamVal := intFlag(limits.MaxConcurrentStreams)
	res, err := r.client.EvalLua(
		ctx,
		luaRemainingScript,
		[]string{rpmKey, tpmKey, streamKey},
		strconv.FormatInt(r.now().UnixMilli(), 10),
		strconv.FormatInt(rpmVal, 10),
		boolBit(hasRPM),
		strconv.FormatInt(tpmVal, 10),
		boolBit(hasTPM),
		strconv.FormatInt(streamVal, 10),
		boolBit(hasStreams),
	)
	if err != nil {
		if IsCrossSlotError(err) {
			return ratelimit.CheckResult{}, fmt.Errorf("%w: %v", ErrCrossOrgKeyViolation, err)
		}
		r.recordDegraded(err)
		return r.fallback.Remaining(ctx, scope, r.scaledLimits(limits))
	}
	arr, err := res.AsIntSlice()
	if err != nil || len(arr) < 3 {
		r.recordDegraded(fmt.Errorf("unexpected lua Remaining reply: %w", err))
		return r.fallback.Remaining(ctx, scope, r.scaledLimits(limits))
	}
	r.recordHealthy()
	return ratelimit.CheckResult{
		Allowed:           true,
		HasRPM:            hasRPM,
		RemainingRequests: arr[0],
		HasTPM:            hasTPM,
		RemainingTokens:   arr[1],
		HasStreams:        hasStreams,
		RemainingStreams:  arr[2],
	}, nil
}

func (r *RateLimiter) scaledLimits(limits ratelimit.Limits) ratelimit.Limits {
	return ratelimit.Limits{
		RPMLimit:             r.scaledPtr(limits.RPMLimit),
		TPMLimit:             r.scaledPtr(limits.TPMLimit),
		MaxConcurrentStreams: r.scaledPtr(limits.MaxConcurrentStreams),
	}
}

func (r *RateLimiter) scaledPtr(v *int64) *int64 {
	if v == nil {
		return nil
	}
	scaled := r.scaledLimitValue(*v)
	return &scaled
}

func (r *RateLimiter) scaledLimitValue(v int64) int64 {
	if v <= 0 || r.replicas <= 1 {
		return v
	}
	q := int64(math.Floor(float64(v) / float64(r.replicas)))
	if q < 1 {
		return 1
	}
	return q
}

func validateRateLimitScope(scope ratelimit.LimitScope) error {
	if scope.OrgID == "" || scope.ProjectID == "" || scope.EnvID == "" || scope.VirtualKeyID == "" {
		return fmt.Errorf("%w: org, project, env, and virtual_key IDs are required", ratelimit.ErrInvalidScope)
	}
	return nil
}

func scopeKeys(scope ratelimit.LimitScope) (string, string, string, error) {
	suffix := fmt.Sprintf("%s:%s:%s", scope.ProjectID, scope.EnvID, scope.VirtualKeyID)
	rpmKey, err := Key(scope.OrgID, "rl", "rpm:"+suffix)
	if err != nil {
		return "", "", "", err
	}
	tpmKey, err := Key(scope.OrgID, "rl", "tpm:"+suffix)
	if err != nil {
		return "", "", "", err
	}
	streamKey, err := Key(scope.OrgID, "rl", "streams:"+suffix)
	if err != nil {
		return "", "", "", err
	}
	return rpmKey, tpmKey, streamKey, nil
}

func intFlag(v *int64) (bool, int64) {
	if v == nil {
		return false, 0
	}
	return true, *v
}

func boolBit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
