// =============================================================================
// AuthContext.jsx — Session-based authentication state for the app
// =============================================================================
//
// Provides login/logout helpers and current user state. On mount, checks the
// existing session by calling GET /auth/check. /auth/* is proxied through the
// frontend server to the upstream ConnectRPC API, so requests are same-origin
// and the session cookie is delivered automatically.

import { useCallback, useEffect, useState } from 'react';
import { AuthContext } from './useAuth.js';

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);
  // `authEnabled` reflects the server-side auth.enable setting. When false,
  // session-dependent UI (like the passphrase panel) is hidden because the
  // backend does not register the relevant endpoints.
  const [authEnabled, setAuthEnabled] = useState(true);

  // Check existing session on mount.
  useEffect(() => {
    fetch('/auth/check')
      .then(r => r.json())
      .then(data => {
        if (data.authenticated) setUser(data.username);
        if (typeof data.authEnabled === 'boolean') setAuthEnabled(data.authEnabled);
      })
      .catch(() => { /* network error — treat as unauthenticated */ })
      .finally(() => setLoading(false));
  }, []);

  // Clear session when any API call receives a 401 (session expired/invalid).
  useEffect(() => {
    const handleExpired = () => setUser(null);
    window.addEventListener('session-expired', handleExpired);
    return () => window.removeEventListener('session-expired', handleExpired);
  }, []);

  const login = useCallback(async (username, password) => {
    const resp = await fetch('/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'Login failed');
    setUser(username);
  }, []);

  const logout = useCallback(async () => {
    await fetch('/auth/logout', { method: 'POST' });
    setUser(null);
  }, []);

  const changePassword = useCallback(async (currentPassword, newPassword) => {
    const resp = await fetch('/auth/change-password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ currentPassword, newPassword }),
    });
    if (resp.status === 204) return;
    let message = 'Failed to change passphrase';
    try {
      const data = await resp.json();
      if (data?.error) message = data.error;
    } catch { /* non-JSON body — use default message */ }
    throw new Error(message);
  }, []);

  return (
    <AuthContext.Provider value={{
      user,
      login,
      logout,
      changePassword,
      isAuthenticated: !!user,
      authEnabled,
      loading,
    }}>
      {children}
    </AuthContext.Provider>
  );
}
