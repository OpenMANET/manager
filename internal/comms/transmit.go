package comms

import (
	"context"
	"time"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/control"
)

// defaultPttStartDelayMs is the mic-warmup floor applied when
// CommsConfig.PttStartDelayMs is left unset (== 0). 50 ms is long enough
// to let USB audio class capture devices commit their first DMA cycle
// before the encoder starts pulling frames. The previous hard-coded
// 200 ms was a conservative guess. The actual time beginTransmission
// sleeps may be larger than this value when the playback output latency
// requires a longer beep settle window — see transmitSettleWait. To
// skip the wait entirely (and accept the start-tone leak risk on
// hardware with speaker→mic coupling) set PttStartDelayMs to a
// negative value.
const defaultPttStartDelayMs = 50

// frameDuration is the wall-clock length of one Opus frame at the
// capture/playback sample rate (20 ms). The start/stop beeps are
// exactly one frame long.
const frameDuration = time.Duration(audiopool.FrameSize) * time.Second /
	time.Duration(audiopool.SampleRate)

// beepSettleMargin is the extra slack added on top of the playback
// output latency and the beep frame duration when waiting for the
// start tone to fully clear the speaker before the mic capture stream
// goes live. Accounts for playback-callback scheduling jitter and
// room reverb tail.
const beepSettleMargin = 20 * time.Millisecond

// pttStartDelay returns the configured mic-warmup duration. A negative
// PttStartDelayMs means "skip the wait"; a zero or unset value falls
// back to defaultPttStartDelayMs; positive values are taken as-is.
// Callers should generally use transmitSettleWait, which also accounts
// for the playback output latency required to keep the start beep out
// of the transmitted stream.
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

// transmitSettleWait returns the duration beginTransmission should
// sleep between queueing the start-tone beep and calling
// BroadcastStream.Start(). It is the greater of:
//
//   - the configured mic-warmup delay (pttStartDelay), and
//   - the beep's physical emission window: the actual playback output
//     latency reported by PortAudio plus one frame (the beep duration)
//     plus a small margin for scheduling jitter and room reverb.
//
// Without the second term the mic stream goes live before the beep has
// physically emerged from the speaker; any acoustic or device sidetone
// path from speaker → mic then captures the beep and it gets encoded
// into the transmitted RTP stream so the remote side hears it.
//
// When PttStartDelayMs is negative the caller has explicitly opted out
// of the settle wait, so this returns 0 even if the beep has not yet
// cleared the speaker.
func (cfg *CommsConfig) transmitSettleWait(rt *CommsRuntime) time.Duration {
	if cfg.PttStartDelayMs < 0 {
		return 0
	}

	beepSettle := rt.PlaybackOutputLatency + frameDuration + beepSettleMargin

	return max(cfg.pttStartDelay(), beepSettle)
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

	// Settle window before the mic capture stream starts. Holds the mic
	// closed until the start-tone beep has fully emerged from the
	// speaker — otherwise an acoustic (or device sidetone) path from
	// speaker → mic captures the beep and the remote side hears it.
	// The wait also covers hardware that warms its capture path slowly.
	// Sized by transmitSettleWait from the playback output latency and
	// CommsConfig.PttStartDelayMs.
	if d := cfg.transmitSettleWait(rt); d > 0 {
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
