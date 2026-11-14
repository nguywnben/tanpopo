import React from 'react';
import '@astryxdesign/core/reset.css';
import '@astryxdesign/core/astryx.css';
import { Theme } from '@astryxdesign/core/theme';
import { themes, defaultThemeSlug } from '../astryx-themes';
import { VStack } from '@astryxdesign/core/VStack';
import { HStack } from '@astryxdesign/core/HStack';
import { Heading } from '@astryxdesign/core/Heading';
import { Text } from '@astryxdesign/core/Text';
import { Button } from '@astryxdesign/core/Button';
import { Card } from '@astryxdesign/core/Card';

export function App() {
  return (
    <Theme theme={themes[defaultThemeSlug]}>
      <VStack gap={6} padding={6}>
        <Heading level={1}>Tanpopo App</Heading>
        <Text>Starter interface built with Astryx Design System and the Neutral theme.</Text>
        <Card>
          <VStack gap={4} padding={4}>
            <Heading level={2}>Welcome!</Heading>
            <Text>The Neutral theme provides a minimalist, subtle warm-gray palette designed to keep focus on your workspace.</Text>
            <HStack gap={3}>
              <Button label="Explore" variant="primary" />
              <Button label="Customize" variant="secondary" />
            </HStack>
          </VStack>
        </Card>
      </VStack>
    </Theme>
  );
}

export default App;
