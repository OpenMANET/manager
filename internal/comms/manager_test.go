package comms

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeStartFunc returns a startFunc that blocks until ctx is canceled and
// records whether it was called.
func fakeStartFunc(called *atomic.Bool) func(*CommsConfig) startFunc {
	return func(_ *CommsConfig) startFunc {
		return func(ctx context.Context) error {
			called.Store(true)
			<-ctx.Done()

			return nil
		}
	}
}

func newTestManager() (*CommsManager, *atomic.Bool) {
	var called atomic.Bool

	m := &CommsManager{
		logger:  zerolog.Nop(),
		buildFn: func() *CommsConfig { return &CommsConfig{} },
		startFn: fakeStartFunc(&called),
	}

	return m, &called
}

func TestCommsManager_EnableDisable(t *testing.T) {
	m, called := newTestManager()

	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error: %v", err)
	}

	// Give goroutine a moment to start
	time.Sleep(20 * time.Millisecond)

	if !called.Load() {
		t.Fatal("expected start function to be called")
	}

	if !m.IsRunning() {
		t.Fatal("expected IsRunning() to be true after Enable")
	}

	m.Disable()

	if m.IsRunning() {
		t.Fatal("expected IsRunning() to be false after Disable")
	}
}

func TestCommsManager_EnableIdempotent(t *testing.T) {
	m, _ := newTestManager()

	if err := m.Enable(); err != nil {
		t.Fatalf("first Enable() error: %v", err)
	}

	defer m.Disable()

	// Second Enable should be a no-op
	if err := m.Enable(); err != nil {
		t.Fatalf("second Enable() error: %v", err)
	}

	if !m.IsRunning() {
		t.Fatal("expected IsRunning() to be true")
	}
}

func TestCommsManager_DisableIdempotent(t *testing.T) {
	m, _ := newTestManager()

	// Disable when not running — should not panic or block
	m.Disable()

	if m.IsRunning() {
		t.Fatal("expected IsRunning() to be false")
	}
}

func TestCommsManager_EnableAfterDisable(t *testing.T) {
	m, called := newTestManager()

	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error: %v", err)
	}

	m.Disable()

	called.Store(false)

	// Re-enable
	if err := m.Enable(); err != nil {
		t.Fatalf("re-Enable() error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if !called.Load() {
		t.Fatal("expected start function to be called on re-enable")
	}

	if !m.IsRunning() {
		t.Fatal("expected IsRunning() to be true after re-enable")
	}

	m.Disable()
}

func TestCommsManager_StartError(t *testing.T) {
	errBoom := errors.New("boom")

	m := &CommsManager{
		logger:  zerolog.Nop(),
		buildFn: func() *CommsConfig { return &CommsConfig{} },
		startFn: func(_ *CommsConfig) startFunc {
			return func(_ context.Context) error {
				return errBoom
			}
		},
	}

	// Enable should succeed (error is async in goroutine)
	if err := m.Enable(); err != nil {
		t.Fatalf("Enable() error: %v", err)
	}

	// Wait for the goroutine to finish
	time.Sleep(50 * time.Millisecond)

	// The done channel should be closed since start returned
	m.Disable()

	if m.IsRunning() {
		t.Fatal("expected IsRunning() to be false after Disable")
	}
}
