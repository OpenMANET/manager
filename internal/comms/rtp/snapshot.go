package rtp

// JitterBufferSnapshot is a point-in-time copy of the per-port receive
// jitter buffer's monotonic counters. Field semantics are documented in
// docs/instrumentation-snapshot.md — keep that file in sync when adding
// or renaming fields here.
//
// Note that depth/occupancy are intentionally omitted. Reading them
// would require acquiring jb.mu, which the snapshot path never holds.
// The three fields below are read via atomic loads and so observe
// producer writes without contention.
type JitterBufferSnapshot struct {
	// Overflows is the monotonic count of incoming packets the jitter
	// buffer rejected because it was full. Sustained non-zero overflow
	// deltas suggest the receiver is behind the sender or the network
	// is bursting.
	Overflows int64 `json:"overflows"`
	// SSRCResets is the monotonic count of mid-stream SSRC transitions
	// the jitter buffer handled by resetting and re-initializing. A
	// high value suggests multiple talkers or a sender restart.
	SSRCResets int64 `json:"ssrc_resets"`
	// IdleResets is the monotonic count of gap-driven buffer resets
	// (the "same SSRC but the stream has been silent for longer than
	// the idle threshold" safety net).
	IdleResets int64 `json:"idle_resets"`
}

// Snapshot copies the current counter values into dst using atomic loads.
// Safe to call concurrently with pushWithSSRC and pop. Nil-safe on both
// receiver and dst. Allocation-free.
func (jb *JitterBuffer) Snapshot(dst *JitterBufferSnapshot) {
	if jb == nil || dst == nil {
		return
	}

	dst.Overflows = jb.Overflows.Load()
	dst.SSRCResets = jb.SSRCResets.Load()
	dst.IdleResets = jb.IdleResets.Load()
}
