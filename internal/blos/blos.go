package blos

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/firewall"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

type BLOS struct {
	Logger             zerolog.Logger
	ctx                context.Context
	uciOpenManetConfig network.OpenMANETConfigReader
	uciNetworkConfig   network.ConfigReader
	uciFirewallConfig  firewall.ConfigReader
	interfaceManager   InterfaceManager
	Config             *config.Config
	statusWorker       *StatusWorker
	lastSyncedPeerIPs  map[string]bool
	mcastJoiner        multicastJoiner
	multicastConns     []*net.UDPConn
	mu                 sync.Mutex
}

func NewBLOS(cfg *config.Config, logger zerolog.Logger) (*BLOS, error) {
	// Get mesh config to determine if we are a gateway
	meshCfg, err := batmanadv.GetMeshConfig(cfg.GetAlfredBatInterface())
	if err != nil {
		logger.Error().Err(err).Msg("Error getting mesh config")

		return nil, err
	}

	// Only initialize BLOS if we are in gateway mode
	if !meshCfg.IsGatewayMode() {
		logger.Info().Msg("Not in gateway mode, skipping BLOS initialization")

		return nil, nil
	}

	r := &BLOS{
		Config:             cfg,
		Logger:             logger,
		ctx:                context.Background(),
		uciOpenManetConfig: network.NewUCIOpenMANETConfigReader(),
		uciNetworkConfig:   network.NewUCINetworkConfigReader(),
		uciFirewallConfig:  firewall.NewUCIFirewallConfigReader(),
		interfaceManager:   &RealInterfaceManager{},
	}

	// Initialize the status worker
	interval := time.Duration(cfg.GetBLOSStatusWorkerInterval()) * time.Second
	r.statusWorker = NewStatusWorker(&LocalStatusClient{}, interval, logger)

	// Set the callback to sync VXLAN peers when Tailscale status updates
	r.statusWorker.SetOnStatusUpdate(func() error {
		return r.syncVXLANPeersWithTailscale()
	})

	if err := r.configureInterfaces(r.ctx); err != nil {
		return nil, err
	}

	// Start the status worker
	r.statusWorker.Start()

	logger.Info().Msg("BLOS (Beyond Line-of-Sight) module initialized successfully")

	return r, nil
}

// configureInterfaces sets up the network interfaces required for BLOS operation.
// It first checks the Tailscale tunnel status and validates that it is in a valid state
// (Running or Starting). If the tunnel requires authentication, it returns an error.
// Then it sequentially configures the tunnel interface, VxLAN interface, and Batman
// interface, and creates VxLAN multicast peers. Returns an error if any step fails.
func (r *BLOS) configureInterfaces(ctx context.Context) error { //nolint:gocognit
	// Implementation of interface configuration would go here
	tunnelStatus, err := local.Status(ctx)
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to get Tailscale status")

		return err
	}

	switch tunnelStatus.BackendState {
	case "Running", "Starting":
		r.Logger.Info().Msgf("Tunnel status: %s", tunnelStatus.BackendState)
	case "Stopped":
		r.Logger.Info().Msg("Tunnel is stopped")

		return errors.New("tunnel is stopped. Ensure Tailscale is running and authenticated, then restart openmanetd")
	case "NeedsLogin", "NeedsMachineAuth":
		r.Logger.Error().Msgf("Tunnel requires login or machine authentication: %s", tunnelStatus.BackendState)

		return errors.New("tunnel needs to autheticate, or machine auth is broken. Fix the authentication and restart openmanetd")
	default:
		r.Logger.Info().Msgf("Tunnel is in state: %s", tunnelStatus.BackendState)
	}

	// Only configure BLOS interfaces if the mesh network interface exists
	if network.NetworkSectionExistsWithReader(r.Config.GetAlfredBatInterface(), r.uciNetworkConfig) { //nolint:nestif
		blosConfigured, err := network.IsBLOSConfiguredWithReader(r.uciOpenManetConfig)
		if err != nil {
			return err
		}

		if !blosConfigured {
			// Configure wireguard (tailscale) tunnel interface
			err = r.createOrConfigureTunnelInterface()
			if err != nil {
				return err
			}

			// Configure VxLAN interface
			err = r.createOrConfigureVxLanInterface()
			if err != nil {
				return err
			}

			// Configure Batman interface for tunnel
			err = r.createOrConfigureBatmanInterface()
			if err != nil {
				return err
			}

			// Create VxLAN multicast peers
			err = r.createVXMulticastPeers()
			if err != nil {
				return err
			}

			// Mark BLOS as configured in the OpenMANET config to avoid reconfiguring on every startup
			err = network.SetBLOSConfiguredWithReader(r.uciOpenManetConfig)
			if err != nil {
				return err
			}

			// Reboot the system to clean up things and apply new network settings.  This is required to properly set up the tunnel and interfaces in the correct order, and to ensure a clean state.  We will likely need to reboot at least once anyway after installation to get Tailscale set up, so doing it here ensures we don't end up in a broken state if we try to configure interfaces before Tailscale is fully set up and authenticated.
			r.Logger.Info().Msg("BLOS configured successfully, rebooting system to apply changes")

			if err = system.Reboot(); err != nil {
				r.Logger.Error().Err(err).Msg("Failed to reboot system after BLOS configuration")

				return err
			}
		}

		// Ensure the MTU is set correctly on the tunnel interface to avoid fragmentation issues with VXLAN encapsulated packets.
		// Note: Tailscale sets the MTU of the tailscale0 interface to 1280 by default, which can cause fragmentation issues when used as the underlying device for a VXLAN interface. Setting it to 1500 allows for better performance while still avoiding fragmentation in most cases, but this may need to be adjusted based on the specific network environment and requirements.
		if err := network.SetMTU(defaultTunnelDeviceName, defaultTunnelDeviceMTUValue); err != nil {
			return err
		}

		// Ensure the MTU is set correctly on the VXLAN interface to avoid fragmentation issues with encapsulated packets.
		// Note: The MTU for the VXLAN interface should be set to a value that accounts for the overhead of VXLAN encapsulation (typically around 50 bytes) to avoid fragmentation issues. Setting it to 1450 allows for better performance while still avoiding fragmentation in most cases, but this may need to be adjusted based on the specific network environment and requirements.
		if err := network.SetMTU(defaultVxLanDeviceName, vxLanDefaultMTUValue); err != nil {
			return err
		}

		// Join multicast groups on the mesh interface so batman-adv forwards
		// multicast traffic across the VXLAN link to BLOS peers.
		if err := r.joinMulticastGroupsOnInterface(r.Config.MeshNetInterface); err != nil {
			r.Logger.Error().Err(err).Msg("Failed to join multicast groups on mesh interface")

			return err
		}

		r.Logger.Debug().Msg("BLOS interfaces configured successfully")
	}

	return nil
}

// GetPeers returns the current Tailscale peer information.
func (r *BLOS) GetPeers() map[key.NodePublic]*ipnstate.PeerStatus {
	if r.statusWorker == nil {
		return nil
	}

	return r.statusWorker.GetPeers()
}

// GetPeer returns a specific Tailscale peer by node key.
func (r *BLOS) GetPeer(nodeKey key.NodePublic) (*ipnstate.PeerStatus, bool) {
	if r.statusWorker == nil {
		return nil, false
	}

	return r.statusWorker.GetPeer(nodeKey)
}

// GetStatus returns the last fetched Tailscale status.
func (r *BLOS) GetStatus() *ipnstate.Status {
	if r.statusWorker == nil {
		return nil
	}

	return r.statusWorker.GetStatus()
}

// Stop stops the BLOS worker processes and closes any open multicast sockets.
func (r *BLOS) Stop() {
	if r.statusWorker != nil {
		r.statusWorker.Stop()
	}

	for _, c := range r.multicastConns {
		if err := c.Close(); err != nil {
			r.Logger.Debug().Err(err).Msg("Error closing multicast socket during shutdown")
		}
	}

	r.multicastConns = nil
}
