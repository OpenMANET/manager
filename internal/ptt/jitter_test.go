package ptt

import (
	"testing"
	"time"
)

func TestJitterBuffer_PushAndPop_InOrder(t *testing.T) {
	jb := newRTPJitterBuffer(1, 24)

	payloads := [][]byte{
		{0x01, 0x02},
		{0x03, 0x04},
		{0x05, 0x06},
	}

	for i, p := range payloads {
		if !jb.push(uint16(i), p) {
			t.Fatalf("push(%d) returned false", i)
		}
	}

	for i, want := range payloads {
		got, ready, skipped := jb.popReady()
		if skipped {
			t.Fatalf("frame %d: unexpected skip", i)
		}

		if !ready {
			t.Fatalf("frame %d: expected ready=true", i)
		}

		if string(got) != string(want) {
			t.Errorf("frame %d: got %v, want %v", i, got, want)
		}
	}
}

func TestJitterBuffer_Prebuffer(t *testing.T) {
	jb := newRTPJitterBuffer(3, 24)

	// Push fewer packets than the prebuffer threshold.
	jb.push(0, []byte{0x01})
	jb.push(1, []byte{0x02})

	// Should not pop before prebuffer is full.
	_, ready, _ := jb.popReady()
	if ready {
		t.Error("expected not ready before prebuffer fills")
	}

	// Add the third packet to hit the threshold.
	jb.push(2, []byte{0x03})

	_, ready, _ = jb.popReady()
	if !ready {
		t.Error("expected ready after prebuffer fills")
	}
}

func TestJitterBuffer_OldPacketDropped(t *testing.T) {
	jb := newRTPJitterBuffer(1, 24)

	jb.push(10, []byte{0xaa})
	jb.popReady() // advances expected to 11

	// Packet older than current cursor should be dropped.
	if jb.push(5, []byte{0xbb}) {
		t.Error("expected old packet to be rejected")
	}
}

func TestJitterBuffer_DuplicateDropped(t *testing.T) {
	jb := newRTPJitterBuffer(1, 24)

	jb.push(0, []byte{0x01})

	if jb.push(0, []byte{0x01}) {
		t.Error("expected duplicate packet to be rejected")
	}
}

func TestJitterBuffer_MaxDepthDropped(t *testing.T) {
	maxDepth := 4
	jb := newRTPJitterBuffer(1, maxDepth)

	for i := 0; i < maxDepth; i++ {
		jb.push(uint16(i), []byte{byte(i)})
	}

	// One more should be dropped.
	if jb.push(uint16(maxDepth), []byte{0xff}) {
		t.Error("expected overflow packet to be rejected")
	}
}

func TestJitterBuffer_SkipMissingAfterHalfFull(t *testing.T) {
	maxDepth := 4
	jb := newRTPJitterBuffer(1, maxDepth)

	// Anchor the buffer at seq 0 and pop it so expected advances to 1.
	jb.push(0, []byte{0x00})
	jb.popReady() // pops seq 0; expected becomes 1

	// Push seq 2 and 3, skipping seq 1 (the expected one).
	jb.push(2, []byte{0x02})
	jb.push(3, []byte{0x03})

	// With maxDepth/2 frames buffered and seq 1 missing, popReady should skip.
	_, _, skipped := jb.popReady()
	if !skipped {
		t.Error("expected missing seq 1 to be skipped when half buffer is full")
	}
}

func TestJitterBuffer_ShouldConceal(t *testing.T) {
	jb := newRTPJitterBuffer(1, 24)
	jb.push(0, []byte{0x01})
	jb.popReady() // starts the buffer

	// Push a packet to record a recent lastPush time.
	jb.push(1, []byte{0x02})
	jb.popReady()

	// Should conceal because a packet was just pushed.
	if !jb.shouldConceal(100 * time.Millisecond) {
		t.Error("expected shouldConceal=true immediately after push")
	}
}

func TestJitterBuffer_ShouldNotConcealWhenIdle(t *testing.T) {
	jb := newRTPJitterBuffer(1, 24)
	// No packets pushed; shouldConceal should return false.
	if jb.shouldConceal(100 * time.Millisecond) {
		t.Error("expected shouldConceal=false when buffer is empty")
	}
}

func TestSeqLess(t *testing.T) {
	cases := []struct {
		a, b uint16
		want bool
	}{
		{0, 1, true},
		{1, 0, false},
		{65535, 0, true},  // wrap-around: 65535 < 0 in seq space
		{0, 65535, false}, // 0 > 65535 in seq space
		{100, 100, false},
	}
	for _, tc := range cases {
		got := seqLess(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("seqLess(%d, %d): got %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
