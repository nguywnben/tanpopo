// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type routeTargetDTO struct {
	DeploymentID  string `json:"deployment_id"`
	PriorityOrder int    `json:"priority_order,omitempty"`
	Weight        int    `json:"weight,omitempty"`
}

type routeUpsertRequest struct {
	Slug               string           `json:"slug"`
	EnvID              string           `json:"env_id"`
	Strategy           string           `json:"strategy"`
	AllowedDataRegions []string         `json:"allowed_data_regions"`
	MaxFailoverHops    *int             `json:"max_failover_hops"`
	RequestTimeoutMs   *int             `json:"request_timeout_ms"`
	Targets            []routeTargetDTO `json:"targets"`
}

type routeResourceDTO struct {
	ID                 string           `json:"id"`
	Slug               string           `json:"slug"`
	EnvID              string           `json:"env_id"`
	Strategy           string           `json:"strategy"`
	AllowedDataRegions []string         `json:"allowed_data_regions"`
	MaxFailoverHops    int              `json:"max_failover_hops"`
	RequestTimeoutMs   int              `json:"request_timeout_ms"`
	Targets            []routeTargetDTO `json:"targets"`
	Version            int64            `json:"version"`
	UpdatedAt          string           `json:"updated_at"`
}

type routeExplainRequestDTO struct {
	EnvID                string   `json:"env_id"`
	TargetModel          string   `json:"target_model"`
	DataRegion           string   `json:"data_region"`
	Dialect              string   `json:"dialect"`
	RequiredCapabilities []string `json:"required_capabilities"`
	EstimatedInputTokens int64    `json:"estimated_input_tokens"`
	MaxOutputTokens      int64    `json:"max_output_tokens"`
}

func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	envFilter := strings.TrimSpace(r.URL.Query().Get("env_id"))
	var scopeProj string
	if envFilter != "" {
		pid, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envFilter)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment filter", true, nil)
			return
		}
		if !found {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
			return
		}
		scopeProj = pid
		if !s.authorize(w, r, sc, rbac.ActionRouteRead, rbac.TargetScope{OrgID: orgID, ProjectID: scopeProj}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionRouteRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("routes|" + envFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "routes", orgID, filterHash)
	if !ok {
		return
	}

	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	projIDs := actorProjectIDs(sc.Actor, orgID)

	var q strings.Builder
	args := []any{orgID}
	q.WriteString(`
		SELECT id, project_id, env_id, route_slug, strategy, max_failover_hops,
		       required_region, version, created_at_ms, updated_at_ms
		FROM route_policies
		WHERE org_id = ?`)
	if envFilter != "" {
		q.WriteString(` AND env_id = ?`)
		args = append(args, envFilter)
	}
	if !orgWide {
		if len(projIDs) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[routeResourceDTO]{Data: []routeResourceDTO{}, HasMore: false})
			return
		}
		q.WriteString(` AND project_id IN (` + strings.Repeat("?,", len(projIDs)-1) + `?)`)
		for _, pid := range projIDs {
			args = append(args, pid)
		}
	}
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), q.String(), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing routes", true, nil)
		return
	}
	defer rows.Close()

	type rteRow struct {
		dto       routeResourceDTO
		createdMs int64
	}
	var items []rteRow
	for rows.Next() {
		var (
			id, projID, envID, slug, strategy string
			maxHops                           int
			reqRegion                         sql.NullString
			ver, createdMs, updatedMs         int64
		)
		if scanErr := rows.Scan(&id, &projID, &envID, &slug, &strategy, &maxHops, &reqRegion, &ver, &createdMs, &updatedMs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning route", true, nil)
			return
		}
		_ = projID
		regions := []string{}
		if reqRegion.Valid && reqRegion.String != "" {
			regions = []string{reqRegion.String}
		}
		targets, terr := s.loadRouteTargets(r.Context(), orgID, id)
		if terr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading route targets", true, nil)
			return
		}
		items = append(items, rteRow{
			dto: routeResourceDTO{
				ID:                 id,
				Slug:               slug,
				EnvID:              envID,
				Strategy:           mapRouteStrategyOpenAPI(strategy),
				AllowedDataRegions: regions,
				MaxFailoverHops:    maxHops,
				RequestTimeoutMs:   60000,
				Targets:            targets,
				Version:            ver,
				UpdatedAt:          formatTimeRFC3339Ms(updatedMs),
			},
			createdMs: createdMs,
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating routes", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]routeResourceDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "routes", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[routeResourceDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req routeUpsertRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	norm, ok := s.validateRouteUpsert(w, r, sc, orgID, &req, "")
	if !ok {
		return
	}

	routeID, err := idgen.New(idgen.PrefixRoute)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting route id", true, nil)
		return
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting route tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	var reqRegArg any
	if norm.RequiredRegion != "" {
		reqRegArg = norm.RequiredRegion
	}
	if _, err := tx.ExecContext(r.Context(), `
		INSERT INTO route_policies (
			id, org_id, project_id, env_id, route_slug, strategy,
			max_failover_hops, required_region, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		routeID, orgID, norm.ProjectID, norm.EnvID, norm.Slug, norm.Strategy,
		norm.MaxFailoverHops, reqRegArg, now, now,
	); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "route slug already exists in environment", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	for _, tgt := range norm.Targets {
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight)
			VALUES (?, ?, ?, ?, ?)`,
			orgID, routeID, tgt.DeploymentID, tgt.PriorityOrder, tgt.Weight,
		); err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		}
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "route_policy", routeID, port.MutationUpsert, false, sc.Actor.PrincipalID, "route.created", map[string]any{
		"route_slug": norm.Slug,
		"env_id":     norm.EnvID,
		"strategy":   norm.Strategy,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording route audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing route", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	regions := []string{}
	if norm.RequiredRegion != "" {
		regions = []string{norm.RequiredRegion}
	}
	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, routeResourceDTO{
		ID:                 routeID,
		Slug:               norm.Slug,
		EnvID:              norm.EnvID,
		Strategy:           norm.Strategy,
		AllowedDataRegions: regions,
		MaxFailoverHops:    norm.MaxFailoverHops,
		RequestTimeoutMs:   norm.RequestTimeoutMs,
		Targets:            norm.Targets,
		Version:            1,
		UpdatedAt:          formatTimeRFC3339Ms(now),
	})
}

func (s *Server) handleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	routeID := strings.TrimSpace(r.PathValue("routeId"))
	if routeID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "route not found", false, nil)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req routeUpsertRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	curDTO, curProjID, err := s.loadRouteResourceDTO(r.Context(), orgID, routeID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "route not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading route", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionRouteWrite, rbac.TargetScope{OrgID: orgID, ProjectID: curProjID}) {
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	norm, ok := s.validateRouteUpsert(w, r, sc, orgID, &req, curDTO.EnvID)
	if !ok {
		return
	}
	now := s.nowMs()

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting route update tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	var reqRegArg any
	if norm.RequiredRegion != "" {
		reqRegArg = norm.RequiredRegion
	}
	res, err := tx.ExecContext(r.Context(), `
		UPDATE route_policies
		SET route_slug = ?, strategy = ?, max_failover_hops = ?, required_region = ?,
		    version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`,
		norm.Slug, norm.Strategy, norm.MaxFailoverHops, reqRegArg, now, orgID, routeID, expectedVer,
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "route slug already exists in environment", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		_ = tx.Rollback()
		latest, _, _ := s.loadRouteResourceDTO(r.Context(), orgID, routeID)
		writeOptimisticConflict(w, r, latest.Version, latest)
		return
	}

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM route_targets WHERE org_id = ? AND route_id = ?`, orgID, routeID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed replacing route targets", true, nil)
		return
	}
	for _, tgt := range norm.Targets {
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO route_targets (org_id, route_id, deployment_id, priority_order, weight)
			VALUES (?, ?, ?, ?, ?)`,
			orgID, routeID, tgt.DeploymentID, tgt.PriorityOrder, tgt.Weight,
		); err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		}
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "route_policy", routeID, port.MutationUpsert, false, sc.Actor.PrincipalID, "route.updated", map[string]any{
		"route_slug": norm.Slug,
		"env_id":     norm.EnvID,
		"strategy":   norm.Strategy,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording route update audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing route update", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	regions := []string{}
	if norm.RequiredRegion != "" {
		regions = []string{norm.RequiredRegion}
	}
	nextVer := expectedVer + 1
	w.Header().Set("ETag", FormatETag(nextVer))
	writeJSON(w, http.StatusOK, routeResourceDTO{
		ID:                 routeID,
		Slug:               norm.Slug,
		EnvID:              norm.EnvID,
		Strategy:           norm.Strategy,
		AllowedDataRegions: regions,
		MaxFailoverHops:    norm.MaxFailoverHops,
		RequestTimeoutMs:   norm.RequestTimeoutMs,
		Targets:            norm.Targets,
		Version:            nextVer,
		UpdatedAt:          formatTimeRFC3339Ms(now),
	})
}

func (s *Server) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	routeID := strings.TrimSpace(r.PathValue("routeId"))
	if routeID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "route not found", false, nil)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	curDTO, curProjID, err := s.loadRouteResourceDTO(r.Context(), orgID, routeID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "route not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading route", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionRouteWrite, rbac.TargetScope{OrgID: orgID, ProjectID: curProjID}) {
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting route delete tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM route_targets WHERE org_id = ? AND route_id = ?`, orgID, routeID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting route targets", true, nil)
		return
	}
	res, err := tx.ExecContext(r.Context(), `DELETE FROM route_policies WHERE org_id = ? AND id = ? AND version = ?`, orgID, routeID, expectedVer)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting route", true, nil)
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		_ = tx.Rollback()
		latest, _, _ := s.loadRouteResourceDTO(r.Context(), orgID, routeID)
		writeOptimisticConflict(w, r, latest.Version, latest)
		return
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "route_policy", routeID, port.MutationDelete, false, sc.Actor.PrincipalID, "route.deleted", map[string]any{
		"route_slug": curDTO.Slug,
		"env_id":     curDTO.EnvID,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording route delete audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing route deletion", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExplainRoute(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req routeExplainRequestDTO
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	envID := strings.TrimSpace(req.EnvID)
	targetModel := strings.TrimSpace(req.TargetModel)
	if envID == "" || targetModel == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "env_id and target_model are required", false, nil)
		return
	}
	projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment", true, nil)
		return
	}
	if !found {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionRouteExplain, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}

	if s.snapshots == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "snapshot_not_ready", "upstream_error", "configuration snapshot unavailable", true, nil)
		return
	}
	if err := s.snapshots.CheckServingReady(); err != nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "snapshot_not_ready", "upstream_error", "configuration snapshot unavailable", true, nil)
		return
	}
	snap := s.snapshots.Current()
	if snap == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "snapshot_not_ready", "upstream_error", "configuration snapshot unavailable", true, nil)
		return
	}

	effCfg, okCfg := snap.LookupEffectiveConfig(orgID, projID, envID)
	var effPol policyengine.EffectivePolicy
	if okCfg {
		effPol, err = policyengine.MergeEffectiveFromSnapshot(snap, effCfg, nil)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "policy_engine_unavailable", "upstream_error", "policy engine unavailable", true, nil)
			return
		}
	}

	estIn := req.EstimatedInputTokens
	if estIn == 0 {
		estIn = 512
	}
	maxOut := req.MaxOutputTokens
	if maxOut == 0 {
		maxOut = 1024
	}
	dialect := strings.TrimSpace(req.Dialect)
	if dialect == "" {
		dialect = "openai_compatible"
	}

	res := s.routeResolver.Explain(resolver.ResolveRequest{
		RequestID:            requestIDFromContext(r.Context()),
		Snapshot:             snap,
		OrgID:                orgID,
		ProjectID:            projID,
		EnvID:                envID,
		TargetModel:          targetModel,
		EffectivePolicy:      effPol,
		RequestedDataRegion:  req.DataRegion,
		Dialect:              dialect,
		RequiredCapabilities: req.RequiredCapabilities,
		EstimatedInputTokens: estIn,
		MaxOutputTokens:      maxOut,
		RequireTestedCaps:    effCfg != nil && effCfg.RequireTestedCaps,
		DryRun:               true,
	})
	res.DryRun = true
	writeJSON(w, http.StatusOK, res)
}

type validatedRouteUpsert struct {
	ProjectID        string
	EnvID            string
	Slug             string
	Strategy         string
	RequiredRegion   string
	MaxFailoverHops  int
	RequestTimeoutMs int
	Targets          []routeTargetDTO
}

func (s *Server) validateRouteUpsert(w http.ResponseWriter, r *http.Request, sc *authSessionContext, orgID string, req *routeUpsertRequest, lockedEnvID string) (*validatedRouteUpsert, bool) {
	slug := strings.TrimSpace(req.Slug)
	envID := strings.TrimSpace(req.EnvID)
	if lockedEnvID != "" && envID != "" && envID != lockedEnvID {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "env_id cannot be changed on existing route", false, nil)
		return nil, false
	}
	if envID == "" {
		envID = lockedEnvID
	}
	strategy := strings.ToUpper(strings.TrimSpace(req.Strategy))
	if slug == "" || envID == "" || len(req.Targets) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug, env_id, and at least one target are required", false, nil)
		return nil, false
	}
	switch strategy {
	case snapshot.RouteStrategyPriorityFailover, snapshot.RouteStrategyWeighted, snapshot.RouteStrategyLowestCost:
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "strategy must be PRIORITY_FAILOVER, WEIGHTED, or LOWEST_COST", false, nil)
		return nil, false
	}

	projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment", true, nil)
		return nil, false
	}
	if !found {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
		return nil, false
	}
	if !s.authorize(w, r, sc, rbac.ActionRouteWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return nil, false
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, projID) {
		return nil, false
	}

	maxHops := 3
	if req.MaxFailoverHops != nil {
		if *req.MaxFailoverHops < 0 || *req.MaxFailoverHops > 3 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "max_failover_hops must be between 0 and 3", false, nil)
			return nil, false
		}
		maxHops = *req.MaxFailoverHops
	}
	reqTimeout := 60000
	if req.RequestTimeoutMs != nil {
		if *req.RequestTimeoutMs < 500 || *req.RequestTimeoutMs > 300000 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "request_timeout_ms must be between 500 and 300000", false, nil)
			return nil, false
		}
		reqTimeout = *req.RequestTimeoutMs
	}

	allowedRegSet := make(map[string]bool)
	var reqRegion string
	for _, rawReg := range req.AllowedDataRegions {
		reg := strings.ToUpper(strings.TrimSpace(rawReg))
		if !isValidDataRegion(reg) {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "allowed_data_regions must contain EU, US, APAC, or ON_PREM", false, nil)
			return nil, false
		}
		allowedRegSet[reg] = true
		if reqRegion == "" {
			reqRegion = reg
		}
	}

	effPol, err := s.computeEffectivePolicySQL(r.Context(), orgID, projID, envID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed evaluating residency policy", true, nil)
		return nil, false
	}
	if len(allowedRegSet) > 0 && effPol.HasAllowedDataRegions {
		allowedAnc := make(map[string]bool, len(effPol.AllowedDataRegions))
		for _, r := range effPol.AllowedDataRegions {
			allowedAnc[r] = true
		}
		if !allowedAnc["*"] {
			for reg := range allowedRegSet {
				if !allowedAnc[reg] {
					writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "policy_relaxation_forbidden", "invalid_request_error", fmt.Sprintf("allowed_data_regions %s violates ancestor residency policy", reg), false, nil)
					return nil, false
				}
			}
		}
	}

	seenDeps := make(map[string]bool, len(req.Targets))
	normTargets := make([]routeTargetDTO, 0, len(req.Targets))
	weightSum := 0
	for i, tgt := range req.Targets {
		depID := strings.TrimSpace(tgt.DeploymentID)
		if depID == "" || seenDeps[depID] {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "each target requires a unique deployment_id", false, nil)
			return nil, false
		}
		seenDeps[depID] = true
		if tgt.Weight < 0 || tgt.Weight > 100 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target weight must be between 0 and 100", false, nil)
			return nil, false
		}
		prio := tgt.PriorityOrder
		if prio < 1 {
			prio = i + 1
		}
		weight := tgt.Weight
		if strategy != snapshot.RouteStrategyWeighted && weight == 0 {
			weight = 100
		}
		weightSum += tgt.Weight

		var depRegion string
		qerr := s.db.QueryRowContext(r.Context(), `
			SELECT pc.data_region
			FROM model_deployments md
			JOIN provider_connections pc ON pc.org_id = md.org_id AND pc.id = md.connection_id
			WHERE md.org_id = ? AND md.id = ?`, orgID, depID,
		).Scan(&depRegion)
		if errors.Is(qerr, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "target deployment not found", false, nil)
			return nil, false
		}
		if qerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking target deployment", true, nil)
			return nil, false
		}
		if len(allowedRegSet) > 0 && !allowedRegSet[depRegion] {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "residency_policy_violation", "invalid_request_error", fmt.Sprintf("deployment %s region %s violates route allowed_data_regions", depID, depRegion), false, nil)
			return nil, false
		}

		normTargets = append(normTargets, routeTargetDTO{
			DeploymentID:  depID,
			PriorityOrder: prio,
			Weight:        weight,
		})
	}
	if strategy == snapshot.RouteStrategyWeighted && weightSum != 100 {
		writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_weight_sum", "invalid_request_error", fmt.Sprintf("WEIGHTED route target weights must sum to 100 (got %d)", weightSum), false, nil)
		return nil, false
	}

	return &validatedRouteUpsert{
		ProjectID:        projID,
		EnvID:            envID,
		Slug:             slug,
		Strategy:         strategy,
		RequiredRegion:   reqRegion,
		MaxFailoverHops:  maxHops,
		RequestTimeoutMs: reqTimeout,
		Targets:          normTargets,
	}, true
}

func (s *Server) loadRouteTargets(ctx context.Context, orgID, routeID string) ([]routeTargetDTO, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT deployment_id, priority_order, weight
		FROM route_targets
		WHERE org_id = ? AND route_id = ?
		ORDER BY priority_order ASC, deployment_id ASC`, orgID, routeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := make([]routeTargetDTO, 0)
	for rows.Next() {
		var t routeTargetDTO
		if scanErr := rows.Scan(&t.DeploymentID, &t.PriorityOrder, &t.Weight); scanErr != nil {
			return nil, scanErr
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

func (s *Server) loadRouteResourceDTO(ctx context.Context, orgID, routeID string) (routeResourceDTO, string, error) {
	var (
		dto       routeResourceDTO
		projID    string
		reqRegion sql.NullString
		updatedMs int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, project_id, env_id, route_slug, strategy, max_failover_hops,
		       required_region, version, updated_at_ms
		FROM route_policies
		WHERE org_id = ? AND id = ?`, orgID, routeID,
	).Scan(&dto.ID, &projID, &dto.EnvID, &dto.Slug, &dto.Strategy, &dto.MaxFailoverHops, &reqRegion, &dto.Version, &updatedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return routeResourceDTO{}, "", port.ErrNotFound
	}
	if err != nil {
		return routeResourceDTO{}, "", err
	}
	dto.Strategy = mapRouteStrategyOpenAPI(dto.Strategy)
	dto.AllowedDataRegions = []string{}
	if reqRegion.Valid && reqRegion.String != "" {
		dto.AllowedDataRegions = []string{reqRegion.String}
	}
	dto.RequestTimeoutMs = 60000
	dto.UpdatedAt = formatTimeRFC3339Ms(updatedMs)
	targets, err := s.loadRouteTargets(ctx, orgID, routeID)
	if err != nil {
		return routeResourceDTO{}, "", err
	}
	dto.Targets = targets
	return dto, projID, nil
}

func mapRouteStrategyOpenAPI(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case snapshot.RouteStrategyWeightedRR, snapshot.RouteStrategyWeighted:
		return snapshot.RouteStrategyWeighted
	case snapshot.RouteStrategyLowestCost, snapshot.RouteStrategyLowestLatency:
		return snapshot.RouteStrategyLowestCost
	default:
		return snapshot.RouteStrategyPriorityFailover
	}
}
