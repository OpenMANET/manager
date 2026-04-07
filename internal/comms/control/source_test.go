package control

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSource struct{ name string }

func (s *stubSource) Events(_ context.Context) <-chan PTTEvent {
	ch := make(chan PTTEvent)
	close(ch)

	return ch
}

func resetRegistry(t *testing.T) {
	t.Helper()

	saved := make(map[string]Factory, len(registry))
	for k, v := range registry {
		saved[k] = v
	}

	registry = map[string]Factory{}

	t.Cleanup(func() { registry = saved })
}

func TestRegisterAndLookup(t *testing.T) {
	resetRegistry(t)

	want := &stubSource{name: "foo"}

	Register("foo", func(_ ControlDeps) (EventSource, error) { return want, nil })

	f, ok := Lookup("foo")
	require.True(t, ok)

	got, err := f(ControlDeps{Log: zerolog.Nop()})
	require.NoError(t, err)
	assert.Same(t, want, got)
}

func TestLookupMissing(t *testing.T) {
	resetRegistry(t)

	_, ok := Lookup("nope")
	assert.False(t, ok)
}

func TestNames(t *testing.T) {
	resetRegistry(t)

	Register("a", func(_ ControlDeps) (EventSource, error) { return &stubSource{}, nil })
	Register("b", func(_ ControlDeps) (EventSource, error) { return &stubSource{}, nil })

	names := Names()
	assert.ElementsMatch(t, []string{"a", "b"}, names)
}

func TestRegisterDuplicatePanics(t *testing.T) {
	resetRegistry(t)

	Register("dup", func(_ ControlDeps) (EventSource, error) { return &stubSource{}, nil })

	assert.Panics(t, func() {
		Register("dup", func(_ ControlDeps) (EventSource, error) { return &stubSource{}, nil })
	})
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	resetRegistry(t)

	assert.Panics(t, func() {
		Register("", func(_ ControlDeps) (EventSource, error) { return &stubSource{}, nil })
	})
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	resetRegistry(t)

	assert.Panics(t, func() {
		Register("nilfac", nil)
	})
}
