import {useCallback, useEffect, useMemo, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Token} from '@astryxdesign/core/Token';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {createMember, createVirtualKey, listMembers, listRecentTraces, listVirtualKeys, revokeVirtualKey, rotateVirtualKey, updateMemberRoles} from '../../api/managementClient';
import {listAdminModels, listDeployments} from '../../api/catalogClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type Key = components['schemas']['VirtualKeySummary'] & Record<string, unknown>;
type Member = components['schemas']['MemberSummary'] & Record<string, unknown>;
type CreatedKey = components['schemas']['VirtualKeyCreatedResponse'];
type Tab = 'keys' | 'members';

const roles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER', 'FINOPS_VIEWER', 'DEVELOPER'];
const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];
const memberReadRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function AccessAndKeysPage({projectId, environmentId, currentPrincipalId, actorRoles}: {projectId: string; environmentId: string; currentPrincipalId: string; actorRoles: Role[]}) {
  const [tab, setTab] = useState<Tab>('keys');
  const [keys, setKeys] = useState<Key[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [traces, setTraces] = useState<components['schemas']['TraceSummary'][]>([]);
  const [availableModels, setAvailableModels] = useState<components['schemas']['LogicalModel'][]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [keyName, setKeyName] = useState('');
  const [allowedModels, setAllowedModels] = useState<string[]>(['*']);
  const [allowedMcpToolsText, setAllowedMcpToolsText] = useState('');
  const [created, setCreated] = useState<CreatedKey | null>(null);
  const [rotateTarget, setRotateTarget] = useState<Key | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<Key | null>(null);
  const [revokeMode, setRevokeMode] = useState<'GRACEFUL_DRAIN' | 'EMERGENCY_ABORT_STREAMS'>('GRACEFUL_DRAIN');
  const [confirmName, setConfirmName] = useState('');
  const [reason, setReason] = useState('');
  const [memberEmail, setMemberEmail] = useState('');
  const [memberRoles, setMemberRoles] = useState<Role[]>(['DEVELOPER']);
  const [editingMember, setEditingMember] = useState<Member | null>(null);
  const [draftRoles, setDraftRoles] = useState<Role[]>([]);
  const canManage = actorRoles.some(role => manageRoles.includes(role));
  const canReadMembers = actorRoles.some(role => memberReadRoles.includes(role));
  const canEmergencyRevoke = canManage || actorRoles.includes('SECURITY_REVIEWER');

  const refresh = useCallback(async () => {
    if (!projectId || !environmentId) { setKeys([]); setMembers([]); setTraces([]); setLoading(false); return; }
    setLoading(true); setError('');
    const results = await Promise.allSettled([
      listVirtualKeys(projectId, environmentId),
      canReadMembers ? listMembers() : Promise.resolve([] as Member[]),
      listRecentTraces(environmentId), listAdminModels(), listDeployments(environmentId),
    ] as const);
    const failures: string[] = [];
    const [keysResult, membersResult, tracesResult, modelsResult, deploymentsResult] = results;
    if (keysResult.status === 'fulfilled') setKeys(keysResult.value as Key[]); else failures.push(`Virtual keys: ${message(keysResult.reason)}`);
    if (membersResult.status === 'fulfilled') setMembers(membersResult.value as Member[]);
    else if (canReadMembers) failures.push(`Members: ${message(membersResult.reason)}`);
    if (tracesResult.status === 'fulfilled') setTraces(tracesResult.value);
    else failures.push(`Trace summary: ${message(tracesResult.reason)}`);
    if (modelsResult.status === 'fulfilled' && deploymentsResult.status === 'fulfilled') {
      const activeModelIDs = new Set(deploymentsResult.value.map(deployment => deployment.logical_model_id));
      setAvailableModels(modelsResult.value.filter(model => activeModelIDs.has(model.id)));
    } else {
      if (modelsResult.status === 'rejected') failures.push(`Models: ${message(modelsResult.reason)}`);
      if (deploymentsResult.status === 'rejected') failures.push(`Deployments: ${message(deploymentsResult.reason)}`);
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, environmentId, canReadMembers]);

  useEffect(() => { void refresh(); }, [refresh]);

  const requestsByKey = useMemo(() => {
    const counts = new Map<string, number>();
    for (const trace of traces) if (trace.virtual_key_id) counts.set(trace.virtual_key_id, (counts.get(trace.virtual_key_id) ?? 0) + 1);
    return counts;
  }, [traces]);

  async function issueKey() {
    if (!canManage || !projectId || !environmentId || !keyName.trim()) return;
    setBusy(true); setError('');
    try {
      const result = await createVirtualKey({name: keyName.trim(), project_id: projectId, env_id: environmentId,
        allowed_models: allowedModels, allowed_mcp_tools: parseToolAllowlist(allowedMcpToolsText)});
      setKeyName(''); setAllowedModels(['*']); setAllowedMcpToolsText(''); setCreated(result); await refresh();
    } catch (failure) { setError(message(failure)); }
    finally { setBusy(false); }
  }

  async function rotateKey() {
    if (!rotateTarget) return;
    setBusy(true); setError('');
    try { const result = await rotateVirtualKey(rotateTarget, 86400); setRotateTarget(null); setCreated(result); await refresh(); }
    catch (failure) { setError(message(failure)); }
    finally { setBusy(false); }
  }

  async function revokeKey() {
    if (!revokeTarget || !(canManage || (revokeMode === 'EMERGENCY_ABORT_STREAMS' && canEmergencyRevoke)) || (revokeMode === 'EMERGENCY_ABORT_STREAMS' && (confirmName !== revokeTarget.name || reason.trim().length < 5))) return;
    setBusy(true); setError('');
    try {
      const result = await revokeVirtualKey(revokeTarget, revokeMode, reason.trim() || 'Graceful key rotation');
      const outcome = `Virtual key revoked. ${result.severed_streams_count} active streams were severed.`;
      setRevokeTarget(null); setConfirmName(''); setReason(''); await refresh(); setError(outcome);
    } catch (failure) { setError(message(failure)); }
    finally { setBusy(false); }
  }

  async function saveRoles(member: Member, nextRoles: Role[]) {
    if (member.id === currentPrincipalId && nextRoles.some(role => !member.roles.includes(role))) {
      setError('You cannot grant yourself additional roles.'); return;
    }
    setBusy(true); setError('');
    try { await updateMemberRoles(member, nextRoles); setEditingMember(null); await refresh(); }
    catch (failure) { setError(message(failure)); }
    finally { setBusy(false); }
  }

  return <VStack gap={4}>
    <Heading level={1}>API keys, users &amp; teams</Heading>
    <Text>Manage environment-bound virtual keys and organization roles. Key secrets are never returned by list operations.</Text>
    {!projectId || !environmentId ? <Text role="status">Select a project and environment to manage access.</Text> : null}
    {error ? <Text role="status">{error}</Text> : null}
    <HStack gap={2} role="group" aria-label="Access management sections"><Button label="Virtual keys" aria-pressed={tab === 'keys'} variant={tab === 'keys' ? 'primary' : 'secondary'} onClick={() => setTab('keys')} /><Button label="Members and roles" aria-pressed={tab === 'members'} variant={tab === 'members' ? 'primary' : 'secondary'} onClick={() => setTab('members')} /></HStack>
    {loading ? <Text role="status">Loading access records…</Text> : null}
    {tab === 'keys' ? <>
      <Section><VStack gap={3}><Heading level={2}>Issue a virtual key</Heading>
        {!canManage ? <Text role="status">Your role has read-only access.</Text> : null}
        <Text>This key is bound to the selected environment: {environmentId || 'none selected'}.</Text>
        <TextInput label="Virtual key name" value={keyName} onChange={setKeyName} isDisabled={!canManage || busy || !environmentId} />
        <Text>Allowed models</Text><CheckboxInput label="All models" value={allowedModels.includes('*')} isDisabled={!canManage || busy} onChange={checked => setAllowedModels(checked ? ['*'] : [])} />
        {availableModels.map(model => <CheckboxInput key={model.id} label={model.slug} value={allowedModels.includes('*') || allowedModels.includes(model.slug)} isDisabled={!canManage || busy || allowedModels.includes('*')} onChange={checked => setAllowedModels(current => checked ? [...current.filter(item => item !== '*'), model.slug] : current.filter(item => item !== model.slug))} />)}
        <TextInput label="Allowed MCP tools (comma-separated; server__tool format)" value={allowedMcpToolsText} onChange={setAllowedMcpToolsText} isDisabled={!canManage || busy} />
        <Text>Leave the MCP tool list empty to deny MCP tools for this key.</Text>
        <Button label={busy ? 'Creating key…' : 'Create key'} variant="primary" isLoading={busy} isDisabled={!canManage || busy || !keyName.trim() || !environmentId || allowedModels.length === 0} onClick={() => void issueKey()} />
      </VStack></Section>
      <Section><VStack gap={3}><Heading level={2}>Virtual keys</Heading>
        {keys.length === 0 && !loading ? <Text>No virtual keys exist in this environment.</Text> : <Table data={keys} idKey="id" density="compact" hasHover columns={[
          {key: 'name', header: 'Name'}, {key: 'key_prefix', header: 'Prefix'}, {key: 'last_four', header: 'Last four'},
          {key: 'state', header: 'State', renderCell: row => <Token label={String(row.state).replaceAll('_', ' ')} color={row.state === 'CREATED_ACTIVE' ? 'green' : row.state === 'REVOKED' ? 'red' : 'yellow'} icon={row.state === 'CREATED_ACTIVE' ? 'success' : 'warning'} />},
          {key: 'created_at', header: 'Created'},
          {key: 'actions', header: 'Actions', renderCell: row => <HStack gap={1} wrap="wrap"><Button label={`Rotate ${String(row.name)}`} variant="secondary" isDisabled={!canManage || row.state === 'REVOKED'} onClick={() => setRotateTarget(row as Key)} /><Button label={`Revoke ${String(row.name)}`} variant="ghost" isDisabled={(!canManage && !canEmergencyRevoke) || row.state === 'REVOKED'} onClick={() => { setRevokeTarget(row as Key); setRevokeMode(canManage ? 'GRACEFUL_DRAIN' : 'EMERGENCY_ABORT_STREAMS'); setConfirmName(''); setReason(''); }} /></HStack>},
        ]} />}
        <Text>Emergency revoke request volume is based on the latest trace records from this environment, limited to the last hour.</Text>
      </VStack></Section>
    </> : !canReadMembers ? <Text role="status">Your role can view virtual keys but does not have access to member records.</Text> : <>
      <Section><VStack gap={3}><Heading level={2}>Add a member</Heading>
        {!canManage ? <Text role="status">Your role has read-only member access.</Text> : null}
        <TextInput label="Member email" type="email" value={memberEmail} onChange={setMemberEmail} isDisabled={!canManage || busy} />
        <Text>Assign roles</Text>{roles.map(role => <CheckboxInput key={role} label={role.replaceAll('_', ' ')} value={memberRoles.includes(role)} isDisabled={!canManage || busy} onChange={checked => setMemberRoles(checked ? [...memberRoles, role] : memberRoles.filter(item => item !== role))} />)}
        <Button label="Add member" variant="primary" isDisabled={!canManage || busy || !memberEmail.includes('@') || memberRoles.length === 0} onClick={() => void addMember(memberEmail, memberRoles, setBusy, setError, refresh, setMemberEmail)} />
      </VStack></Section>
      <Section><VStack gap={3}><Heading level={2}>Members &amp; roles</Heading>
        {members.length === 0 && !loading ? <Text>No members were returned by the Admin API.</Text> : <Table data={members} idKey="id" density="compact" hasHover columns={[
          {key: 'email', header: 'Member'}, {key: 'kind', header: 'Type'}, {key: 'roles', header: 'Roles', renderCell: row => <HStack gap={1} wrap="wrap">{(row.roles as Role[]).map(role => <Token key={role} label={role.replaceAll('_', ' ')} color="blue" icon="info" />)}</HStack>},
          {key: 'actions', header: 'Actions', renderCell: row => <Button label={`Edit roles for ${String(row.email)}`} variant="secondary" isDisabled={!canManage || busy} onClick={() => { setEditingMember(row as Member); setDraftRoles([...(row.roles as Role[])]); }} />},
        ]} />}
      </VStack></Section>
    </>}
    {created ? <Dialog isOpen onOpenChange={open => { if (!open) setCreated(null); }} purpose="required" width={560} padding={5}>
      <VStack gap={3}><DialogHeader title="Store this virtual key now" />
        <Text>This plaintext secret is shown once. It is held only in this dialog and cannot be retrieved later.</Text><Text>{created.secret_key}</Text>
        <Text>{created.name} · {created.key_prefix}…{created.last_four}</Text>
        <Button label="Copy virtual key" variant="secondary" onClick={() => { void navigator.clipboard.writeText(created.secret_key).catch(() => setError('Clipboard access failed. Select and copy the key manually.')); }} />
        <Button label="I stored it; close and erase secret" variant="primary" onClick={() => setCreated(null)} />
      </VStack>
    </Dialog> : null}
    {rotateTarget ? <Dialog isOpen onOpenChange={open => { if (!open && !busy) setRotateTarget(null); }} purpose="required" width={480} padding={5}>
      <VStack gap={3}><DialogHeader title={`Rotate ${rotateTarget.name}`} /><Text>The old and new keys remain valid during a 24-hour grace window. The new secret is displayed once.</Text>
        <HStack gap={2}><Button label={busy ? 'Rotating…' : 'Rotate key'} variant="primary" isLoading={busy} isDisabled={busy} onClick={() => void rotateKey()} /><Button label="Cancel" variant="secondary" isDisabled={busy} onClick={() => setRotateTarget(null)} /></HStack>
      </VStack>
    </Dialog> : null}
    {revokeTarget ? <Dialog isOpen onOpenChange={open => { if (!open && !busy) setRevokeTarget(null); }} purpose="form" width={520} padding={5}>
      <VStack gap={3}><DialogHeader title={`Revoke ${revokeTarget.name}`} />
        <label>Revocation mode<select aria-label="Revocation mode" value={revokeMode} onChange={event => setRevokeMode(event.target.value as typeof revokeMode)}><option value="GRACEFUL_DRAIN" disabled={!canManage}>Graceful drain</option><option value="EMERGENCY_ABORT_STREAMS" disabled={!canEmergencyRevoke}>Emergency security revoke</option></select></label>
        <Text>Recent requests for this key in the last hour: {requestsByKey.get(revokeTarget.id) ?? 0}. Trace-derived count.</Text>
        {revokeMode === 'EMERGENCY_ABORT_STREAMS' ? <>
          <Text>Emergency revoke terminates active streams and cannot be undone. Type the key name and provide an audit reason of at least 5 characters.</Text>
          <TextInput label="Type the virtual key name" value={confirmName} onChange={setConfirmName} />
          <TextInput label="Audit reason" value={reason} onChange={setReason} />
        </> : <Text>Graceful drain stops new requests while allowing active streams to complete.</Text>}
        <HStack gap={2}><Button label={busy ? 'Revoking…' : revokeMode === 'GRACEFUL_DRAIN' ? 'Revoke with graceful drain' : 'Emergency revoke key'} variant="destructive" isLoading={busy} isDisabled={busy || (!canManage && revokeMode !== 'EMERGENCY_ABORT_STREAMS') || (revokeMode === 'EMERGENCY_ABORT_STREAMS' && (confirmName !== revokeTarget.name || reason.trim().length < 5))} onClick={() => void revokeKey()} /><Button label="Cancel" variant="secondary" isDisabled={busy} onClick={() => setRevokeTarget(null)} /></HStack>
      </VStack>
    </Dialog> : null}
    {editingMember ? <Dialog isOpen onOpenChange={open => { if (!open) setEditingMember(null); }} purpose="form" width={520} padding={5}>
      <VStack gap={3}><DialogHeader title={`Roles for ${editingMember.email}`} />
        {roles.map(role => <CheckboxInput key={role} label={role.replaceAll('_', ' ')} value={draftRoles.includes(role)} isDisabled={busy || (editingMember.id === currentPrincipalId && !editingMember.roles.includes(role))} onChange={checked => setDraftRoles(checked ? [...draftRoles, role] : draftRoles.filter(item => item !== role))} />)}
        {editingMember.id === currentPrincipalId ? <Text>Self-service role grants are disabled. You cannot grant yourself additional access.</Text> : null}
        <HStack gap={2}><Button label={busy ? 'Saving roles…' : 'Save roles'} variant="primary" isLoading={busy} isDisabled={busy || draftRoles.length === 0} onClick={() => void saveRoles(editingMember, draftRoles)} /><Button label="Cancel" variant="secondary" isDisabled={busy} onClick={() => setEditingMember(null)} /></HStack>
      </VStack>
    </Dialog> : null}
  </VStack>;
}

async function addMember(email: string, selectedRoles: Role[], setBusy: (value: boolean) => void, setError: (value: string) => void, refresh: () => Promise<void>, resetEmail: (value: string) => void) {
  if (!email || selectedRoles.length === 0) return;
  setBusy(true); setError('');
  try {
    await createMember(email, selectedRoles);
    resetEmail(''); await refresh();
  } catch (failure) { setError(message(failure)); }
  finally { setBusy(false); }
}

function message(error: unknown) { return error instanceof Error ? error.message : 'The Admin API request failed.'; }
function parseToolAllowlist(value: string) { return [...new Set(value.split(',').map(item => item.trim()).filter(Boolean))]; }
