// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package rbac enforces Tanpopo's 6-role canonical RBAC matrix, default-deny
// route authorization guards, credential-plane isolation, request-body org_id
// anti-spoofing, and uniform 404 cross-tenant anti-oracle protection (CR-04 / TP-IMP-0009).
package rbac

import (
	"errors"
	"fmt"
	"strings"
)

// Canonical 6 RBAC roles locked in CR-04 / execution-baseline.md.
const (
	RoleOperator         = "OPERATOR"
	RoleOrgOwner         = "ORG_OWNER"
	RoleTeamAdmin        = "TEAM_ADMIN"
	RoleSecurityReviewer = "SECURITY_REVIEWER"
	RoleFinOpsViewer     = "FINOPS_VIEWER"
	RoleDeveloper        = "DEVELOPER"
)

// Canonical scope_type values matching role_bindings CHECK constraint.
const (
	ScopeInstance = "INSTANCE"
	ScopeOrg      = "ORG"
	ScopeProject  = "PROJECT"
)

// Canonical credential planes separating Control Plane sessions/tokens from Data Plane keys (AUTH-07).
const (
	CredentialPlaneAdminSession = "ADMIN_SESSION"
	CredentialPlaneAdminService = "ADMIN_SERVICE_TOKEN"
	CredentialPlaneAdminOIDC    = "ADMIN_OIDC"
	CredentialPlaneDataVirtual  = "DATA_VIRTUAL_KEY"
)

// Canonical authorization errors.
var (
	ErrUnauthenticated         = errors.New("rbac: unauthenticated principal")
	ErrInvalidCredentialPlane  = errors.New("rbac: invalid_credential_plane")
	ErrPermissionDenied        = errors.New("rbac: insufficient_role_permission")
	ErrResourceNotFound        = errors.New("rbac: resource_not_found")
	ErrInvalidRouteMetadata    = errors.New("rbac: unguarded_admin_route")
	ErrInvalidInput            = errors.New("rbac: invalid authorization input")
)

// Action identifies a strongly typed Control Plane operation.
type Action string

// Canonical Control Plane actions mapped to /admin/v1/* endpoints (SCR-01..16).
const (
	ActionOrgCreate            Action = "org:create"
	ActionOrgRead              Action = "org:read"
	ActionOrgDelete            Action = "org:delete"
	ActionProjectRead          Action = "project:read"
	ActionProjectWrite         Action = "project:write"
	ActionEnvironmentWrite     Action = "environment:write"
	ActionMemberRead           Action = "member:read"
	ActionMemberWrite          Action = "member:write"
	ActionVKeyRead             Action = "virtual_key:read"
	ActionVKeyCreate           Action = "virtual_key:create"
	ActionVKeyRotate           Action = "virtual_key:rotate"
	ActionVKeyRevoke           Action = "virtual_key:revoke"
	ActionSecretReadMeta       Action = "secret:read_metadata"
	ActionSecretWrite          Action = "secret:write"
	ActionConnectionRead       Action = "connection:read"
	ActionConnectionWrite      Action = "connection:write"
	ActionModelRead            Action = "model:read"
	ActionModelWrite           Action = "model:write"
	ActionExtensionRead        Action = "extension:read"
	ActionExtensionStage       Action = "extension:stage_install"
	ActionExtensionApprove     Action = "extension:approve_capability"
	ActionExtensionOrgPolicy   Action = "extension:write_org_policy"
	ActionRouteRead            Action = "route:read"
	ActionRouteWrite           Action = "route:write"
	ActionRouteExplain         Action = "route:explain"
	ActionPolicyRead           Action = "policy:read"
	ActionPolicyInstanceWrite  Action = "policy:write_instance"
	ActionPolicyOrgWrite       Action = "policy:write_org"
	ActionPolicyProjectWrite   Action = "policy:write_project"
	ActionMCPRead              Action = "mcp:read"
	ActionMCPWrite             Action = "mcp:write"
	ActionMCPSchemaApprove     Action = "mcp:approve_schema"
	ActionTraceReadMeta        Action = "trace:read_metadata"
	ActionTraceReadDebugBlob   Action = "trace:read_debug_blob"
	ActionBudgetRead           Action = "budget:read"
	ActionBudgetWrite          Action = "budget:write"
	ActionApprovalRead         Action = "approval:read"
	ActionApprovalDecide       Action = "approval:decide"
	ActionAuditReadVerify      Action = "audit:read_and_verify"
	ActionKillSwitchTrigger    Action = "kill_switch:trigger"
	ActionPlaygroundCreate     Action = "playground:create_session"
	ActionOpsRead              Action = "ops:read"
	ActionOpsWrite             Action = "ops:admin_execute"
	ActionSupportBundleExport  Action = "ops:export_support_bundle"
	ActionRetentionWrite       Action = "ops:write_retention"
)

var allRoles = map[string]bool{
	RoleOperator:         true,
	RoleOrgOwner:         true,
	RoleTeamAdmin:        true,
	RoleSecurityReviewer: true,
	RoleFinOpsViewer:     true,
	RoleDeveloper:        true,
}

// IsValidRole checks whether role is one of the 6 canonical RBAC roles.
func IsValidRole(role string) bool {
	return allRoles[role]
}

// RoleBinding models a persisted RBAC binding row.
type RoleBinding struct {
	ID          string
	OrgID       *string
	PrincipalID string
	ScopeType   string
	ScopeID     string
	RoleName    string
}

// Actor describes an authenticated Control Plane principal and its bindings.
type Actor struct {
	PrincipalID     string
	SessionOrgID    *string
	CredentialPlane string
	Bindings        []RoleBinding
}

// TargetScope specifies the resource scope an authorization check evaluates against.
// OrgID MUST come from authenticated session or verified resource lookup, never raw request body.
type TargetScope struct {
	OrgID            string
	ProjectID        string
	IsCollectionList bool
}

// RoutePermissionSpec declares explicit RBAC requirements for an /admin/v1/* route so
// Control Plane server startup fails closed if any route omits permission metadata (AUTH-11).
type RoutePermissionSpec struct {
	Method string
	Path   string
	Public bool
	Action Action
}

var rolePermissions = map[string]map[Action]bool{
	RoleOperator: {
		ActionOrgCreate:           true,
		ActionOrgRead:             true,
		ActionOrgDelete:           true,
		ActionProjectRead:         true,
		ActionProjectWrite:        true,
		ActionEnvironmentWrite:    true,
		ActionMemberRead:          true,
		ActionMemberWrite:         true,
		ActionVKeyRead:            true,
		ActionVKeyCreate:          true,
		ActionVKeyRotate:          true,
		ActionVKeyRevoke:          true,
		ActionSecretReadMeta:      true,
		ActionSecretWrite:         true,
		ActionConnectionRead:      true,
		ActionConnectionWrite:     true,
		ActionModelRead:           true,
		ActionModelWrite:          true,
		ActionExtensionRead:       true,
		ActionExtensionStage:      true,
		ActionExtensionApprove:    true,
		ActionExtensionOrgPolicy:  true,
		ActionRouteRead:           true,
		ActionRouteWrite:          true,
		ActionRouteExplain:        true,
		ActionPolicyRead:          true,
		ActionPolicyInstanceWrite: true,
		ActionPolicyOrgWrite:      true,
		ActionPolicyProjectWrite:  true,
		ActionMCPRead:             true,
		ActionMCPWrite:            true,
		ActionMCPSchemaApprove:    true,
		ActionTraceReadMeta:       true,
		ActionTraceReadDebugBlob:  true,
		ActionBudgetRead:          true,
		ActionBudgetWrite:         true,
		ActionApprovalRead:        true,
		ActionApprovalDecide:      true,
		ActionAuditReadVerify:     true,
		ActionKillSwitchTrigger:   true,
		ActionPlaygroundCreate:    true,
		ActionOpsRead:             true,
		ActionOpsWrite:            true,
		ActionSupportBundleExport: true,
		ActionRetentionWrite:      true,
	},
	RoleOrgOwner: {
		// Note: ActionOrgCreate (POST /admin/v1/organizations) and ActionSupportBundleExport
		// are strictly reserved for OPERATOR (AUTH-11 / SCR-15 / SCR-16).
		ActionOrgRead:            true,
		ActionOrgDelete:          true,
		ActionProjectRead:        true,
		ActionProjectWrite:       true,
		ActionEnvironmentWrite:   true,
		ActionMemberRead:         true,
		ActionMemberWrite:        true,
		ActionVKeyRead:           true,
		ActionVKeyCreate:         true,
		ActionVKeyRotate:         true,
		ActionVKeyRevoke:         true,
		ActionSecretReadMeta:     true,
		ActionSecretWrite:        true,
		ActionConnectionRead:     true,
		ActionConnectionWrite:    true,
		ActionModelRead:          true,
		ActionModelWrite:         true,
		ActionExtensionRead:      true,
		ActionExtensionOrgPolicy: true,
		ActionRouteRead:          true,
		ActionRouteWrite:         true,
		ActionRouteExplain:       true,
		ActionPolicyRead:         true,
		ActionPolicyOrgWrite:     true,
		ActionPolicyProjectWrite: true,
		ActionMCPRead:            true,
		ActionMCPWrite:           true,
		ActionMCPSchemaApprove:   true,
		ActionTraceReadMeta:      true,
		ActionTraceReadDebugBlob: true,
		ActionBudgetRead:         true,
		ActionBudgetWrite:        true,
		ActionApprovalRead:       true,
		ActionApprovalDecide:     true,
		ActionAuditReadVerify:    true,
		ActionKillSwitchTrigger:  true,
		ActionPlaygroundCreate:   true,
		ActionOpsRead:            true,
		ActionRetentionWrite:     true,
	},
	RoleTeamAdmin: {
		ActionProjectRead:        true,
		ActionMemberRead:         true,
		ActionMemberWrite:        true,
		ActionVKeyRead:           true,
		ActionVKeyCreate:         true,
		ActionVKeyRotate:         true,
		ActionVKeyRevoke:         true,
		ActionSecretReadMeta:     true,
		ActionSecretWrite:        true, // Restricted to project-owned connections/secrets in service layer.
		ActionConnectionRead:     true,
		ActionConnectionWrite:    true,
		ActionModelRead:          true,
		ActionModelWrite:         true,
		ActionExtensionRead:      true,
		ActionRouteRead:          true,
		ActionRouteWrite:         true,
		ActionRouteExplain:       true,
		ActionPolicyRead:         true,
		ActionPolicyProjectWrite: true,
		ActionMCPRead:            true,
		ActionMCPWrite:           true,
		ActionTraceReadMeta:      true,
		ActionBudgetRead:         true,
		ActionBudgetWrite:        true,
		ActionKillSwitchTrigger:  true,
		ActionPlaygroundCreate:   true,
	},
	RoleSecurityReviewer: {
		ActionOrgRead:            true,
		ActionProjectRead:        true,
		ActionMemberRead:         true,
		ActionVKeyRead:           true,
		ActionVKeyRevoke:         true, // Emergency revoke only (SCR-13).
		ActionSecretReadMeta:     true, // Zero secret write/probe (SCR-04).
		ActionConnectionRead:     true,
		ActionModelRead:          true,
		ActionExtensionRead:      true,
		ActionExtensionApprove:   true, // Capability approval / Four-Eyes sign-off (SCR-06/07).
		ActionRouteRead:          true,
		ActionRouteExplain:       true,
		ActionPolicyRead:         true,
		ActionMCPRead:            true,
		ActionMCPSchemaApprove:   true,
		ActionTraceReadMeta:      true,
		ActionTraceReadDebugBlob: true,
		ActionBudgetRead:         true,
		ActionApprovalRead:       true,
		ActionApprovalDecide:     true,
		ActionAuditReadVerify:    true,
		ActionKillSwitchTrigger:  true,
	},
	RoleFinOpsViewer: {
		// Strictly read-only across telemetry, models, routes, policies & budgets;
		// explicitly barred from PLAYGROUND (SCR-15), secrets, approvals, and mutations (AUTH-11).
		ActionOrgRead:        true,
		ActionProjectRead:    true,
		ActionSecretReadMeta: true,
		ActionConnectionRead: true,
		ActionModelRead:      true,
		ActionExtensionRead:  true,
		ActionRouteRead:      true,
		ActionRouteExplain:   true,
		ActionPolicyRead:     true,
		ActionTraceReadMeta:  true,
		ActionBudgetRead:     true,
	},
	RoleDeveloper: {
		ActionProjectRead:      true,
		ActionVKeyRead:         true,
		ActionModelRead:        true,
		ActionExtensionRead:    true,
		ActionRouteRead:        true,
		ActionRouteExplain:     true,
		ActionPolicyRead:       true,
		ActionMCPRead:          true,
		ActionTraceReadMeta:    true,
		ActionBudgetRead:       true, // Scoped to own project/key metadata only.
		ActionPlaygroundCreate: true,
	},
}

// knownActions tracks every canonical Action constant so unknown actions fail closed.
var knownActions = buildKnownActionSet()

func buildKnownActionSet() map[Action]bool {
	set := make(map[Action]bool)
	for _, perms := range rolePermissions {
		for act := range perms {
			set[act] = true
		}
	}
	return set
}

// ValidateRoutePermissions ensures no non-public Admin endpoint boots without an explicit registered Action.
func ValidateRoutePermissions(routes []RoutePermissionSpec) error {
	if len(routes) == 0 {
		return fmt.Errorf("%w: empty admin route registry", ErrInvalidRouteMetadata)
	}
	for _, r := range routes {
		method := strings.TrimSpace(r.Method)
		path := strings.TrimSpace(r.Path)
		if method == "" || path == "" {
			return fmt.Errorf("%w: route missing method or path", ErrInvalidRouteMetadata)
		}
		if r.Public {
			continue
		}
		if r.Action == "" || !knownActions[r.Action] {
			return fmt.Errorf("%w: admin route %s %s omits or uses unknown RBAC action %q", ErrInvalidRouteMetadata, method, path, r.Action)
		}
	}
	return nil
}

// ResolveSessionOrgID strictly prioritizes authenticated session context and ignores any
// client-supplied request body org_id field (AUTH-13 anti-spoofing invariant).
func ResolveSessionOrgID(actor *Actor, _ string) (string, error) {
	if actor == nil || strings.TrimSpace(actor.PrincipalID) == "" {
		return "", ErrUnauthenticated
	}
	if actor.SessionOrgID != nil && strings.TrimSpace(*actor.SessionOrgID) != "" {
		return *actor.SessionOrgID, nil
	}
	for _, b := range actor.Bindings {
		if b.ScopeType == ScopeOrg && b.OrgID != nil && strings.TrimSpace(*b.OrgID) != "" {
			return *b.OrgID, nil
		}
	}
	return "", ErrPermissionDenied
}

// Authorize evaluates whether actor is permitted to invoke action on target using fail-closed defaults.
// Returns:
//   - ErrUnauthenticated when actor identity is absent
//   - ErrInvalidCredentialPlane when a Data Plane Virtual Key credential touches Control Plane (AUTH-07)
//   - ErrResourceNotFound when a non-OPERATOR attempts to access a different organization's resource (AUTH-15)
//   - ErrPermissionDenied when actor's roles inside the authorized scope lack action permission (AUTH-11)
func Authorize(actor *Actor, action Action, target TargetScope) error {
	if actor == nil || strings.TrimSpace(actor.PrincipalID) == "" {
		return ErrUnauthenticated
	}
	switch actor.CredentialPlane {
	case CredentialPlaneAdminSession, CredentialPlaneAdminService, CredentialPlaneAdminOIDC:
	case CredentialPlaneDataVirtual:
		return ErrInvalidCredentialPlane
	default:
		return ErrInvalidCredentialPlane
	}

	if action == "" || !knownActions[action] {
		return ErrPermissionDenied
	}

	var matchedScopeBindings []RoleBinding
	hasInstanceOperator := false
	hasAnyOrgMatch := false

	for _, b := range actor.Bindings {
		if !allRoles[b.RoleName] {
			continue
		}
		if b.ScopeType == ScopeInstance {
			if b.RoleName == RoleOperator && b.OrgID == nil {
				hasInstanceOperator = true
				matchedScopeBindings = append(matchedScopeBindings, b)
			}
			continue
		}
		// Never honor RoleOperator on non-INSTANCE (ORG / PROJECT) bindings (CR-04 escalation defense).
		if b.RoleName == RoleOperator {
			continue
		}
		// Org/Project-scoped operations MUST carry a non-empty verified target.OrgID.
		if target.OrgID == "" {
			continue
		}
		if b.OrgID == nil || *b.OrgID != target.OrgID {
			continue
		}
		hasAnyOrgMatch = true
		switch b.ScopeType {
		case ScopeOrg:
			matchedScopeBindings = append(matchedScopeBindings, b)
		case ScopeProject:
			if target.ProjectID != "" && b.ScopeID == target.ProjectID {
				matchedScopeBindings = append(matchedScopeBindings, b)
			} else if target.ProjectID == "" && target.IsCollectionList && isProjectCollectionReadAction(action) {
				matchedScopeBindings = append(matchedScopeBindings, b)
			}
		}
	}

	// Cross-organization anti-oracle (AUTH-15): an Org A principal probing an Org B UUID receives
	// uniform 404 resource_not_found rather than 403 Permission Denied.
	if target.OrgID != "" && !hasInstanceOperator && !hasAnyOrgMatch {
		return ErrResourceNotFound
	}
	if len(matchedScopeBindings) == 0 {
		return ErrPermissionDenied
	}

	for _, b := range matchedScopeBindings {
		if b.ScopeType == ScopeProject {
			if target.ProjectID == "" && !(target.IsCollectionList && isProjectCollectionReadAction(action)) {
				continue
			}
			if target.ProjectID != "" && b.ScopeID != target.ProjectID {
				continue
			}
		}
		if b.RoleName == RoleTeamAdmin && (action == ActionSecretWrite || action == ActionConnectionWrite) && target.ProjectID == "" {
			// TEAM_ADMIN can never mutate Organization-owned secrets or connections (SCR-04 / TP-AC-F0203).
			continue
		}
		if rolePermissions[b.RoleName][action] {
			return nil
		}
	}

	return ErrPermissionDenied
}

func isProjectCollectionReadAction(action Action) bool {
	switch action {
	case ActionProjectRead, ActionModelRead, ActionExtensionRead, ActionRouteRead, ActionRouteExplain, ActionPolicyRead, ActionMCPRead, ActionTraceReadMeta, ActionBudgetRead, ActionVKeyRead:
		return true
	default:
		return false
	}
}
