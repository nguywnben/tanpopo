import {useCallback, useEffect, useRef, useState, type FormEvent} from 'react';
import {Plus, RefreshCw, ShieldCheck} from 'lucide-react';
import {
  approveMcpToolSchema,
  cancelA2aTask,
  createMcpToolGrant,
  listA2aAgents,
  listA2aTasks,
  listMcpServers,
  listVirtualKeys,
  registerA2aAgent,
  registerMcpServer,
  resumeA2aTask,
  syncMcpServerTools,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {DataTable} from '../../ui/DataTable';
import {Drawer, Modal} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
  formatMicroUSD,
  formatOperationalState,
  stateTone,
  type StatusTone,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type McpServer = components['schemas']['McpServerResource'];
type A2aAgent = components['schemas']['A2aAgentResource'];
type A2aTask = components['schemas']['A2aTaskResource'];
type DataRegion = components['schemas']['DataRegion'];
type VirtualKey = components['schemas']['VirtualKeySummary'];
type SyncedTool = {id: string; name: string; schema_hash: string; drift_locked: boolean; version: number};

const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];
const approveSchemaRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

function a2aTaskTone(state: A2aTask['state']): StatusTone {
  switch (state) {
    case 'completed':
      return 'ok';
    case 'input-required':
      return 'warn';
    case 'failed':
      return 'danger';
    case 'canceled':
      return 'neutral';
    default:
      return 'info';
  }
}

export function McpHubPage({
  pathname = '/mcp/servers',
  projectId,
  environmentId,
  roles,
}: {
  pathname?: string;
  projectId: string;
  environmentId: string;
  roles: Role[];
}) {
  const {t, formatNumber} = useI18n();
  const [tab, setTab] = useState<'mcp' | 'a2a'>(pathname.startsWith('/a2a') ? 'a2a' : 'mcp');
  const [servers, setServers] = useState<McpServer[]>([]);
  const [a2aAgents, setA2aAgents] = useState<A2aAgent[]>([]);
  const [a2aTasks, setA2aTasks] = useState<A2aTask[]>([]);
  const [keys, setKeys] = useState<VirtualKey[]>([]);
  const [toolsByServer, setToolsByServer] = useState<Record<string, SyncedTool[]>>({});
  const [selectedKeys, setSelectedKeys] = useState<Record<string, string>>({});

  const [registerOpen, setRegisterOpen] = useState(false);
  const [mcpFormError, setMcpFormError] = useState('');
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [allowPrivateIp, setAllowPrivateIp] = useState(false);
  const [allowedCidrs, setAllowedCidrs] = useState('');

  const [a2aRegisterOpen, setA2aRegisterOpen] = useState(false);
  const [a2aFormError, setA2aFormError] = useState('');
  const [agentSlug, setAgentSlug] = useState('');
  const [agentName, setAgentName] = useState('');
  const [agentEndpoint, setAgentEndpoint] = useState('');
  const [agentRegion, setAgentRegion] = useState<DataRegion>('US');
  const [agentMaxHops, setAgentMaxHops] = useState('5');
  const [agentAllowPrivateIp, setAgentAllowPrivateIp] = useState(false);
  const [agentAllowedCidrs, setAgentAllowedCidrs] = useState('');

  const [resumeTarget, setResumeTarget] = useState<A2aTask | null>(null);
  const [resumeInputText, setResumeInputText] = useState('');

  const [schemaModal, setSchemaModal] = useState<{server: McpServer; tool: SyncedTool} | null>(null);
  const [riskTier, setRiskTier] = useState<'read_only' | 'mutating' | 'destructive'>('read_only');
  const [justification, setJustification] = useState('');

  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const refreshSeqRef = useRef(0);

  const canManage = roles.some(role => manageRoles.includes(role));
  const canApproveSchema = roles.some(role => approveSchemaRoles.includes(role));

  useEffect(() => {
    setTab(pathname.startsWith('/a2a') ? 'a2a' : 'mcp');
  }, [pathname]);

  const refresh = useCallback(async () => {
    const seq = ++refreshSeqRef.current;
    if (!projectId || !environmentId) {
      setKeys([]);
    }
    setLoading(true);
    setError('');
    try {
      const [serverRows, agentRows, taskRows, keyRows] = await Promise.all([
        listMcpServers(),
        listA2aAgents(),
        listA2aTasks(),
        projectId && environmentId ? listVirtualKeys(projectId, environmentId) : Promise.resolve([]),
      ]);
      if (refreshSeqRef.current !== seq) return;
      setServers(serverRows);
      setA2aAgents(agentRows);
      setA2aTasks(taskRows);
      setKeys(keyRows);
    } catch (cause) {
      if (refreshSeqRef.current !== seq) return;
      setError(cause instanceof Error ? cause.message : t('mcp.loadFailed'));
    } finally {
      if (refreshSeqRef.current === seq) setLoading(false);
    }
  }, [projectId, environmentId, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function handleRegister(event: FormEvent) {
    event.preventDefault();
    if (!projectId || !slug.trim() || !name.trim() || !endpoint.trim()) return;
    setMcpFormError('');
    setError('');
    setNotice('');
    const parsedCidrs = allowPrivateIp
      ? [...new Set(allowedCidrs.split(',').map(v => v.trim()).filter(Boolean))]
      : [];
    if (allowPrivateIp && (parsedCidrs.length === 0 || parsedCidrs.some(c => !isValidCidr(c)))) {
      setMcpFormError(t('providers.errPrivateCidr'));
      return;
    }
    setBusy(true);
    try {
      await registerMcpServer({
        project_id: projectId,
        slug: slug.trim(),
        name: name.trim(),
        transport: 'streamable_http',
        endpoint_url: endpoint.trim(),
        allow_private_ip: allowPrivateIp || undefined,
        ...(parsedCidrs.length > 0 ? {allowed_cidrs: parsedCidrs} : {}),
      });
      setRegisterOpen(false);
      setSlug('');
      setName('');
      setEndpoint('');
      setAllowPrivateIp(false);
      setAllowedCidrs('');
      await refresh();
      setNotice(t('mcp.serverRegistered'));
    } catch (cause) {
      setMcpFormError(cause instanceof Error ? cause.message : t('mcp.registerFailed'));
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
      const syncedTools = result.tools as SyncedTool[];
      const pendingApproval = Math.max(result.drifted_count, syncedTools.filter(tl => tl.drift_locked).length);
      setToolsByServer(curr => ({...curr, [server.id]: syncedTools}));
      setNotice(t('mcp.syncResult', {synced: formatNumber(result.synced_count), name: server.name, drifted: formatNumber(pendingApproval)}));
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

  async function handleRegisterA2a(event: FormEvent) {
    event.preventDefault();
    if (!projectId || !agentName.trim() || !agentEndpoint.trim()) return;
    setA2aFormError('');
    setError('');
    setNotice('');
    const parsedHops = Math.min(5, Math.max(1, Number.parseInt(agentMaxHops, 10) || 5));
    const parsedCidrs = agentAllowPrivateIp
      ? [...new Set(agentAllowedCidrs.split(',').map(v => v.trim()).filter(Boolean))]
      : [];
    if (agentAllowPrivateIp && (parsedCidrs.length === 0 || parsedCidrs.some(c => !isValidCidr(c)))) {
      setA2aFormError(t('providers.errPrivateCidr'));
      return;
    }
    setBusy(true);
    try {
      await registerA2aAgent({
        project_id: projectId,
        agent_slug: agentSlug.trim() || undefined,
        name: agentName.trim(),
        endpoint_url: agentEndpoint.trim(),
        data_region: agentRegion,
        max_delegation_hops: parsedHops,
        allow_private_ip: agentAllowPrivateIp,
        ...(parsedCidrs.length > 0 ? {allowed_cidrs: parsedCidrs} : {}),
      });
      setA2aRegisterOpen(false);
      setAgentSlug('');
      setAgentName('');
      setAgentEndpoint('');
      setAgentRegion('US');
      setAgentMaxHops('5');
      setAgentAllowPrivateIp(false);
      setAgentAllowedCidrs('');
      await refresh();
      setNotice(t('a2a.agentRegistered'));
    } catch (cause) {
      setA2aFormError(cause instanceof Error ? cause.message : t('a2a.registerFailed'));
    } finally {
      setBusy(false);
    }
  }

  async function handleResumeTask(event: FormEvent) {
    event.preventDefault();
    if (!resumeTarget || !resumeInputText.trim()) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await resumeA2aTask(resumeTarget.task_id, [{kind: 'text', text: resumeInputText.trim()}]);
      setResumeTarget(null);
      setResumeInputText('');
      await refresh();
      setNotice(t('a2a.taskResumed'));
    } catch (cause) {
      setError(cause instanceof Error && cause.message ? cause.message : t('a2a.taskResumeFailed'));
    } finally {
      setBusy(false);
    }
  }

  async function handleCancelTask(task: A2aTask) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await cancelA2aTask(task.task_id);
      await refresh();
      setNotice(t('a2a.taskCanceled'));
    } catch (cause) {
      setError(cause instanceof Error && cause.message ? cause.message : t('a2a.taskCancelFailed'));
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
          <div
            className={
              !loading && projectId && ((tab === 'mcp' && servers.length === 0) || (tab === 'a2a' && a2aAgents.length === 0))
                ? 'tp-page-actions tp-header-cta-desktop'
                : 'tp-page-actions'
            }
          >
            <Button
              variant={
                !loading && projectId && ((tab === 'mcp' && servers.length === 0) || (tab === 'a2a' && a2aAgents.length === 0))
                  ? 'secondary'
                  : 'primary'
              }
              size="sm"
              disabled={!projectId}
              onClick={() => {
                if (tab === 'mcp') {
                  setMcpFormError('');
                  setRegisterOpen(true);
                } else {
                  setA2aFormError('');
                  setA2aRegisterOpen(true);
                }
              }}
            >
              <Plus size={14} aria-hidden="true" />
              <span>{tab === 'mcp' ? t('mcp.registerServer') : t('a2a.registerAgent')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('mcp.tabsLabel')}
        activeId={tab}
        onSelect={next => setTab(next as 'mcp' | 'a2a')}
        items={[
          {id: 'mcp', label: t('mcp.tabMcp'), count: servers.length},
          {id: 'a2a', label: t('mcp.tabA2a'), count: a2aAgents.length + a2aTasks.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {tab === 'mcp' ? (
        <>
      <section className="tp-section">
        <SectionHeader title={t('mcp.registeredServers')} count={servers.length} />
        <DataTable
          ariaLabel={t('mcp.registeredServers')}
          loading={loading}
          rows={servers}
          rowKey={row => row.id}
          emptyTitle={t('mcp.emptyServersTitle')}
          emptyDescription={t('mcp.emptyServersDesc')}
          emptyAction={
            canManage && projectId ? (
              <Button
                variant="primary"
                size="md"
                onClick={() => {
                  setMcpFormError('');
                  setRegisterOpen(true);
                }}
              >
                <Plus size={14} aria-hidden="true" />
                <span>{t('mcp.registerServer')}</span>
              </Button>
            ) : undefined
          }
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
        </>
      ) : (
        <>
          <section className="tp-section">
            <SectionHeader title={t('a2a.registeredAgents')} count={a2aAgents.length} />
            <DataTable
              ariaLabel={t('a2a.registeredAgents')}
              loading={loading}
              rows={a2aAgents}
              rowKey={row => row.agent_id}
              emptyTitle={t('a2a.emptyAgentsTitle')}
              emptyDescription={t('a2a.emptyAgentsDesc')}
              emptyAction={
                canManage && projectId ? (
                  <Button variant="primary" size="md" onClick={() => setA2aRegisterOpen(true)}>
                    <Plus size={14} aria-hidden="true" />
                    <span>{t('a2a.registerAgent')}</span>
                  </Button>
                ) : undefined
              }
              columns={[
                {
                  id: 'name',
                  header: t('a2a.col.agent'),
                  primary: true,
                  width: '26%',
                  render: row => (
                    <div className="tp-stack-sm">
                      <span style={{fontWeight: 600}}>{row.name}</span>
                      <span className="mono tp-muted">{row.agent_slug}</span>
                    </div>
                  ),
                },
                {id: 'endpoint', header: t('a2a.col.endpoint'), width: '32%', render: row => <span className="mono">{row.endpoint_url}</span>},
                {id: 'region', header: t('a2a.col.region'), width: '12%', render: row => <span className="mono">{row.data_region}</span>},
                {id: 'hops', header: t('a2a.col.maxHops'), width: '14%', render: row => <span className="num mono">{formatNumber(row.max_delegation_hops)}</span>},
                {id: 'status', header: t('common.state'), width: '16%', render: row => <StatusPill tone={stateTone(row.status)} label={formatOperationalState(row.status)} />},
              ]}
            />
          </section>

          <section className="tp-section">
            <SectionHeader title={t('a2a.activeTasks')} count={a2aTasks.length} />
            <DataTable
              ariaLabel={t('a2a.activeTasks')}
              loading={loading}
              rows={a2aTasks}
              rowKey={row => row.task_id}
              emptyTitle={t('a2a.emptyTasksTitle')}
              emptyDescription={t('a2a.emptyTasksDesc')}
              columns={[
                {
                  id: 'task',
                  header: t('a2a.col.task'),
                  primary: true,
                  width: '26%',
                  render: row => (
                    <div className="tp-stack-sm">
                      <span className="mono" style={{fontWeight: 600}}>{row.task_id}</span>
                      <span className="mono tp-muted">{row.target_agent_slug || row.target_agent_id}</span>
                      {row.prompt_message ? <span className="tp-muted">{row.prompt_message}</span> : null}
                    </div>
                  ),
                },
                {id: 'state', header: t('common.state'), width: '14%', render: row => <StatusPill tone={a2aTaskTone(row.state)} label={t(`a2a.state.${row.state}`)} />},
                {id: 'hops', header: t('a2a.col.hops'), width: '10%', render: row => <span className="num mono">{formatNumber(row.hop_count)} / 5</span>},
                {id: 'region', header: t('a2a.col.region'), width: '10%', render: row => <span className="mono">{row.enforced_region || '—'}</span>},
                {id: 'hold', header: t('a2a.col.subBudget'), width: '14%', render: row => <span className="num mono">{formatMicroUSD(row.sub_budget_hold_micros)}</span>},
                {id: 'settled', header: t('a2a.col.settledCost'), width: '14%', render: row => <span className="num mono">{formatMicroUSD(row.settled_cost_micros ?? 0)}</span>},
                {
                  id: 'actions',
                  header: t('common.actions'),
                  width: '12%',
                  render: row =>
                    canManage && (row.state === 'input-required' || row.state === 'submitted' || row.state === 'working') ? (
                      <div className="tp-row">
                        {row.state === 'input-required' ? (
                          <Button
                            variant="secondary"
                            size="sm"
                            disabled={busy}
                            onClick={() => {
                              setResumeTarget(row);
                              setResumeInputText('');
                            }}
                          >
                            {t('a2a.resumeTask')}
                          </Button>
                        ) : null}
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={busy}
                          onClick={() => void handleCancelTask(row)}
                        >
                          {t('common.cancel')}
                        </Button>
                      </div>
                    ) : (
                      <span className="tp-muted">—</span>
                    ),
                },
              ]}
            />
          </section>
        </>
      )}

      <Drawer
        open={registerOpen}
        onClose={() => {
          setRegisterOpen(false);
          setMcpFormError('');
        }}
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
          {mcpFormError ? <Banner tone="danger">{mcpFormError}</Banner> : null}
          <TextField label={t('mcp.serverSlug')} required placeholder={t('mcp.serverSlugPlaceholder')} value={slug} onChange={event => setSlug(event.target.value)} />
          <TextField label={t('mcp.displayName')} required placeholder={t('mcp.displayNamePlaceholder')} value={name} onChange={event => setName(event.target.value)} />
          <TextField label={t('mcp.endpointUrl')} type="url" required placeholder="https://mcp.example.com/mcp" value={endpoint} onChange={event => setEndpoint(event.target.value)} />
          <CheckboxField
            label={t('mcp.allowPrivate')}
            description={t('mcp.allowPrivateDesc')}
            checked={allowPrivateIp}
            onChange={setAllowPrivateIp}
          />
          {allowPrivateIp ? (
            <TextField
              label={t('providers.allowedCidrs')}
              required
              placeholder="127.0.0.1/32, 10.20.0.0/24"
              value={allowedCidrs}
              onChange={event => setAllowedCidrs(event.target.value)}
            />
          ) : null}
        </form>
      </Drawer>

      <Drawer
        open={a2aRegisterOpen}
        onClose={() => {
          setA2aRegisterOpen(false);
          setA2aFormError('');
        }}
        title={t('a2a.drawerTitle')}
        subtitle={t('a2a.drawerSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setA2aRegisterOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-register-a2a-form" variant="primary" loading={busy}>{t('a2a.registerAgent')}</Button>
          </>
        }
      >
        <form id="tp-register-a2a-form" className="tp-stack" onSubmit={handleRegisterA2a}>
          {a2aFormError ? <Banner tone="danger">{a2aFormError}</Banner> : null}
          <TextField label={t('a2a.agentSlug')} placeholder={t('a2a.agentSlugPlaceholder')} value={agentSlug} onChange={event => setAgentSlug(event.target.value)} />
          <TextField label={t('a2a.agentName')} required placeholder={t('a2a.agentNamePlaceholder')} value={agentName} onChange={event => setAgentName(event.target.value)} />
          <TextField label={t('a2a.endpointUrl')} type="url" required placeholder="https://agent.example.com" value={agentEndpoint} onChange={event => setAgentEndpoint(event.target.value)} />
          <SelectField
            label={t('a2a.col.region')}
            value={agentRegion}
            onChange={next => setAgentRegion(next as DataRegion)}
            options={[
              {value: 'US', label: 'US'},
              {value: 'EU', label: 'EU'},
              {value: 'APAC', label: 'APAC'},
              {value: 'ON_PREM', label: 'ON_PREM'},
            ]}
          />
          <SelectField
            label={t('a2a.col.maxHops')}
            value={agentMaxHops}
            onChange={setAgentMaxHops}
            options={[
              {value: '1', label: '1'},
              {value: '2', label: '2'},
              {value: '3', label: '3'},
              {value: '4', label: '4'},
              {value: '5', label: '5'},
            ]}
          />
          <CheckboxField
            label={t('a2a.allowPrivate')}
            description={t('a2a.allowPrivateDesc')}
            checked={agentAllowPrivateIp}
            onChange={setAgentAllowPrivateIp}
          />
          {agentAllowPrivateIp ? (
            <TextField
              label={t('providers.allowedCidrs')}
              required
              placeholder="127.0.0.1/32, 10.20.0.0/24"
              value={agentAllowedCidrs}
              onChange={event => setAgentAllowedCidrs(event.target.value)}
            />
          ) : null}
        </form>
      </Drawer>

      <Modal
        open={resumeTarget !== null}
        onClose={() => setResumeTarget(null)}
        title={t('a2a.resumeModalTitle', {id: resumeTarget?.task_id ?? ''})}
        subtitle={resumeTarget?.prompt_message || t('a2a.resumeModalSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setResumeTarget(null)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-resume-a2a-form" variant="primary" loading={busy} disabled={!resumeInputText.trim()}>
              {t('a2a.submitResume')}
            </Button>
          </>
        }
      >
        <form id="tp-resume-a2a-form" className="tp-stack" onSubmit={handleResumeTask}>
          <TextField
            label={t('a2a.resumeInputLabel')}
            required
            placeholder={t('a2a.resumeInputPlaceholder')}
            value={resumeInputText}
            onChange={event => setResumeInputText(event.target.value)}
          />
        </form>
      </Modal>

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

function isValidCidr(raw: string): boolean {
  const trimmed = raw.trim();
  const slashIdx = trimmed.indexOf('/');
  if (slashIdx <= 0 || slashIdx !== trimmed.lastIndexOf('/')) return false;
  const addr = trimmed.slice(0, slashIdx);
  const prefixStr = trimmed.slice(slashIdx + 1);
  if (!/^\d+$/.test(prefixStr)) return false;
  const prefix = Number(prefixStr);
  if (addr.includes('.')) {
    const octets = addr.split('.');
    if (octets.length !== 4 || octets.some(o => !/^\d{1,3}$/.test(o) || Number(o) > 255)) return false;
    return prefix >= 1 && prefix <= 32;
  }
  if (addr.includes(':')) {
    if (prefix < 1 || prefix > 128) return false;
    try {
      const parsed = new URL(`http://[${addr}]/`);
      return parsed.hostname.startsWith('[');
    } catch {
      return false;
    }
  }
  return false;
}
