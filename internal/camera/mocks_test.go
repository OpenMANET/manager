package camera

import (
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
}

func (w *fakeMulticastPacketWriter) SetMulticastInterface(bridge *net.Interface) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.bridge = bridge

	return w.InterfaceErr
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

	return 1, w.WriteErr
}

func (w *fakeMulticastPacketWriter) state() (*net.Interface, int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.bridge, w.ttl, w.writes
}
