// =============================================================================
// topologyGraph.test.js — Unit tests for buildTopologyView()
// =============================================================================
// These are pure JS transform tests — no DOM, no React. The component-side
// rendering tests live in TopologyMap.test.jsx.

import { describe, it, expect } from 'vitest';
import { buildTopologyView, shortMac } from '../../components/topologyGraph.js';

// ── Fixture helpers ─────────────────────────────────────────────────────────

// Two hosts sharing a single wlan0↔wlan0 RF link. Alpha is "self" (its
// edge carries signal); Bravo's reverse edge lacks signal.
function simplePair() {
  return {
    nodes: [
      {
        primaryMac: '00:00:00:00:00:01',
        primaryHostname: 'alpha_bat0',
        secondaryMacs: ['00:00:00:00:aa:01'],
        neighbors: [
          {
            routerMac: '00:00:00:00:aa:01', routerHostname: 'alpha_wlan0',
            neighborMac: '00:00:00:00:aa:02', neighborHostname: 'bravo_wlan0',
            metric: 1.02, signal: -50, signalAverage: -52,
          },
        ],
        clients: [{ mac: 'ff:ff:ff:00:00:01', hostname: 'laptop_wlan0' }],
      },
      {
        primaryMac: '00:00:00:00:00:02',
        primaryHostname: 'bravo_bat0',
        secondaryMacs: ['00:00:00:00:aa:02'],
        neighbors: [
          {
            routerMac: '00:00:00:00:aa:02', routerHostname: 'bravo_wlan0',
            neighborMac: '00:00:00:00:aa:01', neighborHostname: 'alpha_wlan0',
            metric: 1.05, signal: 0, signalAverage: 0,
          },
        ],
        clients: [],
      },
    ],
  };
}

// Two hosts linked on BOTH wlan0↔wlan0 and phy2-mesh0↔phy2-mesh0 — the
// aggregate edge should carry two contributors, with bestSignal being the
// stronger (closer-to-zero) of the two dBm readings.
function dualRadioPair() {
  return {
    nodes: [
      {
        primaryMac: '00:00:00:00:00:01',
        primaryHostname: 'alpha_bat0',
        secondaryMacs: ['00:00:00:00:aa:01', '00:00:00:00:bb:01'],
        neighbors: [
          {
            routerMac: '00:00:00:00:aa:01', routerHostname: 'alpha_wlan0',
            neighborMac: '00:00:00:00:aa:02', neighborHostname: 'bravo_wlan0',
            metric: 1.02, signal: -50, signalAverage: -52,
          },
          {
            routerMac: '00:00:00:00:bb:01', routerHostname: 'alpha_phy2-mesh0',
            neighborMac: '00:00:00:00:bb:02', neighborHostname: 'bravo_phy2-mesh0',
            metric: 1.08, signal: -62, signalAverage: -64,
          },
        ],
        clients: [],
      },
      {
        primaryMac: '00:00:00:00:00:02',
        primaryHostname: 'bravo_bat0',
        secondaryMacs: ['00:00:00:00:aa:02', '00:00:00:00:bb:02'],
        neighbors: [],
        clients: [],
      },
    ],
  };
}

// Two mesh segments bridged by a vxlan0 link between alpha and gamma.
// Segment A (alpha ↔ bravo RF), Segment B (gamma ↔ delta RF),
// BLOS edge alpha↔gamma over vxlan0 should produce blosEdges length 1
// and segments length 2.
function twoSegmentBLOS() {
  return {
    nodes: [
      {
        primaryMac: '00:00:00:00:00:01',
        primaryHostname: 'alpha_bat0',
        secondaryMacs: ['00:00:00:00:aa:01', '00:00:00:00:cc:01'],
        neighbors: [
          // RF to bravo
          {
            routerMac: '00:00:00:00:aa:01', routerHostname: 'alpha_wlan0',
            neighborMac: '00:00:00:00:aa:02', neighborHostname: 'bravo_wlan0',
            metric: 1.02, signal: -55, signalAverage: -55,
          },
          // BLOS to gamma via vxlan0
          {
            routerMac: '00:00:00:00:cc:01', routerHostname: 'alpha_vxlan0',
            neighborMac: '00:00:00:00:cc:03', neighborHostname: 'gamma_vxlan0',
            metric: 1.20, signal: 0, signalAverage: 0,
          },
        ],
        clients: [],
      },
      {
        primaryMac: '00:00:00:00:00:02',
        primaryHostname: 'bravo_bat0',
        secondaryMacs: ['00:00:00:00:aa:02'],
        neighbors: [],
        clients: [],
      },
      {
        primaryMac: '00:00:00:00:00:03',
        primaryHostname: 'gamma_bat0',
        secondaryMacs: ['00:00:00:00:aa:03', '00:00:00:00:cc:03'],
        neighbors: [
          {
            routerMac: '00:00:00:00:aa:03', routerHostname: 'gamma_wlan0',
            neighborMac: '00:00:00:00:aa:04', neighborHostname: 'delta_wlan0',
            metric: 1.10, signal: -60, signalAverage: -60,
          },
        ],
        clients: [],
      },
      {
        primaryMac: '00:00:00:00:00:04',
        primaryHostname: 'delta_bat0',
        secondaryMacs: ['00:00:00:00:aa:04'],
        neighbors: [],
        clients: [],
      },
    ],
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
        hosts: 0, segments: 0, links: 0, blosLinks: 0,
        degraded: 0, clients: 0, hopsMax: 0,
      });
    }
  });
});

// ── Host grouping ───────────────────────────────────────────────────────────

describe('TestHostGrouping', () => {
  it('collapses _bat0 / _wlan0 / _phy2-mesh0 onto one host record', () => {
    const v = buildTopologyView(dualRadioPair());
    expect(v.hosts).toHaveLength(2);
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    expect(alpha).toBeDefined();
    const ifaceNames = alpha.interfaces.map((i) => i.name).sort();
    expect(ifaceNames).toEqual(['bat0', 'phy2-mesh0', 'wlan0']);
  });

  it('classifies vxlan0 interfaces as role=blos, others as role=rf', () => {
    const v = buildTopologyView(twoSegmentBLOS());
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    const byName = Object.fromEntries(alpha.interfaces.map((i) => [i.name, i.role]));
    expect(byName.vxlan0).toBe('blos');
    expect(byName.wlan0).toBe('rf');
    expect(byName.bat0).toBe('rf');
  });

  it('marks self using the local-signal heuristic', () => {
    const v = buildTopologyView(simplePair());
    expect(v.self).not.toBeNull();
    expect(v.self.baseHostname).toBe('alpha');
    expect(v.self.isSelf).toBe(true);
  });

  it('falls back gracefully when router_hostname is missing', () => {
    // Omit routerHostname; frontend should reverse-lookup via neighborHostname
    // entries seen elsewhere. alpha's wlan0 MAC appears as bravo's neighbor.
    const topo = simplePair();
    topo.nodes[0].neighbors[0].routerHostname = '';
    topo.nodes[1].neighbors = [{
      routerMac: '00:00:00:00:aa:02', routerHostname: 'bravo_wlan0',
      neighborMac: '00:00:00:00:aa:01', neighborHostname: 'alpha_wlan0',
      metric: 1.05, signal: 0, signalAverage: 0,
    }];
    const v = buildTopologyView(topo);
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    // alpha_wlan0 still gets added via the neighbor-side hostname on bravo.
    expect(alpha.interfaces.some((i) => i.name === 'wlan0')).toBe(true);
  });
});

// ── Aggregate edges ─────────────────────────────────────────────────────────

describe('TestAggregateEdges', () => {
  it('aggregates two interface-pair edges between the same hosts into one edge', () => {
    const v = buildTopologyView(dualRadioPair());
    // Only one segment, one edge (aggregate over two contributors)
    expect(v.segments).toHaveLength(1);
    expect(v.segments[0].edges).toHaveLength(1);
    const edge = v.segments[0].edges[0];
    expect(edge.contributors).toHaveLength(2);
    // bestSignal picks the strongest (closer to 0) of -50 and -62.
    expect(edge.bestSignal).toBe(-50);
    // bestMetric picks the lowest non-zero TQ (1.02 vs 1.08).
    expect(edge.bestMetric).toBeCloseTo(1.02, 3);
    expect(edge.blos).toBe(false);
  });

  it('flags an edge as weak when any contributor crosses the thresholds', () => {
    const topo = dualRadioPair();
    // Bump one contributor into weak territory.
    topo.nodes[0].neighbors[1].metric = 3.0;
    topo.nodes[0].neighbors[1].signal = -85;
    const v = buildTopologyView(topo);
    const edge = v.segments[0].edges[0];
    expect(edge.weak).toBe(true);
    // Both endpoint hosts inherit the degraded flag.
    expect(v.hosts.every((h) => h.degraded)).toBe(true);
  });

  it('dedupes bidirectional samples, preferring the one carrying signal', () => {
    const v = buildTopologyView(simplePair());
    expect(v.segments[0].edges).toHaveLength(1);
    const edge = v.segments[0].edges[0];
    expect(edge.contributors).toHaveLength(1);
    expect(edge.contributors[0].signal).toBe(-50);
  });
});

// ── BLOS + segments ─────────────────────────────────────────────────────────

describe('TestSegmentsAndBLOS', () => {
  it('splits RF-connected components into separate segments with stable letters', () => {
    const v = buildTopologyView(twoSegmentBLOS());
    expect(v.segments).toHaveLength(2);
    expect(v.segments.map((s) => s.id)).toEqual(['A', 'B']);
    // Segment A contains alpha+bravo (smallest hostname is "alpha").
    const segA = v.segments[0];
    const aHosts = segA.hosts.map((h) => h.baseHostname).sort();
    expect(aHosts).toEqual(['alpha', 'bravo']);
    // Segment B contains gamma+delta.
    const segB = v.segments[1];
    const bHosts = segB.hosts.map((h) => h.baseHostname).sort();
    expect(bHosts).toEqual(['delta', 'gamma']);
  });

  it('classifies a vxlan0 edge as a BLOS bridge, not an RF edge', () => {
    const v = buildTopologyView(twoSegmentBLOS());
    expect(v.blosEdges).toHaveLength(1);
    const br = v.blosEdges[0];
    expect(br.blos).toBe(true);
    // The two endpoint hosts live in different segments.
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    const gamma = v.hosts.find((h) => h.baseHostname === 'gamma');
    expect(alpha.segmentId).not.toBe(gamma.segmentId);
    expect([br.hostA, br.hostB].sort()).toEqual(['alpha', 'gamma']);
  });

  it('counts BLOS links separately from RF links', () => {
    const v = buildTopologyView(twoSegmentBLOS());
    expect(v.counts.blosLinks).toBe(1);
    expect(v.counts.links).toBe(2);          // alpha–bravo, gamma–delta
    expect(v.counts.segments).toBe(2);
    expect(v.counts.hosts).toBe(4);
  });

  it('single-segment topology yields segments.length === 1 and no BLOS edges', () => {
    const v = buildTopologyView(simplePair());
    expect(v.segments).toHaveLength(1);
    expect(v.segments[0].id).toBe('A');
    expect(v.blosEdges).toEqual([]);
    expect(v.counts.blosLinks).toBe(0);
  });
});

// ── Clients + tags ──────────────────────────────────────────────────────────

describe('TestClientsAndTags', () => {
  it('attaches clients to their owning base host with C# tags', () => {
    const v = buildTopologyView(simplePair());
    const alpha = v.hosts.find((h) => h.baseHostname === 'alpha');
    expect(alpha.clients).toHaveLength(1);
    expect(alpha.clients[0].tag).toBe('C1');
    expect(alpha.clients[0].mac).toBe('ff:ff:ff:00:00:01');
    expect(v.counts.clients).toBe(1);
  });

  it('assigns stable N-NN tags in (hops asc, host asc) order', () => {
    const v = buildTopologyView(twoSegmentBLOS());
    const tags = v.hosts.map((h) => h.tag);
    expect(new Set(tags).size).toBe(tags.length);
    for (const t of tags) expect(t).toMatch(/^N-\d{2}$/);
    // Self (alpha) has hops 0 → must carry N-01.
    expect(v.self.tag).toBe('N-01');
  });
});

// ── shortMac ────────────────────────────────────────────────────────────────

describe('TestShortMac', () => {
  it('returns the last three MAC octets', () => {
    expect(shortMac('aa:bb:cc:dd:ee:ff')).toBe('dd:ee:ff');
    expect(shortMac('')).toBe('?');
  });
});
