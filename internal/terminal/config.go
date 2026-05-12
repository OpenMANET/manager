// Package terminal provides a single-session PTY backed by a shell, bridged
// to a WebSocket connection. It is consumed by the frontend HTTP server to
// expose an interactive root shell to operators authenticated against the
// Web UI session store.
package terminal

import "time"

// Config controls how the manager spawns and supervises the shell session.
//
// Zero-value Config is invalid; use DefaultConfig() and override fields.
type Config struct {
	// Shell is the absolute path of the binary spawned at session start.
	// Default is /bin/login, matching OpenWrt's LuCI web terminal: login
	// prints /etc/banner, prompts for credentials, then execs the user's
	// login shell. Operators who want to skip the login prompt can
	// override to /bin/ash, /bin/sh, or /bin/bash.
	Shell string

	// Env is the environment passed to the shell process. The manager
	// always overrides TERM=xterm-256color and never inherits the parent
	// environment wholesale, to keep the shell's environment predictable.
	Env []string

	// GraceShutdown is the time the manager waits between sending SIGTERM
	// and SIGKILL to the shell process group when the session is being
	// torn down. Two seconds is enough for ash/bash to flush; longer
	// risks blocking the operator's tab close.
	GraceShutdown time.Duration
}

// DefaultConfig returns a Config wired for OpenWrt + BusyBox.
func DefaultConfig() Config {
	return Config{
		Shell: "/bin/login",
		Env: []string{
			"TERM=xterm-256color",
			"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
			"HOME=/root",
			"LANG=C.UTF-8",
		},
		GraceShutdown: 2 * time.Second,
	}
}
