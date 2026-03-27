package blos

import (
	"errors"
	"sync"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
)

// BLOSLifecycle defines the interface for managing BLOS runtime lifecycle.
// The handler depends on this interface so that tests can provide a mock.
type BLOSLifecycle interface {
	Enable() error
	Disable()
	IsRunning() bool
}

// BLOSManager owns the BLOS lifecycle and serializes enable/disable operations.
// It implements BLOSLifecycle.
type BLOSManager struct {
	logger   zerolog.Logger
	blos     *BLOS
	cfg      *config.Config
	createFn func(*config.Config, zerolog.Logger) (*BLOS, error)
	mu       sync.Mutex
	running  bool
}

// NewBLOSManager creates a new BLOSManager. The manager is created at startup
// regardless of whether BLOS is enabled, so the API handler always has it.
func NewBLOSManager(cfg *config.Config, logger zerolog.Logger) *BLOSManager {
	return &BLOSManager{
		cfg:      cfg,
		logger:   logger,
		createFn: NewBLOS,
	}
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
