import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
	  '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    manifest: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          if (id.includes('/recharts/') || id.includes('/d3-') || id.includes('/victory-vendor/')) return 'charts-vendor';
          if (id.includes('/@mui/') || id.includes('/@emotion/') || id.includes('/react-transition-group/')) return 'ui-vendor';
          if (id.includes('/@tanstack/')) return 'query-vendor';
          return 'vendor';
        },
      },
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': 'http://localhost:9090',
      '/healthz': 'http://localhost:9090',
    },
  },
});
