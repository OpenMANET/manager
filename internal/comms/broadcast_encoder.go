package comms

import (
	"fmt"
	"sync/atomic"

	"github.com/gordonklaus/portaudio"
)

// broadcastEncoderChanDepth bounds the queue between the PortAudio audio
// callback (producer, fires every 20 ms) and the encode-and-send goroutine
// (consumer). At 8 frames = 160 ms it absorbs ~140 ms of consumer slack
// before the producer starts dropping frames. Channel-full drops are
// counted in framesDropped and surfaced in the per-cycle Debug log.
const broadcastEncoderChanDepth = 8

// paStream is the minimal subset of *portaudio.Stream that broadcastEncoder
// depends on. It exists so unit tests can inject a fake stream without
// opening real audio hardware.
type paStream interface {
	Start() error
	Stop() error
	Close() error
	Info() *portaudio.StreamInfo
}

// broadcastEncoder owns the PortAudio capture stream and a dedicated
// encode-and-send goroutine. It exists so that the audio callback thread
// never runs the Opus encoder or a blocking UDP write — both of those move
// to the goroutine, which has its own scheduling slack absorbed by encCh.
//
// The audio callback only copies the captured float32 frame into a pooled
// slice and non-blockingly hands it off; if the consumer cannot keep up the
// frame is counted as dropped and discarded. Gain → int16 → Opus encode →
// sendToAllPorts all run on the encode goroutine, isolated from any cgo
// latency, GC pause, or UDP backpressure that would otherwise starve the
// audio thread and cause ADC overruns at the device.
//
// Lifecycle:
//   - newBroadcastEncoder opens the PortAudio stream (callback registered)
//     and spawns the encode goroutine.
//   - Start resets per-cycle counters and starts the audio thread; the
//     callback begins firing every ~20 ms.
//   - Stop halts the audio thread (no more frames enter encCh) and logs
//     per-cycle counters at Debug level. The encode goroutine remains
//     blocked on <-encCh ready for the next Start, so PTT cycles do not
//     pay a goroutine recreate cost.
//   - Close stops the audio thread, closes encCh (signaling the goroutine
//     to drain remaining frames and exit), waits on done, and releases the
//     PortAudio stream.
type broadcastEncoder struct {
	s     paStream
	cfg   *CommsConfig
	rt    *CommsRuntime
	encCh chan *[]float32
	done  chan struct{}

	// Counters reset in Start, logged in Stop. framesCaptured is written by
	// the audio callback only; framesEncoded / framesDropped / encodeErrors
	// are written by the encode goroutine only; both are read in Stop.
	framesCaptured atomic.Int64
	framesEncoded  atomic.Int64
	framesDropped  atomic.Int64
	encodeErrors   atomic.Int64
}

// newBroadcastEncoder constructs the wrapper, opens the PortAudio capture
// stream with the callback, and spawns the encode goroutine.
func newBroadcastEncoder(cfg *CommsConfig, rt *CommsRuntime, inParams portaudio.StreamParameters) (*broadcastEncoder, error) {
	be := &broadcastEncoder{
		cfg:   cfg,
		rt:    rt,
		encCh: make(chan *[]float32, broadcastEncoderChanDepth),
		done:  make(chan struct{}),
	}

	s, err := portaudio.OpenStream(inParams, be.captureCallback)
	if err != nil {
		return nil, fmt.Errorf("open broadcast stream: %w", err)
	}

	be.s = s

	go be.encodeLoop()

	return be, nil
}

// captureCallback runs on the PortAudio audio callback thread and MUST NOT
// block, allocate from the heap, or hold any mutex other than briefly. Its
// only jobs are: optional VOX tap (preserved unchanged), copy the input
// frame into a pooled buffer, and non-blockingly hand the buffer to the
// encode goroutine.
func (be *broadcastEncoder) captureCallback(in []float32) {
	// Optional VOX tap, identical to the previous inline behavior. The VOX
	// path consumes the raw float32 frame for energy monitoring; it is
	// independent of the encode pipeline.
	if tapPtr := be.rt.broadcastTap.Load(); tapPtr != nil {
		fp := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
		f := (*fp)[:frameSize]
		copy(f, in)

		select {
		case *tapPtr <- f:
		default:
			returnFloat32(f)
		}
	}

	be.framesCaptured.Add(1)

	fp := float32Pool.Get().(*[]float32) //nolint:forcetypeassert
	f := (*fp)[:frameSize]
	copy(f, in)

	// Non-blocking hand-off. If the consumer is so far behind that
	// broadcastEncoderChanDepth frames of slack are exhausted, drop this
	// frame and count it. The audio callback MUST NOT block — blocking
	// here would cause the symptom we are trying to fix.
	select {
	case be.encCh <- fp:
	default:
		be.framesDropped.Add(1)
		float32Pool.Put(fp)
	}
}

// encodeLoop drains encCh, applying gain → int16 → Opus encode → RTP send.
// Exits when encCh is closed by Close.
func (be *broadcastEncoder) encodeLoop() {
	defer close(be.done)

	for fp := range be.encCh {
		be.encodeOne(fp)
	}
}

// encodeOne processes a single captured frame: applies mic gain, converts
// float32 → int16 with hard clipping, encodes to Opus, and ships the
// payload via sendToAllPorts. Pool buffers are released via defer so a
// panic in the encoder still returns them.
func (be *broadcastEncoder) encodeOne(fp *[]float32) {
	in := *fp
	defer float32Pool.Put(fp)

	gain := be.cfg.MicGain
	if gain <= 0 {
		gain = 1.0
	}

	pcmPtr := int16Pool.Get().(*[]int16) //nolint:forcetypeassert

	pcm := (*pcmPtr)[:len(in)]
	defer int16Pool.Put(pcmPtr)

	// Convert float32 samples [-1.0, 1.0] → int16 [-32767, 32767].
	// MicGain is applied first; the result is hard-clipped to the legal
	// float range before scaling to prevent int16 overflow.
	for i, v := range in {
		v *= gain
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}

		pcm[i] = int16(v * 32767)
	}

	bufPtr := encBufPool.Get().(*[]byte) //nolint:forcetypeassert

	buf := *bufPtr
	defer encBufPool.Put(bufPtr)

	n, encErr := be.rt.encoder.Encode(pcm, buf)
	if encErr != nil {
		be.encodeErrors.Add(1)
		// Surface the previously-silent error so on-device testing can
		// see whether encode is failing under pressure. Debug-level so
		// it does not spam Info in healthy operation.
		be.cfg.Log.Debug().Err(encErr).Msg("comms: opus encode failed")

		return
	}

	be.framesEncoded.Add(1)
	be.cfg.sendToAllPorts(be.rt, buf[:n])

	if be.cfg.Trace {
		be.cfg.Log.Trace().Int("encoded_bytes", n).Msg("comms: multicast packet sent")
	}
}

// Start resets per-cycle counters and starts the PortAudio capture stream.
// Counters are reset before s.Start() so the audio callback sees zero on
// its first invocation.
func (be *broadcastEncoder) Start() error {
	be.framesCaptured.Store(0)
	be.framesEncoded.Store(0)
	be.framesDropped.Store(0)
	be.encodeErrors.Store(0)

	if err := be.s.Start(); err != nil {
		return fmt.Errorf("portaudio stream start: %w", err)
	}

	return nil
}

// Stop halts the audio callback (PortAudio's Stop blocks until any
// in-flight callback finishes) and logs the per-PTT-cycle counter values
// so on-device tail -f shows one summary line per transmission.
func (be *broadcastEncoder) Stop() error {
	stopErr := be.s.Stop()

	be.cfg.Log.Debug().
		Int64("captured", be.framesCaptured.Load()).
		Int64("encoded", be.framesEncoded.Load()).
		Int64("dropped", be.framesDropped.Load()).
		Int64("encode_errors", be.encodeErrors.Load()).
		Msg("comms: broadcast cycle stats")

	if stopErr != nil {
		return fmt.Errorf("portaudio stream stop: %w", stopErr)
	}

	return nil
}

// Close stops the audio thread, terminates the encode goroutine, and
// releases the PortAudio resources. After Close returns, the
// broadcastEncoder must not be used.
func (be *broadcastEncoder) Close() error {
	_ = be.s.Stop()
	close(be.encCh)
	<-be.done

	if err := be.s.Close(); err != nil {
		return fmt.Errorf("portaudio stream close: %w", err)
	}

	return nil
}
