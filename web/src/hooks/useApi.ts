import { useQuery } from '@tanstack/react-query';
import type { StatusSummary, DiscoverySummary, SavingsSummary, PowerTarget, AuditEvent } from '../types';
import { friendlyError } from '../utils/errors';

const API_BASE = '/api/v1';

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
  });
  if (res.status === 401) {
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    throw new Error(friendlyError('missing authorization'));
  }
  const contentType = res.headers.get('content-type') || '';
  if (!contentType.includes('application/json')) {
    throw new Error('Server returned an unexpected response. Please reload.');
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(friendlyError(data.error || `Request failed (${res.status})`));
  }
  return res.json();
}

export async function apiPost<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (res.status === 401) {
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    throw new Error(friendlyError('missing authorization'));
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(friendlyError(data.error || `Request failed (${res.status})`));
  }
  return res.json();
}

export async function apiPut<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (res.status === 401) {
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    throw new Error(friendlyError('missing authorization'));
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(friendlyError(data.error || `Request failed (${res.status})`));
  }
  return res.json();
}

export async function apiDelete(path: string): Promise<void> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'DELETE',
    credentials: 'same-origin',
  });
  if (res.status === 401) {
    if (window.location.pathname !== '/login') {
      window.location.href = '/login';
    }
    throw new Error(friendlyError('missing authorization'));
  }
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(friendlyError(data.error || `Request failed (${res.status})`));
  }
}

export function useStatus() {
  return useQuery<StatusSummary>({
    queryKey: ['status'],
    queryFn: () => fetchJSON('/status'),
    refetchInterval: 10000,
  });
}

export function useDiscovery() {
  return useQuery<DiscoverySummary>({
    queryKey: ['discovery'],
    queryFn: () => fetchJSON('/discover'),
    refetchInterval: 30000,
  });
}

export function useTargets(namespace?: string, state?: string) {
  const params = new URLSearchParams();
  if (namespace) params.set('namespace', namespace);
  if (state) params.set('state', state);
  const query = params.toString() ? `?${params.toString()}` : '';

  return useQuery<{ targets: PowerTarget[]; count: number }>({
    queryKey: ['targets', namespace, state],
    queryFn: () => fetchJSON(`/targets${query}`),
    refetchInterval: 15000,
  });
}

export function useExplainTarget(namespace: string, name: string) {
  return useQuery({
    queryKey: ['explain', namespace, name],
    queryFn: () => fetchJSON(`/targets/${namespace}/${name}/explain`),
    enabled: !!namespace && !!name,
  });
}

export function useSavings() {
  return useQuery<SavingsSummary>({
    queryKey: ['savings'],
    queryFn: () => fetchJSON('/savings'),
    refetchInterval: 60000,
  });
}

export function useAuditEvents(targetNamespace?: string, targetName?: string) {
  const params = new URLSearchParams();
  if (targetNamespace) params.set('targetNamespace', targetNamespace);
  if (targetName) params.set('targetName', targetName);
  const query = params.toString() ? `?${params.toString()}` : '';

  return useQuery<{ events: AuditEvent[]; count: number; total: number }>({
    queryKey: ['audit', targetNamespace, targetName],
    queryFn: () => fetchJSON(`/audit${query}`),
    refetchInterval: 15000,
  });
}

export function useNamespaces() {
  return useQuery<{ namespaces: string[] }>({
    queryKey: ['namespaces'],
    queryFn: () => fetchJSON('/namespaces'),
    staleTime: 60000,
  });
}

export function usePolicies() {
  return useQuery<{ items: PolicyResponse[]; count: number }>({
    queryKey: ['policies'],
    queryFn: () => fetchJSON('/policies'),
    refetchInterval: 10000,
  });
}

export function useOverrides() {
  return useQuery<{ items: OverrideResponse[]; count: number }>({
    queryKey: ['overrides'],
    queryFn: () => fetchJSON('/overrides'),
    refetchInterval: 10000,
  });
}

export interface PolicyResponse {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: {
    scope: { namespaces?: string[] };
    schedule: { desiredState: string; windows?: Array<{ start: string; end: string; timezone: string; days?: number[] }> };
    priority: number;
    description?: string;
  };
  status?: { affectedTargets?: number };
}

export interface OverrideResponse {
  metadata: { name: string; namespace: string; creationTimestamp: string };
  spec: {
    scope: { namespaces?: string[]; workloadNames?: string[] };
    state: string;
    priority: number;
    expiresAt: string;
    reason: string;
    reference?: string;
  };
  status?: { phase?: string; expiresIn?: string };
}

export interface NamespaceGroup {
  metadata: { name: string; namespace: string };
  spec: { namespaces: string[] };
}

export function useNamespaceGroups() {
  return useQuery<{ items: NamespaceGroup[]; count: number }>({
    queryKey: ['namespace-groups'],
    queryFn: () => fetchJSON('/namespace-groups'),
    staleTime: 30000,
  });
}
