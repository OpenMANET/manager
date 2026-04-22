package blos

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

// rateRingSize caps the rolling rate-sample ring at a fixed number of
// entries so the status worker never allocates per tick. At the default
// 5-second poll interval, 16 samples covers ~80s — long enough to
// compute a clean 60s rate after a couple of missed ticks.
const rateRingSize = 16

// keepaliveInterval bounds how frequently the status worker emits a
// synthetic BLOS_EVENT_KIND_KEEPALIVE. Subscribers use these events as
// stream-liveness ticks when no real state change has occurred.
const keepaliveInterval = 30 * time.Second

// rateWindow60s is the standard 60-second look-back used by the
// instrumentation snapshot and by the BLOSCounters RPC response.
const rateWindow60s = 60 * time.Second

// backendStateRunning is the BackendState string Tailscale reports once
// the daemon is logged in and the WireGuard engine is carrying traffic.
const backendStateRunning = "Running"

// EventKind categorizes BLOS state-change observations. Values mirror
// the proto enum openmanet.blos.v1.BLOSEventKind one-to-one.
type EventKind int

const (
	// EventKindUnspecified is the zero value and must not be emitted.
	EventKindUnspecified EventKind = 0
	// EventKindBackendState signals a Tailscale backend state transition.
	EventKindBackendState EventKind = 1
	// EventKindPeerAdded signals that a new peer appeared.
	EventKindPeerAdded EventKind = 2
	// EventKindPeerLost signals that an existing peer disappeared.
	EventKindPeerLost EventKind = 3
	// EventKindPeerOnline signals an existing peer went online.
	EventKindPeerOnline EventKind = 4
	// EventKindPeerOffline signals an existing peer went offline.
	EventKindPeerOffline EventKind = 5
	// EventKindDerpChanged signals a DERP region change for self or a peer.
	EventKindDerpChanged EventKind = 6
	// EventKindKeepalive is a periodic stream liveness tick.
	EventKindKeepalive EventKind = 7
)

// Event is a single BLOS state-change observation. The status worker
// diffs successive ipnstate.Status readings and emits Events to all
// registered listeners.
type Event struct {
	At      time.Time
	Subject string
	Message string
	Kind    EventKind
}

// rateSample is one entry in the rolling RX/TX byte-count ring.
type rateSample struct {
	t       time.Time
	rxTotal uint64
	txTotal uint64
}

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
	logger          zerolog.Logger
	lastKeepaliveAt time.Time
	connectedSince  time.Time
	client          StatusClient
	cancel          context.CancelFunc
	onStatusUpdate  func(ctx context.Context) error
	peers           map[key.NodePublic]*ipnstate.PeerStatus
	status          *ipnstate.Status
	listeners       map[uint64]func(Event)
	rateRing        [rateRingSize]rateSample
	wg              sync.WaitGroup
	rateHead        int
	rateCount       int
	interval        time.Duration
	nextListenerID  uint64
	eventsDropped   atomic.Uint64
	mu              sync.RWMutex
	running         bool
}

// NewStatusWorker creates a new StatusWorker with the given configuration.
func NewStatusWorker(client StatusClient, interval time.Duration, logger zerolog.Logger) *StatusWorker {
	return &StatusWorker{
		client:    client,
		interval:  interval,
		logger:    logger,
		peers:     make(map[key.NodePublic]*ipnstate.PeerStatus),
		listeners: make(map[uint64]func(Event)),
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

// Stop halts the status polling worker and clears connected-since tracking so
// a subsequent Start begins a fresh connection window.
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

	w.mu.Lock()
	w.connectedSince = time.Time{}
	w.rateHead = 0
	w.rateCount = 0
	w.mu.Unlock()
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

	now := time.Now()

	w.mu.Lock()
	prev := w.status
	w.status = status
	w.peers = status.Peer
	callback := w.onStatusUpdate

	if status.BackendState == backendStateRunning && w.connectedSince.IsZero() {
		w.connectedSince = now
	}

	if status.BackendState != backendStateRunning {
		w.connectedSince = time.Time{}
	}

	w.recordRateSample(now, status)
	events := w.diffStatus(prev, status, now)
	listeners := w.snapshotListeners()
	w.mu.Unlock()

	w.logger.Debug().Msg("Successfully updated Tailscale status and peers")

	for _, ev := range events {
		w.fireEvent(listeners, ev)
	}

	w.maybeEmitKeepalive(now)

	// Call the callback if set
	if callback != nil {
		if err := callback(ctx); err != nil {
			w.logger.Error().Err(err).Msg("Error in status update callback")
		}
	}
}

// recordRateSample appends a (time, rxTotal, txTotal) triple to the rolling
// ring. Caller must hold w.mu. Zero-allocation: the ring is a fixed array.
func (w *StatusWorker) recordRateSample(now time.Time, status *ipnstate.Status) {
	var rxTotal, txTotal uint64

	for _, p := range status.Peer {
		if p == nil {
			continue
		}

		rxTotal += uint64(p.RxBytes)
		txTotal += uint64(p.TxBytes)
	}

	w.rateRing[w.rateHead] = rateSample{t: now, rxTotal: rxTotal, txTotal: txTotal}
	w.rateHead = (w.rateHead + 1) % rateRingSize

	if w.rateCount < rateRingSize {
		w.rateCount++
	}
}

// RateWindow returns the RX/TX rate in bytes per second averaged over the
// requested look-back window, along with the cumulative rx/tx totals from the
// most recent sample. Zero rates are returned when the ring does not yet have
// at least two samples or when the window is shorter than the sample spacing.
func (w *StatusWorker) RateWindow(window time.Duration) (rxBps, txBps float64, rxTotal, txTotal uint64) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.rateCount < 2 {
		if w.rateCount == 1 {
			// With a single sample we still expose cumulative totals.
			idx := (w.rateHead - 1 + rateRingSize) % rateRingSize
			rxTotal = w.rateRing[idx].rxTotal
			txTotal = w.rateRing[idx].txTotal
		}

		return 0, 0, rxTotal, txTotal
	}

	newestIdx := (w.rateHead - 1 + rateRingSize) % rateRingSize
	newest := w.rateRing[newestIdx]
	rxTotal = newest.rxTotal
	txTotal = newest.txTotal

	cutoff := newest.t.Add(-window)

	// Walk back from newest-1 until we fall out of the window. The oldest
	// sample inside the window (or the very first valid sample, whichever
	// comes later) is the rate-calc anchor.
	anchor := newest

	for offset := 1; offset < w.rateCount; offset++ {
		idx := (newestIdx - offset + rateRingSize) % rateRingSize
		s := w.rateRing[idx]

		if s.t.Before(cutoff) {
			break
		}

		anchor = s
	}

	elapsed := newest.t.Sub(anchor.t).Seconds()
	if elapsed <= 0 {
		return 0, 0, rxTotal, txTotal
	}

	rxBps = float64(newest.rxTotal-anchor.rxTotal) / elapsed
	txBps = float64(newest.txTotal-anchor.txTotal) / elapsed

	// Counter wraparound protection — if Tailscale restarted and counters
	// reset, the subtraction underflows to a huge number. Clamp to zero.
	if newest.rxTotal < anchor.rxTotal {
		rxBps = 0
	}

	if newest.txTotal < anchor.txTotal {
		txBps = 0
	}

	return rxBps, txBps, rxTotal, txTotal
}

// diffStatus compares prev and current and returns the list of Events to
// emit. Caller must hold w.mu. prev may be nil on the first tick.
func (w *StatusWorker) diffStatus(prev, current *ipnstate.Status, now time.Time) []Event {
	if current == nil {
		return nil
	}

	events := make([]Event, 0, 4)

	if prev == nil || prev.BackendState != current.BackendState {
		events = append(events, Event{
			At:      now,
			Kind:    EventKindBackendState,
			Subject: "backend",
			Message: fmt.Sprintf("backend state %q", current.BackendState),
		})
	}

	// Self DERP change
	if prev != nil && prev.Self != nil && current.Self != nil && prev.Self.Relay != current.Self.Relay {
		events = append(events, Event{
			At:      now,
			Kind:    EventKindDerpChanged,
			Subject: "self",
			Message: fmt.Sprintf("DERP region %s -> %s", prev.Self.Relay, current.Self.Relay),
		})
	}

	// Peer add/remove/online-flip/derp-change
	prevPeers := map[key.NodePublic]*ipnstate.PeerStatus{}
	if prev != nil {
		prevPeers = prev.Peer
	}

	for k, p := range current.Peer {
		if p == nil {
			continue
		}

		old, existed := prevPeers[k]
		if !existed {
			events = append(events, Event{
				At:      now,
				Kind:    EventKindPeerAdded,
				Subject: p.HostName,
				Message: fmt.Sprintf("peer %s added", p.HostName),
			})

			continue
		}

		if old.Online != p.Online {
			kind := EventKindPeerOffline
			msg := fmt.Sprintf("peer %s offline", p.HostName)

			if p.Online {
				kind = EventKindPeerOnline
				msg = fmt.Sprintf("peer %s online", p.HostName)
			}

			events = append(events, Event{At: now, Kind: kind, Subject: p.HostName, Message: msg})
		}

		if old.Relay != p.Relay {
			events = append(events, Event{
				At:      now,
				Kind:    EventKindDerpChanged,
				Subject: p.HostName,
				Message: fmt.Sprintf("peer %s DERP %s -> %s", p.HostName, old.Relay, p.Relay),
			})
		}
	}

	for k, p := range prevPeers {
		if p == nil {
			continue
		}

		if _, still := current.Peer[k]; !still {
			events = append(events, Event{
				At:      now,
				Kind:    EventKindPeerLost,
				Subject: p.HostName,
				Message: fmt.Sprintf("peer %s lost", p.HostName),
			})
		}
	}

	return events
}

// maybeEmitKeepalive emits a BLOS_EVENT_KIND_KEEPALIVE event when no event
// has been emitted in keepaliveInterval. Uses the same listener fan-out as
// diff-driven events.
func (w *StatusWorker) maybeEmitKeepalive(now time.Time) {
	w.mu.Lock()

	if !w.lastKeepaliveAt.IsZero() && now.Sub(w.lastKeepaliveAt) < keepaliveInterval {
		w.mu.Unlock()

		return
	}

	w.lastKeepaliveAt = now
	listeners := w.snapshotListeners()
	w.mu.Unlock()

	w.fireEvent(listeners, Event{At: now, Kind: EventKindKeepalive})
}

// snapshotListeners returns a slice copy of the current listener callbacks.
// Caller must hold w.mu. The slice is safe to iterate without the lock.
func (w *StatusWorker) snapshotListeners() []func(Event) {
	if len(w.listeners) == 0 {
		return nil
	}

	out := make([]func(Event), 0, len(w.listeners))
	for _, fn := range w.listeners {
		out = append(out, fn)
	}

	return out
}

// fireEvent invokes each listener with the given event. Listeners that
// panic are logged but do not halt the worker.
func (w *StatusWorker) fireEvent(listeners []func(Event), ev Event) {
	for _, fn := range listeners {
		func() {
			defer func() {
				if r := recover(); r != nil {
					w.logger.Error().Interface("panic", r).Msg("BLOS event listener panicked")
				}
			}()

			fn(ev)
		}()
	}
}

// AddEventListener registers a callback that receives every BLOS Event
// emitted by the status worker. The returned id may be passed to
// RemoveEventListener to stop delivery. Callbacks must return quickly;
// slow or blocking callbacks stall the worker's state-update path.
func (w *StatusWorker) AddEventListener(fn func(Event)) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextListenerID++
	id := w.nextListenerID
	w.listeners[id] = fn

	return id
}

// RemoveEventListener deregisters the listener previously returned from
// AddEventListener. No-op if the id is not registered.
func (w *StatusWorker) RemoveEventListener(id uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.listeners, id)
}

// NoteEventDropped increments the events-dropped counter. Called by
// listeners that use bounded buffers and have to drop an event when the
// buffer is full; surfaces in the BLOS instrumentation snapshot.
func (w *StatusWorker) NoteEventDropped() {
	w.eventsDropped.Add(1)
}

// EventsDropped returns the cumulative number of events dropped by
// listeners since the worker was created.
func (w *StatusWorker) EventsDropped() uint64 {
	return w.eventsDropped.Load()
}

// ConnectedSince returns the time the backend first reached Running in
// the current enable cycle, or zero if the backend has never reached
// Running since the last Start.
func (w *StatusWorker) ConnectedSince() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.connectedSince
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
