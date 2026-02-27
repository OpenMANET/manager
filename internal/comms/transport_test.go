//go:build !omd_omit_comms

package comms

import (
	"net"
	"sync"
	"testing"
	"time"
)

// ─── swappableSender tests ────────────────────────────────────────────────────

func TestSwappableSender_WritesToInitialImpl(t *testing.T) {
	w := &mockWriter{}
	s := newSwappableSender(w)

	if _, err := s.Write([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	if len(w.Packets) != 1 {
		t.Errorf("expected 1 packet; got %d", len(w.Packets))
	}
}

func TestSwappableSender_SwapReturnsOld(t *testing.T) {
	w1 := &mockWriter{}
	w2 := &mockWriter{}
	s := newSwappableSender(w1)

	old := s.swap(w2)

	if old != w1 {
		t.Error("swap should return the previous implementation")
	}
}

func TestSwappableSender_WritesToNewImplAfterSwap(t *testing.T) {
	w1 := &mockWriter{}
	w2 := &mockWriter{}
	s := newSwappableSender(w1)

	s.swap(w2)
	_, _ = s.Write([]byte{9})

	if len(w1.Packets) != 0 {
		t.Error("old writer should not receive packets after swap")
	}

	if len(w2.Packets) != 1 {
		t.Error("new writer should receive packets after swap")
	}
}

func TestSwappableSender_Swap_ClosesOldIfCloser(t *testing.T) {
	w1 := &mockClosingWriter{}
	w2 := &mockWriter{}
	s := newSwappableSender(w1)

	old := s.swap(w2)

	// Simulate what replaceNetwork does.
	if c, ok := old.(interface{ Close() error }); ok {
		_ = c.Close()
	}

	if !w1.closeCalled {
		t.Error("Close() should be called on old writer when it implements io.Closer")
	}
}

func TestSwappableSender_ConcurrentWritesAndSwap(t *testing.T) {
	w1 := &safeMockWriter{}
	w2 := &safeMockWriter{}
	s := newSwappableSender(w1)

	const (
		writers    = 10
		writesEach = 50
	)

	var wg sync.WaitGroup

	for i := 0; i < writers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < writesEach; j++ {
				_, _ = s.Write([]byte{byte(j)})
			}
		}()
	}

	// Swap in the middle of the writes.
	time.Sleep(1 * time.Millisecond)
	s.swap(w2)

	wg.Wait()

	total := w1.count() + w2.count()
	if total != writers*writesEach {
		t.Errorf("total writes = %d; want %d", total, writers*writesEach)
	}
}

// ─── swappableReceiver tests ──────────────────────────────────────────────────

func TestSwappableReceiver_ReadsFromInitialImpl(t *testing.T) {
	src := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1234}
	r := newMockReader(mockPacket{src: src, data: []byte{42}})
	sr := newSwappableReceiver(r)

	buf := make([]byte, 10)

	n, addr, err := sr.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}

	if n != 1 {
		t.Errorf("unexpected read length: got %d, want 1", n)
	} else if buf[0] != 42 { //nolint:gosec // buf is make([]byte,10); n==1 guarantees index is valid
		t.Errorf("unexpected read value: buf[0]=%d, want 42", buf[0]) //nolint:gosec
	}

	if addr.String() != src.String() {
		t.Errorf("addr mismatch: got %s, want %s", addr, src)
	}
}

func TestSwappableReceiver_SwapReturnsOld(t *testing.T) {
	r1 := newMockReader()
	r2 := newMockReader()
	sr := newSwappableReceiver(r1)

	old := sr.swap(r2)

	if old != r1 {
		t.Error("swap should return the previous implementation")
	}
}

func TestSwappableReceiver_Close(t *testing.T) {
	tr := &trackingReader{}
	sr := newSwappableReceiver(tr)

	if err := sr.Close(); err != nil {
		t.Fatal(err)
	}

	if !tr.closed {
		t.Error("Close should propagate to the underlying reader")
	}
}

func TestSwappableReceiver_CloseUnblocksRead(t *testing.T) {
	r := newMockReader() // empty queue — will block until Close
	sr := newSwappableReceiver(r)

	done := make(chan struct{})

	go func() {
		defer close(done)

		buf := make([]byte, 10)
		_, _, _ = sr.ReadFromUDP(buf)
	}()

	time.Sleep(20 * time.Millisecond)

	_ = sr.Close()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Error("Close should unblock ReadFromUDP")
	}
}
