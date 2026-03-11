package mgmt

import (
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	defaultMeshInterfaceMTU = 1532
)

// setTransportInterfaceMTU sets the MTU (Maximum Transmission Unit) for all
// wireless mesh interfaces defined in the WirelessConfig. It retrieves the list
// of mesh interfaces and attempts to set each interface's MTU to
// defaultMeshInterfaceMTU. If setting the MTU for a particular interface fails,
// the error is logged and the process continues with the remaining interfaces.
// A successful MTU update is also logged at the Info level.
//
// Returns an error if the mesh interfaces cannot be retrieved from WirelessConfig,
// otherwise returns nil even if individual interface MTU updates fail.
func (m *ManagementConfig) setTransportInterfaceMTU() error {
	wirelessInterfaces, err := m.WirelessConfig.GetMeshInterfaces()
	if err != nil {
		return err
	}

	for _, iface := range wirelessInterfaces {
		if err := network.SetMTU(iface.Name, defaultMeshInterfaceMTU); err != nil {
			m.Log.Error().Err(err).Str("interface", iface.Name).Msg("Failed to set MTU for mesh interface")
		} else {
			m.Log.Info().Str("interface", iface.Name).Int("mtu", defaultMeshInterfaceMTU).Msg("Set MTU for mesh interface")
		}
	}

	return nil
}
