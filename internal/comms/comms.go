package comms

import (
	"fmt"
	"sync"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

// ─── Package-level constants ──────────────────────────────────────────────────

const (
	sampleRate        int    = 48000
	channels          int    = 1
	frameSize         int    = 960 // 20 ms at 48 kHz
	targetBitrate     int    = 32000
	encoderComplexity int    = 10
	packetLossPerc    int    = 20
	defaultKey        string = "any"
	defaultIface      string = "br-ahwlan"
	defaultCommDevice string = "/dev/hidraw0/*"
	defaultCommName   string = "AllInOneCable"
	defaultCtrlSrc    string = "openvlm"

	// encBufSize is the maximum Opus encode output buffer. 1450 bytes matches
	// the UDP MTU and is far larger than typical Opus output (~80–160 B at
	// 32 kbps).
	encBufSize = 1450
)

// ─── Buffer pools ─────────────────────────────────────────────────────────────
//
// Hot-path audio callbacks and the playout loop allocate fixed-size slices
// every 20 ms. Pooling them eliminates per-frame GC pressure.

var (
	Int16Pool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]int16, frameSize)

			return &s
		},
	}
	// float32Pool has been moved to pools.go (sibling file) so future
	// sub-packages (audio/, control/roip.go) can import it via the parent
	// package without cross-importing each other.
	EncBufPool = sync.Pool{ //nolint:gochecknoglobals
		New: func() any {
			s := make([]byte, encBufSize)

			return &s
		},
	}
)

// returnFloat32 returns a pooled []float32 slice to audiopool.Float32Pool.
// Non-pooled slices (e.g. beep buffers) are silently ignored because their
// capacity will differ from FrameSize.
func returnFloat32(s []float32) {
	audiopool.ReturnFloat32(s)
}

// ReturnInt16 returns a pooled []int16 slice to Int16Pool. Non-pooled slices
// (capacity != frameSize) are silently ignored.
func ReturnInt16(s []int16) {
	if cap(s) != frameSize {
		return
	}

	sp := &s
	Int16Pool.Put(sp)
}

// ─── buildCodec ───────────────────────────────────────────────────────────────

func (cfg *CommsConfig) buildCodec() (AudioEncoder, AudioDecoder, error) {
	complexity := cfg.EncoderComplexity
	if complexity <= 0 || complexity > 10 {
		complexity = encoderComplexity
	}

	enc, err := newOpusEncoder(complexity)
	if err != nil {
		return nil, nil, err
	}

	dec, err := newOpusDecoder()
	if err != nil {
		return nil, nil, err
	}

	return enc, dec, nil
}

// sendToAllPorts sends an encoded RTP payload to every port where sendEnabled
// is true and an rtpSess is configured. Send errors are logged at Debug level
// and do not abort remaining ports.
func (cfg *CommsConfig) sendToAllPorts(rt *CommsRuntime, payload []byte) {
	for _, pc := range rt.Ports {
		if !pc.SendEnabled.Load() || pc.RTPSess == nil {
			continue
		}

		if err := pc.RTPSess.Send(payload); err != nil {
			cfg.Log.Debug().Err(err).
				Str("addr", pc.cfg.Address).
				Int("port", pc.cfg.Port).
				Msg("comms: RTP send failed")
		}
	}
}

// ─── buildEventSource ─────────────────────────────────────────────────────────

// buildEventSource constructs the PTT EventSource for cfg.ControlSource by
// looking the name up in the control-source registry. The four supported
// backends — "openvlm", "roip", "web", "nanoptt" — register themselves via
// init() in control_register.go. Validate() (called from CommsManager.Enable)
// rejects unknown sources up front; this function returns an error if a
// caller still reaches it with an unregistered source.
func (cfg *CommsConfig) buildEventSource(rt *CommsRuntime) (EventSource, error) {
	factory, ok := controlLookup(cfg.ControlSource)
	if !ok {
		return nil, fmt.Errorf("comms: unknown ControlSource %q", cfg.ControlSource)
	}

	deps, err := cfg.buildControlDeps(rt)
	if err != nil {
		return nil, err
	}

	es, err := factory(deps)
	if err != nil {
		return nil, err
	}

	return es, nil
}
