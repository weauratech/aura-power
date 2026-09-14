import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

export const origin = 'http://localhost/api/v1';
// Only the drawer's unconditional read dependencies have defaults. Every
// business endpoint used by a test must have an explicit response in that test.
export const server = setupServer(
  http.get(`${origin}/namespaces`, () => HttpResponse.json({ namespaces: ['fixture-a', 'fixture-b'] })),
  http.get(`${origin}/targets`, () => HttpResponse.json({ targets: [], count: 0 })),
);
