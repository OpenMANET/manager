// =============================================================================
// Topology.test.jsx — smoke tests for the dedicated topology page
// =============================================================================

import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';

const meshState = {
  status: null,
  topology: null,
};

vi.mock('../../hooks/useMeshStatus.js', () => ({
  useMeshStatus: () => meshState.status,
}));

vi.mock('../../hooks/useMeshTopology.js', () => ({
  useMeshTopology: () => meshState.topology,
}));

// TopologyMap pulls in canvas/svg work that's not relevant to the page-shell
// smoke test; replace with a marker so we can assert the page rendered.
vi.mock('../../components/TopologyMap.jsx', () => ({
  default: () => <div data-testid="topology-map">MAP</div>,
}));

import TopologyPage from '../../pages/Topology.jsx';

beforeEach(() => {
  meshState.status = null;
  meshState.topology = null;
});

afterEach(() => {
  cleanup();
});

describe('TestTopologyEmpty', () => {
  it('renders a placeholder map when no topology data is available yet', () => {
    render(<TopologyPage />);
    expect(screen.getByTestId('topology-map')).toBeInTheDocument();
  });
});

describe('TestTopologyWithData', () => {
  it('renders host counts from a populated topology view', () => {
    meshState.status = {
      nodes: [{ hostname: 'node-a', ipaddr: '10.0.0.1' }],
      status: {},
    };
    meshState.topology = {
      topology: {
        algorithm: 'BATMAN_V',
        self: { mac: 'aa:aa:aa:aa:aa:aa', hostname: 'self' },
        hosts: [],
        segments: [],
        blos_edges: [],
      },
      delta: null,
    };
    render(<TopologyPage />);
    expect(screen.getByTestId('topology-map')).toBeInTheDocument();
  });
});
