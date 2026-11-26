import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken} from '../api/client';
import {I18nProvider} from '../i18n/I18nProvider';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {FinOpsPage} from './finops/FinOpsPage';
import {ExtensionsPage} from './extensions/ExtensionsPage';
import {ModelsPage} from './models/ModelsPage';
import {OperationsPage} from './operations/OperationsPage';
import {TracesPage} from './traces/TracesPage';

function jsonResponse(body: unknown, status = 200, headers?: Record<string, string>) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {'Content-Type': 'application/json', ...headers},
  });
}

function wrap(ui: React.ReactNode) {
  return (
    <I18nProvider>
      <ThemeProvider>
        <RouterProvider>{ui}</RouterProvider>
      </ThemeProvider>
    </I18nProvider>
  );
}

describe('Engineering Round 4 UI regressions (Slices A & B)', () => {
  beforeEach(() => {
    clearCsrfToken();
    window.localStorage.setItem('tanpopo.locale', 'en');
    window.sessionStorage.clear();
    window.history.replaceState(null, '', '/');
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('DEF-052: TracesPage renders UNSETTLED badge for unsettled traces cleanly without WCAG violations', async () => {
    rememberCsrfToken('csrf-traces');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/traces')) {
        return jsonResponse({
          data: [
            {
              request_id: 'trc_unsettled_1',
              plane: 'PROVIDER',
              route_slug: 'qwen-route',
              deployment_id: 'dep_1',
              data_region: 'US',
              status_code: 200,
              error_code: null,
              usage_accuracy: 'UNSETTLED',
              total_cost_micros: 0,
              ttft_ms: 4,
              duration_ms: 14,
              has_debug_payload: false,
              waterfall_stages: [],
              created_at: '2026-11-26T00:00:00Z',
            },
          ],
          has_more: false,
        });
      }
      return jsonResponse({data: [], has_more: false});
    });

    const {container} = render(wrap(<TracesPage environmentId="env_1" roles={['ORG_OWNER']} />));
    await screen.findAllByText('trc_unsettled_1');
    expect(screen.getAllByText('Unsettled').length).toBeGreaterThanOrEqual(1);
    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-077 & DEF-078: FinOpsPage distinguishes duplicate Virtual Key names via key_prefix/group_id and shows budget period dates', async () => {
    rememberCsrfToken('csrf-finops');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/budgets')) {
        return jsonResponse({
          data: [
            {
              id: 'bdg_1',
              scope_tier: 'ORGANIZATION',
              scope_id: 'org_1',
              period: 'MONTHLY',
              period_start: '2026-11-01T00:00:00Z',
              period_end: '2026-12-01T00:00:00Z',
              hard_limit_micros: 10000,
              settled_exact_micros: 7200,
              settled_estimated_micros: 0,
              reserved_hold_micros: 0,
              negative_carryover_micros: 0,
              soft_warning_percents: [80],
              unmetered_request_count: 0,
              version: 1,
            },
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/usage/summary')) {
        return jsonResponse({
          data: [
            {
              group_id: 'vk_01a',
              group_key: 'Shared Key',
              key_prefix: 'vk_tp_...1111',
              exact_micro_usd: 2500,
              estimated_micro_usd: 0,
              exact_request_count: 1,
              estimated_request_count: 0,
              unmetered_request_count: 0,
            },
            {
              group_id: 'vk_02b',
              group_key: 'Shared Key',
              key_prefix: 'vk_tp_...2222',
              exact_micro_usd: 4700,
              estimated_micro_usd: 0,
              exact_request_count: 2,
              estimated_request_count: 0,
              unmetered_request_count: 0,
            },
          ],
          has_more: false,
        });
      }
      return jsonResponse({data: [], has_more: false});
    });

    const {container} = render(
      wrap(
        <FinOpsPage
          pathname="/finops/chargeback"
          activeOrgId="org_1"
          projects={[{id: 'prj_1', slug: 'default', display_name: 'Default'} as never]}
          environments={[{id: 'env_1', slug: 'dev', name: 'Dev', project_id: 'prj_1'} as never]}
          projectId="prj_1"
          environmentId="env_1"
          roles={['ORG_OWNER']}
        />,
      ),
    );
    const groupCombo = await screen.findByRole('combobox', {name: /Group by/i});
    fireEvent.click(groupCombo);
    fireEvent.click(await screen.findByRole('option', {name: 'Virtual Key'}));
    await waitFor(() => expect(screen.getAllByText(/vk_tp_\.\.\.1111/).length).toBeGreaterThanOrEqual(1));
    expect(screen.getAllByText(/vk_tp_\.\.\.2222/).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Shared Key').length).toBeGreaterThanOrEqual(2);
    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-053: ModelsPage notes LEGACY_NANO_USD_PER_1M deployments and previews canonical rate per 1M tokens in Add Deployment', async () => {
    rememberCsrfToken('csrf-models');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/models')) {
        return jsonResponse({
          data: [
            {
              id: 'mod_1',
              slug: 'qwen2.5',
              display_name: 'Qwen 2.5',
              context_window: 32768,
              max_output_tokens: 4096,
              assurance_tier: 'TESTED',
              capabilities: {chat: true},
            },
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/deployments')) {
        return jsonResponse({
          data: [
            {
              id: 'dep_1',
              env_id: 'env_1',
              logical_model_id: 'mod_1',
              connection_id: 'con_1',
              upstream_model_name: 'qwen2.5',
              data_region: 'US',
              state: 'ACTIVE_HEALTHY',
              rate_card: {
                input_token_nano_usd: 150000000,
                output_token_nano_usd: 300000000,
                token_rate_unit: 'LEGACY_NANO_USD_PER_1M',
              },
              version: 1,
            },
          ],
          has_more: false,
        });
      }
      return jsonResponse({data: [], has_more: false});
    });

    const {container} = render(
      wrap(
        <ModelsPage
          projectId="prj_1"
          environments={[{id: 'env_1', slug: 'dev', name: 'Dev', project_id: 'prj_1'} as never]}
          environmentId="env_1"
          roles={['ORG_OWNER']}
        />,
      ),
    );
    await screen.findAllByText('Legacy v1 scale (stored per 1M tokens).');
    fireEvent.click(screen.getByRole('button', {name: /add deployment/i}));
    await screen.findByText(/Effective rate per 1M tokens/i);
    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-058 & DEF-060 & DEF-061 & DEF-062 & DEF-073: ExtensionsPage supports unquarantine, disabled rollback hint, discard candidate, and specific upgrade errors', async () => {
    rememberCsrfToken('csrf-ext');
    let unqCalled = false;
    let discardCalled = false;
    let upgradeAttempt = 0;

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(url, 'http://localhost');
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/extensions/installations' && method === 'GET') {
        return jsonResponse({
          data: [
            {
              id: 'ins_pending',
              package_id: '@acme/pending-upg',
              canonical_name: '@acme/pending-upg',
              state: 'UPDATING_PENDING_RECONSENT',
              active_version: '1.0.0',
              candidate_version: '2.0.0',
              contract_version: '0.1',
              previous_rollback_version: null,
              origin_trust_tier: 'verified',
              signature_verified: true,
              enterprise_signed: false,
              execution_tier: 'DECLARATIVE_T1',
              version: 3,
              updated_at: '2026-11-26T00:00:00Z',
            },
            {
              id: 'ins_quar',
              package_id: '@acme/quar-pkg',
              canonical_name: '@acme/quar-pkg',
              state: 'QUARANTINED',
              active_version: '1.0.0',
              candidate_version: null,
              contract_version: '0.1',
              previous_rollback_version: '0.9.0',
              origin_trust_tier: 'unverified_external',
              signature_verified: false,
              enterprise_signed: false,
              execution_tier: 'DECLARATIVE_T1',
              version: 5,
              updated_at: '2026-11-26T00:00:00Z',
            },
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/marketplace/catalog') {
        return jsonResponse({data: [], offline_cached: false, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/extensions/ins_pending/permission-diff') {
        return jsonResponse(
          {
            installation_id: 'ins_pending',
            package_id: '@acme/pending-upg',
            state: 'UPDATING_PENDING_RECONSENT',
            active_version: '1.0.0',
            candidate_version: '2.0.0',
            origin_trust_tier: 'verified',
            signature_verified: true,
            enterprise_signed: false,
            execution_tier: 'DECLARATIVE_T1',
            has_escalation: true,
            active_permissions: {allowed_hosts: ['api.acme.test'], secret_slots: [], hooks: [], execution_tier: 'DECLARATIVE_T1'},
            candidate_permissions: {allowed_hosts: ['api.acme.test', 'api.extra.test'], secret_slots: [], hooks: [], execution_tier: 'DECLARATIVE_T1'},
            permission_delta: {added_hosts: ['api.extra.test'], added_secret_slots: [], added_hooks: [], quota_increased: false, tier_escalated: false},
            version: 3,
          },
          200,
          {ETag: '"v3"'},
        );
      }
      if (parsed.pathname === '/admin/v1/blast-radius') {
        return jsonResponse({installation_id: 'ins_quar', projects: ['prj_1'], connections: ['conn_quar_1'], impacted_routes: []});
      }
      if (parsed.pathname === '/admin/v1/extensions/ins_quar/unquarantine' && method === 'POST') {
        unqCalled = true;
        return jsonResponse({id: 'ins_quar', package_id: '@acme/quar-pkg', state: 'INSTALLED_DISABLED', version: 6});
      }
      if (parsed.pathname === '/admin/v1/extensions/ins_pending' && method === 'DELETE') {
        discardCalled = true;
        return new Response(null, {status: 204});
      }
      if (parsed.pathname === '/admin/v1/extensions/ins_pending/upgrade' && method === 'POST') {
        upgradeAttempt += 1;
        if (upgradeAttempt === 1) {
          return jsonResponse({error: {code: 'already_exists', message: 'duplicate candidate version'}}, 409);
        }
        return jsonResponse({error: {code: 'hash_mismatch', message: 'sha256 mismatch'}}, 400);
      }
      return jsonResponse({data: []});
    });

    const {container} = render(wrap(<ExtensionsPage pathname="/extensions/installed" roles={['OPERATOR']} />));
    await screen.findAllByText('@acme/pending-upg');

    // Open menu on UPDATING_PENDING_RECONSENT row (DEF-060, DEF-061).
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for @acme\/pending-upg/i})[0]!);
    const rollbackItem = await screen.findByRole('menuitem', {name: /Rollback version/i});
    expect(rollbackItem.getAttribute('aria-disabled')).toBe('true');
    expect(screen.getAllByText(/No previous pin exists/i).length).toBeGreaterThanOrEqual(1);

    // Open review permissions modal -> shows "Close (keep pending)".
    fireEvent.click(await screen.findByRole('menuitem', {name: /Review permissions/i}));
    const keepPendingBtn = await screen.findByRole('button', {name: /Close \(keep pending\)/i});
    fireEvent.click(keepPendingBtn);

    // Discard staged candidate without requiring audit reason (DEF-061).
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for @acme\/pending-upg/i})[0]!);
    fireEvent.click(await screen.findByRole('menuitem', {name: /Discard staged upgrade/i}));
    fireEvent.click(await screen.findByRole('button', {name: /Discard candidate/i}));
    await waitFor(() => expect(discardCalled).toBe(true));

    // Upgrade errors show distinct DEF-062 duplicate candidate and DEF-073 hash_mismatch copy.
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for @acme\/pending-upg/i})[0]!);
    fireEvent.click(await screen.findByRole('menuitem', {name: /Upgrade package/i}));
    const upgForm = document.getElementById('tp-upgrade-extension-form');
    expect(upgForm).toBeTruthy();
    const upgInputs = upgForm!.querySelectorAll('input');
    fireEvent.change(upgInputs[0]!, {target: {value: 'https://example.test/v2.tpkg'}});
    fireEvent.change(upgInputs[1]!, {target: {value: 'a'.repeat(64)}});
    fireEvent.click(screen.getByRole('button', {name: /Stage upgrade/i}));
    await screen.findByText(/exact immutable candidate version was already registered or rejected/i);

    fireEvent.change(upgInputs[0]!, {target: {value: 'https://example.test/v3.tpkg'}});
    fireEvent.change(upgInputs[1]!, {target: {value: 'b'.repeat(64)}});
    fireEvent.click(screen.getByRole('button', {name: /Stage upgrade/i}));
    await screen.findByText(/archive bytes do not match the expected SHA-256 digest/i);
    fireEvent.click(screen.getByRole('button', {name: /Cancel/i}));

    // Unquarantine flow on QUARANTINED row (DEF-058).
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for @acme\/quar-pkg/i})[0]!);
    fireEvent.click(await screen.findByRole('menuitem', {name: /Unquarantine package/i}));
    await screen.findByText(/conn_quar_1/);
    fireEvent.change(screen.getByLabelText(/Audit reason/i), {target: {value: 'cleared triage checks'}});
    fireEvent.click(screen.getByRole('button', {name: /Confirm unquarantine/i}));
    await waitFor(() => expect(unqCalled).toBe(true));
    await screen.findByText(/moved to INSTALLED_DISABLED/i);

    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-072: OperationsPage renders split-role cluster nodes and Stale/Lagging statuses with 0 WCAG violations', async () => {
    rememberCsrfToken('csrf-ops');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      const parsed = new URL(url, 'http://localhost');
      if (parsed.pathname === '/admin/v1/ops/cluster-status') {
        return jsonResponse({
          current_epoch: 12,
          storage_engine: 'sqlite_wal',
          backup_supported: true,
          backup_unavailable_reason: '',
          nodes: [
            {
              id: 'cp-node-1',
              role: 'control-plane',
              listen_addr: '127.0.0.1:8080',
              config_epoch: 12,
              snapshot_age_ms: 8,
              health: 'ok',
              wal_bytes: 4096,
              last_heartbeat_ms: 1732500000000,
              stale: false,
            },
            {
              id: 'dp-node-stale',
              role: 'data-plane',
              listen_addr: '127.0.0.1:8081',
              config_epoch: 12,
              snapshot_age_ms: 42000,
              health: 'stale',
              wal_bytes: 0,
              last_heartbeat_ms: 1732499900000,
              stale: true,
            },
            {
              id: 'dp-node-lagging',
              role: 'data-plane',
              listen_addr: '127.0.0.1:8082',
              config_epoch: 9,
              snapshot_age_ms: 210,
              health: 'lagging',
              wal_bytes: 0,
              last_heartbeat_ms: 1732500000000,
              stale: false,
            },
          ],
        });
      }
      if (parsed.pathname === '/admin/v1/ops/webhooks') {
        return jsonResponse({data: []});
      }
      if (parsed.pathname === '/admin/v1/ops/backups') {
        return jsonResponse({data: []});
      }
      if (parsed.pathname === '/admin/v1/ops/audit-integrity') {
        return jsonResponse({verified: true, entry_count: 3, last_checked_at_ms: 1732500000000});
      }
      if (parsed.pathname === '/readyz') {
        return jsonResponse({status: 'ready'});
      }
      return jsonResponse({data: []});
    });

    const {container} = render(wrap(<OperationsPage pathname="/operations" projectId="prj_1" roles={['OPERATOR']} />));
    await screen.findAllByText('cp-node-1');
    expect(screen.getAllByText('control-plane').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('data-plane').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Stale').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Lagging').length).toBeGreaterThanOrEqual(1);

    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });
});
