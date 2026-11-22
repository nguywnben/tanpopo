import {useCallback, useEffect, useState, type FormEvent} from 'react';
import {ArrowRightLeft, Building2, Plus, RefreshCw, Trash2} from 'lucide-react';
import {listOrganizations} from '../../api/client';
import {createOrganization, deleteOrganization} from '../../api/managementClient';
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
}: {
  roles: Role[];
  activeOrgId: string;
  multiOrgEnabled: boolean;
  onSelectOrg: (orgId: string) => void;
  onOrganizationsChanged: () => void | Promise<void>;
}) {
  const {navigate} = useRouter();
  const {t, formatDateTime} = useI18n();
  const isOperator = roles.includes('OPERATOR');

  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [receipt, setReceipt] = useState<ShredReceipt | null>(null);

  const [createOpen, setCreateOpen] = useState(false);
  const [newName, setNewName] = useState('');
  const [newSlug, setNewSlug] = useState('');
  const [slugTouched, setSlugTouched] = useState(false);

  const [deleteTarget, setDeleteTarget] = useState<Organization | null>(null);
  const [shredReason, setShredReason] = useState('');

  const refresh = useCallback(async (): Promise<Organization[]> => {
    setLoading(true);
    setError('');
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
      if ('destroyed_dek_id' in res) {
        setReceipt(res);
      } else {
        setReceipt(null);
        setNotice(t('state.PENDING_APPROVAL'));
      }
      const remaining = await refresh();
      await onOrganizationsChanged();
      if ('destroyed_dek_id' in res && removedId === activeOrgId && remaining[0]) {
        onSelectOrg(remaining[0].id);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('common.requestFailed'));
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
              render: org => (
                <div className="tp-stack-tight">
                  <div className="tp-inline">
                    <Building2 size={14} aria-hidden="true" />
                    <span className="tp-table-primary">{org.name}</span>
                    {org.id === activeOrgId ? (
                      <StatusPill tone="ok" label={t('orgs.activeLabel')} />
                    ) : null}
                  </div>
                  <span className="tp-mono tp-muted">{org.slug} · {org.id}</span>
                </div>
              ),
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
              render: org => <span className="tp-mono tp-muted">{formatDateTime(org.created_at)}</span>,
            },
            {
              id: 'actions',
              header: t('common.actions'),
              render: org => (
                <div className="tp-inline tp-row-actions">
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={org.id === activeOrgId || !multiOrgEnabled}
                    onClick={() => onSelectOrg(org.id)}
                  >
                    <ArrowRightLeft size={14} aria-hidden="true" />
                    <span>{t('orgs.switchAction')}</span>
                  </Button>
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
                </div>
              ),
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
