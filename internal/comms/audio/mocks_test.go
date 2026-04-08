package audio

import (
	"sync"
	"sync/atomic"
	"time"
)

// fakeCaptureStream satisfies captureStream so encoder tests can run
// without opening real audio hardware. Start/Stop/Close error injection
// works the same way as the parent package's mockStream.
type fakeCaptureStream struct {
	startErr   error
	stopErr    error
	closeErr   error
	info       streamInfo
	startCalls int
	stopCalls  int
	closeCalls int
}

func (f *fakeCaptureStream) Start() error {
	f.startCalls++

	return f.startErr
}

func (f *fakeCaptureStream) Stop() error {
	f.stopCalls++

	return f.stopErr
}

func (f *fakeCaptureStream) Close() error {
	f.closeCalls++

	return f.closeErr
}

func (f *fakeCaptureStream) Info() streamInfo { return f.info }

// mockEncoder satisfies codec.AudioEncoder. Each call records the input
// PCM frame (length only, to avoid copying samples) and returns either
// encodeErr or a fake fixed-size payload. sleepDur, if non-zero, makes
// every EncodeS16 sleep for that duration so encode-duration tracking
// can be exercised deterministically.
type mockEncoder struct {
	encodeErr error
	sleepDur  time.Duration
	calls     atomic.Int64
	payloadN  int
}

func (m *mockEncoder) Encode(pcm []int16, data []byte) (int, error) {
	return m.EncodeS16(pcm, data)
}

func (m *mockEncoder) EncodeS16(_ []int16, data []byte) (int, error) {
	m.calls.Add(1)

	if m.sleepDur > 0 {
		time.Sleep(m.sleepDur)
	}

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
