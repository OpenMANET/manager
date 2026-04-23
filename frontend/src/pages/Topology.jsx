// =============================================================================
// Topology.jsx — Dedicated mesh topology view
// =============================================================================
// Hosts the SVG TopologyMap plus Legend, Selected-host inspector, and
// short-term topology delta panels. Consumes the host-grouped view built by
// buildTopologyView(): one node per physical host (merged across its mesh
// interfaces), edges aggregated per host-pair, with BLOS vxlan bridges
// separating RF-disjoint segments.

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

function formatSignal(dBm) {
  return dBm && dBm !== 0 ? `${dBm} dBm` : '—';
}

function roleLabelForHost(host, meshData) {
  if (host.isSelf && meshData?.status?.is_gateway) return 'SELF · GATEWAY';
  if (host.isSelf) return 'SELF';
  if (host.degraded) return 'DEGRADED';
  return 'PEER';
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
      /* non-fatal; keep previous data */
    }
  }, []);

  useEffect(() => {
    poll();
    pollRef.current = setInterval(poll, POLL_INTERVAL);
    return () => clearInterval(pollRef.current);
  }, [poll]);

  const view = useMemo(() => buildTopologyView(topology), [topology]);
  const { self, hosts, segments, blosEdges, counts } = view;

  // Lookup table: base hostname → IP (the node service publishes both
  // base-name and FQDN-style entries; case-insensitive).
  const ipByHostname = useMemo(() => {
    const map = new Map();
    for (const n of meshData?.nodes || []) {
      if (n.hostname) map.set(n.hostname.toLowerCase(), n.ipaddr || n.ip || '');
    }
    return map;
  }, [meshData]);

  const hostById = useMemo(() => {
    const m = new Map();
    for (const h of hosts) m.set(h.id, h);
    return m;
  }, [hosts]);

  // Index every aggregate edge (RF + BLOS) by either endpoint so the
  // Selected panel can list every peer connection with per-interface
  // contributors.
  const edgesByHost = useMemo(() => {
    const m = new Map();
    const add = (key, edge) => {
      if (!m.has(key)) m.set(key, []);
      m.get(key).push(edge);
    };
    for (const seg of segments) {
      for (const e of seg.edges) {
        add(e.hostA, e);
        add(e.hostB, e);
      }
    }
    for (const e of blosEdges) {
      add(e.hostA, e);
      add(e.hostB, e);
    }
    return m;
  }, [segments, blosEdges]);

  // Resolve the currently-selected node (host or client).
  const selected = useMemo(() => {
    if (!selectedId) return null;
    const host = hostById.get(selectedId);
    if (host) return { kind: 'host', host };
    for (const h of hosts) {
      for (const c of h.clients || []) {
        if (c.id === selectedId) return { kind: 'client', host: h, client: c };
      }
    }
    return null;
  }, [selectedId, hosts, hostById]);

  const handleFit = useCallback(() => setFitSignal((n) => n + 1), []);

  const hostCount = counts.hosts;
  const segmentCount = counts.segments;
  const linkCount = counts.links;
  const blosCount = counts.blosLinks;
  const hopsMax = counts.hopsMax;
  const degradedCount = counts.degraded;
  const clientCount = counts.clients;
  const selfTag = self?.tag || '—';
  const selfHost = self?.baseHostname?.toUpperCase() || selfTag;
  const meshUp = hostCount > 0;

  return (
    <>
      <div className="lat-topbar">
        <div className="node-id">
          NODE-{selfHost}
          <span className="ip">
            {hostCount} hosts · {segmentCount} segment{segmentCount === 1 ? '' : 's'}
            {blosCount > 0 ? ` · ${blosCount} BLOS` : ''} · {hopsMax} hops max
          </span>
        </div>
        <div className="chips">
          <span className={`lat-chip ${meshUp ? 'ok' : 'warn'}`}>
            <span className="dot" /> MESH {meshUp ? 'UP' : 'DOWN'}
          </span>
          {blosCount > 0 && (
            <span className="lat-chip"><span className="dot" /> BLOS BRIDGED</span>
          )}
          <span className="lat-chip"><span className="dot" /> AUTO-LAYOUT</span>
        </div>
      </div>
      <div className="lat-view-header">
        <div>
          <h2>◇ Topology</h2>
          <div className="crumb">
            {segmentCount > 1
              ? `${segmentCount} segments · ${linkCount} RF links · ${blosCount} BLOS bridge${blosCount === 1 ? '' : 's'}`
              : `Radial layout · ${linkCount} link${linkCount === 1 ? '' : 's'}`}
            {' · '}{(POLL_INTERVAL / 1000).toFixed(0)}s refresh
          </div>
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
              <span className="k"><span className="dot-i" style={{ background: 'var(--accent)' }} />Mesh host</span>
              <span className="v accent">{hostCount} hosts</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i warn" />Degraded</span>
              <span className={`v${degradedCount > 0 ? ' warn' : ''}`}>{degradedCount} hosts</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i" style={{ background: 'var(--dim)' }} />Client</span>
              <span className="v" style={{ color: 'var(--dim)' }}>{clientCount} devices</span>
            </div>
            <div className="kv">
              <span className="k">● Interface · RF</span>
              <span className="v" style={{ color: 'var(--accent)' }}>wlan0, phy-mesh…</span>
            </div>
            <div className="kv">
              <span className="k">○ Interface · BLOS</span>
              <span className="v warn">vxlan0</span>
            </div>
            <div className="kv">
              <span className="k">— RF link</span>
              <span className="v">thickness = TQ</span>
            </div>
            <div className="kv">
              <span className="k">╌ BLOS link</span>
              <span className="v">bridge between segments</span>
            </div>
          </div>

          <div className="lat-panel">
            <div className="panel-head">
              <h3>{selected ? `Selected · ${selected.kind === 'client' ? selected.client.tag : selected.host.tag}` : 'Selected'}</h3>
            </div>
            {!selected && (
              <div className="topo-empty-hint">Click a node to inspect</div>
            )}
            {selected && selected.kind === 'host' && (
              <HostInspector
                host={selected.host}
                meshData={meshData}
                ipByHostname={ipByHostname}
                hostById={hostById}
                edges={edgesByHost.get(selected.host.id) || []}
                segments={segments}
              />
            )}
            {selected && selected.kind === 'client' && (
              <ClientInspector
                client={selected.client}
                host={selected.host}
                ipByHostname={ipByHostname}
              />
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

function HostInspector({ host, meshData, ipByHostname, hostById, edges, segments }) {
  const role = roleLabelForHost(host, meshData);
  const ip = ipByHostname.get((host.baseHostname || '').toLowerCase()) || '—';
  const segmentId = host.segmentId || (segments.length === 1 ? segments[0].id : '—');

  // Split edges into RF (grouped by peer host) and BLOS.
  const rfByPeer = new Map();
  const blosByPeer = new Map();
  for (const e of edges) {
    const peerKey = e.hostA === host.id ? e.hostB : e.hostA;
    const bucket = e.blos ? blosByPeer : rfByPeer;
    if (!bucket.has(peerKey)) bucket.set(peerKey, e);
  }

  return (
    <>
      <div className="kv">
        <span className="k">Role</span>
        <span className={`v${host.degraded ? ' warn' : ' accent'}`}>{role}</span>
      </div>
      <div className="kv">
        <span className="k">Host</span>
        <span className="v">{host.baseHostname || '—'}</span>
      </div>
      <div className="kv">
        <span className="k">MAC</span>
        <span className="v">{host.primaryMac || '—'}</span>
      </div>
      <div className="kv">
        <span className="k">IP</span>
        <span className="v">{ip}</span>
      </div>
      <div className="kv">
        <span className="k">Segment</span>
        <span className="v">{segmentId}</span>
      </div>
      <div className="kv">
        <span className="k">Hops</span>
        <span className="v">{host.hops < 99 ? host.hops : '—'}</span>
      </div>

      <div className="topo-section-head">Interfaces</div>
      {host.interfaces.length === 0 && <div className="topo-empty-hint">—</div>}
      {host.interfaces.map((iface) => (
        <div key={iface.name} className="kv topo-iface-row">
          <span className="k">
            <span className={`topo-iface-dot ${iface.role}`} />
            {iface.name}
          </span>
          <span className="v">{iface.role === 'blos' ? 'BLOS' : 'RF'}</span>
        </div>
      ))}

      <div className="topo-section-head">Links</div>
      {edges.length === 0 && <div className="topo-empty-hint">No links</div>}
      {[...rfByPeer.entries()].map(([peerKey, edge]) => (
        <LinkRow
          key={`rf:${peerKey}`}
          edge={edge}
          peerHost={hostById.get(peerKey)}
          self={host}
          blos={false}
        />
      ))}
      {[...blosByPeer.entries()].map(([peerKey, edge]) => (
        <LinkRow
          key={`blos:${peerKey}`}
          edge={edge}
          peerHost={hostById.get(peerKey)}
          self={host}
          blos
        />
      ))}

      {/*
       * TODO(diag-rpc): wire PING and TRACE to a real probe RPC against the
       *   selected host once DiagnosticsService lands. Buttons stay disabled.
       */}
      <div className="topo-actions">
        <button className="lat-btn primary" type="button" disabled>PING</button>
        <button className="lat-btn ghost" type="button" disabled>TRACE</button>
      </div>
    </>
  );
}

function LinkRow({ edge, peerHost, self, blos }) {
  if (!peerHost) return null;
  const header = blos
    ? `╌ ${peerHost.tag} (${peerHost.baseHostname}) · via BLOS`
    : `↔ ${peerHost.tag} (${peerHost.baseHostname})`;
  // Contributors were recorded oriented source→destination as sampled; the
  // Selected panel orients them so the self host is always on the left.
  const contributors = edge.contributors.map((c) => {
    const forward = c.srcHost === self.id;
    return {
      localIface: forward ? c.srcIface : c.dstIface,
      peerIface: forward ? c.dstIface : c.srcIface,
      signal: c.signal,
      metric: c.metric,
    };
  });
  return (
    <div className={`topo-link-group${blos ? ' blos' : ''}`}>
      <div className="topo-link-header">{header}</div>
      {contributors.map((c, i) => {
        const body = c.signal
          ? `${formatSignal(c.signal)}`
          : c.metric
            ? `TQ ${c.metric.toFixed(2)}`
            : '—';
        return (
          <div
            key={`${c.localIface}-${c.peerIface}-${i}`}
            className="topo-link-contrib"
          >
            <span className="iface">{c.localIface || '?'} ↔ {c.peerIface || '?'}</span>
            <span className="metric">{body}</span>
          </div>
        );
      })}
    </div>
  );
}

function ClientInspector({ client, host, ipByHostname }) {
  const ip = ipByHostname.get((client.hostname || '').toLowerCase()) || '—';
  return (
    <>
      <div className="kv">
        <span className="k">Role</span>
        <span className="v" style={{ color: 'var(--dim)' }}>CLIENT</span>
      </div>
      <div className="kv">
        <span className="k">Hostname</span>
        <span className="v">{client.hostname || '—'}</span>
      </div>
      <div className="kv">
        <span className="k">MAC</span>
        <span className="v">{client.mac || '—'}</span>
      </div>
      <div className="kv">
        <span className="k">IP</span>
        <span className="v">{ip}</span>
      </div>
      <div className="kv">
        <span className="k">Attached to</span>
        <span className="v">{host.tag} · {host.baseHostname}</span>
      </div>
    </>
  );
}
