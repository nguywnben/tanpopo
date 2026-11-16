// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package breaker implements Tanpopo's canonical SM-04 sliding-window circuit
// breaker, 429/529 Retry-After cooldown state machine, half-open single-probe
// CAS gate, and EWMA latency tracker (SPEC-CORE-01 / TP-IMP-0020).
package breaker

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Canonical SM-04 ModelDeployment lifecycle states.
const (
	StateProvisioned          = "PROVISIONED"
	StateActiveHealthy        = "ACTIVE_HEALTHY"
	StateThrottledCooldown    = "THROTTLED_COOLDOWN"
	StateCircuitOpenUnhealthy = "CIRCUIT_OPEN_UNHEALTHY"
	StateHalfOpenProbing      = "HALF_OPEN_PROBING"
	StateDrainingMaintenance  = "DRAINING_MAINTENANCE"
	StateDisabled             = "DISABLED"
)

const (
	defaultBucketCount      = 12
	defaultBucketDuration   = 5 * time.Second
	defaultMinSamples       = 10
	defaultFailureRatio     = 0.50
	defaultConsecFailures   = 5
	defaultOpenCooldown     = 30 * time.Second
	defaultProbeSuccesses   = 1
	defaultMinRetryAfter    = 1 * time.Second
	defaultMaxRetryAfter    = 120 * time.Second
	defaultFallbackBase     = 2 * time.Second
	defaultFallbackMax      = 8 * time.Second
	defaultFallbackMaxShift = 2 // 2s, 4s, 8s
	defaultJitterFactor     = 0.20
	defaultEWMADecay        = 0.30
)

// OutcomeKind classifies an execution outcome for SM-04 state transitions.
type OutcomeKind int

const (
	// OutcomeSuccess indicates healthy upstream completion (HTTP 2xx).
	OutcomeSuccess OutcomeKind = iota
	// OutcomeThrottle429 indicates upstream rate-limit throttling (HTTP 429/529).
	OutcomeThrottle429
	// OutcomeSystemFault indicates infrastructure fault (HTTP 500/502/503/504, timeout, TCP/TLS fault).
	OutcomeSystemFault
	// OutcomeCallerError indicates caller-induced fault (HTTP 400/422/etc.) that never penalizes SM-04.
	OutcomeCallerError
	// OutcomeClientCanceled indicates caller-side cancellation that never penalizes SM-04.
	OutcomeClientCanceled
)

// AttemptOutcome describes a completed upstream hop recorded against a deployment.
type AttemptOutcome struct {
	Kind          OutcomeKind
	Latency       time.Duration
	RetryAfterRaw string
	IsProbe       bool
}

// DeploymentStatus describes the non-mutating SM-04 status observed for Step 4 / Explain.
type DeploymentStatus struct {
	State          string
	Eligible       bool
	CooldownUntil  time.Time
	OpenUntil      time.Time
	ProbeInFlight  bool
	EWMALatency    time.Duration
	HasEWMALatency bool
}

// Config configures the SM-04 sliding-window circuit breaker thresholds.
type Config struct {
	BucketDuration          time.Duration
	MinSamples              int
	FailureRatio            float64
	ConsecutiveFailureLimit int
	OpenCooldown            time.Duration
	ProbeSuccessRequired    int
	MinRetryAfter           time.Duration
	MaxRetryAfter           time.Duration
	FallbackCooldownBase    time.Duration
	FallbackCooldownMax     time.Duration
	JitterFactor            float64
	EWMADecay               float64
	Now                     func() time.Time
	JitterFn                func(base time.Duration) time.Duration
	OnTransition            func(deploymentID, prevState, newState string)
}

type windowBucket struct {
	epoch    int64
	total    int64
	failures int64
}

type deploymentState struct {
	mu sync.Mutex

	state               string
	cooldownUntil       time.Time
	openUntil           time.Time
	throttleStreak      int
	consecutiveFailures int
	probeInFlight       bool
	probeSeq            uint64
	probeSuccesses      int
	ewmaLatencyNs       float64
	hasEWMA             bool
	buckets             [defaultBucketCount]windowBucket
}

// CircuitBreaker tracks per-deployment SM-04 circuit and cooldown health in memory.
type CircuitBreaker struct {
	cfg     Config
	entries sync.Map // map[string]*deploymentState
}

// New constructs a CircuitBreaker with deterministic SM-04 defaults.
func New(cfg Config) *CircuitBreaker {
	if cfg.BucketDuration <= 0 {
		cfg.BucketDuration = defaultBucketDuration
	}
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = defaultMinSamples
	}
	if cfg.FailureRatio <= 0 || cfg.FailureRatio > 1 {
		cfg.FailureRatio = defaultFailureRatio
	}
	if cfg.ConsecutiveFailureLimit <= 0 {
		cfg.ConsecutiveFailureLimit = defaultConsecFailures
	}
	if cfg.OpenCooldown <= 0 {
		cfg.OpenCooldown = defaultOpenCooldown
	}
	if cfg.ProbeSuccessRequired <= 0 {
		cfg.ProbeSuccessRequired = defaultProbeSuccesses
	}
	if cfg.MinRetryAfter <= 0 {
		cfg.MinRetryAfter = defaultMinRetryAfter
	}
	if cfg.MaxRetryAfter <= 0 {
		cfg.MaxRetryAfter = defaultMaxRetryAfter
	}
	if cfg.MaxRetryAfter < cfg.MinRetryAfter {
		cfg.MaxRetryAfter = cfg.MinRetryAfter
	}
	if cfg.FallbackCooldownBase <= 0 {
		cfg.FallbackCooldownBase = defaultFallbackBase
	}
	if cfg.FallbackCooldownMax <= 0 {
		cfg.FallbackCooldownMax = defaultFallbackMax
	}
	if cfg.JitterFactor < 0 || cfg.JitterFactor > 0.5 {
		cfg.JitterFactor = defaultJitterFactor
	}
	if cfg.EWMADecay <= 0 || cfg.EWMADecay > 1 {
		cfg.EWMADecay = defaultEWMADecay
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.JitterFn == nil {
		factor := cfg.JitterFactor
		cfg.JitterFn = func(base time.Duration) time.Duration {
			return applyCryptoJitter(base, factor)
		}
	}
	return &CircuitBreaker{cfg: cfg}
}

func (b *CircuitBreaker) now() time.Time {
	if b == nil || b.cfg.Now == nil {
		return time.Now().UTC()
	}
	return b.cfg.Now().UTC()
}

func (b *CircuitBreaker) getOrCreate(deploymentID string) *deploymentState {
	if v, ok := b.entries.Load(deploymentID); ok {
		return v.(*deploymentState)
	}
	fresh := &deploymentState{}
	actual, _ := b.entries.LoadOrStore(deploymentID, fresh)
	return actual.(*deploymentState)
}

func isAdminBlockedSnapshotState(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case StateProvisioned, StateDrainingMaintenance, StateDisabled:
		return true
	default:
		return false
	}
}

func (d *deploymentState) seedSnapshotCooldownIfUninitialized(snapshotState string, snapshotCooldownUntilMs *int64, now time.Time) {
	if d.state != "" {
		return
	}
	norm := strings.ToUpper(strings.TrimSpace(snapshotState))
	switch norm {
	case StateThrottledCooldown:
		if snapshotCooldownUntilMs != nil {
			until := time.UnixMilli(*snapshotCooldownUntilMs).UTC()
			if now.Before(until) {
				d.state = StateThrottledCooldown
				d.cooldownUntil = until
				return
			}
		}
	case StateCircuitOpenUnhealthy:
		if snapshotCooldownUntilMs != nil {
			until := time.UnixMilli(*snapshotCooldownUntilMs).UTC()
			if now.Before(until) {
				d.state = StateCircuitOpenUnhealthy
				d.openUntil = until
				return
			}
		}
		d.state = StateHalfOpenProbing
		d.probeInFlight = false
		d.probeSuccesses = 0
		return
	case StateHalfOpenProbing:
		d.state = StateHalfOpenProbing
		return
	}
	d.state = StateActiveHealthy
}

// PeekState returns a side-effect-free view of the deployment's SM-04 eligibility
// for Explain() dry-runs and diagnostic inspection.
func (b *CircuitBreaker) PeekState(deploymentID, snapshotState string, snapshotCooldownUntilMs *int64) DeploymentStatus {
	if isAdminBlockedSnapshotState(snapshotState) {
		norm := strings.ToUpper(strings.TrimSpace(snapshotState))
		return DeploymentStatus{
			State:    norm,
			Eligible: false,
		}
	}
	if b == nil {
		return DeploymentStatus{
			State:    StateActiveHealthy,
			Eligible: true,
		}
	}

	now := b.now()
	var d *deploymentState
	if v, ok := b.entries.Load(deploymentID); ok {
		d = v.(*deploymentState)
		d.mu.Lock()
		defer d.mu.Unlock()
	} else {
		var tmp deploymentState
		tmp.seedSnapshotCooldownIfUninitialized(snapshotState, snapshotCooldownUntilMs, now)
		d = &tmp
	}

	d.seedSnapshotCooldownIfUninitialized(snapshotState, snapshotCooldownUntilMs, now)

	st := DeploymentStatus{
		HasEWMALatency: d.hasEWMA,
		EWMALatency:    time.Duration(d.ewmaLatencyNs),
		ProbeInFlight:  d.probeInFlight,
	}

	switch d.state {
	case StateThrottledCooldown:
		if now.Before(d.cooldownUntil) {
			st.State = StateThrottledCooldown
			st.Eligible = false
			st.CooldownUntil = d.cooldownUntil
			return st
		}
		st.State = StateActiveHealthy
		st.Eligible = true
		return st

	case StateCircuitOpenUnhealthy:
		if now.Before(d.openUntil) {
			st.State = StateCircuitOpenUnhealthy
			st.Eligible = false
			st.OpenUntil = d.openUntil
			return st
		}
		st.State = StateHalfOpenProbing
		st.Eligible = !d.probeInFlight
		return st

	case StateHalfOpenProbing:
		st.State = StateHalfOpenProbing
		st.Eligible = !d.probeInFlight
		return st

	default:
		st.State = StateActiveHealthy
		st.Eligible = true
		return st
	}
}

// AcquireAttempt checks SM-04 eligibility and atomically reserves the single
// HALF_OPEN_PROBING slot if transitioning out of CIRCUIT_OPEN_UNHEALTHY.
// The caller MUST invoke release() when finished if RecordOutcome is skipped.
func (b *CircuitBreaker) AcquireAttempt(deploymentID, snapshotState string, snapshotCooldownUntilMs *int64) (allowed bool, state string, isProbe bool, release func()) {
	if isAdminBlockedSnapshotState(snapshotState) {
		return false, strings.ToUpper(strings.TrimSpace(snapshotState)), false, nil
	}
	if b == nil {
		return true, StateActiveHealthy, false, func() {}
	}

	now := b.now()
	d := b.getOrCreate(deploymentID)
	var transPrev, transNext string
	allowed, state, isProbe, release, transPrev, transNext = func() (bool, string, bool, func(), string, string) {
		d.mu.Lock()
		defer d.mu.Unlock()

		d.seedSnapshotCooldownIfUninitialized(snapshotState, snapshotCooldownUntilMs, now)

		switch d.state {
		case StateThrottledCooldown:
			if now.Before(d.cooldownUntil) {
				return false, StateThrottledCooldown, false, nil, "", ""
			}
			prev := d.state
			d.state = StateActiveHealthy
			d.cooldownUntil = time.Time{}
			return true, StateActiveHealthy, false, func() {}, prev, StateActiveHealthy

		case StateCircuitOpenUnhealthy:
			if now.Before(d.openUntil) {
				return false, StateCircuitOpenUnhealthy, false, nil, "", ""
			}
			prev := d.state
			d.state = StateHalfOpenProbing
			d.probeInFlight = true
			d.probeSeq++
			d.probeSuccesses = 0
			seq := d.probeSeq
			return true, StateHalfOpenProbing, true, func() {
				b.releaseProbeLease(deploymentID, seq)
			}, prev, StateHalfOpenProbing

		case StateHalfOpenProbing:
			if d.probeInFlight {
				return false, StateHalfOpenProbing, false, nil, "", ""
			}
			d.probeInFlight = true
			d.probeSeq++
			seq := d.probeSeq
			return true, StateHalfOpenProbing, true, func() {
				b.releaseProbeLease(deploymentID, seq)
			}, "", ""

		default:
			return true, StateActiveHealthy, false, func() {}, "", ""
		}
	}()
	if transNext != "" && transPrev != transNext && b.cfg.OnTransition != nil {
		b.cfg.OnTransition(deploymentID, transPrev, transNext)
	}
	return allowed, state, isProbe, release
}

func (b *CircuitBreaker) releaseProbeLease(deploymentID string, seq uint64) {
	if b == nil {
		return
	}
	v, ok := b.entries.Load(deploymentID)
	if !ok {
		return
	}
	d := v.(*deploymentState)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.probeSeq == seq {
		d.probeInFlight = false
	}
}

// RecordOutcome updates sliding-window metrics, EWMA latency, and SM-04 state
// transitions according to upstream execution results.
func (b *CircuitBreaker) RecordOutcome(deploymentID string, outcome AttemptOutcome) {
	if b == nil || deploymentID == "" {
		return
	}
	now := b.now()
	d := b.getOrCreate(deploymentID)
	d.mu.Lock()
	if d.state == "" {
		d.state = StateActiveHealthy
	}
	prevState := d.state

	switch outcome.Kind {
	case OutcomeCallerError, OutcomeClientCanceled:
		// Caller 400/422 errors and client cancellations never penalize SM-04.
		if outcome.IsProbe {
			d.probeInFlight = false
		}

	case OutcomeSuccess:
		b.observeLatencyLocked(d, outcome.Latency)
		b.recordWindowSampleLocked(d, now, false)
		d.consecutiveFailures = 0
		d.throttleStreak = 0
		if d.state == StateHalfOpenProbing {
			d.probeInFlight = false
			d.probeSuccesses++
			if d.probeSuccesses >= b.cfg.ProbeSuccessRequired {
				b.resetToHealthyLocked(d)
			}
		} else if d.state == StateThrottledCooldown {
			b.resetToHealthyLocked(d)
		}

	case OutcomeThrottle429:
		d.probeInFlight = false
		d.probeSuccesses = 0
		d.throttleStreak++
		if !(d.state == StateCircuitOpenUnhealthy && now.Before(d.openUntil) && !outcome.IsProbe) {
			cooldown := b.computeCooldownDuration(outcome.RetryAfterRaw, d.throttleStreak-1, now)
			d.state = StateThrottledCooldown
			d.cooldownUntil = now.Add(cooldown)
		}

	case OutcomeSystemFault:
		b.recordWindowSampleLocked(d, now, true)
		d.consecutiveFailures++

		if d.state == StateHalfOpenProbing || outcome.IsProbe {
			d.state = StateCircuitOpenUnhealthy
			d.openUntil = now.Add(b.cfg.OpenCooldown)
			d.probeInFlight = false
			d.probeSuccesses = 0
		} else {
			total, failures := b.sumWindowLocked(d, now)
			tripByStreak := d.consecutiveFailures >= b.cfg.ConsecutiveFailureLimit
			tripByRatio := total >= int64(b.cfg.MinSamples) && float64(failures)/float64(total) >= b.cfg.FailureRatio
			if tripByStreak || tripByRatio {
				d.state = StateCircuitOpenUnhealthy
				d.openUntil = now.Add(b.cfg.OpenCooldown)
				d.probeInFlight = false
				d.probeSuccesses = 0
			}
		}
	}
	nextState := d.state
	d.mu.Unlock()

	if nextState != "" && prevState != nextState && b.cfg.OnTransition != nil {
		b.cfg.OnTransition(deploymentID, prevState, nextState)
	}
}

// EWMALatency returns the current EWMA response latency for deploymentID.
func (b *CircuitBreaker) EWMALatency(deploymentID string) (time.Duration, bool) {
	if b == nil || deploymentID == "" {
		return 0, false
	}
	v, ok := b.entries.Load(deploymentID)
	if !ok {
		return 0, false
	}
	d := v.(*deploymentState)
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.hasEWMA {
		return 0, false
	}
	return time.Duration(d.ewmaLatencyNs), true
}

func (b *CircuitBreaker) observeLatencyLocked(d *deploymentState, lat time.Duration) {
	if lat <= 0 {
		return
	}
	ns := float64(lat.Nanoseconds())
	if !d.hasEWMA {
		d.ewmaLatencyNs = ns
		d.hasEWMA = true
		return
	}
	alpha := b.cfg.EWMADecay
	d.ewmaLatencyNs = alpha*ns + (1.0-alpha)*d.ewmaLatencyNs
}

func (b *CircuitBreaker) resetToHealthyLocked(d *deploymentState) {
	d.state = StateActiveHealthy
	d.cooldownUntil = time.Time{}
	d.openUntil = time.Time{}
	d.consecutiveFailures = 0
	d.throttleStreak = 0
	d.probeInFlight = false
	d.probeSuccesses = 0
	d.buckets = [defaultBucketCount]windowBucket{}
}

func (b *CircuitBreaker) bucketEpoch(now time.Time) int64 {
	sec := max(int64(1), int64(b.cfg.BucketDuration/time.Second))
	return now.Unix() / sec
}

func (b *CircuitBreaker) recordWindowSampleLocked(d *deploymentState, now time.Time, failure bool) {
	epoch := b.bucketEpoch(now)
	idx := ((epoch % defaultBucketCount) + defaultBucketCount) % defaultBucketCount
	slot := &d.buckets[idx]
	if slot.epoch != epoch {
		slot.epoch = epoch
		slot.total = 0
		slot.failures = 0
	}
	slot.total++
	if failure {
		slot.failures++
	}
}

func (b *CircuitBreaker) sumWindowLocked(d *deploymentState, now time.Time) (total, failures int64) {
	epoch := b.bucketEpoch(now)
	for i := range d.buckets {
		slot := d.buckets[i]
		if slot.epoch == 0 && slot.total == 0 {
			continue
		}
		if epoch-slot.epoch < defaultBucketCount && slot.epoch <= epoch {
			total += slot.total
			failures += slot.failures
		}
	}
	return total, failures
}

func (b *CircuitBreaker) computeCooldownDuration(retryAfterRaw string, streak int, now time.Time) time.Duration {
	if d, ok := ParseRetryAfter(retryAfterRaw, now); ok {
		return clampDuration(d, b.cfg.MinRetryAfter, b.cfg.MaxRetryAfter)
	}
	shift := streak
	if shift > defaultFallbackMaxShift {
		shift = defaultFallbackMaxShift
	}
	if shift < 0 {
		shift = 0
	}
	base := b.cfg.FallbackCooldownBase << shift
	if base > b.cfg.FallbackCooldownMax {
		base = b.cfg.FallbackCooldownMax
	}
	jittered := b.cfg.JitterFn(base)
	return clampDuration(jittered, b.cfg.MinRetryAfter, b.cfg.MaxRetryAfter)
}

// ParseRetryAfter parses an HTTP Retry-After header (integer seconds or HTTP-date).
func ParseRetryAfter(raw string, now time.Time) (time.Duration, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		if secs < 0 {
			secs = 0
		}
		const maxDurationSecs = int64(math.MaxInt64 / int64(time.Second))
		if secs > maxDurationSecs {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(trimmed); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

func clampDuration(d, minD, maxD time.Duration) time.Duration {
	if d < minD {
		return minD
	}
	if d > maxD {
		return maxD
	}
	return d
}

func applyCryptoJitter(base time.Duration, factor float64) time.Duration {
	if factor <= 0 || base <= 0 {
		return base
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return base
	}
	u := float64(binary.LittleEndian.Uint64(raw[:])) / float64(^uint64(0))
	mult := (1.0 - factor) + (2.0*factor)*u
	return time.Duration(float64(base) * mult)
}
