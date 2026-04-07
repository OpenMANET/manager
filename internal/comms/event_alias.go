package comms

import "github.com/openmanet/openmanetd/internal/comms/control"

// PTT event types and EventSource interface re-exported from the
// internal/comms/control sub-package. These aliases allow the rest of the
// flat comms package to continue compiling unchanged while the larger comms
// refactor (see .claude/plans/comms-refactor.md) is in progress. Later phases
// will move concrete EventSource implementations into internal/comms/control
// and these aliases can be removed.

// PTTEvent is an alias for control.PTTEvent.
type PTTEvent = control.PTTEvent

// EventSource is an alias for control.EventSource.
type EventSource = control.EventSource

const (
	// PTTDown re-exports control.PTTDown.
	PTTDown = control.PTTDown
	// PTTUp re-exports control.PTTUp.
	PTTUp = control.PTTUp
	// PTTToggle re-exports control.PTTToggle.
	PTTToggle = control.PTTToggle
)
