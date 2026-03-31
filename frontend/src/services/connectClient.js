// =============================================================================
// connectClient.js — ConnectRPC transport for direct browser-to-backend calls
// =============================================================================
//
// Creates a Connect transport targeting the openmanetd ConnectRPC API server
// (port 8087). In development, Vite proxies /rpc/ to avoid CORS issues.

import { createConnectTransport } from "@connectrpc/connect-web";

const baseUrl = import.meta.env.DEV
  ? "/rpc"
// In production, the backend is expected to be on the same host but port 8087.
// Note: location.hostname is used instead of hardcoding "localhost" to support
// The ConnectRPC API is running with http to reduce TLS complexity, so we can't use the same port as the frontend (which is likely running on https).
  : `http://${location.hostname}:8087`;

export const transport = createConnectTransport({ baseUrl });
