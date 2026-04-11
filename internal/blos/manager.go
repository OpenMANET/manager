package blos

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
)

const (
	tailscaleReadyTimeout      = 30 * time.Second
	tailscaleReadyPollInterval = 500 * time.Millisecond
	tailscaleBinaryPath        = "/usr/sbin/tailscale"
)

// TailscaleAuthClient abstracts Tailscale authentication for testability.
type TailscaleAuthClient interface {
	Authenticate(ctx context.Context, authKey, loginServerURL string) error
}

// LocalTailscaleAuthClient is the production implementation that runs the
// tailscale CLI to authenticate and bring up the tunnel.
type LocalTailscaleAuthClient struct{}

// Authenticate runs "tailscale up --authkey=<key> [--login-server=<url>]" to
// authenticate the node with the control server.
func (c *LocalTailscaleAuthClient) Authenticate(ctx context.Context, authKey, loginServerURL string) error {
	args := []string{"up", "--authkey=" + authKey}
	if loginServerURL != "" {
		args = append(args, "--login-server="+loginServerURL)
	}

	return exec.CommandContext(ctx, tailscaleBinaryPath, args...).Run()
}

// BLOSLifecycle defines the interface for managing BLOS runtime lifecycle.
// The handler depends on this interface so that tests can provide a mock.
type BLOSLifecycle interface {
	ConfigureAndEnable(ctx context.Context, authKey string, loginServerURL string) error
	Enable(ctx context.Context) error
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

// waitForTailscaleDaemon polls the Tailscale status endpoint until the daemon is
// accepting connections and the IPN backend has initialized. After
// ensureTailscaleService starts the init.d service the daemon needs time to
// create its socket and initialize its backend. A non-empty BackendState
// indicates the backend is ready to accept commands like Start().
// Must be called with m.mu held.
func (m *BLOSManager) waitForTailscaleDaemon(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, tailscaleReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(tailscaleReadyPollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		status, err := m.statusClient.Status(ctx)
		if err == nil && status.BackendState != "" {
			m.logger.Debug().Str("state", status.BackendState).Msg("Tailscale daemon ready")

			return nil
		}

		if err != nil {
			lastErr = err
			m.logger.Debug().Err(err).Msg("Waiting for Tailscale daemon to accept connections")
		} else {
			m.logger.Debug().Msg("Waiting for Tailscale backend to initialize")
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timeout waiting for tailscale daemon: %w", lastErr)
			}

			return fmt.Errorf("timeout waiting for tailscale backend to initialize: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// waitForTailscaleReady polls Tailscale status until the backend reports "Running".
// This is called after authClient.Start() has already succeeded, so transient states
// like "NeedsLogin" or "Starting" are expected while the daemon processes the auth
// key with the control server. All non-Running states are polled through; if the
// backend never reaches "Running" the call times out after tailscaleReadyTimeout.
// Must be called with m.mu held.
func (m *BLOSManager) waitForTailscaleReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, tailscaleReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(tailscaleReadyPollInterval)
	defer ticker.Stop()

	var lastState string

	for {
		status, err := m.statusClient.Status(ctx)
		if err != nil {
			// Status errors are transient — the daemon may be briefly
			// unresponsive during auth processing. Keep polling.
			m.logger.Debug().Err(err).Msg("Tailscale status check failed, retrying")
		} else {
			lastState = status.BackendState

			if lastState == "Running" {
				return nil
			}

			m.logger.Debug().Str("state", lastState).Msg("Waiting for Tailscale backend to be ready")
		}

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

	if err := m.waitForTailscaleDaemon(ctx); err != nil {
		return fmt.Errorf("tailscale daemon not available: %w", err)
	}

	if err := m.authClient.Authenticate(ctx, authKey, loginServerURL); err != nil {
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

	if err := m.cfg.PersistBLOSConfig(true); err != nil {
		return fmt.Errorf("failed to persist BLOS config: %w", err)
	}

	if err := b.Start(ctx); err != nil {
		if rbErr := m.cfg.PersistBLOSConfig(false); rbErr != nil {
			m.logger.Error().Err(rbErr).Msg("Failed to roll back BLOS config after start failure")
		}

		return err
	}

	m.blos = b
	m.running = true
	m.logger.Info().Msg("BLOS module enabled at runtime")

	return nil
}

// Enable starts the BLOS module. It is idempotent: if BLOS is already running
// it returns nil. Returns a descriptive error if the node is not in gateway
// mode or if initialization fails.
func (m *BLOSManager) Enable(ctx context.Context) error {
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

	if err := b.Start(ctx); err != nil {
		return err
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
