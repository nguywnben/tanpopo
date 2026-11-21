import {useCallback, useEffect, useMemo, useState, type FormEvent} from 'react';
import {KeyRound, Plus, Trash2} from 'lucide-react';
import {
  createConnection,
  createSecret,
  deleteConnection,
  deleteSecret,
  getProviderAdapter,
  listAllDeployments,
  listAllRoutes,
  listConnections,
  listProviderAdapters,
  listSecretMetadata,
  testConnection,
  updateConnection,
} from '../../api/catalogClient';
import {startConnectionOAuth} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {ConfirmDialog, Drawer, Modal} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
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
type Connection = components['schemas']['ProviderConnection'];
type SecretMetadata = components['schemas']['SecretMetadata'];
type Adapter = components['schemas']['ProviderAdapterSummary'];

const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function ProvidersPage({pathname, projectId, roles}: {pathname: string; projectId: string; roles: Role[]}) {
  const {navigate, search} = useRouter();
  const {t, formatNumber} = useI18n();
  const initialTab = pathname.startsWith('/providers/adapters') || pathname.startsWith('/catalog/providers') ? 'adapters' : 'connections';
  const [tab, setTab] = useState<'connections' | 'secrets' | 'adapters'>(initialTab);

  useEffect(() => {
    if (pathname.startsWith('/providers/adapters') || pathname.startsWith('/catalog/providers')) setTab('adapters');
    else if (tab === 'adapters') setTab('connections');
  }, [pathname]);

  const [connections, setConnections] = useState<Connection[]>([]);
  const [secrets, setSecrets] = useState<SecretMetadata[]>([]);
  const [adapters, setAdapters] = useState<Adapter[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [adapterQuery, setAdapterQuery] = useState('');
  const [protocolFilter, setProtocolFilter] = useState('ALL');
  const [tierFilter, setTierFilter] = useState('ALL');
  const [inspectedAdapter, setInspectedAdapter] = useState<Adapter | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [connName, setConnName] = useState('');
  const [adapterId, setAdapterId] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [billingMode, setBillingMode] = useState<'COMMERCIAL_API' | 'SELF_HOSTED_INFRA'>('COMMERCIAL_API');
  const [region, setRegion] = useState<components['schemas']['DataRegion']>('US');
  const [bindings, setBindings] = useState<Record<string, string>>({});
  const [allowPrivateIp, setAllowPrivateIp] = useState(false);
  const [cidrs, setCidrs] = useState('');

  const [editing, setEditing] = useState<Connection | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftEndpoint, setDraftEndpoint] = useState('');
  const [draftBindings, setDraftBindings] = useState<Record<string, string>>({});
  const [conflictServer, setConflictServer] = useState<Record<string, unknown> | null>(null);

  const [oauthTarget, setOauthTarget] = useState<Connection | null>(null);
  const [oauthAuthUrl, setOauthAuthUrl] = useState('');
  const [oauthTokenUrl, setOauthTokenUrl] = useState('');
  const [oauthClientId, setOauthClientId] = useState('');
  const [oauthScopes, setOauthScopes] = useState('');
  const [oauthResource, setOauthResource] = useState('');

  const [deleteTarget, setDeleteTarget] = useState<Connection | null>(null);
  const [deleteImpactRoutes, setDeleteImpactRoutes] = useState<string[]>([]);

  const [sealOpen, setSealOpen] = useState(false);
  const [secretName, setSecretName] = useState('');
  const [secretKind, setSecretKind] = useState<components['schemas']['SecretCreateRequest']['secret_kind']>('API_KEY');
  const [secretValue, setSecretValue] = useState('');
  const [deleteSecretTarget, setDeleteSecretTarget] = useState<SecretMetadata | null>(null);

  const canManage = roles.some(role => manageRoles.includes(role));

  const reload = useCallback(async () => {
    setLoading(true);
    try {
      const [connRows, secretRows, adapterRows] = await Promise.all([
        listConnections(),
        listSecretMetadata(),
        listProviderAdapters(),
      ]);
      setConnections(connRows);
      setSecrets(secretRows);
      setAdapters(adapterRows);
      const firstEnabled = adapterRows.find(item => item.installation_state === 'ENABLED_ACTIVE');
      setAdapterId(current => current || firstEnabled?.id || '');
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => { void reload(); }, [reload]);

  useEffect(() => {
    if (new URLSearchParams(search).get('oauth') === 'connected') {
      setNotice(t('providers.oauthConnected'));
    }
  }, [search, t]);

  const visibleAdapters = useMemo(
    () =>
      adapters.filter(
        item =>
          (protocolFilter === 'ALL' || item.wire_protocol === protocolFilter) &&
          (tierFilter === 'ALL' || item.execution_tier === tierFilter) &&
          `${item.name} ${item.package_id} ${item.wire_protocol ?? ''} ${item.execution_tier}`
            .toLowerCase()
            .includes(adapterQuery.toLowerCase()),
      ),
    [adapters, protocolFilter, tierFilter, adapterQuery],
  );

  async function handleInspectAdapter(adapter: Adapter) {
    setError('');
    setInspectedAdapter(adapter);
    try {
      setInspectedAdapter(await getProviderAdapter(adapter.id));
    } catch (cause) {
      setError(errText(cause, t));
    }
  }

  async function handleCreateConnection(event: FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    const allowedCidrs = cidrs.split(',').map(v => v.trim()).filter(Boolean);
    if (allowPrivateIp && (allowedCidrs.length === 0 || allowedCidrs.some(v => v === '*' || v === '0.0.0.0/0' || v === '::/0'))) {
      setError(t('providers.errPrivateCidr'));
      return;
    }
    setBusy(true);
    try {
      await createConnection({
        name: connName.trim(),
        project_id: projectId || undefined,
        adapter_id: adapterId,
        billing_mode: billingMode,
        endpoint_url: endpoint.trim(),
        data_region: region,
        allow_private_ip: allowPrivateIp,
        ...(allowPrivateIp ? {allowed_cidrs: allowedCidrs} : {}),
        ...selectedBindings(bindings),
      });
      setCreateOpen(false);
      setConnName('');
      setEndpoint('');
      setBindings({});
      setAllowPrivateIp(false);
      setCidrs('');
      await reload();
      setNotice(t('providers.connectionCreated'));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleProbe(connection: Connection) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await testConnection(connection.id);
      if (result.valid) setNotice(t('providers.probeSucceeded', {name: connection.name, ms: formatNumber(result.latency_ms)}));
      else setError(`${connection.name}: ${result.sanitized_message ?? result.failure_code ?? t('providers.probeFailed')}`);
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleSaveEdit(event: FormEvent) {
    event.preventDefault();
    if (!editing) return;
    setBusy(true);
    setError('');
    setConflictServer(null);
    try {
      await updateConnection(editing.id, `"v${editing.version}"`, {
        name: draftName.trim(),
        endpoint_url: draftEndpoint.trim(),
        ...selectedBindings(draftBindings),
      });
      setEditing(null);
      await reload();
      setNotice(t('providers.connectionUpdated'));
    } catch (cause) {
      const err = cause as Error & {status?: number; currentResource?: Record<string, unknown>};
      if (err.status === 409) {
        setConflictServer(err.currentResource ?? {});
        setError(t('providers.conflictMessage'));
      } else {
        setError(errText(cause, t));
      }
    } finally {
      setBusy(false);
    }
  }

  async function handleDisable(connection: Connection) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updateConnection(connection.id, `"v${connection.version}"`, {state: 'DISABLED'});
      await reload();
      setNotice(t('providers.connectionDisabled', {name: connection.name}));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function openDeleteConnection(connection: Connection) {
    setError('');
    try {
      const [depRows, routeRows] = await Promise.all([listAllDeployments(), listAllRoutes()]);
      const depIds = new Set(depRows.filter(d => d.connection_id === connection.id).map(d => d.id));
      setDeleteImpactRoutes(routeRows.filter(r => r.targets.some(tg => depIds.has(tg.deployment_id))).map(r => r.slug));
      setDeleteTarget(connection);
    } catch (cause) {
      setError(errText(cause, t));
    }
  }

  async function confirmDeleteConnection() {
    if (!deleteTarget) return;
    setBusy(true);
    setError('');
    try {
      await deleteConnection(deleteTarget.id, `"v${deleteTarget.version}"`);
      setDeleteTarget(null);
      await reload();
      setNotice(t('providers.connectionDeleted'));
    } catch (cause) {
      setDeleteTarget(null);
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleStartOAuth(event: FormEvent) {
    event.preventDefault();
    if (!oauthTarget) return;
    setBusy(true);
    setError('');
    try {
      const started = await startConnectionOAuth(oauthTarget.id, {
        flow: 'authorization_code_pkce',
        authorization_endpoint: oauthAuthUrl.trim(),
        token_endpoint: oauthTokenUrl.trim(),
        client_id: oauthClientId.trim(),
        scopes: oauthScopes.split(/[,\s]+/).map(s => s.trim()).filter(Boolean),
        resource: oauthResource.trim(),
      });
      if (!started.authorization_url) throw new Error(t('providers.oauthNoUrl'));
      window.location.assign(started.authorization_url);
    } catch (cause) {
      setError(errText(cause, t));
      setBusy(false);
    }
  }

  async function handleSealSecret(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');
    setNotice('');
    const raw = secretValue;
    try {
      const saved = await createSecret({
        name: secretName.trim(),
        project_id: projectId || undefined,
        secret_kind: secretKind,
        secret_value: raw,
      });
      setSecrets(current => [saved, ...current]);
      setSecretName('');
      setSealOpen(false);
      setNotice(t('providers.secretSealed', {name: saved.name, fingerprint: saved.trailing_fingerprint}));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setSecretValue('');
      setBusy(false);
    }
  }

  async function confirmDeleteSecret() {
    if (!deleteSecretTarget) return;
    setBusy(true);
    setError('');
    try {
      await deleteSecret(deleteSecretTarget.credential_id, `"v${deleteSecretTarget.version}"`);
      setDeleteSecretTarget(null);
      await reload();
      setNotice(t('providers.secretDeleted'));
    } catch (cause) {
      setDeleteSecretTarget(null);
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  const activeAdapterSlots = adapters.find(item => item.id === adapterId)?.secret_slots ?? [];
  const editingAdapterSlots = editing ? (adapters.find(item => item.id === editing.adapter_id)?.secret_slots ?? []) : [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('providers.title')}</h1>
          <p className="tp-page-subtitle">{t('providers.subtitle')}</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            <Button variant="secondary" size="sm" onClick={() => setSealOpen(true)}>
              <KeyRound size={14} aria-hidden="true" />
              <span>{t('providers.sealSecret')}</span>
            </Button>
            <Button variant="primary" size="sm" onClick={() => setCreateOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>{t('providers.newConnection')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('providers.tabsLabel')}
        activeId={tab}
        onSelect={id => {
          const next = id as 'connections' | 'secrets' | 'adapters';
          setTab(next);
          if (next === 'adapters') navigate('/providers/adapters');
          else if (pathname.startsWith('/providers/adapters') || pathname.startsWith('/catalog/providers')) navigate('/providers/connections');
        }}
        items={[
          {id: 'connections', label: t('providers.tabConnections'), count: connections.length},
          {id: 'secrets', label: t('providers.tabSecrets'), count: secrets.length},
          {id: 'adapters', label: t('providers.tabAdapters'), count: adapters.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {tab === 'connections' ? (
        <section className="tp-section">
          <SectionHeader title={t('providers.configuredConnections')} count={connections.length} />
          <DataTable
            ariaLabel={t('providers.connectionsAria')}
            loading={loading}
            rows={connections}
            rowKey={row => row.id}
            emptyTitle={t('providers.emptyConnectionsTitle')}
            emptyDescription={t('providers.emptyConnectionsDesc')}
            emptyAction={canManage ? <Button variant="primary" size="sm" onClick={() => setCreateOpen(true)}>{t('providers.newConnection')}</Button> : undefined}
            columns={[
              {
                id: 'name',
                header: t('providers.col.connection'),
                primary: true,
                width: '24%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span style={{fontWeight: 600}}>{row.name}</span>
                    <span className="mono tp-muted">{row.endpoint_url}</span>
                  </div>
                ),
              },
              {id: 'adapter', header: t('providers.col.adapter'), width: '13%', render: row => <span className="mono">{row.adapter_id}</span>},
              {id: 'region', header: t('providers.col.regionBilling'), width: '15%', render: row => <span>{row.data_region} · {t(`common.billingShort.${row.billing_mode}`)}</span>},
              {id: 'state', header: t('common.state'), width: '14%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {
                id: 'creds',
                header: t('providers.col.vaultFingerprints'),
                width: '22%',
                render: row => (
                  <span className="mono tp-muted">
                    {Object.entries(row.secret_slot_fingerprints).map(([slot, fp]) => `${slot}: ${fp}`).join(', ') || t('common.noneBound')}
                  </span>
                ),
              },
              {
                id: 'actions',
                header: t('common.actions'),
                width: '12%',
                render: row => {
                  const supportsOAuth = adapters.find(a => a.id === row.adapter_id)?.supported_auth_methods?.includes('oauth2_pkce');
                  if (!canManage) return <span className="tp-muted">{t('common.readOnly')}</span>;
                  return (
                    <RowActionsMenu
                      label={t('common.actionsFor', {name: row.name})}
                      disabled={busy}
                      items={[
                        {id: 'probe', label: t('providers.action.probe'), onClick: () => void handleProbe(row)},
                        ...(supportsOAuth ? [{id: 'oauth', label: t('providers.action.oauth'), onClick: () => setOauthTarget(row)}] : []),
                        {
                          id: 'edit',
                          label: t('providers.action.edit'),
                          onClick: () => {
                            setEditing(row);
                            setDraftName(row.name);
                            setDraftEndpoint(row.endpoint_url);
                            setDraftBindings({});
                            setConflictServer(null);
                          },
                        },
                        ...(row.state !== 'DISABLED' ? [{id: 'disable', label: t('providers.action.disable'), onClick: () => void handleDisable(row)}] : []),
                        {type: 'divider' as const},
                        {
                          id: 'delete',
                          label: t('providers.action.delete'),
                          variant: 'destructive' as const,
                          onClick: () => void openDeleteConnection(row),
                        },
                      ]}
                    />
                  );
                },
              },
            ]}
          />
        </section>
      ) : null}

      {tab === 'secrets' ? (
        <section className="tp-section">
          <SectionHeader
            title={t('providers.vaultTitle')}
            count={secrets.length}
            actions={canManage ? <Button variant="primary" size="sm" onClick={() => setSealOpen(true)}>{t('providers.sealSecret')}</Button> : undefined}
          />
          <DataTable
            ariaLabel={t('providers.vaultAria')}
            loading={loading}
            rows={secrets}
            rowKey={row => row.credential_id}
            emptyTitle={t('providers.emptySecretsTitle')}
            emptyDescription={t('providers.emptySecretsDesc')}
            columns={[
              {id: 'name', header: t('common.name'), primary: true, width: '28%', render: row => <span style={{fontWeight: 600}}>{row.name}</span>},
              {id: 'kind', header: t('common.kind'), width: '18%', render: row => <span className="mono">{row.secret_kind}</span>},
              {id: 'fp', header: t('providers.col.fingerprint'), width: '22%', render: row => <span className="mono">{row.trailing_fingerprint}</span>},
              {id: 'id', header: t('providers.col.credentialId'), width: '20%', render: row => <span className="mono tp-muted">{row.credential_id}</span>},
              {
                id: 'actions',
                header: t('common.actions'),
                width: '12%',
                render: row =>
                  canManage ? (
                    <Button variant="danger-outline" size="sm" disabled={busy} onClick={() => setDeleteSecretTarget(row)}>
                      <Trash2 size={13} aria-hidden="true" />
                      <span>{t('common.delete')}</span>
                    </Button>
                  ) : null,
              },
            ]}
          />
        </section>
      ) : null}

      {tab === 'adapters' ? (
        <section className="tp-section">
          <div className="tp-toolbar">
            <div className="tp-toolbar-field">
              <TextField label={t('providers.searchAdapters')} type="search" value={adapterQuery} onChange={event => setAdapterQuery(event.target.value)} />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('providers.wireProtocol')}
                value={protocolFilter}
                onChange={setProtocolFilter}
                options={[
                  {value: 'ALL', label: t('providers.allProtocols')},
                  ...[...new Set(adapters.map(a => a.wire_protocol).filter((v): v is string => Boolean(v)))].sort().map(v => ({
                    value: v,
                    label: v,
                  })),
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('providers.executionTier')}
                value={tierFilter}
                onChange={setTierFilter}
                options={[
                  {value: 'ALL', label: t('providers.allTiers')},
                  ...[...new Set(adapters.map(a => a.execution_tier))].sort().map(v => ({
                    value: v,
                    label: v,
                  })),
                ]}
              />
            </div>
          </div>

          <DataTable
            ariaLabel={t('providers.adaptersAria')}
            loading={loading}
            rows={visibleAdapters}
            rowKey={row => row.id}
            onRowClick={row => void handleInspectAdapter(row)}
            emptyTitle={t('providers.emptyAdaptersTitle')}
            columns={[
              {
                id: 'name',
                header: t('providers.col.adapter'),
                primary: true,
                width: '28%',
                render: row => (
                  <Button variant="ghost" size="sm" onClick={event => { event.stopPropagation(); void handleInspectAdapter(row); }}>
                    {row.name}
                  </Button>
                ),
              },
              {id: 'protocol', header: t('providers.col.protocol'), width: '18%', render: row => <span className="mono">{row.wire_protocol ?? '—'}</span>},
              {id: 'tier', header: t('providers.executionTier'), width: '18%', render: row => <span className="mono">{row.execution_tier}</span>},
              {id: 'trust', header: t('providers.col.originTrust'), width: '18%', render: row => <StatusPill tone={stateTone(row.origin_trust_tier)} label={formatOperationalState(row.origin_trust_tier)} />},
              {id: 'state', header: t('common.state'), width: '18%', render: row => <StatusPill tone={stateTone(row.installation_state)} label={formatOperationalState(row.installation_state)} />},
            ]}
          />
        </section>
      ) : null}

      <Drawer
        open={inspectedAdapter !== null}
        onClose={() => setInspectedAdapter(null)}
        title={inspectedAdapter?.name ?? t('providers.col.adapter')}
        subtitle={inspectedAdapter ? `${inspectedAdapter.package_id} · v${inspectedAdapter.version}` : undefined}
        footer={
          inspectedAdapter?.installation_state === 'ENABLED_ACTIVE' && canManage ? (
            <Button
              variant="primary"
              onClick={() => {
                setAdapterId(inspectedAdapter.id);
                setInspectedAdapter(null);
                setTab('connections');
                setCreateOpen(true);
              }}
            >
              {t('providers.newWithAdapter')}
            </Button>
          ) : undefined
        }
      >
        {inspectedAdapter ? (
          <KeyValueList
            items={[
              {label: t('providers.packageId'), value: inspectedAdapter.package_id, mono: true},
              {label: t('common.version'), value: inspectedAdapter.version, mono: true},
              {label: t('providers.col.originTrust'), value: <StatusPill tone={stateTone(inspectedAdapter.origin_trust_tier)} label={formatOperationalState(inspectedAdapter.origin_trust_tier)} />},
              {label: t('providers.enterpriseSigned'), value: inspectedAdapter.enterprise_signed ? t('common.yes') : t('common.no')},
              {label: t('providers.executionTier'), value: inspectedAdapter.execution_tier, mono: true},
              {label: t('providers.supportedAuth'), value: (inspectedAdapter.supported_auth_methods ?? []).join(', ') || t('common.noneDeclared'), mono: true},
              {label: t('providers.secretSlots'), value: (inspectedAdapter.secret_slots ?? []).join(', ') || t('common.none'), mono: true},
              {label: t('providers.allowedHosts'), value: (inspectedAdapter.allowed_hosts ?? []).join(', ') || t('common.noneDeclared'), mono: true},
            ]}
          />
        ) : null}
      </Drawer>

      <Drawer
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        title={t('providers.createTitle')}
        subtitle={t('providers.createSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleCreateConnection}>
          <TextField label={t('providers.connectionName')} required value={connName} onChange={event => setConnName(event.target.value)} />
          <SelectField
            label={t('providers.providerAdapter')}
            value={adapterId}
            placeholder={t('providers.selectAdapter')}
            onChange={next => {
              setAdapterId(next);
              setBindings({});
            }}
            options={adapters
              .filter(a => a.installation_state === 'ENABLED_ACTIVE')
              .map(item => ({value: item.id, label: `${item.name} (${item.id})`}))}
          />
          <TextField label={t('providers.endpointUrl')} type="url" required value={endpoint} onChange={event => setEndpoint(event.target.value)} />
          <div className="tp-grid-2">
            <SelectField
              label={t('providers.billingMode')}
              value={billingMode}
              onChange={next => setBillingMode(next as typeof billingMode)}
              options={[
                {value: 'COMMERCIAL_API', label: t('common.billing.COMMERCIAL_API')},
                {value: 'SELF_HOSTED_INFRA', label: t('common.billing.SELF_HOSTED_INFRA')},
              ]}
            />
            <SelectField
              label={t('providers.dataRegion')}
              value={region}
              onChange={next => setRegion(next as typeof region)}
              options={[
                {value: 'US', label: t('common.region.US')},
                {value: 'EU', label: t('common.region.EU')},
                {value: 'APAC', label: t('common.region.APAC')},
                {value: 'ON_PREM', label: t('common.region.ON_PREM')},
              ]}
            />
          </div>
          {activeAdapterSlots.map(slot => (
            <SelectField
              key={slot}
              label={t('providers.credentialFor', {slot})}
              value={bindings[slot] ?? ''}
              onChange={next => setBindings(curr => ({...curr, [slot]: next}))}
              options={[
                {value: '', label: t('providers.noCredential')},
                ...secrets.map(secret => ({
                  value: secret.credential_id,
                  label: `${secret.name} · ${secret.trailing_fingerprint}`,
                })),
              ]}
            />
          ))}
          <CheckboxField
            label={t('providers.allowPrivate')}
            description={t('providers.allowPrivateDesc')}
            checked={allowPrivateIp}
            onChange={setAllowPrivateIp}
          />
          {allowPrivateIp ? (
            <TextField
              label={t('providers.allowedCidrs')}
              required
              placeholder="127.0.0.1/32, 10.20.0.0/24"
              value={cidrs}
              onChange={event => setCidrs(event.target.value)}
            />
          ) : null}
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setCreateOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!adapterId}>{t('providers.createConnection')}</Button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={editing !== null}
        onClose={() => { setEditing(null); setConflictServer(null); }}
        title={t('providers.editTitle', {name: editing?.name ?? ''})}
        subtitle={t('providers.editSubtitle')}
      >
        {conflictServer ? (
          <Banner tone="warn" title={t('providers.conflictTitle')}>
            <div className="tp-stack-sm">
              <span>{t('providers.serverValues', {name: String(conflictServer.name ?? '—'), endpoint: String(conflictServer.endpoint_url ?? '—')})}</span>
              <Button variant="secondary" size="sm" onClick={() => void reload().then(() => { setConflictServer(null); setEditing(null); })}>
                {t('providers.reloadLatest')}
              </Button>
            </div>
          </Banner>
        ) : null}
        <form className="tp-stack" onSubmit={handleSaveEdit}>
          <TextField label={t('providers.connectionName')} required value={draftName} onChange={event => setDraftName(event.target.value)} />
          <TextField label={t('providers.endpointUrl')} type="url" required value={draftEndpoint} onChange={event => setDraftEndpoint(event.target.value)} />
          {editingAdapterSlots.map(slot => (
            <SelectField
              key={slot}
              label={t('providers.rotateSlot', {slot})}
              value={draftBindings[slot] ?? ''}
              onChange={next => setDraftBindings(curr => ({...curr, [slot]: next}))}
              options={[
                {value: '', label: t('providers.keepBinding')},
                ...secrets.map(secret => ({
                  value: secret.credential_id,
                  label: `${secret.name} · ${secret.trailing_fingerprint}`,
                })),
              ]}
            />
          ))}
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setEditing(null)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('common.saveChanges')}</Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={sealOpen}
        onClose={() => { setSealOpen(false); setSecretValue(''); }}
        title={t('providers.sealModalTitle')}
        subtitle={t('providers.sealModalSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleSealSecret}>
          <TextField label={t('providers.credentialName')} required value={secretName} onChange={event => setSecretName(event.target.value)} />
          <SelectField
            label={t('providers.secretKind')}
            value={secretKind}
            onChange={next => setSecretKind(next as typeof secretKind)}
            options={[
              {value: 'API_KEY', label: t('providers.secretKind.API_KEY')},
              {value: 'AWS_IAM_CREDENTIALS', label: t('providers.secretKind.AWS_IAM_CREDENTIALS')},
              {value: 'WEBHOOK_HMAC_SECRET', label: t('providers.secretKind.WEBHOOK_HMAC_SECRET')},
              {value: 'TLS_CLIENT_CERT', label: t('providers.secretKind.TLS_CLIENT_CERT')},
            ]}
          />
          <TextField
            label={t('providers.secretValue')}
            type="password"
            autoComplete="new-password"
            required
            value={secretValue}
            onChange={event => setSecretValue(event.target.value)}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => { setSealOpen(false); setSecretValue(''); }}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('providers.sealCredential')}</Button>
          </div>
        </form>
      </Modal>

      <Modal
        open={oauthTarget !== null}
        onClose={() => setOauthTarget(null)}
        title={t('providers.oauthTitle', {name: oauthTarget?.name ?? ''})}
        subtitle={t('providers.oauthSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleStartOAuth}>
          <TextField label={t('providers.oauthAuthEndpoint')} type="url" required value={oauthAuthUrl} onChange={event => setOauthAuthUrl(event.target.value)} />
          <TextField label={t('providers.oauthTokenEndpoint')} type="url" required value={oauthTokenUrl} onChange={event => setOauthTokenUrl(event.target.value)} />
          <TextField label={t('providers.oauthClientId')} required value={oauthClientId} onChange={event => setOauthClientId(event.target.value)} />
          <TextField label={t('providers.oauthScopes')} required placeholder="openid profile api.read" value={oauthScopes} onChange={event => setOauthScopes(event.target.value)} />
          <TextField label={t('providers.oauthResource')} type="url" required value={oauthResource} onChange={event => setOauthResource(event.target.value)} />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setOauthTarget(null)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('providers.oauthSubmit')}</Button>
          </div>
        </form>
      </Modal>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={t('providers.deleteConnTitle')}
        description={t('providers.deleteConnDesc', {name: deleteTarget?.name ?? '', routes: deleteImpactRoutes.join(', ') || t('common.none')})}
        confirmLabel={t('providers.action.delete')}
        busy={busy}
        onConfirm={() => void confirmDeleteConnection()}
      />

      <ConfirmDialog
        open={deleteSecretTarget !== null}
        onClose={() => setDeleteSecretTarget(null)}
        title={t('providers.deleteSecretTitle')}
        description={t('providers.deleteSecretDesc', {name: deleteSecretTarget?.name ?? '', fingerprint: deleteSecretTarget?.trailing_fingerprint ?? ''})}
        confirmLabel={t('providers.deleteCredential')}
        busy={busy}
        onConfirm={() => void confirmDeleteSecret()}
      />
    </div>
  );
}

function selectedBindings(raw: Record<string, string>) {
  const entries = Object.entries(raw).filter(([, value]) => Boolean(value));
  return entries.length > 0 ? {secret_bindings: Object.fromEntries(entries)} : {};
}

function errText(cause: unknown, t: (key: string) => string): string {
  return cause instanceof Error ? cause.message : t('common.requestFailed');
}
