import {lazy, Suspense, useEffect, useMemo, useState} from 'react';
import {
  Activity,
  Blocks,
  Cpu,
  DollarSign,
  GitBranch,
  KeyRound,
  LayoutDashboard,
  LogOut,
  Menu,
  Moon,
  Network,
  Search,
  Server,
  ShieldAlert,
  Sliders,
  Sun,
  Terminal,
  X,
} from 'lucide-react';
import {CommandPalette} from '@astryxdesign/core/CommandPalette';
import {Kbd} from '@astryxdesign/core/Kbd';
import {Selector} from '@astryxdesign/core/Selector';
import {Skeleton} from '@astryxdesign/core/Skeleton';
import {createStaticSource, type SearchableItem} from '@astryxdesign/core/Typeahead';
import {getAdminSession, listEnvironments, listProjects, logoutAdmin} from '../api/client';
import {listKillSwitches, listPendingApprovals} from '../api/managementClient';
import type {components} from '../api/generated';
import {Link, useRouter} from '../router/RouterContext';
import {useTheme} from '../styles/theme';
import {Banner, Button, CountPill, IconButton, TanpopoMark} from '../ui/primitives';
import {canAccessRoute, navigationGroups, quickCommands, routeForPath, type ConsoleRoute, type NavIconKey, type Role} from './routes';

const WorkspaceRouter = lazy(() => import('./WorkspaceRouter').then(mod => ({default: mod.WorkspaceRouter})));

type Session = components['schemas']['AdminSessionResponse'];
type Project = components['schemas']['Project'];
type Environment = components['schemas']['Environment'];

const emergencyRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const approvalRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

function NavIcon({icon}: {icon: NavIconKey}) {
  const props = {size: 16, strokeWidth: 1.85, 'aria-hidden': true as const, style: {flexShrink: 0}};
  switch (icon) {
    case 'overview': return <LayoutDashboard {...props} />;
    case 'traces': return <Activity {...props} />;
    case 'finops': return <DollarSign {...props} />;
    case 'providers': return <Server {...props} />;
    case 'models': return <Cpu {...props} />;
    case 'routes': return <GitBranch {...props} />;
    case 'extensions': return <Blocks {...props} />;
    case 'mcp': return <Network {...props} />;
    case 'playground': return <Terminal {...props} />;
    case 'access': return <KeyRound {...props} />;
    case 'security': return <ShieldAlert {...props} />;
    case 'operations': return <Sliders {...props} />;
  }
}

export function ConsoleShell() {
  const {pathname, navigate} = useRouter();
  const {theme, toggleTheme} = useTheme();
  const [session, setSession] = useState<Session | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [projectId, setProjectId] = useState('');
  const [environmentId, setEnvironmentId] = useState('');
  const [loading, setLoading] = useState(true);
  const [offline, setOffline] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [railOpen, setRailOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [pendingApprovals, setPendingApprovals] = useState(0);
  const [activeSwitches, setActiveSwitches] = useState(0);
  const [signingOut, setSigningOut] = useState(false);

  const route: ConsoleRoute = useMemo(() => routeForPath(pathname) ?? routeForPath('/overview')!, [pathname]);

  useEffect(() => {
    setRailOpen(false);
  }, [pathname]);

  useEffect(() => {
    let active = true;
    async function loadContext() {
      setLoading(true);
      setLoadError(false);
      try {
        const activeSession = await getAdminSession();
        const projectPage = await listProjects();
        if (!active) return;
        const list = projectPage.data;
        setSession(activeSession);
        setProjects(list);
        setProjectId(list[0]?.id ?? '');
        setEnvironments([]);
        setEnvironmentId('');
        setOffline(false);

        const userRoles = activeSession.roles;
        if (userRoles.some(role => emergencyRoles.includes(role))) {
          const [switchesRes, approvalsRes] = await Promise.allSettled([
            listKillSwitches(),
            userRoles.some(role => approvalRoles.includes(role)) ? listPendingApprovals() : Promise.resolve([]),
          ]);
          if (active) {
            if (switchesRes.status === 'fulfilled') setActiveSwitches(switchesRes.value.length);
            if (approvalsRes.status === 'fulfilled') setPendingApprovals(approvalsRes.value.length);
          }
        }
      } catch (error) {
        if (!active) return;
        if (error && typeof error === 'object' && 'status' in error && error.status === 401) {
          navigate('/login', {replace: true});
          return;
        }
        setOffline(error instanceof TypeError || !navigator.onLine);
        setLoadError(true);
      } finally {
        if (active) setLoading(false);
      }
    }
    void loadContext();
    return () => { active = false; };
  }, [navigate]);

  useEffect(() => {
    if (!projectId) {
      setEnvironments([]);
      setEnvironmentId('');
      return;
    }
    let active = true;
    setEnvironments([]);
    setEnvironmentId('');
    listEnvironments(projectId)
      .then(({data}) => {
        if (!active) return;
        setEnvironments(data);
        setEnvironmentId(prev => (data.some(item => item.id === prev) ? prev : data[0]?.id ?? ''));
      })
      .catch(error => {
        if (!active) return;
        setOffline(error instanceof TypeError || !navigator.onLine);
        setLoadError(true);
      });
    return () => { active = false; };
  }, [projectId]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setPaletteOpen(open => !open);
      }
      if (event.key === 'Escape' && railOpen) {
        setRailOpen(false);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [railOpen]);

  const roles = session?.roles ?? [];
  const visibleGroups = useMemo(
    () =>
      navigationGroups
        .map(group => ({
          ...group,
          routes: group.routes.filter(item => item.enabled && canAccessRoute(item, roles)),
        }))
        .filter(group => group.routes.length > 0),
    [roles],
  );

  const projectOptions = useMemo(
    () => [
      {value: '', label: 'No project'},
      ...projects.map(project => ({value: project.id, label: project.name})),
    ],
    [projects],
  );

  const environmentOptions = useMemo(
    () => [
      {value: '', label: 'No env'},
      ...environments.map(env => ({value: env.id, label: env.name})),
    ],
    [environments],
  );

  const paletteSource = useMemo(() => {
    const items: Array<SearchableItem<{group: string; path: string}>> = quickCommands
      .filter(cmd => cmd.roles.some(role => roles.includes(role)))
      .map(cmd => ({
        id: cmd.path,
        label: cmd.label,
        auxiliaryData: {group: cmd.section, path: cmd.path},
      }));
    return createStaticSource(items, {
      keywords: item => [item.auxiliaryData?.group ?? '', item.auxiliaryData?.path ?? ''],
    });
  }, [roles]);

  const canUseEmergency = roles.some(role => emergencyRoles.includes(role));
  const securityBadgeCount = pendingApprovals + activeSwitches;

  async function handleSignOut() {
    setSigningOut(true);
    try {
      await logoutAdmin();
    } catch {
      // Redirect even if session already expired.
    } finally {
      setSigningOut(false);
      navigate('/login', {replace: true});
    }
  }

  return (
    <div className="tp-shell">
      <a className="skip-link" href="#main-content">Skip to main content</a>

      {railOpen ? (
        <button
          type="button"
          className="tp-rail-backdrop"
          aria-label="Close navigation menu"
          onClick={() => setRailOpen(false)}
        />
      ) : null}

      <aside className="tp-rail" data-open={railOpen ? 'true' : 'false'} aria-label="Tanpopo console navigation">
        <div className="tp-rail-header">
          <Link href="/overview" className="tp-brand">
            <TanpopoMark size={20} />
            <span>Tanpopo</span>
          </Link>
          <IconButton label="Close navigation" className="tp-mobile-only" onClick={() => setRailOpen(false)}>
            <X size={16} aria-hidden="true" />
          </IconButton>
        </div>

        <nav className="tp-rail-scroll" aria-label="Primary sections">
          {visibleGroups.map(group => (
            <div key={group.title} className="tp-nav-group">
              <div className="tp-nav-group-title">{group.title}</div>
              {group.routes.map(item => {
                const isActive = route.path === item.path;
                const badge = item.path === '/security/approvals' && securityBadgeCount > 0 ? securityBadgeCount : undefined;
                return (
                  <Link
                    key={item.path}
                    href={item.path}
                    className="tp-nav-link"
                    aria-current={isActive ? 'page' : undefined}
                  >
                    <span className="tp-nav-link-main">
                      <NavIcon icon={item.icon} />
                      <span className="tp-nav-link-label">{item.label}</span>
                    </span>
                    {badge !== undefined ? <CountPill count={badge} tone={activeSwitches > 0 ? 'danger' : 'warn'} /> : null}
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>

        <div className="tp-rail-footer">
          <div className="tp-account-row">
            <div className="tp-account-meta">
              <div className="tp-account-email" aria-label="Signed-in account">{session?.email ?? 'Admin session'}</div>
              <div className="tp-account-role">{roles.length > 0 ? roles.join(', ') : 'Authenticating'}</div>
            </div>
            <IconButton label="Sign out" disabled={signingOut || !session} onClick={() => void handleSignOut()}>
              <LogOut size={15} aria-hidden="true" />
            </IconButton>
          </div>
        </div>
      </aside>

      <div className="tp-main-column">
        <header className="tp-topbar">
          <div className="tp-topbar-left">
            <IconButton
              label="Open navigation menu"
              className="tp-mobile-only"
              aria-expanded={railOpen}
              onClick={() => setRailOpen(open => !open)}
            >
              <Menu size={18} aria-hidden="true" />
            </IconButton>

            <div className="tp-scope-group" role="group" aria-label="Active workspace scope">
              <div className="tp-scope-field">
                <span className="tp-scope-label" aria-hidden="true">Project</span>
                <Selector
                  id="project-context"
                  label="Project"
                  isLabelHidden
                  size="sm"
                  variant="input"
                  options={projectOptions}
                  value={projectId}
                  placeholder="No project"
                  isDisabled={loading || projects.length === 0}
                  hasSearch={projects.length > 8}
                  onChange={next => setProjectId(next)}
                  containerClassName="tp-scope-selector"
                />
              </div>
              <div className="tp-scope-field">
                <span className="tp-scope-label" aria-hidden="true">Env</span>
                <Selector
                  id="environment-context"
                  label="Env"
                  isLabelHidden
                  size="sm"
                  variant="input"
                  options={environmentOptions}
                  value={environmentId}
                  placeholder="No env"
                  isDisabled={loading || environments.length === 0}
                  hasSearch={environments.length > 8}
                  onChange={next => setEnvironmentId(next)}
                  containerClassName="tp-scope-selector"
                />
              </div>
            </div>
          </div>

          <div className="tp-topbar-right">
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setPaletteOpen(true)}
            >
              <Search size={14} aria-hidden="true" />
              <span>Jump</span>
              <Kbd keys="mod+k" />
            </Button>

            {canUseEmergency ? (
              <Button
                variant={activeSwitches > 0 ? 'danger' : 'secondary'}
                size="sm"
                onClick={() => navigate('/security/kill-switches')}
              >
                <ShieldAlert
                  size={14}
                  aria-hidden="true"
                  style={activeSwitches === 0 ? {color: 'var(--tp-status-error)'} : undefined}
                />
                <span>Kill Switch</span>
                {activeSwitches > 0 ? <CountPill count={activeSwitches} tone="danger" /> : null}
              </Button>
            ) : null}

            <IconButton label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'} onClick={toggleTheme}>
              {theme === 'dark' ? <Sun size={16} aria-hidden="true" /> : <Moon size={16} aria-hidden="true" />}
            </IconButton>
          </div>
        </header>

        <main id="main-content" className="tp-content" tabIndex={-1}>
          {offline ? (
            <Banner tone="warn" title="Offline or unreachable control plane">
              Local console assets remain available. Retry when the Tanpopo server is reachable.
            </Banner>
          ) : null}

          {loading ? (
            <div className="tp-surface tp-surface-pad tp-stack" role="status" aria-busy="true">
              <Skeleton width="32%" height={20} radius={1} index={0} />
              <Skeleton width="100%" height={64} radius={2} index={1} />
              <Skeleton width="100%" height={140} radius={2} index={2} />
            </div>
          ) : loadError ? (
            <Banner tone="danger" title="Console context unavailable">
              {offline ? 'The console is offline. Local assets remain available.' : 'Unable to load session and project scope from /admin/v1.'}
            </Banner>
          ) : (
            <Suspense fallback={<p className="tp-muted" role="status">Loading section…</p>}>
              <WorkspaceRouter
                route={route}
                pathname={pathname}
                roles={roles}
                projects={projects}
                environments={environments}
                projectId={projectId}
                environmentId={environmentId}
                session={session}
              />
            </Suspense>
          )}
        </main>
      </div>

      <CommandPalette
        isOpen={paletteOpen}
        onOpenChange={setPaletteOpen}
        searchSource={paletteSource}
        label="Quick jump"
        emptySearchText="No matching sections available for your role."
        width={560}
        onValueChange={path => {
          if (path) navigate(path);
        }}
      />
    </div>
  );
}
