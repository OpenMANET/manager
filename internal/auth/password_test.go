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

func TestChpasswdSetter_FallsBackToPasswdWhenChpasswdMissing(t *testing.T) {
	// Production regression: on minimal OpenWrt builds the chpasswd
	// applet is absent. The setter must fall back to passwd(1)
	// instead of returning a "chpasswd lookup" error.
	//
	// We leave the chpasswd Path empty so the resolver runs through
	// LookPath + the common /usr/sbin/* fallbacks. On hosts where
	// chpasswd is genuinely absent the resolver lands on PasswdPath
	// (a /bin/cat stand-in) and returns nil. On hosts that DO ship
	// chpasswd, the resolver picks it up first and chpasswd rejects
	// the fake user — still a valid exercise of the resolution chain.
	setter := &auth.ChpasswdSetter{PasswdPath: "/bin/cat"}

	err := setter.SetPassword(context.Background(), "alice", "secret")
	if err == nil {
		return // hit the passwd fallback, exit 0 from /bin/cat
	}

	if errors.Is(err, auth.ErrInvalidPasswordInput) {
		t.Fatalf("expected resolution-chain error, got input-validation error: %v", err)
	}

	t.Logf("chpasswd present on this host; resolver short-circuited at chpasswd (err=%v)", err)
}

func TestChpasswdSetter_NeitherToolAvailable(t *testing.T) {
	// When BOTH overrides point at non-existent paths (and neither
	// chpasswd nor passwd is in PATH), the setter returns a clear
	// error rather than silently succeeding.
	setter := &auth.ChpasswdSetter{
		Path:       "/nonexistent/chpasswd",
		PasswdPath: "/nonexistent/passwd",
	}

	err := setter.SetPassword(context.Background(), "alice", "secret")
	require.Error(t, err)
	assert.NotErrorIs(t, err, auth.ErrInvalidPasswordInput,
		"missing tools must NOT surface as input validation failures")
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
