package mgmt

import (
	"github.com/mdlayher/wifi"
)

type WirelessConfig struct {
	*wifi.Client
}

// NewWirelessConfig creates and initializes a new WirelessConfig instance.
// It establishes a connection to the WiFi subsystem using the wifi client.
//
// Returns:
//   - *WirelessConfig: A pointer to the newly created WirelessConfig instance
//   - error: An error if the WiFi client initialization fails, nil otherwise
func NewWirelessConfig() (*WirelessConfig, error) {
	wifiClient, err := wifi.New()
	if err != nil {
		return nil, err
	}

	return &WirelessConfig{
		Client: wifiClient,
	}, nil
}

// Close closes the underlying netlink client connection used by the WirelessConfig.
// It returns an error if the client fails to close properly.
func (wc *WirelessConfig) Close() error {
	return wc.Client.Close()
}

// GetMeshInterfaces returns all mesh point interfaces from the wireless configuration.
// It filters the available interfaces and returns only those with InterfaceTypeMeshPoint.
// Returns an error if the underlying Interfaces() call fails.
func (wc *WirelessConfig) GetMeshInterfaces() ([]*wifi.Interface, error) {
	ifaces, err := wc.Interfaces()
	if err != nil {
		return nil, err
	}

	var meshIfaces []*wifi.Interface
	for _, iface := range ifaces {
		if iface.Type == wifi.InterfaceTypeMeshPoint {
			meshIfaces = append(meshIfaces, iface)
		}
	}

	return meshIfaces, nil
}

