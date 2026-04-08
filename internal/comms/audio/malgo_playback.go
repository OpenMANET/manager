package audio

import (
	"fmt"

	"github.com/gen2brain/malgo"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// openMalgoPlayback configures and initializes a malgo playback device
// in FormatS16 bound to the supplied beepBuf / playoutFrame closures.
// The returned *malgo.Device has not been started yet — the caller
// must invoke Start() via device.NewMalgoStream to activate the
// callback thread.
//
// The output callback reinterprets the byte buffer miniaudio hands it
// as []int16 via unsafe.Slice (same alignment argument as the capture
// path in int16View), drains one start/stop beep frame if one is
// pending, and otherwise delegates to playoutFrame to write the next
// jitter-buffered chunk.
func openMalgoPlayback(
	ctx *malgo.AllocatedContext,
	dev device.AudioDeviceInfo,
	sampleRate uint32,
	channels uint32,
	periodFrames uint32,
	beepBuf <-chan []int16,
	playoutFrame func(out []int16),
) (*malgo.Device, error) {
	if ctx == nil {
		return nil, fmt.Errorf("malgo playback: nil context")
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = channels
	cfg.Playback.DeviceID = dev.ID.Pointer()
	cfg.SampleRate = sampleRate
	cfg.PeriodSizeInFrames = periodFrames
	cfg.PerformanceProfile = malgo.LowLatency
	cfg.Alsa.NoMMap = 0

	onData := func(out, _ []byte, frameCount uint32) {
		if len(out) == 0 || frameCount == 0 {
			return
		}

		samples := int16View(out, int(frameCount)*int(channels))

		// Beep injection: TX start/stop tones preempt one frame of
		// jitter-buffered audio. The select is non-blocking so a
		// missing beep falls straight through to playoutFrame.
		select {
		case data := <-beepBuf:
			copy(samples, data)

			return
		default:
		}

		playoutFrame(samples)
	}

	d, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		return nil, fmt.Errorf("malgo init playback device: %w", err)
	}

	return d, nil
}
