// =============================================================================
// SettingsWireless.test.jsx — Tests for the Wireless settings page
// =============================================================================

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';

const {
  mockListRadios, mockGetRadioStatus, mockGetRadioSettings, mockUpdateRadioSettings,
  mockListConnectedClients, mockListMeshPeers, mockGetMeshJoinQR, mockApplyMeshJoin,
} = vi.hoisted(() => ({
  mockListRadios: vi.fn(),
  mockGetRadioStatus: vi.fn(),
  mockGetRadioSettings: vi.fn(),
  mockUpdateRadioSettings: vi.fn(),
  mockListConnectedClients: vi.fn(),
  mockListMeshPeers: vi.fn(),
  mockGetMeshJoinQR: vi.fn(),
  mockApplyMeshJoin: vi.fn(),
}));

vi.mock('@connectrpc/connect', async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...actual,
    createClient: () => ({
      listRadios: mockListRadios,
      getRadioStatus: mockGetRadioStatus,
      getRadioSettings: mockGetRadioSettings,
      updateRadioSettings: mockUpdateRadioSettings,
      listConnectedClients: mockListConnectedClients,
      listMeshPeers: mockListMeshPeers,
      getMeshJoinQR: mockGetMeshJoinQR,
      applyMeshJoin: mockApplyMeshJoin,
    }),
  };
});

vi.mock('../../services/connectClient.js', () => ({ transport: {} }));

import { samplePayload, encodePayload } from '../meshJoinFixtures.js';
import SettingsWireless from '../../pages/SettingsWireless.jsx';
import { dBmToLevel, levelToDbm } from '../../pages/SettingsWireless.power.js';

// The existing suites in this file don't touch mesh join at all, but the
// Share Mesh panel (MeshJoinQR) is now always rendered, so every test needs
// a safe default for the two new RPCs.
beforeEach(() => {
  mockGetMeshJoinQR.mockResolvedValue(QR_RESPONSE);
  mockApplyMeshJoin.mockResolvedValue({ radios: [] });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  mockListRadios.mockReset();
  mockGetRadioStatus.mockReset();
  mockGetRadioSettings.mockReset();
  mockUpdateRadioSettings.mockReset();
  mockListConnectedClients.mockReset();
  mockListMeshPeers.mockReset();
  mockGetMeshJoinQR.mockReset();
  mockApplyMeshJoin.mockReset();
});

const RADIO_AP = {
  name: 'radio2', displayName: '2.4 GHz Radio', hardwareName: 'mt7603e',
  band: 1, interfaceName: 'default_radio2',
};
const RADIO_S1G = {
  name: 'radio0', displayName: 'HaLow Radio', hardwareName: 'morse-micro',
  band: 4, interfaceName: 'halow0',
};

const STATUS_AP = {
  active: true, ssid: 'openmanet', mode: 'AP', wifiMode: 1, channel: 7,
  frequency: 2442, bandwidth: 'HT20', encryption: 'WPA2-PSK', txPower: 17,
  connectedClients: 2, meshPeers: 0,
};

const SETTINGS_AP = {
  settings: {
    ssid: 'openmanet', channel: '7', bandwidth: 2, txPower: 17,
    encryption: 2, country: 'US', mode: 1, // mode = AP
  },
  availableChannels: ['1', '6', '7', '11'],
  availableBandwidths: [1, 2, 5],
  availableEncryptions: [1, 2, 5],
};

const QR_RESPONSE = {
  payloadText: 'OPENMANET1:AAAA',
  svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"></svg>',
  payload: {
    sourceHostname: 'bravo',
    halow: { meshId: 'bravo-mesh', passphrase: 'bravo-pass-1', encryption: 1, bandwidthMhz: 2, channel: 42, countryCode: 'US' },
  },
};

const SETTINGS_S1G = {
  settings: { ssid: 'old-mesh', meshId: 'old-mesh', channel: '42', bandwidth: 15, txPower: 14, encryption: 1, country: 'US', mode: 2 },
  availableChannels: ['12', '28', '44'],
  availableBandwidths: [14, 15, 16, 17],
  availableEncryptions: [1],
};

const SETTINGS_2G_MESH = {
  settings: { ssid: 'old-2g', meshId: 'old-2g', channel: '8', bandwidth: 10, txPower: 20, encryption: 1, country: 'US', mode: 2 },
  availableChannels: ['1', '6', '8', '11'],
  availableBandwidths: [1, 2, 5],
  availableEncryptions: [1, 2],
};

const STATUS_S1G = { active: true, ssid: 'old-mesh', mode: 'Mesh', wifiMode: 2, channel: 42, bandwidth: '2 MHz', encryption: 'WPA3-SAE', txPower: 14, meshPeers: 1 };

// settingsFor routes getRadioSettings by radio name.
function settingsFor(map) {
  mockGetRadioSettings.mockImplementation(({ radioName }) => Promise.resolve(map[radioName]));
}

function pasteCode(text) {
  fireEvent.click(screen.getByRole('button', { name: 'Paste code' }));
  fireEvent.change(screen.getByLabelText('Code text'), { target: { value: text } });
  fireEvent.click(screen.getByRole('button', { name: 'Use code' }));
}

// The default `availableBandwidths` above are Lattice enum ints:
// 14..17 = S1G 1/2/4/8 MHz, 1 = NOHT, 2 = HT20, 5 = HT40, 10 = HE20.

// ── pure helpers ───────────────────────────────────────────────────────────

describe('TestPowerLevelMapping', () => {
  it('snaps a dBm value to the nearest canonical level', () => {
    expect(dBmToLevel(10)).toBe('low');
    expect(dBmToLevel(15)).toBe('medium'); // 15 closer to 17 than 10
    expect(dBmToLevel(17)).toBe('medium');
    expect(dBmToLevel(20)).toBe('high');   // 20 closer to 23 than 17
    expect(dBmToLevel(23)).toBe('high');
    expect(dBmToLevel(28)).toBe('max');
    expect(dBmToLevel(30)).toBe('max');
  });

  it('falls back to medium for null/NaN', () => {
    expect(dBmToLevel(null)).toBe('medium');
    expect(dBmToLevel(undefined)).toBe('medium');
    expect(dBmToLevel(NaN)).toBe('medium');
  });

  it('maps level to canonical dBm', () => {
    expect(levelToDbm('low')).toBe(10);
    expect(levelToDbm('medium')).toBe(17);
    expect(levelToDbm('high')).toBe(23);
    expect(levelToDbm('max')).toBe(30);
    expect(levelToDbm('garbage')).toBe(17);
  });
});

// ── component ──────────────────────────────────────────────────────────────

describe('TestSettingsWirelessLoading', () => {
  it('renders loading state', () => {
    mockListRadios.mockReturnValue(new Promise(() => {}));
    render(<SettingsWireless />);
    expect(screen.getByText(/Loading radios/i)).toBeTruthy();
  });
});

describe('TestSettingsWirelessEmpty', () => {
  it('shows no radios message when list is empty', async () => {
    mockListRadios.mockResolvedValue({ radios: [] });
    render(<SettingsWireless />);
    await waitFor(() => {
      expect(screen.getByText(/No radios detected/i)).toBeTruthy();
    });
  });
});

describe('TestSettingsWirelessListError', () => {
  it('shows error when list fails', async () => {
    mockListRadios.mockRejectedValue(new Error('connection refused'));
    render(<SettingsWireless />);
    await waitFor(() => {
      expect(screen.getByText(/Failed to load radios/)).toBeTruthy();
    });
  });
});

describe('TestSettingsWirelessRender', () => {
  it('renders radio cards with status, putting S1G first', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP, RADIO_S1G] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);

    render(<SettingsWireless />);
    await waitFor(() => {
      expect(screen.getByText('HaLow Radio')).toBeTruthy();
      expect(screen.getByText('2.4 GHz Radio')).toBeTruthy();
    });

    const cards = document.querySelectorAll('.radio-card');
    expect(cards.length).toBeGreaterThanOrEqual(2);
    // S1G should appear first.
    expect(cards[0].textContent).toContain('HaLow Radio');
  });
});

describe('TestSettingsWirelessSave', () => {
  it('shows save button after change and persists on click', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);
    mockUpdateRadioSettings.mockResolvedValue({ success: true });

    render(<SettingsWireless />);
    await waitFor(() => screen.getByText('2.4 GHz Radio'));
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    fireEvent.change(screen.getByDisplayValue('openmanet'), { target: { value: 'new-ssid' } });

    expect(screen.getByText(/Unsaved Changes/i)).toBeTruthy();
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(mockUpdateRadioSettings).toHaveBeenCalledTimes(1);
    });

    const callArg = mockUpdateRadioSettings.mock.calls[0][0];
    expect(callArg.radioName).toBe('radio2');
    expect(callArg.settings.ssid).toBe('new-ssid');
  });

  it('shows error on save failure', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);
    mockUpdateRadioSettings.mockRejectedValue(new Error('radio busy'));

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    fireEvent.change(screen.getByDisplayValue('openmanet'), { target: { value: 'x' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(screen.getByText(/Save failed/)).toBeTruthy();
    });
  });
});

describe('TestSettingsWirelessCancel', () => {
  it('cancel reverts to original value', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    fireEvent.change(screen.getByDisplayValue('openmanet'), { target: { value: 'changed' } });
    expect(screen.getByText(/Unsaved Changes/i)).toBeTruthy();

    fireEvent.click(screen.getByText('Cancel'));
    expect(screen.getByDisplayValue('openmanet')).toBeTruthy();
  });
});

describe('TestSettingsWirelessPowerSelector', () => {
  it('clicking a power level sends the canonical dBm on save', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);
    mockUpdateRadioSettings.mockResolvedValue({ success: true });

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    // Initial power is 17 dBm = Medium. Click "High".
    fireEvent.click(screen.getByText('High'));
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => expect(mockUpdateRadioSettings).toHaveBeenCalledTimes(1));

    const callArg = mockUpdateRadioSettings.mock.calls[0][0];
    expect(callArg.settings.txPower).toBe(23); // canonical dBm for "High"
  });
});

describe('TestSettingsWirelessPowerInitialDisplay', () => {
  function activeLevel(container) {
    const btn = container.querySelector(
      '.power-selector button[role="radio"][aria-checked="true"]',
    );
    return btn?.querySelector('.level')?.textContent ?? null;
  }

  it('seeds the selector from status.txPower when the stored UCI value is 0 (unset)', async () => {
    // Realistic field state: UCI never set `option txpower`, so the backend
    // returns settings.txPower = 0. The radio is actually running at 23 dBm
    // per iwinfo. The selector must reflect the live value, not snap to Low.
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: { ...STATUS_AP, txPower: 23 } });
    mockGetRadioSettings.mockResolvedValue({
      ...SETTINGS_AP,
      settings: { ...SETTINGS_AP.settings, txPower: 0 },
    });

    const { container } = render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    expect(activeLevel(container)).toBe('High');
    // The seeded value matches the seeded original, so the form is not dirty.
    expect(screen.queryByText(/Unsaved Changes/i)).toBeNull();
  });

  it('snaps a non-canonical live status value to the nearest canonical level', async () => {
    // iwinfo can return values that aren't on the 10/17/23/30 grid (e.g. 20).
    // The selector should display the nearest level (High, since 20 is closer
    // to 23 than to 17 by the tie-favours-higher rule).
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: { ...STATUS_AP, txPower: 20 } });
    mockGetRadioSettings.mockResolvedValue({
      ...SETTINGS_AP,
      settings: { ...SETTINGS_AP.settings, txPower: 0 },
    });

    const { container } = render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    expect(activeLevel(container)).toBe('High');
  });

  it('falls back to Medium when both stored and live values are unavailable', async () => {
    // Disabled/uninitialised radios may have no iwinfo reading at all.
    // Default to Medium rather than the misleading Low.
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: { ...STATUS_AP, txPower: 0 } });
    mockGetRadioSettings.mockResolvedValue({
      ...SETTINGS_AP,
      settings: { ...SETTINGS_AP.settings, txPower: 0 },
    });

    const { container } = render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    expect(activeLevel(container)).toBe('Medium');
  });

  it('respects an explicitly stored txPower over the live status value', async () => {
    // If UCI has `option txpower 10` but iwinfo currently reads 23 (e.g. the
    // operator is mid-edit on another device), the operator's stored choice
    // wins — Low must remain selected.
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: { ...STATUS_AP, txPower: 23 } });
    mockGetRadioSettings.mockResolvedValue({
      ...SETTINGS_AP,
      settings: { ...SETTINGS_AP.settings, txPower: 10 },
    });

    const { container } = render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    expect(activeLevel(container)).toBe('Low');
  });
});

describe('TestSettingsWirelessModeSelector', () => {
  it('shows Mode dropdown for non-S1G radios', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    // 'Mode' appears once in the status strip and once as the form label = 2.
    expect(screen.getAllByText('Mode').length).toBe(2);
    const modeBtn = screen.getByRole('button', { name: 'Mode' });
    expect(modeBtn.textContent).toContain('Access Point');
  });

  it('hides Mode dropdown for S1G radios', async () => {
    const settingsS1G = {
      settings: {
        ssid: 'mesh-net', meshId: 'mesh-net', channel: '5',
        bandwidth: 15, txPower: 10, encryption: 1, mode: 2, // mesh
      },
      availableChannels: ['1', '5'],
      availableBandwidths: [14, 15, 16],
      availableEncryptions: [1, 5],
    };
    mockListRadios.mockResolvedValue({ radios: [RADIO_S1G] });
    mockGetRadioStatus.mockResolvedValue({
      status: { ...STATUS_AP, mode: 'Mesh', wifiMode: 2, ssid: 'mesh-net', meshPeers: 3, connectedClients: 0 },
    });
    mockGetRadioSettings.mockResolvedValue(settingsS1G);

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('mesh-net'));

    // S1G card hides the Mode form field, so 'Mode' appears only once
    // (in the status strip) and there is no Mode trigger button.
    expect(screen.getAllByText('Mode').length).toBe(1);
    expect(screen.queryByRole('button', { name: 'Mode' })).toBeNull();
  });

  it('switching to mesh mirrors mesh_id into ssid on save', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);
    mockUpdateRadioSettings.mockResolvedValue({ success: true });

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    // Open the Mode dropdown and click the "Mesh" option.
    fireEvent.click(screen.getByRole('button', { name: 'Mode' }));
    fireEvent.click(screen.getByRole('option', { name: 'Mesh' }));

    // After switching, both the status strip key and the form label say
    // 'Mesh ID' — assert at least one instance is in the document.
    expect(screen.getAllByText('Mesh ID').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => expect(mockUpdateRadioSettings).toHaveBeenCalledTimes(1));
    const callArg = mockUpdateRadioSettings.mock.calls[0][0];
    expect(callArg.settings.mode).toBe(2);
    expect(callArg.settings.meshId).toBe('openmanet');
    expect(callArg.settings.ssid).toBe('openmanet'); // mirrored
  });
});

describe('TestSettingsWirelessEnableToggle', () => {
  it('toggles the disabled flag in the draft', async () => {
    mockListRadios.mockResolvedValue({ radios: [RADIO_AP] });
    mockGetRadioStatus.mockResolvedValue({ status: STATUS_AP });
    mockGetRadioSettings.mockResolvedValue(SETTINGS_AP);
    mockUpdateRadioSettings.mockResolvedValue({ success: true });

    render(<SettingsWireless />);
    await waitFor(() => screen.getByDisplayValue('openmanet'));

    fireEvent.click(screen.getByText('Enabled'));
    expect(screen.getByText(/Unsaved Changes/i)).toBeTruthy();

    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => expect(mockUpdateRadioSettings).toHaveBeenCalledTimes(1));
    expect(mockUpdateRadioSettings.mock.calls[0][0].settings.disabled).toBe(true);
  });
});

describe('TestSettingsWirelessShareMesh', () => {
  function renderMeshNode({ backhaulMode = 2 } = {}) {
    mockListRadios.mockResolvedValue({ radios: [RADIO_S1G, RADIO_AP] });
    mockGetRadioStatus.mockImplementation(({ radioName }) =>
      Promise.resolve({ status: radioName === 'radio0' ? STATUS_S1G : STATUS_AP }));
    settingsFor({
      radio0: SETTINGS_S1G,
      radio2: { ...SETTINGS_2G_MESH, settings: { ...SETTINGS_2G_MESH.settings, mode: backhaulMode } },
    });
    mockGetMeshJoinQR.mockResolvedValue(QR_RESPONSE);
    mockApplyMeshJoin.mockResolvedValue({ radios: [
      { radioName: 'radio0', role: 1, status: 1 },
      { radioName: 'radio2', role: 2, status: 1 },
    ] });
    render(<SettingsWireless />);
    return waitFor(() => screen.getByText('bravo-mesh'));
  }

  it('shows this node\'s QR summary in the Share Mesh panel', async () => {
    await renderMeshNode();
    expect(screen.getByText('Share Mesh')).toBeInTheDocument();
    expect(screen.getByText('bravo')).toBeInTheDocument();
  });

  it('fills the HaLow and mesh-mode 2.4 GHz drafts from a pasted code', async () => {
    await renderMeshNode();
    pasteCode(encodePayload(samplePayload()));

    await waitFor(() => expect(screen.getByDisplayValue('field-mesh')).toBeInTheDocument());
    expect(screen.getByDisplayValue('field-mesh-2g')).toBeInTheDocument();
    expect(screen.getByText(/Filled radio0 \(HaLow\) and radio2 \(backhaul\) from alpha/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Join mesh' })).not.toBeDisabled();
  });

  it('sends one ApplyMeshJoin built from the drafts and names both radios', async () => {
    await renderMeshNode();
    pasteCode(encodePayload(samplePayload()));
    await waitFor(() => screen.getByDisplayValue('field-mesh'));

    // Operator edits the HaLow channel before joining; the edit must be what is sent.
    fireEvent.click(screen.getAllByRole('button', { name: 'Channel' })[0]);
    fireEvent.click(screen.getByRole('option', { name: '28' }));

    fireEvent.click(screen.getByRole('button', { name: 'Join mesh' }));

    await waitFor(() => expect(mockApplyMeshJoin).toHaveBeenCalledTimes(1));
    const req = mockApplyMeshJoin.mock.calls[0][0];
    expect(req.halowRadio).toBe('radio0');
    expect(req.backhaulRadio).toBe('radio2');
    expect(req.payload.halow.meshId).toBe('field-mesh');
    expect(req.payload.halow.passphrase).toBe('correct-horse');
    expect(req.payload.halow.channel).toBe(28);
    expect(req.payload.halow.bandwidthMhz).toBe(8);
    expect(req.payload.backhaul.meshId).toBe('field-mesh-2g');
    expect(req.payload.backhaul.bandwidthMhz).toBe(20);
    expect(mockUpdateRadioSettings).not.toHaveBeenCalled();

    await waitFor(() => expect(screen.getByText(/Joined alpha/)).toBeInTheDocument());
    expect(mockGetMeshJoinQR).toHaveBeenCalledTimes(2);
  });

  it('skips the backhaul when no 2.4 GHz radio is in mesh mode', async () => {
    await renderMeshNode({ backhaulMode: 1 });
    pasteCode(encodePayload(samplePayload()));
    await waitFor(() => screen.getByDisplayValue('field-mesh'));

    expect(screen.getByText(/Backhaul skipped: no 2\.4 GHz radio is in mesh mode/)).toBeInTheDocument();
    expect(screen.queryByDisplayValue('field-mesh-2g')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Join mesh' }));
    await waitFor(() => expect(mockApplyMeshJoin).toHaveBeenCalledTimes(1));
    expect(mockApplyMeshJoin.mock.calls[0][0].backhaulRadio).toBe('');
    expect(mockApplyMeshJoin.mock.calls[0][0].payload.backhaul).toBeUndefined();
  });

  it('warns on a channel the radio does not list and blocks Join mesh', async () => {
    await renderMeshNode();
    const payload = samplePayload();
    payload.halow.channel = 99;
    pasteCode(encodePayload(payload));
    await waitFor(() => screen.getByDisplayValue('field-mesh'));

    expect(screen.getByText(/radio0: Channel 99 is not legal at 8 MHz in US/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Join mesh' })).toBeDisabled();
  });

  it('shows the server rejection when ApplyMeshJoin fails', async () => {
    await renderMeshNode();
    mockApplyMeshJoin.mockRejectedValue(new Error('radio0: channel 44 is not legal at 8 MHz in EU'));
    pasteCode(encodePayload(samplePayload()));
    await waitFor(() => screen.getByDisplayValue('field-mesh'));

    fireEvent.click(screen.getByRole('button', { name: 'Join mesh' }));
    await waitFor(() => expect(screen.getByText(/Join failed: radio0: channel 44/)).toBeInTheDocument());
  });

  it('per-radio Save still uses UpdateRadioSettings', async () => {
    await renderMeshNode();
    mockUpdateRadioSettings.mockResolvedValue({ success: true });
    fireEvent.change(screen.getByDisplayValue('old-mesh'), { target: { value: 'renamed' } });
    fireEvent.click(screen.getAllByText('Save')[0]);
    await waitFor(() => expect(mockUpdateRadioSettings).toHaveBeenCalledTimes(1));
    expect(mockApplyMeshJoin).not.toHaveBeenCalled();
  });
});
