import {useCallback, useEffect, useRef, useState} from 'react';
import {ArrowRight, RefreshCw} from 'lucide-react';
import {SegmentedControl, SegmentedControlItem} from '@astryxdesign/core/SegmentedControl';
import {Skeleton} from '@astryxdesign/core/Skeleton';
import {getOverviewSummary, listActiveAlerts, listUnhealthyDeployments} from '../../api/client';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {Banner, Button, EmptyState, MetricStrip, SectionHeader, StatusPill, formatOperationalState} from '../../ui/primitives';

type WindowRange = components['schemas']['OverviewSummaryResponse']['window'];
type Summary = components['schemas']['OverviewSummaryResponse'];
type AlertItem = components['schemas']['AlertItem'];
type Deployment = components['schemas']['ModelDeployment'];

const windows: WindowRange[] = ['1h', '24h', '7d', '30d'];

export function OverviewPage() {
  const {navigate} = useRouter();
  const {locale, t, formatNumber, formatPercent, formatDateTime, formatTime, formatMicroUSD} = useI18n();
  const [timeWindow, setTimeWindow] = useState<WindowRange>('24h');
  const [summary, setSummary] = useState<Summary | null>(null);
  const [alerts, setAlerts] = useState<AlertItem[]>([]);
  const [unhealthy, setUnhealthy] = useState<Deployment[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [announcement, setAnnouncement] = useState('');
  const seenAlertIds = useRef(new Set<string>());
  const loadedOnce = useRef(false);

  const refresh = useCallback(async (windowKey: WindowRange, isBackground = false) => {
    if (!isBackground) setLoading(true);
    try {
      const [nextSummary, nextAlerts, nextUnhealthy] = await Promise.all([
        getOverviewSummary(windowKey),
        listActiveAlerts(),
        listUnhealthyDeployments(),
      ]);
      const arrived = nextAlerts.filter(item => !seenAlertIds.current.has(item.id));
      if (loadedOnce.current && arrived.length > 0) {
        setAnnouncement(
          t(arrived.length === 1 ? 'overview.newAlertsOne' : 'overview.newAlertsMany', {count: arrived.length}),
        );
      }
      seenAlertIds.current = new Set(nextAlerts.map(item => item.id));
      loadedOnce.current = true;
      setSummary(nextSummary);
      setAlerts(nextAlerts);
      setUnhealthy(nextUnhealthy);
      setError('');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('overview.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void refresh(timeWindow);
    const interval = window.setInterval(() => {
      void refresh(timeWindow, true);
    }, 10000);
    return () => window.clearInterval(interval);
  }, [timeWindow, locale, refresh]);

  const kpis = summary
    ? [
        {label: t('overview.kpi.requests'), value: formatNumber(summary.total_requests), detail: t('overview.kpi.windowDetail', {window: summary.window})},
        {label: t('overview.kpi.errorRate'), value: formatPercent(summary.error_rate_ratio, 2), detail: t('overview.kpi.errorRateDetail')},
        {label: t('overview.kpi.p95Overhead'), value: t('common.ms', {value: formatNumber(summary.p95_overhead_ms)}), detail: t('overview.kpi.p95OverheadDetail')},
        {
          label: t('overview.kpi.settledSpend'),
          value: formatMicroUSD(summary.settled_micro_usd),
          detail: t('overview.kpi.activeHoldsDetail', {amount: formatMicroUSD(summary.active_hold_micros)}),
        },
      ]
    : [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('overview.title')}</h1>
          <p className="tp-page-subtitle">
            {summary
              ? t('overview.subtitleUpdated', {
                  time: formatTime(summary.updated_at),
                  count: formatNumber(summary.unmetered_request_count ?? 0),
                })
              : t('overview.subtitleIdle')}
          </p>
        </div>

        <div className="tp-page-actions">
          <SegmentedControl
            size="sm"
            label={t('overview.windowLabel')}
            value={timeWindow}
            onChange={next => setTimeWindow(next as WindowRange)}
          >
            {windows.map(item => (
              <SegmentedControlItem key={item} value={item} label={item} />
            ))}
          </SegmentedControl>
          <Button variant="secondary" size="sm" onClick={() => void refresh(timeWindow)}>
            <RefreshCw size={14} aria-hidden="true" />
            <span>{t('common.refresh')}</span>
          </Button>
        </div>
      </div>

      <span className="sr-only" role="status" aria-live="polite">{announcement}</span>

      {summary?.sync_lag_warning ? (
        <Banner tone="warn" title={t('overview.syncLagTitle')}>
          {t('overview.syncLagDesc')}
        </Banner>
      ) : null}

      {error ? <Banner tone="danger" title={t('overview.refreshFailedTitle')}>{error}</Banner> : null}

      {loading && !summary ? (
        <div className="tp-surface tp-surface-pad tp-stack" role="status" aria-busy="true">
          <Skeleton width="100%" height={64} radius={2} />
        </div>
      ) : summary ? (
        <MetricStrip items={kpis} />
      ) : null}

      {unhealthy.length > 0 ? (
        <section className="tp-section" aria-label={t('overview.unhealthyAria')}>
          <SectionHeader
            title={t('overview.circuitOpenTitle')}
            count={unhealthy.length}
            actions={
              <Button variant="secondary" size="sm" onClick={() => navigate('/models')}>
                <span>{t('overview.inspectDeployments')}</span>
                <ArrowRight size={14} aria-hidden="true" />
              </Button>
            }
          />
          <ul className="tp-list">
            {unhealthy.map(dep => (
              <li key={dep.id} className="tp-list-item">
                <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 200px'}}>
                  <span className="mono" style={{fontWeight: 600}}>{dep.upstream_model_name}</span>
                  <span className="tp-muted">{t('overview.regionConnection', {region: dep.data_region, connection: dep.connection_id})}</span>
                </div>
                <StatusPill tone="danger" label={formatOperationalState(dep.state)} />
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      <section className="tp-section" aria-label={t('overview.activeAlertsTitle')}>
        <SectionHeader
          title={t('overview.activeAlertsTitle')}
          count={summary?.active_alerts_count ?? alerts.length}
        />
        {alerts.length === 0 && !loading ? (
          <EmptyState
            title={t('overview.allNominalTitle')}
            description={t('overview.allNominalDesc')}
          />
        ) : (
          <ul className="tp-list">
            {alerts.map(alert => {
              const target = new URL(alert.remediation_route, window.location.origin);
              const safeInternal = alert.remediation_route.startsWith('/') && target.origin === window.location.origin;
              const sevKey = `overview.severity.${alert.severity}`;
              const sevLabel = t(sevKey) === sevKey ? alert.severity : t(sevKey);
              return (
                <li key={alert.id} className="tp-list-item">
                  <div className="tp-stack-sm" style={{minWidth: 0, flex: '1 1 240px'}}>
                    <div className="tp-row">
                      <StatusPill tone={alert.severity === 'PAGE' ? 'danger' : 'warn'} label={sevLabel} />
                      <span style={{fontWeight: 600, minWidth: 0, overflowWrap: 'anywhere'}}>{alert.summary}</span>
                    </div>
                    <span className="tp-muted mono">
                      {alert.alert_code} · {formatDateTime(alert.triggered_at)}
                    </span>
                  </div>
                  {safeInternal ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => navigate(`${target.pathname}${target.search}${target.hash}`)}
                    >
                      <span>{t('overview.remediate')}</span>
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
