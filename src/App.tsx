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
        <Text>Giao diện khởi tạo với Astryx Design System và theme Neutral.</Text>
        <Card>
          <VStack gap={4} padding={4}>
            <Heading level={2}>Chào mừng bạn!</Heading>
            <Text>Theme Neutral mang lại cảm giác tối giản, gam màu xám ấm tinh tế giúp người dùng tập trung vào nội dung.</Text>
            <HStack gap={3}>
              <Button label="Khám phá ngay" variant="primary" />
              <Button label="Tùy chỉnh" variant="secondary" />
            </HStack>
          </VStack>
        </Card>
      </VStack>
    </Theme>
  );
}

export default App;
