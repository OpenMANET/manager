package control

import (
	"context"
	"strconv"

	evdev "github.com/gvalkov/golang-evdev"
	"github.com/rs/zerolog"
)

// NanoPTTSource reads Linux input events from an evdev device and emits
// PTTToggle on each key-press that matches the configured key code.
type NanoPTTSource struct {
	log    zerolog.Logger
	dev    *evdev.InputDevice
	pttKey string
}

// NewNanoPTTSource constructs a NanoPTTSource that reads from dev and emits
// a PTTToggle on each press of pttKey ("any" matches any key code, otherwise
// the decimal evdev key code).
func NewNanoPTTSource(dev *evdev.InputDevice, pttKey string, log zerolog.Logger) EventSource {
	return &NanoPTTSource{dev: dev, pttKey: pttKey, log: log}
}

// Events returns the PTT event channel for this source. The channel is closed
// when ctx is cancelled.
func (s *NanoPTTSource) Events(ctx context.Context) <-chan PTTEvent {
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
			case 1:
				s.log.Debug().Msgf("Comm key press (code=%d)", ev.Code)

				select {
				case ch <- PTTToggle:
				case <-ctx.Done():
					return
				}
			case 0:
				s.log.Debug().Msgf("Comm key release (code=%d)", ev.Code)
			}
		}
	}()

	return ch
}
