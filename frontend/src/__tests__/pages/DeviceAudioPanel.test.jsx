// =============================================================================
// DeviceAudioPanel.test.jsx — Device hardware mixer panel
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import React from 'react';

const fetchAudioMixer = vi.fn();
const updateAudioMixer = vi.fn();

vi.mock('../../services/commsApi.js', () => ({
  fetchAudioMixer: (...a) => fetchAudioMixer(...a),
  updateAudioMixer: (...a) => updateAudioMixer(...a),
}));

import DeviceAudioPanel from '../../pages/DeviceAudioPanel.jsx';

const fullState = {
  available: true,
  speakerVolume: 70,
  micVolume: 55,
  agcEnabled: false,
  speakerControl: 'Master',
  micControl: 'Mic Capture Volume',
  agcControl: 'Auto Gain Control',
};

describe('TestDeviceAudioPanel', () => {
  beforeEach(() => {
    fetchAudioMixer.mockReset();
    updateAudioMixer.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders sliders from polled state', async () => {
    fetchAudioMixer.mockResolvedValue(fullState);

    render(<DeviceAudioPanel />);

    await waitFor(() => {
      expect(screen.getByLabelText('Device speaker volume')).toHaveValue('70');
      expect(screen.getByLabelText('Device mic volume')).toHaveValue('55');
    });
  });

  it('shows the no-device empty state when unavailable', async () => {
    fetchAudioMixer.mockResolvedValue({ available: false });

    render(<DeviceAudioPanel />);

    await waitFor(() => {
      expect(screen.getByText('No audio device detected.')).toBeTruthy();
    });
  });

  it('shows an error alert when the fetch fails', async () => {
    fetchAudioMixer.mockResolvedValue(null);

    render(<DeviceAudioPanel />);

    await waitFor(() => {
      expect(screen.getByText(/request failed/i)).toBeTruthy();
    });
  });

  it('commits speaker volume on pointer release, not per tick', async () => {
    fetchAudioMixer.mockResolvedValue(fullState);
    updateAudioMixer.mockResolvedValue({ ...fullState, speakerVolume: 30 });

    render(<DeviceAudioPanel />);
    const slider = await screen.findByLabelText('Device speaker volume');

    fireEvent.change(slider, { target: { value: '30' } });
    expect(updateAudioMixer).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.pointerUp(slider);
    });

    expect(updateAudioMixer).toHaveBeenCalledWith({ speakerVolume: 30 });
  });

  it('toggles AGC with a single call', async () => {
    fetchAudioMixer.mockResolvedValue(fullState);
    updateAudioMixer.mockResolvedValue({ ...fullState, agcEnabled: true });

    render(<DeviceAudioPanel />);
    const toggle = await screen.findByRole('button', { name: /auto gain control/i });

    await act(async () => {
      fireEvent.click(toggle);
    });

    expect(updateAudioMixer).toHaveBeenCalledWith({ agcEnabled: true });
  });

  it('hides the mic slider when the control is absent', async () => {
    fetchAudioMixer.mockResolvedValue({ ...fullState, micVolume: undefined });

    render(<DeviceAudioPanel />);

    await waitFor(() => {
      expect(screen.getByLabelText('Device speaker volume')).toBeTruthy();
    });
    expect(screen.queryByLabelText('Device mic volume')).toBeNull();
  });
});
