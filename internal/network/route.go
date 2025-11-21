package network

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var (
	// ErrNoRouteFound is returned when no route could be found for a given query
	ErrNoRouteFound = errors.New("no route found")
)

// Route represents a routing table entry in the Linux kernel routing table.
// It contains all the necessary information to identify and manipulate a route.
//
// Fields:
//   - Destination: The destination network in CIDR notation. nil represents a default route.
//   - Gateway: The gateway IP address for the route. nil for directly connected networks.
//   - Interface: The name of the network interface to use for this route (e.g., "eth0", "wlan0").
//   - Metric: The route priority/metric. Lower values have higher priority.
//   - Table: The routing table ID (e.g., unix.RT_TABLE_MAIN for the main table).
//   - Scope: The scope of the route (e.g., netlink.SCOPE_UNIVERSE for global routes).
//   - Protocol: The routing protocol that installed this route (e.g., RTPROT_BOOT, RTPROT_STATIC).
type Route struct {
	Destination *net.IPNet
	Gateway     net.IP
	Interface   string
	Metric      int
	Table       int
	Scope       netlink.Scope
	Protocol    netlink.RouteProtocol
}

// AddRoute adds a new route to the kernel routing table.
// It returns an error if the route is nil, the interface doesn't exist,
// or the route cannot be added to the kernel routing table.
//
// Example:
//
//	route := &Route{
//	    Destination: parseIPNet("192.168.1.0/24"),
//	    Gateway:     net.ParseIP("10.0.0.1"),
//	    Interface:   "eth0",
//	    Metric:      100,
//	    Table:       unix.RT_TABLE_MAIN,
//	}
//	err := AddRoute(route)
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func AddRoute(route *Route) error {
	if route == nil {
		return fmt.Errorf("route cannot be nil")
	}

	link, err := netlink.LinkByName(route.Interface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", route.Interface, err)
	}

	nlRoute := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       route.Destination,
		Gw:        route.Gateway,
		Priority:  route.Metric,
		Table:     route.Table,
		Scope:     route.Scope,
		Protocol:  route.Protocol,
	}

	if err := netlink.RouteAdd(nlRoute); err != nil {
		return fmt.Errorf("failed to add route: %w", err)
	}

	return nil
}

// DeleteRoute deletes a route from the kernel routing table.
// It returns an error if the route is nil, the interface doesn't exist,
// or the route cannot be deleted from the kernel routing table.
//
// The route must match an existing route in the kernel routing table.
// All fields of the route (destination, gateway, interface, metric, table) are used
// to identify the route to delete.
//
// Example:
//
//	route := &Route{
//	    Destination: parseIPNet("192.168.1.0/24"),
//	    Gateway:     net.ParseIP("10.0.0.1"),
//	    Interface:   "eth0",
//	    Table:       unix.RT_TABLE_MAIN,
//	}
//	err := DeleteRoute(route)
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func DeleteRoute(route *Route) error {
	if route == nil {
		return fmt.Errorf("route cannot be nil")
	}

	link, err := netlink.LinkByName(route.Interface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", route.Interface, err)
	}

	nlRoute := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       route.Destination,
		Gw:        route.Gateway,
		Priority:  route.Metric,
		Table:     route.Table,
		Scope:     route.Scope,
		Protocol:  route.Protocol,
	}

	if err := netlink.RouteDel(nlRoute); err != nil {
		return fmt.Errorf("failed to delete route: %w", err)
	}

	return nil
}

// ReplaceRoute replaces an existing route or adds it if it doesn't exist.
// This is an atomic operation that either updates an existing matching route
// or creates a new one if no match is found.
//
// It returns an error if the route is nil, the interface doesn't exist,
// or the operation fails.
//
// This is useful when you want to ensure a route exists with specific parameters
// without worrying about whether it already exists or not.
//
// Example:
//
//	route := &Route{
//	    Destination: parseIPNet("192.168.1.0/24"),
//	    Gateway:     net.ParseIP("10.0.0.2"),  // Changed gateway
//	    Interface:   "eth0",
//	    Metric:      100,
//	    Table:       unix.RT_TABLE_MAIN,
//	}
//	err := ReplaceRoute(route)
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func ReplaceRoute(route *Route) error {
	if route == nil {
		return fmt.Errorf("route cannot be nil")
	}

	link, err := netlink.LinkByName(route.Interface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", route.Interface, err)
	}

	nlRoute := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       route.Destination,
		Gw:        route.Gateway,
		Priority:  route.Metric,
		Table:     route.Table,
		Scope:     route.Scope,
		Protocol:  route.Protocol,
	}

	if err := netlink.RouteReplace(nlRoute); err != nil {
		return fmt.Errorf("failed to replace route: %w", err)
	}

	return nil
}

// GetRoutes returns all routes from the specified routing table.
// It queries the kernel for routes in the given table and returns them as a slice
// of Route pointers. Routes for interfaces that cannot be found are silently skipped.
//
// Parameters:
//   - table: The routing table ID to query (e.g., unix.RT_TABLE_MAIN, unix.RT_TABLE_LOCAL)
//
// Returns:
//   - A slice of Route pointers containing all routes in the specified table
//   - An error if the kernel query fails
//
// Example:
//
//	routes, err := GetRoutes(unix.RT_TABLE_MAIN)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for _, route := range routes {
//	    fmt.Println(route.String())
//	}
func GetRoutes(table int) ([]*Route, error) {
	filter := &netlink.Route{
		Table: table,
	}

	nlRoutes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, filter, netlink.RT_FILTER_TABLE)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	routes := make([]*Route, 0, len(nlRoutes))
	for _, nlRoute := range nlRoutes {
		link, err := netlink.LinkByIndex(nlRoute.LinkIndex)
		if err != nil {
			continue // Skip routes for interfaces we can't find
		}

		route := &Route{
			Destination: nlRoute.Dst,
			Gateway:     nlRoute.Gw,
			Interface:   link.Attrs().Name,
			Metric:      nlRoute.Priority,
			Table:       nlRoute.Table,
			Scope:       nlRoute.Scope,
			Protocol:    nlRoute.Protocol,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// GetAllRoutes returns all routes from all routing tables in the system.
// This includes routes from the main table, local table, and any custom routing tables.
// Routes for interfaces that cannot be found are silently skipped.
//
// Returns:
//   - A slice of Route pointers containing all routes from all tables
//   - An error if the kernel query fails
//
// Example:
//
//	routes, err := GetAllRoutes()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Found %d routes\n", len(routes))
//
// Note: This can return a large number of routes on systems with many interfaces
// or complex routing configurations.
func GetAllRoutes() ([]*Route, error) {
	nlRoutes, err := netlink.RouteList(nil, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	routes := make([]*Route, 0, len(nlRoutes))
	for _, nlRoute := range nlRoutes {
		link, err := netlink.LinkByIndex(nlRoute.LinkIndex)
		if err != nil {
			continue // Skip routes for interfaces we can't find
		}

		route := &Route{
			Destination: nlRoute.Dst,
			Gateway:     nlRoute.Gw,
			Interface:   link.Attrs().Name,
			Metric:      nlRoute.Priority,
			Table:       nlRoute.Table,
			Scope:       nlRoute.Scope,
			Protocol:    nlRoute.Protocol,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// GetDefaultRoute returns the default IPv4 route from the routing table.
// The default route is identified by having no destination (0.0.0.0/0) and a gateway.
// If multiple default routes exist, the first one found is returned.
//
// Returns:
//   - A Route pointer to the default route
//   - An error if no default route is found or the kernel query fails
//
// Example:
//
//	defaultRoute, err := GetDefaultRoute()
//	if err != nil {
//	    log.Printf("No default route: %v", err)
//	} else {
//	    fmt.Printf("Default gateway: %s via %s\n", defaultRoute.Gateway, defaultRoute.Interface)
//	}
//
// Note: This function only looks for IPv4 default routes. For IPv6, a separate
// function would be needed.
func GetDefaultRoute() (*Route, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	for _, nlRoute := range routes {
		// Default route has no destination
		if nlRoute.Dst == nil && nlRoute.Gw != nil {
			link, err := netlink.LinkByIndex(nlRoute.LinkIndex)
			if err != nil {
				continue
			}

			return &Route{
				Destination: nil,
				Gateway:     nlRoute.Gw,
				Interface:   link.Attrs().Name,
				Metric:      nlRoute.Priority,
				Table:       nlRoute.Table,
				Scope:       nlRoute.Scope,
				Protocol:    nlRoute.Protocol,
			}, nil
		}
	}

	return nil, fmt.Errorf("no default route found")
}

// AddDefaultRoute adds a default route (0.0.0.0/0) via the specified gateway and interface.
// The route is added to the main routing table (RT_TABLE_MAIN).
//
// Parameters:
//   - gateway: The IP address of the default gateway
//   - iface: The name of the network interface to use
//   - metric: The route priority/metric (lower values have higher priority)
//
// Returns an error if the interface doesn't exist or the route cannot be added.
//
// Example:
//
//	err := AddDefaultRoute(net.ParseIP("192.168.1.1"), "eth0", 100)
//	if err != nil {
//	    log.Fatalf("Failed to add default route: %v", err)
//	}
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func AddDefaultRoute(gateway net.IP, iface string, metric int) error {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", iface, err)
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gateway,
		Priority:  metric,
		Table:     unix.RT_TABLE_MAIN,
	}

	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("failed to add default route: %w", err)
	}

	return nil
}

// DeleteDefaultRoute deletes the default route via the specified gateway and interface.
//
// Parameters:
//   - gateway: The IP address of the default gateway to remove
//   - iface: The name of the network interface
//
// Returns an error if the interface doesn't exist or the route cannot be deleted.
//
// Example:
//
//	err := DeleteDefaultRoute(net.ParseIP("192.168.1.1"), "eth0")
//	if err != nil {
//	    log.Printf("Failed to delete default route: %v", err)
//	}
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func DeleteDefaultRoute(gateway net.IP, iface string) error {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", iface, err)
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        gateway,
	}

	if err := netlink.RouteDel(route); err != nil {
		return fmt.Errorf("failed to delete default route: %w", err)
	}

	return nil
}

// ReplaceDefaultRoute replaces the existing default route with a new gateway.
// It finds the current default route and replaces it atomically with a new one
// using the specified gateway IP address. The interface and metric from the
// existing default route are preserved.
//
// Parameters:
//   - newGateway: The IP address of the new default gateway
//
// Returns an error if:
//   - No default route currently exists
//   - The interface of the existing route cannot be found
//   - The route replacement fails
//
// Example:
//
//	err := ReplaceDefaultRoute(net.ParseIP("192.168.1.254"))
//	if err != nil {
//	    log.Fatalf("Failed to replace default route: %v", err)
//	}
//	fmt.Println("Default gateway changed to 192.168.1.254")
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
// The function preserves the existing route's interface and metric while only changing
// the gateway address.
func ReplaceDefaultRoute(newGateway net.IP) error {
	// Get the current default route
	currentRoute, err := GetDefaultRoute()
	if err != nil {
		return fmt.Errorf("failed to get current default route: %w", err)
	}

	// Get the interface
	link, err := netlink.LinkByName(currentRoute.Interface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", currentRoute.Interface, err)
	}

	// Create the new default route with the new gateway
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Gw:        newGateway,
		Priority:  currentRoute.Metric,
		Table:     unix.RT_TABLE_MAIN,
	}

	// Replace the route atomically
	if err := netlink.RouteReplace(route); err != nil {
		return fmt.Errorf("failed to replace default route: %w", err)
	}

	return nil
}

// FlushRoutes removes all routes from the specified network interface.
// This will delete all routing entries that use the given interface,
// but continues even if some routes fail to delete.
//
// Parameters:
//   - iface: The name of the network interface to flush routes from
//
// Returns an error if the interface doesn't exist or the route list cannot be retrieved.
// Individual route deletion failures are silently ignored.
//
// Example:
//
//	err := FlushRoutes("eth0")
//	if err != nil {
//	    log.Fatalf("Failed to flush routes: %v", err)
//	}
//
// Warning: This is a destructive operation that will remove ALL routes for the interface.
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func FlushRoutes(iface string) error {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", iface, err)
	}

	routes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	for _, route := range routes {
		if err := netlink.RouteDel(&route); err != nil {
			// Continue even if some routes fail to delete
			continue
		}
	}

	return nil
}

// FlushRoutesInTable removes all routes from the specified routing table.
// This will delete all routing entries in the given table, but continues
// even if some routes fail to delete.
//
// Parameters:
//   - table: The routing table ID to flush (e.g., unix.RT_TABLE_MAIN)
//
// Returns an error if the route list cannot be retrieved.
// Individual route deletion failures are silently ignored.
//
// Example:
//
//	err := FlushRoutesInTable(unix.RT_TABLE_MAIN)
//	if err != nil {
//	    log.Fatalf("Failed to flush routing table: %v", err)
//	}
//
// Warning: This is a destructive operation that will remove ALL routes from the table.
// Be especially careful when flushing RT_TABLE_MAIN as it contains the system's main routes.
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func FlushRoutesInTable(table int) error {
	filter := &netlink.Route{
		Table: table,
	}

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_ALL, filter, netlink.RT_FILTER_TABLE)
	if err != nil {
		return fmt.Errorf("failed to list routes: %w", err)
	}

	for _, route := range routes {
		if err := netlink.RouteDel(&route); err != nil {
			// Continue even if some routes fail to delete
			continue
		}
	}

	return nil
}

// GetRouteToDestination finds the route that the kernel would use to reach a destination IP.
// This performs a route lookup query to the kernel and returns the route that would be
// selected for packets destined to the given IP address.
//
// Parameters:
//   - destination: The destination IP address to look up
//
// Returns:
//   - A Route pointer describing the route that would be used
//   - An error if the route lookup fails or the interface cannot be found
//
// Example:
//
//	route, err := GetRouteToDestination(net.ParseIP("8.8.8.8"))
//	if err != nil {
//	    log.Fatalf("Route lookup failed: %v", err)
//	}
//	fmt.Printf("Traffic to 8.8.8.8 goes via %s through %s\n", route.Gateway, route.Interface)
//
// Note: This does not add or modify any routes, it only queries the kernel's routing decision.
func GetRouteToDestination(destination net.IP) (*Route, error) {
	nlRoute, err := netlink.RouteGet(destination)
	if err != nil {
		return nil, fmt.Errorf("failed to get route to %s: %w", destination, err)
	}

	if len(nlRoute) == 0 {
		return nil, ErrNoRouteFound
	}

	r := nlRoute[0]
	link, err := netlink.LinkByIndex(r.LinkIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface for route: %w", err)
	}

	return &Route{
		Destination: r.Dst,
		Gateway:     r.Gw,
		Interface:   link.Attrs().Name,
		Metric:      r.Priority,
		Table:       r.Table,
		Scope:       r.Scope,
		Protocol:    r.Protocol,
	}, nil
}

// RouteExists checks if a specific route exists in the routing table.
// It queries the specified routing table and compares all routes against the provided route
// using destination, gateway, interface, and metric for matching.
//
// Parameters:
//   - route: The route to search for (must not be nil)
//
// Returns:
//   - true if a matching route exists, false otherwise
//   - An error if the route is nil or the routing table cannot be queried
//
// Example:
//
//	route := &Route{
//	    Destination: parseIPNet("192.168.1.0/24"),
//	    Gateway:     net.ParseIP("10.0.0.1"),
//	    Interface:   "eth0",
//	    Metric:      100,
//	    Table:       unix.RT_TABLE_MAIN,
//	}
//	exists, err := RouteExists(route)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if exists {
//	    fmt.Println("Route already exists")
//	}
func RouteExists(route *Route) (bool, error) {
	if route == nil {
		return false, fmt.Errorf("route cannot be nil")
	}

	routes, err := GetRoutes(route.Table)
	if err != nil {
		return false, err
	}

	for _, r := range routes {
		if routesMatch(r, route) {
			return true, nil
		}
	}

	return false, nil
}

// routesMatch checks if two routes are equivalent by comparing their key fields.
// Two routes match if they have the same destination, gateway, interface, and metric.
//
// Parameters:
//   - r1: The first route to compare
//   - r2: The second route to compare
//
// Returns:
//   - true if the routes match, false otherwise
//
// The comparison includes:
//   - Destination network (including both IP and netmask)
//   - Gateway IP address
//   - Interface name
//   - Metric value
//
// Note: nil routes or routes with only one nil field component will not match.
func routesMatch(r1, r2 *Route) bool {
	if r1 == nil || r2 == nil {
		return false
	}

	// Compare destinations
	if (r1.Destination == nil) != (r2.Destination == nil) {
		return false
	}
	if r1.Destination != nil && r2.Destination != nil {
		if !r1.Destination.IP.Equal(r2.Destination.IP) ||
			r1.Destination.Mask.String() != r2.Destination.Mask.String() {
			return false
		}
	}

	// Compare gateways
	if (r1.Gateway == nil) != (r2.Gateway == nil) {
		return false
	}
	if r1.Gateway != nil && r2.Gateway != nil && !r1.Gateway.Equal(r2.Gateway) {
		return false
	}

	// Compare interface and metric
	return r1.Interface == r2.Interface && r1.Metric == r2.Metric
}

// AddHostRoute adds a route for a specific host IP address (/32 route).
// This creates a host-specific route with a /32 netmask, meaning it applies
// only to the exact IP address specified.
//
// Parameters:
//   - hostIP: The specific host IP address to route
//   - gateway: The gateway IP address to use
//   - iface: The name of the network interface
//   - metric: The route priority/metric
//
// Returns an error if the IP parsing fails, the interface doesn't exist,
// or the route cannot be added.
//
// Example:
//
//	err := AddHostRoute(net.ParseIP("192.168.1.100"), net.ParseIP("10.0.0.1"), "eth0", 100)
//	if err != nil {
//	    log.Fatalf("Failed to add host route: %v", err)
//	}
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func AddHostRoute(hostIP net.IP, gateway net.IP, iface string, metric int) error {
	_, ipNet, err := net.ParseCIDR(hostIP.String() + "/32")
	if err != nil {
		return fmt.Errorf("failed to parse host IP: %w", err)
	}

	route := &Route{
		Destination: ipNet,
		Gateway:     gateway,
		Interface:   iface,
		Metric:      metric,
		Table:       unix.RT_TABLE_MAIN,
		Scope:       netlink.SCOPE_UNIVERSE,
	}

	return AddRoute(route)
}

// AddNetworkRoute adds a route for an entire network specified in CIDR notation.
// The route is added to the main routing table with SCOPE_UNIVERSE.
//
// Parameters:
//   - network: The destination network in CIDR notation (e.g., from net.ParseCIDR)
//   - gateway: The gateway IP address to use
//   - iface: The name of the network interface
//   - metric: The route priority/metric
//
// Returns an error if the interface doesn't exist or the route cannot be added.
//
// Example:
//
//	_, network, _ := net.ParseCIDR("10.0.0.0/8")
//	err := AddNetworkRoute(network, net.ParseIP("192.168.1.1"), "eth0", 100)
//	if err != nil {
//	    log.Fatalf("Failed to add network route: %v", err)
//	}
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func AddNetworkRoute(network *net.IPNet, gateway net.IP, iface string, metric int) error {
	route := &Route{
		Destination: network,
		Gateway:     gateway,
		Interface:   iface,
		Metric:      metric,
		Table:       unix.RT_TABLE_MAIN,
		Scope:       netlink.SCOPE_UNIVERSE,
	}

	return AddRoute(route)
}

// DeleteNetworkRoute deletes a route for a network specified in CIDR notation.
// The route is removed from the main routing table.
//
// Parameters:
//   - network: The destination network in CIDR notation
//   - gateway: The gateway IP address of the route to delete
//   - iface: The name of the network interface
//
// Returns an error if the interface doesn't exist or the route cannot be deleted.
//
// Example:
//
//	_, network, _ := net.ParseCIDR("10.0.0.0/8")
//	err := DeleteNetworkRoute(network, net.ParseIP("192.168.1.1"), "eth0")
//	if err != nil {
//	    log.Printf("Failed to delete network route: %v", err)
//	}
//
// Note: This operation requires appropriate privileges (typically root/CAP_NET_ADMIN).
func DeleteNetworkRoute(network *net.IPNet, gateway net.IP, iface string) error {
	route := &Route{
		Destination: network,
		Gateway:     gateway,
		Interface:   iface,
		Table:       unix.RT_TABLE_MAIN,
	}

	return DeleteRoute(route)
}

// GetRoutesForInterface returns all routes associated with a specific network interface.
// This includes routes where the interface is used for forwarding traffic.
//
// Parameters:
//   - iface: The name of the network interface to query
//
// Returns:
//   - A slice of Route pointers for all routes using the specified interface
//   - An error if the interface doesn't exist or the route list cannot be retrieved
//
// Example:
//
//	routes, err := GetRoutesForInterface("eth0")
//	if err != nil {
//	    log.Fatalf("Failed to get routes: %v", err)
//	}
//	fmt.Printf("Found %d routes on eth0\n", len(routes))
//	for _, route := range routes {
//	    fmt.Println(route.String())
//	}
func GetRoutesForInterface(iface string) ([]*Route, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, fmt.Errorf("failed to get interface %s: %w", iface, err)
	}

	nlRoutes, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	routes := make([]*Route, 0, len(nlRoutes))
	for _, nlRoute := range nlRoutes {
		route := &Route{
			Destination: nlRoute.Dst,
			Gateway:     nlRoute.Gw,
			Interface:   iface,
			Metric:      nlRoute.Priority,
			Table:       nlRoute.Table,
			Scope:       nlRoute.Scope,
			Protocol:    nlRoute.Protocol,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// String returns a human-readable representation of the route in a format
// similar to the output of the 'ip route' command.
//
// The format is: "<destination> via <gateway> dev <interface> metric <metric> table <table>"
//
// Special cases:
//   - If Destination is nil, it shows "default" (representing 0.0.0.0/0)
//   - If Gateway is nil, it shows "none" (for directly connected networks)
//
// Returns:
//   - A formatted string describing the route
//   - "<nil>" if the route pointer itself is nil
//
// Example output:
//
//	"192.168.1.0/24 via 10.0.0.1 dev eth0 metric 100 table 254"
//	"default via 192.168.1.1 dev eth0 metric 0 table 254"
//	"172.16.0.0/16 via none dev bat0 metric 10 table 254"
func (r *Route) String() string {
	if r == nil {
		return "<nil>"
	}

	dest := "default"
	if r.Destination != nil {
		dest = r.Destination.String()
	}

	gw := "none"
	if r.Gateway != nil {
		gw = r.Gateway.String()
	}

	return fmt.Sprintf("%s via %s dev %s metric %d table %d",
		dest, gw, r.Interface, r.Metric, r.Table)
}
