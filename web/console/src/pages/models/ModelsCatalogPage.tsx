import {useEffect, useMemo, useState, type FormEvent} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {AlertDialog} from '@astryxdesign/core/AlertDialog';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {VStack} from '@astryxdesign/core/Stack';
import {createAdminModel, createDeployment, deleteDeployment, listAdminModels, listConnections, listDeployments, listRoutes, updateDeployment} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {AssuranceToken, StateToken} from '../catalog/StatusToken';

type Role = components['schemas']['RbacRole'];
type LogicalModel = components['schemas']['LogicalModel'] & Record<string, unknown>;
type Deployment = components['schemas']['ModelDeployment'] & Record<string, unknown>;
type Connection = components['schemas']['ProviderConnection'];
type DeploymentAction = {deployment: Deployment; action: 'disable' | 'delete'; lastHealthyRoutes: string[]};
type RateCard = components['schemas']['RateCardNanoUsd'];
const rateFields = [
  ['input_token_nano_usd', 'Input token rate (integer nano-USD)'], ['output_token_nano_usd', 'Output token rate (integer nano-USD)'],
  ['cache_read_token_nano_usd', 'Cache read token rate (integer nano-USD)'], ['cache_write_token_nano_usd', 'Cache write token rate (integer nano-USD)'],
  ['reasoning_token_nano_usd', 'Reasoning token rate (integer nano-USD)'], ['image_unit_nano_usd', 'Image unit rate (integer nano-USD)'],
  ['audio_sec_nano_usd', 'Audio second rate (integer nano-USD)'], ['document_page_nano_usd', 'Document page rate (integer nano-USD)'],
] as const;
type RateField = typeof rateFields[number][0];
const emptyRates: Record<RateField, string> = Object.fromEntries(rateFields.map(([key]) => [key, '0'])) as Record<RateField, string>;
const capabilityNames = ['streaming', 'tool_calling', 'parallel_tool_calls', 'strict_json_schema', 'json_object', 'vision', 'audio_input', 'document_input', 'reasoning', 'prompt_caching', 'embeddings', 'rerank'] as const;
const emptyCapabilities: components['schemas']['CapabilityProfile'] = {streaming: false, tool_calling: false, parallel_tool_calls: false, strict_json_schema: false, json_object: false, vision: false, audio_input: false, document_input: false, reasoning: false, prompt_caching: false, embeddings: false, rerank: false};
const canManage = (roles: Role[]) => roles.some(role => ['ORG_OWNER', 'TEAM_ADMIN'].includes(role));

export function ModelsCatalogPage({roles = [], environmentId}: {roles?: Role[]; environmentId?: string}) {
  const [models, setModels] = useState<LogicalModel[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [filter, setFilter] = useState('');
  const [assurance, setAssurance] = useState('ALL');
  const [capabilityFilter, setCapabilityFilter] = useState('ALL');
  const [regionFilter, setRegionFilter] = useState('ALL');
  const [healthFilter, setHealthFilter] = useState('ALL');
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [context, setContext] = useState('128000');
  const [maxOutput, setMaxOutput] = useState('8192');
  const [capabilities, setCapabilities] = useState<components['schemas']['CapabilityProfile']>(emptyCapabilities);
  const [newDeployment, setNewDeployment] = useState(false);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedConnection, setSelectedConnection] = useState('');
  const [upstream, setUpstream] = useState('');
  const [rates, setRates] = useState<Record<RateField, string>>(emptyRates);
  const [selfHostedZero, setSelfHostedZero] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [deploymentAction, setDeploymentAction] = useState<DeploymentAction | null>(null);
  const mutable = canManage(roles);
  async function reload() {
    const [modelRows, deploymentRows, connectionRows] = await Promise.all([listAdminModels(), listDeployments(environmentId), listConnections()]);
    setModels(modelRows); setDeployments(deploymentRows); setConnections(connectionRows);
    setSelectedModel(value => value || modelRows[0]?.id || '');
    setSelectedConnection(value => value || connectionRows[0]?.id || '');
  }
  useEffect(() => { reload().catch(failure => setError(message(failure))); }, [environmentId]);
  const visible = useMemo(() => models.filter(model => (assurance === 'ALL' || model.assurance_tier === assurance) &&
    (capabilityFilter === 'ALL' || model.capabilities[capabilityFilter as keyof typeof model.capabilities]) &&
    `${model.slug} ${model.display_name} ${Object.keys(model.capabilities).filter(key => model.capabilities[key as keyof typeof model.capabilities]).join(' ')}`.toLowerCase().includes(filter.toLowerCase())), [models, assurance, capabilityFilter, filter]);
  const visibleDeployments = useMemo(() => deployments.filter(deployment =>
    (regionFilter === 'ALL' || deployment.data_region === regionFilter) && (healthFilter === 'ALL' || deployment.state === healthFilter)),
  [deployments, regionFilter, healthFilter]);

  async function saveModel(event: FormEvent) {
    event.preventDefault(); setError('');
    if (!Number.isInteger(Number(context)) || Number(context) < 1 || !Number.isInteger(Number(maxOutput)) || Number(maxOutput) < 1) { setError('Context and output limits must be positive integer token counts.'); return; }
    try {
      const saved = await createAdminModel({id: slug, slug, display_name: name, context_window: Number(context), max_output_tokens: Number(maxOutput), assurance_tier: 'DECLARED', capabilities});
      setModels(rows => [...rows, saved]); setName(''); setSlug(''); setCapabilities(emptyCapabilities); setNotice('Logical model registered as self-declared. Test or verification evidence is required to raise its assurance tier.');
    } catch (failure) { setError(message(failure)); }
  }

  async function saveDeployment(event: FormEvent) {
    event.preventDefault(); setError('');
    const connection = connections.find(item => item.id === selectedConnection);
    if (!environmentId || !selectedModel || !connection) { setError('Select an environment, model, and provider connection first.'); return; }
    const result = buildRateCard(rates, connection.billing_mode, selfHostedZero);
    if (result.error) { setError(result.error); return; }
    try {
      await createDeployment({env_id: environmentId, logical_model_id: selectedModel, connection_id: selectedConnection, upstream_model_name: upstream, data_region: connection.data_region, activate_immediately: false,
        rate_card: result.rateCard!});
      setNewDeployment(false); setUpstream(''); setRates(emptyRates); await reload(); setNotice('Deployment registered in the provisioned state.');
    } catch (failure) { setError(message(failure)); }
  }

  async function prepareDeploymentAction(deployment: Deployment, action: 'disable' | 'delete') {
    setError('');
    try {
      const [routeRows, deploymentRows] = await Promise.all([listRoutes(), listDeployments(environmentId)]);
      const healthyIds = new Set(deploymentRows.filter(row => row.state === 'ACTIVE_HEALTHY').map(row => row.id));
      const lastHealthyRoutes = deployment.state === 'ACTIVE_HEALTHY' ? routeRows.filter(route => {
        if (!route.targets.some(target => target.deployment_id === deployment.id)) return false;
        return !route.targets.some(target => target.deployment_id !== deployment.id && healthyIds.has(target.deployment_id));
      }).map(route => route.slug) : [];
      setDeploymentAction({deployment, action, lastHealthyRoutes});
    } catch (failure) { setError(message(failure)); }
  }

  async function confirmDeploymentAction() {
    if (!deploymentAction) return;
    try {
      if (deploymentAction.action === 'disable') {
        await updateDeployment(deploymentAction.deployment.id, `"v${deploymentAction.deployment.version}"`, {state: 'DISABLED'});
      } else {
        await deleteDeployment(deploymentAction.deployment.id, `"v${deploymentAction.deployment.version}"`);
      }
      const action = deploymentAction.action;
      setDeploymentAction(null); await reload(); setNotice(action === 'disable' ? 'Deployment disabled.' : 'Deployment deleted.');
    } catch (failure) { setDeploymentAction(null); setError(message(failure)); }
  }

  return <VStack gap={4}>
    <Heading level={1}>Model catalog and deployments</Heading>
    <Text>Compare declared capabilities, assurance, deployment health, data region, and integer nano-USD rates.</Text>
    {error ? <Text role="alert">{error}</Text> : null}{notice ? <Text role="status">{notice}</Text> : null}
    <TextInput label="Filter by model or capability" type="search" value={filter} onChange={setFilter} />
    <label>Assurance tier<select aria-label="Assurance tier" value={assurance} onChange={event => setAssurance(event.target.value)}><option value="ALL">All tiers</option><option>DECLARED</option><option>TESTED</option><option>VERIFIED</option></select></label>
    <label>Capability filter<select aria-label="Capability filter" value={capabilityFilter} onChange={event => setCapabilityFilter(event.target.value)}><option value="ALL">All capabilities</option>{capabilityNames.map(capability => <option key={capability} value={capability}>{capability.replaceAll('_', ' ')}</option>)}</select></label>
    <Table data={visible} density="compact" dividers="rows" columns={[
      {key: 'slug', header: 'Model slug'}, {key: 'display_name', header: 'Model'},
      {key: 'assurance_tier', header: 'Assurance', renderCell: row => <AssuranceToken tier={row.assurance_tier as LogicalModel['assurance_tier']} />},
      {key: 'capabilities', header: 'Capabilities', renderCell: row => <Text>{capabilityNames.filter(key => (row.capabilities as LogicalModel['capabilities'])[key]).join(', ') || 'No capabilities declared'}</Text>},
      {key: 'limits', header: 'Context / output', renderCell: row => <Text>{String(row.context_window)} / {String(row.max_output_tokens)}</Text>},
    ]} />
    <Heading level={2}>Deployments</Heading>
    <label>Data region filter<select aria-label="Data region filter" value={regionFilter} onChange={event => setRegionFilter(event.target.value)}><option value="ALL">All regions</option><option>US</option><option>EU</option><option>APAC</option><option>ON_PREM</option></select></label>
    <label>Deployment health filter<select aria-label="Deployment health filter" value={healthFilter} onChange={event => setHealthFilter(event.target.value)}><option value="ALL">All health states</option>{[...new Set(deployments.map(item => item.state))].sort().map(state => <option key={state} value={state}>{state.replaceAll('_', ' ')}</option>)}</select></label>
    <Table data={visibleDeployments} density="compact" dividers="rows" columns={[
      {key: 'upstream_model_name', header: 'Upstream model'}, {key: 'logical_model_id', header: 'Logical model'}, {key: 'connection_id', header: 'Connection'},
      {key: 'data_region', header: 'Region'}, {key: 'state', header: 'Health', renderCell: row => <StateToken state={String(row.state)} />},
      {key: 'rate_card', header: 'Input / output nano-USD', renderCell: row => <Text>{String((row.rate_card as Deployment['rate_card']).input_token_nano_usd)} / {String((row.rate_card as Deployment['rate_card']).output_token_nano_usd)}</Text>},
      {key: 'actions', header: 'Actions', renderCell: row => mutable ? <VStack gap={1}>{row.state !== 'DISABLED' ? <Button label="Disable deployment" variant="secondary" onClick={() => void prepareDeploymentAction(row as Deployment, 'disable')} /> : null}<Button label="Delete deployment" variant="secondary" onClick={() => void prepareDeploymentAction(row as Deployment, 'delete')} /></VStack> : null},
    ]} />
    {mutable ? <>
      <Section><VStack gap={3}><Heading level={2}>Register logical model</Heading>
        <form onSubmit={saveModel}><VStack gap={3}>
          <TextInput label="Display name" value={name} onChange={setName} isRequired />
          <TextInput label="Canonical model slug" value={slug} onChange={setSlug} isRequired />
          <TextInput label="Context window (tokens)" inputMode="numeric" value={context} onChange={setContext} isRequired />
          <TextInput label="Maximum output tokens" inputMode="numeric" value={maxOutput} onChange={setMaxOutput} isRequired />
          <Text>Capabilities are recorded as your declaration. New models remain self-declared until test or verification evidence is recorded.</Text>
          <Section><VStack gap={2}><Heading level={3}>Declared capabilities</Heading>
            {capabilityNames.map(key => <CheckboxInput key={key} label={key.replaceAll('_', ' ')} value={capabilities[key]} onChange={checked => setCapabilities(current => ({...current, [key]: checked}))} />)}
          </VStack></Section>
          <Button label="Register model" type="submit" />
        </VStack></form>
      </VStack></Section>
      <Section><VStack gap={3}><Heading level={2}>Add deployment</Heading>
        <Button label={newDeployment ? 'Close deployment form' : 'Register deployment'} variant="secondary" onClick={() => setNewDeployment(open => !open)} />
        {newDeployment ? <form onSubmit={saveDeployment}><VStack gap={3}>
          <label>Logical model<select aria-label="Deployment logical model" value={selectedModel} onChange={event => setSelectedModel(event.target.value)}>{models.map(item => <option key={item.id} value={item.id}>{item.slug}</option>)}</select></label>
          <label>Provider connection<select aria-label="Deployment connection" value={selectedConnection} onChange={event => setSelectedConnection(event.target.value)}>{connections.map(item => <option key={item.id} value={item.id}>{item.name} · {item.billing_mode}</option>)}</select></label>
          <TextInput label="Upstream model name" value={upstream} onChange={setUpstream} isRequired />
          {rateFields.map(([key, label]) => <TextInput key={key} label={label} inputMode="numeric" value={rates[key]} onChange={value => setRates(current => ({...current, [key]: value}))} isRequired />)}
          <CheckboxInput label="This is a self-hosted zero-rate deployment" value={selfHostedZero} onChange={setSelfHostedZero} />
          <Button label="Save deployment" type="submit" />
        </VStack></form> : null}
      </VStack></Section>
    </> : null}
    <AlertDialog isOpen={deploymentAction !== null} onOpenChange={open => !open && setDeploymentAction(null)} title={`${deploymentAction?.action === 'delete' ? 'Delete' : 'Disable'} model deployment`}
      description={`${deploymentAction?.action === 'delete' ? 'Delete' : 'Disable'} ${deploymentAction?.deployment.upstream_model_name ?? 'this deployment'}? This deployment is the last healthy target for active routes: ${deploymentAction?.lastHealthyRoutes.join(', ') || 'None'}.`}
      actionLabel={deploymentAction?.action === 'delete' ? 'Delete deployment' : 'Disable deployment'} actionVariant="destructive" onAction={() => void confirmDeploymentAction()} />
  </VStack>;
}

function message(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
export function buildRateCard(rates: Record<RateField, string>, billingMode: Connection['billing_mode'], confirmZeroRate: boolean): {rateCard?: RateCard; error?: string} {
  const numericRates = Object.fromEntries(rateFields.map(([key]) => [key, Number(rates[key])])) as Record<RateField, number>;
  if (rateFields.some(([key]) => !Number.isSafeInteger(numericRates[key]) || numericRates[key] < 0)) return {error: 'All rates must be non-negative safe integer nano-USD values.'};
  const zeroRate = Object.values(numericRates).every(rate => rate === 0);
  if (zeroRate && billingMode !== 'SELF_HOSTED_INFRA') return {error: 'Zero rates are available only for self-hosted infrastructure.'};
  if (zeroRate && !confirmZeroRate) return {error: 'Confirm the self-hosted zero-rate declaration.'};
  return {rateCard: {...numericRates, self_hosted_zero_rate: zeroRate && confirmZeroRate}};
}
