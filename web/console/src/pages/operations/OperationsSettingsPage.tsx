import {useEffect, useState} from 'react';
import {Button} from '@astryxdesign/core/Button';
import {Heading} from '@astryxdesign/core/Heading';
import {Section} from '@astryxdesign/core/Section';
import {Table} from '@astryxdesign/core/Table';
import {Text} from '@astryxdesign/core/Text';
import {TextInput} from '@astryxdesign/core/TextInput';
import {Token} from '@astryxdesign/core/Token';
import {HStack, VStack} from '@astryxdesign/core/Stack';
import {getClusterOpsStatus, getReadinessProbe, listAuditEvents, runUpgradePreflight, createEncryptedBackup} from '../../api/managementClient';
import {getExtensionLockfile} from '../../api/catalogClient';
import type {components} from '../../api/generated';

type Role = components['schemas']['RbacRole'];
type AuditEvent = components['schemas']['AuditEventResource'] & Record<string, unknown>;

export function OperationsSettingsPage({roles}: {roles: Role[]}) {
  const [readiness, setReadiness] = useState<Awaited<ReturnType<typeof getReadinessProbe>>|null>(null);
  const [cluster, setCluster] = useState<Awaited<ReturnType<typeof getClusterOpsStatus>>|null>(null);
  const [lockfile, setLockfile] = useState<unknown>(null);
  const [backups, setBackups] = useState<AuditEvent[]>([]);
  const [targetVersion, setTargetVersion] = useState('');
  const [preflight, setPreflight] = useState<Awaited<ReturnType<typeof runUpgradePreflight>>|null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const authorized = roles.includes('OPERATOR');

  async function refresh() {
    setLoading(true); setMessage('');
    const [readyResult, clusterResult, lockfileResult, backupResult] = await Promise.allSettled([
      getReadinessProbe(), getClusterOpsStatus(), getExtensionLockfile(), listAuditEvents('ops.backup_created', undefined, 'SYSTEM'),
    ]);
    const errors: string[] = [];
    if (readyResult.status === 'fulfilled') setReadiness(readyResult.value); else errors.push(`Readiness probe: ${errorMessage(readyResult.reason)}`);
    if (clusterResult.status === 'fulfilled') setCluster(clusterResult.value); else errors.push(`Cluster status: ${errorMessage(clusterResult.reason)}`);
    if (lockfileResult.status === 'fulfilled') setLockfile(lockfileResult.value); else errors.push(`Extension lockfile: ${errorMessage(lockfileResult.reason)}`);
    if (backupResult.status === 'fulfilled') setBackups(backupResult.value.data as AuditEvent[]); else errors.push(`Backup history: ${errorMessage(backupResult.reason)}`);
    setMessage(errors.join(' ')); setLoading(false);
  }
  useEffect(() => { if (authorized) void refresh(); }, [authorized]);

  async function backup() {
    setBusy(true); setMessage('');
    try { const receipt = await createEncryptedBackup(); setMessage(`Encrypted backup created: ${receipt.archive_filename} · SHA-256 ${receipt.sha256_hex}`); await refresh(); }
    catch (error) { setMessage(`Backup could not be created: ${errorMessage(error)}`); }
    finally { setBusy(false); }
  }

  async function checkUpgrade() {
    if (!targetVersion.trim()) return;
    setBusy(true); setMessage('');
    try { setPreflight(await runUpgradePreflight(targetVersion.trim())); }
    catch (error) { setMessage(`Upgrade preflight unavailable: ${errorMessage(error)}`); }
    finally { setBusy(false); }
  }

  if (!authorized) return <Text role="alert">Instance operations are available to the OPERATOR role only.</Text>;
  const nodes = (cluster?.nodes ?? []) as Array<Record<string, unknown>>;
  const walValues = nodes.map(node => findWalSize(node)).filter((value): value is number => value !== null);
  return <VStack gap={4}>
    <Heading level={1}>Instance operations</Heading>
    {message ? <Text role="status">{message}</Text> : null}
    <Section><VStack gap={3}><Heading level={2}>Readiness and storage</Heading>
      {loading ? <Text role="status">Loading operations status…</Text> : <>
        <HStack gap={2} wrap="wrap"><Token label={readiness?.ready ? 'Ready' : `Not ready: ${readiness?.status ?? 'unavailable'}`} color={readiness?.ready ? 'green' : 'orange'} icon={readiness?.ready ? 'checkDouble' : 'warning'} />
          <Text>Config epoch: {readiness?.configEpoch ?? cluster?.current_epoch ?? 'Unavailable'}</Text>
          <Text>Snapshot age: {readiness?.snapshotAgeMs === undefined ? 'Unavailable' : `${readiness.snapshotAgeMs} ms`}</Text>
          <Text>Storage engine: {cluster?.storage_engine ?? 'Unavailable'}</Text></HStack>
        {walValues.length ? walValues.map((value, index) => <Text key={`wal-${index}`}>Node {index + 1} WAL disk footprint: {formatBytes(value)}</Text>) : <Text>WAL disk footprint is not provided by the current cluster-status response.</Text>}
        <Table aria-label="Cluster nodes" data={nodes} columns={[
          {key: 'id', header: 'Node'}, {key: 'config_epoch', header: 'Config epoch'}, {key: 'snapshot_age_ms', header: 'Snapshot age (ms)'},
          {key: 'health', header: 'Health'},
        ]} />
      </>}
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>Encrypted backups</Heading>
      <Text>Backups are sealed as .tpop.enc archives. History is read from immutable audit events.</Text>
      {cluster?.backup_supported === false ? <Text role="status">{cluster.backup_unavailable_reason || 'Encrypted backups require Linux volatile staging support.'}</Text> : null}
      <Button label="Create encrypted backup" variant="primary" isDisabled={busy || cluster?.backup_supported !== true} onClick={() => void backup()} />
      {backups.length ? <Table aria-label="Backup history" data={backups as Array<Record<string, unknown>>} columns={[
        {key: 'created_at', header: 'Created'}, {key: 'actor_id', header: 'Created by'}, {key: 'target_id', header: 'Backup ID'},
        {key: 'metadata', header: 'Receipt', renderCell: row => <Text>{JSON.stringify(row.metadata ?? {})}</Text>},
      ]} /> : <Text role="status">{loading ? 'Loading backup history…' : 'No backup events were found.'}</Text>}
    </VStack></Section>
    <Section><VStack gap={3}><Heading level={2}>Extension pins and upgrade readiness</Heading>
      <Text>Current tanpopo.lock pins:</Text><pre>{renderLockfile(lockfile)}</pre>
      <HStack gap={2} wrap="wrap"><TextInput label="Target core version" value={targetVersion} onChange={setTargetVersion} /><Button label="Run upgrade preflight" variant="secondary" isDisabled={busy || !targetVersion.trim()} onClick={() => void checkUpgrade()} /></HStack>
      {preflight ? <><Token label={preflight.compatible ? 'Ready to upgrade' : 'Upgrade blocked'} color={preflight.compatible ? 'green' : 'red'} icon={preflight.compatible ? 'checkDouble' : 'warning'} />
        <Text>Preflight state: {preflight.state}</Text>{preflight.blocking_extensions.map((extension, index) => <Text key={`blocked-${index}`}>{JSON.stringify(extension)}</Text>)}</> : null}
    </VStack></Section>
  </VStack>;
}

function findWalSize(node: Record<string, unknown>): number|null {
  for (const [key, value] of Object.entries(node)) if (/wal.*(bytes|size)|(bytes|size).*wal/i.test(key) && typeof value === 'number' && Number.isFinite(value)) return value;
  return null;
}
function formatBytes(value: number) { return `${value.toLocaleString('en-US')} bytes`; }
function renderLockfile(value: unknown) { return value === null ? 'Lockfile data unavailable.' : typeof value === 'string' ? value : JSON.stringify(value, null, 2); }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : 'The request could not be completed.'; }
