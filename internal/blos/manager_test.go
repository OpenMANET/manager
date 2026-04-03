package blos

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// mockTailscaleAuthClient records calls and returns configured errors.
type mockTailscaleAuthClient struct {
	mu        sync.Mutex
	err       error
	startOpts ipn.Options
	calls     int
}

func (m *mockTailscaleAuthClient) Start(_ context.Context, opts ipn.Options) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	m.startOpts = opts

	return m.err
}

func (m *mockTailscaleAuthClient) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.calls
}

func (m *mockTailscaleAuthClient) getStartOpts() ipn.Options {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.startOpts
}

// mockInitDService records calls and returns configured results.
type mockInitDService struct {
	mu           sync.Mutex
	isEnabledVal bool
	isEnabledErr error
	enableErr    error
	enableCalls  int
	isRunningVal bool
	isRunningErr error
	startErr     error
	startCalls   int
}

func (m *mockInitDService) IsEnabled(_ context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.isEnabledVal, m.isEnabledErr
}

func (m *mockInitDService) Enable(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.enableCalls++

	return m.enableErr
}

func (m *mockInitDService) IsRunning(_ context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.isRunningVal, m.isRunningErr
}

func (m *mockInitDService) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startCalls++

	return m.startErr
}

func (m *mockInitDService) getEnableCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enableCalls
}

func (m *mockInitDService) getStartCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.startCalls
}

// runningInitDService returns a mockInitDService that reports already enabled and running.
func runningInitDService() *mockInitDService {
	return &mockInitDService{isEnabledVal: true, isRunningVal: true}
}

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

// ── ConfigureAndEnable ────────────────────────────────────────────────────────

// runningStatusClient returns a MockStatusClient that always reports "Running".
func runningStatusClient() *MockStatusClient {
	sc := &MockStatusClient{}
	sc.SetStatus(&ipnstate.Status{BackendState: "Running"})

	return sc
}

func newTestManagerWithAuth(t *testing.T, auth *mockTailscaleAuthClient, initD InitDService, createFn func(*config.Config, zerolog.Logger) (*BLOS, error)) *BLOSManager {
	t.Helper()

	return &BLOSManager{
		cfg:          &config.Config{},
		logger:       zerolog.Nop(),
		authClient:   auth,
		statusClient: runningStatusClient(),
		initDService: initD,
		createFn:     createFn,
	}
}

func TestBLOSManager_ConfigureAndEnable_Success(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.True(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls())

	opts := auth.getStartOpts()
	assert.Equal(t, "tskey-abc123", opts.AuthKey)
	assert.Nil(t, opts.UpdatePrefs)
}

func TestBLOSManager_ConfigureAndEnable_WithLoginServer(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "https://hs.example.com")
	require.NoError(t, err)
	assert.True(t, m.IsRunning())

	opts := auth.getStartOpts()
	require.NotNil(t, opts.UpdatePrefs)
	assert.Equal(t, "https://hs.example.com", opts.UpdatePrefs.ControlURL)
	assert.True(t, opts.UpdatePrefs.WantRunning)
}

func TestBLOSManager_ConfigureAndEnable_WithoutLoginServer(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)

	opts := auth.getStartOpts()
	assert.Nil(t, opts.UpdatePrefs)
}

func TestBLOSManager_ConfigureAndEnable_AuthFailure(t *testing.T) {
	auth := &mockTailscaleAuthClient{err: errors.New("auth failed")}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-bad", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale authentication failed")
	assert.False(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls())
}

func TestBLOSManager_ConfigureAndEnable_CreateFnFailure(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), func(_ *config.Config, _ zerolog.Logger) (*BLOS, error) {
		return nil, errors.New("create failed")
	})

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create failed")
	assert.False(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls()) // auth was called before createFn
}

func TestBLOSManager_ConfigureAndEnable_Idempotent(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)

	err = m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)

	assert.True(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls(), "auth client should only be called once")
}

func TestBLOSManager_ConfigureAndEnable_NotGatewayMode(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, runningInitDService(), notGatewayCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway mode")
	assert.False(t, m.IsRunning())
}

// ── InitDService integration with ConfigureAndEnable ─────────────────────────

func TestBLOSManager_ConfigureAndEnable_EnablesService(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: false, isRunningVal: true}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.Equal(t, 1, initD.getEnableCalls(), "Enable should be called when not enabled")
	assert.Equal(t, 1, auth.getCalls(), "auth should proceed after enabling service")
}

func TestBLOSManager_ConfigureAndEnable_SkipsEnableWhenAlreadyEnabled(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: true, isRunningVal: true}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.Equal(t, 0, initD.getEnableCalls(), "Enable should not be called when already enabled")
}

func TestBLOSManager_ConfigureAndEnable_StartsService(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: true, isRunningVal: false}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.Equal(t, 1, initD.getStartCalls(), "Start should be called when not running")
	assert.Equal(t, 1, auth.getCalls(), "auth should proceed after starting service")
}

func TestBLOSManager_ConfigureAndEnable_SkipsStartWhenAlreadyRunning(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: true, isRunningVal: true}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.Equal(t, 0, initD.getStartCalls(), "Start should not be called when already running")
}

func TestBLOSManager_ConfigureAndEnable_EnableFailure(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: false, isRunningVal: true, enableErr: errors.New("enable failed")}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enable tailscale service")
	assert.Equal(t, 0, auth.getCalls(), "auth should not be called when enable fails")
	assert.False(t, m.IsRunning())
}

func TestBLOSManager_ConfigureAndEnable_StartFailure(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: true, isRunningVal: false, startErr: errors.New("start failed")}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start tailscale service")
	assert.Equal(t, 0, auth.getCalls(), "auth should not be called when start fails")
	assert.False(t, m.IsRunning())
}

func TestBLOSManager_ConfigureAndEnable_IsEnabledCheckFailure(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledErr: errors.New("check failed")}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check tailscale service enabled")
	assert.Equal(t, 0, auth.getCalls())
}

func TestBLOSManager_ConfigureAndEnable_IsRunningCheckFailure(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	initD := &mockInitDService{isEnabledVal: true, isRunningErr: errors.New("check failed")}
	m := newTestManagerWithAuth(t, auth, initD, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check tailscale service running")
	assert.Equal(t, 0, auth.getCalls())
}

func TestBLOSManager_ConfigureAndEnable_NilInitDService(t *testing.T) {
	auth := &mockTailscaleAuthClient{}
	m := newTestManagerWithAuth(t, auth, nil, successCreateFn)

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.True(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls(), "auth should proceed when initDService is nil")
}

// ── waitForTailscaleReady ────────────────────────────────────────────────────

func TestWaitForTailscaleReady_ImmediatelyRunning(t *testing.T) {
	sc := runningStatusClient()
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	err := m.waitForTailscaleReady(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sc.GetCallCount())
}

func TestWaitForTailscaleReady_TransitionsFromStarting(t *testing.T) {
	var calls atomic.Int32
	sc := &MockStatusClient{
		statusFunc: func(_ context.Context) (*ipnstate.Status, error) {
			n := calls.Add(1)
			if n <= 3 {
				return &ipnstate.Status{BackendState: "Starting"}, nil
			}

			return &ipnstate.Status{BackendState: "Running"}, nil
		},
	}
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	err := m.waitForTailscaleReady(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, int(calls.Load()), 4, "should have polled at least 4 times")
}

func TestWaitForTailscaleReady_TransitionsFromNeedsLogin(t *testing.T) {
	var calls atomic.Int32
	sc := &MockStatusClient{
		statusFunc: func(_ context.Context) (*ipnstate.Status, error) {
			n := calls.Add(1)
			if n <= 3 {
				return &ipnstate.Status{BackendState: "NeedsLogin"}, nil
			}

			return &ipnstate.Status{BackendState: "Running"}, nil
		},
	}
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	err := m.waitForTailscaleReady(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, int(calls.Load()), 4, "should have polled through NeedsLogin until Running")
}

func TestWaitForTailscaleReady_NeedsLoginTimesOut(t *testing.T) {
	sc := &MockStatusClient{}
	sc.SetStatus(&ipnstate.Status{BackendState: "NeedsLogin"})
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := m.waitForTailscaleReady(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for tailscale")
	assert.Contains(t, err.Error(), "NeedsLogin")
}

func TestWaitForTailscaleReady_Timeout(t *testing.T) {
	sc := &MockStatusClient{}
	sc.SetStatus(&ipnstate.Status{BackendState: "Starting"})
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	// Use a short-lived context to avoid waiting the full 30s
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := m.waitForTailscaleReady(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for tailscale")
	assert.Contains(t, err.Error(), "Starting")
}

func TestWaitForTailscaleReady_StatusError(t *testing.T) {
	sc := &MockStatusClient{}
	sc.SetError(errors.New("socket not found"))
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	err := m.waitForTailscaleReady(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check tailscale status")
	assert.Contains(t, err.Error(), "socket not found")
}

func TestBLOSManager_ConfigureAndEnable_WaitsForReady(t *testing.T) {
	var calls atomic.Int32
	sc := &MockStatusClient{
		statusFunc: func(_ context.Context) (*ipnstate.Status, error) {
			n := calls.Add(1)
			if n <= 2 {
				return &ipnstate.Status{BackendState: "Starting"}, nil
			}

			return &ipnstate.Status{BackendState: "Running"}, nil
		},
	}

	auth := &mockTailscaleAuthClient{}
	m := &BLOSManager{
		cfg:          &config.Config{},
		logger:       zerolog.Nop(),
		authClient:   auth,
		statusClient: sc,
		initDService: runningInitDService(),
		createFn:     successCreateFn,
	}

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.True(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls())
	assert.GreaterOrEqual(t, int(calls.Load()), 3, "should have polled until Running")
}

func TestBLOSManager_ConfigureAndEnable_TailscaleNotReady(t *testing.T) {
	sc := &MockStatusClient{}
	sc.SetStatus(&ipnstate.Status{BackendState: "NeedsLogin"})

	auth := &mockTailscaleAuthClient{}
	m := &BLOSManager{
		cfg:          &config.Config{},
		logger:       zerolog.Nop(),
		authClient:   auth,
		statusClient: sc,
		initDService: runningInitDService(),
		createFn:     successCreateFn,
	}

	// Use a short context so the test doesn't wait the full 30s
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	err := m.ConfigureAndEnable(ctx, "tskey-abc123", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tailscale not ready after authentication")
	assert.False(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls(), "auth should have been called before status check")
}

// ── waitForTailscaleDaemon ───────────────────────────────────────────────────

func TestWaitForTailscaleDaemon_ImmediatelyAvailable(t *testing.T) {
	sc := runningStatusClient()
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	err := m.waitForTailscaleDaemon(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, sc.GetCallCount())
}

func TestWaitForTailscaleDaemon_BecomesAvailableAfterRetries(t *testing.T) {
	var calls atomic.Int32
	sc := &MockStatusClient{
		statusFunc: func(_ context.Context) (*ipnstate.Status, error) {
			n := calls.Add(1)
			if n <= 3 {
				return nil, errors.New("dial unix /var/run/tailscale/tailscaled.sock: connect: no such file or directory")
			}

			return &ipnstate.Status{BackendState: "Stopped"}, nil
		},
	}
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	err := m.waitForTailscaleDaemon(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, int(calls.Load()), 4, "should have retried until daemon was reachable")
}

func TestWaitForTailscaleDaemon_Timeout(t *testing.T) {
	sc := &MockStatusClient{}
	sc.SetError(errors.New("no such file or directory"))
	m := &BLOSManager{logger: zerolog.Nop(), statusClient: sc}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := m.waitForTailscaleDaemon(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout waiting for tailscale daemon")
}

func TestBLOSManager_ConfigureAndEnable_WaitsForDaemon(t *testing.T) {
	// Simulate: daemon socket unavailable for 2 polls, then comes up as "Stopped"
	// (logged out), then auth succeeds, then backend transitions to "Running".
	var calls atomic.Int32
	sc := &MockStatusClient{
		statusFunc: func(_ context.Context) (*ipnstate.Status, error) {
			n := calls.Add(1)

			switch {
			case n <= 2:
				// Daemon not yet listening
				return nil, errors.New("dial unix: no such file or directory")
			case n == 3:
				// Daemon up, pre-auth (waitForTailscaleDaemon succeeds here)
				return &ipnstate.Status{BackendState: "Stopped"}, nil
			default:
				// Post-auth polls (waitForTailscaleReady)
				return &ipnstate.Status{BackendState: "Running"}, nil
			}
		},
	}

	auth := &mockTailscaleAuthClient{}
	m := &BLOSManager{
		cfg:          &config.Config{},
		logger:       zerolog.Nop(),
		authClient:   auth,
		statusClient: sc,
		initDService: runningInitDService(),
		createFn:     successCreateFn,
	}

	err := m.ConfigureAndEnable(context.Background(), "tskey-abc123", "")
	require.NoError(t, err)
	assert.True(t, m.IsRunning())
	assert.Equal(t, 1, auth.getCalls())
	// At least 3 calls for daemon wait + 1 for ready wait
	assert.GreaterOrEqual(t, int(calls.Load()), 4)
}
