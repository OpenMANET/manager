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
