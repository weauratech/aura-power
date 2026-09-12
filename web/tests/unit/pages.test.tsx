import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { AuditLog } from '../../src/pages/AuditLog';
import { PendingApprovals } from '../../src/pages/PendingApprovals';
import { Targets } from '../../src/pages/Targets';
import { origin, server } from '../server';
import { renderUI, target } from '../helpers';

describe('operational pages', () => {
  it('filters targets by a value the operator can see', async () => {
    server.use(http.get(`${origin}/targets`, () => HttpResponse.json({
      targets: [target('payments'), target('checkout', 'off', 'fixture-b')], count: 2,
    })));
    renderUI(<Targets />);

    expect(await screen.findByText('payments')).toBeVisible();
    await userEvent.type(screen.getByPlaceholderText(/Search by name or namespace/), 'checkout');

    const table = screen.getByRole('table');
    expect(within(table).getByText('checkout')).toBeVisible();
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
