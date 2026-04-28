package batmanadv_test

import (
	"context"
	"net"
	"strings"
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

	return makePayloadWithHostname(t, primary, "BCM2711-test", collected)
}

// makePayloadWithHostname is the full constructor that lets tests
// explicitly vary the payload hostname. Used by LookupByHostname tests
// and the multi-mesh fallback scenarios.
func makePayloadWithHostname(t *testing.T, primary, hostname string, collected time.Time) []byte {
	t.Helper()

	pb := &netv1.MeshNeighbors{
		PrimaryMac:  primary,
		Hostname:    hostname,
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

// TestMeshNeighborsSnapshotter_EmptyTimestampStillCached confirms
// records without collected_at are still cached. The snapshotter does
// not second-guess alfred on whether a record is "real" — if alfred
// returned it, the handler gets to see it. Age reporting will surface
// the missing timestamp to the UI (gossip_age_seconds=-1) but the
// record still drives classification.
func TestMeshNeighborsSnapshotter_EmptyTimestampStillCached(t *testing.T) {
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

	rec, ok := s.Lookup("aa:bb:cc:dd:ee:01")
	require.True(t, ok, "record without collected_at is still cached")
	require.NotNil(t, rec.Payload)
	// Confirm the missing timestamp round-tripped: GetCollectedAt()
	// returns nil and AsTime() is the zero time.
	assert.Nil(t, rec.Payload.GetCollectedAt())

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

// TestMeshNeighborsSnapshotter_LookupByHostname covers the multi-mesh
// fallback path. On deployments where alfred gossip runs on a
// different batman-adv instance than the one batadv-vis reports from,
// the envelope MAC and payload.primary_mac both reflect the gossip
// mesh — neither matches a vis primary. Hostname is the one join key
// shared across meshes. Record lookups by hostname must succeed and
// be case-insensitive.
func TestMeshNeighborsSnapshotter_LookupByHostname(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		// envelope MAC, payload.primary_mac, and any hypothetical vis
		// primary would all be different on a real multi-mesh node.
		// The hostname is the only stable identifier.
		{
			Source: macBytes("f2:5e:31:f3:01:81"),
			Data:   makePayloadWithHostname(t, "12:9d:04:6c:2d:75", "BCM2711-1003", fixedNow.Add(-5*time.Second)),
		},
		{
			Source: macBytes("f2:20:f9:84:c3:67"),
			Data:   makePayloadWithHostname(t, "2a:f0:44:57:4e:a9", "BCM2711-fc96", fixedNow.Add(-5*time.Second)),
		},
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

	rec, ok := s.LookupByHostname("BCM2711-1003")
	require.True(t, ok, "hostname match returns the correct record")
	assert.Equal(t, "12:9d:04:6c:2d:75", rec.Payload.GetPrimaryMac())

	// Case-insensitive, matching what the handler's hostname index does.
	rec, ok = s.LookupByHostname("bcm2711-fc96")
	require.True(t, ok, "hostname lookup is case-insensitive")
	assert.Equal(t, "2a:f0:44:57:4e:a9", rec.Payload.GetPrimaryMac())

	_, ok = s.LookupByHostname("Venice-unknown")
	assert.False(t, ok, "unknown hostname returns no record")

	_, ok = s.LookupByHostname("")
	assert.False(t, ok, "empty hostname returns no record — protects against handler passing through a missing bat-hosts entry")
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

// makePayloadWithInterfaceMacs builds a payload that advertises the
// given list of interface MACs. Used by tests that exercise the
// dual-index BLOS multi-mesh path.
func makePayloadWithInterfaceMacs(t *testing.T, primary, hostname string, ifaceMacs []string, collected time.Time) []byte {
	t.Helper()

	pb := &netv1.MeshNeighbors{
		PrimaryMac:    primary,
		Hostname:      hostname,
		Algorithm:     15,
		CollectedAt:   timestamppb.New(collected),
		InterfaceMacs: ifaceMacs,
	}

	buf, err := pb.MarshalVT()
	require.NoError(t, err)

	return buf
}

// TestMeshNeighborsSnapshotter_LookupByInterfaceMac mirrors the user's
// production bug: alfred envelope, payload primary_mac, and the MAC
// the local mesh's batadv-vis sees are all different. interface_macs
// names every MAC the publisher knows about; Lookup must hit on any of
// them. This is the regression test for the BLOS multi-mesh fix.
func TestMeshNeighborsSnapshotter_LookupByInterfaceMac(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	envelopeMAC := "f2:83:94:ae:12:a2"
	payloadPrimary := "ba:ec:19:56:28:c4"
	visPrimary := "bc:2a:33:96:ae:6e"

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{
			Source: macBytes(envelopeMAC),
			Data: makePayloadWithInterfaceMacs(t, payloadPrimary, "Gate_04_27",
				[]string{payloadPrimary, visPrimary, envelopeMAC},
				fixedNow.Add(-5*time.Second)),
		},
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

	t.Run("by alfred envelope MAC", func(t *testing.T) {
		rec, ok := s.Lookup(envelopeMAC)
		require.True(t, ok)
		assert.Equal(t, payloadPrimary, rec.Payload.GetPrimaryMac())
	})

	t.Run("by payload primary_mac", func(t *testing.T) {
		rec, ok := s.Lookup(payloadPrimary)
		require.True(t, ok)
		assert.Equal(t, payloadPrimary, rec.Payload.GetPrimaryMac())
	})

	t.Run("by vis primary listed in interface_macs", func(t *testing.T) {
		rec, ok := s.Lookup(visPrimary)
		require.True(t, ok, "vis primary MAC is shipped via interface_macs and resolves the gossip record")
		assert.Equal(t, "Gate_04_27", rec.Payload.GetHostname())
	})

	t.Run("case-insensitive on interface MAC", func(t *testing.T) {
		_, ok := s.Lookup(strings.ToUpper(visPrimary))
		assert.True(t, ok, "interface_macs lookup is case-insensitive")
	})

	t.Run("unrelated MAC still misses", func(t *testing.T) {
		_, ok := s.Lookup("00:00:00:00:00:00")
		assert.False(t, ok)
	})
}

// TestMeshNeighborsSnapshotter_LookupBackCompatPrimaryOnly asserts a
// publisher that omits interface_macs (older fleet rolling forward)
// is still resolvable by its payload.primary_mac. The snapshotter
// indexes primary_mac into the interface-MAC index even when
// interface_macs is empty.
func TestMeshNeighborsSnapshotter_LookupBackCompatPrimaryOnly(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	envelopeMAC := "f2:83:94:ae:12:a2"
	payloadPrimary := "ba:ec:19:56:28:c4"

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{
			Source: macBytes(envelopeMAC),
			// No interface_macs at all — older publisher.
			Data: makePayloadWithHostname(t, payloadPrimary, "BCM2711-old", fixedNow.Add(-5*time.Second)),
		},
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

	_, ok := s.Lookup(envelopeMAC)
	require.True(t, ok, "envelope MAC always resolves")

	_, ok = s.Lookup(payloadPrimary)
	require.True(t, ok, "payload primary_mac is indexed even without interface_macs")
}

// TestMeshNeighborsSnapshotter_CoverageViaInterfaceMacs asserts a
// primary that only appears in the interface_macs index (vis primary
// of a BLOS-attached gateway) still counts toward published coverage.
func TestMeshNeighborsSnapshotter_CoverageViaInterfaceMacs(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	envelopeMAC := "f2:83:94:ae:12:a2"
	payloadPrimary := "ba:ec:19:56:28:c4"
	visPrimary := "bc:2a:33:96:ae:6e"

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{
			Source: macBytes(envelopeMAC),
			Data: makePayloadWithInterfaceMacs(t, payloadPrimary, "Gate_04_27",
				[]string{payloadPrimary, visPrimary},
				fixedNow.Add(-5*time.Second)),
		},
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

	published, total := s.Coverage([]string{visPrimary})
	assert.Equal(t, 1, total)
	assert.Equal(t, 1, published, "vis primary appearing only in interface_macs counts as published")
}

// TestMeshNeighborsSnapshotter_InterfaceMacCollisionLogsAndKeepsLatest
// covers the misconfiguration case where two publishers claim the
// same interface MAC. The snapshotter resolves last-write-wins (map
// iteration order is unstable, so either record may win — the test
// only asserts one record resolves and the warn line was emitted).
func TestMeshNeighborsSnapshotter_InterfaceMacCollisionLogsAndKeepsLatest(t *testing.T) {
	fixedNow := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	collidingMAC := "11:22:33:44:55:66"

	var captured strings.Builder

	logger := zerolog.New(&captured)

	fake := &fakeAlfredRead{}
	fake.setRecords([]alfred.Record{
		{
			Source: macBytes("aa:aa:aa:aa:aa:01"),
			Data: makePayloadWithInterfaceMacs(t, "aa:aa:aa:aa:aa:01", "node-A",
				[]string{collidingMAC}, fixedNow),
		},
		{
			Source: macBytes("bb:bb:bb:bb:bb:02"),
			Data: makePayloadWithInterfaceMacs(t, "bb:bb:bb:bb:bb:02", "node-B",
				[]string{collidingMAC}, fixedNow),
		},
	})

	s := &batmanadv.MeshNeighborsSnapshotter{
		Log:      logger,
		Client:   fake,
		Interval: time.Hour,
		Now:      func() time.Time { return fixedNow },
	}

	ctx, cancel := contextCancel()
	defer cancel()

	s.Start(ctx)

	rec, ok := s.Lookup(collidingMAC)
	require.True(t, ok, "colliding interface MAC still resolves to a record")
	assert.NotNil(t, rec.Payload)

	assert.Contains(t, captured.String(), "two publishers claim the same interface MAC",
		"collision must be logged at warn level")
}
