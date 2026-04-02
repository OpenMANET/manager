// =============================================================================
// connectClient.js — ConnectRPC transport for direct browser-to-backend calls
// =============================================================================
//
// Creates a Connect transport targeting the openmanetd ConnectRPC API server
// (port 8087). In development, Vite proxies /rpc/ to avoid CORS issues.

import { createConnectTransport } from "@connectrpc/connect-web";
import { Code, ConnectError } from "@connectrpc/connect";

// In production, the backend is expected to be on the same host but port 8087.
// Note: location.hostname is used instead of hardcoding "localhost" to support
// The ConnectRPC API is running with http to reduce TLS complexity, so we can't use the same port as the frontend (which is likely running on https).
export const baseUrl = import.meta.env.DEV
  ? "/rpc"
  : `http://${location.hostname}:8087`;

// Interceptor that detects expired/invalid sessions (HTTP 401 → Code.Unauthenticated)
// and notifies the AuthContext so the user is redirected to /login.
const sessionInterceptor = (next) => async (req) => {
  try {
    return await next(req);
  } catch (err) {
    if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
      window.dispatchEvent(new Event('session-expired'));
    }
    throw err;
  }
};

// credentials: "include" is required so the session cookie (set on port 8087)
// is sent with cross-port ConnectRPC requests from the frontend (port 8080/8081).
export const transport = createConnectTransport({
  baseUrl,
  credentials: "include",
  interceptors: [sessionInterceptor],
});
