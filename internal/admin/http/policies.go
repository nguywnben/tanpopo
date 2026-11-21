// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
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
	ID                         string                       `json:"id"`
	ScopeTier                  string                       `json:"scope_tier"`
	ScopeID                    string                       `json:"scope_id"`
	AllowedDataRegions         []string                     `json:"allowed_data_regions"`
	AllowedModels              *[]string                    `json:"allowed_models,omitempty"`
	MinPluginTrust             string                       `json:"min_plugin_trust"`
	MinCapabilityAssurance     string                       `json:"min_capability_assurance"`
	RPMCeiling                 *int64                       `json:"rpm_ceiling,omitempty"`
	TPMCeiling                 *int64                       `json:"tpm_ceiling,omitempty"`
	MaxConcurrentStreams       *int64                       `json:"max_concurrent_streams,omitempty"`
	ResponseCacheEnabled       *bool                        `json:"response_cache_enabled,omitempty"`
	ResponseCacheTTLSeconds    *int64                       `json:"response_cache_ttl_seconds,omitempty"`
	ResponseCacheMaxEntryBytes *int64                       `json:"response_cache_max_entry_bytes,omitempty"`
	GuardrailHooks             []snapshot.GuardrailHookSpec `json:"guardrail_hooks,omitempty"`
	Version                    int64                        `json:"version"`
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
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM projects WHERE org_id = ? AND id = ?`), orgID, projID).Scan(&exists)
		})
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

	eff, _, err := s.computeEffectivePolicyWithVersionSQL(r.Context(), orgID, projID, envID)
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
	scopeDB, scopeErr := mapPolicyScopeTierToDB(scopeTier)
	if scopeErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed resolving writable policy scope", true, nil)
		return
	}

	writePolicyID := scopePolicyWriteID(orgID, scopeDB, scopeID)
	writeVersion := int64(1)
	var ids []string
	var versions []int64
	if err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		storedRows, qerr := q.QueryContext(r.Context(), s.rebind(`SELECT id, version FROM policies WHERE org_id = ? AND scope_type = ? AND scope_id = ? ORDER BY policy_type, id`), orgID, scopeDB, scopeID)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = storedRows.Close() }()
		for storedRows.Next() {
			var id string
			var version int64
			if serr := storedRows.Scan(&id, &version); serr != nil {
				return serr
			}
			ids, versions = append(ids, id), append(versions, version)
		}
		if ierr := storedRows.Err(); ierr != nil {
			return ierr
		}
		return storedRows.Close()
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed resolving writable policy version", true, nil)
		return
	}
	switch len(ids) {
	case 0:
	case 1:
		writePolicyID, writeVersion = ids[0], versions[0]
	default:
		writeVersion = 0
		for _, version := range versions {
			writeVersion += version
		}
	}
	doc := effectivePolicyToDTO(writePolicyID, scopeTier, scopeID, eff, writeVersion)
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
	if s.handleScopePolicyUpdate(w, r, sc, orgID, policyID, &req) {
		return
	}

	if s.policies == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "policy_engine_unavailable", "upstream_error", "policy store unavailable", true, nil)
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionPolicyProjectWrite, rbac.ActionPolicyOrgWrite, rbac.ActionPolicyInstanceWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	// Check if policy exists; foreign-org rows follow the create/auth path and return uniform 404 after RBAC + validation (AUTH-15).
	var existingOrgID, existingScopeType, existingScopeID string
	err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`
		SELECT org_id, scope_type, scope_id
		FROM policies
		WHERE id = ?`), policyID,
		).Scan(&existingOrgID, &existingScopeType, &existingScopeID)
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking policy", true, nil)
		return
	}
	policyExists := err == nil && existingOrgID == orgID
	foreignOrgExists := err == nil && existingOrgID != orgID

	var (
		scopeType     string
		scopeID       string
		targetScope   rbac.TargetScope
		ancestorEnvID string
		expectedVer   int64
	)

	if policyExists {
		var okScope bool
		targetScope, ancestorEnvID, okScope = s.resolvePolicyTargetScope(w, r, orgID, existingScopeType, existingScopeID)
		if !okScope {
			return
		}
		action := rbac.ActionPolicyProjectWrite
		switch existingScopeType {
		case snapshot.PolicyScopeOrg:
			action = rbac.ActionPolicyOrgWrite
		case snapshot.PolicyScopeProject, snapshot.PolicyScopeEnv:
			action = rbac.ActionPolicyProjectWrite
		}
		if !s.authorize(w, r, sc, action, targetScope) {
			return
		}
		if !s.requireActiveOrgAndProject(w, r, orgID, targetScope.ProjectID) {
			return
		}
		var okVer bool
		expectedVer, okVer = requireIfMatch(w, r)
		if !okVer {
			return
		}
		scopeType, err = mapPolicyScopeTierToDB(req.ScopeTier)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		}
		scopeID = strings.TrimSpace(req.ScopeID)
		if scopeID == "" && scopeType == snapshot.PolicyScopeOrg {
			scopeID = orgID
		}
		if scopeID == "" {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_id is required", false, nil)
			return
		}
		if existingScopeType != scopeType || existingScopeID != scopeID {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_tier and scope_id cannot be mutated on existing policy", false, nil)
			return
		}
	} else {
		if strings.EqualFold(strings.TrimSpace(req.ScopeTier), "INSTANCE") {
			if !s.authorize(w, r, sc, rbac.ActionPolicyInstanceWrite, rbac.TargetScope{}) {
				return
			}
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "INSTANCE-tier policies cannot be stored via /admin/v1/policies/{policyId}; use ORGANIZATION, PROJECT, or ENVIRONMENT", false, nil)
			return
		}
		scopeType, err = mapPolicyScopeTierToDB(req.ScopeTier)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		}
		scopeID = strings.TrimSpace(req.ScopeID)
		if scopeID == "" && scopeType == snapshot.PolicyScopeOrg {
			scopeID = orgID
		}
		if scopeID == "" {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_id is required", false, nil)
			return
		}
		switch scopeType {
		case snapshot.PolicyScopeOrg:
			if !s.authorize(w, r, sc, rbac.ActionPolicyOrgWrite, rbac.TargetScope{OrgID: orgID}) {
				return
			}
		case snapshot.PolicyScopeProject:
			if !s.authorize(w, r, sc, rbac.ActionPolicyProjectWrite, rbac.TargetScope{OrgID: orgID, ProjectID: scopeID}) {
				return
			}
		}
		var okScope bool
		targetScope, ancestorEnvID, okScope = s.resolvePolicyTargetScope(w, r, orgID, scopeType, scopeID)
		if !okScope {
			return
		}
		if scopeType == snapshot.PolicyScopeEnv {
			if !s.authorize(w, r, sc, rbac.ActionPolicyProjectWrite, targetScope) {
				return
			}
		}
		if !s.requireActiveOrgAndProject(w, r, orgID, targetScope.ProjectID) {
			return
		}
		var okVer bool
		expectedVer, okVer = requireIfMatch(w, r)
		if !okVer {
			return
		}
	}

	rulesJSON, err := encodePolicyRulesFromDTO(&req)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return
	}

	if !policyExists {
		// Create path on PUT only when policy does not exist in any org, If-Match is initial "v1", and caller passed valid policy payload.
		if foreignOrgExists || expectedVer != 1 {
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
		s.publishChange(r.Context(), snapshot.ChangeEvent{
			OrgID:        orgID,
			EntityKind:   "policy",
			EntityID:     rec.ID,
			MutationType: port.MutationUpsert,
		})
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
	s.publishChange(r.Context(), snapshot.ChangeEvent{
		OrgID:        orgID,
		EntityKind:   "policy",
		EntityID:     updated.ID,
		MutationType: port.MutationUpsert,
	})

	out := policyRecordToDTO(updated)
	w.Header().Set("ETag", FormatETag(out.Version))
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleScopePolicyUpdate(w http.ResponseWriter, r *http.Request, sc *authSessionContext, orgID, policyID string, req *policyDocumentDTO) bool {
	scopeType, err := mapPolicyScopeTierToDB(req.ScopeTier)
	if err != nil {
		return false
	}
	scopeID := strings.TrimSpace(req.ScopeID)
	if scopeID == "" && scopeType == snapshot.PolicyScopeOrg {
		scopeID = orgID
	}
	if scopeID == "" || policyID != scopePolicyWriteID(orgID, scopeType, scopeID) {
		return false
	}
	var count int
	if err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT COUNT(*) FROM policies WHERE org_id = ? AND scope_type = ? AND scope_id = ?`), orgID, scopeType, scopeID).Scan(&count)
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed resolving policy scope", true, nil)
		return true
	}
	if count < 2 {
		return false
	}
	targetScope, ancestorEnvID, ok := s.resolvePolicyTargetScope(w, r, orgID, scopeType, scopeID)
	if !ok {
		return true
	}
	action := rbac.ActionPolicyProjectWrite
	if scopeType == snapshot.PolicyScopeOrg {
		action = rbac.ActionPolicyOrgWrite
	}
	if !s.authorize(w, r, sc, action, targetScope) || !s.requireActiveOrgAndProject(w, r, orgID, targetScope.ProjectID) {
		return true
	}
	expectedVersion, ok := requireIfMatch(w, r)
	if !ok {
		return true
	}
	rulesJSON, err := encodePolicyRulesFromDTO(req)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		return true
	}
	updated, err := s.policies.UpdateScopePolicies(r.Context(), orgID, scopeType, scopeID, expectedVersion, policyengine.UpdatePolicyParams{
		RulesJSON: &rulesJSON, ActorKind: "PRINCIPAL", ActorID: sc.Actor.PrincipalID, EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID: ptrOrNil(requestIDFromContext(r.Context())), AncestorProjID: targetScope.ProjectID, AncestorEnvID: ancestorEnvID,
	})
	if err != nil {
		var currentResource any
		if errors.Is(err, port.ErrVersionConflict) {
			var currentVersion int64
			_ = s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
				rows, queryErr := dbq.QueryContext(r.Context(), s.rebind(`SELECT version FROM policies WHERE org_id = ? AND scope_type = ? AND scope_id = ?`), orgID, scopeType, scopeID)
				if queryErr != nil {
					return queryErr
				}
				defer rows.Close()
				for rows.Next() {
					var version int64
					if rows.Scan(&version) != nil {
						currentVersion = 0
						return nil
					}
					currentVersion += version
				}
				if rows.Err() != nil {
					currentVersion = 0
				}
				return nil
			})
			projID, envID := "", ""
			if scopeType == snapshot.PolicyScopeProject {
				projID = scopeID
			} else if scopeType == snapshot.PolicyScopeEnv {
				envID, projID = scopeID, targetScope.ProjectID
			}
			if currentVersion > 0 {
				if effective, _, viewErr := s.computeEffectivePolicyWithVersionSQL(r.Context(), orgID, projID, envID); viewErr == nil {
					currentResource = effectivePolicyToDTO(policyID, mapPolicyScopeDBToTier(scopeType), scopeID, effective, currentVersion)
				}
			}
		}
		s.writePolicyStoreError(w, r, err, currentResource)
		return true
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	s.publishChange(r.Context(), snapshot.ChangeEvent{
		OrgID:        orgID,
		EntityKind:   "policy",
		EntityID:     policyID,
		MutationType: port.MutationUpsert,
	})
	projID, envID := "", ""
	scopeTier := mapPolicyScopeDBToTier(scopeType)
	if scopeType == snapshot.PolicyScopeProject {
		projID = scopeID
	} else if scopeType == snapshot.PolicyScopeEnv {
		envID, projID = scopeID, targetScope.ProjectID
	}
	effective, _, err := s.computeEffectivePolicyWithVersionSQL(r.Context(), orgID, projID, envID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "policy saved but effective view could not be refreshed", true, nil)
		return true
	}
	doc := effectivePolicyToDTO(policyID, scopeTier, scopeID, effective, updated.Version)
	w.Header().Set("ETag", FormatETag(doc.Version))
	writeJSON(w, http.StatusOK, doc)
	return true
}

func scopePolicyWriteID(orgID, scopeType, scopeID string) string {
	digest := sha256.Sum256([]byte(orgID + "\x00" + scopeType + "\x00" + scopeID))
	encoding := base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)
	return "pol_" + strings.ToLower(encoding.EncodeToString(digest[:16]))
}

func (s *Server) computeEffectivePolicySQL(ctx context.Context, orgID, projID, envID string) (policyengine.EffectivePolicy, error) {
	eff, _, err := s.computeEffectivePolicyWithVersionSQL(ctx, orgID, projID, envID)
	return eff, err
}

func (s *Server) computeEffectivePolicyWithVersionSQL(ctx context.Context, orgID, projID, envID string) (policyengine.EffectivePolicy, int64, error) {
	var minTrust string
	var orgVer int64
	var layers []policyengine.ScopedLayer
	var maxVer int64

	err := s.withOrgReadTx(ctx, orgID, func(dbq adminDBQuerier) error {
		qerr := dbq.QueryRowContext(ctx, s.rebind(`SELECT min_plugin_trust, version FROM organizations WHERE id = ?`), orgID).Scan(&minTrust, &orgVer)
		if errors.Is(qerr, sql.ErrNoRows) {
			return port.ErrNotFound
		}
		if qerr != nil {
			return qerr
		}

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
			if perr := dbq.QueryRowContext(ctx, s.rebind(`SELECT enforced_region FROM projects WHERE org_id = ? AND id = ?`), orgID, projID).Scan(&enforcedReg); perr == nil && enforcedReg.Valid && strings.TrimSpace(enforcedReg.String) != "" {
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
			var reqTested port.FlexBool
			if eerr := dbq.QueryRowContext(ctx, s.rebind(`SELECT require_tested_caps FROM environments WHERE org_id = ? AND id = ?`), orgID, envID).Scan(&reqTested); eerr == nil && bool(reqTested) {
				layers = append(layers, policyengine.ScopedLayer{
					ScopeType: snapshot.PolicyScopeEnv,
					ScopeID:   envID,
					Baseline:  true,
					Rules:     snapshot.PolicyRulesView{MinCapabilityAssurance: policyengine.AssuranceTested},
				})
			}
		}

		maxVer = orgVer
		rows, rerr := dbq.QueryContext(ctx, s.rebind(`
			SELECT scope_type, scope_id, rules_json, version
			FROM policies
			WHERE org_id = ? AND enabled = `+port.BoolExpr(s.dialect, true)+` AND scope_type IN ('ORG', 'PROJECT', 'ENV')`), orgID,
		)
		if rerr != nil {
			return rerr
		}
		defer rows.Close()

		for rows.Next() {
			var st, sid, rj string
			var ver int64
			if scanErr := rows.Scan(&st, &sid, &rj, &ver); scanErr != nil {
				return scanErr
			}
			match := (st == snapshot.PolicyScopeOrg && sid == orgID) ||
				(st == snapshot.PolicyScopeProject && projID != "" && sid == projID) ||
				(st == snapshot.PolicyScopeEnv && envID != "" && sid == envID)
			if !match {
				continue
			}
			parsed, perr := snapshot.ParsePolicyRulesJSONStrict(rj)
			if perr != nil {
				return perr
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
		return rows.Err()
	})
	if err != nil {
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
		err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
			return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM projects WHERE org_id = ? AND id = ?`), orgID, scopeID).Scan(&exists)
		})
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
	case "ORGANIZATION", "ORG":
		return snapshot.PolicyScopeOrg, nil
	case "PROJECT":
		return snapshot.PolicyScopeProject, nil
	case "ENVIRONMENT", "ENV":
		return snapshot.PolicyScopeEnv, nil
	default:
		return "", errors.New("scope_tier must be ORGANIZATION, PROJECT, or ENVIRONMENT")
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
	if dto.ResponseCacheEnabled != nil || dto.ResponseCacheTTLSeconds != nil || dto.ResponseCacheMaxEntryBytes != nil {
		return snapshot.PolicyTypeResponseCache
	}
	return snapshot.PolicyTypeModelAllowlist
}

func encodePolicyRulesFromDTO(dto *policyDocumentDTO) (string, error) {
	payload := make(map[string]any)
	if dto.AllowedModels != nil {
		models := make([]string, 0, len(*dto.AllowedModels))
		for _, raw := range *dto.AllowedModels {
			model := strings.TrimSpace(raw)
			if model == "" {
				return "", errors.New("allowed_models must not contain empty entries")
			}
			models = append(models, model)
		}
		payload["allowed_models"] = models
	}
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
	if dto.ResponseCacheEnabled != nil {
		payload["response_cache_enabled"] = *dto.ResponseCacheEnabled
	}
	if dto.ResponseCacheTTLSeconds != nil {
		payload["response_cache_ttl_seconds"] = *dto.ResponseCacheTTLSeconds
	}
	if dto.ResponseCacheMaxEntryBytes != nil {
		payload["response_cache_max_entry_bytes"] = *dto.ResponseCacheMaxEntryBytes
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
	var cacheEnabledPtr *bool
	if eff.HasResponseCache {
		b := eff.ResponseCacheEnabled
		cacheEnabledPtr = &b
	}
	return policyDocumentDTO{
		ID:                         id,
		ScopeTier:                  scopeTier,
		ScopeID:                    scopeID,
		AllowedDataRegions:         regions,
		AllowedModels:              effectiveModelAllowlist(eff),
		MinPluginTrust:             minTrust,
		MinCapabilityAssurance:     minAssurance,
		RPMCeiling:                 eff.RPMLimit,
		TPMCeiling:                 eff.TPMLimit,
		MaxConcurrentStreams:       eff.MaxConcurrentStreams,
		ResponseCacheEnabled:       cacheEnabledPtr,
		ResponseCacheTTLSeconds:    eff.ResponseCacheTTLSeconds,
		ResponseCacheMaxEntryBytes: eff.ResponseCacheMaxEntryBytes,
		GuardrailHooks:             hooks,
		Version:                    version,
	}
}

func effectiveModelAllowlist(eff policyengine.EffectivePolicy) *[]string {
	if !eff.HasAllowedModels {
		return nil
	}
	models := append([]string{}, eff.AllowedModels...)
	return &models
}

func recordModelAllowlist(r snapshot.PolicyRulesView) *[]string {
	if !r.HasAllowedModels {
		return nil
	}
	models := append([]string{}, r.AllowedModels...)
	return &models
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
	var cacheEnabledPtr *bool
	if r.HasResponseCache {
		b := r.ResponseCacheEnabled
		cacheEnabledPtr = &b
	}
	return policyDocumentDTO{
		ID:                         rec.ID,
		ScopeTier:                  mapPolicyScopeDBToTier(rec.ScopeType),
		ScopeID:                    rec.ScopeID,
		AllowedDataRegions:         regions,
		AllowedModels:              recordModelAllowlist(r),
		MinPluginTrust:             minTrust,
		MinCapabilityAssurance:     minAssurance,
		RPMCeiling:                 r.RPMLimit,
		TPMCeiling:                 r.TPMLimit,
		MaxConcurrentStreams:       r.MaxConcurrentStreams,
		ResponseCacheEnabled:       cacheEnabledPtr,
		ResponseCacheTTLSeconds:    r.ResponseCacheTTLSeconds,
		ResponseCacheMaxEntryBytes: r.ResponseCacheMaxEntryBytes,
		GuardrailHooks:             hooks,
		Version:                    rec.Version,
	}
}
