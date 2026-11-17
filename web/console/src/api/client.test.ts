import {afterEach, describe, expect, it, vi} from 'vitest';
import {api, clearCsrfToken, getAdminSession, listProjects, rememberCsrfToken} from './client';

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
    await listProjects();

    const sessionRequest = fetchMock.mock.calls[0][0] as Request;
    const projectRequest = fetchMock.mock.calls[1][0] as Request;
    expect(sessionRequest).toMatchObject({credentials: 'same-origin', method: 'GET'});
    const nextHeaders = projectRequest.headers;
    expect(nextHeaders.get('X-CSRF-Token')).toBeNull();
  });

  it('adds CSRF and If-Match to writes without persisting credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, {status: 204}));
    vi.stubGlobal('fetch', fetchMock);
    rememberCsrfToken('csrf-test-token');

    await api.PUT('/admin/v1/organizations/{orgId}/extension-policies', {
      params: {path: {orgId: 'org-1'}, header: {'If-Match': '"v3"'}},
      body: {min_plugin_trust: 'VERIFIED_ONLY', policies: []},
    });

    const request = fetchMock.mock.calls[0][0] as Request;
    const headers = request.headers;
    expect(request.url).toBe('http://localhost:3000/admin/v1/organizations/org-1/extension-policies');
    expect(headers.get('X-CSRF-Token')).toBe('csrf-test-token');
    expect(headers.get('If-Match')).toBe('"v3"');
    expect(request.credentials).toBe('same-origin');
  });

  it('refuses writes before a session CSRF token is available', async () => {
    await expect(api.POST('/admin/v1/auth/logout')).rejects.toThrow('CSRF token');
  });
});
