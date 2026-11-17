import createClient from 'openapi-fetch';
import type {components, paths} from './generated';

export const api = createClient<paths>({
  baseUrl: typeof window === 'undefined' ? '' : window.location.origin,
  credentials: 'same-origin',
  fetch: request => globalThis.fetch(request),
});

let csrfToken: string | undefined;

export function rememberCsrfToken(token: string) {
  csrfToken = token;
}

export function clearCsrfToken() {
  csrfToken = undefined;
}

api.use({
  onRequest({request}) {
    if (['GET', 'HEAD', 'OPTIONS'].includes(request.method.toUpperCase())) return request;
    if (!csrfToken) throw new Error('A CSRF token is required for state-changing requests.');
    const headers = new Headers(request.headers);
    headers.set('X-CSRF-Token', csrfToken);
    return new Request(request, {headers});
  },
});

function apiError(response: Response, error: unknown) {
  const detail = error && typeof error === 'object' && 'error' in error
    ? (error as components['schemas']['CanonicalErrorEnvelope']).error
    : undefined;
  const failure = new Error(detail?.message ?? `Admin API request failed (${response.status}).`);
  Object.assign(failure, {status: response.status, code: detail?.code});
  return failure;
}

export async function getAdminSession(): Promise<components['schemas']['AdminSessionResponse']> {
  const result = await api.GET('/admin/v1/auth/session');
  if (!result.response.ok || !result.data) throw apiError(result.response, result.error);
  rememberCsrfToken(result.data.csrf_token);
  return result.data;
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
