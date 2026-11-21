// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	mcphub "github.com/nguywnben/tanpopo/internal/ingress/mcp/hub"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/schema_pin"
	"github.com/nguywnben/tanpopo/internal/pkg/idgen"
	"github.com/nguywnben/tanpopo/internal/security/audit"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func isMCPUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

type mcpServerDTO struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Transport    string `json:"transport"`
	EndpointURL  string `json:"endpoint_url"`
	AllowPrivate bool   `json:"allow_private_ip"`
	State        string `json:"state"`
	ToolCount    int    `json:"tool_count"`
	Version      int64  `json:"version"`
}

type mcpServerCreate struct {
	ProjectID    string   `json:"project_id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Transport    string   `json:"transport"`
	EndpointURL  string   `json:"endpoint_url"`
	AllowPrivate bool     `json:"allow_private_ip"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
	CredentialID *string  `json:"credential_id"`
}

func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPRead, rbac.TargetScope{OrgID: orgID, IsCollectionList: true}) {
		return
	}
	projectIDs := actorProjectIDs(sc.Actor, orgID)
	orgWide := actorHasOrgScopeBinding(sc.Actor, orgID)
	q := `SELECT s.id,s.project_id,s.server_slug,s.display_name,s.transport_kind,s.endpoint_url,s.allow_private_ip,CASE WHEN EXISTS(SELECT 1 FROM mcp_tools d WHERE d.org_id=s.org_id AND d.mcp_server_id=s.id AND d.schema_drift_locked=` + port.BoolExpr(s.dialect, true) + `) THEN 'SCHEMA_DRIFT_LOCKED' ELSE s.status END,s.version,(SELECT COUNT(*) FROM mcp_tools t WHERE t.org_id=s.org_id AND t.mcp_server_id=s.id) FROM mcp_servers s WHERE s.org_id=?`
	args := []any{orgID}
	if !orgWide {
		if len(projectIDs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"data": []mcpServerDTO{}, "has_more": false})
			return
		}
		q += ` AND s.project_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(projectIDs)), ",") + `)`
		for _, id := range projectIDs {
			args = append(args, id)
		}
	}
	q += ` ORDER BY s.server_slug,s.id`
	items := []mcpServerDTO{}
	scanFailed := false
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		rows, err := dbq.QueryContext(r.Context(), s.rebind(q), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d mcpServerDTO
			var priv port.FlexBool
			if err := rows.Scan(&d.ID, &d.ProjectID, &d.Slug, &d.Name, &d.Transport, &d.EndpointURL, &priv, &d.State, &d.Version, &d.ToolCount); err != nil {
				scanFailed = true
				return err
			}
			d.Transport = "streamable_http"
			d.AllowPrivate = bool(priv)
			if d.State == "ACTIVE" {
				d.State = "ACTIVE"
			}
			items = append(items, d)
		}
		return rows.Err()
	})
	if scanFailed {
		mcpInternal(w, r, "failed reading MCP servers")
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed listing MCP servers")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false, "next_cursor": nil})
}

func (s *Server) handleRegisterMCPServer(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	var req mcpServerCreate
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" || !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: req.ProjectID}) {
		if req.ProjectID == "" {
			writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "project_id is required", false, nil)
		}
		return
	}
	var exists int
	if err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM projects WHERE org_id=? AND id=?`), orgID, req.ProjectID).Scan(&exists)
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "project not found", false, nil)
		} else {
			mcpInternal(w, r, "failed validating project")
		}
		return
	}
	req.Slug = strings.TrimSpace(strings.ToLower(req.Slug))
	req.Name = strings.TrimSpace(req.Name)
	if !validMCPSlug(req.Slug) || req.Name == "" || len(req.Name) > 128 {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "slug and name are required; slug must be a lowercase identifier", false, nil)
		return
	}
	transport := strings.ToUpper(strings.ReplaceAll(req.Transport, "-", "_"))
	if transport != "STREAMABLE_HTTP" {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "Unit 36 supports only Streamable HTTP MCP servers", false, nil)
		return
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(req.EndpointURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "endpoint_url must be an absolute HTTP(S) URL without userinfo", false, nil)
		return
	}
	if parsed.Scheme != "https" && !req.AllowPrivate {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "HTTP endpoints require explicit private-network consent", false, nil)
		return
	}
	if req.AllowPrivate && len(req.AllowedCIDRs) == 0 {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "allowed_cidrs is required when allow_private_ip is true", false, nil)
		return
	}
	for _, raw := range req.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(raw); err != nil {
			writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "allowed_cidrs contains an invalid CIDR", false, nil)
			return
		}
	}
	credential := ""
	if req.CredentialID != nil {
		credential = strings.TrimSpace(*req.CredentialID)
		if credential != "" {
			var found int
			err = s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
				return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM secret_credentials WHERE org_id=? AND id=? AND owner_project_id=?`), orgID, credential, req.ProjectID).Scan(&found)
			})
			if err != nil {
				writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "credential not found", false, nil)
				return
			}
		}
	}
	cidrs, _ := json.Marshal(req.AllowedCIDRs)
	id, err := idgen.New(idgen.PrefixMCP)
	if err != nil {
		mcpInternal(w, r, "failed generating MCP server id")
		return
	}
	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		mcpInternal(w, r, "failed starting MCP registration transaction")
		return
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}
	_, err = tx.ExecContext(r.Context(), s.rebind(`INSERT INTO mcp_servers(id,org_id,project_id,server_slug,display_name,transport_kind,endpoint_url,allow_private_ip,allowed_cidrs_json,credential_id,pinning_mode,status,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,'STRICT_LOCK','ACTIVE',1,?,?)`), id, orgID, req.ProjectID, req.Slug, req.Name, transport, parsed.String(), port.BoolVal(s.dialect, req.AllowPrivate), string(cidrs), nullableString(credential), now, now)
	if err != nil {
		if isMCPUniqueErr(err) {
			writeErrorEnvelope(w, r, 409, "resource_conflict", "conflict_error", "MCP server slug already exists in project", false, nil)
		} else {
			mcpInternal(w, r, "failed registering MCP server")
		}
		return
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "mcp.server_registered", "mcp_server", id, nil); err != nil {
		mcpInternal(w, r, "failed recording MCP registration audit event")
		return
	}
	if err := tx.Commit(); err != nil {
		mcpInternal(w, r, "failed committing MCP registration")
		return
	}
	d := mcpServerDTO{ID: id, ProjectID: req.ProjectID, Slug: req.Slug, Name: req.Name, Transport: "streamable_http", EndpointURL: parsed.String(), AllowPrivate: req.AllowPrivate, State: "ACTIVE", ToolCount: 0, Version: 1}
	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) handleSyncMCPTools(w http.ResponseWriter, r *http.Request) {
	syncCtx, cancelSync := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancelSync()
	r = r.WithContext(syncCtx)
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	serverID := r.PathValue("serverId")
	var srv syncServer
	var priv port.FlexBool
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT id,org_id,project_id,server_slug,transport_kind,endpoint_url,allow_private_ip,allowed_cidrs_json,COALESCE(credential_id,'') FROM mcp_servers WHERE org_id=? AND id=?`), orgID, serverID).Scan(&srv.ID, &srv.OrgID, &srv.ProjectID, &srv.Slug, &srv.Transport, &srv.Endpoint, &priv, &srv.CIDRs, &srv.Credential)
	})
	srv.Private = bool(priv)
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "MCP server not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading MCP server")
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: srv.ProjectID}) {
		return
	}
	if srv.Transport != "STREAMABLE_HTTP" || s.broker == nil {
		writeErrorEnvelope(w, r, http.StatusServiceUnavailable, "mcp_sync_unavailable", "upstream_error", "MCP synchronization is unavailable for this transport", true, nil)
		return
	}
	tools, err := s.fetchMCPTools(syncCtx, srv)
	if err != nil {
		writeErrorEnvelope(w, r, 502, "mcp_sync_failed", "upstream_error", "upstream MCP tool synchronization failed", true, nil)
		return
	}
	type synced struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		SchemaHash  string `json:"schema_hash"`
		DriftLocked bool   `json:"drift_locked"`
		Version     int64  `json:"version"`
	}
	out := make([]synced, 0, len(tools))
	drifted := 0
	for _, tool := range tools {
		if err := syncCtx.Err(); err != nil {
			writeErrorEnvelope(w, r, http.StatusGatewayTimeout, "mcp_sync_timeout", "upstream_error", "MCP synchronization exceeded its time limit", true, nil)
			return
		}
		if err := mcphub.ValidateInputSchema(tool.Schema); err != nil {
			if lockErr := s.lockInvalidMCPToolSchema(r, orgID, serverID, tool.Name, tool.Description, tool.Schema, sc.Actor.PrincipalID); lockErr != nil {
				mcpInternal(w, r, "failed locking invalid MCP tool schema")
				return
			}
			writeErrorEnvelope(w, r, 502, "mcp_sync_failed", "upstream_error", "upstream MCP tool schema is invalid", false, nil)
			return
		}
		hash, err := schema_pin.Hash(schema_pin.Definition{Name: tool.Name, Description: tool.Description, InputSchema: tool.Schema})
		if err != nil {
			if lockErr := s.lockInvalidMCPToolSchema(r, orgID, serverID, tool.Name, tool.Description, tool.Schema, sc.Actor.PrincipalID); lockErr != nil {
				mcpInternal(w, r, "failed locking invalid MCP tool schema")
				return
			}
			writeErrorEnvelope(w, r, 502, "mcp_sync_failed", "upstream_error", "upstream MCP tool schema is invalid", false, nil)
			return
		}
		canonical, err := audit.CanonicalizeJSON(tool.Schema)
		if err != nil {
			if lockErr := s.lockInvalidMCPToolSchema(r, orgID, serverID, tool.Name, tool.Description, tool.Schema, sc.Actor.PrincipalID); lockErr != nil {
				mcpInternal(w, r, "failed locking invalid MCP tool schema")
				return
			}
			writeErrorEnvelope(w, r, 502, "mcp_sync_failed", "upstream_error", "upstream MCP tool schema is invalid", false, nil)
			return
		}
		storedToolSchema, err := json.Marshal(map[string]any{
			"description": tool.Description,
			"inputSchema": json.RawMessage(canonical),
		})
		if err != nil {
			writeErrorEnvelope(w, r, 502, "mcp_sync_failed", "upstream_error", "upstream MCP tool schema is invalid", false, nil)
			return
		}
		var id, approved, oldObserved string
		var locked port.FlexBool
		var version int64
		tx, txErr := s.db.BeginTx(r.Context(), nil)
		if txErr != nil {
			mcpInternal(w, r, "failed starting MCP tool sync transaction")
			return
		}
		if rlsErr := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); rlsErr != nil {
			_ = tx.Rollback()
			mcpInternal(w, r, "failed setting organization scope")
			return
		}
		err = tx.QueryRowContext(r.Context(), s.rebind(`SELECT id,approved_schema_sha256,observed_schema_sha256,schema_drift_locked,version FROM mcp_tools WHERE org_id=? AND mcp_server_id=? AND tool_name=?`), orgID, serverID, tool.Name).Scan(&id, &approved, &oldObserved, &locked, &version)
		if errors.Is(err, sql.ErrNoRows) {
			id, err = idgen.New(idgen.PrefixMCP)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), s.rebind(`INSERT INTO mcp_tools(id,org_id,mcp_server_id,tool_name,side_effect_class,approved_schema_sha256,observed_schema_sha256,schema_drift_locked,tool_schema_json,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?, 'mutating',?,?,`+port.BoolExpr(s.dialect, true)+`,?,1,?,?)`), id, orgID, serverID, tool.Name, strings.Repeat("0", 64), hash, string(storedToolSchema), s.nowMs(), s.nowMs())
			}
			approved = strings.Repeat("0", 64)
			locked = true
			version = 1
			if err == nil {
				err = s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "mcp.tool_discovered", "mcp_tool", id, map[string]any{"server_id": serverID, "tool_name": tool.Name, "observed_schema_hash": hash, "approval_required": true})
			}
		} else if err == nil {
			if approved != hash && oldObserved != hash {
				locked = true
				drifted++
				_, err = tx.ExecContext(r.Context(), s.rebind(`UPDATE mcp_tools SET observed_schema_sha256=?,schema_drift_locked=`+port.BoolExpr(s.dialect, true)+`,version=version+1,tool_schema_json=?,updated_at_ms=? WHERE org_id=? AND id=?`), hash, string(storedToolSchema), s.nowMs(), orgID, id)
				version++
				if err == nil {
					err = s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "mcp.tool_schema_drift_detected", "mcp_tool", id, map[string]any{"server_id": serverID, "tool_name": tool.Name, "approved_schema_hash": approved, "observed_schema_hash": hash})
				}
			} else if approved == hash && oldObserved != hash {
				_, err = tx.ExecContext(r.Context(), s.rebind(`UPDATE mcp_tools SET observed_schema_sha256=?,tool_schema_json=?,updated_at_ms=? WHERE org_id=? AND id=?`), hash, string(storedToolSchema), s.nowMs(), orgID, id)
			}
		}
		if err != nil {
			_ = tx.Rollback()
			mcpInternal(w, r, "failed saving synchronized MCP tools")
			return
		}
		if err = tx.Commit(); err != nil {
			mcpInternal(w, r, "failed committing synchronized MCP tool")
			return
		}
		if approved != hash {
			locked = true
		}
		out = append(out, synced{id, tool.Name, hash, bool(locked), version})
	}
	writeJSON(w, 200, map[string]any{"synced_count": len(out), "drifted_count": drifted, "tools": out})
}

func (s *Server) lockInvalidMCPToolSchema(r *http.Request, orgID, serverID, toolName, description string, schema json.RawMessage, actorID string) error {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		return err
	}
	var id, observed string
	newlyDiscovered := false
	err = tx.QueryRowContext(r.Context(), s.rebind(`SELECT id,observed_schema_sha256 FROM mcp_tools WHERE org_id=? AND mcp_server_id=? AND tool_name=?`), orgID, serverID, toolName).Scan(&id, &observed)
	if errors.Is(err, sql.ErrNoRows) {
		id, err = idgen.New(idgen.PrefixMCP)
		if err != nil {
			return err
		}
		sentinel := strings.Repeat("0", 64)
		storedJSON, marshalErr := json.Marshal(map[string]any{"description": description, "inputSchema": schema})
		if marshalErr != nil {
			return marshalErr
		}
		stored := string(storedJSON)
		_, err = tx.ExecContext(r.Context(), s.rebind(`INSERT INTO mcp_tools(id,org_id,mcp_server_id,tool_name,side_effect_class,approved_schema_sha256,observed_schema_sha256,schema_drift_locked,tool_schema_json,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?, 'mutating',?,?,`+port.BoolExpr(s.dialect, true)+`,?,1,?,?)`), id, orgID, serverID, toolName, sentinel, sentinel, stored, s.nowMs(), s.nowMs())
		if err != nil {
			return err
		}
		observed = sentinel
		newlyDiscovered = true
	} else if err != nil {
		return err
	}
	if !newlyDiscovered {
		storedJSON, marshalErr := json.Marshal(map[string]any{"description": description, "inputSchema": schema})
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := tx.ExecContext(r.Context(), s.rebind(`UPDATE mcp_tools SET schema_drift_locked=`+port.BoolExpr(s.dialect, true)+`,tool_schema_json=?,version=version+1,updated_at_ms=? WHERE org_id=? AND id=?`), string(storedJSON), s.nowMs(), orgID, id); err != nil {
			return err
		}
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, actorID, "mcp.tool_schema_drift_detected", "mcp_tool", id,
		map[string]any{"server_id": serverID, "tool_name": toolName, "reason": "invalid_schema_definition", "last_observed_schema_hash": observed, "schema_hash_available": false}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) handleApproveMCPSchema(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	toolID := r.PathValue("toolId")
	var projectID, serverID, toolName string
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT srv.project_id,t.mcp_server_id,t.tool_name FROM mcp_tools t JOIN mcp_servers srv ON srv.org_id=t.org_id AND srv.id=t.mcp_server_id WHERE t.org_id=? AND t.id=?`), orgID, toolID).Scan(&projectID, &serverID, &toolName)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "MCP tool not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading MCP tool")
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPSchemaApprove, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	expected, ok := requireIfMatch(w, r)
	if !ok {
		return
	}
	var req struct {
		Expected      string `json:"expected_schema_hash"`
		Risk          string `json:"risk_tier"`
		Justification string `json:"justification"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if len(req.Expected) != 64 || !isLowerHex(req.Expected) || (req.Risk != "read_only" && req.Risk != "mutating" && req.Risk != "destructive") {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "expected_schema_hash or risk_tier is invalid", false, nil)
		return
	}
	var observed string
	var schemaJSON string
	var version int64
	err = s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT observed_schema_sha256,version,tool_schema_json FROM mcp_tools WHERE org_id=? AND id=?`), orgID, toolID).Scan(&observed, &version, &schemaJSON)
	})
	if err != nil {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "MCP tool not found", false, nil)
		return
	}
	var stored struct {
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if json.Unmarshal([]byte(schemaJSON), &stored) != nil || mcphub.ValidateInputSchema(stored.InputSchema) != nil {
		invalidSchema := stored.InputSchema
		if len(invalidSchema) == 0 {
			invalidSchema = json.RawMessage(`{}`)
		}
		if lockErr := s.lockInvalidMCPToolSchema(r, orgID, serverID, toolName, stored.Description, invalidSchema, sc.Actor.PrincipalID); lockErr != nil {
			mcpInternal(w, r, "failed locking invalid MCP tool schema")
			return
		}
		writeErrorEnvelope(w, r, 409, "mcp_schema_invalid", "conflict_error", "invalid MCP tool schemas cannot be approved", false, nil)
		return
	}
	if version != expected {
		writeOptimisticConflict(w, r, version, map[string]any{"id": toolID, "version": version})
		return
	}
	if observed != req.Expected {
		writeErrorEnvelope(w, r, 409, "schema_hash_mismatch", "conflict_error", "expected_schema_hash does not match currently observed schema", false, map[string]any{"observed_schema_hash": observed})
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		mcpInternal(w, r, "failed starting MCP schema approval transaction")
		return
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}
	res, err := tx.ExecContext(r.Context(), s.rebind(`UPDATE mcp_tools SET approved_schema_sha256=observed_schema_sha256,schema_drift_locked=`+port.BoolExpr(s.dialect, false)+`,side_effect_class=?,version=version+1,updated_at_ms=? WHERE org_id=? AND id=? AND version=? AND observed_schema_sha256=?`), req.Risk, s.nowMs(), orgID, toolID, expected, req.Expected)
	if err != nil {
		mcpInternal(w, r, "failed approving MCP schema")
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		writeErrorEnvelope(w, r, 409, "optimistic_lock_conflict", "conflict_error", "MCP tool changed during approval", true, nil)
		return
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "mcp.tool_schema_approved", "mcp_tool", toolID, map[string]any{"risk_tier": req.Risk, "schema_hash": req.Expected, "justification": req.Justification}); err != nil {
		mcpInternal(w, r, "failed recording MCP schema approval audit event")
		return
	}
	if err := tx.Commit(); err != nil {
		mcpInternal(w, r, "failed committing MCP schema approval")
		return
	}
	w.Header().Set("ETag", FormatETag(expected+1))
	writeJSON(w, 200, map[string]any{"id": toolID, "approved_schema_hash": req.Expected, "observed_schema_hash": req.Expected, "side_effect_class": req.Risk, "schema_drift_locked": false, "version": expected + 1})
}

type mcpToolGrantDTO struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	EnvironmentID string `json:"env_id"`
	VirtualKeyID  string `json:"virtual_key_id"`
	Version       int64  `json:"version"`
	CreatedAtMs   int64  `json:"created_at_ms"`
}

func (s *Server) handleListMCPToolGrants(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	toolID := strings.TrimSpace(r.PathValue("toolId"))
	var projectID string
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT s.project_id FROM mcp_tools t JOIN mcp_servers s ON s.org_id=t.org_id AND s.id=t.mcp_server_id WHERE t.org_id=? AND t.id=?`), orgID, toolID).Scan(&projectID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "MCP tool not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading MCP tool")
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPRead, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	grants := make([]mcpToolGrantDTO, 0)
	scanFailed := false
	err = s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		rows, err := dbq.QueryContext(r.Context(), s.rebind(`SELECT id,project_id,env_id,virtual_key_id,version,created_at_ms FROM mcp_tool_grants WHERE org_id=? AND project_id=? AND mcp_tool_id=? ORDER BY env_id,virtual_key_id,id`), orgID, projectID, toolID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var grant mcpToolGrantDTO
			if err := rows.Scan(&grant.ID, &grant.ProjectID, &grant.EnvironmentID, &grant.VirtualKeyID, &grant.Version, &grant.CreatedAtMs); err != nil {
				scanFailed = true
				return err
			}
			grants = append(grants, grant)
		}
		return rows.Err()
	})
	if scanFailed {
		mcpInternal(w, r, "failed reading MCP tool grants")
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed listing MCP tool grants")
		return
	}
	writeJSON(w, 200, map[string]any{"data": grants})
}

func (s *Server) handleCreateMCPToolGrant(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	toolID := strings.TrimSpace(r.PathValue("toolId"))
	var projectID, serverSlug, toolName string
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT s.project_id,s.server_slug,t.tool_name FROM mcp_tools t JOIN mcp_servers s ON s.org_id=t.org_id AND s.id=t.mcp_server_id WHERE t.org_id=? AND t.id=?`), orgID, toolID).Scan(&projectID, &serverSlug, &toolName)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "MCP tool not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading MCP tool")
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	var req struct {
		EnvironmentID string `json:"env_id"`
		VirtualKeyID  string `json:"virtual_key_id"`
	}
	if !s.decodeJSONAndCheckOrgSpoofing(w, r, sc, &req) {
		return
	}
	if req.EnvironmentID == "" || req.VirtualKeyID == "" {
		writeErrorEnvelope(w, r, 400, "invalid_request", "invalid_request_error", "env_id and virtual_key_id are required", false, nil)
		return
	}
	var exists int
	if err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT 1 FROM environments WHERE org_id=? AND project_id=? AND id=?`), orgID, projectID, req.EnvironmentID).Scan(&exists)
	}); err != nil {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "environment not found", false, nil)
		return
	}
	var planes, allowedJSON, state string
	var expires sql.NullInt64
	err = s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT allowed_planes,allowed_mcp_tools_json,state,expires_at_ms FROM virtual_keys WHERE org_id=? AND project_id=? AND env_id=? AND id=?`), orgID, projectID, req.EnvironmentID, req.VirtualKeyID).Scan(&planes, &allowedJSON, &state, &expires)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "virtual key not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading virtual key MCP permissions")
		return
	}
	var allowedTools []string
	if json.Unmarshal([]byte(allowedJSON), &allowedTools) != nil || !containsPlane(planes, "MCP") || (state != "CREATED_ACTIVE" && state != "ROTATING_DUAL_VALID") || (expires.Valid && expires.Int64 <= s.nowMs()) || !containsString(allowedTools, serverSlug+"__"+toolName) {
		writeErrorEnvelope(w, r, 409, "mcp_tool_not_allowed_by_key", "conflict_error", "virtual key must be active, allow the MCP plane, and explicitly include this namespaced tool", false, nil)
		return
	}
	id, err := idgen.New(idgen.PrefixMCP)
	if err != nil {
		mcpInternal(w, r, "failed generating MCP tool grant id")
		return
	}
	now := s.nowMs()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		mcpInternal(w, r, "failed starting MCP grant transaction")
		return
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}
	if _, err := tx.ExecContext(r.Context(), s.rebind(`INSERT INTO mcp_tool_grants(id,org_id,project_id,env_id,virtual_key_id,mcp_tool_id,granted_by_principal_id,version,created_at_ms) VALUES(?,?,?,?,?,?,?,1,?)`), id, orgID, projectID, req.EnvironmentID, req.VirtualKeyID, toolID, sc.Actor.PrincipalID, now); err != nil {
		if isMCPUniqueErr(err) {
			writeErrorEnvelope(w, r, 409, "resource_conflict", "conflict_error", "MCP tool grant already exists", false, nil)
		} else {
			mcpInternal(w, r, "failed creating MCP tool grant")
		}
		return
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "mcp.tool_granted", "mcp_tool", toolID, map[string]string{"grant_id": id, "virtual_key_id": req.VirtualKeyID, "env_id": req.EnvironmentID}); err != nil {
		mcpInternal(w, r, "failed recording MCP tool grant audit event")
		return
	}
	if err := tx.Commit(); err != nil {
		mcpInternal(w, r, "failed committing MCP tool grant")
		return
	}
	w.Header().Set("ETag", FormatETag(1))
	writeJSON(w, http.StatusCreated, mcpToolGrantDTO{ID: id, ProjectID: projectID, EnvironmentID: req.EnvironmentID, VirtualKeyID: req.VirtualKeyID, Version: 1, CreatedAtMs: now})
}

func (s *Server) handleDeleteMCPToolGrant(w http.ResponseWriter, r *http.Request) {
	sc, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	orgID, ok := s.resolveTargetOrgID(w, r, sc)
	if !ok {
		return
	}
	grantID := strings.TrimSpace(r.PathValue("grantId"))
	var projectID, toolID string
	err := s.withOrgReadTx(r.Context(), orgID, func(dbq adminDBQuerier) error {
		return dbq.QueryRowContext(r.Context(), s.rebind(`SELECT project_id,mcp_tool_id FROM mcp_tool_grants WHERE org_id=? AND id=?`), orgID, grantID).Scan(&projectID, &toolID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		writeErrorEnvelope(w, r, 404, "resource_not_found", "not_found_error", "MCP tool grant not found", false, nil)
		return
	}
	if err != nil {
		mcpInternal(w, r, "failed loading MCP tool grant")
		return
	}
	if !s.authorize(w, r, sc, rbac.ActionMCPWrite, rbac.TargetScope{OrgID: orgID, ProjectID: projectID}) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		mcpInternal(w, r, "failed starting MCP grant deletion transaction")
		return
	}
	defer tx.Rollback()
	if err := port.SetOrgRLSTx(r.Context(), tx, s.dialect, orgID); err != nil {
		mcpInternal(w, r, "failed setting organization scope")
		return
	}
	if _, err := tx.ExecContext(r.Context(), s.rebind(`DELETE FROM mcp_tool_grants WHERE org_id=? AND id=?`), orgID, grantID); err != nil {
		mcpInternal(w, r, "failed deleting MCP tool grant")
		return
	}
	if err := s.appendMCPAuditTx(r, tx, orgID, sc.Actor.PrincipalID, "mcp.tool_grant_revoked", "mcp_tool", toolID, map[string]string{"grant_id": grantID}); err != nil {
		mcpInternal(w, r, "failed recording MCP grant revocation audit event")
		return
	}
	if err := tx.Commit(); err != nil {
		mcpInternal(w, r, "failed committing MCP grant revocation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func containsPlane(planes, wanted string) bool {
	for _, plane := range strings.Split(strings.ToUpper(planes), ",") {
		if strings.TrimSpace(plane) == wanted {
			return true
		}
	}
	return false
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type syncServer struct {
	ID, OrgID, ProjectID, Slug, Transport, Endpoint string
	Private                                         bool
	CIDRs                                           string
	Credential                                      string
}
type upstreamMCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"inputSchema"`
}

func (s *Server) fetchMCPTools(ctx context.Context, srv syncServer) ([]upstreamMCPTool, error) {
	parsed, err := url.Parse(srv.Endpoint)
	if err != nil {
		return nil, err
	}
	portNum := 443
	if parsed.Scheme == "http" {
		portNum = 80
	}
	if parsed.Port() != "" {
		fmt.Sscan(parsed.Port(), &portNum)
	}
	var cidrs []string
	if json.Unmarshal([]byte(srv.CIDRs), &cidrs) != nil {
		return nil, errors.New("invalid cidrs")
	}
	policy := &egress.Policy{AllowedHosts: []string{parsed.Host}, AllowPrivateIP: srv.Private, AllowedCIDRs: cidrs, AllowedPorts: []int{portNum}, RequireTLS: parsed.Scheme == "https", RequirePrivateForHTTP: parsed.Scheme == "http"}
	send := func(id int, method string, params any, session string, notification bool) (json.RawMessage, string, error) {
		request := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if !notification {
			request["id"] = id
		}
		body, _ := json.Marshal(request)
		headers := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
		if session != "" {
			headers["Mcp-Session-Id"] = session
		}
		var injections []broker.CredentialInjection
		if srv.Credential != "" {
			injections = []broker.CredentialInjection{{SecretSlotName: "mcp_server", Location: broker.TargetBearer}}
		}
		resp, err := s.broker.ExecuteStreamWithHostConstraint(ctx, broker.EgressRequest{OrgID: srv.OrgID, ConnectionID: srv.ID, Method: http.MethodPost, TargetURL: srv.Endpoint, SafeHeaders: headers, BodyBytes: body, CredentialInjections: injections, ActorKind: "SYSTEM", ActorID: "admin_mcp_sync", EntryPoint: "control_plane"}, broker.HostEgressConstraint{NarrowPolicy: policy})
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
		if err != nil || len(raw) > 4<<20 {
			return nil, "", errors.New("MCP upstream response too large")
		}
		if notification {
			if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
				return nil, "", fmt.Errorf("notification returned HTTP %d", resp.StatusCode)
			}
			return nil, resp.SanitizedHeaders.Get("Mcp-Session-Id"), nil
		}
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("upstream HTTP %d", resp.StatusCode)
		}
		if strings.Contains(strings.ToLower(resp.SanitizedHeaders.Get("Content-Type")), "text/event-stream") {
			raw, err = lastMCPEvent(raw)
			if err != nil {
				return nil, "", err
			}
		}
		if err := mcphub.ValidateMCPResponseJSON(raw); err != nil {
			return nil, "", errors.New("invalid or overdeep MCP response JSON")
		}
		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Result  json.RawMessage `json:"result"`
			Error   json.RawMessage `json:"error"`
		}
		var gotID int
		if json.Unmarshal(raw, &envelope) != nil || envelope.JSONRPC != "2.0" || json.Unmarshal(envelope.ID, &gotID) != nil || gotID != id || len(envelope.Error) > 0 || len(envelope.Result) == 0 {
			return nil, "", errors.New("invalid MCP response")
		}
		return envelope.Result, resp.SanitizedHeaders.Get("Mcp-Session-Id"), nil
	}
	init, session, err := send(1, "initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "tanpopo-admin", "version": "0.1"}}, "", false)
	if err != nil {
		return nil, err
	}
	var initResult map[string]any
	if json.Unmarshal(init, &initResult) != nil {
		return nil, errors.New("invalid initialize result")
	}
	_, _, err = send(0, "notifications/initialized", map[string]any{}, session, true)
	if err != nil {
		return nil, err
	}
	var all []upstreamMCPTool
	cursor := ""
	catalogBytes := 0
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, _, err := send(page+3, "tools/list", params, session, false)
		if err != nil {
			return nil, err
		}
		catalogBytes, err = schema_pin.AddCatalogPageBytes(catalogBytes, len(result))
		if err != nil {
			return nil, errors.New("MCP tool catalog exceeds byte limit")
		}
		var pageResult struct {
			Tools []upstreamMCPTool `json:"tools"`
			Next  string            `json:"nextCursor"`
		}
		if json.Unmarshal(result, &pageResult) != nil {
			return nil, errors.New("invalid tools/list result")
		}
		all = append(all, pageResult.Tools...)
		if len(all) > 10000 {
			return nil, errors.New("tool limit exceeded")
		}
		if pageResult.Next == "" {
			return all, nil
		}
		if pageResult.Next == cursor {
			return nil, errors.New("cursor did not advance")
		}
		cursor = pageResult.Next
	}
	return nil, errors.New("pagination limit exceeded")
}

func lastMCPEvent(raw []byte) ([]byte, error) {
	var last []byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			last = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		}
	}
	if len(last) == 0 {
		return nil, errors.New("empty SSE response")
	}
	return last, nil
}
func validMCPSlug(v string) bool {
	if len(v) < 1 || len(v) > 64 {
		return false
	}
	for i, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || i > 0 && c == '-') {
			return false
		}
	}
	return true
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func mcpInternal(w http.ResponseWriter, r *http.Request, msg string) {
	writeErrorEnvelope(w, r, 500, "internal_error", "upstream_error", msg, true, nil)
}
func (s *Server) appendMCPAuditTx(r *http.Request, tx *sql.Tx, orgID, actorID, event, targetType, targetID string, metadata any) error {
	if s.txAudit == nil {
		return errors.New("transactional audit sink unavailable")
	}
	id, err := idgen.New(idgen.PrefixAudit)
	if err != nil {
		return err
	}
	var raw string
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		raw = string(b)
	}
	return s.txAudit.AppendTx(r.Context(), tx, port.AuditEvent{ID: id, CreatedAtMs: s.nowMs(), OrgID: &orgID, ActorKind: "PRINCIPAL", ActorID: actorID, EntryPoint: port.AuditEntryPointAdminAPI, EventType: event, TargetType: targetType, TargetID: targetID, RequestID: ptrOrNil(requestIDFromContext(r.Context())), MetadataJSON: raw})
}
func isLowerHex(v string) bool {
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
