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
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
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

	currentDTO := toOrganizationDTO(org)
	if org.Version != expectedVer {
		writeOptimisticConflict(w, r, org.Version, currentDTO)
		return
	}
	if strings.TrimSpace(req.ConfirmSlug) != org.Slug {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug must exactly match organization slug", false, nil)
		return
	}

	now := s.nowMs()
	var revokedKeysCount int
	var activeKeyIDs []string
	var orgPrincipals []string

	// Phase 1 TX: Atomically fence organization version BEFORE destroying any DEK,
	// collect keys/principals to invalidate, and tear down RESTRICT FKs leaving DEK metadata intact.
	tx1, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting teardown fence transaction", true, nil)
		return
	}
	defer func() { _ = tx1.Rollback() }()

	resFence, err := tx1.ExecContext(r.Context(), `
		UPDATE organizations SET status = 'DELETING', version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`,
		now, orgID, expectedVer,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed fencing organization version", true, nil)
		return
	}
	if affected, _ := resFence.RowsAffected(); affected == 0 {
		_ = tx1.Rollback()
		if latest, lerr := s.storage.Tenancy().GetOrganization(r.Context(), orgID); lerr == nil {
			writeOptimisticConflict(w, r, latest.Version, toOrganizationDTO(latest))
			return
		} else if errors.Is(lerr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
			return
		}
		writeOptimisticConflict(w, r, org.Version, currentDTO)
		return
	}
	fencedVer := expectedVer + 1

	kRows, kerr := tx1.QueryContext(r.Context(), `
		SELECT id FROM virtual_keys
		WHERE org_id = ? AND state NOT IN ('REVOKED', 'EXPIRED', 'PURGED_METADATA_RETAINED')`,
		orgID,
	)
	if kerr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed enumerating organization virtual keys", true, nil)
		return
	}
	for kRows.Next() {
		var kid string
		if scanErr := kRows.Scan(&kid); scanErr != nil {
			_ = kRows.Close()
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning virtual key id", true, nil)
			return
		}
		activeKeyIDs = append(activeKeyIDs, kid)
	}
	if rowsErr := kRows.Err(); rowsErr != nil {
		_ = kRows.Close()
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating organization virtual keys", true, nil)
		return
	}
	_ = kRows.Close()
	revokedKeysCount = len(activeKeyIDs)

	pRows, perr := tx1.QueryContext(r.Context(), `SELECT id FROM principals WHERE org_id = ?`, orgID)
	if perr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed enumerating organization principals", true, nil)
		return
	}
	for pRows.Next() {
		var pid string
		if scanErr := pRows.Scan(&pid); scanErr != nil {
			_ = pRows.Close()
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning principal id", true, nil)
			return
		}
		orgPrincipals = append(orgPrincipals, pid)
	}
	if rowsErr := pRows.Err(); rowsErr != nil {
		_ = pRows.Close()
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating organization principals", true, nil)
		return
	}
	_ = pRows.Close()

	if err := cleanupOrgRestrictedFKsTx(r.Context(), tx1, orgID); err != nil {
		_ = tx1.Rollback()
		if errors.Is(err, port.ErrResourceInUse) {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_in_use", "conflict_error", "organization cannot be deleted while its principals are referenced by global kill switches or change requests", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed tearing down organization resources", true, nil)
		return
	}

	if err := tx1.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing organization teardown fence", true, nil)
		return
	}

	// Phase 2: Crypto-shred organization DEK after winning the OCC version fence.
	destroyedDEK := "none"
	if s.vault != nil {
		meta, merr := s.vault.GetOrgKeyMetadata(r.Context(), orgID)
		if merr != nil && !errors.Is(merr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading organization encryption key metadata", true, nil)
			return
		}
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

	// Phase 3 TX: Remove org-scoped kill switches (kept active until crypto-shred succeeds),
	// delete fenced organization row, emit SHRED changelog, and record audit.
	tx2, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting organization delete transaction", true, nil)
		return
	}
	defer func() { _ = tx2.Rollback() }()

	if _, err := tx2.ExecContext(r.Context(), `DELETE FROM emergency_kill_switches WHERE org_id = ?`, orgID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed clearing organization kill switches", true, nil)
		return
	}

	resDel, err := tx2.ExecContext(r.Context(), `DELETE FROM organizations WHERE id = ? AND version = ?`, orgID, fencedVer)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting organization", true, nil)
		return
	}
	if affected, _ := resDel.RowsAffected(); affected == 0 {
		writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "concurrent organization teardown conflict", true, nil)
		return
	}

	resEpoch, err := tx2.ExecContext(r.Context(), `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'organization', ?, 'SHRED', 1, ?)`,
		orgID, orgID, now,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording shred changelog", true, nil)
		return
	}
	shredEpoch, _ := resEpoch.LastInsertId()

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
	if err := s.txAudit.AppendTx(r.Context(), tx2, port.AuditEvent{
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

	if err := tx2.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing organization crypto-shred", true, nil)
		return
	}

	if s.killSwitch != nil {
		_ = s.killSwitch.RefreshFromDB(r.Context())
	}

	if s.snapshots != nil {
		if s.snapshots.Streams() != nil {
			s.snapshots.Streams().AbortScope(orgID, "")
		}
		if refErr := s.snapshots.Refresh(r.Context()); refErr != nil {
			for _, kid := range activeKeyIDs {
				s.snapshots.FenceKeyRevocation(kid, "EMERGENCY_SECURITY", now, shredEpoch)
			}
			w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			s.emitSecurityAudit(r.Context(), sc, "organization.snapshot_sync_pending", map[string]any{
				"org_id":       orgID,
				"shred_epoch":  shredEpoch,
				"fenced_keys":  len(activeKeyIDs),
				"error":        refErr.Error(),
			})
		}
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
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
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
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.requireActiveOrg(w, r, orgID) {
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Name) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	prj, err := s.storage.Tenancy().CreateProject(r.Context(), orgID, port.CreateProjectParams{
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
		if _, derr := s.vault.EnsureProjectDEK(r.Context(), orgID, prj.ID); derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting project DEK", true, nil)
			return
		}
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	dto := toProjectDTO(prj)
	w.Header().Set("ETag", FormatETag(prj.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	if projectID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionProjectWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req destructiveConfirmRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	prj, err := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}

	currentDTO := toProjectDTO(prj)
	if prj.Version != expectedVer {
		writeOptimisticConflict(w, r, prj.Version, currentDTO)
		return
	}
	if strings.TrimSpace(req.ConfirmSlug) != prj.Slug {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug must exactly match project slug", false, nil)
		return
	}

	now := s.nowMs()
	var revokedKeysCount int
	var activeKeyIDs []string
	var projectKSTargets []string

	// Phase 1 TX: Atomically fence project version BEFORE destroying any DEK,
	// collect virtual keys and kill-switch target IDs, and tear down RESTRICT FKs leaving DEK metadata intact.
	tx1, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting project teardown fence transaction", true, nil)
		return
	}
	defer func() { _ = tx1.Rollback() }()

	resFence, err := tx1.ExecContext(r.Context(), `
		UPDATE projects SET status = 'DELETING', version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		now, orgID, projectID, expectedVer,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed fencing project version", true, nil)
		return
	}
	if affected, _ := resFence.RowsAffected(); affected == 0 {
		_ = tx1.Rollback()
		if latest, lerr := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID); lerr == nil {
			writeOptimisticConflict(w, r, latest.Version, toProjectDTO(latest))
			return
		} else if errors.Is(lerr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
			return
		}
		writeOptimisticConflict(w, r, prj.Version, currentDTO)
		return
	}
	fencedVer := expectedVer + 1

	projectKSTargets = append(projectKSTargets, projectID)
	kRows, kerr := tx1.QueryContext(r.Context(), `
		SELECT id, state FROM virtual_keys
		WHERE org_id = ? AND project_id = ?`,
		orgID, projectID,
	)
	if kerr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed enumerating project virtual keys", true, nil)
		return
	}
	for kRows.Next() {
		var kid, kstate string
		if scanErr := kRows.Scan(&kid, &kstate); scanErr != nil {
			_ = kRows.Close()
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning virtual key id", true, nil)
			return
		}
		projectKSTargets = append(projectKSTargets, kid)
		if kstate != "REVOKED" && kstate != "EXPIRED" && kstate != "PURGED_METADATA_RETAINED" {
			activeKeyIDs = append(activeKeyIDs, kid)
		}
	}
	if rowsErr := kRows.Err(); rowsErr != nil {
		_ = kRows.Close()
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating project virtual keys", true, nil)
		return
	}
	_ = kRows.Close()
	revokedKeysCount = len(activeKeyIDs)

	cRows, cerr := tx1.QueryContext(r.Context(), `
		SELECT id FROM provider_connections
		WHERE org_id = ? AND owner_project_id = ?`,
		orgID, projectID,
	)
	if cerr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed enumerating project connections", true, nil)
		return
	}
	for cRows.Next() {
		var cid string
		if scanErr := cRows.Scan(&cid); scanErr != nil {
			_ = cRows.Close()
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning connection id", true, nil)
			return
		}
		projectKSTargets = append(projectKSTargets, cid)
	}
	if rowsErr := cRows.Err(); rowsErr != nil {
		_ = cRows.Close()
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating project connections", true, nil)
		return
	}
	_ = cRows.Close()

	if err := cleanupProjectRestrictedFKsTx(r.Context(), tx1, orgID, projectID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed tearing down project dependencies", true, nil)
		return
	}

	if err := tx1.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing project teardown fence", true, nil)
		return
	}

	// Phase 2: Crypto-shred project DEK after winning the OCC version fence.
	destroyedDEK := "none"
	if s.vault != nil {
		meta, merr := s.vault.GetProjectKeyMetadata(r.Context(), orgID, projectID)
		if merr != nil && !errors.Is(merr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project encryption key metadata", true, nil)
			return
		}
		if merr == nil && meta != nil {
			shredReqID := fmt.Sprintf("admin-proj-shred-%s-%d", projectID, s.nowMs())
			if serr := s.vault.ShredProjectDEK(r.Context(), orgID, projectID, vault.ShredRequest{
				RequestID:          shredReqID,
				ExpectedGeneration: meta.KeyGeneration,
			}); serr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed crypto-shredding project DEK", true, nil)
				return
			}
			destroyedDEK = fmt.Sprintf("%s:%s:v%d:g%d", orgID, projectID, meta.DEKVersion, meta.KeyGeneration)
		} else {
			s.vault.EvictAllDEKs()
		}
	}

	// Phase 3 TX: Remove project-scoped kill switches (kept active until crypto-shred succeeds),
	// delete fenced project row, emit SHRED changelog, and record audit.
	tx2, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting project delete transaction", true, nil)
		return
	}
	defer func() { _ = tx2.Rollback() }()

	if len(projectKSTargets) > 0 {
		placeholders := make([]string, len(projectKSTargets))
		args := make([]any, 0, len(projectKSTargets)+1)
		args = append(args, orgID)
		for i, tid := range projectKSTargets {
			placeholders[i] = "?"
			args = append(args, tid)
		}
		if _, err := tx2.ExecContext(r.Context(), `
			DELETE FROM emergency_kill_switches
			WHERE org_id = ? AND target_id IN (`+strings.Join(placeholders, ",")+`)`,
			args...,
		); err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed clearing project kill switches", true, nil)
			return
		}
	}

	resDel, err := tx2.ExecContext(r.Context(), `DELETE FROM projects WHERE org_id = ? AND id = ? AND version = ?`, orgID, projectID, fencedVer)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting project", true, nil)
		return
	}
	if affected, _ := resDel.RowsAffected(); affected == 0 {
		writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "concurrent project teardown conflict", true, nil)
		return
	}

	resEpoch, err := tx2.ExecContext(r.Context(), `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'project', ?, 'SHRED', 1, ?)`,
		orgID, projectID, now,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording shred changelog", true, nil)
		return
	}
	shredEpoch, _ := resEpoch.LastInsertId()

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
	if err := s.txAudit.AppendTx(r.Context(), tx2, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
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

	if err := tx2.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing project crypto-shred", true, nil)
		return
	}

	if s.killSwitch != nil {
		_ = s.killSwitch.RefreshFromDB(r.Context())
	}

	if s.snapshots != nil {
		if s.snapshots.Streams() != nil {
			s.snapshots.Streams().AbortScope(orgID, projectID)
		}
		if refErr := s.snapshots.Refresh(r.Context()); refErr != nil {
			for _, kid := range activeKeyIDs {
				s.snapshots.FenceKeyRevocation(kid, "EMERGENCY_SECURITY", now, shredEpoch)
			}
			w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			s.emitSecurityAudit(r.Context(), sc, "project.snapshot_sync_pending", map[string]any{
				"project_id":   projectID,
				"shred_epoch":  shredEpoch,
				"fenced_keys":  len(activeKeyIDs),
				"error":        refErr.Error(),
			})
		}
	}
	if s.killSwitch != nil && s.killSwitch.Streams() != nil {
		s.killSwitch.Streams().AbortMatching(killswitch.LevelProjectOrOrgFreeze, orgID, projectID)
	}

	writeJSON(w, http.StatusOK, cryptoShredReceiptDTO{
		TargetType:              "PROJECT",
		TargetID:                projectID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(now),
		RevokedVirtualKeysCount: revokedKeysCount,
	})
}

func toOrganizationDTO(org *port.Organization) organizationDTO {
	return organizationDTO{
		ID:              org.ID,
		Slug:            org.Slug,
		Name:            org.DisplayName,
		MinPluginTrust:  org.MinPluginTrust,
		FourEyesEnabled: org.RequireFourEyesApproval,
		Version:         org.Version,
		CreatedAt:       formatTimeRFC3339Ms(org.CreatedAtMs),
	}
}

func toProjectDTO(prj *port.Project) projectDTO {
	return projectDTO{
		ID:        prj.ID,
		OrgID:     prj.OrgID,
		Slug:      prj.Slug,
		Name:      prj.DisplayName,
		Version:   prj.Version,
		CreatedAt: formatTimeRFC3339Ms(prj.CreatedAtMs),
	}
}

func tableExistsTx(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var cnt int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&cnt)
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func cleanupOrgRestrictedFKsTx(ctx context.Context, tx *sql.Tx, orgID string) error {
	var externalKSCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM emergency_kill_switches
		WHERE (org_id IS NULL OR org_id != ?)
		  AND (
		    triggered_by_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
		    OR cleared_by_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
		  )`,
		orgID, orgID, orgID,
	).Scan(&externalKSCount); err != nil {
		return err
	}
	if externalKSCount > 0 {
		return fmt.Errorf("%w: principals of organization %s are referenced by global or external kill switches", port.ErrResourceInUse, orgID)
	}

	if hasACR, err := tableExistsTx(ctx, tx, "admin_change_requests"); err != nil {
		return err
	} else if hasACR {
		var externalACRCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM admin_change_requests
			WHERE (org_id IS NULL OR org_id != ?)
			  AND (
			    initiator_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
			    OR approver_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
			  )`,
			orgID, orgID, orgID,
		).Scan(&externalACRCount); err != nil {
			return err
		}
		if externalACRCount > 0 {
			return fmt.Errorf("%w: principals of organization %s are referenced by global or external change requests", port.ErrResourceInUse, orgID)
		}
	}

	optionalSteps := []struct {
		table string
		query string
		args  []any
	}{
		{
			table: "a2a_tasks",
			query: `UPDATE a2a_tasks SET parent_task_id = NULL WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "a2a_tasks",
			query: `DELETE FROM a2a_tasks WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "approval_requests",
			query: `DELETE FROM approval_requests WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "admin_change_requests",
			query: `DELETE FROM admin_change_requests WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "webhook_subscriptions",
			query: `DELETE FROM webhook_subscriptions WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "agent_endpoints",
			query: `DELETE FROM agent_endpoints WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "mcp_servers",
			query: `DELETE FROM mcp_servers WHERE org_id = ?`,
			args:  []any{orgID},
		},
	}
	for _, st := range optionalSteps {
		exists, err := tableExistsTx(ctx, tx, st.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return err
		}
	}

	steps := []struct {
		query string
		args  []any
	}{
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
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return err
		}
	}
	return nil
}

func cleanupProjectRestrictedFKsTx(ctx context.Context, tx *sql.Tx, orgID, projectID string) error {
	optionalSteps := []struct {
		table string
		query string
		args  []any
	}{
		{
			table: "a2a_tasks",
			query: `UPDATE a2a_tasks SET parent_task_id = NULL WHERE org_id = ? AND (project_id = ? OR agent_endpoint_id IN (SELECT id FROM agent_endpoints WHERE org_id = ? AND project_id = ?) OR caller_virtual_key_id IN (SELECT id FROM virtual_keys WHERE org_id = ? AND project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID, orgID, projectID},
		},
		{
			table: "a2a_tasks",
			query: `DELETE FROM a2a_tasks WHERE org_id = ? AND (project_id = ? OR agent_endpoint_id IN (SELECT id FROM agent_endpoints WHERE org_id = ? AND project_id = ?) OR caller_virtual_key_id IN (SELECT id FROM virtual_keys WHERE org_id = ? AND project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID, orgID, projectID},
		},
		{
			table: "approval_requests",
			query: `DELETE FROM approval_requests WHERE org_id = ? AND (project_id = ? OR requester_virtual_key_id IN (SELECT id FROM virtual_keys WHERE org_id = ? AND project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
		{
			table: "webhook_subscriptions",
			query: `DELETE FROM webhook_subscriptions WHERE org_id = ? AND (project_id = ? OR signing_secret_credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
		{
			table: "agent_endpoints",
			query: `DELETE FROM agent_endpoints WHERE org_id = ? AND (project_id = ? OR credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
		{
			table: "mcp_servers",
			query: `DELETE FROM mcp_servers WHERE org_id = ? AND (project_id = ? OR credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
	}
	for _, st := range optionalSteps {
		exists, err := tableExistsTx(ctx, tx, st.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, st.query, st.args...); err != nil {
			return err
		}
	}

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
	if projectID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionProjectRead); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	prj, err := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionProjectRead, rbac.TargetScope{OrgID: orgID, ProjectID: prj.ID}) {
		return
	}

	envs, err := s.storage.Tenancy().ListEnvironments(r.Context(), orgID, prj.ID)
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
	if projectID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	var req createEnvironmentRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionEnvironmentWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	prj, err := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionEnvironmentWrite, rbac.TargetScope{OrgID: orgID, ProjectID: prj.ID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, prj.ID) {
		return
	}

	slug := strings.TrimSpace(req.Slug)
	name := strings.TrimSpace(req.Name)
	if slug == "" || name == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	env, err := s.storage.Tenancy().CreateEnvironment(r.Context(), orgID, prj.ID, port.CreateEnvironmentParams{
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
