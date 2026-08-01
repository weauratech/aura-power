import { useQuery } from '@tanstack/react-query';

const API_BASE = '/api/v1/metrics';

interface MetricsSample {
  timestamp: string;
  value: number;
}

export interface ClusterMetrics {
  cpuUsage: MetricsSample[];
  cpuCapacity: MetricsSample[];
  cpuRequested: MetricsSample[];
  memoryUsage: MetricsSample[];
  memoryCapacity: MetricsSample[];
  memoryRequested: MetricsSample[];
  nodeCount: MetricsSample[];
}

export interface NamespaceMetricsData {
  namespace: string;
  cpuUsage: MetricsSample[];
  cpuRequested: MetricsSample[];
  memoryUsage: MetricsSample[];
  memoryRequested: MetricsSample[];
}

export interface NodeMetricsData {
  nodeCount: MetricsSample[];
  totalCost: MetricsSample[];
  savedCost: MetricsSample[];
}

export interface CostSummaryData {
  totalClusterCostPerHour: number;
  costByNamespace: Record<string, number>;
  savedCostPerHour: number;
  projectedMonthlySavings: number;
}

async function fetchMetrics<T>(path: string): Promise<T> {
  const headers: Record<string, string> = {};
  const token = localStorage.getItem('aura_token');
  if (token) headers['Authorization'] = `Bearer ${token}`;

  const res = await fetch(`${API_BASE}${path}`, { headers });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `Metrics API error: ${res.status}`);
  }
  return res.json();
}

export function useClusterMetrics(range: string) {
  return useQuery<ClusterMetrics>({
    queryKey: ['metrics', 'cluster', range],
    queryFn: () => fetchMetrics(`/cluster?range=${range}`),
    refetchInterval: 60000,
    retry: 1,
  });
}

export function useNamespaceMetrics(namespace: string, range: string) {
  return useQuery<NamespaceMetricsData>({
    queryKey: ['metrics', 'namespace', namespace, range],
    queryFn: () => fetchMetrics(`/namespace/${namespace}?range=${range}`),
    enabled: !!namespace,
    refetchInterval: 60000,
    retry: 1,
  });
}

export function useNodeMetrics(range: string) {
  return useQuery<NodeMetricsData>({
    queryKey: ['metrics', 'nodes', range],
    queryFn: () => fetchMetrics(`/nodes?range=${range}`),
    refetchInterval: 60000,
    retry: 1,
  });
}

export function useCostSummary() {
  return useQuery<CostSummaryData>({
    queryKey: ['metrics', 'cost'],
    queryFn: () => fetchMetrics('/cost'),
    refetchInterval: 120000,
    retry: 1,
  });
}
