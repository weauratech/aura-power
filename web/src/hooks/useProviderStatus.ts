import { useQuery } from '@tanstack/react-query';

interface ProviderStatus {
  metricsAvailable: boolean;
  costAvailable: boolean;
  isLoading: boolean;
}

export function useProviderStatus(): ProviderStatus {
  const { data: metricsCheck, isLoading: ml } = useQuery({
    queryKey: ['provider-check', 'metrics'],
    queryFn: async () => {
      try {
        const res = await fetch('/api/v1/metrics/cluster?range=1h');
        return res.ok;
      } catch { return false; }
    },
    staleTime: 60000,
    refetchInterval: 120000,
  });

  const { data: costCheck, isLoading: cl } = useQuery({
    queryKey: ['provider-check', 'cost'],
    queryFn: async () => {
      try {
        const res = await fetch('/api/v1/metrics/cost');
        return res.ok;
      } catch { return false; }
    },
    staleTime: 60000,
    refetchInterval: 120000,
  });

  return {
    metricsAvailable: metricsCheck ?? false,
    costAvailable: costCheck ?? false,
    isLoading: ml || cl,
  };
}
