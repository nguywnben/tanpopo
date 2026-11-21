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
import {useI18n} from '../../i18n/I18nProvider';
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
  const {t} = useI18n();
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

  const [query, setQuery] = useState('');
  const [trustFilter, setTrustFilter] = useState('ALL');
  const [capFilter, setCapFilter] = useState('ALL');
  const [sigFilter, setSigFilter] = useState('ALL');

  const [stageOpen, setStageOpen] = useState(false);
  const [stageUri, setStageUri] = useState('');
  const [stageSha, setStageSha] = useState('');

  const [upgradeTarget, setUpgradeTarget] = useState<Installation | null>(null);
  const [upgradeUri, setUpgradeUri] = useState('');
  const [upgradeSha, setUpgradeSha] = useState('');

  const [reviewDiff, setReviewDiff] = useState<PermissionDiff | null>(null);
  const [reviewEtag, setReviewEtag] = useState('');
  const [reviewMode, setReviewMode] = useState<'install' | 'upgrade'>('install');
  const [ackUnverified, setAckUnverified] = useState(false);
  const [ackDelta, setAckDelta] = useState(false);

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
      setError(errText(cause, t));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => { void reload(); }, [reload]);

  useEffect(() => {
    if (activeTab === 'lockfile' && !lockfile) {
      getExtensionLockfile()
        .then(value => setLockfile(typeof value === 'string' ? value : JSON.stringify(value, null, 2)))
        .catch(cause => setError(errText(cause, t)));
    }
  }, [activeTab, lockfile, t]);

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
      setError(errText(cause, t));
    }
  }

  async function handleStageCustom(event: FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    if (!/^[a-f0-9]{64}$/.test(stageSha.trim())) {
      setError(t('extensions.errSha256'));
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
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleStageCatalog(entry: CatalogEntry) {
    setError('');
    setNotice('');
    if (!entry.source_uri || !/^[a-f0-9]{64}$/.test(entry.sha256)) {
      setError(t('extensions.errMissingDigest', {name: entry.name}));
      return;
    }
    setBusy(true);
    try {
      const staged = await stageExtensionInstall(entry.source_uri, entry.sha256);
      await reload();
      await openReview(staged.installation_id, 'install');
    } catch (cause) {
      setError(errText(cause, t));
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
      setError(t('extensions.errSha256'));
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
        setNotice(t('extensions.upgradeNeedsConsent'));
      } else {
        setNotice(t('extensions.upgradeStaged', {state: formatOperationalState(result.data.state)}));
      }
    } catch (cause) {
      setError(errText(cause, t));
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
      setNotice(t('extensions.permissionsApproved', {state: formatOperationalState(updated.state)}));
    } catch (cause) {
      setError(errText(cause, t));
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
        setError(errText(cause, t));
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
      if (kind === 'quarantine') setNotice(t('extensions.quarantinedNotice', {pkg: row.package_id}));
      else if (kind === 'rollback') setNotice(t('extensions.rollbackCompleted', {pkg: row.package_id}));
      else setNotice(t('extensions.uninstallCompleted', {pkg: row.package_id}));
    } catch (cause) {
      setActionTarget(null);
      setError(errText(cause, t));
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
      setError(errText(cause, t));
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
          <h1>{t('extensions.title')}</h1>
          <p className="tp-page-subtitle">{t('extensions.subtitle')}</p>
        </div>
        {operator ? (
          <div className="tp-page-actions">
            <Button variant="primary" size="sm" onClick={() => setStageOpen(true)}>
              <Upload size={14} aria-hidden="true" />
              <span>{t('extensions.stagePackage')}</span>
            </Button>
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('extensions.tabsLabel')}
        activeId={activeTab}
        onSelect={id => {
          if (id === 'marketplace') navigate('/extensions/marketplace');
          else if (id === 'lockfile') navigate('/extensions/lockfile');
          else navigate('/extensions/installed');
        }}
        items={[
          {id: 'installed', label: t('extensions.tabInstalled'), count: installations.length},
          {id: 'marketplace', label: t('extensions.tabMarketplace'), count: catalog.length},
          {id: 'lockfile', label: t('extensions.tabLockfile')},
        ]}
      />

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'installed' ? (
        <section className="tp-section">
          <SectionHeader title={t('extensions.installedPackages')} count={installations.length} />
          <DataTable
            ariaLabel={t('extensions.installedAria')}
            loading={loading}
            rows={installations}
            rowKey={row => row.id}
            emptyTitle={t('extensions.emptyInstalledTitle')}
            emptyDescription={t('extensions.emptyInstalledDesc')}
            columns={[
              {
                id: 'pkg',
                header: t('extensions.col.package'),
                primary: true,
                width: '22%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono" style={{fontWeight: 600}}>{row.package_id}</span>
                    <span className="mono tp-muted">
                      {t('extensions.activeVersion', {version: row.active_version ?? '—'})}
                      {row.candidate_version ? t('extensions.candidateVersion', {version: row.candidate_version}) : ''}
                    </span>
                  </div>
                ),
              },
              {id: 'state', header: t('extensions.col.lifecycle'), width: '16%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {id: 'origin', header: t('extensions.col.origin'), width: '16%', render: row => <StatusPill tone={stateTone(row.origin_trust_tier)} label={formatOperationalState(row.origin_trust_tier)} />},
              {id: 'sig', header: t('extensions.col.publisher'), width: '15%', render: row => <span>{row.enterprise_signed ? t('extensions.enterpriseSigned') : t('extensions.unsigned')}</span>},
              {id: 'tier', header: t('extensions.col.executionTier'), width: '19%', render: row => <span className="mono">{row.execution_tier}</span>},
              {
                id: 'actions',
                header: t('common.actions'),
                width: '12%',
                render: row => (
                  <RowActionsMenu
                    label={t('common.actionsFor', {name: row.package_id})}
                    items={[
                      {id: 'permissions', label: t('extensions.action.reviewPermissions'), onClick: () => void openReview(row.id, 'upgrade')},
                      ...(operator
                        ? [
                            {id: 'upgrade', label: t('extensions.action.upgrade'), onClick: () => { setUpgradeTarget(row); setUpgradeUri(''); setUpgradeSha(''); }},
                            ...(row.previous_rollback_version
                              ? [{id: 'rollback', label: t('extensions.action.rollback'), onClick: () => void prepareLifecycleAction(row, 'rollback')}]
                              : []),
                            {type: 'divider' as const},
                            {id: 'quarantine', label: t('extensions.action.quarantine'), variant: 'destructive' as const, onClick: () => void prepareLifecycleAction(row, 'quarantine')},
                            {id: 'uninstall', label: t('extensions.action.uninstall'), variant: 'destructive' as const, onClick: () => void prepareLifecycleAction(row, 'uninstall')},
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
          {offlineCatalog ? <Banner tone="info">{t('extensions.offlineCatalog')}</Banner> : null}
          <div className="tp-toolbar">
            <div className="tp-toolbar-field tp-toolbar-field-wide">
              <TextField label={t('extensions.searchCatalog')} fieldSize="sm" type="search" value={query} onChange={event => setQuery(event.target.value)} />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('extensions.originTrust')}
                size="sm"
                value={trustFilter}
                onChange={setTrustFilter}
                options={[
                  {value: 'ALL', label: t('extensions.allOrigins')},
                  {value: 'official', label: t('extensions.origin.official')},
                  {value: 'verified', label: t('extensions.origin.verified')},
                  {value: 'unverified_external', label: t('extensions.origin.unverified_external')},
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('extensions.capability')}
                size="sm"
                value={capFilter}
                onChange={setCapFilter}
                options={[
                  {value: 'ALL', label: t('extensions.allCapabilities')},
                  ...[...new Set(catalog.flatMap(item => item.capabilities))].sort().map(v => ({value: v, label: v})),
                ]}
              />
            </div>
            <div className="tp-toolbar-field">
              <SelectField
                label={t('extensions.publisherSignature')}
                size="sm"
                value={sigFilter}
                onChange={setSigFilter}
                options={[
                  {value: 'ALL', label: t('extensions.allPublishers')},
                  {value: 'SIGNED', label: t('extensions.enterpriseSigned')},
                  {value: 'UNSIGNED', label: t('extensions.unsigned')},
                ]}
              />
            </div>
          </div>

          <DataTable
            ariaLabel={t('extensions.catalogAria')}
            loading={loading}
            rows={visibleCatalog}
            rowKey={row => row.id}
            emptyTitle={t('extensions.emptyCatalogTitle')}
            columns={[
              {
                id: 'name',
                header: t('extensions.col.package'),
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
              {id: 'origin', header: t('extensions.col.origin'), width: '15%', render: row => <StatusPill tone={stateTone(row.origin_trust_tier)} label={formatOperationalState(row.origin_trust_tier)} />},
              {id: 'sig', header: t('extensions.col.signature'), width: '14%', render: row => <span>{row.enterprise_signed ? t('extensions.enterpriseSigned') : t('common.none')}</span>},
              {id: 'tier', header: t('extensions.col.tier'), width: '15%', render: row => <span className="mono">{row.execution_tier}</span>},
              {id: 'caps', header: t('extensions.col.capabilities'), width: '14%', render: row => <span className="tp-muted">{row.capabilities.join(', ') || '—'}</span>},
              {
                id: 'actions',
                header: t('common.actions'),
                width: '16%',
                render: row =>
                  operator ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busy || !row.source_uri || !/^[a-f0-9]{64}$/.test(row.sha256)}
                      onClick={() => void handleStageCatalog(row)}
                    >
                      {t('extensions.stageForReview')}
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
                <span>{t('extensions.refreshLockfile')}</span>
              </Button>
            }
          />
          <CodeBlock title="tanpopo.lock" code={lockfile || t('extensions.noLockfile')} />
        </section>
      ) : null}

      <Drawer
        open={stageOpen}
        onClose={() => setStageOpen(false)}
        title={t('extensions.stageDrawerTitle')}
        subtitle={t('extensions.stageDrawerSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleStageCustom}>
          <TextField label={t('extensions.sourceUri')} required value={stageUri} onChange={event => setStageUri(event.target.value)} />
          <TextField label={t('extensions.expectedSha256')} required value={stageSha} onChange={event => setStageSha(event.target.value)} />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setStageOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('extensions.stagePackage')}</Button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={upgradeTarget !== null}
        onClose={() => setUpgradeTarget(null)}
        title={t('extensions.upgradeDrawerTitle', {pkg: upgradeTarget?.package_id ?? ''})}
        subtitle={t('extensions.upgradeDrawerSubtitle')}
      >
        <form className="tp-stack" onSubmit={handleUpgrade}>
          <TextField label={t('extensions.upgradeSourceUri')} required value={upgradeUri} onChange={event => setUpgradeUri(event.target.value)} />
          <TextField label={t('extensions.expectedSha256')} required value={upgradeSha} onChange={event => setUpgradeSha(event.target.value)} />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setUpgradeTarget(null)}>{t('common.cancel')}</Button>
            <Button type="submit" variant="primary" loading={busy}>{t('extensions.stageUpgrade')}</Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={reviewDiff !== null}
        onClose={() => setReviewDiff(null)}
        size="lg"
        title={t('extensions.reviewModalTitle')}
        subtitle={reviewDiff ? `${reviewDiff.package_id} · ${reviewDiff.active_version ?? t('extensions.versionNew')} → ${reviewDiff.candidate_version}` : undefined}
        footer={
          approver ? (
            <>
              <Button variant="secondary" onClick={() => setReviewDiff(null)}>{t('common.cancel')}</Button>
              {reviewMode === 'install' ? (
                <Button variant="secondary" disabled={busy || reviewBlocked} onClick={() => void handleApproveReview(false)}>
                  {t('extensions.approveKeepDisabled')}
                </Button>
              ) : null}
              <Button variant="primary" loading={busy} disabled={reviewBlocked} onClick={() => void handleApproveReview(true)}>
                {t('extensions.approveAndActivate')}
              </Button>
            </>
          ) : (
            <Button variant="secondary" onClick={() => setReviewDiff(null)}>{t('common.close')}</Button>
          )
        }
      >
        {reviewDiff ? (
          <>
            <KeyValueList
              items={[
                {label: t('extensions.originTrust'), value: <StatusPill tone={stateTone(reviewDiff.origin_trust_tier)} label={formatOperationalState(reviewDiff.origin_trust_tier)} />},
                {label: t('extensions.publisherSignature'), value: reviewDiff.enterprise_signed ? t('extensions.enterpriseSigned') : t('extensions.unsigned')},
                {label: t('extensions.col.executionTier'), value: reviewDiff.execution_tier, mono: true},
              ]}
            />

            <PermissionDiffView diff={reviewDiff} />

            {reviewDiff.has_escalation ? (
              <CheckboxField
                label={t('extensions.consentEscalation')}
                checked={ackDelta}
                onChange={setAckDelta}
              />
            ) : null}
            {reviewDiff.origin_trust_tier === 'unverified_external' ? (
              <CheckboxField
                label={t('extensions.consentUnverified')}
                description={t('extensions.consentUnverifiedDesc')}
                checked={ackUnverified}
                onChange={setAckUnverified}
              />
            ) : null}
            {!approver ? <Banner tone="warn">{t('extensions.cannotApprove')}</Banner> : null}
          </>
        ) : null}
      </Modal>

      <ConfirmDialog
        open={actionTarget !== null}
        onClose={() => setActionTarget(null)}
        title={actionTarget ? t(`extensions.confirmTitle.${actionTarget.kind}`, {pkg: actionTarget.row.package_id}) : ''}
        description={
          actionTarget?.kind === 'quarantine'
            ? t('extensions.confirmDesc.quarantine', {
                projects: actionTarget.impact.projects.join(', ') || t('common.none'),
                connections: actionTarget.impact.connections.join(', ') || t('common.none'),
              })
            : actionTarget?.kind === 'rollback'
              ? t('extensions.confirmDesc.rollback', {
                  version: actionTarget.row.previous_rollback_version ?? '',
                  projects: actionTarget.impact.projects.join(', ') || t('common.none'),
                })
              : t('extensions.confirmDesc.uninstall')
        }
        confirmLabel={actionTarget ? t(`extensions.confirmBtn.${actionTarget.kind}`) : t('common.action')}
        requireMatch={actionTarget?.kind === 'quarantine' ? actionTarget.row.package_id : undefined}
        matchLabel={t('extensions.matchPackageId')}
        busy={busy || actionReason.trim().length < 5}
        onConfirm={() => void confirmLifecycleAction()}
      >
        <TextField
          label={t('extensions.auditReason')}
          required
          value={actionReason}
          onChange={event => setActionReason(event.target.value)}
        />
      </ConfirmDialog>
    </div>
  );
}

function PermissionDiffView({diff}: {diff: PermissionDiff}) {
  const {t} = useI18n();
  const active = diff.active_permissions;
  const candidate = diff.candidate_permissions;
  const delta = diff.permission_delta;
  const rows: Array<{id: string; permission: string; activeVal: string; candidateVal: string; changeKey?: string}> = [
    ...setDiffRows('host', t('extensions.diff.host'), active?.allowed_hosts ?? [], candidate.allowed_hosts ?? [], delta.added_hosts ?? []),
    ...setDiffRows('slot', t('extensions.diff.secretSlot'), active?.secret_slots ?? [], candidate.secret_slots ?? [], delta.added_secret_slots ?? []),
    ...setDiffRows('hook', t('extensions.diff.hook'), active?.hooks ?? [], candidate.hooks ?? [], delta.added_hooks ?? []),
    {
      id: 'tier',
      permission: t('extensions.col.executionTier'),
      activeVal: active?.execution_tier ?? t('common.none'),
      candidateVal: candidate.execution_tier,
      changeKey: delta.tier_escalated ? 'extensions.diff.expanded' : active && active.execution_tier !== candidate.execution_tier ? 'extensions.diff.changed' : undefined,
    },
    boolDiffRow('private-ip', t('extensions.diff.privateIp'), active?.allow_private_ip, candidate.allow_private_ip, delta.private_ip_escalated === true, false, t),
    boolDiffRow('tls', t('extensions.diff.requireTls'), active?.require_tls, candidate.require_tls, delta.tls_downgraded === true, true, t),
    boolDiffRow('req-body', t('extensions.diff.inspectReqBody'), active?.inspect_request_body, candidate.inspect_request_body, delta.payload_access_escalated === true && candidate.inspect_request_body === true, false, t),
    boolDiffRow('res-body', t('extensions.diff.inspectResBody'), active?.inspect_response_body, candidate.inspect_response_body, delta.payload_access_escalated === true && candidate.inspect_response_body === true, false, t),
    {
      id: 'quotas',
      permission: t('extensions.diff.resourceLimits'),
      activeVal: active ? formatQuotas(active, t) : t('common.none'),
      candidateVal: formatQuotas(candidate, t),
      changeKey: delta.memory_increased || delta.cpu_quota_increased ? 'extensions.diff.expanded' : undefined,
    },
  ];

  return (
    <DataTable
      ariaLabel={t('extensions.diffAria')}
      rows={rows}
      rowKey={row => row.id}
      columns={[
        {id: 'perm', header: t('extensions.diff.permission'), primary: true, width: '34%', render: row => <span className="mono">{row.permission}</span>},
        {id: 'active', header: t('extensions.diff.active'), width: '28%', render: row => <span className="mono tp-muted">{row.activeVal}</span>},
        {
          id: 'candidate',
          header: t('extensions.diff.candidate'),
          width: '38%',
          render: row => (
            <div className="tp-row">
              <span className="mono">{row.candidateVal}</span>
              {row.changeKey ? <StatusPill tone={row.changeKey === 'extensions.diff.removed' ? 'ok' : 'warn'} label={t(row.changeKey)} /> : null}
            </div>
          ),
        },
      ]}
    />
  );
}

function setDiffRows(prefix: string, label: string, active: string[], candidate: string[], added: string[]) {
  const cur = new Set(active);
  const next = new Set(candidate);
  const add = new Set(added);
  return [...new Set([...active, ...candidate])].sort().map(item => ({
    id: `${prefix}-${item}`,
    permission: `${label}: ${item}`,
    activeVal: cur.has(item) ? item : '—',
    candidateVal: next.has(item) ? item : '—',
    changeKey: add.has(item) || (!cur.has(item) && next.has(item)) ? 'extensions.diff.added' : cur.has(item) && !next.has(item) ? 'extensions.diff.removed' : undefined,
  }));
}

function boolDiffRow(
  id: string,
  permission: string,
  active: boolean | undefined,
  candidate: boolean | undefined,
  escalated: boolean,
  reverse: boolean,
  t: (key: string) => string,
) {
  const activeVal = active === undefined ? t('common.none') : active ? formatOperationalState('ENABLED') : formatOperationalState('DISABLED');
  const candidateVal = candidate ? formatOperationalState('ENABLED') : formatOperationalState('DISABLED');
  const changed = active !== undefined && active !== candidate;
  const expanded = active === undefined ? !reverse && candidate === true : reverse ? active === true && candidate === false : active === false && candidate === true;
  return {
    id,
    permission,
    activeVal,
    candidateVal,
    changeKey: escalated && expanded ? 'extensions.diff.expanded' : changed ? (expanded ? 'extensions.diff.expanded' : 'extensions.diff.reduced') : undefined,
  };
}

function formatQuotas(p: components['schemas']['ExtensionPermissionSet'], t: (key: string, vars?: Record<string, string | number>) => string) {
  const fmt = (val: number | undefined, unit: string) => (!val ? t('common.default') : `${val} ${unit}`);
  return t('extensions.diff.quotas', {
    mem: fmt(p.max_memory_mib, 'MiB'),
    call: fmt(p.max_cpu_ms_per_call, 'ms'),
    chunk: fmt(p.max_stream_chunk_cpu_ms, 'ms'),
  });
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

function errText(cause: unknown, t: (key: string) => string): string { return cause instanceof Error ? cause.message : t('common.requestFailed'); }
