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
