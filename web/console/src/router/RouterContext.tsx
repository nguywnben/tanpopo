import {createContext, useCallback, useContext, useEffect, useMemo, useState, type AnchorHTMLAttributes, type MouseEvent, type ReactNode} from 'react';

type RouterContextValue = {
  pathname: string;
  search: string;
  navigate: (to: string, options?: {replace?: boolean}) => void;
};

const RouterContext = createContext<RouterContextValue | null>(null);

function readLocation() {
  if (typeof window === 'undefined') return {pathname: '/', search: ''};
  return {pathname: window.location.pathname, search: window.location.search};
}

export function RouterProvider({children}: {children: ReactNode}) {
  const [loc, setLoc] = useState(readLocation);

  useEffect(() => {
    const sync = () => setLoc(readLocation());
    window.addEventListener('popstate', sync);
    return () => window.removeEventListener('popstate', sync);
  }, []);

  const navigate = useCallback((to: string, options?: {replace?: boolean}) => {
    if (typeof window === 'undefined') return;
    const url = new URL(to, window.location.origin);
    if (url.origin !== window.location.origin) {
      window.location.assign(url.toString());
      return;
    }
    const next = `${url.pathname}${url.search}${url.hash}`;
    if (options?.replace) window.history.replaceState(null, '', next);
    else window.history.pushState(null, '', next);
    setLoc({pathname: url.pathname, search: url.search});
  }, []);

  const value = useMemo(() => ({pathname: loc.pathname, search: loc.search, navigate}), [loc.pathname, loc.search, navigate]);
  return <RouterContext.Provider value={value}>{children}</RouterContext.Provider>;
}

export function useRouter() {
  const ctx = useContext(RouterContext);
  if (!ctx) {
    return {
      pathname: typeof window === 'undefined' ? '/' : window.location.pathname,
      search: typeof window === 'undefined' ? '' : window.location.search,
      navigate: (to: string, options?: {replace?: boolean}) => {
        if (typeof window === 'undefined') return;
        if (options?.replace) window.location.replace(to);
        else window.location.assign(to);
      },
    };
  }
  return ctx;
}

export function Link({href, onClick, ...rest}: AnchorHTMLAttributes<HTMLAnchorElement> & {href: string}) {
  const {navigate} = useRouter();
  function handleClick(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event);
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    if (rest.target && rest.target !== '_self') return;
    if (href.startsWith('/')) {
      event.preventDefault();
      navigate(href);
    }
  }
  return <a href={href} onClick={handleClick} {...rest} />;
}
