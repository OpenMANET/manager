package sysupgrade

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// SysupgradeRunner executes the sysupgrade(1) binary with a set of
// options against an image. Implementations are expected to detach the
// child via setsid+nohup so the upgrade survives the daemon being
// SIGTERM'd by sysupgrade's tree walk.
type SysupgradeRunner interface {
	// Run launches sysupgrade and returns the detached child PID. The
	// call returns as soon as the wrapper shell exits — the upgrade
	// continues in the background. logPath is the file that captures
	// the child's stdout+stderr.
	Run(ctx context.Context, imagePath, logPath string, opts SysupgradeOptions) (int, error)

	// Preflight runs "sysupgrade -T <imagePath>" synchronously and
	// returns nil when the binary exits successfully. On failure the
	// returned error wraps the captured short stderr line so it can be
	// surfaced to the operator.
	Preflight(ctx context.Context, imagePath string) error
}

// ExecSysupgradeRunner is the production implementation. It invokes
// sysupgrade through "/usr/bin/setsid /bin/sh -c 'nohup ... &'" so that
// the child is in its own session, immune to SIGHUP, and outlives the
// daemon when sysupgrade kills its parent process tree.
type ExecSysupgradeRunner struct {
	// BinaryPath is the path to the sysupgrade binary; default
	// "/sbin/sysupgrade".
	BinaryPath string
	// SetsidPath is the path to the setsid binary; default
	// "/usr/bin/setsid".
	SetsidPath string
	// ShellPath is the path to the POSIX shell; default "/bin/sh".
	ShellPath string
}

func (r *ExecSysupgradeRunner) binaryPath() string {
	if r.BinaryPath != "" {
		return r.BinaryPath
	}

	return "/sbin/sysupgrade"
}

func (r *ExecSysupgradeRunner) setsidPath() string {
	if r.SetsidPath != "" {
		return r.SetsidPath
	}

	return "/usr/bin/setsid"
}

func (r *ExecSysupgradeRunner) shellPath() string {
	if r.ShellPath != "" {
		return r.ShellPath
	}

	return "/bin/sh"
}

// Run implements SysupgradeRunner.
func (r *ExecSysupgradeRunner) Run(ctx context.Context, imagePath, logPath string, opts SysupgradeOptions) (int, error) {
	args, err := opts.ToArgs()
	if err != nil {
		return 0, err
	}

	args = append(args, imagePath)

	script := fmt.Sprintf(
		"nohup %s %s >%s 2>&1 </dev/null &\necho $!",
		shellQuote(r.binaryPath()),
		shellQuoteArgs(args),
		shellQuote(logPath),
	)

	cmd := exec.CommandContext(ctx, r.setsidPath(), r.shellPath(), "-c", script)

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			return 0, fmt.Errorf("sysupgrade runner: setsid wrapper exited %d: %s",
				exitErr.ExitCode(), string(exitErr.Stderr))
		}

		return 0, fmt.Errorf("sysupgrade runner: %w", err)
	}

	pidStr := strings.TrimSpace(string(out))

	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("sysupgrade runner: malformed pid %q", pidStr)
	}

	return pid, nil
}

// Preflight implements SysupgradeRunner. It invokes "sysupgrade -T
// <imagePath>" in the foreground, capturing stdout+stderr. The exec
// fails with a wrapped exit-error string when sysupgrade rejects the
// image; the operator sees that string in the staged-image card.
func (r *ExecSysupgradeRunner) Preflight(ctx context.Context, imagePath string) error {
	cmd := exec.CommandContext(ctx, r.binaryPath(), "-T", imagePath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		short := firstNonEmptyLine(out)
		if short == "" {
			short = err.Error()
		}

		return fmt.Errorf("sysupgrade preflight: %s", short)
	}

	return nil
}

// firstNonEmptyLine returns the first non-blank line of b trimmed of
// trailing whitespace, or "" when b contains only whitespace. Used to
// shorten sysupgrade's multi-line stderr into a single operator-facing
// status line.
func firstNonEmptyLine(b []byte) string {
	for line := range strings.SplitSeq(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

// shellQuote returns s wrapped in single quotes, with any embedded single
// quotes escaped via the standard '\” technique.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}

	if !shellNeedsQuote(s) {
		return s
	}

	const quote = "'"

	var b strings.Builder

	b.Grow(len(s) + 2)
	b.WriteString(quote)

	for _, r := range s {
		if r == '\'' {
			b.WriteString(`'\''`)

			continue
		}

		b.WriteRune(r)
	}

	b.WriteString(quote)

	return b.String()
}

// shellQuoteArgs returns the argv joined by spaces with each element
// shell-quoted.
func shellQuoteArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}

	return strings.Join(parts, " ")
}

// shellNeedsQuote returns true when s contains a character that the
// POSIX shell would interpret if left unquoted. The whitelist mirrors
// the conservative "letters, digits, and a tiny set of safe symbols"
// classification used by GNU shellutils.
func shellNeedsQuote(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '/' || r == '.' || r == '_' || r == '-' || r == '+' || r == '=' || r == ':' || r == ',':
		default:
			return true
		}
	}

	return false
}
