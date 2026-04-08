package comms

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/control"
)

// ─── mockHIDDevice ────────────────────────────────────────────────────────────

// mockHIDDevice satisfies control.HIDDevice. Each call to Read returns the
// next queued report; when the queue is empty it blocks until Close is called.
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

func (m *mockHIDDevice) queueReport(report []byte) {
	m.reports <- report
}

func (m *mockHIDDevice) Close() error {
	m.closeCalls++

	select {
	case <-m.closed:
	default:
		close(m.closed)
	}

	return m.closeErr
}

func (m *mockHIDDevice) Read(b []byte) (int, error) {
	select {
	case report := <-m.reports:
		n := copy(b, report)

		return n, nil
	case <-m.closed:
		return 0, errors.New("device closed")
	}
}

// ─── errHIDDevice ─────────────────────────────────────────────────────────────

type errHIDDevice struct{}

func (e *errHIDDevice) Read(_ []byte) (int, error) { return 0, errors.New("read error") }
func (e *errHIDDevice) Close() error               { return nil }

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeOpenVLMReport builds a 5-byte OpenVLM report [ReportID, IR0, IR1, IR2, IR3].
// gpio3High sets or clears bit 2 of IR1.
func makeOpenVLMReport(gpio3High bool) []byte {
	ir1 := byte(0x00)
	if gpio3High {
		ir1 |= control.OpenVLMGPIO3Mask
	}

	return []byte{0x00, 0x00, ir1, 0x00, 0x00}
}

func collectPTTEvents(ch <-chan control.PTTEvent, timeout time.Duration) []control.PTTEvent {
	var events []control.PTTEvent

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

func openerReturning(dev control.HIDDevice) control.HIDOpener {
	return func(_, _ uint16) (control.HIDDevice, error) {
		return dev, nil
	}
}

func openerFailing(err error) control.HIDOpener {
	return func(_, _ uint16) (control.HIDDevice, error) {
		return nil, err
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestOpenVLMSource_OpenerError_ClosesChannelImmediately(t *testing.T) {
	src := control.NewOpenVLMSourceWithOpener(openerFailing(errors.New("no device")), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed; received unexpected event")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("channel was not closed after opener error")
	}
}

func TestOpenVLMSource_OpenerCalledWithCorrectVIDPID(t *testing.T) {
	type vidpid struct{ vid, pid uint16 }

	resultCh := make(chan vidpid, 1)

	mock := newMockHIDDevice()
	opener := func(vid, pid uint16) (control.HIDDevice, error) {
		resultCh <- vidpid{vid, pid}

		return mock, nil
	}

	src := control.NewOpenVLMSourceWithOpener(opener, zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src.Events(ctx)

	select {
	case r := <-resultCh:
		if r.vid != control.OpenVLMVendorID {
			t.Errorf("VendorID: got 0x%04X, want 0x%04X", r.vid, control.OpenVLMVendorID)
		}

		if r.pid != control.OpenVLMProductID {
			t.Errorf("ProductID: got 0x%04X, want 0x%04X", r.pid, control.OpenVLMProductID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("opener was not called within timeout")
	}
}

func TestOpenVLMSource_GPIO3_LowReport_NoEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeOpenVLMReport(false))

	src := control.NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 200*time.Millisecond)

	if len(events) != 0 {
		t.Errorf("expected no events for unchanged LOW state; got %v", events)
	}
}

func TestOpenVLMSource_GPIO3_HighReport_EmitsPTTDown(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeOpenVLMReport(true))

	src := control.NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Errorf("expected PTTDown; got %v", ev)
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("timed out waiting for PTTDown event")
	}
}

func TestOpenVLMSource_HighThenLow_EmitsPTTDownThenPTTUp(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeOpenVLMReport(true))
	mock.queueReport(makeOpenVLMReport(false))

	src := control.NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 400*time.Millisecond)

	if len(events) < 2 {
		t.Fatalf("expected 2 events; got %d: %v", len(events), events)
	}

	if events[0] != control.PTTDown {
		t.Errorf("event[0]: got %v, want PTTDown", events[0])
	}

	if events[1] != control.PTTUp {
		t.Errorf("event[1]: got %v, want PTTUp", events[1])
	}
}

func TestOpenVLMSource_DuplicateState_NoExtraEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeOpenVLMReport(true))  // HIGH → PTTDown
	mock.queueReport(makeOpenVLMReport(true))  // HIGH again → no event
	mock.queueReport(makeOpenVLMReport(false)) // LOW → PTTUp

	src := control.NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	events := collectPTTEvents(ch, 400*time.Millisecond)

	if len(events) != 2 {
		t.Fatalf("expected 2 events (no duplicate); got %d: %v", len(events), events)
	}
}

func TestOpenVLMSource_ContextCancel_ClosesChannel(t *testing.T) {
	mock := newMockHIDDevice() // empty queue — will block

	src := control.NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	ch := src.Events(ctx)

	cancel()

	// Give the goroutine time to observe the cancellation.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after context cancel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("channel not closed after context cancel")
	}
}

func TestOpenVLMSource_ReadError_ClosesChannel(t *testing.T) {
	errDev := &errHIDDevice{}

	src := control.NewOpenVLMSourceWithOpener(openerReturning(errDev), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after read error")
		}
	case <-time.After(400 * time.Millisecond):
		t.Error("channel not closed after read error")
	}
}

// ─── ALSA card detection tests ───────────────────────────────────────────────

func TestDetectAndSetALSACardFromRoot_NoCards(t *testing.T) {
	t.Setenv("ALSA_CARD", "")
	os.Unsetenv("ALSA_CARD") //nolint:errcheck

	tmp := t.TempDir()

	control.DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

	if v := os.Getenv("ALSA_CARD"); v != "" {
		t.Errorf("expected ALSA_CARD to remain unset; got %q", v)
	}
}

func TestDetectAndSetALSACardFromRoot_MatchingCard(t *testing.T) {
	os.Unsetenv("ALSA_CARD")                       //nolint:errcheck
	t.Cleanup(func() { os.Unsetenv("ALSA_CARD") }) //nolint:errcheck

	tmp := t.TempDir()
	cardDir := filepath.Join(tmp, "card3")

	if err := os.MkdirAll(cardDir, 0o755); err != nil {
		t.Fatal(err)
	}

	usbidPath := filepath.Join(cardDir, "usbid")

	if err := os.WriteFile(usbidPath, []byte("0D8C:0012\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	control.DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

	if v := os.Getenv("ALSA_CARD"); v != "3" {
		t.Errorf("expected ALSA_CARD=3; got %q", v)
	}
}

func TestDetectAndSetALSACardFromRoot_NonMatchingCard(t *testing.T) {
	os.Unsetenv("ALSA_CARD") //nolint:errcheck

	tmp := t.TempDir()
	cardDir := filepath.Join(tmp, "card0")

	if err := os.MkdirAll(cardDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cardDir, "usbid"), []byte("1234:5678\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	control.DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

	if v := os.Getenv("ALSA_CARD"); v != "" {
		t.Errorf("expected ALSA_CARD to remain unset for non-OpenVLM card; got %q", v)
	}
}

func TestDetectAndSetALSACardFromRoot_AlreadySet(t *testing.T) {
	t.Setenv("ALSA_CARD", "7")

	defer os.Unsetenv("ALSA_CARD") //nolint:errcheck

	tmp := t.TempDir()
	cardDir := filepath.Join(tmp, "card0")

	if err := os.MkdirAll(cardDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cardDir, "usbid"), []byte("0d8c:013c\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	control.DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

	// Should not overwrite the existing value.
	if v := os.Getenv("ALSA_CARD"); v != "7" {
		t.Errorf("expected ALSA_CARD=7 (unchanged); got %q", v)
	}
}

// ─── OpenVLM short-report test ─────────────────────────────────────────────────

func TestOpenVLMSource_ShortReport_SkippedAndContinues(t *testing.T) {
	// A 1-byte report is below the 2-byte minimum and must be skipped.
	// The source should sleep 50 ms then continue processing the queued
	// valid report and emit PTTDown.
	mock := newMockHIDDevice()
	mock.queueReport([]byte{0x00})            // 1-byte short report — skipped
	mock.queueReport(makeOpenVLMReport(true)) // valid HIGH report → PTTDown

	src := control.NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != control.PTTDown {
			t.Errorf("expected PTTDown after short report skipped; got %v", ev)
		}
	case <-time.After(700 * time.Millisecond):
		t.Error("timed out — short report may not have been skipped correctly")
	}
}
