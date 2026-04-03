package handlers

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

// BLOSService implements the BLOS ConnectRPC service.
type BLOSService struct {
	Cfg         *config.Config
	Log         zerolog.Logger
	BLOSManager blos.BLOSLifecycle
}

// GetBLOSStatus retrieves the current status of the BLOS subsystem.
func (b *BLOSService) GetBLOSStatus(_ context.Context, _ *emptypb.Empty) (*v1.GetBLOSStatusResponse, error) {
	enabled := b.Cfg.BLOSEnabled()
	running := b.BLOSManager.IsRunning()

	var message string

	switch {
	case enabled && running:
		message = "BLOS subsystem is enabled and running."
	case enabled && !running:
		message = "BLOS subsystem is enabled in config but not currently running."
	default:
		message = "BLOS subsystem is disabled."
	}

	return &v1.GetBLOSStatusResponse{
		BlosEnabled: enabled,
		Message:     &message,
	}, nil
}

// UpdateBLOSConfig updates the BLOS subsystem configuration. When enabling,
// it first authenticates with Tailscale via the SDK, then persists the config
// change, and finally starts the BLOS module.
func (b *BLOSService) UpdateBLOSConfig(ctx context.Context, req *v1.UpdateBLOSConfigRequest) (*v1.UpdateBLOSConfigResponse, error) {
	if req.EnableBlos {
		return b.enableBLOS(ctx, req)
	}

	return b.disableBLOS()
}

func (b *BLOSService) enableBLOS(ctx context.Context, req *v1.UpdateBLOSConfigRequest) (*v1.UpdateBLOSConfigResponse, error) {
	if strings.TrimSpace(req.AuthKey) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("auth_key is required"))
	}

	var loginServerURL string
	if req.LoginServerUrl != nil && *req.LoginServerUrl != "" {
		loginServerURL = *req.LoginServerUrl
	}

	b.Log.Info().Msg("Configuring Tailscale and enabling BLOS")

	if err := b.BLOSManager.ConfigureAndEnable(ctx, req.AuthKey, loginServerURL); err != nil {
		b.Log.Error().Err(err).Msg("Failed to configure and enable BLOS")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to enable BLOS: %w", err))
	}

	// Persist config change; roll back runtime state on failure
	if err := b.Cfg.PersistBLOSConfig(true); err != nil {
		b.Log.Error().Err(err).Msg("Failed to persist BLOS config, rolling back")
		b.BLOSManager.Disable()

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to enable BLOS: config persistence failed, rolled back: %w", err))
	}

	message := "BLOS enabled successfully."

	return &v1.UpdateBLOSConfigResponse{
		Success: true,
		Message: &message,
	}, nil
}

func (b *BLOSService) disableBLOS() (*v1.UpdateBLOSConfigResponse, error) {
	b.BLOSManager.Disable()

	if err := b.Cfg.PersistBLOSConfig(false); err != nil {
		b.Log.Error().Err(err).Msg("Failed to persist BLOS config, re-enabling")

		if reErr := b.BLOSManager.Enable(context.Background()); reErr != nil {
			b.Log.Error().Err(reErr).Msg("Failed to re-enable BLOS during rollback")
		}

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to disable BLOS: config persistence failed, rolled back: %w", err))
	}

	message := "BLOS disabled successfully."

	return &v1.UpdateBLOSConfigResponse{
		Success: true,
		Message: &message,
	}, nil
}
