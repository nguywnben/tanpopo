import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken, subscribeCrossTabLogout} from '../api/client';
import {I18nProvider} from '../i18n/I18nProvider';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {AccessPage} from './access/AccessPage';
import {ExtensionsPage} from './extensions/ExtensionsPage';
import {ModelsPage} from './models/ModelsPage';
import {OrganizationsAdminPage} from './orgs/OrganizationsAdminPage';
import {PlaygroundPage} from './playground/PlaygroundPage';
import {ProvidersPage} from './providers/ProvidersPage';
import {RoutingPage} from './routes/RoutingPage';
import {SetupPage} from './setup/SetupPage';

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

describe('Engineering Round 3 defect regressions (DEF-032/035 & DEF-040..DEF-050)', () => {
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

  it('DEF-042: AccessPage excludes OPERATOR from organization member create and edit role pickers', async () => {
    rememberCsrfToken('csrf-access');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      if (parsed.pathname === '/admin/v1/members') {
        return jsonResponse({
          data: [
            {
              id: 'usr_1',
              email: 'dev@example.com',
              kind: 'HUMAN_LOCAL',
              status: 'ACTIVE',
              roles: ['DEVELOPER'],
              role_assignments: [{id: 'rb_1', role: 'DEVELOPER', scope_type: 'ORG', scope_id: 'org_1'}],
              version: 1,
              created_at: '2026-11-24T00:00:00Z',
            },
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/virtual-keys' || parsed.pathname === '/admin/v1/projects' || parsed.pathname === '/admin/v1/routes') {
        return jsonResponse({data: [], has_more: false});
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<AccessPage pathname="/access/members" projectId="prj_1" environmentId="env_1" currentPrincipalId="usr_admin" roles={['OPERATOR', 'ORG_OWNER']} />));
    await screen.findAllByText('dev@example.com');
    expect((await axe.run(container)).violations).toEqual([]);

    fireEvent.click(screen.getAllByRole('button', {name: /Add member/i})[0]!);
    const addMemberForm = document.getElementById('tp-add-member-form')!;
    const rolesTrigger = addMemberForm.querySelector('[role="combobox"], button')!;
    fireEvent.click(rolesTrigger);
    expect(screen.queryByRole('option', {name: /OPERATOR/i})).toBeNull();
    expect(screen.queryByText(/^Operator$/i)).toBeNull();
  });

  it('DEF-041: OrganizationsAdminPage labels DELETING orgs cleanly, suppresses Active badge on DELETING active org, and retries finalization', async () => {
    rememberCsrfToken('csrf-orgs');
    let finalized = false;
    const finalizeCalls: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/organizations' && method === 'GET') {
        return jsonResponse({
          data: finalized
            ? [{id: 'org_main', slug: 'main', name: 'Main Org', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', require_four_eyes_approval: false, version: 1, created_at: '2026-11-24T00:00:00Z'}]
            : [
                {id: 'org_main', slug: 'main', name: 'Main Org', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', require_four_eyes_approval: false, version: 1, created_at: '2026-11-24T00:00:00Z'},
                {id: 'org_del', slug: 'shred-me', name: 'Stranded Org', status: 'DELETING', min_plugin_trust: 'ANY_APPROVED', require_four_eyes_approval: false, version: 2, created_at: '2026-11-24T00:00:00Z'},
              ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/organizations/org_del/finalize-erasure' && method === 'POST') {
        finalizeCalls.push(parsed.pathname);
        finalized = true;
        return jsonResponse({target_type: 'ORGANIZATION', target_id: 'org_del', destroyed_dek_id: 'dek_1', shredded_at: '2026-11-24T01:00:00Z', revoked_virtual_keys_count: 1});
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(
      wrap(
        <OrganizationsAdminPage
          roles={['OPERATOR']}
          activeOrgId="org_del"
          onSelectOrg={vi.fn()}
          multiOrgEnabled={true}
          onOrganizationsChanged={vi.fn()}
        />,
      ),
    );
    await screen.findByText(/Organization erasure pending finalization/i);
    expect(screen.getAllByText(/Deleting \(pending erasure\)/i).length).toBeGreaterThan(0);
    // DataTable renders desktop + mobile rows (2 elements per org): only Main Org shows 'Active' (2 total), NOT Stranded DELETING org!
    expect(screen.queryAllByText('Active').length).toBe(2);
    expect((await axe.run(container)).violations).toEqual([]);

    fireEvent.click(screen.getAllByRole('button', {name: /Retry finalization/i})[0]!);
    await waitFor(() => expect(finalizeCalls).toEqual(['/admin/v1/organizations/org_del/finalize-erasure']));
  });

  it('DEF-043 & DEF-044: ExtensionsPage separates publisher signature from enterprise signature, hides uninstall on ENABLED_ACTIVE, and preserves 409 error text', async () => {
    rememberCsrfToken('csrf-ext');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/extensions/installations' && method === 'GET') {
        return jsonResponse({
          data: [
            {
              id: 'extins_active',
              package_id: 'extpkg_1',
              canonical_name: '@tanpopo/anthropic',
              active_version: '0.1.0',
              contract_version: '0.1',
              state: 'ENABLED_ACTIVE',
              origin_trust_tier: 'official',
              signature_verified: true,
              enterprise_signed: false,
              signing_key_fingerprint: 'abcdef1234567890abcdef1234567890',
              execution_tier: 'sandboxed_wasm',
              version: 1,
            },
            {
              id: 'extins_quarantined',
              package_id: 'extpkg_2',
              canonical_name: '@third/custom',
              active_version: '0.1.0',
              contract_version: '0.1',
              state: 'QUARANTINED',
              origin_trust_tier: 'unverified_external',
              signature_verified: false,
              enterprise_signed: true,
              signing_key_fingerprint: '1122334455667788',
              execution_tier: 'declarative',
              version: 2,
            },
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/marketplace/catalog') {
        return jsonResponse({
          data: [
            {
              package_id: '@tanpopo/anthropic',
              canonical_name: 'Anthropic Official',
              version: '0.1.0',
              execution_tier: 'sandboxed_wasm',
              origin_trust_tier: 'official',
              signature_verified: true,
              enterprise_signed: false,
              signing_key_fingerprint: 'abcdef1234567890abcdef1234567890',
              source_uri: 'tanpopo-builtin://anthropic.tpkg',
              expected_sha256: 'a'.repeat(64),
              summary: 'Anthropic adapter',
              capability_types: ['provider_adapter'],
            },
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/extensions/extins_active/permission-diff') {
        return jsonResponse({
          installation_id: 'extins_active',
          package_id: 'extpkg_1',
          candidate_version: '0.1.0',
          origin_trust_tier: 'official',
          signature_verified: true,
          enterprise_signed: false,
          signing_key_fingerprint: 'abcdef1234567890abcdef1234567890',
          execution_tier: 'sandboxed_wasm',
          candidate_permissions: {allowed_hosts: ['api.anthropic.com'], secret_slots: ['api_key'], hooks: []},
          permission_delta: {},
          has_escalation: false,
        }, 200, {ETag: '"v1"'});
      }
      if (parsed.pathname === '/admin/v1/blast-radius') {
        return jsonResponse({projects: [], connections: []});
      }
      if (parsed.pathname === '/admin/v1/extensions/extins_quarantined' && method === 'DELETE') {
        return jsonResponse({error: {code: 'extension_in_use', message: 'Extension is referenced by active provider connections.'}}, 409);
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<ExtensionsPage pathname="/extensions/installed" roles={['OPERATOR']} />));
    await screen.findAllByText('extpkg_1');
    expect(screen.getAllByText(/Signature verified \(abcdef123456\)/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Unsigned · Enterprise signed/i).length).toBeGreaterThan(0);
    expect((await axe.run(container)).violations).toEqual([]);

    // Active extension row menu must offer Quarantine, NOT Uninstall directly (SM-01)!
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for @tanpopo\/anthropic/i})[0]!);
    const activeMenu = (await screen.findAllByRole('menu'))[0]!;
    expect(within(activeMenu).getByText(/Quarantine package/i)).toBeInTheDocument();
    expect(within(activeMenu).queryByText(/Uninstall package/i)).toBeNull();

    // Opening permission review splits Publisher signature and Enterprise signature!
    fireEvent.click(within(activeMenu).getByText(/Review permissions/i));
    expect((await screen.findAllByText(/Enterprise signature/i)).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', {name: /Cancel/i}));

    // Quarantined row offers Uninstall package and surfaces 409 dependency conflict message accurately (not "already installed")!
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for @third\/custom/i})[0]!);
    expect((await screen.findAllByText(/Uninstall package/i)).length).toBeGreaterThan(0);
    fireEvent.click(screen.getAllByText(/Uninstall package/i)[0]!);
    fireEvent.change(await screen.findByLabelText(/Audit reason/i), {target: {value: 'Cleanup unused extension'}});
    fireEvent.click(screen.getByRole('button', {name: /Confirm uninstall/i}));
    expect(await screen.findByText(/referenced by active provider connections/i)).toBeInTheDocument();
    expect(screen.queryByText(/already installed/i)).toBeNull();
  });

  it('DEF-045, DEF-046, DEF-047: PlaygroundPage traces failed requests, links /traces, resets workspace on scope switch, and propagates cross-tab logout', async () => {
    rememberCsrfToken('csrf-pg');
    const traceCalls: string[] = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      if (parsed.pathname === '/admin/v1/routes') {
        const envId = parsed.searchParams.get('env_id');
        return jsonResponse({data: envId === 'env_1' ? [{id: 'rt_1', slug: 'route-alpha', strategy: 'PRIORITY_FAILOVER', targets: [], version: 1}] : [], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/playground/sessions') {
        return jsonResponse({session_id: 'pgs_1', ephemeral_virtual_key: 'vk_eph_1', expires_at: '2026-11-24T05:00:00Z', max_budget_micros: 500000}, 201);
      }
      if (parsed.pathname === '/v1/chat/completions') {
        return jsonResponse({error: {code: 'upstream_error', message: 'Upstream 502 error'}}, 502, {'X-Tanpopo-Trace-Id': 'trc_err_45'});
      }
      if (parsed.pathname === '/admin/v1/traces/trc_err_45') {
        traceCalls.push('trc_err_45');
        return jsonResponse({
          request_id: 'trc_err_45',
          started_at: '2026-11-24T00:00:00Z',
          route_slug: 'route-alpha',
          entry_point: 'playground',
          status_code: 502,
          usage_accuracy: 'UNMETERED_FLAGGED',
          total_cost_micros: 0,
          duration_ms: 42,
        });
      }
      return jsonResponse({}, 404);
    });

    const {rerender, container} = render(wrap(<PlaygroundPage pathname="/devtools/playground" projectId="prj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    await waitFor(() => expect(screen.getAllByText('route-alpha').length).toBeGreaterThan(0));

    fireEvent.change(screen.getByPlaceholderText(/Enter a test prompt/i), {target: {value: 'Trigger failing trace'}});
    await waitFor(() => expect(screen.getByRole('button', {name: /^Send$/i})).not.toBeDisabled());
    fireEvent.click(screen.getByRole('button', {name: /^Send$/i}));

    // Even when /v1/chat/completions returned 502, Playground fetches the failed trace and shows Inspect trace!
    await waitFor(() => expect(traceCalls).toContain('trc_err_45'));
    expect(await screen.findByText(/trc_err_45/)).toBeInTheDocument();
    const inspectBtn = screen.getByRole('button', {name: /Inspect trace/i});
    expect(inspectBtn).toBeInTheDocument();
    expect((await axe.run(container)).violations).toEqual([]);
    fireEvent.click(inspectBtn);
    expect(`${window.location.pathname}${window.location.search}`).toBe('/observability/traces?request_id=trc_err_45');

    // Switching project/env resets transcript, error, trace, and stale route slug!
    rerender(wrap(<PlaygroundPage pathname="/devtools/playground" projectId="prj_2" environmentId="env_2" roles={['ORG_OWNER']} />));
    await waitFor(() => {
      expect(screen.queryByText(/trc_err_45/)).toBeNull();
      expect(screen.queryByText(/Trigger failing trace/)).toBeNull();
      expect(screen.queryByText('route-alpha')).toBeNull();
    });

    // Cross-tab logout storage event notifies listeners!
    const logoutSpy = vi.fn();
    const unsub = subscribeCrossTabLogout(logoutSpy);
    window.dispatchEvent(new StorageEvent('storage', {key: 'tanpopo.auth.logout_ts', newValue: JSON.stringify({type: 'logout', ts: 123, nonce: 'a'})}));
    expect(logoutSpy).toHaveBeenCalledTimes(1);
    unsub();
  });

  it('DEF-032, DEF-035, DEF-040, DEF-049, DEF-050: Setup copy, Routing Target chain without (100%), Models empty-project & short IDs, Providers DISABLED guidance', async () => {
    rememberCsrfToken('csrf-ui');
    const deploymentCalls: string[] = [];
    let routeTraceCalls = 0;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      if (parsed.pathname === '/admin/v1/bootstrap/status') {
        return jsonResponse({initialized: false, available_adapters: [], marketplace_catalog_count: 8});
      }
      if (parsed.pathname === '/admin/v1/policies/effective') {
        return jsonResponse({id: 'pol_1', org_id: 'org_1', scope_tier: 'ORGANIZATION', allowed_data_regions: ['ON_PREM'], allowed_models: ['*'], min_plugin_trust: 'ANY_APPROVED', min_capability_assurance: 'DECLARED', guardrail_hooks: [], version: 1});
      }
      if (parsed.pathname === '/admin/v1/routes') {
        return jsonResponse({
          data: [{id: 'rt_1', slug: 'failover-single', env_id: 'env_1', strategy: 'PRIORITY_FAILOVER', targets: [{deployment_id: 'dep_1', weight: 100, priority_order: 1}], version: 1}],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/deployments') {
        deploymentCalls.push(parsed.search);
        return jsonResponse({
          data: [
            {
              id: 'dep_1',
              env_id: 'env_1',
              logical_model_id: 'mod_1',
              connection_id: 'conn_1234abcd',
              connection_state: 'DISABLED',
              upstream_model_name: 'llama3.2',
              data_region: 'ON_PREM',
              state: 'PROVISIONED',
              rate_card: {input_token_nano_usd: 0, output_token_nano_usd: 0},
              version: 1,
            },
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/traces') {
        routeTraceCalls += 1;
        return jsonResponse({error: {code: 'forbidden'}}, 403);
      }
      if (parsed.pathname === '/admin/v1/policies' || parsed.pathname === '/admin/v1/extensions') {
        return jsonResponse({data: [], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/models') {
        return jsonResponse({data: [{id: 'mod_1', slug: 'llama3.2', display_name: 'Llama 3.2', assurance_tier: 'TESTED', context_window: 8192, max_output_tokens: 2048, capabilities: {}}], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/connections') {
        return jsonResponse({
          data: [{id: 'conn_1234abcd', name: 'Ollama Local', adapter_id: 'adp_1', billing_mode: 'SELF_HOSTED_INFRA', endpoint_url: 'http://127.0.0.1:11434', data_region: 'ON_PREM', state: 'DISABLED', secret_slot_fingerprints: {}, version: 2}],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/secrets' || parsed.pathname === '/admin/v1/provider-adapters') {
        return jsonResponse({data: [], has_more: false});
      }
      return jsonResponse({}, 404);
    });

    // DEF-049: SetupPage copy must not promise provisioning a route!
    render(wrap(<SetupPage />));
    await screen.findByText(/primary organization and owner account/i);
    expect(screen.queryByText(/initial owner and route/i)).toBeNull();
    cleanup();

    // DEF-050: RoutingPage renders "Target chain" header, omits "(100%)" on PRIORITY_FAILOVER, and skips /traces for FINOPS_VIEWER!
    render(wrap(<RoutingPage pathname="/routes" projectId="prj_1" environmentId="env_1" roles={['FINOPS_VIEWER']} />));
    expect((await screen.findAllByText(/Target chain/i)).length).toBeGreaterThan(0);
    expect(screen.queryByText(/Fallback chain/i)).toBeNull();
    expect(screen.queryByText(/\(100%\)/)).toBeNull();
    expect(routeTraceCalls).toBe(0);
    cleanup();

    // DEF-035 & DEF-032: ModelsPage passes project_id, clears empty-env project, shows disabled-connection banner & short ID!
    deploymentCalls.length = 0;
    const modEmpty = render(wrap(<ModelsPage projectId="prj_empty" environmentId="" environments={[]} roles={['ORG_OWNER']} />));
    await waitFor(() => expect(deploymentCalls.length).toBe(0));
    expect(screen.getAllByText(/No deployments found/i).length).toBeGreaterThan(0);
    modEmpty.unmount();

    render(wrap(<ModelsPage projectId="prj_1" environmentId="" environments={[{id: 'env_1', org_id: 'org_1', project_id: 'prj_1', slug: 'prod', name: 'Production', created_at: '2026-11-24T00:00:00Z'}]} roles={['ORG_OWNER']} />));
    await waitFor(() => expect(deploymentCalls[0]).toContain('project_id=prj_1'));
    expect(await screen.findByText(/belong to a disabled provider connection/i)).toBeInTheDocument();
    expect(screen.getAllByText(/Connection: Disabled/i).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('button', {name: /Add deployment/i}));
    expect((await screen.findAllByText(/Ollama Local \(SELF_HOSTED_INFRA · 1234abcd\)/i)).length).toBeGreaterThan(0);
    cleanup();

    // DEF-040: ProvidersPage renders disabled connection recovery banner and Test & reactivate label!
    render(wrap(<ProvidersPage pathname="/providers/connections" projectId="prj_1" roles={['ORG_OWNER']} />));
    expect(await screen.findByText(/Disabled connections cannot serve traffic/i)).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for Ollama Local/i})[0]!);
    expect((await screen.findAllByText(/Test & reactivate connection/i)).length).toBeGreaterThan(0);
  });
});
