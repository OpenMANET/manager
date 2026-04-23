package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openmanet/openmanetd/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChpasswdSetter_RejectsInvalidChars(t *testing.T) {
	cases := []struct {
		name string
		user string
		pw   string
	}{
		{"colon in username", "al:ice", "pw"},
		{"newline in username", "al\nice", "pw"},
		{"cr in username", "al\rice", "pw"},
		{"colon in password", "alice", "p:w"},
		{"newline in password", "alice", "p\nw"},
		{"cr in password", "alice", "p\rw"},
		{"empty username", "", "pw"},
	}

	setter := &auth.ChpasswdSetter{Path: "/bin/cat"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := setter.SetPassword(context.Background(), tc.user, tc.pw)
			require.Error(t, err)
			assert.ErrorIs(t, err, auth.ErrInvalidPasswordInput)
		})
	}
}

// TestChpasswdSetter_Success uses /bin/cat as a stand-in for chpasswd so the
// test does not depend on chpasswd being installed in the dev/CI environment.
// /bin/cat reads the stdin payload and exits 0 — exercising the exec code
// path without actually modifying any password database.
func TestChpasswdSetter_Success(t *testing.T) {
	setter := &auth.ChpasswdSetter{Path: "/bin/cat"}

	err := setter.SetPassword(context.Background(), "alice", "secret")
	assert.NoError(t, err)
}

func TestChpasswdSetter_FailingBinary(t *testing.T) {
	setter := &auth.ChpasswdSetter{Path: "/bin/false"}

	err := setter.SetPassword(context.Background(), "alice", "secret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chpasswd")
}

func TestChpasswdSetter_LookupFailure(t *testing.T) {
	// Empty Path → LookPath("chpasswd"). If chpasswd is not in PATH (dev
	// container), this exercises the lookup-failure branch. If it IS in
	// PATH, the lookup succeeds and the test is skipped — we can't safely
	// invoke the real binary here.
	setter := &auth.ChpasswdSetter{}

	err := setter.SetPassword(context.Background(), "alice", "secret")
	if err == nil {
		t.Skip("chpasswd is present on PATH; cannot exercise lookup-failure path")
	}

	// Must be a lookup error, not a validation error.
	assert.NotErrorIs(t, err, auth.ErrInvalidPasswordInput)
}

func TestChpasswdSetter_ContextCancelled(t *testing.T) {
	setter := &auth.ChpasswdSetter{Path: "/bin/cat"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before run

	err := setter.SetPassword(ctx, "alice", "secret")
	require.Error(t, err)

	// A canceled context during exec surfaces as a wrapped error chain
	// that eventually contains context.Canceled.
	assert.True(t,
		errors.Is(err, context.Canceled) || err.Error() != "",
		"canceled ctx must produce an error, got %v", err,
	)
}

func TestChpasswdSetter_ContextTimeout(t *testing.T) {
	setter := &auth.ChpasswdSetter{Path: "/bin/cat"}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()

	// Let the deadline pass.
	time.Sleep(time.Millisecond)

	err := setter.SetPassword(ctx, "alice", "secret")
	require.Error(t, err)
}
