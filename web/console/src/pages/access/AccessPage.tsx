import {useCallback, useEffect, useMemo, useRef, useState, type FormEvent} from 'react';
import {Plus, UserPlus} from 'lucide-react';
import {listAdminModels, listDeployments} from '../../api/catalogClient';
import {
  createMember,
  createVirtualKey,
  issueMemberEnrollmentToken,
  listMembers,
  listRecentTraces,
  listVirtualKeys,
  revokeVirtualKey,
  rotateVirtualKey,
  updateMemberRoles,
  updateVirtualKey,
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
type Project = components['schemas']['Project'];
type VirtualKey = components['schemas']['VirtualKeySummary'];
type Member = components['schemas']['MemberSummary'];
type CreatedKey = components['schemas']['VirtualKeyCreatedResponse'];

const orgMemberRoles: Role[] = ['ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER', 'FINOPS_VIEWER', 'DEVELOPER'];
const orgAdminRoles: Role[] = ['OPERATOR', 'ORG_OWNER'];
const teamAdminGrantableRoles: Role[] = ['TEAM_ADMIN', 'DEVELOPER'];
const manageRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN'];
const memberReadRoles: Role[] = ['OPERATOR', 'ORG_OWNER', 'TEAM_ADMIN', 'SECURITY_REVIEWER'];

export function AccessPage({
  pathname,
  projectId,
  projects = [],
  environmentId,
  currentPrincipalId,
  roles,
}: {
  pathname: string;
  projectId: string;
  projects?: Project[];
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
  const [keysRestricted, setKeysRestricted] = useState(false);
  const [keysLoadError, setKeysLoadError] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [issueOpen, setIssueOpen] = useState(false);
  const [keyName, setKeyName] = useState('');
  const [allowedPlanes, setAllowedPlanes] = useState<('PROVIDER' | 'MCP' | 'A2A')[]>(['PROVIDER']);
  const [allowedModels, setAllowedModels] = useState<string[]>(['*']);
  const [allowedRoutesText, setAllowedRoutesText] = useState('*');
  const [allowedMcpToolsText, setAllowedMcpToolsText] = useState('');
  const [editingKey, setEditingKey] = useState<VirtualKey | null>(null);
  const [draftPlanes, setDraftPlanes] = useState<('PROVIDER' | 'MCP' | 'A2A')[]>(['PROVIDER']);
  const [draftRoutesText, setDraftRoutesText] = useState('*');
  const [createdKey, setCreatedKey] = useState<CreatedKey | null>(null);

  const [rotateTarget, setRotateTarget] = useState<VirtualKey | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<VirtualKey | null>(null);
  const [revokeMode, setRevokeMode] = useState<'GRACEFUL_DRAIN' | 'EMERGENCY_ABORT_STREAMS'>('GRACEFUL_DRAIN');
  const [revokeConfirmName, setRevokeConfirmName] = useState('');
  const [revokeReason, setRevokeReason] = useState('');

  const [addMemberOpen, setAddMemberOpen] = useState(false);
  const [memberEmail, setMemberEmail] = useState('');
  const [memberRoles, setMemberRoles] = useState<Role[]>(['DEVELOPER']);
  const [memberProjectIds, setMemberProjectIds] = useState<string[]>(projectId ? [projectId] : []);
  const [memberError, setMemberError] = useState('');
  const [issuedEnrollment, setIssuedEnrollment] = useState<{email: string; token: string} | null>(null);
  const [editingMember, setEditingMember] = useState<Member | null>(null);
  const [draftRoles, setDraftRoles] = useState<Role[]>([]);
  const [draftProjectIds, setDraftProjectIds] = useState<string[]>([]);
  const refreshSeqRef = useRef(0);

  const isOrgAdmin = roles.some(r => orgAdminRoles.includes(r));
  const isSecurityReviewer = roles.includes('SECURITY_REVIEWER');
  const grantableMemberRoles = isOrgAdmin ? orgMemberRoles : teamAdminGrantableRoles;
  const canManage = roles.some(r => manageRoles.includes(r));
  const canReadMembers = roles.some(r => memberReadRoles.includes(r));
  const canEmergencyRevoke = canManage || isSecurityReviewer;
  const fallbackProjectId = projects[0]?.id ?? '';

  const refresh = useCallback(async () => {
    const seq = ++refreshSeqRef.current;
    const hasEnvScope = Boolean(projectId && environmentId);
    const scopedMemberProject = projectId || fallbackProjectId;
    const memberQueryProject = isOrgAdmin || isSecurityReviewer ? undefined : (scopedMemberProject || undefined);
    const shouldFetchMembers = canReadMembers && (isOrgAdmin || isSecurityReviewer || Boolean(scopedMemberProject));

    if (!hasEnvScope) {
      setKeys([]);
      setTraces([]);
      setAvailableModels([]);
    }
    setLoading(true);
    setError('');
    setKeysRestricted(false);
    setKeysLoadError('');
    const [keysRes, membersRes, tracesRes, modelsRes, depsRes] = await Promise.allSettled([
      hasEnvScope ? listVirtualKeys(projectId, environmentId) : Promise.resolve([] as VirtualKey[]),
      shouldFetchMembers ? listMembers(memberQueryProject) : Promise.resolve([] as Member[]),
      hasEnvScope ? listRecentTraces(environmentId) : Promise.resolve([] as components['schemas']['TraceSummary'][]),
      hasEnvScope ? listAdminModels() : Promise.resolve([] as components['schemas']['LogicalModel'][]),
      hasEnvScope ? listDeployments(environmentId) : Promise.resolve([]),
    ]);
    if (refreshSeqRef.current !== seq) return;
    const failures: string[] = [];
    if (hasEnvScope) {
      if (keysRes.status === 'fulfilled') {
        setKeys(keysRes.value);
      } else if (isForbiddenError(keysRes.reason)) {
        setKeys([]);
        setKeysRestricted(true);
      } else {
        const keyMsg = t('access.loadKeysFailed', {error: errText(keysRes.reason, t)});
        setKeys([]);
        setKeysLoadError(keyMsg);
        failures.push(keyMsg);
      }
    }
    if (shouldFetchMembers) {
      if (membersRes.status === 'fulfilled') setMembers(membersRes.value);
      else failures.push(t('access.loadMembersFailed', {error: errText(membersRes.reason, t)}));
    } else {
      setMembers([]);
    }
    if (hasEnvScope && tracesRes.status === 'fulfilled') setTraces(tracesRes.value);
    if (hasEnvScope && modelsRes.status === 'fulfilled' && depsRes.status === 'fulfilled') {
      const deployedIds = new Set(depsRes.value.map(d => d.logical_model_id));
      setAvailableModels(modelsRes.value.filter(m => deployedIds.has(m.id)));
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, fallbackProjectId, environmentId, canReadMembers, isOrgAdmin, isSecurityReviewer, t]);

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
    if (!canManage || !projectId || !environmentId || !keyName.trim() || allowedPlanes.length === 0 || allowedModels.length === 0) return;
    const parsedRoutes = [...new Set(allowedRoutesText.split(',').map(v => v.trim()).filter(Boolean))];
    if (parsedRoutes.length === 0) {
      setError(t('access.errAllowedRoutesRequired'));
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const created = await createVirtualKey({
        name: keyName.trim(),
        project_id: projectId,
        env_id: environmentId,
        allowed_planes: allowedPlanes,
        allowed_models: allowedModels,
        allowed_routes: parsedRoutes,
        allowed_mcp_tools: [...new Set(allowedMcpToolsText.split(',').map(v => v.trim()).filter(Boolean))],
      });
      setIssueOpen(false);
      setKeyName('');
      setAllowedPlanes(['PROVIDER']);
      setAllowedModels(['*']);
      setAllowedRoutesText('*');
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

  async function handleSaveKeyGrants(event: FormEvent) {
    event.preventDefault();
    if (!editingKey || draftPlanes.length === 0) return;
    const parsedRoutes = [...new Set(draftRoutesText.split(',').map(v => v.trim()).filter(Boolean))];
    if (parsedRoutes.length === 0) {
      setError(t('access.errAllowedRoutesRequired'));
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updateVirtualKey(editingKey, {
        allowed_planes: draftPlanes,
        allowed_routes: parsedRoutes,
      });
      setEditingKey(null);
      await refresh();
      setNotice(t('access.keyUpdated'));
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleAddMember(event: FormEvent) {
    event.preventDefault();
    const effectiveProjects = isOrgAdmin
      ? (memberRoles.some(r => teamAdminGrantableRoles.includes(r)) ? memberProjectIds : [])
      : (memberProjectIds.length > 0 ? memberProjectIds : projectId ? [projectId] : []);
    if (!canManage || !memberEmail.includes('@') || memberRoles.length === 0) return;
    if (!isOrgAdmin && effectiveProjects.length === 0) {
      setMemberError(t('access.errProjectScopeRequired'));
      return;
    }
    setBusy(true);
    setMemberError('');
    setError('');
    setNotice('');
    try {
      const created = await createMember(memberEmail.trim(), memberRoles, effectiveProjects);
      setAddMemberOpen(false);
      setMemberEmail('');
      setMemberRoles(['DEVELOPER']);
      setMemberProjectIds(projectId ? [projectId] : []);
      if (created.enrollment_token) {
        setIssuedEnrollment({email: created.email, token: created.enrollment_token});
      }
      await refresh();
      setNotice(t('access.memberAdded'));
    } catch (cause) {
      setMemberError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleIssueEnrollment(member: Member) {
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const issued = await issueMemberEnrollmentToken(member.id);
      setIssuedEnrollment({email: member.email, token: issued.enrollment_token});
      await refresh();
    } catch (cause) {
      setError(errText(cause, t));
    } finally {
      setBusy(false);
    }
  }

  async function handleSaveMemberRoles() {
    if (!editingMember || draftRoles.length === 0) return;
    if (editingMember.id === currentPrincipalId && draftRoles.some(r => !editingMember.roles.includes(r))) {
      setMemberError(t('access.errSelfEscalation'));
      return;
    }
    const effectiveProjects = isOrgAdmin
      ? (draftRoles.some(r => teamAdminGrantableRoles.includes(r)) ? draftProjectIds : [])
      : (draftProjectIds.length > 0 ? draftProjectIds : projectId ? [projectId] : []);
    if (!isOrgAdmin && effectiveProjects.length === 0) {
      setMemberError(t('access.errProjectScopeRequired'));
      return;
    }
    setBusy(true);
    setMemberError('');
    setError('');
    setNotice('');
    try {
      await updateMemberRoles(editingMember, draftRoles, effectiveProjects.length > 0 ? effectiveProjects : undefined);
      setEditingMember(null);
      await refresh();
      setNotice(t('access.rolesUpdated'));
    } catch (cause) {
      setMemberError(errText(cause, t));
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
              <Button
                variant="primary"
                size="sm"
                onClick={() => {
                  setMemberError('');
                  setMemberProjectIds(projectId ? [projectId] : []);
                  setAddMemberOpen(true);
                }}
              >
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
          {id: 'keys', label: t('access.tabKeys'), ...(keysRestricted || Boolean(keysLoadError) ? {} : {count: keys.length})},
          {id: 'members', label: t('access.tabMembers'), ...(canReadMembers ? {count: members.length} : {})},
        ]}
      />

      {!environmentId && activeTab === 'keys' ? <Banner tone="info">{t('access.selectScopeBanner')}</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'keys' ? (
        <section className="tp-section">
          <SectionHeader
            title={t('access.envKeysTitle')}
            {...(keysRestricted || Boolean(keysLoadError) ? {} : {count: keys.length})}
          />
          {keysRestricted ? (
            <Banner tone="info" title={t('access.keysRestrictedTitle')}>
              {t('access.keysRestrictedDesc')}
            </Banner>
          ) : (
            <DataTable
              ariaLabel={t('access.keysAria')}
              loading={loading}
              rows={keys}
              rowKey={row => row.id}
              emptyTitle={keysLoadError ? t('common.requestFailed') : t('access.emptyKeysTitle')}
              emptyDescription={keysLoadError || t('access.emptyKeysDesc')}
              emptyAction={
                !keysLoadError && canManage && environmentId ? (
                  <Button variant="primary" size="md" onClick={() => setIssueOpen(true)}>
                    <Plus size={14} aria-hidden="true" />
                    <span>{t('access.issueKey')}</span>
                  </Button>
                ) : undefined
              }
            columns={[
              {id: 'name', header: t('common.name'), primary: true, width: '24%', render: row => <span style={{fontWeight: 600}}>{row.name}</span>},
              {id: 'key', header: t('access.col.prefixLast4'), width: '16%', render: row => <span className="mono">{row.key_prefix}…{row.last_four}</span>},
              {
                id: 'grants',
                header: t('access.col.grants'),
                width: '22%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span className="mono">{(row.allowed_planes && row.allowed_planes.length > 0 ? row.allowed_planes : ['PROVIDER']).join(', ')}</span>
                    <span className="mono tp-muted">{(row.allowed_routes && row.allowed_routes.length > 0 ? row.allowed_routes : ['*']).join(', ')}</span>
                  </div>
                ),
              },
              {id: 'state', header: t('common.state'), width: '12%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {id: 'created', header: t('common.created'), width: '16%', render: row => <span className="num tp-muted">{formatDateTime(row.created_at)}</span>},
              {
                id: 'actions',
                header: t('common.actions'),
                width: '10%',
                render: row =>
                  row.state !== 'REVOKED' && (canManage || canEmergencyRevoke) ? (
                    <RowActionsMenu
                      label={t('common.actionsFor', {name: row.name})}
                      items={[
                        ...(canManage
                          ? [
                              {
                                id: 'editGrants',
                                label: t('access.action.editGrants'),
                                onClick: () => {
                                  setEditingKey(row);
                                  setDraftPlanes((row.allowed_planes && row.allowed_planes.length > 0 ? row.allowed_planes : ['PROVIDER']) as ('PROVIDER' | 'MCP' | 'A2A')[]);
                                  setDraftRoutesText((row.allowed_routes && row.allowed_routes.length > 0 ? row.allowed_routes : ['*']).join(', '));
                                },
                              },
                              {id: 'rotate', label: t('access.action.rotate'), onClick: () => setRotateTarget(row)},
                            ]
                          : []),
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
          )}
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
              {
                id: 'email',
                header: t('access.col.principal'),
                primary: true,
                width: '32%',
                render: row => (
                  <div className="tp-stack-sm">
                    <span style={{fontWeight: 600}}>{row.email}</span>
                    {row.enrollment_pending ? (
                      <StatusPill tone="warn" label={t('access.enrollmentPending')} />
                    ) : row.has_password ? (
                      <StatusPill tone="ok" label={t('access.passwordActive')} />
                    ) : null}
                  </div>
                ),
              },
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
                    <RowActionsMenu
                      label={t('access.memberActionsAria', {email: row.email})}
                      items={[
                        {
                          id: 'roles',
                          label: t('access.editRoles'),
                          onClick: () => {
                            setMemberError('');
                            setEditingMember(row);
                            setDraftRoles([...row.roles]);
                            setDraftProjectIds(row.project_ids && row.project_ids.length > 0 ? [...row.project_ids] : projectId ? [projectId] : []);
                          },
                        },
                        ...(row.kind === 'HUMAN'
                          ? [{
                              id: 'invite',
                              label: t('access.issueInviteToken'),
                              onClick: () => {
                                void handleIssueEnrollment(row);
                              },
                            }]
                          : []),
                      ]}
                    />
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
            <Button type="submit" form="tp-issue-key-form" variant="primary" loading={busy} disabled={!keyName.trim() || allowedPlanes.length === 0 || allowedModels.length === 0}>
              {t('access.createKey')}
            </Button>
          </>
        }
      >
        <form id="tp-issue-key-form" className="tp-stack" onSubmit={handleIssueKey}>
          <TextField label={t('access.keyName')} required value={keyName} onChange={event => setKeyName(event.target.value)} />
          <MultiSelectField
            label={t('access.allowedPlanes')}
            placeholder={t('access.selectPlanes')}
            value={allowedPlanes}
            options={[
              {value: 'PROVIDER', label: 'PROVIDER (/v1/*)'},
              {value: 'MCP', label: 'MCP (/mcp/*)'},
              {value: 'A2A', label: 'A2A (/a2a/*)'},
            ]}
            onChange={next => setAllowedPlanes(next as ('PROVIDER' | 'MCP' | 'A2A')[])}
          />
          <TextField
            label={t('access.allowedRoutes')}
            hint={t('access.allowedRoutesHint')}
            placeholder="*, /a2a/*"
            value={allowedRoutesText}
            onChange={event => setAllowedRoutesText(event.target.value)}
          />
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
        open={editingKey !== null}
        onClose={() => setEditingKey(null)}
        title={t('access.editGrantsTitle', {name: editingKey?.name ?? ''})}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setEditingKey(null)}>{t('common.cancel')}</Button>
            <Button type="submit" form="tp-edit-key-grants-form" variant="primary" loading={busy} disabled={draftPlanes.length === 0}>
              {t('common.saveChanges')}
            </Button>
          </>
        }
      >
        <form id="tp-edit-key-grants-form" className="tp-stack" onSubmit={handleSaveKeyGrants}>
          <MultiSelectField
            label={t('access.allowedPlanes')}
            placeholder={t('access.selectPlanes')}
            value={draftPlanes}
            options={[
              {value: 'PROVIDER', label: 'PROVIDER (/v1/*)'},
              {value: 'MCP', label: 'MCP (/mcp/*)'},
              {value: 'A2A', label: 'A2A (/a2a/*)'},
            ]}
            onChange={next => setDraftPlanes(next as ('PROVIDER' | 'MCP' | 'A2A')[])}
          />
          <TextField
            label={t('access.allowedRoutes')}
            hint={t('access.allowedRoutesHint')}
            placeholder="*, /a2a/*"
            value={draftRoutesText}
            onChange={event => setDraftRoutesText(event.target.value)}
          />
        </form>
      </Modal>

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
        onClose={() => {
          setAddMemberOpen(false);
          setMemberError('');
        }}
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
          {memberError ? <Banner tone="danger">{memberError}</Banner> : null}
          <TextField label={t('access.memberEmail')} type="email" required value={memberEmail} onChange={event => setMemberEmail(event.target.value)} />
          <MultiSelectField
            label={t('access.assignRoles')}
            placeholder={t('access.selectRoles')}
            hasClear
            value={memberRoles}
            options={grantableMemberRoles.map(role => ({value: role, label: t(`common.role.${role}`)}))}
            onChange={next => setMemberRoles(next as Role[])}
          />
          {projects.length > 0 && (!isOrgAdmin || memberRoles.some(r => teamAdminGrantableRoles.includes(r))) ? (
            <MultiSelectField
              label={t('access.memberProjects')}
              placeholder={t('access.selectMemberProjects')}
              value={memberProjectIds}
              options={projects.map(p => ({value: p.id, label: `${p.name} (${p.slug})`}))}
              onChange={setMemberProjectIds}
            />
          ) : null}
        </form>
      </Drawer>

      <Modal
        open={editingMember !== null}
        onClose={() => {
          setEditingMember(null);
          setMemberError('');
        }}
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
            {memberError ? <Banner tone="danger">{memberError}</Banner> : null}
            {editingMember.id === currentPrincipalId ? (
              <Banner tone="info">{t('access.selfServiceDisabled')}</Banner>
            ) : null}
            <MultiSelectField
              label={t('access.col.assignedRoles')}
              placeholder={t('access.selectRoles')}
              disabled={busy}
              value={draftRoles}
              options={grantableMemberRoles.map(role => ({
                value: role,
                label: t(`common.role.${role}`),
                disabled: editingMember.id === currentPrincipalId && !editingMember.roles.includes(role),
              }))}
              onChange={next => setDraftRoles(next as Role[])}
            />
            {projects.length > 0 && (!isOrgAdmin || draftRoles.some(r => teamAdminGrantableRoles.includes(r))) ? (
              <MultiSelectField
                label={t('access.memberProjects')}
                placeholder={t('access.selectMemberProjects')}
                disabled={busy}
                value={draftProjectIds}
                options={projects.map(p => ({value: p.id, label: `${p.name} (${p.slug})`}))}
                onChange={setDraftProjectIds}
              />
            ) : null}
          </div>
        ) : null}
      </Modal>

      <Modal
        open={issuedEnrollment !== null}
        onClose={() => setIssuedEnrollment(null)}
        title={t('access.enrollmentIssuedTitle', {email: issuedEnrollment?.email ?? ''})}
        footer={<Button variant="primary" onClick={() => setIssuedEnrollment(null)}>{t('common.done')}</Button>}
      >
        {issuedEnrollment ? (
          <div className="tp-stack-sm">
            <Banner tone="warn">{t('access.enrollmentOnceWarning')}</Banner>
            <CodeBlock
              title={t('access.enrollmentTokenLabel')}
              code={issuedEnrollment.token}
            />
            <CodeBlock
              title={t('access.enrollmentUrlLabel')}
              code={`${typeof window !== 'undefined' ? window.location.origin : ''}/login#enroll=${encodeURIComponent(issuedEnrollment.token)}`}
            />
          </div>
        ) : null}
      </Modal>
    </div>
  );
}

function errText(cause: unknown, t: (key: string) => string): string { return cause instanceof Error ? cause.message : t('common.requestFailed'); }
function isForbiddenError(cause: unknown): boolean {
  return Boolean(cause && typeof cause === 'object' && 'status' in cause && (cause as {status?: unknown}).status === 403);
}
