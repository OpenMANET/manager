package blos

// BLOSSnapshot is the BLOS section published to the instrumentation
// registry under the "blos" name. Field semantics are documented in
// docs/instrumentation-snapshot.md — keep that file in sync when
// adding or renaming fields here.
type BLOSSnapshot struct {
	BackendState         string  `json:"backend_state"`
	ConnectedSinceUnixNs int64   `json:"connected_since_unix_ns"`
	RxBytesTotal         uint64  `json:"rx_bytes_total"`
	TxBytesTotal         uint64  `json:"tx_bytes_total"`
	RxBps60s             float64 `json:"rx_bps_60s"`
	TxBps60s             float64 `json:"tx_bps_60s"`
	EventsDropped        uint64  `json:"events_dropped"`
	PeerCount            uint32  `json:"peer_count"`
	Running              bool    `json:"running"`
}

// Snapshot fills dst with the manager's current state. Takes a short
// mutex critical section to read the running flag; this does not touch
// any producer hot path. Nil-safe on both receiver and dst.
func (m *BLOSManager) Snapshot(dst *BLOSSnapshot) {
	if m == nil || dst == nil {
		return
	}

	dst.Running = m.IsRunning()

	w := m.statusWorker()
	if w == nil {
		dst.ConnectedSinceUnixNs = 0
		dst.BackendState = ""
		dst.PeerCount = 0
		dst.RxBytesTotal = 0
		dst.TxBytesTotal = 0
		dst.RxBps60s = 0
		dst.TxBps60s = 0
		dst.EventsDropped = 0

		return
	}

	if since := w.ConnectedSince(); !since.IsZero() {
		dst.ConnectedSinceUnixNs = since.UnixNano()
	} else {
		dst.ConnectedSinceUnixNs = 0
	}

	if status := w.GetStatus(); status != nil {
		dst.BackendState = status.BackendState
		dst.PeerCount = uint32(len(status.Peer))
	} else {
		dst.BackendState = ""
		dst.PeerCount = 0
	}

	rxBps, txBps, rxTotal, txTotal := w.RateWindow(rateWindow60s)
	dst.RxBytesTotal = rxTotal
	dst.TxBytesTotal = txTotal
	dst.RxBps60s = rxBps
	dst.TxBps60s = txBps

	dst.EventsDropped = w.EventsDropped()
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
