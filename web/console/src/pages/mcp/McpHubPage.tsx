import {useCallback, useEffect, useState, type FormEvent} from 'react';
import {Plus, RefreshCw, ShieldCheck} from 'lucide-react';
import {
  approveMcpToolSchema,
  createMcpToolGrant,
  listMcpServers,
  listVirtualKeys,
  registerMcpServer,
  syncMcpServerTools,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {DataTable} from '../../ui/DataTable';
import {Drawer, Modal} from '../../ui/Overlays';
import {Banner, Button, CheckboxField, SectionHeader, SelectField, StatusPill, TextField, formatOperationalState, stateTone} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type McpServer = components['schemas']['McpServerResource'];
type VirtualKey = components['schemas']['VirtualKeySummary'];
type SyncedTool = {id: string; name: string; schema_hash: string; drift_locked: boolean; version: number};

const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];
const approveSchemaRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function McpHubPage({projectId, environmentId, roles}: {projectId: string; environmentId: string; roles: Role[]}) {
  const {t, formatNumber} = useI18n();
  const [servers, setServers] = useState<McpServer[]>([]);
  const [keys, setKeys] = useState<VirtualKey[]>([]);
  const [toolsByServer, setToolsByServer] = useState<Record<string, SyncedTool[]>>({});
  const [selectedKeys, setSelectedKeys] = useState<Record<string, string>>({});

  const [registerOpen, setRegisterOpen] = useState(false);
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [allowPrivateIp, setAllowPrivateIp] = useState(false);

  const [schemaModal, setSchemaModal] = useState<{server: McpServer; tool: SyncedTool} | null>(null);
  const [riskTier, setRiskTier] = useState<'read_only' | 'mutating' | 'destructive'>('read_only');
  const [justification, setJustification] = useState('');

  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const canManage = roles.some(role => manageRoles.includes(role));
  const canApproveSchema = roles.some(role => approveSchemaRoles.includes(role));

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const [serverRows, keyRows] = await Promise.all([
        listMcpServers(),
        projectId && environmentId ? listVirtualKeys(projectId, environmentId) : Promise.resolve([]),
      ]);
      setServers(serverRows);
      setKeys(keyRows);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('mcp.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [projectId, environmentId, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function handleRegister(event: FormEvent) {
    event.preventDefault();
    if (!projectId || !slug.trim() || !name.trim() || !endpoint.trim()) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await registerMcpServer({
        project_id: projectId,
        slug: slug.trim(),
        name: name.trim(),
        transport: 'streamable_http',
        endpoint_url: endpoint.trim(),
        allow_private_ip: allowPrivateIp || undefined,
      });
      setRegisterOpen(false);
      setSlug('');
      setName('');
      setEndpoint('');
      setAllowPrivateIp(false);
      await refresh();
      setNotice(t('mcp.serverRegistered'));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('mcp.registerFailed'));
    } finally {
      setBusy(false);
    }
  }

  async function handleSync(server: McpServer) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await syncMcpServerTools(server.id);
      setToolsByServer(curr => ({...curr, [server.id]: result.tools as SyncedTool[]}));
      setNotice(t('mcp.syncResult', {synced: formatNumber(result.synced_count), name: server.name, drifted: formatNumber(result.drifted_count)}));
      await refresh();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('mcp.syncFailed'));
    } finally {
      setBusy(false);
    }
  }

  async function handleGrant(server: McpServer, tool: SyncedTool) {
    const virtualKeyId = selectedKeys[tool.id];
    if (!environmentId || !virtualKeyId) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await createMcpToolGrant(tool.id, environmentId, virtualKeyId);
      setNotice(t('mcp.toolGranted', {tool: `${server.slug}__${tool.name}`}));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('mcp.grantFailed'));
    } finally {
      setBusy(false);
    }
  }

  async function handleApproveSchema(event: FormEvent) {
    event.preventDefault();
    if (!schemaModal) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await approveMcpToolSchema(schemaModal.tool.id, schemaModal.tool.version, {
        expected_schema_hash: schemaModal.tool.schema_hash,
        risk_tier: riskTier,
        justification: justification.trim() || undefined,
      });
      const srv = schemaModal.server;
      setSchemaModal(null);
      setJustification('');
      await handleSync(srv);
      setNotice(t('mcp.schemaPinned', {tier: riskTier}));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('mcp.schemaApprovalFailed'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('mcp.title')}</h1>
          <p className="tp-page-subtitle">{t('mcp.subtitle')}</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            <Button variant="primary" size="sm" disabled={!projectId} onClick={() => setRegisterOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>{t('mcp.registerServer')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      <section className="tp-section">
        <SectionHeader title={t('mcp.registeredServers')} count={servers.length} />
        <DataTable
          ariaLabel={t('mcp.registeredServers')}
          loading={loading}
          rows={servers}
          rowKey={row => row.id}
          emptyTitle={t('mcp.emptyServersTitle')}
          emptyDescription={t('mcp.emptyServersDesc')}
          emptyAction={canManage && projectId ? <Button variant="primary" size="sm" onClick={() => setRegisterOpen(true)}>{t('mcp.registerServer')}</Button> : undefined}
          columns={[
            {
              id: 'name',
              header: t('mcp.col.server'),
              primary: true,
              width: '24%',
              render: row => (
                <div className="tp-stack-sm">
                  <span style={{fontWeight: 600}}>{row.name}</span>
                  <span className="mono tp-muted">{row.slug}</span>
                </div>
              ),
            },
            {id: 'endpoint', header: t('mcp.col.endpoint'), width: '30%', render: row => <span className="mono">{row.endpoint_url}</span>},
            {id: 'state', header: t('mcp.col.schemaState'), width: '18%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
            {id: 'tools', header: t('mcp.col.tools'), width: '10%', render: row => <span className="num">{formatNumber(row.tool_count)}</span>},
            {
              id: 'actions',
              header: t('common.actions'),
              width: '18%',
              render: row => (
                <Button variant="secondary" size="sm" disabled={busy || !canManage} onClick={() => void handleSync(row)}>
                  <RefreshCw size={13} aria-hidden="true" />
                  <span>{t('mcp.syncTools')}</span>
                </Button>
              ),
            },
          ]}
        />
      </section>

      {servers.map(server => {
        const tools = toolsByServer[server.id] ?? [];
        if (tools.length === 0) return null;
        return (
          <section key={server.id} className="tp-section">
            <SectionHeader title={t('mcp.serverTools', {name: server.name})} count={tools.length} />
            <DataTable
              ariaLabel={t('mcp.serverToolsAria', {name: server.name})}
              rows={tools}
              rowKey={row => row.id}
              columns={[
                {id: 'name', header: t('mcp.col.tool'), primary: true, width: '20%', render: row => <span className="mono" style={{fontWeight: 600}}>{server.slug}__{row.name}</span>},
                {id: 'hash', header: t('mcp.col.observedHash'), width: '24%', render: row => <span className="mono tp-muted">{row.schema_hash}</span>},
                {
                  id: 'drift',
                  header: t('mcp.col.schemaReview'),
                  width: '22%',
                  render: row => (
                    <div className="tp-row">
                      <StatusPill
                        tone={row.drift_locked ? 'danger' : 'ok'}
                        label={row.drift_locked ? formatOperationalState('DRIFT_LOCKED') : t('mcp.pinned')}
                      />
                      {row.drift_locked && canApproveSchema ? (
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => {
                            setSchemaModal({server, tool: row});
                            setRiskTier('read_only');
                            setJustification('');
                          }}
                        >
                          <ShieldCheck size={13} aria-hidden="true" />
                          <span>{t('mcp.approveSchema')}</span>
                        </Button>
                      ) : null}
                    </div>
                  ),
                },
                {
                  id: 'grant',
                  header: t('mcp.col.grantKey'),
                  width: '34%',
                  render: row => (
                    <div className="tp-row" style={{alignItems: 'flex-end'}}>
                      <div style={{flex: '1 1 130px', minWidth: 0}}>
                        <SelectField
                          label={t('mcp.keyForTool', {tool: row.name})}
                          isLabelHidden
                          size="sm"
                          value={selectedKeys[row.id] ?? ''}
                          disabled={!canManage || busy}
                          onChange={next => setSelectedKeys(curr => ({...curr, [row.id]: next}))}
                          options={[
                            {value: '', label: t('mcp.selectKey')},
                            ...keys.map(key => ({value: key.id, label: `${key.name} (…${key.last_four})`})),
                          ]}
                        />
                      </div>
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={!canManage || busy || !selectedKeys[row.id] || row.drift_locked}
                        onClick={() => void handleGrant(server, row)}
                      >
                        {t('mcp.grant')}
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          </section>
        );
      })}

      <Drawer
        open={registerOpen}
        onClose={() => setRegisterOpen(false)}
        title={t('mcp.drawerTitle')}
        subtitle={t('mcp.drawerSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setRegisterOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-register-mcp-form" variant="primary" loading={busy}>{t('mcp.registerServer')}</Button>
          </>
        }
      >
        <form id="tp-register-mcp-form" className="tp-stack" onSubmit={handleRegister}>
          <TextField label={t('mcp.serverSlug')} required placeholder={t('mcp.serverSlugPlaceholder')} value={slug} onChange={event => setSlug(event.target.value)} />
          <TextField label={t('mcp.displayName')} required placeholder={t('mcp.displayNamePlaceholder')} value={name} onChange={event => setName(event.target.value)} />
          <TextField label={t('mcp.endpointUrl')} type="url" required placeholder="https://mcp.example.com/mcp" value={endpoint} onChange={event => setEndpoint(event.target.value)} />
          <CheckboxField
            label={t('mcp.allowPrivate')}
            description={t('mcp.allowPrivateDesc')}
            checked={allowPrivateIp}
            onChange={setAllowPrivateIp}
          />
        </form>
      </Drawer>

      <Modal
        open={schemaModal !== null}
        onClose={() => setSchemaModal(null)}
        title={t('mcp.approveModalTitle', {tool: schemaModal?.tool.name ?? ''})}
        subtitle={t('mcp.approveModalSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setSchemaModal(null)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-approve-schema-form" variant="primary" loading={busy}>{t('mcp.pinSchema')}</Button>
          </>
        }
      >
        {schemaModal ? (
          <form id="tp-approve-schema-form" className="tp-stack" onSubmit={handleApproveSchema}>
            <div className="tp-stack-sm">
              <span className="tp-label">{t('mcp.observedSha256')}</span>
              <span className="mono">{schemaModal.tool.schema_hash}</span>
            </div>
            <SelectField
              label={t('mcp.riskClassification')}
              value={riskTier}
              onChange={next => setRiskTier(next as typeof riskTier)}
              options={[
                {value: 'read_only', label: t('mcp.risk.read_only')},
                {value: 'mutating', label: t('mcp.risk.mutating')},
                {value: 'destructive', label: t('mcp.risk.destructive')},
              ]}
            />
            <TextField label={t('mcp.justification')} value={justification} onChange={event => setJustification(event.target.value)} />
          </form>
        ) : null}
      </Modal>
    </div>
  );
}
