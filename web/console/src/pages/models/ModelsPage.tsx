import {useCallback, useEffect, useMemo, useState, type FormEvent} from 'react';
import {Plus} from 'lucide-react';
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
import {translateRuntime, useI18n} from '../../i18n/I18nProvider';
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
  'input_token_nano_usd',
  'output_token_nano_usd',
  'cache_read_token_nano_usd',
  'cache_write_token_nano_usd',
  'reasoning_token_nano_usd',
  'image_unit_nano_usd',
  'audio_sec_nano_usd',
  'document_page_nano_usd',
] as const;

type RateField = (typeof rateFields)[number];

const emptyRates = Object.fromEntries(rateFields.map(key => [key, '0'])) as Record<RateField, string>;

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
  const {t, formatNumber} = useI18n();
  const [tab, setTab] = useState<'deployments' | 'models'>('deployments');
  const [models, setModels] = useState<LogicalModel[]>([]);
  const [deployments, setDeployments] = useState<Deployment[]>([]);
  const [connections, setConnections] = useState<Connection[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [regionFilter, setRegionFilter] = useState('ALL');
  const [healthFilter, setHealthFilter] = useState('ALL');
  const [modelQuery, setModelQuery] = useState('');
  const [assuranceFilter, setAssuranceFilter] = useState('ALL');
  const [capFilter, setCapFilter] = useState('ALL');

  const [modelDrawerOpen, setModelDrawerOpen] = useState(false);
  const [displayName, setDisplayName] = useState('');
  const [slug, setSlug] = useState('');
  const [contextWindow, setContextWindow] = useState('128000');
  const [maxOutput, setMaxOutput] = useState('8192');
  const [capabilities, setCapabilities] = useState<components['schemas']['CapabilityProfile']>(emptyCapabilities);

  const [depDrawerOpen, setDepDrawerOpen] = useState(false);
  const [selectedModel, setSelectedModel] = useState('');
  const [selectedConnection, setSelectedConnection] = useState('');
  const [upstreamName, setUpstreamName] = useState('');
  const [rates, setRates] = useState<Record<RateField, string>>(emptyRates);
  const [selfHostedZero, setSelfHostedZero] = useState(false);

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
      setError(errText(cause, t));
    } finally {
      setLoading(false);
    }
  }, [environmentId, t]);

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
      setError(t('models.errPositiveInts'));
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
      setNotice(t('models.modelRegistered', {slug: saved.slug}));
    } catch (cause) {
      setError(errText(cause, t));
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
      setError(t('models.errSelectRequired'));
      return;
    }
    const card = buildRateCard(rates, connection.billing_mode, selfHostedZero);
    if (card.error || !card.rateCard) {
      setError(card.error ?? t('models.errInvalidRateCard'));
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
      setNotice(t('models.deploymentRegistered'));
    } catch (cause) {
      setError(errText(cause, t));
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
                  route.targets.some(tg => tg.deployment_id === deployment.id) &&
                  !route.targets.some(tg => tg.deployment_id !== deployment.id && healthyIds.has(tg.deployment_id)),
              )
              .map(route => route.slug)
          : [];
      setPendingAction({deployment, action, lastHealthyRoutes});
    } catch (cause) {
      setError(errText(cause, t));
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
      const noticeKey = pendingAction.action === 'disable' ? 'models.deploymentDisabled' : 'models.deploymentDeleted';
      setPendingAction(null);
      await reload();
      setNotice(t(noticeKey));
    } catch (cause) {
      setPendingAction(null);
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('models.title')}</h1>
          <p className="tp-page-subtitle">{t('models.subtitle')}</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            <Button variant="secondary" size="sm" onClick={() => setModelDrawerOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>{t('models.registerModel')}</span>
            </Button>
            <Button variant="primary" size="sm" onClick={() => setDepDrawerOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>{t('models.addDeployment')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('models.tabsLabel')}
        activeId={tab}
        onSelect={id => setTab(id as 'deployments' | 'models')}
        items={[
          {id: 'deployments', label: t('models.tabDeployments'), count: deployments.length},
          {id: 'models', label: t('models.tabModels'), count: models.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {tab === 'deployments' ? (
        <section className="tp-section">
          <div className="tp-toolbar">
            <div className="tp-toolbar-field">
              <SelectField
                label={t('models.dataRegion')}
                size="sm"
                value={regionFilter}
                onChange={setRegionFilter}
                options={[
                  {value: 'ALL', label: t('models.allRegions')},
                  {value: 'US', label: t('common.region.US')},
                  {value: 'EU', label: t('common.region.EU')},
                  {value: 'APAC', label: t('common.region.APAC')},
                  {value: 'ON_PREM', label: t('common.region.ON_PREM')},
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('models.healthState')}
                size="sm"
                value={healthFilter}
                onChange={setHealthFilter}
                options={[
                  {value: 'ALL', label: t('models.allStates')},
                  ...[...new Set(deployments.map(d => d.state))].sort().map(s => ({
                    value: s,
                    label: formatOperationalState(s),
                  })),
                ]}
              />
            </div>
          </div>

          <SectionHeader title={t('models.envDeployments')} count={visibleDeployments.length} />
          <DataTable
            ariaLabel={t('models.deploymentsAria')}
            loading={loading}
            rows={visibleDeployments}
            rowKey={row => row.id}
            emptyTitle={t('models.emptyDeploymentsTitle')}
            emptyDescription={t('models.emptyDeploymentsDesc')}
            columns={[
              {
                id: 'upstream',
                header: t('models.col.upstreamModel'),
                primary: true,
                width: '28%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono" style={{fontWeight: 600}}>{row.upstream_model_name}</span>
                    <span className="mono tp-muted">{row.logical_model_id}</span>
                  </div>
                ),
              },
              {id: 'conn', header: t('models.col.connection'), width: '24%', render: row => <span className="mono">{row.connection_id}</span>},
              {id: 'region', header: t('models.col.region'), width: '9%', render: row => <span>{row.data_region}</span>},
              {
                id: 'state',
                header: t('models.col.health'),
                width: '13%',
                render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />,
              },
              {
                id: 'rates',
                header: t('models.col.rates'),
                width: '18%',
                render: row => (
                  <span className="mono">
                    {formatNumber(row.rate_card.input_token_nano_usd)} / {formatNumber(row.rate_card.output_token_nano_usd)}
                  </span>
                ),
              },
              {
                id: 'actions',
                header: t('common.actions'),
                width: '8%',
                render: row =>
                  canManage ? (
                    <RowActionsMenu
                      label={t('common.actionsFor', {name: row.upstream_model_name})}
                      disabled={busy}
                      items={[
                        ...(row.state !== 'DISABLED'
                          ? [{id: 'disable', label: t('models.action.disable'), onClick: () => void openDeploymentAction(row, 'disable')}]
                          : []),
                        ...(row.state !== 'DISABLED' ? [{type: 'divider' as const}] : []),
                        {
                          id: 'delete',
                          label: t('models.action.delete'),
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
            <div className="tp-toolbar-field tp-toolbar-field-wide">
              <TextField label={t('models.searchModels')} fieldSize="sm" type="search" value={modelQuery} onChange={event => setModelQuery(event.target.value)} />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('models.assuranceTier')}
                size="sm"
                value={assuranceFilter}
                onChange={setAssuranceFilter}
                options={[
                  {value: 'ALL', label: t('models.allTiers')},
                  {value: 'DECLARED', label: formatOperationalState('DECLARED')},
                  {value: 'TESTED', label: formatOperationalState('TESTED')},
                  {value: 'VERIFIED', label: formatOperationalState('VERIFIED')},
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('models.capability')}
                size="sm"
                value={capFilter}
                onChange={setCapFilter}
                options={[
                  {value: 'ALL', label: t('models.allCapabilities')},
                  ...capabilityNames.map(cap => ({value: cap, label: cap.replaceAll('_', ' ')})),
                ]}
              />
            </div>
          </div>

          <SectionHeader title={t('models.registryTitle')} count={visibleModels.length} />
          <DataTable
            ariaLabel={t('models.modelsAria')}
            loading={loading}
            rows={visibleModels}
            rowKey={row => row.id}
            emptyTitle={t('models.emptyModelsTitle')}
            columns={[
              {
                id: 'slug',
                header: t('models.col.model'),
                primary: true,
                width: '24%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono" style={{fontWeight: 600}}>{row.slug}</span>
                    <span className="tp-muted">{row.display_name}</span>
                  </div>
                ),
              },
              {id: 'assurance', header: t('models.col.assurance'), width: '14%', render: row => <StatusPill tone={stateTone(row.assurance_tier)} label={formatOperationalState(row.assurance_tier)} />},
              {
                id: 'limits',
                header: t('models.col.contextOutput'),
                width: '18%',
                render: row => (
                  <span className="mono">
                    {formatNumber(row.context_window)} / {formatNumber(row.max_output_tokens)}
                  </span>
                ),
              },
              {
                id: 'caps',
                header: t('models.col.capabilities'),
                width: '44%',
                render: row => (
                  <span className="tp-muted">
                    {capabilityNames.filter(key => row.capabilities[key]).map(key => key.replaceAll('_', ' ')).join(', ') || t('common.noneDeclared')}
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
        title={t('models.drawerModelTitle')}
        subtitle={t('models.drawerModelSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleCreateModel}>
          <TextField label={t('models.displayName')} required value={displayName} onChange={event => setDisplayName(event.target.value)} />
          <TextField label={t('models.canonicalSlug')} required value={slug} onChange={event => setSlug(event.target.value)} />
          <div className="tp-grid-2">
            <TextField label={t('models.contextTokens')} inputMode="numeric" required value={contextWindow} onChange={event => setContextWindow(event.target.value)} />
            <TextField label={t('models.maxOutputTokens')} inputMode="numeric" required value={maxOutput} onChange={event => setMaxOutput(event.target.value)} />
          </div>
          <MultiSelectField
            label={t('models.col.capabilities')}
            placeholder={t('models.selectCapabilities')}
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
            <Button variant="secondary" onClick={() => setModelDrawerOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('models.registerModel')}</Button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={depDrawerOpen}
        onClose={() => setDepDrawerOpen(false)}
        title={t('models.drawerDepTitle')}
        subtitle={t('models.drawerDepSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleCreateDeployment}>
          <SelectField
            label={t('models.logicalModel')}
            value={selectedModel}
            placeholder={t('models.selectLogicalModel')}
            onChange={setSelectedModel}
            options={models.map(item => ({value: item.id, label: item.slug}))}
          />
          <SelectField
            label={t('models.providerConnection')}
            value={selectedConnection}
            placeholder={t('models.selectConnection')}
            onChange={setSelectedConnection}
            options={connections.map(item => ({value: item.id, label: `${item.name} (${item.billing_mode})`}))}
          />
          <TextField label={t('models.upstreamModelName')} required value={upstreamName} onChange={event => setUpstreamName(event.target.value)} />
          <div className="tp-grid-2">
            {rateFields.map(key => (
              <TextField
                key={key}
                label={t(`models.rate.${key}`)}
                inputMode="numeric"
                required
                value={rates[key]}
                onChange={event => setRates(curr => ({...curr, [key]: event.target.value}))}
              />
            ))}
          </div>
          <CheckboxField
            label={t('models.selfHostedZero')}
            description={t('models.selfHostedZeroDesc')}
            checked={selfHostedZero}
            onChange={setSelfHostedZero}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setDepDrawerOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('models.saveDeployment')}</Button>
          </div>
        </form>
      </Drawer>

      <ConfirmDialog
        open={pendingAction !== null}
        onClose={() => setPendingAction(null)}
        title={pendingAction?.action === 'delete' ? t('models.confirmDeleteTitle') : t('models.confirmDisableTitle')}
        description={t(
          pendingAction?.action === 'delete' ? 'models.confirmDeleteDesc' : 'models.confirmDisableDesc',
          {
            name: pendingAction?.deployment.upstream_model_name ?? '',
            routes: pendingAction?.lastHealthyRoutes.join(', ') || t('common.none'),
          },
        )}
        confirmLabel={pendingAction?.action === 'delete' ? t('models.action.delete') : t('models.action.disable')}
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
  const numericRates = Object.fromEntries(rateFields.map(key => [key, Number(rates[key])])) as Record<RateField, number>;
  if (rateFields.some(key => !Number.isSafeInteger(numericRates[key]) || numericRates[key] < 0)) {
    return {error: translateRuntime('models.errRateIntegers')};
  }
  const zeroRate = Object.values(numericRates).every(rate => rate === 0);
  if (zeroRate && billingMode !== 'SELF_HOSTED_INFRA') {
    return {error: translateRuntime('models.errZeroRateSelfHosted')};
  }
  if (zeroRate && !confirmZeroRate) {
    return {error: translateRuntime('models.errZeroRateConfirm')};
  }
  return {rateCard: {...numericRates, self_hosted_zero_rate: zeroRate && confirmZeroRate}};
}

function errText(cause: unknown, t: (key: string) => string): string {
  return cause instanceof Error ? cause.message : t('common.requestFailed');
}
