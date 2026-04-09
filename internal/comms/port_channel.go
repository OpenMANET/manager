package comms

import (
	"sync/atomic"

	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
)

// ─── McastPortConfig / McastPortState ────────────────────────────────────────

// McastPortConfig describes a single multicast endpoint that the comms
// subsystem listens and/or transmits on. Ports with Send=false will not open
// an RTP/RTCP sender; ports with Receive=false will not open an RTP receiver
// socket.
//
// InitSendEnabled and InitReceiveEnabled seed the runtime atomic flags that
// EnableTalkGroupSend / EnableTalkGroupReceive toggle at runtime. When nil
// the values fall back to Send and Receive respectively, preserving backward
// compatibility for any caller that constructs McastPortConfig directly.
type McastPortConfig struct {
	InitSendEnabled    *bool
	InitReceiveEnabled *bool
	Address            string
	Port               int
	Send               bool
	Receive            bool
}

// McastPortState is a read-only snapshot of the runtime direction-toggle state
// for a single port. Returned by Service.TalkGroupStates.
type McastPortState struct {
	Address        string
	Port           int
	SendEnabled    bool
	ReceiveEnabled bool
}

// ─── PortChannel ─────────────────────────────────────────────────────────────

// PortChannel holds all live resources for one McastPortConfig entry.
// sendEnabled and receiveEnabled are atomic bools that can be toggled at
// runtime via EnableTalkGroupSend / EnableTalkGroupReceive without restarting any goroutine
// or socket.
//
// jitter is the per-port RTP jitter buffer. It is allocated in
// buildSinglePortChannel for ports with a Receive socket and shared between
// receiveLoop (producer) and the PortAudio output callback (consumer). For
// portChannels constructed directly in tests, callers must allocate it
// explicitly.
//
// consecutivePLC is owned by the PortAudio output callback for this port:
// each port has its own callback running on its own audio thread, so the
// field is single-writer and does not need atomic semantics. Tests that call
// playoutOneFrame directly are likewise single-threaded with respect to it.
//
// playbackBuffer is retained as a one-shot side channel for TX beep tones
// (see transmit.go beginTransmission/endTransmission); the PortAudio callback
// drains it before falling through to playoutOneFrame so beeps preempt one
// frame of jitter-buffered audio.
type PortChannel struct {
	RTPSess           rtp.Sender
	PlaybackStream    device.AudioStream
	Sender            *rtp.SwappableSender
	RTCPSend          *rtp.SwappableSender
	Receiver          *rtp.SwappableReceiver
	Jitter            *rtp.JitterBuffer
	PlaybackBuffer    chan []int16
	cfg               McastPortConfig
	ConsecutivePLC    int
	RxGate            control.HalfDuplexGate
	PlaybackUnderruns atomic.Int64

	// Diagnostic RX-path counters. All monotonic since startup; reporters
	// compute deltas across windows. RxPkts is the raw "kernel handed us a
	// packet" count from receiveLoop's ReadFromUDP; the remaining counters
	// segment that count by what happened next. RxPushed + RxPushRejected
	// only sum to RxPkts - RxLoopback - RxParseErrs (and only when the port
	// is receive-enabled). Used to localize RX stutter to one specific
	// stage of the per-port pipeline.
	RxPkts         atomic.Int64
	RxParseErrs    atomic.Int64
	RxLoopback     atomic.Int64
	RxPushed       atomic.Int64
	RxPushRejected atomic.Int64

	// WebPoppedSkipped is bumped by webPlayoutLoop when the jitter
	// buffer's PopReady returns skippedMissing=true (an out-of-order
	// sequence gap wide enough that the buffer advanced the cursor past
	// the hole). Diagnostic only; the audio path never reads this field
	// itself. Zero on the portaudio playout path, which does not use
	// webPlayoutLoop.
	WebPoppedSkipped atomic.Int64

	SendEnabled    atomic.Bool
	ReceiveEnabled atomic.Bool
}

// MarkRemoteRx records that a remote RTP packet has just been received on
// this port for half-duplex enforcement. It stamps the port's RxGate and,
// when this port is currently send-enabled, primes the runtime-wide
// RemoteRxActive cache so the PTT TX path observes a busy channel without
// waiting for the next halfDuplexDecayLoop tick. Receive-only ports never
// block our own transmissions, so the cache is left untouched in that case.
//
// Production callers reach this from receiveLoop after a successful RTP
// parse; tests use it as the single canonical way to express "a remote
// packet arrived" so the cache invariant is exercised the same way it is
// in production.
func (pc *PortChannel) MarkRemoteRx(rt *CommsRuntime) {
	pc.RxGate.Mark()

	if pc.SendEnabled.Load() {
		rt.RemoteRxActive.Store(true)
	}
}

// closePartial closes any sockets and the RTP session that this PortChannel
// has acquired so far. It is safe to call on a nil receiver and on a
// PortChannel where some fields are still nil — used both as the rollback
// path inside buildSinglePortChannel and as the bulk cleanup path in
// buildNetwork when a later port fails.
func (pc *PortChannel) closePartial() {
	if pc == nil {
		return
	}

	if pc.Receiver != nil {
		_ = pc.Receiver.Close()
	}

	if s, ok := pc.RTPSess.(*rtp.Session); ok && s != nil {
		_ = s.Close()
	}

	if pc.Sender != nil {
		_ = pc.Sender.Close()
	}

	if pc.RTCPSend != nil {
		_ = pc.RTCPSend.Close()
	}
}
