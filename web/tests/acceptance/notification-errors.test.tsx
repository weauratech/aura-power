import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { Notifications } from '../../src/pages/Notifications';
import { origin, server } from '../server';
import { renderUI } from '../helpers';

describe('required notification observability contract', () => {
  it('shows a gateway timeout as a load failure rather than an empty channel list', async () => {
    server.use(http.get(`${origin}/notification-channels`, () => HttpResponse.json({ error: 'controller timeout' }, { status: 504 })));
    renderUI(<Notifications />);
    expect(await screen.findByRole('alert')).toHaveTextContent(/failed|timeout/i);
    expect(screen.queryByText('No notification channels')).not.toBeInTheDocument();
  });
});
