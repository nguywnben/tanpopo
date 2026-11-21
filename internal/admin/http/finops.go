// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguywnben/tanpopo/internal/config/snapshot"
	"github.com/nguywnben/tanpopo/internal/finops/ledger"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type budgetResourceDTO struct {
	ID                      string `json:"id"`
	ScopeTier               string `json:"scope_tier"`
	ScopeID                 string `json:"scope_id"`
	Period                  string `json:"period"`
	HardLimitMicros         int64  `json:"hard_limit_micros"`
	SoftWarningPercents     []int  `json:"soft_warning_percents"`
	SettledExactMicros      int64  `json:"settled_exact_micros"`
	SettledEstimatedMicros  int64  `json:"settled_estimated_micros"`
	ReservedHoldMicros      int64  `json:"reserved_hold_micros"`
	NegativeCarryoverMicros int64  `json:"negative_carryover_micros"`
	UnmeteredRequestCount   int64  `json:"unmetered_request_count"`
	Version                 int64  `json:"version"`
}

type updateBudgetRequest struct {
	HardLimitMicros     *int64 `json:"hard_limit_micros"`
	Period              string `json:"period"`
	SoftWarningPercents []int  `json:"soft_warning_percents"`
	ScopeTier           string `json:"scope_tier,omitempty"`
	ScopeID             string `json:"scope_id,omitempty"`
}

type usageChargebackRowDTO struct {
	GroupKey              string `json:"group_key"`
	ExactMicroUSD         int64  `json:"exact_micro_usd"`
	EstimatedMicroUSD     int64  `json:"estimated_micro_usd"`
	ExactRequestCount     int64  `json:"exact_request_count"`
	EstimatedRequestCount int64  `json:"estimated_request_count"`
	UnmeteredRequestCount int64  `json:"unmetered_request_count"`
}

func (s *Server) handleListBudgets(w http.ResponseWriter, r *http.Request) {
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
		var exists int
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM projects WHERE org_id = ? AND id = ?`), orgID, projFilter).Scan(&exists)
		})
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating project", true, nil)
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionBudgetRead, rbac.TargetScope{OrgID: orgID, ProjectID: projFilter}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionBudgetRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("budgets|" + projFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "budgets", orgID, filterHash)
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
	}

	if !orgWide && projFilter == "" && len(allowedProjects) == 0 {
		writeJSON(w, http.StatusOK, paginatedListEnvelope[budgetResourceDTO]{
			Data:    []budgetResourceDTO{},
			HasMore: false,
		})
		return
	}

	type bdgRow struct {
		dto       budgetResourceDTO
		createdMs int64
	}
	items := make([]bdgRow, 0, limit+1)
	needFilter := projFilter != "" || !orgWide
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
		SELECT id, scope_type, scope_id, period_kind, hard_limit_micros,
		       reserved_hold_micros, settled_exact_micros, settled_estimated_micros,
		       negative_carryover_micros, soft_alert_thresholds_pct_json,
		       version, created_at_ms
		FROM budget_ledgers
		WHERE org_id = ?`)
		if hasCur {
			q.WriteString(` AND (created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
			args = append(args, batchCreatedMs, batchCreatedMs, batchLastID)
		}
		q.WriteString(` ORDER BY created_at_ms DESC, id DESC LIMIT ?`)
		args = append(args, batchLimit)

		type rawBudgetRow struct {
			id           string
			scopeType    string
			scopeID      string
			periodKind   string
			threshJSON   string
			hardLimit    int64
			reservedHold int64
			exactMicros  int64
			estMicros    int64
			carryover    int64
			ver          int64
			createdMs    int64
		}
		rawRows := make([]rawBudgetRow, 0, batchLimit)
		var scanErr error
		if err := s.withOrgReadTx(r.Context(), orgID, func(qdb adminDBQuerier) error {
			rows, qerr := qdb.QueryContext(r.Context(), s.rebind(q.String()), args...)
			if qerr != nil {
				return qerr
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var rrow rawBudgetRow
				if serr := rows.Scan(
					&rrow.id, &rrow.scopeType, &rrow.scopeID, &rrow.periodKind, &rrow.hardLimit,
					&rrow.reservedHold, &rrow.exactMicros, &rrow.estMicros, &rrow.carryover,
					&rrow.threshJSON, &rrow.ver, &rrow.createdMs,
				); serr != nil {
					scanErr = serr
					return nil
				}
				rawRows = append(rawRows, rrow)
			}
			if ierr := rows.Err(); ierr != nil {
				return ierr
			}
			return rows.Close()
		}); err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing budgets", true, nil)
			return
		}
		if scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning budget ledger", true, nil)
			return
		}

		sqlCount := len(rawRows)
		for _, rrow := range rawRows {
			batchCreatedMs = rrow.createdMs
			batchLastID = rrow.id
			resolvedProj := s.resolveBudgetScopeProjectID(r.Context(), orgID, rrow.scopeType, rrow.scopeID)
			if projFilter != "" && resolvedProj != projFilter {
				continue
			}
			if !orgWide && (resolvedProj == "" || !allowedProjects[resolvedProj]) {
				continue
			}

			unmetered := s.countScopeUnmeteredRequests(r.Context(), orgID, rrow.scopeType, rrow.scopeID)
			items = append(items, bdgRow{
				dto: budgetResourceDTO{
					ID:                      rrow.id,
					ScopeTier:               mapBudgetScopeDBToOpenAPI(rrow.scopeType),
					ScopeID:                 rrow.scopeID,
					Period:                  mapBudgetPeriodOpenAPI(rrow.periodKind),
					HardLimitMicros:         rrow.hardLimit,
					SoftWarningPercents:     decodeSoftThresholds(rrow.threshJSON),
					SettledExactMicros:      rrow.exactMicros,
					SettledEstimatedMicros:  rrow.estMicros,
					ReservedHoldMicros:      rrow.reservedHold,
					NegativeCarryoverMicros: rrow.carryover,
					UnmeteredRequestCount:   unmetered,
					Version:                 rrow.ver,
				},
				createdMs: rrow.createdMs,
			})
			if len(items) >= limit+1 {
				break
			}
		}
		if len(items) >= limit+1 || sqlCount < batchLimit {
			break
		}
		hasCur = true
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]budgetResourceDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "budgets", orgID, filterHash, last.createdMs, last.dto.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[budgetResourceDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleUpdateBudget(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	budgetID := strings.TrimSpace(r.PathValue("budgetId"))
	if budgetID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "budget not found", false, nil)
		return
	}
	var req updateBudgetRequest
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionBudgetWrite); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	// Cross-tenant existence check (AUTH-15): foreign-org rows follow create/auth path and return uniform 404 after RBAC + validation.
	var rowOrgID, scopeType, scopeID, existingPeriodKind string
	var existingPeriodStart, existingPeriodEnd int64
	err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`
		SELECT org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms
		FROM budget_ledgers
		WHERE id = ?`), budgetID,
		).Scan(&rowOrgID, &scopeType, &scopeID, &existingPeriodKind, &existingPeriodStart, &existingPeriodEnd)
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed looking up budget ledger", true, nil)
		return
	}
	rowExists := err == nil && rowOrgID == orgID
	foreignOrgExists := err == nil && rowOrgID != orgID

	if !rowExists {
		// Creation path: determine scope & authorize before enforcing If-Match / field validation.
		if strings.TrimSpace(req.ScopeTier) == "" || strings.TrimSpace(req.ScopeID) == "" {
			if !s.authorize(w, r, sc, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgID}) {
				return
			}
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "budget not found", false, nil)
			return
		}
		dbScope, sErr := mapBudgetScopeOpenAPIToDB(req.ScopeTier)
		if sErr != nil {
			if !s.authorize(w, r, sc, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgID}) {
				return
			}
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", sErr.Error(), false, nil)
			return
		}
		targetScopeID := strings.TrimSpace(req.ScopeID)
		if dbScope == ledger.ScopeOrg && targetScopeID != orgID {
			if !s.authorize(w, r, sc, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgID}) {
				return
			}
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization scope target not found", false, nil)
			return
		}
		projID, validTarget, vErr := s.verifyBudgetTargetExists(r.Context(), orgID, dbScope, targetScopeID)
		if vErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying budget target scope", true, nil)
			return
		}
		if !validTarget {
			if !s.authorize(w, r, sc, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgID}) {
				return
			}
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "budget target scope not found", false, nil)
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
			return
		}
		if !s.requireActiveOrgAndProject(w, r, orgID, projID) {
			return
		}
		expectedVer, ok := requireIfMatch(w, r)
		if !ok {
			return
		}
		if req.HardLimitMicros == nil || *req.HardLimitMicros < 0 {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "non-negative hard_limit_micros is required", false, nil)
			return
		}
		period := strings.ToUpper(strings.TrimSpace(req.Period))
		if period != ledger.PeriodDaily && period != ledger.PeriodMonthly {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "period must be DAILY or MONTHLY", false, nil)
			return
		}
		thresholds, okThresh := normalizeAndValidateThresholds(req.SoftWarningPercents)
		if !okThresh {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "soft_warning_percents values must be integers between 1 and 100", false, nil)
			return
		}
		if foreignOrgExists || expectedVer != 1 {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "budget not found", false, nil)
			return
		}
		threshJSONBytes, err := json.Marshal(thresholds)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding soft warning thresholds", true, nil)
			return
		}

		now := s.nowMs()
		pStart, pEnd := computePeriodWindowMs(now, period)
		tx, txErr := s.db.BeginTx(r.Context(), nil)
		if txErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting budget creation tx", true, nil)
			return
		}
		defer func() { _ = tx.Rollback() }()
		if rlsErr := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); rlsErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scoping budget creation tx", true, nil)
			return
		}
		if _, insErr := tx.ExecContext(r.Context(), s.rebind(`
			INSERT INTO budget_ledgers (
				id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms,
				hard_limit_micros, soft_alert_thresholds_pct_json, version, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`),
			budgetID, orgID, dbScope, targetScopeID, period, pStart, pEnd,
			*req.HardLimitMicros, string(threshJSONBytes), now, now,
		); insErr != nil {
			var pgErr *pgconn.PgError
			if (errors.As(insErr, &pgErr) && pgErr.Code == "23505") || strings.Contains(strings.ToLower(insErr.Error()), "unique") {
				writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "budget ledger conflict", false, nil)
				return
			}
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed creating budget ledger", true, nil)
			return
		}
		if audErr := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "budget_ledger", budgetID, port.MutationUpsert, false, sc.Actor.PrincipalID, "budget.created", map[string]any{
			"scope_type":        dbScope,
			"scope_id":          targetScopeID,
			"period_kind":       period,
			"hard_limit_micros": *req.HardLimitMicros,
		}, requestIDFromContext(r.Context()), now); audErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording budget audit", true, nil)
			return
		}
		if cErr := tx.Commit(); cErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing budget ledger", true, nil)
			return
		}
		if s.snapshots != nil {
			_ = s.snapshots.Refresh(r.Context())
		}
		s.publishChange(r.Context(), snapshot.ChangeEvent{
			OrgID:        orgID,
			EntityKind:   "budget_ledger",
			EntityID:     budgetID,
			MutationType: port.MutationUpsert,
		})
		createdDTO, err := s.loadBudgetResourceDTO(r.Context(), orgID, budgetID)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading budget ledger", true, nil)
			return
		}
		w.Header().Set("ETag", FormatETag(createdDTO.Version))
		writeJSON(w, http.StatusOK, createdDTO)
		return
	}

	projID := s.resolveBudgetScopeProjectID(r.Context(), orgID, scopeType, scopeID)
	if !s.authorize(w, r, sc, rbac.ActionBudgetWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}
	if !s.requireActiveOrgAndProject(w, r, orgID, projID) {
		return
	}
	expectedVer, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.ScopeTier) != "" {
		reqScope, sErr := mapBudgetScopeOpenAPIToDB(req.ScopeTier)
		if sErr != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", sErr.Error(), false, nil)
			return
		}
		if reqScope != scopeType {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_tier cannot be mutated on existing budget", false, nil)
			return
		}
	}
	if strings.TrimSpace(req.ScopeID) != "" && strings.TrimSpace(req.ScopeID) != scopeID {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "scope_id cannot be mutated on existing budget", false, nil)
		return
	}
	if req.HardLimitMicros == nil || *req.HardLimitMicros < 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "non-negative hard_limit_micros is required", false, nil)
		return
	}
	period := strings.ToUpper(strings.TrimSpace(req.Period))
	if period != ledger.PeriodDaily && period != ledger.PeriodMonthly {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "period must be DAILY or MONTHLY", false, nil)
		return
	}
	thresholds, okThresh := normalizeAndValidateThresholds(req.SoftWarningPercents)
	if !okThresh {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "soft_warning_percents values must be integers between 1 and 100", false, nil)
		return
	}
	threshJSONBytes, err := json.Marshal(thresholds)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding soft warning thresholds", true, nil)
		return
	}

	curDTO, err := s.loadBudgetResourceDTO(r.Context(), orgID, budgetID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "budget not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading budget ledger", true, nil)
		return
	}
	if curDTO.Version != expectedVer {
		writeOptimisticConflict(w, r, curDTO.Version, curDTO)
		return
	}

	totalSettled := curDTO.SettledExactMicros + curDTO.SettledEstimatedMicros
	newCarryover := int64(0)
	if totalSettled > *req.HardLimitMicros {
		newCarryover = totalSettled - *req.HardLimitMicros
	}
	newUtilPct := computeBudgetUtilizationPct(totalSettled, *req.HardLimitMicros)
	now := s.nowMs()
	nextPeriodStart, nextPeriodEnd := existingPeriodStart, existingPeriodEnd
	if period != existingPeriodKind {
		nextPeriodStart, nextPeriodEnd = computePeriodWindowMs(now, period)
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting budget update tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()
	if rlsErr := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); rlsErr != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scoping budget update tx", true, nil)
		return
	}

	res, err := tx.ExecContext(r.Context(), s.rebind(`
		UPDATE budget_ledgers
		SET period_kind = ?,
		    period_start_ms = ?,
		    period_end_ms = ?,
		    hard_limit_micros = ?,
		    negative_carryover_micros = ?,
		    soft_alert_thresholds_pct_json = ?,
		    last_alerted_threshold_pct = CASE WHEN ? < last_alerted_threshold_pct THEN ? ELSE last_alerted_threshold_pct END,
		    version = version + 1,
		    updated_at_ms = ?
		WHERE org_id = ? AND id = ? AND version = ?`),
		period, nextPeriodStart, nextPeriodEnd, *req.HardLimitMicros, newCarryover, string(threshJSONBytes),
		newUtilPct, newUtilPct, now, orgID, budgetID, expectedVer,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if (errors.As(err, &pgErr) && pgErr.Code == "23505") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeErrorEnvelope(w, r, http.StatusConflict, "resource_already_exists", "conflict_error", "budget ledger already exists for scope and period window", false, nil)
			return
		}
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed updating budget ledger", true, nil)
		return
	}
	if aff, _ := res.RowsAffected(); aff == 0 {
		_ = tx.Rollback()
		latest, lerr := s.loadBudgetResourceDTO(r.Context(), orgID, budgetID)
		if errors.Is(lerr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "budget not found", false, nil)
			return
		}
		if lerr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading budget ledger", true, nil)
			return
		}
		writeOptimisticConflict(w, r, latest.Version, latest)
		return
	}

	if err := s.appendChangelogAndAuditTx(r.Context(), tx, &orgID, "budget_ledger", budgetID, port.MutationUpsert, false, sc.Actor.PrincipalID, "budget.updated", map[string]any{
		"period":            period,
		"hard_limit_micros": *req.HardLimitMicros,
	}, requestIDFromContext(r.Context()), now); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording budget update audit", true, nil)
		return
	}
	if err := tx.Commit(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed committing budget update", true, nil)
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	s.publishChange(r.Context(), snapshot.ChangeEvent{
		OrgID:        orgID,
		EntityKind:   "budget_ledger",
		EntityID:     budgetID,
		MutationType: port.MutationUpsert,
	})

	updatedDTO, err := s.loadBudgetResourceDTO(r.Context(), orgID, budgetID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed reloading budget ledger", true, nil)
		return
	}
	w.Header().Set("ETag", FormatETag(updatedDTO.Version))
	writeJSON(w, http.StatusOK, updatedDTO)
}

func (s *Server) handleGetUsageSummary(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionBudgetRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	groupBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("group_by")))
	switch groupBy {
	case "project", "env", "key", "model":
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "group_by must be one of project, env, key, or model", false, nil)
		return
	}

	fromMs, toMs, okRange := parseOptionalTimeWindowParams(w, r)
	if !okRange {
		return
	}

	rows, err := s.queryUsageChargebackRows(r.Context(), sc, orgID, groupBy, fromMs, toMs)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying usage chargeback summary", true, nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": rows})
}

func (s *Server) handleExportUsage(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionBudgetRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "csv"
	}
	if format != "csv" && format != "json" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "format must be csv or json", false, nil)
		return
	}

	groupBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("group_by")))
	if groupBy == "" {
		groupBy = "project"
	}
	switch groupBy {
	case "project", "env", "key", "model":
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "group_by must be one of project, env, key, or model", false, nil)
		return
	}

	fromMs, toMs, okRange := parseOptionalTimeWindowParams(w, r)
	if !okRange {
		return
	}

	rows, err := s.queryUsageChargebackRows(r.Context(), sc, orgID, groupBy, fromMs, toMs)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed exporting usage chargeback", true, nil)
		return
	}

	if format == "json" {
		writeJSON(w, http.StatusOK, rows)
		return
	}

	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	if err := cw.Write([]string{"group_key", "exact_micro_usd", "estimated_micro_usd", "exact_request_count", "estimated_request_count", "unmetered_request_count"}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding CSV header", true, nil)
		return
	}
	for _, row := range rows {
		if err := cw.Write([]string{
			sanitizeCSVCell(row.GroupKey),
			strconv.FormatInt(row.ExactMicroUSD, 10),
			strconv.FormatInt(row.EstimatedMicroUSD, 10),
			strconv.FormatInt(row.ExactRequestCount, 10),
			strconv.FormatInt(row.EstimatedRequestCount, 10),
			strconv.FormatInt(row.UnmeteredRequestCount, 10),
		}); err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding CSV row", true, nil)
			return
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed flushing CSV export", true, nil)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="tanpopo-usage.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// sanitizeCSVCell neutralizes CSV spreadsheet formula injection (=, +, -, @, \t, \r)
// including cells with leading whitespace or control prefixes.
func sanitizeCSVCell(raw string) string {
	trimmed := strings.TrimLeftFunc(raw, unicode.IsSpace)
	if raw == "" {
		return ""
	}
	check := raw
	if len(trimmed) > 0 {
		firstRaw := raw[0]
		firstTrim := trimmed[0]
		if isDangerousCSVPrefix(firstRaw) || isDangerousCSVPrefix(firstTrim) {
			return "'" + check
		}
	} else if isDangerousCSVPrefix(raw[0]) {
		return "'" + raw
	}
	return raw
}

func isDangerousCSVPrefix(b byte) bool {
	switch b {
	case '=', '+', '-', '@', '\t', '\r':
		return true
	default:
		return false
	}
}

func parseOptionalTimeWindowParams(w http.ResponseWriter, r *http.Request) (*int64, *int64, bool) {
	var fromMs, toMs *int64
	if rawFrom := strings.TrimSpace(r.URL.Query().Get("from")); rawFrom != "" {
		t, err := time.Parse(time.RFC3339Nano, rawFrom)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "from must be RFC3339 date-time", false, nil)
			return nil, nil, false
		}
		v := t.UTC().UnixMilli()
		fromMs = &v
	}
	if rawTo := strings.TrimSpace(r.URL.Query().Get("to")); rawTo != "" {
		t, err := time.Parse(time.RFC3339Nano, rawTo)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "to must be RFC3339 date-time", false, nil)
			return nil, nil, false
		}
		v := t.UTC().UnixMilli()
		toMs = &v
	}
	if fromMs != nil && toMs != nil && *fromMs > *toMs {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "from cannot exceed to", false, nil)
		return nil, nil, false
	}
	return fromMs, toMs, true
}

func (s *Server) queryUsageChargebackRows(ctx context.Context, sc *authSessionContext, orgID, groupBy string, fromMs, toMs *int64) ([]usageChargebackRowDTO, error) {
	var selectExpr, joinSQL string
	groupByExpr := "grp_key"
	switch groupBy {
	case "project":
		selectExpr = `COALESCE(p.display_name, p.slug, us.project_id)`
		joinSQL = `LEFT JOIN projects p ON p.org_id = us.org_id AND p.id = us.project_id`
		groupByExpr = `us.project_id, grp_key`
	case "env":
		selectExpr = `COALESCE(e.slug, us.env_id)`
		joinSQL = `LEFT JOIN environments e ON e.org_id = us.org_id AND e.id = us.env_id`
		groupByExpr = `us.env_id, grp_key`
	case "key":
		selectExpr = `COALESCE(vk.name, us.virtual_key_id)`
		joinSQL = `LEFT JOIN virtual_keys vk ON vk.org_id = us.org_id AND vk.id = us.virtual_key_id`
		groupByExpr = `us.virtual_key_id, grp_key`
	case "model":
		selectExpr = `COALESCE(us.logical_model_slug, 'unknown')`
	default:
		return nil, fmt.Errorf("unsupported group_by %q", groupBy)
	}

	var conds []string
	var args []any
	conds = append(conds, `us.org_id = ?`)
	args = append(args, orgID)

	if !actorHasOrgScopeBinding(sc.Actor, orgID) {
		projIDs := actorProjectIDs(sc.Actor, orgID)
		if len(projIDs) == 0 {
			return []usageChargebackRowDTO{}, nil
		}
		conds = append(conds, `us.project_id IN (`+strings.Repeat("?,", len(projIDs)-1)+`?)`)
		for _, pid := range projIDs {
			args = append(args, pid)
		}
	}
	if fromMs != nil {
		conds = append(conds, `us.settled_at_ms >= ?`)
		args = append(args, *fromMs)
	}
	if toMs != nil {
		conds = append(conds, `us.settled_at_ms <= ?`)
		args = append(args, *toMs)
	}

	query := fmt.Sprintf(`
		SELECT %s AS grp_key,
		       COALESCE(SUM(CASE WHEN us.usage_accuracy = 'EXACT' THEN us.total_cost_micros ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN us.usage_accuracy = 'ESTIMATED' THEN us.total_cost_micros ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN us.usage_accuracy = 'EXACT' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN us.usage_accuracy = 'ESTIMATED' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN us.usage_accuracy = 'UNMETERED_FLAGGED' THEN 1 ELSE 0 END), 0)
		FROM usage_settlements us
		%s
		WHERE %s
		GROUP BY %s
		ORDER BY grp_key ASC`, selectExpr, joinSQL, strings.Join(conds, " AND "), groupByExpr)

	out := make([]usageChargebackRowDTO, 0)
	if err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
		rows, qerr := q.QueryContext(ctx, s.rebind(query), args...)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var r usageChargebackRowDTO
			if scanErr := rows.Scan(&r.GroupKey, &r.ExactMicroUSD, &r.EstimatedMicroUSD, &r.ExactRequestCount, &r.EstimatedRequestCount, &r.UnmeteredRequestCount); scanErr != nil {
				return scanErr
			}
			out = append(out, r)
		}
		if ierr := rows.Err(); ierr != nil {
			return ierr
		}
		return rows.Close()
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Server) resolveBudgetScopeProjectID(ctx context.Context, orgID, scopeType, scopeID string) string {
	switch scopeType {
	case ledger.ScopeProject:
		return scopeID
	case ledger.ScopeEnv:
		pid, found, _ := s.lookupEnvironmentProject(ctx, orgID, scopeID)
		if found {
			return pid
		}
	case ledger.ScopeVirtualKey:
		var pid string
		_ = s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(ctx, s.rebind(`SELECT project_id FROM virtual_keys WHERE org_id = ? AND id = ?`), orgID, scopeID).Scan(&pid)
		})
		return pid
	}
	return ""
}

func (s *Server) verifyBudgetTargetExists(ctx context.Context, orgID, scopeType, scopeID string) (string, bool, error) {
	switch scopeType {
	case ledger.ScopeOrg:
		var exists int
		err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(ctx, s.rebind(`SELECT 1 FROM organizations WHERE id = ?`), orgID).Scan(&exists)
		})
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", err == nil, err
	case ledger.ScopeProject:
		var exists int
		err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(ctx, s.rebind(`SELECT 1 FROM projects WHERE org_id = ? AND id = ?`), orgID, scopeID).Scan(&exists)
		})
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return scopeID, err == nil, err
	case ledger.ScopeEnv:
		return s.lookupEnvironmentProject(ctx, orgID, scopeID)
	case ledger.ScopeVirtualKey:
		var pid string
		err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(ctx, s.rebind(`SELECT project_id FROM virtual_keys WHERE org_id = ? AND id = ?`), orgID, scopeID).Scan(&pid)
		})
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return pid, err == nil, err
	default:
		return "", false, nil
	}
}

func (s *Server) countScopeUnmeteredRequests(ctx context.Context, orgID, scopeType, scopeID string) int64 {
	var cnt int64
	_ = s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
		switch scopeType {
		case ledger.ScopeOrg:
			return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM usage_settlements WHERE org_id = ? AND usage_accuracy = 'UNMETERED_FLAGGED'`), orgID).Scan(&cnt)
		case ledger.ScopeProject:
			return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM usage_settlements WHERE org_id = ? AND project_id = ? AND usage_accuracy = 'UNMETERED_FLAGGED'`), orgID, scopeID).Scan(&cnt)
		case ledger.ScopeEnv:
			return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM usage_settlements WHERE org_id = ? AND env_id = ? AND usage_accuracy = 'UNMETERED_FLAGGED'`), orgID, scopeID).Scan(&cnt)
		case ledger.ScopeVirtualKey:
			return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM usage_settlements WHERE org_id = ? AND virtual_key_id = ? AND usage_accuracy = 'UNMETERED_FLAGGED'`), orgID, scopeID).Scan(&cnt)
		default:
			return nil
		}
	})
	return cnt
}

func (s *Server) loadBudgetResourceDTO(ctx context.Context, orgID, budgetID string) (budgetResourceDTO, error) {
	var (
		id, scopeType, scopeID, periodKind, threshJSON  string
		hardLimit, reservedHold, exactMicros, estMicros int64
		carryover, ver                                  int64
	)
	err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(ctx, s.rebind(`
		SELECT id, scope_type, scope_id, period_kind, hard_limit_micros,
		       reserved_hold_micros, settled_exact_micros, settled_estimated_micros,
		       negative_carryover_micros, soft_alert_thresholds_pct_json, version
		FROM budget_ledgers
		WHERE org_id = ? AND id = ?`), orgID, budgetID,
		).Scan(&id, &scopeType, &scopeID, &periodKind, &hardLimit, &reservedHold, &exactMicros, &estMicros, &carryover, &threshJSON, &ver)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return budgetResourceDTO{}, port.ErrNotFound
	}
	if err != nil {
		return budgetResourceDTO{}, err
	}
	return budgetResourceDTO{
		ID:                      id,
		ScopeTier:               mapBudgetScopeDBToOpenAPI(scopeType),
		ScopeID:                 scopeID,
		Period:                  mapBudgetPeriodOpenAPI(periodKind),
		HardLimitMicros:         hardLimit,
		SoftWarningPercents:     decodeSoftThresholds(threshJSON),
		SettledExactMicros:      exactMicros,
		SettledEstimatedMicros:  estMicros,
		ReservedHoldMicros:      reservedHold,
		NegativeCarryoverMicros: carryover,
		UnmeteredRequestCount:   s.countScopeUnmeteredRequests(ctx, orgID, scopeType, scopeID),
		Version:                 ver,
	}, nil
}

func mapBudgetScopeDBToOpenAPI(st string) string {
	switch st {
	case ledger.ScopeOrg:
		return "ORGANIZATION"
	case ledger.ScopeProject:
		return "PROJECT"
	case ledger.ScopeEnv:
		return "ENVIRONMENT"
	case ledger.ScopeVirtualKey:
		return "VIRTUAL_KEY"
	default:
		return "ORGANIZATION"
	}
}

func mapBudgetScopeOpenAPIToDB(tier string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(tier)) {
	case "ORGANIZATION", "ORG":
		return ledger.ScopeOrg, nil
	case "PROJECT":
		return ledger.ScopeProject, nil
	case "ENVIRONMENT", "ENV":
		return ledger.ScopeEnv, nil
	case "VIRTUAL_KEY":
		return ledger.ScopeVirtualKey, nil
	default:
		return "", errors.New("scope_tier must be ORGANIZATION, PROJECT, ENVIRONMENT, or VIRTUAL_KEY")
	}
}

func mapBudgetPeriodOpenAPI(pk string) string {
	if pk == ledger.PeriodDaily {
		return ledger.PeriodDaily
	}
	return ledger.PeriodMonthly
}

func decodeSoftThresholds(raw string) []int {
	var parsed []int
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &parsed)
	}
	if len(parsed) == 0 {
		return []int{50, 80, 100}
	}
	return parsed
}

func normalizeAndValidateThresholds(in []int) ([]int, bool) {
	if len(in) == 0 {
		return []int{50, 80, 100}, true
	}
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v < 1 || v > 100 {
			return nil, false
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out, true
}

func computePeriodWindowMs(nowMs int64, period string) (int64, int64) {
	t := time.UnixMilli(nowMs).UTC()
	if period == ledger.PeriodDaily {
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli()
	}
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start.UnixMilli(), start.AddDate(0, 1, 0).UnixMilli()
}

func computeBudgetUtilizationPct(totalSettled, hardLimit int64) int {
	if totalSettled <= 0 {
		return 0
	}
	if hardLimit <= 0 || totalSettled >= hardLimit {
		return 100
	}
	pct := (totalSettled / hardLimit) * 100
	rem := totalSettled % hardLimit
	if rem > 0 {
		if rem <= 9_000_000_000_000_000 {
			pct += (rem * 100) / hardLimit
		} else {
			pct += rem / (hardLimit / 100)
		}
	}
	if pct > 100 {
		pct = 100
	}
	return int(pct)
}
