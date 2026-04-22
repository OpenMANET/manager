// =============================================================================
// topologyGraph.js — Pure transform from MeshTopology → renderable view object
// =============================================================================
// Produces a deterministic, render-friendly shape for the SVG topology
// renderer in TopologyMap.jsx. Kept separate from the component so the
// transform can be unit-tested without jsdom.
//
// Output shape:
//   {
//     self: { id, mac, hostname, tag, hops: 0, degraded, clients[] } | null,
//     peers: [{ id, mac, hostname, tag, hops, degraded, clients[] }],
//     edges: [{ id, src, dst, signal, metric, weak }],
//     counts: { peers, degraded, clients, hopsMax },
//   }
//
// Responsibilities:
//   - Resolve secondary MACs (mesh radio interfaces) back to the primary MAC
//     that names the node, so edges collapse to primary-primary.
//   - Identify "self" via the heuristic that only our own mesh radio reports
//     signal strength on its outgoing edges.
//   - Dedup bidirectional mesh edges (A→B from A's batadv and B→A from B's
//     batadv) into a single canonical edge, preferring whichever direction
//     carries a local signal reading.
//   - Global-dedup TT clients (roaming entries appearing under multiple
//     peers become one node attached to the first parent seen).
//   - BFS from self to assign a hop-depth per node; synthesized "unknown"
//     peers (edge endpoints not in topology.nodes) get a hop-depth too.
//   - Sort nodes by (hops asc, mac asc) and assign sequential N-NN tags so
//     the operator sees stable numbering across refreshes.

const WEAK_METRIC_THRESHOLD = 2.0;
const WEAK_SIGNAL_DBM = -75;

function shortMac(mac) {
  if (!mac) return '?';
  const parts = mac.split(':');
  return parts.length === 6 ? parts.slice(3).join(':') : mac;
}

function padTag(i) {
  return `N-${String(i + 1).padStart(2, '0')}`;
}

export { shortMac };

// -----------------------------------------------------------------------------
// buildTopologyView(topology)
// -----------------------------------------------------------------------------
export function buildTopologyView(topology) {
  const empty = {
    self: null,
    peers: [],
    edges: [],
    counts: { peers: 0, degraded: 0, clients: 0, hopsMax: 0 },
  };
  if (!topology || !Array.isArray(topology.nodes) || topology.nodes.length === 0) {
    return empty;
  }

  // ── mac → primary ───────────────────────────────────────────────────────
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

  // ── mesh edge dedup (prefer direction with signal) ──────────────────────
  const pairSeen = new Map();
  for (const node of topology.nodes) {
    for (const e of node.neighbors || []) {
      const src = macToPrimary.get((e.routerMac || '').toLowerCase()) || e.routerMac;
      const dst = macToPrimary.get((e.neighborMac || '').toLowerCase()) || e.neighborMac;
      if (!src || !dst || src === dst) continue;
      const pair = [src, dst].sort().join('|');
      const hasSignal = e.signal && e.signal !== 0;
      const existing = pairSeen.get(pair);
      if (!existing) {
        pairSeen.set(pair, {
          src, dst,
          metric: e.metric || 0,
          signal: e.signal || 0,
          neighborHostname: e.neighborHostname || '',
        });
      } else if (hasSignal && !(existing.signal && existing.signal !== 0)) {
        pairSeen.set(pair, {
          src, dst,
          metric: e.metric || 0,
          signal: e.signal || 0,
          neighborHostname: e.neighborHostname || '',
        });
      }
    }
  }

  // ── collect every mac, build adjacency ──────────────────────────────────
  const allMacs = new Set();
  for (const node of topology.nodes) if (node.primaryMac) allMacs.add(node.primaryMac);
  for (const edge of pairSeen.values()) {
    allMacs.add(edge.src);
    allMacs.add(edge.dst);
  }
  const adj = new Map();
  for (const mac of allMacs) adj.set(mac, new Set());
  for (const edge of pairSeen.values()) {
    adj.get(edge.src).add(edge.dst);
    adj.get(edge.dst).add(edge.src);
  }

  // ── BFS from self → hops ────────────────────────────────────────────────
  const hopsByMac = new Map();
  const roots = selfPrimaries.size > 0
    ? [...selfPrimaries]
    : ([...allMacs].slice(0, 1));
  for (const root of roots) {
    if (hopsByMac.has(root)) continue;
    hopsByMac.set(root, 0);
    const queue = [root];
    while (queue.length > 0) {
      const cur = queue.shift();
      const dist = hopsByMac.get(cur);
      for (const nxt of adj.get(cur) || []) {
        if (!hopsByMac.has(nxt)) {
          hopsByMac.set(nxt, dist + 1);
          queue.push(nxt);
        }
      }
    }
  }

  // ── degraded ────────────────────────────────────────────────────────────
  const degradedByMac = new Set();
  for (const edge of pairSeen.values()) {
    if ((edge.metric || 0) > WEAK_METRIC_THRESHOLD) {
      degradedByMac.add(edge.src);
      degradedByMac.add(edge.dst);
    }
  }

  // ── hostnames ───────────────────────────────────────────────────────────
  const hostnameByMac = new Map();
  for (const node of topology.nodes) {
    if (node.primaryMac) hostnameByMac.set(node.primaryMac, node.primaryHostname || '');
  }
  for (const edge of pairSeen.values()) {
    if (edge.neighborHostname && !hostnameByMac.has(edge.dst)) {
      hostnameByMac.set(edge.dst, edge.neighborHostname);
    }
  }

  // ── sort + assign tags ──────────────────────────────────────────────────
  const sortedMacs = [...allMacs].sort((a, b) => {
    const ha = hopsByMac.has(a) ? hopsByMac.get(a) : Number.MAX_SAFE_INTEGER;
    const hb = hopsByMac.has(b) ? hopsByMac.get(b) : Number.MAX_SAFE_INTEGER;
    if (ha !== hb) return ha - hb;
    return a.localeCompare(b);
  });
  const tagByMac = new Map();
  sortedMacs.forEach((mac, i) => tagByMac.set(mac, padTag(i)));

  // ── clients (first-parent-wins dedup) ───────────────────────────────────
  const clientParentByMac = new Map();
  const clientsByParent = new Map();
  for (const node of topology.nodes) {
    if (!node.primaryMac) continue;
    for (const c of node.clients || []) {
      if (!c.mac || clientParentByMac.has(c.mac)) continue;
      clientParentByMac.set(c.mac, node.primaryMac);
      if (!clientsByParent.has(node.primaryMac)) clientsByParent.set(node.primaryMac, []);
      clientsByParent.get(node.primaryMac).push({ mac: c.mac, hostname: c.hostname || '' });
    }
  }

  // ── build peers + self ──────────────────────────────────────────────────
  let clientCounter = 0;
  const nextClientTag = () => `C${++clientCounter}`;

  let self = null;
  const peers = [];
  for (const mac of sortedMacs) {
    const isSelf = selfPrimaries.has(mac);
    const clients = (clientsByParent.get(mac) || []).map((c) => ({
      id: `client:${c.mac}`,
      mac: c.mac,
      hostname: c.hostname,
      tag: nextClientTag(),
    }));
    const hops = hopsByMac.has(mac) ? hopsByMac.get(mac) : 99;
    const record = {
      id: mac,
      mac,
      hostname: hostnameByMac.get(mac) || '',
      tag: tagByMac.get(mac),
      hops,
      degraded: degradedByMac.has(mac),
      clients,
    };
    if (isSelf && !self) {
      self = record;
    } else {
      peers.push(record);
    }
  }

  // ── build edges ─────────────────────────────────────────────────────────
  const edges = [];
  for (const edge of pairSeen.values()) {
    const weak =
      (edge.metric || 0) > WEAK_METRIC_THRESHOLD ||
      (edge.signal !== 0 && edge.signal < WEAK_SIGNAL_DBM);
    edges.push({
      id: `mesh:${edge.src}|${edge.dst}`,
      src: edge.src,
      dst: edge.dst,
      signal: edge.signal || 0,
      metric: edge.metric || 0,
      weak,
    });
  }

  const peerHops = peers.map((p) => p.hops).filter((h) => h < 99);
  const hopsMax = peerHops.length > 0 ? Math.max(...peerHops) : 0;

  return {
    self,
    peers,
    edges,
    counts: {
      peers: peers.length,
      degraded: degradedByMac.size,
      clients: clientCounter,
      hopsMax,
    },
  };
}
