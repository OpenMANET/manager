// =============================================================================
// commsApi.test.js — Tests for fetchCommsStatus
// =============================================================================
//
// commsApi.fetchCommsStatus is the data feed that drives the comms channel
// chip in StatusBar and the codec/ptime readout on the Comms page. A bug
// here causes either crashes (when consumers expect an object and get null
// silently) or stale display state (when consumers expect specific defaults
// and the field-coercion is off). These tests pin both contracts.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createRouterTransport } from '@connectrpc/connect';
import { CommsService } from '../../gen/openmanet/comms/v1/comms_service_connect.js';

vi.mock('../../services/connectClient.js', () => ({ transport: {} }));

describe('TestFetchCommsStatus', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  async function fetchWithTransport(transport) {
    vi.doMock('../../services/connectClient.js', () => ({ transport }));
    const { fetchCommsStatus } = await import('../../services/commsApi.js');
    return fetchCommsStatus();
  }

  it('returns a fully mapped object on success', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(CommsService, {
        getCommsStatus() {
          return {
            activeTalkgroup: 7,
            availableTalkgroups: [1, 2, 7, 9],
            codec: 'OPUS 32K',
            ptimeMs: 20,
            roundTripMs: 18,
            callsign: 'NODE-A',
          };
        },
      });
    });

    const result = await fetchWithTransport(transport);

    expect(result).toEqual({
      activeChannel: 7,
      availableChannels: [1, 2, 7, 9],
      codec: 'OPUS 32K',
      ptimeMs: 20,
      rttMs: 18,
      callsign: 'NODE-A',
    });
  });

  it('coerces missing fields to documented defaults', async () => {
    // Connect-ES protobuf clients return zero values when unset, so the
    // defaults (0, '', []) are what fields-not-on-the-wire actually look
    // like. The mapping must preserve them — UI components depend on
    // strict types (e.g. activeChannel must be a number, not undefined).
    const transport = createRouterTransport(({ service }) => {
      service(CommsService, {
        getCommsStatus() {
          return {};
        },
      });
    });

    const result = await fetchWithTransport(transport);

    expect(result).toEqual({
      activeChannel: 0,
      availableChannels: [],
      codec: '',
      ptimeMs: 0,
      rttMs: 0,
      callsign: '',
    });
  });

  it('returns null when the RPC fails so the caller can show placeholders', async () => {
    const transport = createRouterTransport(({ service }) => {
      service(CommsService, {
        getCommsStatus() {
          throw new Error('comms backend down');
        },
      });
    });

    const result = await fetchWithTransport(transport);
    expect(result).toBeNull();
  });
});
