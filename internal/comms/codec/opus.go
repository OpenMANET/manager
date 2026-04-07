// Package codec defines audio encoder/decoder interfaces and an Opus
// implementation. It is intentionally a leaf package: it depends only on
// libopus and exposes minimal interfaces so the comms package and its
// sub-packages can construct codecs without importing each other.
//
// Phase 5 of the comms refactor (see .claude/plans/comms-refactor.md)
// introduces int16-native hot-path entry points (EncodeS16 / DecodeS16)
// that take / return []int16 directly, eliminating float32↔int16
// conversions on mipsle softfloat targets. The legacy Encode / Decode /
// DecodeFloat32 methods are retained for migration and marked deprecated.
package codec

import (
	"errors"
	"fmt"

	"github.com/hraban/opus"
)

// AudioEncoder encodes PCM int16 samples into a compressed codec frame.
//
// EncodeS16 is the int16-native hot-path entry point. Encode is retained as
// a deprecated alias so legacy callers continue to compile; new code should
// call EncodeS16.
type AudioEncoder interface {
	// EncodeS16 encodes a frame of PCM int16 samples into the output buffer
	// out and returns the number of bytes written. The length of pcm must
	// match the configured frame size. out must be large enough to hold the
	// encoded output. Returns an error when pcm or out is nil / too short.
	EncodeS16(pcm []int16, out []byte) (int, error)

	// Encode is a deprecated alias for EncodeS16.
	//
	// Deprecated: use EncodeS16.
	Encode(pcm []int16, data []byte) (int, error)

	// Close releases underlying codec resources. Safe to call multiple times.
	Close() error
}

// AudioDecoder decodes a compressed codec frame back into PCM samples.
// Passing nil data to DecodeS16 or Decode triggers Opus Packet Loss
// Concealment (PLC).
type AudioDecoder interface {
	// DecodeS16 is the int16-native hot-path entry point. It decodes an
	// Opus packet directly into dst and returns the number of samples
	// written per channel. Passing nil data triggers Opus PLC. Returns an
	// error when dst is nil or too short to hold a frame.
	DecodeS16(data []byte, dst []int16) (int, error)

	// Decode is a deprecated alias for DecodeS16.
	//
	// Deprecated: use DecodeS16.
	Decode(data []byte, pcm []int16) (int, error)

	// DecodeFloat32 decodes directly into float32 PCM. Retained for callers
	// that must emit float32 at a consumer boundary (e.g. the web audio
	// bridge).
	//
	// Deprecated: convert at the consumer boundary; the hot path should use
	// DecodeS16.
	DecodeFloat32(data []byte, pcm []float32) (int, error)

	// Close releases underlying codec resources. Safe to call multiple times.
	Close() error
}

// ErrShortBuffer is returned when a caller-supplied PCM or output buffer is
// nil or too short for a single frame.
var ErrShortBuffer = errors.New("codec: short buffer")

// ─── Opus implementation ──────────────────────────────────────────────────────

type opusEncoder struct {
	enc *opus.Encoder
}

// EncodeS16 encodes a frame of PCM int16 samples into out and returns the
// number of bytes written. It is the int16-native hot-path entry point.
func (o *opusEncoder) EncodeS16(pcm []int16, out []byte) (int, error) {
	if len(pcm) == 0 || len(out) == 0 {
		return 0, ErrShortBuffer
	}

	n, err := o.enc.Encode(pcm, out)
	if err != nil {
		return 0, fmt.Errorf("opus encode: %w", err)
	}

	return n, nil
}

// Encode is a deprecated alias for EncodeS16.
//
// Deprecated: use EncodeS16.
func (o *opusEncoder) Encode(pcm []int16, data []byte) (int, error) {
	return o.EncodeS16(pcm, data)
}

// Close releases the underlying libopus encoder. The hraban/opus encoder
// does not expose an explicit Close; this is a no-op that returns nil so
// callers can safely close the interface.
func (o *opusEncoder) Close() error { return nil }

type opusDecoder struct {
	dec *opus.Decoder
}

// DecodeS16 decodes an Opus-encoded packet directly into dst and returns
// the number of samples written per channel. Passing nil data triggers
// Opus PLC.
func (o *opusDecoder) DecodeS16(data []byte, dst []int16) (int, error) {
	if len(dst) == 0 {
		return 0, ErrShortBuffer
	}

	if data == nil {
		if err := o.dec.DecodePLC(dst); err != nil {
			return 0, fmt.Errorf("opus plc: %w", err)
		}

		return len(dst), nil
	}

	n, err := o.dec.Decode(data, dst)
	if err != nil {
		return 0, fmt.Errorf("opus decode: %w", err)
	}

	return n, nil
}

// Decode is a deprecated alias for DecodeS16.
//
// Deprecated: use DecodeS16.
func (o *opusDecoder) Decode(data []byte, pcm []int16) (int, error) {
	return o.DecodeS16(data, pcm)
}

// DecodeFloat32 decodes directly into float32 PCM. Retained for consumer
// boundaries that require float32 output (e.g. the web audio bridge).
//
// Deprecated: use DecodeS16 on the hot path; convert at the boundary.
func (o *opusDecoder) DecodeFloat32(data []byte, pcm []float32) (int, error) {
	if len(pcm) == 0 {
		return 0, ErrShortBuffer
	}

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

// Close releases the underlying libopus decoder.
func (o *opusDecoder) Close() error { return nil }

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
