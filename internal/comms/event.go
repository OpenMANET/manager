package comms

import (
	"context"
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
// OpenVLM HID backend must satisfy. It emits PTTEvents on a channel that is
// closed when the supplied context is canceled.
type EventSource interface {
	Events(ctx context.Context) <-chan PTTEvent
}
