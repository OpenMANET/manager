package comms

import (
	"math"
	"testing"
	"time"

	pionrtp "github.com/pion/rtp"
	"github.com/rs/zerolog"
)

// ─── Codec benchmarks ────────────────────────────────────────────────────────

func BenchmarkEncodeOpus(b *testing.B) {
	enc, err := newOpusEncoder()
	if err != nil {
		b.Fatal(err)
	}

	pcm := make([]int16, frameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
	}

	buf := make([]byte, 1500)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := enc.Encode(pcm, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeOpus(b *testing.B) {
	enc, err := newOpusEncoder()
	if err != nil {
		b.Fatal(err)
	}

	dec, err := newOpusDecoder()
	if err != nil {
		b.Fatal(err)
	}

	pcm := make([]int16, frameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
	}

	buf := make([]byte, 1500)

	n, err := enc.Encode(pcm, buf)
	if err != nil {
		b.Fatal(err)
	}

	encoded := make([]byte, n)
	copy(encoded, buf[:n])

	out := make([]int16, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := dec.Decode(encoded, out); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Jitter buffer benchmarks ────────────────────────────────────────────────

func BenchmarkJitterPush(b *testing.B) {
	payload := make([]byte, 100) // typical Opus frame size

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		jb := newRTPJitterBuffer(3, jitterMaxDepth)
		for i := range uint16(jitterMaxDepth) {
			jb.push(i, payload)
		}
	}
}

func BenchmarkJitterPushPop(b *testing.B) {
	payload := make([]byte, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		jb := newRTPJitterBuffer(1, jitterMaxDepth)
		for i := range uint16(jitterMaxDepth) {
			jb.push(i, payload)
			jb.popReady()
		}
	}
}

func BenchmarkJitterPopReady(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		jb := newRTPJitterBuffer(1, jitterMaxDepth)
		for i := range uint16(jitterMaxDepth) {
			jb.push(i, make([]byte, 100))
		}

		for range jitterMaxDepth {
			jb.popReady()
		}
	}
}

// ─── RTP parse benchmark ─────────────────────────────────────────────────────

func BenchmarkParseIncomingRTP(b *testing.B) {
	orig := &pionrtp.Packet{
		Header: pionrtp.Header{
			Version:        2,
			PayloadType:    rtpPayloadTypeOpus,
			SequenceNumber: 42,
			Timestamp:      1000,
			SSRC:           0xDEADBEEF,
		},
		Payload: make([]byte, 100),
	}

	raw, err := orig.Marshal()
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := parseIncomingRTP(raw); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Conversion benchmarks ───────────────────────────────────────────────────

func BenchmarkFloat32ToInt16(b *testing.B) {
	in := make([]float32, frameSize)
	for i := range in {
		in[i] = float32(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate))) * 0.9
	}

	out := make([]int16, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for i, v := range in {
			if v > 1.0 {
				v = 1.0
			} else if v < -1.0 {
				v = -1.0
			}

			out[i] = int16(v * 32767) //nolint:gosec
		}
	}
}

func BenchmarkInt16ToFloat32(b *testing.B) {
	in := make([]int16, frameSize)
	for i := range in {
		in[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
	}

	out := make([]float32, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		for i := range len(in) {
			out[i] = float32(in[i]) / 32768
		}
	}
}

// ─── decodeAndQueue benchmark ────────────────────────────────────────────────

func BenchmarkDecodeAndQueue(b *testing.B) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	rt := &CommsRuntime{
		playbackBuffer: make(chan []float32, 64),
		decoder:        &mockDecoder{returnN: frameSize},
	}

	payload := make([]byte, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		cfg.decodeAndQueue(rt, payload)
		// Drain the buffer to avoid backpressure.
		select {
		case <-rt.playbackBuffer:
		default:
		}
	}
}

// ─── popOrConceal benchmark ──────────────────────────────────────────────────

func BenchmarkPopOrConceal(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		jb := newRTPJitterBuffer(1, jitterMaxDepth)
		jb.push(0, make([]byte, 100))
		jb.popReady() // start the buffer

		// Push a few packets so popOrConceal has work to do.
		for i := uint16(1); i <= 5; i++ {
			jb.push(i, make([]byte, 100))
		}

		for range 5 {
			jb.popOrConceal(100 * time.Millisecond)
		}
	}
}
