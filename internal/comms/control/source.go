package control

import (
	"fmt"

	"github.com/rs/zerolog"
)

// ControlDeps is the dependency bundle handed to a control-source Factory.
//
// Log is supplied for every backend. Backend is an opaque, backend-specific
// payload that the registering package fills in and the Factory casts to its
// expected concrete type. This keeps the control package free of any import
// dependency on its consumers (avoids an import cycle with internal/comms)
// while still letting each backend pull whatever it needs from the runtime.
type ControlDeps struct {
	Log     zerolog.Logger
	Backend any
}

// Factory constructs an EventSource from a ControlDeps bundle.
type Factory func(ControlDeps) (EventSource, error)

// registry holds the set of registered control-source factories.
//
// Concurrency: registry is written only from package init() functions, which
// the Go runtime serializes before any user goroutines start. Lookups happen
// after init() at request time. A plain map is therefore sufficient and no
// mutex is required.
var registry = map[string]Factory{}

// Register installs a Factory under the given name. It is intended to be
// called from an init() function in the package that owns the backend.
// Re-registering the same name panics, since duplicate registration almost
// always indicates a programming error.
func Register(name string, f Factory) {
	if name == "" {
		panic("control.Register: empty name")
	}

	if f == nil {
		panic(fmt.Sprintf("control.Register: nil factory for %q", name))
	}

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("control.Register: duplicate registration for %q", name))
	}

	registry[name] = f
}

// Lookup returns the Factory registered under the given name, if any.
func Lookup(name string) (Factory, bool) {
	f, ok := registry[name]

	return f, ok
}

// Names returns the set of registered backend names. The returned slice is a
// copy and may be freely modified by the caller. Order is not specified.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}

	return out
}
