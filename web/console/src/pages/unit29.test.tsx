import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import type {ReactNode} from 'react';
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import type {components} from '../api/generated';
import {RoutesPage} from './routes/RoutesPage';
import {PoliciesPage} from './policies/PoliciesPage';
import {AccessAndKeysPage} from './access/AccessAndKeysPage';

const apiMocks = vi.hoisted(() => ({
  listRoutesForEnvironment: vi.fn(), createRoute: vi.fn(), updateRoute: vi.fn(), deleteRoute: vi.fn(),
  getEffectivePolicy: vi.fn(), updatePolicy: vi.fn(), listVirtualKeys: vi.fn(), createVirtualKey: vi.fn(),
  rotateVirtualKey: vi.fn(), revokeVirtualKey: vi.fn(), listMembers: vi.fn(), createMember: vi.fn(),
  updateMemberRoles: vi.fn(), listRecentTraces: vi.fn(), listDeployments: vi.fn(), listAdminModels: vi.fn(),
}));
type Role = components['schemas']['RbacRole'];

vi.mock('../api/managementClient', () => apiMocks);
vi.mock('../api/catalogClient', () => ({listDeployments: apiMocks.listDeployments, listAdminModels: apiMocks.listAdminModels}));

afterEach(() => { cleanup(); vi.clearAllMocks(); });
beforeEach(() => { apiMocks.listAdminModels.mockResolvedValue([]); apiMocks.listDeployments.mockResolvedValue([]); });

const route: components['schemas']['RouteResource'] = {id: 'route-1', slug: 'prod-route', env_id: 'env-1', strategy: 'PRIORITY_FAILOVER', max_failover_hops: 3, request_timeout_ms: 60000, targets: [{deployment_id: 'dep-1', priority_order: 1}], version: 4, updated_at: '2026-11-17T00:00:00Z'};
const deployment: components['schemas']['ModelDeployment'] = {id: 'dep-1', env_id: 'env-1', logical_model_id: 'model-1', connection_id: 'connection-1', upstream_model_name: 'model-one', data_region: 'EU', state: 'ACTIVE_HEALTHY', idle_chunk_timeout_ms: 15000, first_byte_timeout_ms: 30000, rate_card: {input_token_nano_usd: 1, output_token_nano_usd: 1, cache_read_token_nano_usd: 0, cache_write_token_nano_usd: 0, reasoning_token_nano_usd: 0, image_unit_nano_usd: 0, audio_sec_nano_usd: 0, document_page_nano_usd: 0, self_hosted_zero_rate: false}, version: 2};
const secondDeployment: components['schemas']['ModelDeployment'] = {...deployment, id: 'dep-2', upstream_model_name: 'model-two'};
const logicalModel: components['schemas']['LogicalModel'] = {id: 'model-1', slug: 'prod-assistant', display_name: 'Production assistant', context_window: 8192, max_output_tokens: 2048, assurance_tier: 'TESTED', capabilities: {streaming: true, tool_calling: false, parallel_tool_calls: false, strict_json_schema: false, json_object: false, vision: false, audio_input: false, document_input: false, reasoning: false, prompt_caching: false, embeddings: false, rerank: false}};
const accessKey: components['schemas']['VirtualKeySummary'] = {id: 'key-1', name: 'service-key', project_id: 'project-1', env_id: 'env-1', key_prefix: 'vk_tp_prod_', last_four: '1234', state: 'CREATED_ACTIVE', version: 2, created_at: '2026-11-17T00:00:00Z'};
const policy = (scope_tier: components['schemas']['PolicyDocument']['scope_tier'], scope_id: string, regions: components['schemas']['PolicyDocument']['allowed_data_regions'], models: string[]): components['schemas']['PolicyDocument'] => ({id: `policy-${scope_tier}`, scope_tier, scope_id, allowed_data_regions: regions, allowed_models: models, min_plugin_trust: 'VERIFIED_ONLY', min_capability_assurance: 'TESTED', version: 1});

describe('Unit 29 routing and access safety', () => {
  it('preserves the route draft and shows the current server resource on a 409', async () => {
    apiMocks.listRoutesForEnvironment.mockResolvedValue([route]);
    apiMocks.listDeployments.mockResolvedValue([deployment]);
    apiMocks.listAdminModels.mockResolvedValue([logicalModel]);
    apiMocks.getEffectivePolicy.mockResolvedValue({policy: {allowed_data_regions: ['EU']}, etag: '"v1"'});
    apiMocks.listRecentTraces.mockResolvedValue([]);
    const latest = {...route, slug: 'server-route', version: 5};
    apiMocks.updateRoute.mockRejectedValueOnce(Object.assign(new Error('optimistic_lock_conflict'), {status: 409, currentResource: latest})).mockResolvedValue({...latest, slug: 'my-unsaved-route', version: 6});

    renderInMain(<RoutesPage projectId="project-1" environmentId="env-1" roles={['ORG_OWNER']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Edit prod-route'}));
    fireEvent.change(screen.getByLabelText('Route slug'), {target: {value: 'my-unsaved-route'}});
    fireEvent.click(screen.getByRole('button', {name: 'Save route'}));

    expect(await screen.findByText('Route changed by another administrator')).toBeInTheDocument();
    expect(screen.getByLabelText('Route slug')).toHaveValue('my-unsaved-route');
    expect(screen.getByText('Current server version')).toBeInTheDocument();
    expect(screen.getByText(/server-route/)).toBeInTheDocument();
    expect(await allAxeViolations()).toEqual([]);
    expect(apiMocks.updateRoute).toHaveBeenCalledWith(route, expect.objectContaining({slug: 'my-unsaved-route'}));
    fireEvent.click(screen.getByRole('button', {name: 'Use latest version and keep draft'}));
    fireEvent.click(screen.getByRole('button', {name: 'Save route'}));
    await waitFor(() => expect(apiMocks.updateRoute).toHaveBeenLastCalledWith(latest, expect.objectContaining({slug: 'my-unsaved-route'})));
  });

  it('creates a weighted route only after its fallback weights total 100 percent', async () => {
    apiMocks.listRoutesForEnvironment.mockResolvedValue([]);
    apiMocks.listDeployments.mockResolvedValue([deployment, secondDeployment]);
    apiMocks.listAdminModels.mockResolvedValue([logicalModel]);
    apiMocks.getEffectivePolicy.mockResolvedValue({policy: {allowed_data_regions: ['EU', 'US', 'APAC', 'ON_PREM']}, etag: '"v1"'});
    apiMocks.listRecentTraces.mockResolvedValue([]);
    apiMocks.createRoute.mockResolvedValue(route);

    renderInMain(<RoutesPage projectId="project-1" environmentId="env-1" roles={['TEAM_ADMIN']} />);
    fireEvent.change(await screen.findByLabelText('Route slug'), {target: {value: 'weighted-route'}});
    expect(await screen.findByRole('option', {name: /model-one · ACTIVE_HEALTHY/})).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Routing strategy'), {target: {value: 'WEIGHTED'}});
    fireEvent.change(screen.getByLabelText('Deployment'), {target: {value: 'dep-1'}});
    fireEvent.click(screen.getByRole('button', {name: 'Add deployment'}));
    fireEvent.change(screen.getByLabelText('Deployment'), {target: {value: 'dep-2'}});
    fireEvent.click(screen.getByRole('button', {name: 'Add deployment'}));
    expect(screen.getByLabelText('Route slug')).toHaveValue('weighted-route');
    expect(screen.getByLabelText('Weight for dep-1')).toHaveValue('50');
    expect(screen.getByLabelText('Weight for dep-2')).toHaveValue('50');
    fireEvent.click(screen.getByRole('button', {name: 'Create route'}));
    await waitFor(() => expect(apiMocks.createRoute).toHaveBeenCalledWith(expect.objectContaining({slug: 'weighted-route', strategy: 'WEIGHTED', targets: [
      {deployment_id: 'dep-1', priority_order: 1, weight: 50}, {deployment_id: 'dep-2', priority_order: 2, weight: 50},
    ], allowed_data_regions: []})));
  });

  it('warns when a selected fallback lacks tool calling and offers the supported route strategies', async () => {
    apiMocks.listRoutesForEnvironment.mockResolvedValue([]);
    apiMocks.listDeployments.mockResolvedValue([deployment]);
    apiMocks.listAdminModels.mockResolvedValue([logicalModel]);
    apiMocks.getEffectivePolicy.mockResolvedValue({policy: {allowed_data_regions: ['EU']}, etag: '"v1"'});
    apiMocks.listRecentTraces.mockResolvedValue([]);
    renderInMain(<RoutesPage projectId="project-1" environmentId="env-1" roles={['TEAM_ADMIN']} />);
    fireEvent.change(await screen.findByLabelText('Route slug'), {target: {value: 'capability-route'}});
    expect(screen.getByRole('option', {name: 'LOWEST LATENCY EWMA'})).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Deployment'), {target: {value: 'dep-1'}});
    fireEvent.click(screen.getByRole('button', {name: 'Add deployment'}));
    expect(await screen.findByText(/does not declare tool_calling/)).toBeInTheDocument();
  });

  it('fails closed when every selected fallback violates inherited residency', async () => {
    apiMocks.listRoutesForEnvironment.mockResolvedValue([]);
    apiMocks.listDeployments.mockResolvedValue([{...deployment, data_region: 'US'}]);
    apiMocks.listAdminModels.mockResolvedValue([logicalModel]);
    apiMocks.getEffectivePolicy.mockResolvedValue({policy: {allowed_data_regions: ['EU']}, etag: '"v1"'});
    apiMocks.listRecentTraces.mockResolvedValue([]);
    renderInMain(<RoutesPage projectId="project-1" environmentId="env-1" roles={['TEAM_ADMIN']} />);
    fireEvent.change(await screen.findByLabelText('Route slug'), {target: {value: 'eu-only-route'}});
    fireEvent.change(screen.getByLabelText('Deployment'), {target: {value: 'dep-1'}});
    fireEvent.click(screen.getByRole('button', {name: 'Add deployment'}));
    expect(await screen.findByText(/violates the inherited data residency policy/)).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Create route'})).toHaveAttribute('aria-disabled', 'true');
    expect(apiMocks.createRoute).not.toHaveBeenCalled();
  });

  it('labels inherited organization region and model restrictions and prevents widening them', async () => {
    apiMocks.getEffectivePolicy
      .mockResolvedValueOnce({policy: policy('ORGANIZATION', 'org-1', ['EU', 'US'], ['prod-assistant']), etag: '"v1"'})
      .mockResolvedValueOnce({policy: policy('PROJECT', 'project-1', ['EU'], ['prod-assistant']), etag: '"v1"'})
      .mockResolvedValueOnce({policy: policy('ENVIRONMENT', 'env-1', ['EU'], ['prod-assistant']), etag: '"v1"'});
    renderInMain(<PoliciesPage projectId="project-1" environmentId="env-1" roles={['TEAM_ADMIN']} />);
    expect(await screen.findAllByText('Organization Policy · inherited floor')).toHaveLength(3);
    fireEvent.change(screen.getByLabelText('Allowed model slugs (comma-separated)'), {target: {value: '*'}});
    expect(screen.getByText(/cannot be widened/)).toBeInTheDocument();
    expect(screen.getByRole('button', {name: 'Save policy'})).toHaveAttribute('aria-disabled', 'true');
    expect(await allAxeViolations()).toEqual([]);
  });

  it('adopts a conflicted policy revision while preserving the edited draft', async () => {
    const initial = policy('ENVIRONMENT', 'env-1', ['EU'], []);
    const latest = {...initial, version: 2};
    apiMocks.getEffectivePolicy.mockResolvedValue({policy: initial, etag: '"v1"'});
    apiMocks.updatePolicy.mockRejectedValueOnce(Object.assign(new Error('optimistic_lock_conflict'), {status: 409, currentResource: latest})).mockResolvedValue({policy: latest, etag: '"v2"'});
    renderInMain(<PoliciesPage projectId="project-1" environmentId="env-1" roles={['TEAM_ADMIN']} />);
    expect(await screen.findByLabelText('Allowed model slugs (comma-separated)')).toHaveValue('');
    fireEvent.change(screen.getByLabelText('Minimum plugin trust'), {target: {value: 'OFFICIAL_ONLY'}});
    fireEvent.click(screen.getByRole('button', {name: 'Save policy'}));
    expect(await screen.findByText('Policy changed by another administrator')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', {name: 'Use latest version and keep draft'}));
    expect(screen.getByLabelText('Allowed model slugs (comma-separated)')).toHaveValue('');
    fireEvent.click(screen.getByRole('button', {name: 'Save policy'}));
    await waitFor(() => expect(apiMocks.updatePolicy).toHaveBeenLastCalledWith(expect.objectContaining({version: 2}), '"v2"', expect.objectContaining({allowed_models: [], min_plugin_trust: 'OFFICIAL_ONLY', version: 2})));
    expect(apiMocks.updatePolicy).toHaveBeenNthCalledWith(1, expect.anything(), '"v1"', expect.anything());
  });

  it('loads virtual keys for developers without requesting member records', async () => {
    apiMocks.listVirtualKeys.mockResolvedValue([accessKey]);
    apiMocks.listRecentTraces.mockResolvedValue([]);
    apiMocks.listAdminModels.mockResolvedValue([]);
    apiMocks.listDeployments.mockResolvedValue([]);
    renderInMain(<AccessAndKeysPage projectId="project-1" environmentId="env-1" currentPrincipalId="dev-1" actorRoles={['DEVELOPER']} />);
    expect(await screen.findByText('vk_tp_prod_')).toBeInTheDocument();
    expect(apiMocks.listMembers).not.toHaveBeenCalled();
    expect(await allAxeViolations()).toEqual([]);
    fireEvent.click(screen.getByRole('button', {name: 'Members and roles'}));
    expect(await screen.findByText(/can view virtual keys but does not have access to member records/)).toBeInTheDocument();
    expect(await allAxeViolations()).toEqual([]);
  });


  it('requires typed key name and an audit reason before emergency revocation', async () => {
    apiMocks.listVirtualKeys.mockResolvedValue([accessKey]);
    apiMocks.listMembers.mockResolvedValue([]);
    apiMocks.listRecentTraces.mockResolvedValue([{virtual_key_id: 'key-1', created_at: new Date().toISOString()}]);
    apiMocks.revokeVirtualKey.mockResolvedValue({key: {...accessKey, state: 'REVOKED'}, severed_streams_count: 2});

    renderInMain(<AccessAndKeysPage projectId="project-1" environmentId="env-1" currentPrincipalId="owner-1" actorRoles={['ORG_OWNER']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Revoke service-key'}));
    fireEvent.change(screen.getByLabelText('Revocation mode'), {target: {value: 'EMERGENCY_ABORT_STREAMS'}});
    expect(screen.getByText(/Recent requests for this key in the last hour: 1/)).toBeInTheDocument();
    const revokeButton = screen.getByRole('button', {name: 'Emergency revoke key'});
    expect(revokeButton).toHaveAttribute('aria-disabled', 'true');
    fireEvent.change(screen.getByLabelText('Type the virtual key name'), {target: {value: 'service-key'}});
    expect(revokeButton).toHaveAttribute('aria-disabled', 'true');
    fireEvent.change(screen.getByLabelText('Audit reason'), {target: {value: 'incident response'}});
    expect(revokeButton).not.toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(revokeButton);
    await waitFor(() => expect(apiMocks.revokeVirtualKey).toHaveBeenCalledWith(accessKey, 'EMERGENCY_ABORT_STREAMS', 'incident response'));
    expect(await screen.findByText(/2 active streams were severed/)).toBeInTheDocument();
  });

  it('allows Security Reviewers to emergency revoke keys but not rotate them', async () => {
    apiMocks.listVirtualKeys.mockResolvedValue([accessKey]);
    apiMocks.listMembers.mockResolvedValue([]);
    apiMocks.listRecentTraces.mockResolvedValue([]);
    apiMocks.revokeVirtualKey.mockResolvedValue({key: {...accessKey, state: 'REVOKED'}, severed_streams_count: 1});
    renderInMain(<AccessAndKeysPage projectId="project-1" environmentId="env-1" currentPrincipalId="reviewer-1" actorRoles={['SECURITY_REVIEWER']} />);
    expect(await screen.findByRole('button', {name: 'Rotate service-key'})).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(screen.getByRole('button', {name: 'Revoke service-key'}));
    expect(screen.getByLabelText('Revocation mode')).toHaveValue('EMERGENCY_ABORT_STREAMS');
    fireEvent.change(screen.getByLabelText('Type the virtual key name'), {target: {value: 'service-key'}});
    fireEvent.change(screen.getByLabelText('Audit reason'), {target: {value: 'incident response'}});
    fireEvent.click(screen.getByRole('button', {name: 'Emergency revoke key'}));
    await waitFor(() => expect(apiMocks.revokeVirtualKey).toHaveBeenCalledWith(accessKey, 'EMERGENCY_ABORT_STREAMS', 'incident response'));
  });

  it('reveals a created key once and clears it from the screen when the dialog closes', async () => {
    apiMocks.listVirtualKeys.mockResolvedValue([]);
    apiMocks.listMembers.mockResolvedValue([]);
    apiMocks.listRecentTraces.mockResolvedValue([]);
    apiMocks.listAdminModels.mockResolvedValue([logicalModel]);
    apiMocks.listDeployments.mockResolvedValue([deployment]);
    apiMocks.createVirtualKey.mockResolvedValue({...accessKey, secret_key: 'vk_tp_secret_once'});

    renderInMain(<AccessAndKeysPage projectId="project-1" environmentId="env-1" currentPrincipalId="owner-1" actorRoles={['ORG_OWNER']} />);
    fireEvent.change(screen.getByLabelText('Virtual key name'), {target: {value: 'new service'}});
    fireEvent.click(screen.getByLabelText('All models'));
    fireEvent.click(await screen.findByLabelText('prod-assistant'));
    fireEvent.change(screen.getByLabelText('Allowed MCP tools (comma-separated; server__tool format)'), {target: {value: 'docs__search, billing__read_invoice'}});
    fireEvent.click(screen.getByRole('button', {name: 'Create key'}));
    expect(await screen.findByText('vk_tp_secret_once')).toBeInTheDocument();
    expect(await allAxeViolations()).toEqual([]);
    fireEvent.click(screen.getByRole('button', {name: 'I stored it; close and erase secret'}));
    expect(screen.queryByText('vk_tp_secret_once')).not.toBeInTheDocument();
    expect(apiMocks.createVirtualKey).toHaveBeenCalledWith({name: 'new service', project_id: 'project-1', env_id: 'env-1', allowed_models: ['prod-assistant'], allowed_mcp_tools: ['docs__search', 'billing__read_invoice']});
  });

  it('blocks a principal from granting themselves a new RBAC role', async () => {
    const self = {id: 'owner-1', email: 'owner@example.test', kind: 'HUMAN' as const, roles: ['DEVELOPER'] as Role[], version: 1, created_at: '2026-11-17T00:00:00Z'};
    apiMocks.listVirtualKeys.mockResolvedValue([]);
    apiMocks.listMembers.mockResolvedValue([self]);
    apiMocks.listRecentTraces.mockResolvedValue([]);
    renderInMain(<AccessAndKeysPage projectId="project-1" environmentId="env-1" currentPrincipalId="owner-1" actorRoles={['ORG_OWNER']} />);
    fireEvent.click(await screen.findByRole('button', {name: 'Members and roles'}));
    fireEvent.click(screen.getByRole('button', {name: 'Edit roles for owner@example.test'}));
    expect(screen.getAllByLabelText('ORG OWNER').at(-1)).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByText(/cannot grant yourself additional access/)).toBeInTheDocument();
    expect(apiMocks.updateMemberRoles).not.toHaveBeenCalled();
  });
});

async function allAxeViolations() {
  const result = await axe.run(document.body);
  return result.violations.map(violation => ({id: violation.id, impact: violation.impact, help: violation.help, nodes: violation.nodes.slice(0, 3).map(node => ({target: node.target, summary: node.failureSummary}))}));
}

function renderInMain(node: ReactNode) { return render(<main>{node}</main>); }
