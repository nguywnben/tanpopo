import type {components} from './generated';
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

export async function listMembers() {
  const rows: Member[] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/members', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function createMember(email: string, memberRoles: components['schemas']['RbacRole'][]) {
  const result = await api.POST('/admin/v1/members', {body: {email, kind: 'HUMAN', roles: memberRoles}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateMemberRoles(member: Member, roles: components['schemas']['RbacRole'][]) {
  const result = await api.PATCH('/admin/v1/members/{memberId}/roles', {
    params: {path: {memberId: member.id}, header: {'If-Match': versionTag(member.version)}}, body: {roles},
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

export async function listTracePage(environmentId: string, model?: string) {
  const rows: components['schemas']['TraceSummary'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/traces', {params: {query: {env_id: environmentId || undefined, model: model || undefined, limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor && rows.length < 200);
  return rows.slice(0, 200);
}

export async function getTraceWaterfall(requestId: string) {
  const result = await api.GET('/admin/v1/traces/{requestId}', {params: {path: {requestId}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listBudgets(projectId?: string) {
  const result = await api.GET('/admin/v1/budgets', {params: {query: {project_id: projectId || undefined, limit: 100}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
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

export async function exportUsageChargeback(format: 'csv' | 'json') {
  const result = await api.GET('/admin/v1/usage/export', {params: {query: {format, group_by: 'project'}}});
  if (!result.response.ok || result.data === undefined) throw apiError(result.response, result.error);
  const content = typeof result.data === 'string' ? result.data : JSON.stringify(result.data);
  return new Blob([content], {type: format === 'csv' ? 'text/csv' : 'application/json'});
}

export async function listAuditEvents(action?: string) {
  const result = await api.GET('/admin/v1/audit-events', {params: {query: {action: action || undefined, limit: 100}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function verifyAuditHashChain() {
  const result = await api.POST('/admin/v1/audit-events/verify');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function triggerKillSwitch(payload: components['schemas']['KillSwitchTriggerRequest']) {
  const result = await api.POST('/admin/v1/kill-switches', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function getKillSwitchBlastRadius(target_type: 'EXTENSION' | 'PROVIDER_CONNECTION', target_id: string) {
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
  return {ready: response.ok && data.status === 'ok', status: data.status ?? (response.ok ? 'unknown' : `HTTP ${response.status}`), configEpoch: data.config_epoch, snapshotAgeMs: data.snapshot_age_ms};
}
