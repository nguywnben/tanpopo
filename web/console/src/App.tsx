import {Component, lazy, Suspense, type ReactNode} from 'react';
import {Banner as AstryxBanner} from '@astryxdesign/core/Banner';
import {Button as AstryxButton} from '@astryxdesign/core/Button';
import {RouterProvider, useRouter} from './router/RouterContext';
import {ThemeProvider} from './styles/theme';

const SetupPage = lazy(() => import('./pages/setup/SetupPage').then(mod => ({default: mod.SetupPage})));
const LoginPage = lazy(() => import('./pages/auth/LoginPage').then(mod => ({default: mod.LoginPage})));
const ConsoleShell = lazy(() => import('./shell/ConsoleShell').then(mod => ({default: mod.ConsoleShell})));

class ConsoleErrorBoundary extends Component<{children: ReactNode}, {hasError: boolean}> {
  state = {hasError: false};

  static getDerivedStateFromError(): {hasError: boolean} {
    return {hasError: true};
  }

  render() {
    if (this.state.hasError) {
      return (
        <main className="tp-auth-page">
          <div className="tp-auth-panel">
            <h1>Console unavailable</h1>
            <AstryxBanner
              status="error"
              title="Unexpected interface error"
              description="The console encountered an unexpected error. Reload the page to recover."
              container="card"
              collapsible={false}
              role="alert"
            />
            <AstryxButton variant="primary" label="Reload console" onClick={() => window.location.reload()} />
          </div>
        </main>
      );
    }
    return this.props.children;
  }
}

export function App() {
  return (
    <ThemeProvider>
      <RouterProvider>
        <ConsoleErrorBoundary>
          <RootSwitch />
        </ConsoleErrorBoundary>
      </RouterProvider>
    </ThemeProvider>
  );
}

function RootSwitch() {
  const {pathname} = useRouter();
  if (pathname === '/setup') {
    return (
      <Suspense fallback={<main className="tp-auth-page"><p className="tp-muted" role="status">Loading setup…</p></main>}>
        <SetupPage />
      </Suspense>
    );
  }
  if (pathname === '/login') {
    return (
      <Suspense fallback={<main className="tp-auth-page"><p className="tp-muted" role="status">Loading sign-in…</p></main>}>
        <LoginPage />
      </Suspense>
    );
  }
  return (
    <Suspense fallback={<main className="tp-auth-page"><p className="tp-muted" role="status">Loading console…</p></main>}>
      <ConsoleShell />
    </Suspense>
  );
}

export default App;
