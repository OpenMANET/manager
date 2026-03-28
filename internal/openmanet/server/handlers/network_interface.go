package handlers

import (
	"context"
	"strconv"

	"connectrpc.com/connect"
	niv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

// InterfaceProvider abstracts network interface enumeration for testability.
type InterfaceProvider interface {
	ListAll() ([]network.NetworkInterfaceInfo, error)
}

// DHCPConfigProvider abstracts DHCP configuration reading for testability.
type DHCPConfigProvider interface {
	GetDHCPConfig(section string) (*network.UCIDHCP, error)
	GetDnsmasqConfig() (*network.UCIDnsmasq, error)
	GetStaticHosts() ([]network.UCIStaticHost, error)
	GetNetworkBaseIP(interfaceName string) (string, error)
}

// LeaseProvider abstracts active DHCP lease reading for testability.
type LeaseProvider interface {
	GetCurrentDHCPLeases(ctx context.Context) (*network.DHCPLeasesResponse, error)
}

// NetworkInterfaceService implements the network_interfacev1connect.NetworkInterfaceServiceHandler.
type NetworkInterfaceService struct {
	Log        zerolog.Logger
	Interfaces InterfaceProvider
	DHCP       DHCPConfigProvider
	Leases     LeaseProvider
}

// ListNetworkInterfaces returns all network interfaces with status, addressing, and traffic stats.
func (s *NetworkInterfaceService) ListNetworkInterfaces(_ context.Context, _ *emptypb.Empty) (*niv1.ListNetworkInterfacesResponse, error) {
	infos, err := s.Interfaces.ListAll()
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to list network interfaces")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protos := make([]*niv1.NetworkInterface, 0, len(infos))

	for _, info := range infos {
		protos = append(protos, &niv1.NetworkInterface{
			Name:       info.Name,
			Type:       linkTypeToProto(info.LinkType),
			IpAddress:  info.IP,
			MacAddress: info.MAC,
			Status:     operStateToProto(info.State),
			RxBytes:    info.RxBytes,
			TxBytes:    info.TxBytes,
			Mtu:        int32(info.MTU),
		})
	}

	return &niv1.ListNetworkInterfacesResponse{Interfaces: protos}, nil
}

// GetDHCPServerConfig returns the DHCP server configuration for the primary interface.
func (s *NetworkInterfaceService) GetDHCPServerConfig(ctx context.Context, _ *emptypb.Empty) (*niv1.GetDHCPServerConfigResponse, error) {
	dhcpCfg, err := s.DHCP.GetDHCPConfig("ahwlan")
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to get DHCP config")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	dnsmasqCfg, err := s.DHCP.GetDnsmasqConfig()
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to get dnsmasq config")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	baseIP, err := s.DHCP.GetNetworkBaseIP("ahwlan")
	if err != nil {
		s.Log.Warn().Err(err).Msg("Could not determine base IP for DHCP range; using default")

		baseIP = network.DefaultNetworkAddress
	}

	start, _ := strconv.Atoi(dhcpCfg.Start)
	limit, _ := strconv.Atoi(dhcpCfg.Limit)

	rangeStart, _ := network.ComputeDHCPRangeStart(baseIP, start)
	rangeEnd, _ := network.ComputeDHCPRangeEnd(baseIP, start, limit)

	// Count active leases
	var activeLeaseCnt int32

	leases, leaseErr := s.Leases.GetCurrentDHCPLeases(ctx)
	if leaseErr == nil {
		activeLeaseCnt = int32(len(leases.GetDHCPLeases()))
	}

	// DNS forwarding: dnsmasq local option implies DNS forwarding is active
	dnsForwarding := dnsmasqCfg.Local != ""

	leaseTime := dhcpCfg.LeaseTime
	if leaseTime == "" {
		leaseTime = network.DefaultDHCPLeaseTime
	}

	return &niv1.GetDHCPServerConfigResponse{
		Config: &niv1.DHCPServerConfig{
			InterfaceName:       dhcpCfg.Interface,
			RangeStart:          rangeStart,
			RangeEnd:            rangeEnd,
			LeaseTime:           leaseTime,
			DnsForwardingEnabled: dnsForwarding,
			ActiveLeaseCount:    activeLeaseCnt,
		},
	}, nil
}

// ListActiveDHCPLeases returns all currently active DHCP leases.
func (s *NetworkInterfaceService) ListActiveDHCPLeases(ctx context.Context, _ *emptypb.Empty) (*niv1.ListActiveDHCPLeasesResponse, error) {
	resp, err := s.Leases.GetCurrentDHCPLeases(ctx)
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to get active DHCP leases")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	leases := resp.GetDHCPLeases()
	protos := make([]*niv1.ActiveDHCPLease, 0, len(leases))

	for _, l := range leases {
		protos = append(protos, &niv1.ActiveDHCPLease{
			Hostname:       l.Hostname,
			MacAddress:     l.MacAddr,
			IpAddress:      l.IPAddr,
			ExpiresSeconds: int32(l.Expires),
		})
	}

	return &niv1.ListActiveDHCPLeasesResponse{Leases: protos}, nil
}

// ListStaticDHCPLeases returns all configured static DHCP host reservations.
func (s *NetworkInterfaceService) ListStaticDHCPLeases(_ context.Context, _ *emptypb.Empty) (*niv1.ListStaticDHCPLeasesResponse, error) {
	hosts, err := s.DHCP.GetStaticHosts()
	if err != nil {
		s.Log.Error().Err(err).Msg("Failed to get static DHCP leases")

		return nil, connect.NewError(connect.CodeInternal, err)
	}

	protos := make([]*niv1.StaticDHCPLease, 0, len(hosts))

	for _, h := range hosts {
		protos = append(protos, &niv1.StaticDHCPLease{
			Hostname:   h.Name,
			MacAddress: h.MAC,
			IpAddress:  h.IP,
		})
	}

	return &niv1.ListStaticDHCPLeasesResponse{Leases: protos}, nil
}

// ── Enum Mapping ─────────────────────────────────────────────────────────────

func linkTypeToProto(lt network.InterfaceLinkType) niv1.InterfaceType {
	switch lt {
	case network.LinkTypeBridge:
		return niv1.InterfaceType_INTERFACE_TYPE_BRIDGE
	case network.LinkTypeEthernet:
		return niv1.InterfaceType_INTERFACE_TYPE_ETHERNET
	case network.LinkTypeWiFiAP:
		return niv1.InterfaceType_INTERFACE_TYPE_WIFI_AP
	case network.LinkTypeHaLowMesh:
		return niv1.InterfaceType_INTERFACE_TYPE_HALOW_MESH
	case network.LinkTypeBatman:
		return niv1.InterfaceType_INTERFACE_TYPE_BATMAN
	case network.LinkTypeLoopback:
		return niv1.InterfaceType_INTERFACE_TYPE_LOOPBACK
	case network.LinkTypeVXLAN:
		return niv1.InterfaceType_INTERFACE_TYPE_VXLAN
	default:
		return niv1.InterfaceType_INTERFACE_TYPE_UNSPECIFIED
	}
}

func operStateToProto(s network.InterfaceOperState) niv1.InterfaceStatus {
	switch s {
	case network.OperStateUp:
		return niv1.InterfaceStatus_INTERFACE_STATUS_UP
	case network.OperStateDown:
		return niv1.InterfaceStatus_INTERFACE_STATUS_DOWN
	default:
		return niv1.InterfaceStatus_INTERFACE_STATUS_UNSPECIFIED
	}
}
