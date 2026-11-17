import {defineConfig} from 'vitest/config';
import react from '@vitejs/plugin-react';
import stylexPlugin from '@stylexjs/rollup-plugin';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const rootDir = path.dirname(fileURLToPath(import.meta.url));
const sourceDir = path.join(rootDir, 'src');
const stylex = stylexPlugin({unstable_moduleResolution: {type: 'commonJS', rootDir: sourceDir}});
const originalTransform = stylex.transform;
if (typeof originalTransform === 'function') {
  stylex.transform = function (code, id, ...args) {
    if (!id.startsWith(`${sourceDir}${path.sep}`)) return null;
    return originalTransform.call(this, code, id, ...args);
  };
}

export default defineConfig({
  plugins: [stylex, react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    restoreMocks: true,
  },
});
