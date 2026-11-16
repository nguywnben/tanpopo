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
	"strings"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
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

	traceRows, err := s.db.QueryContext(r.Context(), `
		SELECT http_status, proxy_overhead_us
		FROM request_traces_meta
		WHERE `+strings.Join(traceConds, " AND "), traceArgs...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying overview trace metrics", true, nil)
		return
	}
	defer traceRows.Close()

	var totalReqs, errReqs int64
	var overheads []int64
	for traceRows.Next() {
		var status int
		var overUs int64
		if scanErr := traceRows.Scan(&status, &overUs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning trace metrics", true, nil)
			return
		}
		totalReqs++
		if status >= 400 {
			errReqs++
		}
		overheads = append(overheads, overUs)
	}
	if err := traceRows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating trace metrics", true, nil)
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
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT COALESCE(SUM(total_cost_micros), 0),
		       COALESCE(SUM(CASE WHEN usage_accuracy = 'UNMETERED_FLAGGED' THEN 1 ELSE 0 END), 0)
		FROM usage_settlements
		WHERE `+strings.Join(settleConds, " AND "), settleArgs...,
	).Scan(&settledMicros, &unmeteredCount); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying overview financial metrics", true, nil)
		return
	}

	var activeHolds int64
	holdRows, err := s.db.QueryContext(r.Context(), `SELECT scope_type, scope_id, reserved_hold_micros FROM budget_ledgers WHERE org_id = ?`, orgID)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying active holds", true, nil)
		return
	}
	defer holdRows.Close()
	allowedProjSet := make(map[string]bool, len(projIDs))
	for _, pid := range projIDs {
		allowedProjSet[pid] = true
	}
	for holdRows.Next() {
		var st, sid string
		var hold int64
		if scanErr := holdRows.Scan(&st, &sid, &hold); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning active holds", true, nil)
			return
		}
		if !orgWide {
			resolvedProj := s.resolveBudgetScopeProjectID(r.Context(), orgID, st, sid)
			if resolvedProj == "" || !allowedProjSet[resolvedProj] {
				continue
			}
		}
		activeHolds += hold
	}
	if err := holdRows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating active holds", true, nil)
		return
	}

	alerts, _ := s.collectActiveAlerts(r.Context(), orgID, nowMs)
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

	filterHash := sha256HexString("alerts")
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "alerts", orgID, filterHash)
	if !ok {
		return
	}

	alerts, err := s.collectActiveAlerts(r.Context(), orgID, s.nowMs())
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

	writeJSON(w, http.StatusOK, paginatedListEnvelope[alertItemDTO]{
		Data:       page,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) collectActiveAlerts(ctx context.Context, orgID string, nowMs int64) ([]alertItemDTO, error) {
	var alerts []alertItemDTO

	// 1. Budget >= 80% alerts (ALT-BUDGET-80)
	bRows, err := s.db.QueryContext(ctx, `
		SELECT id, scope_type, hard_limit_micros,
		       settled_exact_micros + settled_estimated_micros + reserved_hold_micros,
		       updated_at_ms
		FROM budget_ledgers
		WHERE org_id = ? AND hard_limit_micros > 0`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer bRows.Close()
	for bRows.Next() {
		var id, scope string
		var limitM, usedM, updMs int64
		if scanErr := bRows.Scan(&id, &scope, &limitM, &usedM, &updMs); scanErr != nil {
			return nil, scanErr
		}
		if limitM > 0 && (usedM*100)/limitM >= 80 {
			alerts = append(alerts, alertItemDTO{
				ID:               "alt_budget_" + id,
				AlertCode:        "ALT-BUDGET-80",
				Severity:         "TICKET",
				Summary:          fmt.Sprintf("%s budget %s exceeded 80%% utilization", scope, id),
				RemediationRoute: "/budgets",
				TriggeredAt:      formatTimeRFC3339Ms(updMs),
			})
		}
	}

	// 2. Unmetered spend alert (ALT-UNMETERED-SPEND)
	var unmeteredCnt int64
	var unmeteredLatestMs sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*), MAX(settled_at_ms)
		FROM usage_settlements
		WHERE org_id = ? AND usage_accuracy = 'UNMETERED_FLAGGED' AND settled_at_ms >= ?`,
		orgID, nowMs-(24*time.Hour).Milliseconds(),
	).Scan(&unmeteredCnt, &unmeteredLatestMs); err == nil && unmeteredCnt > 0 {
		trigMs := nowMs
		if unmeteredLatestMs.Valid {
			trigMs = unmeteredLatestMs.Int64
		}
		alerts = append(alerts, alertItemDTO{
			ID:               "alt_unmetered_" + orgID,
			AlertCode:        "ALT-UNMETERED-SPEND",
			Severity:         "TICKET",
			Summary:          fmt.Sprintf("%d unmetered request(s) flagged in the past 24h", unmeteredCnt),
			RemediationRoute: "/usage",
			TriggeredAt:      formatTimeRFC3339Ms(trigMs),
		})
	}

	// 3. Quarantined plugin alert (ALT-PLUGIN-TRAP)
	qRows, err := s.db.QueryContext(ctx, `
		SELECT id, package_id, updated_at_ms
		FROM extension_installations
		WHERE state = 'QUARANTINED'`,
	)
	if err == nil {
		defer qRows.Close()
		for qRows.Next() {
			var id, pkgID string
			var updMs int64
			if qRows.Scan(&id, &pkgID, &updMs) == nil {
				alerts = append(alerts, alertItemDTO{
					ID:               "alt_plugin_" + id,
					AlertCode:        "ALT-PLUGIN-TRAP",
					Severity:         "PAGE",
					Summary:          fmt.Sprintf("Extension installation %s (%s) is QUARANTINED", id, pkgID),
					RemediationRoute: "/extensions",
					TriggeredAt:      formatTimeRFC3339Ms(updMs),
				})
			}
		}
	}

	sort.Slice(alerts, func(i, j int) bool {
		return alerts[i].ID < alerts[j].ID
	})
	return alerts, nil
}

func (s *Server) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}

	actorFilter := strings.TrimSpace(r.URL.Query().Get("actor"))
	actionFilter := strings.TrimSpace(r.URL.Query().Get("action"))
	partitionFilter := strings.TrimSpace(r.URL.Query().Get("chain_partition"))
	chainKey := orgID
	if sc.IsOperator && strings.EqualFold(partitionFilter, audit.SystemChainKey) {
		chainKey = audit.SystemChainKey
	}

	filterHash := sha256HexString("audit_events|" + chainKey + "|" + actorFilter + "|" + actionFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "audit_events", orgID, filterHash)
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
		conds = append(conds, `(created_at_ms < ? OR (created_at_ms = ? AND id < ?))`)
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

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying audit events", true, nil)
		return
	}
	defer rows.Close()

	type audRow struct {
		dto       auditEventResourceDTO
		createdMs int64
	}
	var items []audRow
	for rows.Next() {
		var (
			id, part, actorID, action, targetType, targetID, metaJSON, prevHash, entryHash string
			evOrg                                                                          sql.NullString
			seqNo, createdMs                                                               int64
		)
		if scanErr := rows.Scan(&id, &evOrg, &part, &seqNo, &actorID, &action, &targetType, &targetID, &metaJSON, &prevHash, &entryHash, &createdMs); scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning audit event", true, nil)
			return
		}
		var metaMap map[string]any
		if strings.TrimSpace(metaJSON) != "" {
			_ = json.Unmarshal([]byte(metaJSON), &metaMap)
		}
		if metaMap == nil {
			metaMap = map[string]any{}
		}
		items = append(items, audRow{
			dto: auditEventResourceDTO{
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
			},
			createdMs: createdMs,
		})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating audit events", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]auditEventResourceDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "audit_events", orgID, filterHash, last.createdMs, last.dto.ID)
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
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID}) {
		return
	}

	keys := []string{orgID}
	report, err := audit.VerifyAuditChain(r.Context(), s.db, audit.VerifyOptions{ChainKeys: keys})
	if err != nil && !errors.Is(err, audit.ErrAuditTampered) {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed verifying audit hash chain", true, nil)
		return
	}

	var totalCount int
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM audit_events WHERE chain_key = ?`, orgID).Scan(&totalCount)

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
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
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

	// Cross-tenant & scope lookup BEFORE authorizing / triggering (AUTH-15).
	var scopeProj string
	switch level {
	case killswitch.LevelVirtualKey:
		var rowOrg, pid string
		err := s.db.QueryRowContext(r.Context(), `SELECT org_id, project_id FROM virtual_keys WHERE id = ?`, targetID).Scan(&rowOrg, &pid)
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
		err := s.db.QueryRowContext(r.Context(), `SELECT org_id FROM provider_connections WHERE id = ?`, targetID).Scan(&rowOrg)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "provider connection not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking provider connection", true, nil)
			return
		}
	case killswitch.LevelProjectOrOrgFreeze:
		if targetID != orgID {
			var rowOrg, slug string
			err := s.db.QueryRowContext(r.Context(), `SELECT org_id, slug FROM projects WHERE id = ?`, targetID).Scan(&rowOrg, &slug)
			if errors.Is(err, sql.ErrNoRows) || (err == nil && rowOrg != orgID) {
				writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "project not found", false, nil)
				return
			}
			if err != nil {
				writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking project", true, nil)
				return
			}
			if cSlug := strings.TrimSpace(req.ConfirmSlug); cSlug != "" && cSlug != slug {
				writeErrorEnvelope(w, r, http.StatusBadRequest, "confirm_slug_mismatch", "invalid_request_error", "confirm_slug does not match project slug", false, nil)
				return
			}
			scopeProj = targetID
		}
	case killswitch.LevelExtensionInstallation:
		var exists int
		err := s.db.QueryRowContext(r.Context(), `SELECT 1 FROM extension_installations WHERE id = ?`, targetID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "extension installation not found", false, nil)
			return
		}
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed checking extension installation", true, nil)
			return
		}
	}

	if !s.authorize(w, r, sc, rbac.ActionKillSwitchTrigger, rbac.TargetScope{OrgID: orgID, ProjectID: scopeProj}) {
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
	beforeStreams := 0
	if abortStreams && s.killSwitch.Streams() != nil {
		beforeStreams = s.killSwitch.Streams().ActiveCount()
	}

	ksOrg := &orgID
	if level == killswitch.LevelExtensionInstallation && sc.IsOperator && !actorHasOrgScopeBinding(sc.Actor, orgID) {
		ksOrg = nil
	}
	_, err := s.killSwitch.Trigger(r.Context(), killswitch.TriggerParams{
		OrgID:                  ksOrg,
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

	severedCount := 0
	if abortStreams && s.killSwitch.Streams() != nil {
		afterStreams := s.killSwitch.Streams().ActiveCount()
		if beforeStreams > afterStreams {
			severedCount = beforeStreams - afterStreams
		}
	}

	configEpoch := int64(0)
	if s.snapshots != nil {
		_ = s.snapshots.Refresh(r.Context())
		if curSnap := s.snapshots.Current(); curSnap != nil {
			configEpoch = curSnap.Epoch
		}
	}
	if configEpoch == 0 {
		_ = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(seq_no), 1) FROM config_changelog`).Scan(&configEpoch)
	}

	affectedCount := s.countKillSwitchAffectedEntities(r.Context(), orgID, level, targetID)
	writeJSON(w, http.StatusOK, killSwitchTriggerResponseDTO{
		ConfigEpoch:           configEpoch,
		SeveredStreamsCount:   severedCount,
		AffectedEntitiesCount: affectedCount,
	})
}

func (s *Server) handleQueryBlastRadius(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionAuditReadVerify, rbac.TargetScope{OrgID: orgID}) {
		return
	}

	tType := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("target_type")))
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	if targetID == "" {
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_type and target_id are required", false, nil)
		return
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
		AllowInstanceScope: sc.IsOperator && strings.TrimSpace(r.URL.Query().Get("org_id")) == "",
		FromMs:             nowMs - dur.Milliseconds(),
		ToMs:               nowMs,
	}
	switch tType {
	case "EXTENSION":
		q.ExtensionInstallationID = targetID
	case "PROVIDER_CONNECTION":
		q.ConnectionID = targetID
	default:
		writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "target_type must be EXTENSION or PROVIDER_CONNECTION", false, nil)
		return
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
	vks := report.AffectedVirtualKeys
	if vks == nil {
		vks = []string{}
	}

	writeJSON(w, http.StatusOK, blastRadiusResponseDTO{
		Organizations: orgs,
		Projects:      projs,
		VirtualKeys:   vks,
		Connections:   connections,
	})
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

func (s *Server) countKillSwitchAffectedEntities(ctx context.Context, orgID string, level killswitch.Level, targetID string) int {
	var cnt int
	switch level {
	case killswitch.LevelVirtualKey:
		return 1
	case killswitch.LevelProviderConnection:
		_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_deployments WHERE org_id = ? AND connection_id = ?`, orgID, targetID).Scan(&cnt)
		if cnt < 1 {
			return 1
		}
		return cnt
	case killswitch.LevelExtensionInstallation:
		_ = s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM provider_connections pc
			JOIN extension_versions ev ON ev.id = pc.adapter_id OR ev.package_id = pc.adapter_id
			JOIN extension_installations ei ON ei.active_version_id = ev.id OR ei.package_id = ev.package_id
			WHERE ei.id = ?`, targetID).Scan(&cnt)
		if cnt < 1 {
			return 1
		}
		return cnt
	case killswitch.LevelProjectOrOrgFreeze:
		if targetID == orgID {
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM virtual_keys WHERE org_id = ? AND state = 'ACTIVE'`, orgID).Scan(&cnt)
		} else {
			_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM virtual_keys WHERE org_id = ? AND project_id = ? AND state = 'ACTIVE'`, orgID, targetID).Scan(&cnt)
		}
		if cnt < 1 {
			return 1
		}
		return cnt
	default:
		return 1
	}
}
