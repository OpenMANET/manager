package comms

import (
	"sync/atomic"
	"time"
)

// defaultHalfDuplexThreshold is the window after the last received remote
// RTP packet during which the channel is considered "actively receiving".
// Transmission is blocked while receiving is active.
const defaultHalfDuplexThreshold = 400 * time.Millisecond

// halfDuplexGate tracks the timestamp of the most recent inbound RTP packet
// and reports whether the channel is currently within the half-duplex
// receive window. It is shared by the RX path (which calls mark on every
// valid packet) and the TX path (which consults active before opening a
// transmission). All operations are lock-free.
//
// The zero value is a usable gate with the default 400 ms threshold.
type halfDuplexGate struct {
	last      atomic.Int64  // unix nanos of last mark; 0 = never marked
	threshold time.Duration // 0 ⇒ defaultHalfDuplexThreshold
}

// mark records that a remote packet was received now.
func (g *halfDuplexGate) mark() {
	g.last.Store(time.Now().UnixNano())
}

// markAt records a remote-receive timestamp explicitly. Test-only helper.
func (g *halfDuplexGate) markAt(t time.Time) {
	g.last.Store(t.UnixNano())
}

// reset clears the gate so active reports false.
func (g *halfDuplexGate) reset() {
	g.last.Store(0)
}

// lastUnixNano returns the raw timestamp; 0 means never marked.
func (g *halfDuplexGate) lastUnixNano() int64 {
	return g.last.Load()
}

// active reports whether a remote packet was marked within the threshold.
func (g *halfDuplexGate) active() bool {
	last := g.last.Load()
	if last == 0 {
		return false
	}

	threshold := g.threshold
	if threshold <= 0 {
		threshold = defaultHalfDuplexThreshold
	}

	return time.Since(time.Unix(0, last)) < threshold
}
