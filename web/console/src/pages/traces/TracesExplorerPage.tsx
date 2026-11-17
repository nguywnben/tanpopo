import {useEffect, useMemo, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Token} from '@astryxdesign/core/Token';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {getTraceWaterfall, listTracePage} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Trace = components['schemas']['TraceSummary'] & Record<string, unknown>;
type Role = components['schemas']['RbacRole'];

export function TracesExplorerPage({environmentId, roles}: {environmentId: string; roles: Role[]}) {
  const [traces, setTraces] = useState<Trace[]>([]);
  const [requestId, setRequestId] = useState('');
  const [errorCode, setErrorCode] = useState('');
  const [model, setModel] = useState('');
  const [appliedError, setAppliedError] = useState('');
  const [selected, setSelected] = useState<Trace | null>(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');
  const mayReadTraces = !roles.includes('FINOPS_VIEWER') || roles.some(role => role !== 'FINOPS_VIEWER');

  useEffect(() => {
    let active = true;
    if (!mayReadTraces) { setLoading(false); return; }
    setLoading(true); setMessage('');
    listTracePage(environmentId, model).then(rows => { if (active) setTraces(rows as Trace[]); })
      .catch(error => { if (active) setMessage(errorMessage(error)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [environmentId, model, mayReadTraces]);

  const visible = useMemo(() => traces.filter(trace => !appliedError || (trace.error_code ?? '').toLowerCase().includes(appliedError.toLowerCase())), [traces, appliedError]);

  async function search() {
    setMessage(''); setAppliedError(errorCode.trim());
    if (!requestId.trim()) { setSelected(null); return; }
    try { setSelected(await getTraceWaterfall(requestId.trim()) as Trace); }
    catch (error) { setSelected(null); setMessage(errorMessage(error)); }
  }

  if (!mayReadTraces) return <Text role="alert">The FINOPS_VIEWER role can access FinOps reports only.</Text>;
  return <VStack gap={4}>
    <Heading level={1}>Request traces</Heading>
    <Text>Search request IDs, error codes, or models. Trace views contain request metadata only; prompt and completion bytes are not stored.</Text>
    <HStack gap={2} wrap="wrap">
      <TextInput label="Request ID" value={requestId} onChange={setRequestId} />
      <TextInput label="Error code" value={errorCode} onChange={setErrorCode} />
      <TextInput label="Model" value={model} onChange={setModel} />
      <Button label="Search traces" variant="primary" onClick={() => void search()} />
    </HStack>
    <HStack gap={2} wrap="wrap"><Token label="Metadata-Only (0 prompt bytes stored)" color="gray" icon="eyeSlash" />
      <Text>Interaction target for a 200-row table: INP &lt; 100 ms (TARGET / UNVERIFIED).</Text></HStack>
    {message ? <Text role="status">{message}</Text> : null}
    {selected ? <Section><VStack gap={3}><Heading level={2}>Request waterfall: {selected.request_id}</Heading>
      <HStack gap={2} wrap="wrap"><Text>Route: {selected.route_slug}</Text><Text>Status: {selected.status_code}</Text><Text>Error: {selected.error_code ?? 'None'}</Text><Text>Duration: {selected.duration_ms} ms</Text></HStack>
      <Token label={selected.usage_accuracy.replaceAll('_', ' ')} color={accuracyColor(selected.usage_accuracy)} icon={accuracyIcon(selected.usage_accuracy)} />
      <Table aria-label="Request execution waterfall" data={(selected.waterfall_stages ?? []) as Array<Record<string, unknown>>} columns={[
        {key: 'stage', header: 'Stage'}, {key: 'duration_ms', header: 'Duration (ms)'}, {key: 'outcome', header: 'Outcome'},
      ]} />
    </VStack></Section> : null}
    <Section><VStack gap={3}><Heading level={2}>Recent request metadata</Heading>
      {loading ? <Text role="status">Loading traces…</Text> : visible.length === 0 ? <Text role="status">No trace records match these filters.</Text> :
        <Table aria-label="Request traces" data={visible as Array<Record<string, unknown>>} columns={[
          {key: 'request_id', header: 'Request ID', renderCell: row => <Button label={String(row.request_id)} variant="ghost" onClick={() => { setRequestId(String(row.request_id)); void getTraceWaterfall(String(row.request_id)).then(setSelected).catch(error => setMessage(errorMessage(error))); }} />},
          {key: 'route_slug', header: 'Route'}, {key: 'status_code', header: 'Status'},
          {key: 'error_code', header: 'Error code', renderCell: row => String(row.error_code ?? '—')},
          {key: 'usage_accuracy', header: 'Usage accuracy', renderCell: row => <Token label={String(row.usage_accuracy).replaceAll('_', ' ')} color={accuracyColor(String(row.usage_accuracy))} icon={accuracyIcon(String(row.usage_accuracy))} />},
          {key: 'duration_ms', header: 'Duration (ms)'}, {key: 'created_at', header: 'Created'},
        ]} />}
    </VStack></Section>
  </VStack>;
}

function accuracyColor(value: string): 'green' | 'cyan' | 'orange' { return value === 'EXACT' ? 'green' : value === 'ESTIMATED' ? 'cyan' : 'orange'; }
function accuracyIcon(value: string): 'checkDouble' | 'wrench' | 'warning' { return value === 'EXACT' ? 'checkDouble' : value === 'ESTIMATED' ? 'wrench' : 'warning'; }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
