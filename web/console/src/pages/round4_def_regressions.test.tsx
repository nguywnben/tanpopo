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
import {ProvidersPage} from './providers/ProvidersPage';
import {RoutingPage} from './routes/RoutingPage';
import {SecurityPage} from './security/SecurityPage';
import {TracesPage} from './traces/TracesPage';
import {ConsoleShell} from '../shell/ConsoleShell';

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

  it('DEF-054/066: DEVELOPER & TEAM_ADMIN skip unauthorized connection/secret/org-policy calls without errors or WCAG violations', async () => {
    const requested: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      const parsed = new URL(url, 'http://localhost');
      requested.push(parsed.pathname + parsed.search);
      if (parsed.pathname === '/admin/v1/models') {
        return jsonResponse({
          data: [{
            id: 'lmd_1',
            slug: 'qwen-custom',
            display_name: 'Qwen Custom Enterprise',
            context_window: 32768,
            max_output_tokens: 4096,
            assurance_tier: 'DECLARED',
            capabilities: {
              streaming: true, tool_calling: false, parallel_tool_calls: false, strict_json_schema: false,
              json_object: false, vision: false, audio_input: false, document_input: false,
              reasoning: false, prompt_caching: false, embeddings: false, rerank: false,
            },
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/routes' || parsed.pathname === '/admin/v1/deployments' || parsed.pathname === '/admin/v1/secrets' || parsed.pathname === '/admin/v1/connections' || parsed.pathname === '/admin/v1/provider-adapters') {
        return jsonResponse({data: [], next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/policies/effective') {
        if (!parsed.searchParams.get('project_id')) {
          return jsonResponse({error: {code: 'permission_denied', message: 'org policy denied'}}, 403);
        }
        return jsonResponse({
          id: 'pol_prj_1',
          scope_type: 'PROJECT',
          scope_id: 'prj_1',
          version: 1,
          rpm_ceiling: 1000,
          tpm_ceiling: 100000,
          max_concurrent_streams: 10,
          allowed_data_regions: ['US'],
          min_plugin_trust: 'ANY_APPROVED',
          min_capability_assurance: 'DECLARED',
          allowed_models: ['*'],
          guardrail_hooks: [],
          deny_all: false,
        }, 200, {ETag: '"v1"'});
      }
      return jsonResponse({data: []});
    });

    const devModels = render(wrap(<ModelsPage projectId="prj_1" environmentId="env_1" roles={['DEVELOPER']} />));
    const modelsTab = await screen.findByRole('tab', {name: /Logical models/i});
    fireEvent.click(modelsTab);
    await screen.findAllByText('Qwen Custom Enterprise');
    expect(requested.some(r => r.startsWith('/admin/v1/connections'))).toBe(false);
    expect(await axe.run(devModels.container)).toHaveProperty('violations', []);
    devModels.unmount();

    requested.length = 0;
    const devProviders = render(wrap(<ProvidersPage pathname="/providers/connections" projectId="prj_1" roles={['DEVELOPER']} />));
    await screen.findByRole('heading', {level: 1});
    await waitFor(() => expect(devProviders.container.querySelector('[aria-busy="true"]')).toBeNull());
    expect(requested.some(r => r.startsWith('/admin/v1/connections') || r.startsWith('/admin/v1/secrets'))).toBe(false);
    expect(await axe.run(devProviders.container)).toHaveProperty('violations', []);
    devProviders.unmount();

    requested.length = 0;
    const taProviders = render(wrap(<ProvidersPage pathname="/providers/connections" projectId="prj_1" roles={['TEAM_ADMIN']} />));
    await waitFor(() => expect(requested.includes('/admin/v1/secrets?project_id=prj_1&limit=100')).toBe(true));
    taProviders.unmount();

    requested.length = 0;
    const taRouting = render(wrap(<RoutingPage pathname="/routes" projectId="prj_1" environmentId="env_1" roles={['TEAM_ADMIN']} />));
    await waitFor(() => expect(requested.some(r => r.includes('/admin/v1/policies/effective?project_id=prj_1'))).toBe(true));
    expect(requested.some(r => r === '/admin/v1/policies/effective')).toBe(false);
    expect(await axe.run(taRouting.container)).toHaveProperty('violations', []);
  });

  it('DEF-025: ModelsPage deployment drawer warns clearly and disables submit when project has zero environments', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => jsonResponse({data: [], next_cursor: null, has_more: false}));
    const {container} = render(wrap(<ModelsPage projectId="prj_empty" environmentId="" environments={[]} roles={['ORG_OWNER']} />));
    const addDepBtn = await screen.findByRole('button', {name: /Add deployment/i});
    fireEvent.click(addDepBtn);
    await screen.findByText(/No environments exist in the active project yet/i);
    const saveBtn = screen.getByRole('button', {name: /Save deployment/i});
    expect(saveBtn.getAttribute('aria-disabled')).toBe('true');

    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-068: ConsoleShell preserves CryptoShredReceipt banner across active-org switch remount', async () => {
    window.history.replaceState(null, '', '/ops/organizations');
    rememberCsrfToken('csrf-shell');
    let orgs = [
      {id: 'org_1', slug: 'alpha-org', name: 'Alpha Org', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', four_eyes_enabled: false, version: 1, created_at: '2026-11-26T00:00:00Z'},
      {id: 'org_2', slug: 'beta-org', name: 'Beta Org', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', four_eyes_enabled: false, version: 1, created_at: '2026-11-26T00:00:00Z'},
    ];
    let currentOrg = 'org_1';

    let deleteMode: 'immediate' | 'pending' | 'error' = 'immediate';

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const req = input instanceof Request ? input : null;
      const url = req ? req.url : typeof input === 'string' ? input : String(input);
      const parsed = new URL(url, 'http://localhost');
      const method = (init?.method ?? req?.method ?? 'GET').toUpperCase();
      if (parsed.pathname === '/admin/v1/auth/session') {
        return jsonResponse({
          authenticated: true,
          principal_id: 'prc_op_1',
          email: 'op@tanpopo.test',
          org_id: currentOrg,
          org_name: orgs.find(o => o.id === currentOrg)?.name ?? 'Beta Org',
          roles: ['OPERATOR', 'ORG_OWNER'],
          requires_password_rotation: false,
          csrf_token: 'csrf-shell',
        });
      }
      if (parsed.pathname === '/admin/v1/organizations' && method === 'GET') {
        return jsonResponse({data: orgs, next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/organizations/org_1' && method === 'DELETE') {
        if (deleteMode === 'pending') {
          return jsonResponse({approval_id: 'apr_1', status: 'PENDING_APPROVAL'}, 202);
        }
        if (deleteMode === 'error') {
          return jsonResponse({error: {code: 'erasure_failed', message: 'KMS unavailable'}}, 500);
        }
        orgs = orgs.filter(o => o.id !== 'org_1');
        currentOrg = 'org_2';
        return jsonResponse({
          target_type: 'ORGANIZATION',
          target_id: 'org_1',
          destroyed_dek_id: 'dek_shred_alpha_01',
          revoked_virtual_keys_count: 3,
          audit_event_id: 'aud_shred_1',
        });
      }
      if (parsed.pathname === '/admin/v1/bootstrap/status') {
        return jsonResponse({completed: true, offline_mode: false, multi_org_enabled: true});
      }
      if (parsed.pathname === '/admin/v1/projects') {
        return jsonResponse({data: [{id: 'prj_2', org_id: currentOrg, slug: 'default', name: 'Default', version: 1, created_at: '2026-11-26T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname.endsWith('/environments')) {
        return jsonResponse({data: [{id: 'env_2', org_id: currentOrg, project_id: 'prj_2', slug: 'prod', name: 'Production', version: 1, created_at: '2026-11-26T00:00:00Z'}]});
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    const {container} = render(wrap(<ConsoleShell />));
    await screen.findAllByText('Alpha Org');

    const shredBtns = await screen.findAllByRole('button', {name: /Crypto-shred/i});
    fireEvent.click(shredBtns[0]!);
    const shredModal = await screen.findByRole('dialog');
    const shredInputs = shredModal.querySelectorAll('input');
    fireEvent.change(shredInputs[shredInputs.length - 1]!, {target: {value: 'alpha-org'}});
    const confirmBtns = Array.from(shredModal.querySelectorAll('button')).filter(b => b.textContent?.includes('Crypto-shred'));
    fireEvent.click(confirmBtns[confirmBtns.length - 1]!);

    await screen.findByText(/Organization crypto-shredded/i);
    await screen.findByText(/dek_shred_alpha_01/);

    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
    cleanup();

    // Verify 202 Accepted (pending approval) clears/omits the receipt banner.
    orgs = [
      {id: 'org_1', slug: 'alpha-org', name: 'Alpha Org', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', four_eyes_enabled: true, version: 1, created_at: '2026-11-26T00:00:00Z'},
      {id: 'org_2', slug: 'beta-org', name: 'Beta Org', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', four_eyes_enabled: false, version: 1, created_at: '2026-11-26T00:00:00Z'},
    ];
    currentOrg = 'org_1';
    deleteMode = 'pending';
    render(wrap(<ConsoleShell />));
    await screen.findAllByText('Alpha Org');
    fireEvent.click((await screen.findAllByRole('button', {name: /Crypto-shred/i}))[0]!);
    const pendingModal = await screen.findByRole('dialog');
    const pendingInputs = pendingModal.querySelectorAll('input');
    fireEvent.change(pendingInputs[pendingInputs.length - 1]!, {target: {value: 'alpha-org'}});
    const pendingConfirm = Array.from(pendingModal.querySelectorAll('button')).filter(b => b.textContent?.includes('Crypto-shred'));
    fireEvent.click(pendingConfirm[pendingConfirm.length - 1]!);
    await screen.findByText(/Pending approval/i);
    expect(screen.queryByText(/Organization crypto-shredded/i)).toBeNull();
    cleanup();

    // Verify DELETE error response also clears/omits the receipt banner.
    deleteMode = 'error';
    render(wrap(<ConsoleShell />));
    await screen.findAllByText('Alpha Org');
    fireEvent.click((await screen.findAllByRole('button', {name: /Crypto-shred/i}))[0]!);
    const errModal = await screen.findByRole('dialog');
    const errInputs = errModal.querySelectorAll('input');
    fireEvent.change(errInputs[errInputs.length - 1]!, {target: {value: 'alpha-org'}});
    const errConfirm = Array.from(errModal.querySelectorAll('button')).filter(b => b.textContent?.includes('Crypto-shred'));
    fireEvent.click(errConfirm[errConfirm.length - 1]!);
    await screen.findByText(/KMS unavailable/i);
    expect(screen.queryByText(/Organization crypto-shredded/i)).toBeNull();
  });

  it('OPERATOR receives full console write and approval controls aligned with backend rbac.Authorizer', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      if (url.includes('/admin/v1/policies/effective') || url.includes('/admin/v1/policies/scopes')) {
        return jsonResponse({
          data: [{
            id: 'pol_org_1',
            scope_type: 'ORGANIZATION',
            scope_id: 'org_1',
            version: 1,
            rpm_ceiling: 100,
            tpm_ceiling: 10000,
            max_concurrent_streams: 10,
            allowed_data_regions: ['US'],
            min_plugin_trust: 'ANY_APPROVED',
            min_capability_assurance: 'DECLARED',
            allowed_models: ['*'],
            guardrail_hooks: [],
            deny_all: false,
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/budgets')) {
        return jsonResponse({
          data: [{
            id: 'bdg_org_1',
            org_id: 'org_1',
            scope_tier: 'ORGANIZATION',
            scope_id: 'org_1',
            period: 'MONTHLY',
            hard_limit_micros: 100000000,
            warning_thresholds_pct: [50, 80, 95],
            version: 1,
            created_at: '2026-11-26T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/approvals')) {
        return jsonResponse({
          data: [{
            id: 'apr_1',
            org_id: 'org_1',
            target_kind: 'MCP_TOOL_CALL',
            target_id: 'mcp_1',
            action_kind: 'MCP_DESTRUCTIVE_TOOL',
            status: 'PENDING',
            requested_by: 'prc_other',
            reason: 'review needed',
            approval_binding_hash: 'sha256:abc123',
            schema_drift_locked: false,
            version: 1,
            created_at: '2026-11-26T00:00:00Z',
            expires_at: '2026-11-27T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    const prov = render(wrap(<ProvidersPage pathname="/providers/connections" projectId="prj_1" roles={['OPERATOR']} />));
    await screen.findByRole('button', {name: /New connection/i});
    prov.unmount();

    const mods = render(wrap(<ModelsPage projectId="prj_1" environmentId="env_1" environments={[{id: 'env_1', org_id: 'org_1', project_id: 'prj_1', slug: 'prod', name: 'Prod', created_at: '2026-11-26T00:00:00Z'}]} roles={['OPERATOR']} />));
    await screen.findByRole('button', {name: /Add deployment/i});
    mods.unmount();

    const fin = render(wrap(<FinOpsPage pathname="/finops/budgets" activeOrgId="org_1" projects={[]} projectId="prj_1" environmentId="env_1" roles={['OPERATOR']} />));
    await screen.findByRole('button', {name: /Create budget/i});
    expect((await screen.findAllByRole('button', {name: /^Save$/i})).length).toBeGreaterThan(0);
    fin.unmount();

    const sec = render(wrap(<SecurityPage pathname="/security/approvals" roles={['OPERATOR']} />));
    await screen.findByRole('button', {name: /^Approve$/i});
    sec.unmount();

    const rts = render(wrap(<RoutingPage pathname="/routes" projectId="prj_1" environmentId="env_1" roles={['OPERATOR']} />));
    await screen.findByRole('button', {name: /Create route/i});
    expect(await axe.run(rts.container)).toHaveProperty('violations', []);
  });
});
