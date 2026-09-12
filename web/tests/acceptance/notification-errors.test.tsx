import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { Notifications } from '../../src/pages/Notifications';
import { origin, server } from '../server';
import { renderUI } from '../helpers';

describe('required notification observability contract', () => {
  it('shows a loading state while channel status is being fetched', () => {
    server.use(http.get(`${origin}/notification-channels`, async () => {
      await new Promise(resolve => setTimeout(resolve, 100));
      return HttpResponse.json({ items: [], count: 0 });
    }));
    renderUI(<Notifications />);
    expect(screen.getByRole('status', { name: 'Loading notification channels' })).toBeInTheDocument();
  });

  it('shows an empty state only after a successful empty response', async () => {
    server.use(http.get(`${origin}/notification-channels`, () => HttpResponse.json({ items: [], count: 0 })));
    renderUI(<Notifications />);
    expect(await screen.findByText('No notification channels')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows a gateway timeout as a load failure rather than an empty channel list', async () => {
    server.use(http.get(`${origin}/notification-channels`, () => HttpResponse.json({ error: 'controller timeout' }, { status: 504 })));
    renderUI(<Notifications />);
    expect(await screen.findByRole('alert')).toHaveTextContent(/failed|timeout/i);
    expect(screen.queryByText('No notification channels')).not.toBeInTheDocument();
  });
});
