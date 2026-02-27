//go:build !omd_omit_comms

package comms

import (
	"sync"
	"time"
)

const (
	jitterPrebufferPackets = 3
	jitterMaxDepth         = 24
)

// rtpJitterBuffer is a sequence-number-ordered buffer for RTP audio payloads.
// It provides prebuffering, late-packet dropping, and gap detection for PLC.
// Keys are RTP uint16 sequence numbers using wrap-around safe comparison.
type rtpJitterBuffer struct {
	lastPush  time.Time
	frames    map[uint16][]byte
	prebuffer int
	maxDepth  int
	mu        sync.Mutex
	expected  uint16
	init      bool
	started   bool
}

func newRTPJitterBuffer(prebuffer, maxDepth int) *rtpJitterBuffer {
	return &rtpJitterBuffer{
		frames:    make(map[uint16][]byte),
		prebuffer: prebuffer,
		maxDepth:  maxDepth,
	}
}

// seqLess compares RTP sequence numbers with uint16 wrap-around awareness.
func seqLess(a, b uint16) bool {
	return int16(a-b) < 0
}

// push stores a received payload keyed by sequence number.
// Returns false if the packet is stale, a duplicate, or the buffer is full.
func (jb *rtpJitterBuffer) push(seq uint16, payload []byte) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.init {
		jb.expected = seq
		jb.init = true
	}

	// Drop packets that are older than the current playout cursor.
	if seqLess(seq, jb.expected) {
		return false
	}

	if _, exists := jb.frames[seq]; exists {
		return false
	}

	if len(jb.frames) >= jb.maxDepth {
		return false
	}

	copied := make([]byte, len(payload))
	copy(copied, payload)
	jb.frames[seq] = copied
	jb.lastPush = time.Now()

	return true
}

// popReady returns the next in-order payload when available.
//
// skippedMissing is true when the buffer advances past a gap (caller should
// apply PLC for the skipped frame). ready is true when a payload is returned.
func (jb *rtpJitterBuffer) popReady() (payload []byte, ready bool, skippedMissing bool) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.init {
		return nil, false, false
	}

	if !jb.started {
		if len(jb.frames) < jb.prebuffer {
			return nil, false, false
		}

		jb.started = true
	}

	if payload, ok := jb.frames[jb.expected]; ok {
		delete(jb.frames, jb.expected)

		jb.expected++

		return payload, true, false
	}

	// If we've buffered a lot and still don't have the expected packet, skip it.
	if len(jb.frames) >= jb.maxDepth/2 {
		jb.expected++

		return nil, false, true
	}

	return nil, false, false
}

// shouldConceal returns true when a packet arrived recently enough that PLC
// is appropriate for a missing frame (i.e. the stream is active but gapped).
func (jb *rtpJitterBuffer) shouldConceal(recentWindow time.Duration) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	if !jb.started || jb.lastPush.IsZero() {
		return false
	}

	return time.Since(jb.lastPush) <= recentWindow
}

// advancePast discards the current expected sequence number and advances the
// playout cursor by one. Call this after emitting a PLC frame so that the
// late original packet (if it arrives later) is treated as stale and dropped,
// maintaining the invariant of exactly one frame produced per playout tick.
func (jb *rtpJitterBuffer) advancePast() {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	delete(jb.frames, jb.expected)
	jb.expected++
}
