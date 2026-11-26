import {useCallback, useEffect, useState, type FormEvent} from 'react';
import {ArrowRightLeft, Building2, Plus, RefreshCw, Trash2} from 'lucide-react';
import {listOrganizations} from '../../api/client';
import {createOrganization, deleteOrganization, finalizeOrganizationErasure} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {ConfirmDialog, Modal} from '../../ui/Overlays';
import {
  Banner,
  Button,
  SectionHeader,
  StatusPill,
  Tabs,
  TextField,
  formatOperationalState,
  stateTone,
} from '../../ui/primitives';

type Role = components['schemas']['RbacRole'];
type Organization = components['schemas']['Organization'];
type ShredReceipt = components['schemas']['CryptoShredReceipt'];

function toSlug(raw: string): string {
  return raw
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 48);
}

export function OrganizationsAdminPage({
  roles,
  activeOrgId,
  multiOrgEnabled,
  onSelectOrg,
  onOrganizationsChanged,
  receipt: controlledReceipt,
  onReceiptChange,
}: {
  roles: Role[];
  activeOrgId: string;
  multiOrgEnabled: boolean;
  onSelectOrg: (orgId: string) => void;
  onOrganizationsChanged: () => void | Promise<void>;
  receipt?: ShredReceipt | null;
  onReceiptChange?: (receipt: ShredReceipt | null) => void;
}) {
  const {navigate} = useRouter();
  const {t, formatDateTime} = useI18n();
  const isOperator = roles.includes('OPERATOR');

  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [localReceipt, setLocalReceipt] = useState<ShredReceipt | null>(null);
  const receipt = controlledReceipt !== undefined ? controlledReceipt : localReceipt;
  const updateReceipt = useCallback(
    (next: ShredReceipt | null) => {
      setLocalReceipt(next);
      onReceiptChange?.(next);
    },
    [onReceiptChange],
  );

  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState('');
  const [newSlug, setNewSlug] = useState('');
  const [slugTouched, setSlugTouched] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<Organization | null>(null);
  const [shredReason, setShredReason] = useState('');

  const refresh = useCallback(async (clearError = true): Promise<Organization[]> => {
    setLoading(true);
    if (clearError) setError('');
    try {
      const items = await listOrganizations();
      setOrgs(items);
      return items;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('common.requestFailed'));
      return [];
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (!isOperator) {
      setLoading(false);
      return;
    }
    void refresh();
  }, [isOperator, refresh]);

  async function handleCreate(event: FormEvent) {
    event.preventDefault();
    const name = newName.trim();
    const slug = (newSlug.trim() || toSlug(name));
    if (!name || !slug || busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const created = await createOrganization(name, slug);
      setCreateOpen(false);
      setNewName('');
      setNewSlug('');
      setSlugTouched(false);
      setNotice(t('orgs.createdNotice', {name: created.name, slug: created.slug}));
      await refresh();
      await onOrganizationsChanged();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('common.requestFailed'));
    } finally {
      setBusy(false);
    }
  }

  async function handleShredConfirm() {
    if (!deleteTarget || busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const res = await deleteOrganization(deleteTarget, deleteTarget.slug, shredReason.trim() || 'operator console erasure');
      const removedId = deleteTarget.id;
      setDeleteTarget(null);
      setShredReason('');
      const hasImmediateReceipt = 'destroyed_dek_id' in res && typeof res.destroyed_dek_id === 'string' && res.destroyed_dek_id.length > 0;
      if (hasImmediateReceipt) {
        updateReceipt(res);
      } else {
        updateReceipt(null);
        setNotice(t('state.PENDING_APPROVAL'));
      }
      const remaining = await refresh();
      await onOrganizationsChanged();
      const nextHealthy = remaining.find(item => item.status !== 'DELETING' && item.status !== 'PENDING_ERASURE') ?? remaining[0];
      if (hasImmediateReceipt && removedId === activeOrgId && nextHealthy) {
        onSelectOrg(nextHealthy.id);
      }
    } catch (cause) {
      setDeleteTarget(null);
      updateReceipt(null);
      setError(cause instanceof Error ? cause.message : t('common.requestFailed'));
      await refresh(false);
    } finally {
      setBusy(false);
    }
  }

  async function handleRetryFinalize(org: Organization) {
    if (busy) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const res = await finalizeOrganizationErasure(org.id);
      const hasImmediateReceipt = 'destroyed_dek_id' in res && typeof res.destroyed_dek_id === 'string' && res.destroyed_dek_id.length > 0;
      updateReceipt(hasImmediateReceipt ? res : null);
      const remaining = await refresh();
      await onOrganizationsChanged();
      const nextHealthy = remaining.find(item => item.status !== 'DELETING' && item.status !== 'PENDING_ERASURE') ?? remaining[0];
      if (hasImmediateReceipt && org.id === activeOrgId && nextHealthy) {
        onSelectOrg(nextHealthy.id);
      }
    } catch (cause) {
      updateReceipt(null);
      setError(cause instanceof Error ? cause.message : t('common.requestFailed'));
      await refresh(false);
    } finally {
      setBusy(false);
    }
  }

  if (!isOperator) {
    return (
      <Banner tone="danger" title={t('operations.restrictedTitle')}>
        {t('orgs.operatorRequired')}
      </Banner>
    );
  }

  const pendingErasureOrgs = orgs.filter(item => item.status === 'DELETING' || item.status === 'PENDING_ERASURE');

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('operations.title')}</h1>
          <p className="tp-page-subtitle">{t('orgs.subtitle')}</p>
        </div>
        <div className="tp-page-actions">
          <Button variant="secondary" size="sm" onClick={() => void refresh()}>
            <RefreshCw size={14} aria-hidden="true" />
            <span>{t('common.refresh')}</span>
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={!multiOrgEnabled}
            onClick={() => {
              setNewName('');
              setNewSlug('');
              setSlugTouched(false);
              setError('');
              setCreateOpen(true);
            }}
          >
            <Plus size={14} aria-hidden="true" />
            <span>{t('orgs.createOrg')}</span>
          </Button>
        </div>
      </div>

      <Tabs
        label={t('operations.tabsLabel')}
        activeId="organizations"
        onSelect={id => {
          if (id === 'backups') navigate('/ops/backups');
          else if (id === 'upgrades') navigate('/ops/upgrades');
          else if (id === 'webhooks') navigate('/settings/residency-retention');
          else if (id === 'health') navigate('/ops/health');
          else navigate('/ops/organizations');
        }}
        items={[
          {id: 'health', label: t('operations.tabHealth')},
          {id: 'backups', label: t('operations.tabBackups')},
          {id: 'upgrades', label: t('operations.tabUpgrades')},
          {id: 'organizations', label: t('orgs.tabTitle'), count: orgs.length},
          {id: 'webhooks', label: t('operations.tabWebhooks')},
        ]}
      />

      {!multiOrgEnabled ? (
        <Banner tone="warn" title={t('orgs.disabledTitle')}>
          {t('orgs.disabledDesc')}
        </Banner>
      ) : null}

      {pendingErasureOrgs.length > 0 ? (
        <Banner tone="warn" title={t('orgs.deletingBannerTitle')}>
          {t('orgs.deletingBannerDesc', {count: String(pendingErasureOrgs.length)})}
        </Banner>
      ) : null}

      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}
      {receipt ? (
        <Banner tone="ok" title={t('orgs.shredReceiptTitle')}>
          {t('orgs.shredReceiptDesc', {
            dek: receipt.destroyed_dek_id,
            keys: String(receipt.revoked_virtual_keys_count),
          })}
        </Banner>
      ) : null}

      <section className="tp-stack" aria-label={t('orgs.directoryTitle')}>
        <SectionHeader title={t('orgs.directoryTitle')} count={orgs.length} />

        <DataTable
          ariaLabel={t('orgs.tableAria')}
          rows={orgs}
          loading={loading}
          rowKey={org => org.id}
          emptyTitle={t('orgs.emptyTitle')}
          emptyDescription={t('orgs.emptyDesc')}
          columns={[
            {
              id: 'name',
              header: t('orgs.colName'),
              primary: true,
              render: org => {
                const deleting = org.status === 'DELETING' || org.status === 'PENDING_ERASURE';
                return (
                  <div className="tp-stack-sm">
                    <div className="tp-row">
                      <Building2 size={14} aria-hidden="true" />
                      <span style={{fontWeight: 600}}>{org.name}</span>
                      {deleting ? (
                        <StatusPill tone="danger" label={t('orgs.statusDeleting')} />
                      ) : org.id === activeOrgId ? (
                        <StatusPill tone="ok" label={t('orgs.activeLabel')} />
                      ) : null}
                    </div>
                    <span className="mono tp-muted">{org.slug} · {org.id}</span>
                  </div>
                );
              },
            },
            {
              id: 'lifecycle',
              header: t('common.state'),
              render: org => {
                const orgStatus = org.status ?? 'ACTIVE';
                const deleting = orgStatus === 'DELETING' || orgStatus === 'PENDING_ERASURE';
                return (
                  <StatusPill
                    tone={deleting ? 'danger' : stateTone(orgStatus)}
                    label={deleting ? t('orgs.statusDeleting') : formatOperationalState(orgStatus)}
                  />
                );
              },
            },
            {
              id: 'trust',
              header: t('orgs.colTrust'),
              render: org => <StatusPill tone="neutral" label={formatOperationalState(org.min_plugin_trust)} />,
            },
            {
              id: 'status',
              header: t('orgs.colFourEyes'),
              render: org => (
                <StatusPill
                  tone={stateTone(org.four_eyes_enabled ? 'ENABLED' : 'DISABLED')}
                  label={formatOperationalState(org.four_eyes_enabled ? 'ENABLED' : 'DISABLED')}
                />
              ),
            },
            {
              id: 'created',
              header: t('common.created'),
              hideOnMobile: true,
              render: org => <span className="mono tp-muted">{formatDateTime(org.created_at)}</span>,
            },
            {
              id: 'actions',
              header: t('common.actions'),
              render: org => {
                const deleting = org.status === 'DELETING' || org.status === 'PENDING_ERASURE';
                return (
                  <div className="tp-row">
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={deleting || org.id === activeOrgId || !multiOrgEnabled}
                      onClick={() => onSelectOrg(org.id)}
                    >
                      <ArrowRightLeft size={14} aria-hidden="true" />
                      <span>{t('orgs.switchAction')}</span>
                    </Button>
                    {deleting ? (
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={busy}
                        onClick={() => void handleRetryFinalize(org)}
                      >
                        <RefreshCw size={14} aria-hidden="true" />
                        <span>{t('orgs.retryFinalizeAction')}</span>
                      </Button>
                    ) : (
                      <Button
                        variant="danger"
                        size="sm"
                        disabled={orgs.length <= 1 || busy}
                        onClick={() => {
                          setShredReason('');
                          setDeleteTarget(org);
                        }}
                      >
                        <Trash2 size={14} aria-hidden="true" />
                        <span>{t('orgs.shredAction')}</span>
                      </Button>
                    )}
                  </div>
                );
              },
            },
          ]}
        />
      </section>

      <Modal
        open={createOpen}
        onClose={() => !busy && setCreateOpen(false)}
        title={t('orgs.createOrg')}
        subtitle={t('orgs.createSubtitle')}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setCreateOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button
              variant="primary"
              type="submit"
              form="create-org-form"
              loading={busy}
              disabled={!newName.trim()}
            >
              {t('orgs.createOrg')}
            </Button>
          </>
        }
      >
        <form id="create-org-form" className="tp-stack" onSubmit={e => void handleCreate(e)}>
          <TextField
            label={t('orgs.nameLabel')}
            value={newName}
            disabled={busy}
            required
            onChange={event => {
              const val = event.target.value;
              setNewName(val);
              if (!slugTouched) setNewSlug(toSlug(val));
            }}
          />
          <TextField
            label={t('orgs.slugLabel')}
            value={newSlug}
            disabled={busy}
            required
            onChange={event => {
              setSlugTouched(true);
              setNewSlug(event.target.value);
            }}
          />
        </form>
      </Modal>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => !busy && setDeleteTarget(null)}
        title={t('orgs.shredTitle')}
        description={t('orgs.shredDesc', {name: deleteTarget?.name ?? ''})}
        confirmLabel={t('orgs.shredAction')}
        confirmVariant="danger"
        requireMatch={deleteTarget?.slug}
        busy={busy}
        onConfirm={() => void handleShredConfirm()}
      >
        <TextField
          label={t('orgs.shredReasonLabel')}
          value={shredReason}
          disabled={busy}
          onChange={event => setShredReason(event.target.value)}
        />
      </ConfirmDialog>
    </div>
  );
}
