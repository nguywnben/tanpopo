import {useEffect, useRef, useState} from 'react';
import * as stylex from '@stylexjs/stylex';
import {AlertDialog} from '@astryxdesign/core/AlertDialog';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {clearKillSwitch, decideApproval, getKillSwitchBlastRadius, listAuditEvents, listKillSwitches, listPendingApprovals, triggerKillSwitch, verifyAuditHashChain} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type TargetLevel = components['schemas']['KillSwitchTriggerRequest']['target_level'];
type AuditEvent = components['schemas']['AuditEventResource'] & Record<string, unknown>;
type KillSwitch = components['schemas']['KillSwitchResource'];
type Approval = components['schemas']['ApprovalRequestResource'];
const securityRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];
const approvalRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function SecurityAuditAndKillSwitchPage({roles}: {roles: Role[]}) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [killSwitches, setKillSwitches] = useState<KillSwitch[]>([]);
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [nextAuditCursor, setNextAuditCursor] = useState<string | undefined>();
  const [actionFilter, setActionFilter] = useState('');
  const [level, setLevel] = useState<TargetLevel>('PROJECT');
  const [targetId, setTargetId] = useState('');
  const [reason, setReason] = useState('');
  const [confirmSlug, setConfirmSlug] = useState('');
  const [abortStreams, setAbortStreams] = useState(true);
  const [blastRadius, setBlastRadius] = useState<{organizations: string[]; projects: string[]; virtual_keys: string[]; connections: string[]}|null>(null);
  const [clearTarget, setClearTarget] = useState<KillSwitch|null>(null);
  const [clearReason, setClearReason] = useState('');
  const [clearDialogOpen, setClearDialogOpen] = useState(false);
  const [verifyResult, setVerifyResult] = useState('');
  const [alertOpen, setAlertOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [message, setMessage] = useState('');
  const auditGeneration = useRef(0);
  const authorized = roles.some(role => securityRoles.includes(role));
  const canTrigger = roles.some(role => ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'].includes(role));
  const canReviewApprovals = roles.some(role => approvalRoles.includes(role));
  const requiresSlugConfirmation = level === 'PROJECT';

  async function refresh() {
    const generation = ++auditGeneration.current;
    setLoading(true); setMessage('');
    setEvents([]); setNextAuditCursor(undefined); setLoadingMore(false);
    try {
      const [page, activeSwitches, pendingApprovals] = await Promise.all([listAuditEvents(actionFilter), listKillSwitches(), canReviewApprovals ? listPendingApprovals() : Promise.resolve([])]);
      if (generation === auditGeneration.current) { setEvents(page.data as AuditEvent[]); setNextAuditCursor(page.nextCursor); setKillSwitches(activeSwitches as KillSwitch[]); setApprovals(pendingApprovals as Approval[]); }
    }
    catch (error) { if (generation === auditGeneration.current) setMessage(errorMessage(error)); }
    finally { if (generation === auditGeneration.current) setLoading(false); }
  }
  useEffect(() => { if (authorized) void refresh(); }, [authorized, actionFilter]);

  async function loadMoreAuditEvents() {
    if (!nextAuditCursor || loading || loadingMore) return;
    const generation = auditGeneration.current;
    setLoadingMore(true); setMessage('');
    try {
      const page = await listAuditEvents(actionFilter, nextAuditCursor);
      if (generation !== auditGeneration.current) return;
      setEvents(current => [...current, ...page.data as AuditEvent[]]); setNextAuditCursor(page.nextCursor);
    } catch (error) { if (generation === auditGeneration.current) setMessage(errorMessage(error)); }
    finally { if (generation === auditGeneration.current) setLoadingMore(false); }
  }

  async function previewBlastRadius() {
    if (!targetId.trim()) { setBlastRadius(null); return; }
    try { setBlastRadius(await getKillSwitchBlastRadius(level as 'PROJECT' | 'VIRTUAL_KEY' | 'EXTENSION' | 'PROVIDER_CONNECTION', targetId.trim())); }
    catch (error) { setBlastRadius(null); setMessage(`Blast-radius preview unavailable: ${errorMessage(error)}`); }
  }

  async function confirmClearKillSwitch() {
    if (!clearTarget || clearReason.trim().length < 5) return;
    setClearDialogOpen(false); setMessage('');
    try { await clearKillSwitch(clearTarget, clearReason.trim()); setClearTarget(null); setClearReason(''); setMessage('Kill switch cleared and recorded in the immutable audit log.'); await refresh(); }
    catch (error) { setMessage(`Kill switch could not be cleared: ${errorMessage(error)}`); }
  }

  async function verifyChain() {
    setMessage('');
    try { const result = await verifyAuditHashChain(); setVerifyResult(result.intact ? `Integrity verified across ${result.checked_events_count} audit records.` : `Integrity failure at sequence ${result.first_broken_seq_no ?? 'unknown'}.`); }
    catch (error) { setMessage(errorMessage(error)); }
  }

  async function confirmKillSwitch() {
    setAlertOpen(false); setMessage('');
    try {
      const result = await triggerKillSwitch({target_level: level, target_id: targetId.trim(), confirm_slug: confirmSlug.trim(), abort_active_streams: abortStreams, reason: reason.trim()});
      setMessage(`Kill switch propagated at configuration epoch ${result.config_epoch}; ${result.affected_entities_count} entities affected and ${result.severed_streams_count} streams severed.`);
      setConfirmSlug(''); setReason(''); setBlastRadius(null); await refresh();
    } catch (error) { setMessage(errorMessage(error)); }
  }

  async function reviewApproval(item: Approval, decision: 'APPROVE' | 'REJECT') {
    setMessage('');
    try { await decideApproval(item, decision, `Reviewed by ${decision.toLowerCase()} action in Tanpopo Console.`); await refresh(); setMessage(`Approval ${decision.toLowerCase()}d and recorded in the audit log.`); }
    catch (error) { setMessage(errorMessage(error)); }
  }

  if (!authorized) return <Text role="alert">Your role does not have access to the security center.</Text>;
  return <VStack gap={4}>
    <Heading level={1}>Security center</Heading>
    <Text>Audit records are immutable. All event data is rendered as plain text.</Text>
    {message ? <Text role="status">{message}</Text> : null}
    <Section><VStack gap={3}><Heading level={2}>Emergency kill switch</Heading>
      {!canTrigger ? <Text role="status">Your role can inspect audit records but cannot trigger emergency controls.</Text> : <>
        <label htmlFor="kill-switch-level">Target level</label>
        <select id="kill-switch-level" value={level} onChange={event => { setLevel(event.target.value as TargetLevel); setBlastRadius(null); }}>
          <option value="VIRTUAL_KEY">Virtual key</option><option value="PROJECT">Project</option><option value="PROVIDER_CONNECTION">Provider connection</option><option value="EXTENSION">Extension</option>
        </select>
        <TextInput label="Target ID" value={targetId} onChange={value => { setTargetId(value); setBlastRadius(null); }} />
        <Button label="Preview blast radius" variant="secondary" isDisabled={!targetId.trim()} onClick={() => void previewBlastRadius()} />
        {blastRadius ? <Text role="status">Blast radius: {blastRadius.organizations.length} organizations, {blastRadius.projects.length} projects, {blastRadius.virtual_keys.length} virtual keys, {blastRadius.connections.length} connections.</Text> : null}
        <TextInput label="Reason (at least 5 characters)" value={reason} onChange={setReason} />
        {requiresSlugConfirmation ? <TextInput label="Project confirmation slug" value={confirmSlug} onChange={setConfirmSlug} /> : null}
        <CheckboxInput label="Abort active streams" value={abortStreams} onChange={setAbortStreams} />
        <Button label="Review emergency kill switch" variant="destructive" isDisabled={!targetId.trim() || reason.trim().length < 5 || (requiresSlugConfirmation && !confirmSlug.trim())} onClick={() => setAlertOpen(true)} />
      </>}
    </VStack></Section>
    <AlertDialog isOpen={alertOpen} onOpenChange={setAlertOpen} title="Confirm emergency kill switch"
      description={`Trigger ${level.replaceAll('_', ' ')} for ${targetId}. ${abortStreams ? 'Active streams will be aborted.' : 'Active streams will not be aborted.'} This action is recorded in the immutable audit log.`}
      actionLabel="Trigger kill switch" actionVariant="destructive" onAction={() => void confirmKillSwitch()} />
    <AlertDialog isOpen={clearDialogOpen} onOpenChange={setClearDialogOpen} title="Clear emergency kill switch"
      description={`Clear the active ${clearTarget?.level ?? ''} switch for ${clearTarget?.target_id ?? ''}? This action is recorded in the immutable audit log.`}
      actionLabel="Clear kill switch" actionVariant="destructive" onAction={() => void confirmClearKillSwitch()} />
    <Section><VStack gap={3}><Heading level={2}>Active kill switches</Heading>
      {clearTarget ? <><Text>Clear {clearTarget.level} for {clearTarget.target_id}</Text>
        <TextInput label="Reason for clearing (at least 5 characters)" value={clearReason} onChange={setClearReason} />
        <Button label="Review kill switch clearing" variant="destructive" isDisabled={clearReason.trim().length < 5} onClick={() => setClearDialogOpen(true)} />
        <Button label="Cancel clearing" variant="secondary" onClick={() => { setClearTarget(null); setClearReason(''); }} />
      </> : null}
      {loading ? <Text role="status">Loading active kill switches…</Text> : killSwitches.length === 0 ? <Text role="status">No active kill switches.</Text> :
        <VStack gap={0} xstyle={styles.tableViewport}><Table aria-label="Active kill switches" data={killSwitches as Array<Record<string, unknown>>} columns={[
          {key: 'level', header: 'Level'}, {key: 'target_id', header: 'Target'}, {key: 'reason', header: 'Trigger reason'},
          {key: 'version', header: 'Version'}, {key: 'actions', header: 'Actions', renderCell: row => {
            const item = killSwitches.find(candidate => candidate.id === row.id);
            return <Button label="Clear" variant="destructive" isDisabled={!canTrigger || !item} onClick={() => { if (item) { setClearTarget(item); setClearReason(''); } }} />;
          }},
        ]} /></VStack>}
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>MCP approval queue</Heading>
      {!canReviewApprovals ? <Text role="status">Only Organization Owners, Team Admins, and Security Reviewers can decide pending approvals.</Text>
        : loading ? <Text role="status">Loading pending approvals…</Text>
          : approvals.length === 0 ? <Text role="status">No pending approvals.</Text>
            : <VStack gap={2}>{approvals.map(item => <Section key={item.id}><VStack gap={2}>
              <Text>{item.action_kind} · Requested by {item.requested_by} · Expires {item.expires_at}</Text>
              <Text>Canonical argument preview (escaped plain text): {item.payload_preview_json ?? 'No argument preview was stored.'}</Text>
              <Text>Binding hash: {item.approval_binding_hash}</Text>
              {item.schema_drift_locked ? <Text role="alert">The MCP tool schema has drifted. Re-approve the observed schema before approving this call.</Text> : null}
              <HStack gap={2}><Button label="Approve request" variant="primary" isDisabled={item.schema_drift_locked} onClick={() => void reviewApproval(item, 'APPROVE')} />
                <Button label="Reject request" variant="destructive" onClick={() => void reviewApproval(item, 'REJECT')} /></HStack>
            </VStack></Section>)}</VStack>}
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>Immutable audit log</Heading>
      <HStack gap={2} wrap="wrap"><TextInput label="Filter by action" value={actionFilter} onChange={setActionFilter} /><Button label="Verify hash chain" variant="secondary" onClick={() => void verifyChain()} /></HStack>
      {verifyResult ? <Text role="status">{verifyResult}</Text> : null}
      {loading ? <Text role="status">Loading audit records…</Text> : <VStack gap={0} xstyle={styles.tableViewport}><Table aria-label="Immutable audit records" data={events as Array<Record<string, unknown>>} columns={[
        {key: 'seq_no', header: 'Sequence'}, {key: 'created_at', header: 'Timestamp'}, {key: 'actor_id', header: 'Actor'},
        {key: 'action', header: 'Action'}, {key: 'target_type', header: 'Target type'}, {key: 'target_id', header: 'Target ID'},
        {key: 'event_hash', header: 'Event hash'}, {key: 'metadata', header: 'Metadata', renderCell: row => <Text>{JSON.stringify(row.metadata ?? {})}</Text>},
      ]} /></VStack>}
      {!loading && nextAuditCursor ? <Button label={loadingMore ? 'Loading audit records…' : 'Load more audit records'} variant="secondary" isDisabled={loadingMore} onClick={() => void loadMoreAuditEvents()} /> : null}
    </VStack></Section>
  </VStack>;
}

const styles = stylex.create({tableViewport: {boxSizing: 'border-box', maxWidth: '100%', minWidth: 0, overflowX: 'auto', width: '100%'}});
function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
