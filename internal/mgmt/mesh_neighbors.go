package mgmt

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/openmanet/go-alfred"
	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ifaceSuffixRe matches a trailing "_<iface>" token whose first
// character is a lowercase letter — the structural fingerprint of
// every Linux network interface name OpenMANET nodes carry on the
// wire (bat0, wlan0, eth0, vxlan0, phy2-mesh0, br-ahwlan,
// wlan-10-04, mesh0, etc.). The character class allows letters,
// digits, dashes, AND underscores so a hostname carrying a chain of
// suffixes ("BCM2711-88ba_phy2-mesh0_bat0") is consumed in a single
// match. Anything starting with a digit ("_27") or uppercase letter
// ("_BACKUP") is preserved — that's the rule that keeps legitimate
// underscored hostnames like "Gate_04_27" intact. Compiled once per
// the performance rules. Kept in lockstep with the duplicate in
// internal/openmanet/server/handlers/mesh_topology.go.
var ifaceSuffixRe = regexp.MustCompile(`_[a-z][a-z0-9_-]*$`)

// blosIfname identifies the BLOS tunnel interface. Duplicated from the
// mesh_topology handler so this package doesn't import it; the value is
// stable across the daemon and the two copies live in lockstep via
// integration tests.
const blosIfname = "vxlan0"

const (
	// MeshNeighborsDataType is the alfred datatype ID reserved for the
	// per-node neighbor+originator gossip payload. Kept in lockstep with
	// DATA_TYPE_MESH_NEIGHBORS in proto/openmanet/network/v1/datatype.proto.
	MeshNeighborsDataType uint8 = uint8(netv1.DataType_DATA_TYPE_MESH_NEIGHBORS)

	// MeshNeighborsDataTypeVersion is bumped when the on-wire encoding of
	// MeshNeighbors changes in a non-additive way. Additive proto changes
	// keep version = 1.
	MeshNeighborsDataTypeVersion uint8 = 1
)

// MeshNeighborsWorker publishes this node's direct batman-adv neighbor
// table (partitioned by interface) and its own best-route originator
// rows to alfred every Interval. Consumers on every other node build a
// true mesh-wide topology graph from the union of all nodes' payloads.
type MeshNeighborsWorker struct {
	Config   *ManagementConfig
	Client   *alfred.Client
	Now      func() time.Time
	done     <-chan struct{}
	Interval time.Duration
}

// NewMeshNeighborsWorker wires a publisher bound to the provided alfred
// client. The returned worker exits its goroutine loops when ctx is
// canceled.
func NewMeshNeighborsWorker(cfg *ManagementConfig, client *alfred.Client, interval time.Duration, ctx context.Context) *MeshNeighborsWorker {
	cfg.Log.Info().Dur("interval", interval).Msg("MeshNeighborsWorker initialized")

	return &MeshNeighborsWorker{
		Config:   cfg,
		Client:   client,
		Interval: interval,
		done:     ctx.Done(),
	}
}

// StartSend runs the publish ticker until the bound context is canceled.
// Errors inside a single tick are logged and swallowed so a transient
// batctl failure doesn't tear down the loop.
func (w *MeshNeighborsWorker) StartSend() {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			if err := w.sendOnce(w.Client); err != nil {
				w.Config.Log.Error().Err(err).Msg("mesh-neighbors publish tick failed")
			}
		}
	}
}

// sendOnce performs a single publish iteration using production data
// sources. Separated from the ticker so tests can drive it directly.
func (w *MeshNeighborsWorker) sendOnce(client alfredClient) error {
	return w.sendOnceWithDeps(
		client,
		batmanadv.GetMeshNeighbors,
		func() ([]batmanadv.Originator, error) {
			return (&batmanadv.BatctlOriginatorProvider{}).GetOriginators()
		},
		func() (*batmanadv.MeshConfig, error) {
			return batmanadv.GetMeshConfig(w.Config.BatInterface)
		},
		network.GetInterfaceByName,
		os.Hostname,
		listLocalInterfaceMacs,
	)
}

// sendOnceWithDeps is the dependency-injected publish body.
//
// Contract: errors reading the neighbor table abort the publish (nothing
// to say without it). Errors reading the originator table or the mesh
// config are logged and the publish continues with the empty/default
// value — neighbor data alone still classifies mesh segments, just
// without multi-hop chain resolution.
func (w *MeshNeighborsWorker) sendOnceWithDeps(
	client alfredClient,
	getNeighbors func() (*batmanadv.Neighbors, error),
	getOriginators func() ([]batmanadv.Originator, error),
	getMeshCfg func() (*batmanadv.MeshConfig, error),
	getIface func(string) network.NetworkInterface,
	getHostname func() (string, error),
	listMacs func() []string,
) error {
	neighbors, err := getNeighbors()
	if err != nil {
		return fmt.Errorf("read batman neighbors: %w", err)
	}

	hostname, err := getHostname()
	if err != nil {
		w.Config.Log.Warn().Err(err).Msg("mesh-neighbors: hostname lookup failed; using empty")

		hostname = ""
	}

	iface := getIface(w.Config.BatInterface)
	primaryMac := strings.ToLower(iface.MAC)

	algorithm := readAlgorithm(getMeshCfg, w.Config.Log)

	payload := &netv1.MeshNeighbors{
		PrimaryMac:    primaryMac,
		Hostname:      stripIfaceSuffix(hostname),
		Algorithm:     algorithm,
		Neighbors:     mapNeighbors(neighbors),
		Originators:   mapOriginatorsBestOnly(getOriginators, w.Config.Log),
		CollectedAt:   timestamppb.New(w.now()),
		InterfaceMacs: collectInterfaceMacs(primaryMac, listMacs),
	}

	buf, err := payload.MarshalVT()
	if err != nil {
		return fmt.Errorf("marshal mesh-neighbors payload: %w", err)
	}

	if err := client.Set(MeshNeighborsDataType, MeshNeighborsDataTypeVersion, buf); err != nil {
		return fmt.Errorf("alfred set mesh-neighbors: %w", err)
	}

	return nil
}

func (w *MeshNeighborsWorker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}

	return time.Now()
}

// readAlgorithm resolves the local batman-adv algorithm. A failure here
// is degraded — consumers treat algorithm=0 as "unknown" and render
// metrics in a unit-agnostic way. The name→id mapping mirrors the
// numeric codes batadv-vis emits (4 = IV, 15 = V).
func readAlgorithm(getMeshCfg func() (*batmanadv.MeshConfig, error), log zerolog.Logger) int32 {
	cfg, err := getMeshCfg()
	if err != nil {
		log.Warn().Err(err).Msg("mesh-neighbors: mesh config lookup failed; algorithm=0")

		return 0
	}

	switch cfg.AlgoName {
	case "BATMAN_IV":
		return 4
	case "BATMAN_V":
		return 15
	default:
		return 0
	}
}

// mapNeighbors converts the batctl nj rows into proto entries. nil and
// empty inputs map to nil for wire compactness.
func mapNeighbors(neighbors *batmanadv.Neighbors) []*netv1.MeshNeighbor {
	if neighbors == nil || len(*neighbors) == 0 {
		return nil
	}

	out := make([]*netv1.MeshNeighbor, 0, len(*neighbors))
	for _, n := range *neighbors {
		out = append(out, &netv1.MeshNeighbor{
			Mac:            strings.ToLower(n.NeighAddress),
			HardIfname:     n.HardIfname,
			Blos:           n.HardIfname == blosIfname,
			LastSeenMsecs:  int32(n.LastSeenMsecs), //nolint:gosec // msec counter fits
			ThroughputKbps: int64(n.Throughput),
			// TQ isn't reported on neighbor rows (only on originators);
			// leave 0 and let consumers fall back to throughput.
		})
	}

	return out
}

// mapOriginatorsBestOnly reads the originator table and keeps only
// Best==true rows (the publisher's own routing view). Failures degrade
// to an empty slice so the neighbor data still ships.
func mapOriginatorsBestOnly(getOrigs func() ([]batmanadv.Originator, error), log zerolog.Logger) []*netv1.Originator {
	origs, err := getOrigs()
	if err != nil {
		log.Warn().Err(err).Msg("mesh-neighbors: originator lookup failed; publishing neighbors only")

		return nil
	}

	out := make([]*netv1.Originator, 0, len(origs))
	for _, o := range origs {
		if !o.Best {
			continue
		}

		out = append(out, &netv1.Originator{
			OrigMac:        strings.ToLower(o.OrigAddress),
			NextHopMac:     strings.ToLower(o.BestNeigh),
			HardIfname:     o.HardIfname,
			ThroughputKbps: int64(o.Throughput),
			Tq:             int32(o.TQ), //nolint:gosec // TQ is 0-255
			// batctl oj doesn't include hops; consumers compute it from
			// (next_hop_mac == orig_mac) direct-neighbor heuristic when
			// they need it.
		})
	}

	return out
}

// stripIfaceSuffix removes a trailing "_<iface>" token from a bat-hosts
// hostname, e.g. "BCM2711-97d6_bat0" → "BCM2711-97d6". Only strips
// suffixes that match a recognized network interface name (bat0,
// vxlan0, wlan0, eth0, etc.) so legitimate underscored hostnames like
// "Gate_04_27" are preserved verbatim. Duplicated from the
// mesh_topology handler — keep the two in lockstep.
func stripIfaceSuffix(name string) string {
	loc := ifaceSuffixRe.FindStringIndex(name)
	if loc == nil || loc[0] == 0 {
		return name
	}

	return name[:loc[0]]
}

// listLocalInterfaceMacs returns the lowercased MACs of local layer-2
// interfaces that participate (or could participate) in batman-adv
// routing — bat0, vxlan0, wlan0, phy<N>-mesh<N>, eth*, etc. Filters
// applied:
//
//   - skip loopback (no MAC anyway, but explicit)
//   - skip point-to-point interfaces (wireguard, tun, tap, ppp,
//     tailscale) — no L2 broadcast domain, no peer vis will see them
//   - skip interfaces whose name matches a non-mesh prefix (defense
//     in depth for drivers that don't set FlagPointToPoint correctly,
//     and for morse0 which IS broadcast-flagged but carries a
//     known-shared placeholder MAC)
//   - skip empty MACs and obvious placeholders (all-zero,
//     multicast-bit set, or a "trailing-five-zeros" pattern like
//     12:00:00:00:00:00). The trailing-zeros check catches drivers
//     that set only an OUI-style first byte and leave the rest
//     unconfigured — universally a placeholder, never a real
//     interface MAC
//
// The list is sorted for determinism in the published payload.
func listLocalInterfaceMacs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		if iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}

		if isNonMeshInterfaceName(iface.Name) {
			continue
		}

		mac := iface.HardwareAddr
		if !isUsableInterfaceMac(mac) {
			continue
		}

		out = append(out, strings.ToLower(mac.String()))
	}

	sort.Strings(out)

	return out
}

// isNonMeshInterfaceName returns true when name matches one of the
// known non-mesh interface prefixes. Case-insensitive on the prefix
// match, since iface names are conventionally lowercase but kernel
// drivers occasionally diverge.
//
// Prefix list (rationale):
//   - morse: Morse Micro HaLow radio. The driver hard-codes
//     12:00:00:00:00:00 as the device MAC on every board — including
//     it in interface_macs would index every node's gossip record
//     under the same colliding key.
//   - tun, tap, wg, tailscale, ppp: tunnel interfaces. Already caught
//     by FlagPointToPoint, but the name filter is faster and survives
//     drivers that mis-flag their devices.
//   - docker: container bridge — never participates in mesh routing.
func isNonMeshInterfaceName(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range []string{"morse", "tun", "tap", "wg", "tailscale", "ppp", "docker"} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	return false
}

// isUsableInterfaceMac returns true when mac is a syntactically valid
// L2 unicast hardware address suitable for use as a node identifier.
// Rejects:
//
//   - wrong length (not 6 bytes)
//   - multicast bit set in byte 0 (not a valid interface MAC)
//   - all-zero (00:00:00:00:00:00 — uninitialised interface)
//   - trailing-five-zeros (e.g. 12:00:00:00:00:00 — the Morse Micro
//     HaLow radio's hard-coded placeholder, plus any other driver
//     defaulting to "set the OUI-style first byte and leave the rest
//     blank"). A real NIC's lower bytes are never all zero outside
//     contrived test vectors.
func isUsableInterfaceMac(mac net.HardwareAddr) bool {
	if len(mac) != 6 {
		return false
	}

	// Multicast bit (LSB of byte 0) must be 0 for an interface MAC.
	if mac[0]&0x01 != 0 {
		return false
	}

	trailingAllZero := true

	for i := 1; i < 6; i++ {
		if mac[i] != 0 {
			trailingAllZero = false

			break
		}
	}

	if trailingAllZero {
		// Catches both 00:00:00:00:00:00 and the morse0 12:00:…
		// pattern in one branch. A real interface's lower five
		// bytes are never all zero.
		return false
	}

	return true
}

// collectInterfaceMacs builds the InterfaceMacs slice published in the
// gossip payload. Always includes primaryMac (when non-empty) so a
// single index lookup is sufficient on the receiver. Deduplicates and
// returns a sorted slice for stable on-wire output.
func collectInterfaceMacs(primaryMac string, listMacs func() []string) []string {
	seen := make(map[string]struct{}, 8)

	add := func(mac string) {
		mac = strings.ToLower(mac)
		if mac == "" {
			return
		}

		seen[mac] = struct{}{}
	}

	add(primaryMac)

	if listMacs != nil {
		for _, m := range listMacs() {
			add(m)
		}
	}

	if len(seen) == 0 {
		return nil
	}

	out := make([]string, 0, len(seen))
	for mac := range seen {
		out = append(out, mac)
	}

	sort.Strings(out)

	return out
}
