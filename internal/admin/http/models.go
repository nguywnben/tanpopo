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
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type capabilityProfileDTO struct {
	Streaming         bool `json:"streaming"`
	ToolCalling       bool `json:"tool_calling"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	StrictJSONSchema  bool `json:"strict_json_schema"`
	JSONObject        bool `json:"json_object"`
	Vision            bool `json:"vision"`
	AudioInput        bool `json:"audio_input"`
	DocumentInput     bool `json:"document_input"`
	Reasoning         bool `json:"reasoning"`
	PromptCaching     bool `json:"prompt_caching"`
	Embeddings        bool `json:"embeddings"`
	Rerank            bool `json:"rerank"`
}

type logicalModelDTO struct {
	ID              string               `json:"id"`
	Slug            string               `json:"slug"`
	DisplayName     string               `json:"display_name"`
	ContextWindow   int64                `json:"context_window"`
	MaxOutputTokens int64                `json:"max_output_tokens"`
	AssuranceTier   string               `json:"assurance_tier"`
	Capabilities    capabilityProfileDTO `json:"capabilities"`
}

type createLogicalModelRequest struct {
	ID              string                `json:"id"`
	Slug            string                `json:"slug"`
	DisplayName     string                `json:"display_name"`
	ModalityKind    string                `json:"modality_kind"`
	ContextWindow   int64                 `json:"context_window"`
	MaxOutputTokens int64                 `json:"max_output_tokens"`
	AssuranceTier   string                `json:"assurance_tier"`
	Capabilities    *capabilityProfileDTO `json:"capabilities"`
}

type rateCardNanoUSDDTO struct {
	InputTokenNanoUSD      int64 `json:"input_token_nano_usd"`
	OutputTokenNanoUSD     int64 `json:"output_token_nano_usd"`
	CacheReadTokenNanoUSD  int64 `json:"cache_read_token_nano_usd"`
	CacheWriteTokenNanoUSD int64 `json:"cache_write_token_nano_usd"`
	ReasoningTokenNanoUSD  int64 `json:"reasoning_token_nano_usd"`
	ImageUnitNanoUSD       int64 `json:"image_unit_nano_usd"`
	AudioSecNanoUSD        int64 `json:"audio_sec_nano_usd"`
	DocumentPageNanoUSD    int64 `json:"document_page_nano_usd"`
	SelfHostedZeroRate     bool  `json:"self_hosted_zero_rate"`
}

type storedPricingDocument struct {
	EnvID                    string `json:"env_id,omitempty"`
	InputPer1MNanoUSD        int64  `json:"input_per_1m_nano_usd"`
	OutputPer1MNanoUSD       int64  `json:"output_per_1m_nano_usd"`
	CacheReadPer1MNanoUSD    int64  `json:"cache_read_per_1m_nano_usd,omitempty"`
	CacheWritePer1MNanoUSD   int64  `json:"cache_write_per_1m_nano_usd,omitempty"`
	ReasoningPer1MNanoUSD    int64  `json:"reasoning_per_1m_nano_usd,omitempty"`
	ImagePerUnitNanoUSD      int64  `json:"image_per_unit_nano_usd,omitempty"`
	AudioInputPerSecNanoUSD  int64  `json:"audio_input_per_sec_nano_usd,omitempty"`
	DocumentPageNanoUSD      int64  `json:"document_page_nano_usd,omitempty"`
	InputTokenNanoUSD        int64  `json:"input_token_nano_usd"`
	OutputTokenNanoUSD       int64  `json:"output_token_nano_usd"`
	CacheReadTokenNanoUSD    int64  `json:"cache_read_token_nano_usd,omitempty"`
	CacheWriteTokenNanoUSD   int64  `json:"cache_write_token_nano_usd,omitempty"`
	ReasoningTokenNanoUSD    int64  `json:"reasoning_token_nano_usd,omitempty"`
	ImageUnitNanoUSD         int64  `json:"image_unit_nano_usd,omitempty"`
	AudioSecNanoUSD          int64  `json:"audio_sec_nano_usd,omitempty"`
	DocumentPageTokenNanoUSD int64  `json:"document_page_token_nano_usd,omitempty"`
	SelfHostedZeroRate       bool   `json:"self_hosted_zero_rate,omitempty"`
}

type modelDeploymentDTO struct {
	ID                 string             `json:"id"`
	EnvID              string             `json:"env_id"`
	LogicalModelID     string             `json:"logical_model_id"`
	ConnectionID       string             `json:"connection_id"`
	UpstreamModelName  string             `json:"upstream_model_name"`
	DataRegion         string             `json:"data_region"`
	State              string             `json:"state"`
	IdleChunkTimeoutMs int64              `json:"idle_chunk_timeout_ms"`
	FirstByteTimeoutMs int64              `json:"first_byte_timeout_ms"`
	RateCard           rateCardNanoUSDDTO `json:"rate_card"`
	Version            int64              `json:"version"`
}

type createDeploymentRequest struct {
	EnvID                string              `json:"env_id"`
	LogicalModelID       string              `json:"logical_model_id"`
	ConnectionID         string              `json:"connection_id"`
	UpstreamModelName    string              `json:"upstream_model_name"`
	DataRegion           string              `json:"data_region"`
	ActivateImmediately  *bool               `json:"activate_immediately"`
	IdleChunkTimeoutMs   int64               `json:"idle_chunk_timeout_ms"`
	FirstByteTimeoutMs   int64               `json:"first_byte_timeout_ms"`
	RateCard             *rateCardNanoUSDDTO `json:"rate_card"`
}

type updateDeploymentRequest struct {
	State              *string             `json:"state"`
	IdleChunkTimeoutMs *int64              `json:"idle_chunk_timeout_ms"`
	FirstByteTimeoutMs *int64              `json:"first_byte_timeout_ms"`
	RateCard           *rateCardNanoUSDDTO `json:"rate_card"`
}

func (s *Server) handleListAdminModels(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	envFilter := strings.TrimSpace(r.URL.Query().Get("env_id"))
	if envFilter != "" {
		projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envFilter)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment filter", true, nil)
			return
		}
		if !found {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionModelRead, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionModelRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("models|" + envFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "models", orgID, filterHash)
	if !ok {
		return
	}

	var q strings.Builder
	args := []any{orgID}
	q.WriteString(`
		SELECT id, canonical_slug, context_window, max_output_tokens, base_capabilities_json, created_at_ms
		FROM logical_models
		WHERE (org_id = ? OR org_id IS NULL)`)
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), q.String(), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing logical models", true, nil)
		return
	}
	defer rows.Close()

	type modelRow struct {
		dto       logicalModelDTO
		createdMs int64
	}
	var items []modelRow
	for rows.Next() {
		var (
			id, slug, capsJSON string
			ctxWin, maxOut     int64
			createdMs          int64
		)
		if scanErr := rows.Scan(&id, &slug, &ctxWin, &maxOut, &capsJSON, &createdMs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning logical model", true, nil)
			return
		}
		var capsMap map[string]string
		if strings.TrimSpace(capsJSON) != "" {
			_ = json.Unmarshal([]byte(capsJSON), &capsMap)
		}
		dto := toLogicalModelDTO(id, slug, slug, ctxWin, maxOut, "", capsMap)
		items = append(items, modelRow{dto: dto, createdMs: createdMs})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating logical models", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]logicalModelDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "models", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding pagination cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[logicalModelDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateAdminModel(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createLogicalModelRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionModelWrite, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.requireActiveOrg(w, r, orgID) {
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" || req.ContextWindow < 1 || req.MaxOutputTokens < 1 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug, positive context_window, and positive max_output_tokens are required", false, nil)
		return
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = slug
	}
	assurance := strings.ToUpper(strings.TrimSpace(req.AssuranceTier))
	if assurance == "" {
		assurance = catalog.AssuranceDeclared
	}
	switch assurance {
	case catalog.AssuranceDeclared, catalog.AssuranceTested, catalog.AssuranceVerified:
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "assurance_tier must be DECLARED, TESTED, or VERIFIED", false, nil)
		return
	}
	caps := capabilityProfileDTO{}
	if req.Capabilities != nil {
		caps = *req.Capabilities
	}
	modality := strings.ToUpper(strings.TrimSpace(req.ModalityKind))
	if modality == "" {
		switch {
		case caps.Embeddings && !caps.Streaming:
			modality = catalog.ModalityEmbedding
		case caps.Rerank && !caps.Streaming:
			modality = catalog.ModalityRerank
		case caps.Vision || caps.AudioInput || caps.DocumentInput:
			modality = catalog.ModalityMultimodal
		default:
			modality = catalog.ModalityChat
		}
	}

	baseCaps := capabilityProfileToMap(caps, assurance)
	capsJSON, err := json.Marshal(baseCaps)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding model capabilities", true, nil)
		return
	}
	modelID, err := idgen.New(idgen.PrefixLogicalModel)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting logical model id", true, nil)
		return
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting logical model tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO logical_models (
			id, org_id, canonical_slug, modality_kind, context_window,
			max_output_tokens, base_capabilities_json, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		modelID, orgID, slug, modality, req.ContextWindow, req.MaxOutputTokens, string(capsJSON), now, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "logical model slug already exists in organization", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "logical_model", modelID, port.MutationUpsert, false, sc.Actor.PrincipalID, "logical_model.created", map[string]any{
		"canonical_slug": slug,
		"modality_kind":  modality,
		"assurance_tier": assurance,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording model audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing logical model", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, toLogicalModelDTO(modelID, slug, displayName, req.ContextWindow, req.MaxOutputTokens, assurance, baseCaps))
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	envFilter := strings.TrimSpace(r.URL.Query().Get("env_id"))
	statusFilter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if envFilter != "" {
		projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envFilter)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment", true, nil)
			return
		}
		if !found {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionModelRead, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionModelRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("deployments|" + envFilter + "|" + statusFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "deployments", orgID, filterHash)
	if !ok {
		return
	}

	var allowedProjects map[string]bool
	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	if !orgWide {
		allowedProjects = make(map[string]bool)
		for _, pid := range actorProjectIDs(sc.Actor, orgID) {
			allowedProjects[pid] = true
		}
	}

	if !orgWide && envFilter == "" && len(allowedProjects) == 0 {
		writeJSON(w, http.StatusOK, paginatedListEnvelope[modelDeploymentDTO]{
			Data:    []modelDeploymentDTO{},
			HasMore: false,
		})
		return
	}

	type depRow struct {
		dto       modelDeploymentDTO
		createdMs int64
	}
	items := make([]depRow, 0, limit+1)
	needFilter := envFilter != "" || !orgWide
	batchLimit := limit + 1
	if needFilter {
		batchLimit = limit*2 + 20
	}
	hasCur := cur != nil
	var batchCreatedMs int64
	var batchLastID string
	if cur != nil {
		batchCreatedMs = cur.CreatedMs
		batchLastID = cur.LastID
	}

	for len(items) < limit+1 {
		var q strings.Builder
		args := []any{orgID}
		q.WriteString(`
		SELECT md.id, md.logical_model_id, md.connection_id, md.upstream_model_name,
		       md.pricing_nano_usd_json, md.state, md.idle_chunk_timeout_ms,
		       md.first_byte_timeout_ms, md.version, md.created_at_ms,
		       pc.data_region, pc.owner_project_id, pc.shared_project_ids_json
		FROM model_deployments md
		JOIN provider_connections pc ON pc.org_id = md.org_id AND pc.id = md.connection_id
		WHERE md.org_id = ?`)
		if statusFilter != "" {
			q.WriteString(` AND md.state = ?`)
			args = append(args, statusFilter)
		}
		if hasCur {
			q.WriteString(` AND (md.created_at_ms < ? OR (md.created_at_ms = ? AND md.id < ?))`)
			args = append(args, batchCreatedMs, batchCreatedMs, batchLastID)
		}
		q.WriteString(` ORDER BY md.created_at_ms DESC, md.id DESC LIMIT ?`)
		args = append(args, batchLimit)

		rows, err := s.db.QueryContext(r.Context(), q.String(), args...)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing deployments", true, nil)
			return
		}

		type rawDepRow struct {
			id           string
			modelID      string
			connID       string
			upstreamName string
			pricingJSON  string
			state        string
			dataRegion   string
			sharedJSON   string
			ownerProj    sql.NullString
			idleTO       int64
			fbTO         int64
			ver          int64
			createdMs    int64
		}
		rawRows := make([]rawDepRow, 0, batchLimit)
		for rows.Next() {
			var rrow rawDepRow
			if scanErr := rows.Scan(&rrow.id, &rrow.modelID, &rrow.connID, &rrow.upstreamName, &rrow.pricingJSON, &rrow.state, &rrow.idleTO, &rrow.fbTO, &rrow.ver, &rrow.createdMs, &rrow.dataRegion, &rrow.ownerProj, &rrow.sharedJSON); scanErr != nil {
				_ = rows.Close()
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning deployment", true, nil)
				return
			}
			rawRows = append(rawRows, rrow)
		}
		iterErr := rows.Err()
		_ = rows.Close()
		if iterErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating deployments", true, nil)
			return
		}

		sqlCount := len(rawRows)
		for _, rrow := range rawRows {
			batchCreatedMs = rrow.createdMs
			batchLastID = rrow.id
			if !orgWide && !connectionVisibleToAnyProject(rrow.ownerProj, rrow.sharedJSON, allowedProjects) {
				continue
			}
			envID, rateCard := decodeStoredPricingDocument(rrow.pricingJSON)
			if envID == "" {
				envID = s.resolveDeploymentEnvID(r.Context(), orgID, rrow.id)
			}
			if !orgWide {
				if envID != "" {
					envProjID, found, envErr := s.lookupEnvironmentProject(r.Context(), orgID, envID)
					if envErr != nil {
						writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed resolving deployment environment", true, nil)
						return
					}
					if !found || !allowedProjects[envProjID] {
						continue
					}
				} else if !rrow.ownerProj.Valid || !allowedProjects[rrow.ownerProj.String] {
					continue
				}
			}
			if envFilter != "" && envID != envFilter {
				continue
			}
			items = append(items, depRow{
				dto: modelDeploymentDTO{
					ID:                 rrow.id,
					EnvID:              envID,
					LogicalModelID:     rrow.modelID,
					ConnectionID:       rrow.connID,
					UpstreamModelName:  rrow.upstreamName,
					DataRegion:         rrow.dataRegion,
					State:              rrow.state,
					IdleChunkTimeoutMs: rrow.idleTO,
					FirstByteTimeoutMs: rrow.fbTO,
					RateCard:           rateCard,
					Version:            rrow.ver,
				},
				createdMs: rrow.createdMs,
			})
			if len(items) >= limit+1 {
				break
			}
		}
		if len(items) >= limit+1 || sqlCount < batchLimit {
			break
		}
		hasCur = true
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]modelDeploymentDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "deployments", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[modelDeploymentDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createDeploymentRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	envID := strings.TrimSpace(req.EnvID)
	modelID := strings.TrimSpace(req.LogicalModelID)
	connID := strings.TrimSpace(req.ConnectionID)
	upstreamName := strings.TrimSpace(req.UpstreamModelName)
	dataRegion := strings.ToUpper(strings.TrimSpace(req.DataRegion))
	if envID == "" || modelID == "" || connID == "" || upstreamName == "" || req.RateCard == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "env_id, logical_model_id, connection_id, upstream_model_name, and rate_card are required", false, nil)
		return
	}
	if !isValidDataRegion(dataRegion) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "data_region must be EU, US, APAC, or ON_PREM", false, nil)
		return
	}
	if !validateRateCardNonNegative(*req.RateCard) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "rate_card nano-USD values cannot be negative", false, nil)
		return
	}

	projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed looking up environment", true, nil)
		return
	}
	if !found {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionModelWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, projID) {
		return
	}

	var connRegion string
	var connOwnerProj sql.NullString
	var connSharedJSON string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT data_region, owner_project_id, shared_project_ids_json
		FROM provider_connections
		WHERE org_id = ? AND id = ?`, orgID, connID,
	).Scan(&connRegion, &connOwnerProj, &connSharedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying provider connection", true, nil)
		return
	}
	if !connectionVisibleToAnyProject(connOwnerProj, connSharedJSON, map[string]bool{projID: true}) && !actorIsOrgOwnerOrOperator(sc.Actor, orgID) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
		return
	}
	if connRegion != dataRegion {
		writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "data_region_mismatch", "invalid_request_error", fmt.Sprintf("deployment data_region %s must match connection data_region %s", dataRegion, connRegion), false, nil)
		return
	}

	var modelOrg sql.NullString
	err = s.db.QueryRowContext(r.Context(), `SELECT org_id FROM logical_models WHERE id = ?`, modelID).Scan(&modelOrg)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && modelOrg.Valid && modelOrg.String != orgID) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "logical model not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying logical model", true, nil)
		return
	}

	state := catalog.DeployStateActiveHealthy
	if req.ActivateImmediately != nil && !*req.ActivateImmediately {
		state = catalog.DeployStateProvisioned
	}
	idleTO := req.IdleChunkTimeoutMs
	if idleTO == 0 {
		idleTO = 15000
	}
	fbTO := req.FirstByteTimeoutMs
	if fbTO == 0 {
		fbTO = 30000
	}
	if idleTO < 1000 || fbTO < 1000 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "timeouts must be >= 1000 ms", false, nil)
		return
	}

	pricingDoc := encodeStoredPricingDocument(envID, *req.RateCard)
	pricingBytes, err := json.Marshal(pricingDoc)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding rate card", true, nil)
		return
	}

	depID, err := idgen.New(idgen.PrefixDeployment)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting deployment id", true, nil)
		return
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting deployment creation tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO model_deployments (
			id, org_id, connection_id, logical_model_id, upstream_model_name,
			native_dialect, capability_overrides_json, assurance_level,
			pricing_nano_usd_json, state, cooldown_until_ms,
			connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms,
			version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, NULL, '{}', ?, ?, ?, NULL, 5000, ?, ?, 1, ?, ?)`,
		depID, orgID, connID, modelID, upstreamName, catalog.AssuranceDeclared,
		string(pricingBytes), state, fbTO, idleTO, now, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "model deployment already exists", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "model_deployment", depID, port.MutationUpsert, false, sc.Actor.PrincipalID, "model_deployment.created", map[string]any{
		"env_id":              envID,
		"connection_id":       connID,
		"logical_model_id":    modelID,
		"upstream_model_name": upstreamName,
		"state":               state,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording deployment audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing deployment", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, modelDeploymentDTO{
		ID:                 depID,
		EnvID:              envID,
		LogicalModelID:     modelID,
		ConnectionID:       connID,
		UpstreamModelName:  upstreamName,
		DataRegion:         connRegion,
		State:              state,
		IdleChunkTimeoutMs: idleTO,
		FirstByteTimeoutMs: fbTO,
		RateCard:           *req.RateCard,
		Version:            1,
	})
}

func (s *Server) handleUpdateDeployment(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	depID := strings.TrimSpace(r.PathValue("deploymentId"))
	if depID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "deployment not found", false, nil)
		return
	}
	var req updateDeploymentRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	cur, ownerProj, sharedJSON, err := s.loadDeploymentDTO(r.Context(), orgID, depID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "deployment not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading deployment", true, nil)
		return
	}
	if _, ok := s.authorizeDeploymentScope(w, r, sc, orgID, cur, ownerProj, sharedJSON); !ok {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}

	nextState := cur.State
	if req.State != nil {
		sVal := strings.ToUpper(strings.TrimSpace(*req.State))
		switch sVal {
		case catalog.DeployStateActiveHealthy, catalog.DeployStateDraining, catalog.DeployStateDisabled:
			nextState = sVal
		default:
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "state must be ACTIVE_HEALTHY, DRAINING_MAINTENANCE, or DISABLED", false, nil)
			return
		}
	}
	nextIdle := cur.IdleChunkTimeoutMs
	if req.IdleChunkTimeoutMs != nil {
		if *req.IdleChunkTimeoutMs < 1000 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "idle_chunk_timeout_ms must be >= 1000", false, nil)
			return
		}
		nextIdle = *req.IdleChunkTimeoutMs
	}
	nextFB := cur.FirstByteTimeoutMs
	if req.FirstByteTimeoutMs != nil {
		if *req.FirstByteTimeoutMs < 1000 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "first_byte_timeout_ms must be >= 1000", false, nil)
			return
		}
		nextFB = *req.FirstByteTimeoutMs
	}
	nextCard := cur.RateCard
	if req.RateCard != nil {
		if !validateRateCardNonNegative(*req.RateCard) {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "rate_card nano-USD values cannot be negative", false, nil)
			return
		}
		nextCard = *req.RateCard
	}

	pricingDoc := encodeStoredPricingDocument(cur.EnvID, nextCard)
	pricingBytes, err := json.Marshal(pricingDoc)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding rate card", true, nil)
		return
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting deployment update tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(r.Context(), `
		UPDATE model_deployments
		SET pricing_nano_usd_json = ?, state = ?, first_byte_timeout_ms = ?, idle_chunk_timeout_ms = ?,
		    version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		string(pricingBytes), nextState, nextFB, nextIdle, now, orgID, depID, expectedVer,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed updating deployment", true, nil)
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		_ = tx.Rollback()
		latest, _, _, lerr := s.loadDeploymentDTO(r.Context(), orgID, depID)
		if errors.Is(lerr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "deployment not found", false, nil)
			return
		}
		if lerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading deployment", true, nil)
			return
		}
		writeOptimisticConflict(w, r, latest.Version, latest)
		return
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "model_deployment", depID, port.MutationUpsert, false, sc.Actor.PrincipalID, "model_deployment.updated", map[string]any{
		"state":                 nextState,
		"idle_chunk_timeout_ms": nextIdle,
		"first_byte_timeout_ms": nextFB,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording deployment update audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing deployment update", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	cur.State = nextState
	cur.IdleChunkTimeoutMs = nextIdle
	cur.FirstByteTimeoutMs = nextFB
	cur.RateCard = nextCard
	cur.Version = expectedVer + 1
	w.Header().Set("ETag", FormatETag(cur.Version))
	writeJSON(w, http.StatusOK, cur)
}

func (s *Server) handleDeleteDeployment(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	depID := strings.TrimSpace(r.PathValue("deploymentId"))
	if depID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "deployment not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	cur, ownerProj, sharedJSON, err := s.loadDeploymentDTO(r.Context(), orgID, depID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "deployment not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading deployment", true, nil)
		return
	}
	if _, ok := s.authorizeDeploymentScope(w, r, sc, orgID, cur, ownerProj, sharedJSON); !ok {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting deployment deletion tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	var refCount int
	if err := tx.QueryRowContext(r.Context(), `SELECT COUNT(1) FROM route_targets WHERE org_id = ? AND deployment_id = ?`, orgID, depID).Scan(&refCount); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking deployment references", true, nil)
		return
	}
	if refCount > 0 {
		writeErrorEnvelope(w, r, http.StatusConflict, "deployment_referenced_by_routes", "conflict_error", "model deployment is referenced by active routes", false, nil)
		return
	}

	res, err := tx.ExecContext(r.Context(), `DELETE FROM model_deployments WHERE org_id = ? AND id = ? AND version = ?`, orgID, depID, expectedVer)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusConflict, "deployment_referenced_by_routes", "conflict_error", "model deployment cannot be deleted while referenced", false, nil)
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		_ = tx.Rollback()
		latest, _, _, lerr := s.loadDeploymentDTO(r.Context(), orgID, depID)
		if errors.Is(lerr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "deployment not found", false, nil)
			return
		}
		if lerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading deployment", true, nil)
			return
		}
		writeOptimisticConflict(w, r, latest.Version, latest)
		return
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "model_deployment", depID, port.MutationDelete, false, sc.Actor.PrincipalID, "model_deployment.deleted", map[string]any{
		"logical_model_id":    cur.LogicalModelID,
		"connection_id":       cur.ConnectionID,
		"upstream_model_name": cur.UpstreamModelName,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording deployment delete audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing deployment deletion", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authorizeDeploymentScope(w http.ResponseWriter, r *http.Request, sc *authSessionContext, orgID string, cur modelDeploymentDTO, ownerProj sql.NullString, _ string) (string, bool) {
	scopeProj := ""
	if cur.EnvID != "" {
		pid, found, err := s.lookupEnvironmentProject(r.Context(), orgID, cur.EnvID)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating deployment environment", true, nil)
			return "", false
		}
		if found {
			scopeProj = pid
		}
	}
	if scopeProj == "" && ownerProj.Valid {
		scopeProj = ownerProj.String
	}
	// Keep scopeProj empty when neither deployment env_id nor connection owner_project_id
	// identifies a single owning project; org-level/shared deployments require org-wide privileges.
	if !s.authorize(w, r, sc, rbac.ActionModelWrite, rbac.TargetScope{OrgID: orgID, ProjectID: scopeProj}) {
		return "", false
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, scopeProj) {
		return "", false
	}
	return scopeProj, true
}

func (s *Server) lookupEnvironmentProject(ctx context.Context, orgID, envID string) (string, bool, error) {
	var projID string
	err := s.db.QueryRowContext(ctx, `SELECT project_id FROM environments WHERE org_id = ? AND id = ?`, orgID, envID).Scan(&projID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return projID, true, nil
}

func (s *Server) resolveDeploymentEnvID(ctx context.Context, orgID, depID string) string {
	var envID string
	err := s.db.QueryRowContext(ctx, `
		SELECT rp.env_id
		FROM route_targets rt
		JOIN route_policies rp ON rp.org_id = rt.org_id AND rp.id = rt.route_id
		WHERE rt.org_id = ? AND rt.deployment_id = ?
		LIMIT 1`, orgID, depID,
	).Scan(&envID)
	if err == nil && envID != "" {
		return envID
	}
	return ""
}

func (s *Server) loadDeploymentDTO(ctx context.Context, orgID, depID string) (modelDeploymentDTO, sql.NullString, string, error) {
	var (
		dto           modelDeploymentDTO
		pricingJSON   string
		ownerProj     sql.NullString
		sharedJSON    string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT md.id, md.logical_model_id, md.connection_id, md.upstream_model_name,
		       md.pricing_nano_usd_json, md.state, md.idle_chunk_timeout_ms,
		       md.first_byte_timeout_ms, md.version,
		       pc.data_region, pc.owner_project_id, pc.shared_project_ids_json
		FROM model_deployments md
		JOIN provider_connections pc ON pc.org_id = md.org_id AND pc.id = md.connection_id
		WHERE md.org_id = ? AND md.id = ?`, orgID, depID,
	).Scan(&dto.ID, &dto.LogicalModelID, &dto.ConnectionID, &dto.UpstreamModelName, &pricingJSON, &dto.State, &dto.IdleChunkTimeoutMs, &dto.FirstByteTimeoutMs, &dto.Version, &dto.DataRegion, &ownerProj, &sharedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return modelDeploymentDTO{}, ownerProj, "", port.ErrNotFound
	}
	if err != nil {
		return modelDeploymentDTO{}, ownerProj, "", err
	}
	envID, rc := decodeStoredPricingDocument(pricingJSON)
	if envID == "" {
		envID = s.resolveDeploymentEnvID(ctx, orgID, depID)
	}
	dto.EnvID = envID
	dto.RateCard = rc
	return dto, ownerProj, sharedJSON, nil
}

func (s *Server) appendChangelogAndAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID *string,
	entityKind, entityID, mutationType string,
	emergency bool,
	actorID, eventType string,
	meta map[string]any,
	reqID string,
	nowMs int64,
) error {
	var orgArg any
	if orgID != nil && *orgID != "" {
		orgArg = *orgID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?)`,
		orgArg, entityKind, entityID, mutationType, boolToInt(emergency), nowMs,
	); err != nil {
		return err
	}
	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	rawMeta, _ := json.Marshal(meta)
	return s.txAudit.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  nowMs,
		OrgID:        orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      actorID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    eventType,
		TargetType:   entityKind,
		TargetID:     entityID,
		RequestID:    ptrOrNil(reqID),
		MetadataJSON: string(rawMeta),
	})
}

func toLogicalModelDTO(id, slug, displayName string, ctxWin, maxOut int64, assurance string, caps map[string]string) logicalModelDTO {
	if displayName == "" {
		displayName = slug
	}
	profile := capabilityProfileDTO{
		Streaming:         caps["streaming"] != "",
		ToolCalling:       caps["tool_calling"] != "",
		ParallelToolCalls: caps["parallel_tool_calls"] != "",
		StrictJSONSchema:  caps["strict_json_schema"] != "",
		JSONObject:        caps["json_object"] != "",
		Vision:            caps["vision"] != "",
		AudioInput:        caps["audio_input"] != "",
		DocumentInput:     caps["document_input"] != "",
		Reasoning:         caps["reasoning"] != "",
		PromptCaching:     caps["prompt_caching"] != "",
		Embeddings:        caps["embeddings"] != "",
		Rerank:            caps["rerank"] != "",
	}
	if assurance == "" {
		assurance = deriveModelAssuranceTier(caps)
	}
	return logicalModelDTO{
		ID:              id,
		Slug:            slug,
		DisplayName:     displayName,
		ContextWindow:   ctxWin,
		MaxOutputTokens: maxOut,
		AssuranceTier:   assurance,
		Capabilities:    profile,
	}
}

func deriveModelAssuranceTier(caps map[string]string) string {
	bestRank := 0
	tier := catalog.AssuranceDeclared
	for _, v := range caps {
		rank := 0
		switch v {
		case catalog.AssuranceVerified:
			rank = 3
		case catalog.AssuranceTested:
			rank = 2
		case catalog.AssuranceDeclared:
			rank = 1
		}
		if rank > bestRank {
			bestRank = rank
			tier = v
		}
	}
	return tier
}

func capabilityProfileToMap(p capabilityProfileDTO, assurance string) map[string]string {
	m := make(map[string]string)
	if p.Streaming {
		m["streaming"] = assurance
	}
	if p.ToolCalling {
		m["tool_calling"] = assurance
	}
	if p.ParallelToolCalls {
		m["parallel_tool_calls"] = assurance
	}
	if p.StrictJSONSchema {
		m["strict_json_schema"] = assurance
	}
	if p.JSONObject {
		m["json_object"] = assurance
	}
	if p.Vision {
		m["vision"] = assurance
	}
	if p.AudioInput {
		m["audio_input"] = assurance
	}
	if p.DocumentInput {
		m["document_input"] = assurance
	}
	if p.Reasoning {
		m["reasoning"] = assurance
	}
	if p.PromptCaching {
		m["prompt_caching"] = assurance
	}
	if p.Embeddings {
		m["embeddings"] = assurance
	}
	if p.Rerank {
		m["rerank"] = assurance
	}
	return m
}

func validateRateCardNonNegative(rc rateCardNanoUSDDTO) bool {
	return rc.InputTokenNanoUSD >= 0 &&
		rc.OutputTokenNanoUSD >= 0 &&
		rc.CacheReadTokenNanoUSD >= 0 &&
		rc.CacheWriteTokenNanoUSD >= 0 &&
		rc.ReasoningTokenNanoUSD >= 0 &&
		rc.ImageUnitNanoUSD >= 0 &&
		rc.AudioSecNanoUSD >= 0 &&
		rc.DocumentPageNanoUSD >= 0
}

func encodeStoredPricingDocument(envID string, rc rateCardNanoUSDDTO) storedPricingDocument {
	return storedPricingDocument{
		EnvID:                    envID,
		InputPer1MNanoUSD:        rc.InputTokenNanoUSD,
		OutputPer1MNanoUSD:       rc.OutputTokenNanoUSD,
		CacheReadPer1MNanoUSD:    rc.CacheReadTokenNanoUSD,
		CacheWritePer1MNanoUSD:   rc.CacheWriteTokenNanoUSD,
		ReasoningPer1MNanoUSD:    rc.ReasoningTokenNanoUSD,
		ImagePerUnitNanoUSD:      rc.ImageUnitNanoUSD,
		AudioInputPerSecNanoUSD:  rc.AudioSecNanoUSD,
		DocumentPageNanoUSD:      rc.DocumentPageNanoUSD,
		InputTokenNanoUSD:        rc.InputTokenNanoUSD,
		OutputTokenNanoUSD:       rc.OutputTokenNanoUSD,
		CacheReadTokenNanoUSD:    rc.CacheReadTokenNanoUSD,
		CacheWriteTokenNanoUSD:   rc.CacheWriteTokenNanoUSD,
		ReasoningTokenNanoUSD:    rc.ReasoningTokenNanoUSD,
		ImageUnitNanoUSD:         rc.ImageUnitNanoUSD,
		AudioSecNanoUSD:          rc.AudioSecNanoUSD,
		DocumentPageTokenNanoUSD: rc.DocumentPageNanoUSD,
		SelfHostedZeroRate:       rc.SelfHostedZeroRate,
	}
}

func decodeStoredPricingDocument(raw string) (string, rateCardNanoUSDDTO) {
	var doc storedPricingDocument
	_ = json.Unmarshal([]byte(raw), &doc)
	rc := rateCardNanoUSDDTO{
		InputTokenNanoUSD:      coalescePositiveInt64(doc.InputTokenNanoUSD, doc.InputPer1MNanoUSD),
		OutputTokenNanoUSD:     coalescePositiveInt64(doc.OutputTokenNanoUSD, doc.OutputPer1MNanoUSD),
		CacheReadTokenNanoUSD:  coalescePositiveInt64(doc.CacheReadTokenNanoUSD, doc.CacheReadPer1MNanoUSD),
		CacheWriteTokenNanoUSD: coalescePositiveInt64(doc.CacheWriteTokenNanoUSD, doc.CacheWritePer1MNanoUSD),
		ReasoningTokenNanoUSD:  coalescePositiveInt64(doc.ReasoningTokenNanoUSD, doc.ReasoningPer1MNanoUSD),
		ImageUnitNanoUSD:       coalescePositiveInt64(doc.ImageUnitNanoUSD, doc.ImagePerUnitNanoUSD),
		AudioSecNanoUSD:        coalescePositiveInt64(doc.AudioSecNanoUSD, doc.AudioInputPerSecNanoUSD),
		DocumentPageNanoUSD:    coalescePositiveInt64(doc.DocumentPageTokenNanoUSD, doc.DocumentPageNanoUSD),
		SelfHostedZeroRate:     doc.SelfHostedZeroRate,
	}
	return strings.TrimSpace(doc.EnvID), rc
}

func coalescePositiveInt64(primary, fallback int64) int64 {
	if primary != 0 {
		return primary
	}
	return fallback
}

func connectionVisibleToAnyProject(ownerProj sql.NullString, sharedJSON string, allowed map[string]bool) bool {
	if ownerProj.Valid && ownerProj.String != "" {
		if allowed[ownerProj.String] {
			return true
		}
	} else {
		var shared []string
		if strings.TrimSpace(sharedJSON) != "" {
			_ = json.Unmarshal([]byte(sharedJSON), &shared)
		}
		if len(shared) == 0 {
			return true
		}
	}
	var shared []string
	if strings.TrimSpace(sharedJSON) != "" {
		_ = json.Unmarshal([]byte(sharedJSON), &shared)
	}
	for _, pid := range shared {
		if allowed[pid] {
			return true
		}
	}
	return false
}
