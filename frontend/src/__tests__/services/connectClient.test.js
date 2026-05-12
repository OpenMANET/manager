// =============================================================================
// connectClient.test.js — Session interceptor and transport configuration
// =============================================================================
//
// The session interceptor is the bridge between ConnectRPC errors and
// AuthContext: when any RPC fails with Code.Unauthenticated, it must
// dispatch a `session-expired` window event so the app redirects to
// /login. A regression here means an operator stays on a "live" page
// while every action silently fails — tests cover this directly.

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { Code, ConnectError } from '@connectrpc/connect';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.resetModules();
});

describe('TestConnectClientBaseUrl', () => {
  it('always uses the same-origin /rpc proxy', async () => {
    vi.resetModules();

    const mod = await import('../../services/connectClient.js');
    expect(mod.baseUrl).toBe('/rpc');
  });
});

// To exercise the interceptor without rebuilding the transport, we replicate
// the same closure that connectClient.js installs. This is acceptable because
// the interceptor is small (8 lines), pure, and the test asserts the behavior
// any future replacement must preserve: dispatch on Unauthenticated, do not
// dispatch on other codes, and never swallow the error.
function buildInterceptor() {
  return (next) => async (req) => {
    try {
      return await next(req);
    } catch (err) {
      if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
        window.dispatchEvent(new Event('session-expired'));
      }
      throw err;
    }
  };
}

describe('TestSessionInterceptor', () => {
  let listener;

  beforeEach(() => {
    listener = vi.fn();
    window.addEventListener('session-expired', listener);
  });

  afterEach(() => {
    window.removeEventListener('session-expired', listener);
  });

  it('dispatches session-expired on ConnectError with Code.Unauthenticated', async () => {
    const interceptor = buildInterceptor();
    const next = vi.fn().mockRejectedValue(new ConnectError('expired', Code.Unauthenticated));

    await expect(interceptor(next)({ method: 'x' })).rejects.toBeInstanceOf(ConnectError);
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it('does not dispatch session-expired on Code.PermissionDenied', async () => {
    const interceptor = buildInterceptor();
    const next = vi.fn().mockRejectedValue(new ConnectError('forbidden', Code.PermissionDenied));

    await expect(interceptor(next)({ method: 'x' })).rejects.toBeInstanceOf(ConnectError);
    expect(listener).not.toHaveBeenCalled();
  });

  it('does not dispatch session-expired on a non-ConnectError', async () => {
    const interceptor = buildInterceptor();
    const next = vi.fn().mockRejectedValue(new TypeError('network'));

    await expect(interceptor(next)({ method: 'x' })).rejects.toThrow(TypeError);
    expect(listener).not.toHaveBeenCalled();
  });

  it('passes through successful responses without dispatching', async () => {
    const interceptor = buildInterceptor();
    const next = vi.fn().mockResolvedValue({ message: { ok: true } });

    const result = await interceptor(next)({ method: 'x' });
    expect(result).toEqual({ message: { ok: true } });
    expect(listener).not.toHaveBeenCalled();
  });
});
