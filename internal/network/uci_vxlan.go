package network

import (
	"fmt"

	"github.com/digineo/go-uci/v2"
)

// UCIVXLANConfig represents the UCI VXLAN interface configuration.
// VXLAN (Virtual Extensible LAN) provides Layer 2 virtualization over Layer 3 networks.
type UCIVXLANConfig struct {
	Proto    string `uci:"option proto"`    // Protocol must be "vxlan"
	Tunlink  string `uci:"option tunlink"`  // Tunnel link device (optional)
	PeerAddr string `uci:"option peeraddr"` // Peer IP address for point-to-point VXLAN
	VID      string `uci:"option vid"`      // VXLAN Network Identifier (VNI) - 24-bit number
	Port     string `uci:"option port"`     // UDP destination port (default: 4789)
	MacAddr  string `uci:"option macaddr"`  // MAC address for the VXLAN interface (optional)
	RxCsum   string `uci:"option rxcsum"`   // Enable RX checksum offload (0/1)
	TxCsum   string `uci:"option txcsum"`   // Enable TX checksum offload (0/1)
	MTU      string `uci:"option mtu"`      // MTU size (optional)
	TTL      string `uci:"option ttl"`      // Time to live for packets (optional)
	TOS      string `uci:"option tos"`      // Type of service for packets (optional)
}

// UCIVXLANPeer represents a VXLAN peer configuration.
type UCIVXLANPeer struct {
	VNI     string `uci:"option vni"`     // VXLAN Network Identifier
	Remote  string `uci:"option remote"`  // Remote peer IP address
	MacAddr string `uci:"option macaddr"` // MAC address of the peer (optional)
}

const (
	DefaultVXLANPort   string = "4789"
	DefaultVXLANProto  string = "vxlan"
	DefaultVXLANRxCsum string = "1"
	DefaultVXLANTxCsum string = "1"
)

// GetVXLANByName loads and returns the UCI VXLAN interface configuration by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "vxlan0", "vxlan1")
//
// Returns the VXLAN configuration or an error if it cannot be read.
//
// Example:
//
//	config, err := GetVXLANByName("vxlan0")
//	if err != nil {
//	    log.Fatalf("Failed to get VXLAN config: %v", err)
//	}
//	fmt.Printf("VXLAN VNI: %s\n", config.VID)
func GetVXLANByName(name string) (*UCIVXLANConfig, error) {
	reader := NewUCINetworkConfigReader()
	return GetVXLANByNameWithReader(name, reader)
}

// GetVXLANByNameWithReader loads and returns the UCI VXLAN interface configuration by name using the provided reader.
func GetVXLANByNameWithReader(name string, reader ConfigReader) (*UCIVXLANConfig, error) {
	config := &UCIVXLANConfig{}

	if values, ok := reader.Get(networkConfigName, name, "proto"); ok && len(values) > 0 {
		config.Proto = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "tunlink"); ok && len(values) > 0 {
		config.Tunlink = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "peeraddr"); ok && len(values) > 0 {
		config.PeerAddr = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "vid"); ok && len(values) > 0 {
		config.VID = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "port"); ok && len(values) > 0 {
		config.Port = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "macaddr"); ok && len(values) > 0 {
		config.MacAddr = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "rxcsum"); ok && len(values) > 0 {
		config.RxCsum = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "txcsum"); ok && len(values) > 0 {
		config.TxCsum = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "mtu"); ok && len(values) > 0 {
		config.MTU = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "ttl"); ok && len(values) > 0 {
		config.TTL = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "tos"); ok && len(values) > 0 {
		config.TOS = values[0]
	}

	return config, nil
}

// SetVXLANConfig creates or updates a VXLAN interface configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - config: The VXLAN configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	vxlanConfig := &UCIVXLANConfig{
//	    Proto:    "vxlan",
//	    PeerAddr: "192.168.1.100",
//	    VID:      "100",
//	    Port:     "4789",
//	}
//	err := SetVXLANConfig("vxlan0", vxlanConfig)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetVXLANConfig(section string, config *UCIVXLANConfig) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANConfigWithReader(section, config, reader)
}

// SetVXLANConfigWithReader creates or updates a VXLAN interface configuration using the provided reader.
func SetVXLANConfigWithReader(section string, config *UCIVXLANConfig, reader ConfigReader) error {
	// Check if the section exists; if not, create it
	if !NetworkSectionExistsWithReader(section, reader) {
		if err := reader.AddSection(networkConfigName, section, "interface"); err != nil {
			return fmt.Errorf("failed to add VXLAN section %s: %w", section, err)
		}
	}

	// Set protocol (required)
	if config.Proto != "" {
		if err := reader.SetType(networkConfigName, section, "proto", uci.TypeOption, config.Proto); err != nil {
			return fmt.Errorf("failed to set VXLAN proto: %w", err)
		}
	}

	// Set tunlink (optional)
	if config.Tunlink != "" {
		if err := reader.SetType(networkConfigName, section, "tunlink", uci.TypeOption, config.Tunlink); err != nil {
			return fmt.Errorf("failed to set VXLAN tunlink: %w", err)
		}
	}

	// Set peeraddr (optional for multicast, required for point-to-point)
	if config.PeerAddr != "" {
		if err := reader.SetType(networkConfigName, section, "peeraddr", uci.TypeOption, config.PeerAddr); err != nil {
			return fmt.Errorf("failed to set VXLAN peeraddr: %w", err)
		}
	}

	// Set VID/VNI (required)
	if config.VID != "" {
		if err := reader.SetType(networkConfigName, section, "vid", uci.TypeOption, config.VID); err != nil {
			return fmt.Errorf("failed to set VXLAN vid: %w", err)
		}
	}

	// Set port (optional, defaults to 4789)
	if config.Port != "" {
		if err := reader.SetType(networkConfigName, section, "port", uci.TypeOption, config.Port); err != nil {
			return fmt.Errorf("failed to set VXLAN port: %w", err)
		}
	}

	// Set macaddr (optional)
	if config.MacAddr != "" {
		if err := reader.SetType(networkConfigName, section, "macaddr", uci.TypeOption, config.MacAddr); err != nil {
			return fmt.Errorf("failed to set VXLAN macaddr: %w", err)
		}
	}

	// Set rxcsum (optional)
	if config.RxCsum != "" {
		if err := reader.SetType(networkConfigName, section, "rxcsum", uci.TypeOption, config.RxCsum); err != nil {
			return fmt.Errorf("failed to set VXLAN rxcsum: %w", err)
		}
	}

	// Set txcsum (optional)
	if config.TxCsum != "" {
		if err := reader.SetType(networkConfigName, section, "txcsum", uci.TypeOption, config.TxCsum); err != nil {
			return fmt.Errorf("failed to set VXLAN txcsum: %w", err)
		}
	}

	// Set mtu (optional)
	if config.MTU != "" {
		if err := reader.SetType(networkConfigName, section, "mtu", uci.TypeOption, config.MTU); err != nil {
			return fmt.Errorf("failed to set VXLAN mtu: %w", err)
		}
	}

	// Set ttl (optional)
	if config.TTL != "" {
		if err := reader.SetType(networkConfigName, section, "ttl", uci.TypeOption, config.TTL); err != nil {
			return fmt.Errorf("failed to set VXLAN ttl: %w", err)
		}
	}

	// Set tos (optional)
	if config.TOS != "" {
		if err := reader.SetType(networkConfigName, section, "tos", uci.TypeOption, config.TOS); err != nil {
			return fmt.Errorf("failed to set VXLAN tos: %w", err)
		}
	}

	// Commit changes
	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN config: %w", err)
	}

	return nil
}

// DeleteVXLANConfig removes a VXLAN interface configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "vxlan0", "vxlan1")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteVXLANConfig("vxlan0")
//	if err != nil {
//	    log.Fatalf("Failed to delete VXLAN config: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteVXLANConfig(section string) error {
	reader := NewUCINetworkConfigReader()
	return DeleteVXLANConfigWithReader(section, reader)
}

// DeleteVXLANConfigWithReader removes a VXLAN interface configuration section using the provided reader.
func DeleteVXLANConfigWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(networkConfigName, section); err != nil {
		return fmt.Errorf("failed to delete VXLAN section %s: %w", section, err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN deletion: %w", err)
	}

	return nil
}

// VXLANSectionExists checks if a VXLAN section exists in the configuration.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "vxlan0", "vxlan1")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := VXLANSectionExists("vxlan0")
//	if exists {
//	    fmt.Println("VXLAN section exists")
//	}
func VXLANSectionExists(section string) bool {
	reader := NewUCINetworkConfigReader()
	return VXLANSectionExistsWithReader(section, reader)
}

// VXLANSectionExistsWithReader checks if a VXLAN section exists using the provided reader.
func VXLANSectionExistsWithReader(section string, reader ConfigReader) bool {
	return NetworkSectionExistsWithReader(section, reader)
}

// SetVXLANProto sets the protocol for a VXLAN interface to "vxlan".
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//
// Example:
//
//	err := SetVXLANProto("vxlan0")
func SetVXLANProto(section string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANProtoWithReader(section, reader)
}

// SetVXLANProtoWithReader sets the protocol using the provided reader.
func SetVXLANProtoWithReader(section string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "proto", uci.TypeOption, DefaultVXLANProto); err != nil {
		return fmt.Errorf("failed to set VXLAN proto: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN proto: %w", err)
	}

	return nil
}

// SetVXLANTunlink sets the tunnel link device for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - tunlink: The tunnel link device (e.g., "eth0", "br-lan")
//
// Example:
//
//	err := SetVXLANTunlink("vxlan0", "eth0")
func SetVXLANTunlink(section string, tunlink string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANTunlinkWithReader(section, tunlink, reader)
}

// SetVXLANTunlinkWithReader sets the tunnel link using the provided reader.
func SetVXLANTunlinkWithReader(section string, tunlink string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "tunlink", uci.TypeOption, tunlink); err != nil {
		return fmt.Errorf("failed to set VXLAN tunlink: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN tunlink: %w", err)
	}

	return nil
}

// SetVXLANPeerAddr sets the peer address for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - peeraddr: The peer IP address (e.g., "192.168.1.100")
//
// Example:
//
//	err := SetVXLANPeerAddr("vxlan0", "192.168.1.100")
func SetVXLANPeerAddr(section string, peeraddr string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANPeerAddrWithReader(section, peeraddr, reader)
}

// SetVXLANPeerAddrWithReader sets the peer address using the provided reader.
func SetVXLANPeerAddrWithReader(section string, peeraddr string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "peeraddr", uci.TypeOption, peeraddr); err != nil {
		return fmt.Errorf("failed to set VXLAN peeraddr: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN peeraddr: %w", err)
	}

	return nil
}

// SetVXLANVID sets the VXLAN Network Identifier (VNI) for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - vid: The VXLAN Network Identifier (24-bit number, e.g., "100")
//
// Example:
//
//	err := SetVXLANVID("vxlan0", "100")
func SetVXLANVID(section string, vid string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANVIDWithReader(section, vid, reader)
}

// SetVXLANVIDWithReader sets the VID using the provided reader.
func SetVXLANVIDWithReader(section string, vid string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "vid", uci.TypeOption, vid); err != nil {
		return fmt.Errorf("failed to set VXLAN vid: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN vid: %w", err)
	}

	return nil
}

// SetVXLANPort sets the UDP destination port for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - port: The UDP port (e.g., "4789")
//
// Example:
//
//	err := SetVXLANPort("vxlan0", "4789")
func SetVXLANPort(section string, port string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANPortWithReader(section, port, reader)
}

// SetVXLANPortWithReader sets the port using the provided reader.
func SetVXLANPortWithReader(section string, port string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "port", uci.TypeOption, port); err != nil {
		return fmt.Errorf("failed to set VXLAN port: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN port: %w", err)
	}

	return nil
}

// SetVXLANMacAddr sets the MAC address for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - macaddr: The MAC address (e.g., "00:11:22:33:44:55")
//
// Example:
//
//	err := SetVXLANMacAddr("vxlan0", "00:11:22:33:44:55")
func SetVXLANMacAddr(section string, macaddr string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANMacAddrWithReader(section, macaddr, reader)
}

// SetVXLANMacAddrWithReader sets the MAC address using the provided reader.
func SetVXLANMacAddrWithReader(section string, macaddr string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "macaddr", uci.TypeOption, macaddr); err != nil {
		return fmt.Errorf("failed to set VXLAN macaddr: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN macaddr: %w", err)
	}

	return nil
}

// SetVXLANRxCsum sets the RX checksum offload for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - rxcsum: Enable RX checksum offload ("0" or "1")
//
// Example:
//
//	err := SetVXLANRxCsum("vxlan0", "1")
func SetVXLANRxCsum(section string, rxcsum string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANRxCsumWithReader(section, rxcsum, reader)
}

// SetVXLANRxCsumWithReader sets the RX checksum using the provided reader.
func SetVXLANRxCsumWithReader(section string, rxcsum string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "rxcsum", uci.TypeOption, rxcsum); err != nil {
		return fmt.Errorf("failed to set VXLAN rxcsum: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN rxcsum: %w", err)
	}

	return nil
}

// SetVXLANTxCsum sets the TX checksum offload for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - txcsum: Enable TX checksum offload ("0" or "1")
//
// Example:
//
//	err := SetVXLANTxCsum("vxlan0", "1")
func SetVXLANTxCsum(section string, txcsum string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANTxCsumWithReader(section, txcsum, reader)
}

// SetVXLANTxCsumWithReader sets the TX checksum using the provided reader.
func SetVXLANTxCsumWithReader(section string, txcsum string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "txcsum", uci.TypeOption, txcsum); err != nil {
		return fmt.Errorf("failed to set VXLAN txcsum: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN txcsum: %w", err)
	}

	return nil
}

// SetVXLANMTU sets the MTU size for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - mtu: The MTU size (e.g., "1450")
//
// Example:
//
//	err := SetVXLANMTU("vxlan0", "1450")
func SetVXLANMTU(section string, mtu string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANMTUWithReader(section, mtu, reader)
}

// SetVXLANMTUWithReader sets the MTU using the provided reader.
func SetVXLANMTUWithReader(section string, mtu string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "mtu", uci.TypeOption, mtu); err != nil {
		return fmt.Errorf("failed to set VXLAN mtu: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN mtu: %w", err)
	}

	return nil
}

// SetVXLANTTL sets the time to live for packets on a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - ttl: The TTL value (e.g., "64")
//
// Example:
//
//	err := SetVXLANTTL("vxlan0", "64")
func SetVXLANTTL(section string, ttl string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANTTLWithReader(section, ttl, reader)
}

// SetVXLANTTLWithReader sets the TTL using the provided reader.
func SetVXLANTTLWithReader(section string, ttl string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ttl", uci.TypeOption, ttl); err != nil {
		return fmt.Errorf("failed to set VXLAN ttl: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN ttl: %w", err)
	}

	return nil
}

// SetVXLANTOS sets the type of service for packets on a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - tos: The TOS value (e.g., "inherit")
//
// Example:
//
//	err := SetVXLANTOS("vxlan0", "inherit")
func SetVXLANTOS(section string, tos string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANTOSWithReader(section, tos, reader)
}

// SetVXLANTOSWithReader sets the TOS using the provided reader.
func SetVXLANTOSWithReader(section string, tos string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "tos", uci.TypeOption, tos); err != nil {
		return fmt.Errorf("failed to set VXLAN tos: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN tos: %w", err)
	}

	return nil
}
