package webaudio

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// recordingSink captures every payload handed to InjectTxFrame so tests can
// assert on the number of forwarded frames and their contents. It is
// mutex-protected so it can be exercised from parallel tests in the future.
type recordingSink struct {
	mu       sync.Mutex
	payloads [][]byte
}

func (s *recordingSink) send(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make([]byte, len(p))
	copy(cp, p)
	s.payloads = append(s.payloads, cp)
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.payloads)
}

func TestBridge_InjectTxFrame_ForwardsToSendFn(t *testing.T) {
	sink := &recordingSink{}
	bridge := NewBridge(zerolog.Nop(), sink.send)

	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	bridge.InjectTxFrame(payload)

	if got := sink.count(); got != 1 {
		t.Errorf("expected 1 forwarded payload, got %d", got)
	}
}

func TestBridge_InjectTxFrame_NilSendIsNoop(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), nil)

	// Must not panic.
	bridge.InjectTxFrame([]byte{0x01})
}

func TestBridge_PushRxFrame_Delivered(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	data := []byte{0xCA, 0xFE}
	bridge.PushRxFrame(data)

	select {
	case got := <-bridge.RxFrames():
		if len(got) != 2 || got[0] != 0xCA || got[1] != 0xFE {
			t.Errorf("unexpected frame data: %v", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for RX frame")
	}
}

func TestBridge_PushRxFrame_DropsOnFull(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	// Fill the channel (cap = 50).
	for range 50 {
		bridge.PushRxFrame([]byte{0x00})
	}

	// This push must not block.
	done := make(chan struct{})

	go func() {
		bridge.PushRxFrame([]byte{0xFF})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("PushRxFrame blocked on full channel")
	}
}

func TestBridge_ConsumerCount(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	if bridge.HasConsumer() {
		t.Error("fresh bridge must report no consumer")
	}

	bridge.AddConsumer()

	if !bridge.HasConsumer() {
		t.Error("HasConsumer should be true after AddConsumer")
	}

	// A second concurrent stream keeps the bridge active until both detach.
	bridge.AddConsumer()
	bridge.RemoveConsumer()

	if !bridge.HasConsumer() {
		t.Error("HasConsumer should stay true while one consumer remains")
	}

	bridge.RemoveConsumer()

	if bridge.HasConsumer() {
		t.Error("HasConsumer should be false after the last RemoveConsumer")
	}
}

func TestBridge_ConsumerCount_NilBridge(t *testing.T) {
	var bridge *Bridge

	// All must be nil-safe no-ops, matching the rest of the Bridge API.
	bridge.AddConsumer()
	bridge.RemoveConsumer()

	if bridge.HasConsumer() {
		t.Error("nil bridge must report no consumer")
	}
}

func TestBridge_RxFrames_ReturnsReadOnlyChannel(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	ch := bridge.RxFrames()
	if ch == nil {
		t.Error("RxFrames() returned nil")
	}

	// Verify it is a receive-only channel (compile-time check via type).
	var _ <-chan []byte = ch
}
