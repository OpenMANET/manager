package firewall

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/digineo/go-uci/v2"
)

const (
	firewallConfigName string = "firewall"
)

// UCIFirewallDefaults represents the global firewall defaults.
type UCIFirewallDefaults struct {
	Input           string `uci:"option input"`
	Output          string `uci:"option output"`
	Forward         string `uci:"option forward"`
	SynfloodProtect string `uci:"option synflood_protect"`
	DisableIPV6     string `uci:"option disable_ipv6"`
	DropInvalid     string `uci:"option drop_invalid"`
}

// UCIFirewallZone represents a firewall zone configuration.
type UCIFirewallZone struct {
	Name    string   `uci:"option name"`
	Input   string   `uci:"option input"`
	Output  string   `uci:"option output"`
	Forward string   `uci:"option forward"`
	Masq    string   `uci:"option masq"`
	MtuFix  string   `uci:"option mtu_fix"`
	Network []string `uci:"list network"`
}

// UCIFirewallForwarding represents a forwarding rule between zones.
type UCIFirewallForwarding struct {
	Src     string `uci:"option src"`
	Dest    string `uci:"option dest"`
	Enabled string `uci:"option enabled"`
}

// UCIFirewallRule represents a firewall rule.
type UCIFirewallRule struct {
	Name     string   `uci:"option name"`
	Src      string   `uci:"option src"`
	Dest     string   `uci:"option dest"`
	Proto    string   `uci:"option proto"`
	DestPort string   `uci:"option dest_port"`
	SrcPort  string   `uci:"option src_port"`
	Target   string   `uci:"option target"`
	Family   string   `uci:"option family"`
	SrcIP    string   `uci:"option src_ip"`
	Limit    string   `uci:"option limit"`
	IcmpType []string `uci:"list icmp_type"`
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

// UCIFirewallConfigReader wraps the UCI functions for firewall configuration.
type UCIFirewallConfigReader struct {
	tree uci.Tree
}

// NewUCIFirewallConfigReader creates a new UCI firewall config reader with the default tree.
func NewUCIFirewallConfigReader() *UCIFirewallConfigReader {
	return &UCIFirewallConfigReader{
		tree: uci.NewTree(uci.DefaultTreePath),
	}
}

func (r *UCIFirewallConfigReader) Get(config, section, option string) ([]string, bool) {
	return r.tree.Get(config, section, option)
}

func (r *UCIFirewallConfigReader) GetSections(config, secType string) ([]string, error) {
	return r.tree.GetSections(config, secType)
}

func (r *UCIFirewallConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return r.tree.SetType(config, section, option, typ, values...)
}

func (r *UCIFirewallConfigReader) Del(config, section, option string) error {
	return r.tree.Del(config, section, option)
}

func (r *UCIFirewallConfigReader) AddSection(config, section, typ string) error {
	return r.tree.AddSection(config, section, typ)
}

func (r *UCIFirewallConfigReader) DelSection(config, section string) error {
	return r.tree.DelSection(config, section)
}

func (r *UCIFirewallConfigReader) Commit() error {
	return r.tree.Commit()
}

func (r *UCIFirewallConfigReader) ReloadConfig() error {
	return r.tree.LoadConfig(firewallConfigName, true)
}

// GetFirewallDefaults loads and returns the UCI firewall defaults configuration.
//
// Returns the firewall defaults configuration or an error if it cannot be read.
//
// Example:
//
//	defaults, err := GetFirewallDefaults()
//	if err != nil {
//	    log.Fatalf("Failed to get firewall defaults: %v", err)
//	}
//	fmt.Printf("Input policy: %s\n", defaults.Input)
func GetFirewallDefaults() (*UCIFirewallDefaults, error) {
	return GetFirewallDefaultsWithReader(NewUCIFirewallConfigReader())
}

// GetFirewallDefaultsWithReader loads and returns the UCI firewall defaults using the provided reader.
func GetFirewallDefaultsWithReader(reader ConfigReader) (*UCIFirewallDefaults, error) {
	var config UCIFirewallDefaults

	section := "defaults"

	if values, ok := reader.Get(firewallConfigName, section, "input"); ok && len(values) > 0 {
		config.Input = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, section, "output"); ok && len(values) > 0 {
		config.Output = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, section, "forward"); ok && len(values) > 0 {
		config.Forward = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, section, "synflood_protect"); ok && len(values) > 0 {
		config.SynfloodProtect = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, section, "disable_ipv6"); ok && len(values) > 0 {
		config.DisableIPV6 = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, section, "drop_invalid"); ok && len(values) > 0 {
		config.DropInvalid = values[0]
	}

	return &config, nil
}

// SetFirewallDefaults creates or updates the firewall defaults configuration.
//
// Parameters:
//   - config: The firewall defaults configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	defaults := &UCIFirewallDefaults{
//	    Input:            "REJECT",
//	    Output:           "ACCEPT",
//	    Forward:          "REJECT",
//	    SynfloodProtect:  "1",
//	}
//	err := SetFirewallDefaults(defaults)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetFirewallDefaults(config *UCIFirewallDefaults) error {
	return SetFirewallDefaultsWithReader(config, NewUCIFirewallConfigReader())
}

// SetFirewallDefaultsWithReader creates or updates the firewall defaults using the provided reader.
func SetFirewallDefaultsWithReader(config *UCIFirewallDefaults, reader ConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	section := "defaults"
	_ = reader.AddSection(firewallConfigName, section, "defaults")

	if config.Input != "" {
		if err := reader.SetType(firewallConfigName, section, "input", uci.TypeOption, config.Input); err != nil {
			return fmt.Errorf("failed to set input: %w", err)
		}
	}

	if config.Output != "" {
		if err := reader.SetType(firewallConfigName, section, "output", uci.TypeOption, config.Output); err != nil {
			return fmt.Errorf("failed to set output: %w", err)
		}
	}

	if config.Forward != "" {
		if err := reader.SetType(firewallConfigName, section, "forward", uci.TypeOption, config.Forward); err != nil {
			return fmt.Errorf("failed to set forward: %w", err)
		}
	}

	if config.SynfloodProtect != "" {
		if err := reader.SetType(firewallConfigName, section, "synflood_protect", uci.TypeOption, config.SynfloodProtect); err != nil {
			return fmt.Errorf("failed to set synflood_protect: %w", err)
		}
	}

	if config.DisableIPV6 != "" {
		if err := reader.SetType(firewallConfigName, section, "disable_ipv6", uci.TypeOption, config.DisableIPV6); err != nil {
			return fmt.Errorf("failed to set disable_ipv6: %w", err)
		}
	}

	if config.DropInvalid != "" {
		if err := reader.SetType(firewallConfigName, section, "drop_invalid", uci.TypeOption, config.DropInvalid); err != nil {
			return fmt.Errorf("failed to set drop_invalid: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// GetFirewallZone loads and returns a firewall zone configuration by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "lan", "wan", "ahwlan")
//
// Returns the zone configuration or an error if it cannot be read.
//
// Example:
//
//	zone, err := GetFirewallZone("lan")
//	if err != nil {
//	    log.Fatalf("Failed to get zone: %v", err)
//	}
//	fmt.Printf("Zone input policy: %s\n", zone.Input)
func GetFirewallZone(name string) (*UCIFirewallZone, error) {
	return GetFirewallZoneWithReader(name, NewUCIFirewallConfigReader())
}

// GetFirewallZoneWithReader loads and returns a firewall zone using the provided reader.
func GetFirewallZoneWithReader(name string, reader ConfigReader) (*UCIFirewallZone, error) {
	var config UCIFirewallZone

	if values, ok := reader.Get(firewallConfigName, name, "name"); ok && len(values) > 0 {
		config.Name = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "input"); ok && len(values) > 0 {
		config.Input = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "output"); ok && len(values) > 0 {
		config.Output = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "forward"); ok && len(values) > 0 {
		config.Forward = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "network"); ok {
		config.Network = values
	}

	if values, ok := reader.Get(firewallConfigName, name, "masq"); ok && len(values) > 0 {
		config.Masq = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "mtu_fix"); ok && len(values) > 0 {
		config.MtuFix = values[0]
	}

	return &config, nil
}

// SetFirewallZone creates or updates a firewall zone configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan", "ahwlan")
//   - config: The zone configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	zone := &UCIFirewallZone{
//	    Name:    "lan",
//	    Input:   "ACCEPT",
//	    Output:  "ACCEPT",
//	    Forward: "ACCEPT",
//	    Network: []string{"lan"},
//	    Masq:    "1",
//	    MtuFix:  "1",
//	}
//	err := SetFirewallZone("lan", zone)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetFirewallZone(section string, config *UCIFirewallZone) error {
	return SetFirewallZoneWithReader(section, config, NewUCIFirewallConfigReader())
}

// SetFirewallZoneWithReader creates or updates a firewall zone using the provided reader.
func SetFirewallZoneWithReader(section string, config *UCIFirewallZone, reader ConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	_ = reader.AddSection(firewallConfigName, section, "zone")

	if config.Name != "" {
		if err := reader.SetType(firewallConfigName, section, "name", uci.TypeOption, config.Name); err != nil {
			return fmt.Errorf("failed to set name: %w", err)
		}
	}

	if config.Input != "" {
		if err := reader.SetType(firewallConfigName, section, "input", uci.TypeOption, config.Input); err != nil {
			return fmt.Errorf("failed to set input: %w", err)
		}
	}

	if config.Output != "" {
		if err := reader.SetType(firewallConfigName, section, "output", uci.TypeOption, config.Output); err != nil {
			return fmt.Errorf("failed to set output: %w", err)
		}
	}

	if config.Forward != "" {
		if err := reader.SetType(firewallConfigName, section, "forward", uci.TypeOption, config.Forward); err != nil {
			return fmt.Errorf("failed to set forward: %w", err)
		}
	}

	if len(config.Network) > 0 {
		if err := reader.SetType(firewallConfigName, section, "network", uci.TypeList, config.Network...); err != nil {
			return fmt.Errorf("failed to set network: %w", err)
		}
	}

	if config.Masq != "" {
		if err := reader.SetType(firewallConfigName, section, "masq", uci.TypeOption, config.Masq); err != nil {
			return fmt.Errorf("failed to set masq: %w", err)
		}
	}

	if config.MtuFix != "" {
		if err := reader.SetType(firewallConfigName, section, "mtu_fix", uci.TypeOption, config.MtuFix); err != nil {
			return fmt.Errorf("failed to set mtu_fix: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// DeleteFirewallZone removes a firewall zone configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "lan", "wan")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteFirewallZone("guest")
//	if err != nil {
//	    log.Fatalf("Failed to delete zone: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteFirewallZone(section string) error {
	return DeleteFirewallZoneWithReader(section, NewUCIFirewallConfigReader())
}

// DeleteFirewallZoneWithReader removes a firewall zone using the provided reader.
func DeleteFirewallZoneWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(firewallConfigName, section); err != nil {
		return fmt.Errorf("failed to delete zone section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// FirewallZoneExists checks if a firewall zone exists in the configuration.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "lan", "wan", "ahwlan")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := FirewallZoneExists("lan")
//	if exists {
//	    fmt.Println("LAN zone exists")
//	}
func FirewallZoneExists(section string) bool {
	return FirewallZoneExistsWithReader(section, NewUCIFirewallConfigReader())
}

// FirewallZoneExistsWithReader checks if a firewall zone exists using the provided reader.
func FirewallZoneExistsWithReader(section string, reader ConfigReader) bool {
	_, exists := reader.Get(firewallConfigName, section, "name")

	return exists
}

// GetFirewallForwarding loads and returns a firewall forwarding rule by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "mmrouter")
//
// Returns the forwarding configuration or an error if it cannot be read.
//
// Example:
//
//	forwarding, err := GetFirewallForwarding("mmrouter")
//	if err != nil {
//	    log.Fatalf("Failed to get forwarding: %v", err)
//	}
//	fmt.Printf("Forward from %s to %s\n", forwarding.Src, forwarding.Dest)
func GetFirewallForwarding(name string) (*UCIFirewallForwarding, error) {
	return GetFirewallForwardingWithReader(name, NewUCIFirewallConfigReader())
}

// GetFirewallForwardingWithReader loads and returns a forwarding rule using the provided reader.
func GetFirewallForwardingWithReader(name string, reader ConfigReader) (*UCIFirewallForwarding, error) {
	var config UCIFirewallForwarding

	if values, ok := reader.Get(firewallConfigName, name, "src"); ok && len(values) > 0 {
		config.Src = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "dest"); ok && len(values) > 0 {
		config.Dest = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "enabled"); ok && len(values) > 0 {
		config.Enabled = values[0]
	}

	return &config, nil
}

// SetFirewallForwarding creates or updates a firewall forwarding rule.
//
// Parameters:
//   - section: The UCI section name (e.g., "mmrouter")
//   - config: The forwarding configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	forwarding := &UCIFirewallForwarding{
//	    Src:     "ahwlan",
//	    Dest:    "lan",
//	    Enabled: "1",
//	}
//	err := SetFirewallForwarding("mmrouter", forwarding)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetFirewallForwarding(section string, config *UCIFirewallForwarding) error {
	return SetFirewallForwardingWithReader(section, config, NewUCIFirewallConfigReader())
}

// SetFirewallForwardingWithReader creates or updates a forwarding rule using the provided reader.
func SetFirewallForwardingWithReader(section string, config *UCIFirewallForwarding, reader ConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	_ = reader.AddSection(firewallConfigName, section, "forwarding")

	if config.Src != "" {
		if err := reader.SetType(firewallConfigName, section, "src", uci.TypeOption, config.Src); err != nil {
			return fmt.Errorf("failed to set src: %w", err)
		}
	}

	if config.Dest != "" {
		if err := reader.SetType(firewallConfigName, section, "dest", uci.TypeOption, config.Dest); err != nil {
			return fmt.Errorf("failed to set dest: %w", err)
		}
	}

	if config.Enabled != "" {
		if err := reader.SetType(firewallConfigName, section, "enabled", uci.TypeOption, config.Enabled); err != nil {
			return fmt.Errorf("failed to set enabled: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// DeleteFirewallForwarding removes a firewall forwarding rule.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "mmrouter")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteFirewallForwarding("guest_fwd")
//	if err != nil {
//	    log.Fatalf("Failed to delete forwarding: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteFirewallForwarding(section string) error {
	return DeleteFirewallForwardingWithReader(section, NewUCIFirewallConfigReader())
}

// DeleteFirewallForwardingWithReader removes a forwarding rule using the provided reader.
func DeleteFirewallForwardingWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(firewallConfigName, section); err != nil {
		return fmt.Errorf("failed to delete forwarding section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// FirewallForwardingExists checks if a firewall forwarding rule exists.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "mmrouter")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := FirewallForwardingExists("mmrouter")
//	if exists {
//	    fmt.Println("Forwarding rule exists")
//	}
func FirewallForwardingExists(section string) bool {
	return FirewallForwardingExistsWithReader(section, NewUCIFirewallConfigReader())
}

// FirewallForwardingExistsWithReader checks if a forwarding rule exists using the provided reader.
func FirewallForwardingExistsWithReader(section string, reader ConfigReader) bool {
	_, exists := reader.Get(firewallConfigName, section, "src")

	return exists
}

// GetFirewallRule loads and returns a firewall rule by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "Allow-DHCP-Renew")
//
// Returns the rule configuration or an error if it cannot be read.
//
// Example:
//
//	rule, err := GetFirewallRule("Allow-Ping")
//	if err != nil {
//	    log.Fatalf("Failed to get rule: %v", err)
//	}
//	fmt.Printf("Rule target: %s\n", rule.Target)
func GetFirewallRule(name string) (*UCIFirewallRule, error) {
	return GetFirewallRuleWithReader(name, NewUCIFirewallConfigReader())
}

// GetFirewallRuleWithReader loads and returns a firewall rule using the provided reader.
func GetFirewallRuleWithReader(name string, reader ConfigReader) (*UCIFirewallRule, error) {
	var config UCIFirewallRule

	if values, ok := reader.Get(firewallConfigName, name, "name"); ok && len(values) > 0 {
		config.Name = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "src"); ok && len(values) > 0 {
		config.Src = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "dest"); ok && len(values) > 0 {
		config.Dest = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "proto"); ok && len(values) > 0 {
		config.Proto = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "dest_port"); ok && len(values) > 0 {
		config.DestPort = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "src_port"); ok && len(values) > 0 {
		config.SrcPort = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "target"); ok && len(values) > 0 {
		config.Target = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "family"); ok && len(values) > 0 {
		config.Family = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "icmp_type"); ok {
		config.IcmpType = values
	}

	if values, ok := reader.Get(firewallConfigName, name, "src_ip"); ok && len(values) > 0 {
		config.SrcIP = values[0]
	}

	if values, ok := reader.Get(firewallConfigName, name, "limit"); ok && len(values) > 0 {
		config.Limit = values[0]
	}

	return &config, nil
}

// SetFirewallRule creates or updates a firewall rule.
//
// Parameters:
//   - section: The UCI section name (e.g., "Allow-Ping")
//   - config: The rule configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	rule := &UCIFirewallRule{
//	    Name:     "Allow-Ping",
//	    Src:      "wan",
//	    Proto:    "icmp",
//	    IcmpType: []string{"echo-request"},
//	    Family:   "ipv4",
//	    Target:   "ACCEPT",
//	}
//	err := SetFirewallRule("Allow-Ping", rule)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetFirewallRule(section string, config *UCIFirewallRule) error {
	return SetFirewallRuleWithReader(section, config, NewUCIFirewallConfigReader())
}

// SetFirewallRuleWithReader creates or updates a firewall rule using the provided reader.
func SetFirewallRuleWithReader(section string, config *UCIFirewallRule, reader ConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	_ = reader.AddSection(firewallConfigName, section, "rule")

	if config.Name != "" {
		if err := reader.SetType(firewallConfigName, section, "name", uci.TypeOption, config.Name); err != nil {
			return fmt.Errorf("failed to set name: %w", err)
		}
	}

	if config.Src != "" {
		if err := reader.SetType(firewallConfigName, section, "src", uci.TypeOption, config.Src); err != nil {
			return fmt.Errorf("failed to set src: %w", err)
		}
	}

	if config.Dest != "" {
		if err := reader.SetType(firewallConfigName, section, "dest", uci.TypeOption, config.Dest); err != nil {
			return fmt.Errorf("failed to set dest: %w", err)
		}
	}

	if config.Proto != "" {
		if err := reader.SetType(firewallConfigName, section, "proto", uci.TypeOption, config.Proto); err != nil {
			return fmt.Errorf("failed to set proto: %w", err)
		}
	}

	if config.DestPort != "" {
		if err := reader.SetType(firewallConfigName, section, "dest_port", uci.TypeOption, config.DestPort); err != nil {
			return fmt.Errorf("failed to set dest_port: %w", err)
		}
	}

	if config.SrcPort != "" {
		if err := reader.SetType(firewallConfigName, section, "src_port", uci.TypeOption, config.SrcPort); err != nil {
			return fmt.Errorf("failed to set src_port: %w", err)
		}
	}

	if config.Target != "" {
		if err := reader.SetType(firewallConfigName, section, "target", uci.TypeOption, config.Target); err != nil {
			return fmt.Errorf("failed to set target: %w", err)
		}
	}

	if config.Family != "" {
		if err := reader.SetType(firewallConfigName, section, "family", uci.TypeOption, config.Family); err != nil {
			return fmt.Errorf("failed to set family: %w", err)
		}
	}

	if len(config.IcmpType) > 0 {
		if err := reader.SetType(firewallConfigName, section, "icmp_type", uci.TypeList, config.IcmpType...); err != nil {
			return fmt.Errorf("failed to set icmp_type: %w", err)
		}
	}

	if config.SrcIP != "" {
		if err := reader.SetType(firewallConfigName, section, "src_ip", uci.TypeOption, config.SrcIP); err != nil {
			return fmt.Errorf("failed to set src_ip: %w", err)
		}
	}

	if config.Limit != "" {
		if err := reader.SetType(firewallConfigName, section, "limit", uci.TypeOption, config.Limit); err != nil {
			return fmt.Errorf("failed to set limit: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// DeleteFirewallRule removes a firewall rule.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "Allow-Ping")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteFirewallRule("old_rule")
//	if err != nil {
//	    log.Fatalf("Failed to delete rule: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteFirewallRule(section string) error {
	return DeleteFirewallRuleWithReader(section, NewUCIFirewallConfigReader())
}

// DeleteFirewallRuleWithReader removes a firewall rule using the provided reader.
func DeleteFirewallRuleWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(firewallConfigName, section); err != nil {
		return fmt.Errorf("failed to delete rule section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// FirewallRuleExists checks if a firewall rule exists.
//
// Parameters:
//   - section: The UCI section name to check (e.g., "Allow-Ping")
//
// Returns true if the section exists, false otherwise.
//
// Example:
//
//	exists := FirewallRuleExists("Allow-Ping")
//	if exists {
//	    fmt.Println("Rule exists")
//	}
func FirewallRuleExists(section string) bool {
	return FirewallRuleExistsWithReader(section, NewUCIFirewallConfigReader())
}

// FirewallRuleExistsWithReader checks if a firewall rule exists using the provided reader.
func FirewallRuleExistsWithReader(section string, reader ConfigReader) bool {
	_, exists := reader.Get(firewallConfigName, section, "name")

	return exists
}

// AddNetworkToZone appends a network to a firewall zone's network list if it's not already present.
//
// Parameters:
//   - zone: The UCI zone section name (e.g., "lan", "wan", "ahwlan")
//   - network: The network interface name to add (e.g., "tailscale0", "wg0")
//
// Returns an error if the zone doesn't exist or if the configuration cannot be saved.
//
// Example:
//
//	err := AddNetworkToZone("ahwlan", "tailscale0")
//	if err != nil {
//	    log.Fatalf("Failed to add network to zone: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
// If the network is already present in the zone, no action is taken.
func AddNetworkToZone(zone, network string) error {
	return AddNetworkToZoneWithReader(zone, network, NewUCIFirewallConfigReader())
}

// AddNetworkToZoneWithReader appends a network to a firewall zone using the provided reader.
func AddNetworkToZoneWithReader(zone, network string, reader ConfigReader) error {
	// Check if zone exists
	if !FirewallZoneExistsWithReader(zone, reader) {
		return fmt.Errorf("zone %q does not exist", zone)
	}

	// Get current zone configuration
	zoneConfig, err := GetFirewallZoneWithReader(zone, reader)
	if err != nil {
		return fmt.Errorf("failed to get zone config: %w", err)
	}

	// Check if network is already in the list
	for _, net := range zoneConfig.Network {
		if net == network {
			// Network already exists, no action needed
			return nil
		}
	}

	// Append the network to the list
	zoneConfig.Network = append(zoneConfig.Network, network)

	// Update the zone configuration
	if err := reader.SetType(firewallConfigName, zone, "network", uci.TypeList, zoneConfig.Network...); err != nil {
		return fmt.Errorf("failed to add network to zone: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit firewall config: %w", err)
	}

	return nil
}

// ReloadFirewall reloads the firewall configuration by executing the OpenWrt firewall init script.
// It calls the '/etc/init.d/firewall reload' command to apply firewall configuration changes
// without restarting the entire firewall subsystem.
//
// Returns an error if the reload command fails to execute or returns a non-zero exit code.
//
// Example:
//
//	err := ReloadFirewall()
//	if err != nil {
//	    log.Fatalf("Failed to reload firewall: %v", err)
//	}
func ReloadFirewall() error {
	cmd := exec.CommandContext(context.Background(), "/etc/init.d/firewall", "reload")

	return cmd.Run()
}

// RestartFirewall hard restarts the firewall service by executing the firewall init script.
// It runs the '/etc/init.d/firewall restart' command and returns an error if the
// command execution fails.
//
// Returns:
//   - error: nil if the firewall restart command succeeds, otherwise returns the error
//     from command execution
//
// Example:
//
//	err := RestartFirewall()
//	if err != nil {
//	    log.Fatalf("Failed to restart firewall: %v", err)
//	}
func RestartFirewall() error {
	cmd := exec.CommandContext(context.Background(), "/etc/init.d/firewall", "restart")

	return cmd.Run()
}
