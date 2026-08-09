package batmanadv

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// ErrOriginatorsUnavailable is returned when batctl exits non-zero — typically
// because the bat0 interface does not exist (batman-adv module not loaded or
// the daemon hasn't provisioned the mesh yet). Callers can map this to a
// user-facing "mesh unavailable" state distinct from internal errors.
var ErrOriginatorsUnavailable = errors.New("batman-adv originators unavailable")

// Algorithm labels surfaced on OriginatorTopology.Algorithm so the UI can
// pick the right metric vocabulary (TQ vs throughput) without parsing.
const (
	AlgorithmBATMANIV = "BATMAN_IV"
	AlgorithmBATMANV  = "BATMAN_V"
)

// OriginatorTopology is the snapshot the mesh topology RPC is built from. It
// represents the local batman-adv originator table — one entry per reachable
// peer, plus our own identity for labeling the self node in the UI.
type OriginatorTopology struct {
	CollectedAt  time.Time
	SelfMAC      string
	SelfHostname string
	Algorithm    string
	Originators  []OriginatorEntry
}

// OriginatorEntry is one row of the originator table, enriched with bat-hosts
// hostname lookups and a derived hop count.
type OriginatorEntry struct {
	OrigMAC         string
	OrigHostname    string // from bat-hosts; empty if unknown
	NextHopMAC      string
	NextHopHostname string // from bat-hosts; empty if unknown
	HardIfname      string // wlh0, phy2-mesh0, vxlan0, …
	TQ              int
	Throughput      float64
	LastSeenMs      int
	Hops            int // 1 when the originator is its own best next hop
}

// OriginatorTopologyProvider abstracts retrieval of the snapshot for
// testability. Production wires BatctlOriginatorTopologyProvider.
type OriginatorTopologyProvider interface {
	GetOriginatorTopology() (*OriginatorTopology, error)
}

// BatctlOriginatorTopologyProvider composes the raw batctl originator wrapper
// with bat-hosts enrichment and self-identity lookup. It is the production
// implementation wired into the mesh topology handler and the delta tracker.
type BatctlOriginatorTopologyProvider struct {
	Originators   OriginatorProvider
	SelfMACReader func() (string, error)
	Now           func() time.Time
	BatHostsPath  string
}

// GetOriginatorTopology builds the snapshot by:
//   - reading the originator list via batctl oj,
//   - filtering to the best-route entries (batctl emits multiple rows per
//     originator; only Best==true rows describe the forwarding state),
//   - looking up hostnames from /tmp/bat-hosts,
//   - deriving hop counts via best-next-hop chain walks,
//   - locating the local bat0 MAC so the UI can label the self node.
//
// Missing data degrades gracefully — a bat-hosts read failure yields empty
// hostnames rather than failing the RPC, and an unknown self MAC leaves those
// fields empty.
func (p *BatctlOriginatorTopologyProvider) GetOriginatorTopology() (*OriginatorTopology, error) {
	orig := p.Originators
	if orig == nil {
		orig = &BatctlOriginatorProvider{}
	}

	rows, err := orig.GetOriginators()
	if err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil, fmt.Errorf("%w: %w", ErrOriginatorsUnavailable, err)
		}

		return nil, err
	}

	hostsPath := p.BatHostsPath
	if hostsPath == "" {
		hostsPath = BatHostsFilePath
	}

	batHosts, _ := ParseBatHostsFile(hostsPath) // best-effort; hostname enrichment is optional.
	if batHosts == nil {
		batHosts = &BatHosts{}
	}

	bestByMAC := make(map[string]Originator, len(rows))
	for _, r := range rows {
		if !r.Best {
			continue
		}

		bestByMAC[strings.ToLower(r.OrigAddress)] = r
	}

	hopsCache := make(map[string]int, len(bestByMAC))
	entries := make([]OriginatorEntry, 0, len(bestByMAC))
	algorithm := ""

	for _, r := range bestByMAC {
		entries = append(entries, OriginatorEntry{
			OrigMAC:         r.OrigAddress,
			OrigHostname:    batHosts.GetHostByMAC(r.OrigAddress),
			NextHopMAC:      r.BestNeigh,
			NextHopHostname: batHosts.GetHostByMAC(r.BestNeigh),
			HardIfname:      r.HardIfname,
			TQ:              r.TQ,
			Throughput:      r.Throughput,
			LastSeenMs:      r.LastSeenMs,
			Hops:            hopsFor(r.OrigAddress, bestByMAC, hopsCache),
		})

		// BATMAN_V reports throughput; BATMAN_IV reports TQ. When we see
		// either we can pin the algorithm label the UI uses.
		if algorithm == "" {
			switch {
			case r.Throughput > 0:
				algorithm = AlgorithmBATMANV
			case r.TQ > 0:
				algorithm = AlgorithmBATMANIV
			}
		}
	}

	selfMAC, _ := p.readSelfMAC() // best-effort; empty string is acceptable.
	selfHostname := deriveSelfHostname(selfMAC, batHosts)

	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}

	return &OriginatorTopology{
		SelfMAC:      selfMAC,
		SelfHostname: selfHostname,
		Algorithm:    algorithm,
		Originators:  entries,
		CollectedAt:  now,
	}, nil
}

// readSelfMAC returns the MAC of the local bat0 interface. Tests can override
// via SelfMACReader; production uses net.InterfaceByName which goes through
// netlink and works on every supported arch.
func (p *BatctlOriginatorTopologyProvider) readSelfMAC() (string, error) {
	if p.SelfMACReader != nil {
		return p.SelfMACReader()
	}

	iface, err := net.InterfaceByName("bat0")
	if err != nil {
		return "", fmt.Errorf("lookup bat0: %w", err)
	}

	return iface.HardwareAddr.String(), nil
}

// deriveSelfHostname looks up the bat0 MAC in bat-hosts and strips the "_bat0"
// suffix to produce the base hostname the UI displays. Falls back to an empty
// string if the MAC is absent from bat-hosts (the frontend then renders the
// MAC directly rather than an unknown hostname).
func deriveSelfHostname(selfMAC string, batHosts *BatHosts) string {
	if selfMAC == "" || batHosts == nil {
		return ""
	}

	full := batHosts.GetHostByMAC(selfMAC)
	if full == "" {
		return ""
	}

	if i := strings.LastIndex(full, "_"); i > 0 {
		return full[:i]
	}

	return full
}

// Hop-derivation tunables. Walks deeper than hopsMaxChain are treated as
// cyclic or misconfigured — no realistic mesh routes that far — and the
// chain is recorded as unknown so the UI doesn't draw spokes to infinity.
const (
	hopsMaxChain = 16
	hopsUnknown  = 99
)

// hopsFor returns the hop distance from the self node to orig, walking the
// best_next_hop chain until a direct neighbor is reached (best_next_hop ==
// orig_address) or the chain is broken/cyclic. Results are memoized in cache
// so the whole snapshot resolves in O(N) amortized.
//
// chain records the walk order from the original orig to the current node;
// when termination is reached, hops are back-filled in LIFO order so that
// every host on the walked path caches its correct distance in one pass.
func hopsFor(origMAC string, best map[string]Originator, cache map[string]int) int {
	key := strings.ToLower(origMAC)
	if v, ok := cache[key]; ok {
		return v
	}

	chain := []string{key}
	cur := key

	for range hopsMaxChain {
		entry, ok := best[cur]
		if !ok {
			break // broken chain: next hop's entry isn't in the table
		}

		next := strings.ToLower(entry.BestNeigh)

		if next == cur || next == "" {
			// cur is a direct neighbor — one hop from self. Back-fill the
			// chain so every intermediate originator gets its distance.
			backfillChain(chain, 1, cache)

			return cache[key]
		}

		// Cache shortcut: if we've already resolved the next hop in a
		// previous walk, use it to seed the current chain.
		if memo, ok := cache[next]; ok && memo != hopsUnknown {
			backfillChain(chain, memo+1, cache)

			return cache[key]
		}

		// Cycle guard: if next is already somewhere on this walk, the chain
		// is pathological and we can't derive a hop count.
		if slices.Contains(chain, next) {
			markUnknown(chain, cache)

			return hopsUnknown
		}

		chain = append(chain, next)
		cur = next
	}

	markUnknown(chain, cache)

	return hopsUnknown
}

// backfillChain writes hops into cache for every host on chain, assigning
// startHops to the tail and incrementing by one for each step back toward
// the head. A chain of [C, B, A] with startHops=1 (A is direct) yields
// cache[A]=1, cache[B]=2, cache[C]=3.
func backfillChain(chain []string, startHops int, cache map[string]int) {
	h := startHops
	for i := len(chain) - 1; i >= 0; i-- {
		cache[chain[i]] = h
		h++
	}
}

func markUnknown(chain []string, cache map[string]int) {
	for _, k := range chain {
		cache[k] = hopsUnknown
	}
}
