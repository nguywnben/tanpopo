// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type approvalResource struct {
	ID                   string         `json:"id"`
	OrgID                string         `json:"org_id"`
	ProjectID            string         `json:"project_id"`
	ActionKind           string         `json:"action_kind"`
	State                approval.State `json:"state"`
	Hash                 string         `json:"approval_binding_hash"`
	Target               string         `json:"payload_preview_json"`
	RequestedBy          string         `json:"requested_by"`
	DecidedBy            *string        `json:"decided_by"`
	CreatedAt            string         `json:"created_at"`
	ExpiresAt            string         `json:"expires_at"`
	Version              int64          `json:"version"`
	SchemaDriftLocked    bool           `json:"schema_drift_locked"`
	createdMs, expiresMs int64
	selfRequested        bool
}

// MCP requests inherit their project from the requesting virtual key. The key
// owner, the request-time credential recipient, and current active/previous key
// recipients are all subject to four-eyes checks, including after rotation.
const effectiveApprovalState = `CASE WHEN a.state IN ('PENDING','APPROVED') AND a.expires_at_ms <= ?
	THEN 'EXPIRED' ELSE a.state END`

func approvalSelectQuery(d port.Dialect) string {
	serverExpr := `json_extract(a.target_identifier,'$.server_slug')`
	toolExpr := `json_extract(a.target_identifier,'$.tool_name')`
	if d == port.DialectPostgres {
		serverExpr = `(a.target_identifier::jsonb)->>'server_slug'`
		toolExpr = `(a.target_identifier::jsonb)->>'tool_name'`
	}
	return `SELECT a.id,a.org_id,k.project_id,` + effectiveApprovalState + `,a.params_jcs_sha256,a.payload_preview_json,
	COALESCE(a.requester_principal_id,k.id),a.decided_by_principal_id,a.created_at_ms,a.expires_at_ms,a.version,
	(COALESCE(a.requester_principal_id,'')=? OR COALESCE(a.requester_credential_principal_id,'')=? OR COALESCE(k.owner_principal_id,'')=?
	 OR COALESCE(k.active_key_recipient_principal_id,'')=? OR COALESCE(k.previous_key_recipient_principal_id,'')=?),
	EXISTS(SELECT 1 FROM mcp_tools t JOIN mcp_servers ms ON ms.org_id=t.org_id AND ms.id=t.mcp_server_id
	 WHERE t.org_id=a.org_id AND ms.server_slug=` + serverExpr + `
	 AND t.tool_name=` + toolExpr + ` AND t.schema_drift_locked=` + port.BoolExpr(d, true) + `)
	FROM approval_requests a JOIN virtual_keys k ON k.org_id=a.org_id AND k.id=a.requester_virtual_key_id
	WHERE a.org_id=? AND a.action_kind='MCP_DESTRUCTIVE_CALL' AND (a.project_id IS NULL OR a.project_id=k.project_id)`
}

type governanceRow interface{ Scan(...any) error }

func readApproval(row governanceRow) (approvalResource, error) {
	var a approvalResource
	var selfReq, driftLocked port.FlexBool
	err := row.Scan(&a.ID, &a.OrgID, &a.ProjectID, &a.State, &a.Hash, &a.Target, &a.RequestedBy, &a.DecidedBy, &a.createdMs, &a.expiresMs, &a.Version, &selfReq, &driftLocked)
	a.selfRequested = bool(selfReq)
	a.SchemaDriftLocked = bool(driftLocked)
	a.ActionKind = "MCP_DESTRUCTIVE_TOOL"
	a.CreatedAt = formatTimeRFC3339Ms(a.createdMs)
	a.ExpiresAt = formatTimeRFC3339Ms(a.expiresMs)
	return a, err
}

// Each project must independently grant this action. An unrelated org-wide role
// or a second, less privileged project binding cannot widen a collection query.
func governanceListScope(w http.ResponseWriter, r *http.Request, sc *authSessionContext, org string, action rbac.Action, projectColumn string) (string, []any, bool) {
	if rbac.Authorize(sc.Actor, action, rbac.TargetScope{OrgID: org}) == nil {
		return "", nil, true
	}
	var ids []any
	for _, id := range actorProjectIDs(sc.Actor, org) {
		if rbac.Authorize(sc.Actor, action, rbac.TargetScope{OrgID: org, ProjectID: id}) == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		writeErrorEnvelope(w, r, 403, "insufficient_role_permission", "permission_error", "insufficient role permission", false, nil)
		return "", nil, false
	}
	return " AND " + projectColumn + " IN (" + strings.Repeat("?,", len(ids)-1) + "?)", ids, true
}

func governancePage[T any](s *Server, w http.ResponseWriter, r *http.Request, resource, org, filter, query, createdColumn, idColumn string, args []any, scan func(governanceRow) (T, error), position func(T) (int64, string)) {
	limit, cursor, ok := parsePageParams(w, r, s.cursorSecret, resource, org, filter)
	if !ok {
		return
	}
	if cursor != nil {
		query += " AND (" + createdColumn + "," + idColumn + ") < (?,?)"
		args = append(args, cursor.CreatedMs, cursor.LastID)
	}
	query += " ORDER BY " + createdColumn + " DESC," + idColumn + " DESC LIMIT ?"
	page := paginatedListEnvelope[T]{Data: []T{}}
	var encodeFailed bool
	err := s.withOrgReadTx(r.Context(), org, func(q adminDBQuerier) error {
		rows, err := q.QueryContext(r.Context(), s.rebind(query), append(args, limit+1)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if len(page.Data) == limit {
				ms, id := position(page.Data[limit-1])
				token, err := encodePaginationCursor(s.cursorSecret, resource, org, filter, ms, id)
				if err != nil {
					encodeFailed = true
					return err
				}
				page.NextCursor = &token
				page.HasMore = true
				break
			}
			item, err := scan(rows)
			if err != nil {
				return err
			}
			page.Data = append(page.Data, item)
		}
		return rows.Err()
	})
	if encodeFailed {
		mcpInternal(w, r, "failed encoding pagination cursor")
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed listing "+resource)
		return
	}
	writeJSON(w, 200, page)
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	org, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	scope, params, ok := governanceListScope(w, r, sc, org, rbac.ActionApprovalRead, "k.project_id")
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	query := approvalSelectQuery(s.dialect) + scope
	now := s.nowMs()
	args := append([]any{now, sc.Actor.PrincipalID, sc.Actor.PrincipalID, sc.Actor.PrincipalID, sc.Actor.PrincipalID, sc.Actor.PrincipalID, org}, params...)
	switch approval.State(status) {
	case "":
	case approval.StatePending, approval.StateApproved, approval.StateConsumed, approval.StateExpired, approval.StateRejected, approval.StateCancelled:
		query += " AND " + effectiveApprovalState + "=?"
		args = append(args, now, status)
	default:
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "unknown approval status", false, nil)
		return
	}
	governancePage(s, w, r, "approvals", org, status, query, "a.created_at_ms", "a.id", args, readApproval, func(a approvalResource) (int64, string) { return a.createdMs, a.ID })
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	org, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	// SM-06 requires an explicit reviewer binding even for instance operators.
	reviewer := *sc.Actor
	reviewer.Bindings = nil
	for _, b := range sc.Actor.Bindings {
		switch b.RoleName {
		case rbac.RoleSecurityReviewer, rbac.RoleOrgOwner, rbac.RoleTeamAdmin:
			reviewer.Bindings = append(reviewer.Bindings, b)
		}
	}
	reviewSession := *sc
	reviewSession.Actor = &reviewer
	scope, params, ok := governanceListScope(w, r, &reviewSession, org, rbac.ActionApprovalDecide, "k.project_id")
	if !ok {
		return
	}
	version, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req struct {
		Decision approval.Decision `json:"decision"`
		Hash     string            `json:"expected_binding_hash"`
		Reason   *string           `json:"reason"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	validDecision := req.Decision == approval.DecisionApprove || req.Decision == approval.DecisionReject
	validHash := len(req.Hash) == 64 && isLowerHex(req.Hash)
	validReason := req.Reason != nil && utf8.RuneCountInString(strings.TrimSpace(*req.Reason)) >= 5
	if !validDecision || !validHash || !validReason {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "invalid decision, binding hash, or reason", false, nil)
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		mcpInternal(w, r, "failed starting approval decision")
		return
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(ctx, tx, s.dialect, org); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}
	actor := sc.Actor.PrincipalID
	// Serialize the decision with virtual-key rotation before evaluating the
	// current recipient in approvalSelect. This makes the Four-Eyes check and
	// rotation observe one transaction order on both SQLite and PostgreSQL.
	locked, err := tx.ExecContext(ctx, s.rebind(`UPDATE virtual_keys SET id=id
		WHERE org_id=? AND id=(SELECT requester_virtual_key_id FROM approval_requests WHERE org_id=? AND id=?)`),
		org, org, r.PathValue("approvalId"))
	if err != nil {
		mcpInternal(w, r, "failed locking approval requester key")
		return
	}
	lockCount, err := locked.RowsAffected()
	if err != nil {
		mcpInternal(w, r, "failed verifying approval requester key lock")
		return
	}
	if lockCount != 1 {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "approval not found", false, nil)
		return
	}
	args := append([]any{s.nowMs(), actor, actor, actor, actor, actor, org, r.PathValue("approvalId")}, params...)
	a, err := readApproval(tx.QueryRowContext(ctx, s.rebind(approvalSelectQuery(s.dialect)+" AND a.id=?"+scope), args...))
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "approval not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading approval")
		return
	}
	if a.Version != version {
		writeOptimisticConflict(w, r, a.Version, a)
		return
	}
	if a.selfRequested {
		s.denyApprovalDecision(w, r, tx, a, actor, "mcp.self_approval_blocked",
			approval.CodeSelfApprovalForbidden, "requesters cannot decide their own approval")
		return
	}
	if a.Hash != req.Hash {
		s.denyApprovalDecision(w, r, tx, a, actor, "mcp.approval_binding_mismatch",
			approval.CodeParameterHashMismatch, "approval binding differs from the reviewed request")
		return
	}
	if a.SchemaDriftLocked && req.Decision == approval.DecisionApprove {
		writeErrorEnvelope(w, r, http.StatusConflict, "mcp_schema_drift_blocked", "conflict_error", "MCP schema drift must be reviewed and re-approved before this call can be approved", false, nil)
		return
	}

	now := s.nowMs()
	if a.State != approval.StatePending || a.expiresMs <= now {
		writeErrorEnvelope(w, r, 409, "approval_already_decided_or_expired", "conflict_error", "approval is no longer pending or has expired", false, nil)
		return
	}
	next := approval.StateApproved
	if req.Decision == approval.DecisionReject {
		next = approval.StateRejected
	}
	// Admins attest to the stored digest. The raw arguments and single-use token
	// belong to the MCP caller and are never required by this endpoint.
	res, err := tx.ExecContext(ctx, s.rebind(`UPDATE approval_requests SET state=?,version=version+1,decided_by_principal_id=?,decided_at_ms=?,decision_reason=?
		WHERE org_id=? AND id=? AND version=? AND state='PENDING' AND params_jcs_sha256=? AND expires_at_ms>?`), next, actor, now, req.Reason, org, a.ID, version, req.Hash, now)
	if err != nil {
		mcpInternal(w, r, "failed recording approval decision")
		return
	}
	n, err := res.RowsAffected()
	if err != nil {
		mcpInternal(w, r, "failed checking approval decision")
		return
	}
	if n != 1 {
		writeErrorEnvelope(w, r, 409, "approval_already_decided_or_expired", "conflict_error", "approval changed during the decision", false, nil)
		return
	}
	err = s.appendMCPAuditTx(r, tx, org, actor, "mcp.approval_decided", "approval_request", a.ID, map[string]any{
		"decision": req.Decision, "approval_binding_hash": a.Hash,
	})
	if err != nil || tx.Commit() != nil {
		mcpInternal(w, r, "failed committing approval decision and audit")
		return
	}
	a.State = next
	a.Version++
	a.DecidedBy = &actor
	w.Header().Set("ETag", FormatETag(a.Version))
	writeJSON(w, 200, a)
}

// Only the denial is committed. Neither token material nor tool arguments enter the audit log.
func (s *Server) denyApprovalDecision(w http.ResponseWriter, r *http.Request, tx *sql.Tx, a approvalResource, actor, event string, code approval.ErrorCode, message string) {
	err := s.appendMCPAuditTx(r, tx, a.OrgID, actor, event, "approval_request", a.ID, nil)
	if err != nil || tx.Commit() != nil {
		mcpInternal(w, r, "failed recording blocked approval decision")
		return
	}
	writeErrorEnvelope(w, r, http.StatusForbidden, string(code), "permission_error", message, false, nil)
}
