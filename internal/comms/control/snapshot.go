package control

// HalfDuplexGateSnapshot is a point-in-time copy of a HalfDuplexGate's
// observable state. Field semantics are documented in
// docs/instrumentation-snapshot.md — keep that file in sync when adding
// or renaming fields here.
type HalfDuplexGateSnapshot struct {
	// LastMarkUnixNano is the timestamp of the most recent Mark call,
	// in unix nanoseconds. Zero means the gate has never been marked.
	LastMarkUnixNano int64 `json:"last_mark_unix_nano"`
	// ThresholdNs is the configured half-duplex receive window, in
	// nanoseconds. A capture outside this window means the gate is
	// inactive regardless of LastMarkUnixNano.
	ThresholdNs int64 `json:"threshold_ns"`
	// Active is true iff the most recent Mark is within ThresholdNs of
	// the current time at capture.
	Active bool `json:"active"`
}

// Snapshot copies the current gate state into dst. Nil-safe on both
// receiver and dst. Allocation-free.
func (g *HalfDuplexGate) Snapshot(dst *HalfDuplexGateSnapshot) {
	if g == nil || dst == nil {
		return
	}

	dst.LastMarkUnixNano = g.last.Load()

	threshold := g.Threshold
	if threshold <= 0 {
		threshold = DefaultHalfDuplexThreshold
	}

	dst.ThresholdNs = threshold.Nanoseconds()
	dst.Active = g.Active()
}
