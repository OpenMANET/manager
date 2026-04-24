package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	meshtopov1 "github.com/openmanet/openmanetd/internal/api/openmanet/mesh_topology/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// blosIfname is the local hard_ifname that identifies a BLOS tunnel
// route. Duplicated here (kept in lockstep with the frontend constant)
// because the Go handler drives segment assignment and needs the literal.
const blosIfname = "vxlan0"

// Segment labels emitted on every MeshNode. Kept in lockstep with the
// frontend's MeshNode.segment union ("local" | "remote").
const (
	segmentLocal  = "local"
	segmentRemote = "remote"
)

// MeshTopologyService serves the mesh-wide topology built from
// batadv-vis (primary) and overlays this node's best-route information
// from its originator table.
type MeshTopologyService struct {
	Log           zerolog.Logger
	VisProvider   batmanadv.VisProvider
	OrigProvider  batmanadv.OriginatorTopologyProvider
	DeltaTracker  *DeltaTracker
	ParseBatHosts func(string) (*batmanadv.BatHosts, error)
	Now           func() time.Time
	BatHostsPath  string
}

func (s *MeshTopologyService) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// GetMeshTopology returns the current mesh-wide topology. Empty/unavailable
// vis data yields an empty response (not an error) so the UI renders its
// empty state without a banner.
func (s *MeshTopologyService) GetMeshTopology(ctx context.Context, _ *emptypb.Empty) (*meshtopov1.GetMeshTopologyResponse, error) {
	s.Log.Debug().Msg("GetMeshTopology Request Received")

	if s.VisProvider == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("vis provider not configured"))
	}

	visDoc, err := s.VisProvider.GetMeshVis(ctx)
	if err != nil {
		if errors.Is(err, batmanadv.ErrVisUnavailable) {
			return &meshtopov1.GetMeshTopologyResponse{
				Topology: &meshtopov1.MeshTopology{CollectedAt: timestamppb.New(s.now())},
			}, nil
		}

		s.Log.Error().Err(err).Msg("batadv-vis fetch failed")

		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("batadv-vis: %w", err))
	}

	// Originator overlay is best-effort. If it fails we still render the
	// mesh graph without hops / my_hard_ifname / on_my_path data.
	origSnap, origErr := s.OrigProvider.GetOriginatorTopology()
	if origErr != nil {
		s.Log.Warn().Err(origErr).Msg("originator provider failed; rendering mesh graph without self-path overlay")

		origSnap = &batmanadv.OriginatorTopology{}
	}

	// bat-hosts for nodes outside the originator table (hostname fallback).
	var batHosts *batmanadv.BatHosts
	if s.ParseBatHosts != nil {
		batHosts, _ = s.ParseBatHosts(s.BatHostsPath)
	}

	merged := mergeMeshTopology(visDoc, origSnap, batHosts)
	merged.CollectedAt = timestamppb.New(s.now())

	return &meshtopov1.GetMeshTopologyResponse{Topology: merged}, nil
}

// mergeMeshTopology transforms vis + originator + bat-hosts inputs into
// the wire-shape MeshTopology. Factored out for test readability.
func mergeMeshTopology(
	visDoc *batmanadv.VisDoc,
	origSnap *batmanadv.OriginatorTopology,
	batHosts *batmanadv.BatHosts,
) *meshtopov1.MeshTopology {
	if visDoc == nil || len(visDoc.Vis) == 0 {
		return &meshtopov1.MeshTopology{
			SelfMac:      origSnap.SelfMAC,
			SelfHostname: origSnap.SelfHostname,
		}
	}

	// 1. MAC → primary index for every node (primary and every secondary
	//    share the node's primary MAC as their key).
	primaryByMac := make(map[string]string, len(visDoc.Vis)*2)
	for _, v := range visDoc.Vis {
		primaryByMac[v.Primary] = v.Primary
		for _, sec := range v.Secondary {
			primaryByMac[sec] = v.Primary
		}
	}

	selfMAC := strings.ToLower(origSnap.SelfMAC)
	selfPrimary := primaryByMac[selfMAC]

	// 2. Segment assignment.
	segmentByPrimary := segmentMap(origSnap, primaryByMac)
	if selfPrimary != "" {
		segmentByPrimary[selfPrimary] = segmentLocal // self is always local
	}

	// 3. My originator entries indexed by orig MAC for overlay lookups.
	origByMac := make(map[string]*batmanadv.OriginatorEntry, len(origSnap.Originators))
	for i := range origSnap.Originators {
		origByMac[strings.ToLower(origSnap.Originators[i].OrigMAC)] = &origSnap.Originators[i]
	}

	// 4. Canonical edges that lie on MY forwarding tree (for on_my_path).
	//    Direct neighbors have OrigMAC == NextHopMAC in my originator
	//    table, so we anchor those edges on selfMAC.
	myEdges := myForwardingEdges(origSnap.Originators, selfMAC)

	// 5. Build MeshNode list.
	nodes := buildMeshNodes(visDoc, origByMac, batHosts, segmentByPrimary, selfPrimary, origSnap.SelfHostname)

	// 6. Build MeshEdge list.
	edges := buildMeshEdges(visDoc, primaryByMac, segmentByPrimary, myEdges)

	// 7. Algorithm label — prefer vis, fall back to originator-derived.
	algorithm := visDoc.AlgorithmLabel()
	if algorithm == "" {
		algorithm = origSnap.Algorithm
	}

	return &meshtopov1.MeshTopology{
		SelfMac:      origSnap.SelfMAC,
		SelfHostname: origSnap.SelfHostname,
		Algorithm:    algorithm,
		Nodes:        nodes,
		Edges:        edges,
	}
}

// segmentMap classifies each vis primary as "local" or "remote" based on
// the serving node's originator table: any route via vxlan0 → remote,
// unless the same primary also has a non-vxlan0 route (RF wins).
// Primaries absent from the originator table default to local.
func segmentMap(
	origSnap *batmanadv.OriginatorTopology,
	primaryByMac map[string]string,
) map[string]string {
	ifsByPrimary := make(map[string]map[string]struct{})

	for _, o := range origSnap.Originators {
		primary := primaryByMac[strings.ToLower(o.OrigMAC)]
		if primary == "" {
			continue
		}

		m, ok := ifsByPrimary[primary]
		if !ok {
			m = make(map[string]struct{})
			ifsByPrimary[primary] = m
		}

		m[o.HardIfname] = struct{}{}
	}

	out := make(map[string]string, len(ifsByPrimary))

	for primary, ifs := range ifsByPrimary {
		hasRF := false
		_, hasBLOS := ifs[blosIfname]

		for name := range ifs {
			if name != "" && name != blosIfname {
				hasRF = true

				break
			}
		}

		switch {
		case hasRF:
			out[primary] = segmentLocal
		case hasBLOS:
			out[primary] = segmentRemote
		default:
			out[primary] = segmentLocal
		}
	}

	return out
}

// myForwardingEdges returns the set of canonical (from|to) edge keys on
// the serving node's best-route tree, used to flag on_my_path edges.
// Direct neighbors (NextHopMAC == OrigMAC) anchor on selfMAC so the
// (self, peer) edge is represented in the set.
func myForwardingEdges(origs []batmanadv.OriginatorEntry, selfMAC string) map[string]struct{} {
	out := make(map[string]struct{}, len(origs))

	for _, o := range origs {
		origMAC := strings.ToLower(o.OrigMAC)
		nextMAC := strings.ToLower(o.NextHopMAC)

		if origMAC == "" {
			continue
		}

		// Direct neighbor: the forwarding edge is self → origMAC.
		if nextMAC == "" || nextMAC == origMAC {
			if selfMAC == "" || selfMAC == origMAC {
				continue
			}

			a, b := canonicalPair(selfMAC, origMAC)
			out[a+"|"+b] = struct{}{}

			continue
		}

		// Multi-hop: forwarding edge is nextHop → origMAC (the next
		// link in the chain originating from our forwarder).
		a, b := canonicalPair(nextMAC, origMAC)
		if a == b {
			continue
		}

		out[a+"|"+b] = struct{}{}
	}

	return out
}

// buildMeshNodes materializes MeshNode records from the vis payload,
// joining in originator overlay and bat-hosts hostnames.
func buildMeshNodes(
	visDoc *batmanadv.VisDoc,
	origByMac map[string]*batmanadv.OriginatorEntry,
	batHosts *batmanadv.BatHosts,
	segmentByPrimary map[string]string,
	selfPrimary string,
	selfHostname string,
) []*meshtopov1.MeshNode {
	nodes := make([]*meshtopov1.MeshNode, 0, len(visDoc.Vis))

	for _, v := range visDoc.Vis {
		node := &meshtopov1.MeshNode{
			Mac:           v.Primary,
			SecondaryMacs: append([]string(nil), v.Secondary...),
			ClientCount:   int32(len(v.Clients)),
			IsSelf:        v.Primary == selfPrimary && selfPrimary != "",
		}

		if seg, ok := segmentByPrimary[v.Primary]; ok {
			node.Segment = seg
		} else {
			node.Segment = segmentLocal
		}

		entry := lookupOrigEntry(v, origByMac)
		switch {
		case node.IsSelf:
			node.HopsFromSelf = 0
		case entry != nil:
			node.HopsFromSelf = int32(entry.Hops)
			node.MyHardIfname = entry.HardIfname
		default:
			node.HopsFromSelf = hopsUnknown
		}

		if node.IsSelf {
			node.Hostname = stripIfaceSuffix(selfHostname)
		} else {
			node.Hostname = lookupHostname(v, entry, batHosts)
		}

		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsSelf != nodes[j].IsSelf {
			return nodes[i].IsSelf
		}

		if nodes[i].Hostname != nodes[j].Hostname {
			if nodes[i].Hostname == "" {
				return false
			}

			if nodes[j].Hostname == "" {
				return true
			}

			return nodes[i].Hostname < nodes[j].Hostname
		}

		return nodes[i].Mac < nodes[j].Mac
	})

	return nodes
}

// buildMeshEdges deduplicates vis neighbor entries into canonical edges
// and tags each with its BLOS / on_my_path flags.
func buildMeshEdges(
	visDoc *batmanadv.VisDoc,
	primaryByMac map[string]string,
	segmentByPrimary map[string]string,
	myEdges map[string]struct{},
) []*meshtopov1.MeshEdge {
	edgeByKey := make(map[string]*meshtopov1.MeshEdge)

	for _, v := range visDoc.Vis {
		for _, n := range v.Neighbors {
			a, b := canonicalPair(n.Router, n.Neighbor)
			if a == "" || b == "" || a == b {
				continue
			}

			key := a + "|" + b
			metric := batmanadv.ParseMetric(n.Metric)

			if e, ok := edgeByKey[key]; ok {
				if isBetterMetric(metric, e.Metric, visDoc.Algorithm) {
					e.Metric = metric
				}

				continue
			}

			segA := segmentOrDefault(segmentByPrimary, primaryByMac[a])
			segB := segmentOrDefault(segmentByPrimary, primaryByMac[b])

			_, onPath := myEdges[key]

			edgeByKey[key] = &meshtopov1.MeshEdge{
				FromMac:  a,
				ToMac:    b,
				Metric:   metric,
				Blos:     segA != segB,
				OnMyPath: onPath,
			}
		}
	}

	edges := make([]*meshtopov1.MeshEdge, 0, len(edgeByKey))
	for _, e := range edgeByKey {
		edges = append(edges, e)
	}

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Blos != edges[j].Blos {
			return !edges[i].Blos
		}

		if edges[i].FromMac != edges[j].FromMac {
			return edges[i].FromMac < edges[j].FromMac
		}

		return edges[i].ToMac < edges[j].ToMac
	})

	return edges
}

// hopsUnknown mirrors the sentinel the frontend treats as "no route info".
const hopsUnknown int32 = 99

// canonicalPair returns (min, max) lowercased MAC pair so (a,b) == (b,a)
// when used as a map key.
func canonicalPair(a, b string) (string, string) {
	la := strings.ToLower(a)
	lb := strings.ToLower(b)

	if la < lb {
		return la, lb
	}

	return lb, la
}

// isBetterMetric decides whether a newly-seen metric should replace the
// one currently recorded for an edge. batadv-vis emits inverse-TQ for IV
// (lower is better) and throughput-derived for V (higher is better).
func isBetterMetric(newMetric, oldMetric float64, algorithm int) bool {
	if newMetric == 0 {
		return false
	}

	if oldMetric == 0 {
		return true
	}

	if algorithm == 15 { // BATMAN_V
		return newMetric > oldMetric
	}

	return newMetric < oldMetric // BATMAN_IV (and default)
}

// lookupOrigEntry finds the originator entry whose OrigMAC matches the
// node's primary or any of its secondaries.
func lookupOrigEntry(
	v batmanadv.VisNode,
	origByMac map[string]*batmanadv.OriginatorEntry,
) *batmanadv.OriginatorEntry {
	if e, ok := origByMac[v.Primary]; ok {
		return e
	}

	for _, s := range v.Secondary {
		if e, ok := origByMac[s]; ok {
			return e
		}
	}

	return nil
}

// lookupHostname resolves a display hostname for a node using (in order):
// the originator entry's bat-hosts hostname, a fresh bat-hosts lookup on
// the primary and secondary MACs, or empty. The returned name has any
// "_<iface>" suffix stripped so the UI renders the base hostname.
func lookupHostname(
	v batmanadv.VisNode,
	entry *batmanadv.OriginatorEntry,
	batHosts *batmanadv.BatHosts,
) string {
	if entry != nil && entry.OrigHostname != "" {
		return stripIfaceSuffix(entry.OrigHostname)
	}

	if batHosts == nil {
		return ""
	}

	if name := batHosts.GetHostByMAC(v.Primary); name != "" {
		return stripIfaceSuffix(name)
	}

	for _, s := range v.Secondary {
		if name := batHosts.GetHostByMAC(s); name != "" {
			return stripIfaceSuffix(name)
		}
	}

	return ""
}

// stripIfaceSuffix removes a trailing "_<iface>" token from a bat-hosts
// name, e.g. "BCM2711-97d6_bat0" → "BCM2711-97d6". Names without an
// underscore round-trip unchanged.
func stripIfaceSuffix(full string) string {
	if i := strings.LastIndex(full, "_"); i > 0 {
		return full[:i]
	}

	return full
}

// segmentOrDefault returns the segment label for a primary or "local"
// when the primary is absent from the map. Unknown primaries default to
// local because a vis node we can't classify still has to land somewhere.
func segmentOrDefault(m map[string]string, primary string) string {
	if primary == "" {
		return segmentLocal
	}

	if seg, ok := m[primary]; ok {
		return seg
	}

	return segmentLocal
}

// GetMeshTopologyDelta returns the aggregated churn metrics over the
// requested look-back window.
func (s *MeshTopologyService) GetMeshTopologyDelta(_ context.Context, req *meshtopov1.GetMeshTopologyDeltaRequest) (*meshtopov1.GetMeshTopologyDeltaResponse, error) {
	if s.DeltaTracker == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("mesh topology delta tracker is not running"))
	}

	var window time.Duration

	if req != nil && req.Window != nil {
		window = req.Window.AsDuration()
	}

	result := s.DeltaTracker.Window(window)

	return &meshtopov1.GetMeshTopologyDeltaResponse{
		RoutesAdded:    result.RoutesAdded,
		RoutesLost:     result.RoutesLost,
		GatewayChanges: result.GatewayChanges,
		Reconverge:     durationpb.New(result.Reconverge),
		ActualWindow:   durationpb.New(result.ActualWindow),
	}, nil
}
