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

  // Adapter inspection & filtering
  const [adapterQuery, setAdapterQuery] = useState('');
  const [protocolFilter, setProtocolFilter] = useState('ALL');
  const [tierFilter, setTierFilter] = useState('ALL');
  const [inspectedAdapter, setInspectedAdapter] = useState<Adapter | null>(null);

  // Create connection drawer
  const [createOpen, setCreateOpen] = useState(false);
  const [connName, setConnName] = useState('');
  const [adapterId, setAdapterId] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [billingMode, setBillingMode] = useState<'COMMERCIAL_API' | 'SELF_HOSTED_INFRA'>('COMMERCIAL_API');
  const [region, setRegion] = useState<components['schemas']['DataRegion']>('US');
  const [bindings, setBindings] = useState<Record<string, string>>({});
  const [allowPrivateIp, setAllowPrivateIp] = useState(false);
  const [cidrs, setCidrs] = useState('');

  // Edit connection drawer
  const [editing, setEditing] = useState<Connection | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftEndpoint, setDraftEndpoint] = useState('');
  const [draftBindings, setDraftBindings] = useState<Record<string, string>>({});
  const [conflictServer, setConflictServer] = useState<Record<string, unknown> | null>(null);

  // OAuth modal
  const [oauthTarget, setOauthTarget] = useState<Connection | null>(null);
  const [oauthAuthUrl, setOauthAuthUrl] = useState('');
  const [oauthTokenUrl, setOauthTokenUrl] = useState('');
  const [oauthClientId, setOauthClientId] = useState('');
  const [oauthScopes, setOauthScopes] = useState('');
  const [oauthResource, setOauthResource] = useState('');

  // Delete connection confirmation
  const [deleteTarget, setDeleteTarget] = useState<Connection | null>(null);
  const [deleteImpactRoutes, setDeleteImpactRoutes] = useState<string[]>([]);

  // Seal secret drawer & delete secret confirmation
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
      setError(errText(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void reload(); }, [reload]);

  useEffect(() => {
    if (new URLSearchParams(search).get('oauth') === 'connected') {
      setNotice('OAuth 2.1 authorization completed. Provider tokens are sealed in the write-only vault.');
    }
  }, [search]);

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
      setError(errText(cause));
    }
  }

  async function handleCreateConnection(event: FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    const allowedCidrs = cidrs.split(',').map(v => v.trim()).filter(Boolean);
    if (allowPrivateIp && (allowedCidrs.length === 0 || allowedCidrs.some(v => v === '*' || v === '0.0.0.0/0' || v === '::/0'))) {
      setError('Private upstream access requires explicit non-wildcard CIDRs.');
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
      setNotice('Connection created. Run a live probe to verify connectivity.');
    } catch (cause) {
      setError(errText(cause));
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
      if (result.valid) setNotice(`${connection.name}: probe succeeded in ${result.latency_ms} ms.`);
      else setError(`${connection.name}: ${result.sanitized_message ?? result.failure_code ?? 'Connection probe failed.'}`);
    } catch (cause) {
      setError(errText(cause));
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
      setNotice('Connection updated. Credential values remain write-only.');
    } catch (cause) {
      const err = cause as Error & {status?: number; currentResource?: Record<string, unknown>};
      if (err.status === 409) {
        setConflictServer(err.currentResource ?? {});
        setError('This connection changed on the server. Review the latest state before retrying.');
      } else {
        setError(errText(cause));
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
      setNotice(`Disabled ${connection.name}.`);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function openDeleteConnection(connection: Connection) {
    setError('');
    try {
      const [depRows, routeRows] = await Promise.all([listAllDeployments(), listAllRoutes()]);
      const depIds = new Set(depRows.filter(d => d.connection_id === connection.id).map(d => d.id));
      setDeleteImpactRoutes(routeRows.filter(r => r.targets.some(t => depIds.has(t.deployment_id))).map(r => r.slug));
      setDeleteTarget(connection);
    } catch (cause) {
      setError(errText(cause));
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
      setNotice('Connection deleted.');
    } catch (cause) {
      setDeleteTarget(null);
      setError(errText(cause));
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
      if (!started.authorization_url) throw new Error('OAuth broker did not return an authorization URL.');
      window.location.assign(started.authorization_url);
    } catch (cause) {
      setError(errText(cause));
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
      setNotice(`Sealed credential "${saved.name}" (fingerprint ${saved.trailing_fingerprint}). Plaintext cannot be read again.`);
    } catch (cause) {
      setError(errText(cause));
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
      setNotice('Credential deleted from vault.');
    } catch (cause) {
      setDeleteSecretTarget(null);
      setError(errText(cause));
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
          <h1>Providers</h1>
          <p className="tp-page-subtitle">Upstream provider connections, write-only vault credentials, and protocol adapters.</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            <Button variant="secondary" size="sm" onClick={() => setSealOpen(true)}>
              <KeyRound size={14} aria-hidden="true" />
              <span>Seal secret</span>
            </Button>
            <Button variant="primary" size="sm" onClick={() => setCreateOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>New connection</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label="Provider management views"
        activeId={tab}
        onSelect={id => {
          const next = id as 'connections' | 'secrets' | 'adapters';
          setTab(next);
          if (next === 'adapters') navigate('/providers/adapters');
          else if (pathname.startsWith('/providers/adapters') || pathname.startsWith('/catalog/providers')) navigate('/providers/connections');
        }}
        items={[
          {id: 'connections', label: 'Connections', count: connections.length},
          {id: 'secrets', label: 'Vault Secrets', count: secrets.length},
          {id: 'adapters', label: 'Adapters', count: adapters.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {tab === 'connections' ? (
        <section className="tp-section">
          <SectionHeader title="Configured connections" count={connections.length} />
          <DataTable
            ariaLabel="Provider connections"
            loading={loading}
            rows={connections}
            rowKey={row => row.id}
            emptyTitle="No provider connections"
            emptyDescription="Create a connection to bind a protocol adapter and write-only vault secret."
            emptyAction={canManage ? <Button variant="primary" size="sm" onClick={() => setCreateOpen(true)}>New connection</Button> : undefined}
            columns={[
              {
                id: 'name',
                header: 'Connection',
                primary: true,
                width: '24%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span style={{fontWeight: 600}}>{row.name}</span>
                    <span className="mono tp-muted">{row.endpoint_url}</span>
                  </div>
                ),
              },
              {id: 'adapter', header: 'Adapter', width: '13%', render: row => <span className="mono">{row.adapter_id}</span>},
              {id: 'region', header: 'Region / Billing', width: '15%', render: row => <span>{row.data_region} · {row.billing_mode === 'SELF_HOSTED_INFRA' ? 'Self-hosted' : 'Commercial'}</span>},
              {id: 'state', header: 'State', width: '14%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {
                id: 'creds',
                header: 'Vault fingerprints',
                width: '22%',
                render: row => (
                  <span className="mono tp-muted">
                    {Object.entries(row.secret_slot_fingerprints).map(([slot, fp]) => `${slot}: ${fp}`).join(', ') || 'None bound'}
                  </span>
                ),
              },
              {
                id: 'actions',
                header: 'Actions',
                width: '12%',
                render: row => {
                  const supportsOAuth = adapters.find(a => a.id === row.adapter_id)?.supported_auth_methods?.includes('oauth2_pkce');
                  if (!canManage) return <span className="tp-muted">Read-only</span>;
                  return (
                    <RowActionsMenu
                      label={`Actions for ${row.name}`}
                      disabled={busy}
                      items={[
                        {id: 'probe', label: 'Probe connection', onClick: () => void handleProbe(row)},
                        ...(supportsOAuth ? [{id: 'oauth', label: 'Configure OAuth', onClick: () => setOauthTarget(row)}] : []),
                        {
                          id: 'edit',
                          label: 'Edit connection',
                          onClick: () => {
                            setEditing(row);
                            setDraftName(row.name);
                            setDraftEndpoint(row.endpoint_url);
                            setDraftBindings({});
                            setConflictServer(null);
                          },
                        },
                        ...(row.state !== 'DISABLED' ? [{id: 'disable', label: 'Disable connection', onClick: () => void handleDisable(row)}] : []),
                        {type: 'divider' as const},
                        {
                          id: 'delete',
                          label: 'Delete connection',
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
            title="Write-only vault credentials"
            count={secrets.length}
            actions={canManage ? <Button variant="primary" size="sm" onClick={() => setSealOpen(true)}>Seal secret</Button> : undefined}
          />
          <DataTable
            ariaLabel="Vault credential metadata"
            loading={loading}
            rows={secrets}
            rowKey={row => row.credential_id}
            emptyTitle="No vault credentials sealed"
            emptyDescription="Plaintext values are sent once to the vault. Only metadata and trailing fingerprints are shown."
            columns={[
              {id: 'name', header: 'Name', primary: true, width: '28%', render: row => <span style={{fontWeight: 600}}>{row.name}</span>},
              {id: 'kind', header: 'Kind', width: '18%', render: row => <span className="mono">{row.secret_kind}</span>},
              {id: 'fp', header: 'Fingerprint', width: '22%', render: row => <span className="mono">{row.trailing_fingerprint}</span>},
              {id: 'id', header: 'Credential ID', width: '20%', render: row => <span className="mono tp-muted">{row.credential_id}</span>},
              {
                id: 'actions',
                header: 'Actions',
                width: '12%',
                render: row =>
                  canManage ? (
                    <Button variant="danger-outline" size="sm" disabled={busy} onClick={() => setDeleteSecretTarget(row)}>
                      <Trash2 size={13} aria-hidden="true" />
                      <span>Delete</span>
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
              <TextField label="Search adapters" type="search" value={adapterQuery} onChange={event => setAdapterQuery(event.target.value)} />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Wire protocol"
                value={protocolFilter}
                onChange={setProtocolFilter}
                options={[
                  {value: 'ALL', label: 'All protocols'},
                  ...[...new Set(adapters.map(a => a.wire_protocol).filter((v): v is string => Boolean(v)))].sort().map(v => ({
                    value: v,
                    label: v,
                  })),
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Execution tier"
                value={tierFilter}
                onChange={setTierFilter}
                options={[
                  {value: 'ALL', label: 'All tiers'},
                  ...[...new Set(adapters.map(a => a.execution_tier))].sort().map(v => ({
                    value: v,
                    label: v,
                  })),
                ]}
              />
            </div>
          </div>

          <DataTable
            ariaLabel="Installed provider adapters"
            loading={loading}
            rows={visibleAdapters}
            rowKey={row => row.id}
            onRowClick={row => void handleInspectAdapter(row)}
            emptyTitle="No matching adapters"
            columns={[
              {
                id: 'name',
                header: 'Adapter',
                primary: true,
                width: '28%',
                render: row => (
                  <Button variant="ghost" size="sm" onClick={event => { event.stopPropagation(); void handleInspectAdapter(row); }}>
                    {row.name}
                  </Button>
                ),
              },
              {id: 'protocol', header: 'Protocol', width: '18%', render: row => <span className="mono">{row.wire_protocol ?? '—'}</span>},
              {id: 'tier', header: 'Execution tier', width: '18%', render: row => <span className="mono">{row.execution_tier}</span>},
              {id: 'trust', header: 'Origin trust', width: '18%', render: row => <StatusPill tone={stateTone(row.origin_trust_tier)} label={formatOperationalState(row.origin_trust_tier)} />},
              {id: 'state', header: 'State', width: '18%', render: row => <StatusPill tone={stateTone(row.installation_state)} label={formatOperationalState(row.installation_state)} />},
            ]}
          />
        </section>
      ) : null}

      <Drawer
        open={inspectedAdapter !== null}
        onClose={() => setInspectedAdapter(null)}
        title={inspectedAdapter?.name ?? 'Adapter'}
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
              New connection with this adapter
            </Button>
          ) : undefined
        }
      >
        {inspectedAdapter ? (
          <KeyValueList
            items={[
              {label: 'Package ID', value: inspectedAdapter.package_id, mono: true},
              {label: 'Version', value: inspectedAdapter.version, mono: true},
              {label: 'Origin trust', value: <StatusPill tone={stateTone(inspectedAdapter.origin_trust_tier)} label={formatOperationalState(inspectedAdapter.origin_trust_tier)} />},
              {label: 'Enterprise signed', value: inspectedAdapter.enterprise_signed ? 'Yes' : 'No'},
              {label: 'Execution tier', value: inspectedAdapter.execution_tier, mono: true},
              {label: 'Supported auth methods', value: (inspectedAdapter.supported_auth_methods ?? []).join(', ') || 'None declared', mono: true},
              {label: 'Secret slots', value: (inspectedAdapter.secret_slots ?? []).join(', ') || 'None', mono: true},
              {label: 'Allowed hosts', value: (inspectedAdapter.allowed_hosts ?? []).join(', ') || 'None declared', mono: true},
            ]}
          />
        ) : null}
      </Drawer>

      <Drawer
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        title="New provider connection"
        subtitle="Bind an enabled adapter, upstream endpoint, and write-only secret slots."
      >
        <form className="tp-stack" onSubmit={handleCreateConnection}>
          <TextField label="Connection name" required value={connName} onChange={event => setConnName(event.target.value)} />
          <SelectField
            label="Provider adapter"
            value={adapterId}
            placeholder="Select provider adapter"
            onChange={next => {
              setAdapterId(next);
              setBindings({});
            }}
            options={adapters
              .filter(a => a.installation_state === 'ENABLED_ACTIVE')
              .map(item => ({value: item.id, label: `${item.name} (${item.id})`}))}
          />
          <TextField label="Endpoint URL" type="url" required value={endpoint} onChange={event => setEndpoint(event.target.value)} />
          <div className="tp-grid-2">
            <SelectField
              label="Billing mode"
              value={billingMode}
              onChange={next => setBillingMode(next as typeof billingMode)}
              options={[
                {value: 'COMMERCIAL_API', label: 'Commercial API'},
                {value: 'SELF_HOSTED_INFRA', label: 'Self-hosted infra'},
              ]}
            />
            <SelectField
              label="Data region"
              value={region}
              onChange={next => setRegion(next as typeof region)}
              options={[
                {value: 'US', label: 'US'},
                {value: 'EU', label: 'EU'},
                {value: 'APAC', label: 'APAC'},
                {value: 'ON_PREM', label: 'ON_PREM'},
              ]}
            />
          </div>
          {activeAdapterSlots.map(slot => (
            <SelectField
              key={slot}
              label={`Credential for ${slot}`}
              value={bindings[slot] ?? ''}
              onChange={next => setBindings(curr => ({...curr, [slot]: next}))}
              options={[
                {value: '', label: 'No credential'},
                ...secrets.map(secret => ({
                  value: secret.credential_id,
                  label: `${secret.name} · ${secret.trailing_fingerprint}`,
                })),
              ]}
            />
          ))}
          <CheckboxField
            label="Allow private or loopback upstream"
            description="Requires explicit non-wildcard CIDRs; metadata endpoints remain blocked."
            checked={allowPrivateIp}
            onChange={setAllowPrivateIp}
          />
          {allowPrivateIp ? (
            <TextField
              label="Allowed CIDRs"
              required
              placeholder="127.0.0.1/32, 10.20.0.0/24"
              value={cidrs}
              onChange={event => setCidrs(event.target.value)}
            />
          ) : null}
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setCreateOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!adapterId}>Create connection</Button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={editing !== null}
        onClose={() => { setEditing(null); setConflictServer(null); }}
        title={`Edit ${editing?.name ?? 'connection'}`}
        subtitle="Rotate individual secret slots without exposing existing plaintext."
      >
        {conflictServer ? (
          <Banner tone="warn" title="Server revision conflict (409)">
            <div className="tp-stack-sm">
              <span>Server values: {String(conflictServer.name ?? '—')} · {String(conflictServer.endpoint_url ?? '—')}</span>
              <Button variant="secondary" size="sm" onClick={() => void reload().then(() => { setConflictServer(null); setEditing(null); })}>
                Reload latest connections
              </Button>
            </div>
          </Banner>
        ) : null}
        <form className="tp-stack" onSubmit={handleSaveEdit}>
          <TextField label="Connection name" required value={draftName} onChange={event => setDraftName(event.target.value)} />
          <TextField label="Endpoint URL" type="url" required value={draftEndpoint} onChange={event => setDraftEndpoint(event.target.value)} />
          {editingAdapterSlots.map(slot => (
            <SelectField
              key={slot}
              label={`Rotate ${slot}`}
              value={draftBindings[slot] ?? ''}
              onChange={next => setDraftBindings(curr => ({...curr, [slot]: next}))}
              options={[
                {value: '', label: 'Keep current binding'},
                ...secrets.map(secret => ({
                  value: secret.credential_id,
                  label: `${secret.name} · ${secret.trailing_fingerprint}`,
                })),
              ]}
            />
          ))}
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setEditing(null)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Save changes</Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={sealOpen}
        onClose={() => { setSealOpen(false); setSecretValue(''); }}
        title="Seal vault credential"
        subtitle="Plaintext is transmitted once to the write-only vault and cleared immediately."
      >
        <form className="tp-stack" onSubmit={handleSealSecret}>
          <TextField label="Credential name" required value={secretName} onChange={event => setSecretName(event.target.value)} />
          <SelectField
            label="Secret kind"
            value={secretKind}
            onChange={next => setSecretKind(next as typeof secretKind)}
            options={[
              {value: 'API_KEY', label: 'API Key'},
              {value: 'AWS_IAM_CREDENTIALS', label: 'AWS IAM Credentials'},
              {value: 'WEBHOOK_HMAC_SECRET', label: 'Webhook HMAC Secret'},
              {value: 'TLS_CLIENT_CERT', label: 'TLS Client Certificate'},
            ]}
          />
          <TextField
            label="Secret value"
            type="password"
            autoComplete="new-password"
            required
            value={secretValue}
            onChange={event => setSecretValue(event.target.value)}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => { setSealOpen(false); setSecretValue(''); }}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Seal credential</Button>
          </div>
        </form>
      </Modal>

      <Modal
        open={oauthTarget !== null}
        onClose={() => setOauthTarget(null)}
        title={`Authorize ${oauthTarget?.name ?? 'connection'} (OAuth 2.1 PKCE)`}
        subtitle="Uses a one-time PKCE S256 verifier and seals provider tokens in the vault."
      >
        <form className="tp-stack" onSubmit={handleStartOAuth}>
          <TextField label="Authorization endpoint" type="url" required value={oauthAuthUrl} onChange={event => setOauthAuthUrl(event.target.value)} />
          <TextField label="Token endpoint" type="url" required value={oauthTokenUrl} onChange={event => setOauthTokenUrl(event.target.value)} />
          <TextField label="OAuth client ID" required value={oauthClientId} onChange={event => setOauthClientId(event.target.value)} />
          <TextField label="Scopes" required placeholder="openid profile api.read" value={oauthScopes} onChange={event => setOauthScopes(event.target.value)} />
          <TextField label="RFC 8707 resource URI" type="url" required value={oauthResource} onChange={event => setOauthResource(event.target.value)} />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setOauthTarget(null)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Authorize with provider</Button>
          </div>
        </form>
      </Modal>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete provider connection"
        description={`Delete "${deleteTarget?.name ?? ''}"? Active routes referencing its deployments: ${deleteImpactRoutes.join(', ') || 'None'}.`}
        confirmLabel="Delete connection"
        busy={busy}
        onConfirm={() => void confirmDeleteConnection()}
      />

      <ConfirmDialog
        open={deleteSecretTarget !== null}
        onClose={() => setDeleteSecretTarget(null)}
        title="Delete vault credential"
        description={`Remove "${deleteSecretTarget?.name ?? ''}" (${deleteSecretTarget?.trailing_fingerprint ?? ''})? Connections referencing it will block deletion.`}
        confirmLabel="Delete credential"
        busy={busy}
        onConfirm={() => void confirmDeleteSecret()}
      />
    </div>
  );
}

function selectedBindings(bindings: Record<string, string>) {
  const entries = Object.entries(bindings).filter(([, id]) => Boolean(id));
  return entries.length > 0 ? {secret_bindings: Object.fromEntries(entries)} : {};
}

function errText(cause: unknown): string {
  return cause instanceof Error ? cause.message : 'Request could not be completed.';
}
