// =============================================================================
// topologyGraph.test.js — Unit tests for buildTopologyView()
// =============================================================================
// Pure JS transform — no DOM, no React. Component-side rendering lives in
// TopologyMap.test.jsx. Fixtures mirror what meshApi.fetchMeshTopology() is
// expected to produce from the originator-based wire format.

import { describe, it, expect } from 'vitest';
import {
  buildTopologyView,
  shortMac,
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

// Two BLOS gateways, each with a multi-hop peer behind it. Exercises the
// gateway-anchored remote-segment partition.
function twoBLOSGateways() {
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
  it('single direct BLOS neighbor produces LOCAL + one REMOTE segment', () => {
    const v = buildTopologyView(oneBLOS());
    expect(v.segments).toHaveLength(2);
    const [local, remote] = v.segments;
    expect(local.kind).toBe('local');
    expect(local.label).toBe('LOCAL');
    expect(remote.kind).toBe('remote');
    expect(remote.label).toBe('REMOTE · gw1');
    const remoteHosts = remote.hosts.map((h) => h.baseHostname);
    expect(remoteHosts).toEqual(['gw1']);
  });

  it('two distinct BLOS gateways → two distinct remote segments', () => {
    const v = buildTopologyView(twoBLOSGateways());
    const remoteSegs = v.segments.filter((s) => s.kind === 'remote');
    expect(remoteSegs).toHaveLength(2);
    expect(remoteSegs.map((s) => s.label).sort()).toEqual(
      ['REMOTE · gw1', 'REMOTE · gw2'],
    );
  });

  it('multi-hop BLOS peer stays in its gateway’s segment (not its own)', () => {
    const v = buildTopologyView(twoBLOSGateways());
    const remote1 = v.hosts.find((h) => h.baseHostname === 'remote1');
    const gw1 = v.hosts.find((h) => h.baseHostname === 'gw1');
    expect(remote1.segmentId).toBe(gw1.segmentId);
    const gw1Seg = v.segments.find((s) => s.id === remote1.segmentId);
    expect(gw1Seg.hosts.map((h) => h.baseHostname).sort()).toEqual(['gw1', 'remote1']);
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
    const v = buildTopologyView(twoBLOSGateways());
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
  it('shortMac returns the last three MAC octets', () => {
    expect(shortMac('aa:bb:cc:dd:ee:ff')).toBe('dd:ee:ff');
    expect(shortMac('')).toBe('?');
  });

  it('shortHostname returns the final dash segment, capped at 6 chars', () => {
    expect(shortHostname('BCM2711-97d6')).toBe('97d6');
    expect(shortHostname('alpha')).toBe('alpha');
    expect(shortHostname('node-ABCDEFGH')).toBe('ABCDEF');
    expect(shortHostname('')).toBe('?');
  });
});
