import {Icon} from '@astryxdesign/core/Icon';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {Token} from '@astryxdesign/core/Token';
import type {components} from '../../api/generated';

type PermissionDiff = components['schemas']['ExtensionPermissionDiff'];
type Change = 'Added' | 'Removed' | 'Expanded' | 'Changed' | 'Reduced';
type PermissionRow = {permission: string; active: string; candidate: string; change?: Change};

export function PermissionDiffTable({diff}: {diff: PermissionDiff}) {
  const active = diff.active_permissions;
  const candidate = diff.candidate_permissions;
  const delta = diff.permission_delta;
  const rows: PermissionRow[] = [
    ...setRows('Allowed host', active?.allowed_hosts ?? [], candidate.allowed_hosts ?? [], delta.added_hosts ?? []),
    ...setRows('Secret slot', active?.secret_slots ?? [], candidate.secret_slots ?? [], delta.added_secret_slots ?? []),
    ...setRows('Hook', active?.hooks ?? [], candidate.hooks ?? [], delta.added_hooks ?? []),
    ...bindingRows(active?.secret_binding_modes ?? {}, candidate.secret_binding_modes ?? {}, delta.binding_mode_changed === true),
    {
      permission: 'Execution tier', active: active?.execution_tier ?? 'No active version', candidate: candidate.execution_tier,
      change: delta.tier_escalated ? 'Expanded' : active?.execution_tier !== candidate.execution_tier ? 'Changed' : undefined,
    },
    booleanRow('Private IP access', active?.allow_private_ip, candidate.allow_private_ip, delta.private_ip_escalated === true, false),
    booleanRow('Require TLS', active?.require_tls, candidate.require_tls, delta.tls_downgraded === true, true),
    booleanRow('Inspect request body', active?.inspect_request_body, candidate.inspect_request_body, delta.payload_access_escalated === true && candidate.inspect_request_body === true, false),
    booleanRow('Inspect response body', active?.inspect_response_body, candidate.inspect_response_body, delta.payload_access_escalated === true && candidate.inspect_response_body === true, false),
    {
      permission: 'Resource limits',
      active: active ? formatQuotas(active) : 'No active version',
      candidate: formatQuotas(candidate),
      change: delta.memory_increased || delta.cpu_quota_increased ? 'Expanded' : active && formatQuotas(active) !== formatQuotas(candidate) ? 'Reduced' : undefined,
    },
  ];

  return <>
    <Text>Compare the complete active and candidate permission sets. Each added or reduced item is marked in its row.</Text>
    <Table data={rows} density="compact" dividers="rows" columns={[
      {key: 'permission', header: 'Permission'},
      {key: 'active', header: `Active ${diff.active_version ?? 'version'}`},
      {key: 'candidate', header: `Candidate ${diff.candidate_version}`, renderCell: row => <>
        <Text>{row.candidate}</Text>
        {row.change ? <Token label={row.change} color={row.change === 'Removed' || row.change === 'Reduced' ? 'teal' : 'orange'} icon={<Icon icon={row.change === 'Removed' || row.change === 'Reduced' ? 'check' : 'warning'} size="sm" />} /> : null}
      </>},
    ]} />
  </>;
}

function setRows(label: string, active: string[], candidate: string[], added: string[]): PermissionRow[] {
  const current = new Set(active);
  const proposed = new Set(candidate);
  const additions = new Set(added);
  const items = [...new Set([...active, ...candidate])].sort();
  return items.map(item => ({
    permission: `${label}: ${item}`,
    active: current.has(item) ? item : '—',
    candidate: proposed.has(item) ? item : '—',
    change: additions.has(item) || (!current.has(item) && proposed.has(item)) ? 'Added' : current.has(item) && !proposed.has(item) ? 'Removed' : undefined,
  }));
}

function bindingRows(active: Record<string, string>, candidate: Record<string, string>, escalated: boolean): PermissionRow[] {
  const slots = [...new Set([...Object.keys(active), ...Object.keys(candidate)])].sort();
  return slots.map(slot => {
    const oldMode = active[slot];
    const newMode = candidate[slot];
    return {
      permission: `Secret binding mode: ${slot}`,
      active: oldMode ?? 'None',
      candidate: newMode ?? 'None',
      change: oldMode !== newMode ? escalated ? 'Changed' : newMode ? 'Changed' : 'Reduced' : undefined,
    };
  });
}

function booleanRow(label: string, active: boolean | undefined, candidate: boolean | undefined, escalated: boolean, reverse: boolean): PermissionRow {
  const oldValue = active === undefined ? 'No active version' : active ? 'Enabled' : 'Disabled';
  const newValue = candidate ? 'Enabled' : 'Disabled';
  const changed = active !== undefined && active !== candidate;
  const isExpansion = active === undefined ? !reverse && candidate === true : reverse ? active === true && candidate === false : active === false && candidate === true;
  return {permission: label, active: oldValue, candidate: newValue, change: escalated && isExpansion ? 'Expanded' : changed ? isExpansion ? 'Expanded' : 'Reduced' : undefined};
}

function formatQuotas(permissions: components['schemas']['ExtensionPermissionSet']) {
  const value = (amount: number | undefined, unit: string) => amount === undefined || amount === 0 ? 'Runtime default' : `${amount} ${unit}`;
  return `Memory: ${value(permissions.max_memory_mib, 'MiB')}; CPU per call: ${value(permissions.max_cpu_ms_per_call, 'ms')}; CPU per stream chunk: ${value(permissions.max_stream_chunk_cpu_ms, 'ms')}`;
}
