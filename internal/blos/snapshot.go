package blos

// BLOSSnapshot is the BLOS section published to the instrumentation
// registry under the "blos" name. Field semantics are documented in
// docs/instrumentation-snapshot.md — keep that file in sync when
// adding or renaming fields here.
type BLOSSnapshot struct {
	// Running reflects whether the BLOS module is currently active
	// (Tailscale daemon up + BLOSManager.running == true). False
	// covers both "disabled by config" and "enabled but stopped".
	Running bool `json:"running"`
}

// Snapshot fills dst with the manager's current state. Takes a short
// mutex critical section to read the running flag; this does not touch
// any producer hot path. Nil-safe on both receiver and dst.
func (m *BLOSManager) Snapshot(dst *BLOSSnapshot) {
	if m == nil || dst == nil {
		return
	}

	dst.Running = m.IsRunning()
}

// BLOSSnapshotter is an instrumentation.Snapshotter adapter that wires
// the BLOS subsystem into the instrumentation registry. It holds an
// internal BLOSSnapshot refreshed in place on every Refresh call.
type BLOSSnapshotter struct {
	// Manager is the live BLOSManager this adapter observes. It must be
	// set before the adapter is registered with the instrumentation
	// registry; the adapter is nil-safe and produces a zero snapshot
	// when Manager is nil.
	Manager *BLOSManager
	data    BLOSSnapshot
}

// Refresh implements instrumentation.Snapshotter.
func (a *BLOSSnapshotter) Refresh() {
	a.Manager.Snapshot(&a.data)
}

// Data implements instrumentation.Snapshotter.
func (a *BLOSSnapshotter) Data() any {
	return &a.data
}
