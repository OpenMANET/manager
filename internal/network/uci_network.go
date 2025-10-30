package network

import (
	"github.com/digineo/go-uci/v2"
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
}

// UCIConfigReader wraps the uci.Get function.
type UCIConfigReader struct{}

func (r *UCIConfigReader) Get(config, section, option string) ([]string, bool) {
	return uci.Get(config, section, option)
}

// GetUCINetworkByName loads and returns the UCI network configuration by name.
func GetUCINetworkByName(name string) (*UCINetwork, error) {
	return GetUCINetworkByNameWithReader(name, &UCIConfigReader{})
}

// GetUCINetworkByNameWithReader loads and returns the UCI network configuration by name using the provided reader.
func GetUCINetworkByNameWithReader(name string, reader ConfigReader) (*UCINetwork, error) {
	var config UCINetwork

	if values, ok := reader.Get("network", name, "proto"); ok {
		config.Proto = values[0]
	}
	if values, ok := reader.Get("network", name, "netmask"); ok {
		config.NetMask = values[0]
	}
	if values, ok := reader.Get("network", name, "ipaddr"); ok {
		config.IPAddr = values[0]
	}
	if values, ok := reader.Get("network", name, "gateway"); ok {
		config.Gateway = values[0]
	}
	if values, ok := reader.Get("network", name, "dns"); ok {
		config.DNS = values[0]
	}
	if values, ok := reader.Get("network", name, "device"); ok {
		config.Device = values[0]
	}

	return &config, nil
}
