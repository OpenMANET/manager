package handlers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
)

// fakeVisibility scripts a deterministic sequence of VisDoc returns so
// DeltaTracker tests can exercise add/remove edges over a known timeline.
type fakeVisibility struct {
	mu   sync.Mutex
	docs []*batmanadv.VisDoc
	errs []error
	idx  int
}

func (f *fakeVisibility) GetVisibility() (*batmanadv.VisDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.idx
	if i >= len(f.docs) {
		i = len(f.docs) - 1
	}

	f.idx++

	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}

	return f.docs[i], err
}

// callCount returns the number of completed GetVisibility calls, safely for
// concurrent reads from test goroutines.
func (f *fakeVisibility) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.idx
}

// fakeGateways scripts a deterministic sequence of gateway sets.
type fakeGateways struct {
	mu   sync.Mutex
	sets []map[string]struct{}
	idx  int
}

func (f *fakeGateways) ListGateways() (map[string]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.idx
	if i >= len(f.sets) {
		i = len(f.sets) - 1
	}

	f.idx++

	return f.sets[i], nil
}

// callCount returns the number of completed ListGateways calls, safely for
// concurrent reads from test goroutines.
func (f *fakeGateways) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.idx
}

// mkVisDoc builds a VisDoc with a single node reporting the supplied
// (router -> neighbor) edges.
func mkVisDoc(router string, neighbors ...string) *batmanadv.VisDoc {
	ns := make([]batmanadv.VisNeighbor, 0, len(neighbors))
	for _, n := range neighbors {
		ns = append(ns, batmanadv.VisNeighbor{Router: router, Neighbor: n, Metric: "1.0"})
	}

	return &batmanadv.VisDoc{
		SourceVersion: "test",
		Algorithm:     15,
		Vis: []batmanadv.VisEntry{
			{Primary: router, Neighbors: ns},
		},
	}
}

func TestDeltaTracker_WindowRoutesAddedAndLost(t *testing.T) {
	vis := &fakeVisibility{
		docs: []*batmanadv.VisDoc{
			mkVisDoc("aa:01", "bb:01"),
			mkVisDoc("aa:01", "bb:01", "bb:02"),
			mkVisDoc("aa:01", "bb:02"),
		},
	}

	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), vis, gw, 10*time.Millisecond, 120)
	tracker.DefaultWindow = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)

	require.Eventually(t, tracker.Ready, time.Second, 5*time.Millisecond, "tracker never became ready")

	// Wait for all three scripted docs to have been consumed.
	require.Eventually(t, func() bool {
		return vis.callCount() >= 3
	}, time.Second, 5*time.Millisecond)

	tracker.Stop()

	result := tracker.Window(5 * time.Second)

	assert.GreaterOrEqual(t, result.RoutesAdded, uint32(1), "expected at least one route added")
	assert.GreaterOrEqual(t, result.RoutesLost, uint32(1), "expected at least one route lost")
	assert.Equal(t, uint32(0), result.GatewayChanges)
}

func TestDeltaTracker_GatewayChanges(t *testing.T) {
	vis := &fakeVisibility{
		docs: []*batmanadv.VisDoc{mkVisDoc("aa:01"), mkVisDoc("aa:01"), mkVisDoc("aa:01")},
	}

	gw := &fakeGateways{
		sets: []map[string]struct{}{
			{"aa:01": {}},
			{"aa:01": {}, "aa:02": {}},
			{"aa:02": {}},
		},
	}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), vis, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return gw.callCount() >= 3 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	// add+add=1, then drop+add=2, total = 3.
	assert.GreaterOrEqual(t, result.GatewayChanges, uint32(2))
}

func TestDeltaTracker_ReconvergeZeroWhenStable(t *testing.T) {
	vis := &fakeVisibility{
		docs: []*batmanadv.VisDoc{
			mkVisDoc("aa:01", "bb:01"),
			mkVisDoc("aa:01", "bb:01"),
			mkVisDoc("aa:01", "bb:01"),
		},
	}
	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), vis, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return vis.callCount() >= 3 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	assert.Equal(t, uint32(0), result.RoutesAdded)
	assert.Equal(t, uint32(0), result.RoutesLost)
	assert.Equal(t, time.Duration(0), result.Reconverge)
}

func TestDeltaTracker_ToleratesVisUnavailable(t *testing.T) {
	vis := &fakeVisibility{
		docs: []*batmanadv.VisDoc{
			mkVisDoc("aa:01", "bb:01"),
			nil,
			mkVisDoc("aa:01", "bb:01"),
		},
		errs: []error{nil, batmanadv.ErrVisUnavailable, nil},
	}

	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), vis, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return vis.callCount() >= 3 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	// The middle sample had a vis-unavailable error; the tracker should still
	// have three samples (each failure still appends an empty sample) and the
	// churn counters should reflect a lost-then-added pair for the edge.
	result := tracker.Window(5 * time.Second)
	assert.GreaterOrEqual(t, result.RoutesAdded+result.RoutesLost, uint32(2))
}

func TestDeltaTracker_ShutsDownCleanly(t *testing.T) {
	vis := &fakeVisibility{docs: []*batmanadv.VisDoc{mkVisDoc("aa:01")}}
	gw := &fakeGateways{sets: []map[string]struct{}{{}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), vis, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())

	tracker.Start(ctx)
	cancel()

	done := make(chan struct{})

	go func() {
		tracker.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tracker did not stop within 2 seconds after context cancel")
	}

	// Idempotent stop must not panic.
	tracker.Stop()

	// Calling Window on a ring with 0 or 1 samples returns an empty result
	// rather than panicking — guard against partial-start regressions.
	result := tracker.Window(0)
	assert.Equal(t, uint32(0), result.RoutesAdded)
}

func TestDeltaTracker_EmptyRingReturnsZero(t *testing.T) {
	tracker := handlers.NewDeltaTracker(
		zerolog.Nop(),
		&fakeVisibility{docs: []*batmanadv.VisDoc{mkVisDoc("aa:01")}},
		&fakeGateways{sets: []map[string]struct{}{{}}},
		10*time.Millisecond,
		120,
	)

	result := tracker.Window(60 * time.Second)
	assert.Equal(t, uint32(0), result.RoutesAdded)
	assert.Equal(t, uint32(0), result.RoutesLost)
	assert.Equal(t, time.Duration(0), result.ActualWindow)
}

// TestBatctlGatewayProvider_ListGateways_NotFoundReturnsError confirms the
// production gateway provider surfaces errors from batctl (the binary is not
// present in the test environment) rather than silently masking them. We
// don't assert the exact error text — only that we get one.
func TestBatctlGatewayProvider_ListGateways_NotFoundReturnsError(t *testing.T) {
	p := handlers.BatctlGatewayProvider{}
	_, err := p.ListGateways()
	require.Error(t, err)

	// The production implementation wraps the batctl exec error; the
	// tracker logs it but does not crash. Ensure we don't accidentally
	// return an unavailable-sentinel that would be ignored.
	assert.False(t, errors.Is(err, batmanadv.ErrVisUnavailable))
}
