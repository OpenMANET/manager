// Package codec defines audio encoder/decoder interfaces and an Opus
// implementation. It is intentionally a leaf package: it depends only on
// libopus and exposes minimal interfaces so the comms package and its
// sub-packages can construct codecs without importing each other.
//
// This package was extracted from internal/comms during Phase 1 of the comms
// refactor (see .claude/plans/comms-refactor.md). Later phases will introduce
// int16-native hot-path entry points; the current API mirrors the original
// flat-package implementation byte-for-byte to preserve behavior.
package codec

import (
	"fmt"

	"github.com/hraban/opus"
)

// AudioEncoder encodes PCM int16 samples into a compressed codec frame.
type AudioEncoder interface {
	Encode(pcm []int16, data []byte) (int, error)
}

// AudioDecoder decodes a compressed codec frame back into PCM int16 samples.
// Passing nil data triggers Opus Packet Loss Concealment (PLC).
type AudioDecoder interface {
	Decode(data []byte, pcm []int16) (int, error)
	// DecodeFloat32 decodes directly into float32 PCM, skipping the int16
	// intermediate stage. Passing nil data triggers Opus PLC.
	DecodeFloat32(data []byte, pcm []float32) (int, error)
}

// ─── Opus implementation ──────────────────────────────────────────────────────

type opusEncoder struct {
	enc *opus.Encoder
}

// Encode encodes a frame of PCM int16 samples into the output buffer data and
// returns the number of bytes written. The length of pcm must match the
// configured frame size (sampleRate × frameDuration × channels). data must be
// large enough to hold the encoded output.
func (o *opusEncoder) Encode(pcm []int16, data []byte) (int, error) {
	n, err := o.enc.Encode(pcm, data)
	if err != nil {
		return 0, fmt.Errorf("opus encode: %w", err)
	}

	return n, nil
}

type opusDecoder struct {
	dec *opus.Decoder
}

// Decode decodes an Opus-encoded packet into PCM int16 samples and returns the
// number of samples written per channel. Passing nil data triggers Opus Packet
// Loss Concealment (PLC), filling pcm with a synthesized replacement frame.
func (o *opusDecoder) Decode(data []byte, pcm []int16) (int, error) {
	n, err := o.dec.Decode(data, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus decode: %w", err)
	}

	return n, nil
}

// DecodeFloat32 decodes an Opus-encoded packet directly into float32 PCM
// samples, skipping the int16 intermediate stage, and returns the number of
// samples written per channel. Passing nil data triggers Opus Packet Loss
// Concealment (PLC) via DecodePLCFloat32, filling pcm with a synthesized
// replacement frame and returning len(pcm) as the sample count.
func (o *opusDecoder) DecodeFloat32(data []byte, pcm []float32) (int, error) {
	if data == nil {
		if err := o.dec.DecodePLCFloat32(pcm); err != nil {
			return 0, fmt.Errorf("opus plc float32: %w", err)
		}

		return len(pcm), nil
	}

	n, err := o.dec.DecodeFloat32(data, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus decode float32: %w", err)
	}

	return n, nil
}

// NewOpusEncoder creates and configures a new Opus audio encoder optimized for
// voice over IP (VoIP) applications. All tunable parameters are passed
// explicitly so this package has no dependency on comms-level constants.
func NewOpusEncoder(sampleRate, channels, targetBitrate, complexity, packetLossPerc int) (AudioEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("opus encoder: %w", err)
	}

	if err := enc.SetBitrate(targetBitrate); err != nil {
		return nil, fmt.Errorf("opus SetBitrate: %w", err)
	}

	if err := enc.SetComplexity(complexity); err != nil {
		return nil, fmt.Errorf("opus SetComplexity: %w", err)
	}

	if err := enc.SetInBandFEC(true); err != nil {
		return nil, fmt.Errorf("opus SetInBandFEC: %w", err)
	}

	if err := enc.SetPacketLossPerc(packetLossPerc); err != nil {
		return nil, fmt.Errorf("opus SetPacketLossPerc: %w", err)
	}

	if err := enc.SetDTX(false); err != nil {
		return nil, fmt.Errorf("opus SetDTX: %w", err)
	}

	return &opusEncoder{enc: enc}, nil
}

// NewOpusDecoder creates an Opus decoder configured for the given sample rate
// and channel count. Returns an AudioDecoder interface wrapping the decoder,
// or an error if the underlying opus.Decoder could not be constructed.
func NewOpusDecoder(sampleRate, channels int) (AudioDecoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("opus decoder: %w", err)
	}

	return &opusDecoder{dec: dec}, nil
}
