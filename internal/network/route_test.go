package network

import (
	"errors"
	"net"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// Helper functions to create test data

func createTestIPNet(cidr string) *net.IPNet {
	_, ipNet, _ := net.ParseCIDR(cidr)
	return ipNet
}

func createTestRoute() *Route {
	return &Route{
		Destination: createTestIPNet("192.168.1.0/24"),
		Gateway:     net.ParseIP("10.0.0.1"),
		Interface:   "eth0",
		Metric:      100,
		Table:       unix.RT_TABLE_MAIN,
		Scope:       netlink.SCOPE_UNIVERSE,
		Protocol:    netlink.RouteProtocol(unix.RTPROT_BOOT),
	}
}

func createTestDefaultRoute() *Route {
	return &Route{
		Destination: nil,
		Gateway:     net.ParseIP("192.168.1.1"),
		Interface:   "eth0",
		Metric:      0,
		Table:       unix.RT_TABLE_MAIN,
		Scope:       netlink.SCOPE_UNIVERSE,
		Protocol:    netlink.RouteProtocol(unix.RTPROT_BOOT),
	}
}

func TestRoute_String(t *testing.T) {
	tests := []struct {
		name  string
		route *Route
		want  string
	}{
		{
			name:  "nil route",
			route: nil,
			want:  "<nil>",
		},
		{
			name: "default route",
			route: &Route{
				Destination: nil,
				Gateway:     net.ParseIP("192.168.1.1"),
				Interface:   "eth0",
				Metric:      100,
				Table:       254,
			},
			want: "default via 192.168.1.1 dev eth0 metric 100 table 254",
		},
		{
			name: "network route",
			route: &Route{
				Destination: createTestIPNet("10.0.0.0/8"),
				Gateway:     net.ParseIP("192.168.1.1"),
				Interface:   "wlan0",
				Metric:      50,
				Table:       255,
			},
			want: "10.0.0.0/8 via 192.168.1.1 dev wlan0 metric 50 table 255",
		},
		{
			name: "route without gateway",
			route: &Route{
				Destination: createTestIPNet("172.16.0.0/16"),
				Gateway:     nil,
				Interface:   "bat0",
				Metric:      10,
				Table:       100,
			},
			want: "172.16.0.0/16 via none dev bat0 metric 10 table 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.route.String()
			if got != tt.want {
				t.Errorf("Route.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoutesMatch(t *testing.T) {
	tests := []struct {
		name string
		r1   *Route
		r2   *Route
		want bool
	}{
		{
			name: "identical routes",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			want: true,
		},
		{
			name: "different destinations",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.2.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			want: false,
		},
		{
			name: "different gateways",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.2"),
				Interface:   "eth0",
				Metric:      100,
			},
			want: false,
		},
		{
			name: "different interfaces",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth1",
				Metric:      100,
			},
			want: false,
		},
		{
			name: "different metrics",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      200,
			},
			want: false,
		},
		{
			name: "nil destinations match",
			r1: &Route{
				Destination: nil,
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: nil,
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			want: true,
		},
		{
			name: "one nil destination",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: nil,
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			want: false,
		},
		{
			name: "nil gateways match",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     nil,
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     nil,
				Interface:   "eth0",
				Metric:      100,
			},
			want: true,
		},
		{
			name: "one nil gateway",
			r1: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     net.ParseIP("10.0.0.1"),
				Interface:   "eth0",
				Metric:      100,
			},
			r2: &Route{
				Destination: createTestIPNet("192.168.1.0/24"),
				Gateway:     nil,
				Interface:   "eth0",
				Metric:      100,
			},
			want: false,
		},
		{
			name: "nil route 1",
			r1:   nil,
			r2:   createTestRoute(),
			want: false,
		},
		{
			name: "nil route 2",
			r1:   createTestRoute(),
			r2:   nil,
			want: false,
		},
		{
			name: "both nil routes",
			r1:   nil,
			r2:   nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := routesMatch(tt.r1, tt.r2)
			if got != tt.want {
				t.Errorf("routesMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddRoute_NilRoute(t *testing.T) {
	err := AddRoute(nil)
	if err == nil {
		t.Error("AddRoute(nil) expected error, got nil")
	}
	if err.Error() != "route cannot be nil" {
		t.Errorf("AddRoute(nil) error = %v, want 'route cannot be nil'", err)
	}
}

func TestDeleteRoute_NilRoute(t *testing.T) {
	err := DeleteRoute(nil)
	if err == nil {
		t.Error("DeleteRoute(nil) expected error, got nil")
	}
	if err.Error() != "route cannot be nil" {
		t.Errorf("DeleteRoute(nil) error = %v, want 'route cannot be nil'", err)
	}
}

func TestReplaceRoute_NilRoute(t *testing.T) {
	err := ReplaceRoute(nil)
	if err == nil {
		t.Error("ReplaceRoute(nil) expected error, got nil")
	}
	if err.Error() != "route cannot be nil" {
		t.Errorf("ReplaceRoute(nil) error = %v, want 'route cannot be nil'", err)
	}
}

func TestRouteExists_NilRoute(t *testing.T) {
	exists, err := RouteExists(nil)
	if err == nil {
		t.Error("RouteExists(nil) expected error, got nil")
	}
	if exists {
		t.Error("RouteExists(nil) should return false")
	}
}

func TestAddRoute_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	route := &Route{
		Destination: createTestIPNet("192.168.1.0/24"),
		Gateway:     net.ParseIP("10.0.0.1"),
		Interface:   "nonexistent999",
		Metric:      100,
		Table:       unix.RT_TABLE_MAIN,
	}

	err := AddRoute(route)
	if err == nil {
		t.Error("AddRoute() with invalid interface expected error, got nil")
	}
}

func TestDeleteRoute_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	route := &Route{
		Destination: createTestIPNet("192.168.1.0/24"),
		Gateway:     net.ParseIP("10.0.0.1"),
		Interface:   "nonexistent999",
		Metric:      100,
		Table:       unix.RT_TABLE_MAIN,
	}

	err := DeleteRoute(route)
	if err == nil {
		t.Error("DeleteRoute() with invalid interface expected error, got nil")
	}
}

func TestReplaceRoute_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	route := &Route{
		Destination: createTestIPNet("192.168.1.0/24"),
		Gateway:     net.ParseIP("10.0.0.1"),
		Interface:   "nonexistent999",
		Metric:      100,
		Table:       unix.RT_TABLE_MAIN,
	}

	err := ReplaceRoute(route)
	if err == nil {
		t.Error("ReplaceRoute() with invalid interface expected error, got nil")
	}
}

func TestAddDefaultRoute_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	err := AddDefaultRoute(net.ParseIP("192.168.1.1"), "nonexistent999", 100)
	if err == nil {
		t.Error("AddDefaultRoute() with invalid interface expected error, got nil")
	}
}

func TestDeleteDefaultRoute_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	err := DeleteDefaultRoute(net.ParseIP("192.168.1.1"), "nonexistent999")
	if err == nil {
		t.Error("DeleteDefaultRoute() with invalid interface expected error, got nil")
	}
}

func TestFlushRoutes_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	err := FlushRoutes("nonexistent999")
	if err == nil {
		t.Error("FlushRoutes() with invalid interface expected error, got nil")
	}
}

func TestAddHostRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// Test with invalid interface to verify error handling
	err := AddHostRoute(net.ParseIP("192.168.1.100"), net.ParseIP("192.168.1.1"), "nonexistent999", 100)
	if err == nil {
		t.Error("AddHostRoute() with invalid interface expected error, got nil")
	}
}

func TestAddNetworkRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// Test with invalid interface to verify error handling
	network := createTestIPNet("10.0.0.0/8")
	err := AddNetworkRoute(network, net.ParseIP("192.168.1.1"), "nonexistent999", 100)
	if err == nil {
		t.Error("AddNetworkRoute() with invalid interface expected error, got nil")
	}
}

func TestDeleteNetworkRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// Test with invalid interface to verify error handling
	network := createTestIPNet("10.0.0.0/8")
	err := DeleteNetworkRoute(network, net.ParseIP("192.168.1.1"), "nonexistent999")
	if err == nil {
		t.Error("DeleteNetworkRoute() with invalid interface expected error, got nil")
	}
}

func TestGetRoutesForInterface_InvalidInterface(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	_, err := GetRoutesForInterface("nonexistent999")
	if err == nil {
		t.Error("GetRoutesForInterface() with invalid interface expected error, got nil")
	}
}

func TestGetRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// This test will only log results as we can't mock netlink easily
	routes, err := GetRoutes(unix.RT_TABLE_MAIN)
	if err != nil {
		t.Logf("GetRoutes() error (may be expected in test environment): %v", err)
	} else {
		t.Logf("GetRoutes() returned %d routes", len(routes))
	}
}

func TestGetAllRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// This test will only log results as we can't mock netlink easily
	routes, err := GetAllRoutes()
	if err != nil {
		t.Logf("GetAllRoutes() error (may be expected in test environment): %v", err)
	} else {
		t.Logf("GetAllRoutes() returned %d routes", len(routes))
		for _, route := range routes {
			t.Logf("  Route: %s", route.String())
		}
	}
}

func TestGetDefaultRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// This test will only log results as we can't mock netlink easily
	route, err := GetDefaultRoute()
	if err != nil {
		// Check if it's the specific error we expect
		if errors.Is(err, ErrNoDefaultRouteFound) {
			t.Logf("GetDefaultRoute() returned ErrNoDefaultRouteFound (expected in test environment without default route)")
		} else {
			t.Logf("GetDefaultRoute() error: %v", err)
		}
	} else {
		t.Logf("GetDefaultRoute() returned: %s", route.String())

		// Validate the returned route
		if route.Destination != nil {
			t.Error("Default route should have nil destination")
		}
		if route.Gateway == nil {
			t.Error("Default route should have a gateway")
		}
		if route.Table != unix.RT_TABLE_MAIN {
			t.Errorf("Default route should be from main routing table, got table %d", route.Table)
		}
		if route.Interface == "" {
			t.Error("Default route should have an interface")
		}
	}
}

func TestFlushRoutesInTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// Use a non-existent table to avoid modifying actual routes
	err := FlushRoutesInTable(999)
	// Should not error even if table is empty
	if err != nil {
		t.Logf("FlushRoutesInTable() error (may be expected): %v", err)
	}
}

func TestReplaceDefaultRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	// This test will verify the function handles the case where no default route exists
	// ReplaceDefaultRoute now attempts to add a route if none exists
	// We can't actually test replacing a real default route without disrupting connectivity
	err := ReplaceDefaultRoute(net.ParseIP("192.168.1.254"), "br-ahwlan")
	if err != nil {
		// Expected to fail in most test environments due to permissions or invalid interface
		t.Logf("ReplaceDefaultRoute() error (expected in test environment): %v", err)
	} else {
		t.Log("ReplaceDefaultRoute() succeeded")
	}
}

func TestGetRouteToDestination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping netlink test in short mode")
	}

	tests := []struct {
		name        string
		destination net.IP
		expectError bool
	}{
		{
			name:        "localhost",
			destination: net.ParseIP("127.0.0.1"),
			expectError: false,
		},
		{
			name:        "google DNS",
			destination: net.ParseIP("8.8.8.8"),
			expectError: false,
		},
		{
			name:        "invalid IP",
			destination: net.ParseIP("invalid"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, err := GetRouteToDestination(tt.destination)
			if tt.expectError {
				if err == nil {
					t.Error("GetRouteToDestination() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Logf("GetRouteToDestination(%s) error (may be expected in test environment): %v", tt.destination, err)
				} else {
					t.Logf("GetRouteToDestination(%s) = %s", tt.destination, route.String())
				}
			}
		})
	}
}

func TestRoute_AllFields(t *testing.T) {
	// Test that all Route fields can be set and retrieved
	route := Route{
		Destination: createTestIPNet("172.16.0.0/12"),
		Gateway:     net.ParseIP("10.0.0.1"),
		Interface:   "bat0",
		Metric:      250,
		Table:       100,
		Scope:       netlink.SCOPE_LINK,
		Protocol:    netlink.RouteProtocol(unix.RTPROT_STATIC),
	}

	if route.Destination.String() != "172.16.0.0/12" {
		t.Errorf("Destination = %v, want 172.16.0.0/12", route.Destination)
	}
	if !route.Gateway.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("Gateway = %v, want 10.0.0.1", route.Gateway)
	}
	if route.Interface != "bat0" {
		t.Errorf("Interface = %v, want bat0", route.Interface)
	}
	if route.Metric != 250 {
		t.Errorf("Metric = %v, want 250", route.Metric)
	}
	if route.Table != 100 {
		t.Errorf("Table = %v, want 100", route.Table)
	}
	if route.Scope != netlink.SCOPE_LINK {
		t.Errorf("Scope = %v, want SCOPE_LINK", route.Scope)
	}
	if route.Protocol != netlink.RouteProtocol(unix.RTPROT_STATIC) {
		t.Errorf("Protocol = %v, want RTPROT_STATIC", route.Protocol)
	}
}

func TestCreateTestIPNet(t *testing.T) {
	tests := []struct {
		cidr string
		want string
	}{
		{"192.168.1.0/24", "192.168.1.0/24"},
		{"10.0.0.0/8", "10.0.0.0/8"},
		{"172.16.0.0/12", "172.16.0.0/12"},
		{"0.0.0.0/0", "0.0.0.0/0"},
	}

	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			ipNet := createTestIPNet(tt.cidr)
			if ipNet == nil {
				t.Fatal("createTestIPNet() returned nil")
			}
			if ipNet.String() != tt.want {
				t.Errorf("createTestIPNet(%s) = %v, want %v", tt.cidr, ipNet.String(), tt.want)
			}
		})
	}
}

func TestCreateTestRoute(t *testing.T) {
	route := createTestRoute()
	if route == nil {
		t.Fatal("createTestRoute() returned nil")
	}
	if route.Destination == nil {
		t.Error("createTestRoute() Destination is nil")
	}
	if route.Gateway == nil {
		t.Error("createTestRoute() Gateway is nil")
	}
	if route.Interface == "" {
		t.Error("createTestRoute() Interface is empty")
	}
}

func TestCreateTestDefaultRoute(t *testing.T) {
	route := createTestDefaultRoute()
	if route == nil {
		t.Fatal("createTestDefaultRoute() returned nil")
	}
	if route.Destination != nil {
		t.Error("createTestDefaultRoute() Destination should be nil")
	}
	if route.Gateway == nil {
		t.Error("createTestDefaultRoute() Gateway is nil")
	}
	if route.Interface == "" {
		t.Error("createTestDefaultRoute() Interface is empty")
	}
}
