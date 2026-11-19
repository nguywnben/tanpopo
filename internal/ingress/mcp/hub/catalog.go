// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/nguywnben/tanpopo/internal/security/egress"
)

var errToolNotFound = errors.New("MCP tool not found or not granted")

type scope struct {
	OrgID     string
	ProjectID string
	EnvID     string
	KeyID     string
	Tools     map[string]struct{}
}

type serverRecord struct {
	ID             string
	OrgID          string
	ProjectID      string
	Slug           string
	Transport      string
	Endpoint       string
	AllowPrivateIP bool
	AllowedCIDRs   []string
	CredentialID   string
	PinningMode    string
	Status         string
}

type toolRecord struct {
	ID              string
	Server          serverRecord
	Name            string
	Schema          json.RawMessage
	ApprovedHash    string
	ObservedHash    string
	DriftLocked     bool
	SideEffectClass string
}

type catalog struct{ db *sql.DB }

func newCatalog(db *sql.DB) (*catalog, error) {
	if db == nil {
		return nil, errors.New("MCP catalog requires a database")
	}
	return &catalog{db: db}, nil
}

func (c *catalog) listGrantedTools(ctx context.Context, sc scope) ([]toolRecord, error) {
	rows, err := c.db.QueryContext(ctx, `
		SELECT t.id, s.id, s.org_id, s.project_id, s.server_slug, s.transport_kind,
		       s.endpoint_url, s.allow_private_ip, s.allowed_cidrs_json,
		       COALESCE(s.credential_id, ''), s.pinning_mode, s.status,
		       t.tool_name, t.tool_schema_json, t.approved_schema_sha256,
		       t.observed_schema_sha256, t.schema_drift_locked, t.side_effect_class
		FROM mcp_tools t
		JOIN mcp_servers s ON s.org_id = t.org_id AND s.id = t.mcp_server_id
		WHERE t.org_id = ? AND s.project_id = ?
		  AND EXISTS (
		      SELECT 1 FROM mcp_tool_grants g
		      WHERE g.org_id = t.org_id AND g.mcp_tool_id = t.id
		        AND g.project_id = ? AND g.env_id = ?
		        AND (g.virtual_key_id IS NULL OR g.virtual_key_id = ?)
		  )
		  AND s.status = 'ACTIVE'
		ORDER BY s.server_slug, t.tool_name`,
		sc.OrgID, sc.ProjectID, sc.ProjectID, sc.EnvID, sc.KeyID)
	if err != nil {
		return nil, fmt.Errorf("list MCP catalog: %w", err)
	}
	defer rows.Close()

	var out []toolRecord
	for rows.Next() {
		var t toolRecord
		var allowPrivate, drift int
		var cidrs string
		var schemaJSON []byte
		if err := rows.Scan(&t.ID, &t.Server.ID, &t.Server.OrgID, &t.Server.ProjectID,
			&t.Server.Slug, &t.Server.Transport, &t.Server.Endpoint, &allowPrivate,
			&cidrs, &t.Server.CredentialID, &t.Server.PinningMode, &t.Server.Status,
			&t.Name, &schemaJSON, &t.ApprovedHash, &t.ObservedHash, &drift,
			&t.SideEffectClass); err != nil {
			return nil, fmt.Errorf("scan MCP catalog: %w", err)
		}
		t.Schema = append(t.Schema[:0], schemaJSON...)
		t.Server.AllowPrivateIP = allowPrivate != 0
		if err := json.Unmarshal([]byte(cidrs), &t.Server.AllowedCIDRs); err != nil {
			return nil, errors.New("invalid MCP egress policy")
		}
		t.DriftLocked = drift != 0
		name, err := toolName(t.Server.Slug, t.Name)
		if err != nil {
			return nil, err
		}
		if _, ok := sc.Tools[name]; !ok {
			continue
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate MCP catalog: %w", err)
	}
	return out, nil
}

func (c *catalog) getGrantedTool(ctx context.Context, sc scope, serverSlug, name string) (toolRecord, error) {
	var t toolRecord
	var allowPrivate, drift int
	var cidrs string
	var schemaJSON []byte
	err := c.db.QueryRowContext(ctx, `
		SELECT t.id, s.id, s.org_id, s.project_id, s.server_slug, s.transport_kind,
		       s.endpoint_url, s.allow_private_ip, s.allowed_cidrs_json,
		       COALESCE(s.credential_id, ''), s.pinning_mode, s.status,
		       t.tool_name, t.tool_schema_json, t.approved_schema_sha256,
		       t.observed_schema_sha256, t.schema_drift_locked, t.side_effect_class
		FROM mcp_tools t
		JOIN mcp_servers s ON s.org_id = t.org_id AND s.id = t.mcp_server_id
		JOIN mcp_tool_grants g ON g.org_id = t.org_id AND g.mcp_tool_id = t.id
		WHERE t.org_id = ? AND s.project_id = ? AND g.project_id = ? AND g.env_id = ?
		  AND (g.virtual_key_id IS NULL OR g.virtual_key_id = ?)
		  AND s.server_slug = ? AND t.tool_name = ? AND s.status = 'ACTIVE'
		LIMIT 1`, sc.OrgID, sc.ProjectID, sc.ProjectID, sc.EnvID, sc.KeyID, serverSlug, name).Scan(
		&t.ID, &t.Server.ID, &t.Server.OrgID, &t.Server.ProjectID,
		&t.Server.Slug, &t.Server.Transport, &t.Server.Endpoint, &allowPrivate,
		&cidrs, &t.Server.CredentialID, &t.Server.PinningMode, &t.Server.Status,
		&t.Name, &schemaJSON, &t.ApprovedHash, &t.ObservedHash, &drift, &t.SideEffectClass)
	if errors.Is(err, sql.ErrNoRows) {
		return toolRecord{}, errToolNotFound
	}
	if err != nil {
		return toolRecord{}, fmt.Errorf("load MCP tool: %w", err)
	}
	t.Schema = append(t.Schema[:0], schemaJSON...)
	t.Server.AllowPrivateIP = allowPrivate != 0
	if err := json.Unmarshal([]byte(cidrs), &t.Server.AllowedCIDRs); err != nil {
		return toolRecord{}, errors.New("invalid MCP egress policy")
	}
	t.DriftLocked = drift != 0
	namespaced, err := toolName(serverSlug, name)
	if err != nil {
		return toolRecord{}, errToolNotFound
	}
	if _, allowed := sc.Tools[namespaced]; !allowed {
		return toolRecord{}, errToolNotFound
	}
	return t, nil
}

func (c *catalog) lockSchemaDrift(ctx context.Context, t toolRecord, observed string) error {
	_, err := c.db.ExecContext(ctx, `
		UPDATE mcp_tools
		SET observed_schema_sha256 = ?, schema_drift_locked = 1, version = version + 1,
		    updated_at_ms = CAST(strftime('%s','now') AS INTEGER) * 1000
		WHERE org_id = ? AND id = ?`, observed, t.Server.OrgID, t.ID)
	if err != nil {
		return fmt.Errorf("lock MCP schema drift: %w", err)
	}
	return nil
}

func (s serverRecord) egressPolicy() (*egress.Policy, error) {
	parsed, err := url.Parse(s.Endpoint)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, errors.New("invalid MCP endpoint")
	}
	if parsed.Scheme != "https" && !s.AllowPrivateIP {
		return nil, errors.New("MCP endpoint must use HTTPS unless explicit private-network consent is configured")
	}
	port := 443
	if parsed.Scheme == "http" {
		port = 80
	}
	if p := parsed.Port(); p != "" {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n < 1 || n > 65535 {
			return nil, errors.New("invalid MCP endpoint port")
		}
		port = n
	}
	return &egress.Policy{AllowedHosts: []string{parsed.Host}, AllowPrivateIP: s.AllowPrivateIP,
		AllowedCIDRs: append([]string(nil), s.AllowedCIDRs...), AllowedPorts: []int{port}, RequireTLS: parsed.Scheme == "https",
		RequirePrivateForHTTP: parsed.Scheme == "http"}, nil
}
