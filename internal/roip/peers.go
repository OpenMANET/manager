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

	// multicastSet is a pre-built set for fast lookups
	multicastSet = map[string]bool{
		"239.2.3.1":   true,
		"224.10.10.1": true,
		"224.0.0.251": true,
	}
)

// createVXMulticastPeers creates VXLan peers for each multicast group address.
// It checks if a peer already exists before creating it to avoid duplicates.
// Each peer is configured with the default tunnel device and VXLan device names.
// Returns an error if the peer creation fails, otherwise returns nil.

// TODO: Only create 1 multicast peer per execution of this function, and rotate through the list of multicast addresses to avoid UCI conflicts when creating multiple anonymous sections in quick succession.
func (r *ROIP) createVXMulticastPeers() error {
	// Create a VXLan peer for each multicast group address
	for _, addr := range multicastGroupAddresses {
		// Check if the peer already exists before creating it
		if !network.VXLANPeerExistsByDst(addr) {
			peer := network.UCIVXLANPeer{
				Dst:   addr,
				Via:   defaultTunnelDeviceName,
				VXLAN: defaultVxLanDeviceName,
			}

			// Create the VXLAN peer in UCI
			// We use the non-reader version here because multicast peers are anonymous and don't have a stable section name to reference for updates, so we just attempt to add them and rely on UCI to handle duplicates gracefully.
			if err := network.AddVXLANPeer(&peer); err != nil {
				r.Logger.Error().
					Err(err).
					Str("address", addr).
					Msg("Failed to create VXLAN multicast peer")
				return err
			}

			r.Logger.Debug().
				Str("address", addr).
				Msg("Created VXLAN multicast peer")
		}
	}

	return nil
}

func (r *ROIP) createVxlanPeer(peerIP string) error {
	// Reload configuration to ensure clean state before peer operations
	if err := r.uciNetworkConfig.ReloadConfig(); err != nil {
		r.Logger.Debug().
			Err(err).
			Msg("Failed to reload UCI config before creating peer, continuing anyway")
	}

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

	// Collect active Tailscale peer IPs (pre-allocate with peer count)
	activePeerIPs := make(map[string]bool, len(tailscalePeers))

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
	// Get all VXLAN peers from UCI configuration
	allPeers, err := network.GetAllVXLANPeersWithReader(r.uciNetworkConfig)
	if err != nil {
		r.Logger.Error().
			Err(err).
			Msg("Failed to get all VXLAN peers")
		return err
	}

	// Check each peer and remove if it's not active and not multicast
	for section, peer := range allPeers {
		// Skip if this is a multicast address
		if multicastSet[peer.Dst] {
			continue
		}

		// Skip if this peer is still active in Tailscale
		if activePeerIPs[peer.Dst] {
			continue
		}

		// This peer should be removed
		r.Logger.Debug().
			Str("dst", peer.Dst).
			Str("section", section).
			Msg("Removing inactive VXLAN peer")

		if err := network.DeleteVXLANPeerByDstWithReader(peer.Dst, r.uciNetworkConfig); err != nil {
			r.Logger.Error().
				Err(err).
				Str("dst", peer.Dst).
				Msg("Failed to remove inactive VXLAN peer")
			return err
		}
	}

	return nil
}
