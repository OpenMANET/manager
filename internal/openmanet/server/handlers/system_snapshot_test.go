package handlers_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/openmanet/openmanetd/internal/system"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSysInfo struct {
	hostnameCalls atomic.Int64
	kernelCalls   atomic.Int64
	uptimeCalls   atomic.Int64
	memCalls      atomic.Int64
	loadCalls     atomic.Int64
	overlayCalls  atomic.Int64

	hostname     string
	kernel       string
	architecture string
	uptime       time.Duration
	memory       *system.MemoryInfo
	cpuLoad      float32
	overlay      *system.OverlayUsage

	hostnameErr error
	kernelErr   error
	uptimeErr   error
	memErr      error
	loadErr     error
	overlayErr  error
}

func (f *fakeSysInfo) GetHostname() (string, error) {
	f.hostnameCalls.Add(1)

	return f.hostname, f.hostnameErr
}

func (f *fakeSysInfo) GetKernelVersion() (string, error) {
	f.kernelCalls.Add(1)

	return f.kernel, f.kernelErr
}

func (f *fakeSysInfo) GetArchitecture() (string, error) { return f.architecture, nil }

func (f *fakeSysInfo) GetUptime() (time.Duration, error) {
	f.uptimeCalls.Add(1)

	return f.uptime, f.uptimeErr
}

func (f *fakeSysInfo) GetMemoryInfo() (*system.MemoryInfo, error) {
	f.memCalls.Add(1)

	return f.memory, f.memErr
}

func (f *fakeSysInfo) GetCPULoadPercent() (float32, error) {
	f.loadCalls.Add(1)

	return f.cpuLoad, f.loadErr
}

func (f *fakeSysInfo) GetOverlayUsage() (*system.OverlayUsage, error) {
	f.overlayCalls.Add(1)

	return f.overlay, f.overlayErr
}

type fakeServiceChecker struct {
	calls atomic.Int64
	resp  []system.ServiceStatus
	err   error
}

func (f *fakeServiceChecker) CheckServices(_ context.Context, names []string) ([]system.ServiceStatus, error) {
	f.calls.Add(1)

	if f.err != nil {
		return nil, f.err
	}

	if f.resp != nil {
		return f.resp, nil
	}
	// Default: echo one status per name.
	out := make([]system.ServiceStatus, 0, len(names))
	for _, n := range names {
		out = append(out, system.ServiceStatus{Name: n, State: system.ServiceStateRunning})
	}

	return out, nil
}

func TestSystemSnapshotter_StartWarmsCacheAndServesFromIt(t *testing.T) {
	inner := &fakeSysInfo{
		kernel:       "5.15.0",
		architecture: "aarch64",
		uptime:       42 * time.Minute,
		memory:       &system.MemoryInfo{TotalBytes: 1024, AvailableBytes: 512},
		cpuLoad:      12.5,
		overlay:      &system.OverlayUsage{TotalBytes: 1000, UsedBytes: 100},
	}
	svcs := &fakeServiceChecker{}

	s := handlers.NewSystemSnapshotter(
		zerolog.Nop(),
		inner,
		svcs,
		[]string{"openmanetd", "dnsmasq"},
		time.Hour,
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.Start(ctx)

	// Sanity-check the accessor plumbing against the warm snapshot.
	gotKernel, err := s.GetKernelVersion()
	require.NoError(t, err)
	assert.Equal(t, "5.15.0", gotKernel)

	gotArch, err := s.GetArchitecture()
	require.NoError(t, err)
	assert.Equal(t, "aarch64", gotArch)

	gotUptime, err := s.GetUptime()
	require.NoError(t, err)
	assert.Equal(t, 42*time.Minute, gotUptime)

	gotMem, err := s.GetMemoryInfo()
	require.NoError(t, err)
	assert.Same(t, inner.memory, gotMem)

	gotLoad, err := s.GetCPULoadPercent()
	require.NoError(t, err)
	assert.InDelta(t, 12.5, gotLoad, 0.0001)

	gotOver, err := s.GetOverlayUsage()
	require.NoError(t, err)
	assert.Same(t, inner.overlay, gotOver)

	// Reading the cached values many times must not drive additional
	// inner calls — Start already captured one snapshot.
	before := inner.kernelCalls.Load()

	for range 100 {
		_, _ = s.GetKernelVersion()
		_, _ = s.GetUptime()
		_, _ = s.GetMemoryInfo()
		_, _ = s.GetCPULoadPercent()
		_, _ = s.GetOverlayUsage()
	}

	assert.Equal(t, before, inner.kernelCalls.Load(),
		"cached accessors must not hit the inner provider on read")
}

func TestSystemSnapshotter_HostnameIsAlwaysLive(t *testing.T) {
	inner := &fakeSysInfo{hostname: "node-42"}
	s := handlers.NewSystemSnapshotter(zerolog.Nop(), inner, &fakeServiceChecker{}, nil, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)

	before := inner.hostnameCalls.Load()

	for range 3 {
		name, err := s.GetHostname()
		require.NoError(t, err)
		assert.Equal(t, "node-42", name)
	}

	assert.Equal(t, before+3, inner.hostnameCalls.Load(),
		"GetHostname must delegate live to the inner provider")
}

func TestSystemSnapshotter_AccessorsFallBackBeforeFirstRefresh(t *testing.T) {
	inner := &fakeSysInfo{kernel: "6.1.0", uptime: time.Minute}
	s := handlers.NewSystemSnapshotter(zerolog.Nop(), inner, &fakeServiceChecker{}, nil, time.Hour)

	// No Start() — simulate a cold cache.
	gotKernel, err := s.GetKernelVersion()
	require.NoError(t, err)
	assert.Equal(t, "6.1.0", gotKernel)

	gotUptime, err := s.GetUptime()
	require.NoError(t, err)
	assert.Equal(t, time.Minute, gotUptime)
}

func TestSystemSnapshotter_AccessorsSurfaceErrors(t *testing.T) {
	wantErr := errors.New("proc read failed")
	inner := &fakeSysInfo{uptimeErr: wantErr, memErr: wantErr, loadErr: wantErr, overlayErr: wantErr, kernelErr: wantErr}
	s := handlers.NewSystemSnapshotter(zerolog.Nop(), inner, &fakeServiceChecker{}, nil, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)

	_, err := s.GetKernelVersion()
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetUptime()
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetMemoryInfo()
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetCPULoadPercent()
	assert.ErrorIs(t, err, wantErr)

	_, err = s.GetOverlayUsage()
	assert.ErrorIs(t, err, wantErr)
}

func TestSystemSnapshotter_CheckServicesServesCachedForConfiguredSet(t *testing.T) {
	svcs := &fakeServiceChecker{
		resp: []system.ServiceStatus{
			{Name: "openmanetd", State: system.ServiceStateRunning},
			{Name: "dnsmasq", State: system.ServiceStateRunning},
		},
	}
	s := handlers.NewSystemSnapshotter(
		zerolog.Nop(),
		&fakeSysInfo{},
		svcs,
		[]string{"openmanetd", "dnsmasq"},
		time.Hour,
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)

	before := svcs.calls.Load()

	for range 5 {
		got, err := s.CheckServices(ctx, []string{"openmanetd", "dnsmasq"})
		require.NoError(t, err)
		assert.Len(t, got, 2)
	}

	assert.Equal(t, before, svcs.calls.Load(),
		"matching service-set reads must be served from the cache")
}

func TestSystemSnapshotter_CheckServicesFallsThroughOnDifferentSet(t *testing.T) {
	svcs := &fakeServiceChecker{}
	s := handlers.NewSystemSnapshotter(
		zerolog.Nop(),
		&fakeSysInfo{},
		svcs,
		[]string{"openmanetd", "dnsmasq"},
		time.Hour,
	)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.Start(ctx)

	before := svcs.calls.Load()
	_, err := s.CheckServices(ctx, []string{"other"})
	require.NoError(t, err)
	assert.Equal(t, before+1, svcs.calls.Load(),
		"different service-set must fall through to the inner checker")
}

func TestSystemSnapshotter_BackgroundLoopStopsOnContextCancel(t *testing.T) {
	inner := &fakeSysInfo{}
	s := handlers.NewSystemSnapshotter(
		zerolog.Nop(),
		inner,
		&fakeServiceChecker{},
		nil,
		5*time.Millisecond,
	)

	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)

	require.Eventually(
		t,
		func() bool { return inner.kernelCalls.Load() >= 2 },
		500*time.Millisecond,
		2*time.Millisecond,
	)

	cancel()

	settled := inner.kernelCalls.Load()

	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(t, inner.kernelCalls.Load()-settled, int64(1),
		"goroutine should stop refreshing after context cancel")
}
