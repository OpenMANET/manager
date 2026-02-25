package ptt

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func newCommsTestRuntime(dec AudioDecoder) *PTTRuntime {
	return &PTTRuntime{
		decoder:        dec,
		playbackBuffer: make(chan []float32, 8),
	}
}

// ─── decodeAndQueue ───────────────────────────────────────────────────────────

func TestDecodeAndQueue_Success(t *testing.T) {
	dec := &mockDecoder{fillValue: 16384} // 0.5 after /32768
	rt := newCommsTestRuntime(dec)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.decodeAndQueue(rt, []byte{0x01, 0x02, 0x03})

	if len(rt.playbackBuffer) != 1 {
		t.Fatalf("expected 1 frame queued; got %d", len(rt.playbackBuffer))
	}

	frame := <-rt.playbackBuffer
	if len(frame) == 0 {
		t.Error("expected non-empty PCM frame")
	}
	for i, v := range frame {
		if v < 0.49 || v > 0.51 {
			t.Errorf("frame[%d] = %f; want ~0.5", i, v)
			break
		}
	}
}

func TestDecodeAndQueue_DecoderError_Dropped(t *testing.T) {
	dec := &mockDecoder{decodeErr: errors.New("bad packet")}
	rt := newCommsTestRuntime(dec)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.decodeAndQueue(rt, []byte{0xff})

	if len(rt.playbackBuffer) != 0 {
		t.Error("expected no frame queued when decoder returns error")
	}
}

func TestDecodeAndQueue_BufferFull_Drops(t *testing.T) {
	dec := &mockDecoder{}
	rt := &PTTRuntime{
		decoder:        dec,
		playbackBuffer: make(chan []float32, 1),
	}
	rt.playbackBuffer <- make([]float32, frameSize)

	ptt := &PTTConfig{Log: zerolog.Nop()}
	ptt.decodeAndQueue(rt, []byte{0x01})

	if len(rt.playbackBuffer) != 1 {
		t.Error("expected buffer to remain at capacity after drop")
	}
}

// ─── decodeAndQueuePLC ────────────────────────────────────────────────────────

func TestDecodeAndQueuePLC_Success(t *testing.T) {
	dec := &mockDecoder{fillValue: -16384}
	rt := newCommsTestRuntime(dec)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.decodeAndQueuePLC(rt)

	if len(rt.playbackBuffer) != 1 {
		t.Fatalf("expected 1 PLC frame queued; got %d", len(rt.playbackBuffer))
	}
}

func TestDecodeAndQueuePLC_DecoderError_Dropped(t *testing.T) {
	dec := &mockDecoder{decodeErr: errors.New("plc error")}
	rt := newCommsTestRuntime(dec)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.decodeAndQueuePLC(rt)

	if len(rt.playbackBuffer) != 0 {
		t.Error("expected no frame queued when PLC decoder returns error")
	}
}

func TestDecodeAndQueuePLC_ZeroSamples_Dropped(t *testing.T) {
	dec := &mockDecoder{returnN: 0, forceN: true}
	rt := newCommsTestRuntime(dec)
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ptt.decodeAndQueuePLC(rt)

	if len(rt.playbackBuffer) != 0 {
		t.Error("expected no frame when decoder reports 0 samples")
	}
}

// ─── receiveLoop: UDP mode ────────────────────────────────────────────────────

func TestReceiveLoop_UDP_DecodesAndQueues(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03, 0x04}
	reader := newMockReader(mockPacket{
		data: payload,
		src:  udpSrc("10.0.0.1"),
	})

	dec := &mockDecoder{}
	rt := &PTTRuntime{
		decoder:        dec,
		receiver:       reader,
		localIP:        "10.0.0.2",
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{
		Log:      zerolog.Nop(),
		Protocol: protocolUDP,
		Loopback: true,
	}

	ctx := cancelAfterDrain(reader)
	ptt.receiveLoop(ctx, rt)

	if len(rt.playbackBuffer) != 1 {
		t.Errorf("expected 1 decoded frame; got %d", len(rt.playbackBuffer))
	}
}

func TestReceiveLoop_LoopbackDrop(t *testing.T) {
	payload := []byte{0x01, 0x02}
	reader := newMockReader(mockPacket{
		data: payload,
		src:  udpSrc("10.0.0.1"),
	})

	dec := &mockDecoder{}
	rt := &PTTRuntime{
		decoder:        dec,
		receiver:       reader,
		localIP:        "10.0.0.1", // same as src
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{
		Log:      zerolog.Nop(),
		Protocol: protocolUDP,
		Loopback: false,
	}

	ctx := cancelAfterDrain(reader)
	ptt.receiveLoop(ctx, rt)

	if len(rt.playbackBuffer) != 0 {
		t.Error("expected loopback packet to be dropped")
	}
}

func TestReceiveLoop_RTP_InvalidHeaderDropped(t *testing.T) {
	bad := make([]byte, rtpHeaderSize+4)
	bad[0] = 0x40 // not 0x80 or 0x81

	reader := newMockReader(mockPacket{data: bad, src: udpSrc("10.0.0.2")})

	dec := &mockDecoder{}
	rt := &PTTRuntime{
		decoder:        dec,
		receiver:       reader,
		localIP:        "10.0.0.1",
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{
		Log:      zerolog.Nop(),
		Protocol: protocolRTP,
		Loopback: true,
	}

	ctx := cancelAfterDrain(reader)
	ptt.receiveLoop(ctx, rt)

	if len(rt.playbackBuffer) != 0 {
		t.Error("expected invalid RTP packet to be dropped")
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func udpSrc(ip string) *net.UDPAddr {
	return &net.UDPAddr{IP: net.ParseIP(ip), Port: 1234}
}

// cancelAfterDrain returns a context that is cancelled once the reader's
// pre-loaded packet queue becomes empty, causing receiveLoop to exit cleanly.
func cancelAfterDrain(r *mockReader) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			time.Sleep(5 * time.Millisecond)
			r.mu.Lock()
			empty := len(r.packets) == 0
			r.mu.Unlock()
			if empty {
				cancel()
				r.Close()
				return
			}
		}
	}()
	return ctx
}

// ─── additional receiveLoop coverage ─────────────────────────────────────────

func TestReceiveLoop_LoopbackIPDrop(t *testing.T) {
	// src.IP.IsLoopback() should also be dropped when Loopback=false.
	payload := []byte{0x01, 0x02}
	reader := newMockReader(mockPacket{
		data: payload,
		src:  udpSrc("127.0.0.1"), // loopback address
	})

	dec := &mockDecoder{}
	rt := &PTTRuntime{
		decoder:        dec,
		receiver:       reader,
		localIP:        "10.0.0.5", // different from src
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{
		Log:      zerolog.Nop(),
		Protocol: protocolUDP,
		Loopback: false,
	}

	ctx := cancelAfterDrain(reader)
	ptt.receiveLoop(ctx, rt)

	if len(rt.playbackBuffer) != 0 {
		t.Error("expected loopback IP packet (127.0.0.1) to be dropped when Loopback=false")
	}
}

func TestReceiveLoop_UDP_AutoDetectRTP(t *testing.T) {
	// In UDP mode, receiveLoop should auto-detect and unwrap RTP framing.
	wrapCfg := &PTTConfig{}
	wrapRT := &PTTRuntime{rtpSeq: 1, rtpSSRC: 0xdeadbeef}
	rawPayload := []byte{0x01, 0x02, 0x03}
	rtpPacket := wrapCfg.wrapRTP(rawPayload, wrapRT)

	reader := newMockReader(mockPacket{
		data: rtpPacket,
		src:  udpSrc("10.0.0.3"),
	})

	dec := &mockDecoder{fillValue: 8192}
	rt := &PTTRuntime{
		decoder:        dec,
		receiver:       reader,
		localIP:        "10.0.0.1",
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{
		Log:      zerolog.Nop(),
		Protocol: protocolUDP, // UDP mode — auto-detect RTP
		Loopback: true,
	}

	ctx := cancelAfterDrain(reader)
	ptt.receiveLoop(ctx, rt)

	if len(rt.playbackBuffer) != 1 {
		t.Errorf("expected 1 decoded frame after UDP/RTP auto-detect; got %d", len(rt.playbackBuffer))
	}
}

// ─── rtpPlayoutLoop ───────────────────────────────────────────────────────────

func TestRtpPlayoutLoop_ReadyFrame(t *testing.T) {
	jb := newRTPJitterBuffer(1, 24) // prebuffer = 1
	jb.push(0, []byte{0x01, 0x02})

	dec := &mockDecoder{fillValue: 10000}
	rt := &PTTRuntime{
		decoder:        dec,
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ptt.rtpPlayoutLoop(ctx, jb, rt)

	if len(rt.playbackBuffer) == 0 {
		t.Error("expected at least one decoded frame queued by rtpPlayoutLoop")
	}
}

func TestRtpPlayoutLoop_SkipMissingAppliesPLC(t *testing.T) {
	// Build a jitter buffer that is past prebuffer, then position it so that
	// the expected sequence is missing and enough frames are buffered to skip.
	maxDepth := 4
	jb := newRTPJitterBuffer(1, maxDepth)

	// Prime the buffer: push seq 0 and pop it to advance expected to 1.
	jb.push(0, []byte{0x00})
	jb.popReady()

	// Push seq 2 and 3 — seq 1 (expected) is missing.
	jb.push(2, []byte{0x02})
	jb.push(3, []byte{0x03})

	dec := &mockDecoder{fillValue: 5000}
	rt := &PTTRuntime{
		decoder:        dec,
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	ptt.rtpPlayoutLoop(ctx, jb, rt)

	// The first pop should have skipped seq 1 and triggered a PLC frame.
	if len(rt.playbackBuffer) == 0 {
		t.Error("expected PLC frame queued when expected sequence was skipped")
	}
}

func TestRtpPlayoutLoop_ShouldConcealQueuesFrame(t *testing.T) {
	// Simulate an active stream that has a gap: started=true and lastPush is
	// recent, but the next expected packet is not yet available.
	jb := newRTPJitterBuffer(1, 24)
	// Push seq 0 to start the buffer, then pop it to advance expected to 1.
	jb.push(0, []byte{0x00})
	jb.popReady()
	// Push seq 1 to set a recent lastPush, then pop it (expected becomes 2).
	jb.push(1, []byte{0x01})
	jb.popReady()
	// Now jb.started=true, lastPush = recent, but seq 2 is not present.
	// shouldConceal(100ms) will return true immediately.

	dec := &mockDecoder{fillValue: 3000}
	rt := &PTTRuntime{
		decoder:        dec,
		playbackBuffer: make(chan []float32, 4),
	}
	ptt := &PTTConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	ptt.rtpPlayoutLoop(ctx, jb, rt)

	if len(rt.playbackBuffer) == 0 {
		t.Error("expected PLC concealment frame queued for active stream with gap")
	}
}
