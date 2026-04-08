package comms

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control"
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
		frame := make([]float32, frameSize)
		for i := range frame {
			frame[i] = amplitude
		}

		ch <- frame
	}
}

// pushSilentFrames pushes n zero-valued frames.
func pushSilentFrames(ch chan<- []float32, n int) {
	for range n {
		ch <- make([]float32, frameSize)
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
	src := control.NewROIPSourceWithOpener(
		openerFailing(errOpenDevice),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
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
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, false)) // COS low

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
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
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true))

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Errorf("expected control.PTTDown; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for control.PTTDown")
	}
}

func TestROIPSource_COS_HighThenLow_EmitsPTTDownThenPTTUp(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true))
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, false))

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 400*time.Millisecond)

	if len(events) < 2 {
		t.Fatalf("expected 2 events; got %d: %v", len(events), events)
	}

	if events[0] != control.PTTDown {
		t.Errorf("event[0]: got %v, want control.PTTDown", events[0])
	}

	if events[1] != control.PTTUp {
		t.Errorf("event[1]: got %v, want control.PTTUp", events[1])
	}
}

func TestROIPSource_COS_DuplicateHigh_NoExtraEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true))  // HIGH → control.PTTDown
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true))  // HIGH again → no event
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, false)) // LOW → PTTUp

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
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
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true))  // COS HIGH while receiving
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, false)) // COS LOW

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask,
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
	// After receiving stops, a second COS HIGH transition must emit control.PTTDown.
	mock := newMockHIDDevice()
	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true)) // HIGH while receiving → suppressed

	var receivingFlag atomic.Bool
	receivingFlag.Store(true)

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask, receivingFlag.Load, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	// Allow the source goroutine to consume and suppress the first HIGH.
	time.Sleep(50 * time.Millisecond)

	// Clear receiving flag first, then queue a new COS HIGH.
	receivingFlag.Store(false)

	mock.queueReport(makeROIPReport(control.ROIPDefaultCOSMask, true)) // should now emit control.PTTDown

	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Errorf("expected control.PTTDown after receiving cleared; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for control.PTTDown after receiving cleared")
	}
}

func TestROIPSource_COS_ContextCancel_ClosesChannel(t *testing.T) {
	mock := newMockHIDDevice() // empty queue — will block on Read

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
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

	src := control.NewROIPSourceWithOpener(
		openerReturning(errDev),
		control.ROIPDefaultCOSMask, neverReceiving, neverBroadcasting, zerolog.Nop(),
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
	// GPIO4 bit set (0x08) — SHOULD trigger control.PTTDown.
	mock.queueReport([]byte{0x00, 0x00, 0x08, 0x00, 0x00})

	src := control.NewROIPSourceWithOpener(
		openerReturning(mock),
		gpio4Mask, neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Errorf("expected control.PTTDown on GPIO4; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for control.PTTDown on custom GPIO mask")
	}
}

// ─── VOX path tests ───────────────────────────────────────────────────────────

// loudAmplitude is above roipDefaultVOXThresh (RMS of a constant 0.1 signal = 0.1 > 0.02).
const loudAmplitude float32 = 0.1

func TestROIPSource_VOX_BelowThreshold_NoEvent(t *testing.T) {
	frameCh := make(chan []float32, 32)

	// Push onset frames that are BELOW threshold (amplitude 0.005, threshold 0.02).
	for range control.ROIPVOXOnsetFrames + 2 {
		frame := make([]float32, frameSize)
		for i := range frame {
			frame[i] = 0.005
		}

		frameCh <- frame
	}

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		control.ROIPDefaultVOXHold,
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
	pushLoudFrames(frameCh, control.ROIPVOXOnsetFrames+1, loudAmplitude)

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		control.ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Errorf("expected control.PTTDown; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for control.PTTDown on VOX onset")
	}
}

func TestROIPSource_VOX_NonConsecutiveFrames_ResetsOnsetCounter(t *testing.T) {
	frameCh := make(chan []float32, 32)

	// Alternate loud / silent: onset counter should never reach control.ROIPVOXOnsetFrames.
	for range control.ROIPVOXOnsetFrames * 4 {
		pushLoudFrames(frameCh, 1, loudAmplitude)
		pushSilentFrames(frameCh, 1)
	}

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		control.ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 250*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected no control.PTTDown for non-consecutive frames; got %v", events)
	}
}

func TestROIPSource_VOX_HalfDuplex_SuppressesWhileReceiving(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, control.ROIPVOXOnsetFrames+2, loudAmplitude)

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		control.ROIPDefaultVOXHold,
		func() bool { return true }, // network always receiving
		neverBroadcasting,
		zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 250*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected control.PTTDown suppressed while receiving; got %v", events)
	}
}

func TestROIPSource_VOX_TailHold_NoEarlyPTTUp(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, control.ROIPVOXOnsetFrames+1, loudAmplitude)

	holdTime := 300 * time.Millisecond

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		holdTime,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := src.Events(ctx)

	// First event must be control.PTTDown.
	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Fatalf("expected control.PTTDown; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for control.PTTDown")
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
	pushLoudFrames(frameCh, control.ROIPVOXOnsetFrames+1, loudAmplitude)
	// No more frames after onset: tap channel is empty → hold timer fires.

	holdTime := 80 * time.Millisecond

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		holdTime,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, holdTime+300*time.Millisecond)

	if len(events) < 2 {
		t.Fatalf("expected [control.PTTDown, PTTUp]; got %v", events)
	}

	if events[0] != control.PTTDown {
		t.Errorf("events[0]: got %v, want control.PTTDown", events[0])
	}

	if events[1] != control.PTTUp {
		t.Errorf("events[1]: got %v, want control.PTTUp", events[1])
	}
}

func TestROIPSource_VOX_PTTUpWhenReceivingWhileActive(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, control.ROIPVOXOnsetFrames+1, loudAmplitude)

	var receivingFlag atomic.Bool

	holdTime := 10 * time.Second // long enough that only isReceiving() triggers PTTUp

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		holdTime,
		receivingFlag.Load, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := src.Events(ctx)

	// Wait for control.PTTDown.
	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Fatalf("expected control.PTTDown; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for control.PTTDown")
	}

	// Simulate network RX starting.
	receivingFlag.Store(true)

	select {
	case ev := <-ch:
		if ev != control.PTTUp {
			t.Errorf("expected control.PTTUp on network RX; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for PTTUp after network RX started")
	}
}

func TestROIPSource_VOX_ContextCancel_ClosesChannel(t *testing.T) {
	frameCh := make(chan []float32, 32)
	pushLoudFrames(frameCh, control.ROIPVOXOnsetFrames+1, loudAmplitude)

	src := control.NewROIPSourceWithMonitor(
		staticMonitorOpener(frameCh),
		control.ROIPDefaultVOXHold,
		neverReceiving, neverBroadcasting, zerolog.Nop(),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := src.Events(ctx)

	// Wait for control.PTTDown to confirm the source is in ACTIVE state, then cancel.
	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Fatalf("expected control.PTTDown; got %v", ev)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for control.PTTDown before cancel")
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
