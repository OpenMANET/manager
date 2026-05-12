// Package logs provides system-log and kernel-ring-buffer readers backed
// by the busybox logread and dmesg applets. Providers are thin wrappers
// around exec.CommandContext; no content interpretation is performed.
package logs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ErrPermissionDenied is returned when the source binary exited non-zero
// with a stderr message indicating the caller lacks the privileges
// required to read it. This is the typical failure mode for `dmesg` on
// hosts where kernel.dmesg_restrict=1 and the daemon is not root /
// lacks CAP_SYSLOG. Handlers map this to CodeFailedPrecondition so the
// UI can render a clean "source unavailable" banner.
var ErrPermissionDenied = errors.New("log source: permission denied")

// defaultMaxLines caps the per-call response size when a caller passes 0
// or a negative value. Handlers typically enforce the cap upstream via
// proto validation, but the provider applies a floor to avoid unbounded
// allocation if called directly.
const defaultMaxLines uint32 = 1000

// LogProvider retrieves a bounded tail of a text-based log source.
// Implementations fork a busybox applet per call; callers should apply a
// context timeout so a wedged source cannot block indefinitely.
type LogProvider interface {
	// Fetch reads the source and returns at most maxLines lines in
	// oldest-first order. When the source emitted more than maxLines,
	// the returned Snapshot has Truncated=true and Lines is the tail
	// (most recent maxLines entries).
	Fetch(ctx context.Context, maxLines uint32) (*Snapshot, error)
}

// Snapshot is the result of a single Fetch call.
type Snapshot struct {
	// CollectedAt is when the provider invoked the source binary.
	CollectedAt time.Time

	// Lines contains the source output, oldest-first.
	Lines []string

	// Truncated is true when Lines was clipped to honor the maxLines
	// cap; the dropped lines were the older ones.
	Truncated bool
}

// LogreadProvider execs busybox `logread`, which dumps the OpenWRT logd
// ring buffer. The zero value runs the "logread" binary on PATH.
type LogreadProvider struct {
	// Now is an overrideable clock for tests. When nil, time.Now is used.
	Now func() time.Time

	// Binary is the command to exec. Empty means "logread" (PATH-resolved).
	Binary string

	// Args are extra arguments appended after the default args. Typically
	// empty in production.
	Args []string
}

// Fetch runs the configured logread binary and parses its output.
func (p *LogreadProvider) Fetch(ctx context.Context, maxLines uint32) (*Snapshot, error) {
	bin := p.Binary
	if bin == "" {
		bin = "logread"
	}

	return runAndParse(ctx, bin, p.Args, maxLines, p.now())
}

func (p *LogreadProvider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}

	return time.Now()
}

// DmesgProvider execs `dmesg` and dumps the kernel ring buffer. The
// zero value runs the "dmesg" binary on PATH with no arguments — the
// portable choice across busybox dmesg (OpenWrt) and util-linux dmesg
// (typical Linux). Output uses kernel-uptime timestamps in the
// `[seconds.microseconds]` format both implementations emit by default.
type DmesgProvider struct {
	// Now is an overrideable clock for tests. When nil, time.Now is used.
	Now func() time.Time

	// Binary is the command to exec. Empty means "dmesg" (PATH-resolved).
	Binary string

	// Args overrides the default arguments. When nil, the provider runs
	// dmesg with no arguments (busybox-compatible). Production
	// deployments should leave this nil unless the host is known to
	// have util-linux dmesg with extra flags worth passing.
	Args []string
}

// Fetch runs the configured dmesg binary and parses its output.
func (p *DmesgProvider) Fetch(ctx context.Context, maxLines uint32) (*Snapshot, error) {
	bin := p.Binary
	if bin == "" {
		bin = "dmesg"
	}

	return runAndParse(ctx, bin, p.Args, maxLines, p.now())
}

func (p *DmesgProvider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}

	return time.Now()
}

// runAndParse forks bin with args, splits stdout into lines, and caps the
// result to maxLines by keeping the tail. exec errors are returned
// wrapped with %w so callers can use errors.Is to classify them. When
// the process exits non-zero with stderr matching a known permission
// pattern (e.g. dmesg on a host with kernel.dmesg_restrict=1), the
// error chain includes ErrPermissionDenied. exec.ErrNotFound passes
// through for the missing-binary case.
func runAndParse(ctx context.Context, bin string, args []string, maxLines uint32, collectedAt time.Time) (*Snapshot, error) {
	if maxLines == 0 {
		maxLines = defaultMaxLines
	}

	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // fixed binary name, args from our own config
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if isPermissionDenied(err, stderrText) {
			return nil, fmt.Errorf("%s: %s: %w", bin, stderrText, ErrPermissionDenied)
		}

		if stderrText != "" {
			return nil, fmt.Errorf("%s exec: %w (%s)", bin, err, stderrText)
		}

		return nil, fmt.Errorf("%s exec: %w", bin, err)
	}

	lines := splitLines(out)

	truncated := false

	if uint32(len(lines)) > maxLines {
		lines = lines[uint32(len(lines))-maxLines:]
		truncated = true
	}

	return &Snapshot{
		CollectedAt: collectedAt,
		Lines:       lines,
		Truncated:   truncated,
	}, nil
}

// isPermissionDenied returns true when the exec error chain plus the
// captured stderr text indicate the caller lacks privileges to read
// the source. Covers the common kernel.dmesg_restrict and
// CAP_SYSLOG-missing cases. Match is case-insensitive against a small
// set of phrases emitted by busybox dmesg, util-linux dmesg, and
// logread on locked-down hosts.
func isPermissionDenied(err error, stderr string) bool {
	if errors.Is(err, exec.ErrNotFound) {
		// Caller-side classification handles missing binary separately.
		return false
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}

	if stderr == "" {
		return false
	}

	lower := strings.ToLower(stderr)

	return strings.Contains(lower, "operation not permitted") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "not permitted")
}

// splitLines splits b on '\n', strips the trailing empty element that a
// final newline produces, and preserves any embedded '\r' characters
// verbatim (operators occasionally see CRLF in syslog forwarders).
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}

	// Trim a single trailing newline so we don't emit a spurious empty
	// last line. Leave any other trailing whitespace alone.
	if b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}

	if len(b) == 0 {
		return nil
	}

	n := bytes.Count(b, []byte{'\n'}) + 1

	out := make([]string, 0, n)

	start := 0

	for i := 0; i < len(b); i++ {
		if b[i] == '\n' {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}

	out = append(out, string(b[start:]))

	return out
}
