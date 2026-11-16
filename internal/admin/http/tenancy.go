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

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type organizationDTO struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	MinPluginTrust  string `json:"min_plugin_trust"`
	FourEyesEnabled bool   `json:"four_eyes_enabled"`
	Version         int64  `json:"version"`
	CreatedAt       string `json:"created_at"`
}

type createOrganizationRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type projectDTO struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
}

type createProjectRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type environmentDTO struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type createEnvironmentRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type destructiveConfirmRequest struct {
	ConfirmSlug string `json:"confirm_slug"`
	Reason      string `json:"reason"`
}

type cryptoShredReceiptDTO struct {
	TargetType              string `json:"target_type"`
	TargetID                string `json:"target_id"`
	DestroyedDEKID          string `json:"destroyed_dek_id"`
	ShreddedAt              string `json:"shredded_at"`
	RevokedVirtualKeysCount int    `json:"revoked_virtual_keys_count"`
}

func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	targetOrg := sc.OrgID
	if !sc.IsOperator {
		if !s.authorize(w, r, sc, rbac.ActionOrgRead, rbac.TargetScope{OrgID: targetOrg, IsCollectionList: true}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionOrgRead, rbac.TargetScope{IsCollectionList: true}) {
			return
		}
	}

	scopeCursorOrg := targetOrg
	if sc.IsOperator {
		scopeCursorOrg = "instance"
	}
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "organizations", scopeCursorOrg, sha256HexString("orgs"))
	if !ok {
		return
	}

	var q strings.Builder
	var args []any
	q.WriteString(`
		SELECT id, slug, display_name, min_plugin_trust, require_four_eyes_approval, version, created_at_ms
		FROM organizations
		WHERE 1=1`)
	if !sc.IsOperator {
		q.WriteString(` AND id = ?`)
		args = append(args, targetOrg)
	}
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), q.String(), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing organizations", true, nil)
		return
	}
	defer rows.Close()

	type orgRow struct {
		dto       organizationDTO
		createdMs int64
	}
	var items []orgRow
	for rows.Next() {
		var (
			id, slug, name, trust string
			fourEyes              int
			ver, createdMs        int64
		)
		if scanErr := rows.Scan(&id, &slug, &name, &trust, &fourEyes, &ver, &createdMs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning organization", true, nil)
			return
		}
		items = append(items, orgRow{
			dto: organizationDTO{
				ID:              id,
				Slug:            slug,
				Name:            name,
				MinPluginTrust:  trust,
				FourEyesEnabled: fourEyes == 1,
				Version:         ver,
				CreatedAt:       formatTimeRFC3339Ms(createdMs),
			},
			createdMs: createdMs,
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating organizations", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]organizationDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "organizations", scopeCursorOrg, sha256HexString("orgs"), last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[organizationDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionOrgCreate, rbac.TargetScope{}) {
		return
	}

	var req createOrganizationRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Name) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	org, err := s.storage.Tenancy().CreateOrganization(r.Context(), port.CreateOrgParams{
		Slug:        strings.TrimSpace(req.Slug),
		DisplayName: strings.TrimSpace(req.Name),
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "organization slug already exists", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating organization", true, nil)
		}
		return
	}

	if s.vault != nil {
		if _, derr := s.vault.EnsureOrgDEK(r.Context(), org.ID); derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting organization DEK", true, nil)
			return
		}
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	dto := organizationDTO{
		ID:              org.ID,
		Slug:            org.Slug,
		Name:            org.DisplayName,
		MinPluginTrust:  org.MinPluginTrust,
		FourEyesEnabled: org.RequireFourEyesApproval,
		Version:         org.Version,
		CreatedAt:       formatTimeRFC3339Ms(org.CreatedAtMs),
	}
	w.Header().Set("ETag", FormatETag(org.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleDeleteOrganization(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID := strings.TrimSpace(r.PathValue("orgId"))
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req destructiveConfirmRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionOrgDelete, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}

	org, err := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading organization", true, nil)
		return
	}

	currentDTO := organizationDTO{
		ID:              org.ID,
		Slug:            org.Slug,
		Name:            org.DisplayName,
		MinPluginTrust:  org.MinPluginTrust,
		FourEyesEnabled: org.RequireFourEyesApproval,
		Version:         org.Version,
		CreatedAt:       formatTimeRFC3339Ms(org.CreatedAtMs),
	}
	if org.Version != expectedVer {
		writeOptimisticConflict(w, r, org.Version, currentDTO)
		return
	}
	if strings.TrimSpace(req.ConfirmSlug) != org.Slug {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug must exactly match organization slug", false, nil)
		return
	}

	destroyedDEK := "none"
	if s.vault != nil {
		meta, merr := s.vault.GetOrgKeyMetadata(r.Context(), orgID)
		if merr == nil && meta != nil {
			shredReqID := fmt.Sprintf("admin-org-shred-%s-%d", orgID, s.nowMs())
			if serr := s.vault.ShredOrgDEK(r.Context(), orgID, vault.ShredRequest{
				RequestID:          shredReqID,
				ExpectedGeneration: meta.KeyGeneration,
			}); serr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed crypto-shredding organization DEK", true, nil)
				return
			}
			destroyedDEK = fmt.Sprintf("%s:v%d:g%d", orgID, meta.DEKVersion, meta.KeyGeneration)
		} else {
			s.vault.EvictAllDEKs()
		}
	}

	now := s.nowMs()
	var revokedKeysCount int
	var orgPrincipals []string

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting teardown transaction", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	_ = tx.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM virtual_keys
		WHERE org_id = ? AND state NOT IN ('REVOKED', 'EXPIRED', 'PURGED_METADATA_RETAINED')`,
		orgID,
	).Scan(&revokedKeysCount)

	pRows, perr := tx.QueryContext(r.Context(), `SELECT id FROM principals WHERE org_id = ?`, orgID)
	if perr == nil {
		for pRows.Next() {
			var pid string
			if scanErr := pRows.Scan(&pid); scanErr == nil {
				orgPrincipals = append(orgPrincipals, pid)
			}
		}
		_ = pRows.Close()
	}

	restrictSteps := []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM emergency_kill_switches WHERE org_id = ? OR triggered_by_principal_id IN (SELECT id FROM principals WHERE org_id = ?) OR cleared_by_principal_id IN (SELECT id FROM principals WHERE org_id = ?)`,
			args:  []any{orgID, orgID, orgID},
		},
		{
			query: `DELETE FROM route_targets WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM route_policies WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM model_deployments WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM connection_secret_bindings WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM provider_connections WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM secret_credentials WHERE org_id = ?`,
			args:  []any{orgID},
		},
	}
	for _, st := range restrictSteps {
		if _, derr := tx.ExecContext(r.Context(), st.query, st.args...); derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed tearing down organization resources", true, nil)
			return
		}
	}

	res, err := tx.ExecContext(r.Context(), `DELETE FROM organizations WHERE id = ? AND version = ?`, orgID, expectedVer)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting organization", true, nil)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		writeOptimisticConflict(w, r, org.Version, currentDTO)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'organization', ?, 'SHRED', 1, ?)`,
		orgID, orgID, now,
	); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording shred changelog", true, nil)
		return
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting audit id", true, nil)
		return
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"confirm_slug":     org.Slug,
		"reason":           strings.TrimSpace(req.Reason),
		"destroyed_dek_id": destroyedDEK,
		"revoked_keys":     revokedKeysCount,
	})
	if err := s.txAudit.AppendTx(r.Context(), tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "organization.crypto_shredded",
		TargetType:   "organization",
		TargetID:     orgID,
		RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording shred audit event", true, nil)
		return
	}

	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing organization crypto-shred", true, nil)
		return
	}

	if s.snapshots != nil {
		if s.snapshots.Streams() != nil {
			s.snapshots.Streams().AbortScope(orgID, "")
		}
		_ = s.snapshots.Refresh(r.Context())
	}
	if s.killSwitch != nil && s.killSwitch.Streams() != nil {
		s.killSwitch.Streams().AbortMatching(killswitch.LevelProjectOrOrgFreeze, orgID, orgID)
	}
	for _, pid := range orgPrincipals {
		s.auth.RevokePrincipalSessions(pid)
	}

	writeJSON(w, http.StatusOK, cryptoShredReceiptDTO{
		TargetType:              "ORGANIZATION",
		TargetID:                orgID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(now),
		RevokedVirtualKeysCount: revokedKeysCount,
	})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID := sc.OrgID
	if sc.IsOperator {
		if qOrg := strings.TrimSpace(r.URL.Query().Get("org_id")); qOrg != "" {
			orgID = qOrg
		}
	}
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "organization scope required", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionProjectRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	var allowedProjects []string
	if !actorHasOrgScopeBinding(sc.Actor, orgID) {
		allowedProjects = actorProjectIDs(sc.Actor, orgID)
		if len(allowedProjects) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[projectDTO]{
				Data:    []projectDTO{},
				HasMore: false,
			})
			return
		}
	}

	filterHash := sha256HexString(strings.Join(allowedProjects, ","))
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "projects", orgID, filterHash)
	if !ok {
		return
	}

	var q strings.Builder
	args := []any{orgID}
	q.WriteString(`
		SELECT id, org_id, slug, display_name, version, created_at_ms
		FROM projects
		WHERE org_id = ?`)
	if len(allowedProjects) > 0 {
		placeholders := make([]string, len(allowedProjects))
		for i, pid := range allowedProjects {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		q.WriteString(` AND id IN (` + strings.Join(placeholders, ",") + `)`)
	}
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), q.String(), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing projects", true, nil)
		return
	}
	defer rows.Close()

	type projRow struct {
		dto       projectDTO
		createdMs int64
	}
	var items []projRow
	for rows.Next() {
		var id, pOrg, slug, name string
		var ver, createdMs int64
		if scanErr := rows.Scan(&id, &pOrg, &slug, &name, &ver, &createdMs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning project", true, nil)
			return
		}
		items = append(items, projRow{
			dto: projectDTO{
				ID:        id,
				OrgID:     pOrg,
				Slug:      slug,
				Name:      name,
				Version:   ver,
				CreatedAt: formatTimeRFC3339Ms(createdMs),
			},
			createdMs: createdMs,
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating projects", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]projectDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "projects", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[projectDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createProjectRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if sc.OrgID == "" {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "organization scope required", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: sc.OrgID}) {
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Name) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	prj, err := s.storage.Tenancy().CreateProject(r.Context(), sc.OrgID, port.CreateProjectParams{
		Slug:        strings.TrimSpace(req.Slug),
		DisplayName: strings.TrimSpace(req.Name),
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "project slug already exists in organization", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating project", true, nil)
		}
		return
	}

	if s.vault != nil {
		if _, derr := s.vault.EnsureProjectDEK(r.Context(), sc.OrgID, prj.ID); derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting project DEK", true, nil)
			return
		}
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	dto := projectDTO{
		ID:        prj.ID,
		OrgID:     prj.OrgID,
		Slug:      prj.Slug,
		Name:      prj.DisplayName,
		Version:   prj.Version,
		CreatedAt: formatTimeRFC3339Ms(prj.CreatedAtMs),
	}
	w.Header().Set("ETag", FormatETag(prj.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req destructiveConfirmRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	prj, err := s.storage.Tenancy().GetProject(r.Context(), sc.OrgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: sc.OrgID, ProjectID: projectID}) {
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}

	currentDTO := projectDTO{
		ID:        prj.ID,
		OrgID:     prj.OrgID,
		Slug:      prj.Slug,
		Name:      prj.DisplayName,
		Version:   prj.Version,
		CreatedAt: formatTimeRFC3339Ms(prj.CreatedAtMs),
	}
	if prj.Version != expectedVer {
		writeOptimisticConflict(w, r, prj.Version, currentDTO)
		return
	}
	if strings.TrimSpace(req.ConfirmSlug) != prj.Slug {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug must exactly match project slug", false, nil)
		return
	}

	destroyedDEK := "none"
	if s.vault != nil {
		meta, merr := s.vault.GetProjectKeyMetadata(r.Context(), sc.OrgID, projectID)
		if merr == nil && meta != nil {
			shredReqID := fmt.Sprintf("admin-proj-shred-%s-%d", projectID, s.nowMs())
			if serr := s.vault.ShredProjectDEK(r.Context(), sc.OrgID, projectID, vault.ShredRequest{
				RequestID:          shredReqID,
				ExpectedGeneration: meta.KeyGeneration,
			}); serr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed crypto-shredding project DEK", true, nil)
				return
			}
			destroyedDEK = fmt.Sprintf("%s:%s:v%d:g%d", sc.OrgID, projectID, meta.DEKVersion, meta.KeyGeneration)
		} else {
			s.vault.EvictAllDEKs()
		}
	}

	now := s.nowMs()
	var revokedKeysCount int

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting project teardown transaction", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	_ = tx.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM virtual_keys
		WHERE org_id = ? AND project_id = ? AND state NOT IN ('REVOKED', 'EXPIRED', 'PURGED_METADATA_RETAINED')`,
		sc.OrgID, projectID,
	).Scan(&revokedKeysCount)

	if err := cleanupProjectRestrictedFKsTx(r.Context(), tx, sc.OrgID, projectID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed tearing down project dependencies", true, nil)
		return
	}

	res, err := tx.ExecContext(r.Context(), `DELETE FROM projects WHERE org_id = ? AND id = ? AND version = ?`, sc.OrgID, projectID, expectedVer)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting project", true, nil)
		return
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		writeOptimisticConflict(w, r, prj.Version, currentDTO)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'project', ?, 'SHRED', 1, ?)`,
		sc.OrgID, projectID, now,
	); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording shred changelog", true, nil)
		return
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting audit id", true, nil)
		return
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"confirm_slug":     prj.Slug,
		"reason":           strings.TrimSpace(req.Reason),
		"destroyed_dek_id": destroyedDEK,
		"revoked_keys":     revokedKeysCount,
	})
	if err := s.txAudit.AppendTx(r.Context(), tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &sc.OrgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "project.crypto_shredded",
		TargetType:   "project",
		TargetID:     projectID,
		RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording shred audit event", true, nil)
		return
	}

	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing project crypto-shred", true, nil)
		return
	}

	if s.snapshots != nil {
		if s.snapshots.Streams() != nil {
			s.snapshots.Streams().AbortScope(sc.OrgID, projectID)
		}
		_ = s.snapshots.Refresh(r.Context())
	}
	if s.killSwitch != nil && s.killSwitch.Streams() != nil {
		s.killSwitch.Streams().AbortMatching(killswitch.LevelProjectOrOrgFreeze, sc.OrgID, projectID)
	}

	writeJSON(w, http.StatusOK, cryptoShredReceiptDTO{
		TargetType:              "PROJECT",
		TargetID:                projectID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(now),
		RevokedVirtualKeysCount: revokedKeysCount,
	})
}

func cleanupProjectRestrictedFKsTx(ctx context.Context, tx *sql.Tx, orgID, projectID string) error {
	steps := []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM route_targets WHERE org_id = ? AND (route_id IN (SELECT id FROM route_policies WHERE org_id = ? AND project_id = ?) OR deployment_id IN (SELECT id FROM model_deployments WHERE org_id = ? AND connection_id IN (SELECT id FROM provider_connections WHERE org_id = ? AND owner_project_id = ?)))`,
			args:  []any{orgID, orgID, projectID, orgID, orgID, projectID},
		},
		{
			query: `DELETE FROM route_policies WHERE org_id = ? AND project_id = ?`,
			args:  []any{orgID, projectID},
		},
		{
			query: `DELETE FROM model_deployments WHERE org_id = ? AND connection_id IN (SELECT id FROM provider_connections WHERE org_id = ? AND owner_project_id = ?)`,
			args:  []any{orgID, orgID, projectID},
		},
		{
			query: `DELETE FROM connection_secret_bindings WHERE org_id = ? AND (connection_id IN (SELECT id FROM provider_connections WHERE org_id = ? AND owner_project_id = ?) OR credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, orgID, projectID, orgID, projectID},
		},
		{
			query: `UPDATE provider_connections SET credential_id = NULL WHERE org_id = ? AND owner_project_id IS NULL AND credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?)`,
			args:  []any{orgID, orgID, projectID},
		},
		{
			query: `DELETE FROM provider_connections WHERE org_id = ? AND owner_project_id = ?`,
			args:  []any{orgID, projectID},
		},
		{
			query: `DELETE FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?`,
			args:  []any{orgID, projectID},
		},
		{
			query: `DELETE FROM emergency_kill_switches WHERE org_id = ? AND target_id = ?`,
			args:  []any{orgID, projectID},
		},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleListProjectEnvironments(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	prj, err := s.storage.Tenancy().GetProject(r.Context(), sc.OrgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionProjectRead, rbac.TargetScope{OrgID: sc.OrgID, ProjectID: prj.ID}) {
		return
	}

	envs, err := s.storage.Tenancy().ListEnvironments(r.Context(), sc.OrgID, prj.ID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing environments", true, nil)
		return
	}

	out := make([]environmentDTO, len(envs))
	for i, e := range envs {
		out[i] = environmentDTO{
			ID:        e.ID,
			OrgID:     e.OrgID,
			ProjectID: e.ProjectID,
			Slug:      e.Slug,
			Name:      e.Slug,
			CreatedAt: formatTimeRFC3339Ms(e.CreatedAtMs),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (s *Server) handleCreateProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	var req createEnvironmentRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	prj, err := s.storage.Tenancy().GetProject(r.Context(), sc.OrgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionEnvironmentWrite, rbac.TargetScope{OrgID: sc.OrgID, ProjectID: prj.ID}) {
		return
	}

	slug := strings.TrimSpace(req.Slug)
	name := strings.TrimSpace(req.Name)
	if slug == "" || name == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	env, err := s.storage.Tenancy().CreateEnvironment(r.Context(), sc.OrgID, prj.ID, port.CreateEnvironmentParams{
		Slug: slug,
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "environment slug already exists in project", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating environment", true, nil)
		}
		return
	}

	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	dto := environmentDTO{
		ID:        env.ID,
		OrgID:     env.OrgID,
		ProjectID: env.ProjectID,
		Slug:      env.Slug,
		Name:      name,
		CreatedAt: formatTimeRFC3339Ms(env.CreatedAtMs),
	}
	w.Header().Set("ETag", FormatETag(env.Version))
	writeJSON(w, http.StatusCreated, dto)
}
