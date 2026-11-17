import {useEffect, useMemo, useState, type FormEvent} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {CheckboxInput} from '@astryxdesign/core/CheckboxInput';
import {Dialog} from '@astryxdesign/core/Dialog';
import {DialogHeader} from '@astryxdesign/core/Dialog';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {VStack} from '@astryxdesign/core/Stack';
import {approveExtension, inspectExtensionPermissions, listMarketplaceCatalog, stageExtensionInstall} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {StateToken, TrustToken} from '../catalog/StatusToken';

type Role = components['schemas']['RbacRole'];
type PermissionDiff = components['schemas']['ExtensionPermissionDiff'];
type CatalogEntry = {id: string; name: string; package_id: string; version: string; origin_trust_tier: 'official' | 'verified' | 'unverified_external'; enterprise_signed: boolean; execution_tier: PermissionDiff['execution_tier']; capabilities: string[]; description: string; source_uri: string; sha256: string};
const canStage = (roles: Role[]) => roles.includes('OPERATOR');
const canApprove = (roles: Role[]) => roles.some(role => ['OPERATOR', 'ORG_OWNER', 'SECURITY_REVIEWER'].includes(role));

export function ExtensionCatalogPage({roles = []}: {roles?: Role[]}) {
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [filter, setFilter] = useState('ALL');
  const [capabilityFilter, setCapabilityFilter] = useState('ALL');
  const [publisherFilter, setPublisherFilter] = useState('ALL');
  const [query, setQuery] = useState('');
  const [sourceUri, setSourceUri] = useState('');
  const [sha256, setSha256] = useState('');
  const [reviewOpen, setReviewOpen] = useState(false);
  const [review, setReview] = useState<PermissionDiff | null>(null);
  const [etag, setEtag] = useState('');
  const [acknowledged, setAcknowledged] = useState(false);
  const [deltaAcknowledged, setDeltaAcknowledged] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  useEffect(() => { listMarketplaceCatalog().then(result => setCatalog(result.data.map(toCatalogEntry).filter((item): item is CatalogEntry => item !== null))).catch(failure => setError(message(failure))); }, []);
  const visible = useMemo(() => catalog.filter(item => (filter === 'ALL' || item.origin_trust_tier === filter) &&
    (capabilityFilter === 'ALL' || item.capabilities.includes(capabilityFilter)) &&
    (publisherFilter === 'ALL' || (publisherFilter === 'SIGNED' ? item.enterprise_signed : !item.enterprise_signed)) &&
    `${item.name} ${item.package_id} ${item.description} ${item.capabilities.join(' ')}`.toLowerCase().includes(query.toLowerCase())), [catalog, filter, capabilityFilter, publisherFilter, query]);

  async function stage(event: FormEvent) {
    event.preventDefault(); setError(''); setNotice('');
    if (!/^[a-f0-9]{64}$/.test(sha256)) { setError('Enter the full 64-character lowercase SHA-256 digest.'); return; }
    try {
      const staged = await stageExtensionInstall(sourceUri, sha256);
      setSourceUri(''); setSha256(''); await inspectAndOpen(staged.installation_id);
    } catch (failure) { setError(message(failure)); }
  }

  async function inspectAndOpen(id: string) {
    try {
      const result = await inspectExtensionPermissions(id);
      setReview(result.data); setEtag(result.etag); setAcknowledged(false); setDeltaAcknowledged(false); setReviewOpen(true);
    } catch (failure) { setError(message(failure)); }
  }

  async function approve(enable: boolean) {
    if (!review || !etag || (review.origin_trust_tier === 'unverified_external' && !acknowledged) || (hasPermissionDelta(review) && !deltaAcknowledged)) return;
    setError('');
    try {
      const result = await approveExtension(review.installation_id, etag, enable, acknowledged);
      setReviewOpen(false); setReview(null); setNotice(`Permission review saved. Package state: ${result.state}.`);
    } catch (failure) { setError(message(failure)); }
  }

  return <VStack gap={4}>
    <Heading level={1}>Extension marketplace</Heading>
    <Text>Review package origin, publisher trust, and execution tier as separate signals. Installation always stages before activation.</Text>
    {error ? <Text role="alert">{error}</Text> : null}{notice ? <Text role="status">{notice}</Text> : null}
    <TextInput label="Search extensions" type="search" value={query} onChange={setQuery} />
    <label>Origin trust filter<select aria-label="Origin trust filter" value={filter} onChange={event => setFilter(event.target.value)}><option value="ALL">All origins</option><option value="official">Official</option><option value="verified">Verified</option><option value="unverified_external">Unverified external</option></select></label>
    <label>Capability type filter<select aria-label="Capability type filter" value={capabilityFilter} onChange={event => setCapabilityFilter(event.target.value)}><option value="ALL">All capabilities</option>{[...new Set(catalog.flatMap(item => item.capabilities))].sort().map(value => <option key={value} value={value}>{value}</option>)}</select></label>
    <label>Enterprise signature filter<select aria-label="Enterprise signature filter" value={publisherFilter} onChange={event => setPublisherFilter(event.target.value)}><option value="ALL">All publisher signatures</option><option value="SIGNED">Enterprise signed</option><option value="UNSIGNED">No enterprise signature</option></select></label>
    <Table data={visible} density="compact" dividers="rows" columns={[
      {key: 'name', header: 'Package'}, {key: 'package_id', header: 'Package ID'}, {key: 'version', header: 'Version'},
      {key: 'origin_trust_tier', header: 'Origin', renderCell: row => <TrustToken trust={row.origin_trust_tier as CatalogEntry['origin_trust_tier']} />},
      {key: 'publisher', header: 'Publisher trust', renderCell: row => <Text>{row.enterprise_signed ? 'Enterprise signed' : 'No enterprise signature'}</Text>},
      {key: 'capabilities', header: 'Capability types', renderCell: row => <Text>{(row.capabilities as string[]).join(', ') || 'No capabilities listed'}</Text>},
      {key: 'execution_tier', header: 'Execution tier', renderCell: row => <StateToken state={String(row.execution_tier)} />},
      {key: 'description', header: 'Description'},
    ]} />
    {canStage(roles) ? <Section><VStack gap={3}><Heading level={2}>Stage a package for review</Heading>
      <form onSubmit={stage}><VStack gap={3}>
        <TextInput label="Package source URI" value={sourceUri} onChange={setSourceUri} description="Use a marketplace reference, commit-pinned Git URL, local .tpkg path, or digest-pinned OCI URI." isRequired />
        <TextInput label="Expected SHA-256" value={sha256} onChange={setSha256} isRequired />
        <Button label="Stage package" type="submit" />
      </VStack></form>
    </VStack></Section> : <Text>Only operators can stage marketplace packages.</Text>}
    <Dialog isOpen={reviewOpen} onOpenChange={setReviewOpen} purpose="form">
      {review ? <VStack gap={3}>
        <DialogHeader title="Extension permission review" subtitle="Review every trust dimension and permission before consent." onOpenChange={setReviewOpen} />
        <Text>Package: {review.package_id} · Candidate version {review.candidate_version}</Text>
        <Section><VStack gap={3}><Heading level={2}>Origin</Heading><TrustToken trust={review.origin_trust_tier} /><Text>Package source and digest were verified during staging.</Text></VStack></Section>
        <Section><VStack gap={3}><Heading level={2}>Publisher trust</Heading><Text>{review.enterprise_signed ? 'Enterprise signed' : 'No enterprise signature present'}</Text></VStack></Section>
        <Section><VStack gap={3}><Heading level={2}>Execution tier</Heading><StateToken state={review.execution_tier} /></VStack></Section>
        <Section><VStack gap={3}><Heading level={2}>Requested permissions</Heading>
          <Text>Added hosts: {review.added_allowed_hosts.join(', ') || 'None'}</Text>
          <Text>Removed hosts: {review.removed_allowed_hosts.join(', ') || 'None'}</Text>
          <Text>Secret slots: {review.secret_slots.join(', ') || 'None'}</Text>
          <Text>Quotas: {Object.entries(review.quotas).map(([key, value]) => `${key}: ${value}`).join(', ') || 'No quota data'}</Text>
        </VStack></Section>
        {hasPermissionDelta(review) ? <CheckboxInput label="I reviewed and explicitly consent to the requested permission changes" value={deltaAcknowledged} onChange={setDeltaAcknowledged} /> : null}
        {review.origin_trust_tier === 'unverified_external' ? <><Text>Your consent does not waive DEC-004 or TP-BR-0301 compliance requirements.</Text><CheckboxInput label="I acknowledge this package is from an unverified external publisher" value={acknowledged} onChange={setAcknowledged} /></> : null}
        {!canApprove(roles) ? <Text role="alert">Your role cannot approve extension permissions.</Text> : null}
        {canApprove(roles) ? <>
          <Button label="Approve, keep disabled" variant="secondary" isDisabled={!etag || (review.origin_trust_tier === 'unverified_external' && !acknowledged) || (hasPermissionDelta(review) && !deltaAcknowledged)} onClick={() => void approve(false)} />
          <Button label="Approve & Activate" isDisabled={!etag || (review.origin_trust_tier === 'unverified_external' && !acknowledged) || (hasPermissionDelta(review) && !deltaAcknowledged)} onClick={() => void approve(true)} />
        </> : null}
      </VStack> : null}
    </Dialog>
  </VStack>;
}

function toCatalogEntry(value: unknown): CatalogEntry | null {
  if (!value || typeof value !== 'object') return null;
  const row = value as Record<string, unknown>;
  const canonicalName = typeof row.canonical_name === 'string' ? row.canonical_name : row.name;
  if (typeof canonicalName !== 'string' || typeof row.package_id !== 'string') return null;
  const trust = row.origin_trust_tier;
  if (trust !== 'official' && trust !== 'verified' && trust !== 'unverified_external') return null;
  const tier = row.execution_tier;
  if (tier !== 'declarative' && tier !== 'sandboxed_wasm' && tier !== 'isolated_sidecar') return null;
  return {id: String(row.id ?? row.package_id), name: canonicalName, package_id: row.package_id, version: String(row.version ?? 'Unknown'), origin_trust_tier: trust,
    enterprise_signed: row.enterprise_signed === true, execution_tier: tier, capabilities: catalogCapabilities(row), description: typeof row.summary === 'string' ? row.summary : typeof row.description === 'string' ? row.description : 'No description provided.',
    source_uri: typeof row.source_uri === 'string' ? row.source_uri : '', sha256: typeof row.expected_sha256 === 'string' ? row.expected_sha256 : typeof row.sha256 === 'string' ? row.sha256 : ''};
}

function catalogCapabilities(row: Record<string, unknown>) {
  const raw = row.capability_types ?? row.capabilities ?? row.capability_type;
  if (Array.isArray(raw)) return raw.filter((item): item is string => typeof item === 'string');
  if (typeof raw === 'string') return [raw];
  if (raw && typeof raw === 'object') return Object.entries(raw).filter(([, enabled]) => enabled === true).map(([name]) => name);
  return [];
}

function message(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
function hasPermissionDelta(value: PermissionDiff) { return value.added_allowed_hosts.length > 0 || value.secret_slots.length > 0 || value.execution_tier !== 'declarative'; }
