// =============================================================================
// useGnssStatus — shared GNSS polling across pages
// =============================================================================
//
// Both Dashboard (chip poll) and GpsStatus (full-page poll) call
// `GNSSService.getGNSSStatus()`. This hook coalesces them onto a single
// shared store so opening both tabs does not duplicate the RPC.

import { useEffect, useState } from 'react';
import { createClient } from '@connectrpc/connect';
import { transport } from '../services/connectClient.js';
import { GNSSService } from '../gen/openmanet/gnss/v1/gnss_service_pb.js';
import { createPollStore } from '../services/pollStore.js';

const gnssClient = createClient(GNSSService, transport);

const store = createPollStore(async () => {
  try {
    return await gnssClient.getGNSSStatus({});
  } catch {
    // Return null so subscribers can render a "no fix" state rather
    // than crashing. Matches the pre-existing per-page try/catch.
    return null;
  }
});

export function useGnssStatus(intervalMs) {
  const [data, setData] = useState(() => store.getSnapshot());

  useEffect(() => {
    return store.subscribe(intervalMs, setData);
  }, [intervalMs]);

  return data;
}
