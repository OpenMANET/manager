package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// validServiceName matches the conservative set of OpenWrt init.d script
// names: alphanumeric plus dash and underscore. Rejects path traversal
// and shell metacharacters defensively, even though all callers in the
// wizard handler use a hardcoded service list.
var validServiceName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ErrInvalidServiceName is returned when a service argument fails the
// validServiceName check.
var ErrInvalidServiceName = errors.New("invalid service name")

// ServiceReloader sends reload/restart signals to OpenWrt init.d
// services. The setup wizard handler uses one of these to nudge
// `wireless`, `network`, `dhcp`, `firewall`, `system`, and `mesh11sd`
// to re-read UCI after ApplySetup commits.
type ServiceReloader interface {
	// Reload runs `/etc/init.d/<service> reload`. Returns the command's
	// underlying exit error on non-zero exit.
	Reload(ctx context.Context, service string) error
	// Restart runs `/etc/init.d/<service> restart`. Used as the second
	// attempt by the wizard handler when Reload fails — some services
	// don't support reload and must be restarted to pick up new UCI.
	Restart(ctx context.Context, service string) error
}

// InitDReloader is the production ServiceReloader. It invokes
// `/etc/init.d/<service> <action>` via exec.CommandContext.
type InitDReloader struct {
	// InitDir is the directory containing init.d scripts. Defaults to
	// "/etc/init.d". Tests substitute a t.TempDir() that holds shell
	// stubs so command shape can be asserted without touching the host.
	InitDir string
}

func (r *InitDReloader) initDir() string {
	if r.InitDir != "" {
		return r.InitDir
	}

	return "/etc/init.d"
}

// Reload implements ServiceReloader.
func (r *InitDReloader) Reload(ctx context.Context, service string) error {
	return r.runAction(ctx, service, "reload")
}

// Restart implements ServiceReloader.
func (r *InitDReloader) Restart(ctx context.Context, service string) error {
	return r.runAction(ctx, service, "restart")
}

// runAction validates the service name and invokes
// `/etc/init.d/<service> <action>`. stderr is captured (rather than
// passed through to the daemon's stderr) so noise like
// `/bin/sh: nohup: not found` from inside the init script becomes
// part of the returned error instead of leaking to the operator's
// console — the wizard's reload phase tolerates failures and reports
// them via structured logs.
func (r *InitDReloader) runAction(ctx context.Context, service, action string) error {
	if !validServiceName.MatchString(service) {
		return fmt.Errorf("%w: %q", ErrInvalidServiceName, service)
	}

	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, filepath.Join(r.initDir(), service), action)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}

		return fmt.Errorf("%w: %s", err, msg)
	}

	return nil
}
