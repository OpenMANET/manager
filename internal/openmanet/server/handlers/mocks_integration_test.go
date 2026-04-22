//go:build integration

package handlers_test

import (
	"github.com/openmanet/openmanetd/internal/blos"
)

// fireEvent synchronously delivers an event to all registered listeners.
// Lives behind the integration build tag because it is only used by the
// integration-test lifecycle check; including it in the default test build
// would trip the linter's unused-method rule.
func (f *fakeBLOSManager) fireEvent(ev blos.Event) {
	f.mu.Lock()
	listeners := make([]func(blos.Event), 0, len(f.listeners))

	for _, fn := range f.listeners {
		listeners = append(listeners, fn)
	}
	f.mu.Unlock()

	for _, fn := range listeners {
		fn(ev)
	}
}
