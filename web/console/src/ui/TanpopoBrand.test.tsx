import {cleanup, render} from '@testing-library/react';
import {afterEach, describe, expect, it} from 'vitest';
import {RouterProvider} from '../router/RouterContext';
import {TanpopoBrand} from './TanpopoBrand';

describe('TanpopoBrand', () => {
  afterEach(() => {
    cleanup();
  });

  it('renders an interactive link with unified 20px mark and wordmark when href is provided', () => {
    const {container} = render(
      <RouterProvider>
        <TanpopoBrand href="/overview" className="tp-topbar-brand" />
      </RouterProvider>,
    );
    const link = container.querySelector('a.tp-brand.tp-topbar-brand');
    expect(link).not.toBeNull();
    expect(link?.getAttribute('href')).toBe('/overview');
    const svg = link?.querySelector('svg');
    expect(svg?.getAttribute('width')).toBe('20');
    expect(svg?.getAttribute('height')).toBe('20');
    expect(svg?.getAttribute('aria-hidden')).toBe('true');
    expect(link?.querySelector('.tp-brand-text')?.textContent).toBe('Tanpopo');
  });

  it('renders a non-interactive container with identical 20px mark on auth screens', () => {
    const {container} = render(<TanpopoBrand className="tp-auth-brand" />);
    expect(container.querySelector('a')).toBeNull();
    const lockup = container.querySelector('div.tp-brand.tp-auth-brand');
    expect(lockup).not.toBeNull();
    const svg = lockup?.querySelector('svg');
    expect(svg?.getAttribute('width')).toBe('20');
    expect(svg?.getAttribute('height')).toBe('20');
    expect(lockup?.querySelector('.tp-brand-text')?.textContent).toBe('Tanpopo');
  });
});
