import {useEffect, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Token} from '@astryxdesign/core/Token';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {exportUsageChargeback, getUsageChargebackSummary, listBudgets, updateBudget} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Budget = components['schemas']['BudgetResource'];
type Chargeback = components['schemas']['UsageChargebackRow'];
type Role = components['schemas']['RbacRole'];
const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function FinOpsBudgetsPage({projectId, roles}: {projectId: string; roles: Role[]}) {
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [rows, setRows] = useState<Chargeback[]>([]);
  const [limitDrafts, setLimitDrafts] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const canManage = roles.some(role => manageRoles.includes(role));

  async function refresh() {
    setLoading(true); setMessage('');
    const results = await Promise.allSettled([listBudgets(projectId), getUsageChargebackSummary('project')]);
    const errors: string[] = [];
    if (results[0].status === 'fulfilled') { setBudgets(results[0].value); setLimitDrafts(Object.fromEntries(results[0].value.map(budget => [budget.id, String(budget.hard_limit_micros)]))); }
    else errors.push(`Budgets: ${errorMessage(results[0].reason)}`);
    if (results[1].status === 'fulfilled') setRows(results[1].value);
    else errors.push(`Chargeback summary: ${errorMessage(results[1].reason)}`);
    setMessage(errors.join(' ')); setLoading(false);
  }
  useEffect(() => { void refresh(); }, [projectId]);

  async function saveBudget(budget: Budget) {
    const value = Number(limitDrafts[budget.id]);
    if (!Number.isSafeInteger(value) || value < 0) { setMessage('Budget limits must be non-negative safe integer micro-USD values.'); return; }
    setBusy(true); setMessage('');
    try { await updateBudget(budget, value); await refresh(); setMessage('Budget saved.'); }
    catch (error) { setMessage(errorMessage(error)); }
    finally { setBusy(false); }
  }

  async function download(format: 'csv' | 'json') {
    setBusy(true); setMessage('');
    try {
      const blob = await exportUsageChargeback(format);
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement('a'); anchor.href = href; anchor.download = `tanpopo-chargeback.${format}`;
      anchor.click(); window.setTimeout(() => URL.revokeObjectURL(href), 1000); setMessage(`${format.toUpperCase()} chargeback export downloaded.`);
    } catch (error) { setMessage(errorMessage(error)); }
    finally { setBusy(false); }
  }

  const holds = budgets.filter(budget => budget.reserved_hold_micros > 0);
  return <VStack gap={4}>
    <Heading level={1}>FinOps budgets &amp; chargeback</Heading>
    <Text>Budget balances use integer micro-USD values. Exact, estimated, and unmetered usage remain separate.</Text>
    <HStack gap={2} wrap="wrap"><Button label="Export CSV" variant="secondary" isDisabled={busy} onClick={() => void download('csv')} /><Button label="Export JSON" variant="secondary" isDisabled={busy} onClick={() => void download('json')} /></HStack>
    {message ? <Text role="status">{message}</Text> : null}
    <Section><VStack gap={3}><Heading level={2}>Budget limits and active holds</Heading>
      {loading ? <Text role="status">Loading budget data…</Text> : budgets.length === 0 ? <Text role="status">No budget records are available.</Text> :
        <Table aria-label="Budgets and active holds" data={budgets as Array<Record<string, unknown>>} columns={[
          {key: 'scope_tier', header: 'Scope'}, {key: 'period', header: 'Period'},
          {key: 'hard_limit_micros', header: 'Hard limit', renderCell: row => formatMicroUSD(row.hard_limit_micros as number)},
          {key: 'settled_exact_micros', header: 'Exact settled', renderCell: row => formatMicroUSD(row.settled_exact_micros as number)},
          {key: 'settled_estimated_micros', header: 'Estimated settled', renderCell: row => formatMicroUSD(row.settled_estimated_micros as number)},
          {key: 'reserved_hold_micros', header: 'Active hold', renderCell: row => <HStack gap={2}><Token label={Number(row.reserved_hold_micros) > 0 ? 'Active Hold' : 'No active hold'} color={Number(row.reserved_hold_micros) > 0 ? 'yellow' : 'gray'} icon={Number(row.reserved_hold_micros) > 0 ? 'clock' : 'info'} /><Text>{formatMicroUSD(row.reserved_hold_micros as number)}</Text></HStack>},
          {key: 'unmetered_request_count', header: 'Unmetered requests'},
          {key: 'actions', header: 'Actions', renderCell: row => <HStack gap={1}><TextInput label={`Hard limit for ${String(row.scope_tier)} ${String(row.scope_id)}`} inputMode="numeric" value={limitDrafts[String(row.id)] ?? String(row.hard_limit_micros)} isDisabled={!canManage || busy} onChange={value => setLimitDrafts(current => ({...current, [String(row.id)]: value}))} /><Button label="Save limit" variant="secondary" isDisabled={!canManage || busy} onClick={() => { const target = budgets.find(item => item.id === row.id); if (target) void saveBudget(target); }} /></HStack>},
        ]} />}
      {holds.map(budget => <Text key={`hold-${budget.id}`}>{budget.scope_tier} active hold: {formatMicroUSD(budget.reserved_hold_micros)} · estimated settled: {formatMicroUSD(budget.settled_estimated_micros)}</Text>)}
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>Chargeback accuracy</Heading>
      {loading ? <Text role="status">Loading usage summary…</Text> : <Table aria-label="Chargeback summary" data={rows as Array<Record<string, unknown>>} columns={[
        {key: 'group_key', header: 'Project'}, {key: 'exact_micro_usd', header: 'Exact', renderCell: row => <HStack gap={2}><Token label="EXACT" color="green" icon="checkDouble" /><Text>{formatMicroUSD(row.exact_micro_usd as number)}</Text></HStack>},
        {key: 'estimated_micro_usd', header: 'Estimated', renderCell: row => <HStack gap={2}><Token label="ESTIMATED" color="cyan" icon="wrench" /><Text>{formatMicroUSD(row.estimated_micro_usd as number)}</Text></HStack>},
        {key: 'exact_request_count', header: 'Exact requests'}, {key: 'estimated_request_count', header: 'Estimated requests'},
        {key: 'unmetered_request_count', header: 'Unmetered', renderCell: row => <HStack gap={2}><Token label="UNMETERED_FLAGGED" color="orange" icon="warning" /><Text>{String(row.unmetered_request_count)}</Text></HStack>},
      ]} />}
    </VStack></Section>
  </VStack>;
}

export function formatMicroUSD(value: number) {
  if (!Number.isSafeInteger(value)) return 'Unavailable: unsafe integer amount';
  const amount = BigInt(value);
  const sign = amount < 0n ? '-' : '';
  const abs = amount < 0n ? -amount : amount;
  const whole = abs / 1_000_000n;
  const fraction = String(abs % 1_000_000n).padStart(6, '0');
  return `${sign}$${whole.toLocaleString('en-US')}.${fraction}`;
}
function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
