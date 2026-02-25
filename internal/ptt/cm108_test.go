package ptt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// ─── mockHIDDevice ────────────────────────────────────────────────────────────

// mockHIDDevice satisfies HIDDevice.  Each call to Read returns the next
// queued report; when the queue is empty it blocks until Close is called.
type mockHIDDevice struct {
	reports    chan []byte
	closed     chan struct{}
	closeErr   error
	closeCalls int
}

func newMockHIDDevice() *mockHIDDevice {
	return &mockHIDDevice{
		reports: make(chan []byte, 16),
		closed:  make(chan struct{}),
	}
}

// queueReport enqueues a raw HID report for the next Read call.
func (m *mockHIDDevice) queueReport(report []byte) {
	m.reports <- report
}

// Close signals the device as closed, unblocking any pending Read.
func (m *mockHIDDevice) Close() error {
	m.closeCalls++
	select {
	case <-m.closed:
		// already closed
	default:
		close(m.closed)
	}

	return m.closeErr
}

// Read returns the next queued report or blocks until Close.
// If no report is queued and the device is closed it returns an error.
func (m *mockHIDDevice) Read(b []byte) (int, error) {
	select {
	case report := <-m.reports:
		n := copy(b, report)

		return n, nil
	case <-m.closed:
		return 0, errors.New("device closed")
	}
}

// ─── mockHIDDevice with forced read error ─────────────────────────────────────

// errHIDDevice always returns an error from Read.
type errHIDDevice struct{}

func (e *errHIDDevice) Read(_ []byte) (int, error) { return 0, errors.New("read error") }
func (e *errHIDDevice) Close() error               { return nil }

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeCM108Report builds a 5-byte CM108 report [ReportID, IR0, IR1, IR2, IR3].
// gpio3High sets or clears bit 2 of IR1 (GPIO3).
func makeCM108Report(gpio3High bool) []byte {
	ir1 := byte(0x00)

	if gpio3High {
		ir1 |= cm108GPIO3Mask
	}

	return []byte{0x00, 0x00, ir1, 0x00, 0x00}
}

// collectEvents drains ch for at most timeout, returning all received events.
func collectEvents(ch <-chan PTTEvent, timeout time.Duration) []PTTEvent {
	var events []PTTEvent
	deadline := time.After(timeout)

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}

			events = append(events, ev)
		case <-deadline:
			return events
		}
	}
}

// ─── opener helpers ───────────────────────────────────────────────────────────

func openerReturning(dev HIDDevice) HIDOpener {
	return func(_, _ uint16) (HIDDevice, error) {
		return dev, nil
	}
}

func openerFailing(err error) HIDOpener {
	return func(_, _ uint16) (HIDDevice, error) {
		return nil, err
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestCM108Source_OpenerError_ClosesChannelImmediately(t *testing.T) {
	src := newCM108SourceWithOpener(openerFailing(errors.New("no device")), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed, received unexpected event")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("channel was not closed after opener error")
	}
}

func TestCM108Source_OpenerCalledWithCorrectVIDPID(t *testing.T) {
	var gotVID, gotPID uint16

	mock := newMockHIDDevice()
	opener := func(vid, pid uint16) (HIDDevice, error) {
		gotVID = vid
		gotPID = pid

		return mock, nil
	}

	src := newCM108SourceWithOpener(opener, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel so the goroutine exits

	src.Events(ctx)
	time.Sleep(50 * time.Millisecond) // let the goroutine start

	if gotVID != cm108VendorID {
		t.Errorf("VendorID: got 0x%04X, want 0x%04X", gotVID, cm108VendorID)
	}

	if gotPID != cm108ProductID {
		t.Errorf("ProductID: got 0x%04X, want 0x%04X", gotPID, cm108ProductID)
	}
}

func TestCM108Source_GPIO3_LowReport_NoEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeCM108Report(false)) // GPIO3 LOW – no state change

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	events := collectEvents(ch, 200*time.Millisecond)
	if len(events) != 0 {
		t.Errorf("expected no events for unchanged LOW state; got %v", events)
	}
}

func TestCM108Source_GPIO3_HighReport_EmitsPTTDown(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeCM108Report(true)) // GPIO3 HIGH → PTTDown

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown; got %v", ev)
		}
	case <-time.After(400*time.Millisecond):
		t.Error("timed out waiting for PTTDown event")
	}
}

func TestCM108Source_GPIO3_HighThenLow_EmitsPTTDownThenPTTUp(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeCM108Report(true))  // press  → PTTDown
	mock.queueReport(makeCM108Report(false)) // release → PTTUp

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	var got []PTTEvent

	for range 2 {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("channel closed before receiving expected events")
			}

			got = append(got, ev)
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out after receiving %d event(s)", len(got))
		}
	}

	if got[0] != PTTDown {
		t.Errorf("event[0]: got %v, want PTTDown", got[0])
	}

	if got[1] != PTTUp {
		t.Errorf("event[1]: got %v, want PTTUp", got[1])
	}
}

func TestCM108Source_DuplicateReports_NoDuplicateEvents(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeCM108Report(true)) // press
	mock.queueReport(makeCM108Report(true)) // duplicate (no change)
	mock.queueReport(makeCM108Report(true)) // duplicate (no change)
	mock.queueReport(makeCM108Report(false)) // release

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	events := collectEvents(ch, 500*time.Millisecond)

	// Expect exactly PTTDown then PTTUp.
	if len(events) != 2 {
		t.Fatalf("expected 2 events; got %d: %v", len(events), events)
	}

	if events[0] != PTTDown {
		t.Errorf("events[0]: got %v, want PTTDown", events[0])
	}

	if events[1] != PTTUp {
		t.Errorf("events[1]: got %v, want PTTUp", events[1])
	}
}

func TestCM108Source_ReadError_ClosesChannel(t *testing.T) {
	src := newCM108SourceWithOpener(openerReturning(&errHIDDevice{}), zerolog.Nop())

	ctx := context.Background()
	ch := src.Events(ctx)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed on read error")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out; channel not closed after read error")
	}
}

func TestCM108Source_ContextCancel_ClosesChannel(t *testing.T) {
	mock := newMockHIDDevice() // will block on Read until closed/cancelled

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	ch := src.Events(ctx)

	// Cancel context and close the device to unblock the Read call.
	cancel()
	mock.Close()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel closed; received unexpected event")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("channel was not closed after context cancellation")
	}
}

func TestCM108Source_ShortReport_SkippedWithoutPanic(t *testing.T) {
	mock := newMockHIDDevice()
	// Queue a 1-byte report (too short to decode), then a valid GPIO3 HIGH report.
	mock.reports <- []byte{0x00}
	mock.queueReport(makeCM108Report(true))

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}

		if ev != PTTDown {
			t.Errorf("expected PTTDown after short-report skip; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for PTTDown after short report")
	}
}

func TestCM108Source_DeviceCloseCalledOnShutdown(t *testing.T) {
	mock := newMockHIDDevice()

	src := newCM108SourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())

	ch := src.Events(ctx)

	cancel()
	mock.Close() // unblock Read

	// Drain the channel to let the goroutine finish.
	for range ch { //nolint:revive
	}

	if mock.closeCalls == 0 {
		t.Error("expected dev.Close() to be called on shutdown; got 0 calls")
	}
}

// ─── CM108 report parsing unit tests ─────────────────────────────────────────

func TestMakeCM108Report_GPIO3High(t *testing.T) {
	r := makeCM108Report(true)
	ir1 := r[cm108PayloadOffset+1]

	if ir1&cm108GPIO3Mask == 0 {
		t.Errorf("expected GPIO3 bit set in IR1; got IR1=0x%02X", ir1)
	}
}

func TestMakeCM108Report_GPIO3Low(t *testing.T) {
	r := makeCM108Report(false)
	ir1 := r[cm108PayloadOffset+1]

	if ir1&cm108GPIO3Mask != 0 {
		t.Errorf("expected GPIO3 bit clear in IR1; got IR1=0x%02X", ir1)
	}
}

func TestCM108Constants(t *testing.T) {
	if cm108VendorID != 0x0D8C {
		t.Errorf("VendorID: got 0x%04X, want 0x0D8C", cm108VendorID)
	}

	if cm108ProductID != 0x013C {
		t.Errorf("ProductID: got 0x%04X, want 0x013C", cm108ProductID)
	}
}
