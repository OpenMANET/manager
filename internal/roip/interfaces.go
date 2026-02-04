package roip

import (
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	defaultTunnelDeviceName    string = "tailscale0"
	defaultVxLanDeviceName     string = "vxlan0"
	defaultBatmanInterfaceName string = "battunnel0"
)

// createOrConfigureTunnelInterface creates or configures a tunnel interface in the UCI network configuration.
// It checks if a tunnel interface with the default device name already exists in the UCI configuration.
// If the interface doesn't exist, it creates a new network section with protocol set to "none" and the
// default tunnel device name. Returns an error if the network configuration operation fails.
func (r *ROIP) createOrConfigureTunnelInterface() error {
	// Check if the tunnel interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultTunnelDeviceName, r.uciNetworkConfig) {

		// Create a new network section for the tunnel interface
		if err := network.SetNetworkConfigWithReader(defaultTunnelDeviceName, &network.UCINetwork{
			Proto:  "none",
			Device: defaultTunnelDeviceName,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		r.Logger.Debug().Msgf("Created ROIP tunnel interface %s", defaultTunnelDeviceName)
	}

	return nil
}

// createOrConfigureVxLanInterface ensures that a VXLAN interface exists in the UCI network configuration.
// It checks if a network section with the default VXLAN device name already exists. If not, it creates
// a new VXLAN network section with the following configuration:
//   - Proto: "vxlan" - sets the protocol type to VXLAN
//   - Learning: "1" - enables MAC address learning
//   - Tunlink: defaultTunnelDeviceName - links the VXLAN to the tunnel device
//   - Proxy: "1" - enables ARP proxying
//
// Returns an error if the VXLAN configuration creation fails, otherwise returns nil.
func (r *ROIP) createOrConfigureVxLanInterface() error {
	// Check if the VXLAN interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultVxLanDeviceName, r.uciNetworkConfig) {
		// Create a new network section for the VXLAN interface
		if err := network.SetVXLANConfigWithReader(defaultVxLanDeviceName, &network.UCIVXLANConfig{
			Proto:    "vxlan",
			Learning: "1",
			Tunlink:  defaultTunnelDeviceName,
			Proxy:    "1",
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		r.Logger.Debug().Msgf("Created ROIP VXLAN interface %s", defaultVxLanDeviceName)
	}

	return nil
}

// createOrConfigureBatmanInterface creates or configures a Batman (Better Approach To Mobile Ad-hoc Networking)
// interface for ROIP (Radio Over IP). It checks if a Batman interface with the default name already exists
// in the UCI (Unified Configuration Interface) network configuration. If the interface does not exist, it creates
// a new network section configured as a batadv_hardif (Batman-adv hard interface) that uses the default VXLAN
// device and associates it with the configured mesh network interface. The function logs the creation of the
// interface when successful. Returns an error if the network configuration operation fails.
func (r *ROIP) createOrConfigureBatmanInterface() error {
	// Check if the Batman interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultBatmanInterfaceName, r.uciNetworkConfig) {
		// Create a new network section for the Batman interface
		if err := network.SetNetworkConfigWithReader(defaultBatmanInterfaceName, &network.UCINetwork{
			Proto:  "batadv_hardif",
			Device: defaultVxLanDeviceName,
			Master: r.Config.MeshNetInterface,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		r.Logger.Debug().Msgf("Created ROIP Batman interface %s", defaultBatmanInterfaceName)
	}

	return nil
}
