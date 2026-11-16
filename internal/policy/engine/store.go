// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package engine

import (
	"context"
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

// PolicyRecord is a persisted row from the policies table.
type PolicyRecord struct {
	ID          string
	OrgID       string
	ScopeType   string
	ScopeID     string
	PolicyType  string
	FailureMode string
	RulesJSON   string
	ParsedRules snapshot.PolicyRulesView
	Enabled     bool
	Version     int64
	CreatedAtMs int64
	UpdatedAtMs int64
}

// CreatePolicyParams specifies inputs for creating a scoped policy row.
type CreatePolicyParams struct {
	ID             string
	OrgID          string
	ScopeType      string
	ScopeID        string
	PolicyType     string
	FailureMode    string
	RulesJSON      string
	Enabled        bool
	ActorKind      string
	ActorID        string
	EntryPoint     string
	RequestID      *string
	AncestorProjID string
	AncestorEnvID  string
}

// UpdatePolicyParams specifies inputs for optimistic-locked policy updates.
type UpdatePolicyParams struct {
	FailureMode    *string
	RulesJSON      *string
	Enabled        *bool
	ActorKind      string
	ActorID        string
	EntryPoint     string
	RequestID      *string
	AncestorProjID string
	AncestorEnvID  string
}

// Store persists scoped policies in SQLite with write-time Monotonic Narrowing,
// optimistic concurrency, config_changelog notifications, and governance audit logging.
type Store struct {
	db    *sql.DB
	audit port.AuditSink
	nowMs func() int64
}

// StoreConfig configures the policy Store.
type StoreConfig struct {
	DB    *sql.DB
	Audit port.AuditSink
	NowMs func() int64
}

// NewStore constructs a transactional SQLite policy Store.
func NewStore(cfg StoreConfig) (*Store, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	nowFn := cfg.NowMs
	if nowFn == nil {
		nowFn = func() int64 { return time.Now().UTC().UnixMilli() }
	}
	return &Store{
		db:    cfg.DB,
		audit: cfg.Audit,
		nowMs: nowFn,
	}, nil
}

// CreatePolicy validates strict ancestor narrowing inside a transaction, rejects child
// relaxations with ErrPolicyRelaxationForbidden (recording a governance AuditEvent),
// inserts the policy row, and bumps config_changelog.
func (s *Store) CreatePolicy(ctx context.Context, p CreatePolicyParams) (*PolicyRecord, error) {
	orgID := strings.TrimSpace(p.OrgID)
	scopeType := strings.ToUpper(strings.TrimSpace(p.ScopeType))
	scopeID := strings.TrimSpace(p.ScopeID)
	policyType := strings.ToUpper(strings.TrimSpace(p.PolicyType))
	failMode := strings.ToUpper(strings.TrimSpace(p.FailureMode))
	if failMode == "" {
		failMode = snapshot.PolicyFailureClosed
	}
	if orgID == "" || scopeID == "" || ScopeHierarchyRank(scopeType) == 0 || !isValidPolicyType(policyType) {
		return nil, fmt.Errorf("%w: missing or invalid org_id, scope_type, scope_id, or policy_type", ErrInvalidPolicyInput)
	}
	parsedRules, err := snapshot.ParsePolicyRulesJSONStrict(p.RulesJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPolicyInput, err)
	}

	policyID := strings.TrimSpace(p.ID)
	if policyID == "" {
		policyID = idgen.MustNew(idgen.PrefixPolicy)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("policy: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	ancestors, ancFailClosed, err := loadAncestorLayersTx(ctx, tx, orgID, scopeType, scopeID, p.AncestorProjID, p.AncestorEnvID, "")
	if err != nil {
		return nil, err
	}
	cand := CandidatePolicy{
		ScopeType:   scopeType,
		ScopeID:     scopeID,
		PolicyType:  policyType,
		FailureMode: failMode,
		Rules:       parsedRules,
	}
	if vErr := ValidateChildPolicyWithinAncestors(ancestors, ancFailClosed, cand); vErr != nil {
		_ = tx.Rollback()
		if errors.Is(vErr, ErrPolicyRelaxationForbidden) {
			s.emitViolationAudit(ctx, orgID, policyID, scopeType, scopeID, p.ActorKind, p.ActorID, p.EntryPoint, p.RequestID, vErr.Error())
		}
		return nil, vErr
	}

	now := s.nowMs()
	enabledInt := 0
	if p.Enabled {
		enabledInt = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO policies (
			id, org_id, scope_type, scope_id, policy_type,
			failure_mode, rules_json, enabled, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		policyID, orgID, scopeType, scopeID, policyType,
		failMode, p.RulesJSON, enabledInt, now, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, port.ErrAlreadyExists
		}
		return nil, fmt.Errorf("policy: insert policy: %w", err)
	}

	if err := appendPolicyChangelogTx(ctx, tx, orgID, policyID, port.MutationUpsert, now); err != nil {
		return nil, err
	}
	if err := s.emitPolicyMutationAuditTx(ctx, tx, orgID, policyID, scopeType, scopeID, policyType, p.ActorKind, p.ActorID, p.EntryPoint, p.RequestID, "policy.created", now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("policy: commit create: %w", err)
	}

	return &PolicyRecord{
		ID:          policyID,
		OrgID:       orgID,
		ScopeType:   scopeType,
		ScopeID:     scopeID,
		PolicyType:  policyType,
		FailureMode: failMode,
		RulesJSON:   p.RulesJSON,
		ParsedRules: parsedRules,
		Enabled:     p.Enabled,
		Version:     1,
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}, nil
}

// UpdatePolicy applies an optimistic-locked policy update with Monotonic Narrowing checks.
func (s *Store) UpdatePolicy(ctx context.Context, orgID, policyID string, expectedVersion int64, p UpdatePolicyParams) (*PolicyRecord, error) {
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(policyID) == "" || expectedVersion < 1 {
		return nil, ErrInvalidPolicyInput
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("policy: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	cur, err := getPolicyTx(ctx, tx, orgID, policyID)
	if err != nil {
		return nil, err
	}
	if cur.Version != expectedVersion {
		return nil, port.ErrVersionConflict
	}

	nextFailMode := cur.FailureMode
	if p.FailureMode != nil {
		nextFailMode = strings.ToUpper(strings.TrimSpace(*p.FailureMode))
		if nextFailMode != snapshot.PolicyFailureClosed && nextFailMode != snapshot.PolicyFailureOpen {
			return nil, fmt.Errorf("%w: invalid failure_mode %q", ErrInvalidPolicyInput, *p.FailureMode)
		}
	}
	nextRulesJSON := cur.RulesJSON
	nextParsed := cur.ParsedRules
	if p.RulesJSON != nil {
		parsed, perr := snapshot.ParsePolicyRulesJSONStrict(*p.RulesJSON)
		if perr != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPolicyInput, perr)
		}
		nextRulesJSON = *p.RulesJSON
		nextParsed = parsed
	}
	nextEnabled := cur.Enabled
	if p.Enabled != nil {
		nextEnabled = *p.Enabled
	}

	if nextEnabled {
		ancestors, ancFailClosed, err := loadAncestorLayersTx(ctx, tx, orgID, cur.ScopeType, cur.ScopeID, p.AncestorProjID, p.AncestorEnvID, cur.ID)
		if err != nil {
			return nil, err
		}
		cand := CandidatePolicy{
			ScopeType:   cur.ScopeType,
			ScopeID:     cur.ScopeID,
			PolicyType:  cur.PolicyType,
			FailureMode: nextFailMode,
			Rules:       nextParsed,
		}
		if vErr := ValidateChildPolicyWithinAncestors(ancestors, ancFailClosed, cand); vErr != nil {
			_ = tx.Rollback()
			if errors.Is(vErr, ErrPolicyRelaxationForbidden) {
				s.emitViolationAudit(ctx, orgID, policyID, cur.ScopeType, cur.ScopeID, p.ActorKind, p.ActorID, p.EntryPoint, p.RequestID, vErr.Error())
			}
			return nil, vErr
		}
	}

	now := s.nowMs()
	enabledInt := 0
	if nextEnabled {
		enabledInt = 1
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE policies
		SET failure_mode = ?, rules_json = ?, enabled = ?, version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		nextFailMode, nextRulesJSON, enabledInt, now, orgID, policyID, expectedVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("policy: update policy: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil, port.ErrVersionConflict
	}

	if err := appendPolicyChangelogTx(ctx, tx, orgID, policyID, port.MutationUpsert, now); err != nil {
		return nil, err
	}
	if err := s.emitPolicyMutationAuditTx(ctx, tx, orgID, policyID, cur.ScopeType, cur.ScopeID, cur.PolicyType, p.ActorKind, p.ActorID, p.EntryPoint, p.RequestID, "policy.updated", now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("policy: commit update: %w", err)
	}

	cur.FailureMode = nextFailMode
	cur.RulesJSON = nextRulesJSON
	cur.ParsedRules = nextParsed
	cur.Enabled = nextEnabled
	cur.Version++
	cur.UpdatedAtMs = now
	return cur, nil
}

// GetPolicy retrieves a tenant-isolated policy by (orgID, policyID).
func (s *Store) GetPolicy(ctx context.Context, orgID, policyID string) (*PolicyRecord, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	return getPolicyTx(ctx, tx, orgID, policyID)
}

func getPolicyTx(ctx context.Context, tx *sql.Tx, orgID, policyID string) (*PolicyRecord, error) {
	var (
		rec        PolicyRecord
		enabledInt int64
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, org_id, scope_type, scope_id, policy_type,
		       failure_mode, rules_json, enabled, version, created_at_ms, updated_at_ms
		FROM policies
		WHERE org_id = ? AND id = ?`, orgID, policyID,
	).Scan(
		&rec.ID, &rec.OrgID, &rec.ScopeType, &rec.ScopeID, &rec.PolicyType,
		&rec.FailureMode, &rec.RulesJSON, &enabledInt, &rec.Version, &rec.CreatedAtMs, &rec.UpdatedAtMs,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, port.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("policy: query policy: %w", err)
	}
	rec.Enabled = enabledInt != 0
	rec.ParsedRules, err = snapshot.ParsePolicyRulesJSONStrict(rec.RulesJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPolicyEngineUnavailable, err)
	}
	return &rec, nil
}

func loadAncestorLayersTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID, childScopeType, childScopeID, hintProjID, hintEnvID, excludePolicyID string,
) ([]ScopedLayer, bool, error) {
	var layers []ScopedLayer
	var ancFailClosed bool

	// Load and validate organization existence and baseline min_plugin_trust.
	var orgMinTrust string
	err := tx.QueryRowContext(ctx, `SELECT min_plugin_trust FROM organizations WHERE id = ?`, orgID).Scan(&orgMinTrust)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, port.ErrNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("policy: load org baseline: %w", err)
	}
	if strings.TrimSpace(orgMinTrust) != "" {
		layers = append(layers, ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   orgID,
			Baseline:  true,
			Rules:     snapshot.PolicyRulesView{MinPluginTrust: orgMinTrust},
		})
	}

	hintProj := strings.TrimSpace(hintProjID)
	hintEnv := strings.TrimSpace(hintEnvID)
	var projID, envID string

	// Authoritatively validate target scope existence/ownership within orgID and derive ancestors from relational hierarchy.
	switch childScopeType {
	case snapshot.PolicyScopeOrg:
		if childScopeID != orgID {
			return nil, false, fmt.Errorf("%w: ORG scope_id %q must match org_id %q", ErrInvalidPolicyInput, childScopeID, orgID)
		}
	case snapshot.PolicyScopeProject:
		var exists int
		qerr := tx.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, childScopeID).Scan(&exists)
		if errors.Is(qerr, sql.ErrNoRows) {
			return nil, false, port.ErrNotFound
		}
		if qerr != nil {
			return nil, false, fmt.Errorf("policy: verify project scope: %w", qerr)
		}
		projID = childScopeID
		if hintProj != "" && hintProj != projID {
			return nil, false, fmt.Errorf("%w: conflicting project hint %q", ErrInvalidPolicyInput, hintProj)
		}
	case snapshot.PolicyScopeEnv:
		envID = childScopeID
		var pid string
		qerr := tx.QueryRowContext(ctx, `SELECT project_id FROM environments WHERE org_id = ? AND id = ?`, orgID, envID).Scan(&pid)
		if errors.Is(qerr, sql.ErrNoRows) {
			return nil, false, port.ErrNotFound
		}
		if qerr != nil {
			return nil, false, fmt.Errorf("policy: verify environment scope: %w", qerr)
		}
		projID = pid
		if hintProj != "" && hintProj != projID {
			return nil, false, fmt.Errorf("%w: conflicting project hint %q", ErrInvalidPolicyInput, hintProj)
		}
		if hintEnv != "" && hintEnv != envID {
			return nil, false, fmt.Errorf("%w: conflicting environment hint %q", ErrInvalidPolicyInput, hintEnv)
		}
	case snapshot.PolicyScopeVirtualKey:
		var pid, eid string
		qerr := tx.QueryRowContext(ctx, `SELECT project_id, env_id FROM virtual_keys WHERE org_id = ? AND id = ?`, orgID, childScopeID).Scan(&pid, &eid)
		if errors.Is(qerr, sql.ErrNoRows) {
			return nil, false, port.ErrNotFound
		}
		if qerr != nil {
			return nil, false, fmt.Errorf("policy: verify virtual_key scope: %w", qerr)
		}
		projID = pid
		envID = eid
		if hintProj != "" && hintProj != projID {
			return nil, false, fmt.Errorf("%w: conflicting project hint %q", ErrInvalidPolicyInput, hintProj)
		}
		if hintEnv != "" && hintEnv != envID {
			return nil, false, fmt.Errorf("%w: conflicting environment hint %q", ErrInvalidPolicyInput, hintEnv)
		}
	default:
		return nil, false, fmt.Errorf("%w: unknown scope_type %q", ErrInvalidPolicyInput, childScopeType)
	}

	if projID != "" && ScopeHierarchyRank(childScopeType) >= ScopeHierarchyRank(snapshot.PolicyScopeProject) {
		var enforcedReg sql.NullString
		qerr := tx.QueryRowContext(ctx, `SELECT enforced_region FROM projects WHERE org_id = ? AND id = ?`, orgID, projID).Scan(&enforcedReg)
		if qerr != nil && !errors.Is(qerr, sql.ErrNoRows) {
			return nil, false, fmt.Errorf("policy: load project enforced_region: %w", qerr)
		}
		if qerr == nil && enforcedReg.Valid && strings.TrimSpace(enforcedReg.String) != "" {
			layers = append(layers, ScopedLayer{
				ScopeType: snapshot.PolicyScopeProject,
				ScopeID:   projID,
				Baseline:  true,
				Rules: snapshot.PolicyRulesView{
					HasAllowedDataRegions: true,
					AllowedDataRegions:    []string{strings.ToUpper(strings.TrimSpace(enforcedReg.String))},
				},
			})
		}
	}

	if envID != "" && ScopeHierarchyRank(childScopeType) >= ScopeHierarchyRank(snapshot.PolicyScopeEnv) {
		var reqTested int64
		qerr := tx.QueryRowContext(ctx, `SELECT require_tested_caps FROM environments WHERE org_id = ? AND id = ?`, orgID, envID).Scan(&reqTested)
		if qerr != nil && !errors.Is(qerr, sql.ErrNoRows) {
			return nil, false, fmt.Errorf("policy: load environment require_tested_caps: %w", qerr)
		}
		if qerr == nil && reqTested != 0 {
			layers = append(layers, ScopedLayer{
				ScopeType: snapshot.PolicyScopeEnv,
				ScopeID:   envID,
				Baseline:  true,
				Rules:     snapshot.PolicyRulesView{MinCapabilityAssurance: AssuranceTested},
			})
		}
	}

	// Load persisted enabled policies strictly above childScopeType.
	rows, err := tx.QueryContext(ctx, `
		SELECT id, scope_type, scope_id, policy_type, failure_mode, rules_json
		FROM policies
		WHERE org_id = ? AND enabled = 1 AND scope_type IN ('ORG', 'PROJECT', 'ENV')`, orgID)
	if err != nil {
		return nil, false, fmt.Errorf("policy: query ancestor policies: %w", err)
	}
	defer rows.Close()

	childRank := ScopeHierarchyRank(childScopeType)
	for rows.Next() {
		var pid, st, sid, pt, fm, rj string
		if scanErr := rows.Scan(&pid, &st, &sid, &pt, &fm, &rj); scanErr != nil {
			return nil, false, fmt.Errorf("policy: scan ancestor policy: %w", scanErr)
		}
		if pid == excludePolicyID {
			continue
		}
		r := ScopeHierarchyRank(st)
		if r == 0 || r >= childRank {
			continue
		}
		matchesScope := (st == snapshot.PolicyScopeOrg && sid == orgID) ||
			(st == snapshot.PolicyScopeProject && projID != "" && sid == projID) ||
			(st == snapshot.PolicyScopeEnv && envID != "" && sid == envID)
		if !matchesScope {
			continue
		}
		parsed, perr := snapshot.ParsePolicyRulesJSONStrict(rj)
		if perr != nil {
			return nil, false, fmt.Errorf("%w: ancestor policy %q corrupt: %v", ErrPolicyEngineUnavailable, pid, perr)
		}
		if strings.EqualFold(pt, snapshot.PolicyTypeGuardrail) && strings.EqualFold(fm, snapshot.PolicyFailureClosed) {
			ancFailClosed = true
		}
		layers = append(layers, ScopedLayer{
			ScopeType: st,
			ScopeID:   sid,
			Rules:     parsed,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("policy: iterate ancestor policies: %w", err)
	}
	return layers, ancFailClosed, nil
}

func appendPolicyChangelogTx(ctx context.Context, tx *sql.Tx, orgID, policyID, mutation string, nowMs int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'policy', ?, ?, 0, ?)`,
		orgID, policyID, mutation, nowMs,
	)
	if err != nil {
		return fmt.Errorf("policy: append config_changelog: %w", err)
	}
	return nil
}

func (s *Store) emitViolationAudit(
	ctx context.Context,
	orgID, policyID, scopeType, scopeID, actorKind, actorID, entryPoint string,
	requestID *string,
	reason string,
) {
	if s.audit == nil {
		return
	}
	if strings.TrimSpace(actorKind) == "" {
		actorKind = "principal"
	}
	if strings.TrimSpace(actorID) == "" {
		actorID = "unknown"
	}
	if strings.TrimSpace(entryPoint) == "" {
		entryPoint = "admin_api"
	}
	metaBytes, _ := json.Marshal(map[string]string{
		"scope_type": scopeType,
		"scope_id":   scopeID,
		"code":       "policy_relaxation_forbidden",
		"reason":     reason,
	})
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = s.audit.Append(auditCtx, port.AuditEvent{
		ID:           idgen.MustNew(idgen.PrefixAudit),
		CreatedAtMs:  s.nowMs(),
		OrgID:        &orgID,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   entryPoint,
		EventType:    "policy.relaxation_blocked",
		TargetType:   "policy",
		TargetID:     policyID,
		RequestID:    requestID,
		MetadataJSON: string(metaBytes),
	})
}

func (s *Store) emitPolicyMutationAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID, policyID, scopeType, scopeID, policyType, actorKind, actorID, entryPoint string,
	requestID *string,
	eventType string,
	nowMs int64,
) error {
	if s.audit == nil {
		return nil
	}
	txSink, ok := s.audit.(port.TxAuditSink)
	if !ok {
		return nil
	}
	if strings.TrimSpace(actorKind) == "" {
		actorKind = "PRINCIPAL"
	}
	if strings.TrimSpace(actorID) == "" {
		actorID = "system"
	}
	if strings.TrimSpace(entryPoint) == "" {
		entryPoint = port.AuditEntryPointAdminAPI
	}
	audID, idErr := idgen.New(idgen.PrefixAudit)
	if idErr != nil {
		return idErr
	}
	metaBytes, err := json.Marshal(map[string]string{
		"scope_type":  scopeType,
		"scope_id":    scopeID,
		"policy_type": policyType,
	})
	if err != nil {
		return err
	}
	return txSink.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  nowMs,
		OrgID:        &orgID,
		ActorKind:    actorKind,
		ActorID:      actorID,
		EntryPoint:   entryPoint,
		EventType:    eventType,
		TargetType:   "policy",
		TargetID:     policyID,
		RequestID:    requestID,
		MetadataJSON: string(metaBytes),
	})
}

func isValidPolicyType(t string) bool {
	switch t {
	case snapshot.PolicyTypeModelAllowlist,
		snapshot.PolicyTypeDataResidency,
		snapshot.PolicyTypeRateLimit,
		snapshot.PolicyTypeGuardrail,
		snapshot.PolicyTypeMCPGovernance,
		snapshot.PolicyTypeResponseCache:
		return true
	default:
		return false
	}
}
