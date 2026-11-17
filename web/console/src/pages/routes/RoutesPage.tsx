import {useCallback, useEffect, useMemo, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Heading} from '@astryxdesign/core/Heading';
import {NumberInput} from '@astryxdesign/core/NumberInput';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Token} from '@astryxdesign/core/Token';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {listAdminModels, listDeployments} from '../../api/catalogClient';
import {createRoute, deleteRoute, getEffectivePolicy, listRecentTraces, listRoutesForEnvironment, updateRoute} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type Route = components['schemas']['RouteResource'] & Record<string, unknown>;
type Deployment = components['schemas']['ModelDeployment'];
type LogicalModel = components['schemas']['LogicalModel'];
type Strategy = components['schemas']['RouteUpsertRequest']['strategy'];
type Target = components['schemas']['RouteUpsertRequest']['targets'][number];
type Conflict = {message: string; current: unknown};

const strategies: Strategy[] = ['PRIORITY_FAILOVER', 'WEIGHTED', 'WEIGHTED_ROUND_ROBIN', 'LOWEST_COST', 'LOWEST_LATENCY_EWMA'];
const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function RoutesPage({projectId, environmentId, roles}: {projectId: string; environmentId: string; roles: Role[]}) {
  const [routes, setRoutes] = useState<Route[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [models, setModels] = useState<LogicalModel[]>([]);
  const [allowedRegions, setAllowedRegions] = useState<components['schemas']['DataRegion'][]>([]);
  const [recentTraces, setRecentTraces] = useState<components['schemas']['TraceSummary'][]>([]);
  const [slug, setSlug] = useState('');
  const [strategy, setStrategy] = useState<Strategy>('PRIORITY_FAILOVER');
  const [maxHops, setMaxHops] = useState(3);
  const [timeout, setTimeout] = useState(60000);
  const [targets, setTargets] = useState<Target[]>([]);
  const [editing, setEditing] = useState<Route | null>(null);
  const [selectedDeployment, setSelectedDeployment] = useState('');
  const [deleteTarget, setDeleteTarget] = useState<Route | null>(null);
  const [confirmSlug, setConfirmSlug] = useState('');
  const [conflict, setConflict] = useState<Conflict | null>(null);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const canManage = roles.some(role => manageRoles.includes(role));

  const refresh = useCallback(async () => {
    if (!environmentId) { setRoutes([]); setDeployments([]); setRecentTraces([]); setLoading(false); return; }
    setLoading(true); setError('');
    try {
      const [nextRoutes, nextDeployments, nextModels, policy, traces] = await Promise.all([
        listRoutesForEnvironment(environmentId), listDeployments(environmentId), listAdminModels(),
        getEffectivePolicy(projectId, environmentId), listRecentTraces(environmentId),
      ]);
      setRoutes(nextRoutes as Route[]);
      setDeployments(nextDeployments);
      setModels(nextModels);
      setAllowedRegions(policy.policy.allowed_data_regions ?? []);
      setRecentTraces(traces);
    } catch (failure) { setError(message(failure)); }
    finally { setLoading(false); }
  }, [projectId, environmentId]);

  useEffect(() => { void refresh(); }, [refresh]);

  const trafficByRoute = useMemo(() => {
    const counts = new Map<string, number>();
    for (const trace of recentTraces) counts.set(trace.route_slug, (counts.get(trace.route_slug) ?? 0) + 1);
    return counts;
  }, [recentTraces]);

  function resetForm() {
    setEditing(null); setSlug(''); setStrategy('PRIORITY_FAILOVER'); setMaxHops(3); setTimeout(60000); setTargets([]); setSelectedDeployment('');
  }

  function startEdit(route: Route) {
    setEditing(route); setSlug(route.slug); setStrategy(route.strategy); setMaxHops(route.max_failover_hops);
    setTimeout(route.request_timeout_ms); setTargets(route.targets.map(target => ({...target})));
    setSelectedDeployment(''); setConflict(null);
  }

  function addTarget() {
    if (!selectedDeployment || targets.some(target => target.deployment_id === selectedDeployment)) return;
    const next = [...targets, {deployment_id: selectedDeployment, priority_order: targets.length + 1}];
    setTargets(isWeighted(strategy) ? distributeWeights(next) : next); setSelectedDeployment('');
  }

  function currentPayload(): components['schemas']['RouteUpsertRequest'] {
    return {slug: slug.trim(), env_id: environmentId, strategy, max_failover_hops: maxHops, request_timeout_ms: timeout,
      allowed_data_regions: editing?.allowed_data_regions ?? [], targets: targets.map((target, index) => ({...target, priority_order: index + 1}))};
  }

  function useLatestRouteVersion() {
    if (!conflict?.current || typeof conflict.current !== 'object' || !('id' in conflict.current) || !('version' in conflict.current)) return;
    const latest = conflict.current as Route;
    if (editing?.id === latest.id) setEditing(latest);
    if (deleteTarget?.id === latest.id) setDeleteTarget(latest);
    setConflict(null);
  }

  async function saveRoute() {
    if (!canManage || !slug.trim() || targets.length === 0 || (isWeighted(strategy) && targets.reduce((sum, target) => sum + (target.weight ?? 0), 0) !== 100)) return;
    setBusy(true); setError(''); setConflict(null);
    try {
      if (editing) await updateRoute(editing, currentPayload());
      else await createRoute(currentPayload());
      resetForm(); await refresh();
    } catch (failure) {
      setError(message(failure));
      if (isConflict(failure)) setConflict({message: message(failure), current: currentResource(failure)});
    } finally { setBusy(false); }
  }

  async function removeRoute() {
    if (!deleteTarget || confirmSlug !== deleteTarget.slug) return;
    setBusy(true); setError('');
    try { await deleteRoute(deleteTarget); setDeleteTarget(null); setConfirmSlug(''); await refresh(); }
    catch (failure) {
      setError(message(failure));
      if (isConflict(failure)) setConflict({message: message(failure), current: currentResource(failure)});
    } finally { setBusy(false); }
  }


  const totalWeight = targets.reduce((sum, target) => sum + (target.weight ?? 0), 0);
  const deploymentById = new Map(deployments.map(deployment => [deployment.id, deployment]));
  const modelById = new Map(models.map(model => [model.id, model]));

  return <VStack gap={4}>
    <Heading level={1}>Routes</Heading>
    <Text>Build capability-aware routes for this environment.</Text>
    {!environmentId ? <Text role="status">Select an environment to manage routes.</Text> : null}
    {error ? <Text role="alert">{error}</Text> : null}
    {allowedRegions.length > 0 ? <Text>Inherited data residency allows: {allowedRegions.join(', ')}. Routes cannot widen these regions.</Text> : <Text>Effective policy currently reports no allowed data regions.</Text>}
    <Section><VStack gap={3}>
      <Heading level={2}>{editing ? `Edit ${editing.slug}` : 'Create route'}</Heading>
      {!canManage ? <Text role="status">Only Organization Owners and Team Admins can change routes.</Text> : null}
      <TextInput label="Route slug" value={slug} onChange={setSlug} isDisabled={!canManage || busy} />
      <label>Routing strategy<select aria-label="Routing strategy" value={strategy} disabled={!canManage || busy} onChange={event => {
        const next = event.target.value as Strategy; setStrategy(next);
        if (isWeighted(next) && targets.length > 0) setTargets(distributeWeights(targets.map(target => ({...target}))));
        if (!isWeighted(next)) setTargets(targets.map(({weight: _weight, ...target}) => target));
      }}>{strategies.map(item => <option key={item} value={item}>{item.replaceAll('_', ' ')}</option>)}</select></label>
      <HStack gap={3} wrap="wrap"><NumberInput label="Maximum failover hops" value={maxHops} min={1} max={3} isIntegerOnly onChange={setMaxHops} isDisabled={!canManage || busy} />
        <NumberInput label="Request timeout (ms)" value={timeout} min={500} max={300000} isIntegerOnly onChange={setTimeout} isDisabled={!canManage || busy} /></HStack>
      <Heading level={3}>Fallback deployments</Heading>
      <HStack gap={2} wrap="wrap"><label>Deployment<select aria-label="Deployment" value={selectedDeployment} onChange={event => setSelectedDeployment(event.target.value)} disabled={!canManage || busy}>
        <option value="">Select a deployment</option>{deployments.filter(item => !targets.some(target => target.deployment_id === item.id)).map(item => <option key={item.id} value={item.id}>{item.upstream_model_name} · {item.state}</option>)}
      </select></label><Button label="Add deployment" variant="secondary" onClick={addTarget} isDisabled={!selectedDeployment || !canManage || busy} /></HStack>
      {targets.length > 0 ? <Table data={targets.map((target, index) => ({...target, id: target.deployment_id, index}))} idKey="id" density="compact" columns={[
        {key: 'priority_order', header: 'Priority'},
        {key: 'deployment_id', header: 'Deployment', renderCell: row => { const deployment = deploymentById.get(String(row.deployment_id)); return <Text>{deployment?.upstream_model_name ?? row.deployment_id} · {deployment?.state ?? 'Unknown'}</Text>; }},
        ...(isWeighted(strategy) ? [{key: 'weight', header: 'Weight', renderCell: (row: Record<string, unknown>) => <NumberInput label={`Weight for ${String(row.deployment_id)}`} value={Number(row.weight ?? 0)} min={0} max={100} isIntegerOnly onChange={value => setTargets(targets.map((item, index) => index === Number(row.index) ? {...item, weight: value} : item))} />} as const] : []),
        {key: 'remove', header: 'Action', renderCell: row => <HStack gap={1} wrap="wrap"><Button label={`Move ${String(row.deployment_id)} up`} variant="ghost" isDisabled={Number(row.index) === 0} onClick={() => moveTarget(targets, Number(row.index), -1, setTargets)} /><Button label={`Move ${String(row.deployment_id)} down`} variant="ghost" isDisabled={Number(row.index) === targets.length - 1} onClick={() => moveTarget(targets, Number(row.index), 1, setTargets)} /><Button label={`Remove ${String(row.deployment_id)}`} variant="ghost" onClick={() => { const remaining = targets.filter(item => item.deployment_id !== row.deployment_id); setTargets(isWeighted(strategy) ? distributeWeights(remaining) : remaining); }} /></HStack>},
      ]} /> : <Text>No deployments selected. Add at least one target before saving.</Text>}
      {targets.some(target => modelById.get(deploymentById.get(target.deployment_id)?.logical_model_id ?? '')?.capabilities.tool_calling !== true) ? <Text role="alert">At least one selected fallback does not declare tool_calling. Requests that require tool calling may skip that deployment.</Text> : null}
      {isWeighted(strategy) && targets.length > 0 && totalWeight !== 100 ? <Text role="alert">Target weights total {totalWeight}%. They must total 100%.</Text> : null}
      {targets.some(target => !deploymentById.has(target.deployment_id)) ? <Text role="alert">A selected fallback deployment is unavailable. Remove it or choose an active deployment.</Text> : null}
      {targets.some(target => { const deployment = deploymentById.get(target.deployment_id); return Boolean(deployment && allowedRegions.length && !allowedRegions.includes(deployment.data_region)); }) ? <Text role="alert">A fallback target violates the inherited data residency policy.</Text> : null}
      <HStack gap={2}><Button label={busy ? 'Saving route…' : editing ? 'Save route' : 'Create route'} variant="primary" isLoading={busy} isDisabled={!canManage || busy || !slug.trim() || targets.length === 0 || (isWeighted(strategy) && totalWeight !== 100) || targets.some(target => { const deployment = deploymentById.get(target.deployment_id); return !deployment || (allowedRegions.length > 0 && !allowedRegions.includes(deployment.data_region)); })} onClick={() => void saveRoute()} />
        {editing ? <Button label="Cancel edit" variant="secondary" isDisabled={busy} onClick={resetForm} /> : null}</HStack>
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>Configured routes</Heading>
      {loading ? <Text role="status">Loading routes…</Text> : routes.length === 0 ? <Text>No routes are configured for this environment.</Text> : <Table data={routes} idKey="id" density="compact" hasHover columns={[
        {key: 'slug', header: 'Route'}, {key: 'strategy', header: 'Strategy'},
        {key: 'targets', header: 'Fallbacks', renderCell: row => <Text>{(row.targets as Target[]).map(target => deploymentById.get(target.deployment_id)?.upstream_model_name ?? target.deployment_id).join(' → ')}</Text>},
        {key: 'health', header: 'Deployment health', renderCell: row => <HStack gap={1} wrap="wrap">{(row.targets as Target[]).map(target => { const deployment = deploymentById.get(target.deployment_id); return <Token key={target.deployment_id} label={deployment?.state ?? 'Unavailable'} color={deployment?.state === 'ACTIVE_HEALTHY' ? 'green' : deployment?.state === 'THROTTLED_COOLDOWN' ? 'yellow' : 'red'} icon={deployment?.state === 'ACTIVE_HEALTHY' ? 'success' : 'warning'} />; })}</HStack>},
        {key: 'actions', header: 'Actions', renderCell: row => <HStack gap={1} wrap="wrap"><Button label={`Edit ${String(row.slug)}`} variant="secondary" onClick={() => startEdit(row as Route)} isDisabled={!canManage} /><Button label={`Delete ${String(row.slug)}`} variant="ghost" onClick={() => { setDeleteTarget(row as Route); setConfirmSlug(''); }} isDisabled={!canManage} /></HStack>},
      ]} />}
      <Text>Recent affected request volume is counted from Admin API traces in the last hour.</Text>
    </VStack></Section>
    {deleteTarget ? <Dialog isOpen onOpenChange={open => { if (!open) { setDeleteTarget(null); setConfirmSlug(''); } }} purpose="form" width={520} padding={5}>
      <VStack gap={3}><DialogHeader title={`Delete route ${deleteTarget.slug}`} />
        <Text>This removes the route configuration. {trafficByRoute.get(deleteTarget.slug) ?? 0} matching requests appear in Admin API traces from the last hour. Active streams are not terminated by route deletion.</Text>
        <TextInput label="Type the route slug to confirm" value={confirmSlug} onChange={setConfirmSlug} />
        <HStack gap={2}><Button label={busy ? 'Deleting route…' : 'Delete route'} variant="destructive" isLoading={busy} isDisabled={busy || confirmSlug !== deleteTarget.slug} onClick={() => void removeRoute()} /><Button label="Cancel" variant="secondary" isDisabled={busy} onClick={() => { setDeleteTarget(null); setConfirmSlug(''); }} /></HStack>
      </VStack>
    </Dialog> : null}
    {conflict ? <Dialog isOpen onOpenChange={open => { if (!open) setConflict(null); }} purpose="required" width={720} padding={5}>
      <VStack gap={3}><DialogHeader title="Route changed by another administrator" /><Text role="alert">{conflict.message}. Your draft is still in the form; compare it with the latest server version before applying changes.</Text>
        <HStack gap={3} wrap="wrap"><Section><VStack gap={2}><Heading level={3}>Your draft</Heading><Text>{JSON.stringify(currentPayload(), null, 2)}</Text></VStack></Section><Section><VStack gap={2}><Heading level={3}>Current server version</Heading><Text>{JSON.stringify(conflict.current ?? {}, null, 2)}</Text></VStack></Section></HStack>
        <Button label="Use latest version and keep draft" variant="secondary" onClick={useLatestRouteVersion} isDisabled={!conflict.current || typeof conflict.current !== 'object' || !('id' in conflict.current) || !('version' in conflict.current)} />
      </VStack>
    </Dialog> : null}
  </VStack>;
}

function isConflict(error: unknown) { return Boolean(error && typeof error === 'object' && 'status' in error && error.status === 409); }
function currentResource(error: unknown): unknown { return error && typeof error === 'object' && 'currentResource' in error ? error.currentResource : null; }
function message(error: unknown) { return error instanceof Error ? error.message : 'The Admin API request failed.'; }
function distributeWeights(targets: Target[]) {
  const base = Math.floor(100 / targets.length);
  let remainder = 100 - base * targets.length;
  return targets.map(target => ({...target, weight: base + (remainder-- > 0 ? 1 : 0)}));
}
function isWeighted(strategy: Strategy) { return strategy === 'WEIGHTED' || strategy === 'WEIGHTED_ROUND_ROBIN'; }
function moveTarget(targets: Target[], index: number, delta: number, setTargets: (targets: Target[]) => void) {
  const nextIndex = index + delta;
  if (nextIndex < 0 || nextIndex >= targets.length) return;
  const next = [...targets]; [next[index], next[nextIndex]] = [next[nextIndex], next[index]]; setTargets(next);
}
