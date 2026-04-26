package terminal

import (
	"context"
	"errors"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

// ErrSessionInUse is returned by Manager.Run when another session is already
// active. When Run returns this error, the 1008 (Policy Violation) close
// frame has already been written to the WebSocket; the caller must not send
// another close frame for it.
var ErrSessionInUse = errors.New("terminal session already in use")

// Manager enforces a single concurrent terminal session per daemon and
// owns the lifetime of each session. It is safe for use from multiple
// goroutines.
type Manager struct {
	log zerolog.Logger
	cfg Config

	mu   sync.Mutex // protects busy below
	busy bool
}

// New returns a Manager configured with cfg.
func New(log zerolog.Logger, cfg Config) *Manager {
	return &Manager{log: log, cfg: cfg}
}

// InUse reports whether a session is currently active. Useful for tests
// and for surfacing status in a future API.
func (m *Manager) InUse() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.busy
}

// Run claims the single session slot, drives a Session against ws until it
// exits, and releases the slot. Returns ErrSessionInUse immediately if the
// slot is already taken; otherwise blocks until the session ends. The
// caller is expected to have already upgraded the HTTP request and is
// responsible for closing ws when Run returns.
//
// On ErrSessionInUse, Run sends a CloseFrame with code 1008 ("terminal
// already in use") to the client before returning, so the frontend can
// react without an additional round-trip.
//
// All session lifecycle logging (start failures, runtime errors, in-use
// rejections) is performed inside Run; callers should not log the
// returned error.
func (m *Manager) Run(ctx context.Context, ws *websocket.Conn) error {
	if !m.tryAcquire() {
		_ = ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "terminal already in use"))

		return ErrSessionInUse
	}
	defer m.release()

	sess, err := NewSession(ctx, m.log, m.cfg, ws)
	if err != nil {
		_ = ws.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
		m.log.Error().Err(err).Msg("terminal: session start failed")

		return err
	}

	err = sess.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		m.log.Warn().Err(err).Msg("terminal: session ended with error")
	}

	return err
}

// tryAcquire returns true if it claimed the slot. Holds the mutex only
// for the flag flip — the Run path does NOT hold the mutex during the
// session itself.
func (m *Manager) tryAcquire() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.busy {
		return false
	}

	m.busy = true

	return true
}

func (m *Manager) release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.busy = false
}
