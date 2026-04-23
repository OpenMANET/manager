// =============================================================================
// topologyGraph.test.js — Unit tests for buildTopologyView()
// =============================================================================
// Pure JS transform — no DOM, no React. Component-side rendering lives in
// TopologyMap.test.jsx. Fixtures mirror what meshApi.fetchMeshTopology() is
// expected to produce from the originator-based wire format.

import { describe, it, expect } from 'vitest';
import {
  buildTopologyView,
  shortHostname,
} from '../../components/topologyGraph.js';

// ── Fixture helpers ─────────────────────────────────────────────────────────

// One direct RF neighbor. Self routes to alpha via wlan0, 1 hop.
function directRF() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_IV',
    originators: [
      {
        origMac: 'aa:aa:aa:aa:aa:01', origHostname: 'alpha_wlan0',
        nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: 'alpha_wlan0',
        hardIfname: 'wlan0', tq: 255, throughput: 0, lastSeenMs: 100, hops: 1,
      },
    ],
  };
}

// Two-hop RF chain. Self → alpha (direct) → bravo (multi-hop via alpha).
function multiHopRF() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_IV',
    originators: [
      {
        origMac: 'aa:aa:aa:aa:aa:01', origHostname: 'alpha_wlan0',
        nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: 'alpha_wlan0',
        hardIfname: 'wlan0', tq: 255, throughput: 0, lastSeenMs: 100, hops: 1,
      },
      {
        origMac: 'bb:bb:bb:bb:bb:01', origHostname: 'bravo_wlan0',
        nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: 'alpha_wlan0',
        hardIfname: 'wlan0', tq: 210, throughput: 0, lastSeenMs: 240, hops: 2,
      },
    ],
  };
}

// One direct BLOS neighbor — creates one remote segment.
function oneBLOS() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_IV',
    originators: [
      {
        origMac: 'cc:cc:cc:cc:cc:01', origHostname: 'gw1_vxlan0',
        nextHopMac: 'cc:cc:cc:cc:cc:01', nextHopHostname: 'gw1_vxlan0',
        hardIfname: 'vxlan0', tq: 230, throughput: 0, lastSeenMs: 400, hops: 1,
      },
    ],
  };
}

// Two direct BLOS peers plus a multi-hop peer behind one of them. All three
// share the same local vxlan0 interface so the partition collapses them into
// ONE remote segment.
function twoBLOSPeersOneInterface() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_IV',
    originators: [
      // Direct RF neighbor (local segment)
      {
        origMac: 'aa:aa:aa:aa:aa:01', origHostname: 'alpha_wlan0',
        nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: 'alpha_wlan0',
        hardIfname: 'wlan0', tq: 255, hops: 1,
      },
      // Gateway 1 (direct BLOS neighbor)
      {
        origMac: 'cc:cc:cc:cc:cc:01', origHostname: 'gw1_vxlan0',
        nextHopMac: 'cc:cc:cc:cc:cc:01', nextHopHostname: 'gw1_vxlan0',
        hardIfname: 'vxlan0', tq: 230, hops: 1,
      },
      // Multi-hop peer behind gateway 1 — same segment as gw1.
      {
        origMac: 'cc:cc:cc:cc:cc:99', origHostname: 'remote1_vxlan0',
        nextHopMac: 'cc:cc:cc:cc:cc:01', nextHopHostname: 'gw1_vxlan0',
        hardIfname: 'vxlan0', tq: 200, hops: 2,
      },
      // Gateway 2 (distinct direct BLOS neighbor) — its own remote segment.
      {
        origMac: 'dd:dd:dd:dd:dd:01', origHostname: 'gw2_vxlan0',
        nextHopMac: 'dd:dd:dd:dd:dd:01', nextHopHostname: 'gw2_vxlan0',
        hardIfname: 'vxlan0', tq: 210, hops: 1,
      },
    ],
  };
}

// Self is itself a BLOS gateway: every reachable peer is a direct vxlan0
// neighbor, no RF peers, no multi-hop. Mirrors the scenario in the user's
// screenshot where the old gateway-keyed partition painted N remote boxes;
// the new interface-keyed partition collapses them into ONE.
function selfIsGatewayManyDirectPeers() {
  const originators = [];
  for (let i = 1; i <= 5; i += 1) {
    const mac = `cc:cc:cc:cc:cc:0${i}`;
    const host = `peer${i}_vxlan0`;
    originators.push({
      origMac: mac, origHostname: host,
      nextHopMac: mac, nextHopHostname: host,
      hardIfname: 'vxlan0', tq: 200 + i, hops: 1,
    });
  }
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_IV',
    originators,
  };
}

// A host reachable via BOTH an RF route and a BLOS route: the non-BLOS
// route wins and the host lands in LOCAL.
function rfAndBLOSToSameHost() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_IV',
    originators: [
      {
        origMac: 'ee:ee:ee:ee:ee:01', origHostname: 'echo_wlan0',
        nextHopMac: 'ee:ee:ee:ee:ee:01', nextHopHostname: 'echo_wlan0',
        hardIfname: 'wlan0', tq: 255, hops: 1,
      },
      // Same host's bat0 MAC also shows up reachable over BLOS — if the
      // frontend treated BLOS as dominant, it would wrongly move echo into
      // a remote segment. The expected outcome: echo stays in LOCAL.
      {
        origMac: 'ee:ee:ee:ee:ee:02', origHostname: 'echo_vxlan0',
        nextHopMac: 'cc:cc:cc:cc:cc:01', nextHopHostname: 'gw1_vxlan0',
        hardIfname: 'vxlan0', tq: 120, hops: 2,
      },
    ],
  };
}

// ── Empty / degenerate ──────────────────────────────────────────────────────

describe('TestBuildTopologyViewEmpty', () => {
  it('returns a zeroed view for null / undefined / empty inputs', () => {
    for (const input of [null, undefined, {}, { originators: [] }]) {
      const v = buildTopologyView(input);
      expect(v.self).toBeNull();
      expect(v.hosts).toEqual([]);
      expect(v.segments).toEqual([]);
      expect(v.blosEdges).toEqual([]);
      expect(v.counts).toEqual({
        hosts: 0, segments: 0, links: 0, blosLinks: 0, clients: 0, hopsMax: 0,
      });
    }
  });

  it('still renders the self node when the mesh has no peers yet', () => {
    const v = buildTopologyView({
      selfMac: '00:00:00:00:00:00',
      selfHostname: 'lonely',
      algorithm: '',
      originators: [],
    });
    expect(v.self).not.toBeNull();
    expect(v.self.baseHostname).toBe('lonely');
    expect(v.hosts).toHaveLength(1);
    expect(v.segments).toHaveLength(1);
    expect(v.segments[0].kind).toBe('local');
  });
});

// ── Host grouping ───────────────────────────────────────────────────────────

describe('TestHostGrouping', () => {
  it('collapses _wlan0 and the local hard_ifname onto one self host record', () => {
    const v = buildTopologyView(directRF());
    expect(v.self).not.toBeNull();
    // Self picks up wlan0 from the hard_ifname of the one best-route row.
    const ifaceNames = v.self.interfaces.map((i) => i.name);
    expect(ifaceNames).toContain('wlan0');
  });

  it('classifies vxlan0 interfaces as role=blos, others as role=rf', () => {
    const v = buildTopologyView(oneBLOS());
    const gw = v.hosts.find((h) => h.baseHostname === 'gw1');
    const byName = Object.fromEntries(gw.interfaces.map((i) => [i.name, i.role]));
    expect(byName.vxlan0).toBe('blos');
    const self = v.self;
    const selfByName = Object.fromEntries(self.interfaces.map((i) => [i.name, i.role]));
    expect(selfByName.vxlan0).toBe('blos');
  });

  it('marks the self host via selfHostname / selfMac', () => {
    const v = buildTopologyView(multiHopRF());
    expect(v.self.isSelf).toBe(true);
    expect(v.self.baseHostname).toBe('me');
  });
});

// ── Hops + aggregate edges ─────────────────────────────────────────────────

describe('TestAggregateEdgesAndHops', () => {
  it('direct RF peer forms a (self, peer) edge carrying the TQ', () => {
    const v = buildTopologyView(directRF());
    expect(v.hosts).toHaveLength(2);
    expect(v.segments[0].edges).toHaveLength(1);
    const edge = v.segments[0].edges[0];
    expect(edge.blos).toBe(false);
    expect(edge.bestTQ).toBe(255);
    expect(edge.contributors).toHaveLength(1);
    expect(edge.contributors[0].srcIface).toBe('wlan0');
  });

  it('multi-hop peer edges anchor to the next-hop host, not self', () => {
    const v = buildTopologyView(multiHopRF());
    expect(v.segments[0].edges.length).toBeGreaterThanOrEqual(2);
    const bravo = v.hosts.find((h) => h.baseHostname === 'bravo');
    // bravo appears in exactly one edge, and that edge's other endpoint is
    // alpha (its next hop), not the self host.
    const edges = v.segments[0].edges.filter(
      (e) => e.hostA === bravo.id || e.hostB === bravo.id,
    );
    expect(edges).toHaveLength(1);
    const peerKey = edges[0].hostA === bravo.id ? edges[0].hostB : edges[0].hostA;
    expect(peerKey).toBe('alpha');
  });

  it('hops come directly from the proto', () => {
    const v = buildTopologyView(multiHopRF());
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    const bravo = v.hosts.find((h) => h.baseHostname === 'bravo');
    expect(alpha.hops).toBe(1);
    expect(bravo.hops).toBe(2);
  });
});

// ── BLOS + segments ─────────────────────────────────────────────────────────

describe('TestSegmentsAndBLOS', () => {
  it('single direct BLOS neighbor produces LOCAL + one REMOTE MESH segment', () => {
    const v = buildTopologyView(oneBLOS());
    expect(v.segments).toHaveLength(2);
    const [local, remote] = v.segments;
    expect(local.kind).toBe('local');
    expect(local.label).toBe('LOCAL');
    expect(remote.kind).toBe('remote');
    expect(remote.label).toBe('REMOTE MESH');
    expect(remote.id).toBe('remote:vxlan0');
    const remoteHosts = remote.hosts.map((h) => h.baseHostname);
    expect(remoteHosts).toEqual(['gw1']);
    expect(remote.anchorHost).toBe('gw1');
  });

  it('peers sharing one local BLOS interface collapse into one segment', () => {
    const v = buildTopologyView(twoBLOSPeersOneInterface());
    const remoteSegs = v.segments.filter((s) => s.kind === 'remote');
    expect(remoteSegs).toHaveLength(1);
    expect(remoteSegs[0].label).toBe('REMOTE MESH');
    // gw1, gw2, and remote1 all share the segment.
    const remoteHosts = remoteSegs[0].hosts
      .map((h) => h.baseHostname)
      .sort();
    expect(remoteHosts).toEqual(['gw1', 'gw2', 'remote1']);
  });

  it('self-as-gateway: N direct vxlan0 peers collapse to ONE remote segment', () => {
    const v = buildTopologyView(selfIsGatewayManyDirectPeers());
    // Self is the only local host; 5 peers live in a single remote segment.
    const localSeg = v.segments.find((s) => s.kind === 'local');
    const remoteSegs = v.segments.filter((s) => s.kind === 'remote');
    expect(localSeg.hosts.map((h) => h.baseHostname)).toEqual(['me']);
    expect(remoteSegs).toHaveLength(1);
    expect(remoteSegs[0].hosts).toHaveLength(5);
    expect(remoteSegs[0].label).toBe('REMOTE MESH');
    // Anchor is chosen by ascending base-hostname among direct neighbors —
    // all 5 are hops=1, so peer1 wins.
    expect(remoteSegs[0].anchorHost).toBe('peer1');
    // Each peer yields a separate BLOS edge from self.
    expect(v.blosEdges).toHaveLength(5);
  });

  it('multi-hop BLOS peer joins the same segment as its gateway', () => {
    const v = buildTopologyView(twoBLOSPeersOneInterface());
    const remote1 = v.hosts.find((h) => h.baseHostname === 'remote1');
    const gw1 = v.hosts.find((h) => h.baseHostname === 'gw1');
    expect(remote1.segmentId).toBe(gw1.segmentId);
    const seg = v.segments.find((s) => s.id === remote1.segmentId);
    // Anchor is the direct neighbor with the lowest base hostname — gw1.
    expect(seg.anchorHost).toBe('gw1');
  });

  it('host reachable by BOTH RF and BLOS lands in LOCAL', () => {
    const v = buildTopologyView(rfAndBLOSToSameHost());
    const echo = v.hosts.find((h) => h.baseHostname === 'echo');
    const localSeg = v.segments.find((s) => s.kind === 'local');
    expect(echo.segmentId).toBe(localSeg.id);
    // BLOS edge still surfaces — it just doesn't move the host.
    expect(v.blosEdges.length).toBeGreaterThan(0);
  });

  it('no vxlan0 originators → one local segment, no BLOS edges', () => {
    const v = buildTopologyView(multiHopRF());
    expect(v.segments).toHaveLength(1);
    expect(v.segments[0].kind).toBe('local');
    expect(v.blosEdges).toEqual([]);
    expect(v.counts.blosLinks).toBe(0);
  });

  it('counts BLOS links separately from RF links', () => {
    const v = buildTopologyView(twoBLOSPeersOneInterface());
    // Self↔alpha is one RF edge. Everything else (self↔gw1, gw1↔remote1,
    // self↔gw2) lives on vxlan0 and counts as BLOS.
    expect(v.counts.links).toBe(1);
    expect(v.counts.blosLinks).toBeGreaterThanOrEqual(2);
  });
});

// ── Bat-hosts degradation ──────────────────────────────────────────────────

describe('TestBatHostsMissing', () => {
  it('renders hosts keyed by MAC when hostnames are empty', () => {
    const v = buildTopologyView({
      selfMac: '00:00:00:00:00:00',
      selfHostname: '',
      algorithm: 'BATMAN_IV',
      originators: [
        {
          origMac: 'aa:aa:aa:aa:aa:01', origHostname: '',
          nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: '',
          hardIfname: 'wlan0', tq: 200, hops: 1,
        },
      ],
    });
    // Two hosts — self (fallback to MAC) and the MAC-only peer.
    expect(v.hosts).toHaveLength(2);
    expect(v.hosts.every((h) => h.baseHostname)).toBe(true);
  });
});

// ── Helpers ────────────────────────────────────────────────────────────────

describe('TestShortHelpers', () => {
  it('shortHostname returns the final dash segment, capped at 6 chars', () => {
    expect(shortHostname('BCM2711-97d6')).toBe('97d6');
    expect(shortHostname('alpha')).toBe('alpha');
    expect(shortHostname('node-ABCDEFGH')).toBe('ABCDEF');
    expect(shortHostname('')).toBe('?');
  });
});
