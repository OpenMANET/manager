package camera

import (
	"context"
	"net"
	"sync"
	"testing"

	"golang.org/x/net/ipv4"
)

type fakeConfigReader struct {
	mu sync.RWMutex // protects the fields below

	values map[string][]string
}

func newFakeConfigReader(t *testing.T, values map[string][]string) *fakeConfigReader {
	t.Helper()

	return &fakeConfigReader{values: values}
}

func (r *fakeConfigReader) Get(config, section, option string) ([]string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	value, ok := r.values[config+"."+section+"."+option]

	return value, ok
}

type fakeMulticastPacketWriter struct {
	mu sync.Mutex // protects the fields below

	bridge *net.Interface
	ttl    int
	writes int

	InterfaceErr error
	TTLErr       error
	WriteErr     error
	WriteErrAt   int
	Cancel       context.CancelFunc
}

func (w *fakeMulticastPacketWriter) SetMulticastInterface(bridge *net.Interface) error {
	w.mu.Lock()
	w.bridge = bridge
	err := w.InterfaceErr
	cancel := w.Cancel
	w.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	return err
}

func (w *fakeMulticastPacketWriter) SetMulticastTTL(ttl int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.ttl = ttl

	return w.TTLErr
}

func (w *fakeMulticastPacketWriter) WriteTo([]byte, *ipv4.ControlMessage, net.Addr) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.writes++

	if w.WriteErrAt == 0 || w.writes == w.WriteErrAt {
		return 1, w.WriteErr
	}

	return 1, nil
}

func (w *fakeMulticastPacketWriter) state() (*net.Interface, int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.bridge, w.ttl, w.writes
}
