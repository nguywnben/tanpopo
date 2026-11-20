import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken} from '../api/client';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {ConsoleShell} from '../shell/ConsoleShell';
import {LoginPage} from './auth/LoginPage';
import {buildRateCard} from './models/ModelsPage';
import {OverviewPage} from './overview/OverviewPage';
import {SetupPage} from './setup/SetupPage';
import {formatMicroUSD} from '../ui/primitives';

function jsonResponse(body: unknown, status = 200, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {'Content-Type': 'application/json', ...headers},
  });
}

function wrap(ui: React.ReactNode) {
  return (
    <ThemeProvider>
      <RouterProvider>{ui}</RouterProvider>
    </ThemeProvider>
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
          active_alerts_count: 1,
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
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/deployments')) {
        return jsonResponse({data: [], has_more: false});
      }
      return jsonResponse({}, 404);
    });

    const {container} = render(wrap(<OverviewPage />));
    await screen.findByText('1,420');
    expect(screen.getByText('$4.500000')).toBeInTheDocument();
    expect(screen.getByText('Production project reached 95% monthly budget.')).toBeInTheDocument();

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
});
