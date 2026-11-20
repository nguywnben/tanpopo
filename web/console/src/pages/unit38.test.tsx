import {afterEach, describe, expect, it, vi} from 'vitest';
import type {ReactNode} from 'react';
import {cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {Theme} from '@astryxdesign/core/theme';
import {neutralTheme} from '@astryxdesign/theme-neutral/built';
import '@astryxdesign/theme-neutral/theme.css';
import {McpHubPage} from './mcp/McpHubPage';
import {PlaygroundPage} from './playground/PlaygroundPage';

const mocks = vi.hoisted(() => ({
  listMcpServers: vi.fn(), listVirtualKeys: vi.fn(), registerMcpServer: vi.fn(), syncMcpServerTools: vi.fn(), createMcpToolGrant: vi.fn(),
  createPlaygroundSession: vi.fn(), getTraceWaterfall: vi.fn(),
}));
vi.mock('../api/managementClient', () => mocks);

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.clearAllMocks(); });

describe('Unit 38 developer and MCP governance screens', () => {
  it('shows MCP schema drift as text and keeps read-only roles from registering tools', async () => {
    mocks.listMcpServers.mockResolvedValue([{id: 'server-1', project_id: 'project-1', slug: 'docs', name: 'Docs MCP', transport: 'streamable_http', endpoint_url: 'https://mcp.example.test', allow_private_ip: false, state: 'SCHEMA_DRIFT_LOCKED', tool_count: 1, version: 1}]);
    mocks.listVirtualKeys.mockResolvedValue([]);
    renderTheme(<McpHubPage projectId="project-1" environmentId="env-1" roles={['DEVELOPER']} />);
    expect(await screen.findByText('SCHEMA_DRIFT_LOCKED')).toBeInTheDocument();
    expect(screen.queryByLabelText('Server slug')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', {name: 'Register server'})).not.toBeInTheDocument();
    expect(await seriousAxeViolations()).toEqual([]);
  });

  it('uses a temporary cost-capped key for side-by-side SSE comparison without sending cookies', async () => {
    mocks.createPlaygroundSession.mockResolvedValue({session_id: 'session-1', ephemeral_virtual_key: 'vk_tp_dev_ephemeral', env_id: 'env-1', expires_at: '2026-11-20T12:15:00Z', max_budget_micros: 500000});
    mocks.getTraceWaterfall
      .mockResolvedValueOnce({request_id: 'req-primary', route_slug: 'model-a', status_code: 200, usage_accuracy: 'EXACT', total_cost_micros: 1200, duration_ms: 25})
      .mockResolvedValueOnce({request_id: 'req-comparison', route_slug: 'model-b', status_code: 200, usage_accuracy: 'ESTIMATED', total_cost_micros: 900, duration_ms: 30});
    const response = (requestId: string) => new Response('data: {"choices":[{"delta":{"content":"hello"}}]}\n\ndata: [DONE]\n\n', {status: 200, headers: {'Content-Type': 'text/event-stream', 'X-Tanpopo-Request-Id': requestId}});
    const fetchMock = vi.fn().mockImplementationOnce(() => response('req-primary')).mockImplementationOnce(() => response('req-comparison'));
    vi.stubGlobal('fetch', fetchMock);
    renderTheme(<PlaygroundPage projectId="project-1" environmentId="env-1" roles={['DEVELOPER']} />);
    fireEvent.change(screen.getByLabelText('Model or route slug'), {target: {value: 'model-a'}});
    fireEvent.change(screen.getByLabelText('Comparison model (optional)'), {target: {value: 'model-b'}});
    fireEvent.change(screen.getByLabelText('Prompt'), {target: {value: 'Hello'} });
    fireEvent.click(screen.getByRole('button', {name: 'Send'}));
    expect(await screen.findAllByText('hello')).not.toHaveLength(0);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByRole('heading', {name: 'model-b'})).toBeInTheDocument();
    expect(await screen.findByText(/Primary request req-primary: route model-a, status 200, exact usage/)).toBeInTheDocument();
    expect(await screen.findByText(/Comparison request req-comparison: route model-b, status 200, estimated usage/)).toBeInTheDocument();
    expect(mocks.getTraceWaterfall).toHaveBeenNthCalledWith(1, 'req-primary');
    expect(mocks.getTraceWaterfall).toHaveBeenNthCalledWith(2, 'req-comparison');
    const requestInit = fetchMock.mock.calls[0][1] as RequestInit;
    const comparisonRequestInit = fetchMock.mock.calls[1][1] as RequestInit;
    expect(requestInit.credentials).toBe('omit');
    expect(comparisonRequestInit.credentials).toBe('omit');
    expect(requestInit.headers).toMatchObject({'Authorization': 'Bearer vk_tp_dev_ephemeral', 'Accept': 'text/event-stream'});
    expect(JSON.parse(String(requestInit.body))).toMatchObject({model: 'model-a', stream: true});
    expect(await seriousAxeViolations()).toEqual([]);
  });

  it('denies FINOPS_VIEWER access to Playground before minting a key', () => {
    renderTheme(<PlaygroundPage projectId="project-1" environmentId="env-1" roles={['FINOPS_VIEWER']} />);
    expect(screen.getByRole('alert')).toHaveTextContent('cannot create Playground sessions');
    expect(mocks.createPlaygroundSession).not.toHaveBeenCalled();
  });
});

function renderTheme(node: ReactNode) {
  document.documentElement.lang = 'en';
  document.title = 'Tanpopo Console';
  return render(<Theme theme={neutralTheme}>{node}</Theme>);
}
async function seriousAxeViolations() {
  const result = await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}});
  return result.violations.filter(item => item.impact === 'critical' || item.impact === 'serious').map(item => item.id);
}
