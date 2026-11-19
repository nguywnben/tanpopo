// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package approval implements the persistence core for key-bound MCP human-in-the-loop approvals.
// Authorization and role checks belong to the HTTP layer.
package approval

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/storage/port"
	"github.com/nguywnben/tanpopo/internal/telemetry/privacy"
)

const (
	// MaxSynchronousWait is the maximum time a synchronous caller may be held for approval.
	MaxSynchronousWait  = 60 * time.Second
	defaultPollInterval = 200 * time.Millisecond
	tokenBytes          = 32
	maxJSONDepth        = 64
)

// State is the persisted approval lifecycle state.
type State string

const (
	StatePending   State = "PENDING"
	StateApproved  State = "APPROVED"
	StateConsumed  State = "CONSUMED"
	StateRejected  State = "REJECTED"
	StateExpired   State = "EXPIRED"
	StateCancelled State = "CANCELLED"
)

// Decision is an authorized HTTP-layer decision. This package intentionally does not perform RBAC.
type Decision string

const (
	DecisionApprove Decision = "APPROVE"
	DecisionReject  Decision = "REJECT"
)

// ErrorCode is a stable machine-readable failure code.
type ErrorCode string

const (
	CodeInvalidInput          ErrorCode = "invalid_input"
	CodeInvalidArguments      ErrorCode = "invalid_arguments"
	CodeNotFound              ErrorCode = "approval_not_found"
	CodeInvalidState          ErrorCode = "approval_invalid_state"
	CodeVersionConflict       ErrorCode = "version_conflict"
	CodeParameterHashMismatch ErrorCode = "parameter_hash_mismatch"
	CodeExpired               ErrorCode = "approval_expired"
	CodeSelfApprovalForbidden ErrorCode = "self_approval_forbidden"
	CodeCapacity              ErrorCode = "approval_capacity_reached"
	CodeInvalidToken          ErrorCode = "invalid_pre_approval_token"
	CodeWaitTimeout           ErrorCode = "approval_wait_timeout"
	CodeStorage               ErrorCode = "approval_storage_error"
)

// Error carries a stable error code while retaining an underlying storage cause when present.
type Error struct {
	Code  ErrorCode
	Cause error
}

func (e *Error) Error() string {
	if e == nil {
		return "approval error"
	}
	if e.Cause != nil {
		return string(e.Code) + ": " + e.Cause.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error { return e.Cause }

// ErrorCodeOf returns the stable code associated with an error, if it is an approval Error.
func ErrorCodeOf(err error) (ErrorCode, bool) {
	var approvalErr *Error
	if errors.As(err, &approvalErr) {
		return approvalErr.Code, true
	}
	return "", false
}

// Binding is the complete five-field MCP approval binding. Arguments must contain one strict JSON value.
type Binding struct {
	OrgID                 string
	ProjectID             string // Notification and review scope; intentionally excluded from the five-tuple digest.
	VirtualKeyID          string
	ServerSlug            string
	ToolName              string
	Arguments             json.RawMessage
	CredentialPrincipalID string // Credential audience identity; excluded from the five-tuple digest.
	ServerID              string
	ToolID                string
	ApprovedSchemaHash    string
}

// Request is safe to return from lookup and polling; it never contains a pre-approval token.
type Request struct {
	ID                   string
	Binding              Binding
	State                State
	Version              int64
	ExpiresAt            time.Time
	CreatedAt            time.Time
	DecidedAt            *time.Time
	ConsumedAt           *time.Time
	DecidedByPrincipalID string
	DecisionReason       string
}

// CreatedRequest returns the raw pre-approval token exactly once, at creation.
type CreatedRequest struct {
	Request
	PreApprovalToken string
}

// Config configures the approval store.
type Config struct {
	DB                   *sql.DB
	Audit                port.TxAuditSink
	Now                  func() time.Time
	PollInterval         time.Duration
	RecordPendingEventTx func(context.Context, *sql.Tx, string, Binding) error
	MaxPendingPerOrg     int
	MaxPendingPerKey     int
	Retention            time.Duration
}

// Store persists approval state in the existing approval_requests table.
type Store struct {
	db                   *sql.DB
	audit                port.TxAuditSink
	now                  func() time.Time
	pollInterval         time.Duration
	recordPendingEventTx func(context.Context, *sql.Tx, string, Binding) error
	maxPendingPerOrg     int
	maxPendingPerKey     int
	retention            time.Duration
}

// NewStore constructs a fail-closed SQLite-backed approval store.
func NewStore(cfg Config) (*Store, error) {
	if cfg.DB == nil || cfg.Audit == nil {
		return nil, &Error{Code: CodeInvalidInput}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.PollInterval > time.Second {
		cfg.PollInterval = time.Second
	}
	if cfg.MaxPendingPerOrg <= 0 || cfg.MaxPendingPerOrg > 1000 {
		cfg.MaxPendingPerOrg = 1000
	}
	if cfg.MaxPendingPerKey <= 0 || cfg.MaxPendingPerKey > 100 {
		cfg.MaxPendingPerKey = 100
	}
	if cfg.Retention <= 0 {
		cfg.Retention = 30 * 24 * time.Hour
	}
	return &Store{db: cfg.DB, audit: cfg.Audit, now: cfg.Now, pollInterval: cfg.PollInterval, recordPendingEventTx: cfg.RecordPendingEventTx,
		maxPendingPerOrg: cfg.MaxPendingPerOrg, maxPendingPerKey: cfg.MaxPendingPerKey, retention: cfg.Retention}, nil
}

// Create inserts a PENDING MCP approval and returns a random single-use token once.
// Only SHA-256(token) is persisted.
func (s *Store) Create(ctx context.Context, binding Binding, requesterPrincipalID string, expiresAt time.Time) (CreatedRequest, error) {
	var out CreatedRequest
	_, tupleHash, target, err := validateAndBind(binding)
	if err != nil {
		return out, err
	}
	canonicalArgs, err := strictCanonicalJSON(binding.Arguments)
	if err != nil {
		return out, &Error{Code: CodeInvalidArguments, Cause: err}
	}
	redacted := privacy.NewDefaultRedactor().SanitizeDebugPayload(canonicalArgs, 6*1024)
	var previewArgs any
	if err := json.Unmarshal(redacted, &previewArgs); err != nil {
		return out, &Error{Code: CodeInvalidArguments, Cause: err}
	}
	previewArgs = redactApprovalPreview(previewArgs)
	preview, err := json.Marshal(map[string]any{"server_slug": binding.ServerSlug, "tool_name": binding.ToolName, "arguments": previewArgs})
	if err != nil || len(preview) > 8*1024 {
		return out, &Error{Code: CodeInvalidArguments, Cause: err}
	}
	now := s.now().UTC()
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(now) || expiresAt.UnixMilli() <= now.UnixMilli() {
		return out, &Error{Code: CodeInvalidInput}
	}
	id, err := idgen.New(idgen.PrefixApproval)
	if err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	rawToken := make([]byte, tokenBytes)
	if _, err := rand.Read(rawToken); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	for i := range rawToken {
		rawToken[i] = 0
	}
	tokenHash := sha256.Sum256([]byte(token))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	defer func() { _ = tx.Rollback() }()
	// Lock the organization row before count-and-insert admission. This makes
	// per-organization and per-key capacity checks serializable across callers.
	lock, err := tx.ExecContext(ctx, `UPDATE organizations SET id=id WHERE id=?`, binding.OrgID)
	if err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	locked, err := lock.RowsAffected()
	if err != nil || locked != 1 {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	cutoff := now.Add(-s.retention).UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE approval_requests SET payload_preview_json='{}', pre_approval_token_hash=NULL
		WHERE org_id=? AND expires_at_ms<? AND payload_preview_json<>'{}'`, binding.OrgID, now.UnixMilli()); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM approval_requests WHERE org_id=? AND expires_at_ms<?`, binding.OrgID, cutoff); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	var pendingOrg, pendingKey int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM approval_requests WHERE org_id=? AND state IN ('PENDING','APPROVED') AND expires_at_ms>?`, binding.OrgID, now.UnixMilli()).Scan(&pendingOrg); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM approval_requests WHERE org_id=? AND requester_virtual_key_id=? AND state IN ('PENDING','APPROVED') AND expires_at_ms>?`, binding.OrgID, binding.VirtualKeyID, now.UnixMilli()).Scan(&pendingKey); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	if pendingOrg >= s.maxPendingPerOrg || pendingKey >= s.maxPendingPerKey {
		return out, &Error{Code: CodeCapacity}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO approval_requests (
			id, org_id, project_id, action_kind, requester_virtual_key_id, requester_principal_id, requester_credential_principal_id,
			 approved_mcp_server_id, approved_mcp_tool_id, approved_schema_sha256,
			 target_identifier, payload_preview_json, params_jcs_sha256, state, pre_approval_token_hash,
			version, expires_at_ms, created_at_ms
		) VALUES (?, ?, ?, 'MCP_DESTRUCTIVE_CALL', ?, ?, ?, ?, ?, ?, ?, ?, ?, 'PENDING', ?, 1, ?, ?)`,
		id, binding.OrgID, nullableString(binding.ProjectID), binding.VirtualKeyID, nullableString(requesterPrincipalID), nullableString(binding.CredentialPrincipalID),
		nullableString(binding.ServerID), nullableString(binding.ToolID), nullableString(binding.ApprovedSchemaHash), target, string(preview),
		tupleHash, tokenHash[:], expiresAt.UnixMilli(), now.UnixMilli())
	if err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	if err := s.appendAuditTx(ctx, tx, id, binding.OrgID, "VIRTUAL_KEY", binding.VirtualKeyID, "mcp.approval.requested", map[string]string{"binding_sha256": tupleHash}); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	if s.recordPendingEventTx != nil {
		if err := s.recordPendingEventTx(ctx, tx, id, cloneBinding(binding)); err != nil {
			return out, &Error{Code: CodeStorage, Cause: err}
		}
	}
	if err := tx.Commit(); err != nil {
		return out, &Error{Code: CodeStorage, Cause: err}
	}
	out.Request = Request{ID: id, Binding: cloneBinding(binding), State: StatePending, Version: 1, ExpiresAt: expiresAt, CreatedAt: now}
	out.PreApprovalToken = token
	return out, nil
}

// CleanupExpired removes expired previews/token hashes and prunes old request rows.
// Audit events remain immutable and are not deleted with request records.
func (s *Store) CleanupExpired(ctx context.Context) error {
	now := s.now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE approval_requests SET payload_preview_json='{}', pre_approval_token_hash=NULL
		WHERE expires_at_ms<? AND (payload_preview_json<>'{}' OR pre_approval_token_hash IS NOT NULL)`, now.UnixMilli()); err != nil {
		return err
	}
	cutoff := now.Add(-s.retention).UnixMilli()
	if _, err := tx.ExecContext(ctx, `DELETE FROM approval_requests WHERE expires_at_ms<?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

func redactApprovalPreview(value any) any {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			lower := strings.ToLower(key)
			if privacy.IsSensitiveKey(key) || strings.Contains(lower, "private") {
				current[key] = "[REDACTED]"
			} else {
				current[key] = redactApprovalPreview(item)
			}
		}
		return current
	case []any:
		for index, item := range current {
			current[index] = redactApprovalPreview(item)
		}
		return current
	default:
		return value
	}
}

// Decide transitions a pending request using an expected version and exact tuple binding.
// The HTTP layer must authenticate and authorize actorPrincipalID before calling this method.
func (s *Store) Decide(ctx context.Context, requestID string, expected Binding, expectedVersion int64, actorPrincipalID string, decision Decision, reason string) (Request, error) {
	if strings.TrimSpace(actorPrincipalID) == "" || expectedVersion < 1 || (decision != DecisionApprove && decision != DecisionReject) {
		return Request{}, &Error{Code: CodeInvalidInput}
	}
	_, wantedHash, wantedTarget, err := validateAndBind(expected)
	if err != nil {
		return Request{}, err
	}
	now := s.now().UTC().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	defer func() { _ = tx.Rollback() }()
	var orgID, keyID, target, paramsHash, state, requester sql.NullString
	var version, expiresAt, createdAt int64
	if err := tx.QueryRowContext(ctx, `SELECT org_id, requester_virtual_key_id, target_identifier, params_jcs_sha256, state, version, expires_at_ms, created_at_ms, requester_principal_id FROM approval_requests WHERE id = ?`, requestID).
		Scan(&orgID, &keyID, &target, &paramsHash, &state, &version, &expiresAt, &createdAt, &requester); err != nil {
		return Request{}, mapReadError(err)
	}
	if !orgID.Valid || !keyID.Valid || orgID.String != expected.OrgID || keyID.String != expected.VirtualKeyID {
		return Request{}, &Error{Code: CodeParameterHashMismatch}
	}
	if !target.Valid || target.String != wantedTarget || !equalHexHash(paramsHash.String, wantedHash) {
		return Request{}, &Error{Code: CodeParameterHashMismatch}
	}
	if version != expectedVersion {
		return Request{}, &Error{Code: CodeVersionConflict}
	}
	if state.String != string(StatePending) {
		return Request{}, &Error{Code: CodeInvalidState}
	}
	if expiresAt <= now {
		result, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state='EXPIRED', version=version+1 WHERE id=? AND state='PENDING' AND version=? AND expires_at_ms<=?`, requestID, expectedVersion, now)
		if err != nil {
			return Request{}, &Error{Code: CodeStorage, Cause: err}
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			if err != nil {
				return Request{}, &Error{Code: CodeStorage, Cause: err}
			}
			return Request{}, &Error{Code: CodeVersionConflict}
		}
		if err := tx.Commit(); err != nil {
			return Request{}, &Error{Code: CodeStorage, Cause: err}
		}
		return Request{}, &Error{Code: CodeExpired}
	}
	if requester.Valid && requester.String == actorPrincipalID {
		return Request{}, &Error{Code: CodeSelfApprovalForbidden}
	}
	next := StateApproved
	if decision == DecisionReject {
		next = StateRejected
	}
	result, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state=?, decided_by_principal_id=?, decision_reason=?, decided_at_ms=?, version=version+1 WHERE id=? AND state='PENDING' AND version=? AND expires_at_ms>?`, next, actorPrincipalID, reason, now, requestID, expectedVersion, now)
	if err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Request{}, &Error{Code: CodeStorage, Cause: err}
		}
		return Request{}, &Error{Code: CodeVersionConflict}
	}
	if err := s.appendAuditTx(ctx, tx, requestID, expected.OrgID, "PRINCIPAL", actorPrincipalID, "mcp.approval.decided", map[string]string{"binding_sha256": wantedHash, "decision": string(decision)}); err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	if err := tx.Commit(); err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	decided := time.UnixMilli(now).UTC()
	return Request{ID: requestID, Binding: cloneBinding(expected), State: next, Version: expectedVersion + 1, ExpiresAt: time.UnixMilli(expiresAt).UTC(), CreatedAt: time.UnixMilli(createdAt).UTC(), DecidedAt: &decided, DecidedByPrincipalID: actorPrincipalID, DecisionReason: reason}, nil
}

// Consume atomically spends an approved token only for the exact five-field tuple.
func (s *Store) Consume(ctx context.Context, token string, binding Binding) (Request, error) {
	if strings.TrimSpace(token) == "" {
		return Request{}, &Error{Code: CodeInvalidToken}
	}
	_, tupleHash, target, err := validateAndBind(binding)
	if err != nil {
		return Request{}, err
	}
	tokenHash := sha256.Sum256([]byte(token))
	now := s.now().UTC().UnixMilli()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	defer func() { _ = tx.Rollback() }()
	var id, orgID, keyID, storedTarget, storedHash, state string
	var serverID, toolID, schemaHash sql.NullString
	var version, expiresAt, createdAt int64
	var requester, decidedBy, reason sql.NullString
	var decidedAt, consumedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, org_id, requester_virtual_key_id, target_identifier, params_jcs_sha256, state, version, expires_at_ms, created_at_ms, requester_principal_id, decided_by_principal_id, decision_reason, decided_at_ms, consumed_at_ms, approved_mcp_server_id, approved_mcp_tool_id, approved_schema_sha256 FROM approval_requests WHERE pre_approval_token_hash=?`, tokenHash[:]).
		Scan(&id, &orgID, &keyID, &storedTarget, &storedHash, &state, &version, &expiresAt, &createdAt, &requester, &decidedBy, &reason, &decidedAt, &consumedAt, &serverID, &toolID, &schemaHash)
	if err != nil {
		return Request{}, mapReadError(err)
	}
	if orgID != binding.OrgID || keyID != binding.VirtualKeyID || storedTarget != target || !equalHexHash(storedHash, tupleHash) {
		return Request{}, &Error{Code: CodeParameterHashMismatch}
	}
	if serverID.Valid && serverID.String != binding.ServerID || toolID.Valid && toolID.String != binding.ToolID || schemaHash.Valid && schemaHash.String != binding.ApprovedSchemaHash {
		return Request{}, &Error{Code: CodeParameterHashMismatch}
	}
	if state != string(StateApproved) && state != string(StatePending) && state != string(StateExpired) {
		if state == string(StateConsumed) {
			return Request{}, &Error{Code: CodeParameterHashMismatch}
		}
		return Request{}, &Error{Code: CodeInvalidState}
	}
	if expiresAt <= now {
		if state == string(StatePending) || state == string(StateApproved) {
			result, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state='EXPIRED', version=version+1 WHERE id=? AND state=? AND version=? AND expires_at_ms<=?`, id, state, version, now)
			if err != nil {
				return Request{}, &Error{Code: CodeStorage, Cause: err}
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				if err != nil {
					return Request{}, &Error{Code: CodeStorage, Cause: err}
				}
				return Request{}, &Error{Code: CodeVersionConflict}
			}
			if err := tx.Commit(); err != nil {
				return Request{}, &Error{Code: CodeStorage, Cause: err}
			}
		}
		return Request{}, &Error{Code: CodeExpired}
	}
	if state != string(StateApproved) {
		return Request{}, &Error{Code: CodeInvalidState}
	}
	result, err := tx.ExecContext(ctx, `UPDATE approval_requests SET state='CONSUMED', consumed_at_ms=?, version=version+1 WHERE id=? AND state='APPROVED' AND version=? AND expires_at_ms>? AND pre_approval_token_hash=?`, now, id, version, now, tokenHash[:])
	if err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Request{}, &Error{Code: CodeStorage, Cause: err}
		}
		return Request{}, &Error{Code: CodeVersionConflict}
	}
	if err := s.appendAuditTx(ctx, tx, id, binding.OrgID, "VIRTUAL_KEY", binding.VirtualKeyID, "mcp.approval.consumed", map[string]string{"binding_sha256": tupleHash}); err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	if err := tx.Commit(); err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	consumed := time.UnixMilli(now).UTC()
	out := Request{ID: id, Binding: cloneBinding(binding), State: StateConsumed, Version: version + 1, ExpiresAt: time.UnixMilli(expiresAt).UTC(), CreatedAt: time.UnixMilli(createdAt).UTC(), DecisionReason: reason.String, DecidedByPrincipalID: decidedBy.String, ConsumedAt: &consumed}
	if decidedAt.Valid {
		v := time.UnixMilli(decidedAt.Int64).UTC()
		out.DecidedAt = &v
	}
	return out, nil
}

func (s *Store) appendAuditTx(ctx context.Context, tx *sql.Tx, targetID, orgID, actorKind, actorID, eventType string, metadata map[string]string) error {
	id, err := idgen.New(idgen.PrefixTrace)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return s.audit.AppendTx(ctx, tx, port.AuditEvent{ID: id, CreatedAtMs: s.now().UTC().UnixMilli(), OrgID: &orgID,
		ActorKind: actorKind, ActorID: actorID, EntryPoint: port.AuditEntryPointDataPlaneGuard,
		EventType: eventType, TargetType: "approval_request", TargetID: targetID, MetadataJSON: string(encoded)})
}

// Lookup returns a request only when both organization and virtual key match. It never returns token material.
func (s *Store) Lookup(ctx context.Context, orgID, virtualKeyID, requestID string) (Request, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(virtualKeyID) == "" || strings.TrimSpace(requestID) == "" {
		return Request{}, &Error{Code: CodeInvalidInput}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE approval_requests SET state='EXPIRED', version=version+1 WHERE id=? AND org_id=? AND requester_virtual_key_id=? AND state IN ('PENDING','APPROVED') AND expires_at_ms<=?`, requestID, orgID, virtualKeyID, s.now().UTC().UnixMilli()); err != nil {
		return Request{}, &Error{Code: CodeStorage, Cause: err}
	}
	var out Request
	var state string
	var target string
	var expiresAt, createdAt, version int64
	var decidedAt, consumedAt sql.NullInt64
	var decidedBy, reason sql.NullString
	var serverID, toolID, schemaHash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, target_identifier, state, version, expires_at_ms, created_at_ms, decided_at_ms, consumed_at_ms, decided_by_principal_id, decision_reason, approved_mcp_server_id, approved_mcp_tool_id, approved_schema_sha256 FROM approval_requests WHERE id=? AND org_id=? AND requester_virtual_key_id=?`, requestID, orgID, virtualKeyID).
		Scan(&out.ID, &target, &state, &version, &expiresAt, &createdAt, &decidedAt, &consumedAt, &decidedBy, &reason, &serverID, &toolID, &schemaHash)
	if err != nil {
		return Request{}, mapReadError(err)
	}
	var targetInfo struct {
		ServerSlug string `json:"server_slug"`
		ToolName   string `json:"tool_name"`
	}
	if err := json.Unmarshal([]byte(target), &targetInfo); err != nil || targetInfo.ServerSlug == "" || targetInfo.ToolName == "" {
		return Request{}, &Error{Code: CodeStorage, Cause: errors.New("invalid persisted target identifier")}
	}
	out.Binding = Binding{OrgID: orgID, VirtualKeyID: virtualKeyID, ServerSlug: targetInfo.ServerSlug, ToolName: targetInfo.ToolName}
	out.Binding.ServerID, out.Binding.ToolID, out.Binding.ApprovedSchemaHash = serverID.String, toolID.String, schemaHash.String
	out.State, out.Version = State(state), version
	out.ExpiresAt, out.CreatedAt = time.UnixMilli(expiresAt).UTC(), time.UnixMilli(createdAt).UTC()
	out.DecidedByPrincipalID, out.DecisionReason = decidedBy.String, reason.String
	if decidedAt.Valid {
		v := time.UnixMilli(decidedAt.Int64).UTC()
		out.DecidedAt = &v
	}
	if consumedAt.Valid {
		v := time.UnixMilli(consumedAt.Int64).UTC()
		out.ConsumedAt = &v
	}
	return out, nil
}

// Wait polls a key-scoped request until it becomes terminal or maxWait elapses.
// maxWait is capped at MaxSynchronousWait. A timeout returns the latest request and CodeWaitTimeout.
func (s *Store) Wait(ctx context.Context, orgID, virtualKeyID, requestID string, maxWait time.Duration) (Request, error) {
	if maxWait <= 0 {
		return Request{}, &Error{Code: CodeInvalidInput}
	}
	if maxWait > MaxSynchronousWait {
		maxWait = MaxSynchronousWait
	}
	deadline := time.NewTimer(maxWait)
	defer deadline.Stop()
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		request, err := s.Lookup(ctx, orgID, virtualKeyID, requestID)
		if err != nil {
			return Request{}, err
		}
		if request.State != StatePending {
			return request, nil
		}
		select {
		case <-ctx.Done():
			return Request{}, ctx.Err()
		case <-deadline.C:
			return request, &Error{Code: CodeWaitTimeout}
		case <-ticker.C:
		}
	}
}

func validateAndBind(binding Binding) ([]byte, string, string, error) {
	if strings.TrimSpace(binding.OrgID) == "" || strings.TrimSpace(binding.VirtualKeyID) == "" || strings.TrimSpace(binding.ServerSlug) == "" || strings.TrimSpace(binding.ToolName) == "" || !utf8.ValidString(binding.ServerSlug) || !utf8.ValidString(binding.ToolName) {
		return nil, "", "", &Error{Code: CodeInvalidInput}
	}
	canonical, err := strictCanonicalJSON(binding.Arguments)
	if err != nil {
		return nil, "", "", &Error{Code: CodeInvalidArguments, Cause: err}
	}
	tuple, err := audit.CanonicalizeValue(map[string]any{
		"org_id": binding.OrgID, "virtual_key_id": binding.VirtualKeyID,
		"server_slug": binding.ServerSlug, "tool_name": binding.ToolName,
		"arguments": json.RawMessage(canonical),
	})
	if err != nil {
		return nil, "", "", &Error{Code: CodeInvalidArguments, Cause: err}
	}
	// CanonicalizeValue marshals json.RawMessage through JSON before canonicalizing it.
	digest := sha256.Sum256(tuple)
	targetBytes, err := audit.CanonicalizeValue(map[string]any{"server_slug": binding.ServerSlug, "tool_name": binding.ToolName})
	if err != nil || len(targetBytes) > 160 {
		return nil, "", "", &Error{Code: CodeInvalidInput, Cause: err}
	}
	return canonical, hex.EncodeToString(digest[:]), string(targetBytes), nil
}

// CanonicalizeArguments returns the exact RFC 8785 bytes used for approval
// binding. Approved actions must dispatch these bytes rather than the original
// JSON spelling so the reviewer-approved values are the values executed.
func CanonicalizeArguments(raw []byte) ([]byte, error) {
	return strictCanonicalJSON(raw)
}

func strictCanonicalJSON(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := checkJSONValue(dec, 0); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing JSON value or malformed suffix")
	}
	return audit.CanonicalizeJSON(raw)
}

func checkJSONValue(dec *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return errors.New("JSON nesting exceeds limit")
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if number, ok := tok.(json.Number); ok {
		if err := rejectUnsafeInteger(number); err != nil {
			return err
		}
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return errors.New("duplicate object key")
			}
			seen[key] = struct{}{}
			if err := checkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("unterminated object")
		}
	case '[':
		for dec.More() {
			if err := checkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("unterminated array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func rejectUnsafeInteger(number json.Number) error {
	rational, ok := new(big.Rat).SetString(number.String())
	if !ok || !rational.IsInt() {
		return nil
	}
	integer := rational.Num()
	limit := big.NewInt(9007199254740991)
	if new(big.Int).Abs(integer).Cmp(limit) > 0 {
		return errors.New("integer exceeds the exact RFC 8785 binary64 range")
	}
	return nil
}

func mapReadError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Code: CodeNotFound}
	}
	return &Error{Code: CodeStorage, Cause: err}
}

func equalHexHash(stored, wanted string) bool {
	decoded, err := hex.DecodeString(stored)
	if err != nil || len(decoded) != sha256.Size {
		return false
	}
	wantedBytes, err := hex.DecodeString(wanted)
	return err == nil && subtle.ConstantTimeCompare(decoded, wantedBytes) == 1
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func cloneBinding(binding Binding) Binding {
	binding.Arguments = append(json.RawMessage(nil), binding.Arguments...)
	return binding
}
