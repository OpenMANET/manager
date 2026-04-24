package mgmt

import (
	"testing"
	"time"

	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeshNeighborsWorker_SendOncePublishesExpectedPayload asserts the
// publisher exec's batctl wrappers, maps every neighbor's hard_ifname
// faithfully (including the pre-computed blos flag), and filters the
// originator table down to Best==true rows.
func TestMeshNeighborsWorker_SendOncePublishesExpectedPayload(t *testing.T) {
	fixedTime := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	neighbors := batmanadv.Neighbors{
		{HardIfname: "wlan0", NeighAddress: "AA:AA:AA:AA:AA:01", LastSeenMsecs: 200, Throughput: 54000},
		{HardIfname: "vxlan0", NeighAddress: "bb:bb:bb:bb:bb:01", LastSeenMsecs: 400, Throughput: 100000},
		{HardIfname: "eth0", NeighAddress: "Cc:Cc:Cc:Cc:Cc:01", LastSeenMsecs: 50, Throughput: 1000000},
	}

	originators := []batmanadv.Originator{
		{OrigAddress: "aa:aa:aa:aa:aa:01", BestNeigh: "aa:aa:aa:aa:aa:01", HardIfname: "wlan0", Throughput: 54000, TQ: 220, Best: true},
		// Non-best row — must be filtered out.
		{OrigAddress: "aa:aa:aa:aa:aa:01", BestNeigh: "dd:dd:dd:dd:dd:01", HardIfname: "wlan0", Throughput: 20000, TQ: 120, Best: false},
		{OrigAddress: "bb:bb:bb:bb:bb:01", BestNeigh: "bb:bb:bb:bb:bb:01", HardIfname: "vxlan0", Throughput: 100000, TQ: 0, Best: true},
	}

	meshCfg := &batmanadv.MeshConfig{AlgoName: "BATMAN_V"}

	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{
			Log:          zerolog.Nop(),
			BatInterface: "bat0",
		},
		Interval: time.Second,
		Now:      func() time.Time { return fixedTime },
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return &neighbors, nil },
		func() ([]batmanadv.Originator, error) { return originators, nil },
		func() (*batmanadv.MeshConfig, error) { return meshCfg, nil },
		func(name string) network.NetworkInterface {
			require.Equal(t, "bat0", name)

			return network.NetworkInterface{MAC: "11:22:33:44:55:66"}
		},
		func() (string, error) { return "BCM2711-1003_bat0", nil },
	)
	require.NoError(t, err)
	require.Equal(t, 1, fake.setCalls, "exactly one publish per tick")

	got := &netv1.MeshNeighbors{}
	require.NoError(t, got.UnmarshalVT(fake.lastData))

	assert.Equal(t, "11:22:33:44:55:66", got.GetPrimaryMac())
	assert.Equal(t, "BCM2711-1003", got.GetHostname(), "iface suffix stripped")
	assert.Equal(t, int32(15), got.GetAlgorithm(), "BATMAN_V → 15")
	assert.Equal(t, fixedTime.Unix(), got.GetCollectedAt().GetSeconds())

	require.Len(t, got.GetNeighbors(), 3)

	byMac := map[string]*netv1.MeshNeighbor{}
	for _, n := range got.GetNeighbors() {
		byMac[n.GetMac()] = n
	}

	wlan, ok := byMac["aa:aa:aa:aa:aa:01"]
	require.True(t, ok)
	assert.Equal(t, "wlan0", wlan.GetHardIfname())
	assert.False(t, wlan.GetBlos(), "wlan0 is not BLOS")
	assert.Equal(t, int32(200), wlan.GetLastSeenMsecs())
	assert.Equal(t, int64(54000), wlan.GetThroughputKbps())

	vxlan, ok := byMac["bb:bb:bb:bb:bb:01"]
	require.True(t, ok)
	assert.Equal(t, "vxlan0", vxlan.GetHardIfname())
	assert.True(t, vxlan.GetBlos(), "vxlan0 is the BLOS interface")

	_, ok = byMac["cc:cc:cc:cc:cc:01"]
	assert.True(t, ok, "mixed-case MACs are normalized to lowercase")

	// Originators: only the two Best==true rows survive.
	require.Len(t, got.GetOriginators(), 2)

	origByMac := map[string]*netv1.Originator{}
	for _, o := range got.GetOriginators() {
		origByMac[o.GetOrigMac()] = o
	}

	require.Contains(t, origByMac, "aa:aa:aa:aa:aa:01")
	assert.Equal(t, int32(220), origByMac["aa:aa:aa:aa:aa:01"].GetTq())
	assert.Equal(t, "wlan0", origByMac["aa:aa:aa:aa:aa:01"].GetHardIfname())
}

// TestMeshNeighborsWorker_OriginatorFailureStillPublishesNeighbors asserts
// the publish continues with an empty originator slice when `batctl oj`
// fails — neighbor data is useful on its own.
func TestMeshNeighborsWorker_OriginatorFailureStillPublishesNeighbors(t *testing.T) {
	neighbors := batmanadv.Neighbors{
		{HardIfname: "wlan0", NeighAddress: "aa:aa:aa:aa:aa:01", LastSeenMsecs: 100, Throughput: 30000},
	}

	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{Log: zerolog.Nop(), BatInterface: "bat0"},
		Now:    func() time.Time { return time.Unix(0, 0) },
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return &neighbors, nil },
		func() ([]batmanadv.Originator, error) { return nil, assert.AnError },
		func() (*batmanadv.MeshConfig, error) { return &batmanadv.MeshConfig{AlgoName: "BATMAN_IV"}, nil },
		func(string) network.NetworkInterface { return network.NetworkInterface{MAC: "aa:bb:cc:dd:ee:ff"} },
		func() (string, error) { return "node", nil },
	)
	require.NoError(t, err, "orig failure must not abort the publish")

	got := &netv1.MeshNeighbors{}
	require.NoError(t, got.UnmarshalVT(fake.lastData))
	assert.Len(t, got.GetNeighbors(), 1)
	assert.Empty(t, got.GetOriginators(), "originators empty when batctl oj fails")
	assert.Equal(t, int32(4), got.GetAlgorithm(), "BATMAN_IV → 4")
}

// TestMeshNeighborsWorker_NeighborFailureAborts asserts a batctl nj
// failure is fatal for the tick (nothing useful to say without it).
func TestMeshNeighborsWorker_NeighborFailureAborts(t *testing.T) {
	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{Log: zerolog.Nop(), BatInterface: "bat0"},
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return nil, assert.AnError },
		func() ([]batmanadv.Originator, error) { return nil, nil },
		func() (*batmanadv.MeshConfig, error) { return &batmanadv.MeshConfig{}, nil },
		func(string) network.NetworkInterface { return network.NetworkInterface{} },
		func() (string, error) { return "", nil },
	)
	require.Error(t, err)
	assert.Zero(t, fake.setCalls, "no publish happens when neighbor fetch fails")
}

// TestMeshNeighborsWorker_UnknownAlgorithmFallsBackToZero covers the
// case where `batctl mj` returns a value we don't recognize. The
// publish still ships; consumers treat algorithm=0 as "unknown".
func TestMeshNeighborsWorker_UnknownAlgorithmFallsBackToZero(t *testing.T) {
	fake := &fakeAlfredClient{}

	worker := &MeshNeighborsWorker{
		Config: &ManagementConfig{Log: zerolog.Nop(), BatInterface: "bat0"},
		Now:    func() time.Time { return time.Unix(0, 0) },
	}

	err := worker.sendOnceWithDeps(
		fake,
		func() (*batmanadv.Neighbors, error) { return &batmanadv.Neighbors{}, nil },
		func() ([]batmanadv.Originator, error) { return nil, nil },
		func() (*batmanadv.MeshConfig, error) {
			return &batmanadv.MeshConfig{AlgoName: "BATMAN_UNKNOWN"}, nil
		},
		func(string) network.NetworkInterface { return network.NetworkInterface{MAC: "aa:bb:cc:dd:ee:ff"} },
		func() (string, error) { return "host", nil },
	)
	require.NoError(t, err)

	got := &netv1.MeshNeighbors{}
	require.NoError(t, got.UnmarshalVT(fake.lastData))
	assert.Equal(t, int32(0), got.GetAlgorithm())
}
