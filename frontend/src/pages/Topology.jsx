// =============================================================================
// Topology.jsx — Dedicated mesh topology view
// =============================================================================
// Hosts the SVG TopologyMap plus Legend, Selected-node inspector, and
// short-term topology delta panels. Shares fetchMeshTopology + mesh status
// data with the Dashboard's embedded topology widget.

import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { fetchMeshStatus, fetchMeshTopology, fetchMeshTopologyDelta } from '../services/meshApi.js';
import { buildTopologyView } from '../components/topologyGraph.js';
import TopologyMap from '../components/TopologyMap.jsx';
import './Topology.css';

const POLL_INTERVAL = 2000;

function formatLatency(ms) {
  if (ms == null) return '—';
  if (ms < 1) return '<1 ms';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export default function TopologyPage() {
  const [topology, setTopology] = useState(null);
  const [meshData, setMeshData] = useState(null);
  const [delta, setDelta] = useState(null);
  const [selectedId, setSelectedId] = useState(null);
  const [fitSignal, setFitSignal] = useState(0);
  const pollRef = useRef(null);

  const poll = useCallback(async () => {
    try {
      const [topo, mesh, d] = await Promise.all([
        fetchMeshTopology(),
        fetchMeshStatus(),
        fetchMeshTopologyDelta(60),
      ]);
      setTopology(topo);
      setMeshData(mesh);
      setDelta(d);
    } catch {
      // non-fatal; keep previous data
    }
  }, []);

  useEffect(() => {
    poll();
    pollRef.current = setInterval(poll, POLL_INTERVAL);
    return () => clearInterval(pollRef.current);
  }, [poll]);

  const view = useMemo(() => buildTopologyView(topology), [topology]);
  const { self, peers, edges, counts } = view;

  // Lookup table: hostname → IP (from the mesh status node list).
  const ipByHostname = useMemo(() => {
    const map = new Map();
    for (const n of meshData?.nodes || []) {
      if (n.hostname) map.set(n.hostname.toLowerCase(), n.ipaddr || n.ip || '');
    }
    return map;
  }, [meshData]);

  // Resolve the currently-selected node from its id.
  const selected = useMemo(() => {
    if (!selectedId) return null;
    if (self && self.id === selectedId) return { node: self, kind: 'self' };
    for (const p of peers) {
      if (p.id === selectedId) return { node: p, kind: p.degraded ? 'weak' : 'peer' };
      for (const c of p.clients) {
        if (c.id === selectedId) return { node: c, kind: 'client', parent: p };
      }
    }
    if (self) {
      for (const c of self.clients) {
        if (c.id === selectedId) return { node: c, kind: 'client', parent: self };
      }
    }
    return null;
  }, [selectedId, self, peers]);

  // Strongest edge touching the selected node — used for TQ/RSSI readouts.
  const selectedEdge = useMemo(() => {
    if (!selected || selected.kind === 'client') return null;
    let best = null;
    for (const e of edges) {
      if (e.src !== selected.node.id && e.dst !== selected.node.id) continue;
      if (!best) { best = e; continue; }
      // Prefer the edge with actual signal data; otherwise the better metric.
      const bestHasSig = best.signal !== 0;
      const curHasSig = e.signal !== 0;
      if (curHasSig && !bestHasSig) { best = e; continue; }
      if (curHasSig === bestHasSig && e.metric && e.metric < (best.metric || Infinity)) best = e;
    }
    return best;
  }, [selected, edges]);

  const selfTag = self?.tag || '—';
  const hostTag = self?.hostname ? self.hostname.toUpperCase() : selfTag;
  const peerCount = counts.peers;
  const hopsMax = counts.hopsMax;
  const degradedCount = counts.degraded;
  const clientCount = counts.clients;

  const handleFit = useCallback(() => {
    setFitSignal((n) => n + 1);
  }, []);

  return (
    <>
      <div className="lat-topbar">
        <div className="node-id">
          NODE-{hostTag}
          <span className="ip">
            {peerCount} peers · {hopsMax} hops max · batman-adv
          </span>
        </div>
        <div className="chips">
          <span className={`lat-chip ${peerCount > 0 ? 'ok' : 'warn'}`}>
            <span className="dot" /> MESH {peerCount > 0 ? 'UP' : 'DOWN'}
          </span>
          <span className="lat-chip"><span className="dot" /> AUTO-LAYOUT</span>
        </div>
      </div>
      <div className="lat-view-header">
        <div>
          <h2>◇ Topology</h2>
          <div className="crumb">Radial layout · {(POLL_INTERVAL / 1000).toFixed(0)}s refresh</div>
        </div>
        <div className="lat-view-toolbar">
          {/* TODO: wire layout/filter actions */}
          <button className="lat-btn ghost" type="button">LAYOUT</button>
          <button className="lat-btn ghost" type="button">FILTER</button>
          <button className="lat-btn" type="button" onClick={handleFit}>FIT</button>
        </div>
      </div>
      <div className="lat-body topology-body">
        <TopologyMap
          topology={topology}
          onSelect={(node) => setSelectedId(node.id)}
          selectedId={selectedId}
          fitSignal={fitSignal}
        />
        <div className="topology-side">
          <div className="lat-panel">
            <div className="panel-head"><h3>Legend</h3></div>
            <div className="kv">
              <span className="k"><span className="dot-i ok" />Self</span>
              <span className="v ok">{selfTag}</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i" style={{ background: 'var(--accent)' }} />Mesh peer</span>
              <span className="v accent">{peerCount} nodes</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i warn" />Degraded</span>
              <span className={`v${degradedCount > 0 ? ' warn' : ''}`}>{degradedCount} nodes</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i" style={{ background: 'var(--dim)' }} />Client</span>
              <span className="v" style={{ color: 'var(--dim)' }}>{clientCount} devices</span>
            </div>
            <div className="kv">
              <span className="k">— Line weight</span>
              <span className="v">= TQ</span>
            </div>
          </div>

          <div className="lat-panel">
            <div className="panel-head">
              <h3>{selected ? `Selected · ${selected.node.tag}` : 'Selected'}</h3>
            </div>
            {!selected && (
              <div className="topo-empty-hint">Click a node to inspect</div>
            )}
            {selected && (
              <>
                <div className="kv">
                  <span className="k">Role</span>
                  <span className="v accent">
                    {selected.kind === 'self' && meshData?.status?.is_gateway ? 'GATEWAY'
                      : selected.kind === 'self' ? 'SELF'
                      : selected.kind === 'client' ? 'CLIENT'
                      : selected.node.degraded ? 'DEGRADED' : 'PEER'}
                  </span>
                </div>
                <div className="kv">
                  <span className="k">MAC</span>
                  <span className="v">{selected.node.mac || '—'}</span>
                </div>
                <div className="kv">
                  <span className="k">IP</span>
                  <span className="v">{ipByHostname.get((selected.node.hostname || '').toLowerCase()) || '—'}</span>
                </div>
                <div className="kv">
                  <span className="k">TQ</span>
                  <span className={`v${selectedEdge && selectedEdge.metric > 2 ? ' warn' : ' ok'}`}>
                    {selectedEdge && selectedEdge.metric
                      ? selectedEdge.metric.toFixed(2)
                      : '—'}
                  </span>
                </div>
                <div className="kv">
                  <span className="k">RSSI</span>
                  <span className="v">
                    {selectedEdge && selectedEdge.signal ? `${selectedEdge.signal} dBm` : '—'}
                  </span>
                </div>
                <div className="kv">
                  <span className="k">Hops</span>
                  <span className="v">
                    {selected.kind === 'client'
                      ? (selected.parent ? selected.parent.hops + 1 : 1)
                      : selected.node.hops}
                  </span>
                </div>
                {/* TODO(api-plan): expose ping / traceroute RPCs so these can fire real probes. */}
                <div className="topo-actions">
                  <button className="lat-btn primary" type="button" disabled>PING</button>
                  <button className="lat-btn ghost" type="button" disabled>TRACE</button>
                </div>
              </>
            )}
          </div>

          <div className="lat-panel">
            <div className="panel-head"><h3>Topology Δ · 60s</h3></div>
            <div className="kv">
              <span className="k">Routes added</span>
              <span className="v">{delta ? delta.routesAdded : '—'}</span>
            </div>
            <div className="kv">
              <span className="k">Routes lost</span>
              <span className={`v${delta && delta.routesLost > 0 ? ' warn' : ''}`}>
                {delta ? delta.routesLost : '—'}
              </span>
            </div>
            <div className="kv">
              <span className="k">Gateway changes</span>
              <span className="v">{delta ? delta.gatewayChanges : '—'}</span>
            </div>
            <div className="kv">
              <span className="k">Reconverge</span>
              <span className="v">{delta ? formatLatency(delta.reconvergeMs) : '—'}</span>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
