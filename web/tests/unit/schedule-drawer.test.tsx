import { screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { ScheduleDrawer } from '../../src/components/ScheduleDrawer';
import { origin, server } from '../server';
import { renderUI } from '../helpers';
import { target } from '../helpers';
import { CurrentUserContext } from '../../src/contexts/CurrentUser';

describe('schedule form contracts', () => {
  it('traps focus, closes with Escape, and returns focus to its trigger', async () => {
    function Harness() {
      const [open, setOpen] = useState(false);
      return <>
        <button onClick={() => setOpen(true)}>Open schedule</button>
        <ScheduleDrawer open={open} onClose={() => setOpen(false)} />
      </>;
    }

    renderUI(<Harness />);
    const user = userEvent.setup();
    const trigger = screen.getByRole('button', { name: 'Open schedule' });
    await user.click(trigger);

    expect(await screen.findByRole('dialog', { name: 'New Schedule' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Close schedule drawer' })).toHaveFocus();
    await user.keyboard('{Escape}');

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'New Schedule' })).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
  });

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
	  metadata: { name: 'fixture-policy' },
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
      return HttpResponse.json({
        totalAffected: 2,
        affectedOn: [],
        affectedOff: [{ namespace: 'team-a', name: 'api', kind: 'Deployment' }],
        blocked: [{ ref: { namespace: 'team-a', name: 'db', kind: 'StatefulSet' }, reasons: ['protected'] }],
      });
    }));
    renderUI(<ScheduleDrawer open onClose={() => undefined} prefill={{ namespaces: ['fixture-a'] }} />);

    await userEvent.click(screen.getByRole('button', { name: 'Preview Impact' }));

    expect(await screen.findByText('This schedule will affect 2 target(s)')).toBeVisible();
    expect(screen.getByText(/1 will be powered off/)).toBeVisible();
    expect(screen.getByText(/1 blocked by guardrails/)).toBeVisible();
    expect(received).toHaveBeenCalledWith(expect.objectContaining({ scope: { namespaces: ['fixture-a'] } }));
  });

  it('submits exact workload references without a namespace/name cross-product', async () => {
    const api = target('api', 'on', 'fixture-a');
    api.spec.targetRef.uid = 'api-a';
    const worker = target('worker', 'on', 'fixture-b');
    worker.spec.targetRef.kind = 'StatefulSet';
    worker.spec.targetRef.uid = 'worker-b';
    const received = vi.fn();
    server.use(
      http.get(`${origin}/targets`, () => HttpResponse.json({ targets: [api, worker], count: 2 })),
      http.post(`${origin}/policies`, async ({ request }) => {
        received(await request.json());
        return HttpResponse.json({ created: true }, { status: 201 });
      }),
    );
    renderUI(<ScheduleDrawer open onClose={() => undefined} prefill={{ targetRefs: [api.spec.targetRef, worker.spec.targetRef] }} />);

    const user = userEvent.setup();
    await user.type(screen.getByRole('textbox', { name: /^Name/ }), 'exact-policy');
    await user.click(screen.getByRole('button', { name: 'Create Schedule' }));

    await waitFor(() => expect(received).toHaveBeenCalledOnce());
    const payload = received.mock.calls[0][0] as { spec: { scope: Record<string, unknown> } };
    expect(payload.spec.scope).toEqual({ targetRefs: [api.spec.targetRef, worker.spec.targetRef] });
    expect(payload.spec.scope).not.toHaveProperty('namespaces');
    expect(payload.spec.scope).not.toHaveProperty('workloadNames');
  });

  it('submits a member change to the durable approval workflow', async () => {
    const received = vi.fn();
    server.use(http.post(`${origin}/pending`, async ({ request }) => { received(await request.json()); return HttpResponse.json({ id: 'pending-1' }, { status: 201 }); }));
    renderUI(<CurrentUserContext.Provider value={{ id: 'member-1', username: 'member', role: 'member' }}><ScheduleDrawer open onClose={() => undefined} prefill={{ namespaces: ['fixture-a'] }} /></CurrentUserContext.Provider>);
    const user = userEvent.setup();
    await user.type(screen.getByRole('textbox', { name: /^Name/ }), 'member-policy');
    await user.click(screen.getByRole('button', { name: 'Create Schedule' }));
    await waitFor(() => expect(received).toHaveBeenCalledOnce());
    expect(received).toHaveBeenCalledWith(expect.objectContaining({ action: 'create', resourceKind: 'PowerPolicy', resourceName: 'member-policy', payload: expect.objectContaining({ spec: expect.any(Object) }) }));
  });
});
