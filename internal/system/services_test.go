package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitDServiceChecker_CheckServices_Running(t *testing.T) {
	runDir := t.TempDir()
	procDir := t.TempDir()

	// Simulate a running service with PID 42
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "dnsmasq.pid"), []byte("42\n"), 0o644))

	// Create /proc/42/status to simulate an existing process
	pidDir := filepath.Join(procDir, "42")
	require.NoError(t, os.MkdirAll(pidDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pidDir, "status"), []byte("Name:\tdnsmasq\n"), 0o644))

	checker := &InitDServiceChecker{RunDir: runDir, ProcDir: procDir}
	results, err := checker.CheckServices(context.Background(), []string{"dnsmasq"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "dnsmasq", results[0].Name)
	assert.Equal(t, ServiceStateRunning, results[0].State)
	assert.Equal(t, 42, results[0].PID)
}

func TestInitDServiceChecker_CheckServices_Stopped(t *testing.T) {
	runDir := t.TempDir()
	procDir := t.TempDir()

	checker := &InitDServiceChecker{RunDir: runDir, ProcDir: procDir}
	results, err := checker.CheckServices(context.Background(), []string{"batadv-vis"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "batadv-vis", results[0].Name)
	assert.Equal(t, ServiceStateStopped, results[0].State)
	assert.Equal(t, 0, results[0].PID)
}

func TestInitDServiceChecker_CheckServices_StaleProcess(t *testing.T) {
	runDir := t.TempDir()
	procDir := t.TempDir()

	// PID file exists but process does not (no /proc/<pid>/status)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "gpsd.pid"), []byte("9999\n"), 0o644))

	checker := &InitDServiceChecker{RunDir: runDir, ProcDir: procDir}
	results, err := checker.CheckServices(context.Background(), []string{"gpsd"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, ServiceStateStopped, results[0].State)
	assert.Equal(t, 0, results[0].PID)
}

func TestInitDServiceChecker_CheckServices_Multiple(t *testing.T) {
	runDir := t.TempDir()
	procDir := t.TempDir()

	// dnsmasq is running (PID 100)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "dnsmasq.pid"), []byte("100\n"), 0o644))
	pidDir := filepath.Join(procDir, "100")
	require.NoError(t, os.MkdirAll(pidDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pidDir, "status"), []byte("Name:\tdnsmasq\n"), 0o644))

	// hostapd is stopped (no PID file)

	checker := &InitDServiceChecker{RunDir: runDir, ProcDir: procDir}
	results, err := checker.CheckServices(context.Background(), []string{"dnsmasq", "hostapd"})
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, ServiceStateRunning, results[0].State)
	assert.Equal(t, 100, results[0].PID)

	assert.Equal(t, ServiceStateStopped, results[1].State)
	assert.Equal(t, 0, results[1].PID)
}

func TestInitDServiceChecker_CheckServices_InvalidPID(t *testing.T) {
	runDir := t.TempDir()
	procDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(runDir, "broken.pid"), []byte("not-a-number\n"), 0o644))

	checker := &InitDServiceChecker{RunDir: runDir, ProcDir: procDir}
	results, err := checker.CheckServices(context.Background(), []string{"broken"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, ServiceStateStopped, results[0].State)
}

func TestDefaultMonitoredServices(t *testing.T) {
	assert.Contains(t, DefaultMonitoredServices(), "openmanetd")
	assert.Contains(t, DefaultMonitoredServices(), "dnsmasq")
	assert.Contains(t, DefaultMonitoredServices(), "batadv-vis")
	assert.Len(t, DefaultMonitoredServices(), 6)
}
