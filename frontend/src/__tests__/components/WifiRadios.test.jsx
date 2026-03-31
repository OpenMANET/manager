// =============================================================================
// WifiRadios.test.jsx — Tests for WiFi radios page
// =============================================================================

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';

const { mockListRadios, mockGetRadioStatus, mockGetRadioSettings, mockUpdateRadioSettings, mockListConnectedClients, mockListMeshPeers } = vi.hoisted(() => ({
  mockListRadios: vi.fn(),
  mockGetRadioStatus: vi.fn(),
  mockGetRadioSettings: vi.fn(),
  mockUpdateRadioSettings: vi.fn(),
  mockListConnectedClients: vi.fn(),
  mockListMeshPeers: vi.fn(),
}));
vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({
    listRadios: mockListRadios,
    getRadioStatus: mockGetRadioStatus,
    getRadioSettings: mockGetRadioSettings,
    updateRadioSettings: mockUpdateRadioSettings,
    listConnectedClients: mockListConnectedClients,
    listMeshPeers: mockListMeshPeers,
  }),
}));
vi.mock('../../services/connectClient.js', () => ({ transport: {} }));

import WifiRadiosPage from '../../pages/WifiRadios.jsx';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  mockListRadios.mockReset();
  mockGetRadioStatus.mockReset();
  mockGetRadioSettings.mockReset();
  mockUpdateRadioSettings.mockReset();
  mockListConnectedClients.mockReset();
  mockListMeshPeers.mockReset();
});

const RADIOS = [
  { name: 'radio0', displayName: 'HaLow Radio', hardwareName: 'morse-micro', band: 4, interfaceName: 'wlan0' },
  { name: 'radio1', displayName: '5GHz Radio', hardwareName: 'ath10k', band: 2, interfaceName: 'wlan1' },
];

const RADIO_STATUS = {
  active: true, ssid: 'mesh-net', mode: 'mesh', wifiMode: 2, channel: 36,
  frequency: 5180, bandwidth: 'HT40', encryption: 'SAE', txPower: 20,
  connectedClients: 0, meshPeers: 3,
};

const RADIO_SETTINGS = {
  settings: { ssid: 'mesh-net', channel: '36', bandwidth: 5, txPower: 20, encryption: 1, password: '' },
  availableChannels: ['1', '6', '11', '36', '40'],
  availableBandwidths: [2, 3, 5],
  availableEncryptions: [1, 5],
};

describe('TestWifiRadiosLoading', () => {
  it('renders loading state', () => {
    mockListRadios.mockReturnValue(new Promise(() => {}));
    render(<WifiRadiosPage />);
    expect(screen.getByText('Loading radios...')).toBeTruthy();
  });
});

describe('TestWifiRadiosEmpty', () => {
  it('shows no radios message when list is empty', async () => {
    mockListRadios.mockResolvedValue({ radios: [] });
    render(<WifiRadiosPage />);
    await waitFor(() => {
      expect(screen.getByText('No radios detected.')).toBeTruthy();
    });
  });
});

describe('TestWifiRadiosListError', () => {
  it('shows error when list fails', async () => {
    mockListRadios.mockRejectedValue(new Error('connection refused'));
    render(<WifiRadiosPage />);
    await waitFor(() => {
      expect(screen.getByText(/Failed to load radios/)).toBeTruthy();
    });
  });
});

describe('TestWifiRadiosRender', () => {
  it('renders radio cards with status', async () => {
    mockListRadios.mockResolvedValue({ radios: RADIOS });
    mockGetRadioStatus.mockResolvedValue({ status: RADIO_STATUS });
    mockGetRadioSettings.mockResolvedValue(RADIO_SETTINGS);

    render(<WifiRadiosPage />);
    await waitFor(() => {
      expect(screen.getByText('HaLow Radio')).toBeTruthy();
      expect(screen.getByText('5GHz Radio')).toBeTruthy();
    });
    // Wait for per-card status/settings to load
    await waitFor(() => {
      expect(screen.getAllByText('Active').length).toBeGreaterThanOrEqual(1);
    });
    // SSID from status
    expect(screen.getAllByText(/mesh-net/).length).toBeGreaterThanOrEqual(1);
  });
});

describe('TestWifiRadiosSaveSettings', () => {
  it('shows save button when settings are edited', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIOS[0]] });
    mockGetRadioStatus.mockResolvedValue({ status: RADIO_STATUS });
    mockGetRadioSettings.mockResolvedValue(RADIO_SETTINGS);
    mockUpdateRadioSettings.mockResolvedValue({ success: true });

    render(<WifiRadiosPage />);
    await waitFor(() => screen.getByText('HaLow Radio'));

    // Change SSID to trigger edit mode
    const ssidInput = screen.getByDisplayValue('mesh-net');
    fireEvent.change(ssidInput, { target: { value: 'new-ssid' } });

    expect(screen.getByText('Unsaved changes')).toBeTruthy();
    const saveBtn = screen.getByText('Save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(mockUpdateRadioSettings).toHaveBeenCalledTimes(1);
    });
  });

  it('shows error on save failure', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIOS[0]] });
    mockGetRadioStatus.mockResolvedValue({ status: RADIO_STATUS });
    mockGetRadioSettings.mockResolvedValue(RADIO_SETTINGS);
    mockUpdateRadioSettings.mockRejectedValue(new Error('radio busy'));

    render(<WifiRadiosPage />);
    await waitFor(() => screen.getByText('HaLow Radio'));

    fireEvent.change(screen.getByDisplayValue('mesh-net'), { target: { value: 'x' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(screen.getByText(/Save failed/)).toBeTruthy();
    });
  });
});

describe('TestWifiRadiosCancel', () => {
  it('cancel button reverts changes', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIOS[0]] });
    mockGetRadioStatus.mockResolvedValue({ status: RADIO_STATUS });
    mockGetRadioSettings.mockResolvedValue(RADIO_SETTINGS);

    render(<WifiRadiosPage />);
    await waitFor(() => screen.getByText('HaLow Radio'));

    fireEvent.change(screen.getByDisplayValue('mesh-net'), { target: { value: 'changed' } });
    expect(screen.getByText('Unsaved changes')).toBeTruthy();

    fireEvent.click(screen.getByText('Cancel'));
    // After cancel, the original value should be restored
    expect(screen.getByDisplayValue('mesh-net')).toBeTruthy();
  });
});
