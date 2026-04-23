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

// GatewayProvider abstracts retrieval of the current mesh-gateway set for
// testability. The production implementation wraps batmanadv.GetMeshGateways.
type GatewayProvider interface {
	// ListGateways returns the set of active mesh gateways, keyed by
	// originator MAC. An empty map with a nil error indicates there are
	// currently no gateways in the mesh (distinct from "batctl failed",
	// which returns a non-nil error).
	ListGateways() (map[string]struct{}, error)
}

// BatctlGatewayProvider is the production implementation of GatewayProvider
// that defers to batmanadv.GetMeshGateways.
type BatctlGatewayProvider struct{}

// ListGateways executes batctl gwj and returns the set of gateway originator
// MACs. Returns an empty map (and nil error) when no gateways are present.
func (BatctlGatewayProvider) ListGateways() (map[string]struct{}, error) {
	gws, err := batmanadv.GetMeshGateways("")
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

// deltaSample is one entry in the rolling ring maintained by DeltaTracker.
type deltaSample struct {
	// at is the wall-clock time this sample was taken.
	at time.Time
	// edges is the set of (router_mac|neighbor_mac) pairs observed at
	// sample time. Stored as a map for O(1) set-difference with the
	// previous sample.
	edges map[string]struct{}
	// gateways is the set of gateway originator MACs at sample time.
	gateways map[string]struct{}
	// routesAdded / routesLost are the per-sample deltas vs. the
	// previous sample, pre-computed so reads over a window are cheap.
	routesAdded uint32
	routesLost  uint32
	// gatewayChanges is the per-sample count of gateway add+remove
	// events vs. the previous sample.
	gatewayChanges uint32
	// stable is true when the per-sample deltas are all zero.
	stable bool
}

// DeltaTracker samples the mesh topology on a fixed cadence and maintains a
// rolling ring of snapshots so callers can compute churn metrics over a
// look-back window. The tracker owns a single goroutine whose lifetime is
// bounded by the context passed to Start.
type DeltaTracker struct {
	Log            zerolog.Logger
	OrigProvider   batmanadv.OriginatorTopologyProvider
	Gateways       GatewayProvider
	cancel         context.CancelFunc
	Now            func() time.Time
	ring           []deltaSample
	wg             sync.WaitGroup
	SampleInterval time.Duration
	head           int
	count          int
	DefaultWindow  time.Duration
	MaxSamples     int
	mu             sync.RWMutex
	started        bool
}

// defaultDeltaWindow is the look-back duration used when
// GetMeshTopologyDelta is called with no window set (zero duration).
const defaultDeltaWindow = 60 * time.Second

// NewDeltaTracker constructs a tracker with sensible defaults. Zero values
// in the exported fields are filled in after construction; callers can
// override MaxSamples etc. before calling Start.
func NewDeltaTracker(
	log zerolog.Logger,
	orig batmanadv.OriginatorTopologyProvider,
	gws GatewayProvider,
	sampleInterval time.Duration,
	maxSamples int,
) *DeltaTracker {
	if sampleInterval <= 0 {
		sampleInterval = 5 * time.Second
	}

	if maxSamples <= 1 {
		maxSamples = 120
	}

	return &DeltaTracker{
		Log:            log,
		OrigProvider:   orig,
		Gateways:       gws,
		SampleInterval: sampleInterval,
		MaxSamples:     maxSamples,
		DefaultWindow:  defaultDeltaWindow,
		ring:           make([]deltaSample, maxSamples),
	}
}

// Start launches the sampling goroutine. Safe to call multiple times; the
// second and subsequent calls are no-ops. The goroutine exits when ctx is
// canceled; callers must call Stop or cancel ctx for clean shutdown.
func (t *DeltaTracker) Start(ctx context.Context) {
	t.mu.Lock()
	if t.started {
		t.mu.Unlock()

		return
	}

	t.started = true
	workerCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.mu.Unlock()

	t.wg.Add(1)

	go t.run(workerCtx)
}

// Stop halts the sampling goroutine and waits for it to exit. Idempotent.
func (t *DeltaTracker) Stop() {
	t.mu.Lock()
	if !t.started {
		t.mu.Unlock()

		return
	}

	t.started = false
	cancel := t.cancel
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	t.wg.Wait()
}

// run is the sampling loop. It takes an immediate sample on start so callers
// that query during cold-start get a non-empty response quickly.
func (t *DeltaTracker) run(ctx context.Context) {
	defer t.wg.Done()

	t.sampleOnce()

	ticker := time.NewTicker(t.SampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.sampleOnce()
		}
	}
}

// sampleOnce executes a single sample of the originator table + batctl gwj
// and appends a deltaSample to the ring. Failures are logged and do not kill
// the loop — a transient batctl failure should not lose the window.
func (t *DeltaTracker) sampleOnce() {
	now := time.Now()
	if t.Now != nil {
		now = t.Now()
	}

	edges := t.collectEdges()
	gateways := t.collectGateways()

	sample := deltaSample{at: now, edges: edges, gateways: gateways}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.count > 0 {
		prev := t.ring[(t.head-1+t.MaxSamples)%t.MaxSamples]
		sample.routesAdded, sample.routesLost = setDiff(edges, prev.edges)
		added, lost := setDiff(gateways, prev.gateways)
		sample.gatewayChanges = added + lost
	}

	sample.stable = sample.routesAdded == 0 && sample.routesLost == 0 && sample.gatewayChanges == 0

	t.ring[t.head] = sample
	t.head = (t.head + 1) % t.MaxSamples

	if t.count < t.MaxSamples {
		t.count++
	}
}

// collectEdges fetches the current originator snapshot and flattens it into
// the "origMac|nextHopMac|hardIfname" route-edge set used by the delta ring.
// An empty set is returned on provider failure — a complete outage is itself
// a data point (prior-sample edges become "lost" on the next diff).
func (t *DeltaTracker) collectEdges() map[string]struct{} {
	if t.OrigProvider == nil {
		return map[string]struct{}{}
	}

	snap, err := t.OrigProvider.GetOriginatorTopology()
	if err != nil {
		if !errors.Is(err, batmanadv.ErrOriginatorsUnavailable) {
			t.Log.Warn().Err(err).Msg("Mesh delta tracker: originator provider failure")
		}

		return map[string]struct{}{}
	}

	return edgesFromOriginators(snap)
}

// collectGateways fetches the current mesh-gateway set, returning an empty
// set on failure (or when no provider is wired).
func (t *DeltaTracker) collectGateways() map[string]struct{} {
	if t.Gateways == nil {
		return map[string]struct{}{}
	}

	gws, err := t.Gateways.ListGateways()
	if err != nil {
		t.Log.Warn().Err(err).Msg("Mesh delta tracker: gateway list failure")

		return map[string]struct{}{}
	}

	return gws
}

// setDiff returns the counts of elements in curr that were not in prev
// (added) and elements in prev that are missing from curr (lost).
func setDiff(curr, prev map[string]struct{}) (added, lost uint32) {
	for k := range curr {
		if _, had := prev[k]; !had {
			added++
		}
	}

	for k := range prev {
		if _, still := curr[k]; !still {
			lost++
		}
	}

	return added, lost
}

// edgesFromOriginators flattens an OriginatorTopology into a set of
// "origMac|nextHopMac|hardIfname" keys. Including the hard interface means
// that a route that fails over from wlan0 to phy2-mesh0 (same next hop, new
// interface) still registers as a route change — which is what operators
// care about. Lower-cased on insertion so MAC case never leaks into
// set-diff results.
func edgesFromOriginators(snap *batmanadv.OriginatorTopology) map[string]struct{} {
	if snap == nil {
		return map[string]struct{}{}
	}

	out := make(map[string]struct{}, len(snap.Originators))

	for _, o := range snap.Originators {
		key := strings.ToLower(o.OrigMAC) + "|" +
			strings.ToLower(o.NextHopMAC) + "|" +
			o.HardIfname
		out[key] = struct{}{}
	}

	return out
}

// DeltaResult summarizes the churn metrics over a window.
type DeltaResult struct {
	RoutesAdded    uint32
	RoutesLost     uint32
	GatewayChanges uint32
	Reconverge     time.Duration
	ActualWindow   time.Duration
}

// Window returns the aggregated churn metrics over the last `window`
// duration, or DefaultWindow when window <= 0. Returns an empty result
// with ActualWindow=0 when the ring contains fewer than two samples.
func (t *DeltaTracker) Window(window time.Duration) DeltaResult {
	if window <= 0 {
		window = t.DefaultWindow
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.count < 2 {
		return DeltaResult{}
	}

	newestIdx := (t.head - 1 + t.MaxSamples) % t.MaxSamples
	newest := t.ring[newestIdx]
	cutoff := newest.at.Add(-window)

	var (
		result           DeltaResult
		firstUnstableAt  time.Time
		lastUnstableAt   time.Time
		oldestInWindowAt = newest.at
	)

	// Walk newest -> oldest, accumulating per-sample deltas until we leave
	// the window or exhaust the ring. We stop at count-1 because the oldest
	// sample has no prior to diff against inside the window.
	for offset := 0; offset < t.count; offset++ {
		idx := (newestIdx - offset + t.MaxSamples) % t.MaxSamples
		s := t.ring[idx]

		if s.at.Before(cutoff) {
			break
		}

		oldestInWindowAt = s.at

		if offset == 0 && t.count == 1 {
			continue
		}

		// The per-sample deltas are relative to the sample *before* s.
		// s's own at carries the transition moment.
		result.RoutesAdded += s.routesAdded
		result.RoutesLost += s.routesLost
		result.GatewayChanges += s.gatewayChanges

		if !s.stable {
			if lastUnstableAt.IsZero() {
				lastUnstableAt = s.at
			}

			firstUnstableAt = s.at
		}
	}

	result.ActualWindow = newest.at.Sub(oldestInWindowAt)

	// Reconverge: distance from the first unstable sample in the window to
	// the most recent unstable sample (inclusive). Zero means the mesh was
	// stable for the entire window.
	if !firstUnstableAt.IsZero() {
		result.Reconverge = lastUnstableAt.Sub(firstUnstableAt)
		if result.Reconverge < 0 {
			result.Reconverge = 0
		}
	}

	return result
}

// Ready reports whether the tracker has at least two samples, making Window
// results meaningful.
func (t *DeltaTracker) Ready() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.count >= 2
}
