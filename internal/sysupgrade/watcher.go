package sysupgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// persistentFailureLogName is the basename used for the persistent
// post-failure breadcrumb under Manager.persistentLogDir. /tmp is
// tmpfs on OpenWrt and the runtime sysupgrade log evaporates on the
// next reboot — the operator needs *somewhere* on the persistent
// overlay to read after the device comes back. This file is small
// (capped at sysupgradeLogTailBytes) and overwritten on each
// failure, so it doesn't grow without bound.
const persistentFailureLogName = "last-failure.log"

// watcherPollInterval is the cadence at which watchSysupgradeChild
// checks whether the detached sysupgrade child is still alive.
const watcherPollInterval = 2 * time.Second

// watcherMaxDuration caps how long the watcher will wait before giving
// up. The longest legitimate sysupgrade run we've observed is ~5 min on
// MIPS, so 30 min is a generous upper bound that still recovers the
// state machine if something pathological keeps the PID alive forever.
const watcherMaxDuration = 30 * time.Minute

// sysupgradeLogTailBytes is how much of the sysupgrade log file is
// captured into Progress.LogTail when the child exits without
// rebooting. ~8 KiB is enough to show the last few status lines plus
// the failure reason without pushing payloads into multi-megabyte
// territory.
const sysupgradeLogTailBytes = 8 * 1024

// watchSysupgradeChild polls the detached sysupgrade child PID and
// surfaces a PhaseFailed transition if the child exits without
// rebooting the device.
//
// Three outcomes are possible:
//
//  1. Sysupgrade succeeds and reboots the device. The kernel kills
//     openmanetd as part of the standard sysupgrade flow (it kills
//     every process holding files open on the rootfs). The watcher
//     goroutine dies with the daemon — no false-positive transition
//     because there is no opportunity to publish one.
//
//  2. Sysupgrade fails before reaching the "kill processes" stage.
//     The PID disappears while openmanetd is still alive; the watcher
//     reads the tail of the sysupgrade log file and publishes
//     PhaseFailed with that content as Progress.LogTail. This is the
//     case the operator cares about — without this watcher the UI
//     would sit at PhaseUpgrading forever.
//
//  3. ctx is canceled (operator cancels the upgrade or the manager
//     is being torn down). Watcher exits without publishing.
//
// On the cooperative-multitasking platforms we target (Linux), `kill
// -0 <pid>` is the cheapest way to ask "does this PID still exist."
// We do not adopt the child (it lives in a different session courtesy
// of setsid), so Wait() is not available; signal-based polling is the
// portable alternative.
func (m *Manager) watchSysupgradeChild(ctx context.Context, pid int, logPath, assetName string) {
	if pid <= 0 {
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		m.log.Warn().Err(err).Int("pid", pid).Msg("sysupgrade: watcher cannot resolve child pid")

		return
	}

	deadline := time.NewTimer(watcherMaxDuration)
	defer deadline.Stop()

	ticker := time.NewTicker(watcherPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.log.Debug().Int("pid", pid).Msg("sysupgrade: watcher exiting (ctx done)")

			return
		case <-deadline.C:
			m.log.Warn().Int("pid", pid).Dur("after", watcherMaxDuration).
				Msg("sysupgrade: watcher giving up; child still alive past deadline")

			return
		case <-ticker.C:
			if processStillRunning(proc) {
				continue
			}

			tail := readSysupgradeLogTail(logPath, sysupgradeLogTailBytes)

			persistentPath := m.writePersistentFailureLog(assetName, pid, tail)

			m.log.Warn().
				Int("pid", pid).
				Str("log", logPath).
				Str("persistent_log", persistentPath).
				Str("asset", assetName).
				Str("tail", tail).
				Msg("sysupgrade: child exited without rebooting")

			hint := "Inspect logread for the sysupgrade-tagged messages."
			if persistentPath != "" {
				hint = fmt.Sprintf("Inspect %s on the device — it survives reboots.", persistentPath)
			}

			m.publishUpgradeFailure(
				"sysupgrade child exited without rebooting",
				hint,
				tail,
			)

			return
		}
	}
}

// processStillRunning returns true when the supplied process exists.
// On Linux, signal 0 performs the existence check without delivering
// a signal: the call returns nil for a live process, ESRCH for a dead
// one, and EPERM when the process exists but we lack permission to
// signal it (which still answers "yes, alive"). We treat EPERM as
// "alive" defensively even though openmanetd already runs as root.
func processStillRunning(proc *os.Process) bool {
	err := proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}

	if errors.Is(err, syscall.EPERM) {
		return true
	}

	return false
}

// readSysupgradeLogTail returns the trailing tailBytes of the
// sysupgrade log file as a UTF-8 string. The result is trimmed to the
// last complete line so the UI doesn't show a half-line at the start.
// Returns "" when the file is missing or unreadable; the caller
// surfaces a fallback message in that case.
func readSysupgradeLogTail(path string, tailBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return ""
	}

	size := fi.Size()
	if size <= 0 {
		return ""
	}

	offset := int64(0)
	if size > tailBytes {
		offset = size - tailBytes
	}

	if _, seekErr := f.Seek(offset, io.SeekStart); seekErr != nil {
		return ""
	}

	buf, err := io.ReadAll(f)
	if err != nil {
		return ""
	}

	out := string(buf)
	if offset > 0 {
		// Drop the partial first line so the tail starts on a clean
		// line boundary.
		if i := strings.IndexByte(out, '\n'); i >= 0 && i+1 < len(out) {
			out = out[i+1:]
		}
	}

	return strings.TrimRight(out, "\n\r\t ")
}

// writePersistentFailureLog dumps the captured sysupgrade tail to a
// fixed file under m.persistentLogDir so the operator can read it
// after the device reboots. Returns the path on success, "" on any
// failure (best-effort: never blocks PhaseFailed publication on a
// disk-write error).
//
// Format: a short header (asset, pid, timestamp) followed by the
// captured tail. Each call overwrites — only the most recent failure
// is kept.
func (m *Manager) writePersistentFailureLog(assetName string, pid int, tail string) string {
	if m.persistentLogDir == "" {
		return ""
	}

	if err := os.MkdirAll(m.persistentLogDir, 0o755); err != nil {
		m.log.Warn().Err(err).Str("dir", m.persistentLogDir).
			Msg("sysupgrade: persistent log dir mkdir failed")

		return ""
	}

	path := filepath.Join(m.persistentLogDir, persistentFailureLogName)

	header := fmt.Sprintf(
		"# sysupgrade failure breadcrumb\n# asset:     %s\n# child_pid: %d\n# captured:  %s\n\n",
		assetName, pid, time.Now().UTC().Format(time.RFC3339),
	)

	body := []byte(header + tail + "\n")

	if err := os.WriteFile(path, body, 0o600); err != nil {
		m.log.Warn().Err(err).Str("path", path).
			Msg("sysupgrade: persistent failure log write failed")

		return ""
	}

	return path
}

// publishUpgradeFailure transitions the manager to PhaseFailed while
// preserving the current ReleaseTag/AssetName/ChildPID and attaching
// a non-empty LogTail. setPhase only carries Message+ErrMsg; the log
// tail rides on Progress separately so the UI can render it in a
// scrollable block instead of a one-liner alert.
func (m *Manager) publishUpgradeFailure(message, errMsg, logTail string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.progress
	m.progress = Progress{
		Phase:      PhaseFailed,
		ReleaseTag: prev.ReleaseTag,
		AssetName:  prev.AssetName,
		BytesDone:  prev.BytesDone,
		BytesTotal: prev.BytesTotal,
		Percent:    prev.Percent,
		ChildPID:   prev.ChildPID,
		Message:    message,
		ErrMsg:     errMsg,
		LogTail:    logTail,
		UpdatedAt:  time.Now(),
	}

	m.publishLocked(m.progress)
}
