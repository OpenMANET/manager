// =============================================================================
// AudioFileTx.test.jsx — Tests for audio file TX panel component
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act, cleanup } from '@testing-library/react';

// Mock service modules before importing component
vi.mock('../../services/audioFileTx.js', () => ({
  loadFile: vi.fn(),
  startPlayback: vi.fn(),
  stopPlayback: vi.fn(),
  isPlaying: vi.fn(() => false),
}));

vi.mock('../../services/audioEngine.js', () => ({
  getAudioContext: vi.fn(),
  getEncoder: vi.fn(),
  resetTxTimestamp: vi.fn(),
}));

import AudioFileTxPanel from '../../components/AudioFileTx.jsx';
import { loadFile, startPlayback, stopPlayback, isPlaying } from '../../services/audioFileTx.js';
import { getAudioContext, getEncoder, resetTxTimestamp } from '../../services/audioEngine.js';

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const defaultProps = {
  onLog: vi.fn(),
  onPttSet: vi.fn(),
  txEnabled: { 1: true, 2: false, 3: false, 4: false, 5: false },
};

function renderPanel(overrides = {}) {
  return render(<AudioFileTxPanel {...defaultProps} {...overrides} />);
}

describe('TestAudioFileTxRender', () => {
  it('renders with play and stop buttons disabled', () => {
    renderPanel();
    const playBtn = screen.getByText('Play');
    const stopBtn = screen.getByText('Stop');
    expect(playBtn.disabled).toBe(true);
    expect(stopBtn.disabled).toBe(true);
  });

  it('renders loop checkbox unchecked', () => {
    renderPanel();
    const loopCb = screen.getByLabelText('Loop');
    expect(loopCb.checked).toBe(false);
  });
});

describe('TestAudioFileTxFileLoad', () => {
  it('enables play on successful file load', async () => {
    getAudioContext.mockReturnValue({});
    loadFile.mockResolvedValue({
      audioBuffer: { duration: 5.2, sampleRate: 48000 },
      name: 'test.wav',
      duration: 5.2,
      sampleRate: 48000,
    });

    renderPanel();
    const fileInput = document.querySelector('input[type="file"]');

    await act(async () => {
      fireEvent.change(fileInput, {
        target: { files: [new File(['audio'], 'test.wav', { type: 'audio/wav' })] },
      });
    });

    expect(screen.getByText('Play').disabled).toBe(false);
    expect(screen.getByText(/test\.wav/)).toBeTruthy();
  });

  it('shows error when audio not initialized', async () => {
    getAudioContext.mockReturnValue(null);
    renderPanel();
    const fileInput = document.querySelector('input[type="file"]');

    await act(async () => {
      fireEvent.change(fileInput, {
        target: { files: [new File(['audio'], 'test.wav', { type: 'audio/wav' })] },
      });
    });

    expect(screen.getByText('Audio not initialized')).toBeTruthy();
    expect(defaultProps.onLog).toHaveBeenCalledWith('Audio not initialized for file TX', 'err');
  });

  it('shows error on decode failure', async () => {
    getAudioContext.mockReturnValue({});
    loadFile.mockRejectedValue(new Error('Bad format'));
    renderPanel();
    const fileInput = document.querySelector('input[type="file"]');

    await act(async () => {
      fireEvent.change(fileInput, {
        target: { files: [new File(['audio'], 'bad.mp3', { type: 'audio/mp3' })] },
      });
    });

    expect(screen.getByText('Error: Bad format')).toBeTruthy();
    expect(screen.getByText('Play').disabled).toBe(true);
  });
});

describe('TestAudioFileTxPlay', () => {
  async function loadFileAndReady(props = {}) {
    getAudioContext.mockReturnValue({});
    getEncoder.mockReturnValue({ state: 'configured' });
    loadFile.mockResolvedValue({
      audioBuffer: { duration: 3.0, sampleRate: 48000 },
      name: 'clip.wav',
      duration: 3.0,
      sampleRate: 48000,
    });

    const result = renderPanel(props);
    const fileInput = document.querySelector('input[type="file"]');
    await act(async () => {
      fireEvent.change(fileInput, {
        target: { files: [new File(['audio'], 'clip.wav', { type: 'audio/wav' })] },
      });
    });
    return result;
  }

  it('logs error when no TX channels enabled', async () => {
    const onLog = vi.fn();
    await loadFileAndReady({
      onLog,
      txEnabled: { 1: false, 2: false, 3: false, 4: false, 5: false },
    });

    fireEvent.click(screen.getByText('Play'));
    expect(onLog).toHaveBeenCalledWith('No TX channels!', 'err');
    expect(startPlayback).not.toHaveBeenCalled();
  });

  it('logs error when encoder not available', async () => {
    const onLog = vi.fn();
    await loadFileAndReady({ onLog });

    // Override encoder to closed state after file is loaded
    getEncoder.mockReturnValue({ state: 'closed' });
    fireEvent.click(screen.getByText('Play'));
    expect(onLog).toHaveBeenCalledWith('Encoder not available', 'err');
  });

  it('starts playback with correct calls', async () => {
    const onPttSet = vi.fn();
    startPlayback.mockReturnValue(vi.fn());
    await loadFileAndReady({ onPttSet });

    fireEvent.click(screen.getByText('Play'));

    expect(onPttSet).toHaveBeenCalledWith(true);
    expect(resetTxTimestamp).toHaveBeenCalled();
    expect(startPlayback).toHaveBeenCalled();
    expect(screen.getByText('Play').disabled).toBe(true);
    expect(screen.getByText('Stop').disabled).toBe(false);
  });

  it('stops playback on stop click', async () => {
    const onPttSet = vi.fn();
    startPlayback.mockReturnValue(vi.fn());
    await loadFileAndReady({ onPttSet });

    fireEvent.click(screen.getByText('Play'));
    fireEvent.click(screen.getByText('Stop'));

    expect(stopPlayback).toHaveBeenCalled();
    expect(onPttSet).toHaveBeenCalledWith(false);
    expect(screen.getByText('Play').disabled).toBe(false);
  });
});

describe('TestAudioFileTxLoop', () => {
  it('toggles loop checkbox', () => {
    renderPanel();
    const loopCb = screen.getByLabelText('Loop');
    expect(loopCb.checked).toBe(false);
    fireEvent.click(loopCb);
    expect(loopCb.checked).toBe(true);
    fireEvent.click(loopCb);
    expect(loopCb.checked).toBe(false);
  });
});
