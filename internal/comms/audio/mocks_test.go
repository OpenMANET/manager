package audio

import (
	"sync"
	"sync/atomic"

	"github.com/gordonklaus/portaudio"
)

// fakePAStream satisfies paStream so encoder tests can run without opening
// real audio hardware. Start/Stop/Close error injection works the same way
// as the parent's mockStream.
type fakePAStream struct {
	startErr   error
	stopErr    error
	closeErr   error
	info       *portaudio.StreamInfo
	startCalls int
	stopCalls  int
	closeCalls int
}

func (f *fakePAStream) Start() error {
	f.startCalls++

	return f.startErr
}

func (f *fakePAStream) Stop() error {
	f.stopCalls++

	return f.stopErr
}

func (f *fakePAStream) Close() error {
	f.closeCalls++

	return f.closeErr
}

func (f *fakePAStream) Info() *portaudio.StreamInfo { return f.info }

// mockEncoder satisfies codec.AudioEncoder. Each call records the input
// PCM frame (length only, to avoid copying samples) and returns either
// encodeErr or a fake fixed-size payload.
type mockEncoder struct {
	encodeErr error
	calls     atomic.Int64
	payloadN  int
}

func (m *mockEncoder) Encode(pcm []int16, data []byte) (int, error) {
	return m.EncodeS16(pcm, data)
}

func (m *mockEncoder) EncodeS16(_ []int16, data []byte) (int, error) {
	m.calls.Add(1)

	if m.encodeErr != nil {
		return 0, m.encodeErr
	}

	n := m.payloadN
	if n == 0 {
		n = 8
	}

	if n > len(data) {
		n = len(data)
	}

	for i := range n {
		data[i] = byte(i)
	}

	return n, nil
}

func (m *mockEncoder) Close() error { return nil }

// recordingSink captures every payload handed to a SendFn so encoder tests
// can assert on the number of forwarded frames and their contents.
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
