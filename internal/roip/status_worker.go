package roip

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

// StatusClient is an interface for interacting with Tailscale status.
// This allows for mocking in tests.
type StatusClient interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
}

// LocalStatusClient is the real implementation that calls tailscale's local.Status.
type LocalStatusClient struct{}

// Status calls the real tailscale local.Status function.
func (c *LocalStatusClient) Status(ctx context.Context) (*ipnstate.Status, error) {
	return local.Status(ctx)
}

// StatusWorker manages periodic polling of Tailscale status and stores peer information.
type StatusWorker struct {
	logger zerolog.Logger

	client         StatusClient
	ctx            context.Context
	peers          map[key.NodePublic]*ipnstate.PeerStatus
	status         *ipnstate.Status
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	interval       time.Duration
	onStatusUpdate func() error

	mu      sync.RWMutex
	running bool
}

// NewStatusWorker creates a new StatusWorker with the given configuration.
func NewStatusWorker(client StatusClient, interval time.Duration, logger zerolog.Logger) *StatusWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &StatusWorker{
		client:   client,
		interval: interval,
		logger:   logger,
		peers:    make(map[key.NodePublic]*ipnstate.PeerStatus),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// SetOnStatusUpdate sets a callback function to be called after status is updated.
func (w *StatusWorker) SetOnStatusUpdate(callback func() error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onStatusUpdate = callback
}

// Start begins the periodic status polling.
func (w *StatusWorker) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run()
}

// Stop halts the status polling worker.
func (w *StatusWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	w.cancel()
	w.wg.Wait()
}

// run is the main worker loop that periodically fetches status.
func (w *StatusWorker) run() {
	defer w.wg.Done()

	// Fetch status immediately on start
	w.fetchAndStoreStatus()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Debug().Msg("Status worker stopped")
			return
		case <-ticker.C:
			w.fetchAndStoreStatus()
		}
	}
}

// fetchAndStoreStatus retrieves the current Tailscale status and stores peer information.
func (w *StatusWorker) fetchAndStoreStatus() {
	status, err := w.client.Status(w.ctx)
	if err != nil {
		w.logger.Error().Err(err).Msg("Failed to fetch Tailscale status")
		return
	}

	w.mu.Lock()
	w.status = status
	w.peers = status.Peer
	callback := w.onStatusUpdate
	w.mu.Unlock()

	w.logger.Debug().Msg("Successfully updated Tailscale status and peers")

	// Call the callback if set
	if callback != nil {
		if err := callback(); err != nil {
			w.logger.Error().Err(err).Msg("Error in status update callback")
		}
	}
}

// GetPeers returns the current peer map.
// Note: Do not modify the returned map.
func (w *StatusWorker) GetPeers() map[key.NodePublic]*ipnstate.PeerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.peers
}

// GetPeer returns a specific peer by node key.
func (w *StatusWorker) GetPeer(nodeKey key.NodePublic) (*ipnstate.PeerStatus, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	peer, ok := w.peers[nodeKey]
	return peer, ok
}

// GetStatus returns a copy of the last fetched status.
func (w *StatusWorker) GetStatus() *ipnstate.Status {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.status
}

// IsRunning returns whether the worker is currently running.
func (w *StatusWorker) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.running
}
