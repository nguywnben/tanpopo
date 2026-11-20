import {readFileSync} from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import {act, fireEvent, render, screen} from '@testing-library/react';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';
import {ThemeProvider, useTheme} from './theme';

function ThemeProbe() {
  const {theme, toggleTheme} = useTheme();
  return (
    <div>
      <span data-testid="current-theme">{theme}</span>
      <button type="button" onClick={toggleTheme}>
        Toggle theme
      </button>
    </div>
  );
}

describe('ThemeProvider synchronized Light/Dark switching', () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    document.documentElement.removeAttribute('data-astryx-theme');
    document.documentElement.removeAttribute('data-theme-switching');
    document.documentElement.style.colorScheme = '';
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('syncs root attributes and suppresses transition drift during Light <-> Dark toggles', () => {
    const queuedFrames: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation(cb => {
      queuedFrames.push(cb);
      return queuedFrames.length;
    });
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(id => {
      if (id >= 1 && id <= queuedFrames.length) {
        queuedFrames[id - 1] = () => undefined;
      }
    });

    window.localStorage.setItem('tanpopo-console-theme', 'light');
    render(
      <ThemeProvider>
        <ThemeProbe />
      </ThemeProvider>,
    );

    expect(screen.getByTestId('current-theme')).toHaveTextContent('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.dataset.astryxTheme).toBe('neutral');
    expect(document.documentElement.style.colorScheme).toBe('light');
    expect(document.documentElement.hasAttribute('data-theme-switching')).toBe(false);

    // Light -> Dark
    fireEvent.click(screen.getByRole('button', {name: 'Toggle theme'}));
    expect(screen.getByTestId('current-theme')).toHaveTextContent('dark');
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.astryxTheme).toBe('neutral');
    expect(document.documentElement.style.colorScheme).toBe('dark');
    expect(window.localStorage.getItem('tanpopo-console-theme')).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme-switching')).toBe('true');

    // First frame keeps switching freeze active through paint; second frame restores transitions.
    act(() => {
      queuedFrames.shift()?.(16);
    });
    expect(document.documentElement.getAttribute('data-theme-switching')).toBe('true');

    act(() => {
      queuedFrames.shift()?.(32);
    });
    expect(document.documentElement.hasAttribute('data-theme-switching')).toBe(false);

    // Rapid back-to-back toggles settle cleanly without leaving switching attribute stuck.
    fireEvent.click(screen.getByRole('button', {name: 'Toggle theme'}));
    fireEvent.click(screen.getByRole('button', {name: 'Toggle theme'}));
    fireEvent.click(screen.getByRole('button', {name: 'Toggle theme'}));
    expect(screen.getByTestId('current-theme')).toHaveTextContent('light');
    expect(document.documentElement.dataset.theme).toBe('light');
    expect(document.documentElement.style.colorScheme).toBe('light');
    expect(document.documentElement.getAttribute('data-theme-switching')).toBe('true');

    act(() => {
      while (queuedFrames.length > 0) {
        queuedFrames.shift()?.(48);
      }
    });
    expect(document.documentElement.hasAttribute('data-theme-switching')).toBe(false);
  });

  it('pre-paint theme-init.js applies persisted dark mode before React mounts', () => {
    const initScript = readFileSync(path.resolve(process.cwd(), 'public/theme-init.js'), 'utf8');
    window.localStorage.setItem('tanpopo-console-theme', 'dark');

    // Execute the static pre-paint boot script synchronously as in <head>.
    vm.runInNewContext(initScript, {window, document});

    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(document.documentElement.dataset.astryxTheme).toBe('neutral');
    expect(document.documentElement.style.colorScheme).toBe('dark');
  });
});
