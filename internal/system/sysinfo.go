package system

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// MemoryInfo holds memory statistics in bytes.
type MemoryInfo struct {
	TotalBytes     int64
	AvailableBytes int64
}

// UsedBytes returns total − available.
func (m MemoryInfo) UsedBytes() int64 {
	return m.TotalBytes - m.AvailableBytes
}

// OverlayUsage holds overlay filesystem statistics in bytes.
type OverlayUsage struct {
	TotalBytes int64
	UsedBytes  int64
}

// SysInfoProvider abstracts system information collection for testability.
type SysInfoProvider interface {
	GetHostname() (string, error)
	GetKernelVersion() (string, error)
	GetArchitecture() (string, error)
	GetUptime() (time.Duration, error)
	GetMemoryInfo() (*MemoryInfo, error)
	GetCPULoadPercent() (float32, error)
	GetOverlayUsage() (*OverlayUsage, error)
}

// LinuxSysInfo is the production implementation that reads from procfs / sysfs.
type LinuxSysInfo struct {
	// ProcDir is the path prefix for proc files (default "/proc").
	ProcDir string
	// OverlayPath is the mountpoint to stat (default "/overlay").
	OverlayPath string
}

func (l *LinuxSysInfo) procDir() string {
	if l.ProcDir != "" {
		return l.ProcDir
	}

	return "/proc"
}

func (l *LinuxSysInfo) overlayPath() string {
	if l.OverlayPath != "" {
		return l.OverlayPath
	}

	return "/overlay"
}

// GetHostname returns the system hostname.
func (l *LinuxSysInfo) GetHostname() (string, error) {
	name, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}

	return name, nil
}

// GetKernelVersion returns the kernel release string via uname(2).
func (l *LinuxSysInfo) GetKernelVersion() (string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return "", fmt.Errorf("uname: %w", err)
	}

	return unix.ByteSliceToString(uts.Release[:]), nil
}

// GetArchitecture returns the machine hardware name via uname(2).
func (l *LinuxSysInfo) GetArchitecture() (string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return "", fmt.Errorf("uname: %w", err)
	}

	return unix.ByteSliceToString(uts.Machine[:]), nil
}

// GetUptime parses /proc/uptime and returns the uptime duration.
func (l *LinuxSysInfo) GetUptime() (time.Duration, error) {
	data, err := os.ReadFile(l.procDir() + "/uptime")
	if err != nil {
		return 0, fmt.Errorf("read uptime: %w", err)
	}

	return ParseUptime(string(data))
}

// ParseUptime extracts the system uptime from /proc/uptime content.
// The file contains "uptime_seconds idle_seconds\n".
func ParseUptime(content string) (time.Duration, error) {
	fields := strings.Fields(content)
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected uptime format")
	}

	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime seconds: %w", err)
	}

	return time.Duration(secs * float64(time.Second)), nil
}

// GetMemoryInfo parses /proc/meminfo for MemTotal and MemAvailable.
func (l *LinuxSysInfo) GetMemoryInfo() (*MemoryInfo, error) {
	f, err := os.Open(l.procDir() + "/meminfo")
	if err != nil {
		return nil, fmt.Errorf("open meminfo: %w", err)
	}
	defer f.Close()

	return ParseMemInfo(f)
}

// ParseMemInfo reads MemTotal and MemAvailable from /proc/meminfo content.
func ParseMemInfo(r io.Reader) (*MemoryInfo, error) {
	info := &MemoryInfo{}
	found := 0

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			v, err := parseMemInfoLine(line)
			if err != nil {
				return nil, err
			}

			info.TotalBytes = v
			found++
		case strings.HasPrefix(line, "MemAvailable:"):
			v, err := parseMemInfoLine(line)
			if err != nil {
				return nil, err
			}

			info.AvailableBytes = v
			found++
		}

		if found == 2 {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan meminfo: %w", err)
	}

	if found < 2 {
		return nil, fmt.Errorf("meminfo: missing MemTotal or MemAvailable")
	}

	return info, nil
}

// parseMemInfoLine parses a line like "MemTotal:       248168 kB" → bytes.
func parseMemInfoLine(line string) (int64, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0, fmt.Errorf("unexpected meminfo line: %s", line)
	}

	kB, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse meminfo value: %w", err)
	}

	return kB * 1024, nil
}

// GetCPULoadPercent reads /proc/loadavg and returns the 1-minute load average
// normalised by the number of CPUs as a percentage.
func (l *LinuxSysInfo) GetCPULoadPercent() (float32, error) {
	data, err := os.ReadFile(l.procDir() + "/loadavg")
	if err != nil {
		return 0, fmt.Errorf("read loadavg: %w", err)
	}

	return ParseCPULoad(string(data), runtime.NumCPU())
}

// ParseCPULoad extracts the 1-minute load average from /proc/loadavg content
// and normalises it by numCPU to produce a 0-100 percentage.
func ParseCPULoad(content string, numCPU int) (float32, error) {
	fields := strings.Fields(content)
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected loadavg format")
	}

	load, err := strconv.ParseFloat(fields[0], 32)
	if err != nil {
		return 0, fmt.Errorf("parse load average: %w", err)
	}

	if numCPU < 1 {
		numCPU = 1
	}

	pct := float32(load / float64(numCPU) * 100)
	if pct > 100 {
		pct = 100
	}

	return pct, nil
}

// GetOverlayUsage returns the total and used bytes of the overlay filesystem.
// Returns zeros without error if the overlay path does not exist.
func (l *LinuxSysInfo) GetOverlayUsage() (*OverlayUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(l.overlayPath(), &stat); err != nil {
		if os.IsNotExist(err) {
			return &OverlayUsage{}, nil
		}

		return nil, fmt.Errorf("statfs %s: %w", l.overlayPath(), err)
	}

	total := int64(stat.Blocks) * stat.Bsize
	free := int64(stat.Bfree) * stat.Bsize

	return &OverlayUsage{
		TotalBytes: total,
		UsedBytes:  total - free,
	}, nil
}
