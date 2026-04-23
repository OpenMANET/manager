// =============================================================================
// TopologyMap.jsx — SVG mesh topology visualization with pan/zoom
// =============================================================================
// Pure-SVG renderer driven by buildTopologyView(). Each RF-connected segment
// gets its own radial layout and bounding box; BLOS links (vxlan0) bridge
// between segments. Single-segment topologies render without box chrome for
// visual parity with the pre-segment layout.
//
// Rendering contract:
//   props.topology         — raw fetchMeshTopology() result (or null)
//   props.onSelect(node)   — invoked with a host/client record on click
//   props.selectedId       — currently-selected node id (highlight stroke)
//   props.compact          — smaller canvas + hide clients + drop segment
//                             chrome, for Dashboard's inlined mini-map
//   props.fitSignal        — counter; change resets zoom/pan to identity

import React, { useEffect, useMemo, useRef } from 'react';
import { zoom, zoomIdentity } from 'd3-zoom';
import { select } from 'd3-selection';
import { buildTopologyView } from './topologyGraph.js';

const RING_STEP = 120;
const CLIENT_ORBIT_RADIUS = 32;
const NODE_RADIUS = 20;
const CLIENT_RADIUS = 6;
const PADDING = 60;
const SEGMENT_GUTTER = 120;          // horizontal space between segment boxes
const SEGMENT_PAD = 36;              // inner padding inside each segment box
const BADGE_RADIUS = 3.2;
const BADGE_SPACING = 9;
const BADGE_Y_OFFSET = NODE_RADIUS + 11;

// ----------------------------------------------------------------------------
// layoutSegment(seg, rootHost) → { positions, bbox }
// ----------------------------------------------------------------------------
// Local radial layout for one segment. Root goes at (0,0); peers are placed
// on concentric rings by hop depth. Clients orbit their owning host. The
// returned bbox already accounts for node radii + badge row + SEGMENT_PAD.
function layoutSegment(seg, rootHost, { includeClients }) {
  const positions = new Map();
  if (!seg.hosts.length) return { positions, bbox: { x: 0, y: 0, w: 0, h: 0 } };

  // Assign a root even when no self lives in this segment (the lowest-hops
  // host wins; ties broken by tag).
  const root = rootHost || [...seg.hosts]
    .slice()
    .sort((a, b) => (a.hops - b.hops) || a.tag.localeCompare(b.tag))[0];
  positions.set(root.id, { x: 0, y: 0 });

  const ring = new Map();
  for (const h of seg.hosts) {
    if (h.id === root.id) continue;
    const rawHops = h.hops < 99 ? h.hops : 1;
    // Normalize to hops-from-root-within-this-segment so a multi-segment
    // view doesn't push rings outward for a host that was "two hops" via a
    // BLOS bridge (we're laying out RF topology here).
    const k = Math.max(1, rawHops - root.hops);
    if (!ring.has(k)) ring.set(k, []);
    ring.get(k).push(h);
  }
  for (const [k, peers] of ring.entries()) {
    peers.sort((a, b) => a.tag.localeCompare(b.tag));
    const radius = RING_STEP * k;
    const offset = (k % 2 === 0 ? Math.PI / peers.length : 0) - Math.PI / 2;
    peers.forEach((peer, i) => {
      const angle = offset + (i / peers.length) * Math.PI * 2;
      positions.set(peer.id, {
        x: Math.cos(angle) * radius,
        y: Math.sin(angle) * radius,
      });
    });
  }

  if (includeClients) {
    for (const host of seg.hosts) {
      const center = positions.get(host.id);
      if (!center || !host.clients || host.clients.length === 0) continue;
      host.clients.forEach((c, i) => {
        const angle = (i / host.clients.length) * Math.PI * 2 + Math.PI / 4;
        positions.set(c.id, {
          x: center.x + Math.cos(angle) * CLIENT_ORBIT_RADIUS,
          y: center.y + Math.sin(angle) * CLIENT_ORBIT_RADIUS,
        });
      });
    }
  }

  let minX = -NODE_RADIUS, minY = -NODE_RADIUS;
  let maxX = NODE_RADIUS, maxY = NODE_RADIUS + BADGE_Y_OFFSET;
  for (const { x, y } of positions.values()) {
    if (x - NODE_RADIUS < minX) minX = x - NODE_RADIUS;
    if (y - NODE_RADIUS < minY) minY = y - NODE_RADIUS;
    if (x + NODE_RADIUS > maxX) maxX = x + NODE_RADIUS;
    if (y + BADGE_Y_OFFSET > maxY) maxY = y + BADGE_Y_OFFSET;
  }
  const bbox = {
    x: minX - SEGMENT_PAD,
    y: minY - SEGMENT_PAD,
    w: (maxX - minX) + SEGMENT_PAD * 2,
    h: (maxY - minY) + SEGMENT_PAD * 2,
  };
  return { positions, bbox };
}

// ----------------------------------------------------------------------------
// globalLayout(view, compact)
// ----------------------------------------------------------------------------
// Places each segment side-by-side and resolves a global position for every
// node. When compact=true, all hosts are laid out in a single radial ring
// ignoring segment membership so the Dashboard mini-map stays compact.
function globalLayout(view, compact) {
  const segmentBoxes = [];
  const positions = new Map();

  if (compact || view.segments.length <= 1) {
    const [seg] = view.segments.length > 0 ? view.segments : [{ hosts: view.hosts, edges: [] }];
    const { positions: local, bbox } = layoutSegment(seg, view.self, {
      includeClients: !compact,
    });
    for (const [id, p] of local.entries()) positions.set(id, p);
    segmentBoxes.push({ id: view.segments[0]?.id || '', bbox, offsetX: 0, hosts: seg.hosts });
  } else {
    let cursor = 0;
    for (const seg of view.segments) {
      const rootHost = (view.self && seg.hosts.find((h) => h.id === view.self.id)) || null;
      const { positions: local, bbox } = layoutSegment(seg, rootHost, {
        includeClients: !compact,
      });
      for (const [id, p] of local.entries()) {
        positions.set(id, { x: p.x + cursor - bbox.x, y: p.y });
      }
      const offsetX = cursor - bbox.x;
      segmentBoxes.push({ id: seg.id, bbox, offsetX, hosts: seg.hosts });
      cursor += bbox.w + SEGMENT_GUTTER;
    }
  }

  // Compute global viewBox.
  let minX = 0, minY = 0, maxX = 0, maxY = 0;
  for (const box of segmentBoxes) {
    const x1 = box.bbox.x + box.offsetX;
    const y1 = box.bbox.y;
    const x2 = x1 + box.bbox.w;
    const y2 = y1 + box.bbox.h;
    if (x1 < minX) minX = x1;
    if (y1 < minY) minY = y1;
    if (x2 > maxX) maxX = x2;
    if (y2 > maxY) maxY = y2;
  }
  const w = maxX - minX + PADDING * 2;
  const h = maxY - minY + PADDING * 2;
  const viewBox = `${minX - PADDING} ${minY - PADDING} ${w} ${h}`;

  return { positions, segmentBoxes, viewBox };
}

// ----------------------------------------------------------------------------
// Presentational components
// ----------------------------------------------------------------------------
function InterfaceBadges({ interfaces }) {
  if (!interfaces || interfaces.length === 0) return null;
  // Drop bat0 from the visual badges — it's always present and visually
  // noisy. It still appears in the Selected panel's full interface list.
  const visibleAll = interfaces.filter((i) => i.name !== 'bat0');
  if (visibleAll.length === 0) return null;
  const MAX = 3;
  const visible = visibleAll.slice(0, MAX);
  const extra = visibleAll.length - visible.length;
  const n = visible.length + (extra > 0 ? 1 : 0);
  const startX = -((n - 1) * BADGE_SPACING) / 2;
  return (
    <g className="topo-badges" transform={`translate(0, ${BADGE_Y_OFFSET})`}>
      {visible.map((iface, i) => (
        <circle
          key={iface.name}
          className={`topo-iface-badge ${iface.role}`}
          cx={startX + i * BADGE_SPACING}
          cy={0}
          r={BADGE_RADIUS}
        >
          <title>{`${iface.name} (${iface.role === 'blos' ? 'BLOS' : 'RF'})`}</title>
        </circle>
      ))}
      {extra > 0 && (
        <text
          className="topo-iface-overflow"
          x={startX + visible.length * BADGE_SPACING}
          y={BADGE_RADIUS}
        >
          {`+${extra}`}
        </text>
      )}
    </g>
  );
}

function HostNode({ host, pos, kind, onSelect, selectedId, compact }) {
  if (!pos) return null;
  const isSelected = selectedId === host.id;
  const classes = ['topo-node', kind];
  if (isSelected) classes.push('selected');
  const radius = compact ? NODE_RADIUS - 4 : NODE_RADIUS;
  return (
    <g
      className={classes.join(' ')}
      transform={`translate(${pos.x},${pos.y})`}
      onClick={(e) => {
        e.stopPropagation();
        onSelect?.(host);
      }}
    >
      {kind === 'self' && <circle className="halo" r={radius + 10} />}
      <circle r={radius} />
      <text>{host.tag}</text>
      {!compact && <InterfaceBadges interfaces={host.interfaces} />}
    </g>
  );
}

function ClientNode({ client, pos, onSelect, selectedId }) {
  if (!pos) return null;
  const isSelected = selectedId === client.id;
  const classes = ['topo-node', 'client'];
  if (isSelected) classes.push('selected');
  return (
    <g
      className={classes.join(' ')}
      transform={`translate(${pos.x},${pos.y})`}
      onClick={(e) => {
        e.stopPropagation();
        onSelect?.(client);
      }}
    >
      <circle r={CLIENT_RADIUS} />
      <text>{client.tag}</text>
    </g>
  );
}

function formatSignal(dBm) {
  if (!dBm || dBm === 0) return '';
  return `${dBm} dBm`;
}

function AggregateEdgeLine({ edge, positions, blos }) {
  const a = positions.get(edge.hostA);
  const b = positions.get(edge.hostB);
  if (!a || !b) return null;
  const classes = ['topo-edge'];
  if (blos) classes.push('blos');
  if (edge.weak) classes.push('weak');
  // Map best TQ to a thickness band (1px cyan for OK, 2px for stronger
  // aggregated links, capped at 3). TQ closer to 1.0 = best.
  const tq = edge.bestMetric || 0;
  let strokeWidth = 1;
  if (tq > 0 && tq <= 1.2) strokeWidth = 2.5;
  else if (tq > 0 && tq <= 1.6) strokeWidth = 1.75;
  const label = blos
    ? 'BLOS'
    : formatSignal(edge.bestSignal) || (tq ? `TQ ${tq.toFixed(2)}` : '');
  const mx = (a.x + b.x) / 2;
  const my = (a.y + b.y) / 2;
  const count = edge.contributors?.length || 0;
  return (
    <g className="topo-edge-group">
      <line
        className={classes.join(' ')}
        x1={a.x} y1={a.y} x2={b.x} y2={b.y}
        strokeWidth={strokeWidth}
      />
      {label && (
        <g className="topo-edge-label" transform={`translate(${mx},${my})`}>
          <text className="topo-edge-label-text">{label}</text>
          {count > 1 && (
            <text className="topo-edge-count" y={12}>{`(${count} links)`}</text>
          )}
        </g>
      )}
    </g>
  );
}

function ClientEdge({ parent, client, positions }) {
  const a = positions.get(parent.id);
  const b = positions.get(client.id);
  if (!a || !b) return null;
  return (
    <line
      className="topo-edge client"
      x1={a.x} y1={a.y} x2={b.x} y2={b.y}
    />
  );
}

function SegmentBox({ box }) {
  const x = box.bbox.x + box.offsetX;
  const y = box.bbox.y;
  return (
    <g className="topo-segment-box">
      <rect
        x={x}
        y={y}
        width={box.bbox.w}
        height={box.bbox.h}
        rx={4} ry={4}
      />
      <text
        className="topo-segment-label"
        x={x + 10}
        y={y + 16}
      >
        {`SEGMENT ${box.id} · ${box.hosts.length} HOSTS`}
      </text>
    </g>
  );
}

// ----------------------------------------------------------------------------
// TopologyMap component
// ----------------------------------------------------------------------------
const TopologyMap = React.memo(function TopologyMap({
  topology,
  onSelect,
  selectedId,
  compact = false,
  fitSignal = 0,
}) {
  const view = useMemo(() => buildTopologyView(topology), [topology]);
  const { positions, segmentBoxes, viewBox } = useMemo(
    () => globalLayout(view, compact),
    [view, compact],
  );
  const svgRef = useRef(null);
  const gRef = useRef(null);
  const zoomRef = useRef(null);

  useEffect(() => {
    if (!svgRef.current || !gRef.current) return undefined;
    const svgSel = select(svgRef.current);
    const gSel = select(gRef.current);
    const z = zoom()
      .scaleExtent([0.4, 4])
      .filter((event) => !event.button && event.type !== 'dblclick')
      .on('zoom', (event) => {
        gSel.attr('transform', event.transform);
      });
    svgSel.call(z);
    svgSel.on('dblclick.zoom', null);
    zoomRef.current = z;
    return () => {
      svgSel.on('.zoom', null);
    };
  }, []);

  useEffect(() => {
    if (!svgRef.current || !zoomRef.current) return;
    select(svgRef.current).call(zoomRef.current.transform, zoomIdentity);
  }, [fitSignal]);

  if (view.hosts.length === 0) {
    return <div className="topo-empty">No topology data</div>;
  }

  // Flatten the client render list once so edges and nodes can be emitted
  // in SVG document order (edges below, nodes above).
  const clientEntries = [];
  if (!compact) {
    for (const host of view.hosts) {
      for (const c of host.clients || []) clientEntries.push({ parent: host, client: c });
    }
  }

  const showSegmentBoxes = !compact && view.segments.length > 1;

  return (
    <div className={`topo-canvas${compact ? ' compact' : ''}`}>
      <svg ref={svgRef} viewBox={viewBox} preserveAspectRatio="xMidYMid meet">
        <g ref={gRef}>
          {showSegmentBoxes && segmentBoxes.map((box) => (
            <SegmentBox key={`seg:${box.id}`} box={box} />
          ))}

          {/* RF edges, grouped by segment so a segment's edges stay visually
              attached to that segment's hosts. */}
          {view.segments.map((seg) => (
            <g key={`seg-edges:${seg.id}`}>
              {seg.edges.map((e) => (
                <AggregateEdgeLine key={e.id} edge={e} positions={positions} blos={false} />
              ))}
            </g>
          ))}

          {/* BLOS bridges — drawn across segment boundaries. */}
          {!compact && view.blosEdges.map((e) => (
            <AggregateEdgeLine key={e.id} edge={e} positions={positions} blos />
          ))}

          {!compact && clientEntries.map(({ parent, client }) => (
            <ClientEdge key={`ce:${client.id}`} parent={parent} client={client} positions={positions} />
          ))}

          {view.hosts.map((host) => {
            const kind = host.isSelf ? 'self' : host.degraded ? 'weak' : 'peer';
            return (
              <HostNode
                key={host.id}
                host={host}
                kind={kind}
                pos={positions.get(host.id)}
                onSelect={onSelect}
                selectedId={selectedId}
                compact={compact}
              />
            );
          })}

          {!compact && clientEntries.map(({ client }) => (
            <ClientNode
              key={client.id}
              client={client}
              pos={positions.get(client.id)}
              onSelect={onSelect}
              selectedId={selectedId}
            />
          ))}
        </g>
      </svg>
    </div>
  );
});

export default TopologyMap;
