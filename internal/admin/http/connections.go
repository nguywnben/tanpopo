// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	extport "github.com/nguywnben/tanpopo/internal/extension/port"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type providerAdapterSummaryDTO struct {
	ID                      string   `json:"id"`
	Slug                    string   `json:"slug"`
	Name                    string   `json:"name"`
	PackageID               string   `json:"package_id"`
	Version                 string   `json:"version"`
	ExecutionTier           string   `json:"execution_tier"`
	OriginTrustTier         string   `json:"origin_trust_tier"`
	EnterpriseSigned        bool     `json:"enterprise_signed"`
	InstallationState       string   `json:"installation_state"`
	WireProtocol            string   `json:"wire_protocol,omitempty"`
	NativePassthroughPrefix *string  `json:"native_passthrough_prefix,omitempty"`
	SupportedAuthMethods    []string `json:"supported_auth_methods"`
	SecretSlots             []string `json:"secret_slots"`
	AllowedHosts            []string `json:"allowed_hosts"`
}

type providerConnectionDTO struct {
	ID                     string            `json:"id"`
	ProjectID              *string           `json:"project_id"`
	SharedProjectIDs       []string          `json:"shared_project_ids,omitempty"`
	Name                   string            `json:"name"`
	AdapterID              string            `json:"adapter_id"`
	BillingMode            string            `json:"billing_mode"`
	EndpointURL            string            `json:"endpoint_url"`
	DataRegion             string            `json:"data_region"`
	AllowPrivateIP         bool              `json:"allow_private_ip"`
	State                  string            `json:"state"`
	SecretSlotFingerprints map[string]string `json:"secret_slot_fingerprints"`
	Version                int64             `json:"version"`
	CreatedAt              string            `json:"created_at"`
}

type createConnectionRequest struct {
	Name             string            `json:"name"`
	ProjectID        *string           `json:"project_id"`
	SharedProjectIDs []string          `json:"shared_project_ids"`
	AdapterID        string            `json:"adapter_id"`
	BillingMode      string            `json:"billing_mode"`
	EndpointURL      string            `json:"endpoint_url"`
	DataRegion       string            `json:"data_region"`
	AllowPrivateIP   bool              `json:"allow_private_ip"`
	AllowedCIDRs     []string          `json:"allowed_cidrs"`
	Config           map[string]any    `json:"config"`
	SecretBindings   map[string]string `json:"secret_bindings"`
}

type updateConnectionRequest struct {
	Name           *string           `json:"name"`
	EndpointURL    *string           `json:"endpoint_url"`
	AllowPrivateIP *bool             `json:"allow_private_ip"`
	AllowedCIDRs   *[]string         `json:"allowed_cidrs"`
	State          *string           `json:"state"`
	SecretBindings map[string]string `json:"secret_bindings"`
}

type createProviderAdapterRequest struct {
	InstallationID string   `json:"installation_id"`
	AdapterSlug    string   `json:"adapter_slug"`
	AllowedHosts   []string `json:"allowed_hosts"`
}

func (s *Server) handleCreateProviderAdapter(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !sc.IsOperator {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "registering a provider adapter requires an instance OPERATOR", false, nil)
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	var req createProviderAdapterRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if s.catalog == nil || s.installer == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "provider_catalog_unavailable", "upstream_error", "provider catalog or extension installer is not configured", true, nil)
		return
	}
	installation, err := s.installer.GetInstallation(r.Context(), req.InstallationID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading extension installation", true, nil)
		return
	}
	if installation.State != installer.StateEnabledActive || installation.ActiveVersionID == nil {
		writeErrorEnvelope(w, r, http.StatusConflict, "extension_not_active", "conflict_error", "provider adapters can only be registered from an active approved extension", false, nil)
		return
	}
	var manifestJSON string
	if err := s.db.QueryRowContext(r.Context(), s.rebind(`SELECT manifest_json FROM extension_versions WHERE id = ? AND package_id = ?`), *installation.ActiveVersionID, installation.PackageID).Scan(&manifestJSON); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading active extension manifest", true, nil)
		return
	}
	var manifest installer.ExtensionManifest
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "extension_manifest_invalid", "extension_error", "active extension manifest is invalid", false, nil)
		return
	}
	var declared *installer.ManifestCap
	for i := range manifest.Capabilities {
		capability := &manifest.Capabilities[i]
		if capability.Type == "provider_adapter" && capability.AdapterSlug == req.AdapterSlug {
			declared = capability
			break
		}
	}
	if declared == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "adapter_capability_not_declared", "invalid_request_error", "adapter_slug must match a provider_adapter capability in the active manifest", false, nil)
		return
	}
	var nativePrefix *string
	if strings.TrimSpace(declared.NativePassthroughPrefix) != "" {
		value := declared.NativePassthroughPrefix
		nativePrefix = &value
	}
	adapter, err := s.catalog.CreateProviderAdapter(r.Context(), catalog.CreateProviderAdapterParams{
		InstallationID: req.InstallationID, ExpectedActiveVersionID: *installation.ActiveVersionID,
		AdapterSlug: declared.AdapterSlug, NativePrefix: nativePrefix,
		SupportedDialects: declared.SupportedDialects, AllowedHosts: req.AllowedHosts,
	})
	if err != nil {
		if errors.Is(err, port.ErrVersionConflict) {
			writeErrorEnvelope(w, r, http.StatusConflict, "extension_version_changed", "conflict_error", "active extension version changed during adapter registration; retry with the current manifest", false, nil)
			return
		}
		if errors.Is(err, port.ErrInvalidInput) {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		}
		if errors.Is(err, port.ErrAlreadyExists) {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_conflict", "conflict_error", "provider adapter is already registered", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed registering provider adapter", true, nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": adapter.ID, "installation_id": adapter.InstallationID, "adapter_slug": adapter.AdapterSlug,
		"native_prefix": adapter.NativePrefix, "supported_dialects": adapter.SupportedDialects,
		"allowed_hosts": adapter.AllowedHosts, "version": adapter.Version,
	})
}

type connectionTestResponseDTO struct {
	Valid            bool     `json:"valid"`
	LatencyMs        int64    `json:"latency_ms"`
	DetectedModelIDs []string `json:"detected_model_ids"`
	FailureCode      *string  `json:"failure_code,omitempty"`
	SanitizedMessage *string  `json:"sanitized_message,omitempty"`
}

func mapExecutionTierOpenAPI(dbTier string) string {
	switch strings.ToUpper(strings.TrimSpace(dbTier)) {
	case "DECLARATIVE_T1", "DECLARATIVE":
		return "declarative"
	case "WASM_SANDBOX_T2", "SANDBOXED_WASM":
		return "sandboxed_wasm"
	case "ISOLATED_SIDECAR_T3", "ISOLATED_SIDECAR":
		return "isolated_sidecar"
	default:
		return "declarative"
	}
}

func mapConnectionStateOpenAPI(dbState string) string {
	switch dbState {
	case catalog.ConnStateDraft, catalog.ConnStateValidating:
		return "CREATED_UNVERIFIED"
	case catalog.ConnStateActive:
		return "ACTIVE"
	case catalog.ConnStateDegraded:
		return "DEGRADED"
	case catalog.ConnStateAuthExpired:
		return "AUTH_EXPIRED_REAUTH_REQUIRED"
	case catalog.ConnStateSuspendedAdmin, catalog.ConnStateRevoked, catalog.ConnStateArchived:
		return "DISABLED"
	default:
		return "CREATED_UNVERIFIED"
	}
}

func writeConnectionSSRFError(w http.ResponseWriter, r *http.Request, ssrfErr error) {
	code := "ssrf_blocked_private_ip"
	msg := "endpoint_url violates egress SSRF policy"
	if errors.Is(ssrfErr, egress.ErrPortDenied) {
		code = "egress_port_denied"
		msg = "endpoint_url targets a blocked local control-plane port"
	} else if errors.Is(ssrfErr, egress.ErrInvalidPolicy) {
		code = "invalid_endpoint_url"
		msg = "valid http or https endpoint_url is required"
	} else if errors.Is(ssrfErr, egress.ErrMetadataBlocked) {
		code = "ssrf_blocked_private_ip"
		msg = "metadata and link-local addresses are unconditionally blocked"
	}
	writeErrorEnvelope(w, r, http.StatusBadRequest, code, "invalid_request_error", msg, false, nil)
}

func (s *Server) handleListProviderAdapters(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "provider_adapters", orgID, sha256HexString("adapters"))
	if !ok {
		return
	}

	var q strings.Builder
	var args []any
	q.WriteString(`
		SELECT pa.id, pa.adapter_slug, pa.native_prefix, pa.supported_dialects_json, pa.allowed_hosts_json, pa.created_at_ms,
		       ep.id, ep.canonical_name, ep.origin_trust_tier,
		       ei.state, ei.approved_permissions_json,
		       COALESCE(ev.semver, '1.0.0'), COALESCE(ev.execution_tier, 'DECLARATIVE_T1'),
		       COALESCE(ev.enterprise_signed, ` + port.BoolExpr(s.dialect, false) + `), COALESCE(ev.manifest_json, '{}')
		FROM provider_adapters pa
		JOIN extension_installations ei ON ei.id = pa.installation_id
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = COALESCE(ei.active_version_id, ei.candidate_version_id)
		WHERE ei.state NOT IN ('UNINSTALLED', 'CANCELLED', 'REJECTED_INVALID')`)
	if cur != nil {
		q.WriteString(` AND (pa.created_at_ms < ? OR (pa.created_at_ms = ? AND pa.id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY pa.created_at_ms DESC, pa.id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), s.rebind(q.String()), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing provider adapters", true, nil)
		return
	}
	defer rows.Close()

	type rowItem struct {
		dto       providerAdapterSummaryDTO
		createdMs int64
	}
	items := make([]rowItem, 0, limit+1)
	for rows.Next() {
		dto, createdMs, scanErr := scanProviderAdapterSummaryRow(rows)
		if scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning provider adapter", true, nil)
			return
		}
		items = append(items, rowItem{dto: dto, createdMs: createdMs})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating provider adapters", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]providerAdapterSummaryDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "provider_adapters", orgID, sha256HexString("adapters"), last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[providerAdapterSummaryDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleGetProviderAdapter(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}
	adapterID := strings.TrimSpace(r.PathValue("adapterId"))
	if adapterID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider adapter not found", false, nil)
		return
	}

	row := s.db.QueryRowContext(r.Context(), s.rebind(`
		SELECT pa.id, pa.adapter_slug, pa.native_prefix, pa.supported_dialects_json, pa.allowed_hosts_json, pa.created_at_ms,
		       ep.id, ep.canonical_name, ep.origin_trust_tier,
		       ei.state, ei.approved_permissions_json,
		       COALESCE(ev.semver, '1.0.0'), COALESCE(ev.execution_tier, 'DECLARATIVE_T1'),
		       COALESCE(ev.enterprise_signed, `+port.BoolExpr(s.dialect, false)+`), COALESCE(ev.manifest_json, '{}')
		FROM provider_adapters pa
		JOIN extension_installations ei ON ei.id = pa.installation_id
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions ev ON ev.id = COALESCE(ei.active_version_id, ei.candidate_version_id)
		WHERE pa.id = ?
		  AND ei.state NOT IN ('UNINSTALLED', 'CANCELLED', 'REJECTED_INVALID')`), adapterID)
	dto, _, err := scanProviderAdapterSummaryRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider adapter not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading provider adapter", true, nil)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func scanProviderAdapterSummaryRow(row interface{ Scan(...any) error }) (providerAdapterSummaryDTO, int64, error) {
	var (
		id, slug, dialectsJSON, hostsJSON, pkgID, canonicalName, trustTier, installState, permsJSON, semver, execTier, manifestJSON string
		nativePrefix                                                                                                                sql.NullString
		createdMs                                                                                                                   int64
		entSigned                                                                                                                   port.FlexBool
	)
	if err := row.Scan(
		&id, &slug, &nativePrefix, &dialectsJSON, &hostsJSON, &createdMs,
		&pkgID, &canonicalName, &trustTier,
		&installState, &permsJSON,
		&semver, &execTier, &entSigned, &manifestJSON,
	); err != nil {
		return providerAdapterSummaryDTO{}, 0, err
	}

	var dialects []string
	_ = json.Unmarshal([]byte(dialectsJSON), &dialects)
	wireProtocol := "openai_compatible"
	if len(dialects) > 0 && strings.TrimSpace(dialects[0]) != "" {
		wireProtocol = dialects[0]
	}

	var hosts []string
	_ = json.Unmarshal([]byte(hostsJSON), &hosts)
	if hosts == nil {
		hosts = []string{}
	}

	var perms installer.PermissionSet
	_ = json.Unmarshal([]byte(permsJSON), &perms)
	secretSlots := perms.SecretSlots
	if secretSlots == nil {
		secretSlots = []string{}
	}

	authMethods := []string{"API_KEY"}
	if strings.TrimSpace(manifestJSON) != "" && manifestJSON != "{}" {
		var mf map[string]any
		if json.Unmarshal([]byte(manifestJSON), &mf) == nil {
			extracted := make([]string, 0)
			if rawAuth, ok := mf["supportedAuthMethods"].([]any); ok {
				for _, item := range rawAuth {
					if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
						extracted = append(extracted, strings.TrimSpace(s))
					}
				}
			}
			if rawCaps, ok := mf["capabilities"].([]any); ok {
				seenAuth := make(map[string]bool)
				for _, capRaw := range rawCaps {
					capMap, ok := capRaw.(map[string]any)
					if !ok || capMap["type"] != "provider_adapter" {
						continue
					}
					if methods, ok := capMap["authMethods"].([]any); ok {
						for _, mRaw := range methods {
							if mMap, ok := mRaw.(map[string]any); ok {
								if mid, ok := mMap["id"].(string); ok && strings.TrimSpace(mid) != "" && !seenAuth[mid] {
									seenAuth[mid] = true
									extracted = append(extracted, strings.TrimSpace(mid))
								}
							}
						}
					}
				}
			}
			if len(extracted) > 0 {
				authMethods = extracted
			}
		}
	}
	if len(authMethods) == 0 {
		authMethods = []string{"API_KEY"}
	}

	dto := providerAdapterSummaryDTO{
		ID:                   id,
		Slug:                 slug,
		Name:                 canonicalName,
		PackageID:            pkgID,
		Version:              semver,
		ExecutionTier:        mapExecutionTierOpenAPI(execTier),
		OriginTrustTier:      trustTier,
		EnterpriseSigned:     bool(entSigned),
		InstallationState:    mapExtensionStateOpenAPI(installState),
		WireProtocol:         wireProtocol,
		SupportedAuthMethods: authMethods,
		SecretSlots:          secretSlots,
		AllowedHosts:         hosts,
	}
	if nativePrefix.Valid {
		v := nativePrefix.String
		dto.NativePassthroughPrefix = &v
	}
	return dto, createdMs, nil
}

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	projFilter := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projFilter != "" {
		if !s.authorize(w, r, sc, rbac.ActionConnectionRead, rbac.TargetScope{OrgID: orgID, ProjectID: projFilter}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionConnectionRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("connections|" + projFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "connections", orgID, filterHash)
	if !ok {
		return
	}

	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	var allowedProjects map[string]bool
	if !orgWide {
		allowedProjects = make(map[string]bool)
		for _, pid := range actorProjectIDs(sc.Actor, orgID) {
			allowedProjects[pid] = true
		}
		if projFilter == "" && len(allowedProjects) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[providerConnectionDTO]{
				Data:    []providerConnectionDTO{},
				HasMore: false,
			})
			return
		}
	}

	type rowItem struct {
		dto           providerConnectionDTO
		primaryCredID *string
		createdMs     int64
	}
	items := make([]rowItem, 0, limit+1)
	var filterProjSet map[string]bool
	if projFilter != "" {
		filterProjSet = map[string]bool{projFilter: true}
	}
	needFilter := !orgWide || projFilter != ""
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
			SELECT id, owner_project_id, shared_project_ids_json, adapter_id, credential_id, name,
			       endpoint_base_url, allow_private_ip, data_region, billing_mode, state,
			       version, created_at_ms
			FROM provider_connections
			WHERE org_id = ?`)
		if hasCur {
			q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
			args = append(args, batchCreatedMs, batchCreatedMs, batchLastID)
		}
		q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
		args = append(args, batchLimit)

		sqlCount := 0
		var errCode, errType, errMsg string
		qErr := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
			rows, err := dbq.QueryContext(r.Context(), s.rebind(q.String()), args...)
			if err != nil {
				errCode, errType, errMsg = "internal_error", "upstream_error", "failed listing provider connections"
				return err
			}
			defer rows.Close()
			for rows.Next() {
				sqlCount++
				var (
					id, sharedJSON, adapterID, name, endpointURL, dataRegion, billingMode, state string
					projNS, credNS                                                               sql.NullString
					allowPriv                                                                    port.FlexBool
					ver, createdMs                                                               int64
				)
				if scanErr := rows.Scan(
					&id, &projNS, &sharedJSON, &adapterID, &credNS, &name,
					&endpointURL, &allowPriv, &dataRegion, &billingMode, &state,
					&ver, &createdMs,
				); scanErr != nil {
					errCode, errType, errMsg = "internal_error", "upstream_error", "failed scanning provider connection"
					return scanErr
				}
				batchCreatedMs = createdMs
				batchLastID = id
				sharedProjIDs, shErr := parseSharedProjectIDsJSON(sharedJSON)
				if shErr != nil {
					errCode, errType, errMsg = "internal_error", "upstream_error", "failed parsing connection visibility metadata"
					return shErr
				}
				if !orgWide {
					vis, visErr := connectionVisibleToAnyProject(projNS, sharedJSON, allowedProjects)
					if visErr != nil {
						errCode, errType, errMsg = "internal_error", "upstream_error", "failed parsing connection visibility metadata"
						return visErr
					}
					if !vis {
						continue
					}
				}
				if projFilter != "" {
					vis, visErr := connectionVisibleToAnyProject(projNS, sharedJSON, filterProjSet)
					if visErr != nil {
						errCode, errType, errMsg = "internal_error", "upstream_error", "failed parsing connection visibility metadata"
						return visErr
					}
					if !vis {
						continue
					}
				}
				var projPtr, credPtr *string
				if projNS.Valid {
					v := projNS.String
					projPtr = &v
				}
				if credNS.Valid && credNS.String != "" {
					v := credNS.String
					credPtr = &v
				}
				items = append(items, rowItem{
					dto: providerConnectionDTO{
						ID:                     id,
						ProjectID:              projPtr,
						SharedProjectIDs:       sharedProjIDs,
						Name:                   name,
						AdapterID:              adapterID,
						BillingMode:            billingMode,
						EndpointURL:            endpointURL,
						DataRegion:             dataRegion,
						AllowPrivateIP:         bool(allowPriv),
						State:                  mapConnectionStateOpenAPI(state),
						SecretSlotFingerprints: map[string]string{},
						Version:                ver,
						CreatedAt:              formatTimeRFC3339Ms(createdMs),
					},
					primaryCredID: credPtr,
					createdMs:     createdMs,
				})
				if len(items) >= limit+1 {
					break
				}
			}
			if rerr := rows.Err(); rerr != nil {
				errCode, errType, errMsg = "internal_error", "upstream_error", "failed iterating provider connections"
				return rerr
			}
			return nil
		})
		if qErr != nil {
			if errMsg == "" {
				errCode, errType, errMsg = "internal_error", "upstream_error", "failed listing provider connections"
			}
			writeErrorEnvelope(w, r, http.StatusInternalServerError, errCode, errType, errMsg, true, nil)
			return
		}

		if !needFilter || sqlCount < batchLimit {
			break
		}
		hasCur = true
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	data := make([]providerConnectionDTO, len(items))
	for i := range items {
		fps, ferr := s.loadConnectionSlotFingerprints(r.Context(), orgID, items[i].dto.ID, items[i].primaryCredID)
		if ferr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading connection secret slot fingerprints", true, nil)
			return
		}
		items[i].dto.SecretSlotFingerprints = fps
		data[i] = items[i].dto
	}

	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "connections", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[providerConnectionDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createConnectionRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	var ownerProjPtr *string
	if req.ProjectID != nil {
		if trimmed := strings.TrimSpace(*req.ProjectID); trimmed != "" {
			ownerProjPtr = &trimmed
		}
	}
	authProjID := ptrStrValue(ownerProjPtr)
	if len(req.SharedProjectIDs) > 0 {
		authProjID = ""
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: authProjID}) {
		return
	}

	var sharedProjIDs []string
	if len(req.SharedProjectIDs) > 0 {
		if ownerProjPtr != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "project_id and shared_project_ids cannot both be set", false, nil)
			return
		}
		seen := make(map[string]struct{}, len(req.SharedProjectIDs))
		sharedProjIDs = make([]string, 0, len(req.SharedProjectIDs))
		for _, rawSP := range req.SharedProjectIDs {
			sp := strings.TrimSpace(rawSP)
			if sp == "" {
				writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "shared_project_ids cannot contain blank entries", false, nil)
				return
			}
			if _, dup := seen[sp]; !dup {
				seen[sp] = struct{}{}
				sharedProjIDs = append(sharedProjIDs, sp)
			}
		}
	}

	name := strings.TrimSpace(req.Name)
	adapterID := strings.TrimSpace(req.AdapterID)
	endpointURL := strings.TrimSpace(req.EndpointURL)
	billingMode := strings.TrimSpace(req.BillingMode)
	dataRegion := strings.TrimSpace(req.DataRegion)
	if name == "" || adapterID == "" || endpointURL == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "name, adapter_id, and endpoint_url are required", false, nil)
		return
	}
	if billingMode != catalog.BillingModeCommercialAPI && billingMode != catalog.BillingModeSelfHostedInfra {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_billing_mode", "invalid_request_error", "billing_mode must be COMMERCIAL_API or SELF_HOSTED_INFRA", false, nil)
		return
	}
	if !isValidDataRegion(dataRegion) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_data_region", "invalid_request_error", "data_region must be one of EU, US, APAC, ON_PREM", false, nil)
		return
	}

	if ownerProjPtr != nil {
		if !s.requireActiveOrgAndProject(w, r, orgID, *ownerProjPtr) {
			return
		}
	} else {
		if !s.requireActiveOrg(w, r, orgID) {
			return
		}
		for _, sp := range sharedProjIDs {
			if !s.requireActiveOrgAndProject(w, r, orgID, sp) {
				return
			}
		}
	}

	if ssrfErr := validateEndpointSSRF(r.Context(), endpointURL, req.AllowPrivateIP, s.hostResolver, s.blockedPorts); ssrfErr != nil {
		writeConnectionSSRFError(w, r, ssrfErr)
		return
	}

	allowedCIDRs := append([]string(nil), req.AllowedCIDRs...)
	if req.AllowPrivateIP && len(allowedCIDRs) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "allowed_cidrs is required when allow_private_ip is true", false, nil)
		return
	}

	slotNames := make([]string, 0, len(req.SecretBindings))
	for k := range req.SecretBindings {
		slotNames = append(slotNames, k)
	}
	sort.Strings(slotNames)

	bindings := make([]catalog.SecretBindingInput, 0, len(slotNames))
	var primaryCredPtr *string
	for _, slot := range slotNames {
		credID := strings.TrimSpace(req.SecretBindings[slot])
		bindings = append(bindings, catalog.SecretBindingInput{
			SlotName:     slot,
			CredentialID: credID,
			InjectMode:   catalog.InjectModeHostBlind,
		})
		if slot == "api_key" || primaryCredPtr == nil {
			v := credID
			primaryCredPtr = &v
		}
	}

	authMode := catalog.AuthModeNone
	if len(bindings) > 0 {
		authMode = catalog.AuthModeAPIKey
	}

	cfgJSON := "{}"
	if len(req.Config) > 0 {
		rawCfg, cerr := json.Marshal(req.Config)
		if cerr != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "invalid connection config object", false, nil)
			return
		}
		cfgJSON = string(rawCfg)
	}

	conn, err := s.catalog.CreateProviderConnection(r.Context(), catalog.CreateProviderConnectionParams{
		OrgID:            orgID,
		OwnerProjectID:   ownerProjPtr,
		SharedProjectIDs: sharedProjIDs,
		AdapterID:        adapterID,
		CredentialID:     primaryCredPtr,
		Name:             name,
		EndpointBaseURL:  endpointURL,
		AllowPrivateIP:   req.AllowPrivateIP,
		AllowedCIDRs:     allowedCIDRs,
		DataRegion:       dataRegion,
		BillingMode:      billingMode,
		AuthMode:         authMode,
		SecretBindings:   bindings,
		ConfigJSON:       cfgJSON,
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", err.Error(), false, nil)
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "provider connection already exists", false, nil)
		case errors.Is(err, catalog.ErrInvalidBillingMode), errors.Is(err, port.ErrInvalidInput):
			code := "invalid_request"
			if strings.Contains(err.Error(), "allowed_hosts") || strings.Contains(err.Error(), "allowedHosts") {
				code = "disallowed_host"
			}
			writeErrorEnvelope(w, r, http.StatusBadRequest, code, "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating provider connection", true, nil)
		}
		return
	}

	dto, ferr := s.buildProviderConnectionDTO(r.Context(), conn)
	if ferr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading created connection metadata", true, nil)
		return
	}

	if audID, aerr := idgen.New(idgen.PrefixAudit); aerr == nil && s.audit != nil {
		rawMeta, _ := json.Marshal(map[string]any{
			"adapter_id":   conn.AdapterID,
			"billing_mode": conn.BillingMode,
			"data_region":  conn.DataRegion,
			"project_id":   conn.OwnerProjectID,
		})
		_ = s.audit.Append(r.Context(), port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  s.nowMs(),
			OrgID:        &orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      sc.Actor.PrincipalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "provider_connection.api_created",
			TargetType:   "provider_connection",
			TargetID:     conn.ID,
			RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
			MetadataJSON: string(rawMeta),
		})
	}

	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	w.Header().Set("ETag", FormatETag(dto.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req updateConnectionRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	connID := strings.TrimSpace(r.PathValue("connectionId"))
	if connID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionConnectionWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	cur, err := s.catalog.GetProviderConnection(r.Context(), orgID, connID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading provider connection", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: ptrStrValue(cur.OwnerProjectID)}) {
		return
	}
	if cur.OwnerProjectID != nil {
		if !s.requireActiveOrgAndProject(w, r, orgID, *cur.OwnerProjectID) {
			return
		}
	} else {
		if !s.requireActiveOrg(w, r, orgID) {
			return
		}
	}

	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	curDTO, ferr := s.buildProviderConnectionDTO(r.Context(), cur)
	if ferr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading connection fingerprints", true, nil)
		return
	}
	if expectedVer != cur.Version {
		writeOptimisticConflict(w, r, cur.Version, curDTO)
		return
	}

	if cur.State == catalog.ConnStateRevoked || cur.State == catalog.ConnStateArchived {
		writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_state_transition", "invalid_request_error", fmt.Sprintf("cannot modify connection in state %s", cur.State), false, curDTO)
		return
	}

	var nextNamePtr *string
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" || len(trimmed) > 128 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "name cannot be blank", false, nil)
			return
		}
		nextNamePtr = &trimmed
	}

	var nextEndpointPtr *string
	nextEndpoint := cur.EndpointBaseURL
	if req.EndpointURL != nil {
		trimmed := strings.TrimSpace(*req.EndpointURL)
		if trimmed == "" {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_endpoint_url", "invalid_request_error", "endpoint_url cannot be blank", false, nil)
			return
		}
		nextEndpoint = trimmed
		nextEndpointPtr = &trimmed
	}

	nextAllowPriv := cur.AllowPrivateIP
	if req.AllowPrivateIP != nil {
		nextAllowPriv = *req.AllowPrivateIP
	}

	var nextCIDRsPtr *[]string
	effectiveCIDRs := cur.AllowedCIDRs
	if req.AllowedCIDRs != nil {
		copied := append([]string(nil), (*req.AllowedCIDRs)...)
		nextCIDRsPtr = &copied
		effectiveCIDRs = copied
	}
	if nextAllowPriv && len(effectiveCIDRs) == 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "allowed_cidrs is required when allow_private_ip is true", false, nil)
		return
	}

	if req.EndpointURL != nil || req.AllowPrivateIP != nil || req.AllowedCIDRs != nil {
		if ssrfErr := validateEndpointSSRF(r.Context(), nextEndpoint, nextAllowPriv, s.hostResolver, s.blockedPorts); ssrfErr != nil {
			writeConnectionSSRFError(w, r, ssrfErr)
			return
		}
	}

	var nextStatePtr *string
	if req.State != nil {
		reqState := strings.ToUpper(strings.TrimSpace(*req.State))
		var mapped string
		switch reqState {
		case "ACTIVE":
			if cur.State == catalog.ConnStateActive || cur.State == catalog.ConnStateDegraded {
				mapped = catalog.ConnStateActive
			} else {
				writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_state_transition", "invalid_request_error", "connections must pass POST /connections/{id}/test validation to activate", false, curDTO)
				return
			}
		case "DISABLED":
			switch cur.State {
			case catalog.ConnStateDraft, catalog.ConnStateActive, catalog.ConnStateDegraded, catalog.ConnStateAuthExpired:
				mapped = catalog.ConnStateSuspendedAdmin
			case catalog.ConnStateSuspendedAdmin, catalog.ConnStateRevoked, catalog.ConnStateArchived:
				mapped = cur.State
			default:
				writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_state_transition", "invalid_request_error", fmt.Sprintf("cannot disable connection in state %s", cur.State), false, curDTO)
				return
			}
		default:
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "state must be ACTIVE or DISABLED", false, nil)
			return
		}
		nextStatePtr = &mapped
	}

	var replaceBindings bool
	var nextBindings []catalog.SecretBindingInput
	if req.SecretBindings != nil {
		replaceBindings = true
		slots := make([]string, 0, len(req.SecretBindings))
		for k := range req.SecretBindings {
			slots = append(slots, k)
		}
		sort.Strings(slots)
		nextBindings = make([]catalog.SecretBindingInput, 0, len(slots))
		for _, slot := range slots {
			nextBindings = append(nextBindings, catalog.SecretBindingInput{
				SlotName:     strings.TrimSpace(slot),
				CredentialID: strings.TrimSpace(req.SecretBindings[slot]),
				InjectMode:   catalog.InjectModeHostBlind,
			})
		}
	}

	updated, err := s.catalog.UpdateProviderConnection(r.Context(), catalog.UpdateProviderConnectionParams{
		OrgID:           orgID,
		ConnectionID:    connID,
		ExpectedVersion: expectedVer,
		Name:            nextNamePtr,
		EndpointBaseURL: nextEndpointPtr,
		AllowPrivateIP:  req.AllowPrivateIP,
		AllowedCIDRs:    nextCIDRsPtr,
		State:           nextStatePtr,
		MergeBindings:   replaceBindings,
		SecretBindings:  nextBindings,
		AuditActorKind:  "PRINCIPAL",
		AuditActorID:    sc.Actor.PrincipalID,
		RequestID:       requestIDFromContext(r.Context()),
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", err.Error(), false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			latest, lerr := s.catalog.GetProviderConnection(r.Context(), orgID, connID)
			if errors.Is(lerr, port.ErrNotFound) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
				return
			}
			if lerr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading provider connection", true, nil)
				return
			}
			latestDTO, _ := s.buildProviderConnectionDTO(r.Context(), latest)
			writeOptimisticConflict(w, r, latest.Version, latestDTO)
		case errors.Is(err, catalog.ErrInvalidStateTransition):
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_state_transition", "invalid_request_error", err.Error(), false, curDTO)
		case errors.Is(err, port.ErrInvalidInput):
			code := "invalid_request"
			if strings.Contains(err.Error(), "allowed_hosts") || strings.Contains(err.Error(), "allowedHosts") {
				code = "disallowed_host"
			}
			writeErrorEnvelope(w, r, http.StatusBadRequest, code, "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed updating provider connection", true, nil)
		}
		return
	}

	dto, err := s.buildProviderConnectionDTO(r.Context(), updated)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading connection fingerprints", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	w.Header().Set("ETag", FormatETag(dto.Version))
	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	connID := strings.TrimSpace(r.PathValue("connectionId"))
	if connID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionConnectionWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	cur, err := s.catalog.GetProviderConnection(r.Context(), orgID, connID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading provider connection", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: ptrStrValue(cur.OwnerProjectID)}) {
		return
	}
	if cur.OwnerProjectID != nil {
		if !s.requireActiveOrgAndProject(w, r, orgID, *cur.OwnerProjectID) {
			return
		}
	} else {
		if !s.requireActiveOrg(w, r, orgID) {
			return
		}
	}

	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	curDTO, _ := s.buildProviderConnectionDTO(r.Context(), cur)
	if expectedVer != cur.Version {
		writeOptimisticConflict(w, r, cur.Version, curDTO)
		return
	}

	if err := s.catalog.DeleteProviderConnection(r.Context(), orgID, connID, expectedVer); err != nil {
		switch {
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			current, getErr := s.catalog.GetProviderConnection(r.Context(), orgID, connID)
			if getErr == nil {
				currentDTO, dtoErr := s.buildProviderConnectionDTO(r.Context(), current)
				if dtoErr == nil {
					writeOptimisticConflict(w, r, current.Version, currentDTO)
					return
				}
			}
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "provider connection changed; reload before retrying", false, nil)
		case errors.Is(err, port.ErrResourceInUse):
			writeErrorEnvelope(w, r, http.StatusConflict, "connection_referenced_by_deployments", "conflict_error", "connection is referenced by active model deployments", false, curDTO)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed deleting provider connection", true, nil)
		}
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	connID := strings.TrimSpace(r.PathValue("connectionId"))
	if connID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionConnectionWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	cur, err := s.catalog.GetProviderConnection(r.Context(), orgID, connID)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading provider connection", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionConnectionWrite, rbac.TargetScope{OrgID: orgID, ProjectID: ptrStrValue(cur.OwnerProjectID)}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, ptrStrValue(cur.OwnerProjectID)) {
		return
	}

	if !s.probeLimiter.Allow(orgID+"|"+sc.Actor.PrincipalID, s.nowMs()) {
		writeErrorEnvelope(w, r, http.StatusTooManyRequests, "probe_rate_limited", "rate_limit_error", "connection test rate limit exceeded", true, nil)
		return
	}

	if ssrfErr := validateEndpointSSRF(r.Context(), cur.EndpointBaseURL, cur.AllowPrivateIP, s.hostResolver, s.blockedPorts); ssrfErr != nil {
		writeConnectionSSRFError(w, r, ssrfErr)
		return
	}

	if s.broker == nil || (s.providerRuntime == nil && s.providerRuntimeFactory == nil) {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "probe_runtime_unavailable", "upstream_error", "connection validation broker or extension runtime unavailable", true, nil)
		return
	}
	runtime := s.providerRuntime
	var releaseRuntime func()
	if s.providerRuntimeFactory != nil {
		runtime, releaseRuntime, err = s.providerRuntimeFactory(r.Context(), orgID, connID)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "probe_runtime_unavailable", "upstream_error", "connection validation runtime unavailable", true, nil)
			return
		}
		if releaseRuntime != nil {
			defer releaseRuntime()
		}
	}
	if runtime == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "probe_runtime_unavailable", "upstream_error", "connection validation runtime unavailable", true, nil)
		return
	}

	started := time.Now()
	var verdict *extport.ValidationVerdict

	switch cur.State {
	case catalog.ConnStateDraft, catalog.ConnStateAuthExpired, catalog.ConnStateSuspendedAdmin:
		verdict, _, err = s.catalog.ValidateConnection(r.Context(), orgID, connID, runtime, s.broker)
	case catalog.ConnStateActive, catalog.ConnStateDegraded:
		verdict, err = s.probeActiveConnection(r.Context(), orgID, cur, runtime)
	default:
		writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_state_transition", "invalid_request_error", fmt.Sprintf("cannot test connection in state %s", cur.State), false, nil)
		return
	}

	latencyMs := time.Since(started).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}

	if err != nil {
		switch {
		case errors.Is(err, egress.ErrPrivateIPBlocked), errors.Is(err, egress.ErrMetadataBlocked), errors.Is(err, egress.ErrHostDenied), errors.Is(err, egress.ErrPortDenied):
			writeConnectionSSRFError(w, r, err)
			return
		case verdict == nil && errors.Is(err, catalog.ErrInvalidStateTransition):
			writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "invalid_state_transition", "invalid_request_error", err.Error(), false, nil)
			return
		case verdict == nil && errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		case verdict == nil && errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", err.Error(), false, nil)
			return
		case verdict == nil || verdict.FailureCode == "probe_egress_failed":
			code, safeMsg := classifySafeProbeError(err)
			verdict = &extport.ValidationVerdict{
				Valid:            false,
				FailureCode:      code,
				SanitizedMessage: safeMsg,
			}
		}
	}

	resp := connectionTestResponseDTO{
		Valid:            verdict != nil && verdict.Valid,
		LatencyMs:        latencyMs,
		DetectedModelIDs: []string{},
	}
	if verdict != nil {
		if len(verdict.DetectedModelIDs) > 0 {
			resp.DetectedModelIDs = append([]string(nil), verdict.DetectedModelIDs...)
		}
		if verdict.FailureCode != "" {
			v := verdict.FailureCode
			resp.FailureCode = &v
		}
		if verdict.SanitizedMessage != "" {
			v := verdict.SanitizedMessage
			resp.SanitizedMessage = &v
		}
	}

	if audID, aerr := idgen.New(idgen.PrefixAudit); aerr == nil && s.audit != nil {
		rawMeta, _ := json.Marshal(map[string]any{
			"valid":        resp.Valid,
			"latency_ms":   resp.LatencyMs,
			"failure_code": resp.FailureCode,
		})
		_ = s.audit.Append(r.Context(), port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  s.nowMs(),
			OrgID:        &orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      sc.Actor.PrincipalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "provider_connection.tested",
			TargetType:   "provider_connection",
			TargetID:     connID,
			RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
			MetadataJSON: string(rawMeta),
		})
	}

	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	writeJSON(w, http.StatusOK, resp)
}

func classifySafeProbeError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout", "upstream validation probe timed out"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "upstream_timeout", "upstream validation probe timed out"
	}
	var (
		certInvalid x509.CertificateInvalidError
		unknownAuth x509.UnknownAuthorityError
		hostErr     x509.HostnameError
		tlsRecErr   tls.RecordHeaderError
	)
	lowerMsg := strings.ToLower(err.Error())
	if errors.As(err, &certInvalid) ||
		errors.As(err, &unknownAuth) ||
		errors.As(err, &hostErr) ||
		errors.As(err, &tlsRecErr) ||
		strings.Contains(lowerMsg, "tls") ||
		strings.Contains(lowerMsg, "x509") ||
		strings.Contains(lowerMsg, "certificate") {
		return "upstream_tls_error", "upstream TLS handshake or certificate validation failed"
	}
	return "upstream_unreachable", "upstream connection could not be reached"
}

func (s *Server) probeActiveConnection(ctx context.Context, orgID string, cur *catalog.ProviderConnection, runtime extport.ProviderRuntime) (*extport.ValidationVerdict, error) {
	probeReqID, err := idgen.New(idgen.PrefixTrace)
	if err != nil {
		probeReqID = "trc_probe_" + cur.ID
	}
	cfgMap := make(map[string]any)
	if trimmed := strings.TrimSpace(cur.ConfigJSON); trimmed != "" && trimmed != "{}" {
		_ = json.Unmarshal([]byte(trimmed), &cfgMap)
	}
	cfgMap["endpoint_base_url"] = cur.EndpointBaseURL
	mergedCfg, _ := json.Marshal(cfgMap)

	slotNames := make([]string, 0, len(cur.SecretBindings))
	for _, b := range cur.SecretBindings {
		slotNames = append(slotNames, b.SlotName)
	}
	probeSpec, err := runtime.BuildValidationProbe(ctx, extport.ValidationProbeInput{
		RequestID:       probeReqID,
		ConfigJSON:      string(mergedCfg),
		SecretSlotNames: slotNames,
	})
	if err != nil {
		return nil, err
	}
	inj, err := catalog.ConvertCredentialInjections(probeSpec.CredentialInjections)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(cur.EndpointBaseURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: invalid endpoint_url", port.ErrInvalidInput)
	}
	portNum := 443
	if u.Scheme == "http" {
		portNum = 80
	}
	if p := u.Port(); p != "" {
		parsedPort, perr := strconv.Atoi(p)
		if perr != nil || parsedPort <= 0 || parsedPort > 65535 {
			return nil, fmt.Errorf("%w: invalid endpoint port %q", port.ErrInvalidInput, p)
		}
		portNum = parsedPort
	}
	narrowPolicy := &egress.Policy{
		AllowedHosts:   []string{strings.ToLower(u.Hostname())},
		AllowPrivateIP: cur.AllowPrivateIP,
		AllowedCIDRs:   cur.AllowedCIDRs,
		AllowedPorts:   []int{portNum},
		RequireTLS:     u.Scheme == "https",
	}
	egressResp, err := s.broker.ExecuteWithHostConstraint(ctx, broker.EgressRequest{
		OrgID:                orgID,
		ConnectionID:         cur.ID,
		Method:               probeSpec.Method,
		TargetURL:            probeSpec.TargetURL,
		SafeHeaders:          probeSpec.SafeHeaders,
		BodyBytes:            probeSpec.BodyBytes,
		CredentialInjections: inj,
		ActorKind:            "SYSTEM",
		ActorID:              "catalog_probe",
		EntryPoint:           port.AuditEntryPointAdminAPI,
		RequestID:            probeReqID,
	}, broker.HostEgressConstraint{NarrowPolicy: narrowPolicy})
	if err != nil {
		code, safeMsg := classifySafeProbeError(err)
		if !isConnectionSSRFOrConfigErr(err) {
			_, _ = s.catalog.RecordRuntimeHealthOutcome(ctx, orgID, cur.ID, &extport.CanonicalProviderError{
				CanonicalCode:      code,
				RetryablePreStream: true,
			})
		}
		return &extport.ValidationVerdict{
			Valid:            false,
			FailureCode:      code,
			SanitizedMessage: safeMsg,
		}, err
	}
	flatHdrs := make(map[string]string, len(egressResp.SanitizedHeaders))
	for k, vals := range egressResp.SanitizedHeaders {
		if len(vals) > 0 {
			flatHdrs[k] = strings.Join(vals, ", ")
		}
	}
	verdict, err := runtime.VerifyValidationResult(ctx, extport.ValidationResultInput{
		RequestID:        probeReqID,
		HTTPStatus:       int32(egressResp.StatusCode),
		SanitizedHeaders: flatHdrs,
		ResponseBody:     egressResp.Body,
	})
	if err != nil {
		return nil, err
	}
	if verdict.Valid {
		_, _ = s.catalog.RecordRuntimeHealthOutcome(ctx, orgID, cur.ID, nil)
	} else {
		reauth := egressResp.StatusCode == http.StatusUnauthorized ||
			egressResp.StatusCode == http.StatusForbidden ||
			strings.Contains(strings.ToLower(verdict.FailureCode), "auth")
		_, _ = s.catalog.RecordRuntimeHealthOutcome(ctx, orgID, cur.ID, &extport.CanonicalProviderError{
			CanonicalCode:           verdict.FailureCode,
			RetryablePreStream:      !reauth,
			TriggerConnectionReauth: reauth,
		})
	}
	return &verdict, nil
}

func isConnectionSSRFOrConfigErr(err error) bool {
	return errors.Is(err, egress.ErrPrivateIPBlocked) ||
		errors.Is(err, egress.ErrMetadataBlocked) ||
		errors.Is(err, egress.ErrHostDenied) ||
		errors.Is(err, egress.ErrPortDenied) ||
		errors.Is(err, catalog.ErrInvalidStateTransition) ||
		errors.Is(err, port.ErrInvalidInput) ||
		errors.Is(err, port.ErrNotFound)
}

func (s *Server) buildProviderConnectionDTO(ctx context.Context, conn *catalog.ProviderConnection) (providerConnectionDTO, error) {
	fps, err := s.loadConnectionSlotFingerprints(ctx, conn.OrgID, conn.ID, conn.CredentialID)
	if err != nil {
		return providerConnectionDTO{}, err
	}
	return providerConnectionDTO{
		ID:                     conn.ID,
		ProjectID:              conn.OwnerProjectID,
		SharedProjectIDs:       append([]string(nil), conn.SharedProjectIDs...),
		Name:                   conn.Name,
		AdapterID:              conn.AdapterID,
		BillingMode:            conn.BillingMode,
		EndpointURL:            conn.EndpointBaseURL,
		DataRegion:             conn.DataRegion,
		AllowPrivateIP:         conn.AllowPrivateIP,
		State:                  mapConnectionStateOpenAPI(conn.State),
		SecretSlotFingerprints: fps,
		Version:                conn.Version,
		CreatedAt:              formatTimeRFC3339Ms(conn.CreatedAtMs),
	}, nil
}

func (s *Server) loadConnectionSlotFingerprints(ctx context.Context, orgID, connID string, primaryCredID *string) (map[string]string, error) {
	out := make(map[string]string)
	err := s.withOrgReadTx(ctx, orgID, func(dbq adminDBQuerier) error {
		rows, err := dbq.QueryContext(ctx, s.rebind(`
			SELECT csb.slot_name, sc.key_fingerprint
			FROM connection_secret_bindings csb
			JOIN secret_credentials sc ON sc.org_id = csb.org_id AND sc.id = csb.credential_id
			WHERE csb.org_id = ? AND csb.connection_id = ?
			ORDER BY csb.slot_name ASC`),
			orgID, connID,
		)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var slot, fp string
			if scanErr := rows.Scan(&slot, &fp); scanErr != nil {
				return scanErr
			}
			out[slot] = formatTrailingFingerprint(fp)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, hasAPIKey := out["api_key"]; !hasAPIKey && primaryCredID != nil && *primaryCredID != "" {
			var fp string
			err := dbq.QueryRowContext(ctx, s.rebind(`
				SELECT key_fingerprint FROM secret_credentials WHERE org_id = ? AND id = ?`),
				orgID, *primaryCredID,
			).Scan(&fp)
			if err == nil {
				out["api_key"] = formatTrailingFingerprint(fp)
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
