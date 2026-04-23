// =============================================================================
// useMeshTopology — shared mesh topology + delta polling
// =============================================================================
//
// Dashboard and TopologyPage both read `getMeshTopology` and
// `getMeshTopologyDelta(60)`. This hook coalesces them onto a single
// shared store.

import { useEffect, useState } from 'react';
import {
  fetchMeshTopology,
  fetchMeshTopologyDelta,
} from '../services/meshApi.js';
import { createPollStore } from '../services/pollStore.js';

const store = createPollStore(async () => {
  const [topology, delta] = await Promise.all([
    fetchMeshTopology(),
    fetchMeshTopologyDelta(60),
  ]);
  return { topology, delta };
});

export function useMeshTopology(intervalMs) {
  const [data, setData] = useState(() => store.getSnapshot());

  useEffect(() => {
    return store.subscribe(intervalMs, setData);
  }, [intervalMs]);

  return data;
}
