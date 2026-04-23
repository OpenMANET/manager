// =============================================================================
// TopologyMap.test.jsx — Tests for mesh topology SVG renderer
// =============================================================================
// The component renders an SVG graph driven by buildTopologyView(). d3-zoom
// is mocked because jsdom's SVG coordinate APIs are not fully implemented
// and the tests don't need to assert on zoom behaviour itself. Pure-JS
// transform tests live in topologyGraph.test.js.

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

// Two hosts with one RF link plus a client attached to alpha.
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
            metric: 1.02, signal: -55, signalAverage: -57,
          },
        ],
        clients: [{ mac: '3c:22:7f:37:4c:0c', hostname: 'mobile_wlan0' }],
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

// Two RF segments (alpha↔bravo and gamma↔delta) bridged by alpha↔gamma
// over vxlan0.
function twoSegmentBLOS() {
  return {
    nodes: [
      {
        primaryMac: '00:00:00:00:00:01',
        primaryHostname: 'alpha_bat0',
        secondaryMacs: ['00:00:00:00:aa:01', '00:00:00:00:cc:01'],
        neighbors: [
          {
            routerMac: '00:00:00:00:aa:01', routerHostname: 'alpha_wlan0',
            neighborMac: '00:00:00:00:aa:02', neighborHostname: 'bravo_wlan0',
            metric: 1.02, signal: -55, signalAverage: -55,
          },
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

// ── Rendering ──────────────────────────────────────────────────────────────

describe('TestTopologyMapEmpty', () => {
  it('renders a placeholder when the topology has no hosts', () => {
    const { container, getByText } = render(<TopologyMap topology={null} />);
    expect(getByText('No topology data')).toBeTruthy();
    expect(container.querySelector('svg')).toBeNull();
  });
});

describe('TestTopologyMapSinglePair', () => {
  it('renders one SVG with 2 hosts + 1 client + 1 RF edge + 1 client edge', () => {
    const { container } = render(<TopologyMap topology={simplePair()} />);
    const svg = container.querySelector('svg');
    expect(svg).not.toBeNull();
    // Hosts are rendered as .topo-node but not .client.
    const hostNodes = container.querySelectorAll('.topo-node:not(.client)');
    expect(hostNodes.length).toBe(2);
    // One client dot.
    expect(container.querySelectorAll('.topo-node.client').length).toBe(1);
    // One aggregated RF edge line plus one client edge line.
    expect(container.querySelectorAll('.topo-edge:not(.client)').length).toBe(1);
    expect(container.querySelectorAll('.topo-edge.client').length).toBe(1);
  });

  it('renders interface badges for non-bat0 mesh interfaces', () => {
    const { container } = render(<TopologyMap topology={simplePair()} />);
    const badges = container.querySelectorAll('.topo-iface-badge');
    // alpha and bravo each have one wlan0 badge (bat0 is hidden on purpose).
    expect(badges.length).toBe(2);
    for (const b of badges) expect(b.classList.contains('rf')).toBe(true);
  });

  it('does not draw a segment box in the single-segment case', () => {
    const { container } = render(<TopologyMap topology={simplePair()} />);
    expect(container.querySelectorAll('.topo-segment-box').length).toBe(0);
  });

  it('hides clients and badges when compact', () => {
    const { container } = render(<TopologyMap topology={simplePair()} compact />);
    expect(container.querySelectorAll('.topo-node.client').length).toBe(0);
    expect(container.querySelectorAll('.topo-edge.client').length).toBe(0);
    expect(container.querySelectorAll('.topo-iface-badge').length).toBe(0);
  });
});

describe('TestTopologyMapSegments', () => {
  it('draws one segment box per segment when more than one exists', () => {
    const { container } = render(<TopologyMap topology={twoSegmentBLOS()} />);
    const boxes = container.querySelectorAll('.topo-segment-box');
    expect(boxes.length).toBe(2);
    // Segment labels include letters A and B.
    const labels = Array.from(
      container.querySelectorAll('.topo-segment-label'),
    ).map((n) => n.textContent);
    expect(labels.some((t) => t.startsWith('SEGMENT A'))).toBe(true);
    expect(labels.some((t) => t.startsWith('SEGMENT B'))).toBe(true);
  });

  it('renders the BLOS bridge with the .blos edge class', () => {
    const { container } = render(<TopologyMap topology={twoSegmentBLOS()} />);
    const blos = container.querySelectorAll('.topo-edge.blos');
    expect(blos.length).toBe(1);
  });

  it('renders a vxlan0 badge as role=blos for hosts with a BLOS interface', () => {
    const { container } = render(<TopologyMap topology={twoSegmentBLOS()} />);
    const blosBadges = container.querySelectorAll('.topo-iface-badge.blos');
    // alpha and gamma each carry a vxlan0 badge.
    expect(blosBadges.length).toBe(2);
  });

  it('suppresses segment chrome when compact (Dashboard mini-map)', () => {
    const { container } = render(
      <TopologyMap topology={twoSegmentBLOS()} compact />,
    );
    expect(container.querySelectorAll('.topo-segment-box').length).toBe(0);
    expect(container.querySelectorAll('.topo-edge.blos').length).toBe(0);
  });
});

describe('TestTopologyMapInteraction', () => {
  it('applies .selected to the node whose id matches selectedId', () => {
    const view = buildTopologyView(simplePair());
    const firstHost = view.hosts[0];
    const { container } = render(
      <TopologyMap topology={simplePair()} selectedId={firstHost.id} />,
    );
    const selected = container.querySelectorAll('.topo-node.selected');
    expect(selected.length).toBe(1);
  });

  it('invokes onSelect with the clicked host record', () => {
    const spy = vi.fn();
    const { container } = render(
      <TopologyMap topology={simplePair()} onSelect={spy} />,
    );
    const nodes = container.querySelectorAll('.topo-node:not(.client)');
    fireEvent.click(nodes[0]);
    expect(spy).toHaveBeenCalledTimes(1);
    const arg = spy.mock.calls[0][0];
    expect(arg).toHaveProperty('id');
    expect(arg).toHaveProperty('tag');
    expect(arg).toHaveProperty('interfaces');
  });
});
