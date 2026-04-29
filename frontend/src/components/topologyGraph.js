// =============================================================================
// topologyGraph.js — Transform MeshTopology (mesh-wide graph) → render view
// =============================================================================
// Input is the server-merged mesh graph from batadv-vis + this node's
// originator overlay:
//
//   {
//     selfMac, selfHostname, algorithm, collectedAt,
//     nodes: [{ mac, secondaryMacs, hostname, segment,
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

// formatAge renders a non-negative second count as a compact duration
// ("8s" / "2m 14s" / "1h 03m"). Used by the topology UI to display
// gossip record freshness on both host labels and the inspector panel.
// Negative or non-finite inputs return the em dash placeholder.
export function formatAge(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  if (m < 60) return `${m}m ${String(s).padStart(2, '0')}s`;
  const h = Math.floor(m / 60);
  const mm = m % 60;
  return `${h}h ${String(mm).padStart(2, '0')}m`;
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
    counts: { hosts: 0, segments: 0, links: 0, blosLinks: 0, hopsMax: 0 },
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
    const remoteGatewayMac = (n.remoteGatewayMac || '').toLowerCase();
    // Each distinct BLOS gateway forms its own REMOTE MESH segment —
    // operators want to see each tunnel-attached mesh as a separate
    // radial cluster, not a single mixed blob. Local hosts share
    // "local".
    const segmentId = n.segment === 'remote'
      ? (remoteGatewayMac ? `remote:${remoteGatewayMac}` : 'remote:unknown')
      : 'local';

    const host = {
      id: key,
      mac: n.mac,
      baseHostname: base,
      primaryMac: n.mac,
      tag: '',
      interfaces: hostInterfacesForKind(n.segment === 'remote' ? 'remote' : 'local'),
      segmentId,
      remoteGatewayMac,
      hops: Number.isFinite(n.hopsFromSelf) ? n.hopsFromSelf : HOPS_UNKNOWN,
      isSelf: Boolean(n.isSelf),
      isGateway: Boolean(n.isGateway),
      myHardIfname: n.myHardIfname || '',
      secondaryMacs: n.secondaryMacs || [],
      gossipStale: Boolean(n.gossipStale),
      // gossipAgeSeconds: -1 means "no record observed"; any other value
      // is seconds since the publisher's payload.collected_at. Self is 0.
      gossipAgeSeconds: Number.isFinite(n.gossipAgeSeconds) ? n.gossipAgeSeconds : -1,
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
  // LOCAL holds every local-segment host; one REMOTE MESH segment per
  // distinct BLOS gateway. Gateway nodes sit at the root of their own
  // radial layout; the vxlan0 tunnel renders as a BLOS edge between
  // the local-side gateway (self) and the remote gateway.
  const segments = [];
  const hasLocalHost = [...hostByMac.values()].some((h) => h.segmentId === 'local');

  if (hasLocalHost) {
    segments.push({
      id: 'local',
      label: 'LOCAL',
      kind: 'local',
      anchorHost: null,
      hosts: [],
      edges: [],
    });
  }

  // Collect distinct remote segment ids in deterministic order (by
  // gateway's base hostname when available, else by the MAC suffix).
  const remoteGatewayHosts = new Map(); // segmentId → gatewayHost (may be null)
  for (const h of hostByMac.values()) {
    if (h.segmentId === 'local') continue;
    if (!remoteGatewayHosts.has(h.segmentId)) {
      const gw = hostByMac.get(h.remoteGatewayMac) || null;
      remoteGatewayHosts.set(h.segmentId, gw);
    }
  }

  const remoteSegIds = [...remoteGatewayHosts.keys()].sort((a, b) => {
    const ga = remoteGatewayHosts.get(a);
    const gb = remoteGatewayHosts.get(b);
    const na = ga?.baseHostname || a;
    const nb = gb?.baseHostname || b;
    return na.localeCompare(nb);
  });

  for (const segId of remoteSegIds) {
    const gw = remoteGatewayHosts.get(segId);
    const label = gw
      ? `REMOTE MESH · ${gw.baseHostname}`
      : `REMOTE MESH · ${shortMac(segId.slice('remote:'.length))}`;
    segments.push({
      id: segId,
      label,
      kind: 'remote',
      anchorHost: gw ? gw.id : null,
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

  // Anchor each remote segment on its gateway host — that's the node
  // terminating the vxlan0 tunnel and is the natural root of the radial
  // layout. The backend sets anchorHost from remoteGatewayMac; fall back
  // to the host with the smallest hops if that gateway wasn't itself
  // rendered (edge case where the chain walk surfaced an unlisted MAC).
  for (const seg of segments) {
    if (seg.kind !== 'remote' || seg.hosts.length === 0) continue;
    if (seg.anchorHost && seg.hosts.some((h) => h.id === seg.anchorHost)) continue;
    const minHops = Math.min(...seg.hosts.map((h) => h.hops));
    const candidates = seg.hosts
      .filter((h) => h.hops === minHops)
      .sort((a, b) => a.baseHostname.localeCompare(b.baseHostname));
    seg.anchorHost = (candidates[0] || seg.hosts[0]).id;
  }

  // ── Edges ──────────────────────────────────────────────────────────────
  // BLOS edges are any edge crossing a segment boundary — the backend
  // sets blos when the coarse local/remote segments differ, but the
  // frontend splits remote into per-gateway sub-segments, so an edge
  // between two distinct remote gateways (a gateway-to-gateway vxlan0
  // tunnel) also needs to render as BLOS even though the backend left
  // its blos flag false.
  const edges = (topology.edges || [])
    .map((e) => {
      const a = (e.fromMac || '').toLowerCase();
      const b = (e.toMac || '').toLowerCase();
      if (!a || !b || a === b) return null;
      const ha = hostByMac.get(a);
      const hb = hostByMac.get(b);
      if (!ha || !hb) return null;

      const crossSegment = ha.segmentId !== hb.segmentId;

      return {
        id: `edge:${a}|${b}`,
        hostA: a,
        hostB: b,
        metric: e.metric || 0,
        blos: Boolean(e.blos) || crossSegment,
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
      hopsMax,
    },
    algorithm: topology.algorithm || '',
    gossipCoverage: topology.gossipCoverage
      ? {
          published: Number(topology.gossipCoverage.published) || 0,
          total: Number(topology.gossipCoverage.total) || 0,
        }
      : null,
  };
}

// hostInterfacesForKind returns a single segment-derived badge so
// peers carry a consistent visual cue even though the mesh-wide feed
// doesn't report per-peer interface names.
function hostInterfacesForKind(kind) {
  if (kind === 'remote') {
    return [{ name: 'vxlan0', role: 'blos' }];
  }
  return [{ name: 'mesh', role: 'rf' }];
}
