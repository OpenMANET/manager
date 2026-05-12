package sysupgrade

// SysupgradeSnapshot is the JSON shape of the sysupgrade section in the
// instrumentation envelope. Field names must stay in lockstep with
// docs/instrumentation-snapshot.md.
type SysupgradeSnapshot struct {
	Phase             string `json:"phase"`
	LastErrorMsg      string `json:"last_error,omitempty"`
	CurrentReleaseTag string `json:"current_release_tag,omitempty"`
	CurrentAssetName  string `json:"current_asset_name,omitempty"`
	CapableReason     string `json:"capable_reason"`
	LastCheckUnix     int64  `json:"last_check_unix"`
	DownloadedBytes   int64  `json:"downloaded_bytes"`
	TotalBytes        int64  `json:"total_bytes"`
	ChildPID          int32  `json:"child_pid"`
	InProgress        bool   `json:"in_progress"`
	Capable           bool   `json:"capable"`
}

// Snapshotter is an instrumentation.Snapshotter adapter that wires the
// sysupgrade manager into the instrumentation registry.
type Snapshotter struct {
	Manager *Manager

	data SysupgradeSnapshot
}

// Refresh implements instrumentation.Snapshotter. The Manager's
// snapshot method takes the manager's mutex briefly and copies primitive
// fields into the adapter-owned struct.
func (s *Snapshotter) Refresh() {
	if s.Manager == nil {
		return
	}

	s.Manager.snapshotForInstrumentation(&s.data)
}

// Data implements instrumentation.Snapshotter. Returns a stable pointer
// across Refresh calls.
func (s *Snapshotter) Data() any {
	return &s.data
}
