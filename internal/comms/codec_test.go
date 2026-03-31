package comms

import (
	"math"
	"testing"
)

func TestNewOpusEncoder_Succeeds(t *testing.T) {
	enc, err := newOpusEncoder(encoderComplexity)
	if err != nil {
		t.Fatalf("newOpusEncoder error: %v", err)
	}

	if enc == nil {
		t.Fatal("expected non-nil encoder")
	}
}

func TestNewOpusDecoder_Succeeds(t *testing.T) {
	dec, err := newOpusDecoder()
	if err != nil {
		t.Fatalf("newOpusDecoder error: %v", err)
	}

	if dec == nil {
		t.Fatal("expected non-nil decoder")
	}
}

func TestOpusEncode_ProducesOutput(t *testing.T) {
	enc, err := newOpusEncoder(encoderComplexity)
	if err != nil {
		t.Fatal(err)
	}

	pcm := make([]int16, frameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
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
	enc, err := newOpusEncoder(encoderComplexity)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := newOpusDecoder()
	if err != nil {
		t.Fatal(err)
	}

	// Generate a 440 Hz sine wave.
	pcmIn := make([]int16, frameSize)
	for i := range pcmIn {
		pcmIn[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
	}

	encoded := make([]byte, 4000)

	n, err := enc.Encode(pcmIn, encoded)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if n <= 0 {
		t.Fatal("Encode produced 0 bytes")
	}

	pcmOut := make([]int16, frameSize)

	decoded, err := dec.Decode(encoded[:n], pcmOut)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if decoded != frameSize {
		t.Errorf("decoded %d samples, want %d", decoded, frameSize)
	}
}

func TestOpusDecodeConsecutiveFrames(t *testing.T) {
	// Verify that the encoder and decoder handle multiple sequential calls
	// correctly — frame boundaries and state are maintained between calls.
	enc, err := newOpusEncoder(encoderComplexity)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := newOpusDecoder()
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 4000)
	pcmIn := make([]int16, frameSize)
	pcmOut := make([]int16, frameSize)

	for frame := range 3 {
		// Use a different constant value per frame so we exercise distinct inputs.
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

		if decoded != frameSize {
			t.Errorf("frame %d: decoded %d samples, want %d", frame, decoded, frameSize)
		}
	}
}
