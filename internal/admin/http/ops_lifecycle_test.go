// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
	"github.com/nguywnben/tanpopo/internal/ops/backup"
	"github.com/nguywnben/tanpopo/internal/security/killswitch"
	"github.com/nguywnben/tanpopo/internal/storage/port"
)

func TestCR13InstanceOpsHandlersReturnCompletedResults(t *testing.T) {
	h := newAdminTestHarness(t)
	h.registerBearerToken("tok_cr13_operator", &rbac.Actor{
		PrincipalID:     "prc_cr13_operator",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})

	status := h.doBearer(t, http.MethodGet, "/admin/v1/ops/cluster-status", "tok_cr13_operator", "", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("GET cluster status code=%d body=%s", status.Code, status.Body.String())
	}
	var cluster struct {
		BackupSupported bool   `json:"backup_supported"`
		BackupReason    string `json:"backup_unavailable_reason"`
		Nodes           []struct {
			WALBytes int64  `json:"wal_bytes"`
			Health   string `json:"health"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &cluster); err != nil {
		t.Fatalf("decode cluster status: %v", err)
	}
	if len(cluster.Nodes) != 1 || cluster.Nodes[0].WALBytes < 0 || cluster.Nodes[0].Health == "" {
		t.Fatalf("expected measured non-negative WAL bytes, got %s", status.Body.String())
	}
	if cluster.BackupSupported != backup.PlatformSupported() {
		t.Fatalf("backup_supported=%t; platform support=%t", cluster.BackupSupported, backup.PlatformSupported())
	}
	if cluster.BackupSupported == (cluster.BackupReason != "") {
		t.Fatalf("backup availability reason should be present exactly when backup is unavailable: %+v", cluster)
	}
	if err := h.snapshots.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh config snapshot: %v", err)
	}
	status = h.doBearer(t, http.MethodGet, "/admin/v1/ops/cluster-status", "tok_cr13_operator", "", nil)
	if err := json.Unmarshal(status.Body.Bytes(), &cluster); err != nil || len(cluster.Nodes) != 1 || cluster.Nodes[0].Health != "ok" {
		t.Fatalf("fresh snapshot should report healthy node: body=%s err=%v", status.Body.String(), err)
	}
	h.fk.Advance(time.Minute)
	status = h.doBearer(t, http.MethodGet, "/admin/v1/ops/cluster-status", "tok_cr13_operator", "", nil)
	if err := json.Unmarshal(status.Body.Bytes(), &cluster); err != nil || len(cluster.Nodes) != 1 || cluster.Nodes[0].Health != "unhealthy" {
		t.Fatalf("stale snapshot should report unhealthy node: body=%s err=%v", status.Body.String(), err)
	}

	artifactPath := filepath.Join(h.srv.installer.ArtifactRoot(), "sample.wasm")
	if err := os.WriteFile(artifactPath, []byte("pinned test extension"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupResp := h.doBearer(t, http.MethodPost, "/admin/v1/ops/backups", "tok_cr13_operator", "", nil)
	if !backup.PlatformSupported() {
		if backupResp.Code != http.StatusServiceUnavailable {
			t.Fatalf("POST backups code=%d; unsupported platform should fail closed: %s", backupResp.Code, backupResp.Body.String())
		}
		preflight := h.doBearer(t, http.MethodPost, "/admin/v1/ops/upgrades/preflight", "tok_cr13_operator", "", map[string]any{"target_core_version": "v0.2.0"})
		if preflight.Code != http.StatusOK {
			t.Fatalf("POST preflight code=%d body=%s", preflight.Code, preflight.Body.String())
		}
		return
	}
	if backupResp.Code != http.StatusCreated {
		t.Fatalf("POST backups code=%d body=%s", backupResp.Code, backupResp.Body.String())
	}
	var receipt struct {
		BackupID        string `json:"backup_id"`
		ArchiveFilename string `json:"archive_filename"`
		SHA256Hex       string `json:"sha256_hex"`
		SizeBytes       int64  `json:"size_bytes"`
	}
	if err := json.Unmarshal(backupResp.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("decode backup receipt: %v", err)
	}
	if receipt.BackupID == "" || receipt.ArchiveFilename == "" || len(receipt.SHA256Hex) != 64 || receipt.SizeBytes <= 0 {
		t.Fatalf("backup returned incomplete receipt: %s", backupResp.Body.String())
	}
	archivePath := filepath.Join(filepath.Dir(h.st.DBPath()), "backups", receipt.ArchiveFilename)
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("backup receipt points to missing archive %q: %v", archivePath, err)
	}
	var backupAuditCount int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type = 'ops.backup_created' AND target_id = ?`, receipt.BackupID).Scan(&backupAuditCount); err != nil {
		t.Fatalf("query backup audit event: %v", err)
	}
	if backupAuditCount != 1 {
		t.Fatalf("expected one immutable backup audit event, got %d", backupAuditCount)
	}

	preflight := h.doBearer(t, http.MethodPost, "/admin/v1/ops/upgrades/preflight", "tok_cr13_operator", "", map[string]any{
		"target_core_version": "v0.2.0",
	})
	if preflight.Code != http.StatusOK {
		t.Fatalf("POST preflight code=%d body=%s", preflight.Code, preflight.Body.String())
	}
	var verdict struct {
		Compatible bool   `json:"compatible"`
		State      string `json:"state"`
	}
	if err := json.Unmarshal(preflight.Body.Bytes(), &verdict); err != nil {
		t.Fatalf("decode preflight verdict: %v", err)
	}
	if !verdict.Compatible || verdict.State != "READY_TO_UPGRADE" {
		t.Fatalf("expected computed compatible preflight result, got %s", preflight.Body.String())
	}
	var preflightAuditCount int
	if err := h.st.RawDB().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE event_type = 'ops.upgrade_preflight_ran' AND target_id = 'v0.2.0'`).Scan(&preflightAuditCount); err != nil {
		t.Fatalf("query preflight audit event: %v", err)
	}
	if preflightAuditCount != 1 {
		t.Fatalf("expected one immutable preflight audit event, got %d", preflightAuditCount)
	}

	sysEvents := h.doBearer(t, http.MethodGet, "/admin/v1/audit-events?chain_partition=SYSTEM&action=ops.upgrade_preflight_ran", "tok_cr13_operator", "", nil)
	if sysEvents.Code != http.StatusOK {
		t.Fatalf("instance OPERATOR GET /audit-events?chain_partition=SYSTEM code=%d body=%s", sysEvents.Code, sysEvents.Body.String())
	}
	var sysEventList struct {
		Data []struct {
			Action         string `json:"action"`
			ChainPartition string `json:"chain_partition"`
			TargetID       string `json:"target_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(sysEvents.Body.Bytes(), &sysEventList); err != nil || len(sysEventList.Data) != 1 || sysEventList.Data[0].ChainPartition != "SYSTEM" || sysEventList.Data[0].TargetID != "v0.2.0" {
		t.Fatalf("expected SYSTEM audit event for ops.upgrade_preflight_ran, got body=%s err=%v", sysEvents.Body.String(), err)
	}
	if backup.PlatformSupported() {
		backupEvents := h.doBearer(t, http.MethodGet, "/admin/v1/audit-events?chain_partition=SYSTEM&action=ops.backup_created", "tok_cr13_operator", "", nil)
		if backupEvents.Code != http.StatusOK {
			t.Fatalf("instance OPERATOR GET backup history code=%d body=%s", backupEvents.Code, backupEvents.Body.String())
		}
		var backupList struct {
			Data []struct {
				TargetID string `json:"target_id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(backupEvents.Body.Bytes(), &backupList); err != nil || len(backupList.Data) != 1 || backupList.Data[0].TargetID != receipt.BackupID {
			t.Fatalf("expected ops.backup_created in SYSTEM partition matching %s, got body=%s err=%v", receipt.BackupID, backupEvents.Body.String(), err)
		}
	}
}

func TestBlastRadiusAndBudgetCrossTenantHardening(t *testing.T) {
	h := newPart2TestHarness(t, 100)
	bootA, ownerCookieA := h.bootstrapOwner(t, "org-alpha", "proj-alpha")
	ctx := context.Background()
	orgB, err := h.st.Tenancy().CreateOrganization(ctx, port.CreateOrgParams{Slug: "org-beta", DisplayName: "Org Beta"})
	if err != nil {
		t.Fatalf("CreateOrganization orgB: %v", err)
	}
	projB, err := h.st.Tenancy().CreateProject(ctx, orgB.ID, port.CreateProjectParams{Slug: "proj-beta", DisplayName: "Proj Beta"})
	if err != nil {
		t.Fatalf("CreateProject projB: %v", err)
	}
	envB, err := h.st.Tenancy().CreateEnvironment(ctx, orgB.ID, projB.ID, port.CreateEnvironmentParams{Slug: "prod-beta"})
	if err != nil {
		t.Fatalf("CreateEnvironment envB: %v", err)
	}

	// Cross-tenant non-operator querying SYSTEM partition is rejected with 403.
	orgOwnerOnlyTok := "tok_org_a_owner_only"
	h.registerBearerToken(orgOwnerOnlyTok, &rbac.Actor{
		PrincipalID:     "prc_org_a_owner_only",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		SessionOrgID:    &bootA.OrgID,
		Bindings:        []rbac.RoleBinding{{OrgID: &bootA.OrgID, ScopeType: rbac.ScopeOrg, ScopeID: bootA.OrgID, RoleName: rbac.RoleOrgOwner}},
	})
	recSysDenied := h.doBearer(t, http.MethodGet, "/admin/v1/audit-events?chain_partition=SYSTEM&action=ops.backup_created", orgOwnerOnlyTok, "", nil)
	if recSysDenied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-operator SYSTEM audit list, got %d body=%s", recSysDenied.Code, recSysDenied.Body.String())
	}

	// Org-bound operator (SessionOrgID != "") cannot obtain multi-org instance blast radius.
	orgOpTok := "tok_org_bound_op"
	h.registerBearerToken(orgOpTok, &rbac.Actor{
		PrincipalID:     bootA.OwnerPrincipalID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		SessionOrgID:    &bootA.OrgID,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO request_traces_meta (
			request_id, created_at_ms, started_at_ms, org_id, project_id, env_id, virtual_key_id, connection_id,
			extension_installation_id, entry_point, route_slug, http_status, proxy_overhead_us, total_duration_ms, waterfall_stages_json
		) VALUES ('trc_org_b', ?, ?, ?, ?, ?, 'vk_b', 'conn_b', 'ext_shared', 'unified_http', 'gpt-4o', 200, 100, 10, '[]')`,
		h.fk.Now().UnixMilli(), h.fk.Now().UnixMilli()-10, orgB.ID, projB.ID, envB.ID,
	); err != nil {
		t.Fatalf("insert trace in org B: %v", err)
	}
	recOrgOpBR := h.doBearer(t, http.MethodGet, "/admin/v1/blast-radius?target_type=EXTENSION&target_id=ext_shared", orgOpTok, "", nil)
	if recOrgOpBR.Code != http.StatusOK {
		t.Fatalf("org-bound operator blast-radius code=%d body=%s", recOrgOpBR.Code, recOrgOpBR.Body.String())
	}
	var brOrgOp blastRadiusResponseDTO
	mustDecodeJSON(t, recOrgOpBR.Body.Bytes(), &brOrgOp)
	if len(brOrgOp.Organizations) != 0 {
		t.Fatalf("org-bound operator must remain restricted to session org, got orgs=%v", brOrgOp.Organizations)
	}

	// Both Org B's real PROVIDER_CONNECTION and a nonexistent connection ID return uniform 404 to Org A (no existence oracle).
	adpID := seedLocalOpenAIAdapter(t, h, "127.0.0.1")
	nowMs := h.fk.Now().UnixMilli()
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO provider_connections (id, org_id, owner_project_id, shared_project_ids_json, adapter_id, name, endpoint_base_url, allow_private_ip, data_region, billing_mode, state, version, created_at_ms, updated_at_ms)
		VALUES ('conn_foreign_b', ?, ?, '[]', ?, 'Foreign Conn', 'https://api.openai.com/v1', 0, 'US', 'COMMERCIAL_API', 'ACTIVE', 1, ?, ?)`,
		orgB.ID, projB.ID, adpID, nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert foreign provider_connection: %v", err)
	}
	recConnForeign := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=PROVIDER_CONNECTION&target_id=conn_foreign_b", ownerCookieA, "", "", nil)
	recConnMissing := h.doAuthedCookie(t, http.MethodGet, "/admin/v1/blast-radius?target_type=PROVIDER_CONNECTION&target_id=conn_does_not_exist", ownerCookieA, "", "", nil)
	if recConnForeign.Code != http.StatusNotFound || recConnMissing.Code != http.StatusNotFound {
		t.Fatalf("expected uniform 404 for foreign and missing PROVIDER_CONNECTION blast-radius, got foreign=%d missing=%d", recConnForeign.Code, recConnMissing.Code)
	}

	// Instance operator without SessionOrgID can verify SYSTEM audit hash chain; non-operator gets 403.
	instOpTok := "tok_inst_op_verify"
	h.registerBearerToken(instOpTok, &rbac.Actor{
		PrincipalID:     bootA.OwnerPrincipalID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, ScopeID: "instance", RoleName: rbac.RoleOperator}},
	})
	recSysVerifyOp := h.doBearer(t, http.MethodPost, "/admin/v1/audit-events/verify?chain_partition=SYSTEM", instOpTok, "", nil)
	if recSysVerifyOp.Code != http.StatusOK {
		t.Fatalf("expected 200 verifying SYSTEM audit chain for instance operator, got %d body=%s", recSysVerifyOp.Code, recSysVerifyOp.Body.String())
	}
	recSysVerifyDenied := h.doBearer(t, http.MethodPost, "/admin/v1/audit-events/verify?chain_partition=SYSTEM", orgOwnerOnlyTok, "", nil)
	if recSysVerifyDenied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 verifying SYSTEM audit chain for non-operator, got %d body=%s", recSysVerifyDenied.Code, recSysVerifyDenied.Body.String())
	}

	// Org-bound operator session (cookie) can clear an instance-scoped EXTENSION kill switch without ?org_id=.
	var extInstallID string
	if err := h.st.RawDB().QueryRowContext(ctx, `SELECT installation_id FROM provider_adapters WHERE id = ?`, adpID).Scan(&extInstallID); err != nil {
		t.Fatalf("lookup extension installation_id: %v", err)
	}
	_, _, trigErr := h.killSwitch.TriggerWithSeveredCount(ctx, killswitch.TriggerParams{
		OrgID: nil, Level: killswitch.LevelExtensionInstallation, TargetID: extInstallID,
		Reason: "instance security freeze", TriggeredByPrincipalID: bootA.OwnerPrincipalID,
	})
	if trigErr != nil {
		t.Fatalf("trigger instance extension kill switch: %v", trigErr)
	}
	instSwitches, err := h.killSwitch.ListInstance(ctx, true)
	if err != nil || len(instSwitches) != 1 {
		t.Fatalf("expected 1 active instance switch, got len=%d err=%v", len(instSwitches), err)
	}
	recClearInst := h.doAuthedCookie(t, http.MethodDelete, "/admin/v1/kill-switches/"+instSwitches[0].ID, ownerCookieA, bootA.CSRFToken, "", map[string]any{
		"expected_version": instSwitches[0].Version,
		"clear_reason":     "resolved instance-level issue",
	})
	if recClearInst.Code != http.StatusOK {
		t.Fatalf("expected 200 clearing instance kill switch from operator cookie session, got %d body=%s", recClearInst.Code, recClearInst.Body.String())
	}

	// Case-variant Org_ID spoofing in JSON payload still triggers 403 org_id_spoofing_forbidden.
	recCaseSpoof := h.doBearer(t, http.MethodPost, "/admin/v1/kill-switches", orgOwnerOnlyTok, "", map[string]any{
		"Org_ID":       orgB.ID,
		"target_level": "PROJECT",
		"target_id":    bootA.ProjectID,
		"confirm_slug": "proj-alpha",
		"reason":       "case-variant spoof check",
	})
	if recCaseSpoof.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on case-variant Org_ID spoofing, got %d body=%s", recCaseSpoof.Code, recCaseSpoof.Body.String())
	}

	// Low-privilege DEVELOPER calling DELETE /organizations/{orgId} without If-Match receives 403 RBAC before 428 precondition_required.
	devTok := "tok_org_a_developer"
	h.registerBearerToken(devTok, &rbac.Actor{
		PrincipalID:     "prc_org_a_developer",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		SessionOrgID:    &bootA.OrgID,
		Bindings:        []rbac.RoleBinding{{OrgID: &bootA.OrgID, ScopeType: rbac.ScopeProject, ScopeID: bootA.ProjectID, RoleName: rbac.RoleDeveloper}},
	})
	recDelDenied := h.doBearer(t, http.MethodDelete, "/admin/v1/organizations/"+bootA.OrgID, devTok, "", nil)
	if recDelDenied.Code != http.StatusForbidden {
		t.Fatalf("expected 403 RBAC denial before 428 precondition check on DELETE organization, got %d body=%s", recDelDenied.Code, recDelDenied.Body.String())
	}

	// PUT /budgets/{id} validates input fields before checking foreignOrgExists (no 404 vs 400 oracle).
	if _, err := h.st.RawDB().ExecContext(ctx, `
		INSERT INTO budget_ledgers (id, org_id, scope_type, scope_id, period_kind, period_start_ms, period_end_ms, hard_limit_micros, soft_alert_thresholds_pct_json, version, created_at_ms, updated_at_ms)
		VALUES ('bgt_foreign_b', ?, 'ORG', ?, 'MONTHLY', 1, 2, 1000, '[80]', 1, ?, ?)`,
		orgB.ID, orgB.ID, nowMs, nowMs,
	); err != nil {
		t.Fatalf("insert foreign budget: %v", err)
	}
	recForeignInvalid := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/bgt_foreign_b", ownerCookieA, bootA.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":        "PROJECT",
		"scope_id":          bootA.ProjectID,
		"hard_limit_micros": -1,
		"period":            "MONTHLY",
	})
	recMissingInvalid := h.doAuthedCookie(t, http.MethodPut, "/admin/v1/budgets/bgt_nonexistent", ownerCookieA, bootA.CSRFToken, `"v1"`, map[string]any{
		"scope_tier":        "PROJECT",
		"scope_id":          bootA.ProjectID,
		"hard_limit_micros": -1,
		"period":            "MONTHLY",
	})
	if recForeignInvalid.Code != http.StatusBadRequest || recMissingInvalid.Code != http.StatusBadRequest {
		t.Fatalf("expected uniform 400 invalid_request for invalid payload regardless of foreign budget existence (foreign=%d missing=%d)", recForeignInvalid.Code, recMissingInvalid.Code)
	}
}
