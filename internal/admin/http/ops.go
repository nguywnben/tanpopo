// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/extension/installer"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/i18n"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type overviewSummaryResponseDTO struct {
	Window                string  `json:"window"`
	TotalRequests         int64   `json:"total_requests"`
	ErrorRateRatio        float64 `json:"error_rate_ratio"`
	P95OverheadMs         float64 `json:"p95_overhead_ms"`
	SettledMicroUSD       int64   `json:"settled_micro_usd"`
	ActiveHoldMicros      int64   `json:"active_hold_micros"`
	UnmeteredRequestCount int64   `json:"unmetered_request_count"`
	ActiveAlertsCount     int     `json:"active_alerts_count"`
	SyncLagWarning        bool    `json:"sync_lag_warning"`
	UpdatedAt             string  `json:"updated_at"`
}

type alertItemDTO struct {
	ID               string `json:"id"`
	AlertCode        string `json:"alert_code"`
	Severity         string `json:"severity"`
	Summary          string `json:"summary"`
	RemediationRoute string `json:"remediation_route"`
	TriggeredAt      string `json:"triggered_at"`
}

type auditEventResourceDTO struct {
	ID             string         `json:"id"`
	OrgID          *string        `json:"org_id"`
	ChainPartition string         `json:"chain_partition"`
	SeqNo          int64          `json:"seq_no"`
	ActorID        string         `json:"actor_id"`
	Action         string         `json:"action"`
	TargetType     string         `json:"target_type"`
	TargetID       string         `json:"target_id"`
	Metadata       map[string]any `json:"metadata"`
	PrevHash       string         `json:"prev_hash"`
	EventHash      string         `json:"event_hash"`
	CreatedAt      string         `json:"created_at"`
}

type verifyAuditResponseDTO struct {
	Intact             bool   `json:"intact"`
	CheckedEventsCount int    `json:"checked_events_count"`
	FirstBrokenSeqNo   *int64 `json:"first_broken_seq_no"`
}

type killSwitchTriggerRequestDTO struct {
	TargetLevel        string `json:"target_level"`
	TargetID           string `json:"target_id"`
	ConfirmSlug        string `json:"confirm_slug"`
	AbortActiveStreams *bool  `json:"abort_active_streams"`
	Reason             string `json:"reason"`
}

type killSwitchTriggerResponseDTO struct {
	ConfigEpoch           int64 `json:"config_epoch"`
	SeveredStreamsCount   int   `json:"severed_streams_count"`
	AffectedEntitiesCount int   `json:"affected_entities_count"`
}

type clearKillSwitchRequestDTO struct {
	ExpectedVersion int64  `json:"expected_version"`
	ClearReason     string `json:"clear_reason"`
}

type blastRadiusResponseDTO struct {
	Organizations []string `json:"organizations"`
	Projects      []string `json:"projects"`
	VirtualKeys   []string `json:"virtual_keys"`
	Connections   []string `json:"connections"`
}

func (s *Server) handleGetOverviewSummary(w http.ResponseWriter, r *http.Request) {
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

	windowParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("window")))
	if windowParam == "" {
		windowParam = "24h"
	}
	var windowDur time.Duration
	switch windowParam {
	case "1h":
		windowDur = time.Hour
	case "24h":
		windowDur = 24 * time.Hour
	case "7d":
		windowDur = 7 * 24 * time.Hour
	case "30d":
		windowDur = 30 * 24 * time.Hour
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "window must be 1h, 24h, 7d, or 30d", false, nil)
		return
	}

	nowMs := s.nowMs()
	sinceMs := nowMs - windowDur.Milliseconds()
	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	projIDs := actorProjectIDs(sc.Actor, orgID)

	var traceConds []string
	var traceArgs []any
	traceConds = append(traceConds, `org_id = ?`, `created_at_ms >= ?`)
	traceArgs = append(traceArgs, orgID, sinceMs)
	if !orgWide {
		if len(projIDs) == 0 {
			writeJSON(w, http.StatusOK, overviewSummaryResponseDTO{
				Window:    windowParam,
				UpdatedAt: formatTimeRFC3339Ms(nowMs),
			})
			return
		}
		traceConds = append(traceConds, `project_id IN (`+strings.Repeat("?,", len(projIDs)-1)+`?)`)
		for _, pid := range projIDs {
			traceArgs = append(traceArgs, pid)
		}
	}

	var totalReqs, errReqs int64
	var overheads []int64
	if err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		traceRows, qerr := q.QueryContext(r.Context(), s.rebind(`
		SELECT http_status, proxy_overhead_us
		FROM request_traces_meta
		WHERE `+strings.Join(traceConds, " AND ")), traceArgs...)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = traceRows.Close() }()
		for traceRows.Next() {
			var status int
			var overUs int64
			if scanErr := traceRows.Scan(&status, &overUs); scanErr != nil {
				return scanErr
			}
			totalReqs++
			if status >= 400 {
				errReqs++
			}
			overheads = append(overheads, overUs)
		}
		if ierr := traceRows.Err(); ierr != nil {
			return ierr
		}
		return traceRows.Close()
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying overview trace metrics", true, nil)
		return
	}

	errRatio := 0.0
	if totalReqs > 0 {
		errRatio = float64(errReqs) / float64(totalReqs)
	}
	p95Ms := 0.0
	if len(overheads) > 0 {
		sort.Slice(overheads, func(i, j int) bool { return overheads[i] < overheads[j] })
		idx := int(math.Ceil(0.95*float64(len(overheads)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(overheads) {
			idx = len(overheads) - 1
		}
		p95Ms = float64(overheads[idx]) / 1000.0
	}

	var settleConds []string
	var settleArgs []any
	settleConds = append(settleConds, `org_id = ?`, `settled_at_ms >= ?`)
	settleArgs = append(settleArgs, orgID, sinceMs)
	if !orgWide && len(projIDs) > 0 {
		settleConds = append(settleConds, `project_id IN (`+strings.Repeat("?,", len(projIDs)-1)+`?)`)
		for _, pid := range projIDs {
			settleArgs = append(settleArgs, pid)
		}
	}

	var settledMicros, unmeteredCount int64
	if err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		return q.QueryRowContext(r.Context(), s.rebind(`
		SELECT COALESCE(SUM(total_cost_micros), 0),
		       COALESCE(SUM(CASE WHEN usage_accuracy = 'UNMETERED_FLAGGED' THEN 1 ELSE 0 END), 0)
		FROM usage_settlements
		WHERE `+strings.Join(settleConds, " AND ")), settleArgs...,
		).Scan(&settledMicros, &unmeteredCount)
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying overview financial metrics", true, nil)
		return
	}

	type rawHoldRow struct {
		scopeType string
		scopeID   string
		hold      int64
	}
	var rawHolds []rawHoldRow
	if err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
		holdRows, qerr := q.QueryContext(r.Context(), s.rebind(`SELECT scope_type, scope_id, reserved_hold_micros FROM budget_ledgers WHERE org_id = ?`), orgID)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = holdRows.Close() }()
		for holdRows.Next() {
			var rrow rawHoldRow
			if scanErr := holdRows.Scan(&rrow.scopeType, &rrow.scopeID, &rrow.hold); scanErr != nil {
				return scanErr
			}
			rawHolds = append(rawHolds, rrow)
		}
		if ierr := holdRows.Err(); ierr != nil {
			return ierr
		}
		return holdRows.Close()
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying active holds", true, nil)
		return
	}

	var activeHolds int64
	allowedProjSet := make(map[string]bool, len(projIDs))
	for _, pid := range projIDs {
		allowedProjSet[pid] = true
	}
	for _, rrow := range rawHolds {
		if !orgWide {
			resolvedProj := s.resolveBudgetScopeProjectID(r.Context(), orgID, rrow.scopeType, rrow.scopeID)
			if resolvedProj == "" || !allowedProjSet[resolvedProj] {
				continue
			}
		}
		activeHolds += rrow.hold
	}

	alerts, err := s.collectActiveAlerts(r.Context(), i18n.ResolveAcceptLanguage(r.Header.Get("Accept-Language")), orgID, orgWide, allowedProjSet, nowMs)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed collecting active alerts", true, nil)
		return
	}
	syncLag := false
	if s.snapshots != nil {
		if s.snapshots.CheckServingReady() != nil {
			syncLag = true
		}
	}

	writeJSON(w, http.StatusOK, overviewSummaryResponseDTO{
		Window:                windowParam,
		TotalRequests:         totalReqs,
		ErrorRateRatio:        errRatio,
		P95OverheadMs:         p95Ms,
		SettledMicroUSD:       settledMicros,
		ActiveHoldMicros:      activeHolds,
		UnmeteredRequestCount: unmeteredCount,
		ActiveAlertsCount:     len(alerts),
		SyncLagWarning:        syncLag,
		UpdatedAt:             formatTimeRFC3339Ms(nowMs),
	})
}

func (s *Server) handleListActiveAlerts(w http.ResponseWriter, r *http.Request) {
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

	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	projIDs := actorProjectIDs(sc.Actor, orgID)
	allowedProjSet := make(map[string]bool, len(projIDs))
	for _, pid := range projIDs {
		allowedProjSet[pid] = true
	}

	filterHash := sha256HexString("alerts")
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "alerts", orgID, filterHash)
	if !ok {
		return
	}

	loc := i18n.ResolveAcceptLanguage(r.Header.Get("Accept-Language"))
	alerts, err := s.collectActiveAlerts(r.Context(), loc, orgID, orgWide, allowedProjSet, s.nowMs())
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading active alerts", true, nil)
		return
	}

	startIdx := 0
	if cur != nil {
		for idx, item := range alerts {
			if item.ID > cur.LastID {
				startIdx = idx
				break
			}
			startIdx = len(alerts)
		}
	}
	endIdx := startIdx + limit
	hasMore := false
	if endIdx < len(alerts) {
		hasMore = true
	} else {
		endIdx = len(alerts)
	}
	page := make([]alertItemDTO, 0, endIdx-startIdx)
	if startIdx < endIdx {
		page = append(page, alerts[startIdx:endIdx]...)
	}
	var nextCursor *string
	if hasMore && len(page) > 0 {
		last := page[len(page)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "alerts", orgID, filterHash, 0, last.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	w.Header().Set("Content-Language", string(loc))
	w.Header().Add("Vary", "Accept-Language")
	writeJSON(w, http.StatusOK, paginatedListEnvelope[alertItemDTO]{
		Data:       page,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) collectActiveAlerts(ctx context.Context, loc i18n.Locale, orgID string, orgWide bool, allowedProjSet map[string]bool, nowMs int64) ([]alertItemDTO, error) {
	var alerts []alertItemDTO

	// 1. Budget >= 80% alerts (ALT-BUDGET-80)
	type budgetAlertRow struct {
		id        string
		scopeType string
		scopeID   string
		limitM    int64
		usedM     int64
		updMs     int64
	}
	var scannedBudgets []budgetAlertRow
	if err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
		bRows, qerr := q.QueryContext(ctx, s.rebind(`
		SELECT id, scope_type, scope_id, hard_limit_micros,
		       settled_exact_micros + settled_estimated_micros + reserved_hold_micros,
		       updated_at_ms
		FROM budget_ledgers
		WHERE org_id = ? AND hard_limit_micros > 0`), orgID,
		)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = bRows.Close() }()
		for bRows.Next() {
			var item budgetAlertRow
			if scanErr := bRows.Scan(&item.id, &item.scopeType, &item.scopeID, &item.limitM, &item.usedM, &item.updMs); scanErr != nil {
				return scanErr
			}
			scannedBudgets = append(scannedBudgets, item)
		}
		if ierr := bRows.Err(); ierr != nil {
			return ierr
		}
		return bRows.Close()
	}); err != nil {
		return nil, err
	}
	for _, b := range scannedBudgets {
		if !orgWide {
			resolvedProj := s.resolveBudgetScopeProjectID(ctx, orgID, b.scopeType, b.scopeID)
			if resolvedProj == "" || !allowedProjSet[resolvedProj] {
				continue
			}
		}
		if b.limitM > 0 && computeBudgetUtilizationPct(b.usedM, b.limitM) >= 80 {
			alerts = append(alerts, alertItemDTO{
				ID:               "alt_budget_" + b.id,
				AlertCode:        "ALT-BUDGET-80",
				Severity:         "TICKET",
				Summary:          i18n.AlertSummary(loc, "ALT-BUDGET-80", fmt.Sprintf("%s budget %s exceeded 80%% utilization", b.scopeType, b.id), "scope", b.scopeType, "id", b.id),
				RemediationRoute: "/budgets",
				TriggeredAt:      formatTimeRFC3339Ms(b.updMs),
			})
		}
	}

	// 2. Unmetered spend alert (ALT-UNMETERED-SPEND)
	if orgWide || len(allowedProjSet) > 0 {
		var uConds []string
		var uArgs []any
		uConds = append(uConds, `org_id = ?`, `usage_accuracy = 'UNMETERED_FLAGGED'`, `settled_at_ms >= ?`)
		uArgs = append(uArgs, orgID, nowMs-(24*time.Hour).Milliseconds())
		if !orgWide {
			projList := make([]string, 0, len(allowedProjSet))
			for pid := range allowedProjSet {
				projList = append(projList, pid)
			}
			sort.Strings(projList)
			uConds = append(uConds, `project_id IN (`+strings.Repeat("?,", len(projList)-1)+`?)`)
			for _, pid := range projList {
				uArgs = append(uArgs, pid)
			}
		}
		var unmeteredCnt int64
		var unmeteredLatestMs sql.NullInt64
		uErr := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(ctx, s.rebind(`
			SELECT COUNT(*), MAX(settled_at_ms)
			FROM usage_settlements
			WHERE `+strings.Join(uConds, " AND ")), uArgs...,
			).Scan(&unmeteredCnt, &unmeteredLatestMs)
		})
		if uErr != nil {
			return nil, uErr
		}
		if unmeteredCnt > 0 {
			trigMs := nowMs
			if unmeteredLatestMs.Valid {
				trigMs = unmeteredLatestMs.Int64
			}
			alerts = append(alerts, alertItemDTO{
				ID:               "alt_unmetered_" + orgID,
				AlertCode:        "ALT-UNMETERED-SPEND",
				Severity:         "TICKET",
				Summary:          i18n.AlertSummary(loc, "ALT-UNMETERED-SPEND", fmt.Sprintf("%d unmetered request(s) flagged in the past 24h", unmeteredCnt), "count", unmeteredCnt),
				RemediationRoute: "/usage",
				TriggeredAt:      formatTimeRFC3339Ms(trigMs),
			})
		}
	}

	// 3. Quarantined plugin alert (ALT-PLUGIN-TRAP) scoped to tenant/project connections & policies.
	if orgWide {
		if err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			qRows, qErr := q.QueryContext(ctx, s.rebind(`
			SELECT ei.id, ei.package_id, ei.updated_at_ms
			FROM extension_installations ei
			WHERE ei.state = 'QUARANTINED'
			  AND (
			    EXISTS (
			      SELECT 1 FROM org_extension_policies oep
			      WHERE oep.org_id = ? AND oep.package_id = ei.package_id AND oep.enabled = `+port.BoolExpr(s.dialect, true)+`
			    )
			    OR EXISTS (
			      SELECT 1 FROM provider_connections pc
			      JOIN provider_adapters pa ON pa.id = pc.adapter_id
			      WHERE pc.org_id = ? AND pa.installation_id = ei.id
			    )
			  )`), orgID, orgID,
			)
			if qErr != nil {
				return qErr
			}
			defer func() { _ = qRows.Close() }()
			for qRows.Next() {
				var id, pkgID string
				var updMs int64
				if scanErr := qRows.Scan(&id, &pkgID, &updMs); scanErr != nil {
					return scanErr
				}
				alerts = append(alerts, alertItemDTO{
					ID:               "alt_plugin_" + id,
					AlertCode:        "ALT-PLUGIN-TRAP",
					Severity:         "PAGE",
					Summary:          i18n.AlertSummary(loc, "ALT-PLUGIN-TRAP", fmt.Sprintf("Extension installation %s (%s) is QUARANTINED", id, pkgID), "id", id, "package", pkgID),
					RemediationRoute: "/extensions",
					TriggeredAt:      formatTimeRFC3339Ms(updMs),
				})
			}
			if ierr := qRows.Err(); ierr != nil {
				return ierr
			}
			return qRows.Close()
		}); err != nil {
			return nil, err
		}
	} else if len(allowedProjSet) > 0 {
		if err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			qRows, qErr := q.QueryContext(ctx, s.rebind(`
			SELECT ei.id, ei.package_id, ei.updated_at_ms, pc.owner_project_id, pc.shared_project_ids_json
			FROM extension_installations ei
			JOIN provider_adapters pa ON pa.installation_id = ei.id
			JOIN provider_connections pc ON pc.adapter_id = pa.id
			WHERE ei.state = 'QUARANTINED' AND pc.org_id = ?`), orgID,
			)
			if qErr != nil {
				return qErr
			}
			defer func() { _ = qRows.Close() }()
			seenExt := make(map[string]bool)
			for qRows.Next() {
				var id, pkgID, sharedJSON string
				var updMs int64
				var ownerProj sql.NullString
				if scanErr := qRows.Scan(&id, &pkgID, &updMs, &ownerProj, &sharedJSON); scanErr != nil {
					return scanErr
				}
				if seenExt[id] {
					continue
				}
				vis, visErr := connectionVisibleToAnyProject(ownerProj, sharedJSON, allowedProjSet)
				if visErr != nil {
					return visErr
				}
				if !vis {
					continue
				}
				seenExt[id] = true
				alerts = append(alerts, alertItemDTO{
					ID:               "alt_plugin_" + id,
					AlertCode:        "ALT-PLUGIN-TRAP",
					Severity:         "PAGE",
					Summary:          i18n.AlertSummary(loc, "ALT-PLUGIN-TRAP", fmt.Sprintf("Extension installation %s (%s) is QUARANTINED", id, pkgID), "id", id, "package", pkgID),
					RemediationRoute: "/extensions",
					TriggeredAt:      formatTimeRFC3339Ms(updMs),
				})
			}
			if ierr := qRows.Err(); ierr != nil {
				return ierr
			}
			return qRows.Close()
		}); err != nil {
			return nil, err
		}
	}

	// 4. OAuth credentials requiring reauthorization (ALT-OAUTH-EXPIRY / INC-05).
	if err := s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
		oRows, oErr := q.QueryContext(ctx, s.rebind(`
		SELECT ots.credential_id, pc.id, pc.owner_project_id, pc.shared_project_ids_json, ots.updated_at_ms
		FROM oauth_token_states ots
		JOIN connection_secret_bindings b ON b.org_id=ots.org_id AND b.credential_id=ots.credential_id AND b.slot_name='oauth_access_token'
		JOIN provider_connections pc ON pc.org_id=b.org_id AND pc.id=b.connection_id
		WHERE ots.org_id=? AND ots.state='AUTH_EXPIRED_REAUTH_REQUIRED'`), orgID)
		if oErr != nil {
			return oErr
		}
		defer func() { _ = oRows.Close() }()
		for oRows.Next() {
			var credentialID, connectionID, sharedJSON string
			var ownerProject sql.NullString
			var updated int64
			if scanErr := oRows.Scan(&credentialID, &connectionID, &ownerProject, &sharedJSON, &updated); scanErr != nil {
				return scanErr
			}
			if !orgWide {
				visible, visErr := connectionVisibleToAnyProject(ownerProject, sharedJSON, allowedProjSet)
				if visErr != nil {
					return visErr
				}
				if !visible {
					continue
				}
			}
			alerts = append(alerts, alertItemDTO{ID: "alt_oauth_" + credentialID, AlertCode: "ALT-OAUTH-EXPIRY", Severity: "PAGE", Summary: i18n.AlertSummary(loc, "ALT-OAUTH-EXPIRY", "OAuth connection requires reauthorization"), RemediationRoute: "/providers/connections", TriggeredAt: formatTimeRFC3339Ms(updated)})
		}
		if ierr := oRows.Err(); ierr != nil {
			return ierr
		}
		return oRows.Close()
	}); err != nil {
		return nil, err
	}

	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].ID < alerts[j].ID
	})
	return alerts, nil
}

func (s *Server) resolveAuditChainPartition(w http.ResponseWriter, r *http.Request, sc *authSessionContext, orgID string) (string, bool) {
	partitionFilter := strings.TrimSpace(r.URL.Query().Get("chain_partition"))
	if partitionFilter == "" || partitionFilter == orgID {
		return orgID, true
	}
	if strings.EqualFold(partitionFilter, audit.SystemChainKey) {
		if !sc.IsOperator {
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "INSTANCE_OPERATOR role required for system audit partition", false, nil)
			return "", false
		}
		return audit.SystemChainKey, true
	}
	writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "invalid chain_partition", false, nil)
	return "", false
}

func (s *Server) resolveAuditReadPartition(w http.ResponseWriter, r *http.Request, sc *authSessionContext, isCollectionList bool) (orgID string, chainKey string, ok bool) {
	partitionFilter := strings.TrimSpace(r.URL.Query().Get("chain_partition"))
	if strings.EqualFold(partitionFilter, audit.SystemChainKey) {
		if !sc.IsOperator {
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "INSTANCE_OPERATOR role required for system audit partition", false, nil)
			return "", "", false
		}
		if qOrg := strings.TrimSpace(r.URL.Query().Get("org_id")); qOrg != "" {
			orgID = qOrg
		} else {
			orgID = sc.OrgID
		}
		if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID, IsCollectionList: isCollectionList}) {
			return "", "", false
		}
		return orgID, audit.SystemChainKey, true
	}
	orgID, ok = s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return "", "", false
	}
	if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID, IsCollectionList: isCollectionList}) {
		return "", "", false
	}
	chainKey, ok = s.resolveAuditChainPartition(w, r, sc, orgID)
	if !ok {
		return "", "", false
	}
	return orgID, chainKey, true
}

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, chainKey, ok := s.resolveAuditReadPartition(w, r, sc, true)
	if !ok {
		return
	}
	cursorScope := orgID
	if cursorScope == "" {
		cursorScope = audit.SystemChainKey
	}
	actorFilter := strings.TrimSpace(r.URL.Query().Get("actor"))
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))

	filterHash := sha256HexString("audit_events|" + chainKey + "|" + actorFilter + "|" + actionFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "audit_events", cursorScope, filterHash)
	if !ok {
		return
	}

	var conds []string
	var args []any
	conds = append(conds, `chain_key = ?`)
	args = append(args, chainKey)
	if actorFilter != "" {
		conds = append(conds, `actor_id = ?`)
		args = append(args, actorFilter)
	}
	if actionFilter != "" {
		conds = append(conds, `event_type = ?`)
		args = append(args, actionFilter)
	}
	if cur != nil {
		conds = append(conds, `(seq_no < ? OR (seq_no = ? AND id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}

	query := `
		SELECT id, org_id, chain_key, seq_no, actor_id, event_type,
		       target_type, target_id, metadata_json, prev_hash, entry_hash, created_at_ms
		FROM audit_events
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY seq_no DESC, id DESC
		LIMIT ?`
	args = append(args, limit+1)

	var items []auditEventResourceDTO
	queryFn := func(q adminDBQuerier) error {
		rows, qerr := q.QueryContext(r.Context(), s.rebind(query), args...)
		if qerr != nil {
			return qerr
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				id, part, actorID, action, targetType, targetID, metaJSON, prevHash, entryHash string
				evOrg                                                                          sql.NullString
				seqNo, createdMs                                                               int64
			)
			if scanErr := rows.Scan(&id, &evOrg, &part, &seqNo, &actorID, &action, &targetType, &targetID, &metaJSON, &prevHash, &entryHash, &createdMs); scanErr != nil {
				return scanErr
			}
			var metaMap map[string]any
			if strings.TrimSpace(metaJSON) != "" {
				_ = json.Unmarshal([]byte(metaJSON), &metaMap)
			}
			if metaMap == nil {
				metaMap = map[string]any{}
			}
			items = append(items, auditEventResourceDTO{
				ID:             id,
				OrgID:          sqlNullStrPtr(evOrg),
				ChainPartition: part,
				SeqNo:          seqNo,
				ActorID:        actorID,
				Action:         action,
				TargetType:     targetType,
				TargetID:       targetID,
				Metadata:       metaMap,
				PrevHash:       prevHash,
				EventHash:      entryHash,
				CreatedAt:      formatTimeRFC3339Ms(createdMs),
			})
		}
		if ierr := rows.Err(); ierr != nil {
			return ierr
		}
		return rows.Close()
	}
	var err error
	if chainKey == audit.SystemChainKey || orgID == "" {
		err = s.withOperatorReadTx(r.Context(), queryFn)
	} else {
		err = s.withOrgReadTx(r.Context(), orgID, queryFn)
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying audit events", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := items
	if data == nil {
		data = []auditEventResourceDTO{}
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "audit_events", cursorScope, filterHash, last.SeqNo, last.ID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[auditEventResourceDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleVerifyAuditHashChain(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	_, chainKey, ok := s.resolveAuditReadPartition(w, r, sc, false)
	if !ok {
		return
	}

	keys := []string{chainKey}
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed starting audit verify tx", true, nil)
		return
	}
	defer func() { _ = tx.Rollback() }()

	report, err := audit.VerifyAuditChainTx(r.Context(), tx, s.dialect, audit.VerifyOptions{ChainKeys: keys})
	if err != nil && !errors.Is(err, audit.ErrAuditTampered) {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying audit hash chain", true, nil)
		return
	}

	var totalCount int
	_ = tx.QueryRowContext(r.Context(), s.rebind(`SELECT COUNT(*) FROM audit_events WHERE chain_key = ?`), chainKey).Scan(&totalCount)
	_ = tx.Commit()

	if report == nil {
		writeJSON(w, http.StatusOK, verifyAuditResponseDTO{
			Intact:             true,
			CheckedEventsCount: totalCount,
		})
		return
	}
	if totalCount < report.TotalVerified {
		totalCount = report.TotalVerified
	}
	var brokenSeq *int64
	if !report.Valid && len(report.Findings) > 0 {
		seq := report.Findings[0].SeqNo
		brokenSeq = &seq
	}

	writeJSON(w, http.StatusOK, verifyAuditResponseDTO{
		Intact:             report.Valid,
		CheckedEventsCount: totalCount,
		FirstBrokenSeqNo:   brokenSeq,
	})
}

func (s *Server) handleTriggerKillSwitch(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var req killSwitchTriggerRequestDTO
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	targetID := strings.TrimSpace(req.TargetID)
	reason := strings.TrimSpace(req.Reason)
	if targetID == "" || len([]rune(reason)) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_id and reason (>= 5 characters) are required", false, nil)
		return
	}

	level, okLevel := mapKillSwitchLevel(req.TargetLevel)
	if !okLevel {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_level must be VIRTUAL_KEY, PROJECT, PROVIDER_CONNECTION, or EXTENSION", false, nil)
		return
	}

	qOrg := strings.TrimSpace(r.URL.Query().Get("org_id"))
	var orgID string
	if level == killswitch.LevelExtensionInstallation && sc.IsOperator && qOrg == "" && sc.OrgID == "" {
		orgID = ""
	} else {
		orgID, ok = s.resolveTargetOrgID(w, r, sc)
		if !ok {
			return
		}
	}

	if rerr := rbac.HasAnyPermissionInOrg(sc.Actor, orgID, rbac.ActionKillSwitchTrigger); rerr != nil {
		s.writeAuthError(w, r, rerr)
		return
	}

	// Cross-tenant & scope lookup (AUTH-15). For EXTENSION, authorize + check multi-tenant OPERATOR BEFORE lookup to prevent global ID enumeration.
	var scopeProj string
	var expectedConfirmSlug string
	switch level {
	case killswitch.LevelVirtualKey:
		var rowOrg, pid string
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT org_id, project_id FROM virtual_keys WHERE id = ?`), targetID).Scan(&rowOrg, &pid)
		})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking virtual key", true, nil)
			return
		}
		scopeProj = pid
	case killswitch.LevelProviderConnection:
		var rowOrg string
		var ownerProj sql.NullString
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT org_id, owner_project_id FROM provider_connections WHERE id = ?`), targetID).Scan(&rowOrg, &ownerProj)
		})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking provider connection", true, nil)
			return
		}
		if ownerProj.Valid {
			scopeProj = ownerProj.String
		}
	case killswitch.LevelProjectOrOrgFreeze:
		if targetID != orgID {
			var rowOrg, slug string
			err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
				return q.QueryRowContext(r.Context(), s.rebind(`SELECT org_id, slug FROM projects WHERE id = ?`), targetID).Scan(&rowOrg, &slug)
			})
			if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
				return
			}
			if err != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking project", true, nil)
				return
			}
			expectedConfirmSlug = slug
			scopeProj = targetID
		} else {
			var orgSlug string
			err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
				return q.QueryRowContext(r.Context(), s.rebind(`SELECT slug FROM organizations WHERE id = ? AND status NOT IN ('DELETING', 'PENDING_ERASURE')`), orgID).Scan(&orgSlug)
			})
			if errors.Is(err, sql.ErrNoRows) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "organization not found", false, nil)
				return
			}
			if err != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking organization", true, nil)
				return
			}
			expectedConfirmSlug = orgSlug
		}
	case killswitch.LevelExtensionInstallation:
		if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{OrgID: orgID}) {
			return
		}
		if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
			return
		}
		var exists int
		err := s.db.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM extension_installations WHERE id = ?`), targetID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking extension installation", true, nil)
			return
		}
	}

	if level != killswitch.LevelExtensionInstallation {
		if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{OrgID: orgID, ProjectID: scopeProj}) {
			return
		}
	}
	if level == killswitch.LevelProjectOrOrgFreeze && strings.TrimSpace(req.ConfirmSlug) != expectedConfirmSlug {
		msg := "confirm_slug must match target project slug"
		if targetID == orgID {
			msg = "confirm_slug must match target organization slug"
		}
		writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", msg, false, nil)
		return
	}
	if s.killSwitch == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_unavailable", "upstream_error", "kill-switch registry is not configured", true, nil)
		return
	}

	abortStreams := true
	if req.AbortActiveStreams != nil {
		abortStreams = *req.AbortActiveStreams
	}

	var ksOrg *string
	if orgID != "" {
		ksOrg = &orgID
	}

	_, severedCount, err := s.killSwitch.TriggerWithSeveredCount(r.Context(), killswitch.TriggerParams{
		OrgID:                  ksOrg,
		ProjectID:              scopeProj,
		Level:                  level,
		TargetID:               targetID,
		Reason:                 reason,
		TriggeredByPrincipalID: sc.Actor.PrincipalID,
		EntryPoint:             port.AuditEntryPointAdminAPI,
		RequestID:              ptrOrNil(requestIDFromContext(r.Context())),
		AbortActiveStreams:     &abortStreams,
	})
	if err != nil {
		switch {
		case errors.Is(err, killswitch.ErrPermissionDenied):
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", err.Error(), false, nil)
		case errors.Is(err, killswitch.ErrAlreadyActive):
			writeErrorEnvelope(w, r, http.StatusConflict, "kill_switch_already_active", "conflict_error", err.Error(), false, nil)
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "kill-switch target not found", false, nil)
		case errors.Is(err, port.ErrInvalidInput):
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed triggering kill-switch", true, nil)
		}
		return
	}
	configEpoch := int64(0)
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
		if curSnap := s.snapshots.Current(); curSnap != nil {
			configEpoch = curSnap.Epoch
		}
	}
	if configEpoch == 0 {
		qFn := func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(config_epoch), 1) FROM config_changelog`).Scan(&configEpoch)
		}
		if orgID == "" || ksOrg == nil {
			_ = s.withOperatorReadTx(r.Context(), qFn)
		} else {
			_ = s.withOrgReadTx(r.Context(), orgID, qFn)
		}
	}

	affectedCount := s.countKillSwitchAffectedEntities(r.Context(), orgID, ksOrg == nil, level, targetID)
	writeJSON(w, http.StatusOK, killSwitchTriggerResponseDTO{
		ConfigEpoch:           configEpoch,
		SeveredStreamsCount:   severedCount,
		AffectedEntitiesCount: affectedCount,
	})
}

func (s *Server) handleListKillSwitches(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	activeOnly := true
	if raw := strings.TrimSpace(r.URL.Query().Get("active_only")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "active_only must be true or false", false, nil)
			return
		}
		activeOnly = parsed
	}
	qOrg := strings.TrimSpace(r.URL.Query().Get("org_id"))
	instanceOnly := sc.IsOperator && qOrg == "" && sc.OrgID == ""
	if instanceOnly {
		if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{}) {
			return
		}
		if s.killSwitch == nil {
			writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_unavailable", "upstream_error", "kill-switch registry is not configured", true, nil)
			return
		}
		switches, err := s.killSwitch.ListInstance(r.Context(), activeOnly)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing instance kill switches", true, nil)
			return
		}
		if switches == nil {
			switches = []killswitch.KillSwitch{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": switches})
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}
	if s.killSwitch == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_unavailable", "upstream_error", "kill-switch registry is not configured", true, nil)
		return
	}
	switches, err := s.killSwitch.ListWithFilter(r.Context(), killswitch.ListFilter{OrgID: orgID, AllowInstanceScope: sc.IsOperator, ActiveOnly: activeOnly})
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing kill switches", true, nil)
		return
	}
	if switches == nil {
		switches = []killswitch.KillSwitch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": switches})
}

func (s *Server) handleClearKillSwitch(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var req clearKillSwitchRequestDTO
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if req.ExpectedVersion < 1 || len([]rune(strings.TrimSpace(req.ClearReason))) < 5 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "expected_version >= 1 and clear_reason (>= 5 characters) are required", false, nil)
		return
	}
	// Serialize clear with the quarantine transaction so an operator cannot
	// remove the abort barrier while the installation is still serving-active.
	s.quarantineMu.Lock()
	defer s.quarantineMu.Unlock()
	if s.killSwitch == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "kill_switch_unavailable", "upstream_error", "kill-switch registry is not configured", true, nil)
		return
	}
	id := strings.TrimSpace(r.PathValue("switchId"))
	qOrg := strings.TrimSpace(r.URL.Query().Get("org_id"))
	instanceOnly := sc.IsOperator && qOrg == "" && sc.OrgID == ""
	var orgID string
	var ks *killswitch.KillSwitch
	var err error
	if instanceOnly {
		if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{}) {
			return
		}
		ks, err = s.killSwitch.GetInstance(r.Context(), id)
	} else {
		orgID, ok = s.resolveTargetOrgID(w, r, sc)
		if !ok {
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{OrgID: orgID}) {
			return
		}
		ks, err = s.killSwitch.Get(r.Context(), orgID, id)
		if errors.Is(err, port.ErrNotFound) && sc.IsOperator && qOrg == "" {
			if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{}) {
				return
			}
			instanceOnly = true
			ks, err = s.killSwitch.GetInstance(r.Context(), id)
		}
	}
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "kill switch not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading kill switch", true, nil)
		return
	}
	if ks.Version != req.ExpectedVersion {
		writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "kill switch version changed; reload before clearing", false, map[string]any{"current_version": ks.Version})
		return
	}
	if ks.Level == killswitch.LevelExtensionInstallation {
		installation, _, loadErr := s.loadExtensionInstallationDTO(r.Context(), ks.TargetID)
		if errors.Is(loadErr, port.ErrNotFound) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
			return
		}
		if loadErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading extension installation before clearing quarantine barrier", true, nil)
			return
		}
		if installation.State == installer.StateQuarantined && s.snapshots != nil {
			if err := s.snapshots.Refresh(r.Context()); err != nil {
				writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "quarantine_snapshot_refresh_failed", "upstream_error", "serving snapshot could not confirm quarantine; emergency barrier remains active", true, nil)
				return
			}
		}
	}
	var orgPtr *string
	if !instanceOnly {
		orgPtr = &orgID
	}
	cleared, err := s.killSwitch.Clear(r.Context(), killswitch.ClearParams{
		OrgID: orgPtr, KillSwitchID: id, ExpectedVersion: req.ExpectedVersion,
		ClearedByPrincipalID: sc.Actor.PrincipalID, ClearReason: req.ClearReason,
		EntryPoint: port.AuditEntryPointAdminAPI, RequestID: ptrOrNil(requestIDFromContext(r.Context())),
	})
	if err != nil {
		switch {
		case errors.Is(err, killswitch.ErrPermissionDenied), errors.Is(err, rbac.ErrPermissionDenied):
			writeErrorEnvelope(w, r, http.StatusForbidden, "insufficient_role_permission", "permission_error", "insufficient role permission", false, nil)
		case errors.Is(err, port.ErrNotFound):
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "kill switch not found", false, nil)
		case errors.Is(err, port.ErrVersionConflict):
			writeErrorEnvelope(w, r, http.StatusConflict, "optimistic_lock_conflict", "conflict_error", "kill switch version changed; reload before clearing", false, nil)
		case errors.Is(err, killswitch.ErrAlreadyCleared):
			writeErrorEnvelope(w, r, http.StatusConflict, "kill_switch_already_cleared", "conflict_error", err.Error(), false, nil)
		default:
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed clearing kill switch", true, nil)
		}
		return
	}
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
	}
	writeJSON(w, http.StatusOK, cleared)
}

func (s *Server) handleQueryBlastRadius(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	tType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("target_type")))
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	if targetID == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_type and target_id are required", false, nil)
		return
	}
	if tType != "EXTENSION" && tType != "PROVIDER_CONNECTION" && tType != "PROJECT" && tType != "VIRTUAL_KEY" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_type must be EXTENSION, PROVIDER_CONNECTION, PROJECT, or VIRTUAL_KEY", false, nil)
		return
	}

	qOrg := strings.TrimSpace(r.URL.Query().Get("org_id"))
	allowInstanceScope := tType == "EXTENSION" && sc.IsOperator && sc.OrgID == "" && qOrg == ""
	var orgID string
	if allowInstanceScope {
		orgID = ""
	} else {
		orgID, ok = s.resolveTargetOrgID(w, r, sc)
		if !ok {
			return
		}
	}
	if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID}) {
		return
	}
	var targetProjectID string
	switch tType {
	case "EXTENSION":
		if !s.ensureGlobalExtensionOperatorIfMultiTenant(w, r, sc) {
			return
		}
	case "PROVIDER_CONNECTION":
		var rowOrg string
		var ownerProj sql.NullString
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT org_id, owner_project_id FROM provider_connections WHERE id = ? AND org_id = ?`), targetID, orgID).Scan(&rowOrg, &ownerProj)
		})
		if errors.Is(err, sql.ErrNoRows) {
			var traceProjID string
			traceErr := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
				return q.QueryRowContext(r.Context(), s.rebind(`SELECT project_id FROM request_traces_meta WHERE org_id = ? AND connection_id = ? LIMIT 1`), orgID, targetID).Scan(&traceProjID)
			})
			if errors.Is(traceErr, sql.ErrNoRows) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
				return
			}
			if traceErr != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating provider connection", true, nil)
				return
			}
			targetProjectID = traceProjID
		} else if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating provider connection", true, nil)
			return
		} else if ownerProj.Valid && ownerProj.String != "" {
			targetProjectID = ownerProj.String
		}
		if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID, ProjectID: targetProjectID}) {
			return
		}
	case "PROJECT":
		var rowOrg string
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT org_id FROM projects WHERE id = ? AND status NOT IN ('DELETING', 'PENDING_ERASURE')`), targetID).Scan(&rowOrg)
		})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating project", true, nil)
			return
		}
		targetProjectID = targetID
		if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID, ProjectID: targetProjectID}) {
			return
		}
	case "VIRTUAL_KEY":
		var rowOrg string
		err := s.withOrgReadTx(r.Context(), orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(r.Context(), s.rebind(`SELECT org_id, project_id FROM virtual_keys WHERE id = ?`), targetID).Scan(&rowOrg, &targetProjectID)
		})
		if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating virtual key", true, nil)
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID, ProjectID: targetProjectID}) {
			return
		}
	}

	sinceStr := strings.TrimSpace(r.URL.Query().Get("since"))
	if sinceStr == "" {
		sinceStr = "168h"
	}
	dur, err := time.ParseDuration(sinceStr)
	if err != nil || dur <= 0 {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "since must be a valid positive Go duration (e.g. 24h, 168h)", false, nil)
		return
	}

	nowMs := s.nowMs()
	q := audit.BlastRadiusQuery{
		OrgID:              orgID,
		AllowInstanceScope: allowInstanceScope,
		ProjectID:          targetProjectID,
		FromMs:             nowMs - dur.Milliseconds(),
		ToMs:               nowMs,
	}
	switch tType {
	case "EXTENSION":
		q.ExtensionInstallationID = targetID
	case "PROVIDER_CONNECTION":
		q.ConnectionID = targetID
	case "PROJECT":
		q.ProjectID = targetID
	case "VIRTUAL_KEY":
		q.VirtualKeyID = targetID
	}

	report, err := audit.QueryBlastRadius(r.Context(), s.db, q)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying blast radius", true, nil)
		return
	}

	connSet := make(map[string]struct{})
	for _, tr := range report.Traces {
		if tr.ConnectionID != nil && *tr.ConnectionID != "" {
			connSet[*tr.ConnectionID] = struct{}{}
		}
	}
	if tType == "PROVIDER_CONNECTION" && report.TotalTraceCount > 0 {
		connSet[targetID] = struct{}{}
	}
	connections := make([]string, 0, len(connSet))
	for cid := range connSet {
		connections = append(connections, cid)
	}
	sort.Strings(connections)

	orgs := report.AffectedOrgs
	if orgs == nil {
		orgs = []string{}
	}
	projs := report.AffectedProjects
	if projs == nil {
		projs = []string{}
	}
	if tType == "PROJECT" {
		projs = appendUniqueString(projs, targetID)
		sort.Strings(projs)
	}
	vks := report.AffectedVirtualKeys
	if vks == nil {
		vks = []string{}
	}
	if tType == "VIRTUAL_KEY" {
		vks = appendUniqueString(vks, targetID)
		sort.Strings(vks)
	}

	writeJSON(w, http.StatusOK, blastRadiusResponseDTO{
		Organizations: orgs,
		Projects:      projs,
		VirtualKeys:   vks,
		Connections:   connections,
	})
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func mapKillSwitchLevel(raw string) (killswitch.Level, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "VIRTUAL_KEY":
		return killswitch.LevelVirtualKey, true
	case "PROJECT", "PROJECT_OR_ORG_FREEZE":
		return killswitch.LevelProjectOrOrgFreeze, true
	case "PROVIDER_CONNECTION":
		return killswitch.LevelProviderConnection, true
	case "EXTENSION", "EXTENSION_INSTALLATION":
		return killswitch.LevelExtensionInstallation, true
	default:
		return "", false
	}
}

func (s *Server) countKillSwitchAffectedEntities(ctx context.Context, orgID string, instanceWide bool, level killswitch.Level, targetID string) int {
	var cnt int
	switch level {
	case killswitch.LevelVirtualKey:
		return 1
	case killswitch.LevelProviderConnection:
		_ = s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM model_deployments WHERE org_id = ? AND connection_id = ?`), orgID, targetID).Scan(&cnt)
		})
		if cnt < 1 {
			return 1
		}
		return cnt
	case killswitch.LevelExtensionInstallation:
		if instanceWide {
			_ = s.withOperatorReadTx(ctx, func(q adminDBQuerier) error {
				return q.QueryRowContext(ctx, s.rebind(`
				SELECT COUNT(*)
				FROM provider_connections pc
				JOIN provider_adapters pa ON pa.id = pc.adapter_id
				WHERE pa.installation_id = ?`), targetID).Scan(&cnt)
			})
		} else {
			_ = s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
				return q.QueryRowContext(ctx, s.rebind(`
				SELECT COUNT(*)
				FROM provider_connections pc
				JOIN provider_adapters pa ON pa.id = pc.adapter_id
				WHERE pa.installation_id = ? AND pc.org_id = ?`), targetID, orgID).Scan(&cnt)
			})
		}
		if cnt < 1 {
			return 1
		}
		return cnt
	case killswitch.LevelProjectOrOrgFreeze:
		_ = s.withOrgReadTx(ctx, orgID, func(q adminDBQuerier) error {
			if targetID == orgID {
				return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM virtual_keys WHERE org_id = ? AND state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID')`), orgID).Scan(&cnt)
			}
			return q.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM virtual_keys WHERE org_id = ? AND project_id = ? AND state IN ('CREATED_ACTIVE', 'ROTATING_DUAL_VALID')`), orgID, targetID).Scan(&cnt)
		})
		if cnt < 1 {
			return 1
		}
		return cnt
	default:
		return 1
	}
}
