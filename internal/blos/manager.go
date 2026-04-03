package blos

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

const (
	tailscaleReadyTimeout      = 30 * time.Second
	tailscaleReadyPollInterval = 500 * time.Millisecond
)

// TailscaleAuthClient abstracts Tailscale authentication for testability.
type TailscaleAuthClient interface {
	Start(ctx context.Context, opts ipn.Options) error
}

// LocalTailscaleAuthClient is the production implementation using the Tailscale SDK.
type LocalTailscaleAuthClient struct{}

// Start calls the real Tailscale local.Client.Start to authenticate and start the daemon.
func (c *LocalTailscaleAuthClient) Start(ctx context.Context, opts ipn.Options) error {
	lc := &local.Client{}

	return lc.Start(ctx, opts)
}

// BLOSLifecycle defines the interface for managing BLOS runtime lifecycle.
// The handler depends on this interface so that tests can provide a mock.
type BLOSLifecycle interface {
	ConfigureAndEnable(ctx context.Context, authKey string, loginServerURL string) error
	Enable() error
	Disable()
	IsRunning() bool
}

// BLOSManager owns the BLOS lifecycle and serializes enable/disable operations.
// It implements BLOSLifecycle.
type BLOSManager struct {
	logger       zerolog.Logger
	blos         *BLOS
	cfg          *config.Config
	authClient   TailscaleAuthClient
	statusClient StatusClient
	initDService InitDService
	createFn     func(*config.Config, zerolog.Logger) (*BLOS, error)
	mu           sync.Mutex
	running      bool
}

// NewBLOSManager creates a new BLOSManager. The manager is created at startup
// regardless of whether BLOS is enabled, so the API handler always has it.
func NewBLOSManager(cfg *config.Config, logger zerolog.Logger) *BLOSManager {
	return &BLOSManager{
		cfg:          cfg,
		logger:       logger,
		authClient:   &LocalTailscaleAuthClient{},
		statusClient: &LocalStatusClient{},
		initDService: &TailscaleInitDService{},
		createFn:     NewBLOS,
	}
}

// ensureTailscaleService makes sure the tailscale init.d service is enabled
// and running before Tailscale SDK authentication. Must be called with m.mu held.
func (m *BLOSManager) ensureTailscaleService(ctx context.Context) error {
	if m.initDService == nil {
		return nil
	}

	enabled, err := m.initDService.IsEnabled(ctx)
	if err != nil {
		return fmt.Errorf("check tailscale service enabled: %w", err)
	}

	if !enabled {
		m.logger.Info().Msg("Enabling tailscale init.d service")

		if enableErr := m.initDService.Enable(ctx); enableErr != nil {
			return fmt.Errorf("enable tailscale service: %w", enableErr)
		}
	}

	running, err := m.initDService.IsRunning(ctx)
	if err != nil {
		return fmt.Errorf("check tailscale service running: %w", err)
	}

	if !running {
		m.logger.Info().Msg("Starting tailscale init.d service")

		if startErr := m.initDService.Start(ctx); startErr != nil {
			return fmt.Errorf("start tailscale service: %w", startErr)
		}
	}

	return nil
}

// waitForTailscaleReady polls Tailscale status until the backend reports "Running".
// It returns immediately if the state is already "Running", fails fast on terminal
// error states ("NeedsLogin", "NeedsMachineAuth"), and times out after
// tailscaleReadyTimeout. Must be called with m.mu held.
func (m *BLOSManager) waitForTailscaleReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, tailscaleReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(tailscaleReadyPollInterval)
	defer ticker.Stop()

	var lastState string

	for {
		status, err := m.statusClient.Status(ctx)
		if err != nil {
			return fmt.Errorf("check tailscale status: %w", err)
		}

		lastState = status.BackendState

		switch lastState {
		case "Running":
			return nil
		case "NeedsLogin", "NeedsMachineAuth":
			return fmt.Errorf("tailscale authentication not complete: %s", lastState)
		}

		m.logger.Debug().Str("state", lastState).Msg("Waiting for Tailscale backend to be ready")

		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for tailscale to be ready (last state: %s): %w", lastState, ctx.Err())
		case <-ticker.C:
		}
	}
}

// ConfigureAndEnable authenticates with Tailscale using the provided credentials
// and then starts the BLOS module. It is idempotent: if BLOS is already running
// it returns nil. It ensures the tailscale init.d service is enabled and running
// before attempting SDK authentication.
func (m *BLOSManager) ConfigureAndEnable(ctx context.Context, authKey string, loginServerURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	if err := m.ensureTailscaleService(ctx); err != nil {
		return fmt.Errorf("tailscale service setup failed: %w", err)
	}

	opts := ipn.Options{
		AuthKey: authKey,
	}

	if loginServerURL != "" {
		opts.UpdatePrefs = &ipn.Prefs{
			ControlURL:  loginServerURL,
			WantRunning: true,
		}
	}

	if err := m.authClient.Start(ctx, opts); err != nil {
		return fmt.Errorf("tailscale authentication failed: %w", err)
	}

	m.logger.Info().Msg("Tailscale authentication successful, waiting for backend to be ready")

	if err := m.waitForTailscaleReady(ctx); err != nil {
		return fmt.Errorf("tailscale not ready after authentication: %w", err)
	}

	m.logger.Info().Msg("Tailscale backend ready")

	b, err := m.createFn(m.cfg, m.logger)
	if err != nil {
		return err
	}

	if b == nil {
		return errors.New("BLOS requires gateway mode; configure the node as a mesh gateway first")
	}

	m.blos = b
	m.running = true
	m.logger.Info().Msg("BLOS module enabled at runtime")

	return nil
}

// Enable starts the BLOS module. It is idempotent: if BLOS is already running
// it returns nil. Returns a descriptive error if the node is not in gateway
// mode or if initialization fails.
func (m *BLOSManager) Enable() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	b, err := m.createFn(m.cfg, m.logger)
	if err != nil {
		return err
	}

	if b == nil {
		return errors.New("BLOS requires gateway mode; configure the node as a mesh gateway first")
	}

	m.blos = b
	m.running = true
	m.logger.Info().Msg("BLOS module enabled at runtime")

	return nil
}

// Disable stops the BLOS module and cleans up resources.
// It is idempotent: no-op if not running.
func (m *BLOSManager) Disable() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.blos.Stop()
	m.blos = nil
	m.running = false
	m.logger.Info().Msg("BLOS module disabled at runtime")
}

// IsRunning reports whether the BLOS module is currently active.
func (m *BLOSManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.running
}

// GetBLOS returns the current BLOS instance, or nil if not running.
func (m *BLOSManager) GetBLOS() *BLOS {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.blos
}
