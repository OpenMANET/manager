// =============================================================================
// Settings.test.jsx — Tests for device settings page
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';

// Mock the ConnectRPC transport and dashboard client used by Settings.jsx
// for the restart button. vi.hoisted ensures the fn is available when
// vi.mock factories execute (they are hoisted above imports).
const { mockExecuteQuickAction, mockGetCommsConfig, mockChangePassword, authEnabledRef } = vi.hoisted(() => ({
  mockExecuteQuickAction: vi.fn().mockResolvedValue({ success: true, message: '' }),
  mockGetCommsConfig: vi.fn().mockResolvedValue({ commsEnabled: true, controlSource: 3 }),
  mockChangePassword: vi.fn().mockResolvedValue(undefined),
  authEnabledRef: { current: true },
}));
vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({
    executeQuickAction: mockExecuteQuickAction,
    getCommsConfig: mockGetCommsConfig,
  }),
}));
vi.mock('../../services/connectClient.js', () => ({ transport: {} }));
vi.mock('../../contexts/useAuth.js', () => ({
  useAuth: () => ({
    changePassword: mockChangePassword,
    authEnabled: authEnabledRef.current,
    user: 'root',
    isAuthenticated: true,
    logout: vi.fn(),
  }),
}));

import SettingsPage from '../../pages/Settings.jsx';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  mockExecuteQuickAction.mockReset().mockResolvedValue({ success: true, message: '' });
  mockGetCommsConfig.mockReset().mockResolvedValue({ commsEnabled: true, controlSource: 3 });
  mockChangePassword.mockReset().mockResolvedValue(undefined);
  authEnabledRef.current = true;
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const HOSTNAME_RESPONSE = { hostname: 'my-device' };
const CONFIG_RESPONSE = {
  comms: {
    enable: true,
    controlSource: 'web',
    debug: false,
    talkgroups: 5,
  },
  network: { mesh: true },
};

function mockFetchSuccess(hostnameRes = HOSTNAME_RESPONSE, configRes = CONFIG_RESPONSE) {
  vi.stubGlobal('fetch', vi.fn((url, opts) => {
    if (opts?.method === 'POST') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
    }
    if (url === '/api/settings/hostname') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(hostnameRes) });
    }
    if (url === '/api/settings/config') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(configRes) });
    }
    return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
  }));
}

/** Click the first Lattice toggle switch (Comms Radio is rendered first). */
function clickCommsToggle(container) {
  const toggle = container.querySelector('.lat-toggle');
  fireEvent.click(toggle);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('TestSettingsLoading', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
  });


  it('renders loading spinner', () => {
    render(<SettingsPage />);
    expect(screen.getByText('Loading settings...')).toBeTruthy();
  });
});

describe('TestSettingsFetchSuccess', () => {
  beforeEach(() => mockFetchSuccess());


  it('populates hostname and config after fetch', async () => {
    render(<SettingsPage />);
    await waitFor(() => {
      expect(screen.getByDisplayValue('my-device')).toBeTruthy();
    });
    expect(screen.getByText('OpenMANETd Configuration')).toBeTruthy();
    expect(screen.getByText('Enabled')).toBeTruthy();
  });
});

describe('TestSettingsPartialFetchFailure', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url === '/api/settings/hostname') {
        return Promise.resolve({ ok: false, status: 404 });
      }
      if (url === '/api/settings/config') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(CONFIG_RESPONSE) });
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
    }));
  });


  it('still renders config when hostname fails', async () => {
    render(<SettingsPage />);
    await waitFor(() => {
      expect(screen.getByText('OpenMANETd Configuration')).toBeTruthy();
    });
    // Hostname defaults to empty
    expect(screen.getByPlaceholderText('device-hostname').value).toBe('');
  });
});

describe('TestSettingsHostnameDirty', () => {
  beforeEach(() => mockFetchSuccess());


  it('enables save button when hostname changes', async () => {
    render(<SettingsPage />);
    await waitFor(() => screen.getByDisplayValue('my-device'));

    const saveBtn = screen.getAllByRole('button').find(b => b.textContent === 'Save');
    expect(saveBtn.disabled).toBe(true);

    fireEvent.change(screen.getByDisplayValue('my-device'), {
      target: { value: 'new-host' },
    });
    expect(saveBtn.disabled).toBe(false);
  });
});

describe('TestSettingsConfigDirty', () => {
  beforeEach(() => mockFetchSuccess());


  it('shows unsaved changes when config toggled', async () => {
    const { container } = render(<SettingsPage />);
    await waitFor(() => screen.getByText('Enabled'));

    const saveCfgBtn = screen.getByText('Save Configuration');
    expect(saveCfgBtn.disabled).toBe(true);

    // Click the toggle switch div
    clickCommsToggle(container);

    expect(screen.getByText('Unsaved changes')).toBeTruthy();
    expect(saveCfgBtn.disabled).toBe(false);
  });
});

describe('TestSettingsSaveHostname', () => {


  it('shows success message on save', async () => {
    mockFetchSuccess();
    render(<SettingsPage />);
    await waitFor(() => screen.getByDisplayValue('my-device'));

    fireEvent.change(screen.getByDisplayValue('my-device'), {
      target: { value: 'new-host' },
    });
    const saveBtn = screen.getAllByRole('button').find(b => b.textContent === 'Save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(screen.getByText('Hostname saved.')).toBeTruthy();
    });
  });

  it('shows error on save failure', async () => {
    vi.stubGlobal('fetch', vi.fn((url, opts) => {
      if (opts?.method === 'POST') {
        return Promise.resolve({ ok: false, status: 500 });
      }
      if (url === '/api/settings/hostname') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(HOSTNAME_RESPONSE) });
      }
      if (url === '/api/settings/config') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(CONFIG_RESPONSE) });
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
    }));

    render(<SettingsPage />);
    await waitFor(() => screen.getByDisplayValue('my-device'));

    fireEvent.change(screen.getByDisplayValue('my-device'), {
      target: { value: 'new-host' },
    });
    const saveBtn = screen.getAllByRole('button').find(b => b.textContent === 'Save');
    fireEvent.click(saveBtn);

    await waitFor(() => {
      expect(screen.getByText(/Failed to save hostname/)).toBeTruthy();
    });
  });
});

describe('TestSettingsSaveConfig', () => {


  it('shows success on config save', async () => {
    mockFetchSuccess();
    const { container } = render(<SettingsPage />);
    await waitFor(() => screen.getByText('Enabled'));

    clickCommsToggle(container);
    fireEvent.click(screen.getByText('Save Configuration'));

    await waitFor(() => {
      expect(screen.getByText('Configuration saved.')).toBeTruthy();
    });
  });

  it('shows error on config save failure', async () => {
    vi.stubGlobal('fetch', vi.fn((url, opts) => {
      if (opts?.method === 'POST' && url === '/api/settings/config') {
        return Promise.resolve({ ok: false, status: 500 });
      }
      if (url === '/api/settings/hostname') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(HOSTNAME_RESPONSE) });
      }
      if (url === '/api/settings/config') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(CONFIG_RESPONSE) });
      }
      return Promise.resolve({ ok: true, json: () => Promise.resolve({}) });
    }));

    const { container } = render(<SettingsPage />);
    await waitFor(() => screen.getByText('Enabled'));

    clickCommsToggle(container);
    fireEvent.click(screen.getByText('Save Configuration'));

    await waitFor(() => {
      expect(screen.getByText(/Failed to save config/)).toBeTruthy();
    });
  });
});

describe('TestSettingsRestart', () => {
  beforeEach(() => mockFetchSuccess());

  it('shows restarting text and success message after click', async () => {
    render(<SettingsPage />);
    await waitFor(() => screen.getByText('Restart openmanetd'));

    fireEvent.click(screen.getByText('Restart openmanetd'));

    await waitFor(() => {
      expect(screen.getByText('openmanetd restart requested.')).toBeTruthy();
    });
    // Button shows "Restarting..." while disabled
    expect(screen.getByText('Restarting...')).toBeTruthy();
    // Verify ConnectRPC was called
    expect(mockExecuteQuickAction).toHaveBeenCalledTimes(1);
  });

  it('shows error when restart fails', async () => {
    mockExecuteQuickAction.mockResolvedValueOnce({ success: false, message: 'service unavailable' });

    render(<SettingsPage />);
    await waitFor(() => screen.getByText('Restart openmanetd'));

    fireEvent.click(screen.getByText('Restart openmanetd'));

    await waitFor(() => {
      expect(screen.getByText(/Failed to restart/)).toBeTruthy();
    });
  });
});

describe('TestSettingsRawYaml', () => {
  beforeEach(() => mockFetchSuccess());


  it('toggles raw YAML display', async () => {
    const { container } = render(<SettingsPage />);
    await waitFor(() => screen.getByText('OpenMANETd Configuration'));

    expect(container.querySelector('pre')).toBeNull();

    fireEvent.click(screen.getByText('Raw Configuration (YAML)'));
    expect(container.querySelector('pre')).toBeTruthy();

    fireEvent.click(screen.getByText('Raw Configuration (YAML)'));
    expect(container.querySelector('pre')).toBeNull();
  });
});

describe('TestSettingsControlSource', () => {
  beforeEach(() => mockFetchSuccess());


  it('changes control source dropdown', async () => {
    render(<SettingsPage />);
    await waitFor(() => screen.getByText('OpenMANETd Configuration'));

    const select = screen.getByDisplayValue('Web UI');
    fireEvent.change(select, { target: { value: 'openvlm' } });
    expect(screen.getByDisplayValue('OpenVLM (default)')).toBeTruthy();
    expect(screen.getByText('Unsaved changes')).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Passphrase panel
// ---------------------------------------------------------------------------

describe('TestSettingsPassphrasePanel', () => {
  beforeEach(() => mockFetchSuccess());

  async function waitForPanel() {
    await waitFor(() => screen.getByText('Passphrase'));
  }

  it('renders the Passphrase panel when auth is enabled', async () => {
    render(<SettingsPage />);
    await waitForPanel();
    expect(screen.getByLabelText('Current Passphrase')).toBeTruthy();
    expect(screen.getByLabelText('New Passphrase')).toBeTruthy();
    expect(screen.getByLabelText('Confirm New Passphrase')).toBeTruthy();
    expect(screen.getByRole('button', { name: /Update Passphrase/i })).toBeTruthy();
  });

  it('hides the Passphrase panel when authEnabled is false', async () => {
    authEnabledRef.current = false;
    render(<SettingsPage />);
    await waitFor(() => screen.getByText('Hostname'));
    expect(screen.queryByText('Passphrase')).toBeNull();
  });

  it('submits current + new passphrase on success and clears fields', async () => {
    render(<SettingsPage />);
    await waitForPanel();

    fireEvent.change(screen.getByLabelText('Current Passphrase'), { target: { value: '' } });
    fireEvent.change(screen.getByLabelText('New Passphrase'), { target: { value: 'alpha123' } });
    fireEvent.change(screen.getByLabelText('Confirm New Passphrase'), { target: { value: 'alpha123' } });

    fireEvent.click(screen.getByRole('button', { name: /Update Passphrase/i }));

    await waitFor(() => {
      expect(mockChangePassword).toHaveBeenCalledWith('', 'alpha123');
    });
    await waitFor(() => {
      expect(screen.getByText('Passphrase updated.')).toBeTruthy();
    });
    expect(screen.getByLabelText('New Passphrase').value).toBe('');
    expect(screen.getByLabelText('Confirm New Passphrase').value).toBe('');
  });

  it('shows a mismatch error and does not call changePassword when confirm differs', async () => {
    render(<SettingsPage />);
    await waitForPanel();

    fireEvent.change(screen.getByLabelText('New Passphrase'), { target: { value: 'alpha123' } });
    fireEvent.change(screen.getByLabelText('Confirm New Passphrase'), { target: { value: 'alpha999' } });

    fireEvent.click(screen.getByRole('button', { name: /Update Passphrase/i }));

    await waitFor(() => {
      expect(screen.getByText('Passphrases do not match.')).toBeTruthy();
    });
    expect(mockChangePassword).not.toHaveBeenCalled();
  });

  it('disables the submit button when new or confirm is empty', async () => {
    render(<SettingsPage />);
    await waitForPanel();
    const button = screen.getByRole('button', { name: /Update Passphrase/i });
    expect(button.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('New Passphrase'), { target: { value: 'alpha' } });
    expect(button.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText('Confirm New Passphrase'), { target: { value: 'alpha' } });
    expect(button.disabled).toBe(false);
  });

  it('renders a server error when the RPC rejects', async () => {
    mockChangePassword.mockRejectedValueOnce(new Error('invalid credentials'));
    render(<SettingsPage />);
    await waitForPanel();

    fireEvent.change(screen.getByLabelText('Current Passphrase'), { target: { value: 'wrong' } });
    fireEvent.change(screen.getByLabelText('New Passphrase'), { target: { value: 'alpha123' } });
    fireEvent.change(screen.getByLabelText('Confirm New Passphrase'), { target: { value: 'alpha123' } });

    fireEvent.click(screen.getByRole('button', { name: /Update Passphrase/i }));

    await waitFor(() => {
      expect(screen.getByText('invalid credentials')).toBeTruthy();
    });
    expect(mockChangePassword).toHaveBeenCalledTimes(1);
  });
});
