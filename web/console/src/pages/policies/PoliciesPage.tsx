import {useCallback, useEffect, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Dialog, DialogHeader} from '@astryxdesign/core/Dialog';
import {Heading} from '@astryxdesign/core/Heading';
import {NumberInput} from '@astryxdesign/core/NumberInput';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Token} from '@astryxdesign/core/Token';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {getEffectivePolicy, updatePolicy} from '../../api/managementClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type Policy = components['schemas']['PolicyDocument'];
type Trust = Policy['min_plugin_trust'];
type Conflict = {message: string; current: unknown};
type Scope = 'ORGANIZATION' | 'PROJECT' | 'ENVIRONMENT';

const trustLevels: Trust[] = ['OFFICIAL_ONLY', 'VERIFIED_OR_ENTERPRISE_SIGNED', 'VERIFIED_ONLY', 'ANY_APPROVED'];
const regions: components['schemas']['DataRegion'][] = ['EU', 'US', 'APAC', 'ON_PREM'];
const trustRank: Record<Trust, number> = {ANY_APPROVED: 0, VERIFIED_OR_ENTERPRISE_SIGNED: 1, VERIFIED_ONLY: 2, OFFICIAL_ONLY: 3};
const assuranceRank: Record<Policy['min_capability_assurance'], number> = {DECLARED: 0, TESTED: 1, VERIFIED: 2};
export function PoliciesPage({projectId, environmentId, roles}: {projectId: string; environmentId: string; roles: Role[]}) {
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [organizationPolicy, setOrganizationPolicy] = useState<Policy | null>(null);
  const [projectPolicy, setProjectPolicy] = useState<Policy | null>(null);
  const [etag, setEtag] = useState('');
  const [organizationEtag, setOrganizationEtag] = useState('');
  const [projectEtag, setProjectEtag] = useState('');
  const [environmentEtag, setEnvironmentEtag] = useState('');
  const [editingScope, setEditingScope] = useState<Scope>('ENVIRONMENT');
  const [draft, setDraft] = useState<Policy | null>(null);
  const [conflict, setConflict] = useState<Conflict | null>(null);
  const [hookExtensionId, setHookExtensionId] = useState('');
  const [hookFailurePosture, setHookFailurePosture] = useState<'FAIL_CLOSED' | 'FAIL_OPEN'>('FAIL_CLOSED');
  const [confirmGuardrail, setConfirmGuardrail] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const selectedPolicy = editingScope === 'ORGANIZATION' ? organizationPolicy : editingScope === 'PROJECT' ? projectPolicy : policy;
  const parentPolicy = editingScope === 'ENVIRONMENT' ? projectPolicy : editingScope === 'PROJECT' ? organizationPolicy : null;
  const canEditPolicy = selectedPolicy ? (editingScope === 'ORGANIZATION' ? roles.includes('ORG_OWNER') : roles.includes('TEAM_ADMIN') || roles.includes('ORG_OWNER')) : false;

  const refresh = useCallback(async () => {
    if (!projectId || !environmentId) { setPolicy(null); setDraft(null); setLoading(false); return; }
    setLoading(true); setError('');
    try {
      const [organization, project, result] = await Promise.all([
        getEffectivePolicy(), getEffectivePolicy(projectId), getEffectivePolicy(projectId, environmentId),
      ]);
      setOrganizationPolicy(organization.policy); setOrganizationEtag(organization.etag);
      setProjectPolicy(project.policy); setProjectEtag(project.etag);
      setPolicy(result.policy); setEnvironmentEtag(result.etag);
      const selected = editingScope === 'ORGANIZATION' ? organization : editingScope === 'PROJECT' ? project : result;
      setDraft(selected.policy); setEtag(selected.etag);
    } catch (failure) { setError(message(failure)); }
    finally { setLoading(false); }
  }, [projectId, environmentId, editingScope]);

  useEffect(() => { void refresh(); }, [refresh]);

  async function save(confirmedGuardrail = false) {
    if (!selectedPolicy || !draft || !etag || !canEditPolicy) return;
    const newBlockingHook = draft.guardrail_hooks?.some(hook => hook.failure_posture === 'FAIL_CLOSED' && !(selectedPolicy.guardrail_hooks ?? []).some(existing => existing.extension_id === hook.extension_id));
    if (newBlockingHook && !confirmedGuardrail) { setConfirmGuardrail(true); return; }
    setBusy(true); setError(''); setConflict(null);
    try {
      await updatePolicy(selectedPolicy, etag, draft);
      await refresh();
    } catch (failure) {
      setError(message(failure));
      if (isConflict(failure)) setConflict({message: message(failure), current: currentResource(failure)});
    } finally { setBusy(false); }
  }

  function useLatestPolicyRevision() {
    if (!draft || !conflict?.current || typeof conflict.current !== 'object' || !('id' in conflict.current) || !('version' in conflict.current)) return;
    const latest = conflict.current as Policy;
    if (latest.scope_tier !== editingScope || !Number.isInteger(latest.version) || latest.version < 1) return;
    if (editingScope === 'ORGANIZATION') { setOrganizationPolicy(latest); setOrganizationEtag(`"v${latest.version}"`); }
    else if (editingScope === 'PROJECT') { setProjectPolicy(latest); setProjectEtag(`"v${latest.version}"`); }
    else { setPolicy(latest); setEnvironmentEtag(`"v${latest.version}"`); }
    setEtag(`"v${latest.version}"`);
    setDraft({...draft, id: latest.id, scope_tier: latest.scope_tier, scope_id: latest.scope_id, version: latest.version});
    setConflict(null);
  }

  const rows = policy ? [
    {id: 'scope', rule: 'Active scope', value: `${policy.scope_tier} · ${policy.scope_id}`, provenance: 'Current API resource'},
    {id: 'regions', rule: 'Allowed data regions', value: (policy.allowed_data_regions ?? []).join(', ') || 'None configured', provenance: regionSource(organizationPolicy, projectPolicy, policy)},
    {id: 'models', rule: 'Allowed models', value: (policy.allowed_models ?? ['*']).join(', ') || 'None configured', provenance: modelSource(organizationPolicy, projectPolicy, policy)},
    {id: 'trust', rule: 'Minimum plugin trust', value: policy.min_plugin_trust, provenance: scalarSource(organizationPolicy, projectPolicy, policy, 'min_plugin_trust')},
    {id: 'assurance', rule: 'Minimum capability assurance', value: policy.min_capability_assurance, provenance: scalarSource(organizationPolicy, projectPolicy, policy, 'min_capability_assurance')},
    {id: 'rpm', rule: 'Requests per minute ceiling', value: policy.rpm_ceiling ?? 'Unspecified', provenance: scalarSource(organizationPolicy, projectPolicy, policy, 'rpm_ceiling')},
    {id: 'tpm', rule: 'Tokens per minute ceiling', value: policy.tpm_ceiling ?? 'Unspecified', provenance: scalarSource(organizationPolicy, projectPolicy, policy, 'tpm_ceiling')},
    {id: 'streams', rule: 'Concurrent streams ceiling', value: policy.max_concurrent_streams ?? 'Unspecified', provenance: scalarSource(organizationPolicy, projectPolicy, policy, 'max_concurrent_streams')},
    {id: 'hooks', rule: 'Guardrail hooks', value: (policy.guardrail_hooks ?? []).map(hook => `${hook.extension_id} · ${hook.failure_posture}`).join(', ') || 'None attached', provenance: hookSource(organizationPolicy, projectPolicy, policy)},
  ] : [];
  const parentModels = parentPolicy?.allowed_models;
  const modelDraftRelaxesParent = Boolean(draft && parentModels && !isSubset(draft.allowed_models ?? ['*'], parentModels));
  const regionChoices = parentPolicy?.allowed_data_regions ?? regions;
  const parentTrust = parentPolicy?.min_plugin_trust ?? 'ANY_APPROVED';
  const parentAssurance = parentPolicy?.min_capability_assurance ?? 'DECLARED';
  const parentRPM = parentPolicy?.rpm_ceiling ?? undefined;
  const parentTPM = parentPolicy?.tpm_ceiling ?? undefined;
  const parentStreams = parentPolicy?.max_concurrent_streams ?? undefined;
  const hookRelaxesParent = hookFailurePosture === 'FAIL_OPEN' && Boolean(parentPolicy?.guardrail_hooks?.some(hook => hook.extension_id === hookExtensionId.trim() && hook.failure_posture === 'FAIL_CLOSED'));

  return <VStack gap={4}>
    <Heading level={1}>Policies</Heading>
    <Text>Review the server-resolved policy for this project and environment. Child policies can only tighten inherited limits.</Text>
    {!projectId || !environmentId ? <Text role="status">Select a project and environment to inspect policy.</Text> : null}
    {error ? <Text role="alert">{error}</Text> : null}
    {loading ? <Text role="status">Loading effective policy…</Text> : null}
    {policy && draft ? <>
      <Section><VStack gap={3}><Heading level={2}>Policy hierarchy</Heading>
        <Table data={rows} idKey="id" density="compact" columns={[
          {key: 'rule', header: 'Constraint'}, {key: 'value', header: 'Effective value'}, {key: 'provenance', header: 'Source'},
        ]} />
        <Text>Organization, project, and environment documents are loaded separately. Effective values show the combined restrictions, with the source column identifying the tier that sets each value.</Text>
      </VStack></Section>
      <Section><VStack gap={3}><Heading level={2}>Manage a policy scope</Heading>
        <label>Scope to edit<select aria-label="Scope to edit" value={editingScope} onChange={event => {
          const next = event.target.value as Scope; setEditingScope(next);
          const selected = next === 'ORGANIZATION' ? organizationPolicy : next === 'PROJECT' ? projectPolicy : policy;
          const selectedEtag = next === 'ORGANIZATION' ? organizationEtag : next === 'PROJECT' ? projectEtag : environmentEtag;
          if (selected) setDraft(selected); setEtag(selectedEtag);
        }}><option value="ORGANIZATION">Organization</option><option value="PROJECT">Project</option><option value="ENVIRONMENT">Environment</option></select></label>
        {!canEditPolicy ? <Text role="status">This policy scope is read-only for your role. Parent constraints remain locked and are enforced by the Admin API.</Text> : null}
        <Text>Scope: {selectedPolicy?.scope_tier} · {selectedPolicy?.scope_id} · revision {draft.version}</Text>
        <label>Allowed data regions<select aria-label="Allowed data regions" multiple value={draft.allowed_data_regions ?? []} disabled={!canEditPolicy || busy} onChange={event => {
          const selected = Array.from(event.currentTarget.selectedOptions, option => option.value as components['schemas']['DataRegion']);
          setDraft({...draft, allowed_data_regions: selected});
        }}>{regionChoices.map(region => <option key={region} value={region}>{region}</option>)}</select></label>
        <Text>Regions outside the parent scope allowlist stay locked. You can remove an allowed region to tighten this scope.</Text>
        <TextInput label="Allowed model slugs (comma-separated)" value={(draft.allowed_models ?? ['*']).join(', ')} onChange={value => {
          const models = value.split(',').map(item => item.trim()).filter(Boolean);
          setDraft({...draft, allowed_models: models});
        }} isDisabled={!canEditPolicy || busy} />
        {modelDraftRelaxesParent ? <Text role="alert">Model allowlists are inherited from parent scopes and cannot be widened here.</Text> : <Text>Leave blank to deny all models. Use * for all models. Child scopes may only keep or narrow an inherited allowlist.</Text>}
        <Heading level={3}>Guardrail extensions</Heading>
        {(draft.guardrail_hooks ?? []).length ? <Table data={draft.guardrail_hooks ?? []} idKey="extension_id" density="compact" columns={[{key: 'extension_id', header: 'Extension'}, {key: 'failure_posture', header: 'Failure posture'}, {key: 'action', header: 'Action', renderCell: row => <Button label={`Remove guardrail ${String(row.extension_id)}`} variant="ghost" isDisabled={!canEditPolicy || busy || parentPolicy?.guardrail_hooks?.some(hook => hook.extension_id === row.extension_id)} onClick={() => setDraft({...draft, guardrail_hooks: (draft.guardrail_hooks ?? []).filter(hook => hook.extension_id !== row.extension_id)})} />}]} /> : <Text>No guardrail extensions are attached at this scope.</Text>}
        <TextInput label="Guardrail extension ID" value={hookExtensionId} onChange={setHookExtensionId} isDisabled={!canEditPolicy || busy} />
        <label>Failure posture<select aria-label="Guardrail failure posture" value={hookFailurePosture} onChange={event => setHookFailurePosture(event.target.value as typeof hookFailurePosture)} disabled={!canEditPolicy || busy}><option value="FAIL_CLOSED">Fail closed</option><option value="FAIL_OPEN">Fail open</option></select></label>
        {hookRelaxesParent ? <Text role="alert">This parent guardrail is fail closed. A child scope cannot change it to fail open.</Text> : null}
        <Button label="Attach guardrail" variant="secondary" isDisabled={!canEditPolicy || busy || !hookExtensionId.trim() || hookRelaxesParent || (draft.guardrail_hooks ?? []).some(hook => hook.extension_id === hookExtensionId.trim())} onClick={() => {
          const hooks = [...(draft.guardrail_hooks ?? []), {extension_id: hookExtensionId.trim(), failure_posture: hookFailurePosture}];
          setDraft({...draft, guardrail_hooks: hooks}); setHookExtensionId('');
        }} />
        <label>Minimum plugin trust<select aria-label="Minimum plugin trust" value={draft.min_plugin_trust} disabled={!canEditPolicy || busy} onChange={event => setDraft({...draft, min_plugin_trust: event.target.value as Trust})}>
          {trustLevels.filter(level => trustRank[level] >= trustRank[parentTrust]).map(level => <option key={level} value={level}>{level.replaceAll('_', ' ')}</option>)}
        </select></label>
        <label>Minimum capability assurance<select aria-label="Minimum capability assurance" value={draft.min_capability_assurance} disabled={!canEditPolicy || busy} onChange={event => setDraft({...draft, min_capability_assurance: event.target.value as Policy['min_capability_assurance']})}>
          {(['VERIFIED', 'TESTED', 'DECLARED'] as const).filter(level => assuranceRank[level] >= assuranceRank[parentAssurance]).map(level => <option key={level} value={level}>{level}</option>)}
        </select></label>
        <HStack gap={3} wrap="wrap">
          <NumberInput label="Requests per minute ceiling" value={draft.rpm_ceiling ?? null} min={0} max={parentRPM} isIntegerOnly isOptional onChange={value => setDraft({...draft, rpm_ceiling: value})} isDisabled={!canEditPolicy || busy} />
          <NumberInput label="Tokens per minute ceiling" value={draft.tpm_ceiling ?? null} min={0} max={parentTPM} isIntegerOnly isOptional onChange={value => setDraft({...draft, tpm_ceiling: value})} isDisabled={!canEditPolicy || busy} />
          <NumberInput label="Concurrent streams ceiling" value={draft.max_concurrent_streams ?? null} min={0} max={parentStreams} isIntegerOnly isOptional onChange={value => setDraft({...draft, max_concurrent_streams: value})} isDisabled={!canEditPolicy || busy} />
        </HStack>
        <HStack gap={2}><Button label={busy ? 'Saving policy…' : 'Save policy'} variant="primary" isLoading={busy} isDisabled={!canEditPolicy || busy || !selectedPolicy || modelDraftRelaxesParent || JSON.stringify(selectedPolicy) === JSON.stringify(draft)} onClick={() => void save()} />
          <Button label="Discard changes" variant="secondary" isDisabled={!selectedPolicy || busy || JSON.stringify(selectedPolicy) === JSON.stringify(draft)} onClick={() => setDraft(selectedPolicy)} /></HStack>
        <Text>Failed updates preserve your draft. The server rejects any relaxation against parent policy.</Text>
      </VStack></Section>
    </> : null}
    {policy ? <HStack gap={2} wrap="wrap"><Token label="Organization" color="default" icon="info" /><Token label="Project" color="default" icon="info" /><Token label="Environment" color="blue" icon="success" /></HStack> : null}
    {conflict ? <Dialog isOpen onOpenChange={open => { if (!open) setConflict(null); }} purpose="required" width={720} padding={5}>
      <VStack gap={3}><DialogHeader title="Policy changed by another administrator" /><Text role="alert">{conflict.message}. Your draft remains available for comparison and editing.</Text>
        <HStack gap={3} wrap="wrap"><Section><VStack gap={2}><Heading level={3}>Your draft</Heading><Text>{JSON.stringify(draft, null, 2)}</Text></VStack></Section><Section><VStack gap={2}><Heading level={3}>Current server version</Heading><Text>{JSON.stringify(conflict.current ?? {}, null, 2)}</Text></VStack></Section></HStack>
        <Button label="Use latest version and keep draft" variant="secondary" onClick={useLatestPolicyRevision} isDisabled={!draft || !conflict.current || typeof conflict.current !== 'object' || !('id' in conflict.current) || !('version' in conflict.current) || (conflict.current as Policy).scope_tier !== editingScope} />
      </VStack>
    </Dialog> : null}
    {confirmGuardrail ? <Dialog isOpen onOpenChange={open => { if (!open) setConfirmGuardrail(false); }} purpose="required" width={520} padding={5}>
      <VStack gap={3}><DialogHeader title="Confirm blocking guardrail" /><Text>Attaching a FAIL_CLOSED guardrail can block requests when the extension is unavailable. Confirm the policy change before saving.</Text><HStack gap={2}><Button label="Confirm and save guardrail" variant="destructive" isDisabled={busy} onClick={() => { setConfirmGuardrail(false); void save(true); }} /><Button label="Cancel" variant="secondary" isDisabled={busy} onClick={() => setConfirmGuardrail(false)} /></HStack></VStack>
    </Dialog> : null}
  </VStack>;
}

function isConflict(error: unknown) { return Boolean(error && typeof error === 'object' && 'status' in error && error.status === 409); }
function currentResource(error: unknown): unknown { return error && typeof error === 'object' && 'currentResource' in error ? error.currentResource : null; }
function message(error: unknown) { return error instanceof Error ? error.message : 'The Admin API request failed.'; }
function isSubset(values: string[], allowed: string[]) { return allowed.includes('*') || (!values.includes('*') && values.every(value => allowed.includes(value))); }
function regionSource(org: Policy | null, project: Policy | null, current: Policy) {
  if (org && isRestricted(org.allowed_data_regions) && sameSet(org.allowed_data_regions ?? [], current.allowed_data_regions ?? [])) return 'Organization Policy · inherited floor';
  if (project && isRestricted(project.allowed_data_regions) && sameSet(project.allowed_data_regions ?? [], current.allowed_data_regions ?? [])) return 'Project Policy · inherited floor';
  return `${current.scope_tier} policy`;
}
function modelSource(org: Policy | null, project: Policy | null, current: Policy) {
  if (org && isRestricted(org.allowed_models) && sameSet(org.allowed_models ?? ['*'], current.allowed_models ?? ['*'])) return 'Organization Policy · inherited floor';
  if (project && isRestricted(project.allowed_models) && sameSet(project.allowed_models ?? ['*'], current.allowed_models ?? ['*'])) return 'Project Policy · inherited floor';
  return `${current.scope_tier} policy`;
}
function scalarSource(org: Policy | null, project: Policy | null, current: Policy, field: keyof Policy) {
  if (org && org[field] != null && org[field] === current[field]) return 'Organization Policy · inherited floor';
  if (project && project[field] != null && project[field] === current[field]) return 'Project Policy · inherited floor';
  return `${current.scope_tier} policy`;
}
function hookSource(org: Policy | null, project: Policy | null, current: Policy) {
  const present = new Set((current.guardrail_hooks ?? []).map(hook => hook.extension_id));
  const orgHooks = new Set((org?.guardrail_hooks ?? []).map(hook => hook.extension_id));
  const projectHooks = new Set((project?.guardrail_hooks ?? []).map(hook => hook.extension_id));
  if ([...present].some(id => orgHooks.has(id))) return 'Organization / project / environment policy';
  if ([...present].some(id => projectHooks.has(id))) return 'Project / environment policy';
  return `${current.scope_tier} policy`;
}
function sameSet(left: string[], right: string[]) { return left.length === right.length && left.every(value => right.includes(value)); }
function isRestricted(values: string[] | undefined) { return Boolean(values?.length && !values.includes('*')); }
