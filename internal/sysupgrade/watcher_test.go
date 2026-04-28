package sysupgrade

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatchSysupgradeChild_DetectsEarlyExit launches a real short-lived
// `sleep 0.2` subprocess, reaps it (so the kernel forgets the PID),
// then asks the watcher to monitor that PID. The watcher must publish
// a PhaseFailed transition with the captured log tail.
func TestWatchSysupgradeChild_DetectsEarlyExit(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	logPath := filepath.Join(t.TempDir(), "sysupgrade.log")
	require.NoError(t, writeLog(logPath, "Reading partition table from bootdisk...\nfatal: corrupt image\n"))

	cmd := exec.Command("sleep", "0.1")
	require.NoError(t, cmd.Start())

	pid := cmd.Process.Pid

	// Reap before starting the watcher so signal(0) actually returns
	// ESRCH instead of treating a zombie as alive.
	require.NoError(t, cmd.Wait())

	ch, unsub := mgr.Subscribe(t.Context())
	defer unsub()

	go mgr.watchSysupgradeChild(t.Context(), pid, logPath, "openmanet.img")

	deadline := time.After(2*watcherPollInterval + 2*time.Second)

	for {
		select {
		case <-deadline:
			t.Fatal("watcher did not publish PhaseFailed within deadline")
		case ev := <-ch:
			if ev.Phase != PhaseFailed {
				continue
			}

			assert.Contains(t, ev.LogTail, "fatal: corrupt image")
			assert.Contains(t, ev.Message, "exited without rebooting")

			return
		}
	}
}

func TestWatchSysupgradeChild_IgnoresLiveProcess(t *testing.T) {
	mgr := makeManager(t, &fakeReleasesFetcher{}, &fakeRunner{}, "1.7.0")

	cmd := exec.Command("sleep", "10")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })

	pid := cmd.Process.Pid

	ch, unsub := mgr.Subscribe(t.Context())
	defer unsub()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan struct{})

	go func() {
		mgr.watchSysupgradeChild(ctx, pid, "/dev/null", "x.img")

		close(done)
	}()

	// Give the watcher a few poll cycles to confirm "still running".
	select {
	case ev := <-ch:
		t.Fatalf("watcher published unexpected event for live process: %+v", ev)
	case <-time.After(2*watcherPollInterval + 500*time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after ctx cancellation")
	}
}

func TestReadSysupgradeLogTail_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")
	require.NoError(t, writeLog(path, "first line\nsecond line\n"))

	tail := readSysupgradeLogTail(path, 8*1024)
	assert.Equal(t, "first line\nsecond line", tail)
}

func TestReadSysupgradeLogTail_TruncatesToLineBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log")

	// Write a sequence of distinguishable lines so we can assert that
	// the partial leading line is dropped.
	var sb strings.Builder
	for range 12 {
		sb.WriteString("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx LINE\n")
	}

	require.NoError(t, writeLog(path, sb.String()))

	tail := readSysupgradeLogTail(path, 200)

	// Every retained line must be a complete "xxxxxxxxxxxxxx... LINE"
	// — a partial line at the start would have been a substring like
	// "xxxxx LINE" missing some leading x's.
	for line := range strings.SplitSeq(tail, "\n") {
		assert.Equal(t, "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx LINE", line)
	}

	assert.True(t, strings.HasSuffix(tail, "LINE"))
}

func TestReadSysupgradeLogTail_MissingFile(t *testing.T) {
	tail := readSysupgradeLogTail(filepath.Join(t.TempDir(), "nope"), 1024)
	assert.Empty(t, tail)
}

func writeLog(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writeLog %q: %w", path, err)
	}

	return nil
}
