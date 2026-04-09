package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

func TestBuildCapturePeriodFrames_DefaultDerivesFromLatency(t *testing.T) {
	// framesPerBuffer == 0 means "not configured" — derive the period
	// from CaptureLatencyMs (mirrors playback). 60 ms @ 48 kHz = 2880
	// frames. The captureChunker re-aligns whatever ALSA gives back onto
	// 960-sample chunks, so a larger period is invisible to the encoder.
	assert.Equal(t, 2880, buildCapturePeriodFrames(0, 60),
		"framesPerBuffer == 0 should derive period from latencyMs")
}

func TestBuildCapturePeriodFrames_DefaultFallbackWhenLatencyUnset(t *testing.T) {
	// When neither knob is set, fall back to one Opus frame.
	assert.Equal(t, audiopool.FrameSize, buildCapturePeriodFrames(0, 0),
		"framesPerBuffer == 0 and latencyMs == 0 should fall back to FrameSize")
	assert.Equal(t, audiopool.FrameSize, buildCapturePeriodFrames(0, -1),
		"framesPerBuffer == 0 and negative latencyMs should fall back to FrameSize")
}

func TestBuildCapturePeriodFrames_UnspecifiedWhenNegative(t *testing.T) {
	// framesPerBuffer < 0 is the operator's explicit escape hatch:
	// returning 0 tells malgo "let miniaudio pick a period aligned with
	// the native ALSA period".
	assert.Equal(t, 0, buildCapturePeriodFrames(-1, 60),
		"negative framesPerBuffer should map to 0 regardless of latencyMs")
}

func TestBuildCapturePeriodFrames_PositivePassthrough(t *testing.T) {
	assert.Equal(t, 1920, buildCapturePeriodFrames(1920, 60),
		"positive framesPerBuffer should be passed through verbatim")
}

func TestComputePlaybackPeriodFrames_DefaultWhenZeroOrNegative(t *testing.T) {
	assert.Equal(t, audiopool.FrameSize, computePlaybackPeriodFrames(0),
		"zero latency should fall back to one Opus frame")
	assert.Equal(t, audiopool.FrameSize, computePlaybackPeriodFrames(-1),
		"negative latency should fall back to one Opus frame")
}

func TestComputePlaybackPeriodFrames_ConvertsMsToFrames(t *testing.T) {
	// 60 ms @ 48 kHz = 2880 frames — the target hardware floor on the
	// CM108 class device that produced the original jitter report.
	assert.Equal(t, 2880, computePlaybackPeriodFrames(60))

	// 20 ms @ 48 kHz = 960 frames — exactly one Opus frame.
	assert.Equal(t, 960, computePlaybackPeriodFrames(20))
}

func TestPeriodFramesToDuration(t *testing.T) {
	tests := []struct {
		name       string
		frames     uint32
		sampleRate uint32
		want       time.Duration
	}{
		{
			name:       "zero frames is zero duration",
			frames:     0,
			sampleRate: uint32(audiopool.SampleRate),
			want:       0,
		},
		{
			name:       "one Opus frame is 20 ms",
			frames:     uint32(audiopool.FrameSize),
			sampleRate: uint32(audiopool.SampleRate),
			want:       20 * time.Millisecond,
		},
		{
			name:       "three Opus frames is 60 ms",
			frames:     uint32(3 * audiopool.FrameSize),
			sampleRate: uint32(audiopool.SampleRate),
			want:       60 * time.Millisecond,
		},
		{
			name:       "zero sample rate returns zero",
			frames:     uint32(audiopool.FrameSize),
			sampleRate: 0,
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, periodFramesToDuration(tt.frames, tt.sampleRate))
		})
	}
}
