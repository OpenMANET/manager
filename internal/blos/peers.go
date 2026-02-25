package blos

import (
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
)

// createVXMulticastPeers creates VXLan peers for each multicast group address.
// It checks if a peer already exists before creating it to avoid duplicates.
// Each peer is configured with the default tunnel device and VXLan device names.
// Returns an error if the peer creation fails, otherwise returns nil.
//
// This function batches the creation of all multicast peers for efficiency,
// reducing the number of UCI commits and reloads from 2N to 2 (where N is the number of peers).
func (r *BLOS) createVXMulticastPeers() error {
	// Reload configuration to ensure clean state before creating peers
	if err := r.uciNetworkConfig.ReloadConfig(); err != nil {
		r.Logger.Debug().
			Err(err).
			Msg("Failed to reload UCI config before creating multicast peers, continuing anyway")
	}

	// Collect peers that need to be created
	peersToCreate := []network.UCIVXLANPeer{}

	for _, addr := range config.GetMulticastGroupAddresses() {
		if !network.VXLANPeerExistsByDst(addr) {
			peersToCreate = append(peersToCreate, network.UCIVXLANPeer{
				Dst:   addr,
				Via:   defaultTunnelDeviceName,
				VXLAN: defaultVxLanDeviceName,
			})
		}
	}

	// If no peers need to be created, return early
	if len(peersToCreate) == 0 {
		r.Logger.Debug().Msg("All multicast peers already exist")

		return nil
	}

	// Batch create all missing multicast peers
	if err := network.BatchAddVXLANPeersWithReader(peersToCreate, r.uciNetworkConfig); err != nil {
		r.Logger.Error().
			Err(err).
			Int("count", len(peersToCreate)).
			Msg("Failed to batch create VXLAN multicast peers")

		return err
	}

	r.Logger.Debug().
		Int("count", len(peersToCreate)).
		Msg("Successfully created VXLAN multicast peers")

	return nil
}

func (r *BLOS) createVxlanPeer(peerIP string) error {
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

	if err := network.ForceReloadConfig(); err != nil {
		r.Logger.Error().
			Err(err).
			Msg("Failed to reload UCI config after creating/updating peer, continuing anyway")
	}

	return nil
}

// syncVXLANPeersWithTailscale synchronizes VXLAN peers with current Tailscale peers.
// It adds/updates VXLAN peers for active Tailscale peers and removes VXLAN peers
// for Tailscale peers that are no longer present.
// This function only makes changes if the peer map has actually changed since the last sync.
func (r *BLOS) syncVXLANPeersWithTailscale() error {
	// Get current Tailscale peers
	tailscalePeers := r.GetPeers()
	if tailscalePeers == nil {
		r.Logger.Debug().Msg("No Tailscale peers available")

		return nil
	}

	// Collect active Tailscale peer IPs (pre-allocate with peer count)
	activePeerIPs := make(map[string]bool, len(tailscalePeers))

	for _, peer := range tailscalePeers {
		if len(peer.TailscaleIPs) == 0 {
			r.Logger.Debug().Str("peer", peer.HostName).Msg("Peer has no Tailscale IPs")

			continue
		}

		// Use the first Tailscale IP
		peerIP := peer.TailscaleIPs[0].String()
		activePeerIPs[peerIP] = true
	}

	// Check if the peer set has changed since last sync
	hasChanges := r.hasPeerChanges(activePeerIPs)

	if !hasChanges {
		r.Logger.Debug().Msg("No changes in Tailscale peers, skipping VXLAN sync")

		return nil
	}

	// Create/update VXLAN peers for each active Tailscale peer
	for peerIP := range activePeerIPs {
		r.Logger.Debug().
			Str("ip", peerIP).
			Msg("Syncing VXLAN peer")

		if err := r.createVxlanPeer(peerIP); err != nil {
			r.Logger.Error().
				Err(err).
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

	// Update the last synced peer IPs
	r.lastSyncedPeerIPs = activePeerIPs

	// Bring up or refresh the VXLAN interface after syncing peers
	if err := r.interfaceManager.BringUp(defaultVxLanDeviceName); err != nil {
		r.Logger.Error().
			Err(err).
			Msgf("Failed to bring up interface %s after syncing peers", defaultVxLanDeviceName)

		return err
	}

	r.Logger.Debug().
		Int("active_peers", len(activePeerIPs)).
		Msg("VXLAN peers synchronized with Tailscale")

	return nil
}

// hasPeerChanges compares the current active peer IPs with the last synced peer IPs
// to determine if there are any changes (additions or removals).
func (r *BLOS) hasPeerChanges(activePeerIPs map[string]bool) bool {
	// If this is the first sync, there are changes
	if r.lastSyncedPeerIPs == nil {
		return true
	}

	// If the number of peers changed, there are changes
	if len(activePeerIPs) != len(r.lastSyncedPeerIPs) {
		return true
	}

	// Check if any peer in activePeerIPs is not in lastSyncedPeerIPs
	for peerIP := range activePeerIPs {
		if !r.lastSyncedPeerIPs[peerIP] {
			return true
		}
	}

	// Check if any peer in lastSyncedPeerIPs is not in activePeerIPs
	for peerIP := range r.lastSyncedPeerIPs {
		if !activePeerIPs[peerIP] {
			return true
		}
	}

	// No changes detected
	return false
}

// removeInactiveVXLANPeers removes VXLAN peers that are not in the active peer list
// and are not multicast addresses.
func (r *BLOS) removeInactiveVXLANPeers(activePeerIPs map[string]bool) error {
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
		if config.GetMulticastGroupSet()[peer.Dst] {
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

	if err := network.ForceReloadConfig(); err != nil {
		r.Logger.Error().
			Err(err).
			Msg("Failed to reload UCI config after removing inactive peers, continuing anyway")
	}

	return nil
}
