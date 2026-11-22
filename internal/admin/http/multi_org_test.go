// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/nguywnben/tanpopo/internal/iam/rbac"
)

func TestMultiOrgListingAndHeaderSwitchingIsolation(t *testing.T) {
	h := newAdminTestHarness(t)
	tok := h.readBootstrapToken(t)
	recBootComplete := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":             tok,
		"owner_email":                 "owner@acme.test",
		"owner_password":              "correct-horse-battery-staple",
		"org_name":                    "Acme Alpha",
		"project_name":                "Alpha Core",
		"master_kek_backup_acknowledged": true,
	})
	if recBootComplete.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d body=%s", recBootComplete.Code, recBootComplete.Body.String())
	}
	var boot bootstrapCompleteResponse
	if err := json.Unmarshal(recBootComplete.Body.Bytes(), &boot); err != nil {
		t.Fatalf("decode bootstrap response: %v", err)
	}

	// Bootstrap operator token creates Org B and Org C.
	opActor := &rbac.Actor{
		PrincipalID:     "prc_op_multi",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, RoleName: rbac.RoleOperator}},
	}
	h.registerBearerToken("tok_op_multi", opActor)

	recB := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_op_multi", "", map[string]any{
		"slug": "org-bravo",
		"name": "Organization Bravo",
	})
	if recB.Code != http.StatusCreated {
		t.Fatalf("create org-bravo failed: %d body=%s", recB.Code, recB.Body.String())
	}
	var orgB organizationDTO
	if err := json.Unmarshal(recB.Body.Bytes(), &orgB); err != nil {
		t.Fatalf("decode orgB: %v", err)
	}

	recC := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_op_multi", "", map[string]any{
		"slug": "org-charlie",
		"name": "Organization Charlie",
	})
	if recC.Code != http.StatusCreated {
		t.Fatalf("create org-charlie failed: %d body=%s", recC.Code, recC.Body.String())
	}
	var orgC organizationDTO
	if err := json.Unmarshal(recC.Body.Bytes(), &orgC); err != nil {
		t.Fatalf("decode orgC: %v", err)
	}

	// Create a distinct project inside Org B via X-Tanpopo-Org-ID header as OPERATOR.
	recProjB := h.doRaw(t, http.MethodPost, "/admin/v1/projects", map[string]any{
		"slug": "proj-bravo",
		"name": "Bravo Project",
	}, map[string]string{
		"Authorization":   "Bearer tok_op_multi",
		"X-Tanpopo-Org-ID": orgB.ID,
	}, nil)
	if recProjB.Code != http.StatusCreated {
		t.Fatalf("OPERATOR create project in Org B via X-Tanpopo-Org-ID failed: %d body=%s", recProjB.Code, recProjB.Body.String())
	}

	// Non-OPERATOR user belonging to Org A (boot.OrgID) and Org B (orgB.ID), NOT Org C.
	dualOrgActor := &rbac.Actor{
		PrincipalID:     "prc_dual_org_owner",
		CredentialPlane: rbac.CredentialPlaneAdminService,
		SessionOrgID:    &boot.OrgID,
		Bindings: []rbac.RoleBinding{
			{ScopeType: rbac.ScopeOrg, OrgID: &boot.OrgID, ScopeID: boot.OrgID, RoleName: rbac.RoleOrgOwner},
			{ScopeType: rbac.ScopeOrg, OrgID: &orgB.ID, ScopeID: orgB.ID, RoleName: rbac.RoleOrgOwner},
		},
	}
	h.registerBearerToken("tok_dual_org", dualOrgActor)

	// GET /organizations lists exactly Org A and Org B, never Org C.
	recOrgs := h.doBearer(t, http.MethodGet, "/admin/v1/organizations", "tok_dual_org", "", nil)
	if recOrgs.Code != http.StatusOK {
		t.Fatalf("dual-org GET /organizations failed: %d body=%s", recOrgs.Code, recOrgs.Body.String())
	}
	var orgList struct {
		Data []organizationDTO `json:"data"`
	}
	if err := json.Unmarshal(recOrgs.Body.Bytes(), &orgList); err != nil {
		t.Fatalf("decode orgList: %v", err)
	}
	if len(orgList.Data) != 2 {
		t.Fatalf("expected 2 visible orgs for dual-org member, got %d: %+v", len(orgList.Data), orgList.Data)
	}
	seenIDs := map[string]bool{}
	for _, o := range orgList.Data {
		seenIDs[o.ID] = true
	}
	if !seenIDs[boot.OrgID] || !seenIDs[orgB.ID] || seenIDs[orgC.ID] {
		t.Fatalf("unexpected visible organization set: %+v", seenIDs)
	}

	// Switching X-Tanpopo-Org-ID to Org B lists Bravo Project.
	recListB := h.doRaw(t, http.MethodGet, "/admin/v1/projects", nil, map[string]string{
		"Authorization":   "Bearer tok_dual_org",
		"X-Tanpopo-Org-ID": orgB.ID,
	}, nil)
	if recListB.Code != http.StatusOK || !bytes.Contains(recListB.Body.Bytes(), []byte("proj-bravo")) {
		t.Fatalf("expected Bravo Project when X-Tanpopo-Org-ID=%s, got %d body=%s", orgB.ID, recListB.Code, recListB.Body.String())
	}

	// AUTH-15: Dual-org principal probing unowned Org C returns 404 resource_not_found.
	recProbeC := h.doRaw(t, http.MethodGet, "/admin/v1/projects", nil, map[string]string{
		"Authorization":   "Bearer tok_dual_org",
		"X-Tanpopo-Org-ID": orgC.ID,
	}, nil)
	if recProbeC.Code != http.StatusNotFound {
		t.Fatalf("expected 404 anti-oracle probing unowned Org C, got %d body=%s", recProbeC.Code, recProbeC.Body.String())
	}

	// Rollback lock: MultiOrgDisabled blocks switching away from session org and advertises multi_org_enabled:false.
	h.srv.multiOrgDisabled = true
	recBoot := h.doPublic(t, http.MethodGet, "/admin/v1/bootstrap/status", nil)
	if !bytes.Contains(recBoot.Body.Bytes(), []byte(`"multi_org_enabled":false`)) {
		t.Fatalf("expected multi_org_enabled:false on bootstrap status when disabled, got %s", recBoot.Body.String())
	}

	recSwitchBlocked := h.doRaw(t, http.MethodGet, "/admin/v1/projects", nil, map[string]string{
		"Authorization":   "Bearer tok_dual_org",
		"X-Tanpopo-Org-ID": orgB.ID,
	}, nil)
	if recSwitchBlocked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 multi_org_disabled when MultiOrgDisabled=true, got %d body=%s", recSwitchBlocked.Code, recSwitchBlocked.Body.String())
	}

	recCreateBlocked := h.doBearer(t, http.MethodPost, "/admin/v1/organizations", "tok_op_multi", "", map[string]any{
		"slug": "org-delta",
		"name": "Organization Delta",
	})
	if recCreateBlocked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 multi_org_disabled blocking POST /organizations, got %d body=%s", recCreateBlocked.Code, recCreateBlocked.Body.String())
	}
}

func TestOperatorExtensionKillSwitchGlobalEvenWithOrgHeader(t *testing.T) {
	h := newAdminTestHarness(t)
	tok := h.readBootstrapToken(t)
	recBoot := h.doPublic(t, http.MethodPost, "/admin/v1/bootstrap/complete", map[string]any{
		"bootstrap_token":                tok,
		"owner_email":                    "owner@acme.test",
		"owner_password":                 "correct-horse-battery-staple",
		"org_name":                       "Acme Alpha",
		"project_name":                   "Alpha Core",
		"master_kek_backup_acknowledged": true,
	})
	if recBoot.Code != http.StatusCreated {
		t.Fatalf("bootstrap failed: %d body=%s", recBoot.Code, recBoot.Body.String())
	}
	var boot bootstrapCompleteResponse
	if err := json.Unmarshal(recBoot.Body.Bytes(), &boot); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}

	opActor := &rbac.Actor{
		PrincipalID:     boot.OwnerPrincipalID,
		CredentialPlane: rbac.CredentialPlaneAdminService,
		Bindings:        []rbac.RoleBinding{{ScopeType: rbac.ScopeInstance, RoleName: rbac.RoleOperator}},
	}
	h.registerBearerToken("tok_op_ks", opActor)

	if _, err := h.st.RawDB().Exec(
		`INSERT INTO extension_packages (id, canonical_name, extension_kind, publisher_id, origin_trust_tier, created_at_ms)
		 VALUES ('pkg_global_test', '@acme/ext', 'PROVIDER_ADAPTER', 'acme', 'verified', 1000)`,
	); err != nil {
		t.Fatalf("seed extension package: %v", err)
	}
	if _, err := h.st.RawDB().Exec(
		`INSERT INTO extension_versions (id, package_id, semver, contract_version, core_version_range, execution_tier, bundle_sha256, signature_verified, enterprise_signed, source_uri, manifest_json, created_at_ms)
		 VALUES ('exv_global_test', 'pkg_global_test', '1.0.0', '0.2', '>=0.1.0', 'DECLARATIVE_T1', '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef', 1, 0, 'builtin://local', '{}', 1000)`,
	); err != nil {
		t.Fatalf("seed extension version: %v", err)
	}
	if _, err := h.st.RawDB().Exec(
		`INSERT INTO extension_installations (id, package_id, active_version_id, state, approved_permissions_json, version, created_at_ms, updated_at_ms)
		 VALUES ('ins_global_test', 'pkg_global_test', 'exv_global_test', 'ENABLED_ACTIVE', '{}', 1, 1000, 1000)`,
	); err != nil {
		t.Fatalf("seed extension installation: %v", err)
	}

	recTrig := h.doRaw(t, http.MethodPost, "/admin/v1/kill-switches", map[string]any{
		"target_level":         "EXTENSION",
		"target_id":            "ins_global_test",
		"abort_active_streams": true,
		"reason":               "malicious compromise across cluster",
	}, map[string]string{
		"Authorization":    "Bearer tok_op_ks",
		"X-Tanpopo-Org-ID": boot.OrgID,
	}, nil)
	if recTrig.Code != http.StatusOK {
		t.Fatalf("trigger global extension kill-switch with X-Tanpopo-Org-ID failed: %d body=%s", recTrig.Code, recTrig.Body.String())
	}

	recList := h.doRaw(t, http.MethodGet, "/admin/v1/kill-switches", nil, map[string]string{
		"Authorization":    "Bearer tok_op_ks",
		"X-Tanpopo-Org-ID": boot.OrgID,
	}, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list kill-switches failed: %d body=%s", recList.Code, recList.Body.String())
	}
	var listResp struct {
		Data []struct {
			ID       string  `json:"id"`
			OrgID    *string `json:"org_id"`
			TargetID string  `json:"target_id"`
			Version  int64   `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode killswitch list: %v", err)
	}
	if len(listResp.Data) == 0 {
		t.Fatalf("expected active global kill-switch in list, got empty: %s", recList.Body.String())
	}
	var matchedID string
	var matchedVersion int64
	for _, ks := range listResp.Data {
		if ks.TargetID == "ins_global_test" {
			if ks.ID == "" {
				t.Fatalf("expected non-empty kill-switch id")
			}
			if ks.OrgID != nil {
				t.Fatalf("expected global EXTENSION kill-switch OrgID == nil even when X-Tanpopo-Org-ID=%s was sent, got %q", boot.OrgID, *ks.OrgID)
			}
			matchedID = ks.ID
			matchedVersion = ks.Version
		}
	}
	if matchedID == "" {
		t.Fatalf("kill-switch for ins_global_test not found in %+v", listResp.Data)
	}

	recClear := h.doRaw(t, http.MethodDelete, "/admin/v1/kill-switches/"+matchedID, map[string]any{
		"expected_version": matchedVersion,
		"clear_reason":     "verified safe rollback",
	}, map[string]string{
		"Authorization":    "Bearer tok_op_ks",
		"X-Tanpopo-Org-ID": boot.OrgID,
	}, nil)
	if recClear.Code != http.StatusOK {
		t.Fatalf("clear global kill-switch with X-Tanpopo-Org-ID failed: %d body=%s", recClear.Code, recClear.Body.String())
	}
}
