import { act, renderHook, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { http, HttpResponse } from 'msw';
import { useAuth } from '../../src/hooks/useAuth';
import { Login } from '../../src/pages/Login';
import { origin, server } from '../server';
import { renderUI } from '../helpers';

describe('session and login contracts', () => {
  it.each(['member', 'approver', 'admin'])('loads an existing %s cookie session', async role => {
    server.use(http.get(`${origin}/auth/me`, ({ request }) => {
      expect(request.credentials).toBe('same-origin');
      return HttpResponse.json({ id: 'fixture-user', username: 'fixture', role });
    }));
    const { result } = renderHook(() => useAuth());
    expect(result.current.isLoading).toBe(true);
    await waitFor(() => expect(result.current.user?.role).toBe(role));
    expect(result.current.isAuthenticated).toBe(true);
    expect(result.current.authEnabled).toBe(true);
  });

  it('keeps a 401 unauthenticated and clears the loading state', async () => {
    server.use(http.get(`${origin}/auth/me`, () => HttpResponse.json({ error: 'missing authorization' }, { status: 401 })));
    const { result } = renderHook(() => useAuth());
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.isAuthenticated).toBe(false);
    expect(result.current.user).toBeNull();
  });

  it('logs out through the server and clears the local user', async () => {
    const logout = vi.fn();
    server.use(
      http.get(`${origin}/auth/me`, () => HttpResponse.json({ id: '1', username: 'fixture', role: 'admin' })),
      http.post(`${origin}/auth/logout`, () => { logout(); return HttpResponse.json({ message: 'logged out' }); }),
    );
    const { result } = renderHook(() => useAuth());
    await waitFor(() => expect(result.current.isAuthenticated).toBe(true));
    await act(() => result.current.logout());
    expect(logout).toHaveBeenCalledOnce();
    expect(result.current.user).toBeNull();
    expect(result.current.isAuthenticated).toBe(false);
  });

  it('requires both fields and submits the entered credentials once', async () => {
    const onLogin = vi.fn();
    const submitted = vi.fn();
    server.use(http.post(`${origin}/auth/login`, async ({ request }) => {
      submitted(await request.json());
      return HttpResponse.json({});
    }));
    renderUI(<Login onLogin={onLogin} />);
    const user = userEvent.setup();
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeDisabled();
    await user.type(screen.getByLabelText('Username'), 'fixture-admin');
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeDisabled();
    await user.type(screen.getByLabelText('Password'), 'fixture-only-password');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(onLogin).toHaveBeenCalledOnce());
    expect(submitted).toHaveBeenCalledTimes(1);
    expect(submitted).toHaveBeenCalledWith({ username: 'fixture-admin', password: 'fixture-only-password' });
  });

  it.each([401, 503])('shows a %s failure and preserves input for retry', async status => {
    const onLogin = vi.fn();
    server.use(http.post(`${origin}/auth/login`, () => HttpResponse.json({ error: 'Sign-in rejected' }, { status })));
    renderUI(<Login onLogin={onLogin} />);
    const user = userEvent.setup();
    await user.type(screen.getByLabelText('Username'), 'fixture');
    await user.type(screen.getByLabelText('Password'), 'fixture-only-password');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Sign-in rejected');
    expect(screen.getByLabelText('Username')).toHaveValue('fixture');
    expect(onLogin).not.toHaveBeenCalled();
  });
});
