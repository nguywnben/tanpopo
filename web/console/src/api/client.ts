import type {components, paths} from './generated';

type Method = 'get' | 'post' | 'put' | 'patch' | 'delete';

export type RequestOptions = {
  pathParams?: Record<string, string>;
  query?: Record<string, string | number | boolean | undefined>;
  body?: unknown;
  ifMatch?: string;
  signal?: AbortSignal;
};

let csrfToken: string | undefined;

export function rememberCsrfToken(token: string) {
  csrfToken = token;
}

export function clearCsrfToken() {
  csrfToken = undefined;
}

export async function request<T = unknown>(
  path: keyof paths,
  method: Method,
  options: RequestOptions = {},
): Promise<T> {
  let url = String(path);
  for (const [name, value] of Object.entries(options.pathParams ?? {})) {
    url = url.replace(`{${name}}`, encodeURIComponent(value));
  }
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(options.query ?? {})) {
    if (value !== undefined) search.set(key, String(value));
  }
  if (search.size > 0) url += `?${search.toString()}`;

  const headers = new Headers({Accept: 'application/json'});
  const mutating = method !== 'get';
  if (mutating) {
    if (!csrfToken) throw new Error('A CSRF token is required for state-changing requests.');
    headers.set('X-CSRF-Token', csrfToken);
  }
  if (options.ifMatch) headers.set('If-Match', options.ifMatch);
  if (options.body !== undefined) headers.set('Content-Type', 'application/json');

  const response = await fetch(url, {
    method: method.toUpperCase(),
    credentials: 'same-origin',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  });
  if (!response.ok) {
    const body = await response.json().catch(() => undefined) as components['schemas']['CanonicalErrorEnvelope'] | undefined;
    const error = new Error(body?.error?.message ?? `Admin API request failed (${response.status}).`);
    Object.assign(error, {status: response.status, code: body?.error?.code});
    throw error;
  }
  if (response.status === 204) return undefined as T;
  return await response.json() as T;
}

export async function getAdminSession(): Promise<components['schemas']['AdminSessionResponse']> {
  const session = await request<components['schemas']['AdminSessionResponse']>('/admin/v1/auth/session', 'get');
  if (session?.csrf_token) rememberCsrfToken(session.csrf_token);
  return session;
}

export function listProjects(): Promise<{data: components['schemas']['Project'][]; has_more: boolean; next_cursor?: string | null}> {
  return request('/admin/v1/projects', 'get', {query: {limit: 200}});
}

export function listEnvironments(projectId: string): Promise<{data: components['schemas']['Environment'][]}> {
  return request('/admin/v1/projects/{projectId}/environments', 'get', {
    pathParams: {projectId},
  });
}
