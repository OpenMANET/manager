//go:build integration

package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/mdlayher/wifi"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	services "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// newTestServer wires up all handlers with fakes and the validation interceptor,
// returning an httptest.Server that mirrors the real server configuration.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	validateInterceptor := validate.NewInterceptor()

	db := newTestDB(t)
	fw := &fakeWireless{
		interfaces:     []*wifi.Interface{makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)},
		meshInterfaces: []*wifi.Interface{makeInterface("mesh0", wifi.InterfaceTypeMeshPoint)},
		stationInfo:    []*wifi.StationInfo{makeStation("aa:bb:cc:dd:ee:ff", -65)},
	}

	parseBatHosts := func(_ string) (*batmanadv.BatHosts, error) {
		return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
	}
	getMeshCfg := func(_ string) (*batmanadv.MeshConfig, error) {
		return &batmanadv.MeshConfig{GwMode: "off"}, nil
	}

	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()

	mux.Handle(services.NewNodeServiceHandler(&handlers.NodeService{
		DB:  db,
		Log: zerolog.Nop(),
	}, handlerOpt))

	mux.Handle(services.NewInterfaceServiceHandler(&handlers.InterfaceService{
		Log:  zerolog.Nop(),
		Wifi: fw,
	}, handlerOpt))

	mux.Handle(services.NewMeshNeighborServiceHandler(&handlers.MeshService{
		Log:           zerolog.Nop(),
		Wifi:          fw,
		ParseBatHosts: parseBatHosts,
	}, handlerOpt))

	mux.Handle(services.NewStatusServiceHandler(&handlers.StatusService{
		Cfg:        &config.Config{AlfredBatInterface: "bat0"},
		Log:        zerolog.Nop(),
		Wifi:       fw,
		GPS:        new(gpsd.GPSService),
		GetMeshCfg: getMeshCfg,
	}, handlerOpt))

	mux.Handle(services.NewCommsServiceHandler(&handlers.CommsService{
		Cfg: &config.Config{CommsEnable: false},
		Log: zerolog.Nop(),
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// ── NodeService ───────────────────────────────────────────────────────────────

func TestIntegration_ListNodes_Empty(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewNodeServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.ListNodes(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNodes())
}

// ── InterfaceService ──────────────────────────────────────────────────────────

func TestIntegration_ListWirelessInterfaces(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewInterfaceServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.ListWirelessInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 1)
	assert.Equal(t, "mesh0", resp.GetInterfaces()[0].GetName())
}

// ── MeshNeighborService ───────────────────────────────────────────────────────

func TestIntegration_ListMeshNeighbors(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewMeshNeighborServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.ListMeshNeighbors(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetNeighbors(), 1)
	assert.Equal(t, "aa:bb:cc:dd:ee:ff", resp.GetNeighbors()[0].GetHardwareAddress())
}

// ── StatusService ─────────────────────────────────────────────────────────────

func TestIntegration_GetServiceStatus(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewStatusServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.GetServiceStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetStatus().GetIsConnected())
}

// ── CommsService ──────────────────────────────────────────────────────────────

func TestIntegration_GetCommsStatus_Disabled(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.GetCommsStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)

	// The connect framework wraps handler errors; assert the message propagates.
	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.Contains(t, connectErr.Message(), "not enabled")
	}
}

func TestIntegration_JoinTalkGroup_Disabled(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.JoinTalkGroup(context.Background(), &serviceproto.JoinTalkGroupRequest{Talkgroup: 1})
	require.Error(t, err)
}

// ── Validation (interceptor enforcement over HTTP) ────────────────────────────

func TestIntegration_Validation_GetNode_EmptyHostname(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewNodeServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	_, err := client.GetNode(context.Background(), &serviceproto.GetNodeRequest{Hostname: ""})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestIntegration_Validation_GetNode_ValidHostname(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewNodeServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// Valid hostname passes validation; handler will return not-found which is a
	// different error code — not InvalidArgument.
	_, err := client.GetNode(context.Background(), &serviceproto.GetNodeRequest{Hostname: "any-node"})
	if err != nil {
		var connectErr *connect.Error
		require.ErrorAs(t, err, &connectErr)
		assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
			"valid hostname must not be rejected by the validator")
	}
}

func TestIntegration_Validation_GetWirelessInterface_EmptyName(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewInterfaceServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	_, err := client.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: ""})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestIntegration_Validation_JoinTalkGroup_TalkgroupTooLarge(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, tg := range []int32{11, 50, 100} {
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.JoinTalkGroup(context.Background(), &serviceproto.JoinTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code(),
				"talkgroup %d must be rejected with InvalidArgument", tg)
		})
	}
}

func TestIntegration_Validation_JoinTalkGroup_TalkgroupNegative(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	_, err := client.JoinTalkGroup(context.Background(), &serviceproto.JoinTalkGroupRequest{Talkgroup: -1})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code(),
		"negative talkgroup must be rejected with InvalidArgument")
}

func TestIntegration_Validation_JoinTalkGroup_ValidTalkgroup(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// Valid talkgroup passes validation; comms module is disabled so the handler
	// returns an error, but it must NOT be InvalidArgument.
	for _, tg := range []int32{0, 1, 5, 10} {
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.JoinTalkGroup(context.Background(), &serviceproto.JoinTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
				"valid talkgroup %d must not be rejected by the validator", tg)
		})
	}
}
