package batmanadv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrVisUnavailable is returned by VisibilityProvider implementations when
// batadv-vis exits non-zero (typically because alfred is not running on the
// local node). Callers can map this to a user-facing
// "mesh visibility unavailable" state distinct from internal errors.
var ErrVisUnavailable = errors.New("batadv-vis unavailable")

// VisDoc mirrors the top-level object produced by `batadv-vis -f jsondoc`.
type VisDoc struct {
	SourceVersion string     `json:"source_version"`
	Vis           []VisEntry `json:"vis"`
	Algorithm     int        `json:"algorithm"`
}

// VisEntry is a single mesh node entry in a VisDoc.
type VisEntry struct {
	Primary   string        `json:"primary"`
	Secondary []string      `json:"secondary,omitempty"`
	Neighbors []VisNeighbor `json:"neighbors"`
	Clients   []string      `json:"clients"`
}

// VisNeighbor is a single directed link reported by a VisEntry.
type VisNeighbor struct {
	Router   string `json:"router"`
	Neighbor string `json:"neighbor"`
	// Metric is kept as the raw string form emitted by batadv-vis (e.g.
	// "1.008"). Use ParseMetric to convert to a float.
	Metric string `json:"metric"`
}

// VisibilityProvider abstracts retrieval of the jsondoc snapshot for
// testability.
type VisibilityProvider interface {
	GetVisibility() (*VisDoc, error)
}

// BatadvVisProvider is the production implementation that shells out to
// `batadv-vis -f jsondoc`.
type BatadvVisProvider struct{}

// GetVisibility executes `batadv-vis -f jsondoc` and returns the parsed
// snapshot. When the command exits non-zero (the common case when alfred is
// not running), the returned error wraps ErrVisUnavailable so callers can
// match with errors.Is.
func (p *BatadvVisProvider) GetVisibility() (*VisDoc, error) {
	cmd := exec.Command("batadv-vis", "-f", "jsondoc") //nolint:noctx // short-lived local command

	output, err := cmd.Output()
	if err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return nil, fmt.Errorf("batadv-vis exit: %w: %w", ErrVisUnavailable, err)
		}

		return nil, fmt.Errorf("batadv-vis: %w", err)
	}

	var doc VisDoc
	if err := json.Unmarshal(output, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal vis jsondoc: %w", err)
	}

	return &doc, nil
}

// ParseMetric converts a batadv-vis jsondoc metric string (e.g. "1.008")
// into a float32. Returns 0 when the input is empty or cannot be parsed;
// the metric is informational and callers treat 0 as "unknown".
func ParseMetric(s string) float32 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 32)
	if err != nil {
		return 0
	}

	return float32(v)
}
