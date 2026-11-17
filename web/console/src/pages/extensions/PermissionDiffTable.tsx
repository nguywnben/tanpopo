import {Icon} from '@astryxdesign/core/Icon';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {Token} from '@astryxdesign/core/Token';
import type {components} from '../../api/generated';

type PermissionDiff = components['schemas']['ExtensionPermissionDiff'];
type PermissionRow = {permission: string; active: string; candidate: string; expanded: boolean};

export function PermissionDiffTable({diff}: {diff: PermissionDiff}) {
  const activeQuotas = diff.active_quotas;
  const rows: PermissionRow[] = [
    {
      permission: 'Allowed hosts',
      active: diff.active_allowed_hosts.join(', ') || 'None',
      candidate: diff.candidate_allowed_hosts.join(', ') || 'None',
      expanded: diff.added_allowed_hosts.length > 0,
    },
    {
      permission: 'Secret slots',
      active: diff.active_secret_slots.join(', ') || 'None',
      candidate: diff.secret_slots.join(', ') || 'None',
      expanded: diff.added_secret_slots.length > 0,
    },
    {
      permission: 'Execution tier',
      active: diff.active_execution_tier ?? 'No active version',
      candidate: diff.execution_tier,
      expanded: tierRank(diff.execution_tier) > tierRank(diff.active_execution_tier ?? 'declarative'),
    },
    {
      permission: 'Resource limits',
      active: activeQuotas ? formatQuotas(activeQuotas) : 'No active version',
      candidate: formatQuotas(diff.quotas),
      expanded: diff.quota_escalated,
    },
  ];

  return <>
    <Text>Compare the active and candidate permission sets. Added permissions and increased resource limits are highlighted.</Text>
    <Table data={rows} density="compact" dividers="rows" columns={[
      {key: 'permission', header: 'Permission'},
      {key: 'active', header: `Active ${diff.active_version ?? 'version'}`},
      {key: 'candidate', header: `Candidate ${diff.candidate_version}`, renderCell: row => <>
        <Text>{row.candidate}</Text>
        {row.expanded ? <Token label={row.permission === 'Resource limits' ? 'Increased' : 'Added'} color="orange" icon={<Icon icon="warning" size="sm" />} /> : null}
      </>},
    ]} />
  </>;
}

function formatQuotas(quotas: components['schemas']['ExtensionQuotas']) {
  const value = (amount: number, unit: string) => amount === 0 ? 'Runtime default' : `${amount} ${unit}`;
  return `Memory: ${value(quotas.max_memory_mib, 'MiB')}; CPU per call: ${value(quotas.max_cpu_ms_per_call, 'ms')}; CPU per stream chunk: ${value(quotas.max_stream_chunk_cpu_ms, 'ms')}`;
}

function tierRank(tier: string) {
  if (tier === 'declarative') return 0;
  if (tier === 'sandboxed_wasm') return 1;
  return tier === 'isolated_sidecar' ? 2 : -1;
}
