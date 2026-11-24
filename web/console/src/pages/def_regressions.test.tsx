import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken, subscribeUnauthorized} from '../api/client';
import {notifySecurityStateChanged, subscribeSecurityStateChanged} from '../api/managementClient';
import {I18nProvider} from '../i18n/I18nProvider';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {ConfirmDialog} from '../ui/Overlays';
import {AccessPage} from './access/AccessPage';
import {LoginPage} from './auth/LoginPage';
import {ExtensionsPage} from './extensions/ExtensionsPage';
import {FinOpsPage} from './finops/FinOpsPage';
import {McpHubPage} from './mcp/McpHubPage';
import {ModelsPage} from './models/ModelsPage';
import {OperationsPage} from './operations/OperationsPage';
import {PlaygroundPage} from './playground/PlaygroundPage';
import {ProvidersPage} from './providers/ProvidersPage';
import {RoutingPage} from './routes/RoutingPage';
import {SetupPage} from './setup/SetupPage';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {'Content-Type': 'application/json'},
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

describe('Engineering Round 1 defect regressions (DEF-001..DEF-015)', () => {
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

  it('DEF-001: evaluates Stepper SC 2.5.3 label-content-name-mismatch without incomplete errors across Setup steps 0->1->2', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/bootstrap/status')) {
        return jsonResponse({
          initialized: false,
          available_adapters: [
            {id: 'builtin:openai', name: 'OpenAI-compatible API', secret_slots: ['api_key'], supports_oauth_pkce: false, billing_mode: 'COMMERCIAL_API'},
          ],
        });
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<SetupPage />));
    await screen.findAllByLabelText(/Bootstrap token/i);

    const step0Audit = await axe.run(container);
    expect(step0Audit.violations).toEqual([]);
    expect(step0Audit.incomplete.filter(item => item.id === 'label-content-name-mismatch')).toEqual([]);

    const tokenInput = (await screen.findAllByLabelText(/Bootstrap token/i)).find(el => el.tagName === 'INPUT')!;
    const emailInput = screen.getByLabelText(/Owner email/i);
    const pwInput = screen.getAllByLabelText(/^Owner password$/i).find(el => el.tagName === 'INPUT')!;
    const confirmInput = screen.getAllByLabelText(/Confirm password/i).find(el => el.tagName === 'INPUT')!;
    fireEvent.change(tokenInput, {target: {value: 'btp_live_valid_token_012345'}});
    fireEvent.change(emailInput, {target: {value: 'owner@tanpopo.test'}});
    fireEvent.change(pwInput, {target: {value: 'TanpopoStrongPass!2026'}});
    fireEvent.change(confirmInput, {target: {value: 'TanpopoStrongPass!2026'}});
    fireEvent.click(screen.getByRole('button', {name: /Continue/i}));

    await screen.findByText(/Initial provider connection/i);
    const step1Audit = await axe.run(container);
    expect(step1Audit.violations).toEqual([]);
    expect(step1Audit.incomplete.filter(item => item.id === 'label-content-name-mismatch')).toEqual([]);
  });

  it('DEF-002 & DEF-013: defaults @tanpopo/ollama to self-hosted on-prem private CIDR, reloads failed probes, and reflects connection_state on Models & Routes', async () => {
    let connState: 'CREATED_UNVERIFIED' | 'ACTIVE' | 'AUTH_EXPIRED_REAUTH_REQUIRED' = 'ACTIVE';
    rememberCsrfToken('csrf-def002');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const pathname = new URL(url, window.location.origin).pathname;
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (pathname === '/admin/v1/provider-adapters') {
        return jsonResponse({
          data: [
            {
              id: '@tanpopo/ollama',
              package_id: 'pkg_ollama',
              name: 'Ollama Local',
              version: '1.0.0',
              wire_protocol: 'ollama',
              origin_trust: 'OFFICIAL',
              enterprise_signed: true,
              installation_state: 'ENABLED_ACTIVE',
              auth_methods: [],
              secret_slots: [],
              allowed_hosts: ['127.0.0.1'],
            },
            {
              id: '@tanpopo/openai',
              package_id: 'pkg_openai',
              name: 'OpenAI-compatible',
              version: '1.0.0',
              wire_protocol: 'openai',
              origin_trust: 'OFFICIAL',
              enterprise_signed: true,
              installation_state: 'ENABLED_ACTIVE',
              auth_methods: ['API_KEY'],
              secret_slots: ['api_key'],
              allowed_hosts: ['api.openai.com'],
            },
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/secrets') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/connections' && method === 'GET') {
        return jsonResponse({
          data: [
            {
              id: 'con_probe_1',
              name: 'Upstream Conn',
              adapter_id: '@tanpopo/openai',
              endpoint_url: 'https://api.openai.com/v1',
              billing_mode: 'COMMERCIAL_API',
              data_region: 'US',
              state: connState,
              allow_private_ip: false,
              allowed_cidrs: [],
              secret_slot_fingerprints: {},
              version: 1,
            },
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/connections/con_probe_1/test' && method === 'POST') {
        connState = 'AUTH_EXPIRED_REAUTH_REQUIRED';
        return jsonResponse({
          valid: false,
          latency_ms: 14,
          failure_code: 'auth_error',
          sanitized_message: '401 Unauthorized',
        });
      }
      if (pathname === '/admin/v1/models') {
        return jsonResponse({
          data: [
            {
              id: 'mod_1',
              slug: 'llama3.2',
              display_name: 'Llama 3.2',
              context_window: 8192,
              max_output_tokens: 2048,
              assurance_tier: 'VERIFIED',
              capabilities: {streaming: true, tool_calling: false, parallel_tool_calls: false, strict_json_schema: false, json_object: false, vision: false, audio_input: false, document_input: false, reasoning: false, prompt_caching: false, embeddings: false, rerank: false},
            },
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/deployments') {
        return jsonResponse({
          data: [
            {
              id: 'dpl_deg',
              env_id: 'env_1',
              logical_model_id: 'mod_1',
              connection_id: 'con_deg',
              connection_state: 'DEGRADED',
              upstream_model_name: 'llama3.2-local',
              data_region: 'ON_PREM',
              state: 'ACTIVE_HEALTHY',
              version: 1,
              rate_card: {input_token_nano_usd: 0, output_token_nano_usd: 0, cache_read_token_nano_usd: 0, cache_write_token_nano_usd: 0, reasoning_token_nano_usd: 0, image_unit_nano_usd: 0, audio_sec_nano_usd: 0, document_page_nano_usd: 0, self_hosted_zero_rate: true},
            },
            {
              id: 'dpl_unv',
              env_id: 'env_1',
              logical_model_id: 'mod_1',
              connection_id: 'con_unv',
              connection_state: 'CREATED_UNVERIFIED',
              upstream_model_name: 'gpt-unverified',
              data_region: 'US',
              state: 'PROVISIONED',
              version: 1,
              rate_card: {input_token_nano_usd: 100, output_token_nano_usd: 200, cache_read_token_nano_usd: 0, cache_write_token_nano_usd: 0, reasoning_token_nano_usd: 0, image_unit_nano_usd: 0, audio_sec_nano_usd: 0, document_page_nano_usd: 0, self_hosted_zero_rate: false},
            },
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/routes') {
        return jsonResponse({
          data: [
            {
              id: 'rt_1',
              env_id: 'env_1',
              slug: 'chat-default',
              strategy: 'priority',
              targets: [{deployment_id: 'dpl_deg', priority_order: 1}],
              version: 1,
            },
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/traces') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/policies/effective') {
        return jsonResponse({
          id: 'pol_1',
          scope_tier: 'ORGANIZATION',
          scope_id: 'org_1',
          data_residency: 'GLOBAL',
          min_plugin_trust: 'OFFICIAL_ONLY',
          min_capability_assurance: 'DECLARED',
          allowed_models: ['*'],
          guardrail_hooks: [],
          version: 1,
        });
      }
      return jsonResponse({}, 404);
    });

    const provRender = render(wrap(<ProvidersPage pathname="/providers/connections" projectId="proj_1" roles={['ORG_OWNER']} />));
    await screen.findAllByText('Upstream Conn');

    // DEF-013: Opening New connection with @tanpopo/ollama defaults pre-populates self-hosted local CIDR.
    fireEvent.click(screen.getAllByRole('button', {name: /New connection/i})[0]!);
    const privateToggle = await screen.findByLabelText(/Allow private or loopback upstream/i) as HTMLInputElement;
    expect(privateToggle.checked).toBe(true);
    const cidrInput = await screen.findByLabelText(/Allowed CIDRs/i) as HTMLInputElement;
    expect(cidrInput.value).toBe('127.0.0.1/32');
    fireEvent.click(screen.getAllByRole('button', {name: /Cancel/i})[0]!);

    // DEF-002: Failed probe immediately reloads connection state to AUTH_EXPIRED_REAUTH_REQUIRED.
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for Upstream Conn/i})[0]!);
    fireEvent.click((await screen.findAllByText(/Probe connection/i))[0]!);
    await waitFor(() => expect(screen.getAllByText(/Re-auth required/i).length).toBeGreaterThan(0));
    provRender.unmount();

    // DEF-002: ModelsPage shows DEGRADED rather than green Healthy when backing connection is DEGRADED.
    const modRender = render(wrap(<ModelsPage environmentId="env_1" roles={['ORG_OWNER']} />));
    await screen.findAllByText('llama3.2-local');
    expect(screen.getAllByText('Degraded').length).toBeGreaterThan(0);
    expect(screen.queryByText('Healthy')).toBeNull();
    // Gated Activate action when backing connection is CREATED_UNVERIFIED
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for gpt-unverified/i})[0]!);
    expect(screen.queryByText(/Activate deployment/i)).toBeNull();
    modRender.unmount();

    // DEF-002: RoutingPage target health also shows Degraded instead of Healthy.
    render(wrap(<RoutingPage pathname="/routes" projectId="proj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    await screen.findAllByText('chat-default');
    expect(screen.getAllByText('Degraded').length).toBeGreaterThan(0);
    expect(screen.queryByText('Healthy')).toBeNull();
  });

  it('DEF-003, DEF-012, DEF-014, DEF-015: computes KPIs from usage rows, avoids hold double-count, creates budgets, and exports CSV/JSON with active groupBy', async () => {
    const budgets: Record<string, unknown>[] = [];
    const exportCalls: string[] = [];
    let budgetPutBody: Record<string, unknown> | null = null;
    let putIfMatch = '';
    rememberCsrfToken('csrf-finops');

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(url, window.location.origin);
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/budgets' && method === 'GET') {
        return jsonResponse({data: budgets, has_more: false});
      }
      if (parsed.pathname.startsWith('/admin/v1/budgets/') && method === 'PUT') {
        const hdrs = new Headers(init?.headers ?? (input instanceof Request ? input.headers : undefined));
        putIfMatch = hdrs.get('If-Match') ?? '';
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        budgetPutBody = JSON.parse(rawBody) as Record<string, unknown>;
        const created = {
          id: parsed.pathname.split('/').pop(),
          ...budgetPutBody,
          settled_exact_micros: 0,
          settled_estimated_micros: 0,
          reserved_hold_micros: 400_000,
          negative_carryover_micros: 0,
          version: 1,
        };
        budgets.push(created);
        return jsonResponse(created, 200);
      }
      if (parsed.pathname === '/admin/v1/virtual-keys') return jsonResponse({data: [], has_more: false});
      if (parsed.pathname === '/admin/v1/usage/summary') {
        return jsonResponse({
          data: [
            {
              group_key: parsed.searchParams.get('group_by') === 'model' ? 'gpt-4o' : 'proj_1',
              exact_micro_usd: 1_500_000,
              estimated_micro_usd: 250_000,
              exact_request_count: 10,
              estimated_request_count: 2,
              unmetered_request_count: 3,
            },
          ],
        });
      }
      if (parsed.pathname === '/admin/v1/usage/export') {
        exportCalls.push(`${parsed.searchParams.get('format')}:${parsed.searchParams.get('group_by')}`);
        if (parsed.searchParams.get('format') === 'csv') {
          return new Response('group_key,exact_micro_usd\ngpt-4o,1500000\n', {
            status: 200,
            headers: {'Content-Type': 'text/csv'},
          });
        }
        return jsonResponse([{group_key: parsed.searchParams.get('group_by'), exact_micro_usd: 1_500_000}]);
      }
      return jsonResponse({}, 404);
    });

    const createObjectUrlSpy = vi.fn(() => 'blob:tanpopo-test');
    const revokeObjectUrlSpy = vi.fn();
    Object.defineProperty(URL, 'createObjectURL', {configurable: true, value: createObjectUrlSpy});
    Object.defineProperty(URL, 'revokeObjectURL', {configurable: true, value: revokeObjectUrlSpy});

    render(
      wrap(
        <FinOpsPage
          pathname="/finops/chargeback"
          activeOrgId="org_1"
          projects={[{id: 'proj_1', org_id: 'org_1', slug: 'default', name: 'Default', version: 1, created_at: '2026-11-20T00:00:00Z'}]}
          projectId="proj_1"
          environmentId="env_1"
          roles={['ORG_OWNER']}
        />,
      ),
    );

    // DEF-003/015: settled metrics are non-zero even when budgets=[]!
    await screen.findAllByText('$1.500000');
    expect(screen.getAllByText('$0.250000').length).toBeGreaterThan(0);
    expect(screen.getAllByText('3').length).toBeGreaterThan(0);

    // DEF-014/015: switch Group by to Model, then export CSV & JSON
    fireEvent.click(screen.getByRole('combobox', {name: /Group by/i}));
    fireEvent.click(await screen.findByRole('option', {name: 'Model'}));
    await waitFor(() => expect(screen.getAllByText('gpt-4o').length).toBeGreaterThan(0));

    fireEvent.click(screen.getByRole('button', {name: /Export CSV/i}));
    await waitFor(() => expect(exportCalls).toContain('csv:model'));
    fireEvent.click(screen.getByRole('button', {name: /Export JSON/i}));
    await waitFor(() => expect(exportCalls).toContain('json:model'));
    expect(createObjectUrlSpy).toHaveBeenCalledTimes(2);
    const csvBlob = (createObjectUrlSpy.mock.calls[0] as unknown[])[0] as Blob;
    expect(await csvBlob.text()).toContain('group_key,exact_micro_usd');

    // DEF-012: Blank/whitespace limit must not coerce to 0 USD or call PUT; valid limit sends If-Match "v1".
    fireEvent.click(screen.getByRole('button', {name: /Create budget/i}));
    const form = document.getElementById('tp-create-budget-form')!;
    const limitInput = screen.getByLabelText(/Hard limit \(micro-USD\)/i) as HTMLInputElement;
    fireEvent.change(limitInput, {target: {value: '   '}});
    fireEvent.submit(form);
    await screen.findByText(/non-negative safe integer/i);
    expect(budgetPutBody).toBeNull();
    fireEvent.change(limitInput, {target: {value: '100000000'}});
    fireEvent.submit(form);
    await waitFor(() => expect(budgetPutBody).not.toBeNull());
    expect(putIfMatch).toBe('"v1"');
    expect(budgetPutBody).toMatchObject({
      scope_tier: 'PROJECT',
      scope_id: 'proj_1',
      hard_limit_micros: 100000000,
      period: 'MONTHLY',
    });
  });

  it('DEF-005 & DEF-011: validates private CIDRs on MCP/A2A registration and resumes input-required A2A tasks', async () => {
    const tasks = [
      {
        task_id: 'tsk_need_input',
        target_agent_id: 'agt_1',
        target_agent_slug: 'billing-analyst',
        state: 'input-required',
        prompt_message: 'Approve invoice #42?',
        hop_count: 1,
        enforced_region: 'US',
        sub_budget_hold_micros: 50_000,
        settled_cost_micros: 0,
        version: 1,
        created_at: '2026-11-22T00:00:00Z',
        updated_at: '2026-11-22T00:00:01Z',
      },
    ];
    let resumedBody: unknown = null;
    let mcpPostedBody: Record<string, unknown> | null = null;
    rememberCsrfToken('csrf-a2a-res');

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const pathname = new URL(url, window.location.origin).pathname;
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (pathname === '/admin/v1/mcp/servers' && method === 'GET') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/mcp/servers' && method === 'POST') {
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        mcpPostedBody = JSON.parse(rawBody) as Record<string, unknown>;
        return jsonResponse({id: 'mcp_1', project_id: 'proj_1', slug: 'int-mcp', name: 'Int MCP', transport: 'streamable_http', endpoint_url: 'https://127.0.0.1:9000/mcp', allow_private_ip: true, state: 'ACTIVE', tool_count: 0, version: 1}, 201);
      }
      if (pathname === '/admin/v1/a2a/agents') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/virtual-keys') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/a2a/tasks' && method === 'GET') return jsonResponse({data: tasks, has_more: false});
      if (pathname === '/admin/v1/a2a/tasks/tsk_need_input/resume' && method === 'POST') {
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        resumedBody = JSON.parse(rawBody);
        tasks[0]!.state = 'completed';
        return jsonResponse(tasks[0], 200);
      }
      return jsonResponse({}, 404);
    });

    render(wrap(<McpHubPage pathname="/a2a/agents" projectId="proj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    await waitFor(() => expect(screen.getAllByText('Approve invoice #42?').length).toBeGreaterThan(0));

    // DEF-011: Resume input-required task via UI Modal
    fireEvent.click(screen.getAllByRole('button', {name: /^Resume$/i})[0]!);
    fireEvent.change(await screen.findByLabelText(/Follow-up input/i), {target: {value: 'Approved for $42'}});
    fireEvent.submit(document.getElementById('tp-resume-a2a-form')!);
    await waitFor(() => expect(resumedBody).toEqual({input_parts: [{kind: 'text', text: 'Approved for $42'}]}));

    // DEF-005: Private MCP server rejects 0.0.0.0/0 and submits allowed_cidrs when valid
    fireEvent.click(screen.getByRole('tab', {name: /MCP servers/i}));
    fireEvent.click(screen.getAllByRole('button', {name: /Register server/i})[0]!);
    const mcpForm = document.getElementById('tp-register-mcp-form')!;
    fireEvent.change(screen.getByLabelText(/Server slug/i), {target: {value: 'int-mcp'}});
    fireEvent.change(screen.getByLabelText(/Display name/i), {target: {value: 'Int MCP'}});
    fireEvent.change(screen.getByLabelText(/Streamable HTTP endpoint URL/i), {target: {value: 'https://127.0.0.1:9000/mcp'}});
    fireEvent.click(mcpForm.querySelector('input[type="checkbox"]')!);
    const mcpCidrInp = (await screen.findAllByLabelText(/Allowed CIDRs/i)).find(el => mcpForm.contains(el))!;
    fireEvent.change(mcpCidrInp, {target: {value: '0.0.0.0/0'}});
    fireEvent.submit(mcpForm);
    await screen.findByText(/explicit non-wildcard CIDRs/i);
    expect(mcpPostedBody).toBeNull();

    fireEvent.change(mcpCidrInp, {target: {value: '127.0.0.1/32'}});
    fireEvent.submit(mcpForm);
    await waitFor(() => expect(mcpPostedBody).toMatchObject({allow_private_ip: true, allowed_cidrs: ['127.0.0.1/32']}));
  });

  it('DEF-006 & DEF-010: enforces Team Admin project-scoped role invites and supports A2A Virtual Key plane/route grants', async () => {
    let createdMemberBody: Record<string, unknown> | null = null;
    let createdKeyBody: {name?: string; allowed_planes?: string[]; allowed_routes?: string[]; allowed_models?: string[]} | null = null;
    rememberCsrfToken('csrf-access');

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const pathname = new URL(url, window.location.origin).pathname;
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (pathname === '/admin/v1/virtual-keys' && method === 'GET') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/virtual-keys' && method === 'POST') {
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        const parsed = JSON.parse(rawBody) as {name?: string; allowed_planes?: string[]; allowed_routes?: string[]; allowed_models?: string[]};
        createdKeyBody = parsed;
        return jsonResponse({
          id: 'vk_a2a',
          name: parsed.name,
          project_id: 'proj_1',
          env_id: 'env_1',
          key_prefix: 'vk_tp_dev_',
          last_four: '9999',
          state: 'CREATED_ACTIVE',
          allowed_planes: parsed.allowed_planes,
          allowed_models: parsed.allowed_models,
          allowed_routes: parsed.allowed_routes,
          version: 1,
          created_at: '2026-11-22T00:00:00Z',
          secret_key: 'vk_tp_dev_secret_once',
        }, 201);
      }
      if (pathname === '/admin/v1/models') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/deployments') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/members' && method === 'GET') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/members' && method === 'POST') {
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        createdMemberBody = JSON.parse(rawBody) as Record<string, unknown>;
        return jsonResponse({
          id: 'usr_dev_1',
          email: createdMemberBody.email,
          kind: 'HUMAN',
          roles: createdMemberBody.roles,
          project_ids: createdMemberBody.project_ids,
          version: 1,
          created_at: '2026-11-22T00:00:00Z',
        }, 201);
      }
      return jsonResponse({}, 404);
    });

    const projects = [{id: 'proj_1', org_id: 'org_1', slug: 'alpha', name: 'Alpha', version: 1, created_at: '2026-11-20T00:00:00Z'}];

    // Render as TEAM_ADMIN without ORG_OWNER/OPERATOR
    const {rerender} = render(
      wrap(
        <AccessPage
          pathname="/access/virtual-keys"
          projectId="proj_1"
          projects={projects}
          environmentId="env_1"
          currentPrincipalId="usr_team_admin"
          roles={['TEAM_ADMIN']}
        />,
      ),
    );

    // DEF-010: Issue Virtual Key with A2A plane and custom route pattern
    await screen.findByText(/Environment Virtual Keys/i);
    fireEvent.click(screen.getAllByRole('button', {name: /Issue key/i})[0]!);
    const issueForm = document.getElementById('tp-issue-key-form')!;
    fireEvent.change(screen.getByLabelText(/Key name/i), {target: {value: 'a2a-runner'}});
    fireEvent.click(screen.getAllByLabelText(/Allowed planes/i).find(el => issueForm.contains(el))!);
    fireEvent.click(await screen.findByRole('option', {name: /A2A \(\/a2a\/\*\)/i}));
    fireEvent.change(screen.getAllByLabelText(/Allowed route patterns/i).find(el => issueForm.contains(el))!, {target: {value: '/a2a/*'}});
    fireEvent.submit(issueForm);
    await waitFor(() => expect(createdKeyBody).not.toBeNull());
    const submittedKey = createdKeyBody as unknown as {allowed_planes?: string[]; allowed_routes?: string[]};
    expect(submittedKey.allowed_planes).toContain('A2A');
    expect(submittedKey.allowed_routes).toEqual(['/a2a/*']);
    fireEvent.click(await screen.findByRole('button', {name: /I stored it/i}));

    // DEF-006: Add Member as TEAM_ADMIN only exposes TEAM_ADMIN/DEVELOPER and passes project_ids: ['proj_1']
    rerender(
      wrap(
        <AccessPage
          pathname="/access/members"
          projectId="proj_1"
          projects={projects}
          environmentId="env_1"
          currentPrincipalId="usr_team_admin"
          roles={['TEAM_ADMIN']}
        />,
      ),
    );
    fireEvent.click((await screen.findAllByRole('button', {name: /Add member/i}))[0]!);
    fireEvent.change(await screen.findByLabelText(/Member email/i), {target: {value: 'dev@alpha.test'}});
    const addMemberForm = document.getElementById('tp-add-member-form')!;
    const rolesTrigger = addMemberForm.querySelector('[role="combobox"], button')!;
    fireEvent.click(rolesTrigger);
    expect(screen.queryByRole('option', {name: 'Org Owner'})).toBeNull();
    expect(screen.queryByRole('option', {name: 'Operator'})).toBeNull();
    expect(await screen.findByRole('option', {name: 'Developer'})).toBeInTheDocument();
    fireEvent.submit(document.getElementById('tp-add-member-form')!);
    await waitFor(() => expect(createdMemberBody).not.toBeNull());
    expect(createdMemberBody).toMatchObject({
      email: 'dev@alpha.test',
      roles: ['DEVELOPER'],
      project_ids: ['proj_1'],
    });
  });
});

describe('Engineering Round 2 defect regressions (DEF-016..DEF-039)', () => {
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

  it('DEF-016, DEF-021, DEF-023, DEF-029: loads members without env, passes project_id for TEAM_ADMIN, discloses enrollment token, and drops stale async responses', async () => {
    const memberQueries: string[] = [];
    const slowResolvers: Array<(value: Response) => void> = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/members' && method === 'GET') {
        memberQueries.push(parsed.searchParams.get('project_id') ?? '');
        return jsonResponse({
          data: [
            {
              id: 'usr_1',
              email: 'alice@tanpopo.test',
              kind: 'HUMAN',
              roles: ['DEVELOPER'],
              project_ids: ['proj_1'],
              has_password: false,
              enrollment_pending: true,
              version: 1,
              created_at: '2026-11-24T00:00:00Z',
            },
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/members' && method === 'POST') {
        return jsonResponse({
          id: 'usr_2',
          email: 'bob@tanpopo.test',
          kind: 'HUMAN',
          roles: ['DEVELOPER'],
          project_ids: ['proj_1'],
          enrollment_token: 'enr_one_time_secret_token_xyz',
          enrollment_expires_at: '2026-11-25T00:00:00Z',
          has_password: false,
          enrollment_pending: true,
          version: 1,
          created_at: '2026-11-24T00:00:00Z',
        }, 201);
      }
      if (parsed.pathname === '/admin/v1/virtual-keys') {
        const envParam = parsed.searchParams.get('env_id');
        if (envParam === 'env_1') {
          return new Promise<Response>(resolve => {
            slowResolvers.push(resolve);
          });
        }
        return jsonResponse({data: [{id: 'vk_2', name: 'fresh-env-key', key_prefix: 'tp_k', last_four: '9999', allowed_planes: ['PROVIDER'], allowed_routes: ['*'], version: 1, state: 'ACTIVE', created_at: '2026-11-24T00:00:00Z'}], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/traces' || parsed.pathname === '/admin/v1/models' || parsed.pathname === '/admin/v1/deployments') {
        return jsonResponse({data: [], has_more: false});
      }
      return jsonResponse({}, 404);
    });

    const projects = [{id: 'proj_1', org_id: 'org_1', slug: 'alpha', name: 'Alpha', version: 1, created_at: '2026-11-24T00:00:00Z'}];

    // Render /access/members as TEAM_ADMIN with NO environment selected -> members MUST still load with ?project_id=proj_1
    const {container, rerender} = render(
      wrap(
        <AccessPage
          pathname="/access/members"
          projectId=""
          projects={projects}
          environmentId=""
          currentPrincipalId="usr_ta"
          roles={['TEAM_ADMIN']}
        />,
      ),
    );
    await screen.findAllByText('alice@tanpopo.test');
    expect(screen.getAllByText(/Pending activation/i).length).toBeGreaterThanOrEqual(1);
    expect(memberQueries[0]).toBe('proj_1');
    expect((await axe.run(container)).violations).toEqual([]);

    // Adding local member shows one-time enrollment token & activation URL modal
    rerender(
      wrap(
        <AccessPage
          pathname="/access/members"
          projectId="proj_1"
          projects={projects}
          environmentId=""
          currentPrincipalId="usr_ta"
          roles={['TEAM_ADMIN']}
        />,
      ),
    );
    await screen.findAllByText('alice@tanpopo.test');
    rememberCsrfToken('csrf-r2');
    fireEvent.click(screen.getAllByRole('button', {name: /Add member/i})[0]!);
    fireEvent.change(await screen.findByLabelText(/Member email/i), {target: {value: 'bob@tanpopo.test'}});
    fireEvent.submit(document.getElementById('tp-add-member-form')!);
    expect((await screen.findAllByText((_c, node) => Boolean(node?.textContent?.includes('enr_one_time_secret_token_xyz')))).length).toBeGreaterThanOrEqual(1);
    fireEvent.click(screen.getByRole('button', {name: /Done/i}));

    // Switching quickly from env_1 (slow) to env_2 (fast) drops stale env_1 response
    rerender(
      wrap(
        <AccessPage
          pathname="/access/virtual-keys"
          projectId="proj_1"
          projects={projects}
          environmentId="env_1"
          currentPrincipalId="usr_ta"
          roles={['TEAM_ADMIN']}
        />,
      ),
    );
    rerender(
      wrap(
        <AccessPage
          pathname="/access/virtual-keys"
          projectId="proj_1"
          projects={projects}
          environmentId="env_2"
          currentPrincipalId="usr_ta"
          roles={['TEAM_ADMIN']}
        />,
      ),
    );
    await screen.findAllByText('fresh-env-key');
    slowResolvers.forEach(resolve =>
      resolve(jsonResponse({data: [{id: 'vk_1', name: 'stale-env-key', key_prefix: 'tp_k', last_four: '1111', allowed_planes: ['PROVIDER'], allowed_routes: ['*'], version: 1, state: 'ACTIVE', created_at: '2026-11-24T00:00:00Z'}], has_more: false})),
    );
    await waitFor(() => expect(screen.queryByText('stale-env-key')).toBeNull());
    expect(screen.getAllByText('fresh-env-key').length).toBeGreaterThanOrEqual(1);
  });

  it('DEF-021 & DEF-022: handles session-expired redirect banner, 401 unauthorized listener, and invite activation form on LoginPage', async () => {
    const unauthorizedEvents = vi.fn();
    const unsub = subscribeUnauthorized(unauthorizedEvents);
    let enrollSubmitted: Record<string, unknown> | null = null;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const pathname = new URL(rawUrl, window.location.origin).pathname;
      if (pathname === '/admin/v1/budgets') {
        return jsonResponse({error: {code: 'unauthorized', message: 'Session revoked'}}, 401);
      }
      if (pathname === '/admin/v1/auth/enroll') {
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        enrollSubmitted = JSON.parse(rawBody) as Record<string, unknown>;
        return jsonResponse({authenticated: true, principal_id: 'usr_2', roles: ['DEVELOPER'], csrf_token: 'csrf_enrolled'});
      }
      return jsonResponse({}, 404);
    });

    // Simulate API 401 firing unauthorized subscriber
    await import('../api/managementClient').then(m => m.listBudgets().catch(() => undefined));
    expect(unauthorizedEvents).toHaveBeenCalledTimes(1);
    unsub();

    // Render LoginPage with ?reason=session_expired&enroll=enr_demo_token_abc
    window.history.replaceState(null, '', '/login?reason=session_expired&enroll=enr_demo_token_abc');
    const {container} = render(wrap(<LoginPage />));
    expect(await screen.findByText(/Session ended/i)).toBeInTheDocument();
    expect((await axe.run(container)).violations).toEqual([]);

    const newPwInput = screen.getAllByLabelText(/^New password$/i).find(el => el.tagName === 'INPUT')!;
    const confirmPwInput = screen.getAllByLabelText(/Confirm password/i).find(el => el.tagName === 'INPUT')!;
    fireEvent.change(newPwInput, {target: {value: 'Short1!'}});
    fireEvent.change(confirmPwInput, {target: {value: 'Short1!'}});
    fireEvent.click(screen.getByRole('button', {name: /Activate account/i}));
    expect(await screen.findByText(/at least 12 characters/i)).toBeInTheDocument();

    fireEvent.change(newPwInput, {target: {value: 'TanpopoMember!2026'}});
    fireEvent.change(confirmPwInput, {target: {value: 'TanpopoMember!2026'}});
    fireEvent.click(screen.getByRole('button', {name: /Activate account/i}));
    await waitFor(() => expect(window.location.pathname).not.toBe('/login'));
    expect(enrollSubmitted).toMatchObject({token: 'enr_demo_token_abc'});
  });

  it('DEF-034 & DEF-020: disables, re-enables, and deletes webhooks safely in OperationsPage and emits security-state-changed signals', async () => {
    rememberCsrfToken('csrf-ops');
    const badgeListener = vi.fn();
    const unsub = subscribeSecurityStateChanged(badgeListener);
    let enabled = true;
    let deleted = false;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/webhooks' && method === 'GET') {
        return jsonResponse({
          data: deleted ? [] : [{id: 'whk_1', target_origin: 'https://hooks.example.test/ev', events: ['audit.alert'], signing_credential_id: 'sec_1', enabled, version: enabled ? 1 : 2}],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/webhooks/whk_1' && method === 'PATCH') {
        enabled = !enabled;
        return jsonResponse({id: 'whk_1', target_origin: 'https://hooks.example.test/ev', events: ['audit.alert'], signing_credential_id: 'sec_1', enabled, version: 2});
      }
      if (parsed.pathname === '/admin/v1/webhooks/whk_1' && method === 'DELETE') {
        deleted = true;
        return new Response(null, {status: 204});
      }
      if (parsed.pathname === '/admin/v1/secrets' || parsed.pathname === '/admin/v1/ops/dlq' || parsed.pathname === '/admin/v1/audit-events') {
        return jsonResponse({data: [], has_more: false});
      }
      return jsonResponse({}, 404);
    });

    notifySecurityStateChanged();
    expect(badgeListener).toHaveBeenCalledTimes(1);
    unsub();

    render(wrap(<OperationsPage pathname="/settings/residency-retention" projectId="proj_1" roles={['OPERATOR']} />));
    await screen.findAllByText('https://hooks.example.test/ev');
    fireEvent.click(screen.getAllByRole('button', {name: /Actions for https:\/\/hooks\.example\.test\/ev/i})[0]!);
    fireEvent.click(await screen.findByRole('menuitem', {name: /Disable webhook/i}));
    await screen.findByText(/Webhook disabled/i);

    fireEvent.click(screen.getAllByRole('button', {name: /Actions for https:\/\/hooks\.example\.test\/ev/i})[0]!);
    fireEvent.click(await screen.findByRole('menuitem', {name: /Delete webhook/i}));
    const confirmBtn = await screen.findByRole('button', {name: /^Delete webhook$/i});
    fireEvent.click(confirmBtn);
    await screen.findByText(/Webhook deleted/i);
  });

  it('DEF-017 & DEF-019: correlates Playground X-Tanpopo-Trace-Id, ends trace polling cleanly, and shows FinOps budget link on 402 budget rejection', async () => {
    let callCount = 0;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const pathname = new URL(rawUrl, window.location.origin).pathname;
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (pathname === '/admin/v1/routes') {
        return jsonResponse({data: [{id: 'rt_1', slug: 'chat-default', strategy: 'PRIORITY_FAILOVER', max_failover_hops: 1, request_timeout_ms: 5000, targets: []}], has_more: false});
      }
      if (pathname === '/admin/v1/playground/sessions' && method === 'POST') {
        return jsonResponse({session_id: 'pgs_1', ephemeral_virtual_key: 'tp_ephemeral_1', max_budget_micros: 100000, expires_at: '2026-11-24T08:00:00Z'}, 201);
      }
      if (pathname === '/v1/chat/completions') {
        callCount += 1;
        if (callCount === 1) {
          return new Response('data: {"choices":[{"delta":{"content":"pong"}}]}\n\ndata: [DONE]\n\n', {
            status: 200,
            headers: {'Content-Type': 'text/event-stream', 'X-Tanpopo-Trace-Id': 'trc_correlation_123'},
          });
        }
        return jsonResponse({error: {code: 'budget_exceeded', message: 'Project hard ceiling exceeded'}}, 402);
      }
      if (pathname === '/admin/v1/traces/trc_correlation_123') {
        return jsonResponse({}, 404);
      }
      return jsonResponse({}, 404);
    });

    rememberCsrfToken('csrf-pg');
    render(wrap(<PlaygroundPage pathname="/devtools/playground" projectId="proj_1" environmentId="env_1" roles={['DEVELOPER']} />));
    const promptInput = await screen.findByPlaceholderText(/Enter a test prompt/i);
    fireEvent.change(promptInput, {target: {value: 'hello'}});
    await waitFor(() => expect(screen.getByRole('button', {name: /^Send$/i})).not.toBeDisabled());
    fireEvent.click(screen.getByRole('button', {name: /^Send$/i}));
    expect(await screen.findByText(/trc_correlation_123/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText(/Trace not available yet/i)).toBeInTheDocument(), {timeout: 3000});

    fireEvent.change(promptInput, {target: {value: 'second'}});
    await waitFor(() => expect(screen.getByRole('button', {name: /^Send$/i})).not.toBeDisabled());
    fireEvent.click(screen.getByRole('button', {name: /^Send$/i}));
    expect(await screen.findByText(/Project hard ceiling exceeded/)).toBeInTheDocument();
    expect(screen.getByRole('button', {name: /Inspect budgets/i})).toBeInTheDocument();
  });

  it('DEF-018, DEF-024, DEF-033: renders ancestor org budget read-only for TEAM_ADMIN, supports TOTAL_LIFETIME, and prevents duplicate create overwrite', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      if (parsed.pathname === '/admin/v1/budgets') {
        expect(parsed.searchParams.get('include_ancestors')).toBe('true');
        return jsonResponse({
          data: [
            {id: 'bgt_org', org_id: 'org_1', scope_tier: 'ORGANIZATION', scope_id: 'org_1', period: 'MONTHLY', hard_limit_micros: 500000000, soft_warning_percents: [80], reserved_hold_micros: 0, settled_exact_micros: 100, settled_estimated_micros: 0, version: 3},
            {id: 'bgt_proj', org_id: 'org_1', scope_tier: 'PROJECT', scope_id: 'proj_1', period: 'TOTAL_LIFETIME', hard_limit_micros: 100000000, soft_warning_percents: [50, 80, 95], reserved_hold_micros: 0, settled_exact_micros: 50, settled_estimated_micros: 0, version: 2},
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/usage/chargeback') return jsonResponse({data: [], has_more: false});
      if (parsed.pathname === '/admin/v1/virtual-keys') return jsonResponse({data: [], has_more: false});
      return jsonResponse({}, 404);
    });

    const projects = [{id: 'proj_1', org_id: 'org_1', slug: 'alpha', name: 'Alpha', version: 1, created_at: '2026-11-24T00:00:00Z'}];
    render(wrap(<FinOpsPage pathname="/finops/budgets" activeOrgId="org_1" projects={projects} projectId="proj_1" roles={['TEAM_ADMIN']} />));

    expect((await screen.findAllByText(/Inherited ceiling \(read-only\)/i)).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText(/Total lifetime/i).length).toBeGreaterThanOrEqual(1);
    // Desktop + mobile views render Save button once per row -> 2 total Save buttons for the single editable project row (none for org inherited row)
    expect(screen.getAllByRole('button', {name: /^Save$/i})).toHaveLength(2);

    fireEvent.click(screen.getByRole('button', {name: /Create budget/i}));
    const budgetForm = document.getElementById('tp-create-budget-form')!;
    const periodTrigger = screen.getAllByLabelText(/^Period$/i).find(el => budgetForm.contains(el) && el.tagName === 'BUTTON')!;
    fireEvent.click(periodTrigger);
    fireEvent.click(await screen.findByRole('option', {name: /Total lifetime/i}));
    fireEvent.submit(budgetForm);
    expect(await screen.findByText(/already exists for this scope and period/i)).toBeInTheDocument();
    expect(screen.getByRole('button', {name: /Update existing budget/i})).toBeInTheDocument();
  });

  it('DEF-027, DEF-028, DEF-039: renders Project/Org policies without env, disambiguates duplicate model deployments, and omits forced openai dialect', async () => {
    let lastExplainBody: Record<string, unknown> | null = null;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      if (parsed.pathname === '/admin/v1/policies/effective') {
        return jsonResponse({id: 'pol_proj', org_id: 'org_1', scope_tier: parsed.searchParams.get('project_id') ? 'PROJECT' : 'ORGANIZATION', allowed_data_regions: ['US'], allowed_models: ['*'], min_plugin_trust: 'ANY_APPROVED', min_capability_assurance: 'DECLARED', guardrail_hooks: [], version: 1});
      }
      if (parsed.pathname === '/admin/v1/models') return jsonResponse({data: [{id: 'm1', slug: 'llama-3', display_name: 'Llama 3', context_window: 8192, max_output_tokens: 2048, assurance_tier: 'TESTED', capabilities: {}}], has_more: false});
      if (parsed.pathname === '/admin/v1/connections') return jsonResponse({data: [{id: 'con_east', name: 'Ollama East'}, {id: 'con_west', name: 'Ollama West'}], has_more: false});
      if (parsed.pathname === '/admin/v1/routes' && parsed.searchParams.get('env_id')) {
        return jsonResponse({
          data: [{id: 'rt_1', slug: 'chat', env_id: 'env_1', strategy: 'PRIORITY_FAILOVER', max_failover_hops: 2, request_timeout_ms: 5000, allowed_data_regions: ['US'], version: 1, targets: [{deployment_id: 'dep_aaaa1111', priority_order: 1}, {deployment_id: 'dep_bbbb2222', priority_order: 2}]}],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/deployments') {
        return jsonResponse({
          data: [
            {id: 'dep_aaaa1111', env_id: 'env_1', logical_model_id: 'm1', connection_id: 'con_east', upstream_model_name: 'llama3:8b', data_region: 'US', state: 'ACTIVE_HEALTHY', version: 1},
            {id: 'dep_bbbb2222', env_id: 'env_1', logical_model_id: 'm1', connection_id: 'con_west', upstream_model_name: 'llama3:8b', data_region: 'US', state: 'ACTIVE_HEALTHY', version: 1},
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/traces') return jsonResponse({data: [], has_more: false});
      if (parsed.pathname === '/admin/v1/routes/explain') {
        const rawBody = input instanceof Request ? await input.clone().text() : String(init?.body ?? '{}');
        lastExplainBody = JSON.parse(rawBody) as Record<string, unknown>;
        return jsonResponse({dry_run: true, route_slug: 'chat', selected_candidates_ordered: [], rejected_deployments: []});
      }
      return jsonResponse({}, 404);
    });

    // DEF-039: /policies without environmentId still displays Effective Policy table
    const {rerender} = render(wrap(<RoutingPage pathname="/policies" projectId="proj_1" environmentId="" roles={['ORG_OWNER']} />));
    expect(await screen.findByText(/Effective resolved policy/i)).toBeInTheDocument();

    // DEF-027: Routes table disambiguates identical upstream_model_name using connection name + short ID
    rerender(wrap(<RoutingPage pathname="/routes" projectId="proj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    expect((await screen.findAllByText(/Ollama East \/ llama3:8b · aa1111 → Ollama West \/ llama3:8b · bb2222/)).length).toBeGreaterThanOrEqual(1);

    // DEF-028: Explain omits forced openai_compatible dialect when Unified normalized is selected
    rememberCsrfToken('csrf-routes');
    rerender(wrap(<RoutingPage pathname="/routes/explain" projectId="proj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    const explainTarget = await screen.findByRole('combobox', {name: /Target model or route slug/i});
    fireEvent.focus(explainTarget);
    fireEvent.change(explainTarget, {target: {value: 'llama-3'}});
    fireEvent.blur(explainTarget);
    const regionTrigger = screen.getAllByLabelText(/Data residency/i).find(el => el.tagName === 'BUTTON')!;
    fireEvent.click(regionTrigger);
    fireEvent.click(await screen.findByRole('option', {name: 'US'}));
    fireEvent.click(screen.getByRole('button', {name: /Explain route/i}));
    await waitFor(() => expect(lastExplainBody).not.toBeNull());
    expect(lastExplainBody).not.toHaveProperty('dialect');
  });

  it('DEF-025, DEF-026, DEF-031, DEF-032, DEF-035, DEF-036: covers in-drawer env deployment & CIDR checks, MCP initial sync count, connection short IDs, and marketplace reconciliation', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      if (parsed.pathname === '/admin/v1/models') return jsonResponse({data: [{id: 'm1', slug: 'gpt-4o', display_name: 'GPT-4o', context_window: 128000, max_output_tokens: 4096, assurance_tier: 'VERIFIED', capabilities: {}}], has_more: false});
      if (parsed.pathname === '/admin/v1/deployments') return jsonResponse({data: [{id: 'dep_1', env_id: 'env_1', logical_model_id: 'm1', connection_id: 'con_aabbccdd', upstream_model_name: 'gpt-4o', data_region: 'US', state: 'ACTIVE_HEALTHY', version: 1, rate_card: {input_token_nano_usd: 100, output_token_nano_usd: 200}}], has_more: false});
      if (parsed.pathname === '/admin/v1/connections') return jsonResponse({data: [{id: 'con_aabbccdd', name: 'OpenAI Primary', adapter_id: 'builtin:openai', billing_mode: 'COMMERCIAL_API', endpoint_url: 'https://api.openai.com/v1', data_region: 'US', state: 'ACTIVE', secret_slot_fingerprints: {}, version: 1}], has_more: false});
      if (parsed.pathname === '/admin/v1/secrets' || parsed.pathname === '/admin/v1/provider-adapters' || parsed.pathname === '/admin/v1/mcp/servers' || parsed.pathname === '/admin/v1/a2a/agents' || parsed.pathname === '/admin/v1/a2a/tasks' || parsed.pathname === '/admin/v1/virtual-keys') {
        return jsonResponse({data: [], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/extensions') {
        return jsonResponse({data: [{id: 'ext_1', package_id: '@tanpopo/openai', name: 'OpenAI Adapter', active_version: '1.0.0', state: 'ENABLED_ACTIVE', origin_trust_tier: 'official', execution_tier: 'declarative', version: 1}], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/marketplace/catalog') {
        return jsonResponse({data: [{id: '@tanpopo/openai', package_id: '@tanpopo/openai', canonical_name: 'OpenAI Adapter', version: '1.0.0', origin_trust_tier: 'official', enterprise_signed: true, execution_tier: 'declarative', capability_types: ['provider'], summary: 'Official'}], offline_cached: true});
      }
      return jsonResponse({}, 404);
    });

    const envs = [{id: 'env_1', org_id: 'org_1', project_id: 'proj_1', slug: 'prod', name: 'Production', created_at: '2026-11-24T00:00:00Z'}];
    // DEF-025 & DEF-035: ModelsPage Add deployment shows environment selector in-drawer when no env preselected
    const {rerender} = render(wrap(<ModelsPage environments={envs} environmentId="" roles={['ORG_OWNER']} />));
    expect(await screen.findByText(/All environment deployments/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', {name: /Add deployment/i}));
    const depForm = document.getElementById('tp-add-deployment-form')!;
    expect(await waitFor(() => screen.getAllByLabelText(/^Environment$/i).find(el => depForm.contains(el)))).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', {name: /Cancel/i}));

    // DEF-032: ProvidersPage shows distinguishing short ID on connection row
    rerender(wrap(<ProvidersPage pathname="/providers/connections" projectId="proj_1" roles={['ORG_OWNER']} />));
    expect((await screen.findAllByText(/aabbccdd/)).length).toBeGreaterThanOrEqual(1);

    // DEF-031: McpHubPage shows invalid CIDR error inside drawer
    rerender(wrap(<McpHubPage pathname="/mcp/servers" projectId="proj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    fireEvent.click((await screen.findAllByRole('button', {name: /Register server/i}))[0]!);
    const mcpForm = document.getElementById('tp-register-mcp-form')!;
    fireEvent.change(await screen.findByLabelText(/Server slug/i), {target: {value: 'local-mcp'}});
    fireEvent.change(screen.getAllByLabelText(/Display name/i).find(el => mcpForm.contains(el))!, {target: {value: 'Local MCP'}});
    fireEvent.change(screen.getAllByLabelText(/Endpoint URL/i).find(el => mcpForm.contains(el))!, {target: {value: 'http://127.0.0.1:9000/mcp'}});
    fireEvent.click(mcpForm.querySelector('input[type="checkbox"]')!);
    const mcpCidrInp = (await screen.findAllByLabelText(/Allowed CIDRs/i)).find(el => mcpForm.contains(el))!;
    fireEvent.change(mcpCidrInp, {target: {value: '127.0.0.1/33'}});
    fireEvent.submit(mcpForm);
    expect(await screen.findByText(/explicit non-wildcard CIDRs/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', {name: /Cancel/i}));

    // DEF-036: Marketplace reconciles installed @tanpopo/openai and shows Installed instead of Stage for review
    rerender(wrap(<ExtensionsPage pathname="/extensions/marketplace" roles={['OPERATOR']} />));
    expect((await screen.findAllByText(/^Installed$/i)).length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByRole('button', {name: /Stage for review/i})).toBeNull();
  });

  it('uses backend secret_kind enums in ProvidersPage, keeps ConfirmDialog cancelable when confirmDisabled, and scopes OperationsPage HMAC secrets', async () => {
    const onClose = vi.fn();
    const onConfirm = vi.fn();
    const {rerender} = render(
      wrap(
        <ConfirmDialog
          open
          onClose={onClose}
          title="Clear safeguard"
          description="Provide a reason"
          confirmLabel="Clear"
          confirmDisabled
          onConfirm={onConfirm}
        >
          <input aria-label="Reason" defaultValue="no" />
        </ConfirmDialog>,
      ),
    );
    const cancelBtn = screen.getByRole('button', {name: /Cancel/i});
    const confirmBtn = screen.getByRole('button', {name: /^Clear$/i});
    expect(cancelBtn).not.toBeDisabled();
    expect(cancelBtn).not.toHaveAttribute('aria-disabled', 'true');
    expect(confirmBtn).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(confirmBtn);
    expect(onConfirm).not.toHaveBeenCalled();
    fireEvent.click(cancelBtn);
    expect(onClose).toHaveBeenCalledTimes(1);

    let postedSecretKind = '';
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const rawUrl = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      const parsed = new URL(rawUrl, window.location.origin);
      const method = (input instanceof Request ? input.method : init?.method) ?? 'GET';
      if (parsed.pathname === '/admin/v1/secrets' && method === 'POST') {
        const body = input instanceof Request
          ? (await input.clone().json() as {secret_kind?: string})
          : (JSON.parse(String(init?.body ?? '{}')) as {secret_kind?: string});
        postedSecretKind = body.secret_kind ?? '';
        return jsonResponse({
          credential_id: 'sec_new', name: 'sigv4', project_id: 'proj_1',
          secret_kind: 'AWS_SIGV4_KEY', trailing_fingerprint: '9999', version: 1,
          created_at: '2026-11-24T00:00:00Z', updated_at: '2026-11-24T00:00:00Z',
        }, 201);
      }
      if (parsed.pathname === '/admin/v1/secrets') {
        return jsonResponse({
          data: [
            {credential_id: 'sec_p1', name: 'Proj1 HMAC', project_id: 'proj_1', secret_kind: 'WEBHOOK_HMAC_SECRET', trailing_fingerprint: '1111', version: 1, created_at: '2026-11-24T00:00:00Z', updated_at: '2026-11-24T00:00:00Z'},
            {credential_id: 'sec_p2', name: 'Proj2 HMAC', project_id: 'proj_2', secret_kind: 'WEBHOOK_HMAC_SECRET', trailing_fingerprint: '2222', version: 1, created_at: '2026-11-24T00:00:00Z', updated_at: '2026-11-24T00:00:00Z'},
          ],
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/connections' || parsed.pathname === '/admin/v1/provider-adapters' || parsed.pathname === '/admin/v1/webhooks') {
        return jsonResponse({data: [], has_more: false});
      }
      if (parsed.pathname === '/admin/v1/ops/cluster-status') return jsonResponse({ha_mode: 'SINGLE_NODE', peers: []});
      if (parsed.pathname === '/admin/v1/extensions/lockfile') return jsonResponse({extensions: []});
      if (parsed.pathname === '/admin/v1/audit-events') return jsonResponse({data: [], has_more: false});
      return jsonResponse({}, 404);
    });

    rememberCsrfToken('csrf-test');
    rerender(wrap(<ProvidersPage pathname="/providers/connections" projectId="proj_1" roles={['ORG_OWNER']} />));
    fireEvent.click(await screen.findByRole('tab', {name: /Vault Secrets/i}));
    fireEvent.click((await screen.findAllByRole('button', {name: /Seal secret/i}))[0]!);
    const sealForm = document.getElementById('tp-seal-secret-form')!;
    fireEvent.change(screen.getAllByLabelText(/Credential name/i).find(el => el.tagName === 'INPUT' && sealForm.contains(el))!, {target: {value: 'sigv4'}});
    fireEvent.click(sealForm.querySelector('[role="combobox"]')!);
    fireEvent.click(await screen.findByRole('option', {name: /AWS IAM Credentials/i}));
    fireEvent.change(screen.getAllByLabelText(/Secret value/i).find(el => el.tagName === 'INPUT' && sealForm.contains(el))!, {target: {value: '{"access_key_id":"a","secret_access_key":"b"}'}});
    fireEvent.click(screen.getByRole('button', {name: /Seal credential/i}));
    await waitFor(() => expect(postedSecretKind).toBe('AWS_SIGV4_KEY'));

    rerender(wrap(<OperationsPage pathname="/settings/residency-retention" projectId="proj_1" roles={['OPERATOR']} />));
    fireEvent.click((await screen.findAllByRole('button', {name: /Add webhook/i}))[0]!);
    const whForm = document.getElementById('tp-register-webhook-form')!;
    const secCombo = Array.from(whForm.querySelectorAll('[role="combobox"]'))[0]!;
    fireEvent.click(secCombo);
    expect(await screen.findByRole('option', {name: /Proj1 HMAC/i})).toBeInTheDocument();
    expect(screen.queryByRole('option', {name: /Proj2 HMAC/i})).toBeNull();

    // Verify fragment #enroll= token populates form and scrubs sensitive token from window.location.
    window.history.replaceState(null, '', '/login#enroll=tpen_fragment_secret');
    rerender(wrap(<LoginPage />));
    expect(await screen.findByDisplayValue('tpen_fragment_secret')).toBeInTheDocument();
    await waitFor(() => expect(window.location.hash).not.toContain('tpen_fragment_secret'));
  });
});

