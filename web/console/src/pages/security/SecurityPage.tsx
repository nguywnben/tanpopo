import {useCallback, useEffect, useRef, useState} from 'react';
import {CheckCircle2, ShieldAlert, ShieldCheck} from 'lucide-react';
import {formatDateAndTime, formatTime} from '../../i18n/format';
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

  // Trigger Kill Switch Drawer
  const [triggerOpen, setTriggerOpen] = useState(false);
  const [level, setLevel] = useState<TargetLevel>('PROJECT');
  const [targetId, setTargetId] = useState('');
  const [reason, setReason] = useState('');
  const [confirmSlug, setConfirmSlug] = useState('');
  const [abortStreams, setAbortStreams] = useState(true);
  const [blastRadius, setBlastRadius] = useState<{organizations: string[]; projects: string[]; virtual_keys: string[]; connections: string[]} | null>(null);
  const [confirmTriggerOpen, setConfirmTriggerOpen] = useState(false);

  // Clear Kill Switch Dialog
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
      if (gen === genRef.current) setError(errText(cause));
    } finally {
      if (gen === genRef.current) setLoading(false);
    }
  }, [authorized, actionFilter, canReviewApprovals]);

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
      if (gen === genRef.current) setError(errText(cause));
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
      setNotice(`Approval ${decision.toLowerCase()}d and recorded in the immutable audit log.`);
    } catch (cause) {
      setError(errText(cause));
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
      setError(`Blast-radius preview unavailable: ${errText(cause)}`);
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
        `Kill switch active at epoch ${result.config_epoch} (${result.affected_entities_count} entities affected, ${result.severed_streams_count} streams severed).`,
      );
    } catch (cause) {
      setError(errText(cause));
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
      setNotice('Kill switch cleared and logged.');
    } catch (cause) {
      setClearTarget(null);
      setError(errText(cause));
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
          ? `Cryptographic RFC 8785 JCS chain intact across ${res.checked_events_count} events.`
          : `Chain verification failed at sequence ${res.first_broken_seq_no ?? 'unknown'}.`,
      });
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  if (!authorized) {
    return <Banner tone="danger" title="Access restricted">Your role does not have access to the security center.</Banner>;
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Security</h1>
          <p className="tp-page-subtitle">Human-in-the-loop MCP approvals, 4-level emergency kill switches, and immutable JCS audit trail.</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="danger" size="sm" onClick={() => setTriggerOpen(true)}>
            <ShieldAlert size={14} aria-hidden="true" />
            <span>Trigger kill switch</span>
          </Button>
        </div>
      </div>

      <Tabs
        label="Security sections"
        activeId={activeTab}
        onSelect={id => {
          if (id === 'kill-switches') navigate('/security/kill-switches');
          else if (id === 'audit') navigate('/security/audit-log');
          else navigate('/security/approvals');
        }}
        items={[
          {id: 'approvals', label: 'Approval Queue', count: approvals.length},
          {id: 'kill-switches', label: 'Kill Switches', count: killSwitches.length},
          {id: 'audit', label: 'Audit Log'},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'approvals' ? (
        <section className="tp-section">
          <SectionHeader title="Pending MCP approvals" count={approvals.length} />
          {!canReviewApprovals ? (
            <Banner tone="info">Only Organization Owners, Team Admins, and Security Reviewers can decide pending approvals.</Banner>
          ) : loading && approvals.length === 0 ? (
            <p className="tp-muted" role="status">Loading pending approvals…</p>
          ) : approvals.length === 0 ? (
            <EmptyState
              title="Approval queue clear"
              description="Destructive MCP tool invocations requiring human-in-the-loop sign-off appear here."
            />
          ) : (
            <ul className="tp-list">
              {approvals.map(item => (
                <li key={item.id} className="tp-list-item" style={{flexDirection: 'column', alignItems: 'stretch'}}>
                  <div className="tp-row" style={{justifyContent: 'space-between'}}>
                    <div className="tp-row">
                      <StatusPill
                        tone={item.schema_drift_locked ? 'danger' : 'warn'}
                        label={item.schema_drift_locked ? 'Schema drift locked' : formatLabel(item.action_kind)}
                      />
                      <span className="mono" style={{fontWeight: 600}}>{item.requested_by}</span>
                    </div>
                    <span className="tp-muted num">Expires {formatDateAndTime(item.expires_at)}</span>
                  </div>

                  {item.schema_drift_locked ? (
                    <Banner tone="danger">
                      The underlying MCP tool schema drifted. Re-approve the schema in MCP Hub before approving this call.
                    </Banner>
                  ) : null}

                  <CodeBlock
                    title="Escaped canonical argument preview"
                    code={item.payload_preview_json ?? 'No argument preview stored.'}
                  />

                  <div className="tp-row" style={{justifyContent: 'space-between'}}>
                    <span className="mono tp-muted">Binding hash: {item.approval_binding_hash}</span>
                    <div className="tp-row">
                      <Button variant="danger-outline" size="sm" disabled={busy} onClick={() => void handleApprovalDecision(item, 'REJECT')}>
                        Reject
                      </Button>
                      <Button variant="primary" size="sm" disabled={busy || item.schema_drift_locked} onClick={() => void handleApprovalDecision(item, 'APPROVE')}>
                        Approve
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
          <SectionHeader
            title="Active kill switches"
            count={killSwitches.length}
          />
          <DataTable
            ariaLabel="Active kill switches"
            loading={loading}
            rows={killSwitches}
            rowKey={row => row.id}
            emptyTitle="No active kill switches"
            emptyDescription="All Virtual Keys, Projects, Connections, and Extensions are operating normally."
            columns={[
              {id: 'level', header: 'Level', primary: true, width: '18%', render: row => <StatusPill tone="danger" label={formatLabel(row.level)} />},
              {id: 'target', header: 'Target ID', width: '24%', render: row => <span className="mono">{row.target_id}</span>},
              {id: 'reason', header: 'Trigger reason', width: '32%', render: row => <span>{row.reason}</span>},
              {id: 'ver', header: 'Rev', width: '10%', render: row => <span className="mono">v{row.version}</span>},
              {
                id: 'actions',
                header: 'Actions',
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
                    Clear switch
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
            <div className="tp-toolbar-field">
              <TextField
                label="Filter by action"
                placeholder="e.g. kill_switch.triggered"
                value={actionFilter}
                onChange={event => setActionFilter(event.target.value)}
              />
            </div>
            <Button variant="secondary" loading={busy} onClick={() => void handleVerifyChain()}>
              <ShieldCheck size={14} aria-hidden="true" />
              <span>Verify hash chain</span>
            </Button>
          </div>

          {verifyBanner ? (
            <Banner tone={verifyBanner.ok ? 'ok' : 'danger'} title={verifyBanner.ok ? 'Hash chain verified' : 'Hash chain integrity failure'}>
              {verifyBanner.text}
            </Banner>
          ) : null}

          <SectionHeader title="Immutable audit events" count={events.length} />
          <DataTable
            ariaLabel="Immutable audit records"
            loading={loading}
            rows={events}
            rowKey={row => `${row.seq_no}-${row.event_hash}`}
            onRowClick={row => setInspectedEvent(row)}
            emptyTitle="No audit events match this filter"
            columns={[
              {id: 'seq', header: 'Seq', primary: true, width: '7%', render: row => <span className="mono" style={{fontWeight: 600}}>#{row.seq_no}</span>},
              {id: 'action', header: 'Action', width: '21%', render: row => <span className="mono">{row.action}</span>},
              {id: 'actor', header: 'Actor', width: '22%', render: row => <span className="mono tp-muted">{row.actor_id}</span>},
              {
                id: 'target',
                header: 'Target',
                width: '28%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="tp-muted" style={{fontSize: '0.75rem'}}>{row.target_type}</span>
                    <span className="mono">{row.target_id}</span>
                  </div>
                ),
              },
              {id: 'time', header: 'Time', width: '12%', render: row => <span className="num tp-muted">{formatTime(row.created_at)}</span>},
              {
                id: 'inspect',
                header: 'Detail',
                width: '10%',
                render: row => (
                  <Button variant="ghost" size="sm" onClick={event => { event.stopPropagation(); setInspectedEvent(row); }}>
                    Inspect
                  </Button>
                ),
              },
            ]}
          />

          {!loading && nextAuditCursor ? (
            <div>
              <Button variant="secondary" size="sm" loading={loadingMore} onClick={() => void loadMoreAudit()}>
                {loadingMore ? 'Loading more…' : 'Load older audit events'}
              </Button>
            </div>
          ) : null}
        </section>
      ) : null}

      <Drawer
        open={triggerOpen}
        onClose={() => setTriggerOpen(false)}
        title="Trigger emergency kill switch"
        subtitle="Propagates immediately across snapshots and writes an immutable audit event."
        footer={
          <>
            <Button variant="secondary" onClick={() => setTriggerOpen(false)}>Cancel</Button>
            <Button
              variant="danger"
              disabled={!targetId.trim() || reason.trim().length < 5 || (requiresSlug && !confirmSlug.trim())}
              onClick={() => setConfirmTriggerOpen(true)}
            >
              Review & trigger
            </Button>
          </>
        }
      >
        <SelectField
          label="Target level"
          value={level}
          onChange={next => {
            setLevel(next as TargetLevel);
            setBlastRadius(null);
          }}
          options={[
            {value: 'VIRTUAL_KEY', label: 'Virtual key'},
            {value: 'PROJECT', label: 'Project'},
            {value: 'PROVIDER_CONNECTION', label: 'Provider connection'},
            {value: 'EXTENSION', label: 'Extension'},
          ]}
        />
        <div className="tp-toolbar">
          <div className="tp-toolbar-field">
            <TextField
              label="Target ID"
              required
              value={targetId}
              onChange={event => {
                setTargetId(event.target.value);
                setBlastRadius(null);
              }}
            />
          </div>
          <Button variant="secondary" disabled={!targetId.trim()} onClick={() => void handlePreviewBlast()}>
            Preview blast radius
          </Button>
        </div>

        {blastRadius ? (
          <Banner tone="warn" title="Estimated blast radius">
            {blastRadius.organizations.length} orgs · {blastRadius.projects.length} projects · {blastRadius.virtual_keys.length} virtual keys · {blastRadius.connections.length} connections
          </Banner>
        ) : null}

        <TextField label="Reason (minimum 5 characters)" required value={reason} onChange={event => setReason(event.target.value)} />
        {requiresSlug ? (
          <TextField label="Project confirmation slug" required value={confirmSlug} onChange={event => setConfirmSlug(event.target.value)} />
        ) : null}
        <CheckboxField
          label="Abort active streams immediately"
          description="Severs live SSE inference and MCP streams using this entity."
          checked={abortStreams}
          onChange={setAbortStreams}
        />
      </Drawer>

      <ConfirmDialog
        open={confirmTriggerOpen}
        onClose={() => setConfirmTriggerOpen(false)}
        title="Confirm emergency kill switch"
        description={`Trigger ${level.replaceAll('_', ' ')} kill switch for "${targetId}"? ${abortStreams ? 'Active streams will be terminated.' : 'Active streams will drain.'}`}
        confirmLabel="Trigger kill switch"
        busy={busy}
        onConfirm={() => void handleConfirmTrigger()}
      />

      <ConfirmDialog
        open={clearTarget !== null}
        onClose={() => setClearTarget(null)}
        title="Clear active kill switch"
        description={`Lift the ${clearTarget?.level ?? ''} kill switch on "${clearTarget?.target_id ?? ''}"?`}
        confirmLabel="Clear kill switch"
        busy={busy || clearReason.trim().length < 5}
        onConfirm={() => void handleConfirmClear()}
      >
        <TextField
          label="Reason for clearing (minimum 5 characters)"
          required
          value={clearReason}
          onChange={event => setClearReason(event.target.value)}
        />
      </ConfirmDialog>

      <Drawer
        open={inspectedEvent !== null}
        onClose={() => setInspectedEvent(null)}
        size="lg"
        title={inspectedEvent ? `Audit event #${inspectedEvent.seq_no}` : 'Audit event'}
        subtitle={inspectedEvent?.action}
      >
        {inspectedEvent ? (
          <>
            <KeyValueList
              items={[
                {label: 'Sequence', value: `#${inspectedEvent.seq_no}`, mono: true},
                {label: 'Timestamp', value: formatDateAndTime(inspectedEvent.created_at)},
                {label: 'Actor ID', value: inspectedEvent.actor_id, mono: true},
                {label: 'Action', value: inspectedEvent.action, mono: true},
                {label: 'Target', value: `${inspectedEvent.target_type} / ${inspectedEvent.target_id}`, mono: true},
                {
                  label: 'Event SHA-256 hash',
                  value: (
                    <span className="tp-row">
                      <CheckCircle2 size={14} style={{color: 'var(--status-ok-ink)', flexShrink: 0}} aria-hidden="true" />
                      <span className="mono">{inspectedEvent.event_hash}</span>
                    </span>
                  ),
                },
              ]}
            />
            <CodeBlock title="Event metadata (plain text JSON)" code={JSON.stringify(inspectedEvent.metadata ?? {}, null, 2)} />
          </>
        ) : null}
      </Drawer>
    </div>
  );
}

function errText(cause: unknown): string { return cause instanceof Error ? cause.message : 'Request failed.'; }
