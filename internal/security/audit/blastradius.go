// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package audit

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/nguywnben/tanpopo/internal/storage/port"
)

// BlastRadiusQuery parameterizes incident forensic correlation across request_traces_meta and audit_events.
type BlastRadiusQuery struct {
	OrgID                   string
	AllowInstanceScope      bool // Enables instance-wide correlation for OPERATOR incident response when OrgID is empty.
	ProjectID               string
	ExtensionInstallationID string
	ConnectionID            string
	VirtualKeyID            string
	FromMs                  int64
	ToMs                    int64
	Limit                   int
}

// TraceHit summarizes an impacted inference trace within an incident blast-radius window.
type TraceHit struct {
	RequestID               string  `json:"request_id"`
	CreatedAtMs             int64   `json:"created_at_ms"`
	OrgID                   string  `json:"org_id"`
	ProjectID               string  `json:"project_id"`
	EnvID                   string  `json:"env_id"`
	VirtualKeyID            *string `json:"virtual_key_id,omitempty"`
	EntryPoint              string  `json:"entry_point"`
	RouteSlug               *string `json:"route_slug,omitempty"`
	SelectedDeploymentID    *string `json:"selected_deployment_id,omitempty"`
	ConnectionID            *string `json:"connection_id,omitempty"`
	ExtensionInstallationID *string `json:"extension_installation_id,omitempty"`
	ExtensionVersionID      *string `json:"extension_version_id,omitempty"`
	HTTPStatus              int     `json:"http_status"`
	CanonicalErrorCode      *string `json:"canonical_error_code,omitempty"`
}

// BlastRadiusReport enumerates impacted projects, environments, virtual keys, deployments,
// request traces, and correlated audit events for security containment (TP-IMP-0023 / OPS-AC-04).
type BlastRadiusReport struct {
	OrgID               string        `json:"org_id,omitempty"`
	FromMs              int64         `json:"from_ms"`
	ToMs                int64         `json:"to_ms"`
	TotalTraceCount     int64         `json:"total_trace_count"`
	AffectedOrgs        []string      `json:"affected_orgs"`
	AffectedProjects    []string      `json:"affected_projects"`
	AffectedEnvs        []string      `json:"affected_envs"`
	AffectedVirtualKeys []string      `json:"affected_virtual_keys"`
	AffectedDeployments []string      `json:"affected_deployments"`
	Traces              []TraceHit    `json:"traces"`
	AuditEvents         []StoredEvent `json:"audit_events"`
}

// QueryBlastRadius correlates request_traces_meta + audit_events by extension_installation_id,
// connection_id, or virtual_key_id within [FromMs, ToMs], enforcing org_id isolation unless
// AllowInstanceScope is explicitly enabled by an OPERATOR context.
func QueryBlastRadius(ctx context.Context, db *sql.DB, q BlastRadiusQuery) (*BlastRadiusReport, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: non-nil *sql.DB is required", port.ErrInvalidInput)
	}
	orgID := strings.TrimSpace(q.OrgID)
	if strings.EqualFold(orgID, SystemChainKey) {
		return nil, fmt.Errorf("%w: org_id cannot be %q; leave org_id empty with AllowInstanceScope=true for instance scope", port.ErrInvalidInput, SystemChainKey)
	}
	if orgID == "" && !q.AllowInstanceScope {
		return nil, fmt.Errorf("%w: org_id is required unless AllowInstanceScope is enabled", port.ErrInvalidInput)
	}
	extID := strings.TrimSpace(q.ExtensionInstallationID)
	connID := strings.TrimSpace(q.ConnectionID)
	vkID := strings.TrimSpace(q.VirtualKeyID)
	projectID := strings.TrimSpace(q.ProjectID)
	if extID == "" && connID == "" && vkID == "" && projectID == "" {
		return nil, fmt.Errorf("%w: at least one blast-radius target is required", port.ErrInvalidInput)
	}
	if q.ToMs > 0 && q.FromMs > q.ToMs {
		return nil, fmt.Errorf("%w: from_ms (%d) cannot exceed to_ms (%d)", port.ErrInvalidInput, q.FromMs, q.ToMs)
	}
	limit := q.Limit
	if limit <= 0 || limit > 2000 {
		limit = 500
	}

	var conds []string
	var args []any

	if orgID != "" {
		conds = append(conds, "org_id = ?")
		args = append(args, orgID)
	}
	if extID != "" {
		conds = append(conds, "extension_installation_id = ?")
		args = append(args, extID)
	}
	if connID != "" {
		conds = append(conds, "connection_id = ?")
		args = append(args, connID)
	}
	if vkID != "" {
		conds = append(conds, "virtual_key_id = ?")
		args = append(args, vkID)
	}
	if projectID != "" {
		conds = append(conds, "project_id = ?")
		args = append(args, projectID)
	}
	if q.FromMs > 0 {
		conds = append(conds, "created_at_ms >= ?")
		args = append(args, q.FromMs)
	}
	if q.ToMs > 0 {
		conds = append(conds, "created_at_ms <= ?")
		args = append(args, q.ToMs)
	}

	dialect := port.DetectDialect(ctx, db)
	whereClause := strings.Join(conds, " AND ")

	orgSet := make(map[string]struct{})
	projSet := make(map[string]struct{})
	envSet := make(map[string]struct{})
	vkSet := make(map[string]struct{})
	depSet := make(map[string]struct{})

	type traceExec interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	}
	collectTraces := func(exec traceExec) (int64, []TraceHit, error) {
		var cnt int64
		if err := exec.QueryRowContext(ctx, port.Rebind(dialect, `SELECT COUNT(*) FROM request_traces_meta WHERE `+whereClause), args...).Scan(&cnt); err != nil {
			return 0, nil, fmt.Errorf("audit: count blast radius traces: %w", err)
		}
		distinctSQL := port.Rebind(dialect, `
			SELECT DISTINCT org_id, project_id, env_id, virtual_key_id, selected_deployment_id
			FROM request_traces_meta
			WHERE `+whereClause)
		distRows, err := exec.QueryContext(ctx, distinctSQL, args...)
		if err != nil {
			return 0, nil, fmt.Errorf("audit: query blast radius affected dimensions: %w", err)
		}
		for distRows.Next() {
			var (
				dOrg, dProj, dEnv string
				dVK, dDep         sql.NullString
			)
			if serr := distRows.Scan(&dOrg, &dProj, &dEnv, &dVK, &dDep); serr != nil {
				_ = distRows.Close()
				return 0, nil, fmt.Errorf("audit: scan blast radius affected dimensions: %w", serr)
			}
			orgSet[dOrg] = struct{}{}
			projSet[dProj] = struct{}{}
			envSet[dEnv] = struct{}{}
			if dVK.Valid && dVK.String != "" {
				vkSet[dVK.String] = struct{}{}
			}
			if dDep.Valid && dDep.String != "" {
				depSet[dDep.String] = struct{}{}
			}
		}
		if err := distRows.Err(); err != nil {
			_ = distRows.Close()
			return 0, nil, fmt.Errorf("audit: iterate blast radius affected dimensions: %w", err)
		}
		_ = distRows.Close()

		traceSQL := port.Rebind(dialect, `
			SELECT request_id, created_at_ms, org_id, project_id, env_id, virtual_key_id,
			       entry_point, route_slug, selected_deployment_id, connection_id,
			       extension_installation_id, extension_version_id, http_status, canonical_error_code
			FROM request_traces_meta
			WHERE `+whereClause+`
			ORDER BY created_at_ms DESC
			LIMIT ?`)
		traceArgs := append(append([]any(nil), args...), limit)
		rows, err := exec.QueryContext(ctx, traceSQL, traceArgs...)
		if err != nil {
			return 0, nil, fmt.Errorf("audit: query blast radius traces: %w", err)
		}
		defer rows.Close()

		var hits []TraceHit
		for rows.Next() {
			var (
				hit                                               TraceHit
				vkNS, routeNS, depNS, connNS, extNS, verNS, errNS sql.NullString
			)
			if serr := rows.Scan(
				&hit.RequestID, &hit.CreatedAtMs, &hit.OrgID, &hit.ProjectID, &hit.EnvID, &vkNS,
				&hit.EntryPoint, &routeNS, &depNS, &connNS,
				&extNS, &verNS, &hit.HTTPStatus, &errNS,
			); serr != nil {
				return 0, nil, fmt.Errorf("audit: scan blast radius trace: %w", serr)
			}
			hit.VirtualKeyID = nullToPtr(vkNS)
			hit.RouteSlug = nullToPtr(routeNS)
			hit.SelectedDeploymentID = nullToPtr(depNS)
			hit.ConnectionID = nullToPtr(connNS)
			hit.ExtensionInstallationID = nullToPtr(extNS)
			hit.ExtensionVersionID = nullToPtr(verNS)
			hit.CanonicalErrorCode = nullToPtr(errNS)
			hits = append(hits, hit)
		}
		if err := rows.Err(); err != nil {
			return 0, nil, fmt.Errorf("audit: iterate blast radius traces: %w", err)
		}
		return cnt, hits, nil
	}

	var (
		totalCount int64
		traces     []TraceHit
	)
	if dialect == port.DialectPostgres {
		tx, berr := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if berr != nil {
			return nil, fmt.Errorf("audit: begin blast radius trace tx: %w", berr)
		}
		defer func() { _ = tx.Rollback() }()
		if orgID != "" {
			if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, orgID); serr != nil {
				return nil, fmt.Errorf("audit: set app.current_org_id: %w", serr)
			}
			cnt, hits, cerr := collectTraces(tx)
			if cerr != nil {
				return nil, cerr
			}
			totalCount, traces = cnt, hits
		} else {
			orgIDs, lerr := listBlastRadiusOrgIDsPG(ctx, tx)
			if lerr != nil {
				return nil, lerr
			}
			for _, oid := range orgIDs {
				if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, oid); serr != nil {
					return nil, fmt.Errorf("audit: set app.current_org_id(%s): %w", oid, serr)
				}
				cnt, hits, cerr := collectTraces(tx)
				if cerr != nil {
					return nil, cerr
				}
				totalCount += cnt
				traces = append(traces, hits...)
			}
			sort.Slice(traces, func(i, j int) bool {
				if traces[i].CreatedAtMs != traces[j].CreatedAtMs {
					return traces[i].CreatedAtMs > traces[j].CreatedAtMs
				}
				return traces[i].RequestID > traces[j].RequestID
			})
			if len(traces) > limit {
				traces = traces[:limit]
			}
		}
	} else {
		cnt, hits, cerr := collectTraces(db)
		if cerr != nil {
			return nil, cerr
		}
		totalCount, traces = cnt, hits
	}

	// Correlate relevant audit_events strictly respecting tenant boundary.
	auditEvents, err := queryBlastRadiusAuditEvents(ctx, db, q, orgID, extID, connID, vkID, projectID, limit)
	if err != nil {
		return nil, err
	}

	return &BlastRadiusReport{
		OrgID:               orgID,
		FromMs:              q.FromMs,
		ToMs:                q.ToMs,
		TotalTraceCount:     totalCount,
		AffectedOrgs:        sortedKeys(orgSet),
		AffectedProjects:    sortedKeys(projSet),
		AffectedEnvs:        sortedKeys(envSet),
		AffectedVirtualKeys: sortedKeys(vkSet),
		AffectedDeployments: sortedKeys(depSet),
		Traces:              traces,
		AuditEvents:         auditEvents,
	}, nil
}

func queryBlastRadiusAuditEvents(
	ctx context.Context,
	db *sql.DB,
	q BlastRadiusQuery,
	orgID, extID, connID, vkID, projectID string,
	limit int,
) ([]StoredEvent, error) {
	var conds []string
	var args []any

	if orgID != "" {
		// Tenant-scoped queries MUST stay isolated to the tenant's own chain unless the caller
		// explicitly holds instance-operator scope (AllowInstanceScope) while inspecting an extension.
		if extID != "" && q.AllowInstanceScope {
			conds = append(conds, "(chain_key = ? OR chain_key = 'SYSTEM')")
			args = append(args, orgID)
		} else {
			conds = append(conds, "chain_key = ?")
			args = append(args, orgID)
		}
	}

	var targetIDs []string
	if extID != "" {
		targetIDs = append(targetIDs, extID)
	}
	if connID != "" {
		targetIDs = append(targetIDs, connID)
	}
	if vkID != "" {
		targetIDs = append(targetIDs, vkID)
	}
	if projectID != "" {
		targetIDs = append(targetIDs, projectID)
	}
	dialect := port.DetectDialect(ctx, db)
	jsonTargetExpr := "json_extract(metadata_json, '$.target_id')"
	if dialect == port.DialectPostgres {
		jsonTargetExpr = "metadata_json->>'target_id'"
	}
	placeholders := strings.Repeat("?,", len(targetIDs))
	placeholders = placeholders[:len(placeholders)-1]
	conds = append(conds, "(target_id IN ("+placeholders+") OR (target_type = 'emergency_kill_switch' AND "+jsonTargetExpr+" IN ("+placeholders+")))")
	for _, tid := range targetIDs {
		args = append(args, tid)
	}
	for _, tid := range targetIDs {
		args = append(args, tid)
	}

	if q.FromMs > 0 {
		conds = append(conds, "created_at_ms >= ?")
		args = append(args, q.FromMs)
	}
	if q.ToMs > 0 {
		conds = append(conds, "created_at_ms <= ?")
		args = append(args, q.ToMs)
	}

	query := port.Rebind(dialect, `
		SELECT id, created_at_ms, org_id, chain_key, seq_no,
		       actor_kind, actor_id, entry_point, event_type, target_type, target_id,
		       request_id, metadata_json, prev_hash, entry_hash
		FROM audit_events
		WHERE `+strings.Join(conds, " AND ")+`
		ORDER BY created_at_ms DESC, seq_no DESC
		LIMIT ?`)
	args = append(args, limit)

	scanEvents := func(rows *sql.Rows) ([]StoredEvent, error) {
		defer rows.Close()
		var list []StoredEvent
		for rows.Next() {
			ev, serr := scanStoredEvent(rows)
			if serr != nil {
				return nil, serr
			}
			list = append(list, ev)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("audit: iterate blast radius audit events: %w", err)
		}
		return list, nil
	}

	if dialect != port.DialectPostgres {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("audit: query blast radius audit events: %w", err)
		}
		return scanEvents(rows)
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("audit: begin blast radius audit tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if orgID != "" {
		opVal := "false"
		if q.AllowInstanceScope && extID != "" {
			opVal = "true"
		}
		if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', $1, true), set_config('app.current_org_id', $2, true)`, opVal, orgID); serr != nil {
			return nil, fmt.Errorf("audit: set blast radius audit scope: %w", serr)
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("audit: query blast radius audit events: %w", err)
		}
		return scanEvents(rows)
	}

	orgIDs, lerr := listBlastRadiusOrgIDsPG(ctx, tx)
	if lerr != nil {
		return nil, lerr
	}

	seen := make(map[string]struct{})
	var events []StoredEvent
	appendUnique := func(batch []StoredEvent) {
		for _, ev := range batch {
			if _, ok := seen[ev.ID]; ok {
				continue
			}
			seen[ev.ID] = struct{}{}
			events = append(events, ev)
		}
	}

	// 1. Collect SYSTEM (org_id IS NULL) events under operator RLS.
	if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true), set_config('app.is_instance_operator', 'true', true)`); serr != nil {
		return nil, fmt.Errorf("audit: set operator audit scope: %w", serr)
	}
	sysRows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query SYSTEM blast radius audit events: %w", err)
	}
	sysBatch, serr := scanEvents(sysRows)
	if serr != nil {
		return nil, serr
	}
	appendUnique(sysBatch)

	// 2. Collect tenant events per organization under FORCE RLS.
	for _, oid := range orgIDs {
		if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.is_instance_operator', 'false', true), set_config('app.current_org_id', $1, true)`, oid); serr != nil {
			return nil, fmt.Errorf("audit: set org audit scope(%s): %w", oid, serr)
		}
		oRows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("audit: query org blast radius audit events(%s): %w", oid, err)
		}
		oBatch, serr := scanEvents(oRows)
		if serr != nil {
			return nil, serr
		}
		appendUnique(oBatch)
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAtMs != events[j].CreatedAtMs {
			return events[i].CreatedAtMs > events[j].CreatedAtMs
		}
		if events[i].SeqNo != events[j].SeqNo {
			return events[i].SeqNo > events[j].SeqNo
		}
		return events[i].ID > events[j].ID
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func listBlastRadiusOrgIDsPG(ctx context.Context, tx *sql.Tx) ([]string, error) {
	if _, serr := tx.ExecContext(ctx, `SELECT set_config('app.current_org_id', '', true), set_config('app.is_instance_operator', 'true', true)`); serr != nil {
		return nil, fmt.Errorf("audit: set operator org list scope: %w", serr)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM organizations ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("audit: list organizations for blast radius: %w", err)
	}
	defer rows.Close()
	var orgIDs []string
	for rows.Next() {
		var oid string
		if serr := rows.Scan(&oid); serr != nil {
			return nil, serr
		}
		orgIDs = append(orgIDs, oid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return orgIDs, nil
}

func nullToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	v := n.String
	return &v
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
