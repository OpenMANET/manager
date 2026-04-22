// =============================================================================
// Topology.jsx — Dedicated mesh topology view
// =============================================================================
// Renders the full force-directed mesh graph with a side panel of legend,
// selected-node inspector, and short-term topology delta.  Reuses the same
// data source (fetchMeshTopology) that Dashboard uses for its embedded
// topology widget so there's one authoritative view of the mesh.

import React, { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react';
import { fetchMeshTopology } from '../services/meshApi.js';
import './Topology.css';

const TopologyMap = lazy(() => import('../components/TopologyMap.jsx'));

const POLL_INTERVAL = 2000;

function TopologyMapFallback() {
  return (
    <div className="lat-panel" style={{ padding: 0 }}>
      <div
        style={{
          height: 420,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: 'var(--muted)',
          fontFamily: 'var(--font-mono)',
          fontSize: 11,
          letterSpacing: '0.14em',
          textTransform: 'uppercase',
        }}
      >
        Loading topology…
      </div>
    </div>
  );
}

export default function TopologyPage() {
  const [topology, setTopology] = useState(null);
  const pollRef = useRef(null);

  const poll = useCallback(async () => {
    try {
      const t = await fetchMeshTopology();
      setTopology(t);
    } catch {
      // non-fatal; keep previous data
    }
  }, []);

  useEffect(() => {
    poll();
    pollRef.current = setInterval(poll, POLL_INTERVAL);
    return () => clearInterval(pollRef.current);
  }, [poll]);

  // Peer count = number of mesh nodes. Clients are nested under each node
  // as `.clients[]` in the mesh topology response. Degraded uses batadv's TQ
  // metric threshold of 2.0 (≈ half the ideal path quality of 1.0).
  const nodes = topology?.nodes ?? [];
  const peerCount = nodes.length;
  const clientCount = nodes.reduce((sum, n) => sum + (n.clients?.length ?? 0), 0);
  const degraded = nodes.reduce((sum, n) => {
    const bad = (n.neighbors ?? []).some(
      (e) => typeof e.metric === 'number' && e.metric > 2.0,
    );
    return bad ? sum + 1 : sum;
  }, 0);

  return (
    <>
      <div className="lat-topbar">
        <div className="node-id">
          TOPOLOGY
          <span className="ip">{peerCount} peers · batman-adv</span>
        </div>
        <div className="chips">
          <span className="lat-chip ok"><span className="dot" /> MESH {topology ? 'UP' : '…'}</span>
          <span className="lat-chip"><span className="dot" /> AUTO-LAYOUT</span>
        </div>
      </div>
      <div className="lat-view-header">
        <div>
          <h2>◇ Topology</h2>
          <div className="crumb">Radial layout · {(POLL_INTERVAL / 1000).toFixed(0)}s refresh</div>
        </div>
        <div className="lat-view-toolbar">
          <button className="lat-btn ghost" type="button">LAYOUT</button>
          <button className="lat-btn ghost" type="button">FILTER</button>
          <button className="lat-btn" type="button">FIT</button>
        </div>
      </div>
      <div className="lat-body topology-body">
        <Suspense fallback={<TopologyMapFallback />}>
          <TopologyMap topology={topology} />
        </Suspense>
        <div className="topology-side">
          <div className="lat-panel">
            <div className="panel-head"><h3>Legend</h3></div>
            <div className="kv"><span className="k">● Self</span><span className="v ok">THIS NODE</span></div>
            <div className="kv"><span className="k">● Mesh peer</span><span className="v accent">{peerCount}</span></div>
            <div className="kv"><span className="k">● Degraded</span><span className="v warn">{degraded}</span></div>
            <div className="kv"><span className="k">● Client</span><span className="v" style={{ color: 'var(--dim)' }}>{clientCount}</span></div>
            <div className="kv"><span className="k">— Edge weight</span><span className="v">= TQ</span></div>
          </div>
          <div className="lat-panel">
            <div className="panel-head"><h3>Mesh Δ · 60s</h3></div>
            {/* TODO(api-plan): expose topology-delta counters (routes_added,
                routes_lost, gateway_changes, reconverge_ms) via mesh topology
                service so this panel shows real data. Until then, render em-dashes. */}
            <div className="kv"><span className="k">Routes added</span><span className="v">—</span></div>
            <div className="kv"><span className="k">Routes lost</span><span className="v">—</span></div>
            <div className="kv"><span className="k">Gateway changes</span><span className="v">—</span></div>
            <div className="kv"><span className="k">Reconverge</span><span className="v">—</span></div>
          </div>
        </div>
      </div>
    </>
  );
}
