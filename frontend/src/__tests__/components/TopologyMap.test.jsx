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

  it('renders the short hostname inside the circle and the role/hops label beneath', () => {
    const { container } = render(<TopologyMap topology={directRF()} />);
    const circleTexts = Array.from(
      container.querySelectorAll('.topo-node > text:not(.topo-host-label):not(.topo-iface-overflow)'),
    ).map((n) => n.textContent);
    expect(circleTexts).toContain('me01');
    expect(circleTexts).toContain('alpha');

    // Self gets "SELF"; a peer 1 hop away reached via wlan0 gets "HOPS 1 · wlan0".
    const labels = Array.from(container.querySelectorAll('.topo-host-label')).map((n) => n.textContent);
    expect(labels).toContain('SELF');
    expect(labels).toContain('HOPS 1 · wlan0');
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

// positionsByLabel keys every rendered node by the short hostname in its
// inner circle (e.g. "gw", "B", "alpha") and returns the {x,y} offset
// from its translate attribute. Uses the inner text rather than the
// secondary .topo-host-label because the latter now carries role/hops
// info (SELF / HOPS N · ifname / STALE · …), which wouldn't uniquely
// identify a fixture node.
function positionsByLabel(container) {
  const out = {};
  for (const n of container.querySelectorAll('.topo-node')) {
    const label = n.querySelector('text:not(.topo-host-label):not(.topo-iface-overflow)')?.textContent;
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

// ringWithCrossEdge is a topology where four depth-1 siblings form a
// ring — gw has four direct children but two of them (B and C) also
// have a non-tree edge between them. The BFS tree alone places the
// four siblings in insertion order across the depth band; the hybrid
// relaxation should pull B and C closer together than their non-
// connected sibling A is to them.
function ringWithCrossEdge() {
  return {
    selfMac: 'aa:aa:aa:aa:aa:00',
    selfHostname: 'gw',
    algorithm: 'BATMAN_V',
    nodes: [
      { mac: 'aa:aa:aa:aa:aa:00', hostname: 'gw', segment: 'local', hopsFromSelf: 0, isSelf: true,  clientCount: 0, myHardIfname: '' },
      { mac: 'bb:bb:bb:bb:bb:00', hostname: 'A',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'cc:cc:cc:cc:cc:00', hostname: 'B',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'dd:dd:dd:dd:dd:00', hostname: 'C',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      { mac: 'ee:ee:ee:ee:ee:00', hostname: 'D',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
    ],
    edges: [
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'bb:bb:bb:bb:bb:00', metric: 15, blos: false, onMyPath: true },
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 15, blos: false, onMyPath: true },
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 15, blos: false, onMyPath: true },
      { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 15, blos: false, onMyPath: true },
      // Non-tree edge: B ↔ C. The hybrid relaxation should pull these
      // two same-depth siblings toward each other.
      { fromMac: 'cc:cc:cc:cc:cc:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 25, blos: false, onMyPath: false },
    ],
  };
}

describe('TestTopologyMapHybridLayout', () => {
  it('preserves BFS y-depth for every node even after relaxation', () => {
    const { container } = render(<TopologyMap topology={ringWithCrossEdge()} />);
    const pos = positionsByLabel(container);

    expect(pos.gw.y).toBe(0);
    // All four direct children of gw sit at the same BFS depth →
    // identical y coordinates; relaxation only touches x.
    expect(pos.A.y).toBe(pos.B.y);
    expect(pos.A.y).toBe(pos.C.y);
    expect(pos.A.y).toBe(pos.D.y);
    expect(pos.A.y).toBeGreaterThan(pos.gw.y);
  });

  it('pulls edge-connected same-depth siblings closer than disconnected ones', () => {
    const { container } = render(<TopologyMap topology={ringWithCrossEdge()} />);
    const pos = positionsByLabel(container);

    // B and C share a non-tree edge. A and D do not share any edge
    // with B/C beyond the gw root. After relaxation, |B.x - C.x|
    // should be at most the gap between B and its nearest non-C
    // sibling (A). If the assertion fails the relaxation either
    // skipped when it shouldn't have, or its parameters are too weak.
    const distBC = Math.abs(pos.B.x - pos.C.x);
    const distBA = Math.abs(pos.B.x - pos.A.x);
    const distCD = Math.abs(pos.C.x - pos.D.x);
    expect(distBC).toBeLessThanOrEqual(Math.min(distBA, distCD));
  });

  it('keeps fully-connected same-depth siblings at least MIN_SEP apart', () => {
    // Real-world regression: a mesh where every depth-1 peer advertises
    // every other depth-1 peer as an RF neighbor (K4 cross-connectivity)
    // used to let the springs pull all four on top of each other — the
    // repulsion force saturated at MIN_SEP² while the springs kept
    // pulling, so nodes visually stacked at the parent's x. The hard
    // post-relaxation sweep must guarantee visible spacing.
    const fullyConnected = {
      selfMac: 'aa:aa:aa:aa:aa:00',
      selfHostname: 'gw',
      algorithm: 'BATMAN_V',
      nodes: [
        { mac: 'aa:aa:aa:aa:aa:00', hostname: 'gw', segment: 'local', hopsFromSelf: 0, isSelf: true,  clientCount: 0, myHardIfname: '' },
        { mac: 'bb:bb:bb:bb:bb:00', hostname: 'A',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
        { mac: 'cc:cc:cc:cc:cc:00', hostname: 'B',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
        { mac: 'dd:dd:dd:dd:dd:00', hostname: 'C',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
        { mac: 'ee:ee:ee:ee:ee:00', hostname: 'D',  segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
      ],
      // Tree edges from gw to every sibling PLUS every sibling-pair edge.
      edges: [
        { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'bb:bb:bb:bb:bb:00', metric: 20, blos: false, onMyPath: true },
        { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 20, blos: false, onMyPath: true },
        { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 20, blos: false, onMyPath: true },
        { fromMac: 'aa:aa:aa:aa:aa:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 20, blos: false, onMyPath: true },
        { fromMac: 'bb:bb:bb:bb:bb:00', toMac: 'cc:cc:cc:cc:cc:00', metric: 20, blos: false, onMyPath: false },
        { fromMac: 'bb:bb:bb:bb:bb:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 20, blos: false, onMyPath: false },
        { fromMac: 'bb:bb:bb:bb:bb:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 20, blos: false, onMyPath: false },
        { fromMac: 'cc:cc:cc:cc:cc:00', toMac: 'dd:dd:dd:dd:dd:00', metric: 20, blos: false, onMyPath: false },
        { fromMac: 'cc:cc:cc:cc:cc:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 20, blos: false, onMyPath: false },
        { fromMac: 'dd:dd:dd:dd:dd:00', toMac: 'ee:ee:ee:ee:ee:00', metric: 20, blos: false, onMyPath: false },
      ],
    };
    const { container } = render(<TopologyMap topology={fullyConnected} />);
    const pos = positionsByLabel(container);
    const xs = [pos.A.x, pos.B.x, pos.C.x, pos.D.x].sort((a, b) => a - b);
    const gaps = xs.slice(1).map((x, i) => x - xs[i]);
    // MIN_SEP is 2*NODE_HALF_WIDTH + 12 = 2*63 + 12 = 138 when label
    // width dominates. Use a lower bound just shy of that to avoid a
    // flaky test if the constant shifts by a pixel.
    for (const g of gaps) {
      expect(g).toBeGreaterThanOrEqual(130);
    }
  });

  it('short-circuits for pure trees so tree-layout tests still hold', () => {
    // Reuse chainAndSingleton (no non-tree edges) — positions should be
    // unchanged vs the pure-BFS era. The TestTopologyMapTreeLayout block
    // above already asserts this; a direct assertion here confirms the
    // short-circuit triggers rather than running and converging to the
    // same answer.
    const { container } = render(<TopologyMap topology={chainAndSingleton()} />);
    const pos = positionsByLabel(container);
    // A should sit directly below its BFS parent B (no x drift).
    expect(pos.A.x).toBe(pos.B.x);
  });
});

describe('TestTopologyMapEdgeQuality', () => {
  it('tags edges with a quality class derived from metric + algorithm', () => {
    const { container } = render(<TopologyMap topology={ringWithCrossEdge()} />);
    const edges = container.querySelectorAll('.topo-edge');
    expect(edges.length).toBeGreaterThan(0);
    // Every edge must carry one of the four quality classes.
    for (const edge of edges) {
      const cls = edge.getAttribute('class') || '';
      const hasQuality = /q-(strong|ok|weak|unknown)/.test(cls);
      expect(hasQuality).toBe(true);
    }
  });

  it('marks unknown-metric edges with q-unknown', () => {
    const fixture = {
      ...directRF(),
      edges: [{ fromMac: '00:00:00:00:00:00', toMac: 'aa:aa:aa:aa:aa:01', metric: 0, blos: false, onMyPath: false }],
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const edge = container.querySelector('.topo-edge');
    expect(edge.getAttribute('class')).toContain('q-unknown');
  });
});

describe('TestTopologyMapStaleNode', () => {
  it('adds .stale to a non-self node whose gossip record is stale', () => {
    const fixture = {
      ...directRF(),
      nodes: [
        { ...directRF().nodes[0] }, // self — never stale
        { ...directRF().nodes[1], gossipStale: true },
      ],
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const stale = container.querySelectorAll('.topo-node.stale');
    expect(stale.length).toBe(1);
  });

  it('does not mark self as stale even when the field is true', () => {
    const fixture = {
      ...directRF(),
      nodes: directRF().nodes.map((n) => ({ ...n, gossipStale: true })),
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const staleNodes = [...container.querySelectorAll('.topo-node.stale')];
    for (const n of staleNodes) {
      expect(n.classList.contains('self')).toBe(false);
    }
  });
});

describe('TestTopologyMapRemoteKindAndGateway', () => {
  it('tags remote-segment peers with the .remote class', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    // gw1 is remote; alpha is local; me01 is self.
    const remote = container.querySelectorAll('.topo-node.remote');
    expect(remote.length).toBe(1);
    // Self must not pick up the remote class.
    const self = container.querySelector('.topo-node.self');
    expect(self).not.toBeNull();
    expect(self.classList.contains('remote')).toBe(false);
  });

  it('labels the anchor of a remote segment as GATEWAY', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    const remoteNode = container.querySelector('.topo-node.remote');
    const label = remoteNode.querySelector('.topo-host-label')?.textContent || '';
    // gw1 is the only remote host → anchor of its segment.
    expect(label.startsWith('GATEWAY')).toBe(true);
    expect(label).toContain('HOPS 1');
  });

  it('labels self as SELF even when gossipStale is incorrectly set', () => {
    const fixture = {
      ...directRF(),
      nodes: directRF().nodes.map((n) => (n.isSelf ? { ...n, gossipStale: true } : n)),
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const selfLabel = container.querySelector('.topo-node.self .topo-host-label')?.textContent;
    expect(selfLabel).toBe('SELF');
  });
});

describe('TestTopologyMapClientCountBadge', () => {
  it('renders a client-count pill when clientCount > 0 and hides it when zero', () => {
    const fixture = {
      ...directRF(),
      nodes: [
        { ...directRF().nodes[0] },
        { ...directRF().nodes[1], clientCount: 3 },
      ],
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const badges = container.querySelectorAll('.topo-client-badge');
    // Self has clientCount=0 → no badge; alpha has 3 → one badge.
    expect(badges.length).toBe(1);
    expect(badges[0].querySelector('text')?.textContent).toBe('·3');
  });

  it('suppresses the badge entirely in compact mode', () => {
    const fixture = {
      ...directRF(),
      nodes: [
        { ...directRF().nodes[0] },
        { ...directRF().nodes[1], clientCount: 5 },
      ],
    };
    const { container } = render(<TopologyMap topology={fixture} compact />);
    expect(container.querySelectorAll('.topo-client-badge').length).toBe(0);
  });
});

describe('TestTopologyMapEdgeLabels', () => {
  it('renders BATMAN_V metrics as "N Mbps"', () => {
    const fixture = {
      ...directRF(),
      algorithm: 'BATMAN_V',
      edges: [{ fromMac: '00:00:00:00:00:00', toMac: 'aa:aa:aa:aa:aa:01', metric: 32.4, blos: false, onMyPath: true }],
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const labels = Array.from(container.querySelectorAll('.topo-edge-label-text')).map((n) => n.textContent);
    expect(labels).toContain('32 Mbps');
  });

  it('renders BATMAN_IV metrics as "TQ N.NN"', () => {
    const fixture = { ...directRF(), algorithm: 'BATMAN_IV' };
    const { container } = render(<TopologyMap topology={fixture} />);
    const labels = Array.from(container.querySelectorAll('.topo-edge-label-text')).map((n) => n.textContent);
    expect(labels.some((t) => t.startsWith('TQ '))).toBe(true);
  });

  it('labels BLOS edges with the vxlan0 tunnel name', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    const labels = Array.from(container.querySelectorAll('.topo-edge-label-text')).map((n) => n.textContent);
    expect(labels.some((t) => t.startsWith('vxlan0'))).toBe(true);
  });
});

describe('TestTopologyMapSegmentSizing', () => {
  // Builds a local segment with `n` siblings of the self root so we
  // can check that a segment box contains every node's rendered
  // footprint (circle + secondary label). Before the label-aware
  // bbox pass, a segment with many children could have its right-
  // most host's "HOPS 1 · wlan0" text spilling past the box edge.
  function localWithSiblings(n) {
    const nodes = [{
      mac: '00:00:00:00:00:00', hostname: 'self', segment: 'local',
      hopsFromSelf: 0, isSelf: true, clientCount: 0, myHardIfname: '',
    }];
    const edges = [];
    for (let i = 0; i < n; i++) {
      const mac = `aa:aa:aa:aa:aa:${String(i + 1).padStart(2, '0')}`;
      nodes.push({
        mac,
        hostname: `BCM2711-peer${i}`,
        segment: 'local',
        hopsFromSelf: 1,
        isSelf: false,
        clientCount: 0,
        myHardIfname: 'wlan0',
      });
      edges.push({ fromMac: '00:00:00:00:00:00', toMac: mac, metric: 1.2, blos: false, onMyPath: true });
    }
    return { selfMac: '00:00:00:00:00:00', selfHostname: 'self', algorithm: 'BATMAN_V', nodes, edges };
  }

  it('keeps sibling host labels from overlapping (LEAF_SPACING > max label width)', () => {
    // Five siblings along a row. If LEAF_SPACING is smaller than the
    // width of the "HOPS 1 · wlan0" secondary label (~105px), adjacent
    // nodes' labels would paint on top of each other.
    const { container } = render(<TopologyMap topology={localWithSiblings(5)} />);
    const nodes = [...container.querySelectorAll('.topo-node:not(.self)')];
    const xs = nodes
      .map((n) => parseFloat(n.getAttribute('transform').match(/translate\(([-\d.]+),/)[1]))
      .sort((a, b) => a - b);
    const gaps = xs.slice(1).map((x, i) => x - xs[i]);
    for (const g of gaps) {
      // 15-char label ≈ 105px wide; siblings must sit at least that far
      // apart so labels don't overlap.
      expect(g).toBeGreaterThanOrEqual(105);
    }
  });

  it('sizes segment bboxes so every rendered node fits inside', () => {
    // Two segments (LOCAL with 4 siblings + a REMOTE gateway) — assert
    // that every rendered node's center sits inside its segment box.
    const topology = {
      selfMac: '00:00:00:00:00:00',
      selfHostname: 'self',
      algorithm: 'BATMAN_V',
      nodes: [
        { mac: '00:00:00:00:00:00', hostname: 'self', segment: 'local', hopsFromSelf: 0, isSelf: true, clientCount: 0, myHardIfname: '' },
        { mac: 'aa:aa:aa:aa:aa:01', hostname: 'BCM2711-a', segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
        { mac: 'aa:aa:aa:aa:aa:02', hostname: 'BCM2711-b', segment: 'local', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'wlan0' },
        { mac: 'cc:cc:cc:cc:cc:01', hostname: 'BCM2711-gw', segment: 'remote', hopsFromSelf: 1, isSelf: false, clientCount: 0, myHardIfname: 'vxlan0' },
        { mac: 'cc:cc:cc:cc:cc:02', hostname: 'BCM2711-rm1', segment: 'remote', hopsFromSelf: 2, isSelf: false, clientCount: 0, myHardIfname: 'vxlan0' },
        { mac: 'cc:cc:cc:cc:cc:03', hostname: 'BCM2711-rm2', segment: 'remote', hopsFromSelf: 2, isSelf: false, clientCount: 0, myHardIfname: 'vxlan0' },
      ],
      edges: [
        { fromMac: '00:00:00:00:00:00', toMac: 'aa:aa:aa:aa:aa:01', metric: 2, blos: false, onMyPath: true },
        { fromMac: '00:00:00:00:00:00', toMac: 'aa:aa:aa:aa:aa:02', metric: 2, blos: false, onMyPath: true },
        { fromMac: '00:00:00:00:00:00', toMac: 'cc:cc:cc:cc:cc:01', metric: 2, blos: true,  onMyPath: true },
        { fromMac: 'cc:cc:cc:cc:cc:01', toMac: 'cc:cc:cc:cc:cc:02', metric: 2, blos: false, onMyPath: false },
        { fromMac: 'cc:cc:cc:cc:cc:01', toMac: 'cc:cc:cc:cc:cc:03', metric: 2, blos: false, onMyPath: false },
      ],
    };
    const { container } = render(<TopologyMap topology={topology} />);

    // Collect every rendered segment box.
    const boxes = [...container.querySelectorAll('.topo-segment-box rect')].map((r) => ({
      x: parseFloat(r.getAttribute('x')),
      y: parseFloat(r.getAttribute('y')),
      w: parseFloat(r.getAttribute('width')),
      h: parseFloat(r.getAttribute('height')),
    }));
    expect(boxes.length).toBeGreaterThan(0);

    // Collect every rendered node's center.
    const centers = [...container.querySelectorAll('.topo-node')].map((n) => {
      const m = n.getAttribute('transform').match(/translate\(([-\d.]+),([-\d.]+)\)/);
      return { x: parseFloat(m[1]), y: parseFloat(m[2]) };
    });

    // Each node center must sit inside at least one segment box —
    // segments don't overlap in the packer, so a node without a
    // containing box would be rendered outside the remote/local chrome.
    for (const c of centers) {
      const inside = boxes.some((b) => c.x >= b.x && c.x <= b.x + b.w && c.y >= b.y && c.y <= b.y + b.h);
      expect(inside).toBe(true);
    }
  });
});

describe('TestTopologyMapStaleHostLabel', () => {
  it('replaces HOPS/ifname with STALE · <age> when gossip is stale and age is known', () => {
    const fixture = {
      ...directRF(),
      nodes: [
        { ...directRF().nodes[0] },
        { ...directRF().nodes[1], gossipStale: true, gossipAgeSeconds: 134 },
      ],
    };
    const { container } = render(<TopologyMap topology={fixture} />);
    const labels = Array.from(container.querySelectorAll('.topo-host-label')).map((n) => n.textContent);
    // 134s → "2m 14s"
    expect(labels.some((t) => t.includes('STALE') && t.includes('2m 14s'))).toBe(true);
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
