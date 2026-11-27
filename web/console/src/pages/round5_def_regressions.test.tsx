import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken} from '../api/client';
import {I18nProvider} from '../i18n/I18nProvider';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {ConsoleShell} from '../shell/ConsoleShell';
import {AccessPage} from './access/AccessPage';
import {McpHubPage} from './mcp/McpHubPage';
import {OverviewPage} from './overview/OverviewPage';
import {RoutingPage} from './routes/RoutingPage';

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

describe('Engineering Round 5 UI regressions (TEEP-00)', () => {
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

  it('DEF-082: quarantine, unquarantine, and extension kill-switch emit security state notifications that refresh header badge without reload', async () => {
    rememberCsrfToken('csrf-def082');
    let switches = [{id: 'ks_1', org_id: 'org_1', level: 'EXTENSION', target_id: 'ext_1', reason: 'initial switch', version: 1, active: true, created_at: '2026-11-27T00:00:00Z'}];
    let instState = 'ACTIVE';
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      const parsed = new URL(url, 'http://localhost');
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/auth/session') {
        return jsonResponse({
          principal_id: 'usr_1',
          email: 'owner@example.com',
          org_id: 'org_1',
          org_name: 'Default',
          roles: ['OPERATOR', 'ORG_OWNER'],
          requires_password_rotation: false,
          csrf_token: 'csrf-def082',
        });
      }
      if (parsed.pathname === '/admin/v1/bootstrap/status') {
        return jsonResponse({completed: true, offline_mode: false, multi_org_enabled: false});
      }
      if (parsed.pathname === '/admin/v1/organizations') {
        return jsonResponse({data: [{id: 'org_1', slug: 'default', name: 'Default', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', four_eyes_enabled: false, version: 1, created_at: '2026-11-27T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/projects') {
        return jsonResponse({data: [{id: 'prj_1', org_id: 'org_1', slug: 'default', name: 'Default', version: 1, created_at: '2026-11-27T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname.endsWith('/environments')) {
        return jsonResponse({data: [{id: 'env_1', org_id: 'org_1', project_id: 'prj_1', slug: 'prod', name: 'Production', version: 1, created_at: '2026-11-27T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/kill-switches' && method === 'GET') {
        return jsonResponse({data: switches, next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/approvals') {
        return jsonResponse({data: [], next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/extensions/installations' && method === 'GET') {
        return jsonResponse({
          data: [{
            id: 'ext_1',
            package_id: 'ext.provider.openai',
            name: 'OpenAI Provider',
            origin_trust_tier: 'OFFICIAL',
            execution_tier: 'IN_PROCESS_BUILTIN',
            state: instState,
            active_version: '1.0.0',
            etag: '"v1"',
            version: 1,
            created_at: '2026-11-27T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/marketplace/catalog') {
        return jsonResponse({data: [], offline_cached: false, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/extensions/ext_1/quarantine' && method === 'POST') {
        instState = 'QUARANTINED';
        switches = [
          ...switches,
          {id: 'ks_2', org_id: 'org_1', level: 'EXTENSION', target_id: 'ext_1', reason: 'quarantine', version: 1, active: true, created_at: '2026-11-27T00:00:00Z'},
        ];
        return jsonResponse({id: 'ext_1', state: 'QUARANTINED', version: 2});
      }
      if (parsed.pathname === '/admin/v1/extensions/ext_1/unquarantine' && method === 'POST') {
        instState = 'ACTIVE';
        switches = [];
        return jsonResponse({id: 'ext_1', state: 'ACTIVE', version: 3});
      }
      if (parsed.pathname === '/admin/v1/kill-switches' && method === 'POST') {
        const created = {id: 'ks_3', org_id: 'org_1', level: 'EXTENSION', target_id: 'ext_1', reason: 'Emergency switch', version: 1, active: true, created_at: '2026-11-27T00:00:00Z'};
        switches = [created];
        return jsonResponse({...created, config_epoch: 2, affected_entities_count: 1, severed_streams_count: 0});
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    window.history.replaceState(null, '', '/extensions/installed');
    render(wrap(<ConsoleShell />));

    await screen.findByRole('button', {name: /Kill Switch.*1/i});
    await screen.findAllByText(/ext\.provider\.openai/);

    const {quarantineExtension, unquarantineExtension, triggerExtensionKillSwitch} = await import('../api/catalogClient');
    await quarantineExtension('ext_1', '"v1"', 'Investigating extension issue');
    await screen.findByRole('button', {name: /Kill Switch.*2/i});

    await unquarantineExtension('ext_1', '"v2"', 'Verified clean after inspection');
    await waitFor(() => {
      expect(screen.queryByRole('button', {name: /Kill Switch.*[12]/i})).toBeNull();
    });

    await triggerExtensionKillSwitch('ext_1', 'Emergency switch', 'ext_1');
    await screen.findByRole('button', {name: /Kill Switch.*1/i});
  });

  it('DEF-084: OverviewPage applies overflow-wrap anywhere to long unbroken alert summaries without axe violations', async () => {
    rememberCsrfToken('csrf-def084');
    const longSummary = 'Budget_exceeded_alert_for_very_long_console_budget_identifier_0123456789abcdefghijklmnopqrstuvwxyz_without_spaces';
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      if (url.includes('/admin/v1/overview/summary')) {
        return jsonResponse({
          window: '24h',
          requests_total: 12,
          requests_failed: 1,
          p95_latency_ms: 42,
          total_cost_micros: 5000,
          active_virtual_keys: 2,
          active_connections: 1,
        });
      }
      if (url.includes('/admin/v1/alerts')) {
        return jsonResponse({
          data: [{
            id: 'alt_1',
            severity: 'PAGE',
            alert_code: 'BUDGET_BREACH',
            summary: longSummary,
            remediation_route: '/finops',
            triggered_at: '2026-11-27T00:00:00Z',
          }],
        });
      }
      return jsonResponse({data: [], has_more: false});
    });

    const {container} = render(wrap(<OverviewPage />));
    const summaryEl = await screen.findByText(longSummary);
    expect(summaryEl.style.overflowWrap).toBe('anywhere');
    expect(summaryEl.style.minWidth).toBe('0px');
    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-080: AccessPage renders explicit restricted notice without zero counts or empty-key false state on 403', async () => {
    rememberCsrfToken('csrf-def080');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      if (url.includes('/admin/v1/virtual-keys')) {
        return jsonResponse({error: {code: 'forbidden', message: 'role cannot list virtual keys'}}, 403);
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    const {container} = render(
      wrap(
        <AccessPage
          pathname="/access/virtual-keys"
          projectId="prj_1"
          projects={[{id: 'prj_1', org_id: 'org_1', slug: 'default', name: 'Default', version: 1, created_at: '2026-11-27T00:00:00Z'}]}
          environmentId="env_1"
          currentPrincipalId="usr_finops"
          roles={['FINOPS_VIEWER']}
        />,
      ),
    );

    await screen.findByText('Virtual Keys restricted');
    expect(screen.getByText('Your role cannot list Virtual Keys in this environment.')).toBeTruthy();
    expect(screen.queryByText('No Virtual Keys in this environment')).toBeNull();
    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-022: ConsoleShell revalidates admin session on 403 and SecurityPage tab navigation after role downgrade', async () => {
    rememberCsrfToken('csrf-def022');
    let currentRoles = ['SECURITY_REVIEWER'];
    let downgraded = false;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      const parsed = new URL(url, 'http://localhost');
      if (parsed.pathname === '/admin/v1/auth/session') {
        return jsonResponse({
          principal_id: 'usr_rev',
          email: 'reviewer@example.com',
          org_id: 'org_1',
          org_name: 'Default',
          roles: currentRoles,
          requires_password_rotation: false,
          csrf_token: 'csrf-def022',
        });
      }
      if (parsed.pathname === '/admin/v1/bootstrap/status') {
        return jsonResponse({completed: true, offline_mode: false, multi_org_enabled: false});
      }
      if (parsed.pathname === '/admin/v1/organizations') {
        return jsonResponse({data: [{id: 'org_1', slug: 'default', name: 'Default', status: 'ACTIVE', min_plugin_trust: 'ANY_APPROVED', four_eyes_enabled: false, version: 1, created_at: '2026-11-27T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/projects') {
        return jsonResponse({data: [{id: 'prj_1', org_id: 'org_1', slug: 'default', name: 'Default', version: 1, created_at: '2026-11-27T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname.endsWith('/environments')) {
        return jsonResponse({data: [{id: 'env_1', org_id: 'org_1', project_id: 'prj_1', slug: 'prod', name: 'Production', version: 1, created_at: '2026-11-27T00:00:00Z'}], next_cursor: null, has_more: false});
      }
      if (parsed.pathname === '/admin/v1/audit-events' || parsed.pathname === '/admin/v1/kill-switches' || parsed.pathname === '/admin/v1/approvals') {
        if (downgraded) {
          return jsonResponse({error: {code: 'forbidden', message: 'Security role revoked'}}, 403);
        }
        return jsonResponse({data: [], next_cursor: null, has_more: false});
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    window.history.replaceState(null, '', '/security/approvals');
    render(wrap(<ConsoleShell />));
    await screen.findByText('Approval queue clear');

    downgraded = true;
    currentRoles = ['FINOPS_VIEWER'];
    fireEvent.click(screen.getByRole('tab', {name: /Kill switches/i}));

    await screen.findByText('Access restricted');
    expect(screen.queryByText('Approval queue clear')).toBeNull();
  });

  it('DEF-069: McpHubPage displays truthful downstream_cancel_status notice and row status without axe violations', async () => {
    rememberCsrfToken('csrf-def069');
    let canceledDone = false;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      const parsed = new URL(url, 'http://localhost');
      const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
      if (parsed.pathname === '/admin/v1/a2a/tasks/task_1/cancel' && method === 'POST') {
        canceledDone = true;
        return jsonResponse({
          task_id: 'task_1',
          state: 'canceled',
          downstream_cancel_status: 'acknowledged',
          downstream_cancel_detail: 'downstream acknowledged',
          hop_count: 1,
          enforced_region: 'US',
          sub_budget_hold_micros: 1000,
          settled_cost_micros: 250,
          target_agent_id: 'agt_1',
          target_agent_slug: 'billing-analyst',
          created_at: '2026-11-27T00:00:00Z',
        });
      }
      if (parsed.pathname === '/admin/v1/a2a/tasks') {
        return jsonResponse({
          data: [{
            task_id: 'task_1',
            state: canceledDone ? 'canceled' : 'input-required',
            downstream_cancel_status: canceledDone ? 'acknowledged' : '',
            downstream_cancel_detail: canceledDone ? 'downstream acknowledged' : '',
            hop_count: 1,
            enforced_region: 'US',
            sub_budget_hold_micros: 1000,
            settled_cost_micros: canceledDone ? 250 : 0,
            target_agent_id: 'agt_1',
            target_agent_slug: 'billing-analyst',
            created_at: '2026-11-27T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    const {container} = render(wrap(<McpHubPage pathname="/a2a" projectId="prj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    const cancelBtns = await screen.findAllByRole('button', {name: 'Cancel'});
    fireEvent.click(cancelBtns[0]!);

    await screen.findByText('Canceled A2A task; downstream acknowledged and budget settled.');
    expect((await screen.findAllByText('Downstream cancel acknowledged')).length).toBeGreaterThanOrEqual(1);
    const res = await axe.run(container);
    expect(res.violations).toHaveLength(0);
  });

  it('DEF-050: RoutingPage capability warning names affected target deployment missing tool_calling', async () => {
    rememberCsrfToken('csrf-def050');
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
      const parsed = new URL(url, 'http://localhost');
      if (parsed.pathname === '/admin/v1/routes') {
        return jsonResponse({
          data: [{
            id: 'rt_1',
            slug: 'chat-primary',
            env_id: 'env_1',
            strategy: 'PRIORITY_FAILOVER',
            max_failover_hops: 1,
            request_timeout_ms: 15000,
            allowed_data_regions: [],
            targets: [{deployment_id: 'dep_no_tools', priority_order: 1}],
            version: 1,
            created_at: '2026-11-27T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/deployments') {
        return jsonResponse({
          data: [{
            id: 'dep_no_tools',
            env_id: 'env_1',
            logical_model_id: 'mdl_1',
            connection_id: 'con_1',
            upstream_model_name: 'llama3-plain',
            data_region: 'US',
            state: 'ACTIVE_HEALTHY',
            connection_state: 'ACTIVE',
            version: 1,
            created_at: '2026-11-27T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/models') {
        return jsonResponse({
          data: [{
            id: 'mdl_1',
            slug: 'llama3-plain',
            display_name: 'Llama 3 Plain',
            capabilities: {tool_calling: false, streaming: true, json_mode: false, vision: false, embeddings: false},
            assurance_tier: 'TESTED',
            version: 1,
            created_at: '2026-11-27T00:00:00Z',
          }],
          next_cursor: null,
          has_more: false,
        });
      }
      if (parsed.pathname === '/admin/v1/policies/effective') {
        return jsonResponse({
          id: 'pol_1',
          scope_type: 'ENVIRONMENT',
          scope_id: 'env_1',
          version: 1,
          min_plugin_trust: 'ANY_APPROVED',
          min_capability_assurance: 'DECLARED',
          allowed_data_regions: [],
          allowed_models: [],
          guardrail_extensions: [],
        });
      }
      return jsonResponse({data: [], next_cursor: null, has_more: false});
    });

    render(wrap(<RoutingPage pathname="/routes" projectId="prj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    const actionsBtns = await screen.findAllByRole('button', {name: /Actions for chat-primary/i});
    fireEvent.click(actionsBtns[0]!);
    const editItems = await screen.findAllByRole('menuitem', {name: /Edit/i});
    fireEvent.click(editItems[0]!);

    await screen.findByText(/Target deployments missing declared tool_calling capability: llama3-plain\./i);
  });
});
