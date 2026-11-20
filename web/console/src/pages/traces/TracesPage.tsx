import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {EyeOff, Search} from 'lucide-react';
import {getTraceWaterfall, listTracePage} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {DataTable} from '../../ui/DataTable';
import {Drawer} from '../../ui/Overlays';
import {Banner, Button, KeyValueList, SectionHeader, StatusPill, TextField, formatMicroUSD, formatOperationalState, stateTone} from '../../ui/primitives';

type Trace = components['schemas']['TraceSummary'];
type Role = components['schemas']['RbacRole'];

export function TracesPage({environmentId, roles}: {environmentId: string; roles: Role[]}) {
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
        if (active) setMessage(cause instanceof Error ? cause.message : 'Traces could not be loaded.');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => { active = false; };
  }, [environmentId, modelFilter, mayReadTraces]);

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
      if (gen === queryGen.current) setMessage(cause instanceof Error ? cause.message : 'Failed to load next page.');
    } finally {
      if (gen === queryGen.current) setLoadingMore(false);
    }
  }, [environmentId, loadingMore, modelFilter, nextCursor]);

  async function inspectById(id: string) {
    const gen = ++detailGen.current;
    setMessage('');
    try {
      const detail = await getTraceWaterfall(id);
      if (gen === detailGen.current) setSelected(detail);
    } catch (cause) {
      if (gen === detailGen.current) {
        setSelected(null);
        setMessage(cause instanceof Error ? cause.message : 'Trace lookup failed.');
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
      <Banner tone="danger" title="Restricted role">
        FINOPS_VIEWER accounts are scoped to budget and chargeback reports only.
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
          <h1>Request traces</h1>
          <p className="tp-page-subtitle">Metadata-only execution telemetry. Zero prompt or completion bytes stored.</p>
        </div>
        <div className="tp-page-actions">
          <span className="tp-meta-tag">
            <EyeOff size={13} aria-hidden="true" />
            <span>Metadata-Only</span>
          </span>
        </div>
      </div>

      <div className="tp-toolbar">
        <div className="tp-toolbar-field">
          <TextField label="Request ID lookup" placeholder="req_…" value={requestId} onChange={event => setRequestId(event.target.value)} />
        </div>
        <div className="tp-toolbar-field">
          <TextField label="Model" placeholder="e.g. gpt-4o" value={modelFilter} onChange={event => setModelFilter(event.target.value)} />
        </div>
        <div className="tp-toolbar-field">
          <TextField label="Error code" placeholder="e.g. rate_limited" value={errorFilter} onChange={event => setErrorFilter(event.target.value)} />
        </div>
        <Button variant="primary" onClick={handleSearch}>
          <Search size={14} aria-hidden="true" />
          <span>Search</span>
        </Button>
      </div>

      {message ? <Banner tone="danger">{message}</Banner> : null}

      <section className="tp-section">
        <SectionHeader title="Recent requests" count={visible.length} />
        <DataTable
          ariaLabel="Request traces"
          loading={loading}
          rows={visible.slice(0, visibleLimit)}
          rowKey={row => row.request_id}
          onRowClick={row => {
            setRequestId(row.request_id);
            void inspectById(row.request_id);
          }}
          emptyTitle="No matching traces"
          emptyDescription="Adjust filters or send traffic through the gateway to record metadata traces."
          columns={[
            {
              id: 'request_id',
              header: 'Request ID',
              primary: true,
              width: '24%',
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
            {id: 'route', header: 'Route', width: '18%', render: row => <span className="mono">{row.route_slug}</span>},
            {
              id: 'status',
              header: 'Status',
              width: '12%',
              render: row => (
                <StatusPill
                  tone={row.status_code >= 500 ? 'danger' : row.status_code >= 400 ? 'warn' : 'ok'}
                  label={String(row.status_code)}
                />
              ),
            },
            {id: 'error', header: 'Error', width: '14%', render: row => <span className="mono">{row.error_code ?? '—'}</span>},
            {
              id: 'accuracy',
              header: 'Metering',
              width: '14%',
              render: row => <StatusPill tone={stateTone(row.usage_accuracy)} label={formatOperationalState(row.usage_accuracy)} />,
            },
            {
              id: 'duration',
              header: 'Duration',
              width: '10%',
              render: row => <span className="num">{row.duration_ms.toLocaleString('en-US')} ms</span>,
            },
            {
              id: 'created',
              header: 'Timestamp',
              width: '14%',
              hideOnMobile: false,
              render: row => <span className="num tp-muted">{new Date(row.created_at).toLocaleTimeString()}</span>,
            },
          ]}
        />

        <div className="tp-row">
          {visibleLimit < visible.length ? (
            <Button variant="secondary" size="sm" onClick={() => setVisibleLimit(count => count + 25)}>
              Show more ({visible.length - visibleLimit} remaining)
            </Button>
          ) : null}
          {!loading && nextCursor ? (
            <Button variant="secondary" size="sm" loading={loadingMore} onClick={() => void loadMore()}>
              {loadingMore ? 'Loading next page…' : 'Load older traces'}
            </Button>
          ) : null}
        </div>
      </section>

      <Drawer
        open={selected !== null}
        onClose={() => setSelected(null)}
        size="lg"
        title="Trace waterfall"
        subtitle={selected?.request_id}
      >
        {selected ? (
          <>
            <KeyValueList
              items={[
                {label: 'Request ID', value: selected.request_id, mono: true},
                {label: 'Route', value: selected.route_slug, mono: true},
                {
                  label: 'HTTP Status / Error',
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
                  label: 'Usage accuracy & Cost',
                  value: (
                    <div className="tp-row">
                      <StatusPill tone={stateTone(selected.usage_accuracy)} label={formatOperationalState(selected.usage_accuracy)} />
                      <span className="mono">{formatMicroUSD(selected.total_cost_micros)}</span>
                    </div>
                  ),
                },
                {label: 'Total duration', value: `${selected.duration_ms.toLocaleString('en-US')} ms`, mono: true},
                {label: 'Recorded at', value: new Date(selected.created_at).toLocaleString()},
              ]}
            />

            <section className="tp-section">
              <h3>Execution stages</h3>
              {(selected.waterfall_stages ?? []).length === 0 ? (
                <p className="tp-muted">No granular stage breakdown recorded for this request.</p>
              ) : (
                <ul className="tp-list">
                  {(selected.waterfall_stages ?? []).map((stage, index) => {
                    const widthPct = Math.min(100, Math.max(4, Math.round((stage.duration_ms / totalWaterfallMs) * 100)));
                    return (
                      <li key={`${stage.stage}-${index}`} className="tp-list-item" style={{flexDirection: 'column', alignItems: 'stretch'}}>
                        <div className="tp-row" style={{justifyContent: 'space-between'}}>
                          <span className="mono" style={{fontWeight: 600}}>{stage.stage}</span>
                          <div className="tp-row">
                            <StatusPill tone={stateTone(stage.outcome)} label={stage.outcome} />
                            <span className="mono">{stage.duration_ms} ms</span>
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
