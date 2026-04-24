package handlers_test

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mdlayher/wifi"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// testFixturesDir returns the path to the testfixtures directory at the root of
// the module, regardless of which package is importing it.
func testFixturesDir() string {
	_, filename, _, _ := runtime.Caller(0)
	// Walk up from internal/openmanet/server/handlers/ to the module root.
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "testfixtures")
}

// fixtureBatHostsPath returns the path to the bat-hosts test fixture.
func fixtureBatHostsPath() string {
	return filepath.Join(testFixturesDir(), "batman-adv", "bat-hosts")
}

func newMeshService(fw *fakeWireless, parseBatHosts func(string) (*batmanadv.BatHosts, error)) *handlers.MeshService {
	return &handlers.MeshService{
		Log:           zerolog.Nop(),
		Wifi:          fw,
		ParseBatHosts: parseBatHosts,
	}
}

func TestListMeshNeighbors_NoInterfaces(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newMeshService(fw, func(path string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNeighbors())
}

func TestListMeshNeighbors_WithStations(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	station := makeStation("9c:ef:d5:f9:80:4d", -65) // MAC present in fixture bat-hosts

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNeighbors(), 1)

	n := resp.GetNeighbors()[0]
	assert.Equal(t, "9c:ef:d5:f9:80:4d", n.GetHardwareAddress())
	assert.Equal(t, "BCM2711-97d6_phy2-mesh0", n.GetNeighbor(), "hostname should come from bat-hosts fixture")
	assert.Equal(t, int32(-65), n.GetSignal())
}

func TestListMeshNeighbors_NoStations(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    nil,
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNeighbors())
}

func TestListMeshNeighbors_WifiError(t *testing.T) {
	fw := &fakeWireless{meshInterfacesErr: errors.New("netlink failure")}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	_, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

func TestListMeshNeighbors_StationInfoError(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfoErr: errors.New("station info failed"),
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	_, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

func TestListMeshNeighbors_BatHostsError(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return nil, errors.New("bat-hosts unavailable")
	})

	_, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

func TestListMeshNeighbors_MultipleInterfaces(t *testing.T) {
	mesh0 := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	mesh1 := makeInterface("mesh1", wifi.InterfaceTypeMeshPoint)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{mesh0, mesh1},
		stationInfoByIface: map[string][]*wifi.StationInfo{
			"mesh0": {makeStation("9c:ef:d5:f9:80:4d", -65)},
			"mesh1": {makeStation("11:22:33:44:55:66", -70)},
		},
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Len(t, resp.GetNeighbors(), 2, "should include stations from both interfaces")
}

func TestListMeshNeighbors_UnknownMAC(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	// Use a MAC that is NOT in the bat-hosts fixture.
	station := makeStation("ff:ff:ff:ff:ff:ff", -50)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNeighbors(), 1)

	// Unknown MAC results in empty hostname, not an error.
	assert.Equal(t, "", resp.GetNeighbors()[0].GetNeighbor())
	assert.Equal(t, "ff:ff:ff:ff:ff:ff", resp.GetNeighbors()[0].GetHardwareAddress())
}

// TestListMeshNeighbors_BatmanAdvThroughputConversion pins the unit
// contract: batctl reports throughput in 100 kbit/s ticks, and the
// handler has to convert to bits/second before putting the value on
// the wire so the frontend's Mbps formatter reads correctly.
//
// Regression: a value of 2400 (= 240 Mbit/s) used to land on the wire
// as a raw 2400, which the UI rendered as "2 Kbps".
func TestListMeshNeighbors_BatmanAdvThroughputConversion(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	mac := "9c:ef:d5:f9:80:4d"
	station := makeStation(mac, -65)

	batNeighbors := batmanadv.Neighbors{
		{
			HardIfname:    "mesh0",
			NeighAddress:  mac,
			LastSeenMsecs: 1200,
			Throughput:    2400, // batman-adv internal unit = 100 kbit/s → 240 Mbit/s
		},
	}

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})
	svc.GetMeshNeighbors = func() (*batmanadv.Neighbors, error) {
		return &batNeighbors, nil
	}

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNeighbors(), 1)

	n := resp.GetNeighbors()[0]
	assert.Equal(t, int64(1200), n.GetLastSeen(), "LastSeenMsecs should propagate")
	assert.Equal(t, int32(240_000_000), n.GetThroughput(),
		"batctl 100 kbit/s ticks should convert to bits-per-second: 2400 × 100_000 = 240 Mbit/s")
}

func TestListMeshNeighbors_BatmanAdvThroughputClampsOnOverflow(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	mac := "9c:ef:d5:f9:80:4d"
	station := makeStation(mac, -55)

	// 50_000 × 100 kbit/s = 5 Gbit/s; as bits/sec that overflows int32.
	// The handler must saturate at int32 max rather than wrap around.
	batNeighbors := batmanadv.Neighbors{
		{HardIfname: "mesh0", NeighAddress: mac, Throughput: 50_000},
	}

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})
	svc.GetMeshNeighbors = func() (*batmanadv.Neighbors, error) {
		return &batNeighbors, nil
	}

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNeighbors(), 1)

	// int32 max is 2_147_483_647 bps ≈ 2.15 Gbit/s; saturate here instead
	// of wrapping to a negative value.
	assert.Equal(t, int32(2_147_483_647), resp.GetNeighbors()[0].GetThroughput())
}

func TestListMeshNeighbors_FieldMapping(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	station := makeStation("9c:ef:d5:f9:80:4d", -65)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newMeshService(fw, func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	})

	resp, err := svc.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNeighbors(), 1)

	n := resp.GetNeighbors()[0]
	assert.Equal(t, int32(-65), n.GetSignalStrength(), "SignalAverage maps to SignalStrength")
	assert.Equal(t, int32(-65), n.GetSignal(), "Signal maps to Signal")
	assert.Equal(t, int32(54000), n.GetThroughput(), "TransmitBitrate maps to Throughput (no batman-adv enrichment)")
	assert.Equal(t, "9c:ef:d5:f9:80:4d", n.GetHardwareAddress(), "HardwareAddr maps to HardwareAddress")
}
