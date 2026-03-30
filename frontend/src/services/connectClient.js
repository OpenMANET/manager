// =============================================================================
// connectClient.js — ConnectRPC transport for direct browser-to-backend calls
// =============================================================================
//
// Creates a Connect transport targeting the openmanetd ConnectRPC API server
// (port 8087). In development, Vite proxies /rpc/ to avoid CORS issues.

import { createConnectTransport } from "@connectrpc/connect-web";

const baseUrl = import.meta.env.DEV
  ? "/rpc"
  : `${location.protocol}//${location.hostname}:8087`;

export const transport = createConnectTransport({ baseUrl });
