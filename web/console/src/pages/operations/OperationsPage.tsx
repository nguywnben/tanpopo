import {useCallback, useEffect, useState, type FormEvent} from 'react';
import {Archive, Plus, RefreshCw, ShieldCheck} from 'lucide-react';
import {getExtensionLockfile, listSecretMetadata} from '../../api/catalogClient';
import {
  createEncryptedBackup,
  createWebhook,
  getClusterOpsStatus,
  getReadinessProbe,
  listAuditEvents,
  listWebhooks,
  runUpgradePreflight,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {MultiSelectField} from '../../ui/MultiSelectField';
import {Drawer} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CodeBlock,
  MetricStrip,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
  formatOperationalState,
  stateTone,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type AuditEvent = components['schemas']['AuditEventResource'];
type WebhookSub = components['schemas']['WebhookSubscriptionResource'];
type WebhookEvent = components['schemas']['WebhookEvents'][number];
type SecretMeta = components['schemas']['SecretMetadata'];

const webhookEventOptions: WebhookEvent[] = [
  'budget.threshold',
  'mcp.approval.pending',
  'extension.quarantined',
  'kill_switch.triggered',
];

export function OperationsPage({pathname, projectId, roles}: {pathname: string; projectId: string; roles: Role[]}) {
  const {navigate} = useRouter();
  const {t, formatNumber, formatDateTime} = useI18n();
  const activeTab = pathname.startsWith('/ops/backups')
    ? 'backups'
    : pathname.startsWith('/ops/upgrades')
      ? 'upgrades'
      : pathname.startsWith('/settings/')
        ? 'webhooks'
        : 'health';

  const [readiness, setReadiness] = useState<Awaited<ReturnType<typeof getReadinessProbe>> | null>(null);
  const [cluster, setCluster] = useState<Awaited<ReturnType<typeof getClusterOpsStatus>> | null>(null);
  const [lockfile, setLockfile] = useState<unknown>(null);
  const [backups, setBackups] = useState<AuditEvent[]>([]);
  const [webhooks, setWebhooks] = useState<WebhookSub[]>([]);
  const [hmacSecrets, setHmacSecrets] = useState<SecretMeta[]>([]);

  const [targetVersion, setTargetVersion] = useState('');
  const [preflight, setPreflight] = useState<Awaited<ReturnType<typeof runUpgradePreflight>> | null>(null);

  const [webhookOpen, setWebhookOpen] = useState(false);
  const [webhookUrl, setWebhookUrl] = useState('');
  const [webhookEvents, setWebhookEvents] = useState<WebhookEvent[]>(['kill_switch.triggered', 'mcp.approval.pending']);
  const [webhookSecretId, setWebhookSecretId] = useState('');

  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const authorized = roles.includes('OPERATOR');

  const refresh = useCallback(async () => {
    if (!authorized) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    const [readyRes, clusterRes, lockRes, backupRes, webhookRes, secretRes] = await Promise.allSettled([
      getReadinessProbe(),
      getClusterOpsStatus(),
      getExtensionLockfile(),
      listAuditEvents('ops.backup_created', undefined, 'SYSTEM'),
      listWebhooks(),
      listSecretMetadata(),
    ]);
    const failures: string[] = [];
    if (readyRes.status === 'fulfilled') setReadiness(readyRes.value);
    else failures.push(t('operations.loadReadinessFailed', {error: errText(readyRes.reason, t)}));
    if (clusterRes.status === 'fulfilled') setCluster(clusterRes.value);
    else failures.push(t('operations.loadClusterFailed', {error: errText(clusterRes.reason, t)}));
    if (lockRes.status === 'fulfilled') setLockfile(lockRes.value);
    if (backupRes.status === 'fulfilled') setBackups(backupRes.value.data);
    if (webhookRes.status === 'fulfilled') setWebhooks(webhookRes.value);
    if (secretRes.status === 'fulfilled') {
      const filtered = secretRes.value.filter(s => s.secret_kind === 'WEBHOOK_HMAC_SECRET');
      setHmacSecrets(filtered);
      setWebhookSecretId(curr => curr || filtered[0]?.credential_id || '');
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [authorized, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function handleBackup() {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const receipt = await createEncryptedBackup();
      setNotice(t('operations.backupCreated', {filename: receipt.archive_filename, sha256: receipt.sha256_hex}));
      await refresh();
    } catch (cause) {
      setError(t('operations.backupFailed', {error: errText(cause, t)}));
    } finally {
      setBusy(false);
    }
  }

  async function handlePreflight(event: FormEvent) {
    event.preventDefault();
    if (!targetVersion.trim()) return;
    setBusy(true);
    setError('');
    try {
      setPreflight(await runUpgradePreflight(targetVersion.trim()));
    } catch (cause) {
      setError(t('operations.preflightFailed', {error: errText(cause, t)}));
    } finally {
      setBusy(false);
    }
  }

  async function handleCreateWebhook(event: FormEvent) {
    event.preventDefault();
    if (!webhookUrl.trim() || !webhookSecretId || webhookEvents.length === 0) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await createWebhook({
        project_id: projectId || undefined,
        target_url: webhookUrl.trim(),
        events: webhookEvents,
        signing_credential_id: webhookSecretId,
      });
      setWebhookOpen(false);
      setWebhookUrl('');
      await refresh();
      setNotice(t('operations.webhookRegistered'));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  if (!authorized) {
    return <Banner tone="danger" title={t('operations.restrictedTitle')}>{t('operations.restrictedDesc')}</Banner>;
  }

  const nodes = cluster?.nodes ?? [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('operations.title')}</h1>
          <p className="tp-page-subtitle">{t('operations.subtitle')}</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="secondary" size="sm" onClick={() => void refresh()}>
            <RefreshCw size={14} aria-hidden="true" />
            <span>{t('common.refresh')}</span>
          </Button>
        </div>
      </div>

      <Tabs
        label={t('operations.tabsLabel')}
        activeId={activeTab}
        onSelect={id => {
          if (id === 'backups') navigate('/ops/backups');
          else if (id === 'upgrades') navigate('/ops/upgrades');
          else if (id === 'organizations') navigate('/ops/organizations');
          else if (id === 'webhooks') navigate('/settings/residency-retention');
          else navigate('/ops/health');
        }}
        items={[
          {id: 'health', label: t('operations.tabHealth')},
          {id: 'backups', label: t('operations.tabBackups'), count: backups.length},
          {id: 'upgrades', label: t('operations.tabUpgrades')},
          {id: 'organizations', label: t('orgs.tabTitle')},
          {id: 'webhooks', label: t('operations.tabWebhooks'), count: webhooks.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'health' ? (
        <div className="tp-stack">
          <MetricStrip
            items={[
              {
                label: t('operations.kpi.readiness'),
                value: readiness?.ready ? t('operations.kpi.ready') : (readiness?.status ?? t('operations.kpi.checking')),
                detail: readiness?.ready ? t('operations.kpi.servingTraffic') : t('operations.kpi.checkNodeStatus'),
              },
              {
                label: t('operations.kpi.configEpoch'),
                value: String(readiness?.configEpoch ?? cluster?.current_epoch ?? '—'),
                detail: t('operations.kpi.cowSnapshot'),
              },
              {
                label: t('operations.kpi.snapshotAge'),
                value: readiness?.snapshotAgeMs === undefined ? '—' : t('common.ms', {value: formatNumber(readiness.snapshotAgeMs)}),
                detail: t('operations.kpi.lastRefresh'),
              },
              {
                label: t('operations.kpi.storageEngine'),
                value: cluster?.storage_engine ?? '—',
                detail: t('operations.kpi.persistenceBackend'),
              },
            ]}
          />

          <section className="tp-section">
            <SectionHeader title={t('operations.clusterNodes')} count={nodes.length} />
            <DataTable
              ariaLabel={t('operations.clusterNodes')}
              loading={loading}
              rows={nodes}
              rowKey={row => row.id ?? 'unknown-node'}
              emptyTitle={t('operations.emptyNodesTitle')}
              columns={[
                {id: 'id', header: t('operations.col.nodeId'), primary: true, width: '32%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.id ?? t('common.unknown')}</span>},
                {id: 'epoch', header: t('operations.kpi.configEpoch'), width: '20%', render: row => <span className="mono">{row.config_epoch ?? '—'}</span>},
                {id: 'age', header: t('operations.kpi.snapshotAge'), width: '24%', render: row => <span className="num">{t('common.ms', {value: formatNumber(row.snapshot_age_ms ?? 0)})}</span>},
                {id: 'health', header: t('operations.col.health'), width: '24%', render: row => <StatusPill tone={stateTone(row.health ?? 'unknown')} label={formatOperationalState(row.health ?? 'unknown')} />},
              ]}
            />
          </section>
        </div>
      ) : null}

      {activeTab === 'backups' ? (
        <section className="tp-section">
          <SectionHeader
            title={t('operations.backupsTitle')}
            count={backups.length}
            actions={
              <Button variant="primary" size="sm" loading={busy} disabled={cluster?.backup_supported !== true} onClick={() => void handleBackup()}>
                <Archive size={14} aria-hidden="true" />
                <span>{t('operations.createBackup')}</span>
              </Button>
            }
          />
          {cluster?.backup_supported === false ? (
            <Banner tone="warn" title={t('operations.backupsUnavailableTitle')}>
              {cluster.backup_unavailable_reason || t('operations.backupsUnavailableFallback')}
            </Banner>
          ) : null}
          <DataTable
            ariaLabel={t('operations.backupHistoryAria')}
            loading={loading}
            rows={backups}
            rowKey={row => `${row.seq_no}-${row.target_id}`}
            emptyTitle={t('operations.emptyBackupsTitle')}
            emptyDescription={t('operations.emptyBackupsDesc')}
            columns={[
              {id: 'created', header: t('common.created'), primary: true, width: '24%', render: row => <span className="num">{formatDateTime(row.created_at)}</span>},
              {id: 'actor', header: t('operations.col.triggeredBy'), width: '20%', render: row => <span className="mono">{row.actor_id}</span>},
              {id: 'target', header: t('operations.col.backupId'), width: '22%', render: row => <span className="mono">{row.target_id}</span>},
              {id: 'meta', header: t('operations.col.receiptMeta'), width: '34%', render: row => <span className="mono tp-muted">{JSON.stringify(row.metadata ?? {})}</span>},
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'upgrades' ? (
        <div className="tp-stack">
          <form className="tp-surface tp-surface-pad tp-stack" onSubmit={handlePreflight}>
            <h2>{t('operations.preflightHeading')}</h2>
            <p className="tp-muted">{t('operations.preflightDesc')}</p>
            <div className="tp-inline-form">
              <div className="tp-inline-form-field" style={{maxWidth: '280px'}}>
                <TextField
                  label={t('operations.targetCoreVersion')}
                  fieldSize="sm"
                  placeholder={t('operations.targetVersionPlaceholder')}
                  value={targetVersion}
                  disabled={busy}
                  onChange={event => setTargetVersion(event.target.value)}
                />
              </div>
              <Button type="submit" variant="primary" size="sm" loading={busy} disabled={!targetVersion.trim()}>
                <ShieldCheck size={14} aria-hidden="true" />
                <span>{t('operations.runPreflight')}</span>
              </Button>
            </div>
            {preflight ? (
              <div className="tp-stack-sm">
                <div className="tp-row">
                  <StatusPill tone={preflight.compatible ? 'ok' : 'danger'} label={preflight.compatible ? t('operations.compatible') : t('operations.upgradeBlocked')} />
                  <span className="mono tp-muted">{t('operations.preflightState', {state: preflight.state})}</span>
                </div>
                {preflight.blocking_extensions.length > 0 ? (
                  <CodeBlock title={t('operations.blockingExtensions')} code={JSON.stringify(preflight.blocking_extensions, null, 2)} />
                ) : null}
              </div>
            ) : null}
          </form>

          <CodeBlock
            title={t('operations.currentLockPins')}
            code={lockfile === null ? t('operations.lockfileUnavailable') : typeof lockfile === 'string' ? lockfile : JSON.stringify(lockfile, null, 2)}
          />
        </div>
      ) : null}

      {activeTab === 'webhooks' ? (
        <section className="tp-section">
          <SectionHeader
            title={t('operations.webhooksTitle')}
            count={webhooks.length}
            actions={
              <Button variant="primary" size="sm" onClick={() => setWebhookOpen(true)}>
                <Plus size={14} aria-hidden="true" />
                <span>{t('operations.addWebhook')}</span>
              </Button>
            }
          />
          <DataTable
            ariaLabel={t('operations.webhooksAria')}
            loading={loading}
            rows={webhooks}
            rowKey={row => row.id}
            emptyTitle={t('operations.emptyWebhooksTitle')}
            emptyDescription={t('operations.emptyWebhooksDesc')}
            columns={[
              {id: 'origin', header: t('operations.col.receiverOrigin'), primary: true, width: '32%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.target_origin}</span>},
              {id: 'events', header: t('operations.col.subscribedEvents'), width: '40%', render: row => <span className="mono tp-muted">{row.events.join(', ')}</span>},
              {id: 'state', header: t('common.state'), width: '14%', render: row => <StatusPill tone={row.enabled ? 'ok' : 'neutral'} label={row.enabled ? formatOperationalState('ENABLED') : formatOperationalState('DISABLED')} />},
              {id: 'created', header: t('common.created'), width: '14%', render: row => <span className="num tp-muted">{formatDateTime(row.created_at_ms, {dateStyle: 'medium'})}</span>},
            ]}
          />
        </section>
      ) : null}

      <Drawer
        open={webhookOpen}
        onClose={() => setWebhookOpen(false)}
        title={t('operations.webhookDrawerTitle')}
        subtitle={t('operations.webhookDrawerSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setWebhookOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-register-webhook-form" variant="primary" loading={busy} disabled={!webhookUrl.trim() || !webhookSecretId || webhookEvents.length === 0}>
              {t('operations.registerWebhook')}
            </Button>
          </>
        }
      >
        <form id="tp-register-webhook-form" className="tp-stack" onSubmit={handleCreateWebhook}>
          <TextField label={t('operations.httpsReceiverUrl')} type="url" required placeholder="https://hooks.example.com/tanpopo" value={webhookUrl} onChange={event => setWebhookUrl(event.target.value)} />
          <SelectField
            label={t('operations.signingCredential')}
            value={webhookSecretId}
            onChange={setWebhookSecretId}
            hint={hmacSecrets.length === 0 ? t('operations.sealWebhookSecretHint') : undefined}
            options={[
              {value: '', label: t('operations.selectSigningSecret')},
              ...hmacSecrets.map(s => ({
                value: s.credential_id,
                label: `${s.name} (${s.trailing_fingerprint})`,
              })),
            ]}
          />
          <MultiSelectField
            label={t('operations.events')}
            placeholder={t('operations.selectWebhookEvents')}
            hasSelectAll
            hasClear
            value={webhookEvents}
            options={webhookEventOptions.map(ev => ({value: ev, label: ev}))}
            onChange={next => setWebhookEvents(next as WebhookEvent[])}
          />
        </form>
      </Drawer>
    </div>
  );
}

function errText(cause: unknown, t: (key: string) => string): string { return cause instanceof Error ? cause.message : t('common.requestFailed'); }
