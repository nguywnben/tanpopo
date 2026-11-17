import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import axe from 'axe-core';
import {afterEach, describe, expect, it, vi} from 'vitest';
import App from '../App';

afterEach(() => vi.unstubAllGlobals());

describe('console shell accessibility', () => {
  it('provides labeled context controls and has no serious axe violations', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({principal_id: 'owner-1', email: 'owner@example.test', roles: ['ORG_OWNER'], csrf_token: 'csrf-value', expires_at: '2026-12-01T00:00:00Z'}))
      .mockResolvedValueOnce(json({data: [{id: 'project-1', org_id: 'org-1', slug: 'sample', name: 'Sample Project', version: 1, created_at: '2026-11-01T00:00:00Z'}], has_more: false}))
      .mockResolvedValueOnce(json({data: [{id: 'env-1', org_id: 'org-1', project_id: 'project-1', slug: 'dev', name: 'Development', created_at: '2026-11-01T00:00:00Z'}]}))
      .mockResolvedValueOnce(json({data: [{id: 'env-1', org_id: 'org-1', project_id: 'project-1', slug: 'dev', name: 'Development', created_at: '2026-11-01T00:00:00Z'}]}));
    vi.stubGlobal('fetch', fetchMock);
    document.documentElement.lang = 'en';
    document.title = 'Tanpopo Console';

    render(<App />);
    expect(await screen.findByRole('heading', {name: 'Overview'})).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText('Project')).toHaveValue('project-1'));
    expect(screen.getByLabelText('Environment')).toHaveValue('env-1');
    expect(screen.getByRole('link', {name: 'Emergency controls'})).toHaveAttribute('href', '/security/kill-switches');
    fireEvent.keyDown(window, {key: 'k', ctrlKey: true});
    expect(await screen.findByRole('dialog', {name: 'Tanpopo navigation and commands'})).toBeInTheDocument();
    const results = await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa']}});
    expect(results.violations.filter(item => item.impact === 'critical' || item.impact === 'serious')).toEqual([]);
  });

  it('shows a usable offline shell when the same-origin API is unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Network unavailable')));
    render(<App />);
    expect(await screen.findByText('Running in Local Network / Offline Mode')).toBeInTheDocument();
    expect(screen.getByText('The console is offline. Local assets remain available.')).toBeInTheDocument();
  });
});

function json(value: unknown) {
  return new Response(JSON.stringify(value), {status: 200, headers: {'Content-Type': 'application/json'}});
}
