package audio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

// newTestCaptureStream constructs a malgoCaptureStream wired only with
// the chunker state — no malgo device. captureChunk operates purely on
// stream.accum / stream.accumLen / stream.chunkSize so this is enough
// to drive the production code path from a unit test without spinning
// up real audio hardware.
func newTestCaptureStream(chunkSize int) *malgoCaptureStream {
	return &malgoCaptureStream{
		accum:     make([]int16, chunkSize),
		chunkSize: chunkSize,
	}
}

// capturedFrames collects every chunk captureChunk emits, defensively
// copying because captureChunk passes its internal accumulator slice
// (which it later mutates) on the slow path.
type capturedFrames struct {
	frames [][]int16
}

func (r *capturedFrames) onFrame(in []int16) {
	cp := make([]int16, len(in))
	copy(cp, in)
	r.frames = append(r.frames, cp)
}

// rampSamples returns a slice of length n where samples[i] = int16(start + i),
// chosen so each test can assert that consecutive callbacks produce a
// strictly contiguous integer sequence with no gaps or duplicates at
// the chunk boundary.
func rampSamples(start, n int) []int16 {
	out := make([]int16, n)
	for i := range n {
		out[i] = int16(start + i)
	}

	return out
}

// TestCaptureChunker_ExactMultipleEmitsInPlace verifies the fast path:
// when nothing is buffered and the input is an exact multiple of the
// chunk size, captureChunk emits each chunk directly without copying
// through the accumulator.
func TestCaptureChunker_ExactMultipleEmitsInPlace(t *testing.T) {
	stream := newTestCaptureStream(audiopool.FrameSize)
	sink := &capturedFrames{}

	// Single 960-sample input → exactly one frame.
	captureChunk(stream, rampSamples(0, audiopool.FrameSize), sink.onFrame)
	require.Len(t, sink.frames, 1)
	assert.Equal(t, rampSamples(0, audiopool.FrameSize), sink.frames[0])
	assert.Zero(t, stream.accumLen, "accumLen should be 0 after exact-multiple input")

	// 1920-sample input (2 chunks) → two frames.
	captureChunk(stream, rampSamples(1000, 2*audiopool.FrameSize), sink.onFrame)
	require.Len(t, sink.frames, 3)
	assert.Equal(t, rampSamples(1000, audiopool.FrameSize), sink.frames[1])
	assert.Equal(t, rampSamples(1000+audiopool.FrameSize, audiopool.FrameSize), sink.frames[2])
	assert.Zero(t, stream.accumLen)
}

// TestCaptureChunker_LargerThanChunkBuffersTail covers the actual
// production case from the bug log: ALSA delivers 1024-frame periods
// when we asked for 960. Each callback should emit one full chunk and
// retain the leftover 64 samples for the next callback.
func TestCaptureChunker_LargerThanChunkBuffersTail(t *testing.T) {
	const periodFrames = 1024

	stream := newTestCaptureStream(audiopool.FrameSize)
	sink := &capturedFrames{}

	captureChunk(stream, rampSamples(0, periodFrames), sink.onFrame)
	require.Len(t, sink.frames, 1)
	assert.Equal(t, rampSamples(0, audiopool.FrameSize), sink.frames[0])
	assert.Equal(t, periodFrames-audiopool.FrameSize, stream.accumLen, "64 leftover samples should remain in accum")
	assert.Equal(t, rampSamples(audiopool.FrameSize, periodFrames-audiopool.FrameSize), stream.accum[:stream.accumLen])

	// Second callback: another 1024 samples. With 64 already buffered,
	// the chunker should emit one more 960-sample chunk (64 carry-over
	// + 896 from the new input) and retain the remaining 128 samples.
	captureChunk(stream, rampSamples(periodFrames, periodFrames), sink.onFrame)
	require.Len(t, sink.frames, 2)
	assert.Equal(t, 2*periodFrames-2*audiopool.FrameSize, stream.accumLen, "128 leftover samples should remain in accum")

	// The second emitted chunk must equal the contiguous sequence that
	// spans the boundary [audiopool.FrameSize .. audiopool.FrameSize+960].
	expected := rampSamples(audiopool.FrameSize, audiopool.FrameSize)
	assert.Equal(t, expected, sink.frames[1])
}

// TestCaptureChunker_PartialFrameBoundary feeds three half-chunk inputs
// in a row and asserts that the chunker fires onFrame exactly once
// (after the second half) and retains the third half in the accumulator.
func TestCaptureChunker_PartialFrameBoundary(t *testing.T) {
	stream := newTestCaptureStream(audiopool.FrameSize)
	sink := &capturedFrames{}

	half := audiopool.FrameSize / 2

	captureChunk(stream, rampSamples(0, half), sink.onFrame)
	assert.Empty(t, sink.frames, "first half-chunk should not emit")
	assert.Equal(t, half, stream.accumLen)

	captureChunk(stream, rampSamples(half, half), sink.onFrame)
	require.Len(t, sink.frames, 1, "second half-chunk should complete one frame")
	assert.Equal(t, rampSamples(0, audiopool.FrameSize), sink.frames[0])
	assert.Zero(t, stream.accumLen)

	captureChunk(stream, rampSamples(audiopool.FrameSize, half), sink.onFrame)
	require.Len(t, sink.frames, 1, "third half-chunk should not emit yet")
	assert.Equal(t, half, stream.accumLen)
}

// TestCaptureChunker_NoLossAcrossManyCallbacks proves the chunker
// preserves every sample with zero gaps and zero duplicates across many
// callbacks at the production 1024-frame period. We feed a recognizable
// counter pattern, concatenate every emitted chunk, and assert the
// concatenation is a strict prefix of the original input.
func TestCaptureChunker_NoLossAcrossManyCallbacks(t *testing.T) {
	const (
		periodFrames = 1024
		callbacks    = 100
	)

	stream := newTestCaptureStream(audiopool.FrameSize)
	sink := &capturedFrames{}

	totalSamples := periodFrames * callbacks
	source := rampSamples(0, totalSamples)

	for cb := range callbacks {
		off := cb * periodFrames
		captureChunk(stream, source[off:off+periodFrames], sink.onFrame)
	}

	// Concatenate every emitted chunk and verify it is the strict
	// prefix of `source` matching the total emitted length.
	var emitted []int16

	for _, frame := range sink.frames {
		require.Len(t, frame, audiopool.FrameSize)
		emitted = append(emitted, frame...)
	}

	expectedFrames := totalSamples / audiopool.FrameSize
	expectedEmitted := expectedFrames * audiopool.FrameSize

	assert.Len(t, sink.frames, expectedFrames)
	assert.Equal(t, source[:expectedEmitted], emitted, "no samples may be lost or duplicated at chunk boundaries")
	assert.Equal(t, totalSamples-expectedEmitted, stream.accumLen, "trailing samples must remain in accum")
}

// TestCaptureChunker_EmptyInputIsNoOp verifies the early-exit guard.
func TestCaptureChunker_EmptyInputIsNoOp(t *testing.T) {
	stream := newTestCaptureStream(audiopool.FrameSize)
	sink := &capturedFrames{}

	captureChunk(stream, nil, sink.onFrame)
	captureChunk(stream, []int16{}, sink.onFrame)

	assert.Empty(t, sink.frames)
	assert.Zero(t, stream.accumLen)
}
