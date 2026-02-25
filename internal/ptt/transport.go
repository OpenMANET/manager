package ptt

import (
	"net"
	"sync"
)

// PacketWriter abstracts the UDP send path so the broadcast callback can be
// tested without a live socket.
type PacketWriter interface {
	Write(b []byte) (int, error)
}

// PacketReader abstracts the UDP receive path so receiveLoop can be exercised
// with pre-seeded byte sequences in tests.
type PacketReader interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	Close() error
}

// swappableSender wraps a PacketWriter so it can be atomically replaced at
// runtime without races with in-flight writes.  The hot-path Write method
// acquires only a read lock, snapshots the current implementation, then
// releases the lock before performing I/O — so no I/O ever happens under a
// write lock.
type swappableSender struct {
	impl PacketWriter
	mu   sync.RWMutex
}

func newSwappableSender(w PacketWriter) *swappableSender {
	return &swappableSender{impl: w}
}

// Write satisfies PacketWriter.
func (s *swappableSender) Write(b []byte) (int, error) {
	s.mu.RLock()
	w := s.impl
	s.mu.RUnlock()

	return w.Write(b)
}

// swap atomically replaces the underlying PacketWriter and returns the
// previous one so the caller can close it.
func (s *swappableSender) swap(newW PacketWriter) PacketWriter {
	s.mu.Lock()
	old := s.impl
	s.impl = newW
	s.mu.Unlock()

	return old
}

// swappableReceiver wraps a PacketReader so it can be atomically replaced at
// runtime without races with the blocking receive loop.
//
// ReadFromUDP snapshots the current implementation under a read lock and then
// releases the lock before blocking, so a concurrent swap is never blocked by
// an in-flight I/O call.  Closing the old connection after the swap unblocks
// any in-progress ReadFromUDP on the old socket, causing receiveLoop to loop
// back and immediately read from the new socket.
type swappableReceiver struct {
	impl PacketReader
	mu   sync.RWMutex
}

func newSwappableReceiver(r PacketReader) *swappableReceiver {
	return &swappableReceiver{impl: r}
}

// ReadFromUDP satisfies PacketReader.
func (r *swappableReceiver) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	r.mu.RLock()
	impl := r.impl
	r.mu.RUnlock()

	return impl.ReadFromUDP(b)
}

// Close satisfies PacketReader and closes the current underlying reader.
func (r *swappableReceiver) Close() error {
	r.mu.RLock()
	impl := r.impl
	r.mu.RUnlock()

	return impl.Close()
}

// swap atomically replaces the underlying PacketReader and returns the
// previous one so the caller can close it (which unblocks any in-flight
// ReadFromUDP on the old connection).
func (r *swappableReceiver) swap(newR PacketReader) PacketReader {
	r.mu.Lock()
	old := r.impl
	r.impl = newR
	r.mu.Unlock()

	return old
}
