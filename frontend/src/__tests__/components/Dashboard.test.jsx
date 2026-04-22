// =============================================================================
// Dashboard.test.jsx — Tests for the Lattice dashboard layout
// =============================================================================

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';

// ── Mocks ──────────────────────────────────────────────────────────────────

const {
  mockGetDashboardStatus,
  mockGetGNSSStatus,
  mockListBLOSPeers,
} = vi.hoisted(() => ({
  mockGetDashboardStatus: vi.fn(),
  mockGetGNSSStatus: vi.fn(),
  mockListBLOSPeers: vi.fn(),
}));

vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({
    // All services share a single mock object — only the methods the page
    // calls need to be populated.
    getDashboardStatus: mockGetDashboardStatus,
    getGNSSStatus: mockGetGNSSStatus,
    listBLOSPeers: mockListBLOSPeers,
  }),
}));
vi.mock('../../services/connectClient.js', () => ({ transport: {} }));
vi.mock('../../gen/openmanet/dashboard/v1/dashboard_service_connect.js', () => ({
  DashboardService: {},
}));
vi.mock('../../gen/openmanet/gnss/v1/gnss_service_connect.js', () => ({
  GNSSService: {},
}));
vi.mock('../../gen/openmanet/blos/v1/blos_service_connect.js', () => ({
  BLOSService: {},
}));
vi.mock('../../gen/openmanet/dashboard/v1/dashboard_pb.js', () => ({
  NetworkInterfaceState: { CONNECTED: 1, DISCONNECTED: 2, NOT_CONNECTED: 3 },
}));
vi.mock('../../services/meshApi.js', () => ({
  fetchMeshStatus: vi.fn().mockResolvedValue({
    status: { connected: false, neighbors: 0, mesh_interfaces: 0, is_gateway: false },
    nodes: [], neighbors: [], interfaces: [],
  }),
  fetchMeshTopology: vi.fn().mockResolvedValue(null),
  fetchMeshTopologyDelta: vi.fn().mockResolvedValue(null),
}));
vi.mock('../../components/TopologyMap.jsx', () => ({
  default: () => null,
}));

import DashboardPage from '../../pages/Dashboard.jsx';

beforeEach(() => {
  mockGetGNSSStatus.mockResolvedValue({
    position: { fixType: 1 },
    satelliteStatus: { satellitesUsed: 0, satellitesInView: 0, satellites: [] },
  });
  mockListBLOSPeers.mockResolvedValue({ peers: [] });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  mockGetDashboardStatus.mockReset();
  mockGetGNSSStatus.mockReset();
  mockListBLOSPeers.mockReset();
});

// ── Fixtures ───────────────────────────────────────────────────────────────

function makeDeviceInfo(overrides = {}) {
  return {
    hostname: 'HaLowLink2-6cff',
    model: 'MorseMicro HaLowLink 2',
    firmware: 'OpenWrt 23.05.3 / OpenMANET 1.7.0',
    kernel: '5.15.150',
    architecture: 'mipsel_24kc',
    ...overrides,
  };
}

function makeSystemResources(overrides = {}) {
  return {
    uptime: { seconds: BigInt(224580), nanos: 0 }, // 2d 14h 23m
    localTime: { seconds: BigInt(1774021337), nanos: 0 },
    cpuLoadPercent: 12,
    memoryTotalBytes: BigInt(260046848),
    memoryUsedBytes: BigInt(176160768),
    overlayTotalBytes: BigInt(3880000),
    overlayUsedBytes: BigInt(3250000),
    ...overrides,
  };
}

function makeNetworkEntry(overrides = {}) {
  return {
    interfaceName: 'eth0',
    displayName: 'WAN (eth0)',
    state: 2,
    detail: 'Disconnected',
    ...overrides,
  };
}

function makeDashboardResponse(overrides = {}) {
  return {
    deviceInfo: makeDeviceInfo(),
    systemResources: makeSystemResources(),
    networkSummary: {
      entries: [
        makeNetworkEntry({ interfaceName: 'eth0', displayName: 'WAN (eth0)', state: 2, detail: 'Disconnected' }),
        makeNetworkEntry({ interfaceName: 'br-ahwlan', displayName: 'LAN (br-ahwlan)', state: 1, detail: '10.41.25.72/16' }),
        makeNetworkEntry({ interfaceName: 'phy1-ap0', displayName: 'HaLow Mesh (phy1-ap0)', state: 1, detail: 'Connected — 3 neighbors' }),
        makeNetworkEntry({ interfaceName: 'bat0', displayName: 'BATMAN (bat0)', state: 1, detail: '4 originators' }),
        makeNetworkEntry({ interfaceName: 'tailscale0', displayName: 'Tailscale (tailscale0)', state: 3, detail: 'Not connected' }),
      ],
    },
    activeServices: [],
    ...overrides,
  };
}

// ── Loading state ──────────────────────────────────────────────────────────

describe('TestDashboardLoading', () => {
  it('renders loading indicator before data arrives', () => {
    mockGetDashboardStatus.mockReturnValue(new Promise(() => {}));
    render(<DashboardPage />);
    expect(screen.getByText('Loading dashboard...')).toBeTruthy();
  });

  it('hides loading after data loads', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('◇ Dashboard')).toBeTruthy();
      expect(screen.queryByText('Loading dashboard...')).toBeNull();
    });
  });

  it('hides loading even on error', async () => {
    mockGetDashboardStatus.mockRejectedValue(new Error('network error'));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.queryByText('Loading dashboard...')).toBeNull();
    });
  });
});

// ── Chrome: topbar + view header ───────────────────────────────────────────

describe('TestDashboardChrome', () => {
  it('renders node id, status chips, and header toolbar', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => screen.getByText('◇ Dashboard'));

    const topbar = container.querySelector('.lat-topbar');
    expect(topbar).toBeTruthy();
    expect(topbar.textContent).toContain('HALOWLINK2-6CFF');

    const chips = [...container.querySelectorAll('.lat-chip')].map((c) => c.textContent.trim());
    expect(chips.some((c) => /MESH (UP|DOWN)/.test(c))).toBe(true);
    expect(chips.some((c) => /GPS/.test(c))).toBe(true);
    expect(chips.some((c) => /BLOS · 0 PEERS/.test(c))).toBe(true);

    const toolbarBtns = [...container.querySelectorAll('.lat-view-toolbar .lat-btn')]
      .map((b) => b.textContent.trim());
    expect(toolbarBtns).toContain('EXPORT');
    expect(toolbarBtns).toContain('CUSTOMIZE');
  });

  it('does not render a PTT Latency card (PTT lives on Comms)', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => screen.getByText('◇ Dashboard'));
    expect(screen.queryByText('PTT Latency')).toBeNull();
  });

  it('uses the first connected interface address as the node IP', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => screen.getByText('◇ Dashboard'));
    const nodeId = container.querySelector('.lat-topbar .node-id');
    expect(nodeId).toBeTruthy();
    expect(nodeId.textContent).toContain('10.41.25.72/16');
  });
});

// ── KPI cards (Row 1) ──────────────────────────────────────────────────────

describe('TestDashboardKpis', () => {
  it('renders the three KPI panels', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Mesh Peers')).toBeTruthy();
      expect(screen.getByText('Link Quality · 5m')).toBeTruthy();
      expect(screen.getByText('Battery & Power')).toBeTruthy();
    });
  });

  it('battery KPI renders dashes until backend exposes it', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => screen.getByText('Battery & Power'));
    const batteryPanel = [...container.querySelectorAll('.lat-panel')]
      .find((p) => p.textContent.includes('Battery & Power'));
    expect(batteryPanel).toBeTruthy();
    expect(batteryPanel.textContent).toContain('—');
  });
});

// ── Row 2 + Row 3 panels ───────────────────────────────────────────────────

describe('TestDashboardPanels', () => {
  it('renders the peers, alerts, resources, interfaces, and topology panels', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Mesh Peers · Live')).toBeTruthy();
      expect(screen.getByText('Alerts · Active')).toBeTruthy();
      expect(screen.getByText('System Resources')).toBeTruthy();
      expect(screen.getByText('Network Interfaces')).toBeTruthy();
      expect(screen.getByText('Network Topology')).toBeTruthy();
    });
  });

  it('renders system-resource pbars and kv column with live data', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('CPU')).toBeTruthy();
      expect(screen.getByText('MEM')).toBeTruthy();
      expect(screen.getByText('OVERLAY')).toBeTruthy();
      expect(screen.getByText('LOAD 1M')).toBeTruthy();
      expect(screen.getByText('2d 14h 23m')).toBeTruthy();
      expect(screen.getByText('5.15.150')).toBeTruthy();
      expect(screen.getByText('OpenWrt 23.05.3 / OpenMANET 1.7.0')).toBeTruthy();
      expect(screen.getByText('mipsel_24kc')).toBeTruthy();
    });
    // 4 pbars (CPU, MEM, OVERLAY, LOAD 1M) rendered.
    expect(container.querySelectorAll('.pbar').length).toBe(4);
  });

  it('renders a network interfaces table row per entry', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Network Interfaces')).toBeTruthy();
    });
    const table = [...container.querySelectorAll('table.lat-table')]
      .find((t) => t.textContent.includes('Iface'));
    expect(table).toBeTruthy();
    // 5 interface rows from the fixture.
    expect(table.querySelectorAll('tbody tr').length).toBe(5);
    expect(table.textContent).toContain('eth0');
    expect(table.textContent).toContain('bat0');
    expect(table.textContent).toContain('tailscale0');
  });

  it('peers-live table shows an empty-state row when no neighbors', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('No neighbors reporting')).toBeTruthy();
    });
  });
});

// ── Alerts ─────────────────────────────────────────────────────────────────

describe('TestDashboardAlerts', () => {
  it('shows a MESH DOWN alert when not connected', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => screen.getByText('Alerts · Active'));
    const alerts = [...container.querySelectorAll('.lat-alert')].map((a) => a.textContent.trim());
    expect(alerts.some((a) => /MESH DOWN/.test(a))).toBe(true);
  });
});

// ── Null data ──────────────────────────────────────────────────────────────

describe('TestDashboardNullData', () => {
  it('handles completely null response fields', async () => {
    mockGetDashboardStatus.mockResolvedValue({
      deviceInfo: null,
      systemResources: null,
      networkSummary: null,
      activeServices: [],
    });
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('◇ Dashboard')).toBeTruthy();
      expect(screen.getByText('System Resources')).toBeTruthy();
      expect(screen.getByText('Network Interfaces')).toBeTruthy();
      expect(screen.getByText('No network data')).toBeTruthy();
    });
  });
});

// ── Polling ────────────────────────────────────────────────────────────────

describe('TestDashboardPolling', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('polls for dashboard status at interval', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => screen.getByText('◇ Dashboard'));
    const initialCalls = mockGetDashboardStatus.mock.calls.length;
    vi.advanceTimersByTime(5000);
    await waitFor(() => {
      expect(mockGetDashboardStatus.mock.calls.length).toBeGreaterThan(initialCalls);
    });
  });

  it('continues polling after fetch error', async () => {
    mockGetDashboardStatus
      .mockResolvedValueOnce(makeDashboardResponse())
      .mockRejectedValueOnce(new Error('transient'))
      .mockResolvedValueOnce(makeDashboardResponse());

    render(<DashboardPage />);
    await waitFor(() => screen.getByText('◇ Dashboard'));

    vi.advanceTimersByTime(5000);
    await waitFor(() => {
      expect(mockGetDashboardStatus.mock.calls.length).toBe(2);
    });

    vi.advanceTimersByTime(5000);
    await waitFor(() => {
      expect(mockGetDashboardStatus.mock.calls.length).toBe(3);
    });
  });
});

// ── Uptime formatting ──────────────────────────────────────────────────────

describe('TestUptimeFormatting', () => {
  it('formats days+hours+minutes', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => expect(screen.getByText('2d 14h 23m')).toBeTruthy());
  });

  it('formats hours only', async () => {
    const resources = makeSystemResources({ uptime: { seconds: BigInt(7380), nanos: 0 } });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => expect(screen.getByText('2h 3m')).toBeTruthy());
  });

  it('formats minutes only', async () => {
    const resources = makeSystemResources({ uptime: { seconds: BigInt(300), nanos: 0 } });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => expect(screen.getByText('5m')).toBeTruthy());
  });
});
