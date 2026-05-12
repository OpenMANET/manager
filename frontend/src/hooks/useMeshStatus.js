// =============================================================================
// useMeshStatus — shared mesh-status polling across pages
// =============================================================================
//
// Dashboard, Comms, and (indirectly via TopologyPage) the topology view all
// read MeshNeighborService + InterfaceService + StatusService + NodeService
// data. Previously each page independently fired `fetchMeshStatus()` on its
// own timer. This hook collapses all of them onto a single shared store so
// simultaneously-mounted pages share one poll cycle's worth of RPCs.

import { useEffect, useState } from 'react';
import { fetchMeshStatus } from '../services/meshApi.js';
import { createPollStore } from '../services/pollStore.js';

const store = createPollStore(fetchMeshStatus);

export function useMeshStatus(intervalMs) {
  const [data, setData] = useState(() => store.getSnapshot());

  useEffect(() => {
    return store.subscribe(intervalMs, setData);
  }, [intervalMs]);

  return data;
}
