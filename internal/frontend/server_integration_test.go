//go:build integration

package frontend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	networkv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	servicev1 "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/util/logger"
	ws "github.com/openmanet/openmanetd/internal/websocket"
	"google.golang.org/protobuf/types/known/emptypb"
)

// integrationStatusHandler returns canned status data.
type integrationStatusHandler struct {
	servicev1connect.UnimplementedStatusServiceHandler
}

func (h *integrationStatusHandler) GetServiceStatus(_ context.Context, _ *emptypb.Empty) (*servicev1.GetServiceStatusResponse, error) {
	return &servicev1.GetServiceStatusResponse{
		Status: &servicev1.ServiceStatus{
			IsConnected:          true,
			ConnectedNeighbors:   2,
			ActiveMeshInterfaces: 1,
			IsMeshGateway:        false,
		},
	}, nil
}

// integrationNodeHandler returns canned node data.
type integrationNodeHandler struct {
	servicev1connect.UnimplementedNodeServiceHandler
}

func (h *integrationNodeHandler) ListNodes(_ context.Context, _ *emptypb.Empty) (*servicev1.ListNodesResponse, error) {
	return &servicev1.ListNodesResponse{
		Nodes: []*networkv1.Node{
			{Hostname: "alpha", Ipaddr: "10.0.0.1"},
			{Hostname: "bravo", Ipaddr: "10.0.0.2"},
		},
	}, nil
}

// integrationMeshHandler returns canned neighbor data.
type integrationMeshHandler struct {
	servicev1connect.UnimplementedMeshNeighborServiceHandler
}

func (h *integrationMeshHandler) ListMeshNeighbors(_ context.Context, _ *emptypb.Empty) (*servicev1.ListMeshNeighborsResponse, error) {
	return &servicev1.ListMeshNeighborsResponse{
		Neighbors: []*servicev1.MeshNeighbor{
			{Neighbor: "alpha", HardwareAddress: "aa:bb:cc:00:00:01", Signal: -42, Throughput: 150},
		},
	}, nil
}

// integrationInterfaceHandler returns canned interface data.
type integrationInterfaceHandler struct {
	servicev1connect.UnimplementedInterfaceServiceHandler
}

func (h *integrationInterfaceHandler) ListWirelessInterfaces(_ context.Context, _ *emptypb.Empty) (*servicev1.ListWirelessInterfacesResponse, error) {
	return &servicev1.ListWirelessInterfacesResponse{
		Interfaces: []*servicev1.WirelessInterface{
			{Name: "wlan0", Frequency: 5180, ChannelWidth: 80, InterfaceType: "mesh"},
		},
	}, nil
}

// TestIntegration_APIEndpoints tests the full flow: HTTP request → server handler → openmanetd client → mock RPC server.
func TestIntegration_APIEndpoints(t *testing.T) {
	// Stand up a mock Connect RPC server with all required services.
	rpcMux := http.NewServeMux()

	statusPath, statusH := servicev1connect.NewStatusServiceHandler(&integrationStatusHandler{})
	rpcMux.Handle(statusPath, statusH)

	nodePath, nodeH := servicev1connect.NewNodeServiceHandler(&integrationNodeHandler{})
	rpcMux.Handle(nodePath, nodeH)

	meshPath, meshH := servicev1connect.NewMeshNeighborServiceHandler(&integrationMeshHandler{})
	rpcMux.Handle(meshPath, meshH)

	ifacePath, ifaceH := servicev1connect.NewInterfaceServiceHandler(&integrationInterfaceHandler{})
	rpcMux.Handle(ifacePath, ifaceH)

	rpcServer := httptest.NewServer(rpcMux)
	defer rpcServer.Close()

	// Create ConnectRPC clients pointing at the mock RPC server.
	rpcHTTPClient := rpcServer.Client()
	statusClient := servicev1connect.NewStatusServiceClient(rpcHTTPClient, rpcServer.URL)
	nodeClient := servicev1connect.NewNodeServiceClient(rpcHTTPClient, rpcServer.URL)
	meshClient := servicev1connect.NewMeshNeighborServiceClient(rpcHTTPClient, rpcServer.URL)
	ifaceClient := servicev1connect.NewInterfaceServiceClient(rpcHTTPClient, rpcServer.URL)

	// Create the frontend server with the real clients.
	hub := ws.NewHub(nil)
	go hub.Run()

	cfg := config.NewWithoutWatch(nil)
	staticFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html></html>")},
	}

	srv := &Server{
		log:             logger.GetLogger("frontend.test"),
		staticFS:        staticFS,
		hub:             hub,
		cfg:             cfg,
		statusClient:    statusClient,
		nodeClient:      nodeClient,
		meshClient:      meshClient,
		interfaceClient: ifaceClient,
	}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	t.Run("status", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/status")
		if err != nil {
			t.Fatalf("GET /api/status error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var result apiStatus
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("json decode: %v", err)
		}

		if !result.Connected {
			t.Error("connected = false, want true")
		}
		if result.Neighbors != 2 {
			t.Errorf("neighbors = %d, want 2", result.Neighbors)
		}
		if result.MeshInterfaces != 1 {
			t.Errorf("mesh_interfaces = %d, want 1", result.MeshInterfaces)
		}
		if result.IsGateway {
			t.Error("is_gateway = true, want false")
		}
	})

	t.Run("nodes", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/nodes")
		if err != nil {
			t.Fatalf("GET /api/nodes error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var result []apiNode
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("json decode: %v", err)
		}

		if len(result) != 2 {
			t.Fatalf("nodes len = %d, want 2", len(result))
		}
		if result[0].Hostname != "alpha" {
			t.Errorf("nodes[0].hostname = %q, want %q", result[0].Hostname, "alpha")
		}
		if result[1].IP != "10.0.0.2" {
			t.Errorf("nodes[1].ip = %q, want %q", result[1].IP, "10.0.0.2")
		}
	})

	t.Run("neighbors", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/neighbors")
		if err != nil {
			t.Fatalf("GET /api/neighbors error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var result []apiNeighbor
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("json decode: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("neighbors len = %d, want 1", len(result))
		}
		if result[0].Name != "alpha" {
			t.Errorf("neighbors[0].name = %q, want %q", result[0].Name, "alpha")
		}
		if result[0].Signal != -42 {
			t.Errorf("neighbors[0].signal = %d, want -42", result[0].Signal)
		}
		if result[0].Throughput != 150 {
			t.Errorf("neighbors[0].throughput = %d, want 150", result[0].Throughput)
		}
	})

	t.Run("interfaces", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/interfaces")
		if err != nil {
			t.Fatalf("GET /api/interfaces error: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}

		var result []apiInterface
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("json decode: %v", err)
		}

		if len(result) != 1 {
			t.Fatalf("interfaces len = %d, want 1", len(result))
		}
		if result[0].Name != "wlan0" {
			t.Errorf("interfaces[0].name = %q, want %q", result[0].Name, "wlan0")
		}
		if result[0].Frequency != 5180 {
			t.Errorf("interfaces[0].frequency = %d, want 5180", result[0].Frequency)
		}
		if result[0].ChannelWidth != 80 {
			t.Errorf("interfaces[0].channel_width = %d, want 80", result[0].ChannelWidth)
		}
		if result[0].Type != "mesh" {
			t.Errorf("interfaces[0].type = %q, want %q", result[0].Type, "mesh")
		}
	})
}
