// =============================================================================
// ringBuffer.js — PCM ring buffer for decoded audio playback
// =============================================================================
//
// WHY A RING BUFFER?
// Opus decodes in 960-sample frames (20 ms at 48 kHz), but WebAudio's
// AudioWorklet and ScriptProcessorNode request 128 or 1024 samples at a time.
// A ring buffer decouples the producer (Opus decoder) from the consumer
// (audio output node), absorbing timing jitter from the network and the
// browser's audio callback scheduling.
//
// THREAD SAFETY
// When AudioWorklet is available, the ring buffer lives in a
// SharedArrayBuffer so both the main thread (writer) and the audio thread
// (reader) can access it without message-passing overhead.  All index reads
// and writes go through Atomics.load / Atomics.store to guarantee visibility
// across threads.  This works even on the main-thread-only
// ScriptProcessorNode fallback because Atomics on regular ArrayBuffers are
// no-ops but syntactically identical.
//
// JITTER PRE-FILL
// Playback doesn't start until JITTER_PREFILL samples have accumulated.
// This prevents the very first frames from playing into a near-empty buffer
// and causing choppy audio.  Once the buffer drains to zero the prefill flag
// resets, so the next burst of audio also gets the jitter cushion.

import { PCM_RING_SIZE, JITTER_PREFILL } from '../constants.js';

// -----------------------------------------------------------------------------
// createRingBuffer(shared)
// -----------------------------------------------------------------------------
// Allocates the ring buffer and its state array.
//
// Parameters:
//   shared — if true, uses SharedArrayBuffer for cross-thread access with
//            AudioWorklet.  If false, uses plain ArrayBuffer (ScriptProcessor
//            fallback).
//
// Returns: { ringBuf, ring, state }
//   ringBuf — the underlying ArrayBuffer / SharedArrayBuffer (needed to post
//             to the AudioWorklet)
//   ring    — Float32Array view over ringBuf for sample data
//   state   — Int32Array with 3 elements: [writeIndex, readIndex, prefilled]
//             "prefilled" is 0 or 1 — flips to 1 once jitter threshold is met
export function createRingBuffer(shared) {
  let ringBuf, ring, state;

  if (shared) {
    // SharedArrayBuffer allows the AudioWorklet thread to read directly.
    ringBuf = new SharedArrayBuffer(PCM_RING_SIZE * 4); // 4 bytes per Float32
    ring = new Float32Array(ringBuf);
    const stateBuf = new SharedArrayBuffer(3 * 4); // 3 Int32 slots
    state = new Int32Array(stateBuf);
  } else {
    // Plain buffers — only accessed from the main thread.
    ring = new Float32Array(PCM_RING_SIZE);
    ringBuf = ring.buffer;
    state = new Int32Array(3); // [writeIndex, readIndex, prefilled]
  }

  // Zero out indices and prefill flag.
  Atomics.store(state, 0, 0); // writeIndex
  Atomics.store(state, 1, 0); // readIndex
  Atomics.store(state, 2, 0); // prefilled

  return { ringBuf, ring, state };
}

// -----------------------------------------------------------------------------
// ringAvail(state, ringSize)
// -----------------------------------------------------------------------------
// Returns the number of samples currently available to read.
// Uses modular arithmetic: (write - read + size) % size.
export function ringAvail(state, ringSize) {
  const wr = Atomics.load(state, 0);
  const rd = Atomics.load(state, 1);
  return (wr - rd + ringSize) % ringSize;
}

// -----------------------------------------------------------------------------
// ringFree(state, ringSize)
// -----------------------------------------------------------------------------
// Returns the number of samples that can be written before the buffer is full.
// We reserve one slot so that write == read always means "empty", not "full".
export function ringFree(state, ringSize) {
  return ringSize - 1 - ringAvail(state, ringSize);
}

// -----------------------------------------------------------------------------
// ringWrite(ring, state, ringSize, src)
// -----------------------------------------------------------------------------
// Writes the Float32Array `src` into the ring buffer.
//
// If there isn't enough room, the entire frame is DROPPED rather than
// partially written.  Dropping is the right strategy for real-time audio:
// a partial frame would produce a glitch, while a dropped frame just means
// slightly more jitter buffer depletion.
//
// After writing, checks whether the jitter pre-fill threshold has been
// reached and sets the prefilled flag so the reader can start playback.
export function ringWrite(ring, state, ringSize, src) {
  const n = src.length;

  // Drop the frame if the ring is full — better to lose a frame than
  // corrupt the buffer with a partial write.
  if (n > ringFree(state, ringSize)) {
    return;
  }

  let wr = Atomics.load(state, 0);
  for (let i = 0; i < n; i++) {
    ring[(wr + i) % ringSize] = src[i];
  }
  Atomics.store(state, 0, (wr + n) % ringSize);

  // Mark prefilled once jitter buffer threshold is reached.
  // This gate prevents the audio output from playing silence-then-audio
  // during the initial buffering period.
  if (!Atomics.load(state, 2) && ringAvail(state, ringSize) >= JITTER_PREFILL) {
    Atomics.store(state, 2, 1);
  }
}

// -----------------------------------------------------------------------------
// ringRead(ring, state, ringSize, dst, count)
// -----------------------------------------------------------------------------
// Reads `count` samples into the Float32Array `dst`.
//
// If the prefill threshold hasn't been met yet, outputs silence (zeros).
// If fewer than `count` samples are available, reads what's there and
// zero-fills the rest (underrun handling — produces a brief silence gap
// rather than a glitch).
//
// When the buffer drains completely after having had data, resets the
// prefilled flag so the next burst of audio gets the jitter cushion again.
export function ringRead(ring, state, ringSize, dst, count) {
  // Don't start playback until jitter pre-fill threshold is met.
  if (!Atomics.load(state, 2)) {
    for (let i = 0; i < count; i++) {
      dst[i] = 0;
    }
    return;
  }

  const avail = ringAvail(state, ringSize);
  let rd = Atomics.load(state, 1);
  const n = Math.min(count, avail);

  // Copy available samples from ring to destination.
  for (let i = 0; i < n; i++) {
    dst[i] = ring[(rd + i) % ringSize];
  }
  Atomics.store(state, 1, (rd + n) % ringSize);

  // Zero-fill any remaining slots if we underran.
  for (let i = n; i < count; i++) {
    dst[i] = 0;
  }

  // If we just drained the buffer completely, reset the prefilled flag.
  // This ensures the next burst of incoming audio re-fills the jitter
  // cushion before playback resumes, avoiding choppy starts.
  if (avail > 0 && ringAvail(state, ringSize) === 0) {
    Atomics.store(state, 2, 0);
  }
}
