package batmanadv

import (
	"context"
	"fmt"
	"sync"

	"github.com/mdlayher/genetlink"
	"github.com/rs/zerolog"
)

// Listener subscribes to batman-adv generic netlink multicast events
// and dispatches registered callbacks when mesh configuration changes.
type Listener struct {
	logger             zerolog.Logger
	ctx                context.Context //nolint:containedctx // lifecycle pattern matching blos/status_worker.go
	eventConn          *genetlink.Conn
	onMeshConfigChange func()
	onGatewayEvent     func()
	cancel             context.CancelFunc
	family             genetlink.Family
	wg                 sync.WaitGroup
	mu                 sync.RWMutex
}

// NewListener creates a new event listener that subscribes to the
// batman-adv CONFIG multicast group.
func NewListener(family genetlink.Family, logger zerolog.Logger) (*Listener, error) {
	conn, err := genetlink.Dial(nil)
	if err != nil {
		return nil, fmt.Errorf("genetlink dial for events: %w", err)
	}

	// Find the CONFIG multicast group
	var groupID uint32

	for _, g := range family.Groups {
		if g.Name == BatadvNLMcgrpConfig {
			groupID = g.ID

			break
		}
	}

	if groupID == 0 {
		conn.Close()

		return nil, fmt.Errorf("multicast group %q not found in batadv family", BatadvNLMcgrpConfig)
	}

	if err := conn.JoinGroup(groupID); err != nil {
		conn.Close()

		return nil, fmt.Errorf("join multicast group %q: %w", BatadvNLMcgrpConfig, err)
	}

	return &Listener{
		eventConn: conn,
		family:    family,
		logger:    logger,
	}, nil
}

// SetOnMeshConfigChange registers a callback invoked when a mesh config
// change event is received via the CONFIG multicast group.
func (l *Listener) SetOnMeshConfigChange(cb func()) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.onMeshConfigChange = cb
}

// SetOnGatewayEvent registers a callback invoked when a gateway uevent
// is detected.
func (l *Listener) SetOnGatewayEvent(cb func()) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.onGatewayEvent = cb
}

// Start begins listening for multicast events in background goroutines.
func (l *Listener) Start(ctx context.Context) {
	l.ctx, l.cancel = context.WithCancel(ctx)

	l.wg.Add(1)

	go l.listenMulticast()

	l.wg.Add(1)

	go l.listenUevents()

	l.logger.Info().Msg("batman-adv event listener started")
}

// Stop halts all event listener goroutines and closes the connection.
func (l *Listener) Stop() {
	if l.cancel != nil {
		l.cancel()
	}

	if l.eventConn != nil {
		l.eventConn.Close()
	}

	l.wg.Wait()

	l.logger.Debug().Msg("batman-adv event listener stopped")
}

// listenMulticast blocks on the genetlink connection reading multicast events.
func (l *Listener) listenMulticast() {
	defer l.wg.Done()

	for {
		msgs, _, err := l.eventConn.Receive()
		if err != nil {
			// Check if we were stopped
			select {
			case <-l.ctx.Done():
				return
			default:
			}

			l.logger.Warn().Err(err).Msg("error receiving multicast event")

			return
		}

		for _, msg := range msgs {
			switch msg.Header.Command {
			case BatadvCmdSetMesh, BatadvCmdSetHardif, BatadvCmdSetVlan:
				l.mu.RLock()
				cb := l.onMeshConfigChange
				l.mu.RUnlock()

				if cb != nil {
					cb()
				}

				l.logger.Debug().
					Uint8("cmd", msg.Header.Command).
					Msg("mesh config change event received")
			default:
				l.logger.Debug().
					Uint8("cmd", msg.Header.Command).
					Msg("ignoring unknown multicast event")
			}
		}
	}
}

// listenUevents listens for batman-adv kobject uevents (gateway changes).
// This is best-effort: if the uevent socket cannot be opened, the goroutine
// exits silently and gateway changes are detected via polling instead.
func (l *Listener) listenUevents() {
	defer l.wg.Done()

	// Kobject uevent listening requires a raw NETLINK_KOBJECT_UEVENT socket.
	// This is best-effort — if it fails (e.g., insufficient permissions or
	// older kernel), we simply don't get gateway events and rely on polling.
	conn, err := openUeventSocket()
	if err != nil {
		l.logger.Debug().Err(err).Msg("uevent socket unavailable, gateway events will use polling fallback")

		return
	}

	defer conn.Close()

	// Set a 1-second read timeout so we can check context cancellation periodically
	if err := conn.SetReadDeadline(1000); err != nil {
		l.logger.Warn().Err(err).Msg("failed to set uevent read deadline")

		return
	}

	buf := make([]byte, 4096)

	for {
		select {
		case <-l.ctx.Done():
			return
		default:
		}

		n, err := conn.Read(buf)
		if err != nil {
			select {
			case <-l.ctx.Done():
				return
			default:
			}

			// Timeout is expected — just loop and re-check context
			continue
		}

		env := parseUevent(buf[:n])

		// Filter for batman-adv gateway events
		if env["SUBSYSTEM"] != "batman-adv" {
			continue
		}

		if env["BATTYPE"] != "gw" {
			continue
		}

		l.mu.RLock()
		cb := l.onGatewayEvent
		l.mu.RUnlock()

		if cb != nil {
			cb()
		}

		l.logger.Debug().
			Str("action", env["ACTION"]).
			Msg("batman-adv gateway uevent received")
	}
}

// parseUevent parses a kobject uevent message (null-separated key=value pairs)
// into a map.
func parseUevent(data []byte) map[string]string {
	env := make(map[string]string)

	start := 0

	for i, b := range data {
		if b == 0 {
			kv := string(data[start:i])
			start = i + 1

			for j := 0; j < len(kv); j++ {
				if kv[j] == '=' {
					env[kv[:j]] = kv[j+1:]

					break
				}
			}
		}
	}

	return env
}

// ueventConn is an abstraction over the uevent socket for reading.
type ueventConn interface {
	Read(p []byte) (n int, err error)
	Close() error
	SetReadDeadline(ms int) error
}

// openUeventSocket opens a NETLINK_KOBJECT_UEVENT socket.
// Returns an error if the socket cannot be created (permissions, missing support).
func openUeventSocket() (ueventConn, error) {
	return dialUeventSocket()
}
