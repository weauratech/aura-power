import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { AuditLog } from '../../src/pages/AuditLog';
import { PendingApprovals } from '../../src/pages/PendingApprovals';
import { Targets } from '../../src/pages/Targets';
import { Schedule } from '../../src/pages/Schedule';
import { Notifications as NotificationsPage } from '../../src/pages/Notifications';
import { Overrides } from '../../src/pages/Overrides';
import { Blocked } from '../../src/pages/Blocked';
import { Layout } from '../../src/components/Layout';
import { origin, server } from '../server';
import { renderUI, target } from '../helpers';

describe('operational pages', () => {
  it('gives global icon actions stable accessible names', () => {
    renderUI(<Layout user={{ username: 'alice', role: 'admin' }} onLogout={() => undefined} />);
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Switch to dark theme' })).toBeVisible();
  });

  it('exposes expandable block reasons with state and ownership', async () => {
    const blocked = target('database');
    blocked.status.blocked = true;
    blocked.status.blockReasons = [{ type: 'protected-namespace', message: 'Protected by policy', waivable: false }];
    server.use(http.get(`${origin}/targets`, () => HttpResponse.json({ targets: [blocked], count: 1 })));
    renderUI(<Blocked />);

    const expand = await screen.findByRole('button', { name: 'Expand block reasons for fixture-a/Deployment/database' });
    expect(expand).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(expand);
    expect(screen.getByRole('button', { name: 'Collapse block reasons for fixture-a/Deployment/database' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText('Protected by policy')).toBeVisible();
  });

  it('opens a schedule row with Enter and exposes contextual icon names', async () => {
    server.use(
      http.get(`${origin}/policies`, () => HttpResponse.json({
        items: [{
          metadata: { name: 'nightly', namespace: 'aura-system' },
          spec: {
            scope: { namespaces: ['fixture-a'] },
            schedule: { desiredState: 'off', windows: [{ start: '20:00', end: '08:00', timezone: 'UTC' }] },
            priority: 100,
          },
          status: { affectedTargets: 1 },
        }], count: 1,
      })),
      http.get(`${origin}/overrides`, () => HttpResponse.json({ items: [], count: 0 })),
    );
    renderUI(<Schedule />);

    const row = await screen.findByRole('row', { name: 'Edit schedule nightly' });
    expect(screen.getByRole('button', { name: 'Delete schedule nightly' })).toBeVisible();
    row.focus();
    await userEvent.keyboard('{Enter}');

    expect(await screen.findByRole('dialog', { name: 'Edit Schedule' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Close schedule drawer' })).toHaveFocus();
  });

  it('opens a notification row with Space and keeps delete separate from editing', async () => {
    const channel = {
      metadata: { name: 'operations', namespace: 'aura-system' },
      spec: { type: 'generic', url: 'https://example.invalid/hook', events: [], namespaceFilter: [], throttle: '5m', enabled: true },
    };
    server.use(http.get(`${origin}/notification-channels`, () => HttpResponse.json({ items: [channel], count: 1 })));
    const view = renderUI(<NotificationsPage />);

    const row = await screen.findByRole('row', { name: 'Edit notification channel operations' });
    row.focus();
    await userEvent.keyboard(' ');
    expect(await screen.findByRole('dialog', { name: 'Edit Channel' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Close notification channel drawer' })).toHaveFocus();
    await userEvent.click(screen.getByLabelText('Provider Type'));
    expect(screen.queryByRole('option', { name: 'Discord' })).not.toBeInTheDocument();
    await userEvent.keyboard('{Escape}');

    view.unmount();
    renderUI(<NotificationsPage />);
    await userEvent.click(await screen.findByRole('button', { name: 'Delete notification channel operations' }));
    expect(await screen.findByRole('dialog', { name: 'Delete Notification Channel' })).toBeVisible();
    expect(screen.queryByRole('dialog', { name: 'Edit Channel' })).not.toBeInTheDocument();
  });

  it('creates an override scoped only by exact workload references', async () => {
    let payload: {
      spec?: {
        scope?: { namespaces?: string[]; targetRefs?: Array<{ namespace: string; kind: string; name: string; uid?: string }> };
        reason?: string;
      };
    } | undefined;
    server.use(
      http.get(`${origin}/overrides`, () => HttpResponse.json({ items: [], count: 0 })),
      http.post(`${origin}/overrides`, async ({ request }) => {
        payload = await request.json() as typeof payload;
        return HttpResponse.json(payload, { status: 201 });
      }),
    );
    renderUI(<Overrides />);

    await userEvent.click(await screen.findByRole('button', { name: 'New Override' }));
    const dialog = await screen.findByRole('dialog', { name: 'New Override' });
    await userEvent.type(within(dialog).getByRole('textbox', { name: /Override Name/ }), 'keep-api-on');
    await userEvent.type(within(dialog).getByRole('textbox', { name: /Exact Workloads/ }), 'fixture-a/Deployment/api#uid-1');
    await userEvent.type(within(dialog).getByRole('textbox', { name: /Reason/ }), 'incident mitigation');
    const submit = within(dialog).getByRole('button', { name: 'Create Override' });
    expect(submit).toBeEnabled();
    await userEvent.click(submit);

    await waitFor(() => expect(payload).toBeDefined());
    expect(payload?.spec).toMatchObject({
      scope: { targetRefs: [{ namespace: 'fixture-a', kind: 'Deployment', name: 'api', uid: 'uid-1' }] },
      reason: 'incident mitigation',
    });
    expect(payload?.spec?.scope?.namespaces).toBeUndefined();
  });

  it('filters targets by a value the operator can see', async () => {
    server.use(http.get(`${origin}/targets`, () => HttpResponse.json({
      targets: [target('payments'), target('checkout', 'off', 'fixture-b')], count: 2,
    })));
    renderUI(<Targets />);

    expect(await screen.findByText('payments')).toBeVisible();
    await userEvent.type(screen.getByPlaceholderText(/Search by name or namespace/), 'checkout');

    const table = screen.getByRole('table');
    expect(within(table).getByText('checkout')).toBeVisible();
    expect(within(table).getByRole('button', { name: 'Create schedule for fixture-b/Deployment/checkout' })).toBeVisible();
    expect(within(table).queryByText('payments')).not.toBeInTheDocument();
    expect(screen.getByText('1 workloads (filtered)')).toBeVisible();
  });

  it('distinguishes an empty audit log from a failed request', async () => {
    server.use(http.get(`${origin}/audit`, () => HttpResponse.json({ events: [], count: 0, total: 0 })));
    const view = renderUI(<AuditLog />);
    expect(await screen.findByText('No audit events')).toBeVisible();

    view.unmount();
    server.use(http.get(`${origin}/audit`, () => HttpResponse.json({ error: 'audit store unavailable' }, { status: 503 })));
    renderUI(<AuditLog />);
    expect(await screen.findByRole('alert')).toHaveTextContent(/audit store unavailable/i);
    expect(screen.queryByText('No audit events')).not.toBeInTheDocument();
  });

  it('distinguishes an empty approvals queue', async () => {
    server.use(http.get(`${origin}/pending`, () => HttpResponse.json({ items: [], count: 0 })));
    renderUI(<PendingApprovals />);
    expect(screen.getByRole('heading', { name: 'Pending Approvals' })).toBeVisible();
    expect(await screen.findByRole('alert')).toHaveTextContent('No pending approval requests.');
    expect(screen.queryByRole('button', { name: /approve|reject/i })).not.toBeInTheDocument();
  });

  it('shows approval details and removes a successfully approved request', async () => {
    let pending = true;
    server.use(
      http.get(`${origin}/pending`, () => HttpResponse.json({
        items: pending ? [{
          id: 'change-1', userId: 'member-1', username: 'alice', action: 'update',
          resourceKind: 'PowerPolicy', resourceNamespace: 'aura-system', resourceName: 'nightly',
          resourceVersion: '42', payload: '{"spec":{"priority":10}}', status: 'pending', createdAt: new Date().toISOString(),
        }] : [], count: pending ? 1 : 0,
      })),
      http.post(`${origin}/pending/change-1/approve`, () => {
        pending = false;
        return HttpResponse.json({ id: 'change-1', status: 'approved' });
      }),
    );
    renderUI(<PendingApprovals />);
    expect(await screen.findByText('nightly')).toBeVisible();
    expect(screen.getByText(/revision 42/)).toBeVisible();
    await userEvent.click(screen.getByText('Review payload'));
    expect(screen.getByText('{"spec":{"priority":10}}')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Approve nightly' }));
    expect(await screen.findByText('No pending approval requests.')).toBeVisible();
  });
});
