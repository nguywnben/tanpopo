// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nguywnben/tanpopo/internal/security/audit"
)

func TestDEF040_UnverifiedConnectionDisableAndRecovery(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF040 Org", "def040-proj")

	var probeMode atomic.Int32 // 0 = 503 fail, 1 = 200 ok
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if probeMode.Load() == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama3.2"}]}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"down"}}`))
	}))
	defer upstream.Close()

	upURL, _ := url.Parse(upstream.URL)
	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "unverified-conn",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     upstream.URL,
		"data_region":      "ON_PREM",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)
	if conn.State != "CREATED_UNVERIFIED" {
		t.Fatalf("expected CREATED_UNVERIFIED initial state, got %q", conn.State)
	}

	// Disable CREATED_UNVERIFIED connection: must transition to recoverable DISABLED (not ARCHIVED!).
	recDis := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(conn.Version), map[string]any{
		"state": "DISABLED",
	})
	if recDis.Code != http.StatusOK {
		t.Fatalf("PATCH disable code=%d body=%s", recDis.Code, recDis.Body.String())
	}
	var disabled providerConnectionDTO
	mustDecodeJSON(t, recDis.Body.Bytes(), &disabled)
	if disabled.State != "DISABLED" {
		t.Fatalf("expected state=DISABLED when disabling CREATED_UNVERIFIED, got %q", disabled.State)
	}

	// Register a logical model and bind a PROVISIONED deployment to the DISABLED connection.
	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "def040-model",
		"display_name":      "DEF040 Model",
		"context_window":    8192,
		"max_output_tokens": 2048,
		"assurance_tier":    "TESTED",
	})
	if recMod.Code != http.StatusCreated {
		t.Fatalf("POST /models code=%d body=%s", recMod.Code, recMod.Body.String())
	}
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":               boot.EnvID,
		"logical_model_id":     mod.ID,
		"connection_id":        conn.ID,
		"upstream_model_name":  "llama3.2",
		"data_region":          "ON_PREM",
		"activate_immediately": false,
		"rate_card":            map[string]any{"input_token_nano_usd": 0, "output_token_nano_usd": 0},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments code=%d body=%s", recDep.Code, recDep.Body.String())
	}
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)

	// While connection is DISABLED, activating deployment fails closed.
	recActFail := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recActFail.Code != http.StatusConflict && recActFail.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected conflict/unprocessable activating deployment on DISABLED connection, got %d body=%s", recActFail.Code, recActFail.Body.String())
	}

	// Disabled connection must remain editable and testable!
	recEdit := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(disabled.Version), map[string]any{
		"name": "unverified-conn-fixed",
	})
	if recEdit.Code != http.StatusOK {
		t.Fatalf("PATCH edit DISABLED connection code=%d body=%s", recEdit.Code, recEdit.Body.String())
	}
	var edited providerConnectionDTO
	mustDecodeJSON(t, recEdit.Body.Bytes(), &edited)
	if edited.State != "DISABLED" || edited.Name != "unverified-conn-fixed" {
		t.Fatalf("unexpected edited DISABLED connection: %+v", edited)
	}

	// Failed probe keeps connection DISABLED (does not erroneously mark ACTIVE).
	recProbeFail := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recProbeFail.Code != http.StatusOK {
		t.Fatalf("POST /test (failing) code=%d body=%s", recProbeFail.Code, recProbeFail.Body.String())
	}
	var resFail connectionTestResponseDTO
	mustDecodeJSON(t, recProbeFail.Body.Bytes(), &resFail)
	if resFail.Valid {
		t.Fatalf("expected probe valid=false while upstream down")
	}

	// Once upstream recovers, Test connection promotes DISABLED -> ACTIVE, allowing deployment activation!
	probeMode.Store(1)
	recProbePass := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recProbePass.Code != http.StatusOK {
		t.Fatalf("POST /test (passing) code=%d body=%s", recProbePass.Code, recProbePass.Body.String())
	}
	var resPass connectionTestResponseDTO
	mustDecodeJSON(t, recProbePass.Body.Bytes(), &resPass)
	if !resPass.Valid {
		t.Fatalf("expected probe valid=true once upstream recovered, got %+v", resPass)
	}

	recGet := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/connections", ownerCookie, "", "", nil)
	var listBody struct {
		Data []providerConnectionDTO `json:"data"`
	}
	mustDecodeJSON(t, recGet.Body.Bytes(), &listBody)
	if len(listBody.Data) != 1 || listBody.Data[0].State != "ACTIVE" {
		t.Fatalf("expected connection recovered to ACTIVE, got %+v", listBody.Data)
	}

	recActOK := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recActOK.Code != http.StatusOK {
		t.Fatalf("expected deployment activation after connection recovery, got %d body=%s", recActOK.Code, recActOK.Body.String())
	}
}

func TestDEF035_DeploymentsFilterByProjectID(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF035 Org", "proj-a")

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m1"}]}`))
	}))
	defer up.Close()
	upURL, _ := url.Parse(up.URL)
	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "c1",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     up.URL,
		"data_region":      "US",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
	})
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "m1", "display_name": "M1", "context_window": 4096, "max_output_tokens": 1024, "assurance_tier": "DECLARED",
	})
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id": boot.EnvID, "logical_model_id": mod.ID, "connection_id": conn.ID,
		"upstream_model_name": "m1", "data_region": "US", "activate_immediately": false,
		"rate_card": map[string]any{"input_token_nano_usd": 0, "output_token_nano_usd": 0},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments code=%d body=%s", recDep.Code, recDep.Body.String())
	}

	// Create second project with zero environments/deployments.
	recProjB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "proj-b", "name": "Project B",
	})
	if recProjB.Code != http.StatusCreated {
		t.Fatalf("POST /projects code=%d body=%s", recProjB.Code, recProjB.Body.String())
	}
	var projB projectDTO
	mustDecodeJSON(t, recProjB.Body.Bytes(), &projB)

	// Filtering by project_id=projB must return ZERO deployments (never leak projA deployments!).
	recListB := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/deployments?project_id="+url.QueryEscape(projB.ID), ownerCookie, "", "", nil)
	if recListB.Code != http.StatusOK {
		t.Fatalf("GET /deployments?project_id=projB code=%d body=%s", recListB.Code, recListB.Body.String())
	}
	var bodyB struct {
		Data []modelDeploymentDTO `json:"data"`
	}
	mustDecodeJSON(t, recListB.Body.Bytes(), &bodyB)
	if len(bodyB.Data) != 0 {
		t.Fatalf("expected 0 deployments for empty project %s, got %d (%+v)", projB.ID, len(bodyB.Data), bodyB.Data)
	}

	// Filtering by project_id=projA returns only projA's deployment.
	recListA := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/deployments?project_id="+url.QueryEscape(boot.ProjectID), ownerCookie, "", "", nil)
	if recListA.Code != http.StatusOK {
		t.Fatalf("GET /deployments?project_id=projA code=%d body=%s", recListA.Code, recListA.Body.String())
	}
	var bodyA struct {
		Data []modelDeploymentDTO `json:"data"`
	}
	mustDecodeJSON(t, recListA.Body.Bytes(), &bodyA)
	if len(bodyA.Data) != 1 || bodyA.Data[0].EnvID != boot.EnvID {
		t.Fatalf("expected 1 deployment for %s, got %+v", boot.ProjectID, bodyA.Data)
	}
}

func TestDEF041_OrgErasurePhase3FailureAndResumableFinalization(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Primary Org", "primary-proj")
	ctx := context.Background()

	// Helper creates a throwaway organization + secret + virtual key.
	createOrg := func(slug, name string) (organizationDTO, string) {
		recCreate := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/organizations", ownerCookie, boot.CSRFToken, "", map[string]any{
			"slug":             slug,
			"name":             name,
			"min_plugin_trust": "ANY_APPROVED",
		})
		if recCreate.Code != http.StatusCreated {
			t.Fatalf("POST /organizations code=%d body=%s", recCreate.Code, recCreate.Body.String())
		}
		var org organizationDTO
		mustDecodeJSON(t, recCreate.Body.Bytes(), &org)
		if org.Status != "ACTIVE" {
			t.Fatalf("expected new organization status=ACTIVE, got %q", org.Status)
		}
		return org, org.ID
	}

	// Inject a SQLite Phase 3 failure on DELETE FROM organizations for org1.
	org1, org1ID := createOrg("erasure-org-1", "Erasure Org 1")
	if _, err := h.st.RawDB().ExecContext(ctx, `
		CREATE TRIGGER inject_phase3_org_delete_fail
		BEFORE DELETE ON organizations
		WHEN OLD.id = '`+org1ID+`'
		BEGIN
			SELECT RAISE(ABORT, 'injected phase 3 delete failure');
		END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	recDelFail := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/organizations/"+org1ID, ownerCookie, boot.CSRFToken, FormatETag(org1.Version), map[string]any{
		"confirm_slug": org1.Slug,
		"reason":            "phase 3 fault injection test",
	})
	if recDelFail.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when Phase 3 fails after DEK shred, got %d body=%s", recDelFail.Code, recDelFail.Body.String())
	}

	// Org must remain visible in GET /organizations with status="DELETING" rather than appearing ACTIVE!
	recList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/organizations", ownerCookie, "", "", nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /organizations code=%d body=%s", recList.Code, recList.Body.String())
	}
	var listResp struct {
		Data []organizationDTO `json:"data"`
	}
	mustDecodeJSON(t, recList.Body.Bytes(), &listResp)
	var foundDeleting *organizationDTO
	for i := range listResp.Data {
		if listResp.Data[i].ID == org1ID {
			foundDeleting = &listResp.Data[i]
		}
	}
	if foundDeleting == nil || foundDeleting.Status != "DELETING" {
		t.Fatalf("expected stranded org with status=DELETING in directory, got %+v", listResp.Data)
	}

	// Drop fault injection trigger and exercise concurrent finalize-erasure calls.
	if _, err := h.st.RawDB().ExecContext(ctx, `DROP TRIGGER inject_phase3_org_delete_fail`); err != nil {
		t.Fatalf("drop trigger: %v", err)
	}
	var wg sync.WaitGroup
	codes := make([]int, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/organizations/"+org1ID+"/finalize-erasure", ownerCookie, boot.CSRFToken, "", nil)
			codes[idx] = rec.Code
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("concurrent finalize-erasure[%d] got code=%d (codes=%v)", i, c, codes)
		}
	}

	// Verify exactly ONE organization.crypto_shredded audit event recorded for org1ID and chain valid!
	var auditCount int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE target_id = ? AND event_type = 'organization.crypto_shredded'`, org1ID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected exactly 1 organization.crypto_shredded audit event for %s under concurrent finalization, got %d", org1ID, auditCount)
	}
	chainRes, err := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{ChainKeys: []string{org1ID}})
	if err != nil || !chainRes.Valid {
		t.Fatalf("expected valid JCS audit chain for %s, got err=%v res=%+v", org1ID, err, chainRes)
	}

	// Now test server startup sweeper ResumePendingOrgDeletions on a second stranded DELETING org!
	org2, org2ID := createOrg("erasure-org-2", "Erasure Org 2")
	if _, err := h.st.RawDB().ExecContext(ctx, `
		CREATE TRIGGER inject_phase3_org_delete_fail_2
		BEFORE DELETE ON organizations
		WHEN OLD.id = '`+org2ID+`'
		BEGIN
			SELECT RAISE(ABORT, 'injected phase 3 delete failure 2');
		END;`); err != nil {
		t.Fatalf("create trigger 2: %v", err)
	}
	recDel2 := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/organizations/"+org2ID, ownerCookie, boot.CSRFToken, FormatETag(org2.Version), map[string]any{
		"confirm_slug": org2.Slug,
		"reason":            "startup sweeper test",
	})
	if recDel2.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on org2 phase 3 failure, got %d", recDel2.Code)
	}
	if _, err := h.st.RawDB().ExecContext(ctx, `DROP TRIGGER inject_phase3_org_delete_fail_2`); err != nil {
		t.Fatalf("drop trigger 2: %v", err)
	}
	if err := h.srv.ResumePendingOrgDeletions(ctx); err != nil {
		t.Fatalf("ResumePendingOrgDeletions: %v", err)
	}

	var remaining int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations WHERE id IN (?, ?)`, org1ID, org2ID).Scan(&remaining); err != nil {
		t.Fatalf("check remaining orgs: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected both finalized orgs removed, remaining=%d", remaining)
	}
	var auditCount2 int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events WHERE target_id = ? AND event_type = 'organization.crypto_shredded'`, org2ID).Scan(&auditCount2); err != nil {
		t.Fatalf("count audit events org2: %v", err)
	}
	if auditCount2 != 1 {
		t.Fatalf("expected 1 organization.crypto_shredded event after startup sweeper, got %d", auditCount2)
	}
}

func TestDEF037_VirtualKeyCreatedAuditPaginationAndNoSecretLeak(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF037 Audit Org", "def037-proj")

	issuedSecrets := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", ownerCookie, boot.CSRFToken, "", map[string]any{
			"project_id": boot.ProjectID,
			"env_id":     boot.EnvID,
			"name":       "def037-key-" + string(rune('a'+i)),
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /virtual-keys code=%d body=%s", rec.Code, rec.Body.String())
		}
		var body struct {
			ID        string `json:"id"`
			SecretKey string `json:"secret_key"`
		}
		mustDecodeJSON(t, rec.Body.Bytes(), &body)
		issuedSecrets = append(issuedSecrets, body.SecretKey)
	}

	// Paginated GET /audit-events?action=virtual_key.created must surface all 4 events without leaking plaintext secret keys!
	page1 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/audit-events?action=virtual_key.created&limit=2", ownerCookie, "", "", nil)
	if page1.Code != http.StatusOK {
		t.Fatalf("GET /audit-events?action=virtual_key.created code=%d body=%s", page1.Code, page1.Body.String())
	}
	var p1Body struct {
		Data []struct {
			ID       string `json:"id"`
			Action   string `json:"action"`
			TargetID string `json:"target_id"`
		} `json:"data"`
		NextCursor string `json:"next_cursor"`
	}
	mustDecodeJSON(t, page1.Body.Bytes(), &p1Body)
	if len(p1Body.Data) != 2 || p1Body.NextCursor == "" {
		t.Fatalf("expected page1 count=2 with next_cursor, got %+v", p1Body)
	}
	page2 := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/audit-events?action=virtual_key.created&limit=2&cursor="+url.QueryEscape(p1Body.NextCursor), ownerCookie, "", "", nil)
	if page2.Code != http.StatusOK {
		t.Fatalf("page2 code=%d body=%s", page2.Code, page2.Body.String())
	}
	rawAudit := page1.Body.String() + page2.Body.String()
	for _, sec := range issuedSecrets {
		if sec != "" && strings.Contains(rawAudit, sec) {
			t.Fatalf("audit response leaked issued plaintext virtual key secret!")
		}
	}
	var p2Body struct {
		Data []struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		} `json:"data"`
	}
	mustDecodeJSON(t, page2.Body.Bytes(), &p2Body)
	if len(p2Body.Data) != 2 {
		t.Fatalf("expected 2 more virtual_key.created events on page2, got %d", len(p2Body.Data))
	}
	for _, ev := range append(p1Body.Data, struct {
		ID       string `json:"id"`
		Action   string `json:"action"`
		TargetID string `json:"target_id"`
	}{ID: p2Body.Data[0].ID, Action: p2Body.Data[0].Action}) {
		if ev.Action != "virtual_key.created" {
			t.Fatalf("expected canonical action=virtual_key.created, got %q", ev.Action)
		}
	}
}

func TestRound3PlaygroundKeyRouteRestrictionAndCap(t *testing.T) {
	h := newPart2TestHarness(t, 21)
	boot, ownerCookie := h.bootstrapOwner(t, "Playground Security Org", "pg-sec-proj")
	ctx := context.Background()

	var firstSessionID string
	for i := 0; i < maxActivePlaygroundKeys; i++ {
		rec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/playground/sessions", ownerCookie, boot.CSRFToken, "", map[string]any{
			"project_id": boot.ProjectID,
			"env_id":     boot.EnvID,
			"ttl_seconds": 900,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /playground/sessions #%d code=%d body=%s", i, rec.Code, rec.Body.String())
		}
		if i == 0 {
			var body struct {
				SessionID string `json:"session_id"`
			}
			mustDecodeJSON(t, rec.Body.Bytes(), &body)
			firstSessionID = body.SessionID
		}
	}

	var allowedPlanes, allowedRoutesJSON string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT allowed_planes, allowed_routes_json FROM virtual_keys WHERE id = ?`, firstSessionID).Scan(&allowedPlanes, &allowedRoutesJSON); err != nil {
		t.Fatalf("query playground vkey %s: %v", firstSessionID, err)
	}
	if allowedPlanes != "PROVIDER" || !strings.Contains(allowedRoutesJSON, `"/v1/chat/completions"`) || strings.Contains(allowedRoutesJSON, `"*"`) {
		t.Fatalf("expected playground key restricted to PROVIDER + [/v1/chat/completions], got planes=%q routes=%q", allowedPlanes, allowedRoutesJSON)
	}

	over := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/playground/sessions", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
		"ttl_seconds": 900,
	})
	if over.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 rate_limit_exceeded beyond active playground key cap, got %d body=%s", over.Code, over.Body.String())
	}
}

func TestSecurityAudit_FourEyesDeletionFullTeardown(t *testing.T) {
	h := newPart2TestHarness(t, 22)
	boot, ownerCookie := h.bootstrapOwner(t, "FourEyes Delete Org", "fe-base-proj")
	ctx := context.Background()

	// Create target project + env + active Virtual Key.
	recPrj := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "fe-target-proj",
		"name": "FE Target Proj",
	})
	if recPrj.Code != http.StatusCreated {
		t.Fatalf("POST /projects code=%d body=%s", recPrj.Code, recPrj.Body.String())
	}
	var prj projectDTO
	mustDecodeJSON(t, recPrj.Body.Bytes(), &prj)

	recEnv := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+prj.ID+"/environments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "prod",
		"name": "Prod",
	})
	if recEnv.Code != http.StatusCreated {
		t.Fatalf("POST /environments code=%d body=%s", recEnv.Code, recEnv.Body.String())
	}
	var env environmentDTO
	mustDecodeJSON(t, recEnv.Body.Bytes(), &env)

	recKey := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id": prj.ID,
		"env_id":     env.ID,
		"name":       "fe-active-key",
	})
	if recKey.Code != http.StatusCreated {
		t.Fatalf("POST /virtual-keys code=%d body=%s", recKey.Code, recKey.Body.String())
	}
	var vk struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recKey.Body.Bytes(), &vk)

	// Add second SECURITY_REVIEWER to satisfy Four-Eyes dual approval & enable four_eyes on org.
	recMem := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email": "reviewer2@foureyes.test",
		"kind":  "HUMAN",
		"roles": []string{"ORG_OWNER"},
	})
	if recMem.Code != http.StatusCreated {
		t.Fatalf("POST /members code=%d body=%s", recMem.Code, recMem.Body.String())
	}
	var mem struct {
		EnrollmentToken string `json:"enrollment_token"`
	}
	mustDecodeJSON(t, recMem.Body.Bytes(), &mem)
	recEnroll := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
		"token":    mem.EnrollmentToken,
		"password": "ReviewerStrong!2026",
	})
	if recEnroll.Code != http.StatusOK {
		t.Fatalf("POST /auth/enroll code=%d body=%s", recEnroll.Code, recEnroll.Body.String())
	}
	var enrollBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeJSON(t, recEnroll.Body.Bytes(), &enrollBody)
	reviewerCookie := recEnroll.Result().Cookies()[0]
	for _, c := range recEnroll.Result().Cookies() {
		if strings.Contains(c.Name, "tanpopo_admin_session") {
			reviewerCookie = c
			break
		}
	}

	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE organizations SET require_four_eyes_approval = 1 WHERE id = ?`, boot.OrgID); err != nil {
		t.Fatalf("enable four eyes on org: %v", err)
	}

	// Owner initiates project delete -> returns 202 Pending Approval change request.
	recDel := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/projects/"+prj.ID, ownerCookie, boot.CSRFToken, FormatETag(prj.Version), map[string]any{
		"confirm_slug": prj.Slug,
		"reason":       "four-eyes full teardown verification",
	})
	if recDel.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted initiating four-eyes project delete, got %d body=%s", recDel.Code, recDel.Body.String())
	}
	var cr struct {
		ID                 string `json:"id"`
		ApprovalBindingHash string `json:"approval_binding_hash"`
		Version            int64  `json:"version"`
	}
	mustDecodeJSON(t, recDel.Body.Bytes(), &cr)

	// Reviewer 2 approves change request -> MUST execute full project teardown + revoke virtual keys!
	recApprove := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/change-requests/"+cr.ID+"/decide", reviewerCookie, enrollBody.CSRFToken, FormatETag(cr.Version), map[string]any{
		"decision":              "APPROVE",
		"expected_binding_hash": cr.ApprovalBindingHash,
		"reason":                "approved project crypto-shred teardown",
	})
	if recApprove.Code != http.StatusOK {
		t.Fatalf("POST /change-requests/decide code=%d body=%s", recApprove.Code, recApprove.Body.String())
	}

	var prjCount, activeKeyCount int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id = ?`, prj.ID).Scan(&prjCount); err != nil {
		t.Fatalf("check remaining project: %v", err)
	}
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM virtual_keys WHERE id = ? AND state = 'CREATED_ACTIVE'`, vk.ID).Scan(&activeKeyCount); err != nil {
		t.Fatalf("check virtual key state: %v", err)
	}
	if prjCount != 0 || activeKeyCount != 0 {
		t.Fatalf("expected approved four-eyes project deletion to remove project & revoke active keys; prjCount=%d activeKeyCount=%d", prjCount, activeKeyCount)
	}
}

func TestSecurityAudit_TeamAdminCannotBindOrgOwnedSecretToProjectConnection(t *testing.T) {
	h := newPart2TestHarness(t, 23)
	boot, ownerCookie := h.bootstrapOwner(t, "SecScope Org", "sec-scope-proj")

	// 1. Seed a built-in provider adapter row & opt-in via org extension policies for child project use.
	adapterID := seedLocalOpenAIAdapter(t, h, "api.openai.com")
	recPol := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/organizations/"+boot.OrgID+"/extension-policies", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"min_plugin_trust": "ANY_APPROVED",
		"policies": []map[string]any{
			{"package_id": "tanpopo.local_openai", "enabled": true},
		},
	})
	if recPol.Code != http.StatusOK {
		t.Fatalf("PUT /extension-policies code=%d body=%s", recPol.Code, recPol.Body.String())
	}

	// 2. ORG_OWNER creates an org-owned secret (owner_project_id NULL) and a project-owned secret.
	recOrgSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "org-master-openai-key",
		"secret_kind":  "API_KEY",
		"secret_value": "sk-org-secret-do-not-expose",
	})
	if recOrgSec.Code != http.StatusCreated {
		t.Fatalf("create org secret code=%d body=%s", recOrgSec.Code, recOrgSec.Body.String())
	}
	var orgSec struct {
		CredentialID string `json:"credential_id"`
	}
	mustDecodeJSON(t, recOrgSec.Body.Bytes(), &orgSec)

	recProjSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "proj-scoped-openai-key",
		"project_id":   boot.ProjectID,
		"secret_kind":  "API_KEY",
		"secret_value": "sk-proj-secret-allowed",
	})
	if recProjSec.Code != http.StatusCreated {
		t.Fatalf("create project secret code=%d body=%s", recProjSec.Code, recProjSec.Body.String())
	}
	var projSec struct {
		CredentialID string `json:"credential_id"`
	}
	mustDecodeJSON(t, recProjSec.Body.Bytes(), &projSec)

	// 3. Create & enroll a project-scoped TEAM_ADMIN bound only to boot.ProjectID.
	recMem := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email":       "ta-secret@tanpopo.test",
		"kind":        "HUMAN",
		"roles":       []string{"TEAM_ADMIN"},
		"project_ids": []string{boot.ProjectID},
	})
	if recMem.Code != http.StatusCreated {
		t.Fatalf("POST /members code=%d body=%s", recMem.Code, recMem.Body.String())
	}
	var mem struct {
		EnrollmentToken string `json:"enrollment_token"`
	}
	mustDecodeJSON(t, recMem.Body.Bytes(), &mem)
	recEnroll := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
		"token":    mem.EnrollmentToken,
		"password": "TeamAdminStrong!2026",
	})
	if recEnroll.Code != http.StatusOK {
		t.Fatalf("POST /auth/enroll code=%d body=%s", recEnroll.Code, recEnroll.Body.String())
	}
	var taBody struct {
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeJSON(t, recEnroll.Body.Bytes(), &taBody)
	taCookie := recEnroll.Result().Cookies()[0]
	for _, c := range recEnroll.Result().Cookies() {
		if strings.Contains(c.Name, "tanpopo_admin_session") {
			taCookie = c
			break
		}
	}

	// 4. TEAM_ADMIN MUST NOT bind org-owned secret on POST /connections -> uniform 404 resource_not_found.
	recCreateBlocked := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", taCookie, taBody.CSRFToken, "", map[string]any{
		"project_id":   boot.ProjectID,
		"name":         "ta-org-secret-conn",
		"adapter_id":   adapterID,
		"endpoint_url": "https://api.openai.com/v1",
		"billing_mode": "COMMERCIAL_API",
		"data_region":  "US",
		"secret_bindings": map[string]string{
			"api_key": orgSec.CredentialID,
		},
	})
	if recCreateBlocked.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when TEAM_ADMIN binds org-owned secret on POST /connections, got %d body=%s", recCreateBlocked.Code, recCreateBlocked.Body.String())
	}

	// 5. TEAM_ADMIN CAN create a project connection using their own project-owned secret -> 201 Created.
	recCreateOK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", taCookie, taBody.CSRFToken, "", map[string]any{
		"project_id":   boot.ProjectID,
		"name":         "ta-proj-secret-conn",
		"adapter_id":   adapterID,
		"endpoint_url": "https://api.openai.com/v1",
		"billing_mode": "COMMERCIAL_API",
		"data_region":  "US",
		"secret_bindings": map[string]string{
			"api_key": projSec.CredentialID,
		},
	})
	if recCreateOK.Code != http.StatusCreated {
		t.Fatalf("expected 201 when TEAM_ADMIN binds project-owned secret, got %d body=%s", recCreateOK.Code, recCreateOK.Body.String())
	}
	var conn struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	mustDecodeJSON(t, recCreateOK.Body.Bytes(), &conn)

	// 6. TEAM_ADMIN MUST NOT rebind that connection to an org-owned secret via PATCH /connections/{id} -> 404.
	recPatchBlocked := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, taCookie, taBody.CSRFToken, FormatETag(conn.Version), map[string]any{
		"secret_bindings": map[string]string{
			"api_key": orgSec.CredentialID,
		},
	})
	if recPatchBlocked.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when TEAM_ADMIN rebinds org-owned secret on PATCH /connections, got %d body=%s", recPatchBlocked.Code, recPatchBlocked.Body.String())
	}

	// 7. ORG_OWNER retains authority to bind the org-owned secret to the connection -> 200 OK.
	recPatchOwnerOK := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/connections/"+conn.ID, ownerCookie, boot.CSRFToken, FormatETag(conn.Version), map[string]any{
		"secret_bindings": map[string]string{
			"api_key": orgSec.CredentialID,
		},
	})
	if recPatchOwnerOK.Code != http.StatusOK {
		t.Fatalf("expected 200 when ORG_OWNER binds org-owned secret on PATCH /connections, got %d body=%s", recPatchOwnerOK.Code, recPatchOwnerOK.Body.String())
	}
}

func TestCodeReview_PlaygroundAndMCPRejectDeletingTenant(t *testing.T) {
	h := newPart2TestHarness(t, 24)
	boot, ownerCookie := h.bootstrapOwner(t, "Deleting Tenant Org", "del-proj")
	ctx := context.Background()

	// Mark project as DELETING via raw UPDATE (simulating in-flight erasure Phase 1 fence).
	if _, err := h.st.RawDB().ExecContext(ctx, `UPDATE projects SET status = 'DELETING' WHERE id = ?`, boot.ProjectID); err != nil {
		t.Fatalf("fence project to DELETING: %v", err)
	}

	recPG := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/playground/sessions", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recPG.Code != http.StatusConflict {
		t.Fatalf("expected 409 on POST /playground/sessions for DELETING project, got %d body=%s", recPG.Code, recPG.Body.String())
	}

	recMCP := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/mcp/servers", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id":   boot.ProjectID,
		"slug":         "deleting-mcp",
		"name":         "Deleting MCP",
		"transport":    "streamable_http",
		"endpoint_url": "https://mcp.example.com/mcp",
	})
	if recMCP.Code != http.StatusConflict {
		t.Fatalf("expected 409 on POST /mcp/servers for DELETING project, got %d body=%s", recMCP.Code, recMCP.Body.String())
	}
}
