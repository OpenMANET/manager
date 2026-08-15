package webaudio

import (
	"testing"

	"github.com/rs/zerolog"
)

// BenchmarkWebDrain measures the per-frame cost of delivering one Opus
// payload from the web playout drain to the RPC consumer: the defensive
// copy webPlayoutLoop makes so the jitter buffer's pooled payload can be
// released, the bridge hand-off, and the consumer receive.
func BenchmarkWebDrain(b *testing.B) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	payload := make([]byte, 100) // typical Opus 20 ms frame

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		// Mirror the webPlayoutLoop drain: copy the pooled payload to a
		// heap slice, then offer it to the bridge.
		cp := make([]byte, len(payload))
		copy(cp, payload)
		bridge.PushRxFrame(cp)

		<-bridge.RxFrames()
	}
}
