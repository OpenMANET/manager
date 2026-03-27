//go:build integration

package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/mdlayher/wifi"
	blosproto "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	blosconnect "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1/blosv1connect"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	services "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
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

	mux.Handle(services.NewWebCommsServiceHandler(&handlers.WebCommsService{
		Log: zerolog.Nop(),
	}, handlerOpt))

	mux.Handle(blosconnect.NewBLOSServiceHandler(&handlers.BLOSService{
		Cfg:         &config.Config{BLOSEnable: false},
		Log:         zerolog.Nop(),
		BLOSManager: &fakeBLOSManager{},
		RunCommand: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// newTestServerEnabled is like newTestServer but with CommsEnable: true so that
// the comms handler proceeds past the "not enabled" guard. The comms runtime is
// not started, so operations that require the live subsystem will return
// {Success: false} response bodies rather than gRPC errors.
func newTestServerEnabled(t *testing.T) *httptest.Server {
	t.Helper()

	validateInterceptor := validate.NewInterceptor()
	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()
	mux.Handle(services.NewCommsServiceHandler(&handlers.CommsService{
		Cfg: &config.Config{CommsEnable: true},
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

func TestIntegration_SetSendTalkGroup_Disabled(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.Contains(t, connectErr.Message(), "not enabled")
	}
}

func TestIntegration_SetSendTalkGroup_NotRunning(t *testing.T) {
	srv := newTestServerEnabled(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	// Comms is enabled but the runtime is not started, so talkGroupPortIdx
	// fails and the error is propagated as a gRPC error (not InvalidArgument).
	_, err := client.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
			"subsystem-not-running must not look like a validation error")
	}
}

func TestIntegration_SetReceiveTalkGroup_Disabled(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.Contains(t, connectErr.Message(), "not enabled")
	}
}

func TestIntegration_SetReceiveTalkGroup_NotRunning(t *testing.T) {
	srv := newTestServerEnabled(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	// Comms is enabled but the runtime is not started, so talkGroupPortIdx
	// fails and the error is propagated as a gRPC error (not InvalidArgument).
	_, err := client.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{
		Talkgroup: 1,
		Enabled:   true,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
			"subsystem-not-running must not look like a validation error")
	}
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

func TestIntegration_GetWirelessInterface_ByName(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewInterfaceServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetWirelessInterface(context.Background(), &serviceproto.GetWirelessInterfaceRequest{Name: "mesh0"})
	require.NoError(t, err)
	require.NotNil(t, resp.GetInterface())
	assert.Equal(t, "mesh0", resp.GetInterface().GetName())
}

func TestIntegration_GetCommsStatus_Enabled(t *testing.T) {
	srv := newTestServerEnabled(t)
	client := services.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	// Comms is enabled but the runtime is not started; the handler should
	// return a valid response (not an error) with the static talkgroup list.
	resp, err := client.GetCommsStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Available talkgroups are derived from the static config and must be non-empty.
	assert.NotEmpty(t, resp.GetAvailableTalkgroups())
}

func TestIntegration_Validation_SetSendTalkGroup_OutOfRange(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// talkgroup field is gte:1 lte:32; values outside that range must be rejected.
	for _, tg := range []int32{0, -1, 33, 100} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code(),
				"talkgroup %d must be rejected with InvalidArgument", tg)
		})
	}
}

func TestIntegration_Validation_SetSendTalkGroup_ValidTalkgroup(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// Valid talkgroup passes validation; comms is disabled so the handler returns
	// a gRPC error, but it must NOT be InvalidArgument.
	for _, tg := range []int32{1, 16, 32} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetSendTalkGroup(context.Background(), &serviceproto.SetSendTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
				"valid talkgroup %d must not be rejected by the validator", tg)
		})
	}
}

func TestIntegration_Validation_SetReceiveTalkGroup_OutOfRange(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, tg := range []int32{0, -1, 33, 100} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code(),
				"talkgroup %d must be rejected with InvalidArgument", tg)
		})
	}
}

func TestIntegration_Validation_SetReceiveTalkGroup_ValidTalkgroup(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, tg := range []int32{1, 16, 32} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetReceiveTalkGroup(context.Background(), &serviceproto.SetReceiveTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
				"valid talkgroup %d must not be rejected by the validator", tg)
		})
	}
}

// ── WebCommsService ───────────────────────────────────────────────────────

func TestIntegration_SendPTTEvent_WebNotActive(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewWebCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.SendPTTEvent(context.Background(), &serviceproto.SendPTTEventRequest{Event: 0})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
		assert.Contains(t, connectErr.Message(), "web control source not active")
	}
}

func TestIntegration_StreamAudioRx_WebNotActive(t *testing.T) {
	srv := newTestServer(t)
	client := services.NewWebCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	stream, err := client.StreamAudioRx(context.Background(), &serviceproto.StreamAudioRxRequest{})
	// connect-go may return the error on the stream.Receive call rather than
	// on the initial call, depending on the protocol.
	if err != nil {
		var connectErr *connect.Error
		if assert.ErrorAs(t, err, &connectErr) {
			assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
		}

		return
	}

	// If the initial call succeeded, the error surfaces on the first Receive.
	ok := stream.Receive()
	assert.False(t, ok)
	require.Error(t, stream.Err())

	var connectErr *connect.Error
	if assert.ErrorAs(t, stream.Err(), &connectErr) {
		assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
	}

	require.NoError(t, stream.Close())
}

// ── BLOSService ───────────────────────────────────────────────────────────

// newBLOSTestServer creates a test server with a real Config backed by a temp file.
func newBLOSTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte("blos:\n  enable: false\n"), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	cfg := config.NewWithoutWatch(v)

	validateInterceptor := validate.NewInterceptor()
	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()
	mux.Handle(blosconnect.NewBLOSServiceHandler(&handlers.BLOSService{
		Cfg:         cfg,
		Log:         zerolog.Nop(),
		BLOSManager: &fakeBLOSManager{},
		RunCommand: func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		},
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestIntegration_GetBLOSStatus(t *testing.T) {
	srv := newBLOSTestServer(t)
	client := blosconnect.NewBLOSServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.GetBLOSStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.BlosEnabled)
	assert.NotNil(t, resp.Message)
}

func TestIntegration_UpdateBLOSConfig_Enable(t *testing.T) {
	srv := newBLOSTestServer(t)
	client := blosconnect.NewBLOSServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.UpdateBLOSConfig(context.Background(), &blosproto.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-test-key",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestIntegration_UpdateBLOSConfig_EmptyAuthKey(t *testing.T) {
	srv := newBLOSTestServer(t)
	client := blosconnect.NewBLOSServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.UpdateBLOSConfig(context.Background(), &blosproto.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "",
	})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestIntegration_UpdateBLOSConfig_Disable(t *testing.T) {
	srv := newBLOSTestServer(t)
	client := blosconnect.NewBLOSServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.UpdateBLOSConfig(context.Background(), &blosproto.UpdateBLOSConfigRequest{
		EnableBlos: false,
		AuthKey:    "",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}
