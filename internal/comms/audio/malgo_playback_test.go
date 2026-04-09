package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

// patternedPlayoutFrame returns a playoutFrame closure that fills the
// supplied buffer with samples (start+0, start+1, ...). After each call
// it advances `start` by chunkSize so successive calls produce
// contiguous, non-overlapping ramps. The closure also bumps a call
// counter so tests can assert how many times it fired.
func patternedPlayoutFrame(start *int, calls *int) func(out []int16) {
	return func(out []int16) {
		*calls++

		for i := range out {
			out[i] = int16(*start + i)
		}

		*start += len(out)
	}
}

// TestPlaybackChunker_ExactDrainCallsPlayoutFrameOnce: a single 960-
// sample drain should fire playoutFrame exactly once and the output
// should equal the chunk that closure produced.
func TestPlaybackChunker_ExactDrainCallsPlayoutFrameOnce(t *testing.T) {
	var (
		start int
		calls int
	)

	pc := newPlaybackChunker(nil, patternedPlayoutFrame(&start, &calls))

	out := make([]int16, audiopool.FrameSize)
	pc.drain(out)

	require.Equal(t, 1, calls)
	assert.Equal(t, rampSamples(0, audiopool.FrameSize), out)
	assert.Equal(t, audiopool.FrameSize, pc.pos, "staging buffer should be fully drained")
}

// TestPlaybackChunker_LargerOutputRefillsAcrossBoundary covers the
// production case from the bug log: ALSA hands the playback callback a
// 1024-sample output buffer when our chunk size is 960. The chunker
// must call playoutFrame twice (once at start, once after the first
// chunk is exhausted) and the output stream must be a contiguous
// concatenation of both chunks with no gaps.
func TestPlaybackChunker_LargerOutputRefillsAcrossBoundary(t *testing.T) {
	const periodFrames = 1024

	var (
		start int
		calls int
	)

	pc := newPlaybackChunker(nil, patternedPlayoutFrame(&start, &calls))

	out := make([]int16, periodFrames)
	pc.drain(out)

	require.Equal(t, 2, calls, "1024-sample drain must trigger two refills (960 + 64)")

	// First 960 samples come from the first refill (ramp 0..959).
	assert.Equal(t, rampSamples(0, audiopool.FrameSize), out[:audiopool.FrameSize])

	// Next 64 samples come from the head of the second refill (ramp 960..1023).
	assert.Equal(t, rampSamples(audiopool.FrameSize, periodFrames-audiopool.FrameSize), out[audiopool.FrameSize:])

	// The chunker should have 64 samples consumed out of the second
	// staged chunk, leaving 896 buffered for the next callback.
	assert.Equal(t, periodFrames-audiopool.FrameSize, pc.pos)
}

// TestPlaybackChunker_SmallOutputDoesNotRefillEarly verifies that small
// drains keep consuming from the same staged chunk and only trigger a
// refill once the chunk is fully consumed.
func TestPlaybackChunker_SmallOutputDoesNotRefillEarly(t *testing.T) {
	var (
		start int
		calls int
	)

	pc := newPlaybackChunker(nil, patternedPlayoutFrame(&start, &calls))

	half := audiopool.FrameSize / 2

	// First half-chunk: triggers the initial refill.
	out := make([]int16, half)
	pc.drain(out)
	assert.Equal(t, 1, calls)
	assert.Equal(t, half, pc.pos)
	assert.Equal(t, rampSamples(0, half), out)

	// Second half-chunk: drains the rest of the staged chunk; no refill.
	pc.drain(out)
	assert.Equal(t, 1, calls, "second half-chunk should NOT trigger a refill")
	assert.Equal(t, audiopool.FrameSize, pc.pos)
	assert.Equal(t, rampSamples(half, half), out)

	// One more sample: now the chunk is exhausted so this triggers refill #2.
	one := make([]int16, 1)
	pc.drain(one)
	assert.Equal(t, 2, calls, "drain past chunkSize should trigger second refill")
	assert.Equal(t, 1, pc.pos)
	assert.Equal(t, int16(audiopool.FrameSize), one[0])
}

// TestPlaybackChunker_BeepReplacesOneChunk verifies that a queued beep
// preempts exactly one playoutFrame refill: the beep fills the entire
// staging buffer and playoutFrame is not called for that chunk.
func TestPlaybackChunker_BeepReplacesOneChunk(t *testing.T) {
	var (
		start int
		calls int
	)

	beepBuf := make(chan []int16, 1)

	beep := rampSamples(9000, audiopool.FrameSize)
	beepBuf <- beep

	pc := newPlaybackChunker(beepBuf, patternedPlayoutFrame(&start, &calls))

	// First drain: should consume the beep.
	out := make([]int16, audiopool.FrameSize)
	pc.drain(out)

	assert.Equal(t, 0, calls, "beep refill must not invoke playoutFrame")
	assert.Equal(t, beep, out)

	// Second drain: beep channel is empty → falls through to playoutFrame.
	pc.drain(out)
	assert.Equal(t, 1, calls)
	assert.Equal(t, rampSamples(0, audiopool.FrameSize), out)
}

// TestPlaybackChunker_BeepSpanningCallbacks pushes a beep, then drains
// a 1024-sample output. The first 960 samples must equal the beep and
// the next 64 must equal the head of a fresh playoutFrame chunk.
func TestPlaybackChunker_BeepSpanningCallbacks(t *testing.T) {
	const periodFrames = 1024

	var (
		start int
		calls int
	)

	beepBuf := make(chan []int16, 1)

	beep := rampSamples(9000, audiopool.FrameSize)
	beepBuf <- beep

	pc := newPlaybackChunker(beepBuf, patternedPlayoutFrame(&start, &calls))

	out := make([]int16, periodFrames)
	pc.drain(out)

	assert.Equal(t, 1, calls, "playoutFrame should fire exactly once (after the beep is consumed)")
	assert.Equal(t, beep, out[:audiopool.FrameSize])
	assert.Equal(t, rampSamples(0, periodFrames-audiopool.FrameSize), out[audiopool.FrameSize:])
}

// TestPlaybackChunker_NoLossAcrossManyCallbacks runs 100 callbacks of
// 1024 samples each through the chunker and asserts that the resulting
// output stream is a strictly contiguous ramp from 0 — no samples are
// dropped or duplicated at the chunk boundary.
func TestPlaybackChunker_NoLossAcrossManyCallbacks(t *testing.T) {
	const (
		periodFrames = 1024
		callbacks    = 100
	)

	var (
		start int
		calls int
	)

	pc := newPlaybackChunker(nil, patternedPlayoutFrame(&start, &calls))

	full := make([]int16, 0, periodFrames*callbacks)
	out := make([]int16, periodFrames)

	for range callbacks {
		pc.drain(out)
		full = append(full, out...)
	}

	expected := rampSamples(0, periodFrames*callbacks)
	assert.Equal(t, expected, full, "output must be strictly contiguous across all callbacks")
}

// TestPlaybackChunker_NoExtraAllocationsInDrain confirms the chunker's
// hot path stays allocation-free, which matters on the constrained
// embedded targets — every 21 ms allocation would chew through the GC
// budget on a MIPS router.
func TestPlaybackChunker_NoExtraAllocationsInDrain(t *testing.T) {
	const periodFrames = 1024

	var (
		start int
		calls int
	)

	// playoutFrame closure must itself be allocation-free for the
	// AllocsPerRun check to be meaningful. patternedPlayoutFrame fits
	// — it only writes through pre-allocated memory.
	pc := newPlaybackChunker(nil, patternedPlayoutFrame(&start, &calls))
	out := make([]int16, periodFrames)

	allocs := testing.AllocsPerRun(100, func() {
		pc.drain(out)
	})

	assert.Zero(t, allocs, "drain must be allocation-free; got %v allocs/op", allocs)
}
