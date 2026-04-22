// =============================================================================
// topologyGraph.js — Pure transform from MeshTopology → reagraph {nodes,edges}
// =============================================================================
// Extracted from TopologyMap.jsx so it can be unit-tested without pulling in
// reagraph's WebGL canvas and so fast-refresh stays happy (component files
// should export components only).

const COLOR_SELF = '#6B8E23';     // olive green
const COLOR_PEER = '#3b82f6';     // blue
const COLOR_UNKNOWN = '#9ca3af';  // gray
const COLOR_CLIENT = '#6b7280';   // darker gray
const COLOR_EDGE_NO_SIGNAL = '#4b5563';

export const TOPOLOGY_COLORS = {
  self: COLOR_SELF,
  peer: COLOR_PEER,
  unknown: COLOR_UNKNOWN,
  client: COLOR_CLIENT,
  edgeNoSignal: COLOR_EDGE_NO_SIGNAL,
};

function signalColor(dBm) {
  if (dBm >= -60) return '#6B8E23'; // strong
  if (dBm >= -75) return '#b8a000'; // moderate
  return '#cc3333';                  // weak
}

// Edge width is derived from TQ metric. batadv metric ≈ 1.0 for a perfect
// link; larger values (>1) mean worse quality. Clamp to [0.5, 3].
function edgeSize(metric) {
  if (!metric || metric <= 0) return 1;
  const raw = 3 / metric;
  return Math.max(0.5, Math.min(3, raw));
}

function shortMac(mac) {
  if (!mac) return '?';
  const parts = mac.split(':');
  return parts.length === 6 ? parts.slice(3).join(':') : mac;
}

function formatMetric(metric) {
  if (!metric) return '';
  return `TQ ${metric.toFixed(2)}`;
}

// -----------------------------------------------------------------------------
// buildGraphData(topology)
// -----------------------------------------------------------------------------
// Given the shape returned by services/meshApi.js#fetchMeshTopology, produces
// { nodes, edges } consumable by reagraph's <GraphCanvas>.
//
// Self-identification heuristic: a MeshNode is "self" when at least one of
// its outgoing MeshEdges has a non-zero `signal` field. The backend only
// populates signal for edges originating on OUR radios, so a node reporting
// signal on any edge must be this device.
//
// Dedupe: batadv-vis emits each mesh link from both endpoints (A→B from A's
// entry, B→A from B's entry). We canonicalize by the unordered MAC pair and
// prefer whichever direction carries a local `signal` reading.
export function buildGraphData(topology) {
  if (!topology || !Array.isArray(topology.nodes)) {
    return { nodes: [], edges: [] };
  }

  const macToPrimary = new Map();
  const selfPrimaries = new Set();

  for (const node of topology.nodes) {
    if (!node.primaryMac) continue;
    macToPrimary.set(node.primaryMac.toLowerCase(), node.primaryMac);
    for (const s of node.secondaryMacs || []) {
      if (s) macToPrimary.set(s.toLowerCase(), node.primaryMac);
    }
    if ((node.neighbors || []).some((e) => e.signal && e.signal !== 0)) {
      selfPrimaries.add(node.primaryMac);
    }
  }

  const nodes = [];
  const nodeIds = new Set();

  for (const node of topology.nodes) {
    if (!node.primaryMac) continue;
    const isSelf = selfPrimaries.has(node.primaryMac);
    nodes.push({
      id: node.primaryMac,
      label: node.primaryHostname || shortMac(node.primaryMac),
      fill: isSelf ? COLOR_SELF : COLOR_PEER,
      size: isSelf ? 14 : 10,
      data: {
        type: isSelf ? 'self' : 'peer',
        cluster: node.primaryMac,
        mac: node.primaryMac,
        hostname: node.primaryHostname,
        secondary: node.secondaryMacs || [],
      },
    });
    nodeIds.add(node.primaryMac);

    for (const c of node.clients || []) {
      if (!c.mac) continue;
      const id = `client:${node.primaryMac}:${c.mac}`;
      nodes.push({
        id,
        label: c.hostname || shortMac(c.mac),
        fill: COLOR_CLIENT,
        size: 5,
        data: {
          type: 'client',
          cluster: node.primaryMac,
          mac: c.mac,
          hostname: c.hostname,
        },
      });
      nodeIds.add(id);
    }
  }

  // Deduplicate mesh edges across directions, preferring any direction that
  // carries a local signal reading.
  const meshEdges = new Map();
  for (const node of topology.nodes) {
    for (const e of node.neighbors || []) {
      const srcPrimary = macToPrimary.get((e.routerMac || '').toLowerCase()) || e.routerMac;
      const dstPrimary = macToPrimary.get((e.neighborMac || '').toLowerCase()) || e.neighborMac;
      if (!srcPrimary || !dstPrimary || srcPrimary === dstPrimary) continue;

      const pair = [srcPrimary, dstPrimary].sort().join('|');
      const hasSignal = e.signal && e.signal !== 0;
      const existing = meshEdges.get(pair);
      if (!existing || (hasSignal && !(existing.edge.signal && existing.edge.signal !== 0))) {
        meshEdges.set(pair, { src: srcPrimary, dst: dstPrimary, edge: e });
      }
    }
  }

  const edges = [];
  for (const { src, dst, edge } of meshEdges.values()) {
    if (!nodeIds.has(dst)) {
      nodes.push({
        id: dst,
        label: edge.neighborHostname || shortMac(dst),
        fill: COLOR_UNKNOWN,
        size: 8,
        data: { type: 'unknown', cluster: dst, mac: dst },
      });
      nodeIds.add(dst);
    }
    if (!nodeIds.has(src)) {
      nodes.push({
        id: src,
        label: shortMac(src),
        fill: COLOR_UNKNOWN,
        size: 8,
        data: { type: 'unknown', cluster: src, mac: src },
      });
      nodeIds.add(src);
    }

    const hasSignal = edge.signal && edge.signal !== 0;
    edges.push({
      id: `mesh:${src}->${dst}`,
      source: src,
      target: dst,
      size: edgeSize(edge.metric),
      fill: hasSignal ? signalColor(edge.signal) : COLOR_EDGE_NO_SIGNAL,
      label: hasSignal ? `${edge.signal} dBm` : formatMetric(edge.metric),
      data: edge,
    });
  }

  for (const node of topology.nodes) {
    for (const c of node.clients || []) {
      if (!c.mac) continue;
      edges.push({
        id: `client:${node.primaryMac}->${c.mac}`,
        source: node.primaryMac,
        target: `client:${node.primaryMac}:${c.mac}`,
        size: 0.5,
        fill: COLOR_CLIENT,
      });
    }
  }

  return { nodes, edges };
}
