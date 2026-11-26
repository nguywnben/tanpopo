import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {ArrowDown, ArrowUp, Play, Plus, Trash2} from 'lucide-react';
import {listAdminModels, listConnections, listDeployments} from '../../api/catalogClient';
import {
  createRoute,
  deleteRoute,
  explainRouteDecision,
  getEffectivePolicy,
  listRecentTraces,
  listRoutesForEnvironment,
  updatePolicy,
  updateRoute,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {ConfirmDialog, Drawer, Modal} from '../../ui/Overlays';
import {RouteTypeahead} from '../../ui/RouteTypeahead';
import {
  Banner,
  Button,
  CheckboxField,
  CodeBlock,
  KeyValueList,
  RowActionsMenu,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
  formatOperationalState,
  stateTone,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type Route = components['schemas']['RouteResource'];
type Deployment = components['schemas']['ModelDeployment'];
type LogicalModel = components['schemas']['LogicalModel'];
type Connection = components['schemas']['ProviderConnection'];
type Strategy = components['schemas']['RouteUpsertRequest']['strategy'];
type Target = components['schemas']['RouteUpsertRequest']['targets'][number];
type Policy = components['schemas']['PolicyDocument'];
type DataRegion = components['schemas']['DataRegion'];
type PolicyScope = 'ORGANIZATION' | 'PROJECT' | 'ENVIRONMENT';

const strategies: Strategy[] = ['PRIORITY_FAILOVER', 'WEIGHTED', 'WEIGHTED_ROUND_ROBIN', 'LOWEST_COST', 'LOWEST_LATENCY_EWMA'];
const allRegions: DataRegion[] = ['EU', 'US', 'APAC', 'ON_PREM'];
const trustLevels: Policy['min_plugin_trust'][] = ['OFFICIAL_ONLY', 'VERIFIED_OR_ENTERPRISE_SIGNED', 'VERIFIED_ONLY', 'ANY_APPROVED'];
const trustRank: Record<Policy['min_plugin_trust'], number> = {ANY_APPROVED: 0, VERIFIED_OR_ENTERPRISE_SIGNED: 1, VERIFIED_ONLY: 2, OFFICIAL_ONLY: 3};
const assuranceRank: Record<Policy['min_capability_assurance'], number> = {DECLARED: 0, TESTED: 1, VERIFIED: 2};
const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];

export function RoutingPage({
  pathname,
  projectId,
  environmentId,
  roles,
}: {
  pathname: string;
  projectId: string;
  environmentId: string;
  roles: Role[];
}) {
  const {navigate} = useRouter();
  const {t, formatNumber} = useI18n();
  const activeTab = pathname.startsWith('/policies')
    ? 'policies'
    : pathname.startsWith('/routes/explain')
      ? 'explain'
      : 'routes';

  const [routes, setRoutes] = useState<Route[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [models, setModels] = useState<LogicalModel[]>([]);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [traces, setTraces] = useState<components['schemas']['TraceSummary'][]>([]);

  const [orgPolicy, setOrgPolicy] = useState<Policy | null>(null);
  const [projPolicy, setProjPolicy] = useState<Policy | null>(null);
  const [envPolicy, setEnvPolicy] = useState<Policy | null>(null);
  const [etags, setEtags] = useState<Record<PolicyScope, string>>({ORGANIZATION: '', PROJECT: '', ENVIRONMENT: ''});
  const [editScope, setEditScope] = useState<PolicyScope>(environmentId ? 'ENVIRONMENT' : projectId ? 'PROJECT' : 'ORGANIZATION');
  const [policyDraft, setPolicyDraft] = useState<Policy | null>(null);
  const [hookExtId, setHookExtId] = useState('');
  const [hookPosture, setHookPosture] = useState<'FAIL_CLOSED' | 'FAIL_OPEN'>('FAIL_CLOSED');
  const [confirmGuardrailOpen, setConfirmGuardrailOpen] = useState(false);

  const [routeDrawerOpen, setRouteDrawerOpen] = useState(false);
  const [editingRoute, setEditingRoute] = useState<Route | null>(null);
  const [slug, setSlug] = useState('');
  const [strategy, setStrategy] = useState<Strategy>('PRIORITY_FAILOVER');
  const [maxHops, setMaxHops] = useState(3);
  const [timeoutMs, setTimeoutMs] = useState(60000);
  const [targets, setTargets] = useState<Target[]>([]);
  const [selectedDep, setSelectedDep] = useState('');
  const [routeFormError, setRouteFormError] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Route | null>(null);

  const [explainModel, setExplainModel] = useState('');
  const [explainRegion, setExplainRegion] = useState<DataRegion | ''>('');
  const [explainDialect, setExplainDialect] = useState('');
  const [explainCaps, setExplainCaps] = useState<string[]>([]);
  const [explainResult, setExplainResult] = useState<components['schemas']['RouteExplainResponse'] | null>(null);
  const [explainBusy, setExplainBusy] = useState(false);

  const [conflict, setConflict] = useState<{kind: 'route' | 'policy'; message: string; current: unknown} | null>(null);

  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const refreshSeqRef = useRef(0);
  const editScopeRef = useRef<PolicyScope>(editScope);
  editScopeRef.current = editScope;

  const canManage = roles.some(role => manageRoles.includes(role));
  const canReadConnections = roles.some(role => ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER', 'FINOPS_VIEWER'].includes(role));
  const canReadTraces = roles.some(role => role !== 'FINOPS_VIEWER');
  const canReadOrgPolicy = roles.some(role => ['OPERATOR', 'ORG_OWNER'].includes(role));
  const scopeLabel = useCallback((scope: PolicyScope) => {
    if (scope === 'ORGANIZATION') return t('common.organization');
    if (scope === 'PROJECT') return t('common.project');
    return t('common.environment');
  }, [t]);

  const refresh = useCallback(async () => {
    const seq = ++refreshSeqRef.current;
    const hasEnvScope = Boolean(projectId && environmentId);
    if (!hasEnvScope) {
      setRoutes([]);
      setDeployments([]);
      setTraces([]);
      setEnvPolicy(null);
      if (editScopeRef.current === 'ENVIRONMENT') {
        const fallbackScope: PolicyScope = projectId ? 'PROJECT' : 'ORGANIZATION';
        editScopeRef.current = fallbackScope;
        setEditScope(fallbackScope);
      }
    }
    setLoading(true);
    setError('');
    try {
      const [routesRes, depsRes, modelsRes, connsRes, tracesRes, orgPolRes, projPolRes, envPolRes] = await Promise.allSettled([
        hasEnvScope ? listRoutesForEnvironment(environmentId) : Promise.resolve([] as Route[]),
        hasEnvScope ? listDeployments(environmentId) : Promise.resolve([] as Deployment[]),
        listAdminModels(),
        canReadConnections ? listConnections() : Promise.resolve([] as Connection[]),
        hasEnvScope && canManage && canReadTraces
          ? listRecentTraces(environmentId)
          : Promise.resolve([] as components['schemas']['TraceSummary'][]),
        canReadOrgPolicy ? getEffectivePolicy() : Promise.resolve(null),
        projectId ? getEffectivePolicy(projectId) : Promise.resolve(null),
        hasEnvScope ? getEffectivePolicy(projectId, environmentId) : Promise.resolve(null),
      ]);
      if (refreshSeqRef.current !== seq) return;
      const primaryFail = [routesRes, depsRes, modelsRes].find(res => res.status === 'rejected');
      if (primaryFail && primaryFail.status === 'rejected') {
        setError(errText(primaryFail.reason, t));
      }
      const routeRows = routesRes.status === 'fulfilled' ? routesRes.value : [];
      const depRows = depsRes.status === 'fulfilled' ? depsRes.value : [];
      const modelRows = modelsRes.status === 'fulfilled' ? modelsRes.value : [];
      const connRows = connsRes.status === 'fulfilled' ? connsRes.value : [];
      const traceRows = tracesRes.status === 'fulfilled' ? tracesRes.value : [];
      const orgRes = orgPolRes.status === 'fulfilled' ? orgPolRes.value : null;
      const projRes = projPolRes.status === 'fulfilled' ? projPolRes.value : null;
      const envRes = envPolRes.status === 'fulfilled' ? envPolRes.value : null;
      setRoutes(routeRows);
      setDeployments(depRows);
      setModels(modelRows);
      setConnections(connRows);
      setTraces(traceRows);
      setOrgPolicy(orgRes ? orgRes.policy : null);
      setProjPolicy(projRes ? projRes.policy : null);
      setEnvPolicy(envRes ? envRes.policy : null);
      setEtags({
        ORGANIZATION: orgRes ? orgRes.etag : '',
        PROJECT: projRes ? projRes.etag : '',
        ENVIRONMENT: envRes ? envRes.etag : '',
      });
      const activeScope = editScopeRef.current;
      const orgPol = orgRes ? orgRes.policy : null;
      const projPol = projRes ? projRes.policy : null;
      const envPol = envRes ? envRes.policy : null;
      const activePol =
        activeScope === 'ORGANIZATION'
          ? orgPol
          : activeScope === 'PROJECT'
            ? (projPol ?? orgPol)
            : (envPol ?? projPol ?? orgPol);
      setPolicyDraft(activePol);
    } catch (cause) {
      if (refreshSeqRef.current !== seq) return;
      setError(errText(cause, t));
    } finally {
      if (refreshSeqRef.current === seq) setLoading(false);
    }
  }, [projectId, environmentId, canManage, canReadConnections, canReadTraces, canReadOrgPolicy, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  const deploymentById = useMemo(() => new Map(deployments.map(d => [d.id, d])), [deployments]);
  const modelById = useMemo(() => new Map(models.map(m => [m.id, m])), [models]);
  const connectionById = useMemo(() => new Map(connections.map(c => [c.id, c])), [connections]);
  const formatDeploymentLabel = useCallback(
    (dep: Deployment) => {
      const connName = connectionById.get(dep.connection_id)?.name ?? dep.connection_id;
      return `${connName} / ${dep.upstream_model_name} · ${dep.id.slice(-6)}`;
    },
    [connectionById],
  );
  const trafficByRoute = useMemo(() => {
    const map = new Map<string, number>();
    for (const trace of traces) map.set(trace.route_slug, (map.get(trace.route_slug) ?? 0) + 1);
    return map;
  }, [traces]);

  const allowedRegions = envPolicy?.allowed_data_regions ?? [];
  const explainRegionChoices = allowedRegions.length > 0 ? allowedRegions : allRegions;
  const explainRegionAllowed = Boolean(explainRegion) && (allowedRegions.length === 0 || allowedRegions.includes(explainRegion as DataRegion));

  function openCreateRoute() {
    setEditingRoute(null);
    setSlug('');
    setStrategy('PRIORITY_FAILOVER');
    setMaxHops(3);
    setTimeoutMs(60000);
    setTargets([]);
    setSelectedDep('');
    setRouteFormError('');
    setRouteDrawerOpen(true);
  }

  function openEditRoute(route: Route) {
    setEditingRoute(route);
    setSlug(route.slug);
    setStrategy(route.strategy);
    setMaxHops(route.max_failover_hops);
    setTimeoutMs(route.request_timeout_ms);
    setTargets(route.targets.map(tg => ({...tg})));
    setSelectedDep('');
    setRouteFormError('');
    setRouteDrawerOpen(true);
  }

  function routePayload(): components['schemas']['RouteUpsertRequest'] {
    return {
      slug: slug.trim(),
      env_id: environmentId,
      strategy,
      max_failover_hops: maxHops,
      request_timeout_ms: timeoutMs,
      allowed_data_regions: editingRoute?.allowed_data_regions ?? [],
      targets: targets.map((tg, index) => ({...tg, priority_order: index + 1})),
    };
  }

  const totalWeight = targets.reduce((sum, tg) => sum + (tg.weight ?? 0), 0);
  const routeHasInvalidRegion = targets.some(tg => {
    const dep = deploymentById.get(tg.deployment_id);
    return !dep || (allowedRegions.length > 0 && !allowedRegions.includes(dep.data_region));
  });
  const routeMissingToolCalling = targets.some(tg => {
    const dep = deploymentById.get(tg.deployment_id);
    return modelById.get(dep?.logical_model_id ?? '')?.capabilities.tool_calling !== true;
  });

  async function saveRoute() {
    if (!canManage || !slug.trim() || targets.length === 0 || (isWeighted(strategy) && totalWeight !== 100) || routeHasInvalidRegion) return;
    setBusy(true);
    setRouteFormError('');
    setError('');
    setNotice('');
    try {
      if (editingRoute) await updateRoute(editingRoute, routePayload());
      else await createRoute(routePayload());
      setRouteDrawerOpen(false);
      await refresh();
      setNotice(editingRoute ? t('routing.routeUpdated', {slug: slug.trim()}) : t('routing.routeCreated', {slug: slug.trim()}));
    } catch (cause) {
      setRouteFormError(errText(cause, t));
      if (isConflict(cause)) setConflict({kind: 'route', message: errText(cause, t), current: currentResource(cause)});
    } finally {
      setBusy(false);
    }
  }

  async function confirmDeleteRoute() {
    if (!deleteTarget) return;
    setBusy(true);
    setError('');
    try {
      await deleteRoute(deleteTarget);
      setDeleteTarget(null);
      await refresh();
      setNotice(t('routing.routeDeleted'));
    } catch (cause) {
      setDeleteTarget(null);
      setError(errText(cause, t));
      if (isConflict(cause)) setConflict({kind: 'route', message: errText(cause, t), current: currentResource(cause)});
    } finally {
      setBusy(false);
    }
  }

  async function runExplain() {
    if (!environmentId || !explainModel.trim() || !explainRegionAllowed) return;
    setExplainBusy(true);
    setError('');
    setExplainResult(null);
    try {
      const payload: components['schemas']['RouteExplainRequest'] = {
        env_id: environmentId,
        target_model: explainModel.trim(),
        data_region: explainRegion as DataRegion,
        required_capabilities: explainCaps,
        estimated_input_tokens: 512,
        max_output_tokens: 1024,
        ...(explainDialect ? {dialect: explainDialect} : {}),
      };
      const result = await explainRouteDecision(payload);
      if (!result.dry_run) throw new Error(t('routing.explainNotDryRun'));
      setExplainResult(result);
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setExplainBusy(false);
    }
  }

  const effectiveDisplayPolicy = envPolicy ?? projPolicy ?? orgPolicy;
  const selectedPolicy = editScope === 'ORGANIZATION' ? orgPolicy : editScope === 'PROJECT' ? projPolicy : envPolicy;
  const parentPolicy = editScope === 'ENVIRONMENT' ? projPolicy : editScope === 'PROJECT' ? orgPolicy : null;
  const canEditPolicy = Boolean(selectedPolicy && (editScope === 'ORGANIZATION' ? (roles.includes('OPERATOR') || roles.includes('ORG_OWNER')) : canManage));
  const parentModels = parentPolicy?.allowed_models;
  const modelRelaxesParent = Boolean(policyDraft && parentModels && !isSubset(policyDraft.allowed_models ?? ['*'], parentModels));
  const parentRegions = parentPolicy?.allowed_data_regions ?? allRegions;
  const parentTrust = parentPolicy?.min_plugin_trust ?? 'ANY_APPROVED';
  const parentAssurance = parentPolicy?.min_capability_assurance ?? 'DECLARED';
  const hookRelaxesParent =
    hookPosture === 'FAIL_OPEN' &&
    Boolean(parentPolicy?.guardrail_hooks?.some(h => h.extension_id === hookExtId.trim() && h.failure_posture === 'FAIL_CLOSED'));

  async function savePolicy(confirmedGuardrail = false) {
    if (!selectedPolicy || !policyDraft || !etags[editScope] || !canEditPolicy) return;
    const addingBlockingHook = policyDraft.guardrail_hooks?.some(
      hook =>
        hook.failure_posture === 'FAIL_CLOSED' &&
        !(selectedPolicy.guardrail_hooks ?? []).some(existing => existing.extension_id === hook.extension_id),
    );
    if (addingBlockingHook && !confirmedGuardrail) {
      setConfirmGuardrailOpen(true);
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updatePolicy(selectedPolicy, etags[editScope], policyDraft);
      await refresh();
      setNotice(t('routing.policySaved', {scope: scopeLabel(editScope)}));
    } catch (cause) {
      setError(errText(cause, t));
      if (isConflict(cause)) setConflict({kind: 'policy', message: errText(cause, t), current: currentResource(cause)});
    } finally {
      setBusy(false);
    }
  }

  function applyConflictVersion() {
    if (!conflict?.current || typeof conflict.current !== 'object' || !('id' in conflict.current) || !('version' in conflict.current)) return;
    if (conflict.kind === 'route') {
      const latest = conflict.current as Route;
      if (editingRoute?.id === latest.id) setEditingRoute(latest);
      if (deleteTarget?.id === latest.id) setDeleteTarget(latest);
    } else if (policyDraft) {
      const latest = conflict.current as Policy;
      if (latest.scope_tier !== editScope) return;
      if (editScope === 'ORGANIZATION') setOrgPolicy(latest);
      else if (editScope === 'PROJECT') setProjPolicy(latest);
      else setEnvPolicy(latest);
      setEtags(prev => ({...prev, [editScope]: `"v${latest.version}"`}));
      setPolicyDraft({...policyDraft, id: latest.id, version: latest.version});
    }
    setConflict(null);
  }

  const policyRows = effectiveDisplayPolicy
    ? [
        {id: 'regions', rule: t('routing.rule.regions'), value: (effectiveDisplayPolicy.allowed_data_regions ?? []).join(', ') || t('common.noneConfigured'), source: regionSource(orgPolicy, projPolicy, effectiveDisplayPolicy, t, scopeLabel)},
        {id: 'models', rule: t('routing.rule.models'), value: (effectiveDisplayPolicy.allowed_models ?? ['*']).join(', ') || t('routing.rule.denyAll'), source: modelSource(orgPolicy, projPolicy, effectiveDisplayPolicy, t, scopeLabel)},
        {id: 'trust', rule: t('routing.rule.trust'), value: t(`routing.trust.${effectiveDisplayPolicy.min_plugin_trust ?? 'ANY_APPROVED'}`), source: scalarSource(orgPolicy, projPolicy, effectiveDisplayPolicy, 'min_plugin_trust', t, scopeLabel)},
        {id: 'assurance', rule: t('routing.rule.assurance'), value: formatOperationalState(effectiveDisplayPolicy.min_capability_assurance ?? 'DECLARED'), source: scalarSource(orgPolicy, projPolicy, effectiveDisplayPolicy, 'min_capability_assurance', t, scopeLabel)},
        {id: 'rpm', rule: t('routing.rule.rpm'), value: effectiveDisplayPolicy.rpm_ceiling != null ? formatNumber(effectiveDisplayPolicy.rpm_ceiling) : t('common.unspecified'), source: scalarSource(orgPolicy, projPolicy, effectiveDisplayPolicy, 'rpm_ceiling', t, scopeLabel)},
        {id: 'tpm', rule: t('routing.rule.tpm'), value: effectiveDisplayPolicy.tpm_ceiling != null ? formatNumber(effectiveDisplayPolicy.tpm_ceiling) : t('common.unspecified'), source: scalarSource(orgPolicy, projPolicy, effectiveDisplayPolicy, 'tpm_ceiling', t, scopeLabel)},
        {id: 'streams', rule: t('routing.rule.streams'), value: effectiveDisplayPolicy.max_concurrent_streams != null ? formatNumber(effectiveDisplayPolicy.max_concurrent_streams) : t('common.unspecified'), source: scalarSource(orgPolicy, projPolicy, effectiveDisplayPolicy, 'max_concurrent_streams', t, scopeLabel)},
        {
          id: 'hooks',
          rule: t('routing.rule.hooks'),
          value: (effectiveDisplayPolicy.guardrail_hooks ?? []).map(h => `${h.extension_id} (${formatOperationalState(h.failure_posture)})`).join(', ') || t('common.noneAttached'),
          source: hookSource(orgPolicy, projPolicy, effectiveDisplayPolicy, t, scopeLabel),
        },
      ]
    : [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('routing.title')}</h1>
          <p className="tp-page-subtitle">{t('routing.subtitle')}</p>
        </div>
        {canManage && activeTab === 'routes' ? (
          <div className={ !loading && routes.length === 0 && environmentId ? 'tp-page-actions tp-header-cta-desktop' : 'tp-page-actions' }>
            <Button
              variant={!loading && routes.length === 0 && environmentId ? 'secondary' : 'primary'}
              size="sm"
              disabled={!environmentId}
              onClick={openCreateRoute}
            >
              <Plus size={14} aria-hidden="true" />
              <span>{t('routing.createRoute')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('routing.tabsLabel')}
        activeId={activeTab}
        onSelect={id => {
          if (id === 'explain') navigate('/routes/explain');
          else if (id === 'policies') navigate('/policies');
          else navigate('/routes');
        }}
        items={[
          {id: 'routes', label: t('routing.tabRoutes'), count: routes.length},
          {id: 'explain', label: t('routing.tabExplain')},
          {id: 'policies', label: t('routing.tabPolicies')},
        ]}
      />

      {!environmentId && activeTab !== 'policies' ? <Banner tone="info">{t('routing.selectScopeBanner')}</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'routes' ? (
        <section className="tp-section">
          {allowedRegions.length > 0 ? (
            <p className="tp-muted">{t('routing.residencyCeiling')} <span className="mono">{allowedRegions.join(', ')}</span></p>
          ) : null}
          <SectionHeader title={t('routing.configuredRoutes')} count={routes.length} />
          <DataTable
            ariaLabel={t('routing.configuredRoutes')}
            loading={loading}
            rows={routes}
            rowKey={row => row.id}
            emptyTitle={t('routing.emptyRoutesTitle')}
            emptyDescription={t('routing.emptyRoutesDesc')}
            emptyAction={
              canManage && environmentId ? (
                <Button variant="primary" size="md" onClick={openCreateRoute}>
                  <Plus size={14} aria-hidden="true" />
                  <span>{t('routing.createRoute')}</span>
                </Button>
              ) : undefined
            }
            columns={[
              {id: 'slug', header: t('routing.col.routeSlug'), primary: true, width: '20%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.slug}</span>},
              {id: 'strategy', header: t('routing.col.strategy'), width: '16%', render: row => <span>{t(`routing.strategy.${row.strategy}`)}</span>},
              {
                id: 'fallbacks',
                header: t('routing.col.fallbackChain'),
                width: '26%',
                render: row => (
                  <span className="mono">
                    {row.targets.map(tg => {
                      const dep = deploymentById.get(tg.deployment_id);
                      const label = dep ? formatDeploymentLabel(dep) : tg.deployment_id;
                      return isWeighted(row.strategy) && tg.weight !== undefined ? `${label} (${tg.weight}%)` : label;
                    }).join(' → ')}
                  </span>
                ),
              },
              {
                id: 'health',
                header: t('routing.col.targetHealth'),
                width: '28%',
                render: row => (
                  <div className="tp-status-chain">
                    {row.targets.map((tg, idx) => {
                      const dep = deploymentById.get(tg.deployment_id);
                      const label =
                        dep && dep.state === 'ACTIVE_HEALTHY' && dep.connection_state && dep.connection_state !== 'ACTIVE'
                          ? dep.connection_state
                          : (dep?.state ?? 'UNAVAILABLE');
                      return (
                        <span key={tg.deployment_id} className="tp-status-chain">
                          {idx > 0 ? <span className="tp-status-sep" aria-hidden="true">·</span> : null}
                          <StatusPill tone={stateTone(label)} label={formatOperationalState(label)} />
                        </span>
                      );
                    })}
                  </div>
                ),
              },
              {
                id: 'actions',
                header: t('common.actions'),
                width: '10%',
                render: row =>
                  canManage ? (
                    <RowActionsMenu
                      label={t('common.actionsFor', {name: row.slug})}
                      items={[
                        {id: 'edit', label: t('routing.action.edit'), onClick: () => openEditRoute(row)},
                        {type: 'divider' as const},
                        {id: 'delete', label: t('routing.action.delete'), variant: 'destructive' as const, onClick: () => setDeleteTarget(row)},
                      ]}
                    />
                  ) : null,
              },
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'explain' ? (
        <section className="tp-section">
          <div className="tp-surface tp-surface-pad tp-stack">
            <h2>{t('routing.explainHeading')}</h2>
            <p className="tp-muted">{t('routing.explainDesc')}</p>
            <div className="tp-grid-3">
              <RouteTypeahead
                label={t('routing.explainTargetLabel')}
                suggestions={[...routes.map(r => r.slug), ...models.map(m => m.slug)]}
                placeholder={t('routing.explainTargetPlaceholder')}
                suggestionDescription={t('routing.explainSuggestionDesc')}
                value={explainModel}
                disabled={explainBusy}
                onChange={setExplainModel}
              />
              <SelectField
                label={t('routing.dataResidency')}
                value={explainRegion}
                disabled={explainBusy}
                onChange={next => setExplainRegion(next as DataRegion | '')}
                options={[
                  {value: '', label: t('routing.chooseRegion')},
                  ...explainRegionChoices.map(reg => ({value: reg, label: t(`common.region.${reg}`)})),
                ]}
              />
              <SelectField
                label={t('routing.explainDialect')}
                value={explainDialect}
                disabled={explainBusy}
                onChange={setExplainDialect}
                options={[
                  {value: '', label: t('routing.dialectNormalized')},
                  {value: 'openai_compatible', label: 'openai_compatible'},
                  {value: 'ollama', label: 'ollama'},
                  {value: 'anthropic', label: 'anthropic'},
                  {value: 'google_gemini', label: 'google_gemini'},
                ]}
              />
            </div>
            <div className="tp-checkbox-group">
              {(['tools', 'vision', 'context_tokens'] as const).map(cap => (
                <CheckboxField
                  key={cap}
                  label={cap.replaceAll('_', ' ')}
                  checked={explainCaps.includes(cap)}
                  onChange={checked => setExplainCaps(curr => (checked ? [...curr, cap] : curr.filter(item => item !== cap)))}
                />
              ))}
            </div>
            <div>
              <Button
                variant="primary"
                size="sm"
                loading={explainBusy}
                disabled={!environmentId || !explainModel.trim() || !explainRegionAllowed}
                onClick={() => void runExplain()}
              >
                <Play size={14} aria-hidden="true" />
                <span>{explainBusy ? t('routing.simulating') : t('routing.explainButton')}</span>
              </Button>
            </div>
          </div>

          {explainResult ? (
            <div className="tp-surface tp-surface-pad tp-stack" role="status">
              <SectionHeader title={t('routing.dryRunDecision')} />
              <KeyValueList
                items={[
                  {label: t('routing.dryRunVerified'), value: <StatusPill tone="ok" label={explainResult.dry_run ? t('routing.dryRunBadge') : t('common.unknown')} />},
                  {label: t('routing.matchedRoute'), value: explainResult.route_slug || '—', mono: true},
                  {label: t('routing.eligibleCandidates'), value: formatNumber(explainResult.selected_candidates_ordered.length), mono: true},
                  {label: t('routing.rejectedDeployments'), value: formatNumber(explainResult.rejected_deployments.length), mono: true},
                ]}
              />
              <CodeBlock title={t('routing.detailedExplainOutput')} code={JSON.stringify(explainResult, null, 2)} />
            </div>
          ) : null}
        </section>
      ) : null}

      {activeTab === 'policies' && effectiveDisplayPolicy && policyDraft ? (
        <div className="tp-stack">
          <section className="tp-section">
            <SectionHeader title={t('routing.effectivePolicy')} />
            <DataTable
              ariaLabel={t('routing.policyHierarchyAria')}
              rows={policyRows}
              rowKey={row => row.id}
              columns={[
                {id: 'rule', header: t('routing.col.constraint'), primary: true, width: '30%', render: row => <span style={{fontWeight: 600}}>{row.rule}</span>},
                {id: 'val', header: t('routing.col.effectiveValue'), width: '35%', render: row => <span className="mono">{String(row.value)}</span>},
                {id: 'src', header: t('routing.col.provenance'), width: '35%', render: row => <span className="tp-muted">{row.source}</span>},
              ]}
            />
          </section>

          <section className="tp-section">
            <SectionHeader
              title={t('routing.editPolicyScope')}
              actions={
                <div className="tp-scope-field">
                  <span className="tp-scope-label" aria-hidden="true">{t('routing.scopeToEdit')}</span>
                  <SelectField
                    label={t('routing.scopeToEdit')}
                    isLabelHidden
                    size="sm"
                    value={editScope}
                    width={168}
                    onChange={nextValue => {
                      const next = nextValue as PolicyScope;
                      setEditScope(next);
                      const nextPol = next === 'ORGANIZATION' ? orgPolicy : next === 'PROJECT' ? projPolicy : envPolicy;
                      if (nextPol) setPolicyDraft(nextPol);
                    }}
                    options={[
                      {value: 'ORGANIZATION', label: t('common.organization')},
                      {value: 'PROJECT', label: t('common.project'), disabled: !projectId || !projPolicy},
                      {value: 'ENVIRONMENT', label: t('common.environment'), disabled: !environmentId || !envPolicy},
                    ]}
                  />
                </div>
              }
            />

            <div className="tp-surface tp-surface-pad tp-stack">
              {!canEditPolicy ? (
                <Banner tone="info">{t('routing.scopeReadOnly')}</Banner>
              ) : null}

              <div className="tp-stack-sm">
                <span className="tp-label">{t('routing.rule.regions')}</span>
                <div className="tp-checkbox-group">
                  {parentRegions.map(reg => {
                    const checked = (policyDraft.allowed_data_regions ?? []).includes(reg);
                    return (
                      <CheckboxField
                        key={reg}
                        label={t(`common.region.${reg}`)}
                        checked={checked}
                        disabled={!canEditPolicy || busy}
                        onChange={nextChecked => {
                          const current = policyDraft.allowed_data_regions ?? [];
                          const updated = nextChecked ? [...new Set([...current, reg])] : current.filter(item => item !== reg);
                          setPolicyDraft({...policyDraft, allowed_data_regions: updated});
                        }}
                      />
                    );
                  })}
                </div>
              </div>

              <TextField
                label={t('routing.allowedModelsInput')}
                value={(policyDraft.allowed_models ?? ['*']).join(', ')}
                disabled={!canEditPolicy || busy}
                error={modelRelaxesParent ? t('routing.errWidenModels') : undefined}
                onChange={event => {
                  const list = event.target.value.split(',').map(v => v.trim()).filter(Boolean);
                  setPolicyDraft({...policyDraft, allowed_models: list});
                }}
              />

              <div className="tp-grid-2">
                <SelectField
                  label={t('routing.rule.trust')}
                  value={policyDraft.min_plugin_trust}
                  disabled={!canEditPolicy || busy}
                  onChange={next => setPolicyDraft({...policyDraft, min_plugin_trust: next as Policy['min_plugin_trust']})}
                  options={trustLevels
                    .filter(level => trustRank[level] >= trustRank[parentTrust])
                    .map(level => ({value: level, label: t(`routing.trust.${level}`)}))}
                />
                <SelectField
                  label={t('routing.rule.assurance')}
                  value={policyDraft.min_capability_assurance}
                  disabled={!canEditPolicy || busy}
                  onChange={next => setPolicyDraft({...policyDraft, min_capability_assurance: next as Policy['min_capability_assurance']})}
                  options={(['VERIFIED', 'TESTED', 'DECLARED'] as const)
                    .filter(level => assuranceRank[level] >= assuranceRank[parentAssurance])
                    .map(level => ({value: level, label: formatOperationalState(level)}))}
                />
              </div>

              <div className="tp-grid-3">
                <TextField
                  label={t('routing.rule.rpm')}
                  type="number"
                  min={0}
                  max={parentPolicy?.rpm_ceiling ?? undefined}
                  value={policyDraft.rpm_ceiling ?? ''}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, rpm_ceiling: event.target.value === '' ? null : Number(event.target.value)})}
                />
                <TextField
                  label={t('routing.rule.tpm')}
                  type="number"
                  min={0}
                  max={parentPolicy?.tpm_ceiling ?? undefined}
                  value={policyDraft.tpm_ceiling ?? ''}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, tpm_ceiling: event.target.value === '' ? null : Number(event.target.value)})}
                />
                <TextField
                  label={t('routing.rule.streams')}
                  type="number"
                  min={0}
                  max={parentPolicy?.max_concurrent_streams ?? undefined}
                  value={policyDraft.max_concurrent_streams ?? ''}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, max_concurrent_streams: event.target.value === '' ? null : Number(event.target.value)})}
                />
              </div>

              <div className="tp-stack-sm">
                <h3>{t('routing.guardrailExtensions')}</h3>
                {(policyDraft.guardrail_hooks ?? []).length > 0 ? (
                  <ul className="tp-list">
                    {(policyDraft.guardrail_hooks ?? []).map(hook => {
                      const lockedByParent = Boolean(parentPolicy?.guardrail_hooks?.some(h => h.extension_id === hook.extension_id));
                      return (
                        <li key={hook.extension_id} className="tp-list-item">
                          <div className="tp-row">
                            <span className="mono">{hook.extension_id}</span>
                            <StatusPill tone={stateTone(hook.failure_posture)} label={formatOperationalState(hook.failure_posture)} />
                          </div>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={!canEditPolicy || busy || lockedByParent}
                            onClick={() => setPolicyDraft({...policyDraft, guardrail_hooks: (policyDraft.guardrail_hooks ?? []).filter(h => h.extension_id !== hook.extension_id)})}
                          >
                            {t('common.remove')}
                          </Button>
                        </li>
                      );
                    })}
                  </ul>
                ) : (
                  <p className="tp-muted">{t('routing.noGuardrails')}</p>
                )}

                {canEditPolicy ? (
                  <div className="tp-inline-form">
                    <div className="tp-inline-form-field">
                      <TextField label={t('routing.guardrailExtId')} fieldSize="sm" value={hookExtId} disabled={busy} onChange={event => setHookExtId(event.target.value)} />
                    </div>
                    <div className="tp-inline-form-field">
                      <SelectField
                        label={t('routing.failurePosture')}
                        size="sm"
                        value={hookPosture}
                        disabled={busy}
                        error={hookRelaxesParent ? t('routing.errRelaxGuardrail') : undefined}
                        onChange={next => setHookPosture(next as 'FAIL_CLOSED' | 'FAIL_OPEN')}
                        options={[
                          {value: 'FAIL_CLOSED', label: formatOperationalState('FAIL_CLOSED')},
                          {value: 'FAIL_OPEN', label: formatOperationalState('FAIL_OPEN')},
                        ]}
                      />
                    </div>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busy || !hookExtId.trim() || hookRelaxesParent || (policyDraft.guardrail_hooks ?? []).some(h => h.extension_id === hookExtId.trim())}
                      onClick={() => {
                        setPolicyDraft({
                          ...policyDraft,
                          guardrail_hooks: [...(policyDraft.guardrail_hooks ?? []), {extension_id: hookExtId.trim(), failure_posture: hookPosture}],
                        });
                        setHookExtId('');
                      }}
                    >
                      {t('routing.attachGuardrail')}
                    </Button>
                  </div>
                ) : null}
              </div>

              <div className="tp-row" style={{justifyContent: 'flex-end'}}>
                <Button
                  variant="secondary"
                  disabled={!selectedPolicy || busy || JSON.stringify(selectedPolicy) === JSON.stringify(policyDraft)}
                  onClick={() => selectedPolicy && setPolicyDraft(selectedPolicy)}
                >
                  {t('routing.discardChanges')}
                </Button>
                <Button
                  variant="primary"
                  loading={busy}
                  disabled={!canEditPolicy || modelRelaxesParent || JSON.stringify(selectedPolicy) === JSON.stringify(policyDraft)}
                  onClick={() => void savePolicy()}
                >
                  {t('routing.savePolicy')}
                </Button>
              </div>
            </div>
          </section>
        </div>
      ) : null}

      <Drawer
        open={routeDrawerOpen}
        onClose={() => {
          setRouteDrawerOpen(false);
          setRouteFormError('');
        }}
        size="lg"
        title={editingRoute ? t('routing.drawerEditTitle', {slug: editingRoute.slug}) : t('routing.createRoute')}
        subtitle={t('routing.drawerSubtitle')}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setRouteDrawerOpen(false)}>{t('common.cancel')}</Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={!canManage || !slug.trim() || targets.length === 0 || (isWeighted(strategy) && totalWeight !== 100) || routeHasInvalidRegion}
              onClick={() => void saveRoute()}
            >
              {editingRoute ? t('routing.saveRoute') : t('routing.createRoute')}
            </Button>
          </>
        }
      >
        {routeFormError ? <Banner tone="danger">{routeFormError}</Banner> : null}
        <TextField label={t('routing.col.routeSlug')} value={slug} disabled={busy} onChange={event => setSlug(event.target.value)} />
        <SelectField
          label={t('routing.routingStrategy')}
          value={strategy}
          disabled={busy}
          onChange={nextValue => {
            const next = nextValue as Strategy;
            setStrategy(next);
            if (isWeighted(next) && targets.length > 0) setTargets(distributeWeights(targets.map(tg => ({...tg}))));
            if (!isWeighted(next)) setTargets(targets.map(({weight: _w, ...rest}) => rest));
          }}
          options={strategies.map(item => ({value: item, label: t(`routing.strategy.${item}`)}))}
        />
        <div className="tp-grid-2">
          <TextField label={t('routing.maxFailoverHops')} type="number" min={1} max={3} value={maxHops} disabled={busy} onChange={event => setMaxHops(Number(event.target.value) || 1)} />
          <TextField label={t('routing.requestTimeoutMs')} type="number" min={500} max={300000} value={timeoutMs} disabled={busy} onChange={event => setTimeoutMs(Number(event.target.value) || 60000)} />
        </div>

        <div className="tp-inline-form">
          <div className="tp-inline-form-field">
            <SelectField
              label={t('routing.addDeploymentTarget')}
              value={selectedDep}
              disabled={busy}
              onChange={setSelectedDep}
              options={[
                {value: '', label: t('routing.selectDeployment')},
                ...deployments
                  .filter(d => !targets.some(tg => tg.deployment_id === d.id))
                  .map(d => {
                    const eff = d.state === 'ACTIVE_HEALTHY' && d.connection_state && d.connection_state !== 'ACTIVE' ? d.connection_state : d.state;
                    return {value: d.id, label: `${formatDeploymentLabel(d)} (${d.data_region} · ${formatOperationalState(eff)})`};
                  }),
              ]}
            />
          </div>
          <Button
            variant="secondary"
            disabled={!selectedDep || busy}
            onClick={() => {
              const next = [...targets, {deployment_id: selectedDep, priority_order: targets.length + 1}];
              setTargets(isWeighted(strategy) ? distributeWeights(next) : next);
              setSelectedDep('');
            }}
          >
            {t('routing.addTarget')}
          </Button>
        </div>

        {targets.length > 0 ? (
          <ul className="tp-list">
            {targets.map((target, index) => {
              const dep = deploymentById.get(target.deployment_id);
              const effState = dep
                ? (dep.state === 'ACTIVE_HEALTHY' && dep.connection_state && dep.connection_state !== 'ACTIVE' ? dep.connection_state : dep.state)
                : '';
              return (
                <li key={target.deployment_id} className="tp-list-item">
                  <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 160px'}}>
                    <span className="mono" style={{fontWeight: 600}}>#{index + 1} {dep ? formatDeploymentLabel(dep) : target.deployment_id}</span>
                    <span className="tp-muted">{dep ? `${dep.data_region} · ${formatOperationalState(effState)}` : t('routing.unavailableDeployment')}</span>
                  </div>
                  <div className="tp-row">
                    {isWeighted(strategy) ? (
                      <div style={{width: 96}}>
                        <TextField
                          label={t('routing.weightFor', {id: target.deployment_id})}
                          type="number"
                          min={0}
                          max={100}
                          value={target.weight ?? 0}
                          onChange={event => {
                            const val = Number(event.target.value) || 0;
                            setTargets(curr => curr.map((item, idx) => (idx === index ? {...item, weight: val} : item)));
                          }}
                        />
                      </div>
                    ) : null}
                    <Button variant="ghost" size="sm" disabled={index === 0} aria-label={t('routing.moveUp', {id: target.deployment_id})} onClick={() => setTargets(moveItem(targets, index, -1))}>
                      <ArrowUp size={14} aria-hidden="true" />
                    </Button>
                    <Button variant="ghost" size="sm" disabled={index === targets.length - 1} aria-label={t('routing.moveDown', {id: target.deployment_id})} onClick={() => setTargets(moveItem(targets, index, 1))}>
                      <ArrowDown size={14} aria-hidden="true" />
                    </Button>
                    <Button
                      variant="danger-outline"
                      size="sm"
                      aria-label={t('routing.removeTarget', {id: target.deployment_id})}
                      onClick={() => {
                        const rem = targets.filter(tg => tg.deployment_id !== target.deployment_id);
                        setTargets(isWeighted(strategy) && rem.length > 0 ? distributeWeights(rem) : rem);
                      }}
                    >
                      <Trash2 size={14} aria-hidden="true" />
                    </Button>
                  </div>
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="tp-muted">{t('routing.addAtLeastOneTarget')}</p>
        )}

        {isWeighted(strategy) && targets.length > 0 && totalWeight !== 100 ? (
          <Banner tone="danger">{t('routing.errWeightSum', {total: formatNumber(totalWeight)})}</Banner>
        ) : null}
        {routeHasInvalidRegion ? (
          <Banner tone="danger">{t('routing.errInvalidRegion')}</Banner>
        ) : null}
        {routeMissingToolCalling ? (
          <Banner tone="warn">{t('routing.warnToolCalling')}</Banner>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('routing.deleteRouteTitle', {slug: deleteTarget?.slug ?? ''})}
        description={t('routing.deleteRouteDesc', {count: formatNumber(trafficByRoute.get(deleteTarget?.slug ?? '') ?? 0)})}
        confirmLabel={t('routing.action.delete')}
        requireMatch={deleteTarget?.slug}
        matchLabel={t('routing.deleteRouteMatch')}
        busy={busy}
        onConfirm={() => void confirmDeleteRoute()}
      />

      <ConfirmDialog
        open={confirmGuardrailOpen}
        onClose={() => setConfirmGuardrailOpen(false)}
        title={t('routing.confirmGuardrailTitle')}
        description={t('routing.confirmGuardrailDesc')}
        confirmLabel={t('routing.confirmAndSavePolicy')}
        busy={busy}
        onConfirm={() => {
          setConfirmGuardrailOpen(false);
          void savePolicy(true);
        }}
      />

      <Modal
        open={conflict !== null}
        onClose={() => setConflict(null)}
        size="lg"
        title={t('routing.occTitle')}
        subtitle={t('routing.occSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setConflict(null)}>{t('common.close')}</Button>
            <Button variant="primary" onClick={applyConflictVersion}>{t('routing.adoptLatest')}</Button>
          </>
        }
      >
        {conflict ? (
          <div className="tp-grid-2">
            <CodeBlock title={t('routing.yourDraft')} code={JSON.stringify(conflict.kind === 'route' ? routePayload() : policyDraft, null, 2)} />
            <CodeBlock title={t('routing.latestServer')} code={JSON.stringify(conflict.current ?? {}, null, 2)} />
          </div>
        ) : null}
      </Modal>
    </div>
  );
}

function isWeighted(strategy: Strategy) { return strategy === 'WEIGHTED' || strategy === 'WEIGHTED_ROUND_ROBIN'; }
function isConflict(error: unknown) { return Boolean(error && typeof error === 'object' && 'status' in error && error.status === 409); }
function currentResource(error: unknown): unknown { return error && typeof error === 'object' && 'currentResource' in error ? error.currentResource : null; }
function errText(cause: unknown, t: (key: string) => string): string { return cause instanceof Error ? cause.message : t('common.requestFailed'); }
function isSubset(values: string[], allowed: string[]) { return allowed.includes('*') || (!values.includes('*') && values.every(v => allowed.includes(v))); }
function sameSet(a: string[], b: string[]) { return a.length === b.length && a.every(v => b.includes(v)); }
function isRestricted(values: string[] | undefined) { return Boolean(values?.length && !values.includes('*')); }

function distributeWeights(targets: Target[]) {
  const base = Math.floor(100 / targets.length);
  let rem = 100 - base * targets.length;
  return targets.map(tg => ({...tg, weight: base + (rem-- > 0 ? 1 : 0)}));
}

function moveItem<T>(list: T[], index: number, delta: number): T[] {
  const target = index + delta;
  if (target < 0 || target >= list.length) return list;
  const copy = [...list];
  [copy[index], copy[target]] = [copy[target], copy[index]];
  return copy;
}

type TranslateFn = (key: string, vars?: Record<string, string | number>) => string;

function regionSource(org: Policy | null, proj: Policy | null, curr: Policy, t: TranslateFn, scopeLabel: (s: PolicyScope) => string) {
  if (org && isRestricted(org.allowed_data_regions) && sameSet(org.allowed_data_regions ?? [], curr.allowed_data_regions ?? [])) return t('routing.source.orgFloor');
  if (proj && isRestricted(proj.allowed_data_regions) && sameSet(proj.allowed_data_regions ?? [], curr.allowed_data_regions ?? [])) return t('routing.source.projectFloor');
  return t('routing.source.scopePolicy', {scope: scopeLabel(curr.scope_tier as PolicyScope)});
}

function modelSource(org: Policy | null, proj: Policy | null, curr: Policy, t: TranslateFn, scopeLabel: (s: PolicyScope) => string) {
  if (org && isRestricted(org.allowed_models) && sameSet(org.allowed_models ?? ['*'], curr.allowed_models ?? ['*'])) return t('routing.source.orgFloor');
  if (proj && isRestricted(proj.allowed_models) && sameSet(proj.allowed_models ?? ['*'], curr.allowed_models ?? ['*'])) return t('routing.source.projectFloor');
  return t('routing.source.scopePolicy', {scope: scopeLabel(curr.scope_tier as PolicyScope)});
}

function scalarSource(org: Policy | null, proj: Policy | null, curr: Policy, field: keyof Policy, t: TranslateFn, scopeLabel: (s: PolicyScope) => string) {
  if (org && org[field] != null && org[field] === curr[field]) return t('routing.source.orgFloor');
  if (proj && proj[field] != null && proj[field] === curr[field]) return t('routing.source.projectFloor');
  return t('routing.source.scopePolicy', {scope: scopeLabel(curr.scope_tier as PolicyScope)});
}

function hookSource(org: Policy | null, proj: Policy | null, curr: Policy, t: TranslateFn, scopeLabel: (s: PolicyScope) => string) {
  const ids = new Set((curr.guardrail_hooks ?? []).map(h => h.extension_id));
  if ([...ids].some(id => (org?.guardrail_hooks ?? []).some(h => h.extension_id === id))) return t('routing.source.orgInherited');
  if ([...ids].some(id => (proj?.guardrail_hooks ?? []).some(h => h.extension_id === id))) return t('routing.source.projectInherited');
  return t('routing.source.scopePolicy', {scope: scopeLabel(curr.scope_tier as PolicyScope)});
}
