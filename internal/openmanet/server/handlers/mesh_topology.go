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
	Log               zerolog.Logger
	VisProvider       batmanadv.VisProvider
	OrigProvider      batmanadv.OriginatorTopologyProvider
	NeighborsProvider batmanadv.MeshNeighborsProvider
	DeltaTracker      *DeltaTracker
	ParseBatHosts     func(string) (*batmanadv.BatHosts, error)
	Now               func() time.Time
	BatHostsPath      string
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

	merged := mergeMeshTopology(visDoc, origSnap, batHosts, s.NeighborsProvider)
	merged.CollectedAt = timestamppb.New(s.now())

	return &meshtopov1.GetMeshTopologyResponse{Topology: merged}, nil
}

// mergeMeshTopology transforms vis + originator + bat-hosts inputs into
// the wire-shape MeshTopology. Factored out for test readability.
//
// Edges are always derived from the serving node's originator table so
// the UI renders connectivity even when batadv-vis returns a bare node
// list (alfred reachable, peer vis-servers silent). Vis-neighbor edges
// are layered on top for links that don't touch the serving node.
func mergeMeshTopology(
	visDoc *batmanadv.VisDoc,
	origSnap *batmanadv.OriginatorTopology,
	batHosts *batmanadv.BatHosts,
	neighborsProvider batmanadv.MeshNeighborsProvider,
) *meshtopov1.MeshTopology {
	// 1. MAC → primary index for every vis node (primary + secondaries
	//    all map to the same primary). Originator-only nodes that vis
	//    hasn't heard about yet get added in buildMeshNodes.
	visNodes := []batmanadv.VisNode(nil)
	if visDoc != nil {
		visNodes = visDoc.Vis
	}

	// secondaryOwners captures "MAC X is listed as a secondary of entry Y".
	// When a different entry also exists with Primary=X, those two entries
	// describe the same physical node (multi-radio host) and X's entry is
	// the duplicate. Without this dedup, a node reported under both roles
	// renders twice — the self-twice bug in practice.
	secondaryOwners := make(map[string]string)

	for _, v := range visNodes {
		for _, sec := range v.Secondary {
			secondaryOwners[sec] = v.Primary
		}
	}

	primaryByMac := make(map[string]string, len(visNodes)*2)
	for _, v := range visNodes {
		canonical := v.Primary
		if owner, ok := secondaryOwners[canonical]; ok && owner != "" {
			canonical = owner
		}

		primaryByMac[v.Primary] = canonical
		for _, sec := range v.Secondary {
			primaryByMac[sec] = canonical
		}
	}

	// Ensure every originator MAC resolves to some primary so segment
	// classification captures peers that appear in our route table but
	// haven't surfaced in vis yet.
	selfMAC := strings.ToLower(origSnap.SelfMAC)
	if selfMAC != "" && primaryByMac[selfMAC] == "" {
		primaryByMac[selfMAC] = selfMAC
	}

	for _, o := range origSnap.Originators {
		mac := strings.ToLower(o.OrigMAC)
		if mac != "" && primaryByMac[mac] == "" {
			primaryByMac[mac] = mac
		}
	}

	// Originator lookup built early so the hostname dedup pass below can
	// resolve OrigHostname without scanning the slice per-node.
	origByMac := make(map[string]*batmanadv.OriginatorEntry, len(origSnap.Originators))
	for i := range origSnap.Originators {
		origByMac[strings.ToLower(origSnap.Originators[i].OrigMAC)] = &origSnap.Originators[i]
	}

	// Multi-radio dedup: fold vis entries sharing a base hostname into a
	// single canonical primary. The `secondary[]` field alone can't catch
	// this when a node publishes a separate vis entry per interface
	// without cross-listing the sibling MACs.
	foldHostnameAliases(visNodes, primaryByMac, origByMac, batHosts, selfMAC, origSnap.SelfHostname)

	selfPrimary := primaryByMac[selfMAC]

	// 2. Build gossip view if a provider is wired. When gossip covers any
	// primary its RF/BLOS neighbor sets drive segment assignment and
	// gateway identification; primaries without coverage fall through to
	// the heuristic classifier.
	gossip := buildGossipView(neighborsProvider, visNodes, primaryByMac)

	segmentByPrimary, gatewayByPrimary := classifyNodesWithGossip(
		origSnap, primaryByMac, selfMAC, selfPrimary, gossip)
	if selfPrimary != "" {
		segmentByPrimary[selfPrimary] = segmentLocal // self is always local
		delete(gatewayByPrimary, selfPrimary)
	}

	// 4. Build MeshNode list.
	nodes := buildMeshNodes(visNodes, origByMac, batHosts, segmentByPrimary, gatewayByPrimary, selfPrimary, origSnap.SelfHostname, primaryByMac)

	// 4a. Mark gossip-stale nodes so the UI can dim them.
	applyGossipStale(nodes, gossip)

	// 5. Build MeshEdge list — originator-derived first, then vis neighbors.
	edges := buildMeshEdges(visDoc, origSnap.Originators, primaryByMac, segmentByPrimary, selfMAC, renderedPrimaries(nodes))

	return &meshtopov1.MeshTopology{
		SelfMac:        origSnap.SelfMAC,
		SelfHostname:   origSnap.SelfHostname,
		Algorithm:      origSnap.Algorithm, // always the batman-adv algorithm, never the vis header
		Nodes:          nodes,
		Edges:          edges,
		GossipCoverage: gossipCoverage(gossip, nodes),
	}
}

// applyGossipStale sets the GossipStale bool on every MeshNode based on
// the pre-computed gossipView. Self is never considered stale.
func applyGossipStale(nodes []*meshtopov1.MeshNode, gossip *gossipView) {
	if gossip == nil {
		return
	}

	for _, n := range nodes {
		if n.GetIsSelf() {
			continue
		}

		primary := strings.ToLower(n.GetMac())
		if gossip.staleByPrimary[primary] {
			n.GossipStale = true
		}
	}
}

// gossipCoverage summarizes how many rendered non-self nodes have fresh
// gossip records. Emitted into the MeshTopology response so the frontend
// can render a "GOSSIP N/M" badge.
func gossipCoverage(gossip *gossipView, nodes []*meshtopov1.MeshNode) *meshtopov1.GossipCoverage {
	if gossip == nil {
		return nil
	}

	published := 0
	total := 0

	for _, n := range nodes {
		if n.GetIsSelf() {
			continue
		}

		total++

		primary := strings.ToLower(n.GetMac())
		if !gossip.staleByPrimary[primary] {
			published++
		}
	}

	return &meshtopov1.GossipCoverage{
		Published: int32(published), //nolint:gosec // bounded by mesh size
		Total:     int32(total),     //nolint:gosec // bounded by mesh size
	}
}

// renderedPrimaries returns the set of primary MACs we emitted in the
// node list so edge-building can drop references to MACs without a
// rendered node (e.g. an originator's next hop that vis never mentioned
// and that we also couldn't synthesize).
func renderedPrimaries(nodes []*meshtopov1.MeshNode) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		out[strings.ToLower(n.Mac)] = struct{}{}
	}

	return out
}

// foldHostnameAliases extends primaryByMac so that vis entries sharing a
// base hostname collapse to one canonical primary. Handles multi-radio
// nodes where each interface publishes its own vis entry without
// cross-listing the sibling MACs as secondaries — without this, the
// same physical node renders once per interface.
//
// Canonical selection prefers selfMAC when present in the group so the
// IsSelf flag lands on the retained node; otherwise the lexicographically
// smallest primary wins for determinism. Secondaries of any folded
// primary are rewritten to point at the canonical as well, so edges
// whose endpoints reference those secondaries still resolve correctly.
func foldHostnameAliases(
	visNodes []batmanadv.VisNode,
	primaryByMac map[string]string,
	origByMac map[string]*batmanadv.OriginatorEntry,
	batHosts *batmanadv.BatHosts,
	selfMAC, selfHostname string,
) {
	if len(visNodes) < 2 {
		return
	}

	groups := groupPrimariesByHostname(visNodes, primaryByMac, origByMac, batHosts, selfMAC, selfHostname)

	for _, group := range groups {
		if len(group) < 2 {
			continue
		}

		canonical := pickCanonicalPrimary(group, selfMAC)
		applyCanonicalPrimary(visNodes, primaryByMac, group, canonical)
	}
}

// groupPrimariesByHostname builds the base-hostname → primary-MAC groups
// used by foldHostnameAliases. Entries already aliased by secondary-MAC
// dedup are skipped so we don't re-group shadow primaries.
func groupPrimariesByHostname(
	visNodes []batmanadv.VisNode,
	primaryByMac map[string]string,
	origByMac map[string]*batmanadv.OriginatorEntry,
	batHosts *batmanadv.BatHosts,
	selfMAC, selfHostname string,
) map[string][]string {
	hostnameByPrimary := make(map[string]string, len(visNodes))

	for _, v := range visNodes {
		if c, ok := primaryByMac[v.Primary]; ok && c != v.Primary {
			continue
		}

		name := lookupHostname(v, lookupOrigEntry(v, origByMac), batHosts)
		if name != "" {
			hostnameByPrimary[v.Primary] = name
		}
	}

	if selfMAC != "" && selfHostname != "" {
		hostnameByPrimary[selfMAC] = stripIfaceSuffix(selfHostname)
	}

	groups := make(map[string][]string)
	for primary, hostname := range hostnameByPrimary {
		groups[hostname] = append(groups[hostname], primary)
	}

	return groups
}

// pickCanonicalPrimary selects the MAC that survives hostname dedup:
// selfMAC wins when present so IsSelf lands on the retained node;
// otherwise the lexicographically smallest MAC wins for determinism.
func pickCanonicalPrimary(group []string, selfMAC string) string {
	canonical := group[0]

	for _, p := range group {
		switch {
		case p == selfMAC:
			return p
		case canonical != selfMAC && p < canonical:
			canonical = p
		}
	}

	return canonical
}

// applyCanonicalPrimary rewrites primaryByMac so every non-canonical
// primary in the group (and its declared secondaries) points at the
// chosen canonical primary.
func applyCanonicalPrimary(
	visNodes []batmanadv.VisNode,
	primaryByMac map[string]string,
	group []string,
	canonical string,
) {
	for _, p := range group {
		if p == canonical {
			continue
		}

		primaryByMac[p] = canonical

		for _, v := range visNodes {
			if v.Primary != p {
				continue
			}

			for _, sec := range v.Secondary {
				primaryByMac[sec] = canonical
			}
		}
	}
}

// classifyNodesHeuristic assigns each primary to a segment and — for
// remote-segment nodes — resolves the direct BLOS neighbor (the
// "gateway") on the other end of the vxlan0 tunnel using only the
// serving node's own originator table. This is the fallback
// classifier: it runs for every primary when gossip is unavailable,
// and for individual primaries whose gossip record is missing or
// stale when gossip is partially available.
//
// Rule: any vxlan0 route → remote unless the same primary also has a
// non-vxlan0 route (RF wins). Primaries absent from the originator table
// default to local with no gateway.
func classifyNodesHeuristic(
	origSnap *batmanadv.OriginatorTopology,
	primaryByMac map[string]string,
	selfMAC string,
) (map[string]string, map[string]string) {
	// Collect hard_ifnames each primary is reachable over.
	ifsByPrimary := make(map[string]map[string]struct{})
	// Track each primary's vxlan0 originator entry (for gateway chain walk).
	blosEntryByPrimary := make(map[string]batmanadv.OriginatorEntry)

	for _, o := range origSnap.Originators {
		primary := primaryByMac[strings.ToLower(o.OrigMAC)]
		if primary == "" {
			continue
		}

		ifs, ok := ifsByPrimary[primary]
		if !ok {
			ifs = make(map[string]struct{})
			ifsByPrimary[primary] = ifs
		}

		ifs[o.HardIfname] = struct{}{}

		if o.HardIfname == blosIfname {
			blosEntryByPrimary[primary] = o
		}
	}

	segments := make(map[string]string, len(ifsByPrimary))
	gateways := make(map[string]string, len(blosEntryByPrimary))

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
			segments[primary] = segmentLocal
		case hasBLOS:
			segments[primary] = segmentRemote

			gw := resolveBLOSGateway(primary, blosEntryByPrimary, primaryByMac, selfMAC)
			if gw != "" {
				gateways[primary] = gw
			}
		default:
			segments[primary] = segmentLocal
		}
	}

	return segments, gateways
}

// resolveBLOSGateway walks a BLOS-reached primary's next-hop chain until
// it hits a direct neighbor (hopsRemaining == 0 or nextHop == primary).
// That direct neighbor's primary MAC identifies the remote mesh segment.
// Cycle-safe, capped at hopsMaxWalk steps.
func resolveBLOSGateway(
	primary string,
	blosByPrimary map[string]batmanadv.OriginatorEntry,
	primaryByMac map[string]string,
	selfMAC string,
) string {
	const hopsMaxWalk = 16

	cursor := primary

	for i := 0; i < hopsMaxWalk; i++ {
		entry, ok := blosByPrimary[cursor]
		if !ok {
			return cursor
		}

		nextMAC := strings.ToLower(entry.NextHopMAC)
		origMAC := strings.ToLower(entry.OrigMAC)

		// Direct neighbor: we reach cursor in one hop via vxlan0. cursor
		// itself is the gateway for its remote mesh segment.
		if nextMAC == "" || nextMAC == origMAC || nextMAC == selfMAC {
			return cursor
		}

		nextPrimary := primaryByMac[nextMAC]
		if nextPrimary == "" || nextPrimary == cursor {
			return cursor
		}

		cursor = nextPrimary
	}

	return cursor
}

// buildMeshNodes materializes MeshNode records from the vis payload, and
// synthesizes nodes for originators vis hasn't surfaced yet so the UI has
// a record for every peer we know how to route to. Overlay data comes
// from the originator table; hostnames from bat-hosts.
func buildMeshNodes(
	visNodes []batmanadv.VisNode,
	origByMac map[string]*batmanadv.OriginatorEntry,
	batHosts *batmanadv.BatHosts,
	segmentByPrimary map[string]string,
	gatewayByPrimary map[string]string,
	selfPrimary string,
	selfHostname string,
	primaryByMac map[string]string,
) []*meshtopov1.MeshNode {
	nodes := make([]*meshtopov1.MeshNode, 0, len(visNodes)+len(origByMac)+1)
	seenPrimary := make(map[string]struct{}, len(visNodes))

	for _, v := range visNodes {
		// Skip vis entries whose canonical primary lives elsewhere.
		// Happens when a multi-radio node is republished under a
		// secondary MAC as its "primary" in a separate entry —
		// without this guard the node renders twice (canonical +
		// shadow). Neighbor info from the shadow entry still flows
		// via layerVisEdges since it resolves MACs through
		// primaryByMac.
		if canonical, ok := primaryByMac[v.Primary]; ok && canonical != v.Primary {
			continue
		}

		node := &meshtopov1.MeshNode{
			Mac:           v.Primary,
			SecondaryMacs: append([]string(nil), v.Secondary...),
			ClientCount:   int32(len(v.Clients)),
			IsSelf:        v.Primary == selfPrimary && selfPrimary != "",
		}

		applySegment(node, segmentByPrimary, gatewayByPrimary, v.Primary)
		applyOverlay(node, lookupOrigEntry(v, origByMac))

		if node.IsSelf {
			node.Hostname = stripIfaceSuffix(selfHostname)
		} else {
			node.Hostname = lookupHostname(v, lookupOrigEntry(v, origByMac), batHosts)
		}

		nodes = append(nodes, node)
		seenPrimary[v.Primary] = struct{}{}
	}

	// Synthesize nodes for originators vis didn't mention yet. Without
	// this, the "vis returned a sparse doc" case drops peers we can still
	// route to from the rendered graph.
	for mac, entry := range origByMac {
		primary := primaryByMac[mac]
		if primary == "" {
			primary = mac
		}

		if _, ok := seenPrimary[primary]; ok {
			continue
		}

		node := &meshtopov1.MeshNode{
			Mac:    primary,
			IsSelf: primary == selfPrimary && selfPrimary != "",
		}

		applySegment(node, segmentByPrimary, gatewayByPrimary, primary)
		applyOverlay(node, entry)
		node.Hostname = stripIfaceSuffix(entry.OrigHostname)

		nodes = append(nodes, node)
		seenPrimary[primary] = struct{}{}
	}

	// Self may be absent from both vis and the originator table during
	// cold-start; add a stub so the UI still has a node to root on.
	if selfPrimary != "" {
		if _, ok := seenPrimary[selfPrimary]; !ok {
			nodes = append(nodes, &meshtopov1.MeshNode{
				Mac:          selfPrimary,
				Hostname:     stripIfaceSuffix(selfHostname),
				Segment:      segmentLocal,
				IsSelf:       true,
				HopsFromSelf: 0,
			})
		}
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

// applySegment copies segment + gateway fields onto a MeshNode.
func applySegment(
	node *meshtopov1.MeshNode,
	segmentByPrimary map[string]string,
	gatewayByPrimary map[string]string,
	primary string,
) {
	if seg, ok := segmentByPrimary[primary]; ok {
		node.Segment = seg
	} else {
		node.Segment = segmentLocal
	}

	if gw, ok := gatewayByPrimary[primary]; ok && node.Segment == segmentRemote {
		node.RemoteGatewayMac = gw
	}
}

// applyOverlay populates HopsFromSelf + MyHardIfname from an originator
// entry, respecting the self-node short-circuit.
func applyOverlay(node *meshtopov1.MeshNode, entry *batmanadv.OriginatorEntry) {
	switch {
	case node.IsSelf:
		node.HopsFromSelf = 0
	case entry != nil:
		node.HopsFromSelf = int32(entry.Hops)
		node.MyHardIfname = entry.HardIfname
	default:
		node.HopsFromSelf = hopsUnknown
	}
}

// buildMeshEdges derives the edge list in two passes:
//
//  1. Seed from the serving node's originator table so every peer we
//     route to is visually connected, even when batadv-vis returns a
//     bare node list with no neighbor entries. These edges are all on
//     my forwarding path by definition.
//  2. Layer in vis-reported neighbor edges for links that don't touch
//     the serving node (peer-to-peer connectivity we learn from other
//     nodes' publications). Bidirectional reports dedupe by canonical
//     MAC pair.
//
// Edges whose endpoints map to nodes we didn't render are dropped so
// the frontend never receives dangling references.
func buildMeshEdges(
	visDoc *batmanadv.VisDoc,
	origs []batmanadv.OriginatorEntry,
	primaryByMac map[string]string,
	segmentByPrimary map[string]string,
	selfMAC string,
	knownPrimaries map[string]struct{},
) []*meshtopov1.MeshEdge {
	edgeByKey := make(map[string]*meshtopov1.MeshEdge)

	seedOriginatorEdges(edgeByKey, origs, primaryByMac, segmentByPrimary, knownPrimaries, selfMAC)
	layerVisEdges(edgeByKey, visDoc, primaryByMac, segmentByPrimary, knownPrimaries)

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

// seedOriginatorEdges populates edgeByKey with the serving node's
// forwarding tree so every peer we route to gets at least one visual
// edge, even when batadv-vis reports no neighbors.
func seedOriginatorEdges(
	edgeByKey map[string]*meshtopov1.MeshEdge,
	origs []batmanadv.OriginatorEntry,
	primaryByMac map[string]string,
	segmentByPrimary map[string]string,
	knownPrimaries map[string]struct{},
	selfMAC string,
) {
	for _, o := range origs {
		fromPrimary, toPrimary, ok := originatorEdgeEndpoints(o, primaryByMac, selfMAC)
		if !ok {
			continue
		}

		addEdge(edgeByKey, fromPrimary, toPrimary, segmentByPrimary, knownPrimaries, 0, true)
	}
}

// originatorEdgeEndpoints projects an originator entry to a canonical
// (from, to) primary pair. Direct neighbors anchor on selfMAC; multi-hop
// entries anchor on the next-hop → origin leg. Returns ok=false when
// the entry can't produce a valid edge (e.g. self-loop, missing data).
func originatorEdgeEndpoints(
	o batmanadv.OriginatorEntry,
	primaryByMac map[string]string,
	selfMAC string,
) (string, string, bool) {
	origMAC := strings.ToLower(o.OrigMAC)
	if origMAC == "" {
		return "", "", false
	}

	nextMAC := strings.ToLower(o.NextHopMAC)

	var fromMAC, toMAC string

	if nextMAC == "" || nextMAC == origMAC {
		if selfMAC == "" || selfMAC == origMAC {
			return "", "", false
		}

		fromMAC, toMAC = selfMAC, origMAC
	} else {
		fromMAC, toMAC = nextMAC, origMAC
	}

	return resolvePrimary(primaryByMac, fromMAC), resolvePrimary(primaryByMac, toMAC), true
}

// layerVisEdges merges vis-reported neighbor entries over the originator
// seeds. Existing edges get their metric enriched; new pairs get added
// as off-my-path edges.
func layerVisEdges(
	edgeByKey map[string]*meshtopov1.MeshEdge,
	visDoc *batmanadv.VisDoc,
	primaryByMac map[string]string,
	segmentByPrimary map[string]string,
	knownPrimaries map[string]struct{},
) {
	if visDoc == nil {
		return
	}

	for _, v := range visDoc.Vis {
		for _, n := range v.Neighbors {
			applyVisNeighbor(edgeByKey, n, visDoc.Algorithm, primaryByMac, segmentByPrimary, knownPrimaries)
		}
	}
}

func applyVisNeighbor(
	edgeByKey map[string]*meshtopov1.MeshEdge,
	n batmanadv.VisNeighbor,
	algorithm int,
	primaryByMac map[string]string,
	segmentByPrimary map[string]string,
	knownPrimaries map[string]struct{},
) {
	aMAC := strings.ToLower(n.Router)

	bMAC := strings.ToLower(n.Neighbor)
	if aMAC == "" || bMAC == "" || aMAC == bMAC {
		return
	}

	aPrimary := resolvePrimary(primaryByMac, aMAC)
	bPrimary := resolvePrimary(primaryByMac, bMAC)

	metric := batmanadv.ParseMetric(n.Metric)

	if existing, ok := edgeByKey[canonicalKey(aPrimary, bPrimary)]; ok {
		if isBetterMetric(metric, existing.Metric, algorithm) {
			existing.Metric = metric
		}

		return
	}

	addEdge(edgeByKey, aPrimary, bPrimary, segmentByPrimary, knownPrimaries, metric, false)
}

// resolvePrimary looks up a MAC's primary, falling back to the MAC itself
// when no mapping exists (keeps callers tidy vs inlining the check).
func resolvePrimary(primaryByMac map[string]string, mac string) string {
	if p, ok := primaryByMac[mac]; ok && p != "" {
		return p
	}

	return mac
}

// canonicalKey returns a stable "from|to" key for an edge, ordered by
// lowercase MAC so (a,b) == (b,a).
func canonicalKey(a, b string) string {
	la, lb := canonicalPair(a, b)

	return la + "|" + lb
}

// addEdge inserts or upgrades an edge record keyed on the canonical MAC
// pair. Drops edges whose endpoints aren't both in the rendered-node
// set so the frontend never sees dangling endpoints.
func addEdge(
	edgeByKey map[string]*meshtopov1.MeshEdge,
	a, b string,
	segmentByPrimary map[string]string,
	knownPrimaries map[string]struct{},
	metric float64,
	onMyPath bool,
) {
	la, lb := canonicalPair(a, b)
	if la == lb {
		return
	}

	if _, ok := knownPrimaries[la]; !ok {
		return
	}

	if _, ok := knownPrimaries[lb]; !ok {
		return
	}

	key := la + "|" + lb

	if existing, ok := edgeByKey[key]; ok {
		if onMyPath {
			existing.OnMyPath = true
		}

		if metric != 0 && existing.Metric == 0 {
			existing.Metric = metric
		}

		return
	}

	segA := segmentOrDefault(segmentByPrimary, la)
	segB := segmentOrDefault(segmentByPrimary, lb)

	edgeByKey[key] = &meshtopov1.MeshEdge{
		FromMac:  la,
		ToMac:    lb,
		Metric:   metric,
		Blos:     segA != segB,
		OnMyPath: onMyPath,
	}
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
