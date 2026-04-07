package comms

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// swapCloseGrace is how long a swap defers closing the previous PacketWriter
// so any in-flight lock-free Write calls against it can finish their single
// UDP syscall. Writes on the hot path take no lock at all (they only do an
// atomic Load), so using a WaitGroup would panic ("Add called concurrently
// with Wait"); instead we bound the grace period and rely on the fact that
// Write's critical path is a single non-blocking sendto(2).
const swapCloseGrace = 50 * time.Millisecond

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
// runtime without races with in-flight writes.
//
// Concurrency model:
//
//   - The hot path (Write) is lock-free: it atomically loads the current
//     PacketWriter pointer and performs the single underlying UDP sendto(2).
//     No mutex and no WaitGroup increment, so concurrent writers do not
//     contend against each other or against swappers.
//   - swap() serializes concurrent swappers via swapMu, publishes the new
//     PacketWriter via atomic.Pointer.Store(), and returns the previous one.
//     The caller is responsible for closing the previous PacketWriter.
//   - Because the hot path holds no lock, swap cannot prove that writers are
//     done with the previous pointer before it returns. Callers that close
//     the returned PacketWriter synchronously risk closing a socket out from
//     under an in-flight sendto(2). To avoid this, swap returns a deferred
//     closer via swapAndDeferClose which schedules the underlying close
//     after swapCloseGrace — long enough for any in-flight write syscall to
//     complete. This is the tradeoff the refactor plan explicitly allows as
//     the fallback to a WaitGroup-based drain (the WaitGroup approach fails
//     with "Add called concurrently with Wait" because writers do not take
//     any lock that could be ordered against Wait).
//   - Close takes a snapshot and closes it if it implements io.Closer.
type swappableSender struct {
	impl   atomic.Pointer[PacketWriter]
	swapMu sync.Mutex // serializes swappers; writes do not take this lock
}

func newSwappableSender(w PacketWriter) *swappableSender {
	s := &swappableSender{}
	s.impl.Store(&w)

	return s
}

// Write satisfies PacketWriter. Fully lock-free on the hot path: a single
// atomic pointer load, then the underlying Write (one sendto(2) on the UDP
// fast path).
func (s *swappableSender) Write(b []byte) (int, error) {
	wp := s.impl.Load()

	return (*wp).Write(b)
}

// swap atomically replaces the underlying PacketWriter and returns the
// previous one so the caller can close it. The caller must not close the
// returned PacketWriter synchronously — see swapAndDeferClose for the safe
// close path that honours the swapCloseGrace window for in-flight writes.
func (s *swappableSender) swap(newW PacketWriter) PacketWriter {
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	oldPtr := s.impl.Load()
	s.impl.Store(&newW)

	return *oldPtr
}

// swapAndDeferClose replaces the underlying PacketWriter with newW and
// schedules the previous one's Close (if it implements io.Closer) after
// swapCloseGrace. The grace window lets any in-flight lock-free Write on the
// old impl finish its single sendto(2) before the underlying fd is closed.
func (s *swappableSender) swapAndDeferClose(newW PacketWriter) {
	old := s.swap(newW)

	closer, ok := old.(io.Closer)
	if !ok {
		return
	}

	time.AfterFunc(swapCloseGrace, func() { _ = closer.Close() })
}

// Close closes the current underlying PacketWriter if it implements io.Closer.
func (s *swappableSender) Close() error {
	wp := s.impl.Load()
	if c, ok := (*wp).(io.Closer); ok {
		return c.Close()
	}

	return nil
}

// swappableReceiver wraps a PacketReader so it can be atomically replaced at
// runtime without races with the blocking receive loop.
//
// ReadFromUDP snapshots the current implementation under a read lock and then
// releases the lock before blocking, so a concurrent swap is never blocked by
// an in-flight I/O call. Closing the old connection after the swap unblocks
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
