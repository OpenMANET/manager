package ptt

import "github.com/hraban/opus"

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
	return o.enc.Encode(pcm, data)
}

// opusDecoder wraps *opus.Decoder to satisfy AudioDecoder.
type opusDecoder struct{ dec *opus.Decoder }

func (o *opusDecoder) Decode(data []byte, pcm []int16) (int, error) {
	return o.dec.Decode(data, pcm)
}

// newOpusEncoder creates an Opus encoder preconfigured with the PTT codec parameters.
func newOpusEncoder() (AudioEncoder, error) {
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		return nil, err
	}
	if err := enc.SetBitrate(targetBitrate); err != nil {
		return nil, err
	}
	if err := enc.SetComplexity(encoderComplexity); err != nil {
		return nil, err
	}
	if err := enc.SetInBandFEC(false); err != nil {
		return nil, err
	}
	if err := enc.SetPacketLossPerc(packetLossPerc); err != nil {
		return nil, err
	}
	if err := enc.SetDTX(false); err != nil {
		return nil, err
	}
	return &opusEncoder{enc: enc}, nil
}

// newOpusDecoder creates an Opus decoder for the PTT codec parameters.
func newOpusDecoder() (AudioDecoder, error) {
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		return nil, err
	}
	return &opusDecoder{dec: dec}, nil
}
