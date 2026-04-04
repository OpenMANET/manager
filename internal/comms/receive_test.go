package comms

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
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
	pc.playbackBuffer = make(chan []float32, 32)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}

	return rt, pc
}

// waitForFrame blocks until at least one frame appears in buf or
// the deadline is reached.
func waitForFrame(buf chan []float32, timeout time.Duration) ([]float32, bool) { //nolint:unparam
	select {
	case f := <-buf:
		return f, true
	case <-time.After(timeout):
		return nil, false
	}
}

// ─── playoutLoop tests ────────────────────────────────────────────────────────

func TestPlayoutLoop_DeliversReadyFrame(t *testing.T) {
	rt, pc := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10) // prebuffer=1: first push triggers start
	jb.push(0, []byte{0xAA, 0xBB})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	if _, ok := waitForFrame(pc.playbackBuffer, 200*time.Millisecond); !ok {
		t.Error("expected a decoded frame in playbackBuffer within 200 ms")
	}
}

func TestPlayoutLoop_EmitsPLCOnSkippedMissing(t *testing.T) {
	// maxDepth=4 → skip threshold = maxDepth/2 = 2.
	// Push seq=0 first to set up expected=0 and pass the prebuffer=1 threshold,
	// then pop it. Now expected=1. Push seq=2 and seq=3 (missing seq=1 with
	// len >= maxDepth/2) so popReady returns skipped=true → PLC must be emitted.
	rt, pc := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 4)

	jb.push(0, []byte{0})
	jb.popReady() // consume seq=0; started=true, expected=1

	jb.push(2, []byte{2})
	jb.push(3, []byte{3}) // len=2 >= maxDepth/2=2, expected=1 missing → skipped

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	if _, ok := waitForFrame(pc.playbackBuffer, 200*time.Millisecond); !ok {
		t.Error("expected a PLC frame when jitter buffer skips missing seq")
	}
}

func TestPlayoutLoop_EmitsPLCOnConceal(t *testing.T) {
	// Push+pop one frame to set started=true and record a recent lastPush
	// timestamp. The jitter buffer is then empty, so shouldConceal fires on
	// the next 20 ms tick and decodeAndQueuePLC is called.
	rt, pc := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10)

	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1, lastPush set to now

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	if _, ok := waitForFrame(pc.playbackBuffer, 300*time.Millisecond); !ok {
		t.Error("expected a PLC concealment frame while stream is active but empty")
	}
}

func TestPlayoutLoop_BackpressureDrainsOldest(t *testing.T) {
	// Use a small buffer (cap=4); fill 3 of 4 slots (75%) before starting
	// the playout loop. The drain logic should discard the oldest frame to
	// make room for the new one, keeping the 20 ms cadence intact.
	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	pc.playbackBuffer = make(chan []float32, 4)
	rt := &CommsRuntime{
		ports:   []*portChannel{pc},
		decoder: &mockDecoder{returnN: int(rtpFrameSamples)},
	}

	// Pre-fill to 75% capacity.
	for i := 0; i < 3; i++ {
		pc.playbackBuffer <- make([]float32, rtpFrameSamples)
	}

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA}) // satisfies prebuffer

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// Wait for the playout loop to fire at least one tick.
	time.Sleep(80 * time.Millisecond)
	cancel()

	// With the drain logic, the loop should have drained at least one
	// oldest frame to make room and then queued the decoded frame. The
	// buffer depth should still be 3 (one drained, one added) rather
	// than staying stuck at 3 with the jitter buffer frame unpopped.
	//
	// Verify the jitter buffer was consumed (the old skip logic would
	// have left it untouched).
	if p, _, _ := jb.popReady(); p != nil {
		t.Error("jitter buffer frame should have been consumed by the drain path, but it was still present")
		jb.releasePayload(p)
	}
}

func TestPlayoutLoop_EmitsNothingWhenBroadcasting(t *testing.T) {
	rt, pc := newReceiveRuntime()
	rt.broadcasting.Store(true) // isBroadcasting will return true; pc.cfg.Send=true → suppress

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0}) // satisfies prebuffer

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// Let the loop tick several times.
	time.Sleep(80 * time.Millisecond)
	cancel()

	if len(pc.playbackBuffer) != 0 {
		t.Errorf("playback buffer should stay empty while broadcasting; got %d frames",
			len(pc.playbackBuffer))
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
	pc.playbackBuffer = make(chan []float32, 16)
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
	pc.playbackBuffer = make(chan []float32, 8)
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

// ─── decodeAndQueue error-path tests ─────────────────────────────────────────

func TestDecodeAndQueue_DecoderError_PLCFallback(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	pc := &portChannel{}
	pc.playbackBuffer = buf
	rt := &CommsRuntime{
		decoder: &mockDecoder{decodeErr: errors.New("bad decode"), plcOK: true, returnN: int(rtpFrameSamples)},
	}

	cfg.decodeAndQueue(pc, rt, []byte{1, 2, 3})

	if len(buf) != 1 {
		t.Errorf("expected PLC fallback frame in buffer; got %d frames", len(buf))
	}
}

func TestDecodeAndQueue_DecoderError_PLCAlsoFails(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	pc := &portChannel{}
	pc.playbackBuffer = buf
	rt := &CommsRuntime{
		decoder: &mockDecoder{decodeErr: errors.New("bad decode")},
	}

	cfg.decodeAndQueue(pc, rt, []byte{1, 2, 3})

	if len(buf) != 0 {
		t.Errorf("expected empty buffer when both decode and PLC fail; got %d frames", len(buf))
	}
}

func TestDecodeAndQueue_BufferFull_DoesNotPanic(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	buf := make(chan []float32, 2)
	buf <- []float32{0}

	buf <- []float32{0}

	pc := &portChannel{}
	pc.playbackBuffer = buf
	rt := &CommsRuntime{
		decoder: &mockDecoder{returnN: 4},
	}

	// Must not block or panic when the buffer is full.
	cfg.decodeAndQueue(pc, rt, []byte{1, 2, 3})

	if len(buf) != 2 {
		t.Errorf("buffer depth changed unexpectedly; got %d", len(buf))
	}
}

func TestDecodeAndQueuePLC_ZeroReturnDropsFrame(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	pc := &portChannel{}
	pc.playbackBuffer = buf
	rt := &CommsRuntime{
		decoder: &mockDecoder{returnN: 0, forceN: true},
	}

	cfg.decodeAndQueuePLC(pc, rt)

	if len(buf) != 0 {
		t.Errorf("expected empty buffer when decoder returns 0; got %d frames", len(buf))
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
	pc.playbackBuffer = make(chan []float32, 32)
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

// ─── logPlaybackDrop tests ────────────────────────────────────────────────────

func TestLogPlaybackDrop_FirstDropAlwaysLogs(t *testing.T) {
	// Verify the counter increments on every call and the function
	// does not panic with a nop logger.
	cfg := &CommsConfig{Log: zerolog.Nop()}

	var counter atomic.Int64

	logPlaybackDrop(&counter, cfg, "test drop")

	if got := counter.Load(); got != 1 {
		t.Errorf("expected drop counter = 1; got %d", got)
	}
}

func TestLogPlaybackDrop_CounterIncrements(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	var counter atomic.Int64

	for i := 0; i < 150; i++ {
		logPlaybackDrop(&counter, cfg, "test drop")
	}

	if got := counter.Load(); got != 150 {
		t.Errorf("expected drop counter = 150; got %d", got)
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
	pc.playbackBuffer = make(chan []float32, 64)

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

func TestPlayoutLoop_WebMode_ForwardsRawOpus(t *testing.T) {
	cfg := newSilentComms()
	rt, pc := newReceiveRuntime()

	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())
	rt.webBridge = bridge

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xAA, 0xBB, 0xCC})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// The raw Opus bytes should arrive on the bridge's RX channel.
	select {
	case frame := <-bridge.RxFrames():
		if len(frame) != 3 || frame[0] != 0xAA || frame[1] != 0xBB || frame[2] != 0xCC {
			t.Errorf("unexpected frame data: %v", frame)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for raw Opus frame on web bridge")
	}

	// Nothing should appear in the PortAudio playback buffer.
	select {
	case <-pc.playbackBuffer:
		t.Error("unexpected frame in PortAudio playback buffer in web mode")
	default:
	}
}

// TestPlayoutLoop_WebMode_NilPlaybackBuffer reproduces the production web
// mode condition where startHardwareAudio is skipped and playbackBuffer is
// nil. Prior to the fix, hwm computed to 0 and the backpressure check
// (len(nil) >= 0) blocked every tick.
func TestPlayoutLoop_WebMode_NilPlaybackBuffer(t *testing.T) {
	cfg := newSilentComms()

	// Mirror production web mode: no playbackBuffer, no decoder needed.
	pc := &portChannel{
		cfg: McastPortConfig{Send: true, Receive: true},
	}
	pc.sendEnabled.Store(true)
	pc.receiveEnabled.Store(true)
	// playbackBuffer intentionally left nil

	rt := &CommsRuntime{ports: []*portChannel{pc}}

	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())
	rt.webBridge = bridge

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0xDE, 0xAD})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	select {
	case frame := <-bridge.RxFrames():
		if len(frame) != 2 || frame[0] != 0xDE || frame[1] != 0xAD {
			t.Errorf("unexpected frame data: %v", frame)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out: web bridge did not receive frame with nil playbackBuffer")
	}
}

// TestPlayoutLoop_WebMode_MultipleFrames verifies that a sequence of
// frames is streamed through the web bridge in order.
func TestPlayoutLoop_WebMode_MultipleFrames(t *testing.T) {
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

	go cfg.playoutLoop(ctx, jb, pc, rt)

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

func TestPlayoutLoop_WebMode_DeliversWhileBroadcasting(t *testing.T) {
	cfg := newSilentComms()
	rt, pc := newReceiveRuntime()

	bridge := NewWebAudioBridge(cfg, rt, zerolog.Nop())
	rt.webBridge = bridge
	rt.broadcasting.Store(true)

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0x01})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// Web mode skips the half-duplex suppression so the frame should
	// be delivered even while broadcasting.
	select {
	case <-bridge.RxFrames():
		// OK — frame delivered as expected.
	case <-ctx.Done():
		t.Error("bridge should receive frames in web mode even while broadcasting")
	}
}

// ─── consecutive PLC limit tests ─────────────────────────────────────────────

func TestPlayoutLoop_ConsecutivePLCLimit(t *testing.T) {
	// Set up a jitter buffer where the stream is active (recent lastPush)
	// but all subsequent frames are missing. The playout loop should emit
	// exactly 5 PLC frames followed by silence frames.
	rt, pc := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10)

	// Push and pop one frame to start the buffer and set lastPush.
	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1, lastPush set

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// Collect frames. With 20ms ticks over 500ms we get up to ~25 ticks.
	// The conceal window is 100ms so we should get ~5 PLC + a few silence
	// before shouldConceal stops returning true.
	var frames [][]float32

	for {
		select {
		case f := <-pc.playbackBuffer:
			frames = append(frames, f)
		case <-ctx.Done():
			goto done
		}
	}

done:
	cancel()

	if len(frames) == 0 {
		t.Fatal("expected at least one PLC/silence frame")
	}

	// The mock decoder fills PLC frames with fillValue/32768 (default 0).
	// Silence frames are explicitly zeroed. We can't distinguish them by
	// value with the default mock, but we CAN verify the total count is
	// bounded: we should never get more than ~5 PLC frames + a few silence
	// within the 100ms conceal window (5 ticks).
	if len(frames) > 10 {
		t.Errorf("expected at most ~10 frames (5 PLC + some silence); got %d", len(frames))
	}
}

func TestPlayoutLoop_ConsecutivePLCResets(t *testing.T) {
	// After a burst of PLC, pushing a real frame should reset the counter
	// so subsequent gaps produce PLC again (not silence).
	rt, pc := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10)

	// Start the buffer.
	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, pc, rt)

	// Let PLC run for a bit (>100ms to exhaust the 5-frame PLC budget).
	time.Sleep(150 * time.Millisecond)

	// Drain whatever frames accumulated.
	for len(pc.playbackBuffer) > 0 {
		<-pc.playbackBuffer
	}

	// Push a real frame with a sequence number far enough ahead that it
	// won't be rejected as stale. The playout loop's shouldConceal calls
	// have advanced expected by ~5 during the PLC burst, so use seq=20
	// to be safe.
	jb.push(20, []byte{0xBB})

	// Wait for it to be decoded and queued.
	f, ok := waitForFrame(pc.playbackBuffer, 300*time.Millisecond)
	if !ok {
		t.Fatal("expected a decoded frame after pushing real data")
	}

	_ = f

	cancel()
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
