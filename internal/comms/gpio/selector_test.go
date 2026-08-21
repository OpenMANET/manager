package gpio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLines is a hand-rolled lineGroup: tests set vals and fire the
// captured edge handler.
type fakeLines struct {
	mu      sync.Mutex
	vals    [5]int
	valsErr error
	closed  bool
	// valuesCalled, when non-nil, receives one token per Values call so
	// tests can serialize edge firings against the watch goroutine's
	// reads without sleeping. Buffered; send is non-blocking.
	valuesCalled chan struct{}
}

func (f *fakeLines) Values(out []int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.valuesCalled != nil {
		select {
		case f.valuesCalled <- struct{}{}:
		default:
		}
	}

	if f.valsErr != nil {
		return f.valsErr
	}

	copy(out, f.vals[:])

	return nil
}

func (f *fakeLines) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true

	return nil
}

func (f *fakeLines) set(vals [5]int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.vals = vals
}

func (f *fakeLines) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.valsErr = err
}

func newFakeSelector(initial [5]int) (*Selector, *fakeLines, *func()) {
	fl := &fakeLines{vals: initial}

	var handler func()

	s := &Selector{
		Log: zerolog.Nop(),
		openFn: func(h func()) (lineGroup, error) {
			handler = h

			return fl, nil
		},
	}

	return s, fl, &handler
}

func recvChannel(t *testing.T, ch <-chan int) int {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for selector event")

		return 0
	}
}

func TestSelector_InitialStateApplied(t *testing.T) {
	// Position 3 active (line index 2 low); all others high (pull-up).
	s, _, _ := newFakeSelector([5]int{1, 1, 0, 1, 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := s.Events(ctx)
	require.NoError(t, err)

	assert.Equal(t, 3, recvChannel(t, events))
}

func TestSelector_EdgeSelectsChannel(t *testing.T) {
	s, fl, handler := newFakeSelector([5]int{0, 1, 1, 1, 1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := s.Events(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, recvChannel(t, events))

	fl.set([5]int{1, 1, 1, 1, 0})
	(*handler)()

	assert.Equal(t, 5, recvChannel(t, events))
}

func TestSelector_GlitchHoldsLastSelection(t *testing.T) {
	tests := []struct {
		name string
		vals [5]int
	}{
		{name: "rotary in transit, none active", vals: [5]int{1, 1, 1, 1, 1}},
		{name: "two active, wiring fault", vals: [5]int{0, 0, 1, 1, 1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, fl, handler := newFakeSelector([5]int{1, 0, 1, 1, 1})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			events, err := s.Events(ctx)
			require.NoError(t, err)
			assert.Equal(t, 2, recvChannel(t, events))

			fl.set(tc.vals)
			(*handler)()

			select {
			case v := <-events:
				t.Fatalf("glitch emitted %d; want hold", v)
			case <-time.After(50 * time.Millisecond):
			}

			var snap SelectorSnapshot

			s.Snapshot(&snap)
			assert.Equal(t, int64(1), snap.HeldGlitches)
		})
	}
}

func TestSelector_CtxCancelClosesAndReleasesLines(t *testing.T) {
	s, fl, _ := newFakeSelector([5]int{0, 1, 1, 1, 1})

	ctx, cancel := context.WithCancel(context.Background())

	events, err := s.Events(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, recvChannel(t, events))

	cancel()

	for range events { //nolint:revive // drain until close
	}

	fl.mu.Lock()
	defer fl.mu.Unlock()

	assert.True(t, fl.closed)
}

func TestSelector_SnapshotNilSafe(t *testing.T) {
	var s *Selector

	var snap SelectorSnapshot

	assert.NotPanics(t, func() { s.Snapshot(&snap) })
}

// TestSelector_ReadErrorBreakerClosesAndReleases pins the error breaker:
// after readErrorBreaker consecutive Values failures the watch goroutine
// closes the events channel and releases the lines. Each edge firing is
// serialized against the watcher's read via valuesCalled, so edges never
// coalesce and every firing produces exactly one failed read.
func TestSelector_ReadErrorBreakerClosesAndReleases(t *testing.T) {
	s, fl, handler := newFakeSelector([5]int{0, 1, 1, 1, 1})
	fl.valuesCalled = make(chan struct{}, readErrorBreaker+1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := s.Events(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, recvChannel(t, events))
	waitValuesCall(t, fl) // the successful boot read

	fl.setErr(errors.New("ioctl: line request revoked"))

	for range readErrorBreaker {
		(*handler)()
		waitValuesCall(t, fl)
	}

	select {
	case v, ok := <-events:
		require.False(t, ok, "breaker must close the channel, got value %d", v)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for events channel to close")
	}

	fl.mu.Lock()
	defer fl.mu.Unlock()

	assert.True(t, fl.closed, "lines released after breaker trip")
}

func waitValuesCall(t *testing.T, fl *fakeLines) {
	t.Helper()

	select {
	case <-fl.valuesCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a Values read")
	}
}
