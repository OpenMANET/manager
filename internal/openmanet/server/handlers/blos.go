package handlers

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	"github.com/openmanet/openmanetd/internal/blos"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/emptypb"
)

// DefaultRunCommand executes a system command and returns its combined output.
func DefaultRunCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // args are constructed by the handler
	if err != nil {
		return out, fmt.Errorf("command %q: %w", name, err)
	}

	return out, nil
}

// BLOSService implements the BLOS ConnectRPC service.
type BLOSService struct {
	Cfg         *config.Config
	Log         zerolog.Logger
	BLOSManager blos.BLOSLifecycle
	RunCommand  func(ctx context.Context, name string, args ...string) ([]byte, error)
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
// it first authenticates with Tailscale, then persists the config change,
// and finally starts the BLOS module.
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

	// Run tailscale up with the provided credentials
	args := []string{"up", "--authkey=" + req.AuthKey}
	if req.LoginServerUrl != nil && *req.LoginServerUrl != "" {
		args = append(args, "--login-server="+*req.LoginServerUrl)
	}

	runCmd := b.RunCommand
	if runCmd == nil {
		runCmd = DefaultRunCommand
	}

	b.Log.Info().Msg("Running tailscale up for BLOS authentication")

	output, err := runCmd(ctx, "tailscale", args...)
	if err != nil {
		errMsg := fmt.Sprintf("tailscale authentication failed: %s", string(output))
		b.Log.Error().Err(err).Str("output", string(output)).Msg("tailscale up failed")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("%s: %w", errMsg, err))
	}

	b.Log.Info().Msg("Tailscale authentication successful")

	// Persist config change
	if err := b.Cfg.PersistBLOSConfig(true); err != nil {
		errMsg := fmt.Sprintf("tailscale authenticated but failed to persist config: %v", err)
		b.Log.Error().Err(err).Msg("Failed to persist BLOS config")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("%s", errMsg))
	}

	// Start BLOS module
	if err := b.BLOSManager.Enable(); err != nil {
		errMsg := fmt.Sprintf("tailscale authenticated and config saved, but BLOS module failed to start: %v", err)
		b.Log.Error().Err(err).Msg("Failed to enable BLOS module")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("%s", errMsg))
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
		errMsg := fmt.Sprintf("BLOS module stopped but failed to persist config: %v", err)
		b.Log.Error().Err(err).Msg("Failed to persist BLOS config")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("%s", errMsg))
	}

	message := "BLOS disabled successfully."

	return &v1.UpdateBLOSConfigResponse{
		Success: true,
		Message: &message,
	}, nil
}
