import {useCallback, useEffect, useRef, useState} from 'react';
import {CheckCircle2, ShieldAlert, ShieldCheck} from 'lucide-react';
import {
  clearKillSwitch,
  decideApproval,
  getKillSwitchBlastRadius,
  listAuditEvents,
  listKillSwitches,
  listPendingApprovals,
  triggerKillSwitch,
  verifyAuditHashChain,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {ConfirmDialog, Drawer} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
  CodeBlock,
  EmptyState,
  KeyValueList,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
  formatLabel,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type TargetLevel = components['schemas']['KillSwitchTriggerRequest']['target_level'];
type AuditEvent = components['schemas']['AuditEventResource'];
type KillSwitch = components['schemas']['KillSwitchResource'];
type Approval = components['schemas']['ApprovalRequestResource'];

const securityRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const approvalRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function SecurityPage({pathname, roles}: {pathname: string; roles: Role[]}) {
  const {navigate} = useRouter();
  const {t, formatNumber, formatDateTime, formatTime} = useI18n();
  const activeTab = pathname.startsWith('/security/audit-log')
    ? 'audit'
    : pathname.startsWith('/security/kill-switches')
      ? 'kill-switches'
      : 'approvals';

  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [killSwitches, setKillSwitches] = useState<KillSwitch[]>([]);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [nextAuditCursor, setNextAuditCursor] = useState<string | undefined>();
  const [actionFilter, setActionFilter] = useState('');
  const [verifyBanner, setVerifyBanner] = useState<{ok: boolean; text: string} | null>(null);
  const [inspectedEvent, setInspectedEvent] = useState<AuditEvent | null>(null);

  const [triggerOpen, setTriggerOpen] = useState(false);
  const [level, setLevel] = useState<TargetLevel>('PROJECT');
  const [targetId, setTargetId] = useState('');
  const [reason, setReason] = useState('');
  const [confirmSlug, setConfirmSlug] = useState('');
  const [abortStreams, setAbortStreams] = useState(true);
  const [blastRadius, setBlastRadius] = useState<{organizations: string[]; projects: string[]; virtual_keys: string[]; connections: string[]} | null>(null);
  const [confirmTriggerOpen, setConfirmTriggerOpen] = useState(false);

  const [clearTarget, setClearTarget] = useState<KillSwitch | null>(null);
  const [clearReason, setClearReason] = useState('');

  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const genRef = useRef(0);

  const authorized = roles.some(r => securityRoles.includes(r));
  const canReviewApprovals = roles.some(r => approvalRoles.includes(r));
  const requiresSlug = level === 'PROJECT';

  const levelLabel = (lvl: string) => {
    const key = `security.level.${lvl}`;
    const val = t(key);
    return val === key ? formatLabel(lvl) : val;
  };

  const refresh = useCallback(async () => {
    if (!authorized) {
      setLoading(false);
      return;
    }
    const gen = ++genRef.current;
    setLoading(true);
    setError('');
    try {
      const [auditPage, switches, pending] = await Promise.all([
        listAuditEvents(actionFilter),
        listKillSwitches(),
        canReviewApprovals ? listPendingApprovals() : Promise.resolve([]),
      ]);
      if (gen !== genRef.current) return;
      setEvents(auditPage.data);
      setNextAuditCursor(auditPage.nextCursor);
      setKillSwitches(switches);
      setApprovals(pending);
    } catch (cause) {
      if (gen === genRef.current) setError(errText(cause, t));
    } finally {
      if (gen === genRef.current) setLoading(false);
    }
  }, [authorized, actionFilter, canReviewApprovals, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function loadMoreAudit() {
    if (!nextAuditCursor || loadingMore) return;
    const gen = genRef.current;
    setLoadingMore(true);
    try {
      const page = await listAuditEvents(actionFilter, nextAuditCursor);
      if (gen !== genRef.current) return;
      setEvents(curr => [...curr, ...page.data]);
      setNextAuditCursor(page.nextCursor);
    } catch (cause) {
      if (gen === genRef.current) setError(errText(cause, t));
    } finally {
      if (gen === genRef.current) setLoadingMore(false);
    }
  }

  async function handleApprovalDecision(item: Approval, decision: 'APPROVE' | 'REJECT') {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await decideApproval(item, decision, `Reviewed (${decision.toLowerCase()}) in Tanpopo Console.`);
      await refresh();
      setNotice(decision === 'APPROVE' ? t('security.approvalApproved') : t('security.approvalRejected'));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handlePreviewBlast() {
    if (!targetId.trim()) { setBlastRadius(null); return; }
    setError('');
    try {
      setBlastRadius(await getKillSwitchBlastRadius(level as 'PROJECT' | 'VIRTUAL_KEY' | 'EXTENSION' | 'PROVIDER_CONNECTION', targetId.trim()));
    } catch (cause) {
      setBlastRadius(null);
      setError(t('security.blastUnavailable', {error: errText(cause, t)}));
    }
  }

  async function handleConfirmTrigger() {
    setConfirmTriggerOpen(false);
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await triggerKillSwitch({
        target_level: level,
        target_id: targetId.trim(),
        confirm_slug: confirmSlug.trim(),
        abort_active_streams: abortStreams,
        reason: reason.trim(),
      });
      setTriggerOpen(false);
      setTargetId('');
      setReason('');
      setConfirmSlug('');
      setBlastRadius(null);
      await refresh();
      setNotice(
        t('security.killSwitchActivated', {
          epoch: formatNumber(result.config_epoch),
          entities: formatNumber(result.affected_entities_count),
          streams: formatNumber(result.severed_streams_count),
        }),
      );
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleConfirmClear() {
    if (!clearTarget || clearReason.trim().length < 5) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await clearKillSwitch(clearTarget, clearReason.trim());
      setClearTarget(null);
      setClearReason('');
      await refresh();
      setNotice(t('security.killSwitchCleared'));
    } catch (cause) {
      setClearTarget(null);
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleVerifyChain() {
    setBusy(true);
    setError('');
    try {
      const res = await verifyAuditHashChain();
      setVerifyBanner({
        ok: res.intact,
        text: res.intact
          ? t('security.chainIntact', {count: formatNumber(res.checked_events_count)})
          : t('security.chainBroken', {seq: res.first_broken_seq_no ?? t('common.unknown')}),
      });
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  if (!authorized) {
    return <Banner tone="danger" title={t('common.accessRestricted')}>{t('security.restrictedDesc')}</Banner>;
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('security.title')}</h1>
          <p className="tp-page-subtitle">{t('security.subtitle')}</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="danger" size="sm" onClick={() => setTriggerOpen(true)}>
            <ShieldAlert size={14} aria-hidden="true" />
            <span>{t('security.triggerKillSwitch')}</span>
          </Button>
        </div>
      </div>

      <Tabs
        label={t('security.tabsLabel')}
        activeId={activeTab}
        onSelect={id => {
          if (id === 'kill-switches') navigate('/security/kill-switches');
          else if (id === 'audit') navigate('/security/audit-log');
          else navigate('/security/approvals');
        }}
        items={[
          {id: 'approvals', label: t('security.tabApprovals'), count: approvals.length},
          {id: 'kill-switches', label: t('security.tabKillSwitches'), count: killSwitches.length},
          {id: 'audit', label: t('security.tabAudit')},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'approvals' ? (
        <section className="tp-section">
          <SectionHeader title={t('security.pendingApprovals')} count={approvals.length} />
          {!canReviewApprovals ? (
            <Banner tone="info">{t('security.approvalsRoleHint')}</Banner>
          ) : loading && approvals.length === 0 ? (
            <p className="tp-muted" role="status">{t('security.loadingApprovals')}</p>
          ) : approvals.length === 0 ? (
            <EmptyState
              title={t('security.queueClearTitle')}
              description={t('security.queueClearDesc')}
            />
          ) : (
            <ul className="tp-list">
              {approvals.map(item => (
                <li key={item.id} className="tp-list-item" style={{flexDirection: 'column', alignItems: 'stretch'}}>
                  <div className="tp-row" style={{justifyContent: 'space-between'}}>
                    <div className="tp-row">
                      <StatusPill
                        tone={item.schema_drift_locked ? 'danger' : 'warn'}
                        label={item.schema_drift_locked ? t('security.schemaDriftLocked') : formatLabel(item.action_kind)}
                      />
                      <span className="mono" style={{fontWeight: 600}}>{item.requested_by}</span>
                    </div>
                    <span className="tp-muted num">{t('security.expiresAt', {time: formatDateTime(item.expires_at)})}</span>
                  </div>

                  {item.schema_drift_locked ? (
                    <Banner tone="danger">{t('security.schemaDriftBanner')}</Banner>
                  ) : null}

                  <CodeBlock
                    title={t('security.argPreviewTitle')}
                    code={item.payload_preview_json ?? t('security.noArgPreview')}
                  />

                  <div className="tp-row" style={{justifyContent: 'space-between'}}>
                    <span className="mono tp-muted">{t('security.bindingHash', {hash: item.approval_binding_hash})}</span>
                    <div className="tp-row">
                      <Button variant="danger-outline" size="sm" disabled={busy} onClick={() => void handleApprovalDecision(item, 'REJECT')}>
                        {t('security.reject')}
                      </Button>
                      <Button variant="primary" size="sm" disabled={busy || item.schema_drift_locked} onClick={() => void handleApprovalDecision(item, 'APPROVE')}>
                        {t('security.approve')}
                      </Button>
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : null}

      {activeTab === 'kill-switches' ? (
        <section className="tp-section">
          <SectionHeader title={t('security.activeSwitches')} count={killSwitches.length} />
          <DataTable
            ariaLabel={t('security.activeSwitches')}
            loading={loading}
            rows={killSwitches}
            rowKey={row => row.id}
            emptyTitle={t('security.emptySwitchesTitle')}
            emptyDescription={t('security.emptySwitchesDesc')}
            columns={[
              {id: 'level', header: t('security.col.level'), primary: true, width: '18%', render: row => <StatusPill tone="danger" label={levelLabel(row.level)} />},
              {id: 'target', header: t('security.col.targetId'), width: '24%', render: row => <span className="mono">{row.target_id}</span>},
              {id: 'reason', header: t('security.col.triggerReason'), width: '32%', render: row => <span>{row.reason}</span>},
              {id: 'ver', header: t('security.col.rev'), width: '10%', render: row => <span className="mono">v{row.version}</span>},
              {
                id: 'actions',
                header: t('common.actions'),
                width: '16%',
                render: row => (
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => {
                      setClearTarget(row);
                      setClearReason('');
                    }}
                  >
                    {t('security.clearSwitch')}
                  </Button>
                ),
              },
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'audit' ? (
        <section className="tp-section">
          <div className="tp-toolbar">
            <div className="tp-toolbar-field tp-toolbar-field-wide">
              <TextField
                label={t('security.filterAction')}
                fieldSize="sm"
                placeholder={t('security.filterActionPlaceholder')}
                value={actionFilter}
                onChange={event => setActionFilter(event.target.value)}
              />
            </div>
            <div className="tp-toolbar-actions">
              <Button variant="secondary" size="sm" loading={busy} onClick={() => void handleVerifyChain()}>
                <ShieldCheck size={14} aria-hidden="true" />
                <span>{t('security.verifyHashChain')}</span>
              </Button>
            </div>
          </div>

          {verifyBanner ? (
            <Banner tone={verifyBanner.ok ? 'ok' : 'danger'} title={verifyBanner.ok ? t('security.chainVerifiedTitle') : t('security.chainFailedTitle')}>
              {verifyBanner.text}
            </Banner>
          ) : null}

          <SectionHeader title={t('security.immutableAuditEvents')} count={events.length} />
          <DataTable
            ariaLabel={t('security.auditAria')}
            loading={loading}
            rows={events}
            rowKey={row => `${row.seq_no}-${row.event_hash}`}
            onRowClick={row => setInspectedEvent(row)}
            emptyTitle={t('security.emptyAuditTitle')}
            columns={[
              {id: 'seq', header: t('security.col.seq'), primary: true, width: '7%', render: row => <span className="mono" style={{fontWeight: 600}}>#{row.seq_no}</span>},
              {id: 'action', header: t('security.col.action'), width: '21%', render: row => <span className="mono">{row.action}</span>},
              {id: 'actor', header: t('security.col.actor'), width: '22%', render: row => <span className="mono tp-muted">{row.actor_id}</span>},
              {
                id: 'target',
                header: t('security.col.target'),
                width: '28%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="tp-muted" style={{fontSize: '0.75rem'}}>{row.target_type}</span>
                    <span className="mono">{row.target_id}</span>
                  </div>
                ),
              },
              {id: 'time', header: t('security.col.time'), width: '12%', render: row => <span className="num tp-muted">{formatTime(row.created_at)}</span>},
              {
                id: 'inspect',
                header: t('common.detail'),
                width: '10%',
                render: row => (
                  <Button variant="ghost" size="sm" className="tp-table-link-btn" onClick={event => { event.stopPropagation(); setInspectedEvent(row); }}>
                    {t('common.inspect')}
                  </Button>
                ),
              },
            ]}
          />

          {!loading && nextAuditCursor ? (
            <div>
              <Button variant="secondary" size="sm" loading={loadingMore} onClick={() => void loadMoreAudit()}>
                {loadingMore ? t('security.loadingMore') : t('security.loadOlderAudit')}
              </Button>
            </div>
          ) : null}
        </section>
      ) : null}

      <Drawer
        open={triggerOpen}
        onClose={() => setTriggerOpen(false)}
        title={t('security.triggerDrawerTitle')}
        subtitle={t('security.triggerDrawerSubtitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setTriggerOpen(false)}>{t('common.cancel')}</Button>
            <Button
              variant="danger"
              disabled={!targetId.trim() || reason.trim().length < 5 || (requiresSlug && !confirmSlug.trim())}
              onClick={() => setConfirmTriggerOpen(true)}
            >
              {t('security.reviewAndTrigger')}
            </Button>
          </>
        }
      >
        <SelectField
          label={t('security.targetLevel')}
          value={level}
          onChange={next => {
            setLevel(next as TargetLevel);
            setBlastRadius(null);
          }}
          options={[
            {value: 'VIRTUAL_KEY', label: t('security.level.VIRTUAL_KEY')},
            {value: 'PROJECT', label: t('security.level.PROJECT')},
            {value: 'PROVIDER_CONNECTION', label: t('security.level.PROVIDER_CONNECTION')},
            {value: 'EXTENSION', label: t('security.level.EXTENSION')},
          ]}
        />
        <div className="tp-inline-form">
          <div className="tp-inline-form-field">
            <TextField
              label={t('security.col.targetId')}
              required
              value={targetId}
              onChange={event => {
                setTargetId(event.target.value);
                setBlastRadius(null);
              }}
            />
          </div>
          <Button variant="secondary" disabled={!targetId.trim()} onClick={() => void handlePreviewBlast()}>
            {t('security.previewBlast')}
          </Button>
        </div>

        {blastRadius ? (
          <Banner tone="warn" title={t('security.blastTitle')}>
            {t('security.blastSummary', {
              orgs: formatNumber(blastRadius.organizations.length),
              projects: formatNumber(blastRadius.projects.length),
              keys: formatNumber(blastRadius.virtual_keys.length),
              connections: formatNumber(blastRadius.connections.length),
            })}
          </Banner>
        ) : null}

        <TextField label={t('security.reasonLabel')} required value={reason} onChange={event => setReason(event.target.value)} />
        {requiresSlug ? (
          <TextField label={t('security.confirmSlugLabel')} required value={confirmSlug} onChange={event => setConfirmSlug(event.target.value)} />
        ) : null}
        <CheckboxField
          label={t('security.abortStreams')}
          description={t('security.abortStreamsDesc')}
          checked={abortStreams}
          onChange={setAbortStreams}
        />
      </Drawer>

      <ConfirmDialog
        open={confirmTriggerOpen}
        onClose={() => setConfirmTriggerOpen(false)}
        title={t('security.confirmTriggerTitle')}
        description={t(
          abortStreams ? 'security.confirmTriggerAbortDesc' : 'security.confirmTriggerDrainDesc',
          {level: levelLabel(level), target: targetId},
        )}
        confirmLabel={t('security.triggerKillSwitch')}
        busy={busy}
        onConfirm={() => void handleConfirmTrigger()}
      />

      <ConfirmDialog
        open={clearTarget !== null}
        onClose={() => setClearTarget(null)}
        title={t('security.clearTitle')}
        description={t('security.clearDesc', {level: clearTarget ? levelLabel(clearTarget.level) : '', target: clearTarget?.target_id ?? ''})}
        confirmLabel={t('security.clearBtn')}
        busy={busy || clearReason.trim().length < 5}
        onConfirm={() => void handleConfirmClear()}
      >
        <TextField
          label={t('security.clearReasonLabel')}
          required
          value={clearReason}
          onChange={event => setClearReason(event.target.value)}
        />
      </ConfirmDialog>

      <Drawer
        open={inspectedEvent !== null}
        onClose={() => setInspectedEvent(null)}
        size="lg"
        title={inspectedEvent ? t('security.auditEventTitle', {seq: inspectedEvent.seq_no}) : t('security.auditEventDefaultTitle')}
        subtitle={inspectedEvent?.action}
      >
        {inspectedEvent ? (
          <>
            <KeyValueList
              items={[
                {label: t('security.sequence'), value: `#${inspectedEvent.seq_no}`, mono: true},
                {label: t('security.timestamp'), value: formatDateTime(inspectedEvent.created_at)},
                {label: t('security.actorId'), value: inspectedEvent.actor_id, mono: true},
                {label: t('security.col.action'), value: inspectedEvent.action, mono: true},
                {label: t('security.col.target'), value: `${inspectedEvent.target_type} / ${inspectedEvent.target_id}`, mono: true},
                {
                  label: t('security.eventHash'),
                  value: (
                    <span className="tp-row">
                      <CheckCircle2 size={14} style={{color: 'var(--status-ok-ink)', flexShrink: 0}} aria-hidden="true" />
                      <span className="mono">{inspectedEvent.event_hash}</span>
                    </span>
                  ),
                },
              ]}
            />
            <CodeBlock title={t('security.eventMetadata')} code={JSON.stringify(inspectedEvent.metadata ?? {}, null, 2)} />
          </>
        ) : null}
      </Drawer>
    </div>
  );
}

function errText(cause: unknown, t: (key: string) => string): string { return cause instanceof Error ? cause.message : t('common.requestFailed'); }
