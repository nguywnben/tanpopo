import {useEffect, useMemo, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {VStack} from '@astryxdesign/core/Stack';
import {getProviderAdapter, listProviderAdapters} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {StateToken, TrustToken} from '../catalog/StatusToken';

type Adapter = components['schemas']['ProviderAdapterSummary'] & Record<string, unknown>;

export function AdaptersPage() {
  const [adapters, setAdapters] = useState<Adapter[]>([]);
  const [selected, setSelected] = useState<Adapter | null>(null);
  const [query, setQuery] = useState('');
  const [error, setError] = useState('');
  useEffect(() => { listProviderAdapters().then(setAdapters).catch(failure => setError(message(failure))); }, []);
  const visible = useMemo(() => adapters.filter(adapter => `${adapter.name} ${adapter.wire_protocol ?? ''} ${adapter.execution_tier}`.toLowerCase().includes(query.toLowerCase())), [adapters, query]);
  async function inspect(adapter: Adapter) {
    setError(''); setSelected(adapter);
    try { setSelected(await getProviderAdapter(adapter.id) as Adapter); }
    catch (failure) { setError(message(failure)); }
  }
  return <VStack gap={4}>
    <Heading level={1}>Provider adapters</Heading>
    <Text>Installed protocol adapters and their declared connection requirements.</Text>
    {error ? <Text role="alert">{error}</Text> : null}
    <TextInput label="Search adapters" type="search" value={query} onChange={setQuery} />
    <Table data={visible} density="compact" dividers="rows" hasHover columns={[
      {key: 'name', header: 'Adapter', renderCell: row => <Button label={String(row.name)} variant="ghost" onClick={() => void inspect(row as Adapter)} />},
      {key: 'wire_protocol', header: 'Protocol'},
      {key: 'execution_tier', header: 'Execution tier'},
      {key: 'origin_trust_tier', header: 'Origin', renderCell: row => <TrustToken trust={row.origin_trust_tier as Adapter['origin_trust_tier']} />},
      {key: 'installation_state', header: 'Installation', renderCell: row => <StateToken state={String(row.installation_state)} />},
    ]} />
    {selected ? <Section><VStack gap={3}><Heading level={2}>{selected.name} details</Heading>
        <Text>Package: {selected.package_id} · Version {selected.version}</Text>
        <Text>Publisher trust: {selected.origin_trust_tier}</Text>
        <Text>Execution tier: {selected.execution_tier}</Text>
        <Text>Enterprise signed: {selected.enterprise_signed ? 'Yes' : 'No'}</Text>
        <Text>Authentication methods: {(selected.supported_auth_methods ?? []).join(', ') || 'None declared'}</Text>
        <Text>Secret slots: {(selected.secret_slots ?? []).join(', ') || 'None'}</Text>
        <Text>Allowed hosts: {(selected.allowed_hosts ?? []).join(', ') || 'None declared'}</Text>
        {selected.installation_state === 'ENABLED_ACTIVE'
          ? <a href="/providers/connections">New connection with this adapter</a>
          : <Text role="status">A connection can be created after this adapter is enabled.</Text>}
        <Button label="Close details" variant="secondary" onClick={() => setSelected(null)} />
    </VStack></Section> : null}
  </VStack>;
}

function message(error: unknown) { return error instanceof Error ? error.message : 'Provider adapters could not be loaded.'; }
