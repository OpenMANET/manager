package comms

import "github.com/openmanet/openmanetd/internal/comms/control"

// PTTEvent aliases the canonical control package event enum so legacy
// in-package sources can continue to compile during the comms refactor.
type PTTEvent = control.PTTEvent

const (
	PTTDown   = control.PTTDown
	PTTUp     = control.PTTUp
	PTTToggle = control.PTTToggle
)

// EventSource aliases the canonical control package source interface.
type EventSource = control.EventSource

// DeviceTonePlayer is an optional extension for control sources that can emit
// start/stop tones directly on the remote hardware.
type DeviceTonePlayer interface {
	PlayStartTone() bool
	PlayStopTone() bool
}
