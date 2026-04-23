package handlers_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeOrigTopology scripts a single response for the MeshTopologyService to
// consume. Goroutine-safe so it can be reused across tests that poll.
type fakeOrigTopology struct {
	mu    sync.Mutex
	snap  *batmanadv.OriginatorTopology
	err   error
	calls int
}

func (f *fakeOrigTopology) GetOriginatorTopology() (*batmanadv.OriginatorTopology, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.err != nil {
		return nil, f.err
	}

	return f.snap, nil
}

func (f *fakeOrigTopology) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// sampleOrigTopology returns a snapshot with one direct RF neighbor, one
// multi-hop RF peer, and one direct BLOS neighbor — enough to exercise
// hostname round-tripping and hop propagation through the handler.
func sampleOrigTopology() *batmanadv.OriginatorTopology {
	return &batmanadv.OriginatorTopology{
		SelfMAC:      "0a:d7:37:78:2d:3e",
		SelfHostname: "BCM2711-97d6",
		Algorithm:    "BATMAN_IV",
		Originators: []batmanadv.OriginatorEntry{
			{
				OrigMAC:         "9c:ef:d5:f9:9e:02",
				OrigHostname:    "BCM2711-88ba_phy2-mesh0",
				NextHopMAC:      "9c:ef:d5:f9:9e:02",
				NextHopHostname: "BCM2711-88ba_phy2-mesh0",
				HardIfname:      "phy2-mesh0",
				TQ:              255,
				LastSeenMs:      120,
				Hops:            1,
			},
			{
				OrigMAC:         "00:0a:52:0b:7d:ae",
				OrigHostname:    "BCM2711-1003_phy1-mesh0",
				NextHopMAC:      "9c:ef:d5:f9:9e:02",
				NextHopHostname: "BCM2711-88ba_phy2-mesh0",
				HardIfname:      "phy2-mesh0",
				TQ:              210,
				LastSeenMs:      240,
				Hops:            2,
			},
			{
				OrigMAC:         "2c:cf:67:b8:88:bb",
				OrigHostname:    "BLOS-GW1_vxlan0",
				NextHopMAC:      "2c:cf:67:b8:88:bb",
				NextHopHostname: "BLOS-GW1_vxlan0",
				HardIfname:      "vxlan0",
				TQ:              230,
				LastSeenMs:      400,
				Hops:            1,
			},
		},
	}
}

func newMeshTopologyService(orig batmanadv.OriginatorTopologyProvider, now func() time.Time) *handlers.MeshTopologyService {
	return &handlers.MeshTopologyService{
		Log:          zerolog.Nop(),
		OrigProvider: orig,
		Now:          now,
	}
}

// TestGetMeshTopology_Success round-trips a multi-entry snapshot through the
// handler and asserts every proto field maps 1:1 from the domain type.
func TestGetMeshTopology_Success(t *testing.T) {
	orig := &fakeOrigTopology{snap: sampleOrigTopology()}
	fixed := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	svc := newMeshTopologyService(orig, func() time.Time { return fixed })

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp.GetTopology())
	assert.Equal(t, 1, orig.callCount())

	topo := resp.GetTopology()
	assert.Equal(t, "0a:d7:37:78:2d:3e", topo.GetSelfMac())
	assert.Equal(t, "BCM2711-97d6", topo.GetSelfHostname())
	assert.Equal(t, "BATMAN_IV", topo.GetAlgorithm())
	require.NotNil(t, topo.GetCollectedAt())
	assert.Equal(t, fixed.Unix(), topo.GetCollectedAt().GetSeconds())
	require.Len(t, topo.GetOriginators(), 3)

	first := topo.GetOriginators()[0]
	assert.Equal(t, "9c:ef:d5:f9:9e:02", first.GetOrigMac())
	assert.Equal(t, "BCM2711-88ba_phy2-mesh0", first.GetOrigHostname())
	assert.Equal(t, "9c:ef:d5:f9:9e:02", first.GetNextHopMac())
	assert.Equal(t, "phy2-mesh0", first.GetHardIfname())
	assert.Equal(t, int32(255), first.GetTq())
	assert.Equal(t, int32(1), first.GetHops())

	blos := topo.GetOriginators()[2]
	assert.Equal(t, "vxlan0", blos.GetHardIfname(), "BLOS edge surfaces hard_ifname so the frontend can segment it")
	assert.Equal(t, "BLOS-GW1_vxlan0", blos.GetOrigHostname())
}

// TestGetMeshTopology_OriginatorsUnavailable maps the provider's sentinel
// error to CodeFailedPrecondition so the frontend can render a dedicated
// "mesh not running" state instead of a generic internal error.
func TestGetMeshTopology_OriginatorsUnavailable(t *testing.T) {
	orig := &fakeOrigTopology{err: batmanadv.ErrOriginatorsUnavailable}
	svc := newMeshTopologyService(orig, nil)

	_, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.Error(t, err)

	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
}

// TestGetMeshTopology_InternalError surfaces non-sentinel errors as
// CodeInternal — distinct from the "unavailable" path.
func TestGetMeshTopology_InternalError(t *testing.T) {
	orig := &fakeOrigTopology{err: errors.New("corrupt JSON")}
	svc := newMeshTopologyService(orig, nil)

	_, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.Error(t, err)

	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeInternal, ce.Code())
}

// TestGetMeshTopology_Empty returns an empty originator list without
// panicking when the mesh has no discovered peers yet.
func TestGetMeshTopology_Empty(t *testing.T) {
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{}}
	svc := newMeshTopologyService(orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetTopology().GetOriginators())
	assert.Empty(t, resp.GetTopology().GetSelfMac())
}
