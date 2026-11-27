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
	"strings"

	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func isDeletingOrgStatus(status string) bool {
	s := strings.ToUpper(strings.TrimSpace(status))
	return s == "DELETING" || s == "PENDING_ERASURE"
}

func (s *Server) handleFinalizeOrganizationErasure(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !sc.IsOperator {
		writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "finalizing organization erasure requires OPERATOR role", false, nil)
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

	s.orgErasureMu.Lock()
	defer s.orgErasureMu.Unlock()

	org, err := s.storage.Tenancy().GetOrganization(r.Context(), orgID)
	if errors.Is(err, port.ErrNotFound) {
		if receipt, found := s.lookupCompletedOrgShredReceipt(r.Context(), orgID); found {
			writeJSON(w, http.StatusOK, receipt)
			return
		}
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reading organization", true, nil)
		return
	}
	if !isDeletingOrgStatus(org.Status) {
		writeErrorEnvelope(w, r, http.StatusConflict, "invalid_state_transition", "conflict_error", fmt.Sprintf("organization %s is in status %s (must be DELETING or PENDING_ERASURE)", org.ID, org.Status), false, toOrganizationDTO(org))
		return
	}

	receipt, snapPending, status, code, msg := s.resumeDeletingOrganization(r.Context(), org, sc.Actor.PrincipalID, requestIDFromContext(r.Context()), "operator erasure finalization retry")
	if status != http.StatusOK {
		errType := "upstream_error"
		if status == http.StatusConflict {
			errType = "conflict_error"
		} else if status == http.StatusNotFound {
			errType = "not_found_error"
		}
		writeErrorEnvelope(w, r, status, code, errType, msg, status >= 500, nil)
		return
	}
	if snapPending {
		w.Header().Set("X-Tanpopo-Snapshot-Sync", "pending")
	}
	writeJSON(w, http.StatusOK, receipt)
}

// ResumePendingOrgDeletions sweeps any organizations stranded in DELETING / PENDING_ERASURE
// from an interrupted Phase 3 relational purge + audit commit and finalizes them idempotently.
func (s *Server) ResumePendingOrgDeletions(ctx context.Context) error {
	if s == nil || s.db == nil || s.storage == nil {
		return nil
	}
	s.orgErasureMu.Lock()
	defer s.orgErasureMu.Unlock()

	var pendingIDs []string
	if err := s.withOperatorReadTx(ctx, func(dbq adminDBQuerier) error {
		rows, qerr := dbq.QueryContext(ctx, s.rebind(`
			SELECT id FROM organizations
			WHERE status IN ('DELETING', 'PENDING_ERASURE')
			ORDER BY updated_at_ms ASC, id ASC`),
		)
		if qerr != nil {
			return qerr
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if serr := rows.Scan(&id); serr != nil {
				return serr
			}
			pendingIDs = append(pendingIDs, id)
		}
		return rows.Err()
	}); err != nil {
		return fmt.Errorf("list deleting organizations: %w", err)
	}

	var firstErr error
	for _, orgID := range pendingIDs {
		org, err := s.storage.Tenancy().GetOrganization(ctx, orgID)
		if errors.Is(err, port.ErrNotFound) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !isDeletingOrgStatus(org.Status) {
			continue
		}
		_, _, status, _, msg := s.resumeDeletingOrganization(ctx, org, "system:erasure-recovery", "", "startup pending organization erasure finalization")
		if status != http.StatusOK && firstErr == nil {
			firstErr = fmt.Errorf("finalize deleting org %s: %s", orgID, msg)
		}
	}
	return firstErr
}

func (s *Server) lookupCompletedOrgShredReceipt(ctx context.Context, orgID string) (cryptoShredReceiptDTO, bool) {
	var createdMs int64
	var metaJSON sql.NullString
	err := s.withOrgReadTx(ctx, orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(ctx, s.rebind(`
			SELECT created_at_ms, metadata_json
			FROM audit_events
			WHERE chain_key = ? AND event_type = 'organization.crypto_shredded' AND target_id = ?
			ORDER BY seq_no DESC LIMIT 1`),
			orgID, orgID,
		).Scan(&createdMs, &metaJSON)
	})
	if err != nil {
		return cryptoShredReceiptDTO{}, false
	}
	destroyedDEK := orgID + ":shredded"
	revokedKeys := 0
	if metaJSON.Valid && strings.TrimSpace(metaJSON.String) != "" {
		var meta struct {
			DestroyedDEKID string `json:"destroyed_dek_id"`
			RevokedKeys    int    `json:"revoked_keys"`
		}
		if jerr := json.Unmarshal([]byte(metaJSON.String), &meta); jerr == nil {
			if strings.TrimSpace(meta.DestroyedDEKID) != "" {
				destroyedDEK = strings.TrimSpace(meta.DestroyedDEKID)
			}
			revokedKeys = meta.RevokedKeys
		}
	}
	return cryptoShredReceiptDTO{
		TargetType:              "ORGANIZATION",
		TargetID:                orgID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(createdMs),
		RevokedVirtualKeysCount: revokedKeys,
	}, true
}

func (s *Server) resumeDeletingOrganization(ctx context.Context, org *port.Organization, actorID, reqID, reason string) (cryptoShredReceiptDTO, bool, int, string, string) {
	orgID := org.ID
	now := s.nowMs()
	destroyedDEK := orgID + ":shredded"

	// Ensure DEKs and pending shred cleanups are destroyed idempotently if Phase 2 was not finished.
	if s.vault != nil {
		meta, merr := s.vault.GetOrgKeyMetadata(ctx, orgID)
		if merr != nil && !errors.Is(merr, port.ErrNotFound) {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed inspecting organization encryption key metadata"
		}
		if merr == nil && meta != nil {
			shredReqID := fmt.Sprintf("admin-org-shred-resume-%s-%d", orgID, now)
			if serr := s.vault.ShredOrgDEK(ctx, orgID, vault.ShredRequest{
				RequestID:          shredReqID,
				ExpectedGeneration: meta.KeyGeneration,
			}); serr != nil {
				return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed crypto-shredding organization DEK during erasure recovery"
			}
			destroyedDEK = fmt.Sprintf("%s:v%d:g%d", orgID, meta.DEKVersion, meta.KeyGeneration)
		} else {
			if ferr := s.vault.FlushPendingShredCleanups(ctx); ferr != nil {
				return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed flushing pending vault shred cleanup"
			}
			s.vault.EvictAllDEKs()
		}
	}

	var activeKeyIDs []string
	var orgPrincipals []string

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed starting organization erasure finalization tx"
	}
	defer func() { _ = tx.Rollback() }()
	if err := port.SetOrgRLSTx(ctx, tx, s.dialect, orgID); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed scoping organization erasure finalization tx"
	}

	kRows, kerr := tx.QueryContext(ctx, s.rebind(`
		SELECT id FROM virtual_keys
		WHERE org_id = ? AND state NOT IN ('REVOKED', 'EXPIRED', 'PURGED_METADATA_RETAINED')`),
		orgID,
	)
	if kerr != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed enumerating organization virtual keys"
	}
	for kRows.Next() {
		var kid string
		if serr := kRows.Scan(&kid); serr != nil {
			_ = kRows.Close()
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed scanning virtual key id"
		}
		activeKeyIDs = append(activeKeyIDs, kid)
	}
	if ierr := kRows.Err(); ierr != nil {
		_ = kRows.Close()
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed iterating organization virtual keys"
	}
	_ = kRows.Close()

	pRows, perr := tx.QueryContext(ctx, s.rebind(`SELECT id FROM principals WHERE org_id = ?`), orgID)
	if perr != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed enumerating organization principals"
	}
	for pRows.Next() {
		var pid string
		if serr := pRows.Scan(&pid); serr != nil {
			_ = pRows.Close()
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed scanning principal id"
		}
		orgPrincipals = append(orgPrincipals, pid)
	}
	if ierr := pRows.Err(); ierr != nil {
		_ = pRows.Close()
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed iterating organization principals"
	}
	_ = pRows.Close()

	if err := cleanupOrgRestrictedFKsTx(ctx, tx, s.dialect, orgID); err != nil {
		if errors.Is(err, port.ErrResourceInUse) {
			return cryptoShredReceiptDTO{}, false, http.StatusConflict, "resource_in_use", "organization cannot be finalized while principals are referenced by global kill switches or change requests"
		}
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed cleaning up organization dependent resources"
	}
	if hasACR, acrErr := tableExistsTx(ctx, tx, s.dialect, "admin_change_requests"); acrErr != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed checking admin change requests table"
	} else if hasACR {
		if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM admin_change_requests WHERE org_id = ?`), orgID); err != nil {
			return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed clearing organization change requests"
		}
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM emergency_kill_switches WHERE org_id = ?`), orgID); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed clearing organization kill switches"
	}

	resDel, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM organizations WHERE id = ? AND status IN ('DELETING', 'PENDING_ERASURE')`), orgID)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed deleting organization row"
	}
	affected, _ := resDel.RowsAffected()
	if affected == 0 {
		_ = tx.Rollback()
		if receipt, found := s.lookupCompletedOrgShredReceipt(ctx, orgID); found {
			return receipt, false, http.StatusOK, "", ""
		}
		return cryptoShredReceiptDTO{}, false, http.StatusConflict, "optimistic_lock_conflict", "organization erasure state changed concurrently"
	}

	shredEpoch, err := s.appendChangelogTx(ctx, tx, orgID, "organization", orgID, "SHRED", true, now)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed recording shred changelog"
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed generating shred audit id"
	}
	metaJSON, err := json.Marshal(map[string]any{
		"reason":           strings.TrimSpace(reason),
		"destroyed_dek_id": destroyedDEK,
		"revoked_keys":     len(activeKeyIDs),
		"resumed_erasure":  true,
	})
	if err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed encoding shred audit metadata"
	}
	if err := s.txAudit.AppendTx(ctx, tx, port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      actorID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "organization.crypto_shredded",
		TargetType:   "organization",
		TargetID:     orgID,
		RequestID:    ptrOrNil(reqID),
		MetadataJSON: string(metaJSON),
	}); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed recording organization crypto-shred audit receipt"
	}

	if err := tx.Commit(); err != nil {
		return cryptoShredReceiptDTO{}, false, http.StatusInternalServerError, "internal_error", "failed committing organization erasure finalization"
	}

	if s.auth != nil {
		for _, pid := range orgPrincipals {
			s.auth.RevokePrincipalSessions(pid)
		}
	}
	if s.vault != nil {
		s.vault.EvictAllDEKs()
	}
	if s.killSwitch != nil {
		_ = s.killSwitch.RefreshFromDB(ctx)
		if s.killSwitch.Streams() != nil {
			for _, kid := range activeKeyIDs {
				s.killSwitch.Streams().AbortMatching(killswitch.LevelVirtualKey, orgID, kid)
			}
			s.killSwitch.Streams().AbortMatching(killswitch.LevelProjectOrOrgFreeze, orgID, orgID)
		}
	}
	snapPending := false
	if s.snapshots != nil {
		if s.snapshots.Streams() != nil {
			s.snapshots.Streams().AbortScope(orgID, "")
		}
		if refErr := s.snapshots.Refresh(ctx); refErr != nil {
			snapPending = true
			for _, kid := range activeKeyIDs {
				s.snapshots.FenceKeyRevocation(kid, "EMERGENCY_SECURITY", now, shredEpoch)
			}
			s.emitSecurityAudit(ctx, &authSessionContext{OrgID: orgID, Actor: &rbac.Actor{PrincipalID: actorID}}, "organization.snapshot_sync_pending", map[string]any{
				"org_id":      orgID,
				"shred_epoch": shredEpoch,
				"fenced_keys": len(activeKeyIDs),
				"error":       refErr.Error(),
			})
		}
	}
	s.publishChange(ctx, snapshot.ChangeEvent{
		ConfigEpoch:  shredEpoch,
		OrgID:        orgID,
		EntityKind:   "organization",
		EntityID:     orgID,
		MutationType: port.MutationShred,
		Emergency:    true,
	})

	return cryptoShredReceiptDTO{
		TargetType:              "ORGANIZATION",
		TargetID:                orgID,
		DestroyedDEKID:          destroyedDEK,
		ShreddedAt:              formatTimeRFC3339Ms(now),
		RevokedVirtualKeysCount: len(activeKeyIDs),
	}, snapPending, http.StatusOK, "", ""
}
