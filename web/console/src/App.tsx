import {Component, type ReactNode} from 'react';
import {Heading} from '@astryxdesign/core/Heading';
import {Text} from '@astryxdesign/core/Text';
import {VStack} from '@astryxdesign/core/Stack';
import {Theme} from '@astryxdesign/core/theme';
import {themes, defaultThemeSlug} from '../astryx-themes';
import {ConsoleShell} from './shell/AppShell';

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
  return (
    <Theme theme={themes[defaultThemeSlug]}><ConsoleErrorBoundary><ConsoleShell /></ConsoleErrorBoundary></Theme>
  );
}

export default App;
