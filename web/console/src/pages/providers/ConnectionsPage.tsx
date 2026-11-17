import {useEffect, useState, type FormEvent} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {VStack} from '@astryxdesign/core/Stack';
import {createConnection, createSecret, listConnections, listProviderAdapters, listSecretMetadata, testConnection, updateConnection} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {StateToken} from '../catalog/StatusToken';

type Role = components['schemas']['RbacRole'];
type Connection = components['schemas']['ProviderConnection'] & Record<string, unknown>;
type SecretMetadata = components['schemas']['SecretMetadata'] & Record<string, unknown>;
type Adapter = components['schemas']['ProviderAdapterSummary'];
const canManage = (roles: Role[]) => roles.some(role => ['ORG_OWNER', 'TEAM_ADMIN'].includes(role));

export function ConnectionsPage({roles = [], projectId}: {roles?: Role[]; projectId?: string}) {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [secrets, setSecrets] = useState<SecretMetadata[]>([]);
  const [adapters, setAdapters] = useState<Adapter[]>([]);
  const [secretName, setSecretName] = useState('');
  const [secretValue, setSecretValue] = useState('');
  const [adapterId, setAdapterId] = useState('');
  const [secretSlot, setSecretSlot] = useState('');
  const [name, setName] = useState('');
  const [endpoint, setEndpoint] = useState('');
  const [billingMode, setBillingMode] = useState<'COMMERCIAL_API' | 'SELF_HOSTED_INFRA'>('COMMERCIAL_API');
  const [region, setRegion] = useState<'US' | 'EU' | 'APAC' | 'ON_PREM'>('US');
  const [credentialId, setCredentialId] = useState('');
  const [editingConnection, setEditingConnection] = useState<Connection | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftEndpoint, setDraftEndpoint] = useState('');
  const [draftCredentialId, setDraftCredentialId] = useState('');
  const [conflictResource, setConflictResource] = useState<Record<string, unknown> | null>(null);
  const [allowPrivateIp, setAllowPrivateIp] = useState(false);
  const [cidrs, setCidrs] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const mutable = canManage(roles);
  async function reload() {
    const [connectionRows, secretRows, adapterRows] = await Promise.all([listConnections(), listSecretMetadata(), listProviderAdapters()]);
    setConnections(connectionRows);
    setSecrets(secretRows);
    setAdapters(adapterRows);
    const initialAdapter = adapterRows.find(item => item.installation_state === 'ENABLED_ACTIVE');
    setAdapterId(current => current || initialAdapter?.id || '');
    setSecretSlot(current => current || adapterRows.find(item => item.id === (adapterId || initialAdapter?.id))?.secret_slots?.[0] || '');
  }
  useEffect(() => { reload().catch(failure => setError(message(failure))); }, []);

  async function sealSecret(event: FormEvent) {
    event.preventDefault(); setError(''); setNotice('');
    const raw = secretValue;
    try {
      const saved = await createSecret({name: secretName, project_id: projectId, secret_kind: 'API_KEY', secret_value: raw});
      setSecrets(items => [saved, ...items]); setCredentialId(saved.credential_id);
      setNotice(`Credential sealed. Fingerprint ${saved.trailing_fingerprint}. The secret cannot be read again.`);
    } catch (failure) { setError(message(failure)); }
    finally { setSecretValue(''); }
  }

  async function saveConnection(event: FormEvent) {
    event.preventDefault(); setError(''); setNotice('');
    const allowedCidrs = cidrs.split(',').map(value => value.trim()).filter(Boolean);
    if (allowPrivateIp && (!allowedCidrs.length || allowedCidrs.some(value => value === '*' || value === '0.0.0.0/0' || value === '::/0'))) {
      setError('Private upstream access needs specific CIDRs. Wildcard ranges are not allowed.'); return;
    }
    try {
      await createConnection({name, project_id: projectId, adapter_id: adapterId, billing_mode: billingMode, endpoint_url: endpoint, data_region: region,
        allow_private_ip: allowPrivateIp, ...(allowPrivateIp ? {allowed_cidrs: allowedCidrs} : {}),
        ...(credentialId && secretSlot ? {secret_bindings: {[secretSlot]: credentialId}} : {})});
      setName(''); setEndpoint(''); setCredentialId(''); await reload(); setNotice('Connection created. Run a validation probe before relying on it.');
    } catch (failure) { setError(message(failure)); }
  }

  async function probe(connection: Connection) {
    setError('');
    try { const result = await testConnection(connection.id); setNotice(result.valid ? `Connection verified in ${result.latency_ms} ms.` : result.sanitized_message ?? result.failure_code ?? 'Connection probe failed.'); }
    catch (failure) { setError(message(failure)); }
  }

  async function disable(connection: Connection) {
    setError(''); setNotice('');
    try { await updateConnection(connection.id, `"v${connection.version}"`, {state: 'DISABLED'}); await reload(); setNotice('Connection disabled.'); }
    catch (failure) { setError(message(failure)); }
  }

  function editConnection(connection: Connection) {
    setEditingConnection(connection); setDraftName(connection.name); setDraftEndpoint(connection.endpoint_url); setDraftCredentialId(''); setConflictResource(null); setError('');
  }

  async function saveConnectionChanges(event: FormEvent) {
    event.preventDefault();
    if (!editingConnection) return;
    setError(''); setConflictResource(null);
    try {
      await updateConnection(editingConnection.id, `"v${editingConnection.version}"`, {name: draftName, endpoint_url: draftEndpoint,
        ...(draftCredentialId && secretSlot ? {secret_bindings: {[secretSlot]: draftCredentialId}} : {})});
      setEditingConnection(null); await reload(); setNotice('Connection updated. Secret values remain write-only.');
    } catch (failure) {
      const conflict = failure as Error & {status?: number; currentResource?: Record<string, unknown>};
      if (conflict.status === 409) {
        setConflictResource(conflict.currentResource ?? {});
        setError('This connection changed on the server. Your draft is preserved below; reload the latest resource before retrying.');
      } else setError(message(failure));
    }
  }

  return <VStack gap={4}>
    <Heading level={1}>Provider connections</Heading>
    <Text>Credential values are write-only. This page displays only names and irreversible fingerprints returned by the vault.</Text>
    {error ? <Text role="alert">{error}</Text> : null}{notice ? <Text role="status">{notice}</Text> : null}
    {error.startsWith('This connection changed on the server.') ? <Section><VStack gap={2}>
      <Text>Your draft: {draftName} · {draftEndpoint}</Text>
      <Text>Latest server values: {String(conflictResource?.name ?? 'Name unavailable')} · {String(conflictResource?.endpoint_url ?? 'Endpoint unavailable')}</Text>
      <Button label="Reload latest connection" variant="secondary" onClick={() => void reload().then(() => setConflictResource(null)).catch(failure => setError(message(failure)))} />
    </VStack></Section> : null}
    <Table data={connections} density="compact" dividers="rows" columns={[
      {key: 'name', header: 'Connection'}, {key: 'adapter_id', header: 'Adapter'}, {key: 'billing_mode', header: 'Billing mode'},
      {key: 'data_region', header: 'Data region'}, {key: 'state', header: 'State', renderCell: row => <StateToken state={String(row.state)} />},
      {key: 'credentials', header: 'Credential metadata', renderCell: row => <Text>{Object.entries(row.secret_slot_fingerprints as Record<string, string>).map(([slot, fingerprint]) => `${slot}: ${fingerprint}`).join(', ') || 'No secret slots bound'}</Text>},
      {key: 'actions', header: 'Actions', renderCell: row => <VStack gap={1}>{mutable ? <Button label="Test connection" variant="secondary" onClick={() => void probe(row as Connection)} /> : null}{mutable ? <Button label="Edit connection" variant="secondary" onClick={() => editConnection(row as Connection)} /> : null}{mutable && row.state !== 'DISABLED' ? <Button label="Disable" variant="secondary" onClick={() => void disable(row as Connection)} /> : null}</VStack>},
    ]} />
    {editingConnection && mutable ? <Section><VStack gap={3}><Heading level={2}>Edit {editingConnection.name}</Heading>
      {error ? <Text role="alert">{error}</Text> : null}
      <form onSubmit={saveConnectionChanges}><VStack gap={3}>
        <TextInput label="Connection name" value={draftName} onChange={setDraftName} isRequired />
        <TextInput label="Endpoint URL" type="url" value={draftEndpoint} onChange={setDraftEndpoint} isRequired />
        <label>Rotate credential<select aria-label="Rotate credential" value={draftCredentialId} onChange={event => setDraftCredentialId(event.target.value)}><option value="">Keep current credential bindings</option>{secrets.map(secret => <option key={secret.credential_id} value={secret.credential_id}>{secret.name} · {secret.trailing_fingerprint}</option>)}</select></label>
        <Button label="Save connection changes" type="submit" />
        <Button label="Cancel editing" variant="secondary" onClick={() => setEditingConnection(null)} />
      </VStack></form>
    </VStack></Section> : null}
    {mutable ? <>
      <Section><VStack gap={3}><Heading level={2}>Seal a credential</Heading>
        <form onSubmit={sealSecret}><VStack gap={3}>
          <TextInput label="Credential name" value={secretName} onChange={setSecretName} isRequired />
          <TextInput label="Credential value (shown once while entering)" type="password" autoComplete="new-password" value={secretValue} onChange={setSecretValue} isRequired />
          <Text>Plaintext is sent once to the write-only vault and cleared after the request. Responses contain metadata only.</Text>
          <Button label="Seal credential" type="submit" />
        </VStack></form>
      </VStack></Section>
      <Section><VStack gap={3}><Heading level={2}>Create provider connection</Heading>
        <form onSubmit={saveConnection}><VStack gap={3}>
          <TextInput label="Connection name" value={name} onChange={setName} isRequired />
          <label>Provider adapter<select aria-label="Provider adapter" value={adapterId} onChange={event => { const id = event.target.value; setAdapterId(id); setSecretSlot(adapters.find(item => item.id === id)?.secret_slots?.[0] ?? ''); }}>{adapters.filter(item => item.installation_state === 'ENABLED_ACTIVE').map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
          <TextInput label="Endpoint URL" type="url" value={endpoint} onChange={setEndpoint} isRequired />
          <label>Billing mode<select aria-label="Billing mode" value={billingMode} onChange={event => setBillingMode(event.target.value as typeof billingMode)}><option value="COMMERCIAL_API">Commercial API</option><option value="SELF_HOSTED_INFRA">Self-hosted infrastructure</option></select></label>
          <label>Data region<select aria-label="Data region" value={region} onChange={event => setRegion(event.target.value as typeof region)}><option>US</option><option>EU</option><option>APAC</option><option>ON_PREM</option></select></label>
          {secretSlot ? <Text>Credential slot: {secretSlot}</Text> : null}
          <label>Sealed credential<select aria-label="Sealed credential" value={credentialId} onChange={event => setCredentialId(event.target.value)}><option value="">No credential</option>{secrets.map(secret => <option key={secret.credential_id} value={secret.credential_id}>{secret.name} · {secret.trailing_fingerprint}</option>)}</select></label>
          <CheckboxInput label="Allow private or loopback upstream" value={allowPrivateIp} onChange={setAllowPrivateIp} />
          {allowPrivateIp ? <TextInput label="Allowed CIDRs (comma separated; wildcard ranges are forbidden)" value={cidrs} onChange={setCidrs} isRequired /> : null}
          <Button label="Create connection" type="submit" isDisabled={!adapterId} />
        </VStack></form>
      </VStack></Section>
    </> : <Text>Connection changes and probes are available to organization owners and team administrators.</Text>}
  </VStack>;
}

function message(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
