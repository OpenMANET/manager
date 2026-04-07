package comms

import "github.com/openmanet/openmanetd/internal/comms/audiopool"

// This file re-exports the audiopool leaf-package constants under their
// historical exported parent-package names so existing call sites in the
// parent package compile unchanged during the audio/ extraction (Step 3C).
//
// float32Pool / rmsEnergy now live in internal/comms/audiopool. Call sites
// inside the parent package use audiopool.Float32Pool and audiopool.RMSEnergy
// directly.

const (
	// FrameSize is the number of audio samples per codec frame (20 ms at 48 kHz).
	FrameSize = audiopool.FrameSize
	// SampleRate is the PCM sample rate used by the audio pipeline.
	SampleRate = audiopool.SampleRate
	// Channels is the PCM channel count (mono).
	Channels = audiopool.Channels
	// MaxConsecutivePLC caps the number of consecutive Packet-Loss-Concealment
	// frames emitted before the receiver drops into silence.
	MaxConsecutivePLC = audiopool.MaxConsecutivePLC
	// ConcealRecentWindow is the inter-arrival window during which a missing
	// frame is concealed with PLC rather than treated as end-of-stream.
	ConcealRecentWindow = audiopool.ConcealRecentWindow
)
