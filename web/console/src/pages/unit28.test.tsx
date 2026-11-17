import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken} from '../api/client';
import {ConnectionsPage} from './providers/ConnectionsPage';
import {ExtensionCatalogPage} from './extensions/ExtensionCatalogPage';
import {InstalledExtensionDetailPage} from './extensions/InstalledExtensionDetailPage';

afterEach(() => { cleanup(); clearCsrfToken(); vi.unstubAllGlobals(); });

describe('Unit 28 catalog and extension workflows', () => {
  it('seals a write-only secret, clears the input, and renders metadata only', async () => {
    rememberCsrfToken('csrf');
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'adapter-1', name: 'Example', installation_state: 'ENABLED_ACTIVE'}], has_more: false}))
      .mockResolvedValueOnce(json({credential_id: 'credential-1', name: 'Production key', secret_kind: 'API_KEY', trailing_fingerprint: '...9a2f', version: 1, created_at: '2026-11-17T00:00:00Z'}, 201));
    vi.stubGlobal('fetch', fetchMock);
    render(<ConnectionsPage roles={['ORG_OWNER']} projectId="project-1" />);
    fireEvent.change(await screen.findByLabelText(/Credential name/), {target: {value: 'Production key'}});
    const secret = screen.getByLabelText(/Credential value/) as HTMLInputElement;
    fireEvent.change(secret, {target: {value: 'sk-secret-never-render-again'}});
    fireEvent.click(screen.getByRole('button', {name: 'Seal credential'}));
    await waitFor(() => expect(secret).toHaveValue(''));
    expect(await screen.findByText(/Credential sealed/i)).toHaveTextContent(/Fingerprint \.\.\.9a2f/i);
    expect(document.body.textContent).not.toContain('sk-secret-never-render-again');
    const request = fetchMock.mock.calls[3][0] as Request;
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf');
    expect(await request.clone().json()).toMatchObject({secret_value: 'sk-secret-never-render-again', project_id: 'project-1'});
  });

  it('preserves connection edits and shows the server copy after an ETag conflict', async () => {
    rememberCsrfToken('csrf-conflict');
    const connection = {id: 'connection-1', name: 'Original connection', adapter_id: 'adapter-1', billing_mode: 'COMMERCIAL_API', endpoint_url: 'https://old.example.test', data_region: 'US', allow_private_ip: false, state: 'ACTIVE', secret_slot_fingerprints: {}, version: 2, created_at: '2026-01-01T00:00:00Z'};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [connection], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'adapter-1', name: 'Example', installation_state: 'ENABLED_ACTIVE', secret_slots: ['api_key']}], has_more: false}))
      .mockResolvedValueOnce(json({error: {code: 'optimistic_lock_conflict', type: 'conflict_error', message: 'The resource changed.', request_id: 'req-conflict', retryable: true, current_resource: {...connection, name: 'Server copy', endpoint_url: 'https://server.example.test', version: 3}}}, 409));
    vi.stubGlobal('fetch', fetchMock);
    render(<ConnectionsPage roles={['ORG_OWNER']} projectId="project-1" />);
    fireEvent.click(await screen.findByRole('button', {name: 'Edit connection'}));
    fireEvent.change(screen.getAllByLabelText(/Connection name/)[0], {target: {value: 'Local draft'}});
    fireEvent.change(screen.getAllByLabelText(/Endpoint URL/)[0], {target: {value: 'https://draft.example.test'}});
    fireEvent.click(screen.getByRole('button', {name: 'Save connection changes'}));
    expect(await screen.findAllByText((_, element) => element?.textContent?.includes('Your draft: Local draft · https://draft.example.test') ?? false)).not.toHaveLength(0);
    expect(screen.getAllByText((_, element) => element?.textContent?.includes('Latest server values: Server copy · https://server.example.test') ?? false)).not.toHaveLength(0);
    expect(screen.getAllByLabelText(/Connection name/)[0]).toHaveValue('Local draft');
    const patchRequest = fetchMock.mock.calls[3][0] as Request;
    expect(patchRequest.headers.get('If-Match')).toBe('"v2"');
    expect(patchRequest.headers.get('X-CSRF-Token')).toBe('csrf-conflict');
  });

  it('holds an unverified extension behind full permission review and explicit acknowledgement', async () => {
    rememberCsrfToken('csrf-extension');
    const permissionDiff = {
      installation_id: 'installation-1', package_id: '@community/agent', candidate_version: '2.0.0', origin_trust_tier: 'unverified_external',
      enterprise_signed: false, execution_tier: 'sandboxed_wasm', added_allowed_hosts: ['api.example.test'], removed_allowed_hosts: [], secret_slots: ['api_key'],
      quotas: {max_memory_mib: 16, max_cpu_ms_per_call: 25},
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [], offline_cached: true, has_more: false}))
      .mockResolvedValueOnce(json(permissionDiff, 202))
      .mockResolvedValueOnce(new Response(JSON.stringify(permissionDiff), {status: 200, headers: {'Content-Type': 'application/json', ETag: '"v4"'}}))
      .mockResolvedValueOnce(json({id: 'installation-1', state: 'ENABLED_ACTIVE', version: 4}, 200));
    vi.stubGlobal('fetch', fetchMock);
    document.documentElement.lang = 'en';
    document.title = 'Tanpopo Console';
    render(<ExtensionCatalogPage roles={['OPERATOR']} />);
    fireEvent.change(await screen.findByLabelText(/Package source URI/), {target: {value: 'oci://packages.example.test/plugin@sha256:abc'}});
    fireEvent.change(screen.getByLabelText(/Expected SHA-256/), {target: {value: 'a'.repeat(64)}});
    fireEvent.click(screen.getByRole('button', {name: 'Stage package'}));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Added hosts: api.example.test')).toBeInTheDocument();
    expect(screen.getByText('Secret slots: api_key')).toBeInTheDocument();
    const audit = await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}});
    expect(audit.violations.filter(item => item.impact === 'critical' || item.impact === 'serious')).toEqual([]);
    const activate = screen.getByRole('button', {name: 'Approve & Activate'});
    expect(activate).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I acknowledge this package is from an unverified external publisher'));
    expect(activate).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I reviewed and explicitly consent to the requested permission changes'));
    expect(activate).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(activate);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    const approval = fetchMock.mock.calls[3][0] as Request;
    expect(approval.headers.get('If-Match')).toBe('"v4"');
    expect(approval.headers.get('X-CSRF-Token')).toBe('csrf-extension');
    expect(await approval.clone().json()).toMatchObject({enable_immediately: true, acknowledge_unverified_external: true});
  });

  it('hides installation lifecycle actions from developers and renders publisher copy as text', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [{id: 'installed-1', package_id: '@community/safe', state: 'INSTALLED_DISABLED', origin_trust_tier: 'verified', enterprise_signed: false, execution_tier: 'declarative', version: 1}], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<InstalledExtensionDetailPage roles={['DEVELOPER']} />);
    expect(await screen.findByText('@community/safe')).toBeInTheDocument();
    expect(screen.queryByRole('button', {name: 'Quarantine'})).not.toBeInTheDocument();
    expect(screen.queryByRole('button', {name: 'Rollback'})).not.toBeInTheDocument();
    expect(screen.queryByRole('button', {name: 'Uninstall or discard candidate'})).not.toBeInTheDocument();
  });

  it('uses the server ETag and requires permission delta review on an escalating upgrade', async () => {
    rememberCsrfToken('csrf-upgrade');
    const installation = {id: 'installed-2', package_id: '@community/upgrade', state: 'ENABLED_ACTIVE', origin_trust_tier: 'verified', enterprise_signed: true, execution_tier: 'declarative', version: 3, active_version: '1.0.0', previous_rollback_version: '0.9.0'};
    const upgradeDiff = {installation_id: 'installed-2', package_id: '@community/upgrade', active_version: '1.0.0', candidate_version: '2.0.0', origin_trust_tier: 'verified', enterprise_signed: true,
      execution_tier: 'sandboxed_wasm', added_allowed_hosts: ['new.example.test'], removed_allowed_hosts: [], secret_slots: [], quotas: {max_memory_mib: 16}};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [installation], has_more: false}))
      .mockResolvedValueOnce(new Response(JSON.stringify({...upgradeDiff, installation_id: 'installed-2', candidate_version: '1.0.0'}), {status: 200, headers: {'Content-Type': 'application/json', ETag: '"v3"'}}))
      .mockResolvedValueOnce(new Response(JSON.stringify(upgradeDiff), {status: 202, headers: {'Content-Type': 'application/json', ETag: '"candidate-opaque-xyz"'}}))
      .mockResolvedValueOnce(json({data: [{...installation, state: 'ENABLED_ACTIVE', version: 4}], has_more: false}))
      .mockResolvedValueOnce(json({...installation, state: 'ENABLED_ACTIVE', version: 4}));
    vi.stubGlobal('fetch', fetchMock);
    render(<InstalledExtensionDetailPage roles={['OPERATOR']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Review permissions'}));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    fireEvent.keyDown(screen.getByRole('dialog'), {key: 'Escape'});
    fireEvent.change(screen.getByLabelText(/Upgrade source URI/), {target: {value: 'oci://example.test/pkg@sha256:abc'}});
    fireEvent.change(screen.getByLabelText(/Expected SHA-256/), {target: {value: 'b'.repeat(64)}});
    fireEvent.click(screen.getByRole('button', {name: 'Stage upgrade'}));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    const approve = screen.getByRole('button', {name: 'Approve permissions and activate'});
    expect(approve).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I reviewed and explicitly consent to the requested permission changes'));
    expect(approve).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(approve);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6));
    const request = fetchMock.mock.calls.find(call => (call[0] as Request).url.endsWith('/approve-permissions'))?.[0] as Request;
    expect(request.headers.get('If-Match')).toBe('"candidate-opaque-xyz"');
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-upgrade');
  });

  it('shows extension blast radius in the destructive quarantine alert', async () => {
    rememberCsrfToken('csrf-quarantine');
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [{id: 'installed-3', package_id: '@community/risky', state: 'ENABLED_ACTIVE', origin_trust_tier: 'verified', enterprise_signed: true, execution_tier: 'sandboxed_wasm', version: 1}], has_more: false}))
      .mockResolvedValueOnce(json({organizations: ['org-a'], projects: ['project-a', 'project-b'], virtual_keys: ['key-a'], connections: ['connection-a']}))
      .mockResolvedValueOnce(json({config_epoch: 5, severed_streams_count: 2, affected_entities_count: 3}))
      .mockResolvedValueOnce(json({data: [], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<InstalledExtensionDetailPage roles={['OPERATOR']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Quarantine'}));
    expect(await screen.findAllByText(/Affected projects: project-a, project-b/)).not.toHaveLength(0);
    fireEvent.change(screen.getByLabelText(/Reason/), {target: {value: 'Malicious behavior'}});
    fireEvent.click(screen.getByRole('button', {name: 'Review quarantine confirmation'}));
    const alert = await screen.findByRole('alertdialog');
    expect(alert).toHaveTextContent('connection-a');
    fireEvent.click(screen.getByRole('button', {name: 'Confirm quarantine'}));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    const request = fetchMock.mock.calls.find(call => (call[0] as Request).method==='POST')?.[0] as Request;
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-quarantine');
    expect(await request.clone().json()).toMatchObject({target_level: 'EXTENSION', target_id: 'installed-3', abort_active_streams: true, reason: 'Malicious behavior'});
  });
});

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {status, headers: {'Content-Type': 'application/json'}});
}
