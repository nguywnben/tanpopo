// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type webhookResource struct {
	ID           string   `json:"id"`
	OrgID        string   `json:"org_id"`
	ProjectID    *string  `json:"project_id"`
	TargetOrigin string   `json:"target_origin"` // Receiver paths and query strings may contain secrets.
	Events       []string `json:"events"`
	Enabled      bool     `json:"enabled"`
	Version      int64    `json:"version"`
	CreatedMs    int64    `json:"created_at_ms"`
}

func readWebhook(row governanceRow) (webhookResource, error) {
	var item webhookResource
	var rawURL, events string
	var enabled port.FlexBool
	err := row.Scan(&item.ID, &item.OrgID, &item.ProjectID, &rawURL, &events, &enabled, &item.Version, &item.CreatedMs)
	if err != nil {
		return item, err
	}
	item.Enabled = bool(enabled)
	u, err := url.Parse(rawURL)
	if err != nil {
		return item, err
	}
	item.TargetOrigin = u.Scheme + "://" + u.Host
	err = json.Unmarshal([]byte(events), &item.Events)
	return item, err
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	org, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	scope, args, ok := governanceListScope(w, r, sc, org, rbac.ActionWebhookRead, "project_id")
	if !ok {
		return
	}
	query := `SELECT id,org_id,project_id,target_url,event_types_json,enabled,version,created_at_ms
		FROM webhook_subscriptions WHERE org_id=?` + scope
	governancePage(s, w, r, "webhooks", org, "", query, "created_at_ms", "id", append([]any{org}, args...), readWebhook, func(v webhookResource) (int64, string) { return v.CreatedMs, v.ID })
}

func validWebhookEvents(events []string) bool {
	seen := map[string]bool{}
	if len(events) == 0 {
		return false
	}
	for _, e := range events {
		switch e {
		case "budget.threshold", "mcp.approval.pending", "extension.quarantined", "kill_switch.triggered":
		default:
			return false
		}
		if seen[e] {
			return false
		}
		seen[e] = true
	}
	return true
}

func (s *Server) validateWebhookTarget(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return nil, egress.ErrInvalidPolicy
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	policy := egress.Policy{AllowedHosts: []string{net.JoinHostPort(u.Hostname(), port)}, RequireTLS: true}
	if _, err = policy.ValidateTargetURL(raw); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = validateEndpointSSRF(ctx, raw, false, s.hostResolver, s.blockedPorts); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	org, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	var req struct {
		ProjectID    string   `json:"project_id"`
		TargetURL    string   `json:"target_url"`
		Events       []string `json:"events"`
		CredentialID string   `json:"signing_credential_id"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	// SCR-12 restricts subscription configuration to budget administrators in
	// the target scope. Both permissions must come from a binding in that scope.
	target := rbac.TargetScope{OrgID: org, ProjectID: req.ProjectID}
	if !s.authorize(w, r, sc, rbac.ActionWebhookWrite, target) || !s.authorize(w, r, sc, rbac.ActionBudgetWrite, target) {
		return
	}
	if !validWebhookEvents(req.Events) || strings.TrimSpace(req.CredentialID) == "" {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "a signing credential and a nonempty list of unique supported events are required", false, nil)
		return
	}
	if !s.requireActiveOrgAndProject(w, r, org, req.ProjectID) {
		return
	}
	targetURL, err := s.validateWebhookTarget(r.Context(), req.TargetURL)
	if err != nil {
		writeConnectionSSRFError(w, r, err)
		return
	}
	id, err := idgen.New(idgen.PrefixWebhook)
	if err != nil {
		mcpInternal(w, r, "failed generating subscription ID")
		return
	}
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		mcpInternal(w, r, "failed starting webhook registration")
		return
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(ctx, tx, s.dialect, org); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}
	var subscriptions int
	if err := tx.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM webhook_subscriptions WHERE org_id=? AND enabled=`+port.BoolExpr(s.dialect, true)), org).Scan(&subscriptions); err != nil {
		mcpInternal(w, r, "failed checking webhook quota")
		return
	}
	if subscriptions >= 100 {
		writeErrorEnvelope(w, r, 429, "webhook_subscription_limit", "rate_limit_error", "organization webhook subscription limit reached", false, nil)
		return
	}

	var audience *string
	credQuery := `SELECT bound_audience FROM secret_credentials WHERE org_id=? AND id=? AND secret_kind=?`
	credArgs := []any{org, req.CredentialID, vault.SecretKindWebhookHMAC}
	if strings.TrimSpace(req.ProjectID) == "" {
		credQuery += ` AND owner_project_id IS NULL`
	} else {
		credQuery += ` AND owner_project_id = ?`
		credArgs = append(credArgs, req.ProjectID)
	}
	err = tx.QueryRowContext(ctx, s.rebind(credQuery), credArgs...).Scan(&audience)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "signing credential unavailable in this scope", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed checking signing credential")
		return
	}
	if matches, _ := broker.AudienceMatches(targetURL, audience); !matches {
		writeErrorEnvelope(w, r, 400, "credential_audience_mismatch", "invalid_request_error", "signing credential is not bound to this target", false, nil)
		return
	}
	fullTarget := []byte(req.TargetURL)
	defer clear(fullTarget)
	var ownerProject *string
	if req.ProjectID != "" {
		ownerProject = &req.ProjectID
	}
	targetSecret, activateTargetSecret, err := s.vault.SealSecretTx(ctx, tx, vault.SealSecretParams{
		OrgID: org, OwnerProjectID: ownerProject, SecretKind: vault.SecretKindWebhookHMAC,
		BoundAudience: &req.TargetURL, Name: "webhook target " + id, Plaintext: fullTarget,
	})
	if err != nil {
		mcpInternal(w, r, "failed sealing webhook target")
		return
	}
	now := s.nowMs()
	events, err := json.Marshal(req.Events)
	if err != nil {
		mcpInternal(w, r, "failed encoding subscription events")
		return
	}
	_, err = tx.ExecContext(ctx, s.rebind(`INSERT INTO webhook_subscriptions(id,org_id,project_id,name,target_url,target_url_credential_id,signing_secret_credential_id,event_types_json,created_at_ms,updated_at_ms)
		VALUES(?,?,?,?,?,?,?,?,?,?)`), id, org, nullableString(req.ProjectID), id, targetURL.Scheme+"://"+targetURL.Host, targetSecret.ID, req.CredentialID, string(events), now, now)
	if err != nil {
		mcpInternal(w, r, "failed saving webhook registration")
		return
	}
	err = s.appendMCPAuditTx(r, tx, org, sc.Actor.PrincipalID, "webhook.registered", "webhook_subscription", id, nil)
	if err != nil || tx.Commit() != nil {
		mcpInternal(w, r, "failed committing webhook registration and audit")
		return
	}
	activateTargetSecret()
	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, webhookResource{
		ID: id, OrgID: org, ProjectID: ptrOrNil(req.ProjectID),
		TargetOrigin: targetURL.Scheme + "://" + targetURL.Host, Events: req.Events,
		Enabled: true, Version: 1, CreatedMs: now,
	})
}

func (s *Server) loadWebhookForMutation(ctx context.Context, orgID, subID string) (webhookResource, string, string, error) {
	var (
		res       webhookResource
		rawURL    string
		eventsRaw string
		projID    sql.NullString
		targetSec string
		sigSec    string
		enabled   port.FlexBool
	)
	err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(ctx, s.rebind(`
			SELECT id, org_id, project_id, target_url, event_types_json, enabled, version, created_at_ms, target_url_credential_id, signing_secret_credential_id
			FROM webhook_subscriptions
			WHERE org_id = ? AND id = ?`),
			orgID, subID,
		).Scan(&res.ID, &res.OrgID, &projID, &rawURL, &eventsRaw, &enabled, &res.Version, &res.CreatedMs, &targetSec, &sigSec)
	})
	if err != nil {
		return res, "", "", err
	}
	res.ProjectID = sqlNullStrPtr(projID)
	res.Enabled = bool(enabled)
	if u, parseErr := url.Parse(rawURL); parseErr == nil && u.Host != "" {
		res.TargetOrigin = u.Scheme + "://" + u.Host
	} else {
		res.TargetOrigin = rawURL
	}
	if err := json.Unmarshal([]byte(eventsRaw), &res.Events); err != nil {
		return res, "", "", err
	}
	return res, targetSec, sigSec, nil
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	subID := strings.TrimSpace(r.PathValue("subscriptionId"))
	if subID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "webhook subscription not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionWebhookWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool     `json:"enabled"`
		Events  *[]string `json:"events"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if req.Enabled == nil && req.Events == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "enabled or events is required", false, nil)
		return
	}
	if req.Events != nil && !validWebhookEvents(*req.Events) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "events must be a nonempty list of unique supported events", false, nil)
		return
	}

	cur, _, _, err := s.loadWebhookForMutation(r.Context(), orgID, subID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "webhook subscription not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading webhook subscription")
		return
	}
	scopeProj := ""
	if cur.ProjectID != nil {
		scopeProj = *cur.ProjectID
	}
	target := rbac.TargetScope{OrgID: orgID, ProjectID: scopeProj}
	if !s.authorize(w, r, sc, rbac.ActionWebhookWrite, target) || !s.authorize(w, r, sc, rbac.ActionBudgetWrite, target) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, scopeProj) {
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}

	nextEnabled := cur.Enabled
	if req.Enabled != nil {
		nextEnabled = *req.Enabled
	}
	nextEvents := cur.Events
	if req.Events != nil {
		nextEvents = append([]string(nil), (*req.Events)...)
	}
	eventsJSON, err := json.Marshal(nextEvents)
	if err != nil {
		mcpInternal(w, r, "failed encoding subscription events")
		return
	}

	now := s.nowMs()
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		mcpInternal(w, r, "failed starting webhook update")
		return
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, s.dialect, orgID); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}

	resUpd, err := tx.ExecContext(ctx, s.rebind(`
		UPDATE webhook_subscriptions
		SET enabled = ?, event_types_json = ?, version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`),
		port.BoolVal(s.dialect, nextEnabled), string(eventsJSON), now, orgID, subID, expectedVer,
	)
	if err != nil {
		mcpInternal(w, r, "failed updating webhook subscription")
		return
	}
	if affected, _ := resUpd.RowsAffected(); affected == 0 {
		_ = tx.Rollback()
		fresh, _, _, ferr := s.loadWebhookForMutation(ctx, orgID, subID)
		if errors.Is(ferr, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "webhook subscription not found", false, nil)
			return
		}
		if ferr != nil {
			mcpInternal(w, r, "failed reloading webhook subscription")
			return
		}
		writeOptimisticConflict(w, r, fresh.Version, fresh)
		return
	}

	var canceledCount int64
	if !nextEnabled {
		resCancel, cerr := tx.ExecContext(ctx, s.rebind(`
			DELETE FROM webhook_deliveries
			WHERE org_id = ? AND subscription_id = ? AND status IN ('PENDING', 'RETRY_SCHEDULED')`),
			orgID, subID,
		)
		if cerr != nil {
			mcpInternal(w, r, "failed clearing pending webhook deliveries")
			return
		}
		canceledCount, _ = resCancel.RowsAffected()
	}

	evType := "webhook.updated"
	if cur.Enabled && !nextEnabled {
		evType = "webhook.disabled"
	} else if !cur.Enabled && nextEnabled {
		evType = "webhook.enabled"
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, evType, "webhook_subscription", subID, map[string]any{
		"enabled":            nextEnabled,
		"events":             nextEvents,
		"canceled_queued":    canceledCount,
	}); err != nil {
		mcpInternal(w, r, "failed recording webhook audit event")
		return
	}
	if err := tx.Commit(); err != nil {
		mcpInternal(w, r, "failed committing webhook update")
		return
	}

	updated := webhookResource{
		ID:           subID,
		OrgID:        orgID,
		ProjectID:    cur.ProjectID,
		TargetOrigin: cur.TargetOrigin,
		Events:       nextEvents,
		Enabled:      nextEnabled,
		Version:      expectedVer + 1,
		CreatedMs:    cur.CreatedMs,
	}
	w.Header().Set("ETag", FormatETag(updated.Version))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	subID := strings.TrimSpace(r.PathValue("subscriptionId"))
	if subID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "webhook subscription not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionWebhookWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	cur, targetSecID, signingSecID, err := s.loadWebhookForMutation(r.Context(), orgID, subID)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "webhook subscription not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading webhook subscription")
		return
	}
	scopeProj := ""
	if cur.ProjectID != nil {
		scopeProj = *cur.ProjectID
	}
	target := rbac.TargetScope{OrgID: orgID, ProjectID: scopeProj}
	if !s.authorize(w, r, sc, rbac.ActionWebhookWrite, target) || !s.authorize(w, r, sc, rbac.ActionBudgetWrite, target) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, scopeProj) {
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}

	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		mcpInternal(w, r, "failed starting webhook deletion")
		return
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, s.dialect, orgID); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}

	if _, err := tx.ExecContext(ctx, s.rebind(`
		DELETE FROM webhook_deliveries WHERE org_id = ? AND subscription_id = ?`),
		orgID, subID,
	); err != nil {
		mcpInternal(w, r, "failed removing webhook deliveries")
		return
	}
	resDel, err := tx.ExecContext(ctx, s.rebind(`
		DELETE FROM webhook_subscriptions WHERE org_id = ? AND id = ? AND version = ?`),
		orgID, subID, expectedVer,
	)
	if err != nil {
		mcpInternal(w, r, "failed deleting webhook subscription")
		return
	}
	if affected, _ := resDel.RowsAffected(); affected == 0 {
		_ = tx.Rollback()
		fresh, _, _, ferr := s.loadWebhookForMutation(ctx, orgID, subID)
		if errors.Is(ferr, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "webhook subscription not found", false, nil)
			return
		}
		if ferr != nil {
			mcpInternal(w, r, "failed reloading webhook subscription")
			return
		}
		writeOptimisticConflict(w, r, fresh.Version, fresh)
		return
	}
	if targetSecID != "" {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			DELETE FROM secret_credentials WHERE org_id = ? AND id = ?`),
			orgID, targetSecID,
		); err != nil {
			mcpInternal(w, r, "failed deleting webhook target credential")
			return
		}
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "webhook.deleted", "webhook_subscription", subID, map[string]any{
		"target_origin":         cur.TargetOrigin,
		"signing_credential_id": signingSecID,
	}); err != nil {
		mcpInternal(w, r, "failed recording webhook delete audit")
		return
	}
	if err := tx.Commit(); err != nil {
		mcpInternal(w, r, "failed committing webhook deletion")
		return
	}
	if s.vault != nil {
		s.vault.EvictAllDEKs()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"id":      subID,
	})
}
