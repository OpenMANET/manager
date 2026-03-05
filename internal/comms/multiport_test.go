package comms

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ─── sendToAllPorts tests ─────────────────────────────────────────────────────

// TestSendToAllPorts_SendsToEnabledPorts verifies that sendToAllPorts sends to
// every port where sendEnabled==true and rtpSess is set, and skips ports where
// sendEnabled==false.
func TestSendToAllPorts_SendsToEnabledPorts(t *testing.T) {
	enabledSess := &mockRTPSender{}
	disabledSess := &mockRTPSender{}

	pc0 := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}, rtpSess: enabledSess}
	pc0.sendEnabled.Store(true)

	pc1 := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}, rtpSess: disabledSess}
	pc1.sendEnabled.Store(false) // disabled at runtime

	rt := &CommsRuntime{ports: []*portChannel{pc0, pc1}}
	cfg := &CommsConfig{Log: zerolog.Nop()}

	cfg.sendToAllPorts(rt, []byte{0xAA, 0xBB})

	if len(enabledSess.Payloads) != 1 {
		t.Errorf("enabled port: got %d sends, want 1", len(enabledSess.Payloads))
	}

	if len(disabledSess.Payloads) != 0 {
		t.Errorf("disabled port: got %d sends, want 0", len(disabledSess.Payloads))
	}
}

// TestSendToAllPorts_SkipsNilSession verifies that ports with nil rtpSess are
// skipped without panicking.
func TestSendToAllPorts_SkipsNilSession(t *testing.T) {
	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}, rtpSess: nil}
	pc.sendEnabled.Store(true)

	rt := &CommsRuntime{ports: []*portChannel{pc}}
	cfg := &CommsConfig{Log: zerolog.Nop()}

	// Must not panic.
	cfg.sendToAllPorts(rt, []byte{0x01})
}

// TestSendToAllPorts_MultiplePortsAllEnabled verifies payload is delivered to
// all send-enabled ports in a multi-port configuration.
func TestSendToAllPorts_MultiplePortsAllEnabled(t *testing.T) {
	sessions := [3]*mockRTPSender{}
	ports := make([]*portChannel, 3)

	for i := range sessions {
		sessions[i] = &mockRTPSender{}
		ports[i] = &portChannel{cfg: McastPortConfig{Send: true}, rtpSess: sessions[i]}
		ports[i].sendEnabled.Store(true)
	}

	rt := &CommsRuntime{ports: ports}
	cfg := &CommsConfig{Log: zerolog.Nop()}

	cfg.sendToAllPorts(rt, []byte{0xDE, 0xAD})

	for i, sess := range sessions {
		if len(sess.Payloads) != 1 {
			t.Errorf("port %d: got %d sends, want 1", i, len(sess.Payloads))
		}
	}
}

// ─── EnableTalkGroupSend / EnableTalkGroupReceive / GetTalkGroupStates tests ────

func setupRuntimeConfig(t *testing.T) (*CommsConfig, *CommsRuntime) {
	t.Helper()

	pc0 := &portChannel{cfg: McastPortConfig{Address: "239.0.0.1", Port: 5004, Send: true, Receive: true}}
	pc0.sendEnabled.Store(true)
	pc0.receiveEnabled.Store(true)

	pc1 := &portChannel{cfg: McastPortConfig{Address: "239.0.0.2", Port: 5006, Send: true, Receive: false}}
	pc1.sendEnabled.Store(true)
	pc1.receiveEnabled.Store(false)

	rt := &CommsRuntime{ports: []*portChannel{pc0, pc1}}
	cfg := &CommsConfig{
		Log:        zerolog.Nop(),
		McastPorts: []McastPortConfig{pc0.cfg, pc1.cfg},
	}
	cfg.runtime = rt
	activeConfig.Store(cfg)
	t.Cleanup(func() { activeConfig.Store(nil) })

	return cfg, rt
}

func TestEnableTalkGroupSend_TogglesEnabled(t *testing.T) {
	_, rt := setupRuntimeConfig(t)

	if err := EnableTalkGroupSend(0, false); err != nil {
		t.Fatalf("EnableTalkGroupSend(0, false): %v", err)
	}

	if rt.ports[0].sendEnabled.Load() {
		t.Error("expected port 0 sendEnabled=false after EnableTalkGroupSend(0, false)")
	}

	if err := EnableTalkGroupSend(0, true); err != nil {
		t.Fatalf("EnableTalkGroupSend(0, true): %v", err)
	}

	if !rt.ports[0].sendEnabled.Load() {
		t.Error("expected port 0 sendEnabled=true after EnableTalkGroupSend(0, true)")
	}
}

func TestEnableTalkGroupSend_OutOfRange(t *testing.T) {
	setupRuntimeConfig(t)

	if err := EnableTalkGroupSend(99, true); err == nil {
		t.Error("expected error for out-of-range port index")
	}

	if err := EnableTalkGroupSend(-1, true); err == nil {
		t.Error("expected error for negative port index")
	}
}

func TestEnableTalkGroupSend_NotRunning(t *testing.T) {
	activeConfig.Store(nil)

	if err := EnableTalkGroupSend(0, true); err == nil {
		t.Error("expected error when comms is not running")
	}
}

func TestEnableTalkGroupReceive_TogglesEnabled(t *testing.T) {
	_, rt := setupRuntimeConfig(t)

	if err := EnableTalkGroupReceive(0, false); err != nil {
		t.Fatalf("EnableTalkGroupReceive(0, false): %v", err)
	}

	if rt.ports[0].receiveEnabled.Load() {
		t.Error("expected port 0 receiveEnabled=false")
	}

	if err := EnableTalkGroupReceive(0, true); err != nil {
		t.Fatalf("EnableTalkGroupReceive(0, true): %v", err)
	}

	if !rt.ports[0].receiveEnabled.Load() {
		t.Error("expected port 0 receiveEnabled=true")
	}
}

func TestEnableTalkGroupReceive_OutOfRange(t *testing.T) {
	setupRuntimeConfig(t)

	if err := EnableTalkGroupReceive(5, false); err == nil {
		t.Error("expected error for out-of-range port index")
	}
}

func TestEnableTalkGroupReceive_NotRunning(t *testing.T) {
	activeConfig.Store(nil)

	if err := EnableTalkGroupReceive(0, false); err == nil {
		t.Error("expected error when comms is not running")
	}
}

func TestGetTalkGroupStates_ReturnsSnapshot(t *testing.T) {
	_, rt := setupRuntimeConfig(t)

	states, err := GetTalkGroupStates()
	if err != nil {
		t.Fatalf("GetTalkGroupStates: %v", err)
	}

	if len(states) != len(rt.ports) {
		t.Fatalf("got %d states, want %d", len(states), len(rt.ports))
	}

	if states[0].Address != "239.0.0.1" || states[0].Port != 5004 {
		t.Errorf("port 0: got %s:%d, want 239.0.0.1:5004", states[0].Address, states[0].Port)
	}

	if !states[0].SendEnabled || !states[0].ReceiveEnabled {
		t.Errorf("port 0: want SendEnabled=true ReceiveEnabled=true; got %v %v",
			states[0].SendEnabled, states[0].ReceiveEnabled)
	}

	if states[1].Address != "239.0.0.2" || states[1].Port != 5006 {
		t.Errorf("port 1: got %s:%d, want 239.0.0.2:5006", states[1].Address, states[1].Port)
	}

	if !states[1].SendEnabled || states[1].ReceiveEnabled {
		t.Errorf("port 1: want SendEnabled=true ReceiveEnabled=false; got %v %v",
			states[1].SendEnabled, states[1].ReceiveEnabled)
	}
}

func TestGetTalkGroupStates_ReflectsRuntimeChanges(t *testing.T) {
	setupRuntimeConfig(t)

	if err := EnableTalkGroupSend(1, false); err != nil {
		t.Fatal(err)
	}

	states, err := GetTalkGroupStates()
	if err != nil {
		t.Fatal(err)
	}

	if states[1].SendEnabled {
		t.Error("expected port 1 SendEnabled=false after SetPortSend(1, false)")
	}
}

func TestGetTalkGroupStates_NotRunning(t *testing.T) {
	activeConfig.Store(nil)

	if _, err := GetTalkGroupStates(); err == nil {
		t.Error("expected error when comms is not running")
	}
}

// ─── isReceivingRemote multi-port tests ───────────────────────────────────────

// TestIsReceivingRemote_SendDisabledPortNotChecked verifies that a port with
// sendEnabled=false does not contribute to the half-duplex block even when it
// has recently received audio.
func TestIsReceivingRemote_SendDisabledPortNotChecked(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	// Port with sendEnabled=false has recent rx – should NOT block transmission.
	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc.sendEnabled.Store(false)
	pc.lastRemoteRx.Store(time.Now().UnixNano())

	rt := &CommsRuntime{ports: []*portChannel{pc}}

	if cfg.isReceivingRemote(rt) {
		t.Error("sendEnabled=false port should not trigger half-duplex block")
	}
}

// TestIsReceivingRemote_MultiPortFirstEnabled verifies that a recent rx on any
// send-enabled port returns true.
func TestIsReceivingRemote_MultiPortFirstEnabled(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	pc0 := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc0.sendEnabled.Store(true)
	pc0.lastRemoteRx.Store(time.Now().UnixNano())

	pc1 := &portChannel{cfg: McastPortConfig{Send: true, Receive: false}}
	pc1.sendEnabled.Store(true)
	// pc1 has never received

	rt := &CommsRuntime{ports: []*portChannel{pc0, pc1}}

	if !cfg.isReceivingRemote(rt) {
		t.Error("expected true when first port has recent rx and sendEnabled=true")
	}
}

// ─── receiveEnabled runtime toggle tests ─────────────────────────────────────

// TestReceiveLoop_SkipsDeliveryWhenReceiveDisabled verifies that when
// pc.receiveEnabled is false, incoming RTP packets are read but not queued
// to the jitter buffer (the playback buffer stays empty).
func TestReceiveLoop_SkipsDeliveryWhenReceiveDisabled(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}

	// Pre-load one valid RTP packet.
	raw := makeRTPBytes(t, 0)
	src := &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 5004}
	reader := newMockReader(
		mockPacket{data: raw, src: src},
	)

	pc := &portChannel{
		cfg:      McastPortConfig{Send: false, Receive: true},
		receiver: newSwappableReceiver(reader),
	}
	pc.sendEnabled.Store(false)
	pc.receiveEnabled.Store(false) // ← disabled
	pc.playbackBuffer = make(chan []float32, 8)

	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		cfg.receiveLoop(ctx, pc, rt)
	}()

	// Wait for the packet to be consumed by the read loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(60 * time.Millisecond) // allow one playout tick interval

	cancel()
	pc.receiver.Close()

	<-done

	if len(pc.playbackBuffer) != 0 {
		t.Errorf("receive disabled: got %d frames queued, want 0", len(pc.playbackBuffer))
	}
}

// ─── per-port drainPlaybackBuffer tests ──────────────────────────────────────

func TestDrainPlaybackBuffer_MultiPort(t *testing.T) {
	pc0 := &portChannel{}
	pc0.playbackBuffer = make(chan []float32, 4)
	pc0.playbackBuffer <- []float32{1}
	pc0.playbackBuffer <- []float32{2}

	pc1 := &portChannel{}
	pc1.playbackBuffer = make(chan []float32, 4)
	pc1.playbackBuffer <- []float32{3}

	rt := &CommsRuntime{ports: []*portChannel{pc0, pc1}}
	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.drainPlaybackBuffer(rt)

	if len(pc0.playbackBuffer) != 0 {
		t.Errorf("port 0: expected empty buffer; got %d items", len(pc0.playbackBuffer))
	}

	if len(pc1.playbackBuffer) != 0 {
		t.Errorf("port 1: expected empty buffer; got %d items", len(pc1.playbackBuffer))
	}
}

// TestBeginTransmission_BeepSentToAllPorts verifies that beginTransmission
// queues the start-beep to every configured port's playback buffer.
func TestBeginTransmission_BeepSentToAllPorts(t *testing.T) {
	pc0 := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc0.sendEnabled.Store(true)
	pc0.receiveEnabled.Store(true)
	pc0.playbackBuffer = make(chan []float32, 4)

	pc1 := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc1.sendEnabled.Store(true)
	pc1.receiveEnabled.Store(true)
	pc1.playbackBuffer = make(chan []float32, 4)

	rt := &CommsRuntime{
		ports:           []*portChannel{pc0, pc1},
		broadcastStream: &mockStream{},
		beepBufferStart: []float32{0.1, 0.2},
		beepBufferStop:  []float32{0.3, 0.4},
		decoder:         &mockDecoder{},
	}

	cfg := &CommsConfig{Log: zerolog.Nop()}
	cfg.beginTransmission(rt)

	if len(pc0.playbackBuffer) == 0 {
		t.Error("port 0: expected start beep in playback buffer")
	}

	if len(pc1.playbackBuffer) == 0 {
		t.Error("port 1: expected start beep in playback buffer")
	}
}

// ─── sendToAllPorts error handling ───────────────────────────────────────────

// TestSendToAllPorts_SendErrorDoesNotPanic verifies that a send error on a
// port's RTP session is logged and skipped without panicking.
func TestSendToAllPorts_SendErrorDoesNotPanic(t *testing.T) {
	sess := &mockRTPSender{sendErr: errors.New("network unreachable")}

	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}, rtpSess: sess}
	pc.sendEnabled.Store(true)

	rt := &CommsRuntime{ports: []*portChannel{pc}}
	cfg := &CommsConfig{Log: zerolog.Nop()}

	// Must not panic.
	cfg.sendToAllPorts(rt, []byte{0xAA, 0xBB})
}

// ─── playoutLoop receive-only port tests ──────────────────────────────────────

// TestPlayoutLoop_ReceiveOnlyPortNotSuppressedDuringBroadcast verifies that
// playout suppression (half-duplex) only applies to send-capable ports.
// A receive-only port (cfg.Send=false) must continue delivering frames to its
// playback buffer even while rt.broadcasting==true.
func TestPlayoutLoop_ReceiveOnlyPortNotSuppressedDuringBroadcast(t *testing.T) {
	// Receive-only port: Send=false.
	pc := &portChannel{
		cfg: McastPortConfig{Send: false, Receive: true},
	}
	pc.sendEnabled.Store(false)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []float32, 8)

	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}
	rt.broadcasting.Store(true) // simulate active broadcast on another port

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB}) // prebuffer=1: immediately ready

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// The frame must be delivered because cfg.Send==false bypasses suppression.
	select {
	case <-pc.playbackBuffer:
		// success: frame appeared despite broadcasting=true
	case <-time.After(200 * time.Millisecond):
		t.Error("receive-only port should deliver frames during broadcast; got none")
	}
}
