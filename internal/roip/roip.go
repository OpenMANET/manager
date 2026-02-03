package roip

import (
	"context"
	"errors"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"tailscale.com/client/local"
)

type ROIP struct {
	Config *config.Config
	Logger zerolog.Logger

	ctx              context.Context
	uciNetworkConfig *network.UCINetworkConfigReader
}

func NewROIP(cfg *config.Config, logger zerolog.Logger) (*ROIP, error) {
	r := &ROIP{
		Config:           cfg,
		Logger:           logger,
		ctx:              context.Background(),
		uciNetworkConfig: network.NewUCINetworkConfigReader(),
	}

	if err := r.configureInterfaces(r.ctx); err != nil {
		return nil, err
	}

	return r, nil
}

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

	if err := r.createOrConfigureTunnelInterface(); err != nil {
		return err
	}

	if err := r.createOrConfigureVxLanInterface(); err != nil {
		return err
	}

	if err := r.createOrConfigureBatmanInterface(); err != nil {
		return err
	}

	if err := r.createVXMulticastPeers(); err != nil {
		return err
	}

	return nil
}
