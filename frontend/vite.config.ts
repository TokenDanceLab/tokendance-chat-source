import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

const publicOrigin = (process.env.VITE_PUBLIC_ORIGIN || 'https://chat.tokendancelab.com').replace(/\/+$/, '');
const devApiTarget = process.env.VITE_DEV_API_TARGET || 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [
    react(),
    {
      name: 'public-origin',
      transformIndexHtml(html) {
        return html.replaceAll('%PUBLIC_ORIGIN%', publicOrigin);
      },
    },
  ],
  resolve: {
    alias: [
      { find: '@', replacement: path.resolve(__dirname, './src') },
    ],
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: devApiTarget,
        changeOrigin: true,
      },
      '/ws': {
        target: devApiTarget.replace(/^http/, 'ws'),
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          'react-vendor': ['react', 'react-dom'],
          'markdown': ['react-markdown', 'remark-gfm', 'remark-parse'],
          'icons': ['lucide-react'],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
    exclude: ['src/e2e/**', 'node_modules/**'],
    server: {
      deps: {
        inline: ['@lobehub/icons', '@lobehub/ui'],
      },
    },
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: [
        'src/e2e/**',
        'src/test-setup.ts',
        'src/test-utils.ts',
        'src/main.tsx',
        'src/**/*.test.{ts,tsx}',
        'src/**/__snapshots__/**',
      ],
    },
  },
});
