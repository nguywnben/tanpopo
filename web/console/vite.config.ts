import path from 'node:path';
import {fileURLToPath} from 'node:url';
import react from '@vitejs/plugin-react';
import {defineConfig} from 'vite';

const rootDir = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  root: rootDir,
  plugins: [react()],
  build: {
    outDir: path.join(rootDir, 'dist'),
    emptyOutDir: true,
    minify: 'terser',
    terserOptions: {
      compress: {passes: 2},
      mangle: true,
      format: {comments: false},
    },
    rollupOptions: {
      output: {
        manualChunks: undefined,
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/admin': 'http://127.0.0.1:8080',
      '/v1': 'http://127.0.0.1:8080',
      '/mcp': 'http://127.0.0.1:8080',
      '/readyz': 'http://127.0.0.1:8080',
      '/healthz': 'http://127.0.0.1:8080',
    },
  },
});
