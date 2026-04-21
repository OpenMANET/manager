// Package blos manages Beyond Line-of-Sight (BLOS) mesh networking by
// orchestrating Tailscale VPN, VXLAN encapsulation, and Batman-adv integration.
//
// # Architecture
//
// The package is organized around three main types:
//
//   - [BLOSManager] owns the lifecycle (enable/disable) and serializes operations
//     via a mutex. It implements [BLOSLifecycle], which is consumed by the API handler.
//   - [BLOS] is the core engine that configures network interfaces, manages VXLAN
//     peers, and joins multicast groups. Construct with [NewBLOS], then call Start.
//   - [StatusWorker] polls the Tailscale daemon for peer status at a configurable
//     interval and triggers VXLAN peer synchronization via a callback.
//
// # Network Stack
//
// BLOS creates a three-layer network stack on gateway nodes:
//
//	tailscale0  (Tailscale tunnel, MTU 1280 — Tailscale's default)
//	  └─ vxlan0   (VXLAN encapsulation over tailscale0, MTU 1230 = 1280 − 50B VXLAN overhead)
//	       └─ battunnel0  (Batman-adv hard interface on vxlan0)
//
// See the MTU chain documentation in interfaces.go for why these values
// account for WireGuard's ~60 byte outer overhead in addition to VXLAN's.
//
// # Lifecycle
//
//  1. [NewBLOS] checks gateway mode and creates the instance (no I/O).
//  2. [BLOS.Start] configures interfaces, sets MTU, joins multicast groups,
//     and starts the [StatusWorker]. On first configuration, the system reboots.
//  3. [StatusWorker] periodically polls Tailscale, calling syncVXLANPeersWithTailscale
//     to add/remove VXLAN peers as Tailscale peers join or leave.
//  4. [BLOS.Stop] stops the worker and closes multicast sockets.
//
// # Testability
//
// All external dependencies are injectable: [TailscaleClient], [StatusClient],
// [InterfaceManager], [TailscaleAuthClient], and [InitDService] are interfaces.
// System-level operations (reboot, exec, mesh config lookup) are overrideable
// function fields on the [BLOS] struct.
package blos
