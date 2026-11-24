import {useCallback, useEffect, useRef, useState, type FormEvent} from 'react';
import {Download, Plus} from 'lucide-react';
import {
  createBudget,
  exportUsageChargeback,
  getUsageChargebackSummary,
  listBudgets,
  listVirtualKeys,
  updateBudget,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {Drawer} from '../../ui/Overlays';
import {Banner, Button, MetricStrip, SectionHeader, SelectField, StatusPill, Tabs, TextField, formatOperationalState} from '../../ui/primitives';

type Budget = components['schemas']['BudgetResource'];
type Chargeback = components['schemas']['UsageChargebackRow'];
type Role = components['schemas']['RbacRole'];
type Project = components['schemas']['Project'];
type Environment = components['schemas']['Environment'];
type VirtualKey = components['schemas']['VirtualKeySummary'];
type GroupBy = 'project' | 'env' | 'key' | 'model';

const manageRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN'];

export function FinOpsPage({
  pathname,
  activeOrgId = '',
  projects = [],
  environments = [],
  projectId,
  environmentId = '',
  roles,
}: {
  pathname: string;
  activeOrgId?: string;
  projects?: Project[];
  environments?: Environment[];
  projectId: string;
  environmentId?: string;
  roles: Role[];
}) {
  const {navigate} = useRouter();
  const {t, formatNumber, formatMicroUSD} = useI18n();
  const activeTab = pathname.startsWith('/finops/chargeback') ? 'chargeback' : 'budgets';
  const [budgets, setBudgets] = useState<Budget[]>([]);
  const [rows, setRows] = useState<Chargeback[]>([]);
  const [keys, setKeys] = useState<VirtualKey[]>([]);
  const [groupBy, setGroupBy] = useState<GroupBy>('project');
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');

  const [createOpen, setCreateOpen] = useState(false);
  const [scopeTier, setScopeTier] = useState<Budget['scope_tier']>(projectId ? 'PROJECT' : 'ORGANIZATION');
  const [scopeId, setScopeId] = useState(projectId || activeOrgId);
  const [period, setPeriod] = useState<Budget['period']>('MONTHLY');
  const [hardLimitText, setHardLimitText] = useState('100000000');
  const [warningsText, setWarningsText] = useState('50, 80, 95');
  const [budgetFormError, setBudgetFormError] = useState('');
  const [duplicateExisting, setDuplicateExisting] = useState<Budget | null>(null);
  const refreshSeqRef = useRef(0);

  const isOrgOwner = roles.includes('ORG_OWNER');
  const canManage = roles.some(role => manageRoles.includes(role));

  const refresh = useCallback(async () => {
    const seq = ++refreshSeqRef.current;
    setLoading(true);
    setError('');
    const [budgetsRes, chargebackRes, keysRes] = await Promise.allSettled([
      listBudgets(projectId, Boolean(projectId)),
      getUsageChargebackSummary(groupBy),
      projectId && environmentId ? listVirtualKeys(projectId, environmentId) : Promise.resolve([]),
    ]);
    if (refreshSeqRef.current !== seq) return;
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
    if (keysRes.status === 'fulfilled') {
      setKeys(keysRes.value);
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, environmentId, groupBy, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  function openCreateBudget() {
    const initialTier: Budget['scope_tier'] = projectId ? 'PROJECT' : 'ORGANIZATION';
    setScopeTier(initialTier);
    setScopeId(initialTier === 'PROJECT' ? projectId : activeOrgId);
    setPeriod('MONTHLY');
    setHardLimitText('100000000');
    setWarningsText('50, 80, 95');
    setBudgetFormError('');
    setDuplicateExisting(null);
    setCreateOpen(true);
  }

  async function handleCreateBudget(event: FormEvent) {
    event.preventDefault();
    setBudgetFormError('');
    setDuplicateExisting(null);
    const hardLimit = Number.parseInt(hardLimitText.trim(), 10);
    if (!Number.isSafeInteger(hardLimit) || hardLimit < 0) {
      setBudgetFormError(t('finops.invalidLimit'));
      return;
    }
    const targetScopeId = scopeId.trim();
    if (!targetScopeId) {
      setBudgetFormError(t('finops.scopeIdRequired'));
      return;
    }
    const existing = budgets.find(
      b => b.scope_tier === scopeTier && b.scope_id === targetScopeId && b.period === period,
    );
    if (existing) {
      setDuplicateExisting(existing);
      setBudgetFormError(t('finops.errBudgetExists'));
      return;
    }
    const rawTokens = warningsText
      .split(',')
      .map(v => v.trim())
      .filter(Boolean);
    const parsedWarnings = [...new Set(rawTokens.map(v => Number.parseInt(v, 10)).filter(n => Number.isSafeInteger(n) && n >= 1 && n <= 100))];
    if (rawTokens.length > 0 && parsedWarnings.length !== rawTokens.length) {
      setBudgetFormError(t('finops.invalidWarnings'));
      return;
    }
    const safeSlug = `bgt_${scopeTier.toLowerCase()}_${targetScopeId.replace(/[^a-zA-Z0-9_-]/g, '_')}_${period.toLowerCase()}`;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await createBudget(safeSlug, {
        scope_tier: scopeTier,
        scope_id: targetScopeId,
        hard_limit_micros: hardLimit,
        period,
        soft_warning_percents: parsedWarnings.length > 0 ? parsedWarnings : [50, 80, 95],
      });
      setCreateOpen(false);
      await refresh();
      setNotice(t('finops.budgetCreated', {scope: scopeTier, id: targetScopeId}));
    } catch (cause) {
      setBudgetFormError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleUpdateDuplicateExisting() {
    if (!duplicateExisting) return;
    const hardLimit = Number.parseInt(hardLimitText.trim(), 10);
    if (!Number.isSafeInteger(hardLimit) || hardLimit < 0) {
      setBudgetFormError(t('finops.invalidLimit'));
      return;
    }
    setBusy(true);
    setBudgetFormError('');
    setError('');
    setNotice('');
    try {
      await updateBudget(duplicateExisting, hardLimit);
      setCreateOpen(false);
      setDuplicateExisting(null);
      await refresh();
      setNotice(t('finops.savedLimit', {scope: duplicateExisting.scope_tier, id: duplicateExisting.scope_id}));
    } catch (cause) {
      setBudgetFormError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function saveLimit(budget: Budget) {
    const rawLimit = (drafts[budget.id] ?? '').trim();
    const nextValue = rawLimit === '' ? Number.NaN : Number(rawLimit);
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
      const blob = await exportUsageChargeback(format, groupBy);
      const href = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = href;
      anchor.download = `tanpopo-chargeback-${groupBy}.${format}`;
      anchor.click();
      window.setTimeout(() => URL.revokeObjectURL(href), 1000);
      setNotice(t('finops.exported', {format: format.toUpperCase()}));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  const totalExact = rows.reduce((sum, item) => sum + item.exact_micro_usd, 0);
  const totalEstimated = rows.reduce((sum, item) => sum + item.estimated_micro_usd, 0);
  const totalUnmetered = rows.reduce((sum, item) => sum + item.unmetered_request_count, 0);
  const holdTierPriority: Budget['scope_tier'][] = ['ORGANIZATION', 'PROJECT', 'ENVIRONMENT', 'VIRTUAL_KEY'];
  const holdTier = holdTierPriority.find(tier => budgets.some(item => item.scope_tier === tier));
  const totalHolds = holdTier
    ? budgets.filter(item => item.scope_tier === holdTier).reduce((sum, item) => sum + item.reserved_hold_micros, 0)
    : 0;

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
          {canManage ? (
            <Button variant="primary" size="sm" disabled={busy} onClick={openCreateBudget}>
              <Plus size={14} aria-hidden="true" />
              <span>{t('finops.createBudget')}</span>
            </Button>
          ) : null}
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
            emptyAction={
              canManage ? (
                <Button variant="primary" size="md" disabled={busy} onClick={openCreateBudget}>
                  <Plus size={14} aria-hidden="true" />
                  <span>{t('finops.createBudget')}</span>
                </Button>
              ) : undefined
            }
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
                    {row.scope_tier === 'ORGANIZATION' && projectId && !isOrgOwner ? (
                      <StatusPill tone="neutral" label={t('finops.inheritedReadOnly')} />
                    ) : null}
                  </div>
                ),
              },
              {id: 'period', header: t('finops.col.period'), width: '10%', render: row => <span>{t(`finops.period.${row.period}`)}</span>},
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
                render: row => {
                  const canEditRow = canManage && (row.scope_tier !== 'ORGANIZATION' || isOrgOwner);
                  return (
                    <div className="tp-row" style={{alignItems: 'flex-end'}}>
                      <div style={{flex: '1 1 120px', minWidth: 0}}>
                        <TextField
                          label={t('finops.hardLimitFor', {scope: row.scope_tier, id: row.scope_id})}
                          fieldSize="sm"
                          inputMode="numeric"
                          value={drafts[row.id] ?? String(row.hard_limit_micros)}
                          disabled={!canEditRow || busy}
                          onChange={event => setDrafts(curr => ({...curr, [row.id]: event.target.value}))}
                        />
                      </div>
                      {canEditRow ? (
                        <Button variant="secondary" size="sm" disabled={busy} onClick={() => void saveLimit(row)}>
                          {t('common.save')}
                        </Button>
                      ) : null}
                    </div>
                  );
                },
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

      <Drawer
        open={createOpen}
        onClose={() => {
          setCreateOpen(false);
          setBudgetFormError('');
          setDuplicateExisting(null);
        }}
        title={t('finops.createBudgetTitle')}
        subtitle={t('finops.createBudgetSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setCreateOpen(false)}>{t('common.cancel')}</Button>
            {duplicateExisting ? (
              <Button variant="primary" loading={busy} onClick={() => void handleUpdateDuplicateExisting()}>
                {t('finops.updateExistingBudget')}
              </Button>
            ) : (
              <Button type="submit" form="tp-create-budget-form" variant="primary" loading={busy} disabled={!scopeId.trim()}>
                {t('finops.createBudget')}
              </Button>
            )}
          </>
        }
      >
        <form id="tp-create-budget-form" className="tp-stack" onSubmit={handleCreateBudget}>
          {budgetFormError ? <Banner tone="danger">{budgetFormError}</Banner> : null}
          <SelectField
            label={t('finops.col.scope')}
            value={scopeTier}
            onChange={next => {
              const tier = next as Budget['scope_tier'];
              setScopeTier(tier);
              setBudgetFormError('');
              setDuplicateExisting(null);
              if (tier === 'ORGANIZATION') setScopeId(activeOrgId);
              else if (tier === 'PROJECT') setScopeId(projectId || projects[0]?.id || '');
              else if (tier === 'ENVIRONMENT') setScopeId(environmentId || environments[0]?.id || '');
              else setScopeId(keys[0]?.id || '');
            }}
            options={[
              {value: 'ORGANIZATION', label: t('finops.scope.ORGANIZATION')},
              {value: 'PROJECT', label: t('finops.scope.PROJECT')},
              {value: 'ENVIRONMENT', label: t('finops.scope.ENVIRONMENT')},
              {value: 'VIRTUAL_KEY', label: t('finops.scope.VIRTUAL_KEY')},
            ]}
          />
          {scopeTier === 'PROJECT' && projects.length > 0 ? (
            <SelectField
              label={t('finops.scopeTarget')}
              value={scopeId}
              onChange={next => {
                setScopeId(next);
                setBudgetFormError('');
                setDuplicateExisting(null);
              }}
              options={projects.map(p => ({value: p.id, label: `${p.name} (${p.slug})`}))}
            />
          ) : scopeTier === 'ENVIRONMENT' && environments.length > 0 ? (
            <SelectField
              label={t('finops.scopeTarget')}
              value={scopeId}
              onChange={next => {
                setScopeId(next);
                setBudgetFormError('');
                setDuplicateExisting(null);
              }}
              options={environments.map(e => ({value: e.id, label: `${e.name} (${e.slug})`}))}
            />
          ) : scopeTier === 'VIRTUAL_KEY' && keys.length > 0 ? (
            <SelectField
              label={t('finops.scopeTarget')}
              value={scopeId}
              onChange={next => {
                setScopeId(next);
                setBudgetFormError('');
                setDuplicateExisting(null);
              }}
              options={keys.map(k => ({value: k.id, label: `${k.name} (${k.key_prefix}…${k.last_four})`}))}
            />
          ) : (
            <TextField
              label={t('finops.scopeTarget')}
              required
              value={scopeId}
              onChange={event => {
                setScopeId(event.target.value);
                setBudgetFormError('');
                setDuplicateExisting(null);
              }}
            />
          )}
          <div className="tp-grid-2">
            <SelectField
              label={t('finops.col.period')}
              value={period}
              onChange={next => {
                setPeriod(next as Budget['period']);
                setBudgetFormError('');
                setDuplicateExisting(null);
              }}
              options={[
                {value: 'MONTHLY', label: t('finops.period.MONTHLY')},
                {value: 'DAILY', label: t('finops.period.DAILY')},
                {value: 'TOTAL_LIFETIME', label: t('finops.period.TOTAL_LIFETIME')},
              ]}
            />
            <TextField
              label={t('finops.col.hardLimit')}
              type="number"
              min={0}
              required
              value={hardLimitText}
              onChange={event => setHardLimitText(event.target.value)}
            />
          </div>
          <TextField
            label={t('finops.warningPercents')}
            placeholder="50, 80, 95"
            value={warningsText}
            onChange={event => setWarningsText(event.target.value)}
          />
        </form>
      </Drawer>
    </div>
  );
}

function errText(cause: unknown, t: (key: string) => string): string {
  return cause instanceof Error ? cause.message : t('common.requestFailed');
}
