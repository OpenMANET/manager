package comms

import (
	"context"
	"errors"
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

func TestBlueALSAPropertyMessages(t *testing.T) {
	sig := &dbus.Signal{
		Name: bluezPropertiesChangedSignal,
		Path: dbus.ObjectPath("/org/bluealsa/hci0/dev_test"),
		Body: []any{
			"org.bluealsa.Device1",
			map[string]dbus.Variant{
				"ATCommand": dbus.MakeVariant("+XEVENT:PTT_DOWN"),
				"State":     dbus.MakeVariant("ignored"),
			},
		},
	}

	msgs := blueALSAPropertyMessages(sig)
	if len(msgs) != 1 {
		t.Fatalf("blueALSAPropertyMessages() len = %d, want 1", len(msgs))
	}

	if msgs[0] != "+XEVENT:PTT_DOWN" {
		t.Fatalf("blueALSAPropertyMessages()[0] = %q, want %q", msgs[0], "+XEVENT:PTT_DOWN")
	}
}

func TestBlueALSAXEventSource_ParsesATMessageSignals(t *testing.T) {
	src := &blueALSAXEventSource{log: zerolog.Nop()}
	ch := make(chan PTTEvent, 1)

	ok := src.handleSignal(context.Background(), ch, &dbus.Signal{
		Name: bluealsaATMessageSignal,
		Body: []any{"+XEVENT:PTT_DOWN"},
	})
	if !ok {
		t.Fatal("handleSignal returned false")
	}

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Fatalf("event = %v, want %v", ev, PTTDown)
		}
	default:
		t.Fatal("expected BlueALSA event to be emitted")
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
