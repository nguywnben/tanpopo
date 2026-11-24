// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
	mcphub "github.com/nguywnben/tanpopo/internal/ingress/mcp/hub"
	"github.com/nguywnben/tanpopo/internal/security/broker"
	"github.com/nguywnben/tanpopo/internal/security/egress"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
	"github.com/nguywnben/tanpopo/internal/webhook"
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

func TestCT36PublicMCPWebhookAdminApprovalAndExactReplay(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "CT36 end to end", "ct36-project")
	toolSchema := `{"type":"object","properties":{"document_id":{"type":"string"}},"required":["document_id"]}`
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
			return
		}
		switch request.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "ct36-upstream")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`, request.ID)
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"delete_document","description":"Delete a document","inputSchema":%s}]}}`, request.ID, toolSchema)
		case "tools/call":
			upstreamCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"deleted"}]}}`, request.ID)
		default:
			http.Error(w, "unexpected MCP method", http.StatusBadRequest)
		}
	}))
	defer upstream.Close()
	h.srv.hostResolver = mcpSyncTestHostResolver{}
	resolver := mcphub.BrokerResolver{DB: h.st.RawDB()}
	brokerService, err := broker.NewBlindBroker(broker.Config{
		Resolver: resolver, PolicyResolver: resolver, Vault: h.vault,
		UnsealCap: vault.MintUnsealCapabilityForBroker(), Audit: h.auditLog,
		HostResolver: mcpSyncTestHostResolver{}, DefaultTimeout: time.Second, MaxTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.srv.broker = brokerService
	register := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers", cookie, boot.CSRFToken, "", map[string]any{
		"project_id": boot.ProjectID, "slug": "ct36-tools", "name": "CT36 tools", "transport": "streamable_http",
		"endpoint_url": upstream.URL, "allow_private_ip": true, "allowed_cidrs": []string{"127.0.0.0/8"},
	})
	if register.Code != http.StatusCreated {
		t.Fatalf("MCP server registration: %d %s", register.Code, register.Body.String())
	}
	var server mcpServerDTO
	mustDecodeJSON(t, register.Body.Bytes(), &server)
	synced := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers/"+server.ID+"/sync-tools", cookie, boot.CSRFToken, "", nil)
	if synced.Code != http.StatusOK {
		t.Fatalf("sync MCP tools: %d %s", synced.Code, synced.Body.String())
	}
	var syncedTools struct {
		Tools []struct {
			ID string `json:"id"`
		} `json:"tools"`
	}
	mustDecodeJSON(t, synced.Body.Bytes(), &syncedTools)
	if len(syncedTools.Tools) != 1 {
		t.Fatalf("unexpected synced tools: %s", synced.Body.String())
	}
	var observed string
	if err := h.st.RawDB().QueryRow(`SELECT observed_schema_sha256 FROM mcp_tools WHERE id=?`, syncedTools.Tools[0].ID).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	approved := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/tools/"+syncedTools.Tools[0].ID+"/approve-schema", cookie, boot.CSRFToken, FormatETag(1), map[string]any{"expected_schema_hash": observed, "risk_tier": "destructive"})
	if approved.Code != http.StatusOK {
		t.Fatalf("approve destructive schema: %d %s", approved.Code, approved.Body.String())
	}
	key, err := h.vkeys.Create(t.Context(), vkey.CreateParams{OrgID: boot.OrgID, ProjectID: boot.ProjectID, EnvID: boot.EnvID,
		OwnerPrincipalID: &boot.OwnerPrincipalID, Name: "ct36-mcp", AllowedPlanes: "MCP", AllowedMCPTools: []string{"ct36-tools__delete_document"}, ActorID: boot.OwnerPrincipalID})
	if err != nil {
		t.Fatal(err)
	}
	grant := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/tools/"+syncedTools.Tools[0].ID+"/grants", cookie, boot.CSRFToken, "", map[string]any{"env_id": boot.EnvID, "virtual_key_id": key.Summary.ID})
	if grant.Code != http.StatusCreated {
		t.Fatalf("grant MCP tool: %d %s", grant.Code, grant.Body.String())
	}
	receiverBody := make(chan []byte, 4)
	receiverSignature := make(chan string, 4)
	receiverTimestamp := make(chan string, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receiverBody <- body
		receiverSignature <- r.Header.Get("X-Tanpopo-Signature-256")
		receiverTimestamp <- r.Header.Get("X-Tanpopo-Timestamp")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	origin := receiver.URL
	macKey := []byte("ct36-webhook-signing-key")
	macAudience := origin
	signing, err := h.vault.SealSecret(t.Context(), vault.SealSecretParams{OrgID: boot.OrgID, OwnerProjectID: &boot.ProjectID,
		SecretKind: vault.SecretKindWebhookHMAC, BoundAudience: &macAudience, Name: "CT36 signing key", Plaintext: macKey})
	if err != nil {
		t.Fatal(err)
	}
	targetURL := origin + "/approval"
	targetAudience := targetURL
	targetSecret, err := h.vault.SealSecret(t.Context(), vault.SealSecretParams{OrgID: boot.OrgID, OwnerProjectID: &boot.ProjectID,
		SecretKind: vault.SecretKindWebhookHMAC, BoundAudience: &targetAudience, Name: "CT36 webhook target", Plaintext: []byte(targetURL)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.RawDB().Exec(`INSERT INTO webhook_subscriptions(id,org_id,project_id,name,target_url,target_url_credential_id,allow_private_ip,allowed_cidrs_json,signing_secret_credential_id,event_types_json,enabled,version,created_at_ms,updated_at_ms)
		VALUES('whk_ct36',?,?, 'CT36', ?, ?, 1, '["127.0.0.0/8"]', ?, '["mcp.approval.pending"]', 1, 1, 1, 1)`, boot.OrgID, boot.ProjectID, origin, targetSecret.ID, signing.ID); err != nil {
		t.Fatal(err)
	}
	capability := vault.MintUnsealCapabilityForBroker()
	dispatcher, err := webhook.New(webhook.Config{DB: h.st.RawDB(), Resolver: mcpSyncTestHostResolver{}, Limits: webhook.Limits{PollInterval: time.Millisecond}, Unseal: func(ctx context.Context, orgID, credentialID string) ([]byte, func(), error) {
		secret, err := h.vault.UnsealWithCapability(ctx, capability, orgID, credentialID)
		if err != nil {
			return nil, nil, err
		}
		return secret.Plaintext, secret.Zeroize, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	approvalCreated := make(chan string, 8)
	mcpServer, err := mcphub.NewServer(mcphub.Config{DB: h.st.RawDB(), Snapshots: h.snapshots, Broker: brokerService, Audit: h.auditLog,
		Upstream: &mcphub.BrokerClient{Broker: brokerService}, ApprovalWait: 2 * time.Second,
		RecordApprovalPendingTx: func(ctx context.Context, tx *sql.Tx, requestID string, binding approval.Binding) error {
			if err := dispatcher.RecordEventTx(ctx, tx, webhook.Event{ID: webhook.StableEventID("mcp.approval.pending:" + requestID), OrgID: binding.OrgID, ProjectID: binding.ProjectID, Type: "mcp.approval.pending", Payload: json.RawMessage(fmt.Sprintf(`{"approval_request_id":%q}`, requestID))}); err != nil {
				return err
			}
			approvalCreated <- requestID
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpServer)
	mux.Handle("/admin/", h.srv)
	public := httptest.NewServer(mux)
	defer public.Close()
	moduleRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	client := netguard.NewHTTPClient()
	defer client.CloseIdleConnections()
	postMCPRaw := func(body, session string, bearer ...string) (*http.Response, []byte, error) {
		req, err := http.NewRequest(http.MethodPost, public.URL+"/mcp", strings.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		token := key.SecretKey
		if len(bearer) != 0 {
			token = bearer[0]
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		return resp, data, nil
	}
	postMCP := func(body, session string) (*http.Response, []byte) {
		t.Helper()
		resp, data, err := postMCPRaw(body, session)
		if err != nil {
			t.Fatal(err)
		}
		return resp, data
	}
	initResp, _ := postMCP(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"ct36-scripted-client","version":"1"}}}`, "")
	if initResp.StatusCode != http.StatusOK || initResp.Header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("public MCP initialize failed: %d headers=%v", initResp.StatusCode, initResp.Header)
	}
	session := initResp.Header.Get("Mcp-Session-Id")
	sseReq, _ := http.NewRequest(http.MethodGet, public.URL+"/mcp", nil)
	sseReq.Header.Set("Authorization", "Bearer "+key.SecretKey)
	sseReq.Header.Set("Accept", "text/event-stream")
	sseReq.Header.Set("Mcp-Session-Id", session)
	sseResponse, err := client.Do(sseReq)
	if err != nil || sseResponse.StatusCode != http.StatusOK {
		t.Fatalf("open public MCP SSE stream: response=%v err=%v", sseResponse, err)
	}
	defer sseResponse.Body.Close()
	progressLine := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(sseResponse.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "notifications/progress") {
				select {
				case progressLine <- line:
				default:
				}
			}
		}
	}()
	pendingResp, pendingBody := postMCP(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ct36-tools__delete_document","arguments":{"document_id":"doc-7"},"_meta":{"progressToken":"ct36-progress"}}}`, session)
	select {
	case line := <-progressLine:
		if !strings.Contains(line, "ct36-progress") {
			t.Fatalf("MCP SSE progress token mismatch: %s", line)
		}
	case <-time.After(time.Second):
		t.Fatal("public MCP SSE stream did not emit approval progress")
	}
	var pending struct {
		Result struct {
			IsError    bool `json:"isError"`
			Structured struct {
				ID    string `json:"approval_request_id"`
				Token string `json:"pre_approval_token"`
				Hash  string `json:"approval_binding_hash"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(pendingBody, &pending); err != nil {
		t.Fatal(err)
	}
	if pendingResp.StatusCode != http.StatusOK || !pending.Result.IsError || pending.Result.Structured.ID == "" || pending.Result.Structured.Token == "" || upstreamCalls.Load() != 0 {
		t.Fatalf("expected HTTP 200 async approval, status=%d body=%s upstream calls=%d", pendingResp.StatusCode, pendingBody, upstreamCalls.Load())
	}
	select {
	case got := <-approvalCreated:
		if got != pending.Result.Structured.ID {
			t.Fatalf("recorded approval id %q differs from MCP response %q", got, pending.Result.Structured.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("async approval creation was not recorded")
	}
	dispatchCtx, stopDispatcher := context.WithCancel(t.Context())
	dispatchDone := make(chan struct{})
	go func() { defer close(dispatchDone); _ = dispatcher.Run(dispatchCtx) }()
	defer func() { stopDispatcher(); <-dispatchDone }()
	var eventBody []byte
	var signature, timestamp string
	select {
	case eventBody = <-receiverBody:
	case <-time.After(10 * time.Second):
		t.Fatal("approval webhook was not delivered")
	}
	signature, timestamp = <-receiverSignature, <-receiverTimestamp
	mac := hmac.New(sha256.New, macKey)
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(eventBody)
	if signature != "sha256="+hex.EncodeToString(mac.Sum(nil)) || !bytes.Contains(eventBody, []byte(pending.Result.Structured.ID)) {
		t.Fatalf("approval webhook signature or request id mismatch: signature=%q body=%s", signature, eventBody)
	}
	reviewer, err := h.authSvc.CreatePrincipal(t.Context(), auth.CreatePrincipalParams{OrgID: &boot.OrgID, Kind: auth.PrincipalKindHumanLocal, EmailOrSub: "ct36-reviewer@example.test", Password: "StrongReviewerPass#2026!"})
	if err != nil {
		t.Fatal(err)
	}
	h.registerBearerToken("ct36-reviewer", &rbac.Actor{PrincipalID: reviewer.ID, SessionOrgID: &boot.OrgID, CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleSecurityReviewer}}})
	referenceClient := exec.CommandContext(t.Context(), "node", filepath.Join(moduleRoot, "tests", "integration", "ct36-reference-client.mjs"), public.URL+"/mcp", key.SecretKey, "ct36-reviewer", "ct36-tools__delete_document")
	if output, err := referenceClient.CombinedOutput(); err != nil {
		t.Fatalf("official MCP SDK approval and replay client failed: %v (upstream calls=%d)\n%s", err, upstreamCalls.Load(), output)
	} else if !strings.Contains(string(output), "approved through Admin API, and replayed the exact call") || upstreamCalls.Load() != 1 {
		t.Fatalf("official SDK did not verify approved replay: output=%s upstream calls=%d", output, upstreamCalls.Load())
	}
	select {
	case <-approvalCreated: // Drain the SDK approval's creation signal.
	default:
	}
	syncKey, err := h.vkeys.Create(t.Context(), vkey.CreateParams{OrgID: boot.OrgID, ProjectID: boot.ProjectID, EnvID: boot.EnvID,
		OwnerPrincipalID: &boot.OwnerPrincipalID, Name: "ct36-sync-mcp", AllowedPlanes: "MCP", AllowedMCPTools: []string{"ct36-tools__delete_document"}, ActorID: boot.OwnerPrincipalID})
	if err != nil {
		t.Fatal(err)
	}
	syncGrant := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/tools/"+syncedTools.Tools[0].ID+"/grants", cookie, boot.CSRFToken, "", map[string]any{"env_id": boot.EnvID, "virtual_key_id": syncKey.Summary.ID})
	if syncGrant.Code != http.StatusCreated {
		t.Fatalf("grant synchronous MCP tool: %d %s", syncGrant.Code, syncGrant.Body.String())
	}
	syncInit, syncInitBody, err := postMCPRaw(`{"jsonrpc":"2.0","id":40,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"ct36-sync-client","version":"1"}}}`, "", syncKey.SecretKey)
	if err != nil || syncInit.StatusCode != http.StatusOK || syncInit.Header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("initialize synchronous MCP session failed: response=%v body=%s err=%v", syncInit, syncInitBody, err)
	}
	syncSession := syncInit.Header.Get("Mcp-Session-Id")
	syncStreamReq, _ := http.NewRequest(http.MethodGet, public.URL+"/mcp", nil)
	syncStreamReq.Header.Set("Authorization", "Bearer "+syncKey.SecretKey)
	syncStreamReq.Header.Set("Accept", "text/event-stream")
	syncStreamReq.Header.Set("Mcp-Session-Id", syncSession)
	syncStreamResp, err := client.Do(syncStreamReq)
	if err != nil || syncStreamResp.StatusCode != http.StatusOK {
		t.Fatalf("open synchronous MCP progress stream: response=%v err=%v", syncStreamResp, err)
	}
	defer syncStreamResp.Body.Close()
	syncProgressLine := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(syncStreamResp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "notifications/progress") {
				select {
				case syncProgressLine <- line:
				default:
				}
			}
		}
	}()
	const syncLimit = 2 * time.Second
	syncStarted := time.Now()
	syncResult := make(chan struct {
		response *http.Response
		body     []byte
		err      error
	}, 1)
	go func() {
		response, body, err := postMCPRaw(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ct36-tools__delete_document","arguments":{"document_id":"sync-doc"},"_meta":{"tanpopoApprovalMode":"sync","progressToken":"ct36-sync"}}}`, syncSession, syncKey.SecretKey)
		syncResult <- struct {
			response *http.Response
			body     []byte
			err      error
		}{response: response, body: body, err: err}
	}()
	var syncApprovalID string
	select {
	case syncApprovalID = <-approvalCreated:
	case <-time.After(time.Second):
		t.Fatal("synchronous approval request was not recorded")
	}
	select {
	case line := <-syncProgressLine:
		if !strings.Contains(line, "ct36-sync") {
			t.Fatalf("synchronous MCP progress token mismatch: %s", line)
		}
	case <-time.After(time.Second):
		t.Fatal("public MCP SSE stream did not emit synchronous approval progress")
	}
	var syncHash string
	if err := h.st.RawDB().QueryRow(`SELECT params_jcs_sha256 FROM approval_requests WHERE id=?`, syncApprovalID).Scan(&syncHash); err != nil {
		t.Fatal(err)
	}
	syncDecision, _ := json.Marshal(map[string]any{"decision": "APPROVE", "expected_binding_hash": syncHash, "reason": "synchronous Admin API approval regression"})
	syncDecisionReq, _ := http.NewRequest(http.MethodPost, public.URL+"/admin/v1/approvals/"+syncApprovalID+"/decide", bytes.NewReader(syncDecision))
	syncDecisionReq.Header.Set("Authorization", "Bearer ct36-reviewer")
	syncDecisionReq.Header.Set("If-Match", FormatETag(1))
	syncDecisionReq.Header.Set("Content-Type", "application/json")
	syncDecisionResp, err := client.Do(syncDecisionReq)
	if err != nil {
		t.Fatal(err)
	}
	syncDecisionBody, _ := io.ReadAll(syncDecisionResp.Body)
	_ = syncDecisionResp.Body.Close()
	if syncDecisionResp.StatusCode != http.StatusOK {
		t.Fatalf("synchronous Admin API decision failed: status=%d body=%s", syncDecisionResp.StatusCode, syncDecisionBody)
	}
	select {
	case result := <-syncResult:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.response.StatusCode != http.StatusOK || !bytes.Contains(result.body, []byte("deleted")) || upstreamCalls.Load() != 2 || time.Since(syncStarted) >= syncLimit {
			t.Fatalf("public synchronous approval failed: status=%d elapsed=%s body=%s upstream calls=%d", result.response.StatusCode, time.Since(syncStarted), result.body, upstreamCalls.Load())
		}
	case <-time.After(syncLimit):
		t.Fatal("synchronous approval did not complete within its configured bound")
	}
}
