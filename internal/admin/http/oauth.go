// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/security/oauth"
)

type oauthStartRequest struct {
	Flow                        string   `json:"flow"`
	AuthorizationEndpoint       string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint               string   `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint,omitempty"`
	ClientID                    string   `json:"client_id"`
	Scopes                      []string `json:"scopes,omitempty"`
	Resource                    string   `json:"resource"`
}

func (s *Server) handleStartConnectionOAuth(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "oauth_unavailable", "server_error", "OAuth service is unavailable", true, nil)
		return
	}
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	connID := strings.TrimSpace(r.PathValue("connectionId"))
	var projectID sql.NullString
	err := s.db.QueryRowContext(r.Context(), `SELECT owner_project_id FROM provider_connections WHERE org_id=? AND id=?`, orgID, connID).Scan(&projectID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "connection not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: nullableSQLString(projectID)}) {
		return
	}
	var req oauthStartRequest
	if !decodePublicJSON(w, r, &req) {
		return
	}
	result, err := s.oauth.Start(r.Context(), oauth.StartInput{OrgID: orgID, PrincipalID: sc.Actor.PrincipalID, ConnectionID: connID, Flow: req.Flow, AuthorizationEndpoint: req.AuthorizationEndpoint, TokenEndpoint: req.TokenEndpoint, DeviceAuthorizationURL: req.DeviceAuthorizationEndpoint, ClientID: req.ClientID, Scopes: req.Scopes, Resource: req.Resource})
	if err != nil {
		writeOAuthError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "oauth_unavailable", "server_error", "OAuth service is unavailable", true, nil)
		return
	}
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	state, code := strings.TrimSpace(r.URL.Query().Get("state")), strings.TrimSpace(r.URL.Query().Get("code"))
	if state == "" || len(state) > 128 || code == "" || len(code) > 8192 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_oauth_state", "invalid_request_error", "OAuth authorization state is invalid or expired", false, nil)
		return
	}
	var projectID sql.NullString
	err := s.db.QueryRowContext(r.Context(), `SELECT project_id FROM oauth_authorization_sessions WHERE org_id=? AND principal_id=? AND state=? AND grant_type='AUTHORIZATION_CODE_PKCE' AND session_status='PENDING' AND expires_at_ms>?`, orgID, sc.Actor.PrincipalID, state, s.nowMs()).Scan(&projectID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_oauth_state", "invalid_request_error", "OAuth authorization state is invalid or expired", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: nullableSQLString(projectID)}) {
		return
	}
	if err := s.oauth.Callback(r.Context(), orgID, sc.Actor.PrincipalID, state, code); err != nil {
		writeOAuthError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/providers/connections?oauth=connected", http.StatusFound)
}

func (s *Server) handleOAuthDevicePoll(w http.ResponseWriter, r *http.Request) {
	if s.oauth == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "oauth_unavailable", "server_error", "OAuth service is unavailable", true, nil)
		return
	}
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	var req struct {
		State string `json:"state"`
	}
	if !decodePublicJSON(w, r, &req) {
		return
	}
	var projectID sql.NullString
	err := s.db.QueryRowContext(r.Context(), `SELECT project_id FROM oauth_authorization_sessions WHERE org_id=? AND principal_id=? AND state=? AND grant_type='DEVICE_CODE' AND session_status='PENDING' AND expires_at_ms>?`, orgID, sc.Actor.PrincipalID, req.State, s.nowMs()).Scan(&projectID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_oauth_state", "invalid_request_error", "OAuth authorization state is invalid or expired", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: nullableSQLString(projectID)}) {
		return
	}
	result, err := s.oauth.PollDevice(r.Context(), oauth.PollInput{OrgID: orgID, PrincipalID: sc.Actor.PrincipalID, State: req.State})
	if err != nil && !errors.Is(err, oauth.ErrAuthorizationPending) {
		writeOAuthError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if errors.Is(err, oauth.ErrAuthorizationPending) {
		writeJSON(w, http.StatusAccepted, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func nullableSQLString(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

func writeOAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, oauth.ErrInvalidOAuthState):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_oauth_state", "invalid_request_error", "OAuth authorization state is invalid or expired", false, nil)
	case errors.Is(err, oauth.ErrSlowDown):
		writeErrorEnvelope(w, r, http.StatusTooManyRequests, "oauth_slow_down", "rate_limit_error", "OAuth device polling interval has not elapsed", true, nil)
	case errors.Is(err, oauth.ErrAuthorizationPending):
		writeErrorEnvelope(w, r, http.StatusAccepted, "authorization_pending", "invalid_request_error", "OAuth authorization is pending", true, nil)
	case errors.Is(err, oauth.ErrInvalidOAuthInput):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_oauth_request", "invalid_request_error", "OAuth request is invalid", false, nil)
	default:
		writeErrorEnvelope(w, r, http.StatusBadGateway, "oauth_upstream_error", "upstream_error", "OAuth authorization could not be completed", false, nil)
	}
}
