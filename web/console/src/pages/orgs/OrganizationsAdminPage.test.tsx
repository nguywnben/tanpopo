import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken} from '../../api/client';
import {RouterProvider} from '../../router/RouterContext';
import {ConsoleShell} from '../../shell/ConsoleShell';
import {ThemeProvider} from '../../styles/theme';
import {OrganizationsAdminPage} from './OrganizationsAdminPage';

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {'Content-Type': 'application/json'},
  });
}

function wrap(ui: React.ReactNode) {
  return (
    <ThemeProvider>
      <RouterProvider>{ui}</RouterProvider>
    </ThemeProvider>
  );
}

describe('Multi-Org progressive disclosure & OrganizationsAdminPage', () => {
  beforeEach(() => {
    clearCsrfToken();
    window.sessionStorage.clear();
    window.history.replaceState(null, '', '/overview');
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('hides Organization switcher for single-org non-OPERATOR principals', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
      if (url.includes('/admin/v1/auth/session')) {
        return jsonResponse({
          principal_id: 'prc_single',
          email: 'owner@alpha.test',
          org_id: 'org_alpha',
          roles: ['ORG_OWNER'],
          csrf_token: 'csrf-1',
          expires_at: '2026-12-01T00:00:00Z',
          multi_org_enabled: true,
        });
      }
      if (url.includes('/admin/v1/organizations')) {
        return jsonResponse({
          data: [{
            id: 'org_alpha', slug: 'alpha', name: 'Alpha Org',
            min_plugin_trust: 'OFFICIAL_ONLY', four_eyes_enabled: false, version: 1, created_at: '2026-11-22T00:00:00Z',
          }],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/bootstrap/status')) return jsonResponse({initialized: true, offline_mode: false, multi_org_enabled: true});
      if (url.includes('/admin/v1/projects/prj_a/environments')) return jsonResponse({data: [{id: 'env_prod', org_id: 'org_alpha', project_id: 'prj_a', slug: 'prod', name: 'prod', created_at: '2026-11-22T00:00:00Z'}]});
      if (url.includes('/admin/v1/projects')) return jsonResponse({data: [{id: 'prj_a', org_id: 'org_alpha', slug: 'core', name: 'Core', version: 1, created_at: '2026-11-22T00:00:00Z'}], has_more: false});
      if (url.includes('/admin/v1/overview/summary')) return jsonResponse({window: '24h', total_requests: 0, error_rate: 0, cache_hit_rate: 0, estimated_savings_micro_usd: 0, total_spend_micro_usd: 0, active_alerts_count: 0, open_circuit_deployments_count: 0});
      if (url.includes('/admin/v1/deployments') || url.includes('/admin/v1/alerts/active') || url.includes('/admin/v1/kill-switches') || url.includes('/admin/v1/mcp/approvals')) return jsonResponse({data: [], has_more: false});
      return jsonResponse({});
    });

    render(wrap(<ConsoleShell />));
    const trigger = await screen.findByRole('button', {name: 'Active workspace scope'});
    await waitFor(() => expect(trigger).not.toBeDisabled());
    expect(trigger.textContent).not.toContain('Alpha Org');
    fireEvent.click(trigger);
    await screen.findByRole('listbox', {name: 'Project'});
    expect(screen.queryByRole('listbox', {name: 'Organization'})).toBeNull();
  });

  it('switches organizations for multi-org members, purges ephemeral cache & query params, and injects X-Tanpopo-Org-ID', async () => {
    window.history.replaceState(null, '', '/overview?project_id=prj_a&org_id=org_alpha');
    window.sessionStorage.setItem('tanpopo.playground.ephemeral_key', 'vk_tp_test_ephemeral');

    const capturedHeaders: Array<string | null> = [];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const req = input instanceof Request ? input : new Request(String(input));
      const url = req.url;
      const orgHdr = req.headers.get('X-Tanpopo-Org-ID');
      if (url.includes('/admin/v1/projects') || url.includes('/admin/v1/overview/summary')) {
        capturedHeaders.push(orgHdr);
      }
      if (url.includes('/admin/v1/auth/session')) {
        return jsonResponse({
          principal_id: 'prc_multi',
          email: 'multi@acme.test',
          org_id: 'org_alpha',
          roles: ['ORG_OWNER'],
          csrf_token: 'csrf-multi',
          expires_at: '2026-12-01T00:00:00Z',
          multi_org_enabled: true,
        });
      }
      if (url.includes('/admin/v1/bootstrap/status')) {
        return jsonResponse({initialized: true, offline_mode: true, multi_org_enabled: true});
      }
      if (url.includes('/admin/v1/organizations')) {
        return jsonResponse({
          data: [
            {id: 'org_alpha', slug: 'alpha', name: 'Alpha Org', min_plugin_trust: 'OFFICIAL_ONLY', four_eyes_enabled: false, version: 1, created_at: '2026-11-22T00:00:00Z'},
            {id: 'org_bravo', slug: 'bravo', name: 'Bravo Org', min_plugin_trust: 'OFFICIAL_ONLY', four_eyes_enabled: false, version: 1, created_at: '2026-11-22T00:00:00Z'},
          ],
          has_more: false,
        });
      }
      if (url.includes('/admin/v1/projects/') && url.includes('/environments')) {
        if (orgHdr === 'org_bravo') {
          return jsonResponse({data: [{id: 'env_b', org_id: 'org_bravo', project_id: 'prj_b', slug: 'staging', name: 'staging', created_at: '2026-11-22T00:00:00Z'}]});
        }
        return jsonResponse({data: [{id: 'env_a', org_id: 'org_alpha', project_id: 'prj_a', slug: 'prod', name: 'prod', created_at: '2026-11-22T00:00:00Z'}]});
      }
      if (url.includes('/admin/v1/projects')) {
        if (orgHdr === 'org_bravo') {
          return jsonResponse({data: [{id: 'prj_b', org_id: 'org_bravo', slug: 'bravo-proj', name: 'Bravo Workspace', version: 1, created_at: '2026-11-22T00:00:00Z'}], has_more: false});
        }
        return jsonResponse({data: [{id: 'prj_a', org_id: 'org_alpha', slug: 'alpha-proj', name: 'Alpha Workspace', version: 1, created_at: '2026-11-22T00:00:00Z'}], has_more: false});
      }
      if (url.includes('/admin/v1/overview/summary')) return jsonResponse({window: '24h', total_requests: 0, error_rate: 0, cache_hit_rate: 0, estimated_savings_micro_usd: 0, total_spend_micro_usd: 0, active_alerts_count: 0, open_circuit_deployments_count: 0});
      if (url.includes('/admin/v1/deployments') || url.includes('/admin/v1/alerts/active') || url.includes('/admin/v1/kill-switches') || url.includes('/admin/v1/mcp/approvals')) return jsonResponse({data: [], has_more: false});
      return jsonResponse({});
    });

    const {container} = render(wrap(<ConsoleShell />));
    await screen.findByText('Air-gapped offline mode');
    const trigger = await screen.findByRole('button', {name: 'Active workspace scope'});
    await waitFor(() => expect(trigger.textContent).toContain('Alpha Org'));

    fireEvent.click(trigger);
    const bravoOption = await screen.findByRole('option', {name: 'Bravo Org'});
    fireEvent.click(bravoOption);

    await waitFor(() => expect(trigger.textContent).toContain('Bravo Workspace'));
    expect(trigger.textContent).not.toContain('Alpha Workspace');
    expect(window.location.search).toBe('');
    expect(window.sessionStorage.getItem('tanpopo.playground.ephemeral_key')).toBeNull();
    expect(capturedHeaders).toContain('org_bravo');

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });

  it('restricts OrganizationsAdminPage to OPERATOR and supports create + crypto-shred with WCAG 2.2 AA', async () => {
    const denied = render(wrap(
      <OrganizationsAdminPage roles={['ORG_OWNER']} activeOrgId="org_1" multiOrgEnabled={true} onSelectOrg={vi.fn()} onOrganizationsChanged={vi.fn()} />,
    ));
    expect(denied.getByText(/Only OPERATOR principals/i)).toBeInTheDocument();
    cleanup();

    const {rememberCsrfToken} = await import('../../api/client');
    rememberCsrfToken('csrf-op');

    let orgs = [
      {id: 'org_1', slug: 'alpha', name: 'Alpha Org', min_plugin_trust: 'OFFICIAL_ONLY', four_eyes_enabled: false, version: 1, created_at: '2026-11-22T00:00:00Z'},
    ];
    vi.spyOn(globalThis, 'fetch').mockImplementation(async input => {
      const req = input instanceof Request ? input : new Request(String(input));
      if (req.url.includes('/admin/v1/organizations') && req.method === 'GET') {
        return jsonResponse({data: orgs, has_more: false});
      }
      if (req.url.includes('/admin/v1/organizations') && req.method === 'POST') {
        const created = {id: 'org_2', slug: 'charlie', name: 'Charlie Org', min_plugin_trust: 'OFFICIAL_ONLY', four_eyes_enabled: false, version: 1, created_at: '2026-11-22T00:00:00Z'};
        orgs = [...orgs, created];
        return jsonResponse(created, 201);
      }
      if (req.url.includes('/admin/v1/organizations/org_2') && req.method === 'DELETE') {
        expect(req.headers.get('If-Match')).toBe('"v1"');
        orgs = orgs.filter(o => o.id !== 'org_2');
        return jsonResponse({target_type: 'ORGANIZATION', target_id: 'org_2', destroyed_dek_id: 'dek_2', shredded_at: '2026-11-22T00:00:00Z', revoked_virtual_keys_count: 3});
      }
      return jsonResponse({});
    });

    const {container} = render(wrap(
      <OrganizationsAdminPage roles={['OPERATOR']} activeOrgId="org_1" multiOrgEnabled={true} onSelectOrg={vi.fn()} onOrganizationsChanged={vi.fn()} />,
    ));
    await screen.findAllByText('Alpha Org');

    fireEvent.click(screen.getByRole('button', {name: 'Create organization'}));
    const dialog = await screen.findByRole('dialog');
    const nameInput = dialog.querySelector('input') as HTMLInputElement;
    fireEvent.change(nameInput, {target: {value: 'Charlie Org'}});
    fireEvent.click(dialog.querySelector('button[type="submit"]') as HTMLButtonElement);

    await screen.findAllByText('Charlie Org');
    const shredBtns = screen.getAllByRole('button', {name: 'Crypto-shred'});
    fireEvent.click(shredBtns[shredBtns.length - 1]);
    const shredModal = await screen.findByRole('dialog');
    const shredInputs = shredModal.querySelectorAll('input');
    fireEvent.change(shredInputs[shredInputs.length - 1], {target: {value: 'charlie'}});
    const confirmBtns = Array.from(shredModal.querySelectorAll('button')).filter(b => b.textContent?.includes('Crypto-shred'));
    fireEvent.click(confirmBtns[confirmBtns.length - 1]);
    await screen.findByText('Organization crypto-shredded');

    const audit = await axe.run(container);
    expect(audit.violations).toEqual([]);
  });
});
