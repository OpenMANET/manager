package handlers_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nopVisFetch satisfies the snapshotter's vis fetcher without forking the
// batadv-vis binary; the default fetcher would exec the real tool (which
// is absent from the CI environment) and fail the tests that don't care
// about vis data.
func nopVisFetch(context.Context) (*batmanadv.VisDoc, error) {
	return nil, batmanadv.ErrVisUnavailable
}

// newTestSnapshotter builds a BatctlSnapshotter whose fetchers are all
// fake and whose Interval is deliberately long so refreshes only happen
// when the test explicitly invokes refresh() via Start.
func newTestSnapshotter(
	origFn func() ([]batmanadv.Originator, error),
	neighFn func() (*batmanadv.Neighbors, error),
	cfgFn func(string) (*batmanadv.MeshConfig, error),
	gwFn func(string) (*batmanadv.Gateways, error),
	hostsFn func(string) (*batmanadv.BatHosts, error),
) *handlers.BatctlSnapshotter {
	return &handlers.BatctlSnapshotter{
		Log:              zerolog.Nop(),
		Interval:         time.Hour,
		Iface:            "bat0",
		BatHostsPath:     "/tmp/bat-hosts",
		FetchOriginators: origFn,
		FetchNeighbors:   neighFn,
		FetchMeshConfig:  cfgFn,
		FetchGateways:    gwFn,
		FetchBatHosts:    hostsFn,
		FetchVis:         nopVisFetch,
	}
}

func TestBatctlSnapshotter_AccessorsReturnNotReadyBeforeStart(t *testing.T) {
	s := newTestSnapshotter(nil, nil, nil, nil, nil)

	_, err := s.GetOriginators()
	require.ErrorIs(t, err, handlers.ErrBatctlSnapshotNotReady)

	_, err = s.GetMeshNeighbors()
	require.ErrorIs(t, err, handlers.ErrBatctlSnapshotNotReady)

	_, err = s.GetMeshConfig("bat0")
	require.ErrorIs(t, err, handlers.ErrBatctlSnapshotNotReady)

	_, err = s.GetMeshGateways("bat0")
	require.ErrorIs(t, err, handlers.ErrBatctlSnapshotNotReady)

	_, err = s.ParseBatHosts("/tmp/bat-hosts")
	require.ErrorIs(t, err, handlers.ErrBatctlSnapshotNotReady)

	_, err = s.GetMeshVis(context.Background())
	require.ErrorIs(t, err, handlers.ErrBatctlSnapshotNotReady)
}

func TestBatctlSnapshotter_StartWarmsCache(t *testing.T) {
	wantOrig := []batmanadv.Originator{{OrigAddress: "aa:bb:cc:dd:ee:ff"}}
	wantNeighbors := &batmanadv.Neighbors{}
	wantCfg := &batmanadv.MeshConfig{}
	wantGws := &batmanadv.Gateways{}
	wantHosts := &batmanadv.BatHosts{}

	s := newTestSnapshotter(
		func() ([]batmanadv.Originator, error) { return wantOrig, nil },
		func() (*batmanadv.Neighbors, error) { return wantNeighbors, nil },
		func(string) (*batmanadv.MeshConfig, error) { return wantCfg, nil },
		func(string) (*batmanadv.Gateways, error) { return wantGws, nil },
		func(string) (*batmanadv.BatHosts, error) { return wantHosts, nil },
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	gotOrig, err := s.GetOriginators()
	require.NoError(t, err)
	assert.Equal(t, wantOrig, gotOrig)

	gotNeighbors, err := s.GetMeshNeighbors()
	require.NoError(t, err)
	assert.Same(t, wantNeighbors, gotNeighbors)

	gotCfg, err := s.GetMeshConfig("bat0")
	require.NoError(t, err)
	assert.Same(t, wantCfg, gotCfg)

	gotGws, err := s.GetMeshGateways("bat0")
	require.NoError(t, err)
	assert.Same(t, wantGws, gotGws)

	gotHosts, err := s.ParseBatHosts("/tmp/bat-hosts")
	require.NoError(t, err)
	assert.Same(t, wantHosts, gotHosts)
}

func TestBatctlSnapshotter_AccessorsSurfaceFetchErrors(t *testing.T) {
	wantErr := errors.New("batctl failed")

	s := newTestSnapshotter(
		func() ([]batmanadv.Originator, error) { return nil, wantErr },
		func() (*batmanadv.Neighbors, error) { return nil, wantErr },
		func(string) (*batmanadv.MeshConfig, error) { return nil, wantErr },
		func(string) (*batmanadv.Gateways, error) { return nil, wantErr },
		func(string) (*batmanadv.BatHosts, error) { return nil, wantErr },
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	_, err := s.GetOriginators()
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetMeshNeighbors()
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetMeshConfig("bat0")
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetMeshGateways("bat0")
	assert.ErrorIs(t, err, wantErr)

	_, err = s.ParseBatHosts("/tmp/bat-hosts")
	assert.ErrorIs(t, err, wantErr)
}

func TestBatctlSnapshotter_ListGatewaysLowercasesMACs(t *testing.T) {
	gws := &batmanadv.Gateways{
		{OrigAddress: "AA:BB:CC:DD:EE:FF"},
		{OrigAddress: "11:22:33:44:55:66"},
	}

	s := newTestSnapshotter(
		func() ([]batmanadv.Originator, error) { return nil, nil },
		func() (*batmanadv.Neighbors, error) { return nil, nil },
		func(string) (*batmanadv.MeshConfig, error) { return nil, nil },
		func(string) (*batmanadv.Gateways, error) { return gws, nil },
		func(string) (*batmanadv.BatHosts, error) { return nil, nil },
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	got, err := s.ListGateways()
	require.NoError(t, err)
	assert.Contains(t, got, "aa:bb:cc:dd:ee:ff")
	assert.Contains(t, got, "11:22:33:44:55:66")
	assert.Len(t, got, 2)
}

func TestBatctlSnapshotter_ListGatewaysReturnsEmptyMapWhenNoGateways(t *testing.T) {
	s := newTestSnapshotter(
		func() ([]batmanadv.Originator, error) { return nil, nil },
		func() (*batmanadv.Neighbors, error) { return nil, nil },
		func(string) (*batmanadv.MeshConfig, error) { return nil, nil },
		func(string) (*batmanadv.Gateways, error) { return nil, nil },
		func(string) (*batmanadv.BatHosts, error) { return nil, nil },
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	got, err := s.ListGateways()
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestBatctlSnapshotter_GetMeshVisReturnsCached(t *testing.T) {
	wantDoc := &batmanadv.VisDoc{SourceVersion: "2025.4", Algorithm: 15}

	s := &handlers.BatctlSnapshotter{
		Log:              zerolog.Nop(),
		Interval:         time.Hour,
		Iface:            "bat0",
		FetchOriginators: func() ([]batmanadv.Originator, error) { return nil, nil },
		FetchNeighbors:   func() (*batmanadv.Neighbors, error) { return nil, nil },
		FetchMeshConfig:  func(string) (*batmanadv.MeshConfig, error) { return nil, nil },
		FetchGateways:    func(string) (*batmanadv.Gateways, error) { return nil, nil },
		FetchBatHosts:    func(string) (*batmanadv.BatHosts, error) { return nil, nil },
		FetchVis:         func(context.Context) (*batmanadv.VisDoc, error) { return wantDoc, nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	got, err := s.GetMeshVis(context.Background())
	require.NoError(t, err)
	assert.Same(t, wantDoc, got)
}

func TestBatctlSnapshotter_GetMeshVisSurfacesUnavailable(t *testing.T) {
	s := &handlers.BatctlSnapshotter{
		Log:              zerolog.Nop(),
		Interval:         time.Hour,
		Iface:            "bat0",
		FetchOriginators: func() ([]batmanadv.Originator, error) { return nil, nil },
		FetchNeighbors:   func() (*batmanadv.Neighbors, error) { return nil, nil },
		FetchMeshConfig:  func(string) (*batmanadv.MeshConfig, error) { return nil, nil },
		FetchGateways:    func(string) (*batmanadv.Gateways, error) { return nil, nil },
		FetchBatHosts:    func(string) (*batmanadv.BatHosts, error) { return nil, nil },
		FetchVis: func(context.Context) (*batmanadv.VisDoc, error) {
			return nil, batmanadv.ErrVisUnavailable
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	_, err := s.GetMeshVis(context.Background())
	assert.ErrorIs(t, err, batmanadv.ErrVisUnavailable)
}

func TestBatctlSnapshotter_BackgroundLoopStopsOnContextCancel(t *testing.T) {
	var origCalls atomic.Int64

	s := &handlers.BatctlSnapshotter{
		Log:      zerolog.Nop(),
		Interval: 10 * time.Millisecond,
		Iface:    "bat0",
		FetchOriginators: func() ([]batmanadv.Originator, error) {
			origCalls.Add(1)

			return nil, nil
		},
		FetchNeighbors:  func() (*batmanadv.Neighbors, error) { return nil, nil },
		FetchMeshConfig: func(string) (*batmanadv.MeshConfig, error) { return nil, nil },
		FetchGateways:   func(string) (*batmanadv.Gateways, error) { return nil, nil },
		FetchBatHosts:   func(string) (*batmanadv.BatHosts, error) { return nil, nil },
		FetchVis:        nopVisFetch,
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	// Let the loop tick a couple of times.
	require.Eventually(
		t,
		func() bool { return origCalls.Load() >= 2 },
		500*time.Millisecond,
		5*time.Millisecond,
	)

	cancel()

	// After cancel, the call count must stabilize.
	settled := origCalls.Load()

	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(t, origCalls.Load()-settled, int64(1),
		"goroutine should stop refreshing after context cancel")
}
