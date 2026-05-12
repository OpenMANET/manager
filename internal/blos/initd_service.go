package blos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

const tailscaleInitDPath = "/etc/init.d/tailscale"

// InitDService abstracts OpenWrt init.d service management for testability.
// It enables the BLOS manager to ensure the tailscale daemon is enabled and
// running before attempting SDK authentication.
type InitDService interface {
	// IsEnabled reports whether the service is enabled at boot.
	IsEnabled(ctx context.Context) (bool, error)
	// Enable enables the service at boot via init.d.
	Enable(ctx context.Context) error
	// IsRunning reports whether the service process is currently running.
	IsRunning(ctx context.Context) (bool, error)
	// Start starts the service via init.d.
	Start(ctx context.Context) error
}

// TailscaleInitDService is the production implementation that manages the
// tailscale service via OpenWrt init.d scripts.
type TailscaleInitDService struct{}

func (s *TailscaleInitDService) runCmd(ctx context.Context, action string) error {
	return exec.CommandContext(ctx, tailscaleInitDPath, action).Run()
}

// IsEnabled reports whether the tailscale service is enabled at boot.
// It runs "/etc/init.d/tailscale enabled"; exit code 0 means enabled.
func (s *TailscaleInitDService) IsEnabled(ctx context.Context) (bool, error) {
	err := s.runCmd(ctx, "enabled")
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}

	return false, fmt.Errorf("check tailscale enabled: %w", err)
}

// Enable enables the tailscale service at boot via init.d.
func (s *TailscaleInitDService) Enable(ctx context.Context) error {
	return s.runCmd(ctx, "enable")
}

// IsRunning reports whether the tailscale daemon is currently running.
// It runs "/etc/init.d/tailscale running"; exit code 0 means running.
func (s *TailscaleInitDService) IsRunning(ctx context.Context) (bool, error) {
	err := s.runCmd(ctx, "running")
	if err == nil {
		return true, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, nil
	}

	return false, fmt.Errorf("check tailscale running: %w", err)
}

// Start starts the tailscale service via init.d.
func (s *TailscaleInitDService) Start(ctx context.Context) error {
	return s.runCmd(ctx, "start")
}
