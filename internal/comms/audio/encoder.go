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
	"sync/atomic"

	"github.com/gordonklaus/portaudio"
	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/codec"
	"github.com/openmanet/openmanetd/internal/comms/device"
)

// broadcastEncoderChanDepth bounds the queue between the PortAudio audio
// callback (producer, fires every 20 ms) and the encode-and-send goroutine
// (consumer). 3 frames = 60 ms of consumer slack before the producer starts
// dropping frames — tight enough that an encoder spike cannot mask more
// than three frames of unsent audio. Channel-full drops are counted in
// framesDropped and surfaced in the per-cycle Debug log; if drops appear
// under load on a target device, raise this and re-bench rather than
// suppressing the counter.
const broadcastEncoderChanDepth = 3

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
	MicGain           float32
	Trace             bool
	Debug             bool
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
// only jobs are: optional VOX tap, copy the input frame into a pooled int16
// buffer, and non-blockingly hand the buffer to the encode goroutine.
func (be *BroadcastEncoder) captureCallback(in []int16) {
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

	n, encErr := be.deps.Encoder.EncodeS16(pcm, buf)
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

// Start resets per-cycle counters and starts the PortAudio capture stream.
func (be *BroadcastEncoder) Start() error {
	be.framesCaptured.Store(0)
	be.framesEncoded.Store(0)
	be.framesDropped.Store(0)
	be.encodeErrors.Store(0)

	if err := be.s.Start(); err != nil {
		return fmt.Errorf("portaudio stream start: %w", err)
	}

	return nil
}

// Stop halts the audio callback and logs per-cycle counter values.
func (be *BroadcastEncoder) Stop() error {
	stopErr := be.s.Stop()

	be.deps.Log.Debug().
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
