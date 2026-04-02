// =============================================================================
// authFetch.js — Fetch wrapper that handles 401 session expiry
// =============================================================================
//
// Drop-in replacement for fetch() that dispatches a 'session-expired' event
// when the server returns HTTP 401, causing AuthContext to redirect to /login.

export default async function authFetch(url, opts = {}) {
  const resp = await fetch(url, { credentials: 'include', ...opts });
  if (resp.status === 401) {
    window.dispatchEvent(new Event('session-expired'));
  }
  return resp;
}
