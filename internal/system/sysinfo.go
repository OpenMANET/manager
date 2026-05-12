package system

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
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
	GetCPUTempCelsius() (float32, error)
	GetOverlayUsage() (*OverlayUsage, error)
}

// LinuxSysInfo is the production implementation that reads from procfs / sysfs.
type LinuxSysInfo struct {
	// ProcDir is the path prefix for proc files (default "/proc").
	ProcDir string
	// OverlayPath is the mountpoint to stat (default "/overlay").
	OverlayPath string
	// ThermalPath is the file to read CPU temperature from in millidegrees C
	// (default "/sys/class/thermal/thermal_zone0/temp").
	ThermalPath string

	// cpuMu guards the cached previous /proc/stat sample used by
	// GetCPULoadPercent for delta-based utilization.
	cpuMu       sync.Mutex
	cpuPrev     cpuStatSample
	cpuHavePrev bool
}

// cpuStatSample is a single read of /proc/stat's aggregate "cpu" line:
// total = sum of all jiffies, idle = idle + iowait. Two consecutive
// samples are required to derive a utilization percentage.
type cpuStatSample struct {
	total uint64
	idle  uint64
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

func (l *LinuxSysInfo) thermalPath() string {
	if l.ThermalPath != "" {
		return l.ThermalPath
	}

	return "/sys/class/thermal/thermal_zone0/temp"
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

// GetCPULoadPercent returns aggregate CPU utilization as a percentage,
// computed from the delta between two consecutive reads of the "cpu"
// line in /proc/stat. The first call after process start has no prior
// sample to compare against and returns 0; subsequent calls return the
// fraction of jiffies spent doing non-idle work since the previous
// call.
//
// One small (~1 KB) procfs read per call, no sleeps, no spawned
// processes — the same I/O cost as the previous loadavg-based
// implementation. The cached sample makes this method
// goroutine-safe and side-effecting; callers should not depend on the
// first result reflecting current load.
func (l *LinuxSysInfo) GetCPULoadPercent() (float32, error) {
	data, err := os.ReadFile(l.procDir() + "/stat")
	if err != nil {
		return 0, fmt.Errorf("read stat: %w", err)
	}

	cur, err := ParseCPUStat(string(data))
	if err != nil {
		return 0, err
	}

	l.cpuMu.Lock()
	defer l.cpuMu.Unlock()

	prev := l.cpuPrev
	hadPrev := l.cpuHavePrev
	l.cpuPrev = cur
	l.cpuHavePrev = true

	if !hadPrev {
		return 0, nil
	}

	return computeCPUPercent(prev, cur), nil
}

// ParseCPUStat extracts the aggregate "cpu" line from /proc/stat
// content and returns its total and idle jiffies. Idle includes
// iowait so the result matches the convention used by top(1) and
// vmstat(1). guest / guest_nice are not added because the kernel
// already accounts for them inside user / nice on Linux 2.6.33+.
func ParseCPUStat(content string) (cpuStatSample, error) {
	for line := range strings.SplitSeq(content, "\n") {
		if !strings.HasPrefix(line, "cpu ") && !strings.HasPrefix(line, "cpu\t") {
			continue
		}

		fields := strings.Fields(line)
		// "cpu" + at least user, nice, system, idle.
		if len(fields) < 5 {
			return cpuStatSample{}, fmt.Errorf("unexpected /proc/stat cpu line: %q", line)
		}

		var total, idle uint64

		for i, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuStatSample{}, fmt.Errorf("parse /proc/stat field %d: %w", i, err)
			}

			total += v
			// idle = field index 3, iowait = field index 4 (0-based,
			// after the "cpu" label).
			if i == 3 || i == 4 {
				idle += v
			}
		}

		return cpuStatSample{total: total, idle: idle}, nil
	}

	return cpuStatSample{}, fmt.Errorf("/proc/stat: missing cpu aggregate line")
}

// computeCPUPercent returns the busy fraction between two samples,
// clamped to [0, 100]. Returns 0 when totals haven't moved (no
// elapsed time) or when a counter reset is detected (cur < prev),
// which keeps the gauge from spiking on monotonicity violations.
func computeCPUPercent(prev, cur cpuStatSample) float32 {
	if cur.total <= prev.total || cur.idle < prev.idle {
		return 0
	}

	totalDelta := cur.total - prev.total

	idleDelta := cur.idle - prev.idle
	if idleDelta > totalDelta {
		return 0
	}

	busy := totalDelta - idleDelta

	pct := float32(busy) * 100 / float32(totalDelta)
	if pct < 0 {
		pct = 0
	}

	if pct > 100 {
		pct = 100
	}

	return pct
}

// GetCPUTempCelsius reads the CPU temperature from sysfs and returns it in
// degrees Celsius. Returns -1 (without error) on devices that do not expose
// a thermal zone, so callers can render an "unavailable" placeholder rather
// than treat the read as a failure.
func (l *LinuxSysInfo) GetCPUTempCelsius() (float32, error) {
	data, err := os.ReadFile(l.thermalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return -1, nil
		}

		return 0, fmt.Errorf("read cpu temp: %w", err)
	}

	milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cpu temp: %w", err)
	}

	return float32(milli) / 1000, nil
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

	total := int64(stat.Blocks) * int64(stat.Bsize) //nolint:unconvert
	free := int64(stat.Bfree) * int64(stat.Bsize)   //nolint:unconvert

	return &OverlayUsage{
		TotalBytes: total,
		UsedBytes:  total - free,
	}, nil
}
