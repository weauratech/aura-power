import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { ScheduleDrawer } from '../../src/components/ScheduleDrawer';
import { origin, server } from '../server';
import { renderUI } from '../helpers';

describe('schedule form contracts', () => {
  it('submits an explicit namespace scope exactly once and reports completion', async () => {
    const received = vi.fn();
    server.use(http.post(`${origin}/policies`, async ({ request }) => {
      received(await request.json());
      return HttpResponse.json({ created: true }, { status: 201 });
    }));
    const onClose = vi.fn();
    const onSuccess = vi.fn();
    renderUI(<ScheduleDrawer open onClose={onClose} onSuccess={onSuccess} prefill={{ namespaces: ['fixture-a'] }} />);

    const user = userEvent.setup();
    await user.type(screen.getByRole('textbox', { name: /^Name/ }), 'fixture-policy');
    await user.click(screen.getByRole('button', { name: 'Create Schedule' }));

    await waitFor(() => expect(received).toHaveBeenCalledTimes(1));
    expect(received).toHaveBeenCalledWith(expect.objectContaining({
      metadata: { name: 'fixture-policy', namespace: 'aura-system' },
      spec: expect.objectContaining({
        scope: { namespaces: ['fixture-a'] },
        schedule: expect.objectContaining({ desiredState: 'off' }),
      }),
    }));
    expect(onSuccess).toHaveBeenCalledWith('Schedule "fixture-policy" created successfully');
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('keeps the drawer open and exposes an API validation error', async () => {
    server.use(http.post(`${origin}/policies`, () => HttpResponse.json({ error: 'invalid timezone' }, { status: 422 })));
    const onClose = vi.fn();
    renderUI(<ScheduleDrawer open onClose={onClose} prefill={{ namespaces: ['fixture-a'] }} />);

    const user = userEvent.setup();
    await user.type(screen.getByRole('textbox', { name: /^Name/ }), 'invalid-policy');
    await user.click(screen.getByRole('button', { name: 'Create Schedule' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(/invalid timezone/i);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('textbox', { name: /^Name/ })).toHaveValue('invalid-policy');
  });

  it('previews the same namespace scope before creation', async () => {
    const received = vi.fn();
    server.use(http.post(`${origin}/preview/policy`, async ({ request }) => {
      received(await request.json());
      return HttpResponse.json({ affectedTargets: 2, poweredOn: 0, poweredOff: 1, blocked: 1 });
    }));
    renderUI(<ScheduleDrawer open onClose={() => undefined} prefill={{ namespaces: ['fixture-a'] }} />);

    await userEvent.click(screen.getByRole('button', { name: 'Preview Impact' }));

    expect(await screen.findByText('This schedule will affect 2 target(s)')).toBeVisible();
    expect(screen.getByText(/1 will be powered off/)).toBeVisible();
    expect(screen.getByText(/1 blocked by guardrails/)).toBeVisible();
    expect(received).toHaveBeenCalledWith(expect.objectContaining({ scope: { namespaces: ['fixture-a'] } }));
  });
});
