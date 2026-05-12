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
	// GapRuns{N} is the monotonic count of contiguous sequence-gap
	// runs observed at the jitter buffer's skip-missing branch,
	// bucketed by run length in frames (20 ms each). A single gap is
	// counted exactly once at the skip point, not once per skipped
	// frame. Distribution across buckets distinguishes isolated loss
	// (bucket 1, 2–5) from long contiguous bursts (11–20 and up) and
	// drives the choice of recovery strategy (FEC/RED vs masking).
	GapRuns1      int64 `json:"gap_runs_1"`
	GapRuns2to5   int64 `json:"gap_runs_2_5"`
	GapRuns6to10  int64 `json:"gap_runs_6_10"`
	GapRuns11to20 int64 `json:"gap_runs_11_20"`
	GapRuns21to50 int64 `json:"gap_runs_21_50"`
	GapRunsOver50 int64 `json:"gap_runs_over_50"`
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
	dst.GapRuns1 = jb.GapRuns1.Load()
	dst.GapRuns2to5 = jb.GapRuns2to5.Load()
	dst.GapRuns6to10 = jb.GapRuns6to10.Load()
	dst.GapRuns11to20 = jb.GapRuns11to20.Load()
	dst.GapRuns21to50 = jb.GapRuns21to50.Load()
	dst.GapRunsOver50 = jb.GapRunsOver50.Load()
}
