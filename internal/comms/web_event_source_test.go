package comms

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestWebEventSource_ImplementsEventSource(t *testing.T) {
	var _ EventSource = NewWebEventSource(zerolog.Nop())
}

func TestWebEventSource_Push_DeliversEvent(t *testing.T) {
	ws := NewWebEventSource(zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := ws.Events(ctx)
	ws.Push(PTTDown)

	select {
	case ev := <-ch:
		if ev != PTTDown {
			t.Errorf("expected PTTDown; got %v", ev)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timed out waiting for PTTDown event")
	}
}

func TestWebEventSource_Push_AllEventTypes(t *testing.T) {
	ws := NewWebEventSource(zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch := ws.Events(ctx)

	events := []PTTEvent{PTTDown, PTTUp, PTTToggle}
	for _, ev := range events {
		ws.Push(ev)
	}

	for _, want := range events {
		select {
		case got := <-ch:
			if got != want {
				t.Errorf("expected %v; got %v", want, got)
			}
		case <-time.After(200 * time.Millisecond):
			t.Errorf("timed out waiting for event %v", want)
		}
	}
}

func TestWebEventSource_Push_DropOnFull(t *testing.T) {
	ws := NewWebEventSource(zerolog.Nop())

	// Fill the channel to capacity (4).
	for i := 0; i < 4; i++ {
		ws.Push(PTTToggle)
	}

	// This push must not block; the event is silently dropped.
	done := make(chan struct{})

	go func() {
		ws.Push(PTTToggle)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("Push blocked on full channel")
	}
}

func TestWebEventSource_Events_ClosedOnContextCancel(t *testing.T) {
	ws := NewWebEventSource(zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	ch := ws.Events(ctx)

	cancel()

	// The channel should eventually close.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("channel was not closed after context cancel")
	}
}

func TestWebEventSource_ConcurrentPush(t *testing.T) {
	const n = 50

	ws := NewWebEventSource(zerolog.Nop())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch := ws.Events(ctx)

	var wg sync.WaitGroup

	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()

			ws.Push(PTTToggle)
		}()
	}

	wg.Wait()

	// Drain whatever was delivered (some may have been dropped due to
	// channel capacity). No race or panic is the real assertion here.
	drained := 0

	for {
		select {
		case <-ch:
			drained++
		default:
			if drained == 0 {
				t.Error("expected at least one event delivered")
			}

			return
		}
	}
}

func TestWebEventSource_RunIntegration(t *testing.T) {
	ws := NewWebEventSource(zerolog.Nop())
	stream := &mockStream{}
	rt := newRunRuntime(stream)
	cfg := &CommsConfig{Log: zerolog.Nop()}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)

		cfg.Run(ctx, rt, ws)
	}()

	// Give the Run loop time to start before pushing events.
	time.Sleep(50 * time.Millisecond)

	ws.Push(PTTDown)

	// Wait for the broadcast stream to be started (beginTransmission sleeps 200ms).
	time.Sleep(300 * time.Millisecond)

	ws.Push(PTTUp)

	// Wait for the Run loop to process PTTUp.
	time.Sleep(100 * time.Millisecond)

	// Cancel context to terminate Run.
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("Run did not exit in time")
	}

	rt.ports[0].receiver.Close()

	if stream.startCalls != 1 {
		t.Errorf("Start called %d times, want 1", stream.startCalls)
	}

	if stream.stopCalls != 1 {
		t.Errorf("Stop called %d times, want 1", stream.stopCalls)
	}
}
