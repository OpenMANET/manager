package roip

import (
	"github.com/openmanet/openmanetd/internal/network"
)

var (
	multicastGroupAddresses = []string{
		"239.2.3.1",
		"224.10.10.1",
		"224.0.0.251",
	}
)

// createVXMulticastPeers creates VXLan peers for each multicast group address.
// It checks if a peer already exists before creating it to avoid duplicates.
// Each peer is configured with the default tunnel device and VXLan device names.
// Returns an error if the peer creation fails, otherwise returns nil.
func (r *ROIP) createVXMulticastPeers() error {
	// Create a VXLan peer for each multicast group address
	for _, addr := range multicastGroupAddresses {
		// Check if the peer already exists before creating it
		if !network.VXLANPeerExistsByDstWithReader(addr, r.uciNetworkConfig) {
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
	}
	
	return nil
}

func (r *ROIP) createVxlanPeer(peerIP string) error {
	peer := network.UCIVXLANPeer{
		Dst:   peerIP,
		Via:   defaultTunnelDeviceName,
		VXLAN: defaultVxLanDeviceName,
	}

	// Check if the peer already exists
	_, section, err := network.GetVXLANPeerByDstWithReader(peerIP, r.uciNetworkConfig)
	if err != nil {
		// Peer doesn't exist, create it
		if err := network.AddVXLANPeerWithReader(&peer, r.uciNetworkConfig); err != nil {
			return err
		}
	} else {
		// Peer exists, update it
		if err := network.UpdateVXLANPeerWithReader(section, &peer, r.uciNetworkConfig); err != nil {
			return err
		}
	}

	return nil
}
