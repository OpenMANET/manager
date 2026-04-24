package batmanadv_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/openmanet/go-alfred"
	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeAlfredRead scripts alfred.Request responses for one datatype.
type fakeAlfredRead struct {
	mu      sync.Mutex
	records []alfred.Record
	err     error
	calls   int
}

func (f *fakeAlfredRead) Request(_ uint8) ([]alfred.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	return f.records, f.err
}

func (f *fakeAlfredRead) setRecords(records []alfred.Record) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.records = records
}

// makePayload builds a proto-encoded MeshNeighbors record for a
// publisher with the given primary MAC + collected time.
func makePayload(t *testing.T, primary string, collected time.Time) []byte {
	t.Helper()

	pb := &netv1.MeshNeighbors{
		PrimaryMac:  primary,
		Hostname:    "BCM2711-test",
		Algorithm:   15,
		CollectedAt: timestamppb.New(collected),
		Neighbors: []*netv1.MeshNeighbor{
			{Mac: "aa:bb:cc:dd:ee:ff", HardIfname: "wlan0", Blos: false, ThroughputKbps: 50000},
		},
	}

	buf, err := pb.MarshalVT()
	require.NoError(t, err)

	return buf
}

func macBytes(mac string) net.HardwareAddr {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		panic(err)
	}

	return hw
}

// TestMeshNeighborsSnapshotter_RefreshDecodesAndKeysByEnvelopeMAC
// confirms the snapshotter stores decoded records keyed by the alfred
// envelope MAC. The cache no longer exposes a Stale bool — records
// present in the cache are implicitly fresh (alfred's own TTL purges
// records whose publisher has gone quiet).
func TestMeshNeighborsSnapshotter_RefreshDecodesAndKeysByEnvelopeMAC(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Version: 1, Data: makePayload(t, "aa:bb:cc:dd:ee:01", fixedNow.Add(-5*time.Second))},
		{Source: macBytes("aa:bb:cc:dd:ee:02"), Version: 1, Data: makePayload(t, "aa:bb:cc:dd:ee:02", fixedNow.Add(-10*time.Second))},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour, // ticker never fires; refresh is called by Start()
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	rec1, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	require.True(t, ok)
	assert.Equal(t, "aa:bb:cc:dd:ee:01", rec1.SourceMac)
	assert.Equal(t, int32(15), rec1.Payload.GetAlgorithm())

	_, ok = s.Lookup("AA:BB:CC:DD:EE:02")
	assert.True(t, ok, "mac lookup is case-insensitive")

	all := s.All()
	assert.Len(t, all, 2)
}

// TestMeshNeighborsSnapshotter_OldRecordsStillCached confirms that a
// record whose publisher wall-clock is far in the past (a stand-in for
// large cross-mesh clock skew) is still cached and served. Staleness
// is never decided by the snapshotter any more — alfred's own record
// TTL is the sole "publisher gone quiet" signal.
func TestMeshNeighborsSnapshotter_OldRecordsStillCached(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		// 90 s skew used to cross the old 45 s StaleAge; the consumer
		// must not reject it any more.
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: makePayload(t, "aa:bb:cc:dd:ee:01", fixedNow.Add(-90*time.Second))},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	rec, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	require.True(t, ok, "record with large publisher-clock skew is still cached")
	require.NotNil(t, rec.Payload)
}

// TestMeshNeighborsSnapshotter_EmptyTimestampDropped confirms records
// with no collected_at timestamp (pre-v1 publishers or malformed
// payloads that round-tripped through the decoder) are dropped from
// the cache rather than cached as "stale". An empty timestamp is a
// parse-level signal that the record didn't round-trip cleanly, not a
// staleness signal.
func TestMeshNeighborsSnapshotter_EmptyTimestampDropped(t *testing.T) {
	// makePayloadNoTimestamp builds a MeshNeighbors payload with no
	// collected_at set. Equivalent to what a publisher older than
	// v1 would emit.
	pb := &netv1.MeshNeighbors{
		PrimaryMac: "aa:bb:cc:dd:ee:01",
		Hostname:   "BCM2711-notimestamp",
		Algorithm:  15,
	}
	buf, err := pb.MarshalVT()
	require.NoError(t, err)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: buf},
		{Source: macBytes("aa:bb:cc:dd:ee:02"), Data: makePayload(t, "aa:bb:cc:dd:ee:02", time.Now())},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	_, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	assert.False(t, ok, "record with empty collected_at is dropped")

	_, ok = s.Lookup("aa:bb:cc:dd:ee:02")
	assert.True(t, ok, "valid timestamped record is kept")
}

// TestMeshNeighborsSnapshotter_MalformedPayloadSkipped confirms that a
// corrupt record is logged and discarded without tanking the refresh.
func TestMeshNeighborsSnapshotter_MalformedPayloadSkipped(t *testing.T) {
	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: []byte{0xff, 0xff, 0xff}},
		{Source: macBytes("aa:bb:cc:dd:ee:02"), Data: makePayload(t, "aa:bb:cc:dd:ee:02", time.Now())},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	_, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	assert.False(t, ok, "malformed record dropped")

	_, ok = s.Lookup("aa:bb:cc:dd:ee:02")
	assert.True(t, ok, "valid record kept")
}

// TestMeshNeighborsSnapshotter_CoverageCountsPresentRecords verifies
// Coverage() counts every primary whose record is present in the cache.
// The snapshotter no longer judges freshness by publisher timestamp —
// alfred's own TTL drops records whose publisher has gone quiet, so
// "present" is a sufficient signal.
func TestMeshNeighborsSnapshotter_CoverageCountsPresentRecords(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: makePayload(t, "aa:bb:cc:dd:ee:01", fixedNow.Add(-5*time.Second))},
		// Large publisher-clock skew — previously this would have been
		// rejected as "stale"; now it counts toward coverage.
		{Source: macBytes("aa:bb:cc:dd:ee:02"), Data: makePayload(t, "aa:bb:cc:dd:ee:02", fixedNow.Add(-90*time.Second))},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	published, total := s.Coverage([]string{
		"aa:bb:cc:dd:ee:01", // present
		"aa:bb:cc:dd:ee:02", // present, but old publisher clock
		"aa:bb:cc:dd:ee:03", // absent from alfred
	})
	assert.Equal(t, 3, total)
	assert.Equal(t, 2, published, "every present record counts — clock skew is not a rejection reason")
}

// TestMeshNeighborsSnapshotter_RequestErrorKeepsPreviousData confirms a
// transient alfred failure does not wipe the cache.
func TestMeshNeighborsSnapshotter_RequestErrorKeepsPreviousData(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: makePayload(t, "aa:bb:cc:dd:ee:01", fixedNow.Add(-5*time.Second))},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx) // warms cache with one record.

	fake.mu.Lock()
	fake.err = assert.AnError
	fake.mu.Unlock()

	// Simulate the ticker tick — refresh is exported via Start's loop
	// goroutine; we drive it directly via a fresh Start on the same
	// snapshotter (idempotent because Start just re-refreshes).
	s.Start(ctx)

	rec, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	require.True(t, ok, "cached data survives a Request error")
	assert.NotNil(t, rec.Payload)
}

// contextCancel returns a context with a no-op cancel so the
// snapshotter's background loop exits immediately when the test ends.
func contextCancel() (ctx context.Context, cancel context.CancelFunc) {
	ctx, cancel = context.WithCancel(context.Background())
	cancel() // cancel immediately: loop goroutine checks ctx.Done before first tick

	return ctx, cancel
}
