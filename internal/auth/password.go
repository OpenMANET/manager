package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// ChpasswdSetter updates the local shadow password database by invoking the
// chpasswd(8) binary and feeding it "user:password\n" on standard input.
// On OpenWrt targets this resolves to the busybox chpasswd applet.
type ChpasswdSetter struct {
	// Path overrides the chpasswd executable path. When empty, the process
	// PATH is searched (via exec.LookPath). Tests use this to point at a
	// harmless stand-in like /bin/cat without needing chpasswd installed.
	Path string
}

// SetPassword validates the inputs and invokes chpasswd. A non-zero exit or
// any stderr output is wrapped into the returned error.
func (c *ChpasswdSetter) SetPassword(ctx context.Context, username, newPassword string) error {
	if err := validatePasswordInput(username, newPassword); err != nil {
		return err
	}

	path := c.Path
	if path == "" {
		resolved, err := exec.LookPath("chpasswd")
		if err != nil {
			return fmt.Errorf("chpasswd lookup: %w", err)
		}

		path = resolved
	}

	var stderr bytes.Buffer

	// path is either the result of exec.LookPath("chpasswd") or a test-
	// only override set by the package that constructed this struct; it is
	// not user input. Credentials travel over stdin, not argv, so the
	// username and password cannot reach the shell.
	cmd := exec.CommandContext(ctx, path) // #nosec G702 -- trusted path, stdin carries credentials
	cmd.Stdin = strings.NewReader(username + ":" + newPassword + "\n")
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("chpasswd: %w (%s)", err, strings.TrimSpace(stderr.String()))
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
