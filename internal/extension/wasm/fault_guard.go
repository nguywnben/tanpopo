// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	storageport "github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	// DefaultFaultThreshold allows up to 20 faults within Window; crossing > 20 triggers quarantine (FMEA-05).
	DefaultFaultThreshold = 20
	// DefaultFaultWindow defines the 60-second sliding fault-counting window (FMEA-05).
	DefaultFaultWindow = 60 * time.Second
	// AlertCodePluginTrap is emitted when a Wasm extension installation crosses the FMEA-05 fault threshold.
	AlertCodePluginTrap = "ALT-PLUGIN-TRAP"
	// DefaultAuditEntryPoint matches the canonical data_plane_guard enum in audit_events.
	DefaultAuditEntryPoint = "data_plane_guard"
)

func normalizeAuditEntryPoint(raw string) (string, error) {
	ep := strings.TrimSpace(raw)
	if ep == "" {
		return DefaultAuditEntryPoint, nil
	}
	switch ep {
	case "admin_api", "cli", "gitops_boot", "system_reaper", "data_plane_guard":
		return ep, nil
	default:
		return DefaultAuditEntryPoint, fmt.Errorf("unsupported audit entry_point %q, using %q", ep, DefaultAuditEntryPoint)
	}
}

// QuarantineFunc matches (*installer.LifecycleService).QuarantineInstallation without creating
// a concrete package dependency from runtime execution onto SQLite installation persistence.
type QuarantineFunc func(ctx context.Context, installationID, reason, actorID string) error

// AlertEvent captures structured operational alert details for ALT-PLUGIN-TRAP emission.
type AlertEvent struct {
	Code           string    `json:"code"`
	InstallationID string    `json:"installation_id"`
	FaultCount     int       `json:"fault_count"`
	WindowSeconds  int       `json:"window_seconds"`
	LastKind       FaultKind `json:"last_kind"`
	Reason         string    `json:"reason"`
	TimestampMs    int64     `json:"timestamp_ms"`
}

// FaultGuardConfig configures FMEA-05 sliding-window trap detection and automatic SM-01 quarantine.
type FaultGuardConfig struct {
	MaxFaultsInWindow int
	Window            time.Duration
	Now               func() time.Time
	Quarantine        QuarantineFunc
	AuditSink         storageport.AuditSink
	ActorID           string
	EntryPoint        string
	OnAlert           func(ctx context.Context, ev AlertEvent)
	OnError           func(ctx context.Context, err error)
}

type installationFaultState struct {
	timestamps   []time.Time
	quarantined  bool
	quarantining bool
}

// FaultGuard tracks per-installation Wasm guest traps, watchdog timeouts, OOMs, and ABI violations.
// Crossing > 20 faults in a 60s window automatically evicts warm pool slots, moves SM-01 to
// QUARANTINED via QuarantineFunc, and emits ALT-PLUGIN-TRAP without disturbing sibling adapters.
type FaultGuard struct {
	threshold  int
	window     time.Duration
	now        func() time.Time
	quarantine QuarantineFunc
	audit      storageport.AuditSink
	actorID    string
	entryPoint string
	onAlert    func(ctx context.Context, ev AlertEvent)
	onError    func(ctx context.Context, err error)
	idGen      *idgen.Generator

	mu     sync.Mutex
	states map[string]*installationFaultState
	pools  map[string][]*wasmPool
}

// NewFaultGuard initializes a FMEA-05 sliding-window FaultGuard.
func NewFaultGuard(cfg FaultGuardConfig) *FaultGuard {
	threshold := cfg.MaxFaultsInWindow
	if threshold <= 0 {
		threshold = DefaultFaultThreshold
	}
	window := cfg.Window
	if window <= 0 {
		window = DefaultFaultWindow
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	actorID := strings.TrimSpace(cfg.ActorID)
	if actorID == "" {
		actorID = "system:wasm_fault_guard"
	}
	entryPoint, epErr := normalizeAuditEntryPoint(cfg.EntryPoint)
	if epErr != nil && cfg.OnError != nil {
		cfg.OnError(context.Background(), epErr)
	}
	return &FaultGuard{
		threshold:  threshold,
		window:     window,
		now:        nowFn,
		quarantine: cfg.Quarantine,
		audit:      cfg.AuditSink,
		actorID:    actorID,
		entryPoint: entryPoint,
		onAlert:    cfg.OnAlert,
		onError:    cfg.OnError,
		idGen:      idgen.NewGenerator(nowFn),
		states:     make(map[string]*installationFaultState),
		pools:      make(map[string][]*wasmPool),
	}
}

// IsQuarantined reports whether installationID has crossed the fault threshold and been quarantined.
func (g *FaultGuard) IsQuarantined(installationID string) bool {
	if g == nil {
		return false
	}
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return false
	}
	g.mu.Lock()
	st, ok := g.states[installationID]
	q := ok && st.quarantined
	g.mu.Unlock()
	return q
}

// WindowCount returns the active fault count inside the current sliding window for installationID.
func (g *FaultGuard) WindowCount(installationID string) int {
	if g == nil {
		return 0
	}
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return 0
	}
	now := g.now().UTC()
	cutoff := now.Add(-g.window)

	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.states[installationID]
	if !ok {
		return 0
	}
	st.timestamps = pruneTimestamps(st.timestamps, cutoff)
	return len(st.timestamps)
}

// Reset clears sliding-window and quarantine state for installationID after operator recovery.
func (g *FaultGuard) Reset(installationID string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	delete(g.states, strings.TrimSpace(installationID))
	g.mu.Unlock()
}

// RecordFault records one guest execution fault for installationID and triggers quarantine
// when the sliding-window count strictly exceeds MaxFaultsInWindow (default > 20 / 60s).
// Caller context cancellations must NOT be passed here.
func (g *FaultGuard) RecordFault(ctx context.Context, installationID string, kind FaultKind, reason string) bool {
	if g == nil || kind == FaultKindQuarantined || kind == FaultKindClosed {
		return false
	}
	installationID = strings.TrimSpace(installationID)
	if installationID == "" {
		return false
	}

	now := g.now().UTC()
	cutoff := now.Add(-g.window)

	g.mu.Lock()
	st, ok := g.states[installationID]
	if !ok {
		st = &installationFaultState{}
		g.states[installationID] = st
	}
	st.timestamps = pruneTimestamps(st.timestamps, cutoff)
	st.timestamps = append(st.timestamps, now)
	count := len(st.timestamps)

	shouldTrigger := count > g.threshold && !st.quarantined && !st.quarantining
	var targetPools []*wasmPool
	if shouldTrigger {
		st.quarantining = true
		if len(g.pools[installationID]) > 0 {
			targetPools = append([]*wasmPool(nil), g.pools[installationID]...)
		}
	}
	g.mu.Unlock()

	if !shouldTrigger {
		return false
	}

	for _, p := range targetPools {
		p.EvictAllWarm()
	}

	qReason := fmt.Sprintf("fmea05_auto_quarantine: %d %s faults within %ds (%s)",
		count, kind, int(g.window.Seconds()), strings.TrimSpace(reason))

	opCtx := context.Background()
	if ctx != nil {
		opCtx = context.WithoutCancel(ctx)
	}
	opCtx, cancel := context.WithTimeout(opCtx, 5*time.Second)
	defer cancel()

	var qErr error
	if g.quarantine != nil {
		qErr = g.quarantine(opCtx, installationID, qReason, g.actorID)
	}

	g.mu.Lock()
	if qErr == nil {
		st.quarantined = true
	}
	st.quarantining = false
	g.mu.Unlock()

	if qErr != nil {
		if g.onError != nil {
			g.onError(opCtx, fmt.Errorf("fmea05 quarantine %s failed: %w", installationID, qErr))
		}
		return false
	}

	alert := AlertEvent{
		Code:           AlertCodePluginTrap,
		InstallationID: installationID,
		FaultCount:     count,
		WindowSeconds:  int(g.window.Seconds()),
		LastKind:       kind,
		Reason:         qReason,
		TimestampMs:    now.UnixMilli(),
	}
	g.emitAlert(opCtx, alert)
	return true
}

func (g *FaultGuard) emitAlert(ctx context.Context, alert AlertEvent) {
	if g.audit != nil {
		metaBytes, _ := json.Marshal(alert)
		evID, err := g.idGen.New(idgen.PrefixAudit)
		if err != nil {
			evID = fmt.Sprintf("aud_%d", alert.TimestampMs)
		}
		if err := g.audit.Append(ctx, storageport.AuditEvent{
			ID:           evID,
			CreatedAtMs:  alert.TimestampMs,
			ActorKind:    "SYSTEM",
			ActorID:      g.actorID,
			EntryPoint:   g.entryPoint,
			EventType:    AlertCodePluginTrap,
			TargetType:   "extension_installation",
			TargetID:     alert.InstallationID,
			MetadataJSON: string(metaBytes),
		}); err != nil && g.onError != nil {
			g.onError(ctx, fmt.Errorf("fmea05 audit append %s failed: %w", alert.InstallationID, err))
		}
	}
	if g.onAlert != nil {
		g.onAlert(ctx, alert)
	}
}

func (g *FaultGuard) registerPool(installationID string, p *wasmPool) {
	if g == nil || installationID == "" || p == nil {
		return
	}
	g.mu.Lock()
	g.pools[installationID] = append(g.pools[installationID], p)
	g.mu.Unlock()
}

func (g *FaultGuard) unregisterPool(installationID string, p *wasmPool) {
	if g == nil || installationID == "" || p == nil {
		return
	}
	g.mu.Lock()
	list := g.pools[installationID]
	for i, item := range list {
		if item == p {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(g.pools, installationID)
	} else {
		g.pools[installationID] = list
	}
	g.mu.Unlock()
}

func pruneTimestamps(items []time.Time, cutoff time.Time) []time.Time {
	keep := 0
	for _, ts := range items {
		// Retain events strictly inside the open-closed window (now - window <= ts).
		if !ts.Before(cutoff) {
			items[keep] = ts
			keep++
		}
	}
	for i := keep; i < len(items); i++ {
		items[i] = time.Time{}
	}
	return items[:keep]
}
