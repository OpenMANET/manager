// =============================================================================
// topologyGraph.js — Transform MeshTopology (mesh-wide graph) → render view
// =============================================================================
// Input is the server-merged mesh graph from batadv-vis + this node's
// originator overlay:
//
//   {
//     selfMac, selfHostname, algorithm, collectedAt,
//     nodes: [{ mac, secondaryMacs, hostname, segment, clientCount,
//               hopsFromSelf, myHardIfname, isSelf }],
//     edges: [{ fromMac, toMac, metric, blos, onMyPath }],
//   }
//
// The transform:
//   1. Lifts each mesh node into a HostRecord keyed by lower-cased primary MAC.
//   2. Partitions hosts into segments (LOCAL / REMOTE MESH) using the server's
//      per-node segment classification — no client-side derivation.
//   3. Resolves edges into intra-segment RF edges and inter-segment BLOS edges.
//   4. Preserves hops / my_hard_ifname / on_my_path overlay so the renderer can
//      highlight the serving node's forwarding tree on demand.
//
// Output shape (stable for the renderer):
//
//   {
//     self: HostRecord | null,
//     hosts: [HostRecord, ...],
//     segments: [
//       { id, label, kind: 'local'|'remote', anchorHost?, hosts, edges }
//     ],
//     blosEdges: [Edge, ...],
//     counts: { hosts, segments, links, blosLinks, clients, hopsMax },
//     algorithm: 'BATMAN_IV' | 'BATMAN_V' | '',
//   }

const HOPS_UNKNOWN = 99;

// ── small helpers ──────────────────────────────────────────────────────────

// shortHostname returns a compact label that fits inside a host circle.
// For hyphenated hostnames we use the final segment (e.g. "BCM2711-97d6" →
// "97d6"); otherwise the full hostname up to a 6-char cap.
export function shortHostname(baseHostname) {
  if (!baseHostname) return '?';
  const parts = baseHostname.split('-');
  const tail = parts[parts.length - 1] || baseHostname;
  return tail.length <= 6 ? tail : tail.slice(0, 6);
}

// shortMac keeps the last three MAC octets — used as a fallback host label
// when bat-hosts has no friendly name.
function shortMac(mac) {
  if (!mac) return '?';
  const parts = mac.split(':');
  return parts.length === 6 ? parts.slice(3).join(':') : mac;
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

  if (!topology || !Array.isArray(topology.nodes)) return empty;
  if (topology.nodes.length === 0) return empty;

  // ── Host records keyed by lower-cased primary MAC ──────────────────────
  const hostByMac = new Map();
  let selfHost = null;

  for (const n of topology.nodes) {
    const key = (n.mac || '').toLowerCase();
    if (!key) continue;

    const base = n.hostname || shortMac(n.mac);
    const segmentId = n.segment === 'remote' ? 'remote' : 'local';

    const host = {
      id: key,
      mac: n.mac,
      baseHostname: base,
      primaryMac: n.mac,
      tag: '',
      interfaces: hostInterfacesForSegment(segmentId),
      segmentId,
      hops: Number.isFinite(n.hopsFromSelf) ? n.hopsFromSelf : HOPS_UNKNOWN,
      isSelf: Boolean(n.isSelf),
      clientCount: n.clientCount || 0,
      myHardIfname: n.myHardIfname || '',
      secondaryMacs: n.secondaryMacs || [],
    };

    hostByMac.set(key, host);
    if (host.isSelf) selfHost = host;
  }

  // The self host carries its actual set of local interfaces used for any
  // reachable peer (my_hard_ifname rolled up across all nodes). That gives
  // a correct multi-badge picture of self instead of the segment-default.
  if (selfHost) {
    const ifnames = new Set();
    for (const n of topology.nodes) {
      if (!n.isSelf && n.myHardIfname) ifnames.add(n.myHardIfname);
    }
    if (ifnames.size > 0) {
      selfHost.interfaces = [...ifnames].sort().map((name) => ({
        name,
        role: name === 'vxlan0' ? 'blos' : 'rf',
      }));
    }
  }

  // ── Segment construction ───────────────────────────────────────────────
  const segments = [];
  const localHosts = [];
  const remoteHosts = [];
  for (const h of hostByMac.values()) {
    if (h.segmentId === 'remote') remoteHosts.push(h);
    else localHosts.push(h);
  }

  if (localHosts.length > 0) {
    segments.push({
      id: 'local',
      label: 'LOCAL',
      kind: 'local',
      anchorHost: null,
      hosts: [],
      edges: [],
    });
  }
  if (remoteHosts.length > 0) {
    segments.push({
      id: 'remote',
      label: 'REMOTE MESH',
      kind: 'remote',
      anchorHost: null,
      hosts: [],
      edges: [],
    });
  }

  // ── Tag & sort hosts: self first, then hops asc, then hostname asc ─────
  const hosts = [...hostByMac.values()].sort((a, b) => {
    if (a.isSelf !== b.isSelf) return a.isSelf ? -1 : 1;
    if (a.hops !== b.hops) return a.hops - b.hops;
    return a.baseHostname.localeCompare(b.baseHostname);
  });
  hosts.forEach((h, i) => { h.tag = padTag(i); });

  const segByID = new Map(segments.map((s) => [s.id, s]));
  for (const h of hosts) {
    const seg = segByID.get(h.segmentId);
    if (seg) seg.hosts.push(h);
  }

  // Anchor selection for the remote segment — the direct BLOS neighbor
  // (min hops in the segment) with the lowest base hostname, so the
  // tunnel's entry point sits at the radial center.
  for (const seg of segments) {
    if (seg.kind !== 'remote' || seg.hosts.length === 0) continue;
    const minHops = Math.min(...seg.hosts.map((h) => h.hops));
    const candidates = seg.hosts
      .filter((h) => h.hops === minHops)
      .sort((a, b) => a.baseHostname.localeCompare(b.baseHostname));
    seg.anchorHost = (candidates[0] || seg.hosts[0]).id;
  }

  // ── Edges ──────────────────────────────────────────────────────────────
  const edges = (topology.edges || [])
    .map((e) => {
      const a = (e.fromMac || '').toLowerCase();
      const b = (e.toMac || '').toLowerCase();
      if (!a || !b || a === b) return null;
      if (!hostByMac.has(a) || !hostByMac.has(b)) return null;

      return {
        id: `edge:${a}|${b}`,
        hostA: a,
        hostB: b,
        metric: e.metric || 0,
        blos: Boolean(e.blos),
        onMyPath: Boolean(e.onMyPath),
      };
    })
    .filter(Boolean);

  const blosEdges = [];
  let rfLinkCount = 0;
  for (const edge of edges) {
    if (edge.blos) {
      blosEdges.push(edge);
      continue;
    }
    rfLinkCount++;
    const segId = hostByMac.get(edge.hostA)?.segmentId;
    segByID.get(segId)?.edges.push(edge);
  }

  // ── Counts for the header strip ────────────────────────────────────────
  const peerHops = hosts
    .filter((h) => !h.isSelf && h.hops < HOPS_UNKNOWN)
    .map((h) => h.hops);
  const hopsMax = peerHops.length > 0 ? Math.max(...peerHops) : 0;
  const clientTotal = hosts.reduce((acc, h) => acc + (h.clientCount || 0), 0);

  return {
    self: selfHost,
    hosts,
    segments,
    blosEdges,
    counts: {
      hosts: hosts.length,
      segments: segments.length,
      links: rfLinkCount,
      blosLinks: blosEdges.length,
      clients: clientTotal,
      hopsMax,
    },
    algorithm: topology.algorithm || '',
  };
}

// hostInterfacesForSegment returns a single segment-derived badge so
// peers carry a consistent visual cue even though the mesh-wide feed
// doesn't report per-peer interface names.
function hostInterfacesForSegment(segmentId) {
  if (segmentId === 'remote') {
    return [{ name: 'vxlan0', role: 'blos' }];
  }
  return [{ name: 'mesh', role: 'rf' }];
}
