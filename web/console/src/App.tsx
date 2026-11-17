import {Component, lazy, Suspense, useEffect, useState, type ReactNode} from 'react';
import {Heading} from '@astryxdesign/core/Heading';
import {Text} from '@astryxdesign/core/Text';
import {VStack} from '@astryxdesign/core/Stack';
import {Theme} from '@astryxdesign/core/theme';
import {themes, defaultThemeSlug} from '../astryx-themes';

const SetupWizardPage = lazy(() => import('./pages/setup/SetupWizardPage').then(module => ({default: module.SetupWizardPage})));
const LoginPage = lazy(() => import('./pages/auth/LoginPage').then(module => ({default: module.LoginPage})));
const ConsoleShell = lazy(() => import('./shell/AppShell').then(module => ({default: module.ConsoleShell})));

class ConsoleErrorBoundary extends Component<{children: ReactNode}, {hasError: boolean}> {
  state = {hasError: false};

  static getDerivedStateFromError(): {hasError: boolean} {
    return {hasError: true};
  }

  render() {
    if (this.state.hasError) {
      return <VStack gap={4} padding={4}><Heading level={1}>Console unavailable</Heading><Text role="alert">The console encountered an unexpected error. Reload the page to try again.</Text></VStack>;
    }
    return this.props.children;
  }
}

export function App() {
  return <Theme theme={themes[defaultThemeSlug]}><ConsoleErrorBoundary><PageRouter /></ConsoleErrorBoundary></Theme>;
}

function PageRouter() {
  const [pathname, setPathname] = useState(window.location.pathname);
  useEffect(() => {
    const updatePath = () => setPathname(window.location.pathname);
    window.addEventListener('popstate', updatePath);
    return () => window.removeEventListener('popstate', updatePath);
  }, []);
  if (pathname === '/setup') return <Suspense fallback={<Text role="status">Loading first-run setup…</Text>}><SetupWizardPage /></Suspense>;
  if (pathname === '/login') return <Suspense fallback={<Text role="status">Loading sign-in…</Text>}><LoginPage /></Suspense>;
  return <Suspense fallback={<Text role="status">Loading console…</Text>}><ConsoleShell /></Suspense>;
}

export default App;
