import {afterEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, getAdminSession, rememberCsrfToken, request} from './client';

afterEach(() => {
  clearCsrfToken();
  vi.unstubAllGlobals();
});

describe('typed Admin API client', () => {
  it('keeps session cookies same-origin and holds the returned CSRF token in memory', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        principal_id: 'principal-1', email: 'admin@example.test', roles: ['ORG_OWNER'],
        csrf_token: 'csrf-session-token', expires_at: '2026-12-01T00:00:00Z',
      }), {status: 200, headers: {'Content-Type': 'application/json'}}))
      .mockResolvedValueOnce(new Response(JSON.stringify({data: [], has_more: false}), {status: 200}));
    vi.stubGlobal('fetch', fetchMock);

    await getAdminSession();
    await request('/admin/v1/projects', 'get');

    expect(fetchMock.mock.calls[0][1]).toMatchObject({credentials: 'same-origin', method: 'GET'});
    const nextHeaders = new Headers(fetchMock.mock.calls[1][1]?.headers);
    expect(nextHeaders.get('X-CSRF-Token')).toBeNull();
  });

  it('adds CSRF and If-Match to writes without persisting credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, {status: 204}));
    vi.stubGlobal('fetch', fetchMock);
    rememberCsrfToken('csrf-test-token');

    await request('/admin/v1/projects/{projectId}', 'put', {
      pathParams: {projectId: 'project/one'}, ifMatch: '"v3"', body: {name: 'Updated'},
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    const headers = new Headers(init.headers);
    expect(url).toBe('/admin/v1/projects/project%2Fone');
    expect(headers.get('X-CSRF-Token')).toBe('csrf-test-token');
    expect(headers.get('If-Match')).toBe('"v3"');
    expect(init.credentials).toBe('same-origin');
  });

  it('refuses writes before a session CSRF token is available', async () => {
    await expect(request('/admin/v1/auth/logout', 'post')).rejects.toThrow('CSRF token');
  });
});
