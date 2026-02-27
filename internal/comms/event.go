package comms

import (
	"context"
	"strconv"

	evdev "github.com/gvalkov/golang-evdev"
	"github.com/rs/zerolog"
)

// PTTEvent represents a semantic PTT state change emitted by an EventSource.
type PTTEvent uint8

const (
	// PTTDown signals that PTT should begin transmission (hold-to-talk press).
	PTTDown PTTEvent = iota
	// PTTUp signals that PTT should end transmission (hold-to-talk release).
	PTTUp
	// PTTToggle signals a press-to-toggle event; the consumer decides whether
	// to begin or end transmission based on current state.
	PTTToggle
)

// EventSource is the single interface that both the evdev backend and the
// CM108 HID backend must satisfy. It emits PTTEvents on a channel that is
// closed when the supplied context is canceled.
type EventSource interface {
	Events(ctx context.Context) <-chan PTTEvent
}

// ─── evdev backend ───────────────────────────────────────────────────────────

// evdevSource reads Linux input events from an evdev device and emits
// PTTToggle on each key-press that matches the configured key code.
type evdevSource struct {
	log    zerolog.Logger
	dev    *evdev.InputDevice
	pttKey string
}

// NewEvdevSource constructs an evdevSource. Exported for use in tests.
func NewEvdevSource(dev *evdev.InputDevice, pttKey string, log zerolog.Logger) EventSource {
	return &evdevSource{dev: dev, pttKey: pttKey, log: log}
}

func (s *evdevSource) Events(ctx context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent, 4)

	go func() {
		defer close(ch)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			ev, err := s.dev.ReadOne()
			if err != nil {
				continue
			}

			if ev.Type != evdev.EV_KEY {
				continue
			}

			// Determine if this key code is the configured PTT key.
			match := false
			if s.pttKey == "any" {
				match = true
			} else if kc, parseErr := strconv.Atoi(s.pttKey); parseErr == nil && kc >= 0 && kc <= 65535 && ev.Code == uint16(kc) {
				match = true
			}

			if !match {
				continue
			}

			switch ev.Value {
			case 1: // key-press: emit toggle
				s.log.Debug().Msgf("Comm key press (code=%d)", ev.Code)

				select {
				case ch <- PTTToggle:
				case <-ctx.Done():
					return
				}
			case 0: // key-release: no action for toggle-style
				s.log.Debug().Msgf("Comm key release (code=%d)", ev.Code)
			}
		}
	}()

	return ch
}
