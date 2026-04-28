package network_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/openmanet/openmanetd/internal/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileSystemUCISnapshotter_RoundTripPreservesContent confirms a
// snapshot/restore cycle leaves every captured file byte-identical to
// its pre-snapshot contents, even when the post-snapshot disk state
// diverged.
func TestFileSystemUCISnapshotter_RoundTripPreservesContent(t *testing.T) {
	dir := t.TempDir()

	// Seed two config files with known contents.
	original := map[string][]byte{
		"wireless": []byte("config wifi-device 'radio0'\n\toption type 'mac80211'\n"),
		"network":  []byte("config interface 'lan'\n\toption proto 'dhcp'\n"),
	}

	for name, data := range original {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
	}

	snap := &network.FileSystemUCISnapshotter{TreePath: dir}

	captured, err := snap.Snapshot(context.Background(), []string{"wireless", "network"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"wireless", "network"}, captured.Configs())

	// Mutate disk state — simulate the wizard committing different
	// contents.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wireless"),
		[]byte("garbage\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "network"),
		[]byte("garbage\n"), 0o644))

	// Restore and confirm the original bytes are back.
	require.NoError(t, snap.Restore(context.Background(), captured))

	for name, want := range original {
		got, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.Equal(t, want, got, "%s must match original after restore", name)
	}
}

// TestFileSystemUCISnapshotter_AtomicWrite confirms a partial write
// mid-restore (simulated by passing a path on a filesystem that
// can't accept writes) doesn't truncate the target file. We can't
// easily simulate ENOSPC in a unit test, so this exercises the
// rename path: a successful restore uses a temp file + rename, which
// we observe by checking no `.uci-snapshot-*` debris is left in the
// directory afterwards.
func TestFileSystemUCISnapshotter_AtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "wireless"),
		[]byte("original\n"), 0o644))

	snap := &network.FileSystemUCISnapshotter{TreePath: dir}

	captured, err := snap.Snapshot(context.Background(), []string{"wireless"})
	require.NoError(t, err)

	// Mutate, restore, verify no temp files left.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wireless"),
		[]byte("dirty\n"), 0o644))
	require.NoError(t, snap.Restore(context.Background(), captured))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.False(t, hasPrefix(e.Name(), ".uci-snapshot-"),
			"restore must clean up temp files; found %s", e.Name())
	}
}

// TestFileSystemUCISnapshotter_MissingFileCapturedAsAbsent confirms
// a config file that doesn't exist at snapshot time is captured as
// "absent" — and Restore will remove the file if it's been created
// in the meantime.
func TestFileSystemUCISnapshotter_MissingFileCapturedAsAbsent(t *testing.T) {
	dir := t.TempDir()

	snap := &network.FileSystemUCISnapshotter{TreePath: dir}

	// Snapshot a config that doesn't exist on disk.
	captured, err := snap.Snapshot(context.Background(), []string{"luci"})
	require.NoError(t, err)
	assert.Contains(t, captured.Configs(), "luci")

	// Wizard creates the file post-snapshot.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "luci"),
		[]byte("config wizard 'wizard'\n\toption used '1'\n"), 0o644))

	// Restore must remove it.
	require.NoError(t, snap.Restore(context.Background(), captured))

	_, err = os.Stat(filepath.Join(dir, "luci"))
	assert.True(t, os.IsNotExist(err),
		"restore must remove files that were absent at snapshot time")
}

// hasPrefix is a tiny helper to avoid importing strings just for one
// use — also avoids cluttering test imports.
func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}

	return s[:len(prefix)] == prefix
}
