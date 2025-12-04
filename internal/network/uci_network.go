package network

import (
	"fmt"
	"net"
	"os/exec"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
)

const (
	networkConfigName     string = "network"
	DefaultNetworkAddress string = "10.41.0.0"
	DefaultNetworkMask    string = "255.255.0.0"
	DefaultNetworkProto   string = "static"
	DefaultIPv6Assign     string = "64"
	DefaultIPv6Class      string = "local"
	DefaultIPv6IfaceID    string = "eui64"

	DefaultULAPrefix string = "fd01:ed20:ecb4::/48"
)

// UCINetworkConfig represents the UCI network configuration.
type UCINetwork struct {
	Proto          string `uci:"option proto"`
	NetMask        string `uci:"option netmask"`
	IPAddr         string `uci:"option ipaddr"`
	Gateway        string `uci:"option gateway"`
	DNS            string `uci:"option dns"`
	Device         string `uci:"option device"`
	IPV6Assignment string `uci:"option ip6assign"`
	IPV6IfaceID    string `uci:"option ip6ifaceid"`
	IPV6Class      string `uci:"list ip6class"`
}

// ConfigReader defines an interface for reading UCI configuration values.
type ConfigReader interface {
	Get(config, section, option string) ([]string, bool)
	SetType(config, section, option string, typ uci.OptionType, values ...string) error
	Del(config, section, option string) error
	AddSection(config, section, typ string) error
	DelSection(config, section string) error
	Commit() error
	ReloadConfig() error
}

// UCINetworkConfigReader wraps the UCI functions for network configuration.
type UCINetworkConfigReader struct {
	tree uci.Tree
}

// NewUCINetworkConfigReader creates a new UCI network config reader with the default tree.
func NewUCINetworkConfigReader() *UCINetworkConfigReader {
	return &UCINetworkConfigReader{
		tree: uci.NewTree(uci.DefaultTreePath),
	}
}

func (r *UCINetworkConfigReader) Get(config, section, option string) ([]string, bool) {
	return r.tree.Get(config, section, option)
}

func (r *UCINetworkConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return r.tree.SetType(config, section, option, typ, values...)
}

func (r *UCINetworkConfigReader) Del(config, section, option string) error {
	return r.tree.Del(config, section, option)
}

func (r *UCINetworkConfigReader) AddSection(config, section, typ string) error {
	return r.tree.AddSection(config, section, typ)
}

func (r *UCINetworkConfigReader) DelSection(config, section string) error {
	return r.tree.DelSection(config, section)
}

func (r *UCINetworkConfigReader) Commit() error {
	return r.tree.Commit()
}

func (r *UCINetworkConfigReader) ReloadConfig() error {
	return r.tree.LoadConfig(networkConfigName, true)
}

// GetUCINetworkByName loads and returns the UCI network configuration by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "lan", "wan", "ahwlan")
//
// Returns the network configuration or an error if it cannot be read.
//
// Example:
//
//	config, err := GetUCINetworkByName("lan")
//	if err != nil {
//	    log.Fatalf("Failed to get network config: %v", err)
//	}
//	fmt.Printf("IP Address: %s\n", config.IPAddr)
func GetUCINetworkByName(name string) (*UCINetwork, error) {
	return GetUCINetworkByNameWithReader(name, NewUCINetworkConfigReader())
}

// GetUCINetworkByNameWithReader loads and returns the UCI network configuration by name using the provided reader.
func GetUCINetworkByNameWithReader(name string, reader ConfigReader) (*UCINetwork, error) {
	var config UCINetwork

	if values, ok := reader.Get(networkConfigName, name, "proto"); ok && len(values) > 0 {
		config.Proto = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "netmask"); ok && len(values) > 0 {
		config.NetMask = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "ipaddr"); ok && len(values) > 0 {
		config.IPAddr = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "gateway"); ok && len(values) > 0 {
		config.Gateway = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "dns"); ok && len(values) > 0 {
		config.DNS = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "device"); ok && len(values) > 0 {
		config.Device = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "ip6assign"); ok && len(values) > 0 {
		config.IPV6Assignment = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "ip6ifaceid"); ok && len(values) > 0 {
		config.IPV6IfaceID = values[0]
	}
	if values, ok := reader.Get(networkConfigName, name, "ip6class"); ok && len(values) > 0 {
		config.IPV6Class = values[0]
	}

	return &config, nil
}

// SetNetworkConfig creates or updates a network interface configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan", "ahwlan")
//   - config: The network configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	netConfig := &UCINetwork{
//	    Proto:   "static",
//	    IPAddr:  "192.168.1.1",
//	    NetMask: "255.255.255.0",
//	}
//	err := SetNetworkConfig("lan", netConfig)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetNetworkConfig(section string, config *UCINetwork) error {
	return SetNetworkConfigWithReader(section, config, NewUCINetworkConfigReader())
}

// SetNetworkConfigWithReader creates or updates a network interface configuration using the provided reader.
func SetNetworkConfigWithReader(section string, config *UCINetwork, reader ConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Add section if it doesn't exist (this will fail silently if it exists)
	_ = reader.AddSection(networkConfigName, section, "interface")

	if config.Proto != "" {
		if err := reader.SetType(networkConfigName, section, "proto", uci.TypeOption, config.Proto); err != nil {
			return fmt.Errorf("failed to set proto: %w", err)
		}
	}
	if config.NetMask != "" {
		if err := reader.SetType(networkConfigName, section, "netmask", uci.TypeOption, config.NetMask); err != nil {
			return fmt.Errorf("failed to set netmask: %w", err)
		}
	}
	if config.IPAddr != "" {
		if err := reader.SetType(networkConfigName, section, "ipaddr", uci.TypeOption, config.IPAddr); err != nil {
			return fmt.Errorf("failed to set ipaddr: %w", err)
		}
	}
	if config.Gateway != "" {
		if err := reader.SetType(networkConfigName, section, "gateway", uci.TypeOption, config.Gateway); err != nil {
			return fmt.Errorf("failed to set gateway: %w", err)
		}
	}
	if config.DNS != "" {
		if err := reader.SetType(networkConfigName, section, "dns", uci.TypeOption, config.DNS); err != nil {
			return fmt.Errorf("failed to set dns: %w", err)
		}
	}
	if config.Device != "" {
		if err := reader.SetType(networkConfigName, section, "device", uci.TypeOption, config.Device); err != nil {
			return fmt.Errorf("failed to set device: %w", err)
		}
	}
	if config.IPV6Assignment != "" {
		if err := reader.SetType(networkConfigName, section, "ip6assign", uci.TypeOption, config.IPV6Assignment); err != nil {
			return fmt.Errorf("failed to set ip6assign: %w", err)
		}
	}
	if config.IPV6IfaceID != "" {
		if err := reader.SetType(networkConfigName, section, "ip6ifaceid", uci.TypeOption, config.IPV6IfaceID); err != nil {
			return fmt.Errorf("failed to set ip6ifaceid: %w", err)
		}
	}
	if config.IPV6Class != "" {
		if err := reader.SetType(networkConfigName, section, "ip6class", uci.TypeList, config.IPV6Class); err != nil {
			return fmt.Errorf("failed to set ip6class: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// DeleteNetworkConfig removes a network interface configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "lan", "wan")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteNetworkConfig("guest")
//	if err != nil {
//	    log.Fatalf("Failed to delete network config: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteNetworkConfig(section string) error {
	return DeleteNetworkConfigWithReader(section, NewUCINetworkConfigReader())
}

// DeleteNetworkConfigWithReader removes a network interface configuration section using the provided reader.
func DeleteNetworkConfigWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(networkConfigName, section); err != nil {
		return fmt.Errorf("failed to delete network section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkProto sets the protocol for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - proto: The protocol (e.g., "static", "dhcp", "batadv")
//
// Example:
//
//	err := SetNetworkProto("wan", "dhcp")
func SetNetworkProto(section, proto string) error {
	return SetNetworkProtoWithReader(section, proto, NewUCINetworkConfigReader())
}

// SetNetworkProtoWithReader sets the protocol using the provided reader.
func SetNetworkProtoWithReader(section, proto string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "proto", uci.TypeOption, proto); err != nil {
		return fmt.Errorf("failed to set proto: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkIPAddr sets the IP address for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - ipaddr: The IP address (e.g., "192.168.1.1")
//
// Example:
//
//	err := SetNetworkIPAddr("lan", "192.168.1.1")
func SetNetworkIPAddr(section, ipaddr string) error {
	return SetNetworkIPAddrWithReader(section, ipaddr, NewUCINetworkConfigReader())
}

// SetNetworkIPAddrWithReader sets the IP address using the provided reader.
func SetNetworkIPAddrWithReader(section, ipaddr string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ipaddr", uci.TypeOption, ipaddr); err != nil {
		return fmt.Errorf("failed to set ipaddr: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkNetmask sets the netmask for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - netmask: The netmask (e.g., "255.255.255.0")
//
// Example:
//
//	err := SetNetworkNetmask("lan", "255.255.255.0")
func SetNetworkNetmask(section, netmask string) error {
	return SetNetworkNetmaskWithReader(section, netmask, NewUCINetworkConfigReader())
}

// SetNetworkNetmaskWithReader sets the netmask using the provided reader.
func SetNetworkNetmaskWithReader(section, netmask string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "netmask", uci.TypeOption, netmask); err != nil {
		return fmt.Errorf("failed to set netmask: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkGateway sets the gateway for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - gateway: The gateway IP address (e.g., "192.168.1.254")
//
// Example:
//
//	err := SetNetworkGateway("wan", "192.168.1.254")
func SetNetworkGateway(section, gateway string) error {
	return SetNetworkGatewayWithReader(section, gateway, NewUCINetworkConfigReader())
}

// SetNetworkGatewayWithReader sets the gateway using the provided reader.
func SetNetworkGatewayWithReader(section, gateway string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "gateway", uci.TypeOption, gateway); err != nil {
		return fmt.Errorf("failed to set gateway: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// DeleteNetworkGateway removes the gateway configuration for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//
// Returns an error if the gateway cannot be deleted.
//
// Example:
//
//	err := DeleteNetworkGateway("wan")
//	if err != nil {
//	    log.Fatalf("Failed to delete gateway: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteNetworkGateway(section string) error {
	return DeleteNetworkGatewayWithReader(section, NewUCINetworkConfigReader())
}

// DeleteNetworkGatewayWithReader removes the gateway configuration using the provided reader.
func DeleteNetworkGatewayWithReader(section string, reader ConfigReader) error {
	if err := reader.Del(networkConfigName, section, "gateway"); err != nil {
		return fmt.Errorf("failed to delete gateway: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkDNS sets the DNS server for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - dns: The DNS server IP address (e.g., "1.1.1.1")
//
// Example:
//
//	err := SetNetworkDNS("lan", "1.1.1.1")
func SetNetworkDNS(section, dns string) error {
	return SetNetworkDNSWithReader(section, dns, NewUCINetworkConfigReader())
}

// SetNetworkDNSWithReader sets the DNS server using the provided reader.
func SetNetworkDNSWithReader(section, dns string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "dns", uci.TypeOption, dns); err != nil {
		return fmt.Errorf("failed to set dns: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkDevice sets the device for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - device: The device name (e.g., "br-lan", "eth0")
//
// Example:
//
//	err := SetNetworkDevice("lan", "br-lan")
func SetNetworkDevice(section, device string) error {
	return SetNetworkDeviceWithReader(section, device, NewUCINetworkConfigReader())
}

// SetNetworkDeviceWithReader sets the device using the provided reader.
func SetNetworkDeviceWithReader(section, device string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "device", uci.TypeOption, device); err != nil {
		return fmt.Errorf("failed to set device: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkIPV6Assignment sets the IPv6 assignment (prefix length) for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - ip6assign: The IPv6 prefix length to assign (e.g., "60", "64")
//
// Example:
//
//	err := SetNetworkIPV6Assignment("lan", "60")
func SetNetworkIPV6Assignment(section, ip6assign string) error {
	return SetNetworkIPV6AssignmentWithReader(section, ip6assign, NewUCINetworkConfigReader())
}

// SetNetworkIPV6AssignmentWithReader sets the IPv6 assignment using the provided reader.
func SetNetworkIPV6AssignmentWithReader(section, ip6assign string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ip6assign", uci.TypeOption, ip6assign); err != nil {
		return fmt.Errorf("failed to set ip6assign: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkIPV6IfaceID sets the IPv6 interface ID for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - ip6ifaceid: The IPv6 interface ID (e.g., "::1")
//
// Example:
//
//	err := SetNetworkIPV6IfaceID("lan", "::1")
func SetNetworkIPV6IfaceID(section, ip6ifaceid string) error {
	return SetNetworkIPV6IfaceIDWithReader(section, ip6ifaceid, NewUCINetworkConfigReader())
}

// SetNetworkIPV6IfaceIDWithReader sets the IPv6 interface ID using the provided reader.
func SetNetworkIPV6IfaceIDWithReader(section, ip6ifaceid string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ip6ifaceid", uci.TypeOption, ip6ifaceid); err != nil {
		return fmt.Errorf("failed to set ip6ifaceid: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SetNetworkIPV6Class sets the IPv6 class for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - ip6class: The IPv6 class (e.g., "local", "wan6")
//
// Example:
//
//	err := SetNetworkIPV6Class("lan", "local")
func SetNetworkIPV6Class(section, ip6class string) error {
	return SetNetworkIPV6ClassWithReader(section, ip6class, NewUCINetworkConfigReader())
}

// SetNetworkIPV6ClassWithReader sets the IPv6 class using the provided reader.
func SetNetworkIPV6ClassWithReader(section, ip6class string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ip6class", uci.TypeList, ip6class); err != nil {
		return fmt.Errorf("failed to set ip6class: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network config: %w", err)
	}

	return nil
}

// SelectAvailableStaticIP selects an available static IP address from the 10.41.0.0/16 network.
//
// Parameters:
//   - records: Array of Alfred records containing address reservations
//   - gatewayMode: If true, selects from 10.41.0.0/24 range only. If false (default), selects from entire 10.41.0.0/16 range
//
// Returns:
//   - An available IP address from the specified range
//   - An error if no available IP can be found
//
// The function excludes:
//   - Already reserved IP addresses (from StaticIp field in AddressReservation)
//   - The 10.41.0.0/24 range (when gatewayMode is false)
//   - The 10.41.253.0/24 range (when gatewayMode is false)
//   - The 10.41.254.0/24 range (when gatewayMode is false)
//   - Network address (10.41.0.0)
//   - Broadcast address (10.41.255.255 or 10.41.0.255)
//
// Example:
//
//	records := []alfred.Record{ /* ... */ }
//	ip, err := SelectAvailableStaticIP(records, false)
//	if err != nil {
//	    log.Fatalf("Failed to select IP: %v", err)
//	}
//	fmt.Printf("Selected IP: %s\n", ip)
func SelectAvailableStaticIP(records []alfred.Record, gatewayMode bool) (string, error) {
	// Collect all reserved IP addresses
	reservedIPs := make(map[string]bool)

	for _, record := range records {
		var addrRes proto.AddressReservation
		if err := addrRes.UnmarshalVT(record.Data); err != nil {
			// Skip records that can't be unmarshaled
			continue
		}

		if addrRes.StaticIp != "" {
			reservedIPs[addrRes.StaticIp] = true
		}
	}

	// Define the base network: 10.41.0.0/16
	baseIP := net.ParseIP(DefaultNetworkAddress)
	if baseIP == nil {
		return "", fmt.Errorf("failed to parse base IP")
	}
	baseIP = baseIP.To4()

	if gatewayMode {
		// Gateway mode: only search in 10.41.0.0/24 range
		for fourthOctet := 1; fourthOctet < 255; fourthOctet++ {
			candidateIP := fmt.Sprintf("10.41.0.%d", fourthOctet)

			// Check if this IP is already reserved
			if !reservedIPs[candidateIP] {
				return candidateIP, nil
			}
		}
		return "", fmt.Errorf("no available IP addresses in 10.41.0.0/24 range")
	}

	// Normal mode: iterate through the 10.41.0.0/16 range
	// We have 256 * 256 = 65536 addresses total
	// Start from 10.41.1.1 (skip network address and 10.41.0.0/24)
	for thirdOctet := 1; thirdOctet < 256; thirdOctet++ {
		// Skip the restricted ranges: 10.41.253.0/24 and 10.41.254.0/24
		if thirdOctet == 253 || thirdOctet == 254 {
			continue
		}

		for fourthOctet := 1; fourthOctet < 255; fourthOctet++ {
			// Skip broadcast address within each /24 subnet
			if fourthOctet == 255 {
				continue
			}

			candidateIP := fmt.Sprintf("10.41.%d.%d", thirdOctet, fourthOctet)

			// Check if this IP is already reserved
			if !reservedIPs[candidateIP] {
				return candidateIP, nil
			}
		}
	}

	return "", fmt.Errorf("no available IP addresses in %s/16 range", DefaultNetworkAddress)
}

// ReloadNetwork reloads the network configuration by executing the OpenWrt network init script.
// It calls the '/etc/init.d/network reload' command to apply network configuration changes
// without restarting the entire network subsystem.
//
// Returns an error if the reload command fails to execute or returns a non-zero exit code.
func ReloadNetwork() error {
	cmd := exec.Command("/etc/init.d/network", "reload")
	return cmd.Run()
}

// RestartNetwork hard restarts the network service by executing the network init script.
// It runs the '/etc/init.d/network restart' command and returns an error if the
// command execution fails.
//
// Returns:
//   - error: nil if the network restart command succeeds, otherwise returns the error
//     from command execution
func RestartNetwork() error {
	cmd := exec.Command("/etc/init.d/network", "restart")
	return cmd.Run()
}
