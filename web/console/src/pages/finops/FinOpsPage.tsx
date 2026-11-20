import {useCallback, useEffect, useState} from 'react';
import {Download} from 'lucide-react';
import {exportUsageChargeback, getUsageChargebackSummary, listBudgets, updateBudget} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {Banner, Button, MetricStrip, SectionHeader, SelectField, StatusPill, Tabs, TextField, formatMicroUSD} from '../../ui/primitives';

type Budget = components['schemas']['BudgetResource'];
type Chargeback = components['schemas']['UsageChargebackRow'];
type Role = components['schemas']['RbacRole'];
type GroupBy = 'project' | 'env' | 'key' | 'model';

const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function FinOpsPage({pathname, projectId, roles}: {pathname: string; projectId: string; roles: Role[]}) {
  const {navigate} = useRouter();
  const activeTab = pathname.startsWith('/finops/chargeback') ? 'chargeback' : 'budgets';
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [rows, setRows] = useState<Chargeback[]>([]);
  const [groupBy, setGroupBy] = useState<GroupBy>('project');
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  const canManage = roles.some(role => manageRoles.includes(role));

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    const [budgetsRes, chargebackRes] = await Promise.allSettled([
      listBudgets(projectId),
      getUsageChargebackSummary(groupBy),
    ]);
    const failures: string[] = [];
    if (budgetsRes.status === 'fulfilled') {
      setBudgets(budgetsRes.value);
      setDrafts(Object.fromEntries(budgetsRes.value.map(item => [item.id, String(item.hard_limit_micros)])));
    } else {
      failures.push(`Budgets: ${errText(budgetsRes.reason)}`);
    }
    if (chargebackRes.status === 'fulfilled') {
      setRows(chargebackRes.value);
    } else {
      failures.push(`Chargeback: ${errText(chargebackRes.reason)}`);
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, groupBy]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function saveLimit(budget: Budget) {
    const nextValue = Number(drafts[budget.id]);
    if (!Number.isSafeInteger(nextValue) || nextValue < 0) {
      setError('Hard limit must be a non-negative safe integer in micro-USD.');
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updateBudget(budget, nextValue);
      await refresh();
      setNotice(`Saved hard limit for ${budget.scope_tier} (${budget.scope_id}).`);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleExport(format: 'csv' | 'json') {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const blob = await exportUsageChargeback(format);
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.download = `tanpopo-chargeback.${format}`;
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(href), 1000);
      setNotice(`Exported ${format.toUpperCase()} chargeback report.`);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  const totalExact = budgets.reduce((sum, item) => sum + item.settled_exact_micros, 0);
  const totalEstimated = budgets.reduce((sum, item) => sum + item.settled_estimated_micros, 0);
  const totalHolds = budgets.reduce((sum, item) => sum + item.reserved_hold_micros, 0);
  const totalUnmetered = budgets.reduce((sum, item) => sum + (item.unmetered_request_count ?? 0), 0);

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>FinOps</h1>
          <p className="tp-page-subtitle">Integer micro-USD ledger with separate exact, estimated, active-hold, and unmetered accounting.</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="secondary" size="sm" disabled={busy} onClick={() => void handleExport('csv')}>
            <Download size={14} aria-hidden="true" />
            <span>Export CSV</span>
          </Button>
          <Button variant="secondary" size="sm" disabled={busy} onClick={() => void handleExport('json')}>
            <Download size={14} aria-hidden="true" />
            <span>Export JSON</span>
          </Button>
        </div>
      </div>

      <Tabs
        label="FinOps views"
        activeId={activeTab}
        onSelect={id => navigate(id === 'chargeback' ? '/finops/chargeback' : '/finops/budgets')}
        items={[
          {id: 'budgets', label: 'Budgets & Holds', count: budgets.length},
          {id: 'chargeback', label: 'Chargeback'},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      <MetricStrip
        items={[
          {label: 'Exact settled', value: formatMicroUSD(totalExact), detail: 'Provider-reported usage'},
          {label: 'Estimated settled', value: formatMicroUSD(totalEstimated), detail: 'Fallback token estimation'},
          {label: 'Active holds', value: formatMicroUSD(totalHolds), detail: 'In-flight pre-egress leases'},
          {label: 'Unmetered requests', value: totalUnmetered.toLocaleString('en-US'), detail: 'Flagged for audit review'},
        ]}
      />

      {activeTab === 'budgets' ? (
        <section className="tp-section">
          <SectionHeader title="Budget ceilings" count={budgets.length} />
          <DataTable
            ariaLabel="Budgets and active holds"
            loading={loading}
            rows={budgets}
            rowKey={row => row.id}
            emptyTitle="No budgets configured"
            emptyDescription="Budget records appear once configured for your organization or project."
            columns={[
              {
                id: 'scope',
                header: 'Scope',
                primary: true,
                width: '18%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span style={{fontWeight: 600}}>{row.scope_tier}</span>
                    <span className="mono tp-muted">{row.scope_id}</span>
                  </div>
                ),
              },
              {id: 'period', header: 'Period', width: '10%', render: row => <span>{row.period}</span>},
              {id: 'exact', header: 'Exact settled', width: '14%', render: row => <span className="mono">{formatMicroUSD(row.settled_exact_micros)}</span>},
              {id: 'estimated', header: 'Estimated', width: '14%', render: row => <span className="mono">{formatMicroUSD(row.settled_estimated_micros)}</span>},
              {
                id: 'hold',
                header: 'Active hold',
                width: '14%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono">{formatMicroUSD(row.reserved_hold_micros)}</span>
                    {row.reserved_hold_micros > 0 ? <StatusPill tone="warn" label="Active Hold" /> : null}
                  </div>
                ),
              },
              {
                id: 'limit',
                header: 'Hard limit (micro-USD)',
                width: '30%',
                render: row => (
                  <div className="tp-row" style={{alignItems: 'flex-end'}}>
                    <div style={{flex: '1 1 120px', minWidth: 0}}>
                      <TextField
                        label={`Hard limit for ${row.scope_tier} ${row.scope_id}`}
                        inputMode="numeric"
                        value={drafts[row.id] ?? String(row.hard_limit_micros)}
                        disabled={!canManage || busy}
                        onChange={event => setDrafts(curr => ({...curr, [row.id]: event.target.value}))}
                      />
                    </div>
                    {canManage ? (
                      <Button variant="secondary" size="sm" disabled={busy} onClick={() => void saveLimit(row)}>
                        Save
                      </Button>
                    ) : null}
                  </div>
                ),
              },
            ]}
          />
        </section>
      ) : (
        <section className="tp-section">
          <SectionHeader
            title="Chargeback accuracy breakdown"
            count={rows.length}
            actions={
              <SelectField
                label="Group by"
                value={groupBy}
                width={180}
                onChange={next => setGroupBy(next as GroupBy)}
                options={[
                  {value: 'project', label: 'Project'},
                  {value: 'env', label: 'Environment'},
                  {value: 'key', label: 'Virtual Key'},
                  {value: 'model', label: 'Model'},
                ]}
              />
            }
          />
          <DataTable
            ariaLabel="Chargeback accuracy summary"
            loading={loading}
            rows={rows}
            rowKey={row => row.group_key}
            emptyTitle="No usage recorded"
            emptyDescription="Chargeback rows populate as metered requests settle."
            columns={[
              {id: 'group', header: groupBy.toUpperCase(), primary: true, width: '22%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.group_key}</span>},
              {
                id: 'exact',
                header: 'Exact',
                width: '22%',
                render: row => (
                  <div className="tp-row">
                    <StatusPill tone="ok" label="EXACT" />
                    <span className="mono">{formatMicroUSD(row.exact_micro_usd)}</span>
                  </div>
                ),
              },
              {
                id: 'estimated',
                header: 'Estimated',
                width: '22%',
                render: row => (
                  <div className="tp-row">
                    <StatusPill tone="info" label="ESTIMATED" />
                    <span className="mono">{formatMicroUSD(row.estimated_micro_usd)}</span>
                  </div>
                ),
              },
              {id: 'counts', header: 'Exact / Est. reqs', width: '16%', render: row => <span className="num">{row.exact_request_count} / {row.estimated_request_count}</span>},
              {
                id: 'unmetered',
                header: 'Unmetered',
                width: '18%',
                render: row => (
                  <div className="tp-row">
                    {row.unmetered_request_count > 0 ? <StatusPill tone="danger" label="UNMETERED_FLAGGED" /> : null}
                    <span className="num">{row.unmetered_request_count}</span>
                  </div>
                ),
              },
            ]}
          />
        </section>
      )}
    </div>
  );
}

function errText(cause: unknown): string {
  return cause instanceof Error ? cause.message : 'Request failed.';
}
