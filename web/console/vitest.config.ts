import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {defineConfig} from 'vitest/config';
import react from '@vitejs/plugin-react';
import {i18nPlugin} from './src/i18n/vite-plugin';

const rootDir = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [i18nPlugin(), react()],
  resolve: {
    alias: {
      '@astryxdesign/theme-neutral/fonts.css': path.join(rootDir, 'assets/fonts/local-figtree-fonts.css'),
    },
  },
  test: {
    environment: 'jsdom',
    fileParallelism: false,
    setupFiles: ['./src/test-setup.ts'],
    restoreMocks: true,
  },
});
