package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUptime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:  "normal uptime",
			input: "225794.13 901176.52\n",
			want:  time.Duration(225794.13 * float64(time.Second)),
		},
		{
			name:  "short uptime",
			input: "60.00 120.00\n",
			want:  60 * time.Second,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "not a number",
			input:   "abc def\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUptime(tt.input)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.InDelta(t, float64(tt.want), float64(got), float64(time.Millisecond))
		})
	}
}

func TestParseMemInfo(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTotal int64
		wantAvail int64
		wantErr   bool
	}{
		{
			name: "typical OpenWrt meminfo",
			input: `MemTotal:         248168 kB
MemFree:           12340 kB
MemAvailable:      80000 kB
Buffers:            5000 kB
`,
			wantTotal: 248168 * 1024,
			wantAvail: 80000 * 1024,
		},
		{
			name: "missing MemAvailable",
			input: `MemTotal:         248168 kB
MemFree:           12340 kB
`,
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMemInfo(strings.NewReader(tt.input))
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, got.TotalBytes)
			assert.Equal(t, tt.wantAvail, got.AvailableBytes)
			assert.Equal(t, tt.wantTotal-tt.wantAvail, got.UsedBytes())
		})
	}
}

func TestParseCPUStat(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTotal uint64
		wantIdle  uint64
		wantErr   bool
	}{
		{
			name: "aggregate cpu line picked over per-core lines",
			// cpu  user=100 nice=20 system=50 idle=800 iowait=30 irq=0 softirq=0 steal=0
			input:     "cpu  100 20 50 800 30 0 0 0\ncpu0 50 10 25 400 15 0 0 0\n",
			wantTotal: 1000,
			wantIdle:  830,
		},
		{
			name:      "missing iowait column still parses",
			input:     "cpu  100 20 50 800\n",
			wantTotal: 970,
			wantIdle:  800,
		},
		{
			name:    "no cpu line is an error",
			input:   "intr 1\nctxt 2\n",
			wantErr: true,
		},
		{
			name:    "malformed value is an error",
			input:   "cpu  100 nope 50 800 30\n",
			wantErr: true,
		},
		{
			name:    "too few fields is an error",
			input:   "cpu  100 20\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCPUStat(tt.input)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, got.total)
			assert.Equal(t, tt.wantIdle, got.idle)
		})
	}
}

func TestComputeCPUPercent(t *testing.T) {
	tests := []struct {
		name string
		prev cpuStatSample
		cur  cpuStatSample
		want float32
	}{
		{
			name: "50% busy",
			prev: cpuStatSample{total: 1000, idle: 800},
			cur:  cpuStatSample{total: 1100, idle: 850},
			want: 50,
		},
		{
			name: "0% when fully idle",
			prev: cpuStatSample{total: 1000, idle: 800},
			cur:  cpuStatSample{total: 1100, idle: 900},
			want: 0,
		},
		{
			name: "100% when fully busy",
			prev: cpuStatSample{total: 1000, idle: 800},
			cur:  cpuStatSample{total: 1100, idle: 800},
			want: 100,
		},
		{
			name: "no elapsed time returns 0",
			prev: cpuStatSample{total: 1000, idle: 800},
			cur:  cpuStatSample{total: 1000, idle: 800},
			want: 0,
		},
		{
			name: "counter regression returns 0 (no spike)",
			prev: cpuStatSample{total: 2000, idle: 1500},
			cur:  cpuStatSample{total: 1000, idle: 800},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCPUPercent(tt.prev, tt.cur)
			assert.InDelta(t, float64(tt.want), float64(got), 0.01)
		})
	}
}

func TestLinuxSysInfo_GetUptime(t *testing.T) {
	// Create a fake procfs
	procDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "uptime"), []byte("86400.50 172801.00\n"), 0o644))

	si := &LinuxSysInfo{ProcDir: procDir}
	uptime, err := si.GetUptime()
	require.NoError(t, err)
	assert.InDelta(t, 86400.5, uptime.Seconds(), 0.01)
}

func TestLinuxSysInfo_GetMemoryInfo(t *testing.T) {
	procDir := t.TempDir()
	meminfo := `MemTotal:         248168 kB
MemFree:           12340 kB
MemAvailable:      80000 kB
Buffers:            5000 kB
Cached:            50000 kB
`
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "meminfo"), []byte(meminfo), 0o644))

	si := &LinuxSysInfo{ProcDir: procDir}
	mem, err := si.GetMemoryInfo()
	require.NoError(t, err)
	assert.Equal(t, int64(248168*1024), mem.TotalBytes)
	assert.Equal(t, int64(80000*1024), mem.AvailableBytes)
}

func TestLinuxSysInfo_GetCPULoadPercent(t *testing.T) {
	procDir := t.TempDir()
	statPath := filepath.Join(procDir, "stat")

	// First sample: total=1000, idle+iowait=830.
	require.NoError(t, os.WriteFile(statPath, []byte("cpu  100 20 50 800 30 0 0 0\n"), 0o644))

	si := &LinuxSysInfo{ProcDir: procDir}

	// First call has no prior sample — must return 0 without erroring.
	pct, err := si.GetCPULoadPercent()
	require.NoError(t, err)
	assert.Equal(t, float32(0), pct)

	// Second sample: total=1100 (delta 100), idle=880 (delta 50) → 50% busy.
	require.NoError(t, os.WriteFile(statPath, []byte("cpu  140 20 60 850 30 0 0 0\n"), 0o644))

	pct, err = si.GetCPULoadPercent()
	require.NoError(t, err)
	assert.InDelta(t, 50.0, float64(pct), 0.5)

	// Third sample with no movement returns 0 (no elapsed time), and
	// does not corrupt the cached previous sample.
	pct, err = si.GetCPULoadPercent()
	require.NoError(t, err)
	assert.Equal(t, float32(0), pct)
}

func TestLinuxSysInfo_GetCPUTempCelsius(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "temp")
		require.NoError(t, os.WriteFile(path, []byte("52345\n"), 0o644))

		si := &LinuxSysInfo{ThermalPath: path}
		c, err := si.GetCPUTempCelsius()
		require.NoError(t, err)
		assert.InDelta(t, 52.345, c, 0.001)
	})

	t.Run("missing returns sentinel", func(t *testing.T) {
		si := &LinuxSysInfo{ThermalPath: "/nonexistent/thermal/zone/temp"}
		c, err := si.GetCPUTempCelsius()
		require.NoError(t, err)
		assert.Equal(t, float32(-1), c)
	})

	t.Run("malformed returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "temp")
		require.NoError(t, os.WriteFile(path, []byte("not-a-number\n"), 0o644))

		si := &LinuxSysInfo{ThermalPath: path}
		_, err := si.GetCPUTempCelsius()
		require.Error(t, err)
	})
}

func TestLinuxSysInfo_GetOverlayUsage_MissingPath(t *testing.T) {
	si := &LinuxSysInfo{OverlayPath: "/nonexistent/overlay/path/that/does/not/exist"}
	usage, err := si.GetOverlayUsage()
	require.NoError(t, err)
	assert.Equal(t, int64(0), usage.TotalBytes)
	assert.Equal(t, int64(0), usage.UsedBytes)
}

func TestLinuxSysInfo_GetOverlayUsage_ExistingPath(t *testing.T) {
	// Use tmp dir as a stand-in for overlay
	dir := t.TempDir()
	si := &LinuxSysInfo{OverlayPath: dir}
	usage, err := si.GetOverlayUsage()
	require.NoError(t, err)
	assert.Greater(t, usage.TotalBytes, int64(0))
}

func TestLinuxSysInfo_GetHostname(t *testing.T) {
	si := &LinuxSysInfo{}
	hostname, err := si.GetHostname()
	require.NoError(t, err)
	assert.NotEmpty(t, hostname)
}

func TestLinuxSysInfo_GetKernelVersion(t *testing.T) {
	si := &LinuxSysInfo{}
	kernel, err := si.GetKernelVersion()
	require.NoError(t, err)
	assert.NotEmpty(t, kernel)
}

func TestLinuxSysInfo_GetArchitecture(t *testing.T) {
	si := &LinuxSysInfo{}
	arch, err := si.GetArchitecture()
	require.NoError(t, err)
	assert.NotEmpty(t, arch)
}

func TestMemoryInfo_UsedBytes(t *testing.T) {
	m := MemoryInfo{TotalBytes: 1000, AvailableBytes: 400}
	assert.Equal(t, int64(600), m.UsedBytes())
}
