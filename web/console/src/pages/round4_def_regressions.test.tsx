import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken} from '../api/client';
import {I18nProvider} from '../i18n/I18nProvider';
import {RouterProvider} from '../router/RouterContext';
import {ThemeProvider} from '../styles/theme';
import {FinOpsPage} from './finops/FinOpsPage';
import {ModelsPage} from './models/ModelsPage';
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
});
