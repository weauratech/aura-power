import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

// Required product contracts. Known failures remain ordinary FAILED tests so
// the command's non-zero status can be consumed by the quality report.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    css: true,
    include: ['tests/acceptance/**/*.test.{ts,tsx}'],
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    restoreMocks: true,
    testTimeout: 10_000,
	pool: 'forks',
	maxWorkers: 2,
	minWorkers: 1,
  },
});
