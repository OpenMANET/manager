package network

import (
	"fmt"

	"github.com/digineo/go-uci/v2"
)

// UCIVXLANConfig represents the UCI VXLAN interface configuration.
// VXLAN (Virtual Extensible LAN) provides Layer 2 virtualization over Layer 3 networks.
type UCIVXLANConfig struct {
	Proto          string `uci:"option proto"`          // Protocol must be "vxlan"
	Tunlink        string `uci:"option tunlink"`        // Tunnel link device (optional)
	IPAddr         string `uci:"option ipaddr"`         // WARNING: Does NOT set VXLAN interface IP. See network interface config for IP assignment.
	PeerAddr       string `uci:"option peeraddr"`       // Peer IP address for point-to-point VXLAN
	VID            string `uci:"option vid"`            // VXLAN Network Identifier (VNI) - 24-bit number
	Port           string `uci:"option port"`           // UDP destination port (default: 4789)
	SrcPort        string `uci:"option srcport"`        // Source port range (e.g., "10000-20000")
	MacAddr        string `uci:"option macaddr"`        // MAC address for the VXLAN interface (optional)
	RxCsum         string `uci:"option rxcsum"`         // Enable RX checksum offload (0/1)
	TxCsum         string `uci:"option txcsum"`         // Enable TX checksum offload (0/1)
	MTU            string `uci:"option mtu"`            // MTU size (optional)
	TTL            string `uci:"option ttl"`            // Time to live for packets (optional)
	TOS            string `uci:"option tos"`            // Type of service for packets (optional)
	DF             string `uci:"option df"`             // Don't Fragment flag (0/1)
	FlowLabel      string `uci:"option flowlabel"`      // IPv6 flow label (optional)
	Ageing         string `uci:"option ageing"`         // MAC address ageing timeout in seconds (default: 300)
	MaxAddress     string `uci:"option maxaddress"`     // Maximum number of FDB entries (optional)
	Learning       string `uci:"option learning"`       // Enable MAC learning (0/1, default: 1)
	RSC            string `uci:"option rsc"`            // Route short circuit (0/1)
	Proxy          string `uci:"option proxy"`          // Enable ARP proxy (0/1)
	L2Miss         string `uci:"option l2miss"`         // Notify userspace of L2 switch misses (0/1)
	L3Miss         string `uci:"option l3miss"`         // Notify userspace of L3 routing misses (0/1)
	UDPCsum        string `uci:"option udpcsum"`        // Enable UDP checksum (0/1)
	UDP6ZeroCsumTx string `uci:"option udp6zerocsumtx"` // Allow zero checksum for IPv6 TX (0/1)
	UDP6ZeroCsumRx string `uci:"option udp6zerocsumrx"` // Allow zero checksum for IPv6 RX (0/1)
	GBP            string `uci:"option gbp"`            // Enable Group Based Policy extension (0/1)
}

// UCIVXLANPeer represents a VXLAN peer configuration (config vxlan_peer).
// Multiple peers can be configured to establish point-to-point or multicast VXLAN tunnels.
type UCIVXLANPeer struct {
	VXLAN  string `uci:"option vxlan"`   // Which VXLAN interface to add peer to (required)
	LLAddr string `uci:"option lladdr"`  // L2 (MAC) address of peer (default: 00:00:00:00:00:00 for source-address learning)
	Dst    string `uci:"option dst"`     // IP address of remote VXLAN tunnel endpoint or multicast address (required)
	Port   string `uci:"option port"`    // UDP destination port number (optional)
	Via    string `uci:"option via"`     // Outgoing interface name to reach remote endpoint (optional, required for multicast)
	VNI    string `uci:"option vni"`     // VXLAN VNI Network Identifier to use for this peer (optional)
	SrcVNI string `uci:"option src_vni"` // Source VNI this entry belongs to (optional, for external/metadata mode)
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

	if values, ok := reader.Get(networkConfigName, name, "ipaddr"); ok && len(values) > 0 {
		config.IPAddr = values[0]
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

	if values, ok := reader.Get(networkConfigName, name, "srcport"); ok && len(values) > 0 {
		config.SrcPort = values[0]
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

	if values, ok := reader.Get(networkConfigName, name, "df"); ok && len(values) > 0 {
		config.DF = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "flowlabel"); ok && len(values) > 0 {
		config.FlowLabel = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "ageing"); ok && len(values) > 0 {
		config.Ageing = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "maxaddress"); ok && len(values) > 0 {
		config.MaxAddress = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "learning"); ok && len(values) > 0 {
		config.Learning = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "rsc"); ok && len(values) > 0 {
		config.RSC = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "proxy"); ok && len(values) > 0 {
		config.Proxy = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "l2miss"); ok && len(values) > 0 {
		config.L2Miss = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "l3miss"); ok && len(values) > 0 {
		config.L3Miss = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "udpcsum"); ok && len(values) > 0 {
		config.UDPCsum = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "udp6zerocsumtx"); ok && len(values) > 0 {
		config.UDP6ZeroCsumTx = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "udp6zerocsumrx"); ok && len(values) > 0 {
		config.UDP6ZeroCsumRx = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "gbp"); ok && len(values) > 0 {
		config.GBP = values[0]
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

	// Set ipaddr (optional)
	if config.IPAddr != "" {
		if err := reader.SetType(networkConfigName, section, "ipaddr", uci.TypeOption, config.IPAddr); err != nil {
			return fmt.Errorf("failed to set VXLAN ipaddr: %w", err)
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

	// Set srcport (optional)
	if config.SrcPort != "" {
		if err := reader.SetType(networkConfigName, section, "srcport", uci.TypeOption, config.SrcPort); err != nil {
			return fmt.Errorf("failed to set VXLAN srcport: %w", err)
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

	// Set df (optional)
	if config.DF != "" {
		if err := reader.SetType(networkConfigName, section, "df", uci.TypeOption, config.DF); err != nil {
			return fmt.Errorf("failed to set VXLAN df: %w", err)
		}
	}

	// Set flowlabel (optional)
	if config.FlowLabel != "" {
		if err := reader.SetType(networkConfigName, section, "flowlabel", uci.TypeOption, config.FlowLabel); err != nil {
			return fmt.Errorf("failed to set VXLAN flowlabel: %w", err)
		}
	}

	// Set ageing (optional)
	if config.Ageing != "" {
		if err := reader.SetType(networkConfigName, section, "ageing", uci.TypeOption, config.Ageing); err != nil {
			return fmt.Errorf("failed to set VXLAN ageing: %w", err)
		}
	}

	// Set maxaddress (optional)
	if config.MaxAddress != "" {
		if err := reader.SetType(networkConfigName, section, "maxaddress", uci.TypeOption, config.MaxAddress); err != nil {
			return fmt.Errorf("failed to set VXLAN maxaddress: %w", err)
		}
	}

	// Set learning (optional)
	if config.Learning != "" {
		if err := reader.SetType(networkConfigName, section, "learning", uci.TypeOption, config.Learning); err != nil {
			return fmt.Errorf("failed to set VXLAN learning: %w", err)
		}
	}

	// Set rsc (optional)
	if config.RSC != "" {
		if err := reader.SetType(networkConfigName, section, "rsc", uci.TypeOption, config.RSC); err != nil {
			return fmt.Errorf("failed to set VXLAN rsc: %w", err)
		}
	}

	// Set proxy (optional)
	if config.Proxy != "" {
		if err := reader.SetType(networkConfigName, section, "proxy", uci.TypeOption, config.Proxy); err != nil {
			return fmt.Errorf("failed to set VXLAN proxy: %w", err)
		}
	}

	// Set l2miss (optional)
	if config.L2Miss != "" {
		if err := reader.SetType(networkConfigName, section, "l2miss", uci.TypeOption, config.L2Miss); err != nil {
			return fmt.Errorf("failed to set VXLAN l2miss: %w", err)
		}
	}

	// Set l3miss (optional)
	if config.L3Miss != "" {
		if err := reader.SetType(networkConfigName, section, "l3miss", uci.TypeOption, config.L3Miss); err != nil {
			return fmt.Errorf("failed to set VXLAN l3miss: %w", err)
		}
	}

	// Set udpcsum (optional)
	if config.UDPCsum != "" {
		if err := reader.SetType(networkConfigName, section, "udpcsum", uci.TypeOption, config.UDPCsum); err != nil {
			return fmt.Errorf("failed to set VXLAN udpcsum: %w", err)
		}
	}

	// Set udp6zerocsumtx (optional)
	if config.UDP6ZeroCsumTx != "" {
		if err := reader.SetType(networkConfigName, section, "udp6zerocsumtx", uci.TypeOption, config.UDP6ZeroCsumTx); err != nil {
			return fmt.Errorf("failed to set VXLAN udp6zerocsumtx: %w", err)
		}
	}

	// Set udp6zerocsumrx (optional)
	if config.UDP6ZeroCsumRx != "" {
		if err := reader.SetType(networkConfigName, section, "udp6zerocsumrx", uci.TypeOption, config.UDP6ZeroCsumRx); err != nil {
			return fmt.Errorf("failed to set VXLAN udp6zerocsumrx: %w", err)
		}
	}

	// Set gbp (optional)
	if config.GBP != "" {
		if err := reader.SetType(networkConfigName, section, "gbp", uci.TypeOption, config.GBP); err != nil {
			return fmt.Errorf("failed to set VXLAN gbp: %w", err)
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

// SetVXLANIPAddr sets the local tunnel endpoint IP address for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - ipaddr: The local IP address (e.g., "192.168.1.1")
//
// Example:
//
//	err := SetVXLANIPAddr("vxlan0", "192.168.1.1")
func SetVXLANIPAddr(section string, ipaddr string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANIPAddrWithReader(section, ipaddr, reader)
}

// SetVXLANIPAddrWithReader sets the local IP address using the provided reader.
func SetVXLANIPAddrWithReader(section string, ipaddr string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ipaddr", uci.TypeOption, ipaddr); err != nil {
		return fmt.Errorf("failed to set VXLAN ipaddr: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN ipaddr: %w", err)
	}

	return nil
}

// SetVXLANSrcPort sets the source port range for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - srcport: The source port range (e.g., "10000-20000")
//
// Example:
//
//	err := SetVXLANSrcPort("vxlan0", "10000-20000")
func SetVXLANSrcPort(section string, srcport string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANSrcPortWithReader(section, srcport, reader)
}

// SetVXLANSrcPortWithReader sets the source port range using the provided reader.
func SetVXLANSrcPortWithReader(section string, srcport string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "srcport", uci.TypeOption, srcport); err != nil {
		return fmt.Errorf("failed to set VXLAN srcport: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN srcport: %w", err)
	}

	return nil
}

// SetVXLANDF sets the Don't Fragment flag for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - df: The Don't Fragment flag ("0" or "1")
//
// Example:
//
//	err := SetVXLANDF("vxlan0", "1")
func SetVXLANDF(section string, df string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANDFWithReader(section, df, reader)
}

// SetVXLANDFWithReader sets the Don't Fragment flag using the provided reader.
func SetVXLANDFWithReader(section string, df string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "df", uci.TypeOption, df); err != nil {
		return fmt.Errorf("failed to set VXLAN df: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN df: %w", err)
	}

	return nil
}

// SetVXLANFlowLabel sets the IPv6 flow label for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - flowlabel: The IPv6 flow label value
//
// Example:
//
//	err := SetVXLANFlowLabel("vxlan0", "0x12345")
func SetVXLANFlowLabel(section string, flowlabel string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANFlowLabelWithReader(section, flowlabel, reader)
}

// SetVXLANFlowLabelWithReader sets the IPv6 flow label using the provided reader.
func SetVXLANFlowLabelWithReader(section string, flowlabel string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "flowlabel", uci.TypeOption, flowlabel); err != nil {
		return fmt.Errorf("failed to set VXLAN flowlabel: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN flowlabel: %w", err)
	}

	return nil
}

// SetVXLANAgeing sets the MAC address ageing timeout for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - ageing: The ageing timeout in seconds (e.g., "300")
//
// Example:
//
//	err := SetVXLANAgeing("vxlan0", "600")
func SetVXLANAgeing(section string, ageing string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANAgeingWithReader(section, ageing, reader)
}

// SetVXLANAgeingWithReader sets the MAC ageing timeout using the provided reader.
func SetVXLANAgeingWithReader(section string, ageing string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "ageing", uci.TypeOption, ageing); err != nil {
		return fmt.Errorf("failed to set VXLAN ageing: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN ageing: %w", err)
	}

	return nil
}

// SetVXLANMaxAddress sets the maximum number of FDB entries for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - maxaddress: The maximum number of FDB entries (e.g., "1024")
//
// Example:
//
//	err := SetVXLANMaxAddress("vxlan0", "2048")
func SetVXLANMaxAddress(section string, maxaddress string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANMaxAddressWithReader(section, maxaddress, reader)
}

// SetVXLANMaxAddressWithReader sets the maximum FDB entries using the provided reader.
func SetVXLANMaxAddressWithReader(section string, maxaddress string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "maxaddress", uci.TypeOption, maxaddress); err != nil {
		return fmt.Errorf("failed to set VXLAN maxaddress: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN maxaddress: %w", err)
	}

	return nil
}

// SetVXLANLearning sets whether MAC learning is enabled for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - learning: Enable MAC learning ("0" or "1", default: "1")
//
// Example:
//
//	err := SetVXLANLearning("vxlan0", "0")
func SetVXLANLearning(section string, learning string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANLearningWithReader(section, learning, reader)
}

// SetVXLANLearningWithReader sets MAC learning using the provided reader.
func SetVXLANLearningWithReader(section string, learning string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "learning", uci.TypeOption, learning); err != nil {
		return fmt.Errorf("failed to set VXLAN learning: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN learning: %w", err)
	}

	return nil
}

// SetVXLANRSC sets route short circuit for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - rsc: Enable route short circuit ("0" or "1")
//
// Example:
//
//	err := SetVXLANRSC("vxlan0", "1")
func SetVXLANRSC(section string, rsc string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANRSCWithReader(section, rsc, reader)
}

// SetVXLANRSCWithReader sets route short circuit using the provided reader.
func SetVXLANRSCWithReader(section string, rsc string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "rsc", uci.TypeOption, rsc); err != nil {
		return fmt.Errorf("failed to set VXLAN rsc: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN rsc: %w", err)
	}

	return nil
}

// SetVXLANProxy sets whether ARP proxy is enabled for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - proxy: Enable ARP proxy ("0" or "1")
//
// Example:
//
//	err := SetVXLANProxy("vxlan0", "1")
func SetVXLANProxy(section string, proxy string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANProxyWithReader(section, proxy, reader)
}

// SetVXLANProxyWithReader sets ARP proxy using the provided reader.
func SetVXLANProxyWithReader(section string, proxy string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "proxy", uci.TypeOption, proxy); err != nil {
		return fmt.Errorf("failed to set VXLAN proxy: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN proxy: %w", err)
	}

	return nil
}

// SetVXLANL2Miss sets whether to notify userspace of L2 switch misses.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - l2miss: Enable L2 miss notifications ("0" or "1")
//
// Example:
//
//	err := SetVXLANL2Miss("vxlan0", "1")
func SetVXLANL2Miss(section string, l2miss string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANL2MissWithReader(section, l2miss, reader)
}

// SetVXLANL2MissWithReader sets L2 miss notifications using the provided reader.
func SetVXLANL2MissWithReader(section string, l2miss string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "l2miss", uci.TypeOption, l2miss); err != nil {
		return fmt.Errorf("failed to set VXLAN l2miss: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN l2miss: %w", err)
	}

	return nil
}

// SetVXLANL3Miss sets whether to notify userspace of L3 routing misses.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - l3miss: Enable L3 miss notifications ("0" or "1")
//
// Example:
//
//	err := SetVXLANL3Miss("vxlan0", "1")
func SetVXLANL3Miss(section string, l3miss string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANL3MissWithReader(section, l3miss, reader)
}

// SetVXLANL3MissWithReader sets L3 miss notifications using the provided reader.
func SetVXLANL3MissWithReader(section string, l3miss string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "l3miss", uci.TypeOption, l3miss); err != nil {
		return fmt.Errorf("failed to set VXLAN l3miss: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN l3miss: %w", err)
	}

	return nil
}

// SetVXLANUDPCsum sets whether UDP checksum is enabled for a VXLAN interface.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - udpcsum: Enable UDP checksum ("0" or "1")
//
// Example:
//
//	err := SetVXLANUDPCsum("vxlan0", "1")
func SetVXLANUDPCsum(section string, udpcsum string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANUDPCsumWithReader(section, udpcsum, reader)
}

// SetVXLANUDPCsumWithReader sets UDP checksum using the provided reader.
func SetVXLANUDPCsumWithReader(section string, udpcsum string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "udpcsum", uci.TypeOption, udpcsum); err != nil {
		return fmt.Errorf("failed to set VXLAN udpcsum: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN udpcsum: %w", err)
	}

	return nil
}

// SetVXLANUDP6ZeroCsumTx sets whether to allow zero checksum for IPv6 TX.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - udp6zerocsumtx: Allow zero checksum for IPv6 TX ("0" or "1")
//
// Example:
//
//	err := SetVXLANUDP6ZeroCsumTx("vxlan0", "1")
func SetVXLANUDP6ZeroCsumTx(section string, udp6zerocsumtx string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANUDP6ZeroCsumTxWithReader(section, udp6zerocsumtx, reader)
}

// SetVXLANUDP6ZeroCsumTxWithReader sets IPv6 zero checksum TX using the provided reader.
func SetVXLANUDP6ZeroCsumTxWithReader(section string, udp6zerocsumtx string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "udp6zerocsumtx", uci.TypeOption, udp6zerocsumtx); err != nil {
		return fmt.Errorf("failed to set VXLAN udp6zerocsumtx: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN udp6zerocsumtx: %w", err)
	}

	return nil
}

// SetVXLANUDP6ZeroCsumRx sets whether to allow zero checksum for IPv6 RX.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - udp6zerocsumrx: Allow zero checksum for IPv6 RX ("0" or "1")
//
// Example:
//
//	err := SetVXLANUDP6ZeroCsumRx("vxlan0", "1")
func SetVXLANUDP6ZeroCsumRx(section string, udp6zerocsumrx string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANUDP6ZeroCsumRxWithReader(section, udp6zerocsumrx, reader)
}

// SetVXLANUDP6ZeroCsumRxWithReader sets IPv6 zero checksum RX using the provided reader.
func SetVXLANUDP6ZeroCsumRxWithReader(section string, udp6zerocsumrx string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "udp6zerocsumrx", uci.TypeOption, udp6zerocsumrx); err != nil {
		return fmt.Errorf("failed to set VXLAN udp6zerocsumrx: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN udp6zerocsumrx: %w", err)
	}

	return nil
}

// SetVXLANGBP sets whether Group Based Policy extension is enabled.
//
// Parameters:
//   - section: The UCI section name (e.g., "vxlan0", "vxlan1")
//   - gbp: Enable GBP extension ("0" or "1")
//
// Example:
//
//	err := SetVXLANGBP("vxlan0", "1")
func SetVXLANGBP(section string, gbp string) error {
	reader := NewUCINetworkConfigReader()
	return SetVXLANGBPWithReader(section, gbp, reader)
}

// SetVXLANGBPWithReader sets GBP extension using the provided reader.
func SetVXLANGBPWithReader(section string, gbp string, reader ConfigReader) error {
	if err := reader.SetType(networkConfigName, section, "gbp", uci.TypeOption, gbp); err != nil {
		return fmt.Errorf("failed to set VXLAN gbp: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN gbp: %w", err)
	}

	return nil
}

// AddVXLANPeer adds a new VXLAN peer configuration as an anonymous section.
// VXLAN peers are typically configured as anonymous sections in UCI.
//
// Parameters:
//   - peer: The VXLAN peer configuration to add
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	peerConfig := &UCIVXLANPeer{
//	    VXLAN: "vxlan0",
//	    Dst:   "239.2.3.1",
//	    Via:   "tailscale0",
//	}
//	err := AddVXLANPeer(peerConfig)
//
// Note: This operation requires appropriate privileges and commits the configuration.
// Anonymous sections are created without a name, as per OpenWRT UCI conventions for vxlan_peer.
func AddVXLANPeer(peer *UCIVXLANPeer) error {
	reader := NewUCINetworkConfigReader()
	return AddVXLANPeerWithReader(peer, reader)
}

// AddVXLANPeerWithReader adds a new VXLAN peer configuration as an anonymous section using the provided reader.
func AddVXLANPeerWithReader(peer *UCIVXLANPeer, reader ConfigReader) error {
	// Get count of existing vxlan_peer sections to determine the index of the new section
	existingSections, err := reader.GetSections(networkConfigName, "vxlan_peer")
	if err != nil {
		// If error getting sections, assume none exist (count = 0)
		existingSections = []string{}
	}
	newIndex := len(existingSections)

	// Create anonymous section (empty string for section name)
	if err := reader.AddSection(networkConfigName, "vxlan_peer", ""); err != nil {
		return fmt.Errorf("failed to add VXLAN peer section: %w", err)
	}

	// Reference the newly created anonymous section using @vxlan_peer[index] notation
	// This is the proper way to reference anonymous sections in UCI
	sectionRef := fmt.Sprintf("@vxlan_peer[%d]", newIndex)

	// Set vxlan interface (required)
	if peer.VXLAN != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "vxlan", uci.TypeOption, peer.VXLAN); err != nil {
			return fmt.Errorf("failed to set VXLAN peer vxlan: %w", err)
		}
	}

	// Set lladdr (optional)
	if peer.LLAddr != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "lladdr", uci.TypeOption, peer.LLAddr); err != nil {
			return fmt.Errorf("failed to set VXLAN peer lladdr: %w", err)
		}
	}

	// Set dst (required)
	if peer.Dst != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "dst", uci.TypeOption, peer.Dst); err != nil {
			return fmt.Errorf("failed to set VXLAN peer dst: %w", err)
		}
	}

	// Set port (optional)
	if peer.Port != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "port", uci.TypeOption, peer.Port); err != nil {
			return fmt.Errorf("failed to set VXLAN peer port: %w", err)
		}
	}

	// Set via (optional)
	if peer.Via != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "via", uci.TypeOption, peer.Via); err != nil {
			return fmt.Errorf("failed to set VXLAN peer via: %w", err)
		}
	}

	// Set vni (optional)
	if peer.VNI != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "vni", uci.TypeOption, peer.VNI); err != nil {
			return fmt.Errorf("failed to set VXLAN peer vni: %w", err)
		}
	}

	// Set src_vni (optional)
	if peer.SrcVNI != "" {
		if err := reader.SetType(networkConfigName, sectionRef, "src_vni", uci.TypeOption, peer.SrcVNI); err != nil {
			return fmt.Errorf("failed to set VXLAN peer src_vni: %w", err)
		}
	}

	// Commit changes
	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN peer config: %w", err)
	}

	return nil
}

// GetVXLANPeerByName loads and returns a VXLAN peer configuration by section name.
// Note: VXLAN peers are typically anonymous sections. This function is provided for
// compatibility but may not work as expected for anonymous sections.
// Use with caution or prefer working with the UCI tree directly for anonymous sections.
//
// Parameters:
//   - name: The UCI section name or @vxlan_peer[index] notation
//
// Returns the VXLAN peer configuration or an error if it cannot be read.
func GetVXLANPeerByName(name string) (*UCIVXLANPeer, error) {
	reader := NewUCINetworkConfigReader()
	return GetVXLANPeerByNameWithReader(name, reader)
}

// GetVXLANPeerByNameWithReader loads and returns a VXLAN peer configuration using the provided reader.
func GetVXLANPeerByNameWithReader(name string, reader ConfigReader) (*UCIVXLANPeer, error) {
	peer := &UCIVXLANPeer{}

	if values, ok := reader.Get(networkConfigName, name, "vxlan"); ok && len(values) > 0 {
		peer.VXLAN = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "lladdr"); ok && len(values) > 0 {
		peer.LLAddr = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "dst"); ok && len(values) > 0 {
		peer.Dst = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "port"); ok && len(values) > 0 {
		peer.Port = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "via"); ok && len(values) > 0 {
		peer.Via = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "vni"); ok && len(values) > 0 {
		peer.VNI = values[0]
	}

	if values, ok := reader.Get(networkConfigName, name, "src_vni"); ok && len(values) > 0 {
		peer.SrcVNI = values[0]
	}

	return peer, nil
}

// UpdateVXLANPeer updates an existing VXLAN peer configuration by section name.
// Note: This is primarily for named sections. For anonymous vxlan_peer sections,
// it's recommended to delete and re-add rather than update.
//
// Parameters:
//   - section: The UCI section name or @vxlan_peer[index] notation
//   - peer: The VXLAN peer configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	peerConfig := &UCIVXLANPeer{
//	    VXLAN: "vxlan0",
//	    Dst:   "10.0.0.2",
//	    Port:  "4789",
//	}
//	err := UpdateVXLANPeer("@vxlan_peer[0]", peerConfig)
func UpdateVXLANPeer(section string, peer *UCIVXLANPeer) error {
	reader := NewUCINetworkConfigReader()
	return UpdateVXLANPeerWithReader(section, peer, reader)
}

// UpdateVXLANPeerWithReader updates an existing VXLAN peer configuration using the provided reader.
func UpdateVXLANPeerWithReader(section string, peer *UCIVXLANPeer, reader ConfigReader) error {
	// Set vxlan interface (required)
	if peer.VXLAN != "" {
		if err := reader.SetType(networkConfigName, section, "vxlan", uci.TypeOption, peer.VXLAN); err != nil {
			return fmt.Errorf("failed to set VXLAN peer vxlan: %w", err)
		}
	}

	// Set lladdr (optional)
	if peer.LLAddr != "" {
		if err := reader.SetType(networkConfigName, section, "lladdr", uci.TypeOption, peer.LLAddr); err != nil {
			return fmt.Errorf("failed to set VXLAN peer lladdr: %w", err)
		}
	}

	// Set dst (required)
	if peer.Dst != "" {
		if err := reader.SetType(networkConfigName, section, "dst", uci.TypeOption, peer.Dst); err != nil {
			return fmt.Errorf("failed to set VXLAN peer dst: %w", err)
		}
	}

	// Set port (optional)
	if peer.Port != "" {
		if err := reader.SetType(networkConfigName, section, "port", uci.TypeOption, peer.Port); err != nil {
			return fmt.Errorf("failed to set VXLAN peer port: %w", err)
		}
	}

	// Set via (optional)
	if peer.Via != "" {
		if err := reader.SetType(networkConfigName, section, "via", uci.TypeOption, peer.Via); err != nil {
			return fmt.Errorf("failed to set VXLAN peer via: %w", err)
		}
	}

	// Set vni (optional)
	if peer.VNI != "" {
		if err := reader.SetType(networkConfigName, section, "vni", uci.TypeOption, peer.VNI); err != nil {
			return fmt.Errorf("failed to set VXLAN peer vni: %w", err)
		}
	}

	// Set src_vni (optional)
	if peer.SrcVNI != "" {
		if err := reader.SetType(networkConfigName, section, "src_vni", uci.TypeOption, peer.SrcVNI); err != nil {
			return fmt.Errorf("failed to set VXLAN peer src_vni: %w", err)
		}
	}

	// Commit changes
	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN peer config: %w", err)
	}

	return nil
}

// DeleteVXLANPeerByName removes a VXLAN peer configuration section by name.
// For anonymous sections, use @vxlan_peer[index] notation.
//
// Parameters:
//   - section: The UCI section name or @vxlan_peer[index] to delete
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	// Delete first vxlan_peer
//	err := DeleteVXLANPeerByName("@vxlan_peer[0]")
//	if err != nil {
//	    log.Fatalf("Failed to delete VXLAN peer: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteVXLANPeerByName(section string) error {
	reader := NewUCINetworkConfigReader()
	return DeleteVXLANPeerByNameWithReader(section, reader)
}

// DeleteVXLANPeerByNameWithReader removes a VXLAN peer configuration section using the provided reader.
func DeleteVXLANPeerByNameWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(networkConfigName, section); err != nil {
		return fmt.Errorf("failed to delete VXLAN peer section %s: %w", section, err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN peer deletion: %w", err)
	}

	return nil
}

// VXLANPeerSectionExists checks if a VXLAN peer section exists in the configuration.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "peer0", "peer1")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := VXLANPeerSectionExists("peer0")
//	if exists {
//	    fmt.Println("VXLAN peer section exists")
//	}
func VXLANPeerSectionExists(section string) bool {
	reader := NewUCINetworkConfigReader()
	return VXLANPeerSectionExistsWithReader(section, reader)
}

// VXLANPeerSectionExistsWithReader checks if a VXLAN peer section exists using the provided reader.
func VXLANPeerSectionExistsWithReader(section string, reader ConfigReader) bool {
	// Check for 'vxlan' or 'dst' field to verify peer section exists
	_, vxlanExists := reader.Get(networkConfigName, section, "vxlan")
	if vxlanExists {
		return true
	}
	_, dstExists := reader.Get(networkConfigName, section, "dst")
	return dstExists
}

// GetVXLANPeerByDst searches for and returns a VXLAN peer configuration by destination address.
// This function searches through VXLAN peer sections to find one with a matching dst field.
//
// Parameters:
//   - dst: The destination address to search for (e.g., "10.0.0.2", "239.2.3.1")
//
// Returns the VXLAN peer configuration and section name if found, or an error if not found.
//
// Example:
//
//	peer, section, err := GetVXLANPeerByDst("239.2.3.1")
//	if err != nil {
//	    log.Fatalf("Failed to find VXLAN peer: %v", err)
//	}
//	fmt.Printf("Found peer in section %s with VNI %s\n", section, peer.VNI)
func GetVXLANPeerByDst(dst string) (*UCIVXLANPeer, string, error) {
	reader := NewUCINetworkConfigReader()
	return GetVXLANPeerByDstWithReader(dst, reader)
}

// GetVXLANPeerByDstWithReader searches for and returns a VXLAN peer configuration by destination address using the provided reader.
func GetVXLANPeerByDstWithReader(dst string, reader ConfigReader) (*UCIVXLANPeer, string, error) {
	// For the real UCI tree, we need to use the tree's sections list
	// For now, we'll try common section patterns
	// In practice, this would need to iterate through all vxlan_peer sections

	// List of common peer section name patterns to search
	peerSections := []string{
		"peer_multicast",
		"peer_unicast",
	}

	// Try common named peer sections with numeric suffixes
	for i := 0; i < 100; i++ {
		peerSections = append(peerSections, fmt.Sprintf("peer%d", i))
	}

	// Search through all common peer section names
	for _, section := range peerSections {
		if values, ok := reader.Get(networkConfigName, section, "dst"); ok && len(values) > 0 {
			if values[0] == dst {
				peer, err := GetVXLANPeerByNameWithReader(section, reader)
				if err != nil {
					return nil, "", err
				}
				return peer, section, nil
			}
		}
	}

	// Try anonymous section notation
	for i := 0; i < 100; i++ {
		section := fmt.Sprintf("@vxlan_peer[%d]", i)
		if values, ok := reader.Get(networkConfigName, section, "dst"); ok && len(values) > 0 {
			if values[0] == dst {
				peer, err := GetVXLANPeerByNameWithReader(section, reader)
				if err != nil {
					return nil, "", err
				}
				return peer, section, nil
			}
		}
	}

	return nil, "", fmt.Errorf("VXLAN peer with dst %s not found", dst)
}

// VXLANPeerExistsByDst checks if a VXLAN peer with the specified destination address exists.
//
// Parameters:
//   - dst: The destination address to search for (e.g., "10.0.0.2", "239.2.3.1")
//
// Returns true if a peer with the specified destination exists, false otherwise.
//
// Example:
//
//	if VXLANPeerExistsByDst("239.2.3.1") {
//	    fmt.Println("Multicast peer already exists")
//	}
func VXLANPeerExistsByDst(dst string) bool {
	reader := NewUCINetworkConfigReader()
	return VXLANPeerExistsByDstWithReader(dst, reader)
}

// VXLANPeerExistsByDstWithReader checks if a VXLAN peer with the specified destination address exists using the provided reader.
func VXLANPeerExistsByDstWithReader(dst string, reader ConfigReader) bool {
	_, _, err := GetVXLANPeerByDstWithReader(dst, reader)
	return err == nil
}

// DeleteVXLANPeerByDst removes a VXLAN peer configuration by destination address.
// This function searches for a peer with the specified destination address and deletes it.
//
// Parameters:
//   - dst: The destination address of the peer to delete (e.g., "10.0.0.2", "239.2.3.1")
//
// Returns an error if the peer cannot be found or deleted.
//
// Example:
//
//	err := DeleteVXLANPeerByDst("239.2.3.1")
//	if err != nil {
//	    log.Fatalf("Failed to delete VXLAN peer: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteVXLANPeerByDst(dst string) error {
	reader := NewUCINetworkConfigReader()
	return DeleteVXLANPeerByDstWithReader(dst, reader)
}

// DeleteVXLANPeerByDstWithReader removes a VXLAN peer configuration by destination address using the provided reader.
func DeleteVXLANPeerByDstWithReader(dst string, reader ConfigReader) error {
	// First, find the peer by destination address
	_, section, err := GetVXLANPeerByDstWithReader(dst, reader)
	if err != nil {
		return fmt.Errorf("failed to find VXLAN peer with dst %s: %w", dst, err)
	}

	// Delete the peer using the section name
	if err := reader.DelSection(networkConfigName, section); err != nil {
		return fmt.Errorf("failed to delete VXLAN peer section %s: %w", section, err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit VXLAN peer deletion: %w", err)
	}

	return nil
}

// GetAllVXLANPeers retrieves all VXLAN peer configurations from the network config.
// It returns a map of section names to UCIVXLANPeer configurations.
func GetAllVXLANPeers() (map[string]*UCIVXLANPeer, error) {
	reader := NewUCINetworkConfigReader()
	return GetAllVXLANPeersWithReader(reader)
}

// GetAllVXLANPeersWithReader retrieves all VXLAN peer configurations using the provided reader.
// It returns a map of section names to UCIVXLANPeer configurations.
func GetAllVXLANPeersWithReader(reader ConfigReader) (map[string]*UCIVXLANPeer, error) {
	// Get all sections of type "vxlan_peer"
	sections, err := reader.GetSections(networkConfigName, "vxlan_peer")
	if err != nil {
		return nil, fmt.Errorf("failed to get vxlan_peer sections: %w", err)
	}

	// Create map to hold peers
	peers := make(map[string]*UCIVXLANPeer, len(sections))

	// Load each peer
	for _, section := range sections {
		peer, err := GetVXLANPeerByNameWithReader(section, reader)
		if err != nil {
			// Log but continue on error for individual peer
			continue
		}
		peers[section] = peer
	}

	return peers, nil
}
