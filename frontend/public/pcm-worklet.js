// =============================================================================
// pcm-worklet.js — AudioWorklet processor for glitch-free PCM playback
// =============================================================================
// Reads decoded PCM from a SharedArrayBuffer ring buffer on the audio thread,
// avoiding main-thread GC/JS jank that causes glitches with ScriptProcessorNode.
// This file runs in the AudioWorklet context, not in React.

class PCMWorkletProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.ring = null;
    this.state = null;
    this.ringSize = 0;
    this.port.onmessage = (e) => {
      if (e.data.type === 'init') {
        this.ring = new Float32Array(e.data.ringBuf);
        this.state = new Int32Array(e.data.stateBuf);
        this.ringSize = this.ring.length;
      }
    };
  }
  process(_inputs, outputs) {
    const out = outputs[0][0];
    if (!this.ring || !this.state) {
      for (let i = 0; i < out.length; i++) out[i] = 0;
      return true;
    }
    if (!Atomics.load(this.state, 2)) {
      for (let i = 0; i < out.length; i++) out[i] = 0;
      return true;
    }
    const wr = Atomics.load(this.state, 0);
    let rd = Atomics.load(this.state, 1);
    const avail = (wr - rd + this.ringSize) % this.ringSize;
    const n = Math.min(out.length, avail);
    for (let i = 0; i < n; i++) out[i] = this.ring[(rd + i) % this.ringSize];
    Atomics.store(this.state, 1, (rd + n) % this.ringSize);
    for (let i = n; i < out.length; i++) out[i] = 0;
    // Prefill is a one-shot gate: once set at the start of the stream it
    // stays set. Transient underruns fall through to the zero-fill loop
    // above; resetting prefill on drain used to amplify every hiccup into
    // a 60 ms audible stall.
    return true;
  }
}
registerProcessor('pcm-worklet', PCMWorkletProcessor);
