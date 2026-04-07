package comms

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// buildAudio resolves PortAudio devices, opens a dedicated playback stream for
// every Receive-capable port (storing it in PortChannel.PlaybackStream), and
// opens the shared broadcast capture stream. Per-port playback streams are
// accessible via rt.Ports after this call returns.
func (cfg *CommsConfig) buildAudio(rt *CommsRuntime) (
	broadcast AudioStream,
	inDev *portaudio.DeviceInfo,
	err error,
) {
	outDev, err := device.ResolveAudio(cfg.BluetoothOutputDevice, false)
	if err != nil {
		return nil, nil, err
	}

	inDev, err = device.ResolveAudio(cfg.BluetoothInputDevice, true)
	if err != nil {
		return nil, nil, err
	}

	cfg.Log.Info().Msgf("comms: audio in=%s out=%s", inDev.Name, outDev.Name)

	// playbackBuffer is a small one-shot side channel used by the TX path
	// (transmit.go) to inject start/stop beep tones into the local speaker.
	// It is no longer the carrier for decoded RTP audio — that flows through
	// pc.Jitter directly into the PortAudio output callback via
	// playoutOneFrame, eliminating the producer/consumer clock mismatch that
	// previously caused stutter.
	const beepChannelDepth = 4

	// Suggest a playback device buffer depth to PortAudio. This is the only
	// layer of buffering that protects against playback-side OS scheduling
	// stalls — the Go-side jitter buffer sits upstream of the DAC and cannot
	// help once the audio thread is preempted. The callback chunk size stays
	// at frameSize so playoutOneFrame is unchanged.
	//
	// Floor at outDev.DefaultHighOutputLatency: some hardware reports a
	// "high" latency that is essentially the same as one callback period
	// (e.g. 21 ms on the OpenVLM USB audio class device, where the next
	// useful step up is the configured value); other hardware reports a
	// genuinely higher value, in which case we honor the device hint
	// rather than overriding it downward. The host API may still clamp
	// the suggestion — the actual granted latency is logged below.
	playbackLatency := time.Duration(cfg.PlaybackLatencyMs) * time.Millisecond
	if playbackLatency < outDev.DefaultHighOutputLatency {
		playbackLatency = outDev.DefaultHighOutputLatency
	}

	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outDev,
			Channels: channels,
			Latency:  playbackLatency,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	// Open a dedicated playback stream for every port that has an open
	// receiver socket, regardless of its initial receiveEnabled state.
	// This ensures that EnableTalkGroupReceive can activate any port at
	// runtime without needing a restart.
	for _, pc := range rt.Ports {
		if pc.Receiver == nil {
			continue
		}

		pc.PlaybackBuffer = make(chan []int16, beepChannelDepth)

		pcRef := pc // capture for callback closure

		// Phase 5: open the playback stream with an int16 callback so
		// PortAudio delivers samples in the native codec format. The
		// gordonklaus/portaudio binding chooses the C sample format
		// (paInt16) from the callback signature via reflection.
		rawPlayback, openErr := portaudio.OpenStream(playbackParams, func(_, out []int16) {
			// Beep injection: TX start/stop tones preempt one frame of
			// jitter-buffered audio. The select is non-blocking so a
			// missing beep falls straight through to playoutOneFrame.
			select {
			case data := <-pcRef.PlaybackBuffer:
				copy(out, data)

				return
			default:
			}

			cfg.playoutOneFrame(pcRef, rt, pcRef.Jitter, out)
		})
		if openErr != nil {
			// Close already-opened per-port streams before propagating error.
			for _, built := range rt.Ports {
				if built.PlaybackStream != nil {
					_ = built.PlaybackStream.Close()
					built.PlaybackStream = nil
				}
			}

			return nil, nil, fmt.Errorf("open playback stream for port %d: %w", pc.cfg.Port, openErr)
		}

		// Log the actual output latency the host API granted. This may
		// differ from playbackLatency if the host API clamped the
		// suggestion. Deploy-time verification uses this to confirm
		// whether the configured comms.playbackLatencyMs took effect or
		// fell back to the device's idea of "high latency". The
		// device_high field is the floor we used (so it is obvious when
		// the configured value was overridden by a higher device hint).
		if info := rawPlayback.Info(); info != nil {
			cfg.Log.Debug().
				Int("configured_latency_ms", cfg.PlaybackLatencyMs).
				Dur("device_high_latency", outDev.DefaultHighOutputLatency).
				Dur("requested_latency", playbackLatency).
				Dur("actual_output_latency", info.OutputLatency).
				Int("port", pc.cfg.Port).
				Msg("comms: playback stream opened")
		}

		pc.PlaybackStream = &portaudioStream{rawPlayback}
	}

	broadcast, err = cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		for _, pc := range rt.Ports {
			if pc.PlaybackStream != nil {
				_ = pc.PlaybackStream.Close()
				pc.PlaybackStream = nil
			}
		}

		return nil, nil, err
	}

	return broadcast, inDev, nil
}

// openBroadcastStreamOn creates a PortAudio capture stream that encodes mic
// audio via Opus and transmits it as RTP to all send-enabled ports via
// sendToAllPorts. The actual encode and RTP send run on a dedicated goroutine
// inside broadcastEncoder, NOT on the PortAudio audio callback thread, so
// encoder spikes / GC pauses / UDP backpressure cannot starve the audio thread
// and cause ADC overruns at the device.
func (cfg *CommsConfig) openBroadcastStreamOn(inDev *portaudio.DeviceInfo, rt *CommsRuntime) (AudioStream, error) {
	// Phase 3 unified discovery: report the current CM108 descriptor count
	// so the broadcast stream open has the same observable device state as
	// the HID (PTT) side. PortAudio is not enumerable from /sys, so the
	// chosen PortAudio device is still supplied by inDev — the walk here is
	// informational and shares the same code path as openvlmSource. Gated
	// behind Debug so production reopens (e.g. after a stale handle on
	// PTTDown) skip the syscall and the descriptor-slice allocation.
	if cfg.Debug {
		if descs, dErr := device.DiscoverCM108(os.DirFS("/sys"), nil); dErr == nil {
			cfg.Log.Debug().
				Int("cm108_count", len(descs)).
				Str("pa_device", inDev.Name).
				Msg("comms: unified CM108 descriptor scan at broadcast open")
		}
	}

	// Suggest a capture device buffer depth to PortAudio. Symmetric to the
	// playback stream in buildAudio. Floor at inDev.DefaultHighInputLatency
	// so we never undercut the host API's recommendation. The host API may
	// still clamp the suggestion — the actual granted latency is logged
	// below.
	captureLatency := time.Duration(cfg.CaptureLatencyMs) * time.Millisecond
	if captureLatency < inDev.DefaultHighInputLatency {
		captureLatency = inDev.DefaultHighInputLatency
	}

	inParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inDev,
			Channels: channels,
			Latency:  captureLatency,
		},
		SampleRate:      float64(sampleRate),
		FramesPerBuffer: frameSize,
	}

	be, err := newBroadcastEncoder(cfg, rt, inParams)
	if err != nil {
		return nil, err
	}

	// Log the actual input latency the host API granted. Mirrors the
	// playback stream open log so deploy-time verification has the same
	// fields on both sides. encode_chan_depth makes the new goroutine-based
	// architecture self-documenting on first deploy.
	if info := be.s.Info(); info != nil {
		cfg.Log.Debug().
			Int("configured_latency_ms", cfg.CaptureLatencyMs).
			Dur("device_high_latency", inDev.DefaultHighInputLatency).
			Dur("requested_latency", captureLatency).
			Dur("actual_input_latency", info.InputLatency).
			Int("encode_chan_depth", broadcastEncoderChanDepth).
			Msg("comms: broadcast stream opened")
	}

	return be, nil
}

// reopenBroadcastStream closes the current broadcast stream and opens a new one.
func (cfg *CommsConfig) reopenBroadcastStream(rt *CommsRuntime, inDev *portaudio.DeviceInfo) error {
	if inDev == nil {
		return errors.New("input device is not set")
	}

	if rt.BroadcastStream != nil {
		_ = rt.BroadcastStream.Close()
		rt.BroadcastStream = nil
	}

	stream, err := cfg.openBroadcastStreamOn(inDev, rt)
	if err != nil {
		return err
	}

	rt.BroadcastStream = stream

	return nil
}

// startHardwareAudio initializes PortAudio, opens broadcast and playback
// streams, and returns a cleanup function that stops and closes them.
func (cfg *CommsConfig) startHardwareAudio(rt *CommsRuntime) (func(), error) {
	silenceALSAProbeNoise()

	err := portaudio.Initialize()

	restoreALSAErrorHandler()

	if err != nil {
		return nil, fmt.Errorf("comms: failed to initialize PortAudio: %w", err)
	}

	broadcastStream, inDev, audioErr := cfg.buildAudio(rt)
	if audioErr != nil {
		_ = portaudio.Terminate()

		return nil, fmt.Errorf("comms: failed to build audio streams: %w", audioErr)
	}

	rt.BroadcastStream = broadcastStream
	rt.ReopenBroadcast = func() error { return cfg.reopenBroadcastStream(rt, inDev) }

	for _, pc := range rt.Ports {
		if pc.PlaybackStream != nil {
			if startErr := pc.PlaybackStream.Start(); startErr != nil {
				_ = broadcastStream.Close()
				_ = portaudio.Terminate()

				return nil, fmt.Errorf("comms: failed to start playback stream: %w", startErr)
			}
		}
	}

	return func() {
		for _, pc := range rt.Ports {
			if pc.PlaybackStream != nil {
				_ = pc.PlaybackStream.Stop()
				_ = pc.PlaybackStream.Close()
			}
		}

		_ = broadcastStream.Close()
		_ = portaudio.Terminate()
	}, nil
}
