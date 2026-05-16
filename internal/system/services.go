package system

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ServiceState mirrors the protobuf ServiceStatus enum values.
type ServiceState int

const (
	ServiceStateUnknown ServiceState = iota
	ServiceStateRunning
	ServiceStateStopped
)

// ServiceStatus holds runtime state of a single system service.
type ServiceStatus struct {
	Name  string
	State ServiceState
	PID   int
}

// Service names monitored or reloaded by this package. Centralized so
// tests and runtime code share the same identifiers (and so goconst is
// happy).
const (
	ServiceOpenManetWebUI = "openmanet-webui"
	ServiceDnsmasq        = "dnsmasq"
)

// defaultMonitoredServices is the set checked by default, matching the
// dashboard screenshot.
var defaultMonitoredServices = []string{ //nolint:gochecknoglobals // package-level config
	"openmanetd",
	ServiceOpenManetWebUI,
	ServiceDnsmasq,
	"hostapd",
	"gpsd",
	"batadv-vis",
}

// DefaultMonitoredServices returns the default list of services to monitor.
func DefaultMonitoredServices() []string {
	return defaultMonitoredServices
}

// ServiceChecker abstracts service status probing for testability.
type ServiceChecker interface {
	CheckServices(ctx context.Context, names []string) ([]ServiceStatus, error)
}

// InitDServiceChecker is the production implementation that checks PID files
// and verifies the process exists in /proc.
type InitDServiceChecker struct {
	// RunDir is the directory containing PID files (default "/var/run").
	RunDir string
	// ProcDir is the /proc mount (default "/proc").
	ProcDir string
}

func (c *InitDServiceChecker) runDir() string {
	if c.RunDir != "" {
		return c.RunDir
	}

	return "/var/run"
}

func (c *InitDServiceChecker) procDir() string {
	if c.ProcDir != "" {
		return c.ProcDir
	}

	return "/proc"
}

// CheckServices returns the status for each named service.
func (c *InitDServiceChecker) CheckServices(_ context.Context, names []string) ([]ServiceStatus, error) {
	results := make([]ServiceStatus, 0, len(names))
	for _, name := range names {
		status := ServiceStatus{Name: name, State: ServiceStateStopped}

		pid, err := c.readPIDFile(name)
		if err == nil && pid > 0 && c.processExists(pid) {
			status.State = ServiceStateRunning
			status.PID = pid
		}

		results = append(results, status)
	}

	return results, nil
}

// readPIDFile attempts to read the PID from /var/run/<name>.pid.
func (c *InitDServiceChecker) readPIDFile(name string) (int, error) {
	path := fmt.Sprintf("%s/%s.pid", c.runDir(), name)

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}

	return pid, nil
}

// processExists checks whether /proc/<pid>/status exists.
func (c *InitDServiceChecker) processExists(pid int) bool {
	path := fmt.Sprintf("%s/%d/status", c.procDir(), pid)
	_, err := os.Stat(path)

	return err == nil
}

// QuickActionExecutor abstracts quick action execution for testability.
type QuickActionExecutor interface {
	Reboot(ctx context.Context) error
	RestartNetwork(ctx context.Context) error
	RestartOpenmanetd(ctx context.Context) error
}

// InitDActionExecutor is the production implementation.
type InitDActionExecutor struct{}

// Reboot initiates a system reboot.
func (e *InitDActionExecutor) Reboot(_ context.Context) error {
	return Reboot()
}

// RestartNetwork restarts the network via init.d.
func (e *InitDActionExecutor) RestartNetwork(ctx context.Context) error {
	return exec.CommandContext(ctx, "/etc/init.d/network", "restart").Run()
}

// RestartOpenmanetd restarts the openmanetd service via init.d.
func (e *InitDActionExecutor) RestartOpenmanetd(ctx context.Context) error {
	return exec.CommandContext(ctx, "/etc/init.d/openmanetd", "restart").Run()
}
