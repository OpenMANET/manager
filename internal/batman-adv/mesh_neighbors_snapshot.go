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
	// Lookup resolves a record by any of three identities: the alfred
	// envelope MAC (record.Source), the payload's self-reported
	// primary_mac, or any MAC the publisher listed in
	// payload.interface_macs. Returns the record on a hit. Required
	// for BLOS multi-mesh deployments, where a single physical node is
	// observed under different MACs by different consumers.
	Lookup(primaryMac string) (*MeshNeighborsRecord, bool)
	// LookupByHostname matches on the payload's stripped hostname.
	// Defense-in-depth fallback for older publishers that don't
	// populate interface_macs and whose envelope/primary MACs disagree
	// with the consumer's vis primary.
	LookupByHostname(hostname string) (*MeshNeighborsRecord, bool)
	All() map[string]*MeshNeighborsRecord
}

// MeshNeighborsSnapshotter polls alfred for DATA_TYPE_MESH_NEIGHBORS
// records every Interval and caches them in two parallel indices:
// byMac is keyed by alfred envelope MAC (one record per publisher);
// byInterfaceMac is keyed by payload primary_mac and every entry of
// payload.interface_macs and points back at the same records. The
// dual index lets Lookup hit in O(1) regardless of which identity the
// caller has on hand. Lifecycle mirrors BatctlSnapshotter: synchronous
// warm-up on Start, ticker-driven refresh until ctx cancellation.
type MeshNeighborsSnapshotter struct {
	Log            zerolog.Logger
	Client         alfred.ReadClient
	Now            func() time.Time
	byMac          map[string]*MeshNeighborsRecord
	byInterfaceMac map[string]*MeshNeighborsRecord
	Interval       time.Duration
	mu             sync.RWMutex
	DataType       uint8
	ready          bool
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
		s.byInterfaceMac = nil
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
	nextByIface := make(map[string]*MeshNeighborsRecord, len(records)*2)

	var dropped int

	for i := range records {
		record := records[i]

		payload := &netv1.MeshNeighbors{}
		if err := payload.UnmarshalVT(record.Data); err != nil {
			s.Log.Warn().
				Err(err).
				Int("record", i).
				Str("source", macString(record.Source)).
				Int("data_bytes", len(record.Data)).
				Msg("mesh-neighbors: unmarshal failed; skipping record")

			dropped++

			continue
		}

		src := macString(record.Source)

		// Per-record trace. Debug level so production logs stay quiet;
		// operators flip the daemon log level to diagnose why a peer's
		// gossip isn't landing in the topology handler (envelope MAC vs
		// payload.primary_mac vs vis primary — any of the three can
		// disagree across mesh boundaries; the interface_macs index
		// stitches them together).
		s.Log.Debug().
			Int("record", i).
			Str("source", src).
			Str("primary_mac", strings.ToLower(payload.GetPrimaryMac())).
			Str("hostname", payload.GetHostname()).
			Int("neighbor_count", len(payload.GetNeighbors())).
			Int("interface_mac_count", len(payload.GetInterfaceMacs())).
			Int64("collected_unix", payload.GetCollectedAt().GetSeconds()).
			Msg("mesh-neighbors: cached record")

		rec := &MeshNeighborsRecord{
			Payload:   payload,
			SourceMac: src,
			Received:  now,
		}
		next[src] = rec

		// Index every interface MAC the publisher advertised, plus the
		// payload's primary_mac (in case the publisher ships it without
		// also listing it in interface_macs). Last-write wins on
		// collision — any conflict means two publishers are claiming
		// the same MAC, which is itself a misconfiguration we surface
		// at warn level.
		s.indexRecordByInterfaceMacs(nextByIface, rec)
	}

	s.mu.Lock()
	s.byMac = next
	s.byInterfaceMac = nextByIface
	s.ready = true
	s.mu.Unlock()

	// Per-refresh summary. Debug level so production logs stay quiet;
	// flip the daemon log level to diagnose whether alfred gossip is
	// reaching this node (received>0) and whether records are being
	// dropped before caching (dropped>0).
	s.Log.Debug().
		Int("received", len(records)).
		Int("cached", len(next)).
		Int("dropped", dropped).
		Msg("mesh-neighbors: refreshed")
}

func (s *MeshNeighborsSnapshotter) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// indexRecordByInterfaceMacs inserts rec into idx under every MAC the
// publisher advertised — payload.primary_mac plus each entry of
// payload.interface_macs. Empty values are skipped. Collisions log a
// warn line and overwrite (last write wins) so a misconfigured fleet
// surfaces in the daemon logs.
func (s *MeshNeighborsSnapshotter) indexRecordByInterfaceMacs(idx map[string]*MeshNeighborsRecord, rec *MeshNeighborsRecord) {
	if rec == nil || rec.Payload == nil {
		return
	}

	add := func(raw string) {
		mac := strings.ToLower(raw)
		if mac == "" {
			return
		}

		if existing, ok := idx[mac]; ok && existing != rec && existing.SourceMac != rec.SourceMac {
			s.Log.Warn().
				Str("mac", mac).
				Str("existing_source", existing.SourceMac).
				Str("new_source", rec.SourceMac).
				Msg("mesh-neighbors: two publishers claim the same interface MAC; using newest")
		}

		idx[mac] = rec
	}

	add(rec.Payload.GetPrimaryMac())

	for _, m := range rec.Payload.GetInterfaceMacs() {
		add(m)
	}
}

// Lookup returns the cached record for a publisher MAC. Accepts any of
// three identities: the alfred envelope MAC (SourceMac), the payload's
// primary_mac, or any entry from payload.interface_macs. All three
// resolve in O(1) via the dual index.
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

	if rec, ok := s.byInterfaceMac[lookup]; ok {
		return rec, true
	}

	return nil, false
}

// LookupByHostname returns the first cached record whose payload
// hostname matches the argument case-insensitively. Used as a fallback
// when MAC-based Lookup fails because the publisher's identity space
// (alfred envelope MAC + payload.primary_mac) lives on a different
// batman-adv instance than the vis-reported primary. Linear scan is
// intentional — fleet sizes top out around 100 peers in practice, and
// the per-poll amortization is negligible compared to the alfred
// round-trip.
func (s *MeshNeighborsSnapshotter) LookupByHostname(hostname string) (*MeshNeighborsRecord, bool) {
	if hostname == "" {
		return nil, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.ready || len(s.byMac) == 0 {
		return nil, false
	}

	for _, rec := range s.byMac {
		if rec.Payload == nil {
			continue
		}

		if strings.EqualFold(rec.Payload.GetHostname(), hostname) {
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
// implies the publisher was heard recently enough to matter. Checks
// both indices so a primary that only matches via interface_macs
// still counts as covered.
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
		key := strings.ToLower(p)
		if _, ok := s.byMac[key]; ok {
			published++

			continue
		}

		if _, ok := s.byInterfaceMac[key]; ok {
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
