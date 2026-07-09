// =============================================================================
// useNetworkInterfaces — shared kernel-interface listing across pages
// =============================================================================
//
// Both Dashboard (network panel, 30s tick) and SettingsNetwork (manual
// refresh + on-mount load) call `NetworkInterfaceService.listNetworkInterfaces`.
// Sharing the snapshot lets SettingsNetwork render instantly from cache
// when the user navigates from Dashboard, and removes the need for each
// page to maintain its own polling logic.

import { useEffect, useState } from 'react';
import { createClient } from '@connectrpc/connect';
import { transport } from '../services/connectClient.js';
import { NetworkInterfaceService } from '../gen/openmanet/network_interface/v1/network_interface_service_pb.js';
import { createPollStore } from '../services/pollStore.js';

const netClient = createClient(NetworkInterfaceService, transport);

const store = createPollStore(async () => {
  try {
    const resp = await netClient.listNetworkInterfaces({});
    return { interfaces: resp.interfaces ?? [], error: null };
  } catch (e) {
    return { interfaces: [], error: e?.message ?? 'Failed to load interfaces' };
  }
});

export function useNetworkInterfaces(intervalMs) {
  const [data, setData] = useState(() => store.getSnapshot());

  useEffect(() => {
    return store.subscribe(intervalMs, setData);
  }, [intervalMs]);

  return data;
}

// refreshNetworkInterfaces triggers an off-cycle fetch — used by the
// SettingsNetwork "Refresh" button so the user gets an immediate update
// instead of waiting for the next regular tick.
export function refreshNetworkInterfaces() {
  store.refresh();
}
