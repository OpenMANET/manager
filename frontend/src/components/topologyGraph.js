// =============================================================================
// topologyGraph.js — Pure transform from MeshTopology → reagraph {nodes,edges}
// =============================================================================
// Extracted from TopologyMap.jsx so it can be unit-tested without pulling in
// reagraph's WebGL canvas and so fast-refresh stays happy (component files
// should export components only).

// Lattice topology palette: self = ok-green (this node), peer = accent-cyan,
// unknown = muted, client = dim, signal thresholds use the shared ok/warn/crit
// tokens to match the rest of the UI.
const COLOR_SELF = '#00e676';     // ok green
const COLOR_PEER = '#00e5ff';     // accent cyan
const COLOR_UNKNOWN = '#5c7682';  // muted
const COLOR_CLIENT = '#3a4b55';   // dim
const COLOR_EDGE_NO_SIGNAL = '#1a2a3a';

export const TOPOLOGY_COLORS = {
  self: COLOR_SELF,
  peer: COLOR_PEER,
  unknown: COLOR_UNKNOWN,
  client: COLOR_CLIENT,
  edgeNoSignal: COLOR_EDGE_NO_SIGNAL,
};

function signalColor(dBm) {
  if (dBm >= -60) return '#00e676'; // strong
  if (dBm >= -75) return '#ffb300'; // moderate
  return '#ff3b4d';                  // weak
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
// prefer whichever direction carries a local `signal` reading. Client MACs
// can also appear under multiple peers (roaming TT entries) — we keep each
// client node globally unique and attach it to the first peer that claims
// it. Dedup is hard-enforced with ID sets so repeats in the upstream data
// never reach the reagraph graph (graphology throws on duplicate addNode).
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

  const pushNode = (node) => {
    if (!node.id || nodeIds.has(node.id)) return;
    nodes.push(node);
    nodeIds.add(node.id);
  };

  // Mesh peer nodes + their clients. Client IDs are global (keyed on MAC
  // only) so a roaming/duplicated client collapses to one visible node.
  for (const node of topology.nodes) {
    if (!node.primaryMac) continue;
    const isSelf = selfPrimaries.has(node.primaryMac);
    pushNode({
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

    for (const c of node.clients || []) {
      if (!c.mac) continue;
      pushNode({
        id: `client:${c.mac}`,
        label: c.hostname || shortMac(c.mac),
        fill: COLOR_CLIENT,
        size: 5,
        data: {
          type: 'client',
          cluster: node.primaryMac,
          mac: c.mac,
          hostname: c.hostname,
          parentMac: node.primaryMac,
        },
      });
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
  const edgeIds = new Set();
  const pushEdge = (edge) => {
    if (!edge.id || edgeIds.has(edge.id)) return;
    edges.push(edge);
    edgeIds.add(edge.id);
  };

  for (const { src, dst, edge } of meshEdges.values()) {
    pushNode({
      id: dst,
      label: edge.neighborHostname || shortMac(dst),
      fill: COLOR_UNKNOWN,
      size: 8,
      data: { type: 'unknown', cluster: dst, mac: dst },
    });
    pushNode({
      id: src,
      label: shortMac(src),
      fill: COLOR_UNKNOWN,
      size: 8,
      data: { type: 'unknown', cluster: src, mac: src },
    });

    const hasSignal = edge.signal && edge.signal !== 0;
    pushEdge({
      id: `mesh:${src}->${dst}`,
      source: src,
      target: dst,
      size: edgeSize(edge.metric),
      fill: hasSignal ? signalColor(edge.signal) : COLOR_EDGE_NO_SIGNAL,
      label: hasSignal ? `${edge.signal} dBm` : formatMetric(edge.metric),
      data: edge,
    });
  }

  // Client edges: attach each client to its first-seen parent peer. Dedupe
  // by client MAC since the node was also deduped globally.
  const clientEdgeSeen = new Set();
  for (const node of topology.nodes) {
    if (!node.primaryMac) continue;
    for (const c of node.clients || []) {
      if (!c.mac || clientEdgeSeen.has(c.mac)) continue;
      clientEdgeSeen.add(c.mac);
      pushEdge({
        id: `client-edge:${c.mac}`,
        source: node.primaryMac,
        target: `client:${c.mac}`,
        size: 0.5,
        fill: COLOR_CLIENT,
      });
    }
  }

  return { nodes, edges };
}
