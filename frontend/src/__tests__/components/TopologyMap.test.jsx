// =============================================================================
// TopologyMap.test.jsx — Tests for network topology visualization
// =============================================================================

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import TopologyMap from '../../components/TopologyMap.jsx';

describe('TestTopologyMapNullData', () => {
  it('renders card with canvas when data is null', () => {
    const { container } = render(<TopologyMap data={null} />);
    expect(screen.getByText('Network Topology')).toBeTruthy();
    expect(container.querySelector('canvas')).toBeTruthy();
  });
});

describe('TestTopologyMapEmptyData', () => {
  it('renders with empty nodes and neighbors', () => {
    const data = { nodes: [], neighbors: [], status: null, interfaces: [] };
    const { container } = render(<TopologyMap data={data} />);
    expect(container.querySelector('canvas')).toBeTruthy();
  });
});

describe('TestTopologyMapWithNeighbors', () => {
  it('renders without crashing with neighbor data', () => {
    const data = {
      nodes: [
        { hostname: 'alpha', ip: '10.0.0.1' },
        { hostname: 'bravo', ip: '10.0.0.2' },
      ],
      neighbors: [
        { name: 'alpha', mac: 'aa:bb:cc:dd:ee:01', signal: -45, throughput: 1800000 },
        { name: 'bravo', mac: 'aa:bb:cc:dd:ee:02', signal: -70, throughput: 500000 },
      ],
      status: { connected: true },
      interfaces: [],
    };
    const { container } = render(<TopologyMap data={data} />);
    expect(container.querySelector('canvas')).toBeTruthy();
  });

  it('handles neighbors without names (MAC only)', () => {
    const data = {
      nodes: [],
      neighbors: [
        { mac: 'ff:ee:dd:cc:bb:aa', signal: -55, throughput: 0 },
      ],
      status: null,
      interfaces: [],
    };
    const { container } = render(<TopologyMap data={data} />);
    expect(container.querySelector('canvas')).toBeTruthy();
  });
});

describe('TestTopologyMapOuterRing', () => {
  it('renders nodes not in neighbor list in outer ring without crashing', () => {
    const data = {
      nodes: [
        { hostname: 'alpha', ip: '10.0.0.1' },
        { hostname: 'charlie', ip: '10.0.0.3' },
      ],
      neighbors: [
        { name: 'alpha', mac: 'aa:bb:cc:dd:ee:01', signal: -45 },
      ],
      status: null,
      interfaces: [],
    };
    const { container } = render(<TopologyMap data={data} />);
    expect(container.querySelector('canvas')).toBeTruthy();
  });
});
