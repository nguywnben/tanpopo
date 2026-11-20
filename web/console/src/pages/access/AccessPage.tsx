import {useCallback, useEffect, useMemo, useState, type FormEvent} from 'react';
import {Plus, UserPlus} from 'lucide-react';
import {formatDateAndTime} from '../../i18n/format';
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
  const activeTab = pathname.startsWith('/access/members') || pathname.startsWith('/access/roles') ? 'members' : 'keys';

  const [keys, setKeys] = useState<VirtualKey[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [traces, setTraces] = useState<components['schemas']['TraceSummary'][]>([]);
  const [availableModels, setAvailableModels] = useState<components['schemas']['LogicalModel'][]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  // Issue Key Drawer
  const [issueOpen, setIssueOpen] = useState(false);
  const [keyName, setKeyName] = useState('');
  const [allowedModels, setAllowedModels] = useState<string[]>(['*']);
  const [allowedMcpToolsText, setAllowedMcpToolsText] = useState('');
  const [createdKey, setCreatedKey] = useState<CreatedKey | null>(null);

  // Rotate / Revoke modals
  const [rotateTarget, setRotateTarget] = useState<VirtualKey | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<VirtualKey | null>(null);
  const [revokeMode, setRevokeMode] = useState<'GRACEFUL_DRAIN' | 'EMERGENCY_ABORT_STREAMS'>('GRACEFUL_DRAIN');
  const [revokeConfirmName, setRevokeConfirmName] = useState('');
  const [revokeReason, setRevokeReason] = useState('');

  // Add / Edit Member
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
    else failures.push(`Virtual keys: ${errText(keysRes.reason)}`);
    if (membersRes.status === 'fulfilled') setMembers(membersRes.value);
    else if (canReadMembers) failures.push(`Members: ${errText(membersRes.reason)}`);
    if (tracesRes.status === 'fulfilled') setTraces(tracesRes.value);
    if (modelsRes.status === 'fulfilled' && depsRes.status === 'fulfilled') {
      const deployedIds = new Set(depsRes.value.map(d => d.logical_model_id));
      setAvailableModels(modelsRes.value.filter(m => deployedIds.has(m.id)));
    }
    setError(failures.join(' '));
    setLoading(false);
  }, [projectId, environmentId, canReadMembers]);

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
      setError(errText(cause));
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
      setError(errText(cause));
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
      setNotice(`Virtual key revoked. ${result.severed_streams_count} active streams severed.`);
    } catch (cause) {
      setError(errText(cause));
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
      setNotice('Member added.');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleSaveMemberRoles() {
    if (!editingMember || draftRoles.length === 0) return;
    if (editingMember.id === currentPrincipalId && draftRoles.some(r => !editingMember.roles.includes(r))) {
      setError('Self-privilege escalation is blocked.');
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      await updateMemberRoles(editingMember, draftRoles);
      setEditingMember(null);
      await refresh();
      setNotice('Member roles updated.');
    } catch (cause) {
      setError(errText(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="tp-stack">
      <div className="tp-page-head">
        <div className="tp-page-head-text">
          <h1>Access & Keys</h1>
          <p className="tp-page-subtitle">Environment-scoped Virtual Keys and 6-role RBAC principal assignments.</p>
        </div>
        {canManage ? (
          <div className="tp-page-actions">
            {activeTab === 'keys' ? (
              <Button variant="primary" size="sm" disabled={!environmentId} onClick={() => setIssueOpen(true)}>
                <Plus size={14} aria-hidden="true" />
                <span>Issue key</span>
              </Button>
            ) : (
              <Button variant="primary" size="sm" onClick={() => setAddMemberOpen(true)}>
                <UserPlus size={14} aria-hidden="true" />
                <span>Add member</span>
              </Button>
            )}
          </div>
        ) : null}
      </div>

      <Tabs
        label="Access management views"
        activeId={activeTab}
        onSelect={id => navigate(id === 'members' ? '/access/members' : '/access/virtual-keys')}
        items={[
          {id: 'keys', label: 'Virtual Keys', count: keys.length},
          {id: 'members', label: 'Members & Roles', count: members.length},
        ]}
      />

      {!environmentId ? <Banner tone="info">Select a project and environment in the top bar to manage Virtual Keys.</Banner> : null}
      {error ? <Banner tone="danger">{error}</Banner> : null}
      {notice ? <Banner tone="ok">{notice}</Banner> : null}

      {activeTab === 'keys' ? (
        <section className="tp-section">
          <SectionHeader title="Environment Virtual Keys" count={keys.length} />
          <DataTable
            ariaLabel="Virtual keys"
            loading={loading}
            rows={keys}
            rowKey={row => row.id}
            emptyTitle="No Virtual Keys in this environment"
            emptyDescription="Issue an environment-bound Virtual Key to authenticate Data Plane /v1/* and /mcp traffic."
            emptyAction={canManage && environmentId ? <Button variant="primary" size="sm" onClick={() => setIssueOpen(true)}>Issue key</Button> : undefined}
            columns={[
              {id: 'name', header: 'Name', primary: true, width: '30%', render: row => <span style={{fontWeight: 600}}>{row.name}</span>},
              {id: 'key', header: 'Prefix / Last 4', width: '20%', render: row => <span className="mono">{row.key_prefix}…{row.last_four}</span>},
              {id: 'state', header: 'State', width: '16%', render: row => <StatusPill tone={stateTone(row.state)} label={formatOperationalState(row.state)} />},
              {id: 'created', header: 'Created', width: '22%', render: row => <span className="num tp-muted">{formatDateAndTime(row.created_at)}</span>},
              {
                id: 'actions',
                header: 'Actions',
                width: '12%',
                render: row =>
                  row.state !== 'REVOKED' && (canManage || canEmergencyRevoke) ? (
                    <RowActionsMenu
                      label={`Actions for ${row.name}`}
                      items={[
                        ...(canManage ? [{id: 'rotate', label: 'Rotate key', onClick: () => setRotateTarget(row)}] : []),
                        ...(canManage ? [{type: 'divider' as const}] : []),
                        {
                          id: 'revoke',
                          label: 'Revoke key',
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
        <Banner tone="info">Your role can inspect Virtual Keys but does not have permission to list organization members.</Banner>
      ) : (
        <section className="tp-section">
          <SectionHeader title="Organization members" count={members.length} />
          <DataTable
            ariaLabel="Organization members and roles"
            loading={loading}
            rows={members}
            rowKey={row => row.id}
            emptyTitle="No members found"
            columns={[
              {id: 'email', header: 'Principal', primary: true, width: '32%', render: row => <span style={{fontWeight: 600}}>{row.email}</span>},
              {id: 'kind', header: 'Kind', width: '14%', render: row => <span className="mono">{row.kind}</span>},
              {
                id: 'roles',
                header: 'Assigned roles',
                width: '36%',
                render: row => (
                  <div className="tp-row">
                    {row.roles.map(role => <span key={role} className="tp-meta-tag">{role.replaceAll('_', ' ')}</span>)}
                  </div>
                ),
              },
              {
                id: 'actions',
                header: 'Actions',
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
                      Edit roles
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
        title="Issue Virtual Key"
        subtitle={`Bound to environment ${environmentId || 'none'}. Plaintext secret is displayed once.`}
      >
        <form className="tp-stack" onSubmit={handleIssueKey}>
          <TextField label="Key name" required value={keyName} onChange={event => setKeyName(event.target.value)} />
          <div className="tp-stack-sm">
            <CheckboxField label="Allow all models (*)" checked={allowedModels.includes('*')} onChange={checked => setAllowedModels(checked ? ['*'] : [])} />
            {!allowedModels.includes('*') ? (
              <MultiSelectField
                label="Allowed models"
                placeholder="Choose allowed models"
                hasSearch
                hasClear
                value={allowedModels}
                options={availableModels.map(model => ({value: model.slug, label: model.slug}))}
                onChange={setAllowedModels}
              />
            ) : null}
          </div>
          <TextField
            label="Allowed MCP tools (comma-separated, server__tool)"
            hint="Leave empty to deny all MCP tools for this key."
            value={allowedMcpToolsText}
            onChange={event => setAllowedMcpToolsText(event.target.value)}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setIssueOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!keyName.trim() || allowedModels.length === 0}>
              Create key
            </Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={createdKey !== null}
        dismissible={false}
        onClose={() => setCreatedKey(null)}
        title="Store this Virtual Key now"
        subtitle="Shown once only. After closing, the plaintext key is erased and cannot be recovered."
        footer={<Button variant="primary" onClick={() => setCreatedKey(null)}>I stored it — close & erase</Button>}
      >
        {createdKey ? (
          <CodeBlock title={`${createdKey.name} (${createdKey.key_prefix}…${createdKey.last_four})`} code={createdKey.secret_key} />
        ) : null}
      </Modal>

      <ConfirmDialog
        open={rotateTarget !== null}
        onClose={() => setRotateTarget(null)}
        title={`Rotate ${rotateTarget?.name ?? ''}`}
        description="Both old and new secrets stay valid for a 24-hour grace window. The new plaintext secret will be shown once."
        confirmLabel="Rotate key"
        confirmVariant="primary"
        busy={busy}
        onConfirm={() => void handleRotate()}
      />

      <Modal
        open={revokeTarget !== null}
        onClose={() => !busy && setRevokeTarget(null)}
        title={`Revoke ${revokeTarget?.name ?? ''}`}
        subtitle={`Recent trace-derived traffic (last 1h): ${revokeTarget ? requestsByKey.get(revokeTarget.id) ?? 0 : 0} requests.`}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setRevokeTarget(null)}>Cancel</Button>
            <Button
              variant="danger"
              loading={busy}
              disabled={
                !revokeTarget ||
                (revokeMode === 'EMERGENCY_ABORT_STREAMS' && (revokeConfirmName !== revokeTarget.name || revokeReason.trim().length < 5))
              }
              onClick={() => void handleRevoke()}
            >
              {revokeMode === 'GRACEFUL_DRAIN' ? 'Revoke (graceful drain)' : 'Emergency revoke & abort'}
            </Button>
          </>
        }
      >
        <SelectField
          label="Revocation mode"
          value={revokeMode}
          onChange={next => setRevokeMode(next as typeof revokeMode)}
          options={[
            {
              value: 'GRACEFUL_DRAIN',
              label: 'Graceful drain (allow active streams to finish)',
              disabled: !canManage,
            },
            {
              value: 'EMERGENCY_ABORT_STREAMS',
              label: 'Emergency security revoke (abort active streams)',
              disabled: !canEmergencyRevoke,
            },
          ]}
        />
        {revokeMode === 'EMERGENCY_ABORT_STREAMS' ? (
          <>
            <Banner tone="danger">Emergency revocation terminates active streams immediately and is irreversible.</Banner>
            <TextField
              label={`Type "${revokeTarget?.name ?? ''}" to confirm`}
              value={revokeConfirmName}
              onChange={event => setRevokeConfirmName(event.target.value)}
            />
            <TextField
              label="Audit reason (minimum 5 characters)"
              value={revokeReason}
              onChange={event => setRevokeReason(event.target.value)}
            />
          </>
        ) : (
          <p className="tp-muted">Graceful drain blocks new requests while letting in-flight streams finish.</p>
        )}
      </Modal>

      <Drawer
        open={addMemberOpen}
        onClose={() => setAddMemberOpen(false)}
        title="Add organization member"
      >
        <form className="tp-stack" onSubmit={handleAddMember}>
          <TextField label="Member email" type="email" required value={memberEmail} onChange={event => setMemberEmail(event.target.value)} />
          <MultiSelectField
            label="Assign roles"
            placeholder="Select roles"
            hasClear
            value={memberRoles}
            options={allRoles.map(role => ({value: role, label: role.replaceAll('_', ' ')}))}
            onChange={next => setMemberRoles(next as Role[])}
          />
          <div className="tp-row" style={{justifyContent: 'flex-end'}}>
            <Button variant="secondary" onClick={() => setAddMemberOpen(false)}>Cancel</Button>
            <Button type="submit" variant="primary" loading={busy} disabled={!memberEmail.includes('@') || memberRoles.length === 0}>
              Add member
            </Button>
          </div>
        </form>
      </Drawer>

      <Modal
        open={editingMember !== null}
        onClose={() => setEditingMember(null)}
        title={`Edit roles for ${editingMember?.email ?? ''}`}
        footer={
          <>
            <Button variant="secondary" disabled={busy} onClick={() => setEditingMember(null)}>Cancel</Button>
            <Button variant="primary" loading={busy} disabled={draftRoles.length === 0} onClick={() => void handleSaveMemberRoles()}>
              Save roles
            </Button>
          </>
        }
      >
        {editingMember ? (
          <div className="tp-stack-sm">
            {editingMember.id === currentPrincipalId ? (
              <Banner tone="info">Self-service privilege grants are disabled. You cannot add new roles to your own account.</Banner>
            ) : null}
            <MultiSelectField
              label="Assigned roles"
              placeholder="Select roles"
              disabled={busy}
              value={draftRoles}
              options={allRoles.map(role => ({
                value: role,
                label: role.replaceAll('_', ' '),
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

function errText(cause: unknown): string { return cause instanceof Error ? cause.message : 'Request failed.'; }
