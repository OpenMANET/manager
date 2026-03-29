// =============================================================================
// audioFileTx.test.js — Tests for audio file TX playback service
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { loadFile, startPlayback, stopPlayback, isPlaying } from '../services/audioFileTx.js';
import { SAMPLE_RATE, FRAME_SIZE } from '../constants.js';

beforeEach(() => {
  vi.stubGlobal('AudioData', class {
    constructor() {}
    close() {}
  });
});

afterEach(() => {
  stopPlayback();
  vi.unstubAllGlobals();
});

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

function createMockAudioCtx() {
  const captureNode = {
    connect: vi.fn(),
    disconnect: vi.fn(),
    onaudioprocess: null,
  };
  const sourceNode = {
    connect: vi.fn(),
    disconnect: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
    buffer: null,
    loop: false,
    onended: null,
  };
  const gainNode = {
    connect: vi.fn(),
    disconnect: vi.fn(),
    gain: { value: 1 },
  };
  return {
    ctx: {
      createBufferSource: vi.fn(() => sourceNode),
      createScriptProcessor: vi.fn(() => captureNode),
      createGain: vi.fn(() => gainNode),
      destination: {},
    },
    sourceNode,
    captureNode,
    gainNode,
  };
}

function makeMockProcessEvent() {
  return {
    inputBuffer: {
      getChannelData: () => new Float32Array(1024),
    },
  };
}

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
  it('encodes frames via ScriptProcessorNode onaudioprocess', () => {
    const audioBuffer = createMockAudioBuffer(5);
    const encoder = createMockEncoder();
    const { ctx, captureNode } = createMockAudioCtx();

    startPlayback(audioBuffer, encoder, false, vi.fn(), ctx);
    expect(isPlaying()).toBe(true);

    // Simulate onaudioprocess events (hardware-clock driven in real code)
    captureNode.onaudioprocess(makeMockProcessEvent());
    expect(encoder.encode).toHaveBeenCalledTimes(1);

    captureNode.onaudioprocess(makeMockProcessEvent());
    expect(encoder.encode).toHaveBeenCalledTimes(2);

    captureNode.onaudioprocess(makeMockProcessEvent());
    expect(encoder.encode).toHaveBeenCalledTimes(3);
  });

  it('stops when audio ends in non-loop mode', () => {
    const audioBuffer = createMockAudioBuffer(2);
    const encoder = createMockEncoder();
    const { ctx, sourceNode } = createMockAudioCtx();

    startPlayback(audioBuffer, encoder, false, vi.fn(), ctx);
    expect(isPlaying()).toBe(true);

    // Simulate the source node finishing playback
    sourceNode.onended();
    expect(isPlaying()).toBe(false);
  });

  it('returns early with noop if encoder is closed', () => {
    const audioBuffer = createMockAudioBuffer(2);
    const encoder = { state: 'closed', encode: vi.fn() };
    const { ctx } = createMockAudioCtx();

    const stopFn = startPlayback(audioBuffer, encoder, false, vi.fn(), ctx);
    stopFn(); // should not throw
    expect(encoder.encode).not.toHaveBeenCalled();
  });
});

describe('TestStopPlayback', () => {
  it('clears the timer and sets isPlaying to false', () => {
    const audioBuffer = createMockAudioBuffer(10);
    const encoder = createMockEncoder();
    const { ctx, captureNode } = createMockAudioCtx();

    startPlayback(audioBuffer, encoder, false, vi.fn(), ctx);
    expect(isPlaying()).toBe(true);

    stopPlayback();
    expect(isPlaying()).toBe(false);

    // onaudioprocess after stop should not encode
    const callCount = encoder.encode.mock.calls.length;
    captureNode.onaudioprocess(makeMockProcessEvent());
    expect(encoder.encode).toHaveBeenCalledTimes(callCount);
  });
});

describe('TestIsPlaying', () => {
  it('reflects current playback state', () => {
    expect(isPlaying()).toBe(false);
  });
});

describe('TestLoopMode', () => {
  it('sets sourceNode.loop to true and stays playing', () => {
    const audioBuffer = createMockAudioBuffer(2);
    const encoder = createMockEncoder();
    const logFn = vi.fn();
    const { ctx, sourceNode, captureNode } = createMockAudioCtx();

    startPlayback(audioBuffer, encoder, true, logFn, ctx);

    // Web Audio API handles looping via sourceNode.loop
    expect(sourceNode.loop).toBe(true);
    expect(isPlaying()).toBe(true);

    // onaudioprocess keeps firing — encoder keeps getting called
    captureNode.onaudioprocess(makeMockProcessEvent());
    captureNode.onaudioprocess(makeMockProcessEvent());
    expect(encoder.encode).toHaveBeenCalledTimes(2);
    expect(isPlaying()).toBe(true);

    stopPlayback();
  });
});
