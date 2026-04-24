package handlers

import (
	"strings"
	"time"

	netv1 "github.com/openmanet/openmanetd/internal/api/openmanet/network/v1"
	batmanadv "github.com/openmanet/openmanetd/internal/batman-adv"
)

// ageMissing is the sentinel gossipView.ageByPrimary carries when a
// primary has no observed gossip record (or the publisher did not
// timestamp the payload). Surfaces to the proto as gossip_age_seconds=-1.
const ageMissing int32 = -1

// Batman-adv algorithm IDs as carried in MeshNeighbors.algorithm.
// Mirrors the constants in internal/batman-adv/vis.go; duplicated here
// so the gossip metric helper doesn't pull in that package solely for
// two integer constants.
const (
	batmanAlgorithmIV int32 = 4
	batmanAlgorithmV  int32 = 15
)

// gossipView is the handler-local projection of MeshNeighborsProvider
// data: per-primary RF and BLOS neighbor adjacency (carrying the
// publisher-reported metric for each edge), plus a per-primary
// staleness bool so the renderer can dim nodes whose publishers have
// gone quiet. Every lookup key is a lowercased canonical primary MAC.
//
// Values in rfByPrimary/blosByPrimary are the edge metric as the UI
// consumes it: throughput in Mbps on BATMAN_V, 255/TQ on BATMAN_IV, 0
// when the publisher did not report a metric. Callers that only care
// about adjacency iterate the map keys and ignore the value.
type gossipView struct {
	rfByPrimary    map[string]map[string]float64
	blosByPrimary  map[string]map[string]float64
	staleByPrimary map[string]bool
	// ageByPrimary is the per-primary age (whole seconds) of the most
	// recent gossip record, measured as (now - payload.collected_at).
	// ageMissing (-1) when the primary has no record or the publisher
	// omitted its collected_at timestamp.
	ageByPrimary map[string]int32
	// originatorsByPrimary is the publisher's own best-route tree, used
	// for multi-hop vxlan0 chain resolution when a remote component has
	// no direct BLOS edge to the local component.
	originatorsByPrimary map[string][]gossipOriginator
	// coverage counts primaries whose records are present and fresh.
	coverage int
}

// gossipNeighborMetric converts a MeshNeighbor's publisher-reported
// link quality into the float64 edge metric the UI consumes. BATMAN_V
// is throughput-derived (Mbps, higher is better); BATMAN_IV is 255/TQ
// (lower is better). Zero signals "no metric reported" and propagates
// through addEdge's upgrade-from-zero rule unchanged.
func gossipNeighborMetric(n *netv1.MeshNeighbor, algorithm int32) float64 {
	if n == nil {
		return 0
	}

	switch algorithm {
	case batmanAlgorithmV:
		// throughput_kbps → Mbps; qualityClass thresholds are in Mbps.
		if kbps := n.GetThroughputKbps(); kbps > 0 {
			return float64(kbps) / 1000.0
		}
	case batmanAlgorithmIV:
		// 255/TQ matches ParseMetric's handling of the vis string field.
		if tq := n.GetTq(); tq > 0 {
			return 255.0 / float64(tq)
		}
	}

	return 0
}

// gossipOriginator is the minimum subset of the proto Originator that
// the handler actually consumes for chain resolution.
type gossipOriginator struct {
	orig    string
	nextHop string
	ifname  string
}

// buildGossipView projects a MeshNeighborsProvider into the handler's
// working representation. Returns a non-nil (empty) view even when the
// provider is nil so downstream code doesn't need nil-checks.
//
// hostnameByPrimary maps each vis primary to its display hostname and
// is used as a fallback join key: on multi-mesh deployments the
// publisher's alfred envelope MAC and payload.primary_mac both reflect
// a batman-adv instance different from the one vis reports, so only
// the hostname matches across the two address spaces.
//
// now is used to compute per-primary gossip ages against each record's
// payload.collected_at timestamp. Callers in production pass time.Now();
// tests inject a fixed clock for deterministic age assertions.
func buildGossipView(
	provider batmanadv.MeshNeighborsProvider,
	visNodes []batmanadv.VisNode,
	primaryByMac map[string]string,
	hostnameByPrimary map[string]string,
	now time.Time,
) *gossipView {
	view := &gossipView{
		rfByPrimary:          make(map[string]map[string]float64),
		blosByPrimary:        make(map[string]map[string]float64),
		staleByPrimary:       make(map[string]bool),
		ageByPrimary:         make(map[string]int32),
		originatorsByPrimary: make(map[string][]gossipOriginator),
	}

	if provider == nil {
		return view
	}

	for _, v := range visNodes {
		primary := resolvePrimary(primaryByMac, v.Primary)
		if primary == "" {
			continue
		}

		rec, ok := provider.Lookup(primary)
		if !ok || rec == nil || rec.Payload == nil {
			// Multi-mesh fallback: the publisher's addressing doesn't
			// share a MAC with the vis primary, but both sides agree
			// on the display hostname (bat-hosts + os.Hostname).
			if hostname := hostnameByPrimary[primary]; hostname != "" {
				if hrec, hok := provider.LookupByHostname(hostname); hok {
					rec = hrec
					ok = true
				}
			}
		}

		if !ok || rec == nil || rec.Payload == nil {
			view.staleByPrimary[primary] = true
			view.ageByPrimary[primary] = gossipRecordAge(rec, now)

			continue
		}

		view.coverage++
		view.ageByPrimary[primary] = gossipRecordAge(rec, now)
		rfSet, blosSet := classifyGossipNeighbors(rec.Payload, primaryByMac)
		view.rfByPrimary[primary] = rfSet
		view.blosByPrimary[primary] = blosSet

		origs := make([]gossipOriginator, 0, len(rec.Payload.GetOriginators()))
		for _, o := range rec.Payload.GetOriginators() {
			origs = append(origs, gossipOriginator{
				orig:    resolvePrimary(primaryByMac, strings.ToLower(o.GetOrigMac())),
				nextHop: resolvePrimary(primaryByMac, strings.ToLower(o.GetNextHopMac())),
				ifname:  o.GetHardIfname(),
			})
		}

		view.originatorsByPrimary[primary] = origs
	}

	return view
}

// classifyGossipNeighbors splits a publisher's MeshNeighbors payload
// into RF and BLOS adjacency maps keyed by primary MAC, each value
// carrying the edge metric the UI will display. Extracted out of
// buildGossipView so the surrounding loop stays under the gocognit
// threshold; it is never called elsewhere.
func classifyGossipNeighbors(
	payload *netv1.MeshNeighbors,
	primaryByMac map[string]string,
) (map[string]float64, map[string]float64) {
	rfSet := make(map[string]float64)
	blosSet := make(map[string]float64)

	if payload == nil {
		return rfSet, blosSet
	}

	algorithm := payload.GetAlgorithm()

	for _, n := range payload.GetNeighbors() {
		nmac := resolvePrimary(primaryByMac, strings.ToLower(n.GetMac()))
		if nmac == "" {
			continue
		}

		metric := gossipNeighborMetric(n, algorithm)
		target := rfSet

		if n.GetBlos() || n.GetHardIfname() == blosIfname {
			target = blosSet
		} else if n.GetHardIfname() == "" {
			continue // ignore entries that can't be classified at all
		}

		// Keep the better of two reports of the same neighbor: either
		// its first appearance or the first one that actually carried a
		// metric. Avoids a perfect-TQ entry being overwritten by a
		// later zero-metric duplicate.
		if existing, seen := target[nmac]; !seen || (metric > 0 && existing == 0) {
			target[nmac] = metric
		}
	}

	return rfSet, blosSet
}

// gossipRecordAge returns the age of a gossip record's payload in whole
// seconds against the serving node's "now". Returns ageMissing when the
// record is absent, its payload is nil, or the publisher omitted the
// collected_at timestamp. Negative raw deltas (publisher clock ahead of
// the serving node) round to 0 rather than ageMissing — a real-but-tiny
// age is more useful than a "no data" marker.
func gossipRecordAge(rec *batmanadv.MeshNeighborsRecord, now time.Time) int32 {
	if rec == nil || rec.Payload == nil {
		return ageMissing
	}

	ts := rec.Payload.GetCollectedAt()
	if ts == nil {
		return ageMissing
	}

	collected := ts.AsTime()
	if collected.IsZero() {
		return ageMissing
	}

	seconds := int64(now.Sub(collected).Seconds())
	if seconds < 0 {
		return 0
	}

	return int32(seconds)
}

// classifyNodesWithGossip replaces the heuristic segment/gateway pass
// when gossip coverage is non-zero. Algorithm:
//
//  1. Build RF adjacency from gossip (either endpoint's RF mention
//     counts as an RF edge).
//  2. Find connected components by BFS.
//  3. Component containing self → local; all others → remote.
//  4. For each remote component, pick a gateway: a member with a BLOS
//     edge to any local-component member. Fall back to multi-hop chain
//     resolution using the component member's own originators[] when
//     no direct BLOS edge exists.
//  5. Fall through to classifyNodesHeuristic for any primary that
//     gossip did not cover (stale or missing publisher).
func classifyNodesWithGossip(
	origSnap *batmanadv.OriginatorTopology,
	primaryByMac map[string]string,
	selfMAC string,
	selfPrimary string,
	gossip *gossipView,
) (map[string]string, map[string]string) {
	if gossip == nil || gossip.coverage == 0 {
		return classifyNodesHeuristic(origSnap, primaryByMac, selfMAC)
	}

	rfAdj := buildRFAdjacency(gossip)
	componentByPrimary, componentMembers := connectedComponents(rfAdj, gossip)

	selfCid, selfHasComponent := componentByPrimary[selfPrimary]

	segments := make(map[string]string)
	gateways := make(map[string]string)

	for primary := range gossip.rfByPrimary {
		cid := componentByPrimary[primary]
		if selfHasComponent && cid == selfCid {
			segments[primary] = segmentLocal
		} else {
			segments[primary] = segmentRemote
		}
	}

	if selfPrimary != "" {
		segments[selfPrimary] = segmentLocal
	}

	localMembers := localComponentSet(componentMembers, selfCid, selfHasComponent, selfPrimary)

	for cid, members := range componentMembers {
		if selfHasComponent && cid == selfCid {
			continue
		}

		gateway := pickComponentGateway(members, localMembers, gossip)
		if gateway == "" {
			continue
		}

		for _, m := range members {
			gateways[m] = gateway
		}
	}

	fallbackSegs, fallbackGws := classifyNodesHeuristic(origSnap, primaryByMac, selfMAC)

	for p, seg := range fallbackSegs {
		if _, ok := segments[p]; !ok {
			segments[p] = seg
		}
	}

	for p, gw := range fallbackGws {
		if _, ok := gateways[p]; !ok {
			gateways[p] = gw
		}
	}

	return segments, gateways
}

// buildRFAdjacency folds both endpoints' RF neighbor sets so either
// side's claim is enough to link the two primaries in the RF graph.
// Any pair we connect this way is guaranteed to be RF-adjacent in the
// real physical mesh.
func buildRFAdjacency(gossip *gossipView) map[string]map[string]struct{} {
	adj := make(map[string]map[string]struct{})

	addEdge := func(a, b string) {
		if a == b || a == "" || b == "" {
			return
		}

		if adj[a] == nil {
			adj[a] = make(map[string]struct{})
		}

		if adj[b] == nil {
			adj[b] = make(map[string]struct{})
		}

		adj[a][b] = struct{}{}
		adj[b][a] = struct{}{}
	}

	for primary, rfSet := range gossip.rfByPrimary {
		for nb := range rfSet {
			addEdge(primary, nb)
		}
	}

	return adj
}

// connectedComponents runs BFS over an adjacency map and returns a
// component-id-per-primary map plus the list of members per component.
// Primaries with no RF neighbors form singleton components.
func connectedComponents(
	adj map[string]map[string]struct{},
	gossip *gossipView,
) (map[string]int, [][]string) {
	componentByPrimary := make(map[string]int)

	var componentMembers [][]string

	start := func(seed string) {
		if _, seen := componentByPrimary[seed]; seen {
			return
		}

		cid := len(componentMembers)

		var members []string

		queue := []string{seed}
		componentByPrimary[seed] = cid

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]

			members = append(members, cur)

			for nb := range adj[cur] {
				if _, seen := componentByPrimary[nb]; seen {
					continue
				}

				componentByPrimary[nb] = cid

				queue = append(queue, nb)
			}
		}

		componentMembers = append(componentMembers, members)
	}

	// Seed from every primary gossip has a record for. Primaries with
	// no RF neighbors become their own component — that's correct for
	// a solo node attached via vxlan0 to some mesh.
	for primary := range gossip.rfByPrimary {
		start(primary)
	}

	return componentByPrimary, componentMembers
}

// localComponentSet materializes the set of primaries in the local
// mesh component, falling back to "self alone" when gossip hasn't
// placed self in any component yet (bootstrap / single-node case).
func localComponentSet(
	componentMembers [][]string,
	selfCid int,
	selfHasComponent bool,
	selfPrimary string,
) map[string]struct{} {
	local := make(map[string]struct{})

	if selfHasComponent && selfCid < len(componentMembers) {
		for _, m := range componentMembers[selfCid] {
			local[m] = struct{}{}
		}
	}

	if selfPrimary != "" {
		local[selfPrimary] = struct{}{}
	}

	return local
}

// pickComponentGateway returns the primary within `members` that has
// the shortest vxlan0 path to any local-component member.
//
// Priority:
//  1. Direct BLOS edge from a member to a local-component peer.
//  2. Multi-hop resolution using the member's gossip originators[]:
//     walk next-hop entries whose ifname is vxlan0 until a local-component
//     primary is reached, capped at 16 hops.
//  3. If neither, return the first member with ANY BLOS neighbor so
//     the UI still groups the component under some gateway rather
//     than scattering it.
func pickComponentGateway(
	members []string,
	localMembers map[string]struct{},
	gossip *gossipView,
) string {
	// 1. Direct BLOS edge.
	for _, m := range members {
		for nb := range gossip.blosByPrimary[m] {
			if _, ok := localMembers[nb]; ok {
				return m
			}
		}
	}

	// 2. Multi-hop chain via gossip originators.
	for _, m := range members {
		if reachableViaBLOSChain(m, localMembers, gossip) {
			return m
		}
	}

	// 3. Fallback: any member with any BLOS edge.
	for _, m := range members {
		if len(gossip.blosByPrimary[m]) > 0 {
			return m
		}
	}

	return ""
}

// reachableViaBLOSChain walks `start`'s originators[] following
// vxlan0-ifname routes until it lands on a local-component member or
// the hop cap is reached. Cycle-safe via a visited-set.
func reachableViaBLOSChain(
	start string,
	localMembers map[string]struct{},
	gossip *gossipView,
) bool {
	const hopsMaxWalk = 16

	visited := map[string]struct{}{start: {}}
	frontier := []string{start}

	for depth := 0; depth < hopsMaxWalk && len(frontier) > 0; depth++ {
		var next []string

		for _, cur := range frontier {
			for _, o := range gossip.originatorsByPrimary[cur] {
				if o.ifname != blosIfname {
					continue
				}

				if o.orig == "" || o.nextHop == "" {
					continue
				}

				candidate := o.nextHop
				if _, ok := localMembers[candidate]; ok {
					return true
				}

				if _, seen := visited[candidate]; seen {
					continue
				}

				visited[candidate] = struct{}{}
				next = append(next, candidate)
			}
		}

		frontier = next
	}

	return false
}
