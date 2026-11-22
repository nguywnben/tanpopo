import {useCallback, useEffect, useMemo, useState, type FormEvent} from 'react';
import {Plus, UserPlus} from 'lucide-react';
import {listAdminModels, listDeployments} from '../../api/catalogClient';
import {
  createMember,
  createVirtualKey,
  listMembers,
  listRecentTraces,
  listVirtualKeys,
  revokeVirtualKey,
  rotateVirtualKey,
  updateMemberRoles,
} from '../../api/managementClient';
import type {components} from '../../api/generated';
import {useI18n} from '../../i18n/I18nProvider';
import {useRouter} from '../../router/RouterContext';
import {DataTable} from '../../ui/DataTable';
import {MultiSelectField} from '../../ui/MultiSelectField';
import {ConfirmDialog, Drawer, Modal} from '../../ui/Overlays';
import {
  Banner,
  Button,
  CheckboxField,
  CodeBlock,
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
type VirtualKey = components['schemas']['VirtualKeySummary'];
type Member = components['schemas']['MemberSummary'];
type CreatedKey = components['schemas']['VirtualKeyCreatedResponse'];

const allRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER', 'FINOPS_VIEWER', 'DEVELOPER'];
const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];
const memberReadRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function AccessPage({
  pathname,
  projectId,
  environmentId,
  currentPrincipalId,
  roles,
}: {
  pathname: string;
  projectId: string;
  environmentId: string;
  currentPrincipalId: string;
  roles: Role[];
}) {
  const {navigate} = useRouter();
  const {t, formatNumber, formatDateTime} = useI18n();
  const activeTab = pathname.startsWith('/access/members') || pathname.startsWith('/access/roles') ? 'members' : 'keys';

  const [keys, setKeys] = useState<VirtualKey[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [traces, setTraces] = useState<components['schemas']['TraceSummary'][]>([]);
  const [availableModels, setAvailableModels] = useState<components['schemas']['LogicalModel'][]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [issueOpen, setIssueOpen] = useState(false);
  const [keyName, setKeyName] = useState('');
  const [allowedModels, setAllowedModels] = useState<string[]>(['*']);
  const [allowedMcpToolsText, setAllowedMcpToolsText] = useState('');
  const [createdKey, setCreatedKey] = useState<CreatedKey | null>(null);

  const [rotateTarget, setRotateTarget] = useState<VirtualKey | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<VirtualKey | null>(null);
  const [revokeMode, setRevokeMode] = useState<'GRACEFUL_DRAIN' | 'EMERGENCY_ABORT_STREAMS'>('GRACEFUL_DRAIN');
  const [revokeConfirmName, setRevokeConfirmName] = useState('');
  const [revokeReason, setRevokeReason] = useState('');

  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [memberEmail, setMemberEmail] = useState('');
  const [memberRoles, setMemberRoles] = useState<Role[]>(['DEVELOPER']);
  const [editingMember, setEditingMember] = useState<Member | null>(null);
  const [draftRoles, setDraftRoles] = useState<Role[]>([]);

  const canManage = roles.some(r => manageRoles.includes(r));
  const canReadMembers = roles.some(r => memberReadRoles.includes(r));
  const canEmergencyRevoke = canManage || roles.includes('SECURITY_REVIEWER');

  const refresh = useCallback(async () => {
    if (!projectId || !environmentId) {
      setKeys([]);
      setMembers([]);
      setTraces([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    const [keysRes, membersRes, tracesRes, modelsRes, depsRes] = await Promise.allSettled([
      listVirtualKeys(projectId, environmentId),
      canReadMembers ? listMembers() : Promise.resolve([] as Member[]),
      listRecentTraces(environmentId),
      listAdminModels(),
      listDeployments(environmentId),
    ]);
    const failures: string[] = [];
    if (keysRes.status === 'fulfilled') setKeys(keysRes.value);
    else failures.push(t('access.loadKeysFailed', {error: errText(keysRes.reason, t)}));
    if (membersRes.status === 'fulfilled') setMembers(membersRes.value);
    else if (canReadMembers) failures.push(t('access.loadMembersFailed', {error: errText(membersRes.reason, t)}));
    if (tracesRes.status === 'fulfilled') setTraces(tracesRes.value);
    if (modelsRes.status === 'fulfilled' && depsRes.status === 'fulfilled') {
      const deployedIds = new Set(depsRes.value.map(d => d.logical_model_id));
      setAvailableModels(modelsRes.value.filter(m => deployedIds.has(m.id)));
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, environmentId, canReadMembers, t]);

  useEffect(() => { void refresh(); }, [refresh]);

  const requestsByKey = useMemo(() => {
    const counts = new Map<string, number>();
    for (const trace of traces) {
      if (trace.virtual_key_id) counts.set(trace.virtual_key_id, (counts.get(trace.virtual_key_id) ?? 0) + 1);
    }
    return counts;
  }, [traces]);

  async function handleIssueKey(event: FormEvent) {
    event.preventDefault();
    if (!canManage || !projectId || !environmentId || !keyName.trim() || allowedModels.length === 0) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const created = await createVirtualKey({
        name: keyName.trim(),
        project_id: projectId,
        env_id: environmentId,
        allowed_models: allowedModels,
        allowed_mcp_tools: [...new Set(allowedMcpToolsText.split(',').map(v => v.trim()).filter(Boolean))],
      });
      setIssueOpen(false);
      setKeyName('');
      setAllowedModels(['*']);
      setAllowedMcpToolsText('');
      setCreatedKey(created);
      await refresh();
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleRotate() {
    if (!rotateTarget) return;
    setBusy(true);
    setError('');
    try {
      const created = await rotateVirtualKey(rotateTarget, 86400);
      setRotateTarget(null);
      setCreatedKey(created);
      await refresh();
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleRevoke() {
    if (!revokeTarget) return;
    if (revokeMode === 'EMERGENCY_ABORT_STREAMS' && (revokeConfirmName !== revokeTarget.name || revokeReason.trim().length < 5)) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const result = await revokeVirtualKey(revokeTarget, revokeMode, revokeReason.trim() || 'Graceful drain revocation');
      setRevokeTarget(null);
      setRevokeConfirmName('');
      setRevokeReason('');
      await refresh();
      setNotice(t('access.keyRevoked', {count: formatNumber(result.severed_streams_count)}));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleAddMember(event: FormEvent) {
    event.preventDefault();
    if (!canManage || !memberEmail.includes('@') || memberRoles.length === 0) return;
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await createMember(memberEmail.trim(), memberRoles);
      setAddMemberOpen(false);
      setMemberEmail('');
      setMemberRoles(['DEVELOPER']);
      await refresh();
      setNotice(t('access.memberAdded'));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleSaveMemberRoles() {
    if (!editingMember || draftRoles.length === 0) return;
    if (editingMember.id === currentPrincipalId && draftRoles.some(r => !editingMember.roles.includes(r))) {
      setError(t('access.errSelfEscalation'));
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updateMemberRoles(editingMember, draftRoles);
      setEditingMember(null);
      await refresh();
      setNotice(t('access.rolesUpdated'));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>{t('access.title')}</h1>
          <p className="tp-page-subtitle">{t('access.subtitle')}</p>
        </div>
        {canManage ? (
          <div className={activeTab === 'keys' && !loading && keys.length === 0 && environmentId ? 'tp-page-actions tp-header-cta-desktop' : 'tp-page-actions'}>
            {activeTab === 'keys' ? (
              <Button
                variant={!loading && keys.length === 0 && environmentId ? 'secondary' : 'primary'}
                size="sm"
                disabled={!environmentId}
                onClick={() => setIssueOpen(true)}
              >
                <Plus size={14} aria-hidden="true" />
                <span>{t('access.issueKey')}</span>
              </Button>
            ) : (
              <Button variant="primary" size="sm" onClick={() => setAddMemberOpen(true)}>
                <UserPlus size={14} aria-hidden="true" />
                <span>{t('access.addMember')}</span>
              </Button>
            )}
          </div>
        ) : null}
      </div>

      <Tabs
        label={t('access.tabsLabel')}
        activeId={activeTab}
        onSelect={id => navigate(id === 'members' ? '/access/members' : '/access/virtual-keys')}
        items={[
          {id: 'keys', label: t('access.tabKeys'), count: keys.length},
          {id: 'members', label: t('access.tabMembers'), count: members.length},
        ]}
      />

      {!environmentId ? <Banner tone="info">{t('access.selectScopeBanner')}</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'keys' ? (
        <section className="tp-section">
          <SectionHeader title={t('access.envKeysTitle')} count={keys.length} />
          <DataTable
            ariaLabel={t('access.keysAria')}
            loading={loading}
            rows={keys}
            rowKey={row => row.id}
            emptyTitle={t('access.emptyKeysTitle')}
            emptyDescription={t('access.emptyKeysDesc')}
            emptyAction={
              canManage && environmentId ? (
                <Button variant="primary" size="md" onClick={() => setIssueOpen(true)}>
                  <Plus size={14} aria-hidden="true" />
                  <span>{t('access.issueKey')}</span>
                </Button>
              ) : undefined
            }
            columns={[
              {id: 'name', header: t('common.name'), primary: true, width: '30%', render: row => <span style={{fontWeight: 600}}>{row.name}</span>},
              {id: 'key', header: t('access.col.prefixLast4'), width: '20%', render: row => <span className="mono">{row.key_prefix}…{row.last_four}</span>},
              {id: 'state', header: t('common.state'), width: '16%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {id: 'created', header: t('common.created'), width: '22%', render: row => <span className="num tp-muted">{formatDateTime(row.created_at)}</span>},
              {
                id: 'actions',
                header: t('common.actions'),
                width: '12%',
                render: row =>
                  row.state !== 'REVOKED' && (canManage || canEmergencyRevoke) ? (
                    <RowActionsMenu
                      label={t('common.actionsFor', {name: row.name})}
                      items={[
                        ...(canManage ? [{id: 'rotate', label: t('access.action.rotate'), onClick: () => setRotateTarget(row)}] : []),
                        ...(canManage ? [{type: 'divider' as const}] : []),
                        {
                          id: 'revoke',
                          label: t('access.action.revoke'),
                          variant: 'destructive' as const,
                          onClick: () => {
                            setRevokeTarget(row);
                            setRevokeMode(canManage ? 'GRACEFUL_DRAIN' : 'EMERGENCY_ABORT_STREAMS');
                            setRevokeConfirmName('');
                            setRevokeReason('');
                          },
                        },
                      ]}
                    />
                  ) : null,
              },
            ]}
          />
        </section>
      ) : !canReadMembers ? (
        <Banner tone="info">{t('access.membersNoRead')}</Banner>
      ) : (
        <section className="tp-section">
          <SectionHeader title={t('access.orgMembersTitle')} count={members.length} />
          <DataTable
            ariaLabel={t('access.membersAria')}
            loading={loading}
            rows={members}
            rowKey={row => row.id}
            emptyTitle={t('access.emptyMembersTitle')}
            columns={[
              {id: 'email', header: t('access.col.principal'), primary: true, width: '32%', render: row => <span style={{fontWeight: 600}}>{row.email}</span>},
              {id: 'kind', header: t('common.kind'), width: '14%', render: row => <span className="mono">{row.kind}</span>},
              {
                id: 'roles',
                header: t('access.col.assignedRoles'),
                width: '36%',
                render: row => (
                  <div className="tp-row">
                    {row.roles.map(role => <span key={role} className="tp-meta-tag">{t(`common.role.${role}`)}</span>)}
                  </div>
                ),
              },
              {
                id: 'actions',
                header: t('common.actions'),
                width: '18%',
                render: row =>
                  canManage ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={busy}
                      onClick={() => {
                        setEditingMember(row);
                        setDraftRoles([...row.roles]);
                      }}
                    >
                      {t('access.editRoles')}
                    </Button>
                  ) : null,
              },
            ]}
          />
        </section>
      )}

      <Drawer
        open={issueOpen}
        onClose={() => setIssueOpen(false)}
        title={t('access.issueTitle')}
        subtitle={t('access.issueSubtitle', {env: environmentId || t('common.none')})}
        footer={
          <>
            <Button variant="secondary" onClick={() => setIssueOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-issue-key-form" variant="primary" loading={busy} disabled={!keyName.trim() || allowedModels.length === 0}>
              {t('access.createKey')}
            </Button>
          </>
        }
      >
        <form id="tp-issue-key-form" className="tp-stack" onSubmit={handleIssueKey}>
          <TextField label={t('access.keyName')} required value={keyName} onChange={event => setKeyName(event.target.value)} />
          <div className="tp-stack-sm">
            <CheckboxField label={t('access.allowAllModels')} checked={allowedModels.includes('*')} onChange={checked => setAllowedModels(checked ? ['*'] : [])} />
            {!allowedModels.includes('*') ? (
              <MultiSelectField
                label={t('access.allowedModels')}
                placeholder={t('access.chooseAllowedModels')}
                hasSearch
                hasClear
                value={allowedModels}
                options={availableModels.map(model => ({value: model.slug, label: model.slug}))}
                onChange={setAllowedModels}
              />
            ) : null}
          </div>
          <TextField
            label={t('access.allowedMcpTools')}
            hint={t('access.allowedMcpToolsHint')}
            value={allowedMcpToolsText}
            onChange={event => setAllowedMcpToolsText(event.target.value)}
          />
        </form>
      </Drawer>

      <Modal
        open={createdKey !== null}
        dismissible={false}
        onClose={() => setCreatedKey(null)}
        title={t('access.storeKeyTitle')}
        subtitle={t('access.storeKeySubtitle')}
        footer={<Button variant="primary" onClick={() => setCreatedKey(null)}>{t('access.storeKeyConfirm')}</Button>}
      >
        {createdKey ? (
          <CodeBlock title={`${createdKey.name} (${createdKey.key_prefix}…${createdKey.last_four})`} code={createdKey.secret_key} />
        ) : null}
      </Modal>

      <ConfirmDialog
        open={rotateTarget !== null}
        onClose={() => setRotateTarget(null)}
        title={t('access.rotateTitle', {name: rotateTarget?.name ?? ''})}
        description={t('access.rotateDesc')}
        confirmLabel={t('access.action.rotate')}
        confirmVariant="primary"
        busy={busy}
        onConfirm={() => void handleRotate()}
      />

      <Modal
        open={revokeTarget !== null}
        onClose={() => !busy && setRevokeTarget(null)}
        title={t('access.revokeTitle', {name: revokeTarget?.name ?? ''})}
        subtitle={t('access.revokeSubtitle', {count: formatNumber(revokeTarget ? requestsByKey.get(revokeTarget.id) ?? 0 : 0)})}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setRevokeTarget(null)}>{t('common.cancel')}</Button>
            <Button
              variant="danger"
              loading={busy}
              disabled={
                !revokeTarget ||
                (revokeMode === 'EMERGENCY_ABORT_STREAMS' && (revokeConfirmName !== revokeTarget.name || revokeReason.trim().length < 5))
              }
              onClick={() => void handleRevoke()}
            >
              {revokeMode === 'GRACEFUL_DRAIN' ? t('access.revokeGracefulBtn') : t('access.revokeEmergencyBtn')}
            </Button>
          </>
        }
      >
        <SelectField
          label={t('access.revocationMode')}
          value={revokeMode}
          onChange={next => setRevokeMode(next as typeof revokeMode)}
          options={[
            {
              value: 'GRACEFUL_DRAIN',
              label: t('access.modeGraceful'),
              disabled: !canManage,
            },
            {
              value: 'EMERGENCY_ABORT_STREAMS',
              label: t('access.modeEmergency'),
              disabled: !canEmergencyRevoke,
            },
          ]}
        />
        {revokeMode === 'EMERGENCY_ABORT_STREAMS' ? (
          <>
            <Banner tone="danger">{t('access.emergencyBanner')}</Banner>
            <TextField
              label={t('common.typeToConfirm', {value: revokeTarget?.name ?? ''})}
              value={revokeConfirmName}
              onChange={event => setRevokeConfirmName(event.target.value)}
            />
            <TextField
              label={t('access.auditReason')}
              value={revokeReason}
              onChange={event => setRevokeReason(event.target.value)}
            />
          </>
        ) : (
          <p className="tp-muted">{t('access.gracefulHint')}</p>
        )}
      </Modal>

      <Drawer
        open={addMemberOpen}
        onClose={() => setAddMemberOpen(false)}
        title={t('access.addMemberTitle')}
        footer={
          <>
            <Button variant="secondary" onClick={() => setAddMemberOpen(false)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-add-member-form" variant="primary" loading={busy} disabled={!memberEmail.includes('@') || memberRoles.length === 0}>
              {t('access.addMember')}
            </Button>
          </>
        }
      >
        <form id="tp-add-member-form" className="tp-stack" onSubmit={handleAddMember}>
          <TextField label={t('access.memberEmail')} type="email" required value={memberEmail} onChange={event => setMemberEmail(event.target.value)} />
          <MultiSelectField
            label={t('access.assignRoles')}
            placeholder={t('access.selectRoles')}
            hasClear
            value={memberRoles}
            options={allRoles.map(role => ({value: role, label: t(`common.role.${role}`)}))}
            onChange={next => setMemberRoles(next as Role[])}
          />
        </form>
      </Drawer>

      <Modal
        open={editingMember !== null}
        onClose={() => setEditingMember(null)}
        title={t('access.editRolesTitle', {email: editingMember?.email ?? ''})}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setEditingMember(null)}>{t('common.cancel')}</Button>
            <Button variant="primary" loading={busy} disabled={draftRoles.length === 0} onClick={() => void handleSaveMemberRoles()}>
              {t('access.saveRoles')}
            </Button>
          </>
        }
      >
        {editingMember ? (
          <div className="tp-stack-sm">
            {editingMember.id === currentPrincipalId ? (
              <Banner tone="info">{t('access.selfServiceDisabled')}</Banner>
            ) : null}
            <MultiSelectField
              label={t('access.col.assignedRoles')}
              placeholder={t('access.selectRoles')}
              disabled={busy}
              value={draftRoles}
              options={allRoles.map(role => ({
                value: role,
                label: t(`common.role.${role}`),
                disabled: editingMember.id === currentPrincipalId && !editingMember.roles.includes(role),
              }))}
              onChange={next => setDraftRoles(next as Role[])}
            />
          </div>
        ) : null}
      </Modal>
    </div>
  );
}

function errText(cause: unknown, t: (key: string) => string): string { return cause instanceof Error ? cause.message : t('common.requestFailed'); }
