// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
)

func TestMCPGovernanceAdminRegistrationApprovalAndDefaultDeny(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "MCP Governance", "mcp-project")
	register := func(body map[string]any, csrf string) *httptest.ResponseRecorder {
		return h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers", cookie, csrf, "", body)
	}
	base := map[string]any{"project_id": boot.ProjectID, "slug": "tools-server", "name": "Tools", "transport": "streamable_http", "endpoint_url": "https://mcp.example.test/mcp"}
	if got := register(base, ""); got.Code != http.StatusForbidden {
		t.Fatalf("registration without CSRF status=%d body=%s", got.Code, got.Body.String())
	}
	badSlug := map[string]any{}
	for k, v := range base {
		badSlug[k] = v
	}
	badSlug["slug"] = "tools_server"
	if got := register(badSlug, boot.CSRFToken); got.Code != http.StatusBadRequest {
		t.Fatalf("underscore slug status=%d body=%s", got.Code, got.Body.String())
	}
	badTransport := map[string]any{}
	for k, v := range base {
		badTransport[k] = v
	}
	badTransport["transport"] = "isolated_sidecar"
	if got := register(badTransport, boot.CSRFToken); got.Code != http.StatusBadRequest {
		t.Fatalf("sidecar transport status=%d body=%s", got.Code, got.Body.String())
	}
	created := register(base, boot.CSRFToken)
	if created.Code != http.StatusCreated {
		t.Fatalf("registration status=%d body=%s", created.Code, created.Body.String())
	}
	var server mcpServerDTO
	mustDecodeJSON(t, created.Body.Bytes(), &server)
	if server.Transport != "streamable_http" || created.Header().Get("ETag") != `"v1"` {
		t.Fatalf("unexpected server contract: %+v etag=%s", server, created.Header().Get("ETag"))
	}
	listed := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/mcp/servers", cookie, "", "", nil)
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), server.ID) || !strings.Contains(listed.Body.String(), `"name":"Tools"`) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	observed := strings.Repeat("b", 64)
	_, err := h.st.RawDB().Exec(`INSERT INTO mcp_tools(id,org_id,mcp_server_id,tool_name,side_effect_class,approved_schema_sha256,observed_schema_sha256,schema_drift_locked,tool_schema_json,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "mcp_tool_test", boot.OrgID, server.ID, "lookup", "mutating", strings.Repeat("0", 64), observed, 1, `{"description":"lookup","inputSchema":{"type":"object"}}`, 1, h.fk.Now().UnixMilli(), h.fk.Now().UnixMilli())
	if err != nil {
		t.Fatalf("seed MCP tool: %v", err)
	}
	path := "/admin/v1/mcp/tools/mcp_tool_test/approve-schema"
	approval := map[string]any{"expected_schema_hash": observed, "risk_tier": "read_only"}
	if got := h.doAuthedCookie(t, http.MethodPost, path, cookie, boot.CSRFToken, "", approval); got.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match status=%d body=%s", got.Code, got.Body.String())
	}
	stale := h.doAuthedCookie(t, http.MethodPost, path, cookie, boot.CSRFToken, `"v8"`, approval)
	if stale.Code != http.StatusConflict || stale.Header().Get("ETag") != `"v1"` {
		t.Fatalf("stale If-Match status=%d etag=%s body=%s", stale.Code, stale.Header().Get("ETag"), stale.Body.String())
	}
	approved := h.doAuthedCookie(t, http.MethodPost, path, cookie, boot.CSRFToken, `"v1"`, approval)
	if approved.Code != 200 || approved.Header().Get("ETag") != `"v2"` {
		t.Fatalf("approval status=%d etag=%s body=%s", approved.Code, approved.Header().Get("ETag"), approved.Body.String())
	}
	var approvedHash string
	var locked int
	var version int64
	if err := h.st.RawDB().QueryRow(`SELECT approved_schema_sha256,schema_drift_locked,version FROM mcp_tools WHERE org_id=? AND id=?`, boot.OrgID, "mcp_tool_test").Scan(&approvedHash, &locked, &version); err != nil {
		t.Fatal(err)
	}
	if approvedHash != observed || locked != 0 || version != 2 {
		t.Fatalf("approval persistence hash=%s locked=%d version=%d", approvedHash, locked, version)
	}
	key, err := h.vkeys.Create(t.Context(), vkey.CreateParams{
		OrgID: boot.OrgID, ProjectID: boot.ProjectID, EnvID: boot.EnvID,
		OwnerPrincipalID: &boot.OwnerPrincipalID, Name: "mcp-grant-test", AllowedPlanes: "MCP",
		AllowedMCPTools: []string{"tools-server__lookup"}, ActorID: boot.OwnerPrincipalID,
	})
	if err != nil {
		t.Fatalf("create MCP-scoped virtual key: %v", err)
	}
	deniedKey, err := h.vkeys.Create(t.Context(), vkey.CreateParams{
		OrgID: boot.OrgID, ProjectID: boot.ProjectID, EnvID: boot.EnvID,
		OwnerPrincipalID: &boot.OwnerPrincipalID, Name: "mcp-grant-denied", AllowedPlanes: "PROVIDER",
		ActorID: boot.OwnerPrincipalID,
	})
	if err != nil {
		t.Fatalf("create non-MCP virtual key: %v", err)
	}
	denied := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/tools/mcp_tool_test/grants", cookie, boot.CSRFToken, "", map[string]any{"env_id": boot.EnvID, "virtual_key_id": deniedKey.Summary.ID})
	if denied.Code != http.StatusConflict {
		t.Fatalf("grant to a key without MCP/tool allowlist status=%d body=%s", denied.Code, denied.Body.String())
	}
	grantBody := map[string]any{"env_id": boot.EnvID, "virtual_key_id": key.Summary.ID}
	grantResponse := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/tools/mcp_tool_test/grants", cookie, boot.CSRFToken, "", grantBody)
	if grantResponse.Code != http.StatusCreated {
		t.Fatalf("create tool grant status=%d body=%s", grantResponse.Code, grantResponse.Body.String())
	}
	var grant mcpToolGrantDTO
	mustDecodeJSON(t, grantResponse.Body.Bytes(), &grant)
	if grant.VirtualKeyID != key.Summary.ID || grant.EnvironmentID != boot.EnvID {
		t.Fatalf("grant scope mismatch: %+v", grant)
	}
	var storedProject, storedEnv, storedKey string
	if err := h.st.RawDB().QueryRow(`SELECT project_id,env_id,virtual_key_id FROM mcp_tool_grants WHERE org_id=? AND id=?`, boot.OrgID, grant.ID).Scan(&storedProject, &storedEnv, &storedKey); err != nil {
		t.Fatal(err)
	}
	if storedProject != boot.ProjectID || storedEnv != boot.EnvID || storedKey != key.Summary.ID {
		t.Fatalf("persisted grant crossed scope: project=%s env=%s key=%s", storedProject, storedEnv, storedKey)
	}
	grantList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/mcp/tools/mcp_tool_test/grants", cookie, "", "", nil)
	if grantList.Code != http.StatusOK || !strings.Contains(grantList.Body.String(), grant.ID) {
		t.Fatalf("list tool grants status=%d body=%s", grantList.Code, grantList.Body.String())
	}
	grantDelete := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/mcp/grants/"+grant.ID, cookie, boot.CSRFToken, "", nil)
	if grantDelete.Code != http.StatusNoContent {
		t.Fatalf("revoke tool grant status=%d body=%s", grantDelete.Code, grantDelete.Body.String())
	}
	grantList = h.doAuthedCookie(t, http.MethodGet, "/admin/v1/mcp/tools/mcp_tool_test/grants", cookie, "", "", nil)
	if grantList.Code != http.StatusOK || strings.Contains(grantList.Body.String(), grant.ID) {
		t.Fatalf("revoked grant remained visible: status=%d body=%s", grantList.Code, grantList.Body.String())
	}
	var grants int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM mcp_tool_grants WHERE org_id=? AND mcp_tool_id=?`, boot.OrgID, "mcp_tool_test").Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 0 {
		t.Fatalf("schema approval unexpectedly created %d grants", grants)
	}
}

type mcpSyncTestResolver struct{ policy *egress.Policy }

func (mcpSyncTestResolver) ResolveSlot(context.Context, string, string, string) (*broker.ResolvedSlot, error) {
	return nil, nil
}

func (r mcpSyncTestResolver) ResolveConnectionPolicy(context.Context, string, string) (*egress.Policy, error) {
	return r.policy, nil
}

type mcpSyncTestHostResolver struct{}

func (mcpSyncTestHostResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host != "127.0.0.1" {
		return nil, fmt.Errorf("unexpected host lookup: %s", host)
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestMCPToolSyncUsesBrokerNotificationAndLeavesToolsLocked(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "MCP Sync", "sync-project")
	var initializedNotification bool
	toolName := "lookup"
	schemaJSON := `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			if req["jsonrpc"] != "2.0" || req["id"] != float64(1) {
				http.Error(w, "bad init request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Mcp-Session-Id", "mcp-session-test")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`))
		case "notifications/initialized":
			_, hasID := req["id"]
			initializedNotification = req["jsonrpc"] == "2.0" && !hasID
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if req["jsonrpc"] != "2.0" || req["id"] != float64(3) || r.Header.Get("Mcp-Session-Id") != "mcp-session-test" {
				http.Error(w, "bad tools request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":%q,"description":"lookup item","inputSchema":%s}]}}`, toolName, schemaJSON)))
		default:
			http.Error(w, "unexpected MCP method", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()

	portNum := upstream.Listener.Addr().(*net.TCPAddr).Port
	cidr := "127.0.0.0/8"
	policy := &egress.Policy{AllowedHosts: []string{"127.0.0.1"}, AllowPrivateIP: true,
		AllowedCIDRs: []string{cidr}, AllowedPorts: []int{portNum}}
	brk, err := broker.NewBlindBroker(broker.Config{
		Resolver: mcpSyncTestResolver{policy: policy}, PolicyResolver: mcpSyncTestResolver{policy: policy},
		Vault: h.vault, UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: h.auditLog,
		NowMs: func() int64 { return h.fk.Now().UnixMilli() }, HostResolver: mcpSyncTestHostResolver{},
		DefaultTimeout: time.Second, MaxTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv.broker = brk
	base := map[string]any{"project_id": boot.ProjectID, "slug": "sync-server", "name": "Sync",
		"transport": "streamable_http", "endpoint_url": upstream.URL, "allow_private_ip": true,
		"allowed_cidrs": []string{cidr}}
	created := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers", cookie, boot.CSRFToken, "", base)
	if created.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", created.Code, created.Body.String())
	}
	var server mcpServerDTO
	mustDecodeJSON(t, created.Body.Bytes(), &server)
	synced := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers/"+server.ID+"/sync-tools", cookie, boot.CSRFToken, "", nil)
	if synced.Code != http.StatusOK {
		t.Fatalf("sync status=%d body=%s", synced.Code, synced.Body.String())
	}
	if !initializedNotification {
		t.Fatal("notifications/initialized was not sent as a JSON-RPC notification")
	}
	var result struct {
		Synced int `json:"synced_count"`
		Tools  []struct {
			ID          string `json:"id"`
			DriftLocked bool   `json:"drift_locked"`
		} `json:"tools"`
	}
	mustDecodeJSON(t, synced.Body.Bytes(), &result)
	if result.Synced != 1 || len(result.Tools) != 1 || !result.Tools[0].DriftLocked {
		t.Fatalf("new tool was not left locked: %+v", result)
	}
	var storedSchema string
	if err := h.st.RawDB().QueryRow(`SELECT tool_schema_json FROM mcp_tools WHERE org_id=? AND id=?`, boot.OrgID, result.Tools[0].ID).Scan(&storedSchema); err != nil {
		t.Fatal(err)
	}
	var storedDefinition struct {
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal([]byte(storedSchema), &storedDefinition); err != nil || storedDefinition.Description != "lookup item" || len(storedDefinition.InputSchema) == 0 {
		t.Fatalf("sync did not persist the complete tool definition: %s (%v)", storedSchema, err)
	}
	var approved, observed string
	var locked int
	if err := h.st.RawDB().QueryRow(`SELECT approved_schema_sha256,observed_schema_sha256,schema_drift_locked FROM mcp_tools WHERE org_id=? AND id=?`, boot.OrgID, result.Tools[0].ID).Scan(&approved, &observed, &locked); err != nil {
		t.Fatal(err)
	}
	if approved == observed || approved != strings.Repeat("0", 64) || locked != 1 {
		t.Fatalf("new tool approval defaults unsafe: approved=%s observed=%s locked=%d", approved, observed, locked)
	}
	var grants int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM mcp_tool_grants WHERE org_id=? AND mcp_tool_id=?`, boot.OrgID, result.Tools[0].ID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 0 {
		t.Fatalf("sync created default grant count=%d", grants)
	}
	if _, err := h.st.RawDB().Exec(`UPDATE mcp_tools SET approved_schema_sha256=observed_schema_sha256,schema_drift_locked=0 WHERE org_id=? AND id=?`, boot.OrgID, result.Tools[0].ID); err != nil {
		t.Fatal(err)
	}
	schemaJSON = `{"type":"object","type":"string"}`
	invalidSync := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers/"+server.ID+"/sync-tools", cookie, boot.CSRFToken, "", nil)
	if invalidSync.Code != http.StatusBadGateway {
		t.Fatalf("invalid schema sync status=%d body=%s", invalidSync.Code, invalidSync.Body.String())
	}
	if err := h.st.RawDB().QueryRow(`SELECT schema_drift_locked FROM mcp_tools WHERE org_id=? AND id=?`, boot.OrgID, result.Tools[0].ID).Scan(&locked); err != nil || locked != 1 {
		t.Fatalf("invalid schema sync did not lock approved tool: locked=%d err=%v", locked, err)
	}
	var auditCount int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE org_id=? AND target_id=? AND event_type='mcp.tool_schema_drift_detected' AND metadata_json LIKE '%invalid_schema_definition%'`, boot.OrgID, result.Tools[0].ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("invalid schema sync audit count=%d err=%v", auditCount, err)
	}
	repeatedSync := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers/"+server.ID+"/sync-tools", cookie, boot.CSRFToken, "", nil)
	if repeatedSync.Code != http.StatusBadGateway {
		t.Fatalf("repeated invalid schema sync status=%d", repeatedSync.Code)
	}
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE org_id=? AND target_id=? AND event_type='mcp.tool_schema_drift_detected' AND metadata_json LIKE '%invalid_schema_definition%'`, boot.OrgID, result.Tools[0].ID).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("repeated invalid schema observation was not audited: count=%d err=%v", auditCount, err)
	}
	toolName = "new-invalid"
	schemaJSON = `{"type":4}`
	newInvalidSync := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers/"+server.ID+"/sync-tools", cookie, boot.CSRFToken, "", nil)
	if newInvalidSync.Code != http.StatusBadGateway {
		t.Fatalf("new invalid tool sync status=%d", newInvalidSync.Code)
	}
	var newID string
	if err := h.st.RawDB().QueryRow(`SELECT id FROM mcp_tools WHERE org_id=? AND mcp_server_id=? AND tool_name=?`, boot.OrgID, server.ID, toolName).Scan(&newID); err != nil {
		t.Fatalf("new invalid tool was not recorded fail-closed: %v", err)
	}
	if err := h.st.RawDB().QueryRow(`SELECT schema_drift_locked FROM mcp_tools WHERE org_id=? AND id=?`, boot.OrgID, newID).Scan(&locked); err != nil || locked != 1 {
		t.Fatalf("new invalid tool was not locked: locked=%d err=%v", locked, err)
	}
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE org_id=? AND target_id=? AND event_type='mcp.tool_schema_drift_detected' AND metadata_json LIKE '%invalid_schema_definition%'`, boot.OrgID, newID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("new invalid tool discovery was not audited: count=%d err=%v", auditCount, err)
	}
	invalidApproval := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/tools/"+newID+"/approve-schema", cookie, boot.CSRFToken, `"v1"`, map[string]any{"expected_schema_hash": strings.Repeat("0", 64), "risk_tier": "read_only"})
	if invalidApproval.Code != http.StatusConflict || !strings.Contains(invalidApproval.Body.String(), "mcp_schema_invalid") {
		t.Fatalf("structurally invalid schema approval status=%d body=%s", invalidApproval.Code, invalidApproval.Body.String())
	}
	if err := h.st.RawDB().QueryRow(`SELECT schema_drift_locked FROM mcp_tools WHERE org_id=? AND id=?`, boot.OrgID, newID).Scan(&locked); err != nil || locked != 1 {
		t.Fatalf("invalid schema approval did not preserve the lock: locked=%d err=%v", locked, err)
	}
}
