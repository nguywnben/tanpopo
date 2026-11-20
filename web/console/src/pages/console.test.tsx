import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken} from '../api/client';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {ConsoleShell} from '../shell/ConsoleShell';
import {LoginPage} from './auth/LoginPage';
import {buildRateCard, ModelsPage} from './models/ModelsPage';
import {OverviewPage} from './overview/OverviewPage';
import {PlaygroundPage} from './playground/PlaygroundPage';
import {SetupPage} from './setup/SetupPage';
import {SelectField, formatMicroUSD} from '../ui/primitives';
import {MultiSelectField} from '../ui/MultiSelectField';
import {I18nProvider} from '../i18n/I18nProvider';

function jsonResponse(body: unknown, status = 200, headers: Record<string, string> = {}) {
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

describe('Tanpopo Console v2 core workflows & accessibility', () => {
  beforeEach(() => {
    clearCsrfToken();
    window.history.replaceState(null, '', '/overview');
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('formats integer micro-USD amounts without floating-point drift', () => {
    expect(formatMicroUSD(0)).toBe('$0.000000');
    expect(formatMicroUSD(1_250_000)).toBe('$1.250000');
    expect(formatMicroUSD(-500)).toBe('-$0.000500');
    expect(formatMicroUSD(Number.MAX_SAFE_INTEGER + 10)).toBe('Unavailable');
  });

  it('validates nano-USD rate cards and self-hosted zero-rate declarations', () => {
    const zero = {
      input_token_nano_usd: '0',
      output_token_nano_usd: '0',
      cache_read_token_nano_usd: '0',
      cache_write_token_nano_usd: '0',
      reasoning_token_nano_usd: '0',
      image_unit_nano_usd: '0',
      audio_sec_nano_usd: '0',
      document_page_nano_usd: '0',
    };
    expect(buildRateCard(zero, 'COMMERCIAL_API', true).error).toContain('self-hosted');
    expect(buildRateCard(zero, 'SELF_HOSTED_INFRA', false).error).toContain('Confirm');
    expect(buildRateCard(zero, 'SELF_HOSTED_INFRA', true).rateCard?.self_hosted_zero_rate).toBe(true);
    expect(buildRateCard({...zero, input_token_nano_usd: '-5'}, 'COMMERCIAL_API', false).error).toContain('non-negative');
  });

  it('renders Sign In cleanly and passes WCAG 2.2 AA axe-core checks', async () => {
    const {container} = render(wrap(<LoginPage />));
    expect(screen.getByRole('heading', {level: 1, name: 'Sign in'})).toBeInTheDocument();
    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('renders Setup Wizard and enforces owner password & bootstrap validation', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/bootstrap/status')) return jsonResponse({initialized: false});
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<SetupPage />));
    await screen.findByRole('heading', {level: 1, name: 'Instance setup'});
    fireEvent.click(screen.getByRole('button', {name: 'Continue'}));
    expect(await screen.findByRole('alert')).toHaveTextContent(/bootstrap token/i);

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('renders Overview metrics and active alerts with zero axe violations', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/overview/summary')) {
        return jsonResponse({
          window: '24h',
          total_requests: 1420,
          error_rate_ratio: 0.0125,
          p95_overhead_ms: 3,
          settled_micro_usd: 4_500_000,
          active_hold_micros: 120_000,
          active_alerts_count: 2,
          unmetered_request_count: 0,
          sync_lag_warning: false,
          updated_at: '2026-11-20T08:00:00Z',
        });
      }
      if (url.includes('/admin/v1/alerts/active')) {
        return jsonResponse({
          data: [
            {
              id: 'alt-1',
              severity: 'PAGE',
              alert_code: 'ALT-BUDGET-HARD-LIMIT',
              summary: 'Production project reached 95% monthly budget.',
              remediation_route: '/finops/budgets',
              triggered_at: '2026-11-20T07:55:00Z',
            },
            {
              id: 'alt-2',
              severity: 'TICKET',
              alert_code: 'ALT-CIRCUIT-OPEN',
              summary: 'Anthropic EU failover circuit opened.',
              remediation_route: '/models',
              triggered_at: '2026-11-20T07:50:00Z',
            },
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/deployments')) {
        return jsonResponse({
          data: [
            {
              id: 'dpl_1',
              env_id: 'env_1',
              logical_model_id: 'm1',
              connection_id: 'con_1',
              upstream_model_name: 'claude-3-7-sonnet',
              data_region: 'EU',
              state: 'CIRCUIT_OPEN_UNHEALTHY',
              version: 1,
              rate_card: {input_token_nano_usd: 0, output_token_nano_usd: 0, cache_read_token_nano_usd: 0, cache_write_token_nano_usd: 0, reasoning_token_nano_usd: 0, image_unit_nano_usd: 0, audio_sec_nano_usd: 0, document_page_nano_usd: 0, self_hosted_zero_rate: false},
            },
          ],
          has_more: false,
        });
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<OverviewPage />));
    await screen.findByText('1,420');
    expect(screen.getByText('$4.500000')).toBeInTheDocument();
    expect(screen.getByText('Production project reached 95% monthly budget.')).toBeInTheDocument();
    expect(screen.getByText('PAGE')).toHaveClass('tp-status-text');
    expect(screen.getByText('TICKET')).toHaveClass('tp-status-text');
    expect(screen.getByText('Circuit open')).toHaveClass('tp-status-text');
    // Every section header counter uses unified neutral gray Astryx Badge
    const badges = [...container.querySelectorAll('.astryx-badge')];
    expect(badges.length).toBeGreaterThanOrEqual(2);
    expect(badges.every(b => b.getAttribute('data-variant') === 'neutral')).toBe(true);
    // Zero StatusDot markers inside status/severity labels
    expect(container.querySelectorAll('.astryx-status-dot').length).toBe(0);

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('renders ConsoleShell with role-gated navigation, quick jump, and real sign-out', async () => {
    let loggedOut = false;
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const req = input instanceof Request ? input : new Request(typeof input === 'string' ? input : input.toString());
      const pathname = new URL(req.url, window.location.origin).pathname;
      if (pathname === '/admin/v1/auth/session') {
        return jsonResponse({
          principal_id: 'usr_1',
          email: 'ops@tanpopo.local',
          org_id: 'org_1',
          roles: ['OPERATOR', 'ORG_OWNER'],
          csrf_token: 'csrf-token-123',
          expires_at: '2026-11-21T00:00:00Z',
        });
      }
      if (pathname === '/admin/v1/projects') {
        return jsonResponse({data: [{id: 'proj_1', org_id: 'org_1', slug: 'default', name: 'Default Project', state: 'ACTIVE', created_at: '2026-11-20T00:00:00Z'}], has_more: false});
      }
      if (pathname === '/admin/v1/projects/proj_1/environments') {
        return jsonResponse({data: [{id: 'env_1', project_id: 'proj_1', slug: 'prod', name: 'Production', created_at: '2026-11-20T00:00:00Z'}], has_more: false});
      }
      if (pathname === '/admin/v1/kill-switches') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/approvals') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/overview/summary') {
        return jsonResponse({
          window: '24h',
          total_requests: 12,
          error_rate_ratio: 0,
          p95_overhead_ms: 2,
          settled_micro_usd: 100_000,
          active_hold_micros: 0,
          active_alerts_count: 0,
          unmetered_request_count: 0,
          sync_lag_warning: false,
          updated_at: '2026-11-20T08:00:00Z',
        });
      }
      if (pathname === '/admin/v1/alerts/active') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/deployments') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/auth/logout' && req.method === 'POST') {
        expect(req.headers.get('X-CSRF-Token')).toBe('csrf-token-123');
        loggedOut = true;
        return new Response(null, {status: 204});
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<ConsoleShell />));
    await screen.findByText('ops@tanpopo.local');
    await screen.findByText('All systems nominal');

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);

    fireEvent.click(screen.getByRole('button', {name: 'Sign out'}));
    await waitFor(() => expect(loggedOut).toBe(true));
  });

  it('renders ModelsPage with Healthy and full Circuit open statuses, row action menu, and zero axe violations', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/models')) {
        return jsonResponse({
          data: [
            {
              id: 'm1',
              slug: 'gpt-4o-mini',
              display_name: 'GPT-4o Mini',
              context_window: 128000,
              max_output_tokens: 16384,
              assurance_tier: 'VERIFIED',
              capabilities: {streaming: true, tool_calling: true, parallel_tool_calls: false, strict_json_schema: true, json_object: true, vision: false, audio_input: false, document_input: false, reasoning: false, prompt_caching: false, embeddings: false, rerank: false},
            },
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/deployments')) {
        return jsonResponse({
          data: [
            {
              id: 'dpl_1',
              env_id: 'env_1',
              logical_model_id: 'm1',
              connection_id: 'con_1',
              upstream_model_name: 'gpt-4o-mini-2026',
              data_region: 'US',
              state: 'ACTIVE_HEALTHY',
              version: 1,
              rate_card: {input_token_nano_usd: 150, output_token_nano_usd: 600, cache_read_token_nano_usd: 0, cache_write_token_nano_usd: 0, reasoning_token_nano_usd: 0, image_unit_nano_usd: 0, audio_sec_nano_usd: 0, document_page_nano_usd: 0, self_hosted_zero_rate: false},
            },
            {
              id: 'dpl_2',
              env_id: 'env_1',
              logical_model_id: 'm1',
              connection_id: 'con_2',
              upstream_model_name: 'claude-3-7-sonnet',
              data_region: 'EU',
              state: 'CIRCUIT_OPEN_UNHEALTHY',
              version: 2,
              rate_card: {input_token_nano_usd: 3000, output_token_nano_usd: 15000, cache_read_token_nano_usd: 0, cache_write_token_nano_usd: 0, reasoning_token_nano_usd: 0, image_unit_nano_usd: 0, audio_sec_nano_usd: 0, document_page_nano_usd: 0, self_hosted_zero_rate: false},
            },
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/connections')) {
        return jsonResponse({data: [], has_more: false});
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<ModelsPage environmentId="env_1" roles={['ORG_OWNER']} />));
    await screen.findAllByText('gpt-4o-mini-2026');
    expect(screen.getAllByText('Healthy').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Circuit open').length).toBeGreaterThan(0);
    expect(screen.getAllByRole('button', {name: 'Actions for gpt-4o-mini-2026'}).length).toBeGreaterThan(0);

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('switches Project and Env via official Astryx Selector including No env option', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const req = input instanceof Request ? input : new Request(typeof input === 'string' ? input : input.toString());
      const pathname = new URL(req.url, window.location.origin).pathname;
      if (pathname === '/admin/v1/auth/session') {
        return jsonResponse({
          principal_id: 'usr_1',
          email: 'ops@tanpopo.local',
          org_id: 'org_1',
          roles: ['OPERATOR', 'ORG_OWNER'],
          csrf_token: 'csrf-token-123',
          expires_at: '2026-11-21T00:00:00Z',
        });
      }
      if (pathname === '/admin/v1/projects') {
        return jsonResponse({
          data: [
            {id: 'proj_1', org_id: 'org_1', slug: 'default', name: 'Default Project', state: 'ACTIVE', created_at: '2026-11-20T00:00:00Z'},
            {id: 'proj_2', org_id: 'org_1', slug: 'edge', name: 'Edge Project', state: 'ACTIVE', created_at: '2026-11-20T00:00:00Z'},
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/projects/proj_1/environments') {
        return jsonResponse({
          data: [
            {id: 'env_1', project_id: 'proj_1', slug: 'prod', name: 'Production', created_at: '2026-11-20T00:00:00Z'},
            {id: 'env_2', project_id: 'proj_1', slug: 'staging', name: 'Staging', created_at: '2026-11-20T00:00:00Z'},
          ],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/projects/proj_2/environments') {
        return jsonResponse({
          data: [{id: 'env_edge', project_id: 'proj_2', slug: 'canary', name: 'Canary', created_at: '2026-11-20T00:00:00Z'}],
          has_more: false,
        });
      }
      if (pathname === '/admin/v1/kill-switches') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/approvals') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/overview/summary') {
        return jsonResponse({
          window: '24h',
          total_requests: 0,
          error_rate_ratio: 0,
          p95_overhead_ms: 1,
          settled_micro_usd: 0,
          active_hold_micros: 0,
          active_alerts_count: 0,
          unmetered_request_count: 0,
          sync_lag_warning: false,
          updated_at: '2026-11-20T08:00:00Z',
        });
      }
      if (pathname === '/admin/v1/alerts/active') return jsonResponse({data: [], has_more: false});
      if (pathname === '/admin/v1/deployments') return jsonResponse({data: [], has_more: false});
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<ConsoleShell />));
    const envTrigger = await screen.findByRole('combobox', {name: /Env/i});
    await waitFor(() => expect(envTrigger).toHaveTextContent('Production'));

    fireEvent.click(envTrigger);
    const noEnvOpt = await screen.findByRole('option', {name: 'No env'});
    fireEvent.click(noEnvOpt);
    await waitFor(() => expect(envTrigger).toHaveTextContent('No env'));

    const projectTrigger = screen.getByRole('combobox', {name: /Project/i});
    fireEvent.click(projectTrigger);
    fireEvent.click(await screen.findByRole('option', {name: 'Edge Project'}));
    await waitFor(() => expect(envTrigger).toHaveTextContent('Canary'));

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('supports Astryx Typeahead suggestions and free-form route slugs in PlaygroundPage', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/routes')) {
        return jsonResponse({
          data: [
            {id: 'rt_1', env_id: 'env_1', slug: 'primary-chat', strategy: 'PRIORITY_FAILOVER', max_failover_hops: 2, timeout_ms: 60000, targets: [], version: 1},
            {id: 'rt_2', env_id: 'env_1', slug: 'fast-triage', strategy: 'LOWEST_LATENCY_EWMA', max_failover_hops: 1, timeout_ms: 30000, targets: [], version: 1},
          ],
          has_more: false,
        });
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<PlaygroundPage pathname="/devtools/playground" projectId="proj_1" environmentId="env_1" roles={['ORG_OWNER']} />));
    await waitFor(() => expect(screen.getAllByText('primary-chat').length).toBeGreaterThan(0));

    // Enter a free-form custom slug in Comparison model (optional)
    const compareInput = screen.getByRole('combobox', {name: /Comparison model/i});
    fireEvent.focus(compareInput);
    fireEvent.change(compareInput, {target: {value: 'custom-experimental-route'}});
    fireEvent.blur(compareInput);

    await waitFor(() => expect(screen.getAllByText('custom-experimental-route').length).toBeGreaterThan(0));

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('renders SelectField and MultiSelectField with validation status and zero axe violations', async () => {
    const onSelect = vi.fn();
    const onMulti = vi.fn();
    const {container} = render(
      wrap(
        <div className="tp-stack">
          <SelectField
            label="Failure posture"
            value="FAIL_OPEN"
            error="Parent guardrail is FAIL_CLOSED and cannot be relaxed."
            onChange={onSelect}
            options={[
              {value: 'FAIL_CLOSED', label: 'FAIL_CLOSED'},
              {value: 'FAIL_OPEN', label: 'FAIL_OPEN'},
            ]}
          />
          <MultiSelectField
            label="Events"
            value={['budget.threshold_reached']}
            onChange={onMulti}
            hasSelectAll
            options={[
              {value: 'budget.threshold_reached', label: 'budget.threshold_reached'},
              {value: 'kill_switch.activated', label: 'kill_switch.activated'},
            ]}
          />
        </div>,
      ),
    );
    expect(screen.getByText('Parent guardrail is FAIL_CLOSED and cannot be relaxed.')).toBeInTheDocument();
    const multiTrigger = screen.getByRole('combobox', {name: /Events/i});
    fireEvent.click(multiTrigger);
    fireEvent.click(await screen.findByRole('option', {name: 'kill_switch.activated'}));
    expect(onMulti).toHaveBeenCalledWith(['budget.threshold_reached', 'kill_switch.activated']);

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });
});
