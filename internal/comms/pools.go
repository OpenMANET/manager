package comms

import "sync"

// This file houses buffer pools and numeric constants that must be shared
// between the parent package and its sub-packages (audio/, control/) once
// those extractions ship. Step 3A of the comms layout refactor introduced
// this sibling file to host float32Pool so the future audio/ and
// control/roip.go can both reference it without either importing the other.

// ─── Exported audio constants ───────────────────────────────────────────────
//
// These mirror the unexported package-level constants in comms.go so that
// files moving into internal/comms/audio/ (Step 3B) can reference them via
// the parent package name. The original unexported names are kept so the
// rest of the parent package compiles unchanged during the transition.

const (
	// FrameSize is the number of audio samples per codec frame (20 ms at 48 kHz).
	FrameSize = frameSize
	// SampleRate is the PCM sample rate used by the audio pipeline.
	SampleRate = sampleRate
	// Channels is the PCM channel count (mono).
	Channels = channels
	// MaxConsecutivePLC caps the number of consecutive Packet-Loss-Concealment
	// frames emitted before the receiver drops into silence.
	MaxConsecutivePLC = maxConsecutivePLC
	// ConcealRecentWindow is the inter-arrival window during which a missing
	// frame is concealed with PLC rather than treated as end-of-stream.
	ConcealRecentWindow = concealRecentWindow
)

// ─── float32Pool ────────────────────────────────────────────────────────────
//
// float32Pool was previously declared inline in comms.go. It is used by
// legacy float32 boundaries (VOX energy scratch paths in control/roip.go
// after Step 4 and any audio-side float32 adapters). Moving it into this
// sibling file gives both internal/comms/audio and internal/comms/control a
// stable place to reference it (as comms.float32Pool via parent package) once
// those extractions land.

var float32Pool = sync.Pool{ //nolint:gochecknoglobals
	New: func() any {
		s := make([]float32, frameSize)

		return &s
	},
}
