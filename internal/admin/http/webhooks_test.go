// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/security/vault"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestAdminWebhookSubscriptions(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "Webhook Org", "webhook-project")
	ctx := t.Context()
	h.srv.hostResolver = &countingResolver{fn: func(host string) ([]netip.Addr, error) {
		public := netip.MustParseAddr("93.184.216.34")
		if host == "mixed.example.test" {
			return []netip.Addr{public, netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{public}, nil
	}}
	otherOrg, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "webhook-other", DisplayName: "Other Organization"})
	if err != nil {
		t.Fatal(err)
	}
	seal := func(org string, project *string, kind string, audience *string) string {
		t.Helper()
		secret, err := h.vault.SealSecret(ctx, vault.SealSecretParams{
			OrgID: org, OwnerProjectID: project, SecretKind: kind, BoundAudience: audience,
			Name: "signing fixture", Plaintext: []byte("private-signing-fixture"),
		})
		if err != nil {
			t.Fatal(err)
		}
		return secret.ID
	}
	aud := "https://hooks.example.test"
	hmacID := seal(boot.OrgID, &boot.ProjectID, vault.SecretKindWebhookHMAC, &aud)
	wrongKind := seal(boot.OrgID, &boot.ProjectID, vault.SecretKindAPIKey, nil)
	foreignSecret := seal(otherOrg.ID, nil, vault.SecretKindWebhookHMAC, nil)
	orgSecret := seal(boot.OrgID, nil, vault.SecretKindWebhookHMAC, nil)

	const path = "/admin/v1/webhooks"
	const rawTarget = "https://hooks.example.test/private-receiver-path"
	body := map[string]any{"target_url": rawTarget, "project_id": boot.ProjectID, "events": []string{"budget.threshold", "mcp.approval.pending"}, "signing_credential_id": hmacID}
	create := func(csrf string) *httptest.ResponseRecorder {
		return h.doAuthedCookie(t, "POST", path, cookie, csrf, "", body)
	}
	expectAdminStatus(t, h.doPublic(t, "GET", path, nil), 401, "")
	expectAdminStatus(t, create(""), 403, "csrf_validation_failed")
	for _, role := range []string{rbac.RoleFinOpsViewer, rbac.RoleSecurityReviewer} {
		h.registerBearerToken(role, &rbac.Actor{PrincipalID: boot.OwnerPrincipalID, SessionOrgID: &boot.OrgID, CredentialPlane: rbac.CredentialPlaneAdminService,
			Bindings: []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: role}}})
		expectAdminStatus(t, h.doBearer(t, "POST", path, role, "", body), 403, "")
	}
	expectAdminStatus(t, h.doBearer(t, "GET", path, rbac.RoleFinOpsViewer, "", nil), 403, "")
	for _, bad := range []string{"http://169.254.169.254/latest", "http://127.0.0.1:6379", "https://169.254.169.254", "https://[::1]", "https://mixed.example.test/sink", "https://secret@hooks.example.test/", "https://hooks.example.test/?token=query-secret", "https://hooks.example.test/#secret"} {
		body["target_url"] = bad
		expectAdminStatus(t, create(boot.CSRFToken), 400, "")
	}
	body["target_url"] = "https://another.example.test/private"
	expectAdminStatus(t, create(boot.CSRFToken), 400, "credential_audience_mismatch")
	body["target_url"] = rawTarget

	for _, events := range [][]string{nil, {"budget.threshold", "budget.threshold"}, {"invalid.event"}} {
		body["events"] = events
		expectAdminStatus(t, create(boot.CSRFToken), 400, "invalid_request")
	}
	body["events"] = []string{"budget.threshold", "mcp.approval.pending"}
	for _, id := range []string{wrongKind, foreignSecret, orgSecret} {
		body["signing_credential_id"] = id
		expectAdminStatus(t, create(boot.CSRFToken), 404, "resource_not_found")
	}
	body["signing_credential_id"] = hmacID
	delete(body, "project_id")
	expectAdminStatus(t, create(boot.CSRFToken), 404, "resource_not_found")
	body["project_id"] = boot.ProjectID
	body["org_id"] = otherOrg.ID
	expectAdminStatus(t, create(boot.CSRFToken), 403, "org_id_spoofing_forbidden")
	delete(body, "org_id")

	created := create(boot.CSRFToken)
	expectAdminStatus(t, created, 201, "")
	if created.Header().Get("ETag") != FormatETag(1) {
		t.Fatal("missing subscription ETag")
	}
	assertPayloadMatchesSchema(t, loadAdminOpenAPISpec(t), "WebhookSubscriptionResource", created.Body.Bytes())
	var resource webhookResource
	mustDecodeJSON(t, created.Body.Bytes(), &resource)
	var target, targetCredential, secret, events string
	err = h.st.RawDB().QueryRowContext(ctx, `SELECT target_url,target_url_credential_id,signing_secret_credential_id,event_types_json
		FROM webhook_subscriptions WHERE org_id=? AND project_id=? AND id=? AND enabled=1`, boot.OrgID, boot.ProjectID, resource.ID).Scan(&target, &targetCredential, &secret, &events)
	if err != nil || target != aud || targetCredential == "" || secret != hmacID || events != `["budget.threshold","mcp.approval.pending"]` {
		t.Fatalf("dispatcher subscription mismatch, query error = %v", err)
	}
	listed := h.doAuthedCookie(t, "GET", path, cookie, "", "", nil)
	expectAdminStatus(t, listed, 200, "")
	var page paginatedListEnvelope[webhookResource]
	mustDecodeJSON(t, listed.Body.Bytes(), &page)
	if len(page.Data) != 1 || page.Data[0].ID != resource.ID || len(page.Data[0].Events) != 2 || page.Data[0].TargetOrigin != aud {
		t.Fatalf("unexpected webhook page %+v", page)
	}
	for _, sensitive := range []string{"private-receiver-path", "query-secret", "private-signing-fixture", hmacID} {
		if strings.Contains(created.Body.String()+listed.Body.String(), sensitive) {
			t.Fatal("subscription response exposed private data")
		}
	}

	foreign := h.doAuthedCookie(t, "GET", path+"?org_id="+otherOrg.ID, cookie, "", "", nil)
	expectAdminStatus(t, foreign, 200, "")
	if strings.Contains(foreign.Body.String(), resource.ID) {
		t.Fatal("cross-tenant subscription disclosure")
	}

	h.registerBearerToken("mixed-bindings", &rbac.Actor{
		PrincipalID: boot.OwnerPrincipalID, SessionOrgID: &boot.OrgID, CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings: []rbac.RoleBinding{
			{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: "other-project", RoleName: rbac.RoleSecurityReviewer},
			{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: boot.ProjectID, RoleName: rbac.RoleDeveloper},
		},
	})
	mixed := h.doBearer(t, "GET", path, "mixed-bindings", "", nil)
	expectAdminStatus(t, mixed, 200, "")
	if strings.Contains(mixed.Body.String(), resource.ID) {
		t.Fatal("project read leaked through a less privileged binding")
	}
}

func TestKillSwitchQueuesOrganizationScopedWebhook(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "Kill Switch Event Org", "kill-switch-event-project")
	var orgID, projectID, eventType string
	var payload map[string]any
	reg, err := killswitch.NewRegistry(t.Context(), killswitch.RegistryConfig{
		DB: h.st.RawDB(), Audit: h.auditLog,
		RecordWebhookTx: func(_ context.Context, _ *sql.Tx, org, project, _ string, event string, body []byte) error {
			orgID, projectID, eventType = org, project, event
			return json.Unmarshal(body, &payload)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.killSwitch = reg
	h.srv.killSwitch = reg
	response := h.doAuthedCookie(t, "POST", "/admin/v1/kill-switches", cookie, boot.CSRFToken, "", map[string]any{
		"target_level": "PROJECT", "target_id": boot.ProjectID, "reason": "security incident response", "abort_active_streams": true,
		"confirm_slug": "kill-switch-event-project",
	})
	expectAdminStatus(t, response, 200, "")
	if orgID != boot.OrgID || projectID != boot.ProjectID || eventType != "kill_switch.triggered" {
		t.Fatalf("webhook scope/type = %q/%q/%q", orgID, projectID, eventType)
	}
	if payload["target_id"] != boot.ProjectID || payload["target_level"] != string(killswitch.LevelProjectOrOrgFreeze) || payload["severed_streams_count"] != nil {
		t.Fatalf("unexpected kill-switch webhook payload: %#v", payload)
	}
}
