// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"database/sql"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type a2aAgentCreateDTO struct {
	ProjectID         string           `json:"project_id"`
	AgentSlug         string           `json:"agent_slug"`
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	EndpointURL       string           `json:"endpoint_url"`
	AllowPrivateIP    bool             `json:"allow_private_ip"`
	AllowedCIDRs      []string         `json:"allowed_cidrs"`
	CredentialID      *string          `json:"credential_id"`
	DataRegion        string           `json:"data_region"`
	MaxDelegationHops int              `json:"max_delegation_hops"`
	Skills            []a2a.AgentSkill `json:"skills"`
	RequiredModels    []string         `json:"required_models"`
}

func (s *Server) a2aRegistry() (*a2a.Registry, error) {
	var blockedPorts []int
	for portNum, denied := range s.blockedPorts {
		if denied {
			blockedPorts = append(blockedPorts, portNum)
		}
	}
	return a2a.NewRegistry(a2a.RegistryConfig{
		DB:           s.db,
		Audit:        s.audit,
		Now:          func() time.Time { return time.UnixMilli(s.nowMs()).UTC() },
		HostResolver: s.hostResolver,
		BlockedPorts: blockedPorts,
	})
}

func (s *Server) handleListA2AAgents(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}
	filterHash := sha256HexString("a2a_agents")
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "a2a_agents", orgID, filterHash)
	if !ok {
		return
	}
	reg, err := s.a2aRegistry()
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initializing A2A registry", true, nil)
		return
	}
	var curMs *int64
	var curID *string
	if cur != nil {
		curMs = &cur.CreatedMs
		curID = &cur.LastID
	}
	page, err := reg.ListAdminAgentsPage(r.Context(), orgID, actorProjectIDs(sc.Actor, orgID), actorHasOrgScopeBinding(sc.Actor, orgID), limit, curMs, curID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing A2A agents", true, nil)
		return
	}
	var nextCursor *string
	if page.HasMore && len(page.Items) > 0 {
		tok, curErr := encodePaginationCursor(s.cursorSecret, "a2a_agents", orgID, filterHash, page.LastMs, page.LastID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": page.Items, "next_cursor": nextCursor, "has_more": page.HasMore})
}

func (s *Server) handleRegisterA2AAgent(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	var req a2aAgentCreateDTO
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Name = strings.TrimSpace(req.Name)
	req.EndpointURL = strings.TrimSpace(req.EndpointURL)
	if req.ProjectID == "" || req.Name == "" || req.EndpointURL == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "project_id, name, and endpoint_url are required", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: req.ProjectID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, req.ProjectID) {
		return
	}
	parsedEP, perr := url.Parse(req.EndpointURL)
	if perr != nil || parsedEP.Host == "" || (parsedEP.Scheme != "https" && parsedEP.Scheme != "http") {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_endpoint_url", "invalid_request_error", "endpoint_url must be a valid http(s) URL", false, nil)
		return
	}
	if parsedEP.Scheme != "https" && !req.AllowPrivateIP {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_endpoint_url", "invalid_request_error", "https endpoint_url is required unless allow_private_ip is true", false, nil)
		return
	}
	if req.AllowPrivateIP && len(req.AllowedCIDRs) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "allowed_cidrs is required when allow_private_ip is true", false, nil)
		return
	}
	normCIDRs := make([]string, 0, len(req.AllowedCIDRs))
	for _, rawCIDR := range req.AllowedCIDRs {
		pfx, cerr := netip.ParsePrefix(strings.TrimSpace(rawCIDR))
		if cerr != nil || !pfx.IsValid() || pfx.Masked().Bits() == 0 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "allowed_cidrs must contain valid non-wildcard CIDR prefixes", false, nil)
			return
		}
		normCIDRs = append(normCIDRs, pfx.Masked().String())
	}
	req.AllowedCIDRs = normCIDRs
	if req.CredentialID != nil && strings.TrimSpace(*req.CredentialID) != "" {
		credID := strings.TrimSpace(*req.CredentialID)
		var credExists int
		if err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
			return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM secret_credentials WHERE org_id=? AND id=? AND owner_project_id=?`), orgID, credID, req.ProjectID).Scan(&credExists)
		}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "credential not found", false, nil)
			} else {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating credential", true, nil)
			}
			return
		}
		req.CredentialID = &credID
	}
	if err := validateEndpointSSRF(r.Context(), req.EndpointURL, req.AllowPrivateIP, s.hostResolver, s.blockedPorts); err != nil {
		var egressErr *egress.Error
		if errors.As(err, &egressErr) && egressErr.HTTPStatus == http.StatusForbidden {
			writeErrorEnvelope(w, r, http.StatusForbidden, egressErr.Code, "permission_error", egressErr.Message, false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_endpoint_url", "invalid_request_error", err.Error(), false, nil)
		return
	}
	reg, err := s.a2aRegistry()
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initializing A2A registry", true, nil)
		return
	}
	ep, err := reg.RegisterAgent(r.Context(), a2a.RegisterAgentParams{
		OrgID:             orgID,
		ProjectID:         req.ProjectID,
		ActorPrincipalID:  sc.Actor.PrincipalID,
		AgentSlug:         req.AgentSlug,
		Name:              req.Name,
		Description:       req.Description,
		EndpointURL:       req.EndpointURL,
		AllowPrivateIP:    req.AllowPrivateIP,
		AllowedCIDRs:      req.AllowedCIDRs,
		CredentialID:      req.CredentialID,
		DataRegion:        req.DataRegion,
		MaxDelegationHops: req.MaxDelegationHops,
		Skills:            req.Skills,
		RequiredModels:    req.RequiredModels,
	})
	if err != nil {
		switch {
		case errors.Is(err, a2a.ErrAgentAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "agent_slug_conflict", "conflict_error", "agent_slug already registered in project", false, nil)
		case errors.Is(err, a2a.ErrInvalidAgentConfig):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed registering A2A agent", true, nil)
		}
		return
	}
	w.Header().Set("ETag", FormatETag(ep.Version))
	writeJSON(w, http.StatusCreated, ep.Card)
}

func (s *Server) handleListA2ATasks(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}
	filterHash := sha256HexString("a2a_tasks")
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "a2a_tasks", orgID, filterHash)
	if !ok {
		return
	}
	coord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		DB:    s.db,
		Audit: s.audit,
	})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initializing A2A coordinator", true, nil)
		return
	}
	var curMs *int64
	var curID *string
	if cur != nil {
		curMs = &cur.CreatedMs
		curID = &cur.LastID
	}
	page, err := coord.ListAdminTasksPage(r.Context(), orgID, actorProjectIDs(sc.Actor, orgID), actorHasOrgScopeBinding(sc.Actor, orgID), limit, curMs, curID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing A2A tasks", true, nil)
		return
	}
	var nextCursor *string
	if page.HasMore && len(page.Items) > 0 {
		tok, curErr := encodePaginationCursor(s.cursorSecret, "a2a_tasks", orgID, filterHash, page.LastMs, page.LastID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": page.Items, "next_cursor": nextCursor, "has_more": page.HasMore})
}

func (s *Server) getA2ACoordinator() (*a2a.TaskCoordinator, error) {
	if s.a2aCoordinator != nil {
		return s.a2aCoordinator, nil
	}
	return a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		Enabled:    true,
		DB:         s.db,
		Snapshots:  s.snapshots,
		Broker:     s.broker,
		Ledger:     s.ledgers,
		Audit:      s.audit,
		KillSwitch: s.killSwitch,
		Now:        func() time.Time { return time.UnixMilli(s.nowMs()).UTC() },
	})
}

func (s *Server) lookupA2ATaskProject(r *http.Request, orgID, taskID string) (string, bool) {
	var projID string
	err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`
			SELECT project_id FROM a2a_tasks WHERE org_id = ? AND id = ?`), orgID, taskID,
		).Scan(&projID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return projID, true
}

func (s *Server) handleResumeA2ATask(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	taskID := strings.TrimSpace(r.PathValue("taskId"))
	if taskID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "a2a task not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionMCPWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	projID, found := s.lookupA2ATaskProject(r, orgID, taskID)
	if !found {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "a2a task not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, projID) {
		return
	}

	var req a2a.ResumeTaskRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if len(req.InputParts) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "input_parts must be a non-empty array", false, nil)
		return
	}
	coord, err := s.getA2ACoordinator()
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initializing A2A coordinator", true, nil)
		return
	}
	view, err := coord.ResumeAdminTask(r.Context(), orgID, projID, taskID, sc.Actor.PrincipalID, req.InputParts)
	if err != nil {
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "a2a task not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "concurrent A2A task update; retry", false, nil)
		case errors.Is(err, a2a.ErrInvalidTaskStateMove):
			writeErrorEnvelope(w, r, http.StatusConflict, "task_not_resumable", "invalid_request_error", err.Error(), false, nil)
		case errors.Is(err, a2a.ErrPrivilegeOrBudget):
			writeErrorEnvelope(w, r, http.StatusForbidden, "budget_or_privilege_exceeded", "permission_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed resuming A2A task", true, nil)
		}
		return
	}
	w.Header().Set("ETag", FormatETag(view.Version))
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCancelA2ATask(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	taskID := strings.TrimSpace(r.PathValue("taskId"))
	if taskID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "a2a task not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionMCPWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	projID, found := s.lookupA2ATaskProject(r, orgID, taskID)
	if !found {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "a2a task not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, projID) {
		return
	}
	coord, err := s.getA2ACoordinator()
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initializing A2A coordinator", true, nil)
		return
	}
	view, err := coord.CancelAdminTask(r.Context(), orgID, projID, taskID, sc.Actor.PrincipalID)
	if err != nil {
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "a2a task not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "concurrent A2A task update; retry", false, nil)
		case errors.Is(err, a2a.ErrInvalidTaskStateMove):
			writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed canceling A2A task", true, nil)
		}
		return
	}
	w.Header().Set("ETag", FormatETag(view.Version))
	writeJSON(w, http.StatusOK, view)
}
