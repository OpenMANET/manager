// =============================================================================
// whisperService.js — Whisper WASM integration for speech-to-text captions
// =============================================================================
//
// This module manages offline speech-to-text using whisper.cpp compiled to
// WebAssembly.  It runs entirely in the browser — no server-side transcription
// is needed.
//
// ARCHITECTURE
//   1. The whisper WASM module is loaded via a <script> tag (whisper-main.js)
//      which populates a global `Module` object with FS and inference methods.
//   2. The tiny.en model (~75 MB) is downloaded from the backend on first use
//      and cached in IndexedDB for subsequent page loads.
//   3. Each channel accumulates its own 16 kHz audio buffer independently.
//      When silence is detected on a channel (no new audio for
//      WHISPER_SILENCE_MS), the accumulated audio is queued for transcription.
//   4. Transcription runs one job at a time (whisper.cpp limitation in WASM).
//      Jobs are queued and processed in order.
//
// DOWNSAMPLING
//   Audio arrives at 48 kHz from the Opus decoder.  Whisper expects 16 kHz.
//   We downsample by taking every 3rd sample (48000/16000 = 3).  This is a
//   simple decimation without an anti-aliasing filter, which is acceptable
//   for speech recognition (the model is robust to aliasing artifacts).

import {
  WHISPER_RATE,
  SAMPLE_RATE,
  WHISPER_SILENCE_MS,
  WHISPER_MIN_SEC,
  WHISPER_MAX_SEC,
  CHANNELS_DEF,
} from '../constants.js';

// -----------------------------------------------------------------------------
// Module state
// -----------------------------------------------------------------------------

let whisperReady = false;
let whisperInstance = null; // Handle returned by Module.init()
let whisperBusy = false;   // Only one transcription at a time

// Per-channel state: each channel independently accumulates audio and tracks
// the speaker IP so transcriptions can be attributed correctly.
const chState = {};
CHANNELS_DEF.forEach((c) => {
  chState[c.ch] = {
    audioBuf: [],       // Array of Float32Array chunks at 16 kHz
    srcIP: '',          // IP of the last speaker on this channel
    lastRx: 0,          // Timestamp of last received audio (Date.now())
    pendingTranscribe: false, // true if already queued for transcription
  };
});

// Transcription job queue — processed one at a time.
const transcribeQueue = [];

// Callbacks set by the consumer.
let _onLog = null;
let _debugFn = null;

// IndexedDB database name and version for model caching.
const DB_NAME = 'comms-whisper';
const DB_VER = 1;

// -----------------------------------------------------------------------------
// Server-side whisper model management
// -----------------------------------------------------------------------------

// checkWhisperAvailable — queries the backend for whisper model availability.
// Returns { available, state, progress, error } or { available: false } on failure.
export async function checkWhisperAvailable() {
  try {
    const resp = await fetch('/api/whisper/status');
    if (!resp.ok) return { available: false, state: 'idle', progress: 0, error: '' };
    return await resp.json();
  } catch {
    return { available: false, state: 'idle', progress: 0, error: '' };
  }
}

// downloadWhisperModel — triggers a server-side download of the whisper model.
// Polls progress and calls onProgress(pct) until complete.
// Returns true on success, false on failure.
export async function downloadWhisperModel(onProgress, onError) {
  try {
    const resp = await fetch('/api/whisper/download', { method: 'POST' });
    if (!resp.ok) {
      const data = await resp.json().catch(() => ({}));
      if (onError) onError(data.error || `HTTP ${resp.status}`);
      return false;
    }

    // Poll download progress.
    return new Promise((resolve) => {
      const poll = setInterval(async () => {
        try {
          const statusResp = await fetch('/api/whisper/download/status');
          if (!statusResp.ok) return;
          const status = await statusResp.json();

          if (onProgress) onProgress(status.progress || 0);

          if (status.state === 'ready') {
            clearInterval(poll);
            resolve(true);
          } else if (status.state === 'error') {
            clearInterval(poll);
            if (onError) onError(status.error || 'Download failed');
            resolve(false);
          }
          // 'downloading' — keep polling.
        } catch {
          // Network error during poll — keep trying.
        }
      }, 1000);
    });
  } catch (e) {
    if (onError) onError(e.message);
    return false;
  }
}

// removeWhisperModel — asks the backend to delete downloaded whisper files.
// Returns true on success, false on failure.
export async function removeWhisperModel() {
  try {
    const resp = await fetch('/api/whisper/remove', { method: 'DELETE' });
    return resp.ok;
  } catch {
    return false;
  }
}

// -----------------------------------------------------------------------------
// initWhisper(onStatus, onLog, debugFn)
// -----------------------------------------------------------------------------
// Loads the Whisper model and initializes the WASM inference engine.
//
// Parameters:
//   onStatus — function(msg) called with status text for the UI
//   onLog    — function(msg, cls) for log messages
//   debugFn  — function(msg) for debug-level messages (only called when
//              debug mode is active in the caller)
//
// Returns true if initialization succeeded, false otherwise.
// On failure, the caller should disable the CC toggle.
export async function initWhisper(onStatus, onLog, debugFn) {
  _onLog = onLog || (() => {});
  _debugFn = debugFn || (() => {});

  onStatus('Loading Whisper model (75 MB)...');
  _onLog('Loading Whisper model...', 'info');

  try {
    _debugFn(`Module exists: ${typeof Module !== 'undefined'}, FS_createDataFile: ${!!(typeof Module !== 'undefined' && Module.FS_createDataFile)}`);
    _debugFn(`SharedArrayBuffer: ${typeof SharedArrayBuffer !== 'undefined'}`);

    // The whisper WASM module must be loaded via <script> tag before this
    // function is called.  It exposes Module.FS_createDataFile,
    // Module.init, Module.full_default, etc.
    if (typeof Module === 'undefined') {
      onStatus('Whisper WASM not loaded');
      _onLog('Whisper WASM script not loaded — check that whisper-main.js is deployed', 'err');
      return false;
    }

    // Wait for the Emscripten runtime to finish initializing.  The WASM
    // binary downloads and compiles asynchronously even after the <script>
    // tag has executed.  Module._runtimeReady is a Promise resolved by
    // onRuntimeInitialized (set up in index.html).
    //
    // If whisper-main.js didn't load (404) or the WASM fails to compile,
    // onRuntimeInitialized never fires.  We use a 10-second timeout to
    // avoid hanging the UI forever in that case.
    if (Module._runtimeReady) {
      _debugFn('Waiting for WASM runtime to initialize...');
      onStatus('Initializing WASM runtime...');
      const timeout = new Promise((_, reject) =>
        setTimeout(() => reject(new Error('WASM runtime init timed out (10s) — whisper files may not be deployed')), 10000)
      );
      try {
        await Promise.race([Module._runtimeReady, timeout]);
        _debugFn('WASM runtime ready');
      } catch (e) {
        onStatus('Failed: ' + e.message);
        _onLog('Whisper WASM init timeout — ensure whisper-main.js and .wasm are deployed at /whisper/', 'err');
        return false;
      }
    }

    if (!Module.FS_createDataFile) {
      onStatus('Whisper WASM failed to initialize');
      _onLog('Whisper WASM failed — FS_createDataFile not available (SharedArrayBuffer required)', 'err');
      _debugFn(`Module keys: ${Object.keys(Module).join(',')}`);
      return false;
    }

    // Try to load the model from IndexedDB cache first.
    let modelData = await loadFromIDB();

    if (!modelData) {
      // Check if the model is available on the server (downloaded to /tmp
      // or embedded in the binary).
      _debugFn('Checking server for whisper model availability...');
      const serverStatus = await checkWhisperAvailable();
      if (!serverStatus.available) {
        onStatus('Whisper model not downloaded \u2014 go to Settings to download');
        _onLog('Whisper model not available. Download it from the Settings page.', 'err');
        return false;
      }

      // Download from the backend server.
      onStatus('Downloading model from server...');
      _debugFn('Fetching model from /whisper/ggml-tiny.en.bin...');

      const resp = await fetch('/whisper/ggml-tiny.en.bin');
      if (!resp.ok) throw new Error('Failed to fetch model: HTTP ' + resp.status);

      modelData = new Uint8Array(await resp.arrayBuffer());
      _debugFn(`Model downloaded: ${modelData.length} bytes (${(modelData.length / 1024 / 1024).toFixed(1)} MB)`);

      // Cache in IndexedDB for next time.
      await saveToIDB(modelData);
      _onLog('Model cached in browser', 'info');
    } else {
      _onLog('Model loaded from cache', 'info');
      _debugFn(`Model from IDB: ${modelData.length} bytes`);
    }

    // Write the model data to the WASM virtual filesystem.
    // Remove any existing file first to avoid errors on reload.
    try { Module.FS_unlink('whisper.bin'); } catch { /* ignore */ }

    _debugFn('Writing model to WASM filesystem...');
    Module.FS_createDataFile('/', 'whisper.bin', modelData, true, true);

    // Initialize the whisper inference context.
    _debugFn('Calling Module.init("whisper.bin")...');
    whisperInstance = Module.init('whisper.bin');
    _debugFn(`Module.init returned: ${whisperInstance} (type: ${typeof whisperInstance})`);

    if (whisperInstance) {
      whisperReady = true;
      onStatus('Whisper ready — listening on all channels');
      _onLog('Whisper initialized', 'info');
      _debugFn(`Whisper ready. Module.full_default exists: ${typeof Module.full_default}`);
      return true;
    } else {
      throw new Error('Module.init returned null/0');
    }
  } catch (e) {
    onStatus('Failed: ' + e.message);
    _onLog('Whisper error: ' + e.message, 'err');
    _debugFn(`Load error stack: ${e.stack}`);
    return false;
  }
}

// -----------------------------------------------------------------------------
// feedAudio(ch, pcm48k, srcIP)
// -----------------------------------------------------------------------------
// Downsamples 48 kHz PCM to 16 kHz and accumulates it in the channel's buffer.
//
// Parameters:
//   ch      — channel number (1-5)
//   pcm48k  — Float32Array of PCM samples at 48 kHz
//   srcIP   — dotted-quad IP of the speaker
//
// This is called from the audio decoder output callback for every decoded
// Opus frame.  The downsampling is simple decimation (pick every Nth sample)
// which is adequate for speech recognition.
export function feedAudio(ch, pcm48k, srcIP) {
  const cs = chState[ch];
  if (!cs) return;

  // Downsample 48 kHz → 16 kHz by factor of 3.
  const ratio = SAMPLE_RATE / WHISPER_RATE; // 3
  const dsLen = Math.floor(pcm48k.length / ratio);
  const ds = new Float32Array(dsLen);
  for (let i = 0; i < dsLen; i++) {
    ds[i] = pcm48k[Math.floor(i * ratio)];
  }

  cs.audioBuf.push(ds);
  cs.lastRx = Date.now();
  cs.srcIP = srcIP;
}

// -----------------------------------------------------------------------------
// checkSilenceAndTranscribe(onTranscript)
// -----------------------------------------------------------------------------
// Checks all channels for silence and triggers transcription when appropriate.
// Should be called on a regular interval (e.g., every 500 ms).
//
// Parameters:
//   onTranscript — function(ch, ip, text) called when a transcription completes
//
// The silence detection logic:
//   1. If a channel has accumulated audio AND the last RX was more than
//      WHISPER_SILENCE_MS ago, the utterance is considered "done".
//   2. The audio is collected, validated (min/max duration), and queued.
//   3. The queue is processed one job at a time.
export function checkSilenceAndTranscribe(onTranscript) {
  if (!whisperReady || !whisperInstance) return;

  CHANNELS_DEF.forEach((c) => {
    const cs = chState[c.ch];
    if (cs.audioBuf.length === 0) return;

    const silenceMs = Date.now() - cs.lastRx;
    if (silenceMs < WHISPER_SILENCE_MS) return; // still receiving audio

    _debugFn(`Ch${c.ch}: ${silenceMs}ms silence > ${WHISPER_SILENCE_MS}ms threshold, triggering transcribe`);
    queueTranscribe(c.ch, onTranscript);
  });
}

// -----------------------------------------------------------------------------
// queueTranscribe(ch, onTranscript)
// -----------------------------------------------------------------------------
// Collects audio from a channel's buffer and adds a transcription job to the
// queue.  Discards audio that's too short (< WHISPER_MIN_SEC) and truncates
// audio that's too long (> WHISPER_MAX_SEC).
function queueTranscribe(ch, onTranscript) {
  const cs = chState[ch];
  if (cs.pendingTranscribe) {
    _debugFn(`Ch${ch}: already pending transcribe, skip`);
    return;
  }
  cs.pendingTranscribe = true;

  // Calculate total accumulated audio length.
  let totalLen = 0;
  for (const chunk of cs.audioBuf) totalLen += chunk.length;

  _debugFn(`Ch${ch}: silence detected, audio=${(totalLen / WHISPER_RATE).toFixed(2)}s (${totalLen} samples, min=${WHISPER_MIN_SEC}s)`);

  // Discard if too short — likely just a click or key noise.
  if (totalLen < WHISPER_MIN_SEC * WHISPER_RATE) {
    _debugFn(`Ch${ch}: too short (${(totalLen / WHISPER_RATE).toFixed(2)}s < ${WHISPER_MIN_SEC}s), discarding`);
    cs.audioBuf = [];
    cs.pendingTranscribe = false;
    return;
  }

  // Truncate to max duration to keep transcription latency reasonable.
  const maxSamples = WHISPER_MAX_SEC * WHISPER_RATE;
  if (totalLen > maxSamples) totalLen = maxSamples;

  // Concatenate all chunks into a single Float32Array.
  const audio = new Float32Array(totalLen);
  let offset = 0;
  for (const chunk of cs.audioBuf) {
    const copyLen = Math.min(chunk.length, totalLen - offset);
    audio.set(chunk.subarray(0, copyLen), offset);
    offset += copyLen;
    if (offset >= totalLen) break;
  }

  const ip = cs.srcIP;
  cs.audioBuf = []; // Clear the buffer — audio has been consumed.

  transcribeQueue.push({ ch, ip, audio, onTranscript });
  processQueue();
}

// -----------------------------------------------------------------------------
// processQueue()
// -----------------------------------------------------------------------------
// Processes the next transcription job in the queue.
//
// whisper.cpp in WASM is single-threaded and can only handle one transcription
// at a time.  Jobs are queued and processed sequentially.
//
// The transcription works by:
//   1. Temporarily hooking Module.print to capture whisper.cpp's text output
//   2. Calling Module.full_default() which runs inference synchronously
//   3. Polling for the "whisper_print_timings" line that signals completion
//   4. Extracting transcript text from the timestamp-prefixed output lines
function processQueue() {
  if (whisperBusy) {
    _debugFn(`processQueue: busy, queue=${transcribeQueue.length}`);
    return;
  }
  if (transcribeQueue.length === 0) return;

  whisperBusy = true;
  const job = transcribeQueue.shift();

  _debugFn(`Transcribing Ch${job.ch}: ${(job.audio.length / WHISPER_RATE).toFixed(2)}s audio, ip=${job.ip}, queue remaining=${transcribeQueue.length}`);

  // Capture whisper.cpp output via window._whisperOutputHook.
  //
  // WHY A GLOBAL HOOK?  Emscripten copies Module.print into a local
  // `var out` at initialization time.  After that, all whisper output
  // goes through that captured reference, NOT through Module.print.
  // Reassigning Module.print later has zero effect.  The solution is to
  // have the ORIGINAL Module.print (defined in index.html, before
  // whisper-main.js loads) check window._whisperOutputHook and call it.
  // That way the captured `out` reference still reaches our hook.
  //
  // whisper.cpp prints transcript lines like:
  //   [00:00:00.000 --> 00:00:02.000] Hello world
  // and finishes with a line containing "whisper_print_timings".
  let transcript = '';
  let finished = false;
  let printCount = 0;

  window._whisperOutputHook = function (text) {
    printCount++;
    _debugFn(`[whisper print #${printCount}] ${text}`);

    // Extract text from timestamp-prefixed lines.
    const match = text.match(/\[\d{2}:\d{2}:\d{2}\.\d{3}\s*-->\s*\d{2}:\d{2}:\d{2}\.\d{3}\]\s*(.*)/);
    if (match && match[1].trim()) {
      transcript += match[1].trim() + ' ';
      _debugFn(`[whisper segment] "${match[1].trim()}"`);
    }

    // Detect completion — whisper.cpp prints timing info at the end.
    if (text.indexOf('whisper_print_timings') !== -1) {
      finished = true;
      _debugFn('Whisper finished (timings printed)');
    }
  };

  // Run inference in a setTimeout to yield to the event loop first.
  // This allows the UI to update the "Transcribing..." status before
  // the potentially blocking WASM call.
  setTimeout(() => {
    // Inspect the audio data to verify it's not silence/zeros
    let audioMin = Infinity, audioMax = -Infinity, audioRms = 0;
    for (let i = 0; i < job.audio.length; i++) {
      const v = job.audio[i];
      if (v < audioMin) audioMin = v;
      if (v > audioMax) audioMax = v;
      audioRms += v * v;
    }
    audioRms = Math.sqrt(audioRms / job.audio.length);
    _debugFn(`Audio stats: min=${audioMin.toFixed(4)} max=${audioMax.toFixed(4)} rms=${audioRms.toFixed(4)} len=${job.audio.length}`);

    _debugFn(`Calling Module.full_default(instance=${whisperInstance}, audioLen=${job.audio.length}, lang=en, threads=4, translate=false)`);

    let retValue;
    try {
      retValue = Module.full_default(whisperInstance, job.audio, 'en', 4, false);
      _debugFn(`Module.full_default returned: ${retValue} (prints=${printCount})`);

      // After full_default completes, try to extract segments using the
      // whisper C API via ccall.  The WASM build may not print segments
      // to stdout, but they're available via whisper_full_n_segments /
      // whisper_full_get_segment_text.
      if (retValue === 0 && printCount <= 3) {
        // Check: is Module.print still our hooked version or did Emscripten overwrite it?
        _debugFn(`Module.print is hooked: ${String(Module.print).includes('_whisperOutputHook')}`);
        _debugFn(`window._whisperOutputHook set: ${!!window._whisperOutputHook}`);
      }
    } catch (e) {
      _onLog('Whisper error: ' + e.message, 'err');
      _debugFn(`full_default exception: ${e.message}\n${e.stack}`);
      window._whisperOutputHook = null;
      chState[job.ch].pendingTranscribe = false;
      whisperBusy = false;
      processQueue();
      return;
    }

    // full_default runs synchronously in single-threaded WASM builds.
    // If Module.print was called during execution, we already have the
    // transcript.  If not (some builds don't use Module.print for output),
    // try extracting text via Module.get_text() or similar methods.
    //
    // For builds that DO use async worker threads, we fall back to polling.
    // But we check synchronous results first to avoid the 30s timeout.

    function finalize() {
      window._whisperOutputHook = null;

      const cleaned = transcript.trim();
      _debugFn(`Transcription result: prints=${printCount} result="${cleaned}"`);

      // Deliver the transcript if it's meaningful.
      // Filter out blank audio markers and single-character noise.
      if (cleaned && cleaned !== '[BLANK_AUDIO]' && cleaned.length > 1) {
        _debugFn(`Adding chat: Ch${job.ch} [${job.ip}] "${cleaned}"`);
        if (job.onTranscript) job.onTranscript(job.ch, job.ip, cleaned);
      } else {
        _debugFn(`Discarded: empty or blank audio (cleaned="${cleaned}")`);
      }

      chState[job.ch].pendingTranscribe = false;
      whisperBusy = false;
      processQueue();
    }

    // The whisper WASM build runs inference on a worker thread.  The
    // system_info and "processing..." lines print synchronously, but the
    // actual transcript segments and whisper_print_timings arrive
    // asynchronously from the worker.  We must poll until either:
    //   - The "whisper_print_timings" line appears (finished = true)
    //   - We time out (30 seconds)
    let elapsed = 0;
    const pollMs = 200;
    const maxMs = 30000;

    const poll = setInterval(() => {
      elapsed += pollMs;

      if (finished || elapsed >= maxMs) {
        clearInterval(poll);
        _debugFn(`Poll done: finished=${finished} elapsed=${elapsed}ms prints=${printCount} transcript="${transcript.trim()}"`);
        finalize();
      }
    }, pollMs);
  }, 50);
}

// -----------------------------------------------------------------------------
// isReady()
// -----------------------------------------------------------------------------
// Returns true if the Whisper model is loaded and ready for transcription.
export function isReady() {
  return whisperReady;
}

// -----------------------------------------------------------------------------
// reset()
// -----------------------------------------------------------------------------
// Clears all per-channel audio buffers.  Useful when disabling captions or
// resetting state.
export function reset() {
  CHANNELS_DEF.forEach((c) => {
    const cs = chState[c.ch];
    cs.audioBuf = [];
    cs.srcIP = '';
    cs.lastRx = 0;
    cs.pendingTranscribe = false;
  });
}

// =============================================================================
// IndexedDB helpers for model caching
// =============================================================================
// The whisper model is ~75 MB.  Downloading it every page load would be slow,
// so we cache it in IndexedDB.  This is preferred over the Cache API because
// IndexedDB handles binary blobs more efficiently and doesn't require a
// Service Worker.

function openIDB() {
  return new Promise((resolve, reject) => {
    const rq = indexedDB.open(DB_NAME, DB_VER);
    rq.onupgradeneeded = (e) => {
      e.target.result.createObjectStore('models');
    };
    rq.onsuccess = (e) => resolve(e.target.result);
    rq.onerror = (e) => reject(e);
  });
}

async function loadFromIDB() {
  try {
    const db = await openIDB();
    return new Promise((resolve) => {
      const rq = db
        .transaction('models', 'readonly')
        .objectStore('models')
        .get('ggml-tiny.en');
      rq.onsuccess = () => resolve(rq.result || null);
      rq.onerror = () => resolve(null);
    });
  } catch {
    return null;
  }
}

async function saveToIDB(data) {
  try {
    const db = await openIDB();
    db.transaction('models', 'readwrite')
      .objectStore('models')
      .put(data, 'ggml-tiny.en');
  } catch {
    // Silently ignore — caching is best-effort.
  }
}
