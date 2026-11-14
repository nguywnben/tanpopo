// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

package rbac

import (
	"errors"
	"os"
	"testing"

	"github.com/nguywnben/tanpopo/internal/testkit/netguard"
)

func TestMain(m *testing.M) {
	os.Exit(netguard.MustRunHermeticMain(m))
}

func strPtr(s string) *string { return &s }

func TestSixRoleRBACMatrixAndFailClosedGuards(t *testing.T) {
	t.Parallel()

	orgA := "org_01jabcdef0123456789012345a"
	orgB := "org_01jabcdef0123456789012345b"
	projA1 := "prj_01jabcdef012345678901234a1"
	projA2 := "prj_01jabcdef012345678901234a2"

	mkActor := func(role, scopeType, scopeID string, orgID *string) *Actor {
		return &Actor{
			PrincipalID:     "prc_01jabcdef01234567890123456",
			SessionOrgID:    orgID,
			CredentialPlane: CredentialPlaneAdminSession,
			Bindings: []RoleBinding{
				{
					ID:          "rbd_01",
					OrgID:       orgID,
					PrincipalID: "prc_01jabcdef01234567890123456",
					ScopeType:   scopeType,
					ScopeID:     scopeID,
					RoleName:    role,
				},
			},
		}
	}

	// AUTH-11: Only OPERATOR may call POST /admin/v1/organizations (ActionOrgCreate).
	op := mkActor(RoleOperator, ScopeInstance, "instance", nil)
	if err := Authorize(op, ActionOrgCreate, TargetScope{}); err != nil {
		t.Fatalf("OPERATOR ActionOrgCreate should succeed: %v", err)
	}
	ownerA := mkActor(RoleOrgOwner, ScopeOrg, orgA, &orgA)
	if err := Authorize(ownerA, ActionOrgCreate, TargetScope{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ORG_OWNER ActionOrgCreate expected ErrPermissionDenied, got %v", err)
	}
	if err := Authorize(ownerA, ActionBudgetWrite, TargetScope{OrgID: orgA, ProjectID: projA1}); err != nil {
		t.Fatalf("ORG_OWNER ActionBudgetWrite in OrgA should succeed: %v", err)
	}

	// NEG-2 / AUTH-11: DEVELOPER and FINOPS_VIEWER receive 403 on plugin approvals, secret writes, and policy writes.
	for _, role := range []string{RoleDeveloper, RoleFinOpsViewer} {
		actor := mkActor(role, ScopeOrg, orgA, &orgA)
		for _, deniedAct := range []Action{ActionExtensionApprove, ActionSecretWrite, ActionPolicyOrgWrite, ActionBudgetWrite, ActionVKeyCreate} {
			if err := Authorize(actor, deniedAct, TargetScope{OrgID: orgA, ProjectID: projA1}); !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("role %s action %s expected ErrPermissionDenied, got %v", role, deniedAct, err)
			}
		}
		if err := Authorize(actor, ActionBudgetRead, TargetScope{OrgID: orgA, ProjectID: projA1}); err != nil {
			t.Fatalf("role %s read budget expected nil, got %v", role, err)
		}
	}

	// FINOPS_VIEWER is barred from Playground (SCR-15), whereas DEVELOPER is allowed.
	finops := mkActor(RoleFinOpsViewer, ScopeOrg, orgA, &orgA)
	if err := Authorize(finops, ActionPlaygroundCreate, TargetScope{OrgID: orgA, ProjectID: projA1}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("FINOPS_VIEWER playground expected ErrPermissionDenied, got %v", err)
	}
	dev := mkActor(RoleDeveloper, ScopeOrg, orgA, &orgA)
	if err := Authorize(dev, ActionPlaygroundCreate, TargetScope{OrgID: orgA, ProjectID: projA1}); err != nil {
		t.Fatalf("DEVELOPER playground expected nil, got %v", err)
	}

	// SECURITY_REVIEWER approves plugin capabilities, reads/verifies audit, emergency-revokes keys,
	// but cannot write secrets or install extensions.
	secRev := mkActor(RoleSecurityReviewer, ScopeOrg, orgA, &orgA)
	for _, allowed := range []Action{ActionExtensionApprove, ActionAuditReadVerify, ActionVKeyRevoke, ActionKillSwitchTrigger, ActionApprovalDecide} {
		if err := Authorize(secRev, allowed, TargetScope{OrgID: orgA, ProjectID: projA1}); err != nil {
			t.Fatalf("SECURITY_REVIEWER %s expected nil, got %v", allowed, err)
		}
	}
	for _, denied := range []Action{ActionSecretWrite, ActionExtensionStage, ActionBudgetWrite} {
		if err := Authorize(secRev, denied, TargetScope{OrgID: orgA, ProjectID: projA1}); !errors.Is(err, ErrPermissionDenied) {
			t.Fatalf("SECURITY_REVIEWER %s expected ErrPermissionDenied, got %v", denied, err)
		}
	}

	// TEAM_ADMIN project isolation: can manage projA1, denied on projA2 and Org-owned secrets (ProjectID == "").
	teamAdmin := mkActor(RoleTeamAdmin, ScopeProject, projA1, &orgA)
	if err := Authorize(teamAdmin, ActionSecretWrite, TargetScope{OrgID: orgA, ProjectID: projA1}); err != nil {
		t.Fatalf("TEAM_ADMIN projA1 secret write expected nil, got %v", err)
	}
	if err := Authorize(teamAdmin, ActionSecretWrite, TargetScope{OrgID: orgA, ProjectID: projA2}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("TEAM_ADMIN sibling projA2 secret write expected ErrPermissionDenied, got %v", err)
	}
	if err := Authorize(teamAdmin, ActionSecretWrite, TargetScope{OrgID: orgA}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("TEAM_ADMIN org-level secret write expected ErrPermissionDenied, got %v", err)
	}

	// AUTH-13: Spoofed request-body org_id is ignored in favor of authenticated session org_id.
	resolvedOrg, err := ResolveSessionOrgID(ownerA, orgB)
	if err != nil || resolvedOrg != orgA {
		t.Fatalf("ResolveSessionOrgID expected %q, got %q (err=%v)", orgA, resolvedOrg, err)
	}

	// AUTH-15: Cross-Org probing returns uniform 404 resource_not_found instead of 403.
	if err := Authorize(ownerA, ActionVKeyRead, TargetScope{OrgID: orgB, ProjectID: "prj_other"}); !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("cross-org probe expected ErrResourceNotFound, got %v", err)
	}

	// AUTH-07: Data Plane Virtual Key presented to Control Plane fails immediately with invalid_credential_plane.
	vkActor := &Actor{
		PrincipalID:     "vky_01jabcdef01234567890123456",
		SessionOrgID:    strPtr(orgA),
		CredentialPlane: CredentialPlaneDataVirtual,
		Bindings:        ownerA.Bindings,
	}
	if err := Authorize(vkActor, ActionOrgRead, TargetScope{OrgID: orgA}); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("Virtual Key on Control Plane expected ErrInvalidCredentialPlane, got %v", err)
	}
}

func TestValidateRoutePermissionsFailsClosed(t *testing.T) {
	t.Parallel()

	// Unguarded route omitting Action must fail boot (AUTH-11).
	if err := ValidateRoutePermissions([]RoutePermissionSpec{
		{Method: "GET", Path: "/admin/v1/bootstrap/status", Public: true},
		{Method: "POST", Path: "/admin/v1/virtual-keys", Public: false, Action: ""},
	}); !errors.Is(err, ErrInvalidRouteMetadata) {
		t.Fatalf("expected ErrInvalidRouteMetadata on unguarded route, got %v", err)
	}

	if err := ValidateRoutePermissions([]RoutePermissionSpec{
		{Method: "GET", Path: "/admin/v1/bootstrap/status", Public: true},
		{Method: "POST", Path: "/admin/v1/virtual-keys", Public: false, Action: ActionVKeyCreate},
	}); err != nil {
		t.Fatalf("valid route table failed: %v", err)
	}
	if err := ValidateRoutePermissions(nil); !errors.Is(err, ErrInvalidRouteMetadata) {
		t.Fatalf("empty route table expected ErrInvalidRouteMetadata, got %v", err)
	}
	if err := ValidateRoutePermissions([]RoutePermissionSpec{{Method: "", Path: "/a", Action: ActionOrgRead}}); !errors.Is(err, ErrInvalidRouteMetadata) {
		t.Fatalf("empty method expected ErrInvalidRouteMetadata, got %v", err)
	}
	if err := ValidateRoutePermissions([]RoutePermissionSpec{{Method: "GET", Path: "/a", Action: Action("unknown:op")}}); !errors.Is(err, ErrInvalidRouteMetadata) {
		t.Fatalf("unknown action expected ErrInvalidRouteMetadata, got %v", err)
	}
}

func TestRBACEdgeCasesAndResolvers(t *testing.T) {
	t.Parallel()
	orgA := "org_a"
	projA1 := "prj_a1"

	if !IsValidRole(RoleOperator) || IsValidRole("root") {
		t.Fatalf("IsValidRole failed")
	}
	if err := Authorize(nil, ActionOrgRead, TargetScope{}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("nil actor expected ErrUnauthenticated, got %v", err)
	}
	if _, err := ResolveSessionOrgID(nil, orgA); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("nil actor ResolveSessionOrgID expected ErrUnauthenticated, got %v", err)
	}

	// Fallback session org resolution from bindings + rejection when empty.
	actorFromBinding := &Actor{
		PrincipalID:     "prc_1",
		CredentialPlane: CredentialPlaneAdminOIDC,
		Bindings:        []RoleBinding{{ScopeType: ScopeOrg, OrgID: &orgA, ScopeID: orgA, RoleName: RoleDeveloper}},
	}
	resolved, err := ResolveSessionOrgID(actorFromBinding, "org_spoofed")
	if err != nil || resolved != orgA {
		t.Fatalf("expected %q, got %q err=%v", orgA, resolved, err)
	}
	noOrgActor := &Actor{PrincipalID: "prc_2", CredentialPlane: CredentialPlaneAdminService}
	if _, err := ResolveSessionOrgID(noOrgActor, "org_spoofed"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("empty bindings ResolveSessionOrgID expected ErrPermissionDenied, got %v", err)
	}

	// Unknown action fails closed even for OPERATOR; bad credential plane rejected fail-closed.
	op := &Actor{
		PrincipalID:     "prc_op",
		CredentialPlane: CredentialPlaneAdminService,
		Bindings:        []RoleBinding{{ScopeType: ScopeInstance, ScopeID: "instance", RoleName: RoleOperator}},
	}
	if err := Authorize(op, Action("does:not:exist"), TargetScope{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unknown action expected ErrPermissionDenied, got %v", err)
	}
	badPlane := &Actor{PrincipalID: "prc_x", CredentialPlane: "WEIRD_PLANE"}
	if err := Authorize(badPlane, ActionOrgRead, TargetScope{}); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("bad plane expected ErrInvalidCredentialPlane, got %v", err)
	}

	// Project-scoped binding permits collection read ONLY when Target.IsCollectionList == true,
	// and blocks un-scoped single-resource reads, un-scoped mutations, and Org-wide connection writes.
	projDev := &Actor{
		PrincipalID:     "prc_pdev",
		SessionOrgID:    &orgA,
		CredentialPlane: CredentialPlaneAdminSession,
		Bindings:        []RoleBinding{{ScopeType: ScopeProject, OrgID: &orgA, ScopeID: projA1, RoleName: RoleTeamAdmin}},
	}
	if err := Authorize(projDev, ActionVKeyRead, TargetScope{OrgID: orgA, IsCollectionList: true}); err != nil {
		t.Fatalf("project collection list expected nil, got %v", err)
	}
	if err := Authorize(projDev, ActionVKeyRead, TargetScope{OrgID: orgA, IsCollectionList: false}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("single-resource read missing ProjectID expected ErrPermissionDenied, got %v", err)
	}
	if err := Authorize(projDev, ActionVKeyCreate, TargetScope{OrgID: orgA}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("unscoped project mutation expected ErrPermissionDenied, got %v", err)
	}
	if err := Authorize(projDev, ActionConnectionWrite, TargetScope{OrgID: orgA}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("org-level connection write expected ErrPermissionDenied, got %v", err)
	}

	// Empty CredentialPlane must fail closed with ErrInvalidCredentialPlane (AUTH-07).
	emptyPlane := &Actor{
		PrincipalID:     "prc_empty_plane",
		SessionOrgID:    &orgA,
		CredentialPlane: "",
		Bindings:        []RoleBinding{{ScopeType: ScopeOrg, OrgID: &orgA, ScopeID: orgA, RoleName: RoleOrgOwner}},
	}
	if err := Authorize(emptyPlane, ActionOrgRead, TargetScope{OrgID: orgA}); !errors.Is(err, ErrInvalidCredentialPlane) {
		t.Fatalf("empty CredentialPlane expected ErrInvalidCredentialPlane, got %v", err)
	}

	// Malicious RoleOperator bound to ORG/PROJECT scope cannot escalate to instance OPERATOR privileges.
	spoofedOp := &Actor{
		PrincipalID:     "prc_spoofed_op",
		SessionOrgID:    &orgA,
		CredentialPlane: CredentialPlaneAdminSession,
		Bindings:        []RoleBinding{{ScopeType: ScopeOrg, OrgID: &orgA, ScopeID: orgA, RoleName: RoleOperator}},
	}
	if err := Authorize(spoofedOp, ActionOrgCreate, TargetScope{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("RoleOperator on ScopeOrg expected ErrPermissionDenied, got %v", err)
	}

	// Org-scoped mutation with empty target.OrgID must fail closed.
	ownerEmptyOrg := &Actor{
		PrincipalID:     "prc_owner",
		SessionOrgID:    &orgA,
		CredentialPlane: CredentialPlaneAdminSession,
		Bindings:        []RoleBinding{{ScopeType: ScopeOrg, OrgID: &orgA, ScopeID: orgA, RoleName: RoleOrgOwner}},
	}
	if err := Authorize(ownerEmptyOrg, ActionOrgDelete, TargetScope{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ActionOrgDelete with empty target.OrgID expected ErrPermissionDenied, got %v", err)
	}
	if err := Authorize(ownerEmptyOrg, ActionVKeyCreate, TargetScope{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("ActionVKeyCreate with empty target.OrgID expected ErrPermissionDenied, got %v", err)
	}
}
