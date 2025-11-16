package batmanadv

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

type MeshConfig struct {
	Version                    string         `json:"version"`
	AlgoName                   string         `json:"algo_name"`
	MeshIfindex                int            `json:"mesh_ifindex"`
	MeshIfname                 string         `json:"mesh_ifname"`
	MeshAddress                string         `json:"mesh_address"`
	HardIfindex                int            `json:"hard_ifindex"`
	HardIfname                 string         `json:"hard_ifname"`
	HardAddress                string         `json:"hard_address"`
	TtTtvn                     int            `json:"tt_ttvn"`
	BlaCrc                     int            `json:"bla_crc"`
	McastFlags                 McastFlags     `json:"mcast_flags"`
	McastFlagsPriv             McastFlagsPriv `json:"mcast_flags_priv"`
	AggregatedOgmsEnabled      bool           `json:"aggregated_ogms_enabled"`
	ApIsolationEnabled         bool           `json:"ap_isolation_enabled"`
	IsolationMark              int            `json:"isolation_mark"`
	IsolationMask              int            `json:"isolation_mask"`
	BondingEnabled             bool           `json:"bonding_enabled"`
	BridgeLoopAvoidanceEnabled bool           `json:"bridge_loop_avoidance_enabled"`
	DistributedArpTableEnabled bool           `json:"distributed_arp_table_enabled"`
	FragmentationEnabled       bool           `json:"fragmentation_enabled"`
	GwBandwidthDown            int            `json:"gw_bandwidth_down"`
	GwBandwidthUp              int            `json:"gw_bandwidth_up"`
	GwMode                     string         `json:"gw_mode"`
	GwSelClass                 int            `json:"gw_sel_class"`
	HopPenalty                 int            `json:"hop_penalty"`
	MulticastForcefloodEnabled bool           `json:"multicast_forceflood_enabled"`
	OrigInterval               int            `json:"orig_interval"`
	MulticastFanout            int            `json:"multicast_fanout"`
}

type McastFlags struct {
	AllUnsnoopables bool `json:"all_unsnoopables"`
	WantAllIpv4     bool `json:"want_all_ipv4"`
	WantAllIpv6     bool `json:"want_all_ipv6"`
	WantNoRtrIpv4   bool `json:"want_no_rtr_ipv4"`
	WantNoRtrIpv6   bool `json:"want_no_rtr_ipv6"`
	Raw             int  `json:"raw"`
}

type McastFlagsPriv struct {
	Bridged              bool `json:"bridged"`
	QuerierIpv4Exists    bool `json:"querier_ipv4_exists"`
	QuerierIpv6Exists    bool `json:"querier_ipv6_exists"`
	QuerierIpv4Shadowing bool `json:"querier_ipv4_shadowing"`
	QuerierIpv6Shadowing bool `json:"querier_ipv6_shadowing"`
	Raw                  int  `json:"raw"`
}

func GetMeshConfig(iface string) (*MeshConfig, error) {
	cmd := exec.Command("batctl", "mj")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var config MeshConfig
	if err := json.Unmarshal(output, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// IsGatewayMode returns true if the mesh interface is configured as a gateway
func (m *MeshConfig) IsGatewayMode() bool {
	return m.GwMode == "server"
}

// IsClientMode returns true if the mesh interface is configured as a gateway client
func (m *MeshConfig) IsClientMode() bool {
	return m.GwMode == "client"
}

// IsOffMode returns true if gateway functionality is disabled
func (m *MeshConfig) IsOffMode() bool {
	return m.GwMode == "off"
}

// GetGatewayBandwidth returns the configured gateway bandwidth in a human-readable format
func (m *MeshConfig) GetGatewayBandwidth() string {
	return formatBandwidth(m.GwBandwidthDown, m.GwBandwidthUp)
}

// IsBridged returns true if the mesh interface is bridged
func (m *MeshConfig) IsBridged() bool {
	return m.McastFlagsPriv.Bridged
}

// HasIPv4Querier returns true if an IPv4 multicast querier exists
func (m *MeshConfig) HasIPv4Querier() bool {
	return m.McastFlagsPriv.QuerierIpv4Exists
}

// HasIPv6Querier returns true if an IPv6 multicast querier exists
func (m *MeshConfig) HasIPv6Querier() bool {
	return m.McastFlagsPriv.QuerierIpv6Exists
}

// WantsAllMulticast returns true if the mesh wants all multicast traffic
func (m *MeshConfig) WantsAllMulticast() bool {
	return m.McastFlags.WantAllIpv4 || m.McastFlags.WantAllIpv6
}

// String returns a JSON representation of the mesh configuration
func (m *MeshConfig) String() string {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

// GetAlgorithm returns the routing algorithm name
func (m *MeshConfig) GetAlgorithm() string {
	return m.AlgoName
}

// GetOriginatorInterval returns the originator interval in milliseconds
func (m *MeshConfig) GetOriginatorInterval() int {
	return m.OrigInterval
}

// IsFragmentationEnabled returns true if fragmentation is enabled
func (m *MeshConfig) IsFragmentationEnabled() bool {
	return m.FragmentationEnabled
}

// IsBondingEnabled returns true if bonding is enabled
func (m *MeshConfig) IsBondingEnabled() bool {
	return m.BondingEnabled
}

// IsBridgeLoopAvoidanceEnabled returns true if bridge loop avoidance is enabled
func (m *MeshConfig) IsBridgeLoopAvoidanceEnabled() bool {
	return m.BridgeLoopAvoidanceEnabled
}

// IsDistributedArpTableEnabled returns true if distributed ARP table is enabled
func (m *MeshConfig) IsDistributedArpTableEnabled() bool {
	return m.DistributedArpTableEnabled
}

// IsAPIsolationEnabled returns true if AP isolation is enabled
func (m *MeshConfig) IsAPIsolationEnabled() bool {
	return m.ApIsolationEnabled
}

// IsMulticastForcefloodEnabled returns true if multicast forceflood is enabled
func (m *MeshConfig) IsMulticastForcefloodEnabled() bool {
	return m.MulticastForcefloodEnabled
}

// formatBandwidth formats bandwidth values into a human-readable string
func formatBandwidth(down, up int) string {
	if down == 0 && up == 0 {
		return "0/0 kbit"
	}
	return formatKbit(down) + "/" + formatKbit(up)
}

// formatKbit formats a bandwidth value in kbit/s
func formatKbit(kbit int) string {
	if kbit >= 1000000 {
		return formatFloat(float64(kbit)/1000000.0) + " gbit"
	} else if kbit >= 1000 {
		return formatFloat(float64(kbit)/1000.0) + " mbit"
	}
	return formatFloat(float64(kbit)) + " kbit"
}

// formatFloat formats a float with appropriate precision
func formatFloat(f float64) string {
	if f == float64(int(f)) {
		return formatInt(int(f))
	}
	return formatDecimal(f)
}

// formatInt converts an integer to string
func formatInt(i int) string {
	return strconv.Itoa(i)
}

// formatDecimal formats a decimal number
func formatDecimal(f float64) string {
	return fmt.Sprintf("%.1f", f)
}
