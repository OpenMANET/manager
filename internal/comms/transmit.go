package comms

import (
	"context"
	"time"
)

// ─── Transmission state ───────────────────────────────────────────────────────

func (cfg *CommsConfig) isBroadcasting(rt *CommsRuntime) bool {
	return rt.broadcasting.Load()
}

func (cfg *CommsConfig) drainPlaybackBuffer(rt *CommsRuntime) {
	for _, pc := range rt.ports {
		buf := pc.playbackBuffer
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
// If the broadcast stream is nil or fails to start, rt.reopenBroadcast is
// called to rebuild it using the input device that was resolved at startup.
func (cfg *CommsConfig) beginTransmission(rt *CommsRuntime) {
	if rt.broadcasting.Load() {
		cfg.Log.Debug().Msg("PTTDown ignored; already broadcasting")

		return
	}

	// Half-duplex: refuse to transmit while the channel is actively receiving
	// audio from a remote peer.
	if cfg.isReceivingRemote(rt) {
		cfg.Log.Debug().Msg("PTTDown ignored; channel busy (receiving remote audio)")

		return
	}

	rt.broadcasting.Store(true)

	// Web mode: the browser provides its own audio I/O and UI feedback,
	// so skip beep tones, playback drain, and PortAudio stream management.
	if rt.webBridge != nil {
		cfg.Log.Debug().Msg("Begin web transmission")

		return
	}

	cfg.Log.Debug().Msg("Begin transmission: playing start tone and starting mic stream")
	cfg.drainPlaybackBuffer(rt)

	playedDeviceTone := false
	if rt.tonePlayer != nil {
		playedDeviceTone = rt.tonePlayer.PlayStartTone()
	}

	if !playedDeviceTone {
		for _, pc := range rt.ports {
			if pc.playbackBuffer != nil {
				pc.playbackBuffer <- rt.beepBufferStart
			}
		}
	}

	time.Sleep(200 * time.Millisecond)

	if rt.broadcastStream == nil {
		cfg.Log.Warn().Msg("Mic stream is nil; attempting to reopen")

		if rt.reopenBroadcast != nil {
			if err := rt.reopenBroadcast(); err != nil {
				cfg.Log.Error().Err(err).Msg("Failed to reopen mic stream")
				rt.broadcasting.Store(false)

				return
			}
		}
	}

	if rt.broadcastStream == nil {
		cfg.Log.Error().Msg("Mic stream still nil after reopen attempt")
		rt.broadcasting.Store(false)

		return
	}

	if err := rt.broadcastStream.Start(); err != nil {
		cfg.Log.Error().Err(err).Msg("Failed to start mic stream; attempting to reopen stream")

		if rt.reopenBroadcast != nil {
			if reErr := rt.reopenBroadcast(); reErr != nil {
				cfg.Log.Error().Err(reErr).Msg("Failed to reopen mic stream")
				rt.broadcasting.Store(false)

				return
			}
		}

		if err := rt.broadcastStream.Start(); err != nil {
			cfg.Log.Error().Err(err).Msg("Failed to start mic stream after reopen")
			rt.broadcasting.Store(false)

			return
		}
	}

	cfg.Log.Debug().Msg("Mic stream started")
}

// endTransmission stops the mic capture stream and plays the stop-tone.
func (cfg *CommsConfig) endTransmission(rt *CommsRuntime) {
	if !rt.broadcasting.Load() {
		cfg.Log.Debug().Msg("PTTUp ignored; mic already idle")

		return
	}

	// Web mode: skip PortAudio stream management and beep tones.
	if rt.webBridge != nil {
		cfg.Log.Debug().Msg("End web transmission")
		rt.broadcasting.Store(false)

		return
	}

	cfg.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")

	if rt.broadcastStream == nil {
		cfg.Log.Warn().Msg("Mic stream was nil during stop")
	} else if err := rt.broadcastStream.Stop(); err != nil {
		cfg.Log.Error().Err(err).Msg("stop mic")
	} else {
		cfg.Log.Debug().Msg("Mic stream stopped")
	}

	cfg.drainPlaybackBuffer(rt)

	playedDeviceTone := false
	if rt.tonePlayer != nil {
		playedDeviceTone = rt.tonePlayer.PlayStopTone()
	}

	if !playedDeviceTone {
		for _, pc := range rt.ports {
			if pc.playbackBuffer != nil {
				pc.playbackBuffer <- rt.beepBufferStop
			}
		}
	}

	rt.broadcasting.Store(false)
}

// Run is the main event loop. It starts a receiveLoop goroutine for every
// Receive-capable port and then blocks, dispatching PTT events until ctx is
// canceled.
func (cfg *CommsConfig) Run(ctx context.Context, rt *CommsRuntime, src EventSource) {
	for _, pc := range rt.ports {
		if pc.receiver != nil {
			go cfg.receiveLoop(ctx, pc, rt)
		}
	}

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
			case PTTDown:
				cfg.beginTransmission(rt)
			case PTTUp:
				cfg.endTransmission(rt)
			case PTTToggle:
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
