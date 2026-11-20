import type {components} from '../api/generated';

type Role = components['schemas']['RbacRole'];
export type ConsoleRoute = {
  path: string;
  aliases?: string[];
  label: string;
  roles: Role[];
  icon: 'overview' | 'catalog' | 'extensions' | 'routes' | 'access' | 'observability' | 'security' | 'developer' | 'operations' | 'agents';
  enabled: boolean;
};

const everyRole: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER', 'FINOPS_VIEWER', 'DEVELOPER'];
const traceRoles: Role[] = everyRole.filter(role => role !== 'FINOPS_VIEWER');
const financeRoles: Role[] = [...everyRole];
const securityRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const operationsRoles: Role[] = ['OPERATOR'];

export const navigationGroups: {title: string; routes: ConsoleRoute[]}[] = [
  {title: 'Overview & Observability', routes: [
    {path: '/overview', label: 'Overview', roles: everyRole, icon: 'overview', enabled: true},
    {path: '/observability/traces', label: 'Request traces', roles: traceRoles, icon: 'observability', enabled: true},
    {path: '/finops/budgets', aliases: ['/finops/chargeback'], label: 'FinOps budgets & chargeback', roles: financeRoles, icon: 'observability', enabled: true},
  ]},
  {title: 'Model Gateway & Routing', routes: [
    {path: '/providers/adapters', aliases: ['/providers/connections', '/models'], label: 'Providers, Connections & Models', roles: everyRole, icon: 'catalog', enabled: true},
    {path: '/routes', aliases: ['/routes/explain', '/policies'], label: 'Routes & Policies', roles: everyRole, icon: 'routes', enabled: true},
  ]},
  {title: 'Ecosystem & Agents', routes: [
    {path: '/extensions/marketplace', aliases: ['/extensions/installed', '/extensions/lockfile'], label: 'Extension Marketplace & Installed Plugins', roles: everyRole, icon: 'extensions', enabled: true},
    {path: '/mcp/servers', aliases: ['/mcp/tools'], label: 'MCP Hub', roles: everyRole, icon: 'agents', enabled: true},
  ]},
  {title: 'Access & Security', routes: [
    {path: '/access/virtual-keys', aliases: ['/access/members', '/access/roles'], label: 'API Keys, Users & Teams', roles: everyRole, icon: 'access', enabled: true},
    {path: '/security/approvals', aliases: ['/security/audit-log', '/security/kill-switches'], label: 'Security, Approvals & Kill-Switches', roles: securityRoles, icon: 'security', enabled: true},
  ]},
  {title: 'Developer Tools & System', routes: [
    {path: '/devtools/playground', aliases: ['/devtools/api-explorer', '/devtools/sdk-snippets', '/devtools/diagnostics'], label: 'Developer Tools & Playground', roles: ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'DEVELOPER'], icon: 'developer', enabled: true},
    {path: '/ops/health', aliases: ['/ops/backups', '/ops/upgrades', '/settings/residency-retention'], label: 'Operations & Settings', roles: operationsRoles, icon: 'operations', enabled: true},
  ]},
];

export const setupRoutes: ConsoleRoute[] = [
  {path: '/setup', label: 'Setup', roles: ['OPERATOR'], icon: 'overview', enabled: true},
  {path: '/login', label: 'Sign in', roles: [], icon: 'overview', enabled: true},
];

export function routeForPath(path: string): ConsoleRoute | undefined {
  return [...setupRoutes, ...navigationGroups.flatMap(group => group.routes)]
    .find(route => [route.path, ...(route.aliases ?? [])].some(candidate => path === candidate || path.startsWith(`${candidate}/`)));
}

export function canAccessRoute(route: ConsoleRoute, roles: Role[]) {
  if (route.path === '/login') return true;
  return route.roles.some(role => roles.includes(role));
}
