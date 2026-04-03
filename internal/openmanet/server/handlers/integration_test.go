//go:build integration

package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/mdlayher/wifi"
	blosproto "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	blosconnect "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1/blosv1connect"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	commsconnect "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1/commsv1connect"
	niv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1"
	niconnect "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1/network_interfacev1connect"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	services "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/network"
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

	mux.Handle(commsconnect.NewCommsServiceHandler(&handlers.CommsService{
		Cfg: &config.Config{CommsEnable: false},
		Log: zerolog.Nop(),
	}, handlerOpt))

	mux.Handle(blosconnect.NewBLOSServiceHandler(&handlers.BLOSService{
		Cfg:         &config.Config{BLOSEnable: false},
		Log:         zerolog.Nop(),
		BLOSManager: &fakeBLOSManager{},
	}, handlerOpt))

	mux.Handle(niconnect.NewNetworkInterfaceServiceHandler(&handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		Interfaces: &fakeInterfaceProvider{
			infos: []network.NetworkInterfaceInfo{
				{Name: "br-ahwlan", LinkType: network.LinkTypeBridge, MAC: "C8:3E:A7:00:6C:FF", IP: "10.41.25.72/16", State: network.OperStateUp, RxBytes: 1000, TxBytes: 2000, MTU: 1500},
			},
		},
		DHCP: &fakeDHCPConfigProvider{
			dhcpCfg:    &network.UCIDHCP{Interface: "ahwlan", Start: "100", Limit: "155", LeaseTime: "12h"},
			dnsmasqCfg: &network.UCIDnsmasq{Local: "/lan/"},
			baseIP:     "10.41.0.0",
			staticHost: []network.UCIStaticHost{
				{Name: "printer", MAC: "AA:BB:CC:11:22:33", IP: "10.41.0.10"},
			},
		},
		Leases: &fakeLeaseProvider{
			resp: &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "laptop", MacAddr: "D4:6D:6D:1A:2B:3C", IPAddr: "10.41.0.101", Expires: 42120},
				},
			},
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
	mux.Handle(commsconnect.NewCommsServiceHandler(&handlers.CommsService{
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
	client := commsconnect.NewCommsServiceClient(
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
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{
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
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	// Comms is enabled but the runtime is not started, so talkGroupPortIdx
	// fails and the error is propagated as a gRPC error (not InvalidArgument).
	_, err := client.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{
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
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{
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
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	// Comms is enabled but the runtime is not started, so talkGroupPortIdx
	// fails and the error is propagated as a gRPC error (not InvalidArgument).
	_, err := client.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{
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
	client := commsconnect.NewCommsServiceClient(
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
	client := commsconnect.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// talkgroup field is gte:1 lte:32; values outside that range must be rejected.
	for _, tg := range []int32{0, -1, 33, 100} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{Talkgroup: tg})
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
	client := commsconnect.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	// Valid talkgroup passes validation; comms is disabled so the handler returns
	// a gRPC error, but it must NOT be InvalidArgument.
	for _, tg := range []int32{1, 16, 32} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetSendTalkGroup(context.Background(), &commsv1.SetSendTalkGroupRequest{Talkgroup: tg})
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
	client := commsconnect.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, tg := range []int32{0, -1, 33, 100} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{Talkgroup: tg})
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
	client := commsconnect.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, tg := range []int32{1, 16, 32} {
		tg := tg
		t.Run(fmt.Sprintf("talkgroup_%d", tg), func(t *testing.T) {
			_, err := client.SetReceiveTalkGroup(context.Background(), &commsv1.SetReceiveTalkGroupRequest{Talkgroup: tg})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.NotEqual(t, connect.CodeInvalidArgument, connectErr.Code(),
				"valid talkgroup %d must not be rejected by the validator", tg)
		})
	}
}

// ── SendPTTEvent / StreamAudio (via unified CommsService) ─────────────────

// ── SendPTTEvent / StreamAudio ────────────────────────────────────────────

func TestIntegration_SendPTTEvent_WebNotActive(t *testing.T) {
	srv := newTestServer(t)
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.SendPTTEvent(context.Background(), &commsv1.SendPTTEventRequest{Event: 0})
	require.Error(t, err)

	var connectErr *connect.Error
	if assert.ErrorAs(t, err, &connectErr) {
		assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
		assert.Contains(t, connectErr.Message(), "web control source not active")
	}
}

func TestIntegration_StreamAudioRx_WebNotActive(t *testing.T) {
	srv := newTestServer(t)
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	stream, err := client.StreamAudioRx(context.Background(), &commsv1.StreamAudioRxRequest{})
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

// ── GetCommsConfig / UpdateCommsConfig ────────────────────────────────────

// newCommsConfigTestServer creates a test server with a real Config backed by a temp file
// so that UpdateCommsConfig can persist changes.
func newCommsConfigTestServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte("comms:\n  enable: false\n  controlSource: openvlm\n"), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	cfg := config.NewWithoutWatch(v)

	validateInterceptor := validate.NewInterceptor()
	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()
	mux.Handle(commsconnect.NewCommsServiceHandler(&handlers.CommsService{
		Cfg: cfg,
		Log: zerolog.Nop(),
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv, cfg
}

func TestIntegration_GetCommsConfig(t *testing.T) {
	srv, _ := newCommsConfigTestServer(t)
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.False(t, resp.GetCommsEnabled())
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_OPENVLM, resp.GetControlSource())
}

func TestIntegration_UpdateCommsConfig_Enable(t *testing.T) {
	srv, cfg := newCommsConfigTestServer(t)
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_WEB,
	})
	require.NoError(t, err)

	// Verify persisted in memory
	assert.True(t, cfg.GetCommsEnable())
	assert.Equal(t, "web", cfg.GetCommsControlSource())

	// Verify GetCommsConfig reflects the change
	resp, err := client.GetCommsConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetCommsEnabled())
	assert.Equal(t, commsv1.ControlSource_CONTROL_SOURCE_WEB, resp.GetControlSource())
}

func TestIntegration_UpdateCommsConfig_Disable(t *testing.T) {
	srv, cfg := newCommsConfigTestServer(t)
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	// First enable
	_, err := client.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_NANOPTT,
	})
	require.NoError(t, err)
	assert.True(t, cfg.GetCommsEnable())

	// Then disable
	_, err = client.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   false,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_OPENVLM,
	})
	require.NoError(t, err)

	assert.False(t, cfg.GetCommsEnable())
	assert.Equal(t, "openvlm", cfg.GetCommsControlSource())
}

func TestIntegration_UpdateCommsConfig_PersistsToFile(t *testing.T) {
	srv, cfg := newCommsConfigTestServer(t)
	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err := client.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_NANOPTT,
	})
	require.NoError(t, err)

	// Read the actual YAML file to confirm persistence
	data, err := os.ReadFile(cfg.GetConfigFilePath())
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "enable: true")
	assert.Contains(t, content, "controlSource: nanoptt")
}

func TestIntegration_UpdateCommsConfig_EnableCallsManager(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte("comms:\n  enable: false\n  controlSource: openvlm\n"), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	cfg := config.NewWithoutWatch(v)

	mgr := &fakeCommsManager{}

	validateInterceptor := validate.NewInterceptor()
	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()
	mux.Handle(commsconnect.NewCommsServiceHandler(&handlers.CommsService{
		Cfg:          cfg,
		Log:          zerolog.Nop(),
		CommsManager: mgr,
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := commsconnect.NewCommsServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err = client.UpdateCommsConfig(context.Background(), &commsv1.UpdateCommsConfigRequest{
		EnableComms:   true,
		ControlSource: commsv1.ControlSource_CONTROL_SOURCE_WEB,
	})
	require.NoError(t, err)

	assert.Equal(t, 1, mgr.getEnableCalls())
	assert.Equal(t, 1, mgr.getDisableCalls())
	assert.True(t, mgr.IsRunning())
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

func TestIntegration_UpdateBLOSConfig_Enable_PersistFailure(t *testing.T) {
	// Create server with a real config backed by a temp file
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte("blos:\n  enable: false\n"), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)

	err = v.ReadInConfig()
	require.NoError(t, err)

	cfg := config.NewWithoutWatch(v)
	mgr := &fakeBLOSManager{}

	validateInterceptor := validate.NewInterceptor()
	handlerOpt := connect.WithInterceptors(validateInterceptor)

	mux := http.NewServeMux()
	mux.Handle(blosconnect.NewBLOSServiceHandler(&handlers.BLOSService{
		Cfg:         cfg,
		Log:         zerolog.Nop(),
		BLOSManager: mgr,
	}, handlerOpt))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Make config file read-only to trigger persist failure
	err = os.Chmod(cfgPath, 0444)
	require.NoError(t, err)

	client := blosconnect.NewBLOSServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	_, err = client.UpdateBLOSConfig(context.Background(), &blosproto.UpdateBLOSConfigRequest{
		EnableBlos: true,
		AuthKey:    "tskey-test-key",
	})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())

	// BLOS should have been rolled back
	assert.False(t, mgr.IsRunning(), "BLOS should be rolled back after persist failure")
}

func TestIntegration_UpdateBLOSConfig_ConcurrentRequests(t *testing.T) {
	srv := newBLOSTestServer(t)

	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)

		enable := i%2 == 0

		go func() {
			defer wg.Done()

			client := blosconnect.NewBLOSServiceClient(
				http.DefaultClient,
				srv.URL,
				connect.WithGRPCWeb(),
			)

			for j := 0; j < 10; j++ {
				if enable {
					_, _ = client.UpdateBLOSConfig(context.Background(), &blosproto.UpdateBLOSConfigRequest{
						EnableBlos: true,
						AuthKey:    "tskey-test",
					})
				} else {
					_, _ = client.UpdateBLOSConfig(context.Background(), &blosproto.UpdateBLOSConfigRequest{
						EnableBlos: false,
					})
				}
			}
		}()
	}

	wg.Wait()
	// If we get here without panic or race, the test passes.
}

// ── NetworkInterfaceService ───────────────────────────────────────────────────

func TestIntegration_ListNetworkInterfaces(t *testing.T) {
	srv := newTestServer(t)
	client := niconnect.NewNetworkInterfaceServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.ListNetworkInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 1)
	assert.Equal(t, "br-ahwlan", resp.GetInterfaces()[0].GetName())
	assert.Equal(t, niv1.InterfaceType_INTERFACE_TYPE_BRIDGE, resp.GetInterfaces()[0].GetType())
}

func TestIntegration_GetDHCPServerConfig(t *testing.T) {
	srv := newTestServer(t)
	client := niconnect.NewNetworkInterfaceServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetDHCPServerConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "ahwlan", resp.GetConfig().GetInterfaceName())
	assert.Equal(t, "10.41.0.100", resp.GetConfig().GetRangeStart())
	assert.Equal(t, "12h", resp.GetConfig().GetLeaseTime())
	assert.True(t, resp.GetConfig().GetDnsForwardingEnabled())
	assert.Equal(t, int32(1), resp.GetConfig().GetActiveLeaseCount())
}

func TestIntegration_ListActiveDHCPLeases(t *testing.T) {
	srv := newTestServer(t)
	client := niconnect.NewNetworkInterfaceServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.ListActiveDHCPLeases(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetLeases(), 1)
	assert.Equal(t, "laptop", resp.GetLeases()[0].GetHostname())
	assert.Equal(t, int32(42120), resp.GetLeases()[0].GetExpiresSeconds())
}

func TestIntegration_ListStaticDHCPLeases(t *testing.T) {
	srv := newTestServer(t)
	client := niconnect.NewNetworkInterfaceServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.ListStaticDHCPLeases(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetLeases(), 1)
	assert.Equal(t, "printer", resp.GetLeases()[0].GetHostname())
	assert.Equal(t, "AA:BB:CC:11:22:33", resp.GetLeases()[0].GetMacAddress())
}
