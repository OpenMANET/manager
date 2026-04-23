// =============================================================================
// TopologyMap.jsx — SVG mesh topology visualization with pan/zoom
// =============================================================================
// Pure-SVG renderer driven by buildTopologyView(). Each segment (LOCAL plus
// one REMOTE per BLOS gateway) gets its own radial layout and bounding box;
// BLOS edges bridge between segments. Single-segment topologies render
// without box chrome so a plain RF mesh looks like the familiar radial view.
//
// Rendering contract:
//   props.topology         — raw fetchMeshTopology() result (or null)
//   props.onSelect(node)   — invoked with a host record on click
//   props.selectedId       — currently-selected node id (highlight stroke)
//   props.compact          — smaller canvas + drop segment chrome for
//                             Dashboard's inlined mini-map
//   props.fitSignal        — counter; change resets zoom/pan to identity

import React, { useEffect, useMemo, useRef } from 'react';
import { zoom, zoomIdentity } from 'd3-zoom';
import { select } from 'd3-selection';
import { buildTopologyView, shortHostname } from './topologyGraph.js';

const RING_STEP = 170;
const NODE_RADIUS = 28;
const PADDING = 72;
const SEGMENT_GUTTER = 160;
const SEGMENT_PAD = 56;
const BADGE_RADIUS = 4;
const BADGE_SPACING = 11;
const HOSTNAME_Y_OFFSET = NODE_RADIUS + 18;   // hostname text sits just below the circle
const BADGE_Y_OFFSET = NODE_RADIUS + 36;      // badge row below the hostname label

// Rough average glyph advance for the monospace segment label at 11px with
// 0.18em letter-spacing. Used to guarantee the segment box is at least wide
// enough to contain its header text (e.g. single-host remote segments whose
// natural bbox is narrower than "REMOTE · HOSTNAME · 1 HOST").
const SEGMENT_LABEL_CHAR_WIDTH = 8.4;
const SEGMENT_LABEL_PADDING = 28;

// ----------------------------------------------------------------------------
// layoutSegment(seg, rootHost) → { positions, bbox }
// ----------------------------------------------------------------------------
// Local radial layout for one segment. Root goes at (0,0); other hosts are
// placed on concentric rings by hop depth (normalized to the root's depth).
function layoutSegment(seg, rootHost) {
  const positions = new Map();
  if (!seg.hosts.length) return { positions, bbox: { x: 0, y: 0, w: 0, h: 0 } };

  const root = rootHost || [...seg.hosts]
    .slice()
    .sort((a, b) => (a.hops - b.hops) || a.tag.localeCompare(b.tag))[0];
  positions.set(root.id, { x: 0, y: 0 });

  const ring = new Map();
  for (const h of seg.hosts) {
    if (h.id === root.id) continue;
    const rawHops = h.hops < 99 ? h.hops : 1;
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

// segmentLabelText reproduces the header string SegmentBox renders so we can
// size the box to fit it before layout is committed.
function segmentLabelText(seg) {
  const count = seg.hosts.length;
  return `${seg.label} · ${count} HOST${count === 1 ? '' : 'S'}`;
}

// expandBBoxToLabel widens a segment's bbox symmetrically so its header label
// doesn't spill past the right edge. Root-centered positioning is preserved
// because the left edge shifts by the same delta.
function expandBBoxToLabel(bbox, seg) {
  const minW = segmentLabelText(seg).length * SEGMENT_LABEL_CHAR_WIDTH
    + SEGMENT_LABEL_PADDING;
  if (bbox.w >= minW) return bbox;
  const delta = minW - bbox.w;
  return { x: bbox.x - delta / 2, y: bbox.y, w: minW, h: bbox.h };
}

// ----------------------------------------------------------------------------
// globalLayout(view, compact) → { positions, segmentBoxes, viewBox }
// ----------------------------------------------------------------------------
function globalLayout(view, compact) {
  const segmentBoxes = [];
  const positions = new Map();

  if (compact || view.segments.length <= 1) {
    const [seg] = view.segments.length > 0
      ? view.segments
      : [{ id: '', label: '', hosts: view.hosts, edges: [], kind: 'local' }];
    const { positions: local, bbox } = layoutSegment(seg, view.self);
    for (const [id, p] of local.entries()) positions.set(id, p);
    segmentBoxes.push({ ...seg, bbox, offsetX: 0 });
  } else {
    let cursor = 0;
    for (const seg of view.segments) {
      // Local segment uses the self host as root; remote segments root on
      // their anchor (a direct BLOS neighbor) so the tunnel's entry point
      // sits centered in its box.
      const rootHost = seg.kind === 'local'
        ? (view.self && seg.hosts.find((h) => h.id === view.self.id)) || null
        : seg.hosts.find((h) => h.id === seg.anchorHost) || null;
      const { positions: local, bbox: rawBBox } = layoutSegment(seg, rootHost);
      const bbox = expandBBoxToLabel(rawBBox, seg);
      for (const [id, p] of local.entries()) {
        positions.set(id, { x: p.x + cursor - bbox.x, y: p.y });
      }
      const offsetX = cursor - bbox.x;
      segmentBoxes.push({ ...seg, bbox, offsetX });
      cursor += bbox.w + SEGMENT_GUTTER;
    }
  }

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
  // bat0 is always present on mesh nodes — hide it from the badge row so
  // the visuals stay focused on the radios that actually carry traffic.
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
      <text>{shortHostname(host.baseHostname) || host.tag}</text>
      {!compact && (
        <text className="topo-host-label" y={HOSTNAME_Y_OFFSET}>
          {host.baseHostname}
        </text>
      )}
      {!compact && <InterfaceBadges interfaces={host.interfaces} />}
    </g>
  );
}

function formatEdgeLabel(edge, algorithm) {
  if (edge.blos) return 'BLOS';
  if (algorithm === 'BATMAN_V' && edge.bestThroughput) {
    // Display kbps or Mbps depending on magnitude.
    return edge.bestThroughput >= 1000
      ? `${(edge.bestThroughput / 1000).toFixed(1)} Mbps`
      : `${Math.round(edge.bestThroughput)} kbps`;
  }
  if (edge.bestTQ) return `TQ ${edge.bestTQ}`;
  return '';
}

function AggregateEdgeLine({ edge, positions, blos, algorithm }) {
  const a = positions.get(edge.hostA);
  const b = positions.get(edge.hostB);
  if (!a || !b) return null;
  const classes = ['topo-edge'];
  if (blos) classes.push('blos');

  // Thickness: higher TQ / throughput ⇒ thicker line.
  let strokeWidth = 1;
  if (algorithm === 'BATMAN_V') {
    const tp = edge.bestThroughput || 0;
    if (tp >= 10000) strokeWidth = 2.5;
    else if (tp >= 1000) strokeWidth = 1.75;
  } else {
    const tq = edge.bestTQ || 0;
    if (tq >= 230) strokeWidth = 2.5;
    else if (tq >= 180) strokeWidth = 1.75;
  }

  const label = formatEdgeLabel(edge, algorithm);
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
            <text className="topo-edge-count" y={12}>{`(${count} paths)`}</text>
          )}
        </g>
      )}
    </g>
  );
}

function SegmentBox({ box }) {
  const x = box.bbox.x + box.offsetX;
  const y = box.bbox.y;
  return (
    <g className={`topo-segment-box ${box.kind}`}>
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
        {`${box.label} · ${box.hosts.length} HOST${box.hosts.length === 1 ? '' : 'S'}`}
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

  const showSegmentBoxes = !compact && view.segments.length > 1;

  return (
    <div className={`topo-canvas${compact ? ' compact' : ''}`}>
      <svg ref={svgRef} viewBox={viewBox} preserveAspectRatio="xMidYMid meet">
        <g ref={gRef}>
          {showSegmentBoxes && segmentBoxes.map((box) => (
            <SegmentBox key={`seg:${box.id}`} box={box} />
          ))}

          {/* RF edges, grouped by segment so each segment's edges stay
              attached to that segment's hosts. */}
          {view.segments.map((seg) => (
            <g key={`seg-edges:${seg.id}`}>
              {seg.edges.map((e) => (
                <AggregateEdgeLine
                  key={e.id}
                  edge={e}
                  positions={positions}
                  blos={false}
                  algorithm={view.algorithm}
                />
              ))}
            </g>
          ))}

          {/* BLOS bridges — drawn across segment boundaries. */}
          {!compact && view.blosEdges.map((e) => (
            <AggregateEdgeLine
              key={e.id}
              edge={e}
              positions={positions}
              blos
              algorithm={view.algorithm}
            />
          ))}

          {view.hosts.map((host) => {
            const kind = host.isSelf ? 'self' : 'peer';
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
        </g>
      </svg>
    </div>
  );
});

export default TopologyMap;
