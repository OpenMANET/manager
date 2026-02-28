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

func newReceiveRuntime() *CommsRuntime {
	return &CommsRuntime{
		playbackBuffer: make(chan []float32, 32),
		decoder:        &mockDecoder{returnN: int(rtpFrameSamples)},
	}
}

// waitForFrame blocks until at least one frame appears in rt.playbackBuffer or
// the deadline is reached.
func waitForFrame(rt *CommsRuntime, timeout time.Duration) ([]float32, bool) { //nolint:unparam
	select {
	case f := <-rt.playbackBuffer:
		return f, true
	case <-time.After(timeout):
		return nil, false
	}
}

// ─── playoutLoop tests ────────────────────────────────────────────────────────

func TestPlayoutLoop_DeliversReadyFrame(t *testing.T) {
	rt := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10) // prebuffer=1: first push triggers start
	jb.push(0, []byte{0xAA, 0xBB})

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, rt)

	if _, ok := waitForFrame(rt, 200*time.Millisecond); !ok {
		t.Error("expected a decoded frame in playbackBuffer within 200 ms")
	}
}

func TestPlayoutLoop_EmitsPLCOnSkippedMissing(t *testing.T) {
	// maxDepth=4 → skip threshold = maxDepth/2 = 2.
	// Push seq=0 first to set up expected=0 and pass the prebuffer=1 threshold,
	// then pop it. Now expected=1. Push seq=2 and seq=3 (missing seq=1 with
	// len >= maxDepth/2) so popReady returns skipped=true → PLC must be emitted.
	rt := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 4)

	jb.push(0, []byte{0})
	jb.popReady() // consume seq=0; started=true, expected=1

	jb.push(2, []byte{2})
	jb.push(3, []byte{3}) // len=2 >= maxDepth/2=2, expected=1 missing → skipped

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, rt)

	if _, ok := waitForFrame(rt, 200*time.Millisecond); !ok {
		t.Error("expected a PLC frame when jitter buffer skips missing seq")
	}
}

func TestPlayoutLoop_EmitsPLCOnConceal(t *testing.T) {
	// Push+pop one frame to set started=true and record a recent lastPush
	// timestamp. The jitter buffer is then empty, so shouldConceal fires on
	// the next 20 ms tick and decodeAndQueuePLC is called.
	rt := newReceiveRuntime()
	jb := newRTPJitterBuffer(1, 10)

	jb.push(0, []byte{0})
	jb.popReady() // started=true, expected=1, lastPush set to now

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, rt)

	if _, ok := waitForFrame(rt, 300*time.Millisecond); !ok {
		t.Error("expected a PLC concealment frame while stream is active but empty")
	}
}

func TestPlayoutLoop_EmitsNothingWhenBroadcasting(t *testing.T) {
	rt := newReceiveRuntime()
	rt.broadcasting = true // isBroadcasting will return true

	jb := newRTPJitterBuffer(1, 10)
	jb.push(0, []byte{0}) // satisfies prebuffer

	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cfg.playoutLoop(ctx, jb, rt)

	// Let the loop tick several times.
	time.Sleep(80 * time.Millisecond)
	cancel()

	if len(rt.playbackBuffer) != 0 {
		t.Errorf("playback buffer should stay empty while broadcasting; got %d frames",
			len(rt.playbackBuffer))
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
	rt := &CommsRuntime{
		playbackBuffer: make(chan []float32, 16),
		decoder:        &mockDecoder{returnN: int(rtpFrameSamples)},
		receiver:       newSwappableReceiver(reader),
	}
	rt.localIP.Store(localIP.String())

	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: false}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, rt)
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
	rt.receiver.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit")
	}

	if len(rt.playbackBuffer) != 0 {
		t.Errorf("own packets should be dropped; got %d frames in buffer",
			len(rt.playbackBuffer))
	}
}

func TestReceiveLoop_DropsMalformedRTP(t *testing.T) {
	// First packet is garbage; receiveLoop should log and continue rather than crash.
	garbled := mockPacket{data: []byte{0xFF, 0x00, 0x01}, src: &net.UDPAddr{IP: net.IPv4(1, 2, 3, 4)}}
	reader := newMockReader(garbled)

	rt := &CommsRuntime{
		playbackBuffer: make(chan []float32, 8),
		decoder:        &mockDecoder{},
		receiver:       newSwappableReceiver(reader),
	}

	cfg := &CommsConfig{Log: zerolog.Nop(), Loopback: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.receiveLoop(ctx, rt)
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
	rt.receiver.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit after malformed packet")
	}
}

// ─── decodeAndQueue error-path tests ─────────────────────────────────────────

func TestDecodeAndQueue_DecoderError(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	rt := &CommsRuntime{
		playbackBuffer: buf,
		decoder:        &mockDecoder{decodeErr: errors.New("bad decode")},
	}

	cfg.decodeAndQueue(rt, []byte{1, 2, 3})

	if len(buf) != 0 {
		t.Errorf("expected empty buffer on decode error; got %d frames", len(buf))
	}
}

func TestDecodeAndQueue_BufferFull_DoesNotPanic(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}

	buf := make(chan []float32, 2)
	buf <- []float32{0}

	buf <- []float32{0}

	rt := &CommsRuntime{
		playbackBuffer: buf,
		decoder:        &mockDecoder{returnN: 4},
	}

	// Must not block or panic when the buffer is full.
	cfg.decodeAndQueue(rt, []byte{1, 2, 3})

	if len(buf) != 2 {
		t.Errorf("buffer depth changed unexpectedly; got %d", len(buf))
	}
}

func TestDecodeAndQueuePLC_ZeroReturnDropsFrame(t *testing.T) {
	cfg := &CommsConfig{Log: zerolog.Nop()}
	buf := make(chan []float32, 4)
	rt := &CommsRuntime{
		playbackBuffer: buf,
		decoder:        &mockDecoder{returnN: 0, forceN: true},
	}

	cfg.decodeAndQueuePLC(rt)

	if len(buf) != 0 {
		t.Errorf("expected empty buffer when decoder returns 0; got %d frames", len(buf))
	}
}
