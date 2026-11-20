import {useEffect, useRef, useState} from 'react';
import {ArrowRight, RefreshCw} from 'lucide-react';
import {getOverviewSummary, listActiveAlerts, listUnhealthyDeployments} from '../../api/client';
import type {components} from '../../api/generated';
import {useRouter} from '../../router/RouterContext';
import {Banner, Button, EmptyState, MetricStrip, SectionHeader, StatusPill, formatMicroUSD} from '../../ui/primitives';

type WindowRange = components['schemas']['OverviewSummaryResponse']['window'];
type Summary = components['schemas']['OverviewSummaryResponse'];
type AlertItem = components['schemas']['AlertItem'];
type Deployment = components['schemas']['ModelDeployment'];

const windows: WindowRange[] = ['1h', '24h', '7d', '30d'];

export function OverviewPage() {
  const {navigate} = useRouter();
  const [timeWindow, setTimeWindow] = useState<WindowRange>('24h');
  const [summary, setSummary] = useState<Summary | null>(null);
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  const [unhealthy, setUnhealthy] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [announcement, setAnnouncement] = useState('');
  const seenAlertIds = useRef(new Set<string>());
  const loadedOnce = useRef(false);

  async function refresh(windowKey: WindowRange, isBackground = false) {
    if (!isBackground) setLoading(true);
    try {
      const [nextSummary, nextAlerts, nextUnhealthy] = await Promise.all([
        getOverviewSummary(windowKey),
        listActiveAlerts(),
        listUnhealthyDeployments(),
      ]);
      const arrived = nextAlerts.filter(item => !seenAlertIds.current.has(item.id));
      if (loadedOnce.current && arrived.length > 0) {
        setAnnouncement(`${arrived.length} new active alert${arrived.length === 1 ? '' : 's'}.`);
      }
      seenAlertIds.current = new Set(nextAlerts.map(item => item.id));
      loadedOnce.current = true;
      setSummary(nextSummary);
      setAlerts(nextAlerts);
      setUnhealthy(nextUnhealthy);
      setError('');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Overview telemetry could not be loaded.');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh(timeWindow);
    const interval = window.setInterval(() => {
      void refresh(timeWindow, true);
    }, 10000);
    return () => window.clearInterval(interval);
  }, [timeWindow]);

  const kpis = summary
    ? [
        {label: 'Requests', value: summary.total_requests.toLocaleString('en-US'), detail: `Window: ${summary.window}`},
        {label: 'Error rate', value: `${(summary.error_rate_ratio * 100).toFixed(2)}%`, detail: 'Internal & upstream'},
        {label: 'p95 proxy overhead', value: `${summary.p95_overhead_ms.toLocaleString('en-US')} ms`, detail: 'Gateway added latency'},
        {
          label: 'Settled spend',
          value: formatMicroUSD(summary.settled_micro_usd),
          detail: `Active holds: ${formatMicroUSD(summary.active_hold_micros)}`,
        },
      ]
    : [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Overview</h1>
          <p className="tp-page-subtitle">
            {summary
              ? `Updated ${new Date(summary.updated_at).toLocaleTimeString()} · ${summary.unmetered_request_count ?? 0} unmetered requests`
              : 'Live gateway posture and remediation queue.'}
          </p>
        </div>

        <div className="tp-page-actions">
          <div className="tp-segmented" role="group" aria-label="Overview time window">
            {windows.map(item => (
              <button
                key={item}
                type="button"
                className="tp-segmented-btn"
                aria-pressed={timeWindow === item}
                onClick={() => setTimeWindow(item)}
              >
                {item}
              </button>
            ))}
          </div>
          <Button variant="secondary" size="sm" onClick={() => void refresh(timeWindow)}>
            <RefreshCw size={14} aria-hidden="true" />
            <span>Refresh</span>
          </Button>
        </div>
      </div>

      <span className="sr-only" role="status" aria-live="polite">{announcement}</span>

      {summary?.sync_lag_warning ? (
        <Banner tone="warn" title="Telemetry sync delayed">
          Recent spans were dropped under load. Summary metrics may lag behind live traffic.
        </Banner>
      ) : null}

      {error ? <Banner tone="danger" title="Refresh failed">{error}</Banner> : null}

      {loading && !summary ? (
        <div className="tp-surface tp-surface-pad tp-stack" role="status" aria-busy="true">
          <div className="tp-skeleton" style={{width: '100%', height: 64}} />
        </div>
      ) : summary ? (
        <MetricStrip items={kpis} />
      ) : null}

      {unhealthy.length > 0 ? (
        <section className="tp-section" aria-label="Unhealthy deployments">
          <SectionHeader
            title="Circuit-open deployments"
            count={unhealthy.length}
            countTone="danger"
            actions={
              <Button variant="secondary" size="sm" onClick={() => navigate('/models')}>
                <span>Inspect deployments</span>
                <ArrowRight size={14} aria-hidden="true" />
              </Button>
            }
          />
          <ul className="tp-list">
            {unhealthy.map(dep => (
              <li key={dep.id} className="tp-list-item">
                <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 200px'}}>
                  <span className="mono" style={{fontWeight: 600}}>{dep.upstream_model_name}</span>
                  <span className="tp-muted">Region {dep.data_region} · Connection {dep.connection_id}</span>
                </div>
                <StatusPill tone="danger" label={dep.state.replaceAll('_', ' ')} />
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="tp-section" aria-label="Active alerts">
        <SectionHeader
          title="Active alerts"
          count={summary?.active_alerts_count ?? alerts.length}
          countTone={alerts.length > 0 ? 'warn' : 'neutral'}
        />
        {alerts.length === 0 && !loading ? (
          <EmptyState
            title="All systems nominal"
            description="No active operational alerts or pending remediation items."
          />
        ) : (
          <ul className="tp-list">
            {alerts.map(alert => {
              const target = new URL(alert.remediation_route, window.location.origin);
              const safeInternal = alert.remediation_route.startsWith('/') && target.origin === window.location.origin;
              return (
                <li key={alert.id} className="tp-list-item">
                  <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 240px'}}>
                    <div className="tp-row">
                      <StatusPill tone={alert.severity === 'PAGE' ? 'danger' : 'warn'} label={alert.severity} />
                      <span style={{fontWeight: 600}}>{alert.summary}</span>
                    </div>
                    <span className="tp-muted mono">
                      {alert.alert_code} · {new Date(alert.triggered_at).toLocaleString()}
                    </span>
                  </div>
                  {safeInternal ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => navigate(`${target.pathname}${target.search}${target.hash}`)}
                    >
                      <span>Remediate</span>
                      <ArrowRight size={14} aria-hidden="true" />
                    </Button>
                  ) : null}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </div>
  );
}
