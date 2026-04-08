package comms

import (
	"context"
	"fmt"
	"math"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audio"
	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
)

// startHardwareAudio constructs an audio.Init bound to cfg/rt, builds the
// per-port playback slots, and starts PortAudio. Returns the cleanup
// function that StartHardware produced (which the caller defers).
//
// Extracted from Start so that Start's cognitive complexity stays under the
// linter threshold and the audio sub-package wiring lives next to its other
// per-port helpers.
func (cfg *CommsConfig) startHardwareAudio(rt *CommsRuntime) (cleanup func(), err error) {
	audioInit := &audio.Init{
		Deps: audio.Deps{
			Log:               cfg.Log,
			Trace:             cfg.Trace,
			Debug:             cfg.Debug,
			MicGain:           cfg.MicGain,
			CaptureLatencyMs:  cfg.CaptureLatencyMs,
			PlaybackLatencyMs: cfg.PlaybackLatencyMs,
			InputDeviceSpec:   cfg.BluetoothInputDevice,
			OutputDeviceSpec:  cfg.BluetoothOutputDevice,
			Encoder:           rt.Encoder,
			Send:              func(payload []byte) { cfg.sendToAllPorts(rt, payload) },
			Tap:               &rt.BroadcastTap,
		},
	}

	// beepChannelDepth is the one-shot side channel that the TX path
	// (transmit.go beginTransmission/endTransmission) uses to inject
	// start/stop beep tones. The PortAudio output callback drains it
	// before falling through to playoutOneFrame.
	const beepChannelDepth = 4

	slots := make([]audio.PortSlot, 0, len(rt.Ports))

	for i := range rt.Ports {
		pc := rt.Ports[i]
		if pc.Receiver == nil {
			continue
		}

		pc.PlaybackBuffer = make(chan []int16, beepChannelDepth)

		pcRef := pc

		slots = append(slots, audio.PortSlot{
			HasReceiver:  true,
			Port:         pc.cfg.Port,
			BeepBuf:      pc.PlaybackBuffer,
			SetStream:    func(s device.AudioStream) { pcRef.PlaybackStream = s },
			PlayoutFrame: func(out []int16) { cfg.playoutOneFrame(pcRef, rt, pcRef.Jitter, out) },
		})
	}

	broadcast, cleanup, hwErr := audioInit.StartHardware(slots)
	if hwErr != nil {
		return nil, hwErr
	}

	rt.BroadcastStream = broadcast
	rt.PlaybackOutputLatency = audioInit.PlaybackOutputLatency
	rt.ReopenBroadcast = func() error {
		be, reopenErr := audioInit.ReopenBroadcast()
		if reopenErr != nil {
			return reopenErr
		}

		rt.BroadcastStream = be

		return nil
	}

	return cleanup, nil
}

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
	beepStart := make([]int16, audiopool.FrameSize)
	beepStop := make([]int16, audiopool.FrameSize)

	const beepAmp = 0.2 * 32767

	for i := range beepStart {
		beepStart[i] = int16(math.Sin(2*math.Pi*1000*float64(i)/float64(audiopool.SampleRate)) * beepAmp)
		beepStop[i] = int16(math.Sin(2*math.Pi*600*float64(i)/float64(audiopool.SampleRate)) * beepAmp)
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

	// Wrap the static config and the freshly built runtime in a *Service
	// so the HTTP handlers and other consumers can resolve them via
	// SetDefault / Default. The Service is published just before the event
	// source is built so anything that depends on the live runtime can
	// observe it as soon as Run starts.
	svc := &Service{Cfg: cfg, Rt: rt}

	defer func() {
		for _, pc := range rt.Ports {
			if pc.Receiver != nil {
				_ = pc.Receiver.Close()
			}

			if pc.RTPSess != nil {
				if s, ok := pc.RTPSess.(*rtp.Session); ok {
					_ = s.Close()
				}
			}
		}

		SetDefault(nil)
	}()

	SetDefault(svc)

	// ── event source ───────────────────────────────────────────────────────
	src, srcErr := cfg.buildEventSource(rt)
	if srcErr != nil {
		return fmt.Errorf("comms: failed to build event source: %w", srcErr)
	}

	// ── audio I/O ─────────────────────────────────────────────────────────
	if cfg.ControlSource == controlSourceWeb {
		// Web mode: skip PortAudio entirely; the browser provides audio I/O.
		rt.WebBridge = webaudio.NewBridge(cfg.Log, func(payload []byte) {
			cfg.sendToAllPorts(rt, payload)
		})
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
