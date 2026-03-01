package comms

import (
	"sync"
	"time"
)

const (
	jitterPrebufferPackets = 3
	jitterMaxDepth         = 24
)

// jitterSlot is one entry in the fixed-size ring buffer.
type jitterSlot struct {
	payload []byte
	seq     uint16
	valid   bool
}

// rtpJitterBuffer is a sequence-number-ordered buffer for RTP audio payloads.
// It provides prebuffering, late-packet dropping, and gap detection for PLC.
//
// Internally, frames are stored in a fixed-size circular array indexed by
// (seq % maxDepth), eliminating all map allocations on the hot path.
type rtpJitterBuffer struct {
	lastPush  time.Time
	slots     [jitterMaxDepth]jitterSlot
	count     int // number of valid slots
	prebuffer int
	maxDepth  int
	mu        sync.Mutex
	expected  uint16
	init      bool
	started   bool
}

func newRTPJitterBuffer(prebuffer, maxDepth int) *rtpJitterBuffer {
	return &rtpJitterBuffer{
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

	return jb.pushLocked(seq, payload)
}

// pushLocked is the internal push implementation; caller must hold jb.mu.
func (jb *rtpJitterBuffer) pushLocked(seq uint16, payload []byte) bool {
	if !jb.init {
		jb.expected = seq
		jb.init = true
	}

	// Drop packets that are older than the current playout cursor.
	if seqLess(seq, jb.expected) {
		return false
	}

	idx := int(seq) % jb.maxDepth
	slot := &jb.slots[idx]

	// Duplicate: same seq already stored in this slot.
	if slot.valid && slot.seq == seq {
		return false
	}

	if jb.count >= jb.maxDepth && !slot.valid {
		return false
	}

	// Overwrite a stale slot.
	if slot.valid && slot.seq != seq {
		jb.count--
	}

	copied := make([]byte, len(payload))
	copy(copied, payload)

	slot.seq = seq
	slot.payload = copied
	slot.valid = true
	jb.count++
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

	return jb.popReadyLocked()
}

// popReadyLocked is the internal pop implementation; caller must hold jb.mu.
func (jb *rtpJitterBuffer) popReadyLocked() (payload []byte, ready bool, skippedMissing bool) {
	if !jb.init {
		return nil, false, false
	}

	if !jb.started {
		if jb.count < jb.prebuffer {
			return nil, false, false
		}

		jb.started = true
	}

	idx := int(jb.expected) % jb.maxDepth
	slot := &jb.slots[idx]

	if slot.valid && slot.seq == jb.expected {
		p := slot.payload
		slot.payload = nil
		slot.valid = false
		jb.count--

		jb.expected++

		return p, true, false
	}

	// If we've buffered a lot and still don't have the expected packet, skip it.
	if jb.count >= jb.maxDepth/2 {
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

	return jb.shouldConcealLocked(recentWindow)
}

// shouldConcealLocked is the lock-free internal implementation.
func (jb *rtpJitterBuffer) shouldConcealLocked(recentWindow time.Duration) bool {
	if !jb.started || jb.lastPush.IsZero() {
		return false
	}

	return time.Since(jb.lastPush) <= recentWindow
}

// advancePast discards the current expected sequence number and advances the
// playout cursor by one.
func (jb *rtpJitterBuffer) advancePast() {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.advancePastLocked()
}

// advancePastLocked is the lock-free internal implementation.
func (jb *rtpJitterBuffer) advancePastLocked() {
	idx := int(jb.expected) % jb.maxDepth
	slot := &jb.slots[idx]

	if slot.valid && slot.seq == jb.expected {
		slot.payload = nil
		slot.valid = false
		jb.count--
	}

	jb.expected++
}

// popOrConceal performs the full playout-tick logic under a single lock
// acquisition, eliminating the 3-lock-per-tick pattern previously used by
// playoutLoop (popReady + shouldConceal + advancePast).
//
// Returns:
//   - payload != nil: an in-order frame was available.
//   - conceal == true: no frame was available but the stream is active and PLC
//     should be applied. The playout cursor has already been advanced.
//   - both nil/false: no frame available and no concealment needed.
func (jb *rtpJitterBuffer) popOrConceal(recentWindow time.Duration) (payload []byte, conceal bool) { //nolint:unparam
	jb.mu.Lock()
	defer jb.mu.Unlock()

	p, ready, skipped := jb.popReadyLocked()
	if ready {
		return p, false
	}

	if skipped {
		return nil, true // caller should emit PLC
	}

	// Buffer empty — check if concealment is warranted.
	if jb.shouldConcealLocked(recentWindow) {
		jb.advancePastLocked()

		return nil, true
	}

	return nil, false
}
