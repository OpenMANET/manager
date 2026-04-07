// Package audiopool holds audio numeric constants and float32 buffer pools
// shared by sibling sub-packages of internal/comms (audio/, control/). It is
// a leaf package: stdlib-only, no dependencies on the parent comms package or
// any of its sub-packages, so it can be imported from anywhere without
// risking an import cycle.
package audiopool

import (
	"math"
	"sync"
)

// Audio constants. These mirror the historical unexported constants in the
// parent comms package and are the canonical home going forward.
const (
	// FrameSize is the number of audio samples per codec frame (20 ms at 48 kHz).
	FrameSize int = 960
	// SampleRate is the PCM sample rate used by the audio pipeline.
	SampleRate int = 48000
	// Channels is the PCM channel count (mono).
	Channels int = 1
	// MaxConsecutivePLC caps the number of consecutive Packet-Loss-Concealment
	// frames emitted by the receiver before it drops into silence.
	MaxConsecutivePLC int = 10
	// ConcealRecentWindow is the inter-arrival window (in milliseconds) during
	// which a missing frame is concealed with PLC rather than treated as
	// end-of-stream. Callers needing a time.Duration should multiply by
	// time.Millisecond.
	ConcealRecentWindow int = 200
)

// Float32Pool pools fixed-size []float32 frames used by float32 boundaries
// (VOX energy scratch paths and float32 audio adapters).
var Float32Pool = sync.Pool{ //nolint:gochecknoglobals
	New: func() any {
		s := make([]float32, FrameSize)

		return &s
	},
}

// ReturnFloat32 returns a pooled []float32 slice to Float32Pool. Non-pooled
// slices (capacity != FrameSize) are silently ignored because their capacity
// will differ from FrameSize.
func ReturnFloat32(s []float32) {
	if cap(s) != FrameSize {
		return
	}

	sp := &s
	Float32Pool.Put(sp)
}

// RMSEnergy computes the root-mean-square energy of a float32 PCM frame.
func RMSEnergy(frame []float32) float32 {
	if len(frame) == 0 {
		return 0
	}

	var sum float64

	for _, v := range frame {
		sum += float64(v) * float64(v)
	}

	return float32(math.Sqrt(sum / float64(len(frame))))
}
