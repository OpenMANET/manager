package audio

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// malgoCaptureStream wraps a malgo input device as a captureStream. It
// owns no goroutines of its own — all audio work runs on the miniaudio
// callback thread, which invokes the configured onFrame with an int16
// view of the same buffer miniaudio allocated (reinterpret via
// unsafe.Slice; safe because FormatS16 guarantees 2-byte alignment, the
// host is little-endian, and the buffer outlives the callback call).
type malgoCaptureStream struct {
	dev  *malgo.Device
	info streamInfo
}

// openMalgoCapture configures and initializes a malgo capture device in
// FormatS16. The returned captureStream has not been started yet —
// audio.BroadcastEncoder.Start kicks off the callback thread.
func openMalgoCapture(
	ctx *malgo.AllocatedContext,
	dev device.AudioDeviceInfo,
	sampleRate uint32,
	channels uint32,
	periodFrames uint32,
	onFrame func(samples []int16),
) (*malgoCaptureStream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("malgo capture: nil context")
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = channels
	cfg.Capture.DeviceID = dev.ID.Pointer()
	cfg.SampleRate = sampleRate
	cfg.PeriodSizeInFrames = periodFrames
	cfg.PerformanceProfile = malgo.LowLatency
	cfg.Alsa.NoMMap = 0

	onData := func(_, in []byte, frameCount uint32) {
		if len(in) == 0 || frameCount == 0 {
			return
		}

		samples := int16View(in, int(frameCount)*int(channels))
		onFrame(samples)
	}

	d, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: onData})
	if err != nil {
		return nil, fmt.Errorf("malgo init capture device: %w", err)
	}

	return &malgoCaptureStream{
		dev: d,
		info: streamInfo{
			InputLatency: periodFramesToDuration(periodFrames, sampleRate),
		},
	}, nil
}

// Start kicks off the miniaudio callback thread for this capture device.
func (m *malgoCaptureStream) Start() error {
	if err := m.dev.Start(); err != nil {
		return fmt.Errorf("malgo capture start: %w", err)
	}

	return nil
}

// Stop halts the capture device without releasing its resources.
func (m *malgoCaptureStream) Stop() error {
	if err := m.dev.Stop(); err != nil {
		return fmt.Errorf("malgo capture stop: %w", err)
	}

	return nil
}

// Close uninitializes the capture device. malgo's Uninit implicitly
// stops the device, so calling Stop first is optional.
func (m *malgoCaptureStream) Close() error {
	m.dev.Uninit()

	return nil
}

// Info returns the configured period-derived latency. miniaudio does
// not expose a negotiated runtime latency so this is the value we asked
// for at open time, not a value read back from ALSA.
func (m *malgoCaptureStream) Info() streamInfo { return m.info }

// int16View reinterprets a byte slice as an int16 slice for the
// duration of a single audio callback. The callback contract with
// miniaudio is that the buffer pointer and length are stable for the
// call, so the returned view is valid for the rest of the callback but
// MUST NOT be retained past return.
//
// Alignment: miniaudio allocates the capture buffer from its own pool
// with at least int16 (2-byte) alignment when FormatS16 is in effect,
// so casting the first byte pointer to *int16 is safe on every
// architecture we build for (amd64, arm64, mipsle).
func int16View(b []byte, samples int) []int16 {
	if len(b) == 0 || samples == 0 {
		return nil
	}

	return unsafe.Slice((*int16)(unsafe.Pointer(&b[0])), samples)
}

// periodFramesToDuration converts a PeriodSizeInFrames value to the
// equivalent wall-clock duration at the given sample rate. Used to fill
// streamInfo.InputLatency / OutputLatency with a diagnostic that maps
// directly onto the configured period size.
func periodFramesToDuration(frames, sampleRate uint32) time.Duration {
	if sampleRate == 0 {
		return 0
	}

	return time.Duration(frames) * time.Second / time.Duration(sampleRate)
}
