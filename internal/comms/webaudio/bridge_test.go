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
		if d := got.Data(); len(d) != 2 || d[0] != 0xCA || d[1] != 0xFE {
			t.Errorf("unexpected frame data: %v", d)
		}

		got.Release()
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for RX frame")
	}
}

// TestBridge_PushRxFrame_CopiesPayload pins the ownership contract: the
// caller's payload may be reused (it aliases a jitter-pool buffer) the
// moment PushRxFrame returns, so the frame in the channel must hold its
// own copy.
func TestBridge_PushRxFrame_CopiesPayload(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	data := []byte{0x11, 0x22}
	bridge.PushRxFrame(data)

	// Caller reuses its buffer immediately.
	data[0] = 0xEE
	data[1] = 0xFF

	got := <-bridge.RxFrames()
	defer got.Release()

	if d := got.Data(); d[0] != 0x11 || d[1] != 0x22 {
		t.Errorf("frame must not alias the caller's buffer; got %v", d)
	}
}

func TestBridge_PushRxFrame_DropsOnFull(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	// Fill the channel.
	for range cap(bridge.rxFrames) {
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

// TestBridge_PushRxFrame_ZeroAlloc pins the pooled hand-off: a full
// push→receive→release cycle must not allocate once the pool is warm.
func TestBridge_PushRxFrame_ZeroAlloc(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	data := make([]byte, 100)

	// Warm the pool.
	bridge.PushRxFrame(data)
	f := <-bridge.RxFrames()
	f.Release()

	allocs := testing.AllocsPerRun(100, func() {
		bridge.PushRxFrame(data)

		got := <-bridge.RxFrames()
		got.Release()
	})

	if allocs != 0 {
		t.Errorf("push/receive/release cycle allocated %.1f/op; want 0", allocs)
	}
}

// TestBridge_PushRxFrame_DropRecyclesBuffer pins the drop branch's pool
// hygiene: a frame dropped because the channel is full must return its
// pooled buffer, so sustained drops do not allocate either.
func TestBridge_PushRxFrame_DropRecyclesBuffer(t *testing.T) {
	bridge := NewBridge(zerolog.Nop(), func(_ []byte) {})

	data := make([]byte, 100)

	for range cap(bridge.rxFrames) {
		bridge.PushRxFrame(data)
	}

	allocs := testing.AllocsPerRun(100, func() {
		bridge.PushRxFrame(data)
	})

	if allocs != 0 {
		t.Errorf("drop path allocated %.1f/op; want 0 (buffer must return to the pool)", allocs)
	}
}

// TestFrame_ZeroValueReleaseIsNoop keeps Release safe on the zero Frame,
// matching the nil-safety of the rest of the Bridge API.
func TestFrame_ZeroValueReleaseIsNoop(t *testing.T) {
	var f Frame

	f.Release()

	if d := f.Data(); len(d) != 0 {
		t.Errorf("zero Frame Data should be empty; got %v", d)
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
	var _ <-chan Frame = ch
}
