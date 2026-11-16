// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package killswitch implements the 4-level Emergency Kill-Switch
// (VIRTUAL_KEY, PROVIDER_CONNECTION, EXTENSION_INSTALLATION, PROJECT_OR_ORG_FREEZE),
// persistent transactional state + immutable audit recording, and sub-second (< 1s)
// multi-dimensional SSE stream severing (TP-IMP-0023 / SLO-07 / OPS-AC-03,04).
//
// DAG Boundary (Tier 3): May import stdlib, internal/pkg, and internal/storage/* only.
package killswitch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Level defines the 4 canonical emergency kill-switch scopes matching SQLite/PostgreSQL CHECK constraints.
type Level string

const (
	LevelVirtualKey            Level = "VIRTUAL_KEY"
	LevelProviderConnection    Level = "PROVIDER_CONNECTION"
	LevelExtensionInstallation Level = "EXTENSION_INSTALLATION"
	LevelProjectOrOrgFreeze    Level = "PROJECT_OR_ORG_FREEZE"
)

// Canonical kill-switch error sentinels.
var (
	ErrAlreadyCleared   = errors.New("killswitch: kill switch is already cleared")
	ErrAlreadyActive    = errors.New("killswitch: matching active kill switch already exists")
	ErrPermissionDenied = errors.New("killswitch: insufficient role permission")
)

// KillSwitch represents a persisted emergency_kill_switches record.
type KillSwitch struct {
	ID                     string  `json:"id"`
	OrgID                  *string `json:"org_id,omitempty"`
	Level                  Level   `json:"level"`
	TargetID               string  `json:"target_id"`
	Active                 bool    `json:"active"`
	AbortActiveStreams     bool    `json:"abort_active_streams"`
	Reason                 string  `json:"reason"`
	TriggeredByPrincipalID string  `json:"triggered_by_principal_id"`
	ClearedByPrincipalID   *string `json:"cleared_by_principal_id,omitempty"`
	ClearReason            *string `json:"clear_reason,omitempty"`
	Version                int64   `json:"version"`
	TriggeredAtMs          int64   `json:"triggered_at_ms"`
	ClearedAtMs            *int64  `json:"cleared_at_ms,omitempty"`
}

// StreamRegistration binds an active SSE/inference stream cancel callback across all 5 dimensions
// required by TP-IMP-0023 (org_id, project_id, vkey_id, connection_id, plugin_install_id).
type StreamRegistration struct {
	OrgID           string
	ProjectID       string
	VirtualKeyID    string
	ConnectionID    string
	PluginInstallID string
	Cancel          context.CancelFunc
}

type activeStreamEntry struct {
	id  uint64
	reg StreamRegistration
}

// StreamRegistry tracks active SSE/request cancellation handles indexed across
// (org_id, project_id, vkey_id, connection_id, plugin_install_id) for < 1s severing.
type StreamRegistry struct {
	mu      sync.Mutex
	nextID  uint64
	entries map[uint64]*activeStreamEntry
}

// NewStreamRegistry constructs an empty multi-dimensional StreamRegistry.
func NewStreamRegistry() *StreamRegistry {
	return &StreamRegistry{
		entries: make(map[uint64]*activeStreamEntry),
	}
}

// Register stores a cancellation handle and returns a unique registration ID.
func (s *StreamRegistry) Register(reg StreamRegistration) uint64 {
	if s == nil || reg.Cancel == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	s.entries[id] = &activeStreamEntry{
		id:  id,
		reg: reg,
	}
	return id
}

// UpdateTarget attaches or updates the resolved provider connection and extension installation
// dimensions on an already-registered stream without dropping cancellation continuity.
func (s *StreamRegistry) UpdateTarget(handleID uint64, connectionID, pluginInstallID string) {
	if s == nil || handleID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ent, ok := s.entries[handleID]; ok {
		ent.reg.ConnectionID = connectionID
		ent.reg.PluginInstallID = pluginInstallID
	}
}

// Unregister removes a finished stream cleanly.
func (s *StreamRegistry) Unregister(handleID uint64) {
	if s == nil || handleID == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, handleID)
}

// ActiveCount reports total currently tracked in-flight streams.
func (s *StreamRegistry) ActiveCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// AbortMatching removes and immediately cancels all active streams matching (level, orgID, targetID).
func (s *StreamRegistry) AbortMatching(level Level, orgID, targetID string) int {
	if s == nil || targetID == "" {
		return 0
	}
	s.mu.Lock()
	var cancels []context.CancelFunc
	for id, ent := range s.entries {
		if matchesStreamTarget(ent.reg, level, orgID, targetID) {
			cancels = append(cancels, ent.reg.Cancel)
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

func matchesStreamTarget(reg StreamRegistration, level Level, scopeOrgID, targetID string) bool {
	if scopeOrgID != "" && reg.OrgID != scopeOrgID {
		return false
	}
	switch level {
	case LevelVirtualKey:
		return reg.VirtualKeyID == targetID
	case LevelProviderConnection:
		return reg.ConnectionID == targetID
	case LevelExtensionInstallation:
		return reg.PluginInstallID == targetID
	case LevelProjectOrOrgFreeze:
		return reg.ProjectID == targetID || reg.OrgID == targetID
	default:
		return false
	}
}

// AdmissionQuery specifies the 5 dimensions checked before admitting or executing a request hop.
type AdmissionQuery struct {
	OrgID           string
	ProjectID       string
	VirtualKeyID    string
	ConnectionID    string
	PluginInstallID string
}

// TriggerParams parameterizes activation of a persistent 4-level emergency kill switch.
type TriggerParams struct {
	OrgID                  *string
	Level                  Level
	TargetID               string
	Reason                 string
	TriggeredByPrincipalID string
	EntryPoint             string // Defaults to port.AuditEntryPointAdminAPI.
	RequestID              *string
	AbortActiveStreams     *bool // nil defaults to true.
}

// ClearParams parameterizes audited clearing of an active emergency kill switch.
type ClearParams struct {
	OrgID                *string // Optional for instance OPERATOR clearing global EXTENSION_INSTALLATION; required for org-scoped switches.
	KillSwitchID         string
	ExpectedVersion      int64
	ClearedByPrincipalID string
	ClearReason          string
	EntryPoint           string // Defaults to port.AuditEntryPointAdminAPI.
	RequestID            *string
}

const (
	// DefaultPollInterval defines the fallback DB sync cadence when StartWatcher is used without snapshot.Manager wakeup.
	DefaultPollInterval = 500 * time.Millisecond
)

// ListFilter configures fail-closed kill-switch listing (requiring non-empty OrgID unless AllowInstanceScope is true).
type ListFilter struct {
	OrgID              string
	AllowInstanceScope bool
	ActiveOnly         bool
}

// RegistryConfig configures the 4-level Emergency Kill-Switch Registry.
type RegistryConfig struct {
	DB      *sql.DB
	Audit   port.AuditSink
	NowMs   func() int64
	Streams *StreamRegistry
	OnAbort func(level Level, orgID, targetID string) // Optional external aborter hook (e.g., snapshot.ActiveStreamRegistry).
}

// Registry persists emergency_kill_switches, enforces transactional audit + config_changelog
// atomicity, serves O(1)/lock-fast CheckAdmission lookups, and severs active SSE streams in < 1s.
type Registry struct {
	db      *sql.DB
	audit   port.TxAuditSink
	nowMs   func() int64
	streams *StreamRegistry
	onAbort func(level Level, orgID, targetID string)

	syncMu      sync.Mutex // Serializes DB-to-active-map sync across Trigger/Clear/RefreshFromDB before releasing for stream aborts.
	mu          sync.RWMutex
	initialized bool
	active      map[string]KillSwitch // keyed by KillSwitch.ID
}

// NewRegistry initializes a kill-switch Registry and hydrates active switches fail-closed from SQLite.
func NewRegistry(ctx context.Context, cfg RegistryConfig) (*Registry, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	if cfg.Audit == nil {
		return nil, fmt.Errorf("%w: non-nil port.TxAuditSink is required for fail-closed kill-switch audit", port.ErrInvalidInput)
	}
	txAudit, ok := cfg.Audit.(port.TxAuditSink)
	if !ok {
		return nil, fmt.Errorf("%w: Audit must implement port.TxAuditSink for transactional kill-switch audit", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	streams := cfg.Streams
	if streams == nil {
		streams = NewStreamRegistry()
	}
	r := &Registry{
		db:      cfg.DB,
		audit:   txAudit,
		nowMs:   nowFn,
		streams: streams,
		onAbort: cfg.OnAbort,
		active:  make(map[string]KillSwitch),
	}
	if err := r.RefreshFromDB(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Streams returns the multi-dimensional StreamRegistry wired to this kill-switch Registry.
func (r *Registry) Streams() *StreamRegistry {
	if r == nil {
		return nil
	}
	return r.streams
}

// RegisterStream registers an active SSE/request handle on this Registry's StreamRegistry.
func (r *Registry) RegisterStream(reg StreamRegistration) uint64 {
	if r == nil {
		return 0
	}
	return r.streams.Register(reg)
}

// UpdateStreamTarget binds connection_id and plugin_install_id onto a live stream handle.
func (r *Registry) UpdateStreamTarget(handleID uint64, connectionID, pluginInstallID string) {
	if r == nil {
		return
	}
	r.streams.UpdateTarget(handleID, connectionID, pluginInstallID)
}

// UnregisterStream removes a completed stream handle.
func (r *Registry) UnregisterStream(handleID uint64) {
	if r == nil {
		return
	}
	r.streams.Unregister(handleID)
}

// RefreshFromDB reloads all active emergency_kill_switches rows into memory and immediately
// severs any live streams matching kill switches activated out-of-band by peer processes.
func (r *Registry) RefreshFromDB(ctx context.Context) error {
	r.syncMu.Lock()
	syncLocked := true
	defer func() {
		if syncLocked {
			r.syncMu.Unlock()
		}
	}()

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, org_id, level, target_id, active, abort_active_streams, reason,
		       triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
		       triggered_at_ms, cleared_at_ms
		FROM emergency_kill_switches
		WHERE active = 1
		ORDER BY triggered_at_ms ASC`,
	)
	if err != nil {
		return fmt.Errorf("killswitch: refresh active switches: %w", err)
	}
	defer rows.Close()

	next := make(map[string]KillSwitch)
	for rows.Next() {
		ks, serr := scanKillSwitch(rows)
		if serr != nil {
			return serr
		}
		next[ks.ID] = ks
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("killswitch: iterate active switches: %w", err)
	}

	var toAbort []KillSwitch
	r.mu.Lock()
	if r.initialized {
		for id, ks := range next {
			if _, existed := r.active[id]; !existed && ks.AbortActiveStreams {
				toAbort = append(toAbort, ks)
			}
		}
	}
	r.active = next
	r.initialized = true
	r.mu.Unlock()

	syncLocked = false
	r.syncMu.Unlock()

	for _, ks := range toAbort {
		scopeOrg := ""
		if ks.OrgID != nil {
			scopeOrg = *ks.OrgID
		}
		r.streams.AbortMatching(ks.Level, scopeOrg, ks.TargetID)
		if r.onAbort != nil {
			r.onAbort(ks.Level, scopeOrg, ks.TargetID)
		}
	}
	return nil
}

// StartWatcher periodically polls emergency_kill_switches when running standalone without
// snapshot.Manager event-driven synchronization until ctx is canceled.
func (r *Registry) StartWatcher(ctx context.Context, interval time.Duration) {
	if r == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.RefreshFromDB(ctx)
		}
	}
}

// CheckAdmission reports whether any active kill switch blocks the queried coordinates.
func (r *Registry) CheckAdmission(q AdmissionQuery) (*KillSwitch, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, ks := range r.active {
		if !ks.Active {
			continue
		}
		if ks.OrgID != nil && *ks.OrgID != "" && q.OrgID != *ks.OrgID {
			continue
		}
		switch ks.Level {
		case LevelProjectOrOrgFreeze:
			if (q.OrgID != "" && ks.TargetID == q.OrgID) || (q.ProjectID != "" && ks.TargetID == q.ProjectID) {
				cp := ks
				return &cp, true
			}
		case LevelVirtualKey:
			if q.VirtualKeyID != "" && ks.TargetID == q.VirtualKeyID {
				cp := ks
				return &cp, true
			}
		case LevelProviderConnection:
			if q.ConnectionID != "" && ks.TargetID == q.ConnectionID {
				cp := ks
				return &cp, true
			}
		case LevelExtensionInstallation:
			if q.PluginInstallID != "" && ks.TargetID == q.PluginInstallID {
				cp := ks
				return &cp, true
			}
		}
	}
	return nil, false
}

// Trigger activates an emergency kill switch, writes config_changelog (emergency=1) and
// an immutable audit_events row inside the exact same transaction, updates the in-memory
// admission map, and severs all matching active SSE streams immediately (< 1s).
func (r *Registry) Trigger(ctx context.Context, p TriggerParams) (*KillSwitch, error) {
	targetID := strings.TrimSpace(p.TargetID)
	reason := strings.TrimSpace(p.Reason)
	principalID := strings.TrimSpace(p.TriggeredByPrincipalID)
	if !isValidLevel(p.Level) || targetID == "" || principalID == "" {
		return nil, fmt.Errorf("%w: valid level, target_id, and triggered_by_principal_id are required", port.ErrInvalidInput)
	}
	if len(reason) < 5 {
		return nil, fmt.Errorf("%w: kill-switch reason must be at least 5 characters", port.ErrInvalidInput)
	}

	var orgPtr *string
	var orgID string
	if p.OrgID != nil && strings.TrimSpace(*p.OrgID) != "" {
		o := strings.TrimSpace(*p.OrgID)
		orgPtr = &o
		orgID = o
	}
	if orgID == "" && p.Level != LevelExtensionInstallation {
		return nil, fmt.Errorf("%w: org_id is required for kill-switch level %s", port.ErrInvalidInput, p.Level)
	}

	entryPoint := strings.TrimSpace(p.EntryPoint)
	if entryPoint == "" {
		entryPoint = port.AuditEntryPointAdminAPI
	}
	abortStreams := true
	if p.AbortActiveStreams != nil {
		abortStreams = *p.AbortActiveStreams
	}

	ksID, err := idgen.New(idgen.PrefixKillSwitch)
	if err != nil {
		return nil, fmt.Errorf("killswitch: generate id: %w", err)
	}
	now := r.nowMs()

	r.syncMu.Lock()
	syncLocked := true
	defer func() {
		if syncLocked {
			r.syncMu.Unlock()
		}
	}()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("killswitch: begin trigger tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := verifyPrincipalAndTargetTx(ctx, tx, orgID, p.Level, targetID, principalID); err != nil {
		return nil, err
	}

	var existingActive int
	if orgID != "" {
		err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM emergency_kill_switches
			WHERE active = 1 AND level = ? AND target_id = ? AND org_id = ?`,
			string(p.Level), targetID, orgID,
		).Scan(&existingActive)
	} else {
		err = tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM emergency_kill_switches
			WHERE active = 1 AND level = ? AND target_id = ? AND org_id IS NULL`,
			string(p.Level), targetID,
		).Scan(&existingActive)
	}
	if err != nil {
		return nil, fmt.Errorf("killswitch: check active duplicate: %w", err)
	}
	if existingActive > 0 {
		return nil, ErrAlreadyActive
	}

	var orgArg any
	if orgPtr != nil {
		orgArg = *orgPtr
	}
	abortInt := 0
	if abortStreams {
		abortInt = 1
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO emergency_kill_switches (
			id, org_id, level, target_id, active, abort_active_streams, reason,
			triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
			triggered_at_ms, cleared_at_ms
		) VALUES (?, ?, ?, ?, 1, ?, ?, ?, NULL, NULL, 1, ?, NULL)`,
		ksID, orgArg, string(p.Level), targetID, abortInt, reason, principalID, now,
	); err != nil {
		return nil, fmt.Errorf("killswitch: insert kill switch: %w", err)
	}

	mutType := port.MutationRevoke
	if p.Level == LevelExtensionInstallation {
		mutType = port.MutationQuarantine
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'emergency_kill_switch', ?, ?, 1, ?)`,
		orgArg, ksID, mutType, now,
	); err != nil {
		return nil, fmt.Errorf("killswitch: append config_changelog: %w", err)
	}

	if err := r.emitAuditTx(ctx, tx, orgPtr, principalID, entryPoint, p.RequestID, "kill_switch.triggered", ksID, map[string]any{
		"level":                string(p.Level),
		"target_id":            targetID,
		"reason":               reason,
		"abort_active_streams": abortStreams,
	}, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("killswitch: commit trigger: %w", err)
	}

	ks := KillSwitch{
		ID:                     ksID,
		OrgID:                  orgPtr,
		Level:                  p.Level,
		TargetID:               targetID,
		Active:                 true,
		AbortActiveStreams:     abortStreams,
		Reason:                 reason,
		TriggeredByPrincipalID: principalID,
		Version:                1,
		TriggeredAtMs:          now,
	}

	r.mu.Lock()
	r.active[ks.ID] = ks
	r.mu.Unlock()

	syncLocked = false
	r.syncMu.Unlock()

	if abortStreams {
		r.streams.AbortMatching(p.Level, orgID, targetID)
		if r.onAbort != nil {
			r.onAbort(p.Level, orgID, targetID)
		}
	}

	return &ks, nil
}

// Clear lifts an active emergency kill switch under optimistic locking and records
// a mandatory audit trail with clear_reason inside the exact same transaction.
func (r *Registry) Clear(ctx context.Context, p ClearParams) (*KillSwitch, error) {
	ksID := strings.TrimSpace(p.KillSwitchID)
	principalID := strings.TrimSpace(p.ClearedByPrincipalID)
	clearReason := strings.TrimSpace(p.ClearReason)
	if ksID == "" || principalID == "" || p.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: kill_switch_id, cleared_by_principal_id, and expected_version >= 1 are required", port.ErrInvalidInput)
	}
	if len(clearReason) < 5 {
		return nil, fmt.Errorf("%w: clear_reason must be at least 5 characters", port.ErrInvalidInput)
	}

	var scopeOrgID string
	if p.OrgID != nil && strings.TrimSpace(*p.OrgID) != "" {
		scopeOrgID = strings.TrimSpace(*p.OrgID)
	}

	entryPoint := strings.TrimSpace(p.EntryPoint)
	if entryPoint == "" {
		entryPoint = port.AuditEntryPointAdminAPI
	}
	now := r.nowMs()

	r.syncMu.Lock()
	syncLocked := true
	defer func() {
		if syncLocked {
			r.syncMu.Unlock()
		}
	}()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("killswitch: begin clear tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Fail closed BEFORE querying emergency_kill_switches to avoid any cross-tenant existence oracle:
	// when scopeOrgID == "", caller MUST be an ACTIVE INSTANCE OPERATOR; when scopeOrgID != "",
	// caller MUST be an ACTIVE INSTANCE OPERATOR or belong to scopeOrgID.
	if err := verifyPrincipalMembershipForScopeTx(ctx, tx, scopeOrgID, principalID); err != nil {
		return nil, err
	}

	var ks KillSwitch
	if scopeOrgID != "" {
		ks, err = scanKillSwitch(tx.QueryRowContext(ctx, `
			SELECT id, org_id, level, target_id, active, abort_active_streams, reason,
			       triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
			       triggered_at_ms, cleared_at_ms
			FROM emergency_kill_switches
			WHERE id = ? AND org_id = ?`, ksID, scopeOrgID))
	} else {
		ks, err = scanKillSwitch(tx.QueryRowContext(ctx, `
			SELECT id, org_id, level, target_id, active, abort_active_streams, reason,
			       triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
			       triggered_at_ms, cleared_at_ms
			FROM emergency_kill_switches
			WHERE id = ? AND org_id IS NULL AND level = 'EXTENSION_INSTALLATION'`, ksID))
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// Verify exact RBAC role authorization against the resolved target/level.
	if err := verifyPrincipalRoleForScopeTx(ctx, tx, scopeOrgID, ks.Level, ks.TargetID, principalID); err != nil {
		return nil, err
	}

	if ks.Version != p.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}
	if !ks.Active {
		return nil, ErrAlreadyCleared
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE emergency_kill_switches
		SET active = 0, cleared_by_principal_id = ?, clear_reason = ?, cleared_at_ms = ?, version = version + 1
		WHERE id = ? AND version = ? AND active = 1`,
		principalID, clearReason, now, ksID, p.ExpectedVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("killswitch: update clear state: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, port.ErrVersionConflict
	}

	var orgArg any
	if ks.OrgID != nil {
		orgArg = *ks.OrgID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'emergency_kill_switch', ?, ?, 0, ?)`,
		orgArg, ksID, port.MutationUpsert, now,
	); err != nil {
		return nil, fmt.Errorf("killswitch: append config_changelog on clear: %w", err)
	}

	if err := r.emitAuditTx(ctx, tx, ks.OrgID, principalID, entryPoint, p.RequestID, "kill_switch.cleared", ksID, map[string]any{
		"level":        string(ks.Level),
		"target_id":    ks.TargetID,
		"clear_reason": clearReason,
	}, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("killswitch: commit clear: %w", err)
	}

	r.mu.Lock()
	delete(r.active, ks.ID)
	r.mu.Unlock()

	syncLocked = false
	r.syncMu.Unlock()

	ks.Active = false
	ks.ClearedByPrincipalID = &principalID
	ks.ClearReason = &clearReason
	ks.ClearedAtMs = &now
	ks.Version++
	return &ks, nil
}

// Get retrieves an organization-scoped kill switch strictly bound to non-empty orgID.
// For instance-wide kill switches (org_id IS NULL), use GetInstance.
func (r *Registry) Get(ctx context.Context, orgID, killSwitchID string) (*KillSwitch, error) {
	trimmedOrg := strings.TrimSpace(orgID)
	trimmedKS := strings.TrimSpace(killSwitchID)
	if trimmedOrg == "" || trimmedKS == "" {
		return nil, fmt.Errorf("%w: non-empty org_id and kill_switch_id are required", port.ErrInvalidInput)
	}
	ks, err := scanKillSwitch(r.db.QueryRowContext(ctx, `
		SELECT id, org_id, level, target_id, active, abort_active_streams, reason,
		       triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
		       triggered_at_ms, cleared_at_ms
		FROM emergency_kill_switches
		WHERE id = ? AND org_id = ?`, trimmedKS, trimmedOrg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ks, nil
}

// GetInstance retrieves a global instance-scoped kill switch (org_id IS NULL).
// Callers MUST authorize INSTANCE OPERATOR prior to invoking this method.
func (r *Registry) GetInstance(ctx context.Context, killSwitchID string) (*KillSwitch, error) {
	trimmedKS := strings.TrimSpace(killSwitchID)
	if trimmedKS == "" {
		return nil, fmt.Errorf("%w: kill_switch_id is required", port.ErrInvalidInput)
	}
	ks, err := scanKillSwitch(r.db.QueryRowContext(ctx, `
		SELECT id, org_id, level, target_id, active, abort_active_streams, reason,
		       triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
		       triggered_at_ms, cleared_at_ms
		FROM emergency_kill_switches
		WHERE id = ? AND org_id IS NULL`, trimmedKS))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ks, nil
}

// List returns kill switches strictly scoped to non-empty orgID.
// To include or query instance-wide switches, callers must use ListWithFilter with AllowInstanceScope=true.
func (r *Registry) List(ctx context.Context, orgID string, activeOnly bool) ([]KillSwitch, error) {
	trimmedOrg := strings.TrimSpace(orgID)
	if trimmedOrg == "" {
		return nil, fmt.Errorf("%w: non-empty org_id is required", port.ErrInvalidInput)
	}
	return r.ListWithFilter(ctx, ListFilter{
		OrgID:      trimmedOrg,
		ActiveOnly: activeOnly,
	})
}

// ListWithFilter returns kill switches filtered by ListFilter, rejecting empty OrgID unless AllowInstanceScope is true.
func (r *Registry) ListWithFilter(ctx context.Context, filter ListFilter) ([]KillSwitch, error) {
	trimmedOrg := strings.TrimSpace(filter.OrgID)
	if trimmedOrg == "" && !filter.AllowInstanceScope {
		return nil, fmt.Errorf("%w: org_id is required unless AllowInstanceScope is enabled", port.ErrInvalidInput)
	}

	query := `
		SELECT id, org_id, level, target_id, active, abort_active_streams, reason,
		       triggered_by_principal_id, cleared_by_principal_id, clear_reason, version,
		       triggered_at_ms, cleared_at_ms
		FROM emergency_kill_switches
		WHERE 1=1`
	var args []any

	if trimmedOrg != "" {
		if filter.AllowInstanceScope {
			query += ` AND (org_id = ? OR org_id IS NULL)`
			args = append(args, trimmedOrg)
		} else {
			query += ` AND org_id = ?`
			args = append(args, trimmedOrg)
		}
	}
	if filter.ActiveOnly {
		query += ` AND active = 1`
	}
	query += ` ORDER BY triggered_at_ms DESC`

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("killswitch: list switches: %w", err)
	}
	defer rows.Close()

	var out []KillSwitch
	for rows.Next() {
		ks, serr := scanKillSwitch(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, ks)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("killswitch: iterate switches: %w", err)
	}
	return out, nil
}

func (r *Registry) emitAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	orgPtr *string,
	actorID, entryPoint string,
	reqID *string,
	eventType, targetID string,
	meta map[string]any,
	nowMs int64,
) error {
	if r.audit == nil {
		return fmt.Errorf("%w: non-nil port.TxAuditSink is required for transactional kill-switch audit", port.ErrInvalidInput)
	}
	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return fmt.Errorf("killswitch: mint audit id: %w", err)
	}
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("killswitch: marshal audit metadata: %w", err)
	}
	return r.audit.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  nowMs,
		OrgID:        orgPtr,
		ActorKind:    "PRINCIPAL",
		ActorID:      actorID,
		EntryPoint:   entryPoint,
		EventType:    eventType,
		TargetType:   "emergency_kill_switch",
		TargetID:     targetID,
		RequestID:    reqID,
		MetadataJSON: string(rawMeta),
	})
}

func verifyPrincipalAndTargetTx(ctx context.Context, tx *sql.Tx, orgID string, level Level, targetID, principalID string) error {
	if err := verifyPrincipalMembershipForScopeTx(ctx, tx, orgID, principalID); err != nil {
		return err
	}
	if err := verifyPrincipalRoleForScopeTx(ctx, tx, orgID, level, targetID, principalID); err != nil {
		return err
	}

	var exists int
	var err error
	switch level {
	case LevelVirtualKey:
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM virtual_keys WHERE org_id = ? AND id = ?`, orgID, targetID).Scan(&exists)
	case LevelProviderConnection:
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM provider_connections WHERE org_id = ? AND id = ?`, orgID, targetID).Scan(&exists)
	case LevelExtensionInstallation:
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM extension_installations WHERE id = ?`, targetID).Scan(&exists)
	case LevelProjectOrOrgFreeze:
		if targetID == orgID {
			err = tx.QueryRowContext(ctx, `SELECT 1 FROM organizations WHERE id = ?`, orgID).Scan(&exists)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, targetID).Scan(&exists)
		}
	default:
		return fmt.Errorf("%w: unsupported kill-switch level %q", port.ErrInvalidInput, level)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return port.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("killswitch: verify target %s (%s): %w", level, targetID, err)
	}
	return nil
}

func verifyPrincipalMembershipForScopeTx(ctx context.Context, tx *sql.Tx, orgID, principalID string) error {
	var (
		pOrgID sql.NullString
		status string
	)
	err := tx.QueryRowContext(ctx, `SELECT org_id, status FROM principals WHERE id = ?`, principalID).Scan(&pOrgID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return port.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("killswitch: verify principal: %w", err)
	}
	if status != "ACTIVE" {
		return fmt.Errorf("%w: principal %q is not ACTIVE (status=%s)", port.ErrInvalidInput, principalID, status)
	}
	opCount, err := countInstanceOperatorRoleTx(ctx, tx, principalID)
	if err != nil {
		return err
	}
	if opCount > 0 {
		return nil
	}
	if orgID == "" {
		return ErrPermissionDenied
	}
	if !pOrgID.Valid || pOrgID.String != orgID {
		return port.ErrNotFound
	}
	return nil
}

func verifyPrincipalRoleForScopeTx(ctx context.Context, tx *sql.Tx, orgID string, level Level, targetID, principalID string) error {
	opCount, err := countInstanceOperatorRoleTx(ctx, tx, principalID)
	if err != nil {
		return err
	}
	if opCount > 0 {
		return nil
	}
	if orgID == "" {
		return ErrPermissionDenied
	}

	var orgRoleCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM role_bindings
		WHERE principal_id = ?
		  AND org_id = ?
		  AND scope_type = 'ORG'
		  AND scope_id = ?
		  AND role_name IN ('ORG_OWNER', 'SECURITY_REVIEWER')`,
		principalID, orgID, orgID,
	).Scan(&orgRoleCount); err != nil {
		return fmt.Errorf("killswitch: verify org kill-switch role binding: %w", err)
	}
	if orgRoleCount > 0 {
		return nil
	}

	// Permit TEAM_ADMIN within their own PROJECT scope (project freeze or virtual key in that project).
	var projRoleCount int
	switch {
	case level == LevelProjectOrOrgFreeze && targetID != orgID:
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM role_bindings
			WHERE principal_id = ?
			  AND org_id = ?
			  AND scope_type = 'PROJECT'
			  AND scope_id = ?
			  AND role_name = 'TEAM_ADMIN'`,
			principalID, orgID, targetID,
		).Scan(&projRoleCount); err != nil {
			return fmt.Errorf("killswitch: verify project role binding: %w", err)
		}
	case level == LevelVirtualKey:
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM virtual_keys vk
			JOIN role_bindings rb
			  ON rb.org_id = vk.org_id
			 AND rb.scope_type = 'PROJECT'
			 AND rb.scope_id = vk.project_id
			WHERE vk.org_id = ?
			  AND vk.id = ?
			  AND rb.principal_id = ?
			  AND rb.role_name = 'TEAM_ADMIN'`,
			orgID, targetID, principalID,
		).Scan(&projRoleCount); err != nil {
			return fmt.Errorf("killswitch: verify project virtual key role binding: %w", err)
		}
	}
	if projRoleCount > 0 {
		return nil
	}
	return ErrPermissionDenied
}

func countInstanceOperatorRoleTx(ctx context.Context, tx *sql.Tx, principalID string) (int, error) {
	var opCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM role_bindings
		WHERE principal_id = ? AND scope_type = 'INSTANCE' AND role_name = 'OPERATOR'`,
		principalID,
	).Scan(&opCount); err != nil {
		return 0, fmt.Errorf("killswitch: verify operator role binding: %w", err)
	}
	return opCount, nil
}

func scanKillSwitch(s interface{ Scan(...any) error }) (KillSwitch, error) {
	var (
		ks                     KillSwitch
		levelStr               string
		orgNS, clearedBy, crNS sql.NullString
		clearedAt              sql.NullInt64
		activeInt, abortInt    int
	)
	if err := s.Scan(
		&ks.ID, &orgNS, &levelStr, &ks.TargetID, &activeInt, &abortInt, &ks.Reason,
		&ks.TriggeredByPrincipalID, &clearedBy, &crNS, &ks.Version, &ks.TriggeredAtMs, &clearedAt,
	); err != nil {
		return KillSwitch{}, err
	}
	ks.Level = Level(levelStr)
	ks.Active = activeInt != 0
	ks.AbortActiveStreams = abortInt != 0
	if orgNS.Valid {
		v := orgNS.String
		ks.OrgID = &v
	}
	if clearedBy.Valid {
		v := clearedBy.String
		ks.ClearedByPrincipalID = &v
	}
	if crNS.Valid {
		v := crNS.String
		ks.ClearReason = &v
	}
	if clearedAt.Valid {
		v := clearedAt.Int64
		ks.ClearedAtMs = &v
	}
	return ks, nil
}

func isValidLevel(l Level) bool {
	switch l {
	case LevelVirtualKey, LevelProviderConnection, LevelExtensionInstallation, LevelProjectOrOrgFreeze:
		return true
	default:
		return false
	}
}
