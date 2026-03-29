// =============================================================================
// audioEngine.test.js — Tests for the core audio engine
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let engine;

// Reusable mock factories
function createMockGainNode() {
  return {
    gain: { value: 0 },
    connect: vi.fn(),
    disconnect: vi.fn(),
  };
}

function createMockAudioContext() {
  const gainNode = createMockGainNode();
  return {
    sampleRate: 48000,
    destination: {},
    createGain: vi.fn(() => createMockGainNode()),
    createScriptProcessor: vi.fn(() => ({
      connect: vi.fn(),
      disconnect: vi.fn(),
      onaudioprocess: null,
    })),
    createMediaStreamSource: vi.fn(() => ({
      connect: vi.fn(),
      disconnect: vi.fn(),
    })),
    createBuffer: vi.fn((ch, len, sr) => ({
      getChannelData: vi.fn(() => ({ set: vi.fn() })),
    })),
    createBufferSource: vi.fn(() => ({
      connect: vi.fn(),
      start: vi.fn(),
      stop: vi.fn(),
      buffer: null,
    })),
    audioWorklet: {
      addModule: vi.fn().mockRejectedValue(new Error('no worklet in test')),
    },
    _gainNode: gainNode,
  };
}

let mockCtx;

beforeEach(async () => {
  vi.resetModules();

  mockCtx = createMockAudioContext();
  vi.stubGlobal('AudioContext', vi.fn(() => mockCtx));
  vi.stubGlobal('webkitAudioContext', undefined);

  // AudioDecoder mock
  vi.stubGlobal('AudioDecoder', vi.fn(function (opts) {
    this._output = opts.output;
    this._error = opts.error;
    this.state = 'configured';
    this.configure = vi.fn();
    this.decode = vi.fn();
    this.close = vi.fn();
  }));

  // AudioEncoder mock
  vi.stubGlobal('AudioEncoder', vi.fn(function (opts) {
    this._output = opts.output;
    this._error = opts.error;
    this.state = 'configured';
    this.configure = vi.fn();
    this.encode = vi.fn();
    this.close = vi.fn();
  }));

  vi.stubGlobal('EncodedAudioChunk', vi.fn(function (opts) {
    this.type = opts.type;
    this.timestamp = opts.timestamp;
    this.data = opts.data;
    this.byteLength = opts.data?.byteLength || 0;
    this.copyTo = vi.fn((buf) => {
      new Uint8Array(buf).set(new Uint8Array(this.data));
    });
  }));

  vi.stubGlobal('AudioData', vi.fn(function (opts) {
    this.format = opts?.format;
    this.sampleRate = opts?.sampleRate;
    this.numberOfFrames = opts?.numberOfFrames;
    this.numberOfChannels = opts?.numberOfChannels;
    this.timestamp = opts?.timestamp;
    this.data = opts?.data;
    this.close = vi.fn();
    this.copyTo = vi.fn((dest) => {
      if (this.data) dest.set(this.data);
    });
  }));

  vi.stubGlobal('AudioWorkletNode', undefined);
  vi.stubGlobal('SharedArrayBuffer', undefined);

  vi.stubGlobal('navigator', {
    mediaDevices: {
      getUserMedia: vi.fn().mockResolvedValue({
        getTracks: () => [{ stop: vi.fn() }],
      }),
      enumerateDevices: vi.fn().mockResolvedValue([]),
    },
  });

  engine = await import('../services/audioEngine.js');
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// initAudio
// ---------------------------------------------------------------------------

describe('TestAudioEngineInit', () => {
  it('creates AudioContext at 48kHz', async () => {
    await engine.initAudio(vi.fn());
    expect(AudioContext).toHaveBeenCalledWith({ sampleRate: 48000 });
  });

  it('is idempotent — second call returns same result', async () => {
    const result1 = await engine.initAudio(vi.fn());
    const result2 = await engine.initAudio(vi.fn());
    expect(AudioContext).toHaveBeenCalledTimes(1);
    expect(result1).toEqual(result2);
  });

  it('falls back to ScriptProcessor when no AudioWorklet', async () => {
    const logFn = vi.fn();
    const result = await engine.initAudio(logFn);
    expect(result.useWorklet).toBe(false);
    expect(mockCtx.createScriptProcessor).toHaveBeenCalled();
  });

  it('sets up Opus decoder', async () => {
    const logFn = vi.fn();
    await engine.initAudio(logFn);
    expect(AudioDecoder).toHaveBeenCalled();
    expect(logFn).toHaveBeenCalledWith('Opus decoder ready', 'info');
  });

  it('sets up Opus encoder', async () => {
    const logFn = vi.fn();
    await engine.initAudio(logFn);
    expect(AudioEncoder).toHaveBeenCalled();
    expect(logFn).toHaveBeenCalledWith('Opus encoder ready', 'info');
  });

  it('handles missing WebCodecs gracefully', async () => {
    vi.stubGlobal('AudioDecoder', undefined);
    vi.resetModules();
    const eng = await import('../services/audioEngine.js');

    const logFn = vi.fn();
    await eng.initAudio(logFn);
    expect(logFn).toHaveBeenCalledWith('WebCodecs not available — RX disabled', 'err');
  });
});

// ---------------------------------------------------------------------------
// setVolume / setMicGain
// ---------------------------------------------------------------------------

describe('TestAudioEngineVolume', () => {
  it('setVolume sets gain node value', async () => {
    await engine.initAudio(vi.fn());
    // The gain node is created via createGain, find it
    const gainCall = mockCtx.createGain.mock.results[0].value;
    engine.setVolume(50);
    expect(gainCall.gain.value).toBe(0.5);
  });

  it('setMicGain stores gain value', () => {
    // setMicGain doesn't need initAudio
    expect(() => engine.setMicGain(60)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// decodeAndPlay
// ---------------------------------------------------------------------------

describe('TestAudioEngineDecodeAndPlay', () => {
  it('calls decoder.decode with EncodedAudioChunk', async () => {
    await engine.initAudio(vi.fn());
    const opusData = new Uint8Array([1, 2, 3]);
    engine.decodeAndPlay(opusData, 1, '10.0.0.1');
    expect(EncodedAudioChunk).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'key', timestamp: 0 })
    );
  });

  it('resets timestamp on source change', async () => {
    await engine.initAudio(vi.fn());
    engine.decodeAndPlay(new Uint8Array([1]), 1, '10.0.0.1');
    engine.decodeAndPlay(new Uint8Array([2]), 1, '10.0.0.1');
    // Second call should have timestamp 20000 (20ms in microseconds)
    const secondChunk = EncodedAudioChunk.mock.calls[1][0];
    expect(secondChunk.timestamp).toBe(20000);

    // Change source — timestamp resets
    engine.decodeAndPlay(new Uint8Array([3]), 2, '10.0.0.2');
    const thirdChunk = EncodedAudioChunk.mock.calls[2][0];
    expect(thirdChunk.timestamp).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// enumerateDevices
// ---------------------------------------------------------------------------

describe('TestAudioEngineEnumerateDevices', () => {
  it('separates inputs and outputs', async () => {
    navigator.mediaDevices.enumerateDevices.mockResolvedValue([
      { kind: 'audioinput', deviceId: 'mic1', label: 'Mic' },
      { kind: 'audiooutput', deviceId: 'spk1', label: 'Speaker' },
      { kind: 'videoinput', deviceId: 'cam1', label: 'Camera' },
    ]);

    const result = await engine.enumerateDevices();
    expect(result.inputs).toHaveLength(1);
    expect(result.outputs).toHaveLength(1);
    expect(result.inputs[0].deviceId).toBe('mic1');
    expect(result.outputs[0].deviceId).toBe('spk1');
  });

  it('returns empty arrays when mediaDevices unavailable', async () => {
    vi.stubGlobal('navigator', {});
    vi.resetModules();
    const eng = await import('../services/audioEngine.js');

    const result = await eng.enumerateDevices();
    expect(result).toEqual({ inputs: [], outputs: [] });
  });
});

// ---------------------------------------------------------------------------
// startMic / stopMic
// ---------------------------------------------------------------------------

describe('TestAudioEngineMic', () => {
  it('startMic acquires microphone with constraints', async () => {
    await engine.initAudio(vi.fn());
    await engine.startMic(vi.fn(), vi.fn());
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledWith({
      audio: expect.objectContaining({
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
      }),
    });
  });

  it('stopMic releases mic tracks', async () => {
    const mockTrack = { stop: vi.fn() };
    navigator.mediaDevices.getUserMedia.mockResolvedValue({
      getTracks: () => [mockTrack],
    });

    await engine.initAudio(vi.fn());
    await engine.startMic(vi.fn(), vi.fn());
    engine.stopMic();
    expect(mockTrack.stop).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// playBuffer
// ---------------------------------------------------------------------------

describe('TestAudioEnginePlayBuffer', () => {
  it('creates buffer source and plays', async () => {
    await engine.initAudio(vi.fn());
    const pcm = new Float32Array([0.1, 0.2, 0.3]);
    const src = engine.playBuffer(pcm);
    expect(mockCtx.createBuffer).toHaveBeenCalledWith(1, 3, 48000);
    expect(mockCtx.createBufferSource).toHaveBeenCalled();
    expect(src).toBeTruthy();
  });

  it('returns null for empty pcm', async () => {
    await engine.initAudio(vi.fn());
    expect(engine.playBuffer(null)).toBeNull();
    expect(engine.playBuffer(new Float32Array(0))).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// resetTxTimestamp
// ---------------------------------------------------------------------------

describe('TestAudioEngineResetTxTimestamp', () => {
  it('resets without error', () => {
    expect(() => engine.resetTxTimestamp()).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// encoder callback
// ---------------------------------------------------------------------------

describe('TestAudioEngineEncoderCallback', () => {
  it('set and clear encoder callback', () => {
    const cb = vi.fn();
    engine.setEncoderCallback(cb);
    engine.clearEncoderCallback();
    // No direct assertion on internal state, but ensure no errors
  });
});

// ---------------------------------------------------------------------------
// getAudioContext / getEncoder
// ---------------------------------------------------------------------------

describe('TestAudioEngineGetters', () => {
  it('getAudioContext returns null before init', () => {
    expect(engine.getAudioContext()).toBeNull();
  });

  it('getAudioContext returns context after init', async () => {
    await engine.initAudio(vi.fn());
    expect(engine.getAudioContext()).toBeTruthy();
  });

  it('getEncoder returns encoder after init', async () => {
    await engine.initAudio(vi.fn());
    expect(engine.getEncoder()).toBeTruthy();
    expect(engine.getEncoder().state).toBe('configured');
  });
});
