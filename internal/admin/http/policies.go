// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	policyengine "github.com/nguywnben/tanpopo/internal/policy/engine"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type policyDocumentDTO struct {
	ID                     string                       `json:"id"`
	ScopeTier              string                       `json:"scope_tier"`
	ScopeID                string                       `json:"scope_id"`
	AllowedDataRegions     []string                     `json:"allowed_data_regions"`
	MinPluginTrust         string                       `json:"min_plugin_trust"`
	MinCapabilityAssurance string                       `json:"min_capability_assurance"`
	RPMCeiling             *int64                       `json:"rpm_ceiling,omitempty"`
	TPMCeiling             *int64                       `json:"tpm_ceiling,omitempty"`
	MaxConcurrentStreams   *int64                       `json:"max_concurrent_streams,omitempty"`
	GuardrailHooks         []snapshot.GuardrailHookSpec `json:"guardrail_hooks,omitempty"`
	Version                int64                        `json:"version"`
}

func (s *Server) handleGetEffectivePolicy(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	projID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	envID := strings.TrimSpace(r.URL.Query().Get("env_id"))
	if envID != "" {
		derivedProj, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envID)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment", true, nil)
			return
		}
		if !found {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
			return
		}
		if projID != "" && projID != derivedProj {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "env_id does not belong to supplied project_id", false, nil)
			return
		}
		projID = derivedProj
	} else if projID != "" {
		var exists int
		err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, projID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating project", true, nil)
			return
		}
	}

	if !s.authorize(w, r, sc, rbac.ActionPolicyRead, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}

	eff, maxVer, err := s.computeEffectivePolicyWithVersionSQL(r.Context(), orgID, projID, envID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed computing effective policy", true, nil)
		return
	}

	scopeTier := "ORGANIZATION"
	scopeID := orgID
	if envID != "" {
		scopeTier = "ENVIRONMENT"
		scopeID = envID
	} else if projID != "" {
		scopeTier = "PROJECT"
		scopeID = projID
	}

	doc := effectivePolicyToDTO("pol_effective_"+scopeID, scopeTier, scopeID, eff, maxVer)
	w.Header().Set("ETag", FormatETag(doc.Version))
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	policyID := strings.TrimSpace(r.PathValue("policyId"))
	if policyID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "policy not found", false, nil)
		return
	}
	var req policyDocumentDTO
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	if s.policies == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "policy_engine_unavailable", "upstream_error", "policy store unavailable", true, nil)
		return
	}

	// Check if policy exists in ANY org first to prevent cross-tenant existence probing (AUTH-15).
	var existingOrgID string
	err := s.db.QueryRowContext(r.Context(), `SELECT org_id FROM policies WHERE id = ?`, policyID).Scan(&existingOrgID)
	policyExists := err == nil
	switch {
	case err == nil && existingOrgID != orgID:
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "policy not found", false, nil)
		return
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking policy", true, nil)
		return
	}

	scopeType, err := mapPolicyScopeTierToDB(req.ScopeTier)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}
	scopeID := strings.TrimSpace(req.ScopeID)
	if scopeID == "" && scopeType == snapshot.PolicyScopeOrg {
		scopeID = orgID
	}
	if scopeID == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_id is required", false, nil)
		return
	}

	targetScope, ancestorEnvID, okScope := s.resolvePolicyTargetScope(w, r, orgID, scopeType, scopeID)
	if !okScope {
		return
	}
	action := rbac.ActionPolicyProjectWrite
	switch strings.ToUpper(strings.TrimSpace(req.ScopeTier)) {
	case "INSTANCE":
		action = rbac.ActionPolicyInstanceWrite
	case "ORGANIZATION":
		action = rbac.ActionPolicyOrgWrite
	case "PROJECT", "ENVIRONMENT":
		action = rbac.ActionPolicyProjectWrite
	}
	if !s.authorize(w, r, sc, action, targetScope) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, targetScope.ProjectID) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}

	rulesJSON, err := encodePolicyRulesFromDTO(&req)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	if !policyExists {
		// Create path on PUT only when If-Match is initial "v1" and caller passed valid policy payload.
		if expectedVer != 1 {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "policy not found", false, nil)
			return
		}
		rec, cerr := s.policies.CreatePolicy(r.Context(), policyengine.CreatePolicyParams{
			ID:             policyID,
			OrgID:          orgID,
			ScopeType:      scopeType,
			ScopeID:        scopeID,
			PolicyType:     inferPolicyTypeFromDTO(&req),
			FailureMode:    snapshot.PolicyFailureClosed,
			RulesJSON:      rulesJSON,
			Enabled:        true,
			ActorKind:      "PRINCIPAL",
			ActorID:        sc.Actor.PrincipalID,
			EntryPoint:     port.AuditEntryPointAdminAPI,
			RequestID:      ptrOrNil(requestIDFromContext(r.Context())),
			AncestorProjID: targetScope.ProjectID,
			AncestorEnvID:  ancestorEnvID,
		})
		if cerr != nil {
			s.writePolicyStoreError(w, r, cerr, nil)
			return
		}
		if s.snapshots != nil {
			_ = s.snapshots.Refresh(r.Context())
		}
		out := policyRecordToDTO(rec)
		w.Header().Set("ETag", FormatETag(out.Version))
		writeJSON(w, http.StatusOK, out)
		return
	}

	cur, err := s.policies.GetPolicy(r.Context(), orgID, policyID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "policy not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading policy", true, nil)
		return
	}
	if cur.ScopeType != scopeType || cur.ScopeID != scopeID {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_tier and scope_id cannot be mutated on existing policy", false, nil)
		return
	}
	if cur.Version != expectedVer {
		writeOptimisticConflict(w, r, cur.Version, policyRecordToDTO(cur))
		return
	}

	updated, err := s.policies.UpdatePolicy(r.Context(), orgID, policyID, expectedVer, policyengine.UpdatePolicyParams{
		RulesJSON:      &rulesJSON,
		ActorKind:      "PRINCIPAL",
		ActorID:        sc.Actor.PrincipalID,
		EntryPoint:     port.AuditEntryPointAdminAPI,
		RequestID:      ptrOrNil(requestIDFromContext(r.Context())),
		AncestorProjID: targetScope.ProjectID,
		AncestorEnvID:  ancestorEnvID,
	})
	if err != nil {
		latest, _ := s.policies.GetPolicy(r.Context(), orgID, policyID)
		var curRes any
		if latest != nil {
			curRes = policyRecordToDTO(latest)
		}
		s.writePolicyStoreError(w, r, err, curRes)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	out := policyRecordToDTO(updated)
	w.Header().Set("ETag", FormatETag(out.Version))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) computeEffectivePolicySQL(ctx context.Context, orgID, projID, envID string) (policyengine.EffectivePolicy, error) {
	eff, _, err := s.computeEffectivePolicyWithVersionSQL(ctx, orgID, projID, envID)
	return eff, err
}

func (s *Server) computeEffectivePolicyWithVersionSQL(ctx context.Context, orgID, projID, envID string) (policyengine.EffectivePolicy, int64, error) {
	var minTrust string
	var orgVer int64
	err := s.db.QueryRowContext(ctx, `SELECT min_plugin_trust, version FROM organizations WHERE id = ?`, orgID).Scan(&minTrust, &orgVer)
	if errors.Is(err, sql.ErrNoRows) {
		return policyengine.EffectivePolicy{}, 0, port.ErrNotFound
	}
	if err != nil {
		return policyengine.EffectivePolicy{}, 0, err
	}

	var layers []policyengine.ScopedLayer
	if strings.TrimSpace(minTrust) != "" {
		layers = append(layers, policyengine.ScopedLayer{
			ScopeType: snapshot.PolicyScopeOrg,
			ScopeID:   orgID,
			Baseline:  true,
			Rules:     snapshot.PolicyRulesView{MinPluginTrust: minTrust},
		})
	}
	if projID != "" {
		var enforcedReg sql.NullString
		if qerr := s.db.QueryRowContext(ctx, `SELECT enforced_region FROM projects WHERE org_id = ? AND id = ?`, orgID, projID).Scan(&enforcedReg); qerr == nil && enforcedReg.Valid && strings.TrimSpace(enforcedReg.String) != "" {
			layers = append(layers, policyengine.ScopedLayer{
				ScopeType: snapshot.PolicyScopeProject,
				ScopeID:   projID,
				Baseline:  true,
				Rules: snapshot.PolicyRulesView{
					HasAllowedDataRegions: true,
					AllowedDataRegions:    []string{strings.ToUpper(strings.TrimSpace(enforcedReg.String))},
				},
			})
		}
	}
	if envID != "" {
		var reqTested int
		if qerr := s.db.QueryRowContext(ctx, `SELECT require_tested_caps FROM environments WHERE org_id = ? AND id = ?`, orgID, envID).Scan(&reqTested); qerr == nil && reqTested != 0 {
			layers = append(layers, policyengine.ScopedLayer{
				ScopeType: snapshot.PolicyScopeEnv,
				ScopeID:   envID,
				Baseline:  true,
				Rules:     snapshot.PolicyRulesView{MinCapabilityAssurance: policyengine.AssuranceTested},
			})
		}
	}

	maxVer := orgVer
	rows, err := s.db.QueryContext(ctx, `
		SELECT scope_type, scope_id, rules_json, version
		FROM policies
		WHERE org_id = ? AND enabled = 1 AND scope_type IN ('ORG', 'PROJECT', 'ENV')`, orgID,
	)
	if err != nil {
		return policyengine.EffectivePolicy{}, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var st, sid, rj string
		var ver int64
		if scanErr := rows.Scan(&st, &sid, &rj, &ver); scanErr != nil {
			return policyengine.EffectivePolicy{}, 0, scanErr
		}
		match := (st == snapshot.PolicyScopeOrg && sid == orgID) ||
			(st == snapshot.PolicyScopeProject && projID != "" && sid == projID) ||
			(st == snapshot.PolicyScopeEnv && envID != "" && sid == envID)
		if !match {
			continue
		}
		parsed, perr := snapshot.ParsePolicyRulesJSONStrict(rj)
		if perr != nil {
			return policyengine.EffectivePolicy{}, 0, perr
		}
		if ver > maxVer {
			maxVer = ver
		}
		layers = append(layers, policyengine.ScopedLayer{
			ScopeType: st,
			ScopeID:   sid,
			Rules:     parsed,
		})
	}
	if err := rows.Err(); err != nil {
		return policyengine.EffectivePolicy{}, 0, err
	}
	return policyengine.MergeLayers(layers...), maxVer, nil
}

func (s *Server) resolvePolicyTargetScope(w http.ResponseWriter, r *http.Request, orgID, scopeType, scopeID string) (rbac.TargetScope, string, bool) {
	switch scopeType {
	case snapshot.PolicyScopeOrg:
		if scopeID != orgID {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
			return rbac.TargetScope{}, "", false
		}
		return rbac.TargetScope{OrgID: orgID}, "", true
	case snapshot.PolicyScopeProject:
		var exists int
		err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM projects WHERE org_id = ? AND id = ?`, orgID, scopeID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
			return rbac.TargetScope{}, "", false
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating project", true, nil)
			return rbac.TargetScope{}, "", false
		}
		return rbac.TargetScope{OrgID: orgID, ProjectID: scopeID}, "", true
	case snapshot.PolicyScopeEnv:
		projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, scopeID)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment", true, nil)
			return rbac.TargetScope{}, "", false
		}
		if !found {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
			return rbac.TargetScope{}, "", false
		}
		return rbac.TargetScope{OrgID: orgID, ProjectID: projID}, scopeID, true
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "unsupported policy scope_tier", false, nil)
		return rbac.TargetScope{}, "", false
	}
}

func (s *Server) writePolicyStoreError(w http.ResponseWriter, r *http.Request, err error, currentResource any) {
	switch {
	case errors.Is(err, policyengine.ErrPolicyRelaxationForbidden):
		writeErrorEnvelope(w, r, http.StatusUnprocessableEntity, "policy_relaxation_forbidden", "invalid_request_error", err.Error(), false, nil)
	case errors.Is(err, port.ErrVersionConflict):
		ver := int64(1)
		if pd, ok := currentResource.(policyDocumentDTO); ok && pd.Version > 0 {
			ver = pd.Version
		}
		writeOptimisticConflict(w, r, ver, currentResource)
	case errors.Is(err, port.ErrNotFound):
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "policy scope target not found", false, nil)
	case errors.Is(err, port.ErrAlreadyExists):
		writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "policy already exists for target scope", false, nil)
	case errors.Is(err, policyengine.ErrInvalidPolicyInput), errors.Is(err, port.ErrInvalidInput):
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
	default:
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed saving policy", true, nil)
	}
}

func mapPolicyScopeTierToDB(tier string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case "INSTANCE", "ORGANIZATION", "ORG":
		return snapshot.PolicyScopeOrg, nil
	case "PROJECT":
		return snapshot.PolicyScopeProject, nil
	case "ENVIRONMENT", "ENV":
		return snapshot.PolicyScopeEnv, nil
	default:
		return "", errors.New("scope_tier must be INSTANCE, ORGANIZATION, PROJECT, or ENVIRONMENT")
	}
}

func mapPolicyScopeDBToTier(scopeType string) string {
	switch scopeType {
	case snapshot.PolicyScopeProject:
		return "PROJECT"
	case snapshot.PolicyScopeEnv:
		return "ENVIRONMENT"
	default:
		return "ORGANIZATION"
	}
}

func inferPolicyTypeFromDTO(dto *policyDocumentDTO) string {
	if len(dto.AllowedDataRegions) > 0 {
		return snapshot.PolicyTypeDataResidency
	}
	if len(dto.GuardrailHooks) > 0 {
		return snapshot.PolicyTypeGuardrail
	}
	if dto.RPMCeiling != nil || dto.TPMCeiling != nil || dto.MaxConcurrentStreams != nil {
		return snapshot.PolicyTypeRateLimit
	}
	return snapshot.PolicyTypeModelAllowlist
}

func encodePolicyRulesFromDTO(dto *policyDocumentDTO) (string, error) {
	payload := make(map[string]any)
	if len(dto.AllowedDataRegions) > 0 {
		regions := make([]string, 0, len(dto.AllowedDataRegions))
		for _, r := range dto.AllowedDataRegions {
			reg := strings.ToUpper(strings.TrimSpace(r))
			if !isValidDataRegion(reg) {
				return "", errors.New("allowed_data_regions must contain EU, US, APAC, or ON_PREM")
			}
			regions = append(regions, reg)
		}
		payload["allowed_data_regions"] = regions
	}
	if strings.TrimSpace(dto.MinPluginTrust) != "" {
		payload["min_plugin_trust"] = strings.ToUpper(strings.TrimSpace(dto.MinPluginTrust))
	}
	if strings.TrimSpace(dto.MinCapabilityAssurance) != "" {
		payload["min_capability_assurance"] = strings.ToUpper(strings.TrimSpace(dto.MinCapabilityAssurance))
	}
	if dto.RPMCeiling != nil {
		payload["rpm_ceiling"] = *dto.RPMCeiling
	}
	if dto.TPMCeiling != nil {
		payload["tpm_ceiling"] = *dto.TPMCeiling
	}
	if dto.MaxConcurrentStreams != nil {
		payload["max_concurrent_streams"] = *dto.MaxConcurrentStreams
	}
	if len(dto.GuardrailHooks) > 0 {
		payload["guardrail_hooks"] = dto.GuardrailHooks
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if _, perr := snapshot.ParsePolicyRulesJSONStrict(string(raw)); perr != nil {
		return "", perr
	}
	return string(raw), nil
}

func effectivePolicyToDTO(id, scopeTier, scopeID string, eff policyengine.EffectivePolicy, version int64) policyDocumentDTO {
	regions := []string{"EU", "US", "APAC", "ON_PREM"}
	if eff.HasAllowedDataRegions {
		regions = append([]string(nil), eff.AllowedDataRegions...)
	}
	minTrust := eff.MinPluginTrust
	if minTrust == "" {
		minTrust = policyengine.MinTrustAnyApproved
	}
	minAssurance := eff.MinCapabilityAssurance
	if minAssurance == "" {
		minAssurance = policyengine.AssuranceDeclared
	}
	hooks := eff.GuardrailHooks
	if hooks == nil {
		hooks = []snapshot.GuardrailHookSpec{}
	}
	if version < 1 {
		version = 1
	}
	return policyDocumentDTO{
		ID:                     id,
		ScopeTier:              scopeTier,
		ScopeID:                scopeID,
		AllowedDataRegions:     regions,
		MinPluginTrust:         minTrust,
		MinCapabilityAssurance: minAssurance,
		RPMCeiling:             eff.RPMLimit,
		TPMCeiling:             eff.TPMLimit,
		MaxConcurrentStreams:   eff.MaxConcurrentStreams,
		GuardrailHooks:         hooks,
		Version:                version,
	}
}

func policyRecordToDTO(rec *policyengine.PolicyRecord) policyDocumentDTO {
	r := rec.ParsedRules
	regions := []string{"EU", "US", "APAC", "ON_PREM"}
	if r.HasAllowedDataRegions {
		regions = append([]string(nil), r.AllowedDataRegions...)
	}
	minTrust := r.MinPluginTrust
	if minTrust == "" {
		minTrust = policyengine.MinTrustAnyApproved
	}
	minAssurance := r.MinCapabilityAssurance
	if minAssurance == "" {
		minAssurance = policyengine.AssuranceDeclared
	}
	hooks := r.GuardrailHooks
	if hooks == nil {
		hooks = []snapshot.GuardrailHookSpec{}
	}
	return policyDocumentDTO{
		ID:                     rec.ID,
		ScopeTier:              mapPolicyScopeDBToTier(rec.ScopeType),
		ScopeID:                rec.ScopeID,
		AllowedDataRegions:     regions,
		MinPluginTrust:         minTrust,
		MinCapabilityAssurance: minAssurance,
		RPMCeiling:             r.RPMLimit,
		TPMCeiling:             r.TPMLimit,
		MaxConcurrentStreams:   r.MaxConcurrentStreams,
		GuardrailHooks:         hooks,
		Version:                rec.Version,
	}
}
