package ptt

import (
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// newTestRuntime returns a PTTRuntime wired with the provided stream and a
// small playback buffer, ready for transmission tests.
func newTestRuntime(stream AudioStream) *PTTRuntime {
	rt := &PTTRuntime{
		broadcastStream: stream,
		playbackBuffer:  make(chan []float32, 4),
		beepBufferStart: make([]float32, 32),
		beepBufferStop:  make([]float32, 32),
	}
	// Pre-populate beeps with distinguishable values.
	for i := range rt.beepBufferStart {
		rt.beepBufferStart[i] = 0.5
	}

	for i := range rt.beepBufferStop {
		rt.beepBufferStop[i] = -0.5
	}

	return rt
}

// newSilentPTT returns a PTTConfig with a no-op logger for use in tests.
func newSilentPTT() *PTTConfig {
	return &PTTConfig{Log: zerolog.Nop()}
}

// ─── isBroadcasting ──────────────────────────────────────────────────────────

func TestIsBroadcasting_FalseByDefault(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	ptt := newSilentPTT()

	if ptt.isBroadcasting(rt) {
		t.Error("expected isBroadcasting=false on a fresh runtime")
	}
}

// ─── drainPlaybackBuffer ─────────────────────────────────────────────────────

func TestDrainPlaybackBuffer(t *testing.T) {
	rt := newTestRuntime(&mockStream{})
	ptt := newSilentPTT()

	rt.playbackBuffer <- make([]float32, 32)

	rt.playbackBuffer <- make([]float32, 32)

	ptt.drainPlaybackBuffer(rt)

	if len(rt.playbackBuffer) != 0 {
		t.Errorf("expected empty buffer; got depth %d", len(rt.playbackBuffer))
	}
}

// ─── beginTransmission ───────────────────────────────────────────────────────

func TestBeginTransmission_StartsStream(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(ms)
	ptt := newSilentPTT()

	go ptt.beginTransmission(rt) // runs in goroutine due to sleep

	time.Sleep(400 * time.Millisecond)

	if ms.startCalls == 0 {
		t.Error("expected broadcastStream.Start() to be called")
	}

	if !ptt.isBroadcasting(rt) {
		t.Error("expected isBroadcasting=true after beginTransmission")
	}
}

func TestBeginTransmission_Idempotent(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(ms)
	ptt := newSilentPTT()

	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	go ptt.beginTransmission(rt) // second call should be a no-op

	time.Sleep(50 * time.Millisecond)

	if ms.startCalls != 1 {
		t.Errorf("expected Start() called once; got %d", ms.startCalls)
	}
}

func TestBeginTransmission_PlaysStartBeep(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(ms)
	ptt := newSilentPTT()

	go ptt.beginTransmission(rt)
	// Give the goroutine time to queue the beep but before the 200ms sleep.
	time.Sleep(50 * time.Millisecond)

	if len(rt.playbackBuffer) == 0 {
		t.Error("expected start beep to be queued to playback buffer")
	}
}

// ─── endTransmission ─────────────────────────────────────────────────────────

func TestEndTransmission_StopsStream(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(ms)
	ptt := newSilentPTT()

	// Start first.
	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	ptt.endTransmission(rt)

	if ms.stopCalls == 0 {
		t.Error("expected broadcastStream.Stop() to be called")
	}

	if ptt.isBroadcasting(rt) {
		t.Error("expected isBroadcasting=false after endTransmission")
	}
}

func TestEndTransmission_PlaysStopBeep(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(ms)
	ptt := newSilentPTT()

	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	// Drain the start beep first.
	ptt.drainPlaybackBuffer(rt)

	ptt.endTransmission(rt)

	if len(rt.playbackBuffer) == 0 {
		t.Error("expected stop beep to be queued to playback buffer")
	}
}

func TestEndTransmission_Idempotent(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(ms)
	ptt := newSilentPTT()

	// Not broadcasting yet — endTransmission should be a no-op.
	ptt.endTransmission(rt)

	if ms.stopCalls != 0 {
		t.Errorf("expected Stop() not called; got %d", ms.stopCalls)
	}
}

// ─── reopenBroadcast closure ─────────────────────────────────────────────────

func TestBeginTransmission_UsesReopenWhenStreamNil(t *testing.T) {
	ms := &mockStream{}
	rt := newTestRuntime(nil) // no stream initially
	ptt := newSilentPTT()

	reopenCalled := false
	rt.reopenBroadcast = func() error {
		reopenCalled = true
		rt.broadcastStream = ms

		return nil
	}

	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	if !reopenCalled {
		t.Error("expected reopenBroadcast to be called when stream is nil")
	}

	if ms.startCalls == 0 {
		t.Error("expected Start() to be called after reopen")
	}
}

// ─── beginTransmission: Start() error recovery paths ─────────────────────────

func TestBeginTransmission_StreamStartError_ReopensSuccessfully(t *testing.T) {
	failStream := &mockStream{startErr: errors.New("device busy")}
	goodStream := &mockStream{}
	rt := newTestRuntime(failStream)
	ptt := newSilentPTT()

	rt.reopenBroadcast = func() error {
		rt.broadcastStream = goodStream

		return nil
	}

	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	if goodStream.startCalls == 0 {
		t.Error("expected new stream Start() to be called after successful reopen")
	}

	if !ptt.isBroadcasting(rt) {
		t.Error("expected broadcasting=true after reopen succeeds and new Start() succeeds")
	}
}

func TestBeginTransmission_StreamStartError_ReopenFails(t *testing.T) {
	failStream := &mockStream{startErr: errors.New("device busy")}
	rt := newTestRuntime(failStream)
	ptt := newSilentPTT()

	rt.reopenBroadcast = func() error {
		return errors.New("reopen failed")
	}

	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	if ptt.isBroadcasting(rt) {
		t.Error("expected broadcasting=false when both Start() and reopenBroadcast fail")
	}
}

func TestBeginTransmission_StreamStartError_SecondStartFails(t *testing.T) {
	// Both the original stream and the reopened stream fail to Start().
	failStream := &mockStream{startErr: errors.New("device busy")}
	alsoFailStream := &mockStream{startErr: errors.New("still busy")}
	rt := newTestRuntime(failStream)
	ptt := newSilentPTT()

	rt.reopenBroadcast = func() error {
		rt.broadcastStream = alsoFailStream

		return nil
	}

	go ptt.beginTransmission(rt)

	time.Sleep(400 * time.Millisecond)

	if ptt.isBroadcasting(rt) {
		t.Error("expected broadcasting=false when reopened stream also fails Start()")
	}
}
