import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {EyeOff, Search} from 'lucide-react';
import {getTraceWaterfall, listTracePage} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {DataTable} from '../../ui/DataTable';
import {Drawer} from '../../ui/Overlays';
import {Banner, Button, KeyValueList, SectionHeader, StatusPill, TextField, formatOperationalState, stateTone} from '../../ui/primitives';

type Trace = components['schemas']['TraceSummary'];
type Role = components['schemas']['RbacRole'];

export function TracesPage({environmentId, roles}: {environmentId: string; roles: Role[]}) {
  const {t, formatNumber, formatDateTime, formatTime, formatMicroUSD} = useI18n();
  const [traces, setTraces] = useState<Trace[]>([]);
  const [requestId, setRequestId] = useState('');
  const [errorFilter, setErrorFilter] = useState('');
  const [appliedError, setAppliedError] = useState('');
  const [modelFilter, setModelFilter] = useState('');
  const [selected, setSelected] = useState<Trace | null>(null);
  const [nextCursor, setNextCursor] = useState<string | undefined>();
  const [visibleLimit, setVisibleLimit] = useState(25);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [message, setMessage] = useState('');
  const queryGen = useRef(0);
  const detailGen = useRef(0);

  const mayReadTraces = !roles.includes('FINOPS_VIEWER') || roles.some(role => role !== 'FINOPS_VIEWER');

  useEffect(() => {
    let active = true;
    queryGen.current += 1;
    setTraces([]);
    setNextCursor(undefined);
    setVisibleLimit(25);
    if (!mayReadTraces) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setMessage('');
    listTracePage(environmentId, modelFilter)
      .then(page => {
        if (!active) return;
        setTraces(page.data);
        setNextCursor(page.nextCursor);
      })
      .catch(cause => {
        if (active) setMessage(cause instanceof Error ? cause.message : t('traces.loadFailed'));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => { active = false; };
  }, [environmentId, modelFilter, mayReadTraces, t]);

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) return;
    const gen = queryGen.current;
    setLoadingMore(true);
    setMessage('');
    try {
      const page = await listTracePage(environmentId, modelFilter, nextCursor);
      if (gen !== queryGen.current) return;
      setTraces(current => [...current, ...page.data]);
      setNextCursor(page.nextCursor);
    } catch (cause) {
      if (gen === queryGen.current) setMessage(cause instanceof Error ? cause.message : t('traces.loadMoreFailed'));
    } finally {
      if (gen === queryGen.current) setLoadingMore(false);
    }
  }, [environmentId, loadingMore, modelFilter, nextCursor, t]);

  async function inspectById(id: string) {
    const gen = ++detailGen.current;
    setMessage('');
    try {
      const detail = await getTraceWaterfall(id);
      if (gen === detailGen.current) setSelected(detail);
    } catch (cause) {
      if (gen === detailGen.current) {
        setSelected(null);
        setMessage(cause instanceof Error ? cause.message : t('traces.lookupFailed'));
      }
    }
  }

  function handleSearch() {
    setAppliedError(errorFilter.trim());
    if (requestId.trim()) {
      void inspectById(requestId.trim());
    }
  }

  const visible = useMemo(
    () => traces.filter(item => !appliedError || (item.error_code ?? '').toLowerCase().includes(appliedError.toLowerCase())),
    [traces, appliedError],
  );

  if (!mayReadTraces) {
    return (
      <Banner tone="danger" title={t('traces.restrictedTitle')}>
        {t('traces.restrictedDesc')}
      </Banner>
    );
  }

  const totalWaterfallMs = Math.max(
    1,
    ...(selected?.waterfall_stages?.map(stage => stage.duration_ms) ?? [1]),
    selected?.duration_ms ?? 1,
  );

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('traces.title')}</h1>
          <p className="tp-page-subtitle">{t('traces.subtitle')}</p>
        </div>
        <div className="tp-page-actions">
          <span className="tp-meta-tag">
            <EyeOff size={13} aria-hidden="true" />
            <span>{t('traces.metadataOnly')}</span>
          </span>
        </div>
      </div>

      <div className="tp-toolbar">
        <div className="tp-toolbar-field tp-toolbar-field-wide">
          <TextField label={t('traces.requestIdLookup')} fieldSize="sm" placeholder="req_…" value={requestId} onChange={event => setRequestId(event.target.value)} />
        </div>
        <div className="tp-toolbar-field">
          <TextField label={t('traces.modelFilter')} fieldSize="sm" placeholder={t('traces.modelPlaceholder')} value={modelFilter} onChange={event => setModelFilter(event.target.value)} />
        </div>
        <div className="tp-toolbar-field">
          <TextField label={t('traces.errorCodeFilter')} fieldSize="sm" placeholder={t('traces.errorCodePlaceholder')} value={errorFilter} onChange={event => setErrorFilter(event.target.value)} />
        </div>
        <Button variant="primary" size="sm" onClick={handleSearch}>
          <Search size={14} aria-hidden="true" />
          <span>{t('common.search')}</span>
        </Button>
      </div>

      {message ? <Banner tone="danger">{message}</Banner> : null}

      <section className="tp-section">
        <SectionHeader title={t('traces.recentRequests')} count={visible.length} />
        <DataTable
          ariaLabel={t('traces.tableAria')}
          loading={loading}
          rows={visible.slice(0, visibleLimit)}
          rowKey={row => row.request_id}
          onRowClick={row => {
            setRequestId(row.request_id);
            void inspectById(row.request_id);
          }}
          emptyTitle={t('traces.emptyTitle')}
          emptyDescription={t('traces.emptyDesc')}
          columns={[
            {
              id: 'request_id',
              header: t('traces.col.requestId'),
              primary: true,
              width: '23%',
              render: row => (
                <Button
                  variant="ghost"
                  size="sm"
                  className="tp-table-link-btn"
                  onClick={event => {
                    event.stopPropagation();
                    setRequestId(row.request_id);
                    void inspectById(row.request_id);
                  }}
                >
                  <span className="mono">{row.request_id}</span>
                </Button>
              ),
            },
            {id: 'route', header: t('traces.col.route'), width: '16%', render: row => <span className="mono">{row.route_slug}</span>},
            {
              id: 'status',
              header: t('traces.col.status'),
              width: '8%',
              render: row => (
                <StatusPill
                  tone={row.status_code >= 500 ? 'danger' : row.status_code >= 400 ? 'warn' : 'ok'}
                  label={String(row.status_code)}
                />
              ),
            },
            {id: 'error', header: t('traces.col.error'), width: '23%', render: row => <span className="mono">{row.error_code ?? '—'}</span>},
            {
              id: 'accuracy',
              header: t('traces.col.metering'),
              width: '10%',
              render: row => <StatusPill tone={stateTone(row.usage_accuracy)} label={formatOperationalState(row.usage_accuracy)} />,
            },
            {
              id: 'duration',
              header: t('traces.col.duration'),
              width: '9%',
              render: row => <span className="num">{t('common.ms', {value: formatNumber(row.duration_ms)})}</span>,
            },
            {
              id: 'created',
              header: t('traces.col.timestamp'),
              width: '11%',
              hideOnMobile: false,
              render: row => <span className="num tp-muted">{formatTime(row.created_at)}</span>,
            },
          ]}
        />

        <div className="tp-row">
          {visibleLimit < visible.length ? (
            <Button variant="secondary" size="sm" onClick={() => setVisibleLimit(count => count + 25)}>
              {t('traces.showMore', {count: formatNumber(visible.length - visibleLimit)})}
            </Button>
          ) : null}
          {!loading && nextCursor ? (
            <Button variant="secondary" size="sm" loading={loadingMore} onClick={() => void loadMore()}>
              {loadingMore ? t('traces.loadingNext') : t('traces.loadOlder')}
            </Button>
          ) : null}
        </div>
      </section>

      <Drawer
        open={selected !== null}
        onClose={() => setSelected(null)}
        size="lg"
        title={t('traces.waterfallTitle')}
        subtitle={selected?.request_id}
      >
        {selected ? (
          <>
            <KeyValueList
              items={[
                {label: t('traces.col.requestId'), value: selected.request_id, mono: true},
                {label: t('traces.col.route'), value: selected.route_slug, mono: true},
                {
                  label: t('traces.httpStatusError'),
                  value: (
                    <div className="tp-row">
                      <StatusPill
                        tone={selected.status_code >= 500 ? 'danger' : selected.status_code >= 400 ? 'warn' : 'ok'}
                        label={String(selected.status_code)}
                      />
                      {selected.error_code ? <span className="mono">{selected.error_code}</span> : null}
                    </div>
                  ),
                },
                {
                  label: t('traces.usageAccuracyCost'),
                  value: (
                    <div className="tp-row">
                      <StatusPill tone={stateTone(selected.usage_accuracy)} label={formatOperationalState(selected.usage_accuracy)} />
                      <span className="mono">{formatMicroUSD(selected.total_cost_micros)}</span>
                    </div>
                  ),
                },
                {label: t('traces.totalDuration'), value: t('common.ms', {value: formatNumber(selected.duration_ms)}), mono: true},
                {label: t('traces.recordedAt'), value: formatDateTime(selected.created_at)},
              ]}
            />

            <section className="tp-section">
              <h3>{t('traces.executionStages')}</h3>
              {(selected.waterfall_stages ?? []).length === 0 ? (
                <p className="tp-muted">{t('traces.noStages')}</p>
              ) : (
                <ul className="tp-list">
                  {(selected.waterfall_stages ?? []).map((stage, index) => {
                    const widthPct = Math.min(100, Math.max(4, Math.round((stage.duration_ms / totalWaterfallMs) * 100)));
                    return (
                      <li key={`${stage.stage}-${index}`} className="tp-list-item" style={{flexDirection: 'column', alignItems: 'stretch'}}>
                        <div className="tp-row" style={{justifyContent: 'space-between'}}>
                          <span className="mono" style={{fontWeight: 600}}>{stage.stage}</span>
                          <div className="tp-row">
                            <StatusPill tone={stateTone(stage.outcome)} label={formatOperationalState(stage.outcome)} />
                            <span className="mono">{t('common.ms', {value: formatNumber(stage.duration_ms)})}</span>
                          </div>
                        </div>
                        <div className="tp-waterfall-bar-track" aria-hidden="true">
                          <div
                            className="tp-waterfall-bar-fill"
                            data-tone={stateTone(stage.outcome) === 'danger' ? 'danger' : undefined}
                            style={{width: `${widthPct}%`}}
                          />
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>
          </>
        ) : null}
      </Drawer>
    </div>
  );
}
