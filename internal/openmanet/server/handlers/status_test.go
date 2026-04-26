package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mdlayher/wifi"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// stubMeshCfg returns a non-gateway MeshConfig for use as a GetMeshCfg stub.
func stubMeshCfg(gateway bool) func(string) (*batmanadv.MeshConfig, error) {
	return func(_ string) (*batmanadv.MeshConfig, error) {
		mode := "off"
		if gateway {
			mode = "server"
		}

		return &batmanadv.MeshConfig{GwMode: mode}, nil
	}
}

func errMeshCfg(msg string) func(string) (*batmanadv.MeshConfig, error) {
	return func(_ string) (*batmanadv.MeshConfig, error) {
		return nil, errors.New(msg)
	}
}

func newStatusService(fw *fakeWireless, meshCfgFn func(string) (*batmanadv.MeshConfig, error)) *handlers.StatusService {
	return &handlers.StatusService{
		Cfg:        &config.Config{AlfredBatInterface: "bat0"},
		Log:        zerolog.Nop(),
		Wifi:       fw,
		GPS:        new(gpsd.GPSService), // zero-value: GetPosition() returns empty PositionReport
		GetMeshCfg: meshCfgFn,
		// Default to "no gateways" so tests don't exec real `batctl gwj`.
		GetMeshGateways: func(_ string) (*batmanadv.Gateways, error) {
			gws := batmanadv.Gateways{}

			return &gws, nil
		},
	}
}

func TestGetServiceStatus_NoInterfaces(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, stubMeshCfg(false))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp.GetStatus())
	assert.False(t, resp.GetStatus().GetIsConnected())
	assert.Equal(t, int32(0), resp.GetStatus().GetConnectedNeighbors())
	assert.False(t, resp.GetStatus().GetIsMeshGateway())
}

func TestGetServiceStatus_Connected(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	station := makeStation("aa:bb:cc:dd:ee:ff", -70)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newStatusService(fw, stubMeshCfg(false))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetStatus().GetIsConnected())
	assert.Equal(t, int32(1), resp.GetStatus().GetConnectedNeighbors())
	assert.Equal(t, int32(1), resp.GetStatus().GetActiveMeshInterfaces())
}

func TestGetServiceStatus_IsGateway(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, stubMeshCfg(true))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetStatus().GetIsMeshGateway())
}

func TestGetServiceStatus_MeshCfgError(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, errMeshCfg("batctl not found"))

	_, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "batctl not found")
}

func TestGetServiceStatus_WifiError(t *testing.T) {
	fw := &fakeWireless{meshInterfacesErr: errors.New("netlink gone")}
	svc := newStatusService(fw, stubMeshCfg(false))

	_, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

func TestGetServiceStatus_SelectedGatewayMac(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, stubMeshCfg(false))
	svc.GetMeshGateways = func(_ string) (*batmanadv.Gateways, error) {
		gws := batmanadv.Gateways{
			{OrigAddress: "aa:bb:cc:dd:ee:01", Best: false},
			{OrigAddress: "aa:bb:cc:dd:ee:02", Best: true},
		}

		return &gws, nil
	}

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "aa:bb:cc:dd:ee:02", resp.GetStatus().GetSelectedGatewayMac())
}

func TestGetServiceStatus_NoSelectedGatewayWhenNoneBest(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, stubMeshCfg(false))
	svc.GetMeshGateways = func(_ string) (*batmanadv.Gateways, error) {
		gws := batmanadv.Gateways{
			{OrigAddress: "aa:bb:cc:dd:ee:01", Best: false},
		}

		return &gws, nil
	}

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetStatus().GetSelectedGatewayMac())
}

func TestGetServiceStatus_GatewayListErrorIsNonFatal(t *testing.T) {
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, stubMeshCfg(false))
	svc.GetMeshGateways = func(_ string) (*batmanadv.Gateways, error) {
		return nil, errors.New("batctl gwj exited 1")
	}

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetStatus().GetSelectedGatewayMac())
}

func TestGetServiceStatus_Position(t *testing.T) {
	// With a zero-value GPSService, position fields should all be zero (no crash).
	fw := &fakeWireless{meshInterfaces: nil}
	svc := newStatusService(fw, stubMeshCfg(false))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	pos := resp.GetStatus().GetPosition()
	require.NotNil(t, pos)
	assert.Equal(t, float64(0), pos.GetLatitude())
	assert.Equal(t, float64(0), pos.GetLongitude())
}

func TestGetServiceStatus_MultipleInterfaces(t *testing.T) {
	// All mesh interfaces report the same single station — total should be the
	// sum across interfaces, not just the first.
	mesh0 := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	mesh1 := makeInterface("mesh1", wifi.InterfaceTypeMeshPoint)
	mesh2 := makeInterface("mesh2", wifi.InterfaceTypeMeshPoint)

	station := makeStation("aa:bb:cc:dd:ee:ff", -65)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{mesh0, mesh1, mesh2},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newStatusService(fw, stubMeshCfg(false))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	status := resp.GetStatus()
	assert.True(t, status.GetIsConnected())
	assert.Equal(t, int32(3), status.GetConnectedNeighbors(), "stations summed across all interfaces")
	assert.Equal(t, int32(3), status.GetActiveMeshInterfaces(), "counts all interfaces")
}

// TestGetServiceStatus_MultipleInterfaces_PerIfaceCounts exercises the
// per-interface station map so each radio reports a different station list.
// A field deployment with 2.4 GHz + 5 GHz mesh radios would otherwise have
// its neighbor count silently truncated to whichever interface the kernel
// happened to enumerate first.
func TestGetServiceStatus_MultipleInterfaces_PerIfaceCounts(t *testing.T) {
	mesh0 := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	mesh1 := makeInterface("mesh1", wifi.InterfaceTypeMeshPoint)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{mesh0, mesh1},
		stationInfoByIface: map[string][]*wifi.StationInfo{
			"mesh0": {makeStation("aa:bb:cc:dd:ee:01", -65)},
			"mesh1": {
				makeStation("aa:bb:cc:dd:ee:02", -70),
				makeStation("aa:bb:cc:dd:ee:03", -72),
				makeStation("aa:bb:cc:dd:ee:04", -75),
			},
		},
	}
	svc := newStatusService(fw, stubMeshCfg(false))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	status := resp.GetStatus()
	assert.True(t, status.GetIsConnected())
	assert.Equal(t, int32(4), status.GetConnectedNeighbors(), "1 from mesh0 + 3 from mesh1")
	assert.Equal(t, int32(2), status.GetActiveMeshInterfaces())
}

func TestGetServiceStatus_StationInfoError(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfoErr: errors.New("netlink station query failed"),
	}
	svc := newStatusService(fw, stubMeshCfg(false))

	_, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mesh0")
}

func TestGetServiceStatus_GatewayAndConnected(t *testing.T) {
	meshIface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	station := makeStation("aa:bb:cc:dd:ee:ff", -60)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}
	svc := newStatusService(fw, stubMeshCfg(true))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	status := resp.GetStatus()
	assert.True(t, status.GetIsConnected())
	assert.True(t, status.GetIsMeshGateway())
	assert.Equal(t, int32(1), status.GetConnectedNeighbors())
}

func TestGetServiceStatus_InterfacesButNoStations(t *testing.T) {
	mesh0 := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	mesh1 := makeInterface("mesh1", wifi.InterfaceTypeMeshPoint)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{mesh0, mesh1},
		stationInfo:    nil,
	}
	svc := newStatusService(fw, stubMeshCfg(false))

	resp, err := svc.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	status := resp.GetStatus()
	assert.False(t, status.GetIsConnected())
	assert.Equal(t, int32(0), status.GetConnectedNeighbors())
	assert.Equal(t, int32(2), status.GetActiveMeshInterfaces())
}
