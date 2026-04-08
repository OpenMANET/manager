package audio

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gordonklaus/portaudio"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
	"github.com/openmanet/openmanetd/internal/comms/device"
)

// Init holds the dependency bundle for opening hardware audio streams.
// The parent comms package constructs one Init per Start() invocation and
// discards it once StartHardware returns. Init carries the live streams it
// opened so its cleanup function can stop and close them in the same
// order they were opened.
type Init struct {
	currentBroadcast *BroadcastEncoder
	inputDevice      *portaudio.DeviceInfo
	playbackStreams  []device.AudioStream
	Deps
	PlaybackOutputLatency time.Duration
}

// BuildAudio resolves PortAudio devices, opens a dedicated playback stream
// for every receive-capable PortSlot, and opens the shared broadcast capture
// stream. Per-port playback streams are installed via PortSlot.SetStream
// after this call returns.
func (in *Init) BuildAudio(slots []PortSlot) (broadcast *BroadcastEncoder, inDev *portaudio.DeviceInfo, err error) {
	outDev, err := device.ResolveAudio(in.OutputDeviceSpec, false)
	if err != nil {
		return nil, nil, err
	}

	inDev, err = device.ResolveAudio(in.InputDeviceSpec, true)
	if err != nil {
		return nil, nil, err
	}

	in.Log.Info().Msgf("comms: audio in=%s out=%s", inDev.Name, outDev.Name)

	// Suggest a playback device buffer depth to PortAudio. This is the only
	// layer of buffering that protects against playback-side OS scheduling
	// stalls — the Go-side jitter buffer sits upstream of the DAC and cannot
	// help once the audio thread is preempted. The callback chunk size stays
	// at audiopool.FrameSize so playoutOneFrame is unchanged.
	//
	// Floor at outDev.DefaultHighOutputLatency: some hardware reports a
	// "high" latency that is essentially the same as one callback period
	// (e.g. 21 ms on the OpenVLM USB audio class device, where the next
	// useful step up is the configured value); other hardware reports a
	// genuinely higher value, in which case we honor the device hint
	// rather than overriding it downward. The host API may still clamp
	// the suggestion — the actual granted latency is logged below.
	playbackLatency := max(
		time.Duration(in.PlaybackLatencyMs)*time.Millisecond,
		outDev.DefaultHighOutputLatency,
	)

	playbackParams := portaudio.StreamParameters{
		Output: portaudio.StreamDeviceParameters{
			Device:   outDev,
			Channels: audiopool.Channels,
			Latency:  playbackLatency,
		},
		SampleRate:      float64(audiopool.SampleRate),
		FramesPerBuffer: audiopool.FrameSize,
	}

	opened := make([]device.AudioStream, 0, len(slots))

	closeOpened := func() {
		for _, s := range opened {
			if s != nil {
				_ = s.Close()
			}
		}
	}

	for i := range slots {
		slot := slots[i]
		if !slot.HasReceiver {
			continue
		}

		beepBuf := slot.BeepBuf
		playoutFrame := slot.PlayoutFrame

		// Open the playback stream with an int16 callback so PortAudio
		// delivers samples in the native codec format. The
		// gordonklaus/portaudio binding chooses the C sample format
		// (paInt16) from the callback signature via reflection.
		rawPlayback, openErr := portaudio.OpenStream(playbackParams, func(_, out []int16) {
			// Beep injection: TX start/stop tones preempt one frame of
			// jitter-buffered audio. The select is non-blocking so a
			// missing beep falls straight through to playoutFrame.
			select {
			case data := <-beepBuf:
				copy(out, data)

				return
			default:
			}

			playoutFrame(out)
		})
		if openErr != nil {
			closeOpened()

			return nil, nil, fmt.Errorf("open playback stream for port %d: %w", slot.Port, openErr)
		}

		// Log the actual output latency the host API granted. This may
		// differ from playbackLatency if the host API clamped the
		// suggestion. Deploy-time verification uses this to confirm
		// whether the configured comms.playbackLatencyMs took effect or
		// fell back to the device's idea of "high latency". The
		// device_high field is the floor we used (so it is obvious when
		// the configured value was overridden by a higher device hint).
		//
		// The actual_output_latency is also captured into
		// in.PlaybackOutputLatency so the parent's TX path can hold
		// the mic stream closed until the start-tone beep has fully
		// emerged from the speaker (see beginTransmission).
		if info := rawPlayback.Info(); info != nil {
			in.PlaybackOutputLatency = max(in.PlaybackOutputLatency, info.OutputLatency)

			in.Log.Debug().
				Int("configured_latency_ms", in.PlaybackLatencyMs).
				Dur("device_high_latency", outDev.DefaultHighOutputLatency).
				Dur("requested_latency", playbackLatency).
				Dur("actual_output_latency", info.OutputLatency).
				Int("port", slot.Port).
				Msg("comms: playback stream opened")
		}

		stream := device.NewPortAudioStream(rawPlayback)
		slot.SetStream(stream)
		opened = append(opened, stream)
		in.playbackStreams = append(in.playbackStreams, stream)
	}

	broadcast, err = in.OpenBroadcastStream(inDev)
	if err != nil {
		closeOpened()

		return nil, nil, err
	}

	return broadcast, inDev, nil
}

// OpenBroadcastStream creates a PortAudio capture stream that encodes mic
// audio via Opus and transmits it as RTP via Deps.Send. The actual encode
// and RTP send run on a dedicated goroutine inside BroadcastEncoder, NOT on
// the PortAudio audio callback thread, so encoder spikes / GC pauses / UDP
// backpressure cannot starve the audio thread and cause ADC overruns at the
// device.
func (in *Init) OpenBroadcastStream(inDev *portaudio.DeviceInfo) (*BroadcastEncoder, error) {
	// Phase 3 unified discovery: report the current CM108 descriptor count
	// so the broadcast stream open has the same observable device state as
	// the HID (PTT) side. PortAudio is not enumerable from /sys, so the
	// chosen PortAudio device is still supplied by inDev — the walk here is
	// informational and shares the same code path as openvlmSource. Gated
	// behind Debug so production reopens (e.g. after a stale handle on
	// PTTDown) skip the syscall and the descriptor-slice allocation.
	if in.Debug {
		if descs, dErr := device.DiscoverCM108(os.DirFS("/sys"), nil); dErr == nil {
			in.Log.Debug().
				Int("cm108_count", len(descs)).
				Str("pa_device", inDev.Name).
				Msg("comms: unified CM108 descriptor scan at broadcast open")
		}
	}

	// Suggest a capture device buffer depth to PortAudio. Symmetric to the
	// playback stream in BuildAudio. Floor at inDev.DefaultHighInputLatency
	// so we never undercut the host API's recommendation. The host API may
	// still clamp the suggestion — the actual granted latency is logged
	// below.
	captureLatency := max(
		time.Duration(in.CaptureLatencyMs)*time.Millisecond,
		inDev.DefaultHighInputLatency,
	)

	inParams := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   inDev,
			Channels: audiopool.Channels,
			Latency:  captureLatency,
		},
		SampleRate:      float64(audiopool.SampleRate),
		FramesPerBuffer: audiopool.FrameSize,
	}

	be, err := NewBroadcastEncoder(in.Deps, inParams)
	if err != nil {
		return nil, err
	}

	// Log the actual input latency the host API granted. Mirrors the
	// playback stream open log so deploy-time verification has the same
	// fields on both sides. encode_chan_depth makes the new
	// goroutine-based architecture self-documenting on first deploy.
	if info := be.s.Info(); info != nil {
		in.Log.Debug().
			Int("configured_latency_ms", in.CaptureLatencyMs).
			Dur("device_high_latency", inDev.DefaultHighInputLatency).
			Dur("requested_latency", captureLatency).
			Dur("actual_input_latency", info.InputLatency).
			Int("encode_chan_depth", broadcastEncoderChanDepth).
			Msg("comms: broadcast stream opened")
	}

	return be, nil
}

// ReopenBroadcast closes the prior broadcast encoder (if any) and opens a
// new one against the input device captured by the previous successful
// StartHardware call. Used by the parent's PTTDown recovery path.
func (in *Init) ReopenBroadcast() (*BroadcastEncoder, error) {
	if in.inputDevice == nil {
		return nil, errors.New("input device is not set")
	}

	if in.currentBroadcast != nil {
		_ = in.currentBroadcast.Close()
		in.currentBroadcast = nil
	}

	be, err := in.OpenBroadcastStream(in.inputDevice)
	if err != nil {
		return nil, err
	}

	in.currentBroadcast = be

	return be, nil
}

// StartHardware initializes PortAudio, opens broadcast and per-port
// playback streams, starts the playback streams, and returns a cleanup
// function that stops and closes them. The returned BroadcastEncoder is
// the same instance the parent runtime should treat as its
// CommsRuntime.BroadcastStream.
func (in *Init) StartHardware(slots []PortSlot) (broadcast *BroadcastEncoder, cleanup func(), err error) {
	device.SilenceALSAProbeNoise()

	paErr := portaudio.Initialize()

	device.RestoreALSAErrorHandler()

	if paErr != nil {
		return nil, nil, fmt.Errorf("comms: failed to initialize PortAudio: %w", paErr)
	}

	be, inDev, audioErr := in.BuildAudio(slots)
	if audioErr != nil {
		_ = portaudio.Terminate()

		return nil, nil, fmt.Errorf("comms: failed to build audio streams: %w", audioErr)
	}

	in.currentBroadcast = be
	in.inputDevice = inDev

	// Start the per-port playback streams that BuildAudio installed. On
	// any failure, stop and close every stream we already started before
	// propagating the error.
	for idx, stream := range in.playbackStreams {
		if startErr := stream.Start(); startErr != nil {
			for j := range idx {
				_ = in.playbackStreams[j].Stop()
			}

			for _, s := range in.playbackStreams {
				_ = s.Close()
			}

			in.playbackStreams = nil

			_ = be.Close()
			in.currentBroadcast = nil

			_ = portaudio.Terminate()

			return nil, nil, fmt.Errorf("comms: failed to start playback stream: %w", startErr)
		}
	}

	cleanup = func() {
		for _, s := range in.playbackStreams {
			_ = s.Stop()
			_ = s.Close()
		}

		in.playbackStreams = nil

		if in.currentBroadcast != nil {
			_ = in.currentBroadcast.Close()
			in.currentBroadcast = nil
		}

		_ = portaudio.Terminate()
	}

	return be, cleanup, nil
}
