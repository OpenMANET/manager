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

// fakeVisProvider scripts a single response for MeshTopologyService.
type fakeVisProvider struct {
	mu    sync.Mutex
	doc   *batmanadv.VisDoc
	err   error
	calls int
}

func (f *fakeVisProvider) GetMeshVis(_ context.Context) (*batmanadv.VisDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.err != nil {
		return nil, f.err
	}

	return f.doc, nil
}

// fakeOrigTopology scripts a single originator-topology response.
type fakeOrigTopology struct {
	mu   sync.Mutex
	snap *batmanadv.OriginatorTopology
	err  error
}

func (f *fakeOrigTopology) GetOriginatorTopology() (*batmanadv.OriginatorTopology, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return nil, f.err
	}

	return f.snap, nil
}

// sampleVisDoc returns a minimal mesh:
//   - "me" is self; has edges to alpha and gw1.
//   - alpha ↔ me (local-local RF edge).
//   - me ↔ gw1 (local→remote BLOS edge).
//   - gw1 ↔ remoteA (remote-remote RF edge, no self involvement).
//
// "alpha" carries 2 clients; nothing else has clients.
func sampleVisDoc() *batmanadv.VisDoc {
	return &batmanadv.VisDoc{
		SourceVersion: "2025.4",
		Algorithm:     15, // BATMAN_V
		Vis: []batmanadv.VisNode{
			{
				Primary:   "aa:aa:aa:aa:aa:00",
				Secondary: []string{"aa:aa:aa:aa:aa:01"},
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "aa:aa:aa:aa:aa:00", Neighbor: "bb:bb:bb:bb:bb:00", Metric: "1.100"},
					{Router: "aa:aa:aa:aa:aa:00", Neighbor: "cc:cc:cc:cc:cc:00", Metric: "3.500"},
				},
				Clients: []string{},
			},
			{
				Primary: "bb:bb:bb:bb:bb:00",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "bb:bb:bb:bb:bb:00", Neighbor: "aa:aa:aa:aa:aa:00", Metric: "1.120"},
				},
				Clients: []string{"f0:0d:00:00:00:01", "f0:0d:00:00:00:02"},
			},
			{
				Primary: "cc:cc:cc:cc:cc:00",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "cc:cc:cc:cc:cc:00", Neighbor: "aa:aa:aa:aa:aa:00", Metric: "3.450"},
					{Router: "cc:cc:cc:cc:cc:00", Neighbor: "dd:dd:dd:dd:dd:00", Metric: "1.400"},
				},
				Clients: []string{},
			},
			{
				Primary: "dd:dd:dd:dd:dd:00",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "dd:dd:dd:dd:dd:00", Neighbor: "cc:cc:cc:cc:cc:00", Metric: "1.420"},
				},
				Clients: []string{},
			},
		},
	}
}

// sampleOrigSnap aligns with sampleVisDoc: self is me (aa:...:00), direct
// RF to alpha (bb:...:00), direct BLOS to gw1 (cc:...:00), multi-hop to
// remoteA (dd:...:00) via gw1.
func sampleOrigSnap() *batmanadv.OriginatorTopology {
	return &batmanadv.OriginatorTopology{
		SelfMAC:      "aa:aa:aa:aa:aa:00",
		SelfHostname: "me",
		Algorithm:    "BATMAN_V",
		Originators: []batmanadv.OriginatorEntry{
			{
				OrigMAC: "bb:bb:bb:bb:bb:00", OrigHostname: "alpha_wlan0",
				NextHopMAC: "bb:bb:bb:bb:bb:00", NextHopHostname: "alpha_wlan0",
				HardIfname: "wlan0", Hops: 1,
			},
			{
				OrigMAC: "cc:cc:cc:cc:cc:00", OrigHostname: "gw1_vxlan0",
				NextHopMAC: "cc:cc:cc:cc:cc:00", NextHopHostname: "gw1_vxlan0",
				HardIfname: "vxlan0", Hops: 1,
			},
			{
				OrigMAC: "dd:dd:dd:dd:dd:00", OrigHostname: "remotea_vxlan0",
				NextHopMAC: "cc:cc:cc:cc:cc:00", NextHopHostname: "gw1_vxlan0",
				HardIfname: "vxlan0", Hops: 2,
			},
		},
	}
}

func newMeshTopologyService(vis batmanadv.VisProvider, orig batmanadv.OriginatorTopologyProvider, now func() time.Time) *handlers.MeshTopologyService {
	return &handlers.MeshTopologyService{
		Log:          zerolog.Nop(),
		VisProvider:  vis,
		OrigProvider: orig,
		Now:          now,
	}
}

// TestGetMeshTopology_MergesVisAndOriginators verifies the full merge:
// every vis node shows up, every canonical edge shows up once, segments
// are correctly assigned, and overlay flags (on_my_path, is_self,
// my_hard_ifname) come from the originator table.
func TestGetMeshTopology_MergesVisAndOriginators(t *testing.T) {
	vis := &fakeVisProvider{doc: sampleVisDoc()}
	orig := &fakeOrigTopology{snap: sampleOrigSnap()}
	fixed := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	svc := newMeshTopologyService(vis, orig, func() time.Time { return fixed })

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	topo := resp.GetTopology()
	require.NotNil(t, topo)
	assert.Equal(t, "aa:aa:aa:aa:aa:00", topo.GetSelfMac())
	assert.Equal(t, "me", topo.GetSelfHostname())
	assert.Equal(t, "BATMAN_V", topo.GetAlgorithm())
	assert.Equal(t, fixed.Unix(), topo.GetCollectedAt().GetSeconds())

	nodes := topo.GetNodes()
	require.Len(t, nodes, 4)

	byMac := map[string]int{}
	for i, n := range nodes {
		byMac[n.GetMac()] = i
	}

	selfNode := nodes[byMac["aa:aa:aa:aa:aa:00"]]
	assert.True(t, selfNode.GetIsSelf())
	assert.Equal(t, "local", selfNode.GetSegment())
	assert.Equal(t, int32(0), selfNode.GetHopsFromSelf())
	assert.Equal(t, "me", selfNode.GetHostname())
	assert.Equal(t, []string{"aa:aa:aa:aa:aa:01"}, selfNode.GetSecondaryMacs())

	alphaNode := nodes[byMac["bb:bb:bb:bb:bb:00"]]
	assert.False(t, alphaNode.GetIsSelf())
	assert.Equal(t, "local", alphaNode.GetSegment())
	assert.Equal(t, int32(1), alphaNode.GetHopsFromSelf())
	assert.Equal(t, "wlan0", alphaNode.GetMyHardIfname())
	assert.Equal(t, "alpha", alphaNode.GetHostname())
	assert.Equal(t, int32(2), alphaNode.GetClientCount())

	gw1Node := nodes[byMac["cc:cc:cc:cc:cc:00"]]
	assert.Equal(t, "remote", gw1Node.GetSegment(), "vxlan0-only route → remote segment")
	assert.Equal(t, "vxlan0", gw1Node.GetMyHardIfname())
	assert.Equal(t, int32(1), gw1Node.GetHopsFromSelf())

	remoteNode := nodes[byMac["dd:dd:dd:dd:dd:00"]]
	assert.Equal(t, "remote", remoteNode.GetSegment())
	assert.Equal(t, int32(2), remoteNode.GetHopsFromSelf())

	// Edges: 3 canonical pairs (alpha↔me, me↔gw1, gw1↔remoteA).
	edges := topo.GetEdges()
	require.Len(t, edges, 3)

	edgeByKey := map[string]*struct {
		Blos     bool
		OnMyPath bool
		Metric   float64
	}{}
	for _, e := range edges {
		edgeByKey[e.GetFromMac()+"|"+e.GetToMac()] = &struct {
			Blos     bool
			OnMyPath bool
			Metric   float64
		}{e.GetBlos(), e.GetOnMyPath(), e.GetMetric()}
	}

	require.Contains(t, edgeByKey, "aa:aa:aa:aa:aa:00|bb:bb:bb:bb:bb:00")
	assert.False(t, edgeByKey["aa:aa:aa:aa:aa:00|bb:bb:bb:bb:bb:00"].Blos, "alpha↔me is RF")
	assert.True(t, edgeByKey["aa:aa:aa:aa:aa:00|bb:bb:bb:bb:bb:00"].OnMyPath, "direct neighbor → on my path")

	require.Contains(t, edgeByKey, "aa:aa:aa:aa:aa:00|cc:cc:cc:cc:cc:00")
	assert.True(t, edgeByKey["aa:aa:aa:aa:aa:00|cc:cc:cc:cc:cc:00"].Blos, "me↔gw1 spans segments → BLOS")
	assert.True(t, edgeByKey["aa:aa:aa:aa:aa:00|cc:cc:cc:cc:cc:00"].OnMyPath)

	require.Contains(t, edgeByKey, "cc:cc:cc:cc:cc:00|dd:dd:dd:dd:dd:00")
	assert.False(t, edgeByKey["cc:cc:cc:cc:cc:00|dd:dd:dd:dd:dd:00"].Blos, "both remote → intra-remote RF")
	assert.True(t, edgeByKey["cc:cc:cc:cc:cc:00|dd:dd:dd:dd:dd:00"].OnMyPath, "on my path to remoteA via gw1")
}

// TestGetMeshTopology_VisUnavailableReturnsEmpty asserts an empty render
// response (not an error) when batadv-vis has no data yet.
func TestGetMeshTopology_VisUnavailableReturnsEmpty(t *testing.T) {
	vis := &fakeVisProvider{err: batmanadv.ErrVisUnavailable}
	orig := &fakeOrigTopology{snap: sampleOrigSnap()}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp.GetTopology())
	assert.Empty(t, resp.GetTopology().GetNodes())
	assert.Empty(t, resp.GetTopology().GetEdges())
}

// TestGetMeshTopology_VisErrorIsInternal maps non-sentinel vis failures
// to CodeInternal.
func TestGetMeshTopology_VisErrorIsInternal(t *testing.T) {
	vis := &fakeVisProvider{err: errors.New("exec failed")}
	orig := &fakeOrigTopology{snap: sampleOrigSnap()}

	svc := newMeshTopologyService(vis, orig, nil)

	_, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.Error(t, err)

	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeInternal, ce.Code())
}

// TestGetMeshTopology_OriginatorFailureDegradesGracefully asserts that
// the mesh graph still renders when the originator provider fails —
// overlay fields are empty but nodes/edges are intact.
func TestGetMeshTopology_OriginatorFailureDegradesGracefully(t *testing.T) {
	vis := &fakeVisProvider{doc: sampleVisDoc()}
	orig := &fakeOrigTopology{err: errors.New("batctl failed")}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	topo := resp.GetTopology()
	assert.Len(t, topo.GetNodes(), 4, "vis nodes survive originator failure")
	assert.NotEmpty(t, topo.GetEdges())

	for _, n := range topo.GetNodes() {
		assert.Empty(t, n.GetMyHardIfname(), "no overlay data when originator fails")
		assert.Equal(t, int32(99), n.GetHopsFromSelf(), "hops unknown without originator data")
	}

	for _, e := range topo.GetEdges() {
		assert.False(t, e.GetOnMyPath(), "no on_my_path without originator data")
	}
}

// TestGetMeshTopology_BidirectionalDedup asserts that a vis fixture with
// A→B and B→A reports produces ONE edge, and the better metric wins.
func TestGetMeshTopology_BidirectionalDedup(t *testing.T) {
	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Algorithm: 15, // BATMAN_V: higher is better
		Vis: []batmanadv.VisNode{
			{
				Primary: "aa:aa:aa:aa:aa:00",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "aa:aa:aa:aa:aa:00", Neighbor: "bb:bb:bb:bb:bb:00", Metric: "1.200"},
				},
			},
			{
				Primary: "bb:bb:bb:bb:bb:00",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "bb:bb:bb:bb:bb:00", Neighbor: "aa:aa:aa:aa:aa:00", Metric: "5.000"},
				},
			},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{}}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	edges := resp.GetTopology().GetEdges()
	require.Len(t, edges, 1)
	assert.InDelta(t, 5.0, edges[0].GetMetric(), 1e-9, "BATMAN_V picks the higher metric")
}

// TestGetMeshTopology_DeltaUnchanged asserts the delta RPC still works
// against the renamed service (contract preserved from the prior plan).
func TestGetMeshTopology_DeltaUnchanged(t *testing.T) {
	svc := &handlers.MeshTopologyService{Log: zerolog.Nop()}

	_, err := svc.GetMeshTopologyDelta(context.Background(), nil)
	require.Error(t, err)

	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
}
