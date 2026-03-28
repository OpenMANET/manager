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

func TestParseCPULoad(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		numCPU  int
		want    float32
		wantErr bool
	}{
		{
			name:   "single CPU 12% load",
			input:  "0.12 0.07 0.02 1/128 4567\n",
			numCPU: 1,
			want:   12.0,
		},
		{
			name:   "dual CPU normalised",
			input:  "1.00 0.50 0.25 2/200 1234\n",
			numCPU: 2,
			want:   50.0,
		},
		{
			name:   "clamped to 100",
			input:  "4.00 2.00 1.00 1/50 100\n",
			numCPU: 1,
			want:   100.0,
		},
		{
			name:   "zero CPUs defaults to 1",
			input:  "0.50 0.25 0.10 1/50 100\n",
			numCPU: 0,
			want:   50.0,
		},
		{
			name:    "empty input",
			input:   "",
			numCPU:  1,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCPULoad(tt.input, tt.numCPU)
			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.InDelta(t, float64(tt.want), float64(got), 0.1)
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
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "loadavg"), []byte("0.12 0.07 0.02 1/128 4567\n"), 0o644))

	si := &LinuxSysInfo{ProcDir: procDir}
	pct, err := si.GetCPULoadPercent()
	require.NoError(t, err)
	assert.Greater(t, pct, float32(0))
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
