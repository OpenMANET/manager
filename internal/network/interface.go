package network

import (
	"fmt"
	"net"
)

type NetworkInterface struct {
	Name  string
	MTU   int
	MAC   string
	IP    []IPAddress
	Flags net.Flags
}

type IPAddress struct {
	IP        net.IP
	Netmask   net.IPMask
	Broadcast net.IP
}

// GetInterfaceByName retrieves information about a network interface by its name.
// It returns an NetworkInterface struct containing details such as the interface's name,
// MTU, flags, MAC address, and associated IP addresses. If the interface is not found
// or an error occurs while fetching interfaces, an empty NetworkInterface is returned.
//
// Parameters:
//   - name: The name of the network interface to look up.
//
// Returns:
//   - NetworkInterface: Struct with details of the specified network interface.
func GetInterfaceByName(name string) NetworkInterface {
	// Get all network interface information of the system
	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println("Failed to get network interface information: ", err)
		return NetworkInterface{}
	}

	for _, iface := range interfaces {
		if iface.Name == name {
			return NetworkInterface{
				Name:  iface.Name,
				MTU:   iface.MTU,
				Flags: iface.Flags,
				MAC:   iface.HardwareAddr.String(),
				IP:    getInterfaceIPAddresses(iface),
			}
		}
	}

	return NetworkInterface{}
}

func getInterfaceIPAddresses(iface net.Interface) []IPAddress {
	var ipAddresses []IPAddress

	addrs, err := iface.Addrs()
	if err != nil {
		fmt.Println("Failed to get IP addresses for interface: ", err)
		return ipAddresses
	}

	for _, addr := range addrs {
		var ip net.IP
		var netmask net.IPMask
		var broadcast net.IP

		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
			netmask = v.Mask
			broadcast = calculateBroadcastAddress(v)
		case *net.IPAddr:
			ip = v.IP
			netmask = ip.DefaultMask()
			broadcast = calculateBroadcastAddress(&net.IPNet{IP: v.IP, Mask: netmask})
		}

		ipAddresses = append(ipAddresses, IPAddress{
			IP:        ip,
			Netmask:   netmask,
			Broadcast: broadcast,
		})
	}

	return ipAddresses
}

func calculateBroadcastAddress(ipNet *net.IPNet) net.IP {
	ip := ipNet.IP.To4()
	if ip == nil {
		return nil
	}

	broadcast := make(net.IP, len(ip))
	for i := 0; i < len(ip); i++ {
		broadcast[i] = ip[i] | ^ipNet.Mask[i]
	}
	return broadcast
}
