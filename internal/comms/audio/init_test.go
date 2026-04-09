package audio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

func TestBuildCapturePeriodFrames_DefaultIsOneOpusFrame(t *testing.T) {
	// framesPerBuffer == 0 always returns one Opus frame (960 = 20 ms).
	// The encoder's per-frame deadline check assumes ALSA wakes the
	// callback every 20 ms; the latencyMs knob controls ring depth via
	// buildCapturePeriods, not period size.
	assert.Equal(t, audiopool.FrameSize, buildCapturePeriodFrames(0, 60))
	assert.Equal(t, audiopool.FrameSize, buildCapturePeriodFrames(0, 0))
}

func TestBuildCapturePeriods(t *testing.T) {
	// 60 ms latency / 20 ms period = 3 periods (matches floor).
	assert.Equal(t, 3, buildCapturePeriods(60, audiopool.FrameSize))
	// 120 ms latency / 20 ms period = 6 periods.
	assert.Equal(t, 6, buildCapturePeriods(120, audiopool.FrameSize))
	// Floor at 3 periods.
	assert.Equal(t, 3, buildCapturePeriods(0, audiopool.FrameSize))
	assert.Equal(t, 3, buildCapturePeriods(20, audiopool.FrameSize))
	// Ceiling at 16 periods.
	assert.Equal(t, 16, buildCapturePeriods(10000, audiopool.FrameSize))
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
