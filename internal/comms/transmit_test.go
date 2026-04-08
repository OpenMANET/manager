package comms

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control"
	"github.com/openmanet/openmanetd/internal/comms/device"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"github.com/openmanet/openmanetd/internal/comms/webaudio"
)

func newSilentComms() *CommsConfig {
	return &CommsConfig{Log: zerolog.Nop()}
}

func newTestRuntime(stream device.AudioStream) *CommsRuntime {
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1", stream.startCalls)
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1", stream.startCalls)
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

	if stream.stopCalls != 1 {
		t.Errorf("Stop called %d times, want 1", stream.stopCalls)
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

	if stream.stopCalls != 0 {
		t.Errorf("Stop called %d times, want 0", stream.stopCalls)
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
// audio.Init.BuildAudio captures from PortAudio on real hardware),
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times after settle, want 1", stream.startCalls)
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

// ─── beginTransmission error-path tests ──────────────────────────────────────

// newRunRuntime extends newTestRuntime with a receiver/sender so receiveLoop
// started inside Run does not panic.
func newRunRuntime(stream device.AudioStream) *CommsRuntime {
	rt := newTestRuntime(stream)
	rt.Ports[0].Receiver = rtp.NewSwappableReceiver(newMockReader())
	rt.Ports[0].Sender = rtp.NewSwappableSender(&mockWriter{})

	return rt
}

func TestBeginTransmission_NilStreamCallsReopen(t *testing.T) {
	reopened := &mockStream{}
	rt := newTestRuntime(nil) // nil broadcastStream triggers reopen path
	rt.ReopenBroadcast = func() error {
		rt.BroadcastStream = reopened

		return nil
	}

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if reopened.startCalls != 1 {
		t.Errorf("Start called %d times on reopened stream, want 1", reopened.startCalls)
	}

	if !cfg.isBroadcasting(rt) {
		t.Error("should be broadcasting after successful reopen")
	}
}

func TestBeginTransmission_ReopenFailureClearsBroadcasting(t *testing.T) {
	rt := newTestRuntime(nil) // nil broadcastStream triggers reopen path
	rt.ReopenBroadcast = func() error {
		return errors.New("hardware fault")
	}

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should NOT be broadcasting when reopen fails")
	}
}

func TestBeginTransmission_StartFailureReopensAndSucceeds(t *testing.T) {
	badStream := &mockStream{startErr: errors.New("start failed")}
	goodStream := &mockStream{}

	rt := newTestRuntime(badStream)
	rt.ReopenBroadcast = func() error {
		rt.BroadcastStream = goodStream

		return nil
	}

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if goodStream.startCalls != 1 {
		t.Errorf("good stream Start called %d times, want 1", goodStream.startCalls)
	}

	if !cfg.isBroadcasting(rt) {
		t.Error("should be broadcasting after start-fail + reopen + restart")
	}
}

func TestBeginTransmission_StartFailureReopenAlsoFails(t *testing.T) {
	rt := newTestRuntime(&mockStream{startErr: errors.New("start failed")})
	rt.ReopenBroadcast = func() error {
		return errors.New("reopen error")
	}

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should NOT be broadcasting when start and reopen both fail")
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

	if stream.startCalls != 0 {
		t.Errorf("Start called %d times, want 0 (channel busy)", stream.startCalls)
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1 (rx is stale)", stream.startCalls)
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1 (never received)", stream.startCalls)
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1", stream.startCalls)
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1", stream.startCalls)
	}

	if stream.stopCalls != 1 {
		t.Errorf("Stop called %d times, want 1", stream.stopCalls)
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

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1", stream.startCalls)
	}

	if stream.stopCalls != 1 {
		t.Errorf("Stop called %d times, want 1", stream.stopCalls)
	}
}

// ─── Additional beginTransmission / endTransmission edge cases ────────────────

// TestBeginTransmission_NilStreamAndNilReopen verifies that beginTransmission
// does not panic and leaves broadcasting=false when both broadcastStream and
// reopenBroadcast are nil.
func TestBeginTransmission_NilStreamAndNilReopen(t *testing.T) {
	rt := newTestRuntime(nil) // nil broadcastStream
	rt.ReopenBroadcast = nil  // also nil

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should NOT be broadcasting when both stream and reopenBroadcast are nil")
	}
}

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

	if stream.startCalls != 0 {
		t.Errorf("Start called %d times, want 0 in web mode", stream.startCalls)
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

	if stream.stopCalls != 0 {
		t.Errorf("Stop called %d times, want 0 in web mode", stream.stopCalls)
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
