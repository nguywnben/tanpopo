// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package snapshot maintains an immutable, lock-free Copy-on-Write ConfigSnapshot
// backed by atomic.Pointer[ConfigSnapshot] and an ActiveStreamRegistry for
// sub-second emergency revocation enforcement (ADR-0006 / TP-IMP-0009).
package snapshot

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Default staleness and polling bounds per ADR-0006.
const (
	DefaultPollInterval = 250 * time.Millisecond
	DefaultMaxStaleMs   = int64(10_000)
	maxRawKeyStackBytes = 256
)

// Canonical Virtual Key states matching SM-05 and SQLite/PostgreSQL CHECK constraints.
const (
	KeyStateCreatedActive           = "CREATED_ACTIVE"
	KeyStateRotatingDualValid       = "ROTATING_DUAL_VALID"
	KeyStateQuotaPaused             = "QUOTA_PAUSED"
	KeyStateSuspended               = "SUSPENDED"
	KeyStateRevoked                 = "REVOKED"
	KeyStateExpired                 = "EXPIRED"
	KeyStatePurgedMetadataRetained  = "PURGED_METADATA_RETAINED"
)

// Hot-path Virtual Key authentication error sentinels mapped to canonical HTTP codes.
var (
	ErrSnapshotNotLoaded      = errors.New("snapshot: configuration snapshot not yet initialized")
	ErrKeyNotFound            = errors.New("snapshot: invalid virtual key")
	ErrKeyRevoked             = errors.New("snapshot: virtual_key_revoked")
	ErrKeyExpired             = errors.New("snapshot: virtual_key_expired")
	ErrKeySuspended           = errors.New("snapshot: virtual_key_suspended")
	ErrKeyQuotaPaused         = errors.New("snapshot: virtual_key_quota_paused")
	ErrEnvironmentScopeDenied = errors.New("snapshot: environment_scope_denied")
	ErrTenantSuspended        = errors.New("snapshot: tenant_or_project_inactive")
)

// ScopeKey uniquely identifies an (OrgID, ProjectID, EnvID) execution tuple.
type ScopeKey struct {
	OrgID     string
	ProjectID string
	EnvID     string
}

// VirtualKeyView is an immutable, hash-only Data Plane view of a Virtual Key.
// It contains zero plaintext key material.
type VirtualKeyView struct {
	ID                    string
	OrgID                 string
	ProjectID             string
	EnvID                 string
	OwnerPrincipalID      *string
	Name                  string
	KeyPrefix             string
	LastFour              string
	ActiveKeyHash         [32]byte
	PreviousKeyHash       [32]byte
	HasPreviousKeyHash    bool
	GraceExpiresAtMs      *int64
	State                 string
	AllowedPlanes         string
	AllowedModels         []string
	AllowedRoutes         []string
	AllowedMCPTools       []string
	RPMLimit              *int64
	TPMLimit              *int64
	MaxConcurrentStreams  int64
	IsEphemeralPlayground bool
	ExpiresAtMs           *int64
	RevokedAtMs           *int64
	RevocationMode        *string
	Version               int64
}

// EffectiveConfig captures immutable read-only tenant/project/environment settings
// hydrated into a ConfigSnapshot for lock-free hot-path evaluation.
type EffectiveConfig struct {
	OrgID                   string
	OrgSlug                 string
	OrgStatus               string
	DefaultDataRegion       *string
	MinPluginTrust          string
	RequireFourEyesApproval bool
	ProjectID               string
	ProjectSlug             string
	ProjectStatus           string
	EnforcedRegion          *string
	EnvID                   string
	EnvSlug                 string
	RequireTestedCaps       bool
}

// IsOperational reports whether the Organization and Project are ACTIVE for inference.
func (e *EffectiveConfig) IsOperational() bool {
	if e == nil {
		return false
	}
	return e.OrgStatus == port.OrgStatusActive && e.ProjectStatus == port.ProjectStatusActive
}

// ConfigSnapshot is an immutable point-in-time read-only view of all active
// Virtual Keys and tenant EffectiveConfig entries.
type ConfigSnapshot struct {
	Epoch            int64
	LoadedAtMs       int64
	byActiveHash     map[[32]byte]*VirtualKeyView
	byPrevHash       map[[32]byte]*VirtualKeyView
	byKeyID          map[string]*VirtualKeyView
	effectiveConfigs map[ScopeKey]*EffectiveConfig
}

// KeyMatchResult returns the resolved immutable VirtualKeyView and whether authentication
// matched the previous grace-window key digest.
type KeyMatchResult struct {
	Key             *VirtualKeyView
	Config          *EffectiveConfig
	MatchedPrevious bool
}

// HashRawVirtualKey computes the canonical 32-byte SHA-256 digest of a raw Virtual Key
// using a fixed stack buffer so short keys incur 0 heap allocations.
func HashRawVirtualKey(rawKey string) ([32]byte, bool) {
	if len(rawKey) == 0 || len(rawKey) > maxRawKeyStackBytes {
		if len(rawKey) == 0 {
			return [32]byte{}, false
		}
		return sha256.Sum256([]byte(rawKey)), true
	}
	var stackBuf [maxRawKeyStackBytes]byte
	n := copy(stackBuf[:], rawKey)
	sum := sha256.Sum256(stackBuf[:n])
	clear(stackBuf[:n])
	return sum, true
}

// LookupVirtualKeyDigest performs an O(1), zero-allocation lookup of a SHA-256 Virtual Key digest.
func (s *ConfigSnapshot) LookupVirtualKeyDigest(digest [32]byte) (*VirtualKeyView, bool, bool) {
	if s == nil {
		return nil, false, false
	}
	if v, ok := s.byActiveHash[digest]; ok {
		if subtle.ConstantTimeCompare(v.ActiveKeyHash[:], digest[:]) == 1 {
			return v, false, true
		}
	}
	if v, ok := s.byPrevHash[digest]; ok {
		if v.HasPreviousKeyHash && subtle.ConstantTimeCompare(v.PreviousKeyHash[:], digest[:]) == 1 {
			return v, true, true
		}
	}
	return nil, false, false
}

// LookupVirtualKey hashes rawKey on the stack and looks up the immutable VirtualKeyView with 0 allocs/op.
func (s *ConfigSnapshot) LookupVirtualKey(rawKey string) (*VirtualKeyView, bool, bool) {
	if s == nil || rawKey == "" {
		return nil, false, false
	}
	digest, ok := HashRawVirtualKey(rawKey)
	if !ok {
		return nil, false, false
	}
	return s.LookupVirtualKeyDigest(digest)
}

// LookupEffectiveConfig performs an O(1), zero-allocation lookup by (orgID, projectID, envID).
func (s *ConfigSnapshot) LookupEffectiveConfig(orgID, projectID, envID string) (*EffectiveConfig, bool) {
	if s == nil {
		return nil, false
	}
	cfg, ok := s.effectiveConfigs[ScopeKey{
		OrgID:     orgID,
		ProjectID: projectID,
		EnvID:     envID,
	}]
	return cfg, ok
}

// LookupVirtualKeyByID returns a key's snapshot view by keyID in O(1).
func (s *ConfigSnapshot) LookupVirtualKeyByID(keyID string) (*VirtualKeyView, bool) {
	if s == nil || keyID == "" {
		return nil, false
	}
	v, ok := s.byKeyID[keyID]
	return v, ok
}

// AuthenticateKey validates rawKey against SM-05 lifecycle rules, grace expiry,
// key TTL expiration, tenant operational status, and optional targetEnvID scope.
func (s *ConfigSnapshot) AuthenticateKey(rawKey, targetEnvID string, nowMs int64) (KeyMatchResult, error) {
	if s == nil {
		return KeyMatchResult{}, ErrSnapshotNotLoaded
	}
	view, matchedPrev, ok := s.LookupVirtualKey(rawKey)
	if !ok {
		return KeyMatchResult{}, ErrKeyNotFound
	}
	return s.evaluateView(view, matchedPrev, targetEnvID, nowMs)
}

// AuthenticateDigest validates a precomputed SHA-256 digest against SM-05 rules.
func (s *ConfigSnapshot) AuthenticateDigest(digest [32]byte, targetEnvID string, nowMs int64) (KeyMatchResult, error) {
	if s == nil {
		return KeyMatchResult{}, ErrSnapshotNotLoaded
	}
	view, matchedPrev, ok := s.LookupVirtualKeyDigest(digest)
	if !ok {
		return KeyMatchResult{}, ErrKeyNotFound
	}
	return s.evaluateView(view, matchedPrev, targetEnvID, nowMs)
}

func (s *ConfigSnapshot) evaluateView(view *VirtualKeyView, matchedPrev bool, targetEnvID string, nowMs int64) (KeyMatchResult, error) {
	switch view.State {
	case KeyStateRevoked, KeyStatePurgedMetadataRetained:
		return KeyMatchResult{}, ErrKeyRevoked
	case KeyStateExpired:
		return KeyMatchResult{}, ErrKeyExpired
	}

	if view.ExpiresAtMs != nil && nowMs >= *view.ExpiresAtMs {
		return KeyMatchResult{}, ErrKeyExpired
	}

	switch view.State {
	case KeyStateSuspended:
		return KeyMatchResult{}, ErrKeySuspended
	case KeyStateQuotaPaused:
		return KeyMatchResult{}, ErrKeyQuotaPaused
	case KeyStateCreatedActive:
		if matchedPrev {
			return KeyMatchResult{}, ErrKeyExpired
		}
	case KeyStateRotatingDualValid:
		if matchedPrev {
			if view.GraceExpiresAtMs == nil || nowMs >= *view.GraceExpiresAtMs {
				return KeyMatchResult{}, ErrKeyExpired
			}
		}
	default:
		return KeyMatchResult{}, ErrKeyRevoked
	}

	cfg, cfgOK := s.LookupEffectiveConfig(view.OrgID, view.ProjectID, view.EnvID)
	if !cfgOK || !cfg.IsOperational() {
		return KeyMatchResult{}, ErrTenantSuspended
	}

	if targetEnvID != "" && view.EnvID != targetEnvID {
		return KeyMatchResult{}, ErrEnvironmentScopeDenied
	}

	return KeyMatchResult{
		Key:             view,
		Config:          cfg,
		MatchedPrevious: matchedPrev,
	}, nil
}

// ActiveStreamRegistry tracks in-flight SSE/request cancellation handles keyed by
// virtual_key_id so emergency revocation or org/project erasure aborts active streams immediately.
type ActiveStreamRegistry struct {
	mu      sync.Mutex
	nextID  uint64
	streams map[string]map[uint64]streamHandle
}

type streamHandle struct {
	orgID     string
	projectID string
	cancel    context.CancelFunc
}

// NewActiveStreamRegistry constructs an empty ActiveStreamRegistry.
func NewActiveStreamRegistry() *ActiveStreamRegistry {
	return &ActiveStreamRegistry{
		streams: make(map[string]map[uint64]streamHandle),
	}
}

// Register binds a stream cancellation function to (keyID, orgID, projectID) and returns its handle ID.
func (r *ActiveStreamRegistry) Register(orgID, projectID, keyID string, cancel context.CancelFunc) uint64 {
	if r == nil || keyID == "" || cancel == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	byStream := r.streams[keyID]
	if byStream == nil {
		byStream = make(map[uint64]streamHandle)
		r.streams[keyID] = byStream
	}
	byStream[id] = streamHandle{
		orgID:     orgID,
		projectID: projectID,
		cancel:    cancel,
	}
	return id
}

// Unregister removes a finished stream handle cleanly without leaking memory.
func (r *ActiveStreamRegistry) Unregister(keyID string, handleID uint64) {
	if r == nil || keyID == "" || handleID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	byStream := r.streams[keyID]
	if byStream == nil {
		return
	}
	delete(byStream, handleID)
	if len(byStream) == 0 {
		delete(r.streams, keyID)
	}
}

// AbortKey cancels all registered active streams for keyID and returns the aborted count.
func (r *ActiveStreamRegistry) AbortKey(keyID string) int {
	if r == nil || keyID == "" {
		return 0
	}
	r.mu.Lock()
	byStream := r.streams[keyID]
	if len(byStream) == 0 {
		r.mu.Unlock()
		return 0
	}
	cancels := make([]context.CancelFunc, 0, len(byStream))
	for _, h := range byStream {
		cancels = append(cancels, h.cancel)
	}
	delete(r.streams, keyID)
	r.mu.Unlock()

	for _, c := range cancels {
		c()
	}
	return len(cancels)
}

// AbortScope cancels all registered active streams matching orgID (and optional projectID).
func (r *ActiveStreamRegistry) AbortScope(orgID, projectID string) int {
	if r == nil || orgID == "" {
		return 0
	}
	r.mu.Lock()
	var cancels []context.CancelFunc
	for keyID, byStream := range r.streams {
		for handleID, h := range byStream {
			if h.orgID == orgID && (projectID == "" || h.projectID == projectID) {
				cancels = append(cancels, h.cancel)
				delete(byStream, handleID)
			}
		}
		if len(byStream) == 0 {
			delete(r.streams, keyID)
		}
	}
	r.mu.Unlock()

	for _, c := range cancels {
		c()
	}
	return len(cancels)
}

// ActiveCount returns the total number of currently tracked active streams.
func (r *ActiveStreamRegistry) ActiveCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, m := range r.streams {
		total += len(m)
	}
	return total
}

// ManagerConfig configures the Copy-on-Write ConfigSnapshot Manager.
type ManagerConfig struct {
	DB         *sql.DB
	NowMs      func() int64
	MaxStaleMs int64
	Streams    *ActiveStreamRegistry
}

// Manager atomically publishes immutable ConfigSnapshot updates by tailing config_changelog
// and aborts active SSE streams immediately on emergency revocations/shreds.
type Manager struct {
	db                *sql.DB
	nowMs             func() int64
	maxStaleMs        int64
	streams           *ActiveStreamRegistry
	snap              atomic.Pointer[ConfigSnapshot]
	lastPollSuccessMs atomic.Int64
	lastRefreshErrMu  sync.Mutex
	lastRefreshErr    error
	refreshMu         sync.Mutex
	wakeCh            chan struct{}
}

// NewManager initializes a ConfigSnapshot Manager and loads an initial snapshot if ctx is ready.
func NewManager(ctx context.Context, cfg ManagerConfig) (*Manager, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	maxStale := cfg.MaxStaleMs
	if maxStale <= 0 {
		maxStale = DefaultMaxStaleMs
	}
	streams := cfg.Streams
	if streams == nil {
		streams = NewActiveStreamRegistry()
	}
	m := &Manager{
		db:         cfg.DB,
		nowMs:      nowFn,
		maxStaleMs: maxStale,
		streams:    streams,
		wakeCh:     make(chan struct{}, 1),
	}
	if err := m.Refresh(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

// Current returns the active immutable ConfigSnapshot via a single lock-free atomic load.
func (m *Manager) Current() *ConfigSnapshot {
	if m == nil {
		return nil
	}
	return m.snap.Load()
}

// Streams returns the ActiveStreamRegistry wired to this snapshot Manager.
func (m *Manager) Streams() *ActiveStreamRegistry {
	if m == nil {
		return nil
	}
	return m.streams
}

// Notify triggers an immediate non-blocking wakeup for StartWatcher.
func (m *Manager) Notify() {
	if m == nil {
		return
	}
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
}

// IsStale reports whether the snapshot manager has exceeded MaxStaleMs without a successful sync.
func (m *Manager) IsStale() bool {
	if m == nil {
		return true
	}
	last := m.lastPollSuccessMs.Load()
	if last == 0 {
		return true
	}
	return (m.nowMs() - last) > m.maxStaleMs
}

// LastError returns the most recent poll/refresh error, if any.
func (m *Manager) LastError() error {
	if m == nil {
		return ErrSnapshotNotLoaded
	}
	m.lastRefreshErrMu.Lock()
	defer m.lastRefreshErrMu.Unlock()
	return m.lastRefreshErr
}

// StartWatcher tails config_changelog on interval (plus immediate wakeups from Notify) until ctx is canceled.
func (m *Manager) StartWatcher(ctx context.Context, interval time.Duration) {
	if m == nil {
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
		case <-m.wakeCh:
			_ = m.Refresh(ctx)
		case <-ticker.C:
			_ = m.Refresh(ctx)
		}
	}
}

// RegisterAuthenticatedStream atomically authenticates rawKey, registers cancel in
// ActiveStreamRegistry, and re-verifies the latest snapshot post-registration so a
// concurrent emergency revocation can never miss an in-flight stream (zero TOCTOU — AUTH-06).
func (m *Manager) RegisterAuthenticatedStream(rawKey, targetEnvID string, cancel context.CancelFunc) (KeyMatchResult, uint64, error) {
	if m == nil {
		return KeyMatchResult{}, 0, ErrSnapshotNotLoaded
	}
	if cancel == nil {
		return KeyMatchResult{}, 0, fmt.Errorf("%w: non-nil cancel function required", port.ErrInvalidInput)
	}
	nowMs := m.nowMs()
	res, err := m.snap.Load().AuthenticateKey(rawKey, targetEnvID, nowMs)
	if err != nil {
		return KeyMatchResult{}, 0, err
	}

	handleID := m.streams.Register(res.Key.OrgID, res.Key.ProjectID, res.Key.ID, cancel)

	// Re-check against the latest atomic snapshot after Register() so if Refresh() stored a
	// revoked snapshot and already called AbortKey() concurrently, we still fail closed.
	res2, err2 := m.snap.Load().AuthenticateKey(rawKey, targetEnvID, m.nowMs())
	if err2 != nil {
		m.streams.Unregister(res.Key.ID, handleID)
		return KeyMatchResult{}, 0, err2
	}
	return res2, handleID, nil
}

// Refresh checks config_changelog for epochs newer than the current snapshot (or loads initial state)
// within a single read-only transaction, swaps in an immutable ConfigSnapshot, and cancels streams
// targeted by emergency revocations/shreds.
func (m *Manager) Refresh(ctx context.Context) error {
	if m == nil {
		return ErrSnapshotNotLoaded
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	cur := m.snap.Load()
	var prevEpoch int64
	if cur != nil {
		prevEpoch = cur.Epoch
	}

	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		m.setLastErr(err)
		return fmt.Errorf("snapshot: begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var latestEpoch sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(config_epoch) FROM config_changelog`).Scan(&latestEpoch); err != nil {
		m.setLastErr(err)
		return fmt.Errorf("snapshot: query latest config_epoch: %w", err)
	}
	targetEpoch := int64(0)
	if latestEpoch.Valid {
		targetEpoch = latestEpoch.Int64
	}

	// Fast-path heartbeat when config_changelog has not advanced since last snapshot build.
	if cur != nil && targetEpoch == prevEpoch {
		_ = tx.Commit()
		m.lastPollSuccessMs.Store(m.nowMs())
		m.setLastErr(nil)
		return nil
	}

	var emergencies []changelogEmergency
	if cur != nil && targetEpoch > prevEpoch {
		emergencies, err = readEmergencyEventsSinceSQL(ctx, tx, prevEpoch)
		if err != nil {
			m.setLastErr(err)
			return err
		}
	}

	effective, err := loadEffectiveConfigsSQL(ctx, tx)
	if err != nil {
		m.setLastErr(err)
		return err
	}
	byActive, byPrev, byID, err := loadVirtualKeysSQL(ctx, tx)
	if err != nil {
		m.setLastErr(err)
		return err
	}
	if err := tx.Commit(); err != nil {
		m.setLastErr(err)
		return fmt.Errorf("snapshot: commit read tx: %w", err)
	}

	next := &ConfigSnapshot{
		Epoch:            targetEpoch,
		LoadedAtMs:       m.nowMs(),
		byActiveHash:     byActive,
		byPrevHash:       byPrev,
		byKeyID:          byID,
		effectiveConfigs: effective,
	}

	m.snap.Store(next)
	m.lastPollSuccessMs.Store(next.LoadedAtMs)
	m.setLastErr(nil)

	for _, ev := range emergencies {
		switch {
		case ev.EntityKind == "virtual_key" && (ev.MutationType == port.MutationRevoke || ev.Emergency):
			m.streams.AbortKey(ev.EntityID)
		case ev.MutationType == port.MutationShred && ev.EntityKind == "project" && ev.OrgID != "":
			m.streams.AbortScope(ev.OrgID, ev.EntityID)
		case ev.MutationType == port.MutationShred && ev.EntityKind == "organization":
			m.streams.AbortScope(ev.EntityID, "")
		}
	}
	return nil
}

func (m *Manager) setLastErr(err error) {
	m.lastRefreshErrMu.Lock()
	m.lastRefreshErr = err
	m.lastRefreshErrMu.Unlock()
}

type changelogEmergency struct {
	OrgID        string
	EntityKind   string
	EntityID     string
	MutationType string
	Emergency    bool
}

func readEmergencyEventsSinceSQL(ctx context.Context, tx *sql.Tx, afterEpoch int64) ([]changelogEmergency, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT COALESCE(org_id, ''), entity_kind, entity_id, mutation_type, emergency
		FROM config_changelog
		WHERE config_epoch > ? AND (emergency = 1 OR mutation_type IN ('REVOKE', 'SHRED'))
		ORDER BY config_epoch ASC`, afterEpoch)
	if err != nil {
		return nil, fmt.Errorf("snapshot: query emergency changelog deltas: %w", err)
	}
	defer rows.Close()

	var events []changelogEmergency
	for rows.Next() {
		var ev changelogEmergency
		var emInt int64
		if scanErr := rows.Scan(&ev.OrgID, &ev.EntityKind, &ev.EntityID, &ev.MutationType, &emInt); scanErr != nil {
			return nil, fmt.Errorf("snapshot: scan emergency changelog delta: %w", scanErr)
		}
		ev.Emergency = emInt != 0
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("snapshot: iterate emergency changelog deltas: %w", err)
	}
	return events, nil
}

func loadEffectiveConfigsSQL(ctx context.Context, tx *sql.Tx) (map[ScopeKey]*EffectiveConfig, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			o.id, o.slug, o.status, o.default_data_region, o.min_plugin_trust, o.require_four_eyes_approval,
			p.id, p.slug, p.status, p.enforced_region,
			e.id, e.slug, e.require_tested_caps
		FROM environments e
		JOIN projects p ON p.org_id = e.org_id AND p.id = e.project_id
		JOIN organizations o ON o.id = e.org_id`)
	if err != nil {
		return nil, fmt.Errorf("snapshot: query effective configs: %w", err)
	}
	defer rows.Close()

	out := make(map[ScopeKey]*EffectiveConfig)
	for rows.Next() {
		var cfg EffectiveConfig
		var defaultReg, enforcedReg sql.NullString
		var fourEyesInt, testedCapsInt int64
		if scanErr := rows.Scan(
			&cfg.OrgID, &cfg.OrgSlug, &cfg.OrgStatus, &defaultReg, &cfg.MinPluginTrust, &fourEyesInt,
			&cfg.ProjectID, &cfg.ProjectSlug, &cfg.ProjectStatus, &enforcedReg,
			&cfg.EnvID, &cfg.EnvSlug, &testedCapsInt,
		); scanErr != nil {
			return nil, fmt.Errorf("snapshot: scan effective config: %w", scanErr)
		}
		if defaultReg.Valid {
			v := defaultReg.String
			cfg.DefaultDataRegion = &v
		}
		if enforcedReg.Valid {
			v := enforcedReg.String
			cfg.EnforcedRegion = &v
		}
		cfg.RequireFourEyesApproval = fourEyesInt != 0
		cfg.RequireTestedCaps = testedCapsInt != 0
		out[ScopeKey{
			OrgID:     cfg.OrgID,
			ProjectID: cfg.ProjectID,
			EnvID:     cfg.EnvID,
		}] = &cfg
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("snapshot: iterate effective configs: %w", err)
	}
	return out, nil
}

func loadVirtualKeysSQL(ctx context.Context, tx *sql.Tx) (
	byActive map[[32]byte]*VirtualKeyView,
	byPrev map[[32]byte]*VirtualKeyView,
	byID map[string]*VirtualKeyView,
	err error,
) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, active_key_hash, previous_key_hash, grace_expires_at_ms,
			state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			expires_at_ms, revoked_at_ms, revocation_mode, version
		FROM virtual_keys`)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("snapshot: query virtual keys: %w", err)
	}
	defer rows.Close()

	byActive = make(map[[32]byte]*VirtualKeyView)
	byPrev = make(map[[32]byte]*VirtualKeyView)
	byID = make(map[string]*VirtualKeyView)

	for rows.Next() {
		var v VirtualKeyView
		var ownerID, revMode sql.NullString
		var activeBytes, prevBytes []byte
		var graceMs, rpmLim, tpmLim, expMs, revMs sql.NullInt64
		var ephInt int64
		var modelsJSON, routesJSON, mcpJSON string

		if scanErr := rows.Scan(
			&v.ID, &v.OrgID, &v.ProjectID, &v.EnvID, &ownerID, &v.Name,
			&v.KeyPrefix, &v.LastFour, &activeBytes, &prevBytes, &graceMs,
			&v.State, &v.AllowedPlanes, &modelsJSON, &routesJSON, &mcpJSON,
			&rpmLim, &tpmLim, &v.MaxConcurrentStreams, &ephInt,
			&expMs, &revMs, &revMode, &v.Version,
		); scanErr != nil {
			return nil, nil, nil, fmt.Errorf("snapshot: scan virtual key: %w", scanErr)
		}

		if ownerID.Valid {
			s := ownerID.String
			v.OwnerPrincipalID = &s
		}
		if revMode.Valid {
			s := revMode.String
			v.RevocationMode = &s
		}
		if graceMs.Valid {
			n := graceMs.Int64
			v.GraceExpiresAtMs = &n
		}
		if rpmLim.Valid {
			n := rpmLim.Int64
			v.RPMLimit = &n
		}
		if tpmLim.Valid {
			n := tpmLim.Int64
			v.TPMLimit = &n
		}
		if expMs.Valid {
			n := expMs.Int64
			v.ExpiresAtMs = &n
		}
		if revMs.Valid {
			n := revMs.Int64
			v.RevokedAtMs = &n
		}
		v.IsEphemeralPlayground = ephInt != 0
		v.AllowedModels = parseJSONStringSlice(modelsJSON)
		v.AllowedRoutes = parseJSONStringSlice(routesJSON)
		v.AllowedMCPTools = parseJSONStringSlice(mcpJSON)

		if len(activeBytes) == 32 {
			copy(v.ActiveKeyHash[:], activeBytes)
			byActive[v.ActiveKeyHash] = &v
		}
		if len(prevBytes) == 32 {
			copy(v.PreviousKeyHash[:], prevBytes)
			v.HasPreviousKeyHash = true
			byPrev[v.PreviousKeyHash] = &v
		}
		byID[v.ID] = &v
	}
	if err := rows.Err(); err != nil {
		return nil, nil, nil, fmt.Errorf("snapshot: iterate virtual keys: %w", err)
	}
	return byActive, byPrev, byID, nil
}

func parseJSONStringSlice(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil
	}
	return out
}
