package comms

import (
	"context"
	"errors"
	"github.com/openmanet/openmanetd/internal/comms/rtp"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newSilentComms() *CommsConfig {
	return &CommsConfig{Log: zerolog.Nop()}
}

func newTestRuntime(stream AudioStream) *CommsRuntime {
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

func TestBeginTransmission_200msSleep(t *testing.T) {
	stream := &mockStream{}
	rt := newTestRuntime(stream)
	cfg := newSilentComms()
	start := time.Now()

	cfg.beginTransmission(rt)

	if time.Since(start) < 180*time.Millisecond {
		t.Errorf("beginTransmission should sleep ~200ms")
	}
}

// ─── beginTransmission error-path tests ──────────────────────────────────────

// newRunRuntime extends newTestRuntime with a receiver/sender so receiveLoop
// started inside Run does not panic.
func newRunRuntime(stream AudioStream) *CommsRuntime {
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
	// Simulate a packet that arrived just now from a remote peer.
	rt.Ports[0].RxGate.Mark()

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

		cfg.Run(ctx, rt, &mockEventSource{ch: make(chan PTTEvent)})
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

	ch := make(chan PTTEvent)
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

	evCh := make(chan PTTEvent, 1)
	evCh <- PTTDown

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

	evCh := make(chan PTTEvent, 2)
	evCh <- PTTDown

	evCh <- PTTUp

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

	evCh := make(chan PTTEvent, 2)
	evCh <- PTTToggle // → beginTransmission

	evCh <- PTTToggle // → endTransmission

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
	rt.WebBridge = &WebAudioBridge{} // non-nil activates web mode

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
	rt.WebBridge = &WebAudioBridge{}

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
	rt.WebBridge = &WebAudioBridge{}
	// Simulate active remote reception.
	rt.Ports[0].RxGate.Mark()

	cfg := newSilentComms()
	cfg.beginTransmission(rt)

	if cfg.isBroadcasting(rt) {
		t.Error("should not be broadcasting while receiving remote audio, even in web mode")
	}
}
