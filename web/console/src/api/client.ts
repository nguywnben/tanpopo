import createClient from 'openapi-fetch';
import type {components, paths} from './generated';
import {operationPolicy} from './operation-policy.generated';
import {getActiveLocale, translateRuntime} from '../i18n/I18nProvider';

export const api = createClient<paths>({
  baseUrl: typeof window === 'undefined' ? '' : window.location.origin,
  credentials: 'same-origin',
  fetch: request => globalThis.fetch(request),
});

let csrfToken: string | undefined;
let activeOrgId: string | undefined;
const cachePurgeListeners = new Set<() => void>();

const operationMatchers = Object.entries(operationPolicy).map(([operation, policy]) => {
  const [method, route] = operation.split(' ', 2);
  const pattern = route.split('/').map(segment => segment.startsWith('{') && segment.endsWith('}') ? '[^/]+' : segment.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('/');
  return {method, pattern: new RegExp(`^${pattern}$`), policy};
});

function policyFor(request: Request) {
  const pathname = new URL(request.url).pathname;
  return operationMatchers.find(operation => operation.method === request.method.toUpperCase() && operation.pattern.test(pathname))?.policy;
}

export function rememberCsrfToken(token: string) {
  csrfToken = token;
}

export function clearCsrfToken() {
  csrfToken = undefined;
  activeOrgId = undefined;
}

export function setActiveOrgId(orgId: string | undefined) {
  const normalized = orgId?.trim();
  activeOrgId = normalized ? normalized : undefined;
}

export function getActiveOrgId(): string | undefined {
  return activeOrgId;
}

export function subscribeOrgCachePurge(listener: () => void): () => void {
  cachePurgeListeners.add(listener);
  return () => { cachePurgeListeners.delete(listener); };
}

export function purgeOrgScopedClientState() {
  for (const clearFn of cachePurgeListeners) {
    try {
      clearFn();
    } catch {
      // Ignore individual cache listener failures during tenant purge.
    }
  }
  if (typeof window === 'undefined') return;
  try {
    const url = new URL(window.location.href);
    let dirty = false;
    for (const key of ['org_id', 'project_id', 'env_id', 'request_id']) {
      if (url.searchParams.has(key)) {
        url.searchParams.delete(key);
        dirty = true;
      }
    }
    if (dirty) {
      window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`);
    }
  } catch {
    // Ignore read-only location errors.
  }
  for (const storage of [window.sessionStorage, window.localStorage]) {
    try {
      const toDelete: string[] = [];
      for (let i = 0; i < storage.length; i++) {
        const key = storage.key(i);
        if (key && (key.startsWith('tanpopo.playground') || key.startsWith('tp_playground'))) {
          toDelete.push(key);
        }
      }
      for (const key of toDelete) storage.removeItem(key);
    } catch {
      // Storage may be unavailable in restricted contexts.
    }
  }
}

api.use({
  onRequest({request}) {
    const method = request.method.toUpperCase();
    const pathname = new URL(request.url).pathname;
    const headers = new Headers(request.headers);
    if (!headers.has('Accept-Language')) {
      headers.set('Accept-Language', getActiveLocale());
    }
    if (
      activeOrgId
      && !headers.has('X-Tanpopo-Org-ID')
      && pathname.startsWith('/admin/v1/')
      && !pathname.startsWith('/admin/v1/auth/login')
      && !pathname.startsWith('/admin/v1/bootstrap/')
    ) {
      headers.set('X-Tanpopo-Org-ID', activeOrgId);
    }
    if (['GET', 'HEAD', 'OPTIONS'].includes(method)) {
      return new Request(request, {headers});
    }
    const policy = policyFor(request);
    if (!policy) {
      throw new Error(translateRuntime('error.noOpenApiPolicy', {method, path: pathname}));
    }
    if (policy.ifMatch && !headers.has('If-Match')) {
      throw new Error(translateRuntime('error.ifMatchRequired'));
    }
    if (!policy.csrf) {
      return new Request(request, {headers});
    }
    if (!csrfToken) {
      throw new Error(translateRuntime('error.csrfRequired'));
    }
    headers.set('X-CSRF-Token', csrfToken);
    return new Request(request, {headers});
  },
  onResponse({request, response}) {
    if (response.status !== 401) return response;
    const pathname = new URL(request.url).pathname;
    if (
      pathname.startsWith('/admin/v1/')
      && !pathname.startsWith('/admin/v1/auth/login')
      && !pathname.startsWith('/admin/v1/auth/enroll')
      && !pathname.startsWith('/admin/v1/auth/oidc/')
      && !pathname.startsWith('/admin/v1/bootstrap/')
    ) {
      clearCsrfToken();
      purgeOrgScopedClientState();
      notifyUnauthorized();
    }
    return response;
  },
});

const unauthorizedListeners = new Set<() => void>();
const crossTabLogoutListeners = new Set<() => void>();
const AUTH_BROADCAST_CHANNEL = 'tanpopo.auth';
const AUTH_STORAGE_EVENT_KEY = 'tanpopo.auth.logout_ts';
let authChannel: BroadcastChannel | null = null;
let storageSubscribed = false;

function ensureCrossTabAuthListeners() {
  if (typeof window === 'undefined') return;
  if (!authChannel && typeof BroadcastChannel !== 'undefined') {
    try {
      authChannel = new BroadcastChannel(AUTH_BROADCAST_CHANNEL);
      authChannel.onmessage = event => {
        if (event.data && typeof event.data === 'object' && event.data.type === 'logout') {
          handleRemoteLogoutSignal();
        }
      };
    } catch {
      authChannel = null;
    }
  }
  if (!storageSubscribed) {
    storageSubscribed = true;
    window.addEventListener('storage', event => {
      if (event.key === AUTH_STORAGE_EVENT_KEY && event.newValue) {
        handleRemoteLogoutSignal();
      }
    });
  }
}

function handleRemoteLogoutSignal() {
  clearCsrfToken();
  purgeOrgScopedClientState();
  for (const listener of crossTabLogoutListeners) {
    try {
      listener();
    } catch {
      // Ignore individual subscriber errors during cross-tab logout.
    }
  }
}

export function broadcastLogoutEvent() {
  if (typeof window === 'undefined') return;
  const payload = {type: 'logout', ts: Date.now(), nonce: Math.random().toString(36).slice(2)};
  try {
    ensureCrossTabAuthListeners();
    authChannel?.postMessage(payload);
  } catch {
    // Ignore BroadcastChannel errors.
  }
  try {
    window.localStorage.setItem(AUTH_STORAGE_EVENT_KEY, JSON.stringify(payload));
  } catch {
    // Storage may be unavailable in restricted contexts.
  }
}

export function subscribeUnauthorized(listener: () => void): () => void {
  unauthorizedListeners.add(listener);
  return () => { unauthorizedListeners.delete(listener); };
}

export function subscribeCrossTabLogout(listener: () => void): () => void {
  ensureCrossTabAuthListeners();
  crossTabLogoutListeners.add(listener);
  return () => { crossTabLogoutListeners.delete(listener); };
}

function notifyUnauthorized() {
  broadcastLogoutEvent();
  for (const listener of unauthorizedListeners) {
    try {
      listener();
    } catch {
      // Ignore individual subscriber errors during session teardown.
    }
  }
}

export function apiError(response: Response, error: unknown) {
  const detail = error && typeof error === 'object' && 'error' in error
    ? (error as components['schemas']['CanonicalErrorEnvelope']).error
    : undefined;
  const failure = new Error(detail?.message ?? translateRuntime('common.apiRequestFailed', {status: response.status}));
  Object.assign(failure, {status: response.status, code: detail?.code, currentResource: detail?.current_resource});
  return failure;
}

export async function getAdminSession(): Promise<components['schemas']['AdminSessionResponse']> {
  const result = await api.GET('/admin/v1/auth/session');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  rememberCsrfToken(result.data.csrf_token);
  return result.data;
}

export async function getBootstrapStatus(): Promise<components['schemas']['BootstrapStatusResponse']> {
  const result = await api.GET('/admin/v1/bootstrap/status');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function completeBootstrap(payload: components['schemas']['BootstrapCompleteRequest']) {
  const result = await api.POST('/admin/v1/bootstrap/complete', {body: payload});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  rememberCsrfToken(result.data.csrf_token);
  return result.data;
}

export async function loginAdmin(email: string, password: string) {
  const result = await api.POST('/admin/v1/auth/login', {body: {email, password}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  rememberCsrfToken(result.data.csrf_token);
  return result.data;
}

export async function enrollLocalMember(token: string, password: string, orgId?: string) {
  const result = await api.POST('/admin/v1/auth/enroll', {
    body: {
      token,
      password,
      ...(orgId ? {org_id: orgId} : {}),
    },
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  rememberCsrfToken(result.data.csrf_token);
  return result.data;
}

export async function logoutAdmin() {
  const result = await api.POST('/admin/v1/auth/logout');
  clearCsrfToken();
  purgeOrgScopedClientState();
  broadcastLogoutEvent();
  if (!result.response.ok && result.response.status !== 204 && result.response.status !== 401) {
    throw apiError(result.response, result.error);
  }
}

export async function getOverviewSummary(window: components['schemas']['OverviewSummaryResponse']['window']) {
  const result = await api.GET('/admin/v1/overview/summary', {params: {query: {window}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listUnhealthyDeployments() {
  const result = await api.GET('/admin/v1/deployments', {params: {query: {status: 'CIRCUIT_OPEN_UNHEALTHY', limit: 50}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function listActiveAlerts() {
  const result = await api.GET('/admin/v1/alerts/active', {params: {query: {limit: 50}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data.data;
}

export async function listProjects() {
  const result = await api.GET('/admin/v1/projects', {params: {query: {limit: 200}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listEnvironments(projectId: string) {
  const result = await api.GET('/admin/v1/projects/{projectId}/environments', {params: {path: {projectId}}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function createProject(name: string, slug: string) {
  const result = await api.POST('/admin/v1/projects', {body: {name, slug}});
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function createEnvironment(projectId: string, name: string, slug: string) {
  const result = await api.POST('/admin/v1/projects/{projectId}/environments', {
    params: {path: {projectId}},
    body: {name, slug},
  });
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  return result.data;
}

export async function listOrganizations(): Promise<components['schemas']['Organization'][]> {
  const rows: components['schemas']['Organization'][] = [];
  let cursor: string | undefined;
  do {
    const result = await api.GET('/admin/v1/organizations', {params: {query: {limit: 100, cursor}}});
    if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
    rows.push(...result.data.data);
    cursor = result.data.next_cursor ?? undefined;
  } while (cursor);
  return rows;
}
