package batmanadv

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openmanet/go-alfred"
	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	"github.com/rs/zerolog"
)

// ErrMeshNeighborsSnapshotNotReady is returned by the snapshotter's
// accessors before the first refresh has completed. Handlers treat it
// identically to a miss — no gossip data for any primary — and fall
// back to heuristic classification.
var ErrMeshNeighborsSnapshotNotReady = errors.New("mesh-neighbors snapshot not ready")

// DefaultMeshNeighborsSnapshotInterval mirrors BatctlSnapshotter so the
// two caches converge on the same roll-over rhythm.
const DefaultMeshNeighborsSnapshotInterval = 5 * time.Second

// MeshNeighborsRecord wraps a decoded gossip payload with the
// snapshotter's bookkeeping. Presence in the cache implies the alfred
// daemon returned the record on the most recent refresh — its own TTL
// purges records whose publisher has gone quiet, so the handler treats
// Lookup misses as the sole "publisher absent" signal.
type MeshNeighborsRecord struct {
	Received  time.Time
	Payload   *netv1.MeshNeighbors
	SourceMac string
}

// MeshNeighborsProvider is the read interface mesh_topology handlers
// consume. A nil provider means "no gossip" — handlers must fall back
// to heuristic classification per-primary.
type MeshNeighborsProvider interface {
	Lookup(primaryMac string) (*MeshNeighborsRecord, bool)
	All() map[string]*MeshNeighborsRecord
}

// MeshNeighborsSnapshotter polls alfred for DATA_TYPE_MESH_NEIGHBORS
// records every Interval and caches them keyed by publisher primary MAC.
// Lifecycle mirrors BatctlSnapshotter: synchronous warm-up on Start,
// ticker-driven refresh until ctx cancellation.
type MeshNeighborsSnapshotter struct {
	Log      zerolog.Logger
	Client   alfred.ReadClient
	Now      func() time.Time
	byMac    map[string]*MeshNeighborsRecord
	Interval time.Duration
	mu       sync.RWMutex
	DataType uint8
	ready    bool
}

// Start warms the cache synchronously (so the first Lookup after Start
// returns immediately) and spawns the refresh loop.
func (s *MeshNeighborsSnapshotter) Start(ctx context.Context) {
	if s.Interval <= 0 {
		s.Interval = DefaultMeshNeighborsSnapshotInterval
	}

	if s.DataType == 0 {
		s.DataType = uint8(netv1.DataType_DATA_TYPE_MESH_NEIGHBORS)
	}

	s.refresh()

	go s.loop(ctx)
}

func (s *MeshNeighborsSnapshotter) loop(ctx context.Context) {
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

func (s *MeshNeighborsSnapshotter) refresh() {
	if s.Client == nil {
		s.mu.Lock()
		s.byMac = nil
		s.ready = true
		s.mu.Unlock()

		return
	}

	records, err := s.Client.Request(s.DataType)
	if err != nil {
		// Soft failure: keep serving the last-known map so a transient
		// alfred hiccup doesn't empty the UI.
		s.Log.Warn().Err(err).Msg("mesh-neighbors: alfred request failed; keeping cached data")

		return
	}

	now := s.now()
	next := make(map[string]*MeshNeighborsRecord, len(records))

	for i := range records {
		record := records[i]

		payload := &netv1.MeshNeighbors{}
		if err := payload.UnmarshalVT(record.Data); err != nil {
			s.Log.Warn().Err(err).Int("record", i).Msg("mesh-neighbors: unmarshal failed; skipping record")

			continue
		}

		// Drop pre-v1 / malformed payloads that lack a timestamp. We no
		// longer use CollectedAt for staleness (clock skew across the
		// mesh makes that unreliable), but an empty timestamp is still a
		// signal that the record didn't round-trip cleanly.
		if !payload.GetCollectedAt().IsValid() || payload.GetCollectedAt().AsTime().IsZero() {
			s.Log.Warn().Int("record", i).Msg("mesh-neighbors: record has no collected_at; skipping")

			continue
		}

		src := macString(record.Source)

		next[src] = &MeshNeighborsRecord{
			Payload:   payload,
			SourceMac: src,
			Received:  now,
		}
	}

	s.mu.Lock()
	s.byMac = next
	s.ready = true
	s.mu.Unlock()
}

func (s *MeshNeighborsSnapshotter) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// Lookup returns the cached record for a publisher MAC. Both SourceMac
// and payload.primary_mac keys are accepted so callers can use
// whichever they already have.
func (s *MeshNeighborsSnapshotter) Lookup(primaryMac string) (*MeshNeighborsRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready || len(s.byMac) == 0 {
		return nil, false
	}

	lookup := strings.ToLower(primaryMac)
	if rec, ok := s.byMac[lookup]; ok {
		return rec, true
	}

	// Fall back to payload.primary_mac if the caller didn't have the
	// alfred envelope MAC. Rare, but lets the handler stitch gossip and
	// vis data when their MAC normalization paths disagree.
	for _, rec := range s.byMac {
		if rec.Payload != nil && strings.EqualFold(rec.Payload.GetPrimaryMac(), primaryMac) {
			return rec, true
		}
	}

	return nil, false
}

// All returns a shallow copy of the current record map. Callers must
// not mutate the returned records (the snapshotter owns them).
func (s *MeshNeighborsSnapshotter) All() map[string]*MeshNeighborsRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready || len(s.byMac) == 0 {
		return map[string]*MeshNeighborsRecord{}
	}

	out := make(map[string]*MeshNeighborsRecord, len(s.byMac))
	for k, v := range s.byMac {
		out[k] = v
	}

	return out
}

// Coverage reports how many of the supplied primaries have a gossip
// record present in the cache. Used by the handler to populate
// MeshTopology.gossip_coverage. Presence alone is sufficient: alfred
// expires records whose publisher has gone quiet, so a cache hit
// implies the publisher was heard recently enough to matter.
func (s *MeshNeighborsSnapshotter) Coverage(primaries []string) (published, total int) {
	total = len(primaries)
	if total == 0 {
		return 0, 0
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready {
		return 0, total
	}

	for _, p := range primaries {
		if _, ok := s.byMac[strings.ToLower(p)]; ok {
			published++
		}
	}

	return published, total
}

// macString formats an alfred envelope MAC into the lowercased colon
// notation the rest of the codebase uses.
func macString(mac []byte) string {
	if len(mac) != 6 {
		return ""
	}

	return strings.ToLower(fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5]))
}
