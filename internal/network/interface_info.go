package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/mdlayher/wifi"
	"github.com/vishvananda/netlink"
)

// InterfaceLinkType classifies a network interface.
type InterfaceLinkType int

const (
	LinkTypeUnknown   InterfaceLinkType = iota
	LinkTypeBridge                      // br-ahwlan, br-lan, etc.
	LinkTypeEthernet                    // eth0, eth1, etc.
	LinkTypeWiFiAP                      // wlan0 in AP mode
	LinkTypeHaLowMesh                   // phy1-ap0 (HaLow mesh)
	LinkTypeBatman                      // bat0 (batman-adv)
	LinkTypeLoopback                    // lo
	LinkTypeVXLAN                       // vxlan interfaces
)

// InterfaceOperState indicates whether the interface is operationally up or down.
type InterfaceOperState int

const (
	OperStateUnknown InterfaceOperState = iota
	OperStateUp
	OperStateDown
)

// NetworkInterfaceInfo holds runtime information about a network interface
// including traffic statistics retrieved from netlink.
type NetworkInterfaceInfo struct {
	Name     string
	MAC      string
	IP       string // primary IPv4 in CIDR notation, empty if none
	RxBytes  uint64
	TxBytes  uint64
	MTU      int
	LinkType InterfaceLinkType
	State    InterfaceOperState
}

// InterfaceInfoProvider abstracts interface listing for testability.
type InterfaceInfoProvider interface {
	ListAll() ([]NetworkInterfaceInfo, error)
}

// NetlinkInterfaceProvider is the production implementation using vishvananda/netlink.
type NetlinkInterfaceProvider struct {
	// WifiInterfaces returns WiFi interface metadata used to classify
	// interfaces by their actual type (MeshPoint, AP, etc.) rather than
	// relying on name heuristics. When nil, WiFi-specific classification
	// is skipped and those interfaces fall through to name-based rules.
	WifiInterfaces func() ([]*wifi.Interface, error)
}

// ListAll enumerates all network interfaces and returns their info.
func (p *NetlinkInterfaceProvider) ListAll() ([]NetworkInterfaceInfo, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, fmt.Errorf("netlink LinkList: %w", err)
	}

	// Build a name→wifi.InterfaceType map so classifyLink can use the
	// real WiFi type instead of guessing from the interface name.
	wifiTypes := map[string]wifi.InterfaceType{}

	if p.WifiInterfaces != nil {
		if ifaces, wifiErr := p.WifiInterfaces(); wifiErr == nil {
			for _, iface := range ifaces {
				wifiTypes[iface.Name] = iface.Type
			}
		}
	}

	infos := make([]NetworkInterfaceInfo, 0, len(links))

	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil {
			continue
		}

		info := NetworkInterfaceInfo{
			Name:     attrs.Name,
			LinkType: classifyLink(link, wifiTypes),
			MAC:      attrs.HardwareAddr.String(),
			MTU:      attrs.MTU,
			State:    operState(attrs),
		}

		// Traffic statistics
		if stats := attrs.Statistics; stats != nil {
			info.RxBytes = stats.RxBytes
			info.TxBytes = stats.TxBytes
		}

		// Primary IPv4 address in CIDR notation
		addrs, addrErr := netlink.AddrList(link, netlink.FAMILY_V4)
		if addrErr == nil && len(addrs) > 0 && addrs[0].IPNet != nil {
			info.IP = addrs[0].IPNet.String()
		}

		infos = append(infos, info)
	}

	return infos, nil
}

// classifyLink determines the InterfaceLinkType from the netlink link, its name,
// and an optional WiFi type map obtained from the wifi subsystem.
func classifyLink(link netlink.Link, wifiTypes map[string]wifi.InterfaceType) InterfaceLinkType {
	attrs := link.Attrs()
	name := attrs.Name

	// Check netlink link type first
	switch link.Type() {
	case "bridge":
		return LinkTypeBridge
	case "vxlan":
		return LinkTypeVXLAN
	}

	// Check for batman-adv (kernel reports type "batadv" or name starts with "bat")
	if link.Type() == "batadv" || strings.HasPrefix(name, "bat") {
		return LinkTypeBatman
	}

	// Loopback
	if attrs.Flags&net.FlagLoopback != 0 {
		return LinkTypeLoopback
	}

	// WiFi interfaces — use actual interface type from the wifi subsystem
	if wt, ok := wifiTypes[name]; ok {
		switch wt {
		case wifi.InterfaceTypeMeshPoint:
			return LinkTypeHaLowMesh
		case wifi.InterfaceTypeAP:
			return LinkTypeWiFiAP
		}
	}

	// Bridge member check via name heuristic
	if strings.HasPrefix(name, "br-") {
		return LinkTypeBridge
	}

	// Ethernet (eth*, en*)
	if strings.HasPrefix(name, "eth") || strings.HasPrefix(name, "en") {
		return LinkTypeEthernet
	}

	return LinkTypeUnknown
}

// operState maps the netlink OperState to our enum.
func operState(attrs *netlink.LinkAttrs) InterfaceOperState {
	if attrs.OperState == netlink.OperUp || attrs.Flags&net.FlagUp != 0 {
		return OperStateUp
	}

	return OperStateDown
}
