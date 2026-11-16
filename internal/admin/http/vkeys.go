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
	AllowedModels        []string `json:"allowed_models,omitempty"`
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
	AllowedModels        []string `json:"allowed_models"`
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
	AllowedModels        []string         `json:"allowed_models"`
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
	orgID := sc.OrgID
	if sc.IsOperator {
		if qOrg := strings.TrimSpace(r.URL.Query().Get("org_id")); qOrg != "" {
			orgID = qOrg
		}
	}
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "organization scope is required", false, nil)
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
		       key_prefix, last_four, state, allowed_models_json, allowed_mcp_tools_json,
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

	rows, err := s.db.QueryContext(r.Context(), query.String(), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying virtual keys", true, nil)
		return
	}
	defer rows.Close()

	type rowItem struct {
		dto       virtualKeySummaryDTO
		createdMs int64
	}
	items := make([]rowItem, 0, limit+1)
	for rows.Next() {
		var (
			id, rowOrg, projectID, envID, name, keyPrefix, lastFour, state string
			ownerID                                                        sql.NullString
			modelsJSON, mcpJSON                                            string
			rpmNS, tpmNS, expNS                                            sql.NullInt64
			maxStreams                                                     int64
			version, createdMs                                             int64
		)
		if scanErr := rows.Scan(
			&id, &rowOrg, &projectID, &envID, &ownerID, &name,
			&keyPrefix, &lastFour, &state, &modelsJSON, &mcpJSON,
			&rpmNS, &tpmNS, &maxStreams, &expNS, &version, &createdMs,
		); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning virtual key row", true, nil)
			return
		}
		dto := virtualKeySummaryDTO{
			ID:                   id,
			Name:                 name,
			ProjectID:            projectID,
			EnvID:                envID,
			KeyPrefix:            keyPrefix,
			LastFour:             lastFour,
			State:                normalizeOpenAPIVKeyState(state),
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
		_ = json.Unmarshal([]byte(modelsJSON), &dto.AllowedModels)
		_ = json.Unmarshal([]byte(mcpJSON), &dto.AllowedMCPTools)
		items = append(items, rowItem{dto: dto, createdMs: createdMs})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating virtual keys", true, nil)
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

	orgID := sc.OrgID
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "organization scope required", false, nil)
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

	var exists int
	if err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, projectID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	} else if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyCreate, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
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

	created, err := s.vkeys.Create(r.Context(), vkey.CreateParams{
		OrgID:                orgID,
		ProjectID:            projectID,
		EnvID:                strings.TrimSpace(req.EnvID),
		OwnerPrincipalID:     &sc.Actor.PrincipalID,
		Name:                 req.Name,
		RPMLimit:             req.RPMLimit,
		TPMLimit:             req.TPMLimit,
		MaxConcurrentStreams: maxStreams,
		AllowedModels:        req.AllowedModels,
		AllowedMCPTools:      req.AllowedMCPTools,
		ExpiresAtMs:          expMsPtr,
		ActorID:              sc.Actor.PrincipalID,
	})
	if err != nil {
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
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	var req updateVirtualKeyRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	cur, err := s.vkeys.Get(r.Context(), sc.OrgID, keyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading virtual key", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyCreate, rbac.TargetScope{OrgID: sc.OrgID, ProjectID: cur.ProjectID}) {
		return
	}

	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, toVirtualKeySummaryDTO(cur))
		return
	}

	params := vkey.UpdateParams{
		OrgID:           sc.OrgID,
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
	if req.AllowedModels != nil {
		params.AllowedModels = req.AllowedModels
		params.HasAllowedModels = true
	}
	if req.AllowedMCPTools != nil {
		params.AllowedMCPTools = req.AllowedMCPTools
		params.HasAllowedMCPTools = true
	}

	updated, err := s.vkeys.Update(r.Context(), params)
	if err != nil {
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			if latest, gerr := s.vkeys.Get(r.Context(), sc.OrgID, keyID); gerr == nil {
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

	cur, err := s.vkeys.Get(r.Context(), sc.OrgID, keyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading virtual key", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyRotate, rbac.TargetScope{OrgID: sc.OrgID, ProjectID: cur.ProjectID}) {
		return
	}

	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, toVirtualKeySummaryDTO(cur))
		return
	}

	rotated, err := s.vkeys.Rotate(r.Context(), vkey.RotateParams{
		OrgID:           sc.OrgID,
		KeyID:           keyID,
		ExpectedVersion: expectedVer,
		GraceSeconds:    graceSec,
		ActorID:         sc.Actor.PrincipalID,
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrVersionConflict):
			if latest, gerr := s.vkeys.Get(r.Context(), sc.OrgID, keyID); gerr == nil {
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

	cur, err := s.vkeys.Get(r.Context(), sc.OrgID, keyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading virtual key", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionVKeyRevoke, rbac.TargetScope{OrgID: sc.OrgID, ProjectID: cur.ProjectID}) {
		return
	}

	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, toVirtualKeySummaryDTO(cur))
		return
	}

	receipt, err := s.vkeys.RevokeWithReceipt(r.Context(), vkey.RevokeParams{
		OrgID:           sc.OrgID,
		KeyID:           keyID,
		ExpectedVersion: expectedVer,
		Mode:            internalMode,
		Reason:          strings.TrimSpace(req.Reason),
		ActorID:         sc.Actor.PrincipalID,
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrVersionConflict):
			if latest, gerr := s.vkeys.Get(r.Context(), sc.OrgID, keyID); gerr == nil {
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
		AllowedModels:        append([]string(nil), s.AllowedModels...),
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
