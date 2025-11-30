package network

import (
	"fmt"
	"net"
	"sort"
	"strconv"

	"github.com/digineo/go-uci/v2"
	"github.com/openmanet/go-alfred"
	proto "github.com/openmanet/openmanetd/internal/api/openmanet/v1"
)

// UCIDnsmasq represents the dnsmasq global configuration section.
type UCIDnsmasq struct {
	DomainNeeded    string `uci:"option domainneeded"`
	LocaliseQueries string `uci:"option localise_queries"`
	RebindLocalhost string `uci:"option rebind_localhost"`
	Local           string `uci:"option local"`
	Domain          string `uci:"option domain"`
	ExpandHosts     string `uci:"option expandhosts"`
	CacheSize       string `uci:"option cachesize"`
	Authoritative   string `uci:"option authoritative"`
	ReadEthers      string `uci:"option readethers"`
	LocalService    string `uci:"option localservice"`
	EdnsPacketMax   string `uci:"option ednspacket_max"`
}

// UCIDHCP represents a DHCP pool configuration.
type UCIDHCP struct {
	Interface  string `uci:"option interface"`
	Start      string `uci:"option start"`
	Limit      string `uci:"option limit"`
	LeaseTime  string `uci:"option leasetime"`
	Ignore     string `uci:"option ignore"`
	DHCPOption string `uci:"list dhcp_option"`
	Ra         string `uci:"option ra"`
	RaDefault  string `uci:"option ra_default"`
	Force      string `uci:"option force"`
}

// DHCPConfigReader defines an interface for reading DHCP UCI configuration values.
type DHCPConfigReader interface {
	Get(config, section, option string) ([]string, bool)
	SetType(config, section, option string, typ uci.OptionType, values ...string) error
	Del(config, section, option string) error
	AddSection(config, section, typ string) error
	DelSection(config, section string) error
	Commit() error
	ReloadConfig() error
}

// UCIDHCPConfigReader wraps the UCI functions for DHCP configuration.
type UCIDHCPConfigReader struct {
	tree uci.Tree
}

// Commit commits the current configuration changes to UCI.
func (r *UCIDHCPConfigReader) Commit() error {
	return r.tree.Commit()
}

func (r *UCIDHCPConfigReader) ReloadConfig() error {
	return r.tree.LoadConfig("dhcp", true)
}

// NewUCIDHCPConfigReader creates a new UCI DHCP config reader with the default tree.
func NewUCIDHCPConfigReader() *UCIDHCPConfigReader {
	return &UCIDHCPConfigReader{
		tree: uci.NewTree(uci.DefaultTreePath),
	}
}

func (r *UCIDHCPConfigReader) Get(config, section, option string) ([]string, bool) {
	return uci.Get(config, section, option)
}

func (r *UCIDHCPConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return r.tree.SetType(config, section, option, typ, values...)
}

func (r *UCIDHCPConfigReader) Del(config, section, option string) error {
	return uci.Del(config, section, option)
}

func (r *UCIDHCPConfigReader) AddSection(config, section, typ string) error {
	return uci.AddSection(config, section, typ)
}

func (r *UCIDHCPConfigReader) DelSection(config, section string) error {
	return uci.DelSection(config, section)
}

// GetDnsmasqConfig loads and returns the dnsmasq global configuration.
func GetDnsmasqConfig() (*UCIDnsmasq, error) {
	return GetDnsmasqConfigWithReader(NewUCIDHCPConfigReader())
}

// GetDnsmasqConfigWithReader loads and returns the dnsmasq configuration using the provided reader.
func GetDnsmasqConfigWithReader(reader DHCPConfigReader) (*UCIDnsmasq, error) {
	var config UCIDnsmasq

	if err := reader.ReloadConfig(); err != nil {
		return nil, fmt.Errorf("failed to reload dnsmasq config: %w", err)
	}

	if values, ok := reader.Get("dhcp", "dnsmasq", "domainneeded"); ok && len(values) > 0 {
		config.DomainNeeded = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "localise_queries"); ok && len(values) > 0 {
		config.LocaliseQueries = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "rebind_localhost"); ok && len(values) > 0 {
		config.RebindLocalhost = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "local"); ok && len(values) > 0 {
		config.Local = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "domain"); ok && len(values) > 0 {
		config.Domain = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "expandhosts"); ok && len(values) > 0 {
		config.ExpandHosts = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "cachesize"); ok && len(values) > 0 {
		config.CacheSize = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "authoritative"); ok && len(values) > 0 {
		config.Authoritative = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "readethers"); ok && len(values) > 0 {
		config.ReadEthers = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "localservice"); ok && len(values) > 0 {
		config.LocalService = values[0]
	}
	if values, ok := reader.Get("dhcp", "dnsmasq", "ednspacket_max"); ok && len(values) > 0 {
		config.EdnsPacketMax = values[0]
	}

	return &config, nil
}

// GetDHCPConfig loads and returns the DHCP pool configuration by section name.
func GetDHCPConfig(section string) (*UCIDHCP, error) {
	return GetDHCPConfigWithReader(section, NewUCIDHCPConfigReader())
}

// GetDHCPConfigWithReader loads and returns the DHCP pool configuration using the provided reader.
func GetDHCPConfigWithReader(section string, reader DHCPConfigReader) (*UCIDHCP, error) {
	var config UCIDHCP

	if err := reader.ReloadConfig(); err != nil {
		return nil, fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	if values, ok := reader.Get("dhcp", section, "interface"); ok && len(values) > 0 {
		config.Interface = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "start"); ok && len(values) > 0 {
		config.Start = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "limit"); ok && len(values) > 0 {
		config.Limit = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "leasetime"); ok && len(values) > 0 {
		config.LeaseTime = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "ignore"); ok && len(values) > 0 {
		config.Ignore = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "dhcp_option"); ok && len(values) > 0 {
		config.DHCPOption = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "ra"); ok && len(values) > 0 {
		config.Ra = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "ra_default"); ok && len(values) > 0 {
		config.RaDefault = values[0]
	}
	if values, ok := reader.Get("dhcp", section, "force"); ok && len(values) > 0 {
		config.Force = values[0]
	}

	return &config, nil
}

// SetDHCPConfig creates or updates a DHCP pool configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan", "ahwlan")
//   - config: The DHCP configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	dhcpConfig := &UCIDHCP{
//	    Interface: "lan",
//	    Start:     "100",
//	    Limit:     "150",
//	    LeaseTime: "12h",
//	}
//	err := SetDHCPConfig("lan", dhcpConfig)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetDHCPConfig(section string, config *UCIDHCP) error {
	return SetDHCPConfigWithReader(section, config, NewUCIDHCPConfigReader())
}

// SetDHCPConfigWithReader creates or updates a DHCP pool configuration using the provided reader.
func SetDHCPConfigWithReader(section string, config *UCIDHCP, reader DHCPConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Add section if it doesn't exist (this will fail silently if it exists)
	_ = reader.AddSection("dhcp", section, "dhcp")

	if config.Interface != "" {
		if err := reader.SetType("dhcp", section, "interface", uci.TypeOption, config.Interface); err != nil {
			return fmt.Errorf("failed to set interface: %w", err)
		}
	}
	if config.Start != "" {
		if err := reader.SetType("dhcp", section, "start", uci.TypeOption, config.Start); err != nil {
			return fmt.Errorf("failed to set start: %w", err)
		}
	}
	if config.Limit != "" {
		if err := reader.SetType("dhcp", section, "limit", uci.TypeOption, config.Limit); err != nil {
			return fmt.Errorf("failed to set limit: %w", err)
		}
	}
	if config.LeaseTime != "" {
		if err := reader.SetType("dhcp", section, "leasetime", uci.TypeOption, config.LeaseTime); err != nil {
			return fmt.Errorf("failed to set leasetime: %w", err)
		}
	}
	if config.Ignore != "" {
		if err := reader.SetType("dhcp", section, "ignore", uci.TypeOption, config.Ignore); err != nil {
			return fmt.Errorf("failed to set ignore: %w", err)
		}
	}
	if config.DHCPOption != "" {
		if err := reader.SetType("dhcp", section, "dhcp_option", uci.TypeOption, config.DHCPOption); err != nil {
			return fmt.Errorf("failed to set dhcp_option: %w", err)
		}
	}
	if config.Ra != "" {
		if err := reader.SetType("dhcp", section, "ra", uci.TypeOption, config.Ra); err != nil {
			return fmt.Errorf("failed to set ra: %w", err)
		}
	}
	if config.RaDefault != "" {
		if err := reader.SetType("dhcp", section, "ra_default", uci.TypeOption, config.RaDefault); err != nil {
			return fmt.Errorf("failed to set ra_default: %w", err)
		}
	}
	if config.Force != "" {
		if err := reader.SetType("dhcp", section, "force", uci.TypeOption, config.Force); err != nil {
			return fmt.Errorf("failed to set force: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit DHCP config: %w", err)
	}

	if err := reader.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	return nil
}

// DeleteDHCPConfig removes a DHCP pool configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "lan", "wan")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteDHCPConfig("guest")
//	if err != nil {
//	    log.Fatalf("Failed to delete DHCP config: %v", err)
//	}
//
// Note: This operation requires appropriate privileges and commits the configuration.
func DeleteDHCPConfig(section string) error {
	return DeleteDHCPConfigWithReader(section, NewUCIDHCPConfigReader())
}

// DeleteDHCPConfigWithReader removes a DHCP pool configuration section using the provided reader.
func DeleteDHCPConfigWithReader(section string, reader DHCPConfigReader) error {
	if err := reader.DelSection("dhcp", section); err != nil {
		return fmt.Errorf("failed to delete DHCP section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit DHCP config: %w", err)
	}

	if err := reader.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	return nil
}

// EnableDHCP enables DHCP on the specified interface section.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//
// This sets the 'ignore' option to '0', enabling DHCP service.
func EnableDHCP(section string) error {
	return EnableDHCPWithReader(section, NewUCIDHCPConfigReader())
}

// EnableDHCPWithReader enables DHCP using the provided reader.
func EnableDHCPWithReader(section string, reader DHCPConfigReader) error {
	if err := reader.SetType("dhcp", section, "ignore", uci.TypeOption, "0"); err != nil {
		return fmt.Errorf("failed to enable DHCP: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit DHCP config: %w", err)
	}

	if err := reader.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	return nil
}

// DisableDHCP disables DHCP on the specified interface section.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//
// This sets the 'ignore' option to '1', disabling DHCP service.
func DisableDHCP(section string) error {
	return DisableDHCPWithReader(section, NewUCIDHCPConfigReader())
}

// DisableDHCPWithReader disables DHCP using the provided reader.
func DisableDHCPWithReader(section string, reader DHCPConfigReader) error {
	if err := reader.SetType("dhcp", section, "ignore", uci.TypeOption, "1"); err != nil {
		return fmt.Errorf("failed to disable DHCP: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit DHCP config: %w", err)
	}

	if err := reader.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	return nil
}

// IsDHCPEnabled checks if DHCP is enabled for the specified section.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan", "wan")
//
// Returns:
//   - true if DHCP is enabled (ignore != '1'), false otherwise
//   - An error if the configuration cannot be read
func IsDHCPEnabled(section string) (bool, error) {
	return IsDHCPEnabledWithReader(section, NewUCIDHCPConfigReader())
}

// IsDHCPEnabledWithReader checks if DHCP is enabled using the provided reader.
func IsDHCPEnabledWithReader(section string, reader DHCPConfigReader) (bool, error) {
	config, err := GetDHCPConfigWithReader(section, reader)
	if err != nil {
		return false, err
	}

	// DHCP is enabled if 'ignore' is not set or is set to '0'
	return config.Ignore != "1", nil
}

// SetDHCPRange sets the DHCP address range for the specified section.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan")
//   - start: The starting address offset (e.g., "100")
//   - limit: The maximum number of addresses to assign (e.g., "150")
//
// Example:
//
//	err := SetDHCPRange("lan", "100", "150")
//	// This will assign addresses from .100 to .249 (100 + 150 - 1)
func SetDHCPRange(section, start, limit string) error {
	return SetDHCPRangeWithReader(section, start, limit, NewUCIDHCPConfigReader())
}

// SetDHCPRangeWithReader sets the DHCP range using the provided reader.
func SetDHCPRangeWithReader(section, start, limit string, reader DHCPConfigReader) error {
	// Validate that start and limit are numeric
	if _, err := strconv.Atoi(start); err != nil {
		return fmt.Errorf("start must be a number: %w", err)
	}
	if _, err := strconv.Atoi(limit); err != nil {
		return fmt.Errorf("limit must be a number: %w", err)
	}

	if err := reader.SetType("dhcp", section, "start", uci.TypeOption, start); err != nil {
		return fmt.Errorf("failed to set start: %w", err)
	}
	if err := reader.SetType("dhcp", section, "limit", uci.TypeOption, limit); err != nil {
		return fmt.Errorf("failed to set limit: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit DHCP config: %w", err)
	}

	if err := reader.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	return nil
}

// SetDHCPLeaseTime sets the lease time for DHCP addresses.
//
// Parameters:
//   - section: The UCI section name (e.g., "lan")
//   - leasetime: The lease time (e.g., "12h", "3600", "infinite")
//
// Example:
//
//	err := SetDHCPLeaseTime("lan", "12h")
func SetDHCPLeaseTime(section, leasetime string) error {
	return SetDHCPLeaseTimeWithReader(section, leasetime, NewUCIDHCPConfigReader())
}

// SetDHCPLeaseTimeWithReader sets the lease time using the provided reader.
func SetDHCPLeaseTimeWithReader(section, leasetime string, reader DHCPConfigReader) error {
	if err := reader.SetType("dhcp", section, "leasetime", uci.TypeOption, leasetime); err != nil {
		return fmt.Errorf("failed to set leasetime: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit DHCP config: %w", err)
	}

	if err := reader.ReloadConfig(); err != nil {
		return fmt.Errorf("failed to reload DHCP config: %w", err)
	}

	return nil
}

// DHCPRange represents an allocated DHCP address range.
type DHCPRange struct {
	Start int // Starting offset
	End   int // Ending offset (Start + Limit - 1)
}

// CalculateAvailableDHCPStart analyzes address reservation records and calculates
// a non-conflicting DHCP start address within the network range.
//
// Parameters:
//   - records: Array of Alfred records containing address reservations
//   - networkAddr: Network address (e.g., "10.41.0.0")
//   - subnetMask: Subnet mask (e.g., "255.255.0.0")
//   - desiredLimit: The desired DHCP limit (number of addresses)
//
// Returns:
//   - The calculated DHCP start offset
//   - An error if no suitable range can be found
//
// Example:
//
//	records := []alfred.Record{ /* ... */ }
//	start, err := CalculateAvailableDHCPStart(records, "10.41.0.0", "255.255.0.0", 150)
//	if err != nil {
//	    log.Fatalf("Failed to calculate DHCP start: %v", err)
//	}
//	fmt.Printf("Use DHCP start: %d\n", start)
//
// Note: This function accounts for existing DHCP ranges to prevent conflicts.
// It attempts to find the lowest available start address that can accommodate
// the desired limit without overlapping with existing ranges.
func CalculateAvailableDHCPStart(records []alfred.Record, networkAddr, subnetMask string, desiredLimit int) (int, error) {
	if desiredLimit <= 0 {
		return 0, fmt.Errorf("desiredLimit must be greater than 0")
	}

	// Parse network address and subnet mask
	ip := net.ParseIP(networkAddr)
	if ip == nil {
		return 0, fmt.Errorf("invalid network address: %s", networkAddr)
	}
	ip = ip.To4()
	if ip == nil {
		return 0, fmt.Errorf("network address must be IPv4: %s", networkAddr)
	}

	mask := net.ParseIP(subnetMask)
	if mask == nil {
		return 0, fmt.Errorf("invalid subnet mask: %s", subnetMask)
	}
	mask = mask.To4()
	if mask == nil {
		return 0, fmt.Errorf("subnet mask must be IPv4: %s", subnetMask)
	}

	// Calculate network size (number of available host addresses)
	// This calculates the total number of addresses in the subnet
	ones, bits := net.IPMask(mask).Size()
	if bits != 32 {
		return 0, fmt.Errorf("invalid subnet mask")
	}
	networkSize := (1 << uint(bits-ones)) - 2 // Subtract network and broadcast addresses

	if networkSize <= 0 {
		return 0, fmt.Errorf("network size too small")
	}

	// Collect existing DHCP ranges from records
	var existingRanges []DHCPRange
	for _, record := range records {
		var addrRes proto.AddressReservation
		if err := addrRes.UnmarshalVT(record.Data); err != nil {
			// Skip records that can't be unmarshaled
			continue
		}

		// Parse start and limit
		start, err := strconv.Atoi(addrRes.UciDhcpStart)
		if err != nil {
			// Skip invalid start values
			continue
		}

		limit, err := strconv.Atoi(addrRes.UciDhcpLimit)
		if err != nil {
			// Skip invalid limit values
			continue
		}

		if start > 0 && limit > 0 {
			existingRanges = append(existingRanges, DHCPRange{
				Start: start,
				End:   start + limit - 1,
			})
		}
	}

	// Sort ranges by start address for easier conflict detection
	sort.Slice(existingRanges, func(i, j int) bool {
		return existingRanges[i].Start < existingRanges[j].Start
	})

	// Find the first available gap that can fit our desired range
	// Start from offset 1 (we typically don't use offset 0, which would be the network address + 1)
	// In practice, many networks start DHCP at offset 100 or similar
	candidate := 100 // Start with a reasonable default offset

	// Try to find a non-conflicting range
	for candidate+desiredLimit-1 <= networkSize {
		conflictFound := false
		proposedEnd := candidate + desiredLimit - 1

		for _, existing := range existingRanges {
			// Check if our proposed range overlaps with this existing range
			if rangesOverlap(candidate, proposedEnd, existing.Start, existing.End) {
				// Move candidate past this existing range
				candidate = existing.End + 1
				conflictFound = true
				break
			}
		}

		if !conflictFound {
			// Found a suitable range
			return candidate, nil
		}
	}

	// If we couldn't find a gap starting from 100, try from offset 1
	candidate = 1
	for candidate+desiredLimit-1 <= networkSize {
		conflictFound := false
		proposedEnd := candidate + desiredLimit - 1

		for _, existing := range existingRanges {
			if rangesOverlap(candidate, proposedEnd, existing.Start, existing.End) {
				candidate = existing.End + 1
				conflictFound = true
				break
			}
		}

		if !conflictFound {
			return candidate, nil
		}
	}

	return 0, fmt.Errorf("no available DHCP range found for limit %d within network size %d", desiredLimit, networkSize)
}

// rangesOverlap checks if two ranges overlap.
func rangesOverlap(start1, end1, start2, end2 int) bool {
	return start1 <= end2 && start2 <= end1
}
