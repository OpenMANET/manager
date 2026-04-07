package comms

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	evdev "github.com/gvalkov/golang-evdev"
	"github.com/rs/zerolog"
)

func TestEvdevSource_PTTToggle(t *testing.T) {
	src := &mockEventSource{ch: make(chan PTTEvent, 4)}
	src.ch <- PTTToggle

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	select {
	case ev := <-ch:
		if ev != PTTToggle {
			t.Errorf("expected PTTToggle; got %v", ev)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out")
	}
}

func TestNewNanoPTTSource_NonNil(t *testing.T) {
	var dev *evdev.InputDevice

	src := NewNanoPTTSource(dev, "any", zerolog.Nop())
	if src == nil {
		t.Error("NewNanoPTTSource should not return nil")
	}
}

func TestNormalizeControlSource(t *testing.T) {
	cases := []struct{ input, want string }{
		{"cm108", "cm108"},
		{"CM108", "cm108"},
		{"  cm108  ", "cm108"},
		{"nanoptt", "nanoptt"},
		{"NANOPTT", "nanoptt"},
		{"bluealsa_xevent", "bluealsa_xevent"},
		{"BLUEALSA_XEVENT", "bluealsa_xevent"},
		{"bluetooth", "bluetooth"},
		{"BLUETOOTH", "bluetooth"},
		{"roip", "roip"},
		{"ROIP", "roip"},
		{"  roip  ", "roip"},
		{"web", "web"},
		{"WEB", "web"},
		{"  web  ", "web"},
		{"", "cm108"},
		{"unknown", "cm108"},
	}
	for _, tc := range cases {
		got := normalizeControlSource(tc.input)
		if got != tc.want {
			t.Errorf("normalizeControlSource(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestBlueALSAEventName(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
		ok   bool
	}{
		{name: "colon delimiter", msg: "+XEVENT:PTT_DOWN", want: "PTT_DOWN", ok: true},
		{name: "equals delimiter", msg: "AT+XEVENT=PTT_UP", want: "PTT_UP", ok: true},
		{name: "comma delimiter", msg: "+XEVENT,PREV_CH", want: "PREV_CH", ok: true},
		{name: "framed rfcomm packet", msg: "\x11\xef&\x00AT+XEVENT=PTT_DOWN\r\xbf", want: "PTT_DOWN", ok: true},
		{name: "missing marker", msg: "garbage", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := blueALSAEventName(tt.msg)
			if ok != tt.ok {
				t.Fatalf("blueALSAEventName(%q) ok = %v, want %v", tt.msg, ok, tt.ok)
			}

			if got != tt.want {
				t.Fatalf("blueALSAEventName(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestBlueALSAJournalEventName(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{
			name: "journal marker",
			line: "daemon.info bluealsa: AT message: SET: command:+XEVENT, value:PTT_DOWN",
			want: "PTT_DOWN",
			ok:   true,
		},
		{
			name: "missing marker",
			line: "daemon.info bluealsa: connected device",
			want: "",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := blueALSAJournalEventName(tt.line)
			if ok != tt.ok {
				t.Fatalf("blueALSAJournalEventName(%q) ok = %v, want %v", tt.line, ok, tt.ok)
			}

			if got != tt.want {
				t.Fatalf("blueALSAJournalEventName(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestBlueALSAEventNames(t *testing.T) {
	got := blueALSAEventNames("\r\n+XEVENT:PTT_DOWN\r\nAT+XEVENT=PTT_UP\r\nignored\r\n")
	want := []string{"PTT_DOWN", "PTT_UP"}
	if len(got) != len(want) {
		t.Fatalf("blueALSAEventNames() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blueALSAEventNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBlueALSAEventNames_FramedRFCOMMPackets(t *testing.T) {
	packet := "\x11\xef&\x00AT+XEVENT=PTT_DOWN\r\xbf\r\n\x11\xef\"\x00AT+XEVENT=PTT_UP\r\xbf\r\n"

	got := blueALSAEventNames(packet)
	want := []string{"PTT_DOWN", "PTT_UP"}

	if len(got) != len(want) {
		t.Fatalf("blueALSAEventNames() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blueALSAEventNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBlueALSARFCOMMPaths(t *testing.T) {
	managed := map[dbus.ObjectPath]map[string]map[string]dbus.Variant{
		dbus.ObjectPath("/org/bluealsa/hci0/dev_BB/rfcomm"): {
			bluealsaRFCOMMInterface: {},
		},
		dbus.ObjectPath("/org/bluealsa/hci0/dev_AA/a2dp"): {
			"org.bluealsa.PCM1": {},
		},
		dbus.ObjectPath("/org/bluealsa/hci0/dev_AA/rfcomm"): {
			bluealsaRFCOMMInterface: {},
		},
	}

	got := blueALSARFCOMMPaths(managed)
	want := []dbus.ObjectPath{
		dbus.ObjectPath("/org/bluealsa/hci0/dev_AA/rfcomm"),
		dbus.ObjectPath("/org/bluealsa/hci0/dev_BB/rfcomm"),
	}

	if len(got) != len(want) {
		t.Fatalf("blueALSARFCOMMPaths() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("blueALSARFCOMMPaths()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBlueALSAXEventSource_SyncRFCOMMPathsStartsMonitors(t *testing.T) {
	src := &blueALSAXEventSource{
		log: zerolog.Nop(),
		listRFCOMM: func(*dbus.Conn) ([]dbus.ObjectPath, error) {
			return []dbus.ObjectPath{
				dbus.ObjectPath("/org/bluealsa/hci0/dev_AA/rfcomm"),
				dbus.ObjectPath("/org/bluealsa/hci0/dev_BB/rfcomm"),
			}, nil
		},
	}

	var got []dbus.ObjectPath
	src.syncRFCOMMPaths(nil, func(path dbus.ObjectPath) {
		got = append(got, path)
	})

	want := []dbus.ObjectPath{
		dbus.ObjectPath("/org/bluealsa/hci0/dev_AA/rfcomm"),
		dbus.ObjectPath("/org/bluealsa/hci0/dev_BB/rfcomm"),
	}

	if len(got) != len(want) {
		t.Fatalf("syncRFCOMMPaths() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("syncRFCOMMPaths()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBlueALSAXEventSource_SyncRFCOMMPathsListError(t *testing.T) {
	src := &blueALSAXEventSource{
		log: zerolog.Nop(),
		listRFCOMM: func(*dbus.Conn) ([]dbus.ObjectPath, error) {
			return nil, errors.New("boom")
		},
	}

	called := false
	src.syncRFCOMMPaths(nil, func(dbus.ObjectPath) {
		called = true
	})

	if called {
		t.Fatal("startMonitor should not be called when RFCOMM listing fails")
	}
}

func TestBlueALSAInterfacesAddedRFCOMMPath(t *testing.T) {
	sig := &dbus.Signal{
		Name: bluealsaInterfacesAddedSignal,
		Body: []any{
			dbus.ObjectPath("/org/bluealsa/hci0/dev_test/rfcomm"),
			map[string]map[string]dbus.Variant{
				bluealsaRFCOMMInterface: {},
				"org.bluealsa.PCM1":     {},
			},
		},
	}

	path, ok := blueALSAInterfacesAddedRFCOMMPath(sig)
	if !ok {
		t.Fatal("expected RFCOMM path to be detected")
	}
	if path != dbus.ObjectPath("/org/bluealsa/hci0/dev_test/rfcomm") {
		t.Fatalf("path = %q, want %q", path, "/org/bluealsa/hci0/dev_test/rfcomm")
	}
}

func TestBlueALSAInterfacesRemovedRFCOMMPath(t *testing.T) {
	sig := &dbus.Signal{
		Name: bluealsaInterfacesRemovedSignal,
		Body: []any{
			dbus.ObjectPath("/org/bluealsa/hci0/dev_test/rfcomm"),
			[]string{"org.bluealsa.PCM1", bluealsaRFCOMMInterface},
		},
	}

	path, ok := blueALSAInterfacesRemovedRFCOMMPath(sig)
	if !ok {
		t.Fatal("expected RFCOMM path to be detected")
	}
	if path != dbus.ObjectPath("/org/bluealsa/hci0/dev_test/rfcomm") {
		t.Fatalf("path = %q, want %q", path, "/org/bluealsa/hci0/dev_test/rfcomm")
	}
}

func TestBlueALSAXEventSource_ConsumesRFCOMM(t *testing.T) {
	src := &blueALSAXEventSource{
		log: zerolog.Nop(),
		openRFCOMM: func(*dbus.Conn, dbus.ObjectPath) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("\x11\xef&\x00AT+XEVENT=PTT_DOWN\r\xbf\r\n\x11\xef\"\x00AT+XEVENT=PTT_UP\r\xbf\r\n")), nil
		},
	}

	ch := make(chan PTTEvent, 2)
	src.monitorRFCOMM(context.Background(), nil, dbus.ObjectPath("/org/bluealsa/hci0/dev_test/rfcomm"), ch)

	ev1 := <-ch
	ev2 := <-ch

	if ev1 != PTTDown {
		t.Fatalf("event 1 = %v, want %v", ev1, PTTDown)
	}
	if ev2 != PTTUp {
		t.Fatalf("event 2 = %v, want %v", ev2, PTTUp)
	}
}

func TestBluetoothEventSource_ForwardsBlueALSAFallback(t *testing.T) {
	fallback := &mockEventSource{ch: make(chan PTTEvent, 1)}
	src := &bluetoothEventSource{
		log:  zerolog.Nop(),
		dial: func() (*dbus.Conn, error) { return nil, errors.New("no bluez") },
		xeventFactory: func(zerolog.Logger) EventSource {
			return fallback
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := src.Events(ctx)
	fallback.ch <- PTTUp

	select {
	case ev := <-ch:
		if ev != PTTUp {
			t.Fatalf("event = %v, want %v", ev, PTTUp)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for forwarded fallback event")
	}

	close(fallback.ch)
}

func TestPTTEvent_Values(t *testing.T) {
	if PTTDown != 0 {
		t.Errorf("PTTDown = %d; want 0", PTTDown)
	}

	if PTTUp != 1 {
		t.Errorf("PTTUp = %d; want 1", PTTUp)
	}

	if PTTToggle != 2 {
		t.Errorf("PTTToggle = %d; want 2", PTTToggle)
	}
}

func TestMockEventSource_ClosedChannel(t *testing.T) {
	src := &mockEventSource{ch: make(chan PTTEvent)}
	close(src.ch)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch := src.Events(ctx)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out")
	}
}

func TestMockEventSource_ConcurrentDelivery(t *testing.T) {
	const n = 50

	src := &mockEventSource{ch: make(chan PTTEvent, n)}
	for i := 0; i < n; i++ {
		src.ch <- PTTToggle
	}

	close(src.ch)
	ch := src.Events(context.Background())

	var (
		wg    sync.WaitGroup
		count int
		mu    sync.Mutex
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		for range ch {
			mu.Lock()
			count++
			mu.Unlock()
		}
	}()

	wg.Wait()

	if count != n {
		t.Errorf("expected %d; got %d", n, count)
	}
}
