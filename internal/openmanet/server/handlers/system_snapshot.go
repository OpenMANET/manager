package handlers

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/openmanet/openmanetd/internal/system"
	"github.com/rs/zerolog"
)

// DefaultSystemSnapshotInterval is the cadence at which procfs + service
// PID files are re-read by the SystemSnapshotter. 2s is short enough that
// the Dashboard CPU-load / memory chips still feel live while amortizing
// the file-walk cost across all concurrent callers.
const DefaultSystemSnapshotInterval = 2 * time.Second

// SystemSnapshotter owns a background goroutine that periodically refreshes
// the /proc/uptime, /proc/meminfo, /proc/loadavg, /overlay, and service PID
// files consumed by DashboardService.GetDashboardStatus.
//
// It implements both system.SysInfoProvider and system.ServiceChecker so
// it can be dropped into DashboardService in place of the live providers
// without any handler code changes. Hostname is served live (operators
// can change it via the Settings API) while the rest is served from the
// last-sampled snapshot.
//
// When the cache is cold (before the first refresh), accessors fall back
// to the inner provider so the first RPC after startup still returns
// valid data. In production Start performs a synchronous warm-up, so
// this fallback path is only exercised in tests that construct the
// snapshotter directly.
type SystemSnapshotter struct {
	Log          zerolog.Logger
	Inner        system.SysInfoProvider
	Services     system.ServiceChecker
	snap         atomic.Pointer[systemSnapshot]
	ServiceNames []string
	Interval     time.Duration
}

// systemSnapshot is one refresh cycle's worth of cached data. Each field
// is paired with its last error so callers keep the same error semantics
// they had when calling the live providers directly.
type systemSnapshot struct {
	at              time.Time
	cpuLoadErr      error
	cpuTempErr      error
	servicesErr     error
	kernelErr       error
	architectureErr error
	overlayErr      error
	uptimeErr       error
	memoryErr       error
	memory          *system.MemoryInfo
	overlay         *system.OverlayUsage
	architecture    string
	kernel          string
	services        []system.ServiceStatus
	uptime          time.Duration
	cpuLoad         float32
	cpuTemp         float32
}

// NewSystemSnapshotter constructs a snapshotter wrapping the given inner
// providers. serviceNames is the list of services whose status the
// snapshotter probes on every refresh; callers should pass the same list
// they intend to query via CheckServices. If interval is <= 0,
// DefaultSystemSnapshotInterval is used.
func NewSystemSnapshotter(
	log zerolog.Logger,
	inner system.SysInfoProvider,
	services system.ServiceChecker,
	serviceNames []string,
	interval time.Duration,
) *SystemSnapshotter {
	if interval <= 0 {
		interval = DefaultSystemSnapshotInterval
	}

	// Copy the service-name slice so an external mutation can't
	// race with the background goroutine.
	names := make([]string, len(serviceNames))
	copy(names, serviceNames)

	return &SystemSnapshotter{
		Log:          log,
		Interval:     interval,
		Inner:        inner,
		Services:     services,
		ServiceNames: names,
	}
}

// Start performs one synchronous refresh so the cache is warm by the time
// it returns, then spawns a goroutine that refreshes every Interval until
// ctx is canceled.
func (s *SystemSnapshotter) Start(ctx context.Context) {
	s.refresh(ctx)

	go s.loop(ctx)
}

func (s *SystemSnapshotter) loop(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx)
		}
	}
}

func (s *SystemSnapshotter) refresh(ctx context.Context) {
	var next systemSnapshot

	next.at = time.Now()

	if s.Inner != nil {
		next.kernel, next.kernelErr = s.Inner.GetKernelVersion()
		next.architecture, next.architectureErr = s.Inner.GetArchitecture()
		next.uptime, next.uptimeErr = s.Inner.GetUptime()
		next.memory, next.memoryErr = s.Inner.GetMemoryInfo()
		next.cpuLoad, next.cpuLoadErr = s.Inner.GetCPULoadPercent()
		next.cpuTemp, next.cpuTempErr = s.Inner.GetCPUTempCelsius()
		next.overlay, next.overlayErr = s.Inner.GetOverlayUsage()
	}

	if s.Services != nil {
		next.services, next.servicesErr = s.Services.CheckServices(ctx, s.ServiceNames)
	}

	s.snap.Store(&next)
}

// ---------------------------------------------------------------------------
// system.SysInfoProvider
// ---------------------------------------------------------------------------

// GetHostname is served live because operators change it via the Settings
// API and stale values would lie to the UI until the next refresh.
func (s *SystemSnapshotter) GetHostname() (string, error) {
	return s.Inner.GetHostname()
}

// GetKernelVersion returns the cached kernel release string.
func (s *SystemSnapshotter) GetKernelVersion() (string, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.kernel, snap.kernelErr
	}

	return s.Inner.GetKernelVersion()
}

// GetArchitecture returns the cached machine architecture string.
func (s *SystemSnapshotter) GetArchitecture() (string, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.architecture, snap.architectureErr
	}

	return s.Inner.GetArchitecture()
}

// GetUptime returns the cached system uptime. Operators see a value at
// most Interval seconds old — a trivial tradeoff against repeated
// /proc/uptime reads on every dashboard request.
func (s *SystemSnapshotter) GetUptime() (time.Duration, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.uptime, snap.uptimeErr
	}

	return s.Inner.GetUptime()
}

// GetMemoryInfo returns the cached memory statistics.
func (s *SystemSnapshotter) GetMemoryInfo() (*system.MemoryInfo, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.memory, snap.memoryErr
	}

	return s.Inner.GetMemoryInfo()
}

// GetCPULoadPercent returns the cached 1-minute-load CPU percentage.
func (s *SystemSnapshotter) GetCPULoadPercent() (float32, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.cpuLoad, snap.cpuLoadErr
	}

	return s.Inner.GetCPULoadPercent()
}

// GetCPUTempCelsius returns the cached CPU temperature in degrees Celsius.
// Negative values mean the device does not expose a thermal zone.
func (s *SystemSnapshotter) GetCPUTempCelsius() (float32, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.cpuTemp, snap.cpuTempErr
	}

	return s.Inner.GetCPUTempCelsius()
}

// GetOverlayUsage returns the cached overlay filesystem usage.
func (s *SystemSnapshotter) GetOverlayUsage() (*system.OverlayUsage, error) {
	if snap := s.snap.Load(); snap != nil {
		return snap.overlay, snap.overlayErr
	}

	return s.Inner.GetOverlayUsage()
}

// ---------------------------------------------------------------------------
// system.ServiceChecker
// ---------------------------------------------------------------------------

// CheckServices returns the cached service-status list when names matches
// the snapshotter's configured service set. For any other name list it
// falls through to the inner ServiceChecker so unusual queries continue
// to work — the snapshotter is an optimization for the common case, not
// a replacement.
func (s *SystemSnapshotter) CheckServices(ctx context.Context, names []string) ([]system.ServiceStatus, error) {
	if s.sameServiceSet(names) {
		if snap := s.snap.Load(); snap != nil {
			return snap.services, snap.servicesErr
		}
	}

	if s.Services == nil {
		return nil, nil
	}

	return s.Services.CheckServices(ctx, names)
}

// sameServiceSet returns true when names is element-wise identical to the
// configured ServiceNames list — order matters because CheckServices
// returns results in the order the caller requested.
func (s *SystemSnapshotter) sameServiceSet(names []string) bool {
	if len(names) != len(s.ServiceNames) {
		return false
	}

	for i, n := range names {
		if n != s.ServiceNames[i] {
			return false
		}
	}

	return true
}
