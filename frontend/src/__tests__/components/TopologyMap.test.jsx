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

// ── Fixtures ────────────────────────────────────────────────────────────────

function directRF() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'BCM2711-me01',
    algorithm: 'BATMAN_IV',
    originators: [
      {
        origMac: 'aa:aa:aa:aa:aa:01', origHostname: 'BCM2711-alpha_wlan0',
        nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: 'BCM2711-alpha_wlan0',
        hardIfname: 'wlan0', tq: 255, hops: 1,
      },
    ],
  };
}

function withBLOSGateway() {
  return {
    selfMac: '00:00:00:00:00:00',
    selfHostname: 'BCM2711-me01',
    algorithm: 'BATMAN_IV',
    originators: [
      {
        origMac: 'aa:aa:aa:aa:aa:01', origHostname: 'BCM2711-alpha_wlan0',
        nextHopMac: 'aa:aa:aa:aa:aa:01', nextHopHostname: 'BCM2711-alpha_wlan0',
        hardIfname: 'wlan0', tq: 255, hops: 1,
      },
      {
        origMac: 'cc:cc:cc:cc:cc:01', origHostname: 'BCM2711-gw1_vxlan0',
        nextHopMac: 'cc:cc:cc:cc:cc:01', nextHopHostname: 'BCM2711-gw1_vxlan0',
        hardIfname: 'vxlan0', tq: 230, hops: 1,
      },
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
    // The circle-centered text is the short form (last dash-segment).
    const circleTexts = Array.from(container.querySelectorAll('.topo-node > text:not(.topo-host-label)'))
      .map((n) => n.textContent);
    expect(circleTexts).toContain('me01');
    expect(circleTexts).toContain('alpha');

    // The label beneath the circle carries the full base hostname.
    const labels = Array.from(container.querySelectorAll('.topo-host-label')).map((n) => n.textContent);
    expect(labels).toContain('BCM2711-me01');
    expect(labels).toContain('BCM2711-alpha');
  });

  it('renders interface badges for non-bat0 mesh interfaces', () => {
    const { container } = render(<TopologyMap topology={directRF()} />);
    const badges = container.querySelectorAll('.topo-iface-badge.rf');
    // self + alpha each carry a wlan0 badge.
    expect(badges.length).toBe(2);
  });

  it('hides badges and hostname label in compact mode', () => {
    const { container } = render(<TopologyMap topology={directRF()} compact />);
    expect(container.querySelectorAll('.topo-iface-badge').length).toBe(0);
    expect(container.querySelectorAll('.topo-host-label').length).toBe(0);
  });
});

describe('TestTopologyMapSegments', () => {
  it('draws one local segment box and one remote segment box when a BLOS gateway is present', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    const boxes = container.querySelectorAll('.topo-segment-box');
    expect(boxes.length).toBe(2);
    const labels = Array.from(container.querySelectorAll('.topo-segment-label')).map((n) => n.textContent);
    expect(labels.some((t) => t.startsWith('LOCAL'))).toBe(true);
    expect(labels.some((t) => t.startsWith('REMOTE · BCM2711-gw1'))).toBe(true);
  });

  it('renders the BLOS bridge with the .blos edge class', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    expect(container.querySelectorAll('.topo-edge.blos').length).toBe(1);
  });

  it('renders a vxlan0 badge as role=blos for the gateway host', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} />);
    // Both self (carrying vxlan0 as the local hard_ifname for the BLOS
    // route) and gw1 (the remote peer with vxlan0) own vxlan0 badges.
    expect(container.querySelectorAll('.topo-iface-badge.blos').length).toBeGreaterThanOrEqual(2);
  });

  it('suppresses segment chrome when compact (Dashboard mini-map)', () => {
    const { container } = render(<TopologyMap topology={withBLOSGateway()} compact />);
    expect(container.querySelectorAll('.topo-segment-box').length).toBe(0);
    expect(container.querySelectorAll('.topo-edge.blos').length).toBe(0);
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
