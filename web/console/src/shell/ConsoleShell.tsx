import {lazy, Suspense, useEffect, useMemo, useRef, useState, type FormEvent} from 'react';
import {
  Activity,
  Blocks,
  Check,
  ChevronsUpDown,
  Cpu,
  DollarSign,
  GitBranch,
  KeyRound,
  Layers,
  LayoutDashboard,
  LogOut,
  Menu,
  Moon,
  Network,
  Plus,
  Search,
  Server,
  ShieldAlert,
  Sliders,
  Sun,
  Terminal,
  X,
} from 'lucide-react';
import {Button as AstryxButton} from '@astryxdesign/core/Button';
import {CommandPalette} from '@astryxdesign/core/CommandPalette';
import {Kbd} from '@astryxdesign/core/Kbd';
import {Popover} from '@astryxdesign/core/Popover';
import {Skeleton} from '@astryxdesign/core/Skeleton';
import {createStaticSource, type SearchableItem} from '@astryxdesign/core/Typeahead';
import {
  createEnvironment,
  createProject,
  getAdminSession,
  getBootstrapStatus,
  listEnvironments,
  listOrganizations,
  listProjects,
  logoutAdmin,
  purgeOrgScopedClientState,
  setActiveOrgId,
  subscribeCrossTabLogout,
  subscribeUnauthorized,
} from '../api/client';
import {listKillSwitches, listPendingApprovals, subscribeSecurityStateChanged} from '../api/managementClient';
import type {components} from '../api/generated';
import {Link, useRouter} from '../router/RouterContext';
import {useI18n} from '../i18n/I18nProvider';
import {LanguageSelector} from '../i18n/LanguageSelector';
import {useTheme} from '../styles/theme';
import {Modal} from '../ui/Overlays';
import {Banner, Button, CountPill, IconButton, TextField} from '../ui/primitives';
import {TanpopoBrand} from '../ui/TanpopoBrand';
import {OrgSwitcher} from './OrgSwitcher';
import {canAccessRoute, navigationGroups, quickCommands, routeForPath, type ConsoleRoute, type NavIconKey, type Role} from './routes';

const WorkspaceRouter = lazy(() => import('./WorkspaceRouter').then(mod => ({default: mod.WorkspaceRouter})));

type Session = components['schemas']['AdminSessionResponse'];
type Organization = components['schemas']['Organization'];
type Project = components['schemas']['Project'];
type Environment = components['schemas']['Environment'];

const emergencyRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const approvalRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const scopeAdminRoles: Role[] = ['OPERATOR', 'ORG_OWNER'];

function toSlug(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 48);
}

function NavIcon({icon}: {icon: NavIconKey}) {
  const props = {size: 15, strokeWidth: 1.8, 'aria-hidden': true as const, style: {flexShrink: 0}};
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
  const {t} = useI18n();
  const [session, setSession] = useState<Session | null>(null);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [activeOrgId, setActiveOrgIdState] = useState('');
  const [orgSwitchEpoch, setOrgSwitchEpoch] = useState(0);
  const orgSwitchSeqRef = useRef(0);
  const [airgapOffline, setAirgapOffline] = useState(false);
  const [projects, setProjects] = useState<Project[]>([]);
  const [environments, setEnvironments] = useState<Environment[]>([]);
  const [projectId, setProjectId] = useState('');
  const [environmentId, setEnvironmentId] = useState('');
  const [loading, setLoading] = useState(true);
  const [offline, setOffline] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [railOpen, setRailOpen] = useState(false);
  const [scopeOpen, setScopeOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [pendingApprovals, setPendingApprovals] = useState(0);
  const [activeSwitches, setActiveSwitches] = useState(0);
  const [signingOut, setSigningOut] = useState(false);

  const [createProjectOpen, setCreateProjectOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState('');
  const [newProjectSlug, setNewProjectSlug] = useState('');
  const [projectSlugTouched, setProjectSlugTouched] = useState(false);

  const [createEnvOpen, setCreateEnvOpen] = useState(false);
  const [newEnvName, setNewEnvName] = useState('');
  const [newEnvSlug, setNewEnvSlug] = useState('');
  const [envSlugTouched, setEnvSlugTouched] = useState(false);

  const [scopeBusy, setScopeBusy] = useState(false);
  const [scopeError, setScopeError] = useState('');

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
        if (!active) return;
        const sessionOrg = activeSession.org_id ?? '';
        setActiveOrgId(sessionOrg || undefined);

        const [orgsRes, bootstrapRes, initialProjRes] = await Promise.all([
          listOrganizations().then(
            value => ({status: 'fulfilled', value}) as const,
            reason => ({status: 'rejected', reason}) as const,
          ),
          getBootstrapStatus().then(
            value => ({status: 'fulfilled', value}) as const,
            reason => ({status: 'rejected', reason}) as const,
          ),
          sessionOrg
            ? listProjects().then(
                value => ({status: 'fulfilled', value}) as const,
                reason => ({status: 'rejected', reason}) as const,
              )
            : Promise.resolve(null),
        ]);
        if (!active) return;

        const resolvedOrgs: Organization[] = orgsRes.status === 'fulfilled' ? orgsRes.value : [];
        const isDeleting = (item: Organization) => item.status === 'DELETING' || item.status === 'PENDING_ERASURE';
        const resolvedOrgId =
          (sessionOrg && resolvedOrgs.some(item => item.id === sessionOrg && !isDeleting(item)) ? sessionOrg : undefined)
          ?? resolvedOrgs.find(item => !isDeleting(item))?.id
          ?? (sessionOrg && resolvedOrgs.length === 0 ? sessionOrg : undefined)
          ?? resolvedOrgs[0]?.id
          ?? sessionOrg;
        setActiveOrgId(resolvedOrgId || undefined);

        let list: Project[] = [];
        if (resolvedOrgId && resolvedOrgId === sessionOrg && initialProjRes) {
          if (initialProjRes.status === 'rejected') throw initialProjRes.reason;
          list = initialProjRes.value.data;
        } else if (resolvedOrgId) {
          const loaded = await listProjects();
          if (!active) return;
          list = loaded.data;
        } else if (initialProjRes && initialProjRes.status === 'rejected') {
          throw initialProjRes.reason;
        }

        setSession(activeSession);
        setOrganizations(resolvedOrgs);
        setActiveOrgIdState(resolvedOrgId);
        setAirgapOffline(bootstrapRes.status === 'fulfilled' && Boolean(bootstrapRes.value.offline_mode));
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
    const resetAndRedirectToLogin = () => {
      setSession(null);
      setOrganizations([]);
      setActiveOrgIdState('');
      setProjects([]);
      setProjectId('');
      setEnvironments([]);
      setEnvironmentId('');
      setPendingApprovals(0);
      setActiveSwitches(0);
      navigate('/login?reason=session_expired', {replace: true});
    };
    const unsubUnauth = subscribeUnauthorized(resetAndRedirectToLogin);
    const unsubCrossTab = subscribeCrossTabLogout(resetAndRedirectToLogin);
    const unsubSec = subscribeSecurityStateChanged(() => {
      const userRoles = session?.roles ?? [];
      if (!userRoles.some(role => emergencyRoles.includes(role))) return;
      void Promise.allSettled([
        listKillSwitches(),
        userRoles.some(role => approvalRoles.includes(role)) ? listPendingApprovals() : Promise.resolve([]),
      ]).then(([switchesRes, approvalsRes]) => {
        if (switchesRes.status === 'fulfilled') setActiveSwitches(switchesRes.value.length);
        if (approvalsRes.status === 'fulfilled') setPendingApprovals(approvalsRes.value.length);
      });
    });
    return () => {
      unsubUnauth();
      unsubCrossTab();
      unsubSec();
    };
  }, [navigate, session]);

  async function refreshOrganizations() {
    try {
      const items = await listOrganizations();
      setOrganizations(items);
    } catch {
      // Non-fatal if organization list refresh fails momentarily.
    }
  }

  async function handleSelectOrg(nextOrgId: string) {
    setScopeOpen(false);
    if (!nextOrgId || nextOrgId === activeOrgId) return;
    const seq = ++orgSwitchSeqRef.current;
    purgeOrgScopedClientState();
    setActiveOrgId(nextOrgId);
    setActiveOrgIdState(nextOrgId);
    setProjects([]);
    setProjectId('');
    setEnvironments([]);
    setEnvironmentId('');
    setOrgSwitchEpoch(epoch => epoch + 1);
    setLoadError(false);
    try {
      const nextProjects = await listProjects();
      if (orgSwitchSeqRef.current !== seq) return;
      setProjects(nextProjects.data);
      setProjectId(nextProjects.data[0]?.id ?? '');
      const userRoles = session?.roles ?? [];
      if (userRoles.some(role => emergencyRoles.includes(role))) {
        const [switchesRes, approvalsRes] = await Promise.allSettled([
          listKillSwitches(),
          userRoles.some(role => approvalRoles.includes(role)) ? listPendingApprovals() : Promise.resolve([]),
        ]);
        if (orgSwitchSeqRef.current !== seq) return;
        if (switchesRes.status === 'fulfilled') setActiveSwitches(switchesRes.value.length);
        if (approvalsRes.status === 'fulfilled') setPendingApprovals(approvalsRes.value.length);
      }
    } catch (error) {
      if (orgSwitchSeqRef.current !== seq) return;
      if (error && typeof error === 'object' && 'status' in error && error.status === 401) {
        navigate('/login', {replace: true});
        return;
      }
      setOffline(error instanceof TypeError || !navigator.onLine);
      setLoadError(true);
    }
  }

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
      ...projects.map(project => ({value: project.id, label: project.name})),
      {value: '', label: t('common.noProject')},
    ],
    [projects, t],
  );

  const environmentOptions = useMemo(
    () => [
      ...environments.map(env => ({value: env.id, label: env.name})),
      {value: '', label: t('common.noEnv')},
    ],
    [environments, t],
  );

  const paletteSource = useMemo(() => {
    const items: Array<SearchableItem<{group: string; path: string}>> = quickCommands
      .filter(cmd => cmd.roles.some(role => roles.includes(role)))
      .map(cmd => ({
        id: cmd.path,
        label: t(cmd.labelKey),
        auxiliaryData: {group: t(cmd.sectionKey), path: cmd.path},
      }));
    return createStaticSource(items, {
      keywords: item => [item.auxiliaryData?.group ?? '', item.auxiliaryData?.path ?? ''],
    });
  }, [roles, t]);

  const canUseEmergency = roles.some(role => emergencyRoles.includes(role));
  const canManageScope = roles.some(role => scopeAdminRoles.includes(role));
  const multiOrgEnabled = session?.multi_org_enabled ?? true;
  const isOperator = roles.includes('OPERATOR');
  const showOrgSwitcher = multiOrgEnabled && (organizations.length > 1 || isOperator);
  const activeOrg = useMemo(() => organizations.find(item => item.id === activeOrgId) ?? organizations[0] ?? null, [organizations, activeOrgId]);
  const activeProject = useMemo(() => projects.find(item => item.id === projectId) ?? null, [projects, projectId]);
  const activeEnvironment = useMemo(() => environments.find(item => item.id === environmentId) ?? null, [environments, environmentId]);
  const securityBadgeCount = pendingApprovals + activeSwitches;

  function openCreateProjectModal() {
    setNewProjectName('');
    setNewProjectSlug('');
    setProjectSlugTouched(false);
    setScopeError('');
    setCreateProjectOpen(true);
  }

  function openCreateEnvironmentModal() {
    if (!projectId) return;
    setNewEnvName('');
    setNewEnvSlug('');
    setEnvSlugTouched(false);
    setScopeError('');
    setCreateEnvOpen(true);
  }

  async function handleCreateProject(event: FormEvent) {
    event.preventDefault();
    const name = newProjectName.trim();
    const slug = (newProjectSlug.trim() || toSlug(name));
    if (!name || !slug || scopeBusy) return;
    setScopeBusy(true);
    setScopeError('');
    try {
      const created = await createProject(name, slug);
      const updated = await listProjects();
      setProjects(updated.data);
      setProjectId(created.id);
      setCreateProjectOpen(false);
    } catch (cause) {
      setScopeError(cause instanceof Error ? cause.message : t('common.requestFailed'));
    } finally {
      setScopeBusy(false);
    }
  }

  async function handleCreateEnvironment(event: FormEvent) {
    event.preventDefault();
    const name = newEnvName.trim();
    const slug = (newEnvSlug.trim() || toSlug(name));
    if (!projectId || !name || !slug || scopeBusy) return;
    setScopeBusy(true);
    setScopeError('');
    try {
      const created = await createEnvironment(projectId, name, slug);
      const updated = await listEnvironments(projectId);
      setEnvironments(updated.data);
      setEnvironmentId(created.id);
      setCreateEnvOpen(false);
    } catch (cause) {
      setScopeError(cause instanceof Error ? cause.message : t('common.requestFailed'));
    } finally {
      setScopeBusy(false);
    }
  }

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
      <a className="skip-link" href="#main-content">{t('shell.skipToMain')}</a>

      {railOpen ? (
        <button
          type="button"
          className="tp-rail-backdrop"
          aria-label={t('shell.closeNavMenu')}
          onClick={() => setRailOpen(false)}
        />
      ) : null}

      <aside className="tp-rail" data-open={railOpen ? 'true' : 'false'} aria-label={t('shell.navLabel')}>
        <div className="tp-rail-header">
          <TanpopoBrand href="/overview" />
          <IconButton label={t('shell.closeNav')} className="tp-mobile-only" onClick={() => setRailOpen(false)}>
            <X size={16} aria-hidden="true" />
          </IconButton>
        </div>

        <nav className="tp-rail-scroll" aria-label={t('shell.primarySections')}>

          {visibleGroups.map(group => (
            <div key={group.title} className="tp-nav-group">
              <div className="tp-nav-group-title">{t(group.titleKey)}</div>
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
                      <span className="tp-nav-link-label">{t(item.labelKey)}</span>
                    </span>
                    {badge !== undefined ? <CountPill count={badge} /> : null}
                  </Link>
                );
              })}
            </div>
          ))}
        </nav>

        <div className="tp-rail-footer">
          {railOpen ? (
            <div className="tp-rail-prefs">
              <LanguageSelector id="drawer-language-selector" className="tp-rail-lang" />
              <IconButton
                variant="secondary"
                label={theme === 'dark' ? t('common.switchToLightTheme') : t('common.switchToDarkTheme')}
                onClick={toggleTheme}
              >
                {theme === 'dark' ? <Sun size={15} aria-hidden="true" /> : <Moon size={15} aria-hidden="true" />}
              </IconButton>
            </div>
          ) : null}
          <div className="tp-account-row">
            <div className="tp-account-meta">
              <div className="tp-account-email" aria-label={t('shell.signedInAccount')}>{session?.email ?? t('shell.adminSession')}</div>
              <div className="tp-account-role">{roles.length > 0 ? roles.map(r => t(`common.role.${r}`)).join(', ') : t('shell.authenticating')}</div>
            </div>
            <IconButton label={t('shell.signOut')} disabled={signingOut || !session} onClick={() => void handleSignOut()}>
              <LogOut size={15} aria-hidden="true" />
            </IconButton>
          </div>
        </div>
      </aside>

      <div className="tp-main-column">
        <header className="tp-topbar">
          <div className="tp-topbar-brand-group">
            <IconButton
              label={t('shell.openNavMenu')}
              className="tp-mobile-only"
              aria-expanded={railOpen}
              onClick={() => setRailOpen(open => !open)}
            >
              <Menu size={18} aria-hidden="true" />
            </IconButton>

            <TanpopoBrand href="/overview" className="tp-topbar-brand" />
          </div>

          <div className="tp-topbar-scope-anchor">
            <Popover
              isOpen={scopeOpen}
              onOpenChange={setScopeOpen}
              placement="below"
              alignment="start"
              width={244}
              padding={0}
              className="tp-scope-popover"
              label={t('shell.activeScope')}
              content={
                <div className="tp-scope-menu">
                  <OrgSwitcher
                    organizations={organizations}
                    activeOrgId={activeOrgId}
                    isOperator={isOperator}
                    multiOrgEnabled={multiOrgEnabled}
                    onSelectOrg={id => void handleSelectOrg(id)}
                    onManageOrgs={() => {
                      setScopeOpen(false);
                      navigate('/ops/organizations');
                    }}
                  />

                  <div className="tp-scope-menu-section">
                    <div className="tp-scope-menu-head">
                      <span>{t('common.project')}</span>
                      {canManageScope ? (
                        <IconButton
                          label={t('shell.newProject')}
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setScopeOpen(false);
                            openCreateProjectModal();
                          }}
                        >
                          <Plus size={14} aria-hidden="true" />
                        </IconButton>
                      ) : null}
                    </div>
                    <div className="tp-scope-menu-list" role="listbox" aria-label={t('common.project')}>
                      {projectOptions.map(opt => {
                        const selected = opt.value === projectId;
                        return (
                          <button
                            key={opt.value || '__no_project__'}
                            type="button"
                            role="option"
                            aria-selected={selected}
                            className="tp-scope-menu-item"
                            data-selected={selected ? 'true' : 'false'}
                            onClick={() => setProjectId(opt.value)}
                          >
                            <span className="tp-scope-menu-item-label">{opt.label}</span>
                            {selected ? <Check size={14} strokeWidth={2.2} className="tp-scope-menu-check" aria-hidden="true" /> : null}
                          </button>
                        );
                      })}
                    </div>
                  </div>

                  <div className="tp-scope-menu-divider" role="separator" />

                  <div className="tp-scope-menu-section">
                    <div className="tp-scope-menu-head">
                      <span>{t('common.envShort')}</span>
                      {canManageScope && projectId ? (
                        <IconButton
                          label={t('shell.newEnvironment')}
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setScopeOpen(false);
                            openCreateEnvironmentModal();
                          }}
                        >
                          <Plus size={14} aria-hidden="true" />
                        </IconButton>
                      ) : null}
                    </div>
                    <div className="tp-scope-menu-list" role="listbox" aria-label={t('common.envShort')}>
                      {environmentOptions.map(opt => {
                        const selected = opt.value === environmentId;
                        return (
                          <button
                            key={opt.value || '__no_env__'}
                            type="button"
                            role="option"
                            aria-selected={selected}
                            className="tp-scope-menu-item"
                            data-selected={selected ? 'true' : 'false'}
                            onClick={() => {
                              setEnvironmentId(opt.value);
                              setScopeOpen(false);
                            }}
                          >
                            <span className="tp-scope-menu-item-label">{opt.label}</span>
                            {selected ? <Check size={14} strokeWidth={2.2} className="tp-scope-menu-check" aria-hidden="true" /> : null}
                          </button>
                        );
                      })}
                    </div>
                  </div>
                </div>
              }
            >
              {triggerProps => (
                <button
                  {...triggerProps}
                  type="button"
                  className="tp-scope-switcher"
                  disabled={loading}
                  title={
                    showOrgSwitcher && activeOrg
                      ? `${activeOrg.name} / ${activeProject?.name ?? t('common.noProject')} / ${activeEnvironment?.name ?? t('common.noEnv')}`
                      : `${activeProject?.name ?? t('common.noProject')} / ${activeEnvironment?.name ?? t('common.noEnv')}`
                  }
                >
                  <Layers size={14} strokeWidth={1.8} className="tp-scope-switcher-icon" aria-hidden="true" />
                  <span className="tp-scope-switcher-text">
                    {showOrgSwitcher && activeOrg ? (
                      <>
                        <span className="tp-scope-switcher-org">{activeOrg.name}</span>
                        {' '}
                        <span className="tp-scope-switcher-sep" aria-hidden="true">/</span>
                        {' '}
                      </>
                    ) : null}
                    <span className="tp-scope-switcher-project">{activeProject?.name ?? t('common.noProject')}</span>
                    {' '}
                    <span className="tp-scope-switcher-sep" aria-hidden="true">/</span>
                    {' '}
                    <span className="tp-scope-switcher-env">{activeEnvironment?.name ?? t('common.noEnv')}</span>
                  </span>
                  <span className="sr-only"> ({t('shell.activeScope')})</span>
                  <ChevronsUpDown size={13} strokeWidth={1.8} className="tp-scope-switcher-chevron" aria-hidden="true" />
                </button>
              )}
            </Popover>
          </div>

          <div className="tp-topbar-actions">
            <AstryxButton
              variant="secondary"
              size="sm"
              className="tp-jump-btn"
              label={t('shell.jump')}
              icon={<Search size={14} aria-hidden="true" />}
              endContent={<span className="tp-desktop-kbd" aria-hidden="true"><Kbd keys="mod+k" /></span>}
              aria-keyshortcuts="Control+K Meta+K"
              onClick={() => setPaletteOpen(true)}
            />

            <div className="tp-topbar-lang">
              <LanguageSelector id="console-language-selector" />
            </div>

            <div className="tp-topbar-theme">
              <IconButton
                variant="secondary"
                label={theme === 'dark' ? t('common.switchToLightTheme') : t('common.switchToDarkTheme')}
                onClick={toggleTheme}
              >
                {theme === 'dark' ? <Sun size={15} aria-hidden="true" /> : <Moon size={15} aria-hidden="true" />}
              </IconButton>
            </div>

            {canUseEmergency ? (
              <>
                <span className="tp-topbar-divider" aria-hidden="true" />
                <AstryxButton
                  variant={activeSwitches > 0 ? 'destructive' : 'secondary'}
                  size="sm"
                  className={activeSwitches > 0 ? 'tp-killswitch-btn' : 'tp-btn-danger-outline tp-killswitch-btn'}
                  label={t('shell.killSwitch')}
                  icon={<ShieldAlert size={14} aria-hidden="true" />}
                  endContent={activeSwitches > 0 ? <CountPill count={activeSwitches} /> : undefined}
                  tooltip={t('shell.killSwitch')}
                  onClick={() => navigate('/security/kill-switches')}
                />
              </>
            ) : null}
          </div>
        </header>

        <main id="main-content" className="tp-content" tabIndex={-1}>
          {airgapOffline ? (
            <Banner tone="info" title={t('shell.airgapModeTitle')}>
              {t('shell.airgapModeDesc')}
            </Banner>
          ) : null}

          {offline ? (
            <Banner tone="warn" title={t('shell.offlineTitle')}>
              {t('shell.offlineDesc')}
            </Banner>
          ) : null}

          {loading ? (
            <div className="tp-surface tp-surface-pad tp-stack" role="status" aria-busy="true">
              <Skeleton width="32%" height={20} radius={1} index={0} />
              <Skeleton width="100%" height={64} radius={2} index={1} />
              <Skeleton width="100%" height={140} radius={2} index={2} />
            </div>
          ) : loadError ? (
            <Banner tone="danger" title={t('shell.contextUnavailableTitle')}>
              {offline ? t('shell.contextOfflineDesc') : t('shell.contextErrorDesc')}
            </Banner>
          ) : (
            <Suspense fallback={<p className="tp-muted" role="status">{t('shell.loadingSection')}</p>}>
              <WorkspaceRouter
                key={`${activeOrgId}:${orgSwitchEpoch}`}
                route={route}
                pathname={pathname}
                roles={roles}
                activeOrgId={activeOrgId}
                multiOrgEnabled={multiOrgEnabled}
                onSelectOrg={id => void handleSelectOrg(id)}
                onOrganizationsChanged={refreshOrganizations}
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
        label={t('shell.quickJump')}
        emptySearchText={t('shell.quickJumpEmpty')}
        width={560}
        onValueChange={path => {
          if (path) navigate(path);
        }}
      />

      <Modal
        open={createProjectOpen}
        title={t('shell.newProject')}
        subtitle={t('shell.newProjectDesc')}
        onClose={() => { if (!scopeBusy) setCreateProjectOpen(false); }}
        footer={
          <>
            <Button variant="secondary" disabled={scopeBusy} onClick={() => setCreateProjectOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              form="tp-create-project-form"
              variant="primary"
              loading={scopeBusy}
              disabled={!newProjectName.trim() || !(newProjectSlug.trim() || toSlug(newProjectName))}
            >
              {t('shell.createProject')}
            </Button>
          </>
        }
      >
        <form id="tp-create-project-form" className="tp-stack" onSubmit={handleCreateProject}>
          {scopeError ? <Banner tone="danger">{scopeError}</Banner> : null}
          <TextField
            label={t('shell.projectName')}
            required
            autoFocus
            value={newProjectName}
            placeholder="customer-support"
            onChange={event => {
              const next = event.target.value;
              setNewProjectName(next);
              if (!projectSlugTouched) setNewProjectSlug(toSlug(next));
            }}
          />
          <TextField
            label={t('shell.projectSlug')}
            required
            value={newProjectSlug}
            placeholder="customer-support"
            onChange={event => {
              setProjectSlugTouched(true);
              setNewProjectSlug(event.target.value);
            }}
          />
        </form>
      </Modal>

      <Modal
        open={createEnvOpen}
        title={t('shell.newEnvironment')}
        subtitle={t('shell.newEnvironmentDesc', {project: activeProject?.name ?? ''})}
        onClose={() => { if (!scopeBusy) setCreateEnvOpen(false); }}
        footer={
          <>
            <Button variant="secondary" disabled={scopeBusy} onClick={() => setCreateEnvOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              type="submit"
              form="tp-create-env-form"
              variant="primary"
              loading={scopeBusy}
              disabled={!newEnvName.trim() || !(newEnvSlug.trim() || toSlug(newEnvName))}
            >
              {t('shell.createEnvironment')}
            </Button>
          </>
        }
      >
        <form id="tp-create-env-form" className="tp-stack" onSubmit={handleCreateEnvironment}>
          {scopeError ? <Banner tone="danger">{scopeError}</Banner> : null}
          <TextField
            label={t('shell.environmentName')}
            required
            autoFocus
            value={newEnvName}
            placeholder="Production"
            onChange={event => {
              const next = event.target.value;
              setNewEnvName(next);
              if (!envSlugTouched) setNewEnvSlug(toSlug(next));
            }}
          />
          <TextField
            label={t('shell.environmentSlug')}
            required
            value={newEnvSlug}
            placeholder="prod"
            onChange={event => {
              setEnvSlugTouched(true);
              setNewEnvSlug(event.target.value);
            }}
          />
        </form>
      </Modal>
    </div>
  );
}
