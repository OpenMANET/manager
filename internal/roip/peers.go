package roip

import (
	"github.com/openmanet/openmanetd/internal/network"
)

var (
	multicastGroupAddresses = []string{
		"239.2.3.1",
		"224.0.0.251",
	}
)

func (r *ROIP) createVXMulticastPeers() error {
	// Create a VXLan peer for each multicast group address
	for _, addr := range multicastGroupAddresses {
		peer := network.UCIVXLANPeer{
			Dst:   addr,
			Via:   defaultTunnelDeviceName,
			VXLAN: defaultVxLanDeviceName,
		}

		// Create the VXLAN peer in UCI
		if err := network.AddVXLANPeerWithReader(&peer, r.uciNetworkConfig); err != nil {
			return err
		}

	}
	return nil
}
