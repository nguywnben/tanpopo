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
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {Drawer} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
  CodeBlock,
  MetricStrip,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
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

  // Upgrade preflight
  const [targetVersion, setTargetVersion] = useState('');
  const [preflight, setPreflight] = useState<Awaited<ReturnType<typeof runUpgradePreflight>> | null>(null);

  // Webhook drawer
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
    else failures.push(`Readiness: ${errText(readyRes.reason)}`);
    if (clusterRes.status === 'fulfilled') setCluster(clusterRes.value);
    else failures.push(`Cluster status: ${errText(clusterRes.reason)}`);
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
  }, [authorized]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function handleBackup() {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const receipt = await createEncryptedBackup();
      setNotice(`Encrypted backup created: ${receipt.archive_filename} (SHA-256 ${receipt.sha256_hex}).`);
      await refresh();
    } catch (cause) {
      setError(`Backup failed: ${errText(cause)}`);
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
      setError(`Upgrade preflight failed: ${errText(cause)}`);
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
      setNotice('HMAC-signed webhook subscription registered.');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  if (!authorized) {
    return <Banner tone="danger" title="Operator role required">Instance operations are restricted to OPERATOR accounts.</Banner>;
  }

  const nodes = cluster?.nodes ?? [];

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Operations</h1>
          <p className="tp-page-subtitle">Readiness probes, cluster snapshot epochs, encrypted backups, upgrades, and signed webhooks.</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="secondary" size="sm" onClick={() => void refresh()}>
            <RefreshCw size={14} aria-hidden="true" />
            <span>Refresh</span>
          </Button>
        </div>
      </div>

      <Tabs
        label="Operations sections"
        activeId={activeTab}
        onSelect={id => {
          if (id === 'backups') navigate('/ops/backups');
          else if (id === 'upgrades') navigate('/ops/upgrades');
          else if (id === 'webhooks') navigate('/settings/residency-retention');
          else navigate('/ops/health');
        }}
        items={[
          {id: 'health', label: 'Health & Cluster'},
          {id: 'backups', label: 'Backups', count: backups.length},
          {id: 'upgrades', label: 'Upgrades'},
          {id: 'webhooks', label: 'Webhooks', count: webhooks.length},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'health' ? (
        <div className="tp-stack">
          <MetricStrip
            items={[
              {
                label: 'Readiness (/readyz)',
                value: readiness?.ready ? 'Ready' : (readiness?.status ?? 'Checking…'),
                detail: readiness?.ready ? 'Serving traffic' : 'Check node status',
              },
              {
                label: 'Config epoch',
                value: String(readiness?.configEpoch ?? cluster?.current_epoch ?? '—'),
                detail: 'Copy-on-write snapshot',
              },
              {
                label: 'Snapshot age',
                value: readiness?.snapshotAgeMs === undefined ? '—' : `${readiness.snapshotAgeMs} ms`,
                detail: 'Last config refresh',
              },
              {
                label: 'Storage engine',
                value: cluster?.storage_engine ?? '—',
                detail: 'Active persistence backend',
              },
            ]}
          />

          <section className="tp-section">
            <SectionHeader title="Cluster nodes" count={nodes.length} />
            <DataTable
              ariaLabel="Cluster nodes"
              loading={loading}
              rows={nodes}
              rowKey={row => row.id ?? 'unknown-node'}
              emptyTitle="No cluster nodes reported"
              columns={[
                {id: 'id', header: 'Node ID', primary: true, width: '32%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.id ?? 'unknown'}</span>},
                {id: 'epoch', header: 'Config epoch', width: '20%', render: row => <span className="mono">{row.config_epoch ?? '—'}</span>},
                {id: 'age', header: 'Snapshot age', width: '24%', render: row => <span className="num">{row.snapshot_age_ms ?? 0} ms</span>},
                {id: 'health', header: 'Health', width: '24%', render: row => <StatusPill tone={stateTone(row.health ?? 'unknown')} label={row.health ?? 'unknown'} />},
              ]}
            />
          </section>
        </div>
      ) : null}

      {activeTab === 'backups' ? (
        <section className="tp-section">
          <SectionHeader
            title="Encrypted backups (.tpop.enc)"
            count={backups.length}
            actions={
              <Button variant="primary" size="sm" loading={busy} disabled={cluster?.backup_supported !== true} onClick={() => void handleBackup()}>
                <Archive size={14} aria-hidden="true" />
                <span>Create encrypted backup</span>
              </Button>
            }
          />
          {cluster?.backup_supported === false ? (
            <Banner tone="warn" title="Encrypted backups unavailable on this host">
              {cluster.backup_unavailable_reason || 'Encrypted backups require verified volatile staging or encrypted paging storage.'}
            </Banner>
          ) : null}
          <DataTable
            ariaLabel="Backup history"
            loading={loading}
            rows={backups}
            rowKey={row => `${row.seq_no}-${row.target_id}`}
            emptyTitle="No backup receipts recorded"
            emptyDescription="Backup receipts are recorded as immutable SYSTEM audit events."
            columns={[
              {id: 'created', header: 'Created', primary: true, width: '24%', render: row => <span className="num">{new Date(row.created_at).toLocaleString()}</span>},
              {id: 'actor', header: 'Triggered by', width: '20%', render: row => <span className="mono">{row.actor_id}</span>},
              {id: 'target', header: 'Backup ID', width: '22%', render: row => <span className="mono">{row.target_id}</span>},
              {id: 'meta', header: 'Receipt metadata', width: '34%', render: row => <span className="mono tp-muted">{JSON.stringify(row.metadata ?? {})}</span>},
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'upgrades' ? (
        <div className="tp-stack">
          <form className="tp-surface tp-surface-pad tp-stack" onSubmit={handlePreflight}>
            <h2>Upgrade compatibility preflight</h2>
            <p className="tp-muted">Validate installed extension ABI compatibility before upgrading Tanpopo Core.</p>
            <div className="tp-toolbar">
              <div className="tp-toolbar-field">
                <TextField
                  label="Target Core version"
                  placeholder="e.g. 0.2.0"
                  value={targetVersion}
                  disabled={busy}
                  onChange={event => setTargetVersion(event.target.value)}
                />
              </div>
              <Button type="submit" variant="primary" loading={busy} disabled={!targetVersion.trim()}>
                <ShieldCheck size={14} aria-hidden="true" />
                <span>Run preflight</span>
              </Button>
            </div>
            {preflight ? (
              <div className="tp-stack-sm">
                <div className="tp-row">
                  <StatusPill tone={preflight.compatible ? 'ok' : 'danger'} label={preflight.compatible ? 'Compatible' : 'Upgrade blocked'} />
                  <span className="mono tp-muted">State: {preflight.state}</span>
                </div>
                {preflight.blocking_extensions.length > 0 ? (
                  <CodeBlock title="Blocking extensions" code={JSON.stringify(preflight.blocking_extensions, null, 2)} />
                ) : null}
              </div>
            ) : null}
          </form>

          <CodeBlock
            title="Current tanpopo.lock pins"
            code={lockfile === null ? 'Lockfile unavailable.' : typeof lockfile === 'string' ? lockfile : JSON.stringify(lockfile, null, 2)}
          />
        </div>
      ) : null}

      {activeTab === 'webhooks' ? (
        <section className="tp-section">
          <SectionHeader
            title="HMAC-signed webhook subscriptions"
            count={webhooks.length}
            actions={
              <Button variant="primary" size="sm" onClick={() => setWebhookOpen(true)}>
                <Plus size={14} aria-hidden="true" />
                <span>Add webhook</span>
              </Button>
            }
          />
          <DataTable
            ariaLabel="Webhook subscriptions"
            loading={loading}
            rows={webhooks}
            rowKey={row => row.id}
            emptyTitle="No webhook subscriptions"
            emptyDescription="Register a public HTTPS receiver signed with a write-only WEBHOOK_HMAC_SECRET credential."
            columns={[
              {id: 'origin', header: 'Receiver origin', primary: true, width: '32%', render: row => <span className="mono" style={{fontWeight: 600}}>{row.target_origin}</span>},
              {id: 'events', header: 'Subscribed events', width: '40%', render: row => <span className="mono tp-muted">{row.events.join(', ')}</span>},
              {id: 'state', header: 'State', width: '14%', render: row => <StatusPill tone={row.enabled ? 'ok' : 'neutral'} label={row.enabled ? 'ENABLED' : 'DISABLED'} />},
              {id: 'created', header: 'Created', width: '14%', render: row => <span className="num tp-muted">{new Date(row.created_at_ms).toLocaleDateString()}</span>},
            ]}
          />
        </section>
      ) : null}

      <Drawer
        open={webhookOpen}
        onClose={() => setWebhookOpen(false)}
        title="Register webhook subscription"
        subtitle="Full receiver URLs are sealed in the vault; only the origin is stored in metadata."
      >
        <form className="tp-stack" onSubmit={handleCreateWebhook}>
          <TextField label="HTTPS receiver URL" type="url" required placeholder="https://hooks.example.com/tanpopo" value={webhookUrl} onChange={event => setWebhookUrl(event.target.value)} />
          <SelectField
            label="Signing credential (WEBHOOK_HMAC_SECRET)"
            value={webhookSecretId}
            onChange={event => setWebhookSecretId(event.target.value)}
            hint={hmacSecrets.length === 0 ? 'Seal a WEBHOOK_HMAC_SECRET credential in Providers → Vault Secrets first.' : undefined}
          >
            <option value="">Select signing secret</option>
            {hmacSecrets.map(s => (
              <option key={s.credential_id} value={s.credential_id}>{s.name} ({s.trailing_fingerprint})</option>
            ))}
          </SelectField>
          <div className="tp-stack-sm">
            <span className="tp-label">Events</span>
            {webhookEventOptions.map(ev => (
              <CheckboxField
                key={ev}
                label={ev}
                checked={webhookEvents.includes(ev)}
                onChange={checked => setWebhookEvents(curr => (checked ? [...curr, ev] : curr.filter(item => item !== ev)))}
              />
            ))}
          </div>
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setWebhookOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!webhookUrl.trim() || !webhookSecretId || webhookEvents.length === 0}>
              Register webhook
            </Button>
          </div>
        </form>
      </Drawer>
    </div>
  );
}

function errText(cause: unknown): string { return cause instanceof Error ? cause.message : 'Request failed.'; }
