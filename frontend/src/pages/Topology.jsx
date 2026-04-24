// =============================================================================
// Topology.jsx — Dedicated mesh topology view
// =============================================================================
// Hosts the SVG TopologyMap plus Legend, Selected-host inspector, and
// short-term topology delta panels. Consumes the mesh-wide view produced
// by buildTopologyView() from batadv-vis + originator overlay data.

import React, { useCallback, useMemo, useState } from 'react';
import { buildTopologyView, formatAge } from '../components/topologyGraph.js';
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

function formatMetric(edge, algorithm) {
  if (!edge || !edge.metric) return '—';
  // BATMAN_V metrics are throughput-derived Mbps. BATMAN_IV metrics
  // are 255/TQ (unitless ratio, lower is better) — show "TQ N.NN" so
  // operators reading the panel don't confuse the two scales.
  if (algorithm === 'BATMAN_V') {
    return `${edge.metric.toFixed(2)} Mbps`;
  }
  return `TQ ${edge.metric.toFixed(2)}`;
}

// isRemoteSegmentId returns true when a host record's segmentId marks
// it as living in any REMOTE MESH segment. topologyGraph builds
// per-gateway remote segments keyed "remote:<mac>", never bare
// "remote", so a simple equality check misses every remote peer —
// that's the inspector's "Role: LOCAL" bug for remote-mesh nodes.
function isRemoteSegmentId(segmentId) {
  return typeof segmentId === 'string' && segmentId.startsWith('remote');
}

// isGatewayHost marks the anchor of a remote segment — the node that
// terminates the vxlan0 tunnel from the serving node's local mesh.
// Derived here (rather than passed down from the renderer) because the
// inspector isn't plumbed into the segment list; we use the same
// remoteGatewayMac field topologyGraph copies off the wire.
function isGatewayHost(host) {
  if (!host || !isRemoteSegmentId(host.segmentId)) return false;
  const gwMac = (host.remoteGatewayMac || '').toLowerCase();
  return gwMac !== '' && gwMac === (host.primaryMac || '').toLowerCase();
}

function roleLabelForHost(host, meshData) {
  if (host.isSelf && meshData?.status?.is_gateway) return 'SELF · GATEWAY';
  if (host.isSelf) return 'SELF';
  if (isGatewayHost(host)) return 'GATEWAY';
  return 'MESH NODE';
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
  const { self, hosts, segments, blosEdges, counts, algorithm, gossipCoverage } = view;

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
          <GossipCoverageBadge coverage={gossipCoverage} />
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
              <span className="k"><span className="swatch ok" /> q-strong</span>
              <span className="v muted">high throughput</span>
            </div>
            <div className="kv">
              <span className="k"><span className="swatch accent" /> q-ok</span>
              <span className="v muted">median</span>
            </div>
            <div className="kv">
              <span className="k"><span className="swatch warn" /> q-weak</span>
              <span className="v muted">low</span>
            </div>
            <div className="kv">
              <span className="k"><span className="swatch dim dashed" /> q-unknown</span>
              <span className="v muted">no metric yet</span>
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
                algorithm={algorithm}
              />
            )}
          </div>

          <div className="lat-panel">
            <div className="panel-head"><h3>Counts</h3></div>
            <div className="kv">
              <span className="k">Hosts</span>
              <span className="v">{hostCount}</span>
            </div>
            <div className="kv">
              <span className="k">Segments</span>
              <span className="v">{segments.length}</span>
            </div>
            <div className="kv">
              <span className="k">RF links</span>
              <span className="v">{linkCount}</span>
            </div>
            <div className="kv">
              <span className="k">BLOS links</span>
              <span className={`v${blosCount > 0 ? ' warn' : ''}`}>{blosCount}</span>
            </div>
            <div className="kv">
              <span className="k">Max hops</span>
              <span className="v">{hopsMax}</span>
            </div>
            {gossipCoverage && gossipCoverage.total > 0 && (
              <div className="kv">
                <span className="k">Gossip cov.</span>
                <span className={`v${gossipCoverage.published * 2 < gossipCoverage.total ? ' warn' : ''}`}>
                  {`${gossipCoverage.published} / ${gossipCoverage.total}`}
                </span>
              </div>
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

// GossipCoverageBadge renders "GOSSIP N/M" in the header so operators
// see at a glance how many mesh members are actively publishing the
// neighbor-list datatype. Muted when coverage is complete (100%), red
// when a majority is missing — it only draws attention when something
// is wrong. Renders nothing when the backend didn't populate the field.
function GossipCoverageBadge({ coverage }) {
  if (!coverage || coverage.total <= 0) return null;
  const { published, total } = coverage;
  const isLow = published * 2 < total;
  const classes = ['topo-coverage'];
  if (isLow) classes.push('warn');
  return (
    <span className={classes.join(' ')} title={`${published} of ${total} mesh members publish neighbor gossip`}>
      {`GOSSIP ${published}/${total}`}
    </span>
  );
}

function HostInspector({ host, meshData, ipByHostname, hostById, edges, algorithm }) {
  const role = roleLabelForHost(host, meshData);
  const ip = ipByHostname.get((host.baseHostname || '').toLowerCase()) || '—';
  const isRemote = isRemoteSegmentId(host.segmentId);
  const segmentLabel = isRemote ? 'REMOTE' : 'LOCAL';
  const onMyRoute = host.isSelf || Boolean(host.myHardIfname);

  return (
    <>
      <div className="kv">
        <span className="k">Role</span>
        <span className={`v${isRemote ? ' warn' : ' accent'}`}>{role}</span>
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
      <div className="kv">
        <span className="k">Clients</span>
        <span className="v">{host.clientCount ?? 0}</span>
      </div>
      {!host.isSelf && <GossipFreshnessRow host={host} />}
      {!host.isSelf && (
        <div className="kv">
          <span className="k">My route via</span>
          <span className="v">{onMyRoute ? host.myHardIfname : '— · NOT ON MY ROUTE'}</span>
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
            algorithm={algorithm}
          />
        );
      })}
    </>
  );
}

// GossipFreshnessRow renders the "Gossip · fresh · 8s ago" line in the
// inspector. Signals three distinct states so operators can triage a
// quiet host — "no record" (backend never saw a publish), "stale" (age
// exceeds the snapshotter's StaleAge), and "fresh" (happy path). Self
// is suppressed upstream because self is always current.
function GossipFreshnessRow({ host }) {
  const age = host.gossipAgeSeconds;
  const hasAge = Number.isFinite(age) && age >= 0;
  let label = '— · NO RECORD';
  let cls = 'v muted';
  if (host.gossipStale && hasAge) {
    label = `stale · ${formatAge(age)}`;
    cls = 'v warn';
  } else if (host.gossipStale) {
    label = 'stale · no record';
    cls = 'v warn';
  } else if (hasAge) {
    label = `fresh · ${formatAge(age)} ago`;
    cls = 'v ok';
  }
  return (
    <div className="kv">
      <span className="k">Gossip</span>
      <span className={cls}>{label}</span>
    </div>
  );
}

function LinkRow({ edge, peerHost, algorithm }) {
  if (!peerHost) return null;
  const header = edge.blos
    ? `╌ ${peerHost.baseHostname || peerHost.id} · via BLOS`
    : `↔ ${peerHost.baseHostname || peerHost.id}`;
  const metric = formatMetric(edge, algorithm);
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
