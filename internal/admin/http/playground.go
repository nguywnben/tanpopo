// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
)

const (
	defaultPlaygroundTTLSeconds = 900
	minPlaygroundTTLSeconds     = 60
	maxPlaygroundTTLSeconds     = 900
	defaultPlaygroundBudget     = int64(500_000)
	minPlaygroundBudget         = int64(1_000)
	maxPlaygroundBudget         = int64(5_000_000)
)

type playgroundSessionCreateRequest struct {
	ProjectID       string `json:"project_id"`
	EnvID           string `json:"env_id"`
	TTLSeconds      *int64 `json:"ttl_seconds"`
	MaxBudgetMicros *int64 `json:"max_budget_micros"`
}

type playgroundSessionResponse struct {
	SessionID           string `json:"session_id"`
	EphemeralVirtualKey string `json:"ephemeral_virtual_key"`
	EnvID               string `json:"env_id"`
	ExpiresAt           string `json:"expires_at"`
	MaxBudgetMicros     int64  `json:"max_budget_micros"`
}

func (s *Server) handleCreatePlaygroundSession(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req playgroundSessionCreateRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(req.ProjectID)
	envID := strings.TrimSpace(req.EnvID)
	if projectID == "" || envID == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "project_id and env_id are required", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionPlaygroundCreate, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	var envProjectID string
	err := s.db.QueryRowContext(r.Context(), `SELECT project_id FROM environments WHERE org_id=? AND id=?`, orgID, envID).Scan(&envProjectID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && envProjectID != projectID) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found in the selected project", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating playground environment", true, nil)
		return
	}
	if s.ledgers == nil || s.vkeys == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "playground_unavailable", "upstream_error", "cost-capped playground sessions are unavailable", true, nil)
		return
	}
	ttl := int64(defaultPlaygroundTTLSeconds)
	if req.TTLSeconds != nil {
		ttl = *req.TTLSeconds
	}
	if ttl < minPlaygroundTTLSeconds || ttl > maxPlaygroundTTLSeconds {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "ttl_seconds must be between 60 and 900", false, nil)
		return
	}
	budget := defaultPlaygroundBudget
	if req.MaxBudgetMicros != nil {
		budget = *req.MaxBudgetMicros
	}
	if budget < minPlaygroundBudget || budget > maxPlaygroundBudget {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "max_budget_micros must be between 1000 and 5000000", false, nil)
		return
	}
	now := s.nowMs()
	expiresAtMs := now + ttl*1000
	ownerID := sc.Actor.PrincipalID
	created, err := s.vkeys.Create(r.Context(), vkey.CreateParams{
		OrgID: orgID, ProjectID: projectID, EnvID: envID, OwnerPrincipalID: &ownerID,
		Name: "Playground session", MaxConcurrentStreams: 4, IsEphemeralPlayground: true,
		ExpiresAtMs: &expiresAtMs, ActorID: ownerID,
	})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating ephemeral playground key", true, nil)
		return
	}
	if _, err = s.ledgers.UpsertLedger(r.Context(), ledger.UpsertLedgerParams{
		OrgID: orgID, ScopeType: ledger.ScopeVirtualKey, ScopeID: created.Summary.ID,
		PeriodKind: ledger.PeriodTotalLifetime, PeriodStartMs: now, PeriodEndMs: expiresAtMs,
		HardLimitMicros: budget,
	}); err != nil {
		_, _ = s.vkeys.RevokeWithReceipt(r.Context(), vkey.RevokeParams{
			OrgID: orgID, KeyID: created.Summary.ID, ExpectedVersion: created.Summary.Version,
			Mode: vkey.RevokeModeStandard, Reason: "rollback playground key after budget ledger creation failure", ActorID: ownerID,
		})
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed provisioning playground budget ceiling", true, nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, playgroundSessionResponse{
		SessionID: created.Summary.ID, EphemeralVirtualKey: created.SecretKey, EnvID: envID,
		ExpiresAt: time.UnixMilli(expiresAtMs).UTC().Format(time.RFC3339), MaxBudgetMicros: budget,
	})
}
