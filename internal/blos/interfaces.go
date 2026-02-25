package blos

import (
	"context"
	"net/netip"
	"os/exec"
	"strconv"

	"github.com/openmanet/openmanetd/internal/firewall"
	"github.com/openmanet/openmanetd/internal/network"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

// InterfaceManager defines an interface for managing network interfaces.
type InterfaceManager interface {
	BringUp(name string) error
}

// RealInterfaceManager is the real implementation that calls actual network commands.
type RealInterfaceManager struct{}

// BringUp brings up a network interface by name using ifup command.
func (r *RealInterfaceManager) BringUp(name string) error {
	return network.PerformIfUp(name)
}

// NoOpInterfaceManager is a no-op implementation for testing.
type NoOpInterfaceManager struct{}

// BringUp does nothing and returns nil (for testing).
func (n *NoOpInterfaceManager) BringUp(name string) error {
	return nil
}

const (
	defaultLearningValue        string = "1"
	defaultProxyValue           string = "1"
	vxLanProtocol               string = "vxlan"
	vxLanDefaultMTUValue        int    = 1450
	defaultTunnelDeviceName     string = "tailscale0"
	defaultTunnelDeviceMTUValue int    = 1500
	defaultVxLanDeviceName      string = "vxlan0"
	defaultBatmanInterfaceName  string = "battunnel0"
	defaultMeshNetZoneName      string = "ahwlan"
)

// createOrConfigureTunnelInterface creates or configures a tunnel interface in the UCI network configuration.
// It checks if a tunnel interface with the default device name already exists in the UCI configuration.
// If the interface doesn't exist, it creates a new network section with protocol set to "none" and the
// default tunnel device name. Returns an error if the network configuration operation fails.
func (r *BLOS) createOrConfigureTunnelInterface() error {
	// Check if the tunnel interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultTunnelDeviceName, r.uciNetworkConfig) {
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

		// Remove tailscale0 from the br-ahwlan bridge if it's there, to avoid conflicts with the VXLAN interface
		device, err := network.GetDeviceByNameWithReader(r.Config.MeshNetInterface, r.uciNetworkConfig)
		if err != nil {
			return err
		}

		if device != nil && containsString(device.Ports, defaultTunnelDeviceName) {
			removeDevice := exec.Command("uci", "del_list", "network."+r.Config.MeshNetInterface+".ports="+defaultTunnelDeviceName)
			if err := removeDevice.Run(); err != nil {
				return err
			}
		}

		r.Logger.Debug().Msgf("Created BLOS tunnel interface %s", defaultTunnelDeviceName)
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
func (r *BLOS) createOrConfigureVxLanInterface() error {
	// Check if the VXLAN interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultVxLanDeviceName, r.uciNetworkConfig) {
		// Create a new network section for the VXLAN interface
		if err := network.SetVXLANConfigWithReader(defaultVxLanDeviceName, &network.UCIVXLANConfig{
			Proto:    vxLanProtocol,
			Learning: defaultLearningValue,
			Tunlink:  defaultTunnelDeviceName,
			Proxy:    defaultProxyValue,
			MTU:      strconv.Itoa(vxLanDefaultMTUValue),
			VID:      "1",
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		if err := network.ForceReloadConfig(); err != nil {
			return err
		}

		r.Logger.Debug().Msgf("Created BLOS VXLAN interface %s", defaultVxLanDeviceName)
	}

	return nil
}

// createOrConfigureBatmanInterface creates or configures a Batman (Better Approach To Mobile Ad-hoc Networking)
// interface for BLOS (Beyond Line of Sight). It checks if a Batman interface with the default name already exists
// in the UCI (Unified Configuration Interface) network configuration. If the interface does not exist, it creates
// a new network section configured as a batadv_hardif (Batman-adv hard interface) that uses the default VXLAN
// device and associates it with the configured mesh network interface. The function logs the creation of the
// interface when successful. Returns an error if the network configuration operation fails.
func (r *BLOS) createOrConfigureBatmanInterface() error {
	// Check if the Batman interface already exists in UCI
	if !network.NetworkSectionExistsWithReader(defaultBatmanInterfaceName, r.uciNetworkConfig) {
		// Create a new network section for the Batman interface
		if err := network.SetNetworkConfigWithReader(defaultBatmanInterfaceName, &network.UCINetwork{
			Proto:  "batadv_hardif",
			Device: defaultVxLanDeviceName,
			Master: r.Config.AlfredBatInterface,
		}, r.uciNetworkConfig); err != nil {
			return err
		}

		r.Logger.Debug().Msgf("Created BLOS Batman interface %s", defaultBatmanInterfaceName)
	}

	return nil
}

// configureTailscalePreferences retrieves current Tailscale preferences, updates them to enable
// RouteAll and disable NoSNAT, and applies the changes back to the Tailscale daemon.
func (r *BLOS) configureTailscalePreferences(ctx context.Context) error {
	lc := &local.Client{}

	// Get current preferences from Tailscale daemon
	prefs, err := lc.GetPrefs(ctx)
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to get Tailscale preferences")

		return err
	}

	// Update preferences using helper function
	r.updateTailscalePreferences(prefs)

	// Apply the updated preferences back to Tailscale
	_, err = lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:              *prefs,
		NoSNATSet:          true,
		AdvertiseRoutesSet: true,
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
func (r *BLOS) updateTailscalePreferences(prefs *ipn.Prefs) {
	// Create a MaskedPrefs with the desired preference changes
	edits := ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			NoSNAT: false,
			AdvertiseRoutes: []netip.Prefix{
				netip.MustParsePrefix("10.41.0.0/16"), // TODO: Figure out how to make this dynamic based on the actual network configuration instead of hardcoding it
			},
		},
		NoSNATSet:          true,
		AdvertiseRoutesSet: true,
	}

	// Apply the edits to the provided Prefs
	prefs.ApplyEdits(&edits)
}

// createVxLanDevice creates a new VXLAN device in the system using UCI commands.
// It sets the device type to "device" and assigns it the default VXLAN device name.
// Returns an error if the UCI commands fail, otherwise returns nil.
func (r *BLOS) createVxLanDevice() error {
	setDevice := exec.Command("uci", "set", "network."+defaultVxLanDeviceName+"=device")
	if err := setDevice.Run(); err != nil {
		return err
	}

	setName := exec.Command("uci", "set", "network."+defaultVxLanDeviceName+".name="+defaultVxLanDeviceName)
	if err := setName.Run(); err != nil {
		return err
	}

	setipv4mtu := exec.Command("uci", "set", "network."+defaultVxLanDeviceName+".mtu="+strconv.Itoa(vxLanDefaultMTUValue))
	if err := setipv4mtu.Run(); err != nil {
		return err
	}

	setipv6mtu := exec.Command("uci", "set", "network."+defaultVxLanDeviceName+".mtu6="+strconv.Itoa(vxLanDefaultMTUValue))
	if err := setipv6mtu.Run(); err != nil {
		return err
	}

	if err := network.ForceReloadConfig(); err != nil {
		return err
	}

	return nil
}

// containsString checks if a slice of strings contains a specific target string.
// It iterates through the slice and returns true if it finds a match, otherwise returns false.
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}

	return false
}
