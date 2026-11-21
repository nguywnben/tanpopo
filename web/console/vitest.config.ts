import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {defineConfig} from 'vitest/config';
import react from '@vitejs/plugin-react';

const rootDir = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@astryxdesign/theme-neutral/fonts.css': path.join(rootDir, 'assets/fonts/local-google-sans-fonts.css'),
    },
  },
  test: {
    environment: 'jsdom',
    fileParallelism: false,
    setupFiles: ['./src/test-setup.ts'],
    restoreMocks: true,
  },
});
