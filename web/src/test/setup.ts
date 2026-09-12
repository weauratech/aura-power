import '@testing-library/jest-dom/vitest';
import { afterAll, afterEach, beforeAll, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { server } from '../../tests/server';

let interceptedFetch: typeof fetch;
beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' });
  interceptedFetch = globalThis.fetch;
  // Browser fetch accepts relative URLs; Node fetch needs an absolute URL.
  globalThis.fetch = (input, init) => interceptedFetch(
    typeof input === 'string' ? new URL(input, window.location.origin) : input, init,
  );
});
afterEach(() => {
  cleanup();
  server.resetHandlers();
  window.localStorage?.clear?.();
  window.history.replaceState({}, '', '/');
  vi.useRealTimers();
});
afterAll(() => {
  globalThis.fetch = interceptedFetch;
  server.close();
});

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false, media: query, onchange: null,
    addListener() {}, removeListener() {}, addEventListener() {},
    removeEventListener() {}, dispatchEvent: () => false,
  }),
});
