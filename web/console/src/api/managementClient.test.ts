import {afterEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken} from './client';
import {createMember, createRoute, getEffectivePolicy, listRoutesForEnvironment, revokeVirtualKey, rotateVirtualKey, updateMemberRoles, updatePolicy, updateRoute} from './managementClient';
import type {components} from './generated';

afterEach(() => { clearCsrfToken(); vi.unstubAllGlobals(); });

const route: components['schemas']['RouteResource'] = {id: 'route-1', slug: 'prod-route', env_id: 'env-1', strategy: 'PRIORITY_FAILOVER', max_failover_hops: 3, request_timeout_ms: 60000, targets: [{deployment_id: 'dep-1', priority_order: 1}], version: 4, updated_at: '2026-11-17T00:00:00Z'};
const policy: components['schemas']['PolicyDocument'] = {id: 'policy-1', scope_tier: 'ENVIRONMENT', scope_id: 'env-1', allowed_data_regions: ['EU'], min_plugin_trust: 'VERIFIED_ONLY', min_capability_assurance: 'TESTED', version: 6};
const key: components['schemas']['VirtualKeySummary'] = {id: 'key-1', name: 'service key', project_id: 'project-1', env_id: 'env-1', key_prefix: 'vk_tp_prod_', last_four: '1234', state: 'CREATED_ACTIVE', version: 7, created_at: '2026-11-17T00:00:00Z'};
const member: components['schemas']['MemberSummary'] = {id: 'member-1', email: 'person@example.test', kind: 'HUMAN', roles: ['DEVELOPER'], version: 3, created_at: '2026-11-17T00:00:00Z'};

describe('typed routing, policy, and access API operations', () => {
  it('sends session CSRF on mutations and exact If-Match ETags for every versioned action', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [route], has_more: false}))
      .mockResolvedValueOnce(json(route))
      .mockResolvedValueOnce(json({data: route, has_more: false}, 201))
      .mockResolvedValueOnce(json({...route, version: 5}))
      .mockResolvedValueOnce(json({id: policy.id, scope_tier: policy.scope_tier, scope_id: policy.scope_id, allowed_data_regions: ['EU'], min_plugin_trust: 'VERIFIED_ONLY', min_capability_assurance: 'TESTED', version: 6}, 200, {'ETag': '"v6"'}))
      .mockResolvedValueOnce(json({...policy, version: 7}, 200, {'ETag': '"v7"'}))
      .mockResolvedValueOnce(json({id: key.id, name: key.name, project_id: key.project_id, env_id: key.env_id, key_prefix: key.key_prefix, last_four: key.last_four, state: key.state, version: 7, created_at: key.created_at, secret_key: 'vk_tp_one_time'}, 200))
      .mockResolvedValueOnce(json({key: {...key, state: 'REVOKED', version: 8}, severed_streams_count: 2}, 200))
      .mockResolvedValueOnce(json({...member, version: 4}, 200));
    vi.stubGlobal('fetch', fetchMock);
    rememberCsrfToken('csrf-unit29');

    await listRoutesForEnvironment('env-1');
    await updateRoute(route, {...route, slug: 'prod-route-v2'});
    await createRoute({...route, slug: 'another-route'});
    await getEffectivePolicy('project-1', 'env-1');
    await updatePolicy(policy, '"v6"', policy);
    await rotateVirtualKey(key, 86400);
    await revokeVirtualKey(key, 'EMERGENCY_ABORT_STREAMS', 'incident response');
    await updateMemberRoles(member, ['DEVELOPER', 'TEAM_ADMIN']);

    const requests = fetchMock.mock.calls.map(call => call[0] as Request);
    expect(requests[1].headers.get('If-Match')).toBe('"v4"');
    expect(requests[4].headers.get('If-Match')).toBe('"v6"');
    expect(requests[5].headers.get('If-Match')).toBe('"v7"');
    expect(requests[6].headers.get('If-Match')).toBe('"v7"');
    expect(requests[7].headers.get('If-Match')).toBe('"v3"');
    for (const index of [1, 2, 4, 5, 6, 7]) expect(requests[index].headers.get('X-CSRF-Token')).toBe('csrf-unit29');
    expect(requests[0].method).toBe('GET');
  });

  it('adds a member through the typed client and never adds If-Match to a create', async () => {
    const fetchMock = vi.fn().mockResolvedValue(json(member, 201));
    vi.stubGlobal('fetch', fetchMock);
    rememberCsrfToken('csrf-create-member');
    await createMember(member.email, member.roles);
    const request = fetchMock.mock.calls[0][0] as Request;
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-create-member');
    expect(request.headers.get('If-Match')).toBeNull();
    expect(await request.clone().json()).toMatchObject({email: member.email, roles: ['DEVELOPER']});
  });

});

function json(value: unknown, status = 200, extraHeaders: Record<string, string> = {}) {
  return new Response(JSON.stringify(value), {status, headers: {'Content-Type': 'application/json', ...extraHeaders}});
}
