package network

import (
	"fmt"
	"strconv"

	"github.com/digineo/go-uci/v2"
)

/*
config openmanet 'config'
	option dhcpconfigured '0'
	option config '/etc/openmanet/config.yml'
*/

// UCIOpenMANET represents the OpenMANET UCI configuration.
type UCIOpenMANET struct {
	DHCPConfigured string `uci:"option dhcpconfigured"`
	Config         string `uci:"option config"`
}

// OpenMANETConfigReader defines an interface for reading OpenMANET UCI configuration values.
type OpenMANETConfigReader interface {
	Get(config, section, option string) ([]string, bool)
	SetType(config, section, option string, typ uci.OptionType, values ...string) error
	Del(config, section, option string) error
	AddSection(config, section, typ string) error
	DelSection(config, section string) error
	Commit() error
}

// UCIOpenMANETConfigReader wraps the UCI functions for OpenMANET configuration.
type UCIOpenMANETConfigReader struct {
	tree uci.Tree
}

func (r *UCIOpenMANETConfigReader) Commit() error {
	return r.tree.Commit()
}

// NewUCIOpenMANETConfigReader creates a new UCI OpenMANET config reader with the default tree.
func NewUCIOpenMANETConfigReader() *UCIOpenMANETConfigReader {
	return &UCIOpenMANETConfigReader{
		tree: uci.NewTree(uci.DefaultTreePath),
	}
}

func (r *UCIOpenMANETConfigReader) Get(config, section, option string) ([]string, bool) {
	return uci.Get(config, section, option)
}

func (r *UCIOpenMANETConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return r.tree.SetType(config, section, option, typ, values...)
}

func (r *UCIOpenMANETConfigReader) Del(config, section, option string) error {
	return uci.Del(config, section, option)
}

func (r *UCIOpenMANETConfigReader) AddSection(config, section, typ string) error {
	return uci.AddSection(config, section, typ)
}

func (r *UCIOpenMANETConfigReader) DelSection(config, section string) error {
	return uci.DelSection(config, section)
}

// GetOpenMANETConfig loads and returns the OpenMANET configuration.
//
// Returns the OpenMANET configuration or an error if it cannot be read.
//
// Example:
//
//	config, err := GetOpenMANETConfig()
//	if err != nil {
//	    log.Fatalf("Failed to get OpenMANET config: %v", err)
//	}
//	fmt.Printf("Config path: %s\n", config.Config)
func GetOpenMANETConfig() (*UCIOpenMANET, error) {
	return GetOpenMANETConfigWithReader(NewUCIOpenMANETConfigReader())
}

// GetOpenMANETConfigWithReader loads and returns the OpenMANET configuration using the provided reader.
func GetOpenMANETConfigWithReader(reader OpenMANETConfigReader) (*UCIOpenMANET, error) {
	var config UCIOpenMANET

	if values, ok := reader.Get("openmanetd", "config", "dhcpconfigured"); ok && len(values) > 0 {
		config.DHCPConfigured = values[0]
	}
	if values, ok := reader.Get("openmanetd", "config", "config"); ok && len(values) > 0 {
		config.Config = values[0]
	}

	return &config, nil
}

// SetOpenMANETConfig creates or updates the OpenMANET configuration.
//
// Parameters:
//   - config: The OpenMANET configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	config := &UCIOpenMANET{
//	    DHCPConfigured: "1",
//	    Config:         "/etc/openmanet/config.yml",
//	}
//	err := SetOpenMANETConfig(config)
//
// Note: This operation requires appropriate privileges and commits the configuration.
func SetOpenMANETConfig(config *UCIOpenMANET) error {
	return SetOpenMANETConfigWithReader(config, NewUCIOpenMANETConfigReader())
}

// SetOpenMANETConfigWithReader creates or updates the OpenMANET configuration using the provided reader.
func SetOpenMANETConfigWithReader(config *UCIOpenMANET, reader OpenMANETConfigReader) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Add section if it doesn't exist (this will fail silently if it exists)
	_ = reader.AddSection("openmanetd", "config", "openmanet")

	if config.DHCPConfigured != "" {
		if err := reader.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, config.DHCPConfigured); err != nil {
			return fmt.Errorf("failed to set dhcpconfigured: %w", err)
		}
	}
	if config.Config != "" {
		if err := reader.SetType("openmanetd", "config", "config", uci.TypeOption, config.Config); err != nil {
			return fmt.Errorf("failed to set config: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit OpenMANET config: %w", err)
	}

	return nil
}

// IsDHCPConfigured checks if DHCP has been configured.
//
// Returns:
//   - true if DHCP is configured (dhcpconfigured == '1'), false otherwise
//   - An error if the configuration cannot be read
//
// Example:
//
//	configured, err := IsDHCPConfigured()
//	if err != nil {
//	    log.Fatalf("Failed to check DHCP status: %v", err)
//	}
//	if !configured {
//	    // Run DHCP configuration
//	}
func IsDHCPConfigured() (bool, error) {
	return IsDHCPConfiguredWithReader(NewUCIOpenMANETConfigReader())
}

// IsDHCPConfiguredWithReader checks if DHCP has been configured using the provided reader.
func IsDHCPConfiguredWithReader(reader OpenMANETConfigReader) (bool, error) {
	config, err := GetOpenMANETConfigWithReader(reader)
	if err != nil {
		return false, err
	}

	// Parse the dhcpconfigured value
	if config.DHCPConfigured == "0" || config.DHCPConfigured == "" {
		return false, nil
	}

	configured, err := strconv.Atoi(config.DHCPConfigured)
	if err != nil {
		return false, fmt.Errorf("invalid dhcpconfigured value: %w", err)
	}

	return configured == 1, nil
}

// SetDHCPConfigured marks DHCP as configured.
//
// This sets the 'dhcpconfigured' option to '1'.
//
// Example:
//
//	err := SetDHCPConfigured()
//	if err != nil {
//	    log.Fatalf("Failed to mark DHCP as configured: %v", err)
//	}
func SetDHCPConfigured() error {
	return SetDHCPConfiguredWithReader(NewUCIOpenMANETConfigReader())
}

// SetDHCPConfiguredWithReader marks DHCP as configured using the provided reader.
func SetDHCPConfiguredWithReader(reader OpenMANETConfigReader) error {
	// Ensure the section exists
	_ = reader.AddSection("openmanetd", "config", "openmanet")

	if err := reader.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, "1"); err != nil {
		return fmt.Errorf("failed to set dhcpconfigured: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit OpenMANET config: %w", err)
	}

	return nil
}

// ClearDHCPConfigured marks DHCP as not configured.
//
// This sets the 'dhcpconfigured' option to '0'.
//
// Example:
//
//	err := ClearDHCPConfigured()
//	if err != nil {
//	    log.Fatalf("Failed to clear DHCP configured flag: %v", err)
//	}
func ClearDHCPConfigured() error {
	return ClearDHCPConfiguredWithReader(NewUCIOpenMANETConfigReader())
}

// ClearDHCPConfiguredWithReader marks DHCP as not configured using the provided reader.
func ClearDHCPConfiguredWithReader(reader OpenMANETConfigReader) error {
	// Ensure the section exists
	_ = reader.AddSection("openmanetd", "config", "openmanet")

	if err := reader.SetType("openmanetd", "config", "dhcpconfigured", uci.TypeOption, "0"); err != nil {
		return fmt.Errorf("failed to clear dhcpconfigured: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit OpenMANET config: %w", err)
	}
	
	return nil
}

// GetConfigPath returns the path to the OpenMANET configuration file.
//
// Returns:
//   - The config file path (defaults to "/etc/openmanet/config.yml" if not set)
//   - An error if the configuration cannot be read
//
// Example:
//
//	path, err := GetConfigPath()
//	if err != nil {
//	    log.Fatalf("Failed to get config path: %v", err)
//	}
//	fmt.Printf("Config path: %s\n", path)
func GetConfigPath() (string, error) {
	return GetConfigPathWithReader(NewUCIOpenMANETConfigReader())
}

// GetConfigPathWithReader returns the path to the OpenMANET configuration file using the provided reader.
func GetConfigPathWithReader(reader OpenMANETConfigReader) (string, error) {
	config, err := GetOpenMANETConfigWithReader(reader)
	if err != nil {
		return "", err
	}

	if config.Config == "" {
		return "/etc/openmanet/config.yml", nil
	}

	return config.Config, nil
}

// SetConfigPath sets the path to the OpenMANET configuration file.
//
// Parameters:
//   - path: The path to the configuration file
//
// Example:
//
//	err := SetConfigPath("/custom/path/config.yml")
//	if err != nil {
//	    log.Fatalf("Failed to set config path: %v", err)
//	}
func SetConfigPath(path string) error {
	return SetConfigPathWithReader(path, NewUCIOpenMANETConfigReader())
}

// SetConfigPathWithReader sets the path to the OpenMANET configuration file using the provided reader.
func SetConfigPathWithReader(path string, reader OpenMANETConfigReader) error {
	if path == "" {
		return fmt.Errorf("config path cannot be empty")
	}

	// Ensure the section exists
	_ = reader.AddSection("openmanetd", "config", "openmanet")

	if err := reader.SetType("openmanetd", "config", "config", uci.TypeOption, path); err != nil {
		return fmt.Errorf("failed to set config path: %w", err)
	}
	return nil
}
