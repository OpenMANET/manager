// =============================================================================
// AuthContext.test.jsx — Tests for the session-based AuthProvider
// =============================================================================
//
// AuthContext is the foundation of every authenticated page; a regression
// here shows up as the entire app being stuck on the login screen, or worse,
// stale user state surviving a backend session expiry. These tests cover:
//   - existing-session check on mount (authenticated and anonymous responses)
//   - the global `session-expired` window event clearing user state
//   - login success / failure
//   - changePassword 204 / non-2xx with JSON / non-2xx without JSON
//   - authEnabled gate flipping based on /auth/check response

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, act, cleanup, waitFor } from '@testing-library/react';

import { AuthProvider } from '../../contexts/AuthContext.jsx';
import { useAuth } from '../../contexts/useAuth.js';

function Probe() {
  const auth = useAuth();
  return (
    <div>
      <span data-testid="user">{auth.user ?? ''}</span>
      <span data-testid="loading">{String(auth.loading)}</span>
      <span data-testid="auth-enabled">{String(auth.authEnabled)}</span>
      <span data-testid="is-authenticated">{String(auth.isAuthenticated)}</span>
      <button onClick={() => auth.login('root', 'pw').catch(e => {
        // surface the error message in the DOM so tests can assert on it
        const el = document.querySelector('[data-testid="login-err"]');
        if (el) el.textContent = e.message;
      })}>login</button>
      <button onClick={() => auth.logout()}>logout</button>
      <button onClick={() => auth.changePassword('a', 'b').catch(e => {
        const el = document.querySelector('[data-testid="change-err"]');
        if (el) el.textContent = e.message;
      })}>change</button>
      <span data-testid="login-err"></span>
      <span data-testid="change-err"></span>
    </div>
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function stubFetch(handler) {
  vi.stubGlobal('fetch', vi.fn(handler));
}

function jsonRes(body, init = {}) {
  return {
    ok: init.status ? init.status >= 200 && init.status < 300 : true,
    status: init.status ?? 200,
    json: () => Promise.resolve(body),
  };
}

describe('TestAuthContext_SessionCheck', () => {
  it('sets user when /auth/check returns authenticated', async () => {
    stubFetch(() => Promise.resolve(jsonRes({ authenticated: true, username: 'op1', authEnabled: true })));

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));
    expect(screen.getByTestId('user').textContent).toBe('op1');
    expect(screen.getByTestId('is-authenticated').textContent).toBe('true');
    expect(screen.getByTestId('auth-enabled').textContent).toBe('true');
  });

  it('leaves user empty when /auth/check returns unauthenticated', async () => {
    stubFetch(() => Promise.resolve(jsonRes({ authenticated: false, authEnabled: true })));

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));
    expect(screen.getByTestId('user').textContent).toBe('');
    expect(screen.getByTestId('is-authenticated').textContent).toBe('false');
  });

  it('treats network error on /auth/check as unauthenticated and stops loading', async () => {
    stubFetch(() => Promise.reject(new Error('network down')));

    render(<AuthProvider><Probe /></AuthProvider>);

    // Loading must clear even on rejection — otherwise the app spins forever
    // when the API server is unreachable.
    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));
    expect(screen.getByTestId('user').textContent).toBe('');
  });

  it('reflects authEnabled=false from server response', async () => {
    stubFetch(() => Promise.resolve(jsonRes({ authenticated: false, authEnabled: false })));

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));
    expect(screen.getByTestId('auth-enabled').textContent).toBe('false');
  });
});

describe('TestAuthContext_SessionExpired', () => {
  it('clears user when window dispatches session-expired', async () => {
    stubFetch(() => Promise.resolve(jsonRes({ authenticated: true, username: 'op1', authEnabled: true })));

    render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe('op1'));

    act(() => {
      window.dispatchEvent(new Event('session-expired'));
    });

    expect(screen.getByTestId('user').textContent).toBe('');
    expect(screen.getByTestId('is-authenticated').textContent).toBe('false');
  });

  it('removes the listener on unmount so stray events do not leak', async () => {
    stubFetch(() => Promise.resolve(jsonRes({ authenticated: false, authEnabled: true })));

    const { unmount } = render(<AuthProvider><Probe /></AuthProvider>);

    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));

    const remove = vi.spyOn(window, 'removeEventListener');
    unmount();
    expect(remove).toHaveBeenCalledWith('session-expired', expect.any(Function));
  });
});

describe('TestAuthContext_Login', () => {
  it('sets user on successful login', async () => {
    let stage = 'check';
    stubFetch((url, opts) => {
      if (url.endsWith('/auth/check')) return Promise.resolve(jsonRes({ authenticated: false, authEnabled: true }));
      if (url.endsWith('/auth/login') && opts?.method === 'POST') {
        stage = 'login';
        return Promise.resolve(jsonRes({}, { status: 200 }));
      }
      return Promise.resolve(jsonRes({}));
    });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));

    await act(async () => { screen.getByText('login').click(); });

    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe('root'));
    expect(stage).toBe('login');
  });

  it('throws and leaves user unset when login returns 401 with error body', async () => {
    stubFetch((url, opts) => {
      if (url.endsWith('/auth/check')) return Promise.resolve(jsonRes({ authenticated: false, authEnabled: true }));
      if (url.endsWith('/auth/login') && opts?.method === 'POST') {
        return Promise.resolve(jsonRes({ error: 'bad credentials' }, { status: 401 }));
      }
      return Promise.resolve(jsonRes({}));
    });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByTestId('loading').textContent).toBe('false'));

    await act(async () => { screen.getByText('login').click(); });

    await waitFor(() => expect(screen.getByTestId('login-err').textContent).toBe('bad credentials'));
    expect(screen.getByTestId('user').textContent).toBe('');
  });
});

describe('TestAuthContext_Logout', () => {
  it('clears user state after a successful logout POST', async () => {
    stubFetch((url, opts) => {
      if (url.endsWith('/auth/check')) return Promise.resolve(jsonRes({ authenticated: true, username: 'op1', authEnabled: true }));
      if (url.endsWith('/auth/logout') && opts?.method === 'POST') return Promise.resolve(jsonRes({}, { status: 204 }));
      return Promise.resolve(jsonRes({}));
    });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe('op1'));

    await act(async () => { screen.getByText('logout').click(); });

    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe(''));
  });
});

describe('TestAuthContext_ChangePassword', () => {
  it('resolves silently on 204', async () => {
    stubFetch((url, opts) => {
      if (url.endsWith('/auth/check')) return Promise.resolve(jsonRes({ authenticated: true, username: 'op1', authEnabled: true }));
      if (url.endsWith('/auth/change-password') && opts?.method === 'POST') {
        return Promise.resolve({ ok: true, status: 204, json: () => Promise.reject(new Error('204 has no body')) });
      }
      return Promise.resolve(jsonRes({}));
    });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe('op1'));

    await act(async () => { screen.getByText('change').click(); });

    // Wait a microtask; no error should be set.
    await Promise.resolve();
    expect(screen.getByTestId('change-err').textContent).toBe('');
  });

  it('throws server-provided error when JSON body has error field', async () => {
    stubFetch((url, opts) => {
      if (url.endsWith('/auth/check')) return Promise.resolve(jsonRes({ authenticated: true, username: 'op1', authEnabled: true }));
      if (url.endsWith('/auth/change-password') && opts?.method === 'POST') {
        return Promise.resolve(jsonRes({ error: 'passphrase too short' }, { status: 400 }));
      }
      return Promise.resolve(jsonRes({}));
    });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe('op1'));

    await act(async () => { screen.getByText('change').click(); });

    await waitFor(() => expect(screen.getByTestId('change-err').textContent).toBe('passphrase too short'));
  });

  it('falls back to default message when response body is non-JSON', async () => {
    stubFetch((url, opts) => {
      if (url.endsWith('/auth/check')) return Promise.resolve(jsonRes({ authenticated: true, username: 'op1', authEnabled: true }));
      if (url.endsWith('/auth/change-password') && opts?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.reject(new Error('not json')) });
      }
      return Promise.resolve(jsonRes({}));
    });

    render(<AuthProvider><Probe /></AuthProvider>);
    await waitFor(() => expect(screen.getByTestId('user').textContent).toBe('op1'));

    await act(async () => { screen.getByText('change').click(); });

    await waitFor(() => expect(screen.getByTestId('change-err').textContent).toBe('Failed to change passphrase'));
  });
});
