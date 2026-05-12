package control

import "context"

// AuxEvent is a non-PTT control event emitted by sources that expose
// auxiliary buttons (volume up/down, mute, etc.). It is delivered on a
// separate channel from PTTEvent so the PTT state machine remains
// untouched and consumers can opt in by type-asserting on AuxEventSource.
type AuxEvent uint8

const (
	// VolumeUpPressed signals the volume-up button transitioned to pressed.
	VolumeUpPressed AuxEvent = iota
	// VolumeUpReleased signals the volume-up button transitioned to released.
	VolumeUpReleased
	// VolumeDownPressed signals the volume-down button transitioned to pressed.
	VolumeDownPressed
	// VolumeDownReleased signals the volume-down button transitioned to released.
	VolumeDownReleased
)

// AuxEventSource is an optional interface that EventSource implementations
// MAY also satisfy when they expose non-PTT control buttons. Consumers
// type-assert on this interface to opt into aux event handling; sources
// without auxiliary buttons simply do not implement it.
type AuxEventSource interface {
	AuxEvents() <-chan AuxEvent
}

// AuxEventHandler reacts to AuxEvent values produced by any source.
// Implementations must be goroutine-safe; long-running work should be
// dispatched to its own goroutine so the dispatch loop does not block.
type AuxEventHandler interface {
	Handle(ctx context.Context, ev AuxEvent)
}
