package frontend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"connectrpc.com/connect"
	blosv1 "github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/blos/v1/blosv1connect"
	commsv1 "github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/comms/v1/commsv1connect"
	dashboardv1 "github.com/openmanet/openmanetd/internal/api/openmanet/dashboard/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/dashboard/v1/dashboardv1connect"
	networkv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	network_interfacev1 "github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/network_interface/v1/network_interfacev1connect"
	servicev1 "github.com/openmanet/openmanetd/internal/api/openmanet/service/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/service/v1/servicev1connect"
	wifi_configv1 "github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1"
	"github.com/openmanet/openmanetd/internal/api/openmanet/wifi_config/v1/wifi_configv1connect"
	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/util/logger"
	ws "github.com/openmanet/openmanetd/internal/websocket"
	"google.golang.org/protobuf/types/known/emptypb"
)

// ---------------------------------------------------------------------------
// Mock ConnectRPC clients
// ---------------------------------------------------------------------------

type mockStatusClient struct {
	resp *servicev1.GetServiceStatusResponse
	err  error
}

func (m *mockStatusClient) GetServiceStatus(_ context.Context, _ *emptypb.Empty) (*servicev1.GetServiceStatusResponse, error) {
	return m.resp, m.err
}

type mockNodeClient struct {
	getResp  *servicev1.GetNodeResponse
	listResp *servicev1.ListNodesResponse
	err      error
}

func (m *mockNodeClient) GetNode(_ context.Context, _ *servicev1.GetNodeRequest) (*servicev1.GetNodeResponse, error) {
	return m.getResp, m.err
}

func (m *mockNodeClient) ListNodes(_ context.Context, _ *emptypb.Empty) (*servicev1.ListNodesResponse, error) {
	return m.listResp, m.err
}

type mockMeshClient struct {
	resp *servicev1.ListMeshNeighborsResponse
	err  error
}

func (m *mockMeshClient) ListMeshNeighbors(_ context.Context, _ *emptypb.Empty) (*servicev1.ListMeshNeighborsResponse, error) {
	return m.resp, m.err
}

type mockInterfaceClient struct {
	getResp  *servicev1.GetWirelessInterfaceResponse
	listResp *servicev1.ListWirelessInterfacesResponse
	err      error
}

func (m *mockInterfaceClient) GetWirelessInterface(_ context.Context, _ *servicev1.GetWirelessInterfaceRequest) (*servicev1.GetWirelessInterfaceResponse, error) {
	return m.getResp, m.err
}

func (m *mockInterfaceClient) ListWirelessInterfaces(_ context.Context, _ *emptypb.Empty) (*servicev1.ListWirelessInterfacesResponse, error) {
	return m.listResp, m.err
}

type mockCommsClient struct {
	commsv1connect.CommsServiceClient
}

type mockDashboardClient struct {
	dashboardv1connect.DashboardServiceClient
}

type mockNetworkInterfaceClient struct {
	network_interfacev1connect.NetworkInterfaceServiceClient
}

type mockWifiConfigClient struct {
	wifi_configv1connect.WifiConfigServiceClient
}

type mockBLOSClient struct {
	blosv1connect.BLOSServiceClient
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestServer(opts ...func(*Server)) *Server {
	hub := ws.NewHub(nil)
	go hub.Run()

	cfg := config.NewWithoutWatch(nil)
	staticFS := fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("<html><body>SPA</body></html>")},
		"assets/app.js":    &fstest.MapFile{Data: []byte("console.log('app')")},
		"assets/style.css": &fstest.MapFile{Data: []byte("body{}")},
		"pcm-worklet.js":   &fstest.MapFile{Data: []byte("// worklet")},
	}

	s := &Server{
		log:             logger.GetLogger("frontend.test"),
		staticFS:        staticFS,
		hub:             hub,
		cfg:             cfg,
		statusClient:    &mockStatusClient{},
		nodeClient:      &mockNodeClient{},
		meshClient:      &mockMeshClient{},
		interfaceClient: &mockInterfaceClient{},
		commsClient:     &mockCommsClient{},
		dashboardClient: &mockDashboardClient{},
		networkClient:   &mockNetworkInterfaceClient{},
		wifiClient:      &mockWifiConfigClient{},
		blosClient:      &mockBLOSClient{},
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

func decodeJSON(t *testing.T, body io.Reader, v any) {
	t.Helper()

	if err := json.NewDecoder(body).Decode(v); err != nil {
		t.Fatalf("json decode: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Handler unit tests
// ---------------------------------------------------------------------------

func TestHandleAPIStatus(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.statusClient = &mockStatusClient{
			resp: &servicev1.GetServiceStatusResponse{
				Status: &servicev1.ServiceStatus{
					IsConnected:          true,
					ConnectedNeighbors:   3,
					ActiveMeshInterfaces: 2,
					IsMeshGateway:        true,
				},
			},
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	srv.handleAPIStatus(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result apiStatus
	decodeJSON(t, w.Body, &result)

	if !result.Connected {
		t.Error("connected = false, want true")
	}

	if result.Neighbors != 3 {
		t.Errorf("neighbors = %d, want 3", result.Neighbors)
	}

	if result.MeshInterfaces != 2 {
		t.Errorf("mesh_interfaces = %d, want 2", result.MeshInterfaces)
	}

	if !result.IsGateway {
		t.Error("is_gateway = false, want true")
	}
}

func TestHandleAPIStatus_Error(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.statusClient = &mockStatusClient{
			err: connect.NewError(connect.CodeUnavailable, errors.New("service down")),
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	srv.handleAPIStatus(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}

	var result errorResponse
	decodeJSON(t, w.Body, &result)

	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHandleAPINodes(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.nodeClient = &mockNodeClient{
			listResp: &servicev1.ListNodesResponse{
				Nodes: []*networkv1.Node{
					{Hostname: "alpha", Ipaddr: "10.0.0.1"},
					{Hostname: "bravo", Ipaddr: "10.0.0.2"},
					{Hostname: "charlie", Ipaddr: "10.0.0.3"},
				},
			},
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	srv.handleAPINodes(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []apiNode
	decodeJSON(t, w.Body, &result)

	if len(result) != 3 {
		t.Fatalf("nodes len = %d, want 3", len(result))
	}

	if result[0].Hostname != "alpha" {
		t.Errorf("nodes[0].hostname = %q, want %q", result[0].Hostname, "alpha")
	}

	if result[2].IP != "10.0.0.3" {
		t.Errorf("nodes[2].ip = %q, want %q", result[2].IP, "10.0.0.3")
	}
}

func TestHandleAPINodes_Error(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.nodeClient = &mockNodeClient{
			err: connect.NewError(connect.CodeInternal, errors.New("db error")),
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	srv.handleAPINodes(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestHandleAPINodes_Empty(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.nodeClient = &mockNodeClient{
			listResp: &servicev1.ListNodesResponse{},
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	srv.handleAPINodes(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []apiNode
	decodeJSON(t, w.Body, &result)

	if len(result) != 0 {
		t.Fatalf("nodes len = %d, want 0", len(result))
	}
}

func TestHandleAPINeighbors(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.meshClient = &mockMeshClient{
			resp: &servicev1.ListMeshNeighborsResponse{
				Neighbors: []*servicev1.MeshNeighbor{
					{Neighbor: "alpha", HardwareAddress: "aa:bb:cc:00:00:01", Signal: -42, Throughput: 150},
					{Neighbor: "bravo", HardwareAddress: "aa:bb:cc:00:00:02", Signal: -55, Throughput: 72},
				},
			},
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/neighbors", nil)
	srv.handleAPINeighbors(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []apiNeighbor
	decodeJSON(t, w.Body, &result)

	if len(result) != 2 {
		t.Fatalf("neighbors len = %d, want 2", len(result))
	}

	if result[0].Name != "alpha" {
		t.Errorf("neighbors[0].name = %q, want %q", result[0].Name, "alpha")
	}

	if result[0].Signal != -42 {
		t.Errorf("neighbors[0].signal = %d, want -42", result[0].Signal)
	}

	if result[1].Throughput != 72 {
		t.Errorf("neighbors[1].throughput = %d, want 72", result[1].Throughput)
	}
}

func TestHandleAPINeighbors_Error(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.meshClient = &mockMeshClient{
			err: connect.NewError(connect.CodeUnavailable, errors.New("mesh unavailable")),
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/neighbors", nil)
	srv.handleAPINeighbors(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestHandleAPIInterfaces(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.interfaceClient = &mockInterfaceClient{
			listResp: &servicev1.ListWirelessInterfacesResponse{
				Interfaces: []*servicev1.WirelessInterface{
					{Name: "wlan0", Frequency: 5180, ChannelWidth: 80, InterfaceType: "mesh"},
					{Name: "wlan1", Frequency: 2437, ChannelWidth: 20, InterfaceType: "ap"},
				},
			},
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/interfaces", nil)
	srv.handleAPIInterfaces(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result []apiInterface
	decodeJSON(t, w.Body, &result)

	if len(result) != 2 {
		t.Fatalf("interfaces len = %d, want 2", len(result))
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

	if result[1].Type != "ap" {
		t.Errorf("interfaces[1].type = %q, want %q", result[1].Type, "ap")
	}
}

func TestHandleAPIInterfaces_Error(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.interfaceClient = &mockInterfaceClient{
			err: connect.NewError(connect.CodeInternal, errors.New("internal")),
		}
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/interfaces", nil)
	srv.handleAPIInterfaces(w, r)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Helper method tests
// ---------------------------------------------------------------------------

func TestWriteJSON(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	srv.writeJSON(w, map[string]string{"key": "value"})

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var result map[string]string
	decodeJSON(t, w.Body, &result)

	if result["key"] != "value" {
		t.Errorf("key = %q, want %q", result["key"], "value")
	}
}

func TestWriteError(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	srv.writeError(w, "something failed")

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var result errorResponse
	decodeJSON(t, w.Body, &result)

	if result.Error != "something failed" {
		t.Errorf("error = %q, want %q", result.Error, "something failed")
	}
}

func TestWriteErrorStatus(t *testing.T) {
	srv := newTestServer()

	w := httptest.NewRecorder()
	srv.writeErrorStatus(w, http.StatusNotFound, "not found")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	var result errorResponse
	decodeJSON(t, w.Body, &result)

	if result.Error != "not found" {
		t.Errorf("error = %q, want %q", result.Error, "not found")
	}
}

// ---------------------------------------------------------------------------
// Middleware tests
// ---------------------------------------------------------------------------

func TestCoiMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := coiMiddleware(inner)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)

	if v := w.Header().Get("Cross-Origin-Opener-Policy"); v != "same-origin" {
		t.Errorf("COOP = %q, want same-origin", v)
	}

	if v := w.Header().Get("Cross-Origin-Embedder-Policy"); v != "require-corp" {
		t.Errorf("COEP = %q, want require-corp", v)
	}
}

// ---------------------------------------------------------------------------
// SPA / static file server tests
// ---------------------------------------------------------------------------

func TestSPAHandler_ServesIndexAtRoot(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !containsStr(string(body), "SPA") {
		t.Errorf("body does not contain SPA: %s", body)
	}
}

func TestSPAHandler_ServesStaticAsset(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("GET /assets/app.js error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !containsStr(string(body), "console.log") {
		t.Errorf("body does not contain expected JS: %s", body)
	}
}

func TestSPAHandler_FallsBackToIndex(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	// A client-side route that doesn't map to a static file should return index.html.
	resp, err := http.Get(ts.URL + "/settings")
	if err != nil {
		t.Fatalf("GET /settings error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !containsStr(string(body), "SPA") {
		t.Errorf("expected SPA fallback, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Route registration test
// ---------------------------------------------------------------------------

func TestHandler_RegistersExpectedRoutes(t *testing.T) {
	srv := newTestServer()

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	routes := []struct {
		path string
		code int
	}{
		{"/api/status", http.StatusBadGateway},    // mock returns zero-value error
		{"/api/nodes", http.StatusBadGateway},     // mock returns zero-value error
		{"/api/neighbors", http.StatusBadGateway}, // mock returns zero-value error
		{"/api/interfaces", http.StatusBadGateway},
		{"/", http.StatusOK}, // SPA index
	}

	for _, tc := range routes {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tc.path)
			if err != nil {
				t.Fatalf("GET %s error: %v", tc.path, err)
			}

			resp.Body.Close()
			// Just verify the route is registered (we get a response, not 404).
			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("route %s returned 404, expected it to be registered", tc.path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Concurrency test
// ---------------------------------------------------------------------------

func TestHandleAPIStatus_Concurrent(t *testing.T) {
	srv := newTestServer(func(s *Server) {
		s.statusClient = &mockStatusClient{
			resp: &servicev1.GetServiceStatusResponse{
				Status: &servicev1.ServiceStatus{
					IsConnected:        true,
					ConnectedNeighbors: 5,
				},
			},
		}
	})

	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	const goroutines = 50

	errCh := make(chan error, goroutines)

	for range goroutines {
		go func() {
			resp, err := http.Get(ts.URL + "/api/status")
			if err != nil {
				errCh <- err

				return
			}

			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errCh <- errors.New("unexpected status code")

				return
			}

			errCh <- nil
		}()
	}

	for range goroutines {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent request error: %v", err)
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// Compile-time interface satisfaction checks — these ensure the mocks
// implement the same interfaces the real ConnectRPC clients do.
var (
	_ servicev1connect.StatusServiceClient                     = (*mockStatusClient)(nil)
	_ servicev1connect.NodeServiceClient                       = (*mockNodeClient)(nil)
	_ servicev1connect.MeshNeighborServiceClient               = (*mockMeshClient)(nil)
	_ servicev1connect.InterfaceServiceClient                  = (*mockInterfaceClient)(nil)
	_ commsv1connect.CommsServiceClient                        = (*mockCommsClient)(nil)
	_ dashboardv1connect.DashboardServiceClient                = (*mockDashboardClient)(nil)
	_ network_interfacev1connect.NetworkInterfaceServiceClient = (*mockNetworkInterfaceClient)(nil)
	_ wifi_configv1connect.WifiConfigServiceClient             = (*mockWifiConfigClient)(nil)
	_ blosv1connect.BLOSServiceClient                          = (*mockBLOSClient)(nil)
)

// Ensure proto types referenced in mocks are valid imports.
var (
	_ *blosv1.GetBLOSStatusResponse                      = nil
	_ *commsv1.GetCommsStatusResponse                    = nil
	_ *dashboardv1.GetDashboardStatusResponse            = nil
	_ *network_interfacev1.ListNetworkInterfacesResponse = nil
	_ *wifi_configv1.ListRadiosResponse                  = nil
)
