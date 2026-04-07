package comms

import (
	"context"
	"fmt"
	"math"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control"
)

// Start initializes all comms subsystems and blocks until ctx is canceled.
// Returns nil on clean shutdown, or an error if initialization fails.
// The caller is responsible for canceling ctx to stop the subsystem.
func (cfg *CommsConfig) Start(ctx context.Context) error {
	if !cfg.Enable {
		cfg.Log.Info().Msg("comms: functionality disabled; not starting")

		return nil
	}

	cfg.applyDefaults()

	if cfg.ControlSource != controlSourceWeb {
		if cfg.ControlSource == defaultCtrlSrc || cfg.ControlSource == controlSourceROIP {
			control.DetectAndSetALSACard(cfg.Log)
		}
	}

	switch {
	case cfg.Trace:
		cfg.Log = cfg.Log.Level(zerolog.TraceLevel)
	case cfg.Debug:
		cfg.Log = cfg.Log.Level(zerolog.DebugLevel)
	}

	if cfg.Debug && cfg.ControlSource != controlSourceWeb {
		cfg.logInputDeviceList()
	}

	cfg.Log.Info().Msgf(
		"comms: starting iface=%s talkgroups=%d key=%s debug=%t trace=%t loopback=%t device=%s",
		cfg.Iface, len(cfg.McastPorts), cfg.CommKey,
		cfg.Debug, cfg.Trace, cfg.Loopback, cfg.ControlSource,
	)

	// ── codec ──────────────────────────────────────────────────────────────
	enc, dec, err := cfg.buildCodec()
	if err != nil {
		return fmt.Errorf("comms: failed to build Opus codec: %w", err)
	}

	// ── beep tones ─────────────────────────────────────────────────────────
	// Phase 5: beep buffers are int16-native so they can be written directly
	// into the PortAudio int16 playback callback without an extra conversion.
	// Amplitude 0.2 * 32767 ≈ 6553 matches the previous float32 volume.
	beepStart := make([]int16, frameSize)
	beepStop := make([]int16, frameSize)

	const beepAmp = 0.2 * 32767

	for i := range beepStart {
		beepStart[i] = int16(math.Sin(2*math.Pi*1000*float64(i)/float64(sampleRate)) * beepAmp)
		beepStop[i] = int16(math.Sin(2*math.Pi*600*float64(i)/float64(sampleRate)) * beepAmp)
	}

	// ── network ────────────────────────────────────────────────────────────
	ports, localIP, netErr := cfg.buildNetwork()
	if netErr != nil {
		return fmt.Errorf("comms: failed to set up network: %w", netErr)
	}

	// ── assemble runtime ───────────────────────────────────────────────────
	rt := &CommsRuntime{
		Encoder:         enc,
		Decoder:         dec,
		Ports:           ports,
		BeepBufferStart: beepStart,
		BeepBufferStop:  beepStop,
	}

	rt.LocalIP.Store(&localIP)

	defer func() {
		for _, pc := range rt.Ports {
			if pc.Receiver != nil {
				_ = pc.Receiver.Close()
			}

			if pc.RTPSess != nil {
				if s, ok := pc.RTPSess.(*RTPSession); ok {
					_ = s.Close()
				}
			}
		}

		cfg.runtime = nil

		SetDefault(nil)
	}()

	cfg.runtime = rt
	SetDefault(cfg)

	// ── event source ───────────────────────────────────────────────────────
	src, srcErr := cfg.buildEventSource(rt)
	if srcErr != nil {
		return fmt.Errorf("comms: failed to build event source: %w", srcErr)
	}

	// ── audio I/O ─────────────────────────────────────────────────────────
	if cfg.ControlSource == controlSourceWeb {
		// Web mode: skip PortAudio entirely; the browser provides audio I/O.
		rt.WebBridge = NewWebAudioBridge(cfg, rt, cfg.Log)
	} else {
		cleanup, hwErr := cfg.startHardwareAudio(rt)
		if hwErr != nil {
			return hwErr
		}

		defer cleanup()
	}

	// ── run loop ───────────────────────────────────────────────────────────
	cfg.Run(ctx, rt, src)

	cfg.Log.Info().Msg("comms: subsystem stopped")

	return nil
}
