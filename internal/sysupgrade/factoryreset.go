package sysupgrade

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/openmanet/openmanetd/internal/system"
)

// Sentinel errors for the factory-reset flow. These are translated to
// ConnectRPC error codes by the handler's mapManagerError; the same
// strings round-trip back to the operator unchanged.
var (
	// ErrFactoryResetNotCapable is returned when the running OS is
	// not eligible for factory reset (no overlay + rootfs_data, or
	// firstboot not installed). Carries the FailedPrecondition code.
	ErrFactoryResetNotCapable = errors.New("sysupgrade: device cannot perform factory reset")

	// ErrFactoryResetHostnameUnknown is returned when the daemon
	// can't determine the device's current hostname so the typed
	// confirmation cannot be validated. Carries FailedPrecondition.
	ErrFactoryResetHostnameUnknown = errors.New("sysupgrade: device hostname is empty; cannot validate factory reset confirmation")

	// ErrFactoryResetHostnameMismatch is returned when the operator's
	// confirm_hostname doesn't match the device's current hostname.
	// Carries InvalidArgument.
	ErrFactoryResetHostnameMismatch = errors.New("sysupgrade: confirm_hostname does not match device hostname")
)

// FactoryResetRunner launches the OpenWrt firstboot helper. The
// production implementation invokes "/sbin/firstboot -r -y", which
// wipes the overlay and reboots the device synchronously via the
// kernel reboot(2) syscall — taking openmanetd with it.
type FactoryResetRunner interface {
	Run(ctx context.Context) error
}

// ExecFactoryResetRunner is the production implementation. firstboot
// itself is a 3-line shell wrapper around /sbin/jffs2reset; the `-r`
// flag arms the reboot and `-y` skips the interactive confirmation.
type ExecFactoryResetRunner struct {
	// BinaryPath is the path to the firstboot helper; default
	// "/sbin/firstboot".
	BinaryPath string
}

func (r *ExecFactoryResetRunner) binaryPath() string {
	if r.BinaryPath != "" {
		return r.BinaryPath
	}

	return "/sbin/firstboot"
}

// Run launches firstboot in fire-and-forget mode. The child will wipe
// the overlay (≈2 seconds on a Pi 4 squashfs+overlay image), then call
// reboot(2). By the time it would have returned to userland, the
// kernel is already taking openmanetd down. We Start() and Release()
// rather than Wait(): there is no normal exit to wait for.
//
// The supplied ctx is not propagated to the child via CommandContext —
// firstboot must outlive the request that triggered it (the wrapper
// would otherwise SIGKILL the child the moment the RPC handler
// returns). We pass context.Background() so the child is fully
// detached from request lifetime.
func (r *ExecFactoryResetRunner) Run(_ context.Context) error {
	cmd := exec.CommandContext(context.Background(), r.binaryPath(), "-r", "-y")
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("factory reset runner: %w", err)
	}

	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("factory reset runner: release: %w", err)
	}

	return nil
}

// GetFactoryResetCapability returns a copy of the capability decision,
// with the device hostname filled in from SysInfoProvider so the UI
// can surface it as the typed-confirmation target. Always returns a
// non-nil pointer; on detection error the result is non-capable with
// the error in Reason.
func (m *Manager) GetFactoryResetCapability() *system.FactoryResetCapability {
	if m.factoryResetCap == nil {
		return &system.FactoryResetCapability{Reason: "factory reset provider not configured"}
	}

	cap, err := m.factoryResetCap.GetFactoryResetCapability()
	if err != nil || cap == nil {
		out := &system.FactoryResetCapability{Reason: "capability detection failed"}
		if err != nil {
			out.Reason = err.Error()
		}

		return out
	}

	if hn, hnErr := m.sysInfo.GetHostname(); hnErr == nil {
		cap.Hostname = hn
	}

	return cap
}

// PerformFactoryReset validates the operator's typed confirmation
// against the device's current hostname, then triggers
// FactoryResetRunner. The runner is fire-and-forget — by the time
// this method's caller writes the response, the kernel may already
// be rebooting.
//
// Errors:
//   - ErrFactoryResetNotCapable      — overlay/rootfs_data missing or firstboot unavailable
//   - ErrFactoryResetHostnameUnknown — sysinfo couldn't read /etc/hostname
//   - ErrFactoryResetHostnameMismatch — typed confirmation didn't match
//   - ErrBusy                         — a sysupgrade is in flight
func (m *Manager) PerformFactoryReset(_ context.Context, confirmHostname string) error {
	if m.factoryResetRunner == nil {
		return ErrFactoryResetNotCapable
	}

	cap := m.GetFactoryResetCapability()
	if !cap.Capable {
		return fmt.Errorf("%w: %s", ErrFactoryResetNotCapable, cap.Reason)
	}

	actual, err := m.sysInfo.GetHostname()
	if err != nil || strings.TrimSpace(actual) == "" {
		return ErrFactoryResetHostnameUnknown
	}

	if !hostnamesMatch(confirmHostname, actual) {
		return ErrFactoryResetHostnameMismatch
	}

	m.mu.Lock()
	busy := m.progress.Phase == PhaseDownloading ||
		m.progress.Phase == PhaseVerifying ||
		m.progress.Phase == PhaseUpgrading
	m.mu.Unlock()

	if busy {
		return ErrBusy
	}

	m.log.Warn().
		Str("hostname", actual).
		Str("firstboot", cap.FirstbootPath).
		Msg("sysupgrade: triggering factory reset (overlay wipe + reboot)")

	if err := m.factoryResetRunner.Run(context.Background()); err != nil {
		m.log.Error().Err(err).Msg("sysupgrade: factory reset runner failed to launch")

		return fmt.Errorf("factory reset: %w", err)
	}

	return nil
}

// hostnamesMatch returns true when typed and actual refer to the same
// hostname after trimming whitespace and lowercasing both sides.
// Empty strings on either side are never a match — defends against a
// device with an unset hostname accepting any input.
func hostnamesMatch(typed, actual string) bool {
	t := strings.ToLower(strings.TrimSpace(typed))
	a := strings.ToLower(strings.TrimSpace(actual))

	if t == "" || a == "" {
		return false
	}

	return t == a
}
