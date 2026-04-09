package comms

import (
	"github.com/openmanet/openmanetd/internal/comms/audio"
	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
)

// CommsSnapshot is the full comms subsystem section that the instrumentation
// registry publishes under the "comms" name. Field semantics are documented
// in docs/instrumentation-snapshot.md — keep that file in sync when adding
// or renaming fields here.
type CommsSnapshot struct {
	// ControlSource is the active PTT control source (openvlm, nanoptt,
	// web, roip). Always populated even when the subsystem is disabled.
	ControlSource string `json:"control_source"`
	// BroadcastEncoder is the TX-side audio encoder snapshot. Zero when
	// the runtime is absent or the broadcast stream is not a production
	// BroadcastEncoder (e.g. under a unit-test fake).
	BroadcastEncoder audio.AudioEncoderSnapshot `json:"broadcast_encoder"`
	// WebBridge is the web-mode audio bridge snapshot. Zero when the
	// runtime is absent.
	WebBridge webaudio.BridgeSnapshot `json:"web_bridge"`
	// Ports holds one entry per multicast talk group. Reused across
	// captures — callers must copy if they want to retain the slice
	// beyond a Capture boundary.
	Ports []PortSnapshot `json:"ports"`
	// Enabled reflects whether the comms subsystem runtime is currently
	// published. When false, every other field is zero and should be
	// interpreted as "subsystem is off", not "subsystem is broken".
	Enabled bool `json:"enabled"`
	// Broadcasting reflects whether the TX gate is currently open.
	Broadcasting bool `json:"broadcasting"`
	// RemoteRxActive reflects the half-duplex cache flag maintained by
	// halfDuplexDecayLoop. When true, the TX path blocks transmission.
	RemoteRxActive bool `json:"remote_rx_active"`
}

// PortSnapshot is the per-talk-group section of a CommsSnapshot.
type PortSnapshot struct {
	// Address is the multicast group address (e.g. "239.0.0.1").
	Address string `json:"address"`
	// Jitter is the per-port receive jitter buffer snapshot.
	Jitter rtp.JitterBufferSnapshot `json:"jitter"`
	// RxGate is the per-port half-duplex gate state.
	RxGate control.HalfDuplexGateSnapshot `json:"rx_gate"`
	// Port is the multicast UDP port.
	Port int `json:"port"`
	// PlaybackUnderruns counts playback-side decode failures that the
	// port audio callback had to recover from via PLC.
	PlaybackUnderruns int64 `json:"playback_underruns"`
	// SendEnabled is the runtime send-direction toggle. A port with
	// SendEnabled=false will not open an encoder or publish TX frames.
	SendEnabled bool `json:"send_enabled"`
	// ReceiveEnabled is the runtime receive-direction toggle. A port
	// with ReceiveEnabled=false will not push incoming RTP frames into
	// its jitter buffer.
	ReceiveEnabled bool `json:"receive_enabled"`
}

// Snapshot fills dst with a consistent view of the comms runtime. The
// method is nil-safe on the receiver and dereferences s.Rt exactly once
// so it observes a single consistent runtime pointer across the call,
// even if SetDefault races with it.
//
// dst.Ports is reused across captures: the slice capacity is retained
// and only resized when the number of ports changes. After the first
// call with the steady-state runtime present, Snapshot is allocation-
// free (verified by TestService_SnapshotZeroAlloc).
func (s *Service) Snapshot(dst *CommsSnapshot) {
	if dst == nil {
		return
	}

	if s == nil {
		dst.Enabled = false
		dst.Broadcasting = false
		dst.RemoteRxActive = false
		dst.ControlSource = ""
		dst.BroadcastEncoder = audio.AudioEncoderSnapshot{}
		dst.WebBridge = webaudio.BridgeSnapshot{}
		dst.Ports = dst.Ports[:0]

		return
	}

	if s.Cfg != nil {
		dst.ControlSource = s.Cfg.ControlSource
	} else {
		dst.ControlSource = ""
	}

	rt := s.Rt
	if rt == nil {
		dst.Enabled = false
		dst.Broadcasting = false
		dst.RemoteRxActive = false
		dst.BroadcastEncoder = audio.AudioEncoderSnapshot{}
		dst.WebBridge = webaudio.BridgeSnapshot{}
		dst.Ports = dst.Ports[:0]

		return
	}

	dst.Enabled = true
	dst.Broadcasting = rt.Broadcasting.Load()
	dst.RemoteRxActive = rt.RemoteRxActive.Load()

	// BroadcastStream is an interface. In production the live instance is
	// always a *audio.BroadcastEncoder; test fakes may substitute a
	// minimal stub that does not carry counters. The type assertion falls
	// back to a zero snapshot in that case.
	if be, ok := rt.BroadcastStream.(*audio.BroadcastEncoder); ok {
		be.Snapshot(&dst.BroadcastEncoder)
	} else {
		dst.BroadcastEncoder = audio.AudioEncoderSnapshot{}
	}

	rt.WebBridge.Snapshot(&dst.WebBridge)

	n := len(rt.Ports)
	if cap(dst.Ports) < n {
		dst.Ports = make([]PortSnapshot, n)
	} else {
		dst.Ports = dst.Ports[:n]
	}

	for i, pc := range rt.Ports {
		pc.Snapshot(&dst.Ports[i])
	}
}

// Snapshot fills dst with the port's counter state. Nil-safe. Zero-alloc.
func (pc *PortChannel) Snapshot(dst *PortSnapshot) {
	if pc == nil || dst == nil {
		return
	}

	dst.Address = pc.cfg.Address
	dst.Port = pc.cfg.Port
	dst.SendEnabled = pc.SendEnabled.Load()
	dst.ReceiveEnabled = pc.ReceiveEnabled.Load()
	dst.PlaybackUnderruns = pc.PlaybackUnderruns.Load()
	pc.Jitter.Snapshot(&dst.Jitter)
	pc.RxGate.Snapshot(&dst.RxGate)
}

// CommsSnapshotter is an instrumentation.Snapshotter adapter that wires the
// comms subsystem into the instrumentation registry. It holds an internal
// CommsSnapshot that is refreshed in place on every Refresh() call. The
// adapter looks up the live comms service on each call so it transparently
// handles enable/disable transitions.
type CommsSnapshotter struct {
	data CommsSnapshot
}

// Refresh implements instrumentation.Snapshotter. Zero-alloc after the
// first call that establishes the Ports slice capacity.
func (c *CommsSnapshotter) Refresh() {
	Default().Snapshot(&c.data)
}

// Data implements instrumentation.Snapshotter. Returns a pointer that is
// stable across Refresh calls.
func (c *CommsSnapshotter) Data() any {
	return &c.data
}
