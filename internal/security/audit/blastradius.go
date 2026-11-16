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
	if orgID == "" && !q.AllowInstanceScope {
		return nil, fmt.Errorf("%w: org_id is required unless AllowInstanceScope is enabled", port.ErrInvalidInput)
	}
	extID := strings.TrimSpace(q.ExtensionInstallationID)
	connID := strings.TrimSpace(q.ConnectionID)
	vkID := strings.TrimSpace(q.VirtualKeyID)
	if extID == "" && connID == "" && vkID == "" {
		return nil, fmt.Errorf("%w: at least one of extension_installation_id, connection_id, or virtual_key_id is required", port.ErrInvalidInput)
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
	if q.FromMs > 0 {
		conds = append(conds, "created_at_ms >= ?")
		args = append(args, q.FromMs)
	}
	if q.ToMs > 0 {
		conds = append(conds, "created_at_ms <= ?")
		args = append(args, q.ToMs)
	}

	whereClause := strings.Join(conds, " AND ")

	var totalCount int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_traces_meta WHERE `+whereClause, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("audit: count blast radius traces: %w", err)
	}

	traceSQL := `
		SELECT request_id, created_at_ms, org_id, project_id, env_id, virtual_key_id,
		       entry_point, route_slug, selected_deployment_id, connection_id,
		       extension_installation_id, extension_version_id, http_status, canonical_error_code
		FROM request_traces_meta
		WHERE ` + whereClause + `
		ORDER BY created_at_ms DESC
		LIMIT ?`
	traceArgs := append(append([]any(nil), args...), limit)

	rows, err := db.QueryContext(ctx, traceSQL, traceArgs...)
	if err != nil {
		return nil, fmt.Errorf("audit: query blast radius traces: %w", err)
	}
	defer rows.Close()

	orgSet := make(map[string]struct{})
	projSet := make(map[string]struct{})
	envSet := make(map[string]struct{})
	vkSet := make(map[string]struct{})
	depSet := make(map[string]struct{})
	var traces []TraceHit

	for rows.Next() {
		var (
			hit                                      TraceHit
			vkNS, routeNS, depNS, connNS, extNS, verNS, errNS sql.NullString
		)
		if serr := rows.Scan(
			&hit.RequestID, &hit.CreatedAtMs, &hit.OrgID, &hit.ProjectID, &hit.EnvID, &vkNS,
			&hit.EntryPoint, &routeNS, &depNS, &connNS,
			&extNS, &verNS, &hit.HTTPStatus, &errNS,
		); serr != nil {
			return nil, fmt.Errorf("audit: scan blast radius trace: %w", serr)
		}
		hit.VirtualKeyID = nullToPtr(vkNS)
		hit.RouteSlug = nullToPtr(routeNS)
		hit.SelectedDeploymentID = nullToPtr(depNS)
		hit.ConnectionID = nullToPtr(connNS)
		hit.ExtensionInstallationID = nullToPtr(extNS)
		hit.ExtensionVersionID = nullToPtr(verNS)
		hit.CanonicalErrorCode = nullToPtr(errNS)

		orgSet[hit.OrgID] = struct{}{}
		projSet[hit.ProjectID] = struct{}{}
		envSet[hit.EnvID] = struct{}{}
		if hit.VirtualKeyID != nil {
			vkSet[*hit.VirtualKeyID] = struct{}{}
		}
		if hit.SelectedDeploymentID != nil {
			depSet[*hit.SelectedDeploymentID] = struct{}{}
		}
		traces = append(traces, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterate blast radius traces: %w", err)
	}

	// Correlate relevant audit_events strictly respecting tenant boundary.
	auditEvents, err := queryBlastRadiusAuditEvents(ctx, db, q, orgID, extID, connID, vkID, limit)
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
	orgID, extID, connID, vkID string,
	limit int,
) ([]StoredEvent, error) {
	var conds []string
	var args []any

	if orgID != "" {
		// Extension installations live at SYSTEM chain_key when instance-installed, or org chain_key for org-scoped mutations.
		if extID != "" && connID == "" && vkID == "" {
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
	placeholders := strings.Repeat("?,", len(targetIDs))
	placeholders = placeholders[:len(placeholders)-1]
	conds = append(conds, "target_id IN ("+placeholders+")")
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

	query := `
		SELECT id, created_at_ms, org_id, chain_key, seq_no,
		       actor_kind, actor_id, entry_point, event_type, target_type, target_id,
		       request_id, metadata_json, prev_hash, entry_hash
		FROM audit_events
		WHERE ` + strings.Join(conds, " AND ") + `
		ORDER BY created_at_ms DESC, seq_no DESC
		LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query blast radius audit events: %w", err)
	}
	defer rows.Close()

	var events []StoredEvent
	for rows.Next() {
		ev, serr := scanStoredEvent(rows)
		if serr != nil {
			return nil, serr
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: iterate blast radius audit events: %w", err)
	}
	return events, nil
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
