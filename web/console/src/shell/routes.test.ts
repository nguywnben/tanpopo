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

  it('keeps roadmap MCP and A2A navigation hidden until its release wave', () => {
    const roadmap = navigationGroups.flatMap(group => group.routes).find(route => route.path === '/mcp/servers');
    expect(roadmap?.enabled).toBe(false);
  });

  it('uses only the six canonical API roles', () => {
    const roles = new Set(navigationGroups.flatMap(group => group.routes).flatMap(route => route.roles));
    expect([...roles].sort()).toEqual(['DEVELOPER', 'FINOPS_VIEWER', 'OPERATOR', 'ORG_OWNER', 'SECURITY_REVIEWER', 'TEAM_ADMIN']);
  });

  it('limits operational navigation to roles authorized by the information architecture', () => {
    const operations = routeForPath('/ops/health');
    const security = routeForPath('/security/audit-log');
    expect(operations && canAccessRoute(operations, ['OPERATOR'])).toBe(true);
    expect(operations && canAccessRoute(operations, ['DEVELOPER'])).toBe(false);
    expect(security && canAccessRoute(security, ['SECURITY_REVIEWER'])).toBe(true);
    expect(security && canAccessRoute(security, ['FINOPS_VIEWER'])).toBe(false);
  });
});
