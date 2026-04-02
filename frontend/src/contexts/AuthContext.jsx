// =============================================================================
// AuthContext.jsx — Session-based authentication state for the app
// =============================================================================
//
// Provides login/logout helpers and current user state. On mount, checks the
// existing session by calling GET /auth/check on the API server (port 8087).
// Auth endpoints are on the API server, accessed via the /rpc Vite proxy in
// dev and directly on port 8087 in production.

import { useCallback, useEffect, useState } from 'react';
import { baseUrl } from '../services/connectClient.js';
import { AuthContext } from './useAuth.js';

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(true);

  // Check existing session on mount.
  useEffect(() => {
    fetch(`${baseUrl}/auth/check`, { credentials: 'include' })
      .then(r => r.json())
      .then(data => { if (data.authenticated) setUser(data.username); })
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
    const resp = await fetch(`${baseUrl}/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ username, password }),
    });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'Login failed');
    setUser(username);
  }, []);

  const logout = useCallback(async () => {
    await fetch(`${baseUrl}/auth/logout`, { method: 'POST', credentials: 'include' });
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, login, logout, isAuthenticated: !!user, loading }}>
      {children}
    </AuthContext.Provider>
  );
}
