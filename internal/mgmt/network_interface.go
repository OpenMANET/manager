package mgmt

import (
	"github.com/openmanet/openmanetd/internal/network"
)

const (
	defaultMeshInterfaceMTU = 1532
)

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
