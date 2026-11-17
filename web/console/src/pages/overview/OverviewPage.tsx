import {useEffect, useRef, useState} from 'react';
import {Banner} from '@astryxdesign/core/Banner';
import {Button} from '@astryxdesign/core/Button';
import {Card} from '@astryxdesign/core/Card';
import {Grid} from '@astryxdesign/core/Grid';
import {Heading, Text} from '@astryxdesign/core/Text';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {Icon} from '@astryxdesign/core/Icon';
import {Token} from '@astryxdesign/core/Token';
import {SegmentedControl, SegmentedControlItem} from '@astryxdesign/core/SegmentedControl';
import {getOverviewSummary, listActiveAlerts} from '../../api/client';
import type {components} from '../../api/generated';

type Window = components['schemas']['OverviewSummaryResponse']['window'];
type Summary = components['schemas']['OverviewSummaryResponse'];
type Alert = components['schemas']['AlertItem'];
const windows: Window[] = ['1h', '24h', '7d', '30d'];

export function OverviewPage() {
  const [timeWindow, setTimeWindow] = useState<Window>('24h');
  const [summary, setSummary] = useState<Summary | null>(null);
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [announcement, setAnnouncement] = useState('');
  const seenAlertIds = useRef(new Set<string>());
  const hasLoadedAlerts = useRef(false);

  useEffect(() => {
    let active = true;
    async function refresh() {
      try {
        const [nextSummary, nextAlerts] = await Promise.all([getOverviewSummary(timeWindow), listActiveAlerts()]);
        if (!active) return;
        const nextIds = new Set(nextAlerts.map(alert => alert.id));
        const arrived = nextAlerts.filter(alert => !seenAlertIds.current.has(alert.id));
        if (hasLoadedAlerts.current && arrived.length > 0) setAnnouncement(`${arrived.length} new active alert${arrived.length === 1 ? '' : 's'}.`);
        seenAlertIds.current = nextIds;
        hasLoadedAlerts.current = true;
        setSummary(nextSummary);
        setAlerts(nextAlerts);
        setError('');
      } catch (cause) {
        if (active) setError(cause instanceof Error ? cause.message : 'Overview data could not be loaded.');
      } finally {
        if (active) setLoading(false);
      }
    }
    setLoading(true);
    void refresh();
    const timer = globalThis.setInterval(() => { void refresh(); }, 5000);
    return () => { active = false; globalThis.clearInterval(timer); };
  }, [timeWindow]);

  const metrics = summary ? [
    {label: 'Requests', value: summary.total_requests.toLocaleString(), detail: `In the last ${summary.window}`},
    {label: 'Error rate', value: `${(summary.error_rate_ratio * 100).toFixed(2)}%`, detail: 'Internal and upstream'},
    {label: 'p95 proxy overhead', value: `${summary.p95_overhead_ms.toLocaleString()} ms`, detail: 'Request latency'},
    {label: 'Settled spend', value: formatMicroUSD(summary.settled_micro_usd), detail: `Active holds: ${formatMicroUSD(summary.active_hold_micros)}`},
  ] : [];

  return (
    <VStack gap={5}>
      <HStack gap={3} wrap="wrap" vAlign="center">
        <VStack gap={0}>
          <Heading level={1}>Overview</Heading>
          <Text>System posture and actionable alerts</Text>
        </VStack>
        <SegmentedControl label="Overview time window" value={timeWindow} onChange={value => {
          if (windows.includes(value as Window)) setTimeWindow(value as Window);
        }}>
          {windows.map(item => <SegmentedControlItem key={item} value={item} label={item} />)}
        </SegmentedControl>
      </HStack>
      <Text aria-live="polite" role="status">{announcement}</Text>
      {summary?.sync_lag_warning ? <Banner status="warning" title="Telemetry sync is delayed" description="Some recent spans were dropped. The summary may not include all recent requests." collapsible={false} /> : null}
      {error ? <Banner status="error" title="Overview could not refresh" description={error} collapsible={false} /> : null}
      {loading && !summary ? <Text role="status">Loading system posture…</Text> : null}
      {summary ? <Grid gap={3} columns={{minWidth: 240, max: 4}}>
        {metrics.map(metric => <Card key={metric.label} padding={4}>
          <VStack gap={2}>
            <Text type="label">{metric.label}</Text>
            <Heading level={2}>{metric.value}</Heading>
            <Text>{metric.detail}</Text>
          </VStack>
        </Card>)}
      </Grid> : null}
      <HStack gap={2} vAlign="center">
        <Heading level={2}>Active alerts</Heading>
        <Token label={String(summary?.active_alerts_count ?? alerts.length)} color={alerts.length ? 'orange' : 'green'} icon={<Icon icon={alerts.length ? 'warning' : 'success'} />} />
      </HStack>
      {alerts.length === 0 && !loading ? <Card padding={4}><Text>No active alerts. The system has no current remediation items.</Text></Card> : null}
      <VStack gap={3}>
        {alerts.map(alert => <AlertCard key={alert.id} alert={alert} />)}
      </VStack>
      {summary ? <Text>Updated {new Date(summary.updated_at).toLocaleTimeString()} · {summary.unmetered_request_count ?? 0} unmetered requests</Text> : null}
    </VStack>
  );
}

function AlertCard({alert}: {alert: Alert}) {
  const isPage = alert.severity === 'PAGE';
  const canNavigate = alert.remediation_route.startsWith('/') && !alert.remediation_route.startsWith('//');
  return (
    <Card padding={4}>
      <HStack gap={3} wrap="wrap" vAlign="center">
        <Token label={isPage ? 'Page' : 'Ticket'} color={isPage ? 'red' : 'yellow'} icon={<Icon icon={isPage ? 'error' : 'warning'} />} />
        <VStack gap={1}>
          <Heading level={3}>{alert.summary}</Heading>
          <Text>{alert.alert_code} · {new Date(alert.triggered_at).toLocaleString()}</Text>
        </VStack>
        {canNavigate ? <Button label="Open remediation" variant="secondary" onClick={() => window.location.assign(alert.remediation_route)} /> : null}
      </HStack>
    </Card>
  );
}

function formatMicroUSD(value: number): string {
  return `$${(value / 1_000_000).toFixed(2)}`;
}
