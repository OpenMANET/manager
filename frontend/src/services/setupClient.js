// =============================================================================
// setupClient.js — ConnectRPC client for the first-boot setup wizard
// =============================================================================
//
// The wizard is reachable BEFORE the user has any credentials, so we cannot
// share connectClient.js's transport: that transport's session-expired
// interceptor would treat any 401 as a session-expiry event and redirect
// users to /login. The setup wizard endpoints (/openmanet.setup.v1.SetupService/*)
// are registered as auth-exempt on the backend (see internal/auth/middleware.go
// isAPISkipPath), so we use a plain transport with no interceptors.
//
// This client also retains the ApplySetup streaming API surface — callers
// `for await (const ev of stream)` over the response to receive per-phase
// progress events.

import { createConnectTransport } from '@connectrpc/connect-web';
import { createPromiseClient } from '@connectrpc/connect';
import { SetupService } from '../gen/openmanet/setup/v1/setup_service_connect.js';

const setupTransport = createConnectTransport({
  baseUrl: '/rpc',
  // Intentionally no interceptors: the wizard pre-dates any session.
});

export const setupClient = createPromiseClient(SetupService, setupTransport);
