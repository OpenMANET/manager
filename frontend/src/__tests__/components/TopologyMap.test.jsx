// =============================================================================
// TopologyMap.test.jsx — Tests for mesh topology visualization
// =============================================================================
// The interactive reagraph canvas depends on WebGL and can't render meaningfully
// under jsdom, so these tests focus on the pure `buildGraphData` transform that
// feeds reagraph. A single smoke-test covers the component shell by mocking
// the reagraph import.

import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';

vi.mock('reagraph', () => ({
  GraphCanvas: (props) => (
    <div
      data-testid="graph-canvas"
      data-node-count={props.nodes.length}
      data-edge-count={props.edges.length}
    />
  ),
  darkTheme: { canvas: { background: '#000' }, node: {}, edge: {} },
}));

import { buildGraphData } from '../../components/topologyGraph.js';
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

// ── buildGraphData ──────────────────────────────────────────────────────────

describe('TestBuildGraphDataNull', () => {
  it('returns empty graph for null/undefined topology', () => {
    expect(buildGraphData(null)).toEqual({ nodes: [], edges: [] });
    expect(buildGraphData(undefined)).toEqual({ nodes: [], edges: [] });
    expect(buildGraphData({})).toEqual({ nodes: [], edges: [] });
  });
});

describe('TestBuildGraphDataNodeShape', () => {
  it('emits one node per mesh peer, one per client (global dedup), plus synthesized unknowns', () => {
    const { nodes } = buildGraphData(twoNodeTopology());
    // 2 peers + 1 client + 1 synthesized "charlie" (edge target not in
    // topology.nodes) = 4 nodes.
    expect(nodes).toHaveLength(4);
    const ids = nodes.map((n) => n.id);
    expect(ids).toContain('0a:d7:37:78:2d:3e');
    expect(ids).toContain('2c:cf:67:b8:88:ba');
    expect(ids).toContain('client:3c:22:7f:37:4c:0c');
    expect(ids).toContain('00:0a:52:0b:7d:ae');
  });

  it('marks the node whose edge carries signal as self', () => {
    const { nodes } = buildGraphData(twoNodeTopology());
    const alpha = nodes.find((n) => n.id === '0a:d7:37:78:2d:3e');
    const bravo = nodes.find((n) => n.id === '2c:cf:67:b8:88:ba');
    expect(alpha.data.type).toBe('self');
    expect(bravo.data.type).toBe('peer');
  });

  it('clusters clients with their parent peer via data.cluster', () => {
    const { nodes } = buildGraphData(twoNodeTopology());
    const client = nodes.find((n) => n.data.type === 'client');
    expect(client.data.cluster).toBe('0a:d7:37:78:2d:3e');
    expect(client.data.parentMac).toBe('0a:d7:37:78:2d:3e');
  });

  it('uses hostname as label when present, falling back to a short MAC', () => {
    const topo = twoNodeTopology();
    topo.nodes[0].primaryHostname = '';
    const { nodes } = buildGraphData(topo);
    const alpha = nodes.find((n) => n.id === '0a:d7:37:78:2d:3e');
    expect(alpha.label).toBe('78:2d:3e');
  });
});

describe('TestBuildGraphDataEdgeDedup', () => {
  it('merges reverse-direction mesh edges into a single canonical edge', () => {
    const { edges } = buildGraphData(twoNodeTopology());
    const meshEdges = edges.filter((e) => e.id.startsWith('mesh:'));
    // alpha↔bravo appears in both nodes' neighbor lists → one edge only.
    // alpha→charlie is a second mesh edge.
    expect(meshEdges).toHaveLength(2);
  });

  it('prefers the edge direction that carries a signal reading', () => {
    const { edges } = buildGraphData(twoNodeTopology());
    const alphaBravo = edges.find(
      (e) =>
        e.id.startsWith('mesh:') &&
        ((e.source === '0a:d7:37:78:2d:3e' && e.target === '2c:cf:67:b8:88:ba') ||
          (e.source === '2c:cf:67:b8:88:ba' && e.target === '0a:d7:37:78:2d:3e')),
    );
    expect(alphaBravo.data.signal).toBe(-55);
    expect(alphaBravo.label).toBe('-55 dBm');
  });

  it('renders remote-only edges with the TQ metric label and neutral fill', () => {
    const { edges } = buildGraphData(twoNodeTopology());
    const alphaCharlie = edges.find((e) => e.id === 'mesh:0a:d7:37:78:2d:3e->00:0a:52:0b:7d:ae');
    expect(alphaCharlie.label).toMatch(/^TQ /);
    expect(alphaCharlie.fill).toBe('#1a2a3a');
  });
});

describe('TestBuildGraphDataSecondaryRouting', () => {
  it('resolves router_mac and neighbor_mac on secondary interfaces to primary MAC node ids', () => {
    const { edges } = buildGraphData(twoNodeTopology());
    // The fixture's first edge has router=9c:ef:d5:f9:80:4d (secondary of alpha)
    // and neighbor=9c:ef:d5:f9:9e:02. After dedup with the peer's reverse
    // edge, the edge must connect alpha ↔ bravo by PRIMARY MAC.
    const alphaBravo = edges.find(
      (e) =>
        e.id.startsWith('mesh:') &&
        ((e.source === '0a:d7:37:78:2d:3e' && e.target === '2c:cf:67:b8:88:ba') ||
          (e.source === '2c:cf:67:b8:88:ba' && e.target === '0a:d7:37:78:2d:3e')),
    );
    expect(alphaBravo).toBeDefined();
  });
});

describe('TestBuildGraphDataUnknownTarget', () => {
  it('synthesizes a node when a neighbor MAC is not listed in topology.nodes', () => {
    const topo = {
      nodes: [
        {
          primaryMac: 'aa:aa:aa:aa:aa:aa',
          primaryHostname: 'alpha',
          secondaryMacs: [],
          neighbors: [
            { routerMac: 'aa:aa:aa:aa:aa:aa', neighborMac: 'bb:bb:bb:bb:bb:bb', neighborHostname: 'ghost', metric: 1.0, signal: 0, signalAverage: 0 },
          ],
          clients: [],
        },
      ],
    };
    const { nodes, edges } = buildGraphData(topo);
    expect(nodes.find((n) => n.id === 'bb:bb:bb:bb:bb:bb')).toBeDefined();
    expect(edges.find((e) => e.target === 'bb:bb:bb:bb:bb:bb')).toBeDefined();
  });
});

describe('TestBuildGraphDataClientEdges', () => {
  it('connects each client to its parent peer with a thin gray edge', () => {
    const { edges } = buildGraphData(twoNodeTopology());
    const clientEdges = edges.filter((e) => e.id.startsWith('client-edge:'));
    expect(clientEdges).toHaveLength(1);
    expect(clientEdges[0].source).toBe('0a:d7:37:78:2d:3e');
    expect(clientEdges[0].target).toBe('client:3c:22:7f:37:4c:0c');
  });
});

describe('TestBuildGraphDataDedup', () => {
  it('deduplicates clients that appear under multiple parent nodes', () => {
    // The same client MAC listed under both alpha and bravo — e.g., a
    // roaming TT entry — should produce a single client node and a single
    // client edge (attached to the first parent seen).
    const topo = twoNodeTopology();
    topo.nodes[1].clients = [{ mac: '3c:22:7f:37:4c:0c', hostname: 'wandering' }];
    const { nodes, edges } = buildGraphData(topo);

    const clientNodes = nodes.filter((n) => n.id === 'client:3c:22:7f:37:4c:0c');
    expect(clientNodes).toHaveLength(1);

    const clientEdges = edges.filter((e) => e.id === 'client-edge:3c:22:7f:37:4c:0c');
    expect(clientEdges).toHaveLength(1);
    expect(clientEdges[0].source).toBe('0a:d7:37:78:2d:3e');
  });

  it('deduplicates repeated client MACs within a single parent', () => {
    const topo = twoNodeTopology();
    topo.nodes[0].clients.push({ mac: '3c:22:7f:37:4c:0c', hostname: 'alpha-wlan0' });
    const { nodes } = buildGraphData(topo);
    const clientNodes = nodes.filter((n) => n.id === 'client:3c:22:7f:37:4c:0c');
    expect(clientNodes).toHaveLength(1);
  });

  it('produces no duplicate node ids regardless of upstream shape', () => {
    const { nodes } = buildGraphData(twoNodeTopology());
    const ids = new Set();
    for (const n of nodes) {
      expect(ids.has(n.id)).toBe(false);
      ids.add(n.id);
    }
  });
});

// ── Component shell ─────────────────────────────────────────────────────────

describe('TestTopologyMapEmpty', () => {
  it('shows the empty state when topology is null', () => {
    render(<TopologyMap topology={null} />);
    expect(screen.getByText('Network Topology')).toBeTruthy();
    expect(screen.getByText('No topology data')).toBeTruthy();
  });
});

describe('TestTopologyMapRender', () => {
  it('forwards nodes and edges to GraphCanvas when topology has data', () => {
    render(<TopologyMap topology={twoNodeTopology()} />);
    const canvas = screen.getByTestId('graph-canvas');
    // 2 peers + 1 client + 1 synthesized unknown = 4 nodes.
    expect(canvas.getAttribute('data-node-count')).toBe('4');
    // 2 mesh edges (after dedup) + 1 client edge.
    expect(canvas.getAttribute('data-edge-count')).toBe('3');
  });
});
