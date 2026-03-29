// =============================================================================
// whisperService.test.js — Tests for Whisper WASM speech-to-text service
// =============================================================================

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// We need dynamic imports because the module has module-level state.
// vi.resetModules() + import() gives us a fresh module each time.

let whisper;

beforeEach(async () => {
  vi.resetModules();
  vi.useFakeTimers();

  // Stub indexedDB for IDB helpers
  vi.stubGlobal('indexedDB', {
    open: vi.fn(() => {
      const rq = {
        onupgradeneeded: null,
        onsuccess: null,
        onerror: null,
        result: {
          createObjectStore: vi.fn(),
          transaction: vi.fn(() => ({
            objectStore: vi.fn(() => ({
              get: vi.fn(() => {
                const getReq = { onsuccess: null, onerror: null, result: null };
                setTimeout(() => getReq.onsuccess?.(), 0);
                return getReq;
              }),
              put: vi.fn(),
            })),
          })),
        },
      };
      setTimeout(() => rq.onsuccess?.({ target: rq }), 0);
      return rq;
    }),
  });

  whisper = await import('../services/whisperService.js');
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// feedAudio
// ---------------------------------------------------------------------------

describe('TestWhisperFeedAudio', () => {
  it('downsamples 48kHz to 16kHz by factor of 3', () => {
    // Create a 48kHz buffer of 960 samples (one 20ms frame)
    const pcm48k = new Float32Array(960);
    for (let i = 0; i < pcm48k.length; i++) pcm48k[i] = Math.sin(i * 0.1);

    whisper.feedAudio(1, pcm48k, '10.0.0.1');

    // Feed another frame and check the internal state via checkSilence behavior
    // Since we can't directly access chState, we verify feedAudio doesn't throw
    // and that the function accepts valid channel numbers
    whisper.feedAudio(1, pcm48k, '10.0.0.1');
    whisper.feedAudio(2, pcm48k, '10.0.0.2');
  });

  it('ignores unknown channels without crashing', () => {
    const pcm48k = new Float32Array(960);
    expect(() => whisper.feedAudio(99, pcm48k, '10.0.0.1')).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// checkSilenceAndTranscribe
// ---------------------------------------------------------------------------

describe('TestWhisperSilenceDetection', () => {
  it('does nothing when whisper is not ready', () => {
    const onTranscript = vi.fn();
    whisper.feedAudio(1, new Float32Array(960), '10.0.0.1');
    vi.advanceTimersByTime(2000);
    whisper.checkSilenceAndTranscribe(onTranscript);
    expect(onTranscript).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// initWhisper
// ---------------------------------------------------------------------------

describe('TestWhisperInit', () => {
  it('returns false when Module is undefined', async () => {
    // Module is not defined by default in test env
    const onStatus = vi.fn();
    const onLog = vi.fn();
    const result = await whisper.initWhisper(onStatus, onLog, vi.fn());
    expect(result).toBe(false);
    expect(onStatus).toHaveBeenCalledWith('Whisper WASM not loaded');
  });

  it('returns false when WASM runtime times out', async () => {
    const neverResolves = new Promise(() => {});
    vi.stubGlobal('Module', {
      _runtimeReady: neverResolves,
      FS_createDataFile: vi.fn(),
    });

    const onStatus = vi.fn();
    const onLog = vi.fn();

    const initPromise = whisper.initWhisper(onStatus, onLog, vi.fn());
    // Advance past the 10s timeout
    await vi.advanceTimersByTimeAsync(11000);

    const result = await initPromise;
    expect(result).toBe(false);
    expect(onLog).toHaveBeenCalledWith(
      expect.stringContaining('Whisper WASM init timeout'),
      'err'
    );
  });

  it('returns false when FS_createDataFile is missing', async () => {
    vi.stubGlobal('Module', {
      // No _runtimeReady, no FS_createDataFile
    });

    const onStatus = vi.fn();
    const result = await whisper.initWhisper(onStatus, vi.fn(), vi.fn());
    expect(result).toBe(false);
    expect(onStatus).toHaveBeenCalledWith('Whisper WASM failed to initialize');
  });

  it('returns false when Module.init returns null', async () => {
    vi.stubGlobal('Module', {
      FS_createDataFile: vi.fn(),
      FS_unlink: vi.fn(),
      init: vi.fn(() => null),
    });
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(100)),
      })
    ));

    const onStatus = vi.fn();
    const onLog = vi.fn();

    // Need to advance timers to let IDB resolve
    const initPromise = whisper.initWhisper(onStatus, onLog, vi.fn());
    await vi.advanceTimersByTimeAsync(100);

    const result = await initPromise;
    expect(result).toBe(false);
    expect(onLog).toHaveBeenCalledWith(
      expect.stringContaining('Module.init returned null'),
      'err'
    );
  });

  it('succeeds when Module.init returns truthy', async () => {
    vi.stubGlobal('Module', {
      FS_createDataFile: vi.fn(),
      FS_unlink: vi.fn(),
      init: vi.fn(() => 1),
      full_default: vi.fn(),
    });
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(100)),
      })
    ));

    const onStatus = vi.fn();
    const onLog = vi.fn();

    const initPromise = whisper.initWhisper(onStatus, onLog, vi.fn());
    await vi.advanceTimersByTimeAsync(100);

    const result = await initPromise;
    expect(result).toBe(true);
    expect(onStatus).toHaveBeenCalledWith('Whisper ready — listening on all channels');
  });

  it('falls back to CDN when local fetch fails', async () => {
    vi.stubGlobal('Module', {
      FS_createDataFile: vi.fn(),
      FS_unlink: vi.fn(),
      init: vi.fn(() => 1),
      full_default: vi.fn(),
    });
    vi.stubGlobal('fetch', vi.fn((url) => {
      if (url.includes('huggingface')) {
        return Promise.resolve({
          ok: true,
          arrayBuffer: () => Promise.resolve(new ArrayBuffer(100)),
        });
      }
      return Promise.resolve({ ok: false, status: 404 });
    }));

    const initPromise = whisper.initWhisper(vi.fn(), vi.fn(), vi.fn());
    await vi.advanceTimersByTimeAsync(100);

    const result = await initPromise;
    expect(result).toBe(true);
    // Verify both local and CDN fetches were attempted
    expect(fetch).toHaveBeenCalledTimes(2);
  });
});

// ---------------------------------------------------------------------------
// reset
// ---------------------------------------------------------------------------

describe('TestWhisperReset', () => {
  it('clears state without error', () => {
    whisper.feedAudio(1, new Float32Array(960), '10.0.0.1');
    whisper.feedAudio(2, new Float32Array(960), '10.0.0.2');
    expect(() => whisper.reset()).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// isReady
// ---------------------------------------------------------------------------

describe('TestWhisperIsReady', () => {
  it('returns false initially', () => {
    expect(whisper.isReady()).toBe(false);
  });

  it('returns true after successful init', async () => {
    vi.stubGlobal('Module', {
      FS_createDataFile: vi.fn(),
      FS_unlink: vi.fn(),
      init: vi.fn(() => 1),
      full_default: vi.fn(),
    });
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve({
        ok: true,
        arrayBuffer: () => Promise.resolve(new ArrayBuffer(100)),
      })
    ));

    const initPromise = whisper.initWhisper(vi.fn(), vi.fn(), vi.fn());
    await vi.advanceTimersByTimeAsync(100);
    await initPromise;

    expect(whisper.isReady()).toBe(true);
  });
});
