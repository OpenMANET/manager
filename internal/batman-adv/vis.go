package batmanadv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrVisUnavailable is returned when the batadv-vis binary ran successfully
// but produced no mesh data. In practice this happens during alfred
// cold-start: the client connected, received zero published vis payloads,
// and emitted an empty "vis" array. Callers treat this as an empty-state
// render rather than an error.
var ErrVisUnavailable = errors.New("batman-adv vis: data not available")

// batmanAlgorithmIV is the batman-adv numeric algorithm code for BATMAN_IV.
const batmanAlgorithmIV = 4

// batmanAlgorithmV is the batman-adv numeric algorithm code for BATMAN_V.
const batmanAlgorithmV = 15

// VisDoc is the root document emitted by "batadv-vis -f jsondoc". It is a
// mesh-wide snapshot — every publishing node contributes one VisNode entry
// describing its neighbors and attached clients.
type VisDoc struct {
	CollectedAt   time.Time `json:"-"`
	SourceVersion string    `json:"source_version"`
	Vis           []VisNode `json:"vis"`
	Algorithm     int       `json:"algorithm"`
}

// VisNode is one publishing node. Primary is its bat0 MAC; Secondary lists
// the MACs it advertises on other hard interfaces. Neighbors is this node's
// one-hop mesh view; Clients is its transtable-local set (non-mesh MACs
// bridged via ethernet or AP).
type VisNode struct {
	Primary   string        `json:"primary"`
	Secondary []string      `json:"secondary,omitempty"`
	Neighbors []VisNeighbor `json:"neighbors"`
	Clients   []string      `json:"clients,omitempty"`
}

// VisNeighbor is one edge in a VisNode's neighbor list. Router is the MAC
// on this node's side (matches Primary or one of Secondary); Neighbor is
// the peer MAC; Metric is the link metric as a raw string (units depend on
// the document's Algorithm field — see ParseMetric for parsing).
type VisNeighbor struct {
	Router   string `json:"router"`
	Neighbor string `json:"neighbor"`
	Metric   string `json:"metric"`
}

// VisProvider abstracts retrieval of the mesh-wide topology snapshot so
// tests can script doc sequences without forking a binary. The production
// implementation wraps batadv-vis.
type VisProvider interface {
	GetMeshVis(ctx context.Context) (*VisDoc, error)
}

// BatadvVisProvider execs `batadv-vis -f jsondoc` and parses its output.
// Binary, Args, and Now are overrideable for tests; the zero value runs
// the "batadv-vis" binary with "-f jsondoc" and no extra arguments.
type BatadvVisProvider struct {
	Now    func() time.Time
	Binary string
	Args   []string
}

// GetMeshVis fetches and parses the current mesh-wide vis snapshot.
// Returns ErrVisUnavailable when the document is empty (alfred running but
// no nodes publishing yet), a wrapped exec error on fork failure, or a
// wrapped json error on malformed output.
func (p BatadvVisProvider) GetMeshVis(ctx context.Context) (*VisDoc, error) {
	bin := p.Binary
	if bin == "" {
		bin = "batadv-vis"
	}

	args := make([]string, 0, 2+len(p.Args))
	args = append(args, "-f", "jsondoc")
	args = append(args, p.Args...)

	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // fixed binary name, args come from our own config

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("batadv-vis exec: %w", err)
	}

	doc, err := ParseVisDoc(out)
	if err != nil {
		return nil, err
	}

	if p.Now != nil {
		doc.CollectedAt = p.Now()
	} else {
		doc.CollectedAt = time.Now()
	}

	return doc, nil
}

// ParseVisDoc unmarshals the "batadv-vis -f jsondoc" output and normalizes
// every MAC to lowercase so downstream set operations don't need per-lookup
// ToLower. An empty "vis" array returns ErrVisUnavailable.
func ParseVisDoc(b []byte) (*VisDoc, error) {
	var doc VisDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("batadv-vis jsondoc parse: %w", err)
	}

	if len(doc.Vis) == 0 {
		return nil, ErrVisUnavailable
	}

	for i := range doc.Vis {
		v := &doc.Vis[i]
		v.Primary = strings.ToLower(v.Primary)

		for j := range v.Secondary {
			v.Secondary[j] = strings.ToLower(v.Secondary[j])
		}

		for j := range v.Neighbors {
			v.Neighbors[j].Router = strings.ToLower(v.Neighbors[j].Router)
			v.Neighbors[j].Neighbor = strings.ToLower(v.Neighbors[j].Neighbor)
		}

		for j := range v.Clients {
			v.Clients[j] = strings.ToLower(v.Clients[j])
		}
	}

	return &doc, nil
}

// AlgorithmLabel converts the batadv-vis numeric algorithm code into the
// human-readable label the UI uses to pick metric formatters. Unknown
// codes return an empty string.
func (d *VisDoc) AlgorithmLabel() string {
	switch d.Algorithm {
	case batmanAlgorithmIV:
		return AlgorithmBATMANIV
	case batmanAlgorithmV:
		return AlgorithmBATMANV
	}

	return ""
}

// ParseMetric converts the string metric field batadv-vis emits into a
// float64. For BATMAN_IV the value is 255/TQ (lower is better); for
// BATMAN_V the value is throughput-derived. Empty or malformed input
// yields zero, which the UI renders as "metric unknown".
func ParseMetric(s string) float64 {
	if s == "" {
		return 0
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}

	return f
}
