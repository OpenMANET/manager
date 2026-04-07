package comms

import (
	"fmt"
	"sync/atomic"

	"github.com/gordonklaus/portaudio"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
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
// Phase 5 of the comms refactor switched the capture hot path from float32
// to int16: the audio callback registers an int16 signature so PortAudio
// delivers samples in the native codec format, the encode worker calls
// EncodeS16 directly, and the float32↔int16 conversion is eliminated. The
// pooled []int16 frame is released via defer after each send.
type broadcastEncoder struct {
	s     paStream
	cfg   *CommsConfig
	rt    *CommsRuntime
	encCh chan *[]int16
	done  chan struct{}

	framesCaptured atomic.Int64
	framesEncoded  atomic.Int64
	framesDropped  atomic.Int64
	encodeErrors   atomic.Int64
}

// newBroadcastEncoder constructs the wrapper, opens the PortAudio capture
// stream with the int16 callback, and spawns the encode goroutine.
func newBroadcastEncoder(cfg *CommsConfig, rt *CommsRuntime, inParams portaudio.StreamParameters) (*broadcastEncoder, error) {
	be := &broadcastEncoder{
		cfg:   cfg,
		rt:    rt,
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
func (be *broadcastEncoder) captureCallback(in []int16) {
	// Optional VOX tap. The ROIP VOX consumer (roip.go) operates on float32
	// frames for RMS energy. This is a boundary conversion off the hot path
	// — the tap is only active when the ROIP control source is selected and
	// VOX is currently monitoring. Regular TX (openvlm, nanoptt, web) never
	// enters this branch and pays nothing.
	if tapPtr := be.rt.BroadcastTap.Load(); tapPtr != nil {
		fp := audiopool.Float32Pool.Get().(*[]float32) //nolint:forcetypeassert

		f := (*fp)[:frameSize]
		for i, v := range in {
			f[i] = float32(v) / 32768
		}

		select {
		case *tapPtr <- f:
		default:
			returnFloat32(f)
		}
	}

	be.framesCaptured.Add(1)

	fp := Int16Pool.Get().(*[]int16) //nolint:forcetypeassert
	f := (*fp)[:frameSize]
	copy(f, in)
	*fp = f

	// Non-blocking hand-off. If the consumer is so far behind that
	// broadcastEncoderChanDepth frames of slack are exhausted, drop this
	// frame and count it. The audio callback MUST NOT block.
	select {
	case be.encCh <- fp:
	default:
		be.framesDropped.Add(1)
		Int16Pool.Put(fp)
	}
}

// encodeLoop drains encCh, applying gain → Opus EncodeS16 → RTP send.
// Exits when encCh is closed by Close.
func (be *broadcastEncoder) encodeLoop() {
	defer close(be.done)

	for fp := range be.encCh {
		be.encodeOne(fp)
	}
}

// encodeOne processes a single captured frame: applies mic gain in the
// integer domain (clipping to int16 range), encodes via EncodeS16, and
// ships the payload via sendToAllPorts. Pool buffers are released via
// defer so a panic in the encoder still returns them.
func (be *broadcastEncoder) encodeOne(fp *[]int16) {
	defer Int16Pool.Put(fp)

	pcm := *fp

	gain := be.cfg.MicGain
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

	bufPtr := EncBufPool.Get().(*[]byte) //nolint:forcetypeassert

	buf := *bufPtr
	defer EncBufPool.Put(bufPtr)

	n, encErr := be.rt.Encoder.EncodeS16(pcm, buf)
	if encErr != nil {
		be.encodeErrors.Add(1)
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

// Stop halts the audio callback and logs per-cycle counter values.
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
// releases the PortAudio resources.
func (be *broadcastEncoder) Close() error {
	_ = be.s.Stop()
	close(be.encCh)
	<-be.done

	if err := be.s.Close(); err != nil {
		return fmt.Errorf("portaudio stream close: %w", err)
	}

	return nil
}
