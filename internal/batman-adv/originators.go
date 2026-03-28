package batmanadv

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Originator represents a single entry from batctl's originator JSON output.
type Originator struct {
	OrigAddress string `json:"orig_address"`
	HardIfname  string `json:"hard_ifname"`
	BestNeigh   string `json:"best_next_hop"`
	LastSeenMs  int    `json:"last_seen_msecs"`
	TQ          int    `json:"tq"`
}

// OriginatorProvider abstracts originator retrieval for testability.
type OriginatorProvider interface {
	GetOriginators() ([]Originator, error)
}

// BatctlOriginatorProvider is the production implementation using batctl.
type BatctlOriginatorProvider struct{}

// GetOriginators executes `batctl oj` and returns the parsed originator list.
func (p *BatctlOriginatorProvider) GetOriginators() ([]Originator, error) {
	cmd := exec.Command("batctl", "oj") //nolint:noctx // short-lived local command

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("batctl oj: %w", err)
	}

	var originators []Originator
	if err := json.Unmarshal(output, &originators); err != nil {
		return nil, fmt.Errorf("unmarshal originators: %w", err)
	}

	return originators, nil
}

// GetOriginatorCount returns the number of unique originators.
func GetOriginatorCount(originators []Originator) int {
	seen := make(map[string]struct{}, len(originators))
	for _, o := range originators {
		seen[o.OrigAddress] = struct{}{}
	}

	return len(seen)
}
