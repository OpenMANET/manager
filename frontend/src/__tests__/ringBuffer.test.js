// =============================================================================
// ringBuffer.test.js — Tests for the PCM ring buffer
// =============================================================================

import { describe, it, expect, beforeEach } from 'vitest';
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
    const { ringBuf, ring, state } = createRingBuffer(false);
    expect(ring).toBeInstanceOf(Float32Array);
    expect(ring.length).toBe(PCM_RING_SIZE);
    expect(state).toBeInstanceOf(Int32Array);
    expect(state.length).toBe(3);
    // All indices start at zero.
    expect(Atomics.load(state, 0)).toBe(0); // writeIndex
    expect(Atomics.load(state, 1)).toBe(0); // readIndex
    expect(Atomics.load(state, 2)).toBe(0); // prefilled
  });

  it('creates SharedArrayBuffer when available', () => {
    // jsdom supports SharedArrayBuffer
    const { ringBuf, ring, state } = createRingBuffer(true);
    expect(ringBuf).toBeInstanceOf(SharedArrayBuffer);
    expect(ring).toBeInstanceOf(Float32Array);
    expect(ring.length).toBe(PCM_RING_SIZE);
    expect(state).toBeInstanceOf(Int32Array);
    expect(state.length).toBe(3);
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

  it('drops frame when ring is full', () => {
    const { ring, state } = createRingBuffer(false);
    // Fill the ring buffer to capacity (ringSize - 1)
    const fillSize = PCM_RING_SIZE - 1;
    const src = new Float32Array(fillSize);
    ringWrite(ring, state, PCM_RING_SIZE, src);
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(fillSize);

    // Try to write one more sample — should be dropped
    const extra = new Float32Array(1);
    extra[0] = 99.0;
    ringWrite(ring, state, PCM_RING_SIZE, extra);
    // Avail should not change
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(fillSize);
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

  it('zero-fills on underrun', () => {
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

  it('resets prefill when buffer drains completely', () => {
    const { ring, state } = createRingBuffer(false);

    // Fill to prefill threshold
    const src = new Float32Array(JITTER_PREFILL);
    for (let i = 0; i < src.length; i++) src[i] = 0.5;
    ringWrite(ring, state, PCM_RING_SIZE, src);
    expect(Atomics.load(state, 2)).toBe(1); // prefilled

    // Drain completely
    const dst = new Float32Array(JITTER_PREFILL);
    ringRead(ring, state, PCM_RING_SIZE, dst, JITTER_PREFILL);

    // Buffer should be empty and prefill should be reset
    expect(ringAvail(state, PCM_RING_SIZE)).toBe(0);
    expect(Atomics.load(state, 2)).toBe(0); // reset
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
