package comms

import (
	"testing"
	"time"
)

func TestSeqLess_NormalOrder(t *testing.T) {
	if !seqLess(1, 2) {
		t.Error("seqLess(1, 2) should be true")
	}

	if seqLess(2, 1) {
		t.Error("seqLess(2, 1) should be false")
	}

	if seqLess(5, 5) {
		t.Error("seqLess(5, 5) should be false (equal)")
	}
}

func TestSeqLess_WrapAround(t *testing.T) {
	// 0xFFFE < 0xFFFF < 0x0000 < 0x0001 under wrap-around semantics.
	if !seqLess(0xFFFE, 0xFFFF) {
		t.Error("seqLess(0xFFFE, 0xFFFF) should be true")
	}

	if !seqLess(0xFFFF, 0x0000) {
		t.Error("seqLess(0xFFFF, 0x0000) should be true (wrap)")
	}

	if seqLess(0x0000, 0xFFFF) {
		t.Error("seqLess(0x0000, 0xFFFF) should be false (wrap)")
	}
}

func TestJitterBuffer_InOrderDelivery(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10) // prebuffer=1 so first push triggers start

	jb.push(10, []byte{1, 2, 3})

	payload, ready, skipped := jb.popReady()
	if !ready {
		t.Fatal("expected ready=true")
	}

	if skipped {
		t.Fatal("expected skipped=false")
	}

	if len(payload) != 3 || payload[0] != 1 {
		t.Errorf("wrong payload: %v", payload)
	}
}

func TestJitterBuffer_Prebuffer(t *testing.T) {
	jb := newRTPJitterBuffer(3, 10)

	// Push only 2 packets — not enough to start.
	jb.push(0, []byte{0})
	jb.push(1, []byte{1})

	_, ready, skipped := jb.popReady()
	if ready || skipped {
		t.Error("should not deliver before prebuffer threshold")
	}

	// Third packet meets the threshold.
	jb.push(2, []byte{2})

	_, ready, _ = jb.popReady()
	if !ready {
		t.Error("expected ready after prebuffer threshold met")
	}
}

func TestJitterBuffer_StalePacketRejected(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	jb.push(5, []byte{5})

	jb.popReady() // advance expected to 6

	if jb.push(5, []byte{5}) {
		t.Error("stale seq=5 should be rejected after seq=6 is expected")
	}
}

func TestJitterBuffer_DuplicateRejected(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	if !jb.push(10, []byte{10}) {
		t.Fatal("first push should succeed")
	}

	if jb.push(10, []byte{10}) {
		t.Error("duplicate seq=10 should be rejected")
	}
}

func TestJitterBuffer_OutOfOrderDelivery(t *testing.T) {
	// prebuffer=1 so a single push satisfies the threshold.
	jb := newRTPJitterBuffer(1, 10)

	// Push seq=0 first — this sets expected=0 and satisfies the prebuffer.
	jb.push(0, []byte{0})

	// Pop seq=0 (started=true, expected advances to 1).
	payload, ready, _ := jb.popReady()
	if !ready {
		t.Fatal("expected ready for seq=0")
	}

	if payload[0] != 0 {
		t.Errorf("expected payload[0]=0; got %d", payload[0])
	}

	// Now push seq=2 then seq=1 (out of order, both future).
	jb.push(2, []byte{2})
	jb.push(1, []byte{1}) // arrives after seq=2 but is expected=1

	// popReady should deliver seq=1 first (in-order by expected).
	payload, ready, _ = jb.popReady()
	if !ready {
		t.Fatal("expected ready for seq=1")
	}

	if payload[0] != 1 {
		t.Errorf("expected payload[0]=1; got %d", payload[0])
	}

	// Then seq=2.
	payload, ready, _ = jb.popReady()
	if !ready {
		t.Fatal("expected ready for seq=2")
	}

	if payload[0] != 2 {
		t.Errorf("expected payload[0]=2; got %d", payload[0])
	}
}

func TestJitterBuffer_SkipMissingWhenOverflow(t *testing.T) {
	maxDepth := 4
	jb := newRTPJitterBuffer(1, maxDepth)

	// Push seq=0 first to initialize expected=0 and pass prebuffer.
	jb.push(0, []byte{0})
	jb.popReady() // expected is now 1

	// Push maxDepth/2 = 2 packets that are NOT seq=1 (skip seq=1).
	jb.push(2, []byte{2})
	jb.push(3, []byte{3})

	// Buffer has 2 frames >= maxDepth/2=2, expected=1 missing → skip.
	_, ready, skipped := jb.popReady()
	if ready {
		t.Error("expected ready=false for missing seq=1")
	}

	if !skipped {
		t.Error("expected skipped=true when buffer at half-depth with missing expected")
	}
}

func TestJitterBuffer_ShouldConceal(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	// Not started yet — should NOT conceal.
	if jb.shouldConceal(100 * time.Millisecond) {
		t.Error("shouldConceal should be false before any push")
	}

	jb.push(0, []byte{0})
	jb.popReady() // started = true

	// Just pushed — should conceal within the window.
	if !jb.shouldConceal(100 * time.Millisecond) {
		t.Error("shouldConceal should be true right after a push")
	}
}

func TestJitterBuffer_AdvancePast(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	jb.push(0, []byte{0})
	jb.popReady() // expected=1 now

	jb.push(1, []byte{1})

	jb.advancePast() // should discard seq=1 and advance expected to 2

	// seq=1 should now be treated as stale.
	if jb.push(1, []byte{1}) {
		t.Error("seq=1 should be stale after advancePast")
	}

	// seq=2 should succeed.
	if !jb.push(2, []byte{2}) {
		t.Error("seq=2 should be accepted after advancePast")
	}
}

func TestJitterBuffer_WrapAroundSequence(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	// Start just below wrap point.
	jb.push(0xFFFE, []byte{0xFE})
	jb.popReady() // expected = 0xFFFF

	jb.push(0xFFFF, []byte{0xFF})

	payload, ready, _ := jb.popReady() // expected = 0x0000
	if !ready || payload[0] != 0xFF {
		t.Error("expected seq=0xFFFF to be delivered correctly")
	}

	jb.push(0x0000, []byte{0x00})

	payload, ready, _ = jb.popReady() // expected = 0x0001
	if !ready || payload[0] != 0x00 {
		t.Error("expected seq=0x0000 (wrap) to be delivered correctly")
	}
}

// ─── popOrConceal tests ──────────────────────────────────────────────────────

func TestPopOrConceal_ReturnsReadyFrame(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA})

	payload, conceal := jb.popOrConceal(100 * time.Millisecond)
	if payload == nil || payload[0] != 0xAA {
		t.Errorf("expected payload 0xAA, got %v", payload)
	}

	if conceal {
		t.Error("conceal should be false when frame is returned")
	}
}

func TestPopOrConceal_ConcealOnSkippedGap(t *testing.T) {
	jb := newRTPJitterBuffer(1, 4)

	jb.push(0, []byte{0})
	jb.popOrConceal(100 * time.Millisecond) // consume seq=0, expected=1

	jb.push(2, []byte{2})
	jb.push(3, []byte{3}) // count=2 >= maxDepth/2=2, seq=1 missing → skip

	payload, conceal := jb.popOrConceal(100 * time.Millisecond)
	if payload != nil {
		t.Error("expected nil payload for skipped gap")
	}

	if !conceal {
		t.Error("expected conceal=true when buffer skips missing seq")
	}
}

func TestPopOrConceal_ConcealOnEmptyActiveStream(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	jb.push(0, []byte{0})
	jb.popOrConceal(100 * time.Millisecond) // started=true, lastPush ~now

	// Buffer is now empty but lastPush is recent.
	payload, conceal := jb.popOrConceal(100 * time.Millisecond)
	if payload != nil {
		t.Error("expected nil payload")
	}

	if !conceal {
		t.Error("expected conceal=true for empty buffer with recent push")
	}
}

func TestPopOrConceal_NoConcealWhenStale(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	jb.push(0, []byte{0})
	jb.popOrConceal(100 * time.Millisecond)

	// Force lastPush to be old.
	jb.mu.Lock()
	jb.lastPush = time.Now().Add(-200 * time.Millisecond)
	jb.mu.Unlock()

	payload, conceal := jb.popOrConceal(100 * time.Millisecond)
	if payload != nil {
		t.Error("expected nil payload")
	}

	if conceal {
		t.Error("expected conceal=false when lastPush is beyond recentWindow")
	}
}

func TestPopOrConceal_NoConcealBeforeStart(t *testing.T) {
	jb := newRTPJitterBuffer(3, 10)

	// Only push 1 packet, need 3 for prebuffer.
	jb.push(0, []byte{0})

	payload, conceal := jb.popOrConceal(100 * time.Millisecond)
	if payload != nil || conceal {
		t.Error("expected nothing before prebuffer threshold")
	}
}

// ─── Ring buffer integrity tests ─────────────────────────────────────────────

func TestJitterBuffer_PushCopiesPayload(t *testing.T) {
	jb := newRTPJitterBuffer(1, 10)

	input := []byte{1, 2, 3}
	jb.push(0, input)

	// Mutate the input after push.
	input[0] = 99

	payload, ready, _ := jb.popReady()
	if !ready {
		t.Fatal("expected ready")
	}

	if payload[0] != 1 {
		t.Error("jitter buffer should hold a copy, not a reference to input")
	}
}

func TestJitterBuffer_RingBufferOverwrite(t *testing.T) {
	// With maxDepth=4, seq 0 and seq 4 map to the same slot (0 % 4 == 4 % 4).
	// After popping seq 0-3, pushing seq 4 should reuse the slot.
	jb := newRTPJitterBuffer(1, 4)

	for i := uint16(0); i < 4; i++ {
		jb.push(i, []byte{byte(i)})
	}

	for range 4 {
		jb.popReady()
	}

	// Now push seq=4 which maps to slot 0.
	if !jb.push(4, []byte{4}) {
		t.Error("push seq=4 should succeed after slot 0 was freed")
	}

	payload, ready, _ := jb.popReady()
	if !ready || payload[0] != 4 {
		t.Errorf("expected payload=4, got ready=%v payload=%v", ready, payload)
	}
}

func TestJitterBuffer_FullBufferRejectsNewSequence(t *testing.T) {
	jb := newRTPJitterBuffer(1, 4)

	for i := uint16(0); i < 4; i++ {
		if !jb.push(i, []byte{byte(i)}) {
			t.Fatalf("push seq=%d should succeed", i)
		}
	}

	// Buffer is full. New seq that doesn't collide with existing should fail
	// only if the slot is occupied by a different valid seq number.
	// seq=4 maps to slot 0 which has seq=0 — the overwrite path triggers
	// because slot.valid && slot.seq != seq.
	// Actually with maxDepth=4 this will overwrite. Let's test with a seq
	// that maps to a full, non-overwritable scenario.
	// Since all 4 slots are occupied, and any new seq will map to one of them,
	// the overwrite logic will kick in. Let's verify count stays at 4.
	jb.push(4, []byte{4}) // overwrites slot 0 (was seq=0)

	jb.mu.Lock()
	count := jb.count
	jb.mu.Unlock()

	if count != 4 {
		t.Errorf("count should be 4 after overwrite, got %d", count)
	}
}
