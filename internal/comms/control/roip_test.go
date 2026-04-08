package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/audiopool"
)

// errOpenDevice is a sentinel error for tests that need an opener that fails.
var errOpenDevice = errors.New("no HID device")

// ─── Helpers ──────────────────────────────────────────────────────────────────

// makeROIPReport builds a 5-byte OpenVLM HID report [ReportID, IR0, IR1, IR2, IR3]
// where the supplied gpioMask bit in IR1 is set (cosHigh=true) or cleared.
func makeROIPReport(gpioMask byte, cosHigh bool) []byte {
	ir1 := byte(0x00)
	if cosHigh {
		ir1 |= gpioMask
	}

	return []byte{0x00, 0x00, ir1, 0x00, 0x00}
}

// neverReceiving is a no-op isReceiving callback that always returns false.
func neverReceiving() bool { return false }

// neverBroadcasting is a no-op isBroadcasting callback that always returns false.
func neverBroadcasting() bool { return false }

// pushLoudFrames pushes n frames whose RMS energy exceeds the given threshold.
func pushLoudFrames(ch chan<- []float32, n int, amplitude float32) {
	for range n {
		frame := make([]float32, audiopool.FrameSize)
		for i := range frame {
			frame[i] = amplitude
		}

		ch <- frame
	}
}

// pushSilentFrames pushes n zero-valued frames.
func pushSilentFrames(ch chan<- []float32, n int) {
	for range n {
		ch <- make([]float32, audiopool.FrameSize)
	}
}

// staticMonitorOpener returns an openMonitor function that always provides the
// same pre-created channel. The closer is a no-op so tests retain control.
func staticMonitorOpener(frameCh chan []float32) func() (<-chan []float32, func(), error) {
	return func() (<-chan []float32, func(), error) {
		return frameCh, func() {}, nil
	}
}

// ─── COS path tests ───────────────────────────────────────────────────────────

func TestROIPSource_COS_OpenerError_ClosesChannelImmediately(t *testing.T) {
	src := NewROIPSourceWithOpener(
		openerFailing(errOpenDevice),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed after opener error; got event")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("channel not closed after opener error")
	}
}

func TestROIPSource_COS_COSLow_NoInitialEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, false)) // COS low

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 200*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected no events for initial COS LOW; got %v", events)
	}
}

func TestROIPSource_COS_COSHigh_EmitsPTTDown(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true))

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for PTTDown")
	}
}

func TestROIPSource_COS_HighThenLow_EmitsPTTDownThenPTTUp(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true))
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, false))

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 400*time.Millisecond)

	if len(events) < 2 {
		t.Fatalf("expected 2 events; got %d: %v", len(events), events)
	}

	if events[0] != PTTDown {
		t.Errorf("event[0]: got %v, want PTTDown", events[0])
	}

	if events[1] != PTTUp {
		t.Errorf("event[1]: got %v, want PTTUp", events[1])
	}
}

func TestROIPSource_COS_DuplicateHigh_NoExtraEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true))  // HIGH → PTTDown
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true))  // HIGH again → no event
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, false)) // LOW → PTTUp

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 400*time.Millisecond)

	if len(events) != 2 {
		t.Fatalf("expected exactly 2 events; got %d: %v", len(events), events)
	}
}

func TestROIPSource_COS_HalfDuplex_SuppressesPTTDownWhileReceiving(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true))  // COS HIGH while receiving
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, false)) // COS LOW

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask,
		func() bool { return true }, // network always receiving
		neverBroadcasting,
		zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 250*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected no events while network is receiving; got %v", events)
	}
}

func TestROIPSource_COS_HalfDuplex_EmitsAfterReceivingClears(t *testing.T) {
	// First COS HIGH whilst receiving is suppressed (and prevCOS reset to false).
	// After receiving stops, a second COS HIGH transition must emit PTTDown.
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true)) // HIGH while receiving → suppressed

	var receivingFlag atomic.Bool
	receivingFlag.Store(true)

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask, receivingFlag.Load, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	// Allow the source goroutine to consume and suppress the first HIGH.
	time.Sleep(50 * time.Millisecond)

	// Clear receiving flag first, then queue a new COS HIGH.
	receivingFlag.Store(false)

	mock.queueReport(makeROIPReport(ROIPDefaultCOSMask, true)) // should now emit PTTDown

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown after receiving cleared; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for PTTDown after receiving cleared")
	}
}

func TestROIPSource_COS_ContextCancel_ClosesChannel(t *testing.T) {
	mock := newMockHIDDevice() // empty queue — will block on Read

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := src.Events(ctx)

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed after context cancel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("channel not closed after context cancel")
	}
}

func TestROIPSource_COS_ReadError_ClosesChannel(t *testing.T) {
	errDev := &errHIDDevice{}

	src := NewROIPSourceWithOpener(
		openerReturning(errDev),
		ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed after read error")
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("channel not closed after read error")
	}
}

func TestROIPSource_COS_CustomGPIOMask(t *testing.T) {
	const gpio4Mask byte = 0x08

	mock := newMockHIDDevice()
	// GPIO3 bit set (0x04) — should NOT trigger with gpio4Mask.
	mock.queueReport([]byte{0x00, 0x00, 0x04, 0x00, 0x00})
	// GPIO4 bit set (0x08) — SHOULD trigger PTTDown.
	mock.queueReport([]byte{0x00, 0x00, 0x08, 0x00, 0x00})

	src := NewROIPSourceWithOpener(
		openerReturning(mock),
		gpio4Mask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown on GPIO4; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for PTTDown on custom GPIO mask")
	}
}

// ─── VOX path tests ───────────────────────────────────────────────────────────

// loudAmplitude is above roipDefaultVOXThresh (RMS of a constant 0.1 signal = 0.1 > 0.02).
const loudAmplitude float32 = 0.1

func TestROIPSource_VOX_BelowThreshold_NoEvent(t *testing.T) {
	frameCh := make(chan []float32, 32)

	// Push onset frames that are BELOW threshold (amplitude 0.005, threshold 0.02).
	for range ROIPVOXOnsetFrames + 2 {
		frame := make([]float32, audiopool.FrameSize)
		for i := range frame {
			frame[i] = 0.005
		}

		frameCh <- frame
	}

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 250*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected no events below threshold; got %v", events)
	}
}

func TestROIPSource_VOX_OnsetThreshold_PTTDown(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, ROIPVOXOnsetFrames+1, loudAmplitude)

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for PTTDown on VOX onset")
	}
}

func TestROIPSource_VOX_NonConsecutiveFrames_ResetsOnsetCounter(t *testing.T) {
	frameCh := make(chan []float32, 32)

	// Alternate loud / silent: onset counter should never reach ROIPVOXOnsetFrames.
	for range ROIPVOXOnsetFrames * 4 {
		pushLoudFrames(frameCh, 1, loudAmplitude)
		pushSilentFrames(frameCh, 1)
	}

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 250*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected no PTTDown for non-consecutive frames; got %v", events)
	}
}

func TestROIPSource_VOX_HalfDuplex_SuppressesWhileReceiving(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, ROIPVOXOnsetFrames+2, loudAmplitude)

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		ROIPDefaultVOXHold,
		func() bool { return true }, // network always receiving
		neverBroadcasting,
		zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 250*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected PTTDown suppressed while receiving; got %v", events)
	}
}

func TestROIPSource_VOX_TailHold_NoEarlyPTTUp(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, ROIPVOXOnsetFrames+1, loudAmplitude)

	holdTime := 300 * time.Millisecond

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		holdTime,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := src.Events(ctx)

	// First event must be PTTDown.
	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Fatalf("expected PTTDown; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for PTTDown")
	}

	// PTTUp must NOT arrive before the hold time expires.
	select {
	case ev := <-ch:
		t.Errorf("unexpected early event %v before hold time (%s)", ev, holdTime)
	case <-time.After(holdTime / 2):
		// Good — no premature PTTUp within half the hold window.
	}
}

func TestROIPSource_VOX_PTTUpAfterHoldTime(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, ROIPVOXOnsetFrames+1, loudAmplitude)
	// No more frames after onset: tap channel is empty → hold timer fires.

	holdTime := 80 * time.Millisecond

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		holdTime,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, holdTime+300*time.Millisecond)

	if len(events) < 2 {
		t.Fatalf("expected [PTTDown, PTTUp]; got %v", events)
	}

	if events[0] != PTTDown {
		t.Errorf("events[0]: got %v, want PTTDown", events[0])
	}

	if events[1] != PTTUp {
		t.Errorf("events[1]: got %v, want PTTUp", events[1])
	}
}

func TestROIPSource_VOX_PTTUpWhenReceivingWhileActive(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, ROIPVOXOnsetFrames+1, loudAmplitude)

	var receivingFlag atomic.Bool

	holdTime := 10 * time.Second // long enough that only isReceiving() triggers PTTUp

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		holdTime,
		receivingFlag.Load, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := src.Events(ctx)

	// Wait for PTTDown.
	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Fatalf("expected PTTDown; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for PTTDown")
	}

	// Simulate network RX starting.
	receivingFlag.Store(true)

	select {
	case ev := <-ch:
		if ev != PTTUp {
			t.Errorf("expected PTTUp on network RX; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for PTTUp after network RX started")
	}
}

func TestROIPSource_VOX_ContextCancel_ClosesChannel(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, ROIPVOXOnsetFrames+1, loudAmplitude)

	src := NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := src.Events(ctx)

	// Wait for PTTDown to confirm the source is in ACTIVE state, then cancel.
	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Fatalf("expected PTTDown; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for PTTDown before cancel")
	}

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed after context cancel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("channel not closed after context cancel")
	}
}
