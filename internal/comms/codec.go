package comms

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

// newOpusEncoder creates and configures a new Opus audio encoder optimized for
// voice over IP (VoIP) applications.
//
// The encoder is configured with the following settings:
//   - Sample rate: [sampleRate] Hz
//   - Channels: [channels]
//   - Application: opus.AppVoIP for voice optimization
//   - Bitrate: [targetBitrate] bps
//   - Complexity: [encoderComplexity] (higher values = better quality, more CPU usage)
//   - In-Band FEC: disabled (no forward error correction)
//   - Packet Loss Percentage: [packetLossPerc]%
//   - DTX: disabled (discontinuous transmission)
//
// Returns an AudioEncoder interface wrapping the configured Opus encoder,
// or an error if the encoder could not be created or configured.
//
// Possible errors:
//   - "opus encoder": failed to create the base Opus encoder
//   - "opus SetBitrate": failed to set the target bitrate
//   - "opus SetComplexity": failed to set the encoder complexity
//   - "opus SetInBandFEC": failed to disable in-band FEC
//   - "opus SetPacketLossPerc": failed to set the packet loss percentage
//   - "opus SetDTX": failed to disable discontinuous transmission
func newOpusEncoder() (AudioEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("opus encoder: %w", err)
	}

	if err := enc.SetBitrate(targetBitrate); err != nil {
		return nil, fmt.Errorf("opus SetBitrate: %w", err)
	}

	if err := enc.SetComplexity(encoderComplexity); err != nil {
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

// newOpusDecoder creates an Opus decoder configured for [sampleRate] Hz and
// [channels] channels. Returns an AudioDecoder interface wrapping the decoder,
// or an error if the underlying opus.Decoder could not be constructed.
func newOpusDecoder() (AudioDecoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("opus decoder: %w", err)
	}

	return &opusDecoder{dec: dec}, nil
}
