package comms

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func newReceiveRuntime() (*CommsRuntime, *portChannel) {
	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []int16, 4)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}

	return rt, pc
}

// isAllZero reports whether every sample in the slice is exactly zero.
// Used by playoutOneFrame tests to distinguish silence from decoded audio.
func isAllZero(out []int16) bool {
	for _, v := range out {
		if v != 0 {
			return false
		}
	}

	return true
}

// ─── playoutOneFrame tests ────────────────────────────────────────────────────

// driveOneFrame is a small helper for tests that need to call playoutOneFrame
// against a fresh PCM output buffer of the standard frame size.
func driveOneFrame(cfg *CommsConfig, pc *portChannel, rt *CommsRuntime, jb *rtpJitterBuffer) []int16 {
	out := make([]int16, frameSize)
	cfg.playoutOneFrame(pc, rt, jb, out)

	return out
}

func TestPlayoutOneFrame_DecodesPayload(t *testing.T) {
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{fillValue: 1234, returnN: frameSize}

	jb := newRTPJitterBuffer(1, 10) // prebuffer=1: first push triggers start
	jb.push(0, []byte{0xAA, 0xBB})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if isAllZero(out) {
		t.Fatal("expected decoded samples, got silence")
	}

	if pc.consecutivePLC != 0 {
		t.Errorf("consecutivePLC should be 0 after a decoded payload; got %d", pc.consecutivePLC)
	}
}

func TestPlayoutOneFrame_PLCOnSkippedMissing(t *testing.T) {
	// maxDepth=4 → skip threshold = maxDepth/2 = 2.
	// Push seq=0 first to set up expected=0 and pass the prebuffer=1 threshold,
	// then pop it. Now expected=1. Push seq=2 and seq=3 (missing seq=1 with
	// len >= maxDepth/2) so popOrConceal returns conceal=true → PLC must
	// be invoked via the decoder with nil payload.
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{fillValue: 99, returnN: frameSize}

	jb := newRTPJitterBuffer(1, 4)

	jb.push(0, []byte{0})
	jb.popReady() // consume seq=0; started=true, expected=1

	jb.push(2, []byte{2})
	jb.push(3, []byte{3}) // len=2 >= maxDepth/2=2, expected=1 missing → skipped

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if isAllZero(out) {
		t.Fatal("expected a PLC frame when jitter buffer skips missing seq, got silence")
	}

	if pc.consecutivePLC != 1 {
		t.Errorf("consecutivePLC should be 1 after one PLC frame; got %d", pc.consecutivePLC)
	}
}

func TestPlayoutOneFrame_PLCOnConceal(t *testing.T) {
	// Push+pop one frame to set started=true and record a recent lastPush.
	// The jitter buffer is then empty, so shouldConceal fires on the next
	// playoutOneFrame call and the decoder is invoked with nil payload.
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{fillValue: 99, returnN: frameSize}

	jb := newRTPJitterBuffer(1, 10)

	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1, lastPush set to now

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if isAllZero(out) {
		t.Fatal("expected a PLC concealment frame while stream is active but empty, got silence")
	}

	if pc.consecutivePLC != 1 {
		t.Errorf("consecutivePLC should increment to 1 after concealment; got %d", pc.consecutivePLC)
	}
}

func TestPlayoutOneFrame_SilenceAfterMaxPLC(t *testing.T) {
	// After maxConsecutivePLC frames, playoutOneFrame should emit clean
	// silence rather than calling the (now degraded) decoder PLC.
	rt, pc := newReceiveRuntime()
	dec := &mockDecoder{fillValue: 99, returnN: frameSize}
	rt.decoder = dec

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0})
	jb.popReady() // started=true, lastPush set

	cfg := &CommsConfig{Log: zerolog.Nop()}

	// Drive playout until we exceed the PLC budget. The mock decoder
	// returns non-zero samples for both real and PLC frames so we can tell
	// PLC frames apart from silence by inspecting the samples.
	for i := 0; i < maxConsecutivePLC; i++ {
		out := driveOneFrame(cfg, pc, rt, jb)
		if isAllZero(out) {
			t.Fatalf("frame %d: expected a PLC frame, got silence", i)
		}
	}

	// The next call should be silence (consecutivePLC > maxConsecutivePLC).
	out := driveOneFrame(cfg, pc, rt, jb)
	if !isAllZero(out) {
		t.Errorf("expected silence after %d PLC frames; got non-zero samples", maxConsecutivePLC)
	}
}

func TestPlayoutOneFrame_SilenceWhenBroadcasting(t *testing.T) {
	rt, pc := newReceiveRuntime()
	rt.broadcasting.Store(true) // isBroadcasting will return true; pc.sendEnabled=true → suppress

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if !isAllZero(out) {
		t.Errorf("playoutOneFrame should emit silence while broadcasting; got non-zero samples")
	}
}

func TestPlayoutOneFrame_SilenceWhenReceiveDisabled(t *testing.T) {
	rt, pc := newReceiveRuntime()
	pc.receiveEnabled.Store(false)

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if !isAllZero(out) {
		t.Errorf("playoutOneFrame should emit silence when receive is disabled; got non-zero samples")
	}
}

func TestPlayoutOneFrame_NilJitter(t *testing.T) {
	rt, pc := newReceiveRuntime()
	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, nil)
	if !isAllZero(out) {
		t.Errorf("playoutOneFrame with nil jitter should emit silence; got non-zero samples")
	}

	if pc.playbackUnderruns.Load() != 0 {
		t.Errorf("nil jitter should not count as underrun; got %d", pc.playbackUnderruns.Load())
	}
}

func TestPlayoutOneFrame_NoUnderrunOnIdleStream(t *testing.T) {
	// Stream that has never started (no packets yet) should write silence
	// without incrementing the underrun counter — silence != underrun.
	rt, pc := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10)

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if !isAllZero(out) {
		t.Errorf("expected silence on idle stream; got non-zero samples")
	}

	if pc.playbackUnderruns.Load() != 0 {
		t.Errorf("idle stream should not increment playbackUnderruns; got %d", pc.playbackUnderruns.Load())
	}
}

func TestPlayoutOneFrame_UnderrunOnDecoderError(t *testing.T) {
	// Decoder returning an error AND PLC also failing → silence + underrun++.
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{decodeErr: errors.New("bad decode")}

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if !isAllZero(out) {
		t.Errorf("expected silence on decoder error; got non-zero samples")
	}

	if pc.playbackUnderruns.Load() != 1 {
		t.Errorf("expected playbackUnderruns=1 after decoder error; got %d", pc.playbackUnderruns.Load())
	}
}

func TestPlayoutOneFrame_DecoderErrorPLCFallback(t *testing.T) {
	// Real decode fails but PLC succeeds → playoutOneFrame should emit the
	// PLC samples, reset consecutivePLC, and NOT count an underrun.
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{
		decodeErr: errors.New("bad decode"),
		plcOK:     true,
		returnN:   frameSize,
		fillValue: 1234,
	}

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	out := driveOneFrame(cfg, pc, rt, jb)
	if isAllZero(out) {
		t.Errorf("expected PLC samples on decoder error fallback; got silence")
	}

	if pc.playbackUnderruns.Load() != 0 {
		t.Errorf("PLC fallback success should not count as underrun; got %d", pc.playbackUnderruns.Load())
	}
}

// ─── receiveLoop branch tests ─────────────────────────────────────────────────

func TestReceiveLoop_DropsOwnPackets(t *testing.T) {
	// All packets arrive from the local IP → all should be dropped.
	localIP := net.IPv4(192, 168, 1, 1)
	localAddr := &net.UDPAddr{IP: localIP}

	var pkts []mockPacket

	for i := 0; i < jitterPrebufferPackets+1; i++ {
		raw := makeRTPBytes(t, uint16(i))
		pkts = append(pkts, mockPacket{data: raw, src: localAddr})
	}

	reader := newMockReader(pkts...)
	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(reader),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []int16, 16)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}
	s := localIP.String()
	rt.localIP.Store(&s)

	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: false}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, pc, rt)
	}()

	// Wait for all queued packets to be consumed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(60 * time.Millisecond) // allow one playout tick

	cancel()
	pc.receiver.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit")
	}

	if len(pc.playbackBuffer) != 0 {
		t.Errorf("own packets should be dropped; got %d frames in buffer",
			len(pc.playbackBuffer))
	}
}

func TestReceiveLoop_DropsMalformedRTP(t *testing.T) {
	// First packet is garbage; receiveLoop should log and continue rather than crash.
	garbled := mockPacket{data: []byte{0xFF, 0x00, 0x01}, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}}
	reader := newMockReader(garbled)

	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(reader),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []int16, 8)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{},
	}

	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, pc, rt)
	}()

	// Wait for the garbled packet to be consumed.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	// The loop survived; cancel and ensure clean shutdown.
	cancel()
	pc.receiver.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit after malformed packet")
	}
}

// ─── isReceivingRemote tests ──────────────────────────────────────────────────

func TestIsReceivingRemote_FalseWhenNeverReceived(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	// rt has no ports → isReceivingRemote always returns false.
	rt := &CommsRuntime{}

	if cfg.isReceivingRemote(rt) {
		t.Error("expected false when no packet has ever been received")
	}
}

func TestIsReceivingRemote_TrueWhenRecent(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc.sendEnabled.Store(true)
	pc.lastRemoteRx.Store(time.Now().UnixNano())
	rt := &CommsRuntime{ports: []*portChannel{pc}}

	if !cfg.isReceivingRemote(rt) {
		t.Error("expected true when a packet was just received")
	}
}

func TestIsReceivingRemote_FalseWhenStale(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	pc := &portChannel{cfg: McastPortConfig{Send: true, Receive: true}}
	pc.sendEnabled.Store(true)
	// Store a timestamp older than rxActiveThreshold.
	pc.lastRemoteRx.Store(time.Now().Add(-(rxActiveThreshold + time.Second)).UnixNano())
	rt := &CommsRuntime{ports: []*portChannel{pc}}

	if cfg.isReceivingRemote(rt) {
		t.Error("expected false when last received packet is older than rxActiveThreshold")
	}
}

func TestReceiveLoop_StampsLastRemoteRx(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}

	raw := makeRTPBytes(t, 0)
	reader := newMockReader(mockPacket{data: raw, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}})
	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(reader),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []int16, 32)
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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	pc.receiver.Close()

	<-done

	if pc.lastRemoteRx.Load() == 0 {
		t.Error("expected lastRemoteRx to be set after receiving a remote packet")
	}
}

// ─── receiveLoop socket-swap recovery tests ───────────────────────────────────

// netErrClosedReader wraps a mockReader and translates any read error to
// net.ErrClosed, matching the error that real *net.UDPConn.ReadFromUDP returns
// after the connection is closed. This allows receiveLoop's socket-swap path
// (errors.Is(err, net.ErrClosed) → jitter.reset()) to be exercised in tests.
type netErrClosedReader struct {
	*mockReader
}

func (r *netErrClosedReader) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	n, addr, err := r.mockReader.ReadFromUDP(b)
	if err != nil {
		return 0, nil, net.ErrClosed
	}

	return n, addr, nil
}

// TestReceiveLoop_SocketSwapResetsJitter simulates an UpdateMulticastEndpoint
// mid-stream socket swap. The old reader is closed (returning net.ErrClosed),
// and a new reader with fresh packets is swapped in. The test verifies that the
// loop recovers and delivers frames from the new reader, proving the jitter
// buffer was reset and the loop continued correctly.
func TestReceiveLoop_SocketSwapResetsJitter(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}

	// reader1 holds a burst of packets that will fill the jitter buffer, then
	// it will block until explicitly closed (simulating the old socket).
	var pkts1 []mockPacket

	for i := 0; i < jitterPrebufferPackets+2; i++ {
		raw := makeRTPBytes(t, uint16(i))
		pkts1 = append(pkts1, mockPacket{data: raw, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}})
	}

	reader1 := &netErrClosedReader{newMockReader(pkts1...)}

	// reader2 holds fresh packets that arrive after the swap.
	var pkts2 []mockPacket

	for i := 0; i < jitterPrebufferPackets+2; i++ {
		raw := makeRTPBytes(t, uint16(i))
		pkts2 = append(pkts2, mockPacket{data: raw, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}})
	}

	reader2 := newMockReader(pkts2...)

	pc := &portChannel{
		cfg:      McastPortConfig{Send: true, Receive: true},
		receiver: newSwappableReceiver(reader1),
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []int16, 64)

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

	// Wait for reader1 to be exhausted.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader1.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	if reader1.remaining() != 0 {
		t.Fatal("timed out waiting for reader1 to be exhausted")
	}

	// Drain any frames already queued from reader1.
	for len(pc.playbackBuffer) > 0 {
		<-pc.playbackBuffer
	}

	// Swap in reader2 then close reader1 to unblock the stale ReadFromUDP.
	// receiveLoop will get net.ErrClosed, call jitter.reset(), then pick up
	// reader2 on the next iteration.
	pc.receiver.swap(reader2)
	reader1.Close()

	// Wait for reader2 packets to be delivered to receiveLoop.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reader2.remaining() == 0 {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	pc.receiver.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit after context cancel")
	}

	// Verify reader2 was fully consumed, proving the loop recovered after the
	// socket swap and the jitter buffer was reset.
	if reader2.remaining() != 0 {
		t.Errorf("receiveLoop did not consume reader2 packets after socket swap; %d remaining",
			reader2.remaining())
	}
}

// ─── Web-mode playout tests ─────────────────────────────────────────────────

// In web mode the receiveLoop spawns webPlayoutLoop, which forwards raw
// Opus payloads from the jitter buffer to the WebAudioBridge for streaming
// to the browser. PortAudio is not active and playoutOneFrame is not used.

func TestWebPlayoutLoop_ForwardsRawOpus(t *testing.T) {
	cfg := newSilentComms()
	rt, _ := newReceiveRuntime()

	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())
	rt.webBridge = bridge

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB, 0xCC})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.webPlayoutLoop(ctx, jb, rt)

	// The raw Opus bytes should arrive on the bridge's RX channel.
	select {
	case frame := <-bridge.RxFrames():
		if len(frame) != 3 || frame[0] != 0xAA || frame[1] != 0xBB || frame[2] != 0xCC {
			t.Errorf("unexpected frame data: %v", frame)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for raw Opus frame on web bridge")
	}
}

// TestWebPlayoutLoop_MultipleFrames verifies that a sequence of frames is
// streamed through the web bridge in order.
func TestWebPlayoutLoop_MultipleFrames(t *testing.T) {
	cfg := newSilentComms()

	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)

	rt := &CommsRuntime{ports: []*portChannel{pc}}

	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())
	rt.webBridge = bridge

	jb := newRTPJitterBuffer(1, 10)
	for i := 0; i < 5; i++ {
		jb.push(uint16(i), []byte{byte(i)})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go cfg.webPlayoutLoop(ctx, jb, rt)

	for i := 0; i < 5; i++ {
		select {
		case frame := <-bridge.RxFrames():
			if len(frame) != 1 || frame[0] != byte(i) {
				t.Errorf("frame %d: got %v, want [%d]", i, frame, i)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out waiting for frame %d", i)
		}
	}
}

// TestWebPlayoutLoop_DeliversWhileBroadcasting verifies that the web
// playout loop has no half-duplex suppression: frames flow to the bridge
// even while the local node is broadcasting.
func TestWebPlayoutLoop_DeliversWhileBroadcasting(t *testing.T) {
	cfg := newSilentComms()
	rt, _ := newReceiveRuntime()

	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())
	rt.webBridge = bridge
	rt.broadcasting.Store(true)

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0x01})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cfg.webPlayoutLoop(ctx, jb, rt)

	select {
	case <-bridge.RxFrames():
		// OK — frame delivered as expected.
	case <-ctx.Done():
		t.Error("bridge should receive frames in web mode even while broadcasting")
	}
}

// ─── consecutive PLC limit tests ─────────────────────────────────────────────

func TestPlayoutOneFrame_ConsecutivePLCLimit(t *testing.T) {
	// Set up a jitter buffer where the stream is active (recent lastPush)
	// but all subsequent frames are missing. playoutOneFrame should emit
	// exactly maxConsecutivePLC PLC frames followed by silence.
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{fillValue: 99, returnN: frameSize}

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1, lastPush set

	cfg := &CommsConfig{Log: zerolog.Nop()}

	// First maxConsecutivePLC calls should produce non-zero PLC samples.
	for i := 0; i < maxConsecutivePLC; i++ {
		out := driveOneFrame(cfg, pc, rt, jb)
		if isAllZero(out) {
			t.Fatalf("frame %d: expected PLC samples; got silence", i)
		}
	}

	// All subsequent calls should return silence.
	for i := 0; i < 5; i++ {
		out := driveOneFrame(cfg, pc, rt, jb)
		if !isAllZero(out) {
			t.Errorf("post-cap frame %d: expected silence; got non-zero samples", i)
		}
	}
}

func TestPlayoutOneFrame_ConsecutivePLCResets(t *testing.T) {
	// After a burst of PLC, decoding a real frame should reset the
	// consecutivePLC counter so a subsequent gap produces PLC again.
	rt, pc := newReceiveRuntime()
	rt.decoder = &mockDecoder{fillValue: 99, returnN: frameSize}

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1, lastPush set

	cfg := &CommsConfig{Log: zerolog.Nop()}

	// Drive a few PLC frames. Each call advances expected via
	// advancePastLocked → expected goes 1→2→3→4 across these 3 calls.
	for i := 0; i < 3; i++ {
		_ = driveOneFrame(cfg, pc, rt, jb)
	}

	if pc.consecutivePLC == 0 {
		t.Fatal("expected consecutivePLC > 0 after PLC frames")
	}

	// Push a real packet at the current expected cursor (4) so the next
	// pop returns it directly. Anything ahead of expected is buffered but
	// not popped until the cursor advances to its slot.
	jb.push(4, []byte{0xBB})

	out := driveOneFrame(cfg, pc, rt, jb)
	if isAllZero(out) {
		t.Fatal("expected decoded samples after pushing real frame")
	}

	if pc.consecutivePLC != 0 {
		t.Errorf("consecutivePLC should reset to 0 after a real frame; got %d", pc.consecutivePLC)
	}
}

// ─── jitter buffer overflow counter test ─────────────────────────────────────

func TestJitterBuffer_OverflowCounter(t *testing.T) {
	jb := newRTPJitterBuffer(1, 4)

	// Fill buffer to maxDepth with seqs 0-3.
	for i := 0; i < 4; i++ {
		if !jb.push(uint16(i), []byte{byte(i)}) {
			t.Fatalf("push(%d) failed unexpectedly", i)
		}
	}

	// Push a duplicate while the buffer is full. The duplicate is rejected
	// AND count >= maxDepth, so the overflow counter should increment.
	if jb.push(0, []byte{0xFF}) {
		t.Error("duplicate push should have failed")
	}

	if got := jb.overflows.Load(); got != 1 {
		t.Errorf("expected overflows=1; got %d", got)
	}

	// Push more duplicates — each should increment.
	jb.push(1, []byte{0xFF})
	jb.push(2, []byte{0xFF})
	jb.push(3, []byte{0xFF})

	if got := jb.overflows.Load(); got != 4 {
		t.Errorf("expected overflows=4; got %d", got)
	}
}
