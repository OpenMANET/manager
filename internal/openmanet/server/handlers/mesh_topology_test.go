package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/mdlayher/wifi"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/openmanet/server/handlers"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// makeInterfaceWithMAC returns a wifi.Interface with the given MAC, useful
// when a test must line up a local interface MAC with router_mac in a
// VisEntry (signal enrichment only fires for edges originating from our
// own radios).
func makeInterfaceWithMAC(name, macStr string) *wifi.Interface {
	mac, _ := net.ParseMAC(macStr)

	iface := makeInterface(name, wifi.InterfaceTypeMeshPoint)
	iface.HardwareAddr = mac

	return iface
}

// fakeVisibilityProvider implements batmanadv.VisibilityProvider for tests.
type fakeVisibilityProvider struct {
	mu    sync.Mutex
	doc   *batmanadv.VisDoc
	err   error
	calls int
}

func (f *fakeVisibilityProvider) GetVisibility() (*batmanadv.VisDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.err != nil {
		return nil, f.err
	}

	return f.doc, nil
}

func (f *fakeVisibilityProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// sampleVisDoc returns a two-node VisDoc used across tests. Keep in sync with
// testfixtures/batman-adv/vis-jsondoc.json.
func sampleVisDoc() *batmanadv.VisDoc {
	return &batmanadv.VisDoc{
		SourceVersion: "2013.3.0-14-gcd34783",
		Algorithm:     4,
		Vis: []batmanadv.VisEntry{
			{
				Primary:   "0a:d7:37:78:2d:3e",
				Secondary: []string{"2c:cf:67:6a:97:d9"},
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "9c:ef:d5:f9:80:4d", Neighbor: "9c:ef:d5:f9:9e:02", Metric: "1.008"},
					{Router: "9c:ef:d5:f9:80:4d", Neighbor: "00:0a:52:0b:7d:ae", Metric: "1.250"},
				},
				Clients: []string{"3c:22:7f:37:4c:0c", "2c:cf:67:6a:97:d6"},
			},
			{
				Primary: "2c:cf:67:b8:88:ba",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "9c:ef:d5:f9:9e:02", Neighbor: "9c:ef:d5:f9:80:4d", Metric: "1.000"},
				},
				Clients: []string{"bc:2a:33:96:b1:84"},
			},
		},
	}
}

func newMeshTopologyService(
	vis batmanadv.VisibilityProvider,
	fw *fakeWireless,
	parseBatHosts func(string) (*batmanadv.BatHosts, error),
	now func() time.Time,
) *handlers.MeshTopologyService {
	return &handlers.MeshTopologyService{
		Log:           zerolog.Nop(),
		Visibility:    vis,
		Wifi:          fw,
		ParseBatHosts: parseBatHosts,
		Now:           now,
	}
}

func TestGetMeshTopology_Success(t *testing.T) {
	vis := &fakeVisibilityProvider{doc: sampleVisDoc()}

	// The mesh interface's MAC must match the router_mac in the fixture's
	// first entry so signal enrichment fires (edges are only enriched when
	// they originate from one of our own radios).
	meshIface := makeInterfaceWithMAC("mesh0", "9c:ef:d5:f9:80:4d")
	// Station whose MAC matches the first edge's neighbor — expect signal
	// enrichment on that edge and zeros on the others.
	station := makeStation("9c:ef:d5:f9:9e:02", -65)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{meshIface},
		stationInfo:    []*wifi.StationInfo{station},
	}

	fixed := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	svc := newMeshTopologyService(vis, fw,
		func(_ string) (*batmanadv.BatHosts, error) {
			return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
		},
		func() time.Time { return fixed },
	)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)
	require.NotNil(t, resp.GetTopology())
	assert.Equal(t, 1, vis.callCount())

	topo := resp.GetTopology()
	assert.Equal(t, "2013.3.0-14-gcd34783", topo.GetSourceVersion())
	assert.Equal(t, int32(4), topo.GetAlgorithm())
	require.Len(t, topo.GetNodes(), 2)
	require.NotNil(t, topo.GetCollectedAt())
	assert.Equal(t, fixed.Unix(), topo.GetCollectedAt().GetSeconds())

	node0 := topo.GetNodes()[0]
	assert.Equal(t, "0a:d7:37:78:2d:3e", node0.GetPrimaryMac())
	assert.Equal(t, "BCM2711-97d6_bat0", node0.GetPrimaryHostname())
	assert.Equal(t, []string{"2c:cf:67:6a:97:d9"}, node0.GetSecondaryMacs())
	require.Len(t, node0.GetNeighbors(), 2)
	require.Len(t, node0.GetClients(), 2)

	edge0 := node0.GetNeighbors()[0]
	assert.Equal(t, "9c:ef:d5:f9:80:4d", edge0.GetRouterMac())
	assert.Equal(t, "BCM2711-97d6_phy2-mesh0", edge0.GetRouterHostname())
	assert.Equal(t, "9c:ef:d5:f9:9e:02", edge0.GetNeighborMac())
	assert.Equal(t, "BCM2711-88ba_phy2-mesh0", edge0.GetNeighborHostname())
	assert.Less(t, math.Abs(float64(edge0.GetMetric()-1.008)), 1e-4)
	assert.Equal(t, int32(-65), edge0.GetSignal(), "signal enrichment on local edge")
	assert.Equal(t, int32(-65), edge0.GetSignalAverage())

	edge1 := node0.GetNeighbors()[1]
	assert.Equal(t, "9c:ef:d5:f9:80:4d", edge1.GetRouterMac())
	assert.Equal(t, "BCM2711-97d6_phy2-mesh0", edge1.GetRouterHostname())
	assert.Equal(t, "00:0a:52:0b:7d:ae", edge1.GetNeighborMac())
	assert.Equal(t, "BCM2711-1003_phy1-mesh0", edge1.GetNeighborHostname())
	assert.Zero(t, edge1.GetSignal(), "no station match — signal must be zero")
	assert.Zero(t, edge1.GetSignalAverage())

	client0 := node0.GetClients()[0]
	assert.Equal(t, "3c:22:7f:37:4c:0c", client0.GetMac())
	assert.Equal(t, "BCM2711-97d6_wlan0", client0.GetHostname())

	// Second node has no "secondary" entry in the jsondoc — expect empty.
	node1 := topo.GetNodes()[1]
	assert.Equal(t, "2c:cf:67:b8:88:ba", node1.GetPrimaryMac())
	assert.Empty(t, node1.GetSecondaryMacs())
}

func TestGetMeshTopology_SignalNotLeakedAcrossNodes(t *testing.T) {
	// The second fixture entry has router=9c:ef:d5:f9:9e:02 (not one of OUR
	// interfaces) and neighbor=9c:ef:d5:f9:80:4d (which IS in our station
	// map because it matches the local mesh iface MAC we stub below). Signal
	// must NOT be populated for that foreign-originated edge, even though
	// the neighbor MAC is in our lookup.
	vis := &fakeVisibilityProvider{doc: sampleVisDoc()}

	ourIface := makeInterfaceWithMAC("mesh0", "9c:ef:d5:f9:80:4d")
	// A station reading for our local peer 9c:ef:d5:f9:9e:02.
	station := makeStation("9c:ef:d5:f9:9e:02", -55)

	fw := &fakeWireless{
		meshInterfaces: []*wifi.Interface{ourIface},
		stationInfo:    []*wifi.StationInfo{station},
	}

	svc := newMeshTopologyService(vis, fw,
		func(_ string) (*batmanadv.BatHosts, error) {
			return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
		},
		nil,
	)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	// Second node's only edge has router_mac = 9c:ef:d5:f9:9e:02 (remote);
	// its signal must be zero even though neighbor_mac matches a station.
	node1 := resp.GetTopology().GetNodes()[1]
	require.Len(t, node1.GetNeighbors(), 1)
	assert.Zero(t, node1.GetNeighbors()[0].GetSignal(),
		"edges originating from a foreign router must not carry our signal")
	assert.Zero(t, node1.GetNeighbors()[0].GetSignalAverage())
}

func TestGetMeshTopology_AlfredUnavailable(t *testing.T) {
	wrapped := fmt.Errorf("simulated exit: %w", batmanadv.ErrVisUnavailable)
	vis := &fakeVisibilityProvider{err: wrapped}

	svc := newMeshTopologyService(vis, &fakeWireless{},
		func(_ string) (*batmanadv.BatHosts, error) {
			return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
		},
		nil,
	)

	_, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.Error(t, err)

	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeFailedPrecondition, ce.Code())
}

func TestGetMeshTopology_UnmarshalFailure(t *testing.T) {
	vis := &fakeVisibilityProvider{err: errors.New("corrupt json")}

	svc := newMeshTopologyService(vis, &fakeWireless{},
		func(_ string) (*batmanadv.BatHosts, error) {
			return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
		},
		nil,
	)

	_, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.Error(t, err)

	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeInternal, ce.Code())
}

func TestGetMeshTopology_BatHostsMissing(t *testing.T) {
	vis := &fakeVisibilityProvider{doc: sampleVisDoc()}

	svc := newMeshTopologyService(vis, &fakeWireless{},
		func(_ string) (*batmanadv.BatHosts, error) {
			return nil, errors.New("bat-hosts unavailable")
		},
		nil,
	)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err, "bat-hosts failure should degrade gracefully, not fail the RPC")

	node0 := resp.GetTopology().GetNodes()[0]
	assert.Empty(t, node0.GetPrimaryHostname())
	assert.Empty(t, node0.GetNeighbors()[0].GetRouterHostname())
	assert.Empty(t, node0.GetNeighbors()[0].GetNeighborHostname())
	assert.Empty(t, node0.GetClients()[0].GetHostname())
}

func TestGetMeshTopology_WifiUnavailable(t *testing.T) {
	vis := &fakeVisibilityProvider{doc: sampleVisDoc()}

	fw := &fakeWireless{meshInterfacesErr: errors.New("netlink failure")}

	svc := newMeshTopologyService(vis, fw,
		func(_ string) (*batmanadv.BatHosts, error) {
			return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
		},
		nil,
	)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err, "wifi failure should degrade gracefully, not fail the RPC")

	for _, node := range resp.GetTopology().GetNodes() {
		for _, edge := range node.GetNeighbors() {
			assert.Zero(t, edge.GetSignal())
			assert.Zero(t, edge.GetSignalAverage())
		}
	}
}

func TestGetMeshTopology_UnknownMAC(t *testing.T) {
	doc := &batmanadv.VisDoc{
		SourceVersion: "x",
		Algorithm:     4,
		Vis: []batmanadv.VisEntry{
			{
				Primary: "ff:ff:ff:ff:ff:ff",
				Neighbors: []batmanadv.VisNeighbor{
					{Router: "ff:ff:ff:ff:ff:ff", Neighbor: "ee:ee:ee:ee:ee:ee", Metric: "1.500"},
				},
				Clients: []string{"dd:dd:dd:dd:dd:dd"},
			},
		},
	}
	vis := &fakeVisibilityProvider{doc: doc}

	svc := newMeshTopologyService(vis, &fakeWireless{},
		func(_ string) (*batmanadv.BatHosts, error) {
			return batmanadv.ParseBatHostsFile(fixtureBatHostsPath())
		},
		nil,
	)

	resp, err := svc.GetMeshTopology(context.Background(), &emptypb.Empty{})
	require.NoError(t, err)

	node := resp.GetTopology().GetNodes()[0]
	assert.Equal(t, "ff:ff:ff:ff:ff:ff", node.GetPrimaryMac())
	assert.Empty(t, node.GetPrimaryHostname())
	assert.Empty(t, node.GetNeighbors()[0].GetRouterHostname())
	assert.Empty(t, node.GetNeighbors()[0].GetNeighborHostname())
	assert.Empty(t, node.GetClients()[0].GetHostname())
}
