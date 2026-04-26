package sysupgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeBinary creates a tiny shell script that records its argv to a
// file and returns immediately. The script path is suitable for use as
// ExecSysupgradeRunner.BinaryPath in tests.
func writeFakeBinary(t *testing.T, dir, recordPath string) string {
	t.Helper()

	p := filepath.Join(dir, "fake-sysupgrade")
	body := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> " + recordPath + "; done\nexit 0\n"
	require.NoError(t, os.WriteFile(p, []byte(body), 0o755))

	return p
}

func TestExecSysupgradeRunner_Run(t *testing.T) {
	if _, err := os.Stat("/usr/bin/setsid"); os.IsNotExist(err) {
		t.Skip("/usr/bin/setsid not present in this environment")
	}

	dir := t.TempDir()
	recordPath := filepath.Join(dir, "argv.txt")

	bin := writeFakeBinary(t, dir, recordPath)
	logPath := filepath.Join(dir, "sysupgrade.log")
	imagePath := filepath.Join(dir, "image.bin")
	require.NoError(t, os.WriteFile(imagePath, []byte("img"), 0o644))

	r := &ExecSysupgradeRunner{BinaryPath: bin}
	pid, err := r.Run(context.Background(), imagePath, logPath, SysupgradeOptions{Verbose: true, TestOnly: true})
	require.NoError(t, err)
	assert.Greater(t, pid, 0)

	// Wait briefly for the detached child to write its argv. The child
	// is writing under setsid+nohup, so the parent shell has exited
	// already; the file usually appears within microseconds.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(recordPath); err == nil {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	got, err := os.ReadFile(recordPath)
	require.NoError(t, err, "argv record file should exist after detached child runs")

	args := strings.Split(strings.TrimSpace(string(got)), "\n")
	assert.Contains(t, args, "-T")
	assert.Contains(t, args, "-v")
	assert.Contains(t, args, imagePath)
}

func TestExecSysupgradeRunner_RejectsConflictingOptions(t *testing.T) {
	r := &ExecSysupgradeRunner{BinaryPath: "/bin/true"}
	_, err := r.Run(context.Background(), "/tmp/img", "/tmp/log", SysupgradeOptions{TestOnly: true, Force: true})
	require.Error(t, err)
}

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: "''"},
		{in: "/bin/sh", want: "/bin/sh"},
		{in: "hello world", want: "'hello world'"},
		{in: "it's", want: `'it'\''s'`},
		{in: "/tmp/log file.txt", want: "'/tmp/log file.txt'"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, shellQuote(tc.in))
		})
	}
}
