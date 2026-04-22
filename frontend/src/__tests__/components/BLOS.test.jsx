// =============================================================================
// BLOS.test.jsx — Tests for BLOS (VPN) page
// =============================================================================

import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';

const { mockGetBLOSStatus, mockUpdateBLOSConfig } = vi.hoisted(() => ({
  mockGetBLOSStatus: vi.fn(),
  mockUpdateBLOSConfig: vi.fn(),
}));
vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({
    getBLOSStatus: mockGetBLOSStatus,
    updateBLOSConfig: mockUpdateBLOSConfig,
  }),
}));
vi.mock('../../services/connectClient.js', () => ({ transport: {} }));

import BLOSPage from '../../pages/BLOS.jsx';

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  mockGetBLOSStatus.mockReset();
  mockUpdateBLOSConfig.mockReset();
});

describe('TestBLOSLoading', () => {
  it('renders loading state', () => {
    mockGetBLOSStatus.mockReturnValue(new Promise(() => {}));
    render(<BLOSPage />);
    expect(screen.getByText('Loading BLOS status...')).toBeTruthy();
  });
});

describe('TestBLOSStatusEnabled', () => {
  it('shows enabled status', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: true, message: 'connected to headscale' });
    render(<BLOSPage />);
    await waitFor(() => {
      expect(screen.getByText('Enabled')).toBeTruthy();
      expect(screen.getByText(/connected to headscale/)).toBeTruthy();
    });
  });
});

describe('TestBLOSStatusDisabled', () => {
  it('shows disabled status', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: false });
    render(<BLOSPage />);
    await waitFor(() => {
      expect(screen.getByText('Disabled')).toBeTruthy();
    });
    // Toggle should show Off
    expect(screen.getByText('Off')).toBeTruthy();
  });
});

describe('TestBLOSStatusError', () => {
  it('shows error when status fetch fails', async () => {
    mockGetBLOSStatus.mockRejectedValue(new Error('connection refused'));
    render(<BLOSPage />);
    await waitFor(() => {
      expect(screen.getByText('connection refused')).toBeTruthy();
    });
  });
});

describe('TestBLOSToggle', () => {
  it('toggles enable switch', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: false });
    const { container } = render(<BLOSPage />);
    await waitFor(() => screen.getByText('Off'));

    // Click the toggle
    const toggle = container.querySelector('.lat-toggle');
    fireEvent.click(toggle);
    expect(screen.getByText('On')).toBeTruthy();
  });
});

describe('TestBLOSUpdateSuccess', () => {
  it('shows success message after save', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: false });
    mockUpdateBLOSConfig.mockResolvedValue({ success: true, message: 'BLOS enabled' });

    const { container } = render(<BLOSPage />);
    await waitFor(() => screen.getByText('Off'));

    // Toggle on
    const toggle = container.querySelector('.lat-toggle');
    fireEvent.click(toggle);

    // Enter auth key
    const authInput = screen.getByPlaceholderText('Paste auth key');
    fireEvent.change(authInput, { target: { value: 'tskey-auth-abc123' } });

    // Save
    fireEvent.click(screen.getByText('Update BLOS Config'));

    await waitFor(() => {
      expect(screen.getByText('BLOS enabled')).toBeTruthy();
    });
    expect(mockUpdateBLOSConfig).toHaveBeenCalledWith(
      expect.objectContaining({ enableBlos: true, authKey: 'tskey-auth-abc123' }),
    );
  });
});

describe('TestBLOSUpdateWithLoginServer', () => {
  it('sends login server URL when provided', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: true });
    mockUpdateBLOSConfig.mockResolvedValue({ success: true });

    render(<BLOSPage />);
    await waitFor(() => screen.getByText('Enabled'));

    fireEvent.change(screen.getByPlaceholderText('Paste auth key'), {
      target: { value: 'key123' },
    });
    fireEvent.change(screen.getByPlaceholderText('https://headscale.example.com'), {
      target: { value: 'https://hs.local' },
    });
    fireEvent.click(screen.getByText('Update BLOS Config'));

    await waitFor(() => {
      expect(mockUpdateBLOSConfig).toHaveBeenCalledWith(
        expect.objectContaining({ loginServerUrl: 'https://hs.local' }),
      );
    });
  });
});

describe('TestBLOSUpdateFailure', () => {
  it('shows error on update failure', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: false });
    mockUpdateBLOSConfig.mockRejectedValue(new Error('unauthorized'));

    render(<BLOSPage />);
    await waitFor(() => screen.getByText('Off'));

    fireEvent.click(screen.getByText('Update BLOS Config'));

    await waitFor(() => {
      expect(screen.getByText(/Failed to update BLOS/)).toBeTruthy();
    });
  });

  it('shows error when response indicates failure', async () => {
    mockGetBLOSStatus.mockResolvedValue({ blosEnabled: false });
    mockUpdateBLOSConfig.mockResolvedValue({ success: false, message: 'invalid auth key' });

    render(<BLOSPage />);
    await waitFor(() => screen.getByText('Off'));

    fireEvent.click(screen.getByText('Update BLOS Config'));

    await waitFor(() => {
      expect(screen.getByText(/Failed to update BLOS/)).toBeTruthy();
    });
  });
});
