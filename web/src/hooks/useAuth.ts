import { useState, useEffect, useCallback } from 'react';

interface User {
  id: string;
  username: string;
  role: 'member' | 'approver' | 'admin';
}

interface AuthState {
  user: User | null;
  isAuthenticated: boolean;
  isLoading: boolean;
}

export function useAuth() {
  const [state, setState] = useState<AuthState>({
    user: null,
    isAuthenticated: false,
    isLoading: true,
  });

  const [authEnabled, setAuthEnabled] = useState<boolean | null>(null);

  useEffect(() => {
    checkAuth();
  }, []);

  const checkAuth = async () => {
    try {
      // Try to access /api/v1/auth/me — if we have a valid session cookie,
      // the server will return user info. If not, 401.
      const res = await fetch('/api/v1/auth/me', {
        credentials: 'same-origin',
      });

      if (res.ok) {
        const user = await res.json();
        setAuthEnabled(true);
        setState({ user, isAuthenticated: true, isLoading: false });
      } else if (res.status === 401) {
        // Not authenticated — auth is enabled but no valid session
        setAuthEnabled(true);
        setState({ user: null, isAuthenticated: false, isLoading: false });
      } else {
        // Other error — check if auth is even enabled
        const statusRes = await fetch('/api/v1/status', { credentials: 'same-origin' });
        if (statusRes.ok) {
          // Auth not enforced (status returned without auth)
          setAuthEnabled(false);
          setState({ user: null, isAuthenticated: true, isLoading: false });
        } else {
          setAuthEnabled(true);
          setState({ user: null, isAuthenticated: false, isLoading: false });
        }
      }
    } catch {
      // Backend unreachable
      setAuthEnabled(false);
      setState({ user: null, isAuthenticated: true, isLoading: false });
    }
  };

  const login = useCallback(async (username: string, password: string): Promise<string | null> => {
    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });

      if (!res.ok) {
        const data = await res.json();
        return data.error || 'Login failed';
      }

      // Cookie is set automatically by the server (HttpOnly, SameSite=Strict)
      // Now fetch user info to confirm session is active
      const meRes = await fetch('/api/v1/auth/me', { credentials: 'same-origin' });
      if (meRes.ok) {
        const user = await meRes.json();
        setState({ user, isAuthenticated: true, isLoading: false });
      }
      return null;
    } catch {
      return 'Connection failed';
    }
  }, []);

  const logout = useCallback(async () => {
    await fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'same-origin',
    });
    setState({ user: null, isAuthenticated: false, isLoading: false });
  }, []);

  return { ...state, authEnabled, login, logout };
}
