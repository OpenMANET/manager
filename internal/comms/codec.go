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
}

// ─── Opus implementation ──────────────────────────────────────────────────────

type opusEncoder struct {
	enc *opus.Encoder
}

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

func (o *opusDecoder) Decode(data []byte, pcm []int16) (int, error) {
	n, err := o.dec.Decode(data, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus decode: %w", err)
	}

	return n, nil
}

// newOpusEncoder creates an Opus encoder configured for low-latency voice.
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

	if err := enc.SetInBandFEC(false); err != nil {
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

// newOpusDecoder creates an Opus decoder for 48 kHz mono.
func newOpusDecoder() (AudioDecoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("opus decoder: %w", err)
	}

	return &opusDecoder{dec: dec}, nil
}
