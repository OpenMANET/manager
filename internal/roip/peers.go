package roip

import (
	"fmt"

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

// syncVXLANPeersWithTailscale synchronizes VXLAN peers with current Tailscale peers.
// It adds/updates VXLAN peers for active Tailscale peers and removes VXLAN peers
// for Tailscale peers that are no longer present.
func (r *ROIP) syncVXLANPeersWithTailscale() error {
	// Get current Tailscale peers
	tailscalePeers := r.GetPeers()
	if tailscalePeers == nil {
		r.Logger.Debug().Msg("No Tailscale peers available")
		return nil
	}

	// Collect active Tailscale peer IPs
	activePeerIPs := make(map[string]bool)

	// Create/update VXLAN peers for each active Tailscale peer
	for _, peer := range tailscalePeers {
		if len(peer.TailscaleIPs) == 0 {
			r.Logger.Debug().Str("peer", peer.HostName).Msg("Peer has no Tailscale IPs")
			continue
		}

		// Use the first Tailscale IP
		peerIP := peer.TailscaleIPs[0].String()
		activePeerIPs[peerIP] = true

		r.Logger.Debug().
			Str("peer", peer.HostName).
			Str("ip", peerIP).
			Msg("Syncing VXLAN peer")

		if err := r.createVxlanPeer(peerIP); err != nil {
			r.Logger.Error().
				Err(err).
				Str("peer", peer.HostName).
				Str("ip", peerIP).
				Msg("Failed to create/update VXLAN peer")
			return err
		}
	}

	// Remove VXLAN peers that are no longer in Tailscale
	// We need to check all VXLAN peers and remove those not in activePeerIPs
	// and not in multicast addresses
	if err := r.removeInactiveVXLANPeers(activePeerIPs); err != nil {
		return err
	}

	r.Logger.Info().
		Int("active_peers", len(activePeerIPs)).
		Msg("VXLAN peers synchronized with Tailscale")

	return nil
}

// removeInactiveVXLANPeers removes VXLAN peers that are not in the active peer list
// and are not multicast addresses.
func (r *ROIP) removeInactiveVXLANPeers(activePeerIPs map[string]bool) error {
	// Build a set of multicast addresses to preserve
	multicastSet := make(map[string]bool)
	for _, addr := range multicastGroupAddresses {
		multicastSet[addr] = true
	}

	// We need to search for all VXLAN peers and check if they should be removed
	// Since we can't enumerate all sections easily with the current interface,
	// we'll try common patterns
	peerSections := []string{
		"peer_multicast",
		"peer_unicast",
	}

	// Try common named peer sections with numeric suffixes
	for i := 0; i < 100; i++ {
		peerSections = append(peerSections, fmt.Sprintf("peer%d", i))
	}

	// Try anonymous section notation
	for i := 0; i < 100; i++ {
		peerSections = append(peerSections, fmt.Sprintf("@vxlan_peer[%d]", i))
	}

	for _, section := range peerSections {
		// Try to get the dst for this section
		if values, ok := r.uciNetworkConfig.Get("network", section, "dst"); ok && len(values) > 0 {
			dst := values[0]

			// Skip if this is a multicast address
			if multicastSet[dst] {
				continue
			}

			// Skip if this peer is still active in Tailscale
			if activePeerIPs[dst] {
				continue
			}

			// This peer should be removed
			r.Logger.Debug().
				Str("dst", dst).
				Str("section", section).
				Msg("Removing inactive VXLAN peer")

			if err := network.DeleteVXLANPeerByDstWithReader(dst, r.uciNetworkConfig); err != nil {
				r.Logger.Error().
					Err(err).
					Str("dst", dst).
					Msg("Failed to remove inactive VXLAN peer")
				return err
			}
		}
	}

	return nil
}
