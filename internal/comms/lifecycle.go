package comms

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audio"
	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
)

// startHardwareAudio constructs an audio.Init bound to cfg/rt, builds the
// per-port playback slots, and starts the malgo streams. Returns the cleanup
// function that StartHardware produced (which the caller defers).
//
// Extracted from Start so that Start's cognitive complexity stays under the
// linter threshold and the audio sub-package wiring lives next to its other
// per-port helpers.
func (cfg *CommsConfig) startHardwareAudio(rt *CommsRuntime) (cleanup func(), err error) {
	audioInit := &audio.Init{
		Deps: audio.Deps{
			Log:                    cfg.Log,
			Trace:                  cfg.Trace,
			Debug:                  cfg.Debug,
			MicGain:                cfg.MicGain,
			CaptureLatencyMs:       cfg.CaptureLatencyMs,
			PlaybackLatencyMs:      cfg.PlaybackLatencyMs,
			CaptureFramesPerBuffer: cfg.CaptureFramesPerBuffer,
			InputDeviceSpec:        cfg.BluetoothInputDevice,
			OutputDeviceSpec:       cfg.BluetoothOutputDevice,
			Encoder:                rt.Encoder,
			Send:                   func(payload []byte) { cfg.sendToAllPorts(rt, payload) },
			Tap:                    &rt.BroadcastTap,
		},
	}

	// beepChannelDepth is the one-shot side channel that the TX path
	// (transmit.go beginTransmission/endTransmission) uses to inject
	// start/stop beep tones. The malgo playback callback drains it
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

	rt.SetBroadcast(broadcast)
	rt.PlaybackOutputLatency = audioInit.PlaybackOutputLatency

	return cleanup, nil
}

const (
	// audioInitAttempts bounds the startup hardware-audio init retries.
	// The OpenVLM dongle can transiently fail its first dmix slave start
	// while USB enumeration settles at boot (EPIPE from snd_pcm_start);
	// a couple of spaced retries absorbs that without delaying startup
	// noticeably.
	audioInitAttempts = 3

	// defaultAudioInitRetryDelay is the production wait between startup
	// attempts; applyDefaults installs it when the field is zero.
	defaultAudioInitRetryDelay = 750 * time.Millisecond
)

// initAudioIO wires up the local audio path for cfg.ControlSource. In web
// mode it constructs a webaudio bridge; in non-web modes (openvlm, nanoptt,
// roip) it tries to open the malgo capture/playback streams, retrying up to
// audioInitAttempts times with cfg.audioInitRetryDelay between attempts.
//
// A failure to bring up local audio is non-fatal: the comms subsystem stays
// alive so RTP relay between mesh peers continues, and the WebUI's
// per-channel RX/TX toggles still take effect. The TX/RX hot paths already
// guard against the resulting nil BroadcastStream / PlaybackStream. The Run
// loop's audio recovery ticker keeps re-attempting init in the background.
//
// Returns the malgo cleanup function (nil when there is nothing to clean
// up) so Start can defer it.
func (cfg *CommsConfig) initAudioIO(ctx context.Context, rt *CommsRuntime) func() {
	if cfg.ControlSource == controlSourceWeb {
		// Web mode: skip the malgo pipeline entirely; the browser provides audio I/O.
		rt.WebBridge = webaudio.NewBridge(cfg.Log, func(payload []byte) {
			cfg.sendToAllPorts(rt, payload)
		})

		return nil
	}

	startHA := cfg.startHardwareAudioFn
	if startHA == nil {
		startHA = cfg.startHardwareAudio
	}

	var lastErr error

	for attempt := 1; attempt <= audioInitAttempts; attempt++ {
		cleanup, hwErr := startHA(rt)
		if hwErr == nil {
			if attempt > 1 {
				cfg.Log.Info().Int("attempt", attempt).Msg("comms: hardware audio init succeeded after retry")
			}

			return cleanup
		}

		lastErr = hwErr
		cfg.Log.Warn().Err(hwErr).
			Int("attempt", attempt).
			Int("max_attempts", audioInitAttempts).
			Msg("comms: hardware audio init attempt failed")

		if attempt == audioInitAttempts {
			break
		}

		if !sleepCtx(ctx, cfg.audioInitRetryDelay) {
			return nil
		}
	}

	cfg.Log.Error().Err(lastErr).
		Str("alsa_card", os.Getenv("ALSA_CARD")).
		Msg("comms: hardware audio init failed; continuing without local mic/speaker")

	return nil
}

// sleepCtx waits for d or until ctx is canceled. Returns false when the
// context ended the wait (or was already canceled).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
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
	// into the malgo int16 playback callback without an extra conversion.
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

	// ── FEC adapter ────────────────────────────────────────────────────────
	// Construct after ports are populated so the adapter's prev slice is
	// sized correctly. The constructor also makes an initial
	// SetPacketLossPerc(floor) call to scrub any stale level from a prior
	// enable cycle. The Run goroutine is launched inside cfg.Run()
	// alongside halfDuplexDecayLoop.
	rt.FECAdapter = NewFECAdapter(rt, enc, cfg.PacketLossPerc, cfg.Log)

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
	if cleanup := cfg.initAudioIO(ctx, rt); cleanup != nil {
		defer cleanup()
	}

	// ── run loop ───────────────────────────────────────────────────────────
	cfg.Run(ctx, rt, src)

	cfg.Log.Info().Msg("comms: subsystem stopped")

	return nil
}
