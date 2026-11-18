import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import {Theme} from '@astryxdesign/core/theme';
import {neutralTheme} from '@astryxdesign/theme-neutral/built';
import '@astryxdesign/theme-neutral/theme.css';
import axe from 'axe-core';
import type {components} from '../api/generated';
import {TracesExplorerPage} from './traces/TracesExplorerPage';
import {FinOpsBudgetsPage, formatMicroUSD} from './finops/FinOpsBudgetsPage';
import {SecurityAuditAndKillSwitchPage} from './security/SecurityAuditAndKillSwitchPage';
import {OperationsSettingsPage} from './operations/OperationsSettingsPage';

const mocks = vi.hoisted(() => ({
  listTracePage: vi.fn(), getTraceWaterfall: vi.fn(), listBudgets: vi.fn(), getUsageChargebackSummary: vi.fn(),
  exportUsageChargeback: vi.fn(), updateBudget: vi.fn(), listAuditEvents: vi.fn(), verifyAuditHashChain: vi.fn(),
  triggerKillSwitch: vi.fn(), listKillSwitches: vi.fn(), clearKillSwitch: vi.fn(), getKillSwitchBlastRadius: vi.fn(), getReadinessProbe: vi.fn(),
  getClusterOpsStatus: vi.fn(), createEncryptedBackup: vi.fn(), runUpgradePreflight: vi.fn(), getExtensionLockfile: vi.fn(),
}));

vi.mock('../api/managementClient', () => mocks);
vi.mock('../api/catalogClient', () => ({getExtensionLockfile: mocks.getExtensionLockfile}));

afterEach(() => { cleanup(); vi.clearAllMocks(); Object.defineProperty(window, 'innerWidth', {configurable: true, value: 1024}); });
beforeEach(() => {
  mocks.listTracePage.mockResolvedValue({data: [], nextCursor: undefined}); mocks.getTraceWaterfall.mockResolvedValue(null);
  mocks.listBudgets.mockResolvedValue([]); mocks.getUsageChargebackSummary.mockResolvedValue([]);
  mocks.exportUsageChargeback.mockResolvedValue(new Blob(['report'])); mocks.updateBudget.mockResolvedValue({});
  mocks.listAuditEvents.mockResolvedValue({data: [], nextCursor: undefined}); mocks.verifyAuditHashChain.mockResolvedValue({intact: true, checked_events_count: 0});
  mocks.listKillSwitches.mockResolvedValue([]); mocks.clearKillSwitch.mockResolvedValue({});
  mocks.triggerKillSwitch.mockResolvedValue({config_epoch: 3, affected_entities_count: 2, severed_streams_count: 1});
  mocks.getKillSwitchBlastRadius.mockResolvedValue({organizations: [], projects: [], virtual_keys: [], connections: []});
  mocks.getReadinessProbe.mockResolvedValue({ready: true, status: 'ok', configEpoch: 3, snapshotAgeMs: 5});
  mocks.getClusterOpsStatus.mockResolvedValue({current_epoch: 3, storage_engine: 'sqlite_wal', backup_supported: true, backup_unavailable_reason: '', nodes: [{id: 'standalone', wal_bytes: 8192}]});
  mocks.createEncryptedBackup.mockResolvedValue({archive_filename: 'backup.tpop.enc', sha256_hex: 'a'.repeat(64)});
  mocks.runUpgradePreflight.mockResolvedValue({compatible: true, state: 'READY_TO_UPGRADE', blocking_extensions: []});
  mocks.getExtensionLockfile.mockResolvedValue({lockfileVersion: 1, extensions: {'@tanpopo/adapter': {version: '1.2.0', digest: 'sha256:abc'}}});
});

describe('Unit 30 observability and operations screens', () => {
  it('searches trace IDs and renders injected trace values as inert text', async () => {
    const payload = '<img src=x onerror=alert(1)>';
    const trace = {request_id: 'req-1', plane: 'PROVIDER', route_slug: 'safe-route', status_code: 502, error_code: payload,
      usage_accuracy: 'ESTIMATED', total_cost_micros: 3, duration_ms: 42, has_debug_payload: false, created_at: '2026-11-17T00:00:00Z',
      waterfall_stages: [{stage: 'Route', duration_ms: 4, outcome: 'failed'}]} as components['schemas']['TraceSummary'];
    mocks.listTracePage.mockResolvedValue({data: [trace], nextCursor: undefined}); mocks.getTraceWaterfall.mockResolvedValue(trace);
    renderTheme(<TracesExplorerPage environmentId="env-1" roles={['DEVELOPER']} />, 'light');
    expect(await screen.findByText('Metadata-Only (0 prompt bytes stored)')).toBeInTheDocument();
    expect(await screen.findByText(payload)).toBeInTheDocument();
    expect(document.querySelector('img')).toBeNull();
    fireEvent.change(screen.getByLabelText('Request ID'), {target: {value: 'req-1'}});
    fireEvent.click(screen.getByRole('button', {name: 'Search traces'}));
    expect(await screen.findByText('Request waterfall: req-1')).toBeInTheDocument();
    expect(await screen.findAllByText('Route')).toHaveLength(2);
    expect(await axeViolations()).toEqual([]);
  });

  it('limits FINOPS_VIEWER to the FinOps view and keeps its budget controls read-only', async () => {
    const budget = {id: 'budget-1', scope_tier: 'PROJECT', scope_id: 'project-1', period: 'MONTHLY', hard_limit_micros: 1500000,
      settled_exact_micros: 1000001, settled_estimated_micros: 20, reserved_hold_micros: 50000, negative_carryover_micros: 0, version: 1} as components['schemas']['BudgetResource'];
    mocks.listBudgets.mockResolvedValue([budget]);
    mocks.getUsageChargebackSummary.mockResolvedValue([{group_key: 'project-1', exact_micro_usd: 1000001, estimated_micro_usd: 20, unmetered_request_count: 2}]);
    renderTheme(<FinOpsBudgetsPage projectId="project-1" roles={['FINOPS_VIEWER']} />, 'dark');
    expect(await screen.findAllByText('$1.000001')).toHaveLength(2);
    expect(screen.getByText('EXACT')).toBeInTheDocument(); expect(screen.getByText('ESTIMATED')).toBeInTheDocument();
    expect(screen.getByText('UNMETERED_FLAGGED')).toBeInTheDocument(); expect(screen.getByText('Active Hold')).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Save limit'})).toHaveAttribute('aria-disabled', 'true');
    expect(await axeViolations()).toEqual([]);
    expect(formatMicroUSD(1)).toBe('$0.000001'); expect(formatMicroUSD(1000001)).toBe('$1.000001');
    expect(formatMicroUSD(Number.MAX_SAFE_INTEGER + 1)).toContain('unsafe integer');
  });

  it('confirms emergency actions and verifies immutable audit records without interpreting payload HTML', async () => {
    const event = {id: 'audit-1', chain_partition: 'org-1', seq_no: 7, actor_id: 'operator-1', action: 'killswitch.triggered',
      metadata: {detail: '<script>alert(1)</script>'}, prev_hash: '0'.repeat(64), event_hash: 'f'.repeat(64), created_at: '2026-11-17T00:00:00Z'};
    mocks.listAuditEvents.mockResolvedValue({data: [event], nextCursor: undefined});
    renderTheme(<SecurityAuditAndKillSwitchPage roles={['OPERATOR']} />, 'light');
    expect(await screen.findByText('killswitch.triggered')).toBeInTheDocument();
    expect(document.querySelector('script')).toBeNull();
    fireEvent.click(screen.getByRole('button', {name: 'Verify hash chain'}));
    expect(await screen.findByText('Integrity verified across 0 audit records.')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Target ID'), {target: {value: 'project-id'}});
    fireEvent.change(screen.getByLabelText('Reason (at least 5 characters)'), {target: {value: 'incident response'}});
    fireEvent.change(screen.getByLabelText('Project confirmation slug'), {target: {value: 'project-slug'}});
    fireEvent.click(screen.getByRole('button', {name: 'Review emergency kill switch'}));
    fireEvent.click(screen.getByRole('button', {name: 'Trigger kill switch'}));
    await waitFor(() => expect(mocks.triggerKillSwitch).toHaveBeenCalledWith(expect.objectContaining({target_level: 'PROJECT', target_id: 'project-id', confirm_slug: 'project-slug', reason: 'incident response'})));
    expect(await axeViolations()).toEqual([]);
  });

  it('shows readiness, backup history, lockfile pins and upgrade preflight to operators', async () => {
    renderTheme(<OperationsSettingsPage roles={['OPERATOR']} />, 'dark');
    expect(await screen.findByText('Ready')).toBeInTheDocument();
    expect(screen.getByText(/@tanpopo\/adapter/)).toBeInTheDocument();
    expect(await screen.findByText('Node 1 WAL disk footprint: 8,192 bytes')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Target core version'), {target: {value: 'v0.2.0'}});
    fireEvent.click(screen.getByRole('button', {name: 'Run upgrade preflight'}));
    await waitFor(() => expect(mocks.runUpgradePreflight).toHaveBeenCalledWith('v0.2.0'));
    expect(await screen.findByText('Ready to upgrade')).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it('explains and disables encrypted backups when secure staging is unavailable', async () => {
    mocks.getClusterOpsStatus.mockResolvedValue({current_epoch: 3, storage_engine: 'sqlite_wal', backup_supported: false,
      backup_unavailable_reason: 'Encrypted backups require Linux /dev/shm tmpfs staging with swap disabled.',
      nodes: [{id: 'standalone', wal_bytes: 8192}]});
    renderTheme(<OperationsSettingsPage roles={['OPERATOR']} />, 'dark');
    expect(await screen.findByText('Encrypted backups require Linux /dev/shm tmpfs staging with swap disabled.')).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Create encrypted backup'})).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByRole('button', {name: 'Create encrypted backup'}));
    expect(mocks.createEncryptedBackup).not.toHaveBeenCalled();
    expect(await axeViolations()).toEqual([]);
  });

  it('renders a 200-row trace result set in responsive chunks', async () => {
    mocks.listTracePage.mockResolvedValue({data: Array.from({length: 200}, (_, index) => ({request_id: `req-${index}`, plane: 'PROVIDER', route_slug: 'route',
      status_code: 200, error_code: null, usage_accuracy: 'EXACT', total_cost_micros: 0, duration_ms: 1, has_debug_payload: false, created_at: '2026-11-17T00:00:00Z'})), nextCursor: undefined});
    renderTheme(<TracesExplorerPage environmentId="env-1" roles={['DEVELOPER']} />, 'dark');
    expect(await screen.findByText('Showing 10 of 200 matching trace records.')).toBeInTheDocument();
    for (let i = 0; i < 19; i += 1) fireEvent.click(screen.getByRole('button', {name: 'Show more traces'}));
    expect(await screen.findByText('req-199')).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it('follows trace and audit cursors when the operator loads more records', async () => {
    mocks.listTracePage.mockResolvedValueOnce({data: [{request_id: 'req-new', plane: 'PROVIDER', route_slug: 'route', status_code: 200,
      error_code: 'safe', usage_accuracy: 'EXACT', total_cost_micros: 0, duration_ms: 1, has_debug_payload: false, created_at: '2026-11-17T00:00:00Z'}], nextCursor: 'trace-cursor'})
      .mockResolvedValueOnce({data: [{request_id: 'req-old', plane: 'PROVIDER', route_slug: 'route', status_code: 500,
      error_code: 'older_failure', usage_accuracy: 'EXACT', total_cost_micros: 0, duration_ms: 1, has_debug_payload: false, created_at: '2026-11-16T00:00:00Z'}], nextCursor: undefined});
    renderTheme(<TracesExplorerPage environmentId="env-1" roles={['DEVELOPER']} />, 'light');
    expect(await screen.findByText('req-new')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Error code'), {target: {value: 'older_failure'}});
    fireEvent.click(screen.getByRole('button', {name: 'Search traces'}));
    expect(screen.getByText('No trace records match these filters.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', {name: 'Load more traces'}));
    expect(await screen.findByText('req-old')).toBeInTheDocument();
    expect(mocks.listTracePage).toHaveBeenLastCalledWith('env-1', '', 'trace-cursor');
    cleanup();

    mocks.listAuditEvents.mockResolvedValueOnce({data: [], nextCursor: 'audit-cursor'})
      .mockResolvedValueOnce({data: [{id: 'audit-old', chain_partition: 'org-1', seq_no: 8, actor_id: 'operator-1', action: 'older.action',
        metadata: {}, prev_hash: '0'.repeat(64), event_hash: 'f'.repeat(64), created_at: '2026-11-16T00:00:00Z'}], nextCursor: undefined});
    renderTheme(<SecurityAuditAndKillSwitchPage roles={['OPERATOR']} />, 'light');
    fireEvent.click(await screen.findByRole('button', {name: 'Load more audit records'}));
    expect(await screen.findByText('older.action')).toBeInTheDocument();
    expect(mocks.listAuditEvents).toHaveBeenLastCalledWith('', 'audit-cursor');
    expect(await axeViolations()).toEqual([]);
  });

  it('keeps all four screens accessible in both light and dark themes', async () => {
    for (const mode of ['light', 'dark'] as const) {
      renderTheme(<TracesExplorerPage environmentId="env-1" roles={['DEVELOPER']} />, mode);
      expect(await screen.findByText('No trace records match these filters.')).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]); cleanup();

      renderTheme(<FinOpsBudgetsPage projectId="project-1" roles={['FINOPS_VIEWER']} />, mode);
      expect(await screen.findByText('No budget records are available.')).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]); cleanup();

      renderTheme(<SecurityAuditAndKillSwitchPage roles={['SECURITY_REVIEWER']} />, mode);
      expect(await screen.findByRole('heading', {name: 'Immutable audit log'})).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]); cleanup();

      renderTheme(<OperationsSettingsPage roles={['OPERATOR']} />, mode);
      expect(await screen.findByText('Ready')).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]); cleanup();
    }
  });

  it('keeps the emergency kill-switch controls available at a 375px mobile viewport', async () => {
    Object.defineProperty(window, 'innerWidth', {configurable: true, value: 375});
    renderTheme(<SecurityAuditAndKillSwitchPage roles={['OPERATOR']} />, 'light');
    expect(await screen.findByRole('heading', {name: 'Emergency kill switch'})).toBeInTheDocument();
    expect(screen.getByLabelText('Target ID')).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Review emergency kill switch'})).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it('previews the four-level blast radius and clears an active kill switch with an audited reason', async () => {
    mocks.listKillSwitches.mockResolvedValue([{id: 'switch-1', org_id: 'org-1', level: 'PROJECT_OR_ORG_FREEZE', target_id: 'project-1', active: true,
      abort_active_streams: true, reason: 'incident', triggered_by_principal_id: 'operator', version: 2, triggered_at_ms: 10}]);
    renderTheme(<SecurityAuditAndKillSwitchPage roles={['OPERATOR']} />, 'light');
    expect(await screen.findByText('PROJECT_OR_ORG_FREEZE')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Target ID'), {target: {value: 'project-1'}});
    fireEvent.click(screen.getByRole('button', {name: 'Preview blast radius'}));
    await waitFor(() => expect(mocks.getKillSwitchBlastRadius).toHaveBeenCalledWith('PROJECT', 'project-1'));
    fireEvent.click(screen.getByRole('button', {name: 'Clear'}));
    fireEvent.change(screen.getByLabelText('Reason for clearing (at least 5 characters)'), {target: {value: 'incident resolved'}});
    fireEvent.click(screen.getByRole('button', {name: 'Review kill switch clearing'}));
    fireEvent.click(screen.getByRole('button', {name: 'Clear kill switch'}));
    await waitFor(() => expect(mocks.clearKillSwitch).toHaveBeenCalledWith(expect.objectContaining({id: 'switch-1', version: 2}), 'incident resolved'));
    expect(await axeViolations()).toEqual([]);
  });
});

function renderTheme(node: React.ReactNode, mode: 'light'|'dark') { return render(<Theme theme={neutralTheme} mode={mode}><main>{node}</main></Theme>); }
async function axeViolations() { const result = await axe.run(document.body); return result.violations.map(item => item.id); }
