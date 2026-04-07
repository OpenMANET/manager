package rtp

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	// PrebufferPackets is the number of frames the jitter buffer must
	// hold before playout begins. Each frame is 20 ms, so 5 packets ≈ 100 ms.
	// This sets the minimum receive latency floor and the safety margin for
	// arrival jitter on the mesh. 100 ms tolerates roughly one full packet
	// of late arrival before the buffer empties and playout falls into PLC.
	PrebufferPackets = 5
	MaxDepth         = 24

	// MaxOpusPayloadSize is the RFC 6716 §3.2.1 maximum encoded frame size.
	// Payload pool buffers are sized to this capacity to eliminate per-packet
	// heap allocations in the jitter buffer hot path.
	MaxOpusPayloadSize = 1275

	// jitterIdleResetThreshold is the inter-arrival gap after which the jitter
	// buffer treats the next packet as a fresh stream and re-initializes its
	// expected sequence cursor. This is a defensive safety net for cases where
	// SSRC tracking misses an edge (e.g. RFC 3550 §8.2 collision-driven SSRC
	// rotation, or a sender that resets seq without rotating SSRC). Two seconds
	// is well beyond any realistic jitter window and well below user-noticeable.
	jitterIdleResetThreshold = 2 * time.Second
)

// jitterSlot is one entry in the fixed-size ring buffer.
type jitterSlot struct {
	payload []byte
	seq     uint16
	valid   bool
}

// JitterBuffer is a sequence-number-ordered buffer for RTP audio payloads.
// It provides prebuffering, late-packet dropping, gap detection for PLC, and
// SSRC-change/idle-gap detection so a new talker is never silently dropped
// because their starting sequence number lies in the "past half" of the
// previous talker's frozen sequence cursor.
//
// Internally, frames are stored in a fixed-size circular array indexed by
// (seq % maxDepth), eliminating all map allocations on the hot path.
type JitterBuffer struct {
	// now is injectable for deterministic idle-reset tests. nil → time.Now.
	now         func() time.Time
	payloadPool sync.Pool
	lastPush    time.Time
	slots       [MaxDepth]jitterSlot
	Overflows   atomic.Int64
	SSRCResets  atomic.Int64
	IdleResets  atomic.Int64
	count       int
	prebuffer   int
	maxDepth    int
	mu          sync.Mutex
	ssrc        uint32
	expected    uint16
	init        bool
	started     bool
	haveSSRC    bool
}

func NewJitterBuffer(prebuffer, maxDepth int) *JitterBuffer {
	jb := &JitterBuffer{
		prebuffer: prebuffer,
		maxDepth:  maxDepth,
	}

	jb.payloadPool.New = func() any {
		s := make([]byte, MaxOpusPayloadSize)

		return &s
	}

	return jb
}

// nowFn returns the current time, honoring an injected clock if set.
func (jb *JitterBuffer) nowFn() time.Time {
	if jb.now != nil {
		return jb.now()
	}

	return time.Now()
}

// seqLess compares RTP sequence numbers with uint16 wrap-around awareness.
func seqLess(a, b uint16) bool {
	return int16(a-b) < 0
}

// push stores a received payload keyed by sequence number.
// Returns false if the packet is stale, a duplicate, or the buffer is full.
//
// Deprecated: use pushWithSSRC. push is retained for tests that pre-date SSRC
// tracking; it treats every packet as belonging to a single anonymous stream.
func (jb *JitterBuffer) Push(seq uint16, payload []byte) bool {
	return jb.PushWithSSRC(0, seq, payload, nil)
}

// pushWithSSRC stores a received payload, tracking the SSRC of the source
// stream. When the SSRC changes mid-stream, the buffer is reset and re-
// initialized from the new packet — without this, a new talker whose starting
// sequence number happens to lie in the "past half" of the previous talker's
// frozen cursor would be silently rejected forever.
//
// If onSSRCChange is non-nil, it is invoked (without holding jb.mu) when an
// SSRC change is detected, with the old and new SSRC values. Pass nil if you
// don't need notification (e.g. tests).
func (jb *JitterBuffer) PushWithSSRC(ssrc uint32, seq uint16, payload []byte, onSSRCChange func(oldSSRC, newSSRC uint32)) bool {
	jb.mu.Lock()

	var (
		oldSSRC     uint32
		ssrcChanged bool
	)

	if jb.haveSSRC && jb.init && ssrc != jb.ssrc {
		oldSSRC = jb.ssrc
		ssrcChanged = true

		jb.resetLocked()
		jb.SSRCResets.Add(1)
	}

	ok := jb.pushLocked(seq, payload)
	if !ok && jb.count >= jb.maxDepth {
		jb.Overflows.Add(1)
	}

	if ok {
		jb.ssrc = ssrc
		jb.haveSSRC = true
	}

	jb.mu.Unlock()

	if ssrcChanged && onSSRCChange != nil {
		onSSRCChange(oldSSRC, ssrc)
	}

	return ok
}

// pushLocked is the internal push implementation; caller must hold jb.mu.
func (jb *JitterBuffer) pushLocked(seq uint16, payload []byte) bool {
	// Idle-reset safety net: if a long gap has elapsed since the last push,
	// treat the next packet as the start of a fresh stream regardless of
	// sequence number. Catches edge cases the SSRC check cannot, e.g. a sender
	// that resets seq without rotating SSRC, or RFC 3550 §8.2 collision-driven
	// SSRC rotation that the caller did not propagate.
	if jb.init && !jb.lastPush.IsZero() && jb.nowFn().Sub(jb.lastPush) > jitterIdleResetThreshold {
		jb.resetLocked()
		jb.IdleResets.Add(1)
	}

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

	// Overwrite a stale slot, returning its buffer to the pool first.
	if slot.valid && slot.seq != seq {
		jb.ReleasePayload(slot.payload)
		jb.count--
	}

	bufPtr := jb.payloadPool.Get().(*[]byte) //nolint:forcetypeassert
	buf := (*bufPtr)[:len(payload)]
	copy(buf, payload)

	slot.seq = seq
	slot.payload = buf
	slot.valid = true
	jb.count++
	jb.lastPush = jb.nowFn()

	return true
}

// popReady returns the next in-order payload when available.
//
// skippedMissing is true when the buffer advances past a gap (caller should
// apply PLC for the skipped frame). ready is true when a payload is returned.
func (jb *JitterBuffer) PopReady() (payload []byte, ready bool, skippedMissing bool) {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	return jb.popReadyLocked()
}

// popReadyLocked is the internal pop implementation; caller must hold jb.mu.
func (jb *JitterBuffer) popReadyLocked() (payload []byte, ready bool, skippedMissing bool) {
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
func (jb *JitterBuffer) ShouldConceal(recentWindow time.Duration) bool {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	return jb.shouldConcealLocked(recentWindow)
}

// shouldConcealLocked is the lock-free internal implementation.
func (jb *JitterBuffer) shouldConcealLocked(recentWindow time.Duration) bool {
	if !jb.started || jb.lastPush.IsZero() {
		return false
	}

	return time.Since(jb.lastPush) <= recentWindow
}

// advancePast discards the current expected sequence number and advances the
// playout cursor by one.
func (jb *JitterBuffer) AdvancePast() {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.advancePastLocked()
}

// advancePastLocked is the lock-free internal implementation.
func (jb *JitterBuffer) advancePastLocked() {
	idx := int(jb.expected) % jb.maxDepth
	slot := &jb.slots[idx]

	if slot.valid && slot.seq == jb.expected {
		jb.ReleasePayload(slot.payload)
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
func (jb *JitterBuffer) PopOrConceal(recentWindow time.Duration) (payload []byte, conceal bool) { //nolint:unparam
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

// reset clears all buffered state so the jitter buffer can be reused for a
// new RTP stream (e.g. after a talk-group switch). The next push will
// re-initialize the expected sequence number from the first arriving packet.
func (jb *JitterBuffer) Reset() {
	jb.mu.Lock()
	defer jb.mu.Unlock()

	jb.resetLocked()
	jb.haveSSRC = false
	jb.ssrc = 0
}

// resetLocked is the internal reset implementation; caller must hold jb.mu.
// It does NOT clear the SSRC tracking fields — that is the caller's choice
// (e.g. pushWithSSRC overwrites jb.ssrc with the new value after reset).
func (jb *JitterBuffer) resetLocked() {
	for i := range jb.slots {
		if jb.slots[i].valid {
			jb.ReleasePayload(jb.slots[i].payload)
		}

		jb.slots[i] = jitterSlot{}
	}

	jb.count = 0
	jb.expected = 0
	jb.init = false
	jb.started = false
	jb.lastPush = time.Time{}
}

// releasePayload returns a jitter-buffer payload slice back to the pool.
// Only pool-allocated slices (cap == MaxOpusPayloadSize) are accepted;
// anything else (test slices, nil) is silently ignored.
func (jb *JitterBuffer) ReleasePayload(p []byte) {
	if cap(p) != MaxOpusPayloadSize {
		return
	}

	full := p[:cap(p)]
	jb.payloadPool.Put(&full)
}
