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

  it('labels the approvals screen honestly as a static empty state', () => {
    renderUI(<PendingApprovals />);
    expect(screen.getByRole('heading', { name: 'Pending Approvals' })).toBeVisible();
    expect(screen.getByRole('alert')).toHaveTextContent('No pending approval requests.');
    expect(screen.queryByRole('button', { name: /approve|reject/i })).not.toBeInTheDocument();
  });
});
