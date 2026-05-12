package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrInvalidPasswordInput is returned when a username or password contains
// characters that would break the chpasswd input format ("\n", "\r", or ":").
var ErrInvalidPasswordInput = errors.New("username or password contains invalid characters")

// PasswordSetter updates a user's password in the system authentication store.
// Implementations are safe for concurrent use.
type PasswordSetter interface {
	SetPassword(ctx context.Context, username, newPassword string) error
}

// commonChpasswdPaths lists the paths chpasswd typically lives at on
// OpenWrt-derived targets when the daemon's PATH happens to be narrow
// enough that exec.LookPath misses it. Tried in order before falling
// back to passwd(1).
var commonChpasswdPaths = []string{ //nolint:gochecknoglobals // package-level constant
	"/usr/sbin/chpasswd",
	"/sbin/chpasswd",
	"/bin/chpasswd",
	"/usr/bin/chpasswd",
}

// commonPasswdPaths is the same list for passwd(1). passwd is the
// fallback when the firmware build did not include the busybox
// chpasswd applet.
var commonPasswdPaths = []string{ //nolint:gochecknoglobals // package-level constant
	"/usr/bin/passwd",
	"/bin/passwd",
	"/usr/sbin/passwd",
	"/sbin/passwd",
}

// ChpasswdSetter updates the local shadow password database. It first
// tries chpasswd(8) (busybox applet on OpenWrt) and falls back to
// passwd(1) when chpasswd isn't installed in the firmware build. Both
// utilities accept credentials on stdin so neither password nor
// username ever reach an argv that could be inspected via /proc.
type ChpasswdSetter struct {
	// Path overrides the chpasswd executable path. When empty, the
	// resolver tries exec.LookPath("chpasswd"), then a small list of
	// common absolute paths, then falls back to passwd(1). Tests use
	// this to point at a harmless stand-in (e.g. /bin/cat) without
	// needing chpasswd installed.
	Path string

	// PasswdPath overrides the passwd(1) fallback path with the same
	// rules as Path.
	PasswdPath string
}

// SetPassword validates the inputs and invokes chpasswd or passwd.
// A non-zero exit or any stderr output is wrapped into the returned
// error.
func (c *ChpasswdSetter) SetPassword(ctx context.Context, username, newPassword string) error {
	if err := validatePasswordInput(username, newPassword); err != nil {
		return err
	}

	if path := resolveTool(c.Path, "chpasswd", commonChpasswdPaths); path != "" {
		return runChpasswd(ctx, path, username, newPassword)
	}

	if path := resolveTool(c.PasswdPath, "passwd", commonPasswdPaths); path != "" {
		return runPasswd(ctx, path, username, newPassword)
	}

	return fmt.Errorf("neither chpasswd nor passwd is available on this device")
}

// resolveTool returns a usable absolute path for `name`, checking (in
// order): the explicit override, the process PATH, and a list of known
// install paths. Returns "" if none of those resolve to an executable.
func resolveTool(override, name string, fallbacks []string) string {
	if override != "" {
		return override
	}

	if resolved, err := exec.LookPath(name); err == nil {
		return resolved
	}

	for _, p := range fallbacks {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	return ""
}

// runChpasswd invokes chpasswd with `user:password\n` on stdin.
//
// path is either an operator-provided override or one of the resolved
// tool paths from resolveTool — never user input. Credentials are
// piped over stdin so they never appear on argv.
func runChpasswd(ctx context.Context, path, username, newPassword string) error {
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, path) //nolint:gosec // trusted path from resolver; credentials on stdin not argv
	cmd.Stdin = strings.NewReader(username + ":" + newPassword + "\n")
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chpasswd: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// runPasswd invokes passwd(1) with the password supplied twice on
// stdin (matching busybox's "Enter new UNIX password / Retype" prompt).
// Used when chpasswd is unavailable.
//
// path is either an operator-provided override or one of the resolved
// tool paths from resolveTool — never user input. The username travels
// on argv but is sanitized by validatePasswordInput.
func runPasswd(ctx context.Context, path, username, newPassword string) error {
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, path, username) //nolint:gosec // trusted path from resolver; sanitized username on argv, password on stdin
	cmd.Stdin = strings.NewReader(newPassword + "\n" + newPassword + "\n")
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("passwd: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// validatePasswordInput rejects strings containing characters that would
// break the `user:password\n` line format chpasswd reads on stdin.
func validatePasswordInput(username, newPassword string) error {
	if username == "" {
		return ErrInvalidPasswordInput
	}

	if strings.ContainsAny(username, "\n\r:") {
		return ErrInvalidPasswordInput
	}

	if strings.ContainsAny(newPassword, "\n\r:") {
		return ErrInvalidPasswordInput
	}

	return nil
}
