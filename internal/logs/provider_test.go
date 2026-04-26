package logs

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedNow returns a deterministic clock for assertions on CollectedAt.
func fixedNow() time.Time {
	return time.Date(2026, 4, 25, 14, 30, 0, 0, time.UTC)
}

func TestLogreadProvider_Fetch_HappyPath(t *testing.T) {
	t.Parallel()

	p := &LogreadProvider{
		Binary: "/bin/sh",
		Args:   []string{"-c", "printf 'line1\\nline2\\nline3\\n'"},
		Now:    fixedNow,
	}

	snap, err := p.Fetch(context.Background(), 10)
	require.NoError(t, err)
	require.NotNil(t, snap)

	assert.Equal(t, []string{"line1", "line2", "line3"}, snap.Lines)
	assert.False(t, snap.Truncated)
	assert.Equal(t, fixedNow(), snap.CollectedAt)
}

func TestLogreadProvider_Fetch_Truncates(t *testing.T) {
	t.Parallel()

	// Emit 10 lines, cap to 3. We expect the last 3 (most recent).
	p := &LogreadProvider{
		Binary: "/bin/sh",
		Args:   []string{"-c", "for i in 1 2 3 4 5 6 7 8 9 10; do echo line$i; done"},
		Now:    fixedNow,
	}

	snap, err := p.Fetch(context.Background(), 3)
	require.NoError(t, err)

	assert.Equal(t, []string{"line8", "line9", "line10"}, snap.Lines)
	assert.True(t, snap.Truncated)
}

func TestLogreadProvider_Fetch_Empty(t *testing.T) {
	t.Parallel()

	p := &LogreadProvider{
		Binary: "/bin/sh",
		Args:   []string{"-c", "true"},
	}

	snap, err := p.Fetch(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, snap.Lines)
	assert.False(t, snap.Truncated)
}

func TestLogreadProvider_Fetch_BinaryNotFound(t *testing.T) {
	t.Parallel()

	// Use a PATH-resolved miss so exec returns an *exec.Error wrapping
	// ErrNotFound — the handler layer classifies on this specifically.
	p := &LogreadProvider{Binary: "definitely-not-a-real-bin-xyzzy"}

	_, err := p.Fetch(context.Background(), 10)
	require.Error(t, err)

	var execErr *exec.Error
	assert.True(t, errors.As(err, &execErr), "expected *exec.Error, got %T: %v", err, err)
	assert.True(t, errors.Is(err, exec.ErrNotFound), "expected errors.Is ErrNotFound, got %v", err)
}

func TestLogreadProvider_Fetch_ContextCancelled(t *testing.T) {
	t.Parallel()

	// A command that sleeps longer than our context will be killed.
	p := &LogreadProvider{
		Binary: "/bin/sh",
		Args:   []string{"-c", "sleep 5"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := p.Fetch(ctx, 10)
	require.Error(t, err)
}

func TestLogreadProvider_Fetch_DefaultBinaryEmpty(t *testing.T) {
	t.Parallel()

	// Zero value uses "logread", which may or may not exist on the test
	// host. We don't assert success — we assert no panic and that a
	// missing binary surfaces as an error (dev containers won't have it).
	p := &LogreadProvider{}
	_, err := p.Fetch(context.Background(), 10)
	// In CI the binary won't exist; that's the expected path.
	if err == nil {
		return
	}

	assert.Contains(t, err.Error(), "exec")
}

func TestDmesgProvider_Fetch_HappyPath(t *testing.T) {
	t.Parallel()

	p := &DmesgProvider{
		Binary: "/bin/sh",
		Args:   []string{"-c", "printf 'kern1\\nkern2\\n'"},
		Now:    fixedNow,
	}

	snap, err := p.Fetch(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, []string{"kern1", "kern2"}, snap.Lines)
	assert.Equal(t, fixedNow(), snap.CollectedAt)
}

func TestDmesgProvider_Fetch_DefaultArgsAreNone(t *testing.T) {
	t.Parallel()

	// When Args is nil, the provider must run dmesg with no arguments
	// for busybox compatibility. Verify by pointing Binary at /bin/echo
	// (which prints its args, one per line if any) — with no args, the
	// output should be a single empty line, which splitLines() drops to
	// produce no lines at all.
	p := &DmesgProvider{
		Binary: "/bin/echo",
		// Args nil intentionally — must default to no args.
	}

	snap, err := p.Fetch(context.Background(), 10)
	require.NoError(t, err)
	assert.Empty(t, snap.Lines, "default Args should be empty so dmesg runs with no flags")
}

func TestDmesgProvider_Fetch_ExplicitArgs(t *testing.T) {
	t.Parallel()

	// Explicit args are passed through verbatim.
	p := &DmesgProvider{
		Binary: "/bin/sh",
		Args:   []string{"-c", "echo hi"},
	}

	snap, err := p.Fetch(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"hi"}, snap.Lines)
}

func TestRunAndParse_PermissionDenied(t *testing.T) {
	t.Parallel()

	// Simulate dmesg's typical failure on a locked-down host: exit 1 with
	// "Operation not permitted" on stderr.
	snap, err := runAndParse(
		context.Background(),
		"/bin/sh",
		[]string{"-c", "echo 'dmesg: read kernel buffer failed: Operation not permitted' >&2; exit 1"},
		10,
		fixedNow(),
	)
	require.Nil(t, snap)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPermissionDenied)
	assert.Contains(t, err.Error(), "Operation not permitted")
}

func TestRunAndParse_NonPermissionExitIncludesStderr(t *testing.T) {
	t.Parallel()

	// Non-permission failures should not be classified as permission and
	// should still surface the stderr text in the error message so
	// operators can debug.
	_, err := runAndParse(
		context.Background(),
		"/bin/sh",
		[]string{"-c", "echo 'something else broke' >&2; exit 2"},
		10,
		fixedNow(),
	)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrPermissionDenied)
	assert.Contains(t, err.Error(), "something else broke")
}

func TestIsPermissionDenied(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"operation-not-permitted", "dmesg: Operation not permitted", true},
		{"permission-denied", "logread: permission denied opening /dev/log", true},
		{"not-permitted-shorter", "not permitted", true},
		{"unrelated-error", "command not found", false},
		{"empty-stderr", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fakeExitErr := &exec.ExitError{}
			got := isPermissionDenied(fakeExitErr, tc.stderr)
			assert.Equal(t, tc.want, got)
		})
	}

	// ErrNotFound must never classify as permission-denied.
	assert.False(t, isPermissionDenied(exec.ErrNotFound, "Operation not permitted"))
}

func TestRunAndParse_MaxLinesZeroUsesDefault(t *testing.T) {
	t.Parallel()

	snap, err := runAndParse(
		context.Background(),
		"/bin/sh",
		[]string{"-c", "echo one; echo two"},
		0,
		fixedNow(),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, snap.Lines)
	assert.False(t, snap.Truncated)
}

func TestSplitLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"just-newline", "\n", nil},
		{"one-no-trailing", "a", []string{"a"}},
		{"one-with-trailing", "a\n", []string{"a"}},
		{"two-with-trailing", "a\nb\n", []string{"a", "b"}},
		{"two-no-trailing", "a\nb", []string{"a", "b"}},
		{"blank-lines-preserved", "a\n\nb\n", []string{"a", "", "b"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := splitLines([]byte(tc.in))
			assert.Equal(t, tc.want, got)
		})
	}
}
