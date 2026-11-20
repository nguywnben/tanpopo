import {useCallback, useEffect, useMemo, useState, type FormEvent} from 'react';
import {Plus} from 'lucide-react';
import {formatNumber} from '../../i18n/format';
import {
  createAdminModel,
  createDeployment,
  deleteDeployment,
  listAdminModels,
  listConnections,
  listDeployments,
  listRoutes,
  updateDeployment,
} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {DataTable} from '../../ui/DataTable';
import {MultiSelectField} from '../../ui/MultiSelectField';
import {ConfirmDialog, Drawer} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
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
type LogicalModel = components['schemas']['LogicalModel'];
type Deployment = components['schemas']['ModelDeployment'];
type Connection = components['schemas']['ProviderConnection'];
type RateCard = components['schemas']['RateCardNanoUsd'];

const rateFields = [
  ['input_token_nano_usd', 'Input token (nano-USD)'],
  ['output_token_nano_usd', 'Output token (nano-USD)'],
  ['cache_read_token_nano_usd', 'Cache read (nano-USD)'],
  ['cache_write_token_nano_usd', 'Cache write (nano-USD)'],
  ['reasoning_token_nano_usd', 'Reasoning token (nano-USD)'],
  ['image_unit_nano_usd', 'Image unit (nano-USD)'],
  ['audio_sec_nano_usd', 'Audio second (nano-USD)'],
  ['document_page_nano_usd', 'Document page (nano-USD)'],
] as const;

type RateField = (typeof rateFields)[number][0];

const emptyRates = Object.fromEntries(rateFields.map(([key]) => [key, '0'])) as Record<RateField, string>;

const capabilityNames = [
  'streaming',
  'tool_calling',
  'parallel_tool_calls',
  'strict_json_schema',
  'json_object',
  'vision',
  'audio_input',
  'document_input',
  'reasoning',
  'prompt_caching',
  'embeddings',
  'rerank',
] as const;

const emptyCapabilities: components['schemas']['CapabilityProfile'] = {
  streaming: false,
  tool_calling: false,
  parallel_tool_calls: false,
  strict_json_schema: false,
  json_object: false,
  vision: false,
  audio_input: false,
  document_input: false,
  reasoning: false,
  prompt_caching: false,
  embeddings: false,
  rerank: false,
};

const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function ModelsPage({environmentId, roles}: {environmentId: string; roles: Role[]}) {
  const [tab, setTab] = useState<'deployments' | 'models'>('deployments');
  const [models, setModels] = useState<LogicalModel[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  // Filters
  const [regionFilter, setRegionFilter] = useState('ALL');
  const [healthFilter, setHealthFilter] = useState('ALL');
  const [modelQuery, setModelQuery] = useState('');
  const [assuranceFilter, setAssuranceFilter] = useState('ALL');
  const [capFilter, setCapFilter] = useState('ALL');

  // Register Model Drawer
  const [modelDrawerOpen, setModelDrawerOpen] = useState(false);
  const [displayName, setDisplayName] = useState('');
  const [slug, setSlug] = useState('');
  const [contextWindow, setContextWindow] = useState('128000');
  const [maxOutput, setMaxOutput] = useState('8192');
  const [capabilities, setCapabilities] = useState<components['schemas']['CapabilityProfile']>(emptyCapabilities);

  // Register Deployment Drawer
  const [depDrawerOpen, setDepDrawerOpen] = useState(false);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedConnection, setSelectedConnection] = useState('');
  const [upstreamName, setUpstreamName] = useState('');
  const [rates, setRates] = useState<Record<RateField, string>>(emptyRates);
  const [selfHostedZero, setSelfHostedZero] = useState(false);

  // Action confirmation
  const [pendingAction, setPendingAction] = useState<{deployment: Deployment; action: 'disable' | 'delete'; lastHealthyRoutes: string[]} | null>(null);

  const canManage = roles.some(role => manageRoles.includes(role));

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const [modelRows, depRows, connRows] = await Promise.all([
        listAdminModels(),
        listDeployments(environmentId || undefined),
        listConnections(),
      ]);
      setModels(modelRows);
      setDeployments(depRows);
      setConnections(connRows);
      setSelectedModel(curr => curr || modelRows[0]?.id || '');
      setSelectedConnection(curr => curr || connRows[0]?.id || '');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setLoading(false);
    }
  }, [environmentId]);

  useEffect(() => { void reload(); }, [reload]);

  const visibleDeployments = useMemo(
    () =>
      deployments.filter(
        item => (regionFilter === 'ALL' || item.data_region === regionFilter) && (healthFilter === 'ALL' || item.state === healthFilter),
      ),
    [deployments, regionFilter, healthFilter],
  );

  const visibleModels = useMemo(
    () =>
      models.filter(
        item =>
          (assuranceFilter === 'ALL' || item.assurance_tier === assuranceFilter) &&
          (capFilter === 'ALL' || item.capabilities[capFilter as keyof typeof item.capabilities]) &&
          `${item.slug} ${item.display_name}`.toLowerCase().includes(modelQuery.toLowerCase()),
      ),
    [models, assuranceFilter, capFilter, modelQuery],
  );

  async function handleCreateModel(event: FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    if (!Number.isInteger(Number(contextWindow)) || Number(contextWindow) < 1 || !Number.isInteger(Number(maxOutput)) || Number(maxOutput) < 1) {
      setError('Context window and maximum output tokens must be positive integers.');
      return;
    }
    setBusy(true);
    try {
      const saved = await createAdminModel({
        id: slug.trim(),
        slug: slug.trim(),
        display_name: displayName.trim(),
        context_window: Number(contextWindow),
        max_output_tokens: Number(maxOutput),
        assurance_tier: 'DECLARED',
        capabilities,
      });
      setModels(rows => [...rows, saved]);
      setModelDrawerOpen(false);
      setDisplayName('');
      setSlug('');
      setCapabilities(emptyCapabilities);
      setNotice(`Registered logical model "${saved.slug}" as DECLARED.`);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateDeployment(event: FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    const connection = connections.find(c => c.id === selectedConnection);
    if (!environmentId || !selectedModel || !connection) {
      setError('Select an active environment, logical model, and provider connection.');
      return;
    }
    const card = buildRateCard(rates, connection.billing_mode, selfHostedZero);
    if (card.error || !card.rateCard) {
      setError(card.error ?? 'Invalid rate card.');
      return;
    }
    setBusy(true);
    try {
      await createDeployment({
        env_id: environmentId,
        logical_model_id: selectedModel,
        connection_id: selectedConnection,
        upstream_model_name: upstreamName.trim(),
        data_region: connection.data_region,
        activate_immediately: false,
        rate_card: card.rateCard,
      });
      setDepDrawerOpen(false);
      setUpstreamName('');
      setRates(emptyRates);
      setSelfHostedZero(false);
      await reload();
      setNotice('Deployment registered in provisioned state.');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function openDeploymentAction(deployment: Deployment, action: 'disable' | 'delete') {
    setError('');
    try {
      const [routeRows, depRows] = await Promise.all([listRoutes(), listDeployments(environmentId || undefined)]);
      const healthyIds = new Set(depRows.filter(d => d.state === 'ACTIVE_HEALTHY').map(d => d.id));
      const lastHealthyRoutes =
        deployment.state === 'ACTIVE_HEALTHY'
          ? routeRows
              .filter(
                route =>
                  route.targets.some(t => t.deployment_id === deployment.id) &&
                  !route.targets.some(t => t.deployment_id !== deployment.id && healthyIds.has(t.deployment_id)),
              )
              .map(route => route.slug)
          : [];
      setPendingAction({deployment, action, lastHealthyRoutes});
    } catch (cause) {
      setError(errText(cause));
    }
  }

  async function confirmAction() {
    if (!pendingAction) return;
    setBusy(true);
    setError('');
    try {
      if (pendingAction.action === 'disable') {
        await updateDeployment(pendingAction.deployment.id, `"v${pendingAction.deployment.version}"`, {state: 'DISABLED'});
      } else {
        await deleteDeployment(pendingAction.deployment.id, `"v${pendingAction.deployment.version}"`);
      }
      const verb = pendingAction.action === 'disable' ? 'disabled' : 'deleted';
      setPendingAction(null);
      await reload();
      setNotice(`Deployment ${verb}.`);
    } catch (cause) {
      setPendingAction(null);
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Models & Deployments</h1>
          <p className="tp-page-subtitle">Logical model capability declarations, assurance tiers, and environment deployments.</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            <Button variant="secondary" size="sm" onClick={() => setModelDrawerOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>Register model</span>
            </Button>
            <Button variant="primary" size="sm" onClick={() => setDepDrawerOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>Add deployment</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label="Model catalog sections"
        activeId={tab}
        onSelect={id => setTab(id as 'deployments' | 'models')}
        items={[
          {id: 'deployments', label: 'Deployments', count: deployments.length},
          {id: 'models', label: 'Logical Models', count: models.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {tab === 'deployments' ? (
        <section className="tp-section">
          <div className="tp-toolbar">
            <div className="tp-toolbar-field">
              <SelectField
                label="Data region"
                value={regionFilter}
                onChange={setRegionFilter}
                options={[
                  {value: 'ALL', label: 'All regions'},
                  {value: 'US', label: 'US'},
                  {value: 'EU', label: 'EU'},
                  {value: 'APAC', label: 'APAC'},
                  {value: 'ON_PREM', label: 'ON_PREM'},
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Health state"
                value={healthFilter}
                onChange={setHealthFilter}
                options={[
                  {value: 'ALL', label: 'All states'},
                  ...[...new Set(deployments.map(d => d.state))].sort().map(s => ({
                    value: s,
                    label: formatOperationalState(s),
                  })),
                ]}
              />
            </div>
          </div>

          <SectionHeader title="Environment deployments" count={visibleDeployments.length} />
          <DataTable
            ariaLabel="Model deployments"
            loading={loading}
            rows={visibleDeployments}
            rowKey={row => row.id}
            emptyTitle="No deployments found"
            emptyDescription="Bind a logical model to a provider connection with an integer nano-USD rate card."
            columns={[
              {
                id: 'upstream',
                header: 'Upstream model',
                primary: true,
                width: '28%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono" style={{fontWeight: 600}}>{row.upstream_model_name}</span>
                    <span className="mono tp-muted">{row.logical_model_id}</span>
                  </div>
                ),
              },
              {id: 'conn', header: 'Connection', width: '18%', render: row => <span className="mono">{row.connection_id}</span>},
              {id: 'region', header: 'Region', width: '10%', render: row => <span>{row.data_region}</span>},
              {
                id: 'state',
                header: 'Health',
                width: '18%',
                render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />,
              },
              {
                id: 'rates',
                header: 'In / Out (nano-USD)',
                width: '18%',
                render: row => (
                  <span className="mono">
                    {row.rate_card.input_token_nano_usd} / {row.rate_card.output_token_nano_usd}
                  </span>
                ),
              },
              {
                id: 'actions',
                header: 'Actions',
                width: '8%',
                render: row =>
                  canManage ? (
                    <RowActionsMenu
                      label={`Actions for ${row.upstream_model_name}`}
                      disabled={busy}
                      items={[
                        ...(row.state !== 'DISABLED'
                          ? [{id: 'disable', label: 'Disable deployment', onClick: () => void openDeploymentAction(row, 'disable')}]
                          : []),
                        ...(row.state !== 'DISABLED' ? [{type: 'divider' as const}] : []),
                        {
                          id: 'delete',
                          label: 'Delete deployment',
                          variant: 'destructive' as const,
                          onClick: () => void openDeploymentAction(row, 'delete'),
                        },
                      ]}
                    />
                  ) : null,
              },
            ]}
          />
        </section>
      ) : (
        <section className="tp-section">
          <div className="tp-toolbar">
            <div className="tp-toolbar-field">
              <TextField label="Search models" type="search" value={modelQuery} onChange={event => setModelQuery(event.target.value)} />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Assurance tier"
                value={assuranceFilter}
                onChange={setAssuranceFilter}
                options={[
                  {value: 'ALL', label: 'All tiers'},
                  {value: 'DECLARED', label: 'DECLARED'},
                  {value: 'TESTED', label: 'TESTED'},
                  {value: 'VERIFIED', label: 'VERIFIED'},
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Capability"
                value={capFilter}
                onChange={setCapFilter}
                options={[
                  {value: 'ALL', label: 'All capabilities'},
                  ...capabilityNames.map(cap => ({value: cap, label: cap.replaceAll('_', ' ')})),
                ]}
              />
            </div>
          </div>

          <SectionHeader title="Logical model registry" count={visibleModels.length} />
          <DataTable
            ariaLabel="Logical models"
            loading={loading}
            rows={visibleModels}
            rowKey={row => row.id}
            emptyTitle="No logical models registered"
            columns={[
              {
                id: 'slug',
                header: 'Model',
                primary: true,
                width: '24%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono" style={{fontWeight: 600}}>{row.slug}</span>
                    <span className="tp-muted">{row.display_name}</span>
                  </div>
                ),
              },
              {id: 'assurance', header: 'Assurance', width: '14%', render: row => <StatusPill tone={stateTone(row.assurance_tier)} label={formatOperationalState(row.assurance_tier)} />},
              {
                id: 'limits',
                header: 'Context / Output',
                width: '18%',
                render: row => (
                  <span className="mono">
                    {formatNumber(row.context_window)} / {formatNumber(row.max_output_tokens)}
                  </span>
                ),
              },
              {
                id: 'caps',
                header: 'Declared capabilities',
                width: '44%',
                render: row => (
                  <span className="tp-muted">
                    {capabilityNames.filter(key => row.capabilities[key]).map(key => key.replaceAll('_', ' ')).join(', ') || 'None declared'}
                  </span>
                ),
              },
            ]}
          />
        </section>
      )}

      <Drawer
        open={modelDrawerOpen}
        onClose={() => setModelDrawerOpen(false)}
        title="Register logical model"
        subtitle="New models start as DECLARED until test or verification evidence promotes them."
      >
        <form className="tp-stack" onSubmit={handleCreateModel}>
          <TextField label="Display name" required value={displayName} onChange={event => setDisplayName(event.target.value)} />
          <TextField label="Canonical model slug" required value={slug} onChange={event => setSlug(event.target.value)} />
          <div className="tp-grid-2">
            <TextField label="Context tokens" inputMode="numeric" required value={contextWindow} onChange={event => setContextWindow(event.target.value)} />
            <TextField label="Max output tokens" inputMode="numeric" required value={maxOutput} onChange={event => setMaxOutput(event.target.value)} />
          </div>
          <MultiSelectField
            label="Declared capabilities"
            placeholder="Select model capabilities"
            hasSelectAll
            hasClear
            hasSearch
            value={capabilityNames.filter(cap => capabilities[cap])}
            options={capabilityNames.map(cap => ({value: cap, label: cap.replaceAll('_', ' ')}))}
            onChange={selected => {
              const selectedSet = new Set(selected);
              setCapabilities(
                Object.fromEntries(capabilityNames.map(cap => [cap, selectedSet.has(cap)])) as Record<
                  (typeof capabilityNames)[number],
                  boolean
                >,
              );
            }}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setModelDrawerOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Register model</Button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={depDrawerOpen}
        onClose={() => setDepDrawerOpen(false)}
        title="Register model deployment"
        subtitle="All pricing rates use exact non-negative integer nano-USD values."
      >
        <form className="tp-stack" onSubmit={handleCreateDeployment}>
          <SelectField
            label="Logical model"
            value={selectedModel}
            placeholder="Select logical model"
            onChange={setSelectedModel}
            options={models.map(item => ({value: item.id, label: item.slug}))}
          />
          <SelectField
            label="Provider connection"
            value={selectedConnection}
            placeholder="Select provider connection"
            onChange={setSelectedConnection}
            options={connections.map(item => ({value: item.id, label: `${item.name} (${item.billing_mode})`}))}
          />
          <TextField label="Upstream model name" required value={upstreamName} onChange={event => setUpstreamName(event.target.value)} />
          <div className="tp-grid-2">
            {rateFields.map(([key, label]) => (
              <TextField
                key={key}
                label={label}
                inputMode="numeric"
                required
                value={rates[key]}
                onChange={event => setRates(curr => ({...curr, [key]: event.target.value}))}
              />
            ))}
          </div>
          <CheckboxField
            label="Self-hosted zero-rate deployment"
            description="Only valid for connections with SELF_HOSTED_INFRA billing mode."
            checked={selfHostedZero}
            onChange={setSelfHostedZero}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setDepDrawerOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Save deployment</Button>
          </div>
        </form>
      </Drawer>

      <ConfirmDialog
        open={pendingAction !== null}
        onClose={() => setPendingAction(null)}
        title={`${pendingAction?.action === 'delete' ? 'Delete' : 'Disable'} deployment`}
        description={`${pendingAction?.action === 'delete' ? 'Delete' : 'Disable'} "${pendingAction?.deployment.upstream_model_name ?? ''}"? Routes where this is the last healthy target: ${pendingAction?.lastHealthyRoutes.join(', ') || 'None'}.`}
        confirmLabel={pendingAction?.action === 'delete' ? 'Delete deployment' : 'Disable deployment'}
        busy={busy}
        onConfirm={() => void confirmAction()}
      />
    </div>
  );
}

export function buildRateCard(
  rates: Record<RateField, string>,
  billingMode: Connection['billing_mode'],
  confirmZeroRate: boolean,
): {rateCard?: RateCard; error?: string} {
  const numericRates = Object.fromEntries(rateFields.map(([key]) => [key, Number(rates[key])])) as Record<RateField, number>;
  if (rateFields.some(([key]) => !Number.isSafeInteger(numericRates[key]) || numericRates[key] < 0)) {
    return {error: 'All rates must be non-negative safe integer nano-USD values.'};
  }
  const zeroRate = Object.values(numericRates).every(rate => rate === 0);
  if (zeroRate && billingMode !== 'SELF_HOSTED_INFRA') {
    return {error: 'Zero rates are available only for self-hosted infrastructure.'};
  }
  if (zeroRate && !confirmZeroRate) {
    return {error: 'Confirm the self-hosted zero-rate declaration.'};
  }
  return {rateCard: {...numericRates, self_hosted_zero_rate: zeroRate && confirmZeroRate}};
}

function errText(cause: unknown): string {
  return cause instanceof Error ? cause.message : 'Request failed.';
}
