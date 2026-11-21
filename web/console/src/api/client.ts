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
}

api.use({
  onRequest({request}) {
    const method = request.method.toUpperCase();
    const headers = new Headers(request.headers);
    if (!headers.has('Accept-Language')) {
      headers.set('Accept-Language', getActiveLocale());
    }
    if (['GET', 'HEAD', 'OPTIONS'].includes(method)) {
      return new Request(request, {headers});
    }
    const policy = policyFor(request);
    if (!policy) {
      throw new Error(translateRuntime('error.noOpenApiPolicy', {method, path: new URL(request.url).pathname}));
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
});

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

export async function logoutAdmin() {
  const result = await api.POST('/admin/v1/auth/logout');
  clearCsrfToken();
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
