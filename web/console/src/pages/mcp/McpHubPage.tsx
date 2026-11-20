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
  const [servers, setServers] = useState<McpServer[]>([]);
  const [keys, setKeys] = useState<VirtualKey[]>([]);
  const [toolsByServer, setToolsByServer] = useState<Record<string, SyncedTool[]>>({});
  const [selectedKeys, setSelectedKeys] = useState<Record<string, string>>({});

  // Register Server Drawer
  const [registerOpen, setRegisterOpen] = useState(false);
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [allowPrivateIp, setAllowPrivateIp] = useState(false);

  // Approve Schema Modal
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
      setError(cause instanceof Error ? cause.message : 'MCP inventory could not be loaded.');
    } finally {
      setLoading(false);
    }
  }, [projectId, environmentId]);

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
      setNotice('MCP server registered. Run Sync tools to pin observed schemas.');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not register MCP server.');
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
      setNotice(`Synced ${result.synced_count} tools on ${server.name}; ${result.drifted_count} schema changes require approval.`);
      await refresh();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Tool sync failed.');
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
      setNotice(`Granted ${server.slug}__${tool.name} to selected Virtual Key.`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Tool grant failed.');
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
      setNotice(`Pinned schema hash and classified risk tier (${riskTier}).`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Schema approval failed.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>MCP Hub</h1>
          <p className="tp-page-subtitle">Streamable HTTP MCP server registry, RFC 8785 JCS schema pinning, and per-tool Virtual Key grants.</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            <Button variant="primary" size="sm" disabled={!projectId} onClick={() => setRegisterOpen(true)}>
              <Plus size={14} aria-hidden="true" />
              <span>Register server</span>
            </Button>
          </div>
        ) : null}
      </div>

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      <section className="tp-section">
        <SectionHeader title="Registered MCP servers" count={servers.length} />
        <DataTable
          ariaLabel="Registered MCP servers"
          loading={loading}
          rows={servers}
          rowKey={row => row.id}
          emptyTitle="No MCP servers registered"
          emptyDescription="Register a Streamable HTTP MCP endpoint to discover tools and enforce schema pins."
          emptyAction={canManage && projectId ? <Button variant="primary" size="sm" onClick={() => setRegisterOpen(true)}>Register server</Button> : undefined}
          columns={[
            {
              id: 'name',
              header: 'Server',
              primary: true,
              width: '24%',
              render: row => (
                <div className="tp-stack-sm">
                  <span style={{fontWeight: 600}}>{row.name}</span>
                  <span className="mono tp-muted">{row.slug}</span>
                </div>
              ),
            },
            {id: 'endpoint', header: 'Endpoint', width: '30%', render: row => <span className="mono">{row.endpoint_url}</span>},
            {id: 'state', header: 'Schema state', width: '18%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
            {id: 'tools', header: 'Tools', width: '10%', render: row => <span className="num">{row.tool_count}</span>},
            {
              id: 'actions',
              header: 'Actions',
              width: '18%',
              render: row => (
                <Button variant="secondary" size="sm" disabled={busy || !canManage} onClick={() => void handleSync(row)}>
                  <RefreshCw size={13} aria-hidden="true" />
                  <span>Sync tools</span>
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
            <SectionHeader title={`${server.name} tools`} count={tools.length} />
            <DataTable
              ariaLabel={`${server.name} synchronized tools`}
              rows={tools}
              rowKey={row => row.id}
              columns={[
                {id: 'name', header: 'Tool', primary: true, width: '20%', render: row => <span className="mono" style={{fontWeight: 600}}>{server.slug}__{row.name}</span>},
                {id: 'hash', header: 'Observed JCS hash', width: '24%', render: row => <span className="mono tp-muted">{row.schema_hash}</span>},
                {
                  id: 'drift',
                  header: 'Schema review',
                  width: '22%',
                  render: row => (
                    <div className="tp-row">
                      <StatusPill
                        tone={row.drift_locked ? 'danger' : 'ok'}
                        label={row.drift_locked ? 'Drift locked' : 'Pinned'}
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
                          <span>Approve schema</span>
                        </Button>
                      ) : null}
                    </div>
                  ),
                },
                {
                  id: 'grant',
                  header: 'Grant to Virtual Key',
                  width: '34%',
                  render: row => (
                    <div className="tp-row" style={{alignItems: 'flex-end'}}>
                      <div style={{flex: '1 1 130px', minWidth: 0}}>
                        <SelectField
                          label={`Virtual key for ${row.name}`}
                          value={selectedKeys[row.id] ?? ''}
                          disabled={!canManage || busy}
                          onChange={event => setSelectedKeys(curr => ({...curr, [row.id]: event.target.value}))}
                        >
                          <option value="">Select key</option>
                          {keys.map(key => <option key={key.id} value={key.id}>{key.name} (…{key.last_four})</option>)}
                        </SelectField>
                      </div>
                      <Button
                        variant="secondary"
                        size="sm"
                        disabled={!canManage || busy || !selectedKeys[row.id] || row.drift_locked}
                        onClick={() => void handleGrant(server, row)}
                      >
                        Grant
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
        title="Register MCP server"
        subtitle="Only Streamable HTTP transport is permitted."
      >
        <form className="tp-stack" onSubmit={handleRegister}>
          <TextField label="Server slug" required placeholder="e.g. github-ops" value={slug} onChange={event => setSlug(event.target.value)} />
          <TextField label="Display name" required placeholder="e.g. GitHub Operations" value={name} onChange={event => setName(event.target.value)} />
          <TextField label="Streamable HTTP endpoint URL" type="url" required placeholder="https://mcp.example.com/mcp" value={endpoint} onChange={event => setEndpoint(event.target.value)} />
          <CheckboxField
            label="Allow private or loopback endpoint"
            description="Only enable for explicitly trusted internal MCP servers."
            checked={allowPrivateIp}
            onChange={setAllowPrivateIp}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setRegisterOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Register server</Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={schemaModal !== null}
        onClose={() => setSchemaModal(null)}
        title={`Approve schema for ${schemaModal?.tool.name ?? ''}`}
        subtitle="Pinning the JCS schema hash unlocks tool execution and sets the HITL approval risk tier."
      >
        {schemaModal ? (
          <form className="tp-stack" onSubmit={handleApproveSchema}>
            <div className="tp-stack-sm">
              <span className="tp-label">Observed SHA-256 schema hash</span>
              <span className="mono">{schemaModal.tool.schema_hash}</span>
            </div>
            <SelectField label="Risk classification" value={riskTier} onChange={event => setRiskTier(event.target.value as typeof riskTier)}>
              <option value="read_only">read_only (no HITL required)</option>
              <option value="mutating">mutating (audited mutation)</option>
              <option value="destructive">destructive (mandatory HITL approval)</option>
            </SelectField>
            <TextField label="Review justification (optional)" value={justification} onChange={event => setJustification(event.target.value)} />
            <div className="tp-row" style={{justifyContent: 'flex-end'}}>
              <Button variant="secondary" onClick={() => setSchemaModal(null)}>Cancel</Button>
              <Button type="submit" variant="primary" loading={busy}>Pin schema</Button>
            </div>
          </form>
        ) : null}
      </Modal>
    </div>
  );
}
