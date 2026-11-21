// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"net/http"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestA2AAdminEndpointsRegistrationAndListing(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "A2A Admin Org", "a2a-proj")

	base := map[string]any{
		"project_id":          boot.ProjectID,
		"agent_slug":          "invoice-triage",
		"name":                "Invoice Triage Agent",
		"description":         "Extracts regional invoice metadata",
		"endpoint_url":        "https://agent.example.test/a2a",
		"data_region":         "EU",
		"max_delegation_hops": 4,
		"skills": []map[string]any{
			{"id": "extract", "name": "Extract"},
		},
	}

	// Missing CSRF token must fail closed with 403.
	if got := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, "", "", base); got.Code != http.StatusForbidden {
		t.Fatalf("registration without CSRF status=%d body=%s", got.Code, got.Body.String())
	}

	// Delegation hops > 5 must be rejected (NEG-1 / INV-A2A).
	tooDeep := map[string]any{}
	for k, v := range base {
		tooDeep[k] = v
	}
	tooDeep["max_delegation_hops"] = 6
	if got := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", tooDeep); got.Code != http.StatusBadRequest {
		t.Fatalf("hop > 5 status=%d body=%s", got.Code, got.Body.String())
	}

	// Private loopback endpoint without allow_private_ip consent must fail SSRF check.
	ssrfReq := map[string]any{}
	for k, v := range base {
		ssrfReq[k] = v
	}
	ssrfReq["endpoint_url"] = "http://127.0.0.1:8080/a2a"
	if got := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", ssrfReq); got.Code != http.StatusForbidden && got.Code != http.StatusBadRequest {
		t.Fatalf("SSRF loopback status=%d body=%s", got.Code, got.Body.String())
	}

	// Valid registration must succeed with ETag header.
	created := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", base)
	if created.Code != http.StatusCreated {
		t.Fatalf("register A2A agent status=%d body=%s", created.Code, created.Body.String())
	}
	if created.Header().Get("ETag") != `"v1"` {
		t.Fatalf("expected ETag \"v1\", got %q", created.Header().Get("ETag"))
	}
	var card a2a.AgentCard
	mustDecodeJSON(t, created.Body.Bytes(), &card)
	if !strings.HasPrefix(card.AgentID, "agt_") || card.AgentSlug != "invoice-triage" || card.DataRegion != "EU" || card.MaxDelegationHops != 4 {
		t.Fatalf("unexpected AgentCard contract: %+v", card)
	}

	// Listing registered A2A agents must include the new AgentCard.
	listed := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/agents", cookie, "", "", nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), card.AgentID) {
		t.Fatalf("list A2A agents status=%d body=%s", listed.Code, listed.Body.String())
	}

	// Seed a Virtual Key and an A2A task row, then verify GET /admin/v1/a2a/tasks returns it.
	vkRec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
		"name":       "a2a-admin-test-key",
	})
	if vkRec.Code != http.StatusCreated {
		t.Fatalf("create virtual key status=%d body=%s", vkRec.Code, vkRec.Body.String())
	}
	var vkCreated struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, vkRec.Body.Bytes(), &vkCreated)
	now := h.fk.Now().UnixMilli()
	_, err := h.st.RawDB().Exec(
		port.Rebind(h.srv.dialect, `INSERT INTO a2a_tasks(id,org_id,project_id,env_id,agent_endpoint_id,caller_virtual_key_id,trace_root_id,hop_count,call_chain_ids_json,sub_budget_cap_micros,reservation_id,enforced_region,state,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`),
		"tsk_admin_01", boot.OrgID, boot.ProjectID, boot.EnvID, card.AgentID, vkCreated.ID, "req_root_01", 2, `["`+card.AgentID+`"]`, int64(150000), "res_01", "EU", "WORKING", 1, now, now,
	)
	if err != nil {
		t.Fatalf("seed a2a_tasks: %v", err)
	}
	taskList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/tasks", cookie, "", "", nil)
	if taskList.Code != http.StatusOK || !strings.Contains(taskList.Body.String(), "tsk_admin_01") || !strings.Contains(taskList.Body.String(), `"state":"working"`) {
		t.Fatalf("list A2A tasks status=%d body=%s", taskList.Code, taskList.Body.String())
	}

	// Reject public http:// without allow_private_ip and reject wildcard /0 CIDRs.
	badHTTP := map[string]any{}
	for k, v := range base {
		badHTTP[k] = v
	}
	badHTTP["agent_slug"] = "bad-http"
	badHTTP["endpoint_url"] = "http://agent.example.test/a2a"
	if got := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", badHTTP); got.Code != http.StatusBadRequest {
		t.Fatalf("public http:// status=%d want 400 body=%s", got.Code, got.Body.String())
	}
	badCIDR := map[string]any{}
	for k, v := range base {
		badCIDR[k] = v
	}
	badCIDR["agent_slug"] = "bad-cidr"
	badCIDR["allowed_cidrs"] = []string{"0.0.0.0/0"}
	if got := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", badCIDR); got.Code != http.StatusBadRequest {
		t.Fatalf("wildcard CIDR status=%d want 400 body=%s", got.Code, got.Body.String())
	}

	// Register a 2nd agent and insert a 2nd task, then verify HMAC keyset cursor pagination (?limit=1).
	h.fk.Advance(100_000_000) // advance clock by 100ms for deterministic descending timestamp ordering
	second := map[string]any{}
	for k, v := range base {
		second[k] = v
	}
	second["agent_slug"] = "invoice-triage-2"
	second["name"] = "Invoice Triage Agent 2"
	created2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", second)
	if created2.Code != http.StatusCreated {
		t.Fatalf("register 2nd A2A agent status=%d body=%s", created2.Code, created2.Body.String())
	}
	var card2 a2a.AgentCard
	mustDecodeJSON(t, created2.Body.Bytes(), &card2)

	var agentPage1 paginatedListEnvelope[a2a.AgentCard]
	p1 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/agents?limit=1", cookie, "", "", nil)
	if p1.Code != http.StatusOK {
		t.Fatalf("GET /a2a/agents?limit=1 status=%d body=%s", p1.Code, p1.Body.String())
	}
	mustDecodeJSON(t, p1.Body.Bytes(), &agentPage1)
	if !agentPage1.HasMore || agentPage1.NextCursor == nil || len(agentPage1.Data) != 1 || agentPage1.Data[0].AgentID != card2.AgentID {
		t.Fatalf("unexpected agent page 1: %+v", agentPage1)
	}
	var agentPage2 paginatedListEnvelope[a2a.AgentCard]
	p2 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/agents?limit=1&cursor="+*agentPage1.NextCursor, cookie, "", "", nil)
	if p2.Code != http.StatusOK {
		t.Fatalf("GET /a2a/agents page 2 status=%d body=%s", p2.Code, p2.Body.String())
	}
	mustDecodeJSON(t, p2.Body.Bytes(), &agentPage2)
	if agentPage2.HasMore || len(agentPage2.Data) != 1 || agentPage2.Data[0].AgentID != card.AgentID {
		t.Fatalf("unexpected agent page 2: %+v", agentPage2)
	}

	now2 := h.fk.Now().UnixMilli()
	_, err = h.st.RawDB().Exec(
		port.Rebind(h.srv.dialect, `INSERT INTO a2a_tasks(id,org_id,project_id,env_id,agent_endpoint_id,caller_virtual_key_id,trace_root_id,hop_count,call_chain_ids_json,sub_budget_cap_micros,reservation_id,enforced_region,state,version,created_at_ms,updated_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`),
		"tsk_admin_02", boot.OrgID, boot.ProjectID, boot.EnvID, card2.AgentID, vkCreated.ID, "req_root_02", 1, `["`+card2.AgentID+`"]`, int64(50000), "res_02", "EU", "COMPLETED", 1, now2, now2,
	)
	if err != nil {
		t.Fatalf("seed 2nd a2a_tasks: %v", err)
	}
	var taskPage1 paginatedListEnvelope[a2a.TaskView]
	tp1 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/tasks?limit=1", cookie, "", "", nil)
	if tp1.Code != http.StatusOK {
		t.Fatalf("GET /a2a/tasks?limit=1 status=%d body=%s", tp1.Code, tp1.Body.String())
	}
	mustDecodeJSON(t, tp1.Body.Bytes(), &taskPage1)
	if !taskPage1.HasMore || taskPage1.NextCursor == nil || len(taskPage1.Data) != 1 || taskPage1.Data[0].TaskID != "tsk_admin_02" {
		t.Fatalf("unexpected task page 1: %+v", taskPage1)
	}
	var taskPage2 paginatedListEnvelope[a2a.TaskView]
	tp2 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/tasks?limit=1&cursor="+*taskPage1.NextCursor, cookie, "", "", nil)
	if tp2.Code != http.StatusOK {
		t.Fatalf("GET /a2a/tasks page 2 status=%d body=%s", tp2.Code, tp2.Body.String())
	}
	mustDecodeJSON(t, tp2.Body.Bytes(), &taskPage2)
	if taskPage2.HasMore || len(taskPage2.Data) != 1 || taskPage2.Data[0].TaskID != "tsk_admin_01" {
		t.Fatalf("unexpected task page 2: %+v", taskPage2)
	}
}
