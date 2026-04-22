// =============================================================================
// TopologyMap.test.jsx — Tests for mesh topology SVG renderer
// =============================================================================
// The component renders an SVG graph driven by buildTopologyView(). d3-zoom
// is mocked because jsdom's SVG coordinate APIs are not fully implemented
// and the tests don't need to assert on zoom behaviour itself.

import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';

// d3-zoom stub: chainable methods, no-op behaviour. d3-selection is real.
// The returned zoom behaviour must be *callable* so `selection.call(z)`
// resolves; d3-selection's `call` does `callback.apply(selection, args)`.
vi.mock('d3-zoom', () => {
  const makeZoom = () => {
    const z = function () { /* no-op when selection.call(z) invokes it */ };
    z.scaleExtent = () => z;
    z.filter = () => z;
    z.on = () => z;
    z.transform = () => z;
    return z;
  };
  return {
    zoom: () => makeZoom(),
    zoomIdentity: { x: 0, y: 0, k: 1 },
  };
});

import { buildTopologyView } from '../../components/topologyGraph.js';
import TopologyMap from '../../components/TopologyMap.jsx';

// ── Fixtures ────────────────────────────────────────────────────────────────

// Two nodes, each with its mesh radio MAC listed as a secondary interface so
// that router/neighbor MAC edge endpoints resolve to node primary MACs.
//   alpha (0a:d7:...:3e) — bat0;   secondary 9c:ef:d5:f9:80:4d — mesh radio
//   bravo (2c:cf:...:ba) — bat0;   secondary 9c:ef:d5:f9:9e:02 — mesh radio
// alpha has one local mesh edge to bravo (signal populated — so alpha is
// "self") plus one remote edge to a node not listed in topology.nodes
// (exercises the "synthesize unknown node" path). bravo emits the reverse
// edge back to alpha (no signal) — exercise dedup. alpha also owns a TT
// client — exercises the client clustering path.
function twoNodeTopology() {
  return {
    sourceVersion: '2013.3.0-14-gcd34783',
    algorithm: 4,
    collectedAt: null,
    nodes: [
      {
        primaryMac: '0a:d7:37:78:2d:3e',
        primaryHostname: 'alpha',
        secondaryMacs: ['9c:ef:d5:f9:80:4d'],
        neighbors: [
          { routerMac: '9c:ef:d5:f9:80:4d', neighborMac: '9c:ef:d5:f9:9e:02', neighborHostname: 'bravo', metric: 1.008, signal: -55, signalAverage: -57 },
          { routerMac: '9c:ef:d5:f9:80:4d', neighborMac: '00:0a:52:0b:7d:ae', neighborHostname: 'charlie', metric: 1.250, signal: 0, signalAverage: 0 },
        ],
        clients: [
          { mac: '3c:22:7f:37:4c:0c', hostname: 'alpha-wlan0' },
        ],
      },
      {
        primaryMac: '2c:cf:67:b8:88:ba',
        primaryHostname: 'bravo',
        secondaryMacs: ['9c:ef:d5:f9:9e:02'],
        neighbors: [
          { routerMac: '9c:ef:d5:f9:9e:02', neighborMac: '9c:ef:d5:f9:80:4d', neighborHostname: 'alpha', metric: 1.000, signal: 0, signalAverage: 0 },
        ],
        clients: [],
      },
    ],
  };
}

// ── buildTopologyView ───────────────────────────────────────────────────────

describe('TestBuildTopologyViewEmpty', () => {
  it('returns a zeroed view for null/undefined/empty topology', () => {
    for (const input of [null, undefined, {}, { nodes: [] }]) {
      const v = buildTopologyView(input);
      expect(v.self).toBeNull();
      expect(v.peers).toEqual([]);
      expect(v.edges).toEqual([]);
      expect(v.counts).toEqual({ peers: 0, degraded: 0, clients: 0, hopsMax: 0 });
    }
  });
});

describe('TestBuildTopologyViewShape', () => {
  it('detects self via the signal heuristic and separates it from peers', () => {
    const v = buildTopologyView(twoNodeTopology());
    expect(v.self).not.toBeNull();
    expect(v.self.mac).toBe('0a:d7:37:78:2d:3e');
    expect(v.self.hostname).toBe('alpha');
    // Peers: bravo + synthesized "charlie" unknown.
    expect(v.peers).toHaveLength(2);
    const ids = v.peers.map((p) => p.mac);
    expect(ids).toContain('2c:cf:67:b8:88:ba');
    expect(ids).toContain('00:0a:52:0b:7d:ae');
  });

  it('assigns sequential N-NN tags in (hops, mac) order so self is stable', () => {
    const v = buildTopologyView(twoNodeTopology());
    // Self is hops=0, peers are hops=1. Order on the same hop is mac asc.
    // Self's primary mac (0a:..) sorts before 00:.. by localeCompare since
    // '0' < '0' with the '0a' vs '00' tiebreak — so tags end up stable.
    const tags = [v.self.tag, ...v.peers.map((p) => p.tag)];
    // All tags are N-01..N-03, no duplicates.
    expect(new Set(tags).size).toBe(3);
    for (const t of tags) expect(t).toMatch(/^N-\d{2}$/);
  });

  it('assigns sequential C# tags to clients', () => {
    const v = buildTopologyView(twoNodeTopology());
    const selfClient = v.self.clients[0];
    expect(selfClient.tag).toBe('C1');
    expect(selfClient.mac).toBe('3c:22:7f:37:4c:0c');
  });

  it('attaches each client to its first-seen parent (global dedup)', () => {
    const topo = twoNodeTopology();
    // Also list the same client MAC under bravo — should stay with alpha.
    topo.nodes[1].clients = [{ mac: '3c:22:7f:37:4c:0c', hostname: 'wandering' }];
    const v = buildTopologyView(topo);
    expect(v.self.clients).toHaveLength(1);
    const bravo = v.peers.find((p) => p.mac === '2c:cf:67:b8:88:ba');
    expect(bravo.clients).toHaveLength(0);
  });

  it('dedupes repeated client MACs within a single parent', () => {
    const topo = twoNodeTopology();
    topo.nodes[0].clients.push({ mac: '3c:22:7f:37:4c:0c', hostname: 'alpha-wlan0' });
    const v = buildTopologyView(topo);
    expect(v.self.clients).toHaveLength(1);
  });
});

describe('TestBuildTopologyViewEdges', () => {
  it('dedupes bidirectional mesh edges into a single canonical edge', () => {
    const v = buildTopologyView(twoNodeTopology());
    expect(v.edges).toHaveLength(2);
  });

  it('prefers the edge direction carrying a signal reading', () => {
    const v = buildTopologyView(twoNodeTopology());
    const alphaBravo = v.edges.find((e) =>
      (e.src === '0a:d7:37:78:2d:3e' && e.dst === '2c:cf:67:b8:88:ba') ||
      (e.src === '2c:cf:67:b8:88:ba' && e.dst === '0a:d7:37:78:2d:3e'),
    );
    expect(alphaBravo).toBeDefined();
    expect(alphaBravo.signal).toBe(-55);
    expect(alphaBravo.metric).toBeCloseTo(1.008, 3);
  });

  it('resolves router_mac/neighbor_mac secondary interfaces to primary MACs', () => {
    const v = buildTopologyView(twoNodeTopology());
    const macs = new Set();
    for (const e of v.edges) {
      macs.add(e.src);
      macs.add(e.dst);
    }
    // No secondary mesh-radio MAC should leak into the edge list.
    expect(macs.has('9c:ef:d5:f9:80:4d')).toBe(false);
    expect(macs.has('9c:ef:d5:f9:9e:02')).toBe(false);
  });

  it('synthesizes an entry for edge endpoints not in topology.nodes', () => {
    const topo = {
      nodes: [{
        primaryMac: 'aa:aa:aa:aa:aa:aa',
        primaryHostname: 'alpha',
        secondaryMacs: [],
        neighbors: [
          { routerMac: 'aa:aa:aa:aa:aa:aa', neighborMac: 'bb:bb:bb:bb:bb:bb', neighborHostname: 'ghost', metric: 1.0, signal: -55, signalAverage: 0 },
        ],
        clients: [],
      }],
    };
    const v = buildTopologyView(topo);
    expect(v.peers.find((p) => p.mac === 'bb:bb:bb:bb:bb:bb')).toBeDefined();
    expect(v.edges).toHaveLength(1);
    expect(v.edges[0].src === 'bb:bb:bb:bb:bb:bb' || v.edges[0].dst === 'bb:bb:bb:bb:bb:bb').toBe(true);
  });

  it('flags weak edges when metric exceeds the threshold', () => {
    const topo = {
      nodes: [{
        primaryMac: 'aa:aa:aa:aa:aa:aa',
        primaryHostname: 'alpha',
        secondaryMacs: [],
        neighbors: [
          { routerMac: 'aa:aa:aa:aa:aa:aa', neighborMac: 'bb:bb:bb:bb:bb:bb', neighborHostname: 'ghost', metric: 3.5, signal: -85, signalAverage: 0 },
        ],
        clients: [],
      }],
    };
    const v = buildTopologyView(topo);
    expect(v.edges[0].weak).toBe(true);
    // Degraded propagates to the incident nodes.
    expect(v.counts.degraded).toBeGreaterThan(0);
  });
});

describe('TestBuildTopologyViewCounts', () => {
  it('reports peer / degraded / client counts + hopsMax', () => {
    const v = buildTopologyView(twoNodeTopology());
    expect(v.counts.peers).toBe(2);
    expect(v.counts.clients).toBe(1);
    expect(v.counts.hopsMax).toBe(1);
    expect(v.counts.degraded).toBe(0);
  });
});

// ── Component smoke tests ───────────────────────────────────────────────────

describe('TestTopologyMapEmpty', () => {
  it('renders an empty-state placeholder when topology has no nodes', () => {
    const { container, getByText } = render(<TopologyMap topology={null} />);
    expect(getByText('No topology data')).toBeTruthy();
    expect(container.querySelector('svg')).toBeNull();
  });
});

describe('TestTopologyMapRender', () => {
  it('renders an svg with one <g> per node and one <line> per edge', () => {
    const { container } = render(<TopologyMap topology={twoNodeTopology()} />);
    const svg = container.querySelector('svg');
    expect(svg).not.toBeNull();
    // self + 2 peers + 1 client = 4 nodes.
    expect(container.querySelectorAll('.topo-node').length).toBe(4);
    // 2 mesh edges + 1 client edge.
    expect(container.querySelectorAll('.topo-edge').length).toBe(3);
  });

  it('hides client dots + client edges in compact mode', () => {
    const { container } = render(<TopologyMap topology={twoNodeTopology()} compact />);
    expect(container.querySelectorAll('.topo-node.client').length).toBe(0);
    expect(container.querySelectorAll('.topo-edge.client').length).toBe(0);
    // Mesh edges still render.
    expect(container.querySelectorAll('.topo-edge').length).toBe(2);
  });

  it('applies .selected to the node whose id matches selectedId', () => {
    const view = buildTopologyView(twoNodeTopology());
    const firstPeer = view.peers[0];
    const { container } = render(
      <TopologyMap topology={twoNodeTopology()} selectedId={firstPeer.id} />,
    );
    const selected = container.querySelectorAll('.topo-node.selected');
    expect(selected.length).toBe(1);
  });

  it('invokes onSelect with the clicked node', () => {
    const spy = vi.fn();
    const { container } = render(<TopologyMap topology={twoNodeTopology()} onSelect={spy} />);
    const nodes = container.querySelectorAll('.topo-node');
    fireEvent.click(nodes[0]);
    expect(spy).toHaveBeenCalledTimes(1);
    const arg = spy.mock.calls[0][0];
    expect(arg).toHaveProperty('id');
    expect(arg).toHaveProperty('tag');
  });
});
