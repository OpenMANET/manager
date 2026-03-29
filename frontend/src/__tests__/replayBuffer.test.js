// =============================================================================
// replayBuffer.test.js — Tests for per-channel audio replay buffer
// =============================================================================

import { describe, it, expect } from 'vitest';
import { appendReplay, getReplayPcm, hasReplayData } from '../services/replayBuffer.js';
import { SAMPLE_RATE } from '../constants.js';

describe('TestReplayBufferEmpty', () => {
  it('hasReplayData returns false for unused channel', () => {
    // Channel 5 should be unused at test start (unless other tests wrote to it).
    // We test the general contract: no data means no replay.
    expect(hasReplayData(99)).toBe(false);
  });

  it('getReplayPcm returns null for unused channel', () => {
    expect(getReplayPcm(99)).toBeNull();
  });
});

describe('TestReplayBufferAppend', () => {
  it('stores data that can be retrieved', () => {
    const pcm = new Float32Array(960);
    for (let i = 0; i < pcm.length; i++) pcm[i] = (i + 1) * 0.001;
    appendReplay(4, pcm);

    expect(hasReplayData(4)).toBe(true);
    const result = getReplayPcm(4);
    expect(result).toBeInstanceOf(Float32Array);
    expect(result.length).toBeGreaterThanOrEqual(960);

    // Last 960 samples should match what we wrote.
    const tail = result.slice(result.length - 960);
    for (let i = 0; i < 960; i++) {
      expect(tail[i]).toBeCloseTo(pcm[i], 5);
    }
  });

  it('appends multiple frames', () => {
    const frame1 = new Float32Array(960);
    const frame2 = new Float32Array(960);
    for (let i = 0; i < 960; i++) {
      frame1[i] = 0.1;
      frame2[i] = 0.2;
    }
    appendReplay(3, frame1);
    appendReplay(3, frame2);

    const result = getReplayPcm(3);
    expect(result.length).toBeGreaterThanOrEqual(1920);
  });
});

describe('TestReplayBufferCircular', () => {
  it('caps at 30 seconds of audio', () => {
    const maxSamples = SAMPLE_RATE * 30;
    // Write 31 seconds worth of data.
    const chunkSize = 4800; // 100ms chunks
    const totalChunks = Math.ceil((SAMPLE_RATE * 31) / chunkSize);
    for (let c = 0; c < totalChunks; c++) {
      const chunk = new Float32Array(chunkSize);
      chunk.fill(c * 0.0001);
      appendReplay(2, chunk);
    }

    const result = getReplayPcm(2);
    expect(result.length).toBeLessThanOrEqual(maxSamples);
  });
});

describe('TestReplayBufferInvalidChannel', () => {
  it('appendReplay does not throw for unknown channel', () => {
    expect(() => appendReplay(99, new Float32Array(100))).not.toThrow();
  });
});
