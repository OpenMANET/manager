//go:build !omd_omit_comms

package comms

import (
	"context"
	"time"
)

// ─── Transmission state ───────────────────────────────────────────────────────

func (cfg *CommsConfig) isBroadcasting(rt *CommsRuntime) bool {
	rt.recordMutex.Lock()
	defer rt.recordMutex.Unlock()

	return rt.broadcasting
}

func (cfg *CommsConfig) drainPlaybackBuffer(rt *CommsRuntime) {
	for {
		select {
		case <-rt.playbackBuffer:
		default:
			return
		}
	}
}

// beginTransmission starts the mic capture stream and plays the start-tone
// into the local speaker to signal the start of transmission.
//
// If the broadcast stream is nil or fails to start, rt.reopenBroadcast is
// called to rebuild it using the input device that was resolved at startup.
func (cfg *CommsConfig) beginTransmission(rt *CommsRuntime) {
	rt.recordMutex.Lock()
	if rt.broadcasting {
		cfg.Log.Debug().Msg("PTTDown ignored; already broadcasting")
		rt.recordMutex.Unlock()

		return
	}

	rt.broadcasting = true
	rt.recordMutex.Unlock()

	cfg.Log.Debug().Msg("Begin transmission: playing start tone and starting mic stream")
	cfg.drainPlaybackBuffer(rt)

	rt.playbackBuffer <- rt.beepBufferStart

	time.Sleep(200 * time.Millisecond)

	if rt.broadcastStream == nil {
		cfg.Log.Warn().Msg("Mic stream is nil; attempting to reopen")

		if rt.reopenBroadcast != nil {
			if err := rt.reopenBroadcast(); err != nil {
				cfg.Log.Error().Err(err).Msg("Failed to reopen mic stream")
				rt.recordMutex.Lock()
				rt.broadcasting = false
				rt.recordMutex.Unlock()

				return
			}
		}
	}

	if rt.broadcastStream == nil {
		cfg.Log.Error().Msg("Mic stream still nil after reopen attempt")
		rt.recordMutex.Lock()
		rt.broadcasting = false
		rt.recordMutex.Unlock()

		return
	}

	if err := rt.broadcastStream.Start(); err != nil {
		cfg.Log.Error().Err(err).Msg("Failed to start mic stream; attempting to reopen stream")

		if rt.reopenBroadcast != nil {
			if reErr := rt.reopenBroadcast(); reErr != nil {
				cfg.Log.Error().Err(reErr).Msg("Failed to reopen mic stream")
				rt.recordMutex.Lock()
				rt.broadcasting = false
				rt.recordMutex.Unlock()

				return
			}
		}

		if err := rt.broadcastStream.Start(); err != nil {
			cfg.Log.Error().Err(err).Msg("Failed to start mic stream after reopen")
			rt.recordMutex.Lock()
			rt.broadcasting = false
			rt.recordMutex.Unlock()

			return
		}
	}

	cfg.Log.Debug().Msg("Mic stream started")
}

// endTransmission stops the mic capture stream and plays the stop-tone.
func (cfg *CommsConfig) endTransmission(rt *CommsRuntime) {
	rt.recordMutex.Lock()
	if !rt.broadcasting {
		cfg.Log.Debug().Msg("PTTUp ignored; mic already idle")
		rt.recordMutex.Unlock()

		return
	}
	rt.recordMutex.Unlock()

	cfg.Log.Debug().Msg("End transmission: stopping mic stream and playing stop tone")

	if rt.broadcastStream == nil {
		cfg.Log.Warn().Msg("Mic stream was nil during stop")
	} else if err := rt.broadcastStream.Stop(); err != nil {
		cfg.Log.Error().Err(err).Msg("stop mic")
	} else {
		cfg.Log.Debug().Msg("Mic stream stopped")
	}

	cfg.drainPlaybackBuffer(rt)

	rt.playbackBuffer <- rt.beepBufferStop

	rt.recordMutex.Lock()
	rt.broadcasting = false
	rt.recordMutex.Unlock()
}

// Run is the main event loop. It starts the receive goroutine and the event
// source and blocks until ctx is canceled.
func (cfg *CommsConfig) Run(ctx context.Context, rt *CommsRuntime, src EventSource) {
	go cfg.receiveLoop(ctx, rt)

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
