// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
)

type callReply struct {
	Status int
	Result struct {
		IsError bool `json:"isError"`
		Details struct {
			ID     string `json:"approval_request_id"`
			Token  string `json:"pre_approval_token"`
			Hash   string `json:"approval_binding_hash"`
			Status string `json:"status"`
			Resume string `json:"resume_tool"`
		} `json:"structuredContent"`
	}
	Error struct {
		Code      any
		Message   string
		Retryable *bool
	}
	Raw string
}

func invokeMCP(t *testing.T, h *hubTestSetup, session, tool string, args json.RawMessage, meta map[string]any) callReply {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "tools/call", "id": 2, "params": map[string]any{"name": tool, "arguments": args, "_meta": meta}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set(sessionHeader, session)
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, req)
	var reply callReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	reply.Status = w.Code
	reply.Raw = w.Body.String()
	return reply
}

func approvalSession(t *testing.T, h *hubTestSetup) string {
	t.Helper()
	status, headers, _ := h.post(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, "")
	if status != http.StatusOK || headers.Get(sessionHeader) == "" {
		t.Fatal("initialize failed", status)
	}
	return headers.Get(sessionHeader)
}

func destructiveFixture(t *testing.T, h *hubTestSetup) approval.Binding {
	t.Helper()
	if _, err := h.handler.cfg.DB.Exec("UPDATE mcp_tools SET side_effect_class='destructive' WHERE id='tool_000'"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.handler.cfg.DB.Exec(`INSERT INTO principals(id,org_id,kind,email_or_sub,password_argon2id,status,version,created_at_ms,updated_at_ms)
        VALUES ('reviewer',?,'HUMAN_LOCAL','reviewer@example.test','x','ACTIVE',1,1,1)`, h.scope.OrgID); err != nil {
		t.Fatal(err)
	}
	return approval.Binding{OrgID: h.scope.OrgID, ProjectID: h.scope.ProjectID, VirtualKeyID: h.scope.KeyID, ToolName: "tool_000", ServerSlug: "github", Arguments: json.RawMessage(`{"query":"dev","a":1}`)}
}

func TestHubApprovalExactTupleAndSingleUse(t *testing.T) {
	h := newHubTestSetup(t, 3, false)
	b := destructiveFixture(t, h)
	session := approvalSession(t, h)
	_, _, listed := h.post(t, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, session)
	if !bytes.Contains(listed["result"], []byte(approvalStatusTool)) {
		t.Fatal("missing approval status tool")
	}
	create := invokeMCP(t, h, session, "github__tool_000", b.Arguments, nil)
	pending := create.Result.Details
	canonical := fmt.Sprintf(`{"arguments":{"a":1,"query":"dev"},"org_id":%q,"server_slug":"github","tool_name":"tool_000","virtual_key_id":%q}`, b.OrgID, b.VirtualKeyID)
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	if create.Status != 200 || !create.Result.IsError || pending.Status != "APPROVAL_PENDING" || pending.ID == "" || pending.Token == "" || pending.Hash != wantHash || pending.Resume != approvalStatusTool || h.upstream.calls != 0 {
		t.Fatalf("pending response status=%d state=%q hash=%q error=%v", create.Status, pending.Status, pending.Hash, create.Error)
	}
	pollArgs := json.RawMessage(fmt.Sprintf(`{"approval_request_id":%q}`, pending.ID))
	poll := invokeMCP(t, h, session, approvalStatusTool, pollArgs, nil)
	if poll.Result.Details.Status != "APPROVAL_PENDING" || strings.Contains(poll.Raw, "pre_approval_token") || strings.Contains(poll.Raw, pending.Token) {
		t.Fatal("status poll omitted state or exposed token")
	}
	meta := map[string]any{"tanpopoApprovalToken": pending.Token}
	early := invokeMCP(t, h, session, "github__tool_000", b.Arguments, meta)
	if early.Error.Message != string(approval.CodeInvalidState) || h.upstream.calls != 0 {
		t.Fatal("pending replay reached upstream or returned wrong error")
	}
	if _, err := h.handler.approvals.Decide(t.Context(), pending.ID, b, 1, "reviewer", approval.DecisionApprove, "reviewed"); err != nil {
		t.Fatal(err)
	}

	// A token must also be checked when the alternate target is mutating.
	for _, tt := range []struct {
		name string
		args json.RawMessage
	}{
		{"github__tool_000", json.RawMessage(`{"query":"prod","a":1}`)},
		{"github__tool_002", b.Arguments},
	} {
		reply := invokeMCP(t, h, session, tt.name, tt.args, meta)
		if reply.Status != 403 || reply.Error.Code != "parameter_hash_mismatch" || reply.Error.Retryable == nil || *reply.Error.Retryable {
			t.Fatal("tuple mismatch accepted or wrong error", reply.Error)
		}
	}
	svc, err := vkey.NewService(vkey.ServiceConfig{DB: h.handler.cfg.DB, Snapshots: h.handler.cfg.Snapshots, AuditSink: h.audit})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Create(t.Context(), vkey.CreateParams{OrgID: b.OrgID, ProjectID: h.scope.ProjectID, EnvID: h.scope.EnvID, Name: "second", AllowedPlanes: "MCP", AllowedMCPTools: []string{"github__tool_000"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.handler.cfg.DB.Exec(`UPDATE mcp_tool_grants SET virtual_key_id=NULL WHERE mcp_tool_id='tool_000'`); err != nil {
		t.Fatal(err)
	}
	otherHub := *h
	otherHub.key = other.SecretKey
	otherSession := approvalSession(t, &otherHub)
	if got := invokeMCP(t, &otherHub, otherSession, "github__tool_000", b.Arguments, meta); got.Status != 403 || got.Error.Code != "parameter_hash_mismatch" {
		t.Fatal("second key replay", got.Error)
	}
	if got := invokeMCP(t, &otherHub, otherSession, approvalStatusTool, pollArgs, nil); got.Error.Message != string(approval.CodeNotFound) {
		t.Fatal("second key could poll approval", got.Error)
	}
	if h.upstream.calls != 0 {
		t.Fatal("mismatched replay dispatched")
	}
	var audited int
	for _, event := range h.audit.events {
		if event.EventType == "mcp.approval_parameter_hash_mismatch" {
			audited++
			if strings.Contains(event.MetadataJSON, pending.Token) {
				t.Fatal("token in audit")
			}
		}
	}
	if audited != 3 {
		t.Fatal("tuple mismatch audits", audited)
	}
	accepted := invokeMCP(t, h, session, "github__tool_000", json.RawMessage(`{"a":1.0,"query":"dev"}`), meta)
	if accepted.Error.Code != nil || h.upstream.calls != 1 || strings.Contains(accepted.Raw, pending.Token) {
		t.Fatal("canonical replay failed", accepted.Error, h.upstream.calls)
	}
	again := invokeMCP(t, h, session, "github__tool_000", b.Arguments, meta)
	if again.Status != http.StatusForbidden || again.Error.Code != string(approval.CodeParameterHashMismatch) || h.upstream.calls != 1 {
		t.Fatal("second execution", "status", again.Status, again.Error, h.upstream.calls)
	}
	if status := invokeMCP(t, h, session, approvalStatusTool, pollArgs, nil); status.Result.Details.Status != "CONSUMED" || strings.Contains(status.Raw, pending.Token) {
		t.Fatal("unexpected final approval status")
	}
}

func TestHubSyncApprovalProgressAndTimeout(t *testing.T) {
	for _, mode := range []string{"approve", "timeout", "revoked"} {
		t.Run(mode, func(t *testing.T) {
			created := make(chan string, 1)
			var h *hubTestSetup
			h = newHubTestSetupWithPendingRecorder(t, 1, false, func(_ context.Context, _ *sql.Tx, id string, binding approval.Binding) error {
				if binding.OrgID != h.scope.OrgID || binding.ProjectID != h.scope.ProjectID {
					return fmt.Errorf("wrong approval scope")
				}
				created <- id
				return nil
			})
			b := destructiveFixture(t, h)
			session := approvalSession(t, h)
			events := h.handler.sessions[session].events
			h.handler.cfg.ApprovalWait = 2 * time.Second
			if mode == "timeout" {
				h.handler.cfg.ApprovalWait = 100 * time.Millisecond
			}
			result := make(chan callReply, 1)
			go func() {
				result <- invokeMCP(t, h, session, "github__tool_000", b.Arguments, map[string]any{"progressToken": "waiting"})
			}()
			var id string
			select {
			case id = <-created:
			case <-time.After(3 * time.Second):
				t.Fatal("approval not created")
			}
			select {
			case msg := <-events:
				if !bytes.Contains(msg, []byte("notifications/progress")) || !bytes.Contains(msg, []byte("waiting")) {
					t.Fatal("missing progress notification")
				}
			case <-time.After(time.Second):
				t.Fatal("no progress before decision")
			}
			if mode != "timeout" {
				if mode == "revoked" {
					if _, err := h.handler.cfg.DB.Exec("DELETE FROM mcp_tool_grants"); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := h.handler.approvals.Decide(t.Context(), id, b, 1, "reviewer", approval.DecisionApprove, "reviewed"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case got := <-result:
				switch mode {
				case "timeout":
					if got.Status != 200 || !got.Result.IsError || got.Result.Details.ID != id || got.Result.Details.Token == "" || h.upstream.calls != 0 {
						t.Fatal("invalid pending fallback", got.Error)
					}
				case "approve":
					if got.Error.Code != nil || strings.Contains(got.Raw, "pre_approval_token") || h.upstream.calls != 1 {
						t.Fatal("sync approval failed", got.Error)
					}
				case "revoked":
					if got.Error.Message != "tool_access_denied" || h.upstream.calls != 0 {
						t.Fatal("dispatched after grant revocation", got.Error)
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("hold exceeded bound")
			}
		})
	}
}

func TestHubResumeChecksIDNonceAndOriginalArguments(t *testing.T) {
	h := newHubTestSetup(t, 1, false)
	b := destructiveFixture(t, h)
	session := approvalSession(t, h)
	first := invokeMCP(t, h, session, "github__tool_000", b.Arguments, nil).Result.Details
	second := invokeMCP(t, h, session, "github__tool_000", b.Arguments, nil).Result.Details
	if _, err := h.handler.approvals.Decide(t.Context(), first.ID, b, 1, "reviewer", approval.DecisionApprove, "reviewed"); err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(fmt.Sprintf(`{"approval_request_id":%q,"arguments":%s}`, first.ID, b.Arguments))
	wrong := invokeMCP(t, h, session, approvalStatusTool, args, map[string]any{"tanpopoApprovalToken": second.Token})
	if wrong.Error.Message != string(approval.CodeInvalidToken) || h.upstream.calls != 0 {
		t.Fatal("accepted nonce for a different request")
	}
	ok := invokeMCP(t, h, session, approvalStatusTool, args, map[string]any{"tanpopoApprovalToken": first.Token})
	if ok.Error.Code != nil || h.upstream.calls != 1 {
		t.Fatal("valid resume failed", ok.Error)
	}
	again := invokeMCP(t, h, session, "github__tool_000", b.Arguments, map[string]any{"tanpopoApprovalToken": first.Token})
	if again.Status != http.StatusForbidden || again.Error.Code != string(approval.CodeParameterHashMismatch) || h.upstream.calls != 1 {
		t.Fatal("resume and replay dispatched twice")
	}
}

func TestHubPendingOutboxFailureRollsBackCreate(t *testing.T) {
	var h *hubTestSetup
	var eventID string
	calls := 0
	h = newHubTestSetupWithPendingRecorder(t, 1, false, func(_ context.Context, _ *sql.Tx, id string, binding approval.Binding) error {
		calls++
		if binding.OrgID != h.scope.OrgID || binding.ProjectID != h.scope.ProjectID {
			t.Fatal("incorrect event scope")
		}
		eventID = id
		return fmt.Errorf("private dispatcher diagnostic")
	})
	b := destructiveFixture(t, h)
	session := approvalSession(t, h)
	got := invokeMCP(t, h, session, "github__tool_000", b.Arguments, nil)
	if calls != 1 || eventID == "" || got.Result.Details.ID != "" || got.Result.Details.Token != "" || got.Error.Message == "" ||
		got.Status != http.StatusOK || strings.Contains(got.Raw, "private dispatcher diagnostic") {
		t.Fatalf("outbox failure returned a receipt for an uncommitted approval: calls=%d eventID=%q status=%d details=%+v raw=%s", calls, eventID, got.Status, got.Result.Details, got.Raw)
	}
	var count int
	if err := h.handler.cfg.DB.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE id=?`, eventID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("approval survived transactional outbox failure, count=%d err=%v", count, err)
	}
}
