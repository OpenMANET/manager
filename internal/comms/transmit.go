package comms

import (
	"context"
	"time"

	"github.com/openmanet/openmanetd/internal/comms/control"
)

// defaultPttStartDelayMs is the start-tone settle window applied when
// CommsConfig.PttStartDelayMs is left unset (≤ 0). 50 ms is short enough to
// be imperceptible at the operator end and long enough to let USB audio
// class capture devices commit their first DMA cycle before the encoder
// starts pulling frames. The previous hard-coded 200 ms was a conservative
// guess; the PortAudio output callback already drains the beep buffer
// before falling through to playoutOneFrame so beep + mic samples cannot
// collide regardless of this duration. To skip the wait entirely set
// PttStartDelayMs to a negative value.
const defaultPttStartDelayMs = 50

// pttStartDelay returns the configured start-tone settle duration. A
// negative PttStartDelayMs means "skip the wait"; a zero or unset value
// falls back to defaultPttStartDelayMs; positive values are taken as-is.
func (cfg *CommsConfig) pttStartDelay() time.Duration {
	switch {
	case cfg.PttStartDelayMs < 0:
		return 0
	case cfg.PttStartDelayMs == 0:
		return defaultPttStartDelayMs * time.Millisecond
	default:
		return time.Duration(cfg.PttStartDelayMs) * time.Millisecond
	}
}

// ─── Transmission state ───────────────────────────────────────────────────────

func (cfg *CommsConfig) isBroadcasting(rt *CommsRuntime) bool {
	return rt.Broadcasting.Load()
}

func (cfg *CommsConfig) drainPlaybackBuffer(rt *CommsRuntime) {
	for _, pc := range rt.Ports {
		buf := pc.PlaybackBuffer
		if buf == nil {
			continue
		}

		// Drain this port's buffer non-blockingly via a labeled break.
	drain:
		for {
			select {
			case <-buf:
			default:
				break drain
			}
		}
	}
}

// beginTransmission starts the mic capture stream and plays the start-tone
// into the local speaker to signal the start of transmission.
//
// If the broadcast stream is nil or fails to start, rt.ReopenBroadcast is
// called to rebuild it using the input device that was resolved at startup.
func (cfg *CommsConfig) beginTransmission(rt *CommsRuntime) {
	if rt.Broadcasting.Load() {
		cfg.Log.Debug().Msg("PTTDown ignored; already broadcasting")

		return
	}

	// Half-duplex: refuse to transmit while the channel is actively receiving
	// audio from a remote peer.
	if cfg.isReceivingRemote(rt) {
		cfg.Log.Debug().Msg("PTTDown ignored; channel busy (receiving remote audio)")

		return
	}

	rt.Broadcasting.Store(true)

	// Web mode: the browser provides its own audio I/O and UI feedback,
	// so skip beep tones, playback drain, and PortAudio stream management.
	if rt.WebBridge != nil {
		cfg.Log.Debug().Msg("Begin web transmission")

		return
	}

	cfg.Log.Debug().Msg("Begin transmission: playing start tone and starting mic stream")
	cfg.drainPlaybackBuffer(rt)

	for _, pc := range rt.Ports {
		if pc.PlaybackBuffer != nil {
			pc.PlaybackBuffer <- rt.BeepBufferStart
		}
	}

	// Brief settle window before the mic capture stream starts. The
	// PortAudio output callback drains the beep buffer ahead of
	// playoutOneFrame so beep and mic samples cannot collide; the wait is
	// purely for hardware that warms its capture path slowly. Configurable
	// via CommsConfig.PttStartDelayMs (see pttStartDelay).
	if d := cfg.pttStartDelay(); d > 0 {
		time.Sleep(d)
	}

	if rt.BroadcastStream == nil {
		cfg.Log.Warn().Msg("Mic stream is nil; attempting to reopen")

		if rt.ReopenBroadcast != nil {
			if err := rt.ReopenBroadcast(); err != nil {
				cfg.Log.Error().Err(err).Msg("Failed to reopen mic stream")
				rt.Broadcasting.Store(false)

				return
			}
		}
	}

	if rt.BroadcastStream == nil {
		cfg.Log.Error().Msg("Mic stream still nil after reopen attempt")
		rt.Broadcasting.Store(false)

		return
	}

	if err := rt.BroadcastStream.Start(); err != nil {
		cfg.Log.Error().Err(err).Msg("Failed to start mic stream; attempting to reopen stream")

		if rt.ReopenBroadcast != nil {
			if reErr := rt.ReopenBroadcast(); reErr != nil {
				cfg.Log.Error().Err(reErr).Msg("Failed to reopen mic stream")
				rt.Broadcasting.Store(false)

				return
			}
		}

		if err := rt.BroadcastStream.Start(); err != nil {
			cfg.Log.Error().Err(err).Msg("Failed to start mic stream after reopen")
			rt.Broadcasting.Store(false)

			return
		}
	}

	cfg.Log.Debug().Msg("Mic stream started")
}

// endTransmission stops the mic capture stream and plays the stop-tone.
func (cfg *CommsConfig) endTransmission(rt *CommsRuntime) {
	if !rt.Broadcasting.Load() {
		cfg.Log.Debug().Msg("PTTUp ignored; mic already idle")

		return
	}

	// Web mode: skip PortAudio stream management and beep tones.
	if rt.WebBridge != nil {
		cfg.Log.Debug().Msg("End web transmission")
		rt.Broadcasting.Store(false)

		return
	}

	cfg.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")

	if rt.BroadcastStream == nil {
		cfg.Log.Warn().Msg("Mic stream was nil during stop")
	} else if err := rt.BroadcastStream.Stop(); err != nil {
		cfg.Log.Error().Err(err).Msg("stop mic")
	} else {
		cfg.Log.Debug().Msg("Mic stream stopped")
	}

	cfg.drainPlaybackBuffer(rt)

	for _, pc := range rt.Ports {
		if pc.PlaybackBuffer != nil {
			pc.PlaybackBuffer <- rt.BeepBufferStop
		}
	}

	rt.Broadcasting.Store(false)
}

// Run is the main event loop. It starts a receiveLoop goroutine for every
// Receive-capable port plus a single halfDuplexDecayLoop that clears the
// cached RemoteRxActive flag when every gate has gone quiet, then blocks
// dispatching PTT events until ctx is canceled.
func (cfg *CommsConfig) Run(ctx context.Context, rt *CommsRuntime, src control.EventSource) {
	for _, pc := range rt.Ports {
		if pc.Receiver != nil {
			go cfg.receiveLoop(ctx, pc, rt)
		}
	}

	go cfg.halfDuplexDecayLoop(ctx, rt)

	events := src.Events(ctx)

	for {
		select {
		case <-ctx.Done():
			cfg.Log.Info().Msg("comms context canceled; exiting run loop")

			return
		case ev, ok := <-events:
			if !ok {
				return
			}

			switch ev {
			case control.PTTDown:
				cfg.beginTransmission(rt)
			case control.PTTUp:
				cfg.endTransmission(rt)
			case control.PTTToggle:
				if cfg.isBroadcasting(rt) {
					cfg.Log.Debug().Msg("Comm toggle: stopping transmission")
					cfg.endTransmission(rt)
				} else {
					cfg.Log.Debug().Msg("Comm toggle: starting transmission")
					cfg.beginTransmission(rt)
				}
			}
		}
	}
}
