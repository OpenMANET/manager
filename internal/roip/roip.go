package roip

import (
	"context"
	"errors"
	"time"

	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

type ROIP struct {
	Config *config.Config
	Logger zerolog.Logger

	ctx              context.Context
	uciNetworkConfig network.ConfigReader
	statusWorker     *StatusWorker
}

func NewROIP(cfg *config.Config, logger zerolog.Logger) (*ROIP, error) {
	// Get mesh config to determine if we are a gateway
	meshCfg, err := batmanadv.GetMeshConfig(cfg.GetAlfredBatInterface())
	if err != nil {
		logger.Error().Err(err).Msg("Error getting mesh config")
		return nil, err
	}

	// Only initialize ROIP if we are in gateway mode
	if !meshCfg.IsGatewayMode() {
		logger.Info().Msg("Not in gateway mode, skipping ROIP initialization")
		return nil, nil
	}

	r := &ROIP{
		Config:           cfg,
		Logger:           logger,
		ctx:              context.Background(),
		uciNetworkConfig: network.NewUCINetworkConfigReader(),
	}

	// Initialize the status worker
	interval := time.Duration(cfg.GetROIPStatusWorkerInterval()) * time.Second
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

	return r, nil
}

// configureInterfaces sets up the network interfaces required for ROIP operation.
// It first checks the Tailscale tunnel status and validates that it is in a valid state
// (Running or Starting). If the tunnel requires authentication, it returns an error.
// Then it sequentially configures the tunnel interface, VxLAN interface, and Batman
// interface, and creates VxLAN multicast peers. Returns an error if any step fails.
func (r *ROIP) configureInterfaces(ctx context.Context) error {
	// Implementation of interface configuration would go here
	tunnelStatus, err := local.Status(ctx)
	if err != nil {
		r.Logger.Error().Err(err).Msg("Failed to get Tailscale status")
		return err
	}

	switch tunnelStatus.BackendState {
	case "Running", "Starting":
		r.Logger.Info().Msg("Tunnel is starting or running")
	case "Stopped":
		r.Logger.Info().Msg("Tunnel is stopped")
	case "NeedsLogin", "NeedsMachineAuth":
		r.Logger.Error().Msgf("Tunnel requires login or machine authentication: %s", tunnelStatus.BackendState)

		return errors.New("Tunnel needs to autheticate, or machine auth is broken.  Fix the authentication and restart openmanetd.")
	default:
		r.Logger.Info().Msgf("Tunnel is in state: %s", tunnelStatus.BackendState)
	}

	r.Logger.Info().Msgf("Tunnel status: %v", tunnelStatus)

	// Only configure ROIP interfaces if the mesh network interface exists
	if network.NetworkSectionExistsWithReader(r.Config.GetAlfredBatInterface(), r.uciNetworkConfig) {

		// Configure wireguard (tailscale) tunnel interface
		if err := r.createOrConfigureTunnelInterface(); err != nil {
			return err
		}

		// Configure VxLAN interface
		if err := r.createOrConfigureVxLanInterface(); err != nil {
			return err
		}

		// Configure Batman interface for tunnel
		if err := r.createOrConfigureBatmanInterface(); err != nil {
			return err
		}

		// Create VxLAN multicast peers
		if err := r.createVXMulticastPeers(); err != nil {
			return err
		}

		r.Logger.Debug().Msg("ROIP interfaces configured successfully")
	}

	return nil
}

// GetPeers returns the current Tailscale peer information.
func (r *ROIP) GetPeers() map[key.NodePublic]*ipnstate.PeerStatus {
	if r.statusWorker == nil {
		return nil
	}
	return r.statusWorker.GetPeers()
}

// GetPeer returns a specific Tailscale peer by node key.
func (r *ROIP) GetPeer(nodeKey key.NodePublic) (*ipnstate.PeerStatus, bool) {
	if r.statusWorker == nil {
		return nil, false
	}
	return r.statusWorker.GetPeer(nodeKey)
}

// GetStatus returns the last fetched Tailscale status.
func (r *ROIP) GetStatus() *ipnstate.Status {
	if r.statusWorker == nil {
		return nil
	}
	return r.statusWorker.GetStatus()
}

// Stop stops the ROIP worker processes.
func (r *ROIP) Stop() {
	if r.statusWorker != nil {
		r.statusWorker.Stop()
	}
}
