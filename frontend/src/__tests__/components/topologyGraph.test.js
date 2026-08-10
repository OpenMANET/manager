// =============================================================================
// topologyGraph.test.js — Unit tests for buildTopologyView()
// =============================================================================
// Pure JS transform — no DOM, no React. Component-side rendering lives in
// TopologyMap.test.jsx. Fixtures mirror what meshApi.fetchMeshTopology()
// produces from the mesh-wide vis + overlay wire format.

import { describe, it, expect } from 'vitest';
import {
  buildTopologyView,
  formatAge,
  shortHostname,
} from '../../components/topologyGraph.js';

// ── Fixture helpers ─────────────────────────────────────────────────────────

// One local triangle (me, alpha, bravo fully meshed).
function localTriangle() {
  return {
    selfMac: 'aa:aa:aa:aa:aa:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_V',
    nodes: [
      {
        mac: 'aa:aa:aa:aa:aa:00', hostname: 'me', segment: 'local',
        hopsFromSelf: 0, isSelf: true, myHardIfname: '',
        secondaryMacs: [],
      },
      {
        mac: 'bb:bb:bb:bb:bb:00', hostname: 'alpha', segment: 'local',
        hopsFromSelf: 1, isSelf: false, myHardIfname: 'wlh0',
        secondaryMacs: [],
      },
      {
        mac: 'cc:cc:cc:cc:cc:00', hostname: 'bravo', segment: 'local',
        hopsFromSelf: 1, isSelf: false, myHardIfname: 'wlh0',
        secondaryMacs: [],
      },
    ],
    edges: [
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'bb:bb:bb:bb:bb:00', metric: 1.1, blos: false, onMyPath: true },
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 1.2, blos: false, onMyPath: true },
      { fromMac: 'bb:bb:bb:bb:bb:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 1.3, blos: false, onMyPath: false },
    ],
  };
}

// Local pair + remote triangle bridged by one BLOS edge. Used to test
// segment partition + BLOS edge routing + anchor selection. gw1 is the
// remote mesh's BLOS gateway; peer-e and peer-f sit behind it and carry
// remoteGatewayMac=gw1.
function localAndRemote() {
  return {
    selfMac: 'aa:aa:aa:aa:aa:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_V',
    nodes: [
      { mac: 'aa:aa:aa:aa:aa:00', hostname: 'me',     segment: 'local',  remoteGatewayMac: '',                     hopsFromSelf: 0, isSelf: true,  myHardIfname: '',       secondaryMacs: [] },
      { mac: 'bb:bb:bb:bb:bb:00', hostname: 'alpha',  segment: 'local',  remoteGatewayMac: '',                     hopsFromSelf: 1, isSelf: false, myHardIfname: 'wlh0',  secondaryMacs: [] },
      { mac: 'cc:cc:cc:cc:cc:00', hostname: 'gw1',    segment: 'remote', remoteGatewayMac: 'cc:cc:cc:cc:cc:00',    hopsFromSelf: 1, isSelf: false, myHardIfname: 'vxlan0', secondaryMacs: [] },
      { mac: 'dd:dd:dd:dd:dd:00', hostname: 'peer-e', segment: 'remote', remoteGatewayMac: 'cc:cc:cc:cc:cc:00',    hopsFromSelf: 2, isSelf: false, myHardIfname: 'vxlan0', secondaryMacs: [] },
      { mac: 'ee:ee:ee:ee:ee:00', hostname: 'peer-f', segment: 'remote', remoteGatewayMac: 'cc:cc:cc:cc:cc:00',    hopsFromSelf: 2, isSelf: false, myHardIfname: 'vxlan0', secondaryMacs: [] },
    ],
    edges: [
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'bb:bb:bb:bb:bb:00', metric: 1.1, blos: false, onMyPath: true },
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 1.5, blos: true,  onMyPath: true },
      { fromMac: 'cc:cc:cc:cc:cc:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 1.2, blos: false, onMyPath: true },
      { fromMac: 'cc:cc:cc:cc:cc:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 1.3, blos: false, onMyPath: false },
      { fromMac: 'dd:dd:dd:dd:dd:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 1.4, blos: false, onMyPath: false },
    ],
  };
}

// Two remote BLOS meshes reached through two different gateways. Used
// to verify distinct-gateway → distinct-segment partitioning.
function twoRemoteMeshes() {
  return {
    selfMac: 'aa:aa:aa:aa:aa:00',
    selfHostname: 'me',
    algorithm: 'BATMAN_V',
    nodes: [
      { mac: 'aa:aa:aa:aa:aa:00', hostname: 'me',  segment: 'local',  remoteGatewayMac: '',                    hopsFromSelf: 0, isSelf: true,  myHardIfname: '' },
      { mac: 'cc:cc:cc:cc:cc:00', hostname: 'gw1', segment: 'remote', remoteGatewayMac: 'cc:cc:cc:cc:cc:00',   hopsFromSelf: 1, isSelf: false, myHardIfname: 'vxlan0' },
      { mac: 'dd:dd:dd:dd:dd:00', hostname: 'r1a', segment: 'remote', remoteGatewayMac: 'cc:cc:cc:cc:cc:00',   hopsFromSelf: 2, isSelf: false, myHardIfname: 'vxlan0' },
      { mac: 'ee:ee:ee:ee:ee:00', hostname: 'gw2', segment: 'remote', remoteGatewayMac: 'ee:ee:ee:ee:ee:00',   hopsFromSelf: 1, isSelf: false, myHardIfname: 'vxlan0' },
      { mac: 'ff:ff:ff:ff:ff:00', hostname: 'r2a', segment: 'remote', remoteGatewayMac: 'ee:ee:ee:ee:ee:00',   hopsFromSelf: 2, isSelf: false, myHardIfname: 'vxlan0' },
    ],
    edges: [],
  };
}

// ── Empty / degenerate ──────────────────────────────────────────────────────

describe('TestBuildTopologyViewEmpty', () => {
  it('returns a zeroed view for null / undefined / empty inputs', () => {
    for (const input of [null, undefined, {}, { nodes: [] }]) {
      const v = buildTopologyView(input);
      expect(v.self).toBeNull();
      expect(v.hosts).toEqual([]);
      expect(v.segments).toEqual([]);
      expect(v.blosEdges).toEqual([]);
      expect(v.counts).toEqual({
        hosts: 0, segments: 0, links: 0, blosLinks: 0, hopsMax: 0,
      });
    }
  });
});

// ── Host record shape ───────────────────────────────────────────────────────

describe('TestHostRecords', () => {
  it('marks the self node and tags it first', () => {
    const v = buildTopologyView(localTriangle());
    expect(v.self).not.toBeNull();
    expect(v.self.baseHostname).toBe('me');
    expect(v.hosts[0].isSelf).toBe(true);
    expect(v.hosts[0].tag).toBe('N-01');
  });

  it('self.interfaces rolls up the set of my hard ifnames', () => {
    const v = buildTopologyView(localAndRemote());
    const names = v.self.interfaces.map((i) => i.name).sort();
    expect(names).toEqual(['vxlan0', 'wlh0']);
    const vxlan = v.self.interfaces.find((i) => i.name === 'vxlan0');
    expect(vxlan.role).toBe('blos');
  });

  it('peer interfaces derive a single segment-based badge', () => {
    const v = buildTopologyView(localAndRemote());
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    expect(alpha.interfaces).toHaveLength(1);
    expect(alpha.interfaces[0].role).toBe('rf');

    const peerE = v.hosts.find((h) => h.baseHostname === 'peer-e');
    expect(peerE.interfaces).toHaveLength(1);
    expect(peerE.interfaces[0].role).toBe('blos');
  });
});

// ── Segments + edges ────────────────────────────────────────────────────────

describe('TestSegmentsAndEdges', () => {
  it('local triangle produces one LOCAL segment with 3 edges', () => {
    const v = buildTopologyView(localTriangle());
    expect(v.segments).toHaveLength(1);
    const [local] = v.segments;
    expect(local.kind).toBe('local');
    expect(local.label).toBe('LOCAL');
    expect(local.hosts).toHaveLength(3);
    expect(local.edges).toHaveLength(3);
    expect(v.blosEdges).toHaveLength(0);
  });

  it('mixed topology routes edges to correct segment; BLOS edge is split off', () => {
    const v = buildTopologyView(localAndRemote());
    expect(v.segments).toHaveLength(2);

    const local = v.segments.find((s) => s.kind === 'local');
    const remote = v.segments.find((s) => s.kind === 'remote');
    expect(local.hosts.map((h) => h.baseHostname).sort()).toEqual(['alpha', 'me']);
    expect(remote.hosts.map((h) => h.baseHostname).sort()).toEqual(['gw1', 'peer-e', 'peer-f']);
    expect(remote.label).toBe('REMOTE MESH · gw1');

    // 1 local RF edge (me↔alpha), 3 remote RF edges (gw1↔e, gw1↔f, e↔f), 1 BLOS edge.
    expect(local.edges).toHaveLength(1);
    expect(remote.edges).toHaveLength(3);
    expect(v.blosEdges).toHaveLength(1);
  });

  it('distinct BLOS gateways produce distinct REMOTE MESH segments', () => {
    const v = buildTopologyView(twoRemoteMeshes());
    const remotes = v.segments.filter((s) => s.kind === 'remote');
    expect(remotes).toHaveLength(2);
    expect(remotes.map((s) => s.label).sort()).toEqual([
      'REMOTE MESH · gw1',
      'REMOTE MESH · gw2',
    ]);
    const gw1Seg = remotes.find((s) => s.label.endsWith('gw1'));
    expect(gw1Seg.hosts.map((h) => h.baseHostname).sort()).toEqual(['gw1', 'r1a']);
    expect(gw1Seg.anchorHost).toBe('cc:cc:cc:cc:cc:00');
  });

  it('counts RF vs BLOS links separately', () => {
    const v = buildTopologyView(localAndRemote());
    expect(v.counts.links).toBe(4);
    expect(v.counts.blosLinks).toBe(1);
  });

  it('remote segment anchor comes from remoteGatewayMac when present', () => {
    const v = buildTopologyView(localAndRemote());
    const remote = v.segments.find((s) => s.kind === 'remote');
    expect(remote.anchorHost).toBe('cc:cc:cc:cc:cc:00');
  });

  it('preserves onMyPath flag through the transform', () => {
    const v = buildTopologyView(localAndRemote());
    const allEdges = [...v.blosEdges, ...v.segments.flatMap((s) => s.edges)];
    const mypath = allEdges.filter((e) => e.onMyPath);
    expect(mypath).toHaveLength(3); // me↔alpha, me↔gw1 (BLOS), gw1↔peer-e
  });
});

// ── Edge filtering ──────────────────────────────────────────────────────────

describe('TestEdgeFiltering', () => {
  it('drops edges that reference unknown MACs', () => {
    const v = buildTopologyView({
      nodes: [
        { mac: 'aa:aa:aa:aa:aa:00', hostname: 'me', segment: 'local', hopsFromSelf: 0, isSelf: true },
      ],
      edges: [
        { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'dead:beef', metric: 1, blos: false, onMyPath: false },
      ],
    });
    expect(v.hosts).toHaveLength(1);
    expect(v.counts.links).toBe(0);
  });

  it('self-loop edges (fromMac == toMac) are ignored', () => {
    const v = buildTopologyView({
      nodes: [{ mac: 'aa:aa:aa:aa:aa:00', hostname: 'me', segment: 'local', hopsFromSelf: 0, isSelf: true }],
      edges: [{ fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'aa:aa:aa:aa:aa:00', metric: 1, blos: false, onMyPath: false }],
    });
    expect(v.counts.links).toBe(0);
  });
});

// ── Bat-hosts fallback ──────────────────────────────────────────────────────

describe('TestHostnameFallback', () => {
  it('uses shortMac when hostname is missing', () => {
    const v = buildTopologyView({
      nodes: [{ mac: 'aa:bb:cc:dd:ee:ff', hostname: '', segment: 'local', hopsFromSelf: 99, isSelf: false }],
      edges: [],
    });
    expect(v.hosts[0].baseHostname).toBe('dd:ee:ff');
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

  it('formatAge renders sub-minute durations in seconds', () => {
    expect(formatAge(0)).toBe('0s');
    expect(formatAge(8)).toBe('8s');
    expect(formatAge(59)).toBe('59s');
  });

  it('formatAge renders minute-scale durations with zero-padded seconds', () => {
    expect(formatAge(60)).toBe('1m 00s');
    expect(formatAge(134)).toBe('2m 14s');
    expect(formatAge(3599)).toBe('59m 59s');
  });

  it('formatAge renders hour-scale durations with zero-padded minutes', () => {
    expect(formatAge(3600)).toBe('1h 00m');
    expect(formatAge(3780)).toBe('1h 03m');
  });

  it('formatAge returns the em dash placeholder for non-finite or negative inputs', () => {
    expect(formatAge(-1)).toBe('—');
    expect(formatAge(NaN)).toBe('—');
    expect(formatAge(undefined)).toBe('—');
  });
});

describe('TestGossipAgePropagates', () => {
  it('copies gossipAgeSeconds from the input onto the host record', () => {
    const view = buildTopologyView({
      selfMac: 'aa:aa:aa:aa:aa:00',
      selfHostname: 'me',
      algorithm: 'BATMAN_V',
      nodes: [
        { mac: 'aa:aa:aa:aa:aa:00', hostname: 'me', segment: 'local',
          hopsFromSelf: 0, isSelf: true, gossipStale: false, gossipAgeSeconds: 0 },
        { mac: 'bb:bb:bb:bb:bb:00', hostname: 'fresh', segment: 'local',
          hopsFromSelf: 1, isSelf: false, gossipStale: false, gossipAgeSeconds: 8 },
        { mac: 'cc:cc:cc:cc:cc:00', hostname: 'stale', segment: 'local',
          hopsFromSelf: 1, isSelf: false, gossipStale: true, gossipAgeSeconds: 134 },
        { mac: 'dd:dd:dd:dd:dd:00', hostname: 'nogossip', segment: 'local',
          hopsFromSelf: 1, isSelf: false, gossipStale: true, gossipAgeSeconds: -1 },
      ],
      edges: [],
    });
    const byHost = {};
    for (const h of view.hosts) byHost[h.baseHostname] = h;
    expect(byHost.fresh.gossipAgeSeconds).toBe(8);
    expect(byHost.stale.gossipAgeSeconds).toBe(134);
    expect(byHost.nogossip.gossipAgeSeconds).toBe(-1);
  });
});
