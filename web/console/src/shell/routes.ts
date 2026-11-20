import type {components} from '../api/generated';

export type Role = components['schemas']['RbacRole'];

export type NavIconKey =
  | 'overview'
  | 'traces'
  | 'finops'
  | 'providers'
  | 'models'
  | 'routes'
  | 'extensions'
  | 'mcp'
  | 'playground'
  | 'access'
  | 'security'
  | 'operations';

export type ConsoleRoute = {
  path: string;
  aliases?: string[];
  label: string;
  shortLabel?: string;
  roles: Role[];
  icon: NavIconKey;
  enabled: boolean;
};

export type NavigationGroup = {
  title: string;
  routes: ConsoleRoute[];
};

const everyRole: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER', 'FINOPS_VIEWER', 'DEVELOPER'];
const traceRoles: Role[] = everyRole.filter(role => role !== 'FINOPS_VIEWER');
const securityRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const developerRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'DEVELOPER'];
const operationsRoles: Role[] = ['OPERATOR'];

export const navigationGroups: NavigationGroup[] = [
  {
    title: 'Monitor',
    routes: [
      {path: '/overview', label: 'Overview', roles: everyRole, icon: 'overview', enabled: true},
      {path: '/observability/traces', label: 'Traces', roles: traceRoles, icon: 'traces', enabled: true},
      {path: '/finops/budgets', aliases: ['/finops/chargeback'], label: 'FinOps', roles: everyRole, icon: 'finops', enabled: true},
    ],
  },
  {
    title: 'Gateway',
    routes: [
      {
        path: '/providers/connections',
        aliases: ['/providers/adapters', '/catalog/providers'],
        label: 'Providers',
        roles: everyRole,
        icon: 'providers',
        enabled: true,
      },
      {path: '/models', label: 'Models', roles: everyRole, icon: 'models', enabled: true},
      {
        path: '/routes',
        aliases: ['/routes/explain', '/policies'],
        label: 'Routes & Policies',
        roles: everyRole,
        icon: 'routes',
        enabled: true,
      },
    ],
  },
  {
    title: 'Ecosystem',
    routes: [
      {
        path: '/extensions/installed',
        aliases: ['/extensions/marketplace', '/extensions/lockfile'],
        label: 'Extensions',
        roles: everyRole,
        icon: 'extensions',
        enabled: true,
      },
      {path: '/mcp/servers', aliases: ['/mcp/tools'], label: 'MCP Hub', roles: everyRole, icon: 'mcp', enabled: true},
      {
        path: '/devtools/playground',
        aliases: ['/devtools/api-explorer', '/devtools/sdk-snippets', '/devtools/diagnostics'],
        label: 'Playground',
        roles: developerRoles,
        icon: 'playground',
        enabled: true,
      },
    ],
  },
  {
    title: 'Governance',
    routes: [
      {
        path: '/access/virtual-keys',
        aliases: ['/access/members', '/access/roles'],
        label: 'Access & Keys',
        roles: everyRole,
        icon: 'access',
        enabled: true,
      },
      {
        path: '/security/approvals',
        aliases: ['/security/audit-log', '/security/kill-switches'],
        label: 'Security',
        roles: securityRoles,
        icon: 'security',
        enabled: true,
      },
      {
        path: '/ops/health',
        aliases: ['/ops/backups', '/ops/upgrades', '/settings/residency-retention'],
        label: 'Operations',
        roles: operationsRoles,
        icon: 'operations',
        enabled: true,
      },
    ],
  },
];

export const setupRoutes: ConsoleRoute[] = [
  {path: '/setup', label: 'First-Run Setup', roles: ['OPERATOR'], icon: 'overview', enabled: true},
  {path: '/login', label: 'Sign In', roles: [], icon: 'overview', enabled: true},
];

export type QuickCommand = {
  id: string;
  label: string;
  section: string;
  path: string;
  roles: Role[];
};

export const quickCommands: QuickCommand[] = [
  {id: 'overview', label: 'Overview & System Posture', section: 'Monitor', path: '/overview', roles: everyRole},
  {id: 'traces', label: 'Request Traces & Waterfall', section: 'Monitor', path: '/observability/traces', roles: traceRoles},
  {id: 'budgets', label: 'FinOps Budgets & Active Holds', section: 'Monitor', path: '/finops/budgets', roles: everyRole},
  {id: 'chargeback', label: 'Chargeback Summary & Export', section: 'Monitor', path: '/finops/chargeback', roles: everyRole},
  {id: 'connections', label: 'Provider Connections & Vault Secrets', section: 'Gateway', path: '/providers/connections', roles: everyRole},
  {id: 'adapters', label: 'Provider Protocol Adapters', section: 'Gateway', path: '/providers/adapters', roles: everyRole},
  {id: 'models', label: 'Models & Deployments', section: 'Gateway', path: '/models', roles: everyRole},
  {id: 'routes', label: 'Routing Rules', section: 'Gateway', path: '/routes', roles: everyRole},
  {id: 'route-explain', label: 'Dry-Run Route Explain', section: 'Gateway', path: '/routes/explain', roles: everyRole},
  {id: 'policies', label: 'Hierarchical Policies', section: 'Gateway', path: '/policies', roles: everyRole},
  {id: 'ext-installed', label: 'Installed Extensions', section: 'Ecosystem', path: '/extensions/installed', roles: everyRole},
  {id: 'ext-marketplace', label: 'Extension Marketplace', section: 'Ecosystem', path: '/extensions/marketplace', roles: everyRole},
  {id: 'ext-lockfile', label: 'Extension Lockfile (tanpopo.lock)', section: 'Ecosystem', path: '/extensions/lockfile', roles: everyRole},
  {id: 'mcp', label: 'MCP Servers & Tool Grants', section: 'Ecosystem', path: '/mcp/servers', roles: everyRole},
  {id: 'playground', label: 'Inference Playground', section: 'Ecosystem', path: '/devtools/playground', roles: developerRoles},
  {id: 'sdk-snippets', label: 'SDK Snippets & Quickstart', section: 'Ecosystem', path: '/devtools/sdk-snippets', roles: developerRoles},
  {id: 'vkeys', label: 'Virtual Keys', section: 'Governance', path: '/access/virtual-keys', roles: everyRole},
  {id: 'members', label: 'Members & RBAC Roles', section: 'Governance', path: '/access/members', roles: everyRole},
  {id: 'approvals', label: 'MCP Approval Queue', section: 'Governance', path: '/security/approvals', roles: securityRoles},
  {id: 'kill-switches', label: 'Emergency Kill Switches', section: 'Governance', path: '/security/kill-switches', roles: securityRoles},
  {id: 'audit', label: 'Immutable JCS Audit Log', section: 'Governance', path: '/security/audit-log', roles: securityRoles},
  {id: 'ops-health', label: 'Cluster Health & Readiness', section: 'Governance', path: '/ops/health', roles: operationsRoles},
  {id: 'ops-backups', label: 'Encrypted Backups', section: 'Governance', path: '/ops/backups', roles: operationsRoles},
  {id: 'ops-upgrades', label: 'Upgrade Preflight', section: 'Governance', path: '/ops/upgrades', roles: operationsRoles},
  {id: 'ops-webhooks', label: 'Webhooks & Settings', section: 'Governance', path: '/settings/residency-retention', roles: operationsRoles},
];

export function routeForPath(path: string): ConsoleRoute | undefined {
  const normalized = path === '/' ? '/overview' : path;
  return [...setupRoutes, ...navigationGroups.flatMap(group => group.routes)].find(route =>
    [route.path, ...(route.aliases ?? [])].some(candidate => normalized === candidate || normalized.startsWith(`${candidate}/`)),
  );
}

export function canAccessRoute(route: ConsoleRoute, roles: Role[]): boolean {
  if (route.path === '/login') return true;
  return route.roles.some(role => roles.includes(role));
}
