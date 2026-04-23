// =============================================================================
// topologyGraph.js — Pure transform from MeshTopology → renderable view object
// =============================================================================
// Produces a deterministic, render-friendly shape for the SVG topology
// renderer in TopologyMap.jsx. Kept separate from the component so the
// transform can be unit-tested without jsdom.
//
// Output shape:
//   {
//     self: HostRecord | null,
//     hosts: [HostRecord, ...],                       // every host, sorted by tag
//     segments: [{ id, hosts: [HostRecord,...],
//                  edges: [AggregateEdge,...] }, ...],
//     blosEdges: [AggregateEdge, ...],                // bridges across segments
//     counts: { hosts, segments, links, blosLinks,
//               degraded, clients, hopsMax },
//   }
//
// A "host" is one physical mesh device identified by its base hostname
// (everything before the last underscore in the /tmp/bat-hosts friendly
// name, e.g. "BCM2711-97d6" from "BCM2711-97d6_phy2-mesh0"). A single host
// may carry multiple mesh interfaces (wlan0, phy2-mesh0, vxlan0…) and
// those collapse into one HostRecord with an `interfaces` list.
//
// Edges are aggregated per host-pair: if two hosts are linked on both
// wlan0 and phy2-mesh0, they produce one AggregateEdge carrying two
// contributors. An edge whose endpoint interface is `vxlan0` is classified
// as a BLOS bridge; its endpoint hosts are split into distinct segments.

const WEAK_METRIC_THRESHOLD = 2.0;
const WEAK_SIGNAL_DBM = -75;
const BLOS_INTERFACE = 'vxlan0';

function shortMac(mac) {
  if (!mac) return '?';
  const parts = mac.split(':');
  return parts.length === 6 ? parts.slice(3).join(':') : mac;
}

function padTag(i) {
  return `N-${String(i + 1).padStart(2, '0')}`;
}

function segmentLetter(i) {
  // Sequence A…Z, AA…AZ, BA… — plenty for any realistic mesh.
  let n = i;
  let out = '';
  do {
    out = String.fromCharCode(65 + (n % 26)) + out;
    n = Math.floor(n / 26) - 1;
  } while (n >= 0);
  return out;
}

// Split a bat-hosts friendly name at the LAST underscore so that base
// hostnames containing '-' or '_' still round-trip.
//   "BCM2711-97d6_phy2-mesh0" → { base: "BCM2711-97d6", iface: "phy2-mesh0" }
//   "alpha"                   → { base: "alpha", iface: "" }
//   ""                        → { base: "", iface: "" }
function splitHostname(hostname) {
  if (!hostname) return { base: '', iface: '' };
  const i = hostname.lastIndexOf('_');
  if (i < 0) return { base: hostname, iface: '' };
  return { base: hostname.slice(0, i), iface: hostname.slice(i + 1) };
}

function interfaceRole(name) {
  return name === BLOS_INTERFACE ? 'blos' : 'rf';
}

export { shortMac };

// -----------------------------------------------------------------------------
// buildTopologyView(topology)
// -----------------------------------------------------------------------------
export function buildTopologyView(topology) {
  const empty = {
    self: null,
    hosts: [],
    segments: [],
    blosEdges: [],
    counts: {
      hosts: 0,
      segments: 0,
      links: 0,
      blosLinks: 0,
      degraded: 0,
      clients: 0,
      hopsMax: 0,
    },
  };
  if (!topology || !Array.isArray(topology.nodes) || topology.nodes.length === 0) {
    return empty;
  }

  // ── MAC → { base, iface } lookup ────────────────────────────────────────
  // Populated from every source that names a MAC with a friendly hostname:
  //   • MeshNode.primaryHostname (MAC = primaryMac, iface typically "bat0")
  //   • MeshEdge.routerHostname  (MAC = routerMac, iface = suffix)
  //   • MeshEdge.neighborHostname(MAC = neighborMac, iface = suffix)
  // Missing entries degrade gracefully: the MAC becomes its own base host
  // so the graph still renders, just without interface-level detail.
  const macToHost = new Map();
  const noteMac = (mac, hostname) => {
    if (!mac) return;
    const key = mac.toLowerCase();
    if (macToHost.has(key)) return;
    const { base, iface } = splitHostname(hostname);
    if (!base) return;
    macToHost.set(key, { base, iface });
  };

  for (const node of topology.nodes) {
    noteMac(node.primaryMac, node.primaryHostname);
    for (const edge of node.neighbors || []) {
      noteMac(edge.routerMac, edge.routerHostname);
      noteMac(edge.neighborMac, edge.neighborHostname);
    }
  }

  // Resolve a MAC to its (base, iface). Falls back to the MAC itself as the
  // base when no hostname was ever seen — this keeps the view renderable
  // against a node whose bat-hosts entry is missing.
  const resolveMac = (mac) => {
    if (!mac) return { base: '', iface: '', key: '' };
    const key = mac.toLowerCase();
    const hit = macToHost.get(key);
    if (hit) return { base: hit.base, iface: hit.iface, key: hit.base.toLowerCase() };
    return { base: mac, iface: '', key };
  };

  // ── Host records keyed by base hostname (lowercase) ─────────────────────
  const hostByKey = new Map();
  const ensureHost = (base, primaryMac) => {
    const key = base.toLowerCase();
    let h = hostByKey.get(key);
    if (!h) {
      h = {
        id: key,
        baseHostname: base,
        primaryMac: primaryMac || '',
        tag: '',
        interfaces: [],      // [{ name, role, mac }]
        ifaceByName: new Map(),
        segmentId: '',
        hops: Number.MAX_SAFE_INTEGER,
        isSelf: false,
        degraded: false,
        clients: [],
      };
      hostByKey.set(key, h);
    } else if (!h.primaryMac && primaryMac) {
      h.primaryMac = primaryMac;
    }
    return h;
  };

  const addInterface = (host, name, mac) => {
    if (!name) return;
    const existing = host.ifaceByName.get(name);
    if (existing) {
      if (!existing.mac && mac) existing.mac = mac;
      return;
    }
    const entry = { name, role: interfaceRole(name), mac: mac || '' };
    host.ifaceByName.set(name, entry);
    host.interfaces.push(entry);
  };

  // Seed hosts from primaryHostname; track which node primaries carry local
  // signal (the self-detection heuristic: only our own radios can observe
  // signal strength on their outgoing edges).
  const selfBaseKeys = new Set();
  for (const node of topology.nodes) {
    const { base: primaryBase, iface: primaryIface } = splitHostname(node.primaryHostname);
    const base = primaryBase || node.primaryMac || '';
    if (!base) continue;
    const host = ensureHost(base, node.primaryMac);
    if (primaryIface) addInterface(host, primaryIface, node.primaryMac);

    if ((node.neighbors || []).some((e) => e.signal && e.signal !== 0)) {
      selfBaseKeys.add(host.id);
    }
  }

  // Walk every edge to (a) add router/neighbor interfaces to their hosts
  // and (b) build the per-interface-pair edge set with bidirectional dedup.
  //
  // Per-interface-pair key: sort endpoints lexicographically by
  // `<hostKey>|<iface>` so that A→B (wlan0→wlan0) and B→A (wlan0→wlan0)
  // collapse to one entry; if both carry samples, prefer the one with a
  // non-zero signal reading.
  const pairs = new Map();
  for (const node of topology.nodes) {
    for (const edge of node.neighbors || []) {
      const r = resolveMac(edge.routerMac);
      const n = resolveMac(edge.neighborMac);
      if (!r.key || !n.key || r.key === n.key) continue;

      const rHost = ensureHost(r.base, '');
      const nHost = ensureHost(n.base, '');
      if (r.iface) addInterface(rHost, r.iface, edge.routerMac);
      if (n.iface) addInterface(nHost, n.iface, edge.neighborMac);

      const a = { hostKey: r.key, iface: r.iface };
      const b = { hostKey: n.key, iface: n.iface };
      // Canonical ordering by (hostKey, iface) so direction is deterministic.
      const ordered = (a.hostKey < b.hostKey ||
        (a.hostKey === b.hostKey && a.iface < b.iface))
        ? [a, b]
        : [b, a];
      const pairKey = `${ordered[0].hostKey}|${ordered[0].iface}#${ordered[1].hostKey}|${ordered[1].iface}`;

      const sample = {
        srcHostKey: r.key,
        srcIface: r.iface,
        dstHostKey: n.key,
        dstIface: n.iface,
        metric: edge.metric || 0,
        signal: edge.signal || 0,
        signalAverage: edge.signalAverage || 0,
      };
      const existing = pairs.get(pairKey);
      const hasSignal = sample.signal !== 0;
      if (!existing) {
        pairs.set(pairKey, sample);
      } else if (hasSignal && existing.signal === 0) {
        pairs.set(pairKey, sample);
      }
    }
  }

  // ── Aggregate per (hostA, hostB) pair ───────────────────────────────────
  const aggregates = new Map();
  for (const s of pairs.values()) {
    const [aKey, bKey] = s.srcHostKey < s.dstHostKey
      ? [s.srcHostKey, s.dstHostKey]
      : [s.dstHostKey, s.srcHostKey];
    const aggKey = `${aKey}|${bKey}`;

    // Contributor is always stored oriented source→destination as sampled
    // so the Selected panel can show "wlan0 → wlan0 -50 dBm" in the real
    // direction the reading was taken.
    const contributor = {
      srcHost: s.srcHostKey,
      srcIface: s.srcIface,
      dstHost: s.dstHostKey,
      dstIface: s.dstIface,
      metric: s.metric,
      signal: s.signal,
      signalAverage: s.signalAverage,
    };

    let agg = aggregates.get(aggKey);
    if (!agg) {
      agg = {
        id: `agg:${aggKey}`,
        hostA: aKey,
        hostB: bKey,
        bestSignal: 0,       // dBm: most positive wins (closer to zero = stronger)
        bestMetric: 0,       // TQ: smallest non-zero wins (closer to 1 = best)
        weak: false,
        blos: false,
        contributors: [],
      };
      aggregates.set(aggKey, agg);
    }
    agg.contributors.push(contributor);

    if (s.signal !== 0) {
      if (agg.bestSignal === 0 || s.signal > agg.bestSignal) {
        agg.bestSignal = s.signal;
      }
    }
    if (s.metric > 0) {
      if (agg.bestMetric === 0 || s.metric < agg.bestMetric) {
        agg.bestMetric = s.metric;
      }
    }
    const thisWeak =
      (s.metric || 0) > WEAK_METRIC_THRESHOLD ||
      (s.signal !== 0 && s.signal < WEAK_SIGNAL_DBM);
    if (thisWeak) agg.weak = true;
    if (s.srcIface === BLOS_INTERFACE || s.dstIface === BLOS_INTERFACE) {
      agg.blos = true;
    }
  }

  // ── Segment detection (BFS over RF-only aggregate edges) ────────────────
  const adjRF = new Map();
  const adjAll = new Map();
  for (const key of hostByKey.keys()) {
    adjRF.set(key, new Set());
    adjAll.set(key, new Set());
  }
  for (const agg of aggregates.values()) {
    adjAll.get(agg.hostA).add(agg.hostB);
    adjAll.get(agg.hostB).add(agg.hostA);
    if (!agg.blos) {
      adjRF.get(agg.hostA).add(agg.hostB);
      adjRF.get(agg.hostB).add(agg.hostA);
    }
  }

  const segmentByHost = new Map();
  const rawSegments = [];   // [{ hosts: Set<key>, edges: AggregateEdge[] }]
  const visited = new Set();
  // Deterministic BFS root order → segment ordering is stable across
  // refreshes. Sort host keys ascending for the outer pass.
  const hostKeysSorted = [...hostByKey.keys()].sort();
  for (const root of hostKeysSorted) {
    if (visited.has(root)) continue;
    const segHosts = new Set();
    const q = [root];
    visited.add(root);
    while (q.length > 0) {
      const cur = q.shift();
      segHosts.add(cur);
      for (const nxt of adjRF.get(cur) || []) {
        if (!visited.has(nxt)) {
          visited.add(nxt);
          q.push(nxt);
        }
      }
    }
    rawSegments.push({ hosts: segHosts });
  }

  // Order segments by their lexicographically smallest host key so the
  // first segment (with the smallest host) is always "A".
  rawSegments.sort((x, y) => {
    const mx = [...x.hosts].sort()[0] || '';
    const my = [...y.hosts].sort()[0] || '';
    return mx.localeCompare(my);
  });

  rawSegments.forEach((seg, i) => {
    const id = segmentLetter(i);
    for (const k of seg.hosts) segmentByHost.set(k, id);
    seg.id = id;
    seg.edges = [];
  });

  for (const agg of aggregates.values()) {
    if (agg.blos) continue;
    const segA = segmentByHost.get(agg.hostA);
    const segB = segmentByHost.get(agg.hostB);
    // RF edges must not cross segments by definition of the partition.
    if (segA === segB && segA) {
      const seg = rawSegments.find((s) => s.id === segA);
      if (seg) seg.edges.push(agg);
    }
  }

  const blosEdges = [...aggregates.values()].filter((a) => a.blos);

  // ── Hops: BFS from self over the full (RF+BLOS) adjacency ───────────────
  const hopsByKey = new Map();
  const bfsRoots = selfBaseKeys.size > 0
    ? [...selfBaseKeys]
    : hostKeysSorted.slice(0, 1);
  for (const root of bfsRoots) {
    if (hopsByKey.has(root)) continue;
    hopsByKey.set(root, 0);
    const q = [root];
    while (q.length > 0) {
      const cur = q.shift();
      const d = hopsByKey.get(cur);
      for (const nxt of adjAll.get(cur) || []) {
        if (!hopsByKey.has(nxt)) {
          hopsByKey.set(nxt, d + 1);
          q.push(nxt);
        }
      }
    }
  }
  for (const host of hostByKey.values()) {
    host.hops = hopsByKey.has(host.id) ? hopsByKey.get(host.id) : 99;
    host.segmentId = segmentByHost.get(host.id) || '';
    host.isSelf = selfBaseKeys.has(host.id);
  }

  // ── Degraded propagation: host is degraded if any incident edge is weak.
  for (const agg of aggregates.values()) {
    if (!agg.weak) continue;
    const a = hostByKey.get(agg.hostA);
    const b = hostByKey.get(agg.hostB);
    if (a) a.degraded = true;
    if (b) b.degraded = true;
  }

  // ── Clients: first-parent-wins across all nodes, attached by base host ──
  const clientParent = new Map();
  const clientsByHostKey = new Map();
  for (const node of topology.nodes) {
    const { base } = splitHostname(node.primaryHostname);
    const key = (base || node.primaryMac || '').toLowerCase();
    if (!key || !hostByKey.has(key)) continue;
    for (const c of node.clients || []) {
      if (!c.mac || clientParent.has(c.mac)) continue;
      clientParent.set(c.mac, key);
      if (!clientsByHostKey.has(key)) clientsByHostKey.set(key, []);
      clientsByHostKey.get(key).push({ mac: c.mac, hostname: c.hostname || '' });
    }
  }

  // ── Stable sort: (hops asc, baseHostname asc) and assign tags ──────────
  const hosts = [...hostByKey.values()].sort((a, b) => {
    if (a.hops !== b.hops) return a.hops - b.hops;
    return a.id.localeCompare(b.id);
  });
  hosts.forEach((h, i) => { h.tag = padTag(i); });

  let clientCounter = 0;
  const nextClientTag = () => `C${++clientCounter}`;
  for (const h of hosts) {
    h.clients = (clientsByHostKey.get(h.id) || []).map((c) => ({
      id: `client:${c.mac}`,
      mac: c.mac,
      hostname: c.hostname,
      tag: nextClientTag(),
    }));
    // Freeze the per-host interfaces list in a stable order: RF first
    // (alphabetical), then BLOS interfaces at the end so the vxlan0 badge
    // consistently sits last in the host circle.
    h.interfaces.sort((a, b) => {
      if (a.role !== b.role) return a.role === 'blos' ? 1 : -1;
      return a.name.localeCompare(b.name);
    });
    delete h.ifaceByName;
  }

  // Hydrate segments with sorted host refs (match the global tag order).
  const segments = rawSegments.map((seg) => ({
    id: seg.id,
    hosts: hosts.filter((h) => seg.hosts.has(h.id)),
    edges: seg.edges,
  }));

  const self = hosts.find((h) => h.isSelf) || null;

  const peerHops = hosts
    .filter((h) => !h.isSelf)
    .map((h) => h.hops)
    .filter((h) => h < 99);
  const hopsMax = peerHops.length > 0 ? Math.max(...peerHops) : 0;

  return {
    self,
    hosts,
    segments,
    blosEdges,
    counts: {
      hosts: hosts.length,
      segments: segments.length,
      links: aggregates.size - blosEdges.length,
      blosLinks: blosEdges.length,
      degraded: hosts.filter((h) => h.degraded).length,
      clients: clientCounter,
      hopsMax,
    },
  };
}
