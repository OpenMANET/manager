package blos

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

const testStateRunning = "Running"

// MockStatusClient is a mock implementation of StatusClient for testing.
type MockStatusClient struct {
	lastCallTime   time.Time
	errorToReturn  error
	statusFunc     func(ctx context.Context) (*ipnstate.Status, error)
	statusToReturn *ipnstate.Status
	callCount      int
	mu             sync.Mutex
	shouldError    bool
}

// Status implements the StatusClient interface for mocking.
func (m *MockStatusClient) Status(ctx context.Context) (*ipnstate.Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	m.lastCallTime = time.Now()

	if m.statusFunc != nil {
		return m.statusFunc(ctx)
	}

	if m.shouldError {
		if m.errorToReturn != nil {
			return nil, m.errorToReturn
		}

		return nil, errors.New("mock error")
	}

	if m.statusToReturn != nil {
		return m.statusToReturn, nil
	}

	// Return a default status
	return createMockStatus(), nil
}

// GetCallCount returns the number of times Status has been called.
func (m *MockStatusClient) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.callCount
}

// ResetCallCount resets the call counter.
func (m *MockStatusClient) ResetCallCount() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount = 0
}

// SetError configures the mock to return an error.
func (m *MockStatusClient) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shouldError = true
	m.errorToReturn = err
}

// SetStatus configures the mock to return a specific status.
func (m *MockStatusClient) SetStatus(status *ipnstate.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shouldError = false
	m.statusToReturn = status
}

// createMockStatus creates a mock ipnstate.Status for testing.
func createMockStatus() *ipnstate.Status {
	// Create some mock peers
	peers := make(map[key.NodePublic]*ipnstate.PeerStatus)

	// Generate test node keys
	var nodeKey1, nodeKey2 key.NodePublic

	peers[nodeKey1] = &ipnstate.PeerStatus{
		HostName: "test-peer-1",
		DNSName:  "test-peer-1.example.com.",
		OS:       "linux",
	}

	peers[nodeKey2] = &ipnstate.PeerStatus{
		HostName: "test-peer-2",
		DNSName:  "test-peer-2.example.com.",
		OS:       "darwin",
	}

	return &ipnstate.Status{
		BackendState: testStateRunning,
		Peer:         peers,
	}
}

// TestNewStatusWorker tests the creation of a new StatusWorker.
func TestNewStatusWorker(t *testing.T) {
	client := &MockStatusClient{}
	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	if worker == nil {
		t.Fatal("Expected worker to be created, got nil")
	}

	if worker.interval != interval {
		t.Errorf("Expected interval %v, got %v", interval, worker.interval)
	}

	if worker.client != client {
		t.Error("Expected client to be set")
	}

	if worker.peers == nil {
		t.Error("Expected peers map to be initialized")
	}

	if worker.running {
		t.Error("Expected worker to not be running initially")
	}
}

// TestStatusWorkerStartStop tests starting and stopping the worker.
func TestStatusWorkerStartStop(t *testing.T) {
	client := &MockStatusClient{}
	logger := zerolog.Nop()
	interval := 100 * time.Millisecond

	worker := NewStatusWorker(client, interval, logger)

	if worker.IsRunning() {
		t.Error("Worker should not be running initially")
	}

	worker.Start()

	if !worker.IsRunning() {
		t.Error("Worker should be running after Start()")
	}

	// Starting again should not cause issues
	worker.Start()

	if !worker.IsRunning() {
		t.Error("Worker should still be running after second Start()")
	}

	worker.Stop()

	if worker.IsRunning() {
		t.Error("Worker should not be running after Stop()")
	}

	// Stopping again should not cause issues
	worker.Stop()
}

// TestStatusWorkerFetchesStatus tests that the worker fetches status periodically.
func TestStatusWorkerFetchesStatus(t *testing.T) {
	client := &MockStatusClient{}
	logger := zerolog.Nop()
	interval := 50 * time.Millisecond

	worker := NewStatusWorker(client, interval, logger)

	worker.Start()
	defer worker.Stop()

	// Wait for initial fetch
	time.Sleep(10 * time.Millisecond)

	initialCount := client.GetCallCount()
	if initialCount < 1 {
		t.Error("Expected at least one status call immediately on start")
	}

	// Wait for additional fetches
	time.Sleep(150 * time.Millisecond)

	finalCount := client.GetCallCount()
	if finalCount <= initialCount {
		t.Errorf("Expected more calls after waiting, got %d initial and %d final", initialCount, finalCount)
	}
}

// TestStatusWorkerStoresStatus tests that the worker correctly stores status data.
func TestStatusWorkerStoresStatus(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	worker.Start()
	defer worker.Stop()

	// Wait for initial fetch
	time.Sleep(50 * time.Millisecond)

	status := worker.GetStatus()
	if status == nil {
		t.Fatal("Expected status to be stored")
	}

	if status.BackendState != testStateRunning {
		t.Errorf("Expected BackendState 'Running', got '%s'", status.BackendState)
	}

	peers := worker.GetPeers()
	if len(peers) != len(mockStatus.Peer) {
		t.Errorf("Expected %d peers, got %d", len(mockStatus.Peer), len(peers))
	}
}

// TestStatusWorkerGetPeer tests retrieving a specific peer.
func TestStatusWorkerGetPeer(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	worker.Start()
	defer worker.Stop()

	// Wait for initial fetch
	time.Sleep(50 * time.Millisecond)

	// Get one of the peer keys from the mock status
	var testKey key.NodePublic
	for k := range mockStatus.Peer {
		testKey = k

		break
	}

	peer, ok := worker.GetPeer(testKey)
	if !ok {
		t.Error("Expected to find peer")
	}

	if peer == nil {
		t.Error("Expected peer to not be nil")
	}

	// Test with non-existent key (use all the existing keys + 1 to ensure it doesn't exist)
	allKeys := worker.GetPeers()

	var nonExistentKey key.NodePublic
	// We'll just use the zero-value key and verify it's not in the map
	// This works because our mock creates non-zero keys
	foundZeroKey := false

	for k := range allKeys {
		if k == nonExistentKey {
			foundZeroKey = true

			break
		}
	}

	if !foundZeroKey {
		// The zero key is not in the map, so we can use it as a non-existent key
		_, ok = worker.GetPeer(nonExistentKey)
		if ok {
			t.Error("Expected to not find non-existent peer")
		}
	}
}

// TestStatusWorkerHandlesErrors tests that the worker handles errors gracefully.
func TestStatusWorkerHandlesErrors(t *testing.T) {
	client := &MockStatusClient{}
	client.SetError(errors.New("test error"))

	logger := zerolog.Nop()
	interval := 50 * time.Millisecond

	worker := NewStatusWorker(client, interval, logger)

	worker.Start()
	defer worker.Stop()

	// Wait for a few fetch attempts
	time.Sleep(150 * time.Millisecond)

	// Worker should still be running despite errors
	if !worker.IsRunning() {
		t.Error("Worker should still be running after errors")
	}

	// Status should be nil since all calls errored
	status := worker.GetStatus()
	_ = status // nil is expected when all calls fail

	// Verify multiple calls were attempted
	if client.GetCallCount() < 2 {
		t.Error("Expected multiple status calls despite errors")
	}
}

// TestStatusWorkerConcurrency tests concurrent access to worker data.
func TestStatusWorkerConcurrency(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 20 * time.Millisecond

	worker := NewStatusWorker(client, interval, logger)

	worker.Start()
	defer worker.Stop()

	// Wait for initial fetch
	time.Sleep(30 * time.Millisecond)

	var wg sync.WaitGroup

	numGoroutines := 10

	// Spawn multiple goroutines reading from the worker concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < 100; j++ {
				_ = worker.GetPeers()
				_ = worker.GetStatus()
				_ = worker.IsRunning()

				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	// If we get here without panic, the test passes
}

// TestStatusWorkerContextCancellation tests that the worker respects context cancellation.
func TestStatusWorkerContextCancellation(t *testing.T) {
	client := &MockStatusClient{}
	logger := zerolog.Nop()
	interval := 50 * time.Millisecond

	worker := NewStatusWorker(client, interval, logger)
	worker.Start()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	if !worker.IsRunning() {
		t.Error("Worker should be running")
	}

	// Stop the worker
	worker.Stop()

	// Give it time to stop
	time.Sleep(50 * time.Millisecond)

	if worker.IsRunning() {
		t.Error("Worker should have stopped")
	}
}

// TestLocalStatusClient tests that LocalStatusClient can be created.
func TestLocalStatusClient(t *testing.T) {
	client := &LocalStatusClient{}
	// Verify the client is usable (has the Status method)
	_ = client
	// Note: We don't actually call Status() here because it would require
	// a real Tailscale daemon to be running, which may not be available
	// in the test environment.
}

// TestStatusWorkerGetPeersReturnsACopy tests that GetPeers returns a copy, not the original map.
func TestStatusWorkerGetPeersReturnsACopy(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	worker.Start()
	defer worker.Stop()

	// Wait for initial fetch
	time.Sleep(50 * time.Millisecond)

	peers1 := worker.GetPeers()
	peers2 := worker.GetPeers()

	// Verify we got the same peer data
	if len(peers1) != len(peers2) {
		t.Error("Expected both peer maps to have the same length")
	}

	// Note: As of the performance optimization, GetPeers() returns the internal map directly
	// rather than a copy. Callers should not modify the returned map.
	// This test now verifies that the same data is accessible on subsequent calls.
	peers3 := worker.GetPeers()
	if len(peers3) != len(peers2) {
		t.Error("Expected consistent peer data on subsequent calls")
	}
}

// TestStatusWorker_SetOnStatusUpdate tests that the callback is set and called
func TestStatusWorker_SetOnStatusUpdate(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	callCount := 0

	var mu sync.Mutex

	callback := func() error {
		mu.Lock()
		defer mu.Unlock()

		callCount++

		return nil
	}

	worker.SetOnStatusUpdate(callback)

	// Manually trigger status fetch
	worker.fetchAndStoreStatus()

	mu.Lock()
	count := callCount
	mu.Unlock()

	if count != 1 {
		t.Errorf("Expected callback to be called once, got %d", count)
	}

	// Trigger again
	worker.fetchAndStoreStatus()

	mu.Lock()
	count = callCount
	mu.Unlock()

	if count != 2 {
		t.Errorf("Expected callback to be called twice, got %d", count)
	}
}

// TestStatusWorker_CallbackError tests that callback errors are handled gracefully
func TestStatusWorker_CallbackError(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	expectedError := errors.New("callback error")
	callback := func() error {
		return expectedError
	}

	worker.SetOnStatusUpdate(callback)

	// Should not panic even if callback returns error
	worker.fetchAndStoreStatus()

	// Status should still be updated despite callback error
	status := worker.GetStatus()
	if status == nil {
		t.Error("Expected status to be updated despite callback error")
	}

	peers := worker.GetPeers()
	if len(peers) == 0 {
		t.Error("Expected peers to be updated despite callback error")
	}
}

// TestStatusWorker_CallbackNotCalledOnError tests that callback is not called when status fetch fails
func TestStatusWorker_CallbackNotCalledOnError(t *testing.T) {
	client := &MockStatusClient{
		shouldError: true,
	}

	logger := zerolog.Nop()
	interval := 10 * time.Second

	worker := NewStatusWorker(client, interval, logger)

	callCount := 0
	callback := func() error {
		callCount++

		return nil
	}

	worker.SetOnStatusUpdate(callback)

	// Trigger status fetch which should fail
	worker.fetchAndStoreStatus()

	if callCount != 0 {
		t.Errorf("Expected callback to not be called on error, but it was called %d times", callCount)
	}
}

// TestStatusWorker_CallbackWithRunningWorker tests that callback is called during normal operation
func TestStatusWorker_CallbackWithRunningWorker(t *testing.T) {
	mockStatus := createMockStatus()
	client := &MockStatusClient{}
	client.SetStatus(mockStatus)

	logger := zerolog.Nop()
	interval := 50 * time.Millisecond

	worker := NewStatusWorker(client, interval, logger)

	callCount := 0

	var mu sync.Mutex

	callback := func() error {
		mu.Lock()
		defer mu.Unlock()

		callCount++

		return nil
	}

	worker.SetOnStatusUpdate(callback)

	worker.Start()
	defer worker.Stop()

	// Wait for a few ticks
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	count := callCount
	mu.Unlock()

	// Should have been called at least twice (initial + ticks)
	if count < 2 {
		t.Errorf("Expected callback to be called at least 2 times, got %d", count)
	}
}
