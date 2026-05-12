// =============================================================================
// replayBuffer.js — Per-channel circular buffer for audio replay
// =============================================================================
// Stores the last 30 seconds of decoded PCM per channel so operators can
// replay the most recent transmission they missed.

import { SAMPLE_RATE, CHANNELS_DEF } from '../constants.js';

const REPLAY_SECONDS = 30;
const REPLAY_SIZE = SAMPLE_RATE * REPLAY_SECONDS;

const buffers = {};
CHANNELS_DEF.forEach((c) => {
  buffers[c.ch] = { data: new Float32Array(REPLAY_SIZE), writePos: 0, totalWritten: 0 };
});

export function appendReplay(ch, pcm) {
  const buf = buffers[ch];
  if (!buf) return;
  for (let i = 0; i < pcm.length; i++) {
    buf.data[buf.writePos % REPLAY_SIZE] = pcm[i];
    buf.writePos++;
  }
  buf.totalWritten += pcm.length;
}

export function getReplayPcm(ch) {
  const buf = buffers[ch];
  if (!buf || buf.totalWritten === 0) return null;
  const len = Math.min(buf.totalWritten, REPLAY_SIZE);
  const out = new Float32Array(len);
  const start = buf.writePos - len;
  for (let i = 0; i < len; i++) {
    out[i] = buf.data[(start + i + REPLAY_SIZE) % REPLAY_SIZE];
  }
  return out;
}

export function hasReplayData(ch) {
  const buf = buffers[ch];
  return !!(buf && buf.totalWritten > 0);
}
