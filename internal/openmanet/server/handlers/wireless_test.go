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
		makeInterface("wlan0", wifi.InterfaceTypeStation),
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
		makeInterface("wlan0", wifi.InterfaceTypeStation),
		makeInterface("mesh0", wifi.InterfaceTypeMeshPoint),
	}
	svc := newInterfaceService(&fakeWireless{interfaces: ifaces})

	resp, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "wlan0"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetInterface())
	assert.Equal(t, "wlan0", resp.GetInterface().GetName())
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

	_, err := svc.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "wlan0"})
	require.Error(t, err)
}
