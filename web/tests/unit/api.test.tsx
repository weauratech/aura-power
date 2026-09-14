import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { apiDelete, apiPost, apiPut, useTargets } from '../../src/hooks/useApi';
import { useProviderStatus } from '../../src/hooks/useProviderStatus';
import { friendlyError } from '../../src/utils/errors';
import { origin, server } from '../server';
import { providers, target } from '../helpers';

describe('HTTP contracts through MSW (no mocked hooks)', () => {
  it('encodes filters and returns the target response without silently widening scope', async () => {
    const query = vi.fn();
    server.use(http.get(`${origin}/targets`, ({ request }) => {
      const url = new URL(request.url);
      query(Object.fromEntries(url.searchParams));
      return HttpResponse.json({ targets: [target('api')], count: 1 });
    }));
    const { result } = renderHook(() => useTargets('fixture-a', 'off'), { wrapper: providers() });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(query).toHaveBeenCalledWith({ namespace: 'fixture-a', state: 'off' });
    expect(result.current.data?.targets[0].spec.targetRef.name).toBe('api');
  });

  it('rejects an HTML proxy error as an unexpected response', async () => {
    server.use(http.get(`${origin}/targets`, () => new HttpResponse('<html>gateway error</html>', { status: 502, headers: { 'Content-Type': 'text/html' } })));
    const { result } = renderHook(() => useTargets(), { wrapper: providers() });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error?.message).toMatch(/unexpected response/i);
    expect(result.current.data).toBeUndefined();
  });

  it.each(['POST', 'PUT', 'DELETE'])('preserves 403 failure for %s without success', async method => {
    server.use(http.all(`${origin}/fixture`, () => HttpResponse.json({ error: 'insufficient permissions' }, { status: 403 })));
    const request = method === 'POST' ? apiPost('/fixture', {}) : method === 'PUT' ? apiPut('/fixture', {}) : apiDelete('/fixture');
    await expect(request).rejects.toThrow('You do not have permission');
  });

  it('sends JSON and same-origin credentials for an update', async () => {
    const body = { spec: { priority: 0 } };
    server.use(http.put(`${origin}/fixture`, async ({ request }) => {
      expect(request.credentials).toBe('same-origin');
      expect(request.headers.get('content-type')).toBe('application/json');
      expect(await request.json()).toEqual(body);
      return HttpResponse.json({ updated: true });
    }));
    await expect(apiPut('/fixture', body)).resolves.toEqual({ updated: true });
  });

  it('accepts a 204 delete with no response body', async () => {
    server.use(http.delete(`${origin}/fixture`, () => new HttpResponse(null, { status: 204 })));
    await expect(apiDelete('/fixture')).resolves.toBeUndefined();
  });

  it('keeps metrics and cost availability independent', async () => {
    server.use(
      http.get(`${origin}/metrics/cluster`, () => HttpResponse.json({})),
      http.get(`${origin}/metrics/cost`, () => HttpResponse.json({ error: 'cost provider not available' }, { status: 503 })),
    );
    const { result } = renderHook(() => useProviderStatus(), { wrapper: providers() });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current).toMatchObject({ metricsAvailable: true, costAvailable: false });
  });

  it.each([
    ['invalid credentials', 'Username or password is incorrect. Please try again.'],
    ['INSUFFICIENT PERMISSIONS', 'You do not have permission to perform this action.'],
    ['request timeout', 'The request timed out. Please try again.'],
  ])('maps %s into a useful message', (raw, expected) => {
    expect(friendlyError(raw)).toBe(expected);
  });
});
