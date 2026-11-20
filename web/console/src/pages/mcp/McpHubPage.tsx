import {useCallback, useEffect, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {createMcpToolGrant, listMcpServers, listVirtualKeys, registerMcpServer, syncMcpServerTools} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type Server = components['schemas']['McpServerResource'];
type Key = components['schemas']['VirtualKeySummary'];
type SyncedTool = {id: string; name: string; schema_hash: string; drift_locked: boolean; version: number};
const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];

export function McpHubPage({projectId, environmentId, roles}: {projectId: string; environmentId: string; roles: Role[]}) {
  const [servers, setServers] = useState<Server[]>([]);
  const [keys, setKeys] = useState<Key[]>([]);
  const [toolsByServer, setToolsByServer] = useState<Record<string, SyncedTool[]>>({});
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [selectedKeys, setSelectedKeys] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const canManage = roles.some(role => manageRoles.includes(role));

  const refresh = useCallback(async () => {
    try {
      const [serverRows, keyRows] = await Promise.all([listMcpServers(), projectId && environmentId ? listVirtualKeys(projectId, environmentId) : Promise.resolve([])]);
      setServers(serverRows as Server[]);
      setKeys(keyRows as Key[]);
    } catch (error) { setMessage(error instanceof Error ? error.message : 'MCP inventory could not be loaded.'); }
  }, [projectId, environmentId]);
  useEffect(() => { void refresh(); }, [refresh]);

  async function addServer() {
    if (!projectId || !slug.trim() || !name.trim() || !endpoint.trim()) return;
    setBusy(true); setMessage('');
    try {
      await registerMcpServer({project_id: projectId, slug: slug.trim(), name: name.trim(), transport: 'streamable_http', endpoint_url: endpoint.trim()});
      setSlug(''); setName(''); setEndpoint(''); setMessage('MCP server registered.'); await refresh();
    } catch (error) { setMessage(error instanceof Error ? error.message : 'MCP server could not be registered.'); }
    finally { setBusy(false); }
  }

  async function sync(server: Server) {
    setBusy(true); setMessage('');
    try {
      const result = await syncMcpServerTools(server.id);
      setToolsByServer(current => ({...current, [server.id]: result.tools as SyncedTool[]}));
      setMessage(`Synced ${result.synced_count} tools; ${result.drifted_count} schema changes require review.`);
      await refresh();
    } catch (error) { setMessage(error instanceof Error ? error.message : 'MCP tools could not be synchronized.'); }
    finally { setBusy(false); }
  }

  async function grant(server: Server, tool: SyncedTool) {
    const virtualKeyId = selectedKeys[tool.id];
    if (!environmentId || !virtualKeyId) return;
    setBusy(true); setMessage('');
    try { await createMcpToolGrant(tool.id, environmentId, virtualKeyId); setMessage(`Grant created for ${server.slug}__${tool.name}.`); }
    catch (error) { setMessage(error instanceof Error ? error.message : 'MCP tool grant could not be created.'); }
    finally { setBusy(false); }
  }

  return <VStack gap={4}>
    <Heading level={1}>MCP Hub</Heading>
    <Text>Register Streamable HTTP servers, synchronize tool schemas, and grant tools to a Virtual Key in the active environment.</Text>
    {message ? <Text role="status">{message}</Text> : null}
    <Section><VStack gap={3}><Heading level={2}>Register server</Heading>
      {!canManage ? <Text role="status">Your role can inspect MCP servers but cannot register servers or grant tools.</Text> : <>
        <TextInput label="Server slug" value={slug} onChange={setSlug} isDisabled={busy} />
        <TextInput label="Server name" value={name} onChange={setName} isDisabled={busy} />
        <TextInput label="Streamable HTTP endpoint" value={endpoint} onChange={setEndpoint} isDisabled={busy} />
        <Button label={busy ? 'Working…' : 'Register server'} variant="primary" isDisabled={busy || !projectId || !slug.trim() || !name.trim() || !endpoint.trim()} onClick={() => void addServer()} />
      </>}
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>Registered servers</Heading>
      {servers.length === 0 ? <Text>No MCP servers are registered for this organization.</Text> : <Table data={servers} idKey="id" density="compact" columns={[
        {key: 'name', header: 'Server'}, {key: 'endpoint_url', header: 'Endpoint'}, {key: 'state', header: 'Schema status'}, {key: 'tool_count', header: 'Tools'},
        {key: 'sync', header: 'Actions', renderCell: row => { const server = row as unknown as Server; return <Button label={`Sync tools for ${server.name}`} variant="secondary" isDisabled={busy || !canManage} onClick={() => void sync(server)} />; }},
      ]} />}
    </VStack></Section>
    {servers.map(server => {
      const tools = toolsByServer[server.id] ?? [];
      if (tools.length === 0) return null;
      return <Section key={server.id}><VStack gap={3}><Heading level={2}>{server.name} tools</Heading>
        <Table data={tools} idKey="id" density="compact" columns={[
          {key: 'name', header: 'Tool'}, {key: 'schema_hash', header: 'Observed schema hash'},
          {key: 'drift_locked', header: 'Review', renderCell: row => <Text role={row.drift_locked ? 'alert' : undefined}>{row.drift_locked ? 'Schema drift locked; approval required' : 'Schema matches approved version'}</Text>},
          {key: 'grant', header: 'Grant', renderCell: row => <HStack gap={2} wrap="wrap"><label>Virtual Key<select aria-label={`Virtual Key for ${String(row.name)}`} value={selectedKeys[String(row.id)] ?? ''} onChange={event => setSelectedKeys(current => ({...current, [String(row.id)]: event.target.value}))} disabled={!canManage || busy}>
            <option value="">Select a key</option>{keys.map(key => <option key={key.id} value={key.id}>{key.name} · {key.last_four}</option>)}
          </select></label><Button label={`Grant ${String(row.name)}`} variant="secondary" isDisabled={!canManage || busy || !selectedKeys[String(row.id)] || Boolean(row.drift_locked)} onClick={() => void grant(server, row as unknown as SyncedTool)} /></HStack>},
        ]} />
      </VStack></Section>;
    })}
  </VStack>;
}
