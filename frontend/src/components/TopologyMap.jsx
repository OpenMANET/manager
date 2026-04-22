// =============================================================================
// TopologyMap.jsx — SVG mesh topology visualization with pan/zoom
// =============================================================================
// Pure-SVG renderer replacing the old reagraph/three.js implementation. Uses
// a deterministic radial layout (self at centre, peers in concentric rings by
// hop depth, clients orbiting their parent peer) and d3-zoom for pan, wheel
// zoom, and pinch-to-zoom on touch devices.
//
// Rendering contract:
//   props.topology         — raw fetchMeshTopology() result (or null)
//   props.onSelect(node)   — invoked with a peer/self/client record on click
//   props.selectedId       — currently-selected node id (highlight stroke)
//   props.compact          — smaller canvas + hide client dots for Dashboard
//   props.fitSignal        — counter; change resets zoom/pan to identity

import React, { useEffect, useMemo, useRef } from 'react';
import { zoom, zoomIdentity } from 'd3-zoom';
import { select } from 'd3-selection';
import { buildTopologyView } from './topologyGraph.js';

const RING_STEP = 120;           // px between concentric rings
const CLIENT_ORBIT_RADIUS = 32;  // px from parent peer to each of its clients
const NODE_RADIUS = 20;          // peer/self node radius
const CLIENT_RADIUS = 6;         // client dot radius
const PADDING = 60;              // viewBox padding around extreme positions

// ----------------------------------------------------------------------------
// radialLayout(view) → { positions, viewBox }
// ----------------------------------------------------------------------------
// Self at origin. Each hop level k ≥ 1 gets a ring at radius RING_STEP × k;
// peers on the ring are spaced by even angle, ordered by tag, with a small
// per-ring phase offset so ring-to-ring spokes don't align. Clients orbit
// their parent peer at a fixed radius with an angle offset by client index.
function radialLayout(view) {
  const positions = new Map();

  if (view.self) {
    positions.set(view.self.id, { x: 0, y: 0 });
  }

  // Group peers by hop depth.
  const ring = new Map();
  for (const p of view.peers) {
    const k = p.hops > 0 && p.hops < 99 ? p.hops : 1;
    if (!ring.has(k)) ring.set(k, []);
    ring.get(k).push(p);
  }

  for (const [k, peers] of ring.entries()) {
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

  // Client orbits.
  for (const peer of view.peers.concat(view.self ? [view.self] : [])) {
    const center = positions.get(peer.id);
    if (!center || peer.clients.length === 0) continue;
    peer.clients.forEach((c, i) => {
      const angle = (i / peer.clients.length) * Math.PI * 2 + Math.PI / 4;
      positions.set(c.id, {
        x: center.x + Math.cos(angle) * CLIENT_ORBIT_RADIUS,
        y: center.y + Math.sin(angle) * CLIENT_ORBIT_RADIUS,
      });
    });
  }

  // Compute viewBox from extremes.
  let minX = -NODE_RADIUS, minY = -NODE_RADIUS, maxX = NODE_RADIUS, maxY = NODE_RADIUS;
  for (const { x, y } of positions.values()) {
    if (x < minX) minX = x;
    if (y < minY) minY = y;
    if (x > maxX) maxX = x;
    if (y > maxY) maxY = y;
  }
  const w = maxX - minX + PADDING * 2;
  const h = maxY - minY + PADDING * 2;
  const viewBox = `${minX - PADDING} ${minY - PADDING} ${w} ${h}`;

  return { positions, viewBox };
}

// ----------------------------------------------------------------------------
// Node / edge presentational components
// ----------------------------------------------------------------------------
function NodeCircle({ node, kind, pos, onSelect, selectedId, compact }) {
  if (!pos) return null;
  const isSelected = selectedId === node.id;
  const classes = ['topo-node', kind];
  if (isSelected) classes.push('selected');
  const radius = kind === 'client' ? CLIENT_RADIUS : (compact ? NODE_RADIUS - 4 : NODE_RADIUS);
  return (
    <g
      className={classes.join(' ')}
      transform={`translate(${pos.x},${pos.y})`}
      onClick={(e) => {
        e.stopPropagation();
        onSelect?.(node);
      }}
    >
      {kind === 'self' && (
        <circle className="halo" r={radius + 10} />
      )}
      <circle r={radius} />
      <text>{node.tag}</text>
    </g>
  );
}

function EdgeLine({ edge, positions }) {
  const a = positions.get(edge.src);
  const b = positions.get(edge.dst);
  if (!a || !b) return null;
  return (
    <line
      className={`topo-edge${edge.weak ? ' weak' : ''}`}
      x1={a.x} y1={a.y} x2={b.x} y2={b.y}
    />
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

// ----------------------------------------------------------------------------
// Component
// ----------------------------------------------------------------------------
const TopologyMap = React.memo(function TopologyMap({
  topology,
  onSelect,
  selectedId,
  compact = false,
  fitSignal = 0,
}) {
  const view = useMemo(() => buildTopologyView(topology), [topology]);
  const { positions, viewBox } = useMemo(() => radialLayout(view), [view]);
  const svgRef = useRef(null);
  const gRef = useRef(null);
  const zoomRef = useRef(null);

  // Attach d3-zoom once. Wheel, drag and pinch all produce a transform that
  // we apply to the inner <g> so the child nodes + edges translate/scale as
  // one unit.
  useEffect(() => {
    if (!svgRef.current || !gRef.current) return;
    const svgSel = select(svgRef.current);
    const gSel = select(gRef.current);
    const z = zoom()
      .scaleExtent([0.4, 4])
      // Reject right-click and double-click; let pointerdown/wheel through so
      // drag-pan and wheel-zoom still work. Touch pinch is handled via the
      // default filter path.
      .filter((event) => !event.button && event.type !== 'dblclick')
      .on('zoom', (event) => {
        gSel.attr('transform', event.transform);
      });
    svgSel.call(z);
    // Reserve double-click for a future "focus selected node" action.
    svgSel.on('dblclick.zoom', null);
    zoomRef.current = z;
    return () => {
      svgSel.on('.zoom', null);
    };
  }, []);

  // Parent bumps fitSignal → snap back to identity. No d3-transition dep so
  // the reset is instant; the viewport jump is tiny in practice.
  useEffect(() => {
    if (!svgRef.current || !zoomRef.current) return;
    select(svgRef.current).call(zoomRef.current.transform, zoomIdentity);
  }, [fitSignal]);

  if (!view.self && view.peers.length === 0) {
    return <div className="topo-empty">No topology data</div>;
  }

  const clientEntries = [];
  const sources = view.peers.concat(view.self ? [view.self] : []);
  for (const parent of sources) {
    for (const c of parent.clients) clientEntries.push({ parent, client: c });
  }

  return (
    <div className={`topo-canvas${compact ? ' compact' : ''}`}>
      <svg ref={svgRef} viewBox={viewBox} preserveAspectRatio="xMidYMid meet">
        <g ref={gRef}>
          {view.edges.map((e) => (
            <EdgeLine key={e.id} edge={e} positions={positions} />
          ))}
          {!compact && clientEntries.map(({ parent, client }) => (
            <ClientEdge key={`ce:${client.id}`} parent={parent} client={client} positions={positions} />
          ))}
          {view.self && (
            <NodeCircle
              node={view.self}
              kind="self"
              pos={positions.get(view.self.id)}
              onSelect={onSelect}
              selectedId={selectedId}
              compact={compact}
            />
          )}
          {view.peers.map((p) => (
            <NodeCircle
              key={p.id}
              node={p}
              kind={p.degraded ? 'weak' : 'peer'}
              pos={positions.get(p.id)}
              onSelect={onSelect}
              selectedId={selectedId}
              compact={compact}
            />
          ))}
          {!compact && clientEntries.map(({ client }) => (
            <NodeCircle
              key={client.id}
              node={client}
              kind="client"
              pos={positions.get(client.id)}
              onSelect={onSelect}
              selectedId={selectedId}
              compact={compact}
            />
          ))}
        </g>
      </svg>
    </div>
  );
});

export default TopologyMap;
