package sysupgrade

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmanet/openmanetd/internal/system"
)

// fakeFactoryResetCapabilityProvider returns a fixed capability struct
// (or an error). Used to wire a manager without touching real /proc.
type fakeFactoryResetCapabilityProvider struct {
	cap *system.FactoryResetCapability
	err error
}

func (f *fakeFactoryResetCapabilityProvider) GetFactoryResetCapability() (*system.FactoryResetCapability, error) {
	return f.cap, f.err
}

// fakeFactoryResetRunner records Run() invocations and lets a test
// inject a return error.
type fakeFactoryResetRunner struct {
	mu       sync.Mutex
	calls    int
	returnEr error
}

func (r *fakeFactoryResetRunner) Run(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++

	return r.returnEr
}

func (r *fakeFactoryResetRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.calls
}

// makeManagerWithReset wires a manager with the supplied factory reset
// dependencies. Other deps reuse the standard fakes from manager_test.go.
func makeManagerWithReset(
	t *testing.T,
	resetCap system.FactoryResetCapabilityProvider,
	resetRunner FactoryResetRunner,
	hostname string,
) *Manager {
	t.Helper()

	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")
	mgr.factoryResetCap = resetCap
	mgr.factoryResetRunner = resetRunner
	mgr.sysInfo = &fakeSysInfoProvider{hostname: hostname, kernel: "6.6.0", arch: "aarch64"}

	return mgr
}

func TestGetFactoryResetCapability_PopulatesHostname(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{
			Capable:           true,
			Reason:            "ok",
			OverlayMountpoint: "overlayfs:/overlay /",
			BackingFS:         "overlay",
			FirstbootPath:     "/sbin/firstboot",
		},
	}

	mgr := makeManagerWithReset(t, provider, &fakeFactoryResetRunner{}, "BCM2711-1003")

	cap := mgr.GetFactoryResetCapability()
	require.NotNil(t, cap)
	assert.True(t, cap.Capable)
	assert.Equal(t, "BCM2711-1003", cap.Hostname,
		"manager must populate hostname from SysInfoProvider so the UI does not need a second round trip")
}

func TestGetFactoryResetCapability_NilProvider(t *testing.T) {
	mgr := makeManagerWithReset(t, nil, nil, "host")

	cap := mgr.GetFactoryResetCapability()
	require.NotNil(t, cap)
	assert.False(t, cap.Capable)
	assert.Contains(t, cap.Reason, "factory reset provider not configured")
}

func TestPerformFactoryReset_HappyPath(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: true, Reason: "ok", FirstbootPath: "/sbin/firstboot"},
	}
	runner := &fakeFactoryResetRunner{}

	mgr := makeManagerWithReset(t, provider, runner, "device-1")

	require.NoError(t, mgr.PerformFactoryReset(t.Context(), "device-1"))
	assert.Equal(t, 1, runner.callCount())
}

func TestPerformFactoryReset_HostnameCaseInsensitive(t *testing.T) {
	// Trim + lowercase comparison on both sides — the typed value can
	// differ in case from the actual hostname.
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: true, Reason: "ok", FirstbootPath: "/sbin/firstboot"},
	}
	runner := &fakeFactoryResetRunner{}

	mgr := makeManagerWithReset(t, provider, runner, "BCM2711-1003")

	require.NoError(t, mgr.PerformFactoryReset(t.Context(), "  bcm2711-1003  "))
	assert.Equal(t, 1, runner.callCount())
}

func TestPerformFactoryReset_HostnameMismatch(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: true, Reason: "ok", FirstbootPath: "/sbin/firstboot"},
	}
	runner := &fakeFactoryResetRunner{}

	mgr := makeManagerWithReset(t, provider, runner, "real-name")

	err := mgr.PerformFactoryReset(t.Context(), "wrong-name")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFactoryResetHostnameMismatch))
	assert.Equal(t, 0, runner.callCount(), "runner must not run on hostname mismatch")
}

func TestPerformFactoryReset_EmptyHostnameOnDevice(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: true, Reason: "ok", FirstbootPath: "/sbin/firstboot"},
	}
	runner := &fakeFactoryResetRunner{}

	// Empty device hostname — even matching empty input must be rejected
	// to defend against a misconfigured device accepting any input.
	mgr := makeManagerWithReset(t, provider, runner, "")

	err := mgr.PerformFactoryReset(t.Context(), "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFactoryResetHostnameUnknown))
	assert.Equal(t, 0, runner.callCount())
}

func TestPerformFactoryReset_NotCapable(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: false, Reason: "no rootfs_data partition or overlayfs mount"},
	}
	runner := &fakeFactoryResetRunner{}

	mgr := makeManagerWithReset(t, provider, runner, "host")

	err := mgr.PerformFactoryReset(t.Context(), "host")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrFactoryResetNotCapable))
	assert.Contains(t, err.Error(), "no rootfs_data partition")
	assert.Equal(t, 0, runner.callCount())
}

func TestPerformFactoryReset_RejectsConcurrentUpgrade(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: true, Reason: "ok", FirstbootPath: "/sbin/firstboot"},
	}
	runner := &fakeFactoryResetRunner{}

	mgr := makeManagerWithReset(t, provider, runner, "host")

	// Park the manager in PhaseUpgrading.
	mgr.mu.Lock()
	mgr.progress.Phase = PhaseUpgrading
	mgr.mu.Unlock()

	err := mgr.PerformFactoryReset(t.Context(), "host")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBusy))
	assert.Equal(t, 0, runner.callCount())
}

func TestPerformFactoryReset_RunnerError(t *testing.T) {
	provider := &fakeFactoryResetCapabilityProvider{
		cap: &system.FactoryResetCapability{Capable: true, Reason: "ok", FirstbootPath: "/sbin/firstboot"},
	}
	runner := &fakeFactoryResetRunner{returnEr: errors.New("boom")}

	mgr := makeManagerWithReset(t, provider, runner, "host")

	err := mgr.PerformFactoryReset(t.Context(), "host")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestExecFactoryResetRunner_LaunchesAndReleases(t *testing.T) {
	// /bin/true is "/sbin/firstboot" for this test — it forks, exits 0
	// immediately, and the runner Releases the Process handle. The
	// goal is to verify that Start() is called and no error is
	// returned; we don't assert on side effects (a real firstboot
	// would reboot the host).
	r := &ExecFactoryResetRunner{BinaryPath: "/bin/true"}
	require.NoError(t, r.Run(t.Context()))
}

func TestExecFactoryResetRunner_BinaryMissing(t *testing.T) {
	r := &ExecFactoryResetRunner{BinaryPath: "/no/such/binary/here"}
	err := r.Run(t.Context())
	require.Error(t, err)

	// On Unix, a missing binary surfaces as exec.ErrNotFound or a
	// PathError wrapping ENOENT; either way the error string carries
	// the binary path.
	assert.True(t,
		errors.Is(err, exec.ErrNotFound) || assert.Contains(t, err.Error(), "no such file"),
	)
}
