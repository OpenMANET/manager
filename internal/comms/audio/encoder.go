// Package audio hosts the PortAudio capture/playback wrappers and the Opus
// broadcast encoder used by the comms subsystem. The package owns the
// hardware-bound side of the audio pipeline (PortAudio stream lifecycle, the
// dedicated encode-and-send goroutine, and the per-port playback open path)
// while the parent comms package retains the orchestration layer
// (CommsConfig/Manager/Service) and the receive-side playback callback.
//
// The package never imports the parent. All dependencies flow in via the
// Deps struct (for the broadcast encoder) and the PortSlot slice (for
// per-port playback streams). The parent supplies callbacks at construction
// so the package neither knows about *PortChannel nor walks the runtime.
package audio

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/codec"
	"github.com/openmanet/openmanetd/internal/comms/device"
)

// frameDuration is the wall-clock length of one captured frame at the
// configured sample rate (20 ms at 48 kHz). Used as the per-frame budget
// against which we flag over-budget encodes.
const frameDuration = time.Duration(audiopool.FrameSize) * time.Second /
	time.Duration(audiopool.SampleRate)

// broadcastEncoderChanDepth bounds the queue between the PortAudio audio
// callback (producer, fires every 20 ms) and the encode-and-send goroutine
// (consumer). 10 frames = 200 ms of consumer slack before the producer
// starts dropping frames. The depth is sized to match the receive-side
// jitter buffer prebuffer (rtp.PrebufferPackets * 20 ms) so that any
// transient encoder spike the receiver can absorb downstream the producer
// can absorb upstream too. The previous depth of 3 (60 ms) was too tight
// for slow MIPS targets where Opus encoding plus GC pauses regularly
// crossed the per-frame budget; channel-full drops then manifested as
// stutter on the receive side. Channel-full drops are still counted in
// framesDropped and surfaced in the per-cycle Debug log so a regression
// is loud, not silent.
const broadcastEncoderChanDepth = 10

// SendFn delivers an Opus payload to every send-enabled multicast port.
// The parent comms package binds it to its sendToAllPorts helper at
// BroadcastEncoder construction so this package never touches port lists.
type SendFn func(payload []byte)

// Deps is the dependency bundle BroadcastEncoder needs from the parent
// package. Construct it once at startup and pass to NewBroadcastEncoder.
type Deps struct {
	Log               zerolog.Logger
	Encoder           codec.AudioEncoder
	Send              SendFn
	Tap               *atomic.Pointer[chan []float32]
	InputDeviceSpec   string
	OutputDeviceSpec  string
	CaptureLatencyMs  int
	PlaybackLatencyMs int
	// CaptureFramesPerBuffer is the per-callback frame count passed to
	// PortAudio as StreamParameters.FramesPerBuffer. A value of 0 means
	// paFramesPerBufferUnspecified (let PortAudio choose a frame count
	// aligned with the native ALSA period). A positive value is passed
	// through verbatim. The default (audiopool.FrameSize = 960 @ 48 kHz
	// mono = 20 ms) matches the Opus encoder frame so every callback
	// produces exactly one RTP packet with no accumulation step.
	CaptureFramesPerBuffer int
	MicGain                float32
	Trace                  bool
	Debug                  bool
}

// paStream is the minimal subset of *portaudio.Stream that BroadcastEncoder
// depends on. It exists so unit tests can inject a fake stream without
// opening real audio hardware. It is intentionally NOT device.AudioStream
// because Info() *portaudio.StreamInfo is portaudio-specific and does not
// belong on the public surface.
type paStream interface {
	Start() error
	Stop() error
	Close() error
	Info() *portaudio.StreamInfo
}

// BroadcastEncoder owns the PortAudio capture stream and a dedicated
// encode-and-send goroutine. It exists so that the audio callback thread
// never runs the Opus encoder or a blocking UDP write — both of those move
// to the goroutine, which has its own scheduling slack absorbed by encCh.
//
// The capture hot path is int16-native: the audio callback registers an
// int16 signature so PortAudio delivers samples in the native codec format,
// the encode worker calls EncodeS16 directly, and the float32↔int16
// conversion is eliminated. The pooled []int16 frame is released via defer
// after each send.
type BroadcastEncoder struct {
	s              paStream
	encCh          chan *[]int16
	done           chan struct{}
	deps           Deps
	framesCaptured atomic.Int64
	framesEncoded  atomic.Int64
	framesDropped  atomic.Int64
	encodeErrors   atomic.Int64

	// encodeDurMaxNs / encodeDurSumNs / encodeDurCount track the
	// per-frame Opus encode time so the per-cycle Stop() debug log can
	// report max and average encode latency. On constrained MIPS/ARM
	// targets this is the canonical signal that the encoder is starving
	// the consumer (per-frame budget = frameDuration ≈ 20 ms).
	// CAS-loop max + atomic accumulators keep encodeOne lock-free.
	encodeDurMaxNs atomic.Int64
	encodeDurSumNs atomic.Int64
	encodeDurCount atomic.Int64

	// overBudgetWarned ensures the over-budget encode warning fires at
	// most once per Start/Stop cycle so a sustained overrun does not
	// flood the log. Reset to false in Start.
	overBudgetWarned atomic.Bool

	// captureThreadElevateOnce guards the first-call elevation of the
	// PortAudio capture callback thread to SCHED_FIFO. The elevator is
	// package-level so tests can replace it with a fake. sync.Once is
	// used instead of an atomic bool because the Once guarantees the
	// elevator has finished running before any subsequent caller
	// observes the done flag, which matters for the deterministic
	// "elevator called exactly once per cycle" assertion in the tests.
	captureThreadElevateOnce sync.Once

	// lastCaptureNs / captureGapMaxNs / captureLateCount track the
	// wall-clock inter-arrival time between consecutive PortAudio
	// capture callbacks. On constrained hardware PortAudio can
	// deliver frames in bursts with gaps even when the encoder
	// reports zero drops — the frames still arrive and still encode,
	// but their emission onto the wire carries the capture jitter,
	// which can exceed the receive-side prebuffer (100 ms) and cause
	// stutter at the remote playout clock. captureGapMaxNs is the
	// worst observed inter-arrival; captureLateCount is the number
	// of callbacks whose delta exceeded 2 * frameDuration (40 ms).
	// The captureCallback is single-producer (one PortAudio stream
	// thread), so the Load/Store/CAS sequence races only against
	// resets in Start, which happens while the stream is stopped.
	lastCaptureNs    atomic.Int64
	captureGapMaxNs  atomic.Int64
	captureLateCount atomic.Int64
}

// Compile-time assertion: BroadcastEncoder satisfies device.AudioStream so
// the parent's CommsRuntime.BroadcastStream field accepts it directly.
var _ device.AudioStream = (*BroadcastEncoder)(nil)

// NewBroadcastEncoder constructs the wrapper, opens the PortAudio capture
// stream with the int16 callback, and spawns the encode goroutine.
func NewBroadcastEncoder(deps Deps, inParams portaudio.StreamParameters) (*BroadcastEncoder, error) {
	be := &BroadcastEncoder{
		deps:  deps,
		encCh: make(chan *[]int16, broadcastEncoderChanDepth),
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
// only jobs are: elevate the thread to SCHED_FIFO once, optional VOX tap,
// copy the input frame into a pooled int16 buffer, and non-blockingly hand
// the buffer to the encode goroutine.
func (be *BroadcastEncoder) captureCallback(in []int16) {
	// First-call thread elevation. We run this inside the callback rather
	// than at stream-open time because the PortAudio audio thread is
	// created by the host API and the Go side only observes it when it
	// calls us back for the first time. gordonklaus/portaudio uses a
	// direct //export streamCallback (no Go-side channel indirection),
	// so the current OS thread inside this function IS the PortAudio
	// audio thread — elevating pid 0 to SCHED_FIFO therefore elevates
	// the correct thread and the elevation persists across subsequent
	// callback invocations from the same C thread. See thread_linux.go
	// for the syscall details and the graceful-EPERM fallback.
	be.captureThreadElevateOnce.Do(func() {
		elevateAudioThread(be.deps.Log, "capture")
	})

	be.recordCaptureArrival(time.Now())

	// Optional VOX tap. The ROIP VOX consumer (control/roip.go) operates on
	// float32 frames for RMS energy. This is a boundary conversion off the
	// hot path — the tap is only active when the ROIP control source is
	// selected and VOX is currently monitoring. Regular TX (openvlm,
	// nanoptt, web) never enters this branch and pays nothing.
	if be.deps.Tap != nil {
		if tapPtr := be.deps.Tap.Load(); tapPtr != nil {
			fp := audiopool.Float32Pool.Get().(*[]float32) //nolint:forcetypeassert

			f := (*fp)[:audiopool.FrameSize]
			for i, v := range in {
				f[i] = float32(v) / 32768
			}

			select {
			case *tapPtr <- f:
			default:
				audiopool.ReturnFloat32(f)
			}
		}
	}

	be.framesCaptured.Add(1)

	fp := audiopool.Int16Pool.Get().(*[]int16) //nolint:forcetypeassert
	f := (*fp)[:audiopool.FrameSize]
	copy(f, in)
	*fp = f

	// Non-blocking hand-off. If the consumer is so far behind that
	// broadcastEncoderChanDepth frames of slack are exhausted, drop this
	// frame and count it. The audio callback MUST NOT block.
	select {
	case be.encCh <- fp:
	default:
		be.framesDropped.Add(1)
		audiopool.Int16Pool.Put(fp)
	}
}

// encodeLoop drains encCh, applying gain → Opus EncodeS16 → RTP send.
// Exits when encCh is closed by Close.
func (be *BroadcastEncoder) encodeLoop() {
	defer close(be.done)

	for fp := range be.encCh {
		be.encodeOne(fp)
	}
}

// encodeOne processes a single captured frame: applies mic gain in the
// integer domain (clipping to int16 range), encodes via EncodeS16, and
// ships the payload via Deps.Send. Pool buffers are released via defer so
// a panic in the encoder still returns them.
func (be *BroadcastEncoder) encodeOne(fp *[]int16) {
	defer audiopool.Int16Pool.Put(fp)

	pcm := *fp

	gain := be.deps.MicGain
	if gain != 1.0 && gain > 0 {
		// Apply gain in int32 space with hard clipping to int16 range.
		for i, v := range pcm {
			scaled := float32(v) * gain
			if scaled > 32767 {
				scaled = 32767
			} else if scaled < -32768 {
				scaled = -32768
			}

			pcm[i] = int16(scaled)
		}
	}

	bufPtr := audiopool.EncBufPool.Get().(*[]byte) //nolint:forcetypeassert

	buf := *bufPtr
	defer audiopool.EncBufPool.Put(bufPtr)

	encStart := time.Now()
	n, encErr := be.deps.Encoder.EncodeS16(pcm, buf)
	encDur := time.Since(encStart)

	be.recordEncodeDuration(encDur)

	if encErr != nil {
		be.encodeErrors.Add(1)
		be.deps.Log.Debug().Err(encErr).Msg("comms: opus encode failed")

		return
	}

	be.framesEncoded.Add(1)
	be.deps.Send(buf[:n])

	if be.deps.Trace {
		be.deps.Log.Trace().Int("encoded_bytes", n).Msg("comms: multicast packet sent")
	}
}

// recordCaptureArrival tracks inter-arrival timing between consecutive
// PortAudio capture callbacks. The first callback in a cycle has no
// previous timestamp to compare against, so it only seeds lastCaptureNs
// and returns. Subsequent callbacks compute the delta from the previous
// arrival, update the running max via a CAS loop, and increment the
// late-callback counter if the gap ≥ 2 * frameDuration.
//
// captureCallback is single-producer (one PortAudio stream thread), so
// the Load/Store/CAS sequence races only against resets in Start, which
// is guaranteed to run with the stream stopped.
func (be *BroadcastEncoder) recordCaptureArrival(now time.Time) {
	nowNs := now.UnixNano()

	prevNs := be.lastCaptureNs.Swap(nowNs)
	if prevNs == 0 {
		return
	}

	deltaNs := nowNs - prevNs
	if deltaNs <= 0 {
		return
	}

	for {
		cur := be.captureGapMaxNs.Load()
		if deltaNs <= cur {
			break
		}

		if be.captureGapMaxNs.CompareAndSwap(cur, deltaNs) {
			break
		}
	}

	if deltaNs >= 2*frameDuration.Nanoseconds() {
		be.captureLateCount.Add(1)
	}
}

// recordEncodeDuration accumulates the encode-time stats and emits a
// one-shot Warn the first time a frame crosses the per-frame budget
// within a Start/Stop cycle. Lock-free: max is updated via a CAS loop,
// sum/count via plain Add, the warn flag via CompareAndSwap.
func (be *BroadcastEncoder) recordEncodeDuration(d time.Duration) {
	ns := d.Nanoseconds()

	be.encodeDurSumNs.Add(ns)
	be.encodeDurCount.Add(1)

	for {
		cur := be.encodeDurMaxNs.Load()
		if ns <= cur {
			break
		}

		if be.encodeDurMaxNs.CompareAndSwap(cur, ns) {
			break
		}
	}

	if d >= frameDuration && be.overBudgetWarned.CompareAndSwap(false, true) {
		be.deps.Log.Warn().
			Dur("encode_dur", d).
			Dur("frame_budget", frameDuration).
			Msg("comms: opus encode exceeded per-frame budget; expect TX frame drops " +
				"and RX stutter — lower cfg.EncoderComplexity")
	}
}

// Start resets per-cycle counters and starts the PortAudio capture stream.
func (be *BroadcastEncoder) Start() error {
	// Reset the thread-elevation guard so the first callback of the new
	// Start/Stop cycle re-runs elevateAudioThread. PortAudio does not
	// guarantee that the audio thread persists across Stop/Start, so a
	// re-elevation is mandatory for safety. Safe to reassign without a
	// mutex because Start is not called concurrently with the capture
	// callback (the stream is stopped at this point).
	be.captureThreadElevateOnce = sync.Once{}

	be.framesCaptured.Store(0)
	be.framesEncoded.Store(0)
	be.framesDropped.Store(0)
	be.encodeErrors.Store(0)
	be.encodeDurMaxNs.Store(0)
	be.encodeDurSumNs.Store(0)
	be.encodeDurCount.Store(0)
	be.overBudgetWarned.Store(false)
	be.lastCaptureNs.Store(0)
	be.captureGapMaxNs.Store(0)
	be.captureLateCount.Store(0)

	if err := be.s.Start(); err != nil {
		return fmt.Errorf("portaudio stream start: %w", err)
	}

	return nil
}

// Stop halts the audio callback and logs per-cycle counter values.
func (be *BroadcastEncoder) Stop() error {
	stopErr := be.s.Stop()

	maxDur := time.Duration(be.encodeDurMaxNs.Load())

	var avgDur time.Duration
	if count := be.encodeDurCount.Load(); count > 0 {
		avgDur = time.Duration(be.encodeDurSumNs.Load() / count)
	}

	captureGapMax := time.Duration(be.captureGapMaxNs.Load())

	be.deps.Log.Debug().
		Int64("captured", be.framesCaptured.Load()).
		Int64("encoded", be.framesEncoded.Load()).
		Int64("dropped", be.framesDropped.Load()).
		Int64("encode_errors", be.encodeErrors.Load()).
		Dur("encode_dur_max", maxDur).
		Dur("encode_dur_avg", avgDur).
		Dur("frame_budget", frameDuration).
		Dur("capture_gap_max", captureGapMax).
		Int64("capture_late", be.captureLateCount.Load()).
		Msg("comms: broadcast cycle stats")

	if stopErr != nil {
		return fmt.Errorf("portaudio stream stop: %w", stopErr)
	}

	return nil
}

// Close stops the audio thread, terminates the encode goroutine, and
// releases the PortAudio resources.
func (be *BroadcastEncoder) Close() error {
	_ = be.s.Stop()
	close(be.encCh)
	<-be.done

	if err := be.s.Close(); err != nil {
		return fmt.Errorf("portaudio stream close: %w", err)
	}

	return nil
}
