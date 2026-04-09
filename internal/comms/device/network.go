package device

import (
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
)

// IfaceByName returns the *net.Interface value for the named interface
// without requiring it to have an IPv4 address. Use this when the
// interface is a L2 carrier (e.g. a batman-adv bat0 interface) that
// only needs to be resolved so the caller can perform a multicast
// group join on it.
func IfaceByName(name string) (*net.Interface, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %q not found: %w", name, err)
	}

	return ifi, nil
}

// IfaceIPv4 returns the first IPv4 address on the named network interface
// together with the *net.Interface value (needed for multicast group join).
func IfaceIPv4(name string) (string, *net.Interface, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return "", nil, fmt.Errorf("interface %q not found: %w", name, err)
	}

	addrs, err := ifi.Addrs()
	if err != nil {
		return "", nil, fmt.Errorf("interface %q addrs: %w", name, err)
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}

		if ip4 := ipNet.IP.To4(); ip4 != nil {
			return ip4.String(), ifi, nil
		}
	}

	return "", nil, fmt.Errorf("interface %q has no IPv4 address", name)
}

// JoinMulticastGroup joins a UDP connection to an IPv4 multicast group.
func JoinMulticastGroup(ifi *net.Interface, conn *net.UDPConn, group net.IP) error {
	pc := ipv4.NewPacketConn(conn)
	if err := pc.JoinGroup(ifi, &net.UDPAddr{IP: group}); err != nil {
		return fmt.Errorf("join multicast group %s on %s: %w", group, ifi.Name, err)
	}

	return nil
}
