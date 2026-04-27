package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeStub creates an executable shell stub at <dir>/<name> that writes
// its argv (after the script path) to <dir>/<name>.args and exits with
// the supplied code. The stub lets a test assert the exact init.d
// command shape that InitDReloader produced without hitting the host
// filesystem.
func writeStub(t *testing.T, dir, name string, exitCode int) string {
	t.Helper()

	path := filepath.Join(dir, name)
	argsFile := filepath.Join(dir, name+".args")

	script := "#!/bin/sh\n" +
		"echo \"$@\" > " + argsFile + "\n" +
		"exit " + map[bool]string{true: "0", false: "1"}[exitCode == 0]

	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))

	return argsFile
}

func TestInitDReloader_Reload_InvokesScriptWithReloadArg(t *testing.T) {
	dir := t.TempDir()
	argsFile := writeStub(t, dir, "wireless", 0)

	r := &InitDReloader{InitDir: dir}
	err := r.Reload(context.Background(), "wireless")
	require.NoError(t, err)

	got, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, "reload\n", string(got))
}

func TestInitDReloader_Restart_InvokesScriptWithRestartArg(t *testing.T) {
	dir := t.TempDir()
	argsFile := writeStub(t, dir, "network", 0)

	r := &InitDReloader{InitDir: dir}
	err := r.Restart(context.Background(), "network")
	require.NoError(t, err)

	got, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, "restart\n", string(got))
}

func TestInitDReloader_Reload_PropagatesNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "firewall", 1)

	r := &InitDReloader{InitDir: dir}
	err := r.Reload(context.Background(), "firewall")
	assert.Error(t, err)
}

func TestInitDReloader_Restart_PropagatesNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	writeStub(t, dir, "dhcp", 2)

	r := &InitDReloader{InitDir: dir}
	err := r.Restart(context.Background(), "dhcp")
	assert.Error(t, err)
}

func TestInitDReloader_RejectsInvalidServiceName(t *testing.T) {
	r := &InitDReloader{InitDir: t.TempDir()}

	cases := []string{
		"",
		"foo bar",
		"foo/bar",
		"../passwd",
		"foo;rm -rf /",
		"foo\nbar",
		"foo$bar",
	}

	for _, name := range cases {
		t.Run(strings.ReplaceAll(name, "\n", "\\n"), func(t *testing.T) {
			err := r.Reload(context.Background(), name)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidServiceName), "Reload should reject %q", name)

			err = r.Restart(context.Background(), name)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidServiceName), "Restart should reject %q", name)
		})
	}
}

func TestInitDReloader_AcceptsValidNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"wireless", "mesh11sd", "openmanet-webui", "foo_bar"} {
		writeStub(t, dir, name, 0)
	}

	r := &InitDReloader{InitDir: dir}

	for _, name := range []string{"wireless", "mesh11sd", "openmanet-webui", "foo_bar"} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, r.Reload(context.Background(), name))
			require.NoError(t, r.Restart(context.Background(), name))
		})
	}
}

func TestInitDReloader_ContextCancellation(t *testing.T) {
	dir := t.TempDir()

	// Stub that sleeps for 10s so we can cancel mid-flight.
	path := filepath.Join(dir, "slow")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nsleep 10\n"), 0o755))

	r := &InitDReloader{InitDir: dir}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := r.Reload(ctx, "slow")
	assert.Error(t, err, "cancelled context should produce an error")
}

func TestInitDReloader_DefaultInitDirWhenEmpty(t *testing.T) {
	r := &InitDReloader{}
	// Default path is /etc/init.d, which won't exist for arbitrary names
	// in the dev container; we just assert the path is computed (not
	// that it succeeds).
	err := r.Reload(context.Background(), "definitely-not-a-real-service")
	assert.Error(t, err)
}
