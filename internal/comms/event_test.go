package comms

import (
	"context"
	"sync"
	"testing"
	"time"

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
		{"openvlm", "openvlm"},
		{"OPENVLM", "openvlm"},
		{"  openvlm  ", "openvlm"},
		{"nanoptt", "nanoptt"},
		{"NANOPTT", "nanoptt"},
		{"bluealsa_xevent", "bluealsa_xevent"},
		{"BLUEALSA_XEVENT", "bluealsa_xevent"},
		{"roip", "roip"},
		{"ROIP", "roip"},
		{"  roip  ", "roip"},
		{"web", "web"},
		{"WEB", "web"},
		{"  web  ", "web"},
		{"", "openvlm"},
		{"unknown", "openvlm"},
	}
	for _, tc := range cases {
		got := normalizeControlSource(tc.input)
		if got != tc.want {
			t.Errorf("normalizeControlSource(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
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
