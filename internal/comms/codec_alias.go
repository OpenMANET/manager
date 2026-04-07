package comms

import "github.com/openmanet/openmanetd/internal/comms/codec"

// Aliases bridging the legacy unexported codec types/constructors to the new
// internal/comms/codec sub-package. They keep the rest of the flat comms
// package compiling unchanged while later phases of the comms refactor
// (see .claude/plans/comms-refactor.md) migrate call sites directly.

// AudioEncoder is an alias for codec.AudioEncoder.
type AudioEncoder = codec.AudioEncoder

// AudioDecoder is an alias for codec.AudioDecoder.
type AudioDecoder = codec.AudioDecoder

// newOpusEncoder constructs an Opus encoder using the package-level audio
// constants.
func newOpusEncoder(complexity int) (AudioEncoder, error) {
	return codec.NewOpusEncoder(sampleRate, channels, targetBitrate, complexity, packetLossPerc)
}

// newOpusDecoder constructs an Opus decoder using the package-level audio
// constants.
func newOpusDecoder() (AudioDecoder, error) {
	return codec.NewOpusDecoder(sampleRate, channels)
}
