// =============================================================================
// Dashboard.test.jsx — Tests for Dashboard page
// =============================================================================

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';

// ── Mocks ──────────────────────────────────────────────────────────────────

const { mockGetDashboardStatus, mockExecuteQuickAction } = vi.hoisted(() => ({
  mockGetDashboardStatus: vi.fn(),
  mockExecuteQuickAction: vi.fn(),
}));
vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({
    getDashboardStatus: mockGetDashboardStatus,
    executeQuickAction: mockExecuteQuickAction,
  }),
}));
vi.mock('../../services/connectClient.js', () => ({ transport: {} }));
vi.mock('../../gen/openmanet/dashboard/v1/dashboard_service_connect.js', () => ({
  DashboardService: {},
}));
vi.mock('../../services/meshApi.js', () => ({
  fetchMeshStatus: vi.fn().mockResolvedValue({
    status: null, nodes: null, neighbors: null, interfaces: null,
  }),
  fetchMeshTopology: vi.fn().mockResolvedValue(null),
}));
vi.mock('../../components/MeshStatus.jsx', () => ({
  default: () => null,
}));
vi.mock('../../components/TopologyMap.jsx', () => ({
  default: () => null,
}));

import DashboardPage from '../../pages/Dashboard.jsx';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  mockGetDashboardStatus.mockReset();
  mockExecuteQuickAction.mockReset();
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
    localTime: { seconds: BigInt(1774021337), nanos: 0 }, // 2026-03-20 09:42:17 UTC
    cpuLoadPercent: 12,
    memoryTotalBytes: BigInt(260046848),  // 248 MB
    memoryUsedBytes: BigInt(176160768),   // 168 MB
    overlayTotalBytes: BigInt(3880000),   // ~3.7 MB
    overlayUsedBytes: BigInt(3250000),    // ~3.1 MB
    ...overrides,
  };
}

function makeNetworkEntry(overrides = {}) {
  return {
    interfaceName: 'eth0',
    displayName: 'WAN (eth0)',
    state: 2, // DISCONNECTED
    detail: 'Disconnected',
    ...overrides,
  };
}

function makeServiceInfo(overrides = {}) {
  return {
    name: 'openmanetd',
    status: 1, // RUNNING
    pid: 1842,
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
        makeNetworkEntry({ interfaceName: 'phy1-ap0', displayName: 'HaLow Mesh (phy1-ap0)', state: 1, detail: 'Connected \u2014 3 neighbors' }),
        makeNetworkEntry({ interfaceName: 'bat0', displayName: 'BATMAN (bat0)', state: 1, detail: '4 originators' }),
        makeNetworkEntry({ interfaceName: 'tailscale0', displayName: 'Tailscale (tailscale0)', state: 3, detail: 'Not connected' }),
      ],
    },
    activeServices: [
      makeServiceInfo({ name: 'openmanetd', status: 1, pid: 1842 }),
      makeServiceInfo({ name: 'openmanet-webui', status: 1, pid: 2105 }),
      makeServiceInfo({ name: 'dnsmasq', status: 1, pid: 987 }),
      makeServiceInfo({ name: 'hostapd', status: 1, pid: 1102 }),
      makeServiceInfo({ name: 'gpsd', status: 1, pid: 1456 }),
      makeServiceInfo({ name: 'batadv-vis', status: 2, pid: 0 }),
    ],
    ...overrides,
  };
}

// ── Loading State ──────────────────────────────────────────────────────────

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
      expect(screen.getByText('Dashboard')).toBeTruthy();
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

// ── Device Information ─────────────────────────────────────────────────────

describe('TestDeviceInformation', () => {
  it('renders all device info fields', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Device Information')).toBeTruthy();
      expect(screen.getByText('HaLowLink2-6cff')).toBeTruthy();
      expect(screen.getByText('MorseMicro HaLowLink 2')).toBeTruthy();
      expect(screen.getByText('OpenWrt 23.05.3 / OpenMANET 1.7.0')).toBeTruthy();
      expect(screen.getByText('5.15.150')).toBeTruthy();
      expect(screen.getByText('mipsel_24kc')).toBeTruthy();
    });
  });

  it('renders labels for all device fields', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Hostname')).toBeTruthy();
      expect(screen.getByText('Model')).toBeTruthy();
      expect(screen.getByText('Firmware')).toBeTruthy();
      expect(screen.getByText('Kernel')).toBeTruthy();
      expect(screen.getByText('Architecture')).toBeTruthy();
    });
  });

  it('shows dashes for null device info', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ deviceInfo: null }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Device Information')).toBeTruthy();
      const dashes = screen.getAllByText('-');
      expect(dashes.length).toBeGreaterThanOrEqual(5);
    });
  });
});

// ── System Resources ───────────────────────────────────────────────────────

describe('TestSystemResources', () => {
  it('renders system resources section', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('System Resources')).toBeTruthy();
    });
  });

  it('formats uptime correctly for days+hours+minutes', async () => {
    // 2d 14h 23m = 224580 seconds
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('2d 14h 23m')).toBeTruthy();
    });
  });

  it('formats uptime correctly for hours only', async () => {
    const resources = makeSystemResources({ uptime: { seconds: BigInt(7380), nanos: 0 } }); // 2h 3m
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('2h 3m')).toBeTruthy();
    });
  });

  it('formats uptime correctly for minutes only', async () => {
    const resources = makeSystemResources({ uptime: { seconds: BigInt(300), nanos: 0 } }); // 5m
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('5m')).toBeTruthy();
    });
  });

  it('formats zero uptime', async () => {
    const resources = makeSystemResources({ uptime: { seconds: BigInt(0), nanos: 0 } });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('0m')).toBeTruthy();
    });
  });

  it('shows dash for null uptime', async () => {
    const resources = makeSystemResources({ uptime: null });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Uptime')).toBeTruthy();
    });
  });

  it('formats local time as UTC string', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText(/2026-\d{2}-\d{2} \d{2}:\d{2}:\d{2} UTC/)).toBeTruthy();
    });
  });

  it('renders CPU load progress bar', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('CPU Load')).toBeTruthy();
      expect(screen.getByText('12 % / 100 %')).toBeTruthy();
      const fills = container.querySelectorAll('.dashboard-bar-fill');
      expect(fills.length).toBe(3); // CPU, Memory, Overlay
    });
  });

  it('renders memory progress bar with formatted bytes', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Memory')).toBeTruthy();
      expect(screen.getByText(/168\.0 MB.*248\.0 MB/)).toBeTruthy();
    });
  });

  it('renders overlay progress bar with formatted bytes', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Overlay')).toBeTruthy();
    });
  });

  it('shows dashes for null system resources', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: null }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('System Resources')).toBeTruthy();
    });
  });
});

describe('TestProgressBarColors', () => {
  it('uses green for low CPU usage', async () => {
    const resources = makeSystemResources({ cpuLoadPercent: 30 });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      const fills = container.querySelectorAll('.dashboard-bar-fill');
      expect(fills[0].style.background).toBe('var(--green)');
    });
  });

  it('uses yellow for moderate CPU usage', async () => {
    const resources = makeSystemResources({ cpuLoadPercent: 75 });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      const fills = container.querySelectorAll('.dashboard-bar-fill');
      expect(fills[0].style.background).toBe('var(--yellow)');
    });
  });

  it('uses red for high CPU usage', async () => {
    const resources = makeSystemResources({ cpuLoadPercent: 95 });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      const fills = container.querySelectorAll('.dashboard-bar-fill');
      expect(fills[0].style.background).toBe('var(--red)');
    });
  });
});

// ── Network Summary ────────────────────────────────────────────────────────

describe('TestNetworkSummary', () => {
  it('renders all network entries', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Network Summary')).toBeTruthy();
      expect(screen.getByText('WAN (eth0)')).toBeTruthy();
      expect(screen.getByText('LAN (br-ahwlan)')).toBeTruthy();
      expect(screen.getByText('HaLow Mesh (phy1-ap0)')).toBeTruthy();
      expect(screen.getByText('BATMAN (bat0)')).toBeTruthy();
      expect(screen.getByText('Tailscale (tailscale0)')).toBeTruthy();
    });
  });

  it('shows detail text for connected interfaces', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('10.41.25.72/16')).toBeTruthy();
      expect(screen.getByText('Connected \u2014 3 neighbors')).toBeTruthy();
      expect(screen.getByText('4 originators')).toBeTruthy();
    });
  });

  it('shows disconnected detail for disconnected interfaces', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Disconnected')).toBeTruthy();
    });
  });

  it('shows not connected detail for not-connected interfaces', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Not connected')).toBeTruthy();
    });
  });

  it('uses correct status dot colors', async () => {
    const resp = makeDashboardResponse({
      networkSummary: {
        entries: [
          makeNetworkEntry({ interfaceName: 'connected', displayName: 'Connected IF', state: 1, detail: 'OK' }),
          makeNetworkEntry({ interfaceName: 'disconnected', displayName: 'Disconnected IF', state: 2, detail: 'Down' }),
          makeNetworkEntry({ interfaceName: 'notconn', displayName: 'Not Connected IF', state: 3, detail: '' }),
        ],
      },
    });
    mockGetDashboardStatus.mockResolvedValue(resp);
    const { container } = render(<DashboardPage />);
    await waitFor(() => {
      const dots = container.querySelectorAll('.status-dot');
      // Find dots within the network summary section
      const networkDots = [];
      dots.forEach(dot => {
        const parent = dot.closest('.card');
        if (parent && parent.textContent.includes('Network Summary')) {
          networkDots.push(dot);
        }
      });
      expect(networkDots.length).toBe(3);
      expect(networkDots[0].style.background).toBe('var(--green)');
      expect(networkDots[1].style.background).toBe('var(--red)');
      expect(networkDots[2].style.background).toBe('var(--muted)');
    });
  });

  it('shows no network data message for empty entries', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({
      networkSummary: { entries: [] },
    }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('No network data')).toBeTruthy();
    });
  });

  it('handles null network summary', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ networkSummary: null }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Network Summary')).toBeTruthy();
      expect(screen.getByText('No network data')).toBeTruthy();
    });
  });
});

// ── Quick Actions ──────────────────────────────────────────────────────────

describe('TestQuickActions', () => {
  it('renders reboot button', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('Quick Actions')).toBeTruthy();
      expect(screen.getByText('Reboot Device')).toBeTruthy();
    });
  });

  it('calls executeQuickAction on confirmed reboot', async () => {
    vi.stubGlobal('confirm', vi.fn(() => true));
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    mockExecuteQuickAction.mockResolvedValue({ success: true });
    render(<DashboardPage />);
    await waitFor(() => screen.getByText('Reboot Device'));

    fireEvent.click(screen.getByText('Reboot Device'));

    await waitFor(() => {
      expect(window.confirm).toHaveBeenCalledWith(
        expect.stringContaining('reboot')
      );
      expect(mockExecuteQuickAction).toHaveBeenCalledWith(
        expect.objectContaining({ action: 1 }) // REBOOT_DEVICE
      );
    });
  });

  it('does not call API when reboot is cancelled', async () => {
    vi.stubGlobal('confirm', vi.fn(() => false));
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    render(<DashboardPage />);
    await waitFor(() => screen.getByText('Reboot Device'));

    fireEvent.click(screen.getByText('Reboot Device'));

    expect(window.confirm).toHaveBeenCalled();
    expect(mockExecuteQuickAction).not.toHaveBeenCalled();
  });

  it('shows rebooting state after confirmation', async () => {
    vi.stubGlobal('confirm', vi.fn(() => true));
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    mockExecuteQuickAction.mockResolvedValue({ success: true });
    render(<DashboardPage />);
    await waitFor(() => screen.getByText('Reboot Device'));

    fireEvent.click(screen.getByText('Reboot Device'));

    await waitFor(() => {
      expect(screen.getByText('Rebooting\u2026')).toBeTruthy();
    });
  });

  it('handles reboot API error gracefully', async () => {
    vi.stubGlobal('confirm', vi.fn(() => true));
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse());
    mockExecuteQuickAction.mockRejectedValue(new Error('connection lost'));
    render(<DashboardPage />);
    await waitFor(() => screen.getByText('Reboot Device'));

    fireEvent.click(screen.getByText('Reboot Device'));

    // Should show rebooting state (device may be unreachable)
    await waitFor(() => {
      expect(screen.getByText('Rebooting\u2026')).toBeTruthy();
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
    await waitFor(() => screen.getByText('Dashboard'));

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
    await waitFor(() => screen.getByText('Dashboard'));

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

// ── Null / Empty Data ──────────────────────────────────────────────────────

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
      expect(screen.getByText('Dashboard')).toBeTruthy();
      expect(screen.getByText('Device Information')).toBeTruthy();
      expect(screen.getByText('System Resources')).toBeTruthy();
      expect(screen.getByText('Network Summary')).toBeTruthy();
      expect(screen.getByText('Quick Actions')).toBeTruthy();
    });
  });

  it('handles missing fields in device info', async () => {
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({
      deviceInfo: { hostname: 'test', model: '', firmware: '', kernel: '', architecture: '' },
    }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText('test')).toBeTruthy();
    });
  });
});

// ── Bytes Formatting ───────────────────────────────────────────────────────

describe('TestBytesFormatting', () => {
  it('formats small byte values', async () => {
    const resources = makeSystemResources({
      memoryTotalBytes: BigInt(512),
      memoryUsedBytes: BigInt(100),
    });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText(/100 B.*512 B/)).toBeTruthy();
    });
  });

  it('formats KB values', async () => {
    const resources = makeSystemResources({
      memoryTotalBytes: BigInt(10240),
      memoryUsedBytes: BigInt(5120),
    });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText(/5\.0 KB.*10\.0 KB/)).toBeTruthy();
    });
  });

  it('formats GB values', async () => {
    const resources = makeSystemResources({
      memoryTotalBytes: BigInt(2147483648),  // 2 GB
      memoryUsedBytes: BigInt(1073741824),   // 1 GB
    });
    mockGetDashboardStatus.mockResolvedValue(makeDashboardResponse({ systemResources: resources }));
    render(<DashboardPage />);
    await waitFor(() => {
      expect(screen.getByText(/1\.00 GB.*2\.00 GB/)).toBeTruthy();
    });
  });
});
