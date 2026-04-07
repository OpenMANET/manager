package codec_test

import (
	"math"
	"testing"

	"github.com/openmanet/openmanetd/internal/comms/codec"
)

const (
	testSampleRate     = 48000
	testChannels       = 1
	testFrameSize      = 960
	testBitrate        = 32000
	testComplexity     = 10
	testPacketLossPerc = 20
)

func newEnc(t *testing.T) codec.AudioEncoder {
	t.Helper()

	enc, err := codec.NewOpusEncoder(testSampleRate, testChannels, testBitrate, testComplexity, testPacketLossPerc)
	if err != nil {
		t.Fatalf("NewOpusEncoder error: %v", err)
	}

	return enc
}

func newDec(t *testing.T) codec.AudioDecoder {
	t.Helper()

	dec, err := codec.NewOpusDecoder(testSampleRate, testChannels)
	if err != nil {
		t.Fatalf("NewOpusDecoder error: %v", err)
	}

	return dec
}

func TestNewOpusEncoder_Succeeds(t *testing.T) {
	if enc := newEnc(t); enc == nil {
		t.Fatal("expected non-nil encoder")
	}
}

func TestNewOpusDecoder_Succeeds(t *testing.T) {
	if dec := newDec(t); dec == nil {
		t.Fatal("expected non-nil decoder")
	}
}

func TestOpusEncode_ProducesOutput(t *testing.T) {
	enc := newEnc(t)

	pcm := make([]int16, testFrameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(testSampleRate)) * 16000)
	}

	buf := make([]byte, 4000)

	n, err := enc.Encode(pcm, buf)
	if err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	if n <= 0 {
		t.Fatal("Encode produced 0 bytes")
	}
}

func TestOpusRoundTrip(t *testing.T) {
	enc := newEnc(t)
	dec := newDec(t)

	pcmIn := make([]int16, testFrameSize)
	for i := range pcmIn {
		pcmIn[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(testSampleRate)) * 16000)
	}

	encoded := make([]byte, 4000)

	n, err := enc.Encode(pcmIn, encoded)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if n <= 0 {
		t.Fatal("Encode produced 0 bytes")
	}

	pcmOut := make([]int16, testFrameSize)

	decoded, err := dec.Decode(encoded[:n], pcmOut)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded != testFrameSize {
		t.Errorf("decoded %d samples, want %d", decoded, testFrameSize)
	}
}

func TestOpusEncodeS16DecodeS16_RoundTrip(t *testing.T) {
	enc := newEnc(t)
	dec := newDec(t)

	defer enc.Close()
	defer dec.Close()

	pcmIn := make([]int16, testFrameSize)
	for i := range pcmIn {
		pcmIn[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(testSampleRate)) * 16000)
	}

	encoded := make([]byte, 4000)

	n, err := enc.EncodeS16(pcmIn, encoded)
	if err != nil {
		t.Fatalf("EncodeS16: %v", err)
	}

	if n <= 0 {
		t.Fatal("EncodeS16 produced 0 bytes")
	}

	pcmOut := make([]int16, testFrameSize)

	decoded, err := dec.DecodeS16(encoded[:n], pcmOut)
	if err != nil {
		t.Fatalf("DecodeS16: %v", err)
	}

	if decoded != testFrameSize {
		t.Errorf("decoded %d samples, want %d", decoded, testFrameSize)
	}

	// PLC path: passing nil payload triggers Packet Loss Concealment.
	plcOut := make([]int16, testFrameSize)

	plcN, plcErr := dec.DecodeS16(nil, plcOut)
	if plcErr != nil {
		t.Fatalf("DecodeS16 PLC: %v", plcErr)
	}

	if plcN != testFrameSize {
		t.Errorf("PLC decoded %d samples, want %d", plcN, testFrameSize)
	}
}

func TestOpusEncodeS16_RejectsShortBuffers(t *testing.T) {
	enc := newEnc(t)
	defer enc.Close()

	if _, err := enc.EncodeS16(nil, make([]byte, 4000)); err == nil {
		t.Error("EncodeS16(nil pcm) should error")
	}

	if _, err := enc.EncodeS16(make([]int16, testFrameSize), nil); err == nil {
		t.Error("EncodeS16(nil out) should error")
	}
}

func TestOpusDecodeS16_RejectsShortBuffers(t *testing.T) {
	dec := newDec(t)
	defer dec.Close()

	if _, err := dec.DecodeS16(nil, nil); err == nil {
		t.Error("DecodeS16(nil dst) should error")
	}
}

func TestOpusClose_Idempotent(t *testing.T) {
	enc := newEnc(t)
	dec := newDec(t)

	if err := enc.Close(); err != nil {
		t.Errorf("enc.Close: %v", err)
	}

	if err := enc.Close(); err != nil {
		t.Errorf("enc.Close (2nd): %v", err)
	}

	if err := dec.Close(); err != nil {
		t.Errorf("dec.Close: %v", err)
	}

	if err := dec.Close(); err != nil {
		t.Errorf("dec.Close (2nd): %v", err)
	}
}

func TestOpusDecodeConsecutiveFrames(t *testing.T) {
	enc := newEnc(t)
	dec := newDec(t)

	buf := make([]byte, 4000)
	pcmIn := make([]int16, testFrameSize)
	pcmOut := make([]int16, testFrameSize)

	for frame := range 3 {
		for i := range pcmIn {
			pcmIn[i] = int16(1000 * (frame + 1))
		}

		n, encErr := enc.Encode(pcmIn, buf)
		if encErr != nil {
			t.Fatalf("frame %d Encode: %v", frame, encErr)
		}

		decoded, decErr := dec.Decode(buf[:n], pcmOut)
		if decErr != nil {
			t.Fatalf("frame %d Decode: %v", frame, decErr)
		}

		if decoded != testFrameSize {
			t.Errorf("frame %d: decoded %d samples, want %d", frame, decoded, testFrameSize)
		}
	}
}
