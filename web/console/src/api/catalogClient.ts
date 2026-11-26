import type {components} from './generated';
import {api, apiError} from './client';

export async function listProviderAdapters() {
  const rows: components['schemas']['ProviderAdapterSummary'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/provider-adapters', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function getProviderAdapter(id: string) {
  const result = await api.GET('/admin/v1/provider-adapters/{adapterId}', {params: {path: {adapterId: id}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listConnections() {
  const rows: components['schemas']['ProviderConnection'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/connections', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function listSecretMetadata() {
  const rows: components['schemas']['SecretMetadata'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/secrets', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function createSecret(payload: components['schemas']['SecretCreateRequest']) {
  const result = await api.POST('/admin/v1/secrets', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function deleteSecret(credentialId: string, etag: string) {
  const result = await api.DELETE('/admin/v1/secrets/{credentialId}', {
    params: {path: {credentialId}, header: {'If-Match': etag}},
  });
  if (!result.response.ok) throw apiError(result.response, result.error);
}

export async function createConnection(payload: components['schemas']['ProviderConnectionCreateRequest']) {
  const result = await api.POST('/admin/v1/connections', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateConnection(id: string, etag: string, payload: {state?: 'ACTIVE' | 'DISABLED'; name?: string; endpoint_url?: string; secret_bindings?: Record<string, string>}) {
  const result = await api.PATCH('/admin/v1/connections/{connectionId}', {params: {path: {connectionId: id}, header: {'If-Match': etag}}, body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function deleteConnection(id: string, etag: string) {
  const result = await api.DELETE('/admin/v1/connections/{connectionId}', {params: {path: {connectionId: id}, header: {'If-Match': etag}}});
  if (!result.response.ok) throw apiError(result.response, result.error);
}

export async function listRoutes() {
  const rows: components['schemas']['RouteResource'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/routes', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function listAllRoutes() {
  return listRoutes();
}

export async function testConnection(id: string) {
  const result = await api.POST('/admin/v1/connections/{connectionId}/test', {params: {path: {connectionId: id}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listAdminModels() {
  const rows: components['schemas']['LogicalModel'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/models', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function createAdminModel(payload: components['schemas']['LogicalModel']) {
  const result = await api.POST('/admin/v1/models', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listDeployments(envId?: string, projectId?: string) {
  const rows: components['schemas']['ModelDeployment'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/deployments', {
      params: {query: {env_id: envId || undefined, project_id: projectId || undefined, limit: 100, cursor}},
    });
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function listAllDeployments() {
  return listDeployments();
}

export async function createDeployment(payload: {env_id: string; logical_model_id: string; connection_id: string; upstream_model_name: string; data_region: components['schemas']['DataRegion']; activate_immediately: boolean; rate_card: components['schemas']['RateCardNanoUsd']}) {
  const result = await api.POST('/admin/v1/deployments', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function updateDeployment(id: string, etag: string, payload: {state?: 'ACTIVE_HEALTHY' | 'DRAINING_MAINTENANCE' | 'DISABLED'}) {
  const result = await api.PATCH('/admin/v1/deployments/{deploymentId}', {params: {path: {deploymentId: id}, header: {'If-Match': etag}}, body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function deleteDeployment(id: string, etag: string) {
  const result = await api.DELETE('/admin/v1/deployments/{deploymentId}', {params: {path: {deploymentId: id}, header: {'If-Match': etag}}});
  if (!result.response.ok) throw apiError(result.response, result.error);
}

export async function listMarketplaceCatalog() {
  const rows: components['schemas']['MarketplaceCatalogEntry'][] = [];
  let cursor: string | undefined;
  let offlineCached = false;
  do {
    const result = await api.GET('/admin/v1/marketplace/catalog', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    offlineCached ||= result.data.offline_cached;
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return {data: rows, offline_cached: offlineCached, has_more: false};
}

export async function listExtensionInstallations() {
  const rows: components['schemas']['ExtensionInstallation'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/extensions/installations', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}

export async function inspectExtensionPermissions(id: string) {
  const result = await api.GET('/admin/v1/extensions/{installationId}/permission-diff', {params: {path: {installationId: id}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return {data: result.data, etag: result.response.headers.get('ETag') ?? ''};
}

export async function stageExtensionInstall(sourceUri: string, sha256: string) {
  const result = await api.POST('/admin/v1/extensions/install', {body: {source_uri: sourceUri, expected_sha256: sha256}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function upgradeExtension(id: string, etag: string, sourceUri: string, sha256: string) {
  const result = await api.POST('/admin/v1/extensions/{installationId}/upgrade', {
    params: {path: {installationId: id}, header: {'If-Match': etag}}, body: {source_uri: sourceUri, expected_sha256: sha256},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return {data: result.data, etag: result.response.headers.get('ETag') ?? ''};
}

export async function uninstallExtension(id: string, etag: string) {
  const result = await api.DELETE('/admin/v1/extensions/{installationId}', {params: {path: {installationId: id}, header: {'If-Match': etag}}});
  if (!result.response.ok) throw apiError(result.response, result.error);
}

export async function getExtensionLockfile() {
  const result = await api.GET('/admin/v1/extensions/lockfile', {headers: {Accept: 'application/json'}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function getExtensionBlastRadius(id: string) {
  const result = await api.GET('/admin/v1/blast-radius', {params: {query: {target_type: 'EXTENSION', target_id: id}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function approveExtension(id: string, etag: string, enableImmediately: boolean, acknowledgeUnverified: boolean) {
  const result = await api.POST('/admin/v1/extensions/{installationId}/approve-permissions', {
    params: {path: {installationId: id}, header: {'If-Match': etag}},
    body: {enable_immediately: enableImmediately, acknowledge_unverified_external: acknowledgeUnverified},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function rollbackExtension(id: string, etag: string, reason: string) {
  const result = await api.POST('/admin/v1/extensions/{installationId}/rollback', {
    params: {path: {installationId: id}, header: {'If-Match': etag}}, body: {reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function quarantineExtension(id: string, etag: string, reason: string) {
  const result = await api.POST('/admin/v1/extensions/{installationId}/quarantine', {
    params: {path: {installationId: id}, header: {'If-Match': etag}}, body: {reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function unquarantineExtension(id: string, etag: string, reason: string) {
  const result = await api.POST('/admin/v1/extensions/{installationId}/unquarantine', {
    params: {path: {installationId: id}, header: {'If-Match': etag}}, body: {reason},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function triggerExtensionKillSwitch(id: string, reason: string, confirmSlug: string) {
  const result = await api.POST('/admin/v1/kill-switches', {body: {target_level: 'EXTENSION', target_id: id, confirm_slug: confirmSlug, abort_active_streams: true, reason}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}
