import {describe, expect, it} from 'vitest';
import {canAccessRoute, navigationGroups, routeForPath, setupRoutes} from './routes';

describe('console information architecture', () => {
  it('resolves setup, sign-in, and every specified shell domain route', () => {
    const allRoutes = [...setupRoutes, ...navigationGroups.flatMap(group => group.routes)];
    expect(allRoutes.map(route => routeForPath(route.path)?.path)).toEqual(allRoutes.map(route => route.path));
    expect(routeForPath('/routes/explain')?.path).toBe('/routes');
    expect(routeForPath('/settings/residency-retention')?.path).toBe('/ops/health');
    expect(routeForPath('/providers/connections')?.path).toBe('/providers/adapters');
    expect(routeForPath('/extensions/lockfile')?.path).toBe('/extensions/marketplace');
  });

  it('exposes the Unit 38 MCP Hub route while keeping A2A routes out of this release wave', () => {
    const roadmap = navigationGroups.flatMap(group => group.routes).find(route => route.path === '/mcp/servers');
    expect(roadmap?.enabled).toBe(true);
    expect(roadmap?.aliases).not.toContain('/a2a/agents');
  });

  it('uses only the six canonical API roles', () => {
    const roles = new Set(navigationGroups.flatMap(group => group.routes).flatMap(route => route.roles));
    expect([...roles].sort()).toEqual(['DEVELOPER', 'FINOPS_VIEWER', 'OPERATOR', 'ORG_OWNER', 'SECURITY_REVIEWER', 'TEAM_ADMIN']);
  });

  it('limits operational navigation to roles authorized by the information architecture', () => {
    const operations = routeForPath('/ops/health');
    const security = routeForPath('/security/audit-log');
    const traces = routeForPath('/observability/traces');
    const finops = routeForPath('/finops/budgets');
    expect(operations && canAccessRoute(operations, ['OPERATOR'])).toBe(true);
    expect(operations && canAccessRoute(operations, ['DEVELOPER'])).toBe(false);
    expect(security && canAccessRoute(security, ['SECURITY_REVIEWER'])).toBe(true);
    expect(security && canAccessRoute(security, ['FINOPS_VIEWER'])).toBe(false);
    expect(traces && canAccessRoute(traces, ['FINOPS_VIEWER'])).toBe(false);
    expect(finops && canAccessRoute(finops, ['FINOPS_VIEWER'])).toBe(true);
  });

  it('keeps sign-in reachable without an authenticated role', () => {
    const login = routeForPath('/login');
    expect(login && canAccessRoute(login, [])).toBe(true);
  });
});
