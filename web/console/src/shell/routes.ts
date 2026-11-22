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
  labelKey: string;
  shortLabel?: string;
  roles: Role[];
  icon: NavIconKey;
  enabled: boolean;
};

export type NavigationGroup = {
  title: string;
  titleKey: string;
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
    titleKey: 'nav.group.monitor',
    routes: [
      {path: '/overview', label: 'Overview', labelKey: 'nav.route.overview', roles: everyRole, icon: 'overview', enabled: true},
      {path: '/observability/traces', label: 'Traces', labelKey: 'nav.route.traces', roles: traceRoles, icon: 'traces', enabled: true},
      {path: '/finops/budgets', aliases: ['/finops/chargeback'], label: 'FinOps', labelKey: 'nav.route.finops', roles: everyRole, icon: 'finops', enabled: true},
    ],
  },
  {
    title: 'Gateway',
    titleKey: 'nav.group.gateway',
    routes: [
      {
        path: '/providers/connections',
        aliases: ['/providers/adapters', '/catalog/providers'],
        label: 'Providers',
        labelKey: 'nav.route.providers',
        roles: everyRole,
        icon: 'providers',
        enabled: true,
      },
      {path: '/models', label: 'Models', labelKey: 'nav.route.models', roles: everyRole, icon: 'models', enabled: true},
      {
        path: '/routes',
        aliases: ['/routes/explain', '/policies'],
        label: 'Routes & Policies',
        labelKey: 'nav.route.routes',
        roles: everyRole,
        icon: 'routes',
        enabled: true,
      },
    ],
  },
  {
    title: 'Ecosystem',
    titleKey: 'nav.group.ecosystem',
    routes: [
      {
        path: '/extensions/installed',
        aliases: ['/extensions/marketplace', '/extensions/lockfile'],
        label: 'Extensions',
        labelKey: 'nav.route.extensions',
        roles: everyRole,
        icon: 'extensions',
        enabled: true,
      },
      {path: '/mcp/servers', aliases: ['/mcp/tools', '/a2a/agents'], label: 'MCP Hub', labelKey: 'nav.route.mcp', roles: everyRole, icon: 'mcp', enabled: true},
      {
        path: '/devtools/playground',
        aliases: ['/devtools/api-explorer', '/devtools/sdk-snippets', '/devtools/diagnostics'],
        label: 'Playground',
        labelKey: 'nav.route.playground',
        roles: developerRoles,
        icon: 'playground',
        enabled: true,
      },
    ],
  },
  {
    title: 'Governance',
    titleKey: 'nav.group.governance',
    routes: [
      {
        path: '/access/virtual-keys',
        aliases: ['/access/members', '/access/roles'],
        label: 'Access & Keys',
        labelKey: 'nav.route.access',
        roles: everyRole,
        icon: 'access',
        enabled: true,
      },
      {
        path: '/security/approvals',
        aliases: ['/security/audit-log', '/security/kill-switches'],
        label: 'Security',
        labelKey: 'nav.route.security',
        roles: securityRoles,
        icon: 'security',
        enabled: true,
      },
      {
        path: '/ops/health',
        aliases: ['/ops/backups', '/ops/upgrades', '/ops/organizations', '/settings/residency-retention'],
        label: 'Operations',
        labelKey: 'nav.route.operations',
        roles: operationsRoles,
        icon: 'operations',
        enabled: true,
      },
    ],
  },
];

export const setupRoutes: ConsoleRoute[] = [
  {path: '/setup', label: 'First-Run Setup', labelKey: 'nav.route.setup', roles: ['OPERATOR'], icon: 'overview', enabled: true},
  {path: '/login', label: 'Sign In', labelKey: 'nav.route.login', roles: [], icon: 'overview', enabled: true},
];

export type QuickCommand = {
  id: string;
  label: string;
  labelKey: string;
  section: string;
  sectionKey: string;
  path: string;
  roles: Role[];
};

export const quickCommands: QuickCommand[] = [
  {id: 'overview', label: 'Overview & System Posture', labelKey: 'cmd.overview', section: 'Monitor', sectionKey: 'nav.group.monitor', path: '/overview', roles: everyRole},
  {id: 'traces', label: 'Request Traces & Waterfall', labelKey: 'cmd.traces', section: 'Monitor', sectionKey: 'nav.group.monitor', path: '/observability/traces', roles: traceRoles},
  {id: 'budgets', label: 'FinOps Budgets & Active Holds', labelKey: 'cmd.budgets', section: 'Monitor', sectionKey: 'nav.group.monitor', path: '/finops/budgets', roles: everyRole},
  {id: 'chargeback', label: 'Chargeback Summary & Export', labelKey: 'cmd.chargeback', section: 'Monitor', sectionKey: 'nav.group.monitor', path: '/finops/chargeback', roles: everyRole},
  {id: 'connections', label: 'Provider Connections & Vault Secrets', labelKey: 'cmd.connections', section: 'Gateway', sectionKey: 'nav.group.gateway', path: '/providers/connections', roles: everyRole},
  {id: 'adapters', label: 'Provider Protocol Adapters', labelKey: 'cmd.adapters', section: 'Gateway', sectionKey: 'nav.group.gateway', path: '/providers/adapters', roles: everyRole},
  {id: 'models', label: 'Models & Deployments', labelKey: 'cmd.models', section: 'Gateway', sectionKey: 'nav.group.gateway', path: '/models', roles: everyRole},
  {id: 'routes', label: 'Routing Rules', labelKey: 'cmd.routes', section: 'Gateway', sectionKey: 'nav.group.gateway', path: '/routes', roles: everyRole},
  {id: 'route-explain', label: 'Dry-Run Route Explain', labelKey: 'cmd.routeExplain', section: 'Gateway', sectionKey: 'nav.group.gateway', path: '/routes/explain', roles: everyRole},
  {id: 'policies', label: 'Hierarchical Policies', labelKey: 'cmd.policies', section: 'Gateway', sectionKey: 'nav.group.gateway', path: '/policies', roles: everyRole},
  {id: 'ext-installed', label: 'Installed Extensions', labelKey: 'cmd.extInstalled', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/extensions/installed', roles: everyRole},
  {id: 'ext-marketplace', label: 'Extension Marketplace', labelKey: 'cmd.extMarketplace', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/extensions/marketplace', roles: everyRole},
  {id: 'ext-lockfile', label: 'Extension Lockfile (tanpopo.lock)', labelKey: 'cmd.extLockfile', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/extensions/lockfile', roles: everyRole},
  {id: 'mcp', label: 'MCP Servers & Tool Grants', labelKey: 'cmd.mcp', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/mcp/servers', roles: everyRole},
  {id: 'a2a', label: 'A2A Agents & Active Tasks', labelKey: 'cmd.a2a', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/a2a/agents', roles: everyRole},
  {id: 'playground', label: 'Inference Playground', labelKey: 'cmd.playground', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/devtools/playground', roles: developerRoles},
  {id: 'sdk-snippets', label: 'SDK Snippets & Quickstart', labelKey: 'cmd.sdkSnippets', section: 'Ecosystem', sectionKey: 'nav.group.ecosystem', path: '/devtools/sdk-snippets', roles: developerRoles},
  {id: 'vkeys', label: 'Virtual Keys', labelKey: 'cmd.vkeys', section: 'Governance', sectionKey: 'nav.group.governance', path: '/access/virtual-keys', roles: everyRole},
  {id: 'members', label: 'Members & RBAC Roles', labelKey: 'cmd.members', section: 'Governance', sectionKey: 'nav.group.governance', path: '/access/members', roles: everyRole},
  {id: 'approvals', label: 'MCP Approval Queue', labelKey: 'cmd.approvals', section: 'Governance', sectionKey: 'nav.group.governance', path: '/security/approvals', roles: securityRoles},
  {id: 'kill-switches', label: 'Emergency Kill Switches', labelKey: 'cmd.killSwitches', section: 'Governance', sectionKey: 'nav.group.governance', path: '/security/kill-switches', roles: securityRoles},
  {id: 'audit', label: 'Immutable JCS Audit Log', labelKey: 'cmd.audit', section: 'Governance', sectionKey: 'nav.group.governance', path: '/security/audit-log', roles: securityRoles},
  {id: 'ops-health', label: 'Cluster Health & Readiness', labelKey: 'cmd.opsHealth', section: 'Governance', sectionKey: 'nav.group.governance', path: '/ops/health', roles: operationsRoles},
  {id: 'ops-backups', label: 'Encrypted Backups', labelKey: 'cmd.opsBackups', section: 'Governance', sectionKey: 'nav.group.governance', path: '/ops/backups', roles: operationsRoles},
  {id: 'ops-upgrades', label: 'Upgrade Preflight', labelKey: 'cmd.opsUpgrades', section: 'Governance', sectionKey: 'nav.group.governance', path: '/ops/upgrades', roles: operationsRoles},
  {id: 'ops-orgs', label: 'Organizations Governance', labelKey: 'cmd.opsOrgs', section: 'Governance', sectionKey: 'nav.group.governance', path: '/ops/organizations', roles: operationsRoles},
  {id: 'ops-webhooks', label: 'Webhooks & Settings', labelKey: 'cmd.opsWebhooks', section: 'Governance', sectionKey: 'nav.group.governance', path: '/settings/residency-retention', roles: operationsRoles},
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
