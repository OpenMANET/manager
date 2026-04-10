package codec_test

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func TestOpusDecodeFloat32_RoundTrip(t *testing.T) {
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

	pcmOut := make([]float32, testFrameSize)

	decoded, err := dec.DecodeFloat32(encoded[:n], pcmOut)
	if err != nil {
		t.Fatalf("DecodeFloat32: %v", err)
	}

	if decoded != testFrameSize {
		t.Errorf("DecodeFloat32 returned %d samples, want %d", decoded, testFrameSize)
	}

	// Decoded float32 samples must lie within the [-1, 1] normalized range.
	for i, v := range pcmOut {
		if v < -1.0 || v > 1.0 {
			t.Fatalf("pcmOut[%d] = %v out of [-1, 1] range", i, v)
		}
	}
}

func TestOpusDecodeFloat32_PLC(t *testing.T) {
	dec := newDec(t)
	defer dec.Close()

	pcmOut := make([]float32, testFrameSize)

	// nil data triggers Packet Loss Concealment.
	n, err := dec.DecodeFloat32(nil, pcmOut)
	if err != nil {
		t.Fatalf("DecodeFloat32(nil) PLC: %v", err)
	}

	if n != testFrameSize {
		t.Errorf("PLC decoded %d samples, want %d", n, testFrameSize)
	}
}

func TestOpusDecodeFloat32_RejectsEmptyDst(t *testing.T) {
	dec := newDec(t)
	defer dec.Close()

	if _, err := dec.DecodeFloat32(nil, nil); err == nil {
		t.Error("DecodeFloat32(nil dst) should error")
	}

	if _, err := dec.DecodeFloat32(nil, []float32{}); err == nil {
		t.Error("DecodeFloat32(empty dst) should error")
	}
}

func TestOpusDecodeFloat32_DecodeError(t *testing.T) {
	dec := newDec(t)
	defer dec.Close()

	// An empty (non-nil) byte slice is a length-0 packet, which Opus
	// rejects with "buffer too small". This exercises the error-wrapping
	// branch in DecodeFloat32 without faking the underlying decoder.
	pcmOut := make([]float32, testFrameSize)

	if _, err := dec.DecodeFloat32([]byte{}, pcmOut); err == nil {
		t.Error("DecodeFloat32(empty payload) should return an error")
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

// TestOpusEncoder_SetPacketLossPerc verifies the runtime retune path
// accepts valid values and produces usable output after each change.
func TestOpusEncoder_SetPacketLossPerc(t *testing.T) {
	enc := newEnc(t)

	for _, perc := range []int{0, 10, 20, 30, 40, 100} {
		if err := enc.SetPacketLossPerc(perc); err != nil {
			t.Fatalf("SetPacketLossPerc(%d): %v", perc, err)
		}

		pcm := make([]int16, testFrameSize)
		buf := make([]byte, 4000)

		n, err := enc.EncodeS16(pcm, buf)
		if err != nil {
			t.Fatalf("EncodeS16 after SetPacketLossPerc(%d): %v", perc, err)
		}

		if n <= 0 {
			t.Fatalf("EncodeS16 after SetPacketLossPerc(%d) returned %d bytes", perc, n)
		}
	}
}

// TestOpusEncoder_SetPacketLossPerc_OutOfRange verifies that libopus's
// own validation surfaces through as a wrapped error.
func TestOpusEncoder_SetPacketLossPerc_OutOfRange(t *testing.T) {
	enc := newEnc(t)

	for _, perc := range []int{-1, 101, 500} {
		if err := enc.SetPacketLossPerc(perc); err == nil {
			t.Errorf("SetPacketLossPerc(%d) = nil, want error", perc)
		}
	}
}

// TestOpusEncoder_SetPacketLossPerc_Concurrent exercises the mutex added
// to opusEncoder to guard EncodeS16 against concurrent SetPacketLossPerc
// calls. Fails under `go test -race` if the serialization is wrong.
func TestOpusEncoder_SetPacketLossPerc_Concurrent(t *testing.T) {
	enc := newEnc(t)

	var (
		wg       sync.WaitGroup
		stop     atomic.Bool
		encodeN  atomic.Int64
		setPercN atomic.Int64
	)

	// Four encode goroutines.
	for range 4 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			pcm := make([]int16, testFrameSize)
			buf := make([]byte, 4000)

			for !stop.Load() {
				if _, err := enc.EncodeS16(pcm, buf); err != nil {
					t.Errorf("concurrent EncodeS16: %v", err)

					return
				}

				encodeN.Add(1)
			}
		}()
	}

	// One retune goroutine cycling through levels.
	wg.Add(1)

	go func() {
		defer wg.Done()

		levels := []int{10, 20, 30, 40}
		i := 0

		for !stop.Load() {
			if err := enc.SetPacketLossPerc(levels[i%len(levels)]); err != nil {
				t.Errorf("concurrent SetPacketLossPerc: %v", err)

				return
			}

			setPercN.Add(1)

			i++
		}
	}()

	time.Sleep(200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()

	if encodeN.Load() == 0 {
		t.Error("expected at least one EncodeS16 call")
	}

	if setPercN.Load() == 0 {
		t.Error("expected at least one SetPacketLossPerc call")
	}
}
