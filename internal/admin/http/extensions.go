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
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/trust"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

var hexSHA256Regex = regexp.MustCompile(`^[a-f0-9]{64}$`)

type marketplaceCatalogResponse struct {
	Data          []MarketplaceCatalogEntry `json:"data"`
	OfflineCached bool                      `json:"offline_cached"`
	NextCursor    *string                   `json:"next_cursor,omitempty"`
	HasMore       bool                      `json:"has_more"`
}

type extensionInstallRequest struct {
	SourceURI      string `json:"source_uri"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

type extensionQuotasDTO struct {
	MaxMemoryMib        int `json:"max_memory_mib"`
	MaxCPUMsPerCall     int `json:"max_cpu_ms_per_call"`
	MaxStreamChunkCPUMs int `json:"max_stream_chunk_cpu_ms"`
}

type extensionPermissionDiffDTO struct {
	InstallationID        string                    `json:"installation_id"`
	PackageID             string                    `json:"package_id"`
	ActiveVersion         *string                   `json:"active_version"`
	CandidateVersion      string                    `json:"candidate_version"`
	OriginTrustTier       string                    `json:"origin_trust_tier"`
	SignatureVerified     bool                      `json:"signature_verified"`
	EnterpriseSigned      bool                      `json:"enterprise_signed"`
	SigningKeyFingerprint string                    `json:"signing_key_fingerprint,omitempty"`
	ExecutionTier         string                    `json:"execution_tier"`
	ActiveExecutionTier   *string                   `json:"active_execution_tier"`
	ActiveAllowedHosts    []string                  `json:"active_allowed_hosts"`
	CandidateAllowedHosts []string                  `json:"candidate_allowed_hosts"`
	AddedAllowedHosts     []string                  `json:"added_allowed_hosts"`
	RemovedAllowedHosts   []string                  `json:"removed_allowed_hosts"`
	SecretSlots           []string                  `json:"secret_slots"`
	ActiveSecretSlots     []string                  `json:"active_secret_slots"`
	AddedSecretSlots      []string                  `json:"added_secret_slots"`
	QuotaEscalated        bool                      `json:"quota_escalated"`
	ActiveQuotas          *extensionQuotasDTO       `json:"active_quotas"`
	Quotas                extensionQuotasDTO        `json:"quotas"`
	ActivePermissions     *installer.PermissionSet  `json:"active_permissions"`
	CandidatePermissions  installer.PermissionSet   `json:"candidate_permissions"`
	PermissionDelta       installer.PermissionDelta `json:"permission_delta"`
	HasEscalation         bool                      `json:"has_escalation"`
}

type extensionInstallationDTO struct {
	ID                      string  `json:"id"`
	PackageID               string  `json:"package_id"`
	CanonicalName           string  `json:"canonical_name,omitempty"`
	ActiveVersion           *string `json:"active_version"`
	CandidateVersion        *string `json:"candidate_version"`
	PreviousRollbackVersion *string `json:"previous_rollback_version"`
	ContractVersion         string  `json:"contract_version"`
	State                   string  `json:"state"`
	OriginTrustTier         string  `json:"origin_trust_tier"`
	SignatureVerified       bool    `json:"signature_verified"`
	EnterpriseSigned        bool    `json:"enterprise_signed"`
	SigningKeyFingerprint   string  `json:"signing_key_fingerprint,omitempty"`
	ExecutionTier           string  `json:"execution_tier"`
	Version                 int64   `json:"version"`
}

type approvePermissionsRequest struct {
	EnableImmediately             bool `json:"enable_immediately"`
	AcknowledgeUnverifiedExternal bool `json:"acknowledge_unverified_external"`
}

type rollbackExtensionRequest struct {
	Reason string `json:"reason"`
}

type putOrgExtensionPoliciesRequest struct {
	MinPluginTrust string `json:"min_plugin_trust"`
	Policies       []struct {
		PackageID            string  `json:"package_id"`
		Enabled              bool    `json:"enabled"`
		PinnedInstallationID *string `json:"pinned_installation_id"`
	} `json:"policies"`
}

func (s *Server) handleListMarketplaceCatalog(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	kindFilter := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("kind")))
	filterHash := sha256HexString("marketplace|" + kindFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "marketplace", orgID, filterHash)
	if !ok {
		return
	}

	var filtered []MarketplaceCatalogEntry
	for _, entry := range s.marketplace {
		if kindFilter != "" && !strings.Contains(strings.ToUpper(entry.CanonicalName+" "+strings.Join(entry.Capabilities, " ")), kindFilter) {
			continue
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].PackageID < filtered[j].PackageID
	})

	startIdx := 0
	if cur != nil {
		for idx, item := range filtered {
			if item.PackageID > cur.LastID {
				startIdx = idx
				break
			}
			startIdx = len(filtered)
		}
	}

	endIdx := startIdx + limit
	hasMore := false
	if endIdx < len(filtered) {
		hasMore = true
	} else {
		endIdx = len(filtered)
	}
	page := make([]MarketplaceCatalogEntry, 0, endIdx-startIdx)
	if startIdx < endIdx {
		page = append(page, filtered[startIdx:endIdx]...)
	}

	var nextCursor *string
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "marketplace", orgID, filterHash, 0, last.PackageID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, marketplaceCatalogResponse{
		Data:          page,
		OfflineCached: s.offlineMode,
		NextCursor:    nextCursor,
		HasMore:       hasMore,
	})
}

func (s *Server) handleStageExtensionInstall(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req extensionInstallRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionStage, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	if s.installer == nil || s.archiveResolver == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer or archive resolver is not configured", true, nil)
		return
	}

	sourceURI := strings.TrimSpace(req.SourceURI)
	expectedSHA := strings.ToLower(strings.TrimSpace(req.ExpectedSHA256))
	if sourceURI == "" || !hexSHA256Regex.MatchString(expectedSHA) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "source_uri and lowercase 64-hex expected_sha256 are required", false, nil)
		return
	}

	stageReq, cleanup, err := s.archiveResolver(r.Context(), orgID, sourceURI, expectedSHA)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.writeInstallerError(w, r, err, nil)
		return
	}
	if stageReq == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_extension_package", "extension_error", "empty staged extension package", false, nil)
		return
	}
	stageReq.Source.URI = sourceURI
	stageReq.Source.ExpectedSHA256 = expectedSHA
	stageReq.ActorID = sc.Actor.PrincipalID
	stageReq.RequestID = requestIDFromContext(r.Context())
	if stageReq.TargetOrgID == "" {
		stageReq.TargetOrgID = orgID
	}

	res, err := s.installer.StageFreshInstall(r.Context(), *stageReq)
	if err != nil {
		s.writeInstallerError(w, r, err, nil)
		return
	}

	diff, err := s.loadExtensionPermissionDiffDTO(r.Context(), res.Installation.InstallationID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed building permission diff", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(res.Installation.Version))
	writeJSON(w, http.StatusAccepted, diff)
}

func (s *Server) handleListExtensionInstallations(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	filterHash := sha256HexString("extension_installations")
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "extension_installations", orgID, filterHash)
	if !ok {
		return
	}

	var q strings.Builder
	var args []any
	q.WriteString(`
		SELECT ei.id, ei.package_id, ep.canonical_name, ep.origin_trust_tier, ei.state, ei.version, ei.created_at_ms,
		       va.semver, vc.semver, vp.semver,
		       COALESCE(va.contract_version, vc.contract_version, '1.0'),
		       COALESCE(va.execution_tier, vc.execution_tier, 'DECLARATIVE_T1'),
		       COALESCE(va.enterprise_signed, vc.enterprise_signed, ` + port.BoolExpr(s.dialect, false) + `),
		       COALESCE(va.signature_verified, vc.signature_verified, ` + port.BoolExpr(s.dialect, false) + `),
		       COALESCE(va.signing_key_fingerprint, vc.signing_key_fingerprint, ''),
		       COALESCE(va.enterprise_key_fingerprint, vc.enterprise_key_fingerprint, '')
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions va ON va.id = ei.active_version_id
		LEFT JOIN extension_versions vc ON vc.id = ei.candidate_version_id
		LEFT JOIN extension_versions vp ON vp.id = ei.previous_pinned_version_id
		WHERE ei.state NOT IN ('UNINSTALLED', 'CANCELLED')`)
	if cur != nil {
		q.WriteString(` AND (ei.created_at_ms < ? OR (ei.created_at_ms = ? AND ei.id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY ei.created_at_ms DESC, ei.id DESC LIMIT ?`)
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), s.rebind(q.String()), args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing extension installations", true, nil)
		return
	}
	defer rows.Close()

	type insRow struct {
		dto       extensionInstallationDTO
		createdMs int64
	}
	var items []insRow
	for rows.Next() {
		var (
			id, pkgID, canonicalName, trust, state, contractVer, execTier, sigFP, entFP string
			ver, createdMs                                                              int64
			actVer, candVer, prevVer                                                    sql.NullString
			entBool, sigBool                                                            port.FlexBool
		)
		if scanErr := rows.Scan(&id, &pkgID, &canonicalName, &trust, &state, &ver, &createdMs, &actVer, &candVer, &prevVer, &contractVer, &execTier, &entBool, &sigBool, &sigFP, &entFP); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning installation", true, nil)
			return
		}
		liveTrust := trust
		liveSig := bool(sigBool)
		liveEnt := bool(entBool)
		liveFP := strings.ToLower(strings.TrimSpace(sigFP))
		if liveFP == "" && liveEnt {
			liveFP = strings.ToLower(strings.TrimSpace(entFP))
		}
		if s.installer != nil {
			liveTrust, liveSig, liveEnt, liveFP = installer.ComputeKeyringEffectiveProvenance(canonicalName, bool(sigBool), sigFP, bool(entBool), entFP, s.installer.Keyring())
		}
		items = append(items, insRow{
			dto: extensionInstallationDTO{
				ID:                      id,
				PackageID:               pkgID,
				CanonicalName:           canonicalName,
				ActiveVersion:           sqlNullStrPtr(actVer),
				CandidateVersion:        sqlNullStrPtr(candVer),
				PreviousRollbackVersion: sqlNullStrPtr(prevVer),
				ContractVersion:         contractVer,
				State:                   mapExtensionStateOpenAPI(state),
				OriginTrustTier:         liveTrust,
				SignatureVerified:       liveSig,
				EnterpriseSigned:        liveEnt,
				SigningKeyFingerprint:   liveFP,
				ExecutionTier:           mapExecutionTierOpenAPI(execTier),
				Version:                 ver,
			},
			createdMs: createdMs,
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating installations", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]extensionInstallationDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "extension_installations", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[extensionInstallationDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleGetExtensionPermissionDiff(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionRead, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}

	diff, ver, err := s.loadExtensionPermissionDiffWithVersion(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading permission diff", true, nil)
		return
	}

	w.Header().Set("ETag", FormatETag(ver))
	writeJSON(w, http.StatusOK, diff)
}

func (s *Server) handleApproveExtensionPermissions(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	var req approvePermissionsRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionApprove, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if s.installer == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer is not configured", true, nil)
		return
	}

	curDTO, curDBState, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading installation", true, nil)
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	blocked, fourErr := s.isFourEyesSelfApproval(r.Context(), orgID, insID, sc.Actor.PrincipalID)
	if fourErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking four-eyes approval state", true, nil)
		return
	}
	if blocked {
		s.emitSecurityAudit(r.Context(), sc, "extension.self_approval_blocked", map[string]any{
			"installation_id": insID,
			"principal_id":    sc.Actor.PrincipalID,
		})
		writeErrorEnvelope(w, r, http.StatusForbidden, "self_approval_forbidden", "permission_error", "staging principal cannot approve own extension permissions when four-eyes approval is enabled", false, nil)
		return
	}

	candTrust, candEntSigned, provErr := s.installer.CandidateProvenance(r.Context(), insID)
	if errors.Is(provErr, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if provErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying candidate extension provenance", true, nil)
		return
	}
	if candTrust == lockfile.TrustTierUnverifiedExternal && !candEntSigned && !req.AcknowledgeUnverifiedExternal {
		writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "unverified_external_acknowledgement_required", "invalid_request_error", "acknowledge_unverified_external=true is required to approve unsigned unverified_external extensions", false, curDTO)
		return
	}

	// Pass empty orgID to LifecycleService if caller is an INSTANCE-scoped OPERATOR without ORG-level binding.
	approverOrg := orgID
	if sc.IsOperator && !actorHasOrgScopedApproverBinding(sc.Actor, orgID) {
		approverOrg = ""
	}
	reqID := requestIDFromContext(r.Context())

	var rec *installer.InstallationRecord
	switch curDBState {
	case installer.StatePendingPermissionApproval:
		rec, err = s.installer.ApproveAndActivate(r.Context(), installer.ApproveInstallRequest{
			InstallationID:      insID,
			ExpectedVersion:     expectedVer,
			ApproverPrincipalID: sc.Actor.PrincipalID,
			ActivateImmediately: req.EnableImmediately,
			OrgID:               approverOrg,
			RequestID:           reqID,
		})
	case installer.StateUpdatingPendingReconsent:
		rec, err = s.installer.ApproveEscalatedUpgrade(r.Context(), insID, expectedVer, sc.Actor.PrincipalID, approverOrg, reqID)
	case installer.StateInstalledDisabled:
		if !req.EnableImmediately {
			writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", "disabled installation requires enable_immediately=true to reactivate", false, curDTO)
			return
		}
		rec, err = s.installer.EnableDisabledInstallation(r.Context(), insID, expectedVer, sc.Actor.PrincipalID, approverOrg, reqID)
	default:
		writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", fmt.Sprintf("installation in state %s cannot be approved", curDBState), false, nil)
		return
	}
	if err != nil {
		latest, _, _ := s.loadExtensionInstallationDTO(r.Context(), insID)
		s.writeInstallerError(w, r, err, latest)
		return
	}

	syncErr := s.syncExtensionProviderAdapters(r.Context(), rec.InstallationID)
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	if syncErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "adapter_registration_failed", "upstream_error", "extension activated cleanly, but failed registering provider adapters", true, nil)
		return
	}

	out, _, err := s.loadExtensionInstallationDTO(r.Context(), rec.InstallationID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading installation", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(out.Version))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpgradeExtension(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	var req extensionInstallRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionStage, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if s.installer == nil || s.archiveResolver == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer or archive resolver is not configured", true, nil)
		return
	}

	curDTO, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading installation", true, nil)
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	sourceURI := strings.TrimSpace(req.SourceURI)
	expectedSHA := strings.ToLower(strings.TrimSpace(req.ExpectedSHA256))
	if sourceURI == "" || !hexSHA256Regex.MatchString(expectedSHA) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "source_uri and lowercase 64-hex expected_sha256 are required", false, nil)
		return
	}

	stageReq, cleanup, err := s.archiveResolver(r.Context(), orgID, sourceURI, expectedSHA)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.writeInstallerError(w, r, err, curDTO)
		return
	}
	if stageReq == nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_extension_package", "extension_error", "empty staged extension package", false, nil)
		return
	}
	stageReq.Source.URI = sourceURI
	stageReq.Source.ExpectedSHA256 = expectedSHA

	approverOrg := orgID
	if sc.IsOperator && !actorHasOrgScopedApproverBinding(sc.Actor, orgID) {
		approverOrg = ""
	}
	upRes, err := s.installer.StageUpgrade(r.Context(), installer.StageUpgradeRequest{
		InstallationID:        insID,
		ExpectedVersion:       expectedVer,
		ArchiveReader:         stageReq.ArchiveReader,
		ArchivePath:           stageReq.ArchivePath,
		Source:                stageReq.Source,
		Signature:             stageReq.Signature,
		SBOMDigest:            stageReq.SBOMDigest,
		ActorID:               sc.Actor.PrincipalID,
		OrgID:                 approverOrg,
		RequestID:             requestIDFromContext(r.Context()),
		TrustedBuiltinArchive: stageReq.TrustedBuiltinArchive,
	})
	if err != nil {
		latest, _, _ := s.loadExtensionInstallationDTO(r.Context(), insID)
		s.writeInstallerError(w, r, err, latest)
		return
	}

	if upRes.Escalated {
		diff, ver, derr := s.loadExtensionPermissionDiffWithVersion(r.Context(), insID)
		if derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading escalation permission diff", true, nil)
			return
		}
		w.Header().Set("ETag", FormatETag(ver))
		writeJSON(w, http.StatusAccepted, diff)
		return
	}

	syncErr := s.syncExtensionProviderAdapters(r.Context(), insID)
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	if syncErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "adapter_registration_failed", "upstream_error", "extension upgraded cleanly, but failed registering provider adapters", true, nil)
		return
	}
	out, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading upgraded installation", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(out.Version))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRollbackExtension(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	var req rollbackExtensionRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionApprove, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}
	if s.installer == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer is not configured", true, nil)
		return
	}

	curDTO, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading installation", true, nil)
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	approverOrg := orgID
	if sc.IsOperator && !actorHasOrgScopedApproverBinding(sc.Actor, orgID) {
		approverOrg = ""
	}
	if _, err := s.installer.RollbackInstallation(r.Context(), insID, expectedVer, sc.Actor.PrincipalID, approverOrg, requestIDFromContext(r.Context())); err != nil {
		latest, _, _ := s.loadExtensionInstallationDTO(r.Context(), insID)
		s.writeInstallerError(w, r, err, latest)
		return
	}

	if audID, aerr := idgen.New(idgen.PrefixAudit); aerr == nil && s.audit != nil {
		rawMeta, _ := json.Marshal(map[string]any{"reason": reason})
		_ = s.audit.Append(r.Context(), port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  s.nowMs(),
			OrgID:        &orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      sc.Actor.PrincipalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "extension.rollback_reason_recorded",
			TargetType:   "extension_installation",
			TargetID:     insID,
			RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
			MetadataJSON: string(rawMeta),
		})
	}

	syncErr := s.syncExtensionProviderAdapters(r.Context(), insID)
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	if syncErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "adapter_registration_failed", "upstream_error", "extension rolled back cleanly, but failed syncing provider adapters", true, nil)
		return
	}
	out, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading installation after rollback", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(out.Version))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleQuarantineExtension(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	var req rollbackExtensionRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionQuarantine, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !sc.IsOperator {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "global extension quarantine requires an instance OPERATOR", false, nil)
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	// Serialize authorized quarantine requests so a request-created barrier
	// cannot be mistaken for a concurrent quarantine request's barrier.
	s.quarantineMu.Lock()
	defer s.quarantineMu.Unlock()
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}
	if s.installer == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer is not configured", true, nil)
		return
	}
	cur, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading extension installation", true, nil)
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}
	if s.killSwitch == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_unavailable", "upstream_error", "extension quarantine requires the emergency kill-switch registry", true, nil)
		return
	}
	abortStreams := true
	requestSwitch, _, killErr := s.killSwitch.TriggerWithSeveredCount(r.Context(), killswitch.TriggerParams{
		Level: killswitch.LevelExtensionInstallation, TargetID: insID, Reason: reason,
		TriggeredByPrincipalID: sc.Actor.PrincipalID, EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID: ptrOrNil(requestIDFromContext(r.Context())), AbortActiveStreams: &abortStreams,
	})
	alreadyActive := errors.Is(killErr, killswitch.ErrAlreadyActive)
	if alreadyActive {
		_, killErr = s.killSwitch.EnsureActiveAbortBarrier(r.Context(), killswitch.LevelExtensionInstallation, insID, nil)
	}
	if killErr != nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "quarantine_kill_switch_failed", "upstream_error", "extension quarantine could not establish its emergency stream barrier", true, nil)
		return
	}
	if err := s.installer.QuarantineInstallationIfVersion(r.Context(), insID, expectedVer, reason, sc.Actor.PrincipalID); err != nil {
		latest, _, latestErr := s.loadExtensionInstallationDTO(r.Context(), insID)
		if errors.Is(err, port.ErrVersionConflict) && requestSwitch != nil && latestErr != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "quarantine_state_reload_failed", "upstream_error", "quarantine outcome could not be confirmed; emergency barrier remains active", true, nil)
			return
		}
		if errors.Is(err, port.ErrVersionConflict) && requestSwitch != nil && latest.State != installer.StateQuarantined {
			_, clearErr := s.killSwitch.Clear(r.Context(), killswitch.ClearParams{
				KillSwitchID: requestSwitch.ID, ExpectedVersion: requestSwitch.Version,
				ClearedByPrincipalID: sc.Actor.PrincipalID,
				ClearReason:          "quarantine lost installation version race; remove request-created barrier",
				EntryPoint:           port.AuditEntryPointAdminAPI, RequestID: ptrOrNil(requestIDFromContext(r.Context())),
			})
			if clearErr != nil {
				writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "quarantine_barrier_cleanup_failed", "upstream_error", "stale quarantine was rejected but its request-created emergency barrier could not be cleared", true, nil)
				return
			}
		}
		s.writeInstallerError(w, r, err, latest)
		return
	}
	if s.snapshots != nil {
		if err := s.snapshots.Refresh(r.Context()); err != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "quarantine_snapshot_refresh_failed", "upstream_error", "extension quarantine committed but its serving snapshot could not be refreshed; emergency barrier remains active", true, nil)
			return
		}
	}
	quarantined, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading quarantined extension", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(quarantined.Version))
	writeJSON(w, http.StatusOK, quarantined)
}

func (s *Server) handleUnquarantineExtension(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	var req rollbackExtensionRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionQuarantine, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !sc.IsOperator {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "global extension unquarantine requires an instance OPERATOR", false, nil)
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	s.quarantineMu.Lock()
	defer s.quarantineMu.Unlock()
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}
	if s.installer == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer is not configured", true, nil)
		return
	}
	cur, curDBState, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading extension installation", true, nil)
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, cur)
		return
	}
	if curDBState != installer.StateQuarantined {
		writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", fmt.Sprintf("installation in state %s is not quarantined", curDBState), false, cur)
		return
	}

	if err := s.installer.UnquarantineInstallationIfVersion(r.Context(), insID, expectedVer, reason, sc.Actor.PrincipalID); err != nil {
		latest, _, _ := s.loadExtensionInstallationDTO(r.Context(), insID)
		s.writeInstallerError(w, r, err, latest)
		return
	}

	if s.onExtensionUnquarantined != nil {
		s.onExtensionUnquarantined(insID)
	}

	if s.killSwitch != nil {
		activeSwitches, lErr := s.killSwitch.ListWithFilter(r.Context(), killswitch.ListFilter{
			AllowInstanceScope: true,
			ActiveOnly:         true,
		})
		if lErr != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_clear_failed", "upstream_error", "extension unquarantined cleanly, but clearing emergency kill switch failed", true, nil)
			return
		}
		for _, sw := range activeSwitches {
			if sw.Active && sw.Level == killswitch.LevelExtensionInstallation && sw.TargetID == insID {
				if _, cErr := s.killSwitch.Clear(r.Context(), killswitch.ClearParams{
					OrgID:                sw.OrgID,
					KillSwitchID:         sw.ID,
					ExpectedVersion:      sw.Version,
					ClearedByPrincipalID: sc.Actor.PrincipalID,
					ClearReason:          "extension unquarantined: " + reason,
					EntryPoint:           port.AuditEntryPointAdminAPI,
					RequestID:            ptrOrNil(requestIDFromContext(r.Context())),
				}); cErr != nil && !errors.Is(cErr, killswitch.ErrAlreadyCleared) {
					writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_clear_failed", "upstream_error", "extension unquarantined cleanly, but clearing emergency kill switch failed", true, nil)
					return
				}
			}
		}
	}

	if s.snapshots != nil {
		if err := s.snapshots.Refresh(r.Context()); err != nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "snapshot_refresh_failed", "upstream_error", "extension unquarantined cleanly, but serving snapshot refresh failed", true, nil)
			return
		}
	}
	unquarantined, _, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading unquarantined extension", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(unquarantined.Version))
	writeJSON(w, http.StatusOK, unquarantined)
}

func (s *Server) handleUninstallOrDiscardExtension(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	insID := strings.TrimSpace(r.PathValue("installationId"))
	if insID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionApprove, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if s.installer == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer is not configured", true, nil)
		return
	}

	curDTO, curDBState, err := s.loadExtensionInstallationDTO(r.Context(), insID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading installation", true, nil)
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	approverOrg := orgID
	if sc.IsOperator && !actorHasOrgScopedApproverBinding(sc.Actor, orgID) {
		approverOrg = ""
	}
	switch curDBState {
	case installer.StateUpdatingPendingReconsent:
		_, err = s.installer.RejectEscalatedUpgrade(r.Context(), insID, expectedVer, sc.Actor.PrincipalID, approverOrg, requestIDFromContext(r.Context()))
	case installer.StatePendingPermissionApproval:
		err = s.cancelPendingInstallationTx(r.Context(), insID, expectedVer, orgID, sc.Actor.PrincipalID, requestIDFromContext(r.Context()))
	default:
		err = s.installer.UninstallInstallation(r.Context(), insID, expectedVer, "", sc.Actor.PrincipalID, approverOrg)
	}
	if err != nil {
		latest, _, _ := s.loadExtensionInstallationDTO(r.Context(), insID)
		s.writeInstallerError(w, r, err, latest)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetExtensionLockfile(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionRead, rbac.TargetScope{OrgID: orgID}) {
		return
	}

	var lf *lockfile.Lockfile
	if s.lockfilePath != "" {
		readLF, err := lockfile.Read(s.lockfilePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "extension_error", "failed reading lockfile", false, nil)
			return
		}
		lf = readLF
	}
	if lf == nil {
		lf = lockfile.NewEmpty(time.UnixMilli(s.nowMs()).UTC())
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") {
		writeJSON(w, http.StatusOK, lf)
		return
	}
	yamlBytes, err := lf.MarshalDeterministic()
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "extension_error", "failed serializing lockfile", false, nil)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(yamlBytes)
}

func (s *Server) handlePutOrgExtensionPolicies(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID := strings.TrimSpace(r.PathValue("orgId"))
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if !sc.IsOperator && sc.OrgID != orgID && !actorHasAnyBindingInOrg(sc.Actor, orgID) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	var req putOrgExtensionPoliciesRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionExtensionOrgPolicy, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.requireActiveOrg(w, r, orgID) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	minTrust := strings.ToUpper(strings.TrimSpace(req.MinPluginTrust))
	switch minTrust {
	case "OFFICIAL_ONLY", "VERIFIED_OR_ENTERPRISE_SIGNED", "VERIFIED_ONLY", "ANY_APPROVED":
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "invalid min_plugin_trust", false, nil)
		return
	}

	curOrg, err := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading organization", true, nil)
		return
	}
	if curOrg.Version != expectedVer {
		writeOptimisticConflict(w, r, curOrg.Version, toOrganizationDTO(curOrg))
		return
	}

	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting org extension policy tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scoping org extension policy tx", true, nil)
		return
	}

	res, err := tx.ExecContext(r.Context(), s.rebind(`
		UPDATE organizations
		SET min_plugin_trust = ?, version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`),
		minTrust, now, orgID, expectedVer,
	)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed updating organization min_plugin_trust", true, nil)
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		_ = tx.Rollback()
		latest, lerr := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
		if errors.Is(lerr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
			return
		}
		if lerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading organization", true, nil)
			return
		}
		writeOptimisticConflict(w, r, latest.Version, toOrganizationDTO(latest))
		return
	}

	for _, pol := range req.Policies {
		pkgID := strings.TrimSpace(pol.PackageID)
		if pkgID == "" {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "policy package_id is required", false, nil)
			return
		}
		var canonicalName string
		qerr := tx.QueryRowContext(r.Context(), s.rebind(`SELECT canonical_name FROM extension_packages WHERE id = ?`), pkgID).Scan(&canonicalName)
		if errors.Is(qerr, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension package not found", false, nil)
			return
		}
		if qerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking extension package", true, nil)
			return
		}
		_ = canonicalName

		var pinnedArg any
		if pol.PinnedInstallationID != nil && strings.TrimSpace(*pol.PinnedInstallationID) != "" {
			pinID := strings.TrimSpace(*pol.PinnedInstallationID)
			var pinExists int
			perr := tx.QueryRowContext(r.Context(), s.rebind(`
				SELECT 1
				FROM extension_installations
				WHERE id = ? AND package_id = ? AND state NOT IN ('UNINSTALLED', 'CANCELLED', 'REJECTED_INVALID')`),
				pinID, pkgID,
			).Scan(&pinExists)
			if errors.Is(perr, sql.ErrNoRows) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "pinned installation not found for extension package", false, nil)
				return
			}
			if perr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying pinned installation", true, nil)
				return
			}
			pinnedArg = pinID
		}

		requiresOrgOwner := false
		if pol.Enabled {
			if s.installer == nil {
				writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "extension_installer_unavailable", "extension_error", "extension installer is not configured", true, nil)
				return
			}
			reqOwner, trustErr := s.installer.EvaluatePackagePolicyTrustTx(r.Context(), tx, pkgID, minTrust, pol.PinnedInstallationID)
			if errors.Is(trustErr, port.ErrNotFound) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension package or pinned installation not found", false, nil)
				return
			}
			if errors.Is(trustErr, trust.ErrOrgTrustPolicyDenied) {
				writeErrorEnvelope(w, r, http.StatusForbidden, "org_trust_policy_denied", "permission_error", trustErr.Error(), false, nil)
				return
			}
			if trustErr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed evaluating extension trust policy", true, nil)
				return
			}
			requiresOrgOwner = reqOwner
			if requiresOrgOwner && !actorIsOrgOwnerOrOperator(sc.Actor, orgID) {
				writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "ORG_OWNER or OPERATOR role required to enable unverified_external package", false, nil)
				return
			}
		}

		optIn := port.BoolVal(s.dialect, pol.Enabled && requiresOrgOwner)
		if _, err := tx.ExecContext(r.Context(), s.rebind(`
			INSERT INTO org_extension_policies (org_id, package_id, enabled, org_owner_opt_in, pinned_installation_id, version, created_at_ms, updated_at_ms)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT (org_id, package_id) DO UPDATE SET
				enabled = excluded.enabled,
				org_owner_opt_in = excluded.org_owner_opt_in,
				pinned_installation_id = excluded.pinned_installation_id,
				version = org_extension_policies.version + 1,
				updated_at_ms = excluded.updated_at_ms`),
			orgID, pkgID, port.BoolVal(s.dialect, pol.Enabled), optIn, pinnedArg, now, now,
		); err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed persisting org extension policy", true, nil)
			return
		}
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "org_extension_policy", orgID, port.MutationUpsert, false, sc.Actor.PrincipalID, "extension.org_policy_updated", map[string]any{
		"min_plugin_trust": minTrust,
		"policy_count":     len(req.Policies),
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording org extension policy audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing org extension policies", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	s.publishChange(r.Context(), snapshot.ChangeEvent{
		OrgID:        orgID,
		EntityKind:   "org_extension_policy",
		EntityID:     orgID,
		MutationType: port.MutationUpsert,
	})

	updatedOrg, err := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading organization", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(updatedOrg.Version))
	writeJSON(w, http.StatusOK, toOrganizationDTO(updatedOrg))
}

func (s *Server) ensureGlobalExtensionOperatorIfMultiTenant(w http.ResponseWriter, r *http.Request, sc *authSessionContext) bool {
	if sc == nil {
		return false
	}
	if sc.IsOperator {
		return true
	}
	var orgCount int
	if err := s.withOperatorReadTx(r.Context(), func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM organizations WHERE status NOT IN ('DELETING', 'PENDING_ERASURE')`).Scan(&orgCount)
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking active tenant count", true, nil)
		return false
	}
	if orgCount > 1 {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "multi-tenant global extension mutations require INSTANCE OPERATOR role", false, nil)
		return false
	}
	return true
}

func (s *Server) cancelPendingInstallationTx(ctx context.Context, insID string, expectedVer int64, orgID, actorID, reqID string) error {
	now := s.nowMs()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if orgID != "" {
		if err := port.SetOrgRLSTx(ctx, tx, s.dialect, orgID); err != nil {
			return err
		}
	} else {
		if err := port.SetOperatorRLSTx(ctx, tx, s.dialect); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, s.rebind(`
		UPDATE extension_installations
		SET candidate_version_id = NULL, state = 'CANCELLED', version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ? AND state = 'PENDING_PERMISSION_APPROVAL'`),
		now, insID, expectedVer,
	)
	if err != nil {
		return err
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		return port.ErrVersionConflict
	}
	if err := s.appendChangelogAndAuditTx(ctx, tx, &orgID, "extension_installation", insID, port.MutationDelete, false, actorID, "extension.staged_install_cancelled", map[string]any{
		"installation_id": insID,
	}, reqID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) isFourEyesSelfApproval(ctx context.Context, _ string, insID, actorID string) (bool, error) {
	var fourEyesOrgs int
	if err := s.withOperatorReadTx(ctx, func(q adminDBQuerier) error {
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations WHERE status NOT IN ('DELETING', 'PENDING_ERASURE') AND require_four_eyes_approval = `+port.BoolExpr(s.dialect, true)).Scan(&fourEyesOrgs)
	}); err != nil {
		return true, err
	}
	if fourEyesOrgs == 0 {
		return false, nil
	}
	var stagerID string
	err := s.withOperatorReadTx(ctx, func(q adminDBQuerier) error {
		return q.QueryRowContext(ctx, s.rebind(`
		SELECT actor_id
		FROM audit_events
		WHERE target_type = 'extension_installation' AND target_id = ?
		  AND event_type IN ('extension.staged_pending_approval', 'extension.upgrade_pending_reconsent')
		ORDER BY created_at_ms DESC, seq_no DESC LIMIT 1`), insID,
		).Scan(&stagerID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	return strings.TrimSpace(stagerID) == strings.TrimSpace(actorID), nil
}

func actorHasOrgScopedApproverBinding(actor *rbac.Actor, orgID string) bool {
	if actor == nil {
		return false
	}
	for _, b := range actor.Bindings {
		if b.ScopeType == rbac.ScopeOrg && b.OrgID != nil && *b.OrgID == orgID &&
			(b.RoleName == rbac.RoleOrgOwner || b.RoleName == rbac.RoleSecurityReviewer) {
			return true
		}
	}
	return false
}

func (s *Server) loadExtensionInstallationDTO(ctx context.Context, insID string) (extensionInstallationDTO, string, error) {
	var (
		id, pkgID, canonicalName, trust, state, contractVer, execTier, sigFP, entFP string
		ver                                                                         int64
		actVer, candVer, prevVer                                                    sql.NullString
		entBool, sigBool                                                            port.FlexBool
	)
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT ei.id, ei.package_id, ep.canonical_name, ep.origin_trust_tier, ei.state, ei.version,
		       va.semver, vc.semver, vp.semver,
		       COALESCE(va.contract_version, vc.contract_version, '1.0'),
		       COALESCE(va.execution_tier, vc.execution_tier, 'DECLARATIVE_T1'),
		       COALESCE(va.enterprise_signed, vc.enterprise_signed, `+port.BoolExpr(s.dialect, false)+`),
		       COALESCE(va.signature_verified, vc.signature_verified, `+port.BoolExpr(s.dialect, false)+`),
		       COALESCE(va.signing_key_fingerprint, vc.signing_key_fingerprint, ''),
		       COALESCE(va.enterprise_key_fingerprint, vc.enterprise_key_fingerprint, '')
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions va ON va.id = ei.active_version_id
		LEFT JOIN extension_versions vc ON vc.id = ei.candidate_version_id
		LEFT JOIN extension_versions vp ON vp.id = ei.previous_pinned_version_id
		WHERE ei.id = ?`), insID,
	).Scan(&id, &pkgID, &canonicalName, &trust, &state, &ver, &actVer, &candVer, &prevVer, &contractVer, &execTier, &entBool, &sigBool, &sigFP, &entFP)
	if errors.Is(err, sql.ErrNoRows) {
		return extensionInstallationDTO{}, "", port.ErrNotFound
	}
	if err != nil {
		return extensionInstallationDTO{}, "", err
	}
	liveTrust := trust
	liveSig := bool(sigBool)
	liveEnt := bool(entBool)
	liveFP := strings.ToLower(strings.TrimSpace(sigFP))
	if liveFP == "" && liveEnt {
		liveFP = strings.ToLower(strings.TrimSpace(entFP))
	}
	if s.installer != nil {
		liveTrust, liveSig, liveEnt, liveFP = installer.ComputeKeyringEffectiveProvenance(canonicalName, bool(sigBool), sigFP, bool(entBool), entFP, s.installer.Keyring())
	}
	return extensionInstallationDTO{
		ID:                      id,
		PackageID:               pkgID,
		CanonicalName:           canonicalName,
		ActiveVersion:           sqlNullStrPtr(actVer),
		CandidateVersion:        sqlNullStrPtr(candVer),
		PreviousRollbackVersion: sqlNullStrPtr(prevVer),
		ContractVersion:         contractVer,
		State:                   mapExtensionStateOpenAPI(state),
		OriginTrustTier:         liveTrust,
		SignatureVerified:       liveSig,
		EnterpriseSigned:        liveEnt,
		SigningKeyFingerprint:   liveFP,
		ExecutionTier:           mapExecutionTierOpenAPI(execTier),
		Version:                 ver,
	}, state, nil
}

func (s *Server) loadExtensionPermissionDiffDTO(ctx context.Context, insID string) (extensionPermissionDiffDTO, error) {
	dto, _, err := s.loadExtensionPermissionDiffWithVersion(ctx, insID)
	return dto, err
}

func (s *Server) loadExtensionPermissionDiffWithVersion(ctx context.Context, insID string) (extensionPermissionDiffDTO, int64, error) {
	var (
		pkgID, canonicalName, trust, approvedRaw string
		actSemver, candSemver                    sql.NullString
		candManifest, actManifest                sql.NullString
		candExec, actExec                        sql.NullString
		candEnt, actEnt                          port.FlexBool
		candSig, actSig                          port.FlexBool
		candSigFP, actSigFP                      sql.NullString
		candEntFP, actEntFP                      sql.NullString
		ver                                      int64
	)
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT ei.package_id, ep.canonical_name, ep.origin_trust_tier, ei.approved_permissions_json, ei.version,
		       va.semver, vc.semver, va.manifest_json, vc.manifest_json,
		       va.execution_tier, vc.execution_tier,
		       COALESCE(va.enterprise_signed, `+port.BoolExpr(s.dialect, false)+`), COALESCE(vc.enterprise_signed, `+port.BoolExpr(s.dialect, false)+`),
		       COALESCE(va.signature_verified, `+port.BoolExpr(s.dialect, false)+`), COALESCE(vc.signature_verified, `+port.BoolExpr(s.dialect, false)+`),
		       va.signing_key_fingerprint, vc.signing_key_fingerprint,
		       va.enterprise_key_fingerprint, vc.enterprise_key_fingerprint
		FROM extension_installations ei
		JOIN extension_packages ep ON ep.id = ei.package_id
		LEFT JOIN extension_versions va ON va.id = ei.active_version_id
		LEFT JOIN extension_versions vc ON vc.id = ei.candidate_version_id
		WHERE ei.id = ?`), insID,
	).Scan(&pkgID, &canonicalName, &trust, &approvedRaw, &ver, &actSemver, &candSemver, &actManifest, &candManifest, &actExec, &candExec, &actEnt, &candEnt, &actSig, &candSig, &actSigFP, &candSigFP, &actEntFP, &candEntFP)
	if errors.Is(err, sql.ErrNoRows) {
		return extensionPermissionDiffDTO{}, 0, port.ErrNotFound
	}
	if err != nil {
		return extensionPermissionDiffDTO{}, 0, err
	}

	var approvedPerms installer.PermissionSet
	if strings.TrimSpace(approvedRaw) != "" {
		_ = json.Unmarshal([]byte(approvedRaw), &approvedPerms)
	}

	targetManifest := candManifest
	targetSemver := candSemver
	targetExec := candExec
	targetEnt := bool(candEnt)
	targetSig := bool(candSig)
	targetSigFP := candSigFP.String
	targetEntFP := candEntFP.String
	if !targetManifest.Valid || strings.TrimSpace(targetManifest.String) == "" {
		targetManifest = actManifest
		targetSemver = actSemver
		targetExec = actExec
		targetEnt = bool(actEnt)
		targetSig = bool(actSig)
		targetSigFP = actSigFP.String
		targetEntFP = actEntFP.String
	}

	liveTrust := trust
	liveSig := targetSig
	liveEnt := targetEnt
	liveFP := strings.ToLower(strings.TrimSpace(targetSigFP))
	if liveFP == "" && liveEnt {
		liveFP = strings.ToLower(strings.TrimSpace(targetEntFP))
	}
	if s.installer != nil {
		liveTrust, liveSig, liveEnt, liveFP = installer.ComputeKeyringEffectiveProvenance(canonicalName, targetSig, targetSigFP, targetEnt, targetEntFP, s.installer.Keyring())
	}

	var extManifest installer.ExtensionManifest
	if targetManifest.Valid && strings.TrimSpace(targetManifest.String) != "" {
		_ = json.Unmarshal([]byte(targetManifest.String), &extManifest)
	}

	candHosts := normalizeHostList(extManifest.Permissions.NetworkEgress.AllowedHosts)
	appHosts := normalizeHostList(approvedPerms.AllowedHosts)
	appSet := make(map[string]bool, len(appHosts))
	for _, h := range appHosts {
		appSet[h] = true
	}
	candSet := make(map[string]bool, len(candHosts))
	for _, h := range candHosts {
		candSet[h] = true
	}
	addedHosts := make([]string, 0)
	for _, h := range candHosts {
		if !appSet[h] {
			addedHosts = append(addedHosts, h)
		}
	}
	removedHosts := make([]string, 0)
	for _, h := range appHosts {
		if !candSet[h] {
			removedHosts = append(removedHosts, h)
		}
	}
	activeSlots := append([]string{}, approvedPerms.SecretSlots...)
	sort.Strings(activeSlots)

	var activeQuotas *extensionQuotasDTO
	if actSemver.Valid {
		activeQuotas = &extensionQuotasDTO{
			MaxMemoryMib:        approvedPerms.MaxMemoryMib,
			MaxCPUMsPerCall:     approvedPerms.MaxCpuMsPerCall,
			MaxStreamChunkCPUMs: approvedPerms.MaxStreamChunkCpuMs,
		}
	}
	var activeExecutionTier *string
	if actExec.Valid && strings.TrimSpace(actExec.String) != "" {
		value := mapExecutionTierOpenAPI(actExec.String)
		activeExecutionTier = &value
	}
	candVerStr := "0.0.0"
	if targetSemver.Valid && targetSemver.String != "" {
		candVerStr = targetSemver.String
	}
	execTierStr := installer.ExecutionTierDeclarativeT1
	if targetExec.Valid && targetExec.String != "" {
		execTierStr = targetExec.String
	}
	candidatePerms := permissionSetFromManifest(extManifest, execTierStr)
	permissionDelta := installer.ComputePermissionDelta(permissionDiffBaseline(approvedPerms, actSemver.Valid), candidatePerms)
	candHosts = candidatePerms.AllowedHosts
	slots := candidatePerms.SecretSlots
	addedHosts = permissionDelta.AddedHosts
	addedSlots := permissionDelta.AddedSecretSlots
	quotaEscalated := permissionDelta.MemoryIncreased || permissionDelta.CpuQuotaIncreased
	candidateQuotas := extensionQuotasDTO{
		MaxMemoryMib:        candidatePerms.MaxMemoryMib,
		MaxCPUMsPerCall:     candidatePerms.MaxCpuMsPerCall,
		MaxStreamChunkCPUMs: candidatePerms.MaxStreamChunkCpuMs,
	}
	var activePermissions *installer.PermissionSet
	if actSemver.Valid {
		approved := approvedPerms
		activePermissions = &approved
	}

	return extensionPermissionDiffDTO{
		InstallationID:        insID,
		PackageID:             pkgID,
		ActiveVersion:         sqlNullStrPtr(actSemver),
		CandidateVersion:      candVerStr,
		OriginTrustTier:       liveTrust,
		SignatureVerified:     liveSig,
		EnterpriseSigned:      liveEnt,
		SigningKeyFingerprint: liveFP,
		ExecutionTier:         mapExecutionTierOpenAPI(execTierStr),
		ActiveExecutionTier:   activeExecutionTier,
		ActiveAllowedHosts:    appHosts,
		CandidateAllowedHosts: candHosts,
		AddedAllowedHosts:     addedHosts,
		RemovedAllowedHosts:   removedHosts,
		SecretSlots:           slots,
		ActiveSecretSlots:     activeSlots,
		AddedSecretSlots:      addedSlots,
		QuotaEscalated:        quotaEscalated,
		ActiveQuotas:          activeQuotas,
		Quotas:                candidateQuotas,
		ActivePermissions:     activePermissions,
		CandidatePermissions:  candidatePerms,
		PermissionDelta:       permissionDelta,
		HasEscalation:         permissionDelta.HasEscalation(),
	}, ver, nil
}

func permissionDiffBaseline(approved installer.PermissionSet, hasActiveVersion bool) installer.PermissionSet {
	if !hasActiveVersion && approved.ExecutionTier == "" {
		approved.ExecutionTier = installer.ExecutionTierDeclarativeT1
	}
	return approved
}

func permissionSetFromManifest(manifest installer.ExtensionManifest, executionTier string) installer.PermissionSet {
	slotSet := make(map[string]bool)
	bindingModes := make(map[string]string)
	for _, slot := range manifest.SecretSlots {
		name := strings.TrimSpace(slot.Slot)
		if name != "" {
			slotSet[name] = true
		}
	}
	authModes := make(map[string]string)
	for _, capability := range manifest.Capabilities {
		for _, auth := range capability.AuthMethods {
			authModes[strings.TrimSpace(auth.ID)] = strings.TrimSpace(auth.Mode)
		}
	}
	if rawAuth, ok := manifest.Adapter["auth"].([]any); ok {
		for _, entry := range rawAuth {
			auth, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			mode, _ := auth["mode"].(string)
			name, _ := auth["secretSlot"].(string)
			if name == "" {
				name, _ = auth["slot"].(string)
			}
			name, mode = strings.TrimSpace(name), strings.TrimSpace(mode)
			if name != "" {
				slotSet[name] = true
				if mode != "" {
					bindingModes[name] = mode
				}
			}
		}
	}
	for _, slot := range manifest.SecretSlots {
		name := strings.TrimSpace(slot.Slot)
		for _, authID := range slot.RequiredForAuthMethods {
			if mode := authModes[strings.TrimSpace(authID)]; mode != "" {
				if previous := bindingModes[name]; previous != "" && previous != mode {
					bindingModes[name] = previous + "," + mode
				} else {
					bindingModes[name] = mode
				}
			}
		}
	}
	slots := make([]string, 0, len(slotSet))
	for name := range slotSet {
		slots = append(slots, name)
	}
	sort.Strings(slots)
	var maxMemory, maxCPU, maxChunkCPU int
	if manifest.Runtime != nil && manifest.Runtime.Quotas != nil {
		maxMemory = manifest.Runtime.Quotas.MaxMemoryMib
		maxCPU = manifest.Runtime.Quotas.MaxCpuMsPerCall
		maxChunkCPU = manifest.Runtime.Quotas.MaxStreamChunkCpuMs
	}
	inspectRequest, inspectResponse := false, false
	if manifest.Permissions.PayloadAccess != nil {
		inspectRequest = manifest.Permissions.PayloadAccess.InspectRequestBody
		inspectResponse = manifest.Permissions.PayloadAccess.InspectResponseBody
	}
	var suffixes []string
	if manifest.Adapter != nil {
		if rawSuffixes, ok := manifest.Adapter["endpointHostSuffixes"].([]any); ok {
			for _, item := range rawSuffixes {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					suffixes = append(suffixes, strings.TrimSpace(s))
				}
			}
		} else if strSuffixes, ok := manifest.Adapter["endpointHostSuffixes"].([]string); ok {
			for _, s := range strSuffixes {
				if strings.TrimSpace(s) != "" {
					suffixes = append(suffixes, strings.TrimSpace(s))
				}
			}
		}
	}
	return installer.NormalizePermissionSet(installer.PermissionSet{
		AllowedHosts:         manifest.Permissions.NetworkEgress.AllowedHosts,
		EndpointHostSuffixes: suffixes,
		AllowPrivateIP:       manifest.Permissions.NetworkEgress.AllowPrivateIP,
		RequireTLS:           manifest.Permissions.NetworkEgress.RequireTLS,
		ExecutionTier:        executionTier,
		SecretSlots:          slots,
		SecretBindingModes:   bindingModes,
		Hooks:                manifest.Permissions.Hooks,
		MaxMemoryMib:         maxMemory,
		MaxCpuMsPerCall:      maxCPU,
		MaxStreamChunkCpuMs:  maxChunkCPU,
		InspectRequestBody:   inspectRequest,
		InspectResponseBody:  inspectResponse,
	})
}

func (s *Server) writeInstallerError(w http.ResponseWriter, r *http.Request, err error, currentResource any) {
	switch {
	case errors.Is(err, installer.ErrReservedNamespaceSpoof):
		writeErrorEnvelope(w, r, http.StatusForbidden, "reserved_namespace_spoofing", "permission_error", "reserved namespace is restricted to official root-signed packages", false, nil)
	case errors.Is(err, installer.ErrPermissionDenied):
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "extension installation permission denied", false, nil)
	case errors.Is(err, installer.ErrRevokedExtension):
		writeErrorEnvelope(w, r, http.StatusForbidden, "extension_revoked", "extension_error", "extension package or version has been revoked", false, nil)
	case errors.Is(err, installer.ErrDigestMismatch), errors.Is(err, lockfile.ErrDigestMismatch):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "hash_mismatch", "extension_error", "extension package SHA-256 digest does not match expected value", false, nil)
	case errors.Is(err, installer.ErrUnsupportedContractVersion), errors.Is(err, installer.ErrIncompatibleCoreVersion):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "contract_version_incompatible", "extension_error", "extension contract or core version is incompatible", false, nil)
	case errors.Is(err, installer.ErrUnpinnedGitReference):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "mutable_git_ref_forbidden", "invalid_request_error", "mutable git references are forbidden; pin a 40-hex commit SHA", false, nil)
	case errors.Is(err, installer.ErrUnpinnedOCIDigest), errors.Is(err, installer.ErrUnpinnedReference):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "unpinned_source_reference", "invalid_request_error", "source reference must be immutably pinned by digest", false, nil)
	case errors.Is(err, installer.ErrManifestInvalid),
		errors.Is(err, installer.ErrInvalidArchive),
		errors.Is(err, installer.ErrArchivePathTraversal),
		errors.Is(err, installer.ErrProhibitedArchiveEntry),
		errors.Is(err, installer.ErrTooManyArchiveEntries),
		errors.Is(err, installer.ErrArchiveBomb),
		errors.Is(err, installer.ErrProhibitedCookiePermission),
		errors.Is(err, installer.ErrWildcardEgressForbidden),
		errors.Is(err, installer.ErrProhibitedCapabilityTier),
		errors.Is(err, installer.ErrEntrypointMismatch),
		errors.Is(err, installer.ErrInvalidSignature):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_extension_package", "extension_error", "extension package failed archive, manifest, permission, or signature validation", false, nil)
	case errors.Is(err, port.ErrNotFound):
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension package or installation not found", false, nil)
	case errors.Is(err, installer.ErrMarketplaceArchiveResolverUnavailable):
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "marketplace_archive_resolver_unavailable", "extension_error", "marketplace archive resolver is unavailable", true, nil)
	case errors.Is(err, port.ErrVersionConflict):
		ver := int64(1)
		if ins, ok := currentResource.(extensionInstallationDTO); ok && ins.Version > 0 {
			ver = ins.Version
		}
		writeOptimisticConflict(w, r, ver, currentResource)
	case errors.Is(err, port.ErrResourceInUse):
		writeErrorEnvelope(w, r, http.StatusConflict, "extension_referenced_by_active_connections", "conflict_error", "extension installation is still referenced by active connections", false, nil)
	case errors.Is(err, port.ErrAlreadyExists):
		writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "extension package or candidate version already exists", false, nil)
	case errors.Is(err, installer.ErrNoRollbackTarget):
		writeErrorEnvelope(w, r, http.StatusConflict, "no_rollback_target", "conflict_error", "no previous pinned extension version is available for rollback", false, nil)
	case errors.Is(err, installer.ErrInvalidStateTransition):
		writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", "extension installation state transition is not permitted", false, nil)
	case errors.Is(err, port.ErrInvalidInput):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "invalid extension request parameters", false, nil)
	default:
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "extension_error", "extension installer operation failed", true, nil)
	}
}

func mapExtensionStateOpenAPI(state string) string {
	switch state {
	case installer.StateDiscovered, installer.StateVerifying, installer.StatePendingPermissionApproval:
		return "PENDING_PERMISSION_APPROVAL"
	case installer.StateRolledBack:
		return "ENABLED_ACTIVE"
	case installer.StateInstalledDisabled,
		installer.StateEnabledActive,
		installer.StateUpdatingPendingReconsent,
		installer.StateDraining,
		installer.StateQuarantined,
		installer.StateRejectedInvalid:
		return state
	default:
		return "INSTALLED_DISABLED"
	}
}

func normalizeHostList(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, h := range in {
		canon := strings.ToLower(strings.TrimSpace(h))
		if canon != "" && !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	sort.Strings(out)
	return out
}

func sqlNullStrPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func (s *Server) syncExtensionProviderAdapters(ctx context.Context, installationID string) error {
	if s.catalog == nil || strings.TrimSpace(installationID) == "" {
		return nil
	}
	return s.catalog.SyncExtensionProviderAdapters(ctx, installationID)
}
