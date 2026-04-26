// =============================================================================
// connectClient.js — ConnectRPC transport for browser-to-backend calls
// =============================================================================
//
// All ConnectRPC traffic flows through the same origin as the WebUI: the
// frontend server proxies /rpc/* to the upstream ConnectRPC API server. This
// lets the WebUI run over HTTPS without hitting mixed-content blocks when
// reaching the plain-HTTP API server, and keeps cookies same-origin so no
// `credentials: "include"` gymnastics are needed.

import { createConnectTransport } from "@connectrpc/connect-web";
import { Code, ConnectError } from "@connectrpc/connect";

export const baseUrl = "/rpc";

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

export const transport = createConnectTransport({
  baseUrl,
  interceptors: [sessionInterceptor],
});
