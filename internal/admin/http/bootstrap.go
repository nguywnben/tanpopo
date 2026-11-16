// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/bootstrap"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type bootstrapStatusResponse struct {
	Initialized bool   `json:"initialized"`
	OfflineMode bool   `json:"offline_mode"`
	Version     string `json:"version,omitempty"`
}

type bootstrapInitialConnectionReq struct {
	AdapterID      string `json:"adapter_id"`
	Name           string `json:"name"`
	BillingMode    string `json:"billing_mode"`
	EndpointURL    string `json:"endpoint_url"`
	AllowPrivateIP bool   `json:"allow_private_ip"`
	SecretValue    string `json:"secret_value"`
	ModelID        string `json:"model_id"`
	DataRegion     string `json:"data_region"`
}

type bootstrapCompleteRequest struct {
	BootstrapToken              string                         `json:"bootstrap_token"`
	OwnerEmail                  string                         `json:"owner_email"`
	OwnerPassword               string                         `json:"owner_password"`
	OrgName                     string                         `json:"org_name"`
	ProjectName                 string                         `json:"project_name"`
	MasterKEKBackupAcknowledged bool                           `json:"master_kek_backup_acknowledged"`
	InitialConnection           *bootstrapInitialConnectionReq `json:"initial_connection,omitempty"`
}

type bootstrapCompleteResponse struct {
	OrgID             string                   `json:"org_id"`
	ProjectID         string                   `json:"project_id"`
	EnvID             string                   `json:"env_id"`
	OwnerPrincipalID  string                   `json:"owner_principal_id"`
	CSRFToken         string                   `json:"csrf_token"`
	ConnectionID      string                   `json:"connection_id,omitempty"`
	DeploymentID      string                   `json:"deployment_id,omitempty"`
	DefaultRouteID    string                   `json:"default_route_id,omitempty"`
	InitialVirtualKey *virtualKeyCreatedDTO    `json:"initial_virtual_key,omitempty"`
}

func (s *Server) handleBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	initialized, err := s.bootstrap.IsBootstrapped(r.Context())
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking bootstrap status", true, nil)
		return
	}
	writeJSON(w, http.StatusOK, bootstrapStatusResponse{
		Initialized: initialized,
		OfflineMode: s.offlineMode,
		Version:     s.version,
	})
}

func (s *Server) handleBootstrapComplete(w http.ResponseWriter, r *http.Request) {
	var req bootstrapCompleteRequest
	if !decodePublicJSON(w, r, &req) {
		return
	}

	// Verify bootstrap completion status, token rate-limits, and Bootstrap Token
	// BEFORE probing initial_connection DNS/SSRF or provider_adapters, without burning the token.
	if err := s.bootstrap.PreflightCompleteRequest(r.Context(), bootstrap.CompleteRequest{
		BootstrapToken:              req.BootstrapToken,
		OwnerEmail:                  req.OwnerEmail,
		OwnerPassword:               req.OwnerPassword,
		OrgName:                     req.OrgName,
		ProjectName:                 req.ProjectName,
		MasterKEKBackupAcknowledged: req.MasterKEKBackupAcknowledged,
	}); err != nil {
		s.writeBootstrapCompleteError(w, r, err)
		return
	}

	var resolvedAdapterID string
	if req.InitialConnection != nil {
		ic := req.InitialConnection
		if strings.TrimSpace(ic.AdapterID) == "" || strings.TrimSpace(ic.Name) == "" || strings.TrimSpace(ic.ModelID) == "" {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_initial_connection", "invalid_request_error", "initial_connection requires adapter_id, name, billing_mode, endpoint_url, model_id, and data_region", false, nil)
			return
		}
		if ic.BillingMode != "COMMERCIAL_API" && ic.BillingMode != "SELF_HOSTED_INFRA" {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_billing_mode", "invalid_request_error", "billing_mode must be COMMERCIAL_API or SELF_HOSTED_INFRA", false, nil)
			return
		}
		if !isValidDataRegion(ic.DataRegion) {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_data_region", "invalid_request_error", "data_region must be one of EU, US, APAC, ON_PREM", false, nil)
			return
		}
		if ic.SecretValue != "" && s.vault == nil {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "vault_unavailable", "invalid_request_error", "vault is required to store initial_connection.secret_value", false, nil)
			return
		}

		// Validate endpoint SSRF rules BEFORE consuming the single-use Bootstrap Token.
		if ssrfErr := validateEndpointSSRF(r.Context(), ic.EndpointURL, ic.AllowPrivateIP, s.hostResolver, s.blockedPorts); ssrfErr != nil {
			code := "ssrf_blocked_private_ip"
			msg := "endpoint_url violates egress SSRF policy"
			if errors.Is(ssrfErr, egress.ErrPortDenied) {
				code = "egress_port_denied"
				msg = "endpoint_url targets a blocked local control-plane port"
			} else if errors.Is(ssrfErr, egress.ErrInvalidPolicy) || errors.Is(ssrfErr, port.ErrInvalidInput) {
				code = "invalid_endpoint_url"
				msg = ssrfErr.Error()
			}
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, code, "invalid_request_error", msg, false, nil)
			return
		}

		err := s.db.QueryRowContext(r.Context(), `
			SELECT id FROM provider_adapters WHERE id = ? OR adapter_slug = ? LIMIT 1`,
			strings.TrimSpace(ic.AdapterID), strings.TrimSpace(ic.AdapterID),
		).Scan(&resolvedAdapterID)
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "unknown_provider_adapter", "invalid_request_error", "initial_connection.adapter_id does not reference an installed provider adapter", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating initial_connection adapter", true, nil)
			return
		}
	}

	res, err := s.bootstrap.Complete(r.Context(), bootstrap.CompleteRequest{
		BootstrapToken:              req.BootstrapToken,
		OwnerEmail:                  req.OwnerEmail,
		OwnerPassword:               req.OwnerPassword,
		OrgName:                     req.OrgName,
		ProjectName:                 req.ProjectName,
		MasterKEKBackupAcknowledged: req.MasterKEKBackupAcknowledged,
	})
	if err != nil {
		s.writeBootstrapCompleteError(w, r, err)
		return
	}

	if s.vault != nil {
		if _, err := s.vault.EnsureOrgDEK(r.Context(), res.OrgID); err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed provisioning organization encryption key", true, nil)
			return
		}
		if _, err := s.vault.EnsureProjectDEK(r.Context(), res.OrgID, res.ProjectID); err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed provisioning project encryption key", true, nil)
			return
		}
	}

	resp := bootstrapCompleteResponse{
		OrgID:            res.OrgID,
		ProjectID:        res.ProjectID,
		EnvID:            res.EnvID,
		OwnerPrincipalID: res.OwnerPrincipalID,
	}

	if req.InitialConnection != nil {
		connID, deployID, routeID, createdKey, provErr := s.provisionInitialBootstrapTopology(
			r.Context(),
			res.OrgID,
			res.ProjectID,
			res.EnvID,
			res.OwnerPrincipalID,
			resolvedAdapterID,
			req.InitialConnection,
		)
		if provErr != nil {
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "initial_connection_failed", "invalid_request_error", provErr.Error(), false, nil)
			return
		}
		resp.ConnectionID = connID
		resp.DeploymentID = deployID
		resp.DefaultRouteID = routeID
		if createdKey != nil {
			dto := toVirtualKeyCreatedDTO(createdKey)
			resp.InitialVirtualKey = &dto
		}
	}

	clientIP := extractClientIP(r)
	sessInfo, cookie, err := s.auth.LoginForOrg(r.Context(), clientIP, res.OrgID, req.OwnerEmail, req.OwnerPassword)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "bootstrap completed but session creation failed", true, nil)
		return
	}
	resp.CSRFToken = sessInfo.CSRFToken

	if s.snapshots != nil {
		if refErr := s.snapshots.Refresh(r.Context()); refErr != nil {
			w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			s.emitSecurityAudit(r.Context(), &authSessionContext{OrgID: res.OrgID}, "bootstrap.snapshot_sync_pending", map[string]any{
				"error": refErr.Error(),
			})
		}
	}

	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) writeBootstrapCompleteError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, bootstrap.ErrBootstrapAlreadyCompleted):
		writeErrorEnvelope(w, r, http.StatusGone, "bootstrap_already_completed", "conflict_error", "instance bootstrap has already been completed", false, nil)
	case errors.Is(err, bootstrap.ErrInvalidBootstrapToken):
		writeErrorEnvelope(w, r, http.StatusUnauthorized, "invalid_bootstrap_token", "authentication_error", "invalid or unreadable bootstrap token", false, nil)
	case errors.Is(err, bootstrap.ErrBootstrapRateLimited):
		writeErrorEnvelope(w, r, http.StatusTooManyRequests, "auth_rate_limited", "rate_limit_error", "too many invalid bootstrap token attempts", true, nil)
	case errors.Is(err, bootstrap.ErrMasterKEKAckRequired):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "master_kek_ack_required", "invalid_request_error", "master_kek_backup_acknowledged must be true", false, nil)
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_bootstrap_request", "invalid_request_error", err.Error(), false, nil)
	}
}

func (s *Server) provisionInitialBootstrapTopology(
	ctx context.Context,
	orgID, projectID, envID, ownerPrincipalID, adapterID string,
	ic *bootstrapInitialConnectionReq,
) (string, string, string, *vkey.CreatedVirtualKey, error) {
	var credIDPtr *string
	if ic.SecretValue != "" {
		if s.vault == nil {
			return "", "", "", nil, fmt.Errorf("vault is required to store initial_connection.secret_value")
		}
		secretBuf := []byte(ic.SecretValue)
		defer clear(secretBuf)
		meta, err := s.vault.SealSecret(ctx, vault.SealSecretParams{
			OrgID:      orgID,
			Name:       strings.TrimSpace(ic.Name) + " API Key",
			SecretKind: vault.SecretKindAPIKey,
			Plaintext:  secretBuf,
		})
		if err != nil {
			return "", "", "", nil, fmt.Errorf("seal initial secret: %w", err)
		}
		credIDPtr = &meta.ID
	}

	connID, err := idgen.New(idgen.PrefixConnection)
	if err != nil {
		return "", "", "", nil, err
	}
	deployID, err := idgen.New(idgen.PrefixDeployment)
	if err != nil {
		return "", "", "", nil, err
	}
	routeID, err := idgen.New(idgen.PrefixRoute)
	if err != nil {
		return "", "", "", nil, err
	}

	now := s.nowMs()
	cidrsJSON := "[]"
	if ic.AllowPrivateIP {
		cidrsJSON = `["127.0.0.0/8","10.0.0.0/8","172.16.0.0/12","192.168.0.0/16","::1/128"]`
	}
	authMode := "API_KEY"
	if credIDPtr == nil {
		authMode = "NONE"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", "", nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_connections (
			id, org_id, owner_project_id, adapter_id, credential_id, name,
			endpoint_base_url, allow_private_ip, allowed_cidrs_json, data_region,
			billing_mode, auth_mode, state, shared_project_ids_json, config_json,
			version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ACTIVE', '[]', '{}', 1, ?, ?)`,
		connID, orgID, projectID, adapterID, nullStrPtr(credIDPtr), strings.TrimSpace(ic.Name),
		strings.TrimSpace(ic.EndpointURL), boolToInt(ic.AllowPrivateIP), cidrsJSON, ic.DataRegion,
		ic.BillingMode, authMode, now, now,
	); err != nil {
		return "", "", "", nil, err
	}

	if credIDPtr != nil {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO connection_secret_bindings (
				org_id, connection_id, slot_name, credential_id, inject_mode, created_at_ms
			) VALUES (?, ?, 'api_key', ?, 'HOST_BLIND_INJECT', ?)`,
			orgID, connID, *credIDPtr, now,
		); err != nil {
			return "", "", "", nil, err
		}
	}

	modelSlug := strings.TrimSpace(ic.ModelID)
	var logicalModelID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM logical_models
		WHERE canonical_slug = ? AND (org_id = ? OR org_id IS NULL)
		ORDER BY (org_id IS NOT NULL) DESC
		LIMIT 1`,
		modelSlug, orgID,
	).Scan(&logicalModelID)
	if errors.Is(err, sql.ErrNoRows) {
		logicalModelID, err = idgen.New(idgen.PrefixLogicalModel)
		if err != nil {
			return "", "", "", nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO logical_models (
				id, org_id, canonical_slug, modality_kind, context_window, max_output_tokens,
				base_capabilities_json, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, 'CHAT', 128000, 8192, '{"streaming":"VERIFIED"}', 1, ?, ?)`,
			logicalModelID, orgID, modelSlug, now, now,
		); err != nil {
			return "", "", "", nil, err
		}
	} else if err != nil {
		return "", "", "", nil, err
	}

	pricingJSON := `{"input_token_nano_usd":0,"output_token_nano_usd":0}`
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO model_deployments (
			id, org_id, connection_id, logical_model_id, upstream_model_name,
			native_dialect, capability_overrides_json, assurance_level,
			pricing_nano_usd_json, state, cooldown_until_ms,
			connect_timeout_ms, first_byte_timeout_ms, idle_chunk_timeout_ms,
			version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, NULL, '{}', 'DECLARED', ?, 'ACTIVE_HEALTHY', NULL, 5000, 30000, 15000, 1, ?, ?)`,
		deployID, orgID, connID, logicalModelID, modelSlug, pricingJSON, now, now,
	); err != nil {
		return "", "", "", nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO route_policies (
			id, org_id, project_id, env_id, route_slug, strategy,
			max_failover_hops, required_region, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, 'default', 'PRIORITY_FAILOVER', 3, ?, 1, ?, ?)`,
		routeID, orgID, projectID, envID, ic.DataRegion, now, now,
	); err != nil {
		return "", "", "", nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO route_targets (
			org_id, route_id, deployment_id, priority_order, weight
		) VALUES (?, ?, ?, 1, 100)`,
		orgID, routeID, deployID,
	); err != nil {
		return "", "", "", nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO config_changelog (org_id, entity_kind, entity_id, mutation_type, emergency, created_at_ms)
		VALUES (?, 'route_policy', ?, 'UPSERT', 0, ?)`,
		orgID, routeID, now,
	); err != nil {
		return "", "", "", nil, err
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return "", "", "", nil, err
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"connection_id": connID,
		"deployment_id": deployID,
		"route_id":      routeID,
	})
	if err := s.txAudit.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      ownerPrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "bootstrap.initial_topology_provisioned",
		TargetType:   "provider_connection",
		TargetID:     connID,
		RequestID:    ptrOrNil(requestIDFromContext(ctx)),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		return "", "", "", nil, err
	}

	if err := tx.Commit(); err != nil {
		return "", "", "", nil, err
	}

	createdKey, err := s.vkeys.Create(ctx, vkey.CreateParams{
		OrgID:            orgID,
		ProjectID:        projectID,
		EnvID:            envID,
		OwnerPrincipalID: &ownerPrincipalID,
		Name:             "bootstrap-default-key",
		ActorID:          ownerPrincipalID,
	})
	if err != nil {
		return "", "", "", nil, err
	}
	return connID, deployID, routeID, createdKey, nil
}

func isValidDataRegion(r string) bool {
	switch r {
	case "EU", "US", "APAC", "ON_PREM":
		return true
	default:
		return false
	}
}

func extractClientIP(r *http.Request) string {
	raw := strings.TrimSpace(r.RemoteAddr)
	if raw == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(raw); err == nil && host != "" {
		return host
	}
	return raw
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStrPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}
