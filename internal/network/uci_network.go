package network

import (
	"fmt"

	"github.com/digineo/go-uci/v2"
)

const (
	networkConfigName string = "network"
)

// UCINetworkConfig represents the UCI network configuration.
type UCINetwork struct {
	Proto   string `uci:"option proto"`
	NetMask string `uci:"option netmask"`
	IPAddr  string `uci:"option ipaddr"`
	Gateway string `uci:"option gateway"`
	DNS     string `uci:"option dns"`
	Device  string `uci:"option device"`
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

// UCIConfigReader wraps the UCI functions for network configuration.
type UCIConfigReader struct {
	tree uci.Tree
}

// NewUCIConfigReader creates a new UCI network config reader with the default tree.
func NewUCIConfigReader() *UCIConfigReader {
	return &UCIConfigReader{
		tree: uci.NewTree(uci.DefaultTreePath),
	}
}

func (r *UCIConfigReader) Get(config, section, option string) ([]string, bool) {
	return r.tree.Get(config, section, option)
}

func (r *UCIConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return r.tree.SetType(config, section, option, typ, values...)
}

func (r *UCIConfigReader) Del(config, section, option string) error {
	return r.tree.Del(config, section, option)
}

func (r *UCIConfigReader) AddSection(config, section, typ string) error {
	return r.tree.AddSection(config, section, typ)
}

func (r *UCIConfigReader) DelSection(config, section string) error {
	return r.tree.DelSection(config, section)
}

func (r *UCIConfigReader) Commit() error {
	return r.tree.Commit()
}

func (r *UCIConfigReader) ReloadConfig() error {
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
	return GetUCINetworkByNameWithReader(name, NewUCIConfigReader())
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
	return SetNetworkConfigWithReader(section, config, NewUCIConfigReader())
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
	return DeleteNetworkConfigWithReader(section, NewUCIConfigReader())
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
	return SetNetworkProtoWithReader(section, proto, NewUCIConfigReader())
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
	return SetNetworkIPAddrWithReader(section, ipaddr, NewUCIConfigReader())
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
	return SetNetworkNetmaskWithReader(section, netmask, NewUCIConfigReader())
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
	return SetNetworkGatewayWithReader(section, gateway, NewUCIConfigReader())
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
	return SetNetworkDNSWithReader(section, dns, NewUCIConfigReader())
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
	return SetNetworkDeviceWithReader(section, device, NewUCIConfigReader())
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
