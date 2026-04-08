package comms

import (
	"fmt"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/codec"
	"github.com/openmanet/openmanetd/internal/comms/control"
)

// ─── Package-level constants ──────────────────────────────────────────────────

const (
	targetBitrate int = 32000
	// encoderComplexity is the default Opus encoder complexity used when
	// CommsConfig.EncoderComplexity is unset or out of range. Complexity is
	// the libopus quality/CPU tradeoff knob: 0 is fastest, 10 is highest
	// quality. The libopus reference default for VoIP is 5.
	//
	// We default to 5 because the deployment target includes constrained
	// MIPS/ARM edge routers (per CLAUDE.md the binary must run on
	// linux/mipsle). Empirically, complexity 10 at 48 kHz mono can take
	// 20-30 ms per 20 ms frame on those CPUs, which saturates the
	// per-frame budget and causes the PortAudio capture callback to
	// overflow encCh and drop frames — surfacing as audible stutter on
	// the receive side. Web mode bypasses this entirely (the browser
	// supplies pre-encoded Opus), which is why the symptom is PortAudio-
	// only. Operators on faster CPUs can opt back in to complexity 10
	// via cfg.EncoderComplexity.
	encoderComplexity int    = 5
	packetLossPerc    int    = 20
	defaultKey        string = "any"
	defaultIface      string = "br-ahwlan"
	defaultCommDevice string = "/dev/hidraw0/*"
	defaultCommName   string = "AllInOneCable"
	defaultCtrlSrc    string = "openvlm"
)

// ─── buildCodec ───────────────────────────────────────────────────────────────

func (cfg *CommsConfig) buildCodec() (codec.AudioEncoder, codec.AudioDecoder, error) {
	complexity := cfg.EncoderComplexity
	if complexity <= 0 || complexity > 10 {
		complexity = encoderComplexity
	}

	enc, err := codec.NewOpusEncoder(audiopool.SampleRate, audiopool.Channels, targetBitrate, complexity, packetLossPerc)
	if err != nil {
		return nil, nil, err
	}

	dec, err := codec.NewOpusDecoder(audiopool.SampleRate, audiopool.Channels)
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
func (cfg *CommsConfig) buildEventSource(rt *CommsRuntime) (control.EventSource, error) {
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
