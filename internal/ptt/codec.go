package ptt

import (
	"fmt"

	"github.com/hraban/opus"
)

// AudioEncoder encodes int16 PCM samples into a compressed byte slice.
// A thin interface over *opus.Encoder to allow test doubles.
type AudioEncoder interface {
	Encode(pcm []int16, data []byte) (int, error)
}

// AudioDecoder decodes a compressed byte slice back to int16 PCM samples.
// Passing a nil data slice triggers Opus Packet Loss Concealment (PLC).
type AudioDecoder interface {
	Decode(data []byte, pcm []int16) (int, error)
}

// opusEncoder wraps *opus.Encoder to satisfy AudioEncoder.
type opusEncoder struct{ enc *opus.Encoder }

func (o *opusEncoder) Encode(pcm []int16, data []byte) (int, error) {
	n, err := o.enc.Encode(pcm, data)
	if err != nil {
		return 0, fmt.Errorf("opus encode: %w", err)
	}

	return n, nil
}

// opusDecoder wraps *opus.Decoder to satisfy AudioDecoder.
type opusDecoder struct{ dec *opus.Decoder }

func (o *opusDecoder) Decode(data []byte, pcm []int16) (int, error) {
	n, err := o.dec.Decode(data, pcm)
	if err != nil {
		return 0, fmt.Errorf("opus decode: %w", err)
	}

	return n, nil
}

// newOpusEncoder creates an Opus encoder preconfigured with the PTT codec parameters.
func newOpusEncoder() (AudioEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, fmt.Errorf("create opus encoder: %w", err)
	}

	if err := enc.SetBitrate(targetBitrate); err != nil {
		return nil, fmt.Errorf("set opus bitrate: %w", err)
	}

	if err := enc.SetComplexity(encoderComplexity); err != nil {
		return nil, fmt.Errorf("set opus complexity: %w", err)
	}

	if err := enc.SetInBandFEC(false); err != nil {
		return nil, fmt.Errorf("set opus in-band FEC: %w", err)
	}

	if err := enc.SetPacketLossPerc(packetLossPerc); err != nil {
		return nil, fmt.Errorf("set opus packet loss percentage: %w", err)
	}

	if err := enc.SetDTX(false); err != nil {
		return nil, fmt.Errorf("set opus DTX: %w", err)
	}

	return &opusEncoder{enc: enc}, nil
}

// newOpusDecoder creates an Opus decoder for the PTT codec parameters.
func newOpusDecoder() (AudioDecoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("create opus decoder: %w", err)
	}

	return &opusDecoder{dec: dec}, nil
}
