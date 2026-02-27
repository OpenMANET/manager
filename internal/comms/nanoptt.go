package comms

import (
	"context"
	"strconv"

	evdev "github.com/gvalkov/golang-evdev"
	"github.com/rs/zerolog"
)

// ─── evdev backend ───────────────────────────────────────────────────────────

// nanoPTTSource reads Linux input events from an evdev device and emits
// PTTToggle on each key-press that matches the configured key code.
type nanoPTTSource struct {
	log    zerolog.Logger
	dev    *evdev.InputDevice
	pttKey string
}

// NewNanoPTTSource constructs a nanoPTTSource. Exported for use in tests.
func NewNanoPTTSource(dev *evdev.InputDevice, pttKey string, log zerolog.Logger) EventSource {
	return &nanoPTTSource{dev: dev, pttKey: pttKey, log: log}
}

func (s *nanoPTTSource) Events(ctx context.Context) <-chan PTTEvent {
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
