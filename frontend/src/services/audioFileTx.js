// =============================================================================
// audioFileTx.js — Audio file TX playback service
// =============================================================================
//
// Manages loading an audio file and streaming it for transmission using the
// Web Audio API's hardware clock for perfect timing.
//
// HOW IT WORKS
// Instead of manually chunking PCM with setTimeout (which drifts 10-50ms),
// we play the file through an AudioBufferSourceNode and tap the output with
// a ScriptProcessorNode.  The ScriptProcessorNode's onaudioprocess callback
// fires at exact hardware-clock intervals (every 1024 samples at 48kHz =
// ~21.3ms), giving us jitter-free frame delivery to the Opus encoder.
//
// The audio never reaches the speakers — the ScriptProcessor is connected
// to a silent gain node.  It only captures samples for encoding.

import { SAMPLE_RATE, FRAME_SIZE } from '../constants.js';

// -----------------------------------------------------------------------------
// Module state
// -----------------------------------------------------------------------------

let filePlaying = false;
let sourceNode = null;       // AudioBufferSourceNode — the file playback
let captureNode = null;      // ScriptProcessorNode — taps audio for encoding
let silentGain = null;       // Gain node at 0 — prevents file audio from speakers
let txTimestamp = 0;

// -----------------------------------------------------------------------------
// loadFile(file, audioCtx)
// -----------------------------------------------------------------------------
// Decodes an audio file using the Web Audio API's decodeAudioData.
// The AudioContext's sample rate (48kHz) handles resampling automatically.
//
// Returns: { audioBuffer, duration, sampleRate, name }
export async function loadFile(file, audioCtx) {
  const arrayBuf = await file.arrayBuffer();
  const audioBuffer = await audioCtx.decodeAudioData(arrayBuf);

  return {
    audioBuffer,
    duration: audioBuffer.duration,
    sampleRate: audioBuffer.sampleRate,
    name: file.name,
  };
}

// -----------------------------------------------------------------------------
// startPlayback(audioBuffer, encoder, loop, onLog, audioCtx)
// -----------------------------------------------------------------------------
// Streams the audio file through the Web Audio graph for encoding.
//
// The AudioBufferSourceNode plays the file at the exact sample rate.
// A ScriptProcessorNode captures the output and feeds it to the Opus
// encoder frame by frame.  This gives hardware-clock precision — no
// setTimeout drift, no choppy audio.
//
// Parameters:
//   audioBuffer — decoded AudioBuffer (from loadFile)
//   encoder     — WebCodecs AudioEncoder instance
//   loop        — boolean, whether to loop playback
//   onLog       — function(msg, cls) for log messages
//   audioCtx    — the AudioContext (needed to create nodes)
//
// Returns: a stop function.
export function startPlayback(audioBuffer, encoder, loop, onLog, audioCtx) {
  if (!audioBuffer || !encoder || encoder.state === 'closed' || !audioCtx) {
    return () => {};
  }

  filePlaying = true;
  txTimestamp = 0;
  const log = onLog || (() => {});
  log('File TX start', 'tx');

  // Create the playback source.
  sourceNode = audioCtx.createBufferSource();
  sourceNode.buffer = audioBuffer;
  sourceNode.loop = loop;

  // ScriptProcessorNode captures the audio output for encoding.
  // Buffer size must be a power of 2 (browser requirement).  We use 1024
  // (~21.3ms at 48kHz) which is close to the 960-sample Opus frame size.
  // The Opus encoder handles the frame size mismatch internally.
  captureNode = audioCtx.createScriptProcessor(1024, 1, 1);

  captureNode.onaudioprocess = (e) => {
    if (!filePlaying || !encoder || encoder.state === 'closed') return;

    const input = e.inputBuffer.getChannelData(0);

    try {
      const ad = new AudioData({
        format: 'f32-planar',
        sampleRate: SAMPLE_RATE,
        numberOfFrames: input.length,
        numberOfChannels: 1,
        timestamp: txTimestamp,
        data: input,
      });
      txTimestamp += (input.length / SAMPLE_RATE) * 1000000; // microseconds
      encoder.encode(ad);
      ad.close();
    } catch (e) {
      // Encoder may have been closed during stop — ignore.
    }
  };

  // Connect: source → capture → silent gain → destination
  // The silent gain prevents the file from playing through speakers.
  // The capture node must be connected to the destination (even silently)
  // or the browser may garbage-collect it and stop firing events.
  silentGain = audioCtx.createGain();
  silentGain.gain.value = 0;

  sourceNode.connect(captureNode);
  captureNode.connect(silentGain);
  silentGain.connect(audioCtx.destination);

  // Handle natural end of playback (non-looping).
  sourceNode.onended = () => {
    if (!loop && filePlaying) {
      stopPlayback();
      log('File TX complete', 'tx');
    }
  };

  // Start playback.
  sourceNode.start(0);

  return stopPlayback;
}

// -----------------------------------------------------------------------------
// stopPlayback()
// -----------------------------------------------------------------------------
// Stops file playback and disconnects all audio nodes.
export function stopPlayback() {
  filePlaying = false;

  if (sourceNode) {
    try { sourceNode.stop(); } catch (e) { /* already stopped */ }
    sourceNode.disconnect();
    sourceNode = null;
  }
  if (captureNode) {
    captureNode.disconnect();
    captureNode = null;
  }
  if (silentGain) {
    silentGain.disconnect();
    silentGain = null;
  }
}

// -----------------------------------------------------------------------------
// isPlaying()
// -----------------------------------------------------------------------------
export function isPlaying() {
  return filePlaying;
}
