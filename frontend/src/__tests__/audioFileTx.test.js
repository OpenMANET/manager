// =============================================================================
// audioFileTx.test.js — Tests for audio file TX playback service
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { loadFile, startPlayback, stopPlayback, isPlaying } from '../services/audioFileTx.js';
import { SAMPLE_RATE, FRAME_SIZE } from '../constants.js';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  stopPlayback();
  vi.useRealTimers();
});

describe('TestLoadFile', () => {
  it('calls audioCtx.decodeAudioData and returns file info', async () => {
    const mockAudioBuffer = {
      duration: 2.5,
      sampleRate: 48000,
      getChannelData: vi.fn(() => new Float32Array(120000)),
    };

    const mockAudioCtx = {
      decodeAudioData: vi.fn().mockResolvedValue(mockAudioBuffer),
    };

    const mockFile = {
      name: 'test.wav',
      arrayBuffer: vi.fn().mockResolvedValue(new ArrayBuffer(100)),
    };

    const result = await loadFile(mockFile, mockAudioCtx);

    expect(mockFile.arrayBuffer).toHaveBeenCalled();
    expect(mockAudioCtx.decodeAudioData).toHaveBeenCalled();
    expect(result.audioBuffer).toBe(mockAudioBuffer);
    expect(result.duration).toBe(2.5);
    expect(result.sampleRate).toBe(48000);
    expect(result.name).toBe('test.wav');
  });
});

describe('TestStartPlayback', () => {
  function createMockAudioBuffer(numFrames) {
    const totalSamples = numFrames * FRAME_SIZE;
    const pcm = new Float32Array(totalSamples);
    for (let i = 0; i < totalSamples; i++) pcm[i] = 0.5;
    return {
      sampleRate: SAMPLE_RATE,
      getChannelData: () => pcm,
      duration: totalSamples / SAMPLE_RATE,
    };
  }

  function createMockEncoder() {
    return {
      state: 'configured',
      encode: vi.fn(),
    };
  }

  it('begins sending frames at 20ms intervals', () => {
    const audioBuffer = createMockAudioBuffer(5);
    const encoder = createMockEncoder();

    // Mock AudioData global
    vi.stubGlobal('AudioData', class {
      constructor() {}
      close() {}
    });

    startPlayback(audioBuffer, encoder, false, vi.fn());
    expect(isPlaying()).toBe(true);

    // First frame is sent immediately
    expect(encoder.encode).toHaveBeenCalledTimes(1);

    // Advance 20ms — second frame
    vi.advanceTimersByTime(20);
    expect(encoder.encode).toHaveBeenCalledTimes(2);

    // Advance another 20ms — third frame
    vi.advanceTimersByTime(20);
    expect(encoder.encode).toHaveBeenCalledTimes(3);

    vi.unstubAllGlobals();
  });

  it('stops when audio ends in non-loop mode', () => {
    // 2 frames worth of audio
    const audioBuffer = createMockAudioBuffer(2);
    const encoder = createMockEncoder();

    vi.stubGlobal('AudioData', class {
      constructor() {}
      close() {}
    });

    startPlayback(audioBuffer, encoder, false, vi.fn());

    // Frame 1 (immediate), Frame 2 (at 20ms)
    vi.advanceTimersByTime(20);
    expect(encoder.encode).toHaveBeenCalledTimes(2);

    // Frame 3 would be at 40ms but audio should have ended
    vi.advanceTimersByTime(20);
    expect(isPlaying()).toBe(false);

    vi.unstubAllGlobals();
  });

  it('returns early with noop if encoder is closed', () => {
    const audioBuffer = createMockAudioBuffer(2);
    const encoder = { state: 'closed', encode: vi.fn() };

    const stopFn = startPlayback(audioBuffer, encoder, false, vi.fn());
    stopFn(); // should not throw
    expect(encoder.encode).not.toHaveBeenCalled();
  });
});

describe('TestStopPlayback', () => {
  it('clears the timer and sets isPlaying to false', () => {
    vi.stubGlobal('AudioData', class {
      constructor() {}
      close() {}
    });

    const totalSamples = 10 * FRAME_SIZE;
    const pcm = new Float32Array(totalSamples);
    const audioBuffer = {
      sampleRate: SAMPLE_RATE,
      getChannelData: () => pcm,
      duration: totalSamples / SAMPLE_RATE,
    };
    const encoder = { state: 'configured', encode: vi.fn() };

    startPlayback(audioBuffer, encoder, false, vi.fn());
    expect(isPlaying()).toBe(true);

    stopPlayback();
    expect(isPlaying()).toBe(false);

    // No more frames after stop
    const callCount = encoder.encode.mock.calls.length;
    vi.advanceTimersByTime(100);
    expect(encoder.encode).toHaveBeenCalledTimes(callCount);

    vi.unstubAllGlobals();
  });
});

describe('TestIsPlaying', () => {
  it('reflects current playback state', () => {
    expect(isPlaying()).toBe(false);
  });
});

describe('TestLoopMode', () => {
  it('restarts from beginning when audio ends in loop mode', () => {
    vi.stubGlobal('AudioData', class {
      constructor() {}
      close() {}
    });

    // 2 frames of audio
    const totalSamples = 2 * FRAME_SIZE;
    const pcm = new Float32Array(totalSamples);
    const audioBuffer = {
      sampleRate: SAMPLE_RATE,
      getChannelData: () => pcm,
      duration: totalSamples / SAMPLE_RATE,
    };
    const encoder = { state: 'configured', encode: vi.fn() };
    const logFn = vi.fn();

    startPlayback(audioBuffer, encoder, true, logFn);

    // Play through 2 frames
    vi.advanceTimersByTime(20); // frame 2
    vi.advanceTimersByTime(20); // end reached, loop restart, frame 1 again

    expect(isPlaying()).toBe(true);
    // Should have logged the loop restart
    expect(logFn).toHaveBeenCalledWith('File TX loop restart', 'tx');

    stopPlayback();
    vi.unstubAllGlobals();
  });
});
