package handlers_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	meshtopov1 "github.com/openmanet/openmanetd/internal/api/openmanet/mesh_topology/v1"
	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
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

// TestGetMeshTopology_RemoteGatewaySplit asserts that remote peers
// reached through two DIFFERENT direct BLOS neighbors land in SEPARATE
// mesh segments via distinct remote_gateway_mac values. Peers behind
// the same gateway (multi-hop chain) share a segment.
func TestGetMeshTopology_RemoteGatewaySplit(t *testing.T) {
	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			{Primary: "aa:aa:aa:aa:aa:00"}, // self
			{Primary: "cc:cc:cc:cc:cc:00"}, // gw1 (direct BLOS)
			{Primary: "dd:dd:dd:dd:dd:00"}, // behind gw1
			{Primary: "ee:ee:ee:ee:ee:00"}, // gw2 (different direct BLOS)
			{Primary: "ff:ff:ff:ff:ff:00"}, // behind gw2
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC:      "aa:aa:aa:aa:aa:00",
		SelfHostname: "me",
		Algorithm:    "BATMAN_V",
		Originators: []batmanadv.OriginatorEntry{
			{
				OrigMAC: "cc:cc:cc:cc:cc:00", NextHopMAC: "cc:cc:cc:cc:cc:00",
				HardIfname: "vxlan0", Hops: 1,
			},
			{
				OrigMAC: "dd:dd:dd:dd:dd:00", NextHopMAC: "cc:cc:cc:cc:cc:00",
				HardIfname: "vxlan0", Hops: 2,
			},
			{
				OrigMAC: "ee:ee:ee:ee:ee:00", NextHopMAC: "ee:ee:ee:ee:ee:00",
				HardIfname: "vxlan0", Hops: 1,
			},
			{
				OrigMAC: "ff:ff:ff:ff:ff:00", NextHopMAC: "ee:ee:ee:ee:ee:00",
				HardIfname: "vxlan0", Hops: 2,
			},
		},
	}}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	gwByMac := map[string]string{}
	for _, n := range resp.GetTopology().GetNodes() {
		gwByMac[n.GetMac()] = n.GetRemoteGatewayMac()
	}

	assert.Equal(t, "cc:cc:cc:cc:cc:00", gwByMac["cc:cc:cc:cc:cc:00"], "direct BLOS peer is its own gateway")
	assert.Equal(t, "cc:cc:cc:cc:cc:00", gwByMac["dd:dd:dd:dd:dd:00"], "multi-hop peer inherits the chain's gateway")
	assert.Equal(t, "ee:ee:ee:ee:ee:00", gwByMac["ee:ee:ee:ee:ee:00"], "second direct BLOS peer is its own gateway")
	assert.Equal(t, "ee:ee:ee:ee:ee:00", gwByMac["ff:ff:ff:ff:ff:00"], "multi-hop behind gw2 inherits gw2")
	assert.Empty(t, gwByMac["aa:aa:aa:aa:aa:00"], "self has no gateway")
}

// TestGetMeshTopology_SynthesizesEdgesWhenVisEmpty verifies the
// originator-derived edge fallback: when batadv-vis returns only node
// stubs with no neighbor reports (alfred cold-start, peer vis-servers
// silent), the handler still emits enough edges to visually connect
// every peer we route to.
func TestGetMeshTopology_SynthesizesEdgesWhenVisEmpty(t *testing.T) {
	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			{Primary: "aa:aa:aa:aa:aa:00"},
			{Primary: "bb:bb:bb:bb:bb:00"},
			{Primary: "cc:cc:cc:cc:cc:00"},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC: "aa:aa:aa:aa:aa:00",
		Originators: []batmanadv.OriginatorEntry{
			{OrigMAC: "bb:bb:bb:bb:bb:00", NextHopMAC: "bb:bb:bb:bb:bb:00", HardIfname: "wlan0", Hops: 1},
			{OrigMAC: "cc:cc:cc:cc:cc:00", NextHopMAC: "bb:bb:bb:bb:bb:00", HardIfname: "wlan0", Hops: 2},
		},
	}}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	edges := resp.GetTopology().GetEdges()
	require.Len(t, edges, 2, "direct-neighbor edge + downstream chain edge")

	for _, e := range edges {
		assert.True(t, e.GetOnMyPath(), "originator-derived edges are by definition on my path")
	}
}

// TestGetMeshTopology_AlgorithmFromBatmanAdv asserts the algorithm chip
// reads the batman-adv (originator) algorithm label, never the
// batadv-vis header, so the UI mirrors what batctl reports.
func TestGetMeshTopology_AlgorithmFromBatmanAdv(t *testing.T) {
	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Algorithm: 15, // BATMAN_V in vis header
		Vis:       []batmanadv.VisNode{{Primary: "aa:aa:aa:aa:aa:00"}},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC:   "aa:aa:aa:aa:aa:00",
		Algorithm: "BATMAN_IV", // what batctl actually reported
	}}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	assert.Equal(t, "BATMAN_IV", resp.GetTopology().GetAlgorithm(),
		"algorithm reflects batman-adv (batctl), not the vis-header")
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

// TestGetMeshTopology_DedupesSelfAcrossAliasedVisEntries asserts that when
// batadv-vis publishes two entries describing the same physical node
// (self reported once as Primary, once as a Secondary under another
// Primary), only one MeshNode is emitted. Without the dedup, operators
// see self twice on the canvas.
func TestGetMeshTopology_DedupesSelfAcrossAliasedVisEntries(t *testing.T) {
	const selfMAC = "aa:aa:aa:aa:aa:00"

	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			// The canonical entry claims selfMAC as a secondary —
			// this "11:..." primary is the authoritative identity.
			{
				Primary:   "11:11:11:11:11:11",
				Secondary: []string{selfMAC},
			},
			// Alfred also republished a bare entry with selfMAC as
			// its primary. Without dedup this produces a duplicate
			// node.
			{Primary: selfMAC},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC: selfMAC,
	}}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	nodes := resp.GetTopology().GetNodes()
	require.Len(t, nodes, 1, "aliased vis entries must collapse to one node")

	assert.True(t, nodes[0].GetIsSelf(), "the surviving node represents self")
	assert.Equal(t, "11:11:11:11:11:11", nodes[0].GetMac(),
		"canonical primary wins; duplicate entry's primary is dropped")
}

// TestGetMeshTopology_DedupesByHostname asserts that two vis entries
// with different primaries and no shared secondary list collapse to one
// MeshNode when they resolve to the same base hostname. This is the
// common multi-radio-node case: each interface publishes its own vis
// entry (bat0, eth0, wlan0) carrying a distinct MAC, but all three
// belong to the same physical device identified by bat-hosts name
// "BCM2711-1003".
func TestGetMeshTopology_DedupesByHostname(t *testing.T) {
	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			// Self (bare MAC, no hostname in vis payload).
			{Primary: "aa:aa:aa:aa:aa:00"},
			// Two entries for the same physical remote node. Neither
			// lists the other as a secondary — the only cross-entry
			// link is the shared base hostname "BCM2711-1003".
			{Primary: "bb:bb:bb:bb:bb:01"},
			{Primary: "bb:bb:bb:bb:bb:02"},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC:      "aa:aa:aa:aa:aa:00",
		SelfHostname: "self-node",
		Originators: []batmanadv.OriginatorEntry{
			{
				OrigMAC: "bb:bb:bb:bb:bb:01", OrigHostname: "BCM2711-1003_bat0",
				NextHopMAC: "bb:bb:bb:bb:bb:01", HardIfname: "wlan0", Hops: 1,
			},
			{
				OrigMAC: "bb:bb:bb:bb:bb:02", OrigHostname: "BCM2711-1003_eth0",
				NextHopMAC: "bb:bb:bb:bb:bb:02", HardIfname: "wlan0", Hops: 1,
			},
		},
	}}

	svc := newMeshTopologyService(vis, orig, nil)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	nodes := resp.GetTopology().GetNodes()
	require.Len(t, nodes, 2, "BCM2711-1003's two vis entries must collapse to one")

	var remote *struct {
		hostname string
		mac      string
	}

	for _, n := range nodes {
		if n.GetIsSelf() {
			continue
		}

		remote = &struct {
			hostname string
			mac      string
		}{n.GetHostname(), n.GetMac()}
	}

	require.NotNil(t, remote, "non-self node exists")
	assert.Equal(t, "BCM2711-1003", remote.hostname, "base hostname is retained")
	assert.Equal(t, "bb:bb:bb:bb:bb:01", remote.mac,
		"lex-smallest MAC in the group wins when self isn't in the group")
}

// fakeNeighborsProvider scripts MeshNeighbors gossip responses for the
// handler. Maps lowercase primary MACs to their canned MeshNeighbors
// payload. Missing entries cause Lookup to return (nil, false).
type fakeNeighborsProvider struct {
	records map[string]*batmanadv.MeshNeighborsRecord
}

func (f *fakeNeighborsProvider) Lookup(primaryMac string) (*batmanadv.MeshNeighborsRecord, bool) {
	if rec, ok := f.records[primaryMac]; ok {
		return rec, true
	}

	return nil, false
}

func (f *fakeNeighborsProvider) LookupByHostname(hostname string) (*batmanadv.MeshNeighborsRecord, bool) {
	if hostname == "" {
		return nil, false
	}

	for _, rec := range f.records {
		if rec.Payload != nil && strings.EqualFold(rec.Payload.GetHostname(), hostname) {
			return rec, true
		}
	}

	return nil, false
}

func (f *fakeNeighborsProvider) All() map[string]*batmanadv.MeshNeighborsRecord {
	out := make(map[string]*batmanadv.MeshNeighborsRecord, len(f.records))
	for k, v := range f.records {
		out[k] = v
	}

	return out
}

// TestGetMeshTopology_GossipClassifiesNodeBehindRemoteGateway is the
// canonical regression test for the BCM2711-fc8e → BCM2711-fc96 bug:
// fc8e is an RF peer of fc96 (the gateway), but without gossip the
// handler would render fc8e as its own remote segment. With gossip,
// fc96's record says fc8e is on wlan0 → fc8e lives in fc96's remote
// mesh component and inherits fc96 as its gateway.
func TestGetMeshTopology_GossipClassifiesNodeBehindRemoteGateway(t *testing.T) {
	const (
		selfMAC = "aa:aa:aa:aa:aa:01"
		gwMAC   = "bb:bb:bb:bb:bb:01" // fc96
		behMAC  = "cc:cc:cc:cc:cc:01" // fc8e
	)

	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			{Primary: selfMAC},
			{Primary: gwMAC},
			{Primary: behMAC},
		},
	}}

	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC:      selfMAC,
		SelfHostname: "self",
		Originators: []batmanadv.OriginatorEntry{
			// Both remote peers look like direct vxlan0 neighbors from
			// self's perspective — exactly the broadcast-overlay case
			// the heuristic can't disambiguate.
			{OrigMAC: gwMAC, NextHopMAC: gwMAC, HardIfname: "vxlan0", Hops: 1},
			{OrigMAC: behMAC, NextHopMAC: behMAC, HardIfname: "vxlan0", Hops: 1},
		},
	}}

	// fc96 publishes gossip stating fc8e is an RF peer on wlan0.
	gwPayload := &netv1.MeshNeighbors{
		PrimaryMac: gwMAC,
		Neighbors: []*netv1.MeshNeighbor{
			{Mac: behMAC, HardIfname: "wlan0", Blos: false},
			{Mac: selfMAC, HardIfname: "vxlan0", Blos: true},
		},
	}
	behPayload := &netv1.MeshNeighbors{
		PrimaryMac: behMAC,
		Neighbors: []*netv1.MeshNeighbor{
			{Mac: gwMAC, HardIfname: "wlan0", Blos: false},
		},
	}

	neighbors := &fakeNeighborsProvider{records: map[string]*batmanadv.MeshNeighborsRecord{
		gwMAC:  {Payload: gwPayload, SourceMac: gwMAC},
		behMAC: {Payload: behPayload, SourceMac: behMAC},
	}}

	svc := newMeshTopologyService(vis, orig, nil)
	svc.NeighborsProvider = neighbors

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	byMac := make(map[string]*meshtopov1.MeshNode)
	for _, n := range resp.GetTopology().GetNodes() {
		byMac[n.GetMac()] = n
	}

	require.Contains(t, byMac, gwMAC)
	require.Contains(t, byMac, behMAC)

	assert.Equal(t, "remote", byMac[gwMAC].GetSegment())
	assert.Equal(t, "remote", byMac[behMAC].GetSegment(),
		"fc8e must land in a remote segment, not local")
	assert.Equal(t, gwMAC, byMac[behMAC].GetRemoteGatewayMac(),
		"fc8e's remote gateway is fc96, not itself")
	assert.Equal(t, gwMAC, byMac[gwMAC].GetRemoteGatewayMac(),
		"the gateway itself points at itself so UI groups them together")
}

// TestGetMeshTopology_GossipCoverageReflectsPublishers confirms the
// MeshTopology.GossipCoverage field counts non-self nodes with fresh
// gossip records — and *only* those confirmed via a successful
// buildGossipView match. Originator-synthesized nodes (present in the
// route table but not in vis) must not be counted as published by
// accident; that was the pre-fix false positive where a node without
// any gossip record still showed GOSSIP N/M with N inflated by one.
func TestGetMeshTopology_GossipCoverageReflectsPublishers(t *testing.T) {
	const (
		selfMAC  = "aa:aa:aa:aa:aa:00"
		pubMAC   = "bb:bb:bb:bb:bb:00"
		quietMAC = "cc:cc:cc:cc:cc:00"
		// synthMAC appears only in the originator table, never in vis —
		// buildGossipView never visits it, so it should neither count
		// toward published nor provoke a stale flag.
		synthMAC = "dd:dd:dd:dd:dd:00"
	)

	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			{Primary: selfMAC},
			{Primary: pubMAC},
			{Primary: quietMAC},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC: selfMAC,
		Originators: []batmanadv.OriginatorEntry{
			{OrigMAC: synthMAC, NextHopMAC: synthMAC, HardIfname: "wlan0", Hops: 1},
		},
	}}

	neighbors := &fakeNeighborsProvider{records: map[string]*batmanadv.MeshNeighborsRecord{
		pubMAC: {Payload: &netv1.MeshNeighbors{PrimaryMac: pubMAC}, SourceMac: pubMAC},
	}}

	svc := newMeshTopologyService(vis, orig, nil)
	svc.NeighborsProvider = neighbors

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	cov := resp.GetTopology().GetGossipCoverage()
	require.NotNil(t, cov)
	assert.Equal(t, int32(3), cov.GetTotal(), "3 non-self nodes rendered: pubMAC, quietMAC, and the synthesized originator-only node")
	assert.Equal(t, int32(1), cov.GetPublished(),
		"only pubMAC has a gossip record; quietMAC has none and synthMAC was never visited by buildGossipView")
}

// TestGetMeshTopology_GossipHostnameFallback covers the multi-mesh
// deployment where alfred gossip runs on a different batman-adv
// instance than the one vis reports. The record's envelope MAC and
// payload.primary_mac both differ from the vis primary; only the
// hostname matches. buildGossipView must fall back to LookupByHostname
// before marking the node stale.
func TestGetMeshTopology_GossipHostnameFallback(t *testing.T) {
	const (
		selfMAC    = "aa:aa:aa:aa:aa:00"
		visPrimary = "3c:22:7f:71:df:30" // batadv-vis's view of the peer (bat0)
		gossipMAC  = "2a:f0:44:57:4e:a9" // the same peer's MAC on the batmesh1 address space
		sharedHost = "BCM2711-fc96"
	)

	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			{Primary: selfMAC},
			{Primary: visPrimary},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{
		SelfMAC: selfMAC,
		Originators: []batmanadv.OriginatorEntry{
			{OrigMAC: visPrimary, OrigHostname: sharedHost + "_bat0", NextHopMAC: visPrimary, HardIfname: "vxlan0", Hops: 1},
		},
	}}

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	collected := now.Add(-10 * time.Second)

	// Record's keys don't match the vis primary at all — exactly the
	// BCM2711-fc96 case from the field evidence. Only the hostname
	// connects the two meshes.
	neighbors := &fakeNeighborsProvider{records: map[string]*batmanadv.MeshNeighborsRecord{
		// Keyed by envelope MAC, which the fake keeps for Lookup tests;
		// real snapshotter key is arbitrary and irrelevant here since
		// no lookup path will target it.
		"f2:20:f9:84:c3:67": {
			Payload: &netv1.MeshNeighbors{
				PrimaryMac:  gossipMAC,
				Hostname:    sharedHost,
				CollectedAt: timestamppb.New(collected),
				Neighbors: []*netv1.MeshNeighbor{
					{Mac: "some-rf-peer", HardIfname: "wlan0"},
				},
			},
			SourceMac: "f2:20:f9:84:c3:67",
		},
	}}

	svc := newMeshTopologyService(vis, orig, func() time.Time { return now })
	svc.NeighborsProvider = neighbors

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	byMac := make(map[string]*meshtopov1.MeshNode)
	for _, n := range resp.GetTopology().GetNodes() {
		byMac[n.GetMac()] = n
	}

	peer := byMac[visPrimary]
	require.NotNil(t, peer, "peer is rendered under its vis primary MAC")
	assert.False(t, peer.GetGossipStale(),
		"hostname-based fallback must match the gossip record across mesh boundaries")
	assert.Equal(t, int32(10), peer.GetGossipAgeSeconds(),
		"age still computed from payload.collected_at after the fallback hit")

	cov := resp.GetTopology().GetGossipCoverage()
	require.NotNil(t, cov)
	assert.Equal(t, int32(1), cov.GetPublished(),
		"coverage reflects the hostname-matched record")
}

// TestGetMeshTopology_GossipStatePropagates confirms the wire shape of
// gossip bookkeeping. Under the post-clock-skew design, staleness is
// purely a presence check — alfred's own record TTL drops publishers
// that have gone quiet, so a cache miss is the sole "stale" signal.
// Age is still reported from payload.collected_at as an independent
// dimension, so the UI can distinguish "just heard" from "heard long
// ago" without using age for any rejection decisions.
func TestGetMeshTopology_GossipStatePropagates(t *testing.T) {
	const (
		selfMAC      = "aa:aa:aa:aa:aa:00"
		recentMAC    = "bb:bb:bb:bb:bb:00"
		clockSkewMAC = "cc:cc:cc:cc:cc:00"
		missingMAC   = "dd:dd:dd:dd:dd:00"
	)

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	vis := &fakeVisProvider{doc: &batmanadv.VisDoc{
		Vis: []batmanadv.VisNode{
			{Primary: selfMAC},
			{Primary: recentMAC},
			{Primary: clockSkewMAC},
			{Primary: missingMAC},
		},
	}}
	orig := &fakeOrigTopology{snap: &batmanadv.OriginatorTopology{SelfMAC: selfMAC}}

	recentCollected := now.Add(-8 * time.Second)
	// clockSkewMAC's publisher wall-clock reports 2m 14s in the past.
	// Previously this crossed the 45 s StaleAge and was rejected; now
	// the handler must accept it because alfred returned the record.
	clockSkewCollected := now.Add(-134 * time.Second)

	neighbors := &fakeNeighborsProvider{records: map[string]*batmanadv.MeshNeighborsRecord{
		recentMAC: {
			Payload:   &netv1.MeshNeighbors{PrimaryMac: recentMAC, CollectedAt: timestamppb.New(recentCollected)},
			SourceMac: recentMAC,
		},
		clockSkewMAC: {
			Payload:   &netv1.MeshNeighbors{PrimaryMac: clockSkewMAC, CollectedAt: timestamppb.New(clockSkewCollected)},
			SourceMac: clockSkewMAC,
		},
	}}

	svc := newMeshTopologyService(vis, orig, func() time.Time { return now })
	svc.NeighborsProvider = neighbors

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	byMac := make(map[string]*meshtopov1.MeshNode)
	for _, n := range resp.GetTopology().GetNodes() {
		byMac[n.GetMac()] = n
	}

	// Self is never stale and carries no explicit age.
	self := byMac[selfMAC]
	require.NotNil(t, self)
	assert.True(t, self.GetIsSelf())
	assert.False(t, self.GetGossipStale(), "self is never stale")
	assert.Equal(t, int32(0), self.GetGossipAgeSeconds(), "self carries zero age")

	// Recent record — not stale, age rounds to the raw second delta.
	recent := byMac[recentMAC]
	require.NotNil(t, recent)
	assert.False(t, recent.GetGossipStale(), "record present in cache is not stale")
	assert.Equal(t, int32(8), recent.GetGossipAgeSeconds(), "age matches (now - collected_at) in seconds")

	// Record with large publisher-clock skew — still NOT stale, because
	// the record is present in alfred's cache. The reported age reflects
	// the publisher's wall-clock so the UI can show the lag, but the
	// stale flag stays false so classification and dimming use the
	// record's actual neighbor set.
	skew := byMac[clockSkewMAC]
	require.NotNil(t, skew)
	assert.False(t, skew.GetGossipStale(),
		"record with large publisher-clock skew is still fresh — presence alone is the stale signal")
	assert.Equal(t, int32(134), skew.GetGossipAgeSeconds(),
		"age still reflects the publisher's collected_at for UI display")

	// Missing record — stale, -1 sentinel age.
	missing := byMac[missingMAC]
	require.NotNil(t, missing)
	assert.True(t, missing.GetGossipStale(),
		"no cached record is the sole stale signal after the clock-skew fix")
	assert.Equal(t, int32(-1), missing.GetGossipAgeSeconds(),
		"missing record surfaces ageMissing sentinel")
}

// TestGetMeshTopology_MissingGossipFallsBackToHeuristic asserts that
// when gossip is absent for a primary, the legacy chain-walk classifier
// still runs for it. This is the mixed-fleet safety net.
func TestGetMeshTopology_MissingGossipFallsBackToHeuristic(t *testing.T) {
	// Same fixture as the original merge test but with a nil
	// NeighborsProvider — behavior must match pre-gossip handler.
	vis := &fakeVisProvider{doc: sampleVisDoc()}
	orig := &fakeOrigTopology{snap: sampleOrigSnap()}

	svc := newMeshTopologyService(vis, orig, nil)
	svc.NeighborsProvider = nil // explicit — fallback path

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	// Assert a known gw1 node is remote — unchanged from the pre-gossip test.
	for _, n := range resp.GetTopology().GetNodes() {
		if n.GetMac() == "cc:cc:cc:cc:cc:00" {
			assert.Equal(t, "remote", n.GetSegment())
		}
	}
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
