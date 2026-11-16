// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

type traceWaterfallStageDTO struct {
	Stage      string  `json:"stage"`
	DurationMs float64 `json:"duration_ms"`
	Outcome    string  `json:"outcome"`
}

type traceSummaryDTO struct {
	RequestID       string                   `json:"request_id"`
	Plane           string                   `json:"plane"`
	EnvID           string                   `json:"env_id,omitempty"`
	VirtualKeyID    string                   `json:"virtual_key_id,omitempty"`
	RouteSlug       string                   `json:"route_slug"`
	DeploymentID    *string                  `json:"deployment_id"`
	DataRegion      *string                  `json:"data_region"`
	StatusCode      int                      `json:"status_code"`
	ErrorCode       *string                  `json:"error_code"`
	UsageAccuracy   string                   `json:"usage_accuracy"`
	TotalCostMicros int64                    `json:"total_cost_micros"`
	TTFTMs          *float64                 `json:"ttft_ms"`
	DurationMs      float64                  `json:"duration_ms"`
	HasDebugPayload bool                     `json:"has_debug_payload"`
	WaterfallStages []traceWaterfallStageDTO `json:"waterfall_stages"`
	CreatedAt       string                   `json:"created_at"`
}

type debugPayloadResponseDTO struct {
	RequestID string `json:"request_id"`
	ExpiresAt string `json:"expires_at"`
	Payload   any    `json:"payload"`
}

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	envFilter := strings.TrimSpace(r.URL.Query().Get("env_id"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	modelFilter := strings.TrimSpace(r.URL.Query().Get("model"))

	if envFilter != "" {
		projID, found, err := s.lookupEnvironmentProject(r.Context(), orgID, envFilter)
		if err != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed validating environment", true, nil)
			return
		}
		if !found {
			writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "environment not found", false, nil)
			return
		}
		if !s.authorize(w, r, sc, rbac.ActionTraceReadMeta, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
			return
		}
	} else {
		if !s.authorize(w, r, sc, rbac.ActionTraceReadMeta, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
			return
		}
	}

	filterHash := sha256HexString("traces|" + envFilter + "|" + statusFilter + "|" + modelFilter)
	limit, cur, ok := parsePageParams(w, r, s.cursorSecret, "traces", orgID, filterHash)
	if !ok {
		return
	}

	nowMs := s.nowMs()
	var conds []string
	var args []any
	args = append(args, nowMs)
	conds = append(conds, `t.org_id = ?`)
	args = append(args, orgID)

	if envFilter != "" {
		conds = append(conds, `t.env_id = ?`)
		args = append(args, envFilter)
	}
	if modelFilter != "" {
		conds = append(conds, `t.route_slug = ?`)
		args = append(args, modelFilter)
	}
	if statusFilter != "" {
		if code, convErr := strconv.Atoi(statusFilter); convErr == nil {
			conds = append(conds, `t.http_status = ?`)
			args = append(args, code)
		} else {
			switch strings.ToLower(statusFilter) {
			case "error", "failed":
				conds = append(conds, `t.http_status >= 400`)
			case "ok", "success":
				conds = append(conds, `t.http_status < 400`)
			default:
				writeErrorEnvelope(w, r, http.StatusBadRequest, "invalid_request", "invalid_request_error", "status filter must be an HTTP status code, ok, or error", false, nil)
				return
			}
		}
	}
	if !actorHasOrgScopeBinding(sc.Actor, orgID) {
		projIDs := actorProjectIDs(sc.Actor, orgID)
		if len(projIDs) == 0 {
			writeJSON(w, http.StatusOK, paginatedListEnvelope[traceSummaryDTO]{Data: []traceSummaryDTO{}, HasMore: false})
			return
		}
		conds = append(conds, `t.project_id IN (`+strings.Repeat("?,", len(projIDs)-1)+`?)`)
		for _, pid := range projIDs {
			args = append(args, pid)
		}
	}
	if cur != nil {
		conds = append(conds, `(t.created_at_ms < ? OR (t.created_at_ms = ? AND t.request_id < ?))`)
		args = append(args, cur.CreatedMs, cur.CreatedMs, cur.LastID)
	}

	query := `
		SELECT t.request_id, t.created_at_ms, t.project_id, t.env_id, t.virtual_key_id,
		       t.entry_point, t.route_slug, t.selected_deployment_id, t.http_status,
		       t.canonical_error_code, t.ttft_ms, t.total_duration_ms, t.waterfall_stages_json,
		       pc.data_region,
		       (SELECT us.usage_accuracy FROM usage_settlements us WHERE us.org_id = t.org_id AND us.request_id = t.request_id ORDER BY us.settled_at_ms DESC LIMIT 1),
		       (SELECT COALESCE(SUM(us.total_cost_micros), 0) FROM usage_settlements us WHERE us.org_id = t.org_id AND us.request_id = t.request_id),
		       EXISTS(SELECT 1 FROM debug_payload_blobs d WHERE d.org_id = t.org_id AND d.project_id = t.project_id AND d.request_id = t.request_id AND d.expires_at_ms > ?)
		FROM request_traces_meta t
		LEFT JOIN provider_connections pc ON pc.org_id = t.org_id AND pc.id = t.connection_id
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY t.created_at_ms DESC, t.request_id DESC
		LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed listing traces", true, nil)
		return
	}
	defer rows.Close()

	type trRow struct {
		dto       traceSummaryDTO
		createdMs int64
	}
	var items []trRow
	for rows.Next() {
		dto, createdMs, scanErr := scanTraceSummaryRow(rows)
		if scanErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed scanning trace", true, nil)
			return
		}
		items = append(items, trRow{dto: dto, createdMs: createdMs})
	}
	if err := rows.Err(); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed iterating traces", true, nil)
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	data := make([]traceSummaryDTO, len(items))
	for i := range items {
		data[i] = items[i].dto
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		tok, curErr := encodePaginationCursor(s.cursorSecret, "traces", orgID, filterHash, last.createdMs, last.dto.RequestID)
		if curErr != nil {
			writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed encoding cursor", true, nil)
			return
		}
		nextCursor = &tok
	}

	writeJSON(w, http.StatusOK, paginatedListEnvelope[traceSummaryDTO]{
		Data:       data,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (s *Server) handleGetTraceWaterfall(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	reqID := strings.TrimSpace(r.PathValue("requestId"))
	if reqID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "trace not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	dto, projID, err := s.loadTraceByRequestID(r.Context(), orgID, reqID)
	if errors.Is(err, port.ErrNotFound) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "trace not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed loading trace", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionTraceReadMeta, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}

	writeJSON(w, http.StatusOK, dto)
}

func (s *Server) handleGetTraceDebugPayload(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	reqID := strings.TrimSpace(r.PathValue("requestId"))
	if reqID == "" {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "debug payload not found", false, nil)
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}

	var (
		blobOrgID, projID string
		nonce, ciphertext []byte
		expiresAtMs       int64
	)
	err := s.db.QueryRowContext(r.Context(), `
		SELECT org_id, project_id, nonce, encrypted_payload_aead, expires_at_ms
		FROM debug_payload_blobs
		WHERE org_id = ? AND request_id = ?
		ORDER BY created_at_ms DESC LIMIT 1`, orgID, reqID,
	).Scan(&blobOrgID, &projID, &nonce, &ciphertext, &expiresAtMs)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && blobOrgID != orgID) {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "debug payload not found", false, nil)
		return
	}
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed querying debug payload", true, nil)
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionTraceReadDebugBlob, rbac.TargetScope{OrgID: orgID, ProjectID: projID}) {
		return
	}

	now := s.nowMs()
	if expiresAtMs <= now {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "debug payload expired or unavailable", false, nil)
		return
	}
	if s.vault == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "vault_unavailable", "upstream_error", "vault is not configured for debug payload decryption", true, nil)
		return
	}

	plaintext, err := s.vault.UnsealProjectBlob(r.Context(), orgID, projID, vault.RecordKindDebugBlob, reqID, nonce, ciphertext)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusNotFound, "resource_not_found", "not_found_error", "debug payload unreadable or crypto-shredded", false, nil)
		return
	}
	defer clear(plaintext)

	var decodedObj map[string]any
	var payloadOut any
	if uErr := json.Unmarshal(plaintext, &decodedObj); uErr == nil && decodedObj != nil {
		payloadOut = decodedObj
	} else {
		payloadOut = map[string]any{"raw": string(plaintext)}
	}

	audID, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed minting audit id", true, nil)
		return
	}
	rawMeta, _ := json.Marshal(map[string]any{
		"project_id":    projID,
		"expires_at_ms": expiresAtMs,
	})
	if err := s.audit.Append(r.Context(), port.AuditEvent{
		ID:           audID,
		CreatedAtMs:  now,
		OrgID:        &orgID,
		ActorKind:    "PRINCIPAL",
		ActorID:      sc.Actor.PrincipalID,
		EntryPoint:   port.AuditEntryPointAdminAPI,
		EventType:    "trace.debug_payload_viewed",
		TargetType:   "request_trace",
		TargetID:     reqID,
		RequestID:    ptrOrNil(requestIDFromContext(r.Context())),
		MetadataJSON: string(rawMeta),
	}); err != nil {
		writeErrorEnvelope(w, r, http.StatusInternalServerError, "internal_error", "upstream_error", "failed recording immutable debug payload view audit", true, nil)
		return
	}

	writeJSON(w, http.StatusOK, debugPayloadResponseDTO{
		RequestID: reqID,
		ExpiresAt: formatTimeRFC3339Ms(expiresAtMs),
		Payload:   payloadOut,
	})
}

func (s *Server) loadTraceByRequestID(ctx context.Context, orgID, reqID string) (traceSummaryDTO, string, error) {
	nowMs := s.nowMs()
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.request_id, t.created_at_ms, t.project_id, t.env_id, t.virtual_key_id,
		       t.entry_point, t.route_slug, t.selected_deployment_id, t.http_status,
		       t.canonical_error_code, t.ttft_ms, t.total_duration_ms, t.waterfall_stages_json,
		       pc.data_region,
		       (SELECT us.usage_accuracy FROM usage_settlements us WHERE us.org_id = t.org_id AND us.request_id = t.request_id ORDER BY us.settled_at_ms DESC LIMIT 1),
		       (SELECT COALESCE(SUM(us.total_cost_micros), 0) FROM usage_settlements us WHERE us.org_id = t.org_id AND us.request_id = t.request_id),
		       EXISTS(SELECT 1 FROM debug_payload_blobs d WHERE d.org_id = t.org_id AND d.project_id = t.project_id AND d.request_id = t.request_id AND d.expires_at_ms > ?)
		FROM request_traces_meta t
		LEFT JOIN provider_connections pc ON pc.org_id = t.org_id AND pc.id = t.connection_id
		WHERE t.org_id = ? AND t.request_id = ?
		ORDER BY t.created_at_ms DESC LIMIT 1`, nowMs, orgID, reqID,
	)
	if err != nil {
		return traceSummaryDTO{}, "", err
	}
	defer rows.Close()
	if !rows.Next() {
		if rerr := rows.Err(); rerr != nil {
			return traceSummaryDTO{}, "", rerr
		}
		return traceSummaryDTO{}, "", port.ErrNotFound
	}
	var (
		dto       traceSummaryDTO
		createdMs int64
		projID    string
	)
	dto, createdMs, projID, err = scanTraceSummaryRowWithProject(rows)
	_ = createdMs
	if err != nil {
		return traceSummaryDTO{}, "", err
	}
	return dto, projID, rows.Err()
}

func scanTraceSummaryRow(rows *sql.Rows) (traceSummaryDTO, int64, error) {
	dto, createdMs, _, err := scanTraceSummaryRowWithProject(rows)
	return dto, createdMs, err
}

func scanTraceSummaryRowWithProject(rows *sql.Rows) (traceSummaryDTO, int64, string, error) {
	var (
		reqID, projID, envID, entryPoint, stagesJSON        string
		vkID, routeSlug, depID, errCode, dataReg, accuracy  sql.NullString
		ttftMs                                              sql.NullInt64
		createdMs, totalDurMs, costMicros                   int64
		httpStatus, hasBlobInt                              int
	)
	if err := rows.Scan(
		&reqID, &createdMs, &projID, &envID, &vkID,
		&entryPoint, &routeSlug, &depID, &httpStatus,
		&errCode, &ttftMs, &totalDurMs, &stagesJSON,
		&dataReg, &accuracy, &costMicros, &hasBlobInt,
	); err != nil {
		return traceSummaryDTO{}, 0, "", err
	}

	var stages []traceWaterfallStageDTO
	if strings.TrimSpace(stagesJSON) != "" {
		_ = json.Unmarshal([]byte(stagesJSON), &stages)
	}
	if stages == nil {
		stages = []traceWaterfallStageDTO{}
	}

	var ttftPtr *float64
	if ttftMs.Valid {
		v := float64(ttftMs.Int64)
		ttftPtr = &v
	}

	usageAcc := "EXACT"
	if accuracy.Valid && accuracy.String != "" {
		usageAcc = accuracy.String
	}

	routeStr := ""
	if routeSlug.Valid {
		routeStr = routeSlug.String
	}
	vkStr := ""
	if vkID.Valid {
		vkStr = vkID.String
	}

	return traceSummaryDTO{
		RequestID:       reqID,
		Plane:           mapTracePlaneOpenAPI(entryPoint),
		EnvID:           envID,
		VirtualKeyID:    vkStr,
		RouteSlug:       routeStr,
		DeploymentID:    sqlNullStrPtr(depID),
		DataRegion:      sqlNullStrPtr(dataReg),
		StatusCode:      httpStatus,
		ErrorCode:       sqlNullStrPtr(errCode),
		UsageAccuracy:   usageAcc,
		TotalCostMicros: costMicros,
		TTFTMs:          ttftPtr,
		DurationMs:      float64(totalDurMs),
		HasDebugPayload: hasBlobInt == 1,
		WaterfallStages: stages,
		CreatedAt:       formatTimeRFC3339Ms(createdMs),
	}, createdMs, projID, nil
}

func mapTracePlaneOpenAPI(entryPoint string) string {
	switch strings.ToLower(strings.TrimSpace(entryPoint)) {
	case "native_http":
		return "NATIVE"
	case "mcp_http":
		return "MCP"
	case "a2a_http":
		return "A2A"
	default:
		return "PROVIDER"
	}
}
