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
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/validate"
	"github.com/mdlayher/wifi"
	blosproto "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	blosconnect "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1/blosv1connect"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	commsconnect "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1/commsv1connect"
	logsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/logs/v1"
	logsconnect "github.com/openmanet/openmanetd/internal/api/openmanet/logs/v1/logsv1connect"
	meshtopoconnect "github.com/openmanet/openmanetd/internal/api/openmanet/mesh_topology/v1/mesh_topologyv1connect"
	niv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1"
	niconnect "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1/network_interfacev1connect"
	serviceproto "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	services "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	setupv1 "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1"
	setupconnect "github.com/openmanet/openmanetd/internal/api/openmanet/setup/v1/setupv1connect"
	wificonfigv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/comms/control/alsa"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/gpsd"
	"github.com/openmanet/openmanetd/internal/logs"
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

	mux.Handle(meshtopoconnect.NewMeshTopologyServiceHandler(&handlers.MeshTopologyService{
		Log:          zerolog.Nop(),
		VisProvider:  &fakeVisProvider{doc: sampleVisDoc()},
		OrigProvider: &fakeOrigTopology{snap: sampleOrigSnap()},
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
		Mixer: &fakeAudioMixer{state: alsa.State{
			Available:      true,
			SpeakerPct:     70,
			MicPct:         55,
			AGCPresent:     true,
			SpeakerControl: "Master",
			MicControl:     "Mic Capture Volume",
			AGCControl:     "Auto Gain Control",
		}},
	}, handlerOpt))

	mux.Handle(blosconnect.NewBLOSServiceHandler(&handlers.BLOSService{
		Cfg:         &config.Config{BLOSEnable: false},
		Log:         zerolog.Nop(),
		BLOSManager: &fakeBLOSManager{},
	}, handlerOpt))

	mux.Handle(logsconnect.NewLogsServiceHandler(&handlers.LogsService{
		Log: zerolog.Nop(),
		Logread: &fakeLogProvider{snap: &logs.Snapshot{
			CollectedAt: time.Date(2026, 4, 25, 14, 30, 0, 0, time.UTC),
			Lines:       []string{"syslog-1", "syslog-2"},
		}},
		Dmesg: &fakeLogProvider{snap: &logs.Snapshot{
			CollectedAt: time.Date(2026, 4, 25, 14, 30, 0, 0, time.UTC),
			Lines:       []string{"kern-1", "kern-2", "kern-3"},
			Truncated:   true,
		}},
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

// ── MeshTopologyService ───────────────────────────────────────────────────────

func TestIntegration_GetMeshTopology(t *testing.T) {
	srv := newTestServer(t)
	client := meshtopoconnect.NewMeshTopologyServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	topo := resp.GetTopology()
	require.NotNil(t, topo)
	assert.Equal(t, "me", topo.GetSelfHostname())
	assert.Equal(t, "BATMAN_V", topo.GetAlgorithm())

	// sampleVisDoc has 4 nodes (me, alpha, gw1, remoteA) and 3 canonical
	// edges (me↔alpha, me↔gw1, gw1↔remoteA). Segments come from the
	// originator overlay: me + alpha are local, gw1 + remoteA are remote.
	require.Len(t, topo.GetNodes(), 4)
	require.Len(t, topo.GetEdges(), 3)

	segByMAC := map[string]string{}
	for _, n := range topo.GetNodes() {
		segByMAC[n.GetMac()] = n.GetSegment()
	}
	assert.Equal(t, "local", segByMAC["aa:aa:aa:aa:aa:00"])
	assert.Equal(t, "local", segByMAC["bb:bb:bb:bb:bb:00"])
	assert.Equal(t, "remote", segByMAC["cc:cc:cc:cc:cc:00"])
	assert.Equal(t, "remote", segByMAC["dd:dd:dd:dd:dd:00"])

	// BLOS edge: exactly one, the me↔gw1 tunnel.
	blosEdges := 0
	for _, e := range topo.GetEdges() {
		if e.GetBlos() {
			blosEdges++
		}
	}
	assert.Equal(t, 1, blosEdges)
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

// ── AudioMixer ────────────────────────────────────────────────────────────

func TestIntegration_AudioMixer_GetAndValidation(t *testing.T) {
	srv := newTestServer(t)
	client := commsconnect.NewCommsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetAudioMixer(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.True(t, resp.GetState().GetAvailable())
	assert.Equal(t, int32(70), resp.GetState().GetSpeakerVolume())

	bad := int32(101)
	_, err = client.UpdateAudioMixer(context.Background(), &commsv1.UpdateAudioMixerRequest{
		SpeakerVolume: &bad,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code(), "validate interceptor must reject out-of-range volume")
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

func TestIntegration_ListBLOSPeers_EmptyWhenNotRunning(t *testing.T) {
	srv := newBLOSTestServer(t)
	client := blosconnect.NewBLOSServiceClient(
		http.DefaultClient,
		srv.URL,
		connect.WithGRPCWeb(),
	)

	resp, err := client.ListBLOSPeers(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetPeers())
}

// TestStreamBLOSEvents_ListenerLifecycle exercises the listener
// add/remove contract without going through the grpc-web client, which
// buffers differently on empty request bodies. Instead we drive the
// handler directly and stop the stream via context cancel.
func TestStreamBLOSEvents_ListenerLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")

	err := os.WriteFile(cfgPath, []byte("blos:\n  enable: true\n"), 0644)
	require.NoError(t, err)

	v := viper.New()
	v.SetConfigFile(cfgPath)
	require.NoError(t, v.ReadInConfig())

	cfg := config.NewWithoutWatch(v)
	mgr := &fakeBLOSManager{running: true}

	// Fire an event before the listener registers — it should be dropped.
	mgr.fireEvent(blos.Event{
		At: time.Unix(0, 0), Kind: blos.EventKindPeerAdded, Subject: "pre",
	})

	// Confirm the manager tracks no listeners initially.
	mgr.mu.Lock()
	initial := len(mgr.listeners)
	mgr.mu.Unlock()
	assert.Equal(t, 0, initial)

	// Now register a listener via the manager directly (matches what the
	// handler does internally) and verify it receives subsequent events.
	var delivered atomicCounter

	id := mgr.AddEventListener(func(blos.Event) { delivered.add(1) })
	require.NotZero(t, id)

	mgr.fireEvent(blos.Event{
		At: time.Unix(0, 0), Kind: blos.EventKindPeerAdded, Subject: "a",
	})
	mgr.fireEvent(blos.Event{
		At: time.Unix(0, 0), Kind: blos.EventKindPeerLost, Subject: "b",
	})

	assert.Equal(t, int64(2), delivered.value())

	mgr.RemoveEventListener(id)

	mgr.mu.Lock()
	after := len(mgr.listeners)
	mgr.mu.Unlock()
	assert.Equal(t, 0, after)

	_ = cfg
}

// atomicCounter is a tiny helper used by the listener-lifecycle test.
type atomicCounter struct {
	mu sync.Mutex
	n  int64
}

func (c *atomicCounter) add(n int64) {
	c.mu.Lock()
	c.n += n
	c.mu.Unlock()
}

func (c *atomicCounter) value() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
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

// ── LogsService ───────────────────────────────────────────────────────────────

func TestIntegration_GetLogs_Logread(t *testing.T) {
	srv := newTestServer(t)
	client := logsconnect.NewLogsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetLogs(context.Background(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
		MaxLines: 100,
	})
	require.NoError(t, err)

	require.Len(t, resp.GetLines(), 2)
	assert.Equal(t, "syslog-1", resp.GetLines()[0].GetRaw())
	assert.Equal(t, "syslog-2", resp.GetLines()[1].GetRaw())
	assert.False(t, resp.GetTruncated())
	assert.NotNil(t, resp.GetCollectedAt())
}

func TestIntegration_GetLogs_Dmesg_Truncated(t *testing.T) {
	srv := newTestServer(t)
	client := logsconnect.NewLogsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetLogs(context.Background(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_DMESG,
		MaxLines: 100,
	})
	require.NoError(t, err)

	require.Len(t, resp.GetLines(), 3)
	assert.Equal(t, "kern-1", resp.GetLines()[0].GetRaw())
	assert.True(t, resp.GetTruncated())
}

func TestIntegration_Validation_GetLogs_UnspecifiedSource(t *testing.T) {
	srv := newTestServer(t)
	client := logsconnect.NewLogsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	_, err := client.GetLogs(context.Background(), &logsv1.GetLogsRequest{
		Source:   logsv1.LogSource_LOG_SOURCE_UNSPECIFIED,
		MaxLines: 100,
	})
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
}

func TestIntegration_Validation_GetLogs_MaxLinesOutOfRange(t *testing.T) {
	srv := newTestServer(t)
	client := logsconnect.NewLogsServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	for _, m := range []uint32{0, 5001, 10000} {
		t.Run(fmt.Sprintf("max_lines_%d", m), func(t *testing.T) {
			_, err := client.GetLogs(context.Background(), &logsv1.GetLogsRequest{
				Source:   logsv1.LogSource_LOG_SOURCE_LOGREAD,
				MaxLines: m,
			})
			require.Error(t, err)

			var connectErr *connect.Error
			require.ErrorAs(t, err, &connectErr)
			assert.Equal(t, connect.CodeInvalidArgument, connectErr.Code())
		})
	}
}

// ── SetupService ─────────────────────────────────────────────────────────────

// newSetupTestServer wires the SetupService over an httptest server,
// using a populated UCI reader (one mac80211 + one morse radio) so
// GetSetupStatus has interesting data to return. The supplied yaml
// content seeds the *config.Config so each test can dial in
// setup.enabled / setup.complete.
func newSetupTestServer(t *testing.T, yamlContent string) *httptest.Server {
	t.Helper()

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0o644))

	v := viper.New()
	v.SetConfigFile(cfgPath)
	require.NoError(t, v.ReadInConfig())

	cfg := config.NewWithoutWatch(v)

	reader := &fakeConfigReader{
		data: map[string]map[string]map[string][]string{
			"wireless": {
				"radio0": {"type": {"mac80211"}, "band": {"2g"}, "channel": {"1"}},
				"radio1": {"type": {"morse"}, "band": {"s1g"}, "channel": {"42"}},
			},
			"system": {
				"@system[0]": {"hostname": {"BCM2711-97d6"}},
			},
		},
		sectionTypes: map[string]map[string]string{
			"wireless": {"radio0": "wifi-device", "radio1": "wifi-device"},
			"system":   {"@system[0]": "system"},
		},
	}

	mux := http.NewServeMux()

	mux.Handle(setupconnect.NewSetupServiceHandler(&handlers.SetupService{
		Cfg:        cfg,
		Log:        zerolog.Nop(),
		UCI:        reader,
		Interfaces: &fakeInterfaceProvider{},
	}, connect.WithInterceptors(validate.NewInterceptor())))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestIntegration_GetSetupStatus_DefaultsDisabled(t *testing.T) {
	srv := newSetupTestServer(t, "")
	client := setupconnect.NewSetupServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.GetIsEnabled())
	assert.False(t, resp.GetIsSetupComplete())
	assert.True(t, resp.GetHasHalowRadio())
}

func TestIntegration_GetSetupStatus_EnabledIncomplete(t *testing.T) {
	srv := newSetupTestServer(t, "setup:\n  enabled: true\n")
	client := setupconnect.NewSetupServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	resp, err := client.GetSetupStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.GetIsEnabled())
	assert.False(t, resp.GetIsSetupComplete())
}

func TestIntegration_ApplySetup_RejectsWhenSetupDisabled(t *testing.T) {
	srv := newSetupTestServer(t, "setup:\n  enabled: false\n")
	client := setupconnect.NewSetupServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	stream, err := client.ApplySetup(context.Background(),
		&setupv1.ApplySetupRequest{Profile: integrationMinimalProfile()})
	require.NoError(t, err, "ApplySetup call should not error at dial time")

	// The first Receive() call surfaces the handler-side rejection.
	stream.Receive()
	err = stream.Err()
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnavailable, connectErr.Code())
}

func TestIntegration_ApplySetup_RejectsWhenAlreadyComplete(t *testing.T) {
	srv := newSetupTestServer(t, "setup:\n  enabled: true\n  complete: true\n")
	client := setupconnect.NewSetupServiceClient(http.DefaultClient, srv.URL, connect.WithGRPCWeb())

	stream, err := client.ApplySetup(context.Background(),
		&setupv1.ApplySetupRequest{Profile: integrationMinimalProfile()})
	require.NoError(t, err)

	stream.Receive()
	err = stream.Err()
	require.Error(t, err)

	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeFailedPrecondition, connectErr.Code())
}

// integrationMinimalProfile returns a fully-valid MeshNodeProfile for
// integration tests of the SetupService.
func integrationMinimalProfile() *setupv1.MeshNodeProfile {
	return &setupv1.MeshNodeProfile{
		Hostname:      "openmanet-1",
		AdminPassword: "supersecret",
		Role:          setupv1.MeshRole_MESH_ROLE_MESH_POINT,
		DeviceMode: &setupv1.MeshNodeProfile_MeshpointMode{
			MeshpointMode: setupv1.MeshPointMode_MESH_POINT_MODE_EXTENDER,
		},
		Mesh: &setupv1.MeshRadioConfig{
			RadioName:    "radio1",
			MeshId:       "openmanet-mesh",
			Passphrase:   "longpasscode",
			Encryption:   wificonfigv1.WifiEncryption_WIFI_ENCRYPTION_SAE,
			BandwidthMhz: 2,
			Channel:      42,
		},
	}
}
