package comms

import (
	"errors"
	"testing"
)

// newTestService constructs a *Service directly (bypassing Start) with a
// minimal runtime suitable for exercising Service methods in isolation. It
// publishes the service via SetDefault and registers cleanup to clear it.
func newTestService(t *testing.T, ports []McastPortConfig) *Service {
	t.Helper()

	svc := &Service{McastPorts: ports}
	rt := &CommsRuntime{Ports: make([]*PortChannel, len(ports))}

	for i, pc := range ports {
		rt.Ports[i] = &PortChannel{cfg: pc}
		rt.Ports[i].SendEnabled.Store(true)
		rt.Ports[i].ReceiveEnabled.Store(true)
	}

	svc.runtime = rt
	SetDefault(svc)
	t.Cleanup(func() { SetDefault(nil) })

	return svc
}

func TestService_ActiveMulticastAccessors(t *testing.T) {
	svc := newTestService(t, []McastPortConfig{{Address: "239.1.2.3", Port: 5004}})

	if got := svc.ActiveMulticastAddr(); got != "239.1.2.3" {
		t.Errorf("ActiveMulticastAddr = %q, want 239.1.2.3", got)
	}

	if got := svc.ActiveMulticastPort(); got != 5004 {
		t.Errorf("ActiveMulticastPort = %d, want 5004", got)
	}

	// Shim free functions must reach the same data.
	if GetActiveMulticastAddr() != "239.1.2.3" {
		t.Error("GetActiveMulticastAddr shim mismatch")
	}

	if GetActiveMulticastPort() != 5004 {
		t.Error("GetActiveMulticastPort shim mismatch")
	}
}

func TestService_NilDefault(t *testing.T) {
	SetDefault(nil)

	if got := GetActiveMulticastAddr(); got != "" {
		t.Errorf("nil default addr = %q, want empty", got)
	}

	if got := GetActiveMulticastPort(); got != 0 {
		t.Errorf("nil default port = %d, want 0", got)
	}

	// Service methods on a nil default must surface ErrNotRunning so callers
	// (handlers, manager, tests) can use errors.Is to distinguish "not yet
	// started" from other failures.
	if _, err := GetTalkGroupStates(); !errors.Is(err, ErrNotRunning) {
		t.Errorf("GetTalkGroupStates on nil default: want ErrNotRunning, got %v", err)
	}

	if err := EnableTalkGroupSend(0, true); !errors.Is(err, ErrNotRunning) {
		t.Errorf("EnableTalkGroupSend on nil default: want ErrNotRunning, got %v", err)
	}

	if err := EnableTalkGroupReceive(0, true); !errors.Is(err, ErrNotRunning) {
		t.Errorf("EnableTalkGroupReceive on nil default: want ErrNotRunning, got %v", err)
	}
}

func TestService_EnableTalkGroupAndStates(t *testing.T) {
	svc := newTestService(t, []McastPortConfig{
		{Address: "239.1.1.1", Port: 5004},
		{Address: "239.1.1.2", Port: 5006},
	})

	if err := svc.EnableTalkGroupSend(1, false); err != nil {
		t.Fatalf("EnableTalkGroupSend: %v", err)
	}

	if err := svc.EnableTalkGroupReceive(0, false); err != nil {
		t.Fatalf("EnableTalkGroupReceive: %v", err)
	}

	states, err := svc.TalkGroupStates()
	if err != nil {
		t.Fatalf("TalkGroupStates: %v", err)
	}

	if len(states) != 2 {
		t.Fatalf("states len = %d, want 2", len(states))
	}

	if states[0].ReceiveEnabled || !states[0].SendEnabled {
		t.Errorf("port 0 state = %+v", states[0])
	}

	if !states[1].ReceiveEnabled || states[1].SendEnabled {
		t.Errorf("port 1 state = %+v", states[1])
	}

	if err := svc.EnableTalkGroupSend(42, true); err == nil {
		t.Error("out-of-range EnableTalkGroupSend: want error")
	}
}
