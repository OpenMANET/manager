package webaudio

// BridgeSnapshot is a point-in-time copy of the web RX bridge's monotonic
// counters. Field semantics are documented in
// docs/instrumentation-snapshot.md — keep that file in sync when adding
// or renaming fields here.
type BridgeSnapshot struct {
	// RxPushIn is the monotonic count of frames offered to the bridge
	// by the jitter buffer side (every PushRxFrame invocation).
	RxPushIn int64 `json:"rx_push_in"`
	// RxPushDrop is the monotonic count of frames dropped because the
	// bridge's rxFrames channel was full. Compute rx_push_drop /
	// rx_push_in as a ratio; sustained values above ~1% indicate the
	// web client is not draining received audio fast enough.
	RxPushDrop int64 `json:"rx_push_drop"`
}

// Snapshot copies the current counter values into dst using atomic loads.
// Nil-safe on both receiver and dst. Allocation-free.
func (b *Bridge) Snapshot(dst *BridgeSnapshot) {
	if b == nil || dst == nil {
		return
	}

	dst.RxPushIn = b.RxPushIn.Load()
	dst.RxPushDrop = b.RxPushDrop.Load()
}
