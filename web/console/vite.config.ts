import path from 'node:path';
import {fileURLToPath} from 'node:url';
import react from '@vitejs/plugin-react';
import stylexPlugin from '@stylexjs/rollup-plugin';
import {defineConfig} from 'vite';

const rootDir = path.dirname(fileURLToPath(import.meta.url));
const sourceDir = path.join(rootDir, 'src');
const stylex = stylexPlugin({
  fileName: 'stylex.css',
  unstable_moduleResolution: {type: 'commonJS', rootDir: sourceDir},
});
const originalTransform = stylex.transform;
if (typeof originalTransform === 'function') {
  stylex.transform = function (code, id, ...args) {
    if (!id.startsWith(`${sourceDir}${path.sep}`)) return null;
    return originalTransform.call(this, code, id, ...args);
  };
}

export default defineConfig({
  root: rootDir,
  plugins: [react(), stylex],
  resolve: {
    alias: {
      '@astryxdesign/theme-neutral/fonts.css': path.join(rootDir, 'assets/fonts/local-figtree-fonts.css'),
    },
  },
  build: {
    outDir: path.join(rootDir, 'dist'),
    emptyOutDir: true,
    rollupOptions: {output: {manualChunks: undefined}},
  },
});
