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

  it('forwards If-Match through the typed client and adds session CSRF', async () => {
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

  it('rejects an ETag-protected operation when the client request omits If-Match', async () => {
    const fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const putWithoutHeaders = api.PUT as unknown as (path: string, options: unknown) => Promise<unknown>;

    await expect(putWithoutHeaders('/admin/v1/organizations/{orgId}/extension-policies', {
      params: {path: {orgId: 'org-1'}},
      body: {min_plugin_trust: 'VERIFIED_ONLY', policies: []},
    })).rejects.toThrow('If-Match ETag');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('does not require or attach CSRF on the OpenAPI pre-auth login and bootstrap operations', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, {status: 204}));
    vi.stubGlobal('fetch', fetchMock);
    rememberCsrfToken('csrf-existing-session');

    await api.POST('/admin/v1/auth/login', {body: {email: 'admin@example.test', password: 'password'}});
    await api.POST('/admin/v1/bootstrap/complete', {body: {
      bootstrap_token: 'b'.repeat(32),
      owner_email: 'owner@example.test',
      owner_password: 'long-enough-password',
      org_name: 'Example Organization',
      project_name: 'Example Project',
      master_kek_backup_acknowledged: true,
    }});

    expect(fetchMock).toHaveBeenCalledTimes(2);
    for (const [input] of fetchMock.mock.calls) {
      expect((input as Request).headers.get('X-CSRF-Token')).toBeNull();
    }
  });

  it('requires CSRF for session mutations and attaches the in-memory session token', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, {status: 204}));
    vi.stubGlobal('fetch', fetchMock);
    await expect(api.POST('/admin/v1/auth/logout')).rejects.toThrow('CSRF token');
    expect(fetchMock).not.toHaveBeenCalled();

    rememberCsrfToken('csrf-session-token');
    await api.POST('/admin/v1/auth/logout');
    expect((fetchMock.mock.calls[0][0] as Request).headers.get('X-CSRF-Token')).toBe('csrf-session-token');
  });
});
