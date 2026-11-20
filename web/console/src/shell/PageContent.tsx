import {lazy, Suspense} from 'react';
import {Heading} from '@astryxdesign/core/Heading';
import {Text} from '@astryxdesign/core/Text';
import {VStack} from '@astryxdesign/core/Stack';
import type {components} from '../api/generated';
import type {ConsoleRoute} from './routes';

type Role = components['schemas']['RbacRole'];
type Project = components['schemas']['Project'];
type Environment = components['schemas']['Environment'];
type Session = components['schemas']['AdminSessionResponse'];
const OverviewPage = lazy(() => import('../pages/overview/OverviewPage').then(module => ({default: module.OverviewPage})));
const AdaptersPage = lazy(() => import('../pages/providers/AdaptersPage').then(module => ({default: module.AdaptersPage})));
const ConnectionsPage = lazy(() => import('../pages/providers/ConnectionsPage').then(module => ({default: module.ConnectionsPage})));
const ModelsCatalogPage = lazy(() => import('../pages/models/ModelsCatalogPage').then(module => ({default: module.ModelsCatalogPage})));
const ExtensionCatalogPage = lazy(() => import('../pages/extensions/ExtensionCatalogPage').then(module => ({default: module.ExtensionCatalogPage})));
const InstalledExtensionDetailPage = lazy(() => import('../pages/extensions/InstalledExtensionDetailPage').then(module => ({default: module.InstalledExtensionDetailPage})));
const RoutesPage = lazy(() => import('../pages/routes/RoutesPage').then(module => ({default: module.RoutesPage})));
const PoliciesPage = lazy(() => import('../pages/policies/PoliciesPage').then(module => ({default: module.PoliciesPage})));
const AccessAndKeysPage = lazy(() => import('../pages/access/AccessAndKeysPage').then(module => ({default: module.AccessAndKeysPage})));
const TracesExplorerPage = lazy(() => import('../pages/traces/TracesExplorerPage').then(module => ({default: module.TracesExplorerPage})));
const FinOpsBudgetsPage = lazy(() => import('../pages/finops/FinOpsBudgetsPage').then(module => ({default: module.FinOpsBudgetsPage})));
const SecurityAuditAndKillSwitchPage = lazy(() => import('../pages/security/SecurityAuditAndKillSwitchPage').then(module => ({default: module.SecurityAuditAndKillSwitchPage})));
const OperationsSettingsPage = lazy(() => import('../pages/operations/OperationsSettingsPage').then(module => ({default: module.OperationsSettingsPage})));
const McpHubPage = lazy(() => import('../pages/mcp/McpHubPage').then(module => ({default: module.McpHubPage})));
const PlaygroundPage = lazy(() => import('../pages/playground/PlaygroundPage').then(module => ({default: module.PlaygroundPage})));

export function PageContent({route, pathname, roles, loading, loadError, offline, projects, environments, projectId, environmentId, session}: {
  route: ConsoleRoute; pathname: string; roles: Role[]; loading: boolean; loadError: boolean; offline: boolean;
  projects: Project[]; environments: Environment[]; projectId: string; environmentId: string; session: Session | null;
}) {
  const allowed = route.roles.some(role => roles.includes(role));
  if (loading) return <Text role="status">Loading console context…</Text>;
  if (!allowed) return <Text role="alert">Your account does not have access to this section.</Text>;
  if (loadError) return <Text role="status">{offline ? 'The console is offline. Local assets remain available.' : 'Console context could not be loaded.'}</Text>;
  if (route.path === '/overview') return <Suspense fallback={<Text role="status">Loading overview…</Text>}><OverviewPage /></Suspense>;
  if (pathname.startsWith('/providers/')) return <Suspense fallback={<Text role="status">Loading provider catalog…</Text>}>
    {pathname.startsWith('/providers/connections') ? <ConnectionsPage roles={roles} projectId={projectId} /> : <AdaptersPage />}
  </Suspense>;
  if (pathname.startsWith('/models')) return <Suspense fallback={<Text role="status">Loading model catalog…</Text>}><ModelsCatalogPage roles={roles} environmentId={environmentId} /></Suspense>;
  if (pathname.startsWith('/extensions/')) return <Suspense fallback={<Text role="status">Loading extensions…</Text>}>
    {pathname.startsWith('/extensions/installed') || pathname.startsWith('/extensions/lockfile')
      ? <InstalledExtensionDetailPage roles={roles} />
      : <ExtensionCatalogPage roles={roles} />}
  </Suspense>;
  if (pathname.startsWith('/routes')) return <Suspense fallback={<Text role="status">Loading routes…</Text>}><RoutesPage projectId={projectId} environmentId={environmentId} roles={roles} /></Suspense>;
  if (pathname.startsWith('/policies')) return <Suspense fallback={<Text role="status">Loading policies…</Text>}><PoliciesPage projectId={projectId} environmentId={environmentId} roles={roles} /></Suspense>;
  if (pathname.startsWith('/access/')) return <Suspense fallback={<Text role="status">Loading access records…</Text>}><AccessAndKeysPage projectId={projectId} environmentId={environmentId} currentPrincipalId={session?.principal_id ?? ''} actorRoles={roles} /></Suspense>;
  if (pathname.startsWith('/observability/traces')) return <Suspense fallback={<Text role="status">Loading request traces…</Text>}><TracesExplorerPage environmentId={environmentId} roles={roles} /></Suspense>;
  if (pathname.startsWith('/finops/')) return <Suspense fallback={<Text role="status">Loading FinOps reports…</Text>}><FinOpsBudgetsPage projectId={projectId} roles={roles} /></Suspense>;
  if (pathname.startsWith('/security/')) return <Suspense fallback={<Text role="status">Loading security center…</Text>}><SecurityAuditAndKillSwitchPage roles={roles} /></Suspense>;
  if (pathname.startsWith('/mcp/')) return <Suspense fallback={<Text role="status">Loading MCP Hub…</Text>}><McpHubPage projectId={projectId} environmentId={environmentId} roles={roles} /></Suspense>;
  if (pathname.startsWith('/devtools/')) return <Suspense fallback={<Text role="status">Loading Playground…</Text>}><PlaygroundPage projectId={projectId} environmentId={environmentId} roles={roles} /></Suspense>;
  if (pathname.startsWith('/ops/') || pathname.startsWith('/settings/')) return <Suspense fallback={<Text role="status">Loading operations…</Text>}><OperationsSettingsPage roles={roles} /></Suspense>;
  if (projects.length === 0) return <VStack gap={4}><Heading level={1}>{route.label}</Heading><Text role="status">No projects are available for this account.</Text></VStack>;
  const activeProject = projects.find(project => project.id === projectId);
  const activeEnvironment = environments.find(environment => environment.id === environmentId);
  return <VStack gap={4}><Heading level={1}>{route.label}</Heading><Text>
    {activeProject?.name ?? 'Project'}{activeEnvironment ? ` / ${activeEnvironment.name}` : ''}{session ? ` · ${session.roles.join(', ')}` : ''}
  </Text></VStack>;
}
