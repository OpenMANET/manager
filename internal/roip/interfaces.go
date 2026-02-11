package roip

import (
	"context"

	"github.com/openmanet/openmanetd/internal/firewall"
	"github.com/openmanet/openmanetd/internal/network"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

const (
	defaultLearningValue        string = "1"
	defaultProxyValue           string = "1"
	vxLanProtocol               string = "vxlan"
	vxLanDefaultMTUValue        string = "1450"
	defaultTunnelDeviceName     string = "tailscale0"
	defaultTunnelDeviceMTUValue string = "1500"
	defaultVxLanDeviceName      string = "vxlan0"
	defaultBatmanInterfaceName  string = "battunnel0"
	defaultMeshNetZoneName      string = "ahwlan"
)

// createOrConfigureTunnelInterface creates or configures a tunnel interface in the UCI network configuration.
// It checks if a tunnel interface with the default device name already exists in the UCI configuration.
// If the interface doesn't exist, it creates a new network section with protocol set to "none" and the
// default tunnel device name. Returns an error if the network configuration operation fails.
func (r *ROIP) createOrConfigureTunnelInterface() error {
	// Check if the tunnel interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultTunnelDeviceName, r.uciNetworkConfig) {

		// Create a new network device for the tunnel interface
		if err := network.SetDeviceConfigWithReader(defaultTunnelDeviceName, &network.UCIDevice{
			Name: defaultTunnelDeviceName,
			MTU:  defaultTunnelDeviceMTUValue,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		// Create a new network section for the tunnel interface
		if err := network.SetNetworkConfigWithReader(defaultTunnelDeviceName, &network.UCINetwork{
			Proto:  "none",
			Device: defaultTunnelDeviceName,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		if err := firewall.AddNetworkToZoneWithReader(defaultMeshNetZoneName, defaultTunnelDeviceName, r.uciFirewallConfig); err != nil {
			return err
		}

		if err := r.configureTailscalePreferences(r.ctx); err != nil {
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
		// Create a new network device for the VXLAN interface
		if err := network.SetDeviceConfigWithReader(defaultVxLanDeviceName, &network.UCIDevice{
			Name: defaultVxLanDeviceName,
			MTU:  vxLanDefaultMTUValue,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		// Create a new network section for the VXLAN interface
		if err := network.SetVXLANConfigWithReader(defaultVxLanDeviceName, &network.UCIVXLANConfig{
			Proto:    vxLanProtocol,
			Learning: defaultLearningValue,
			Tunlink:  defaultTunnelDeviceName,
			Proxy:    defaultProxyValue,
			MTU:      vxLanDefaultMTUValue,
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

// configureTailscalePreferences retrieves current Tailscale preferences, updates them to enable
// RouteAll and disable NoSNAT, and applies the changes back to the Tailscale daemon.
func (r *ROIP) configureTailscalePreferences(ctx context.Context) error {
	lc := &local.Client{}

	// Get current preferences from Tailscale daemon
	prefs, err := lc.GetPrefs(ctx)
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to get Tailscale preferences")
		return err
	}

	// Update preferences using helper function
	updateTailscalePreferences(prefs)

	// Apply the updated preferences back to Tailscale
	_, err = lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:       *prefs,
		RouteAllSet: true,
		NoSNATSet:   true,
	})
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to update Tailscale preferences")
		return err
	}

	r.Logger.Info().Msg("Successfully configured Tailscale preferences (RouteAll: true, NoSNAT: false)")
	return nil
}

// updateTailscalePreferences updates the provided Prefs to enable RouteAll and disable NoSNAT.
// It uses the ApplyEdits method from the Prefs struct to safely apply the changes.
func updateTailscalePreferences(prefs *ipn.Prefs) {
	// Create a MaskedPrefs with the desired preference changes
	edits := ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			RouteAll: true,
			NoSNAT:   false,
		},
		RouteAllSet: true,
		NoSNATSet:   true,
	}

	// Apply the edits to the provided Prefs
	prefs.ApplyEdits(&edits)
}

// tailscaleUp starts Tailscale by setting WantRunning to true.
// This is equivalent to running `tailscale up` on the command line.
func (r *ROIP) tailscaleUp(ctx context.Context) error {
	lc := &local.Client{}

	_, err := lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			WantRunning: true,
		},
		WantRunningSet: true,
	})
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to bring Tailscale up")
		return err
	}

	r.Logger.Info().Msg("Tailscale brought up")
	return nil
}

// tailscaleDown stops Tailscale by setting WantRunning to false.
// This is equivalent to running `tailscale down` on the command line.
func (r *ROIP) tailscaleDown(ctx context.Context) error {
	lc := &local.Client{}

	_, err := lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			WantRunning: false,
		},
		WantRunningSet: true,
	})
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to bring Tailscale down")
		return err
	}

	r.Logger.Info().Msg("Tailscale brought down")
	return nil
}

// cycleTailscale restarts the Tailscale service by bringing it down and back up.
// It returns the first error encountered from either operation.
func (r *ROIP) cycleTailscale(ctx context.Context) error {
	if err := r.tailscaleDown(ctx); err != nil {
		return err
	}

	if err := r.tailscaleUp(ctx); err != nil {
		return err
	}

	return nil
}
