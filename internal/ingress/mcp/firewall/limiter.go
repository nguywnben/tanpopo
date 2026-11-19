// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package firewall

import (
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	maxKeyBytes       = 256
	maxLimiterEntries = 4096
)

var errInvalidLimiterConfig = errors.New("firewall: invalid rate limiter configuration")

// LimiterConfig bounds mutating and destructive calls in fixed windows.
// Now should be deterministic in tests; nil uses the system clock.
type LimiterConfig struct {
	Limit      int
	Window     time.Duration
	MaxEntries int
	Now        func() time.Time
}

type windowEntry struct {
	started time.Time
	count   int
}

// Limiter is a concurrency-safe, memory-bounded admission limiter. If its
// entry cap is reached, unseen keys are denied until expired entries are
// reclaimed. This avoids evicting live buckets and bypassing their limits.
type Limiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Duration
	maxEntries int
	now        func() time.Time
	entries    map[string]windowEntry
}

// NewLimiter constructs a bounded fixed-window limiter. Limit, Window, and
// MaxEntries must be between 1 and 4096; invalid configurations fail closed.
func NewLimiter(cfg LimiterConfig) (*Limiter, error) {
	if cfg.Limit <= 0 || cfg.Window <= 0 || cfg.MaxEntries <= 0 || cfg.MaxEntries > maxLimiterEntries {
		return nil, errInvalidLimiterConfig
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		limit: cfg.Limit, window: cfg.Window, maxEntries: cfg.MaxEntries,
		now: now, entries: make(map[string]windowEntry, cfg.MaxEntries),
	}, nil
}

// Allow reports whether a call for key can be admitted. Read-only calls bypass
// this high-risk limiter. Unknown classes normalize to Mutating. Empty or
// oversized keys and capacity exhaustion are denied.
func (l *Limiter) Allow(class Class, key string) bool {
	if NormalizeClass(string(class)) == ReadOnly {
		return true
	}
	if l == nil || key == "" || len(key) > maxKeyBytes || strings.TrimSpace(key) == "" {
		return false
	}

	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	for candidate, entry := range l.entries {
		if !now.Before(entry.started.Add(l.window)) {
			delete(l.entries, candidate)
		}
	}

	entry, exists := l.entries[key]
	if !exists {
		if len(l.entries) >= l.maxEntries {
			return false
		}
		l.entries[key] = windowEntry{started: now, count: 1}
		return true
	}
	// A backwards-moving clock must not reset a bucket early.
	if now.Before(entry.started) {
		return false
	}
	if now.Sub(entry.started) >= l.window {
		l.entries[key] = windowEntry{started: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}
