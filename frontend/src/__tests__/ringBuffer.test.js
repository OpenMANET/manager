// =============================================================================
// ringBuffer.test.js — Tests for the PCM ring buffer
// =============================================================================

import { describe, it, expect } from 'vitest';
import {
  createRingBuffer,
  ringAvail,
  ringFree,
  ringWrite,
  ringRead,
} from '../services/ringBuffer.js';
import { PCM_RING_SIZE, JITTER_PREFILL } from '../constants.js';

describe('TestCreateRingBuffer', () => {
  it('creates non-shared buffer with correct sizes', () => {
    const { ring, state } = createRingBuffer(false);
    expect(ring).toBeInstanceOf(Float32Array);
    expect(ring.length).toBe(PCM_RING_SIZE);
    expect(state).toBeInstanceOf(Int32Array);
    expect(state.length).toBe(5);
    // All indices start at zero.
    expect(Atomics.load(state, 0)).toBe(0); // writeIndex
    expect(Atomics.load(state, 1)).toBe(0); // readIndex
    expect(Atomics.load(state, 2)).toBe(0); // prefilled
    expect(Atomics.load(state, 3)).toBe(0); // droppedFrames
    expect(Atomics.load(state, 4)).toBe(0); // underrunSamples
  });

  it('creates SharedArrayBuffer when available', () => {
    // jsdom supports SharedArrayBuffer
    const { ringBuf, ring, state } = createRingBuffer(true);
    expect(ringBuf).toBeInstanceOf(SharedArrayBuffer);
    expect(ring).toBeInstanceOf(Float32Array);
    expect(ring.length).toBe(PCM_RING_SIZE);
    expect(state).toBeInstanceOf(Int32Array);
    expect(state.length).toBe(5);
  });
});

describe('TestRingAvail', () => {
  it('returns 0 on empty buffer', () => {
    const { state } = createRingBuffer(false);
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(0);
  });

  it('returns correct count after write', () => {
    const { ring, state } = createRingBuffer(false);
    // Force prefill so we can test properly
    const src = new Float32Array(100);
    ringWrite(ring, state, PCM_RING_SIZE, src);
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(100);
  });
});

describe('TestRingFree', () => {
  it('returns ringSize - 1 on empty buffer', () => {
    const { state } = createRingBuffer(false);
    expect(ringFree(state, PCM_RING_SIZE)).toBe(PCM_RING_SIZE - 1);
  });
});

describe('TestRingWrite', () => {
  it('writes data that can be read back', () => {
    const { ring, state } = createRingBuffer(false);
    // Write enough to trigger prefill
    const src = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < src.length; i++) src[i] = (i + 1) * 0.001;
    ringWrite(ring, state, PCM_RING_SIZE, src);

    expect(ringAvail(state, PCM_RING_SIZE)).toBe(JITTER_PREFILL);
    // Prefilled flag should be set
    expect(Atomics.load(state, 2)).toBe(1);
  });

  it('drops frame when ring is full and increments drop counter', () => {
    const { ring, state } = createRingBuffer(false);
    // Fill the ring buffer to capacity (ringSize - 1)
    const fillSize = PCM_RING_SIZE - 1;
    const src = new Float32Array(fillSize);
    ringWrite(ring, state, PCM_RING_SIZE, src);
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(fillSize);
    expect(Atomics.load(state, 3)).toBe(0); // no drops yet

    // Try to write one more sample — should be dropped, counter ticks.
    const extra = new Float32Array(1);
    extra[0] = 99.0;
    ringWrite(ring, state, PCM_RING_SIZE, extra);
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(fillSize);
    expect(Atomics.load(state, 3)).toBe(1);

    // A second drop bumps the counter again.
    ringWrite(ring, state, PCM_RING_SIZE, extra);
    expect(Atomics.load(state, 3)).toBe(2);
  });
});

describe('TestRingRead', () => {
  it('returns written data correctly', () => {
    const { ring, state } = createRingBuffer(false);
    // Write JITTER_PREFILL samples to trigger prefilled flag
    const src = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < src.length; i++) src[i] = (i + 1) * 0.001;
    ringWrite(ring, state, PCM_RING_SIZE, src);

    const dst = new Float32Array(JITTER_PREFILL);
    ringRead(ring, state, PCM_RING_SIZE, dst, JITTER_PREFILL);

    for (let i = 0; i < JITTER_PREFILL; i++) {
      expect(dst[i]).toBeCloseTo(src[i], 5);
    }
  });

  it('zero-fills on underrun and increments underrun counter', () => {
    const { ring, state } = createRingBuffer(false);
    // Write JITTER_PREFILL to enable playback
    const src = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < src.length; i++) src[i] = 0.5;
    ringWrite(ring, state, PCM_RING_SIZE, src);

    // Read more than available
    const readCount = JITTER_PREFILL + 100;
    const dst = new Float32Array(readCount);
    ringRead(ring, state, PCM_RING_SIZE, dst, readCount);

    // First JITTER_PREFILL samples should be 0.5
    for (let i = 0; i < JITTER_PREFILL; i++) {
      expect(dst[i]).toBeCloseTo(0.5, 5);
    }
    // Remaining should be zero-filled
    for (let i = JITTER_PREFILL; i < readCount; i++) {
      expect(dst[i]).toBe(0);
    }
    // Underrun counter bumped by the gap size.
    expect(Atomics.load(state, 4)).toBe(100);
  });

  it('does not bump underrun counter when the read is fully satisfied', () => {
    const { ring, state } = createRingBuffer(false);
    const src = new Float32Array(JITTER_PREFILL);
    ringWrite(ring, state, PCM_RING_SIZE, src);

    const dst = new Float32Array(JITTER_PREFILL);
    ringRead(ring, state, PCM_RING_SIZE, dst, JITTER_PREFILL);

    expect(Atomics.load(state, 4)).toBe(0);
  });
});

describe('TestJitterPrefill', () => {
  it('does not start playback until JITTER_PREFILL samples accumulated', () => {
    const { ring, state } = createRingBuffer(false);

    // Write fewer than JITTER_PREFILL
    const partial = new Float32Array(JITTER_PREFILL - 1);
    for (let i = 0; i < partial.length; i++) partial[i] = 1.0;
    ringWrite(ring, state, PCM_RING_SIZE, partial);

    // Prefilled flag should still be 0
    expect(Atomics.load(state, 2)).toBe(0);

    // Read should output silence
    const dst = new Float32Array(100);
    ringRead(ring, state, PCM_RING_SIZE, dst, 100);
    for (let i = 0; i < 100; i++) {
      expect(dst[i]).toBe(0);
    }
  });

  it('keeps prefill set after buffer drains completely', () => {
    // Regression guard for the RX stutter fix: the ring used to reset
    // prefill on every drain-to-zero, which turned every transient
    // underrun into a mandatory 60 ms silence stall. The new behavior is
    // one-shot prefill — once set, it stays set, and underruns fall
    // through to the zero-fill path in ringRead.
    const { ring, state } = createRingBuffer(false);

    const src = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < src.length; i++) src[i] = 0.5;
    ringWrite(ring, state, PCM_RING_SIZE, src);
    expect(Atomics.load(state, 2)).toBe(1); // prefilled

    const dst = new Float32Array(JITTER_PREFILL);
    ringRead(ring, state, PCM_RING_SIZE, dst, JITTER_PREFILL);

    expect(ringAvail(state, PCM_RING_SIZE)).toBe(0);
    expect(Atomics.load(state, 2)).toBe(1); // still prefilled
  });

  it('plays new samples immediately after a full drain (no re-prefill stall)', () => {
    const { ring, state } = createRingBuffer(false);

    // Prefill, drain, then write a small burst of known samples.
    const warmup = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < warmup.length; i++) warmup[i] = 0.25;
    ringWrite(ring, state, PCM_RING_SIZE, warmup);
    const drain = new Float32Array(JITTER_PREFILL);
    ringRead(ring, state, PCM_RING_SIZE, drain, JITTER_PREFILL);

    // New burst must play right away — NOT be blocked behind a re-prefill.
    const burst = new Float32Array(128);
    for (let i = 0; i < burst.length; i++) burst[i] = 0.75;
    ringWrite(ring, state, PCM_RING_SIZE, burst);

    const out = new Float32Array(128);
    ringRead(ring, state, PCM_RING_SIZE, out, 128);
    for (let i = 0; i < 128; i++) {
      expect(out[i]).toBeCloseTo(0.75, 5);
    }
  });
});

describe('TestWrapAround', () => {
  it('handles write past end of buffer and reads back correctly', () => {
    const { ring, state } = createRingBuffer(false);

    // Advance writeIndex and readIndex near the end of the buffer
    // by writing and reading a large chunk.
    const advanceSize = PCM_RING_SIZE - 500;
    const advanceSrc = new Float32Array(advanceSize);
    ringWrite(ring, state, PCM_RING_SIZE, advanceSrc);
    // Force prefilled flag so we can read
    Atomics.store(state, 2, 1);
    const advanceDst = new Float32Array(advanceSize);
    ringRead(ring, state, PCM_RING_SIZE, advanceDst, advanceSize);

    // Now write and readIndex are near the end. Write 1000 samples
    // which will wrap around.
    // Need to re-trigger prefill — write JITTER_PREFILL samples
    const wrapSrc = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < wrapSrc.length; i++) wrapSrc[i] = (i + 1) * 0.0001;
    ringWrite(ring, state, PCM_RING_SIZE, wrapSrc);

    expect(ringAvail(state, PCM_RING_SIZE)).toBe(JITTER_PREFILL);

    const wrapDst = new Float32Array(JITTER_PREFILL);
    ringRead(ring, state, PCM_RING_SIZE, wrapDst, JITTER_PREFILL);

    for (let i = 0; i < JITTER_PREFILL; i++) {
      expect(wrapDst[i]).toBeCloseTo(wrapSrc[i], 5);
    }
  });
});
