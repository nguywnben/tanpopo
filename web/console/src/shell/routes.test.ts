import {describe, expect, it} from 'vitest';
import {canAccessRoute, navigationGroups, routeForPath, setupRoutes} from './routes';

describe('console v2 information architecture & URL compatibility', () => {
  it('resolves setup, sign-in, and every canonical & legacy route path', () => {
    const allRoutes = [...setupRoutes, ...navigationGroups.flatMap(group => group.routes)];
    expect(allRoutes.map(route => routeForPath(route.path)?.path)).toEqual(allRoutes.map(route => route.path));

    expect(routeForPath('/')?.path).toBe('/overview');
    expect(routeForPath('/providers/adapters')?.path).toBe('/providers/connections');
    expect(routeForPath('/catalog/providers')?.path).toBe('/providers/connections');
    expect(routeForPath('/routes/explain')?.path).toBe('/routes');
    expect(routeForPath('/policies')?.path).toBe('/routes');
    expect(routeForPath('/extensions/marketplace')?.path).toBe('/extensions/installed');
    expect(routeForPath('/extensions/lockfile')?.path).toBe('/extensions/installed');
    expect(routeForPath('/mcp/tools')?.path).toBe('/mcp/servers');
    expect(routeForPath('/access/members')?.path).toBe('/access/virtual-keys');
    expect(routeForPath('/access/roles')?.path).toBe('/access/virtual-keys');
    expect(routeForPath('/security/audit-log')?.path).toBe('/security/approvals');
    expect(routeForPath('/security/kill-switches')?.path).toBe('/security/approvals');
    expect(routeForPath('/devtools/sdk-snippets')?.path).toBe('/devtools/playground');
    expect(routeForPath('/finops/chargeback')?.path).toBe('/finops/budgets');
    expect(routeForPath('/ops/backups')?.path).toBe('/ops/health');
    expect(routeForPath('/ops/upgrades')?.path).toBe('/ops/health');
    expect(routeForPath('/settings/residency-retention')?.path).toBe('/ops/health');
  });

  it('exposes the Unit 38 MCP Hub route while keeping A2A out of this release wave', () => {
    const mcp = navigationGroups.flatMap(group => group.routes).find(route => route.path === '/mcp/servers');
    expect(mcp?.enabled).toBe(true);
    expect(mcp?.aliases).not.toContain('/a2a/agents');
  });

  it('uses only the six canonical API roles', () => {
    const roles = new Set(navigationGroups.flatMap(group => group.routes).flatMap(route => route.roles));
    expect([...roles].sort()).toEqual(['DEVELOPER', 'FINOPS_VIEWER', 'OPERATOR', 'ORG_OWNER', 'SECURITY_REVIEWER', 'TEAM_ADMIN']);
  });

  it('enforces role boundaries on operations, security, traces, and finops', () => {
    const operations = routeForPath('/ops/health');
    const security = routeForPath('/security/audit-log');
    const traces = routeForPath('/observability/traces');
    const finops = routeForPath('/finops/budgets');
    const playground = routeForPath('/devtools/playground');

    expect(operations && canAccessRoute(operations, ['OPERATOR'])).toBe(true);
    expect(operations && canAccessRoute(operations, ['DEVELOPER'])).toBe(false);
    expect(security && canAccessRoute(security, ['SECURITY_REVIEWER'])).toBe(true);
    expect(security && canAccessRoute(security, ['FINOPS_VIEWER'])).toBe(false);
    expect(traces && canAccessRoute(traces, ['FINOPS_VIEWER'])).toBe(false);
    expect(finops && canAccessRoute(finops, ['FINOPS_VIEWER'])).toBe(true);
    expect(playground && canAccessRoute(playground, ['DEVELOPER'])).toBe(true);
    expect(playground && canAccessRoute(playground, ['FINOPS_VIEWER'])).toBe(false);
  });

  it('keeps sign-in reachable without an authenticated role', () => {
    const login = routeForPath('/login');
    expect(login && canAccessRoute(login, [])).toBe(true);
  });
});
