import {useCallback, useEffect, useMemo, useState, type FormEvent} from 'react';
import {Download, Upload} from 'lucide-react';
import {
  approveExtension,
  getExtensionBlastRadius,
  getExtensionLockfile,
  inspectExtensionPermissions,
  listExtensionInstallations,
  listMarketplaceCatalog,
  rollbackExtension,
  stageExtensionInstall,
  triggerExtensionKillSwitch,
  uninstallExtension,
  upgradeExtension,
} from '../../api/catalogClient';
import type {components} from '../../api/generated';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {ConfirmDialog, Drawer, Modal} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
  CodeBlock,
  KeyValueList,
  RowActionsMenu,
  SectionHeader,
  SelectField,
  StatusPill,
  Tabs,
  TextField,
  formatOperationalState,
  stateTone,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type Installation = components['schemas']['ExtensionInstallation'];
type PermissionDiff = components['schemas']['ExtensionPermissionDiff'];
type CatalogEntry = {
  id: string;
  name: string;
  package_id: string;
  version: string;
  origin_trust_tier: 'official' | 'verified' | 'unverified_external';
  enterprise_signed: boolean;
  execution_tier: PermissionDiff['execution_tier'];
  capabilities: string[];
  description: string;
  source_uri: string;
  sha256: string;
};

const canOperate = (roles: Role[]) => roles.includes('OPERATOR');
const canApprove = (roles: Role[]) => roles.some(role => ['OPERATOR', 'ORG_OWNER', 'SECURITY_REVIEWER'].includes(role));

export function ExtensionsPage({pathname, roles}: {pathname: string; roles: Role[]}) {
  const {navigate} = useRouter();
  const activeTab = pathname.startsWith('/extensions/marketplace')
    ? 'marketplace'
    : pathname.startsWith('/extensions/lockfile')
      ? 'lockfile'
      : 'installed';

  const [installations, setInstallations] = useState<Installation[]>([]);
  const [catalog, setCatalog] = useState<CatalogEntry[]>([]);
  const [offlineCatalog, setOfflineCatalog] = useState(false);
  const [lockfile, setLockfile] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  // Catalog filters
  const [query, setQuery] = useState('');
  const [trustFilter, setTrustFilter] = useState('ALL');
  const [capFilter, setCapFilter] = useState('ALL');
  const [sigFilter, setSigFilter] = useState('ALL');

  // Stage custom package drawer
  const [stageOpen, setStageOpen] = useState(false);
  const [stageUri, setStageUri] = useState('');
  const [stageSha, setStageSha] = useState('');

  // Upgrade drawer
  const [upgradeTarget, setUpgradeTarget] = useState<Installation | null>(null);
  const [upgradeUri, setUpgradeUri] = useState('');
  const [upgradeSha, setUpgradeSha] = useState('');

  // Permission Review Modal
  const [reviewDiff, setReviewDiff] = useState<PermissionDiff | null>(null);
  const [reviewEtag, setReviewEtag] = useState('');
  const [reviewMode, setReviewMode] = useState<'install' | 'upgrade'>('install');
  const [ackUnverified, setAckUnverified] = useState(false);
  const [ackDelta, setAckDelta] = useState(false);

  // Lifecycle Action Modal (rollback / quarantine / uninstall)
  const [actionTarget, setActionTarget] = useState<{row: Installation; kind: 'rollback' | 'quarantine' | 'uninstall'; impact: {projects: string[]; connections: string[]}} | null>(null);
  const [actionReason, setActionReason] = useState('');

  const operator = canOperate(roles);
  const approver = canApprove(roles);

  const reload = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [instRows, catRes] = await Promise.all([listExtensionInstallations(), listMarketplaceCatalog()]);
      setInstallations(instRows);
      setCatalog(catRes.data.map(toCatalogEntry).filter((item): item is CatalogEntry => item !== null));
      setOfflineCatalog(catRes.offline_cached);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void reload(); }, [reload]);

  useEffect(() => {
    if (activeTab === 'lockfile' && !lockfile) {
      getExtensionLockfile()
        .then(value => setLockfile(typeof value === 'string' ? value : JSON.stringify(value, null, 2)))
        .catch(cause => setError(errText(cause)));
    }
  }, [activeTab, lockfile]);

  const visibleCatalog = useMemo(
    () =>
      catalog.filter(
        item =>
          (trustFilter === 'ALL' || item.origin_trust_tier === trustFilter) &&
          (capFilter === 'ALL' || item.capabilities.includes(capFilter)) &&
          (sigFilter === 'ALL' || (sigFilter === 'SIGNED' ? item.enterprise_signed : !item.enterprise_signed)) &&
          `${item.name} ${item.package_id} ${item.description} ${item.capabilities.join(' ')}`
            .toLowerCase()
            .includes(query.toLowerCase()),
      ),
    [catalog, trustFilter, capFilter, sigFilter, query],
  );

  async function openReview(installationId: string, mode: 'install' | 'upgrade') {
    setError('');
    try {
      const res = await inspectExtensionPermissions(installationId);
      setReviewDiff(res.data);
      setReviewEtag(res.etag);
      setReviewMode(mode);
      setAckUnverified(false);
      setAckDelta(false);
    } catch (cause) {
      setError(errText(cause));
    }
  }

  async function handleStageCustom(event: FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    if (!/^[a-f0-9]{64}$/.test(stageSha.trim())) {
      setError('Expected SHA-256 must be a 64-character lowercase hexadecimal digest.');
      return;
    }
    setBusy(true);
    try {
      const staged = await stageExtensionInstall(stageUri.trim(), stageSha.trim());
      setStageOpen(false);
      setStageUri('');
      setStageSha('');
      await reload();
      await openReview(staged.installation_id, 'install');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleStageCatalog(entry: CatalogEntry) {
    setError('');
    setNotice('');
    if (!entry.source_uri || !/^[a-f0-9]{64}$/.test(entry.sha256)) {
      setError(`${entry.name} is missing a verified source URI or SHA-256 digest.`);
      return;
    }
    setBusy(true);
    try {
      const staged = await stageExtensionInstall(entry.source_uri, entry.sha256);
      await reload();
      await openReview(staged.installation_id, 'install');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleUpgrade(event: FormEvent) {
    event.preventDefault();
    if (!upgradeTarget) return;
    setError('');
    setNotice('');
    if (!/^[a-f0-9]{64}$/.test(upgradeSha.trim())) {
      setError('Expected SHA-256 must be a 64-character lowercase hexadecimal digest.');
      return;
    }
    setBusy(true);
    try {
      const result = await upgradeExtension(upgradeTarget.id, `"v${upgradeTarget.version}"`, upgradeUri.trim(), upgradeSha.trim());
      setUpgradeTarget(null);
      setUpgradeUri('');
      setUpgradeSha('');
      await reload();
      if ('installation_id' in result.data) {
        setReviewDiff(result.data);
        setReviewEtag(result.etag);
        setReviewMode('upgrade');
        setAckUnverified(false);
        setAckDelta(false);
        setNotice('Upgrade staged and requires permission re-consent before activation.');
      } else {
        setNotice(`Upgrade staged (${result.data.state}).`);
      }
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleApproveReview(enableImmediately: boolean) {
    if (!reviewDiff || !reviewEtag) return;
    if (reviewDiff.origin_trust_tier === 'unverified_external' && !ackUnverified) return;
    if (reviewDiff.has_escalation && !ackDelta) return;
    setBusy(true);
    setError('');
    try {
      const updated = await approveExtension(reviewDiff.installation_id, reviewEtag, enableImmediately, ackUnverified);
      setReviewDiff(null);
      await reload();
      setNotice(`Permissions approved. Package state: ${updated.state}.`);
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function prepareLifecycleAction(row: Installation, kind: 'rollback' | 'quarantine' | 'uninstall') {
    setError('');
    setActionReason('');
    let impact = {projects: [] as string[], connections: [] as string[]};
    if (kind === 'rollback' || kind === 'quarantine') {
      try {
        const blast = await getExtensionBlastRadius(row.id);
        impact = {projects: blast.projects, connections: blast.connections};
      } catch (cause) {
        setError(errText(cause));
        return;
      }
    }
    setActionTarget({row, kind, impact});
  }

  async function confirmLifecycleAction() {
    if (!actionTarget || actionReason.trim().length < 5) return;
    setBusy(true);
    setError('');
    try {
      const {row, kind} = actionTarget;
      if (kind === 'rollback') await rollbackExtension(row.id, `"v${row.version}"`, actionReason.trim());
      if (kind === 'quarantine') await triggerExtensionKillSwitch(row.id, actionReason.trim(), row.package_id);
      if (kind === 'uninstall') await uninstallExtension(row.id, `"v${row.version}"`);
      setActionTarget(null);
      await reload();
      setNotice(
        kind === 'quarantine'
          ? `${row.package_id} quarantined and active streams severed.`
          : `${row.package_id} ${kind} completed.`,
      );
    } catch (cause) {
      setActionTarget(null);
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function refreshLockfile() {
    setBusy(true);
    setError('');
    try {
      const value = await getExtensionLockfile();
      setLockfile(typeof value === 'string' ? value : JSON.stringify(value, null, 2));
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  const reviewBlocked =
    !reviewDiff ||
    !reviewEtag ||
    (reviewDiff.origin_trust_tier === 'unverified_external' && !ackUnverified) ||
    (reviewDiff.has_escalation && !ackDelta);

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Extensions</h1>
          <p className="tp-page-subtitle">Separate origin, publisher signature, and execution-tier verification with staged permission consent.</p>
        </div>
        {operator ? (
          <div className="tp-page-actions">
            <Button variant="primary" size="sm" onClick={() => setStageOpen(true)}>
              <Upload size={14} aria-hidden="true" />
              <span>Stage package</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label="Extension views"
        activeId={activeTab}
        onSelect={id => {
          if (id === 'marketplace') navigate('/extensions/marketplace');
          else if (id === 'lockfile') navigate('/extensions/lockfile');
          else navigate('/extensions/installed');
        }}
        items={[
          {id: 'installed', label: 'Installed', count: installations.length},
          {id: 'marketplace', label: 'Marketplace', count: catalog.length},
          {id: 'lockfile', label: 'Lockfile'},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'installed' ? (
        <section className="tp-section">
          <SectionHeader title="Installed packages" count={installations.length} />
          <DataTable
            ariaLabel="Installed extensions"
            loading={loading}
            rows={installations}
            rowKey={row => row.id}
            emptyTitle="No extensions installed"
            emptyDescription="Stage a package from the Marketplace or supply a digest-pinned source URI."
            columns={[
              {
                id: 'pkg',
                header: 'Package',
                primary: true,
                width: '22%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono" style={{fontWeight: 600}}>{row.package_id}</span>
                    <span className="mono tp-muted">
                      Active: {row.active_version ?? '—'}{row.candidate_version ? ` → Candidate: ${row.candidate_version}` : ''}
                    </span>
                  </div>
                ),
              },
              {id: 'state', header: 'Lifecycle', width: '16%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {id: 'origin', header: 'Origin', width: '16%', render: row => <StatusPill tone={stateTone(row.origin_trust_tier)} label={formatOperationalState(row.origin_trust_tier)} />},
              {id: 'sig', header: 'Publisher', width: '15%', render: row => <span>{row.enterprise_signed ? 'Enterprise signed' : 'Unsigned'}</span>},
              {id: 'tier', header: 'Execution tier', width: '19%', render: row => <span className="mono">{row.execution_tier}</span>},
              {
                id: 'actions',
                header: 'Actions',
                width: '12%',
                render: row => (
                  <RowActionsMenu
                    label={`Actions for ${row.package_id}`}
                    items={[
                      {id: 'permissions', label: 'Review permissions', onClick: () => void openReview(row.id, 'upgrade')},
                      ...(operator
                        ? [
                            {id: 'upgrade', label: 'Upgrade package', onClick: () => { setUpgradeTarget(row); setUpgradeUri(''); setUpgradeSha(''); }},
                            ...(row.previous_rollback_version
                              ? [{id: 'rollback', label: 'Rollback version', onClick: () => void prepareLifecycleAction(row, 'rollback')}]
                              : []),
                            {type: 'divider' as const},
                            {id: 'quarantine', label: 'Quarantine package', variant: 'destructive' as const, onClick: () => void prepareLifecycleAction(row, 'quarantine')},
                            {id: 'uninstall', label: 'Uninstall package', variant: 'destructive' as const, onClick: () => void prepareLifecycleAction(row, 'uninstall')},
                          ]
                        : []),
                    ]}
                  />
                ),
              },
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'marketplace' ? (
        <section className="tp-section">
          {offlineCatalog ? <Banner tone="info">Showing cached offline marketplace catalog.</Banner> : null}
          <div className="tp-toolbar">
            <div className="tp-toolbar-field">
              <TextField label="Search catalog" type="search" value={query} onChange={event => setQuery(event.target.value)} />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Origin trust"
                value={trustFilter}
                onChange={setTrustFilter}
                options={[
                  {value: 'ALL', label: 'All origins'},
                  {value: 'official', label: 'Official'},
                  {value: 'verified', label: 'Verified'},
                  {value: 'unverified_external', label: 'Unverified external'},
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Capability"
                value={capFilter}
                onChange={setCapFilter}
                options={[
                  {value: 'ALL', label: 'All capabilities'},
                  ...[...new Set(catalog.flatMap(item => item.capabilities))].sort().map(v => ({value: v, label: v})),
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label="Publisher signature"
                value={sigFilter}
                onChange={setSigFilter}
                options={[
                  {value: 'ALL', label: 'All publishers'},
                  {value: 'SIGNED', label: 'Enterprise signed'},
                  {value: 'UNSIGNED', label: 'Unsigned'},
                ]}
              />
            </div>
          </div>

          <DataTable
            ariaLabel="Extension marketplace catalog"
            loading={loading}
            rows={visibleCatalog}
            rowKey={row => row.id}
            emptyTitle="No matching marketplace packages"
            columns={[
              {
                id: 'name',
                header: 'Package',
                primary: true,
                width: '26%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span style={{fontWeight: 600}}>{row.name}</span>
                    <span className="mono tp-muted">{row.package_id} · v{row.version}</span>
                    <span className="tp-muted">{row.description}</span>
                  </div>
                ),
              },
              {id: 'origin', header: 'Origin', width: '15%', render: row => <StatusPill tone={stateTone(row.origin_trust_tier)} label={formatOperationalState(row.origin_trust_tier)} />},
              {id: 'sig', header: 'Signature', width: '14%', render: row => <span>{row.enterprise_signed ? 'Enterprise signed' : 'None'}</span>},
              {id: 'tier', header: 'Tier', width: '15%', render: row => <span className="mono">{row.execution_tier}</span>},
              {id: 'caps', header: 'Capabilities', width: '14%', render: row => <span className="tp-muted">{row.capabilities.join(', ') || '—'}</span>},
              {
                id: 'actions',
                header: 'Actions',
                width: '16%',
                render: row =>
                  operator ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busy || !row.source_uri || !/^[a-f0-9]{64}$/.test(row.sha256)}
                      onClick={() => void handleStageCatalog(row)}
                    >
                      Stage for review
                    </Button>
                  ) : null,
              },
            ]}
          />
        </section>
      ) : null}

      {activeTab === 'lockfile' ? (
        <section className="tp-section">
          <SectionHeader
            title="tanpopo.lock"
            actions={
              <Button variant="secondary" size="sm" loading={busy} onClick={() => void refreshLockfile()}>
                <Download size={14} aria-hidden="true" />
                <span>Refresh lockfile</span>
              </Button>
            }
          />
          <CodeBlock title="tanpopo.lock" code={lockfile || 'No lockfile content returned.'} />
        </section>
      ) : null}

      <Drawer
        open={stageOpen}
        onClose={() => setStageOpen(false)}
        title="Stage extension package"
        subtitle="Provide a pinned Git/OCI/archive URI and its exact 64-character SHA-256 digest."
      >
        <form className="tp-stack" onSubmit={handleStageCustom}>
          <TextField label="Package source URI" required value={stageUri} onChange={event => setStageUri(event.target.value)} />
          <TextField label="Expected SHA-256" required value={stageSha} onChange={event => setStageSha(event.target.value)} />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setStageOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Stage package</Button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={upgradeTarget !== null}
        onClose={() => setUpgradeTarget(null)}
        title={`Stage upgrade for ${upgradeTarget?.package_id ?? ''}`}
        subtitle="Any permission escalation triggers explicit re-consent."
      >
        <form className="tp-stack" onSubmit={handleUpgrade}>
          <TextField label="Upgrade source URI" required value={upgradeUri} onChange={event => setUpgradeUri(event.target.value)} />
          <TextField label="Expected SHA-256" required value={upgradeSha} onChange={event => setUpgradeSha(event.target.value)} />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setUpgradeTarget(null)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy}>Stage upgrade</Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={reviewDiff !== null}
        onClose={() => setReviewDiff(null)}
        size="lg"
        title="Extension permission review"
        subtitle={reviewDiff ? `${reviewDiff.package_id} · ${reviewDiff.active_version ?? 'new'} → ${reviewDiff.candidate_version}` : undefined}
        footer={
          approver ? (
            <>
              <Button variant="secondary" onClick={() => setReviewDiff(null)}>Cancel</Button>
              {reviewMode === 'install' ? (
                <Button variant="secondary" disabled={busy || reviewBlocked} onClick={() => void handleApproveReview(false)}>
                  Approve, keep disabled
                </Button>
              ) : null}
              <Button variant="primary" loading={busy} disabled={reviewBlocked} onClick={() => void handleApproveReview(true)}>
                Approve & activate
              </Button>
            </>
          ) : (
            <Button variant="secondary" onClick={() => setReviewDiff(null)}>Close</Button>
          )
        }
      >
        {reviewDiff ? (
          <>
            <KeyValueList
              items={[
                {label: 'Origin trust', value: <StatusPill tone={stateTone(reviewDiff.origin_trust_tier)} label={formatOperationalState(reviewDiff.origin_trust_tier)} />},
                {label: 'Publisher signature', value: reviewDiff.enterprise_signed ? 'Enterprise signed' : 'Unsigned'},
                {label: 'Execution tier', value: reviewDiff.execution_tier, mono: true},
              ]}
            />

            <PermissionDiffView diff={reviewDiff} />

            {reviewDiff.has_escalation ? (
              <CheckboxField
                label="I reviewed and explicitly consent to these permission escalations and resource limits"
                checked={ackDelta}
                onChange={setAckDelta}
              />
            ) : null}
            {reviewDiff.origin_trust_tier === 'unverified_external' ? (
              <CheckboxField
                label="I acknowledge this package is from an unverified external publisher"
                description="Consent does not waive DEC-004 or TP-BR-0301 compliance boundaries."
                checked={ackUnverified}
                onChange={setAckUnverified}
              />
            ) : null}
            {!approver ? <Banner tone="warn">Your role cannot approve extension permissions.</Banner> : null}
          </>
        ) : null}
      </Modal>

      <ConfirmDialog
        open={actionTarget !== null}
        onClose={() => setActionTarget(null)}
        title={`${actionTarget ? actionTarget.kind[0].toUpperCase() + actionTarget.kind.slice(1) : ''} ${actionTarget?.row.package_id ?? ''}`}
        description={
          actionTarget?.kind === 'quarantine'
            ? `Disable immediately and sever active streams. Affected projects: ${actionTarget.impact.projects.join(', ') || 'None'}. Affected connections: ${actionTarget.impact.connections.join(', ') || 'None'}.`
            : actionTarget?.kind === 'rollback'
              ? `Restore previous known-good version ${actionTarget.row.previous_rollback_version ?? ''}. Affected projects: ${actionTarget.impact.projects.join(', ') || 'None'}.`
              : 'Remove this extension or discard its staged candidate.'
        }
        confirmLabel={`Confirm ${actionTarget?.kind ?? 'action'}`}
        requireMatch={actionTarget?.kind === 'quarantine' ? actionTarget.row.package_id : undefined}
        matchLabel="Type the exact package ID to confirm quarantine"
        busy={busy || actionReason.trim().length < 5}
        onConfirm={() => void confirmLifecycleAction()}
      >
        <TextField
          label="Audit reason (minimum 5 characters)"
          required
          value={actionReason}
          onChange={event => setActionReason(event.target.value)}
        />
      </ConfirmDialog>
    </div>
  );
}

function PermissionDiffView({diff}: {diff: PermissionDiff}) {
  const active = diff.active_permissions;
  const candidate = diff.candidate_permissions;
  const delta = diff.permission_delta;
  const rows: Array<{id: string; permission: string; activeVal: string; candidateVal: string; change?: string}> = [
    ...setDiffRows('Host', active?.allowed_hosts ?? [], candidate.allowed_hosts ?? [], delta.added_hosts ?? []),
    ...setDiffRows('Secret slot', active?.secret_slots ?? [], candidate.secret_slots ?? [], delta.added_secret_slots ?? []),
    ...setDiffRows('Hook', active?.hooks ?? [], candidate.hooks ?? [], delta.added_hooks ?? []),
    {
      id: 'tier',
      permission: 'Execution tier',
      activeVal: active?.execution_tier ?? 'None',
      candidateVal: candidate.execution_tier,
      change: delta.tier_escalated ? 'Expanded' : active && active.execution_tier !== candidate.execution_tier ? 'Changed' : undefined,
    },
    boolDiffRow('private-ip', 'Private IP access', active?.allow_private_ip, candidate.allow_private_ip, delta.private_ip_escalated === true, false),
    boolDiffRow('tls', 'Require TLS', active?.require_tls, candidate.require_tls, delta.tls_downgraded === true, true),
    boolDiffRow('req-body', 'Inspect request body', active?.inspect_request_body, candidate.inspect_request_body, delta.payload_access_escalated === true && candidate.inspect_request_body === true, false),
    boolDiffRow('res-body', 'Inspect response body', active?.inspect_response_body, candidate.inspect_response_body, delta.payload_access_escalated === true && candidate.inspect_response_body === true, false),
    {
      id: 'quotas',
      permission: 'Resource limits',
      activeVal: active ? formatQuotas(active) : 'None',
      candidateVal: formatQuotas(candidate),
      change: delta.memory_increased || delta.cpu_quota_increased ? 'Expanded' : undefined,
    },
  ];

  return (
    <DataTable
      ariaLabel="Permission diff"
      rows={rows}
      rowKey={row => row.id}
      columns={[
        {id: 'perm', header: 'Permission', primary: true, width: '34%', render: row => <span className="mono">{row.permission}</span>},
        {id: 'active', header: 'Active', width: '28%', render: row => <span className="mono tp-muted">{row.activeVal}</span>},
        {
          id: 'candidate',
          header: 'Candidate',
          width: '38%',
          render: row => (
            <div className="tp-row">
              <span className="mono">{row.candidateVal}</span>
              {row.change ? <StatusPill tone={row.change === 'Removed' ? 'ok' : 'warn'} label={row.change} /> : null}
            </div>
          ),
        },
      ]}
    />
  );
}

function setDiffRows(label: string, active: string[], candidate: string[], added: string[]) {
  const cur = new Set(active);
  const next = new Set(candidate);
  const add = new Set(added);
  return [...new Set([...active, ...candidate])].sort().map(item => ({
    id: `${label}-${item}`,
    permission: `${label}: ${item}`,
    activeVal: cur.has(item) ? item : '—',
    candidateVal: next.has(item) ? item : '—',
    change: add.has(item) || (!cur.has(item) && next.has(item)) ? 'Added' : cur.has(item) && !next.has(item) ? 'Removed' : undefined,
  }));
}

function boolDiffRow(id: string, permission: string, active: boolean | undefined, candidate: boolean | undefined, escalated: boolean, reverse: boolean) {
  const activeVal = active === undefined ? 'None' : active ? 'Enabled' : 'Disabled';
  const candidateVal = candidate ? 'Enabled' : 'Disabled';
  const changed = active !== undefined && active !== candidate;
  const expanded = active === undefined ? !reverse && candidate === true : reverse ? active === true && candidate === false : active === false && candidate === true;
  return {
    id,
    permission,
    activeVal,
    candidateVal,
    change: escalated && expanded ? 'Expanded' : changed ? (expanded ? 'Expanded' : 'Reduced') : undefined,
  };
}

function formatQuotas(p: components['schemas']['ExtensionPermissionSet']) {
  const fmt = (val: number | undefined, unit: string) => (!val ? 'default' : `${val} ${unit}`);
  return `Mem ${fmt(p.max_memory_mib, 'MiB')} · Call ${fmt(p.max_cpu_ms_per_call, 'ms')} · Chunk ${fmt(p.max_stream_chunk_cpu_ms, 'ms')}`;
}

function toCatalogEntry(value: unknown): CatalogEntry | null {
  if (!value || typeof value !== 'object') return null;
  const row = value as Record<string, unknown>;
  const name = typeof row.canonical_name === 'string' ? row.canonical_name : row.name;
  if (typeof name !== 'string' || typeof row.package_id !== 'string') return null;
  const trust = row.origin_trust_tier;
  if (trust !== 'official' && trust !== 'verified' && trust !== 'unverified_external') return null;
  const tier = row.execution_tier;
  if (tier !== 'declarative' && tier !== 'sandboxed_wasm' && tier !== 'isolated_sidecar') return null;
  const rawCaps = row.capability_types ?? row.capabilities ?? row.capability_type;
  const caps = Array.isArray(rawCaps)
    ? rawCaps.filter((v): v is string => typeof v === 'string')
    : typeof rawCaps === 'string'
      ? [rawCaps]
      : [];
  return {
    id: String(row.id ?? row.package_id),
    name,
    package_id: row.package_id,
    version: String(row.version ?? '0.0.0'),
    origin_trust_tier: trust,
    enterprise_signed: row.enterprise_signed === true,
    execution_tier: tier,
    capabilities: caps,
    description: typeof row.summary === 'string' ? row.summary : typeof row.description === 'string' ? row.description : '',
    source_uri: typeof row.source_uri === 'string' ? row.source_uri : '',
    sha256: typeof row.expected_sha256 === 'string' ? row.expected_sha256 : typeof row.sha256 === 'string' ? row.sha256 : '',
  };
}

function errText(cause: unknown): string { return cause instanceof Error ? cause.message : 'Request failed.'; }
