// =============================================================================
// TopologyMap.test.jsx — Tests for mesh topology SVG renderer
// =============================================================================
// Component-side tests only; the transform's own tests live in
// topologyGraph.test.js. d3-zoom is mocked because jsdom's SVG coordinate
// APIs are incomplete and the tests don't need to assert on zoom behavior.

import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent } from '@testing-library/react';

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

// ── Fixtures (new wire shape: { nodes, edges }) ────────────────────────────

function directRF() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'BCM2711-me01',
    algorithm: 'BATMAN_IV',
    nodes: [
      { mac: '00:00:00:00:00:00', hostname: 'BCM2711-me01', segment: 'local',
        hopsFromSelf: 0, isSelf: true, clientCount: 0, myHardIfname: '' },
      { mac: 'aa:aa:aa:aa:aa:01', hostname: 'BCM2711-alpha', segment: 'local',
        hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
    ],
    edges: [
      { fromMac: '00:00:00:00:00:00', toMac: 'aa:aa:aa:aa:aa:01',
        metric: 1.2, blos: false, onMyPath: true },
    ],
  };
}

function withBLOSGateway() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'BCM2711-me01',
    algorithm: 'BATMAN_IV',
    nodes: [
      { mac: '00:00:00:00:00:00', hostname: 'BCM2711-me01', segment: 'local',
        hopsFromSelf: 0, isSelf: true, clientCount: 0, myHardIfname: '' },
      { mac: 'aa:aa:aa:aa:aa:01', hostname: 'BCM2711-alpha', segment: 'local',
        hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'cc:cc:cc:cc:cc:01', hostname: 'BCM2711-gw1', segment: 'remote',
        hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'vxlan0' },
    ],
    edges: [
      { fromMac: '00:00:00:00:00:00', toMac: 'aa:aa:aa:aa:aa:01',
        metric: 1.2, blos: false, onMyPath: true },
      { fromMac: '00:00:00:00:00:00', toMac: 'cc:cc:cc:cc:cc:01',
        metric: 1.5, blos: true, onMyPath: true },
    ],
  };
}

// ── Rendering ───────────────────────────────────────────────────────────────

describe('TestTopologyMapEmpty', () => {
  it('renders a placeholder when there are no hosts', () => {
    const { container, getByText } = render(<TopologyMap topology={null} />);
    expect(getByText('No topology data')).toBeTruthy();
    expect(container.querySelector('svg')).toBeNull();
  });
});

describe('TestTopologyMapSinglePair', () => {
  it('renders one SVG with 2 hosts + 1 RF edge + no segment chrome', () => {
    const { container } = render(<TopologyMap topology={directRF()} />);
    expect(container.querySelector('svg')).not.toBeNull();
    expect(container.querySelectorAll('.topo-node').length).toBe(2);
    expect(container.querySelectorAll('.topo-edge:not(.blos)').length).toBe(1);
    expect(container.querySelectorAll('.topo-segment-box').length).toBe(0);
  });

  it('renders the short hostname inside the circle and the full hostname label beneath', () => {
    const { container } = render(<TopologyMap topology={directRF()} />);
    const circleTexts = Array.from(container.querySelectorAll('.topo-node > text:not(.topo-host-label)'))
      .map((n) => n.textContent);
    expect(circleTexts).toContain('me01');
    expect(circleTexts).toContain('alpha');

    const labels = Array.from(container.querySelectorAll('.topo-host-label')).map((n) => n.textContent);
    expect(labels).toContain('BCM2711-me01');
    expect(labels).toContain('BCM2711-alpha');
  });

  it('hides badges and hostname label in compact mode', () => {
    const { container } = render(<TopologyMap topology={directRF()} compact />);
    expect(container.querySelectorAll('.topo-iface-badge').length).toBe(0);
    expect(container.querySelectorAll('.topo-host-label').length).toBe(0);
  });
});

describe('TestTopologyMapSegments', () => {
  it('draws LOCAL and REMOTE MESH segment boxes when a BLOS gateway is present', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    const boxes = container.querySelectorAll('.topo-segment-box');
    expect(boxes.length).toBe(2);
    const labels = Array.from(container.querySelectorAll('.topo-segment-label')).map((n) => n.textContent);
    expect(labels.some((t) => t.startsWith('LOCAL'))).toBe(true);
    expect(labels.some((t) => t.startsWith('REMOTE MESH'))).toBe(true);
  });

  it('renders the BLOS bridge with the .blos edge class', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    expect(container.querySelectorAll('.topo-edge.blos').length).toBe(1);
  });

  it('suppresses segment chrome when compact (Dashboard mini-map)', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} compact />);
    expect(container.querySelectorAll('.topo-segment-box').length).toBe(0);
    expect(container.querySelectorAll('.topo-edge.blos').length).toBe(0);
  });
});

describe('TestTopologyMapMyPathsOverlay', () => {
  it('adds neither .mypath nor .muted classes by default', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    expect(container.querySelectorAll('.topo-edge.mypath').length).toBe(0);
    expect(container.querySelectorAll('.topo-edge.muted').length).toBe(0);
  });

  it('highlights on-my-path edges and mutes the rest when myPathsOverlay is on', () => {
    // Add a non-path edge so we can observe .muted appearing.
    const topology = withBLOSGateway();
    topology.nodes.push({
      mac: 'dd:dd:dd:dd:dd:01', hostname: 'BCM2711-peer', segment: 'local',
      hopsFromSelf: 2, isSelf: false, clientCount: 0, myHardIfname: '',
    });
    topology.edges.push({
      fromMac: 'aa:aa:aa:aa:aa:01', toMac: 'dd:dd:dd:dd:dd:01',
      metric: 1.1, blos: false, onMyPath: false,
    });

    const { container } = render(
      <TopologyMap topology={topology} myPathsOverlay />,
    );
    expect(container.querySelectorAll('.topo-edge.mypath').length).toBe(2);
    expect(container.querySelectorAll('.topo-edge.muted').length).toBe(1);
  });
});

// ── Intra-mesh tree layout ──────────────────────────────────────────────────
//
// The renderer places each segment's hosts in a BFS spanning tree rooted on
// the anchor (self for LOCAL, gateway for REMOTE MESH). A peer reachable
// only through another non-anchor node should land directly below its
// actual parent, not on an arbitrary concentric ring — that's the core
// fix behind this layout.

function positionsByLabel(container) {
  const out = {};
  for (const n of container.querySelectorAll('.topo-node')) {
    const label = n.querySelector('.topo-host-label')?.textContent;
    const m = n.getAttribute('transform')?.match(/translate\(([-\d.]+),([-\d.]+)\)/);
    if (label && m) out[label] = { x: parseFloat(m[1]), y: parseFloat(m[2]) };
  }
  return out;
}

// User-drawn topology — chain on one side of the gateway, singleton on the
// other:
//
//   gw ─ B ─ A ─ C
//   │
//   D
//
// Every node is in the LOCAL segment. BFS from gw should place B/D as
// siblings at depth 1, A below B at depth 2, C below A at depth 3.
function chainAndSingleton() {
  return {
    selfMac: 'aa:aa:aa:aa:aa:00',
    selfHostname: 'gw',
    algorithm: 'BATMAN_V',
    nodes: [
      { mac: 'aa:aa:aa:aa:aa:00', hostname: 'gw',  segment: 'local', hopsFromSelf: 0, isSelf: true,  clientCount: 0, myHardIfname: '' },
      { mac: 'bb:bb:bb:bb:bb:00', hostname: 'B',   segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'dd:dd:dd:dd:dd:00', hostname: 'D',   segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'cc:cc:cc:cc:cc:00', hostname: 'A',   segment: 'local', hopsFromSelf: 2, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'ee:ee:ee:ee:ee:00', hostname: 'C',   segment: 'local', hopsFromSelf: 3, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
    ],
    edges: [
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'bb:bb:bb:bb:bb:00', metric: 1.1, blos: false, onMyPath: true },
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 1.2, blos: false, onMyPath: true },
      { fromMac: 'bb:bb:bb:bb:bb:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 1.3, blos: false, onMyPath: true },
      { fromMac: 'cc:cc:cc:cc:cc:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 1.4, blos: false, onMyPath: false },
    ],
  };
}

describe('TestTopologyMapTreeLayout', () => {
  it('places descendants below their real parent, not on a hop ring', () => {
    const { container } = render(<TopologyMap topology={chainAndSingleton()} />);
    const pos = positionsByLabel(container);

    expect(pos.gw).toBeDefined();
    expect(pos.gw.y).toBe(0);

    // B and D are both direct children of gw — same depth, different x.
    expect(pos.B.y).toBe(pos.D.y);
    expect(pos.B.y).toBeGreaterThan(pos.gw.y);
    expect(pos.B.x).not.toBe(pos.D.x);

    // A sits one level below B (its BFS parent), not on a ring keyed by
    // hops-from-self. Before this fix A would have been placed on the
    // same ring as D with no relationship to B's x coordinate.
    expect(pos.A.y).toBeGreaterThan(pos.B.y);
    expect(pos.A.x).toBe(pos.B.x);

    // C is a leaf below A.
    expect(pos.C.y).toBeGreaterThan(pos.A.y);
    expect(pos.C.x).toBe(pos.A.x);
  });

  it('attaches hosts without edges to the root as orphan children', () => {
    const topology = chainAndSingleton();
    topology.nodes.push({
      mac: 'ff:ff:ff:ff:ff:00', hostname: 'Z', segment: 'local',
      hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0',
    });
    // No edges referencing Z — it should still land on the canvas.

    const { container } = render(<TopologyMap topology={topology} />);
    const pos = positionsByLabel(container);

    expect(pos.Z).toBeDefined();
    // Orphans attach to the root, so they sit at depth 1 alongside other
    // direct children.
    expect(pos.Z.y).toBe(pos.B.y);
  });
});

describe('TestTopologyMapInteraction', () => {
  it('applies .selected to the node whose id matches selectedId', () => {
    const view = buildTopologyView(directRF());
    const firstHost = view.hosts[0];
    const { container } = render(
      <TopologyMap topology={directRF()} selectedId={firstHost.id} />,
    );
    expect(container.querySelectorAll('.topo-node.selected').length).toBe(1);
  });

  it('invokes onSelect with the clicked host record', () => {
    const spy = vi.fn();
    const { container } = render(
      <TopologyMap topology={directRF()} onSelect={spy} />,
    );
    const nodes = container.querySelectorAll('.topo-node');
    fireEvent.click(nodes[0]);
    expect(spy).toHaveBeenCalledTimes(1);
    const arg = spy.mock.calls[0][0];
    expect(arg).toHaveProperty('baseHostname');
    expect(arg).toHaveProperty('interfaces');
  });
});
