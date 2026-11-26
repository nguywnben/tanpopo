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
	"sort"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/foureyes"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type organizationDTO struct {
	ID              string `json:"id"`
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	MinPluginTrust  string `json:"min_plugin_trust"`
	FourEyesEnabled bool   `json:"four_eyes_enabled"`
	Version         int64  `json:"version"`
	CreatedAt       string `json:"created_at"`
}

type createOrganizationRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type projectDTO struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
}

type createProjectRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type environmentDTO struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type createEnvironmentRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

type destructiveConfirmRequest struct {
	ConfirmSlug string `json:"confirm_slug"`
	Reason      string `json:"reason"`
}

type cryptoShredReceiptDTO struct {
	TargetType              string `json:"target_type"`
	TargetID                string `json:"target_id"`
	DestroyedDEKID          string `json:"destroyed_dek_id"`
	ShreddedAt              string `json:"shredded_at"`
	RevokedVirtualKeysCount int    `json:"revoked_virtual_keys_count"`
}

func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	targetOrg := sc.OrgID
	if !sc.IsOperator {
		boundOrgs := collectReadableActorOrgIDs(sc.Actor, sc.OrgID)
		if len(boundOrgs) == 0 {
			_ = s.authorize(w, r, sc, rbac.ActionOrgRead, rbac.TargetScope{OrgID: targetOrg, IsCollectionList: true})
			return
		}
		if s.multiOrgDisabled && sc.OrgID != "" {
			boundOrgs = []string{sc.OrgID}
		}
		scopeCursorOrg := "multi:" + sc.Actor.PrincipalID
		if len(boundOrgs) == 1 {
			scopeCursorOrg = boundOrgs[0]
		}
		limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "organizations", scopeCursorOrg, sha256HexString("orgs"))
		if !ok {
			return
		}
		type orgRow struct {
			dto       organizationDTO
			createdMs int64
		}
		var allRows []orgRow
		for _, orgID := range boundOrgs {
			var q strings.Builder
			var args []any
			q.WriteString(`
				SELECT id, slug, display_name, status, min_plugin_trust, require_four_eyes_approval, version, created_at_ms
				FROM organizations
				WHERE id = ?`)
			args = append(args, orgID)
			if cur != nil {
				q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
				args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
			}
			err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
				rows, qerr := dbq.QueryContext(r.Context(), s.rebind(q.String()), args...)
				if qerr != nil {
					return qerr
				}
				defer rows.Close()
				for rows.Next() {
					var (
						id, slug, name, status, trust string
						fourEyes                      port.FlexBool
						ver, createdMs                int64
					)
					if scanErr := rows.Scan(&id, &slug, &name, &status, &trust, &fourEyes, &ver, &createdMs); scanErr != nil {
						return scanErr
					}
					allRows = append(allRows, orgRow{
						dto: organizationDTO{
							ID:              id,
							Slug:            slug,
							Name:            name,
							Status:          status,
							MinPluginTrust:  trust,
							FourEyesEnabled: bool(fourEyes),
							Version:         ver,
							CreatedAt:       formatTimeRFC3339Ms(createdMs),
						},
						createdMs: createdMs,
					})
				}
				return rows.Err()
			})
			if err != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing organizations", true, nil)
				return
			}
		}
		sort.Slice(allRows, func(i, j int) bool {
			if allRows[i].createdMs != allRows[j].createdMs {
				return allRows[i].createdMs > allRows[j].createdMs
			}
			return allRows[i].dto.ID > allRows[j].dto.ID
		})
		hasMore := len(allRows) > limit
		if hasMore {
			allRows = allRows[:limit]
		}
		data := make([]organizationDTO, len(allRows))
		for i := range allRows {
			data[i] = allRows[i].dto
		}
		var nextCursor *string
		if hasMore && len(allRows) > 0 {
			last := allRows[len(allRows)-1]
			tok, curErr := encodePaginationCursor(s.cursorSecret, "organizations", scopeCursorOrg, sha256HexString("orgs"), last.createdMs, last.dto.ID)
			if curErr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
				return
			}
			nextCursor = &tok
		}
		writeJSON(w, http.StatusOK, paginatedListEnvelope[organizationDTO]{
			Data:       data,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		})
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionOrgRead, rbac.TargetScope{IsCollectionList: true}) {
		return
	}

	scopeCursorOrg := "instance"
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "organizations", scopeCursorOrg, sha256HexString("orgs"))
	if !ok {
		return
	}

	var q strings.Builder
	var args []any
	q.WriteString(`
		SELECT id, slug, display_name, status, min_plugin_trust, require_four_eyes_approval, version, created_at_ms
		FROM organizations
		WHERE 1=1`)
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	type orgRow struct {
		dto       organizationDTO
		createdMs int64
	}
	var items []orgRow
	err := s.withOperatorReadTx(r.Context(), func(dbq adminDBQuerier) error {
		rows, err := dbq.QueryContext(r.Context(), s.rebind(q.String()), args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				id, slug, name, status, trust string
				fourEyes                      port.FlexBool
				ver, createdMs                int64
			)
			if scanErr := rows.Scan(&id, &slug, &name, &status, &trust, &fourEyes, &ver, &createdMs); scanErr != nil {
				return scanErr
			}
			items = append(items, orgRow{
				dto: organizationDTO{
					ID:              id,
					Slug:            slug,
					Name:            name,
					Status:          status,
					MinPluginTrust:  trust,
					FourEyesEnabled: bool(fourEyes),
					Version:         ver,
					CreatedAt:       formatTimeRFC3339Ms(createdMs),
				},
				createdMs: createdMs,
			})
		}
		return rows.Err()
	})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing organizations", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]organizationDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "organizations", scopeCursorOrg, sha256HexString("orgs"), last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[organizationDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionOrgCreate, rbac.TargetScope{}) {
		return
	}
	if s.multiOrgDisabled {
		writeErrorEnvelope(w, r, http.StatusForbidden, "multi_org_disabled", "permission_error", "organization creation is disabled when multi-organization mode is off", false, nil)
		return
	}

	var req createOrganizationRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Name) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	auditCtx := port.WithAuditActor(r.Context(), port.AuditActor{
		ActorKind:  "PRINCIPAL",
		ActorID:    sc.Actor.PrincipalID,
		EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID:  requestIDFromContext(r.Context()),
	})
	org, err := s.storage.Tenancy().CreateOrganization(auditCtx, port.CreateOrgParams{
		Slug:        strings.TrimSpace(req.Slug),
		DisplayName: strings.TrimSpace(req.Name),
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "organization slug already exists", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating organization", true, nil)
		}
		return
	}

	if s.vault != nil {
		if _, derr := s.vault.EnsureOrgDEK(r.Context(), org.ID); derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting organization DEK", true, nil)
			return
		}
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	dto := toOrganizationDTO(org)
	w.Header().Set("ETag", FormatETag(org.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleDeleteOrganization(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID := strings.TrimSpace(r.PathValue("orgId"))
	if orgID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionOrgDelete, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req destructiveConfirmRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}

	org, err := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading organization", true, nil)
		return
	}

	currentDTO := toOrganizationDTO(org)
	if org.Version != expectedVer {
		writeOptimisticConflict(w, r, org.Version, currentDTO)
		return
	}
	if strings.TrimSpace(req.ConfirmSlug) != org.Slug {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug must exactly match organization slug", false, nil)
		return
	}
	if isDeletingOrgStatus(org.Status) {
		receipt, snapPending, status, code, msg, foundReceipt := func() (cryptoShredReceiptDTO, bool, int, string, string, bool) {
			s.orgErasureMu.Lock()
			defer s.orgErasureMu.Unlock()
			latest, lerr := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
			if errors.Is(lerr, port.ErrNotFound) {
				if rec, found := s.lookupCompletedOrgShredReceipt(r.Context(), orgID); found {
					return rec, false, http.StatusOK, "", "", true
				}
				return cryptoShredReceiptDTO{}, false, http.StatusNotFound, "resource_not_found", "organization not found", false
			}
			if lerr != nil {
				return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed reading organization", false
			}
			rec, sp, st, cd, ms := s.resumeDeletingOrganization(r.Context(), latest, sc.Actor.PrincipalID, requestIDFromContext(r.Context()), req.Reason)
			return rec, sp, st, cd, ms, st == http.StatusOK
		}()
		if foundReceipt && status == http.StatusOK {
			if snapPending {
				w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
			}
			writeJSON(w, http.StatusOK, receipt)
			return
		}
		errType := "upstream_error"
		retryable := status >= 500
		if status == http.StatusNotFound {
			errType = "not_found_error"
		} else if status == http.StatusConflict {
			errType = "conflict_error"
		}
		writeErrorEnvelope(w, r, status, code, errType, msg, retryable, nil)
		return
	}
	if s.fourEyes != nil {
		needFE, feErr := s.fourEyes.RequiresFourEyes(r.Context(), &orgID)
		if feErr != nil && !errors.Is(feErr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking four-eyes policy", true, nil)
			return
		}
		if needFE {
			keyGen := int64(1)
			if s.vault != nil {
				meta, merr := s.vault.GetOrgKeyMetadata(r.Context(), orgID)
				if merr != nil && !errors.Is(merr, port.ErrNotFound) {
					writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading organization encryption key metadata", true, nil)
					return
				}
				if merr == nil && meta != nil && meta.KeyGeneration > 0 {
					keyGen = meta.KeyGeneration
				}
			}
			payloadBytes, _ := json.Marshal(map[string]any{
				"expected_generation": keyGen,
				"confirm_slug":        req.ConfirmSlug,
				"reason":              strings.TrimSpace(req.Reason),
			})
			cr, crErr := s.fourEyes.Initiate(r.Context(), foureyes.InitiateParams{
				OrgID:                &orgID,
				ActionType:           foureyes.ActionVaultDestroyRootKey,
				TargetType:           "org_encryption_key",
				TargetID:             orgID,
				PayloadJSON:          payloadBytes,
				InitiatorPrincipalID: sc.Actor.PrincipalID,
				RequestID:            ptrOrNil(requestIDFromContext(r.Context())),
			})
			if crErr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initiating four-eyes organization shred request", true, nil)
				return
			}
			w.Header().Set("ETag", FormatETag(cr.Version))
			writeJSON(w, http.StatusAccepted, cr)
			return
		}
	}

	receipt, snapPending, status, code, msg, conflictLatest := s.executeOrganizationDeletion(
		r.Context(), sc, org, expectedVer, requestIDFromContext(r.Context()), req.Reason, "",
	)
	if status != http.StatusOK {
		if status == http.StatusConflict && code == "optimistic_lock_conflict" && conflictLatest != nil {
			writeOptimisticConflict(w, r, conflictLatest.Version, toOrganizationDTO(conflictLatest))
			return
		}
		errType := "upstream_error"
		retryable := status >= 500
		if status == http.StatusNotFound {
			errType = "not_found_error"
		} else if status == http.StatusConflict {
			errType = "conflict_error"
		}
		writeErrorEnvelope(w, r, status, code, errType, msg, retryable, nil)
		return
	}
	if snapPending {
		w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *Server) executeOrganizationDeletion(
	ctx context.Context,
	sc *authSessionContext,
	org *port.Organization,
	expectedVer int64,
	requestID string,
	reason string,
	shredRequestID string,
) (cryptoShredReceiptDTO, bool, int, string, string, *port.Organization) {
	finalizeFn, receipt, immediate, snapPending, status, code, msg, conflictLatest := s.prepareAndShredOrganizationDeletion(
		ctx, sc, org, expectedVer, requestID, reason, shredRequestID,
	)
	if status != http.StatusOK {
		return receipt, snapPending, status, code, msg, conflictLatest
	}
	if immediate {
		return receipt, snapPending, http.StatusOK, "", "", nil
	}
	receipt, snapPending, st, fcode, fmsg := finalizeFn(ctx)
	return receipt, snapPending, st, fcode, fmsg, nil
}

type orgDeletionFinalizeFn func(ctx context.Context) (cryptoShredReceiptDTO, bool, int, string, string)

func (s *Server) prepareAndShredOrganizationDeletion(
	ctx context.Context,
	sc *authSessionContext,
	org *port.Organization,
	expectedVer int64,
	requestID string,
	reason string,
	shredRequestID string,
) (orgDeletionFinalizeFn, cryptoShredReceiptDTO, bool, bool, int, string, string, *port.Organization) {
	if org == nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusNotFound, "resource_not_found", "organization not found", nil
	}
	orgID := org.ID
	if isDeletingOrgStatus(org.Status) {
		s.orgErasureMu.Lock()
		defer s.orgErasureMu.Unlock()
		rec, sp, st, cd, ms := s.resumeDeletingOrganization(ctx, org, sc.Actor.PrincipalID, requestID, reason)
		return nil, rec, st == http.StatusOK, sp, st, cd, ms, nil
	}

	now := s.nowMs()
	var revokedKeysCount int
	var activeKeyIDs []string
	var orgPrincipals []string

	// Phase 1 TX: Atomically fence organization version BEFORE destroying any DEK,
	// collect keys/principals to invalidate, and tear down RESTRICT FKs leaving DEK metadata intact.
	tx1, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed starting teardown fence transaction", nil
	}
	defer func() { _ = tx1.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx1, s.dialect, orgID); err != nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed initializing teardown RLS context", nil
	}

	resFence, err := tx1.ExecContext(ctx, s.rebind(`
		UPDATE organizations SET status = 'DELETING', version = version + 1, updated_at_ms = ?
		WHERE id = ? AND version = ?`),
		now, orgID, expectedVer,
	)
	if err != nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed fencing organization version", nil
	}
	if affected, _ := resFence.RowsAffected(); affected == 0 {
		_ = tx1.Rollback()
		if latest, lerr := s.storage.Tenancy().GetOrganization(ctx, orgID); lerr == nil {
			return nil, cryptoShredReceiptDTO{}, false, false, http.StatusConflict, "optimistic_lock_conflict", "optimistic lock conflict", latest
		} else if errors.Is(lerr, port.ErrNotFound) {
			return nil, cryptoShredReceiptDTO{}, false, false, http.StatusNotFound, "resource_not_found", "organization not found", nil
		}
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusConflict, "optimistic_lock_conflict", "optimistic lock conflict", org
	}
	fencedVer := expectedVer + 1

	kRows, kerr := tx1.QueryContext(ctx, s.rebind(`
		SELECT id FROM virtual_keys
		WHERE org_id = ? AND state NOT IN ('REVOKED', 'EXPIRED', 'PURGED_METADATA_RETAINED')`),
		orgID,
	)
	if kerr != nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed enumerating organization virtual keys", nil
	}
	for kRows.Next() {
		var kid string
		if scanErr := kRows.Scan(&kid); scanErr != nil {
			_ = kRows.Close()
			return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed scanning virtual key id", nil
		}
		activeKeyIDs = append(activeKeyIDs, kid)
	}
	if rowsErr := kRows.Err(); rowsErr != nil {
		_ = kRows.Close()
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed iterating organization virtual keys", nil
	}
	_ = kRows.Close()
	revokedKeysCount = len(activeKeyIDs)

	pRows, perr := tx1.QueryContext(ctx, s.rebind(`SELECT id FROM principals WHERE org_id = ?`), orgID)
	if perr != nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed enumerating organization principals", nil
	}
	for pRows.Next() {
		var pid string
		if scanErr := pRows.Scan(&pid); scanErr != nil {
			_ = pRows.Close()
			return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed scanning principal id", nil
		}
		orgPrincipals = append(orgPrincipals, pid)
	}
	if rowsErr := pRows.Err(); rowsErr != nil {
		_ = pRows.Close()
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed iterating organization principals", nil
	}
	_ = pRows.Close()

	if err := cleanupOrgRestrictedFKsTx(ctx, tx1, s.dialect, orgID); err != nil {
		_ = tx1.Rollback()
		if errors.Is(err, port.ErrResourceInUse) {
			return nil, cryptoShredReceiptDTO{}, false, false, http.StatusConflict, "resource_in_use", "organization cannot be deleted while its principals are referenced by global kill switches or change requests", nil
		}
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed tearing down organization resources", nil
	}

	if err := tx1.Commit(); err != nil {
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed committing organization teardown fence", nil
	}

	s.orgErasureMu.Lock()
	if latestOrg, lerr := s.storage.Tenancy().GetOrganization(ctx, orgID); errors.Is(lerr, port.ErrNotFound) {
		s.orgErasureMu.Unlock()
		if receipt, found := s.lookupCompletedOrgShredReceipt(ctx, orgID); found {
			return nil, receipt, true, false, http.StatusOK, "", "", nil
		}
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusNotFound, "resource_not_found", "organization not found", nil
	} else if lerr != nil {
		s.orgErasureMu.Unlock()
		return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed reading organization", nil
	} else if latestOrg.Version != fencedVer {
		receipt, sp, status, code, msg := s.resumeDeletingOrganization(ctx, latestOrg, sc.Actor.PrincipalID, requestID, reason)
		s.orgErasureMu.Unlock()
		return nil, receipt, status == http.StatusOK, sp, status, code, msg, nil
	}

	// Phase 2: Crypto-shred organization DEK after winning the OCC version fence.
	destroyedDEK := "none"
	if s.vault != nil {
		meta, merr := s.vault.GetOrgKeyMetadata(ctx, orgID)
		if merr != nil && !errors.Is(merr, port.ErrNotFound) {
			s.orgErasureMu.Unlock()
			return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed reading organization encryption key metadata", nil
		}
		if merr == nil && meta != nil {
			reqID := strings.TrimSpace(shredRequestID)
			if reqID == "" {
				reqID = fmt.Sprintf("admin-org-shred-%s-%d", orgID, s.nowMs())
			}
			if serr := s.vault.ShredOrgDEK(ctx, orgID, vault.ShredRequest{
				RequestID:          reqID,
				ExpectedGeneration: meta.KeyGeneration,
			}); serr != nil {
				s.orgErasureMu.Unlock()
				return nil, cryptoShredReceiptDTO{}, false, false, http.StatusInternalServerError, "internal_error", "failed crypto-shredding organization DEK", nil
			}
			destroyedDEK = fmt.Sprintf("%s:v%d:g%d", orgID, meta.DEKVersion, meta.KeyGeneration)
		} else {
			s.vault.EvictAllDEKs()
		}
	}
	s.orgErasureMu.Unlock()

	orgSlug := org.Slug
	actorPrincipalID := sc.Actor.PrincipalID
	finalizeFn := func(fctx context.Context) (cryptoShredReceiptDTO, bool, int, string, string) {
		s.orgErasureMu.Lock()
		defer s.orgErasureMu.Unlock()

		// Phase 3 TX: Remove org-scoped change requests & kill switches,
		// delete fenced organization row, emit SHRED changelog, and record audit.
		tx2, err := s.db.BeginTx(fctx, nil)
		if err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed starting organization delete transaction"
		}
		defer func() { _ = tx2.Rollback() }()
		if err := port.SetOrgRLSTx(fctx, tx2, s.dialect, orgID); err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed initializing organization delete RLS context"
		}

		if hasACR, acrErr := tableExistsTx(fctx, tx2, s.dialect, "admin_change_requests"); acrErr != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed checking admin change requests table"
		} else if hasACR {
			if _, err := tx2.ExecContext(fctx, s.rebind(`DELETE FROM admin_change_requests WHERE org_id = ?`), orgID); err != nil {
				return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed clearing organization change requests"
			}
		}

		if _, err := tx2.ExecContext(fctx, s.rebind(`DELETE FROM emergency_kill_switches WHERE org_id = ?`), orgID); err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed clearing organization kill switches"
		}

		resDel, err := tx2.ExecContext(fctx, s.rebind(`DELETE FROM organizations WHERE id = ? AND version = ?`), orgID, fencedVer)
		if err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed deleting organization"
		}
		if affected, _ := resDel.RowsAffected(); affected == 0 {
			_ = tx2.Rollback()
			if receipt, found := s.lookupCompletedOrgShredReceipt(fctx, orgID); found {
				return receipt, false, http.StatusOK, "", ""
			}
			return cryptoShredReceiptDTO{}, false, http.StatusConflict, "optimistic_lock_conflict", "concurrent organization teardown conflict"
		}

		shredEpoch, err := s.appendChangelogTx(fctx, tx2, orgID, "organization", orgID, "SHRED", true, now)
		if err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed recording shred changelog"
		}

		audID, err := idgen.New(idgen.PrefixAudit)
		if err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed minting audit id"
		}
		metaJSON, _ := json.Marshal(map[string]any{
			"confirm_slug":     orgSlug,
			"reason":           strings.TrimSpace(reason),
			"destroyed_dek_id": destroyedDEK,
			"revoked_keys":     revokedKeysCount,
		})
		if err := s.txAudit.AppendTx(fctx, tx2, port.AuditEvent{
			ID:           audID,
			CreatedAtMs:  now,
			OrgID:        &orgID,
			ActorKind:    "PRINCIPAL",
			ActorID:      actorPrincipalID,
			EntryPoint:   port.AuditEntryPointAdminAPI,
			EventType:    "organization.crypto_shredded",
			TargetType:   "organization",
			TargetID:     orgID,
			RequestID:    ptrOrNil(requestID),
			MetadataJSON: string(metaJSON),
		}); err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed recording shred audit event"
		}

		if err := tx2.Commit(); err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed committing organization crypto-shred"
		}

		if s.killSwitch != nil {
			_ = s.killSwitch.RefreshFromDB(fctx)
		}

		snapPending := false
		if s.snapshots != nil {
			if s.snapshots.Streams() != nil {
				s.snapshots.Streams().AbortScope(orgID, "")
			}
			if refErr := s.snapshots.Refresh(fctx); refErr != nil {
				snapPending = true
				for _, kid := range activeKeyIDs {
					s.snapshots.FenceKeyRevocation(kid, "EMERGENCY_SECURITY", now, shredEpoch)
				}
				s.emitSecurityAudit(fctx, sc, "organization.snapshot_sync_pending", map[string]any{
					"org_id":      orgID,
					"shred_epoch": shredEpoch,
					"fenced_keys": len(activeKeyIDs),
					"error":       refErr.Error(),
				})
			}
		}
		if s.killSwitch != nil && s.killSwitch.Streams() != nil {
			s.killSwitch.Streams().AbortMatching(killswitch.LevelProjectOrOrgFreeze, orgID, orgID)
		}
		for _, pid := range orgPrincipals {
			s.auth.RevokePrincipalSessions(pid)
		}

		return cryptoShredReceiptDTO{
			TargetType:              "ORGANIZATION",
			TargetID:                orgID,
			DestroyedDEKID:          destroyedDEK,
			ShreddedAt:              formatTimeRFC3339Ms(now),
			RevokedVirtualKeysCount: revokedKeysCount,
		}, snapPending, http.StatusOK, "", ""
	}

	preview := cryptoShredReceiptDTO{
		TargetType:              "ORGANIZATION",
		TargetID:                orgID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(now),
		RevokedVirtualKeysCount: revokedKeysCount,
	}
	return finalizeFn, preview, false, false, http.StatusOK, "", "", nil
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionProjectRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	var allowedProjects []string
	if !actorHasOrgScopeBinding(sc.Actor, orgID) {
		allowedProjects = actorProjectIDs(sc.Actor, orgID)
		if len(allowedProjects) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[projectDTO]{
				Data:    []projectDTO{},
				HasMore: false,
			})
			return
		}
	}

	filterHash := sha256HexString(strings.Join(allowedProjects, ","))
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "projects", orgID, filterHash)
	if !ok {
		return
	}

	var q strings.Builder
	args := []any{orgID}
	q.WriteString(`
		SELECT id, org_id, slug, display_name, version, created_at_ms
		FROM projects
		WHERE org_id = ?`)
	if len(allowedProjects) > 0 {
		placeholders := make([]string, len(allowedProjects))
		for i, pid := range allowedProjects {
			placeholders[i] = "?"
			args = append(args, pid)
		}
		q.WriteString(` AND id IN (` + strings.Join(placeholders, ",") + `)`)
	}
	if cur != nil {
		q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}
	q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
	args = append(args, limit+1)

	type projRow struct {
		dto       projectDTO
		createdMs int64
	}
	var items []projRow
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		rows, err := dbq.QueryContext(r.Context(), s.rebind(q.String()), args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var id, pOrg, slug, name string
			var ver, createdMs int64
			if scanErr := rows.Scan(&id, &pOrg, &slug, &name, &ver, &createdMs); scanErr != nil {
				return scanErr
			}
			items = append(items, projRow{
				dto: projectDTO{
					ID:        id,
					OrgID:     pOrg,
					Slug:      slug,
					Name:      name,
					Version:   ver,
					CreatedAt: formatTimeRFC3339Ms(createdMs),
				},
				createdMs: createdMs,
			})
		}
		return rows.Err()
	})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing projects", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]projectDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "projects", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[projectDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req createProjectRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	if !s.requireActiveOrg(w, r, orgID) {
		return
	}
	if strings.TrimSpace(req.Slug) == "" || strings.TrimSpace(req.Name) == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	auditCtx := port.WithAuditActor(r.Context(), port.AuditActor{
		ActorKind:  "PRINCIPAL",
		ActorID:    sc.Actor.PrincipalID,
		EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID:  requestIDFromContext(r.Context()),
	})
	prj, err := s.storage.Tenancy().CreateProject(auditCtx, orgID, port.CreateProjectParams{
		Slug:        strings.TrimSpace(req.Slug),
		DisplayName: strings.TrimSpace(req.Name),
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "project slug already exists in organization", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating project", true, nil)
		}
		return
	}

	if s.vault != nil {
		if _, derr := s.vault.EnsureProjectDEK(r.Context(), orgID, prj.ID); derr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting project DEK", true, nil)
			return
		}
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	dto := toProjectDTO(prj)
	w.Header().Set("ETag", FormatETag(prj.Version))
	writeJSON(w, http.StatusCreated, dto)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	if projectID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionProjectWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req destructiveConfirmRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}

	prj, err := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionProjectWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "reason must be at least 5 characters", false, nil)
		return
	}

	currentDTO := toProjectDTO(prj)
	if prj.Version != expectedVer {
		writeOptimisticConflict(w, r, prj.Version, currentDTO)
		return
	}
	if strings.TrimSpace(req.ConfirmSlug) != prj.Slug {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug must exactly match project slug", false, nil)
		return
	}
	if s.fourEyes != nil {
		needFE, feErr := s.fourEyes.RequiresFourEyes(r.Context(), &orgID)
		if feErr != nil && !errors.Is(feErr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking four-eyes policy", true, nil)
			return
		}
		if needFE {
			keyGen := int64(1)
			if s.vault != nil {
				meta, merr := s.vault.GetProjectKeyMetadata(r.Context(), orgID, projectID)
				if merr != nil && !errors.Is(merr, port.ErrNotFound) {
					writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project encryption key metadata", true, nil)
					return
				}
				if merr == nil && meta != nil && meta.KeyGeneration > 0 {
					keyGen = meta.KeyGeneration
				}
			}
			payloadBytes, _ := json.Marshal(map[string]any{
				"expected_generation": keyGen,
				"confirm_slug":        req.ConfirmSlug,
				"reason":              strings.TrimSpace(req.Reason),
			})
			cr, crErr := s.fourEyes.Initiate(r.Context(), foureyes.InitiateParams{
				OrgID:                &orgID,
				ActionType:           foureyes.ActionVaultDestroyRootKey,
				TargetType:           "project_encryption_key",
				TargetID:             projectID,
				PayloadJSON:          payloadBytes,
				InitiatorPrincipalID: sc.Actor.PrincipalID,
				RequestID:            ptrOrNil(requestIDFromContext(r.Context())),
			})
			if crErr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed initiating four-eyes project shred request", true, nil)
				return
			}
			w.Header().Set("ETag", FormatETag(cr.Version))
			writeJSON(w, http.StatusAccepted, cr)
			return
		}
	}

	receipt, snapPending, status, code, msg, conflictLatest := s.executeProjectDeletion(
		r.Context(), sc, orgID, prj, expectedVer, requestIDFromContext(r.Context()), req.Reason, "",
	)
	if status != http.StatusOK {
		if status == http.StatusConflict && code == "optimistic_lock_conflict" && conflictLatest != nil {
			writeOptimisticConflict(w, r, conflictLatest.Version, toProjectDTO(conflictLatest))
			return
		}
		errType := "upstream_error"
		retryable := status >= 500
		if status == http.StatusNotFound {
			errType = "not_found_error"
		} else if status == http.StatusConflict {
			errType = "conflict_error"
		}
		writeErrorEnvelope(w, r, status, code, errType, msg, retryable, nil)
		return
	}
	if snapPending {
		w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
	}
	writeJSON(w, http.StatusOK, receipt)
}

func (s *Server) executeProjectDeletion(
	ctx context.Context,
	sc *authSessionContext,
	orgID string,
	prj *port.Project,
	expectedVer int64,
	requestID string,
	reason string,
	shredRequestID string,
) (cryptoShredReceiptDTO, bool, int, string, string, *port.Project) {
	if prj == nil {
		return cryptoShredReceiptDTO{}, false, http.StatusNotFound, "resource_not_found", "project not found", nil
	}
	projectID := prj.ID
	now := s.nowMs()
	var revokedKeysCount int
	var activeKeyIDs []string
	var projectKSTargets []string

	// Phase 1 TX: Atomically fence project version BEFORE destroying any DEK,
	// collect virtual keys and kill-switch target IDs, and tear down RESTRICT FKs leaving DEK metadata intact.
	tx1, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed starting project teardown fence transaction", nil
	}
	defer func() { _ = tx1.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx1, s.dialect, orgID); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed initializing project teardown RLS context", nil
	}

	resFence, err := tx1.ExecContext(ctx, s.rebind(`
		UPDATE projects SET status = 'DELETING', version = version + 1, updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`),
		now, orgID, projectID, expectedVer,
	)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed fencing project version", nil
	}
	if affected, _ := resFence.RowsAffected(); affected == 0 {
		_ = tx1.Rollback()
		if latest, lerr := s.storage.Tenancy().GetProject(ctx, orgID, projectID); lerr == nil {
			return cryptoShredReceiptDTO{}, false, http.StatusConflict, "optimistic_lock_conflict", "optimistic lock conflict", latest
		} else if errors.Is(lerr, port.ErrNotFound) {
			return cryptoShredReceiptDTO{}, false, http.StatusNotFound, "resource_not_found", "project not found", nil
		}
		return cryptoShredReceiptDTO{}, false, http.StatusConflict, "optimistic_lock_conflict", "optimistic lock conflict", prj
	}
	fencedVer := expectedVer + 1

	projectKSTargets = append(projectKSTargets, projectID)
	kRows, kerr := tx1.QueryContext(ctx, s.rebind(`
		SELECT id, state FROM virtual_keys
		WHERE org_id = ? AND project_id = ?`),
		orgID, projectID,
	)
	if kerr != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed enumerating project virtual keys", nil
	}
	for kRows.Next() {
		var kid, kstate string
		if scanErr := kRows.Scan(&kid, &kstate); scanErr != nil {
			_ = kRows.Close()
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed scanning virtual key id", nil
		}
		projectKSTargets = append(projectKSTargets, kid)
		if kstate != "REVOKED" && kstate != "EXPIRED" && kstate != "PURGED_METADATA_RETAINED" {
			activeKeyIDs = append(activeKeyIDs, kid)
		}
	}
	if rowsErr := kRows.Err(); rowsErr != nil {
		_ = kRows.Close()
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed iterating project virtual keys", nil
	}
	_ = kRows.Close()
	revokedKeysCount = len(activeKeyIDs)

	cRows, cerr := tx1.QueryContext(ctx, s.rebind(`
		SELECT id FROM provider_connections
		WHERE org_id = ? AND owner_project_id = ?`),
		orgID, projectID,
	)
	if cerr != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed enumerating project connections", nil
	}
	for cRows.Next() {
		var cid string
		if scanErr := cRows.Scan(&cid); scanErr != nil {
			_ = cRows.Close()
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed scanning connection id", nil
		}
		projectKSTargets = append(projectKSTargets, cid)
	}
	if rowsErr := cRows.Err(); rowsErr != nil {
		_ = cRows.Close()
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed iterating project connections", nil
	}
	_ = cRows.Close()

	if err := cleanupProjectRestrictedFKsTx(ctx, tx1, s.dialect, orgID, projectID); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed tearing down project dependencies", nil
	}

	if err := tx1.Commit(); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed committing project teardown fence", nil
	}

	// Phase 2: Crypto-shred project DEK after winning the OCC version fence.
	destroyedDEK := "none"
	if s.vault != nil {
		meta, merr := s.vault.GetProjectKeyMetadata(ctx, orgID, projectID)
		if merr != nil && !errors.Is(merr, port.ErrNotFound) {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed reading project encryption key metadata", nil
		}
		if merr == nil && meta != nil {
			reqID := strings.TrimSpace(shredRequestID)
			if reqID == "" {
				reqID = fmt.Sprintf("admin-proj-shred-%s-%d", projectID, s.nowMs())
			}
			if serr := s.vault.ShredProjectDEK(ctx, orgID, projectID, vault.ShredRequest{
				RequestID:          reqID,
				ExpectedGeneration: meta.KeyGeneration,
			}); serr != nil {
				return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed crypto-shredding project DEK", nil
			}
			destroyedDEK = fmt.Sprintf("%s:%s:v%d:g%d", orgID, projectID, meta.DEKVersion, meta.KeyGeneration)
		} else {
			s.vault.EvictAllDEKs()
		}
	}

	// Phase 3 TX: Remove project-scoped kill switches (kept active until crypto-shred succeeds),
	// delete fenced project row, emit SHRED changelog, and record audit.
	tx2, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed starting project delete transaction", nil
	}
	defer func() { _ = tx2.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx2, s.dialect, orgID); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed initializing project delete RLS context", nil
	}

	if len(projectKSTargets) > 0 {
		placeholders := make([]string, len(projectKSTargets))
		args := make([]any, 0, len(projectKSTargets)+1)
		args = append(args, orgID)
		for i, tid := range projectKSTargets {
			placeholders[i] = "?"
			args = append(args, tid)
		}
		if _, err := tx2.ExecContext(ctx, s.rebind(`
			DELETE FROM emergency_kill_switches
			WHERE org_id = ? AND target_id IN (`+strings.Join(placeholders, ",")+`)`),
			args...,
		); err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed clearing project kill switches", nil
		}
	}

	resDel, err := tx2.ExecContext(ctx, s.rebind(`DELETE FROM projects WHERE org_id = ? AND id = ? AND version = ?`), orgID, projectID, fencedVer)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed deleting project", nil
	}
	if affected, _ := resDel.RowsAffected(); affected == 0 {
		return cryptoShredReceiptDTO{}, false, http.StatusConflict, "optimistic_lock_conflict", "concurrent project teardown conflict", nil
	}

	shredEpoch, err := s.appendChangelogTx(ctx, tx2, orgID, "project", projectID, "SHRED", true, now)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed recording shred changelog", nil
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed minting audit id", nil
	}
	metaJSON, _ := json.Marshal(map[string]any{
		"confirm_slug":     prj.Slug,
		"reason":           strings.TrimSpace(reason),
		"destroyed_dek_id": destroyedDEK,
		"revoked_keys":     revokedKeysCount,
	})
	if err := s.txAudit.AppendTx(ctx, tx2, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "project.crypto_shredded",
		TargetType:   "project",
		TargetID:     projectID,
		RequestID:    ptrOrNil(requestID),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed recording shred audit event", nil
	}

	if err := tx2.Commit(); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed committing project crypto-shred", nil
	}

	if s.killSwitch != nil {
		_ = s.killSwitch.RefreshFromDB(ctx)
	}

	snapPending := false
	if s.snapshots != nil {
		if s.snapshots.Streams() != nil {
			s.snapshots.Streams().AbortScope(orgID, projectID)
		}
		if refErr := s.snapshots.Refresh(ctx); refErr != nil {
			snapPending = true
			for _, kid := range activeKeyIDs {
				s.snapshots.FenceKeyRevocation(kid, "EMERGENCY_SECURITY", now, shredEpoch)
			}
			s.emitSecurityAudit(ctx, sc, "project.snapshot_sync_pending", map[string]any{
				"project_id":  projectID,
				"shred_epoch": shredEpoch,
				"fenced_keys": len(activeKeyIDs),
				"error":       refErr.Error(),
			})
		}
	}
	if s.killSwitch != nil && s.killSwitch.Streams() != nil {
		s.killSwitch.Streams().AbortMatching(killswitch.LevelProjectOrOrgFreeze, orgID, projectID)
	}

	return cryptoShredReceiptDTO{
		TargetType:              "PROJECT",
		TargetID:                projectID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(now),
		RevokedVirtualKeysCount: revokedKeysCount,
	}, snapPending, http.StatusOK, "", "", nil
}

func toOrganizationDTO(org *port.Organization) organizationDTO {
	status := strings.TrimSpace(org.Status)
	if status == "" {
		status = "ACTIVE"
	}
	return organizationDTO{
		ID:              org.ID,
		Slug:            org.Slug,
		Name:            org.DisplayName,
		Status:          status,
		MinPluginTrust:  org.MinPluginTrust,
		FourEyesEnabled: org.RequireFourEyesApproval,
		Version:         org.Version,
		CreatedAt:       formatTimeRFC3339Ms(org.CreatedAtMs),
	}
}

func toProjectDTO(prj *port.Project) projectDTO {
	return projectDTO{
		ID:        prj.ID,
		OrgID:     prj.OrgID,
		Slug:      prj.Slug,
		Name:      prj.DisplayName,
		Version:   prj.Version,
		CreatedAt: formatTimeRFC3339Ms(prj.CreatedAtMs),
	}
}

func tableExistsTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect, name string) (bool, error) {
	if dialect == port.DialectPostgres {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
			return false, err
		}
		return exists, nil
	}
	var cnt int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&cnt)
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func cleanupOrgRestrictedFKsTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect, orgID string) error {
	if dialect == port.DialectPostgres {
		if err := port.SetOrgRLSTx(ctx, tx, dialect, orgID); err != nil {
			return err
		}
		pRows, err := tx.QueryContext(ctx, port.Rebind(dialect, `SELECT id FROM principals WHERE org_id = ?`), orgID)
		if err != nil {
			return err
		}
		var pids []string
		for pRows.Next() {
			var pid string
			if serr := pRows.Scan(&pid); serr != nil {
				_ = pRows.Close()
				return serr
			}
			pids = append(pids, pid)
		}
		rerr := pRows.Err()
		_ = pRows.Close()
		if rerr != nil {
			return rerr
		}

		if len(pids) > 0 {
			orgIDs, err := port.ListOrgIDsTx(ctx, tx, dialect)
			if err != nil {
				return err
			}
			hasACR, err := tableExistsTx(ctx, tx, dialect, "admin_change_requests")
			if err != nil {
				return err
			}
			ph := make([]string, len(pids))
			for i := range pids {
				ph[i] = "?"
			}
			inClause := strings.Join(ph, ",")
			checkKS := port.Rebind(dialect, `SELECT COUNT(*) FROM emergency_kill_switches WHERE (triggered_by_principal_id IN (`+inClause+`) OR cleared_by_principal_id IN (`+inClause+`))`)
			checkACR := port.Rebind(dialect, `SELECT COUNT(*) FROM admin_change_requests WHERE (initiator_principal_id IN (`+inClause+`) OR approver_principal_id IN (`+inClause+`))`)
			checkArgs := make([]any, 0, len(pids)*2)
			for _, pid := range pids {
				checkArgs = append(checkArgs, pid)
			}
			for _, pid := range pids {
				checkArgs = append(checkArgs, pid)
			}

			// Check global (org_id IS NULL) references via operator scope.
			if err := port.SetOperatorRLSTx(ctx, tx, dialect); err != nil {
				return err
			}
			var cnt int
			if err := tx.QueryRowContext(ctx, port.Rebind(dialect, `SELECT COUNT(*) FROM emergency_kill_switches WHERE org_id IS NULL AND (triggered_by_principal_id IN (`+inClause+`) OR cleared_by_principal_id IN (`+inClause+`))`), checkArgs...).Scan(&cnt); err != nil {
				return err
			}
			if cnt > 0 {
				return fmt.Errorf("%w: principals of organization %s are referenced by global or external kill switches", port.ErrResourceInUse, orgID)
			}
			if hasACR {
				if err := tx.QueryRowContext(ctx, port.Rebind(dialect, `SELECT COUNT(*) FROM admin_change_requests WHERE org_id IS NULL AND (initiator_principal_id IN (`+inClause+`) OR approver_principal_id IN (`+inClause+`))`), checkArgs...).Scan(&cnt); err != nil {
					return err
				}
				if cnt > 0 {
					return fmt.Errorf("%w: principals of organization %s are referenced by global or external change requests", port.ErrResourceInUse, orgID)
				}
			}

			// Check external org references across all other orgs.
			for _, oid := range orgIDs {
				if oid == orgID {
					continue
				}
				if err := port.SetOrgRLSTx(ctx, tx, dialect, oid); err != nil {
					return err
				}
				if err := tx.QueryRowContext(ctx, checkKS, checkArgs...).Scan(&cnt); err != nil {
					return err
				}
				if cnt > 0 {
					return fmt.Errorf("%w: principals of organization %s are referenced by global or external kill switches", port.ErrResourceInUse, orgID)
				}
				if hasACR {
					if err := tx.QueryRowContext(ctx, checkACR, checkArgs...).Scan(&cnt); err != nil {
						return err
					}
					if cnt > 0 {
						return fmt.Errorf("%w: principals of organization %s are referenced by global or external change requests", port.ErrResourceInUse, orgID)
					}
				}
			}
		}
		if err := port.SetOrgRLSTx(ctx, tx, dialect, orgID); err != nil {
			return err
		}
	} else {
		var externalKSCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM emergency_kill_switches
			WHERE (org_id IS NULL OR org_id != ?)
			  AND (
			    triggered_by_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
			    OR cleared_by_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
			  )`,
			orgID, orgID, orgID,
		).Scan(&externalKSCount); err != nil {
			return err
		}
		if externalKSCount > 0 {
			return fmt.Errorf("%w: principals of organization %s are referenced by global or external kill switches", port.ErrResourceInUse, orgID)
		}

		if hasACR, err := tableExistsTx(ctx, tx, dialect, "admin_change_requests"); err != nil {
			return err
		} else if hasACR {
			var externalACRCount int
			if err := tx.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM admin_change_requests
				WHERE (org_id IS NULL OR org_id != ?)
				  AND (
				    initiator_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
				    OR approver_principal_id IN (SELECT id FROM principals WHERE org_id = ?)
				  )`,
				orgID, orgID, orgID,
			).Scan(&externalACRCount); err != nil {
				return err
			}
			if externalACRCount > 0 {
				return fmt.Errorf("%w: principals of organization %s are referenced by global or external change requests", port.ErrResourceInUse, orgID)
			}
		}
	}

	optionalSteps := []struct {
		table string
		query string
		args  []any
	}{
		{
			table: "a2a_tasks",
			query: `UPDATE a2a_tasks SET parent_task_id = NULL WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "a2a_tasks",
			query: `DELETE FROM a2a_tasks WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "approval_requests",
			query: `DELETE FROM approval_requests WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "webhook_subscriptions",
			query: `DELETE FROM webhook_subscriptions WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "agent_endpoints",
			query: `DELETE FROM agent_endpoints WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			table: "mcp_servers",
			query: `DELETE FROM mcp_servers WHERE org_id = ?`,
			args:  []any{orgID},
		},
	}
	for _, st := range optionalSteps {
		exists, err := tableExistsTx(ctx, tx, dialect, st.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, port.Rebind(dialect, st.query), st.args...); err != nil {
			return err
		}
	}

	steps := []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM route_targets WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM route_policies WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM model_deployments WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM connection_secret_bindings WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM provider_connections WHERE org_id = ?`,
			args:  []any{orgID},
		},
		{
			query: `DELETE FROM secret_credentials WHERE org_id = ?`,
			args:  []any{orgID},
		},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, port.Rebind(dialect, st.query), st.args...); err != nil {
			return err
		}
	}
	return nil
}

func cleanupProjectRestrictedFKsTx(ctx context.Context, tx *sql.Tx, dialect port.Dialect, orgID, projectID string) error {
	optionalSteps := []struct {
		table string
		query string
		args  []any
	}{
		{
			table: "a2a_tasks",
			query: `UPDATE a2a_tasks SET parent_task_id = NULL WHERE org_id = ? AND (project_id = ? OR agent_endpoint_id IN (SELECT id FROM agent_endpoints WHERE org_id = ? AND project_id = ?) OR caller_virtual_key_id IN (SELECT id FROM virtual_keys WHERE org_id = ? AND project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID, orgID, projectID},
		},
		{
			table: "a2a_tasks",
			query: `DELETE FROM a2a_tasks WHERE org_id = ? AND (project_id = ? OR agent_endpoint_id IN (SELECT id FROM agent_endpoints WHERE org_id = ? AND project_id = ?) OR caller_virtual_key_id IN (SELECT id FROM virtual_keys WHERE org_id = ? AND project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID, orgID, projectID},
		},
		{
			table: "approval_requests",
			query: `DELETE FROM approval_requests WHERE org_id = ? AND (project_id = ? OR requester_virtual_key_id IN (SELECT id FROM virtual_keys WHERE org_id = ? AND project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
		{
			table: "webhook_subscriptions",
			query: `DELETE FROM webhook_subscriptions WHERE org_id = ? AND (project_id = ? OR signing_secret_credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
		{
			table: "agent_endpoints",
			query: `DELETE FROM agent_endpoints WHERE org_id = ? AND (project_id = ? OR credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
		{
			table: "mcp_servers",
			query: `DELETE FROM mcp_servers WHERE org_id = ? AND (project_id = ? OR credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, projectID, orgID, projectID},
		},
	}
	for _, st := range optionalSteps {
		exists, err := tableExistsTx(ctx, tx, dialect, st.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, port.Rebind(dialect, st.query), st.args...); err != nil {
			return err
		}
	}

	steps := []struct {
		query string
		args  []any
	}{
		{
			query: `DELETE FROM route_targets WHERE org_id = ? AND (route_id IN (SELECT id FROM route_policies WHERE org_id = ? AND project_id = ?) OR deployment_id IN (SELECT id FROM model_deployments WHERE org_id = ? AND connection_id IN (SELECT id FROM provider_connections WHERE org_id = ? AND owner_project_id = ?)))`,
			args:  []any{orgID, orgID, projectID, orgID, orgID, projectID},
		},
		{
			query: `DELETE FROM route_policies WHERE org_id = ? AND project_id = ?`,
			args:  []any{orgID, projectID},
		},
		{
			query: `DELETE FROM model_deployments WHERE org_id = ? AND connection_id IN (SELECT id FROM provider_connections WHERE org_id = ? AND owner_project_id = ?)`,
			args:  []any{orgID, orgID, projectID},
		},
		{
			query: `DELETE FROM connection_secret_bindings WHERE org_id = ? AND (connection_id IN (SELECT id FROM provider_connections WHERE org_id = ? AND owner_project_id = ?) OR credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?))`,
			args:  []any{orgID, orgID, projectID, orgID, projectID},
		},
		{
			query: `UPDATE provider_connections SET credential_id = NULL WHERE org_id = ? AND owner_project_id IS NULL AND credential_id IN (SELECT id FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?)`,
			args:  []any{orgID, orgID, projectID},
		},
		{
			query: `DELETE FROM provider_connections WHERE org_id = ? AND owner_project_id = ?`,
			args:  []any{orgID, projectID},
		},
		{
			query: `DELETE FROM secret_credentials WHERE org_id = ? AND owner_project_id = ?`,
			args:  []any{orgID, projectID},
		},
	}
	for _, st := range steps {
		if _, err := tx.ExecContext(ctx, port.Rebind(dialect, st.query), st.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleListProjectEnvironments(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	if projectID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionProjectRead); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}
	prj, err := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionProjectRead, rbac.TargetScope{OrgID: orgID, ProjectID: prj.ID}) {
		return
	}

	envs, err := s.storage.Tenancy().ListEnvironments(r.Context(), orgID, prj.ID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing environments", true, nil)
		return
	}

	out := make([]environmentDTO, len(envs))
	for i, e := range envs {
		envName := strings.TrimSpace(e.DisplayName)
		if envName == "" {
			envName = e.Slug
		}
		out[i] = environmentDTO{
			ID:        e.ID,
			OrgID:     e.OrgID,
			ProjectID: e.ProjectID,
			Slug:      e.Slug,
			Name:      envName,
			CreatedAt: formatTimeRFC3339Ms(e.CreatedAtMs),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (s *Server) handleCreateProjectEnvironment(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.PathValue("projectId"))
	if projectID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	var req createEnvironmentRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionEnvironmentWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	prj, err := s.storage.Tenancy().GetProject(r.Context(), orgID, projectID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading project", true, nil)
		return
	}

	if !s.authorize(w, r, sc, rbac.ActionEnvironmentWrite, rbac.TargetScope{OrgID: orgID, ProjectID: prj.ID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, prj.ID) {
		return
	}

	slug := strings.TrimSpace(req.Slug)
	name := strings.TrimSpace(req.Name)
	if slug == "" || name == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "slug and name are required", false, nil)
		return
	}

	auditCtx := port.WithAuditActor(r.Context(), port.AuditActor{
		ActorKind:  "PRINCIPAL",
		ActorID:    sc.Actor.PrincipalID,
		EntryPoint: port.AuditEntryPointAdminAPI,
		RequestID:  requestIDFromContext(r.Context()),
	})
	env, err := s.storage.Tenancy().CreateEnvironment(auditCtx, orgID, prj.ID, port.CreateEnvironmentParams{
		Slug:        slug,
		DisplayName: name,
	})
	if err != nil {
		switch {
		case errors.Is(err, port.ErrAlreadyExists):
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "environment slug already exists in project", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating environment", true, nil)
		}
		return
	}

	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}

	envName := strings.TrimSpace(env.DisplayName)
	if envName == "" {
		envName = name
	}
	dto := environmentDTO{
		ID:        env.ID,
		OrgID:     env.OrgID,
		ProjectID: env.ProjectID,
		Slug:      env.Slug,
		Name:      envName,
		CreatedAt: formatTimeRFC3339Ms(env.CreatedAtMs),
	}
	w.Header().Set("ETag", FormatETag(env.Version))
	writeJSON(w, http.StatusCreated, dto)
}
