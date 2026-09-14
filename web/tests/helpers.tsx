import type { PropsWithChildren } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { ThemeProvider } from '@mui/material/styles';
import { render } from '@testing-library/react';
import { createAuraTheme } from '../src/design-system/mui';
import { NotificationProvider } from '../src/components/Notifications';
import type { PowerTarget } from '../src/types';

export function providers(router = true) {
  const client = new QueryClient({ defaultOptions: {
    queries: { retry: false, retryDelay: 0, gcTime: 0, refetchOnWindowFocus: false },
    mutations: { retry: false },
  } });
  return function Wrapper({ children }: PropsWithChildren) {
    return <QueryClientProvider client={client}>
      <ThemeProvider theme={createAuraTheme('light')}>
        <NotificationProvider>{router ? <MemoryRouter>{children}</MemoryRouter> : children}</NotificationProvider>
      </ThemeProvider>
    </QueryClientProvider>;
  };
}

export function renderUI(ui: React.ReactElement, router = true) {
  return render(ui, { wrapper: providers(router) });
}

export function target(name: string, state: 'on' | 'off' = 'on', namespace = 'fixture-a'): PowerTarget {
  return {
    metadata: { name: `${namespace}--${name}`, namespace: 'fixture-control' },
    spec: { targetRef: { namespace, name, kind: 'Deployment' } },
    status: {
      observedState: { replicas: state === 'on' ? 2 : 0, suspended: false, powerState: state },
      desiredState: state, managed: true, divergent: false, blocked: false,
    },
  };
}
