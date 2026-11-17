import {useEffect, useRef, useState} from 'react';
import {AlertDialog} from '@astryxdesign/core/AlertDialog';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {getKillSwitchBlastRadius, listAuditEvents, triggerKillSwitch, verifyAuditHashChain} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type TargetLevel = components['schemas']['KillSwitchTriggerRequest']['target_level'];
type AuditEvent = components['schemas']['AuditEventResource'] & Record<string, unknown>;
const securityRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function SecurityAuditAndKillSwitchPage({roles}: {roles: Role[]}) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [nextAuditCursor, setNextAuditCursor] = useState<string | undefined>();
  const [actionFilter, setActionFilter] = useState('');
  const [level, setLevel] = useState<TargetLevel>('PROJECT');
  const [targetId, setTargetId] = useState('');
  const [reason, setReason] = useState('');
  const [confirmSlug, setConfirmSlug] = useState('');
  const [abortStreams, setAbortStreams] = useState(true);
  const [blastRadius, setBlastRadius] = useState<{organizations: string[]; projects: string[]; virtual_keys: string[]; connections: string[]}|null>(null);
  const [verifyResult, setVerifyResult] = useState('');
  const [alertOpen, setAlertOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [message, setMessage] = useState('');
  const auditGeneration = useRef(0);
  const authorized = roles.some(role => securityRoles.includes(role));
  const canTrigger = roles.some(role => ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'].includes(role));
  const requiresSlugConfirmation = level === 'PROJECT';

  async function refresh() {
    const generation = ++auditGeneration.current;
    setLoading(true); setMessage('');
    setEvents([]); setNextAuditCursor(undefined); setLoadingMore(false);
    try { const page = await listAuditEvents(actionFilter); if (generation === auditGeneration.current) { setEvents(page.data as AuditEvent[]); setNextAuditCursor(page.nextCursor); } }
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
    if (!targetId.trim() || !['EXTENSION', 'PROVIDER_CONNECTION'].includes(level)) { setBlastRadius(null); return; }
    try { setBlastRadius(await getKillSwitchBlastRadius(level as 'EXTENSION' | 'PROVIDER_CONNECTION', targetId.trim())); }
    catch (error) { setBlastRadius(null); setMessage(`Blast-radius preview unavailable: ${errorMessage(error)}`); }
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
        <Button label="Preview blast radius" variant="secondary" isDisabled={!targetId.trim() || !['EXTENSION', 'PROVIDER_CONNECTION'].includes(level)} onClick={() => void previewBlastRadius()} />
        {blastRadius ? <Text role="status">Blast radius: {blastRadius.organizations.length} organizations, {blastRadius.projects.length} projects, {blastRadius.virtual_keys.length} virtual keys, {blastRadius.connections.length} connections.</Text> : null}
        {!['EXTENSION', 'PROVIDER_CONNECTION'].includes(level) ? <Text>Blast-radius preview is not available for this target type in the Admin API.</Text> : null}
        <TextInput label="Reason (at least 5 characters)" value={reason} onChange={setReason} />
        {requiresSlugConfirmation ? <TextInput label="Project confirmation slug" value={confirmSlug} onChange={setConfirmSlug} /> : null}
        <CheckboxInput label="Abort active streams" value={abortStreams} onChange={setAbortStreams} />
        <Button label="Review emergency kill switch" variant="destructive" isDisabled={!targetId.trim() || reason.trim().length < 5 || (requiresSlugConfirmation && !confirmSlug.trim())} onClick={() => setAlertOpen(true)} />
      </>}
    </VStack></Section>
    <AlertDialog isOpen={alertOpen} onOpenChange={setAlertOpen} title="Confirm emergency kill switch"
      description={`Trigger ${level.replaceAll('_', ' ')} for ${targetId}. ${abortStreams ? 'Active streams will be aborted.' : 'Active streams will not be aborted.'} This action is recorded in the immutable audit log.`}
      actionLabel="Trigger kill switch" actionVariant="destructive" onAction={() => void confirmKillSwitch()} />
    <Section><VStack gap={3}><Heading level={2}>Immutable audit log</Heading>
      <HStack gap={2} wrap="wrap"><TextInput label="Filter by action" value={actionFilter} onChange={setActionFilter} /><Button label="Verify hash chain" variant="secondary" onClick={() => void verifyChain()} /></HStack>
      {verifyResult ? <Text role="status">{verifyResult}</Text> : null}
      {loading ? <Text role="status">Loading audit records…</Text> : <Table aria-label="Immutable audit records" data={events as Array<Record<string, unknown>>} columns={[
        {key: 'seq_no', header: 'Sequence'}, {key: 'created_at', header: 'Timestamp'}, {key: 'actor_id', header: 'Actor'},
        {key: 'action', header: 'Action'}, {key: 'target_type', header: 'Target type'}, {key: 'target_id', header: 'Target ID'},
        {key: 'event_hash', header: 'Event hash'}, {key: 'metadata', header: 'Metadata', renderCell: row => <Text>{JSON.stringify(row.metadata ?? {})}</Text>},
      ]} />}
      {!loading && nextAuditCursor ? <Button label={loadingMore ? 'Loading audit records…' : 'Load more audit records'} variant="secondary" isDisabled={loadingMore} onClick={() => void loadMoreAuditEvents()} /> : null}
    </VStack></Section>
  </VStack>;
}

function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
