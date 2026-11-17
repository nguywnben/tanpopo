import {useEffect, useMemo, useState} from 'react';
import {AppShell as AstryxAppShell} from '@astryxdesign/core/AppShell';
import {Banner} from '@astryxdesign/core/Banner';
import {Heading} from '@astryxdesign/core/Heading';
import {Icon} from '@astryxdesign/core/Icon';
import type {IconName} from '@astryxdesign/core/Icon';
import * as stylex from '@stylexjs/stylex';
import {SideNav, SideNavHeading, SideNavItem, SideNavSection} from '@astryxdesign/core/SideNav';
import {Text} from '@astryxdesign/core/Text';
import {TopNav, TopNavHeading} from '@astryxdesign/core/TopNav';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {getAdminSession, listProjects, listEnvironments} from '../api/client';
import type {components} from '../api/generated';
import {canAccessRoute, routeForPath, navigationGroups, type ConsoleRoute} from './routes';
import {styles} from './shell.stylex';

type Session = components['schemas']['AdminSessionResponse'];
type Project = components['schemas']['Project'];
type Environment = components['schemas']['Environment'];

const navIcons: Record<ConsoleRoute['icon'], IconName> = {
  overview: 'info',
  catalog: 'search',
  extensions: 'upload',
  routes: 'arrowsUpDown',
  access: 'eyeSlash',
  observability: 'clock',
  security: 'warning',
  developer: 'wrench',
  operations: 'calendar',
  agents: 'menu',
};

function currentRoute(pathname: string): ConsoleRoute {
  return routeForPath(pathname) ?? routeForPath('/overview')!;
}

export function ConsoleShell() {
  const [session, setSession] = useState<Session | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [projectId, setProjectId] = useState('');
  const [environmentId, setEnvironmentId] = useState('');
  const [loading, setLoading] = useState(true);
  const [offline, setOffline] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [pathname, setPathname] = useState(window.location.pathname);
  const route = useMemo(() => currentRoute(pathname), [pathname]);

  useEffect(() => {
    const updatePath = () => setPathname(window.location.pathname);
    window.addEventListener('popstate', updatePath);
    return () => window.removeEventListener('popstate', updatePath);
  }, []);

  useEffect(() => {
    let active = true;
    async function loadContext() {
      setLoading(true);
      setLoadError(false);
      try {
        const activeSession = await getAdminSession();
        const projectPage = await listProjects();
        const projectList = projectPage.data;
        const selectedProject = projectList[0];
        const environmentList = selectedProject ? (await listEnvironments(selectedProject.id)).data : [];
        if (!active) return;
        setSession(activeSession);
        setProjects(projectList);
        setEnvironments(environmentList);
        setProjectId(selectedProject?.id ?? '');
        setEnvironmentId(environmentList[0]?.id ?? '');
        setOffline(false);
      } catch (error) {
        if (!active) return;
        setOffline(error instanceof TypeError || !navigator.onLine);
        setLoadError(true);
      } finally {
        if (active) setLoading(false);
      }
    }
    void loadContext();
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!projectId) return;
    let active = true;
    listEnvironments(projectId).then(({data}) => {
      if (active) {
        setEnvironments(data);
        setEnvironmentId(previous => data.some(item => item.id === previous) ? previous : data[0]?.id ?? '');
      }
    }).catch(() => {
      if (active) setLoadError(true);
    });
    return () => { active = false; };
  }, [projectId]);

  const roles = session?.roles ?? [];
  const visibleGroups = navigationGroups.map(group => ({
    ...group,
    routes: group.routes.filter(item => item.enabled && canAccessRoute(item, roles)),
  })).filter(group => group.routes.length > 0);
  const activeProject = projects.find(project => project.id === projectId);
  const activeEnvironment = environments.find(environment => environment.id === environmentId);

  return (
    <AstryxAppShell
      height="fill"
      variant="section"
      contentPadding={4}
      banner={offline ? <Banner status="info" title="Running in Local Network / Offline Mode" collapsible={false} /> : undefined}
      topNav={
        <TopNav
          label="System context and global command search"
          heading={<TopNavHeading heading="Tanpopo" headingHref="/overview" />}
          startContent={
            <HStack gap={2} wrap="wrap" xstyle={styles.contextControls}>
              <label htmlFor="project-context">Project</label>
              <select {...stylex.props(styles.contextSelect)} id="project-context" value={projectId} disabled={loading || projects.length === 0} onChange={event => setProjectId(event.target.value)}>
                <option value="">Select project</option>
                {projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}
              </select>
              <label htmlFor="environment-context">Environment</label>
              <select {...stylex.props(styles.contextSelect)} id="environment-context" value={environmentId} disabled={loading || environments.length === 0} onChange={event => setEnvironmentId(event.target.value)}>
                <option value="">Select environment</option>
                {environments.map(environment => <option key={environment.id} value={environment.id}>{environment.name}</option>)}
              </select>
            </HStack>
          }
          endContent={<Text aria-label="Signed-in account">{session?.email ?? 'Admin session'}</Text>}
        />
      }
      sideNav={
        <SideNav
          aria-label="Tanpopo subsystem navigation"
          collapsible
          resizable={{defaultWidth: 256, minWidth: 220, maxWidth: 420}}
          header={<SideNavHeading heading="Console" headingHref="/overview" />}
        >
          {visibleGroups.map(group => (
            <SideNavSection key={group.title} title={group.title}>
              {group.routes.map(item => {
                const ItemIcon = navIcons[item.icon];
                return <SideNavItem key={item.path} label={item.label} href={item.path} icon={<Icon icon={ItemIcon} size="sm" />} isSelected={route.path === item.path} />;
              })}
            </SideNavSection>
          ))}
        </SideNav>
      }
    >
      <VStack gap={4}>
        <Heading level={1}>{route.label}</Heading>
        {loading ? <Text role="status">Loading console context…</Text> : null}
        {!loading && loadError ? <Text role="status">{offline ? 'The console is offline. Local assets remain available.' : 'Console context could not be loaded.'}</Text> : null}
        {!loading && !loadError && projects.length === 0 ? <Text role="status">No projects are available for this account.</Text> : null}
        {!loading && !loadError && projects.length > 0 ? (
          <Text>
            {activeProject?.name ?? 'Project'}{activeEnvironment ? ` / ${activeEnvironment.name}` : ''}
            {session ? ` · ${session.roles.join(', ')}` : ''}
          </Text>
        ) : null}
        {!canAccessRoute(route, roles) && !loading ? <Text role="alert">Your account does not have access to this section.</Text> : null}
      </VStack>
    </AstryxAppShell>
  );
}
