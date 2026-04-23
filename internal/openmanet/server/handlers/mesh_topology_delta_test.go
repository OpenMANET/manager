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

// fakeOrigScript scripts a deterministic sequence of OriginatorTopology
// returns so DeltaTracker tests can walk a known timeline of route changes.
type fakeOrigScript struct {
	mu    sync.Mutex
	snaps []*batmanadv.OriginatorTopology
	errs  []error
	idx   int
}

func (f *fakeOrigScript) GetOriginatorTopology() (*batmanadv.OriginatorTopology, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.idx
	if i >= len(f.snaps) {
		i = len(f.snaps) - 1
	}

	f.idx++

	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}

	return f.snaps[i], err
}

// callCount returns the number of completed GetOriginatorTopology calls.
func (f *fakeOrigScript) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.idx
}

// fakeGateways scripts a deterministic sequence of gateway sets — unchanged
// from the pre-originator test file; the gateway path is independent of the
// topology data source.
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

func (f *fakeGateways) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.idx
}

// mkSnap builds a minimal OriginatorTopology with one best-route row per
// (orig, nextHop, iface) tuple supplied. Tests use it to stitch together
// route churn timelines without spelling out every field.
func mkSnap(entries ...batmanadv.OriginatorEntry) *batmanadv.OriginatorTopology {
	return &batmanadv.OriginatorTopology{
		Algorithm:   "BATMAN_IV",
		Originators: entries,
	}
}

func entry(orig, next, iface string) batmanadv.OriginatorEntry {
	return batmanadv.OriginatorEntry{
		OrigMAC:    orig,
		NextHopMAC: next,
		HardIfname: iface,
		Hops:       1,
	}
}

// TestDeltaTracker_WindowRoutesAddedAndLost walks three snapshots that add
// and then lose a route, and asserts the window aggregation matches.
func TestDeltaTracker_WindowRoutesAddedAndLost(t *testing.T) {
	orig := &fakeOrigScript{
		snaps: []*batmanadv.OriginatorTopology{
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
			mkSnap(entry("bb:01", "bb:01", "wlan0"), entry("bb:02", "bb:02", "wlan0")),
			mkSnap(entry("bb:02", "bb:02", "wlan0")),
		},
	}
	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), orig, gw, 10*time.Millisecond, 120)
	tracker.DefaultWindow = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)

	require.Eventually(t, tracker.Ready, time.Second, 5*time.Millisecond, "tracker never became ready")
	require.Eventually(t, func() bool { return orig.callCount() >= 3 }, time.Second, 5*time.Millisecond)

	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	assert.GreaterOrEqual(t, result.RoutesAdded, uint32(1), "expected at least one route added")
	assert.GreaterOrEqual(t, result.RoutesLost, uint32(1), "expected at least one route lost")
	assert.Equal(t, uint32(0), result.GatewayChanges)
}

// TestDeltaTracker_RouteChangeOnInterfaceFlip verifies that a failover from
// wlan0 to phy2-mesh0 (same orig, same next hop, different iface) registers
// as a route change — this was the whole point of including hardIfname in
// the edge key.
func TestDeltaTracker_RouteChangeOnInterfaceFlip(t *testing.T) {
	orig := &fakeOrigScript{
		snaps: []*batmanadv.OriginatorTopology{
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
			mkSnap(entry("bb:01", "bb:01", "phy2-mesh0")),
		},
	}
	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), orig, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return orig.callCount() >= 2 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	assert.GreaterOrEqual(t, result.RoutesAdded+result.RoutesLost, uint32(2),
		"interface flip should count as both a loss (old) and an add (new)")
}

// TestDeltaTracker_GatewayChanges keeps the pre-existing gateway-churn
// assertion — the gateway feed doesn't depend on the originator provider.
func TestDeltaTracker_GatewayChanges(t *testing.T) {
	orig := &fakeOrigScript{
		snaps: []*batmanadv.OriginatorTopology{mkSnap(), mkSnap(), mkSnap()},
	}
	gw := &fakeGateways{
		sets: []map[string]struct{}{
			{"aa:01": {}},
			{"aa:01": {}, "aa:02": {}},
			{"aa:02": {}},
		},
	}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), orig, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return gw.callCount() >= 3 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	assert.GreaterOrEqual(t, result.GatewayChanges, uint32(2))
}

// TestDeltaTracker_ReconvergeZeroWhenStable asserts a perfectly stable mesh
// produces zero reconverge time — no unstable samples ⇒ no spread.
func TestDeltaTracker_ReconvergeZeroWhenStable(t *testing.T) {
	orig := &fakeOrigScript{
		snaps: []*batmanadv.OriginatorTopology{
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
		},
	}
	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), orig, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return orig.callCount() >= 3 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	assert.Equal(t, uint32(0), result.RoutesAdded)
	assert.Equal(t, uint32(0), result.RoutesLost)
	assert.Equal(t, time.Duration(0), result.Reconverge)
}

// TestDeltaTracker_ToleratesUnavailable treats the sentinel as an empty
// snapshot — the tracker keeps sampling and the churn counters pick up the
// resulting add/lost pair on either side of the outage.
func TestDeltaTracker_ToleratesUnavailable(t *testing.T) {
	orig := &fakeOrigScript{
		snaps: []*batmanadv.OriginatorTopology{
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
			nil,
			mkSnap(entry("bb:01", "bb:01", "wlan0")),
		},
		errs: []error{nil, batmanadv.ErrOriginatorsUnavailable, nil},
	}
	gw := &fakeGateways{sets: []map[string]struct{}{{}, {}, {}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), orig, gw, 10*time.Millisecond, 120)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	tracker.Start(ctx)
	require.Eventually(t, func() bool { return orig.callCount() >= 3 }, time.Second, 5*time.Millisecond)
	tracker.Stop()

	result := tracker.Window(5 * time.Second)
	assert.GreaterOrEqual(t, result.RoutesAdded+result.RoutesLost, uint32(2))
}

// TestDeltaTracker_ShutsDownCleanly confirms a canceled context tears the
// worker goroutine down quickly and that Stop is idempotent.
func TestDeltaTracker_ShutsDownCleanly(t *testing.T) {
	orig := &fakeOrigScript{snaps: []*batmanadv.OriginatorTopology{mkSnap()}}
	gw := &fakeGateways{sets: []map[string]struct{}{{}}}

	tracker := handlers.NewDeltaTracker(zerolog.Nop(), orig, gw, 10*time.Millisecond, 120)
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

	tracker.Stop() // idempotent
	result := tracker.Window(0)
	assert.Equal(t, uint32(0), result.RoutesAdded)
}

// TestDeltaTracker_EmptyRingReturnsZero asserts Window on a tracker that was
// never started returns an empty (non-panicking) result.
func TestDeltaTracker_EmptyRingReturnsZero(t *testing.T) {
	tracker := handlers.NewDeltaTracker(
		zerolog.Nop(),
		&fakeOrigScript{snaps: []*batmanadv.OriginatorTopology{mkSnap()}},
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
// production gateway provider surfaces batctl errors rather than silently
// swallowing them. The binary is not present in the test environment.
func TestBatctlGatewayProvider_ListGateways_NotFoundReturnsError(t *testing.T) {
	p := handlers.BatctlGatewayProvider{}
	_, err := p.ListGateways()
	require.Error(t, err)

	assert.False(t, errors.Is(err, batmanadv.ErrOriginatorsUnavailable))
}
