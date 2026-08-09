package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mdlayher/wifi"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newInterfaceService(fw *fakeWireless) *handlers.InterfaceService {
	return &handlers.InterfaceService{
		Log:  zerolog.Nop(),
		Wifi: fw,
	}
}

// ── ListWirelessInterfaces ───────────────────────────────────────────────────

func TestListWirelessInterfaces_Empty(t *testing.T) {
	svc := newInterfaceService(&fakeWireless{meshInterfaces: nil})

	resp, err := svc.ListWirelessInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetInterfaces())
}

func TestListWirelessInterfaces_WithData(t *testing.T) {
	ifaces := []*wifi.Interface{
		makeInterface("mesh0", wifi.InterfaceTypeMeshPoint),
		makeInterface("mesh1", wifi.InterfaceTypeMeshPoint),
	}
	svc := newInterfaceService(&fakeWireless{meshInterfaces: ifaces})

	resp, err := svc.ListWirelessInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 2)
	assert.Equal(t, "mesh0", resp.GetInterfaces()[0].GetName())
	assert.Equal(t, "mesh1", resp.GetInterfaces()[1].GetName())
}

func TestListWirelessInterfaces_Error(t *testing.T) {
	svc := newInterfaceService(&fakeWireless{meshInterfacesErr: errors.New("no wifi")})

	_, err := svc.ListWirelessInterfaces(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

// ── GetWirelessInterface ─────────────────────────────────────────────────────

func TestGetWirelessInterface_NoName_ReturnsLast(t *testing.T) {
	// When Name is empty the handler iterates all interfaces and returns the
	// last one it encounters (no early break in the current implementation).
	ifaces := []*wifi.Interface{
		makeInterface("wlh0", wifi.InterfaceTypeStation),
		makeInterface("mesh0", wifi.InterfaceTypeMeshPoint),
	}
	svc := newInterfaceService(&fakeWireless{interfaces: ifaces})

	resp, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{})
	require.NoError(t, err)
	// No name filter — last interface wins.
	assert.Equal(t, "mesh0", resp.GetInterface().GetName())
}

func TestGetWirelessInterface_ByName(t *testing.T) {
	ifaces := []*wifi.Interface{
		makeInterface("wlh0", wifi.InterfaceTypeStation),
		makeInterface("mesh0", wifi.InterfaceTypeMeshPoint),
	}
	svc := newInterfaceService(&fakeWireless{interfaces: ifaces})

	resp, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "wlh0"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetInterface())
	assert.Equal(t, "wlh0", resp.GetInterface().GetName())
}

func TestGetWirelessInterface_NotFound(t *testing.T) {
	ifaces := []*wifi.Interface{makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)}
	svc := newInterfaceService(&fakeWireless{interfaces: ifaces})

	resp, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "doesnotexist"})
	require.NoError(t, err)
	// Handler returns nil interface when no match is found.
	assert.Nil(t, resp.GetInterface())
}

func TestGetWirelessInterface_Error(t *testing.T) {
	svc := newInterfaceService(&fakeWireless{interfacesErr: errors.New("iw failure")})

	_, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "wlh0"})
	require.Error(t, err)
}

func TestGetWirelessInterface_FieldMapping(t *testing.T) {
	iface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	svc := newInterfaceService(&fakeWireless{interfaces: []*wifi.Interface{iface}})

	resp, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "mesh0"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetInterface())

	wi := resp.GetInterface()
	assert.Equal(t, int32(1), wi.GetIndex())
	assert.Equal(t, "mesh0", wi.GetName())
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", wi.GetHardwareAddress())
	assert.Equal(t, int32(0), wi.GetPhy())
	assert.Equal(t, int32(1), wi.GetDevice())
	assert.Equal(t, "mesh point", wi.GetInterfaceType())
	assert.Equal(t, int32(2412), wi.GetFrequency())
	assert.Equal(t, int32(20), wi.GetChannelWidth())
}

func TestListWirelessInterfaces_FieldMapping(t *testing.T) {
	iface := makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)
	svc := newInterfaceService(&fakeWireless{meshInterfaces: []*wifi.Interface{iface}})

	resp, err := svc.ListWirelessInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 1)

	wi := resp.GetInterfaces()[0]
	assert.Equal(t, int32(1), wi.GetIndex())
	assert.Equal(t, "mesh0", wi.GetName())
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", wi.GetHardwareAddress())
	assert.Equal(t, int32(0), wi.GetPhy())
	assert.Equal(t, int32(1), wi.GetDevice())
	assert.Equal(t, "mesh point", wi.GetInterfaceType())
	assert.Equal(t, int32(2412), wi.GetFrequency())
	assert.Equal(t, int32(20), wi.GetChannelWidth())
}
