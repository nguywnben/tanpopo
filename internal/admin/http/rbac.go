// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type memberSummaryDTO struct {
	ID         string   `json:"id"`
	Email      string   `json:"email"`
	Kind       string   `json:"kind"`
	Roles      []string `json:"roles"`
	ProjectIDs []string `json:"project_ids,omitempty"`
	Version    int64    `json:"version"`
	CreatedAt  string   `json:"created_at"`
}

type createMemberRequest struct {
	Email      string   `json:"email"`
	Kind       string   `json:"kind"`
	Roles      []string `json:"roles"`
	ProjectIDs []string `json:"project_ids"`
}

type updateMemberRolesRequest struct {
	Roles      []string `json:"roles"`
	ProjectIDs []string `json:"project_ids"`
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	projFilter := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projFilter != "" {
		if !s.authorize(w, r, sc, rbac.ActionMemberRead, rbac.TargetScope{OrgID: orgID, ProjectID: projFilter}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionMemberRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("members|" + projFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "members", orgID, filterHash)
	if !ok {
		return
	}

	var q strings.Builder
	args := []any{orgID}
	q.WriteString(`
		SELECT p.id, p.email_or_sub, p.kind, p.version, p.created_at_ms
		FROM principals p
		WHERE p.org_id = ?`)
	if projFilter != "" {
		q.WriteString(` AND EXISTS (SELECT 1 FROM role_bindings rb WHERE rb.principal_id = p.id AND rb.org_id = p.org_id AND rb.scope_type = 'PROJECT' AND rb.scope_id = ?)`)
		args = append(args, projFilter)
	}
	if cur != nil {
		q.WriteString(` AND (p.created_at_ms < ? OR (p.created_at_ms = ? AND p.id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY p.created_at_ms DESC, p.id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), q.String(), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing members", true, nil)
		return
	}
	defer rows.Close()

	type memberRow struct {
		dto       memberSummaryDTO
		createdMs int64
	}
	var items []memberRow
	for rows.Next() {
		var id, email, kind string
		var ver, createdMs int64
		if scanErr := rows.Scan(&id, &email, &kind, &ver, &createdMs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning member", true, nil)
			return
		}
		items = append(items, memberRow{
			dto: memberSummaryDTO{
				ID:        id,
				Email:     email,
				Kind:      toOpenAPIMemberKind(kind),
				Roles:     []string{},
				Version:   ver,
				CreatedAt: formatTimeRFC3339Ms(createdMs),
			},
			createdMs: createdMs,
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating members", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	data := make([]memberSummaryDTO, len(items))
	if len(items) > 0 {
		principalIDs := make([]string, len(items))
		for i := range items {
			principalIDs[i] = items[i].dto.ID
		}
		bindingsMap, berr := s.loadMemberBindingDetailsBatch(r.Context(), orgID, principalIDs)
		if berr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading member roles", true, nil)
			return
		}
		for i := range items {
			if details, ok := bindingsMap[items[i].dto.ID]; ok {
				items[i].dto.Roles = details.roles
				items[i].dto.ProjectIDs = details.projs
			}
			data[i] = items[i].dto
		}
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "members", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[memberSummaryDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createMemberRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "valid email is required", false, nil)
		return
	}
	if len(req.Roles) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "at least one role is required", false, nil)
		return
	}

	dbKind := auth.PrincipalKindHumanLocal
	switch strings.TrimSpace(req.Kind) {
	case "", "HUMAN":
		dbKind = auth.PrincipalKindHumanLocal
	case "SERVICE_ACCOUNT":
		dbKind = auth.PrincipalKindAdminServiceToken
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "kind must be HUMAN or SERVICE_ACCOUNT", false, nil)
		return
	}

	projIDs := normalizeUniqueIDs(req.ProjectIDs)
	if len(projIDs) > 0 {
		for _, pid := range projIDs {
			if !s.authorize(w, r, sc, rbac.ActionMemberWrite, rbac.TargetScope{OrgID: orgID, ProjectID: pid}) {
				return
			}
		}
		for _, pid := range projIDs {
			var pExists int
			err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, pid).Scan(&pExists)
			if errors.Is(err, sql.ErrNoRows) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
				return
			}
			if err != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating project_ids", true, nil)
				return
			}
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionMemberWrite, rbac.TargetScope{OrgID: orgID}) {
			return
		}
	}

	roles, projIDsForResp, err := normalizeAndCheckGrantRoles(sc, orgID, req.Roles, projIDs)
	if err != nil {
		if errors.Is(err, rbac.ErrPermissionDenied) {
			writeErrorEnvelope(w, r, http.StatusForbidden, "role_escalation_denied", "permission_error", err.Error(), false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}
	if !s.requireActiveOrg(w, r, orgID) {
		return
	}

	prcID, err := idgen.New(idgen.PrefixPrincipal)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting principal id", true, nil)
		return
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting member creation tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO principals (
			id, org_id, kind, email_or_sub, password_argon2id, oidc_issuer,
			status, version, last_login_at_ms, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, NULL, NULL, 'ACTIVE', 1, NULL, ?, ?)`,
		prcID, orgID, dbKind, email, now, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "member with email already exists in organization", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating member", true, nil)
		return
	}

	if err := insertRoleBindingsTx(r.Context(), tx, orgID, prcID, roles, projIDsForResp, now); err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting audit id", true, nil)
		return
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"email":       email,
		"kind":        toOpenAPIMemberKind(dbKind),
		"roles":       roles,
		"project_ids": projIDsForResp,
	})
	if err := s.txAudit.AppendTx(r.Context(), tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "member.created",
		TargetType:   "principal",
		TargetID:     prcID,
		RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording member audit event", true, nil)
		return
	}

	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing member", true, nil)
		return
	}

	dto := memberSummaryDTO{
		ID:         prcID,
		Email:      email,
		Kind:       toOpenAPIMemberKind(dbKind),
		Roles:      roles,
		ProjectIDs: projIDsForResp,
		Version:    1,
		CreatedAt:  formatTimeRFC3339Ms(now),
	}
	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleUpdateMemberRoles(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	memberID := strings.TrimSpace(r.PathValue("memberId"))
	if memberID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "member not found", false, nil)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req updateMemberRolesRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	var (
		email, kind string
		version     int64
		createdMs   int64
	)
	err := s.db.QueryRowContext(r.Context(), `
		SELECT email_or_sub, kind, version, created_at_ms
		FROM principals WHERE id = ? AND org_id = ?`,
		memberID, orgID,
	).Scan(&email, &kind, &version, &createdMs)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "member not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading member", true, nil)
		return
	}

	curRoles, curProjs, curBindings, berr := s.loadMemberBindingDetails(r.Context(), orgID, memberID)
	if berr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading current member roles", true, nil)
		return
	}
	currentDTO := memberSummaryDTO{
		ID:         memberID,
		Email:      email,
		Kind:       toOpenAPIMemberKind(kind),
		Roles:      curRoles,
		ProjectIDs: curProjs,
		Version:    version,
		CreatedAt:  formatTimeRFC3339Ms(createdMs),
	}

	// Anti-self-escalation: never allow a principal to elevate their own roles or add scopes.
	if memberID == sc.Actor.PrincipalID {
		s.emitSecurityAudit(r.Context(), sc, "security.self_role_escalation_blocked", map[string]any{
			"member_id":       memberID,
			"requested_roles": req.Roles,
		})
		writeErrorEnvelope(w, r, http.StatusForbidden, "role_escalation_denied", "permission_error", "administrators cannot escalate or alter their own RBAC roles", false, nil)
		return
	}

	// Enforce authorization over ALL existing member bindings BEFORE version conflict checks
	// so unauthorized callers never leak current_resource via 409 or tamper across scopes.
	if !actorIsOrgOwnerOrOperator(sc.Actor, orgID) {
		for _, b := range curBindings {
			if b.ScopeType != rbac.ScopeProject ||
				b.RoleName == rbac.RoleOrgOwner ||
				b.RoleName == rbac.RoleSecurityReviewer ||
				b.RoleName == rbac.RoleFinOpsViewer ||
				b.RoleName == rbac.RoleOperator {
				writeErrorEnvelope(w, r, http.StatusForbidden, "role_escalation_denied", "permission_error", "only ORG_OWNER or OPERATOR can modify organization-level role holders", false, nil)
				return
			}
			if aErr := rbac.Authorize(sc.Actor, rbac.ActionMemberWrite, rbac.TargetScope{OrgID: orgID, ProjectID: b.ScopeID}); aErr != nil {
				writeErrorEnvelope(w, r, http.StatusForbidden, "role_escalation_denied", "permission_error", "cannot modify member bindings outside caller project scope", false, nil)
				return
			}
		}
	}

	projIDs := normalizeUniqueIDs(req.ProjectIDs)
	if len(projIDs) > 0 {
		for _, pid := range projIDs {
			if !s.authorize(w, r, sc, rbac.ActionMemberWrite, rbac.TargetScope{OrgID: orgID, ProjectID: pid}) {
				return
			}
		}
		for _, pid := range projIDs {
			var pExists int
			pErr := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, pid).Scan(&pExists)
			if errors.Is(pErr, sql.ErrNoRows) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
				return
			}
			if pErr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying project_ids", true, nil)
				return
			}
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionMemberWrite, rbac.TargetScope{OrgID: orgID}) {
			return
		}
	}

	nextRoles, projIDsForResp, err := normalizeAndCheckGrantRoles(sc, orgID, req.Roles, projIDs)
	if err != nil {
		if errors.Is(err, rbac.ErrPermissionDenied) {
			writeErrorEnvelope(w, r, http.StatusForbidden, "role_escalation_denied", "permission_error", err.Error(), false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}
	if !s.requireActiveOrg(w, r, orgID) {
		return
	}

	if version != expectedVer {
		writeOptimisticConflict(w, r, version, currentDTO)
		return
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed beginning role update tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM role_bindings WHERE principal_id = ? AND org_id = ?`, memberID, orgID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed clearing previous bindings", true, nil)
		return
	}
	if err := insertRoleBindingsTx(r.Context(), tx, orgID, memberID, nextRoles, projIDsForResp, now); err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	res, err := tx.ExecContext(r.Context(), `
		UPDATE principals SET version = version + 1, updated_at_ms = ?
		WHERE id = ? AND org_id = ? AND version = ?`,
		now, memberID, orgID, expectedVer,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed bumping member version", true, nil)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		_ = tx.Rollback()
		var freshEmail, freshKind string
		var freshVer, freshCreatedMs int64
		rerr := s.db.QueryRowContext(r.Context(), `
			SELECT email_or_sub, kind, version, created_at_ms
			FROM principals WHERE id = ? AND org_id = ?`,
			memberID, orgID,
		).Scan(&freshEmail, &freshKind, &freshVer, &freshCreatedMs)
		if errors.Is(rerr, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "member not found", false, nil)
			return
		}
		if rerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading member after conflict", true, nil)
			return
		}
		freshRoles, freshProjs, _, berr := s.loadMemberBindingDetails(r.Context(), orgID, memberID)
		if berr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading member roles after conflict", true, nil)
			return
		}
		freshDTO := memberSummaryDTO{
			ID:         memberID,
			Email:      freshEmail,
			Kind:       toOpenAPIMemberKind(freshKind),
			Roles:      freshRoles,
			ProjectIDs: freshProjs,
			Version:    freshVer,
			CreatedAt:  formatTimeRFC3339Ms(freshCreatedMs),
		}
		writeOptimisticConflict(w, r, freshVer, freshDTO)
		return
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting audit id", true, nil)
		return
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"previous_roles": curRoles,
		"next_roles":     nextRoles,
		"project_ids":    projIDsForResp,
	})
	if err := s.txAudit.AppendTx(r.Context(), tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "member.roles_updated",
		TargetType:   "principal",
		TargetID:     memberID,
		RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording role update audit", true, nil)
		return
	}

	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing member role update", true, nil)
		return
	}

	nextVer := expectedVer + 1
	updated := memberSummaryDTO{
		ID:         memberID,
		Email:      email,
		Kind:       toOpenAPIMemberKind(kind),
		Roles:      nextRoles,
		ProjectIDs: projIDsForResp,
		Version:    nextVer,
		CreatedAt:  formatTimeRFC3339Ms(createdMs),
	}
	w.Header().Set("ETag", FormatETag(nextVer))
	writeJSON(w, http.StatusOK, updated)
}

type memberBindingRow struct {
	RoleName  string
	ScopeType string
	ScopeID   string
}

type memberBindingSummary struct {
	roles []string
	projs []string
}

func (s *Server) loadMemberBindingDetailsBatch(ctx context.Context, orgID string, principalIDs []string) (map[string]memberBindingSummary, error) {
	out := make(map[string]memberBindingSummary, len(principalIDs))
	if len(principalIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(principalIDs))
	args := make([]any, 0, len(principalIDs)+1)
	args = append(args, orgID)
	for i, pid := range principalIDs {
		placeholders[i] = "?"
		args = append(args, pid)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT principal_id, role_name, scope_type, scope_id
		FROM role_bindings
		WHERE org_id = ? AND principal_id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY created_at_ms ASC, id ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seenRoles := make(map[string]map[string]struct{}, len(principalIDs))
	seenProjs := make(map[string]map[string]struct{}, len(principalIDs))
	for rows.Next() {
		var pid, roleName, scopeType, scopeID string
		if scanErr := rows.Scan(&pid, &roleName, &scopeType, &scopeID); scanErr != nil {
			return nil, scanErr
		}
		cur := out[pid]
		if cur.roles == nil {
			cur.roles = []string{}
		}
		if seenRoles[pid] == nil {
			seenRoles[pid] = make(map[string]struct{})
		}
		if _, ok := seenRoles[pid][roleName]; !ok {
			seenRoles[pid][roleName] = struct{}{}
			cur.roles = append(cur.roles, roleName)
		}
		if scopeType == rbac.ScopeProject && scopeID != "" {
			if seenProjs[pid] == nil {
				seenProjs[pid] = make(map[string]struct{})
			}
			if _, ok := seenProjs[pid][scopeID]; !ok {
				seenProjs[pid][scopeID] = struct{}{}
				cur.projs = append(cur.projs, scopeID)
			}
		}
		out[pid] = cur
	}
	return out, rows.Err()
}

func (s *Server) loadMemberBindingDetails(ctx context.Context, orgID, principalID string) ([]string, []string, []memberBindingRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT role_name, scope_type, scope_id
		FROM role_bindings WHERE org_id = ? AND principal_id = ? ORDER BY created_at_ms ASC, id ASC`,
		orgID, principalID,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	seenRoles := make(map[string]struct{})
	seenProjs := make(map[string]struct{})
	roles := []string{}
	var projs []string
	var bindings []memberBindingRow
	for rows.Next() {
		var row memberBindingRow
		if scanErr := rows.Scan(&row.RoleName, &row.ScopeType, &row.ScopeID); scanErr != nil {
			return nil, nil, nil, scanErr
		}
		bindings = append(bindings, row)
		if _, ok := seenRoles[row.RoleName]; !ok {
			seenRoles[row.RoleName] = struct{}{}
			roles = append(roles, row.RoleName)
		}
		if row.ScopeType == rbac.ScopeProject && row.ScopeID != "" {
			if _, ok := seenProjs[row.ScopeID]; !ok {
				seenProjs[row.ScopeID] = struct{}{}
				projs = append(projs, row.ScopeID)
			}
		}
	}
	return roles, projs, bindings, rows.Err()
}

func normalizeAndCheckGrantRoles(sc *authSessionContext, orgID string, rawRoles []string, projIDs []string) ([]string, []string, error) {
	if len(rawRoles) == 0 {
		return nil, nil, fmt.Errorf("%w: roles list cannot be empty", port.ErrInvalidInput)
	}
	isOrgOwnerOrOp := actorIsOrgOwnerOrOperator(sc.Actor, orgID)
	seen := make(map[string]struct{}, len(rawRoles))
	out := make([]string, 0, len(rawRoles))
	hasProjectBindableRole := false

	for _, r := range rawRoles {
		role := strings.TrimSpace(r)
		if !rbac.IsValidRole(role) {
			return nil, nil, fmt.Errorf("%w: unknown RBAC role %q", port.ErrInvalidInput, role)
		}
		if role == rbac.RoleOperator {
			return nil, nil, fmt.Errorf("%w: OPERATOR role cannot be bound inside an organization member", rbac.ErrPermissionDenied)
		}
		if role == rbac.RoleTeamAdmin || role == rbac.RoleDeveloper {
			hasProjectBindableRole = true
		}
		if !isOrgOwnerOrOp {
			if role != rbac.RoleTeamAdmin && role != rbac.RoleDeveloper {
				return nil, nil, fmt.Errorf("%w: only ORG_OWNER or OPERATOR can grant organization role %s", rbac.ErrPermissionDenied, role)
			}
			if len(projIDs) == 0 {
				return nil, nil, fmt.Errorf("%w: project-scoped admin must specify project_ids and cannot grant organization-scoped bindings", rbac.ErrPermissionDenied)
			}
		}
		if _, ok := seen[role]; !ok {
			seen[role] = struct{}{}
			out = append(out, role)
		}
	}
	if len(projIDs) > 0 && !hasProjectBindableRole {
		return nil, nil, fmt.Errorf("%w: project_ids can only be specified with TEAM_ADMIN or DEVELOPER roles", port.ErrInvalidInput)
	}
	var boundProjIDs []string
	if hasProjectBindableRole && len(projIDs) > 0 {
		boundProjIDs = projIDs
	}
	return out, boundProjIDs, nil
}

func insertRoleBindingsTx(ctx context.Context, tx *sql.Tx, orgID, principalID string, roles, projIDs []string, now int64) error {
	for _, role := range roles {
		switch role {
		case rbac.RoleOrgOwner, rbac.RoleSecurityReviewer, rbac.RoleFinOpsViewer:
			rbID, err := mintRoleBindingID()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
				VALUES (?, ?, ?, 'ORG', ?, ?, 1, ?)`,
				rbID, orgID, principalID, orgID, role, now,
			); err != nil {
				return err
			}
		case rbac.RoleTeamAdmin, rbac.RoleDeveloper:
			if len(projIDs) > 0 {
				for _, pid := range projIDs {
					rbID, err := mintRoleBindingID()
					if err != nil {
						return err
					}
					if _, err := tx.ExecContext(ctx, `
						INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
						VALUES (?, ?, ?, 'PROJECT', ?, ?, 1, ?)`,
						rbID, orgID, principalID, pid, role, now,
					); err != nil {
						return err
					}
				}
			} else {
				rbID, err := mintRoleBindingID()
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO role_bindings (id, org_id, principal_id, scope_type, scope_id, role_name, version, created_at_ms)
					VALUES (?, ?, ?, 'ORG', ?, ?, 1, ?)`,
					rbID, orgID, principalID, orgID, role, now,
				); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func mintRoleBindingID() (string, error) {
	raw, err := idgen.New(idgen.PrefixPrincipal)
	if err != nil {
		return "", err
	}
	return "rbd_" + raw[len(idgen.PrefixPrincipal)+1:], nil
}

func normalizeUniqueIDs(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func toOpenAPIMemberKind(dbKind string) string {
	switch dbKind {
	case auth.PrincipalKindAdminServiceToken:
		return "SERVICE_ACCOUNT"
	default:
		return "HUMAN"
	}
}
