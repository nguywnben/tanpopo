import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, describe, expect, it, vi} from 'vitest';
import {clearCsrfToken, rememberCsrfToken} from '../api/client';
import {ConnectionsPage} from './providers/ConnectionsPage';
import {ExtensionCatalogPage} from './extensions/ExtensionCatalogPage';
import {InstalledExtensionDetailPage} from './extensions/InstalledExtensionDetailPage';
import {buildRateCard, ModelsCatalogPage} from './models/ModelsCatalogPage';

afterEach(() => { cleanup(); clearCsrfToken(); vi.unstubAllGlobals(); });

describe('Unit 28 catalog and extension workflows', () => {
  it('filters marketplace entries by capability, origin, and enterprise signature', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(json({data: [
      {id: 'vision-ext', canonical_name: 'Vision tools', package_id: '@community/vision', version: '1.0.0', origin_trust_tier: 'verified', enterprise_signed: true, execution_tier: 'declarative', capabilities: ['vision', 'image'], summary: 'Image support'},
      {id: 'audio-ext', canonical_name: 'Audio tools', package_id: '@community/audio', version: '1.0.0', origin_trust_tier: 'official', enterprise_signed: false, execution_tier: 'declarative', capabilities: ['audio'], summary: 'Audio support'},
    ], offline_cached: true, has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<ExtensionCatalogPage roles={['DEVELOPER']} />);
    expect(await screen.findByText('Vision tools')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Capability type filter'), {target: {value: 'vision'}});
    fireEvent.change(screen.getByLabelText('Enterprise signature filter'), {target: {value: 'SIGNED'}});
    expect(screen.getByText('Vision tools')).toBeInTheDocument();
    expect(screen.queryByText('Audio tools')).not.toBeInTheDocument();
  });

  it('stages a marketplace row directly and opens its permission review', async () => {
    rememberCsrfToken('csrf-marketplace');
    const sha256 = 'd'.repeat(64);
    const permissionDiff = {
      installation_id: 'market-installation', package_id: '@community/vision', candidate_version: '1.2.0', origin_trust_tier: 'verified', enterprise_signed: true,
      execution_tier: 'declarative', active_execution_tier: null, active_allowed_hosts: [], candidate_allowed_hosts: ['api.example.test'],
      added_allowed_hosts: ['api.example.test'], removed_allowed_hosts: [], secret_slots: ['api_key'], active_secret_slots: [], added_secret_slots: ['api_key'],
      quota_escalated: false, active_quotas: null, quotas: {max_memory_mib: 0, max_cpu_ms_per_call: 0, max_stream_chunk_cpu_ms: 0},
      active_permissions: null, candidate_permissions: {execution_tier: 'DECLARATIVE_T1', allowed_hosts: ['api.example.test'], secret_slots: ['api_key']},
      permission_delta: {added_hosts: ['api.example.test'], added_secret_slots: ['api_key']}, has_escalation: true,
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [{id: 'vision', canonical_name: 'Vision adapter', package_id: '@community/vision', version: '1.2.0', origin_trust_tier: 'verified', enterprise_signed: true, execution_tier: 'declarative', capabilities: ['vision'], source_uri: 'marketplace://@community/vision@1.2.0', expected_sha256: sha256}], has_more: false}))
      .mockResolvedValueOnce(json(permissionDiff, 202))
      .mockResolvedValueOnce(new Response(JSON.stringify(permissionDiff), {status: 200, headers: {'Content-Type': 'application/json', ETag: '"v1"'}}));
    vi.stubGlobal('fetch', fetchMock);
    render(<ExtensionCatalogPage roles={['OPERATOR']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Stage for permission review'}));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Active version')).toBeInTheDocument();
    expect(screen.getByText('Candidate 1.2.0')).toBeInTheDocument();
    const stageRequest = fetchMock.mock.calls[1][0] as Request;
    expect(await stageRequest.clone().json()).toEqual({source_uri: 'marketplace://@community/vision@1.2.0', expected_sha256: sha256});
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('maps and validates every integer token and media rate', () => {
    const rates = Object.fromEntries(['input_token_nano_usd', 'output_token_nano_usd', 'cache_read_token_nano_usd', 'cache_write_token_nano_usd', 'reasoning_token_nano_usd', 'image_unit_nano_usd', 'audio_sec_nano_usd', 'document_page_nano_usd'].map((key, index) => [key, String(index + 1)])) as Parameters<typeof buildRateCard>[0];
    expect(buildRateCard(rates, 'COMMERCIAL_API', false)).toEqual({rateCard: {
      input_token_nano_usd: 1, output_token_nano_usd: 2, cache_read_token_nano_usd: 3, cache_write_token_nano_usd: 4,
      reasoning_token_nano_usd: 5, image_unit_nano_usd: 6, audio_sec_nano_usd: 7, document_page_nano_usd: 8, self_hosted_zero_rate: false,
    }});
    expect(buildRateCard({...rates, image_unit_nano_usd: '1.5'}, 'COMMERCIAL_API', false).error).toMatch(/integer/);
    expect(buildRateCard({...rates, audio_sec_nano_usd: '9007199254740992'}, 'COMMERCIAL_API', false).error).toMatch(/safe integer/);
  });

  it('confirms disabling the last healthy deployment and sends its ETag with CSRF', async () => {
    rememberCsrfToken('csrf-deployment');
    const model = {id: 'model-1', slug: 'model-one', display_name: 'Model One', context_window: 1000, max_output_tokens: 100, assurance_tier: 'DECLARED', capabilities: {vision: true}};
    const deployment = {id: 'deployment-1', env_id: 'env-1', logical_model_id: 'model-1', connection_id: 'connection-1', upstream_model_name: 'upstream-one', data_region: 'US', state: 'ACTIVE_HEALTHY', rate_card: {input_token_nano_usd: 1, output_token_nano_usd: 1}, version: 3};
    const connection = {id: 'connection-1', name: 'Commercial', adapter_id: 'adapter-1', billing_mode: 'COMMERCIAL_API', endpoint_url: 'https://api.example.test', data_region: 'US', allow_private_ip: false, state: 'ACTIVE', secret_slot_fingerprints: {}, version: 1, created_at: '2026-01-01T00:00:00Z'};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [model], has_more: false}))
      .mockResolvedValueOnce(json({data: [deployment], has_more: false}))
      .mockResolvedValueOnce(json({data: [connection], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'route-1', slug: 'production-route', targets: [{deployment_id: deployment.id, priority_order: 1}]}], has_more: false}))
      .mockResolvedValueOnce(json({data: [deployment], has_more: false}))
      .mockResolvedValueOnce(json({...deployment, state: 'DISABLED', version: 4}))
      .mockResolvedValueOnce(json({data: [model], has_more: false}))
      .mockResolvedValueOnce(json({data: [{...deployment, state: 'DISABLED', version: 4}], has_more: false}))
      .mockResolvedValueOnce(json({data: [connection], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<ModelsCatalogPage roles={['TEAM_ADMIN']} environmentId="env-1" />);
    fireEvent.click(await screen.findByRole('button', {name: 'Disable deployment'}));
    const confirmation = await screen.findByRole('alertdialog');
    expect(confirmation).toHaveTextContent('production-route');
    fireEvent.click(screen.getAllByRole('button', {name: 'Disable deployment'}).at(-1)!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(9));
    const request = fetchMock.mock.calls[5][0] as Request;
    expect(request.headers.get('If-Match')).toBe('"v3"');
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-deployment');
  });

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

  it('rotates one credential slot using the edited connection adapter', async () => {
    rememberCsrfToken('csrf-slots');
    const connection = {id: 'connection-2', name: 'Second adapter', adapter_id: 'adapter-2', billing_mode: 'COMMERCIAL_API', endpoint_url: 'https://old.example.test', data_region: 'US', allow_private_ip: false, state: 'ACTIVE', secret_slot_fingerprints: {api_key: '...a1', organization: '...b2'}, version: 7, created_at: '2026-01-01T00:00:00Z'};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [connection], has_more: false}))
      .mockResolvedValueOnce(json({data: [{credential_id: 'key-new', name: 'New API key', trailing_fingerprint: '...c3'}, {credential_id: 'org-new', name: 'New organization', trailing_fingerprint: '...d4'}], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'adapter-1', name: 'First', installation_state: 'ENABLED_ACTIVE', secret_slots: ['token']}, {id: 'adapter-2', name: 'Second', installation_state: 'ENABLED_ACTIVE', secret_slots: ['api_key', 'organization']}], has_more: false}))
      .mockResolvedValueOnce(json({...connection, version: 8}))
      .mockResolvedValueOnce(json({data: [{...connection, version: 8}], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<ConnectionsPage roles={['ORG_OWNER']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Edit connection'}));
    fireEvent.change(screen.getByLabelText('Rotate api_key'), {target: {value: 'key-new'}});
    fireEvent.click(screen.getByRole('button', {name: 'Save connection changes'}));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(7));
    const request = fetchMock.mock.calls[3][0] as Request;
    expect(request.headers.get('If-Match')).toBe('"v7"');
    expect(await request.clone().json()).toMatchObject({secret_bindings: {api_key: 'key-new'}});
  });

  it('deletes a detached connection with its current ETag and session CSRF token', async () => {
    rememberCsrfToken('csrf-delete');
    const connection = {id: 'connection-delete', name: 'Detached connection', adapter_id: 'adapter-1', billing_mode: 'COMMERCIAL_API', endpoint_url: 'https://old.example.test', data_region: 'US', allow_private_ip: false, state: 'DISABLED', secret_slot_fingerprints: {}, version: 5, created_at: '2026-01-01T00:00:00Z'};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [connection], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'adapter-1', name: 'Example', installation_state: 'ENABLED_ACTIVE'}], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(new Response(null, {status: 204}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'adapter-1', name: 'Example', installation_state: 'ENABLED_ACTIVE'}], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<ConnectionsPage roles={['ORG_OWNER']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Delete connection'}));
    const dialog = await screen.findByRole('alertdialog');
    expect(dialog).toHaveTextContent('Active routes referencing its deployments: None');
    fireEvent.click(screen.getAllByRole('button', {name: 'Delete connection'}).at(-1)!);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(9));
    const request = fetchMock.mock.calls[5][0] as Request;
    expect(request.method).toBe('DELETE');
    expect(request.headers.get('If-Match')).toBe('"v5"');
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-delete');
  });

  it('holds an unverified extension behind full permission review and explicit acknowledgement', async () => {
    rememberCsrfToken('csrf-extension');
    const permissionDiff = {
      installation_id: 'installation-1', package_id: '@community/agent', candidate_version: '2.0.0', origin_trust_tier: 'unverified_external',
      enterprise_signed: false, execution_tier: 'sandboxed_wasm', active_execution_tier: null, active_allowed_hosts: [], candidate_allowed_hosts: ['api.example.test'],
      added_allowed_hosts: ['api.example.test'], removed_allowed_hosts: [], secret_slots: ['api_key'], active_secret_slots: [], added_secret_slots: ['api_key'], quota_escalated: false, active_quotas: null,
      quotas: {max_memory_mib: 16, max_cpu_ms_per_call: 25},
      active_permissions: null, candidate_permissions: {execution_tier: 'WASM_SANDBOX_T2', allowed_hosts: ['api.example.test'], secret_slots: ['api_key'], max_memory_mib: 16, max_cpu_ms_per_call: 15, max_stream_chunk_cpu_ms: 5},
      permission_delta: {added_hosts: ['api.example.test'], added_secret_slots: ['api_key'], tier_escalated: true}, has_escalation: true,
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
    expect(screen.getAllByText('api.example.test')).not.toHaveLength(0);
    expect(screen.getAllByText('api_key')).not.toHaveLength(0);
    const audit = await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}});
    expect(audit.violations.filter(item => item.impact === 'critical' || item.impact === 'serious')).toEqual([]);
    const activate = screen.getByRole('button', {name: 'Approve & Activate'});
    expect(activate).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I acknowledge this package is from an unverified external publisher'));
    expect(activate).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I reviewed and explicitly consent to the permission changes and resource limits'));
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
      execution_tier: 'sandboxed_wasm', active_execution_tier: 'declarative', active_allowed_hosts: ['api.example.test'], candidate_allowed_hosts: ['api.example.test', 'new.example.test'],
      added_allowed_hosts: ['new.example.test'], removed_allowed_hosts: [], secret_slots: [], active_secret_slots: [], added_secret_slots: [], quota_escalated: true,
      active_quotas: {max_memory_mib: 8, max_cpu_ms_per_call: 10, max_stream_chunk_cpu_ms: 3}, quotas: {max_memory_mib: 16, max_cpu_ms_per_call: 15, max_stream_chunk_cpu_ms: 5},
      active_permissions: {execution_tier: 'DECLARATIVE_T1', allowed_hosts: ['api.example.test'], secret_slots: [], max_memory_mib: 8, max_cpu_ms_per_call: 10, max_stream_chunk_cpu_ms: 3},
      candidate_permissions: {execution_tier: 'WASM_SANDBOX_T2', allowed_hosts: ['api.example.test', 'new.example.test'], secret_slots: [], max_memory_mib: 16, max_cpu_ms_per_call: 15, max_stream_chunk_cpu_ms: 5},
      permission_delta: {added_hosts: ['new.example.test'], tier_escalated: true, memory_increased: true, cpu_quota_increased: true}, has_escalation: true};
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
    fireEvent.click(screen.getByLabelText('I reviewed and explicitly consent to the permission changes and resource limits'));
    expect(approve).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(approve);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(6));
    const request = fetchMock.mock.calls.find(call => (call[0] as Request).url.endsWith('/approve-permissions'))?.[0] as Request;
    expect(request.headers.get('If-Match')).toBe('"candidate-opaque-xyz"');
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-upgrade');
  });

  it('requires explicit acknowledgement for a quota-only escalation', async () => {
    rememberCsrfToken('csrf-quota');
    const installation = {id: 'quota-installation', package_id: '@community/quota', state: 'UPDATING_PENDING_RECONSENT', origin_trust_tier: 'verified', enterprise_signed: true, execution_tier: 'sandboxed_wasm', version: 4, active_version: '1.0.0'};
    const diff = {installation_id: installation.id, package_id: installation.package_id, active_version: '1.0.0', candidate_version: '2.0.0', origin_trust_tier: 'verified', enterprise_signed: true,
      execution_tier: 'sandboxed_wasm', active_execution_tier: 'sandboxed_wasm', active_allowed_hosts: ['api.example.test'], candidate_allowed_hosts: ['api.example.test'],
      added_allowed_hosts: [], removed_allowed_hosts: [], secret_slots: ['api_key'], active_secret_slots: ['api_key'], added_secret_slots: [], quota_escalated: true,
      active_quotas: {max_memory_mib: 8, max_cpu_ms_per_call: 10, max_stream_chunk_cpu_ms: 3}, quotas: {max_memory_mib: 16, max_cpu_ms_per_call: 10, max_stream_chunk_cpu_ms: 3},
      active_permissions: {execution_tier: 'WASM_SANDBOX_T2', allowed_hosts: ['api.example.test'], secret_slots: ['api_key'], max_memory_mib: 8, max_cpu_ms_per_call: 10, max_stream_chunk_cpu_ms: 3},
      candidate_permissions: {execution_tier: 'WASM_SANDBOX_T2', allowed_hosts: ['api.example.test'], secret_slots: ['api_key'], max_memory_mib: 16, max_cpu_ms_per_call: 10, max_stream_chunk_cpu_ms: 3},
      permission_delta: {memory_increased: true}, has_escalation: true};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [installation], has_more: false}))
      .mockResolvedValueOnce(new Response(JSON.stringify(diff), {status: 200, headers: {'Content-Type': 'application/json', ETag: '"v4"'}}))
      .mockResolvedValueOnce(json({...installation, state: 'ENABLED_ACTIVE', version: 5}))
      .mockResolvedValueOnce(json({data: [{...installation, state: 'ENABLED_ACTIVE', version: 5}], has_more: false}));
    vi.stubGlobal('fetch', fetchMock);
    render(<InstalledExtensionDetailPage roles={['OPERATOR']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Review permissions'}));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Memory: 8 MiB; CPU per call: 10 ms; CPU per stream chunk: 3 ms')).toBeInTheDocument();
    expect(screen.getByText('Memory: 16 MiB; CPU per call: 10 ms; CPU per stream chunk: 3 ms')).toBeInTheDocument();
    const approve = screen.getByRole('button', {name: 'Approve permissions and activate'});
    expect(approve).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I reviewed and explicitly consent to the permission changes and resource limits'));
    expect(approve).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(approve);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    const request = fetchMock.mock.calls[2][0] as Request;
    expect(request.headers.get('If-Match')).toBe('"v4"');
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-quota');
  });

  it('shows and gates hook, network, binding-mode, and payload permission escalations', async () => {
    const installation = {id: 'permissions-installation', package_id: '@community/permissions', state: 'UPDATING_PENDING_RECONSENT', origin_trust_tier: 'verified', enterprise_signed: true, execution_tier: 'sandboxed_wasm', version: 2, active_version: '1.0.0'};
    const activePermissions = {execution_tier: 'WASM_SANDBOX_T2', allowed_hosts: ['api.example.test'], allow_private_ip: false, require_tls: true, secret_slots: ['api_key'], secret_binding_modes: {api_key: 'host_blind_inject'}, hooks: ['provider:before'], inspect_request_body: false, inspect_response_body: false};
    const candidatePermissions = {execution_tier: 'WASM_SANDBOX_T2', allowed_hosts: ['api.example.test'], allow_private_ip: true, require_tls: false, secret_slots: ['api_key'], secret_binding_modes: {api_key: 'host_crypto_sign'}, hooks: ['provider:before', 'provider:after'], inspect_request_body: true, inspect_response_body: false};
    const diff = {installation_id: installation.id, package_id: installation.package_id, active_version: '1.0.0', candidate_version: '2.0.0', origin_trust_tier: 'verified', enterprise_signed: true,
      execution_tier: 'sandboxed_wasm', active_execution_tier: 'sandboxed_wasm', active_allowed_hosts: ['api.example.test'], candidate_allowed_hosts: ['api.example.test'],
      added_allowed_hosts: [], removed_allowed_hosts: [], secret_slots: ['api_key'], active_secret_slots: ['api_key'], added_secret_slots: [], quota_escalated: false,
      active_quotas: {max_memory_mib: 0, max_cpu_ms_per_call: 0, max_stream_chunk_cpu_ms: 0}, quotas: {max_memory_mib: 0, max_cpu_ms_per_call: 0, max_stream_chunk_cpu_ms: 0},
      active_permissions: activePermissions, candidate_permissions: candidatePermissions,
      permission_delta: {added_hooks: ['provider:after'], binding_mode_changed: true, private_ip_escalated: true, tls_downgraded: true, payload_access_escalated: true}, has_escalation: true};
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({data: [installation], has_more: false}))
      .mockResolvedValueOnce(new Response(JSON.stringify(diff), {status: 200, headers: {'Content-Type': 'application/json', ETag: '"v2"'}}));
    vi.stubGlobal('fetch', fetchMock);
    render(<InstalledExtensionDetailPage roles={['OPERATOR']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Review permissions'}));
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    for (const label of ['Hook: provider:after', 'Secret binding mode: api_key', 'Private IP access', 'Require TLS', 'Inspect request body']) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    const approve = screen.getByRole('button', {name: 'Approve permissions and activate'});
    expect(approve).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByLabelText('I reviewed and explicitly consent to the permission changes and resource limits'));
    expect(approve).not.toHaveAttribute('aria-disabled', 'true');
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
    const reviewQuarantine = screen.getByRole('button', {name: 'Review quarantine confirmation'});
    expect(reviewQuarantine).toHaveAttribute('aria-disabled', 'true');
    fireEvent.change(screen.getByLabelText(/Type the exact package ID/), {target: {value: '@community/risky'}});
    fireEvent.change(screen.getByLabelText(/Reason/), {target: {value: 'Malicious behavior'}});
    expect(reviewQuarantine).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(reviewQuarantine);
    const alert = await screen.findByRole('alertdialog');
    expect(alert).toHaveTextContent('connection-a');
    fireEvent.click(screen.getByRole('button', {name: 'Confirm quarantine'}));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4));
    const request = fetchMock.mock.calls.find(call => (call[0] as Request).method==='POST')?.[0] as Request;
    expect(request.headers.get('X-CSRF-Token')).toBe('csrf-quarantine');
    expect(await request.clone().json()).toMatchObject({target_level: 'EXTENSION', target_id: 'installed-3', confirm_slug: '@community/risky', abort_active_streams: true, reason: 'Malicious behavior'});
  });
});

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {status, headers: {'Content-Type': 'application/json'}});
}
