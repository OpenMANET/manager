// Package sysupgrade orchestrates firmware upgrade discovery, download,
// verification, and execution. The Manager type wraps the GitHub releases
// client, an asset matcher, an image-download path with sha256
// verification, and a sysupgrade(1) runner. It exposes a small public API
// suitable for direct consumption by a ConnectRPC handler.
//
// State is kept in-process; the only persistence is a JSON cache of the
// most recently fetched releases under <stateDir>/sysupgrade-releases.json.
//
// Concurrency:
//   - Manager.mu guards all mutable state.
//   - At most one upgrade goroutine runs at a time; StartUpgrade rejects
//     a second call while busy.
//   - Subscribers receive Progress events through buffered channels;
//     full channels drop the event for that subscriber rather than
//     blocking the publisher.
package sysupgrade

import (
	"time"
)

// Phase describes the high-level state of the manager.
type Phase int32

// Phase values. Mirror the sysupgradev1 proto enum.
const (
	PhaseUnspecified Phase = iota
	PhaseIdle
	PhaseChecking
	PhaseDownloading
	PhaseVerifying
	PhaseReady
	PhaseUpgrading
	PhaseFailed
)

// phaseStrings maps each Phase to its lowercase JSON representation. The
// snapshot framework reads these once per Refresh; pre-computing avoids
// allocations.
//
//nolint:gochecknoglobals // closed-set lookup table; treated as const
var phaseStrings = [...]string{
	PhaseUnspecified: "unspecified",
	PhaseIdle:        "idle",
	PhaseChecking:    "checking",
	PhaseDownloading: "downloading",
	PhaseVerifying:   "verifying",
	PhaseReady:       "ready",
	PhaseUpgrading:   "upgrading",
	PhaseFailed:      "failed",
}

// String returns the lowercase JSON representation of the Phase.
func (p Phase) String() string {
	if int(p) < 0 || int(p) >= len(phaseStrings) {
		return phaseStrings[PhaseUnspecified]
	}

	return phaseStrings[p]
}

// Progress is one observable event from the manager. It is both the
// in-memory snapshot used by GetUpgradeStatus and the unit emitted to
// every subscriber of StreamUpgradeProgress.
type Progress struct {
	UpdatedAt  time.Time
	Message    string
	ErrMsg     string
	ReleaseTag string
	AssetName  string
	BytesDone  int64
	BytesTotal int64
	Phase      Phase
	Percent    int32
	ChildPID   int32
}
