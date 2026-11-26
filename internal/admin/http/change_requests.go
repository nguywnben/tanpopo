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
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/extension/lockfile"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/security/foureyes"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type initiateChangeRequestBody struct {
	OrgID      string          `json:"org_id,omitempty"`
	ActionType string          `json:"action_type"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Payload    json.RawMessage `json:"payload"`
}

type decideChangeRequestBody struct {
	OrgID    string `json:"org_id,omitempty"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (s *Server) handleListChangeRequests(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.fourEyes == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "foureyes_unavailable", "api_error", "four-eyes governance service unavailable", true, nil)
		return
	}

	orgID, orgPtr, ok := s.resolveChangeRequestScope(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionApprovalRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	limit := 50
	if rawLim := strings.TrimSpace(r.URL.Query().Get("limit")); rawLim != "" {
		if parsed, perr := strconv.Atoi(rawLim); perr == nil && parsed > 0 {
			limit = parsed
		}
	}
	stateFilter := strings.TrimSpace(r.URL.Query().Get("state"))
	items, err := s.fourEyes.List(r.Context(), orgPtr, stateFilter, limit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing change requests", true, nil)
		return
	}
	if items == nil {
		items = []foureyes.ChangeRequest{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (s *Server) handleInitiateChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	if s.fourEyes == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "foureyes_unavailable", "api_error", "four-eyes governance service unavailable", true, nil)
		return
	}

	var req initiateChangeRequestBody
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, orgPtr, ok := s.resolveChangeRequestScope(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionApprovalDecide, rbac.TargetScope{OrgID: orgID}) {
		return
	}

	action := strings.ToUpper(strings.TrimSpace(req.ActionType))
	if !foureyes.IsValidActionType(action) {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "unsupported action_type for change request", false, nil)
		return
	}
	targetType := strings.TrimSpace(req.TargetType)
	targetID := strings.TrimSpace(req.TargetID)
	if targetType == "" || targetID == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_type and target_id are required", false, nil)
		return
	}
	if !s.authorizeChangeRequestAction(w, r, sc, orgPtr, action, targetType, targetID) {
		return
	}

	cr, err := s.fourEyes.Initiate(r.Context(), foureyes.InitiateParams{
		OrgID:                orgPtr,
		ActionType:           action,
		TargetType:           targetType,
		TargetID:             targetID,
		PayloadJSON:          req.Payload,
		InitiatorPrincipalID: sc.Actor.PrincipalID,
		RequestID:            ptrOrNil(requestIDFromContext(r.Context())),
	})
	if err != nil {
		if errors.Is(err, port.ErrInvalidInput) {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating change request", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(cr.Version))
	writeJSON(w, http.StatusAccepted, cr)
}

func (s *Server) handleDecideChangeRequest(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	if s.fourEyes == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "foureyes_unavailable", "api_error", "four-eyes governance service unavailable", true, nil)
		return
	}
	reqID := strings.TrimSpace(r.PathValue("requestId"))
	if reqID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "change request not found", false, nil)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req decideChangeRequestBody
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	orgID, orgPtr, ok := s.resolveChangeRequestScope(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionApprovalDecide, rbac.TargetScope{OrgID: orgID}) {
		return
	}

	decision := strings.ToUpper(strings.TrimSpace(req.Decision))
	if decision != foureyes.DecisionApprove && decision != foureyes.DecisionReject {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "decision must be APPROVE or REJECT", false, nil)
		return
	}

	curCR, gerr := s.fourEyes.Get(r.Context(), orgPtr, reqID)
	if errors.Is(gerr, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "change request not found", false, nil)
		return
	}
	if gerr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading change request", true, nil)
		return
	}
	if decision == foureyes.DecisionApprove {
		if !s.authorizeChangeRequestAction(w, r, sc, curCR.OrgID, curCR.ActionType, curCR.TargetType, curCR.TargetID) {
			return
		}
	}

	var postApproveFinalize func(context.Context) error
	updated, err := s.fourEyes.Decide(r.Context(), foureyes.DecideParams{
		OrgID:               orgPtr,
		RequestID:           reqID,
		ApproverPrincipalID: sc.Actor.PrincipalID,
		Decision:            decision,
		Reason:              strings.TrimSpace(req.Reason),
		ExpectedVersion:     expectedVer,
		CorrelationID:       ptrOrNil(requestIDFromContext(r.Context())),
	}, func(applyCtx context.Context, cr *foureyes.ChangeRequest) error {
		finalizeFn, aerr := s.applyApprovedChangeRequest(applyCtx, sc, cr)
		if aerr != nil {
			return aerr
		}
		postApproveFinalize = finalizeFn
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, foureyes.ErrSelfApprovalForbidden):
			writeErrorEnvelope(w, r, http.StatusForbidden, "self_approval_forbidden", "permission_error", "initiator cannot approve own change request under four-eyes dual control", false, nil)
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "change request or target resource not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			latest, _ := s.fourEyes.Get(r.Context(), orgPtr, reqID)
			curVer := expectedVer
			if latest != nil {
				curVer = latest.Version
			}
			writeOptimisticConflict(w, r, curVer, latest)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		case errors.Is(err, foureyes.ErrInvalidStateTransition), errors.Is(err, foureyes.ErrRequestExpired):
			writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "change_request_apply_failed", "upstream_error", "failed applying approved change request", true, nil)
		}
		return
	}
	if postApproveFinalize != nil {
		if ferr := postApproveFinalize(r.Context()); ferr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "change_request_apply_failed", "upstream_error", "failed finalizing approved change request", true, nil)
			return
		}
	}

	w.Header().Set("ETag", FormatETag(updated.Version))
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) authorizeChangeRequestAction(w http.ResponseWriter, r *http.Request, sc *authSessionContext, orgPtr *string, action, targetType, targetID string) bool {
	orgID := ""
	if orgPtr != nil {
		orgID = strings.TrimSpace(*orgPtr)
	}
	switch action {
	case foureyes.ActionExtensionActivateExternal:
		if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
			return false
		}
		return s.authorize(w, r, sc, rbac.ActionExtensionApprove, rbac.TargetScope{OrgID: orgID})

	case foureyes.ActionKillSwitchClear:
		if s.killSwitch == nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_unavailable", "upstream_error", "kill-switch registry is not configured", true, nil)
			return false
		}
		var (
			ks  *killswitch.KillSwitch
			err error
		)
		if orgPtr != nil {
			ks, err = s.killSwitch.Get(r.Context(), orgID, targetID)
		} else {
			ks, err = s.killSwitch.GetInstance(r.Context(), targetID)
		}
		if errors.Is(err, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "kill switch not found", false, nil)
			return false
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating kill switch scope", true, nil)
			return false
		}
		_ = ks
		return s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{OrgID: orgID})

	case foureyes.ActionVaultRotateRootKey:
		if targetType == "project_encryption_key" {
			if orgPtr == nil {
				writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "org scope is required for project_encryption_key", false, nil)
				return false
			}
			if !s.verifyProjectBelongsToOrg(w, r, orgID, targetID) {
				return false
			}
			return s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: orgID, ProjectID: targetID})
		}
		if !sc.IsOperator || orgPtr != nil {
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "instance root key rotation requires OPERATOR role without org scoping", false, nil)
			return false
		}
		return s.authorize(w, r, sc, rbac.ActionOpsWrite, rbac.TargetScope{})

	case foureyes.ActionVaultDestroyRootKey:
		if targetType == "project_encryption_key" {
			if orgPtr == nil {
				writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "org scope is required for project_encryption_key", false, nil)
				return false
			}
			if !s.verifyProjectBelongsToOrg(w, r, orgID, targetID) {
				return false
			}
			return s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: orgID, ProjectID: targetID})
		}
		if orgPtr != nil && targetID != orgID {
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "cross-organization root key destruction forbidden", false, nil)
			return false
		}
		if orgPtr == nil && !sc.IsOperator {
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "cross-organization root key destruction requires OPERATOR role", false, nil)
			return false
		}
		return s.authorize(w, r, sc, rbac.ActionOrgDelete, rbac.TargetScope{OrgID: targetID})

	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "unsupported action_type", false, nil)
		return false
	}
}

func (s *Server) verifyProjectBelongsToOrg(w http.ResponseWriter, r *http.Request, orgID, projectID string) bool {
	var exists int
	err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM projects WHERE org_id = ? AND id = ?`), orgID, projectID).Scan(&exists)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found in organization", false, nil)
		return false
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating project ownership", true, nil)
		return false
	}
	return true
}

func (s *Server) applyApprovedChangeRequest(ctx context.Context, sc *authSessionContext, cr *foureyes.ChangeRequest) (func(context.Context) error, error) {
	reqID := requestIDFromContext(ctx)
	switch cr.ActionType {
	case foureyes.ActionExtensionActivateExternal:
		if s.installer == nil {
			return nil, fmt.Errorf("extension installer unavailable")
		}
		var payload struct {
			ExpectedVersion               int64 `json:"expected_version"`
			AcknowledgeUnverifiedExternal bool  `json:"acknowledge_unverified_external"`
			EnableImmediately             *bool `json:"enable_immediately"`
		}
		if err := json.Unmarshal([]byte(cr.PayloadJCSJSON), &payload); err != nil {
			return nil, fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
		}
		curDTO, curDBState, lerr := s.loadExtensionInstallationDTO(ctx, cr.TargetID)
		if lerr != nil {
			return nil, lerr
		}
		ver := payload.ExpectedVersion
		if ver < 1 {
			ver = curDTO.Version
		}
		candTrust, candEntSigned, provErr := s.installer.CandidateProvenance(ctx, cr.TargetID)
		if provErr != nil {
			return nil, provErr
		}
		if candTrust == lockfile.TrustTierUnverifiedExternal && !candEntSigned && !payload.AcknowledgeUnverifiedExternal {
			return nil, fmt.Errorf("%w: acknowledge_unverified_external=true required for unsigned external extension", port.ErrInvalidInput)
		}
		approverOrg := ""
		if cr.OrgID != nil {
			approverOrg = *cr.OrgID
		}
		if sc.IsOperator && !actorHasOrgScopedApproverBinding(sc.Actor, approverOrg) {
			approverOrg = ""
		}
		enableNow := true
		if payload.EnableImmediately != nil {
			enableNow = *payload.EnableImmediately
		}
		switch curDBState {
		case installer.StatePendingPermissionApproval:
			if _, err := s.installer.ApproveAndActivate(ctx, installer.ApproveInstallRequest{
				InstallationID:      cr.TargetID,
				ExpectedVersion:     ver,
				ApproverPrincipalID: sc.Actor.PrincipalID,
				ActivateImmediately: enableNow,
				OrgID:               approverOrg,
				RequestID:           reqID,
			}); err != nil {
				return nil, err
			}
		case installer.StateUpdatingPendingReconsent:
			if _, err := s.installer.ApproveEscalatedUpgrade(ctx, cr.TargetID, ver, sc.Actor.PrincipalID, approverOrg, reqID); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%w: installation in state %s cannot be approved", foureyes.ErrInvalidStateTransition, curDBState)
		}
		syncErr := s.syncExtensionProviderAdapters(ctx, cr.TargetID)
		if s.snapshots != nil {
			_ = s.snapshots.Refresh(ctx)
		}
		return nil, syncErr

	case foureyes.ActionKillSwitchClear:
		if s.killSwitch == nil {
			return nil, fmt.Errorf("kill-switch registry unavailable")
		}
		var payload struct {
			ExpectedVersion int64  `json:"expected_version"`
			ClearReason     string `json:"clear_reason"`
		}
		if err := json.Unmarshal([]byte(cr.PayloadJCSJSON), &payload); err != nil {
			return nil, fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
		}
		if strings.TrimSpace(payload.ClearReason) == "" {
			payload.ClearReason = "four-eyes approved kill-switch clear"
		}
		if payload.ExpectedVersion < 1 {
			payload.ExpectedVersion = 1
		}
		s.quarantineMu.Lock()
		defer s.quarantineMu.Unlock()
		var (
			ks   *killswitch.KillSwitch
			gerr error
		)
		if cr.OrgID != nil {
			ks, gerr = s.killSwitch.Get(ctx, *cr.OrgID, cr.TargetID)
		} else {
			ks, gerr = s.killSwitch.GetInstance(ctx, cr.TargetID)
		}
		if gerr != nil {
			return nil, gerr
		}
		if status, _, msg := s.verifyKillSwitchClearBarrier(ctx, ks); status != 0 {
			if status == http.StatusNotFound {
				return nil, port.ErrNotFound
			}
			return nil, errors.New(msg)
		}
		if _, err := s.killSwitch.Clear(ctx, killswitch.ClearParams{
			OrgID:                cr.OrgID,
			KillSwitchID:         cr.TargetID,
			ExpectedVersion:      payload.ExpectedVersion,
			ClearedByPrincipalID: sc.Actor.PrincipalID,
			ClearReason:          payload.ClearReason,
			EntryPoint:           port.AuditEntryPointAdminAPI,
			RequestID:            ptrOrNil(reqID),
		}); err != nil {
			return nil, err
		}
		if s.snapshots != nil {
			_ = s.snapshots.Refresh(ctx)
		}
		return nil, nil

	case foureyes.ActionVaultRotateRootKey:
		if s.vault == nil {
			return nil, fmt.Errorf("vault unavailable")
		}
		var payload struct {
			KMSKeyRef string `json:"kms_key_ref"`
			OldKEKID  string `json:"old_kek_id"`
			NewKEKID  string `json:"new_kek_id"`
		}
		if err := json.Unmarshal([]byte(cr.PayloadJCSJSON), &payload); err != nil {
			return nil, fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
		}
		if cr.OrgID != nil && cr.TargetType == "project_encryption_key" && strings.TrimSpace(payload.KMSKeyRef) != "" {
			if _, err := s.vault.BindProjectExternalRootKey(ctx, *cr.OrgID, cr.TargetID, payload.KMSKeyRef); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if !sc.IsOperator || cr.OrgID != nil {
			return nil, fmt.Errorf("%w: instance root key rotation forbidden for org scope", port.ErrInvalidInput)
		}
		if _, err := s.vault.RewrapAllOrgDEKs(ctx, payload.OldKEKID, payload.NewKEKID); err != nil {
			return nil, err
		}
		return nil, nil

	case foureyes.ActionVaultDestroyRootKey:
		if s.vault == nil {
			return nil, fmt.Errorf("vault unavailable")
		}
		var payload struct {
			ExpectedGeneration int64  `json:"expected_generation"`
			ShredRequestID     string `json:"shred_request_id"`
			ConfirmSlug        string `json:"confirm_slug"`
			Reason             string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(cr.PayloadJCSJSON), &payload); err != nil {
			return nil, fmt.Errorf("%w: %v", port.ErrInvalidInput, err)
		}
		if payload.ExpectedGeneration < 1 {
			payload.ExpectedGeneration = 1
		}
		if strings.TrimSpace(payload.ShredRequestID) == "" {
			payload.ShredRequestID = cr.ID
		}
		if cr.OrgID != nil && cr.TargetType == "project_encryption_key" {
			if strings.TrimSpace(payload.ConfirmSlug) != "" {
				prj, perr := s.storage.Tenancy().GetProject(ctx, *cr.OrgID, cr.TargetID)
				if perr != nil {
					return nil, perr
				}
				if strings.TrimSpace(payload.ConfirmSlug) != prj.Slug {
					return nil, fmt.Errorf("%w: confirm_slug mismatch", port.ErrInvalidInput)
				}
				reason := strings.TrimSpace(payload.Reason)
				if reason == "" {
					reason = "four-eyes approved project deletion"
				}
				_, _, st, code, msg, _ := s.executeProjectDeletion(ctx, sc, *cr.OrgID, prj, prj.Version, reqID, reason, payload.ShredRequestID)
				if st != http.StatusOK {
					switch st {
					case http.StatusNotFound:
						return nil, fmt.Errorf("%w: %s", port.ErrNotFound, msg)
					case http.StatusConflict:
						return nil, fmt.Errorf("%w: %s", port.ErrVersionConflict, msg)
					case http.StatusBadRequest:
						return nil, fmt.Errorf("%w: %s", port.ErrInvalidInput, msg)
					default:
						return nil, fmt.Errorf("%s: %s", code, msg)
					}
				}
				return nil, nil
			}
			return nil, s.vault.ShredProjectDEK(ctx, *cr.OrgID, cr.TargetID, vault.ShredRequest{
				RequestID:          payload.ShredRequestID,
				ExpectedGeneration: payload.ExpectedGeneration,
			})
		}
		if cr.OrgID != nil && cr.TargetID != *cr.OrgID {
			return nil, fmt.Errorf("%w: cross-organization root key destruction forbidden", port.ErrInvalidInput)
		}
		if cr.OrgID == nil && !sc.IsOperator {
			return nil, fmt.Errorf("%w: cross-organization root key destruction requires OPERATOR", port.ErrInvalidInput)
		}
		if strings.TrimSpace(payload.ConfirmSlug) != "" {
			org, oerr := s.storage.Tenancy().GetOrganization(ctx, cr.TargetID)
			if oerr != nil {
				return nil, oerr
			}
			if strings.TrimSpace(payload.ConfirmSlug) != org.Slug {
				return nil, fmt.Errorf("%w: confirm_slug mismatch", port.ErrInvalidInput)
			}
			reason := strings.TrimSpace(payload.Reason)
			if reason == "" {
				reason = "four-eyes approved organization deletion"
			}
			finalizeFn, _, immediate, _, st, code, msg, _ := s.prepareAndShredOrganizationDeletion(
				ctx, sc, org, org.Version, reqID, reason, payload.ShredRequestID,
			)
			if st != http.StatusOK {
				switch st {
				case http.StatusNotFound:
					return nil, fmt.Errorf("%w: %s", port.ErrNotFound, msg)
				case http.StatusConflict:
					return nil, fmt.Errorf("%w: %s", port.ErrVersionConflict, msg)
				case http.StatusBadRequest:
					return nil, fmt.Errorf("%w: %s", port.ErrInvalidInput, msg)
				default:
					return nil, fmt.Errorf("%s: %s", code, msg)
				}
			}
			if immediate || finalizeFn == nil {
				return nil, nil
			}
			return func(fctx context.Context) error {
				_, _, fst, fcode, fmsg := finalizeFn(fctx)
				if fst != http.StatusOK {
					return fmt.Errorf("%s: %s", fcode, fmsg)
				}
				return nil
			}, nil
		}
		return nil, s.vault.ShredOrgDEK(ctx, cr.TargetID, vault.ShredRequest{
			RequestID:          payload.ShredRequestID,
			ExpectedGeneration: payload.ExpectedGeneration,
		})

	default:
		return nil, fmt.Errorf("unsupported action_type %q", cr.ActionType)
	}
}

func (s *Server) resolveChangeRequestScope(w http.ResponseWriter, r *http.Request, sc *authSessionContext) (string, *string, bool) {
	qOrg := requestedTargetOrgID(r)
	if sc.IsOperator && qOrg == "" && sc.OrgID == "" {
		return "", nil, true
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return "", nil, false
	}
	return orgID, &orgID, true
}
