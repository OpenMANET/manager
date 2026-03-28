package network

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/digineo/go-uci/v2"
)

const (
	defaultWirelessConfigPath string = "/etc/config/wireless"
	wirelessConfigName        string = "wireless"
)

// UCIWirelessDevice represents a UCI wireless radio device (wifi-device section) configuration.
type UCIWirelessDevice struct {
	Type                   string `uci:"option type"`
	Path                   string `uci:"option path"`
	Band                   string `uci:"option band"`
	Channel                string `uci:"option channel"`
	HTMode                 string `uci:"option htmode"`
	Country                string `uci:"option country"`
	CellDensity            string `uci:"option cell_density"`
	HWMode                 string `uci:"option hwmode"`
	Reconf                 string `uci:"option reconf"`
	EnableMcastWhitelist   string `uci:"option enable_mcast_whitelist"`
	EnableMcastRateControl string `uci:"option enable_mcast_rate_control"`
	EnablePS               string `uci:"option enable_ps"`
	EnableDynamicPSOffload string `uci:"option enable_dynamic_ps_offload"`
	EnableTWT              string `uci:"option enable_twt"`
	BCF                    string `uci:"option bcf"`
	TxPower                string `uci:"option txpower"`
}

// UCIWirelessIface represents a UCI wireless interface (wifi-iface section) configuration.
type UCIWirelessIface struct {
	Device            string `uci:"option device"`
	Network           string `uci:"option network"`
	Mode              string `uci:"option mode"`
	Key               string `uci:"option key"`
	MeshID            string `uci:"option mesh_id"`
	MeshFwding        string `uci:"option mesh_fwding"`
	MeshRSSIThreshold string `uci:"option mesh_rssi_threshold"`
	Encryption        string `uci:"option encryption"`
	SSID              string `uci:"option ssid"`
	BeaconInt         string `uci:"option beacon_int"`
	Disabled          string `uci:"option disabled"`
}

// UCIWirelessConfigReader wraps the UCI functions for wireless configuration.
type UCIWirelessConfigReader struct {
	tree uci.Tree
}

// NewUCIWirelessConfigReader creates a new UCI wireless config reader with the default tree.
func NewUCIWirelessConfigReader() *UCIWirelessConfigReader {
	return &UCIWirelessConfigReader{
		tree: uci.NewTree(uci.DefaultTreePath),
	}
}

func (r *UCIWirelessConfigReader) Get(config, section, option string) ([]string, bool) {
	return r.tree.Get(config, section, option)
}

func (r *UCIWirelessConfigReader) GetSections(config, secType string) ([]string, error) {
	return r.tree.GetSections(config, secType)
}

func (r *UCIWirelessConfigReader) SetType(config, section, option string, typ uci.OptionType, values ...string) error {
	return r.tree.SetType(config, section, option, typ, values...)
}

func (r *UCIWirelessConfigReader) Del(config, section, option string) error {
	return r.tree.Del(config, section, option)
}

func (r *UCIWirelessConfigReader) AddSection(config, section, typ string) error {
	return r.tree.AddSection(config, section, typ)
}

func (r *UCIWirelessConfigReader) DelSection(config, section string) error {
	return r.tree.DelSection(config, section)
}

func (r *UCIWirelessConfigReader) Commit() error {
	return r.tree.Commit()
}

func (r *UCIWirelessConfigReader) ReloadConfig() error {
	return r.tree.LoadConfig(wirelessConfigName, true)
}

type wirelessSection struct {
	options map[string]string
	typ     string
}

// GetWirelessMeshPassphrase returns the first enabled mesh interface passphrase
// from the OpenWrt wireless UCI configuration.
func GetWirelessMeshPassphrase() (string, error) {
	return GetWirelessMeshPassphraseFromPath(defaultWirelessConfigPath)
}

// GetWirelessMeshPassphraseFromPath returns the first enabled mesh interface
// passphrase from the provided OpenWrt wireless config path.
func GetWirelessMeshPassphraseFromPath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open wireless config %s: %w", path, err)
	}
	defer file.Close()

	var (
		current      *wirelessSection
		meshFound    bool
		meshMissingK bool
	)

	finalize := func(section *wirelessSection) (string, bool) {
		if section == nil || section.typ != "wifi-iface" {
			return "", false
		}

		if strings.ToLower(strings.TrimSpace(section.options["mode"])) != "mesh" { //nolint:goconst
			return "", false
		}

		if strings.TrimSpace(section.options["disabled"]) == "1" {
			return "", false
		}

		meshFound = true

		key := strings.TrimSpace(section.options["key"])
		if key == "" {
			meshMissingK = true

			return "", false
		}

		return key, true
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if typ, _, ok := parseUCIConfigLine(line); ok {
			if key, found := finalize(current); found {
				return key, nil
			}

			current = &wirelessSection{
				typ:     typ,
				options: map[string]string{},
			}

			continue
		}

		if current == nil {
			continue
		}

		if option, value, ok := parseUCIOptionLine(line); ok {
			current.options[option] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read wireless config %s: %w", path, err)
	}

	if key, found := finalize(current); found {
		return key, nil
	}

	if !meshFound {
		return "", fmt.Errorf("no enabled mesh wifi-iface section found in %s", path)
	}

	if meshMissingK {
		return "", fmt.Errorf("enabled mesh wifi-iface section missing key in %s", path)
	}

	return "", fmt.Errorf("mesh key not found in %s", path)
}

func parseUCIConfigLine(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "config" {
		return "", "", false
	}

	typ := trimUCIValue(fields[1])

	name := ""
	if len(fields) > 2 {
		name = trimUCIValue(fields[2])
	}

	return typ, name, true
}

func parseUCIOptionLine(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "option") {
		return "", "", false
	}

	if len(line) > len("option") {
		next := line[len("option")]
		if next != ' ' && next != '\t' {
			return "", "", false
		}
	}

	rest := strings.TrimSpace(strings.TrimPrefix(line, "option"))
	if rest == "" {
		return "", "", false
	}

	spaceIdx := strings.IndexAny(rest, " \t")
	if spaceIdx == -1 {
		return "", "", false
	}

	name := strings.TrimSpace(rest[:spaceIdx])
	if name == "" {
		return "", "", false
	}

	value := trimUCIValue(strings.TrimSpace(rest[spaceIdx+1:]))

	return name, value, true
}

func trimUCIValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return v
	}

	first := v[0]
	last := v[len(v)-1]

	if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
		return v[1 : len(v)-1]
	}

	return v
}

// GetWirelessDeviceByName loads and returns the UCI wireless device configuration by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "radio0", "radio1")
//
// Returns the device configuration or an error if it cannot be read.
//
// Example:
//
//	cfg, err := GetWirelessDeviceByName("radio0")
//	if err != nil {
//	    log.Fatalf("Failed to get wireless device: %v", err)
//	}
//	fmt.Printf("Band: %s, Channel: %s\n", cfg.Band, cfg.Channel)
func GetWirelessDeviceByName(name string) (*UCIWirelessDevice, error) {
	return GetWirelessDeviceByNameWithReader(name, NewUCIWirelessConfigReader())
}

// GetWirelessDeviceByNameWithReader loads and returns the UCI wireless device configuration by
// name using the provided reader.
func GetWirelessDeviceByNameWithReader(name string, reader ConfigReader) (*UCIWirelessDevice, error) { //nolint:gocyclo,gocognit
	config := &UCIWirelessDevice{}

	if values, ok := reader.Get(wirelessConfigName, name, "type"); ok && len(values) > 0 {
		config.Type = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "path"); ok && len(values) > 0 {
		config.Path = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "band"); ok && len(values) > 0 {
		config.Band = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "channel"); ok && len(values) > 0 {
		config.Channel = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "htmode"); ok && len(values) > 0 {
		config.HTMode = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "country"); ok && len(values) > 0 {
		config.Country = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "cell_density"); ok && len(values) > 0 {
		config.CellDensity = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "hwmode"); ok && len(values) > 0 {
		config.HWMode = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "reconf"); ok && len(values) > 0 {
		config.Reconf = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "enable_mcast_whitelist"); ok && len(values) > 0 {
		config.EnableMcastWhitelist = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "enable_mcast_rate_control"); ok && len(values) > 0 {
		config.EnableMcastRateControl = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "enable_ps"); ok && len(values) > 0 {
		config.EnablePS = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "enable_dynamic_ps_offload"); ok && len(values) > 0 {
		config.EnableDynamicPSOffload = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "enable_twt"); ok && len(values) > 0 {
		config.EnableTWT = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "bcf"); ok && len(values) > 0 {
		config.BCF = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "txpower"); ok && len(values) > 0 {
		config.TxPower = values[0]
	}

	return config, nil
}

// GetWirelessIfaceByName loads and returns the UCI wireless interface configuration by name.
//
// Parameters:
//   - name: The UCI section name (e.g., "default_radio0", "mesh0")
//
// Returns the interface configuration or an error if it cannot be read.
//
// Example:
//
//	cfg, err := GetWirelessIfaceByName("default_radio0")
//	if err != nil {
//	    log.Fatalf("Failed to get wireless iface: %v", err)
//	}
//	fmt.Printf("Mode: %s, Encryption: %s\n", cfg.Mode, cfg.Encryption)
func GetWirelessIfaceByName(name string) (*UCIWirelessIface, error) {
	return GetWirelessIfaceByNameWithReader(name, NewUCIWirelessConfigReader())
}

// GetWirelessIfaceByNameWithReader loads and returns the UCI wireless interface configuration by
// name using the provided reader.
func GetWirelessIfaceByNameWithReader(name string, reader ConfigReader) (*UCIWirelessIface, error) {
	config := &UCIWirelessIface{}

	if values, ok := reader.Get(wirelessConfigName, name, "device"); ok && len(values) > 0 {
		config.Device = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "network"); ok && len(values) > 0 {
		config.Network = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "mode"); ok && len(values) > 0 {
		config.Mode = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "key"); ok && len(values) > 0 {
		config.Key = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "mesh_id"); ok && len(values) > 0 {
		config.MeshID = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "mesh_fwding"); ok && len(values) > 0 {
		config.MeshFwding = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "mesh_rssi_threshold"); ok && len(values) > 0 {
		config.MeshRSSIThreshold = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "encryption"); ok && len(values) > 0 {
		config.Encryption = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "ssid"); ok && len(values) > 0 {
		config.SSID = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "beacon_int"); ok && len(values) > 0 {
		config.BeaconInt = values[0]
	}

	if values, ok := reader.Get(wirelessConfigName, name, "disabled"); ok && len(values) > 0 {
		config.Disabled = values[0]
	}

	return config, nil
}

// SetWirelessDeviceConfig creates or updates a wireless radio device configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "radio0", "radio1")
//   - config: The wireless device configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	cfg := &UCIWirelessDevice{
//	    Type:    "mac80211",
//	    Band:    "2g",
//	    Channel: "6",
//	    HTMode:  "HT20",
//	    Country: "US",
//	}
//	err := SetWirelessDeviceConfig("radio0", cfg)
func SetWirelessDeviceConfig(section string, config *UCIWirelessDevice) error {
	return SetWirelessDeviceConfigWithReader(section, config, NewUCIWirelessConfigReader())
}

// SetWirelessDeviceConfigWithReader creates or updates a wireless device configuration using
// the provided reader.
func SetWirelessDeviceConfigWithReader(section string, config *UCIWirelessDevice, reader ConfigReader) error { //nolint:gocognit,gocyclo
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	_ = reader.AddSection(wirelessConfigName, section, "wifi-device")

	if config.Type != "" {
		if err := reader.SetType(wirelessConfigName, section, "type", uci.TypeOption, config.Type); err != nil {
			return fmt.Errorf("failed to set type: %w", err)
		}
	}

	if config.Path != "" {
		if err := reader.SetType(wirelessConfigName, section, "path", uci.TypeOption, config.Path); err != nil {
			return fmt.Errorf("failed to set path: %w", err)
		}
	}

	if config.Band != "" {
		if err := reader.SetType(wirelessConfigName, section, "band", uci.TypeOption, config.Band); err != nil {
			return fmt.Errorf("failed to set band: %w", err)
		}
	}

	if config.Channel != "" {
		if err := reader.SetType(wirelessConfigName, section, "channel", uci.TypeOption, config.Channel); err != nil {
			return fmt.Errorf("failed to set channel: %w", err)
		}
	}

	if config.HTMode != "" {
		if err := reader.SetType(wirelessConfigName, section, "htmode", uci.TypeOption, config.HTMode); err != nil {
			return fmt.Errorf("failed to set htmode: %w", err)
		}
	}

	if config.Country != "" {
		if err := reader.SetType(wirelessConfigName, section, "country", uci.TypeOption, config.Country); err != nil {
			return fmt.Errorf("failed to set country: %w", err)
		}
	}

	if config.CellDensity != "" {
		if err := reader.SetType(wirelessConfigName, section, "cell_density", uci.TypeOption, config.CellDensity); err != nil {
			return fmt.Errorf("failed to set cell_density: %w", err)
		}
	}

	if config.HWMode != "" {
		if err := reader.SetType(wirelessConfigName, section, "hwmode", uci.TypeOption, config.HWMode); err != nil {
			return fmt.Errorf("failed to set hwmode: %w", err)
		}
	}

	if config.Reconf != "" {
		if err := reader.SetType(wirelessConfigName, section, "reconf", uci.TypeOption, config.Reconf); err != nil {
			return fmt.Errorf("failed to set reconf: %w", err)
		}
	}

	if config.EnableMcastWhitelist != "" {
		if err := reader.SetType(wirelessConfigName, section, "enable_mcast_whitelist", uci.TypeOption, config.EnableMcastWhitelist); err != nil {
			return fmt.Errorf("failed to set enable_mcast_whitelist: %w", err)
		}
	}

	if config.EnableMcastRateControl != "" {
		if err := reader.SetType(wirelessConfigName, section, "enable_mcast_rate_control", uci.TypeOption, config.EnableMcastRateControl); err != nil {
			return fmt.Errorf("failed to set enable_mcast_rate_control: %w", err)
		}
	}

	if config.EnablePS != "" {
		if err := reader.SetType(wirelessConfigName, section, "enable_ps", uci.TypeOption, config.EnablePS); err != nil {
			return fmt.Errorf("failed to set enable_ps: %w", err)
		}
	}

	if config.EnableDynamicPSOffload != "" {
		if err := reader.SetType(wirelessConfigName, section, "enable_dynamic_ps_offload", uci.TypeOption, config.EnableDynamicPSOffload); err != nil {
			return fmt.Errorf("failed to set enable_dynamic_ps_offload: %w", err)
		}
	}

	if config.EnableTWT != "" {
		if err := reader.SetType(wirelessConfigName, section, "enable_twt", uci.TypeOption, config.EnableTWT); err != nil {
			return fmt.Errorf("failed to set enable_twt: %w", err)
		}
	}

	if config.BCF != "" {
		if err := reader.SetType(wirelessConfigName, section, "bcf", uci.TypeOption, config.BCF); err != nil {
			return fmt.Errorf("failed to set bcf: %w", err)
		}
	}

	if config.TxPower != "" {
		if err := reader.SetType(wirelessConfigName, section, "txpower", uci.TypeOption, config.TxPower); err != nil {
			return fmt.Errorf("failed to set txpower: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit wireless config: %w", err)
	}

	return nil
}

// SetWirelessIfaceConfig creates or updates a wireless interface configuration.
//
// Parameters:
//   - section: The UCI section name (e.g., "default_radio0", "mesh0")
//   - config: The wireless interface configuration to set
//
// Returns an error if the configuration cannot be saved.
//
// Example:
//
//	cfg := &UCIWirelessIface{
//	    Device:     "radio0",
//	    Network:    "batmesh0",
//	    Mode:       "mesh",
//	    MeshID:     "mymesh",
//	    Encryption: "sae",
//	    Key:        "mysecretkey",
//	}
//	err := SetWirelessIfaceConfig("mesh0", cfg)
func SetWirelessIfaceConfig(section string, config *UCIWirelessIface) error {
	return SetWirelessIfaceConfigWithReader(section, config, NewUCIWirelessConfigReader())
}

// SetWirelessIfaceConfigWithReader creates or updates a wireless interface configuration using
// the provided reader.
func SetWirelessIfaceConfigWithReader(section string, config *UCIWirelessIface, reader ConfigReader) error { //nolint:gocognit
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	_ = reader.AddSection(wirelessConfigName, section, "wifi-iface")

	if config.Device != "" {
		if err := reader.SetType(wirelessConfigName, section, "device", uci.TypeOption, config.Device); err != nil {
			return fmt.Errorf("failed to set device: %w", err)
		}
	}

	if config.Network != "" {
		if err := reader.SetType(wirelessConfigName, section, "network", uci.TypeOption, config.Network); err != nil {
			return fmt.Errorf("failed to set network: %w", err)
		}
	}

	if config.Mode != "" {
		if err := reader.SetType(wirelessConfigName, section, "mode", uci.TypeOption, config.Mode); err != nil {
			return fmt.Errorf("failed to set mode: %w", err)
		}
	}

	if config.Key != "" {
		if err := reader.SetType(wirelessConfigName, section, "key", uci.TypeOption, config.Key); err != nil {
			return fmt.Errorf("failed to set key: %w", err)
		}
	}

	if config.MeshID != "" {
		if err := reader.SetType(wirelessConfigName, section, "mesh_id", uci.TypeOption, config.MeshID); err != nil {
			return fmt.Errorf("failed to set mesh_id: %w", err)
		}
	}

	if config.MeshFwding != "" {
		if err := reader.SetType(wirelessConfigName, section, "mesh_fwding", uci.TypeOption, config.MeshFwding); err != nil {
			return fmt.Errorf("failed to set mesh_fwding: %w", err)
		}
	}

	if config.MeshRSSIThreshold != "" {
		if err := reader.SetType(wirelessConfigName, section, "mesh_rssi_threshold", uci.TypeOption, config.MeshRSSIThreshold); err != nil {
			return fmt.Errorf("failed to set mesh_rssi_threshold: %w", err)
		}
	}

	if config.Encryption != "" {
		if err := reader.SetType(wirelessConfigName, section, "encryption", uci.TypeOption, config.Encryption); err != nil {
			return fmt.Errorf("failed to set encryption: %w", err)
		}
	}

	if config.SSID != "" {
		if err := reader.SetType(wirelessConfigName, section, "ssid", uci.TypeOption, config.SSID); err != nil {
			return fmt.Errorf("failed to set ssid: %w", err)
		}
	}

	if config.BeaconInt != "" {
		if err := reader.SetType(wirelessConfigName, section, "beacon_int", uci.TypeOption, config.BeaconInt); err != nil {
			return fmt.Errorf("failed to set beacon_int: %w", err)
		}
	}

	if config.Disabled != "" {
		if err := reader.SetType(wirelessConfigName, section, "disabled", uci.TypeOption, config.Disabled); err != nil {
			return fmt.Errorf("failed to set disabled: %w", err)
		}
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit wireless config: %w", err)
	}

	return nil
}

// DeleteWirelessDevice removes a wireless radio device (wifi-device) configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "radio0", "radio1")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteWirelessDevice("radio0")
func DeleteWirelessDevice(section string) error {
	return DeleteWirelessDeviceWithReader(section, NewUCIWirelessConfigReader())
}

// DeleteWirelessDeviceWithReader removes a wireless device configuration section using the
// provided reader.
func DeleteWirelessDeviceWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(wirelessConfigName, section); err != nil {
		return fmt.Errorf("failed to delete wireless device section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit wireless config: %w", err)
	}

	return nil
}

// DeleteWirelessIface removes a wireless interface (wifi-iface) configuration section.
//
// Parameters:
//   - section: The UCI section name to delete (e.g., "default_radio0", "mesh0")
//
// Returns an error if the section cannot be deleted.
//
// Example:
//
//	err := DeleteWirelessIface("default_radio0")
func DeleteWirelessIface(section string) error {
	return DeleteWirelessIfaceWithReader(section, NewUCIWirelessConfigReader())
}

// DeleteWirelessIfaceWithReader removes a wireless interface configuration section using the
// provided reader.
func DeleteWirelessIfaceWithReader(section string, reader ConfigReader) error {
	if err := reader.DelSection(wirelessConfigName, section); err != nil {
		return fmt.Errorf("failed to delete wireless iface section: %w", err)
	}

	if err := reader.Commit(); err != nil {
		return fmt.Errorf("failed to commit wireless config: %w", err)
	}

	return nil
}
