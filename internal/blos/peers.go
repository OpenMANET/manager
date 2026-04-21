package blos

import (
	"context"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
)

func (r *BLOS) createVxlanPeer(ctx context.Context, peerIP string) error {
	// Reload configuration to ensure clean state before peer operations
	if err := r.uciNetworkConfig.ReloadConfig(); err != nil {
		r.logger.Debug().
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

	if err := network.ForceReloadConfig(ctx); err != nil {
		r.logger.Error().
			Err(err).
			Msg("Failed to reload UCI config after creating/updating peer, continuing anyway")
	}

	return nil
}

// syncVXLANPeersWithTailscale synchronizes VXLAN peers with current Tailscale peers.
// It adds/updates VXLAN peers for active Tailscale peers and removes VXLAN peers
// for Tailscale peers that are no longer present.
// This function only makes changes if the peer map has actually changed since the last sync.
func (r *BLOS) syncVXLANPeersWithTailscale(ctx context.Context) error {
	// Get current Tailscale peers
	tailscalePeers := r.GetPeers()
	if tailscalePeers == nil {
		r.logger.Debug().Msg("No Tailscale peers available")

		return nil
	}

	// Collect active Tailscale peer IPs (pre-allocate with peer count)
	activePeerIPs := make(map[string]bool, len(tailscalePeers))

	for _, peer := range tailscalePeers {
		if len(peer.TailscaleIPs) == 0 {
			r.logger.Debug().Str("peer", peer.HostName).Msg("Peer has no Tailscale IPs")

			continue
		}

		// Use the first Tailscale IP
		peerIP := peer.TailscaleIPs[0].String()
		activePeerIPs[peerIP] = true
	}

	// Check if the peer set has changed since last sync (lock protects lastSyncedPeerIPs)
	r.mu.Lock()
	hasChanges := r.hasPeerChanges(activePeerIPs)
	r.mu.Unlock()

	if !hasChanges {
		r.logger.Debug().Msg("No changes in Tailscale peers, skipping VXLAN sync")

		return nil
	}

	// Perform I/O operations outside the lock
	// Create/update VXLAN peers for each active Tailscale peer
	for peerIP := range activePeerIPs {
		r.logger.Debug().
			Str("ip", peerIP).
			Msg("Syncing VXLAN peer")

		if err := r.createVxlanPeer(ctx, peerIP); err != nil {
			r.logger.Error().
				Err(err).
				Str("ip", peerIP).
				Msg("Failed to create/update VXLAN peer")

			return err
		}
	}

	// Remove VXLAN peers that are no longer in Tailscale
	if err := r.removeInactiveVXLANPeers(ctx, activePeerIPs); err != nil {
		return err
	}

	// Update the last synced peer IPs (lock protects lastSyncedPeerIPs)
	r.mu.Lock()
	r.lastSyncedPeerIPs = activePeerIPs
	r.mu.Unlock()

	// Bring up or refresh the VXLAN interface after syncing peers
	if err := r.interfaceManager.BringUp(ctx, defaultVxLanDeviceName); err != nil {
		r.logger.Error().
			Err(err).
			Msgf("Failed to bring up interface %s after syncing peers", defaultVxLanDeviceName)

		return err
	}

	r.logger.Debug().
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

	// Check if any peer in activePeerIPs is not in lastSyncedPeerIPs.
	// The reverse check is unnecessary: equal lengths + all keys in A exist in B
	// guarantees all keys in B exist in A.
	for peerIP := range activePeerIPs {
		if !r.lastSyncedPeerIPs[peerIP] {
			return true
		}
	}

	return false
}

// removeInactiveVXLANPeers removes VXLAN peers that are not in the active peer list
// and are not multicast addresses.
func (r *BLOS) removeInactiveVXLANPeers(ctx context.Context, activePeerIPs map[string]bool) error {
	// Reload configuration to ensure clean state before peer operations
	if err := r.uciNetworkConfig.ReloadConfig(); err != nil {
		r.logger.Debug().
			Err(err).
			Msg("Failed to reload UCI config before removing inactive peers, continuing anyway")
	}

	// Get all VXLAN peers from UCI configuration
	allPeers, err := network.GetAllVXLANPeersWithReader(r.uciNetworkConfig)
	if err != nil {
		r.logger.Error().
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
		r.logger.Debug().
			Str("dst", peer.Dst).
			Str("section", section).
			Msg("Removing inactive VXLAN peer")

		if err := network.DeleteVXLANPeerByDstWithReader(peer.Dst, r.uciNetworkConfig); err != nil {
			r.logger.Error().
				Err(err).
				Str("dst", peer.Dst).
				Msg("Failed to remove inactive VXLAN peer")

			return err
		}
	}

	if err := network.ForceReloadConfig(ctx); err != nil {
		r.logger.Error().
			Err(err).
			Msg("Failed to reload UCI config after removing inactive peers, continuing anyway")
	}

	return nil
}
