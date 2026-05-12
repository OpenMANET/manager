package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/rs/zerolog"
)

// ErrBatctlSnapshotNotReady is returned by BatctlSnapshotter accessors
// before the first background refresh has completed. Handlers should
// treat it the same as any other upstream failure. In production the
// snapshotter is warmed synchronously in Start, so this error is only
// visible in tests that skip the warm-up step.
var ErrBatctlSnapshotNotReady = errors.New("batctl snapshot not ready")

// DefaultBatctlSnapshotInterval is the cadence at which the snapshotter
// re-shells out to batctl. 5s matches the existing DeltaTracker default
// and the staleness window called out in the load-reduction plan.
const DefaultBatctlSnapshotInterval = 5 * time.Second

// BatctlSnapshotter owns a single background goroutine that periodically
// refreshes the outputs of `batctl oj` / `nj` / `mj` / `gwj` and the
// `/tmp/bat-hosts` file. Callers read the cached data via accessors
// whose signatures match the override-function fields the existing
// handlers already expose, so wiring the snapshotter in is a drop-in
// replacement for the direct batmanadv function calls.
//
// Every expensive exec is amortized across all concurrent RPC handlers
// — previously each of DashboardService, StatusService, MeshService,
// MeshTopologyService, and WifiConfigService would fork `batctl`
// independently per request.
type BatctlSnapshotter struct {
	Log              zerolog.Logger
	FetchOriginators func() ([]batmanadv.Originator, error)
	FetchNeighbors   func() (*batmanadv.Neighbors, error)
	FetchMeshConfig  func(string) (*batmanadv.MeshConfig, error)
	FetchGateways    func(string) (*batmanadv.Gateways, error)
	FetchBatHosts    func(string) (*batmanadv.BatHosts, error)
	FetchVis         func(context.Context) (*batmanadv.VisDoc, error)
	Iface            string
	BatHostsPath     string
	cached           batctlCache
	Interval         time.Duration
	mu               sync.RWMutex
	ready            bool
}

// batctlCache holds the five pieces of state the snapshotter serves.
// Each field is paired with its last error so handlers keep the same
// error semantics they had when calling batmanadv directly.
type batctlCache struct {
	at             time.Time
	originatorsErr error
	neighborsErr   error
	meshConfigErr  error
	gatewaysErr    error
	batHostsErr    error
	visErr         error
	neighbors      *batmanadv.Neighbors
	meshConfig     *batmanadv.MeshConfig
	gateways       *batmanadv.Gateways
	batHosts       *batmanadv.BatHosts
	vis            *batmanadv.VisDoc
	originators    []batmanadv.Originator
}

// NewBatctlSnapshotter constructs a snapshotter for the given batman-adv
// interface. Call Start to warm the cache and spawn the refresh loop.
func NewBatctlSnapshotter(log zerolog.Logger, iface string, interval time.Duration) *BatctlSnapshotter {
	if interval <= 0 {
		interval = DefaultBatctlSnapshotInterval
	}

	return &BatctlSnapshotter{
		Log:          log,
		Interval:     interval,
		Iface:        iface,
		BatHostsPath: batmanadv.BatHostsFilePath,
	}
}

// Start performs one synchronous refresh so the cache is warm by the time
// it returns, then spawns a goroutine that refreshes every Interval until
// ctx is canceled.
func (s *BatctlSnapshotter) Start(ctx context.Context) {
	s.refresh()

	go s.loop(ctx)
}

func (s *BatctlSnapshotter) loop(ctx context.Context) {
	t := time.NewTicker(s.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh()
		}
	}
}

func (s *BatctlSnapshotter) refresh() {
	origFn, neighFn, cfgFn, gwFn, hostsFn, visFn := s.resolveFetchers()

	var next batctlCache

	next.at = time.Now()
	next.originators, next.originatorsErr = origFn()
	next.neighbors, next.neighborsErr = neighFn()
	next.meshConfig, next.meshConfigErr = cfgFn(s.Iface)
	next.gateways, next.gatewaysErr = gwFn(s.Iface)
	next.batHosts, next.batHostsErr = hostsFn(s.hostsPath())

	// vis gets its own context bounded to the sampling interval so a
	// hung exec can never wedge the refresh loop.
	visCtx, cancel := context.WithTimeout(context.Background(), s.Interval)
	next.vis, next.visErr = visFn(visCtx)

	cancel()

	s.mu.Lock()
	s.cached = next
	s.ready = true
	s.mu.Unlock()
}

func (s *BatctlSnapshotter) hostsPath() string {
	if s.BatHostsPath != "" {
		return s.BatHostsPath
	}

	return batmanadv.BatHostsFilePath
}

func (s *BatctlSnapshotter) resolveFetchers() (
	origFn func() ([]batmanadv.Originator, error),
	neighFn func() (*batmanadv.Neighbors, error),
	cfgFn func(string) (*batmanadv.MeshConfig, error),
	gwFn func(string) (*batmanadv.Gateways, error),
	hostsFn func(string) (*batmanadv.BatHosts, error),
	visFn func(context.Context) (*batmanadv.VisDoc, error),
) {
	origFn = s.FetchOriginators
	if origFn == nil {
		p := &batmanadv.BatctlOriginatorProvider{}
		origFn = p.GetOriginators
	}

	neighFn = s.FetchNeighbors
	if neighFn == nil {
		neighFn = batmanadv.GetMeshNeighbors
	}

	cfgFn = s.FetchMeshConfig
	if cfgFn == nil {
		cfgFn = batmanadv.GetMeshConfig
	}

	gwFn = s.FetchGateways
	if gwFn == nil {
		gwFn = batmanadv.GetMeshGateways
	}

	hostsFn = s.FetchBatHosts
	if hostsFn == nil {
		hostsFn = batmanadv.ParseBatHostsFile
	}

	visFn = s.FetchVis
	if visFn == nil {
		p := batmanadv.BatadvVisProvider{}
		visFn = p.GetMeshVis
	}

	return
}

// ---------------------------------------------------------------------------
// Accessors.
//
// Signatures match the override-function fields the existing handlers
// already expose (plus batmanadv.OriginatorProvider and the DeltaTracker
// GatewayProvider interface). The iface / path arguments on per-iface
// getters are accepted for signature compatibility but ignored — the
// snapshotter is configured with a single interface at construction.
// ---------------------------------------------------------------------------

// GetOriginators implements batmanadv.OriginatorProvider.
func (s *BatctlSnapshotter) GetOriginators() ([]batmanadv.Originator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return nil, ErrBatctlSnapshotNotReady
	}

	return s.cached.originators, s.cached.originatorsErr
}

// GetMeshNeighbors returns the cached neighbor list.
func (s *BatctlSnapshotter) GetMeshNeighbors() (*batmanadv.Neighbors, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return nil, ErrBatctlSnapshotNotReady
	}

	return s.cached.neighbors, s.cached.neighborsErr
}

// GetMeshConfig returns the cached mesh config. The iface argument is
// ignored; the snapshotter is wired with a single interface at
// construction time.
func (s *BatctlSnapshotter) GetMeshConfig(_ string) (*batmanadv.MeshConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return nil, ErrBatctlSnapshotNotReady
	}

	return s.cached.meshConfig, s.cached.meshConfigErr
}

// GetMeshGateways returns the cached gateway list. The iface argument is
// ignored.
func (s *BatctlSnapshotter) GetMeshGateways(_ string) (*batmanadv.Gateways, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return nil, ErrBatctlSnapshotNotReady
	}

	return s.cached.gateways, s.cached.gatewaysErr
}

// ParseBatHosts returns the cached /tmp/bat-hosts parse result. The path
// argument is ignored; the snapshotter is wired with a fixed path at
// construction time.
func (s *BatctlSnapshotter) ParseBatHosts(_ string) (*batmanadv.BatHosts, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return nil, ErrBatctlSnapshotNotReady
	}

	return s.cached.batHosts, s.cached.batHostsErr
}

// GetMeshVis implements batmanadv.VisProvider using the cached vis doc.
// The ctx argument is accepted for interface compatibility; the actual
// exec ran at refresh time with its own bounded context.
func (s *BatctlSnapshotter) GetMeshVis(_ context.Context) (*batmanadv.VisDoc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return nil, ErrBatctlSnapshotNotReady
	}

	return s.cached.vis, s.cached.visErr
}

// ListGateways implements the DeltaTracker GatewayProvider interface
// using the cached gateway list. Returns an empty map (and nil error)
// when no gateways are present, matching BatctlGatewayProvider.
func (s *BatctlSnapshotter) ListGateways() (map[string]struct{}, error) {
	gws, err := s.GetMeshGateways("")
	if err != nil {
		return nil, err
	}

	if gws == nil {
		return map[string]struct{}{}, nil
	}

	out := make(map[string]struct{}, len(*gws))
	for _, g := range *gws {
		out[strings.ToLower(g.OrigAddress)] = struct{}{}
	}

	return out, nil
}
