package blos

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
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
	logger     zerolog.Logger
	blos       *BLOS
	cfg        *config.Config
	authClient TailscaleAuthClient
	createFn   func(*config.Config, zerolog.Logger) (*BLOS, error)
	mu         sync.Mutex
	running    bool
}

// NewBLOSManager creates a new BLOSManager. The manager is created at startup
// regardless of whether BLOS is enabled, so the API handler always has it.
func NewBLOSManager(cfg *config.Config, logger zerolog.Logger) *BLOSManager {
	return &BLOSManager{
		cfg:        cfg,
		logger:     logger,
		authClient: &LocalTailscaleAuthClient{},
		createFn:   NewBLOS,
	}
}

// ConfigureAndEnable authenticates with Tailscale using the provided credentials
// and then starts the BLOS module. It is idempotent: if BLOS is already running
// it returns nil.
func (m *BLOSManager) ConfigureAndEnable(ctx context.Context, authKey string, loginServerURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
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

	m.logger.Info().Msg("Tailscale authentication successful")

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
