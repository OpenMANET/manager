package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"

	"github.com/openmanet/openmanetd/internal/comms/device"
)

// ─── mockHIDDevice ────────────────────────────────────────────────────────────

// mockHIDDevice satisfies HIDDevice. Each call to Read returns the next queued
// report; when the queue is empty it blocks until Close is called.
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
		ir1 |= OpenVLMGPIO3Mask
	}

	return []byte{0x00, 0x00, ir1, 0x00, 0x00}
}

func collectPTTEvents(ch <-chan PTTEvent, timeout time.Duration) []PTTEvent {
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

func openerReturning(dev HIDDevice) HIDOpener {
	return func(_, _ uint16, _ string) (HIDDevice, error) {
		return dev, nil
	}
}

func openerFailing(err error) HIDOpener {
	return func(_, _ uint16, _ string) (HIDDevice, error) {
		return nil, err
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestOpenVLMSource_OpenerError_ClosesChannelImmediately(t *testing.T) {
	src := NewOpenVLMSourceWithOpener(openerFailing(errors.New("no device")), zerolog.Nop())

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
	opener := func(vid, pid uint16, _ string) (HIDDevice, error) {
		resultCh <- vidpid{vid, pid}

		return mock, nil
	}

	src := NewOpenVLMSourceWithOpener(opener, zerolog.Nop())

	src.Events(t.Context())

	select {
	case r := <-resultCh:
		if r.vid != OpenVLMVendorID {
			t.Errorf("VendorID: got 0x%04X, want 0x%04X", r.vid, OpenVLMVendorID)
		}

		if r.pid != OpenVLMProductID {
			t.Errorf("ProductID: got 0x%04X, want 0x%04X", r.pid, OpenVLMProductID)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("opener was not called within timeout")
	}
}

func TestOpenVLMSource_GPIO3_LowReport_NoEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeOpenVLMReport(false))

	src := NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

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

	src := NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != PTTDown {
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

	src := NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

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

func TestOpenVLMSource_DuplicateState_NoExtraEvent(t *testing.T) {
	mock := newMockHIDDevice()
	mock.queueReport(makeOpenVLMReport(true))  // HIGH → PTTDown
	mock.queueReport(makeOpenVLMReport(true))  // HIGH again → no event
	mock.queueReport(makeOpenVLMReport(false)) // LOW → PTTUp

	src := NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

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

	src := NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

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

	src := NewOpenVLMSourceWithOpener(openerReturning(errDev), zerolog.Nop())

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

	DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

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

	DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

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

	DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

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

	DetectAndSetALSACardFromRoot(tmp, zerolog.Nop())

	// Should not overwrite the existing value.
	if v := os.Getenv("ALSA_CARD"); v != "7" {
		t.Errorf("expected ALSA_CARD=7 (unchanged); got %q", v)
	}
}

// ─── DetectAndSetALSACardFromSys tests ──────────────────────────────────────

// mkOpenVLMSysFS builds a minimal sysfs tree containing one CM108-family USB
// device located under bus/usb/devices/<name>. The sound child names a
// "card<idx>" so DiscoverCM108 returns ALSACardIdx=idx for the descriptor.
func mkOpenVLMSysFS(name, vendor, product, iface string, alsaCardIdx int) fstest.MapFS {
	fsys := fstest.MapFS{}
	base := "bus/usb/devices/" + name

	fsys[base+"/idVendor"] = &fstest.MapFile{Data: []byte(vendor + "\n")}
	fsys[base+"/idProduct"] = &fstest.MapFile{Data: []byte(product + "\n")}
	fsys[base+"/"+iface+"/sound/card"+strconv.Itoa(alsaCardIdx)+"/id"] =
		&fstest.MapFile{Data: []byte("OpenVLM\n")}

	return fsys
}

func TestDetectAndSetALSACardFromSys_AlreadySet(t *testing.T) {
	t.Setenv("ALSA_CARD", "9")

	fsys := mkOpenVLMSysFS("1-1", "0d8c", "0012", "1-1:1.3", 4)

	if !DetectAndSetALSACardFromSys(fsys, zerolog.Nop()) {
		t.Error("expected true (already-set short-circuit)")
	}

	if v := os.Getenv("ALSA_CARD"); v != "9" {
		t.Errorf("ALSA_CARD = %q, want %q (unchanged)", v, "9")
	}
}

func TestDetectAndSetALSACardFromSys_MatchingCard(t *testing.T) {
	os.Unsetenv("ALSA_CARD")                       //nolint:errcheck
	t.Cleanup(func() { os.Unsetenv("ALSA_CARD") }) //nolint:errcheck

	fsys := mkOpenVLMSysFS("1-1", "0d8c", "0012", "1-1:1.3", 4)

	if !DetectAndSetALSACardFromSys(fsys, zerolog.Nop()) {
		t.Error("expected true after matching card found")
	}

	if v := os.Getenv("ALSA_CARD"); v != "4" {
		t.Errorf("ALSA_CARD = %q, want %q", v, "4")
	}
}

func TestDetectAndSetALSACardFromSys_NoCM108Devices(t *testing.T) {
	os.Unsetenv("ALSA_CARD")                       //nolint:errcheck
	t.Cleanup(func() { os.Unsetenv("ALSA_CARD") }) //nolint:errcheck

	// Empty sysfs — DiscoverCM108 returns no descriptors and the function
	// must report false (caller falls back to FromRoot).
	if DetectAndSetALSACardFromSys(fstest.MapFS{}, zerolog.Nop()) {
		t.Error("expected false on empty sysfs")
	}

	if v := os.Getenv("ALSA_CARD"); v != "" {
		t.Errorf("ALSA_CARD = %q, want unset", v)
	}
}

func TestDetectAndSetALSACardFromSys_NonCM108DeviceSkipped(t *testing.T) {
	os.Unsetenv("ALSA_CARD")                       //nolint:errcheck
	t.Cleanup(func() { os.Unsetenv("ALSA_CARD") }) //nolint:errcheck

	// Vendor matches CM108 family but product ID does not — should be skipped.
	fsys := mkOpenVLMSysFS("1-1", "1d6b", "0002", "1-1:1.0", 1)

	if DetectAndSetALSACardFromSys(fsys, zerolog.Nop()) {
		t.Error("expected false for non-CM108 device")
	}

	if v := os.Getenv("ALSA_CARD"); v != "" {
		t.Errorf("ALSA_CARD = %q, want unset", v)
	}
}

func TestDetectAndSetALSACardFromSys_DeviceWithoutALSACard(t *testing.T) {
	os.Unsetenv("ALSA_CARD")                       //nolint:errcheck
	t.Cleanup(func() { os.Unsetenv("ALSA_CARD") }) //nolint:errcheck

	// CM108 device with no sound child → ALSACardIdx == -1, must be skipped
	// (the function only sets ALSA_CARD for descriptors with ALSACardIdx>=0).
	fsys := fstest.MapFS{
		"bus/usb/devices/1-1/idVendor":  &fstest.MapFile{Data: []byte("0d8c\n")},
		"bus/usb/devices/1-1/idProduct": &fstest.MapFile{Data: []byte("0012\n")},
	}

	if DetectAndSetALSACardFromSys(fsys, zerolog.Nop()) {
		t.Error("expected false when no ALSA card child present")
	}

	if v := os.Getenv("ALSA_CARD"); v != "" {
		t.Errorf("ALSA_CARD = %q, want unset", v)
	}
}

func TestDetectAndSetALSACard_FallsBackToRoot(t *testing.T) {
	// Top-level wrapper: Sys path returns false (empty MapFS would be
	// returned by DirFS("/sys") in production, but we cannot fake DirFS
	// here). Instead, verify the wrapper does not panic and produces a
	// stable observable state when no card is found anywhere.
	os.Unsetenv("ALSA_CARD")                       //nolint:errcheck
	t.Cleanup(func() { os.Unsetenv("ALSA_CARD") }) //nolint:errcheck

	// Just call it — on a CI host without OpenVLM hardware this exercises
	// both the FromSys (likely false) and FromRoot fallback paths without
	// crashing.
	DetectAndSetALSACard(zerolog.Nop())
}

// ─── OpenVLM short-report test ─────────────────────────────────────────────────

func TestOpenVLMSource_ShortReport_SkippedAndContinues(t *testing.T) {
	// A 1-byte report is below the 2-byte minimum and must be skipped.
	// The source should sleep 50 ms then continue processing the queued
	// valid report and emit PTTDown.
	mock := newMockHIDDevice()
	mock.queueReport([]byte{0x00})            // 1-byte short report — skipped
	mock.queueReport(makeOpenVLMReport(true)) // valid HIGH report → PTTDown

	src := NewOpenVLMSourceWithOpener(openerReturning(mock), zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown after short report skipped; got %v", ev)
		}
	case <-time.After(700 * time.Millisecond):
		t.Error("timed out — short report may not have been skipped correctly")
	}
}

// ─── preferredOpenVLMSerial ────────────────────────────────────────────────────

func TestPreferredOpenVLMSerial_PicksStrappedDevice(t *testing.T) {
	descs := []device.CM108Descriptor{
		{Serial: "GENERIC-CM108", IsOpenVLM: false},
		{Serial: "OPENVLM-001", IsOpenVLM: true},
		{Serial: "OPENVLM-002", IsOpenVLM: true},
	}

	if got := preferredOpenVLMSerial(descs); got != "OPENVLM-001" {
		t.Errorf("preferredOpenVLMSerial = %q, want OPENVLM-001 (first strapped)", got)
	}
}

func TestPreferredOpenVLMSerial_NoStrappedReturnsEmpty(t *testing.T) {
	descs := []device.CM108Descriptor{
		{Serial: "A", IsOpenVLM: false},
		{Serial: "B", IsOpenVLM: false},
	}

	if got := preferredOpenVLMSerial(descs); got != "" {
		t.Errorf("preferredOpenVLMSerial = %q, want empty string", got)
	}
}

func TestPreferredOpenVLMSerial_SkipsStrappedWithoutSerial(t *testing.T) {
	// A strapped device with no serial can't be pinned — skip it and look
	// for a later one. hid.Open(vid, pid, "") would fall back to any match,
	// which defeats the purpose of the preference.
	descs := []device.CM108Descriptor{
		{Serial: "", IsOpenVLM: true},
		{Serial: "OPENVLM-GOOD", IsOpenVLM: true},
	}

	if got := preferredOpenVLMSerial(descs); got != "OPENVLM-GOOD" {
		t.Errorf("preferredOpenVLMSerial = %q, want OPENVLM-GOOD", got)
	}
}

func TestPreferredOpenVLMSerial_EmptyInput(t *testing.T) {
	if got := preferredOpenVLMSerial(nil); got != "" {
		t.Errorf("preferredOpenVLMSerial(nil) = %q, want empty string", got)
	}

	if got := preferredOpenVLMSerial([]device.CM108Descriptor{}); got != "" {
		t.Errorf("preferredOpenVLMSerial([]) = %q, want empty string", got)
	}
}
