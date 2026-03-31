package batmanadv

import (
	"context"
	"testing"
	"time"

	"github.com/mdlayher/genetlink"
	"github.com/rs/zerolog"
)

func TestParseUevent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want map[string]string
	}{
		{
			name: "batman-adv gateway event",
			data: []byte("ACTION=change\x00SUBSYSTEM=batman-adv\x00BATTYPE=gw\x00BATDEV=bat0\x00"),
			want: map[string]string{
				"ACTION":    "change",
				"SUBSYSTEM": "batman-adv",
				"BATTYPE":   "gw",
				"BATDEV":    "bat0",
			},
		},
		{
			name: "empty data",
			data: []byte{},
			want: map[string]string{},
		},
		{
			name: "single entry",
			data: []byte("KEY=VALUE\x00"),
			want: map[string]string{"KEY": "VALUE"},
		},
		{
			name: "entry without equals",
			data: []byte("NOEQUALS\x00KEY=VALUE\x00"),
			want: map[string]string{"KEY": "VALUE"},
		},
		{
			name: "empty value",
			data: []byte("KEY=\x00"),
			want: map[string]string{"KEY": ""},
		},
		{
			name: "value with equals",
			data: []byte("KEY=VAL=UE\x00"),
			want: map[string]string{"KEY": "VAL=UE"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseUevent(tt.data)

			if len(got) != len(tt.want) {
				t.Errorf("parseUevent() returned %d entries, want %d", len(got), len(tt.want))
			}

			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseUevent()[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestListener_SetCallbacks(t *testing.T) {
	l := &Listener{
		logger: zerolog.Nop(),
	}

	called := false

	l.SetOnMeshConfigChange(func() { called = true })

	l.mu.RLock()
	cb := l.onMeshConfigChange
	l.mu.RUnlock()

	if cb == nil {
		t.Fatal("onMeshConfigChange should not be nil after SetOnMeshConfigChange")
	}

	cb()

	if !called {
		t.Error("callback was not invoked")
	}

	gwCalled := false

	l.SetOnGatewayEvent(func() { gwCalled = true })

	l.mu.RLock()
	gwCb := l.onGatewayEvent
	l.mu.RUnlock()

	if gwCb == nil {
		t.Fatal("onGatewayEvent should not be nil after SetOnGatewayEvent")
	}

	gwCb()

	if !gwCalled {
		t.Error("gateway callback was not invoked")
	}
}

func TestListener_StopWithoutStart(t *testing.T) {
	// Stop without Start should not panic
	l := &Listener{
		logger: zerolog.Nop(),
	}

	// Should not panic
	l.Stop()
}

func TestListener_ContextCancellation(t *testing.T) {
	// Create a listener with a nil eventConn to test context-based shutdown
	// of the uevent goroutine (multicast goroutine needs a real conn)
	l := &Listener{
		logger: zerolog.Nop(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	l.ctx, l.cancel = ctx, cancel

	// Just test the uevent goroutine — it should exit on context cancel
	// when the uevent socket isn't available
	l.wg.Add(1)

	go l.listenUevents()

	// Give it a moment to start and discover uevent socket is unavailable
	time.Sleep(10 * time.Millisecond)

	// The goroutine should have already exited (uevent socket fails in container)
	// but cancel just in case
	cancel()

	done := make(chan struct{})

	go func() {
		l.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Good, goroutine exited
	case <-time.After(2 * time.Second):
		t.Fatal("uevent goroutine did not exit after context cancellation")
	}
}

func TestNewListener_NilFamily(t *testing.T) {
	// A family with no groups should fail to find the config multicast group
	family := genetlink.Family{
		ID:     30,
		Name:   BatadvNLName,
		Groups: nil,
	}

	_, err := NewListener(family, zerolog.Nop())
	if err == nil {
		t.Error("expected error for family with no multicast groups")
	}
}
