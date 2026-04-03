package blos

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
	logger         zerolog.Logger
	client         StatusClient
	cancel         context.CancelFunc
	onStatusUpdate func(ctx context.Context) error
	peers          map[key.NodePublic]*ipnstate.PeerStatus
	status         *ipnstate.Status
	wg             sync.WaitGroup
	interval       time.Duration
	mu             sync.RWMutex // protects peers, status, onStatusUpdate, running
	running        bool
}

// NewStatusWorker creates a new StatusWorker with the given configuration.
func NewStatusWorker(client StatusClient, interval time.Duration, logger zerolog.Logger) *StatusWorker {
	return &StatusWorker{
		client:   client,
		interval: interval,
		logger:   logger,
		peers:    make(map[key.NodePublic]*ipnstate.PeerStatus),
	}
}

// SetOnStatusUpdate sets a callback function to be called after status is updated.
func (w *StatusWorker) SetOnStatusUpdate(callback func(ctx context.Context) error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.onStatusUpdate = callback
}

// Start begins the periodic status polling. The provided context is used as
// the parent for all polling operations; canceling it (or calling Stop) will
// terminate the worker.
func (w *StatusWorker) Start(ctx context.Context) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()

		return
	}

	w.running = true
	w.mu.Unlock()

	workerCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	w.wg.Add(1)

	go w.run(workerCtx)
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
func (w *StatusWorker) run(ctx context.Context) {
	defer w.wg.Done()

	// Fetch status immediately on start
	w.fetchAndStoreStatus(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug().Msg("Status worker stopped")

			return
		case <-ticker.C:
			w.fetchAndStoreStatus(ctx)
		}
	}
}

// fetchAndStoreStatus retrieves the current Tailscale status and stores peer information.
func (w *StatusWorker) fetchAndStoreStatus(ctx context.Context) {
	status, err := w.client.Status(ctx)
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
		if err := callback(ctx); err != nil {
			w.logger.Error().Err(err).Msg("Error in status update callback")
		}
	}
}

// GetPeers returns a shallow copy of the current peer map.
func (w *StatusWorker) GetPeers() map[key.NodePublic]*ipnstate.PeerStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.peers == nil {
		return nil
	}

	cp := make(map[key.NodePublic]*ipnstate.PeerStatus, len(w.peers))
	for k, v := range w.peers {
		cp[k] = v
	}

	return cp
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
