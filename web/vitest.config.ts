import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
    include: ['tests/unit/**/*.test.{ts,tsx}'],
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    restoreMocks: true,
    testTimeout: 10000,
	pool: 'forks',
	maxWorkers: 2,
	minWorkers: 1,
  },
});
