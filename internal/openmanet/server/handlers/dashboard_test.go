package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/openmanet/openmanetd/internal/api/openmanet/dashboard/v1"
	dashboardconnect "github.com/openmanet/openmanetd/internal/api/openmanet/dashboard/v1/dashboardv1connect"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/openmanet/openmanetd/internal/util/board"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// --- Mock implementations ---

type mockBoardProvider struct {
	board *board.Board
	err   error
}

func (m *mockBoardProvider) GetBoard() (*board.Board, error) {
	return m.board, m.err
}

type mockSysInfoProvider struct {
	hostname string
	kernel   string
	arch     string
	uptime   time.Duration
	memTotal int64
	memAvail int64
	cpuLoad  float32
	overlay  system.OverlayUsage
	err      error
}

func (m *mockSysInfoProvider) GetHostname() (string, error) {
	if m.err != nil {
		return "", m.err
	}

	return m.hostname, nil
}

func (m *mockSysInfoProvider) GetKernelVersion() (string, error) {
	if m.err != nil {
		return "", m.err
	}

	return m.kernel, nil
}

func (m *mockSysInfoProvider) GetArchitecture() (string, error) {
	if m.err != nil {
		return "", m.err
	}

	return m.arch, nil
}

func (m *mockSysInfoProvider) GetUptime() (time.Duration, error) {
	if m.err != nil {
		return 0, m.err
	}

	return m.uptime, nil
}

func (m *mockSysInfoProvider) GetMemoryInfo() (*system.MemoryInfo, error) {
	if m.err != nil {
		return nil, m.err
	}

	return &system.MemoryInfo{TotalBytes: m.memTotal, AvailableBytes: m.memAvail}, nil
}

func (m *mockSysInfoProvider) GetCPULoadPercent() (float32, error) {
	if m.err != nil {
		return 0, m.err
	}

	return m.cpuLoad, nil
}

func (m *mockSysInfoProvider) GetOverlayUsage() (*system.OverlayUsage, error) {
	if m.err != nil {
		return nil, m.err
	}

	return &m.overlay, nil
}

type mockFirmwareProvider struct {
	info *system.FirmwareInfo
	err  error
}

func (m *mockFirmwareProvider) GetFirmwareInfo() (*system.FirmwareInfo, error) {
	return m.info, m.err
}

type mockInterfaceProvider struct {
	ifaces []network.NetworkInterfaceInfo
	err    error
}

func (m *mockInterfaceProvider) ListAll() ([]network.NetworkInterfaceInfo, error) {
	return m.ifaces, m.err
}

type mockWifiProvider struct {
	count int
	err   error
}

func (m *mockWifiProvider) GetMeshNeighborCount() (int, error) {
	return m.count, m.err
}

type mockOriginatorProvider struct {
	originators []batmanadv.Originator
	err         error
}

func (m *mockOriginatorProvider) GetOriginators() ([]batmanadv.Originator, error) {
	return m.originators, m.err
}

type mockTailscaleProvider struct {
	running bool
}

func (m *mockTailscaleProvider) IsRunning() bool {
	return m.running
}

type mockServiceChecker struct {
	statuses []system.ServiceStatus
	err      error
}

func (m *mockServiceChecker) CheckServices(_ context.Context, _ []string) ([]system.ServiceStatus, error) {
	return m.statuses, m.err
}

type mockQuickActionExecutor struct {
	rebootErr    error
	networkErr   error
	openmanetErr error
}

func (m *mockQuickActionExecutor) Reboot(_ context.Context) error            { return m.rebootErr }
func (m *mockQuickActionExecutor) RestartNetwork(_ context.Context) error    { return m.networkErr }
func (m *mockQuickActionExecutor) RestartOpenmanetd(_ context.Context) error { return m.openmanetErr }

// --- Tests ---

func newTestDashboardService() *DashboardService {
	return &DashboardService{
		Log: zerolog.Nop(),
		Board: &mockBoardProvider{
			board: &board.Board{
				Model: board.Model{Name: "MorseMicro HaLowLink 2"},
			},
		},
		SysInfo: &mockSysInfoProvider{
			hostname: "HaLowLink2-6cff",
			kernel:   "5.15.150",
			arch:     "mipsel_24kc",
			memTotal: 248168 * 1024,
			memAvail: 80000 * 1024,
			cpuLoad:  12.0,
			overlay:  system.OverlayUsage{TotalBytes: 3700000, UsedBytes: 3100000},
		},
		Firmware: &mockFirmwareProvider{
			info: &system.FirmwareInfo{Description: "OpenWrt 23.05.3 / OpenMANET 1.7.0"},
		},
		Interfaces: &mockInterfaceProvider{
			ifaces: []network.NetworkInterfaceInfo{
				{Name: "eth0", LinkType: network.LinkTypeEthernet, State: network.OperStateDown},
				{Name: "br-ahwlan", LinkType: network.LinkTypeBridge, State: network.OperStateUp, IP: "10.41.25.72/16"},
				{Name: "phy1-ap0", LinkType: network.LinkTypeHaLowMesh, State: network.OperStateUp},
				{Name: "bat0", LinkType: network.LinkTypeBatman, State: network.OperStateUp},
				{Name: "tailscale0", LinkType: network.LinkTypeUnknown, State: network.OperStateDown},
			},
		},
		Wifi: &mockWifiProvider{count: 3},
		Originators: &mockOriginatorProvider{
			originators: []batmanadv.Originator{
				{OrigAddress: "aa:bb:cc:dd:ee:01"},
				{OrigAddress: "aa:bb:cc:dd:ee:02"},
				{OrigAddress: "aa:bb:cc:dd:ee:03"},
				{OrigAddress: "aa:bb:cc:dd:ee:04"},
			},
		},
		Tailscale: &mockTailscaleProvider{running: false},
		Services: &mockServiceChecker{
			statuses: []system.ServiceStatus{
				{Name: "openmanetd", State: system.ServiceStateRunning, PID: 1842},
				{Name: "openmanet-webui", State: system.ServiceStateRunning, PID: 2105},
				{Name: "dnsmasq", State: system.ServiceStateRunning, PID: 987},
				{Name: "hostapd", State: system.ServiceStateRunning, PID: 1102},
				{Name: "gpsd", State: system.ServiceStateRunning, PID: 1456},
				{Name: "batadv-vis", State: system.ServiceStateStopped, PID: 0},
			},
		},
		Actions: &mockQuickActionExecutor{},
	}
}

func TestDashboardService_GetDashboardStatus_DeviceInfo(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	di := resp.DeviceInfo
	require.NotNil(t, di)
	assert.Equal(t, "HaLowLink2-6cff", di.Hostname)
	assert.Equal(t, "MorseMicro HaLowLink 2", di.Model)
	assert.Equal(t, "OpenWrt 23.05.3 / OpenMANET 1.7.0", di.Firmware)
	assert.Equal(t, "5.15.150", di.Kernel)
	assert.Equal(t, "mipsel_24kc", di.Architecture)
}

func TestDashboardService_GetDashboardStatus_SystemResources(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	sr := resp.SystemResources
	require.NotNil(t, sr)
	assert.NotNil(t, sr.LocalTime)
	assert.InDelta(t, 12.0, float64(sr.CpuLoadPercent), 0.1)
	assert.Equal(t, int64(248168*1024), sr.MemoryTotalBytes)
	assert.Equal(t, int64((248168-80000)*1024), sr.MemoryUsedBytes)
	assert.Equal(t, int64(3700000), sr.OverlayTotalBytes)
	assert.Equal(t, int64(3100000), sr.OverlayUsedBytes)
}

func TestDashboardService_GetDashboardStatus_NetworkSummary(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	ns := resp.NetworkSummary
	require.NotNil(t, ns)
	require.Len(t, ns.Entries, 5) // WAN, LAN, HaLow, BATMAN, Tailscale

	// WAN — disconnected
	wan := ns.Entries[0]
	assert.Equal(t, "eth0", wan.InterfaceName)
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_DISCONNECTED, wan.State)
	assert.Equal(t, "Disconnected", wan.Detail)

	// LAN — connected with IP
	lan := ns.Entries[1]
	assert.Equal(t, "br-ahwlan", lan.InterfaceName)
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED, lan.State)
	assert.Equal(t, "10.41.25.72/16", lan.Detail)

	// HaLow Mesh — 3 neighbors
	mesh := ns.Entries[2]
	assert.Contains(t, mesh.DisplayName, "HaLow Mesh")
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED, mesh.State)
	assert.Contains(t, mesh.Detail, "3 neighbors")

	// BATMAN — 4 originators
	batman := ns.Entries[3]
	assert.Contains(t, batman.DisplayName, "BATMAN")
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED, batman.State)
	assert.Contains(t, batman.Detail, "4 originators")

	// Tailscale — not connected
	ts := ns.Entries[4]
	assert.Contains(t, ts.DisplayName, "Tailscale")
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED, ts.State)
}

func TestDashboardService_GetDashboardStatus_ActiveServices(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	require.Len(t, resp.ActiveServices, 6)

	// openmanetd — running
	assert.Equal(t, "openmanetd", resp.ActiveServices[0].Name)
	assert.Equal(t, v1.ServiceStatus_SERVICE_STATUS_RUNNING, resp.ActiveServices[0].Status)
	assert.Equal(t, int32(1842), resp.ActiveServices[0].Pid)

	// batadv-vis — stopped
	assert.Equal(t, "batadv-vis", resp.ActiveServices[5].Name)
	assert.Equal(t, v1.ServiceStatus_SERVICE_STATUS_STOPPED, resp.ActiveServices[5].Status)
	assert.Equal(t, int32(0), resp.ActiveServices[5].Pid)
}

func TestDashboardService_ExecuteQuickAction_Reboot(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.ExecuteQuickAction(context.Background(), &v1.ExecuteQuickActionRequest{
		Action: v1.QuickAction_QUICK_ACTION_REBOOT_DEVICE,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Contains(t, *resp.Message, "Reboot")
}

func TestDashboardService_ExecuteQuickAction_RestartNetwork(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.ExecuteQuickAction(context.Background(), &v1.ExecuteQuickActionRequest{
		Action: v1.QuickAction_QUICK_ACTION_RESTART_NETWORK,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestDashboardService_ExecuteQuickAction_RestartOpenmanetd(t *testing.T) {
	svc := newTestDashboardService()
	resp, err := svc.ExecuteQuickAction(context.Background(), &v1.ExecuteQuickActionRequest{
		Action: v1.QuickAction_QUICK_ACTION_RESTART_OPENMANETD,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestDashboardService_ExecuteQuickAction_UnknownAction(t *testing.T) {
	svc := newTestDashboardService()
	_, err := svc.ExecuteQuickAction(context.Background(), &v1.ExecuteQuickActionRequest{
		Action: v1.QuickAction_QUICK_ACTION_UNSPECIFIED,
	})
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestDashboardService_ExecuteQuickAction_Error(t *testing.T) {
	svc := newTestDashboardService()
	svc.Actions = &mockQuickActionExecutor{rebootErr: errors.New("permission denied")}
	_, err := svc.ExecuteQuickAction(context.Background(), &v1.ExecuteQuickActionRequest{
		Action: v1.QuickAction_QUICK_ACTION_REBOOT_DEVICE,
	})
	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestDashboardService_GetDashboardStatus_ServiceCheckError(t *testing.T) {
	svc := newTestDashboardService()
	svc.Services = &mockServiceChecker{err: errors.New("service check failed")}
	_, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestDashboardService_GetDashboardStatus_NoTailscaleProvider(t *testing.T) {
	svc := newTestDashboardService()
	svc.Tailscale = nil
	// Also remove tailscale0 from interfaces so no tailscale entry shows
	svc.Interfaces = &mockInterfaceProvider{
		ifaces: []network.NetworkInterfaceInfo{
			{Name: "eth0", LinkType: network.LinkTypeEthernet, State: network.OperStateDown},
		},
	}
	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	// Should not contain tailscale entry
	for _, entry := range resp.NetworkSummary.Entries {
		assert.NotContains(t, entry.DisplayName, "Tailscale")
	}
}

func TestDashboardService_GetDashboardStatus_IntegrationViaConnectRPC(t *testing.T) {
	svc := newTestDashboardService()

	mux := http.NewServeMux()
	mux.Handle(dashboardconnect.NewDashboardServiceHandler(svc))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := dashboardconnect.NewDashboardServiceClient(srv.Client(), srv.URL)

	resp, err := client.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "HaLowLink2-6cff", resp.DeviceInfo.Hostname)
	assert.Len(t, resp.ActiveServices, 6)
	assert.Len(t, resp.NetworkSummary.Entries, 5)
}

func TestDashboardService_ExecuteQuickAction_IntegrationViaConnectRPC(t *testing.T) {
	svc := newTestDashboardService()

	mux := http.NewServeMux()
	mux.Handle(dashboardconnect.NewDashboardServiceHandler(svc))

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := dashboardconnect.NewDashboardServiceClient(srv.Client(), srv.URL)

	resp, err := client.ExecuteQuickAction(context.Background(), &v1.ExecuteQuickActionRequest{
		Action: v1.QuickAction_QUICK_ACTION_RESTART_NETWORK,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestServiceStateToProto(t *testing.T) {
	assert.Equal(t, v1.ServiceStatus_SERVICE_STATUS_RUNNING, serviceStateToProto(system.ServiceStateRunning))
	assert.Equal(t, v1.ServiceStatus_SERVICE_STATUS_STOPPED, serviceStateToProto(system.ServiceStateStopped))
	assert.Equal(t, v1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED, serviceStateToProto(system.ServiceStateUnknown))
}

func findTailscaleEntry(entries []*v1.NetworkSummaryEntry) *v1.NetworkSummaryEntry {
	for _, e := range entries {
		if e.InterfaceName == "tailscale0" {
			return e
		}
	}
	return nil
}

func TestDashboardService_BuildTailscaleEntry(t *testing.T) {
	tests := []struct {
		name         string
		tailscale    TailscaleStatusProvider
		ifaces       []network.NetworkInterfaceInfo
		wantState    v1.NetworkInterfaceState
		wantDetail   string
		wantNilEntry bool
	}{
		{
			name:      "running with interface present shows connected with IP",
			tailscale: &mockTailscaleProvider{running: true},
			ifaces: []network.NetworkInterfaceInfo{
				{Name: "tailscale0", IP: "100.64.0.5/32", State: network.OperStateUp},
			},
			wantState:  v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED,
			wantDetail: "100.64.0.5/32",
		},
		{
			name:      "not running with interface present shows not connected",
			tailscale: &mockTailscaleProvider{running: false},
			ifaces: []network.NetworkInterfaceInfo{
				{Name: "tailscale0", IP: "100.64.0.5/32", State: network.OperStateUp},
			},
			wantState:  v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED,
			wantDetail: "Not connected",
		},
		{
			name:      "running without interface present shows not connected",
			tailscale: &mockTailscaleProvider{running: true},
			ifaces: []network.NetworkInterfaceInfo{
				{Name: "eth0", State: network.OperStateUp},
			},
			wantState:  v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED,
			wantDetail: "Not connected",
		},
		{
			name:      "nil provider with interface present shows not connected",
			tailscale: nil,
			ifaces: []network.NetworkInterfaceInfo{
				{Name: "tailscale0", IP: "100.64.0.5/32", State: network.OperStateUp},
			},
			wantState:  v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED,
			wantDetail: "Not connected",
		},
		{
			name:         "nil provider without interface returns nil",
			tailscale:    nil,
			ifaces:       []network.NetworkInterfaceInfo{{Name: "eth0"}},
			wantNilEntry: true,
		},
		{
			name:      "running with interface present but empty IP",
			tailscale: &mockTailscaleProvider{running: true},
			ifaces: []network.NetworkInterfaceInfo{
				{Name: "tailscale0", IP: "", State: network.OperStateUp},
			},
			wantState:  v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED,
			wantDetail: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &DashboardService{
				Log:       zerolog.Nop(),
				Tailscale: tc.tailscale,
			}

			entry := svc.buildTailscaleEntry(tc.ifaces)

			if tc.wantNilEntry {
				assert.Nil(t, entry)
				return
			}

			require.NotNil(t, entry)
			assert.Equal(t, "tailscale0", entry.InterfaceName)
			assert.Equal(t, "Tailscale (tailscale0)", entry.DisplayName)
			assert.Equal(t, tc.wantState, entry.State)
			assert.Equal(t, tc.wantDetail, entry.Detail)
		})
	}
}

func TestDashboardService_GetDashboardStatus_TailscaleRunning(t *testing.T) {
	svc := newTestDashboardService()
	svc.Tailscale = &mockTailscaleProvider{running: true}
	svc.Interfaces = &mockInterfaceProvider{
		ifaces: []network.NetworkInterfaceInfo{
			{Name: "eth0", LinkType: network.LinkTypeEthernet, State: network.OperStateDown},
			{Name: "tailscale0", LinkType: network.LinkTypeUnknown, State: network.OperStateUp, IP: "100.64.0.5/32"},
		},
	}

	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	entry := findTailscaleEntry(resp.NetworkSummary.Entries)
	require.NotNil(t, entry, "tailscale entry should be present")
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_CONNECTED, entry.State)
	assert.Equal(t, "100.64.0.5/32", entry.Detail)
}

func TestDashboardService_GetDashboardStatus_TailscaleNotRunning(t *testing.T) {
	svc := newTestDashboardService()
	svc.Tailscale = &mockTailscaleProvider{running: false}
	svc.Interfaces = &mockInterfaceProvider{
		ifaces: []network.NetworkInterfaceInfo{
			{Name: "eth0", LinkType: network.LinkTypeEthernet, State: network.OperStateDown},
			{Name: "tailscale0", LinkType: network.LinkTypeUnknown, State: network.OperStateUp, IP: "100.64.0.5/32"},
		},
	}

	resp, err := svc.GetDashboardStatus(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	entry := findTailscaleEntry(resp.NetworkSummary.Entries)
	require.NotNil(t, entry, "tailscale entry should be present")
	assert.Equal(t, v1.NetworkInterfaceState_NETWORK_INTERFACE_STATE_NOT_CONNECTED, entry.State)
	assert.Equal(t, "Not connected", entry.Detail)
}

func TestDashboardService_MonitoredServices_Default(t *testing.T) {
	svc := newTestDashboardService()
	svc.MonitoredServices = nil
	assert.Equal(t, system.DefaultMonitoredServices(), svc.monitoredServices())
}

func TestDashboardService_MonitoredServices_Custom(t *testing.T) {
	svc := newTestDashboardService()
	svc.MonitoredServices = []string{"custom-svc"}
	assert.Equal(t, []string{"custom-svc"}, svc.monitoredServices())
}
