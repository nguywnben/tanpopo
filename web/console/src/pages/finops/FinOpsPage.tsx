import {useCallback, useEffect, useState} from 'react';
import {Download} from 'lucide-react';
import {exportUsageChargeback, getUsageChargebackSummary, listBudgets, updateBudget} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {Banner, Button, MetricStrip, SectionHeader, SelectField, StatusPill, Tabs, TextField, formatOperationalState} from '../../ui/primitives';

type Budget = components['schemas']['BudgetResource'];
type Chargeback = components['schemas']['UsageChargebackRow'];
type Role = components['schemas']['RbacRole'];
type GroupBy = 'project' | 'env' | 'key' | 'model';

const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function FinOpsPage({pathname, projectId, roles}: {pathname: string; projectId: string; roles: Role[]}) {
  const {navigate} = useRouter();
  const {t, formatNumber, formatMicroUSD} = useI18n();
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
      failures.push(t('finops.loadBudgetsFailed', {error: errText(budgetsRes.reason, t)}));
    }
    if (chargebackRes.status === 'fulfilled') {
      setRows(chargebackRes.value);
    } else {
      failures.push(t('finops.loadChargebackFailed', {error: errText(chargebackRes.reason, t)}));
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, groupBy, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function saveLimit(budget: Budget) {
    const nextValue = Number(drafts[budget.id]);
    if (!Number.isSafeInteger(nextValue) || nextValue < 0) {
      setError(t('finops.invalidLimit'));
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updateBudget(budget, nextValue);
      await refresh();
      setNotice(t('finops.savedLimit', {scope: budget.scope_tier, id: budget.scope_id}));
    } catch (cause) {
      setError(errText(cause, t));
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
      setNotice(t('finops.exported', {format: format.toUpperCase()}));
    } catch (cause) {
      setError(errText(cause, t));
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
          <h1>{t('finops.title')}</h1>
          <p className="tp-page-subtitle">{t('finops.subtitle')}</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="secondary" size="sm" disabled={busy} onClick={() => void handleExport('csv')}>
            <Download size={14} aria-hidden="true" />
            <span>{t('finops.exportCsv')}</span>
          </Button>
          <Button variant="secondary" size="sm" disabled={busy} onClick={() => void handleExport('json')}>
            <Download size={14} aria-hidden="true" />
            <span>{t('finops.exportJson')}</span>
          </Button>
        </div>
      </div>

      <Tabs
        label={t('finops.tabsLabel')}
        activeId={activeTab}
        onSelect={id => navigate(id === 'chargeback' ? '/finops/chargeback' : '/finops/budgets')}
        items={[
          {id: 'budgets', label: t('finops.tabBudgets'), count: budgets.length},
          {id: 'chargeback', label: t('finops.tabChargeback')},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      <MetricStrip
        items={[
          {label: t('finops.kpi.exactSettled'), value: formatMicroUSD(totalExact), detail: t('finops.kpi.exactDetail')},
          {label: t('finops.kpi.estimatedSettled'), value: formatMicroUSD(totalEstimated), detail: t('finops.kpi.estimatedDetail')},
          {label: t('finops.kpi.activeHolds'), value: formatMicroUSD(totalHolds), detail: t('finops.kpi.activeHoldsDetail')},
          {label: t('finops.kpi.unmetered'), value: formatNumber(totalUnmetered), detail: t('finops.kpi.unmeteredDetail')},
        ]}
      />

      {activeTab === 'budgets' ? (
        <section className="tp-section">
          <SectionHeader title={t('finops.budgetCeilings')} count={budgets.length} />
          <DataTable
            ariaLabel={t('finops.budgetsTableAria')}
            loading={loading}
            rows={budgets}
            rowKey={row => row.id}
            emptyTitle={t('finops.emptyBudgetsTitle')}
            emptyDescription={t('finops.emptyBudgetsDesc')}
            columns={[
              {
                id: 'scope',
                header: t('finops.col.scope'),
                primary: true,
                width: '18%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span style={{fontWeight: 600}}>{row.scope_tier}</span>
                    <span className="mono tp-muted">{row.scope_id}</span>
                  </div>
                ),
              },
              {id: 'period', header: t('finops.col.period'), width: '10%', render: row => <span>{row.period}</span>},
              {id: 'exact', header: t('finops.col.exactSettled'), width: '14%', render: row => <span className="mono">{formatMicroUSD(row.settled_exact_micros)}</span>},
              {id: 'estimated', header: t('finops.col.estimated'), width: '14%', render: row => <span className="mono">{formatMicroUSD(row.settled_estimated_micros)}</span>},
              {
                id: 'hold',
                header: t('finops.col.activeHold'),
                width: '14%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono">{formatMicroUSD(row.reserved_hold_micros)}</span>
                    {row.reserved_hold_micros > 0 ? <StatusPill tone="warn" label={t('finops.activeHoldPill')} /> : null}
                  </div>
                ),
              },
              {
                id: 'limit',
                header: t('finops.col.hardLimit'),
                width: '30%',
                render: row => (
                  <div className="tp-row" style={{alignItems: 'flex-end'}}>
                    <div style={{flex: '1 1 120px', minWidth: 0}}>
                      <TextField
                        label={t('finops.hardLimitFor', {scope: row.scope_tier, id: row.scope_id})}
                        fieldSize="sm"
                        inputMode="numeric"
                        value={drafts[row.id] ?? String(row.hard_limit_micros)}
                        disabled={!canManage || busy}
                        onChange={event => setDrafts(curr => ({...curr, [row.id]: event.target.value}))}
                      />
                    </div>
                    {canManage ? (
                      <Button variant="secondary" size="sm" disabled={busy} onClick={() => void saveLimit(row)}>
                        {t('common.save')}
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
            title={t('finops.chargebackBreakdown')}
            count={rows.length}
            actions={
              <SelectField
                label={t('finops.groupBy')}
                value={groupBy}
                width={180}
                onChange={next => setGroupBy(next as GroupBy)}
                options={[
                  {value: 'project', label: t('finops.group.project')},
                  {value: 'env', label: t('finops.group.env')},
                  {value: 'key', label: t('finops.group.key')},
                  {value: 'model', label: t('finops.group.model')},
                ]}
              />
            }
          />
          <DataTable
            ariaLabel={t('finops.chargebackTableAria')}
            loading={loading}
            rows={rows}
            rowKey={row => row.group_key}
            emptyTitle={t('finops.emptyChargebackTitle')}
            emptyDescription={t('finops.emptyChargebackDesc')}
            columns={[
              {id: 'group', header: t(`finops.group.${groupBy}`), primary: true, width: '22%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.group_key}</span>},
              {
                id: 'exact',
                header: t('finops.col.exact'),
                width: '22%',
                render: row => (
                  <div className="tp-row">
                    <StatusPill tone="ok" label={formatOperationalState('EXACT')} />
                    <span className="mono">{formatMicroUSD(row.exact_micro_usd)}</span>
                  </div>
                ),
              },
              {
                id: 'estimated',
                header: t('finops.col.estimated'),
                width: '22%',
                render: row => (
                  <div className="tp-row">
                    <StatusPill tone="warn" label={formatOperationalState('ESTIMATED')} />
                    <span className="mono">{formatMicroUSD(row.estimated_micro_usd)}</span>
                  </div>
                ),
              },
              {id: 'counts', header: t('finops.col.exactEstReqs'), width: '16%', render: row => <span className="num">{formatNumber(row.exact_request_count)} / {formatNumber(row.estimated_request_count)}</span>},
              {
                id: 'unmetered',
                header: t('finops.col.unmetered'),
                width: '18%',
                render: row => (
                  <div className="tp-row">
                    {row.unmetered_request_count > 0 ? <StatusPill tone="danger" label={formatOperationalState('UNMETERED_FLAGGED')} /> : null}
                    <span className="num">{formatNumber(row.unmetered_request_count)}</span>
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

function errText(cause: unknown, t: (key: string) => string): string {
  return cause instanceof Error ? cause.message : t('common.requestFailed');
}
