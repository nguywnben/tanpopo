// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
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

func TestDEF007_PopulatedTenantCryptoShredErasureProof(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF007 Survivor Org", "def007-surv")

	recOrg := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/organizations", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "def007-shred-target",
		"name": "DEF007 Shred Target",
	})
	if recOrg.Code != http.StatusCreated {
		t.Fatalf("POST /organizations code=%d body=%s", recOrg.Code, recOrg.Body.String())
	}
	var targetOrg struct {
		ID      string `json:"id"`
		Slug    string `json:"slug"`
		Version int64  `json:"version"`
	}
	mustDecodeJSON(t, recOrg.Body.Bytes(), &targetOrg)

	// Switch org context on a test session or create project/secret inside targetOrg via storage/http.
	// Using a dedicated ORG_OWNER bearer token bound to targetOrg exercises HTTP provisioning + delete atomically.
	targetTok := "tok_def007_target_owner"
	h.registerBearerToken(targetTok, &rbac.Actor{
		PrincipalID:     boot.OwnerPrincipalID,
		SessionOrgID:    &targetOrg.ID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &targetOrg.ID, ScopeType: rbac.ScopeOrg, ScopeID: targetOrg.ID, RoleName: rbac.RoleOrgOwner}},
	})
	recProj := h.doBearer(t, http.MethodPost, "/admin/v1/projects", targetTok, "", map[string]any{
		"slug": "shred-proj",
		"name": "Shred Proj",
	})
	if recProj.Code != http.StatusCreated {
		t.Fatalf("POST /projects code=%d body=%s", recProj.Code, recProj.Body.String())
	}
	var proj struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recProj.Body.Bytes(), &proj)
	recEnv := h.doBearer(t, http.MethodPost, "/admin/v1/projects/"+proj.ID+"/environments", targetTok, "", map[string]any{
		"slug": "prod",
		"name": "Prod",
	})
	if recEnv.Code != http.StatusCreated {
		t.Fatalf("POST /environments code=%d body=%s", recEnv.Code, recEnv.Body.String())
	}
	var env struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recEnv.Body.Bytes(), &env)
	recVK := h.doBearer(t, http.MethodPost, "/admin/v1/virtual-keys", targetTok, "", map[string]any{
		"name":       "shred-vk",
		"project_id": proj.ID,
		"env_id":     env.ID,
	})
	if recVK.Code != http.StatusCreated {
		t.Fatalf("POST /virtual-keys code=%d body=%s", recVK.Code, recVK.Body.String())
	}
	recSec := h.doBearer(t, http.MethodPost, "/admin/v1/secrets", targetTok, "", map[string]any{
		"name":         "shred-secret",
		"secret_kind":  "API_KEY",
		"secret_value": "sk-def007-secret-proof-value",
	})
	if recSec.Code != http.StatusCreated {
		t.Fatalf("POST /secrets code=%d body=%s", recSec.Code, recSec.Body.String())
	}
	var sec struct {
		ID string `json:"credential_id"`
	}
	mustDecodeJSON(t, recSec.Body.Bytes(), &sec)

	ctx := context.Background()
	var sealedBlob []byte
	var encNonce []byte
	if err := h.st.RawDB().QueryRowContext(ctx, `
		SELECT ciphertext, nonce FROM secret_credentials WHERE org_id = ? AND id = ?`,
		targetOrg.ID, sec.ID,
	).Scan(&sealedBlob, &encNonce); err != nil {
		t.Fatalf("capture pre-shred ciphertext: %v", err)
	}
	if len(sealedBlob) == 0 || len(encNonce) == 0 || strings.Contains(string(sealedBlob), "sk-def007-secret-proof-value") {
		t.Fatal("secret ciphertext missing or contained plaintext before shred")
	}

	// Execute real HTTP org delete/crypto-shred.
	recDel := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+targetOrg.ID, targetTok, FormatETag(targetOrg.Version), map[string]any{
		"confirm_slug": targetOrg.Slug,
		"reason":       "tenant erasure verification test",
	})
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE /organizations code=%d body=%s", recDel.Code, recDel.Body.String())
	}

	// Prove org, project, DEKs, secret_credentials, and active keys are gone while sibling org survives.
	for _, table := range []string{"organizations", "projects", "org_encryption_keys", "project_encryption_keys", "secret_credentials", "virtual_keys"} {
		col := "org_id"
		if table == "organizations" {
			col = "id"
		}
		var cnt int
		if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE `+col+` = ?`, targetOrg.ID).Scan(&cnt); err != nil {
			t.Fatalf("count %s for shredded org: %v", table, err)
		}
		if cnt != 0 {
			t.Fatalf("expected 0 remaining rows in %s for shredded org, got %d", table, cnt)
		}
	}
	if _, err := h.st.Tenancy().GetOrganization(ctx, boot.OrgID); err != nil {
		t.Fatalf("sibling survivor org must remain readable after shred: %v", err)
	}
	if _, err := h.vault.GetSecretMetadata(ctx, targetOrg.ID, sec.ID); err == nil {
		t.Fatal("expected vault secret metadata lookup to fail after crypto-shred")
	}
	if err := h.vault.VerifyAllDEKEnvelopes(ctx); err != nil {
		t.Fatalf("VerifyAllDEKEnvelopes after shred: %v", err)
	}
	if _, err := h.vault.VerifyAllSecretEnvelopes(ctx); err != nil {
		t.Fatalf("VerifyAllSecretEnvelopes after shred: %v", err)
	}

	// Verify both survivor org chain and deleted org chain (ending in organization.crypto_shredded) pass audit verification.
	rep, err := audit.VerifyAuditChain(ctx, h.st.RawDB(), audit.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyAuditChain after crypto-shred: %v report=%+v", err, rep)
	}
	if !rep.Valid || !rep.Chains[targetOrg.ID].Valid || rep.Chains[targetOrg.ID].VerifiedCount < 5 {
		t.Fatalf("expected valid retained chain for shredded org, got %+v", rep.Chains[targetOrg.ID])
	}
}

func TestDEF037_VirtualKeyCreatedAuditVocabularyAndFilter(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF037 Vocab Org", "def037-proj")

	recVK := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/virtual-keys", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":       "def037-key",
		"project_id": boot.ProjectID,
		"env_id":     boot.EnvID,
	})
	if recVK.Code != http.StatusCreated {
		t.Fatalf("POST /virtual-keys code=%d body=%s", recVK.Code, recVK.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Secret string `json:"secret_key"`
	}
	mustDecodeJSON(t, recVK.Body.Bytes(), &created)

	recAudit := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/audit-events?action=virtual_key.created&limit=20", ownerCookie, boot.CSRFToken, "", nil)
	if recAudit.Code != http.StatusOK {
		t.Fatalf("GET /audit-events?action=virtual_key.created code=%d body=%s", recAudit.Code, recAudit.Body.String())
	}
	var listResp struct {
		Data []struct {
			Action     string         `json:"action"`
			TargetID   string         `json:"target_id"`
			TargetType string         `json:"target_type"`
			ActorID    string         `json:"actor_id"`
			Metadata   map[string]any `json:"metadata"`
		} `json:"data"`
	}
	mustDecodeJSON(t, recAudit.Body.Bytes(), &listResp)
	if len(listResp.Data) != 1 || listResp.Data[0].Action != "virtual_key.created" || listResp.Data[0].TargetID != created.ID {
		t.Fatalf("expected 1 canonical virtual_key.created event for %s, got %+v", created.ID, listResp.Data)
	}
	if created.Secret != "" && strings.Contains(recAudit.Body.String(), created.Secret) {
		t.Fatal("audit event response leaked plaintext virtual key secret")
	}
	// Verify documented deprecated alias returns zero records once docs/contract canonicalized on virtual_key.created.
	recOld := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/audit-events?action=virtual_key.issued&limit=20", ownerCookie, boot.CSRFToken, "", nil)
	if recOld.Code != http.StatusOK {
		t.Fatalf("GET /audit-events?action=virtual_key.issued code=%d body=%s", recOld.Code, recOld.Body.String())
	}
	var oldResp struct {
		Data []any `json:"data"`
	}
	mustDecodeJSON(t, recOld.Body.Bytes(), &oldResp)
	if len(oldResp.Data) != 0 {
		t.Fatalf("expected 0 records for deprecated alias virtual_key.issued, got %+v", oldResp.Data)
	}
}

func TestDEF021_And_DEF022_MemberEnrollmentAndImmediateRoleRevocation(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF021 Enrollment Org", "def021-proj")

	// 1. Add a second project to verify TEAM_ADMIN scope isolation.
	recProj2 := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "other-proj",
		"name": "Other Proj",
	})
	if recProj2.Code != http.StatusCreated {
		t.Fatalf("POST /projects other-proj code=%d body=%s", recProj2.Code, recProj2.Body.String())
	}
	var proj2 struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recProj2.Body.Bytes(), &proj2)

	// 2. ORG_OWNER creates HUMAN member -> returns single-use enrollment_token and enrollment_pending=true.
	recCreate := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email":       "enroll.dev@tanpopo.test",
		"kind":        "HUMAN",
		"roles":       []string{"DEVELOPER"},
		"project_ids": []string{boot.ProjectID},
	})
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("POST /members code=%d body=%s", recCreate.Code, recCreate.Body.String())
	}
	var created struct {
		ID                string   `json:"id"`
		Email             string   `json:"email"`
		EnrollmentToken   string   `json:"enrollment_token"`
		HasPassword       bool     `json:"has_password"`
		EnrollmentPending bool     `json:"enrollment_pending"`
		Version           int64    `json:"version"`
		ProjectIDs        []string `json:"project_ids"`
	}
	mustDecodeJSON(t, recCreate.Body.Bytes(), &created)
	if created.EnrollmentToken == "" || !strings.HasPrefix(created.EnrollmentToken, "tpen_") {
		t.Fatalf("expected tpen_ enrollment token, got %q", created.EnrollmentToken)
	}
	if created.HasPassword || !created.EnrollmentPending {
		t.Fatalf("expected has_password=false, enrollment_pending=true; got %+v", created)
	}

	// 3. Weak password (< 12 chars) rejected with 400.
	recWeak := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
		"token":    created.EnrollmentToken,
		"password": "Short1!",
	})
	if recWeak.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on weak enrollment password, got %d body=%s", recWeak.Code, recWeak.Body.String())
	}

	// 4. Valid password activates account, returns session cookie + CSRF, and logs member.enrolled.
	recEnroll := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
		"token":    created.EnrollmentToken,
		"password": "TanpopoEnrolled!2026",
	})
	if recEnroll.Code != http.StatusOK {
		t.Fatalf("POST /auth/enroll code=%d body=%s", recEnroll.Code, recEnroll.Body.String())
	}
	var enrolledSession struct {
		PrincipalID string   `json:"principal_id"`
		CSRFToken   string   `json:"csrf_token"`
		Roles       []string `json:"roles"`
	}
	mustDecodeJSON(t, recEnroll.Body.Bytes(), &enrolledSession)
	if enrolledSession.PrincipalID != created.ID || enrolledSession.CSRFToken == "" {
		t.Fatalf("unexpected enroll session payload: %+v", enrolledSession)
	}

	// 5. Replaying single-use enrollment token fails with 401.
	recReplay := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
		"token":    created.EnrollmentToken,
		"password": "AnotherStrongPass!2026",
	})
	if recReplay.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 replaying enrollment token, got %d body=%s", recReplay.Code, recReplay.Body.String())
	}

	// 6. Standard /auth/login works with newly enrolled password.
	recLogin := h.doPublic(t, http.MethodPost, "/admin/v1/auth/login", map[string]any{
		"email":    "enroll.dev@tanpopo.test",
		"password": "TanpopoEnrolled!2026",
	})
	if recLogin.Code != http.StatusOK {
		t.Fatalf("POST /auth/login for enrolled member code=%d body=%s", recLogin.Code, recLogin.Body.String())
	}
	var devSess struct {
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeJSON(t, recLogin.Body.Bytes(), &devSess)
	devCookie := findCookie(t, recLogin.Result().Cookies(), ownerCookie.Name)

	// Authenticated call works before role change.
	recMeBefore := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/budgets?project_id="+boot.ProjectID, devCookie, "", "", nil)
	if recMeBefore.Code != http.StatusOK {
		t.Fatalf("expected 200 before role revocation, got %d body=%s", recMeBefore.Code, recMeBefore.Body.String())
	}

	// 7. DEF-022: Updating member roles immediately revokes active sessions for that principal.
	// Enrollment bumped principals.version from v1 to v2.
	recRolePatch := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/members/"+created.ID+"/roles", ownerCookie, boot.CSRFToken, FormatETag(created.Version+1), map[string]any{
		"roles":       []string{"TEAM_ADMIN"},
		"project_ids": []string{boot.ProjectID},
	})
	if recRolePatch.Code != http.StatusOK {
		t.Fatalf("PATCH /members/{id}/roles code=%d body=%s", recRolePatch.Code, recRolePatch.Body.String())
	}
	var updatedMember struct {
		HasPassword       bool `json:"has_password"`
		EnrollmentPending bool `json:"enrollment_pending"`
		Version           int64 `json:"version"`
	}
	mustDecodeJSON(t, recRolePatch.Body.Bytes(), &updatedMember)
	if !updatedMember.HasPassword || updatedMember.EnrollmentPending || updatedMember.Version != 3 {
		t.Fatalf("expected has_password=true, enrollment_pending=false, version=3 after role update; got %+v", updatedMember)
	}
	recAfterRevoke := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/budgets?project_id="+boot.ProjectID, devCookie, "", "", nil)
	if recAfterRevoke.Code != http.StatusUnauthorized {
		t.Fatalf("DEF-022: expected 401 immediately after role change revokes active session, got %d body=%s", recAfterRevoke.Code, recAfterRevoke.Body.String())
	}

	// 8. Re-issue enrollment token as owner works; TEAM_ADMIN out-of-scope member reissue returns 403.
	recReissue := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members/"+created.ID+"/enrollment-token", ownerCookie, boot.CSRFToken, "", nil)
	if recReissue.Code != http.StatusCreated {
		t.Fatalf("POST /members/{id}/enrollment-token code=%d body=%s", recReissue.Code, recReissue.Body.String())
	}
	otherTok := "tok_def021_other_ta"
	h.registerBearerToken(otherTok, &rbac.Actor{
		PrincipalID:     "pri_other_ta",
		SessionOrgID:    &boot.OrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &boot.OrgID, ScopeType: rbac.ScopeProject, ScopeID: proj2.ID, RoleName: rbac.RoleTeamAdmin}},
	})
	recCrossScope := h.doBearer(t, http.MethodPost, "/admin/v1/members/"+created.ID+"/enrollment-token", otherTok, "", nil)
	if recCrossScope.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-project TEAM_ADMIN enrollment reissue, got %d body=%s", recCrossScope.Code, recCrossScope.Body.String())
	}

	// 9. Enrollment brute-force rate limit returns 429 Too Many Requests.
	var saw429 bool
	for i := 0; i < 20; i++ {
		rlRec := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
			"token":    "enr_wrong_token_attempt",
			"password": "TanpopoStrongPass!2026",
		})
		if rlRec.Code == http.StatusTooManyRequests {
			saw429 = true
			break
		}
	}
	if !saw429 {
		t.Fatal("expected 429 auth_rate_limited after repeated invalid enrollment token attempts")
	}

	// 10. Audit events contain member.created, member.enrolled, and member.enrollment_token_issued (zero plaintext tokens).
	recAud := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/audit-events?limit=50", ownerCookie, boot.CSRFToken, "", nil)
	if recAud.Code != http.StatusOK {
		t.Fatalf("GET /audit-events code=%d", recAud.Code)
	}
	audBody := recAud.Body.String()
	for _, evType := range []string{"member.created", "member.enrolled", "member.enrollment_issued"} {
		if !strings.Contains(audBody, evType) {
			t.Fatalf("expected audit event %q, got %s", evType, audBody)
		}
	}
	if strings.Contains(audBody, created.EnrollmentToken) {
		t.Fatal("audit event body leaked plaintext enrollment token")
	}
}

func TestDEF034_WebhookUpdateDisableDeleteLifecycle(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF034 Webhook Org", "def034-proj")

	// 1. Seal signing secret and create webhook.
	recSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "whk-hmac",
		"project_id":   boot.ProjectID,
		"secret_kind":  "WEBHOOK_HMAC_SECRET",
		"secret_value": "whsec_def034_hmac_signing_key",
	})
	if recSec.Code != http.StatusCreated {
		t.Fatalf("POST /secrets code=%d body=%s", recSec.Code, recSec.Body.String())
	}
	var sec secretMetadataDTO
	mustDecodeJSON(t, recSec.Body.Bytes(), &sec)

	recWH := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/webhooks", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id":            boot.ProjectID,
		"target_url":            "https://hooks.example.test/receiver",
		"events":                []string{"budget.threshold", "kill_switch.triggered"},
		"signing_credential_id": sec.CredentialID,
	})
	if recWH.Code != http.StatusCreated {
		t.Fatalf("POST /webhooks code=%d body=%s", recWH.Code, recWH.Body.String())
	}
	var wh webhookResource
	mustDecodeJSON(t, recWH.Body.Bytes(), &wh)
	if !wh.Enabled || wh.Version != 1 {
		t.Fatalf("expected enabled v1 webhook, got %+v", wh)
	}

	// Secret referenced by webhook cannot be deleted (409 conflict).
	recDelSecBlocked := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/secrets/"+sec.CredentialID, ownerCookie, boot.CSRFToken, FormatETag(sec.Version), nil)
	if recDelSecBlocked.Code != http.StatusConflict {
		t.Fatalf("expected 409 deleting secret referenced by webhook, got %d body=%s", recDelSecBlocked.Code, recDelSecBlocked.Body.String())
	}

	// Seed a PENDING delivery row to verify disable cancels queued deliveries.
	ctx := context.Background()
	now := h.fk.Now().UTC().UnixMilli()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO webhook_deliveries (id, org_id, subscription_id, event_id, event_type, payload_json, status, attempt_count, next_attempt_at_ms, created_at_ms)
		VALUES ('whd_def034_1', ?, ?, 'evt_def034_1', 'budget.threshold', '{}', 'PENDING', 0, ?, ?)`,
		boot.OrgID, wh.ID, now, now,
	); err != nil {
		t.Fatalf("seed webhook delivery: %v", err)
	}

	// Stale If-Match returns 409.
	recStale := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/webhooks/"+wh.ID, ownerCookie, boot.CSRFToken, FormatETag(99), map[string]any{
		"enabled": false,
	})
	if recStale.Code != http.StatusConflict {
		t.Fatalf("expected 409 on stale If-Match PATCH webhook, got %d body=%s", recStale.Code, recStale.Body.String())
	}

	// 2. PATCH disable sets enabled=false and cancels PENDING deliveries.
	recDisable := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/webhooks/"+wh.ID, ownerCookie, boot.CSRFToken, FormatETag(wh.Version), map[string]any{
		"enabled": false,
	})
	if recDisable.Code != http.StatusOK {
		t.Fatalf("PATCH disable webhook code=%d body=%s", recDisable.Code, recDisable.Body.String())
	}
	mustDecodeJSON(t, recDisable.Body.Bytes(), &wh)
	if wh.Enabled || wh.Version != 2 {
		t.Fatalf("expected disabled v2 webhook, got %+v", wh)
	}
	var remainingPending int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(1) FROM webhook_deliveries WHERE id = 'whd_def034_1'`).Scan(&remainingPending); err != nil {
		t.Fatalf("query webhook delivery count: %v", err)
	}
	if remainingPending != 0 {
		t.Fatalf("expected queued webhook delivery deleted after disable, got count=%d", remainingPending)
	}

	// Re-enable works cleanly.
	recEnable := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/webhooks/"+wh.ID, ownerCookie, boot.CSRFToken, FormatETag(wh.Version), map[string]any{
		"enabled": true,
	})
	if recEnable.Code != http.StatusOK {
		t.Fatalf("PATCH enable webhook code=%d body=%s", recEnable.Code, recEnable.Body.String())
	}
	mustDecodeJSON(t, recEnable.Body.Bytes(), &wh)
	if !wh.Enabled || wh.Version != 3 {
		t.Fatalf("expected enabled v3 webhook, got %+v", wh)
	}

	// Cross-org tenant cannot PATCH or DELETE webhook (403/404).
	otherOrgTok := "tok_def034_other"
	otherOrgID := "org_def034_other"
	h.registerBearerToken(otherOrgTok, &rbac.Actor{
		PrincipalID:     "pri_other",
		SessionOrgID:    &otherOrgID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{OrgID: &otherOrgID, ScopeType: rbac.ScopeOrg, ScopeID: otherOrgID, RoleName: rbac.RoleOperator}},
	})
	if recX := h.doBearer(t, http.MethodDelete, "/admin/v1/webhooks/"+wh.ID, otherOrgTok, FormatETag(wh.Version), nil); recX.Code != http.StatusNotFound && recX.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant DELETE webhook denied (403/404), got %d", recX.Code)
	}

	// 3. DELETE webhook removes subscription, deliveries, internal target_url secret, and unblocks signing secret deletion.
	recDel := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/webhooks/"+wh.ID, ownerCookie, boot.CSRFToken, FormatETag(wh.Version), nil)
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE /webhooks/{id} code=%d body=%s", recDel.Code, recDel.Body.String())
	}
	var whCount int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(1) FROM webhook_subscriptions WHERE id = ?`, wh.ID).Scan(&whCount); err != nil || whCount != 0 {
		t.Fatalf("expected webhook deleted from DB, count=%d err=%v", whCount, err)
	}
	recDelSecNow := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/secrets/"+sec.CredentialID, ownerCookie, boot.CSRFToken, FormatETag(sec.Version), nil)
	if recDelSecNow.Code != http.StatusNoContent {
		t.Fatalf("expected signing secret deletion to succeed after webhook deleted, got %d body=%s", recDelSecNow.Code, recDelSecNow.Body.String())
	}
}

func TestDEF018_DEF024_BudgetTotalLifetimeAndAncestorScoping(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF018 Budget Org", "def018-proj-a")

	// Second project to verify sibling isolation on include_ancestors=true.
	recProjB := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "def018-proj-b",
		"name": "Proj B",
	})
	if recProjB.Code != http.StatusCreated {
		t.Fatalf("POST /projects B code=%d body=%s", recProjB.Code, recProjB.Body.String())
	}
	var projB struct {
		ID string `json:"id"`
	}
	mustDecodeJSON(t, recProjB.Body.Bytes(), &projB)

	// Org ceiling (MONTHLY)
	if recOrgB := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/bgt_org_ceil", ownerCookie, boot.CSRFToken, FormatETag(1), map[string]any{
		"scope_tier":            "ORGANIZATION",
		"scope_id":              boot.OrgID,
		"period":                "MONTHLY",
		"hard_limit_micros":     500000000,
		"soft_warning_percents": []int{80},
	}); recOrgB.Code != http.StatusCreated && recOrgB.Code != http.StatusOK {
		t.Fatalf("PUT org budget code=%d body=%s", recOrgB.Code, recOrgB.Body.String())
	}

	// DEF-018: TOTAL_LIFETIME budget persisted and updated without turning into MONTHLY.
	recLife := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/bgt_proj_a_life", ownerCookie, boot.CSRFToken, FormatETag(1), map[string]any{
		"scope_tier":            "PROJECT",
		"scope_id":              boot.ProjectID,
		"period":                "TOTAL_LIFETIME",
		"hard_limit_micros":     100000000,
		"soft_warning_percents": []int{50, 90},
	})
	if recLife.Code != http.StatusCreated && recLife.Code != http.StatusOK {
		t.Fatalf("PUT TOTAL_LIFETIME budget code=%d body=%s", recLife.Code, recLife.Body.String())
	}
	var bLife budgetResourceDTO
	mustDecodeJSON(t, recLife.Body.Bytes(), &bLife)
	if bLife.Period != "TOTAL_LIFETIME" {
		t.Fatalf("DEF-018: expected period TOTAL_LIFETIME, got %q", bLife.Period)
	}
	recLifeUpd := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/bgt_proj_a_life", ownerCookie, boot.CSRFToken, FormatETag(bLife.Version), map[string]any{
		"scope_tier":            "PROJECT",
		"scope_id":              boot.ProjectID,
		"period":                "TOTAL_LIFETIME",
		"hard_limit_micros":     200000000,
		"soft_warning_percents": []int{50, 90},
	})
	if recLifeUpd.Code != http.StatusOK {
		t.Fatalf("PUT update TOTAL_LIFETIME budget code=%d body=%s", recLifeUpd.Code, recLifeUpd.Body.String())
	}
	mustDecodeJSON(t, recLifeUpd.Body.Bytes(), &bLife)
	if bLife.Period != "TOTAL_LIFETIME" || bLife.HardLimitMicros != 200000000 {
		t.Fatalf("expected updated TOTAL_LIFETIME 200000000, got %+v", bLife)
	}

	// Sibling project B budget
	if recB := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/bgt_proj_b", ownerCookie, boot.CSRFToken, FormatETag(1), map[string]any{
		"scope_tier":            "PROJECT",
		"scope_id":              projB.ID,
		"period":                "DAILY",
		"hard_limit_micros":     50000000,
		"soft_warning_percents": []int{80},
	}); recB.Code != http.StatusCreated && recB.Code != http.StatusOK {
		t.Fatalf("PUT proj B budget code=%d body=%s", recB.Code, recB.Body.String())
	}

	// DEF-024: include_ancestors=true returns org ceiling + proj A budget, NEVER sibling proj B.
	recList := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/budgets?project_id="+boot.ProjectID+"&include_ancestors=true", ownerCookie, boot.CSRFToken, "", nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /budgets include_ancestors code=%d body=%s", recList.Code, recList.Body.String())
	}
	var listBody struct {
		Data []budgetResourceDTO `json:"data"`
	}
	mustDecodeJSON(t, recList.Body.Bytes(), &listBody)
	if len(listBody.Data) != 2 {
		t.Fatalf("expected 2 budgets (org ceiling + proj A), got %d: %+v", len(listBody.Data), listBody.Data)
	}
	for _, b := range listBody.Data {
		if b.ScopeID == projB.ID {
			t.Fatalf("DEF-024 leaked sibling project budget %+v", b)
		}
	}
}

func TestDEF028_ExplainRouteWithoutDialectMatchesConfiguredRoute(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF028 Explain Org", "def028-proj")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o-mini"}]}`))
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)
	adpID := seedLocalOpenAIAdapter(t, h, upURL.Hostname())
	ctx := context.Background()
	var insID string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT installation_id FROM provider_adapters WHERE id = ?`, adpID).Scan(&insID); err != nil {
		t.Fatalf("lookup installation_id for %s: %v", adpID, err)
	}
	recPol := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/organizations/"+boot.OrgID+"/extension-policies", ownerCookie, boot.CSRFToken, `"v1"`, map[string]any{
		"min_plugin_trust": "ANY_APPROVED",
		"policies": []map[string]any{
			{
				"package_id":             "tanpopo.local_openai",
				"enabled":                true,
				"pinned_installation_id": insID,
			},
		},
	})
	if recPol.Code != http.StatusOK {
		t.Fatalf("PUT /extension-policies code=%d body=%s", recPol.Code, recPol.Body.String())
	}

	recSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "def028-key",
		"secret_kind":  "API_KEY",
		"secret_value": "sk-def028-test",
	})
	var sec secretMetadataDTO
	mustDecodeJSON(t, recSec.Body.Bytes(), &sec)

	recConn := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":             "def028-conn",
		"adapter_id":       adpID,
		"billing_mode":     "SELF_HOSTED_INFRA",
		"endpoint_url":     upstream.URL,
		"data_region":      "US",
		"allow_private_ip": true,
		"allowed_cidrs":    []string{"127.0.0.1/32"},
		"secret_bindings":  map[string]string{"api_key": sec.CredentialID},
	})
	if recConn.Code != http.StatusCreated {
		t.Fatalf("POST /connections code=%d body=%s", recConn.Code, recConn.Body.String())
	}
	var conn providerConnectionDTO
	mustDecodeJSON(t, recConn.Body.Bytes(), &conn)

	// Probe connection so SM-02 promotes CREATED_UNVERIFIED -> ACTIVE before activating deployment.
	recProbe := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/connections/"+conn.ID+"/test", ownerCookie, boot.CSRFToken, "", nil)
	if recProbe.Code != http.StatusOK {
		t.Fatalf("POST /connections/{id}/test code=%d body=%s", recProbe.Code, recProbe.Body.String())
	}
	var passProbe connectionTestResponseDTO
	mustDecodeJSON(t, recProbe.Body.Bytes(), &passProbe)
	if !passProbe.Valid {
		t.Fatalf("expected valid=true on test probe, got %+v", passProbe)
	}

	recMod := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/models", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":              "def028-chat",
		"display_name":      "DEF028 Chat",
		"context_window":    32768,
		"max_output_tokens": 4096,
		"assurance_tier":    "VERIFIED",
	})
	var mod logicalModelDTO
	mustDecodeJSON(t, recMod.Body.Bytes(), &mod)

	recDep := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/deployments", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":              boot.EnvID,
		"logical_model_id":    mod.ID,
		"connection_id":       conn.ID,
		"upstream_model_name": "gpt-4o-mini",
		"data_region":         "US",
		"rate_card":           map[string]any{"input_token_nano_usd": 100, "output_token_nano_usd": 200},
	})
	if recDep.Code != http.StatusCreated {
		t.Fatalf("POST /deployments code=%d body=%s", recDep.Code, recDep.Body.String())
	}
	var dep modelDeploymentDTO
	mustDecodeJSON(t, recDep.Body.Bytes(), &dep)

	recRoute := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug":     "def028-route",
		"env_id":   boot.EnvID,
		"strategy": "PRIORITY_FAILOVER",
		"targets":  []map[string]any{{"deployment_id": dep.ID, "priority_order": 1}},
	})
	if recRoute.Code != http.StatusCreated {
		t.Fatalf("POST /routes code=%d body=%s", recRoute.Code, recRoute.Body.String())
	}

	// Omitting dialect must select candidate cleanly without forcing a dialect mismatch.
	recExplain := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/routes/explain", ownerCookie, boot.CSRFToken, "", map[string]any{
		"env_id":        boot.EnvID,
		"target_model":  "def028-route",
		"data_region":   "US",
		"dialect":       "",
		"estimated_input_tokens": 128,
		"max_output_tokens":      256,
	})
	if recExplain.Code != http.StatusOK {
		t.Fatalf("POST /routes/explain empty dialect code=%d body=%s", recExplain.Code, recExplain.Body.String())
	}
	var explained resolver.ExplainResult
	mustDecodeJSON(t, recExplain.Body.Bytes(), &explained)
	if !explained.DryRun || len(explained.SelectedCandidatesOrdered) != 1 || explained.SelectedCandidatesOrdered[0].DeploymentID != dep.ID {
		t.Fatalf("DEF-028: expected dry_run with selected candidate %s, got %+v", dep.ID, explained)
	}
}

func TestDEF007_ProvisionTenantFaultInjectionAtomicRollback(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF007 Rollback Org", "def007-rb")

	// Inject a failing SQLite trigger on config_changelog during project provisioning so atomic tx must roll back project + environment + audit_events.
	ctx := context.Background()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		CREATE TRIGGER fail_changelog_on_proj_rb
		BEFORE INSERT ON config_changelog
		WHEN NEW.entity_kind = 'project'
		BEGIN
			SELECT RAISE(ABORT, 'injected fault during project provisioning');
		END;
	`); err != nil {
		t.Fatalf("create fault injection trigger: %v", err)
	}

	recProj := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/projects", ownerCookie, boot.CSRFToken, "", map[string]any{
		"slug": "doomed-proj",
		"name": "Doomed Project",
	})
	if recProj.Code == http.StatusCreated || recProj.Code == http.StatusOK {
		t.Fatalf("expected fault injection to fail POST /projects, got %d body=%s", recProj.Code, recProj.Body.String())
	}

	var leakedProjects, leakedAudits int
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE org_id = ? AND slug = 'doomed-proj'`, boot.OrgID).Scan(&leakedProjects); err != nil {
		t.Fatal(err)
	}
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT COUNT(1) FROM audit_events WHERE org_id = ? AND event_type = 'project.created' AND metadata_json LIKE '%doomed-proj%'`, boot.OrgID).Scan(&leakedAudits); err != nil {
		t.Fatal(err)
	}
	if leakedProjects != 0 || leakedAudits != 0 {
		t.Fatalf("DEF-007 fault injection leaked partial commit: projects=%d audits=%d", leakedProjects, leakedAudits)
	}
}

func TestDEF017_TraceIDAdminLookupCorrelation(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "DEF017 Trace Org", "def017-proj")

	ctx := context.Background()
	now := h.fk.Now().UTC().UnixMilli()
	const reqID = "req_01def017tracecorrelation01"
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id,
			entry_point, route_slug, http_status, proxy_overhead_us, ttft_ms,
			total_duration_ms, waterfall_stages_json
		) VALUES (?, ?, ?, ?, ?, ?, 'unified_http', 'def017-route', 200, 95, 14, 48, '[{"stage":"auth","duration_ms":1.2,"outcome":"ok"}]')`,
		reqID, now, now-48, boot.OrgID, boot.ProjectID, boot.EnvID,
	); err != nil {
		t.Fatalf("insert request_traces_meta: %v", err)
	}

	rec := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/traces/"+reqID, ownerCookie, "", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("DEF-017: GET /admin/v1/traces/%s code=%d body=%s", reqID, rec.Code, rec.Body.String())
	}
	var trace traceSummaryDTO
	mustDecodeJSON(t, rec.Body.Bytes(), &trace)
	if trace.RequestID != reqID || trace.RouteSlug != "def017-route" || trace.StatusCode != http.StatusOK {
		t.Fatalf("DEF-017: unexpected trace summary payload: %+v", trace)
	}
}

func TestTeamAdmin_ProjectWebhookUpdateDeleteAndOrgDenial(t *testing.T) {
	h := newPart2TestHarness(t, 20)
	boot, ownerCookie := h.bootstrapOwner(t, "Webhook RBAC Org", "wh-rbac-proj")

	// Seal org-scoped and project-scoped HMAC secrets + create matching webhooks.
	recOrgSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "org-wh-hmac",
		"secret_kind":  "WEBHOOK_SECRET",
		"secret_value": "whsec_org_scope_secret_key",
	})
	if recOrgSec.Code != http.StatusCreated {
		t.Fatalf("POST org /secrets code=%d body=%s", recOrgSec.Code, recOrgSec.Body.String())
	}
	var orgSec secretMetadataDTO
	mustDecodeJSON(t, recOrgSec.Body.Bytes(), &orgSec)

	recOrgWH := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/webhooks", ownerCookie, boot.CSRFToken, "", map[string]any{
		"target_url":            "https://org-hooks.example.test/events",
		"events":                []string{"budget.threshold"},
		"signing_credential_id": orgSec.CredentialID,
	})
	if recOrgWH.Code != http.StatusCreated {
		t.Fatalf("POST org /webhooks code=%d body=%s", recOrgWH.Code, recOrgWH.Body.String())
	}
	var orgWH webhookResource
	mustDecodeJSON(t, recOrgWH.Body.Bytes(), &orgWH)

	recProjSec := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/secrets", ownerCookie, boot.CSRFToken, "", map[string]any{
		"name":         "proj-wh-hmac",
		"project_id":   boot.ProjectID,
		"secret_kind":  "WEBHOOK_SECRET",
		"secret_value": "whsec_proj_scope_secret_key",
	})
	if recProjSec.Code != http.StatusCreated {
		t.Fatalf("POST proj /secrets code=%d body=%s", recProjSec.Code, recProjSec.Body.String())
	}
	var projSec secretMetadataDTO
	mustDecodeJSON(t, recProjSec.Body.Bytes(), &projSec)

	recProjWH := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/webhooks", ownerCookie, boot.CSRFToken, "", map[string]any{
		"project_id":            boot.ProjectID,
		"target_url":            "https://proj-hooks.example.test/events",
		"events":                []string{"budget.threshold"},
		"signing_credential_id": projSec.CredentialID,
	})
	if recProjWH.Code != http.StatusCreated {
		t.Fatalf("POST proj /webhooks code=%d body=%s", recProjWH.Code, recProjWH.Body.String())
	}
	var projWH webhookResource
	mustDecodeJSON(t, recProjWH.Body.Bytes(), &projWH)

	// Create and enroll a project-scoped TEAM_ADMIN.
	recMember := h.doAuthedCookie(t, http.MethodPost, "/admin/v1/members", ownerCookie, boot.CSRFToken, "", map[string]any{
		"email":       "team.webhook@tanpopo.test",
		"roles":       []string{"TEAM_ADMIN"},
		"project_ids": []string{boot.ProjectID},
	})
	if recMember.Code != http.StatusCreated {
		t.Fatalf("POST /members code=%d body=%s", recMember.Code, recMember.Body.String())
	}
	var created struct {
		EnrollmentToken string `json:"enrollment_token"`
	}
	mustDecodeJSON(t, recMember.Body.Bytes(), &created)
	recEnroll := h.doPublic(t, http.MethodPost, "/admin/v1/auth/enroll", map[string]any{
		"token":    created.EnrollmentToken,
		"password": "TeamAdminWebhook!2026",
	})
	if recEnroll.Code != http.StatusOK {
		t.Fatalf("POST /auth/enroll code=%d body=%s", recEnroll.Code, recEnroll.Body.String())
	}
	var teamSess struct {
		CSRFToken string `json:"csrf_token"`
	}
	mustDecodeJSON(t, recEnroll.Body.Bytes(), &teamSess)
	teamCookie := findCookie(t, recEnroll.Result().Cookies(), ownerCookie.Name)

	// TEAM_ADMIN must be able to PATCH and DELETE its project-scoped webhook.
	recPatchProj := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/webhooks/"+projWH.ID, teamCookie, teamSess.CSRFToken, FormatETag(projWH.Version), map[string]any{
		"enabled": false,
	})
	if recPatchProj.Code != http.StatusOK {
		t.Fatalf("TEAM_ADMIN PATCH project webhook code=%d body=%s", recPatchProj.Code, recPatchProj.Body.String())
	}
	var updatedProj webhookResource
	mustDecodeJSON(t, recPatchProj.Body.Bytes(), &updatedProj)

	// TEAM_ADMIN must receive 403 when mutating the org-scoped webhook.
	recPatchOrg := h.doAuthedCookie(t, http.MethodPatch, "/admin/v1/webhooks/"+orgWH.ID, teamCookie, teamSess.CSRFToken, FormatETag(orgWH.Version), map[string]any{
		"enabled": false,
	})
	if recPatchOrg.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for TEAM_ADMIN PATCH org webhook, got %d body=%s", recPatchOrg.Code, recPatchOrg.Body.String())
	}
	recDelOrg := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/webhooks/"+orgWH.ID, teamCookie, teamSess.CSRFToken, FormatETag(orgWH.Version), nil)
	if recDelOrg.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for TEAM_ADMIN DELETE org webhook, got %d body=%s", recDelOrg.Code, recDelOrg.Body.String())
	}

	recDelProj := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/webhooks/"+projWH.ID, teamCookie, teamSess.CSRFToken, FormatETag(updatedProj.Version), nil)
	if recDelProj.Code != http.StatusOK {
		t.Fatalf("TEAM_ADMIN DELETE project webhook code=%d body=%s", recDelProj.Code, recDelProj.Body.String())
	}
}

func TestTrustedProxyExtractClientIP_AndHSTS(t *testing.T) {
	srv := &Server{
		trustedProxyCIDRs: func() []netip.Prefix {
			p, _ := parseTrustedProxyCIDRs([]string{"10.0.0.0/8", "127.0.0.1/32"})
			return p
		}(),
	}

	// Trusted proxy chain: rightmost non-trusted hop wins (ignores left-hand client spoof).
	reqTrusted, _ := http.NewRequest(http.MethodPost, "/admin/v1/auth/login", nil)
	reqTrusted.RemoteAddr = "10.1.2.3:54321"
	reqTrusted.Header.Add("X-Forwarded-For", "198.51.100.99, 203.0.113.42, 10.9.8.7")
	reqTrusted.Header.Set("X-Forwarded-Proto", "https")
	if got := srv.extractClientIP(reqTrusted); got != "203.0.113.42" {
		t.Fatalf("expected rightmost untrusted hop 203.0.113.42, got %q", got)
	}
	if !srv.oidcRequestIsHTTPS(reqTrusted) {
		t.Fatal("expected oidcRequestIsHTTPS=true behind trusted proxy with X-Forwarded-Proto=https")
	}

	// Untrusted peer cannot spoof X-Forwarded-For or X-Forwarded-Proto.
	reqUntrusted, _ := http.NewRequest(http.MethodPost, "/admin/v1/auth/login", nil)
	reqUntrusted.RemoteAddr = "192.0.2.55:12345"
	reqUntrusted.Header.Set("X-Forwarded-For", "198.51.100.1")
	reqUntrusted.Header.Set("X-Forwarded-Proto", "https")
	if got := srv.extractClientIP(reqUntrusted); got != "192.0.2.55" {
		t.Fatalf("expected untrusted peer IP 192.0.2.55 ignoring XFF spoof, got %q", got)
	}
	if srv.oidcRequestIsHTTPS(reqUntrusted) {
		t.Fatal("expected oidcRequestIsHTTPS=false for untrusted proxy spoofing X-Forwarded-Proto")
	}
}



