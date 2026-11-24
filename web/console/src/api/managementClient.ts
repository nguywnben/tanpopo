import type {components, operations} from './generated';
import {api, apiError} from './client';

type RouteRequest = components['schemas']['RouteUpsertRequest'];
type Route = components['schemas']['RouteResource'];
type Policy = components['schemas']['PolicyDocument'];
type Key = components['schemas']['VirtualKeySummary'];
type Member = components['schemas']['MemberSummary'];

function versionTag(version: number) {
  return `"v${version}"`;
}

export async function listRoutesForEnvironment(environmentId: string) {
  const rows: Route[] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/routes', {params: {query: {env_id: environmentId, limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function createRoute(payload: RouteRequest) {
  const result = await api.POST('/admin/v1/routes', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateRoute(route: Route, payload: RouteRequest) {
  const result = await api.PUT('/admin/v1/routes/{routeId}', {
    params: {path: {routeId: route.id}, header: {'If-Match': versionTag(route.version)}}, body: payload,
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function deleteRoute(route: Route) {
  const result = await api.DELETE('/admin/v1/routes/{routeId}', {
    params: {path: {routeId: route.id}, header: {'If-Match': versionTag(route.version)}},
  });
  if (!result.response.ok) throw apiError(result.response, result.error);
}

export async function explainRouteDecision(payload: components['schemas']['RouteExplainRequest']) {
  const result = await api.POST('/admin/v1/routes/explain', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function createPlaygroundSession(payload: components['schemas']['PlaygroundSessionCreateRequest']) {
  const result = await api.POST('/admin/v1/playground/sessions', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listMcpServers() {
  const result = await api.GET('/admin/v1/mcp/servers', {params: {query: {limit: 100}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function registerMcpServer(payload: {
  project_id: string;
  slug: string;
  name: string;
  transport: 'streamable_http';
  endpoint_url: string;
  allow_private_ip?: boolean;
  allowed_cidrs?: string[];
}) {
  const result = await api.POST('/admin/v1/mcp/servers', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function syncMcpServerTools(serverId: string) {
  const result = await api.POST('/admin/v1/mcp/servers/{serverId}/sync-tools', {params: {path: {serverId}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function createMcpToolGrant(toolId: string, envId: string, virtualKeyId: string) {
  const result = await api.POST('/admin/v1/mcp/tools/{toolId}/grants', {
    params: {path: {toolId}}, body: {env_id: envId, virtual_key_id: virtualKeyId},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function approveMcpToolSchema(
  toolId: string,
  version: number,
  payload: {expected_schema_hash: string; risk_tier: 'read_only' | 'mutating' | 'destructive'; justification?: string},
) {
  const result = await api.POST('/admin/v1/mcp/tools/{toolId}/approve-schema', {
    params: {path: {toolId}, header: {'If-Match': versionTag(version)}},
    body: payload,
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listMcpToolGrants(toolId: string) {
  const result = await api.GET('/admin/v1/mcp/tools/{toolId}/grants', {params: {path: {toolId}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function revokeMcpToolGrant(grantId: string) {
  const result = await api.DELETE('/admin/v1/mcp/grants/{grantId}', {params: {path: {grantId}}});
  if (!result.response.ok) throw apiError(result.response, result.error);
}

export async function listA2aAgents() {
  const result = await api.GET('/admin/v1/a2a/agents', {params: {query: {limit: 100}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function registerA2aAgent(payload: components['schemas']['A2aAgentCreateRequest']) {
  const result = await api.POST('/admin/v1/a2a/agents', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listA2aTasks() {
  const result = await api.GET('/admin/v1/a2a/tasks', {params: {query: {limit: 50}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function resumeA2aTask(taskId: string, inputParts: Record<string, unknown>[]) {
  const result = await api.POST('/admin/v1/a2a/tasks/{taskId}/resume', {
    params: {path: {taskId}},
    body: {input_parts: inputParts},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function cancelA2aTask(taskId: string) {
  const result = await api.POST('/admin/v1/a2a/tasks/{taskId}/cancel', {
    params: {path: {taskId}},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listWebhooks() {
  const result = await api.GET('/admin/v1/webhooks', {params: {query: {limit: 100}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function createWebhook(payload: {
  project_id?: string;
  target_url: string;
  events: components['schemas']['WebhookEvents'];
  signing_credential_id: string;
}) {
  const result = await api.POST('/admin/v1/webhooks', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateWebhook(
  subscription: components['schemas']['WebhookSubscriptionResource'],
  payload: {enabled?: boolean; events?: components['schemas']['WebhookEvents']},
) {
  const result = await api.PATCH('/admin/v1/webhooks/{subscriptionId}', {
    params: {path: {subscriptionId: subscription.id}, header: {'If-Match': versionTag(subscription.version)}},
    body: payload,
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function deleteWebhook(subscription: components['schemas']['WebhookSubscriptionResource']) {
  const result = await api.DELETE('/admin/v1/webhooks/{subscriptionId}', {
    params: {path: {subscriptionId: subscription.id}, header: {'If-Match': versionTag(subscription.version)}},
  });
  if (!result.response.ok) throw apiError(result.response, result.error);
}

const securityStateListeners = new Set<() => void>();

export function subscribeSecurityStateChanged(listener: () => void): () => void {
  securityStateListeners.add(listener);
  return () => { securityStateListeners.delete(listener); };
}

export function notifySecurityStateChanged() {
  for (const listener of securityStateListeners) {
    try {
      listener();
    } catch {
      // Ignore individual subscriber errors during badge sync.
    }
  }
}

export async function listPendingApprovals() {
  const result = await api.GET('/admin/v1/approvals', {params: {query: {status: 'PENDING', limit: 100}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function decideApproval(item: components['schemas']['ApprovalRequestResource'], decision: 'APPROVE' | 'REJECT', reason: string) {
  const result = await api.POST('/admin/v1/approvals/{approvalId}/decide', {
    params: {path: {approvalId: item.id}, header: {'If-Match': `"v${item.version}"`}},
    body: {decision, expected_binding_hash: item.approval_binding_hash, reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  notifySecurityStateChanged();
  return result.data;
}

export async function startConnectionOAuth(connectionId: string, payload: {flow: 'authorization_code_pkce' | 'device_code' | 'client_credentials'; authorization_endpoint?: string; token_endpoint: string; device_authorization_endpoint?: string; client_id: string; scopes?: string[]; resource: string}) {
  const result = await api.POST('/admin/v1/connections/{connectionId}/oauth/start', {params: {path: {connectionId}}, body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function getEffectivePolicy(projectId?: string, environmentId?: string) {
  const result = await api.GET('/admin/v1/policies/effective', {params: {query: {project_id: projectId || undefined, env_id: environmentId || undefined}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return {policy: result.data, etag: result.response.headers.get('ETag') ?? versionTag(result.data.version)};
}

export async function updatePolicy(policy: Policy, etag: string, payload: Policy) {
  const result = await api.PUT('/admin/v1/policies/{policyId}', {
    params: {path: {policyId: policy.id}, header: {'If-Match': etag}}, body: payload,
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return {policy: result.data, etag: result.response.headers.get('ETag') ?? versionTag(result.data.version)};
}

export async function listVirtualKeys(projectId: string, environmentId: string) {
  const rows: Key[] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/virtual-keys', {params: {query: {project_id: projectId, env_id: environmentId, limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function createVirtualKey(payload: components['schemas']['VirtualKeyCreateRequest']) {
  const result = await api.POST('/admin/v1/virtual-keys', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateVirtualKey(
  key: Key,
  payload: NonNullable<operations['updateVirtualKey']['requestBody']>['content']['application/json'],
) {
  const result = await api.PATCH('/admin/v1/virtual-keys/{keyId}', {
    params: {path: {keyId: key.id}, header: {'If-Match': versionTag(key.version)}},
    body: payload,
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function rotateVirtualKey(key: Key, graceWindowSeconds: number) {
  const result = await api.POST('/admin/v1/virtual-keys/{keyId}/rotate', {
    params: {path: {keyId: key.id}, header: {'If-Match': versionTag(key.version)}}, body: {grace_window_seconds: graceWindowSeconds},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function revokeVirtualKey(key: Key, mode: 'GRACEFUL_DRAIN' | 'EMERGENCY_ABORT_STREAMS', reason: string) {
  const result = await api.POST('/admin/v1/virtual-keys/{keyId}/revoke', {
    params: {path: {keyId: key.id}, header: {'If-Match': versionTag(key.version)}}, body: {mode, reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listMembers(projectId?: string) {
  const rows: Member[] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/members', {params: {query: {project_id: projectId || undefined, limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function createMember(
  email: string,
  memberRoles: components['schemas']['RbacRole'][],
  projectIds?: string[],
) {
  const result = await api.POST('/admin/v1/members', {
    body: {
      email,
      kind: 'HUMAN',
      roles: memberRoles,
      ...(projectIds && projectIds.length > 0 ? {project_ids: projectIds} : {}),
    },
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function issueMemberEnrollmentToken(memberId: string) {
  const result = await api.POST('/admin/v1/members/{memberId}/enrollment-token', {
    params: {path: {memberId}},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateMemberRoles(
  member: Member,
  roles: components['schemas']['RbacRole'][],
  projectIds?: string[],
) {
  const result = await api.PATCH('/admin/v1/members/{memberId}/roles', {
    params: {path: {memberId: member.id}, header: {'If-Match': versionTag(member.version)}},
    body: {
      roles,
      ...(projectIds !== undefined ? {project_ids: projectIds} : {}),
    },
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listRecentTraces(environmentId: string) {
  const rows: components['schemas']['TraceSummary'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/traces', {params: {query: {env_id: environmentId, limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  const oneHourAgo = Date.now() - 60 * 60 * 1000;
  return rows.filter(row => Date.parse(row.created_at) >= oneHourAgo);
}

export async function listTracePage(environmentId: string, model?: string, cursor?: string) {
  const rows: components['schemas']['TraceSummary'][] = [];
  let nextCursor = cursor;
  for (let page = 0; page < 2; page += 1) {
    const result = await api.GET('/admin/v1/traces', {params: {query: {env_id: environmentId || undefined, model: model || undefined, limit: 100, cursor: nextCursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    nextCursor = result.data.next_cursor ?? undefined;
    if (!nextCursor) break;
  }
  return {data: rows, nextCursor};
}

export async function getTraceWaterfall(requestId: string) {
  const result = await api.GET('/admin/v1/traces/{requestId}', {params: {path: {requestId}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listBudgets(projectId?: string, includeAncestors?: boolean) {
  const result = await api.GET('/admin/v1/budgets', {
    params: {
      query: {
        project_id: projectId || undefined,
        include_ancestors: projectId && includeAncestors ? true : undefined,
        limit: 100,
      },
    },
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function createBudget(
  budgetId: string,
  payload: {
    scope_tier: components['schemas']['BudgetResource']['scope_tier'];
    scope_id: string;
    hard_limit_micros: number;
    period: components['schemas']['BudgetResource']['period'];
    soft_warning_percents?: number[];
  },
) {
  const result = await api.PUT('/admin/v1/budgets/{budgetId}', {
    params: {path: {budgetId}, header: {'If-Match': '"v1"'}},
    body: payload,
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateBudget(budget: components['schemas']['BudgetResource'], hardLimitMicros: number) {
  const result = await api.PUT('/admin/v1/budgets/{budgetId}', {
    params: {path: {budgetId: budget.id}, header: {'If-Match': `"v${budget.version}"`}},
    body: {scope_tier: budget.scope_tier, scope_id: budget.scope_id, hard_limit_micros: hardLimitMicros, period: budget.period, soft_warning_percents: budget.soft_warning_percents},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function getUsageChargebackSummary(groupBy: 'project' | 'env' | 'key' | 'model') {
  const result = await api.GET('/admin/v1/usage/summary', {params: {query: {group_by: groupBy}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function exportUsageChargeback(
  format: 'csv' | 'json',
  groupBy: 'project' | 'env' | 'key' | 'model' = 'project',
) {
  const result = await api.GET('/admin/v1/usage/export', {
    params: {query: {format, group_by: groupBy}},
    parseAs: format === 'csv' ? 'text' : 'json',
  });
  if (!result.response.ok || result.data === undefined) throw apiError(result.response, result.error);
  const content = typeof result.data === 'string' ? result.data : JSON.stringify(result.data, null, 2);
  return new Blob([content], {type: format === 'csv' ? 'text/csv;charset=utf-8' : 'application/json'});
}

export async function listAuditEvents(action?: string, cursor?: string, chainPartition?: string) {
  const result = await api.GET('/admin/v1/audit-events', {params: {query: {action: action || undefined, chain_partition: chainPartition || undefined, limit: 100, cursor}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return {data: result.data.data, nextCursor: result.data.next_cursor ?? undefined};
}

export async function verifyAuditHashChain() {
  const result = await api.POST('/admin/v1/audit-events/verify');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function triggerKillSwitch(payload: components['schemas']['KillSwitchTriggerRequest']) {
  const result = await api.POST('/admin/v1/kill-switches', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  notifySecurityStateChanged();
  return result.data;
}

export async function listKillSwitches() {
  const result = await api.GET('/admin/v1/kill-switches', {params: {query: {active_only: true}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function clearKillSwitch(item: components['schemas']['KillSwitchResource'], clear_reason: string) {
  const result = await api.DELETE('/admin/v1/kill-switches/{switchId}', {
    params: {path: {switchId: item.id}, query: {org_id: item.org_id ?? undefined}},
    body: {expected_version: item.version, clear_reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  notifySecurityStateChanged();
  return result.data;
}

export async function getKillSwitchBlastRadius(target_type: 'PROJECT' | 'VIRTUAL_KEY' | 'EXTENSION' | 'PROVIDER_CONNECTION', target_id: string) {
  const result = await api.GET('/admin/v1/blast-radius', {params: {query: {target_type, target_id}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function getClusterOpsStatus() {
  const result = await api.GET('/admin/v1/ops/cluster-status');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function createEncryptedBackup() {
  const result = await api.POST('/admin/v1/ops/backups');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function runUpgradePreflight(targetCoreVersion: string) {
  const result = await api.POST('/admin/v1/ops/upgrades/preflight', {body: {target_core_version: targetCoreVersion}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function getReadinessProbe() {
  const response = await fetch(new URL('/readyz', window.location.origin), {credentials: 'same-origin', headers: {Accept: 'application/json'}});
  const data = await response.json() as {status?: string; config_epoch?: number; snapshot_age_ms?: number};
  return {ready: response.ok && (data.status === 'ok' || data.status === 'ready'), status: data.status ?? (response.ok ? 'unknown' : `HTTP ${response.status}`), configEpoch: data.config_epoch, snapshotAgeMs: data.snapshot_age_ms};
}

export async function createOrganization(name: string, slug: string) {
  const result = await api.POST('/admin/v1/organizations', {body: {name, slug}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function deleteOrganization(org: components['schemas']['Organization'], confirmSlug: string, reason: string) {
  const result = await api.DELETE('/admin/v1/organizations/{orgId}', {
    params: {path: {orgId: org.id}, header: {'If-Match': versionTag(org.version)}},
    body: {confirm_slug: confirmSlug, reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}
