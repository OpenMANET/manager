package comms

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
)

func newSilentComms() *CommsConfig {
	return &CommsConfig{Log: zerolog.Nop()}
}

func newTestRuntime(stream BroadcastCapture) *CommsRuntime {
	pc := &PortChannel{
		cfg:     McastPortConfig{Send: true, Receive: true},
		RTPSess: &mockRTPSender{},
	}
	pc.SendEnabled.Store(true)
	pc.ReceiveEnabled.Store(true)
	pc.PlaybackBuffer = make(chan []int16, 16)

	return &CommsRuntime{
		Ports:           []*PortChannel{pc},
		BeepBufferStart: []int16{100, 200},
		BeepBufferStop:  []int16{300, 400},
		BroadcastStream: stream,
		Decoder:         &mockDecoder{},
	}
}

func TestBeginTransmission_StartsStream(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1", stream.txEnableCalls)
	}
}

func TestBeginTransmission_SetsBroadcasting(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if !cfg.isBroadcasting(rt) {
		t.Error("should be broadcasting after beginTransmission")
	}
}

func TestBeginTransmission_QueuesStartBeep(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	select {
	case frame := <-rt.Ports[0].PlaybackBuffer:
		if len(frame) != 2 {
			t.Errorf("beep frame len=%d want 2", len(frame))
		}
	default:
		t.Error("expected start beep in buffer")
	}
}

func TestBeginTransmission_DoublePressIgnored(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	for len(rt.Ports[0].PlaybackBuffer) > 0 {
		<-rt.Ports[0].PlaybackBuffer
	}

	cfg.beginTransmission(rt)

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1", stream.txEnableCalls)
	}
}

func TestEndTransmission_StopsStream(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	for len(rt.Ports[0].PlaybackBuffer) > 0 {
		<-rt.Ports[0].PlaybackBuffer
	}

	cfg.endTransmission(rt)

	if stream.txDisableCalls != 1 {
		t.Errorf("SetTxEnabled(false) called %d times, want 1", stream.txDisableCalls)
	}
}

func TestEndTransmission_ClearsBroadcasting(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	for len(rt.Ports[0].PlaybackBuffer) > 0 {
		<-rt.Ports[0].PlaybackBuffer
	}

	cfg.endTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should not be broadcasting after endTransmission")
	}
}

func TestEndTransmission_WhenNotBroadcasting_Noop(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	cfg := newSilentComms()
	cfg.endTransmission(rt)

	if stream.txDisableCalls != 0 {
		t.Errorf("SetTxEnabled(false) called %d times, want 0", stream.txDisableCalls)
	}
}

func TestDrainPlaybackBuffer(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	cfg := newSilentComms()

	rt.Ports[0].PlaybackBuffer <- []int16{1}

	rt.Ports[0].PlaybackBuffer <- []int16{2}

	cfg.drainPlaybackBuffer(rt)

	if len(rt.Ports[0].PlaybackBuffer) != 0 {
		t.Errorf("expected empty buffer; got %d items", len(rt.Ports[0].PlaybackBuffer))
	}
}

func TestIsBroadcasting_InitiallyFalse(t *testing.T) {
	rt := newTestRuntime(&mockStream{})

	cfg := newSilentComms()
	if cfg.isBroadcasting(rt) {
		t.Error("should not be broadcasting initially")
	}
}

// TestBeginTransmission_DefaultStartDelay verifies that beginTransmission
// honors the default settle window (defaultPttStartDelayMs) so hardware that
// needs a brief warm-up before the first encoded frame still gets it.
func TestBeginTransmission_DefaultStartDelay(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	cfg := newSilentComms()

	want := defaultPttStartDelayMs * time.Millisecond

	start := time.Now()

	cfg.beginTransmission(rt)

	elapsed := time.Since(start)

	// Allow a tiny tolerance: scheduling, mock-stream Start cost, etc.
	const slop = 10 * time.Millisecond
	if elapsed+slop < want {
		t.Errorf("beginTransmission elapsed=%s, want at least %s (default settle)", elapsed, want)
	}
}

// TestBeginTransmission_SettleCoversPlaybackLatency verifies that when the
// runtime carries a non-zero PlaybackOutputLatency (mirroring what
// audio.Init.BuildAudio derives from the malgo playback period on real hardware),
// beginTransmission sleeps long enough to cover the start-beep's physical
// emission window before starting the mic stream. This is the regression
// guard against the start-beep leaking into the transmitted RTP stream via
// acoustic coupling between the speaker and the mic.
func TestBeginTransmission_SettleCoversPlaybackLatency(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	rt.PlaybackOutputLatency = 100 * time.Millisecond

	cfg := newSilentComms()

	// Expected floor: PlaybackOutputLatency + frameDuration (20 ms) +
	// beepSettleMargin (20 ms) = 140 ms.
	wantFloor := 100*time.Millisecond + frameDuration + beepSettleMargin

	start := time.Now()

	cfg.beginTransmission(rt)

	elapsed := time.Since(start)

	// Allow a small slop below the floor for clock granularity.
	const slop = 10 * time.Millisecond
	if elapsed+slop < wantFloor {
		t.Errorf("beginTransmission elapsed=%s, want at least %s (beep settle floor)", elapsed, wantFloor)
	}

	// Sanity upper bound — settle should not balloon.
	if elapsed > 400*time.Millisecond {
		t.Errorf("beginTransmission elapsed=%s, slept far longer than expected", elapsed)
	}

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times after settle, want 1", stream.txEnableCalls)
	}
}

// TestTransmitSettleWait_NegativeSkips verifies that an explicit negative
// PttStartDelayMs causes transmitSettleWait to return zero even when the
// runtime carries a large PlaybackOutputLatency. The negative value is the
// operator opt-out for low-latency PTT-to-ready and implies acceptance of
// the beep-leak risk.
func TestTransmitSettleWait_NegativeSkips(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	rt.PlaybackOutputLatency = 500 * time.Millisecond

	cfg := newSilentComms()
	cfg.PttStartDelayMs = -1

	if got := cfg.transmitSettleWait(rt); got != 0 {
		t.Errorf("transmitSettleWait = %s, want 0 when PttStartDelayMs<0", got)
	}
}

// TestTransmitSettleWait_PicksMaxOfWarmupAndBeepSettle verifies the max()
// behavior across the two contributing components: warmup (from
// pttStartDelay) and beep settle (from PlaybackOutputLatency).
func TestTransmitSettleWait_PicksMaxOfWarmupAndBeepSettle(t *testing.T) {
	t.Run("warmup_dominates", func(t *testing.T) {
		rt := newTestRuntime(&mockStream{})
		rt.PlaybackOutputLatency = 0 // beep settle = 0+20+20 = 40 ms

		cfg := newSilentComms()
		cfg.PttStartDelayMs = 100 // explicit warmup

		want := 100 * time.Millisecond
		if got := cfg.transmitSettleWait(rt); got != want {
			t.Errorf("transmitSettleWait = %s, want %s (warmup should win)", got, want)
		}
	})

	t.Run("beep_settle_dominates", func(t *testing.T) {
		rt := newTestRuntime(&mockStream{})
		rt.PlaybackOutputLatency = 80 * time.Millisecond // beep settle = 80+20+20 = 120 ms

		cfg := newSilentComms()
		cfg.PttStartDelayMs = 30 // small warmup

		want := 80*time.Millisecond + frameDuration + beepSettleMargin
		if got := cfg.transmitSettleWait(rt); got != want {
			t.Errorf("transmitSettleWait = %s, want %s (beep settle should win)", got, want)
		}
	})
}

// TestBeginTransmission_ConfigurablePttStartDelay verifies that an explicit
// PttStartDelayMs overrides the default and that a negative value skips the
// wait entirely.
func TestBeginTransmission_ConfigurablePttStartDelay(t *testing.T) {
	t.Run("custom", func(t *testing.T) {
		stream := &mockStream{}
		rt := newTestRuntime(stream)
		cfg := newSilentComms()
		cfg.PttStartDelayMs = 20

		start := time.Now()

		cfg.beginTransmission(rt)

		elapsed := time.Since(start)

		if elapsed < 15*time.Millisecond {
			t.Errorf("expected ~20ms wait, got %s", elapsed)
		}

		if elapsed > 200*time.Millisecond {
			t.Errorf("expected ~20ms wait, got %s (slept too long)", elapsed)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		stream := &mockStream{}
		rt := newTestRuntime(stream)
		cfg := newSilentComms()
		cfg.PttStartDelayMs = -1 // skip the wait entirely

		start := time.Now()

		cfg.beginTransmission(rt)

		elapsed := time.Since(start)

		if elapsed > 30*time.Millisecond {
			t.Errorf("expected near-zero wait when PttStartDelayMs<0, got %s", elapsed)
		}
	})
}

// ─── beginTransmission nil-stream test ───────────────────────────────────────

// newRunRuntime extends newTestRuntime with a receiver/sender so receiveLoop
// started inside Run does not panic.
func newRunRuntime(stream BroadcastCapture) *CommsRuntime {
	rt := newTestRuntime(stream)
	rt.Ports[0].Receiver = rtp.NewSwappableReceiver(newMockReader())
	rt.Ports[0].Sender = rtp.NewSwappableSender(&mockWriter{})

	return rt
}

// TestBeginTransmission_NilStreamClearsBroadcasting verifies that begin with
// a nil BroadcastStream fails cleanly (no panic) and leaves Broadcasting
// false. Under the unified always-on design the stream is opened once at
// StartHardware so a nil here indicates an initialization error, not a
// recoverable runtime condition — there is no reopen-on-demand path.
func TestBeginTransmission_NilStreamClearsBroadcasting(t *testing.T) {
	rt := newTestRuntime(nil)
	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should NOT be broadcasting when BroadcastStream is nil")
	}
}

// ─── Half-duplex tests ────────────────────────────────────────────────────────

func TestBeginTransmission_BlockedWhenReceivingRemote(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	// Simulate a packet that arrived just now from a remote peer. Use the
	// canonical helper so the half-duplex cache is primed the same way
	// receiveLoop does it in production.
	rt.Ports[0].MarkRemoteRx(rt)

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if stream.txEnableCalls != 0 {
		t.Errorf("SetTxEnabled(true) called %d times, want 0 (channel busy)", stream.txEnableCalls)
	}

	if cfg.isBroadcasting(rt) {
		t.Error("should not be broadcasting while actively receiving remote audio")
	}
}

func TestBeginTransmission_AllowedWhenRxStale(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	// Store a timestamp well beyond rxActiveThreshold.
	rt.Ports[0].RxGate.MarkAt(time.Now().Add(-(rxActiveThreshold + time.Second)))

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1 (rx is stale)", stream.txEnableCalls)
	}

	if !cfg.isBroadcasting(rt) {
		t.Error("should be broadcasting when last rx is older than rxActiveThreshold")
	}
}

func TestBeginTransmission_AllowedWhenNeverReceived(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	// rxGate is zero — never received a packet.

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1 (never received)", stream.txEnableCalls)
	}
}

// ─── Run event-loop tests ─────────────────────────────────────────────────────

func TestRun_ExitsOnContextCancel(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	rt := newRunRuntime(&mockStream{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so Run returns without blocking

	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.Run(ctx, rt, &mockEventSource{ch: make(chan control.PTTEvent)})
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Run did not exit after context cancel")
	}

	rt.Ports[0].Receiver.Close() // unblock any lingering receiveLoop goroutine
}

func TestRun_ClosedEventChannelExits(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	rt := newRunRuntime(&mockStream{})

	ch := make(chan control.PTTEvent)
	close(ch) // closed before Run is called

	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.Run(context.Background(), rt, &mockEventSource{ch: ch})
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Run did not exit when event channel is closed")
	}

	rt.Ports[0].Receiver.Close()
}

func TestRun_PTTDownStartsTransmission(t *testing.T) {
	stream := &mockStream{}
	rt := newRunRuntime(stream)
	cfg := &CommsConfig{Log: zerolog.Nop()}

	evCh := make(chan control.PTTEvent, 1)
	evCh <- control.PTTDown

	close(evCh)

	cfg.Run(context.Background(), rt, &mockEventSource{ch: evCh})

	rt.Ports[0].Receiver.Close()

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1", stream.txEnableCalls)
	}
}

func TestRun_PTTUpStopsTransmission(t *testing.T) {
	stream := &mockStream{}
	rt := newRunRuntime(stream)
	cfg := &CommsConfig{Log: zerolog.Nop()}

	evCh := make(chan control.PTTEvent, 2)
	evCh <- control.PTTDown

	evCh <- control.PTTUp

	close(evCh)

	cfg.Run(context.Background(), rt, &mockEventSource{ch: evCh})

	rt.Ports[0].Receiver.Close()

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1", stream.txEnableCalls)
	}

	if stream.txDisableCalls != 1 {
		t.Errorf("SetTxEnabled(false) called %d times, want 1", stream.txDisableCalls)
	}
}

func TestRun_PTTToggleFlips(t *testing.T) {
	stream := &mockStream{}
	rt := newRunRuntime(stream)
	cfg := &CommsConfig{Log: zerolog.Nop()}

	evCh := make(chan control.PTTEvent, 2)
	evCh <- control.PTTToggle // → beginTransmission

	evCh <- control.PTTToggle // → endTransmission

	close(evCh)

	cfg.Run(context.Background(), rt, &mockEventSource{ch: evCh})

	rt.Ports[0].Receiver.Close()

	if stream.txEnableCalls != 1 {
		t.Errorf("SetTxEnabled(true) called %d times, want 1", stream.txEnableCalls)
	}

	if stream.txDisableCalls != 1 {
		t.Errorf("SetTxEnabled(false) called %d times, want 1", stream.txDisableCalls)
	}
}

// ─── Additional beginTransmission / endTransmission edge cases ────────────────

// TestEndTransmission_QueuesStopBeepToAllPorts verifies that endTransmission
// queues beepBufferStop to every configured port, mirroring the multi-port
// start-beep behavior tested by TestBeginTransmission_BeepSentToAllPorts.
func TestEndTransmission_QueuesStopBeepToAllPorts(t *testing.T) {
	pc0 := &PortChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc0.PlaybackBuffer = make(chan []int16, 16)

	pc1 := &PortChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc1.PlaybackBuffer = make(chan []int16, 16)

	rt := &CommsRuntime{
		Ports:           []*PortChannel{pc0, pc1},
		BeepBufferStart: []int16{100, 200},
		BeepBufferStop:  []int16{300, 400},
		BroadcastStream: &mockStream{},
		Decoder:         &mockDecoder{},
	}

	cfg := newSilentComms()

	// Begin so broadcasting=true, then drain the start beeps before asserting.
	cfg.beginTransmission(rt)

	for len(pc0.PlaybackBuffer) > 0 {
		<-pc0.PlaybackBuffer
	}

	for len(pc1.PlaybackBuffer) > 0 {
		<-pc1.PlaybackBuffer
	}

	cfg.endTransmission(rt)

	// Both ports must have received a stop beep.
	for i, pc := range []*PortChannel{pc0, pc1} {
		select {
		case frame := <-pc.PlaybackBuffer:
			if len(frame) != 2 {
				t.Errorf("port %d: stop-beep frame len=%d, want 2", i, len(frame))
			}
		default:
			t.Errorf("port %d: expected stop beep in buffer", i)
		}
	}
}

// ─── Web-mode tests ────────────────────────────────────────────────────────

func TestBeginTransmission_WebMode_SkipsBroadcastStream(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	rt.WebBridge = &webaudio.Bridge{} // non-nil activates web mode

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if !cfg.isBroadcasting(rt) {
		t.Error("should be broadcasting in web mode")
	}

	if stream.txEnableCalls != 0 {
		t.Errorf("SetTxEnabled(true) called %d times, want 0 in web mode", stream.txEnableCalls)
	}

	// No beep should be queued.
	select {
	case <-rt.Ports[0].PlaybackBuffer:
		t.Error("unexpected beep in playback buffer in web mode")
	default:
	}
}

func TestEndTransmission_WebMode_SkipsBroadcastStream(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	rt.WebBridge = &webaudio.Bridge{}

	cfg := newSilentComms()

	// Begin first so broadcasting is true.
	rt.Broadcasting.Store(true)
	cfg.endTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should not be broadcasting after endTransmission in web mode")
	}

	if stream.txDisableCalls != 0 {
		t.Errorf("SetTxEnabled(false) called %d times, want 0 in web mode", stream.txDisableCalls)
	}

	// No beep should be queued.
	select {
	case <-rt.Ports[0].PlaybackBuffer:
		t.Error("unexpected beep in playback buffer in web mode")
	default:
	}
}

func TestBeginTransmission_WebMode_HalfDuplexStillWorks(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	rt.WebBridge = &webaudio.Bridge{}
	// Simulate active remote reception via the canonical helper so the
	// half-duplex cache is primed exactly as receiveLoop would.
	rt.Ports[0].MarkRemoteRx(rt)

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should not be broadcasting while receiving remote audio, even in web mode")
	}
}
