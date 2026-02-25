package ptt

import (
	"context"
	"errors"
	"net"
	"sync"
)

// ─── Mock AudioStream ─────────────────────────────────────────────────────────

// mockStream satisfies AudioStream.  Start / Stop failures can be injected by
// setting the corresponding error fields before the call.
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

// ─── Mock AudioDecoder ────────────────────────────────────────────────────────

// mockDecoder satisfies AudioDecoder.  It fills pcm with a fixed repeating value.
type mockDecoder struct {
	decodeErr error
	returnN   int
	fillValue int16
	forceN    bool
}

func (m *mockDecoder) Decode(data []byte, pcm []int16) (int, error) {
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

// ─── Mock PacketWriter ────────────────────────────────────────────────────────

// mockWriter satisfies PacketWriter.  Written packets accumulate in Packets.
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

// ─── Mock PacketReader ────────────────────────────────────────────────────────

type mockPacket struct {
	src  *net.UDPAddr
	data []byte
}

// mockReader satisfies PacketReader.  Pre-loaded packets are returned one per
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

// ─── Mock EventSource ─────────────────────────────────────────────────────────

// mockEventSource satisfies EventSource.  Pre-loaded events are sent on ch;
// closing ch causes Events to return a closed channel so Run exits cleanly.
type mockEventSource struct {
	ch chan PTTEvent
}

func (m *mockEventSource) Events(_ context.Context) <-chan PTTEvent {
	return m.ch
}
