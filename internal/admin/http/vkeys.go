// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type virtualKeySummaryDTO struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	ProjectID            string   `json:"project_id"`
	EnvID                string   `json:"env_id"`
	OwnerPrincipalID     *string  `json:"owner_principal_id,omitempty"`
	KeyPrefix            string   `json:"key_prefix"`
	LastFour             string   `json:"last_four"`
	State                string   `json:"state"`
	ExpiresAt            *string  `json:"expires_at,omitempty"`
	RPMLimit             *int64   `json:"rpm_limit,omitempty"`
	TPMLimit             *int64   `json:"tpm_limit,omitempty"`
	MaxConcurrentStreams *int64   `json:"max_concurrent_streams,omitempty"`
	AllowedPlanes        []string `json:"allowed_planes,omitempty"`
	AllowedModels        []string `json:"allowed_models,omitempty"`
	AllowedRoutes        []string `json:"allowed_routes,omitempty"`
	AllowedMCPTools      []string `json:"allowed_mcp_tools,omitempty"`
	Version              int64    `json:"version"`
	CreatedAt            string   `json:"created_at"`
}

type virtualKeyCreatedDTO struct {
	virtualKeySummaryDTO
	SecretKey string `json:"secret_key"`
}

type createVirtualKeyRequest struct {
	Name                 string   `json:"name"`
	ProjectID            string   `json:"project_id"`
	EnvID                string   `json:"env_id"`
	ExpiresAt            *string  `json:"expires_at"`
	RPMLimit             *int64   `json:"rpm_limit"`
	TPMLimit             *int64   `json:"tpm_limit"`
	MaxConcurrentStreams *int64   `json:"max_concurrent_streams"`
	BudgetCeilingMicros  *int64   `json:"budget_ceiling_micros"`
	AllowedPlanes        []string `json:"allowed_planes"`
	AllowedModels        []string `json:"allowed_models"`
	AllowedRoutes        []string `json:"allowed_routes"`
	AllowedMCPTools      []string `json:"allowed_mcp_tools"`
}

type nullableInt64Field struct {
	Set   bool
	Valid bool
	Value int64
}

func (f *nullableInt64Field) UnmarshalJSON(data []byte) error {
	f.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		f.Valid = false
		return nil
	}
	var v int64
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	f.Valid = true
	f.Value = v
	return nil
}

type updateVirtualKeyRequest struct {
	Name                 *string          `json:"name"`
	RPMLimit             nullableInt64Field `json:"rpm_limit"`
	TPMLimit             nullableInt64Field `json:"tpm_limit"`
	MaxConcurrentStreams *int64           `json:"max_concurrent_streams"`
	AllowedPlanes        []string         `json:"allowed_planes"`
	AllowedModels        []string         `json:"allowed_models"`
	AllowedRoutes        []string         `json:"allowed_routes"`
	AllowedMCPTools      []string         `json:"allowed_mcp_tools"`
}

type rotateVirtualKeyRequest struct {
	GraceWindowSeconds *int64 `json:"grace_window_seconds"`
}

type revokeVirtualKeyRequest struct {
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
}

type revokeVirtualKeyResponse struct {
	Key                 virtualKeySummaryDTO `json:"key"`
	SeveredStreamsCount int                  `json:"severed_streams_count"`
}

func (s *Server) handleListVirtualKeys(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	projFilter := strings.TrimSpace(r.URL.Query().Get("project_id"))
	envFilter := strings.TrimSpace(r.URL.Query().Get("env_id"))

	if projFilter != "" {
		if !s.authorize(w, r, sc, rbac.ActionVKeyRead, rbac.TargetScope{OrgID: orgID, ProjectID: projFilter}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionVKeyRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	if s.vkeys != nil {
		_, _ = s.vkeys.FinalizeExpiredRotationsForOrg(r.Context(), orgID)
	}

	var allowedProjects []string
	if !actorHasOrgScopeBinding(sc.Actor, orgID) {
		allowedProjects = actorProjectIDs(sc.Actor, orgID)
		if len(allowedProjects) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[virtualKeySummaryDTO]{
				Data:    []virtualKeySummaryDTO{},
				HasMore: false,
			})
			return
		}
	}

	filterHash := sha256HexString(fmt.Sprintf("%s|%s|%s", projFilter, envFilter, strings.Join(allowedProjects, ",")))
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "virtual_keys", orgID, filterHash)
	if !ok {
		return
	}

	var query strings.Builder
	args := []any{orgID}
	query.WriteString(`
		SELECT id, org_id, project_id, env_id, owner_principal_id, name,
		       key_prefix, last_four, state, allowed_planes, allowed_models_json, allowed_routes_json, allowed_mcp_tools_json,
		       rpm_limit, tpm_limit, max_concurrent_streams, expires_at_ms, version, created_at_ms
		FROM virtual_keys
		WHERE org_id = ?`)

	if projFilter != "" {
		query.WriteString(` AND project_id = ?`)
		args = append(args, projFilter)
	}
	if envFilter != "" {
		query.WriteString(` AND env_id = ?`)
		args = append(args, envFilter)
	}
	if len(allowedProjects) > 0 {
		placeholders := make([]string, len(allowedProjects))
		for i, pid := range allowedProjects {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		query.WriteString(` AND project_id IN (` + strings.Join(placeholders, ",") + `)`)
	}
	if cur != nil {
		query.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	query.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	type rowItem struct {
		dto       virtualKeySummaryDTO
		createdMs int64
	}
	items := make([]rowItem, 0, limit+1)
	err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		rows, err := q.QueryContext(r.Context(), s.rebind(query.String()), args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id, rowOrg, projectID, envID, name, keyPrefix, lastFour, state string
				ownerID                                                        sql.NullString
				planesCSV, modelsJSON, routesJSON, mcpJSON                     string
				rpmNS, tpmNS, expNS                                            sql.NullInt64
				maxStreams                                                     int64
				version, createdMs                                             int64
			)
			if scanErr := rows.Scan(
				&id, &rowOrg, &projectID, &envID, &ownerID, &name,
				&keyPrefix, &lastFour, &state, &planesCSV, &modelsJSON, &routesJSON, &mcpJSON,
				&rpmNS, &tpmNS, &maxStreams, &expNS, &version, &createdMs,
			); scanErr != nil {
				return scanErr
			}
			dto := virtualKeySummaryDTO{
				ID:                   id,
				Name:                 name,
				ProjectID:            projectID,
				EnvID:                envID,
				KeyPrefix:            keyPrefix,
				LastFour:             lastFour,
				State:                normalizeOpenAPIVKeyState(state),
				AllowedPlanes:        splitAllowedPlanes(planesCSV),
				MaxConcurrentStreams: &maxStreams,
				Version:              version,
				CreatedAt:            formatTimeRFC3339Ms(createdMs),
			}
			if ownerID.Valid {
				v := ownerID.String
				dto.OwnerPrincipalID = &v
			}
			if rpmNS.Valid {
				v := rpmNS.Int64
				dto.RPMLimit = &v
			}
			if tpmNS.Valid {
				v := tpmNS.Int64
				dto.TPMLimit = &v
			}
			if expNS.Valid {
				v := formatTimeRFC3339Ms(expNS.Int64)
				dto.ExpiresAt = &v
			}
			if uerr := json.Unmarshal([]byte(modelsJSON), &dto.AllowedModels); uerr != nil {
				return fmt.Errorf("corrupt allowed_models_json: %w", uerr)
			}
			if strings.TrimSpace(routesJSON) == "" {
				routesJSON = `["*"]`
			}
			if uerr := json.Unmarshal([]byte(routesJSON), &dto.AllowedRoutes); uerr != nil {
				return fmt.Errorf("corrupt allowed_routes_json: %w", uerr)
			}
			if uerr := json.Unmarshal([]byte(mcpJSON), &dto.AllowedMCPTools); uerr != nil {
				return fmt.Errorf("corrupt allowed_mcp_tools_json: %w", uerr)
			}
			items = append(items, rowItem{dto: dto, createdMs: createdMs})
		}
		return rows.Err()
	})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying virtual keys", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]virtualKeySummaryDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "virtual_keys", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[virtualKeySummaryDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateVirtualKey(w http.ResponseWriter, r *http.Request) {
	s.createVirtualKeyCommon(w, r, "")
}

func (s *Server) handleCreateProjectVirtualKey(w http.ResponseWriter, r *http.Request) {
	pathProj := strings.TrimSpace(r.PathValue("projectId"))
	if pathProj == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	s.createVirtualKeyCommon(w, r, pathProj)
}

func (s *Server) createVirtualKeyCommon(w http.ResponseWriter, r *http.Request, pathProjectID string) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createVirtualKeyRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	projectID := strings.TrimSpace(req.ProjectID)
	if pathProjectID != "" {
		if projectID != "" && projectID != pathProjectID {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "project_id_mismatch", "invalid_request_error", "body project_id does not match URL projectId", false, nil)
			return
		}
		projectID = pathProjectID
	}

	if strings.TrimSpace(req.Name) == "" || len(req.Name) > 128 || projectID == "" || strings.TrimSpace(req.EnvID) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "name, project_id, and env_id are required", false, nil)
		return
	}
	if req.RPMLimit != nil && *req.RPMLimit < 1 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "rpm_limit must be >= 1", false, nil)
		return
	}
	if req.TPMLimit != nil && *req.TPMLimit < 1 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "tpm_limit must be >= 1", false, nil)
		return
	}
	if req.MaxConcurrentStreams != nil && *req.MaxConcurrentStreams < 1 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "max_concurrent_streams must be >= 1", false, nil)
		return
	}
	if req.BudgetCeilingMicros != nil && *req.BudgetCeilingMicros < 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "budget_ceiling_micros must be >= 0", false, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyCreate, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}

	if !s.requireActiveOrgAndProject(w, r, orgID, projectID) {
		return
	}

	var ownerPrincipalPtr *string
	var ownerInOrg int
	if err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM principals WHERE id = ? AND org_id = ?`), sc.Actor.PrincipalID, orgID).Scan(&ownerInOrg)
	}); err == nil {
		id := sc.Actor.PrincipalID
		ownerPrincipalPtr = &id
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying owner principal scope", true, nil)
		return
	}

	var expMsPtr *int64
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "expires_at must be valid RFC3339 timestamp", false, nil)
			return
		}
		ms := parsed.UTC().UnixMilli()
		expMsPtr = &ms
	}

	var maxStreams int64
	if req.MaxConcurrentStreams != nil {
		maxStreams = *req.MaxConcurrentStreams
	}
	allowedPlanesCSV := ""
	if req.AllowedPlanes != nil {
		var perr error
		allowedPlanesCSV, perr = normalizePlanesListToCSV(req.AllowedPlanes)
		if perr != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", perr.Error(), false, nil)
			return
		}
	}

	created, err := s.vkeys.Create(r.Context(), vkey.CreateParams{
		OrgID:                orgID,
		ProjectID:            projectID,
		EnvID:                strings.TrimSpace(req.EnvID),
		OwnerPrincipalID:     ownerPrincipalPtr,
		Name:                 req.Name,
		RPMLimit:             req.RPMLimit,
		TPMLimit:             req.TPMLimit,
		MaxConcurrentStreams: maxStreams,
		AllowedPlanes:        allowedPlanesCSV,
		AllowedModels:        req.AllowedModels,
		AllowedRoutes:        req.AllowedRoutes,
		AllowedMCPTools:      req.AllowedMCPTools,
		ExpiresAtMs:          expMsPtr,
		ActorID:              sc.Actor.PrincipalID,
	})
	snapshotSyncPending := false
	if err != nil {
		if errors.Is(err, vkey.ErrSnapshotSyncFailed) && created != nil {
			snapshotSyncPending = true
		} else {
			switch {
			case errors.Is(err, port.ErrNotFound):
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment or project not found", false, nil)
			case errors.Is(err, port.ErrInvalidInput):
				writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			default:
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating virtual key", true, nil)
			}
			return
		}
	}

	if req.BudgetCeilingMicros != nil && s.ledgers != nil {
		nowTime := time.UnixMilli(s.nowMs()).UTC()
		monthStart := time.Date(nowTime.Year(), nowTime.Month(), 1, 0, 0, 0, 0, time.UTC)
		monthEnd := monthStart.AddDate(0, 1, 0)
		if _, lerr := s.ledgers.UpsertLedger(r.Context(), ledger.UpsertLedgerParams{
			OrgID:           orgID,
			ScopeType:       ledger.ScopeVirtualKey,
			ScopeID:         created.Summary.ID,
			PeriodKind:      ledger.PeriodMonthly,
			PeriodStartMs:   monthStart.UnixMilli(),
			PeriodEndMs:     monthEnd.UnixMilli(),
			HardLimitMicros: *req.BudgetCeilingMicros,
		}); lerr != nil {
			_, _ = s.vkeys.RevokeWithReceipt(r.Context(), vkey.RevokeParams{
				OrgID:           orgID,
				KeyID:           created.Summary.ID,
				ExpectedVersion: created.Summary.Version,
				Mode:            vkey.RevokeModeStandard,
				Reason:          "rollback orphaned key after budget ledger creation failure",
				ActorID:         sc.Actor.PrincipalID,
			})
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed provisioning virtual key budget ledger", true, nil)
			return
		}
	}

	if snapshotSyncPending {
		w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
	}
	w.Header().Set("ETag", FormatETag(created.Summary.Version))
	writeJSON(w, http.StatusCreated, toVirtualKeyCreatedDTO(created))
}

func (s *Server) handleUpdateVirtualKey(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	keyID := strings.TrimSpace(r.PathValue("keyId"))
	if keyID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionVKeyCreate); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	var req updateVirtualKeyRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	if s.vkeys != nil {
		_, _ = s.vkeys.FinalizeExpiredRotationsForOrg(r.Context(), orgID)
	}
	cur, err := s.vkeys.Get(r.Context(), orgID, keyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading virtual key", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyCreate, rbac.TargetScope{OrgID: orgID, ProjectID: cur.ProjectID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, cur.ProjectID) {
		return
	}

	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, toVirtualKeySummaryDTO(cur))
		return
	}

	params := vkey.UpdateParams{
		OrgID:           orgID,
		KeyID:           keyID,
		ExpectedVersion: expectedVer,
		Name:            req.Name,
		ActorID:         sc.Actor.PrincipalID,
	}
	if req.RPMLimit.Set {
		if req.RPMLimit.Valid {
			v := req.RPMLimit.Value
			params.RPMLimit = &v
		} else {
			params.ClearRPMLimit = true
		}
	}
	if req.TPMLimit.Set {
		if req.TPMLimit.Valid {
			v := req.TPMLimit.Value
			params.TPMLimit = &v
		} else {
			params.ClearTPMLimit = true
		}
	}
	if req.MaxConcurrentStreams != nil {
		params.MaxConcurrentStreams = req.MaxConcurrentStreams
	}
	if req.AllowedPlanes != nil {
		normCSV, perr := normalizePlanesListToCSV(req.AllowedPlanes)
		if perr != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", perr.Error(), false, nil)
			return
		}
		params.AllowedPlanes = normCSV
		params.HasAllowedPlanes = true
	}
	if req.AllowedModels != nil {
		params.AllowedModels = trimNonEmptyStrings(req.AllowedModels)
		params.HasAllowedModels = true
	}
	if req.AllowedRoutes != nil {
		params.AllowedRoutes = trimNonEmptyStrings(req.AllowedRoutes)
		params.HasAllowedRoutes = true
	}
	if req.AllowedMCPTools != nil {
		params.AllowedMCPTools = trimNonEmptyStrings(req.AllowedMCPTools)
		params.HasAllowedMCPTools = true
	}

	updated, err := s.vkeys.Update(r.Context(), params)
	if err != nil {
		if errors.Is(err, vkey.ErrSnapshotSyncFailed) && updated != nil {
			w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			w.Header().Set("ETag", FormatETag(updated.Version))
			writeJSON(w, http.StatusOK, toVirtualKeySummaryDTO(updated))
			return
		}
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			if latest, gerr := s.vkeys.Get(r.Context(), orgID, keyID); gerr == nil {
				writeOptimisticConflict(w, r, latest.Version, toVirtualKeySummaryDTO(latest))
				return
			}
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "resource version conflict", true, nil)
		case errors.Is(err, vkey.ErrInvalidStateTransition):
			writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", err.Error(), false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed updating virtual key", true, nil)
		}
		return
	}

	w.Header().Set("ETag", FormatETag(updated.Version))
	writeJSON(w, http.StatusOK, toVirtualKeySummaryDTO(updated))
}

func (s *Server) handleRotateVirtualKey(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	keyID := strings.TrimSpace(r.PathValue("keyId"))
	if keyID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionVKeyRotate); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	var req rotateVirtualKeyRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	graceSec := int64(86400)
	if req.GraceWindowSeconds != nil {
		graceSec = *req.GraceWindowSeconds
	}
	if graceSec < 60 || graceSec > 259200 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "grace_window_seconds must be within [60, 259200]", false, nil)
		return
	}

	if s.vkeys != nil {
		_, _ = s.vkeys.FinalizeExpiredRotationsForOrg(r.Context(), orgID)
	}
	cur, err := s.vkeys.Get(r.Context(), orgID, keyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading virtual key", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyRotate, rbac.TargetScope{OrgID: orgID, ProjectID: cur.ProjectID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, cur.ProjectID) {
		return
	}

	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, toVirtualKeySummaryDTO(cur))
		return
	}

	rotated, err := s.vkeys.Rotate(r.Context(), vkey.RotateParams{
		OrgID:           orgID,
		KeyID:           keyID,
		ExpectedVersion: expectedVer,
		GraceSeconds:    graceSec,
		ActorID:         sc.Actor.PrincipalID,
	})
	if err != nil {
		if errors.Is(err, vkey.ErrSnapshotSyncFailed) && rotated != nil {
			w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			w.Header().Set("ETag", FormatETag(rotated.Summary.Version))
			writeJSON(w, http.StatusOK, toVirtualKeyCreatedDTO(rotated))
			return
		}
		switch {
		case errors.Is(err, port.ErrVersionConflict):
			if latest, gerr := s.vkeys.Get(r.Context(), orgID, keyID); gerr == nil {
				writeOptimisticConflict(w, r, latest.Version, toVirtualKeySummaryDTO(latest))
				return
			}
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "resource version conflict", true, nil)
		case errors.Is(err, vkey.ErrInvalidStateTransition):
			writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed rotating virtual key", true, nil)
		}
		return
	}

	w.Header().Set("ETag", FormatETag(rotated.Summary.Version))
	writeJSON(w, http.StatusOK, toVirtualKeyCreatedDTO(rotated))
}

func (s *Server) handleRevokeVirtualKey(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	keyID := strings.TrimSpace(r.PathValue("keyId"))
	if keyID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionVKeyRevoke); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	var req revokeVirtualKeyRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	var internalMode string
	switch req.Mode {
	case "GRACEFUL_DRAIN":
		internalMode = vkey.RevokeModeStandard
	case "EMERGENCY_ABORT_STREAMS":
		internalMode = vkey.RevokeModeEmergency
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "mode must be GRACEFUL_DRAIN or EMERGENCY_ABORT_STREAMS", false, nil)
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}

	if s.vkeys != nil {
		_, _ = s.vkeys.FinalizeExpiredRotationsForOrg(r.Context(), orgID)
	}
	cur, err := s.vkeys.Get(r.Context(), orgID, keyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading virtual key", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyRevoke, rbac.TargetScope{OrgID: orgID, ProjectID: cur.ProjectID}) {
		return
	}
	if internalMode == vkey.RevokeModeStandard && !actorCanGracefulRevokeVKey(sc.Actor, orgID, cur.ProjectID) {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "SECURITY_REVIEWER may only execute EMERGENCY_ABORT_STREAMS revocation", false, nil)
		return
	}

	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, toVirtualKeySummaryDTO(cur))
		return
	}

	receipt, err := s.vkeys.RevokeWithReceipt(r.Context(), vkey.RevokeParams{
		OrgID:           orgID,
		KeyID:           keyID,
		ExpectedVersion: expectedVer,
		Mode:            internalMode,
		Reason:          strings.TrimSpace(req.Reason),
		ActorID:         sc.Actor.PrincipalID,
	})
	if err != nil {
		if errors.Is(err, vkey.ErrSnapshotSyncFailed) && receipt != nil && receipt.Summary != nil {
			w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			w.Header().Set("ETag", FormatETag(receipt.Summary.Version))
			writeJSON(w, http.StatusOK, revokeVirtualKeyResponse{
				Key:                 toVirtualKeySummaryDTO(receipt.Summary),
				SeveredStreamsCount: receipt.SeveredStreamsCount,
			})
			return
		}
		switch {
		case errors.Is(err, port.ErrVersionConflict):
			if latest, gerr := s.vkeys.Get(r.Context(), orgID, keyID); gerr == nil {
				writeOptimisticConflict(w, r, latest.Version, toVirtualKeySummaryDTO(latest))
				return
			}
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "resource version conflict", true, nil)
		case errors.Is(err, vkey.ErrAlreadyRevoked), errors.Is(err, vkey.ErrInvalidStateTransition):
			writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed revoking virtual key", true, nil)
		}
		return
	}

	w.Header().Set("ETag", FormatETag(receipt.Summary.Version))
	writeJSON(w, http.StatusOK, revokeVirtualKeyResponse{
		Key:                 toVirtualKeySummaryDTO(receipt.Summary),
		SeveredStreamsCount: receipt.SeveredStreamsCount,
	})
}

func actorCanGracefulRevokeVKey(actor *rbac.Actor, orgID, projectID string) bool {
	if actorHasOperator(actor) {
		return true
	}
	if actor == nil || orgID == "" {
		return false
	}
	for _, b := range actor.Bindings {
		if b.OrgID == nil || *b.OrgID != orgID {
			continue
		}
		if b.ScopeType == rbac.ScopeOrg && (b.RoleName == rbac.RoleOrgOwner || b.RoleName == rbac.RoleTeamAdmin) {
			return true
		}
		if b.ScopeType == rbac.ScopeProject && b.ScopeID == projectID && b.RoleName == rbac.RoleTeamAdmin {
			return true
		}
	}
	return false
}

// normalizeOpenAPIVKeyState collapses the 7 internal SM-05 storage states onto the
// 5 canonical OpenAPI VirtualKeySummary.state enum values:
//   - SUSPENDED -> QUOTA_PAUSED
//   - PURGED_METADATA_RETAINED -> REVOKED
func normalizeOpenAPIVKeyState(state string) string {
	switch state {
	case snapshot.KeyStateCreatedActive,
		snapshot.KeyStateRotatingDualValid,
		snapshot.KeyStateQuotaPaused,
		snapshot.KeyStateExpired,
		snapshot.KeyStateRevoked:
		return state
	case snapshot.KeyStatePurgedMetadataRetained:
		return snapshot.KeyStateRevoked
	case snapshot.KeyStateSuspended:
		return snapshot.KeyStateQuotaPaused
	default:
		return state
	}
}

func toVirtualKeySummaryDTO(s *vkey.VirtualKeySummary) virtualKeySummaryDTO {
	var expStr *string
	if s.ExpiresAtMs != nil {
		v := formatTimeRFC3339Ms(*s.ExpiresAtMs)
		expStr = &v
	}
	maxStreams := s.MaxConcurrentStreams
	return virtualKeySummaryDTO{
		ID:                   s.ID,
		Name:                 s.Name,
		ProjectID:            s.ProjectID,
		EnvID:                s.EnvID,
		OwnerPrincipalID:     s.OwnerPrincipalID,
		KeyPrefix:            s.KeyPrefix,
		LastFour:             s.LastFour,
		State:                normalizeOpenAPIVKeyState(s.State),
		ExpiresAt:            expStr,
		RPMLimit:             s.RPMLimit,
		TPMLimit:             s.TPMLimit,
		MaxConcurrentStreams: &maxStreams,
		AllowedPlanes:        splitAllowedPlanes(s.AllowedPlanes),
		AllowedModels:        append([]string(nil), s.AllowedModels...),
		AllowedRoutes:        append([]string(nil), s.AllowedRoutes...),
		AllowedMCPTools:      append([]string(nil), s.AllowedMCPTools...),
		Version:              s.Version,
		CreatedAt:            formatTimeRFC3339Ms(s.CreatedAtMs),
	}
}

func toVirtualKeyCreatedDTO(c *vkey.CreatedVirtualKey) virtualKeyCreatedDTO {
	return virtualKeyCreatedDTO{
		virtualKeySummaryDTO: toVirtualKeySummaryDTO(&c.Summary),
		SecretKey:            c.SecretKey,
	}
}

func normalizePlanesListToCSV(planes []string) (string, error) {
	if len(planes) == 0 {
		return "", fmt.Errorf("%w: allowed_planes must include at least one plane (PROVIDER, MCP, A2A)", port.ErrInvalidInput)
	}
	seen := make(map[string]bool, len(planes))
	out := make([]string, 0, len(planes))
	for _, raw := range planes {
		token := strings.ToUpper(strings.TrimSpace(raw))
		switch token {
		case "PROVIDER", "MCP", "A2A":
		default:
			return "", fmt.Errorf("%w: invalid allowed_planes entry %q", port.ErrInvalidInput, raw)
		}
		if !seen[token] {
			seen[token] = true
			out = append(out, token)
		}
	}
	return strings.Join(out, ","), nil
}

func splitAllowedPlanes(csv string) []string {
	parts := strings.Split(strings.TrimSpace(csv), ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, raw := range parts {
		token := strings.ToUpper(strings.TrimSpace(raw))
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	if len(out) == 0 {
		return []string{"PROVIDER"}
	}
	return out
}

func trimNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, raw := range items {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
