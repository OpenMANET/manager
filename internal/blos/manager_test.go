package blos

import (
	"errors"
	"sync"
	"testing"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestManager creates a BLOSManager with an injectable createFn for testing.
func newTestManager(t *testing.T, createFn func(*config.Config, zerolog.Logger) (*BLOS, error)) *BLOSManager {
	t.Helper()

	return &BLOSManager{
		cfg:      &config.Config{},
		logger:   zerolog.Nop(),
		createFn: createFn,
	}
}

// successCreateFn returns a minimal BLOS instance.
func successCreateFn(_ *config.Config, _ zerolog.Logger) (*BLOS, error) {
	return &BLOS{}, nil
}

// notGatewayCreateFn simulates NewBLOS returning (nil, nil) when not in gateway mode.
func notGatewayCreateFn(_ *config.Config, _ zerolog.Logger) (*BLOS, error) {
	return nil, nil
}

func TestBLOSManager_Enable_Success(t *testing.T) {
	m := newTestManager(t, successCreateFn)

	err := m.Enable()
	require.NoError(t, err)
	assert.True(t, m.IsRunning())
	assert.NotNil(t, m.GetBLOS())
}

func TestBLOSManager_Enable_Idempotent(t *testing.T) {
	callCount := 0
	m := newTestManager(t, func(c *config.Config, l zerolog.Logger) (*BLOS, error) {
		callCount++

		return &BLOS{}, nil
	})

	err := m.Enable()
	require.NoError(t, err)

	err = m.Enable()
	require.NoError(t, err)

	assert.True(t, m.IsRunning())
	assert.Equal(t, 1, callCount, "createFn should only be called once")
}

func TestBLOSManager_Enable_NotGatewayMode(t *testing.T) {
	m := newTestManager(t, notGatewayCreateFn)

	err := m.Enable()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway mode")
	assert.False(t, m.IsRunning())
	assert.Nil(t, m.GetBLOS())
}

func TestBLOSManager_Enable_Error(t *testing.T) {
	m := newTestManager(t, func(_ *config.Config, _ zerolog.Logger) (*BLOS, error) {
		return nil, errors.New("tailscale not running")
	})

	err := m.Enable()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale not running")
	assert.False(t, m.IsRunning())
}

func TestBLOSManager_Disable_Success(t *testing.T) {
	stopCalled := false
	m := newTestManager(t, func(_ *config.Config, _ zerolog.Logger) (*BLOS, error) {
		b := &BLOS{}

		return b, nil
	})

	err := m.Enable()
	require.NoError(t, err)
	assert.True(t, m.IsRunning())

	// Replace the BLOS instance's multicastConns to verify Stop behavior
	// Stop() closes multicast connections and stops the status worker.
	// Since our test BLOS has nil statusWorker and nil multicastConns,
	// Stop() will just nil them out.
	_ = stopCalled

	m.Disable()
	assert.False(t, m.IsRunning())
	assert.Nil(t, m.GetBLOS())
}

func TestBLOSManager_Disable_Idempotent(t *testing.T) {
	m := newTestManager(t, successCreateFn)

	// Disable when not running should not panic
	m.Disable()
	assert.False(t, m.IsRunning())

	// Enable then disable twice
	err := m.Enable()
	require.NoError(t, err)

	m.Disable()
	m.Disable()
	assert.False(t, m.IsRunning())
}

func TestBLOSManager_Disable_ThenReEnable(t *testing.T) {
	callCount := 0
	m := newTestManager(t, func(c *config.Config, l zerolog.Logger) (*BLOS, error) {
		callCount++

		return &BLOS{}, nil
	})

	// First cycle
	err := m.Enable()
	require.NoError(t, err)
	assert.True(t, m.IsRunning())

	m.Disable()
	assert.False(t, m.IsRunning())

	// Second cycle
	err = m.Enable()
	require.NoError(t, err)
	assert.True(t, m.IsRunning())
	assert.Equal(t, 2, callCount, "createFn should be called once per enable cycle")
}

func TestBLOSManager_ConcurrentAccess(t *testing.T) {
	m := newTestManager(t, func(_ *config.Config, _ zerolog.Logger) (*BLOS, error) {
		return &BLOS{}, nil
	})

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 50; j++ {
				_ = m.Enable()
				_ = m.IsRunning()
				_ = m.GetBLOS()
				m.Disable()
			}
		}()
	}

	wg.Wait()
	// If we get here without race/panic, the test passes.
}

func TestBLOSManager_IsRunning_InitialState(t *testing.T) {
	m := newTestManager(t, successCreateFn)
	assert.False(t, m.IsRunning())
}

func TestBLOSManager_GetBLOS_WhenNotRunning(t *testing.T) {
	m := newTestManager(t, successCreateFn)
	assert.Nil(t, m.GetBLOS())
}
