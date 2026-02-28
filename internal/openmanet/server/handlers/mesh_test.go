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
