package network

import (
	"fmt"
	"math/rand"
	"net"
	"os/exec"
	"time"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/openmanetd/internal/database/models"
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
	Master         string `uci:"option master"`
	IPV6Assignment string `uci:"option ip6assign"`
	IPV6IfaceID    string `uci:"option ip6ifaceid"`
	IPV6Class      string `uci:"list ip6class"`
}

// UCIDevice represents a UCI network device configuration (config device).
// Devices can be physical interfaces, bridges, VLANs, or other virtual devices.
type UCIDevice struct {
	Name         string   `uci:"option name"`         // Device name (required)
	Type         string   `uci:"option type"`         // Device type (e.g., bridge, vlan, macvlan, veth)
	MacAddr      string   `uci:"option macaddr"`      // MAC address override
	MTU          string   `uci:"option mtu"`          // Maximum transmission unit
	TxQueueLen   string   `uci:"option txqueuelen"`   // Transmit queue length
	Ports        []string `uci:"list ports"`          // Bridge member ports (for bridge type)
	Enabled      string   `uci:"option enabled"`      // Enable/disable device (0/1)
	Promisc      string   `uci:"option promisc"`      // Promiscuous mode (0/1)
	AcceptLocal  string   `uci:"option acceptlocal"`  // Accept packets with local source addresses (0/1)
	IGMPVersion  string   `uci:"option igmpversion"`  // IGMP version (2/3)
	MLDVersion   string   `uci:"option mldversion"`   // MLD version (1/2)
	Multicast    string   `uci:"option multicast"`    // Multicast support (0/1)
	IPV6         string   `uci:"option ipv6"`         // IPv6 support (0/1)
	RPS          string   `uci:"option rps"`          // Receive packet steering (0/1)
	XPS          string   `uci:"option xps"`          // Transmit packet steering (0/1)
	Dadtransmits string   `uci:"option dadtransmits"` // DAD transmits count
	Multicast_to_unicast string `uci:"option multicast_to_unicast"` // Convert multicast to unicast (0/1)
	SendRedirects string `uci:"option sendredirects"` // Send ICMP redirects (0/1)
	Drop_v4_unicast_in_l2_multicast string `uci:"option drop_v4_unicast_in_l2_multicast"` // Drop IPv4 unicast in L2 multicast (0/1)
	Drop_v6_unicast_in_l2_multicast string `uci:"option drop_v6_unicast_in_l2_multicast"` // Drop IPv6 unicast in L2 multicast (0/1)
	Drop_gratuitous_arp string `uci:"option drop_gratuitous_arp"` // Drop gratuitous ARP (0/1)
	Drop_unsolicited_na string `uci:"option drop_unsolicited_na"` // Drop unsolicited neighbor advertisements (0/1)
	ARP_accept   string   `uci:"option arp_accept"`   // Accept gratuitous ARP (0/1)
}

// ConfigReader defines an interface for reading UCI configuration values.
type ConfigReader interface {
	Get(config, section, option string) ([]string, bool)
	GetSections(config, secType string) ([]string, error)
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

func (r *UCINetworkConfigReader) GetSections(config, secType string) ([]string, error) {
	return r.tree.GetSections(config, secType)
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
	if values, ok := reader.Get(networkConfigName, name, "master"); ok && len(values) > 0 {
		config.Master = values[0]
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
	if config.Master != "" {
		if err := reader.SetType(networkConfigName, section, "master", uci.TypeOption, config.Master); err != nil {
			return fmt.Errorf("failed to set master: %w", err)
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

// NetworkSectionExists checks if a network section exists in the configuration.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "lan", "wan", "ahwlan")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := NetworkSectionExists("lan")
//	if exists {
//	    fmt.Println("LAN section exists")
//	}
func NetworkSectionExists(section string) bool {
	return NetworkSectionExistsWithReader(section, NewUCINetworkConfigReader())
}

// NetworkSectionExistsWithReader checks if a network section exists using the provided reader.
func NetworkSectionExistsWithReader(section string, reader ConfigReader) bool {
	// Try to get any option from the section to verify it exists
	// We check for 'proto' as it's a common option in network sections
	_, exists := reader.Get(networkConfigName, section, "proto")
	return exists
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

// SetNetworkMaster sets the master interface for a network interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//   - master: The master interface name (e.g., "br-lan")
//
// Example:
//
//	err := SetNetworkMaster("eth0", "br-lan")
func SetNetworkMaster(section, master string) error {
	return SetNetworkMasterWithReader(section, master, NewUCINetworkConfigReader())
}

// SetNetworkMasterWithReader sets the master interface using the provided reader.
func SetNetworkMasterWithReader(section, master string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "master", uci.TypeOption, master); err != nil {
		return fmt.Errorf("failed to set network master: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit network master: %w", err)
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

// SelectAvailableStaticIPFromNodeData selects an available static IP address from the 10.41.0.0/16 network.
//
// Parameters:
//   - nodes: Array of MeshNode records containing address reservations
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
//	nodes := []models.MeshNode{ /* ... */ }
//	ip, err := SelectAvailableStaticIPFromNodeData(nodes, false)
//	if err != nil {
//	    log.Fatalf("Failed to select IP: %v", err)
//	}
//	fmt.Printf("Selected IP: %s\n", ip)
func SelectAvailableStaticIPFromNodeData(nodes []models.MeshNode, gatewayMode bool) (string, error) {
	// Collect all reserved IP addresses
	reservedIPs := make(map[string]bool)

	for _, node := range nodes {
		if node.IpAddr != "" {
			reservedIPs[node.IpAddr] = true
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
			if reservedIPs[candidateIP] {
				// IP is reserved, continue to next candidate
				continue
			}

			// IP is available, return it
			return candidateIP, nil
		}
		return "", fmt.Errorf("no available IP addresses in 10.41.0.0/24 range")
	}

	// Normal mode: If there are 1 or fewer nodes, select a random IP to avoid conflicts
	// when multiple nodes start simultaneously
	if len(nodes) <= 1 {
		// Initialize random seed
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		// Try to find a random available IP (max 1000 attempts to avoid infinite loop)
		for attempt := 0; attempt < 1000; attempt++ {
			// Generate random third octet (1-252, excluding 0, 253, 254, 255)
			thirdOctet := rng.Intn(252) + 1 // Generates 1-252
			if thirdOctet == 253 {
				thirdOctet = 252 // Avoid 253
			}

			// Generate random fourth octet (1-254)
			fourthOctet := rng.Intn(254) + 1 // Generates 1-254

			candidateIP := fmt.Sprintf("10.41.%d.%d", thirdOctet, fourthOctet)

			// Check if this IP is already reserved
			if !reservedIPs[candidateIP] {
				// IP is available, return it
				return candidateIP, nil
			}
		}
		// If random selection didn't find an IP, fall through to sequential search
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
			if reservedIPs[candidateIP] {
				// IP is reserved, continue to next candidate
				continue
			}

			// IP is available, return it
			return candidateIP, nil
		}
	}

	return "", fmt.Errorf("no available IP addresses in %s/16 range", DefaultNetworkAddress)
}

// GetDeviceByName loads and returns the UCI device configuration by name.
//
// Parameters:
//   - name: The device name (e.g., "br-ahwlan", "vxlan0", "tailscale0")
//
// Returns the device configuration or an error if it cannot be read.
//
// Example:
//
//	device, err := GetDeviceByName("br-ahwlan")
//	if err != nil {
//	    log.Fatalf("Failed to get device config: %v", err)
//	}
//	fmt.Printf("Device type: %s\n", device.Type)
func GetDeviceByName(name string) (*UCIDevice, error) {
	reader := NewUCINetworkConfigReader()
	return GetDeviceByNameWithReader(name, reader)
}

// GetDeviceByNameWithReader loads and returns the UCI device configuration by name using the provided reader.
func GetDeviceByNameWithReader(name string, reader ConfigReader) (*UCIDevice, error) {
	device := &UCIDevice{}

	if values, ok := reader.Get(networkConfigName, name, "name"); ok && len(values) > 0 {
		device.Name = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "type"); ok && len(values) > 0 {
		device.Type = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "macaddr"); ok && len(values) > 0 {
		device.MacAddr = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "mtu"); ok && len(values) > 0 {
		device.MTU = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "txqueuelen"); ok && len(values) > 0 {
		device.TxQueueLen = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "ports"); ok && len(values) > 0 {
		device.Ports = values
	}

	if values, ok := reader.Get(networkConfigName, name, "enabled"); ok && len(values) > 0 {
		device.Enabled = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "promisc"); ok && len(values) > 0 {
		device.Promisc = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "acceptlocal"); ok && len(values) > 0 {
		device.AcceptLocal = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "igmpversion"); ok && len(values) > 0 {
		device.IGMPVersion = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "mldversion"); ok && len(values) > 0 {
		device.MLDVersion = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "multicast"); ok && len(values) > 0 {
		device.Multicast = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "ipv6"); ok && len(values) > 0 {
		device.IPV6 = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "rps"); ok && len(values) > 0 {
		device.RPS = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "xps"); ok && len(values) > 0 {
		device.XPS = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "dadtransmits"); ok && len(values) > 0 {
		device.Dadtransmits = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "multicast_to_unicast"); ok && len(values) > 0 {
		device.Multicast_to_unicast = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "sendredirects"); ok && len(values) > 0 {
		device.SendRedirects = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "drop_v4_unicast_in_l2_multicast"); ok && len(values) > 0 {
		device.Drop_v4_unicast_in_l2_multicast = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "drop_v6_unicast_in_l2_multicast"); ok && len(values) > 0 {
		device.Drop_v6_unicast_in_l2_multicast = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "drop_gratuitous_arp"); ok && len(values) > 0 {
		device.Drop_gratuitous_arp = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "drop_unsolicited_na"); ok && len(values) > 0 {
		device.Drop_unsolicited_na = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "arp_accept"); ok && len(values) > 0 {
		device.ARP_accept = values[0]
	}

	return device, nil
}

// SetDeviceConfig creates or updates a device configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "br-ahwlan", "vxlan0")
//   - device: The device configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	device := &UCIDevice{
//	    Name: "br-ahwlan",
//	    Type: "bridge",
//	    Ports: []string{"bat0"},
//	}
//	err := SetDeviceConfig("br-ahwlan", device)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetDeviceConfig(section string, device *UCIDevice) error {
	reader := NewUCINetworkConfigReader()
	return SetDeviceConfigWithReader(section, device, reader)
}

// SetDeviceConfigWithReader creates or updates a device configuration using the provided reader.
func SetDeviceConfigWithReader(section string, device *UCIDevice, reader ConfigReader) error {
	// Add the section if it doesn't exist
	if err := reader.AddSection(networkConfigName, section, "device"); err != nil {
		// Section might already exist, which is fine
	}

	if device.Name != "" {
		if err := reader.SetType(networkConfigName, section, "name", uci.TypeOption, device.Name); err != nil {
			return fmt.Errorf("failed to set device name: %w", err)
		}
	}

	if device.Type != "" {
		if err := reader.SetType(networkConfigName, section, "type", uci.TypeOption, device.Type); err != nil {
			return fmt.Errorf("failed to set device type: %w", err)
		}
	}

	if device.MacAddr != "" {
		if err := reader.SetType(networkConfigName, section, "macaddr", uci.TypeOption, device.MacAddr); err != nil {
			return fmt.Errorf("failed to set device macaddr: %w", err)
		}
	}

	if device.MTU != "" {
		if err := reader.SetType(networkConfigName, section, "mtu", uci.TypeOption, device.MTU); err != nil {
			return fmt.Errorf("failed to set device mtu: %w", err)
		}
	}

	if device.TxQueueLen != "" {
		if err := reader.SetType(networkConfigName, section, "txqueuelen", uci.TypeOption, device.TxQueueLen); err != nil {
			return fmt.Errorf("failed to set device txqueuelen: %w", err)
		}
	}

	if len(device.Ports) > 0 {
		if err := reader.SetType(networkConfigName, section, "ports", uci.TypeList, device.Ports...); err != nil {
			return fmt.Errorf("failed to set device ports: %w", err)
		}
	}

	if device.Enabled != "" {
		if err := reader.SetType(networkConfigName, section, "enabled", uci.TypeOption, device.Enabled); err != nil {
			return fmt.Errorf("failed to set device enabled: %w", err)
		}
	}

	if device.Promisc != "" {
		if err := reader.SetType(networkConfigName, section, "promisc", uci.TypeOption, device.Promisc); err != nil {
			return fmt.Errorf("failed to set device promisc: %w", err)
		}
	}

	if device.AcceptLocal != "" {
		if err := reader.SetType(networkConfigName, section, "acceptlocal", uci.TypeOption, device.AcceptLocal); err != nil {
			return fmt.Errorf("failed to set device acceptlocal: %w", err)
		}
	}

	if device.IGMPVersion != "" {
		if err := reader.SetType(networkConfigName, section, "igmpversion", uci.TypeOption, device.IGMPVersion); err != nil {
			return fmt.Errorf("failed to set device igmpversion: %w", err)
		}
	}

	if device.MLDVersion != "" {
		if err := reader.SetType(networkConfigName, section, "mldversion", uci.TypeOption, device.MLDVersion); err != nil {
			return fmt.Errorf("failed to set device mldversion: %w", err)
		}
	}

	if device.Multicast != "" {
		if err := reader.SetType(networkConfigName, section, "multicast", uci.TypeOption, device.Multicast); err != nil {
			return fmt.Errorf("failed to set device multicast: %w", err)
		}
	}

	if device.IPV6 != "" {
		if err := reader.SetType(networkConfigName, section, "ipv6", uci.TypeOption, device.IPV6); err != nil {
			return fmt.Errorf("failed to set device ipv6: %w", err)
		}
	}

	if device.RPS != "" {
		if err := reader.SetType(networkConfigName, section, "rps", uci.TypeOption, device.RPS); err != nil {
			return fmt.Errorf("failed to set device rps: %w", err)
		}
	}

	if device.XPS != "" {
		if err := reader.SetType(networkConfigName, section, "xps", uci.TypeOption, device.XPS); err != nil {
			return fmt.Errorf("failed to set device xps: %w", err)
		}
	}

	if device.Dadtransmits != "" {
		if err := reader.SetType(networkConfigName, section, "dadtransmits", uci.TypeOption, device.Dadtransmits); err != nil {
			return fmt.Errorf("failed to set device dadtransmits: %w", err)
		}
	}

	if device.Multicast_to_unicast != "" {
		if err := reader.SetType(networkConfigName, section, "multicast_to_unicast", uci.TypeOption, device.Multicast_to_unicast); err != nil {
			return fmt.Errorf("failed to set device multicast_to_unicast: %w", err)
		}
	}

	if device.SendRedirects != "" {
		if err := reader.SetType(networkConfigName, section, "sendredirects", uci.TypeOption, device.SendRedirects); err != nil {
			return fmt.Errorf("failed to set device sendredirects: %w", err)
		}
	}

	if device.Drop_v4_unicast_in_l2_multicast != "" {
		if err := reader.SetType(networkConfigName, section, "drop_v4_unicast_in_l2_multicast", uci.TypeOption, device.Drop_v4_unicast_in_l2_multicast); err != nil {
			return fmt.Errorf("failed to set device drop_v4_unicast_in_l2_multicast: %w", err)
		}
	}

	if device.Drop_v6_unicast_in_l2_multicast != "" {
		if err := reader.SetType(networkConfigName, section, "drop_v6_unicast_in_l2_multicast", uci.TypeOption, device.Drop_v6_unicast_in_l2_multicast); err != nil {
			return fmt.Errorf("failed to set device drop_v6_unicast_in_l2_multicast: %w", err)
		}
	}

	if device.Drop_gratuitous_arp != "" {
		if err := reader.SetType(networkConfigName, section, "drop_gratuitous_arp", uci.TypeOption, device.Drop_gratuitous_arp); err != nil {
			return fmt.Errorf("failed to set device drop_gratuitous_arp: %w", err)
		}
	}

	if device.Drop_unsolicited_na != "" {
		if err := reader.SetType(networkConfigName, section, "drop_unsolicited_na", uci.TypeOption, device.Drop_unsolicited_na); err != nil {
			return fmt.Errorf("failed to set device drop_unsolicited_na: %w", err)
		}
	}

	if device.ARP_accept != "" {
		if err := reader.SetType(networkConfigName, section, "arp_accept", uci.TypeOption, device.ARP_accept); err != nil {
			return fmt.Errorf("failed to set device arp_accept: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit device configuration: %w", err)
	}

	return nil
}

// DeleteDeviceConfig removes a device configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "br-ahwlan", "vxlan0")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteDeviceConfig("br-guest")
//	if err != nil {
//	    log.Fatalf("Failed to delete device config: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteDeviceConfig(section string) error {
	reader := NewUCINetworkConfigReader()
	return DeleteDeviceConfigWithReader(section, reader)
}

// DeleteDeviceConfigWithReader removes a device configuration section using the provided reader.
func DeleteDeviceConfigWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(networkConfigName, section); err != nil {
		return fmt.Errorf("failed to delete device section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit device deletion: %w", err)
	}

	return nil
}

// DeviceSectionExists checks if a device section exists in the configuration.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "br-ahwlan", "vxlan0")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := DeviceSectionExists("br-ahwlan")
//	if exists {
//	    fmt.Println("Device section exists")
//	}
func DeviceSectionExists(section string) bool {
	reader := NewUCINetworkConfigReader()
	return DeviceSectionExistsWithReader(section, reader)
}

// DeviceSectionExistsWithReader checks if a device section exists using the provided reader.
func DeviceSectionExistsWithReader(section string, reader ConfigReader) bool {
	// Try to get the name option as a check for existence
	_, exists := reader.Get(networkConfigName, section, "name")
	return exists
}

// GetAllDevices retrieves all device configurations from the network config.
// It returns a map of section names to UCIDevice configurations.
func GetAllDevices() (map[string]*UCIDevice, error) {
	reader := NewUCINetworkConfigReader()
	return GetAllDevicesWithReader(reader)
}

// GetAllDevicesWithReader retrieves all device configurations using the provided reader.
// It returns a map of section names to UCIDevice configurations.
func GetAllDevicesWithReader(reader ConfigReader) (map[string]*UCIDevice, error) {
	// Get all sections of type "device"
	sections, err := reader.GetSections(networkConfigName, "device")
	if err != nil {
		return nil, fmt.Errorf("failed to get device sections: %w", err)
	}

	// Create map to hold devices
	devices := make(map[string]*UCIDevice, len(sections))

	// Load each device
	for _, section := range sections {
		device, err := GetDeviceByNameWithReader(section, reader)
		if err != nil {
			// Log but continue on error for individual device
			continue
		}
		devices[section] = device
	}

	return devices, nil
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
