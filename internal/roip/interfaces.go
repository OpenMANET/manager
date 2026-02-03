package roip

import (
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	defaultTunnelDeviceName string = "tailscale0"
	defaultVxLanDeviceName  string = "vxlan0"
	defaultBatmanInterfaceName string = "batmesh2"

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

func (r *ROIP) createOrConfigureVxLanInterface() error {
	// Check if the VXLAN interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultVxLanDeviceName, r.uciNetworkConfig) {
		// Create a new network section for the VXLAN interface
		if err := network.SetVXLANConfigWithReader(defaultVxLanDeviceName, &network.UCIVXLANConfig{
			Proto: "vxlan",
			Learning: "1",
			Tunlink: defaultTunnelDeviceName,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		r.Logger.Debug().Msgf("Created ROIP VXLAN interface %s", defaultVxLanDeviceName)
	}

	return nil
}

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
