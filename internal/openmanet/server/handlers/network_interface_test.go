package handlers_test

import (
	"context"
	"errors"
	"testing"

	niv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ── Fakes ────────────────────────────────────────────────────────────────────

type fakeInterfaceProvider struct {
	infos []network.NetworkInterfaceInfo
	err   error
}

func (f *fakeInterfaceProvider) ListAll() ([]network.NetworkInterfaceInfo, error) {
	return f.infos, f.err
}

type fakeDHCPConfigProvider struct {
	dhcpCfg    *network.UCIDHCP
	dhcpErr    error
	dnsmasqCfg *network.UCIDnsmasq
	dnsmasqErr error
	staticHost []network.UCIStaticHost
	staticErr  error
	baseIP     string
	baseIPErr  error
}

func (f *fakeDHCPConfigProvider) GetDHCPConfig(_ string) (*network.UCIDHCP, error) {
	return f.dhcpCfg, f.dhcpErr
}

func (f *fakeDHCPConfigProvider) GetDnsmasqConfig() (*network.UCIDnsmasq, error) {
	return f.dnsmasqCfg, f.dnsmasqErr
}

func (f *fakeDHCPConfigProvider) GetStaticHosts() ([]network.UCIStaticHost, error) {
	return f.staticHost, f.staticErr
}

func (f *fakeDHCPConfigProvider) GetNetworkBaseIP(_ string) (string, error) {
	return f.baseIP, f.baseIPErr
}

type fakeLeaseProvider struct {
	resp *network.DHCPLeasesResponse
	err  error
}

func (f *fakeLeaseProvider) GetCurrentDHCPLeases(_ context.Context) (*network.DHCPLeasesResponse, error) {
	return f.resp, f.err
}

// ── ListNetworkInterfaces ────────────────────────────────────────────────────

func TestListNetworkInterfaces_Success(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		Interfaces: &fakeInterfaceProvider{
			infos: []network.NetworkInterfaceInfo{
				{
					Name:     "br-ahwlan",
					LinkType: network.LinkTypeBridge,
					MAC:      "C8:3E:A7:00:6C:FF",
					IP:       "10.41.25.72/16",
					State:    network.OperStateUp,
					RxBytes:  142300000,
					TxBytes:  87600000,
					MTU:      1500,
				},
				{
					Name:     "eth0",
					LinkType: network.LinkTypeEthernet,
					MAC:      "C8:3E:A7:00:6C:FE",
					IP:       "",
					State:    network.OperStateDown,
					RxBytes:  0,
					TxBytes:  0,
					MTU:      1500,
				},
			},
		},
	}

	resp, err := svc.ListNetworkInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 2)

	br := resp.GetInterfaces()[0]
	assert.Equal(t, "br-ahwlan", br.GetName())
	assert.Equal(t, niv1.InterfaceType_INTERFACE_TYPE_BRIDGE, br.GetType())
	assert.Equal(t, "10.41.25.72/16", br.GetIpAddress())
	assert.Equal(t, "C8:3E:A7:00:6C:FF", br.GetMacAddress())
	assert.Equal(t, niv1.InterfaceStatus_INTERFACE_STATUS_UP, br.GetStatus())
	assert.Equal(t, uint64(142300000), br.GetRxBytes())
	assert.Equal(t, uint64(87600000), br.GetTxBytes())
	assert.Equal(t, int32(1500), br.GetMtu())

	eth := resp.GetInterfaces()[1]
	assert.Equal(t, "eth0", eth.GetName())
	assert.Equal(t, niv1.InterfaceType_INTERFACE_TYPE_ETHERNET, eth.GetType())
	assert.Equal(t, niv1.InterfaceStatus_INTERFACE_STATUS_DOWN, eth.GetStatus())
}

func TestListNetworkInterfaces_HidesMorse0(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		Interfaces: &fakeInterfaceProvider{
			infos: []network.NetworkInterfaceInfo{
				{Name: "br-ahwlan", LinkType: network.LinkTypeBridge, State: network.OperStateUp},
				{Name: "morse0", LinkType: network.LinkTypeHaLowMesh, State: network.OperStateDown},
				{Name: "eth0", LinkType: network.LinkTypeEthernet, State: network.OperStateUp},
			},
		},
	}

	resp, err := svc.ListNetworkInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 2)

	names := []string{resp.GetInterfaces()[0].GetName(), resp.GetInterfaces()[1].GetName()}
	assert.NotContains(t, names, "morse0")
	assert.Contains(t, names, "br-ahwlan")
	assert.Contains(t, names, "eth0")
}

func TestListNetworkInterfaces_Empty(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log:        zerolog.Nop(),
		Interfaces: &fakeInterfaceProvider{},
	}

	resp, err := svc.ListNetworkInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetInterfaces())
}

func TestListNetworkInterfaces_Error(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log:        zerolog.Nop(),
		Interfaces: &fakeInterfaceProvider{err: errors.New("netlink fail")},
	}

	_, err := svc.ListNetworkInterfaces(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

// ── GetDHCPServerConfig ──────────────────────────────────────────────────────

func TestGetDHCPServerConfig_Success(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			dhcpCfg:    &network.UCIDHCP{Interface: "ahwlan", Start: "100", Limit: "155", LeaseTime: "12h"},
			dnsmasqCfg: &network.UCIDnsmasq{Local: "/lan/"},
			baseIP:     "10.41.0.0",
		},
		Leases: &fakeLeaseProvider{
			resp: &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "laptop", IPAddr: "10.41.0.101"},
					{Hostname: "phone", IPAddr: "10.41.0.102"},
					{Hostname: "", IPAddr: "10.41.0.103"},
				},
			},
		},
	}

	resp, err := svc.GetDHCPServerConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	cfg := resp.GetConfig()
	assert.Equal(t, "ahwlan", cfg.GetInterfaceName())
	assert.Equal(t, "10.41.0.100", cfg.GetRangeStart())
	assert.Equal(t, "10.41.0.254", cfg.GetRangeEnd())
	assert.Equal(t, "12h", cfg.GetLeaseTime())
	assert.True(t, cfg.GetDnsForwardingEnabled())
	assert.Equal(t, int32(3), cfg.GetActiveLeaseCount())
}

func TestGetDHCPServerConfig_DefaultLeaseTime(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			dhcpCfg:    &network.UCIDHCP{Interface: "ahwlan", Start: "100", Limit: "155"},
			dnsmasqCfg: &network.UCIDnsmasq{},
			baseIP:     "10.41.0.0",
		},
		Leases: &fakeLeaseProvider{
			resp: &network.DHCPLeasesResponse{},
		},
	}

	resp, err := svc.GetDHCPServerConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "12h", resp.GetConfig().GetLeaseTime())
	assert.False(t, resp.GetConfig().GetDnsForwardingEnabled())
}

func TestGetDHCPServerConfig_DHCPError(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			dhcpErr: errors.New("UCI fail"),
		},
	}

	_, err := svc.GetDHCPServerConfig(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

func TestGetDHCPServerConfig_LeaseCountOnError(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			dhcpCfg:    &network.UCIDHCP{Interface: "ahwlan", Start: "100", Limit: "155"},
			dnsmasqCfg: &network.UCIDnsmasq{},
			baseIP:     "10.41.0.0",
		},
		Leases: &fakeLeaseProvider{err: errors.New("ubus fail")},
	}

	resp, err := svc.GetDHCPServerConfig(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	// Lease count should be 0 when ubus fails (non-fatal)
	assert.Equal(t, int32(0), resp.GetConfig().GetActiveLeaseCount())
}

// ── ListActiveDHCPLeases ─────────────────────────────────────────────────────

func TestListActiveDHCPLeases_Success(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		Leases: &fakeLeaseProvider{
			resp: &network.DHCPLeasesResponse{
				DHCPLeases: []network.DHCPLease{
					{Hostname: "laptop-bravo", MacAddr: "D4:6D:6D:1A:2B:3C", IPAddr: "10.41.0.101", Expires: 42120},
					{Hostname: "phone-alpha", MacAddr: "F0:18:98:4D:5E:6F", IPAddr: "10.41.0.102", Expires: 33300},
				},
			},
		},
	}

	resp, err := svc.ListActiveDHCPLeases(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetLeases(), 2)

	assert.Equal(t, "laptop-bravo", resp.GetLeases()[0].GetHostname())
	assert.Equal(t, "D4:6D:6D:1A:2B:3C", resp.GetLeases()[0].GetMacAddress())
	assert.Equal(t, "10.41.0.101", resp.GetLeases()[0].GetIpAddress())
	assert.Equal(t, int32(42120), resp.GetLeases()[0].GetExpiresSeconds())

	assert.Equal(t, "phone-alpha", resp.GetLeases()[1].GetHostname())
}

func TestListActiveDHCPLeases_Empty(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		Leases: &fakeLeaseProvider{
			resp: &network.DHCPLeasesResponse{},
		},
	}

	resp, err := svc.ListActiveDHCPLeases(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetLeases())
}

func TestListActiveDHCPLeases_Error(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log:    zerolog.Nop(),
		Leases: &fakeLeaseProvider{err: errors.New("ubus fail")},
	}

	_, err := svc.ListActiveDHCPLeases(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

// ── ListStaticDHCPLeases ─────────────────────────────────────────────────────

func TestListStaticDHCPLeases_Success(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			staticHost: []network.UCIStaticHost{
				{Name: "printer-office", MAC: "AA:BB:CC:11:22:33", IP: "10.41.0.10"},
				{Name: "camera-north", MAC: "AA:BB:CC:44:55:66", IP: "10.41.0.11"},
				{Name: "nas-storage", MAC: "AA:BB:CC:77:88:99", IP: "10.41.0.12"},
			},
		},
	}

	resp, err := svc.ListStaticDHCPLeases(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetLeases(), 3)

	assert.Equal(t, "printer-office", resp.GetLeases()[0].GetHostname())
	assert.Equal(t, "AA:BB:CC:11:22:33", resp.GetLeases()[0].GetMacAddress())
	assert.Equal(t, "10.41.0.10", resp.GetLeases()[0].GetIpAddress())
}

func TestListStaticDHCPLeases_Empty(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			staticHost: []network.UCIStaticHost{},
		},
	}

	resp, err := svc.ListStaticDHCPLeases(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetLeases())
}

func TestListStaticDHCPLeases_Error(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		DHCP: &fakeDHCPConfigProvider{
			staticErr: errors.New("UCI error"),
		},
	}

	_, err := svc.ListStaticDHCPLeases(context.Background(), &emptypb.Empty{})
	require.Error(t, err)
}

// ── Enum Mapping ─────────────────────────────────────────────────────────────

func TestLinkTypeMapping_AllTypes(t *testing.T) {
	svc := &handlers.NetworkInterfaceService{
		Log: zerolog.Nop(),
		Interfaces: &fakeInterfaceProvider{
			infos: []network.NetworkInterfaceInfo{
				{Name: "br0", LinkType: network.LinkTypeBridge, State: network.OperStateUp},
				{Name: "eth0", LinkType: network.LinkTypeEthernet, State: network.OperStateDown},
				{Name: "wlan0", LinkType: network.LinkTypeWiFiAP, State: network.OperStateUp},
				{Name: "phy1", LinkType: network.LinkTypeHaLowMesh},
				{Name: "bat0", LinkType: network.LinkTypeBatman},
				{Name: "lo", LinkType: network.LinkTypeLoopback},
				{Name: "vx0", LinkType: network.LinkTypeVXLAN},
				{Name: "tun0", LinkType: network.LinkTypeUnknown},
			},
		},
	}

	resp, err := svc.ListNetworkInterfaces(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.Len(t, resp.GetInterfaces(), 8)

	expected := []niv1.InterfaceType{
		niv1.InterfaceType_INTERFACE_TYPE_BRIDGE,
		niv1.InterfaceType_INTERFACE_TYPE_ETHERNET,
		niv1.InterfaceType_INTERFACE_TYPE_WIFI_AP,
		niv1.InterfaceType_INTERFACE_TYPE_HALOW_MESH,
		niv1.InterfaceType_INTERFACE_TYPE_BATMAN,
		niv1.InterfaceType_INTERFACE_TYPE_LOOPBACK,
		niv1.InterfaceType_INTERFACE_TYPE_VXLAN,
		niv1.InterfaceType_INTERFACE_TYPE_UNSPECIFIED,
	}

	for i, iface := range resp.GetInterfaces() {
		assert.Equal(t, expected[i], iface.GetType(), "interface %s", iface.GetName())
	}
}
