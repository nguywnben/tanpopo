import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, describe, expect, it, vi} from 'vitest';
import App from '../App';

afterEach(() => {
  window.history.replaceState({}, '', '/');
  vi.unstubAllGlobals();
});

describe('Unit 27 setup, login, and overview pages', () => {
  it('provides an accessible local administrator sign-in form', async () => {
    window.history.replaceState({}, '', '/login');
    document.documentElement.lang = 'en';
    document.title = 'Tanpopo Console';
    vi.stubGlobal('fetch', vi.fn());
    render(<App />);
    expect(await screen.findByRole('heading', {name: 'Sign in to Tanpopo'})).toBeInTheDocument();
    expect(screen.getByLabelText('Email address')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    const results = await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}});
    expect(results.violations.filter(item => item.impact === 'critical' || item.impact === 'serious')).toEqual([]);
  });

  it('requires explicit local-network consent and submits bootstrap without session CSRF', async () => {
    window.history.replaceState({}, '', '/setup');
    const bootstrapResponse = {
      org_id: 'org-1', project_id: 'project-1', env_id: 'env-1', owner_principal_id: 'owner-1', csrf_token: 'csrf-session',
      initial_virtual_key: {
        id: 'vk-1', name: 'First key', project_id: 'project-1', env_id: 'env-1', key_prefix: 'vk_tp_dev_',
        last_four: '4abc', state: 'CREATED_ACTIVE', version: 1, created_at: '2026-11-17T00:00:00Z', secret_key: 'vk_tp_live_once',
      },
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({initialized: false, offline_mode: false, version: '0.1.0'}))
      .mockResolvedValueOnce(json(bootstrapResponse, 201));
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByRole('heading', {name: 'Set up Tanpopo'})).toBeInTheDocument();
    expect(await seriousAxeViolations()).toEqual([]);
    fireEvent.change(screen.getByLabelText('Bootstrap token'), {target: {value: 'bootstrap-token'}});
    fireEvent.change(screen.getByLabelText('Owner email'), {target: {value: 'owner@example.test'}});
    fireEvent.change(screen.getByLabelText('Owner password'), {target: {value: 'correct-horse-battery'}});
    fireEvent.change(screen.getByLabelText('Confirm password'), {target: {value: 'correct-horse-battery'}});
    fireEvent.click(screen.getByRole('button', {name: 'Continue'}));

    expect(await screen.findByRole('radio', {name: 'Local Ollama server'})).toBeChecked();
    fireEvent.click(screen.getByRole('button', {name: 'Continue'}));
    expect(await screen.findByText(/Explicitly allow the local upstream/)).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('Allow private or loopback upstream for this connection'));
    fireEvent.click(screen.getByRole('button', {name: 'Continue'}));
    fireEvent.click(screen.getByLabelText('I have securely backed up the Tanpopo master key'));
    fireEvent.click(screen.getByRole('button', {name: 'Continue'}));
    expect(await screen.findByRole('heading', {name: 'Review and complete'})).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', {name: 'Complete setup'}));

    expect(await screen.findByRole('heading', {name: 'Store your virtual key now'})).toBeInTheDocument();
    expect(await seriousAxeViolations()).toEqual([]);
    expect(screen.getByText('vk_tp_live_once')).toBeInTheDocument();
    const request = fetchMock.mock.calls[1][0] as Request;
    expect(request.method).toBe('POST');
    expect(request.headers.get('X-CSRF-Token')).toBeNull();
    const body = await request.clone().json();
    expect(body.initial_connection).toMatchObject({adapter_id: 'ollama', billing_mode: 'SELF_HOSTED_INFRA', allow_private_ip: true, allowed_cidrs: ['127.0.0.1/32']});
    expect(body.initial_connection.secret_value).toBeUndefined();
    expect(screen.getByRole('button', {name: 'Continue to Overview'})).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I have securely stored this virtual key'));
    expect(screen.getByRole('button', {name: 'Continue to Overview'})).toBeEnabled();
  });

  it('renders the API scoped overview KPIs and remediation alerts', async () => {
    window.history.replaceState({}, '', '/overview');
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({principal_id: 'owner-1', email: 'owner@example.test', roles: ['ORG_OWNER'], csrf_token: 'csrf', expires_at: '2026-12-01T00:00:00Z'}))
      .mockResolvedValueOnce(json({data: [{id: 'project-1', org_id: 'org-1', slug: 'sample', name: 'Sample Project', version: 1, created_at: '2026-11-01T00:00:00Z'}], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'env-1', org_id: 'org-1', project_id: 'project-1', slug: 'dev', name: 'Development', created_at: '2026-11-01T00:00:00Z'}]}))
      .mockResolvedValueOnce(json({window: '24h', total_requests: 120, error_rate_ratio: 0.02, p95_overhead_ms: 95, settled_micro_usd: 1200000, active_hold_micros: 200000, active_alerts_count: 1, updated_at: '2026-11-17T00:00:00Z'}))
      .mockResolvedValueOnce(json({data: [{id: 'alert-1', alert_code: 'ALT-BUDGET-80', severity: 'TICKET', summary: 'Budget is nearing its limit', remediation_route: '/finops/budgets', triggered_at: '2026-11-17T00:00:00Z'}], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<App />);

    expect(await screen.findByText('120')).toBeInTheDocument();
    expect(screen.getByText('Budget is nearing its limit')).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Open remediation'})).toBeInTheDocument();
    expect(await seriousAxeViolations()).toEqual([]);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(5));
  });
});

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {status, headers: {'Content-Type': 'application/json'}});
}

async function seriousAxeViolations() {
  const results = await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}});
  return results.violations.filter(item => item.impact === 'critical' || item.impact === 'serious');
}
