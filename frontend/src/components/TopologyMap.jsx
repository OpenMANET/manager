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
//   props.myPathsOverlay   — when true, brighten edges on the serving
//                             node's best-route tree and dim the rest

import React, { useEffect, useMemo, useRef } from 'react';
import { zoom, zoomIdentity } from 'd3-zoom';
import { select } from 'd3-selection';
import { buildTopologyView, shortHostname } from './topologyGraph.js';

const LEVEL_HEIGHT = 150;                     // vertical spacing between BFS tree depths
const LEAF_SPACING = 2 * 28 + 40;             // horizontal spacing between sibling leaves (node dia + gutter)
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
// Builds a BFS spanning tree over the segment's own RF edges, rooted at the
// anchor (self for LOCAL, gateway for each REMOTE MESH). Each node sits
// directly below its parent in the tree, with siblings spaced by a leaf
// count so subtrees don't overlap — i.e. a two-hop peer renders next to
// its actual upstream neighbor, not at an arbitrary angle.
//
// Nodes that the segment's edges don't reach (isolated peers, reporting
// gaps) attach to the root as extra children so every host still lands
// somewhere sensible.
function layoutSegment(seg, rootHost) {
  const positions = new Map();
  if (!seg.hosts.length) return { positions, bbox: { x: 0, y: 0, w: 0, h: 0 } };

  const root = rootHost && seg.hosts.some((h) => h.id === rootHost.id)
    ? rootHost
    : [...seg.hosts]
      .slice()
      .sort((a, b) => (a.hops - b.hops) || a.tag.localeCompare(b.tag))[0];

  const hostIds = new Set(seg.hosts.map((h) => h.id));
  const adj = new Map(seg.hosts.map((h) => [h.id, []]));
  for (const e of seg.edges || []) {
    if (!hostIds.has(e.hostA) || !hostIds.has(e.hostB)) continue;
    adj.get(e.hostA).push(e.hostB);
    adj.get(e.hostB).push(e.hostA);
  }
  // Sort neighbors deterministically so the layout doesn't flip between
  // refreshes just because the edge list re-orders.
  for (const [id, neighbors] of adj.entries()) {
    neighbors.sort();
    adj.set(id, neighbors);
  }

  // BFS from root along adjacency; parent-of-child wins the enqueue order.
  const children = new Map(seg.hosts.map((h) => [h.id, []]));
  const parent = new Map();
  const depth = new Map();
  const visited = new Set([root.id]);
  const queue = [root.id];
  depth.set(root.id, 0);
  while (queue.length > 0) {
    const cur = queue.shift();
    for (const nb of adj.get(cur) || []) {
      if (visited.has(nb)) continue;
      visited.add(nb);
      children.get(cur).push(nb);
      parent.set(nb, cur);
      depth.set(nb, depth.get(cur) + 1);
      queue.push(nb);
    }
  }
  // Orphans (unreachable from root via this segment's edges) hang off the
  // root so they still render — sorted by hostname for stability.
  const orphans = seg.hosts
    .filter((h) => !visited.has(h.id))
    .sort((a, b) => a.baseHostname.localeCompare(b.baseHostname))
    .map((h) => h.id);
  for (const id of orphans) {
    children.get(root.id).push(id);
    parent.set(id, root.id);
    depth.set(id, 1);
  }

  // Count leaves under each node so internal nodes can sit centered above
  // the horizontal extent they occupy.
  const leafCount = new Map();
  function countLeaves(id) {
    const kids = children.get(id) || [];
    if (kids.length === 0) {
      leafCount.set(id, 1);
      return 1;
    }
    let sum = 0;
    for (const k of kids) sum += countLeaves(k);
    leafCount.set(id, sum);
    return sum;
  }
  countLeaves(root.id);

  // Assign tree positions (root at x=0, y=0; children flow downward and
  // spread horizontally by leaf-count share).
  function place(id, dep, xCenter) {
    positions.set(id, { x: xCenter, y: dep * LEVEL_HEIGHT });
    const kids = children.get(id) || [];
    if (kids.length === 0) return;
    const totalLeaves = leafCount.get(id);
    const totalWidth = (totalLeaves - 1) * LEAF_SPACING;
    let cursor = xCenter - totalWidth / 2;
    for (const k of kids) {
      const kLeaves = leafCount.get(k);
      const kWidth = (kLeaves - 1) * LEAF_SPACING;
      place(k, dep + 1, cursor + kWidth / 2);
      cursor += kWidth + LEAF_SPACING;
    }
  }
  place(root.id, 0, 0);

  // Hybrid relaxation: run a bounded 2D force pass that pins y (depth
  // stays the BFS-assigned level) and pins the root at x=0, nudging
  // sibling x-coordinates so edge-connected peers within a depth band
  // end up adjacent. Skipped for pure-tree segments where every segment
  // edge is already in the BFS spanning tree.
  relaxSegmentPositions(seg, positions, depth, root.id);

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

// qualityClass maps a link metric to a CSS class bucket. BATMAN_V
// reports throughput-derived kbps/unit (higher is better); BATMAN_IV
// reports 255/TQ (lower is better). Unknown → q-unknown so operators
// can visually tell "we have no reading" apart from "reading is weak".
function qualityClass(metric, algorithm) {
  if (!Number.isFinite(metric) || metric <= 0) return 'q-unknown';
  if (algorithm === 'BATMAN_V') {
    if (metric >= 20) return 'q-strong';
    if (metric >= 5) return 'q-ok';
    return 'q-weak';
  }
  // BATMAN_IV and default: lower is better (metric = 255/TQ).
  if (metric <= 1.3) return 'q-strong';
  if (metric <= 1.8) return 'q-ok';
  return 'q-weak';
}

// relaxSegmentPositions runs a bounded horizontal force simulation over
// the segment's own edges. y stays fixed (BFS depth); x is nudged so
// non-tree same-depth edges pull their endpoints together. Cap
// iterations at RELAX_ITERS and skip entirely when there are no
// same-depth cross edges to untangle — the BFS layout is already
// optimal for pure trees.
function relaxSegmentPositions(seg, positions, depth, rootId) {
  const hostIds = new Set(seg.hosts.map((h) => h.id));
  // Same-depth (cross) edges are the only ones relaxation acts on.
  // Tree / parent-child edges are already satisfied by the BFS layout;
  // adding springs for them just tugs on already-correct positions.
  const crossEdges = [];
  for (const e of seg.edges || []) {
    if (!hostIds.has(e.hostA) || !hostIds.has(e.hostB)) continue;
    if (depth.get(e.hostA) === depth.get(e.hostB)) {
      crossEdges.push([e.hostA, e.hostB]);
    }
  }
  if (crossEdges.length === 0) return;

  const RELAX_ITERS = 60;
  const IDEAL_CROSS = LEAF_SPACING * 0.35; // connected siblings want to be closer
  const SPRING_K = 0.15;
  const REPULSION = LEAF_SPACING * LEAF_SPACING * 0.6;
  const MIN_SEP = NODE_RADIUS * 2.5;

  let alpha = 0.5;
  const cooling = alpha / RELAX_ITERS;

  // Bucket nodes by depth for same-depth repulsion.
  const byDepth = new Map();
  for (const id of hostIds) {
    const d = depth.get(id) ?? 0;
    if (!byDepth.has(d)) byDepth.set(d, []);
    byDepth.get(d).push(id);
  }

  for (let iter = 0; iter < RELAX_ITERS; iter++) {
    const dx = new Map();
    for (const id of hostIds) dx.set(id, 0);

    // Attractive spring on same-depth cross edges only.
    for (const [a, b] of crossEdges) {
      const pa = positions.get(a);
      const pb = positions.get(b);
      if (!pa || !pb) continue;
      const delta = pb.x - pa.x;
      const dist = Math.abs(delta) || 0.01;
      const force = (dist - IDEAL_CROSS) * SPRING_K * Math.sign(delta);
      dx.set(a, dx.get(a) + force);
      dx.set(b, dx.get(b) - force);
    }

    // Repulsion between same-depth siblings — keeps unrelated peers
    // from stacking on top of each other as the springs contract.
    for (const band of byDepth.values()) {
      for (let i = 0; i < band.length; i++) {
        for (let j = i + 1; j < band.length; j++) {
          const pa = positions.get(band[i]);
          const pb = positions.get(band[j]);
          if (!pa || !pb) continue;
          const delta = pb.x - pa.x;
          const distSq = Math.max(delta * delta, MIN_SEP * MIN_SEP);
          const sign = delta >= 0 ? 1 : -1;
          const force = (REPULSION / distSq) * sign;
          dx.set(band[i], dx.get(band[i]) - force);
          dx.set(band[j], dx.get(band[j]) + force);
        }
      }
    }

    // Apply, except for root which is pinned at x=0.
    for (const id of hostIds) {
      if (id === rootId) continue;
      const p = positions.get(id);
      if (!p) continue;
      p.x += dx.get(id) * alpha;
    }

    alpha = Math.max(0, alpha - cooling);
  }
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
// Segments are arranged in a two-row plan:
//   Row 0: the LOCAL segment (self-rooted).
//   Row 1+: REMOTE MESH segments in a wrap-grid, each rooted on its gateway.
//
// A pure-horizontal row becomes unreadable once more than two or three
// gateways are attached — wrapping to a grid keeps each remote mesh
// readable at zoom=1 while still letting operators pan/zoom to explore.
// Targeted column count is conservative so each radial cluster gets
// enough breathing room.
const MAX_COLS = 3;

function globalLayout(view, compact) {
  const segmentBoxes = [];
  const positions = new Map();

  if (compact || view.segments.length <= 1) {
    const [seg] = view.segments.length > 0
      ? view.segments
      : [{ id: '', label: '', hosts: view.hosts, edges: [], kind: 'local' }];
    const { positions: local, bbox } = layoutSegment(seg, view.self);
    for (const [id, p] of local.entries()) positions.set(id, p);
    segmentBoxes.push({ ...seg, bbox, offsetX: 0, offsetY: 0 });
  } else {
    // 1) Lay out each segment independently and expand its bbox so the
    //    header label fits. Collect them into a list we can pack.
    const laidOut = view.segments.map((seg) => {
      const rootHost = seg.kind === 'local'
        ? (view.self && seg.hosts.find((h) => h.id === view.self.id)) || null
        : seg.hosts.find((h) => h.id === seg.anchorHost) || null;
      const { positions: local, bbox: rawBBox } = layoutSegment(seg, rootHost);
      const bbox = expandBBoxToLabel(rawBBox, seg);
      return { seg, local, bbox };
    });

    // 2) Pack into rows: LOCAL gets its own row on top; remote segments
    //    wrap at MAX_COLS. Row height = tallest bbox in that row.
    const localRow = laidOut.filter((e) => e.seg.kind === 'local');
    const remoteRow = laidOut.filter((e) => e.seg.kind !== 'local');

    const rows = [];
    if (localRow.length > 0) rows.push(localRow);
    for (let i = 0; i < remoteRow.length; i += MAX_COLS) {
      rows.push(remoteRow.slice(i, i + MAX_COLS));
    }

    let cursorY = 0;
    for (const row of rows) {
      const rowHeight = Math.max(...row.map((e) => e.bbox.h));
      let cursorX = 0;
      for (const entry of row) {
        const { seg, local, bbox } = entry;
        const offsetX = cursorX - bbox.x;
        const offsetY = cursorY - bbox.y;
        for (const [id, p] of local.entries()) {
          positions.set(id, { x: p.x + offsetX, y: p.y + offsetY });
        }
        segmentBoxes.push({ ...seg, bbox, offsetX, offsetY });
        cursorX += bbox.w + SEGMENT_GUTTER;
      }
      cursorY += rowHeight + SEGMENT_GUTTER;
    }
  }

  let minX = 0, minY = 0, maxX = 0, maxY = 0;
  for (const box of segmentBoxes) {
    const x1 = box.bbox.x + box.offsetX;
    const y1 = box.bbox.y + (box.offsetY || 0);
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
  if (host.gossipStale && !host.isSelf) classes.push('stale');
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
  if (!edge.metric) return '';
  // BATMAN_V: metric is throughput-derived (higher = better).
  // BATMAN_IV: metric is 255/TQ (lower = better). Display the raw value
  // in both cases — operators reading the canvas already know the
  // algorithm from the header chip.
  if (algorithm === 'BATMAN_V') {
    return edge.metric >= 1
      ? `${edge.metric.toFixed(1)}`
      : `${edge.metric.toFixed(2)}`;
  }
  return `${edge.metric.toFixed(2)}`;
}

function EdgeLine({ edge, positions, algorithm, myPathsOverlay }) {
  const a = positions.get(edge.hostA);
  const b = positions.get(edge.hostB);
  if (!a || !b) return null;

  const classes = ['topo-edge', qualityClass(edge.metric, algorithm)];
  if (edge.blos) classes.push('blos');
  if (myPathsOverlay) {
    if (edge.onMyPath) classes.push('mypath');
    else classes.push('muted');
  }

  // Thickness grows with a better metric. Overlay mode's .mypath rule
  // adds a further bump via CSS, so we keep this base light.
  let strokeWidth = 1.1;
  if (algorithm === 'BATMAN_V') {
    if (edge.metric >= 10) strokeWidth = 2.2;
    else if (edge.metric >= 2) strokeWidth = 1.6;
  } else if (edge.metric > 0) {
    // BATMAN_IV: lower metric is better; invert.
    if (edge.metric <= 1.1) strokeWidth = 2.2;
    else if (edge.metric <= 1.4) strokeWidth = 1.6;
  }

  const label = formatEdgeLabel(edge, algorithm);
  const mx = (a.x + b.x) / 2;
  const my = (a.y + b.y) / 2;

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
        </g>
      )}
    </g>
  );
}

function SegmentBox({ box }) {
  const x = box.bbox.x + box.offsetX;
  const y = box.bbox.y + (box.offsetY || 0);
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
  myPathsOverlay = false,
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
                <EdgeLine
                  key={e.id}
                  edge={e}
                  positions={positions}
                  algorithm={view.algorithm}
                  myPathsOverlay={myPathsOverlay}
                />
              ))}
            </g>
          ))}

          {/* BLOS bridges — drawn across segment boundaries. */}
          {!compact && view.blosEdges.map((e) => (
            <EdgeLine
              key={e.id}
              edge={e}
              positions={positions}
              algorithm={view.algorithm}
              myPathsOverlay={myPathsOverlay}
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
