package blos

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/openmanet/openmanetd/internal/config"
)

// fakeJoinCall records one invocation of the fakeJoiner.
type fakeJoinCall struct {
	ifaceName string
	groupIP   string
}

// fakeJoiner is a test double for multicastJoiner. It binds real UDP sockets
// to 127.0.0.1:0 so the returned *net.UDPConn is valid without requiring
// batman-adv or an active network.
type fakeJoiner struct {
	failErr error
	failOn  string
	calls   []fakeJoinCall
	mu      sync.Mutex
}

func newFakeJoiner() *fakeJoiner {
	return &fakeJoiner{}
}

func (f *fakeJoiner) join(iface *net.Interface, group net.IP) (*net.UDPConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, fakeJoinCall{ifaceName: iface.Name, groupIP: group.String()})

	if f.failOn != "" && group.String() == f.failOn {
		return nil, f.failErr
	}

	// Bind to loopback only — no real multicast join needed since we mock the joiner.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, fmt.Errorf("fakeJoiner: %w", err)
	}

	return conn, nil
}

func (f *fakeJoiner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

func (f *fakeJoiner) calledWith(groupIP string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, c := range f.calls {
		if c.groupIP == groupIP {
			return true
		}
	}

	return false
}

// createTestBLOSWithFakeJoiner extends createTestBLOS with a fake multicast joiner.
func createTestBLOSWithFakeJoiner(fj *fakeJoiner) *BLOS {
	r := createTestBLOS()
	r.mcastJoiner = fj.join

	return r
}

// TestJoinMulticastGroupsOnInterface_InterfaceNotFound verifies that an error is
// returned when the named interface does not exist. No network access required.
func TestJoinMulticastGroupsOnInterface_InterfaceNotFound(t *testing.T) {
	fj := newFakeJoiner()
	r := createTestBLOSWithFakeJoiner(fj)

	err := r.joinMulticastGroupsOnInterface("nonexistent-iface-xyz-99")
	if err == nil {
		t.Fatal("expected error for non-existent interface, got nil")
	}

	if fj.callCount() != 0 {
		t.Errorf("expected joiner not to be called, got %d calls", fj.callCount())
	}
}

// TestJoinMulticastGroupsOnInterface_IncludesTalkGroupWhenCommsDisabled verifies
// that all multicast addresses are joined when comms is disabled (the default).
func TestJoinMulticastGroupsOnInterface_IncludesTalkGroupWhenCommsDisabled(t *testing.T) {
	fj := newFakeJoiner()
	r := createTestBLOSWithFakeJoiner(fj)
	r.Config.CommsEnable = false

	err := r.joinMulticastGroupsOnInterface("lo")
	if err != nil {
		t.Fatalf("joinMulticastGroupsOnInterface failed: %v", err)
	}

	allAddrs := config.GetMulticastGroupAddresses()

	if fj.callCount() != len(allAddrs) {
		t.Errorf("expected %d join calls, got %d", len(allAddrs), fj.callCount())
	}

	for _, addr := range allAddrs {
		if !fj.calledWith(addr) {
			t.Errorf("expected join call for %s, not found", addr)
		}
	}

	defer r.Stop()
}

// TestJoinMulticastGroupsOnInterface_SkipsTalkGroupWhenCommsEnabled verifies
// that TalkGroupMcastAddr is skipped when the comms subsystem is enabled.
func TestJoinMulticastGroupsOnInterface_SkipsTalkGroupWhenCommsEnabled(t *testing.T) {
	fj := newFakeJoiner()
	r := createTestBLOSWithFakeJoiner(fj)
	r.Config.CommsEnable = true

	err := r.joinMulticastGroupsOnInterface("lo")
	if err != nil {
		t.Fatalf("joinMulticastGroupsOnInterface failed: %v", err)
	}

	allAddrs := config.GetMulticastGroupAddresses()
	expectedCount := len(allAddrs) - 1 // TalkGroupMcastAddr is skipped

	if fj.callCount() != expectedCount {
		t.Errorf("expected %d join calls (TalkGroup skipped), got %d", expectedCount, fj.callCount())
	}

	if fj.calledWith(config.TalkGroupMcastAddr) {
		t.Errorf("expected TalkGroupMcastAddr %s to be skipped, but it was joined", config.TalkGroupMcastAddr)
	}

	for _, addr := range allAddrs {
		if addr == config.TalkGroupMcastAddr {
			continue
		}

		if !fj.calledWith(addr) {
			t.Errorf("expected join call for %s, not found", addr)
		}
	}

	defer r.Stop()
}

// TestJoinMulticastGroupsOnInterface_StoresConns verifies that successful joins
// populate r.multicastConns with non-nil connections.
func TestJoinMulticastGroupsOnInterface_StoresConns(t *testing.T) {
	fj := newFakeJoiner()
	r := createTestBLOSWithFakeJoiner(fj)
	r.Config.CommsEnable = false

	err := r.joinMulticastGroupsOnInterface("lo")
	if err != nil {
		t.Fatalf("joinMulticastGroupsOnInterface failed: %v", err)
	}

	defer r.Stop()

	allAddrs := config.GetMulticastGroupAddresses()

	if len(r.multicastConns) != len(allAddrs) {
		t.Errorf("expected %d stored conns, got %d", len(allAddrs), len(r.multicastConns))
	}

	for i, c := range r.multicastConns {
		if c == nil {
			t.Errorf("multicastConns[%d] is nil", i)
		}
	}
}

// TestJoinMulticastGroupsOnInterface_ClosesConnsOnError verifies that when a
// join fails mid-loop, the already-opened conns are closed and none are stored
// on the BLOS struct.
func TestJoinMulticastGroupsOnInterface_ClosesConnsOnError(t *testing.T) {
	allAddrs := config.GetMulticastGroupAddresses()
	if len(allAddrs) < 2 {
		t.Skip("need at least 2 multicast addresses to test rollback")
	}

	// Fail on the second address so at least one conn was opened first.
	failAddr := allAddrs[1]
	sentinelErr := errors.New("injected join failure")

	fj := newFakeJoiner()
	fj.failOn = failAddr
	fj.failErr = sentinelErr

	r := createTestBLOSWithFakeJoiner(fj)

	err := r.joinMulticastGroupsOnInterface("lo")
	if err == nil {
		t.Fatal("expected error from injected failure, got nil")
	}

	if !errors.Is(err, sentinelErr) {
		t.Errorf("expected sentinel error, got: %v", err)
	}

	if len(r.multicastConns) != 0 {
		t.Errorf("expected no conns stored after rollback, got %d", len(r.multicastConns))
	}
}

// TestBLOS_StopClosesMulticastConns verifies that Stop() closes all stored
// multicast connections and nils the slice.
func TestBLOS_StopClosesMulticastConns(t *testing.T) {
	fj := newFakeJoiner()
	r := createTestBLOSWithFakeJoiner(fj)

	err := r.joinMulticastGroupsOnInterface("lo")
	if err != nil {
		t.Fatalf("joinMulticastGroupsOnInterface failed: %v", err)
	}

	// Capture the conns before Stop clears them.
	captured := make([]*net.UDPConn, len(r.multicastConns))
	copy(captured, r.multicastConns)

	if len(captured) == 0 {
		t.Fatal("expected at least one conn to be stored before Stop")
	}

	r.Stop()

	if r.multicastConns != nil {
		t.Errorf("expected multicastConns to be nil after Stop, got len=%d", len(r.multicastConns))
	}

	// Verify conns are actually closed: closing an already-closed conn returns an error.
	for i, c := range captured {
		if err := c.Close(); err == nil {
			t.Errorf("captured conn[%d] still appears open after Stop (Close returned nil)", i)
		}
	}
}

// TestBLOS_StopWithNoConns verifies that Stop() is safe when no multicast
// sockets have been stored.
func TestBLOS_StopWithNoConns(t *testing.T) {
	r := createTestBLOS()

	// Must not panic.
	r.Stop()

	if r.multicastConns != nil {
		t.Error("expected multicastConns to remain nil")
	}
}
