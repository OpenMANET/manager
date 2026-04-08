package audio

import (
	"testing"
	"time"

	"github.com/gordonklaus/portaudio"
	"github.com/stretchr/testify/assert"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

// fakeDevice returns a *portaudio.DeviceInfo suitable for unit-testing
// buildCaptureStreamParameters without opening a real audio device. Only
// Name and the latency fields are populated; the rest are zero values.
func fakeDevice(highInputLatency time.Duration) *portaudio.DeviceInfo {
	return &portaudio.DeviceInfo{
		Name:                    "test-device",
		MaxInputChannels:        1,
		DefaultHighInputLatency: highInputLatency,
		DefaultSampleRate:       float64(audiopool.SampleRate),
	}
}

func TestBuildCaptureStreamParameters_LatencyFloor(t *testing.T) {
	// Device reports a high input latency of 80 ms; caller requests 40 ms.
	// The floor at DefaultHighInputLatency wins, because the device's own
	// recommendation is higher than the caller's suggestion.
	inDev := fakeDevice(80 * time.Millisecond)

	params := buildCaptureStreamParameters(inDev, 40, audiopool.FrameSize)

	assert.Equal(t, 80*time.Millisecond, params.Input.Latency,
		"latency should be floored at DefaultHighInputLatency (80 ms), not caller suggestion (40 ms)")
}

func TestBuildCaptureStreamParameters_HonorsHigherCallerRequest(t *testing.T) {
	// Device reports a modest high input latency (21 ms on CM108-class
	// hardware), caller requests 60 ms. The caller's request wins because
	// it is above the floor.
	inDev := fakeDevice(21 * time.Millisecond)

	params := buildCaptureStreamParameters(inDev, 60, audiopool.FrameSize)

	assert.Equal(t, 60*time.Millisecond, params.Input.Latency,
		"caller's 60 ms request should win over device's 21 ms floor")
}

func TestBuildCaptureStreamParameters_FramesPerBufferDefaultWhenZero(t *testing.T) {
	// framesPerBuffer == 0 means "not configured" (legacy / test /
	// programmatic path) — substitute audiopool.FrameSize (960) so each
	// callback still produces exactly one Opus frame.
	inDev := fakeDevice(60 * time.Millisecond)

	params := buildCaptureStreamParameters(inDev, 60, 0)

	assert.Equal(t, audiopool.FrameSize, params.FramesPerBuffer,
		"framesPerBuffer == 0 should be substituted with audiopool.FrameSize")
}

func TestBuildCaptureStreamParameters_FramesPerBufferUnspecifiedWhenNegative(t *testing.T) {
	// framesPerBuffer < 0 is the operator's explicit escape hatch:
	// paFramesPerBufferUnspecified (0 in the PortAudio C API) lets the
	// host API pick a frame count aligned with the native ALSA period.
	inDev := fakeDevice(60 * time.Millisecond)

	params := buildCaptureStreamParameters(inDev, 60, -1)

	assert.Equal(t, 0, params.FramesPerBuffer,
		"negative framesPerBuffer should map to 0 (paFramesPerBufferUnspecified)")
}

func TestBuildCaptureStreamParameters_FramesPerBufferPositivePassthrough(t *testing.T) {
	inDev := fakeDevice(60 * time.Millisecond)

	params := buildCaptureStreamParameters(inDev, 60, 1920)

	assert.Equal(t, 1920, params.FramesPerBuffer,
		"positive framesPerBuffer should be passed through verbatim")
}

func TestBuildCaptureStreamParameters_CommonFields(t *testing.T) {
	// Verify the non-translation fields (sample rate, channels, device
	// pointer) are set correctly so a regression in the shared parts of
	// the helper is obvious.
	inDev := fakeDevice(60 * time.Millisecond)

	params := buildCaptureStreamParameters(inDev, 60, audiopool.FrameSize)

	assert.InEpsilon(t, float64(audiopool.SampleRate), params.SampleRate, 1e-9)
	assert.Equal(t, audiopool.Channels, params.Input.Channels)
	assert.Same(t, inDev, params.Input.Device)
}

func TestLatencyToFrames(t *testing.T) {
	tests := []struct {
		name    string
		latency time.Duration
		want    int
	}{
		{
			name:    "zero latency is zero frames",
			latency: 0,
			want:    0,
		},
		{
			// 20 ms @ 48 kHz = 960 samples — one Opus frame.
			name:    "one frame period",
			latency: 20 * time.Millisecond,
			want:    audiopool.FrameSize,
		},
		{
			// 60 ms @ 48 kHz = 2880 samples — three frames of buffering,
			// the default capture latency on CM108-class hardware.
			name:    "three frame periods",
			latency: 60 * time.Millisecond,
			want:    3 * audiopool.FrameSize,
		},
		{
			// Matches the observed actual_input_latency on the openvlm
			// target that produced the original stutter report.
			name:    "arbitrary sub-millisecond value",
			latency: 21 * time.Millisecond,
			want:    1008,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, latencyToFrames(tt.latency))
		})
	}
}
