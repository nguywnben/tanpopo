// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/auth"
	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/iam/vkey"
	"github.com/nguywnben/tanpopo/internal/ingress/mcp/approval"
)

func expectAdminStatus(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d. Response %s", response.Code, status, response.Body)
	}
	if code != "" {
		var envelope CanonicalErrorEnvelope
		mustDecodeJSON(t, response.Body.Bytes(), &envelope)
		if envelope.Error.Code != code {
			t.Fatalf("code = %q, want %q", envelope.Error.Code, code)
		}
	}
}

func TestAdminMCPApprovalDecisions(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "Approval Org", "approval-project")
	ctx := t.Context()
	store, err := approval.NewStore(approval.Config{DB: h.st.RawDB(), Audit: h.auditLog, Now: h.fk.Now})
	if err != nil {
		t.Fatal(err)
	}
	key, err := h.vkeys.Create(ctx, mustVKeyCreateParams(boot.OrgID, boot.ProjectID, boot.EnvID, boot.OwnerPrincipalID, "approval-key"))
	if err != nil {
		t.Fatal(err)
	}
	binding := approval.Binding{OrgID: boot.OrgID, VirtualKeyID: key.Summary.ID, ServerSlug: "docs", ToolName: "delete", Arguments: json.RawMessage(`{"private":"argument-sentinel"}`)}
	create := func() approval.CreatedRequest {
		t.Helper()
		req, err := store.Create(ctx, binding, "", h.fk.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	req := create()
	second := create()

	reviewer, err := h.authSvc.CreatePrincipal(ctx, auth.CreatePrincipalParams{OrgID: &boot.OrgID, Kind: auth.PrincipalKindHumanLocal, EmailOrSub: "reviewer@example.test", Password: "StrongReviewerPass#2026!"})
	if err != nil {
		t.Fatal(err)
	}
	register := func(token, role, project, org string) {
		scope := rbac.ScopeProject
		scopeID := project
		if project == "" {
			scope = rbac.ScopeOrg
			scopeID = org
		}
		h.registerBearerToken(token, &rbac.Actor{PrincipalID: reviewer.ID, SessionOrgID: &org, CredentialPlane: rbac.CredentialPlaneAdminService,
			Bindings: []rbac.RoleBinding{{OrgID: &org, ScopeType: scope, ScopeID: scopeID, RoleName: role}}})
	}
	register("reviewer", rbac.RoleSecurityReviewer, boot.ProjectID, boot.OrgID)
	register("other-project", rbac.RoleSecurityReviewer, "other-project", boot.OrgID)
	register("other-org", rbac.RoleSecurityReviewer, "", "another-org")
	register("finops", rbac.RoleFinOpsViewer, "", boot.OrgID)

	const queue = "/admin/v1/approvals"
	expectAdminStatus(t, h.doPublic(t, "GET", queue, nil), 401, "")
	expectAdminStatus(t, h.doBearer(t, "GET", queue, "finops", "", nil), 403, "")
	expectAdminStatus(t, h.doBearer(t, "GET", queue+"?status=unknown", "reviewer", "", nil), 400, "invalid_request")
	for _, token := range []string{"other-org", "other-project"} {
		result := h.doBearer(t, "GET", queue, token, "", nil)
		expectAdminStatus(t, result, 200, "")
		var page paginatedListEnvelope[approvalResource]
		mustDecodeJSON(t, result.Body.Bytes(), &page)
		if len(page.Data) != 0 {
			t.Fatalf("%s can see another scope's queue", token)
		}
	}
	list := h.doBearer(t, "GET", queue+"?status=PENDING&limit=1", "reviewer", "", nil)
	expectAdminStatus(t, list, 200, "")
	var page paginatedListEnvelope[approvalResource]
	mustDecodeJSON(t, list.Body.Bytes(), &page)
	if len(page.Data) != 1 || !page.HasMore || page.NextCursor == nil {
		t.Fatalf("unexpected first page %+v", page)
	}
	more := h.doBearer(t, "GET", queue+"?status=PENDING&limit=1&cursor="+url.QueryEscape(*page.NextCursor), "reviewer", "", nil)
	expectAdminStatus(t, more, 200, "")
	var last paginatedListEnvelope[approvalResource]
	mustDecodeJSON(t, more.Body.Bytes(), &last)
	if len(last.Data) != 1 || last.HasMore || last.Data[0].ID == page.Data[0].ID {
		t.Fatalf("unexpected second page %+v", last)
	}
	rotatingKey, err := h.vkeys.Create(ctx, mustVKeyCreateParams(boot.OrgID, boot.ProjectID, boot.EnvID, boot.OwnerPrincipalID, "rotation-approval-key"))
	if err != nil {
		t.Fatal(err)
	}
	rotatingBinding := binding
	rotatingBinding.VirtualKeyID = rotatingKey.Summary.ID
	rotatingBinding.CredentialPrincipalID = boot.OwnerPrincipalID
	rotatingApproval, err := store.Create(ctx, rotatingBinding, "", h.fk.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.vkeys.Rotate(ctx, vkey.RotateParams{OrgID: boot.OrgID, KeyID: rotatingKey.Summary.ID, ExpectedVersion: rotatingKey.Summary.Version, GraceSeconds: 60, ActorID: reviewer.ID}); err != nil {
		t.Fatal(err)
	}
	var rotatingHash string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT params_jcs_sha256 FROM approval_requests WHERE id=?`, rotatingApproval.ID).Scan(&rotatingHash); err != nil {
		t.Fatal(err)
	}
	rotationDecision := h.doBearer(t, "POST", "/admin/v1/approvals/"+rotatingApproval.ID+"/decide", "reviewer", FormatETag(1), map[string]any{
		"decision": "APPROVE", "expected_binding_hash": rotatingHash, "reason": "rotation self-approval regression",
	})
	expectAdminStatus(t, rotationDecision, 403, "self_approval_forbidden")
	for _, secret := range []string{req.PreApprovalToken, "pre_approval_token", "argument-sentinel"} {
		if strings.Contains(list.Body.String()+more.Body.String(), secret) {
			t.Fatal("queue leaked private approval data")
		}
	}
	hash := page.Data[0].Hash
	body := map[string]any{"decision": "APPROVE", "expected_binding_hash": hash, "reason": "reviewed for safety"}
	path := queue + "/" + req.ID + "/decide"
	decide := func(token, etag string) *httptest.ResponseRecorder {
		return h.doBearer(t, "POST", path, token, etag, body)
	}
	expectAdminStatus(t, decide("other-project", FormatETag(1)), 404, "resource_not_found")
	expectAdminStatus(t, decide("other-org", FormatETag(1)), 404, "resource_not_found")
	expectAdminStatus(t, decide("finops", FormatETag(1)), 403, "")
	expectAdminStatus(t, h.doAuthedCookie(t, "POST", path, cookie, "", FormatETag(1), body), 403, "csrf_validation_failed")
	expectAdminStatus(t, h.doAuthedCookie(t, "POST", path, cookie, boot.CSRFToken, FormatETag(1), body), 403, "self_approval_forbidden")
	var deniedEvents int
	err = h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events
		WHERE org_id=? AND actor_id=? AND target_id=? AND event_type='mcp.self_approval_blocked'`,
		boot.OrgID, boot.OwnerPrincipalID, req.ID).Scan(&deniedEvents)
	if err != nil || deniedEvents != 1 {
		t.Fatalf("self-approval audit count = %d, error = %v", deniedEvents, err)
	}
	expectAdminStatus(t, decide("reviewer", ""), 428, "precondition_required")
	expectAdminStatus(t, decide("reviewer", "*"), 400, "invalid_if_match")
	stale := decide("reviewer", FormatETag(2))
	expectAdminStatus(t, stale, 409, "optimistic_lock_conflict")
	if stale.Header().Get("ETag") != FormatETag(1) {
		t.Fatal("missing current ETag on conflict")
	}

	body["expected_binding_hash"] = strings.Repeat("0", 64)
	expectAdminStatus(t, decide("reviewer", FormatETag(1)), 403, "parameter_hash_mismatch")
	body["expected_binding_hash"] = hash
	approved := decide("reviewer", FormatETag(1))
	expectAdminStatus(t, approved, 200, "")
	assertPayloadMatchesSchema(t, loadAdminOpenAPISpec(t), "ApprovalRequestResource", approved.Body.Bytes())
	if approved.Header().Get("ETag") != FormatETag(2) {
		t.Fatal("missing new ETag on decision")
	}
	expectAdminStatus(t, decide("reviewer", FormatETag(2)), 409, "approval_already_decided_or_expired")
	if _, err = store.Consume(ctx, req.PreApprovalToken, binding); err != nil {
		t.Fatalf("consume approved request: %v", err)
	}
	if _, err = store.Consume(ctx, req.PreApprovalToken, binding); err == nil {
		t.Fatal("token consumed twice")
	}

	body["decision"] = "REJECT"
	path = queue + "/" + second.ID + "/decide"
	expectAdminStatus(t, decide("reviewer", FormatETag(1)), 200, "")
	if _, err = store.Consume(ctx, second.PreApprovalToken, binding); err == nil {
		t.Fatal("rejected request was consumed")
	}

	expired := create()
	h.fk.Advance(2 * time.Minute)
	expiredList := h.doBearer(t, "GET", queue+"?status=EXPIRED", "reviewer", "", nil)
	expectAdminStatus(t, expiredList, 200, "")
	if !strings.Contains(expiredList.Body.String(), expired.ID) {
		t.Fatal("expired approval was missing from the expired queue")
	}
	path = queue + "/" + expired.ID + "/decide"
	expectAdminStatus(t, decide("reviewer", FormatETag(1)), 409, "approval_already_decided_or_expired")
}

func TestAdminMCPApprovalQueueBlocksDriftedSchema(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, _ := h.bootstrapOwner(t, "Approval Drift Org", "approval-drift-project")
	ctx := t.Context()
	key, err := h.vkeys.Create(ctx, mustVKeyCreateParams(boot.OrgID, boot.ProjectID, boot.EnvID, boot.OwnerPrincipalID, "approval-drift-key"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `INSERT INTO mcp_servers(id,org_id,project_id,server_slug,display_name,transport_kind,endpoint_url,allow_private_ip,allowed_cidrs_json,pinning_mode,status,version,created_at_ms,updated_at_ms)
		VALUES('mcp_drift_server',?,?, 'drift-server','Drift server','STREAMABLE_HTTP','https://mcp.example.test',0,'[]','STRICT_LOCK','ACTIVE',1,?,?)`, boot.OrgID, boot.ProjectID, h.fk.Now().UnixMilli(), h.fk.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.st.RawDB().ExecContext(ctx, `INSERT INTO mcp_tools(id,org_id,mcp_server_id,tool_name,side_effect_class,approved_schema_sha256,observed_schema_sha256,schema_drift_locked,tool_schema_json,version,created_at_ms,updated_at_ms)
		VALUES('mcp_drift_tool',?,'mcp_drift_server','delete','destructive',?,?,1,'{}',1,?,?)`, boot.OrgID, strings.Repeat("a", 64), strings.Repeat("b", 64), h.fk.Now().UnixMilli(), h.fk.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	store, err := approval.NewStore(approval.Config{DB: h.st.RawDB(), Audit: h.auditLog, Now: h.fk.Now})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(ctx, approval.Binding{OrgID: boot.OrgID, ProjectID: boot.ProjectID, VirtualKeyID: key.Summary.ID, ServerSlug: "drift-server", ToolName: "delete", Arguments: json.RawMessage(`{"id":"doc-1"}`)}, "", h.fk.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := h.authSvc.CreatePrincipal(ctx, auth.CreatePrincipalParams{OrgID: &boot.OrgID, Kind: auth.PrincipalKindHumanLocal, EmailOrSub: "drift-reviewer@example.test", Password: "StrongReviewerPass#2026!"})
	if err != nil {
		t.Fatal(err)
	}
	h.registerBearerToken("drift-reviewer", &rbac.Actor{PrincipalID: reviewer.ID, SessionOrgID: &boot.OrgID, CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleSecurityReviewer}}})
	queue := h.doBearer(t, http.MethodGet, "/admin/v1/approvals?status=PENDING", "drift-reviewer", "", nil)
	expectAdminStatus(t, queue, http.StatusOK, "")
	var page paginatedListEnvelope[approvalResource]
	mustDecodeJSON(t, queue.Body.Bytes(), &page)
	if len(page.Data) != 1 || !page.Data[0].SchemaDriftLocked {
		t.Fatalf("drift warning missing from approval queue: %+v", page.Data)
	}
	decision := h.doBearer(t, http.MethodPost, "/admin/v1/approvals/"+created.ID+"/decide", "drift-reviewer", FormatETag(1), map[string]any{"decision": "APPROVE", "expected_binding_hash": page.Data[0].Hash, "reason": "review drift"})
	expectAdminStatus(t, decision, http.StatusConflict, "mcp_schema_drift_blocked")
}
