package ptt

import (
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// mockClosingWriter extends mockWriter with a Close method so the
// "close old sender" code path in replaceNetwork can be exercised.
type mockClosingWriter struct {
	closeErr error
	mockWriter
	closeCalled bool
}

func (m *mockClosingWriter) Close() error {
	m.closeCalled = true

	return m.closeErr
}

// safeMockWriter is a goroutine-safe PacketWriter used by concurrency tests.
// The built-in mockWriter is not thread-safe (unsynchronised slice append), so
// races reported by the race detector would be false positives about the mock
// rather than about swappableSender itself.
type safeMockWriter struct {
	Packets [][]byte
	mu      sync.Mutex
}

func (s *safeMockWriter) Write(b []byte) (int, error) {
	cp := make([]byte, len(b))
	copy(cp, b)
	s.mu.Lock()
	s.Packets = append(s.Packets, cp)
	s.mu.Unlock()

	return len(b), nil
}

func (s *safeMockWriter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.Packets)
}

// mockClosingReader extends mockReader with a close-tracking field separate
// from mockReader's own close mechanism, so tests can inspect whether the
// swappable wrapper invoked Close on the correct underlying reader.
type trackingReader struct {
	closed bool
	mu     sync.Mutex
}

func (t *trackingReader) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	return 0, nil, errors.New("tracking reader: not for reading")
}

func (t *trackingReader) Close() error {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()

	return nil
}

func (t *trackingReader) wasClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closed
}

// ─── swappableSender ─────────────────────────────────────────────────────────

func TestSwappableSender_WriteDelegatesToImpl(t *testing.T) {
	w := &mockWriter{}
	s := newSwappableSender(w)

	n, err := s.Write([]byte{1, 2, 3})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if n != 3 {
		t.Fatalf("expected n=3; got %d", n)
	}

	if len(w.Packets) != 1 {
		t.Fatalf("expected 1 packet captured; got %d", len(w.Packets))
	}
}

func TestSwappableSender_WriteReturnsImplError(t *testing.T) {
	w := &mockWriter{writeErr: errors.New("send failed")}
	s := newSwappableSender(w)

	_, err := s.Write([]byte{0xff})
	if err == nil || err.Error() != "send failed" {
		t.Fatalf("expected 'send failed' error; got %v", err)
	}
}

func TestSwappableSender_SwapReturnsOldImpl(t *testing.T) {
	old := &mockWriter{}
	newW := &mockWriter{}
	s := newSwappableSender(old)

	returned := s.swap(newW)
	if returned != old {
		t.Error("swap should return the previous PacketWriter")
	}
}

func TestSwappableSender_AfterSwap_WritesGoToNewImpl(t *testing.T) {
	old := &mockWriter{}
	newW := &mockWriter{}
	s := newSwappableSender(old)

	s.swap(newW)
	_, _ = s.Write([]byte{0xAA})

	if len(old.Packets) != 0 {
		t.Error("expected no packets on old writer after swap")
	}

	if len(newW.Packets) != 1 {
		t.Error("expected 1 packet on new writer after swap")
	}
}

func TestSwappableSender_MultipleSwaps(t *testing.T) {
	w1 := &mockWriter{}
	w2 := &mockWriter{}
	w3 := &mockWriter{}
	s := newSwappableSender(w1)

	s.swap(w2)
	s.swap(w3)
	_, _ = s.Write([]byte{1})

	if len(w3.Packets) != 1 {
		t.Error("expected write to reach the most-recent writer (w3)")
	}

	if len(w1.Packets)+len(w2.Packets) != 0 {
		t.Error("expected no writes on w1 or w2 after double swap")
	}
}

// TestSwappableSender_ConcurrentWrites verifies that concurrent writes and a
// concurrent swap do not race.  Run with -race to detect data races.
func TestSwappableSender_ConcurrentWrites(t *testing.T) {
	var (
		wOld = &safeMockWriter{}
		wNew = &safeMockWriter{}
		s    = newSwappableSender(wOld)
		wg   sync.WaitGroup
	)

	const goroutines = 50

	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			_, _ = s.Write([]byte{0x01})
		}()
	}

	// Trigger the swap mid-flight.
	s.swap(wNew)

	wg.Wait()
	// All writes should have reached either old or new — total count must be goroutines.
	total := wOld.count() + wNew.count()
	if total != goroutines {
		t.Errorf("expected %d total packets; got %d (old=%d new=%d)",
			goroutines, total, wOld.count(), wNew.count())
	}
}

// ─── swappableReceiver ────────────────────────────────────────────────────────

func TestSwappableReceiver_ReadDelegatesToImpl(t *testing.T) {
	payload := []byte{0xCA, 0xFE}
	r := newSwappableReceiver(newMockReader(
		mockPacket{data: payload, src: udpSrc("192.168.1.1")},
	))

	buf := make([]byte, 32)

	n, src, err := r.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if n != 2 || buf[0] != 0xCA || buf[1] != 0xFE { //nolint:gosec // buf is always 32 bytes
		t.Errorf("unexpected read result: n=%d buf[0:2]=%v", n, buf[:2])
	}

	if src.IP.String() != "192.168.1.1" {
		t.Errorf("unexpected src: %s", src.IP.String())
	}
}

func TestSwappableReceiver_CloseDelegatesToImpl(t *testing.T) {
	tr := &trackingReader{}
	r := newSwappableReceiver(tr)

	if err := r.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !tr.wasClosed() {
		t.Error("expected underlying reader to be closed")
	}
}

func TestSwappableReceiver_SwapReturnsOldImpl(t *testing.T) {
	old := &trackingReader{}
	newR := &trackingReader{}
	r := newSwappableReceiver(old)

	returned := r.swap(newR)
	if returned != old {
		t.Error("swap should return the previous PacketReader")
	}
}

func TestSwappableReceiver_AfterSwap_ReadsFromNewImpl(t *testing.T) {
	oldReader := newMockReader(mockPacket{data: []byte{0x01}, src: udpSrc("1.1.1.1")})
	newReader := newMockReader(mockPacket{data: []byte{0x02}, src: udpSrc("2.2.2.2")})

	r := newSwappableReceiver(oldReader)
	r.swap(newReader)

	buf := make([]byte, 4)

	n, src, err := r.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if n != 1 || buf[0] != 0x02 {
		t.Errorf("expected data from new reader (0x02); got n=%d buf[0]=%#x", n, buf[0])
	}

	if src.IP.String() != "2.2.2.2" {
		t.Errorf("expected src 2.2.2.2; got %s", src.IP.String())
	}
}

func TestSwappableReceiver_AfterSwap_CloseClosesNewImpl(t *testing.T) {
	old := &trackingReader{}
	newTR := &trackingReader{}
	r := newSwappableReceiver(old)
	r.swap(newTR)

	_ = r.Close()

	if !newTR.wasClosed() {
		t.Error("expected new impl to be closed after swap+Close")
	}

	if old.wasClosed() {
		t.Error("expected old impl NOT to be closed by the wrapper's Close() after swap")
	}
}

// ─── replaceNetwork ───────────────────────────────────────────────────────────

// newReplaceNetworkRuntime builds a minimal PTTRuntime wired with swappable
// sender and receiver ready for replaceNetwork tests.
func newReplaceNetworkRuntime(sender PacketWriter, receiver PacketReader) *PTTRuntime {
	rt := &PTTRuntime{
		sender:   newSwappableSender(sender),
		receiver: newSwappableReceiver(receiver),
	}
	rt.localIP.Store("10.0.0.1")

	return rt
}

func TestReplaceNetwork_WritesGoToNewSender(t *testing.T) {
	oldSender := &mockWriter{}
	newSender := &mockWriter{}
	oldReceiver := &trackingReader{}
	newReceiver := &trackingReader{}

	rt := newReplaceNetworkRuntime(oldSender, oldReceiver)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, newSender, newReceiver, "10.0.0.2")

	_, _ = rt.sender.Write([]byte{0xAB})

	if len(newSender.Packets) != 1 {
		t.Error("expected write to reach newSender after replaceNetwork")
	}

	if len(oldSender.Packets) != 0 {
		t.Error("expected no writes on oldSender after replaceNetwork")
	}
}

func TestReplaceNetwork_ReadsFromNewReceiver(t *testing.T) {
	oldSender := &mockWriter{}
	newSender := &mockWriter{}
	oldReceiver := newMockReader(mockPacket{data: []byte{0x01}, src: udpSrc("1.1.1.1")})
	newReceiver := newMockReader(mockPacket{data: []byte{0x02}, src: udpSrc("2.2.2.2")})

	rt := newReplaceNetworkRuntime(oldSender, oldReceiver)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, newSender, newReceiver, "10.0.0.2")

	buf := make([]byte, 4)
	n, src, _ := rt.receiver.ReadFromUDP(buf)

	if n != 1 || buf[0] != 0x02 {
		t.Errorf("expected read from new receiver (0x02 from 2.2.2.2); got n=%d buf[0]=%#x src=%s",
			n, buf[0], src)
	}
}

func TestReplaceNetwork_ClosesOldReceiver(t *testing.T) {
	oldReceiver := &trackingReader{}
	newReceiver := &trackingReader{}

	rt := newReplaceNetworkRuntime(&mockWriter{}, oldReceiver)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, &mockWriter{}, newReceiver, "10.0.0.2")

	if !oldReceiver.wasClosed() {
		t.Error("expected old receiver to be closed after replaceNetwork")
	}
}

func TestReplaceNetwork_DoesNotCloseNewReceiver(t *testing.T) {
	newReceiver := &trackingReader{}

	rt := newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, &mockWriter{}, newReceiver, "10.0.0.2")

	if newReceiver.wasClosed() {
		t.Error("expected new receiver NOT to be closed by replaceNetwork")
	}
}

func TestReplaceNetwork_ClosesOldSenderIfCloseable(t *testing.T) {
	oldSender := &mockClosingWriter{}
	newSender := &mockWriter{}

	rt := newReplaceNetworkRuntime(oldSender, &trackingReader{})
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, newSender, &trackingReader{}, "10.0.0.2")

	if !oldSender.closeCalled {
		t.Error("expected Close() to be called on old sender that implements io.Closer")
	}
}

func TestReplaceNetwork_OldSenderNotCloseableNoError(t *testing.T) {
	// mockWriter does NOT implement Close() — this must not panic.
	oldSender := &mockWriter{}
	ptt := &PTTConfig{Log: zerolog.Nop()}
	rt := newReplaceNetworkRuntime(oldSender, &trackingReader{})

	// Should not panic.
	ptt.replaceNetwork(rt, &mockWriter{}, &trackingReader{}, "10.0.0.2")
}

func TestReplaceNetwork_UpdatesLocalIP(t *testing.T) {
	rt := newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, &mockWriter{}, &trackingReader{}, "172.16.0.5")

	got, ok := rt.localIP.Load().(string)
	if !ok {
		t.Fatal("localIP is not a string")
	}

	if got != "172.16.0.5" {
		t.Errorf("expected localIP=172.16.0.5 after replaceNetwork; got %q", got)
	}
}

// TestReplaceNetwork_ConcurrentWritesDuringSwap checks that writes in flight
// while replaceNetwork executes do not race.  Run with -race to detect races.
func TestReplaceNetwork_ConcurrentWritesDuringSwap(t *testing.T) {
	oldSender := &safeMockWriter{}
	newSender := &safeMockWriter{}
	rt := newReplaceNetworkRuntime(oldSender, &trackingReader{})
	ptt := &PTTConfig{Log: zerolog.Nop()}

	var wg sync.WaitGroup

	const goroutines = 40

	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			_, _ = rt.sender.Write([]byte{0x01})
		}()
	}

	ptt.replaceNetwork(rt, newSender, &trackingReader{}, "10.0.0.2")
	wg.Wait()

	total := oldSender.count() + newSender.count()
	if total != goroutines {
		t.Errorf("expected %d total packets; got %d", goroutines, total)
	}
}

// ─── UpdateMulticastEndpoint validation ──────────────────────────────────────

// setActiveForTest registers cfg as the active config for the duration of the
// test and restores nil when the test ends, preventing cross-test pollution.
func setActiveForTest(t *testing.T, cfg *PTTConfig) {
	t.Helper()
	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })
}

func TestUpdateMulticastEndpoint_NotStarted_Error(t *testing.T) {
	// activeConfig is nil (no Store called) — subsystem never started.
	activeConfig.Store(nil)

	err := UpdateMulticastEndpoint("224.0.0.2", 5008)
	if err == nil {
		t.Fatal("expected error when activeConfig is nil")
	}

	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_NilRuntime_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	// runtime is nil — Start was not completed.
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("224.0.0.2", 5008)
	if err == nil {
		t.Fatal("expected error when runtime is nil")
	}

	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_InvalidIP_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("not-an-ip", 5008)
	if err == nil {
		t.Fatal("expected error for unparseable IP")
	}

	if !strings.Contains(err.Error(), "not a valid IPv4 address") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_IPv6_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	// Valid IPv6 multicast address — should be rejected (not IPv4).
	err := UpdateMulticastEndpoint("ff02::1", 5008)
	if err == nil {
		t.Fatal("expected error for IPv6 address")
	}

	if !strings.Contains(err.Error(), "not a valid IPv4 address") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_UnicastIP_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("10.0.0.1", 5008)
	if err == nil {
		t.Fatal("expected error for non-multicast IPv4 address")
	}

	if !strings.Contains(err.Error(), "not a multicast address") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_BroadcastIP_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("255.255.255.255", 5008)
	if err == nil {
		t.Fatal("expected error for broadcast address (not multicast)")
	}
}

func TestUpdateMulticastEndpoint_PortZero_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("224.0.0.2", 0)
	if err == nil {
		t.Fatal("expected error for port 0")
	}

	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_PortTooHigh_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("224.0.0.2", 65536)
	if err == nil {
		t.Fatal("expected error for port 65536")
	}

	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestUpdateMulticastEndpoint_PortBoundaries_Valid(t *testing.T) {
	// Ports 1 and 65535 must pass validation (buildNetwork will fail due to
	// no real network in tests, but the port check itself must succeed).
	for _, port := range []int{1, 65535} {
		cfg := &PTTConfig{Log: zerolog.Nop(), Iface: "lo"}
		cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
		setActiveForTest(t, cfg)

		err := UpdateMulticastEndpoint("224.0.0.2", port)
		// Expect either nil or a network-level error — NOT a port range error.
		if err != nil && strings.Contains(err.Error(), "out of range") {
			t.Errorf("port %d should be valid but got range error: %v", port, err)
		}
	}
}

func TestUpdateMulticastEndpoint_NegativePort_Error(t *testing.T) {
	cfg := &PTTConfig{Log: zerolog.Nop()}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	err := UpdateMulticastEndpoint("224.0.0.2", -1)
	if err == nil {
		t.Fatal("expected error for negative port")
	}

	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestUpdateMulticastEndpoint_ConfigRolledBackOnBuildNetworkFailure verifies
// that McastAddr and McastPort are restored to their previous values when
// buildNetwork fails, leaving the running config unchanged.
func TestUpdateMulticastEndpoint_ConfigRolledBackOnBuildNetworkFailure(t *testing.T) {
	cfg := &PTTConfig{
		Log:       zerolog.Nop(),
		McastAddr: "224.0.0.1",
		McastPort: 5007,
		// Use a nonexistent iface so buildNetwork fails, triggering rollback.
		Iface: "nonexistent-iface-xyz",
	}
	cfg.runtime = newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	setActiveForTest(t, cfg)

	_ = UpdateMulticastEndpoint("224.0.0.99", 9999)

	// Config must be rolled back to original values regardless of outcome.
	if cfg.McastAddr != "224.0.0.1" {
		t.Errorf("expected McastAddr rolled back to 224.0.0.1; got %q", cfg.McastAddr)
	}

	if cfg.McastPort != 5007 {
		t.Errorf("expected McastPort rolled back to 5007; got %d", cfg.McastPort)
	}
}

// TestUpdateMulticastEndpoint_LocalIPUpdatedAfterSuccessfulSwap verifies that
// localIP in the runtime is updated when replaceNetwork succeeds.  We call
// replaceNetwork directly so no real sockets are needed.
func TestUpdateMulticastEndpoint_LocalIPUpdatedAfterSuccessfulSwap(t *testing.T) {
	rt := newReplaceNetworkRuntime(&mockWriter{}, &trackingReader{})
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.replaceNetwork(rt, &mockWriter{}, &trackingReader{}, "192.168.10.5")

	var v atomic.Value // just to confirm the type works the same way

	v.Store("192.168.10.5")

	want, ok := v.Load().(string)
	if !ok {
		t.Fatal("want is not a string")
	}

	got, ok2 := rt.localIP.Load().(string)
	if !ok2 {
		t.Fatal("localIP is not a string")
	}

	if got != want {
		t.Errorf("expected localIP %q; got %q", want, got)
	}
}
