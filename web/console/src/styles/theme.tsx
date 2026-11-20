import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import {Theme} from '@astryxdesign/core/theme';
import {defaultThemeSlug, themes} from '../astryx-themes';

export type ThemeMode = 'light' | 'dark';

type ThemeContextValue = {
  theme: ThemeMode;
  toggleTheme: () => void;
};

const STORAGE_KEY = 'tanpopo-console-theme';
const SWITCHING_ATTR = 'data-theme-switching';
const ThemeContext = createContext<ThemeContextValue>({theme: 'light', toggleTheme: () => undefined});

function initialTheme(): ThemeMode {
  if (typeof window === 'undefined') return 'light';
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    // Ignore storage restrictions and fall back to document attribute / media query.
  }
  const preset = document.documentElement.dataset.theme;
  if (preset === 'light' || preset === 'dark') return preset;
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyRootTheme(mode: ThemeMode) {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  root.dataset.theme = mode;
  root.dataset.astryxTheme = defaultThemeSlug;
  root.style.colorScheme = mode;
}

export function ThemeProvider({children}: {children: ReactNode}) {
  const [theme, setTheme] = useState<ThemeMode>(initialTheme);
  const switchingRef = useRef(false);
  const rafIdsRef = useRef<{outer: number | null; inner: number | null}>({outer: null, inner: null});

  const cancelPendingRestore = useCallback(() => {
    if (typeof window === 'undefined') return;
    if (rafIdsRef.current.outer !== null) {
      window.cancelAnimationFrame(rafIdsRef.current.outer);
      rafIdsRef.current.outer = null;
    }
    if (rafIdsRef.current.inner !== null) {
      window.cancelAnimationFrame(rafIdsRef.current.inner);
      rafIdsRef.current.inner = null;
    }
  }, []);

  useLayoutEffect(() => {
    applyRootTheme(theme);
    try {
      window.localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      // Ignore storage restrictions in private browsing modes.
    }

    if (!switchingRef.current || typeof document === 'undefined') {
      return;
    }

    // Flush style recalculation synchronously while data-theme-switching="true" suppresses
    // per-control transitions across main content and document.body portals.
    void document.documentElement.offsetHeight;

    cancelPendingRestore();
    if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
      rafIdsRef.current.outer = window.requestAnimationFrame(() => {
        rafIdsRef.current.outer = null;
        rafIdsRef.current.inner = window.requestAnimationFrame(() => {
          rafIdsRef.current.inner = null;
          switchingRef.current = false;
          document.documentElement.removeAttribute(SWITCHING_ATTR);
        });
      });
    } else {
      switchingRef.current = false;
      document.documentElement.removeAttribute(SWITCHING_ATTR);
    }
  }, [theme, cancelPendingRestore]);

  useEffect(() => {
    return () => {
      cancelPendingRestore();
      switchingRef.current = false;
      if (typeof document !== 'undefined') {
        document.documentElement.removeAttribute(SWITCHING_ATTR);
      }
    };
  }, [cancelPendingRestore]);

  const toggleTheme = useCallback(() => {
    cancelPendingRestore();
    if (typeof document !== 'undefined') {
      switchingRef.current = true;
      document.documentElement.setAttribute(SWITCHING_ATTR, 'true');
    }
    setTheme(current => (current === 'light' ? 'dark' : 'light'));
  }, [cancelPendingRestore]);

  const value = useMemo(() => ({theme, toggleTheme}), [theme, toggleTheme]);
  return (
    <ThemeContext.Provider value={value}>
      <Theme theme={themes[defaultThemeSlug]} mode={theme}>
        {children}
      </Theme>
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  return useContext(ThemeContext);
}
