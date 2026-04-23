// =============================================================================
// topologyGraph.js — Transform MeshTopology (originator rows) → render view
// =============================================================================
// Input is the host's local batman-adv originator table. Each row is one
// best-route entry: a reachable originator, the local next hop we forward
// through, and the hardware interface that carries the route.
//
// The transform:
//   1. Groups rows by base hostname (stripping the "_iface" suffix that
//      /tmp/bat-hosts uses to disambiguate interfaces on one physical node).
//      One HostRecord per physical device; its `interfaces[]` captures every
//      suffix we've seen for that host.
//   2. Partitions hosts into segments:
//        - LOCAL:  hosts whose best route uses a non-vxlan0 interface.
//        - REMOTE: one segment per local BLOS interface. Every originator
//          reached via that interface — direct neighbors AND multi-hop peers
//          behind them — collapses into the same segment, because from this
//          node's point of view they share a single tunnel.
//   3. Aggregates per-host-pair edges so two hosts linked on multiple radios
//      render as a single line with a contributor list in the Selected panel.
//   4. Preserves hops as reported by the server (no client-side BFS).
//
// Output shape (stable for the renderer):
//
//   {
//     self: HostRecord | null,
//     hosts: [HostRecord, ...],
//     segments: [
//       { id, label, kind: 'local'|'remote', anchorHost?, hosts, edges }
//     ],
//     blosEdges: [AggregateEdge, ...],
//     counts: { hosts, segments, links, blosLinks, clients, hopsMax },
//     algorithm: 'BATMAN_IV' | 'BATMAN_V' | '',
//   }

const BLOS_INTERFACE = 'vxlan0';

// ── small helpers ──────────────────────────────────────────────────────────

// Split a bat-hosts friendly name at the LAST underscore so base hostnames
// containing '-' still round-trip.
//   "BCM2711-97d6_phy2-mesh0" → { base: "BCM2711-97d6", iface: "phy2-mesh0" }
function splitHostname(hostname) {
  if (!hostname) return { base: '', iface: '' };
  const i = hostname.lastIndexOf('_');
  if (i < 0) return { base: hostname, iface: '' };
  return { base: hostname.slice(0, i), iface: hostname.slice(i + 1) };
}

// shortHostname returns a compact label that fits inside a host circle.
// For hyphenated hostnames we use the final segment (e.g. "BCM2711-97d6" →
// "97d6"); otherwise the full hostname up to a 6-char cap.
export function shortHostname(baseHostname) {
  if (!baseHostname) return '?';
  const parts = baseHostname.split('-');
  const tail = parts[parts.length - 1] || baseHostname;
  return tail.length <= 6 ? tail : tail.slice(0, 6);
}

function interfaceRole(name) {
  return name === BLOS_INTERFACE ? 'blos' : 'rf';
}

function padTag(i) {
  return `N-${String(i + 1).padStart(2, '0')}`;
}

// -----------------------------------------------------------------------------
// buildTopologyView(topology)
// -----------------------------------------------------------------------------
export function buildTopologyView(topology) {
  const empty = {
    self: null,
    hosts: [],
    segments: [],
    blosEdges: [],
    counts: { hosts: 0, segments: 0, links: 0, blosLinks: 0, clients: 0, hopsMax: 0 },
    algorithm: '',
  };

  if (!topology || !Array.isArray(topology.originators)) return empty;

  const originators = topology.originators;
  if (originators.length === 0 && !topology.selfHostname) return empty;

  // ── MAC → { base, iface } lookup, for resolving next-hop MACs to hosts ──
  const macToHost = new Map();
  const noteMac = (mac, hostname) => {
    if (!mac) return;
    const key = mac.toLowerCase();
    if (macToHost.has(key)) return;
    const { base, iface } = splitHostname(hostname);
    if (!base) return;
    macToHost.set(key, { base, iface });
  };
  for (const o of originators) {
    noteMac(o.origMac, o.origHostname);
    noteMac(o.nextHopMac, o.nextHopHostname);
  }
  // Self gets its own entry so next-hop MACs that point back to us resolve.
  if (topology.selfMac) {
    noteMac(topology.selfMac, `${topology.selfHostname || topology.selfMac}_bat0`);
  }

  const resolveMac = (mac) => {
    if (!mac) return { base: '', iface: '', key: '' };
    const key = mac.toLowerCase();
    const hit = macToHost.get(key);
    if (hit) return { base: hit.base, iface: hit.iface, key: hit.base.toLowerCase() };
    return { base: mac, iface: '', key };
  };

  // ── Host records keyed by base hostname (lowercase) ────────────────────
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
        interfaces: [],
        ifaceByName: new Map(),
        segmentId: '',
        hops: Number.MAX_SAFE_INTEGER,
        isSelf: false,
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

  // Seed the self host even when there are zero originators — the UI still
  // needs something to render.
  let selfHost = null;
  if (topology.selfHostname || topology.selfMac) {
    const selfBase = topology.selfHostname || topology.selfMac;
    selfHost = ensureHost(selfBase, topology.selfMac);
    selfHost.isSelf = true;
    selfHost.hops = 0;
  }

  // Build hosts + per-originator pairs. We also track per-originator
  // segment-assignment hints: the local hard_ifname of every BLOS-reached
  // originator becomes the key for its remote segment, so all peers sharing
  // a tunnel collapse into one segment regardless of their far-side gateway.
  const blosIfnameByKey = new Map();   // hostKey → local BLOS hard_ifname carrying the route
  const rfOrigByKey = new Map();       // hostKey → true when the host has any non-BLOS best route
  const aggregates = new Map();        // canonical (hostA|hostB) → AggregateEdge

  const selfKey = selfHost ? selfHost.id : '';

  for (const o of originators) {
    const orig = resolveMac(o.origMac);
    if (!orig.key) continue;

    const origHost = ensureHost(orig.base, o.origMac);
    if (orig.iface) addInterface(origHost, orig.iface, o.origMac);
    origHost.hops = Math.min(origHost.hops, o.hops || 99);

    // The local hard_ifname carried the route — add it as one of OUR
    // interfaces so the self node's badge row stays complete.
    if (selfHost && o.hardIfname) {
      addInterface(selfHost, o.hardIfname, topology.selfMac);
    }

    // Track segment hints. For BLOS-reached hosts we record the LOCAL
    // hard_ifname (e.g. vxlan0); that becomes the remote segment's key, so
    // every peer reached via that same interface collapses into one box.
    if (o.hardIfname === BLOS_INTERFACE) {
      if (!blosIfnameByKey.has(orig.key)) blosIfnameByKey.set(orig.key, o.hardIfname);
    } else if (o.hardIfname) {
      rfOrigByKey.set(orig.key, true);
    }

    // Aggregate into a per-host-pair edge. Direct neighbors form a (self,
    // host) edge; multi-hop peers form a (nextHopHost, origHost) edge so
    // the chain renders as concatenated links rather than spokes back to us.
    const next = resolveMac(o.nextHopMac);
    let hostA = selfKey;
    let hostB = orig.key;
    if (o.hops > 1 && next.key && next.key !== orig.key) {
      hostA = next.key;
      hostB = orig.key;
    }
    if (!hostA || !hostB || hostA === hostB) continue;

    const [aKey, bKey] = hostA < hostB ? [hostA, hostB] : [hostB, hostA];
    const aggKey = `${aKey}|${bKey}`;
    let agg = aggregates.get(aggKey);
    if (!agg) {
      agg = {
        id: `agg:${aggKey}`,
        hostA: aKey,
        hostB: bKey,
        bestTQ: 0,
        bestThroughput: 0,
        blos: false,
        contributors: [],
      };
      aggregates.set(aggKey, agg);
    }
    agg.contributors.push({
      srcHost: hostA,
      srcIface: o.hardIfname,
      dstHost: hostB,
      dstIface: orig.iface,
      tq: o.tq || 0,
      throughput: o.throughput || 0,
      hops: o.hops || 0,
      lastSeenMs: o.lastSeenMs || 0,
    });
    if ((o.tq || 0) > agg.bestTQ) agg.bestTQ = o.tq || 0;
    if ((o.throughput || 0) > agg.bestThroughput) agg.bestThroughput = o.throughput || 0;
    if (o.hardIfname === BLOS_INTERFACE) agg.blos = true;
  }

  // ── Segment assignment ─────────────────────────────────────────────────
  // Rule: if a host has ANY non-BLOS best route, it's local. Otherwise it
  // joins the remote segment keyed by the LOCAL hard_ifname carrying its
  // BLOS route. All peers reached via the same local tunnel collapse into
  // one segment regardless of how many distinct far-side gateways exist.
  const segmentByHost = new Map();

  // Local segment first — self is always local when it exists.
  const localKeys = new Set();
  if (selfHost) localKeys.add(selfHost.id);
  for (const k of rfOrigByKey.keys()) localKeys.add(k);

  // Remote segments: one per local BLOS interface.
  const remoteByIfname = new Map(); // ifname → Set(hostKey)
  for (const [hostKey, ifname] of blosIfnameByKey.entries()) {
    if (localKeys.has(hostKey)) continue; // host has a non-BLOS path, stays local
    if (!remoteByIfname.has(ifname)) remoteByIfname.set(ifname, new Set());
    remoteByIfname.get(ifname).add(hostKey);
  }

  // Build segment list. Local first, then remote segments sorted by ifname.
  const segments = [];
  if (localKeys.size > 0) {
    segments.push({
      id: 'local',
      label: 'LOCAL',
      kind: 'local',
      anchorHost: null,
      hosts: [],
      edges: [],
    });
    for (const k of localKeys) segmentByHost.set(k, 'local');
  }

  const remoteIfnames = [...remoteByIfname.keys()].sort();
  for (const ifname of remoteIfnames) {
    const segId = `remote:${ifname}`;
    segments.push({
      id: segId,
      label: 'REMOTE MESH',
      kind: 'remote',
      anchorHost: null, // populated after host hops are finalized below
      hosts: [],
      edges: [],
    });
    for (const k of remoteByIfname.get(ifname)) segmentByHost.set(k, segId);
  }

  // Populate segmentId on each host.
  for (const host of hostByKey.values()) {
    host.segmentId = segmentByHost.get(host.id) || (segments[0]?.id || '');
  }

  // ── Assign tags: self first, then hops asc, then hostname asc ──────────
  const hosts = [...hostByKey.values()].sort((a, b) => {
    if (a.isSelf !== b.isSelf) return a.isSelf ? -1 : 1;
    if (a.hops !== b.hops) return a.hops - b.hops;
    return a.id.localeCompare(b.id);
  });
  hosts.forEach((h, i) => { h.tag = padTag(i); });

  // Stable-sort each host's interface list so badges don't shuffle.
  for (const h of hosts) {
    h.interfaces.sort((a, b) => {
      if (a.role !== b.role) return a.role === 'blos' ? 1 : -1;
      return a.name.localeCompare(b.name);
    });
    delete h.ifaceByName;
  }

  // Place hosts into their segments (in tag order).
  const segByID = new Map(segments.map((s) => [s.id, s]));
  for (const h of hosts) {
    const seg = segByID.get(h.segmentId);
    if (seg) seg.hosts.push(h);
  }

  // Pick an anchor for each remote segment: the direct BLOS neighbor (min
  // hops in the segment) with the lowest base hostname. Layout uses this
  // as the radial root so the "entry point" into the remote mesh sits at
  // the box center. Defensive fallback: first host in the segment.
  for (const seg of segments) {
    if (seg.kind !== 'remote' || seg.hosts.length === 0) continue;
    const minHops = Math.min(...seg.hosts.map((h) => h.hops));
    const candidates = seg.hosts
      .filter((h) => h.hops === minHops)
      .sort((a, b) => a.baseHostname.localeCompare(b.baseHostname));
    seg.anchorHost = (candidates[0] || seg.hosts[0]).id;
  }

  // RF edges belong in the segment both endpoints share (by definition of
  // the partition, only intra-segment edges are RF). BLOS edges are the
  // bridges between segments.
  const blosEdges = [];
  for (const agg of aggregates.values()) {
    if (agg.blos) {
      blosEdges.push(agg);
      continue;
    }
    const segA = segmentByHost.get(agg.hostA);
    const segB = segmentByHost.get(agg.hostB);
    if (segA && segA === segB) {
      segByID.get(segA)?.edges.push(agg);
    }
  }

  const peerHops = hosts.filter((h) => !h.isSelf).map((h) => h.hops).filter((h) => h < 99);
  const hopsMax = peerHops.length > 0 ? Math.max(...peerHops) : 0;

  return {
    self: selfHost,
    hosts,
    segments,
    blosEdges,
    counts: {
      hosts: hosts.length,
      segments: segments.length,
      links: aggregates.size - blosEdges.length,
      blosLinks: blosEdges.length,
      clients: 0,
      hopsMax,
    },
    algorithm: topology.algorithm || '',
  };
}
