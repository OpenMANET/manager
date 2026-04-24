// =============================================================================
// Topology.jsx — Dedicated mesh topology view
// =============================================================================
// Hosts the SVG TopologyMap plus Legend, Selected-host inspector, and
// short-term topology delta panels. Consumes the mesh-wide view produced
// by buildTopologyView() from batadv-vis + originator overlay data.

import React, { useCallback, useMemo, useState } from 'react';
import { buildTopologyView } from '../components/topologyGraph.js';
import TopologyMap from '../components/TopologyMap.jsx';
import { useMeshStatus } from '../hooks/useMeshStatus.js';
import { useMeshTopology } from '../hooks/useMeshTopology.js';
import './Topology.css';

// Aligned with the backend BatctlSnapshotter / DeltaTracker 5s cadence —
// polling faster than the snapshot updates only returns duplicate data.
const POLL_INTERVAL = 5000;

function formatLatency(ms) {
  if (ms == null) return '—';
  if (ms < 1) return '<1 ms';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

function formatMetric(edge) {
  if (!edge || !edge.metric) return '—';
  // BATMAN_V: higher metric = better (throughput-derived). BATMAN_IV:
  // metric is 255/TQ (lower = better). Show the raw value; the header
  // chip already tells operators which algorithm is in play.
  return edge.metric >= 1
    ? `${edge.metric.toFixed(2)}`
    : `${edge.metric.toFixed(3)}`;
}

function roleLabelForHost(host, meshData) {
  if (host.isSelf && meshData?.status?.is_gateway) return 'SELF · GATEWAY';
  if (host.isSelf) return 'SELF';
  if (host.segmentId === 'remote') return 'REMOTE';
  return 'LOCAL';
}

export default function TopologyPage() {
  const meshData = useMeshStatus(POLL_INTERVAL);
  const meshTopology = useMeshTopology(POLL_INTERVAL);
  const topology = meshTopology?.topology ?? null;
  const delta = meshTopology?.delta ?? null;

  const [selectedId, setSelectedId] = useState(null);
  const [fitSignal, setFitSignal] = useState(0);
  const [myPathsOn, setMyPathsOn] = useState(false);

  const view = useMemo(() => buildTopologyView(topology), [topology]);
  const { self, hosts, segments, blosEdges, counts, algorithm } = view;

  // Lookup table: base hostname → IP from the node service.
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

  // Index every edge (RF + BLOS) by either endpoint so the Selected panel
  // can list every peer connection.
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

  const selected = useMemo(() => {
    if (!selectedId) return null;
    const host = hostById.get(selectedId);
    return host ? { kind: 'host', host } : null;
  }, [selectedId, hostById]);

  const handleFit = useCallback(() => setFitSignal((n) => n + 1), []);
  const handleToggleMyPaths = useCallback(() => setMyPathsOn((v) => !v), []);

  const hostCount = counts.hosts;
  const remoteSegmentCount = segments.filter((s) => s.kind === 'remote').length;
  const linkCount = counts.links;
  const blosCount = counts.blosLinks;
  const hopsMax = counts.hopsMax;
  const selfHost = self?.baseHostname?.toUpperCase() || '—';
  const meshUp = hostCount > 0;

  return (
    <>
      <div className="lat-topbar">
        <div className="node-id">
          NODE-{selfHost}
          <span className="ip">
            {hostCount} host{hostCount === 1 ? '' : 's'} · {linkCount} RF link{linkCount === 1 ? '' : 's'}
            {remoteSegmentCount > 0 ? ` · ${remoteSegmentCount} remote segment${remoteSegmentCount === 1 ? '' : 's'}` : ''}
            · {hopsMax} hops max
          </span>
        </div>
        <div className="chips">
          <span className={`lat-chip ${meshUp ? 'ok' : 'warn'}`}>
            <span className="dot" /> MESH {meshUp ? 'UP' : 'DOWN'}
          </span>
          {blosCount > 0 && (
            <span className="lat-chip"><span className="dot" /> BLOS BRIDGED</span>
          )}
          {algorithm && (
            <span className="lat-chip"><span className="dot" /> {algorithm}</span>
          )}
        </div>
      </div>
      <div className="lat-view-header">
        <div>
          <h2>◇ Topology</h2>
          <div className="crumb">
            {remoteSegmentCount > 0
              ? `LOCAL + ${remoteSegmentCount} REMOTE · ${linkCount} RF · ${blosCount} BLOS`
              : `Radial layout · ${linkCount} link${linkCount === 1 ? '' : 's'}`}
            {' · '}{(POLL_INTERVAL / 1000).toFixed(0)}s refresh
          </div>
        </div>
        <div className="lat-view-toolbar">
          <button className="lat-btn ghost" type="button">LAYOUT</button>
          <button className="lat-btn ghost" type="button">FILTER</button>
          <button
            className={`lat-btn${myPathsOn ? '' : ' ghost'}`}
            type="button"
            aria-pressed={myPathsOn}
            onClick={handleToggleMyPaths}
          >
            MY PATHS
          </button>
          <button className="lat-btn" type="button" onClick={handleFit}>FIT</button>
        </div>
      </div>
      <div className="lat-body topology-body">
        <TopologyMap
          topology={topology}
          onSelect={(node) => setSelectedId(node.id)}
          selectedId={selectedId}
          fitSignal={fitSignal}
          myPathsOverlay={myPathsOn}
        />
        <div className="topology-side">
          <div className="lat-panel">
            <div className="panel-head"><h3>Legend</h3></div>
            <div className="kv">
              <span className="k"><span className="dot-i ok" />Self</span>
              <span className="v ok">{selfHost}</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i" style={{ background: 'var(--accent)' }} />Mesh host</span>
              <span className="v accent">{hostCount} host{hostCount === 1 ? '' : 's'}</span>
            </div>
            <div className="kv">
              <span className="k"><span className="dot-i warn" />Remote mesh</span>
              <span className={`v${remoteSegmentCount > 0 ? ' warn' : ''}`}>{remoteSegmentCount}</span>
            </div>
            <div className="kv">
              <span className="k">— RF link</span>
              <span className="v">within a segment</span>
            </div>
            <div className="kv">
              <span className="k">╌ BLOS link</span>
              <span className="v">bridge between segments</span>
            </div>
            <div className="kv">
              <span className="k">MY PATHS</span>
              <span className="v">{myPathsOn ? 'ON · my tree highlighted' : 'OFF · uniform edges'}</span>
            </div>
          </div>

          <div className="lat-panel">
            <div className="panel-head">
              <h3>
                {selected
                  ? `Selected · ${selected.host.baseHostname || selected.host.id}`
                  : 'Selected'}
              </h3>
            </div>
            {!selected && (
              <div className="topo-empty-hint">Click a host to inspect</div>
            )}
            {selected && (
              <HostInspector
                host={selected.host}
                meshData={meshData}
                ipByHostname={ipByHostname}
                hostById={hostById}
                edges={edgesByHost.get(selected.host.id) || []}
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

function HostInspector({ host, meshData, ipByHostname, hostById, edges }) {
  const role = roleLabelForHost(host, meshData);
  const ip = ipByHostname.get((host.baseHostname || '').toLowerCase()) || '—';
  const segmentLabel = host.segmentId === 'remote' ? 'REMOTE MESH' : 'LOCAL';
  const onMyRoute = host.isSelf || Boolean(host.myHardIfname);

  return (
    <>
      <div className="kv">
        <span className="k">Role</span>
        <span className={`v${host.segmentId === 'remote' ? ' warn' : ' accent'}`}>{role}</span>
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
        <span className="v">{segmentLabel}</span>
      </div>
      <div className="kv">
        <span className="k">Hops</span>
        <span className="v">{host.hops < 99 ? host.hops : '—'}</span>
      </div>
      {!host.isSelf && (
        <div className="kv">
          <span className="k">My route via</span>
          <span className="v">{onMyRoute ? host.myHardIfname : '— · NOT ON MY ROUTE'}</span>
        </div>
      )}
      {host.clientCount > 0 && (
        <div className="kv">
          <span className="k">Clients</span>
          <span className="v">{host.clientCount}</span>
        </div>
      )}

      <div className="topo-section-head">Links</div>
      {edges.length === 0 && <div className="topo-empty-hint">No links</div>}
      {edges.map((edge) => {
        const peerKey = edge.hostA === host.id ? edge.hostB : edge.hostA;
        const peer = hostById.get(peerKey);
        return (
          <LinkRow
            key={edge.id}
            edge={edge}
            peerHost={peer}
          />
        );
      })}
    </>
  );
}

function LinkRow({ edge, peerHost }) {
  if (!peerHost) return null;
  const header = edge.blos
    ? `╌ ${peerHost.baseHostname || peerHost.id} · via BLOS`
    : `↔ ${peerHost.baseHostname || peerHost.id}`;
  const metric = formatMetric(edge);
  return (
    <div className={`topo-link-group${edge.blos ? ' blos' : ''}`}>
      <div className="topo-link-header">
        <span>{header}</span>
        <span className="topo-link-metric">{metric}</span>
      </div>
      {edge.onMyPath && (
        <div className="topo-link-contrib">
          <span className="iface">on my forwarding path</span>
        </div>
      )}
    </div>
  );
}
