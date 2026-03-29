// =============================================================================
// MeshStatus.test.jsx — Tests for mesh network status panel
// =============================================================================

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import MeshStatusPanel from '../../components/MeshStatus.jsx';

describe('TestMeshStatusNullData', () => {
  it('shows connecting state when data is null', () => {
    const { container } = render(<MeshStatusPanel data={null} />);
    expect(screen.getByText('Connecting...')).toBeTruthy();
    expect(container.querySelector('.dot-yellow')).toBeTruthy();
  });
});

describe('TestMeshStatusEmptyData', () => {
  it('handles all-null sub-fields gracefully', () => {
    const data = { status: null, nodes: null, neighbors: null, interfaces: null };
    render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('No active nodes')).toBeTruthy();
    expect(screen.getByText('No neighbors')).toBeTruthy();
    expect(screen.getByText('No radios')).toBeTruthy();
  });

  it('handles empty arrays', () => {
    const data = {
      status: { connected: true, neighbors: 0, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [],
      interfaces: [],
    };
    render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('No active nodes')).toBeTruthy();
    expect(screen.getByText('No neighbors')).toBeTruthy();
    expect(screen.getByText('No radios')).toBeTruthy();
  });
});

describe('TestMeshStatusNodeList', () => {
  it('renders node list with hostname and IP', () => {
    const data = {
      status: { connected: true, neighbors: 1, mesh_interfaces: 1 },
      nodes: [
        { hostname: 'alpha', ip: '10.0.0.1' },
        { hostname: 'bravo', ip: '10.0.0.2' },
      ],
      neighbors: [
        { name: 'alpha', mac: 'aa:bb:cc:dd:ee:01' },
        { name: 'bravo', mac: 'aa:bb:cc:dd:ee:02' },
      ],
      interfaces: [],
    };
    render(<MeshStatusPanel data={data} />);
    // Names appear in both active nodes and neighbors sections
    expect(screen.getAllByText('alpha').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('10.0.0.1')).toBeTruthy();
    expect(screen.getAllByText('bravo').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('10.0.0.2')).toBeTruthy();
  });

  it('shows ? for nodes without hostname', () => {
    const data = {
      status: { connected: true, neighbors: 0, mesh_interfaces: 0 },
      nodes: [{ hostname: '', ip: '10.0.0.5' }],
      neighbors: [{ name: 'any', mac: 'xx:xx' }],
      interfaces: [],
    };
    render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('?')).toBeTruthy();
  });
});

describe('TestMeshStatusNeighbors', () => {
  it('shows neighbor signal strength', () => {
    const data = {
      status: { connected: true, neighbors: 1, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [
        { mac: 'aa:bb:cc:dd:ee:ff', signal: -45, throughput: 1500000 },
      ],
      interfaces: [],
    };
    render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('aa:bb:cc:dd:ee:ff')).toBeTruthy();
    expect(screen.getByText('-45dBm')).toBeTruthy();
    expect(screen.getByText('1.5 Mbps')).toBeTruthy();
  });

  it('shows named neighbor', () => {
    const data = {
      status: { connected: true, neighbors: 1, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [{ name: 'node-bravo', mac: 'ff:ee:dd', signal: -60 }],
      interfaces: [],
    };
    render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('node-bravo')).toBeTruthy();
    expect(screen.getByText('-60dBm')).toBeTruthy();
  });
});

describe('TestMeshStatusSparklines', () => {
  it('renders sparklines when neighbor history is provided', () => {
    const data = {
      status: { connected: true, neighbors: 1, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [
        { name: 'alpha', mac: 'aa:bb', signal: -50, throughput: 1000000 },
      ],
      interfaces: [],
    };
    const neighborHistory = {
      alpha: {
        signal: [-55, -52, -50],
        throughput: [900000, 950000, 1000000],
      },
    };
    const { container } = render(
      <MeshStatusPanel data={data} neighborHistory={neighborHistory} />
    );
    // Should have sparkline canvases (2 per neighbor: signal + throughput)
    const canvases = container.querySelectorAll('canvas');
    expect(canvases.length).toBe(2);
  });

  it('does not render sparklines with insufficient history', () => {
    const data = {
      status: { connected: true, neighbors: 1, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [
        { name: 'alpha', mac: 'aa:bb', signal: -50 },
      ],
      interfaces: [],
    };
    const neighborHistory = {
      alpha: { signal: [-50], throughput: [0] },
    };
    const { container } = render(
      <MeshStatusPanel data={data} neighborHistory={neighborHistory} />
    );
    const canvases = container.querySelectorAll('canvas');
    expect(canvases.length).toBe(0);
  });
});

describe('TestMeshStatusRadios', () => {
  it('shows radio frequencies and channel width', () => {
    const data = {
      status: { connected: true, neighbors: 0, mesh_interfaces: 1 },
      nodes: [],
      neighbors: [],
      interfaces: [
        { name: 'wlan0', frequency: 5180, channel_width: 80, type: '802.11ac' },
      ],
    };
    render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('wlan0')).toBeTruthy();
    expect(screen.getByText('5180MHz')).toBeTruthy();
    expect(screen.getByText('80MHz')).toBeTruthy();
    expect(screen.getByText('802.11ac')).toBeTruthy();
  });
});

describe('TestMeshStatusConnection', () => {
  it('shows Connected with green dot when connected', () => {
    const data = {
      status: { connected: true, neighbors: 0, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [],
      interfaces: [],
    };
    const { container } = render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('Connected')).toBeTruthy();
    expect(container.querySelector('.dot-green')).toBeTruthy();
  });

  it('shows Disconnected with red dot when not connected', () => {
    const data = {
      status: { connected: false, neighbors: 0, mesh_interfaces: 0 },
      nodes: [],
      neighbors: [],
      interfaces: [],
    };
    const { container } = render(<MeshStatusPanel data={data} />);
    expect(screen.getByText('Disconnected')).toBeTruthy();
    expect(container.querySelector('.dot-red')).toBeTruthy();
  });
});
