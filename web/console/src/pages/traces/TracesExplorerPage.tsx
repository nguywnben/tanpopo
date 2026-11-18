import {memo, useCallback, useEffect, useMemo, useRef, useState} from 'react';
import * as stylex from '@stylexjs/stylex';
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
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [message, setMessage] = useState('');
  const queryGeneration = useRef(0);
  const selectionGeneration = useRef(0);
  const mayReadTraces = !roles.includes('FINOPS_VIEWER') || roles.some(role => role !== 'FINOPS_VIEWER');

  useEffect(() => {
    let active = true;
    queryGeneration.current += 1;
    setTraces([]); setNextCursor(undefined); setLoadingMore(false);
    if (!mayReadTraces) { setLoading(false); return; }
    setLoading(true); setMessage('');
    listTracePage(environmentId, model).then(page => { if (active) { setTraces(page.data as Trace[]); setNextCursor(page.nextCursor); } })
      .catch(error => { if (active) setMessage(errorMessage(error)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [environmentId, model, mayReadTraces]);

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) return;
    const generation = queryGeneration.current;
    setLoadingMore(true); setMessage('');
    try {
      const page = await listTracePage(environmentId, model, nextCursor);
      if (generation !== queryGeneration.current) return;
      setTraces(current => [...current, ...page.data as Trace[]]);
      setNextCursor(page.nextCursor);
    } catch (error) { if (generation === queryGeneration.current) setMessage(errorMessage(error)); }
    finally { if (generation === queryGeneration.current) setLoadingMore(false); }
  }, [environmentId, loadingMore, model, nextCursor]);

  const visible = useMemo(() => traces.filter(trace => !appliedError || (trace.error_code ?? '').toLowerCase().includes(appliedError.toLowerCase())), [traces, appliedError]);

  async function search() {
    const generation = ++selectionGeneration.current;
    setMessage(''); setAppliedError(errorCode.trim());
    if (!requestId.trim()) { setSelected(null); return; }
    try {
      const detail = await getTraceWaterfall(requestId.trim()) as Trace;
      if (generation === selectionGeneration.current) setSelected(detail);
    } catch (error) {
      if (generation === selectionGeneration.current) { setSelected(null); setMessage(errorMessage(error)); }
    }
  }

  const selectTrace = useCallback((trace: Trace) => {
    const generation = ++selectionGeneration.current;
    const id = String(trace.request_id);
    setRequestId(id);
    void getTraceWaterfall(id)
      .then(detail => { if (generation === selectionGeneration.current) setSelected(detail as Trace); })
      .catch(error => { if (generation === selectionGeneration.current) setMessage(errorMessage(error)); });
  }, []);

  if (!mayReadTraces) return <Text role="alert">The FINOPS_VIEWER role can access FinOps reports only.</Text>;
  return <VStack gap={4}>
    <Heading level={1}>Request traces</Heading>
    <Text>Search request IDs, error codes, or models. Trace views contain request metadata only; prompt and completion bytes are not stored. Error-code matches are filtered within loaded pages.</Text>
    <HStack gap={2} wrap="wrap">
      <TextInput label="Request ID" value={requestId} onChange={setRequestId} />
      <TextInput label="Error code" value={errorCode} onChange={setErrorCode} />
      <TextInput label="Model" value={model} onChange={setModel} />
      <Button label="Search traces" variant="primary" onClick={() => void search()} />
    </HStack>
    <HStack gap={2} wrap="wrap"><Token label="Metadata-Only (0 prompt bytes stored)" color="gray" icon="eyeSlash" />
      <Text>Interaction target for a 200-row trace result set: INP &lt; 100 ms.</Text></HStack>
    {message ? <Text role="status">{message}</Text> : null}
    {selected ? <Section><VStack gap={3}><Heading level={2}>Request waterfall: {selected.request_id}</Heading>
      <HStack gap={2} wrap="wrap"><Text>Route: {selected.route_slug}</Text><Text>Status: {selected.status_code}</Text><Text>Error: {selected.error_code ?? 'None'}</Text><Text>Duration: {selected.duration_ms} ms</Text></HStack>
      <Token label={selected.usage_accuracy.replaceAll('_', ' ')} color={accuracyColor(selected.usage_accuracy)} icon={accuracyIcon(selected.usage_accuracy)} />
      <Table aria-label="Request execution waterfall" data={(selected.waterfall_stages ?? []) as Array<Record<string, unknown>>} columns={[
        {key: 'stage', header: 'Stage'}, {key: 'duration_ms', header: 'Duration (ms)'}, {key: 'outcome', header: 'Outcome'},
      ]} />
    </VStack></Section> : null}
    <TraceResults loading={loading} traces={visible} nextCursor={nextCursor} loadingMore={loadingMore} resetKey={`${environmentId}\0${model}\0${appliedError}`} onLoadMore={loadMore} onSelect={selectTrace} />
  </VStack>;
}

const TraceResults = memo(function TraceResults({loading, traces, nextCursor, loadingMore, resetKey, onLoadMore, onSelect}: {
  loading: boolean; traces: Trace[]; nextCursor?: string; loadingMore: boolean; resetKey: string; onLoadMore: () => void; onSelect: (trace: Trace) => void;
}) {
  const [shownRows, setShownRows] = useState(10);
  useEffect(() => setShownRows(10), [resetKey]);
  return <Section><VStack gap={3}><Heading level={2}>Recent request metadata</Heading>
    {loading ? <Text role="status">Loading traces…</Text> : traces.length === 0 ? <Text role="status">No trace records match these filters.</Text> :
      <>
      <Text role="status">Showing {Math.min(shownRows, traces.length)} of {traces.length} matching trace records.</Text>
      <VStack gap={0} xstyle={styles.tableViewport}><Table aria-label="Request traces" data={traces.slice(0, shownRows) as Array<Record<string, unknown>>} columns={[
        {key: 'request_id', header: 'Request ID', renderCell: row => <Button label={String(row.request_id)} variant="ghost" onClick={() => onSelect(row as Trace)} />},
        {key: 'route_slug', header: 'Route'}, {key: 'status_code', header: 'Status'},
        {key: 'error_code', header: 'Error code', renderCell: row => String(row.error_code ?? '—')},
        {key: 'usage_accuracy', header: 'Usage accuracy', renderCell: row => <Token label={String(row.usage_accuracy).replaceAll('_', ' ')} color={accuracyColor(String(row.usage_accuracy))} icon={accuracyIcon(String(row.usage_accuracy))} />},
        {key: 'duration_ms', header: 'Duration (ms)'}, {key: 'created_at', header: 'Created'},
      ]} /></VStack>
      {shownRows < traces.length ? <Button label="Show more traces" variant="secondary" onClick={() => setShownRows(count => Math.min(count + 10, traces.length))} /> : null}
      </>}
    {!loading && nextCursor ? <Button label={loadingMore ? 'Loading traces…' : 'Load more traces'} variant="secondary" isDisabled={loadingMore} onClick={onLoadMore} /> : null}
  </VStack></Section>;
});

const styles = stylex.create({tableViewport: {boxSizing: 'border-box', maxWidth: '100%', minWidth: 0, overflowX: 'auto', width: '100%'}});

function accuracyColor(value: string): 'green' | 'cyan' | 'orange' { return value === 'EXACT' ? 'green' : value === 'ESTIMATED' ? 'cyan' : 'orange'; }
function accuracyIcon(value: string): 'checkDouble' | 'wrench' | 'warning' { return value === 'EXACT' ? 'checkDouble' : value === 'ESTIMATED' ? 'wrench' : 'warning'; }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
