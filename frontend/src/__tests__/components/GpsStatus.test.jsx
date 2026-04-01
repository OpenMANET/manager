// =============================================================================
// GpsStatus.test.jsx — Tests for GPS / GNSS status and configuration page
// =============================================================================

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';

const { mockGetGNSSStatus, mockGetGNSSConfig, mockUpdateGNSSConfig } = vi.hoisted(() => ({
  mockGetGNSSStatus: vi.fn(),
  mockGetGNSSConfig: vi.fn(),
  mockUpdateGNSSConfig: vi.fn(),
}));
vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({
    getGNSSStatus: mockGetGNSSStatus,
    getGNSSConfig: mockGetGNSSConfig,
    updateGNSSConfig: mockUpdateGNSSConfig,
  }),
}));
vi.mock('../../services/connectClient.js', () => ({ transport: {} }));
vi.mock('../../gen/openmanet/gnss/v1/gnss_service_connect.js', () => ({
  GNSSService: {},
}));

import GpsStatusPage from '../../pages/GpsStatus.jsx';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  mockGetGNSSStatus.mockReset();
  mockGetGNSSConfig.mockReset();
  mockUpdateGNSSConfig.mockReset();
});

// ── Fixtures ────────────────────────────────────────────────────────────────

const CONFIG_DISABLED = {
  settings: { enableGps: false },
  outputProtocols: { sendAsNmea: false, sendAsCot: false, cotUid: '' },
};

const CONFIG_ENABLED = {
  settings: { enableGps: true },
  outputProtocols: { sendAsNmea: true, sendAsCot: true, cotUid: 'my-device-123' },
};

const STATUS_3D_FIX = {
  position: {
    fixType: 3,
    latitude: 34.0522,
    longitude: -118.2437,
    altitude: 71,
    speed: 0,
    heading: 245.0,
    pdop: 1.2,
    hdop: 0.9,
    lastUpdate: new Date('2026-04-01T09:42:15Z'),
  },
  satelliteStatus: {
    satellitesUsed: 8,
    satellitesInView: 12,
    satellites: [
      { prn: 2, elevation: 45, azimuth: 120, snr: 38, used: true },
      { prn: 5, elevation: 72, azimuth: 210, snr: 42, used: true },
      { prn: 7, elevation: 15, azimuth: 330, snr: 18, used: false },
    ],
  },
};

const STATUS_NO_FIX = {
  position: { fixType: 0, latitude: 0, longitude: 0, altitude: 0, speed: 0, heading: 0, pdop: 0, hdop: 0 },
  satelliteStatus: { satellitesUsed: 0, satellitesInView: 0, satellites: [] },
};

// ── Loading state ───────────────────────────────────────────────────────────

describe('TestGpsStatusLoading', () => {
  it('renders loading state while fetching config', () => {
    mockGetGNSSConfig.mockReturnValue(new Promise(() => {}));
    mockGetGNSSStatus.mockReturnValue(new Promise(() => {}));
    render(<GpsStatusPage />);
    expect(screen.getByText('Loading GNSS data...')).toBeTruthy();
  });
});

// ── Error state ─────────────────────────────────────────────────────────────

describe('TestGpsStatusConfigError', () => {
  it('shows error when config fetch fails', async () => {
    mockGetGNSSConfig.mockRejectedValue(new Error('connection refused'));
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText(/Failed to load GNSS config: connection refused/)).toBeTruthy();
    });
  });
});

// ── No GPS data ─────────────────────────────────────────────────────────────

describe('TestGpsStatusNoData', () => {
  it('renders page with no position data', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(null);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('GPS / GNSS')).toBeTruthy();
    });
    expect(screen.getByText('No position data')).toBeTruthy();
    expect(screen.getByText('No satellite data available.')).toBeTruthy();
  });
});

// ── No fix state ────────────────────────────────────────────────────────────

describe('TestGpsStatusNoFix', () => {
  it('shows No Fix when fix type is 0', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('No Fix')).toBeTruthy();
    });
    expect(screen.getByText('Satellites (0 used / 0 in view)')).toBeTruthy();
  });
});

// ── 3D fix with full data ───────────────────────────────────────────────────

describe('TestGpsStatus3DFix', () => {
  it('displays position data for a 3D fix', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_3D_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('3D Fix')).toBeTruthy();
    });
    expect(screen.getByText('34.052200')).toBeTruthy();
    expect(screen.getByText('-118.243700')).toBeTruthy();
    expect(screen.getByText('71 m MSL')).toBeTruthy();
    expect(screen.getByText('0.0 km/h')).toBeTruthy();
    expect(screen.getByText('245.0 deg')).toBeTruthy();
    expect(screen.getByText('1.2')).toBeTruthy();
    expect(screen.getByText('0.9')).toBeTruthy();
  });
});

// ── Map view ────────────────────────────────────────────────────────────────

describe('TestGpsStatusMapView', () => {
  it('shows coordinates in map view', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_3D_FIX);
    const { container } = render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getAllByText('Map View').length).toBe(2); // card-title + inner label
    });
    // Coordinate div uses toFixed(4) with N/S E/W suffixes
    const coordDiv = container.querySelector('div[style*="monospace"][style*="color: var(--green)"]');
    expect(coordDiv).toBeTruthy();
    expect(coordDiv.textContent).toContain('34.0522');
    expect(coordDiv.textContent).toContain('N');
    expect(coordDiv.textContent).toContain('118.2437');
    expect(coordDiv.textContent).toContain('W');
    // Altitude line
    expect(screen.getByText(/71m MSL/)).toBeTruthy();
  });
});

// ── Satellite table ─────────────────────────────────────────────────────────

describe('TestGpsStatusSatellites', () => {
  it('renders satellite table with correct data', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_3D_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('Satellites (8 used / 12 in view)')).toBeTruthy();
    });
    // PRN labels
    expect(screen.getByText('G2')).toBeTruthy();
    expect(screen.getByText('G5')).toBeTruthy();
    expect(screen.getByText('G7')).toBeTruthy();
    // Elevation
    expect(screen.getByText('45 deg')).toBeTruthy();
    expect(screen.getByText('72 deg')).toBeTruthy();
    expect(screen.getByText('15 deg')).toBeTruthy();
    // Azimuth
    expect(screen.getByText('120 deg')).toBeTruthy();
    expect(screen.getByText('210 deg')).toBeTruthy();
    expect(screen.getByText('330 deg')).toBeTruthy();
    // SNR values
    expect(screen.getByText('38')).toBeTruthy();
    expect(screen.getByText('42')).toBeTruthy();
    expect(screen.getByText('18')).toBeTruthy();
  });
});

// ── GPS settings panel ──────────────────────────────────────────────────────

describe('TestGpsStatusSettingsDisabled', () => {
  it('shows GPS disabled state', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('GPS Settings')).toBeTruthy();
    });
    expect(screen.getByText('Enable GPS')).toBeTruthy();
    expect(screen.getByText('Send as NMEA')).toBeTruthy();
    expect(screen.getByText('Send as CoT (Cursor on Target)')).toBeTruthy();
    expect(screen.getByText('Save Settings')).toBeTruthy();
  });
});

describe('TestGpsStatusSettingsEnabled', () => {
  it('shows GPS enabled state with config values', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_ENABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_3D_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('GPS Settings')).toBeTruthy();
    });
    // CoT UID input should have the value from config
    const cotInput = screen.getByPlaceholderText('Leave empty for hostname');
    expect(cotInput.value).toBe('my-device-123');
  });
});

// ── Toggle interactions ─────────────────────────────────────────────────────

describe('TestGpsStatusToggleGPS', () => {
  it('toggles Enable GPS switch', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    const { container } = render(<GpsStatusPage />);
    await waitFor(() => screen.getByText('GPS Settings'));

    // Find the first toggle (Enable GPS) and click it
    const toggles = container.querySelectorAll('div[style*="width: 42px"]');
    expect(toggles.length).toBeGreaterThanOrEqual(1);
    fireEvent.click(toggles[0]);

    // After clicking, updateGNSSConfig hasn't been called yet (need to click Save)
    expect(mockUpdateGNSSConfig).not.toHaveBeenCalled();
  });
});

// ── Save config ─────────────────────────────────────────────────────────────

describe('TestGpsStatusSaveSuccess', () => {
  it('saves config and shows success message', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    mockUpdateGNSSConfig.mockResolvedValue({ success: true, message: 'GNSS configuration updated successfully.' });

    render(<GpsStatusPage />);
    await waitFor(() => screen.getByText('Save Settings'));

    fireEvent.click(screen.getByText('Save Settings'));

    await waitFor(() => {
      expect(screen.getByText('GNSS configuration updated successfully.')).toBeTruthy();
    });
    expect(mockUpdateGNSSConfig).toHaveBeenCalledWith(
      expect.objectContaining({
        settings: CONFIG_DISABLED.settings,
        outputProtocols: CONFIG_DISABLED.outputProtocols,
      }),
    );
  });
});

describe('TestGpsStatusSaveFailure', () => {
  it('shows error on save failure', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    mockUpdateGNSSConfig.mockRejectedValue(new Error('permission denied'));

    render(<GpsStatusPage />);
    await waitFor(() => screen.getByText('Save Settings'));

    fireEvent.click(screen.getByText('Save Settings'));

    await waitFor(() => {
      expect(screen.getByText(/Failed to save GNSS config: permission denied/)).toBeTruthy();
    });
  });

  it('shows error when response indicates failure', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    mockUpdateGNSSConfig.mockResolvedValue({ success: false, message: 'config file locked' });

    render(<GpsStatusPage />);
    await waitFor(() => screen.getByText('Save Settings'));

    fireEvent.click(screen.getByText('Save Settings'));

    await waitFor(() => {
      expect(screen.getByText(/Failed to save GNSS config/)).toBeTruthy();
    });
  });
});

// ── CoT UID input ───────────────────────────────────────────────────────────

describe('TestGpsStatusCoTUIDInput', () => {
  it('allows editing CoT UID and includes it in save', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);
    mockUpdateGNSSConfig.mockResolvedValue({ success: true, message: 'saved' });

    render(<GpsStatusPage />);
    await waitFor(() => screen.getByText('Save Settings'));

    const cotInput = screen.getByPlaceholderText('Leave empty for hostname');
    fireEvent.change(cotInput, { target: { value: 'test-uid-456' } });
    expect(cotInput.value).toBe('test-uid-456');

    fireEvent.click(screen.getByText('Save Settings'));

    await waitFor(() => {
      expect(mockUpdateGNSSConfig).toHaveBeenCalledWith(
        expect.objectContaining({
          outputProtocols: expect.objectContaining({ cotUid: 'test-uid-456' }),
        }),
      );
    });
  });
});

// ── 2D fix ──────────────────────────────────────────────────────────────────

describe('TestGpsStatus2DFix', () => {
  it('displays 2D Fix label for fix type 2', async () => {
    const status2D = {
      position: { fixType: 2, latitude: 51.5074, longitude: -0.1278, altitude: 0, speed: 3.0, heading: 90.0, pdop: 0, hdop: 1.5 },
      satelliteStatus: { satellitesUsed: 4, satellitesInView: 6, satellites: [] },
    };
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(status2D);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('2D Fix')).toBeTruthy();
    });
    expect(screen.getByText('51.507400')).toBeTruthy();
    expect(screen.getByText('10.8 km/h')).toBeTruthy();
  });
});

// ── Speed conversion ────────────────────────────────────────────────────────

describe('TestGpsStatusSpeedConversion', () => {
  it('converts m/s to km/h correctly', async () => {
    const status = {
      position: { fixType: 3, latitude: 1, longitude: 1, speed: 10, heading: 0, pdop: 0, hdop: 0 },
      satelliteStatus: { satellitesUsed: 0, satellitesInView: 0, satellites: [] },
    };
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(status);
    render(<GpsStatusPage />);
    await waitFor(() => {
      // 10 m/s * 3.6 = 36.0 km/h
      expect(screen.getByText('36.0 km/h')).toBeTruthy();
    });
  });
});

// ── Timestamp formatting ────────────────────────────────────────────────────

describe('TestGpsStatusTimestamp', () => {
  it('formats timestamp as HH:MM:SS UTC', async () => {
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_3D_FIX);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('09:42:15 UTC')).toBeTruthy();
    });
  });

  it('shows dash when no timestamp', async () => {
    const statusNoTs = {
      position: { fixType: 3, latitude: 1, longitude: 1, speed: 0, heading: 0, pdop: 0, hdop: 0 },
      satelliteStatus: { satellitesUsed: 0, satellitesInView: 0, satellites: [] },
    };
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(statusNoTs);
    render(<GpsStatusPage />);
    await waitFor(() => {
      expect(screen.getByText('3D Fix')).toBeTruthy();
    });
    // Last Update row should show '-'
    const cells = screen.getAllByText('-');
    expect(cells.length).toBeGreaterThan(0);
  });
});

// ── Polling ─────────────────────────────────────────────────────────────────

describe('TestGpsStatusPolling', () => {
  it('polls for status updates on interval', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGetGNSSConfig.mockResolvedValue(CONFIG_DISABLED);
    mockGetGNSSStatus.mockResolvedValue(STATUS_NO_FIX);

    render(<GpsStatusPage />);
    await waitFor(() => screen.getByText('GPS / GNSS'));

    // Initial calls: fetchConfig + fetchStatus
    const initialCalls = mockGetGNSSStatus.mock.calls.length;

    // Advance by one poll interval (2000ms)
    vi.advanceTimersByTime(2000);
    await waitFor(() => {
      expect(mockGetGNSSStatus.mock.calls.length).toBeGreaterThan(initialCalls);
    });

    vi.useRealTimers();
  });
});
