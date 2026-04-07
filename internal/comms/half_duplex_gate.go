package comms

import (
	"sync/atomic"
	"time"
)

// defaultHalfDuplexThreshold is the window after the last received remote
// RTP packet during which the channel is considered "actively receiving".
// Transmission is blocked while receiving is active.
const defaultHalfDuplexThreshold = 400 * time.Millisecond

// HalfDuplexGate tracks the timestamp of the most recent inbound RTP packet
// and reports whether the channel is currently within the half-duplex
// receive window. It is shared by the RX path (which calls Mark on every
// valid packet) and the TX path (which consults Active before opening a
// transmission). All operations are lock-free.
//
// The zero value is a usable gate with the default 400 ms threshold.
type HalfDuplexGate struct {
	last      atomic.Int64  // unix nanos of last mark; 0 = never marked
	Threshold time.Duration // 0 ⇒ defaultHalfDuplexThreshold
}

// Mark records that a remote packet was received now.
func (g *HalfDuplexGate) Mark() {
	g.last.Store(time.Now().UnixNano())
}

// MarkAt records a remote-receive timestamp explicitly. Test-only helper.
func (g *HalfDuplexGate) MarkAt(t time.Time) {
	g.last.Store(t.UnixNano())
}

// Reset clears the gate so Active reports false.
func (g *HalfDuplexGate) Reset() {
	g.last.Store(0)
}

// LastUnixNano returns the raw timestamp; 0 means never marked.
func (g *HalfDuplexGate) LastUnixNano() int64 {
	return g.last.Load()
}

// Active reports whether a remote packet was marked within the Threshold.
func (g *HalfDuplexGate) Active() bool {
	last := g.last.Load()
	if last == 0 {
		return false
	}

	threshold := g.Threshold
	if threshold <= 0 {
		threshold = defaultHalfDuplexThreshold
	}

	return time.Since(time.Unix(0, last)) < threshold
}
