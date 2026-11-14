// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package vkey implements canonical Virtual Key generation (vk_tp_<env>_<32_base62>),
// SHA-256 hash-only persistence, SM-05 state machine transitions (including
// dual-key grace rotation and non-revivable QUOTA_PAUSED -> REVOKED), and
// sub-second ConfigSnapshot + ActiveStreamRegistry revocation propagation (TP-IMP-0009).
package vkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

const (
	base62Alphabet      = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	randomKeyBodyLen    = 32
	maxGraceSeconds     = int64(259200) // 72 hours max grace window per SCR-13
	defaultMaxStreams   = int64(50)
)

// Revocation modes matching virtual_keys.revocation_mode CHECK constraint.
const (
	RevokeModeStandard  = "STANDARD"
	RevokeModeEmergency = "EMERGENCY_SECURITY"
)

// Canonical Virtual Key lifecycle error sentinels.
var (
	ErrInvalidStateTransition = errors.New("vkey: invalid SM-05 state transition")
	ErrAlreadyRevoked         = errors.New("vkey: key already revoked")
)

// CreateParams configures creation of a new Environment-bound Virtual Key (TP-BR-0103).
type CreateParams struct {
	OrgID                 string
	ProjectID             string
	EnvID                 string
	OwnerPrincipalID      *string
	Name                  string
	AllowedPlanes         string
	AllowedModels         []string
	AllowedRoutes         []string
	AllowedMCPTools       []string
	RPMLimit              *int64
	TPMLimit              *int64
	MaxConcurrentStreams  int64
	IsEphemeralPlayground bool
	ExpiresAtMs           *int64
}

// RotateParams configures zero-downtime Virtual Key rotation under optimistic locking.
type RotateParams struct {
	OrgID           string
	KeyID           string
	ExpectedVersion int64
	GraceSeconds    int64
}

// RevokeParams configures standard or emergency security key revocation.
type RevokeParams struct {
	OrgID           string
	KeyID           string
	ExpectedVersion int64
	Mode            string
	ActorID         string
}

// VirtualKeySummary exposes read-safe metadata — NEVER plaintext key material or hashes (UX-GR-01).
type VirtualKeySummary struct {
	ID                    string   `json:"id"`
	OrgID                 string   `json:"org_id"`
	ProjectID             string   `json:"project_id"`
	EnvID                 string   `json:"env_id"`
	OwnerPrincipalID      *string  `json:"owner_principal_id,omitempty"`
	Name                  string   `json:"name"`
	KeyPrefix             string   `json:"key_prefix"`
	LastFour              string   `json:"last_four"`
	State                 string   `json:"state"`
	AllowedPlanes         string   `json:"allowed_planes"`
	AllowedModels         []string `json:"allowed_models"`
	AllowedRoutes         []string `json:"allowed_routes"`
	AllowedMCPTools       []string `json:"allowed_mcp_tools"`
	RPMLimit              *int64   `json:"rpm_limit,omitempty"`
	TPMLimit              *int64   `json:"tpm_limit,omitempty"`
	MaxConcurrentStreams  int64    `json:"max_concurrent_streams"`
	IsEphemeralPlayground bool     `json:"is_ephemeral_playground"`
	GraceExpiresAtMs      *int64   `json:"grace_expires_at_ms,omitempty"`
	ExpiresAtMs           *int64   `json:"expires_at_ms,omitempty"`
	RevokedAtMs           *int64   `json:"revoked_at_ms,omitempty"`
	RevocationMode        *string  `json:"revocation_mode,omitempty"`
	Version               int64    `json:"version"`
	CreatedAtMs           int64    `json:"created_at_ms"`
	UpdatedAtMs           int64    `json:"updated_at_ms"`
}

// CreatedVirtualKey returns the read-safe summary plus the one-time SecretKey revealed solely at create/rotate.
type CreatedVirtualKey struct {
	Summary   VirtualKeySummary `json:"summary"`
	SecretKey string            `json:"secret_key"`
}

// ServiceConfig configures Virtual Key lifecycle management.
type ServiceConfig struct {
	DB        *sql.DB
	NowMs     func() int64
	Snapshots *snapshot.Manager
	AuditSink port.AuditSink
}

// Service manages SM-05 Virtual Key persistence, hashing, rotation, and revocation.
type Service struct {
	db        *sql.DB
	nowMs     func() int64
	snapshots *snapshot.Manager
	auditSink port.AuditSink
}

// NewService validates dependencies and constructs a Virtual Key Service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Service{
		db:        cfg.DB,
		nowMs:     nowFn,
		snapshots: cfg.Snapshots,
		auditSink: cfg.AuditSink,
	}, nil
}

// Create mints a new vk_tp_<env>_<32_base62> key, stores only its raw 32-byte SHA-256 digest,
// records a config_changelog entry, refreshes ConfigSnapshot, and returns the secret strictly once.
func (s *Service) Create(ctx context.Context, params CreateParams) (*CreatedVirtualKey, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.ProjectID) == "" ||
		strings.TrimSpace(params.EnvID) == "" || strings.TrimSpace(params.Name) == "" {
		return nil, fmt.Errorf("%w: org_id, project_id, env_id, and name are required", port.ErrInvalidInput)
	}
	if params.RPMLimit != nil && *params.RPMLimit <= 0 {
		return nil, fmt.Errorf("%w: rpm_limit must be > 0", port.ErrInvalidInput)
	}
	if params.TPMLimit != nil && *params.TPMLimit <= 0 {
		return nil, fmt.Errorf("%w: tpm_limit must be > 0", port.ErrInvalidInput)
	}

	maxStreams := params.MaxConcurrentStreams
	if maxStreams <= 0 {
		maxStreams = defaultMaxStreams
	}
	planes, err := normalizeAllowedPlanes(params.AllowedPlanes)
	if err != nil {
		return nil, err
	}
	models := params.AllowedModels
	if len(models) == 0 {
		models = []string{"*"}
	}
	routes := params.AllowedRoutes
	if len(routes) == 0 {
		routes = []string{"*"}
	}
	mcpTools := params.AllowedMCPTools
	if mcpTools == nil {
		mcpTools = []string{}
	}

	modelsJSON, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	routesJSON, err := json.Marshal(routes)
	if err != nil {
		return nil, err
	}
	mcpJSON, err := json.Marshal(mcpTools)
	if err != nil {
		return nil, err
	}

	keyID, err := idgen.New(idgen.PrefixVirtualKey)
	if err != nil {
		return nil, err
	}

	now := s.nowMs()
	if params.ExpiresAtMs != nil && *params.ExpiresAtMs <= now {
		return nil, fmt.Errorf("%w: expires_at_ms must be in the future", port.ErrInvalidInput)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var envSlug string
	if scanErr := tx.QueryRowContext(ctx, `
		SELECT slug FROM environments
		WHERE org_id = ? AND project_id = ? AND id = ?`,
		params.OrgID, params.ProjectID, params.EnvID,
	).Scan(&envSlug); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, scanErr
	}

	rawSecret, prefix, lastFour, digest, err := generateVirtualKey(envSlug)
	if err != nil {
		return nil, err
	}
	defer clear(digest[:])

	var ownerVal any
	var ownerCopy *string
	if params.OwnerPrincipalID != nil && strings.TrimSpace(*params.OwnerPrincipalID) != "" {
		v := strings.TrimSpace(*params.OwnerPrincipalID)
		var ownerOK int
		if ownerErr := tx.QueryRowContext(ctx, `
			SELECT 1 FROM principals WHERE id = ? AND org_id = ?`,
			v, params.OrgID,
		).Scan(&ownerOK); ownerErr != nil {
			if errors.Is(ownerErr, sql.ErrNoRows) {
				// Return uniform 404 resource_not_found on cross-org or missing principal IDs (AUTH-15).
				return nil, port.ErrNotFound
			}
			return nil, fmt.Errorf("vkey: verify owner_principal_id: %w", ownerErr)
		}
		ownerVal = v
		ownerCopy = &v
	}

	ephInt := 0
	if params.IsEphemeralPlayground {
		ephInt = 1
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO virtual_keys (
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, active_key_hash, previous_key_hash, grace_expires_at_ms,
			state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, 'CREATED_ACTIVE', ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, 1, ?, ?)`,
		keyID, params.OrgID, params.ProjectID, params.EnvID, ownerVal, strings.TrimSpace(params.Name),
		prefix, lastFour, digest[:], planes, string(modelsJSON), string(routesJSON), string(mcpJSON),
		nullInt64Ptr(params.RPMLimit), nullInt64Ptr(params.TPMLimit), maxStreams, ephInt,
		nullInt64Ptr(params.ExpiresAtMs), now, now,
	); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'virtual_key', ?, ?, 0, ?)`,
		params.OrgID, keyID, port.MutationUpsert, now,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.syncSnapshot(ctx)

	return &CreatedVirtualKey{
		Summary: VirtualKeySummary{
			ID:                    keyID,
			OrgID:                 params.OrgID,
			ProjectID:             params.ProjectID,
			EnvID:                 params.EnvID,
			OwnerPrincipalID:      ownerCopy,
			Name:                  strings.TrimSpace(params.Name),
			KeyPrefix:             prefix,
			LastFour:              lastFour,
			State:                 snapshot.KeyStateCreatedActive,
			AllowedPlanes:         planes,
			AllowedModels:         append([]string(nil), models...),
			AllowedRoutes:         append([]string(nil), routes...),
			AllowedMCPTools:       append([]string(nil), mcpTools...),
			RPMLimit:              cloneInt64Ptr(params.RPMLimit),
			TPMLimit:              cloneInt64Ptr(params.TPMLimit),
			MaxConcurrentStreams:  maxStreams,
			IsEphemeralPlayground: params.IsEphemeralPlayground,
			ExpiresAtMs:           cloneInt64Ptr(params.ExpiresAtMs),
			Version:               1,
			CreatedAtMs:           now,
			UpdatedAtMs:           now,
		},
		SecretKey: rawSecret,
	}, nil
}

// Get fetches read-safe Virtual Key metadata scoped strictly to orgID (AUTH-15).
func (s *Service) Get(ctx context.Context, orgID, keyID string) (*VirtualKeySummary, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(keyID) == "" {
		return nil, fmt.Errorf("%w: org_id and key_id required", port.ErrInvalidInput)
	}
	return scanVirtualKeySummary(s.db.QueryRowContext(ctx, `
		SELECT
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			grace_expires_at_ms, expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		FROM virtual_keys
		WHERE org_id = ? AND id = ?`, orgID, keyID))
}

// Rotate transitions a key to ROTATING_DUAL_VALID (or replaces active hash immediately if graceSeconds == 0)
// keeping previous_key_hash valid until now + graceSeconds under optimistic locking (AC-2 / AUTH-05).
func (s *Service) Rotate(ctx context.Context, params RotateParams) (*CreatedVirtualKey, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.KeyID) == "" || params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, key_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	if params.GraceSeconds < 0 || params.GraceSeconds > maxGraceSeconds {
		return nil, fmt.Errorf("%w: grace_seconds must be within [0, %d]", port.ErrInvalidInput, maxGraceSeconds)
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		envID      string
		curState   string
		curVer     int64
		curHash    []byte
		curExpMs   sql.NullInt64
		envSlug    string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT vk.env_id, vk.state, vk.version, vk.active_key_hash, vk.expires_at_ms, e.slug
		FROM virtual_keys vk
		JOIN environments e ON e.org_id = vk.org_id AND e.id = vk.env_id
		WHERE vk.org_id = ? AND vk.id = ?`,
		params.OrgID, params.KeyID,
	).Scan(&envID, &curState, &curVer, &curHash, &curExpMs, &envSlug)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if curVer != params.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}
	if curExpMs.Valid && now >= curExpMs.Int64 {
		return nil, ErrInvalidStateTransition
	}
	switch curState {
	case snapshot.KeyStateCreatedActive, snapshot.KeyStateRotatingDualValid:
	default:
		return nil, fmt.Errorf("%w: cannot rotate key in state %s", ErrInvalidStateTransition, curState)
	}

	rawSecret, prefix, lastFour, newDigest, err := generateVirtualKey(envSlug)
	if err != nil {
		return nil, err
	}
	defer clear(newDigest[:])

	nextState := snapshot.KeyStateCreatedActive
	var prevHashVal any
	var graceVal any
	if params.GraceSeconds > 0 {
		nextState = snapshot.KeyStateRotatingDualValid
		prevHashVal = curHash
		graceVal = now + params.GraceSeconds*1000
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE virtual_keys
		SET key_prefix = ?, last_four = ?, active_key_hash = ?, previous_key_hash = ?,
		    grace_expires_at_ms = ?, state = ?, version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		prefix, lastFour, newDigest[:], prevHashVal, graceVal, nextState, now,
		params.OrgID, params.KeyID, params.ExpectedVersion,
	)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, port.ErrVersionConflict
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'virtual_key', ?, ?, 0, ?)`,
		params.OrgID, params.KeyID, port.MutationUpsert, now,
	); err != nil {
		return nil, err
	}

	summary, err := scanVirtualKeySummary(tx.QueryRowContext(ctx, `
		SELECT
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			grace_expires_at_ms, expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		FROM virtual_keys
		WHERE org_id = ? AND id = ?`, params.OrgID, params.KeyID))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.syncSnapshot(ctx)

	return &CreatedVirtualKey{
		Summary:   *summary,
		SecretKey: rawSecret,
	}, nil
}

// MarkQuotaPaused transitions an active or rotating key into QUOTA_PAUSED (SM-05).
func (s *Service) MarkQuotaPaused(ctx context.Context, orgID, keyID string, expectedVersion int64) (*VirtualKeySummary, error) {
	return s.transitionState(ctx, orgID, keyID, expectedVersion,
		[]string{snapshot.KeyStateCreatedActive, snapshot.KeyStateRotatingDualValid},
		snapshot.KeyStateQuotaPaused,
	)
}

// ResumeFromQuotaPaused transitions ONLY a still-QUOTA_PAUSED key back to active.
// Enforces SM-05 Non-Revival Invariant: REVOKED or EXPIRED keys can never auto-revive (AUTH-05).
func (s *Service) ResumeFromQuotaPaused(ctx context.Context, orgID, keyID string, expectedVersion int64) (*VirtualKeySummary, error) {
	return s.transitionState(ctx, orgID, keyID, expectedVersion,
		[]string{snapshot.KeyStateQuotaPaused},
		snapshot.KeyStateCreatedActive,
	)
}

// Suspend places a key in temporary investigation hold (SUSPENDED).
func (s *Service) Suspend(ctx context.Context, orgID, keyID string, expectedVersion int64) (*VirtualKeySummary, error) {
	return s.transitionState(ctx, orgID, keyID, expectedVersion,
		[]string{snapshot.KeyStateCreatedActive, snapshot.KeyStateRotatingDualValid, snapshot.KeyStateQuotaPaused},
		snapshot.KeyStateSuspended,
	)
}

// ReactivateFromSuspended returns a cleared SUSPENDED key to CREATED_ACTIVE.
func (s *Service) ReactivateFromSuspended(ctx context.Context, orgID, keyID string, expectedVersion int64) (*VirtualKeySummary, error) {
	return s.transitionState(ctx, orgID, keyID, expectedVersion,
		[]string{snapshot.KeyStateSuspended},
		snapshot.KeyStateCreatedActive,
	)
}

func (s *Service) transitionState(ctx context.Context, orgID, keyID string, expectedVersion int64, allowedFrom []string, nextState string) (*VirtualKeySummary, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(keyID) == "" || expectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, key_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		curState string
		curVer   int64
		curExpMs sql.NullInt64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT state, version, expires_at_ms FROM virtual_keys WHERE org_id = ? AND id = ?`,
		orgID, keyID,
	).Scan(&curState, &curVer, &curExpMs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if curVer != expectedVersion {
		return nil, port.ErrVersionConflict
	}
	if curExpMs.Valid && now >= curExpMs.Int64 {
		return nil, fmt.Errorf("%w: key TTL has already expired", ErrInvalidStateTransition)
	}

	allowed := false
	for _, st := range allowedFrom {
		if curState == st {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("%w: %s -> %s not permitted", ErrInvalidStateTransition, curState, nextState)
	}

	var res sql.Result
	if nextState == snapshot.KeyStateCreatedActive {
		res, err = tx.ExecContext(ctx, `
			UPDATE virtual_keys
			SET state = ?, previous_key_hash = NULL, grace_expires_at_ms = NULL, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			nextState, now, orgID, keyID, expectedVersion,
		)
	} else {
		res, err = tx.ExecContext(ctx, `
			UPDATE virtual_keys
			SET state = ?, version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND version = ?`,
			nextState, now, orgID, keyID, expectedVersion,
		)
	}
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, port.ErrVersionConflict
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'virtual_key', ?, ?, 0, ?)`,
		orgID, keyID, port.MutationUpsert, now,
	); err != nil {
		return nil, err
	}

	summary, err := scanVirtualKeySummary(tx.QueryRowContext(ctx, `
		SELECT
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			grace_expires_at_ms, expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		FROM virtual_keys
		WHERE org_id = ? AND id = ?`, orgID, keyID))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.syncSnapshot(ctx)
	return summary, nil
}

// Revoke permanently revokes a Virtual Key from any non-terminal state (including QUOTA_PAUSED).
// Under RevokeModeEmergency, it immediately aborts all active SSE streams bound to this key (AC-3 / AUTH-06).
func (s *Service) Revoke(ctx context.Context, params RevokeParams) (*VirtualKeySummary, error) {
	if strings.TrimSpace(params.OrgID) == "" || strings.TrimSpace(params.KeyID) == "" || params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: org_id, key_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	mode := params.Mode
	if mode == "" {
		mode = RevokeModeStandard
	}
	if mode != RevokeModeStandard && mode != RevokeModeEmergency {
		return nil, fmt.Errorf("%w: invalid revocation_mode %q", port.ErrInvalidInput, mode)
	}
	emergency := mode == RevokeModeEmergency
	now := s.nowMs()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		curState string
		curVer   int64
	)
	err = tx.QueryRowContext(ctx, `
		SELECT state, version FROM virtual_keys WHERE org_id = ? AND id = ?`,
		params.OrgID, params.KeyID,
	).Scan(&curState, &curVer)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if curVer != params.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}

	switch curState {
	case snapshot.KeyStateCreatedActive, snapshot.KeyStateRotatingDualValid, snapshot.KeyStateQuotaPaused, snapshot.KeyStateSuspended:
	case snapshot.KeyStateRevoked, snapshot.KeyStatePurgedMetadataRetained:
		return nil, ErrAlreadyRevoked
	default:
		return nil, fmt.Errorf("%w: cannot revoke key in state %s", ErrInvalidStateTransition, curState)
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE virtual_keys
		SET state = ?, previous_key_hash = NULL, grace_expires_at_ms = NULL,
		    revoked_at_ms = ?, revocation_mode = ?, version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		snapshot.KeyStateRevoked, now, mode, now, params.OrgID, params.KeyID, params.ExpectedVersion,
	)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, port.ErrVersionConflict
	}

	emInt := 0
	if emergency {
		emInt = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'virtual_key', ?, ?, ?, ?)`,
		params.OrgID, params.KeyID, port.MutationRevoke, emInt, now,
	); err != nil {
		return nil, err
	}

	summary, err := scanVirtualKeySummary(tx.QueryRowContext(ctx, `
		SELECT
			id, org_id, project_id, env_id, owner_principal_id, name,
			key_prefix, last_four, state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
			rpm_limit, tpm_limit, max_concurrent_streams, is_ephemeral_playground,
			grace_expires_at_ms, expires_at_ms, revoked_at_ms, revocation_mode, version, created_at_ms, updated_at_ms
		FROM virtual_keys
		WHERE org_id = ? AND id = ?`, params.OrgID, params.KeyID))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	s.syncSnapshot(ctx)
	if emergency && s.snapshots != nil && s.snapshots.Streams() != nil {
		s.snapshots.Streams().AbortKey(params.KeyID)
	}

	if s.auditSink != nil {
		if audID, idErr := idgen.New(idgen.PrefixAudit); idErr == nil {
			actor := strings.TrimSpace(params.ActorID)
			if actor == "" {
				actor = "system"
			}
			_ = s.auditSink.Append(ctx, port.AuditEvent{
				ID:           audID,
				CreatedAtMs:  now,
				OrgID:        &params.OrgID,
				ActorKind:    "PRINCIPAL",
				ActorID:      actor,
				EntryPoint:   "admin_api",
				EventType:    "virtual_key.revoked",
				TargetType:   "virtual_key",
				TargetID:     params.KeyID,
				MetadataJSON: fmt.Sprintf(`{"mode":%q}`, mode),
			})
		}
	}

	return summary, nil
}

// FinalizeExpiredRotations clears elapsed previous_key_hash grace windows cleanly back to CREATED_ACTIVE.
func (s *Service) FinalizeExpiredRotations(ctx context.Context) (int, error) {
	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		SELECT org_id, id FROM virtual_keys
		WHERE state = 'ROTATING_DUAL_VALID' AND grace_expires_at_ms IS NOT NULL AND grace_expires_at_ms <= ?`, now)
	if err != nil {
		return 0, err
	}
	type keyRef struct{ orgID, id string }
	var due []keyRef
	for rows.Next() {
		var r keyRef
		if scanErr := rows.Scan(&r.orgID, &r.id); scanErr != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("vkey: scan expired rotation: %w", scanErr)
		}
		due = append(due, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("vkey: iterate expired rotations: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("vkey: close expired rotations cursor: %w", err)
	}
	if len(due) == 0 {
		return 0, nil
	}

	for _, r := range due {
		if _, err := tx.ExecContext(ctx, `
			UPDATE virtual_keys
			SET state = 'CREATED_ACTIVE', previous_key_hash = NULL, grace_expires_at_ms = NULL,
			    version = version + 1, updated_at_ms = ?
			WHERE org_id = ? AND id = ? AND state = 'ROTATING_DUAL_VALID' AND grace_expires_at_ms IS NOT NULL AND grace_expires_at_ms <= ?`,
			now, r.orgID, r.id, now,
		); err != nil {
			return 0, fmt.Errorf("vkey: finalize expired rotation %s: %w", r.id, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
			VALUES (?, 'virtual_key', ?, ?, 0, ?)`,
			r.orgID, r.id, port.MutationUpsert, now,
		); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.syncSnapshot(ctx)
	return len(due), nil
}

func (s *Service) syncSnapshot(ctx context.Context) {
	if s.snapshots == nil {
		return
	}
	_ = s.snapshots.Refresh(ctx)
	s.snapshots.Notify()
}

func generateVirtualKey(envSlug string) (rawSecret, keyPrefix, lastFour string, digest [32]byte, err error) {
	cleanSlug := sanitizeEnvSlug(envSlug)
	body, err := randomBase62(randomKeyBodyLen)
	if err != nil {
		return "", "", "", [32]byte{}, err
	}
	keyPrefix = "vk_tp_" + cleanSlug + "_"
	rawSecret = keyPrefix + body
	lastFour = rawSecret[len(rawSecret)-4:]
	digest = sha256.Sum256([]byte(rawSecret))
	return rawSecret, keyPrefix, lastFour, digest, nil
}

func sanitizeEnvSlug(slug string) string {
	lower := strings.ToLower(strings.TrimSpace(slug))
	var b strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == '-' || r == '_' {
			b.WriteByte('-')
		}
		if b.Len() >= 16 { // Ensures "vk_tp_" + <=16 + "_" <= 23 <= VARCHAR(24)
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "env"
	}
	return out
}

func randomBase62(length int) (string, error) {
	out := make([]byte, length)
	buf := make([]byte, length*2)
	written := 0
	for written < length {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("vkey: rand read: %w", err)
		}
		for _, b := range buf {
			if b < 248 { // 248 = 4 * 62 => zero modulo bias
				out[written] = base62Alphabet[int(b)%len(base62Alphabet)]
				written++
				if written == length {
					break
				}
			}
		}
	}
	clear(buf)
	secret := string(out)
	clear(out)
	return secret, nil
}

func scanVirtualKeySummary(row interface{ Scan(...any) error }) (*VirtualKeySummary, error) {
	var (
		s                                  VirtualKeySummary
		ownerID, revMode                   sql.NullString
		rpmLim, tpmLim, graceMs, expMs     sql.NullInt64
		revMs                              sql.NullInt64
		ephInt                             int64
		modelsJSON, routesJSON, mcpJSON    string
	)
	err := row.Scan(
		&s.ID, &s.OrgID, &s.ProjectID, &s.EnvID, &ownerID, &s.Name,
		&s.KeyPrefix, &s.LastFour, &s.State, &s.AllowedPlanes, &modelsJSON, &routesJSON, &mcpJSON,
		&rpmLim, &tpmLim, &s.MaxConcurrentStreams, &ephInt,
		&graceMs, &expMs, &revMs, &revMode, &s.Version, &s.CreatedAtMs, &s.UpdatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if ownerID.Valid {
		v := ownerID.String
		s.OwnerPrincipalID = &v
	}
	if revMode.Valid {
		v := revMode.String
		s.RevocationMode = &v
	}
	if rpmLim.Valid {
		n := rpmLim.Int64
		s.RPMLimit = &n
	}
	if tpmLim.Valid {
		n := tpmLim.Int64
		s.TPMLimit = &n
	}
	if graceMs.Valid {
		n := graceMs.Int64
		s.GraceExpiresAtMs = &n
	}
	if expMs.Valid {
		n := expMs.Int64
		s.ExpiresAtMs = &n
	}
	if revMs.Valid {
		n := revMs.Int64
		s.RevokedAtMs = &n
	}
	s.IsEphemeralPlayground = ephInt != 0
	_ = json.Unmarshal([]byte(modelsJSON), &s.AllowedModels)
	_ = json.Unmarshal([]byte(routesJSON), &s.AllowedRoutes)
	_ = json.Unmarshal([]byte(mcpJSON), &s.AllowedMCPTools)
	return &s, nil
}

func nullInt64Ptr(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func normalizeAllowedPlanes(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "PROVIDER", nil
	}
	parts := strings.Split(trimmed, ",")
	seen := make(map[string]bool, len(parts))
	normalized := make([]string, 0, len(parts))
	for _, p := range parts {
		token := strings.ToUpper(strings.TrimSpace(p))
		switch token {
		case "PROVIDER", "MCP", "A2A":
		default:
			return "", fmt.Errorf("%w: invalid allowed_planes token %q", port.ErrInvalidInput, p)
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		normalized = append(normalized, token)
	}
	if len(normalized) == 0 {
		return "", fmt.Errorf("%w: allowed_planes cannot be empty", port.ErrInvalidInput)
	}
	return strings.Join(normalized, ","), nil
}

func cloneInt64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
