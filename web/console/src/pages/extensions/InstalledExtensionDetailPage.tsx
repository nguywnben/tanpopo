import {useEffect, useState, type FormEvent} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {AlertDialog} from '@astryxdesign/core/AlertDialog';
import {CodeBlock} from '@astryxdesign/core/CodeBlock';
import {Dialog} from '@astryxdesign/core/Dialog';
import {DialogHeader} from '@astryxdesign/core/Dialog';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {VStack} from '@astryxdesign/core/Stack';
import {approveExtension, getExtensionBlastRadius, getExtensionLockfile, inspectExtensionPermissions, listExtensionInstallations, rollbackExtension, triggerExtensionKillSwitch, uninstallExtension, upgradeExtension} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {StateToken, TrustToken} from '../catalog/StatusToken';

type Role = components['schemas']['RbacRole'];
type Installation = components['schemas']['ExtensionInstallation'] & Record<string, unknown>;
type PermissionDiff = components['schemas']['ExtensionPermissionDiff'];
const canOperate = (roles: Role[]) => roles.includes('OPERATOR');
const canApprove = (roles: Role[]) => roles.some(role => ['OPERATOR', 'ORG_OWNER', 'SECURITY_REVIEWER'].includes(role));

export function InstalledExtensionDetailPage({roles = []}: {roles?: Role[]}) {
  const [installations, setInstallations] = useState<Installation[]>([]);
  const [selected, setSelected] = useState<Installation | null>(null);
  const [diff, setDiff] = useState<PermissionDiff | null>(null);
  const [etag, setEtag] = useState('');
  const [reviewOpen, setReviewOpen] = useState(false);
  const [source, setSource] = useState('');
  const [sha, setSha] = useState('');
  const [reason, setReason] = useState('');
  const [confirmSlug, setConfirmSlug] = useState('');
  const [confirmAction, setConfirmAction] = useState<'rollback' | 'quarantine' | 'uninstall' | null>(null);
  const [alertOpen, setAlertOpen] = useState(false);
  const [impact, setImpact] = useState<{projects: string[]; connections: string[]}>({projects: [], connections: []});
  const [acknowledged, setAcknowledged] = useState(false);
  const [deltaAcknowledged, setDeltaAcknowledged] = useState(false);
  const [lockfile, setLockfile] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const operator = canOperate(roles);

  async function reload() { setInstallations(await listExtensionInstallations()); }
  useEffect(() => { reload().catch(failure => setError(message(failure))); }, []);

  async function inspect(row: Installation) {
    setError('');
    try {
      const result = await inspectExtensionPermissions(row.id);
      setSelected(row); setDiff(result.data); setEtag(result.etag); setAcknowledged(false); setDeltaAcknowledged(false); setReviewOpen(true);
    } catch (failure) { setError(message(failure)); }
  }

  async function stageUpgrade(event: FormEvent) {
    event.preventDefault();
    if (!selected) return;
    if (!/^[a-f0-9]{64}$/.test(sha)) { setError('Enter the full 64-character lowercase SHA-256 digest.'); return; }
    setError('');
    try {
      const result = await upgradeExtension(selected.id, `"v${selected.version}"`, source, sha);
      setSource(''); setSha(''); await reload();
      if ('installation_id' in result.data) { setDiff(result.data); setEtag(result.etag); setAcknowledged(false); setDeltaAcknowledged(false); setReviewOpen(true); setNotice('Upgrade needs permission re-consent before activation.'); }
      else setNotice(`Upgrade staged. Package state: ${result.data.state}.`);
    } catch (failure) { setError(message(failure)); }
  }

  async function approveReconsent() {
    if (!selected || !diff || !etag || (diff.origin_trust_tier === 'unverified_external' && !acknowledged) || (hasPermissionDelta(diff) && !deltaAcknowledged)) return;
    try {
      const updated = await approveExtension(selected.id, etag, true, acknowledged);
      setReviewOpen(false); await reload(); setNotice(`Permissions approved. Package state: ${updated.state}.`);
    } catch (failure) { setError(message(failure)); }
  }

  async function prepareAction(row: Installation, action: 'rollback' | 'quarantine' | 'uninstall') {
    setSelected(row); setReason(''); setConfirmSlug(''); setError(''); setImpact({projects: [], connections: []});
    if (action === 'rollback' || action === 'quarantine') {
      try { const result = await getExtensionBlastRadius(row.id); setImpact({projects: result.projects, connections: result.connections}); }
      catch (failure) { setError(message(failure)); return; }
    }
    setConfirmAction(action);
  }

  async function confirm() {
    if (!selected || !confirmAction || reason.trim().length < 5 || (confirmAction === 'quarantine' && confirmSlug !== selected.package_id)) return;
    setError('');
    try {
      if (confirmAction === 'rollback') await rollbackExtension(selected.id, `"v${selected.version}"`, reason);
      if (confirmAction === 'quarantine') await triggerExtensionKillSwitch(selected.id, reason, confirmSlug);
      if (confirmAction === 'uninstall') await uninstallExtension(selected.id, `"v${selected.version}"`);
      setAlertOpen(false); setConfirmAction(null); setReason(''); await reload(); setNotice(confirmAction === 'quarantine' ? 'Extension quarantined and active streams cut off.' : `${confirmAction[0].toUpperCase()}${confirmAction.slice(1)} completed.`);
    } catch (failure) { setAlertOpen(false); setError(message(failure)); }
  }

  async function exportLockfile() {
    try { const value = await getExtensionLockfile(); setLockfile(typeof value === 'string' ? value : JSON.stringify(value, null, 2)); }
    catch (failure) { setError(message(failure)); }
  }

  return <VStack gap={4}>
    <Heading level={1}>Installed extensions and lockfile</Heading>
    <Text>Inspect version state, the three trust dimensions, permission changes, and rollback availability.</Text>
    {error ? <Text role="alert">{error}</Text> : null}{notice ? <Text role="status">{notice}</Text> : null}
    <Table data={installations} density="compact" dividers="rows" columns={[
      {key: 'package_id', header: 'Package'}, {key: 'active_version', header: 'Active version'}, {key: 'candidate_version', header: 'Candidate version'},
      {key: 'state', header: 'Lifecycle', renderCell: row => <StateToken state={String(row.state)} />},
      {key: 'origin_trust_tier', header: 'Origin', renderCell: row => <TrustToken trust={row.origin_trust_tier as Installation['origin_trust_tier']} />},
      {key: 'enterprise_signed', header: 'Publisher trust', renderCell: row => <Text>{row.enterprise_signed ? 'Enterprise signed' : 'Unsigned'}</Text>},
      {key: 'execution_tier', header: 'Execution tier'},
      {key: 'actions', header: 'Actions', renderCell: row => <VStack gap={1}>
        <Button label="Review permissions" variant="secondary" onClick={() => void inspect(row as Installation)} />
        {operator ? <>
          {row.previous_rollback_version ? <Button label="Rollback" variant="secondary" onClick={() => void prepareAction(row as Installation, 'rollback')} /> : null}
          <Button label="Quarantine" variant="secondary" onClick={() => void prepareAction(row as Installation, 'quarantine')} />
          <Button label="Uninstall or discard candidate" variant="secondary" onClick={() => void prepareAction(row as Installation, 'uninstall')} />
        </> : null}
      </VStack>},
    ]} />
    <Button label="Export tanpopo.lock" variant="secondary" onClick={() => void exportLockfile()} />
    {lockfile ? <Section><VStack gap={3}><Heading level={2}>tanpopo.lock</Heading><CodeBlock code={lockfile} language="yaml" title="tanpopo.lock" hasCopyButton /></VStack></Section> : null}
    {selected && operator ? <Section><VStack gap={3}><Heading level={2}>Stage upgrade for {selected.package_id}</Heading>
      <form onSubmit={stageUpgrade}><VStack gap={3}>
        <TextInput label="Upgrade source URI" value={source} onChange={setSource} isRequired />
        <TextInput label="Expected SHA-256" value={sha} onChange={setSha} isRequired />
        <Button label="Stage upgrade" type="submit" />
      </VStack></form>
    </VStack></Section> : null}
    <Dialog isOpen={reviewOpen} onOpenChange={setReviewOpen} purpose="form">
      {diff && selected ? <VStack gap={3}>
        <DialogHeader title="Permission and version review" subtitle="Review the old and candidate permissions before approving this package." onOpenChange={setReviewOpen} />
        <Text>{diff.package_id} · {diff.active_version ?? selected.active_version ?? 'No active version'} → {diff.candidate_version}</Text>
        <Section><VStack gap={3}><Heading level={2}>Origin</Heading><TrustToken trust={diff.origin_trust_tier} /><Text>Origin trust is independent of the publisher signature and execution tier.</Text></VStack></Section>
        <Section><VStack gap={3}><Heading level={2}>Publisher trust</Heading><Text>{diff.enterprise_signed ? 'Enterprise signed' : 'No enterprise signature'}</Text></VStack></Section>
        <Section><VStack gap={3}><Heading level={2}>Execution tier</Heading><StateToken state={diff.execution_tier} /></VStack></Section>
        <Section><VStack gap={3}><Heading level={2}>Permission delta</Heading>
          <Text>Added hosts: {diff.added_allowed_hosts.join(', ') || 'None'}</Text><Text>Removed hosts: {diff.removed_allowed_hosts.join(', ') || 'None'}</Text>
          <Text>Secret slots: {diff.secret_slots.join(', ') || 'None'}</Text>
          <Text>Quotas: {Object.entries(diff.quotas).map(([key, value]) => `${key}: ${value}`).join(', ') || 'No quota data'}</Text>
        </VStack></Section>
        {hasPermissionDelta(diff) ? <CheckboxInput label="I reviewed and explicitly consent to the requested permission changes" value={deltaAcknowledged} onChange={setDeltaAcknowledged} /> : null}
        {diff.origin_trust_tier === 'unverified_external' ? <><Text>Your consent does not waive DEC-004 or TP-BR-0301 compliance requirements.</Text><CheckboxInput label="I acknowledge this unverified external publisher" value={acknowledged} onChange={setAcknowledged} /></> : null}
        {canApprove(roles) ? <Button label="Approve permissions and activate" isDisabled={!etag || (diff.origin_trust_tier === 'unverified_external' && !acknowledged) || (hasPermissionDelta(diff) && !deltaAcknowledged)} onClick={() => void approveReconsent()} /> : <Text role="alert">Your role cannot approve this permission change.</Text>}
      </VStack> : null}
    </Dialog>
    <Dialog isOpen={confirmAction !== null} onOpenChange={open => !open && setConfirmAction(null)} purpose="form">
      {selected && confirmAction ? <VStack gap={3}>
        <DialogHeader title={`Prepare ${confirmAction}`} subtitle={`This action applies to ${selected.package_id}.`} onOpenChange={open => !open && setConfirmAction(null)} />
        {confirmAction === 'rollback' ? <Text>Restore the previous known-good version {selected.previous_rollback_version}.</Text> : null}
        {confirmAction === 'quarantine' ? <Text>Disable this extension immediately and cut off its active streams.</Text> : null}
        {confirmAction === 'uninstall' ? <Text>Remove the extension or discard its staged candidate. The API will preserve it if active connections still reference it.</Text> : null}
        {(confirmAction === 'rollback' || confirmAction === 'quarantine') ? <Text>Affected projects: {impact.projects.join(', ') || 'None'}. Affected connections: {impact.connections.join(', ') || 'None'}.</Text> : null}
        {confirmAction === 'quarantine' ? <TextInput label="Type the exact package ID to confirm quarantine" value={confirmSlug} onChange={setConfirmSlug} isRequired /> : null}
        <TextInput label="Reason (at least 5 characters)" value={reason} onChange={setReason} isRequired />
        <Button label={`Review ${confirmAction} confirmation`} variant="secondary" isDisabled={reason.trim().length < 5 || (confirmAction === 'quarantine' && confirmSlug !== selected.package_id)} onClick={() => setAlertOpen(true)} />
      </VStack> : null}
    </Dialog>
    <AlertDialog isOpen={alertOpen} onOpenChange={setAlertOpen} title={`Confirm ${confirmAction ?? 'extension action'}`}
      description={`${confirmAction === 'quarantine' ? 'Quarantine' : confirmAction === 'rollback' ? 'Rollback' : 'Uninstall'} ${selected?.package_id ?? 'extension'}. Affected projects: ${impact.projects.join(', ') || 'None'}. Affected connections: ${impact.connections.join(', ') || 'None'}.`}
      actionLabel={`Confirm ${confirmAction ?? 'action'}`} actionVariant="destructive" onAction={() => void confirm()} />
  </VStack>;
}

function message(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
function hasPermissionDelta(value: PermissionDiff) { return value.added_allowed_hosts.length > 0 || value.secret_slots.length > 0 || value.execution_tier !== 'declarative'; }
