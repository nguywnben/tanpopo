import {useCallback, useEffect, useMemo, useState} from 'react';
import {ArrowDown, ArrowUp, Play, Plus, Trash2} from 'lucide-react';
import {listAdminModels, listDeployments} from '../../api/catalogClient';
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
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {ConfirmDialog, Drawer, Modal} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
  CodeBlock,
  KeyValueList,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
  stateTone,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type Route = components['schemas']['RouteResource'];
type Deployment = components['schemas']['ModelDeployment'];
type LogicalModel = components['schemas']['LogicalModel'];
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
const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

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
  const activeTab = pathname.startsWith('/policies')
    ? 'policies'
    : pathname.startsWith('/routes/explain')
      ? 'explain'
      : 'routes';

  const [routes, setRoutes] = useState<Route[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [models, setModels] = useState<LogicalModel[]>([]);
  const [traces, setTraces] = useState<components['schemas']['TraceSummary'][]>([]);

  // Policies hierarchy
  const [orgPolicy, setOrgPolicy] = useState<Policy | null>(null);
  const [projPolicy, setProjPolicy] = useState<Policy | null>(null);
  const [envPolicy, setEnvPolicy] = useState<Policy | null>(null);
  const [etags, setEtags] = useState<Record<PolicyScope, string>>({ORGANIZATION: '', PROJECT: '', ENVIRONMENT: ''});
  const [editScope, setEditScope] = useState<PolicyScope>('ENVIRONMENT');
  const [policyDraft, setPolicyDraft] = useState<Policy | null>(null);
  const [hookExtId, setHookExtId] = useState('');
  const [hookPosture, setHookPosture] = useState<'FAIL_CLOSED' | 'FAIL_OPEN'>('FAIL_CLOSED');
  const [confirmGuardrailOpen, setConfirmGuardrailOpen] = useState(false);

  // Route editor drawer
  const [routeDrawerOpen, setRouteDrawerOpen] = useState(false);
  const [editingRoute, setEditingRoute] = useState<Route | null>(null);
  const [slug, setSlug] = useState('');
  const [strategy, setStrategy] = useState<Strategy>('PRIORITY_FAILOVER');
  const [maxHops, setMaxHops] = useState(3);
  const [timeoutMs, setTimeoutMs] = useState(60000);
  const [targets, setTargets] = useState<Target[]>([]);
  const [selectedDep, setSelectedDep] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Route | null>(null);

  // Explain simulator
  const [explainModel, setExplainModel] = useState('');
  const [explainRegion, setExplainRegion] = useState<DataRegion | ''>('');
  const [explainCaps, setExplainCaps] = useState<string[]>([]);
  const [explainResult, setExplainResult] = useState<components['schemas']['RouteExplainResponse'] | null>(null);
  const [explainBusy, setExplainBusy] = useState(false);

  // OCC conflict modal
  const [conflict, setConflict] = useState<{kind: 'route' | 'policy'; message: string; current: unknown} | null>(null);

  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const canManage = roles.some(role => manageRoles.includes(role));

  const refresh = useCallback(async () => {
    if (!projectId || !environmentId) {
      setRoutes([]);
      setDeployments([]);
      setEnvPolicy(null);
      setPolicyDraft(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    try {
      const [routeRows, depRows, modelRows, traceRows, orgRes, projRes, envRes] = await Promise.all([
        listRoutesForEnvironment(environmentId),
        listDeployments(environmentId),
        listAdminModels(),
        listRecentTraces(environmentId),
        getEffectivePolicy(),
        getEffectivePolicy(projectId),
        getEffectivePolicy(projectId, environmentId),
      ]);
      setRoutes(routeRows);
      setDeployments(depRows);
      setModels(modelRows);
      setTraces(traceRows);
      setOrgPolicy(orgRes.policy);
      setProjPolicy(projRes.policy);
      setEnvPolicy(envRes.policy);
      setEtags({ORGANIZATION: orgRes.etag, PROJECT: projRes.etag, ENVIRONMENT: envRes.etag});
      const activePol = editScope === 'ORGANIZATION' ? orgRes.policy : editScope === 'PROJECT' ? projRes.policy : envRes.policy;
      setPolicyDraft(activePol);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setLoading(false);
    }
  }, [projectId, environmentId, editScope]);

  useEffect(() => { void refresh(); }, [refresh]);

  const deploymentById = useMemo(() => new Map(deployments.map(d => [d.id, d])), [deployments]);
  const modelById = useMemo(() => new Map(models.map(m => [m.id, m])), [models]);
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
    setRouteDrawerOpen(true);
  }

  function openEditRoute(route: Route) {
    setEditingRoute(route);
    setSlug(route.slug);
    setStrategy(route.strategy);
    setMaxHops(route.max_failover_hops);
    setTimeoutMs(route.request_timeout_ms);
    setTargets(route.targets.map(t => ({...t})));
    setSelectedDep('');
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
      targets: targets.map((t, index) => ({...t, priority_order: index + 1})),
    };
  }

  const totalWeight = targets.reduce((sum, t) => sum + (t.weight ?? 0), 0);
  const routeHasInvalidRegion = targets.some(t => {
    const dep = deploymentById.get(t.deployment_id);
    return !dep || (allowedRegions.length > 0 && !allowedRegions.includes(dep.data_region));
  });
  const routeMissingToolCalling = targets.some(t => {
    const dep = deploymentById.get(t.deployment_id);
    return modelById.get(dep?.logical_model_id ?? '')?.capabilities.tool_calling !== true;
  });

  async function saveRoute() {
    if (!canManage || !slug.trim() || targets.length === 0 || (isWeighted(strategy) && totalWeight !== 100) || routeHasInvalidRegion) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      if (editingRoute) await updateRoute(editingRoute, routePayload());
      else await createRoute(routePayload());
      setRouteDrawerOpen(false);
      await refresh();
      setNotice(editingRoute ? `Updated route "${slug.trim()}".` : `Created route "${slug.trim()}".`);
    } catch (cause) {
      setError(errText(cause));
      if (isConflict(cause)) setConflict({kind: 'route', message: errText(cause), current: currentResource(cause)});
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
      setNotice('Route deleted.');
    } catch (cause) {
      setDeleteTarget(null);
      setError(errText(cause));
      if (isConflict(cause)) setConflict({kind: 'route', message: errText(cause), current: currentResource(cause)});
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
      const result = await explainRouteDecision({
        env_id: environmentId,
        target_model: explainModel.trim(),
        data_region: explainRegion as DataRegion,
        dialect: 'openai_compatible',
        required_capabilities: explainCaps,
        estimated_input_tokens: 512,
        max_output_tokens: 1024,
      });
      if (!result.dry_run) throw new Error('Route explain did not confirm dry-run mode.');
      setExplainResult(result);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setExplainBusy(false);
    }
  }

  // Policy editing helpers
  const selectedPolicy = editScope === 'ORGANIZATION' ? orgPolicy : editScope === 'PROJECT' ? projPolicy : envPolicy;
  const parentPolicy = editScope === 'ENVIRONMENT' ? projPolicy : editScope === 'PROJECT' ? orgPolicy : null;
  const canEditPolicy = Boolean(selectedPolicy && (editScope === 'ORGANIZATION' ? roles.includes('ORG_OWNER') : canManage));
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
      setNotice(`Saved ${editScope.toLowerCase()} policy.`);
    } catch (cause) {
      setError(errText(cause));
      if (isConflict(cause)) setConflict({kind: 'policy', message: errText(cause), current: currentResource(cause)});
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

  const policyRows = envPolicy
    ? [
        {id: 'regions', rule: 'Allowed data regions', value: (envPolicy.allowed_data_regions ?? []).join(', ') || 'None configured', source: regionSource(orgPolicy, projPolicy, envPolicy)},
        {id: 'models', rule: 'Allowed models', value: (envPolicy.allowed_models ?? ['*']).join(', ') || 'Deny all', source: modelSource(orgPolicy, projPolicy, envPolicy)},
        {id: 'trust', rule: 'Minimum plugin trust', value: envPolicy.min_plugin_trust, source: scalarSource(orgPolicy, projPolicy, envPolicy, 'min_plugin_trust')},
        {id: 'assurance', rule: 'Minimum capability assurance', value: envPolicy.min_capability_assurance, source: scalarSource(orgPolicy, projPolicy, envPolicy, 'min_capability_assurance')},
        {id: 'rpm', rule: 'RPM ceiling', value: envPolicy.rpm_ceiling ?? 'Unspecified', source: scalarSource(orgPolicy, projPolicy, envPolicy, 'rpm_ceiling')},
        {id: 'tpm', rule: 'TPM ceiling', value: envPolicy.tpm_ceiling ?? 'Unspecified', source: scalarSource(orgPolicy, projPolicy, envPolicy, 'tpm_ceiling')},
        {id: 'streams', rule: 'Concurrent streams ceiling', value: envPolicy.max_concurrent_streams ?? 'Unspecified', source: scalarSource(orgPolicy, projPolicy, envPolicy, 'max_concurrent_streams')},
        {
          id: 'hooks',
          rule: 'Guardrail hooks',
          value: (envPolicy.guardrail_hooks ?? []).map(h => `${h.extension_id} (${h.failure_posture})`).join(', ') || 'None attached',
          source: hookSource(orgPolicy, projPolicy, envPolicy),
        },
      ]
    : [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Routes & Policies</h1>
          <p className="tp-page-subtitle">Deterministic capability routing, dry-run simulation, and monotonic policy inheritance.</p>
        </div>
        {canManage && activeTab === 'routes' ? (
          <div className="tp-page-actions">
            <Button variant="primary" size="sm" disabled={!environmentId} onClick={openCreateRoute}>
              <Plus size={14} aria-hidden="true" />
              <span>Create route</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label="Routing and policy views"
        activeId={activeTab}
        onSelect={id => {
          if (id === 'explain') navigate('/routes/explain');
          else if (id === 'policies') navigate('/policies');
          else navigate('/routes');
        }}
        items={[
          {id: 'routes', label: 'Routes', count: routes.length},
          {id: 'explain', label: 'Route Explain'},
          {id: 'policies', label: 'Policies'},
        ]}
      />

      {!environmentId ? <Banner tone="info">Select a project and environment in the top bar to manage routes and policies.</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'routes' ? (
        <section className="tp-section">
          {allowedRegions.length > 0 ? (
            <p className="tp-muted">Inherited data residency ceiling: <span className="mono">{allowedRegions.join(', ')}</span></p>
          ) : null}
          <SectionHeader title="Configured routes" count={routes.length} />
          <DataTable
            ariaLabel="Configured routes"
            loading={loading}
            rows={routes}
            rowKey={row => row.id}
            emptyTitle="No routes in this environment"
            emptyDescription="Create a route to map model slugs to ordered or weighted deployments."
            emptyAction={canManage && environmentId ? <Button variant="primary" size="sm" onClick={openCreateRoute}>Create route</Button> : undefined}
            columns={[
              {id: 'slug', header: 'Route slug', primary: true, width: '20%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.slug}</span>},
              {id: 'strategy', header: 'Strategy', width: '16%', render: row => <span>{row.strategy.replaceAll('_', ' ')}</span>},
              {
                id: 'fallbacks',
                header: 'Fallback chain',
                width: '26%',
                render: row => (
                  <span className="mono">
                    {row.targets.map(t => {
                      const name = deploymentById.get(t.deployment_id)?.upstream_model_name ?? t.deployment_id;
                      return t.weight !== undefined ? `${name} (${t.weight}%)` : name;
                    }).join(' → ')}
                  </span>
                ),
              },
              {
                id: 'health',
                header: 'Target health',
                width: '20%',
                render: row => (
                  <div className="tp-row">
                    {row.targets.map(t => {
                      const dep = deploymentById.get(t.deployment_id);
                      const label = dep?.state ?? 'UNAVAILABLE';
                      return <StatusPill key={t.deployment_id} tone={stateTone(label)} label={label.replaceAll('_', ' ')} />;
                    })}
                  </div>
                ),
              },
              {
                id: 'actions',
                header: 'Actions',
                width: '18%',
                render: row =>
                  canManage ? (
                    <div className="tp-row">
                      <Button variant="secondary" size="sm" onClick={() => openEditRoute(row)}>Edit</Button>
                      <Button variant="danger-outline" size="sm" onClick={() => setDeleteTarget(row)}>Delete</Button>
                    </div>
                  ) : null,
              },
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'explain' ? (
        <section className="tp-section">
          <div className="tp-surface tp-surface-pad tp-stack">
            <h2>Dry-run route simulator</h2>
            <p className="tp-muted">Evaluates live routing rules without upstream egress or budget holds.</p>
            <div className="tp-grid-2">
              <TextField
                label="Target model or route slug"
                placeholder="e.g. gpt-4o-mini or primary-chat"
                value={explainModel}
                disabled={explainBusy}
                onChange={event => setExplainModel(event.target.value)}
              />
              <SelectField
                label="Data residency"
                value={explainRegion}
                disabled={explainBusy}
                onChange={event => setExplainRegion(event.target.value as DataRegion | '')}
              >
                <option value="">Choose region</option>
                {explainRegionChoices.map(reg => <option key={reg} value={reg}>{reg}</option>)}
              </SelectField>
            </div>
            <div className="tp-row">
              {(['tools', 'vision', 'context_tokens'] as const).map(cap => (
                <CheckboxField
                  key={cap}
                  label={cap}
                  checked={explainCaps.includes(cap)}
                  onChange={checked => setExplainCaps(curr => (checked ? [...curr, cap] : curr.filter(item => item !== cap)))}
                />
              ))}
            </div>
            <div>
              <Button
                variant="primary"
                loading={explainBusy}
                disabled={!environmentId || !explainModel.trim() || !explainRegionAllowed}
                onClick={() => void runExplain()}
              >
                <Play size={14} aria-hidden="true" />
                <span>{explainBusy ? 'Simulating…' : 'Explain route'}</span>
              </Button>
            </div>
          </div>

          {explainResult ? (
            <div className="tp-surface tp-surface-pad tp-stack" role="status">
              <SectionHeader title="Dry-run decision" />
              <KeyValueList
                items={[
                  {label: 'Dry-run verified', value: <StatusPill tone="ok" label={explainResult.dry_run ? 'DRY RUN' : 'UNKNOWN'} />},
                  {label: 'Matched route', value: explainResult.route_slug || '—', mono: true},
                  {label: 'Eligible candidates', value: String(explainResult.selected_candidates_ordered.length), mono: true},
                  {label: 'Rejected deployments', value: String(explainResult.rejected_deployments.length), mono: true},
                ]}
              />
              <CodeBlock title="Detailed explain output" code={JSON.stringify(explainResult, null, 2)} />
            </div>
          ) : null}
        </section>
      ) : null}

      {activeTab === 'policies' && envPolicy && policyDraft ? (
        <div className="tp-stack">
          <section className="tp-section">
            <SectionHeader title="Effective resolved policy" />
            <DataTable
              ariaLabel="Effective policy hierarchy"
              rows={policyRows}
              rowKey={row => row.id}
              columns={[
                {id: 'rule', header: 'Constraint', primary: true, width: '30%', render: row => <span style={{fontWeight: 600}}>{row.rule}</span>},
                {id: 'val', header: 'Effective value', width: '35%', render: row => <span className="mono">{String(row.value)}</span>},
                {id: 'src', header: 'Provenance', width: '35%', render: row => <span className="tp-muted">{row.source}</span>},
              ]}
            />
          </section>

          <section className="tp-section">
            <SectionHeader
              title="Edit policy scope"
              actions={
                <SelectField
                  label="Scope to edit"
                  value={editScope}
                  onChange={event => {
                    const next = event.target.value as PolicyScope;
                    setEditScope(next);
                    const nextPol = next === 'ORGANIZATION' ? orgPolicy : next === 'PROJECT' ? projPolicy : envPolicy;
                    if (nextPol) setPolicyDraft(nextPol);
                  }}
                >
                  <option value="ORGANIZATION">Organization</option>
                  <option value="PROJECT">Project</option>
                  <option value="ENVIRONMENT">Environment</option>
                </SelectField>
              }
            />

            <div className="tp-surface tp-surface-pad tp-stack">
              {!canEditPolicy ? (
                <Banner tone="info">This scope is read-only for your role. Child policies can only tighten inherited parent bounds.</Banner>
              ) : null}

              <div className="tp-stack-sm">
                <span className="tp-label">Allowed data regions</span>
                <div className="tp-row">
                  {parentRegions.map(reg => {
                    const checked = (policyDraft.allowed_data_regions ?? []).includes(reg);
                    return (
                      <CheckboxField
                        key={reg}
                        label={reg}
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
                label="Allowed model slugs (comma-separated, * for all)"
                value={(policyDraft.allowed_models ?? ['*']).join(', ')}
                disabled={!canEditPolicy || busy}
                error={modelRelaxesParent ? 'Cannot widen inherited parent model allowlist.' : undefined}
                onChange={event => {
                  const list = event.target.value.split(',').map(v => v.trim()).filter(Boolean);
                  setPolicyDraft({...policyDraft, allowed_models: list});
                }}
              />

              <div className="tp-grid-2">
                <SelectField
                  label="Minimum plugin trust"
                  value={policyDraft.min_plugin_trust}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, min_plugin_trust: event.target.value as Policy['min_plugin_trust']})}
                >
                  {trustLevels.filter(level => trustRank[level] >= trustRank[parentTrust]).map(level => (
                    <option key={level} value={level}>{level.replaceAll('_', ' ')}</option>
                  ))}
                </SelectField>
                <SelectField
                  label="Minimum capability assurance"
                  value={policyDraft.min_capability_assurance}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, min_capability_assurance: event.target.value as Policy['min_capability_assurance']})}
                >
                  {(['VERIFIED', 'TESTED', 'DECLARED'] as const)
                    .filter(level => assuranceRank[level] >= assuranceRank[parentAssurance])
                    .map(level => <option key={level} value={level}>{level}</option>)}
                </SelectField>
              </div>

              <div className="tp-grid-3">
                <TextField
                  label="RPM ceiling"
                  type="number"
                  min={0}
                  max={parentPolicy?.rpm_ceiling ?? undefined}
                  value={policyDraft.rpm_ceiling ?? ''}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, rpm_ceiling: event.target.value === '' ? null : Number(event.target.value)})}
                />
                <TextField
                  label="TPM ceiling"
                  type="number"
                  min={0}
                  max={parentPolicy?.tpm_ceiling ?? undefined}
                  value={policyDraft.tpm_ceiling ?? ''}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, tpm_ceiling: event.target.value === '' ? null : Number(event.target.value)})}
                />
                <TextField
                  label="Concurrent streams ceiling"
                  type="number"
                  min={0}
                  max={parentPolicy?.max_concurrent_streams ?? undefined}
                  value={policyDraft.max_concurrent_streams ?? ''}
                  disabled={!canEditPolicy || busy}
                  onChange={event => setPolicyDraft({...policyDraft, max_concurrent_streams: event.target.value === '' ? null : Number(event.target.value)})}
                />
              </div>

              <div className="tp-stack-sm">
                <h3>Guardrail extensions</h3>
                {(policyDraft.guardrail_hooks ?? []).length > 0 ? (
                  <ul className="tp-list">
                    {(policyDraft.guardrail_hooks ?? []).map(hook => {
                      const lockedByParent = Boolean(parentPolicy?.guardrail_hooks?.some(h => h.extension_id === hook.extension_id));
                      return (
                        <li key={hook.extension_id} className="tp-list-item">
                          <div className="tp-row">
                            <span className="mono">{hook.extension_id}</span>
                            <StatusPill tone={stateTone(hook.failure_posture)} label={hook.failure_posture} />
                          </div>
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={!canEditPolicy || busy || lockedByParent}
                            onClick={() => setPolicyDraft({...policyDraft, guardrail_hooks: (policyDraft.guardrail_hooks ?? []).filter(h => h.extension_id !== hook.extension_id)})}
                          >
                            Remove
                          </Button>
                        </li>
                      );
                    })}
                  </ul>
                ) : (
                  <p className="tp-muted">No guardrail extensions attached at this scope.</p>
                )}

                {canEditPolicy ? (
                  <div className="tp-toolbar">
                    <div className="tp-toolbar-field">
                      <TextField label="Guardrail extension ID" value={hookExtId} disabled={busy} onChange={event => setHookExtId(event.target.value)} />
                    </div>
                    <div className="tp-toolbar-field">
                      <SelectField
                        label="Failure posture"
                        value={hookPosture}
                        disabled={busy}
                        error={hookRelaxesParent ? 'Parent guardrail is FAIL_CLOSED and cannot be relaxed.' : undefined}
                        onChange={event => setHookPosture(event.target.value as 'FAIL_CLOSED' | 'FAIL_OPEN')}
                      >
                        <option value="FAIL_CLOSED">FAIL_CLOSED</option>
                        <option value="FAIL_OPEN">FAIL_OPEN</option>
                      </SelectField>
                    </div>
                    <Button
                      variant="secondary"
                      disabled={busy || !hookExtId.trim() || hookRelaxesParent || (policyDraft.guardrail_hooks ?? []).some(h => h.extension_id === hookExtId.trim())}
                      onClick={() => {
                        setPolicyDraft({
                          ...policyDraft,
                          guardrail_hooks: [...(policyDraft.guardrail_hooks ?? []), {extension_id: hookExtId.trim(), failure_posture: hookPosture}],
                        });
                        setHookExtId('');
                      }}
                    >
                      Attach guardrail
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
                  Discard changes
                </Button>
                <Button
                  variant="primary"
                  loading={busy}
                  disabled={!canEditPolicy || modelRelaxesParent || JSON.stringify(selectedPolicy) === JSON.stringify(policyDraft)}
                  onClick={() => void savePolicy()}
                >
                  Save policy
                </Button>
              </div>
            </div>
          </section>
        </div>
      ) : null}

      <Drawer
        open={routeDrawerOpen}
        onClose={() => setRouteDrawerOpen(false)}
        size="lg"
        title={editingRoute ? `Edit route ${editingRoute.slug}` : 'Create route'}
        subtitle="Configure strategy, failover limits, and ordered deployment targets."
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setRouteDrawerOpen(false)}>Cancel</Button>
            <Button
              variant="primary"
              loading={busy}
              disabled={!canManage || !slug.trim() || targets.length === 0 || (isWeighted(strategy) && totalWeight !== 100) || routeHasInvalidRegion}
              onClick={() => void saveRoute()}
            >
              {editingRoute ? 'Save route' : 'Create route'}
            </Button>
          </>
        }
      >
        <TextField label="Route slug" value={slug} disabled={busy} onChange={event => setSlug(event.target.value)} />
        <SelectField
          label="Routing strategy"
          value={strategy}
          disabled={busy}
          onChange={event => {
            const next = event.target.value as Strategy;
            setStrategy(next);
            if (isWeighted(next) && targets.length > 0) setTargets(distributeWeights(targets.map(t => ({...t}))));
            if (!isWeighted(next)) setTargets(targets.map(({weight: _w, ...rest}) => rest));
          }}
        >
          {strategies.map(item => <option key={item} value={item}>{item.replaceAll('_', ' ')}</option>)}
        </SelectField>
        <div className="tp-grid-2">
          <TextField label="Max failover hops (1–3)" type="number" min={1} max={3} value={maxHops} disabled={busy} onChange={event => setMaxHops(Number(event.target.value) || 1)} />
          <TextField label="Request timeout (ms)" type="number" min={500} max={300000} value={timeoutMs} disabled={busy} onChange={event => setTimeoutMs(Number(event.target.value) || 60000)} />
        </div>

        <div className="tp-toolbar">
          <div className="tp-toolbar-field">
            <SelectField label="Add deployment target" value={selectedDep} disabled={busy} onChange={event => setSelectedDep(event.target.value)}>
              <option value="">Select deployment</option>
              {deployments.filter(d => !targets.some(t => t.deployment_id === d.id)).map(d => (
                <option key={d.id} value={d.id}>{d.upstream_model_name} ({d.data_region} · {d.state})</option>
              ))}
            </SelectField>
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
            Add target
          </Button>
        </div>

        {targets.length > 0 ? (
          <ul className="tp-list">
            {targets.map((target, index) => {
              const dep = deploymentById.get(target.deployment_id);
              return (
                <li key={target.deployment_id} className="tp-list-item">
                  <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 160px'}}>
                    <span className="mono" style={{fontWeight: 600}}>#{index + 1} {dep?.upstream_model_name ?? target.deployment_id}</span>
                    <span className="tp-muted">{dep ? `${dep.data_region} · ${dep.state}` : 'Unavailable deployment'}</span>
                  </div>
                  <div className="tp-row">
                    {isWeighted(strategy) ? (
                      <div style={{width: 96}}>
                        <TextField
                          label={`Weight % for ${target.deployment_id}`}
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
                    <Button variant="ghost" size="sm" disabled={index === 0} aria-label={`Move ${target.deployment_id} up`} onClick={() => setTargets(moveItem(targets, index, -1))}>
                      <ArrowUp size={14} aria-hidden="true" />
                    </Button>
                    <Button variant="ghost" size="sm" disabled={index === targets.length - 1} aria-label={`Move ${target.deployment_id} down`} onClick={() => setTargets(moveItem(targets, index, 1))}>
                      <ArrowDown size={14} aria-hidden="true" />
                    </Button>
                    <Button
                      variant="danger-outline"
                      size="sm"
                      aria-label={`Remove ${target.deployment_id}`}
                      onClick={() => {
                        const rem = targets.filter(t => t.deployment_id !== target.deployment_id);
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
          <p className="tp-muted">Add at least one deployment target before saving.</p>
        )}

        {isWeighted(strategy) && targets.length > 0 && totalWeight !== 100 ? (
          <Banner tone="danger">Target weights total {totalWeight}%. Weighted strategies require an exact 100% sum.</Banner>
        ) : null}
        {routeHasInvalidRegion ? (
          <Banner tone="danger">One or more targets are unavailable or violate the inherited data residency policy.</Banner>
        ) : null}
        {routeMissingToolCalling ? (
          <Banner tone="warn">At least one fallback target does not declare tool_calling capability.</Banner>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={`Delete route ${deleteTarget?.slug ?? ''}`}
        description={`Removing this route affects ${trafficByRoute.get(deleteTarget?.slug ?? '') ?? 0} trace-recorded requests in the last hour. Active streams are not terminated.`}
        confirmLabel="Delete route"
        requireMatch={deleteTarget?.slug}
        matchLabel="Type the route slug to confirm"
        busy={busy}
        onConfirm={() => void confirmDeleteRoute()}
      />

      <ConfirmDialog
        open={confirmGuardrailOpen}
        onClose={() => setConfirmGuardrailOpen(false)}
        title="Confirm blocking guardrail"
        description="Attaching a FAIL_CLOSED guardrail will reject traffic if that extension is unavailable."
        confirmLabel="Confirm and save policy"
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
        title="Optimistic concurrency conflict (409)"
        subtitle="Another administrator modified this resource. Your draft is preserved."
        footer={
          <>
            <Button variant="secondary" onClick={() => setConflict(null)}>Close</Button>
            <Button variant="primary" onClick={applyConflictVersion}>Adopt latest version & keep draft</Button>
          </>
        }
      >
        {conflict ? (
          <div className="tp-grid-2">
            <CodeBlock title="Your draft" code={JSON.stringify(conflict.kind === 'route' ? routePayload() : policyDraft, null, 2)} />
            <CodeBlock title="Latest server version" code={JSON.stringify(conflict.current ?? {}, null, 2)} />
          </div>
        ) : null}
      </Modal>
    </div>
  );
}

function isWeighted(strategy: Strategy) { return strategy === 'WEIGHTED' || strategy === 'WEIGHTED_ROUND_ROBIN'; }
function isConflict(error: unknown) { return Boolean(error && typeof error === 'object' && 'status' in error && error.status === 409); }
function currentResource(error: unknown): unknown { return error && typeof error === 'object' && 'currentResource' in error ? error.currentResource : null; }
function errText(cause: unknown): string { return cause instanceof Error ? cause.message : 'Request failed.'; }
function isSubset(values: string[], allowed: string[]) { return allowed.includes('*') || (!values.includes('*') && values.every(v => allowed.includes(v))); }
function sameSet(a: string[], b: string[]) { return a.length === b.length && a.every(v => b.includes(v)); }
function isRestricted(values: string[] | undefined) { return Boolean(values?.length && !values.includes('*')); }

function distributeWeights(targets: Target[]) {
  const base = Math.floor(100 / targets.length);
  let rem = 100 - base * targets.length;
  return targets.map(t => ({...t, weight: base + (rem-- > 0 ? 1 : 0)}));
}

function moveItem<T>(list: T[], index: number, delta: number): T[] {
  const target = index + delta;
  if (target < 0 || target >= list.length) return list;
  const copy = [...list];
  [copy[index], copy[target]] = [copy[target], copy[index]];
  return copy;
}

function regionSource(org: Policy | null, proj: Policy | null, curr: Policy) {
  if (org && isRestricted(org.allowed_data_regions) && sameSet(org.allowed_data_regions ?? [], curr.allowed_data_regions ?? [])) return 'Organization floor';
  if (proj && isRestricted(proj.allowed_data_regions) && sameSet(proj.allowed_data_regions ?? [], curr.allowed_data_regions ?? [])) return 'Project floor';
  return `${curr.scope_tier} policy`;
}

function modelSource(org: Policy | null, proj: Policy | null, curr: Policy) {
  if (org && isRestricted(org.allowed_models) && sameSet(org.allowed_models ?? ['*'], curr.allowed_models ?? ['*'])) return 'Organization floor';
  if (proj && isRestricted(proj.allowed_models) && sameSet(proj.allowed_models ?? ['*'], curr.allowed_models ?? ['*'])) return 'Project floor';
  return `${curr.scope_tier} policy`;
}

function scalarSource(org: Policy | null, proj: Policy | null, curr: Policy, field: keyof Policy) {
  if (org && org[field] != null && org[field] === curr[field]) return 'Organization floor';
  if (proj && proj[field] != null && proj[field] === curr[field]) return 'Project floor';
  return `${curr.scope_tier} policy`;
}

function hookSource(org: Policy | null, proj: Policy | null, curr: Policy) {
  const ids = new Set((curr.guardrail_hooks ?? []).map(h => h.extension_id));
  if ([...ids].some(id => (org?.guardrail_hooks ?? []).some(h => h.extension_id === id))) return 'Organization / inherited';
  if ([...ids].some(id => (proj?.guardrail_hooks ?? []).some(h => h.extension_id === id))) return 'Project / inherited';
  return `${curr.scope_tier} policy`;
}
