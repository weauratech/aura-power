import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { ChangePasswordDialog } from '../../src/components/ChangePasswordDialog';
import { Layout } from '../../src/components/Layout';
import { renderUI } from '../helpers';
import { origin, server } from '../server';

describe('password change experience', () => {
  it('shows the action to every authenticated role', async () => {
    for (const role of ['member', 'approver', 'admin']) {
      const view = renderUI(<Layout user={{ username: `fixture-${role}`, role }} />);
      expect(screen.getByRole('button', { name: 'Change password' })).toBeVisible();
      view.unmount();
    }
  });

  it('submits the exact contract and requires reauthentication after success', async () => {
    const submitted = vi.fn();
    const onChanged = vi.fn();
    server.use(http.put(`${origin}/auth/password`, async ({ request }) => {
      expect(request.credentials).toBe('same-origin');
      submitted(await request.json());
      return HttpResponse.json({ updated: true });
    }));
    renderUI(<ChangePasswordDialog open onClose={vi.fn()} onChanged={onChanged} />);
    const user = userEvent.setup();
    expect(screen.getByText(/at least 12 characters/i)).toBeVisible();
    await user.type(screen.getByLabelText(/Current password/), 'Current-password-17!');
    await user.type(screen.getByLabelText(/^New password/), 'New-passphrase-28!');
    await user.type(screen.getByLabelText(/Confirm new password/), 'New-passphrase-28!');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
    expect(submitted).toHaveBeenCalledWith({
      currentPassword: 'Current-password-17!',
      newPassword: 'New-passphrase-28!',
    });
  });

  it('rejects a confirmation mismatch without sending credentials', async () => {
    const submitted = vi.fn();
    server.use(http.put(`${origin}/auth/password`, () => { submitted(); return HttpResponse.json({ updated: true }); }));
    renderUI(<ChangePasswordDialog open onClose={vi.fn()} onChanged={vi.fn()} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/Current password/), 'Current-password-17!');
    await user.type(screen.getByLabelText(/^New password/), 'New-passphrase-28!');
    await user.type(screen.getByLabelText(/Confirm new password/), 'Different-passphrase-29!');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/do not match/i);
    expect(submitted).not.toHaveBeenCalled();
  });

  it('keeps a successful rotation successful when logout cleanup rejects', async () => {
    server.use(http.put(`${origin}/auth/password`, () => HttpResponse.json({ updated: true })));
    const onClose = vi.fn();
    const onChanged = vi.fn().mockRejectedValue(new Error('logout unavailable'));
    renderUI(<ChangePasswordDialog open onClose={onClose} onChanged={onChanged} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/Current password/), 'Current-password-17!');
    await user.type(screen.getByLabelText(/^New password/), 'New-passphrase-28!');
    await user.type(screen.getByLabelText(/Confirm new password/), 'New-passphrase-28!');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledOnce());
    expect(onClose).toHaveBeenCalledOnce();
    expect(screen.queryByText(/Password change failed/i)).not.toBeInTheDocument();
  });

  it.each([400, 401])('shows a %s response in place without redirecting or logging out', async status => {
    server.use(http.put(`${origin}/auth/password`, () => HttpResponse.json({ error: 'current password is invalid' }, { status })));
    const onChanged = vi.fn();
    renderUI(<ChangePasswordDialog open onClose={vi.fn()} onChanged={onChanged} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText(/Current password/), 'Wrong-password-17!');
    await user.type(screen.getByLabelText(/^New password/), 'New-passphrase-28!');
    await user.type(screen.getByLabelText(/Confirm new password/), 'New-passphrase-28!');
    await user.click(screen.getByRole('button', { name: 'Change password' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Current password is invalid');
    expect(window.location.pathname).toBe('/');
    expect(onChanged).not.toHaveBeenCalled();
    expect(screen.getByLabelText(/Current password/)).toHaveValue('Wrong-password-17!');
  });
});
