// =============================================================================
// useBLOSStatus — shared BLOS status + peer polling across pages
// =============================================================================
//
// Both Dashboard (chip showing peer count) and the BLOS page poll
// `BLOSService.getBLOSStatus()` and `listBLOSPeers()`. This hook coalesces
// them onto a single shared store so the snapshot is reused across page
// navigation — Dashboard's BLOS chip renders from cache when returning
// from /blos, and the BLOS page renders peers immediately on mount.

import { useEffect, useState } from 'react';
import { createClient } from '@connectrpc/connect';
import { transport } from '../services/connectClient.js';
import { BLOSService } from '../gen/openmanet/blos/v1/blos_service_connect.js';
import { createPollStore } from '../services/pollStore.js';

const blosClient = createClient(BLOSService, transport);

const store = createPollStore(async () => {
  // allSettled so a transient peers failure does not blank out a working
  // status (and vice-versa). Each call is wrapped in an async IIFE so a
  // synchronous throw (e.g., a missing method on a test double) becomes
  // a rejected promise instead of breaking the array literal.
  const [statusRes, peersRes] = await Promise.allSettled([
    (async () => blosClient.getBLOSStatus({}))(),
    (async () => blosClient.listBLOSPeers({}))(),
  ]);
  return {
    status: statusRes.status === 'fulfilled' ? statusRes.value : null,
    peers:
      peersRes.status === 'fulfilled' ? (peersRes.value?.peers ?? []) : [],
    error:
      statusRes.status === 'rejected'
        ? (statusRes.reason?.message ?? 'Failed to load BLOS status')
        : null,
  };
});

export function useBLOSStatus(intervalMs) {
  const [data, setData] = useState(() => store.getSnapshot());

  useEffect(() => {
    return store.subscribe(intervalMs, setData);
  }, [intervalMs]);

  return data;
}

// refreshBLOSStatus triggers an off-cycle fetch — used by the BLOS page
// after a config update so the user sees the new state without waiting
// for the next regular tick.
export function refreshBLOSStatus() {
  store.refresh();
}
