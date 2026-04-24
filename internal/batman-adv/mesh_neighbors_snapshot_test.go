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
// envelope MAC and marks fresh records as non-stale.
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
		StaleAge: 30 * time.Second,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	rec1, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	require.True(t, ok)
	assert.False(t, rec1.Stale, "record 5 s old < StaleAge")
	assert.Equal(t, "aa:bb:cc:dd:ee:01", rec1.SourceMac)
	assert.Equal(t, int32(15), rec1.Payload.GetAlgorithm())

	_, ok = s.Lookup("AA:BB:CC:DD:EE:02")
	assert.True(t, ok, "mac lookup is case-insensitive")

	all := s.All()
	assert.Len(t, all, 2)
}

// TestMeshNeighborsSnapshotter_MarksStaleRecords confirms records whose
// collected_at is older than StaleAge are served with Stale=true.
func TestMeshNeighborsSnapshotter_MarksStaleRecords(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: makePayload(t, "aa:bb:cc:dd:ee:01", fixedNow.Add(-90*time.Second))},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
		StaleAge: 45 * time.Second,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	rec, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	require.True(t, ok)
	assert.True(t, rec.Stale, "90 s > 45 s StaleAge")
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

// TestMeshNeighborsSnapshotter_CoverageCountsFreshRecords verifies
// Coverage() counts only non-stale records.
func TestMeshNeighborsSnapshotter_CoverageCountsFreshRecords(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{Source: macBytes("aa:bb:cc:dd:ee:01"), Data: makePayload(t, "aa:bb:cc:dd:ee:01", fixedNow.Add(-5*time.Second))},
		{Source: macBytes("aa:bb:cc:dd:ee:02"), Data: makePayload(t, "aa:bb:cc:dd:ee:02", fixedNow.Add(-90*time.Second))},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      zerolog.Nop(),
		Client:   fake,
		Interval: time.Hour,
		StaleAge: 30 * time.Second,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	published, total := s.Coverage([]string{
		"aa:bb:cc:dd:ee:01", // fresh
		"aa:bb:cc:dd:ee:02", // stale
		"aa:bb:cc:dd:ee:03", // not in gossip
	})
	assert.Equal(t, 3, total)
	assert.Equal(t, 1, published, "only fresh records count")
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
