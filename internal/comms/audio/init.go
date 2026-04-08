package audio

import (
	"fmt"
	"os"
	"time"

	"github.com/gen2brain/malgo"

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
	malgoCtx         *malgo.AllocatedContext
	playbackStreams  []device.AudioStream
	Deps
	PlaybackOutputLatency time.Duration
}

// BuildAudio resolves the configured capture / playback devices, opens a
// dedicated malgo playback device for every receive-capable PortSlot, and
// opens the shared broadcast capture device. Per-port playback streams
// are installed via PortSlot.SetStream after this call returns.
//
// All devices share the malgo context stored on Init.malgoCtx, which is
// initialized in StartHardware and freed in the cleanup closure.
func (in *Init) BuildAudio(slots []PortSlot) (*BroadcastEncoder, device.AudioDeviceInfo, error) {
	var zeroIn device.AudioDeviceInfo

	if in.malgoCtx == nil {
		return nil, zeroIn, fmt.Errorf("audio.BuildAudio: malgo context is not initialized")
	}

	outDev, err := device.ResolveAudio(in.malgoCtx, in.OutputDeviceSpec, false)
	if err != nil {
		return nil, zeroIn, err
	}

	inDev, err := device.ResolveAudio(in.malgoCtx, in.InputDeviceSpec, true)
	if err != nil {
		return nil, zeroIn, err
	}

	in.Log.Info().Msgf("comms: audio in=%s out=%s", inDev.Name, outDev.Name)

	playbackPeriod := uint32(computePlaybackPeriodFrames(in.PlaybackLatencyMs))

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

		rawPlayback, openErr := openMalgoPlayback(
			in.malgoCtx,
			outDev,
			uint32(audiopool.SampleRate),
			uint32(audiopool.Channels),
			playbackPeriod,
			beepBuf,
			playoutFrame,
		)
		if openErr != nil {
			closeOpened()

			return nil, zeroIn, fmt.Errorf("open playback stream for port %d: %w", slot.Port, openErr)
		}

		// Record the period-derived playback latency so the TX path's
		// beep-emergence wait (transmitSettleWait) has something to
		// anchor on. miniaudio does not expose a runtime "actual" output
		// latency the way PortAudio did, so the diagnostic is the
		// configured period duration — a conservative lower bound that
		// is generally correct for ALSA.
		period := periodFramesToDuration(playbackPeriod, uint32(audiopool.SampleRate))
		in.PlaybackOutputLatency = max(in.PlaybackOutputLatency, period)

		in.Log.Info().
			Int("configured_latency_ms", in.PlaybackLatencyMs).
			Int("period_frames", int(playbackPeriod)).
			Dur("period_duration", period).
			Int("port", slot.Port).
			Str("device", outDev.Name).
			Msg("comms: playback stream opened")

		stream := device.NewMalgoStream(rawPlayback)
		slot.SetStream(stream)
		opened = append(opened, stream)
		in.playbackStreams = append(in.playbackStreams, stream)
	}

	broadcast, err := in.OpenBroadcastStream(inDev)
	if err != nil {
		closeOpened()

		return nil, zeroIn, err
	}

	return broadcast, inDev, nil
}

// OpenBroadcastStream creates a malgo capture device that feeds the Opus
// encoder and RTP send pipeline via BroadcastEncoder. The actual encode
// and RTP send run on a dedicated goroutine inside BroadcastEncoder, NOT
// on the miniaudio audio callback thread, so encoder spikes / GC pauses
// / UDP backpressure cannot starve the audio thread and cause ADC
// overruns at the device.
//
// Under the unified design the capture device stays open for the
// lifetime of the comms run — beginTransmission / endTransmission
// toggle BroadcastEncoder.SetTxEnabled rather than opening/closing
// this stream.
func (in *Init) OpenBroadcastStream(inDev device.AudioDeviceInfo) (*BroadcastEncoder, error) {
	if in.Debug {
		if descs, dErr := device.DiscoverCM108(os.DirFS("/sys"), nil); dErr == nil {
			in.Log.Debug().
				Int("cm108_count", len(descs)).
				Str("device", inDev.Name).
				Msg("comms: unified CM108 descriptor scan at broadcast open")
		}
	}

	periodFrames := uint32(buildCapturePeriodFrames(in.CaptureFramesPerBuffer))

	opener := func(onFrame func(samples []int16)) (captureStream, error) {
		return openMalgoCapture(
			in.malgoCtx,
			inDev,
			uint32(audiopool.SampleRate),
			uint32(audiopool.Channels),
			periodFrames,
			onFrame,
		)
	}

	be, err := NewBroadcastEncoder(in.Deps, opener)
	if err != nil {
		return nil, err
	}

	info := be.s.Info()

	in.Log.Info().
		Int("configured_latency_ms", in.CaptureLatencyMs).
		Int("requested_period_frames", int(periodFrames)).
		Dur("period_duration", info.InputLatency).
		Int("period_frames", latencyToFrames(info.InputLatency)).
		Int("encode_chan_depth", broadcastEncoderChanDepth).
		Str("device", inDev.Name).
		Msg("comms: broadcast stream opened")

	return be, nil
}

// buildCapturePeriodFrames translates the CaptureFramesPerBuffer config
// knob into the value handed to malgo.DeviceConfig.PeriodSizeInFrames.
//
// Rules (mirrors the previous PortAudio semantics to keep existing
// YAML configs working unchanged):
//
//   - framesPerBuffer == 0 → substitute audiopool.FrameSize (960 =
//     20 ms @ 48 kHz) so each callback still produces exactly one Opus
//     frame.
//   - framesPerBuffer < 0 → pass 0 to malgo, letting miniaudio pick a
//     period aligned with the native ALSA period. BroadcastEncoder's
//     capture callback is responsible for handling the resulting
//     variable-length frame cadence downstream.
//   - framesPerBuffer > 0 → passthrough verbatim.
func buildCapturePeriodFrames(framesPerBuffer int) int {
	switch {
	case framesPerBuffer == 0:
		return audiopool.FrameSize
	case framesPerBuffer < 0:
		return 0
	default:
		return framesPerBuffer
	}
}

// computePlaybackPeriodFrames translates the PlaybackLatencyMs config
// knob into a malgo PeriodSizeInFrames value. The rules mirror the
// prior PortAudio floor logic:
//
//   - latencyMs <= 0 → audiopool.FrameSize (20 ms) as a safe default
//   - latencyMs > 0  → the equivalent frame count at audiopool.SampleRate
//
// miniaudio may still round the period to a backend-preferred value at
// InitDevice time, but the requested period is what we log.
func computePlaybackPeriodFrames(latencyMs int) int {
	if latencyMs <= 0 {
		return audiopool.FrameSize
	}

	return latencyMs * audiopool.SampleRate / 1000
}

// latencyToFrames converts a duration into an equivalent frame count at
// the codec sample rate. Used to render diagnostic latencies as a frame
// count in the stream open log.
func latencyToFrames(latency time.Duration) int {
	return int(latency.Seconds() * float64(audiopool.SampleRate))
}

// StartHardware initializes the malgo context, opens the broadcast and
// per-port playback streams, starts them (always-on capture + playback),
// and returns a cleanup function that stops and closes them. The
// returned BroadcastEncoder is the same instance the parent runtime
// should treat as its CommsRuntime.BroadcastStream.
func (in *Init) StartHardware(slots []PortSlot) (broadcast *BroadcastEncoder, cleanup func(), err error) {
	logProc := func(message string) {
		in.Log.Debug().Str("source", "malgo").Msg(message)
	}

	ctx, ctxErr := malgo.InitContext(nil, malgo.ContextConfig{}, logProc)
	if ctxErr != nil {
		return nil, nil, fmt.Errorf("comms: failed to initialize malgo context: %w", ctxErr)
	}

	in.malgoCtx = ctx

	if in.Debug {
		device.LogAudioDevices(ctx, in.Log)
	}

	be, _, audioErr := in.BuildAudio(slots)
	if audioErr != nil {
		_ = ctx.Uninit()
		ctx.Free()

		in.malgoCtx = nil

		return nil, nil, fmt.Errorf("comms: failed to build audio streams: %w", audioErr)
	}

	in.currentBroadcast = be

	// Start the always-on broadcast capture stream. Under the unified
	// design the capture callback runs continuously from here until
	// cleanup — SetTxEnabled toggles whether captured frames reach
	// the encoder, not whether the stream itself is running.
	if startErr := be.Start(); startErr != nil {
		_ = be.Close()
		in.currentBroadcast = nil

		for _, s := range in.playbackStreams {
			_ = s.Close()
		}

		in.playbackStreams = nil

		_ = ctx.Uninit()
		ctx.Free()

		in.malgoCtx = nil

		return nil, nil, fmt.Errorf("comms: failed to start broadcast stream: %w", startErr)
	}

	// Start the per-port playback streams that BuildAudio installed. On
	// any failure, stop and close every stream we already started plus
	// the broadcast stream before propagating the error.
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

			_ = ctx.Uninit()
			ctx.Free()

			in.malgoCtx = nil

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

		if in.malgoCtx != nil {
			_ = in.malgoCtx.Uninit()
			in.malgoCtx.Free()
			in.malgoCtx = nil
		}
	}

	return be, cleanup, nil
}
