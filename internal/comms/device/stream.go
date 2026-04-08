package device

import (
	"fmt"

	"github.com/gen2brain/malgo"
)

// AudioStream wraps a malgo device with a minimal lifecycle interface so
// that audio streams can be started, stopped, and closed without exposing
// the underlying binding to the rest of the codebase.
type AudioStream interface {
	Start() error
	Stop() error
	Close() error
}

// malgoStream satisfies AudioStream by wrapping *malgo.Device. It is
// unexported so the malgo binding does not leak into the public surface;
// callers obtain instances via NewMalgoStream.
type malgoStream struct {
	d *malgo.Device
}

// NewMalgoStream wraps d as an AudioStream. Returns nil if d is nil.
func NewMalgoStream(d *malgo.Device) AudioStream {
	if d == nil {
		return nil
	}

	return &malgoStream{d: d}
}

// Start activates the device. For playback devices this begins playback;
// for capture devices it begins recording.
func (m *malgoStream) Start() error {
	if err := m.d.Start(); err != nil {
		return fmt.Errorf("malgo stream start: %w", err)
	}

	return nil
}

// Stop puts the device to sleep without releasing its resources. Use
// Start to resume. Stop is idempotent with respect to Close: if the
// device is already stopped this is a no-op at the miniaudio layer.
func (m *malgoStream) Stop() error {
	if err := m.d.Stop(); err != nil {
		return fmt.Errorf("malgo stream stop: %w", err)
	}

	return nil
}

// Close uninitializes the underlying malgo device. The wrapped device
// must not be used after Close returns. Close also implicitly stops the
// device, so callers do not need to invoke Stop first.
func (m *malgoStream) Close() error {
	m.d.Uninit()

	return nil
}
