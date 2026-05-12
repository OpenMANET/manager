package comms

import (
	"testing"

	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/stretchr/testify/assert"
)

// TestControlRegistry_KnownSourcesRegistered ensures every control source name
// supported by the legacy switch in buildEventSource has a corresponding
// registry entry installed by control_register.go's init().
func TestControlRegistry_KnownSourcesRegistered(t *testing.T) {
	for _, name := range []string{
		defaultCtrlSrc,
		controlSourceROIP,
		controlSourceWeb,
		defaultControlSourceNanoPTT,
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			f, ok := control.Lookup(name)
			assert.True(t, ok, "expected %q registered", name)
			assert.NotNil(t, f)
		})
	}
}

// TestCommsConfig_Validate exercises the Phase 2 Validate() method which
// gates the configured ControlSource against the registry.
func TestCommsConfig_Validate(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		cfg := &CommsConfig{ControlSource: defaultCtrlSrc}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("unknown", func(t *testing.T) {
		// "bluealsa_xevent" is preserved by normalizeControlSource but not
		// installed in the registry, so Validate must reject it.
		cfg := &CommsConfig{ControlSource: "bluealsa_xevent"}
		assert.Error(t, cfg.Validate())
	})
}
