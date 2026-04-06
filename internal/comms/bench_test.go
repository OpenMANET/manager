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
	enc, err := newOpusEncoder(encoderComplexity)
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

// BenchmarkDecodeOpus measures the Encode→Decode (int16) round-trip used by
// the send path.
func BenchmarkDecodeOpus(b *testing.B) {
	enc, err := newOpusEncoder(encoderComplexity)
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

	encoded := buf[:n]
	out := make([]int16, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := dec.Decode(encoded, out); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecodeOpusFloat32 measures the receive hot path: DecodeFloat32
// decodes directly to float32, skipping the int16 intermediate stage.
func BenchmarkDecodeOpusFloat32(b *testing.B) {
	enc, err := newOpusEncoder(encoderComplexity)
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

	encoded := buf[:n]
	out := make([]float32, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		if _, err := dec.DecodeFloat32(encoded, out); err != nil {
			b.Fatal(err)
		}
	}
}

// ─── Jitter buffer benchmarks ────────────────────────────────────────────────

// BenchmarkJitterPush measures steady-state push throughput with a warmed pool.
// The buffer is created once outside the loop so pool slots are reused across
// iterations, reflecting production behavior.
func BenchmarkJitterPush(b *testing.B) {
	payload := make([]byte, 100) // typical Opus frame size
	jb := newRTPJitterBuffer(3, jitterMaxDepth)

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		seq := uint16(i % jitterMaxDepth)
		jb.push(seq, payload)
	}
}

// BenchmarkJitterPushPop measures the push→pop cycle with pool recycling.
// releasePayload is called after each pop to return the buffer to the pool,
// mirroring what playoutLoop does in production.
func BenchmarkJitterPushPop(b *testing.B) {
	payload := make([]byte, 100)
	jb := newRTPJitterBuffer(1, jitterMaxDepth)

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		seq := uint16(i)
		jb.push(seq, payload)

		p, _, _ := jb.popReady()
		if p != nil {
			jb.releasePayload(p)
		}
	}
}

// BenchmarkJitterPopReady fills the buffer once, then measures sustained pop
// performance. Each popped payload is returned to the pool so subsequent pushes
// reuse the same allocations.
func BenchmarkJitterPopReady(b *testing.B) {
	payload := make([]byte, 100)
	jb := newRTPJitterBuffer(1, jitterMaxDepth)

	// Pre-fill to warm the pool and prime the playout cursor.
	for i := range uint16(jitterMaxDepth) {
		jb.push(i, payload)
	}

	b.ResetTimer()
	b.ReportAllocs()

	seq := uint16(jitterMaxDepth)

	for b.Loop() {
		p, _, _ := jb.popReady()
		if p != nil {
			jb.releasePayload(p)
		}

		jb.push(seq, payload)
		seq++
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

// BenchmarkFloat32ToInt16 measures the mic callback's float32→int16 conversion
// (broadcast / send path).
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

// ─── playoutOneFrame benchmarks ──────────────────────────────────────────────

// BenchmarkPlayoutOneFrame_Mock measures the playout primitive with a no-op
// decoder. The PCM output buffer is reused across iterations so 0 allocs/op
// confirms that the decode path is allocation-free.
func BenchmarkPlayoutOneFrame_Mock(b *testing.B) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	rt := &CommsRuntime{
		decoder: &mockDecoder{returnN: frameSize},
	}

	jb := newRTPJitterBuffer(1, jitterMaxDepth)
	payload := make([]byte, 100)
	out := make([]float32, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		// Push a fresh payload each iteration so the buffer never empties.
		jb.push(0, payload)
		cfg.playoutOneFrame(pc, rt, jb, out)
	}
}

// BenchmarkPlayoutOneFrame_Real measures playoutOneFrame using the real Opus
// decoder, confirming that DecodeFloat32 directly into a caller-supplied
// buffer yields 0 allocs/op.
func BenchmarkPlayoutOneFrame_Real(b *testing.B) {
	enc, err := newOpusEncoder(encoderComplexity)
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

	encBuf := make([]byte, 1500)

	n, err := enc.Encode(pcm, encBuf)
	if err != nil {
		b.Fatal(err)
	}

	encoded := encBuf[:n]

	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	rt := &CommsRuntime{decoder: dec}

	jb := newRTPJitterBuffer(1, jitterMaxDepth)
	out := make([]float32, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		jb.push(0, encoded)
		cfg.playoutOneFrame(pc, rt, jb, out)
	}
}

// ─── PLC decode benchmark ───────────────────────────────────────────────────

// BenchmarkPlayoutOneFrame_PLC measures the PLC decode path through
// playoutOneFrame using the real Opus decoder. The jitter buffer is set up
// so that every call hits the conceal branch and invokes DecodePLCFloat32.
func BenchmarkPlayoutOneFrame_PLC(b *testing.B) {
	enc, err := newOpusEncoder(encoderComplexity)
	if err != nil {
		b.Fatal(err)
	}

	dec, err := newOpusDecoder()
	if err != nil {
		b.Fatal(err)
	}

	// Feed the decoder one real frame first so PLC has state to work with.
	pcm := make([]int16, frameSize)
	for i := range pcm {
		pcm[i] = int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 16000)
	}

	encBuf := make([]byte, 1500)

	n, err := enc.Encode(pcm, encBuf)
	if err != nil {
		b.Fatal(err)
	}

	warmup := make([]float32, frameSize)
	if _, err := dec.DecodeFloat32(encBuf[:n], warmup); err != nil {
		b.Fatal(err)
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	rt := &CommsRuntime{decoder: dec}

	// Prime the jitter buffer with a single push+pop so started=true and
	// lastPush is recent; subsequent playoutOneFrame calls will hit conceal.
	jb := newRTPJitterBuffer(1, jitterMaxDepth)
	jb.push(0, encBuf[:n])
	jb.popReady()

	out := make([]float32, frameSize)

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		// Reset consecutivePLC each iteration so we always exercise the
		// PLC decode path rather than falling through to silence.
		pc.consecutivePLC = 0
		cfg.playoutOneFrame(pc, rt, jb, out)
	}
}

// ─── Burst loss conceal benchmark ───────────────────────────────────────────

// BenchmarkPopOrConceal_BurstLoss simulates a burst loss scenario: frames are
// pushed and consumed, then the buffer is left empty while the stream is still
// active (recent lastPush). Repeated popOrConceal calls exercise the
// shouldConceal path, measuring conceal decision throughput during burst loss.
func BenchmarkPopOrConceal_BurstLoss(b *testing.B) {
	payload := make([]byte, 100)
	jb := newRTPJitterBuffer(1, jitterMaxDepth)

	// Push and pop one frame to start the buffer and set lastPush.
	jb.push(0, payload)

	if p, _, _ := jb.popReady(); p != nil {
		jb.releasePayload(p)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		// Refresh lastPush to keep shouldConceal returning true.
		jb.push(1, payload)

		if p, _, _ := jb.popReady(); p != nil {
			jb.releasePayload(p)
		}

		// Simulate 5 consecutive conceal decisions on empty buffer.
		for range 5 {
			p, _ := jb.popOrConceal(100 * time.Millisecond)
			if p != nil {
				jb.releasePayload(p)
			}
		}
	}
}

// ─── popOrConceal benchmark ──────────────────────────────────────────────────

// BenchmarkPopOrConceal measures steady-state popOrConceal with a warmed pool.
// Setup (push) is done outside b.Loop(); releasePayload mirrors playoutLoop.
func BenchmarkPopOrConceal(b *testing.B) {
	payload := make([]byte, 100)
	jb := newRTPJitterBuffer(1, jitterMaxDepth)

	// Prime the playout cursor: push seq=0, pop it to start the buffer.
	jb.push(0, payload)

	if p, _, _ := jb.popReady(); p != nil {
		jb.releasePayload(p)
	}

	b.ReportAllocs()

	seq := uint16(1)

	for b.Loop() {
		// Keep 5 frames ahead of the playout cursor.
		for range 5 {
			jb.push(seq, payload)
			seq++
		}

		for range 5 {
			p, _ := jb.popOrConceal(100 * time.Millisecond)
			if p != nil {
				jb.releasePayload(p)
			}
		}
	}
}
