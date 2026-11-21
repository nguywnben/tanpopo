// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package foureyes implements dual-administrator Four-Eyes governance over
// admin_change_requests (AUTH-12 / TP-IMP-0041) for high-impact mutations
// across both SQLite WAL and PostgreSQL 16+ FORCE RLS.
//
// DAG Boundary (Tier 3): Imports stdlib, internal/pkg, internal/storage/port,
// and internal/security/audit only.
package foureyes

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// Supported high-impact action types governed by Four-Eyes dual control (AUTH-12).
const (
	ActionExtensionActivateExternal = "EXTENSION_ACTIVATE_EXTERNAL"
	ActionVaultRotateRootKey        = "VAULT_ROTATE_ROOT_KEY"
	ActionVaultDestroyRootKey       = "VAULT_DESTROY_ROOT_KEY"
	ActionKillSwitchClear           = "KILL_SWITCH_CLEAR"
)

// Canonical admin_change_requests lifecycle states.
const (
	StatePendingSecondApprover = "PENDING_SECOND_APPROVER"
	StateApprovedApplied       = "APPROVED_APPLIED"
	StateRejected              = "REJECTED"
	StateExpired               = "EXPIRED"
	StateCancelled             = "CANCELLED"
)

// Canonical Four-Eyes decisions.
const (
	DecisionApprove = "APPROVE"
	DecisionReject  = "REJECT"
)

const (
	defaultRequestTTL = 24 * time.Hour
	maxRequestTTL     = 7 * 24 * time.Hour
)

// Canonical Four-Eyes errors.
var (
	ErrSelfApprovalForbidden = errors.New("foureyes: initiator cannot approve own change request")
	ErrInvalidStateTransition = errors.New("foureyes: invalid change request state transition")
	ErrRequestExpired        = errors.New("foureyes: change request expired")
	ErrPayloadTampered       = errors.New("foureyes: change request payload hash mismatch")
)

// Config configures the Four-Eyes Service fail-closed.
type Config struct {
	DB        *sql.DB
	AuditSink port.TxAuditSink
	NowMs     func() int64
}

// ChangeRequest represents one row in admin_change_requests.
type ChangeRequest struct {
	ID                   string  `json:"id"`
	OrgID                *string `json:"org_id,omitempty"`
	ActionType           string  `json:"action_type"`
	TargetType           string  `json:"target_type"`
	TargetID             string  `json:"target_id"`
	PayloadJCSJSON       string  `json:"payload_jcs_json"`
	PayloadSHA256        string  `json:"payload_sha256"`
	InitiatorPrincipalID string  `json:"initiator_principal_id"`
	ApproverPrincipalID  *string `json:"approver_principal_id,omitempty"`
	State                string  `json:"state"`
	DecisionReason       *string `json:"decision_reason,omitempty"`
	Version              int64   `json:"version"`
	ExpiresAtMs          int64   `json:"expires_at_ms"`
	CreatedAtMs          int64   `json:"created_at_ms"`
	DecidedAtMs          *int64  `json:"decided_at_ms,omitempty"`
}

// InitiateParams creates a pending second-approver change request.
type InitiateParams struct {
	OrgID                *string
	ActionType           string
	TargetType           string
	TargetID             string
	PayloadJSON          json.RawMessage
	InitiatorPrincipalID string
	TTL                  time.Duration
	RequestID            *string
}

// DecideParams submits a second-approver decision (APPROVE or REJECT) with optimistic version fencing.
type DecideParams struct {
	OrgID               *string
	RequestID           string
	ApproverPrincipalID string
	Decision            string
	Reason              string
	ExpectedVersion     int64
	CorrelationID       *string
}

// CancelParams cancels a pending change request by its initiator or an authorized reviewer.
type CancelParams struct {
	OrgID            *string
	RequestID        string
	ActorPrincipalID string
	Reason           string
	ExpectedVersion  int64
	CorrelationID    *string
}

// ApplyFunc executes the approved administrative mutation once the second approver verifies and locks the request.
type ApplyFunc func(ctx context.Context, req *ChangeRequest) error

// Service manages durable Four-Eyes dual-control change requests across SQLite and PostgreSQL 16 FORCE RLS.
type Service struct {
	db      *sql.DB
	dialect port.Dialect
	txAudit port.TxAuditSink
	nowMs   func() int64
}

// NewService constructs a fail-closed Four-Eyes Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	if cfg.AuditSink == nil {
		return nil, fmt.Errorf("%w: non-nil port.TxAuditSink is required", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Service{
		db:      cfg.DB,
		dialect: port.DetectDialect(context.Background(), cfg.DB),
		txAudit: cfg.AuditSink,
		nowMs:   nowFn,
	}, nil
}

// IsValidActionType validates canonical high-impact action identifiers.
func IsValidActionType(action string) bool {
	switch strings.TrimSpace(action) {
	case ActionExtensionActivateExternal, ActionVaultRotateRootKey, ActionVaultDestroyRootKey, ActionKillSwitchClear:
		return true
	default:
		return false
	}
}

// RequiresFourEyes reports whether the target organization (or any active non-deleting org when orgID == nil)
// enforces require_four_eyes_approval.
func (s *Service) RequiresFourEyes(ctx context.Context, orgID *string) (bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return true, fmt.Errorf("foureyes: begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		oid := strings.TrimSpace(*orgID)
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, oid); err != nil {
			return true, err
		}
		var enabled port.FlexBool
		q := port.Rebind(s.dialect, `SELECT require_four_eyes_approval FROM organizations WHERE id = ?`)
		if err := tx.QueryRowContext(ctx, q, oid).Scan(&enabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, port.ErrNotFound
			}
			return true, err
		}
		return bool(enabled), nil
	}

	if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
		return true, err
	}
	var cnt int
	q := `SELECT COUNT(*) FROM organizations WHERE status NOT IN ('DELETING', 'PENDING_ERASURE') AND require_four_eyes_approval = ` + port.BoolExpr(s.dialect, true)
	if err := tx.QueryRowContext(ctx, q).Scan(&cnt); err != nil {
		return true, err
	}
	return cnt > 0, nil
}

// Initiate queues a high-impact change request in state PENDING_SECOND_APPROVER.
func (s *Service) Initiate(ctx context.Context, params InitiateParams) (*ChangeRequest, error) {
	action := strings.TrimSpace(params.ActionType)
	if !IsValidActionType(action) {
		return nil, fmt.Errorf("%w: unsupported four-eyes action_type %q", port.ErrInvalidInput, params.ActionType)
	}
	targetType := strings.TrimSpace(params.TargetType)
	targetID := strings.TrimSpace(params.TargetID)
	initiator := strings.TrimSpace(params.InitiatorPrincipalID)
	if targetType == "" || targetID == "" || initiator == "" {
		return nil, fmt.Errorf("%w: target_type, target_id, and initiator_principal_id required", port.ErrInvalidInput)
	}
	if len(targetType) > 32 || len(targetID) > 64 {
		return nil, fmt.Errorf("%w: target_type or target_id exceeds schema limit", port.ErrInvalidInput)
	}

	rawPayload := params.PayloadJSON
	if len(strings.TrimSpace(string(rawPayload))) == 0 {
		rawPayload = []byte(`{}`)
	}
	canonPayload, err := audit.CanonicalizeJSON(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("%w: payload_json invalid for JCS: %v", port.ErrInvalidInput, err)
	}
	sum := sha256.Sum256(canonPayload)
	payloadSHA := hex.EncodeToString(sum[:])

	ttl := params.TTL
	if ttl <= 0 {
		ttl = defaultRequestTTL
	}
	if ttl > maxRequestTTL {
		ttl = maxRequestTTL
	}
	now := s.nowMs()
	expiresAt := now + ttl.Milliseconds()

	rawID, err := idgen.New(idgen.PrefixApproval)
	if err != nil {
		return nil, err
	}
	id := "chg_" + strings.TrimPrefix(rawID, idgen.PrefixApproval+"_")

	var orgArg any
	var normOrg *string
	if params.OrgID != nil && strings.TrimSpace(*params.OrgID) != "" {
		v := strings.TrimSpace(*params.OrgID)
		normOrg = &v
		orgArg = v
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("foureyes: begin Initiate tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if normOrg != nil {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, *normOrg); err != nil {
			return nil, err
		}
	} else {
		if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
			return nil, err
		}
	}

	_, err = tx.ExecContext(ctx, port.Rebind(s.dialect, `
		INSERT INTO admin_change_requests (
			id, org_id, action_type, target_type, target_id,
			payload_jcs_json, payload_sha256, initiator_principal_id,
			state, version, expires_at_ms, created_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`),
		id, orgArg, action, targetType, targetID,
		string(canonPayload), payloadSHA, initiator,
		StatePendingSecondApprover, expiresAt, now,
	)
	if err != nil {
		return nil, mapSQLiteOrPGError(err)
	}

	if err := s.appendAuditTx(ctx, tx, normOrg, initiator, "change_request.initiated", id, map[string]any{
		"action_type":    action,
		"target_type":    targetType,
		"target_id":      targetID,
		"payload_sha256": payloadSHA,
	}, params.RequestID, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("foureyes: commit Initiate: %w", err)
	}
	committed = true

	return &ChangeRequest{
		ID:                   id,
		OrgID:                normOrg,
		ActionType:           action,
		TargetType:           targetType,
		TargetID:             targetID,
		PayloadJCSJSON:       string(canonPayload),
		PayloadSHA256:        payloadSHA,
		InitiatorPrincipalID: initiator,
		State:                StatePendingSecondApprover,
		Version:              1,
		ExpiresAtMs:          expiresAt,
		CreatedAtMs:          now,
	}, nil
}

// Get loads a single change request scoped by orgID (nil => instance operator).
func (s *Service) Get(ctx context.Context, orgID *string, requestID string) (*ChangeRequest, error) {
	reqID := strings.TrimSpace(requestID)
	if reqID == "" {
		return nil, port.ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("foureyes: begin Get tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, strings.TrimSpace(*orgID)); err != nil {
			return nil, err
		}
		return loadChangeRequestTx(ctx, tx, s.dialect, reqID, orgID)
	}
	if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
		return nil, err
	}
	return loadChangeRequestTx(ctx, tx, s.dialect, reqID, nil)
}

// List lists change requests visible to orgID (or instance-level when orgID == nil).
func (s *Service) List(ctx context.Context, orgID *string, stateFilter string, limit int) ([]ChangeRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("foureyes: begin List tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var where strings.Builder
	var args []any
	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		oid := strings.TrimSpace(*orgID)
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, oid); err != nil {
			return nil, err
		}
		where.WriteString("WHERE org_id = ?")
		args = append(args, oid)
	} else {
		if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
			return nil, err
		}
		where.WriteString("WHERE org_id IS NULL")
	}
	if sf := strings.TrimSpace(stateFilter); sf != "" {
		where.WriteString(" AND state = ?")
		args = append(args, sf)
	}
	args = append(args, limit)
	q := port.Rebind(s.dialect, `
		SELECT id, org_id, action_type, target_type, target_id,
		       payload_jcs_json, payload_sha256, initiator_principal_id, approver_principal_id,
		       state, decision_reason, version, expires_at_ms, created_at_ms, decided_at_ms
		FROM admin_change_requests `+where.String()+`
		ORDER BY created_at_ms DESC, id DESC LIMIT ?`)

	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ChangeRequest
	for rows.Next() {
		item, serr := scanChangeRequest(rows)
		if serr != nil {
			return nil, serr
		}
		out = append(out, item)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, rerr
	}
	return out, nil
}

// Decide evaluates a second-administrator decision (APPROVE or REJECT).
// On APPROVE, verifies initiator != approver (ErrSelfApprovalForbidden), payload JCS integrity,
// expiry, and version fence before invoking applyFn and transitioning atomically to APPROVED_APPLIED.
func (s *Service) Decide(ctx context.Context, params DecideParams, applyFn ApplyFunc) (*ChangeRequest, error) {
	reqID := strings.TrimSpace(params.RequestID)
	approver := strings.TrimSpace(params.ApproverPrincipalID)
	decision := strings.ToUpper(strings.TrimSpace(params.Decision))
	if reqID == "" || approver == "" || params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: request_id, approver_principal_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}
	if decision != DecisionApprove && decision != DecisionReject {
		return nil, fmt.Errorf("%w: decision must be APPROVE or REJECT", port.ErrInvalidInput)
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("foureyes: begin Decide tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if params.OrgID != nil && strings.TrimSpace(*params.OrgID) != "" {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, strings.TrimSpace(*params.OrgID)); err != nil {
			return nil, err
		}
	} else {
		if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
			return nil, err
		}
	}

	cur, err := loadChangeRequestTx(ctx, tx, s.dialect, reqID, params.OrgID)
	if err != nil {
		return nil, err
	}

	if cur.InitiatorPrincipalID == approver {
		_ = s.appendAuditTx(ctx, tx, cur.OrgID, approver, "change_request.self_approval_blocked", cur.ID, map[string]any{
			"action_type": cur.ActionType,
			"target_id":   cur.TargetID,
		}, params.CorrelationID, now)
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		committed = true
		return nil, ErrSelfApprovalForbidden
	}

	if cur.State != StatePendingSecondApprover {
		return nil, fmt.Errorf("%w: request %s is in state %s", ErrInvalidStateTransition, cur.ID, cur.State)
	}
	if cur.Version != params.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}

	if now >= cur.ExpiresAtMs {
		res, uerr := tx.ExecContext(ctx, port.Rebind(s.dialect, `
			UPDATE admin_change_requests
			SET state = ?, decided_at_ms = ?, version = version + 1
			WHERE id = ? AND version = ? AND state = ?`),
			StateExpired, now, cur.ID, cur.Version, StatePendingSecondApprover,
		)
		if uerr != nil {
			return nil, mapSQLiteOrPGError(uerr)
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			return nil, port.ErrVersionConflict
		}
		if aerr := s.appendAuditTx(ctx, tx, cur.OrgID, approver, "change_request.expired", cur.ID, map[string]any{
			"action_type": cur.ActionType,
		}, params.CorrelationID, now); aerr != nil {
			return nil, aerr
		}
		if cerr := tx.Commit(); cerr != nil {
			return nil, cerr
		}
		committed = true
		return nil, ErrRequestExpired
	}

	canonPayload, cerr := audit.CanonicalizeJSON([]byte(cur.PayloadJCSJSON))
	if cerr != nil {
		return nil, fmt.Errorf("%w: stored payload invalid: %v", ErrPayloadTampered, cerr)
	}
	sum := sha256.Sum256(canonPayload)
	if hex.EncodeToString(sum[:]) != strings.ToLower(strings.TrimSpace(cur.PayloadSHA256)) {
		return nil, ErrPayloadTampered
	}

	if decision == DecisionReject {
		var reasonArg any
		var reasonPtr *string
		if trimmed := strings.TrimSpace(params.Reason); trimmed != "" {
			reasonArg = trimmed
			reasonPtr = &trimmed
		}
		res, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
			UPDATE admin_change_requests
			SET approver_principal_id = ?, state = ?, decision_reason = ?, decided_at_ms = ?, version = version + 1
			WHERE id = ? AND version = ? AND state = ? AND initiator_principal_id <> ?`),
			approver, StateRejected, reasonArg, now,
			cur.ID, cur.Version, StatePendingSecondApprover, approver,
		)
		if err != nil {
			return nil, mapSQLiteOrPGError(err)
		}
		if aff, _ := res.RowsAffected(); aff == 0 {
			return nil, port.ErrVersionConflict
		}
		updated := *cur
		updated.ApproverPrincipalID = &approver
		updated.State = StateRejected
		updated.DecisionReason = reasonPtr
		updated.Version = cur.Version + 1
		updated.DecidedAtMs = &now
		updated.PayloadJCSJSON = string(canonPayload)
		if err := s.appendAuditTx(ctx, tx, cur.OrgID, approver, "change_request.rejected", cur.ID, map[string]any{
			"action_type":    cur.ActionType,
			"target_type":    cur.TargetType,
			"target_id":      cur.TargetID,
			"payload_sha256": cur.PayloadSHA256,
			"reason":         params.Reason,
		}, params.CorrelationID, now); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("foureyes: commit Decide reject: %w", err)
		}
		committed = true
		return &updated, nil
	}

	var reasonArg any
	var reasonPtr *string
	if trimmed := strings.TrimSpace(params.Reason); trimmed != "" {
		reasonArg = trimmed
		reasonPtr = &trimmed
	}

	// Atomically claim the pending request (setting approver_principal_id and advancing version)
	// while keeping state = PENDING_SECOND_APPROVER until applyFn succeeds.
	claimedVer := cur.Version + 1
	res, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
		UPDATE admin_change_requests
		SET approver_principal_id = ?, decision_reason = ?, version = version + 1
		WHERE id = ? AND version = ? AND state = ? AND approver_principal_id IS NULL AND initiator_principal_id <> ?`),
		approver, reasonArg,
		cur.ID, cur.Version, StatePendingSecondApprover, approver,
	)
	if err != nil {
		return nil, mapSQLiteOrPGError(err)
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		return nil, port.ErrVersionConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("foureyes: commit Decide claim: %w", err)
	}
	committed = true

	claimed := *cur
	claimed.ApproverPrincipalID = &approver
	claimed.DecisionReason = reasonPtr
	claimed.Version = claimedVer
	claimed.PayloadJCSJSON = string(canonPayload)

	if applyFn != nil {
		if aerr := applyFn(ctx, &claimed); aerr != nil {
			_ = s.releaseClaimOnApplyFailure(ctx, &claimed, approver, aerr)
			return nil, aerr
		}
	}

	tx2, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("foureyes: begin Decide finalize tx: %w", err)
	}
	committed2 := false
	defer func() {
		if !committed2 {
			_ = tx2.Rollback()
		}
	}()
	if cur.OrgID != nil {
		if err := port.SetOrgRLSTx(ctx, tx2, s.dialect, *cur.OrgID); err != nil {
			return nil, err
		}
	} else {
		if err := port.SetOperatorRLSTx(ctx, tx2, s.dialect); err != nil {
			return nil, err
		}
	}

	res2, err := tx2.ExecContext(ctx, port.Rebind(s.dialect, `
		UPDATE admin_change_requests
		SET state = ?, decided_at_ms = ?
		WHERE id = ? AND version = ? AND state = ? AND approver_principal_id = ?`),
		StateApprovedApplied, now,
		cur.ID, claimedVer, StatePendingSecondApprover, approver,
	)
	if err != nil {
		return nil, mapSQLiteOrPGError(err)
	}
	if aff, _ := res2.RowsAffected(); aff == 0 {
		return nil, port.ErrVersionConflict
	}

	finalized := claimed
	finalized.State = StateApprovedApplied
	finalized.DecidedAtMs = &now

	if err := s.appendAuditTx(ctx, tx2, cur.OrgID, approver, "change_request.approved_applied", cur.ID, map[string]any{
		"action_type":    cur.ActionType,
		"target_type":    cur.TargetType,
		"target_id":      cur.TargetID,
		"payload_sha256": cur.PayloadSHA256,
		"reason":         params.Reason,
	}, params.CorrelationID, now); err != nil {
		return nil, err
	}
	if err := tx2.Commit(); err != nil {
		return nil, fmt.Errorf("foureyes: commit Decide finalize: %w", err)
	}
	committed2 = true
	return &finalized, nil
}

func (s *Service) releaseClaimOnApplyFailure(ctx context.Context, claimed *ChangeRequest, approver string, applyErr error) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	now := s.nowMs()
	tx, err := s.db.BeginTx(releaseCtx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if claimed.OrgID != nil {
		if err := port.SetOrgRLSTx(releaseCtx, tx, s.dialect, *claimed.OrgID); err != nil {
			return err
		}
	} else {
		if err := port.SetOperatorRLSTx(releaseCtx, tx, s.dialect); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(releaseCtx, port.Rebind(s.dialect, `
		UPDATE admin_change_requests
		SET approver_principal_id = NULL, decision_reason = NULL
		WHERE id = ? AND version = ? AND state = ? AND approver_principal_id = ?`),
		claimed.ID, claimed.Version, StatePendingSecondApprover, approver,
	); err != nil {
		return err
	}
	_ = s.appendAuditTx(releaseCtx, tx, claimed.OrgID, approver, "change_request.apply_failed", claimed.ID, map[string]any{
		"action_type": claimed.ActionType,
		"target_type": claimed.TargetType,
		"target_id":   claimed.TargetID,
		"error":       applyErr.Error(),
	}, nil, now)
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// Cancel transitions a PENDING_SECOND_APPROVER request to CANCELLED.
func (s *Service) Cancel(ctx context.Context, params CancelParams) (*ChangeRequest, error) {
	reqID := strings.TrimSpace(params.RequestID)
	actor := strings.TrimSpace(params.ActorPrincipalID)
	if reqID == "" || actor == "" || params.ExpectedVersion < 1 {
		return nil, fmt.Errorf("%w: request_id, actor_principal_id, and expected_version >= 1 required", port.ErrInvalidInput)
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("foureyes: begin Cancel tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if params.OrgID != nil && strings.TrimSpace(*params.OrgID) != "" {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, strings.TrimSpace(*params.OrgID)); err != nil {
			return nil, err
		}
	} else {
		if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
			return nil, err
		}
	}

	cur, err := loadChangeRequestTx(ctx, tx, s.dialect, reqID, params.OrgID)
	if err != nil {
		return nil, err
	}
	if cur.State != StatePendingSecondApprover {
		return nil, fmt.Errorf("%w: request %s is in state %s", ErrInvalidStateTransition, cur.ID, cur.State)
	}
	if cur.Version != params.ExpectedVersion {
		return nil, port.ErrVersionConflict
	}

	var reasonArg any
	var reasonPtr *string
	if trimmed := strings.TrimSpace(params.Reason); trimmed != "" {
		reasonArg = trimmed
		reasonPtr = &trimmed
	}

	res, err := tx.ExecContext(ctx, port.Rebind(s.dialect, `
		UPDATE admin_change_requests
		SET state = ?, decision_reason = ?, decided_at_ms = ?, version = version + 1
		WHERE id = ? AND version = ? AND state = ?`),
		StateCancelled, reasonArg, now, cur.ID, cur.Version, StatePendingSecondApprover,
	)
	if err != nil {
		return nil, mapSQLiteOrPGError(err)
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		return nil, port.ErrVersionConflict
	}

	if err := s.appendAuditTx(ctx, tx, cur.OrgID, actor, "change_request.cancelled", cur.ID, map[string]any{
		"action_type": cur.ActionType,
		"reason":      params.Reason,
	}, params.CorrelationID, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("foureyes: commit Cancel: %w", err)
	}
	committed = true

	out := *cur
	out.State = StateCancelled
	out.DecisionReason = reasonPtr
	out.Version = cur.Version + 1
	out.DecidedAtMs = &now
	return &out, nil
}

func (s *Service) appendAuditTx(ctx context.Context, tx *sql.Tx, orgID *string, actorID, eventType, targetID string, meta map[string]any, reqID *string, nowMs int64) error {
	if s.txAudit == nil {
		return nil
	}
	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(meta)
	return s.txAudit.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  nowMs,
		OrgID:        orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      actorID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    eventType,
		TargetType:   "admin_change_request",
		TargetID:     targetID,
		RequestID:    reqID,
		MetadataJSON: string(raw),
	})
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func loadChangeRequestTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect, id string, orgID *string) (*ChangeRequest, error) {
	var q string
	var args []any
	if orgID != nil && strings.TrimSpace(*orgID) != "" {
		q = port.Rebind(dialect, `
			SELECT id, org_id, action_type, target_type, target_id,
			       payload_jcs_json, payload_sha256, initiator_principal_id, approver_principal_id,
			       state, decision_reason, version, expires_at_ms, created_at_ms, decided_at_ms
			FROM admin_change_requests
			WHERE id = ? AND org_id = ?`)
		args = []any{id, strings.TrimSpace(*orgID)}
	} else {
		q = port.Rebind(dialect, `
			SELECT id, org_id, action_type, target_type, target_id,
			       payload_jcs_json, payload_sha256, initiator_principal_id, approver_principal_id,
			       state, decision_reason, version, expires_at_ms, created_at_ms, decided_at_ms
			FROM admin_change_requests
			WHERE id = ?`)
		args = []any{id}
	}
	row := tx.QueryRowContext(ctx, q, args...)
	item, err := scanChangeRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, mapSQLiteOrPGError(err)
	}
	return &item, nil
}

func scanChangeRequest(sc sqlScanner) (ChangeRequest, error) {
	var (
		out      ChangeRequest
		orgNS    sql.NullString
		approvNS sql.NullString
		reasonNS sql.NullString
		decided  sql.NullInt64
	)
	if err := sc.Scan(
		&out.ID,
		&orgNS,
		&out.ActionType,
		&out.TargetType,
		&out.TargetID,
		&out.PayloadJCSJSON,
		&out.PayloadSHA256,
		&out.InitiatorPrincipalID,
		&approvNS,
		&out.State,
		&reasonNS,
		&out.Version,
		&out.ExpiresAtMs,
		&out.CreatedAtMs,
		&decided,
	); err != nil {
		return ChangeRequest{}, err
	}
	if orgNS.Valid {
		v := orgNS.String
		out.OrgID = &v
	}
	if approvNS.Valid {
		v := approvNS.String
		out.ApproverPrincipalID = &v
	}
	if reasonNS.Valid {
		v := reasonNS.String
		out.DecisionReason = &v
	}
	if decided.Valid {
		v := decided.Int64
		out.DecidedAtMs = &v
	}
	return out, nil
}

func mapSQLiteOrPGError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %v", port.ErrAlreadyExists, err)
		case "23503", "23514", "22001", "23502":
			return fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "UNIQUE constraint failed") {
		return fmt.Errorf("%w: %v", port.ErrAlreadyExists, err)
	}
	if strings.Contains(msg, "FOREIGN KEY constraint failed") || strings.Contains(msg, "CHECK constraint failed") {
		return fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
	}
	return err
}

