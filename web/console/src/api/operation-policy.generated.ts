// Generated from ../../openapi/admin-v1.yaml; do not edit.
export const operationPolicy = {
  "GET /admin/v1/bootstrap/status": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/bootstrap/complete": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/auth/login": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/auth/oidc/start": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/auth/oidc/callback": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/auth/oidc/refresh": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/auth/session": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/auth/logout": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/organizations": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/organizations": {
    "csrf": true,
    "ifMatch": false
  },
  "DELETE /admin/v1/organizations/{orgId}": {
    "csrf": true,
    "ifMatch": true
  },
  "PUT /admin/v1/organizations/{orgId}/extension-policies": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/projects": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/projects": {
    "csrf": true,
    "ifMatch": false
  },
  "DELETE /admin/v1/projects/{projectId}": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/projects/{projectId}/environments": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/projects/{projectId}/environments": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/projects/{projectId}/virtual-keys": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/virtual-keys": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/virtual-keys": {
    "csrf": true,
    "ifMatch": false
  },
  "PATCH /admin/v1/virtual-keys/{keyId}": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/virtual-keys/{keyId}/rotate": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/virtual-keys/{keyId}/revoke": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/members": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/members": {
    "csrf": true,
    "ifMatch": false
  },
  "PATCH /admin/v1/members/{memberId}/roles": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/overview/summary": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/alerts/active": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/provider-adapters": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/provider-adapters": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/provider-adapters/{adapterId}": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/secrets": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/secrets": {
    "csrf": true,
    "ifMatch": false
  },
  "DELETE /admin/v1/secrets/{credentialId}": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/connections": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/connections": {
    "csrf": true,
    "ifMatch": false
  },
  "PATCH /admin/v1/connections/{connectionId}": {
    "csrf": true,
    "ifMatch": true
  },
  "DELETE /admin/v1/connections/{connectionId}": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/connections/{connectionId}/test": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/connections/{connectionId}/oauth/start": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/oauth/callback": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/oauth/device/poll": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/models": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/models": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/deployments": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/deployments": {
    "csrf": true,
    "ifMatch": false
  },
  "PATCH /admin/v1/deployments/{deploymentId}": {
    "csrf": true,
    "ifMatch": true
  },
  "DELETE /admin/v1/deployments/{deploymentId}": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/marketplace/catalog": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/extensions/install": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/extensions/installations": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/extensions/{installationId}/permission-diff": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/extensions/{installationId}/approve-permissions": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/extensions/{installationId}/upgrade": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/extensions/{installationId}/quarantine": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/extensions/{installationId}/rollback": {
    "csrf": true,
    "ifMatch": true
  },
  "DELETE /admin/v1/extensions/{installationId}": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/extensions/lockfile": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/routes": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/routes": {
    "csrf": true,
    "ifMatch": false
  },
  "PUT /admin/v1/routes/{routeId}": {
    "csrf": true,
    "ifMatch": true
  },
  "DELETE /admin/v1/routes/{routeId}": {
    "csrf": true,
    "ifMatch": true
  },
  "POST /admin/v1/routes/explain": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/policies/effective": {
    "csrf": false,
    "ifMatch": false
  },
  "PUT /admin/v1/policies/{policyId}": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/mcp/servers": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/mcp/servers": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/mcp/servers/{serverId}/sync-tools": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/mcp/tools/{toolId}/approve-schema": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/mcp/tools/{toolId}/grants": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/mcp/tools/{toolId}/grants": {
    "csrf": true,
    "ifMatch": false
  },
  "DELETE /admin/v1/mcp/grants/{grantId}": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/a2a/agents": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/a2a/agents": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/a2a/tasks": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/traces": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/traces/{requestId}": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/traces/{requestId}/debug-payload": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/budgets": {
    "csrf": false,
    "ifMatch": false
  },
  "PUT /admin/v1/budgets/{budgetId}": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/usage/summary": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/usage/export": {
    "csrf": false,
    "ifMatch": false
  },
  "GET /admin/v1/webhooks": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/webhooks": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/approvals": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/approvals/{approvalId}/decide": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/change-requests": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/change-requests": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/change-requests/{requestId}/decide": {
    "csrf": true,
    "ifMatch": true
  },
  "GET /admin/v1/audit-events": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/audit-events/verify": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/kill-switches": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/kill-switches": {
    "csrf": true,
    "ifMatch": false
  },
  "DELETE /admin/v1/kill-switches/{switchId}": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/blast-radius": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/playground/sessions": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/openapi.json": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/diagnostics/smoke-test": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/diagnostics/support-bundle": {
    "csrf": true,
    "ifMatch": false
  },
  "GET /admin/v1/ops/cluster-status": {
    "csrf": false,
    "ifMatch": false
  },
  "POST /admin/v1/ops/backups": {
    "csrf": true,
    "ifMatch": false
  },
  "POST /admin/v1/ops/upgrades/preflight": {
    "csrf": true,
    "ifMatch": false
  },
  "PUT /admin/v1/settings/retention": {
    "csrf": true,
    "ifMatch": true
  }
} as const;
