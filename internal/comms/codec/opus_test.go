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
