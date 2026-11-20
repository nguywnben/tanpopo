import {lazy, Suspense} from 'react';
import type {components} from '../api/generated';
import {Banner} from '../ui/primitives';
import type {ConsoleRoute, Role} from './routes';

type Project = components['schemas']['Project'];
type Environment = components['schemas']['Environment'];
type Session = components['schemas']['AdminSessionResponse'];

const OverviewPage = lazy(() => import('../pages/overview/OverviewPage').then(mod => ({default: mod.OverviewPage})));
const TracesPage = lazy(() => import('../pages/traces/TracesPage').then(mod => ({default: mod.TracesPage})));
const FinOpsPage = lazy(() => import('../pages/finops/FinOpsPage').then(mod => ({default: mod.FinOpsPage})));
const ProvidersPage = lazy(() => import('../pages/providers/ProvidersPage').then(mod => ({default: mod.ProvidersPage})));
const ModelsPage = lazy(() => import('../pages/models/ModelsPage').then(mod => ({default: mod.ModelsPage})));
const RoutingPage = lazy(() => import('../pages/routes/RoutingPage').then(mod => ({default: mod.RoutingPage})));
const ExtensionsPage = lazy(() => import('../pages/extensions/ExtensionsPage').then(mod => ({default: mod.ExtensionsPage})));
const McpHubPage = lazy(() => import('../pages/mcp/McpHubPage').then(mod => ({default: mod.McpHubPage})));
const PlaygroundPage = lazy(() => import('../pages/playground/PlaygroundPage').then(mod => ({default: mod.PlaygroundPage})));
const AccessPage = lazy(() => import('../pages/access/AccessPage').then(mod => ({default: mod.AccessPage})));
const SecurityPage = lazy(() => import('../pages/security/SecurityPage').then(mod => ({default: mod.SecurityPage})));
const OperationsPage = lazy(() => import('../pages/operations/OperationsPage').then(mod => ({default: mod.OperationsPage})));

export function WorkspaceRouter({
  route,
  pathname,
  roles,
  projectId,
  environmentId,
  session,
}: {
  route: ConsoleRoute;
  pathname: string;
  roles: Role[];
  projects: Project[];
  environments: Environment[];
  projectId: string;
  environmentId: string;
  session: Session | null;
}) {
  const allowed = route.roles.some(role => roles.includes(role));
  if (!allowed) {
    return (
      <Banner tone="danger" title="Access restricted">
        Your assigned roles ({roles.join(', ') || 'None'}) do not permit access to {route.label}.
      </Banner>
    );
  }

  return (
    <Suspense fallback={<p className="tp-muted" role="status">Loading {route.label.toLowerCase()}…</p>}>
      {route.path === '/overview' ? <OverviewPage /> : null}
      {route.path === '/observability/traces' ? <TracesPage environmentId={environmentId} roles={roles} /> : null}
      {route.path === '/finops/budgets' ? <FinOpsPage pathname={pathname} projectId={projectId} roles={roles} /> : null}
      {route.path === '/providers/connections' ? <ProvidersPage pathname={pathname} projectId={projectId} roles={roles} /> : null}
      {route.path === '/models' ? <ModelsPage environmentId={environmentId} roles={roles} /> : null}
      {route.path === '/routes' ? <RoutingPage pathname={pathname} projectId={projectId} environmentId={environmentId} roles={roles} /> : null}
      {route.path === '/extensions/installed' ? <ExtensionsPage pathname={pathname} roles={roles} /> : null}
      {route.path === '/mcp/servers' ? <McpHubPage projectId={projectId} environmentId={environmentId} roles={roles} /> : null}
      {route.path === '/devtools/playground' ? <PlaygroundPage pathname={pathname} projectId={projectId} environmentId={environmentId} roles={roles} /> : null}
      {route.path === '/access/virtual-keys' ? (
        <AccessPage
          pathname={pathname}
          projectId={projectId}
          environmentId={environmentId}
          currentPrincipalId={session?.principal_id ?? ''}
          roles={roles}
        />
      ) : null}
      {route.path === '/security/approvals' ? <SecurityPage pathname={pathname} roles={roles} /> : null}
      {route.path === '/ops/health' ? <OperationsPage pathname={pathname} projectId={projectId} roles={roles} /> : null}
    </Suspense>
  );
}
