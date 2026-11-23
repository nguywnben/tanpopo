// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/ingress/a2a"
	"github.com/nguywnben/tanpopo/internal/ops/backup"
	"github.com/nguywnben/tanpopo/internal/provider/catalog"
	"github.com/nguywnben/tanpopo/internal/routing/resolver"
	"github.com/nguywnben/tanpopo/internal/security/audit"
)

func TestDEF002_DeploymentActivationAndConnectionHealthConsistency(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF002 Health Org", "def002-proj")

	var mode atomic.Int32 // 0 = 500 unreachable/server fail, 1 = 200 ok, 2 = 401 unauthorized, 3 = closed/unreachable
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o-mini"}]}`))
		case 2:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid key"}}`))
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"service down"}}`))
		}
	}))
	defer upstream.Close()
	upURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())

	recSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "def002-key",
		"secret_kind":  "API_KEY",
		"secret_value": "sk-def002-test-1234",
	})
	if recSec.Code != http.StatusCreated {
		t.Fatalf("POST /secrets code=%d body=%s", recSec.Code, recSec.Body.String())
	}
	var secMeta secretMetadataDTO
	mustDecodeJSON(t, recSec.Body.Bytes(), &secMeta)

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "def002-conn",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     upstream.URL,
		"data_region":      "US",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
		"secret_bindings":  map[string]string{"api_key": secMeta.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "def002-model",
		"display_name":      "DEF002 Model",
		"context_window":    128000,
		"max_output_tokens": 4096,
		"assurance_tier":    "VERIFIED",
	})
	if recMod.Code != http.StatusCreated {
		t.Fatalf("POST /models code=%d body=%s", recMod.Code, recMod.Body.String())
	}
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	// 1. Creating an active deployment directly (default activate_immediately=true or explicit true)
	// on an unverified connection must be rejected with 422 connection_not_active.
	for _, explicitTrue := range []bool{false, true} {
		payload := map[string]any{
			"env_id":              boot.EnvID,
			"logical_model_id":    mod.ID,
			"connection_id":       conn.ID,
			"upstream_model_name": "gpt-4o-mini",
			"data_region":         "US",
			"rate_card": map[string]any{
				"input_token_nano_usd":  150,
				"output_token_nano_usd": 600,
			},
		}
		if explicitTrue {
			payload["activate_immediately"] = true
		}
		recBad := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", payload)
		if recBad.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 creating active deployment on unverified conn (explicitTrue=%t), got %d body=%s", explicitTrue, recBad.Code, recBad.Body.String())
		}
		var envErr CanonicalErrorEnvelope
		mustDecodeJSON(t, recBad.Body.Bytes(), &envErr)
		if envErr.Error.Code != "connection_not_active" {
			t.Fatalf("expected connection_not_active code, got %q", envErr.Error.Code)
		}
	}

	// 2. Creating a PROVISIONED deployment with activate_immediately=false succeeds cleanly.
	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":               boot.EnvID,
		"logical_model_id":     mod.ID,
		"connection_id":        conn.ID,
		"upstream_model_name":  "gpt-4o-mini",
		"data_region":          "US",
		"activate_immediately": false,
		"rate_card": map[string]any{
			"input_token_nano_usd":  150,
			"output_token_nano_usd": 600,
		},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments activate_immediately=false code=%d body=%s", recDep.Code, recDep.Body.String())
	}
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)
	if dep.State != catalog.DeployStateProvisioned || dep.ConnectionState != "CREATED_UNVERIFIED" {
		t.Fatalf("expected PROVISIONED deployment on CREATED_UNVERIFIED conn, got %+v", dep)
	}

	// 3. Failing initial test probe keeps connection unverified and blocks PATCH ACTIVE_HEALTHY.
	mode.Store(0)
	recFailInit := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recFailInit.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test (fail) code=%d body=%s", recFailInit.Code, recFailInit.Body.String())
	}
	var failProbe connectionTestResponseDTO
	mustDecodeJSON(t, recFailInit.Body.Bytes(), &failProbe)
	if failProbe.Valid {
		t.Fatalf("expected valid=false on 503 upstream probe")
	}

	recBlockPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recBlockPatch.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 activating deployment after failed probe, got %d body=%s", recBlockPatch.Code, recBlockPatch.Body.String())
	}

	// 4. Passing probe transitions connection ACTIVE and allows deployment activation + route explain selection.
	mode.Store(1)
	recPass := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recPass.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test (pass) code=%d body=%s", recPass.Code, recPass.Body.String())
	}
	var passProbe connectionTestResponseDTO
	mustDecodeJSON(t, recPass.Body.Bytes(), &passProbe)
	if !passProbe.Valid {
		t.Fatalf("expected valid=true on healthy upstream probe, got %+v", passProbe)
	}

	recActivate := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recActivate.Code != http.StatusOK {
		t.Fatalf("expected 200 activating deployment on ACTIVE connection, got %d body=%s", recActivate.Code, recActivate.Body.String())
	}
	mustDecodeJSON(t, recActivate.Body.Bytes(), &dep)
	if dep.State != catalog.DeployStateActiveHealthy || dep.ConnectionState != "ACTIVE" {
		t.Fatalf("expected ACTIVE_HEALTHY / ACTIVE, got %+v", dep)
	}

	recRoute := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":                 "def002-route",
		"env_id":               boot.EnvID,
		"strategy":             "PRIORITY_FAILOVER",
		"allowed_data_regions": []string{"US"},
		"targets":              []map[string]any{{"deployment_id": dep.ID, "priority_order": 1, "weight": 100}},
	})
	if recRoute.Code != http.StatusCreated {
		t.Fatalf("POST /routes code=%d body=%s", recRoute.Code, recRoute.Body.String())
	}

	// 5. Subsequent failing probe with 503 transitions ACTIVE -> DEGRADED, then recovery transitions DEGRADED -> ACTIVE.
	mode.Store(0)
	recDegProbe := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recDegProbe.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test (degrade) code=%d body=%s", recDegProbe.Code, recDegProbe.Body.String())
	}
	recListDeps := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/deployments", ownerCookie, "", "", nil)
	if recListDeps.Code != http.StatusOK {
		t.Fatalf("GET /deployments code=%d body=%s", recListDeps.Code, recListDeps.Body.String())
	}
	var listDeps paginatedListEnvelope[modelDeploymentDTO]
	mustDecodeJSON(t, recListDeps.Body.Bytes(), &listDeps)
	if len(listDeps.Data) != 1 || listDeps.Data[0].ConnectionState != "DEGRADED" {
		t.Fatalf("expected deployment connection_state=DEGRADED after 503 probe, got %+v", listDeps.Data)
	}

	mode.Store(1)
	recRecovProbe := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recRecovProbe.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test (recover) code=%d body=%s", recRecovProbe.Code, recRecovProbe.Body.String())
	}
	recListDeps = h.doAuthedCookie(t, http.MethodGet, "/admin/v1/deployments", ownerCookie, "", "", nil)
	mustDecodeJSON(t, recListDeps.Body.Bytes(), &listDeps)
	if len(listDeps.Data) != 1 || listDeps.Data[0].ConnectionState != "ACTIVE" {
		t.Fatalf("expected deployment connection_state=ACTIVE after recovery probe, got %+v", listDeps.Data)
	}

	// 6. Subsequent failing probe with 401 transitions ACTIVE -> AUTH_EXPIRED_REAUTH_REQUIRED,
	// blocks routing candidates (CONNECTION_INACTIVE), and rejects deployment activation.
	mode.Store(2)
	recAuthFail := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recAuthFail.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test (401) code=%d body=%s", recAuthFail.Code, recAuthFail.Body.String())
	}
	recListDeps = h.doAuthedCookie(t, http.MethodGet, "/admin/v1/deployments", ownerCookie, "", "", nil)
	mustDecodeJSON(t, recListDeps.Body.Bytes(), &listDeps)
	if len(listDeps.Data) != 1 || listDeps.Data[0].ConnectionState != "AUTH_EXPIRED_REAUTH_REQUIRED" {
		t.Fatalf("expected AUTH_EXPIRED_REAUTH_REQUIRED after 401 probe, got %+v", listDeps.Data)
	}

	recExplain := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes/explain", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":       boot.EnvID,
		"target_model": "def002-route",
	})
	if recExplain.Code != http.StatusOK {
		t.Fatalf("POST /routes/explain code=%d body=%s", recExplain.Code, recExplain.Body.String())
	}
	var exp resolver.ExplainResult
	mustDecodeJSON(t, recExplain.Body.Bytes(), &exp)
	if len(exp.SelectedCandidatesOrdered) != 0 {
		t.Fatalf("expected 0 selected candidates after auth failure, got %+v", exp.SelectedCandidatesOrdered)
	}

	recReActFail := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/deployments/"+dep.ID, ownerCookie, boot.CSRFToken, FormatETag(dep.Version), map[string]any{
		"state": "ACTIVE_HEALTHY",
	})
	if recReActFail.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 re-activating deployment while connection AUTH_EXPIRED, got %d body=%s", recReActFail.Code, recReActFail.Body.String())
	}
}

func TestDEF007_BootstrapAndTenancyAuditChain(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF007 Audit Org", "def007-proj")

	// 1. Freshly bootstrapped organization must have org-chain provisioning events with intact hash chain.
	ctx := context.Background()
	report, err := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{
		ChainKeys: []string{"SYSTEM", boot.OrgID},
	})
	if err != nil {
		t.Fatalf("VerifyAuditChain(boot org): %v report=%+v", err, report)
	}
	if !report.Valid || report.Chains[boot.OrgID].VerifiedCount < 3 {
		t.Fatalf("expected valid org chain with >= 3 bootstrap provisioning events, got %+v", report.Chains[boot.OrgID])
	}

	var bootEvents []string
	rows, err := h.st.RawDB().QueryContext(ctx, `
		SELECT event_type, actor_kind, actor_id FROM audit_events
		WHERE org_id = ? ORDER BY seq_no ASC`, boot.OrgID)
	if err != nil {
		t.Fatalf("query boot org audit events: %v", err)
	}
	for rows.Next() {
		var et, ak, ai string
		if serr := rows.Scan(&et, &ak, &ai); serr != nil {
			t.Fatal(serr)
		}
		if ak != "PRINCIPAL" || ai != boot.OwnerPrincipalID {
			t.Fatalf("expected actor PRINCIPAL/%s on bootstrap org event %s, got %s/%s", boot.OwnerPrincipalID, et, ak, ai)
		}
		bootEvents = append(bootEvents, et)
	}
	_ = rows.Close()
	if len(bootEvents) < 3 || bootEvents[0] != "organization.created" || bootEvents[1] != "project.created" || bootEvents[2] != "environment.created" {
		t.Fatalf("unexpected bootstrap org event sequence: %v", bootEvents)
	}

	// 2. Subsequent HTTP org, project, and environment mutations must emit PRINCIPAL-attributed org audit events.
	recOrg := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/organizations", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "def007-org-two",
		"name": "DEF007 Org Two",
	})
	if recOrg.Code != http.StatusCreated {
		t.Fatalf("POST /organizations code=%d body=%s", recOrg.Code, recOrg.Body.String())
	}
	var org2 struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recOrg.Body.Bytes(), &org2)

	recProj := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "def007-p2",
		"name": "DEF007 Proj Two",
	})
	if recProj.Code != http.StatusCreated {
		t.Fatalf("POST /projects code=%d body=%s", recProj.Code, recProj.Body.String())
	}
	var proj2 struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recProj.Body.Bytes(), &proj2)

	recEnv := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects/"+proj2.ID+"/environments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "staging",
		"name": "Staging",
	})
	if recEnv.Code != http.StatusCreated {
		t.Fatalf("POST /environments code=%d body=%s", recEnv.Code, recEnv.Body.String())
	}

	report2, err := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{ChainKeys: []string{org2.ID, boot.OrgID}})
	if err != nil {
		t.Fatalf("VerifyAuditChain(org2, boot): %v report=%+v", err, report2)
	}
	if !report2.Valid || report2.Chains[org2.ID].VerifiedCount != 1 || report2.Chains[boot.OrgID].VerifiedCount < 5 {
		t.Fatalf("expected intact 1-event chain for org2 and >=5-event chain for boot org, got org2=%+v boot=%+v", report2.Chains[org2.ID], report2.Chains[boot.OrgID])
	}
	rows2, err := h.st.RawDB().QueryContext(ctx, `
		SELECT event_type, actor_kind, actor_id, request_id FROM audit_events
		WHERE org_id = ? AND seq_no > 3 AND event_type IN ('project.created', 'environment.created') ORDER BY seq_no ASC`, boot.OrgID)
	if err != nil {
		t.Fatalf("query boot org post-bootstrap audit events: %v", err)
	}
	defer rows2.Close()
	postEvents := 0
	for rows2.Next() {
		postEvents++
		var et, ak, ai string
		var rid *string
		if serr := rows2.Scan(&et, &ak, &ai, &rid); serr != nil {
			t.Fatal(serr)
		}
		if ak != "PRINCIPAL" || ai != boot.OwnerPrincipalID || rid == nil || *rid == "" {
			t.Fatalf("expected PRINCIPAL/%s with request_id on %s, got %s/%s rid=%v", boot.OwnerPrincipalID, et, ak, ai, rid)
		}
	}
	if postEvents != 2 {
		t.Fatalf("expected 2 post-bootstrap tenancy events (project.created + environment.created), got %d", postEvents)
	}
	var ak2, ai2 string
	var rid2 *string
	if err := h.st.RawDB().QueryRowContext(ctx, `
		SELECT actor_kind, actor_id, request_id FROM audit_events WHERE org_id = ? AND event_type = 'organization.created'`, org2.ID).Scan(&ak2, &ai2, &rid2); err != nil {
		t.Fatalf("query org2 audit event: %v", err)
	}
	if ak2 != "PRINCIPAL" || ai2 != boot.OwnerPrincipalID || rid2 == nil || *rid2 == "" {
		t.Fatalf("expected PRINCIPAL/%s with request_id on org2 creation, got %s/%s rid=%v", boot.OwnerPrincipalID, ak2, ai2, rid2)
	}
}

func TestDEF008_BackupAvailabilityMatchesCreationPrerequisites(t *testing.T) {
	h := newAdminTestHarness(t)
	h.registerBearerToken("tok_def008_op", &rbac.Actor{
		PrincipalID:     "prc_def008_op",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})

	// 1. With all prerequisite files present, backup_supported matches platform capability.
	status := h.doBearer(t, http.MethodGet, "/admin/v1/ops/cluster-status", "tok_def008_op", "", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("GET cluster status code=%d body=%s", status.Code, status.Body.String())
	}
	var cluster struct {
		BackupSupported bool   `json:"backup_supported"`
		BackupReason    string `json:"backup_unavailable_reason"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &cluster); err != nil {
		t.Fatal(err)
	}
	if cluster.BackupSupported != backup.PlatformSupported() {
		t.Fatalf("expected backup_supported=%t, got %t (reason=%q)", backup.PlatformSupported(), cluster.BackupSupported, cluster.BackupReason)
	}

	// 2. When configPath file is missing on disk, cluster-status must proactively report backup_supported=false
	// and match POST /ops/backups returning 503 ops_backend_unavailable.
	if err := os.Remove(h.srv.configPath); err != nil {
		t.Fatalf("remove configPath: %v", err)
	}
	statusMissing := h.doBearer(t, http.MethodGet, "/admin/v1/ops/cluster-status", "tok_def008_op", "", nil)
	if statusMissing.Code != http.StatusOK {
		t.Fatalf("GET cluster status missing config code=%d body=%s", statusMissing.Code, statusMissing.Body.String())
	}
	if err := json.Unmarshal(statusMissing.Body.Bytes(), &cluster); err != nil {
		t.Fatal(err)
	}
	if cluster.BackupSupported || cluster.BackupReason == "" {
		t.Fatalf("expected backup_supported=false with reason when config file missing, got %+v", cluster)
	}
	backupResp := h.doBearer(t, http.MethodPost, "/admin/v1/ops/backups", "tok_def008_op", "", nil)
	if backupResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 POST /ops/backups when prerequisites missing, got %d body=%s", backupResp.Code, backupResp.Body.String())
	}
}

func TestDEF010_VirtualKeyA2APlaneAndRouteGrants(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "DEF010 VKey Org", "def010-proj")

	// 1. Default Virtual Key omits allowed_planes -> defaults to ["PROVIDER"] and allowed_routes=["*"].
	recDef := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":       "def010-default",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recDef.Code != http.StatusCreated {
		t.Fatalf("create default vkey code=%d body=%s", recDef.Code, recDef.Body.String())
	}
	var defKey virtualKeyCreatedDTO
	mustDecodeJSON(t, recDef.Body.Bytes(), &defKey)
	if len(defKey.AllowedPlanes) != 1 || defKey.AllowedPlanes[0] != "PROVIDER" {
		t.Fatalf("expected default allowed_planes=[PROVIDER], got %+v", defKey.AllowedPlanes)
	}

	// 2. Reject empty or bogus allowed_planes cleanly (400).
	for _, bad := range [][]string{{}, {"BOGUS_PLANE"}} {
		recBad := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
			"name":           "def010-bad",
			"project_id":     boot.ProjectID,
			"env_id":         boot.EnvID,
			"allowed_planes": bad,
		})
		if recBad.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 on bad allowed_planes %v, got %d body=%s", bad, recBad.Code, recBad.Body.String())
		}
	}

	// 3. Provision key explicitly scoped to PROVIDER + A2A and route "invoice-triage".
	recA2A := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"name":           "def010-a2a-grant",
		"project_id":     boot.ProjectID,
		"env_id":         boot.EnvID,
		"allowed_planes": []string{"PROVIDER", "A2A"},
		"allowed_routes": []string{"invoice-triage"},
	})
	if recA2A.Code != http.StatusCreated {
		t.Fatalf("create A2A vkey code=%d body=%s", recA2A.Code, recA2A.Body.String())
	}
	var a2aKey virtualKeyCreatedDTO
	mustDecodeJSON(t, recA2A.Body.Bytes(), &a2aKey)
	if strings.Join(a2aKey.AllowedPlanes, ",") != "PROVIDER,A2A" || strings.Join(a2aKey.AllowedRoutes, ",") != "invoice-triage" {
		t.Fatalf("unexpected A2A key scopes: planes=%v routes=%v", a2aKey.AllowedPlanes, a2aKey.AllowedRoutes)
	}

	// Verify GET /virtual-keys lists allowed_planes and allowed_routes accurately.
	recList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/virtual-keys", cookie, "", "", nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /virtual-keys code=%d body=%s", recList.Code, recList.Body.String())
	}
	var listed paginatedListEnvelope[virtualKeySummaryDTO]
	mustDecodeJSON(t, recList.Body.Bytes(), &listed)
	foundA2A := false
	for _, k := range listed.Data {
		if k.ID == a2aKey.ID {
			foundA2A = true
			if strings.Join(k.AllowedPlanes, ",") != "PROVIDER,A2A" || strings.Join(k.AllowedRoutes, ",") != "invoice-triage" {
				t.Fatalf("listed key lost allowed_planes/routes: %+v", k)
			}
		}
	}
	if !foundA2A {
		t.Fatalf("A2A key not found in GET /virtual-keys")
	}

	// 4. PATCH updating allowed_planes and allowed_routes persists & refreshes snapshot,
	// and ROTATE preserves granted planes/routes.
	recPatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/virtual-keys/"+a2aKey.ID, cookie, boot.CSRFToken, FormatETag(a2aKey.Version), map[string]any{
		"allowed_planes": []string{"PROVIDER", "MCP", "A2A"},
		"allowed_routes": []string{"*"},
	})
	if recPatch.Code != http.StatusOK {
		t.Fatalf("PATCH /virtual-keys code=%d body=%s", recPatch.Code, recPatch.Body.String())
	}
	var patched virtualKeySummaryDTO
	mustDecodeJSON(t, recPatch.Body.Bytes(), &patched)
	if strings.Join(patched.AllowedPlanes, ",") != "PROVIDER,MCP,A2A" || strings.Join(patched.AllowedRoutes, ",") != "*" {
		t.Fatalf("expected updated planes/routes, got %+v", patched)
	}

	recRot := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys/"+patched.ID+"/rotate", cookie, boot.CSRFToken, FormatETag(patched.Version), map[string]any{
		"grace_window_seconds": 300,
	})
	if recRot.Code != http.StatusOK {
		t.Fatalf("POST /rotate code=%d body=%s", recRot.Code, recRot.Body.String())
	}
	var rotated virtualKeyCreatedDTO
	mustDecodeJSON(t, recRot.Body.Bytes(), &rotated)
	if strings.Join(rotated.AllowedPlanes, ",") != "PROVIDER,MCP,A2A" || strings.Join(rotated.AllowedRoutes, ",") != "*" {
		t.Fatalf("expected rotation to preserve planes/routes, got %+v", rotated)
	}
}

func TestDEF011_AdminA2ATaskResumeAndCancel(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, cookie := h.bootstrapOwner(t, "DEF011 Admin Org", "def011-proj")

	agentRec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/agents", cookie, boot.CSRFToken, "", map[string]any{
		"project_id":          boot.ProjectID,
		"agent_slug":          "def011-agent",
		"name":                "DEF011 Agent",
		"description":         "Handles approval resume",
		"endpoint_url":        "https://agent.example.test/a2a",
		"data_region":         "EU",
		"max_delegation_hops": 3,
	})
	if agentRec.Code != http.StatusCreated {
		t.Fatalf("register agent code=%d body=%s", agentRec.Code, agentRec.Body.String())
	}
	var card a2a.AgentCard
	mustDecodeJSON(t, agentRec.Body.Bytes(), &card)

	vkRec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", cookie, boot.CSRFToken, "", map[string]any{
		"project_id":     boot.ProjectID,
		"env_id":         boot.EnvID,
		"name":           "def011-key",
		"allowed_planes": []string{"A2A"},
	})
	if vkRec.Code != http.StatusCreated {
		t.Fatalf("create vkey code=%d body=%s", vkRec.Code, vkRec.Body.String())
	}
	var vk virtualKeyCreatedDTO
	mustDecodeJSON(t, vkRec.Body.Bytes(), &vk)

	coord, err := a2a.NewTaskCoordinator(a2a.CoordinatorConfig{
		Enabled: true,
		DB:      h.st.RawDB(),
		Audit:   h.srv.audit,
		Now:     h.fk.Now,
	})
	if err != nil {
		t.Fatalf("NewTaskCoordinator: %v", err)
	}
	h.srv.a2aCoordinator = coord

	now := h.fk.Now().UnixMilli()
	if _, err := h.st.RawDB().Exec(`
		INSERT INTO a2a_tasks (
			id, org_id, project_id, env_id, agent_endpoint_id, caller_virtual_key_id, trace_root_id,
			hop_count, call_chain_ids_json, sub_budget_cap_micros, reservation_id, enforced_region,
			artifacts_json, state, version, created_at_ms, updated_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, 100000, '', 'EU', ?, 'INPUT_REQUIRED', 1, ?, ?)`,
		"tsk_def011_pause", boot.OrgID, boot.ProjectID, boot.EnvID, card.AgentID, vk.ID, "req_def011",
		`["`+card.AgentID+`"]`, `[{"__tanpopo_prompt_message":"Confirm budget override"}]`, now, now,
	); err != nil {
		t.Fatalf("seed paused task: %v", err)
	}

	// List must expose prompt_message and state=input-required.
	listRec := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/a2a/tasks", cookie, "", "", nil)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "Confirm budget override") {
		t.Fatalf("expected prompt_message in GET /a2a/tasks, code=%d body=%s", listRec.Code, listRec.Body.String())
	}

	// Missing CSRF returns 403.
	noCSRF := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/tasks/tsk_def011_pause/resume", cookie, "", "", map[string]any{
		"input_parts": []map[string]any{{"decision": "approve"}},
	})
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without CSRF, got %d", noCSRF.Code)
	}

	// Unknown task returns 404.
	notFound := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/tasks/tsk_nonexistent/resume", cookie, boot.CSRFToken, "", map[string]any{
		"input_parts": []map[string]any{{"decision": "approve"}},
	})
	if notFound.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown task, got %d", notFound.Code)
	}

	// Resume moves task input-required -> working (when no upstream transport) and clears prompt_message.
	resumedRec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/tasks/tsk_def011_pause/resume", cookie, boot.CSRFToken, "", map[string]any{
		"input_parts": []map[string]any{{"decision": "approve"}},
	})
	if resumedRec.Code != http.StatusOK {
		t.Fatalf("POST /admin/v1/a2a/tasks/{id}/resume code=%d body=%s", resumedRec.Code, resumedRec.Body.String())
	}
	var resumed a2a.TaskView
	mustDecodeJSON(t, resumedRec.Body.Bytes(), &resumed)
	if resumed.State != a2a.StateWorking || resumed.PromptMessage != "" {
		t.Fatalf("expected working task with cleared prompt_message, got %+v", resumed)
	}

	// Repeat resume returns 409 and writes zero audit events.
	dupRec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/tasks/tsk_def011_pause/resume", cookie, boot.CSRFToken, "", map[string]any{
		"input_parts": []map[string]any{{"decision": "again"}},
	})
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 resuming non-input-required task, got %d body=%s", dupRec.Code, dupRec.Body.String())
	}

	// Cancel moves working task to canceled.
	cancelRec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/a2a/tasks/tsk_def011_pause/cancel", cookie, boot.CSRFToken, "", nil)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("POST /admin/v1/a2a/tasks/{id}/cancel code=%d body=%s", cancelRec.Code, cancelRec.Body.String())
	}
	var canceled a2a.TaskView
	mustDecodeJSON(t, cancelRec.Body.Bytes(), &canceled)
	if canceled.State != a2a.StateCanceled {
		t.Fatalf("expected canceled state, got %+v", canceled)
	}

	// Non-privileged role must get uniform 403 on nonexistent and existing task IDs (no 404/403 oracle).
	finopsTok := "tok_def011_finops"
	h.registerBearerToken(finopsTok, &rbac.Actor{
		PrincipalID:     "pri_def011_finops",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: boot.OrgID, RoleName: rbac.RoleFinOpsViewer}},
	})
	for _, target := range []string{"tsk_nonexistent", "tsk_def011_pause"} {
		rRec := h.doBearer(t, http.MethodPost, "/admin/v1/a2a/tasks/"+target+"/resume", finopsTok, "", map[string]any{"input_parts": []map[string]any{{"x": 1}}})
		if rRec.Code != http.StatusForbidden {
			t.Fatalf("expected uniform 403 for FINOPS_VIEWER resume on %s, got %d body=%s", target, rRec.Code, rRec.Body.String())
		}
		cRec := h.doBearer(t, http.MethodPost, "/admin/v1/a2a/tasks/"+target+"/cancel", finopsTok, "", nil)
		if cRec.Code != http.StatusForbidden {
			t.Fatalf("expected uniform 403 for FINOPS_VIEWER cancel on %s, got %d body=%s", target, cRec.Code, cRec.Body.String())
		}
	}

	// Verify exact counts and PRINCIPAL/admin_api attribution for resume and cancel audit events.
	var resumeAudits, cancelAudits int
	if err := h.st.RawDB().QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE org_id = ? AND event_type = 'a2a.task.resumed' AND actor_kind = 'PRINCIPAL' AND entry_point = 'admin_api'`,
		boot.OrgID,
	).Scan(&resumeAudits); err != nil || resumeAudits != 1 {
		t.Fatalf("expected 1 a2a.task.resumed PRINCIPAL/admin_api audit event, count=%d err=%v", resumeAudits, err)
	}
	if err := h.st.RawDB().QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE org_id = ? AND event_type = 'a2a.task.canceled_by_admin' AND actor_kind = 'PRINCIPAL' AND entry_point = 'admin_api'`,
		boot.OrgID,
	).Scan(&cancelAudits); err != nil || cancelAudits != 1 {
		t.Fatalf("expected 1 a2a.task.canceled_by_admin PRINCIPAL/admin_api audit event, count=%d err=%v", cancelAudits, err)
	}
}

