package mgmt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/openmanet/go-alfred"
	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/openmanet/openmanetd/internal/network"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

	algorithm := readAlgorithm(getMeshCfg, w.Config.Log)

	payload := &netv1.MeshNeighbors{
		PrimaryMac:  strings.ToLower(iface.MAC),
		Hostname:    stripIfaceSuffix(hostname),
		Algorithm:   algorithm,
		Neighbors:   mapNeighbors(neighbors),
		Originators: mapOriginatorsBestOnly(getOriginators, w.Config.Log),
		CollectedAt: timestamppb.New(w.now()),
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
// hostname, e.g. "BCM2711-97d6_bat0" → "BCM2711-97d6". Names without
// an underscore round-trip unchanged. Duplicated from the mesh_topology
// handler so the publisher doesn't import that package.
func stripIfaceSuffix(name string) string {
	if i := strings.LastIndex(name, "_"); i > 0 {
		return name[:i]
	}

	return name
}
