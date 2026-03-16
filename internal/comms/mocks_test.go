package comms

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
)

// ─── mockStream ───────────────────────────────────────────────────────────────

// mockStream satisfies AudioStream. Start/Stop/Close failures can be injected
// by setting the corresponding error fields before the call.
type mockStream struct {
	startErr   error
	stopErr    error
	closeErr   error
	startCalls int
	stopCalls  int
	closeCalls int
}

func (m *mockStream) Start() error {
	m.startCalls++

	return m.startErr
}

func (m *mockStream) Stop() error {
	m.stopCalls++

	return m.stopErr
}

func (m *mockStream) Close() error {
	m.closeCalls++

	return m.closeErr
}

// ─── mockDecoder ─────────────────────────────────────────────────────────────

// mockDecoder satisfies AudioDecoder. It fills pcm with a fixed repeating value.
type mockDecoder struct {
	decodeErr error
	returnN   int
	fillValue int16
	forceN    bool
	// plcOK makes DecodeFloat32 succeed when payload is nil (PLC) even if
	// decodeErr is set. This allows testing the PLC-fallback-on-decode-error
	// path introduced in Change 3.
	plcOK bool
}

func (m *mockDecoder) Decode(_ []byte, pcm []int16) (int, error) {
	if m.decodeErr != nil {
		return 0, m.decodeErr
	}

	for i := range pcm {
		pcm[i] = m.fillValue
	}

	if m.returnN > 0 || m.forceN {
		return m.returnN, nil
	}

	return len(pcm), nil
}

func (m *mockDecoder) DecodeFloat32(payload []byte, pcm []float32) (int, error) {
	if m.decodeErr != nil && !(m.plcOK && payload == nil) {
		return 0, m.decodeErr
	}

	fv := float32(m.fillValue) / 32768

	for i := range pcm {
		pcm[i] = fv
	}

	if m.returnN > 0 || m.forceN {
		return m.returnN, nil
	}

	return len(pcm), nil
}

// ─── mockEncoder ─────────────────────────────────────────────────────────────

// ─── mockWriter ──────────────────────────────────────────────────────────────

// mockWriter satisfies PacketWriter. Written packets accumulate in Packets.
type mockWriter struct {
	writeErr error
	Packets  [][]byte
}

func (m *mockWriter) Write(b []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}

	cp := make([]byte, len(b))
	copy(cp, b)
	m.Packets = append(m.Packets, cp)

	return len(b), nil
}

// safeMockWriter is a goroutine-safe PacketWriter for race-detector tests.
type safeMockWriter struct {
	Packets [][]byte
	mu      sync.Mutex
}

func (m *safeMockWriter) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]byte, len(b))
	copy(cp, b)
	m.Packets = append(m.Packets, cp)

	return len(b), nil
}

func (m *safeMockWriter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.Packets)
}

// mockClosingWriter adds a Close() method to mockWriter for testing the
// "close old sender if it implements io.Closer" swap path.
type mockClosingWriter struct {
	closeErr error
	mockWriter
	closeCalled bool
}

func (m *mockClosingWriter) Close() error {
	m.closeCalled = true

	return m.closeErr
}

// ─── trackingReader ───────────────────────────────────────────────────────────

// trackingReader is a minimal PacketReader that only tracks Close() calls.
type trackingReader struct {
	closed bool
	mu     sync.Mutex
}

func (r *trackingReader) ReadFromUDP(_ []byte) (int, *net.UDPAddr, error) {
	// Block forever until closed – tests drive this via Close().
	select {} //nolint:staticcheck
}

func (r *trackingReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true

	return nil
}

// ─── mockPacket / mockReader ──────────────────────────────────────────────────

type mockPacket struct {
	src  *net.UDPAddr
	data []byte
}

// mockReader satisfies PacketReader. Pre-loaded packets are returned one per
// call; when the queue is empty it blocks until Close is called.
type mockReader struct {
	closed  chan struct{}
	packets []mockPacket
	once    sync.Once
	mu      sync.Mutex
}

func newMockReader(pkts ...mockPacket) *mockReader {
	return &mockReader{
		packets: pkts,
		closed:  make(chan struct{}),
	}
}

func (m *mockReader) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	for {
		m.mu.Lock()

		if len(m.packets) > 0 {
			pkt := m.packets[0]
			m.packets = m.packets[1:]
			n := copy(b, pkt.data)
			m.mu.Unlock()

			return n, pkt.src, nil
		}

		m.mu.Unlock()

		select {
		case <-m.closed:
			return 0, nil, errors.New("reader closed")
		default:
		}
	}
}

func (m *mockReader) Close() error {
	m.once.Do(func() { close(m.closed) })

	return nil
}

// remaining returns the number of un-consumed packets (for test assertions).
func (m *mockReader) remaining() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.packets)
}

// ─── mockEventSource ─────────────────────────────────────────────────────────

// mockEventSource satisfies EventSource. Pre-loaded events are sent on ch;
// closing ch causes Events to return a closed channel so Run exits cleanly.
type mockEventSource struct {
	ch chan PTTEvent
}

func (m *mockEventSource) Events(_ context.Context) <-chan PTTEvent {
	return m.ch
}

// ─── mockRTPSender ───────────────────────────────────────────────────────────

// mockRTPSender satisfies rtpSender. Sent payloads accumulate in Payloads.
type mockRTPSender struct {
	sendErr  error
	Payloads [][]byte
	mu       sync.Mutex
}

func (m *mockRTPSender) send(payload []byte) error {
	if m.sendErr != nil {
		return m.sendErr
	}

	m.mu.Lock()
	cp := make([]byte, len(payload))
	copy(cp, payload)
	m.Payloads = append(m.Payloads, cp)
	m.mu.Unlock()

	return nil
}

// safeCountingReader satisfies PacketReader. Every ReadFromUDP call returns
// immediately with a dummy byte, making it safe for concurrent race-detector
// tests that need a non-blocking reader.
type safeCountingReader struct {
	calls atomic.Int64
}

func (r *safeCountingReader) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	r.calls.Add(1)

	if len(b) > 0 {
		b[0] = 0xAB
	}

	return 1, &net.UDPAddr{}, nil
}

func (r *safeCountingReader) Close() error { return nil }

func (r *safeCountingReader) count() int64 { return r.calls.Load() }
